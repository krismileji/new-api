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
	InitialQuota       int64  `gorm:"not null"`
	InitialCostNanoCNY int64  `gorm:"not null"`
	CostNanoCNY        int64  `gorm:"not null"`
	CreatedAt          int64  `gorm:"not null"`
	UpdatedAt          int64  `gorm:"not null"`
}

type ChannelTaskCostEventInput struct {
	CostEventId    string
	ChannelId      int
	OccurredAt     int64
	APIKeyId       int
	APIKeyName     string
	KeyFingerprint string
	KeyDisplay     string
	InitialQuota   int64
	CostNanoCNY    int64
}

// RegisterChannelTaskCostEvent atomically creates the task cost state and
// applies its initial value to both channel and API-key daily totals. Replaying
// the same immutable event is a no-op and returns its current corrected cost.
func RegisterChannelTaskCostEvent(ctx context.Context, input ChannelTaskCostEventInput) (int64, error) {
	input.CostEventId = strings.TrimSpace(input.CostEventId)
	input.APIKeyName = strings.TrimSpace(input.APIKeyName)
	if err := validateChannelTaskCostEventInput(input); err != nil {
		return 0, err
	}

	var currentCost int64
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
			if err := addChannelDailyCost(tx, input.ChannelId, input.OccurredAt, input.CostNanoCNY, 0, 1, 0); err != nil {
				return err
			}
			if input.KeyFingerprint != "" {
				if err := addChannelDailyAPIKeyCost(tx, input.ChannelId, input.OccurredAt, input.CostNanoCNY, 1, 0, input.APIKeyId, input.APIKeyName, input.KeyFingerprint, input.KeyDisplay); err != nil {
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
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event ChannelTaskCostEvent
		if err := lockForUpdate(tx).Where("cost_event_id = ?", costEventId).First(&event).Error; err != nil {
			return err
		}
		var err error
		targetCost, err = target(event)
		if err != nil {
			return err
		}
		if targetCost < 0 {
			return errors.New("task cost must not be negative")
		}
		if targetCost == event.CostNanoCNY {
			return nil
		}
		if err := replaceChannelDailyCostValue(tx, event, targetCost, updatedAt); err != nil {
			return err
		}
		result := tx.Model(&ChannelTaskCostEvent{}).
			Where("id = ? AND cost_nano_cny = ?", event.Id, event.CostNanoCNY).
			Updates(map[string]any{"cost_nano_cny": targetCost, "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("task cost event changed concurrently")
		}
		return nil
	})
	return targetCost, err
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

	if event.KeyFingerprint == "" {
		return nil
	}
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
		event.InitialCostNanoCNY == input.CostNanoCNY
}
