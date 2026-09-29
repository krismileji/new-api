package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageProtocolPricingGuardBeforePluginSelection(t *testing.T) {
	require.NoError(t, i18n.Init())
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMemory := common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.MemoryCacheEnabled = oldMemory
	})
	setupRelayRouterTestDB(t)
	common.MemoryCacheEnabled = false
	db := model.DB
	oldLimit := setting.ModelRequestRateLimitEnabled
	oldRegistry := pluginruntime.DefaultRegistry
	oldRatios := ratio_setting.ImageRatio2JSONString()
	setting.ModelRequestRateLimitEnabled = false
	pluginruntime.DefaultRegistry = pluginruntime.NewRegistry()
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = oldLimit
		pluginruntime.DefaultRegistry = oldRegistry
		require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(oldRatios))
	})
	require.NoError(t, db.Create(&model.User{
		Id: 190012, Username: "image-guard-user", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", Quota: 100,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		UserId: 190012, Key: "imageguardtoken", Status: common.TokenStatusEnabled,
		ExpiredTime: -1, UnlimitedQuota: true,
	}).Error)
	_, err := pluginruntime.DefaultRegistry.Register(`
export const meta={apiVersion:1,key:"image-guard",name:"Image guard",version:"1.0.0",author:{name:"Test"},models:["guard-image"],fetchMode:"per_task",protocols:["openai_image"]};
export function buildSubmitRequest(){throw new Error("must not submit");}
export function parseSubmitResponse(){return {taskId:"unused"};}
export function buildQueryRequest(){return {url:"https://example.invalid/task"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export const protocols={openai_image:{
 decodeRequest:function(){throw new Error("image guard reached plugin parser");},
 render:function(){return {data:[]};}
}};
`, pluginruntime.Options{})
	require.NoError(t, err)
	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup())
	SetTaskPluginProtocolRouter(engine)
	for _, tc := range []struct {
		name, path, body, ratios string
		unauthenticated          bool
		wantStatus               int
		wantMessage              string
	}{
		{"missing plugin ratio", "/v1/images/generations", `{"model":"guard-image"}`, `{}`, false, 200, "image generation is currently not supported"},
		{"missing unclaimed ratio", "/v1/images/generations", `{"model":"other-image"}`, `{}`, false, 200, "image generation is currently not supported"},
		{"canonical name cannot bypass original model", "/v1/images/generations", `{"model":"GUARD-IMAGE"}`, `{"guard-image":1}`, false, 200, "image generation is currently not supported"},
		{"configured ratio reaches plugin", "/v1/images/generations", `{"model":"guard-image"}`, `{"guard-image":2}`, false, 400, "image guard reached plugin parser"},
		{"explicit zero ratio reaches plugin", "/v1/images/generations", `{"model":"guard-image"}`, `{"guard-image":0}`, false, 400, "image guard reached plugin parser"},
		{"image edit is unaffected", "/v1/images/edits", `{"model":"guard-image"}`, `{}`, false, 400, "image guard reached plugin parser"},
		{"malformed request keeps protocol validation", "/v1/images/generations", `{"model":`, `{}`, false, 400, "Invalid task protocol request"},
		{"authentication still runs first", "/v1/images/generations", `{"model":"guard-image"}`, `{}`, true, 401, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(tc.ratios))
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			if !tc.unauthenticated {
				request.Header.Set("Authorization", "Bearer sk-imageguardtoken")
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
			if tc.wantMessage != "" {
				var response struct {
					Error struct{ Message, Code string } `json:"error"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				assert.Contains(t, response.Error.Message, tc.wantMessage)
				if tc.wantStatus == http.StatusOK {
					assert.Equal(t, "invalid_request", response.Error.Code)
				}
			}
		})
	}
}
