package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetAllUserVisibleTaskFiltersAndIncludesChannelAndUser(t *testing.T) {
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "task-user-visible.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Task{}))
	model.DB = db
	common.RedisEnabled = false

	user := &model.User{
		Id:       88002,
		Username: "task-user-visible-user",
		Group:    "default",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "task-user-visible-test",
		Platform:  constant.TaskPlatform("test"),
		UserId:    user.Id,
		ChannelId: 321,
		Status:    model.TaskStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "task-user-visible-other-channel",
		Platform:  constant.TaskPlatform("test"),
		UserId:    user.Id,
		ChannelId: 654,
		Status:    model.TaskStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "task-user-visible-in-progress",
		Platform:  constant.TaskPlatform("test"),
		UserId:    user.Id,
		ChannelId: 321,
		Status:    model.TaskStatusInProgress,
	}).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:     "task-user-visible-failure",
		Platform:   constant.TaskPlatform("test"),
		UserId:     user.Id,
		ChannelId:  321,
		Status:     model.TaskStatusFailure,
		FailReason: "upstream failed",
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/task/user-visible?p=1&page_size=20&channel_id=321", nil)

	GetAllUserVisibleTask(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Total int            `json:"total"`
			Items []*dto.TaskDto `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Items, 2)
	for _, item := range response.Data.Items {
		assert.Equal(t, 321, item.ChannelId)
		assert.Equal(t, user.Username, item.Username)
		assert.NotEqual(t, string(model.TaskStatusInProgress), item.Status)
	}

	selfRecorder := httptest.NewRecorder()
	selfContext, _ := gin.CreateTestContext(selfRecorder)
	selfContext.Request = httptest.NewRequest(http.MethodGet, "/api/task/self?p=1&page_size=20", nil)
	selfContext.Set("id", user.Id)

	GetUserTask(selfContext)

	assert.Equal(t, http.StatusOK, selfRecorder.Code)
	var selfResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Total int            `json:"total"`
			Items []*dto.TaskDto `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(selfRecorder.Body.Bytes(), &selfResponse))
	require.True(t, selfResponse.Success)
	assert.Equal(t, 3, selfResponse.Data.Total)
	require.Len(t, selfResponse.Data.Items, 3)
	for _, item := range selfResponse.Data.Items {
		assert.Zero(t, item.ChannelId)
		assert.NotEqual(t, string(model.TaskStatusInProgress), item.Status)
	}
}
