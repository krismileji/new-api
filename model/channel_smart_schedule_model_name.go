package model

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func channelSmartScheduleModelName(modelName string) string {
	return ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
}

// channelSmartScheduleRouteModelNames follows channel selection semantics:
// use an explicitly configured model first and only fall back to its matching
// wildcard when the exact route has no usable channel.
func channelSmartScheduleRouteModelNames(modelName string) []string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	normalized := channelSmartScheduleModelName(modelName)
	if normalized == "" || normalized == modelName {
		return []string{modelName}
	}
	return []string{modelName, normalized}
}
