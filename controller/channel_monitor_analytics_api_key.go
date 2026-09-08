package controller

import "github.com/QuantumNous/new-api/model"

// Group the inbound credential independently of the upstream credential used
// by each attempt. Known system sources are opaque identities for keyless work.
const channelMonitorAnalyticsCostAPIKeyGroupSQL = "CASE WHEN api_key_id > 0 THEN '' WHEN source_kind IN ('status_probe', 'group_probe', 'smart_probe', 'manual_test', 'model_detection') THEN source_kind ELSE api_key_key END"

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

func channelMonitorAnalyticsCostAPIKeyIdentity(id int, fingerprint, source string) string {
	if id > 0 {
		return ""
	}
	if channelMonitorAnalyticsSystemAPIKey(source) {
		return source
	}
	return fingerprint
}
