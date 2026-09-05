package model

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelTaskCostEvent keeps the authoritative cost state for one asynchronous
// task. InitialCostNanoCNY and InitialQuota are immutable settlement inputs;
// CostNanoCNY is the latest corrected value reflected in the daily totals.
type ChannelTaskCostEvent struct {
	Id                 int64  `gorm:"primaryKey"`
	CostEventId        string `gorm:"type:varchar(128);not null;uniqueIndex"`
	RegistrationToken  string `gorm:"type:varchar(36);not null"`
	ChannelId          int    `gorm:"not null;index"`
	DayStart           int64  `gorm:"not null;index"`
	OccurredAt         int64  `gorm:"not null"`
	APIKeyId           int    `gorm:"not null;default:0"`
	APIKeyName         string `gorm:"size:255;not null;default:''"`
	KeyFingerprint     string `gorm:"size:64;not null;default:''"`
	KeyDisplay         string `gorm:"size:64;not null;default:''"`
	UserId             int    `gorm:"not null;default:0"`
	UserAttribution    string `gorm:"size:16;not null;default:''"`
	ModelName          string `gorm:"size:255;not null;default:''"`
	InitialQuota       int64  `gorm:"not null"`
	InitialCostNanoCNY int64  `gorm:"not null"`
	CostNanoCNY        int64  `gorm:"not null"`
	CreatedAt          int64  `gorm:"not null"`
	UpdatedAt          int64  `gorm:"not null"`
}

type ChannelTaskCostEventInput struct {
	CostEventId     string
	ChannelId       int
	OccurredAt      int64
	APIKeyId        int
	APIKeyName      string
	KeyFingerprint  string
	KeyDisplay      string
	UserId          int
	UserAttribution string
	ModelName       string
	InitialQuota    int64
	CostNanoCNY     int64
}

// RegisterChannelTaskCostEvent atomically creates the task cost state and
// applies its initial value to both channel and API-key daily totals. Replaying
// the same immutable event is a no-op and returns its current corrected cost.
func RegisterChannelTaskCostEvent(ctx context.Context, input ChannelTaskCostEventInput) (int64, error) {
	if DB == nil {
		return 0, errors.New("task cost event database is unavailable")
	}
	input.CostEventId = strings.TrimSpace(input.CostEventId)
	input.APIKeyName = strings.TrimSpace(input.APIKeyName)
	input.UserAttribution = strings.TrimSpace(input.UserAttribution)
	input.ModelName = strings.TrimSpace(input.ModelName)
	if err := validateChannelTaskCostEventInput(input); err != nil {
		return 0, err
	}

	var currentCost int64
	err := withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		registrationToken := uuid.NewString()
		record := ChannelTaskCostEvent{
			CostEventId:        input.CostEventId,
			RegistrationToken:  registrationToken,
			ChannelId:          input.ChannelId,
			DayStart:           ChannelDailyCostDayStart(input.OccurredAt),
			OccurredAt:         input.OccurredAt,
			APIKeyId:           input.APIKeyId,
			APIKeyName:         input.APIKeyName,
			KeyFingerprint:     input.KeyFingerprint,
			KeyDisplay:         input.KeyDisplay,
			UserId:             input.UserId,
			UserAttribution:    input.UserAttribution,
			ModelName:          input.ModelName,
			InitialQuota:       input.InitialQuota,
			InitialCostNanoCNY: input.CostNanoCNY,
			CostNanoCNY:        input.CostNanoCNY,
			CreatedAt:          input.OccurredAt,
			UpdatedAt:          input.OccurredAt,
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if result.Error != nil {
			return result.Error
		}

		var existing ChannelTaskCostEvent
		if err := lockForUpdate(tx).Where("cost_event_id = ?", input.CostEventId).First(&existing).Error; err != nil {
			return err
		}
		if existing.RegistrationToken == registrationToken {
			if err := addChannelDailyCost(tx, input.ChannelId, input.OccurredAt, input.CostNanoCNY, 0, 0, 1, 0); err != nil {
				return err
			}
			if input.KeyFingerprint != "" {
				if err := addChannelDailyAPIKeyCost(tx, input.ChannelId, input.OccurredAt, input.CostNanoCNY, 1, 0, input.APIKeyId, input.APIKeyName, input.KeyFingerprint, input.KeyDisplay); err != nil {
					return err
				}
			}
			if tx.Migrator().HasTable(&ChannelMonitorDailyCostDetail{}) {
				if err := addChannelMonitorDailyCostDetail(tx, ChannelDailyCostDelta{
					ChannelId: input.ChannelId, OccurredAt: input.OccurredAt, CostNanoCNY: input.CostNanoCNY,
					SettledDelta: 1, APIKeyId: input.APIKeyId, APIKeyName: input.APIKeyName,
					KeyFingerprint: input.KeyFingerprint, KeyDisplay: input.KeyDisplay, UserId: input.UserId,
					UserAttribution: input.UserAttribution, ModelName: input.ModelName, SourceKind: "business",
				}); err != nil {
					return err
				}
			}
			currentCost = input.CostNanoCNY
			return nil
		}

		if !channelTaskCostEventMatchesInput(existing, input) {
			return errors.New("task cost event id was reused with different immutable data")
		}
		currentCost = existing.CostNanoCNY
		return nil
	})
	return currentCost, err
}

// SetChannelTaskCostEventCost atomically replaces one task's cost in the
// original submission day. The daily settled request count remains unchanged.
func SetChannelTaskCostEventCost(ctx context.Context, costEventId string, costNanoCNY int64, updatedAt int64) (int64, error) {
	if costNanoCNY < 0 {
		return 0, errors.New("task cost must not be negative")
	}
	return updateChannelTaskCostEvent(ctx, costEventId, updatedAt, func(ChannelTaskCostEvent) (int64, error) {
		return costNanoCNY, nil
	})
}

// SetChannelTaskCostEventQuota derives an absolute target from the immutable
// initial cost and initial billed quota. Replaying the same actual quota is
// therefore idempotent even after earlier corrections.
func SetChannelTaskCostEventQuota(ctx context.Context, costEventId string, actualQuota int64, updatedAt int64) (int64, error) {
	if actualQuota < 0 {
		return 0, errors.New("task quota must not be negative")
	}
	return updateChannelTaskCostEvent(ctx, costEventId, updatedAt, func(event ChannelTaskCostEvent) (int64, error) {
		if event.InitialQuota <= 0 {
			return 0, errors.New("task cost event initial quota must be positive for quota correction")
		}
		cost := decimal.NewFromInt(event.InitialCostNanoCNY).
			Mul(decimal.NewFromInt(actualQuota)).
			Div(decimal.NewFromInt(event.InitialQuota)).
			Round(0)
		if cost.IsNegative() || cost.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
			return 0, errors.New("corrected task cost exceeds int64")
		}
		return cost.IntPart(), nil
	})
}

func updateChannelTaskCostEvent(ctx context.Context, costEventId string, updatedAt int64, target func(ChannelTaskCostEvent) (int64, error)) (int64, error) {
	costEventId = strings.TrimSpace(costEventId)
	if costEventId == "" || len(costEventId) > ChannelMonitorEventMaxIdentityLength {
		return 0, errors.New("task cost event id must contain at most 128 bytes")
	}
	if updatedAt <= 0 {
		return 0, errors.New("task cost update timestamp must be positive")
	}

	var targetCost int64
	err := withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		targetCost, err = updateChannelTaskCostEventTx(tx, costEventId, updatedAt, target)
		return err
	})
	return targetCost, err
}

// updateChannelTaskCostEventTx applies a cost correction inside an existing
// transaction. The event's updated_at is a monotonic version: an older
// correction is rejected, while an equal timestamp is accepted for retries.
// Keeping this helper transaction-scoped lets task billing update the task,
// account balances, and channel cost event atomically.
func updateChannelTaskCostEventTx(tx *gorm.DB, costEventId string, updatedAt int64, target func(ChannelTaskCostEvent) (int64, error)) (int64, error) {
	costEventId = strings.TrimSpace(costEventId)
	if costEventId == "" || len(costEventId) > ChannelMonitorEventMaxIdentityLength {
		return 0, errors.New("task cost event id must contain at most 128 bytes")
	}
	if updatedAt <= 0 {
		return 0, errors.New("task cost update timestamp must be positive")
	}
	var event ChannelTaskCostEvent
	if err := lockForUpdate(tx).Where("cost_event_id = ?", costEventId).First(&event).Error; err != nil {
		return 0, err
	}
	if updatedAt < event.UpdatedAt {
		return event.CostNanoCNY, errors.New("task cost event update is stale")
	}
	targetCost, err := target(event)
	if err != nil {
		return 0, err
	}
	if targetCost < 0 {
		return 0, errors.New("task cost must not be negative")
	}
	if targetCost == event.CostNanoCNY {
		if updatedAt > event.UpdatedAt {
			result := tx.Model(&ChannelTaskCostEvent{}).
				Where("id = ? AND updated_at <= ?", event.Id, updatedAt).
				Update("updated_at", updatedAt)
			if result.Error != nil {
				return 0, result.Error
			}
			if result.RowsAffected != 1 {
				return 0, errors.New("task cost event changed concurrently")
			}
		}
		return targetCost, nil
	}
	if err := replaceChannelDailyCostValue(tx, event, targetCost, updatedAt); err != nil {
		return 0, err
	}
	result := tx.Model(&ChannelTaskCostEvent{}).
		Where("id = ? AND cost_nano_cny = ? AND updated_at <= ?", event.Id, event.CostNanoCNY, updatedAt).
		Updates(map[string]any{"cost_nano_cny": targetCost, "updated_at": updatedAt})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, errors.New("task cost event changed concurrently")
	}
	return targetCost, nil
}

func replaceChannelDailyCostValue(tx *gorm.DB, event ChannelTaskCostEvent, targetCost int64, updatedAt int64) error {
	var total ChannelDailyCost
	if err := lockForUpdate(tx).
		Where("channel_id = ? AND day_start = ?", event.ChannelId, event.DayStart).
		First(&total).Error; err != nil {
		return err
	}
	newTotal, err := replaceCostComponent(total.CostNanoCNY, event.CostNanoCNY, targetCost)
	if err != nil {
		return err
	}
	result := tx.Model(&ChannelDailyCost{}).
		Where("id = ? AND cost_nano_cny = ?", total.Id, total.CostNanoCNY).
		Updates(map[string]any{"cost_nano_cny": newTotal, "updated_at": updatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("channel daily cost changed concurrently")
	}

	if event.KeyFingerprint != "" {
		var keyCost ChannelDailyAPIKeyCost
		if err := lockForUpdate(tx).
			Where("channel_id = ? AND day_start = ? AND key_fingerprint = ?", event.ChannelId, event.DayStart, event.KeyFingerprint).
			First(&keyCost).Error; err != nil {
			return err
		}
		newKeyCost, err := replaceCostComponent(keyCost.CostNanoCNY, event.CostNanoCNY, targetCost)
		if err != nil {
			return err
		}
		result = tx.Model(&ChannelDailyAPIKeyCost{}).
			Where("id = ? AND cost_nano_cny = ?", keyCost.Id, keyCost.CostNanoCNY).
			Updates(map[string]any{"cost_nano_cny": newKeyCost, "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("channel daily API key cost changed concurrently")
		}
	}
	if !tx.Migrator().HasTable(&ChannelMonitorDailyCostDetail{}) {
		return nil
	}
	var detail ChannelMonitorDailyCostDetail
	if err := lockForUpdate(tx).Where("day_start = ? AND channel_id = ? AND user_id = ? AND api_key_id = ? AND api_key_key = ? AND model_key = ? AND source_kind = ?",
		event.DayStart, event.ChannelId, event.UserId, event.APIKeyId, event.KeyFingerprint, ChannelMonitorDailyCostModelKey(event.ModelName), "business").First(&detail).Error; err == nil {
		newDetailCost, detailErr := replaceCostComponent(detail.CostNanoCNY, event.CostNanoCNY, targetCost)
		if detailErr != nil {
			return detailErr
		}
		if err := tx.Model(&ChannelMonitorDailyCostDetail{}).Where("id = ? AND cost_nano_cny = ?", detail.Id, detail.CostNanoCNY).Updates(map[string]any{"cost_nano_cny": newDetailCost, "updated_at": updatedAt}).Error; err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func replaceCostComponent(total int64, previous int64, next int64) (int64, error) {
	if total < 0 || previous < 0 || next < 0 || total < previous {
		return 0, errors.New("daily cost cannot cover the task cost correction")
	}
	remaining := total - previous
	if next > math.MaxInt64-remaining {
		return 0, errors.New("corrected daily cost exceeds int64")
	}
	return remaining + next, nil
}

func validateChannelTaskCostEventInput(input ChannelTaskCostEventInput) error {
	if input.CostEventId == "" || len(input.CostEventId) > ChannelMonitorEventMaxIdentityLength {
		return errors.New("task cost event id must contain at most 128 bytes")
	}
	if input.ChannelId <= 0 {
		return errors.New("channel id must be positive")
	}
	if input.OccurredAt <= 0 {
		return errors.New("task cost occurrence timestamp must be positive")
	}
	if input.APIKeyId < 0 || len(input.APIKeyName) > ChannelMonitorEventMaxNameLength {
		return errors.New("task cost API key metadata is invalid")
	}
	if input.UserId < 0 || (input.UserAttribution != "" && input.UserAttribution != string(ChannelMonitorEventUserAttributionRequest) && input.UserAttribution != string(ChannelMonitorEventUserAttributionUnknown)) || len(input.ModelName) > ChannelMonitorEventMaxNameLength {
		return errors.New("task cost attribution metadata is invalid")
	}
	if input.CostNanoCNY < 0 || input.InitialQuota < 0 {
		return errors.New("task cost and quota must not be negative")
	}
	if input.KeyFingerprint != "" {
		if len(input.KeyFingerprint) != 64 || len(input.KeyDisplay) > 64 {
			return errors.New("task cost API key identity is invalid")
		}
		if _, err := hex.DecodeString(input.KeyFingerprint); err != nil {
			return errors.New("task cost API key identity is invalid")
		}
	}
	return nil
}

func channelTaskCostEventMatchesInput(event ChannelTaskCostEvent, input ChannelTaskCostEventInput) bool {
	return event.ChannelId == input.ChannelId &&
		event.DayStart == ChannelDailyCostDayStart(input.OccurredAt) &&
		event.OccurredAt == input.OccurredAt &&
		event.APIKeyId == input.APIKeyId &&
		event.APIKeyName == input.APIKeyName &&
		event.KeyFingerprint == input.KeyFingerprint &&
		event.KeyDisplay == input.KeyDisplay &&
		event.InitialQuota == input.InitialQuota &&
		event.InitialCostNanoCNY == input.CostNanoCNY &&
		event.UserId == input.UserId && event.UserAttribution == input.UserAttribution && event.ModelName == input.ModelName
}
