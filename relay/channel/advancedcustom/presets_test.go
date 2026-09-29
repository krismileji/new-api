package advancedcustom_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/advancedcustom"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSGLangChannelProtocols(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeSGLang)
	require.True(t, ok)
	adaptor := relay.GetAdaptor(apiType)
	require.NotNil(t, adaptor)
	assert.Equal(t, "advanced_custom", adaptor.GetChannelName())
	assert.Contains(t, common.GetEndpointTypesByChannelType(constant.ChannelTypeSGLang, "served-model"), constant.EndpointTypeAnthropic)
	assert.Contains(t, common.GetEndpointTypesByChannelType(constant.ChannelTypeSGLang, "served-model"), constant.EndpointTypeOpenAIResponse)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSGLang, ChannelBaseUrl: "https://inference.example/prefix", ApiKey: "test-key"}}
	info.ChannelOtherSettings.AdvancedCustom = common.GetAdvancedCustomPreset(constant.ChannelTypeSGLang)
	adaptor.Init(info)
	for _, path := range []string{"/v1/chat/completions", "/v1/completions", "/v1/embeddings", "/v1/responses", "/v1/messages"} {
		info.RequestURLPath = path
		adaptor = &advancedcustom.Adaptor{}
		adaptor.Init(info)
		url, err := adaptor.GetRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, "https://inference.example/prefix"+path, url)
	}
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		info.RelayFormat = format
		info.RequestURLPath = "/v1/messages"
		adaptor = &advancedcustom.Adaptor{}
		adaptor.Init(info)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		headers := http.Header{}
		require.NoError(t, adaptor.SetupRequestHeader(c, &headers, info))
		assert.Equal(t, "Bearer test-key", headers.Get("Authorization"))
	}
	info.RequestURLPath = "/v1/chat/completions"
	adaptor = &advancedcustom.Adaptor{}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := &dto.GeneralOpenAIRequest{Model: "served-model"}
	converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	assert.Same(t, request, converted)
	info.RequestURLPath = "/v1/messages"
	adaptor = &advancedcustom.Adaptor{}
	adaptor.Init(info)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	claudeRequest := &dto.ClaudeRequest{Model: "served-model", Thinking: &dto.Thinking{Type: "adaptive"}}
	converted, err = adaptor.ConvertClaudeRequest(c, info, claudeRequest)
	require.NoError(t, err)
	assert.Same(t, claudeRequest, converted)
	info.RequestURLPath = "/v1beta/models/test:generateContent"
	adaptor = &advancedcustom.Adaptor{}
	adaptor.Init(info)
	_, err = adaptor.ConvertGeminiRequest(nil, info, &dto.GeminiChatRequest{})
	require.Error(t, err)
}

func TestSGLangRerankRequestAndResponse(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/prefix/v1/rerank", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		var request dto.RerankRequest
		require.NoError(t, common.DecodeJson(r.Body, &request))
		assert.Equal(t, "served-reranker", request.Model)
		assert.Equal(t, []any{"first", "second"}, request.Documents)
		_, _ = io.WriteString(w, `[{"index":1,"score":0.9,"document":"second","meta_info":{"prompt_tokens":5}}]`)
	}))
	defer upstream.Close()
	info := &relaycommon.RelayInfo{
		ChannelMeta:    &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSGLang, ChannelBaseUrl: upstream.URL + "/prefix", ApiKey: "test-key"},
		RequestURLPath: "/rerank", RelayMode: relayconstant.RelayModeRerank,
		RelayFormat:  types.RelayFormatOpenAI,
		RerankerInfo: &relaycommon.RerankerInfo{Documents: []any{"first", "second"}, ReturnDocuments: true},
		PriceData:    hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
	}
	info.SetEstimatePromptTokens(12)
	adaptor := &advancedcustom.Adaptor{}
	info.ChannelOtherSettings.AdvancedCustom = common.GetAdvancedCustomPreset(constant.ChannelTypeSGLang)
	adaptor.Init(info)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/rerank", nil).WithContext(context.Background())
	converted, err := adaptor.ConvertRerankRequest(c, info.RelayMode, dto.RerankRequest{Model: "served-reranker", Query: "query", Documents: info.Documents})
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	response, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
	require.Nil(t, apiErr)
	assert.Equal(t, 12, usage.(*dto.Usage).TotalTokens)
	var decoded dto.RerankResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &decoded))
	assert.Equal(t, []dto.RerankResponseResult{{Index: 1, RelevanceScore: 0.9, Document: map[string]any{"text": "second"}}}, decoded.Results)
	assert.Equal(t, 12, decoded.Usage.PromptTokens)
	assert.Zero(t, decoded.Usage.CompletionTokens)
	assert.False(t, service.CalculateChannelModelDetectionQuota(c, info, usage.(*dto.Usage)).Reliable,
		"SGLang rerank local estimates must not become confirmed downstream cost")
}

// External DSNs must refer to isolated test databases.
func TestSGLangRerankCostDatabaseMatrix(t *testing.T) {
	previousUnit, previousRedis := common.QuotaPerUnit, common.RedisEnabled
	previousIncome := model.ChannelMonitorIncomeReady.Swap(false)
	common.QuotaPerUnit, common.RedisEnabled = 500_000, false
	t.Cleanup(func() {
		common.QuotaPerUnit, common.RedisEnabled = previousUnit, previousRedis
		model.ChannelMonitorIncomeReady.Store(previousIncome)
	})
	for _, dialect := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.kind), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.kind == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			previousDB := model.DB
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			model.DB = db
			common.SetDatabaseTypes(dialect.kind, previousLogType)
			service.ResetChannelDailyCostSnapshotCache()
			t.Cleanup(func() {
				assert.NoError(t, service.FlushChannelDailyCostEvents())
				service.ResetChannelDailyCostSnapshotCache()
				model.DB = previousDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
			})
			require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}))
			query := "SELECT version()"
			if dialect.kind == common.DatabaseTypeSQLite {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database: %s", version)
			for index, tc := range []struct {
				name        string
				channelType int
				expression  string
				perCall     bool
			}{
				{name: "named token estimate", channelType: constant.ChannelTypeSGLang},
				{name: "custom converter token estimate", channelType: constant.ChannelTypeAdvancedCustom},
				{name: "fixed expression estimate", channelType: constant.ChannelTypeSGLang, expression: `tier("request", fixed(0.01))`},
				{name: "legacy per-call estimate", channelType: constant.ChannelTypeSGLang, perCall: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					channelID := index + 1
					conversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{Mode: service.ChannelMonitorCostConversionNone})
					require.NoError(t, err)
					require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: channelID, Ratio: 1, UpdatedTime: 1, CostConversion: conversion}).Error)
					t.Cleanup(func() {
						assert.NoError(t, service.FlushChannelDailyCostEvents())
						for _, table := range []any{&model.ChannelRatioMonitor{}, &model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}} {
							assert.NoError(t, db.Where("channel_id = ?", channelID).Delete(table).Error)
						}
					})
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/rerank", nil)
					service.CaptureChannelDailyCostSnapshot(ctx, channelID)
					service.BeginChannelDailyCostAttempt(ctx, channelID)
					info := &relaycommon.RelayInfo{
						ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID, ChannelType: tc.channelType, UpstreamModelName: "served-reranker",
							ChannelOtherSettings: dto.ChannelOtherSettings{AdvancedCustom: common.GetAdvancedCustomPreset(constant.ChannelTypeSGLang)}},
						RequestURLPath: "/v1/rerank", RelayMode: relayconstant.RelayModeRerank, RelayFormat: types.RelayFormatOpenAI,
						RerankerInfo: &relaycommon.RerankerInfo{Documents: []any{"first", "second"}},
						PriceData:    hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 2}},
					}
					info.SetEstimatePromptTokens(12)
					if tc.expression != "" {
						info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
							BillingMode: "tiered_expr", ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression),
							QuotaPerUnit: common.QuotaPerUnit, GroupRatio: 2, EstimatedQuotaAfterGroup: 10_000,
						}
					} else if tc.perCall {
						info.PriceData.UsePrice, info.PriceData.ModelPrice = true, 0.01
					}
					adaptor := &advancedcustom.Adaptor{}
					adaptor.Init(info)
					value, apiErr := adaptor.DoResponse(ctx, &http.Response{StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader(`[{"index":1,"score":0.9,"meta_info":{"prompt_tokens":5}}]`))}, info)
					require.Nil(t, apiErr)
					usage, ok := value.(*dto.Usage)
					require.True(t, ok)
					assert.Equal(t, &dto.Usage{PromptTokens: 12, TotalTokens: 12}, usage, "official billing usage stays unchanged")
					assert.False(t, service.CalculateChannelModelDetectionQuota(ctx, info, usage).Reliable)
					service.RecordChannelTestDailyCost(ctx, info, 0, nil, usage, true)
					require.NoError(t, service.FlushChannelDailyCostEvents())
					var cost model.ChannelDailyCost
					require.NoError(t, db.Where("channel_id = ?", channelID).Take(&cost).Error)
					if tc.perCall {
						assert.Equal(t, int64(10_000_000), cost.CostNanoCNY, "cost excludes the user's group multiplier")
						assert.Equal(t, int64(1), cost.SettledCount)
						assert.Zero(t, cost.UnresolvedCount)
					} else {
						assert.Zero(t, cost.CostNanoCNY)
						assert.Zero(t, cost.SettledCount)
						assert.Equal(t, int64(1), cost.UnresolvedCount)
						assert.Nil(t, service.ChannelDailyCostAttemptSettledCost(ctx, channelID))
					}
				})
			}
		})
	}
}

func TestSGLangRerankRejectsMalformedResults(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[{"index":99,"score":1}]`, `[{"index":-1,"score":0}]`, `[{"index":0}]`, `[{"score":1}]`} {
		t.Run(body, func(t *testing.T) {
			adaptor := &advancedcustom.Adaptor{}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelOtherSettings: dto.ChannelOtherSettings{AdvancedCustom: common.GetAdvancedCustomPreset(constant.ChannelTypeSGLang)}}, RequestURLPath: "/v1/rerank", RelayMode: relayconstant.RelayModeRerank, RerankerInfo: &relaycommon.RerankerInfo{Documents: []any{"only document"}}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			usage, err := adaptor.DoResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, info)
			require.NotNil(t, err)
			assert.Nil(t, usage)
			assert.Empty(t, recorder.Body.String())
		})
	}
}
