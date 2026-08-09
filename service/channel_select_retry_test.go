package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheGetRandomSatisfiedChannelRejectsMissingRetryState(t *testing.T) {
	selected, group, err := CacheGetRandomSatisfiedChannel(nil)
	require.Error(t, err)
	assert.Nil(t, selected)
	assert.Empty(t, group)

	selected, group, err = CacheGetRandomSatisfiedChannel(&RetryParam{TokenGroup: "default"})
	require.Error(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "default", group)
}

func TestAutoGroupFailureExclusionsHonorCrossGroupRetrySetting(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-group-retry-boundary-model"
	createChannelSelectAutoGroupsChannel(t, db, 2201, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2202, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "vip")
	retry := 1
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
		IsRetry:     true,
	}
	options := model.ChannelSelectionOptions{ExcludedChannelIds: []int{2201}}

	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param, options)
	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", selectedGroup)
	assert.Zero(t, common.GetContextKeyInt(ctx, constant.ContextKeyAutoGroupIndex))

	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	selected, selectedGroup, err = CacheGetRandomSatisfiedChannel(param, options)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2202, selected.Id)
	assert.Equal(t, "default", selectedGroup)
}

func TestAutoGroupInitialSelectionExclusionsStillAllowFallback(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-group-initial-fallback-model"
	createChannelSelectAutoGroupsChannel(t, db, 2211, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2212, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       common.GetPointer(0),
	}
	options := model.ChannelSelectionOptions{ExcludedChannelIds: []int{2211}}

	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param, options)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2212, selected.Id)
	assert.Equal(t, "default", selectedGroup)
}

func TestAutoGroupRetryRemainsInSelectedGroupWithoutExclusionList(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-group-retry-lock-model"
	createChannelSelectAutoGroupsChannel(t, db, 2221, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2222, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "vip")
	model.CacheUpdateChannelStatus(2221, common.ChannelStatusAutoDisabled)
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       common.GetPointer(0),
		IsRetry:     true,
	}

	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)

	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", selectedGroup)
}

func TestAutoGroupRetryStopsWhenSelectedGroupIsNoLongerAllowed(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-group-retry-removed-group-model"
	createChannelSelectAutoGroupsChannel(t, db, 2223, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "vip")
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       common.GetPointer(1),
		IsRetry:     true,
	}

	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)

	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", selectedGroup)
	assert.Equal(t, 1, param.GetRetry())
}

func TestAutoGroupRetryAdvancesAfterLastPriorityWithoutResettingBudget(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	common.RetryTimes = 2
	const modelName = "auto-group-last-priority-model"
	createChannelSelectAutoGroupsChannel(t, db, 2231, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2232, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       common.GetPointer(0),
	}

	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2231, selected.Id)
	assert.Equal(t, "vip", selectedGroup)

	param.IsRetry = true
	param.IncreaseRetry()
	selected, selectedGroup, err = CacheGetRandomSatisfiedChannel(
		param,
		model.ChannelSelectionOptions{ExcludedChannelIds: []int{2231}},
	)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2232, selected.Id)
	assert.Equal(t, "default", selectedGroup)
	assert.Equal(t, 1, param.GetRetry())
}

func TestChannelRateLimitCooldownUsesOnlyChannelAsFallback(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "rate-limit-only-channel-model"
	createChannelSelectAutoGroupsChannel(t, db, 2241, "vip", modelName)
	model.InitChannelCache()
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)
	StartChannelRateLimitCooldown(2241, modelName, 60)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       common.GetPointer(0),
	})

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2241, selected.Id)
	assert.Equal(t, "vip", selectedGroup)
}

func TestChannelRateLimitCooldownUsesCooledChannelAfterAlternativesAreExcluded(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "rate-limit-fallback-after-alternative-model"
	createChannelSelectAutoGroupsChannel(t, db, 2251, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2252, "vip", modelName)
	model.InitChannelCache()
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)
	StartChannelRateLimitCooldown(2251, modelName, 60)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       common.GetPointer(0),
	}

	selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2252, selected.Id)
	assert.Equal(t, "vip", selectedGroup)

	selected, selectedGroup, err = CacheGetRandomSatisfiedChannel(
		param,
		model.ChannelSelectionOptions{ExcludedChannelIds: []int{2252}},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2251, selected.Id)
	assert.Equal(t, "vip", selectedGroup)
}
