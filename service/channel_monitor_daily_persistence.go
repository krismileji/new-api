package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const channelMonitorDailyDirtyDaysKey = ChannelMonitorRedisKeyPrefix + ":daily:dirty"
const channelMonitorDailyVersionField = "meta:daily_projection_version"

func channelMonitorDailyMetricScope(identity model.ChannelMonitorDailyMetricIdentity) string {
	data, _ := common.Marshal(identity)
	return "fact:" + base64.RawURLEncoding.EncodeToString(data)
}

func channelMonitorDailyMetricIdentity(scope string) (model.ChannelMonitorDailyMetricIdentity, error) {
	var identity model.ChannelMonitorDailyMetricIdentity
	raw, err := base64.RawURLEncoding.DecodeString(scope)
	if err != nil {
		return identity, err
	}
	err = common.Unmarshal(raw, &identity)
	return identity, err
}

func ensureChannelMonitorDailyMetrics(ctx context.Context, client *redis.Client, day int64) error {
	if model.DB == nil || !common.RedisEnabled {
		return nil
	}
	version, err := client.HGet(ctx, ChannelMonitorRedisSuccessDayKey(day), channelMonitorDailyVersionField).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if version == "1" {
		return nil
	}
	if !model.DB.Migrator().HasTable(&model.ChannelMonitorDailyCheckpoint{}) {
		return nil
	}
	return rebuildChannelMonitorDailyMetrics(ctx, client, day)
}

func rebuildChannelMonitorDailyMetrics(ctx context.Context, client *redis.Client, day int64) error {
	key := ChannelMonitorRedisSuccessDayKey(day)
	leaseKey := key + ":rebuild:lease"
	token := common.GetUUID()
	acquired, err := client.SetNX(ctx, leaseKey, token, channelMonitorRedisDailyRebuildLeaseTTL).Result()
	if err != nil {
		return err
	}
	if !acquired {
		return errors.New("渠道监控日汇总正在重建")
	}
	defer client.Eval(context.WithoutCancel(ctx), channelMonitorRedisLeaseReleaseScript, []string{leaseKey}, token)
	for attempt := 0; attempt < channelMonitorRedisSharedWriteRetries; attempt++ {
		err = client.Watch(ctx, func(tx *redis.Tx) error {
			var checkpoint model.ChannelMonitorDailyCheckpoint
			var rows []model.ChannelMonitorDailySuccessLedger
			options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
			if model.DB.Dialector.Name() == "sqlite" {
				options = &sql.TxOptions{}
			}
			err := model.DB.WithContext(ctx).Transaction(func(db *gorm.DB) error {
				err := db.Where("day_start = ?", day).First(&checkpoint).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				return db.Where("day_start = ?", day).Find(&rows).Error
			}, options)
			if err != nil {
				return err
			}
			aggregates := make(map[string]ChannelMonitorRedisSharedAggregate)
			for _, row := range rows {
				if err := validateChannelMonitorDailySuccessLedgerRow(row); err != nil {
					return err
				}
				aggregate := ChannelMonitorRedisSharedAggregate{
					APIKeyName: row.APIKeyName, ActualSuccessCount: row.ActualSuccessCount,
					ActualFailureCount: row.ActualFailureCount, FinalSuccessCount: row.FinalSuccessCount,
					FinalFailureCount: row.FinalFailureCount, CacheHitCount: row.CacheHitCount,
					CacheSampleCount: row.CacheSampleCount, CacheReadTokens: row.CacheReadTokens,
					InputTokens: row.InputTokens, CacheWriteRequestCount: row.CacheWriteCount,
				}
				if row.AggregateJSON != "" {
					if err := common.UnmarshalJsonStr(row.AggregateJSON, &aggregate); err != nil {
						return err
					}
				}
				event := model.ChannelMonitorEvent{ChannelId: row.ChannelId, UserId: row.UserId,
					APIKeyId: row.APIKeyId, APIKeyName: row.APIKeyName, ModelName: row.ModelName, GroupName: row.GroupName}
				for _, scope := range channelMonitorRedisSuccessDayScopes(event) {
					if strings.HasPrefix(scope, "fact:") {
						continue
					}
					value := aggregates[scope]
					if err := mergeChannelMonitorRedisSharedAggregate(&value, aggregate); err != nil {
						return err
					}
					aggregates[scope] = value
				}
				aggregates[channelMonitorDailyMetricScope(model.ChannelMonitorDailyMetricIdentityFromRow(row))] = aggregate
			}
			fields := encodeChannelMonitorRedisDailySuccessAggregates(aggregates)
			if checkpoint.CoveragePartial {
				fields["meta:coverage_partial"] = "1"
			}
			fields["meta:event_watermark"] = strconv.FormatInt(checkpoint.EventWatermark, 10)
			fields["meta:data_cutoff_at"] = strconv.FormatInt(checkpoint.DataCutoffAt, 10)
			// Legacy rows have no Stream checkpoint. Their old closed-minute
			// boundary is used only for migration, never for new late events.
			legacyThrough := int64(0)
			if checkpoint.Revision == 0 && len(rows) > 0 {
				if coverage, err := model.GetChannelMonitorAggregationCoverage(ctx); err == nil {
					legacyThrough = coverage.CompletedThrough
				}
				fields["meta:coverage_partial"] = "1"
			}
			watermark, err := replayChannelMonitorDailyTail(ctx, client, day, checkpoint.EventWatermark, legacyThrough, fields)
			if err != nil {
				return err
			}
			fields[channelMonitorDailyVersionField] = "1"
			fields["meta:database_event_watermark"] = strconv.FormatInt(watermark, 10)
			fields["meta:event_watermark"] = strconv.FormatInt(watermark, 10)
			previousRevision, err := tx.HGet(ctx, key, "meta:revision").Int64()
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			revision, err := channelMonitorRedisSharedCheckedAddInt64(max(checkpoint.Revision, previousRevision), 1)
			if err != nil {
				return err
			}
			fields["meta:revision"] = strconv.FormatInt(revision, 10)
			fields["meta:processed_at"] = strconv.FormatInt(time.Now().Unix(), 10)
			owner, err := tx.Get(ctx, leaseKey).Result()
			if err != nil || owner != token {
				return errors.New("渠道监控日汇总重建租约已失效")
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, key)
				pipe.HSet(ctx, key, fields)
				pipe.Expire(ctx, key, channelMonitorRedisSharedSuccessDayTTL)
				pipe.SAdd(ctx, channelMonitorDailyDirtyDaysKey, strconv.FormatInt(day, 10))
				pipe.Incr(ctx, ChannelMonitorRedisSharedRevisionKey)
				return nil
			})
			return err
		}, key, leaseKey)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return redis.TxFailedErr
}

func replayChannelMonitorDailyTail(ctx context.Context, client *redis.Client, day, watermark, legacyThrough int64, fields map[string]string) (int64, error) {
	last, err := client.XRevRangeN(ctx, ChannelMonitorRedisEventStream, "+", "-", 1).Result()
	if err != nil {
		return watermark, err
	}
	if len(last) == 0 {
		if watermark > 0 {
			fields["meta:coverage_partial"] = "1"
		}
		return watermark, nil
	}
	if watermark > 0 {
		first, err := client.XRangeN(ctx, ChannelMonitorRedisEventStream, "-", "+", 1).Result()
		if err != nil {
			return watermark, err
		}
		if len(first) > 0 {
			sequence, err := channelMonitorRedisEventSequenceFromStreamID(first[0].ID)
			if err != nil {
				return watermark, err
			}
			if sequence > uint64(watermark) {
				// The persisted boundary has left the retained stream. A missing
				// interval cannot be reconstructed from its remaining tail.
				fields["meta:coverage_partial"] = "1"
			}
		}
	}
	end := last[0].ID
	start := "-"
	if watermark > 0 {
		start = fmt.Sprintf("(%d-%d", uint64(watermark)>>channelMonitorRedisStreamSequenceBits, uint64(watermark)&channelMonitorRedisStreamSequenceLimit)
	}
	seen := make(map[string]bool)
	for {
		messages, err := client.XRangeN(ctx, ChannelMonitorRedisEventStream, start, end, 256).Result()
		if err != nil {
			return watermark, err
		}
		if len(messages) == 0 {
			return watermark, nil
		}
		for _, message := range messages {
			sequence, err := channelMonitorRedisEventSequenceFromStreamID(message.ID)
			if err != nil {
				return watermark, err
			}
			watermark = max(watermark, int64(sequence))
			var event model.ChannelMonitorEvent
			payload := channelMonitorRedisSharedRedisValueString(message.Values[ChannelMonitorRedisEventFieldPayload])
			if err := common.UnmarshalJsonStr(payload, &event); err != nil {
				fields["meta:coverage_partial"] = "1"
				continue
			}
			if err := event.Validate(); err != nil {
				fields["meta:coverage_partial"] = "1"
				continue
			}
			if seen[event.EventId] {
				continue
			}
			seen[event.EventId] = true
			event.EventSequence = sequence
			if event.Source != model.ChannelMonitorEventSourceBusiness || model.ChannelDailyCostDayStart(event.OccurredAt) != day {
				continue
			}
			if legacyThrough > 0 && event.OccurredAt < legacyThrough {
				continue
			}
			first, err := client.Get(ctx, ChannelMonitorRedisSharedEventKey(event.EventId)).Uint64()
			if err != nil && !errors.Is(err, redis.Nil) {
				return watermark, err
			}
			base, _ := strconv.ParseUint(fields["meta:event_watermark"], 10, 64)
			if first > 0 && first <= base {
				continue
			}
			delta, ok := channelMonitorRedisSharedEventDeltaFromEvent(event)
			if !ok {
				continue
			}
			for _, scope := range channelMonitorRedisSuccessDayScopes(event) {
				for metric, amount := range delta.Integers {
					field := scope + ":" + metric
					previous, err := strconv.ParseInt(fields[field], 10, 64)
					if fields[field] == "" {
						previous, err = 0, nil
					}
					if err != nil {
						return watermark, err
					}
					next, err := channelMonitorRedisSharedCheckedAddInt64(previous, amount)
					if err != nil || next < 0 {
						return watermark, errors.New("渠道监控日汇总回放计数无效")
					}
					fields[field] = strconv.FormatInt(next, 10)
				}
				for metric, amount := range delta.Floats {
					field := scope + ":" + metric
					previous, err := strconv.ParseFloat(fields[field], 64)
					if fields[field] == "" {
						previous, err = 0, nil
					}
					if err != nil {
						return watermark, err
					}
					next := previous + amount
					if math.IsNaN(next) || math.IsInf(next, 0) || next < 0 {
						return watermark, errors.New("渠道监控日汇总回放性能数值无效")
					}
					fields[field] = strconv.FormatFloat(next, 'f', -1, 64)
				}
				if event.APIKeyName != "" && isChannelMonitorRedisDailyAPIKeyScope(scope) {
					fields[scope+":"+channelMonitorRedisSharedMetricAPIKeyName] = event.APIKeyName
				}
			}
			cutoff, _ := strconv.ParseInt(fields["meta:data_cutoff_at"], 10, 64)
			fields["meta:data_cutoff_at"] = strconv.FormatInt(max(cutoff, event.OccurredAt), 10)
		}
		start = "(" + messages[len(messages)-1].ID
		if len(messages) < 256 {
			return watermark, nil
		}
	}
}

// Refuse a checkpoint with unapplied holes, even if later channel partitions
// have already published higher sequence numbers to the day hash.
func channelMonitorDailyCheckpointReady(ctx context.Context, client *redis.Client, watermark uint64) error {
	pending, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: ChannelMonitorRedisEventStream, Group: ChannelMonitorRedisConsumerGroup, Start: "-", End: "+", Count: 1001,
	}).Result()
	if err != nil && strings.Contains(err.Error(), "NOGROUP") {
		return nil
	}
	if err != nil {
		return err
	}
	if len(pending) > 1000 {
		return errors.New("渠道监控消费积压，稍后保存日汇总检查点")
	}
	for _, item := range pending {
		sequence, err := channelMonitorRedisEventSequenceFromStreamID(item.ID)
		if err != nil {
			return err
		}
		if sequence > watermark {
			continue
		}
		messages, err := client.XRangeN(ctx, ChannelMonitorRedisEventStream, item.ID, item.ID, 1).Result()
		if err != nil {
			return err
		}
		if len(messages) != 1 {
			return errors.New("渠道监控待处理事件缺失，日汇总检查点未推进")
		}
		id := channelMonitorRedisSharedRedisValueString(messages[0].Values[ChannelMonitorRedisEventFieldEventID])
		exists, err := client.Exists(ctx, ChannelMonitorRedisSharedEventKey(id)).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return errors.New("渠道监控仍有未聚合事件，稍后保存日汇总检查点")
		}
	}
	return nil
}

func persistChannelMonitorDailyMetrics(ctx context.Context, client *redis.Client, day int64) error {
	key := ChannelMonitorRedisSuccessDayKey(day)
	before, err := client.HGet(ctx, key, "meta:revision").Int64()
	if err != nil {
		return err
	}
	daily, err := queryChannelMonitorRedisDailySuccessWithClient(ctx, client, day, []string{"fact:*", "meta:*"})
	if err != nil {
		return err
	}
	if err := channelMonitorDailyCheckpointReady(ctx, client, daily.EventWatermark); err != nil {
		return err
	}
	after, err := client.HGet(ctx, key, "meta:revision").Int64()
	if err != nil {
		return err
	}
	if before != after {
		return redis.TxFailedErr
	}
	rows := make([]model.ChannelMonitorDailySuccessLedger, 0, len(daily.Entries))
	for _, entry := range daily.Entries {
		if entry.Scope != "fact" {
			continue
		}
		identity, err := channelMonitorDailyMetricIdentity(entry.Identity)
		if err != nil {
			return err
		}
		row := identity.LedgerRow(day)
		aggregate := entry.Aggregate
		row.APIKeyName = aggregate.APIKeyName
		row.ActualSuccessCount, row.ActualFailureCount = aggregate.ActualSuccessCount, aggregate.ActualFailureCount
		row.FinalSuccessCount, row.FinalFailureCount = aggregate.FinalSuccessCount, aggregate.FinalFailureCount
		row.CacheHitCount, row.CacheSampleCount = aggregate.CacheHitCount, aggregate.CacheSampleCount
		row.CacheReadTokens, row.InputTokens = aggregate.CacheReadTokens, aggregate.InputTokens
		row.CacheWriteCount = aggregate.CacheWriteRequestCount
		encoded, err := common.Marshal(aggregate)
		if err != nil {
			return err
		}
		row.AggregateJSON = string(encoded)
		rows = append(rows, row)
	}
	if err := model.PersistChannelMonitorDailySnapshot(ctx, model.ChannelMonitorDailyCheckpoint{
		DayStart: day, Revision: after, EventWatermark: int64(daily.EventWatermark),
		DataCutoffAt: daily.DataCutoffAt, ProcessedAt: daily.ProcessedAt,
		CoveragePartial: daily.CoveragePartial,
	}, rows); err != nil {
		return err
	}
	_, err = client.Eval(ctx, `
		if redis.call("HGET", KEYS[1], "meta:revision") == ARGV[1] then
			return redis.call("SREM", KEYS[2], ARGV[2])
		end
		return 0`, []string{key, channelMonitorDailyDirtyDaysKey}, strconv.FormatInt(after, 10), strconv.FormatInt(day, 10)).Result()
	return err
}

func runChannelMonitorDailyPersistence(ctx context.Context, now int64) error {
	client := common.RedisMonitorConsumerClient()
	if client == nil {
		return ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	today := model.ChannelDailyCostDayStart(now)
	for _, day := range []int64{today, today - 24*60*60} {
		if err := ensureChannelMonitorDailyMetrics(ctx, client, day); err != nil {
			return err
		}
	}
	days, err := client.SMembers(ctx, channelMonitorDailyDirtyDaysKey).Result()
	if err != nil {
		return err
	}
	var failures []error
	for _, raw := range days {
		day, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		opCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = ensureChannelMonitorDailyMetrics(opCtx, client, day)
		if err == nil {
			err = persistChannelMonitorDailyMetrics(opCtx, client, day)
		}
		cancel()
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
