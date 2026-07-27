package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	ChannelRatioFetchStatusSucceeded                   = "succeeded"
	ChannelRatioFetchStatusFailed                      = "failed"
	ChannelSmartScheduleStatusSucceeded                = "succeeded"
	ChannelSmartScheduleStatusSkipped                  = "skipped"
	ChannelSmartScheduleStatusFailed                   = "failed"
	ChannelSmartScheduleStabilityDegraded              = "degraded"
	ChannelSmartScheduleStabilityProbing               = "probing"
	ChannelSmartScheduleControlRevisionOption          = "ChannelMonitorSmartScheduleControlRevision"
	ChannelSmartScheduleParticipationInitializedOption = "ChannelMonitorSmartScheduleParticipationInitialized"
)

type ChannelRatioMonitor struct {
	Id                            int      `json:"id"`
	ChannelId                     int      `json:"channel_id" gorm:"uniqueIndex;not null"`
	Ratio                         float64  `json:"ratio" gorm:"not null"`
	PreviousRatio                 *float64 `json:"previous_ratio"`
	Remark                        string   `json:"remark" gorm:"type:varchar(255);default:''"`
	UpdatedTime                   int64    `json:"updated_time" gorm:"bigint;index"`
	UpdatedBy                     int      `json:"updated_by" gorm:"index"`
	UpdatedByUsername             string   `json:"updated_by_username" gorm:"type:varchar(64);default:''"`
	LastFetchStatus               string   `json:"last_fetch_status" gorm:"type:varchar(16);index"`
	LastFetchError                string   `json:"last_fetch_error" gorm:"type:varchar(255)"`
	LastFetchTime                 int64    `json:"last_fetch_time" gorm:"bigint;index"`
	ConsecutiveFailures           int      `json:"consecutive_failures"`
	UpstreamBalance               *float64 `json:"upstream_balance"`
	LastBalanceTime               int64    `json:"last_balance_time" gorm:"bigint"`
	LastBalanceError              string   `json:"last_balance_error" gorm:"type:varchar(255)"`
	BalanceConsecutiveFailures    int      `json:"balance_consecutive_failures"`
	BalanceWarningThreshold       *float64 `json:"balance_warning_threshold"`
	BalanceAutoDisableThreshold   *float64 `json:"balance_auto_disable_threshold"`
	BalanceAlertNotified          bool     `json:"balance_alert_notified"`
	UpstreamType                  string   `json:"upstream_type" gorm:"type:varchar(32)"`
	UpstreamBaseURL               string   `json:"upstream_base_url" gorm:"type:text"`
	UpstreamGroup                 string   `json:"upstream_group" gorm:"type:varchar(64)"`
	UpstreamAuthType              string   `json:"upstream_auth_type" gorm:"type:varchar(16)"`
	UpstreamUserId                int      `json:"upstream_user_id"`
	UpstreamAccessToken           string   `json:"-" gorm:"type:text"`
	UpstreamAccount               string   `json:"-" gorm:"type:varchar(320)"`
	UpstreamPassword              string   `json:"-" gorm:"type:text"`
	CostConversion                string   `json:"-" gorm:"type:text"`
	CustomUpstreamConfig          string   `json:"-" gorm:"type:text"`
	UpstreamRatioSyncDisabled     bool     `json:"-"`
	UpstreamBalanceSyncDisabled   bool     `json:"-"`
	SingleChannelAction           string   `json:"single_channel_action" gorm:"type:varchar(32)"`
	MultipleChannelsAction        string   `json:"multiple_channels_action" gorm:"type:varchar(32)"`
	SmartScheduleParticipationSet bool     `json:"smart_schedule_participation_set"`
	SmartScheduleExcluded         bool     `json:"smart_schedule_excluded"`
	SmartScheduleRevision         int64    `json:"-" gorm:"bigint"`
	LastScheduleStatus            string   `json:"last_schedule_status" gorm:"type:varchar(16);index"`
	LastScheduleError             string   `json:"last_schedule_error" gorm:"type:varchar(255)"`
	LastScheduleScore             *float64 `json:"last_schedule_score"`
	LastSchedulePriority          int64    `json:"last_schedule_priority" gorm:"bigint"`
	LastScheduleWeight            uint     `json:"last_schedule_weight"`
	LastScheduleTime              int64    `json:"last_schedule_time" gorm:"bigint;index"`
	SmartScheduleStabilityState   string   `json:"smart_schedule_stability_state" gorm:"type:varchar(16);index"`
	SmartScheduleStabilityUntil   int64    `json:"smart_schedule_stability_until" gorm:"bigint;index"`
	SmartScheduleStabilitySince   int64    `json:"smart_schedule_stability_since" gorm:"bigint"`
	SmartScheduleSavedPriority    int64    `json:"smart_schedule_saved_priority" gorm:"bigint"`
	SmartScheduleSavedWeight      uint     `json:"smart_schedule_saved_weight"`
	ConcurrencyLimit              int      `json:"concurrency_limit"`
	ConcurrencyRevision           int64    `json:"-" gorm:"bigint"`
}

type ChannelConcurrencyConfig struct {
	Limit    int
	Revision int64
}

type ChannelRatioUpstreamOptions struct {
	SingleChannelAction         string
	MultipleChannelsAction      string
	BalanceWarningThreshold     *float64
	BalanceAutoDisableThreshold *float64
	RatioSyncEnabled            bool
	BalanceSyncEnabled          bool
	CostConversion              string
	CustomUpstreamConfig        string
	UpstreamAccount             string
	UpstreamPassword            string
}

type ChannelSmartScheduleConfigOptions struct {
	Excluded bool
}

type ChannelSmartScheduleResultUpdate struct {
	ChannelId               int
	Status                  string
	Error                   string
	Score                   *float64
	Priority                int64
	Weight                  uint
	Time                    int64
	Stability               *ChannelSmartScheduleStabilityUpdate
	GuardCurrent            bool
	ExpectedRevision        int64
	ExpectedControlRevision string
	ExpectedPriority        int64
	ExpectedWeight          uint
	ApplyPriorityWeight     bool
}

type ChannelSmartScheduleApplyOutcome struct {
	ChannelId      int
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

func (monitor ChannelRatioMonitor) ParticipatesInSmartSchedule() bool {
	return monitor.SmartScheduleParticipationSet && !monitor.SmartScheduleExcluded
}

type ChannelRatioHistory struct {
	Id               int     `json:"id"`
	ChannelId        int     `json:"channel_id" gorm:"index;not null"`
	OldRatio         float64 `json:"old_ratio" gorm:"not null"`
	NewRatio         float64 `json:"new_ratio" gorm:"not null"`
	Remark           string  `json:"remark" gorm:"type:varchar(255);default:''"`
	CreatedTime      int64   `json:"created_time" gorm:"bigint;index"`
	OperatorId       int     `json:"operator_id" gorm:"index"`
	OperatorUsername string  `json:"operator_username" gorm:"type:varchar(64);default:''"`
}

func GetAllChannelsForMonitor() ([]*Channel, error) {
	var channels []*Channel
	err := resolveChannelSortOptions(false, nil).Apply(DB).
		Omit("key").
		Find(&channels).Error
	return channels, err
}

func GetChannelRatioMonitors() ([]ChannelRatioMonitor, error) {
	var monitors []ChannelRatioMonitor
	err := DB.Find(&monitors).Error
	return monitors, err
}

func GetChannelRatioMonitorCostMetadata() ([]ChannelRatioMonitor, error) {
	var monitors []ChannelRatioMonitor
	err := DB.Select("channel_id", "ratio", "cost_conversion", "updated_time").Find(&monitors).Error
	return monitors, err
}

func GetChannelRatioMonitor(channelId int) (ChannelRatioMonitor, error) {
	var monitor ChannelRatioMonitor
	err := DB.Where("channel_id = ?", channelId).First(&monitor).Error
	return monitor, err
}

func GetChannelConcurrencyConfigs() (map[int]ChannelConcurrencyConfig, error) {
	var monitors []ChannelRatioMonitor
	err := DB.Select("channel_id", "concurrency_limit", "concurrency_revision").
		Where("concurrency_limit > ? OR concurrency_revision > ?", 0, 0).
		Find(&monitors).Error
	if err != nil {
		return nil, err
	}
	configs := make(map[int]ChannelConcurrencyConfig, len(monitors))
	for _, monitor := range monitors {
		configs[monitor.ChannelId] = ChannelConcurrencyConfig{
			Limit:    monitor.ConcurrencyLimit,
			Revision: monitor.ConcurrencyRevision,
		}
	}
	return configs, nil
}

func SaveChannelConcurrencyLimit(channelId int, limit int) (monitor ChannelRatioMonitor, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}
		if monitor.ConcurrencyRevision == math.MaxInt64 {
			return errors.New("渠道并发配置修订号已达上限")
		}
		monitor.ConcurrencyLimit = limit
		monitor.ConcurrencyRevision++
		return tx.Save(&monitor).Error
	})
	return monitor, err
}

func SaveChannelRatioUpstreamConfig(channelId int, upstreamType string, baseURL string, group string, authType string, userId int, accessToken string, options ChannelRatioUpstreamOptions) (monitor ChannelRatioMonitor, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}
		upstreamAccountChanged := monitor.UpstreamType != upstreamType ||
			monitor.UpstreamBaseURL != baseURL ||
			monitor.UpstreamAuthType != authType ||
			monitor.UpstreamUserId != userId ||
			monitor.UpstreamAccessToken != accessToken ||
			monitor.UpstreamAccount != options.UpstreamAccount ||
			monitor.UpstreamPassword != options.UpstreamPassword ||
			monitor.CustomUpstreamConfig != options.CustomUpstreamConfig
		balanceWarningThresholdChanged :=
			(monitor.BalanceWarningThreshold == nil) != (options.BalanceWarningThreshold == nil) ||
				(monitor.BalanceWarningThreshold != nil && options.BalanceWarningThreshold != nil &&
					*monitor.BalanceWarningThreshold != *options.BalanceWarningThreshold)
		ratioSyncChanged := monitor.UpstreamRatioSyncDisabled != !options.RatioSyncEnabled
		balanceSyncChanged := monitor.UpstreamBalanceSyncDisabled != !options.BalanceSyncEnabled
		ratioRequestChanged := upstreamAccountChanged ||
			monitor.UpstreamGroup != group ||
			monitor.CostConversion != options.CostConversion ||
			ratioSyncChanged
		balanceRequestChanged := upstreamAccountChanged || balanceSyncChanged

		monitor.UpstreamType = upstreamType
		monitor.UpstreamBaseURL = baseURL
		monitor.UpstreamGroup = group
		monitor.UpstreamAuthType = authType
		monitor.UpstreamUserId = userId
		monitor.UpstreamAccessToken = accessToken
		monitor.UpstreamAccount = options.UpstreamAccount
		monitor.UpstreamPassword = options.UpstreamPassword
		monitor.CostConversion = options.CostConversion
		monitor.CustomUpstreamConfig = options.CustomUpstreamConfig
		monitor.UpstreamRatioSyncDisabled = !options.RatioSyncEnabled
		monitor.UpstreamBalanceSyncDisabled = !options.BalanceSyncEnabled
		monitor.SingleChannelAction = options.SingleChannelAction
		monitor.MultipleChannelsAction = options.MultipleChannelsAction
		if options.BalanceWarningThreshold == nil {
			monitor.BalanceWarningThreshold = nil
		} else {
			value := *options.BalanceWarningThreshold
			monitor.BalanceWarningThreshold = &value
		}
		if options.BalanceAutoDisableThreshold == nil {
			monitor.BalanceAutoDisableThreshold = nil
		} else {
			value := *options.BalanceAutoDisableThreshold
			monitor.BalanceAutoDisableThreshold = &value
		}
		if upstreamAccountChanged {
			monitor.UpstreamBalance = nil
			monitor.LastBalanceTime = 0
		}
		if ratioRequestChanged {
			monitor.ConsecutiveFailures = 0
			if monitor.LastFetchStatus == ChannelRatioFetchStatusFailed {
				monitor.LastFetchStatus = ""
				monitor.LastFetchError = ""
				monitor.LastFetchTime = 0
			}
		}
		if balanceRequestChanged {
			monitor.LastBalanceError = ""
			monitor.BalanceConsecutiveFailures = 0
		}
		if upstreamAccountChanged || balanceWarningThresholdChanged || balanceSyncChanged {
			monitor.BalanceAlertNotified = false
		}
		return tx.Save(&monitor).Error
	})
	return monitor, err
}

func SaveChannelSmartScheduleConfig(channelId int, options ChannelSmartScheduleConfigOptions) (monitor ChannelRatioMonitor, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		created := errors.Is(findErr, gorm.ErrRecordNotFound)
		if created {
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}

		wasParticipating := monitor.ParticipatesInSmartSchedule()
		if monitor.SmartScheduleParticipationSet && monitor.SmartScheduleExcluded == options.Excluded {
			return nil
		}
		if monitor.SmartScheduleRevision == math.MaxInt64 {
			return errors.New("渠道智能调度修订号已达上限")
		}
		monitor.SmartScheduleParticipationSet = true
		monitor.SmartScheduleExcluded = options.Excluded
		if !wasParticipating && !options.Excluded &&
			monitor.SmartScheduleStabilityState == ChannelSmartScheduleStabilityProbing {
			monitor.SmartScheduleStabilitySince = common.GetTimestamp()
		}
		monitor.SmartScheduleRevision++
		if created {
			return tx.Create(&monitor).Error
		}
		return tx.Save(&monitor).Error
	})
	return monitor, err
}

func InitializeChannelSmartScheduleParticipation(smartScheduleEnabled bool) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		findOptionErr := lockForUpdate(tx).
			Where(&Option{Key: ChannelSmartScheduleParticipationInitializedOption}).
			First(&option).Error
		if findOptionErr == nil && option.Value == "true" {
			return nil
		}
		if findOptionErr != nil && !errors.Is(findOptionErr, gorm.ErrRecordNotFound) {
			return findOptionErr
		}

		var channelIds []int
		if err := tx.Model(&Channel{}).Pluck("id", &channelIds).Error; err != nil {
			return err
		}
		var monitors []ChannelRatioMonitor
		if len(channelIds) > 0 {
			if err := lockForUpdate(tx).Where("channel_id IN ?", channelIds).Find(&monitors).Error; err != nil {
				return err
			}
		}
		monitorByChannel := make(map[int]ChannelRatioMonitor, len(monitors))
		for _, monitor := range monitors {
			monitorByChannel[monitor.ChannelId] = monitor
		}
		adoptLegacyParticipation := smartScheduleEnabled
		if !adoptLegacyParticipation {
			for _, monitor := range monitors {
				if monitor.SmartScheduleExcluded || monitor.LastScheduleStatus != "" ||
					monitor.LastScheduleTime > 0 || monitor.SmartScheduleStabilityState != "" ||
					monitor.SmartScheduleStabilitySince > 0 || monitor.SmartScheduleSavedPriority != 0 ||
					monitor.SmartScheduleSavedWeight != 0 {
					adoptLegacyParticipation = true
					break
				}
			}
		}
		for _, channelId := range channelIds {
			monitor, exists := monitorByChannel[channelId]
			if exists && monitor.SmartScheduleParticipationSet {
				continue
			}
			monitor.ChannelId = channelId
			monitor.SmartScheduleParticipationSet = true
			if !adoptLegacyParticipation {
				monitor.SmartScheduleExcluded = true
			}
			if monitor.SmartScheduleRevision == math.MaxInt64 {
				return errors.New("渠道智能调度修订号已达上限")
			}
			monitor.SmartScheduleRevision++
			if exists {
				if err := tx.Save(&monitor).Error; err != nil {
					return err
				}
			} else if err := tx.Create(&monitor).Error; err != nil {
				return err
			}
		}

		option.Key = ChannelSmartScheduleParticipationInitializedOption
		option.Value = "true"
		if errors.Is(findOptionErr, gorm.ErrRecordNotFound) {
			return tx.Create(&option).Error
		}
		return tx.Save(&option).Error
	})
	if err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[ChannelSmartScheduleParticipationInitializedOption] = "true"
	common.OptionMapRWMutex.Unlock()
	return nil
}

func ApplyChannelSmartScheduleResults(results []ChannelSmartScheduleResultUpdate) ([]ChannelSmartScheduleApplyOutcome, error) {
	if len(results) == 0 {
		return nil, nil
	}
	outcomes := make([]ChannelSmartScheduleApplyOutcome, 0, len(results))
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, result := range results {
			outcome := ChannelSmartScheduleApplyOutcome{ChannelId: result.ChannelId}
			var controlOption Option
			if result.GuardCurrent {
				optionErr := lockForUpdate(tx).Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).
					First(&controlOption).Error
				if errors.Is(optionErr, gorm.ErrRecordNotFound) {
					controlOption.Value = ""
				} else if optionErr != nil {
					return optionErr
				}
			}
			var monitor ChannelRatioMonitor
			findErr := lockForUpdate(tx).Where("channel_id = ?", result.ChannelId).First(&monitor).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				monitor = ChannelRatioMonitor{
					ChannelId:                     result.ChannelId,
					SmartScheduleParticipationSet: true,
					SmartScheduleExcluded:         true,
				}
			} else if findErr != nil {
				return findErr
			}
			if result.GuardCurrent {
				if controlOption.Value != result.ExpectedControlRevision ||
					monitor.SmartScheduleRevision != result.ExpectedRevision ||
					!monitor.ParticipatesInSmartSchedule() {
					outcomes = append(outcomes, outcome)
					continue
				}
				var channel Channel
				if err := lockForUpdate(tx).Where("id = ?", result.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				if channel.Status != common.ChannelStatusEnabled || channel.GetPriority() != result.ExpectedPriority ||
					uint(channel.GetWeight()) != result.ExpectedWeight {
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
			monitor.LastScheduleStatus = result.Status
			monitor.LastScheduleError = message
			monitor.LastScheduleScore = result.Score
			monitor.LastSchedulePriority = result.Priority
			monitor.LastScheduleWeight = result.Weight
			monitor.LastScheduleTime = updatedTime
			if result.Stability != nil {
				monitor.SmartScheduleStabilityState = result.Stability.State
				monitor.SmartScheduleStabilityUntil = result.Stability.Until
				monitor.SmartScheduleStabilitySince = result.Stability.Since
				monitor.SmartScheduleSavedPriority = result.Stability.SavedPriority
				monitor.SmartScheduleSavedWeight = result.Stability.SavedWeight
			}
			if monitor.SmartScheduleRevision == math.MaxInt64 {
				return errors.New("渠道智能调度修订号已达上限")
			}
			monitor.SmartScheduleRevision++
			if result.ApplyPriorityWeight {
				priority := result.Priority
				weight := result.Weight
				if err := updateChannelSmartSchedulePriorityWeightTx(tx, result.ChannelId, &priority, &weight); err != nil {
					return err
				}
				outcome.RoutingChanged = priority != result.ExpectedPriority || weight != result.ExpectedWeight
			}
			if err := tx.Save(&monitor).Error; err != nil {
				return err
			}
			outcome.Applied = true
			outcomes = append(outcomes, outcome)
		}
		return nil
	})
	return outcomes, err
}

func SaveChannelSmartScheduleResults(results []ChannelSmartScheduleResultUpdate) error {
	_, err := ApplyChannelSmartScheduleResults(results)
	return err
}

func ClearChannelSmartScheduleStability(channelId int, fallbackPriority int64, fallbackWeight uint) (result ChannelSmartScheduleStabilityClearResult, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		if err := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error; err != nil {
			return err
		}
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", channelId).First(&channel).Error; err != nil {
			return err
		}
		result.PreviousState = monitor.SmartScheduleStabilityState
		result.Priority = channel.GetPriority()
		result.Weight = uint(channel.GetWeight())
		if result.PreviousState == "" {
			return nil
		}
		if monitor.SmartScheduleRevision == math.MaxInt64 {
			return errors.New("渠道智能调度修订号已达上限")
		}
		result.Priority = monitor.SmartScheduleSavedPriority
		if result.Priority <= 0 {
			result.Priority = fallbackPriority
		}
		result.Weight = monitor.SmartScheduleSavedWeight
		if result.Weight == 0 {
			result.Weight = fallbackWeight
		}
		if err := updateChannelSmartSchedulePriorityWeightTx(tx, channelId, &result.Priority, &result.Weight); err != nil {
			return err
		}
		now := common.GetTimestamp()
		monitor.SmartScheduleStabilityState = ""
		monitor.SmartScheduleStabilityUntil = 0
		monitor.SmartScheduleStabilitySince = now
		monitor.SmartScheduleSavedPriority = 0
		monitor.SmartScheduleSavedWeight = 0
		monitor.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		monitor.LastScheduleError = "管理员已手动解除稳定性保护"
		monitor.LastScheduleScore = nil
		monitor.LastSchedulePriority = result.Priority
		monitor.LastScheduleWeight = result.Weight
		monitor.LastScheduleTime = now
		monitor.SmartScheduleRevision++
		result.Cleared = true
		return tx.Save(&monitor).Error
	})
	return result, err
}

func UpdateChannelSmartSchedulePriorityWeight(channelId int, priority *int64, weight *uint) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return updateChannelSmartSchedulePriorityWeightTx(tx, channelId, priority, weight)
	})
}

func updateChannelSmartSchedulePriorityWeightTx(tx *gorm.DB, channelId int, priority *int64, weight *uint) error {
	channelUpdates := make(map[string]any, 2)
	abilityUpdates := make(map[string]any, 2)
	if priority != nil {
		channelUpdates["priority"] = *priority
		abilityUpdates["priority"] = *priority
	}
	if weight != nil {
		channelUpdates["weight"] = *weight
		abilityUpdates["weight"] = *weight
	}
	if len(channelUpdates) == 0 {
		return nil
	}

	result := tx.Model(&Channel{}).Where("id = ?", channelId).Updates(channelUpdates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
	}
	return tx.Model(&Ability{}).Where("channel_id = ?", channelId).Updates(abilityUpdates).Error
}

func ResetChannelSmartSchedulePriorityWeight(channelIds []int, priority int64, weight uint) error {
	if len(channelIds) == 0 {
		return nil
	}

	const batchSize = 500
	updates := map[string]any{
		"priority": priority,
		"weight":   weight,
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(channelIds); start += batchSize {
			end := min(start+batchSize, len(channelIds))
			batch := channelIds[start:end]
			if err := tx.Model(&Channel{}).Where("id IN ?", batch).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.Model(&Ability{}).Where("channel_id IN ?", batch).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func RestoreChannelSmartScheduleStabilityStates(fallbackPriority int64, fallbackWeight uint) (int, error) {
	var monitors []ChannelRatioMonitor
	err := DB.Where("smart_schedule_stability_state <> ? OR smart_schedule_stability_since > ?", "", 0).
		Find(&monitors).Error
	if err != nil || len(monitors) == 0 {
		return 0, err
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		for _, monitor := range monitors {
			if monitor.SmartScheduleStabilityState != "" {
				priority := monitor.SmartScheduleSavedPriority
				if priority <= 0 {
					priority = fallbackPriority
				}
				weight := monitor.SmartScheduleSavedWeight
				if weight == 0 {
					weight = fallbackWeight
				}
				if err := updateChannelSmartSchedulePriorityWeightTx(tx, monitor.ChannelId, &priority, &weight); err != nil {
					return err
				}
			}
			if err := tx.Model(&ChannelRatioMonitor{}).
				Where("channel_id = ?", monitor.ChannelId).
				Updates(map[string]any{
					"smart_schedule_stability_state": "",
					"smart_schedule_stability_until": 0,
					"smart_schedule_stability_since": 0,
					"smart_schedule_saved_priority":  0,
					"smart_schedule_saved_weight":    0,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return len(monitors), err
}

func UpdateChannelRatioMonitor(channelId int, ratio float64, remark string, operatorId int, operatorUsername string) (monitor ChannelRatioMonitor, created bool, changed bool, err error) {
	return updateChannelRatioMonitor(channelId, ratio, remark, operatorId, operatorUsername, false)
}

func UpdateChannelRatioMonitorFromUpstream(channelId int, ratio float64, remark string, operatorId int, operatorUsername string) (monitor ChannelRatioMonitor, created bool, changed bool, err error) {
	return updateChannelRatioMonitor(channelId, ratio, remark, operatorId, operatorUsername, true)
}

func RecordChannelRatioMonitorFetchFailure(channelId int, fetchError string) error {
	message := strings.TrimSpace(fetchError)
	if message == "" {
		message = "上游倍率获取失败"
	}
	messageRunes := []rune(message)
	if len(messageRunes) > 255 {
		message = string(messageRunes[:255])
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}

		monitor.LastFetchStatus = ChannelRatioFetchStatusFailed
		monitor.LastFetchError = message
		monitor.LastFetchTime = common.GetTimestamp()
		if monitor.ConsecutiveFailures < 0 {
			monitor.ConsecutiveFailures = 0
		}
		if monitor.ConsecutiveFailures < math.MaxInt {
			monitor.ConsecutiveFailures++
		}
		return tx.Save(&monitor).Error
	})
}

func RecordChannelRatioMonitorBalance(channelId int, balance *float64, fetchError string) error {
	message := strings.TrimSpace(fetchError)
	if balance != nil && (math.IsNaN(*balance) || math.IsInf(*balance, 0)) {
		balance = nil
		message = "上游余额不是有效数字"
	}
	messageRunes := []rune(message)
	if len(messageRunes) > 255 {
		message = string(messageRunes[:255])
	}
	if balance == nil && message == "" {
		return nil
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}

		if balance != nil {
			value := *balance
			monitor.UpstreamBalance = &value
			monitor.LastBalanceTime = common.GetTimestamp()
			monitor.LastBalanceError = ""
			monitor.BalanceConsecutiveFailures = 0
			if monitor.BalanceWarningThreshold == nil || value >= *monitor.BalanceWarningThreshold {
				monitor.BalanceAlertNotified = false
			}
		} else {
			monitor.LastBalanceError = message
			if monitor.BalanceConsecutiveFailures < 0 {
				monitor.BalanceConsecutiveFailures = 0
			}
			if monitor.BalanceConsecutiveFailures < math.MaxInt {
				monitor.BalanceConsecutiveFailures++
			}
		}
		return tx.Save(&monitor).Error
	})
}

func MarkChannelRatioMonitorBalanceAlertsNotified(channelIds []int) error {
	if len(channelIds) == 0 {
		return nil
	}
	return DB.Model(&ChannelRatioMonitor{}).
		Where("channel_id IN ?", channelIds).
		Update("balance_alert_notified", true).Error
}

func updateChannelRatioMonitor(channelId int, ratio float64, remark string, operatorId int, operatorUsername string, fetchedFromUpstream bool) (monitor ChannelRatioMonitor, created bool, changed bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		query := lockForUpdate(tx).Where("channel_id = ?", channelId)
		findErr := query.First(&monitor).Error
		now := common.GetTimestamp()
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			monitor = ChannelRatioMonitor{
				ChannelId:         channelId,
				Ratio:             ratio,
				Remark:            remark,
				UpdatedTime:       now,
				UpdatedBy:         operatorId,
				UpdatedByUsername: operatorUsername,
			}
			if fetchedFromUpstream {
				monitor.LastFetchStatus = ChannelRatioFetchStatusSucceeded
				monitor.LastFetchTime = now
			}
			created = true
			return tx.Create(&monitor).Error
		}
		if findErr != nil {
			return findErr
		}

		if monitor.UpdatedTime == 0 {
			monitor.Ratio = ratio
			monitor.Remark = remark
			monitor.UpdatedTime = now
			monitor.UpdatedBy = operatorId
			monitor.UpdatedByUsername = operatorUsername
			if fetchedFromUpstream {
				monitor.LastFetchStatus = ChannelRatioFetchStatusSucceeded
				monitor.LastFetchError = ""
				monitor.LastFetchTime = now
				monitor.ConsecutiveFailures = 0
			}
			return tx.Save(&monitor).Error
		}

		changed = math.Abs(monitor.Ratio-ratio) > 1e-9
		if changed {
			history := ChannelRatioHistory{
				ChannelId:        channelId,
				OldRatio:         monitor.Ratio,
				NewRatio:         ratio,
				Remark:           remark,
				CreatedTime:      common.GetTimestamp(),
				OperatorId:       operatorId,
				OperatorUsername: operatorUsername,
			}
			if err := tx.Create(&history).Error; err != nil {
				return err
			}
			previousRatio := monitor.Ratio
			monitor.PreviousRatio = &previousRatio
		}

		monitor.Ratio = ratio
		monitor.Remark = remark
		monitor.UpdatedTime = now
		monitor.UpdatedBy = operatorId
		monitor.UpdatedByUsername = operatorUsername
		if fetchedFromUpstream {
			monitor.LastFetchStatus = ChannelRatioFetchStatusSucceeded
			monitor.LastFetchError = ""
			monitor.LastFetchTime = now
			monitor.ConsecutiveFailures = 0
		}
		return tx.Save(&monitor).Error
	})
	return monitor, created, changed, err
}

func GetChannelRatioHistory(channelId int, startIdx int, num int) (history []ChannelRatioHistory, total int64, err error) {
	query := DB.Model(&ChannelRatioHistory{}).Where("channel_id = ?", channelId)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("created_time desc, id desc").Limit(num).Offset(startIdx).Find(&history).Error
	return history, total, err
}

func GetChannelRatioMonitorTasks(startIdx int, num int) (tasks []*SystemTask, total int64, err error) {
	return GetChannelMonitorTasksByType(SystemTaskTypeChannelRatioMonitor, startIdx, num)
}

func GetChannelMonitorTasksByType(taskType string, startIdx int, num int) (tasks []*SystemTask, total int64, err error) {
	query := DB.Model(&SystemTask{}).Where("type = ?", taskType)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	return tasks, total, err
}
