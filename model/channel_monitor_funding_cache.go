package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// One durable repair per cache key, committed with the balance mutation.
// Token cache keys contain an HMAC, never a usable API credential.
type ChannelMonitorFundingCacheRepair struct {
	CacheKey  string `gorm:"size:128;primaryKey"`
	Revision  string `gorm:"size:36;not null"`
	UpdatedAt int64  `gorm:"not null;index"`
}

func queueChannelMonitorFundingCacheRepair(tx *gorm.DB, key string) error {
	if !common.RedisEnabled {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "cache_key"}},
		// Keep the oldest pending timestamp so a busy account cannot be
		// continually moved behind other accounts during a repair backlog.
		DoUpdates: clause.AssignmentColumns([]string{"revision"}),
	}).Create(&ChannelMonitorFundingCacheRepair{CacheKey: key, Revision: common.GetUUID(), UpdatedAt: time.Now().Unix()}).Error
}

func fundingCacheGeneration(key string) (string, error) {
	if !common.RedisEnabled {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	value, err := common.RDB.Get(ctx, "funding:generation:"+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}

// Advancing the generation and deleting the hash is atomic. A reader must
// capture the generation BEFORE reading DB, then compare it inside its cache
// publication script. Generations do not expire: a paused reader has no TTL.
func invalidateFundingCacheKeys(ctx context.Context, client *redis.Client, keys []string) error {
	const script = `
for i = 1, #KEYS, 2 do
  redis.call('SET', KEYS[i+1], ARGV[1])
  redis.call('DEL', KEYS[i])
end
return 1`
	paired := make([]string, 0, 2*len(keys))
	for _, key := range keys {
		paired = append(paired, key, "funding:generation:"+key)
	}
	return client.Eval(ctx, script, paired, common.GetUUID()).Err()
}

func RecoverChannelMonitorFundingCaches(ctx context.Context, limit int) (int, error) {
	if !common.RedisEnabled || common.RDB == nil || limit <= 0 {
		return 0, nil
	}
	// The cursor is independent of repair rows: a locked ACK must not also
	// block advancing the scan. Losing Redis state only restarts the scan;
	// SQL remains the durable source of pending work. A fixed upper bound
	// prevents newly inserted keys from indefinitely extending this sweep.
	const cursorKey = "channel_monitor:funding_cache_recovery:cursor"
	client := common.RedisMonitorWriteClient()
	cursorCtx, stopCursor := context.WithTimeout(ctx, 5*time.Second)
	saved, err := client.Get(cursorCtx, cursorKey).Result()
	stopCursor()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, err
	}
	var cursor struct {
		After    string `json:"after"`
		Through  string `json:"through"`
		Revision string `json:"revision"`
	}
	if saved != "" {
		if err := common.UnmarshalJsonStr(saved, &cursor); err != nil {
			// Scan position is disposable, unlike the durable SQL intents.
			cursor.After, cursor.Through = "", ""
		}
	}
	var repairs []ChannelMonitorFundingCacheRepair
	if cursor.Through != "" {
		err = DB.WithContext(ctx).Where("cache_key > ? AND cache_key <= ?", cursor.After, cursor.Through).
			Order("cache_key ASC").Limit(min(limit, 500)).Find(&repairs).Error
		if err != nil {
			return 0, err
		}
	}
	if len(repairs) == 0 {
		var last ChannelMonitorFundingCacheRepair
		err = DB.WithContext(ctx).Order("cache_key DESC").Take(&last).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		cursor.After, cursor.Through = "", last.CacheKey
		if err := DB.WithContext(ctx).Where("cache_key <= ?", cursor.Through).
			Order("cache_key ASC").Limit(min(limit, 500)).Find(&repairs).Error; err != nil {
			return 0, err
		}
	}
	completed := 0
	var failures error
	for _, repair := range repairs {
		if ctx.Err() != nil {
			return completed, errors.Join(failures, ctx.Err())
		}
		cursor.After = repair.CacheKey
		cursor.Revision = common.GetUUID()
		next, err := common.Marshal(cursor)
		if err != nil {
			return completed, errors.Join(failures, err)
		}
		recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		// Save progress before attempting the row, even if SQL ACK blocks.
		// CAS avoids overwriting progress changed by another recovery call.
		advanced, err := client.Eval(recordCtx, `
if (redis.call('GET', KEYS[1]) or '') ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2])
return 1`, []string{cursorKey}, saved, string(next)).Int()
		if err != nil || advanced != 1 {
			cancel()
			return completed, errors.Join(failures, err)
		}
		saved = string(next)
		err = invalidateFundingCacheKeys(recordCtx, client, []string{repair.CacheKey})
		// A concurrent commit replaces Revision; never ACK that newer intent.
		if err == nil {
			result := DB.WithContext(recordCtx).Where("cache_key = ? AND revision = ?", repair.CacheKey, repair.Revision).Delete(&ChannelMonitorFundingCacheRepair{})
			err = result.Error
			if err == nil {
				completed += int(result.RowsAffected)
			}
		}
		cancel()
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("额度缓存 %s 恢复失败: %w", repair.CacheKey, err))
		}
	}
	return completed, failures
}
