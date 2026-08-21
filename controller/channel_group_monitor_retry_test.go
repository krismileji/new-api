/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunChannelGroupMonitorGroupRetriesLikeRelay(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalStreamingTimeout })

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = originalRetryTimes })

	user := model.User{
		Username: "group-monitor-retry-user", Password: "password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)

	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if requestCount.Load() == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, err := w.Write([]byte(`{"error":{"message":"temporary failure"}}`))
			assert.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, "data: {\"id\":\"probe\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	priority := int64(100)
	weight := uint(10)
	channels := []model.Channel{
		{Id: 1201, Name: "group-monitor-retry-a", Type: constant.ChannelTypeOpenAI, Key: "key-a", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL, Models: "gpt-4.1", Group: "default", Priority: &priority, Weight: &weight},
		{Id: 1202, Name: "group-monitor-retry-b", Type: constant.ChannelTypeOpenAI, Key: "key-b", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL, Models: "gpt-4.1", Group: "default", Priority: &priority, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-4.1", ChannelId: 1201, Enabled: true, Priority: &priority, Weight: weight},
		{Group: "default", Model: "gpt-4.1", ChannelId: 1202, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorExecution{},
		&model.ChannelGroupMonitorState{},
	))

	config := model.ChannelGroupMonitorConfig{Enabled: true, Revision: 1}
	claim := model.ChannelGroupMonitorClaim{
		RunId: "group-monitor-retry-run", Config: config,
	}
	validCandidates := map[string][]string{"default": {"gpt-4.1"}}
	err := runChannelGroupMonitorGroup(
		context.Background(), claim,
		model.ChannelGroupMonitorGroup{GroupName: "default", ProbeModel: "gpt-4.1"},
		validCandidates, user.Id, nil,
	)
	require.NoError(t, err)

	assert.Equal(t, int64(2), requestCount.Load())
	var execution model.ChannelGroupMonitorExecution
	require.NoError(t, db.Where("run_id = ?", claim.RunId).First(&execution).Error)
	assert.Equal(t, model.ChannelGroupMonitorResultSuccess, execution.Result)
}

func TestRunChannelGroupMonitorGroupSkipsSetupFailureWithoutRetryBudget(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalStreamingTimeout })

	originalRetryTimes := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = originalRetryTimes })

	user := model.User{
		Username: "group-monitor-setup-retry-user", Password: "password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)

	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, "data: {\"id\":\"probe\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	highPriority := int64(200)
	lowPriority := int64(100)
	weight := uint(10)
	channels := []model.Channel{
		{
			Id: 1211, Name: "group-monitor-no-key", Type: constant.ChannelTypeOpenAI,
			Key: "disabled-key", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL,
			Models: "gpt-4.1", Group: "default", Priority: &highPriority, Weight: &weight,
			ChannelInfo: model.ChannelInfo{
				IsMultiKey: true, MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			},
		},
		{Id: 1212, Name: "group-monitor-available", Type: constant.ChannelTypeOpenAI, Key: "key", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL, Models: "gpt-4.1", Group: "default", Priority: &lowPriority, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-4.1", ChannelId: 1211, Enabled: true, Priority: &highPriority, Weight: weight},
		{Group: "default", Model: "gpt-4.1", ChannelId: 1212, Enabled: true, Priority: &lowPriority, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorExecution{},
		&model.ChannelGroupMonitorState{},
	))

	claim := model.ChannelGroupMonitorClaim{
		RunId:  "group-monitor-setup-retry-run",
		Config: model.ChannelGroupMonitorConfig{Enabled: true, Revision: 1},
	}
	err := runChannelGroupMonitorGroup(
		context.Background(), claim,
		model.ChannelGroupMonitorGroup{GroupName: "default", ProbeModel: "gpt-4.1"},
		map[string][]string{"default": {"gpt-4.1"}}, user.Id, nil,
	)
	require.NoError(t, err)

	assert.Equal(t, int64(1), requestCount.Load())
	var execution model.ChannelGroupMonitorExecution
	require.NoError(t, db.Where("run_id = ?", claim.RunId).First(&execution).Error)
	assert.Equal(t, model.ChannelGroupMonitorResultSuccess, execution.Result)
	assert.Equal(t, 1212, execution.ChannelId)
}
