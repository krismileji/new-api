package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const channelLimitRegistryKey = "channelLimitGroups:v1:registry"
const channelLimitQueueSize = 256

type ChannelLimitStatus struct {
	Members  []int                   `json:"members,omitempty"`
	Tiers    []ChannelLimitTierUsage `json:"tiers,omitempty"`
	GroupID  int64                   `json:"group_id"`
	Priority int                     `json:"priority"`
	Active   int                     `json:"active"`
	RPM      int                     `json:"rpm"`
	Waiting  int                     `json:"waiting"`
	Reason   string                  `json:"reason,omitempty"`
}

type ChannelLimitTierUsage struct {
	Priority int `json:"priority"`
	Active   int `json:"active"`
	RPM      int `json:"rpm"`
}

type channelLimitRegistry struct {
	Groups  map[string]model.ChannelLimitGroup `json:"groups"`
	Members map[string]int64                   `json:"members"`
}

var channelLimitConfig = struct {
	sync.Mutex
	writes   sync.Mutex
	db       *gorm.DB
	registry channelLimitRegistry
	local    map[int64]*channelLimitLocalPool
}{local: make(map[int64]*channelLimitLocalPool)}

func channelLimitRegistryFromGroups(groups []model.ChannelLimitGroup) channelLimitRegistry {
	registry := channelLimitRegistry{Groups: make(map[string]model.ChannelLimitGroup), Members: make(map[string]int64)}
	for _, group := range groups {
		registry.Groups[strconv.FormatInt(group.ID, 10)] = group
		for _, member := range group.Members {
			registry.Members[strconv.Itoa(member.ChannelID)] = group.ID
		}
	}
	return registry
}

// Configuration and channel usage are independent; rebuilding the registry preserves usage.
func ensureChannelLimitRegistry(ctx context.Context) error {
	if !common.RedisEnabled {
		channelLimitConfig.Lock()
		defer channelLimitConfig.Unlock()
		if channelLimitConfig.db == model.DB && channelLimitConfig.registry.Groups != nil {
			return nil
		}
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			groups, _, err := model.ReadChannelLimitGroups(model.DB)
			if err != nil {
				return err
			}
			if len(groups) > 0 {
				return errors.New("Redis 不可用，共享限流停止新准入")
			}
			return nil
		}
		ready, err := common.RDB.HGet(ctx, channelLimitRegistryKey, "ready").Result()
		if err != nil && err != redis.Nil {
			return err
		}
		if ready == "1" {
			return nil
		}
	}
	groups, revision, err := model.ReadChannelLimitGroups(model.DB)
	if err != nil {
		return err
	}
	registry := channelLimitRegistryFromGroups(groups)
	if common.RedisEnabled {
		payload, err := common.Marshal(registry)
		if err != nil {
			return err
		}
		return common.RDB.Eval(ctx, channelLimitPublishScript, []string{channelLimitRegistryKey}, string(payload), revision).Err()
	}
	channelLimitConfig.db = model.DB
	channelLimitConfig.registry = registry
	channelLimitConfig.local = make(map[int64]*channelLimitLocalPool)
	return nil
}

// SaveChannelLimitGroup publishes configuration without resetting channel usage.
// Admissions keep using the previous configuration until the atomic publication.
func SaveChannelLimitGroup(ctx context.Context, group *model.ChannelLimitGroup, remove bool) error {
	if model.DB == nil {
		return errors.New("数据库不可用")
	}
	channelLimitConfig.writes.Lock()
	defer channelLimitConfig.writes.Unlock()
	updated := *group
	var next []model.ChannelLimitGroup
	var version int64
	err := model.MutateChannelLimitGroup(ctx, &updated, remove, func(_, groups []model.ChannelLimitGroup, revision int64) error {
		next, version = groups, revision
		return nil
	})
	if err != nil {
		return err
	}
	*group = updated
	registry := channelLimitRegistryFromGroups(next)
	if common.RedisEnabled {
		if common.RDB == nil {
			return errors.New("配置已保存，但 Redis 不可用，请恢复连接后重新保存")
		}
		payload, err := common.Marshal(registry)
		if err != nil {
			return err
		}
		publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelConcurrencyRedisOpTimeout)
		defer cancel()
		if err := common.RDB.Eval(publishCtx, channelLimitPublishScript, []string{channelLimitRegistryKey}, string(payload), version).Err(); err != nil {
			return fmt.Errorf("配置已保存但尚未生效，请刷新并重新保存: %w", err)
		}
	}
	channelLimitConfig.Lock()
	defer channelLimitConfig.Unlock()
	channelLimitConfig.db = model.DB
	channelLimitConfig.registry = registry
	if remove {
		delete(channelLimitConfig.local, group.ID)
	}
	return nil
}

type channelAdmissionContextKey struct{}
type ChannelAdmission struct {
	ID        string
	Deadline  time.Time
	Waiting   bool
	groupID   int64
	channelID int
}

func NewChannelAdmission(ctx context.Context, deadline time.Time) (context.Context, *ChannelAdmission) {
	admission := &ChannelAdmission{ID: common.NodeName + ":" + common.GetUUID(), Deadline: deadline}
	return context.WithValue(ctx, channelAdmissionContextKey{}, admission), admission
}

func (admission *ChannelAdmission) Close() {
	if admission == nil || admission.groupID == 0 {
		return
	}
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisOpTimeout)
		defer cancel()
		prefix := fmt.Sprintf("channelLimitGroups:v1:group:%d:", admission.groupID)
		_ = common.RDB.Eval(ctx, `redis.call('ZREM', KEYS[1], ARGV[1]); redis.call('HDEL', KEYS[2], ARGV[1]); return 1`, []string{prefix + "queue", prefix + "waiters"}, admission.ID).Err()
	} else {
		channelLimitConfig.Lock()
		if pool := channelLimitConfig.local[admission.groupID]; pool != nil {
			delete(pool.waiters, admission.ID)
		}
		channelLimitConfig.Unlock()
	}
	admission.groupID = 0
}

const channelLimitPublishScript = `
local current = tonumber(redis.call('HGET', KEYS[1], 'revision') or '-1')
if tonumber(ARGV[2]) < current then return 0 end
local registry = cjson.decode(ARGV[1])
for _, key in ipairs(redis.call('HKEYS', KEYS[1])) do
 if string.sub(key,1,6) == 'group:' and not registry.groups[string.sub(key,7)] then
  local prefix = 'channelLimitGroups:v1:group:'..string.sub(key,7)..':'
  redis.call('DEL',prefix..'queue',prefix..'waiters',prefix..'sequence')
 end
 if string.sub(key,1,7) == 'member:' or string.sub(key,1,6) == 'group:' then redis.call('HDEL', KEYS[1], key) end
end
for id, group in pairs(registry.groups) do redis.call('HSET', KEYS[1], 'group:'..id, cjson.encode(group)) end
for id, group in pairs(registry.members) do redis.call('HSET', KEYS[1], 'member:'..id, group) end
redis.call('HSET', KEYS[1], 'data', ARGV[1], 'revision', ARGV[2], 'ready', '1')
redis.call('HDEL', KEYS[1], 'fence', 'pending')
return 1
`
