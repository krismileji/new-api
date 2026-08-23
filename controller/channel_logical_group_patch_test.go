package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateGroupedChannelPartialPatchPreservesEffectiveAddress(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionConfig{}, &model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionLogicalConfig{}, &model.ChannelModelDetectionLogicalTarget{},
	))
	address := "https://shared.example.com/v1"
	weight := uint(10)
	channels := []*model.Channel{
		{Id: 2601, Name: "patch-a", Type: constant.ChannelTypeAnthropic, Key: "key-a", Status: common.ChannelStatusEnabled, BaseURL: &address, Weight: &weight},
		{Id: 2602, Name: "patch-b", Type: constant.ChannelTypeAnthropic, Key: "key-b", Status: common.ChannelStatusEnabled, BaseURL: &address, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	group, err := service.CreateLogicalChannelGroup("partial patch", "", 0, []service.LogicalChannelGroupMemberInput{{ChannelID: 2601}, {ChannelID: 2602}})
	require.NoError(t, err)

	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "name only", body: map[string]any{"id": 2601, "name": "renamed"}},
		{name: "key only", body: map[string]any{"id": 2601, "key": "new-key"}},
		{name: "weight only", body: map[string]any{"id": 2601, "weight": 25}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel/", test.body)
			ctx.Set("role", common.RoleRootUser)
			UpdateChannel(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.True(t, response.Success, response.Message)

			var stored model.Channel
			require.NoError(t, db.First(&stored, 2601).Error)
			assert.Equal(t, constant.ChannelTypeAnthropic, stored.Type)
			assert.Equal(t, address, stored.GetBaseURL())
			storedGroup, getErr := service.GetLogicalChannelGroup(group.ID)
			require.NoError(t, getErr)
			assert.Equal(t, group.Revision, storedGroup.Revision, "non-address patch must not revise the logical group")
		})
	}
}
