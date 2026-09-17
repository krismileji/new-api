package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelProbePolicyAPI(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 41, Name: "probe policy", Key: "private"}).Error)
	router := gin.New()
	router.GET("/:id", GetChannelMonitorProbePolicy)
	router.PUT("/:id", UpdateChannelMonitorProbePolicy)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"default", "GET", "/41", "", 200},
		{"missing channel", "GET", "/42", "", 404},
		{"missing fields", "PUT", "/41", `{}`, 400},
		{"invalid threshold", "PUT", "/41", `{"auto_probe_disabled":true,"small_input_response_enabled":true,"small_input_threshold_tokens":0,"small_input_response_text":"hello","probe_policy_revision":0}`, 400},
		{"save", "PUT", "/41", `{"auto_probe_disabled":true,"small_input_response_enabled":true,"small_input_threshold_tokens":1000,"small_input_response_text":"hello","probe_policy_revision":0}`, 200},
		{"stale edit", "PUT", "/41", `{"auto_probe_disabled":false,"small_input_response_enabled":false,"small_input_threshold_tokens":0,"small_input_response_text":"","probe_policy_revision":0}`, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body)))
			assert.Equal(t, tc.status, response.Code, response.Body.String())
			assert.NotContains(t, response.Body.String(), "private")
		})
	}
	policy, err := model.GetChannelProbePolicy(t.Context(), 41)
	require.NoError(t, err)
	assert.True(t, policy.AutoProbeDisabled)
	assert.True(t, policy.SmallInputResponseEnabled)
	assert.EqualValues(t, 1, policy.ProbePolicyRevision)
}

func TestChannelProbePolicyStopsAutomaticDispatchButKeepsManual(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	var dispatched atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"test upstream","type":"rate_limit_error"}}`))
	}))
	t.Cleanup(upstream.Close)
	user := &model.User{Username: "manual-probe", Group: "default", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Quota: 1_000_000}
	require.NoError(t, db.Create(user).Error)
	channel := &model.Channel{Id: 42, Type: constant.ChannelTypeOpenAI, Name: "forbidden", Key: "private", Models: "gpt-4o", Status: common.ChannelStatusEnabled}
	channel.BaseURL = common.GetPointer(upstream.URL)
	channel.Group = "default"
	require.NoError(t, db.Create(channel).Error)
	_, _, err := model.SaveChannelProbePolicy(t.Context(), channel.Id, model.ChannelProbePolicy{AutoProbeDisabled: true})
	require.NoError(t, err)
	scheduled := service.WithChannelProbeTrigger(t.Context(), model.ChannelStatusProbeTriggerScheduled)
	manual := service.WithChannelProbeTrigger(t.Context(), model.ChannelStatusProbeTriggerManual)
	require.ErrorIs(t, service.CheckChannelProbeAllowed(scheduled, channel.Id), service.ErrChannelAutoProbeDisabled)
	require.NoError(t, service.CheckChannelProbeAllowed(manual, channel.Id))
	require.ErrorIs(t, service.CheckChannelProbeAllowed(scheduled, 9999), service.ErrChannelProbePolicyUnavailable)
	result := testChannel(scheduled, channel, 0, "gpt-4o", "", true)
	require.ErrorIs(t, result.localErr, service.ErrChannelAutoProbeDisabled)
	assert.False(t, result.requestDispatched)
	outcome := executeChannelStatusProbeModel(scheduled, channel, 0, "gpt-4o")
	assert.Equal(t, model.ChannelStatusProbeResultSkipped, outcome.Result)
	assert.False(t, outcome.TestExecuted)
	summary := testChannelForHealthCheck(scheduled, channel, 0, true, 1)
	assert.Equal(t, channelTestSummary{}, summary)
	assert.Zero(t, dispatched.Load())
	manualOutcome := executeChannelStatusProbeModel(manual, channel, user.Id, "gpt-4o")
	assert.True(t, manualOutcome.ProbeResult.requestDispatched)
	assert.EqualValues(t, 1, dispatched.Load())
	require.NoError(t, db.Migrator().DropTable(&model.ChannelRatioMonitor{}))
	require.ErrorIs(t, service.CheckChannelProbeAllowed(scheduled, channel.Id), service.ErrChannelProbePolicyUnavailable)
	require.NoError(t, service.CheckChannelProbeAllowed(manual, channel.Id))
}

func TestChannelProbePolicyStatusFailoverSkipsForbiddenMember(t *testing.T) {
	channels := map[int]*model.Channel{
		1: {Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-4o"},
		2: {Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-4o"},
	}
	snapshot := model.LogicalChannelGroupSnapshot{LogicalChannelID: 7, Revision: 1, Status: model.ChannelLogicalGroupStatusEnabled,
		Members: []model.LogicalChannelMemberSnapshot{{ChannelID: 1, Weight: 1}, {ChannelID: 2, Weight: 0}}}
	outcome, err := executeChannelStatusProbeModelWithMemberFailover(snapshot, channels, "gpt-4o", nil, func(channel *model.Channel) channelStatusProbeOutcome {
		if channel.Id == 1 {
			return channelProbePolicySkippedOutcome(service.ErrChannelAutoProbeDisabled)
		}
		return channelStatusProbeOutcome{Result: model.ChannelStatusProbeResultSuccess, TestExecuted: true}
	})
	require.NoError(t, err)
	assert.Equal(t, 2, outcome.ActualChannelId)
	assert.Equal(t, model.ChannelStatusProbeResultSuccess, outcome.Result)
}
