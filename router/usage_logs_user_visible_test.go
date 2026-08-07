package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserVisibleLogRoutesRejectCommonUsers(t *testing.T) {
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalSessionSecret := common.SessionSecret
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.SessionSecret = originalSessionSecret
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "router.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB = db
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.SessionSecret = "user-visible-log-route-test-secret"

	user := &model.User{
		Id:          88001,
		Username:    "user-visible-route-common-user",
		Password:    "unused",
		AffCode:     "user-visible-route-common-user",
		Group:       "default",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	session, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "test-agent")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	paths := []string{
		"/api/log/user-visible",
		"/api/log/user-visible/stat",
		"/api/mj/user-visible",
		"/api/task/user-visible",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer "+session.AccessToken)

			engine.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusForbidden, recorder.Code)
		})
	}
}
