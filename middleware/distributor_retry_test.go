package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDistributorRetryTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalDatabaseType := common.MainDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "distributor-retry.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.Channel{}, &model.Ability{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	service.ResetChannelDailyCostSnapshotCache()
	t.Cleanup(func() {
		model.DB = originalDB
		common.SetMainDatabaseType(originalDatabaseType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		service.ResetChannelDailyCostSnapshotCache()
		if originalMemoryCacheEnabled && originalDB != nil &&
			originalDB.Migrator().HasTable(&model.Channel{}) && originalDB.Migrator().HasTable(&model.Ability{}) {
			model.InitChannelCache()
		}
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
}

func TestSetupContextForInitialChannelSkipsChannelWithoutEnabledKeys(t *testing.T) {
	setupDistributorRetryTestDB(t)
	priority := int64(100)
	weight := uint(10)
	badBaseURL := "https://bad.example"
	goodBaseURL := "https://good.example"
	badChannel := &model.Channel{
		Id: 51, Type: constant.ChannelTypeOpenAI, Name: "no-enabled-keys", Key: "disabled-key",
		Status: common.ChannelStatusEnabled, BaseURL: &badBaseURL, Group: "vip", Models: "model-a",
		Priority: &priority, Weight: &weight,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	goodChannel := &model.Channel{
		Id: 52, Type: constant.ChannelTypeOpenAI, Name: "usable", Key: "good-key",
		Status: common.ChannelStatusEnabled, BaseURL: &goodBaseURL, Group: "vip", Models: "model-a",
		Priority: &priority, Weight: &weight,
	}
	require.NoError(t, model.DB.Create([]*model.Channel{badChannel, goodChannel}).Error)
	require.NoError(t, model.DB.Create([]*model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 51, Enabled: true, Priority: &priority, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 52, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "vip")

	selected, setupErr := setupContextForInitialChannel(c, badChannel, "model-a", true)

	require.Nil(t, setupErr)
	require.NotNil(t, selected)
	assert.Equal(t, 52, selected.Id)
	assert.Equal(t, 52, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	assert.Equal(t, goodBaseURL, common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
	assert.Equal(t, "good-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
}

func TestSetupContextForInitialAutoChannelFallsBackAcrossGroups(t *testing.T) {
	setupDistributorRetryTestDB(t)
	originalRetryTimes := common.RetryTimes
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalMaxTokenAutoGroups := setting.GetMaxTokenAutoGroups()
	t.Cleanup(func() {
		common.RetryTimes = originalRetryTimes
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMaxTokenAutoGroups)))
	})
	common.RetryTimes = 0
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))

	priority := int64(100)
	weight := uint(10)
	badBaseURL := "https://bad.example"
	goodBaseURL := "https://good.example"
	badChannel := &model.Channel{
		Id: 53, Type: constant.ChannelTypeOpenAI, Name: "no-enabled-keys", Key: "disabled-key",
		Status: common.ChannelStatusEnabled, BaseURL: &badBaseURL, Group: "vip", Models: "model-a",
		Priority: &priority, Weight: &weight,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	goodChannel := &model.Channel{
		Id: 54, Type: constant.ChannelTypeOpenAI, Name: "usable", Key: "good-key",
		Status: common.ChannelStatusEnabled, BaseURL: &goodBaseURL, Group: "default", Models: "model-a",
		Priority: &priority, Weight: &weight,
	}
	require.NoError(t, model.DB.Create([]*model.Channel{badChannel, goodChannel}).Error)
	require.NoError(t, model.DB.Create([]*model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 53, Enabled: true, Priority: &priority, Weight: weight},
		{Group: "default", Model: "model-a", ChannelId: 54, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, false)
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "vip")
	common.SetContextKey(c, constant.ContextKeyAutoGroupIndex, 0)

	selected, setupErr := setupContextForInitialChannel(c, badChannel, "model-a", true)

	require.Nil(t, setupErr)
	require.NotNil(t, selected)
	assert.Equal(t, 54, selected.Id)
	assert.Equal(t, "default", common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
	assert.Equal(t, goodBaseURL, common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
}

func TestSetupContextForSelectedChannelDoesNotPartiallySwitchOnKeyFailure(t *testing.T) {
	setupDistributorRetryTestDB(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 60)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://current.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "current-key")
	common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)

	failedChannel := &model.Channel{
		Id: 61, Type: constant.ChannelTypeOpenAI, Name: "failed", Key: "disabled-key",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}

	setupErr := SetupContextForSelectedChannel(c, failedChannel, "model-a")

	require.NotNil(t, setupErr)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, setupErr.GetErrorCode())
	assert.Equal(t, 60, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	assert.Equal(t, "https://current.example", common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
	assert.Equal(t, "current-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey))
}

func TestSetupContextForSelectedChannelClearsPreviousOptionalValues(t *testing.T) {
	setupDistributorRetryTestDB(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelOrganization, "old-org")
	common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 8)
	c.Set("api_version", "old-version")
	c.Set("region", "old-region")
	c.Set("plugin", "old-plugin")
	c.Set("bot_id", "old-bot")

	channel := &model.Channel{
		Id:   42,
		Type: constant.ChannelTypeOpenAI,
		Name: "retry-target",
		Key:  "new-key",
	}
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "model-a"))

	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyChannelOrganization))
	assert.Zero(t, common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex))
	assert.Empty(t, c.GetString("api_version"))
	assert.Empty(t, c.GetString("region"))
	assert.Empty(t, c.GetString("plugin"))
	assert.Empty(t, c.GetString("bot_id"))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey))
	assert.Equal(t, "new-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
}

func TestSetupContextForSelectedChannelSetsOnlyCurrentTypeMetadata(t *testing.T) {
	setupDistributorRetryTestDB(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("api_version", "old-version")
	c.Set("plugin", "old-plugin")

	channel := &model.Channel{
		Id:    43,
		Type:  constant.ChannelTypeCoze,
		Name:  "coze-target",
		Key:   "new-key",
		Other: "new-bot",
	}
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "model-a"))

	assert.Equal(t, "new-bot", c.GetString("bot_id"))
	assert.Empty(t, c.GetString("api_version"))
	assert.Empty(t, c.GetString("plugin"))
}
