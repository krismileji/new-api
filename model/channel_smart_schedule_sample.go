package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// ChannelSmartScheduleSamplesJSON stores the complete rolling sample buffer.
// MySQL TEXT is limited to 64 KiB, so its schema needs LONGTEXT while SQLite
// and PostgreSQL can use TEXT.
type ChannelSmartScheduleSamplesJSON string

func (ChannelSmartScheduleSamplesJSON) GormDataType() string {
	return "text"
}

func (ChannelSmartScheduleSamplesJSON) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}

type ChannelSmartScheduleModelSampleState struct {
	Id        int64  `json:"id"`
	ChannelId int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_smart_schedule_model_sample"`
	ModelName string `json:"model" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_smart_schedule_model_sample"`

	WindowStart                int64                           `json:"window_start" gorm:"bigint"`
	ObservationSince           int64                           `json:"observation_since" gorm:"bigint;index"`
	RecoverySuccessCount       int                             `json:"recovery_success_count"`
	RecoverySuccessAt          int64                           `json:"recovery_success_at" gorm:"bigint;index"`
	LastTime                   int64                           `json:"last_time" gorm:"bigint;index"`
	LastSuccess                bool                            `json:"last_success"`
	LastError                  string                          `json:"last_error" gorm:"type:varchar(255)"`
	SampleCount                int64                           `json:"sample_count" gorm:"bigint"`
	SuccessCount               int64                           `json:"success_count" gorm:"bigint"`
	FailureDurationSampleCount int64                           `json:"failure_duration_sample_count" gorm:"bigint"`
	AverageFailureDurationMs   *float64                        `json:"average_failure_duration_ms"`
	FirstTokenSampleCount      int64                           `json:"first_token_sample_count" gorm:"bigint"`
	AverageFirstTokenMs        *float64                        `json:"average_first_token_ms"`
	TPSSampleCount             int64                           `json:"tps_sample_count" gorm:"bigint"`
	AverageTPS                 *float64                        `json:"average_tps"`
	SamplesJSON                ChannelSmartScheduleSamplesJSON `json:"-"`
}

type ChannelSmartScheduleModelSampleResult struct {
	ChannelId     int
	Model         string
	Source        string
	SampleId      string
	WindowStart   int64
	Time          int64
	Success       bool
	Error         string
	DurationMs    *float64
	FirstTokenMs  *float64
	TPS           *float64
	ProbeRecovery *ChannelSmartScheduleProbeRecoveryRequest
}

type ChannelSmartScheduleSampleMetrics struct {
	WindowStart                   int64
	LastTime                      int64
	LastSuccess                   bool
	SampleCount                   int64
	SuccessCount                  int64
	FailureCount                  int64
	FailureDurationSampleCount    int64
	FailureDurationTotalMs        float64
	FailureDurationBuckets        []ChannelMonitorFailureDurationBucket
	FirstTokenSampleCount         int64
	AverageFirstTokenMs           *float64
	FirstTokenP50Ms               *float64
	FirstTokenP95Ms               *float64
	WinsorizedAverageFirstTokenMs *float64
	FirstTokenDurationBuckets     []ChannelMonitorDurationBucket
	TPSSampleCount                int64
	AverageTPS                    *float64
}

// ChannelSmartScheduleAdaptiveHealthMetric contains request-level signals for
// the adaptive sampling window. Production request logs and scheduled/manual
// samples use the same counters so their ratios are directly comparable.
type ChannelSmartScheduleAdaptiveHealthMetric struct {
	RequestCount                int64
	FailureCount                int64
	SlowRequestCount            int64
	HealthyRequestCount         int64
	FirstTokenCount             int64
	FirstTokenTotalMs           float64
	TPSSampleCount              int64
	TPSTotal                    float64
	TPSOutputTokens             int64
	TPSGenerationDurationMs     int64
	LatencyPressure             float64
	LastUsedTime                int64
	StabilitySuccessCount       int64
	StabilityFailureCount       int64
	StabilityFinalFailureCount  int64
	StabilityRetryFailureCount  int64
	RetryFailureDurationTotalMs float64
	RetryFailureDurationBuckets []ChannelMonitorFailureDurationBucket
	FirstTokenDurationBuckets   []ChannelMonitorDurationBucket
}

// ChannelSmartScheduleSampleSeries is one parsed channel/model rolling sample
// buffer. Callers can reuse it across the performance, stability, and probing
// windows without repeatedly decoding SamplesJSON.
type ChannelSmartScheduleSampleSeries struct {
	observationSince int64
	samples          []channelSmartScheduleSample
}

type channelSmartScheduleSample struct {
	Time              int64    `json:"time"`
	Success           bool     `json:"success"`
	Source            string   `json:"source,omitempty"`
	SampleId          string   `json:"sample_id,omitempty"`
	FailureDurationMs *float64 `json:"failure_duration_ms,omitempty"`
	FirstTokenMs      *float64 `json:"first_token_ms,omitempty"`
	TPS               *float64 `json:"tps,omitempty"`
}

type channelSmartScheduleModelKey struct {
	channelId int
	modelName string
}

const (
	channelSmartScheduleMaxSamples                 = 1500
	ChannelSmartScheduleSampleScopeChannelModel    = "channel_model"
	ChannelSmartScheduleSampleSourceScheduledProbe = "scheduled_probe"
	ChannelSmartScheduleSampleSourceManualTest     = "manual_test"
	ChannelSmartScheduleSampleSourceStatusProbe    = "status_probe"
)

func (state ChannelSmartScheduleModelSampleState) MetricsSince(windowStart int64) ChannelSmartScheduleSampleMetrics {
	series, err := state.SampleSeries()
	if err != nil {
		common.SysError(err.Error())
		return ChannelSmartScheduleSampleMetrics{}
	}
	return series.MetricsSince(windowStart)
}

func (state ChannelSmartScheduleModelSampleState) ManualTestMetricsSince(windowStart int64) ChannelSmartScheduleSampleMetrics {
	series, err := state.SampleSeries()
	if err != nil {
		common.SysError(err.Error())
		return ChannelSmartScheduleSampleMetrics{}
	}
	return series.ManualTestMetricsSince(windowStart)
}

func (state ChannelSmartScheduleModelSampleState) SampleSeries() (ChannelSmartScheduleSampleSeries, error) {
	series := ChannelSmartScheduleSampleSeries{observationSince: state.ObservationSince}
	if strings.TrimSpace(string(state.SamplesJSON)) == "" {
		return series, nil
	}
	if err := common.UnmarshalJsonStr(string(state.SamplesJSON), &series.samples); err != nil {
		return ChannelSmartScheduleSampleSeries{}, fmt.Errorf(
			"解析渠道 %d 模型 %s 的智能调度共享样本失败: %w",
			state.ChannelId,
			state.ModelName,
			err,
		)
	}
	return series, nil
}

func (series ChannelSmartScheduleSampleSeries) MetricsSince(windowStart int64) ChannelSmartScheduleSampleMetrics {
	if series.observationSince > windowStart {
		windowStart = series.observationSince
	}
	return channelSmartScheduleCalculateSampleMetrics(series.samples, windowStart)
}

func (series ChannelSmartScheduleSampleSeries) AdaptiveHealthMetricsSince(
	windowStart int64,
	warningSeconds float64,
	criticalSeconds float64,
) ChannelSmartScheduleAdaptiveHealthMetric {
	return series.AdaptiveHealthMetricsSinceWithMaxRequests(
		windowStart, -1, warningSeconds, criticalSeconds,
	)
}

// AdaptiveHealthMetricsSinceWithMaxRequests evaluates the newest valid
// requests in a time window. A negative limit keeps the historical unlimited
// behavior, while zero means that the caller has no remaining request budget.
func (series ChannelSmartScheduleSampleSeries) AdaptiveHealthMetricsSinceWithMaxRequests(
	windowStart int64,
	maxRequests int,
	warningSeconds float64,
	criticalSeconds float64,
) ChannelSmartScheduleAdaptiveHealthMetric {
	if maxRequests == 0 {
		return ChannelSmartScheduleAdaptiveHealthMetric{}
	}
	if series.observationSince > windowStart {
		windowStart = series.observationSince
	}
	metric := ChannelSmartScheduleAdaptiveHealthMetric{}
	firstTokenBuckets := make(map[int]ChannelMonitorDurationBucket)
	failureBucketCounts := [6]int64{}
	samples := make([]channelSmartScheduleSample, 0, len(series.samples))
	for _, sample := range series.samples {
		if sample.Time >= windowStart {
			samples = append(samples, sample)
		}
	}
	if maxRequests > 0 && len(samples) > maxRequests {
		sort.SliceStable(samples, func(i, j int) bool {
			return samples[i].Time > samples[j].Time
		})
		samples = samples[:maxRequests]
	}
	for _, sample := range samples {
		metric.RequestCount++
		metric.LastUsedTime = max(metric.LastUsedTime, sample.Time)
		if !sample.Success {
			metric.FailureCount++
			metric.StabilityFailureCount++
			metric.StabilityRetryFailureCount++
			if sample.FailureDurationMs != nil && *sample.FailureDurationMs >= 0 &&
				!math.IsNaN(*sample.FailureDurationMs) && !math.IsInf(*sample.FailureDurationMs, 0) {
				metric.RetryFailureDurationTotalMs += *sample.FailureDurationMs
				switch {
				case *sample.FailureDurationMs < 1000:
					failureBucketCounts[0]++
				case *sample.FailureDurationMs < 3000:
					failureBucketCounts[1]++
				case *sample.FailureDurationMs < 10000:
					failureBucketCounts[2]++
				case *sample.FailureDurationMs < 30000:
					failureBucketCounts[3]++
				case *sample.FailureDurationMs < 60000:
					failureBucketCounts[4]++
				default:
					failureBucketCounts[5]++
				}
			}
			continue
		}
		metric.StabilitySuccessCount++
		metric.HealthyRequestCount++
		if sample.TPS != nil && *sample.TPS > 0 &&
			!math.IsNaN(*sample.TPS) && !math.IsInf(*sample.TPS, 0) {
			metric.TPSSampleCount++
			metric.TPSTotal += *sample.TPS
		}
		if sample.FirstTokenMs == nil || *sample.FirstTokenMs <= 0 ||
			math.IsNaN(*sample.FirstTokenMs) || math.IsInf(*sample.FirstTokenMs, 0) {
			continue
		}
		metric.FirstTokenCount++
		metric.FirstTokenTotalMs += *sample.FirstTokenMs
		bucketIndex := channelMonitorDurationBucketIndex(*sample.FirstTokenMs)
		bucket := firstTokenBuckets[bucketIndex]
		bucket.Count++
		bucket.TotalMs += *sample.FirstTokenMs
		firstTokenBuckets[bucketIndex] = bucket
		latencyPressure := channelSmartScheduleAdaptiveLatencyPressure(
			*sample.FirstTokenMs, warningSeconds, criticalSeconds,
		)
		metric.LatencyPressure += latencyPressure
		if *sample.FirstTokenMs >= warningSeconds*1000 {
			metric.SlowRequestCount++
			metric.HealthyRequestCount--
		}
	}
	metric.RetryFailureDurationBuckets = []ChannelMonitorFailureDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: failureBucketCounts[0]},
		{LowerBoundMs: 1000, UpperBoundMs: 3000, Count: failureBucketCounts[1]},
		{LowerBoundMs: 3000, UpperBoundMs: 10000, Count: failureBucketCounts[2]},
		{LowerBoundMs: 10000, UpperBoundMs: 30000, Count: failureBucketCounts[3]},
		{LowerBoundMs: 30000, UpperBoundMs: 60000, Count: failureBucketCounts[4]},
		{LowerBoundMs: 60000, UpperBoundMs: 0, Count: failureBucketCounts[5]},
	}
	metric.FirstTokenDurationBuckets = channelMonitorDurationBucketsFromAggregates(firstTokenBuckets)
	return metric
}

func channelSmartScheduleAdaptiveLatencyPressure(valueMs, warningSeconds, criticalSeconds float64) float64 {
	warningMs := warningSeconds * 1000
	criticalMs := criticalSeconds * 1000
	if math.IsNaN(valueMs) || math.IsInf(valueMs, 0) ||
		math.IsNaN(warningMs) || math.IsInf(warningMs, 0) ||
		math.IsNaN(criticalMs) || math.IsInf(criticalMs, 0) ||
		criticalMs <= warningMs || valueMs <= warningMs {
		return 0
	}
	if valueMs >= criticalMs {
		return 1
	}
	return (valueMs - warningMs) / (criticalMs - warningMs)
}

func (series ChannelSmartScheduleSampleSeries) ManualTestMetricsSince(windowStart int64) ChannelSmartScheduleSampleMetrics {
	if series.observationSince > windowStart {
		windowStart = series.observationSince
	}
	manualSamples := make([]channelSmartScheduleSample, 0)
	for _, sample := range series.samples {
		if sample.Source == ChannelSmartScheduleSampleSourceManualTest {
			manualSamples = append(manualSamples, sample)
		}
	}
	return channelSmartScheduleCalculateSampleMetrics(manualSamples, windowStart)
}

// Windowed returns the public sample-state snapshot for the requested
// scheduling window without mutating the persisted rolling sample buffer.
func (state ChannelSmartScheduleModelSampleState) Windowed(windowStart int64) ChannelSmartScheduleModelSampleState {
	metrics := state.MetricsSince(windowStart)
	return state.WindowedWithMetrics(metrics)
}

func (state ChannelSmartScheduleModelSampleState) WindowedWithMetrics(
	metrics ChannelSmartScheduleSampleMetrics,
) ChannelSmartScheduleModelSampleState {
	windowed := state
	windowed.WindowStart = metrics.WindowStart
	windowed.LastTime = metrics.LastTime
	windowed.LastSuccess = metrics.LastSuccess
	windowed.SampleCount = metrics.SampleCount
	windowed.SuccessCount = metrics.SuccessCount
	windowed.FailureDurationSampleCount = metrics.FailureDurationSampleCount
	windowed.AverageFailureDurationMs = nil
	if metrics.FailureDurationSampleCount > 0 {
		value := metrics.FailureDurationTotalMs / float64(metrics.FailureDurationSampleCount)
		windowed.AverageFailureDurationMs = &value
	}
	windowed.FirstTokenSampleCount = metrics.FirstTokenSampleCount
	windowed.AverageFirstTokenMs = metrics.AverageFirstTokenMs
	windowed.TPSSampleCount = metrics.TPSSampleCount
	windowed.AverageTPS = metrics.AverageTPS
	if metrics.LastTime == 0 || metrics.LastTime != state.LastTime || metrics.LastSuccess {
		windowed.LastError = ""
	}
	return windowed
}

func channelSmartScheduleCalculateSampleMetrics(
	samples []channelSmartScheduleSample,
	windowStart int64,
) ChannelSmartScheduleSampleMetrics {
	metrics := ChannelSmartScheduleSampleMetrics{}
	var firstTokenTotal float64
	var tpsTotal float64
	failureBucketCounts := [6]int64{}
	firstTokenBuckets := make(map[int]ChannelMonitorDurationBucket)
	for _, sample := range samples {
		if sample.Time < windowStart {
			continue
		}
		if metrics.WindowStart == 0 || sample.Time < metrics.WindowStart {
			metrics.WindowStart = sample.Time
		}
		if sample.Time >= metrics.LastTime {
			metrics.LastTime = sample.Time
			metrics.LastSuccess = sample.Success
		}
		metrics.SampleCount++
		if sample.Success {
			metrics.SuccessCount++
		} else {
			metrics.FailureCount++
			if sample.FailureDurationMs != nil && *sample.FailureDurationMs >= 0 &&
				!math.IsNaN(*sample.FailureDurationMs) && !math.IsInf(*sample.FailureDurationMs, 0) {
				metrics.FailureDurationSampleCount++
				metrics.FailureDurationTotalMs += *sample.FailureDurationMs
				durationMs := *sample.FailureDurationMs
				switch {
				case durationMs < 1000:
					failureBucketCounts[0]++
				case durationMs < 3000:
					failureBucketCounts[1]++
				case durationMs < 10000:
					failureBucketCounts[2]++
				case durationMs < 30000:
					failureBucketCounts[3]++
				case durationMs < 60000:
					failureBucketCounts[4]++
				default:
					failureBucketCounts[5]++
				}
			}
		}
		if sample.Success && sample.FirstTokenMs != nil && *sample.FirstTokenMs > 0 &&
			!math.IsNaN(*sample.FirstTokenMs) && !math.IsInf(*sample.FirstTokenMs, 0) {
			metrics.FirstTokenSampleCount++
			firstTokenTotal += *sample.FirstTokenMs
			bucketIndex := channelMonitorDurationBucketIndex(*sample.FirstTokenMs)
			bucket := firstTokenBuckets[bucketIndex]
			bucket.Count++
			bucket.TotalMs += *sample.FirstTokenMs
			firstTokenBuckets[bucketIndex] = bucket
		}
		if sample.Success && sample.TPS != nil && *sample.TPS > 0 &&
			!math.IsNaN(*sample.TPS) && !math.IsInf(*sample.TPS, 0) {
			metrics.TPSSampleCount++
			tpsTotal += *sample.TPS
		}
	}
	if metrics.FirstTokenSampleCount > 0 {
		value := firstTokenTotal / float64(metrics.FirstTokenSampleCount)
		metrics.AverageFirstTokenMs = &value
	}
	metrics.FirstTokenDurationBuckets = channelMonitorDurationBucketsFromAggregates(firstTokenBuckets)
	_, metrics.FirstTokenP50Ms, metrics.FirstTokenP95Ms,
		metrics.WinsorizedAverageFirstTokenMs = SummarizeChannelMonitorDurationBuckets(
		metrics.FirstTokenDurationBuckets,
	)
	if metrics.TPSSampleCount > 0 {
		value := tpsTotal / float64(metrics.TPSSampleCount)
		metrics.AverageTPS = &value
	}
	metrics.FailureDurationBuckets = []ChannelMonitorFailureDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: failureBucketCounts[0]},
		{LowerBoundMs: 1000, UpperBoundMs: 3000, Count: failureBucketCounts[1]},
		{LowerBoundMs: 3000, UpperBoundMs: 10000, Count: failureBucketCounts[2]},
		{LowerBoundMs: 10000, UpperBoundMs: 30000, Count: failureBucketCounts[3]},
		{LowerBoundMs: 30000, UpperBoundMs: 60000, Count: failureBucketCounts[4]},
		{LowerBoundMs: 60000, UpperBoundMs: 0, Count: failureBucketCounts[5]},
	}
	return metrics
}

func GetChannelSmartScheduleModelSampleStates() ([]ChannelSmartScheduleModelSampleState, error) {
	var states []ChannelSmartScheduleModelSampleState
	err := DB.Find(&states).Error
	return states, err
}

func lockChannelSmartScheduleModelSampleStateTx(
	tx *gorm.DB,
	channelId int,
	modelName string,
) (state ChannelSmartScheduleModelSampleState, err error) {
	conditions := &ChannelSmartScheduleModelSampleState{ChannelId: channelId, ModelName: modelName}
	findErr := lockForUpdate(tx).Where(conditions).First(&state).Error
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(conditions).Error; err != nil {
			return state, err
		}
		if err := lockForUpdate(tx).Where(conditions).First(&state).Error; err != nil {
			return state, err
		}
		return state, nil
	}
	return state, findErr
}

func advanceChannelSmartScheduleObservationSinceTx(
	tx *gorm.DB,
	channelId int,
	modelName string,
	observationSince int64,
) (state ChannelSmartScheduleModelSampleState, advanced bool, err error) {
	modelName = channelSmartScheduleModelName(modelName)
	if channelId <= 0 || modelName == "" || observationSince <= 0 {
		return state, false, errors.New("智能调度共享观测边界缺少渠道、模型或时间")
	}
	state, err = lockChannelSmartScheduleModelSampleStateTx(tx, channelId, modelName)
	if err != nil || observationSince <= state.ObservationSince {
		return state, false, err
	}
	state.ObservationSince = observationSince
	state.RecoverySuccessCount = 0
	state.RecoverySuccessAt = 0
	state.WindowStart = 0
	state.LastTime = 0
	state.LastSuccess = false
	state.LastError = ""
	state.SampleCount = 0
	state.SuccessCount = 0
	state.FailureDurationSampleCount = 0
	state.AverageFailureDurationMs = nil
	state.FirstTokenSampleCount = 0
	state.AverageFirstTokenMs = nil
	state.TPSSampleCount = 0
	state.AverageTPS = nil
	if err := tx.Save(&state).Error; err != nil {
		return state, false, err
	}
	return state, true, nil
}

func SaveChannelSmartScheduleModelSample(
	result ChannelSmartScheduleModelSampleResult,
) (state ChannelSmartScheduleModelSampleState, err error) {
	result.Model = channelSmartScheduleModelName(result.Model)
	if result.ChannelId <= 0 || result.Model == "" {
		return state, errors.New("智能调度共享样本缺少渠道或模型")
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var recoveryTx *channelSmartScheduleProbeRecoveryTx
		if result.ProbeRecovery != nil {
			var prepareErr error
			recoveryTx, prepareErr = prepareChannelSmartScheduleProbeRecoveryTx(
				tx, result.ChannelId, *result.ProbeRecovery,
			)
			if prepareErr != nil {
				return prepareErr
			}
		}
		if recoveryTx == nil || !recoveryTx.revisionMatched {
			if err := lockChannelForDependentWriteTx(tx, result.ChannelId); err != nil {
				return err
			}
		}
		state, err = lockChannelSmartScheduleModelSampleStateTx(tx, result.ChannelId, result.Model)
		if err != nil {
			return err
		}

		sampleTime := result.Time
		if sampleTime <= 0 {
			sampleTime = common.GetTimestamp()
		}
		retentionStart := result.WindowStart
		if retentionStart <= 0 || retentionStart > sampleTime {
			retentionStart = sampleTime
		}
		metricsStart := retentionStart
		if state.ObservationSince > metricsStart {
			metricsStart = state.ObservationSince
		}
		source := strings.TrimSpace(result.Source)
		if source == "" {
			source = ChannelSmartScheduleSampleSourceScheduledProbe
		}
		if source != ChannelSmartScheduleSampleSourceScheduledProbe &&
			source != ChannelSmartScheduleSampleSourceManualTest &&
			source != ChannelSmartScheduleSampleSourceStatusProbe {
			return errors.New("智能调度样本来源无效")
		}
		sampleId := strings.TrimSpace(result.SampleId)

		var samples []channelSmartScheduleSample
		if strings.TrimSpace(string(state.SamplesJSON)) != "" {
			if err := common.UnmarshalJsonStr(string(state.SamplesJSON), &samples); err != nil {
				return fmt.Errorf("解析智能调度共享样本失败: %w", err)
			}
		}
		retained := samples[:0]
		for _, sample := range samples {
			if sample.Time >= retentionStart {
				if sampleId != "" && sample.SampleId == sampleId && sample.Source == source {
					return nil
				}
				retained = append(retained, sample)
			}
		}
		sample := channelSmartScheduleSample{
			Time: sampleTime, Success: result.Success, Source: source, SampleId: sampleId,
		}
		if !result.Success && result.DurationMs != nil && *result.DurationMs >= 0 &&
			!math.IsNaN(*result.DurationMs) && !math.IsInf(*result.DurationMs, 0) {
			value := *result.DurationMs
			sample.FailureDurationMs = &value
		}
		if result.Success && result.FirstTokenMs != nil && *result.FirstTokenMs > 0 &&
			!math.IsNaN(*result.FirstTokenMs) && !math.IsInf(*result.FirstTokenMs, 0) {
			value := *result.FirstTokenMs
			sample.FirstTokenMs = &value
		}
		if result.Success && result.TPS != nil && *result.TPS > 0 &&
			!math.IsNaN(*result.TPS) && !math.IsInf(*result.TPS, 0) {
			value := *result.TPS
			sample.TPS = &value
		}
		samples = append(retained, sample)
		sort.SliceStable(samples, func(i, j int) bool {
			return samples[i].Time < samples[j].Time
		})
		if len(samples) > channelSmartScheduleMaxSamples {
			samples = samples[len(samples)-channelSmartScheduleMaxSamples:]
		}
		rawSamples, err := common.Marshal(samples)
		if err != nil {
			return fmt.Errorf("保存智能调度共享样本失败: %w", err)
		}
		metrics := channelSmartScheduleCalculateSampleMetrics(samples, metricsStart)
		state.WindowStart = metrics.WindowStart
		latestVisibleIndex := -1
		for index := len(samples) - 1; index >= 0; index-- {
			if samples[index].Time >= metricsStart {
				latestVisibleIndex = index
				break
			}
		}
		if latestVisibleIndex < 0 {
			state.LastTime = 0
			state.LastSuccess = false
			state.LastError = ""
		} else {
			latestSample := samples[latestVisibleIndex]
			state.LastTime = latestSample.Time
			state.LastSuccess = latestSample.Success
			message := strings.TrimSpace(result.Error)
			if latestSample.Time != sampleTime {
				message = state.LastError
			}
			messageRunes := []rune(message)
			if len(messageRunes) > 255 {
				message = string(messageRunes[:255])
			}
			state.LastError = message
		}
		state.SampleCount = metrics.SampleCount
		state.SuccessCount = metrics.SuccessCount
		state.FailureDurationSampleCount = metrics.FailureDurationSampleCount
		state.AverageFailureDurationMs = nil
		if metrics.FailureDurationSampleCount > 0 {
			value := metrics.FailureDurationTotalMs / float64(metrics.FailureDurationSampleCount)
			state.AverageFailureDurationMs = &value
		}
		state.FirstTokenSampleCount = metrics.FirstTokenSampleCount
		state.AverageFirstTokenMs = metrics.AverageFirstTokenMs
		state.TPSSampleCount = metrics.TPSSampleCount
		state.AverageTPS = metrics.AverageTPS
		state.SamplesJSON = ChannelSmartScheduleSamplesJSON(rawSamples)
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		if result.ProbeRecovery == nil {
			return nil
		}
		recoveryResult, recoveryErr := applyChannelSmartScheduleProbeRecoveryTx(
			tx,
			&state,
			result.Success,
			sampleTime,
			recoveryTx,
		)
		if recoveryErr != nil {
			return recoveryErr
		}
		result.ProbeRecovery.Result = recoveryResult
		return tx.Where(&ChannelSmartScheduleModelSampleState{Id: state.Id}).First(&state).Error
	})
	if err == nil && result.ProbeRecovery != nil && result.ProbeRecovery.Result.ObservationSince > 0 {
		InvalidateChannelMonitorAggregateCaches()
	}
	return state, err
}
