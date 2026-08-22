package model

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// MigrateChannelSmartScheduleGroupPolicies restores the stability window that
// was previously supplied by the runtime default. Policies are kept intact
// when the stored JSON is malformed so an unrelated migration cannot destroy
// an administrator's configuration.
func MigrateChannelSmartScheduleGroupPolicies() error {
	if DB == nil {
		return errors.New("database is not initialized")
	}

	channelMonitorOptionMu.Lock()
	defer channelMonitorOptionMu.Unlock()

	var migratedValue string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		if err := lockForUpdate(tx).
			Where(&Option{Key: ChannelMonitorSmartScheduleGroupPoliciesOption}).
			First(&option).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		value, changed, err := migrateMissingStabilityWindows(option.Value)
		if err != nil {
			common.SysError("智能调度策略迁移跳过无效 JSON: " + err.Error())
			return nil
		}
		if !changed {
			return nil
		}
		if err := tx.Model(&Option{}).
			Where(&Option{Key: option.Key}).
			Update("value", value).Error; err != nil {
			return err
		}
		migratedValue = value
		return nil
	})
	if err != nil {
		return err
	}
	if migratedValue != "" {
		common.OptionMapRWMutex.RLock()
		optionMapInitialized := common.OptionMap != nil
		common.OptionMapRWMutex.RUnlock()
		if optionMapInitialized {
			return updateOptionMap(ChannelMonitorSmartScheduleGroupPoliciesOption, migratedValue)
		}
	}
	return nil
}

func migrateMissingStabilityWindows(value string) (string, bool, error) {
	var policies []map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(value, &policies); err != nil {
		return "", false, err
	}
	defaultWindow, err := common.Marshal(ChannelMonitorSmartScheduleDefaultStabilityWindowMinutes)
	if err != nil {
		return "", false, err
	}
	changed := false
	for _, policy := range policies {
		if policy == nil {
			continue
		}
		rawWindow, exists := policy["stability_window_minutes"]
		if exists && strings.TrimSpace(string(rawWindow)) != "" &&
			strings.TrimSpace(string(rawWindow)) != "null" {
			continue
		}
		policy["stability_window_minutes"] = defaultWindow
		changed = true
	}
	if !changed {
		return "", false, nil
	}
	encoded, err := common.Marshal(policies)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}
