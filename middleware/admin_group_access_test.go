package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAdminGroupAccessTest(t *testing.T) {
	t.Helper()
	setupDashboardAuthMiddlewareTest(t)
	previousLogDB := model.LOG_DB
	previousLogType := common.LogDatabaseType()
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	require.NoError(t, i18n.Init())
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))

	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"internal":2}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"missing":"Stale selectable group"}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		model.LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogType)
	})
}

func createGroupAccessToken(t *testing.T, role int, key, group string) {
	t.Helper()
	user := &model.User{
		Username:    "u" + key,
		Password:    "password-placeholder",
		Role:        role,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     "a" + key,
	}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         user.Id,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		Name:           key,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Group:          group,
	}).Error)
}

func runGroupAccessTokenRequest(t *testing.T, key string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.GET("/relay", TokenAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"role":        c.GetInt("role"),
			"using_group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		})
	})
	request := httptest.NewRequest(http.MethodGet, "/relay", nil)
	request.Header.Set("Authorization", "Bearer sk-"+key)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestTokenAuthUsesRoleAwareGroupAccess(t *testing.T) {
	setupAdminGroupAccessTest(t)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		role       int
		group      string
		wantStatus int
		wantBody   string
	}{
		{name: "ordinary user remains restricted", role: common.RoleCommonUser, group: "internal", wantStatus: http.StatusForbidden, wantBody: "无权访问 internal 分组"},
		{name: "administrator can use configured group", role: common.RoleAdminUser, group: "internal", wantStatus: http.StatusOK},
		{name: "root can use configured group", role: common.RoleRootUser, group: "internal", wantStatus: http.StatusOK},
		{name: "administrator cannot use unconfigured group", role: common.RoleAdminUser, group: "missing", wantStatus: http.StatusForbidden, wantBody: "分组 missing 已被弃用"},
		{name: "administrator auto access keeps existing opt in", role: common.RoleAdminUser, group: "auto", wantStatus: http.StatusForbidden, wantBody: "无权访问 auto 分组"},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := fmt.Sprintf("groupaccess%d", index)
			createGroupAccessToken(t, test.role, key, test.group)
			response := runGroupAccessTokenRequest(t, key)

			assert.Equal(t, test.wantStatus, response.Code)
			if test.wantBody != "" {
				assert.Contains(t, response.Body.String(), test.wantBody)
			}
			if test.wantStatus != http.StatusOK {
				return
			}
			var body struct {
				Role       int    `json:"role"`
				UsingGroup string `json:"using_group"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, test.role, body.Role)
			assert.Equal(t, test.group, body.UsingGroup)
		})
	}
}

func runPlaygroundGroupRequest(t *testing.T, role int, group string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.POST(
		"/pg/chat/completions",
		func(c *gin.Context) {
			c.Set("role", role)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			c.Next()
		},
		Distribute(),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/pg/chat/completions",
		bytes.NewBufferString(`{"model":"group-access-model","group":"`+group+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestPlaygroundGroupOverrideUsesRoleAwareGroupAccess(t *testing.T) {
	setupAdminGroupAccessTest(t)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		role       int
		group      string
		wantStatus int
	}{
		{name: "ordinary user can use own non-selectable group", role: common.RoleCommonUser, group: "default", wantStatus: http.StatusServiceUnavailable},
		{name: "ordinary user remains restricted", role: common.RoleCommonUser, group: "internal", wantStatus: http.StatusForbidden},
		{name: "administrator reaches channel selection", role: common.RoleAdminUser, group: "internal", wantStatus: http.StatusServiceUnavailable},
		{name: "root reaches channel selection", role: common.RoleRootUser, group: "internal", wantStatus: http.StatusServiceUnavailable},
		{name: "administrator cannot select unconfigured group", role: common.RoleAdminUser, group: "unknown", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := runPlaygroundGroupRequest(t, test.role, test.group)
			assert.Equal(t, test.wantStatus, response.Code)
		})
	}
}
