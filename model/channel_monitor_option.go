package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelMonitorGroupCoefficientsOption                      = "ChannelMonitorGroupCoefficients"
	ChannelMonitorSmartSchedulePerformanceWindowOption         = "ChannelMonitorSmartSchedulePerformanceWindowMinutes"
	ChannelMonitorSmartScheduleStabilityWindowOption           = "ChannelMonitorSmartScheduleStabilityWindowMinutes"
	ChannelMonitorSmartScheduleMaxWindowMinutes                = ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes
	ChannelMonitorSmartScheduleDefaultPerformanceWindowMinutes = 60
	ChannelMonitorSmartScheduleDefaultStabilityWindowMinutes   = 5
)

const channelMonitorGroupRatioEpsilon = 1e-9

var ErrChannelMonitorSettingsChanged = errors.New("渠道监控设置已被其他请求修改，请刷新后重试")

var channelMonitorOptionMu sync.Mutex

type ChannelMonitorGroupRatioRevisionGuard map[string]map[int]int64
type ChannelMonitorGroupRatioMembershipGuard map[string]map[int]string
type ChannelMonitorGroupRatioStatusGuard map[string]map[int]int
type ChannelMonitorGroupRatioValueSnapshot struct {
	Ratio       float64
	Coefficient float64
}
type ChannelMonitorGroupRatioValueGuard map[string]ChannelMonitorGroupRatioValueSnapshot

// MergeChannelMonitorGroupOptions updates only the supplied group keys while
// preserving values committed by concurrent channel-monitor operations. When
// onlyIncreaseRatios is true, a stale policy plan cannot lower a newer ratio.
func MergeChannelMonitorGroupOptions(
	ratioUpdates map[string]float64,
	coefficientUpdates map[string]float64,
	onlyIncreaseRatios bool,
) (ratioUpdatesApplied int, err error) {
	return MergeChannelMonitorGroupOptionsIfCurrent(
		ratioUpdates,
		coefficientUpdates,
		onlyIncreaseRatios,
		nil,
		nil,
		nil,
		nil,
	)
}

func MergeChannelMonitorGroupOptionsIfCurrent(
	ratioUpdates map[string]float64,
	coefficientUpdates map[string]float64,
	onlyIncreaseRatios bool,
	revisionGuards ChannelMonitorGroupRatioRevisionGuard,
	membershipGuards ChannelMonitorGroupRatioMembershipGuard,
	statusGuards ChannelMonitorGroupRatioStatusGuard,
	valueGuards ChannelMonitorGroupRatioValueGuard,
) (ratioUpdatesApplied int, err error) {
	if len(ratioUpdates) == 0 && len(coefficientUpdates) == 0 {
		return 0, nil
	}
	for group, ratio := range ratioUpdates {
		if group == "" || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
			return 0, fmt.Errorf("分组 %q 的倍率无效", group)
		}
	}
	for group, coefficient := range coefficientUpdates {
		if group == "" || math.IsNaN(coefficient) || math.IsInf(coefficient, 0) || coefficient < 0 {
			return 0, fmt.Errorf("分组 %q 的系数无效", group)
		}
	}
	for group, expected := range valueGuards {
		if group == "" || math.IsNaN(expected.Ratio) || math.IsInf(expected.Ratio, 0) || expected.Ratio < 0 ||
			math.IsNaN(expected.Coefficient) || math.IsInf(expected.Coefficient, 0) || expected.Coefficient < 0 {
			return 0, fmt.Errorf("分组 %q 的倍率计划快照无效", group)
		}
	}

	ratioSeed, err := common.Marshal(ratio_setting.GetGroupRatioCopy())
	if err != nil {
		return 0, err
	}
	coefficientSeed := "{}"
	common.OptionMapRWMutex.RLock()
	if value := common.OptionMap[ChannelMonitorGroupCoefficientsOption]; value != "" {
		coefficientSeed = value
	}
	common.OptionMapRWMutex.RUnlock()

	channelMonitorOptionMu.Lock()
	defer channelMonitorOptionMu.Unlock()

	committedValues := map[string]string{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		seeds := map[string]string{}
		if len(ratioUpdates) > 0 {
			seeds["GroupRatio"] = string(ratioSeed)
			seeds[ChannelMonitorEconomicRevisionOption] = ""
		}
		if len(coefficientUpdates) > 0 || len(valueGuards) > 0 {
			seeds[ChannelMonitorGroupCoefficientsOption] = coefficientSeed
		}
		options, lockErr := lockChannelMonitorOptionsTx(tx, seeds)
		if lockErr != nil {
			return lockErr
		}

		if len(ratioUpdates) > 0 {
			staleGroups := make(map[string]struct{})
			if len(membershipGuards) > 0 || len(statusGuards) > 0 {
				var channels []Channel
				if err := lockForUpdate(tx).
					Select("id", "group", "status").
					Order("id ASC").
					Find(&channels).Error; err != nil {
					return err
				}
				channelById := make(map[int]Channel, len(channels))
				membersByGroup := make(map[string]map[int]struct{}, len(membershipGuards))
				for group := range membershipGuards {
					membersByGroup[group] = make(map[int]struct{})
				}
				for _, channel := range channels {
					channelById[channel.Id] = channel
					for _, group := range channel.GetGroups() {
						if members, guarded := membersByGroup[group]; guarded {
							members[channel.Id] = struct{}{}
						}
					}
				}
				for group, expectedByChannel := range membershipGuards {
					currentMembers := membersByGroup[group]
					if len(currentMembers) != len(expectedByChannel) {
						staleGroups[group] = struct{}{}
						continue
					}
					for channelId, expectedGroups := range expectedByChannel {
						channel, exists := channelById[channelId]
						_, member := currentMembers[channelId]
						if !exists || !member || channel.Group != expectedGroups {
							staleGroups[group] = struct{}{}
							break
						}
					}
				}
				for group, expectedByChannel := range statusGuards {
					for channelId, expectedStatus := range expectedByChannel {
						channel, exists := channelById[channelId]
						if !exists || channel.Status != expectedStatus {
							staleGroups[group] = struct{}{}
							break
						}
					}
				}
			}
			guardedMonitorIds := make([]int, 0)
			expectedRevisionByChannel := make(map[int]int64)
			for _, expectedByChannel := range revisionGuards {
				for channelId, expectedRevision := range expectedByChannel {
					if previous, exists := expectedRevisionByChannel[channelId]; exists && previous != expectedRevision {
						return errors.New("分组倍率计划包含冲突的渠道配置修订号")
					}
					expectedRevisionByChannel[channelId] = expectedRevision
				}
			}
			for channelId := range expectedRevisionByChannel {
				guardedMonitorIds = append(guardedMonitorIds, channelId)
			}
			if len(guardedMonitorIds) > 0 {
				sort.Ints(guardedMonitorIds)
				var monitors []ChannelRatioMonitor
				if err := lockForUpdate(tx).
					Select("channel_id", "upstream_revision").
					Where("channel_id IN ?", guardedMonitorIds).
					Order("channel_id ASC").
					Find(&monitors).Error; err != nil {
					return err
				}
				currentRevisionByChannel := make(map[int]int64, len(monitors))
				for _, monitor := range monitors {
					currentRevisionByChannel[monitor.ChannelId] = monitor.UpstreamRevision
				}
				for group, expectedByChannel := range revisionGuards {
					for channelId, expectedRevision := range expectedByChannel {
						currentRevision, exists := currentRevisionByChannel[channelId]
						if !exists || currentRevision != expectedRevision {
							staleGroups[group] = struct{}{}
							break
						}
					}
				}
			}
			groupRatios := map[string]float64{}
			if unmarshalErr := common.UnmarshalJsonStr(options["GroupRatio"].Value, &groupRatios); unmarshalErr != nil {
				return fmt.Errorf("解析 GroupRatio 失败: %w", unmarshalErr)
			}
			if groupRatios == nil {
				groupRatios = map[string]float64{}
			}
			if len(valueGuards) > 0 {
				coefficients := map[string]float64{}
				if unmarshalErr := common.UnmarshalJsonStr(options[ChannelMonitorGroupCoefficientsOption].Value, &coefficients); unmarshalErr != nil {
					return fmt.Errorf("解析 %s 失败: %w", ChannelMonitorGroupCoefficientsOption, unmarshalErr)
				}
				if coefficients == nil {
					coefficients = map[string]float64{}
				}
				for group, expected := range valueGuards {
					currentRatio, exists := groupRatios[group]
					if !exists {
						currentRatio = 1
					}
					currentCoefficient, exists := coefficients[group]
					if !exists {
						currentCoefficient = 1
					}
					if currentRatio != expected.Ratio || currentCoefficient != expected.Coefficient {
						staleGroups[group] = struct{}{}
					}
				}
			}
			for group, target := range ratioUpdates {
				if _, stale := staleGroups[group]; stale {
					continue
				}
				current, exists := groupRatios[group]
				if onlyIncreaseRatios {
					if !exists {
						current = 1
					}
					if target-current <= channelMonitorGroupRatioEpsilon {
						continue
					}
				}
				if exists && target == current {
					continue
				}
				groupRatios[group] = target
				ratioUpdatesApplied++
			}
			if ratioUpdatesApplied > 0 {
				encoded, marshalErr := common.Marshal(groupRatios)
				if marshalErr != nil {
					return marshalErr
				}
				committedValues["GroupRatio"] = string(encoded)
				committedValues[ChannelMonitorEconomicRevisionOption] = common.GetUUID()
			}
		}

		if len(coefficientUpdates) > 0 {
			coefficients := map[string]float64{}
			if unmarshalErr := common.UnmarshalJsonStr(options[ChannelMonitorGroupCoefficientsOption].Value, &coefficients); unmarshalErr != nil {
				return fmt.Errorf("解析 %s 失败: %w", ChannelMonitorGroupCoefficientsOption, unmarshalErr)
			}
			if coefficients == nil {
				coefficients = map[string]float64{}
			}
			changed := false
			for group, coefficient := range coefficientUpdates {
				if current, exists := coefficients[group]; exists && current == coefficient {
					continue
				}
				coefficients[group] = coefficient
				changed = true
			}
			if changed {
				encoded, marshalErr := common.Marshal(coefficients)
				if marshalErr != nil {
					return marshalErr
				}
				committedValues[ChannelMonitorGroupCoefficientsOption] = string(encoded)
			}
		}

		return saveLockedChannelMonitorOptionsTx(tx, committedValues)
	})
	if err != nil {
		return 0, err
	}
	if err := refreshChannelMonitorOptions(committedValues); err != nil {
		return ratioUpdatesApplied, err
	}
	return ratioUpdatesApplied, nil
}

// UpdateChannelMonitorSettingsOptions commits setting changes and temporary
// traffic cleanup as one database transaction.
func UpdateChannelMonitorSettingsOptions(
	values map[string]string,
	clearTemporaryTraffic bool,
	expectedSmartScheduleControlRevision *string,
) (routingChanged bool, err error) {
	for key, value := range values {
		if err := validateOptionValue(key, value); err != nil {
			return false, err
		}
	}
	if expectedSmartScheduleControlRevision != nil {
		if _, exists := values[ChannelSmartScheduleControlRevisionOption]; !exists {
			return false, errors.New("智能调度配置更新缺少新修订号")
		}
	}
	committedValues := make(map[string]string, len(values)+1)
	for key, value := range values {
		committedValues[key] = value
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	channelMonitorOptionMu.Lock()
	defer channelMonitorOptionMu.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		if len(values) > 0 {
			seeds := make(map[string]string, len(committedValues)+1)
			for key, value := range committedValues {
				seeds[key] = value
			}
			if _, exists := values["GroupRatio"]; exists {
				seeds["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
				seeds[ChannelMonitorEconomicRevisionOption] = ""
			}
			if expectedSmartScheduleControlRevision != nil {
				seeds[ChannelSmartScheduleControlRevisionOption] = *expectedSmartScheduleControlRevision
			}
			options, lockErr := lockChannelMonitorOptionsTx(tx, seeds)
			if lockErr != nil {
				return lockErr
			}
			if expectedSmartScheduleControlRevision != nil &&
				options[ChannelSmartScheduleControlRevisionOption].Value != *expectedSmartScheduleControlRevision {
				return ErrChannelMonitorSettingsChanged
			}
			if groupRatio, exists := values["GroupRatio"]; exists && options["GroupRatio"].Value != groupRatio {
				committedValues[ChannelMonitorEconomicRevisionOption] = common.GetUUID()
			}
			if saveErr := saveLockedChannelMonitorOptionsTx(tx, committedValues); saveErr != nil {
				return saveErr
			}
		}
		if !clearTemporaryTraffic {
			return nil
		}
		var clearErr error
		routingChanged, clearErr = clearChannelSmartScheduleTemporaryTrafficTx(tx)
		return clearErr
	})
	if err != nil {
		if errors.Is(err, ErrChannelMonitorSettingsChanged) {
			refreshErr := refreshChannelSmartScheduleOptionsLocked()
			if refreshErr != nil {
				return false, fmt.Errorf("%w（刷新当前智能调度设置失败：%v）", err, refreshErr)
			}
		}
		return false, err
	}
	if err := refreshChannelMonitorOptions(committedValues); err != nil {
		return routingChanged, err
	}
	if expectedSmartScheduleControlRevision != nil {
		if err := refreshChannelSmartScheduleOptionsLocked(); err != nil {
			return routingChanged, err
		}
	}
	return routingChanged, nil
}

// RefreshChannelSmartScheduleOptions reloads one database snapshot into the
// process option cache before a fresh form is handled by a stale instance.
func RefreshChannelSmartScheduleOptions() error {
	channelMonitorOptionMu.Lock()
	defer channelMonitorOptionMu.Unlock()
	return refreshChannelSmartScheduleOptionsLocked()
}

func refreshChannelSmartScheduleOptionsLocked() error {
	var options []Option
	if err := DB.Where(commonKeyCol+" LIKE ?", "ChannelMonitorSmartSchedule%").
		Order(commonKeyCol + " ASC").
		Find(&options).Error; err != nil {
		return err
	}
	values := make(map[string]string, len(options))
	for _, option := range options {
		values[option.Key] = option.Value
	}
	return refreshChannelMonitorOptions(values)
}

func lockChannelMonitorOptionsTx(tx *gorm.DB, seeds map[string]string) (map[string]Option, error) {
	keys := make([]string, 0, len(seeds))
	for key := range seeds {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		option := Option{Key: key, Value: seeds[key]}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
			return nil, err
		}
	}
	options := make(map[string]Option, len(keys))
	for _, key := range keys {
		var option Option
		if err := lockForUpdate(tx).Where(&Option{Key: key}).First(&option).Error; err != nil {
			return nil, err
		}
		options[key] = option
	}
	return options, nil
}

func saveLockedChannelMonitorOptionsTx(tx *gorm.DB, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result := tx.Model(&Option{}).Where(&Option{Key: key}).Update("value", values[key])
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

func refreshChannelMonitorOptions(values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := updateOptionMap(key, values[key]); err != nil {
			return err
		}
	}
	return nil
}
