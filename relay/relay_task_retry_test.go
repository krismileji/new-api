package relay

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type taskRetryBillingSettler struct {
	preConsumedQuota int
	reserveTargets   []int
}

func (*taskRetryBillingSettler) Settle(int) error { return nil }

func (*taskRetryBillingSettler) Refund(*gin.Context) {}

func (*taskRetryBillingSettler) NeedsRefund() bool { return false }

func (s *taskRetryBillingSettler) GetPreConsumedQuota() int { return s.preConsumedQuota }

func (s *taskRetryBillingSettler) Reserve(targetQuota int) error {
	s.reserveTargets = append(s.reserveTargets, targetQuota)
	if targetQuota > s.preConsumedQuota {
		s.preConsumedQuota = targetQuota
	}
	return nil
}

func TestPrepareTaskBillingRaisesReservationBeforeRetry(t *testing.T) {
	billing := &taskRetryBillingSettler{preConsumedQuota: 100}
	info := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 100,
		PriceData:             hosttypes.PriceData{Quota: 250},
	}

	require.Nil(t, prepareTaskBilling(nil, info))
	assert.True(t, info.ForcePreConsume)
	assert.Equal(t, []int{250}, billing.reserveTargets)
	assert.Equal(t, 250, info.FinalPreConsumedQuota)
}

func TestResolveOriginTaskDoesNotRequireExistingChannelMeta(t *testing.T) {
	originalDB := model.DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "relay-task-retry.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Task{}))
	model.DB = db
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	baseURL := "https://origin.example"
	require.NoError(t, db.Create(&model.Channel{
		Id: 51, Type: constant.ChannelTypeOpenAI, Name: "origin-channel", Key: "origin-key",
		Status: common.ChannelStatusEnabled, BaseURL: &baseURL,
	}).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID: "task-origin", UserId: 71, ChannelId: 51,
		Properties: model.Properties{OriginModelName: "sora-test"},
	}).Error)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task-origin/remix", nil)
	c.Params = gin.Params{{Key: "video_id", Value: "task-origin"}}
	info := &relaycommon.RelayInfo{UserId: 71, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	require.NotPanics(t, func() {
		require.Nil(t, ResolveOriginTask(c, info))
	})
	assert.Nil(t, info.ChannelMeta)
	assert.Equal(t, "sora-test", info.OriginModelName)
	assert.Equal(t, constant.TaskActionRemix, info.Action)
	locked, ok := info.LockedChannel.(*model.Channel)
	require.True(t, ok)
	require.NotNil(t, locked)
	assert.Equal(t, 51, locked.Id)
	assert.Equal(t, baseURL, locked.GetBaseURL())
	assert.Zero(t, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
}

func TestRelayTaskSubmitPreservesOriginTaskBillingRatios(t *testing.T) {
	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-remix-retry-test":0.01}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	receivedPath := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, err := w.Write([]byte(`{"id":"upstream-task","object":"video","model":"sora-remix-retry-test","status":"queued"}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos/origin-task/remix",
		bytes.NewBufferString(`{"prompt":"remix this video"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelId, 71)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSora)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")

	billing := &taskRetryBillingSettler{preConsumedQuota: 100}
	info := &relaycommon.RelayInfo{
		OriginModelName: "sora-remix-retry-test",
		UserGroup:       "default",
		UsingGroup:      "default",
		Billing:         billing,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionRemix,
			OriginTaskID: "origin-task",
		},
	}
	info.PriceData.AddOtherRatio("seconds", 8)
	info.PriceData.AddOtherRatio("size", 1.5)

	result, taskErr := RelayTaskSubmit(c, info)

	require.Nil(t, taskErr)
	require.NotNil(t, result)
	assert.Equal(t, "/v1/videos/origin-task/remix", <-receivedPath)
	assert.Equal(t, map[string]float64{"seconds": 8, "size": 1.5}, info.PriceData.OtherRatios())
	assert.Equal(t, []int{60_000}, billing.reserveTargets)
	assert.Equal(t, 60_000, info.FinalPreConsumedQuota)
	assert.Equal(t, 60_000, result.Quota)
}

func TestRelayTaskSubmitDoesNotRetryAfterAcceptedResponseParseFailure(t *testing.T) {
	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-parse-failure-test":0.01}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"id":`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos/origin-task/remix",
		bytes.NewBufferString(`{"prompt":"remix this video"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelId, 72)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSora)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")

	info := &relaycommon.RelayInfo{
		OriginModelName: "sora-parse-failure-test",
		UserGroup:       "default",
		UsingGroup:      "default",
		Billing:         &taskRetryBillingSettler{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionRemix,
			OriginTaskID: "origin-task",
		},
	}

	result, taskErr := RelayTaskSubmit(c, info)

	assert.Nil(t, result)
	require.NotNil(t, taskErr)
	assert.False(t, taskErr.LocalError, "the channel must still receive the parse failure")
	var apiErr *relaytypes.NewAPIError
	require.True(t, errors.As(taskErr.Error, &apiErr))
	assert.True(t, relaytypes.IsSkipRetryError(apiErr))
	assert.Equal(t, 1, requestCount)
}
