package model

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	ChannelRatioFetchStatusSucceeded          = "succeeded"
	ChannelRatioFetchStatusFailed             = "failed"
	ChannelRatioFailureAlertRatio             = "ratio"
	ChannelRatioFailureAlertBalance           = "balance"
	ChannelSmartScheduleStatusSucceeded       = "succeeded"
	ChannelSmartScheduleStatusSkipped         = "skipped"
	ChannelSmartScheduleStatusFailed          = "failed"
	ChannelSmartScheduleStabilityDegraded     = "degraded"
	ChannelSmartScheduleStabilityProbing      = "probing"
	ChannelSmartScheduleControlRevisionOption = "ChannelMonitorSmartScheduleControlRevision"
)

// ErrChannelRatioMonitorConfigChanged indicates that an upstream response was
// produced from a stale monitor configuration and must not be applied.
var ErrChannelRatioMonitorConfigChanged = errors.New("渠道监控配置已变更，已丢弃本次上游结果")

type ChannelRatioMonitor struct {
	Id                          int      `json:"id"`
	ChannelId                   int      `json:"channel_id" gorm:"uniqueIndex;not null"`
	Ratio                       float64  `json:"ratio" gorm:"not null"`
	PreviousRatio               *float64 `json:"previous_ratio"`
	Remark                      string   `json:"remark" gorm:"type:varchar(255);default:''"`
	UpdatedTime                 int64    `json:"updated_time" gorm:"bigint;index"`
	UpdatedBy                   int      `json:"updated_by" gorm:"index"`
	UpdatedByUsername           string   `json:"updated_by_username" gorm:"type:varchar(64);default:''"`
	LastFetchStatus             string   `json:"last_fetch_status" gorm:"type:varchar(16);index"`
	LastFetchError              string   `json:"last_fetch_error" gorm:"type:varchar(255)"`
	LastFetchTime               int64    `json:"last_fetch_time" gorm:"bigint;index"`
	ConsecutiveFailures         int      `json:"consecutive_failures"`
	FetchFailureAlertNotified   bool     `json:"-"`
	UpstreamBalance             *float64 `json:"upstream_balance"`
	LastBalanceTime             int64    `json:"last_balance_time" gorm:"bigint"`
	LastBalanceCostNanoCNY      *int64   `json:"-" gorm:"bigint"`
	BalancePendingConsumption   float64  `json:"-"`
	LastBalanceError            string   `json:"last_balance_error" gorm:"type:varchar(255)"`
	BalanceConsecutiveFailures  int      `json:"balance_consecutive_failures"`
	BalanceFailureAlertNotified bool     `json:"-"`
	BalanceWarningThreshold     *float64 `json:"balance_warning_threshold"`
	BalanceAutoDisableThreshold *float64 `json:"balance_auto_disable_threshold"`
	BalanceAlertNotified        bool     `json:"balance_alert_notified"`
	UpstreamType                string   `json:"upstream_type" gorm:"type:varchar(32)"`
	UpstreamBaseURL             string   `json:"upstream_base_url" gorm:"type:text"`
	UpstreamGroup               string   `json:"upstream_group" gorm:"type:varchar(64)"`
	UpstreamAuthType            string   `json:"upstream_auth_type" gorm:"type:varchar(16)"`
	UpstreamUserId              int      `json:"upstream_user_id"`
	UpstreamAccessToken         string   `json:"-" gorm:"type:text"`
	UpstreamRefreshToken        string   `json:"-" gorm:"type:text"`
	UpstreamAccount             string   `json:"-" gorm:"type:varchar(320)"`
	UpstreamPassword            string   `json:"-" gorm:"type:text"`
	UpstreamRevision            int64    `json:"-" gorm:"bigint"`
	CostConversion              string   `json:"-" gorm:"type:text"`
	CustomUpstreamConfig        string   `json:"-" gorm:"type:text"`
	UpstreamRatioSyncDisabled   bool     `json:"-"`
	UpstreamBalanceSyncDisabled bool     `json:"-"`
	SingleChannelAction         string   `json:"single_channel_action" gorm:"type:varchar(32)"`
	MultipleChannelsAction      string   `json:"multiple_channels_action" gorm:"type:varchar(32)"`
	ConcurrencyLimit            int      `json:"concurrency_limit"`
	ConcurrencyRevision         int64    `json:"-" gorm:"bigint"`
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
	UpstreamRefreshToken        string
}

// ChannelRatioMonitorBalanceEstimateState is the persisted state needed to
// carry local consumption forward until the upstream balance reflects it.
type ChannelRatioMonitorBalanceEstimateState struct {
	CostBaseline       ChannelDailyCostBaseline
	PendingConsumption float64
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
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockChannelForDependentWriteTx(tx, channelId); err != nil {
			return err
		}
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
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		economicRevision, err := lockChannelMonitorEconomicRevisionTx(tx)
		if err != nil {
			return err
		}
		if err := lockChannelForDependentWriteTx(tx, channelId); err != nil {
			return err
		}
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
			monitor.UpstreamRefreshToken != options.UpstreamRefreshToken ||
			monitor.UpstreamAccount != options.UpstreamAccount ||
			monitor.UpstreamPassword != options.UpstreamPassword ||
			monitor.CustomUpstreamConfig != options.CustomUpstreamConfig
		balanceWarningThresholdChanged :=
			(monitor.BalanceWarningThreshold == nil) != (options.BalanceWarningThreshold == nil) ||
				(monitor.BalanceWarningThreshold != nil && options.BalanceWarningThreshold != nil &&
					*monitor.BalanceWarningThreshold != *options.BalanceWarningThreshold)
		balanceAutoDisableThresholdChanged :=
			(monitor.BalanceAutoDisableThreshold == nil) != (options.BalanceAutoDisableThreshold == nil) ||
				(monitor.BalanceAutoDisableThreshold != nil && options.BalanceAutoDisableThreshold != nil &&
					*monitor.BalanceAutoDisableThreshold != *options.BalanceAutoDisableThreshold)
		ratioSyncChanged := monitor.UpstreamRatioSyncDisabled != !options.RatioSyncEnabled
		balanceSyncChanged := monitor.UpstreamBalanceSyncDisabled != !options.BalanceSyncEnabled
		costConversionChanged := monitor.CostConversion != options.CostConversion
		ratioRequestChanged := upstreamAccountChanged ||
			monitor.UpstreamGroup != group ||
			costConversionChanged ||
			ratioSyncChanged
		balanceRequestChanged := upstreamAccountChanged || balanceSyncChanged
		upstreamConfigChanged := upstreamAccountChanged ||
			monitor.UpstreamGroup != group ||
			costConversionChanged ||
			monitor.CustomUpstreamConfig != options.CustomUpstreamConfig ||
			ratioSyncChanged || balanceSyncChanged ||
			monitor.SingleChannelAction != options.SingleChannelAction ||
			(monitor.BalanceWarningThreshold == nil) != (options.BalanceWarningThreshold == nil) ||
			(monitor.BalanceWarningThreshold != nil && options.BalanceWarningThreshold != nil &&
				*monitor.BalanceWarningThreshold != *options.BalanceWarningThreshold) ||
			(monitor.BalanceAutoDisableThreshold == nil) != (options.BalanceAutoDisableThreshold == nil) ||
			(monitor.BalanceAutoDisableThreshold != nil && options.BalanceAutoDisableThreshold != nil &&
				*monitor.BalanceAutoDisableThreshold != *options.BalanceAutoDisableThreshold) ||
			monitor.MultipleChannelsAction != options.MultipleChannelsAction
		if upstreamConfigChanged {
			if monitor.UpstreamRevision == math.MaxInt64 {
				return errors.New("渠道监控上游配置修订号已达上限")
			}
			monitor.UpstreamRevision++
		}

		monitor.UpstreamType = upstreamType
		monitor.UpstreamBaseURL = baseURL
		monitor.UpstreamGroup = group
		monitor.UpstreamAuthType = authType
		monitor.UpstreamUserId = userId
		monitor.UpstreamAccessToken = accessToken
		monitor.UpstreamRefreshToken = options.UpstreamRefreshToken
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
		if upstreamAccountChanged || costConversionChanged || balanceSyncChanged ||
			balanceWarningThresholdChanged || balanceAutoDisableThresholdChanged {
			monitor.LastBalanceCostNanoCNY = nil
			monitor.BalancePendingConsumption = 0
		}
		if ratioRequestChanged {
			monitor.ConsecutiveFailures = 0
			monitor.FetchFailureAlertNotified = false
			if monitor.LastFetchStatus == ChannelRatioFetchStatusFailed {
				monitor.LastFetchStatus = ""
				monitor.LastFetchError = ""
				monitor.LastFetchTime = 0
			}
		}
		if balanceRequestChanged {
			monitor.LastBalanceError = ""
			monitor.BalanceConsecutiveFailures = 0
			monitor.BalanceFailureAlertNotified = false
		}
		if upstreamAccountChanged || balanceWarningThresholdChanged || balanceSyncChanged {
			monitor.BalanceAlertNotified = false
		}
		if costConversionChanged {
			monitor.PreviousRatio = nil
			if err := economicRevision.bump(tx); err != nil {
				return err
			}
		}
		return tx.Save(&monitor).Error
	})
	return monitor, err
}

// RotateChannelRatioUpstreamCredential replaces a one-time rotating credential
// only when the monitor still has the revision and credential used by the caller.
func RotateChannelRatioUpstreamCredential(channelId int, upstreamType string, authType string, expectedRevision int64, oldCredential string, newCredential string) (bool, error) {
	if channelId <= 0 || expectedRevision < 0 || oldCredential == "" || newCredential == "" {
		return false, errors.New("渠道监控上游凭据轮换参数无效")
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	result := DB.Model(&ChannelRatioMonitor{}).
		Where(
			"channel_id = ? AND upstream_type = ? AND upstream_auth_type = ? AND upstream_revision = ? AND upstream_access_token = ?",
			channelId,
			upstreamType,
			authType,
			expectedRevision,
			oldCredential,
		).
		Update("upstream_access_token", newCredential)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RotateChannelRatioUpstreamRefreshToken updates a rotated refresh credential
// only when the monitor snapshot used by the request is still current.
func RotateChannelRatioUpstreamRefreshToken(channelId int, upstreamType string, authType string, expectedRevision int64, oldCredential string, newCredential string) (bool, error) {
	if channelId <= 0 || expectedRevision < 0 || oldCredential == "" || newCredential == "" {
		return false, errors.New("渠道监控 Refresh Token 轮换参数无效")
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	result := DB.Model(&ChannelRatioMonitor{}).
		Where(
			"channel_id = ? AND upstream_type = ? AND upstream_auth_type = ? AND upstream_revision = ? AND upstream_refresh_token = ?",
			channelId,
			upstreamType,
			authType,
			expectedRevision,
			oldCredential,
		).
		Update("upstream_refresh_token", newCredential)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func UpdateChannelRatioMonitor(channelId int, ratio float64, remark string, operatorId int, operatorUsername string) (monitor ChannelRatioMonitor, created bool, changed bool, err error) {
	return updateChannelRatioMonitor(channelId, ratio, remark, operatorId, operatorUsername, false)
}

func UpdateChannelRatioMonitorFromUpstream(channelId int, ratio float64, remark string, operatorId int, operatorUsername string) (monitor ChannelRatioMonitor, created bool, changed bool, err error) {
	return updateChannelRatioMonitor(channelId, ratio, remark, operatorId, operatorUsername, true)
}

// UpdateChannelRatioMonitorFromUpstreamIfRevision applies a fetched ratio only
// when the upstream configuration has not changed since the request started.
// The bool is false when the response was stale and was intentionally ignored.
func UpdateChannelRatioMonitorFromUpstreamIfRevision(channelId int, expectedRevision int64, ratio float64, remark string, operatorId int, operatorUsername string) (monitor ChannelRatioMonitor, created bool, changed bool, applied bool, err error) {
	return updateChannelRatioMonitorWithRevision(channelId, ratio, remark, operatorId, operatorUsername, true, &expectedRevision)
}

func RecordChannelRatioMonitorFetchFailure(channelId int, fetchError string) error {
	_, err := recordChannelRatioMonitorFetchFailure(channelId, fetchError, nil)
	return err
}

// RecordChannelRatioMonitorFetchFailureIfRevision records a failure only when
// it belongs to the currently saved upstream configuration.
func RecordChannelRatioMonitorFetchFailureIfRevision(channelId int, expectedRevision int64, fetchError string) (bool, error) {
	return recordChannelRatioMonitorFetchFailure(channelId, fetchError, &expectedRevision)
}

func recordChannelRatioMonitorFetchFailure(channelId int, fetchError string, expectedRevision *int64) (applied bool, err error) {
	message := strings.TrimSpace(fetchError)
	if message == "" {
		message = "上游倍率获取失败"
	}
	messageRunes := []rune(message)
	if len(messageRunes) > 255 {
		message = string(messageRunes[:255])
	}

	applied = true
	// Keep monitor row transactions serialized with other channel writes so
	// concurrent refresh workers remain compatible with SQLite locking.
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	err = DB.Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if expectedRevision != nil {
				applied = false
				return nil
			}
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}
		if expectedRevision != nil && monitor.UpstreamRevision != *expectedRevision {
			applied = false
			return nil
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
	return applied, err
}

func RecordChannelRatioMonitorBalance(channelId int, balance *float64, fetchError string) error {
	_, err := recordChannelRatioMonitorBalance(channelId, balance, fetchError, nil, nil)
	return err
}

// RecordChannelRatioMonitorBalanceIfRevision records a balance only when it
// belongs to the currently saved upstream configuration.
func RecordChannelRatioMonitorBalanceIfRevision(channelId int, expectedRevision int64, balance *float64, fetchError string) (bool, error) {
	return recordChannelRatioMonitorBalance(channelId, balance, fetchError, &expectedRevision, nil)
}

// RecordChannelRatioMonitorBalanceWithEstimateIfRevision records an upstream
// balance together with the cost baseline used to evaluate delayed spending.
// The revision guard prevents a response for an old upstream configuration
// from changing the current monitor state.
func RecordChannelRatioMonitorBalanceWithEstimateIfRevision(
	channelId int,
	expectedRevision int64,
	balance *float64,
	fetchError string,
	estimateState *ChannelRatioMonitorBalanceEstimateState,
) (bool, error) {
	return recordChannelRatioMonitorBalance(channelId, balance, fetchError, &expectedRevision, estimateState)
}

func recordChannelRatioMonitorBalance(
	channelId int,
	balance *float64,
	fetchError string,
	expectedRevision *int64,
	estimateState *ChannelRatioMonitorBalanceEstimateState,
) (applied bool, err error) {
	message := strings.TrimSpace(fetchError)
	if balance != nil && (math.IsNaN(*balance) || math.IsInf(*balance, 0)) {
		balance = nil
		message = "上游余额不是有效数字"
	}
	if balance != nil && estimateState != nil {
		if estimateState.CostBaseline.Timestamp <= 0 || estimateState.CostBaseline.CostNanoCNY < 0 ||
			math.IsNaN(estimateState.PendingConsumption) || math.IsInf(estimateState.PendingConsumption, 0) ||
			estimateState.PendingConsumption < 0 {
			return false, errors.New("余额消费估算状态无效")
		}
	}
	messageRunes := []rune(message)
	if len(messageRunes) > 255 {
		message = string(messageRunes[:255])
	}
	if balance == nil && message == "" {
		return true, nil
	}

	applied = true
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	err = DB.Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&monitor).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if expectedRevision != nil {
				applied = false
				return nil
			}
			monitor = ChannelRatioMonitor{ChannelId: channelId}
		} else if findErr != nil {
			return findErr
		}
		if expectedRevision != nil && monitor.UpstreamRevision != *expectedRevision {
			applied = false
			return nil
		}

		if balance != nil {
			value := *balance
			monitor.UpstreamBalance = &value
			monitor.LastBalanceTime = common.GetTimestamp()
			monitor.LastBalanceCostNanoCNY = nil
			monitor.BalancePendingConsumption = 0
			if estimateState != nil {
				baselineCost := estimateState.CostBaseline.CostNanoCNY
				monitor.LastBalanceTime = estimateState.CostBaseline.Timestamp
				monitor.LastBalanceCostNanoCNY = &baselineCost
				monitor.BalancePendingConsumption = estimateState.PendingConsumption
			}
			monitor.LastBalanceError = ""
			monitor.BalanceConsecutiveFailures = 0
			monitor.BalanceFailureAlertNotified = false
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
	return applied, err
}

type ChannelRatioMonitorBalanceAlertGuard struct {
	ChannelId        int
	UpstreamRevision int64
	WarningThreshold float64
}

func MarkChannelRatioMonitorBalanceAlertsNotified(guards []ChannelRatioMonitorBalanceAlertGuard) error {
	for _, guard := range guards {
		if guard.ChannelId <= 0 || math.IsNaN(guard.WarningThreshold) || math.IsInf(guard.WarningThreshold, 0) {
			return errors.New("余额预警通知快照无效")
		}
	}
	if len(guards) == 0 {
		return nil
	}
	orderedGuards := append([]ChannelRatioMonitorBalanceAlertGuard(nil), guards...)
	sort.Slice(orderedGuards, func(i, j int) bool {
		return orderedGuards[i].ChannelId < orderedGuards[j].ChannelId
	})
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, guard := range orderedGuards {
			var monitor ChannelRatioMonitor
			findErr := lockForUpdate(tx).
				Where("channel_id = ?", guard.ChannelId).
				First(&monitor).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				continue
			}
			if findErr != nil {
				return findErr
			}
			if monitor.UpstreamRevision != guard.UpstreamRevision ||
				monitor.BalanceWarningThreshold == nil ||
				*monitor.BalanceWarningThreshold != guard.WarningThreshold ||
				monitor.UpstreamBalance == nil ||
				*monitor.UpstreamBalance >= guard.WarningThreshold ||
				monitor.BalanceAlertNotified {
				continue
			}
			if err := tx.Model(&ChannelRatioMonitor{}).
				Where("id = ?", monitor.Id).
				Update("balance_alert_notified", true).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type ChannelRatioMonitorFailureAlertGuard struct {
	ChannelId        int
	UpstreamRevision int64
	FailureType      string
	FailureCount     int
}

// MarkChannelRatioMonitorFailureAlertsNotified marks final sync failures after
// the notification email has been sent. Revision and counter guards prevent a
// stale task from acknowledging a newer configuration or failure cycle.
func MarkChannelRatioMonitorFailureAlertsNotified(guards []ChannelRatioMonitorFailureAlertGuard) error {
	for _, guard := range guards {
		if guard.ChannelId <= 0 || guard.FailureCount <= 0 ||
			(guard.FailureType != ChannelRatioFailureAlertRatio && guard.FailureType != ChannelRatioFailureAlertBalance) {
			return errors.New("上游同步失败通知快照无效")
		}
	}
	if len(guards) == 0 {
		return nil
	}
	orderedGuards := append([]ChannelRatioMonitorFailureAlertGuard(nil), guards...)
	sort.Slice(orderedGuards, func(i, j int) bool {
		if orderedGuards[i].ChannelId != orderedGuards[j].ChannelId {
			return orderedGuards[i].ChannelId < orderedGuards[j].ChannelId
		}
		return orderedGuards[i].FailureType < orderedGuards[j].FailureType
	})
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, guard := range orderedGuards {
			var monitor ChannelRatioMonitor
			findErr := lockForUpdate(tx).
				Where("channel_id = ?", guard.ChannelId).
				First(&monitor).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				continue
			}
			if findErr != nil {
				return findErr
			}
			if monitor.UpstreamRevision != guard.UpstreamRevision {
				continue
			}

			field := ""
			switch guard.FailureType {
			case ChannelRatioFailureAlertRatio:
				if monitor.FetchFailureAlertNotified ||
					monitor.LastFetchStatus != ChannelRatioFetchStatusFailed ||
					monitor.ConsecutiveFailures < guard.FailureCount {
					continue
				}
				field = "fetch_failure_alert_notified"
			case ChannelRatioFailureAlertBalance:
				if monitor.BalanceFailureAlertNotified ||
					strings.TrimSpace(monitor.LastBalanceError) == "" ||
					monitor.BalanceConsecutiveFailures < guard.FailureCount {
					continue
				}
				field = "balance_failure_alert_notified"
			}
			if err := tx.Model(&ChannelRatioMonitor{}).
				Where("id = ?", monitor.Id).
				Update(field, true).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func updateChannelRatioMonitor(channelId int, ratio float64, remark string, operatorId int, operatorUsername string, fetchedFromUpstream bool) (monitor ChannelRatioMonitor, created bool, changed bool, err error) {
	monitor, created, changed, _, err = updateChannelRatioMonitorWithRevision(channelId, ratio, remark, operatorId, operatorUsername, fetchedFromUpstream, nil)
	return
}

func updateChannelRatioMonitorWithRevision(channelId int, ratio float64, remark string, operatorId int, operatorUsername string, fetchedFromUpstream bool, expectedRevision *int64) (monitor ChannelRatioMonitor, created bool, changed bool, applied bool, err error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	applied = true
	err = DB.Transaction(func(tx *gorm.DB) error {
		economicRevision, err := lockChannelMonitorEconomicRevisionTx(tx)
		if err != nil {
			return err
		}
		if err := lockChannelForDependentWriteTx(tx, channelId); err != nil {
			if expectedRevision != nil && errors.Is(err, gorm.ErrRecordNotFound) {
				applied = false
				return nil
			}
			return err
		}
		query := lockForUpdate(tx).Where("channel_id = ?", channelId)
		findErr := query.First(&monitor).Error
		now := common.GetTimestamp()
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if expectedRevision != nil {
				applied = false
				return nil
			}
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
			if err := economicRevision.bump(tx); err != nil {
				return err
			}
			return tx.Create(&monitor).Error
		}
		if findErr != nil {
			return findErr
		}
		if expectedRevision != nil && monitor.UpstreamRevision != *expectedRevision {
			applied = false
			return nil
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
				monitor.FetchFailureAlertNotified = false
			}
			if err := economicRevision.bump(tx); err != nil {
				return err
			}
			return tx.Save(&monitor).Error
		}

		economicRatioChanged := monitor.Ratio != ratio
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
			monitor.FetchFailureAlertNotified = false
		}
		if economicRatioChanged {
			if err := economicRevision.bump(tx); err != nil {
				return err
			}
		}
		return tx.Save(&monitor).Error
	})
	return monitor, created, changed, applied, err
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
