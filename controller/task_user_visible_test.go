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

func TestGetAllUserVisibleTaskHidesChannelAndIncludesUser(t *testing.T) {
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

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/task/user-visible?p=1&page_size=20", nil)

	GetAllUserVisibleTask(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []*dto.TaskDto `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	assert.Zero(t, response.Data.Items[0].ChannelId)
	assert.Equal(t, user.Username, response.Data.Items[0].Username)
}
