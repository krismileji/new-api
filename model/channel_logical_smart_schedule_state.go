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

// ChannelLogicalSmartScheduleSampleState is the logical scheduling-only
// sample buffer. Ordinary monitor events remain stored and queried by the
// physical ChannelId; this table is never read by ordinary monitor APIs.
type ChannelLogicalSmartScheduleSampleState struct {
	Id                   int64                           `json:"-" gorm:"primaryKey"`
	LogicalGroupID       int64                           `json:"-" gorm:"bigint;not null;uniqueIndex:uk_logical_smart_sample,priority:1"`
	LogicalRevision      int64                           `json:"-" gorm:"bigint;not null;uniqueIndex:uk_logical_smart_sample,priority:2"`
	GroupName            string                          `json:"-" gorm:"type:varchar(64);not null;uniqueIndex:uk_logical_smart_sample,priority:3"`
	ModelName            string                          `json:"-" gorm:"type:varchar(255);not null;uniqueIndex:uk_logical_smart_sample,priority:4"`
	ObservationSince     int64                           `json:"-" gorm:"bigint;not null"`
	RecoverySuccessCount int                             `json:"-" gorm:"not null"`
	RecoverySuccessAt    int64                           `json:"-" gorm:"bigint;not null"`
	SamplesJSON          ChannelSmartScheduleSamplesJSON `json:"-"`
	UpdatedAt            int64                           `json:"-" gorm:"bigint;not null"`
}

// ChannelLogicalSmartScheduleRouteState stores one protection/scoring/primary
// state per immutable logical identity. StateJSON uses the existing physical
// route-state shape so the scheduler can reuse its scoring algorithm.
type ChannelLogicalSmartScheduleRouteState struct {
	Id              int64                           `json:"-" gorm:"primaryKey"`
	LogicalGroupID  int64                           `json:"-" gorm:"bigint;not null;uniqueIndex:uk_logical_smart_route,priority:1"`
	LogicalRevision int64                           `json:"-" gorm:"bigint;not null;uniqueIndex:uk_logical_smart_route,priority:2"`
	GroupName       string                          `json:"-" gorm:"type:varchar(64);not null;uniqueIndex:uk_logical_smart_route,priority:3"`
	ModelName       string                          `json:"-" gorm:"type:varchar(255);not null;uniqueIndex:uk_logical_smart_route,priority:4"`
	StateRevision   int64                           `json:"-" gorm:"bigint;not null"`
	StateJSON       ChannelSmartScheduleSamplesJSON `json:"-"`
	UpdatedAt       int64                           `json:"-" gorm:"bigint;not null"`
}

type channelLogicalSmartScheduleRoutePayload struct {
	State                      ChannelSmartScheduleRouteState `json:"state"`
	ManualPrimarySaved         bool                           `json:"manual_primary_saved"`
	ManualPrimarySavedPriority int64                          `json:"manual_primary_saved_priority"`
	ManualPrimarySavedWeight   uint                           `json:"manual_primary_saved_weight"`
	EffectiveRoutingSet        bool                           `json:"effective_routing_set"`
	EffectivePriority          int64                          `json:"effective_priority"`
	EffectiveWeight            uint                           `json:"effective_weight"`
}

type channelLogicalSmartScheduleRouteKey struct {
	logicalID int64
	revision  int64
	group     string
	model     string
}

type channelLogicalSmartScheduleRouting struct {
	priority int64
	weight   uint
}

func normalizeLogicalSmartScheduleIdentity(identity LogicalChannelIdentity, groupName, modelName string) (string, string, error) {
	groupName = strings.TrimSpace(groupName)
	modelName = channelSmartScheduleModelName(modelName)
	if identity.ChannelID <= 0 || identity.LogicalChannelID <= 0 || identity.Revision <= 0 ||
		groupName == "" || modelName == "" || identity.LogicalChannelID == int64(identity.ChannelID) {
		return "", "", errors.New("逻辑智能调度身份缺少逻辑组、修订号、分组或模型")
	}
	return groupName, modelName, nil
}

func lockLogicalSmartScheduleIdentityTx(tx *gorm.DB, identity LogicalChannelIdentity) (ChannelLogicalGroup, error) {
	var group ChannelLogicalGroup
	if err := lockForUpdate(tx).Select("id", "status", "revision", "updated_at").
		Where("id = ?", identity.LogicalChannelID).First(&group).Error; err != nil {
		return ChannelLogicalGroup{}, err
	}
	if group.Revision != identity.Revision {
		return ChannelLogicalGroup{}, ErrChannelLogicalGroupRevisionConflict
	}
	if !IsLogicalChannelGroupActive(group.Status) {
		return ChannelLogicalGroup{}, ErrLogicalChannelSelectionGroupDisabled
	}
	var count int64
	if err := tx.Model(&ChannelLogicalGroupMember{}).Where(
		"logical_group_id = ? AND channel_id = ?", identity.LogicalChannelID, identity.ChannelID,
	).Count(&count).Error; err != nil {
		return ChannelLogicalGroup{}, err
	}
	if count != 1 {
		return ChannelLogicalGroup{}, ErrChannelLogicalGroupRevisionConflict
	}
	return group, nil
}

// SaveLogicalChannelSmartScheduleModelSample writes a sample to the frozen
// logical identity. Ungrouped/disabled rollout callers retain the original
// physical sample behavior. The physical execution channel is retained on
// every logical sample for audit and never becomes an ordinary monitor row.
func SaveLogicalChannelSmartScheduleModelSample(
	identity LogicalChannelIdentity,
	groupName string,
	result ChannelSmartScheduleModelSampleResult,
) (ChannelSmartScheduleModelSampleState, error) {
	if !IsLogicalChannelGroupingEnabled() || identity.Revision == 0 ||
		identity.LogicalChannelID == int64(identity.ChannelID) {
		return SaveChannelSmartScheduleModelSample(result)
	}
	groupName, modelName, err := normalizeLogicalSmartScheduleIdentity(identity, groupName, result.Model)
	if err != nil {
		return ChannelSmartScheduleModelSampleState{}, err
	}
	result.Model = modelName
	channelStatusLock.Lock()

	var view ChannelSmartScheduleModelSampleState
	err = DB.Transaction(func(tx *gorm.DB) error {
		group, err := lockLogicalSmartScheduleIdentityTx(tx, identity)
		if err != nil {
			return err
		}
		conditions := ChannelLogicalSmartScheduleSampleState{
			LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
			GroupName: groupName, ModelName: modelName,
		}
		var state ChannelLogicalSmartScheduleSampleState
		findErr := lockForUpdate(tx).Where(&conditions).First(&state).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			state = conditions
			state.ObservationSince = group.UpdatedAt
			state.UpdatedAt = common.GetTimestamp()
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).Where(&conditions).First(&state).Error; err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		}
		sampleTime := result.Time
		if sampleTime <= 0 {
			sampleTime = common.GetTimestamp()
		}
		retentionStart := result.WindowStart
		if retentionStart <= 0 || retentionStart > sampleTime {
			retentionStart = sampleTime
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
		sampleID := strings.TrimSpace(result.SampleId)
		var samples []channelSmartScheduleSample
		if strings.TrimSpace(string(state.SamplesJSON)) != "" {
			if err := common.UnmarshalJsonStr(string(state.SamplesJSON), &samples); err != nil {
				return fmt.Errorf("解析逻辑智能调度共享样本失败: %w", err)
			}
		}
		retained := samples[:0]
		for _, sample := range samples {
			if sample.Time < retentionStart {
				continue
			}
			if sampleID != "" && sample.SampleId == sampleID && sample.Source == source {
				view = logicalSmartScheduleSampleView(identity.ChannelID, modelName, state)
				return nil
			}
			retained = append(retained, sample)
		}
		sample := channelSmartScheduleSample{
			Time: sampleTime, Success: result.Success, Source: source, SampleId: sampleID,
			ChannelId: result.ChannelId,
		}
		if !result.Success && result.DurationMs != nil && *result.DurationMs >= 0 &&
			!math.IsNaN(*result.DurationMs) && !math.IsInf(*result.DurationMs, 0) {
			value := *result.DurationMs
			sample.FailureDurationMs = &value
		}
		if result.Success && result.FirstTokenMs != nil && *result.FirstTokenMs >= 0 &&
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
		sort.SliceStable(samples, func(i, j int) bool { return samples[i].Time < samples[j].Time })
		if len(samples) > channelSmartScheduleMaxSamples {
			samples = samples[len(samples)-channelSmartScheduleMaxSamples:]
		}
		raw, err := common.Marshal(samples)
		if err != nil {
			return err
		}
		state.SamplesJSON = ChannelSmartScheduleSamplesJSON(raw)
		state.UpdatedAt = common.GetTimestamp()
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		view = logicalSmartScheduleSampleView(identity.ChannelID, modelName, state)
		return nil
	})
	channelStatusLock.Unlock()
	if err == nil && result.ProbeRecovery != nil {
		result.ProbeRecovery.Result, err = ApplyLogicalChannelSmartScheduleProbeRecovery(
			identity, groupName, modelName, result.Success, view.LastTime, *result.ProbeRecovery,
		)
	}
	return view, err
}

func logicalSmartScheduleSampleView(
	physicalChannelID int,
	modelName string,
	state ChannelLogicalSmartScheduleSampleState,
) ChannelSmartScheduleModelSampleState {
	view := ChannelSmartScheduleModelSampleState{
		ChannelId: physicalChannelID, ModelName: modelName,
		ObservationSince:     state.ObservationSince,
		RecoverySuccessCount: state.RecoverySuccessCount, RecoverySuccessAt: state.RecoverySuccessAt,
		SamplesJSON: state.SamplesJSON,
	}
	return view.Windowed(0)
}

func loadLogicalSmartScheduleSampleView(
	identity LogicalChannelIdentity, groupName, modelName string, physicalChannelID int,
) (ChannelSmartScheduleModelSampleState, error) {
	groupName, modelName, err := normalizeLogicalSmartScheduleIdentity(identity, groupName, modelName)
	if err != nil {
		return ChannelSmartScheduleModelSampleState{}, err
	}
	var state ChannelLogicalSmartScheduleSampleState
	err = DB.Where(&ChannelLogicalSmartScheduleSampleState{
		LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
		GroupName: groupName, ModelName: modelName,
	}).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelSmartScheduleModelSampleState{ChannelId: physicalChannelID, ModelName: modelName}, nil
	}
	if err != nil {
		return ChannelSmartScheduleModelSampleState{}, err
	}
	return logicalSmartScheduleSampleView(physicalChannelID, modelName, state), nil
}

func advanceLogicalChannelSmartScheduleObservationSinceTx(
	tx *gorm.DB,
	identity LogicalChannelIdentity,
	groupName string,
	modelName string,
	observationSince int64,
) (state ChannelLogicalSmartScheduleSampleState, advanced bool, err error) {
	if observationSince <= 0 {
		return state, false, errors.New("逻辑智能调度共享观测边界缺少时间")
	}
	groupName, modelName, err = normalizeLogicalSmartScheduleIdentity(identity, groupName, modelName)
	if err != nil {
		return state, false, err
	}
	conditions := ChannelLogicalSmartScheduleSampleState{
		LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
		GroupName: groupName, ModelName: modelName,
	}
	if err := lockForUpdate(tx).Where(&conditions).First(&state).Error; err != nil {
		return state, false, err
	}
	if observationSince <= state.ObservationSince {
		return state, false, nil
	}
	state.ObservationSince = observationSince
	state.RecoverySuccessCount = 0
	state.RecoverySuccessAt = 0
	state.UpdatedAt = common.GetTimestamp()
	if err := tx.Save(&state).Error; err != nil {
		return state, false, err
	}
	return state, true, nil
}

func encodeLogicalSmartScheduleRouteState(state ChannelSmartScheduleRouteState) (ChannelSmartScheduleSamplesJSON, error) {
	return encodeLogicalSmartScheduleRouteStateWithRouting(
		state, state.LastSchedulePriority, state.LastScheduleWeight,
	)
}

func encodeLogicalSmartScheduleRouteStateWithRouting(
	state ChannelSmartScheduleRouteState,
	priority int64,
	weight uint,
) (ChannelSmartScheduleSamplesJSON, error) {
	payload := channelLogicalSmartScheduleRoutePayload{
		State:                      state,
		ManualPrimarySaved:         state.ManualPrimarySaved,
		ManualPrimarySavedPriority: state.ManualPrimarySavedPriority,
		ManualPrimarySavedWeight:   state.ManualPrimarySavedWeight,
		EffectiveRoutingSet:        true,
		EffectivePriority:          priority,
		EffectiveWeight:            weight,
	}
	raw, err := common.Marshal(payload)
	return ChannelSmartScheduleSamplesJSON(raw), err
}

func decodeLogicalSmartScheduleRoutePayload(
	raw ChannelSmartScheduleSamplesJSON,
) (channelLogicalSmartScheduleRoutePayload, error) {
	var payload channelLogicalSmartScheduleRoutePayload
	if err := common.UnmarshalJsonStr(string(raw), &payload); err != nil {
		return channelLogicalSmartScheduleRoutePayload{}, err
	}
	payload.State.ManualPrimarySaved = payload.ManualPrimarySaved
	payload.State.ManualPrimarySavedPriority = payload.ManualPrimarySavedPriority
	payload.State.ManualPrimarySavedWeight = payload.ManualPrimarySavedWeight
	if !payload.EffectiveRoutingSet {
		payload.EffectivePriority = payload.State.LastSchedulePriority
		payload.EffectiveWeight = payload.State.LastScheduleWeight
	}
	return payload, nil
}

func decodeLogicalSmartScheduleRouteState(raw ChannelSmartScheduleSamplesJSON) (ChannelSmartScheduleRouteState, error) {
	payload, err := decodeLogicalSmartScheduleRoutePayload(raw)
	return payload.State, err
}

func loadOrCreateLogicalSmartScheduleRouteState(
	identity LogicalChannelIdentity,
	groupName, modelName string,
	seed ChannelSmartScheduleRouteState,
	seedPriority int64,
	seedWeight uint,
) (ChannelSmartScheduleRouteState, int64, uint, error) {
	groupName, modelName, err := normalizeLogicalSmartScheduleIdentity(identity, groupName, modelName)
	if err != nil {
		return ChannelSmartScheduleRouteState{}, 0, 0, err
	}
	var result ChannelSmartScheduleRouteState
	effectivePriority := seedPriority
	effectiveWeight := seedWeight
	err = DB.Transaction(func(tx *gorm.DB) error {
		group, err := lockLogicalSmartScheduleIdentityTx(tx, identity)
		if err != nil {
			return err
		}
		conditions := ChannelLogicalSmartScheduleRouteState{
			LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
			GroupName: groupName, ModelName: modelName,
		}
		var stored ChannelLogicalSmartScheduleRouteState
		findErr := lockForUpdate(tx).Where(&conditions).First(&stored).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			seed.Id = 0
			seed.ChannelId = 0
			seed.GroupName = groupName
			seed.ModelName = modelName
			if seed.Revision <= 0 {
				seed.Revision = 1
			}
			raw, err := encodeLogicalSmartScheduleRouteStateWithRouting(seed, seedPriority, seedWeight)
			if err != nil {
				return err
			}
			stored = conditions
			stored.StateRevision = seed.Revision
			stored.StateJSON = raw
			stored.UpdatedAt = common.GetTimestamp()
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&stored).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).Where(&conditions).First(&stored).Error; err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		}
		sampleConditions := ChannelLogicalSmartScheduleSampleState{
			LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
			GroupName: groupName, ModelName: modelName,
		}
		sampleConditions.ObservationSince = group.UpdatedAt
		sampleConditions.UpdatedAt = common.GetTimestamp()
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&sampleConditions).Error; err != nil {
			return err
		}
		payload, decodeErr := decodeLogicalSmartScheduleRoutePayload(stored.StateJSON)
		if decodeErr != nil {
			return decodeErr
		}
		result = payload.State
		effectivePriority = payload.EffectivePriority
		effectiveWeight = payload.EffectiveWeight
		result.Revision = stored.StateRevision
		result.ChannelId = 0
		result.GroupName = groupName
		result.ModelName = modelName
		return nil
	})
	return result, effectivePriority, effectiveWeight, err
}

func protectLogicalChannelSmartScheduleRouteOnRuntimeFailure(
	identity LogicalChannelIdentity,
	groupName string,
	modelName string,
	protectionUntil int64,
	reason string,
	expectedControlRevision string,
	allowNormalRoute bool,
	recoveryProbeOnly bool,
	redisEventSequence int64,
) (result ChannelSmartScheduleRuntimeFailureResult, err error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	now := common.GetTimestamp()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockLogicalSmartScheduleIdentityTx(tx, identity); err != nil {
			return err
		}
		var redisEffectState *ChannelMonitorRedisEffectState
		if redisEventSequence > 0 {
			redisEffectState, err = lockChannelMonitorRedisEffectStateTx(
				tx, channelMonitorRedisLogicalProtectionEffectKey(
					identity.LogicalChannelID, identity.Revision, groupName, modelName,
				),
			)
			if err != nil {
				return err
			}
			if redisEventSequence <= redisEffectState.EventSequence {
				return nil
			}
		}
		finishRedisEffect := func() error {
			return advanceChannelMonitorRedisEffectStateTx(tx, redisEffectState, redisEventSequence)
		}
		controlRevision, err := lockChannelSmartScheduleControlRevisionTx(tx)
		if err != nil {
			return err
		}
		if controlRevision != expectedControlRevision {
			return finishRedisEffect()
		}

		var members []ChannelLogicalGroupMember
		if err := lockForUpdate(tx).Where("logical_group_id = ?", identity.LogicalChannelID).
			Order("channel_id ASC").Find(&members).Error; err != nil {
			return err
		}
		memberIDs := make([]int, 0, len(members))
		memberSet := make(map[int]struct{}, len(members))
		for _, member := range members {
			memberIDs = append(memberIDs, member.ChannelID)
			memberSet[member.ChannelID] = struct{}{}
		}
		if len(memberIDs) < 2 {
			return ErrChannelLogicalGroupRevisionConflict
		}
		pool := channelSmartScheduleRoutePool{group: groupName, model: modelName}
		if _, err := lockChannelSmartScheduleRoutePoolChannelsTx(tx, groupName, modelName, memberIDs...); err != nil {
			return err
		}
		states, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, []channelSmartScheduleRoutePool{pool})
		if err != nil {
			return err
		}
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, []channelSmartScheduleRoutePool{pool})
		if err != nil {
			return err
		}
		stateByChannel := make(map[int]*ChannelSmartScheduleRouteState, len(memberIDs))
		for index := range states {
			if _, member := memberSet[states[index].ChannelId]; member {
				stateByChannel[states[index].ChannelId] = &states[index]
			}
		}
		abilityByChannel := make(map[int]*Ability, len(memberIDs))
		for index := range abilities {
			if _, member := memberSet[abilities[index].ChannelId]; member {
				abilityByChannel[abilities[index].ChannelId] = &abilities[index]
			}
		}
		triggerState := stateByChannel[identity.ChannelID]
		triggerAbility := abilityByChannel[identity.ChannelID]
		if triggerState == nil || triggerAbility == nil || !triggerState.Participates() || !triggerAbility.Enabled {
			return finishRedisEffect()
		}

		conditions := ChannelLogicalSmartScheduleRouteState{
			LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
			GroupName: groupName, ModelName: modelName,
		}
		var stored ChannelLogicalSmartScheduleRouteState
		findErr := lockForUpdate(tx).Where(&conditions).First(&stored).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			seed := *triggerState
			seed.Id = 0
			seed.ChannelId = 0
			seed.GroupName = groupName
			seed.ModelName = modelName
			priority, weight := channelSmartScheduleAbilityRouting(*triggerAbility)
			raw, err := encodeLogicalSmartScheduleRouteStateWithRouting(seed, priority, weight)
			if err != nil {
				return err
			}
			stored = conditions
			stored.StateRevision = max(seed.Revision, 1)
			stored.StateJSON = raw
			stored.UpdatedAt = now
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&stored).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).Where(&conditions).First(&stored).Error; err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		}
		payload, err := decodeLogicalSmartScheduleRoutePayload(stored.StateJSON)
		if err != nil {
			return err
		}
		state := payload.State
		state.Revision = stored.StateRevision
		if !state.Participates() || protectionUntil <= now {
			return finishRedisEffect()
		}
		activeTemporaryTraffic := state.TemporaryTrafficKind != "" ||
			state.StabilityState == ChannelSmartScheduleStabilityProbing
		activeFixedPrimary := state.ManualPrimaryUntil > now &&
			state.ManualPrimaryAllowStabilityDegrade && state.StabilityState == ""
		normalRouteEligible := allowNormalRoute &&
			(state.StabilityState == "" || state.StabilityState == ChannelSmartScheduleStabilityDegraded)
		recoveryProbeEligible := recoveryProbeOnly &&
			(state.StabilityState == ChannelSmartScheduleStabilityDegraded ||
				state.StabilityState == ChannelSmartScheduleStabilityProbing)
		manualPrimaryBlocksDegrade := state.ManualPrimaryUntil > now &&
			!state.ManualPrimaryAllowStabilityDegrade && state.StabilityState == ""
		if manualPrimaryBlocksDegrade ||
			(!activeTemporaryTraffic && !activeFixedPrimary && !normalRouteEligible && !recoveryProbeEligible) {
			return finishRedisEffect()
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}

		result.Handled = true
		result.PreviousState = state.StabilityState
		savedPriority := state.StabilitySavedPriority
		savedWeight := state.StabilitySavedWeight
		if state.TemporaryTrafficKind != "" {
			if state.BasePriority > 0 {
				savedPriority = state.BasePriority
			}
			if state.BaseWeight > 0 {
				savedWeight = state.BaseWeight
			}
		}
		if savedPriority <= 0 {
			savedPriority = payload.EffectivePriority
		}
		if savedWeight == 0 {
			savedWeight = payload.EffectiveWeight
		}
		if savedPriority <= 0 {
			savedPriority = channelSmartScheduleRuntimeFallbackPriority
		}
		if savedWeight == 0 {
			savedWeight = channelSmartScheduleRuntimeFallbackWeight
		}
		state.TemporaryTrafficKind = ""
		state.TemporaryTrafficSince = 0
		state.TemporaryTrafficTargetPercent = 0
		state.ExplorationMaxPromptTokens = 0
		state.StabilityReleaseMaxPromptTokens = 0
		state.StabilityState = ChannelSmartScheduleStabilityDegraded
		state.StabilityUntil = max(state.StabilityUntil, protectionUntil)
		state.StabilitySince = 0
		state.StabilitySavedPriority = savedPriority
		state.StabilitySavedWeight = savedWeight
		state.RuntimeProtectionUntil = max(state.RuntimeProtectionUntil, protectionUntil)
		state.LastScheduleStatus = ChannelSmartScheduleStatusFailed
		state.LastScheduleError = reason
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = 0
		state.LastScheduleWeight = 0
		state.LastScheduleTime = now
		state.Revision++

		result.RoutingChanged = payload.EffectivePriority != 0 || payload.EffectiveWeight != 0
		raw, err := encodeLogicalSmartScheduleRouteStateWithRouting(state, 0, 0)
		if err != nil {
			return err
		}
		updated := tx.Model(&ChannelLogicalSmartScheduleRouteState{}).Where(
			"id = ? AND state_revision = ?", stored.Id, stored.StateRevision,
		).Updates(map[string]any{
			"state_revision": state.Revision, "state_json": raw, "updated_at": now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelLogicalGroupRevisionConflict
		}
		return finishRedisEffect()
	})
	if err != nil {
		result.Handled = false
		result.RoutingChanged = false
	}
	return result, err
}

func ApplyLogicalChannelSmartScheduleProbeRecovery(
	identity LogicalChannelIdentity,
	groupName string,
	modelName string,
	succeeded bool,
	resultTime int64,
	request ChannelSmartScheduleProbeRecoveryRequest,
) (result ChannelSmartScheduleProbeRecoveryResult, err error) {
	groupName, modelName, err = normalizeLogicalSmartScheduleIdentity(identity, groupName, modelName)
	if err != nil {
		return result, err
	}
	if resultTime <= 0 {
		resultTime = common.GetTimestamp()
	}
	var spec *ChannelSmartScheduleProbeRecoveryRoute
	for index := range request.Routes {
		route := request.Routes[index]
		if strings.TrimSpace(route.Group) != groupName || channelSmartScheduleModelName(route.Model) != modelName {
			continue
		}
		if route.RecoverySuccessThreshold <= 0 {
			route.RecoverySuccessThreshold = 1
		}
		if spec == nil || route.RecoverySuccessThreshold < spec.RecoverySuccessThreshold ||
			route.CooldownUntil > spec.CooldownUntil {
			copy := route
			spec = &copy
		}
	}
	if spec == nil {
		return result, nil
	}

	channelStatusLock.Lock()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockLogicalSmartScheduleIdentityTx(tx, identity); err != nil {
			return err
		}
		controlRevision, err := lockChannelSmartScheduleControlRevisionTx(tx)
		if err != nil {
			return err
		}
		if controlRevision != request.ExpectedControlRevision {
			return nil
		}
		var sampleState ChannelLogicalSmartScheduleSampleState
		if err := lockForUpdate(tx).Where(&ChannelLogicalSmartScheduleSampleState{
			LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
			GroupName: groupName, ModelName: modelName,
		}).First(&sampleState).Error; err != nil {
			return err
		}
		var stored ChannelLogicalSmartScheduleRouteState
		if err := lockForUpdate(tx).Where(&ChannelLogicalSmartScheduleRouteState{
			LogicalGroupID: identity.LogicalChannelID, LogicalRevision: identity.Revision,
			GroupName: groupName, ModelName: modelName,
		}).First(&stored).Error; err != nil {
			return err
		}
		payload, err := decodeLogicalSmartScheduleRoutePayload(stored.StateJSON)
		if err != nil {
			return err
		}
		state := payload.State
		state.Revision = stored.StateRevision
		protected := state.Participates() &&
			(state.StabilityState == ChannelSmartScheduleStabilityDegraded ||
				state.StabilityState == ChannelSmartScheduleStabilityProbing)
		result.Applied = true
		if !protected {
			sampleState.RecoverySuccessCount = 0
			sampleState.RecoverySuccessAt = 0
			return tx.Save(&sampleState).Error
		}
		if !succeeded {
			sampleState.RecoverySuccessCount = 0
			sampleState.RecoverySuccessAt = 0
			return tx.Save(&sampleState).Error
		}
		if sampleState.RecoverySuccessCount < math.MaxInt {
			sampleState.RecoverySuccessCount++
		}
		sampleState.RecoverySuccessAt = resultTime
		result.RecoverySuccessCount = sampleState.RecoverySuccessCount
		if sampleState.RecoverySuccessCount < spec.RecoverySuccessThreshold {
			return tx.Save(&sampleState).Error
		}

		priority := state.StabilitySavedPriority
		weight := state.StabilitySavedWeight
		if priority <= 0 {
			priority = state.BasePriority
		}
		if weight == 0 {
			weight = state.BaseWeight
		}
		if priority <= 0 {
			priority = payload.EffectivePriority
		}
		if weight == 0 {
			weight = payload.EffectiveWeight
		}
		if priority <= 0 {
			priority = channelSmartScheduleRuntimeFallbackPriority
		}
		if weight == 0 {
			weight = channelSmartScheduleRuntimeFallbackWeight
		}
		state.StabilityState = ""
		state.StabilityUntil = 0
		state.StabilitySince = 0
		state.StabilitySavedPriority = 0
		state.StabilitySavedWeight = 0
		state.RuntimeProtectionUntil = 0
		state.StabilityReleaseMaxPromptTokens = 0
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "恢复探测连续成功达到阈值，已立即解除稳定性保护"
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = priority
		state.LastScheduleWeight = weight
		state.LastScheduleTime = resultTime
		state.Revision++

		result.RoutingChanged = payload.EffectivePriority != priority || payload.EffectiveWeight != weight
		result.Recovered = append(result.Recovered, channelSmartScheduleRouteKey(
			identity.ChannelID, groupName, modelName,
		))
		raw, err := encodeLogicalSmartScheduleRouteStateWithRouting(state, priority, weight)
		if err != nil {
			return err
		}
		updated := tx.Model(&ChannelLogicalSmartScheduleRouteState{}).Where(
			"id = ? AND state_revision = ?", stored.Id, stored.StateRevision,
		).Updates(map[string]any{
			"state_revision": state.Revision, "state_json": raw, "updated_at": resultTime,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelLogicalGroupRevisionConflict
		}
		sampleState.RecoverySuccessCount = 0
		sampleState.RecoverySuccessAt = 0
		sampleState.ObservationSince = max(sampleState.ObservationSince, resultTime)
		result.RecoverySuccessCount = 0
		result.ObservationSince = sampleState.ObservationSince
		return tx.Save(&sampleState).Error
	})
	channelStatusLock.Unlock()
	if err != nil || succeeded || !result.Applied {
		return result, err
	}
	reason := strings.TrimSpace(request.FailureReason)
	if reason == "" {
		reason = "恢复探测失败，已清零连续成功次数并续满稳定性保护"
	}
	if runes := []rune(reason); len(runes) > 255 {
		reason = string(runes[:255])
	}
	protected, protectErr := protectLogicalChannelSmartScheduleRouteOnRuntimeFailure(
		identity, groupName, modelName, spec.CooldownUntil, reason,
		request.ExpectedControlRevision, false, true, 0,
	)
	if protectErr != nil {
		return result, protectErr
	}
	result.RoutingChanged = protected.RoutingChanged
	if protected.Handled {
		result.Renewed = append(result.Renewed, channelSmartScheduleRouteKey(
			identity.ChannelID, groupName, modelName,
		))
	}
	return result, nil
}

// CoalesceChannelSmartScheduleSchedulingRoutes is used only by scheduler
// execution. Management APIs keep their original physical route rows.
func CoalesceChannelSmartScheduleSchedulingRoutes(
	routes []ChannelSmartScheduleRoute,
) ([]ChannelSmartScheduleRoute, error) {
	if len(routes) < 2 || !IsLogicalChannelGroupingEnabled() {
		return routes, nil
	}
	channelIDs := make([]int, 0, len(routes))
	for _, route := range routes {
		channelIDs = append(channelIDs, route.ChannelId)
	}
	runtime, err := loadChannelSmartScheduleLogicalRuntime(channelIDs)
	if err != nil || runtime == nil {
		return routes, err
	}
	result := make([]ChannelSmartScheduleRoute, 0, len(routes))
	type logicalRouteKey struct {
		logicalID int64
		revision  int64
		group     string
		model     string
	}
	indexes := make(map[logicalRouteKey]int)
	for _, route := range routes {
		identity, exists := runtime.Channels[route.ChannelId]
		group, grouped := runtime.Groups[identity.LogicalChannelID]
		if !exists || identity.Revision <= 0 || !grouped || !IsLogicalChannelGroupActive(group.Status) {
			result = append(result, route)
			continue
		}
		key := logicalRouteKey{identity.LogicalChannelID, identity.Revision, route.Group, channelSmartScheduleModelName(route.Model)}
		index, found := indexes[key]
		if !found {
			index = len(result)
			indexes[key] = index
			logicalRoute := route
			logicalRoute.LogicalChannelId = identity.LogicalChannelID
			logicalRoute.LogicalRevision = identity.Revision
			logicalRoute.LogicalMemberIds = []int{route.ChannelId}
			result = append(result, logicalRoute)
			continue
		}
		logicalRoute := &result[index]
		logicalRoute.LogicalMemberIds = append(logicalRoute.LogicalMemberIds, route.ChannelId)
		if route.ChannelStatus == common.ChannelStatusEnabled {
			logicalRoute.ChannelStatus = common.ChannelStatusEnabled
		}
		logicalRoute.Enabled = logicalRoute.Enabled || route.Enabled
		if route.TrafficPausedUntil == 0 || logicalRoute.TrafficPausedUntil == 0 {
			logicalRoute.TrafficPausedUntil = 0
		} else {
			logicalRoute.TrafficPausedUntil = min(logicalRoute.TrafficPausedUntil, route.TrafficPausedUntil)
		}
		if route.Priority > logicalRoute.Priority {
			logicalRoute.Priority = route.Priority
		}
		if route.Weight > logicalRoute.Weight {
			logicalRoute.Weight = route.Weight
		}
	}
	for index := range result {
		route := &result[index]
		if route.LogicalChannelId <= 0 {
			continue
		}
		sort.Ints(route.LogicalMemberIds)
		identity := LogicalChannelIdentity{
			ChannelID: route.ChannelId, LogicalChannelID: route.LogicalChannelId, Revision: route.LogicalRevision,
		}
		state, priority, weight, err := loadOrCreateLogicalSmartScheduleRouteState(
			identity, route.Group, route.Model, route.State, route.Priority, route.Weight,
		)
		if err != nil {
			return nil, err
		}
		samples, err := loadLogicalSmartScheduleSampleView(identity, route.Group, route.Model, route.ChannelId)
		if err != nil {
			return nil, err
		}
		route.State = state
		route.Priority = priority
		route.Weight = weight
		route.SharedSamples = samples
	}
	return result, nil
}
