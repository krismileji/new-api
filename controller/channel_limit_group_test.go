package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sharedLimitControllerFixture(t *testing.T) *model.ChannelLimitGroup {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelLimitGroup{}, &model.ChannelLimitGroupTier{}, &model.ChannelLimitGroupMember{}, &model.ChannelLimitGroupRevision{}))
	useChannelMonitorOptionMap(t, map[string]string{channelMonitorChannelConcurrencyWaitSecondsOption: "0"})
	for id := 1; id <= 4; id++ {
		priority, weight := int64(100-id), uint(1)
		group := "vip"
		if id == 4 {
			group = "private"
		}
		require.NoError(t, db.Create(&model.Channel{Id: id, Name: fmt.Sprintf("渠道%d", id), Type: 1, Key: "test-key", Status: common.ChannelStatusEnabled, Group: group, Models: "model-a", Priority: &priority, Weight: &weight}).Error)
		require.NoError(t, db.Create(&model.Ability{Group: group, Model: "model-a", ChannelId: id, Enabled: true, Priority: &priority, Weight: weight}).Error)
	}
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	group := &model.ChannelLimitGroup{Name: "上游", Enabled: true, ConcurrencyLimit: 1, RPMLimit: 10, Tiers: []model.ChannelLimitGroupTier{{Priority: 100}}, Members: []model.ChannelLimitGroupMember{{ChannelID: 1, Priority: 100}, {ChannelID: 2, Priority: 100}}}
	require.NoError(t, service.SaveChannelLimitGroup(t.Context(), group, false))
	return group
}

func TestSharedLimitRelayFallbackPreservesRoutingAndSpecificChannel(t *testing.T) {
	for _, specific := range []bool{false, true} {
		t.Run(fmt.Sprintf("specific=%t", specific), func(t *testing.T) {
			sharedLimitControllerFixture(t)
			held, ok, _, err := service.AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if specific {
				ctx.Set("specific_channel_id", "1")
			}
			retry := &service.RetryParam{Ctx: ctx, TokenGroup: "vip", ModelName: "model-a", RequestPath: ctx.Request.URL.Path, Retry: common.GetPointer(0)}
			info := &relaycommon.RelayInfo{OriginModelName: "model-a", TokenGroup: "vip", UsingGroup: "vip"}
			first, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			routing := newRelayRetryRouting()
			selected, lease, apiErr := acquireRelayChannelConcurrency(ctx, info, retry, routing, first, true)
			if specific {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Nil(t, selected)
				assert.Nil(t, lease)
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, lease)
				t.Cleanup(lease.Release)
				assert.Equal(t, 3, selected.Id)
				assert.Contains(t, routing.excluded, 2)
			}
			views, err := service.ListChannelLimitGroupViews(t.Context())
			require.NoError(t, err)
			assert.Equal(t, 1, views[0].Runtime.RPM)
			assert.Zero(t, views[0].Runtime.Waiting)
			unchanged, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusEnabled, unchanged.Status)
			assert.Zero(t, unchanged.UsedQuota)
		})
	}
}

func TestSharedLimitManagementAPIRejectsStaleAndIncompleteConfig(t *testing.T) {
	group := sharedLimitControllerFixture(t)
	router := gin.New()
	router.PUT("/limit-groups/:id", SaveChannelLimitGroup)
	router.GET("/limit-groups", ListChannelLimitGroups)
	group.Revision--
	body, err := common.Marshal(group)
	require.NoError(t, err)
	for _, input := range []struct {
		method, path string
		body         []byte
		status       int
	}{
		{http.MethodPut, fmt.Sprintf("/limit-groups/%d", group.ID), body, http.StatusConflict},
		{http.MethodPut, fmt.Sprintf("/limit-groups/%d", group.ID), []byte(`{"name":"incomplete"}`), http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(input.method, input.path, bytes.NewReader(input.body)))
		assert.Equal(t, input.status, recorder.Code, recorder.Body.String())
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/limit-groups", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                            `json:"success"`
		Data    []service.ChannelLimitGroupView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 1)
	assert.Equal(t, group.Revision+1, response.Data[0].Revision)
	assert.Empty(t, response.Data[0].Runtime.Reason)
}
