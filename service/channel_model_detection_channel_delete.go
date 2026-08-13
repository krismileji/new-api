package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

func CancelChannelModelDetectionRunsForChannels(ctx context.Context, db *gorm.DB, channelIDs []int) error {
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return errors.New("模型检测数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	seen := make(map[int]struct{}, len(channelIDs))
	normalized := make([]int, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, exists := seen[channelID]; exists {
			continue
		}
		seen[channelID] = struct{}{}
		normalized = append(normalized, channelID)
	}
	if len(normalized) == 0 || !db.Migrator().HasTable(&model.ChannelModelDetectionRun{}) {
		return nil
	}
	sort.Ints(normalized)
	var runs []model.ChannelModelDetectionRun
	if err := db.WithContext(ctx).
		Select("run_id", "channel_id", "status").
		Where("channel_id IN ? AND status IN ?", normalized, []string{
			model.ChannelModelDetectionRunStatusQueued,
			model.ChannelModelDetectionRunStatusWaitingDetector,
			model.ChannelModelDetectionRunStatusSubmitting,
			model.ChannelModelDetectionRunStatusRunning,
			model.ChannelModelDetectionRunStatusSubmissionUnknown,
			model.ChannelModelDetectionRunStatusCanceling,
		}).Order("channel_id ASC, id ASC").Find(&runs).Error; err != nil {
		return err
	}
	for _, run := range runs {
		if _, err := CancelChannelModelDetectionRun(ctx, db, run.RunId); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("删除渠道前取消模型检测轮次 %s 失败: %w", run.RunId, err)
		}
	}
	return nil
}

func CancelChannelModelDetectionRunsForStatuses(ctx context.Context, db *gorm.DB, statuses []int64) error {
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return errors.New("模型检测数据库不可用")
	}
	if len(statuses) == 0 {
		return nil
	}
	var channelIDs []int
	if err := db.WithContext(ctx).Model(&model.Channel{}).
		Where("status IN ?", statuses).
		Order("id ASC").
		Pluck("id", &channelIDs).Error; err != nil {
		return err
	}
	return CancelChannelModelDetectionRunsForChannels(ctx, db, channelIDs)
}
