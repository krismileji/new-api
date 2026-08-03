package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyChannelMonitorUpstreamGroupDoesNotRecordStaleFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.4}}}`))
		case "/api/token/search":
			require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
				Where("channel_id = ?", 122).
				Updates(map[string]any{
					"upstream_revision": int64(2),
					"upstream_group":    "standard",
				}).Error)
			_, _ = w.Write([]byte(`{"success":false,"message":"token lookup failed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	revision := int64(1)
	require.NoError(t, db.Create(&model.Channel{
		Id: 122, Name: "stale apply failure", Key: "sk-channel", Group: "vip",
		BaseURL: &baseURL, Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           122,
		Ratio:               1,
		UpdatedTime:         1,
		UpstreamType:        service.NewAPIUpstreamType,
		UpstreamBaseURL:     server.URL,
		UpstreamGroup:       "vip",
		UpstreamAuthType:    service.NewAPIUpstreamAuthUser,
		UpstreamUserId:      42,
		UpstreamAccessToken: "dashboard-token",
		UpstreamRevision:    revision,
		LastFetchStatus:     model.ChannelRatioFetchStatusSucceeded,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel_monitor/channel/122/upstream/group/apply", nil,
	)
	ctx.Params = gin.Params{{Key: "id", Value: "122"}}
	ApplyChannelMonitorUpstreamGroup(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "上游配置已变化")

	monitor, err := model.GetChannelRatioMonitor(122)
	require.NoError(t, err)
	assert.Equal(t, int64(2), monitor.UpstreamRevision)
	assert.Equal(t, "standard", monitor.UpstreamGroup)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
	assert.Empty(t, monitor.LastFetchError)
	assert.Zero(t, monitor.ConsecutiveFailures)
}

func TestRunChannelRatioMonitorTaskDoesNotRecordLookupFailureAfterConfigChange(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:                   123,
		UpstreamType:                service.NewAPIUpstreamType,
		UpstreamBaseURL:             "https://example.com",
		UpstreamGroup:               "vip",
		UpstreamAuthType:            service.NewAPIUpstreamAuthPublic,
		UpstreamRevision:            1,
		UpstreamBalanceSyncDisabled: true,
		LastFetchStatus:             model.ChannelRatioFetchStatusSucceeded,
	}).Error)

	const callbackName = "test:change_ratio_monitor_revision_before_channel_lookup"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "channels" {
			return
		}
		tx.Session(&gorm.Session{NewDB: true}).
			Model(&model.ChannelRatioMonitor{}).
			Where("channel_id = ?", 123).
			Updates(map[string]any{
				"upstream_revision": int64(2),
				"upstream_group":    "standard",
			})
	}))
	t.Cleanup(func() {
		db.Callback().Query().Remove(callbackName)
	})

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)

	monitor, err := model.GetChannelRatioMonitor(123)
	require.NoError(t, err)
	assert.Equal(t, int64(2), monitor.UpstreamRevision)
	assert.Equal(t, "standard", monitor.UpstreamGroup)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
	assert.Empty(t, monitor.LastFetchError)
	assert.Zero(t, monitor.ConsecutiveFailures)
}
