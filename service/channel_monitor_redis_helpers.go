package service

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func channelMonitorRealtimeAddFloat64(left float64, right float64) float64 {
	if right <= 0 {
		return left
	}
	if left > math.MaxFloat64-right {
		return math.MaxFloat64
	}
	return left + right
}

func channelMonitorRealtimeCostEventId(event model.ChannelMonitorEvent) string {
	if strings.TrimSpace(event.OtherJson) == "" {
		return ""
	}
	var other struct {
		CostEventId string `json:"cost_event_id"`
	}
	if err := common.UnmarshalJsonStr(event.OtherJson, &other); err != nil || strings.TrimSpace(other.CostEventId) == "" {
		return ""
	}
	return string(event.Source) + ":" + strings.TrimSpace(other.CostEventId)
}

func channelMonitorRealtimeCostStatusRank(status model.ChannelMonitorEventCostStatus) int {
	switch status {
	case model.ChannelMonitorEventCostSettled:
		return 2
	case model.ChannelMonitorEventCostUnresolved:
		return 1
	default:
		return 0
	}
}
