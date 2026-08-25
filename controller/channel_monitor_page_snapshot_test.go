package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var channelMonitorPageSnapshotTestUserID atomic.Int64

func TestShouldSyncChannelMonitorPageSnapshotsOnlyAfterSuccessfulActions(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		response   string
		shouldSync bool
	}{
		{name: "mutation success", method: http.MethodPut, path: "/api/channel_monitor/channel/1/schedule/route/primary", response: `{"success":true}`, shouldSync: true},
		{name: "mutation business error", method: http.MethodPut, path: "/api/channel_monitor/channel/1/schedule/route/primary", response: `{"success":false}`, shouldSync: false},
		{name: "regular monitor read", method: http.MethodGet, path: "/api/channel_monitor/schedule", response: `{"success":true}`, shouldSync: false},
		{name: "channel test", method: http.MethodGet, path: "/api/channel/test/1", response: `{"success":true}`, shouldSync: true},
		{name: "channel model fetch", method: http.MethodGet, path: "/api/channel/fetch_models/1", response: `{"success":true}`, shouldSync: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(test.method, test.path, nil)
			assert.Equal(t, test.shouldSync, shouldSyncChannelMonitorPageSnapshots(context, []byte(test.response)))
		})
	}
}

func TestChannelMonitorPageSnapshotContractCoversMonitorPages(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	pages := []struct {
		page      string
		targetURL string
	}{
		{channelMonitorPageSnapshotOverview, "/api/channel_monitor/"},
		{channelMonitorPageSnapshotCost, "/api/channel_monitor/cost"},
		{channelMonitorPageSnapshotPerformance, "/api/channel_monitor/performance"},
		{channelMonitorPageSnapshotSuccess, "/api/channel_monitor/success/today"},
		{channelMonitorPageSnapshotSuccessDetail, "/api/channel_monitor/success/detail?channel_id=1"},
		{channelMonitorPageSnapshotSchedule, "/api/channel_monitor/schedule"},
	}
	var builds atomic.Int32
	handler := func(c *gin.Context) {
		builds.Add(1)
		common.ApiSuccess(c, gin.H{
			"label":           "complete",
			"data_cutoff_at":  int64(123),
			"event_watermark": uint64(456),
		})
	}
	for _, page := range pages {
		recorder, target := newChannelMonitorPageSnapshotContext(
			t,
			page.targetURL,
			int(channelMonitorPageSnapshotTestUserID.Add(1)),
		)
		require.True(t, serveChannelMonitorPageSnapshot(target, page.page, handler))
		assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		query := channelMonitorPageSnapshotQuery(target, page.page)
		waitForChannelMonitorPageSnapshot(t, query)
		recorder, target = newChannelMonitorPageSnapshotContext(
			t,
			page.targetURL,
			target.GetInt("id"),
		)
		require.True(t, serveChannelMonitorPageSnapshot(target, page.page, handler))
		response := decodeChannelMonitorPageSnapshotResponse(t, recorder)
		assert.True(t, response.Success)
		assert.Positive(t, response.Data.GeneratedAt)
		assert.Equal(t, int64(123), response.Data.DataCutoffAt)
		assert.Equal(t, uint64(456), response.Data.EventWatermark)
		assert.False(t, response.Data.Stale)
	}
	assert.Equal(t, int32(len(pages)), builds.Load())
}

func TestChannelMonitorSmartScheduleRoutesColdMissBuildsOnlyInBackground(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "false",
		channelMonitorSmartScheduleGroupPoliciesOption: "[]",
	})
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	invalidRecorder, invalidTarget := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/schedule?metrics=invalid", userID,
	)
	GetChannelMonitorSmartScheduleRoutes(invalidTarget)
	assert.Equal(t, http.StatusBadRequest, invalidRecorder.Code)
	assert.Contains(t, invalidRecorder.Body.String(), "metrics 参数必须是布尔值")

	targetURL := "/api/channel_monitor/schedule?metrics=false"
	recorder, target := newChannelMonitorPageSnapshotContext(t, targetURL, userID)

	GetChannelMonitorSmartScheduleRoutes(target)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "CHANNEL_MONITOR_SNAPSHOT_REFRESHING")
	query := channelMonitorPageSnapshotQuery(target, channelMonitorPageSnapshotSchedule)
	waitForChannelMonitorPageSnapshot(t, query)

	recorder, target = newChannelMonitorPageSnapshotContext(t, targetURL, userID)
	GetChannelMonitorSmartScheduleRoutes(target)
	assert.Equal(t, http.StatusOK, recorder.Code)
	response := decodeChannelMonitorPageSnapshotResponse(t, recorder)
	assert.True(t, response.Success)
	assert.False(t, response.Data.Stale)
}

func TestChannelMonitorPageSnapshotHitsSamePermissionAndIsolatesUsers(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	var builds atomic.Int32
	handler := func(c *gin.Context) {
		common.ApiSuccess(c, gin.H{
			"build":           builds.Add(1),
			"data_cutoff_at":  int64(10),
			"event_watermark": uint64(20),
		})
	}
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	firstRecorder, first := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(first, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, http.StatusServiceUnavailable, firstRecorder.Code)
	waitForChannelMonitorPageSnapshot(t, channelMonitorPageSnapshotQuery(first, channelMonitorPageSnapshotPerformance))
	secondRecorder, second := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(second, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, int32(1), builds.Load())
	cachedRecorder, cached := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(cached, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, secondRecorder.Body.String(), cachedRecorder.Body.String())

	otherUserID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	otherRecorder, otherUser := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", otherUserID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(otherUser, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, http.StatusServiceUnavailable, otherRecorder.Code)
	waitForChannelMonitorPageSnapshot(t, channelMonitorPageSnapshotQuery(otherUser, channelMonitorPageSnapshotPerformance))
	otherRecorder, otherUser = newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", otherUserID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(otherUser, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, int32(2), builds.Load())
	assert.NotEqual(t, secondRecorder.Body.String(), otherRecorder.Body.String())
}

func TestChannelMonitorPageSnapshotQueryNormalizesEquivalentFilters(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	_, first := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/cost?days=030&summary_only=TRUE&page=01",
		userID,
	)
	_, second := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/cost?page=1&summary_only=true&days=30",
		userID,
	)
	firstKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(first, channelMonitorPageSnapshotCost),
	)
	require.NoError(t, err)
	secondKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(second, channelMonitorPageSnapshotCost),
	)
	require.NoError(t, err)
	assert.Equal(t, firstKey, secondKey)
}

func TestChannelMonitorPageSnapshotQueryKeepsRejectedWhitespaceDistinct(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	tests := []struct {
		page       string
		validURL   string
		invalidURL string
	}{
		{
			page:       channelMonitorPageSnapshotPerformance,
			validURL:   "/api/channel_monitor/performance?minutes=15",
			invalidURL: "/api/channel_monitor/performance?minutes=%2015%20",
		},
		{
			page:       channelMonitorPageSnapshotCost,
			validURL:   "/api/channel_monitor/cost?days=30",
			invalidURL: "/api/channel_monitor/cost?days=%2030%20",
		},
		{
			page:       channelMonitorPageSnapshotSuccess,
			validURL:   "/api/channel_monitor/success/today?date=2026-08-24",
			invalidURL: "/api/channel_monitor/success/today?date=%202026-08-24%20",
		},
	}
	for _, test := range tests {
		_, valid := newChannelMonitorPageSnapshotContext(t, test.validURL, userID)
		_, invalid := newChannelMonitorPageSnapshotContext(t, test.invalidURL, userID)
		validKey, err := service.ChannelMonitorPageSnapshotKey(
			channelMonitorPageSnapshotQuery(valid, test.page),
		)
		require.NoError(t, err)
		invalidKey, err := service.ChannelMonitorPageSnapshotKey(
			channelMonitorPageSnapshotQuery(invalid, test.page),
		)
		require.NoError(t, err)
		assert.NotEqual(t, validKey, invalidKey, test.page)
	}
}

func TestChannelMonitorPageSnapshotQueryKeepsInvalidOptionalChannelDistinct(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	tests := []struct {
		page       string
		validURL   string
		invalidURL string
	}{
		{
			page:       channelMonitorPageSnapshotCost,
			validURL:   "/api/channel_monitor/cost?days=30",
			invalidURL: "/api/channel_monitor/cost?days=30&channel_id=0",
		},
		{
			page:       channelMonitorPageSnapshotSuccessDetail,
			validURL:   "/api/channel_monitor/success/detail?minutes=30&group=vip",
			invalidURL: "/api/channel_monitor/success/detail?minutes=30&group=vip&channel_id=0",
		},
	}
	for _, test := range tests {
		_, valid := newChannelMonitorPageSnapshotContext(t, test.validURL, userID)
		_, invalid := newChannelMonitorPageSnapshotContext(t, test.invalidURL, userID)
		validKey, err := service.ChannelMonitorPageSnapshotKey(
			channelMonitorPageSnapshotQuery(valid, test.page),
		)
		require.NoError(t, err)
		invalidKey, err := service.ChannelMonitorPageSnapshotKey(
			channelMonitorPageSnapshotQuery(invalid, test.page),
		)
		require.NoError(t, err)
		assert.NotEqual(t, validKey, invalidKey, test.page)
	}
}

func TestChannelMonitorPageSnapshotQueryIncludesSuccessDetailFilters(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	_, first := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/success/detail?minutes=030&channel_id=007&model_name=%20gpt-4o%20",
		userID,
	)
	_, equivalent := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/success/detail?model_name=gpt-4o&channel_id=7&minutes=30",
		userID,
	)
	_, differentGroup := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/success/detail?minutes=30&group=vip",
		userID,
	)
	_, equivalentGroup := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/success/detail?group=%20vip%20&minutes=030&model_name=ignored",
		userID,
	)
	firstKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(first, channelMonitorPageSnapshotSuccessDetail),
	)
	require.NoError(t, err)
	equivalentKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(equivalent, channelMonitorPageSnapshotSuccessDetail),
	)
	require.NoError(t, err)
	differentGroupKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(differentGroup, channelMonitorPageSnapshotSuccessDetail),
	)
	require.NoError(t, err)
	equivalentGroupKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(equivalentGroup, channelMonitorPageSnapshotSuccessDetail),
	)
	require.NoError(t, err)
	assert.Equal(t, firstKey, equivalentKey)
	assert.NotEqual(t, firstKey, differentGroupKey)
	assert.Equal(t, differentGroupKey, equivalentGroupKey)
}

func TestChannelMonitorPageSnapshotQueryUsesEffectivePerformanceRange(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	optionKeys := []string{
		channelMonitorSmartScheduleEnabledOption,
		channelMonitorSmartScheduleGroupPoliciesOption,
		channelMonitorSmartSchedulePerformanceWindowOption,
	}
	previous := make(map[string]string, len(optionKeys))
	present := make(map[string]bool, len(optionKeys))
	common.OptionMapRWMutex.Lock()
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	for _, key := range optionKeys {
		previous[key], present[key] = common.OptionMap[key]
	}
	common.OptionMap[channelMonitorSmartScheduleEnabledOption] = "true"
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"gpt-4o"}, 2, 80, 30,
	)
	common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption] = channelSmartScheduleTestGroupPoliciesJSON(t, policy)
	common.OptionMap[channelMonitorSmartSchedulePerformanceWindowOption] = "120"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		for _, key := range optionKeys {
			if present[key] {
				common.OptionMap[key] = previous[key]
			} else {
				delete(common.OptionMap, key)
			}
		}
		if optionMapWasNil {
			common.OptionMap = nil
		}
	})

	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	_, first := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	_, equivalent := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=60", userID,
	)
	firstKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(first, channelMonitorPageSnapshotPerformance),
	)
	require.NoError(t, err)
	equivalentKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(equivalent, channelMonitorPageSnapshotPerformance),
	)
	require.NoError(t, err)
	assert.Equal(t, firstKey, equivalentKey)

	common.OptionMapRWMutex.Lock()
	common.OptionMap[channelMonitorSmartScheduleEnabledOption] = "false"
	common.OptionMapRWMutex.Unlock()
	_, manual := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=120", userID,
	)
	manualKey, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(manual, channelMonitorPageSnapshotPerformance),
	)
	require.NoError(t, err)
	assert.NotEqual(t, firstKey, manualKey)
}

func TestChannelMonitorPageSnapshotServesStaleWhileRefreshing(t *testing.T) {
	client, _ := setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	var builds atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	handler := func(c *gin.Context) {
		build := builds.Add(1)
		label := "old"
		if build > 1 {
			label = "new"
			startedOnce.Do(func() { close(started) })
			<-release
		}
		common.ApiSuccess(c, gin.H{
			"label":           label,
			"data_cutoff_at":  build,
			"event_watermark": build,
		})
	}
	_, first := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(first, channelMonitorPageSnapshotPerformance, handler))
	query := channelMonitorPageSnapshotQuery(first, channelMonitorPageSnapshotPerformance)
	waitForChannelMonitorPageSnapshot(t, query)
	key, err := service.ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	raw, err := client.Get(context.Background(), key).Bytes()
	require.NoError(t, err)
	var snapshot service.ChannelMonitorPageSnapshot
	require.NoError(t, common.Unmarshal(raw, &snapshot))
	snapshot.GeneratedAt = time.Now().Add(-2 * time.Second).Unix()
	snapshot.GeneratedAtUnixMilli = time.Now().Add(-2 * time.Second).UnixMilli()
	raw, err = common.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), key, raw, time.Minute).Err())
	require.NoError(t, client.HSet(
		context.Background(),
		key+":meta",
		"revision", snapshot.Revision,
		"event_watermark", snapshot.EventWatermark,
		"generated_at_unix_milli", snapshot.GeneratedAtUnixMilli,
	).Err())
	// The first request also leaves a process-local copy. Wait for that copy
	// to cross the fresh TTL so the next request exercises stale-while-refresh,
	// rather than the newer-local-copy rollback guard.
	require.Eventually(t, func() bool {
		_, state, loadErr := service.LoadChannelMonitorPageSnapshot(
			context.Background(), query,
		)
		return loadErr == nil && state == service.ChannelMonitorPageSnapshotStale
	}, 2*time.Second, 10*time.Millisecond)

	staleRecorder, staleRequest := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(
		staleRequest,
		channelMonitorPageSnapshotPerformance,
		handler,
	))
	stale := decodeChannelMonitorPageSnapshotResponse(t, staleRecorder)
	assert.True(t, stale.Data.Stale)
	assert.Equal(t, "old", stale.Data.Label)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stale snapshot did not submit its background refresh")
	}
	close(release)
	require.Eventually(t, func() bool {
		loaded, state, loadErr := service.LoadChannelMonitorPageSnapshot(
			context.Background(),
			query,
		)
		return loadErr == nil &&
			state == service.ChannelMonitorPageSnapshotFresh &&
			string(loaded.Payload) != string(snapshot.Payload)
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), builds.Load())
}

func TestChannelMonitorPageSnapshotDoesNotCacheNon2xxResponse(t *testing.T) {
	client, _ := setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	var builds atomic.Int32
	built := make(chan struct{})
	handler := func(c *gin.Context) {
		builds.Add(1)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数无效"})
		close(built)
	}
	recorder, target := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(target, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	select {
	case <-built:
	case <-time.After(time.Second):
		t.Fatal("non-2xx snapshot build did not run")
	}
	assert.Equal(t, int32(1), builds.Load())
	key, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(target, channelMonitorPageSnapshotPerformance),
	)
	require.NoError(t, err)
	assert.False(t, client.Exists(context.Background(), key).Val() > 0)
}

func TestChannelMonitorPageSnapshotPreservesInvalidQueryBoundary(t *testing.T) {
	_, server := setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	tests := []struct {
		name        string
		targetURL   string
		handler     gin.HandlerFunc
		messagePart string
	}{
		{
			name:        "performance range",
			targetURL:   "/api/channel_monitor/performance?minutes=0",
			handler:     GetChannelMonitorPerformance,
			messagePart: "1 到 1440",
		},
		{
			name:        "cost date",
			targetURL:   "/api/channel_monitor/cost?days=30&date=not-a-date",
			handler:     GetChannelMonitorCostOverview,
			messagePart: "统计日期",
		},
		{
			name:        "success detail scope",
			targetURL:   "/api/channel_monitor/success/detail?minutes=15",
			handler:     GetChannelMonitorSuccessDetail,
			messagePart: "指定一个渠道或分组",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, target := newChannelMonitorPageSnapshotContext(t, test.targetURL, userID)
			test.handler(target)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), test.messagePart)
		})
	}
	assert.Empty(t, server.Keys(), "invalid requests must not allocate snapshot keys or leases")
}

func TestChannelMonitorPageSnapshotDoesNotPersistSensitiveFields(t *testing.T) {
	client, _ := setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	built := make(chan struct{})
	recorder, target := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(
		target,
		channelMonitorPageSnapshotOverview,
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"success":       true,
				"message":       "",
				"authorization": "Bearer must-not-persist",
				"data":          gin.H{"label": "complete"},
			})
			close(built)
		},
	))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	select {
	case <-built:
	case <-time.After(time.Second):
		t.Fatal("sensitive snapshot build did not run")
	}
	key, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(target, channelMonitorPageSnapshotOverview),
	)
	require.NoError(t, err)
	assert.Zero(t, client.Exists(context.Background(), key).Val())
}

func TestChannelMonitorPageSnapshotDoesNotPersistSensitiveFieldsInNestedArrays(t *testing.T) {
	client, _ := setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	built := make(chan struct{})
	recorder, target := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(
		target,
		channelMonitorPageSnapshotOverview,
		func(c *gin.Context) {
			common.ApiSuccess(c, gin.H{
				"nested": []any{[]any{gin.H{"refresh_token": "must-not-persist"}}},
			})
			close(built)
		},
	))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	select {
	case <-built:
	case <-time.After(time.Second):
		t.Fatal("nested sensitive snapshot build did not run")
	}
	key, err := service.ChannelMonitorPageSnapshotKey(
		channelMonitorPageSnapshotQuery(target, channelMonitorPageSnapshotOverview),
	)
	require.NoError(t, err)
	assert.Zero(t, client.Exists(context.Background(), key).Val())
}

func TestChannelMonitorPageSnapshotAllowsOnlySanitizedPageCredentialMetadata(t *testing.T) {
	tests := []struct {
		name    string
		page    string
		payload string
		wantErr bool
	}{
		{
			name:    "masked cost API key",
			page:    channelMonitorPageSnapshotCost,
			payload: `{"success":true,"data":{"api_keys":[{"api_key":"sk-a**********last"}]}}`,
		},
		{
			name:    "blank sanitized custom secret",
			page:    channelMonitorPageSnapshotOverview,
			payload: `{"success":true,"data":{"headers":[{"key":"Authorization","value":"","secret":true,"has_value":true}]}}`,
		},
		{
			name:    "raw cost API key",
			page:    channelMonitorPageSnapshotCost,
			payload: `{"success":true,"data":{"api_keys":[{"api_key":"sk-live-credential"}]}}`,
			wantErr: true,
		},
		{
			name:    "camel case token",
			page:    channelMonitorPageSnapshotOverview,
			payload: `{"success":true,"data":{"accessToken":"must-not-persist"}}`,
			wantErr: true,
		},
		{
			name:    "hyphenated client secret",
			page:    channelMonitorPageSnapshotOverview,
			payload: `{"success":true,"data":{"client-secret":"must-not-persist"}}`,
			wantErr: true,
		},
		{
			name:    "generic value marked secret",
			page:    channelMonitorPageSnapshotOverview,
			payload: `{"success":true,"data":{"headers":[{"key":"Authorization","value":"Bearer must-not-persist","secret":true}]}}`,
			wantErr: true,
		},
		{
			name:    "credential-shaped dynamic key",
			page:    channelMonitorPageSnapshotOverview,
			payload: `{"success":true,"data":{"headers":[{"key":"X-API-Key","value":"must-not-persist","secret":false}]}}`,
			wantErr: true,
		},
		{
			name:    "malformed metadata",
			page:    channelMonitorPageSnapshotPerformance,
			payload: `{"success":true,"data":{"data_cutoff_at":"not-a-timestamp","event_watermark":1}}`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, _, _, err := normalizeChannelMonitorPageSnapshotPayload(
				test.page, []byte(test.payload),
			)
			if test.wantErr {
				assert.ErrorIs(t, err, service.ErrChannelMonitorPageSnapshotNotCacheable)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestChannelMonitorPageSnapshotReturnsProtectionResponseWithoutRedis(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	previousClient := common.RDB
	common.RDB = nil
	t.Cleanup(func() { common.RDB = previousClient })

	recorder, target := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	var builds atomic.Int32
	require.True(t, serveChannelMonitorPageSnapshot(
		target,
		channelMonitorPageSnapshotPerformance,
		func(c *gin.Context) {
			builds.Add(1)
			common.ApiSuccess(c, gin.H{"unexpected": true})
		},
	))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "CHANNEL_MONITOR_SNAPSHOT_REFRESHING")
	assert.Zero(t, builds.Load())
}

func TestChannelMonitorPageSnapshotReturnsProtectionResponseWhenRedisIsDisabled(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	common.RedisEnabled = false
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	recorder, target := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	var builds atomic.Int32
	require.True(t, serveChannelMonitorPageSnapshot(
		target,
		channelMonitorPageSnapshotPerformance,
		func(c *gin.Context) {
			builds.Add(1)
			common.ApiSuccess(c, gin.H{"unexpected": true})
		},
	))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "CHANNEL_MONITOR_SNAPSHOT_REFRESHING")
	assert.Zero(t, builds.Load())
}

func TestChannelMonitorPageSnapshotServesLocalStaleCopyWhenRedisIsDisabled(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	var builds atomic.Int32
	handler := func(c *gin.Context) {
		common.ApiSuccess(c, gin.H{
			"build":           builds.Add(1),
			"data_cutoff_at":  int64(10),
			"event_watermark": uint64(20),
		})
	}
	firstRecorder, first := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(first, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, http.StatusServiceUnavailable, firstRecorder.Code)
	waitForChannelMonitorPageSnapshot(t, channelMonitorPageSnapshotQuery(first, channelMonitorPageSnapshotPerformance))
	firstRecorder, first = newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(first, channelMonitorPageSnapshotPerformance, handler))
	assert.Equal(t, int32(1), builds.Load())
	firstResponse := decodeChannelMonitorPageSnapshotResponse(t, firstRecorder)

	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	secondRecorder, second := newChannelMonitorPageSnapshotContext(
		t, "/api/channel_monitor/performance?minutes=15", userID,
	)
	require.True(t, serveChannelMonitorPageSnapshot(second, channelMonitorPageSnapshotPerformance, handler))
	secondResponse := decodeChannelMonitorPageSnapshotResponse(t, secondRecorder)
	assert.Equal(t, http.StatusOK, secondRecorder.Code)
	assert.Equal(t, int32(1), builds.Load())
	assert.Equal(t, firstResponse.Data.Label, secondResponse.Data.Label)
	assert.True(t, secondResponse.Data.Stale)
}

func TestChannelMonitorSuccessDetailUsesPageSnapshotProtection(t *testing.T) {
	_, _ = setupChannelMonitorPageSnapshotControllerTest(t)
	previousClient := common.RDB
	common.RDB = nil
	t.Cleanup(func() { common.RDB = previousClient })
	userID := int(channelMonitorPageSnapshotTestUserID.Add(1))
	recorder, target := newChannelMonitorPageSnapshotContext(
		t,
		"/api/channel_monitor/success/detail?minutes=15&channel_id=1",
		userID,
	)

	GetChannelMonitorSuccessDetail(target)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "CHANNEL_MONITOR_SNAPSHOT_REFRESHING")
}

type channelMonitorPageSnapshotTestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Label          string `json:"label"`
		GeneratedAt    int64  `json:"generated_at"`
		DataCutoffAt   int64  `json:"data_cutoff_at"`
		EventWatermark uint64 `json:"event_watermark"`
		Stale          bool   `json:"stale"`
	} `json:"data"`
}

func decodeChannelMonitorPageSnapshotResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) channelMonitorPageSnapshotTestResponse {
	t.Helper()
	var response channelMonitorPageSnapshotTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func waitForChannelMonitorPageSnapshot(
	t *testing.T,
	query service.ChannelMonitorPageSnapshotQuery,
) service.ChannelMonitorPageSnapshot {
	t.Helper()
	var snapshot service.ChannelMonitorPageSnapshot
	require.Eventually(t, func() bool {
		var err error
		snapshot, _, err = service.LoadChannelMonitorPageSnapshot(context.Background(), query)
		return err == nil
	}, time.Second, 10*time.Millisecond)
	return snapshot
}

func newChannelMonitorPageSnapshotContext(
	t *testing.T,
	targetURL string,
	userID int,
) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	recorder := httptest.NewRecorder()
	target, _ := gin.CreateTestContext(recorder)
	request, err := http.NewRequest(http.MethodGet, targetURL, nil)
	require.NoError(t, err)
	target.Request = request
	target.Set("role", common.RoleRootUser)
	target.Set("id", userID)
	target.Set("group", "root")
	target.Set("user_group", "root")
	target.Set("auth_version", int64(1))
	return recorder, target
}

func setupChannelMonitorPageSnapshotControllerTest(
	t *testing.T,
) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled := common.RedisEnabled
	previousClient := common.RDB
	previousRead := common.RDBMonitorRead
	previousWrite := common.RDBMonitorWrite
	common.RedisEnabled = true
	common.RDB = client
	common.RDBMonitorRead = nil
	common.RDBMonitorWrite = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
		common.RDBMonitorRead = previousRead
		common.RDBMonitorWrite = previousWrite
		_ = client.Close()
	})
	return client, server
}
