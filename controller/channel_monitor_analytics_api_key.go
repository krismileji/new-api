package controller

import "github.com/QuantumNous/new-api/model"

// Group the inbound credential independently of the upstream credential used
// by each attempt. Known system sources are opaque identities for keyless work.
// Older smart probes and model tests were recorded as business with no inbound token ID.
const channelMonitorAnalyticsCostAPIKeyGroupSQL = "CASE WHEN api_key_id > 0 THEN '' " +
	"WHEN source_kind IN ('status_probe', 'group_probe', 'smart_probe', 'manual_test', 'model_detection') THEN source_kind " +
	"WHEN source_kind = 'business' AND api_key_name = '智能调度探测' THEN 'smart_probe' " +
	"WHEN source_kind = 'business' AND api_key_name = '模型测试' THEN 'manual_test' ELSE api_key_key END"

func channelMonitorAnalyticsSystemAPIKey(source string) bool {
	switch model.ChannelMonitorEventSource(source) {
	case model.ChannelMonitorEventSourceStatusProbe, model.ChannelMonitorEventSourceGroupProbe,
		model.ChannelMonitorEventSourceSmartProbe, model.ChannelMonitorEventSourceManualTest,
		model.ChannelMonitorEventSourceModelDetection:
		return true
	default:
		return false
	}
}

func channelMonitorAnalyticsCostAPIKeyIdentity(id int, fingerprint, source, name string) string {
	if id > 0 {
		return ""
	}
	if channelMonitorAnalyticsSystemAPIKey(source) {
		return source
	}
	if source == "business" {
		switch name {
		case "智能调度探测":
			return string(model.ChannelMonitorEventSourceSmartProbe)
		case "模型测试":
			return string(model.ChannelMonitorEventSourceManualTest)
		}
	}
	return fingerprint
}
