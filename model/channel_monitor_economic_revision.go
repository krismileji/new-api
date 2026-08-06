package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

const ChannelMonitorEconomicRevisionOption = "ChannelMonitorEconomicRevision"

type ChannelSmartScheduleEconomicSnapshot struct {
	Revision    string
	Monitors    []ChannelRatioMonitor
	GroupRatios map[string]float64
}

type channelMonitorEconomicRevisionLock struct {
	available bool
}

func lockChannelMonitorEconomicRevisionTx(tx *gorm.DB) (channelMonitorEconomicRevisionLock, error) {
	if !hasChannelSmartScheduleOptionTable(tx) {
		return channelMonitorEconomicRevisionLock{}, nil
	}
	_, err := lockChannelMonitorOptionsTx(tx, map[string]string{
		ChannelMonitorEconomicRevisionOption: "",
	})
	if err != nil {
		return channelMonitorEconomicRevisionLock{}, err
	}
	return channelMonitorEconomicRevisionLock{available: true}, nil
}

func (lock channelMonitorEconomicRevisionLock) bump(tx *gorm.DB) error {
	if !lock.available {
		return nil
	}
	return saveLockedChannelMonitorOptionsTx(tx, map[string]string{
		ChannelMonitorEconomicRevisionOption: common.GetUUID(),
	})
}

func lockChannelSmartScheduleRevisionsTx(tx *gorm.DB) (controlRevision string, economicRevision string, err error) {
	if !hasChannelSmartScheduleOptionTable(tx) {
		return "", "", nil
	}
	options, err := lockChannelMonitorOptionsTx(tx, map[string]string{
		ChannelMonitorEconomicRevisionOption:      "",
		ChannelSmartScheduleControlRevisionOption: "",
	})
	if err != nil {
		return "", "", err
	}
	return options[ChannelSmartScheduleControlRevisionOption].Value,
		options[ChannelMonitorEconomicRevisionOption].Value,
		nil
}

func GetChannelMonitorEconomicRevision() (string, error) {
	if !hasChannelSmartScheduleOptionTable(DB) {
		return "", nil
	}
	revision := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		options, lockErr := lockChannelMonitorOptionsTx(tx, map[string]string{
			ChannelMonitorEconomicRevisionOption: "",
		})
		if lockErr != nil {
			return lockErr
		}
		revision = options[ChannelMonitorEconomicRevisionOption].Value
		return nil
	})
	if err != nil {
		return "", err
	}
	return revision, nil
}

func UpdateChannelMonitorGroupRatioOption(value string) error {
	if err := ratio_setting.CheckGroupRatio(value); err != nil {
		return err
	}
	channelMonitorOptionMu.Lock()
	defer channelMonitorOptionMu.Unlock()

	committedValues := map[string]string{"GroupRatio": value}
	err := DB.Transaction(func(tx *gorm.DB) error {
		options, lockErr := lockChannelMonitorOptionsTx(tx, map[string]string{
			ChannelMonitorEconomicRevisionOption: "",
			"GroupRatio":                         ratio_setting.GroupRatio2JSONString(),
		})
		if lockErr != nil {
			return lockErr
		}
		if options["GroupRatio"].Value != value {
			committedValues[ChannelMonitorEconomicRevisionOption] = common.GetUUID()
		}
		return saveLockedChannelMonitorOptionsTx(tx, committedValues)
	})
	if err != nil {
		return err
	}
	return refreshChannelMonitorOptions(committedValues)
}

func GetChannelSmartScheduleEconomicSnapshot() (snapshot ChannelSmartScheduleEconomicSnapshot, err error) {
	ratioSeed, err := common.Marshal(ratio_setting.GetGroupRatioCopy())
	if err != nil {
		return snapshot, err
	}
	if !hasChannelSmartScheduleOptionTable(DB) {
		snapshot.GroupRatios = ratio_setting.GetGroupRatioCopy()
		snapshot.Monitors, err = GetChannelRatioMonitorCostMetadata()
		return snapshot, err
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		options, lockErr := lockChannelMonitorOptionsTx(tx, map[string]string{
			ChannelMonitorEconomicRevisionOption: "",
			"GroupRatio":                         string(ratioSeed),
		})
		if lockErr != nil {
			return lockErr
		}
		snapshot.Revision = options[ChannelMonitorEconomicRevisionOption].Value
		if unmarshalErr := common.UnmarshalJsonStr(options["GroupRatio"].Value, &snapshot.GroupRatios); unmarshalErr != nil {
			return fmt.Errorf("解析 GroupRatio 失败: %w", unmarshalErr)
		}
		if snapshot.GroupRatios == nil {
			snapshot.GroupRatios = map[string]float64{}
		}
		return tx.Select("channel_id", "ratio", "cost_conversion", "updated_time").
			Find(&snapshot.Monitors).Error
	})
	return snapshot, err
}
