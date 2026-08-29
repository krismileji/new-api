package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func isChannelMonitorManagedOption(key string) bool {
	return strings.HasPrefix(key, "ChannelMonitor") ||
		key == "GroupRatio" ||
		key == common.RelayResponseHeaderTimeoutOptionKey
}

func updateOptionMapFromDatabase(key string, value string) error {
	if isChannelMonitorManagedOption(key) {
		channelMonitorOptionMu.Lock()
		defer channelMonitorOptionMu.Unlock()

		var option Option
		if err := DB.Where(&Option{Key: key}).First(&option).Error; err != nil {
			return err
		}
		value = option.Value
	}
	return updateOptionMap(key, value)
}
