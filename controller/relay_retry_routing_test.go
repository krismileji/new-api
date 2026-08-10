package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureRelayRetryAutoGroupsTest(t *testing.T) {
	t.Helper()
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
}

func TestRelayRetryRoutingExcludesFailedChannelsImmediately(t *testing.T) {
	routing := newRelayRetryRouting()

	routing.exclude(26)
	routing.exclude(7)

	options, ok := routing.selectionOptions()
	require.True(t, ok)
	assert.Equal(t, []int{26, 7}, options.ExcludedChannelIds)
	assert.False(t, routing.candidatesExhausted())
}

func TestRelayRetryRoutingReloadsCompletePinnedChannelForRetry(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	setting := `{"proxy":"http://proxy.example"}`
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 26, Type: constant.ChannelTypeOpenAI, Name: "same-channel", Key: "secret-key",
		Status: common.ChannelStatusEnabled, BaseURL: &baseURL, Setting: &setting,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 26, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	routing := newRelayRetryRouting()
	initialChannel := &model.Channel{Id: 26, Type: constant.ChannelTypeOpenAI, Name: "same-channel"}

	routing.retrySameChannel(initialChannel, "vip")
	options, hasExcludedChannels := routing.selectionOptions()
	assert.False(t, hasExcludedChannels)
	assert.Empty(t, options.ExcludedChannelIds)

	selected, group, err := routing.selectChannel(&service.RetryParam{
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.NotSame(t, initialChannel, selected)
	assert.Equal(t, baseURL, selected.GetBaseURL())
	assert.Equal(t, "secret-key", selected.Key)
	assert.Equal(t, "http://proxy.example", selected.GetSetting().Proxy)
	assert.Equal(t, "vip", group)
	assert.Zero(t, routing.sameChannelID)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, selected, "model-a"))
	info := &relaycommon.RelayInfo{}
	info.InitChannelMeta(ctx)
	assert.Equal(t, baseURL, info.ChannelBaseUrl)
	assert.Equal(t, "secret-key", info.ApiKey)
	assert.Equal(t, "http://proxy.example", info.ChannelSetting.Proxy)
	assert.Equal(t, baseURL+ctx.Request.URL.Path,
		relaycommon.GetFullRequestURL(info.ChannelBaseUrl, ctx.Request.URL.Path, info.ChannelType))

	routing.retrySameChannel(initialChannel, "vip")
	routing.exclude(initialChannel.Id)
	assert.Zero(t, routing.sameChannelID)
	options, hasExcludedChannels = routing.selectionOptions()
	require.True(t, hasExcludedChannels)
	assert.Equal(t, []int{initialChannel.Id}, options.ExcludedChannelIds)
}

func TestRelayRetryRoutingRejectsNonparticipatingSameChannelWhenSmartScheduleEnabled(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 261, Name: "未参与原渠道", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 262, Name: "参与候选", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 261, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 262, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 262, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled, hadEnabled := common.OptionMap["ChannelMonitorSmartScheduleEnabled"]
	originalPolicies, hadPolicies := common.OptionMap["ChannelMonitorSmartScheduleGroupPolicies"]
	common.OptionMap["ChannelMonitorSmartScheduleEnabled"] = "true"
	common.OptionMap["ChannelMonitorSmartScheduleGroupPolicies"] = `[{"group":"vip","models":["model-a"]}]`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if hadEnabled {
			common.OptionMap["ChannelMonitorSmartScheduleEnabled"] = originalEnabled
		} else {
			delete(common.OptionMap, "ChannelMonitorSmartScheduleEnabled")
		}
		if hadPolicies {
			common.OptionMap["ChannelMonitorSmartScheduleGroupPolicies"] = originalPolicies
		} else {
			delete(common.OptionMap, "ChannelMonitorSmartScheduleGroupPolicies")
		}
		common.OptionMapRWMutex.Unlock()
	})
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	routing := newRelayRetryRouting()
	routing.retrySameChannel(&model.Channel{Id: 261}, "vip")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selected, group, err := routing.selectChannel(&service.RetryParam{
		Ctx: ctx, TokenGroup: "vip", ModelName: "model-a", RequestPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 262, selected.Id)
	assert.Equal(t, "vip", group)
}

func TestRelayRetryRoutingRetries502WithReloadedBaseURL(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requestCount.Add(1) == 1 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 66, Type: constant.ChannelTypeOpenAI, Name: "retry-after-502", Key: "secret-key",
		Status: common.ChannelStatusEnabled, BaseURL: &baseURL, Group: "vip", Models: "model-a",
		Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 66, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	fullChannel, err := model.CacheGetChannel(66)
	require.NoError(t, err)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, fullChannel, "model-a"))
	info := &relaycommon.RelayInfo{OriginModelName: "model-a"}
	info.InitChannelMeta(ctx)

	firstURL := relaycommon.GetFullRequestURL(info.ChannelBaseUrl, ctx.Request.URL.Path, info.ChannelType)
	firstRequest, err := http.NewRequest(http.MethodPost, firstURL, nil)
	require.NoError(t, err)
	firstResponse, err := upstream.Client().Do(firstRequest)
	require.NoError(t, err)
	require.NoError(t, firstResponse.Body.Close())
	assert.Equal(t, http.StatusBadGateway, firstResponse.StatusCode)

	routing := newRelayRetryRouting()
	routing.retrySameChannel(&model.Channel{Id: 66, Type: constant.ChannelTypeOpenAI, Name: "sparse-first-attempt"}, "vip")
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
		IsRetry:     true,
	}
	reloaded, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, "vip", group)
	assert.Equal(t, upstream.URL, reloaded.GetBaseURL())
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, reloaded, "model-a"))
	info.InitChannelMeta(ctx)

	retryURL := relaycommon.GetFullRequestURL(info.ChannelBaseUrl, ctx.Request.URL.Path, info.ChannelType)
	retryRequest, err := http.NewRequest(http.MethodPost, retryURL, nil)
	require.NoError(t, err)
	retryResponse, err := upstream.Client().Do(retryRequest)
	require.NoError(t, err)
	require.NoError(t, retryResponse.Body.Close())
	assert.Equal(t, http.StatusOK, retryResponse.StatusCode)
	assert.EqualValues(t, 2, requestCount.Load())
}

func TestRelayRetryRoutingFallsBackWhenPinnedChannelLosesModelAbility(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priorityHigh := int64(200)
	priorityLow := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 63, Name: "removed-ability", Key: "key-1", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityHigh, Weight: &weight},
		{Id: 64, Name: "fallback", Key: "key-2", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityLow, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 63, Enabled: true, Priority: &priorityHigh, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 64, Enabled: true, Priority: &priorityLow, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
		IsRetry:     true,
	}
	routing := newRelayRetryRouting()
	routing.retrySameChannel(&model.Channel{Id: 63}, "vip")
	require.NoError(t, db.Model(&model.Ability{}).
		Where(&model.Ability{ChannelId: 63, Group: "vip", Model: "model-a"}).
		Update("enabled", false).Error)
	model.InitChannelCache()

	selected, group, err := routing.selectChannel(retryParam)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 64, selected.Id)
	assert.Equal(t, "vip", group)

	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "vip")
	retryParam.TokenGroup = "auto"
	routing = newRelayRetryRouting()
	routing.retrySameChannel(&model.Channel{Id: 63}, "vip")

	selected, group, err = routing.selectChannel(retryParam)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 64, selected.Id)
	assert.Equal(t, "vip", group)
}

func TestRelayRetryRoutingFallsBackWhenPinnedChannelIsDisabled(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priorityHigh := int64(200)
	priorityLow := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 55, Name: "disabled-pinned", Key: "key-1", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityHigh, Weight: &weight,
		},
		{
			Id: 56, Name: "fallback", Key: "key-2", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityLow, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 55, Enabled: true, Priority: &priorityHigh, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 56, Enabled: true, Priority: &priorityLow, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
		IsRetry:     true,
	}
	routing := newRelayRetryRouting()
	routing.retrySameChannel(&model.Channel{Id: 55}, "vip")
	model.CacheUpdateChannelStatus(55, common.ChannelStatusAutoDisabled)

	selected, group, err := routing.selectChannel(retryParam)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 56, selected.Id)
	assert.Equal(t, "vip", group)
}

func TestRelayRetryRoutingStopsAfterAllAutoGroupsAreExhausted(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	configureRelayRetryAutoGroupsTest(t)

	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 57, Name: "vip-no-enabled-keys", Key: "disabled-key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:         true,
				MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			},
		},
		{Id: 58, Name: "default", Key: "key-2", Group: "default", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 57, Enabled: true, Priority: &priority, Weight: weight},
		{Group: "default", Model: "model-a", ChannelId: 58, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-a",
		UserGroup:       "default",
		UsingGroup:      "vip",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	selected, apiErr := getChannel(ctx, info, retryParam, routing)
	require.Nil(t, apiErr)
	require.NotNil(t, selected)
	assert.Equal(t, 58, selected.Id)
	assert.False(t, retryParam.IsRetry)

	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	retryParam.IsRetry = true
	routing.exclude(58)

	selected, _, err := routing.selectChannel(retryParam)

	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.True(t, routing.candidatesExhausted())
}

func TestGetChannelSkipsRetryCandidateWithoutEnabledKeys(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	badBaseURL := "https://bad.example"
	goodBaseURL := "https://good.example"
	badPriority := int64(200)
	goodPriority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 53, Type: constant.ChannelTypeOpenAI, Name: "no-enabled-keys", Key: "disabled-key",
			Status: common.ChannelStatusEnabled, BaseURL: &badBaseURL, Group: "vip", Models: "model-a",
			Priority: &badPriority, Weight: &weight,
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:         true,
				MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			},
		},
		{
			Id: 54, Type: constant.ChannelTypeOpenAI, Name: "usable", Key: "good-key",
			Status: common.ChannelStatusEnabled, BaseURL: &goodBaseURL, Group: "vip", Models: "model-a",
			Priority: &goodPriority, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 53, Enabled: true, Priority: &badPriority, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 54, Enabled: true, Priority: &goodPriority, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "vip")
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(1),
		IsRetry:     true,
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-a",
	}

	selected, apiErr := getChannel(ctx, info, retryParam, newRelayRetryRouting())

	require.Nil(t, apiErr)
	require.NotNil(t, selected)
	assert.Equal(t, 54, selected.Id)
	assert.Equal(t, goodBaseURL, common.GetContextKeyString(ctx, constant.ContextKeyChannelBaseUrl))
	assert.Equal(t, "good-key", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
}

func TestGetChannelReturnsSetupErrorWithoutFailedChannel(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 61, Type: constant.ChannelTypeOpenAI, Name: "no-enabled-keys", Key: "disabled-key",
		Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a",
		Priority: &priority, Weight: &weight,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 61, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(1),
		IsRetry:     true,
	}
	info := &relaycommon.RelayInfo{OriginModelName: "model-a"}

	selected, apiErr := getChannel(ctx, info, retryParam, newRelayRetryRouting())

	assert.Nil(t, selected)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, apiErr.GetErrorCode())
}

func TestRelayRetryRoutingRestartsRoundsUntilRetryBudgetIsUsed(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	channelIDs := []int{26, 7, 8, 9, 10}
	priorities := []int64{500, 400, 300, 200, 100}
	weight := uint(10)
	channels := make([]model.Channel, 0, len(channelIDs))
	abilities := make([]model.Ability, 0, len(channelIDs))
	for i, channelID := range channelIDs {
		channels = append(channels, model.Channel{
			Id: channelID, Name: "retry-channel", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: common.GetPointer(priorities[i]), Weight: &weight,
		})
		abilities = append(abilities, model.Ability{
			Group: "vip", Model: "model-a", ChannelId: channelID, Enabled: true,
			Priority: common.GetPointer(priorities[i]), Weight: weight,
		})
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&abilities).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	currentChannelID := channelIDs[0]
	attemptedChannelIDs := []int{currentChannelID}
	for retry := 1; retry <= 10; retry++ {
		routing.exclude(currentChannelID)
		retryParam.SetRetry(retry)
		channel, group, err := routing.selectChannel(retryParam)
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "vip", group)
		assert.False(t, routing.candidatesExhausted())
		currentChannelID = channel.Id
		attemptedChannelIDs = append(attemptedChannelIDs, currentChannelID)
	}

	assert.Equal(t, []int{26, 7, 8, 9, 10, 26, 7, 8, 9, 10, 26}, attemptedChannelIDs)
}

func TestRelayRetryRoutingTriesSamePriorityChannelsWithoutReplacement(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	channelIDs := []int{31, 32, 33}
	priority := int64(100)
	weights := []uint{1, 3, 6}
	channels := make([]model.Channel, 0, len(channelIDs))
	abilities := make([]model.Ability, 0, len(channelIDs))
	for index, channelID := range channelIDs {
		weight := weights[index]
		channels = append(channels, model.Channel{
			Id: channelID, Name: "same-priority", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		})
		abilities = append(abilities, model.Ability{
			Group: "vip", Model: "model-a", ChannelId: channelID, Enabled: true,
			Priority: &priority, Weight: weight,
		})
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&abilities).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	attempted := make(map[int]struct{}, len(channelIDs))
	for range channelIDs {
		selected, group, err := routing.selectChannel(retryParam)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, "vip", group)
		_, repeated := attempted[selected.Id]
		assert.False(t, repeated)
		attempted[selected.Id] = struct{}{}
		routing.exclude(selected.Id)
		retryParam.IncreaseRetry()
	}
	assert.Len(t, attempted, len(channelIDs))

	selected, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "vip", group)
	assert.Contains(t, attempted, selected.Id)
}

func TestRelayRetryRoutingRepeatsTheOnlyAvailableChannel(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 27, Name: "only-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 27, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	current := &model.Channel{Id: 27}
	attempted := []int{current.Id}
	for retry := 1; retry <= 3; retry++ {
		routing.exclude(current.Id)
		retryParam.SetRetry(retry)
		selected, group, err := routing.selectChannel(retryParam)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, "vip", group)
		assert.Equal(t, 27, selected.Id)
		attempted = append(attempted, selected.Id)
		current = selected
	}
	assert.Equal(t, []int{27, 27, 27, 27}, attempted)
}

func TestRelayRetryRoutingRepeatsCooledOnlyChannelAfterRoundExhaustion(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 69, Name: "only-cooled-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 69, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	service.StartChannelRateLimitCooldown(69, "model-a", 60)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()

	selected, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 69, selected.Id)
	assert.Equal(t, "vip", group)

	routing.exclude(selected.Id)
	retryParam.IncreaseRetry()
	selected, group, err = routing.selectChannel(retryParam)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 69, selected.Id)
	assert.Equal(t, "vip", group)
	assert.False(t, routing.candidatesExhausted())
}

func TestRelayRetryRoutingCurrentRoundDoesNotRestartOnlyChannel(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 67, Name: "only-saturated-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 67, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	routing.exclude(67)

	selected, group, err := routing.selectChannelCurrentRound(retryParam)

	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", group)
	assert.True(t, routing.candidatesExhausted())
	assert.Zero(t, retryParam.GetRetry())
}

func TestRelayRetryRoutingStopsWhenAllCandidatesBecomeUnavailable(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 28, Name: "removed-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 28, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(1),
	}
	routing := newRelayRetryRouting()
	routing.exclude(28)
	model.CacheUpdateChannelStatus(28, common.ChannelStatusAutoDisabled)

	selected, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", group)
	assert.True(t, routing.candidatesExhausted())
}

func TestRelayRetryRoutingReleasesLimitedSpecialRouteAfterPreferredCandidates(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priorityExploration := int64(500)
	priorityStable := int64(400)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 29, Name: "exploration", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityExploration, Weight: &weight,
		},
		{
			Id: 30, Name: "stable", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityStable, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{
			Group: "vip", Model: "model-a", ChannelId: 29, Enabled: true,
			Priority: &priorityExploration, Weight: weight,
		},
		{
			Group: "vip", Model: "model-a", ChannelId: 30, Enabled: true,
			Priority: &priorityStable, Weight: weight,
		},
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 29, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		TemporaryTrafficKind:       model.ChannelSmartScheduleTemporaryTrafficExploration,
		ExplorationMaxPromptTokens: 100,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:              ctx,
		TokenGroup:       "vip",
		ModelName:        "model-a",
		RequestPath:      ctx.Request.URL.Path,
		Retry:            common.GetPointer(0),
		SelectionOptions: model.ChannelSelectionOptions{EstimatedPromptTokens: 101},
	}
	routing := newRelayRetryRouting()
	selected, _, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 30, selected.Id)

	routing.exclude(selected.Id)
	retryParam.IncreaseRetry()
	selected, _, err = routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 29, selected.Id)
}

func TestRelayRetryRoutingAutoFallbackRevisitsGroupsForLimitedSpecialRoute(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	configureRelayRetryAutoGroupsTest(t)
	priorityExploration := int64(200)
	priorityStable := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 59, Name: "exploration", Key: "key-1", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityExploration, Weight: &weight,
		},
		{
			Id: 60, Name: "failed-stable", Key: "key-2", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityStable, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 59, Enabled: true, Priority: &priorityExploration, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 60, Enabled: true, Priority: &priorityStable, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 59, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		TemporaryTrafficKind:       model.ChannelSmartScheduleTemporaryTrafficExploration,
		ExplorationMaxPromptTokens: 100,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "vip")
	common.SetContextKey(ctx, constant.ContextKeyAutoGroupIndex, 0)
	retryParam := &service.RetryParam{
		Ctx:              ctx,
		TokenGroup:       "auto",
		ModelName:        "model-a",
		RequestPath:      ctx.Request.URL.Path,
		Retry:            common.GetPointer(0),
		IsRetry:          true,
		SelectionOptions: model.ChannelSelectionOptions{EstimatedPromptTokens: 101},
	}
	routing := newRelayRetryRouting()
	routing.exclude(60)

	selected, group, err := routing.selectChannel(retryParam)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 59, selected.Id)
	assert.Equal(t, "vip", group)
	assert.False(t, routing.candidatesExhausted())
}

func TestRelayRetryRoutingDoesNotRestartFailedAutoGroupWithoutCrossGroupPermission(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	configureRelayRetryAutoGroupsTest(t)
	common.RetryTimes = 1
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 68, Name: "failed-auto-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 68, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	retry := 0
	param := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       &retry,
	}
	routing := newRelayRetryRouting()
	selected, group, err := routing.selectChannel(param)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 68, selected.Id)
	assert.Equal(t, "vip", group)

	param.IsRetry = true
	param.IncreaseRetry()
	routing.exclude(selected.Id)

	selected, group, err = routing.selectChannel(param)
	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", group)
	assert.True(t, routing.candidatesExhausted())
}
