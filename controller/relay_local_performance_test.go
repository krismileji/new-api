package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayLocalResponseDoesNotSampleUpstreamPerformance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metricsSetting, ok := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	require.True(t, ok)
	previousEnabled := metricsSetting.Enabled
	previousSensitive := setting.CheckSensitiveEnabled
	metricsSetting.Enabled = true
	setting.CheckSensitiveEnabled = false
	t.Cleanup(func() {
		metricsSetting.Enabled = previousEnabled
		setting.CheckSensitiveEnabled = previousSensitive
	})
	for _, dialect := range []struct{ name, env string }{
		{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dialect.env != "" && dsn == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.name, dsn)
			require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.ChannelSmartScheduleRouteState{}, &model.PerfMetric{}))
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer upstream.Close()
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
					modelName := fmt.Sprintf("gpt-4o-local-metrics-%s-%t", dialect.name, stream)
					channel := model.Channel{Name: modelName, Models: modelName, Group: "default", Type: constant.ChannelTypeOpenAI,
						Key: "test", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled}
					require.NoError(t, db.Create(&channel).Error)
					_, _, err := model.SaveChannelProbePolicy(t.Context(), channel.Id, model.ChannelProbePolicy{
						AutoProbeDisabled: true, SmallInputResponseEnabled: true, SmallInputThresholdTokens: 1000, SmallInputResponseText: "本地测试响应",
					})
					require.NoError(t, err)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":%t}`, modelName, stream)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					common.SetContextKey(c, constant.ContextKeyUserId, 1)
					common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
					common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
					common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
					require.Nil(t, middleware.SetupContextForSelectedChannel(c, &channel, modelName))
					Relay(c, types.RelayFormatOpenAI)
					assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					assert.Equal(t, "local_response", recorder.Header().Get("X-New-Api-Response-Source"))
					assert.Contains(t, recorder.Body.String(), "本地测试响应")
					assert.Zero(t, calls.Load())
					metrics, err := perfmetrics.Query(perfmetrics.QueryParams{Model: modelName, Hours: 24})
					require.NoError(t, err)
					assert.Nil(t, metrics.Summary, "a local response must not improve upstream health")
					assert.Empty(t, metrics.Series)
					assert.Empty(t, metrics.Groups)
				})
			}
		})
	}
}
