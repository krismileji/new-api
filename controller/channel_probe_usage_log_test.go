package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTestUsageLogFollowsProbeResponseSetting(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.ChannelRatioMonitor{}))
	withSelfUseModeEnabled(t)
	service.InitHttpClient()

	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	common.OptionMapRWMutex.Lock()
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	originalProbeResponseEnabled, hadProbeResponseSetting := common.OptionMap[channelprobe.OptionKey]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.OptionMapRWMutex.Lock()
		if optionMapWasNil {
			common.OptionMap = nil
		} else if hadProbeResponseSetting {
			common.OptionMap[channelprobe.OptionKey] = originalProbeResponseEnabled
		} else {
			delete(common.OptionMap, channelprobe.OptionKey)
		}
		common.OptionMapRWMutex.Unlock()
		service.ResetChannelDailyCostSnapshotCache()
	})

	user := &model.User{
		Username: "channel-test-user",
		Password: "channel-test-password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(user).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"gpt-3.5-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"Hi. What are you working on?"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	channel := &model.Channel{
		Id:      42,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-channel-test",
		Name:    "channel test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
		Models:  "gpt-3.5-turbo",
		Group:   "default",
	}

	tests := []struct {
		name            string
		probeEnabled    string
		wantConsumeLogs int64
	}{
		{name: "enabled", probeEnabled: "true", wantConsumeLogs: 0},
		{name: "disabled", probeEnabled: "false", wantConsumeLogs: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			common.OptionMap[channelprobe.OptionKey] = test.probeEnabled
			common.OptionMapRWMutex.Unlock()

			result := testChannel(context.Background(), channel, user.Id, "gpt-3.5-turbo", "", false)

			require.NoError(t, result.localErr)
			var consumeLogCount int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
			assert.Equal(t, test.wantConsumeLogs, consumeLogCount)
		})
	}
}
