package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelGroupMonitorCacheRatesUsesBusinessSamplesInLast24Hours(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	originalClient, originalEnabled := common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorRead, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDBMonitorRead, common.RedisEnabled = originalClient, originalEnabled })
	now := int64(1_750_032_000)
	windowStart := now - now%60 + 60 - 24*60*60
	var events []model.ChannelMonitorEvent
	for _, fixture := range []struct {
		id    string
		group string
		at    int64
		cache *int64
		probe bool
	}{
		{"hit", "vip", now - 120, common.GetPointer(int64(20)), false},
		{"miss", "vip", now - 60, common.GetPointer(int64(0)), false},
		{"missing-usage", "vip", now - 60, nil, false},
		{"outside-window", "vip", windowStart - 1, common.GetPointer(int64(30)), false},
		{"probe", "vip", now - 60, common.GetPointer(int64(20)), true},
		{"zero", "zero", now - 60, common.GetPointer(int64(0)), false},
		{"unknown", "unknown", now - 60, nil, false},
		{"private", "private", now - 60, common.GetPointer(int64(20)), false},
	} {
		event := newChannelMonitorRedisSharedProjectionTestEvent(fixture.id, fixture.at)
		event.GroupName, event.CacheReadTokens = fixture.group, fixture.cache
		if fixture.cache != nil {
			event.InputTokens = common.GetPointer(int64(100))
		}
		if fixture.probe {
			event.Source = model.ChannelMonitorEventSourceGroupProbe
		}
		events = append(events, event)
	}
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))
	rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"vip", "zero", "unknown", "empty"}, now)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"vip": 50, "zero": 0}, rates)
}

func TestGetChannelGroupMonitorCacheRatesWithoutRedisReturnsUnavailable(t *testing.T) {
	originalEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalEnabled })
	rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"vip"}, 1_750_032_000)
	assert.ErrorIs(t, err, ErrChannelMonitorRedisSharedProjectionUnavailable)
	assert.Nil(t, rates)
	rates, err = GetChannelGroupMonitorCacheRates(context.Background(), nil, 1_750_032_000)
	require.NoError(t, err)
	assert.Empty(t, rates)
}
