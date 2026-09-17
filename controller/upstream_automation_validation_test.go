package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAutomationInvalidConfigMessages(t *testing.T) {
	for _, endpoint := range []struct {
		name    string
		method  string
		path    string
		handler gin.HandlerFunc
	}{
		{"保存", http.MethodPut, "/api/channel_monitor/automations", SaveUpstreamAutomation},
		{"测试指标", http.MethodPost, "/api/channel_monitor/automations/test", TestUpstreamAutomation},
		{"获取变量", http.MethodPost, "/api/channel_monitor/automations/variable/fetch", FetchUpstreamAutomationDraftVariables},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, test := range []struct {
				name string
				body string
				want string
			}{
				{"检查间隔为字符串", `{"interval_minutes":"1"}`, "检查间隔（分钟）必须以整数提交，请刷新页面后重试"},
				{"超时为小数", `{"request_timeout":1.5}`, "请求超时（秒）必须以整数提交，请刷新页面后重试"},
				{"规则阈值类型无效", `{"custom_config":{"actions":[{"threshold":"secret-value"}]}}`, "自动任务配置字段「custom_config.actions.threshold」类型无效，请检查填写内容"},
				{"超出请求限制", `{"proxy":"` + strings.Repeat("x", 128<<10) + `"}`, "自动任务配置请求超过 128 KiB，请减少请求正文或变量内容"},
				{"JSON 不完整", `{"name":"secret-value"`, "自动任务配置 JSON 格式无效或内容不完整，请刷新页面后重试"},
				{"JSON 不是对象", `[]`, "自动任务配置必须是 JSON 对象"},
			} {
				t.Run(test.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(test.body))
					ctx.Request.Header.Set("Content-Type", "application/json")
					endpoint.handler(ctx)
					var response struct {
						Success bool   `json:"success"`
						Message string `json:"message"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					assert.False(t, response.Success)
					assert.Equal(t, test.want, response.Message)
					assert.NotContains(t, recorder.Body.String(), "secret-value")
				})
			}
		})
	}
}
