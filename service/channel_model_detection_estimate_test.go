package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionEstimateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-estimate.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelLogicalGroup{},
		&model.ChannelLogicalGroupMember{},
		&model.ChannelRatioMonitor{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionLogicalConfig{},
		&model.ChannelModelDetectionLogicalTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	))
	previousDB := model.DB
	model.DB = db
	ratio_setting.InitRatioSettings()
	ResetChannelDailyCostSnapshotCache()
	t.Cleanup(func() {
		model.DB = previousDB
		ResetChannelDailyCostSnapshotCache()
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedChannelModelDetectionEstimate(t *testing.T, db *gorm.DB, channelID int, detectorURL string, requestModels ...string) model.ChannelModelDetectionConfig {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "estimate", Key: "channel-secret", Models: "gpt-5.6-sol,gpt-5.6-terra", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{DetectorURL: detectorURL, ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: channelID, Revision: 1}
	require.NoError(t, db.Create(&config).Error)
	for position, requestModel := range requestModels {
		claimed := model.ChannelModelDetectionClaimedModelSol
		if requestModel == "gpt-5.6-terra" {
			claimed = model.ChannelModelDetectionClaimedModelTerra
		}
		require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: channelID, TargetKey: "target-" + requestModel, RequestModel: requestModel, ClaimedModel: claimed, Position: position, Enabled: true}).Error)
	}
	return config
}

func newChannelModelDetectionEstimateServer(t *testing.T, totalRequests int64, bootstrapCalls, estimateCalls *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case channelModelDetectorBootstrapPath:
			bootstrapCalls.Add(1)
			preset := `{"mode":"single","workers":2,"retries":9}`
			_, _ = writer.Write([]byte(`{"session_token":"session-secret","schema_version":2,"single_presets":{"low":` + preset + `,"medium":` + preset + `,"high":` + preset + `}}`))
		case channelModelDetectorEstimatePath:
			estimateCalls.Add(1)
			assert.Equal(t, "session-secret", request.Header.Get("X-GPT56-Session"))
			_, _ = writer.Write([]byte(`{"total_requests":` + strconv.FormatInt(totalRequests, 10) + `,"fixed_32k_requests":3,"config_hash":"dynamic-hash"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestChannelModelDetectionEstimateAPICallsOfficialEndpointsOnceAndReusesEstimate(t *testing.T) {
	db := setupChannelModelDetectionEstimateTestDB(t)
	var bootstrapCalls atomic.Int64
	var estimateCalls atomic.Int64
	server := newChannelModelDetectionEstimateServer(t, 7, &bootstrapCalls, &estimateCalls)
	seedChannelModelDetectionEstimate(t, db, 701, server.URL, "gpt-5.6-sol", "gpt-5.6-terra")

	response, err := EstimateChannelModelDetectionCost(context.Background(), db, 701, model.ChannelModelDetectionPresetHigh, ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
	require.NoError(t, err)
	assert.EqualValues(t, 1, bootstrapCalls.Load())
	assert.EqualValues(t, 1, estimateCalls.Load())
	require.Len(t, response.Targets, 2)
	assert.Equal(t, int64(7), response.Targets[0].EstimatedLogicalRequests)
	assert.Equal(t, int64(7), response.Targets[0].EstimatedHTTPAttempts)
	assert.Equal(t, int64(7), response.Targets[1].EstimatedLogicalRequests)
	assert.Equal(t, int64(7), response.Targets[1].EstimatedHTTPAttempts)
	assert.Equal(t, "dynamic-hash", response.OfficialEstimate.ConfigHash)

	var runs, executions, events int64
	require.NoError(t, db.Model(&model.ChannelModelDetectionRun{}).Count(&runs).Error)
	require.NoError(t, db.Model(&model.ChannelModelDetectionExecution{}).Count(&executions).Error)
	require.NoError(t, db.Model(&model.ChannelModelDetectionCostEvent{}).Count(&events).Error)
	assert.Zero(t, runs)
	assert.Zero(t, executions)
	assert.Zero(t, events)
}

func TestChannelModelDetectionEstimateReadsSharedLogicalTargetsFromAnyMember(t *testing.T) {
	db := setupChannelModelDetectionEstimateTestDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
	var bootstrapCalls, estimateCalls atomic.Int64
	server := newChannelModelDetectionEstimateServer(t, 4, &bootstrapCalls, &estimateCalls)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{DetectorURL: server.URL, ScheduledPreset: model.ChannelModelDetectionPresetLow, Revision: 1}).Error)
	address := "https://api.example.com/v1"
	group := model.ChannelLogicalGroup{Name: "estimate-shared", Status: model.ChannelLogicalGroupStatusEnabled, Revision: 2}
	require.NoError(t, db.Create(&group).Error)
	channels := []model.Channel{
		{Id: 706, Name: "estimate-a", LogicalChannelID: &group.Id, BaseURL: &address, Models: "alpha", Status: common.ChannelStatusEnabled},
		{Id: 707, Name: "estimate-b", LogicalChannelID: &group.Id, BaseURL: &address, Models: "beta", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 706, Weight: 1, AddressFingerprint: strings.Repeat("b", 64)},
		{LogicalGroupID: group.Id, ChannelID: 707, Weight: 1, AddressFingerprint: strings.Repeat("b", 64)},
	}).Error)
	config := model.ChannelModelDetectionLogicalConfig{LogicalChannelId: group.Id, Revision: 1}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionLogicalTarget{
		ConfigId: config.Id, LogicalChannelId: group.Id, TargetKey: "shared-beta", RequestModel: "beta",
		ClaimedModel: model.ChannelModelDetectionClaimedModelTerra, Enabled: true,
	}).Error)

	response, err := EstimateChannelModelDetectionCost(context.Background(), db, 706, model.ChannelModelDetectionPresetLow, ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
	require.NoError(t, err)
	require.Len(t, response.Targets, 1)
	assert.Equal(t, "shared-beta", response.Targets[0].TargetKey)
	assert.Equal(t, "beta", response.Targets[0].RequestModel)
	assert.EqualValues(t, 1, bootstrapCalls.Load())
	assert.EqualValues(t, 1, estimateCalls.Load())
}

func TestChannelModelDetectionEstimateAPIReturnsRequestVolumeWithoutCostEstimate(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		db := setupChannelModelDetectionEstimateTestDB(t)
		var bootstrapCalls, estimateCalls atomic.Int64
		server := newChannelModelDetectionEstimateServer(t, 2, &bootstrapCalls, &estimateCalls)
		seedChannelModelDetectionEstimate(t, db, 702, server.URL, "gpt-5.6-sol")
		require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 702, Ratio: 0.5, UpdatedTime: time.Now().Unix()}).Error)
		ResetChannelDailyCostSnapshotCache()

		response, err := EstimateChannelModelDetectionCost(context.Background(), db, 702, model.ChannelModelDetectionPresetLow, ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
		require.NoError(t, err)
		require.Len(t, response.Targets, 1)
		assert.Nil(t, response.Targets[0].EstimatedQuota)
		assert.Nil(t, response.Targets[0].EstimatedCostNanoCNY)
		assert.Nil(t, response.Targets[0].EstimatedCostCNY)
		assert.False(t, response.Targets[0].CostEstimateUnknown)
		assert.Zero(t, response.CostEstimateUnknownCount)
		assert.Contains(t, response.Targets[0].EstimateBasis, "上游 Usage")
		assert.Nil(t, response.EstimatedQuota)
		assert.Nil(t, response.EstimatedCostNanoCNY)
		assert.Nil(t, response.EstimatedCostCNY)
	})

	t.Run("unknown", func(t *testing.T) {
		db := setupChannelModelDetectionEstimateTestDB(t)
		var bootstrapCalls, estimateCalls atomic.Int64
		server := newChannelModelDetectionEstimateServer(t, 2, &bootstrapCalls, &estimateCalls)
		seedChannelModelDetectionEstimate(t, db, 703, server.URL, "gpt-5.6-sol")
		response, err := EstimateChannelModelDetectionCost(context.Background(), db, 703, model.ChannelModelDetectionPresetLow, ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
		require.NoError(t, err)
		require.Len(t, response.Targets, 1)
		assert.Nil(t, response.Targets[0].EstimatedCostNanoCNY)
		assert.Nil(t, response.Targets[0].EstimatedCostCNY)
		assert.False(t, response.Targets[0].CostEstimateUnknown)
		assert.Zero(t, response.CostEstimateUnknownCount)
		encoded, err := common.Marshal(response)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), `"estimated_cost_nano_cny":null`)
		assert.NotContains(t, string(encoded), "session-secret")
		assert.NotContains(t, string(encoded), server.URL)
		assert.NotContains(t, string(encoded), "channel-secret")
	})

	t.Run("unknown-model-price", func(t *testing.T) {
		db := setupChannelModelDetectionEstimateTestDB(t)
		var bootstrapCalls, estimateCalls atomic.Int64
		server := newChannelModelDetectionEstimateServer(t, 3, &bootstrapCalls, &estimateCalls)
		require.NoError(t, db.Create(&model.Channel{Id: 705, Name: "unknown-price", Key: "secret", Models: "custom-gpt56-alias", Status: common.ChannelStatusEnabled}).Error)
		require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{DetectorURL: server.URL, ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1}).Error)
		config := model.ChannelModelDetectionConfig{ChannelId: 705, Revision: 1}
		require.NoError(t, db.Create(&config).Error)
		require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: 705, TargetKey: "custom-target", RequestModel: "custom-gpt56-alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}).Error)
		require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 705, Ratio: 0.5, UpdatedTime: time.Now().Unix()}).Error)
		ResetChannelDailyCostSnapshotCache()

		response, err := EstimateChannelModelDetectionCost(context.Background(), db, 705, model.ChannelModelDetectionPresetLow, ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
		require.NoError(t, err)
		require.Len(t, response.Targets, 1)
		assert.Nil(t, response.Targets[0].EstimatedQuota)
		assert.Nil(t, response.Targets[0].EstimatedCostNanoCNY)
		assert.False(t, response.Targets[0].CostEstimateUnknown)
		assert.Zero(t, response.CostEstimateUnknownCount)
	})
}

func TestChannelModelDetectionEstimateAPIRejectsIncompatibleOfficialCount(t *testing.T) {
	db := setupChannelModelDetectionEstimateTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == channelModelDetectorBootstrapPath {
			preset := `{"mode":"single"}`
			_, _ = writer.Write([]byte(`{"session_token":"secret","schema_version":2,"single_presets":{"low":` + preset + `,"medium":` + preset + `,"high":` + preset + `}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"total_requests":-1}`))
	}))
	t.Cleanup(server.Close)
	seedChannelModelDetectionEstimate(t, db, 704, server.URL, "gpt-5.6-sol")
	_, err := EstimateChannelModelDetectionCost(context.Background(), db, 704, model.ChannelModelDetectionPresetLow, ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
	assert.ErrorIs(t, err, ErrChannelModelDetectionEstimateInvalid)
}
