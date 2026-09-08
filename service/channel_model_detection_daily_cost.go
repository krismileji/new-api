package service

import "github.com/QuantumNous/new-api/model"

// All today-cost fields come from the same Redis projection. Detailed run
// histories continue to expose their original quotas and settlement evidence.
func applyChannelModelDetectionDailyCosts(response *ChannelModelDetectionOverviewResponse, view ChannelMonitorRedisSharedDailyCostView) error {
	counts := make(map[int]ChannelMonitorRedisSharedAggregate)
	for _, detail := range view.Details {
		if detail.SourceKind != string(model.ChannelMonitorEventSourceModelDetection) {
			continue
		}
		total := counts[detail.ChannelId]
		if err := mergeChannelMonitorRedisSharedAggregate(&total, channelMonitorRedisDailyCostDetailAggregate(detail)); err != nil {
			return err
		}
		counts[detail.ChannelId] = total
	}
	for index := range response.Channels {
		channel := &response.Channels[index]
		total := counts[channel.ID]
		amount := view.Channels[channel.ID].ModelDetectionSettledCostNanoCNY
		channel.TodayModelDetectionCost = nil
		if amount == 0 && total.SettledRequestCount == 0 && total.UnresolvedRequestCount == 0 {
			continue
		}
		aggregate := ChannelModelDetectionCostAggregate{
			SettledCostNanoCNY: &amount, SettledRequestCount: total.SettledRequestCount,
			UnresolvedRequestCount: total.UnresolvedRequestCount, UnresolvedCostUnknownCount: total.UnresolvedRequestCount,
			Status: ChannelModelDetectionCostStatusPartial,
		}
		if total.UnresolvedRequestCount > 0 && total.SettledRequestCount == 0 && amount == 0 {
			aggregate.SettledCostNanoCNY = nil
			aggregate.Status = ChannelModelDetectionCostStatusUnresolved
		} else if total.UnresolvedRequestCount == 0 && total.SettledRequestCount > 0 {
			aggregate.Status = ChannelModelDetectionCostStatusSettled
		}
		cost := channelModelDetectionCostResponse(aggregate)
		channel.TodayModelDetectionCost = &cost
	}
	return nil
}
