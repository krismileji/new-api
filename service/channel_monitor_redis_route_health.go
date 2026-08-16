package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
)

const (
	channelMonitorRedisRouteHealthVersion      = 1
	channelMonitorRedisRouteHealthRetention    = time.Hour
	channelMonitorRedisRouteHealthSampleLimit  = 1000
	channelMonitorRedisRouteHealthWriteRetries = 128
	channelMonitorRedisRouteHealthStateTTL     = channelMonitorRedisRouteHealthRetention
	channelMonitorRedisRouteHealthIndexKey     = ChannelMonitorRedisRouteProjectionPrefix + "health:index"
)

var ErrChannelMonitorRedisRouteHealthUnavailable = errors.New("渠道监控 Redis 路由健康窗口不可用")

// ChannelMonitorRedisRouteHealthSample is the compact scheduling subset of a
// channel monitor event. Raw request, token, cost and extension fields are
// deliberately excluded from the shared Redis window.
type ChannelMonitorRedisRouteHealthSample struct {
	EventID                   string                           `json:"event_id"`
	EventSequence             uint64                           `json:"event_sequence"`
	OccurredAt                int64                            `json:"occurred_at"`
	GroupName                 string                           `json:"group,omitempty"`
	Source                    model.ChannelMonitorEventSource  `json:"source"`
	Outcome                   model.ChannelMonitorEventOutcome `json:"outcome"`
	IsRetryAttempt            bool                             `json:"is_retry_attempt"`
	IsFinalAttempt            bool                             `json:"is_final_attempt"`
	FinalRetrySummary         bool                             `json:"final_retry_summary"`
	RequestDispatched         bool                             `json:"request_dispatched"`
	SchedulingEligible        bool                             `json:"scheduling_eligible"`
	RuntimeProtectionEligible bool                             `json:"runtime_protection_eligible"`
	StatusCode                *int                             `json:"status_code,omitempty"`
	ErrorCode                 string                           `json:"error_code,omitempty"`
	ErrorMessage              string                           `json:"error_message,omitempty"`
	FirstTokenMs              *float64                         `json:"first_token_ms,omitempty"`
	TPS                       *float64                         `json:"tps,omitempty"`
	AttemptDurationMs         *int64                           `json:"attempt_duration_ms,omitempty"`
}

// ChannelMonitorRedisRouteHealthSnapshot is the shared, query-ready summary
// used by scheduling. It intentionally mirrors the scheduling-relevant part
// of the local realtime snapshot without carrying complete events.
type ChannelMonitorRedisRouteHealthSnapshot struct {
	ChannelID             int                                       `json:"channel_id"`
	ModelName             string                                    `json:"model"`
	EventCount            int64                                     `json:"event_count"`
	BusinessRequestCount  int64                                     `json:"business_request_count"`
	ActualSuccessCount    int64                                     `json:"actual_success_count"`
	ActualFailureCount    int64                                     `json:"actual_failure_count"`
	ActualSampleCount     int64                                     `json:"actual_sample_count"`
	ActualSuccessRate     float64                                   `json:"actual_success_rate"`
	FinalSuccessCount     int64                                     `json:"final_success_count"`
	FinalFailureCount     int64                                     `json:"final_failure_count"`
	FinalSampleCount      int64                                     `json:"final_sample_count"`
	FinalSuccessRate      float64                                   `json:"final_success_rate"`
	FirstTokenSampleCount int64                                     `json:"first_token_sample_count"`
	FirstTokenTotalMs     float64                                   `json:"first_token_total_ms"`
	AverageFirstTokenMs   *float64                                  `json:"average_first_token_ms"`
	TPSSampleCount        int64                                     `json:"tps_sample_count"`
	TPSTotal              float64                                   `json:"tps_total"`
	AverageTPS            *float64                                  `json:"average_tps"`
	SourceCounts          map[model.ChannelMonitorEventSource]int64 `json:"source_counts"`
	WindowStart           int64                                     `json:"window_start"`
	WindowEnd             int64                                     `json:"window_end"`
	CoverageStart         int64                                     `json:"coverage_start"`
	DataCutoffAt          int64                                     `json:"data_cutoff_at"`
	ProcessedAt           int64                                     `json:"processed_at"`
	EventWatermark        uint64                                    `json:"event_watermark"`
}

type ChannelMonitorRedisRouteHealthWindow struct {
	Snapshot ChannelMonitorRedisRouteHealthSnapshot `json:"snapshot"`
	Samples  []ChannelMonitorRedisRouteHealthSample `json:"samples"`
}

type channelMonitorRedisRouteHealthState struct {
	Version   int                                  `json:"version"`
	ChannelID int                                  `json:"channel_id"`
	ModelName string                               `json:"model"`
	Window    ChannelMonitorRedisRouteHealthWindow `json:"window"`
}

type channelMonitorRedisRouteHealthRouteKey struct {
	channelID int
	modelName string
}

type ChannelMonitorRedisRouteHealthProjection struct {
	client   *redis.Client
	now      func(context.Context) (time.Time, error)
	maxRetry int
}

var _ ChannelMonitorRedisEventHandler = (*ChannelMonitorRedisRouteHealthProjection)(nil)

// ChannelMonitorRedisRouteHealthWindowKey returns the stable per-route key.
// There is no global route cardinality cap; the index is a bounded sorted set
// whose scores track the per-route window expiry time.
func ChannelMonitorRedisRouteHealthWindowKey(channelID int, modelName string) string {
	identity := strconv.Itoa(channelID) + ":" + ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	return ChannelMonitorRedisProjectionKeyForRoute(identity)
}

func ChannelMonitorRedisRouteHealthIndexKey() string {
	return channelMonitorRedisRouteHealthIndexKey
}

func ChannelMonitorRedisRouteHealthCoverageStart() int64 {
	return time.Now().Add(-channelMonitorRedisRouteHealthRetention).Unix()
}

func NewChannelMonitorRedisRouteHealthProjection() (*ChannelMonitorRedisRouteHealthProjection, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return nil, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	return newChannelMonitorRedisRouteHealthProjection(common.RDB, nil, 0)
}

func NewChannelMonitorRedisRouteHealthProjectionForClient(client *redis.Client) (*ChannelMonitorRedisRouteHealthProjection, error) {
	return newChannelMonitorRedisRouteHealthProjection(client, nil, 0)
}

func GetChannelMonitorRedisRouteHealthWindow(
	ctx context.Context,
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthWindow, bool, error) {
	projection, err := NewChannelMonitorRedisRouteHealthProjection()
	if err != nil {
		return ChannelMonitorRedisRouteHealthWindow{}, false, err
	}
	return projection.GetRouteHealthWindow(ctx, channelID, modelName)
}

func newChannelMonitorRedisRouteHealthProjection(
	client *redis.Client,
	now func(context.Context) (time.Time, error),
	maxRetry int,
) (*ChannelMonitorRedisRouteHealthProjection, error) {
	if client == nil {
		return nil, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	if maxRetry <= 0 {
		maxRetry = channelMonitorRedisRouteHealthWriteRetries
	}
	projection := &ChannelMonitorRedisRouteHealthProjection{client: client, maxRetry: maxRetry}
	if now == nil {
		projection.now = func(ctx context.Context) (time.Time, error) {
			return client.Time(ctx).Result()
		}
	} else {
		projection.now = now
	}
	return projection, nil
}

// HandleChannelMonitorEvents implements the REDIS-03 single logical
// aggregator handler. Each route is committed with WATCH/MULTI so concurrent
// writers converge on the same deduplicated, ordered window.
func (projection *ChannelMonitorRedisRouteHealthProjection) HandleChannelMonitorEvents(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	if projection == nil || projection.client == nil {
		return ErrChannelMonitorRedisRouteHealthUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(events) == 0 {
		return nil
	}

	grouped := make(map[channelMonitorRedisRouteHealthRouteKey][]ChannelMonitorRedisRouteHealthSample)
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
		modelName := ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		if !event.SchedulingEligible || event.ChannelId <= 0 || modelName == "" {
			continue
		}
		event.ModelName = modelName
		key := channelMonitorRedisRouteHealthRouteKey{channelID: event.ChannelId, modelName: modelName}
		grouped[key] = append(grouped[key], channelMonitorRedisRouteHealthSampleFromEvent(event))
	}
	if len(grouped) == 0 {
		return nil
	}
	now, err := projection.now(ctx)
	if err != nil {
		return err
	}

	keys := make([]channelMonitorRedisRouteHealthRouteKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].modelName != keys[j].modelName {
			return keys[i].modelName < keys[j].modelName
		}
		return keys[i].channelID < keys[j].channelID
	})
	for _, key := range keys {
		if err := projection.updateRoute(ctx, key, grouped[key], now); err != nil {
			return err
		}
	}
	return nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) Write(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	return projection.HandleChannelMonitorEvents(ctx, events)
}

func (projection *ChannelMonitorRedisRouteHealthProjection) updateRoute(
	ctx context.Context,
	key channelMonitorRedisRouteHealthRouteKey,
	incoming []ChannelMonitorRedisRouteHealthSample,
	now time.Time,
) error {
	redisKey := ChannelMonitorRedisRouteHealthWindowKey(key.channelID, key.modelName)
	cutoff := now.Unix() - int64(channelMonitorRedisRouteHealthRetention/time.Second)
	indexExpiryCutoff := strconv.FormatInt(now.UnixMilli(), 10)
	indexExpiresAt := now.Add(channelMonitorRedisRouteHealthStateTTL).UnixMilli()
	for attempt := 0; attempt < projection.maxRetry; attempt++ {
		err := projection.client.Watch(ctx, func(tx *redis.Tx) error {
			stored, err := tx.Get(ctx, redisKey).Bytes()
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			state := channelMonitorRedisRouteHealthState{
				Version:   channelMonitorRedisRouteHealthVersion,
				ChannelID: key.channelID,
				ModelName: key.modelName,
			}
			if len(stored) > 0 {
				if err := common.Unmarshal(stored, &state); err != nil {
					return fmt.Errorf("解析 Redis 路由健康窗口失败: %w", err)
				}
				if state.Version != channelMonitorRedisRouteHealthVersion ||
					state.ChannelID != key.channelID || state.ModelName != key.modelName {
					return errors.New("Redis 路由健康窗口版本或路由标识无效")
				}
			}

			byEventID := make(map[string]ChannelMonitorRedisRouteHealthSample, len(state.Window.Samples)+len(incoming))
			for _, sample := range state.Window.Samples {
				byEventID[sample.EventID] = sample
			}
			for _, sample := range incoming {
				if existing, exists := byEventID[sample.EventID]; !exists || channelMonitorRedisRouteHealthSampleCanonicalLess(sample, existing) {
					byEventID[sample.EventID] = sample
				}
			}
			samples := make([]ChannelMonitorRedisRouteHealthSample, 0, len(byEventID))
			for _, sample := range byEventID {
				if sample.OccurredAt >= cutoff {
					samples = append(samples, sample)
				}
			}
			sortChannelMonitorRedisRouteHealthSamples(samples)
			if len(samples) > channelMonitorRedisRouteHealthSampleLimit {
				samples = samples[len(samples)-channelMonitorRedisRouteHealthSampleLimit:]
			}
			if len(samples) == 0 {
				_, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.Del(ctx, redisKey)
					pipe.ZRemRangeByScore(ctx, channelMonitorRedisRouteHealthIndexKey, "-inf", indexExpiryCutoff)
					pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, redisKey)
					return nil
				})
				return err
			}
			state.Window.Samples = samples
			state.Window.Snapshot = buildChannelMonitorRedisRouteHealthSnapshot(
				key.channelID, key.modelName, samples, cutoff, now.Unix(),
			)
			payload, err := common.Marshal(state)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, redisKey, payload, channelMonitorRedisRouteHealthStateTTL)
				pipe.ZRemRangeByScore(ctx, channelMonitorRedisRouteHealthIndexKey, "-inf", indexExpiryCutoff)
				pipe.ZAdd(ctx, channelMonitorRedisRouteHealthIndexKey, &redis.Z{
					Score:  float64(indexExpiresAt),
					Member: redisKey,
				})
				pipe.Expire(ctx, channelMonitorRedisRouteHealthIndexKey, channelMonitorRedisRouteHealthStateTTL)
				return nil
			})
			return err
		}, redisKey)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return fmt.Errorf("Redis 路由健康窗口并发更新重试次数耗尽: %s", redisKey)
}

func (projection *ChannelMonitorRedisRouteHealthProjection) GetRouteHealthWindow(
	ctx context.Context,
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthWindow, bool, error) {
	if projection == nil || projection.client == nil {
		return ChannelMonitorRedisRouteHealthWindow{}, false, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now, err := projection.now(ctx)
	if err != nil {
		return ChannelMonitorRedisRouteHealthWindow{}, false, err
	}
	redisKey := ChannelMonitorRedisRouteHealthWindowKey(channelID, modelName)
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	for attempt := 0; attempt < projection.maxRetry; attempt++ {
		payload, err := projection.client.Get(ctx, redisKey).Bytes()
		if errors.Is(err, redis.Nil) {
			if removeErr := projection.removeMissingRouteHealthIndexEntry(ctx, redisKey); removeErr != nil {
				return ChannelMonitorRedisRouteHealthWindow{}, false, removeErr
			}
			return ChannelMonitorRedisRouteHealthWindow{}, false, nil
		}
		if err != nil {
			return ChannelMonitorRedisRouteHealthWindow{}, false, err
		}
		var state channelMonitorRedisRouteHealthState
		if err := common.Unmarshal(payload, &state); err != nil {
			return ChannelMonitorRedisRouteHealthWindow{}, false, fmt.Errorf("解析 Redis 路由健康窗口失败: %w", err)
		}
		if state.Version != channelMonitorRedisRouteHealthVersion || state.ChannelID != channelID {
			return ChannelMonitorRedisRouteHealthWindow{}, false, errors.New("Redis 路由健康窗口版本或路由标识无效")
		}
		if state.ModelName != modelName {
			return ChannelMonitorRedisRouteHealthWindow{}, false, errors.New("Redis 路由健康窗口模型标识不一致")
		}
		cutoff := now.Unix() - int64(channelMonitorRedisRouteHealthRetention/time.Second)
		samples := make([]ChannelMonitorRedisRouteHealthSample, 0, len(state.Window.Samples))
		for _, sample := range state.Window.Samples {
			if sample.OccurredAt >= cutoff {
				samples = append(samples, sample)
			}
		}
		sortChannelMonitorRedisRouteHealthSamples(samples)
		if len(samples) > channelMonitorRedisRouteHealthSampleLimit {
			samples = samples[len(samples)-channelMonitorRedisRouteHealthSampleLimit:]
		}
		if len(samples) > 0 {
			return ChannelMonitorRedisRouteHealthWindow{
				Samples:  samples,
				Snapshot: buildChannelMonitorRedisRouteHealthSnapshot(channelID, modelName, samples, cutoff, now.Unix()),
			}, true, nil
		}
		deleted := false
		err = projection.client.Watch(ctx, func(tx *redis.Tx) error {
			currentPayload, getErr := tx.Get(ctx, redisKey).Bytes()
			if errors.Is(getErr, redis.Nil) {
				_, getErr = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, redisKey)
					return nil
				})
				return getErr
			}
			if getErr != nil {
				return getErr
			}
			if string(currentPayload) != string(payload) {
				return redis.TxFailedErr
			}
			_, getErr = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, redisKey)
				pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, redisKey)
				return nil
			})
			if getErr == nil {
				deleted = true
			}
			return getErr
		}, redisKey)
		if err == nil && deleted {
			return ChannelMonitorRedisRouteHealthWindow{}, false, nil
		}
		// A concurrent writer can make WATCH return without an explicit error
		// in some go-redis versions. Re-read instead of reporting a transient
		// empty window.
		if err == nil {
			continue
		}
		if !errors.Is(err, redis.TxFailedErr) {
			return ChannelMonitorRedisRouteHealthWindow{}, false, err
		}
	}
	return ChannelMonitorRedisRouteHealthWindow{}, false, fmt.Errorf("Redis 路由健康窗口并发清理重试次数耗尽: %s", redisKey)
}

func (projection *ChannelMonitorRedisRouteHealthProjection) GetSnapshot(
	ctx context.Context,
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthSnapshot, bool, error) {
	window, available, err := projection.GetRouteHealthWindow(ctx, channelID, modelName)
	return window.Snapshot, available, err
}

func (projection *ChannelMonitorRedisRouteHealthProjection) ListRouteHealthWindows(
	ctx context.Context,
) ([]ChannelMonitorRedisRouteHealthWindow, error) {
	if projection == nil || projection.client == nil {
		return nil, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now, err := projection.now(ctx)
	if err != nil {
		return nil, err
	}
	var keysCommand *redis.StringSliceCmd
	_, err = projection.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowMilliseconds := strconv.FormatInt(now.UnixMilli(), 10)
		pipe.ZRemRangeByScore(ctx, channelMonitorRedisRouteHealthIndexKey, "-inf", nowMilliseconds)
		keysCommand = pipe.ZRangeByScore(ctx, channelMonitorRedisRouteHealthIndexKey, &redis.ZRangeBy{
			Min: "(" + nowMilliseconds,
			Max: "+inf",
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	keys, err := keysCommand.Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	result := make([]ChannelMonitorRedisRouteHealthWindow, 0, len(keys))
	for _, key := range keys {
		payload, getErr := projection.client.Get(ctx, key).Bytes()
		if errors.Is(getErr, redis.Nil) {
			if removeErr := projection.removeMissingRouteHealthIndexEntry(ctx, key); removeErr != nil {
				return nil, removeErr
			}
			continue
		}
		if getErr != nil {
			return nil, getErr
		}
		var state channelMonitorRedisRouteHealthState
		if err := common.Unmarshal(payload, &state); err != nil {
			return nil, err
		}
		window, available, getErr := projection.GetRouteHealthWindow(ctx, state.ChannelID, state.ModelName)
		if getErr != nil {
			return nil, getErr
		}
		if !available {
			continue
		}
		result = append(result, window)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Snapshot.ModelName != result[j].Snapshot.ModelName {
			return result[i].Snapshot.ModelName < result[j].Snapshot.ModelName
		}
		return result[i].Snapshot.ChannelID < result[j].Snapshot.ChannelID
	})
	return result, nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) removeMissingRouteHealthIndexEntry(
	ctx context.Context,
	redisKey string,
) error {
	for attempt := 0; attempt < projection.maxRetry; attempt++ {
		err := projection.client.Watch(ctx, func(tx *redis.Tx) error {
			exists, err := tx.Exists(ctx, redisKey).Result()
			if err != nil || exists > 0 {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, redisKey)
				return nil
			})
			return err
		}, redisKey)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return fmt.Errorf("Redis 路由健康索引并发清理重试次数耗尽: %s", redisKey)
}

func channelMonitorRedisRouteHealthSampleFromEvent(event model.ChannelMonitorEvent) ChannelMonitorRedisRouteHealthSample {
	errorMessage := event.ErrorMessage
	if messageRunes := []rune(errorMessage); len(messageRunes) > 256 {
		errorMessage = string(messageRunes[:256])
	}
	sample := ChannelMonitorRedisRouteHealthSample{
		EventID: event.EventId, EventSequence: event.EventSequence, OccurredAt: event.OccurredAt,
		GroupName: event.GroupName,
		Source:    event.Source, Outcome: event.Outcome, IsRetryAttempt: event.IsRetryAttempt,
		IsFinalAttempt: event.IsFinalAttempt, FinalRetrySummary: event.FinalRetrySummary,
		RequestDispatched: event.RequestDispatched, SchedulingEligible: event.SchedulingEligible,
		RuntimeProtectionEligible: event.RuntimeProtectionEligible, ErrorCode: event.ErrorCode,
		ErrorMessage: errorMessage,
	}
	if event.StatusCode != nil {
		value := *event.StatusCode
		sample.StatusCode = &value
	}
	if event.FirstTokenMs != nil {
		value := *event.FirstTokenMs
		sample.FirstTokenMs = &value
	}
	if event.TPS != nil {
		value := *event.TPS
		sample.TPS = &value
	}
	if event.AttemptDurationMs != nil {
		value := *event.AttemptDurationMs
		sample.AttemptDurationMs = &value
	}
	return sample
}

func channelMonitorRedisRouteHealthSampleCanonicalLess(
	candidate ChannelMonitorRedisRouteHealthSample,
	current ChannelMonitorRedisRouteHealthSample,
) bool {
	if candidate.OccurredAt != current.OccurredAt {
		return candidate.OccurredAt < current.OccurredAt
	}
	if candidate.EventSequence != current.EventSequence {
		return candidate.EventSequence < current.EventSequence
	}
	if candidate.EventID != current.EventID {
		return candidate.EventID < current.EventID
	}
	// Duplicate IDs should normally carry identical data. A serialized tie
	// breaker keeps malformed duplicates convergent across concurrent writers.
	candidatePayload, _ := common.Marshal(candidate)
	currentPayload, _ := common.Marshal(current)
	return string(candidatePayload) < string(currentPayload)
}

func sortChannelMonitorRedisRouteHealthSamples(samples []ChannelMonitorRedisRouteHealthSample) {
	sort.SliceStable(samples, func(i, j int) bool {
		left, right := samples[i], samples[j]
		if left.OccurredAt != right.OccurredAt {
			return left.OccurredAt < right.OccurredAt
		}
		if left.EventSequence != right.EventSequence {
			return left.EventSequence < right.EventSequence
		}
		return left.EventID < right.EventID
	})
}

func buildChannelMonitorRedisRouteHealthSnapshot(
	channelID int,
	modelName string,
	samples []ChannelMonitorRedisRouteHealthSample,
	cutoff int64,
	processedAt int64,
) ChannelMonitorRedisRouteHealthSnapshot {
	snapshot := ChannelMonitorRedisRouteHealthSnapshot{
		ChannelID: channelID, ModelName: modelName, CoverageStart: cutoff,
		ProcessedAt: processedAt, SourceCounts: make(map[model.ChannelMonitorEventSource]int64),
	}
	for _, sample := range samples {
		snapshot.EventCount++
		snapshot.SourceCounts[sample.Source]++
		snapshot.EventWatermark = max(snapshot.EventWatermark, sample.EventSequence)
		if sample.Source != model.ChannelMonitorEventSourceBusiness || !sample.RequestDispatched {
			continue
		}
		if sample.FinalRetrySummary {
			if sample.Outcome == model.ChannelMonitorEventOutcomeFailure {
				snapshot.FinalFailureCount++
			}
			continue
		}
		switch sample.Outcome {
		case model.ChannelMonitorEventOutcomeSuccess:
			snapshot.ActualSuccessCount++
			if sample.IsFinalAttempt {
				snapshot.FinalSuccessCount++
			}
		case model.ChannelMonitorEventOutcomeFailure:
			snapshot.ActualFailureCount++
			if sample.IsFinalAttempt {
				snapshot.FinalFailureCount++
			}
		default:
			continue
		}
		snapshot.BusinessRequestCount++
		if sample.FirstTokenMs != nil {
			snapshot.FirstTokenSampleCount++
			snapshot.FirstTokenTotalMs = channelMonitorRealtimeAddFloat64(snapshot.FirstTokenTotalMs, *sample.FirstTokenMs)
		}
		if sample.TPS != nil {
			snapshot.TPSSampleCount++
			snapshot.TPSTotal = channelMonitorRealtimeAddFloat64(snapshot.TPSTotal, *sample.TPS)
		}
	}
	snapshot.ActualSampleCount = snapshot.ActualSuccessCount + snapshot.ActualFailureCount
	if snapshot.ActualSampleCount > 0 {
		snapshot.ActualSuccessRate = float64(snapshot.ActualSuccessCount) / float64(snapshot.ActualSampleCount)
	}
	snapshot.FinalSampleCount = snapshot.FinalSuccessCount + snapshot.FinalFailureCount
	if snapshot.FinalSampleCount > 0 {
		snapshot.FinalSuccessRate = float64(snapshot.FinalSuccessCount) / float64(snapshot.FinalSampleCount)
	}
	if snapshot.FirstTokenSampleCount > 0 {
		average := snapshot.FirstTokenTotalMs / float64(snapshot.FirstTokenSampleCount)
		snapshot.AverageFirstTokenMs = &average
	}
	if snapshot.TPSSampleCount > 0 {
		average := snapshot.TPSTotal / float64(snapshot.TPSSampleCount)
		snapshot.AverageTPS = &average
	}
	if len(samples) > 0 {
		snapshot.WindowStart = samples[0].OccurredAt
		snapshot.WindowEnd = samples[len(samples)-1].OccurredAt
		snapshot.DataCutoffAt = snapshot.WindowEnd
	}
	return snapshot
}
