package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type requestStatsTaskHandler struct{}

func (requestStatsTaskHandler) Type() string            { return "channel_monitor_request_stats" }
func (requestStatsTaskHandler) Enabled() bool           { return common.RedisEnabled }
func (requestStatsTaskHandler) Interval() time.Duration { return time.Minute }
func (requestStatsTaskHandler) NewPayload() any         { return nil }
func (requestStatsTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	err := runRequestStatsDailyDecision(ctx, time.Now())
	mailErr := deliverRequestStatsNotifications(ctx)
	status := model.SystemTaskStatusSucceeded
	message := ""
	if err = errors.Join(err, mailErr); err != nil {
		status = model.SystemTaskStatusFailed
		message = err.Error()
	}
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, status, nil, message); finishErr != nil {
		common.SysError("用户请求统计任务结束失败: " + finishErr.Error())
	}
}

func init() { RegisterSystemTaskHandler(requestStatsTaskHandler{}) }

type requestStatsDecisionSample struct {
	UserID   int   `json:"user_id"`
	APIKeyID int   `json:"api_key_id"`
	Known    int64 `json:"known"`
	Low      int64 `json:"low"`
}

func requestStatsShouldDisable(known, low int64, config model.ChannelMonitorRequestStatsConfig) bool {
	if !config.Enabled || known < config.MinimumKnownInputRequests || known <= 0 || low < 0 || low > known {
		return false
	}
	left := new(big.Int).Mul(big.NewInt(low), big.NewInt(100))
	right := new(big.Int).Mul(big.NewInt(known), big.NewInt(int64(config.DisableRatePercent)))
	return left.Cmp(right) >= 0
}

func requestStatsRecentDrop(lastDroppedAt, cutoff int64) bool {
	return lastDroppedAt > 0 && lastDroppedAt >= cutoff-86400
}

func runRequestStatsDailyDecision(ctx context.Context, now time.Time) error {
	config, err := model.GetChannelMonitorRequestStatsConfig(ctx)
	if err != nil {
		return err
	}
	today := model.ChannelDailyCostDayStart(now.Unix())
	day := today - 86400
	// Do not apply stale decisions from an outage to keys being used on a new day.
	if err := model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDay{}).
		Where("day_start < ? AND status = ?", day, "pending").Updates(map[string]any{"status": "skipped", "last_error": "已超过次日判定窗口", "updated_at": now.Unix()}).Error; err != nil {
		return err
	}
	decision, config, err := model.FreezeRequestStatsDay(ctx, day, config)
	if err != nil || decision.Status != "pending" {
		return err
	}
	err = executeRequestStatsDecision(ctx, day, today, decision, config)
	if err != nil {
		message := []rune(err.Error())
		if len(message) > 255 {
			message = message[:255]
		}
		_ = model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDay{}).Where("day_start = ?", day).
			Updates(map[string]any{"last_error": string(message), "updated_at": now.Unix()}).Error
	}
	return err
}

func executeRequestStatsDecision(ctx context.Context, day, cutoff int64, decision model.ChannelMonitorRequestStatsDay, config model.ChannelMonitorRequestStatsConfig) error {
	var samples []requestStatsDecisionSample
	if decision.SnapshotJSON == "" {
		client := common.RedisMonitorConsumerClient()
		if !common.RedisEnabled || client == nil {
			return errors.New("统计队列不可用，稍后重试")
		}
		if err := requestStatsProjectionReady(ctx, client, cutoff); err != nil {
			return err
		}
		if err := ensureChannelMonitorDailyMetrics(ctx, client, day); err != nil {
			return err
		}
		if err := persistChannelMonitorDailyMetrics(ctx, client, day); err != nil {
			return err
		}
		var rows []model.ChannelMonitorDailySuccessLedger
		options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
		if model.DB.Dialector.Name() == "sqlite" {
			options = &sql.TxOptions{}
		}
		if err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var checkpoint model.ChannelMonitorDailyCheckpoint
			if err := tx.First(&checkpoint, "day_start = ?", day).Error; err != nil {
				return err
			}
			if checkpoint.CoveragePartial {
				return errors.New("当日统计存在缺失，跳过禁用并等待重试")
			}
			return tx.Where("day_start = ?", day).Find(&rows).Error
		}, options); err != nil {
			return err
		}
		byKey := map[int]*requestStatsDecisionSample{}
		for _, row := range rows {
			if row.APIKeyId <= 0 || row.UserId <= 0 {
				continue
			}
			var aggregate ChannelMonitorRedisSharedAggregate
			if row.AggregateJSON != "" {
				if err := common.UnmarshalJsonStr(row.AggregateJSON, &aggregate); err != nil {
					return err
				}
			}
			if err := validateRequestStatsDistribution(aggregate.RequestStats); err != nil {
				return err
			}
			sample := byKey[row.APIKeyId]
			if sample == nil {
				sample = &requestStatsDecisionSample{UserID: row.UserId, APIKeyID: row.APIKeyId}
				byKey[row.APIKeyId] = sample
			}
			if sample.UserID != row.UserId {
				return errors.New("API Key 统计归属不一致，停止自动判定")
			}
			var err error
			sample.Known, err = channelMonitorRedisSharedCheckedAddInt64(sample.Known, aggregate.RequestStats["known"])
			if err != nil {
				return err
			}
			sample.Low, err = channelMonitorRedisSharedCheckedAddInt64(sample.Low, requestStatsLowCount(aggregate.RequestStats, config.LowInputThresholdK))
			if err != nil {
				return err
			}
		}
		samples = []requestStatsDecisionSample{}
		for _, sample := range byKey {
			samples = append(samples, *sample)
		}
		encoded, err := common.Marshal(samples)
		if err != nil {
			return err
		}
		// Freeze the data as well as the policy before the first key mutation.
		if err := model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDay{}).
			Where("day_start = ? AND status = ? AND snapshot_json = ?", day, "pending", "").Update("snapshot_json", string(encoded)).Error; err != nil {
			return err
		}
		if err := model.DB.WithContext(ctx).First(&decision, "day_start = ?", day).Error; err != nil {
			return err
		}
	}
	if err := common.UnmarshalJsonStr(decision.SnapshotJSON, &samples); err != nil {
		return err
	}
	for _, sample := range samples {
		if !requestStatsShouldDisable(sample.Known, sample.Low, config) {
			continue
		}
		_, err := model.DisableRequestStatsToken(ctx, model.ChannelMonitorRequestStatsDisable{
			DayStart: day, APIKeyId: sample.APIKeyID, UserId: sample.UserID, ConfigRevision: config.Revision,
			ThresholdK: config.LowInputThresholdK, DisableRatePercent: config.DisableRatePercent,
			MinimumKnownInputRequests: config.MinimumKnownInputRequests, KnownInputRequests: sample.Known, LowInputCount: sample.Low,
		})
		if err != nil {
			return err
		}
	}
	return model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDay{}).Where("day_start = ? AND status = ?", day, "pending").
		Updates(map[string]any{"status": "completed", "last_error": "", "updated_at": time.Now().Unix()}).Error
}

func requestStatsProjectionReady(ctx context.Context, client *redis.Client, cutoff int64) error {
	stats := GetChannelMonitorEventWriterStats()
	if stats.QueueDepth > 0 || requestStatsRecentDrop(GetChannelMonitorEventPublishStats().LastDroppedAt, cutoff) {
		return errors.New("统计写入队列尚未完成或存在事件丢失，稍后重试")
	}
	var outbox int64
	// Creation time can be today for an event that completed yesterday. Wait
	// for the durable fallback to drain instead of using creation time as a
	// substitute for the event's statistical day.
	if err := model.DB.WithContext(ctx).Model(&model.ChannelMonitorEventOutbox{}).Where("processed_at = ?", 0).Count(&outbox).Error; err != nil {
		return err
	}
	if outbox > 0 {
		return errors.New("持久统计队列尚未完成，稍后重试")
	}
	quarantined, err := client.HGet(ctx, ChannelMonitorRedisObservabilityKey, ChannelMonitorRedisObservabilityFieldLastQuarantinedAt).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if quarantined != "" {
		at, err := strconv.ParseInt(quarantined, 10, 64)
		if err != nil {
			return err
		}
		if at >= cutoff-86400 {
			return errors.New("统计事件存在隔离记录，停止自动判定")
		}
	}
	groups, err := loadChannelMonitorRedisGroups(ctx, client)
	if err != nil {
		return err
	}
	found := false
	for _, group := range groups {
		if group.Name != ChannelMonitorRedisConsumerGroup {
			continue
		}
		found = true
		pending, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: ChannelMonitorRedisEventStream, Group: group.Name, Start: "-", End: "+", Count: 1001}).Result()
		if err != nil {
			return err
		}
		if len(pending) > 1000 {
			return errors.New("统计消费积压，稍后重试")
		}
		for _, entry := range pending {
			messages, err := client.XRangeN(ctx, ChannelMonitorRedisEventStream, entry.ID, entry.ID, 1).Result()
			if err != nil {
				return err
			}
			if len(messages) != 1 {
				return errors.New("统计待处理事件缺失")
			}
			var event model.ChannelMonitorEvent
			if err := common.UnmarshalJsonStr(channelMonitorRedisSharedRedisValueString(messages[0].Values[ChannelMonitorRedisEventFieldPayload]), &event); err != nil {
				return err
			}
			if event.OccurredAt < cutoff {
				return errors.New("前一日事件尚未聚合，稍后重试")
			}
		}
		unread, err := client.XRangeN(ctx, ChannelMonitorRedisEventStream, "("+group.LastDeliveredID, "+", 1001).Result()
		if err != nil {
			return err
		}
		if len(unread) > 1000 {
			return errors.New("统计未读事件积压，稍后重试")
		}
		for _, entry := range unread {
			var event model.ChannelMonitorEvent
			if err := common.UnmarshalJsonStr(channelMonitorRedisSharedRedisValueString(entry.Values[ChannelMonitorRedisEventFieldPayload]), &event); err != nil {
				return err
			}
			if event.OccurredAt < cutoff {
				return errors.New("前一日事件尚未消费，稍后重试")
			}
		}
	}
	if !found {
		return fmt.Errorf("统计消费组尚未就绪")
	}
	return nil
}
