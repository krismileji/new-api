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
)

var channelSmartScheduleRouteProbeFields = []string{
	"ProbeWindowStart",
	"ProbeLastTime",
	"ProbeLastSuccess",
	"ProbeLastError",
	"ProbeSampleCount",
	"ProbeSuccessCount",
	"ProbeFailureDurationSampleCount",
	"ProbeAverageFailureDurationMs",
	"ProbeFirstTokenSampleCount",
	"ProbeAverageFirstTokenMs",
	"ProbeTPSSampleCount",
	"ProbeAverageTPS",
	"ProbeSamples",
}

type ChannelSmartScheduleRouteState struct {
	Id               int64  `json:"id"`
	ChannelId        int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_smart_schedule_route"`
	GroupName        string `json:"group" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_smart_schedule_route"`
	ModelName        string `json:"model" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_smart_schedule_route"`
	ParticipationSet bool   `json:"participation_set"`
	Excluded         bool   `json:"excluded"`
	Revision         int64  `json:"-" gorm:"bigint"`

	LastScheduleStatus   string   `json:"last_schedule_status" gorm:"type:varchar(16);index"`
	LastScheduleError    string   `json:"last_schedule_error" gorm:"type:varchar(255)"`
	LastScheduleScore    *float64 `json:"last_schedule_score"`
	LastSchedulePriority int64    `json:"last_schedule_priority" gorm:"bigint"`
	LastScheduleWeight   uint     `json:"last_schedule_weight"`
	LastScheduleTime     int64    `json:"last_schedule_time" gorm:"bigint;index"`

	StabilityState             string   `json:"stability_state" gorm:"type:varchar(16);index"`
	StabilityUntil             int64    `json:"stability_until" gorm:"bigint;index"`
	StabilitySince             int64    `json:"stability_since" gorm:"bigint"`
	StabilitySavedPriority     int64    `json:"stability_saved_priority" gorm:"bigint"`
	StabilitySavedWeight       uint     `json:"stability_saved_weight"`
	JitterBaselineFirstTokenMs *float64 `json:"jitter_baseline_first_token_ms"`
	JitterBaselineUpdatedAt    int64    `json:"jitter_baseline_updated_at" gorm:"bigint"`

	ExplorationActive        bool  `json:"exploration_active" gorm:"index"`
	ExplorationSince         int64 `json:"exploration_since" gorm:"bigint"`
	ExplorationSavedPriority int64 `json:"exploration_saved_priority" gorm:"bigint"`
	ExplorationSavedWeight   uint  `json:"exploration_saved_weight"`

	ProbeWindowStart                int64    `json:"probe_window_start" gorm:"bigint"`
	ProbeLastTime                   int64    `json:"probe_last_time" gorm:"bigint;index"`
	ProbeLastSuccess                bool     `json:"probe_last_success"`
	ProbeLastError                  string   `json:"probe_last_error" gorm:"type:varchar(255)"`
	ProbeSampleCount                int64    `json:"probe_sample_count" gorm:"bigint"`
	ProbeSuccessCount               int64    `json:"probe_success_count" gorm:"bigint"`
	ProbeFailureDurationSampleCount int64    `json:"probe_failure_duration_sample_count" gorm:"bigint"`
	ProbeAverageFailureDurationMs   *float64 `json:"probe_average_failure_duration_ms"`
	ProbeFirstTokenSampleCount      int64    `json:"probe_first_token_sample_count" gorm:"bigint"`
	ProbeAverageFirstTokenMs        *float64 `json:"probe_average_first_token_ms"`
	ProbeTPSSampleCount             int64    `json:"probe_tps_sample_count" gorm:"bigint"`
	ProbeAverageTPS                 *float64 `json:"probe_average_tps"`
	// ProbeSamples is an internal rolling JSON sample buffer. Leaving the
	// string type unbounded lets each supported dialect choose its long-text
	// equivalent instead of truncating high-frequency probe history.
	ProbeSamples string `json:"-"`
}

func (state ChannelSmartScheduleRouteState) Participates() bool {
	return state.ParticipationSet && !state.Excluded
}

func saveChannelSmartScheduleRouteStateWithoutProbeTx(tx *gorm.DB, state *ChannelSmartScheduleRouteState) error {
	return tx.Omit(channelSmartScheduleRouteProbeFields...).Save(state).Error
}

type ChannelSmartScheduleRouteKey struct {
	ChannelId int
	Group     string
	Model     string
}

type ChannelSmartScheduleRoute struct {
	ChannelId       int                            `json:"channel_id"`
	ChannelName     string                         `json:"channel_name"`
	ChannelStatus   int                            `json:"channel_status"`
	ChannelPriority int64                          `json:"channel_priority"`
	ChannelWeight   uint                           `json:"channel_weight"`
	Group           string                         `json:"group"`
	Model           string                         `json:"model"`
	Enabled         bool                           `json:"enabled"`
	Priority        int64                          `json:"priority"`
	Weight          uint                           `json:"weight"`
	State           ChannelSmartScheduleRouteState `json:"state"`
}

type ChannelSmartScheduleRouteResultUpdate struct {
	ChannelId               int
	Group                   string
	Model                   string
	Status                  string
	Error                   string
	Score                   *float64
	Priority                int64
	Weight                  uint
	Time                    int64
	Stability               *ChannelSmartScheduleStabilityUpdate
	Jitter                  *ChannelSmartScheduleJitterUpdate
	Exploration             *ChannelSmartScheduleExplorationUpdate
	GuardCurrent            bool
	ExpectedRevision        int64
	ExpectedControlRevision string
	ExpectedPriority        int64
	ExpectedWeight          uint
	ApplyPriorityWeight     bool
}

type ChannelSmartScheduleRouteApplyOutcome struct {
	Key            ChannelSmartScheduleRouteKey
	Applied        bool
	RoutingChanged bool
}

type ChannelSmartScheduleStabilityClearResult struct {
	PreviousState string
	Cleared       bool
	Priority      int64
	Weight        uint
}

type ChannelSmartScheduleStabilityUpdate struct {
	State         string
	Until         int64
	Since         int64
	SavedPriority int64
	SavedWeight   uint
}

type ChannelSmartScheduleJitterUpdate struct {
	BaselineFirstTokenMs *float64
	BaselineUpdatedAt    int64
}

type ChannelSmartScheduleExplorationUpdate struct {
	Active        bool
	Since         int64
	SavedPriority int64
	SavedWeight   uint
}

type ChannelSmartScheduleProbeResult struct {
	ChannelId    int
	Group        string
	Model        string
	WindowStart  int64
	Time         int64
	Success      bool
	Error        string
	DurationMs   *float64
	FirstTokenMs *float64
	TPS          *float64
}

type ChannelSmartScheduleProbeMetrics struct {
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

type channelSmartScheduleProbeSample struct {
	Time              int64    `json:"time"`
	Success           bool     `json:"success"`
	FailureDurationMs *float64 `json:"failure_duration_ms,omitempty"`
	FirstTokenMs      *float64 `json:"first_token_ms,omitempty"`
	TPS               *float64 `json:"tps,omitempty"`
}

const channelSmartScheduleMaxProbeSamples = 1500

func (state ChannelSmartScheduleRouteState) ProbeMetricsSince(windowStart int64) ChannelSmartScheduleProbeMetrics {
	if strings.TrimSpace(state.ProbeSamples) == "" {
		return ChannelSmartScheduleProbeMetrics{}
	}
	var samples []channelSmartScheduleProbeSample
	if err := common.UnmarshalJsonStr(state.ProbeSamples, &samples); err != nil {
		return ChannelSmartScheduleProbeMetrics{}
	}
	return channelSmartScheduleCalculateProbeMetrics(samples, windowStart)
}

func channelSmartScheduleCalculateProbeMetrics(
	samples []channelSmartScheduleProbeSample,
	windowStart int64,
) ChannelSmartScheduleProbeMetrics {
	metrics := ChannelSmartScheduleProbeMetrics{}
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

type ChannelSmartScheduleChannelConfigResult struct {
	Total          int  `json:"total"`
	Updated        int  `json:"updated"`
	RoutingChanged bool `json:"-"`
}

func abilityPriority(ability Ability) int64 {
	if ability.Priority == nil {
		return 0
	}
	return *ability.Priority
}

func channelSmartScheduleRouteKey(channelId int, group string, modelName string) ChannelSmartScheduleRouteKey {
	return ChannelSmartScheduleRouteKey{
		ChannelId: channelId,
		Group:     group,
		Model:     modelName,
	}
}

func InitializeChannelSmartScheduleRouteStates() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var abilities []Ability
		if err := tx.Find(&abilities).Error; err != nil {
			return err
		}
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Find(&states).Error; err != nil {
			return err
		}
		stateByKey := make(map[ChannelSmartScheduleRouteKey]struct{}, len(states))
		for _, state := range states {
			stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = struct{}{}
		}

		newStates := make([]ChannelSmartScheduleRouteState, 0)
		for _, ability := range abilities {
			key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
			if _, exists := stateByKey[key]; exists {
				continue
			}
			state := ChannelSmartScheduleRouteState{
				ChannelId:        ability.ChannelId,
				GroupName:        ability.Group,
				ModelName:        ability.Model,
				ParticipationSet: true,
				Excluded:         true,
				Revision:         1,
			}
			newStates = append(newStates, state)
		}
		if len(newStates) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(newStates, 500).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func GetChannelSmartScheduleRoutes() ([]ChannelSmartScheduleRoute, error) {
	var abilities []Ability
	if err := DB.Find(&abilities).Error; err != nil {
		return nil, err
	}
	var channels []Channel
	if err := DB.Select("id", "name", "status", "priority", "weight").Find(&channels).Error; err != nil {
		return nil, err
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Find(&states).Error; err != nil {
		return nil, err
	}
	channelById := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}
	stateByKey := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteState, len(states))
	for _, state := range states {
		stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
	}
	routes := make([]ChannelSmartScheduleRoute, 0, len(abilities))
	for _, ability := range abilities {
		channel, exists := channelById[ability.ChannelId]
		if !exists {
			continue
		}
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		routes = append(routes, ChannelSmartScheduleRoute{
			ChannelId:       ability.ChannelId,
			ChannelName:     channel.Name,
			ChannelStatus:   channel.Status,
			ChannelPriority: channel.GetPriority(),
			ChannelWeight:   uint(channel.GetWeight()),
			Group:           ability.Group,
			Model:           ability.Model,
			Enabled:         ability.Enabled,
			Priority:        abilityPriority(ability),
			Weight:          ability.Weight,
			State:           stateByKey[key],
		})
	}
	sort.Slice(routes, func(i int, j int) bool {
		if routes[i].Group != routes[j].Group {
			return routes[i].Group < routes[j].Group
		}
		if routes[i].Model != routes[j].Model {
			return routes[i].Model < routes[j].Model
		}
		return routes[i].ChannelId < routes[j].ChannelId
	})
	return routes, nil
}

func SaveChannelSmartScheduleRouteConfig(channelId int, group string, modelName string, excluded bool) (state ChannelSmartScheduleRouteState, routingChanged bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var ability Ability
		if err := lockForUpdate(tx).Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).First(&ability).Error; err != nil {
			return err
		}
		findErr := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{ChannelId: channelId, GroupName: group, ModelName: modelName}).
			First(&state).Error
		created := errors.Is(findErr, gorm.ErrRecordNotFound)
		if created {
			state = ChannelSmartScheduleRouteState{
				ChannelId: channelId,
				GroupName: group,
				ModelName: modelName,
			}
		} else if findErr != nil {
			return findErr
		}
		wasParticipating := state.Participates()
		if state.ParticipationSet && state.Excluded == excluded {
			return nil
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		state.ParticipationSet = true
		state.Excluded = excluded
		if excluded {
			if state.ExplorationActive {
				priority := state.ExplorationSavedPriority
				weight := state.ExplorationSavedWeight
				routingChanged = abilityPriority(ability) != priority || ability.Weight != weight
				if err := updateAbilitySmartSchedulePriorityWeightTx(
					tx, channelSmartScheduleRouteKey(channelId, group, modelName), &priority, &weight,
				); err != nil {
					return err
				}
			}
			state.ExplorationActive = false
			state.ExplorationSince = 0
			state.ExplorationSavedPriority = 0
			state.ExplorationSavedWeight = 0
		}
		if !wasParticipating && !excluded && state.StabilityState == ChannelSmartScheduleStabilityProbing {
			state.StabilitySince = common.GetTimestamp()
		}
		state.Revision++
		if created {
			return tx.Create(&state).Error
		}
		return saveChannelSmartScheduleRouteStateWithoutProbeTx(tx, &state)
	})
	return state, routingChanged, err
}

func SaveChannelSmartScheduleChannelConfig(channelId int, excluded bool) (result ChannelSmartScheduleChannelConfigResult, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var abilities []Ability
		if err := lockForUpdate(tx).Where("channel_id = ?", channelId).Find(&abilities).Error; err != nil {
			return err
		}
		result.Total = len(abilities)
		if result.Total == 0 {
			return nil
		}

		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Where("channel_id = ?", channelId).Find(&states).Error; err != nil {
			return err
		}
		stateByKey := make(map[ChannelSmartScheduleRouteKey]*ChannelSmartScheduleRouteState, len(states))
		for index := range states {
			state := &states[index]
			stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
		}

		now := common.GetTimestamp()
		for _, ability := range abilities {
			key := channelSmartScheduleRouteKey(channelId, ability.Group, ability.Model)
			state := stateByKey[key]
			created := state == nil
			if created {
				state = &ChannelSmartScheduleRouteState{
					ChannelId: channelId,
					GroupName: ability.Group,
					ModelName: ability.Model,
				}
			}
			wasParticipating := state.Participates()
			if state.ParticipationSet && state.Excluded == excluded {
				continue
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.ParticipationSet = true
			state.Excluded = excluded
			if excluded {
				if state.ExplorationActive {
					priority := state.ExplorationSavedPriority
					weight := state.ExplorationSavedWeight
					if abilityPriority(ability) != priority || ability.Weight != weight {
						result.RoutingChanged = true
					}
					if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
						return err
					}
				}
				state.ExplorationActive = false
				state.ExplorationSince = 0
				state.ExplorationSavedPriority = 0
				state.ExplorationSavedWeight = 0
			}
			if !wasParticipating && !excluded && state.StabilityState == ChannelSmartScheduleStabilityProbing {
				state.StabilitySince = now
			}
			state.Revision++
			if created {
				if err := tx.Create(state).Error; err != nil {
					return err
				}
			} else if err := saveChannelSmartScheduleRouteStateWithoutProbeTx(tx, state); err != nil {
				return err
			}
			result.Updated++
		}
		return nil
	})
	return result, err
}

func ClearChannelSmartScheduleExplorations() (routingChanged bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Where("exploration_active = ?", true).Find(&states).Error; err != nil {
			return err
		}
		for index := range states {
			state := &states[index]
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
			var ability Ability
			findErr := lockForUpdate(tx).Where(&Ability{
				ChannelId: state.ChannelId, Group: state.GroupName, Model: state.ModelName,
			}).First(&ability).Error
			if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return findErr
			}
			if findErr == nil && (abilityPriority(ability) != state.ExplorationSavedPriority ||
				ability.Weight != state.ExplorationSavedWeight) {
				priority := state.ExplorationSavedPriority
				weight := state.ExplorationSavedWeight
				if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
					return err
				}
				routingChanged = true
			}
			state.ExplorationActive = false
			state.ExplorationSince = 0
			state.ExplorationSavedPriority = 0
			state.ExplorationSavedWeight = 0
			state.Revision++
			if err := saveChannelSmartScheduleRouteStateWithoutProbeTx(tx, state); err != nil {
				return err
			}
		}
		return nil
	})
	return routingChanged, err
}

func SaveChannelSmartScheduleProbeResult(result ChannelSmartScheduleProbeResult) (state ChannelSmartScheduleRouteState, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(&ChannelSmartScheduleRouteState{
			ChannelId: result.ChannelId, GroupName: result.Group, ModelName: result.Model,
		}).First(&state).Error; err != nil {
			return err
		}
		probeTime := result.Time
		if probeTime <= 0 {
			probeTime = common.GetTimestamp()
		}
		windowStart := result.WindowStart
		if windowStart <= 0 || windowStart > probeTime {
			windowStart = probeTime
		}

		var samples []channelSmartScheduleProbeSample
		if strings.TrimSpace(state.ProbeSamples) != "" {
			if err := common.UnmarshalJsonStr(state.ProbeSamples, &samples); err != nil {
				return fmt.Errorf("解析智能调度探测样本失败: %w", err)
			}
		}
		retained := samples[:0]
		for _, sample := range samples {
			if sample.Time >= windowStart && sample.Time <= probeTime {
				retained = append(retained, sample)
			}
		}
		sample := channelSmartScheduleProbeSample{Time: probeTime, Success: result.Success}
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
		if len(samples) > channelSmartScheduleMaxProbeSamples {
			samples = samples[len(samples)-channelSmartScheduleMaxProbeSamples:]
		}
		rawSamples, err := common.Marshal(samples)
		if err != nil {
			return fmt.Errorf("保存智能调度探测样本失败: %w", err)
		}
		metrics := channelSmartScheduleCalculateProbeMetrics(samples, windowStart)

		state.ProbeLastTime = probeTime
		state.ProbeLastSuccess = result.Success
		message := strings.TrimSpace(result.Error)
		messageRunes := []rune(message)
		if len(messageRunes) > 255 {
			message = string(messageRunes[:255])
		}
		state.ProbeLastError = message
		state.ProbeSamples = string(rawSamples)
		state.ProbeWindowStart = metrics.WindowStart
		state.ProbeSampleCount = metrics.SampleCount
		state.ProbeSuccessCount = metrics.SuccessCount
		state.ProbeFailureDurationSampleCount = metrics.FailureDurationSampleCount
		state.ProbeAverageFailureDurationMs = nil
		if metrics.FailureDurationSampleCount > 0 {
			value := metrics.FailureDurationTotalMs / float64(metrics.FailureDurationSampleCount)
			state.ProbeAverageFailureDurationMs = &value
		}
		state.ProbeFirstTokenSampleCount = metrics.FirstTokenSampleCount
		state.ProbeAverageFirstTokenMs = metrics.AverageFirstTokenMs
		state.ProbeTPSSampleCount = metrics.TPSSampleCount
		state.ProbeAverageTPS = metrics.AverageTPS
		return tx.Model(&ChannelSmartScheduleRouteState{}).
			Where("id = ?", state.Id).
			Updates(map[string]any{
				"probe_window_start":                  state.ProbeWindowStart,
				"probe_last_time":                     state.ProbeLastTime,
				"probe_last_success":                  state.ProbeLastSuccess,
				"probe_last_error":                    state.ProbeLastError,
				"probe_sample_count":                  state.ProbeSampleCount,
				"probe_success_count":                 state.ProbeSuccessCount,
				"probe_failure_duration_sample_count": state.ProbeFailureDurationSampleCount,
				"probe_average_failure_duration_ms":   state.ProbeAverageFailureDurationMs,
				"probe_first_token_sample_count":      state.ProbeFirstTokenSampleCount,
				"probe_average_first_token_ms":        state.ProbeAverageFirstTokenMs,
				"probe_tps_sample_count":              state.ProbeTPSSampleCount,
				"probe_average_tps":                   state.ProbeAverageTPS,
				"probe_samples":                       state.ProbeSamples,
			}).Error
	})
	return state, err
}

func ApplyChannelSmartScheduleRouteResults(results []ChannelSmartScheduleRouteResultUpdate) ([]ChannelSmartScheduleRouteApplyOutcome, error) {
	if len(results) == 0 {
		return nil, nil
	}
	outcomes := make([]ChannelSmartScheduleRouteApplyOutcome, 0, len(results))
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, result := range results {
			key := channelSmartScheduleRouteKey(result.ChannelId, result.Group, result.Model)
			outcome := ChannelSmartScheduleRouteApplyOutcome{Key: key}
			var controlOption Option
			if result.GuardCurrent {
				optionErr := lockForUpdate(tx).Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).First(&controlOption).Error
				if errors.Is(optionErr, gorm.ErrRecordNotFound) {
					controlOption.Value = ""
				} else if optionErr != nil {
					return optionErr
				}
			}
			var state ChannelSmartScheduleRouteState
			findErr := lockForUpdate(tx).
				Where(&ChannelSmartScheduleRouteState{ChannelId: result.ChannelId, GroupName: result.Group, ModelName: result.Model}).
				First(&state).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				state = ChannelSmartScheduleRouteState{
					ChannelId: result.ChannelId, GroupName: result.Group, ModelName: result.Model,
					ParticipationSet: true, Excluded: true,
				}
			} else if findErr != nil {
				return findErr
			}
			var ability Ability
			if err := lockForUpdate(tx).
				Where(&Ability{ChannelId: result.ChannelId, Group: result.Group, Model: result.Model}).
				First(&ability).Error; err != nil {
				return err
			}
			if result.GuardCurrent {
				if controlOption.Value != result.ExpectedControlRevision || state.Revision != result.ExpectedRevision ||
					!state.Participates() || !ability.Enabled || abilityPriority(ability) != result.ExpectedPriority ||
					ability.Weight != result.ExpectedWeight {
					outcomes = append(outcomes, outcome)
					continue
				}
				var channel Channel
				if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", result.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				if channel.Status != common.ChannelStatusEnabled {
					outcomes = append(outcomes, outcome)
					continue
				}
			}

			message := strings.TrimSpace(result.Error)
			messageRunes := []rune(message)
			if len(messageRunes) > 255 {
				message = string(messageRunes[:255])
			}
			updatedTime := result.Time
			if updatedTime <= 0 {
				updatedTime = common.GetTimestamp()
			}
			state.LastScheduleStatus = result.Status
			state.LastScheduleError = message
			state.LastScheduleScore = result.Score
			state.LastSchedulePriority = result.Priority
			state.LastScheduleWeight = result.Weight
			state.LastScheduleTime = updatedTime
			if result.Stability != nil {
				state.StabilityState = result.Stability.State
				state.StabilityUntil = result.Stability.Until
				state.StabilitySince = result.Stability.Since
				state.StabilitySavedPriority = result.Stability.SavedPriority
				state.StabilitySavedWeight = result.Stability.SavedWeight
			}
			if result.Jitter != nil {
				state.JitterBaselineFirstTokenMs = result.Jitter.BaselineFirstTokenMs
				state.JitterBaselineUpdatedAt = result.Jitter.BaselineUpdatedAt
			}
			if result.Exploration != nil {
				state.ExplorationActive = result.Exploration.Active
				state.ExplorationSince = result.Exploration.Since
				state.ExplorationSavedPriority = result.Exploration.SavedPriority
				state.ExplorationSavedWeight = result.Exploration.SavedWeight
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.Revision++
			if result.ApplyPriorityWeight {
				priority := result.Priority
				weight := result.Weight
				if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
					return err
				}
				outcome.RoutingChanged = priority != result.ExpectedPriority || weight != result.ExpectedWeight
			}
			if state.Id == 0 {
				if err := tx.Create(&state).Error; err != nil {
					return err
				}
			} else if err := saveChannelSmartScheduleRouteStateWithoutProbeTx(tx, &state); err != nil {
				return err
			}
			outcome.Applied = true
			outcomes = append(outcomes, outcome)
		}
		return nil
	})
	return outcomes, err
}

func ClearChannelSmartScheduleRouteStability(channelId int, group string, modelName string, fallbackPriority int64, fallbackWeight uint) (result ChannelSmartScheduleStabilityClearResult, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var state ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{ChannelId: channelId, GroupName: group, ModelName: modelName}).
			First(&state).Error; err != nil {
			return err
		}
		var ability Ability
		if err := lockForUpdate(tx).Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).First(&ability).Error; err != nil {
			return err
		}
		result.PreviousState = state.StabilityState
		result.Priority = abilityPriority(ability)
		result.Weight = ability.Weight
		if result.PreviousState == "" {
			return nil
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		result.Priority = state.StabilitySavedPriority
		if result.Priority <= 0 {
			result.Priority = fallbackPriority
		}
		result.Weight = state.StabilitySavedWeight
		if result.Weight == 0 {
			result.Weight = fallbackWeight
		}
		key := channelSmartScheduleRouteKey(channelId, group, modelName)
		if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &result.Priority, &result.Weight); err != nil {
			return err
		}
		now := common.GetTimestamp()
		state.StabilityState = ""
		state.StabilityUntil = 0
		state.StabilitySince = now
		state.StabilitySavedPriority = 0
		state.StabilitySavedWeight = 0
		state.JitterBaselineFirstTokenMs = nil
		state.JitterBaselineUpdatedAt = 0
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "管理员已手动解除稳定性保护"
		state.LastScheduleScore = nil
		state.LastSchedulePriority = result.Priority
		state.LastScheduleWeight = result.Weight
		state.LastScheduleTime = now
		state.Revision++
		result.Cleared = true
		return saveChannelSmartScheduleRouteStateWithoutProbeTx(tx, &state)
	})
	return result, err
}

func updateAbilitySmartSchedulePriorityWeightTx(tx *gorm.DB, key ChannelSmartScheduleRouteKey, priority *int64, weight *uint) error {
	updates := make(map[string]any, 2)
	if priority != nil {
		updates["priority"] = *priority
	}
	if weight != nil {
		updates["weight"] = *weight
	}
	if len(updates) == 0 {
		return nil
	}
	conditions := &Ability{ChannelId: key.ChannelId, Group: key.Group, Model: key.Model}
	result := tx.Model(&Ability{}).Where(conditions).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&Ability{}).Where(conditions).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
