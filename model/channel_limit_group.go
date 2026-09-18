package model

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	MaxChannelLimit         = 100_000
	MaxChannelLimitGroups   = 128
	MaxChannelLimitMembers  = 64
	MaxChannelLimitTiers    = 8
	MaxChannelLimitPriority = 1000
)

var ErrChannelLimitConflict = errors.New("共享限流配置已变化，请刷新后重试")

type ChannelLimitGroup struct {
	ID               int64                     `json:"id" gorm:"primaryKey"`
	Name             string                    `json:"name" gorm:"type:varchar(128);not null"`
	ConcurrencyLimit int                       `json:"concurrency_limit" gorm:"not null"`
	RPMLimit         int                       `json:"rpm_limit" gorm:"not null"`
	Enabled          bool                      `json:"enabled" gorm:"not null"`
	Revision         int64                     `json:"revision" gorm:"not null"`
	UpdatedAt        int64                     `json:"updated_at" gorm:"not null"`
	Tiers            []ChannelLimitGroupTier   `json:"tiers" gorm:"-"`
	Members          []ChannelLimitGroupMember `json:"members" gorm:"-"`
}

type ChannelLimitGroupTier struct {
	ID                  int64 `json:"-" gorm:"primaryKey"`
	GroupID             int64 `json:"-" gorm:"not null;uniqueIndex:uk_limit_tier"`
	Priority            int   `json:"priority" gorm:"not null;uniqueIndex:uk_limit_tier"`
	ReservedConcurrency int   `json:"reserved_concurrency" gorm:"not null"`
	ReservedRPM         int   `json:"reserved_rpm" gorm:"not null"`
}

type ChannelLimitGroupMember struct {
	ID        int64 `json:"-" gorm:"primaryKey"`
	GroupID   int64 `json:"-" gorm:"not null;index"`
	ChannelID int   `json:"channel_id" gorm:"not null;uniqueIndex"`
	Priority  int   `json:"priority" gorm:"not null"`
}

// The registry row serializes whole configuration publications across nodes.
type ChannelLimitGroupRevision struct {
	ID       int   `gorm:"primaryKey"`
	Revision int64 `gorm:"not null"`
}

func (group *ChannelLimitGroup) Validate() error {
	group.Name = strings.TrimSpace(group.Name)
	if group.Name == "" || len([]rune(group.Name)) > 128 {
		return errors.New("共享限流组名称必须为 1 到 128 个字符")
	}
	if group.ConcurrencyLimit < 0 || group.ConcurrencyLimit > MaxChannelLimit || group.RPMLimit < 0 || group.RPMLimit > MaxChannelLimit {
		return errors.New("并发与 RPM 限制必须在 0 到 100000 之间")
	}
	if len(group.Tiers) == 0 || len(group.Tiers) > MaxChannelLimitTiers || len(group.Members) == 0 || len(group.Members) > MaxChannelLimitMembers {
		return errors.New("共享限流组必须有 1 到 8 个等级和 1 到 64 个成员")
	}
	priorities := make(map[int]bool)
	concurrency, rpm := 0, 0
	for _, tier := range group.Tiers {
		if tier.Priority < 0 || tier.Priority > MaxChannelLimitPriority || priorities[tier.Priority] {
			return errors.New("资源优先级必须在 0 到 1000 之间且不能重复")
		}
		if tier.ReservedConcurrency < 0 || tier.ReservedConcurrency > MaxChannelLimit || tier.ReservedRPM < 0 || tier.ReservedRPM > MaxChannelLimit {
			return errors.New("预留额度必须在 0 到 100000 之间")
		}
		priorities[tier.Priority] = true
		concurrency += tier.ReservedConcurrency
		rpm += tier.ReservedRPM
	}
	if concurrency > group.ConcurrencyLimit || rpm > group.RPMLimit {
		return errors.New("预留额度合计不能超过组上限；不限额的维度不能设置预留")
	}
	members := make(map[int]bool)
	for _, member := range group.Members {
		if member.ChannelID <= 0 || members[member.ChannelID] || !priorities[member.Priority] {
			return errors.New("共享限流成员重复、渠道 ID 无效或资源等级不存在")
		}
		members[member.ChannelID] = true
	}
	sort.Slice(group.Tiers, func(i, j int) bool { return group.Tiers[i].Priority > group.Tiers[j].Priority })
	sort.Slice(group.Members, func(i, j int) bool { return group.Members[i].ChannelID < group.Members[j].ChannelID })
	return nil
}

func ReadChannelLimitGroups(db *gorm.DB) ([]ChannelLimitGroup, int64, error) {
	groups := make([]ChannelLimitGroup, 0)
	if db == nil {
		return groups, 0, nil
	}
	if !db.Migrator().HasTable(&ChannelLimitGroupRevision{}) {
		// HasTable only returns a bool; do not mistake a database outage for an
		// older database without the feature and publish an empty registry.
		var reachable int
		if err := db.Raw("SELECT 1").Scan(&reachable).Error; err != nil {
			return nil, 0, err
		}
		return groups, 0, nil
	}
	var registry ChannelLimitGroupRevision
	if err := db.First(&registry, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return groups, 0, nil
		}
		return nil, 0, err
	}
	if err := db.Order("id ASC").Find(&groups).Error; err != nil {
		return nil, 0, err
	}
	var tiers []ChannelLimitGroupTier
	var members []ChannelLimitGroupMember
	if err := db.Order("priority DESC").Find(&tiers).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("channel_id ASC").Find(&members).Error; err != nil {
		return nil, 0, err
	}
	for i := range groups {
		groups[i].Tiers = make([]ChannelLimitGroupTier, 0)
		groups[i].Members = make([]ChannelLimitGroupMember, 0)
		for _, tier := range tiers {
			if tier.GroupID == groups[i].ID {
				groups[i].Tiers = append(groups[i].Tiers, tier)
			}
		}
		for _, member := range members {
			if member.GroupID == groups[i].ID {
				groups[i].Members = append(groups[i].Members, member)
			}
		}
	}
	var latest ChannelLimitGroupRevision
	if err := db.First(&latest, 1).Error; err != nil {
		return nil, 0, err
	}
	if latest.Revision != registry.Revision {
		return nil, 0, ErrChannelLimitConflict
	}
	return groups, registry.Revision, nil
}

// MutateChannelLimitGroup exposes the next configuration inside the locked
// transaction. The service publishes that snapshot only after commit succeeds.
func MutateChannelLimitGroup(ctx context.Context, group *ChannelLimitGroup, remove bool, publish func([]ChannelLimitGroup, []ChannelLimitGroup, int64) error) error {
	if !remove {
		if err := group.Validate(); err != nil {
			return err
		}
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var registry ChannelLimitGroupRevision
		if err := tx.FirstOrCreate(&registry, ChannelLimitGroupRevision{ID: 1}).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&registry, 1).Error; err != nil {
			return err
		}
		if registry.Revision >= math.MaxInt64-1 {
			return errors.New("共享限流配置版本已达上限")
		}
		old, _, err := ReadChannelLimitGroups(tx)
		if err != nil {
			return err
		}
		var previous *ChannelLimitGroup
		for i := range old {
			if old[i].ID == group.ID {
				previous = &old[i]
			}
		}
		if group.ID != 0 && (previous == nil || previous.Revision != group.Revision) {
			return ErrChannelLimitConflict
		}
		if group.ID == 0 && (remove || group.Revision != 0 || len(old) >= MaxChannelLimitGroups) {
			return ErrChannelLimitConflict
		}
		if !remove {
			ids := make([]int, 0, len(group.Members))
			for _, member := range group.Members {
				ids = append(ids, member.ChannelID)
			}
			var channels []Channel
			if err := lockForUpdate(tx).Select("id").Where("id IN ?", ids).Order("id ASC").Find(&channels).Error; err != nil {
				return err
			}
			if len(channels) != len(ids) {
				return errors.New("共享限流组包含不存在的渠道")
			}
			var bound int64
			if err := tx.Model(&ChannelLimitGroupMember{}).Where("channel_id IN ? AND group_id <> ?", ids, group.ID).Count(&bound).Error; err != nil {
				return err
			}
			if bound != 0 {
				return errors.New("渠道已加入其他共享限流组，请先从原组移出")
			}
		}
		group.Revision = registry.Revision + 1
		group.UpdatedAt = time.Now().UnixMilli()
		if remove {
			if err := tx.Delete(&ChannelLimitGroup{}, group.ID).Error; err != nil {
				return err
			}
		} else if err := tx.Save(group).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&ChannelLimitGroupTier{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&ChannelLimitGroupMember{}).Error; err != nil {
			return err
		}
		if !remove {
			for i := range group.Tiers {
				group.Tiers[i].ID = 0
				group.Tiers[i].GroupID = group.ID
			}
			for i := range group.Members {
				group.Members[i].ID = 0
				group.Members[i].GroupID = group.ID
			}
			if err := tx.Create(&group.Tiers).Error; err != nil {
				return err
			}
			if err := tx.Create(&group.Members).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&registry).Update("revision", group.Revision).Error; err != nil {
			return err
		}
		next, _, err := ReadChannelLimitGroups(tx)
		if err != nil {
			return err
		}
		return publish(old, next, group.Revision)
	})
}
