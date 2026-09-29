package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesProtocolLocalProbeBeforePluginSelection(t *testing.T) {
	require.NoError(t, i18n.Init())
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMemory := common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.MemoryCacheEnabled = oldMemory
	})
	setupRelayRouterTestDB(t)
	common.MemoryCacheEnabled = false
	oldRegistry := pluginruntime.DefaultRegistry
	oldLimit := setting.ModelRequestRateLimitEnabled
	oldCount, oldSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	oldDuration := setting.ModelRequestRateLimitDurationMinutes
	oldGroups := setting.ModelRequestRateLimitGroup2JSONString()
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{
		channelprobe.OptionKey: "true", channelprobe.MatchInputOptionKey: "health check",
		channelprobe.ResponseTextOptionKey: "本地探针正常", channelprobe.MinDelayMsOptionKey: "0",
		channelprobe.MaxDelayMsOptionKey: "0", channelprobe.AllowedIPsOptionKey: "192.0.2.1",
	}
	common.OptionMapRWMutex.Unlock()
	pluginruntime.DefaultRegistry = pluginruntime.NewRegistry()
	setting.ModelRequestRateLimitEnabled = false
	setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = 1, 0
	setting.ModelRequestRateLimitDurationMinutes = 1
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{}`))
	t.Cleanup(func() {
		pluginruntime.DefaultRegistry = oldRegistry
		setting.ModelRequestRateLimitEnabled = oldLimit
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = oldCount, oldSuccess
		setting.ModelRequestRateLimitDurationMinutes = oldDuration
		require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(oldGroups))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	user := model.User{Id: 190013, Username: "responses-probe-user", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId: user.Id, Key: "responsesprobetoken", Status: common.TokenStatusEnabled,
		ExpiredTime: -1, UnlimitedQuota: true,
	}).Error)
	_, err := pluginruntime.DefaultRegistry.Register(`
export const meta={apiVersion:1,key:"responses-probe",name:"Responses probe",version:"1.0.0",author:{name:"Test"},models:["probe-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","stream","background"]}]};
export function buildSubmitRequest(){throw new Error("must not submit");}
export function parseSubmitResponse(){return {taskId:"unused"};}
export function buildQueryRequest(){return {url:"https://example.invalid/task"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export const protocols={openai_responses:{
 decodeRequest:function(ctx){throw new Error("probe plugin received input: " + ctx.body.value.input);},
 renderFinal:function(){return {};}, renderEvents:function(){return {events:[],done:false};}
}};
`, pluginruntime.Options{})
	require.NoError(t, err)
	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	for _, tc := range []struct {
		name, path, body, wantText                   string
		disabled, deniedIP, unauthenticated, limited bool
		wantStatus                                   int
	}{
		{name: "claimed JSON", body: `{"model":"probe-model","input":"health check"}`, wantText: `"text":"本地探针正常"`, wantStatus: 200},
		{name: "claimed stream", body: `{"model":"probe-model","input":"health check","stream":true}`, wantText: "event: response.completed", wantStatus: 200},
		{name: "unclaimed model", body: `{"model":"unclaimed-model","input":"health check"}`, wantText: `"text":"本地探针正常"`, wantStatus: 200},
		{name: "disabled", body: `{"model":"probe-model","input":"health check"}`, disabled: true, wantText: "probe plugin received input: health check", wantStatus: 400},
		{name: "IP outside allowlist", body: `{"model":"probe-model","input":"health check"}`, deniedIP: true, wantText: "probe plugin received input: health check", wantStatus: 400},
		{name: "nonmatching body remains readable", body: `{"model":"probe-model","input":"real question"}`, wantText: "probe plugin received input: real question", wantStatus: 400},
		{name: "previous response bypasses local probe", body: `{"model":"probe-model","input":"health check","previous_response_id":"resp_prior"}`, wantText: "probe plugin received input: health check", wantStatus: 400},
		{name: "malformed JSON", body: `{"model":`, wantText: "Invalid task protocol request", wantStatus: 400},
		{name: "authentication before local response", body: `{"model":"probe-model","input":"health check"}`, unauthenticated: true, wantStatus: 401},
		{name: "chat route preserved", path: "/v1/chat/completions", body: `{"model":"probe-model","messages":[{"role":"user","content":"health check"}]}`, wantText: `"content":"本地探针正常"`, wantStatus: 200},
		{name: "probe still consumes rate limit", body: `{"model":"probe-model","input":"health check"}`, limited: true, wantText: `"text":"本地探针正常"`, wantStatus: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			common.OptionMap[channelprobe.OptionKey] = "true"
			if tc.disabled {
				common.OptionMap[channelprobe.OptionKey] = "false"
			}
			common.OptionMapRWMutex.Unlock()
			setting.ModelRequestRateLimitEnabled = tc.limited
			path := tc.path
			if path == "" {
				path = "/v1/responses"
			}
			requests := 1
			if tc.limited {
				requests = 2
			}
			for attempt := 0; attempt < requests; attempt++ {
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tc.body))
				request.RemoteAddr = "192.0.2.1:12345"
				if tc.deniedIP {
					request.RemoteAddr = "192.0.2.2:12345"
				}
				request.Header.Set("Content-Type", "application/json")
				if !tc.unauthenticated {
					request.Header.Set("Authorization", "Bearer sk-responsesprobetoken")
				}
				recorder := httptest.NewRecorder()
				engine.ServeHTTP(recorder, request)
				if attempt == 1 {
					assert.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
					assert.NotContains(t, recorder.Body.String(), "本地探针正常")
					continue
				}
				require.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
				if tc.wantText != "" {
					assert.Contains(t, recorder.Body.String(), tc.wantText)
				}
			}
		})
	}
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	assert.Equal(t, user.Quota, after.Quota)
	assert.Zero(t, after.UsedQuota)
}
