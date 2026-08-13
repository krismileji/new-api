package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelModelDetectorRouterExecutor struct {
	executions []service.ChannelModelDetectorRelayExecution
}

func (executor *channelModelDetectorRouterExecutor) ExecuteChannelModelDetectorAttempt(_ context.Context, execution service.ChannelModelDetectorRelayExecution) (service.ChannelModelDetectorRelayUpstreamResult, error) {
	executor.executions = append(executor.executions, execution)
	body := []byte(`{"id":"router-contract","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
	return service.ChannelModelDetectorRelayUpstreamResult{
		StatusCode:   http.StatusOK,
		ContentType:  "application/json",
		ResponseBody: body,
		UsagePayload: body,
		Dispatched:   true,
		RequestID:    "router-contract-request",
	}, nil
}

type channelModelDetectionRouteContract struct {
	method       string
	pattern      string
	concretePath string
}

var channelModelDetectionRouteContracts = []channelModelDetectionRouteContract{
	{method: http.MethodGet, pattern: "/api/channel_monitor/model_detection", concretePath: "/api/channel_monitor/model_detection"},
	{method: http.MethodGet, pattern: "/api/channel_monitor/model_detection/settings", concretePath: "/api/channel_monitor/model_detection/settings"},
	{method: http.MethodPut, pattern: "/api/channel_monitor/model_detection/settings", concretePath: "/api/channel_monitor/model_detection/settings"},
	{method: http.MethodGet, pattern: "/api/channel_monitor/model_detection/service", concretePath: "/api/channel_monitor/model_detection/service"},
	{method: http.MethodPost, pattern: "/api/channel_monitor/model_detection/service/test", concretePath: "/api/channel_monitor/model_detection/service/test"},
	{method: http.MethodPut, pattern: "/api/channel_monitor/model_detection/channel/:id/config", concretePath: "/api/channel_monitor/model_detection/channel/1/config"},
	{method: http.MethodPost, pattern: "/api/channel_monitor/model_detection/channel/:id/estimate", concretePath: "/api/channel_monitor/model_detection/channel/1/estimate"},
	{method: http.MethodPost, pattern: "/api/channel_monitor/model_detection/channel/:id/run", concretePath: "/api/channel_monitor/model_detection/channel/1/run"},
	{method: http.MethodGet, pattern: "/api/channel_monitor/model_detection/channel/:id/runs", concretePath: "/api/channel_monitor/model_detection/channel/1/runs"},
	{method: http.MethodGet, pattern: "/api/channel_monitor/model_detection/runs/:run_id", concretePath: "/api/channel_monitor/model_detection/runs/router-contract"},
	{method: http.MethodPost, pattern: "/api/channel_monitor/model_detection/runs/:run_id/cancel", concretePath: "/api/channel_monitor/model_detection/runs/router-contract/cancel"},
}

func TestChannelModelDetectionRoutesRegisterDocumentedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelMonitorRoutes(engine.Group("/api"))

	actual := make(map[string]struct{})
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/channel_monitor/model_detection") {
			actual[route.Method+" "+route.Path] = struct{}{}
		}
	}
	expected := make(map[string]struct{}, len(channelModelDetectionRouteContracts))
	for _, route := range channelModelDetectionRouteContracts {
		expected[route.method+" "+route.pattern] = struct{}{}
	}

	assert.Equal(t, expected, actual)
}

func TestChannelModelDetectionAPIRootAuthContract(t *testing.T) {
	rootToken, commonToken := setupChannelModelDetectionRouterAuthTest(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelMonitorRoutes(engine.Group("/api"))

	for _, route := range channelModelDetectionRouteContracts {
		name := route.method + " " + route.pattern
		t.Run(name+" missing credential", func(t *testing.T) {
			recorder := performChannelModelDetectionRouterRequest(engine, route, "")
			assert.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
			assert.NotContains(t, recorder.Body.String(), rootToken)
			assert.NotContains(t, recorder.Body.String(), commonToken)
		})
		t.Run(name+" common user", func(t *testing.T) {
			recorder := performChannelModelDetectionRouterRequest(engine, route, commonToken)
			assert.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
			assert.NotContains(t, recorder.Body.String(), rootToken)
			assert.NotContains(t, recorder.Body.String(), commonToken)
		})
	}

	overview := channelModelDetectionRouteContracts[0]
	recorder := performChannelModelDetectionRouterRequest(engine, overview, rootToken)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.NotContains(t, recorder.Body.String(), rootToken)
	assert.NotContains(t, recorder.Body.String(), commonToken)
}

func TestChannelModelDetectionRelayEndpointUsesDedicatedRuntimeBearer(t *testing.T) {
	previousDB := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = previousDB })

	store, err := service.GetChannelModelDetectorTokenStore()
	require.NoError(t, err)
	credential, err := store.Issue(service.ChannelModelDetectorTokenSpec{
		RunID:           "router-runtime-run",
		TargetID:        17,
		ExecutionID:     1701,
		ChannelID:       23,
		RequestModel:    "upstream-router-model",
		ClaimedModel:    model.ChannelModelDetectionClaimedModelSol,
		Preset:          model.ChannelModelDetectionPresetLow,
		RelayBaseURL:    "http://127.0.0.1/internal/model-detector/v1",
		MaxHTTPAttempts: 2,
		ExpiresAt:       time.Now().UTC().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	executor := &channelModelDetectorRouterExecutor{}
	relay, err := service.NewChannelModelDetectorRelay(store, executor)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelModelDetectorRelayRoutes(engine, controller.NewChannelModelDetectorRelayHandler(relay))

	getRecorder := httptest.NewRecorder()
	engine.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/internal/model-detector/v1/responses", nil))
	assert.Equal(t, http.StatusNotFound, getRecorder.Code)

	invalidRecorder := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	invalidRequest.Header.Set("Authorization", "Bearer ordinary-api-token")
	invalidRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(invalidRecorder, invalidRequest)
	assert.Equal(t, http.StatusUnauthorized, invalidRecorder.Code, invalidRecorder.Body.String())
	assert.Contains(t, invalidRecorder.Body.String(), `"type":"model_detector_relay_error"`)
	assert.NotContains(t, invalidRecorder.Body.String(), "ordinary-api-token")
	assert.Empty(t, executor.executions)

	acceptedRecorder := httptest.NewRecorder()
	acceptedRequest := httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","input":"router contract"}`))
	acceptedRequest.Header.Set("Authorization", "Bearer "+credential.BearerToken())
	acceptedRequest.Header.Set("Content-Type", "application/json")
	acceptedRequest.Header.Set("X-GPT56-Request-Id", "router-contract-attempt")
	engine.ServeHTTP(acceptedRecorder, acceptedRequest)

	require.Equal(t, http.StatusOK, acceptedRecorder.Code, acceptedRecorder.Body.String())
	assert.Equal(t, "router-contract-request", acceptedRecorder.Header().Get("X-Request-Id"))
	assert.Contains(t, acceptedRecorder.Body.String(), `"id":"router-contract"`)
	assert.NotContains(t, acceptedRecorder.Body.String(), credential.BearerToken())
	assert.NotContains(t, acceptedRecorder.Body.String(), credential.Claims.Nonce)
	require.Len(t, executor.executions, 1)
	assert.Equal(t, 23, executor.executions[0].ChannelID)
	assert.Equal(t, "upstream-router-model", executor.executions[0].RequestModel)
	assert.Equal(t, "router-contract-attempt", executor.executions[0].DetectorRequestID)
	var forwarded struct {
		Model string `json:"model"`
	}
	require.NoError(t, common.Unmarshal(executor.executions[0].RequestBody, &forwarded))
	assert.Equal(t, "upstream-router-model", forwarded.Model)
}

func setupChannelModelDetectionRouterAuthTest(t *testing.T) (string, string) {
	t.Helper()
	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-router.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Channel{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	))
	model.DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	rootToken := strings.Repeat("r", 32)
	commonToken := strings.Repeat("u", 32)
	users := []model.User{
		{Id: 97001, Username: "model-detection-router-root", Password: "unused-password", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "model-detection-router-root", AccessToken: &rootToken, AuthVersion: 1},
		{Id: 97002, Username: "model-detection-router-user", Password: "unused-password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "model-detection-router-user", AccessToken: &commonToken, AuthVersion: 1},
	}
	require.NoError(t, db.Create(&users).Error)
	return rootToken, commonToken
}

func performChannelModelDetectionRouterRequest(engine *gin.Engine, route channelModelDetectionRouteContract, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	var body *strings.Reader
	if route.method == http.MethodGet {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(`{}`)
	}
	request := httptest.NewRequest(route.method, route.concretePath, body)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}
