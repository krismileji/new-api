package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/model"
)

// Read disjoint API-key contributions together with their enclosing totals,
// so keyless attempts remain visible as unattributed rows in drill-downs.
func QueryChannelMonitorRedisMinuteAnalytics(ctx context.Context, startAt, endAt int64, filter model.ChannelMonitorSuccessFilter, includeFailures bool) (ChannelMonitorRedisSharedProjectionView, error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return ChannelMonitorRedisSharedProjectionView{}, err
	}
	channelIdentity := "*"
	if filter.ChannelId > 0 {
		channelIdentity = strconv.Itoa(filter.ChannelId)
	}
	groupIdentity := "*"
	if filter.Group != "" {
		groupIdentity = channelMonitorRedisSharedDimension(filter.Group)
	}
	patterns := []string{
		channelMonitorRedisSharedScopeMetadata + ":*",
		fmt.Sprintf("%s:*.%s.*.%s:*", channelMonitorRedisSharedScopeAPIKeyRoute, channelIdentity, groupIdentity),
	}
	if includeFailures {
		patterns = append(patterns, fmt.Sprintf("%s:%s.*.%s.*.*.*:*", channelMonitorRedisSharedScopeFailure, channelIdentity, groupIdentity))
	}
	if filter.Group == "" {
		patterns = append(patterns, fmt.Sprintf("%s:%s.*:*", channelMonitorRedisSharedScopeRoute, channelIdentity))
	} else {
		patterns = append(patterns, fmt.Sprintf("%s:%s.%s:*", channelMonitorRedisSharedScopeGroupRoute, groupIdentity, channelIdentity))
	}
	return projection.querySelected(ctx, startAt, endAt, channelMonitorRedisSharedQuerySelection{patterns: patterns})
}
