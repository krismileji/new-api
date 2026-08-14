package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetAllUserVisibleMidjourneyFiltersChannel(t *testing.T) {
	originalDB := model.DB
	originalForwardEnabled := setting.MjForwardUrlEnabled
	t.Cleanup(func() {
		model.DB = originalDB
		setting.MjForwardUrlEnabled = originalForwardEnabled
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "midjourney-user-visible.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	model.DB = db
	setting.MjForwardUrlEnabled = false

	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     1,
		MjId:       "mj-user-visible-channel",
		ChannelId:  321,
		SubmitTime: 100,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     2,
		MjId:       "mj-user-visible-other-channel",
		ChannelId:  654,
		SubmitTime: 101,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/mj/user-visible?p=1&page_size=20&channel_id=321", nil)

	GetAllUserVisibleMidjourney(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []*model.Midjourney `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 321, response.Data.Items[0].ChannelId)
}
