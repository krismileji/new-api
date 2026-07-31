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
	ChannelId    int
	Model        string
	Source       string
	SampleId     string
	WindowStart  int64
	Time         int64
	Success      bool
	Error        string
	DurationMs   *float64
	FirstTokenMs *float64
	TPS          *float64
}

type ChannelSmartScheduleSampleMetrics struct {
	WindowStart                   int64
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
)

func (state ChannelSmartScheduleModelSampleState) MetricsSince(windowStart int64) ChannelSmartScheduleSampleMetrics {
	return state.metricsSince(windowStart, "")
}

func (state ChannelSmartScheduleModelSampleState) ManualTestMetricsSince(windowStart int64) ChannelSmartScheduleSampleMetrics {
	return state.metricsSince(windowStart, ChannelSmartScheduleSampleSourceManualTest)
}

func (state ChannelSmartScheduleModelSampleState) metricsSince(
	windowStart int64,
	source string,
) ChannelSmartScheduleSampleMetrics {
	if strings.TrimSpace(string(state.SamplesJSON)) == "" {
		return ChannelSmartScheduleSampleMetrics{}
	}
	var samples []channelSmartScheduleSample
	if err := common.UnmarshalJsonStr(string(state.SamplesJSON), &samples); err != nil {
		return ChannelSmartScheduleSampleMetrics{}
	}
	if source != "" {
		filtered := samples[:0]
		for _, sample := range samples {
			if sample.Source == source {
				filtered = append(filtered, sample)
			}
		}
		samples = filtered
	}
	return channelSmartScheduleCalculateSampleMetrics(samples, windowStart)
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

func SaveChannelSmartScheduleModelSample(
	result ChannelSmartScheduleModelSampleResult,
) (state ChannelSmartScheduleModelSampleState, err error) {
	result.Model = strings.TrimSpace(result.Model)
	if result.ChannelId <= 0 || result.Model == "" {
		return state, errors.New("智能调度共享样本缺少渠道或模型")
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		conditions := &ChannelSmartScheduleModelSampleState{ChannelId: result.ChannelId, ModelName: result.Model}
		findErr := lockForUpdate(tx).Where(conditions).First(&state).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(conditions).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).Where(conditions).First(&state).Error; err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		}

		sampleTime := result.Time
		if sampleTime <= 0 {
			sampleTime = common.GetTimestamp()
		}
		windowStart := result.WindowStart
		if windowStart <= 0 || windowStart > sampleTime {
			windowStart = sampleTime
		}
		source := strings.TrimSpace(result.Source)
		if source == "" {
			source = ChannelSmartScheduleSampleSourceScheduledProbe
		}
		if source != ChannelSmartScheduleSampleSourceScheduledProbe &&
			source != ChannelSmartScheduleSampleSourceManualTest {
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
			if sample.Time >= windowStart {
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
		metrics := channelSmartScheduleCalculateSampleMetrics(samples, windowStart)
		latestSample := samples[len(samples)-1]

		state.WindowStart = metrics.WindowStart
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
		return tx.Save(&state).Error
	})
	return state, err
}
