package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelModelDetectionIntegrationRecoversCompletedSessionWithFixedChannelCost(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	require.NoError(t, db.AutoMigrate(
		&model.Token{},
		&model.UserSubscription{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionBatch{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	))

	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"integration-upstream-model":0.01}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	const (
		boundChannelID   = 98101
		backupChannelID  = 98102
		pricingUserID    = 98103
		pricingTokenID   = 98104
		subscriptionID   = 98105
		configHash       = "integration-hash"
		officialSession  = "integration-official-session"
		detectorRequest  = "integration-detector-request"
		bootstrapToken   = "integration-bootstrap-token"
		boundChannelKey  = "integration-bound-secret"
		backupChannelKey = "integration-backup-secret"
	)
	now := time.Now().UTC().Truncate(time.Second)

	var boundRequests atomic.Int64
	boundUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		boundRequests.Add(1)
		assert.Equal(t, "/v1/responses", request.URL.Path)
		assert.Equal(t, "Bearer "+boundChannelKey, request.Header.Get("Authorization"))
		var payload struct {
			Model string `json:"model"`
		}
		assert.NoError(t, common.DecodeJson(request.Body, &payload))
		assert.Equal(t, "integration-upstream-model", payload.Model)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "integration-upstream-request")
		_, err := io.WriteString(w, `{"id":"integration-response","object":"response","status":"completed","model":"integration-upstream-model","output":[],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}`)
		assert.NoError(t, err)
	}))
	defer boundUpstream.Close()

	var backupRequests atomic.Int64
	backupUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err := io.WriteString(w, `{"id":"backup-should-not-run","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
		assert.NoError(t, err)
	}))
	defer backupUpstream.Close()

	pricingUser := model.User{
		Id: pricingUserID, Username: "model-detection-integration", Password: "integration-password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default",
		Quota: 987654, UsedQuota: 12345, RequestCount: 67, AffCode: "model-detection-integration",
	}
	require.NoError(t, db.Create(&pricingUser).Error)
	pricingToken := model.Token{
		Id: pricingTokenID, UserId: pricingUserID, Key: strings.Repeat("k", 32), Name: "integration-token",
		Status: common.TokenStatusEnabled, CreatedTime: now.Unix(), AccessedTime: now.Add(-time.Hour).Unix(),
		ExpiredTime: -1, RemainQuota: 456789, UsedQuota: 321,
	}
	require.NoError(t, db.Create(&pricingToken).Error)
	pricingSubscription := model.UserSubscription{
		Id: subscriptionID, UserId: pricingUserID, AmountTotal: 900000, AmountUsed: 23456,
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(24 * time.Hour).Unix(), Status: "active", Source: "admin",
	}
	require.NoError(t, db.Create(&pricingSubscription).Error)

	boundBaseURL := boundUpstream.URL
	backupBaseURL := backupUpstream.URL
	boundChannel := model.Channel{
		Id: boundChannelID, Type: constant.ChannelTypeOpenAI, Key: boundChannelKey,
		Status: common.ChannelStatusEnabled, Name: "integration-bound", BaseURL: &boundBaseURL,
		Models: "integration-upstream-model", Group: "default", UsedQuota: 778899,
	}
	backupChannel := model.Channel{
		Id: backupChannelID, Type: constant.ChannelTypeOpenAI, Key: backupChannelKey,
		Status: common.ChannelStatusEnabled, Name: "integration-backup", BaseURL: &backupBaseURL,
		Models: "integration-upstream-model", Group: "default", UsedQuota: 112233,
	}
	require.NoError(t, db.Create(&boundChannel).Error)
	require.NoError(t, db.Create(&backupChannel).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: boundChannelID, Ratio: 0.8, UpdatedTime: now.Unix(),
	}).Error)
	dailyCost := model.ChannelDailyCost{
		ChannelId: boundChannelID, DayStart: model.ChannelDailyCostDayStart(now.Unix()),
		CostNanoCNY: 123456789, SettledCount: 9, UnresolvedCount: 2,
		CreatedAt: now.Add(-time.Hour).Unix(), UpdatedAt: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, db.Create(&dailyCost).Error)

	var baselineUser model.User
	var baselineToken model.Token
	var baselineSubscription model.UserSubscription
	var baselineDailyCost model.ChannelDailyCost
	var baselineBoundChannel model.Channel
	require.NoError(t, db.First(&baselineUser, pricingUserID).Error)
	require.NoError(t, db.First(&baselineToken, pricingTokenID).Error)
	require.NoError(t, db.First(&baselineSubscription, subscriptionID).Error)
	require.NoError(t, db.First(&baselineDailyCost, dailyCost.Id).Error)
	require.NoError(t, db.First(&baselineBoundChannel, boundChannelID).Error)
	var baselineLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Count(&baselineLogCount).Error)

	tokenStore, err := service.NewChannelModelDetectorTokenStore()
	require.NoError(t, err)
	relay, err := service.NewChannelModelDetectorRelay(tokenStore, NewChannelModelDetectorFixedExecutor(db))
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	relayEngine := gin.New()
	relayEngine.POST("/internal/model-detector/v1/responses", NewChannelModelDetectorRelayHandler(relay).PostChannelModelDetectorRelay)
	relayServer := httptest.NewServer(relayEngine)
	defer relayServer.Close()
	relayBaseURL := relayServer.URL + "/internal/model-detector/v1"
	t.Setenv("GPT56_INTERNAL_RELAY_URL", relayBaseURL)

	var detectorStarted atomic.Bool
	var detectorStartCalls atomic.Int64
	var detectorStatusCalls atomic.Int64
	var detectorReportCalls atomic.Int64
	var detectorFactoryCalls atomic.Int64
	var replayStatus atomic.Int64
	var credentialMu sync.Mutex
	issuedCredential := ""
	detectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/bootstrap":
			_, err := io.WriteString(w, `{"session_token":"`+bootstrapToken+`","schema_version":2,"single_presets":{"low":{"config_hash":"integration-low"},"medium":{"config_hash":"`+configHash+`","probe_profile":"medium"},"high":{"config_hash":"integration-high"}},"continuous_presets":{},"schema":{},"probe_catalog":[]}`)
			assert.NoError(t, err)
		case "/api/detector/estimate":
			assert.Equal(t, bootstrapToken, request.Header.Get("X-GPT56-Session"))
			_, err := io.WriteString(w, `{"total_requests":1,"fixed_32k_requests":0,"config_hash":"`+configHash+`"}`)
			assert.NoError(t, err)
		case "/api/detector/status":
			detectorStatusCalls.Add(1)
			if !detectorStarted.Load() {
				_, err := io.WriteString(w, `{"status":"idle","session_id":""}`)
				assert.NoError(t, err)
				return
			}
			_, err := io.WriteString(w, `{"status":"complete","session_id":"`+officialSession+`","config_hash":"`+configHash+`","claimed_model":"gpt-5.6-sol","request_model":"integration-upstream-model","report_available":true,"progress":{"planned":1,"logical_completed":1,"successful":1,"errors":0,"cancelled":0,"http_attempts":1,"retries":0}}`)
			assert.NoError(t, err)
		case "/api/detector/start":
			detectorStartCalls.Add(1)
			assert.Equal(t, bootstrapToken, request.Header.Get("X-GPT56-Session"))
			var payload struct {
				BaseURL      string                                   `json:"base_url"`
				APIKey       string                                   `json:"api_key"`
				Model        string                                   `json:"model"`
				ClaimedModel string                                   `json:"claimed_model"`
				RequestModel string                                   `json:"request_model"`
				Config       service.ChannelModelDetectorPresetConfig `json:"config"`
			}
			if !assert.NoError(t, common.DecodeJson(request.Body, &payload)) {
				http.Error(w, "invalid start payload", http.StatusBadRequest)
				return
			}
			assert.Equal(t, relayBaseURL, payload.BaseURL)
			assert.Equal(t, model.ChannelModelDetectionClaimedModelSol, payload.Model)
			assert.Equal(t, model.ChannelModelDetectionClaimedModelSol, payload.ClaimedModel)
			assert.Equal(t, "integration-upstream-model", payload.RequestModel)
			assert.NotEmpty(t, payload.APIKey)
			credentialMu.Lock()
			issuedCredential = payload.APIKey
			credentialMu.Unlock()

			expectedStatuses := []int{http.StatusOK, http.StatusConflict}
			for _, expectedStatus := range expectedStatuses {
				relayRequest, err := http.NewRequestWithContext(request.Context(), http.MethodPost, payload.BaseURL+"/responses", bytes.NewBufferString(`{"model":"`+payload.RequestModel+`","input":"integration"}`))
				if !assert.NoError(t, err) {
					http.Error(w, "relay request creation failed", http.StatusBadGateway)
					return
				}
				relayRequest.Header.Set("Authorization", "Bearer "+payload.APIKey)
				relayRequest.Header.Set("Content-Type", "application/json")
				relayRequest.Header.Set("X-GPT56-Request-Id", detectorRequest)
				relayResponse, err := relayServer.Client().Do(relayRequest)
				if !assert.NoError(t, err) {
					http.Error(w, "relay request failed", http.StatusBadGateway)
					return
				}
				_, readErr := io.ReadAll(relayResponse.Body)
				closeErr := relayResponse.Body.Close()
				assert.NoError(t, readErr)
				assert.NoError(t, closeErr)
				assert.Equal(t, expectedStatus, relayResponse.StatusCode)
				replayStatus.Store(int64(relayResponse.StatusCode))
			}
			detectorStarted.Store(true)
			_, err := io.WriteString(w, `{"started":true,"session_id":"`+officialSession+`","official":true,"config_hash":"`+configHash+`"}`)
			assert.NoError(t, err)
		case "/api/detector/report":
			detectorReportCalls.Add(1)
			_, err := io.WriteString(w, `{"session_id":"`+officialSession+`","schema_version":4,"scoring_version":"trusted-fingerprint-v3","config_hash":"`+configHash+`","baseline_id":"integration-baseline","baseline_sha256":"integration-baseline-sha","build_hash":"integration-build","official":true,"claimed_model":"gpt-5.6-sol","request_model":"integration-upstream-model","overall_verdict":"通过","outcome_code":"juice_pass_fingerprint_strong","candidate_configuration_without_key":{"base_url":"http://redacted.invalid","claimed_model":"gpt-5.6-sol","request_model":"integration-upstream-model"}}`)
			assert.NoError(t, err)
		default:
			http.NotFound(w, request)
		}
	}))
	defer detectorServer.Close()

	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: detectorServer.URL, ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		ScheduleEnabled: false, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai",
	}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: boundChannelID, ScheduleEnabled: true}
	require.NoError(t, db.Create(&config).Error)
	target := model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: boundChannelID, TargetKey: "integration-target",
		RequestModel: "integration-upstream-model", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		Enabled: true,
	}
	require.NoError(t, db.Create(&target).Error)
	run, err := service.CreateChannelModelDetectionManualRun(context.Background(), db, service.ChannelModelDetectionManualRunInput{
		ChannelID: boundChannelID, Preset: model.ChannelModelDetectionPresetMedium,
		CreatedByUserID: pricingUserID, CreatedByUsername: pricingUser.Username,
	}, now)
	require.NoError(t, err)

	detectorFactory := func(detectorURL string) (service.ChannelModelDetectionDetector, error) {
		detectorFactoryCalls.Add(1)
		assert.Equal(t, detectorServer.URL, detectorURL)
		return service.NewChannelModelDetectorClientWithHTTPClient(detectorURL, detectorServer.Client())
	}
	runtime := &channelModelDetectionRuntime{tokens: tokenStore}
	firstWorker := service.NewChannelModelDetectionWorker(db, detectorFactory, runtime.issueCredential)
	firstWorker.Now = func() time.Time { return now }
	started, err := firstWorker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, started.Started)
	assert.Equal(t, officialSession, started.OfficialSessionID)

	secondWorker := service.NewChannelModelDetectionWorker(db, detectorFactory, nil)
	secondWorker.Now = func() time.Time { return now.Add(time.Minute) }
	completed, err := secondWorker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, completed.Completed)
	assert.Equal(t, officialSession, completed.OfficialSessionID)

	assert.EqualValues(t, 1, detectorStartCalls.Load())
	assert.EqualValues(t, 2, detectorStatusCalls.Load())
	assert.EqualValues(t, 1, detectorReportCalls.Load())
	assert.EqualValues(t, 2, detectorFactoryCalls.Load())
	assert.EqualValues(t, http.StatusConflict, replayStatus.Load())
	assert.EqualValues(t, 1, boundRequests.Load())
	assert.Zero(t, backupRequests.Load())

	var storedRun model.ChannelModelDetectionRun
	var storedExecution model.ChannelModelDetectionExecution
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&storedRun).Error)
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&storedExecution).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCompleted, storedRun.Status)
	assert.Equal(t, pricingUserID, storedRun.PricingContextUserId)
	assert.Equal(t, 1, storedRun.CompletedTargetCount)
	assert.Positive(t, storedRun.FinishedAt)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusCompleted, storedExecution.Status)
	assert.Equal(t, officialSession, storedExecution.OfficialSessionId)
	assert.Equal(t, configHash, storedExecution.ConfigHash)
	assert.Equal(t, "juice_pass_fingerprint_strong", storedExecution.OutcomeCode)
	assert.Equal(t, "trusted-fingerprint-v3", storedExecution.ScoringVersion)
	assert.Equal(t, "integration-baseline", storedExecution.BaselineId)
	assert.Equal(t, "integration-build", storedExecution.BuildHash)
	assert.NotEmpty(t, storedExecution.ReportSHA256)

	var events []model.ChannelModelDetectionCostEvent
	require.NoError(t, db.Where("run_id = ?", run.RunId).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, boundChannelID, event.ChannelId)
	assert.Equal(t, detectorRequest, event.DetectorRequestId)
	assert.Equal(t, 1, event.AttemptNo)
	assert.Equal(t, model.ChannelModelDetectionDispatchDispatched, event.DispatchState)
	assert.Equal(t, model.ChannelModelDetectionSettlementSettled, event.SettlementStatus)
	require.NotNil(t, event.SettledCostNanoCNY)
	assert.Positive(t, *event.SettledCostNanoCNY)
	assert.Positive(t, event.SettledAt)

	detail, err := service.GetChannelModelDetectionRunDetail(context.Background(), db, run.RunId)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCompleted, detail.Run.Status)
	assert.Equal(t, service.ChannelModelDetectionCostStatusSettled, detail.Run.Cost.Status)
	assert.EqualValues(t, 1, detail.Run.Cost.SettledRequestCount)
	require.NotNil(t, detail.Run.Cost.SettledCostNanoCNY)
	assert.Positive(t, *detail.Run.Cost.SettledCostNanoCNY)
	require.Len(t, detail.Executions, 1)
	assert.Equal(t, officialSession, detail.Executions[0].OfficialSessionID)
	assert.Equal(t, configHash, detail.Executions[0].ConfigHash)
	assert.Equal(t, 4, detail.Executions[0].SchemaVersion)
	assert.Equal(t, "trusted-fingerprint-v3", detail.Executions[0].ScoringVersion)
	assert.Equal(t, "integration-baseline", detail.Executions[0].BaselineID)
	assert.Equal(t, "integration-build", detail.Executions[0].BuildHash)
	assert.Equal(t, "juice_pass_fingerprint_strong", detail.Executions[0].OutcomeCode)

	credentialMu.Lock()
	capturedCredential := issuedCredential
	credentialMu.Unlock()
	require.NotEmpty(t, capturedCredential)
	detailJSON, err := common.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(detailJSON), capturedCredential)
	assert.NotContains(t, string(detailJSON), boundChannelKey)
	assert.NotContains(t, string(detailJSON), bootstrapToken)
	assert.NotContains(t, storedExecution.OfficialConfigJSON, capturedCredential)
	assert.NotContains(t, storedExecution.ReportJSON, capturedCredential)

	var storedUser model.User
	var storedToken model.Token
	var storedSubscription model.UserSubscription
	var storedDailyCost model.ChannelDailyCost
	var storedBoundChannel model.Channel
	require.NoError(t, db.First(&storedUser, pricingUserID).Error)
	require.NoError(t, db.First(&storedToken, pricingTokenID).Error)
	require.NoError(t, db.First(&storedSubscription, subscriptionID).Error)
	require.NoError(t, db.First(&storedDailyCost, dailyCost.Id).Error)
	require.NoError(t, db.First(&storedBoundChannel, boundChannelID).Error)
	assert.Equal(t, baselineUser.Quota, storedUser.Quota)
	assert.Equal(t, baselineUser.UsedQuota, storedUser.UsedQuota)
	assert.Equal(t, baselineUser.RequestCount, storedUser.RequestCount)
	assert.Equal(t, baselineToken.RemainQuota, storedToken.RemainQuota)
	assert.Equal(t, baselineToken.UsedQuota, storedToken.UsedQuota)
	assert.Equal(t, baselineToken.AccessedTime, storedToken.AccessedTime)
	assert.Equal(t, baselineSubscription.AmountUsed, storedSubscription.AmountUsed)
	assert.Equal(t, baselineSubscription.UpdatedAt, storedSubscription.UpdatedAt)
	assert.Equal(t, baselineBoundChannel.UsedQuota, storedBoundChannel.UsedQuota)
	assert.Equal(t, baselineDailyCost.CostNanoCNY+*event.SettledCostNanoCNY, storedDailyCost.CostNanoCNY)
	assert.Equal(t, baselineDailyCost.ModelDetectionCostNanoCNY+*event.SettledCostNanoCNY, storedDailyCost.ModelDetectionCostNanoCNY)
	assert.Equal(t, model.ChannelDailyCostDayStart(event.SettledAt), storedDailyCost.DayStart)
	assert.Equal(t, baselineDailyCost.SettledCount+1, storedDailyCost.SettledCount)
	assert.Equal(t, baselineDailyCost.UnresolvedCount, storedDailyCost.UnresolvedCount)
	assert.Equal(t, event.SettledAt, storedDailyCost.UpdatedAt)
	var storedDailyCostCount int64
	var storedLogCount int64
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Count(&storedDailyCostCount).Error)
	require.NoError(t, db.Model(&model.Log{}).Count(&storedLogCount).Error)
	assert.EqualValues(t, 1, storedDailyCostCount)
	assert.Equal(t, baselineLogCount, storedLogCount)
}
