package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelMonitorRedisEffectState stores low-cardinality sequence watermarks
// for Redis-triggered effects that commit outside the Redis Stream ACK.
type ChannelMonitorRedisEffectState struct {
	EffectKey     string `gorm:"type:char(64);primaryKey"`
	EventSequence int64  `gorm:"bigint;not null"`
	UpdatedAt     int64  `gorm:"bigint;not null"`
}

func lockChannelMonitorRedisEffectStateTx(
	tx *gorm.DB,
	effectKey string,
) (*ChannelMonitorRedisEffectState, error) {
	effectKey = strings.TrimSpace(effectKey)
	if effectKey == "" {
		return nil, errors.New("渠道监控 Redis 副作用水位键不能为空")
	}
	initial := &ChannelMonitorRedisEffectState{EffectKey: effectKey}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(initial).Error; err != nil {
		return nil, err
	}
	var state ChannelMonitorRedisEffectState
	if err := lockForUpdate(tx).Where("effect_key = ?", effectKey).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func advanceChannelMonitorRedisEffectStateTx(
	tx *gorm.DB,
	state *ChannelMonitorRedisEffectState,
	eventSequence int64,
) error {
	if state == nil || eventSequence <= state.EventSequence {
		return nil
	}
	previousSequence := state.EventSequence
	updatedAt := common.GetTimestamp()
	result := tx.Model(&ChannelMonitorRedisEffectState{}).
		Where("effect_key = ? AND event_sequence = ?", state.EffectKey, previousSequence).
		Updates(map[string]any{
			"event_sequence": eventSequence,
			"updated_at":     updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errSystemTaskStateChanged
	}
	state.EventSequence = eventSequence
	state.UpdatedAt = updatedAt
	return nil
}

func channelMonitorRedisEffectKey(kind string, parts ...string) string {
	var builder strings.Builder
	builder.WriteString(kind)
	for _, part := range parts {
		builder.WriteByte(':')
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
	}
	digest := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}

func channelMonitorRedisProtectionEffectKey(channelID int, groupName, modelName string) string {
	return channelMonitorRedisEffectKey(
		"runtime_protection",
		strconv.Itoa(channelID),
		groupName,
		modelName,
	)
}

func channelMonitorRedisLogicalProtectionEffectKey(
	logicalID int64,
	revision int64,
	groupName string,
	modelName string,
) string {
	return channelMonitorRedisEffectKey(
		"logical_runtime_protection",
		strconv.FormatInt(logicalID, 10),
		strconv.FormatInt(revision, 10),
		groupName,
		modelName,
	)
}

func channelMonitorRedisAdaptiveEffectKey(groupName, modelName string) string {
	return channelMonitorRedisEffectKey("adaptive_refresh", groupName, modelName)
}

// EnqueueRequiredSystemTaskAfterRedisSequence atomically advances the
// scheduling watermark with the task insert or pending-payload merge.
func EnqueueRequiredSystemTaskAfterRedisSequence(
	taskType string,
	payload any,
	eventSequence int64,
) (*SystemTask, bool, bool, error) {
	if eventSequence <= 0 {
		return nil, false, false, errors.New("渠道监控 Redis 调度事件顺序无效")
	}
	basePayloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, false, false, err
	}

	var lastErr error
	for range 5 {
		var queuedTask *SystemTask
		created := false
		applied := false
		err = DB.Transaction(func(tx *gorm.DB) error {
			state, stateErr := lockChannelMonitorRedisEffectStateTx(
				tx,
				channelMonitorRedisEffectKey("schedule", "channel_smart_schedule"),
			)
			if stateErr != nil {
				return stateErr
			}
			if eventSequence <= state.EventSequence {
				return nil
			}
			queuedTask, created, stateErr = enqueueRequiredSystemTaskTx(tx, taskType, payload, basePayloadText)
			if stateErr != nil {
				return stateErr
			}
			if stateErr = advanceChannelMonitorRedisEffectStateTx(tx, state, eventSequence); stateErr != nil {
				return stateErr
			}
			applied = true
			return nil
		})
		if err == nil {
			return queuedTask, created, applied, nil
		}
		if errors.Is(err, errSystemTaskStateChanged) {
			continue
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, false, false, lastErr
	}
	return nil, false, false, errSystemTaskStateChanged
}
