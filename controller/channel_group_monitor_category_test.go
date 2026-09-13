package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelGroupMonitorSettingsCategory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		category string
		want     string
		invalid  bool
	}{
		{name: "omitted category stays uncategorized"},
		{name: "whitespace becomes uncategorized", category: "  "},
		{name: "category is trimmed and persisted", category: "  编程模型  ", want: "编程模型"},
		{name: "64 Unicode characters are accepted", category: strings.Repeat("🚀", 64), want: strings.Repeat("🚀", 64)},
		{name: "long category is rejected before saving", category: strings.Repeat("类", 65), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
			require.NoError(t, db.Create(&model.Channel{
				Id: 907, Name: "分类配置测试渠道", Type: constant.ChannelTypeOpenAI,
				Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4.1",
			}).Error)
			require.NoError(t, db.Create(&model.Ability{
				Group: "default", Model: "gpt-4.1", ChannelId: 907, Enabled: true,
			}).Error)
			body, err := common.Marshal(map[string]any{
				"enabled": true,
				"groups": []model.ChannelGroupMonitorGroup{{
					GroupName: "default", ProbeModel: "gpt-4.1", Category: tc.category,
				}},
				"interval_seconds": 60, "display_value": 60, "display_unit": "minute", "revision": 0,
			})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/channel_monitor/group_monitor/settings", bytes.NewReader(body))
			UpdateChannelGroupMonitorSettings(c)
			if tc.invalid {
				assert.Equal(t, http.StatusBadRequest, recorder.Code)
				assert.Contains(t, recorder.Body.String(), "分类名称不能超过 64 个字符")
				var count int64
				require.NoError(t, db.Model(&model.ChannelGroupMonitorConfig{}).Count(&count).Error)
				assert.Zero(t, count)
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

			recorder = httptest.NewRecorder()
			c, _ = gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/group_monitor/settings", nil)
			GetChannelGroupMonitorSettings(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			var payload struct {
				Success bool `json:"success"`
				Data    struct {
					Settings channelGroupMonitorConfigResponse `json:"settings"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			require.True(t, payload.Success)
			require.Len(t, payload.Data.Settings.Groups, 1)
			assert.Equal(t, tc.want, payload.Data.Settings.Groups[0].Category)
		})
	}
}

func putChannelGroupMonitorCategories(t *testing.T, categories []string, groups []model.ChannelGroupMonitorGroup, revision int64) *httptest.ResponseRecorder {
	t.Helper()
	request := map[string]any{
		"enabled": false, "groups": groups, "revision": revision,
		"interval_seconds": 60, "display_value": 60, "display_unit": "minute",
	}
	if categories != nil {
		request["categories"] = categories
	}
	body, err := common.Marshal(request)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/channel_monitor/group_monitor/settings", bytes.NewReader(body))
	UpdateChannelGroupMonitorSettings(c)
	return recorder
}

func TestChannelGroupMonitorCategoryManagement(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
	require.NoError(t, db.Create(&model.Channel{
		Id: 920, Name: "分类管理测试渠道", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-4.1", ChannelId: 920, Enabled: true},
		{Group: "vip", Model: "gpt-4.1", ChannelId: 920, Enabled: true},
	}).Error)

	categories := []string{"通用模型", "编程模型", "预留分类"}
	response := putChannelGroupMonitorCategories(t, categories, []model.ChannelGroupMonitorGroup{}, 0)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var payload struct {
		Data channelGroupMonitorConfigResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.Equal(t, categories, payload.Data.Categories)
	assert.Empty(t, payload.Data.Groups)

	// Updating through an older client must not discard saved empty categories.
	response = putChannelGroupMonitorCategories(t, nil, []model.ChannelGroupMonitorGroup{}, 1)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	stored, err := model.GetChannelGroupMonitorConfig()
	require.NoError(t, err)
	savedCategories, err := stored.Categories()
	require.NoError(t, err)
	assert.Equal(t, categories, savedCategories)

	groups := []model.ChannelGroupMonitorGroup{
		{GroupName: "vip", ProbeModel: "gpt-4.1", Category: "编程模型"},
		{GroupName: "default", ProbeModel: "gpt-4.1", Category: "通用模型"},
	}
	response = putChannelGroupMonitorCategories(t, categories, groups, 2)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.Equal(t, []model.ChannelGroupMonitorGroup{groups[1], groups[0]}, payload.Data.Groups)

	// Rename, reorder, and remove an empty category in one revision.
	groups[0].Category = "编程服务"
	categories = []string{"编程服务", "通用模型"}
	response = putChannelGroupMonitorCategories(t, categories, groups, 3)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = putChannelGroupMonitorCategories(t, []string{}, []model.ChannelGroupMonitorGroup{}, 3)
	assert.Equal(t, http.StatusConflict, response.Code)
	stored, err = model.GetChannelGroupMonitorConfig()
	require.NoError(t, err)
	savedCategories, err = stored.Categories()
	require.NoError(t, err)
	assert.Equal(t, categories, savedCategories)
	savedGroups, err := stored.Groups()
	require.NoError(t, err)
	assert.Equal(t, groups, savedGroups)
}

func TestChannelGroupMonitorCategoryValidationRejectsInvalidHierarchy(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
	require.NoError(t, db.Create(&model.Channel{
		Id: 921, Name: "分类校验测试渠道", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-4.1", ChannelId: 921, Enabled: true}).Error)
	for _, tc := range []struct {
		name       string
		categories []string
		message    string
	}{
		{name: "missing parent", categories: []string{}, message: "请先创建监控分组所属的分类"},
		{name: "blank category", categories: []string{" "}, message: "分类名称不能为空"},
		{name: "duplicate names after trimming", categories: []string{"通用模型", " 通用模型 "}, message: "分类名称不能重复"},
		{name: "long name", categories: []string{strings.Repeat("类", 65)}, message: "不能超过 64 个字符"},
		{name: "too many categories", categories: make([]string, 101), message: "监控分类不能超过 100 个"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := putChannelGroupMonitorCategories(t, tc.categories, []model.ChannelGroupMonitorGroup{
				{GroupName: "default", ProbeModel: "gpt-4.1", Category: "通用模型"},
			}, 0)
			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), tc.message)
			var count int64
			require.NoError(t, db.Model(&model.ChannelGroupMonitorConfig{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}
