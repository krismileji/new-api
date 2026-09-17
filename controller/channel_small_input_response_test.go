package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmallInputResponseThresholdAndManualBoundary(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 17, Name: "small", Key: "test"}).Error)
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`, &request))
	info := &relaycommon.RelayInfo{Request: &request, OriginModelName: "gpt-4o", RelayMode: relayconstant.RelayModeChatCompletions}
	tokens, _, valid := service.EstimateChannelSmallInput(info)
	require.True(t, valid)
	for _, tc := range []struct {
		name                         string
		threshold                    int
		auto, enabled, manual, match bool
	}{
		{"below", tokens + 1, true, true, false, true},
		{"equal", tokens, true, true, false, false},
		{"above", tokens - 1, true, true, false, false},
		{"auto allowed", tokens + 1, false, true, false, false},
		{"feature off", tokens + 1, true, false, false, false},
		{"manual", tokens + 1, true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := model.GetChannelProbePolicy(t.Context(), 17)
			require.NoError(t, err)
			policy.AutoProbeDisabled = tc.auto
			policy.SmallInputResponseEnabled = tc.enabled
			policy.SmallInputThresholdTokens = tc.threshold
			policy.SmallInputResponseText = "自定义\n响应"
			_, _, err = model.SaveChannelProbePolicy(t.Context(), 17, policy)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
			c.Set("channel_test", tc.manual)
			handled, apiErr := tryChannelSmallInputResponse(c, info, 17)
			require.Nil(t, apiErr)
			assert.Equal(t, tc.match, handled)
			if tc.match {
				assert.Contains(t, recorder.Body.String(), "自定义")
				assert.Equal(t, "local_response", recorder.Header().Get("X-New-Api-Response-Source"))
			} else {
				assert.Empty(t, recorder.Body.String())
			}
		})
	}
}

func TestChannelSmallInputResponseAfterConcurrencyReselection(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	high, low, weight := int64(100), int64(90), uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 101, Name: "busy", Key: "test", Group: "vip", Models: "gpt-4o", Status: common.ChannelStatusEnabled, Priority: &high, Weight: &weight},
		{Id: 102, Name: "local", Key: "test", Group: "vip", Models: "gpt-4o", Status: common.ChannelStatusEnabled, Priority: &low, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{{ChannelId: 101, Group: "vip", Model: "gpt-4o", Enabled: true, Priority: &high, Weight: weight}, {ChannelId: 102, Group: "vip", Model: "gpt-4o", Enabled: true, Priority: &low, Weight: weight}}).Error)
	_, _, err := model.SaveChannelProbePolicy(t.Context(), 102, model.ChannelProbePolicy{AutoProbeDisabled: true, SmallInputResponseEnabled: true, SmallInputThresholdTokens: 1000, SmallInputResponseText: "备用渠道本地响应"})
	require.NoError(t, err)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	_, err = service.SaveChannelConcurrencyLimit(t.Context(), 101, 1)
	require.NoError(t, err)
	held, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), 101)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(held.Release)
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`, &request))
	info := &relaycommon.RelayInfo{Request: &request, OriginModelName: "gpt-4o", RelayMode: relayconstant.RelayModeChatCompletions, TokenGroup: "vip", UsingGroup: "vip"}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	retry := &service.RetryParam{Ctx: c, TokenGroup: "vip", ModelName: "gpt-4o", RequestPath: c.Request.URL.Path, Retry: common.GetPointer(0)}
	channel, err := model.GetChannelById(101, true)
	require.NoError(t, err)
	selected, lease, apiErr := acquireRelayChannelConcurrency(c, info, retry, newRelayRetryRouting(), channel, true)
	require.Nil(t, apiErr)
	require.Nil(t, lease)
	require.NotNil(t, selected)
	assert.Equal(t, 102, selected.Id)
	assert.Equal(t, 102, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	assert.True(t, c.GetBool(service.ChannelLocalResponseContextKey))
	assert.Contains(t, recorder.Body.String(), "备用渠道本地响应")
}
