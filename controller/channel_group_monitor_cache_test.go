package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelGroupMonitorSnapshotOnlyReadsChangedGroups(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorExecution{}))
	config, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: true, ShowCacheRate: true, Groups: []model.ChannelGroupMonitorGroup{
			{GroupName: "vip", ProbeModel: "gpt-test"}, {GroupName: "quiet", ProbeModel: "gpt-test"},
		}, IntervalSeconds: 300, DisplayValue: 24, DisplayUnit: "hour",
	}, common.GetTimestamp())
	require.NoError(t, err)
	prepareChannelGroupMonitorPageSnapshot(t)
	client := common.RedisMonitorWriteClient()
	generation, err := service.SyncChannelGroupMonitorGeneration(t.Context(), config)
	require.NoError(t, err)
	var first channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &first))
	first.NextRefreshAt = common.GetTimestamp() + 3600
	require.NoError(t, service.PublishChannelGroupMonitorSnapshot(t.Context(), generation, first))
	start := model.ChannelStatusProbeDisplayBucketStart(common.GetTimestamp(), "hour")
	quietBucket := generation.Key("group:" + base64.RawURLEncoding.EncodeToString([]byte("quiet")) + ":probe:" + strconv.FormatInt(start, 10))
	require.NoError(t, client.Set(t.Context(), quietBucket, "unreadable probe bucket", 0).Err())
	event := model.NewChannelMonitorEvent(1, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, common.GetTimestamp())
	event.GroupName, event.GroupMonitorGeneration, event.APIKeyId = "vip", generation.ID, 1
	event.IsStream, event.RequestDispatched, event.GroupCacheExcluded = true, true, common.GetPointer(false)
	event.InputTokens, event.CacheReadTokens = common.GetPointer(int64(100)), common.GetPointer(int64(25))
	require.NoError(t, service.ProjectChannelGroupMonitorEvents(t.Context(), client, []model.ChannelMonitorEvent{event}, common.GetTimestamp()))
	require.NoError(t, client.Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context()))
	var updated channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &updated))
	require.Len(t, updated.Public.Items, 2)
	assert.Equal(t, first.Overview.Items[1], updated.Overview.Items[1])
	require.NotNil(t, updated.Public.Items[0].CacheRateAverage)
	assert.Equal(t, 25.0, *updated.Public.Items[0].CacheRateAverage)
	// Once quiet has its own new event, it must no longer reuse the old result.
	event.EventId, event.GroupName = "quiet-changed", "quiet"
	require.NoError(t, service.ProjectChannelGroupMonitorEvents(t.Context(), client, []model.ChannelMonitorEvent{event}, common.GetTimestamp()))
	require.NoError(t, client.Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	assert.Error(t, refreshChannelGroupMonitorSnapshot(t.Context()))
	require.NoError(t, client.Del(t.Context(), quietBucket, service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context()))
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &updated))
	require.NotNil(t, updated.Public.Items[1].CacheRateAverage)
	assert.Equal(t, 25.0, *updated.Public.Items[1].CacheRateAverage)
}

func TestChannelGroupMonitorSnapshotSkipsUnchangedBucketsAndRefreshesDirtyData(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorExecution{}))
	config, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: true, ShowCacheRate: true, Groups: []model.ChannelGroupMonitorGroup{{GroupName: "vip", ProbeModel: "gpt-test"}},
		IntervalSeconds: 300, DisplayValue: 24, DisplayUnit: "hour",
	}, common.GetTimestamp())
	require.NoError(t, err)
	prepareChannelGroupMonitorPageSnapshot(t)
	client := common.RedisMonitorWriteClient()
	generation, err := service.SyncChannelGroupMonitorGeneration(t.Context(), config)
	require.NoError(t, err)
	var first channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &first))
	// Keep the test independent of a wall-clock bucket boundary.
	first.NextRefreshAt = common.GetTimestamp() + 3600
	require.NoError(t, service.PublishChannelGroupMonitorSnapshot(t.Context(), generation, first))
	before, err := client.Get(t.Context(), service.ChannelGroupMonitorSnapshotKey).Result()
	require.NoError(t, err)
	start := model.ChannelStatusProbeDisplayBucketStart(common.GetTimestamp(), "hour")
	unusedBucket := generation.Key("bucket:" + strconv.FormatInt(start-3600, 10))
	require.NoError(t, client.Set(t.Context(), unusedBucket, "unreadable bucket", 0).Err())
	require.NoError(t, client.Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context()), "unchanged snapshots must not query statistic buckets")
	assert.Equal(t, before, client.Get(t.Context(), service.ChannelGroupMonitorSnapshotKey).Val())
	require.NoError(t, client.Del(t.Context(), unusedBucket).Err())
	event := model.NewChannelMonitorEvent(1, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, common.GetTimestamp())
	event.GroupName, event.GroupMonitorGeneration = "vip", generation.ID
	event.APIKeyId, event.IsStream, event.RequestDispatched = 1, true, true
	event.GroupCacheExcluded = common.GetPointer(false)
	event.InputTokens, event.CacheReadTokens = common.GetPointer(int64(100)), common.GetPointer(int64(25))
	require.NoError(t, service.ProjectChannelGroupMonitorEvents(t.Context(), client, []model.ChannelMonitorEvent{event}, common.GetTimestamp()))
	require.NoError(t, client.Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context()))
	var changed channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &changed))
	assert.NotEqual(t, first.EventVersion, changed.EventVersion)
	require.Len(t, changed.Public.Items, 1)
	require.NotNil(t, changed.Public.Items[0].CacheRateAverage)
	assert.Equal(t, 25.0, *changed.Public.Items[0].CacheRateAverage)
	// A due snapshot is rebuilt even though no further event arrived.
	changed.NextRefreshAt = common.GetTimestamp()
	require.NoError(t, service.PublishChannelGroupMonitorSnapshot(t.Context(), generation, changed))
	require.NoError(t, client.Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context()))
	var rolled channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &rolled))
	assert.Greater(t, rolled.NextRefreshAt, changed.NextRefreshAt)
	assert.Equal(t, changed.EventVersion, rolled.EventVersion)
	// Description edits have no stream event but must still refresh the page.
	originalGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalGroups)) })
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"vip":"新的分组说明"}`))
	require.NoError(t, client.Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context()))
	var metadataChanged channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &metadataChanged))
	require.Len(t, metadataChanged.Public.Items, 1)
	assert.Equal(t, "新的分组说明", metadataChanged.Public.Items[0].Description)
	assert.Equal(t, rolled.Generation, metadataChanged.Generation)
	assert.Equal(t, rolled.EventVersion, metadataChanged.EventVersion)
	assert.Equal(t, rolled.Public.Items[0].CacheRateAverage, metadataChanged.Public.Items[0].CacheRateAverage)
}

func TestChannelGroupMonitorNextRefreshUsesWindowAndProbeDeadlines(t *testing.T) {
	config := model.ChannelGroupMonitorConfig{Enabled: true, IntervalSeconds: 300, DisplayValue: 24, DisplayUnit: "hour"}
	now := int64(10000)
	assert.Equal(t, int64(10800), channelGroupMonitorNextRefreshAt(config, nil, now))
	assert.Equal(t, int64(10101), channelGroupMonitorNextRefreshAt(config, []channelGroupMonitorItemResponse{{Status: "healthy", LastFinishedAt: 9500}}, now))
	assert.Equal(t, int64(10800), channelGroupMonitorNextRefreshAt(config, []channelGroupMonitorItemResponse{{Status: "paused", LastFinishedAt: 9500}}, now))
	assert.Equal(t, int64(10200), channelGroupMonitorNextRefreshAt(config, []channelGroupMonitorItemResponse{{Passive: &channelGroupPassiveResponse{IntervalSeconds: 300}}}, now))
}

// Existing display fixtures describe a previously collected window. Install that
// window explicitly in Redis and send its logical results through the projection;
// production never imports historical execution rows when Redis is empty.
func seedChannelGroupMonitorProjection(t *testing.T, config model.ChannelGroupMonitorConfig, now int64) service.ChannelGroupMonitorGeneration {
	t.Helper()
	common.RedisEnabled = true
	generation, err := service.SyncChannelGroupMonitorGeneration(t.Context(), config)
	require.NoError(t, err)
	generation.StartedAt = 1
	payload, err := common.Marshal(generation)
	require.NoError(t, err)
	require.NoError(t, common.RedisMonitorWriteClient().Set(t.Context(), service.ChannelGroupMonitorRedisPrefix+"config", payload, 0).Err())
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelGroupMonitorExecution{}))
	var rows []model.ChannelGroupMonitorExecution
	require.NoError(t, model.DB.Order("finished_at ASC, id ASC").Find(&rows).Error)
	var events []model.ChannelMonitorEvent
	for _, row := range rows {
		if row.StartedAt <= 0 {
			row.StartedAt = row.FinishedAt
		}
		row.ConfigRevision = config.Revision
		event := model.NewChannelMonitorEvent(0, model.ChannelMonitorEventSourceGroupSummary, model.ChannelMonitorEventOutcomeSuccess, row.FinishedAt)
		event.CreatedAt = now
		event.EventId = row.RunId + ":" + row.GroupName
		event.GroupName, event.GroupMonitorGeneration, event.GroupMonitorProbe = row.GroupName, generation.ID, &row
		events = append(events, event)
	}
	require.NoError(t, service.ProjectChannelGroupMonitorEvents(t.Context(), common.RedisMonitorWriteClient(), events, now))
	return generation
}

func prepareChannelGroupMonitorPageSnapshot(t *testing.T) {
	t.Helper()
	config, err := model.GetChannelGroupMonitorConfigOrDefaultWithContext(t.Context())
	require.NoError(t, err)
	seedChannelGroupMonitorProjection(t, config, common.GetTimestamp())
	require.NoError(t, common.RedisMonitorWriteClient().Del(t.Context(), service.ChannelGroupMonitorRedisPrefix+"snapshot_lease").Err())
	require.NoError(t, refreshChannelGroupMonitorSnapshot(t.Context(), true))
}

func TestChannelGroupMonitorPagesReadSnapshotsWithoutDatabase(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorExecution{}))
	_, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: true, Groups: []model.ChannelGroupMonitorGroup{{GroupName: "vip", ProbeModel: "gpt-test"}},
		IntervalSeconds: 60, DisplayValue: 60, DisplayUnit: "minute",
	}, common.GetTimestamp())
	require.NoError(t, err)
	prepareChannelGroupMonitorPageSnapshot(t)
	var snapshot channelGroupMonitorSnapshot
	require.NoError(t, service.ReadChannelGroupMonitorSnapshot(t.Context(), &snapshot))
	snapshot.UserRatios = map[string]map[string]float64{"private": {"vip": 0.25}}
	config, err := model.GetChannelGroupMonitorConfigOrDefaultWithContext(t.Context())
	require.NoError(t, err)
	generation, err := service.SyncChannelGroupMonitorGeneration(t.Context(), config)
	require.NoError(t, err)
	require.NoError(t, service.PublishChannelGroupMonitorSnapshot(t.Context(), generation, snapshot))
	previousDB := model.DB
	model.DB = nil
	defer func() { model.DB = previousDB }()
	for _, handler := range []gin.HandlerFunc{GetPricingGroupMonitor, GetChannelGroupMonitorOverview, GetChannelGroupMonitorSettings} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/group-monitor", nil)
		c.Set("id", 17)
		c.Set("user_group", "private")
		handler(c)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		assert.Contains(t, recorder.Body.String(), "vip")
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/group-monitor", nil)
	c.Set("user_group", "private")
	GetPricingGroupMonitor(c)
	assert.Contains(t, recorder.Body.String(), `"group_ratio":0.25`)
	assert.NotContains(t, recorder.Body.String(), "user_ratios")
	require.NoError(t, common.RedisMonitorWriteClient().Del(t.Context(), service.ChannelGroupMonitorSnapshotKey).Err())
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/group-monitor", nil)
	GetPricingGroupMonitor(c)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestChannelGroupMonitorCacheRateSettingsPreserveOmittedAndExplicitFalse(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
	for index, tc := range []struct {
		name    string
		value   *bool
		want    bool
		min     *int
		wantMin int
	}{
		{"legacy default", nil, false, nil, 0},
		{"enable", common.GetPointer(true), true, common.GetPointer(32), 32},
		{"legacy update preserves enabled", nil, true, nil, 32},
		{"explicit disable and reset", common.GetPointer(false), false, common.GetPointer(0), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := map[string]any{
				"enabled": false, "groups": []model.ChannelGroupMonitorGroup{},
				"interval_seconds": 60, "display_value": 60, "display_unit": "minute", "revision": index,
			}
			if tc.value != nil {
				request["show_cache_rate"] = *tc.value
			}
			if tc.min != nil {
				request["cache_min_context_k"] = *tc.min
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
			assert.Equal(t, tc.wantMin, saved.Data.CacheMinContextK)

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
			assert.Equal(t, tc.wantMin, loaded.Data.Settings.CacheMinContextK)
		})
	}
}

func TestGetPricingGroupMonitorCacheRateVisibilityAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name           string
		show           bool
		redisAvailable bool
		wantRate       bool
		minContextK    int
		stream         bool
	}{
		{"enabled", true, true, true, 0, true},
		{"non-stream excluded without threshold", true, true, false, 0, false},
		{"disabled hides existing rates", false, true, false, 0, false},
		{"unavailable retains monitoring", true, false, false, 0, false},
		{"stream at threshold", true, true, true, 10, true},
		{"stream below threshold", true, true, false, 11, true},
		{"non-stream excluded", true, true, false, 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(
				&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
			))
			now := common.GetTimestamp()
			config, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
				Enabled: true, ShowCacheRate: tc.show,
				CacheMinContextK: tc.minContextK,
				Groups:           []model.ChannelGroupMonitorGroup{{GroupName: "vip", ProbeModel: "gpt-4.1"}},
				IntervalSeconds:  60, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
			}, now)
			require.NoError(t, err)
			// Writing the Redis projection alone isolates the response contract from
			// the independent daily database rebuild worker.
			generation := seedChannelGroupMonitorProjection(t, config, now)
			require.NoError(t, service.ProjectChannelGroupMonitorEvents(context.Background(), common.RDB, []model.ChannelMonitorEvent{{
				GroupMonitorGeneration: generation.ID, EventId: "cache-hit", EventSequence: 1, SchemaVersion: model.ChannelMonitorEventSchemaVersion,
				OccurredAt: now - 60, CreatedAt: now, ChannelId: 11, GroupName: "vip", ModelName: "gpt-4.1",
				APIKeyId: 42, APIKeyName: "不公开的令牌名称",
				Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
				CostStatus: model.ChannelMonitorEventCostNone, RequestDispatched: true, IsFinalAttempt: true,
				GroupCacheExcluded: common.GetPointer(tc.minContextK > 0 && (!tc.stream || tc.minContextK > 10)),
				InputTokens:        common.GetPointer(int64(10000)), CacheReadTokens: common.GetPointer(int64(20)), IsStream: tc.stream,
			}}, now))
			prepareChannelGroupMonitorPageSnapshot(t)
			common.RedisEnabled = tc.redisAvailable
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
			GetPricingGroupMonitor(c)
			if !tc.redisAvailable {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var payload struct {
				Data struct {
					ShowCacheRate    bool             `json:"show_cache_rate"`
					CacheMinContextK int              `json:"cache_min_context_k"`
					Items            []map[string]any `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.Equal(t, tc.show, payload.Data.ShowCacheRate)
			assert.Equal(t, tc.minContextK, payload.Data.CacheMinContextK)
			require.Len(t, payload.Data.Items, 1)
			assert.Equal(t, "vip", payload.Data.Items[0]["group"])
			assert.Contains(t, payload.Data.Items[0], "recent_window")
			if tc.wantRate {
				assert.Equal(t, 0.2, payload.Data.Items[0]["cache_rate"])
				assert.Equal(t, 0.2, payload.Data.Items[0]["cache_rate_max"])
				assert.Equal(t, 0.2, payload.Data.Items[0]["cache_rate_average"])
			} else {
				assert.NotContains(t, payload.Data.Items[0], "cache_rate")
				assert.NotContains(t, payload.Data.Items[0], "cache_rate_max")
				assert.NotContains(t, payload.Data.Items[0], "cache_rate_average")
			}
			assert.NotContains(t, recorder.Body.String(), "不公开的令牌名称")
			assert.NotContains(t, payload.Data.Items[0], "api_key_id")
			assert.NotContains(t, payload.Data.Items[0], "channel_id")
		})
	}
}

func TestChannelGroupMonitorCacheRateRejectsInvalidContextThreshold(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
	for _, value := range []any{-1, 1.5, model.ChannelGroupMonitorMaxCacheContextK + 1, "32", 1e30} {
		body, err := common.Marshal(map[string]any{
			"enabled": false, "groups": []model.ChannelGroupMonitorGroup{},
			"interval_seconds": 60, "display_value": 60, "display_unit": "minute", "revision": 0,
			"cache_min_context_k": value,
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/channel_monitor/group_monitor/settings", bytes.NewReader(body))
		UpdateChannelGroupMonitorSettings(c)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	var count int64
	require.NoError(t, db.Model(&model.ChannelGroupMonitorConfig{}).Count(&count).Error)
	assert.Zero(t, count)
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
			generation := seedChannelGroupMonitorProjection(t, config, now)
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
					GroupMonitorGeneration: generation.ID, GroupCacheExcluded: common.GetPointer(false), EventId: fixture.id, EventSequence: uint64(len(events) + 1), SchemaVersion: model.ChannelMonitorEventSchemaVersion,
					OccurredAt: fixture.at, CreatedAt: now, ChannelId: 11, GroupName: "vip", ModelName: "gpt-4.1",
					Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
					CostStatus: model.ChannelMonitorEventCostNone, RequestDispatched: true, IsFinalAttempt: true,
					IsStream:    true,
					InputTokens: common.GetPointer(int64(100)), CacheReadTokens: common.GetPointer(fixture.cache),
				})
			}
			require.NoError(t, service.ProjectChannelGroupMonitorEvents(context.Background(), common.RDB, events, now))
			common.RedisEnabled = true
			items, err := buildChannelGroupMonitorItems(context.Background(), config, map[string][]string{"vip": {"gpt-4.1"}}, now)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.NotNil(t, items[0].CacheRate)
			assert.InDelta(t, 40.0/300*100, *items[0].CacheRate, 0.000001)
			assert.Equal(t, tc.start, items[0].RecentWindow[0].StartedAt)
		})
	}
}
