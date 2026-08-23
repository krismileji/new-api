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
	activeStatuses := []string{
		model.ChannelModelDetectionRunStatusQueued,
		model.ChannelModelDetectionRunStatusWaitingDetector,
		model.ChannelModelDetectionRunStatusSubmitting,
		model.ChannelModelDetectionRunStatusRunning,
		model.ChannelModelDetectionRunStatusSubmissionUnknown,
		model.ChannelModelDetectionRunStatusCanceling,
	}
	var runs []model.ChannelModelDetectionRun
	if err := db.WithContext(ctx).
		Select("id", "run_id", "channel_id", "status", "logical_channel_id", "logical_revision", "logical_member_snapshot_json").
		Where("status IN ?", activeStatuses).Order("channel_id ASC, id ASC").Find(&runs).Error; err != nil {
		return err
	}
	deleted := make(map[int]struct{}, len(normalized))
	for _, channelID := range normalized {
		deleted[channelID] = struct{}{}
	}
	runByID := make(map[string]model.ChannelModelDetectionRun, len(runs))
	selected := make(map[string]struct{})
	for _, run := range runs {
		runByID[run.RunId] = run
		if _, ownerDeleted := deleted[run.ChannelId]; ownerDeleted {
			selected[run.RunId] = struct{}{}
		}
		if run.LogicalRevision <= 0 {
			continue
		}
		members, err := run.LogicalMemberSnapshot()
		if err != nil {
			return err
		}
		for _, member := range members {
			if _, memberDeleted := deleted[member.ChannelID]; memberDeleted {
				selected[run.RunId] = struct{}{}
				break
			}
		}
	}
	if len(runs) > 0 && db.Migrator().HasTable(&model.ChannelModelDetectionExecution{}) {
		var executionRunIDs []string
		if err := db.WithContext(ctx).Model(&model.ChannelModelDetectionExecution{}).
			Where("channel_id IN ?", normalized).Distinct().Pluck("run_id", &executionRunIDs).Error; err != nil {
			return err
		}
		for _, runID := range executionRunIDs {
			if _, active := runByID[runID]; active {
				selected[runID] = struct{}{}
			}
		}
	}
	runIDs := make([]string, 0, len(selected))
	for runID := range selected {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)
	for _, runID := range runIDs {
		if _, err := CancelChannelModelDetectionRun(ctx, db, runID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("删除渠道前取消模型检测轮次 %s 失败: %w", runID, err)
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
