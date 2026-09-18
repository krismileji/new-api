package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelGroupMonitorCacheRateSettingsPreserveOmittedAndExplicitFalse(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
	for index, tc := range []struct {
		name  string
		value *bool
		want  bool
	}{
		{"legacy default", nil, false},
		{"enable", common.GetPointer(true), true},
		{"legacy update preserves enabled", nil, true},
		{"explicit disable", common.GetPointer(false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := map[string]any{
				"enabled": false, "groups": []model.ChannelGroupMonitorGroup{},
				"interval_seconds": 60, "display_value": 60, "display_unit": "minute", "revision": index,
			}
			if tc.value != nil {
				request["show_cache_rate"] = *tc.value
			}
			body, err := common.Marshal(request)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/channel_monitor/group_monitor/settings", bytes.NewReader(body))
			UpdateChannelGroupMonitorSettings(c)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var saved struct {
				Data channelGroupMonitorConfigResponse `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saved))
			assert.Equal(t, tc.want, saved.Data.ShowCacheRate)

			recorder = httptest.NewRecorder()
			c, _ = gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/group_monitor/settings", nil)
			GetChannelGroupMonitorSettings(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			var loaded struct {
				Data struct {
					Settings channelGroupMonitorConfigResponse `json:"settings"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &loaded))
			assert.Equal(t, tc.want, loaded.Data.Settings.ShowCacheRate)
		})
	}
}

func TestGetPricingGroupMonitorCacheRateVisibilityAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name           string
		show           bool
		redisAvailable bool
		wantRate       bool
	}{
		{"enabled", true, true, true},
		{"disabled hides existing rates", false, true, false},
		{"unavailable retains monitoring", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(
				&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
			))
			now := common.GetTimestamp()
			_, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
				Enabled: true, ShowCacheRate: tc.show,
				Groups:          []model.ChannelGroupMonitorGroup{{GroupName: "vip", ProbeModel: "gpt-4.1"}},
				IntervalSeconds: 60, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
			}, now)
			require.NoError(t, err)
			// Writing the Redis projection alone isolates the response contract from
			// the independent daily database rebuild worker.
			common.RedisEnabled = false
			projection := service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB)
			require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{{
				EventId: "cache-hit", EventSequence: 1, SchemaVersion: model.ChannelMonitorEventSchemaVersion,
				OccurredAt: now - 60, CreatedAt: now, ChannelId: 11, GroupName: "vip", ModelName: "gpt-4.1",
				Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
				CostStatus: model.ChannelMonitorEventCostNone, RequestDispatched: true, IsFinalAttempt: true,
				InputTokens: common.GetPointer(int64(100)), CacheReadTokens: common.GetPointer(int64(20)),
			}}))
			common.RedisEnabled = tc.redisAvailable
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
			GetPricingGroupMonitor(c)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var payload struct {
				Data struct {
					ShowCacheRate bool             `json:"show_cache_rate"`
					Items         []map[string]any `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.Equal(t, tc.show, payload.Data.ShowCacheRate)
			require.Len(t, payload.Data.Items, 1)
			assert.Equal(t, "vip", payload.Data.Items[0]["group"])
			assert.Contains(t, payload.Data.Items[0], "recent_window")
			if tc.wantRate {
				assert.Equal(t, float64(100), payload.Data.Items[0]["cache_rate"])
			} else {
				assert.NotContains(t, payload.Data.Items[0], "cache_rate")
			}
			assert.NotContains(t, payload.Data.Items[0], "channel_id")
		})
	}
}

func TestChannelGroupMonitorCacheRateFollowsDisplayWindow(t *testing.T) {
	zone := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.September, 19, 10, 37, 25, 0, zone).Unix()
	for _, tc := range []struct {
		name  string
		value int
		unit  string
		start int64
	}{
		{"minutes", 15, model.ChannelStatusProbeDisplayUnitMinute, time.Date(2026, time.September, 19, 10, 23, 0, 0, zone).Unix()},
		{"hours", 3, model.ChannelStatusProbeDisplayUnitHour, time.Date(2026, time.September, 19, 8, 0, 0, 0, zone).Unix()},
		{"day", 1, model.ChannelStatusProbeDisplayUnitDay, time.Date(2026, time.September, 19, 0, 0, 0, 0, zone).Unix()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(
				&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
			))
			config, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
				Enabled: true, ShowCacheRate: true,
				Groups:          []model.ChannelGroupMonitorGroup{{GroupName: "vip", ProbeModel: "gpt-4.1"}},
				IntervalSeconds: 60, DisplayValue: tc.value, DisplayUnit: tc.unit,
			}, now)
			require.NoError(t, err)
			common.RedisEnabled = false
			projection := service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB)
			var events []model.ChannelMonitorEvent
			for _, fixture := range []struct {
				id    string
				at    int64
				cache int64
			}{
				{"before-window", tc.start - 1, 20},
				{"at-start", tc.start, 0},
				{"recent-hit", now - 60, 20},
				{"current-hit", now, 20},
			} {
				events = append(events, model.ChannelMonitorEvent{
					EventId: fixture.id, EventSequence: uint64(len(events) + 1), SchemaVersion: model.ChannelMonitorEventSchemaVersion,
					OccurredAt: fixture.at, CreatedAt: now, ChannelId: 11, GroupName: "vip", ModelName: "gpt-4.1",
					Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
					CostStatus: model.ChannelMonitorEventCostNone, RequestDispatched: true, IsFinalAttempt: true,
					InputTokens: common.GetPointer(int64(100)), CacheReadTokens: common.GetPointer(fixture.cache),
				})
			}
			require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))
			common.RedisEnabled = true
			items, err := buildChannelGroupMonitorItems(context.Background(), config, map[string][]string{"vip": {"gpt-4.1"}}, now)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.NotNil(t, items[0].CacheRate)
			assert.InDelta(t, 200.0/3, *items[0].CacheRate, 0.000001)
			assert.Equal(t, tc.start, items[0].RecentWindow[0].StartedAt)
		})
	}
}
