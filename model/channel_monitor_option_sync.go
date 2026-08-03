package model

import "strings"

func isChannelMonitorManagedOption(key string) bool {
	return strings.HasPrefix(key, "ChannelMonitor") || key == "GroupRatio"
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
