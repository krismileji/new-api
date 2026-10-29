package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useContentCaptureRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalEnabled, originalClient := common.RedisEnabled, common.RDB
	originalWrite, originalRead := common.RDBMonitorWrite, common.RDBMonitorRead
	common.RedisEnabled, common.RDB = true, client
	common.RDBMonitorWrite, common.RDBMonitorRead = nil, nil
	t.Cleanup(func() {
		require.NoError(t, service.DisableContentCapture(context.Background()))
		common.RedisEnabled, common.RDB = originalEnabled, originalClient
		common.RDBMonitorWrite, common.RDBMonitorRead = originalWrite, originalRead
		require.NoError(t, client.Close())
	})
	return server
}

func TestContentCaptureSessionKeepsNewestAndRejectsStaleWrites(t *testing.T) {
	server := useContentCaptureRedis(t)
	ctx := context.Background()

	config, err := service.EnableContentCapture(ctx, 2, 4, 1)
	require.NoError(t, err)
	require.True(t, config.Enabled)
	for _, id := range []string{"a", "b", "c"} {
		record := service.ContentCaptureRecord{RequestBody: "req-" + id, ResponseBody: "resp-" + id}
		record.Id = id
		require.NoError(t, service.SaveContentCaptureRecord(ctx, config.Session, record))
	}
	items, err := service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{"c", "b"}, []string{items[0].Id, items[1].Id})
	trimmed, err := service.GetContentCaptureRecord(ctx, "a")
	require.NoError(t, err)
	assert.Nil(t, trimmed)
	assert.Greater(t, server.TTL("channelMonitor:contentCapture:v1:bodies"), time.Duration(0))

	updated, err := service.EnableContentCapture(ctx, 1, 4, 1)
	require.NoError(t, err)
	assert.Equal(t, config.Session, updated.Session, "updating limits keeps the active session")
	items, err = service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, service.DisableContentCapture(ctx))
	stale := service.ContentCaptureRecord{}
	stale.Id = "late"
	require.NoError(t, service.SaveContentCaptureRecord(ctx, config.Session, stale))
	assert.Empty(t, server.Keys(), "disable clears every key and late writes are dropped")

	next, err := service.EnableContentCapture(ctx, 5, 4, 1)
	require.NoError(t, err)
	assert.NotEqual(t, config.Session, next.Session)
	require.NoError(t, service.SaveContentCaptureRecord(ctx, config.Session, stale))
	items, err = service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, items, "a write from the previous session is rejected")
}

func TestContentCaptureMiddlewareRecordsTruncatedRelayBodies(t *testing.T) {
	useContentCaptureRedis(t)
	gin.SetMode(gin.TestMode)
	_, err := service.EnableContentCapture(context.Background(), 10, 1, 1)
	require.NoError(t, err)

	router := gin.New()
	router.Use(BodyStorageCleanup(), ChannelMonitorContentCapture())
	router.POST("/v1/chat/completions", RouteTag("relay"), func(c *gin.Context) {
		c.Set(common.RequestIdKey, "req-1")
		c.Set("id", 0)
		c.Set("token_id", 3)
		c.Set("original_model", "gpt-test")
		_, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, strings.Repeat("y", 1500))
	})
	router.POST("/v1/other", func(c *gin.Context) { c.Status(http.StatusOK) })

	requestBody := `{"messages":"` + strings.Repeat("x", 2000) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), request)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/other", strings.NewReader("{}")))

	var record *service.ContentCaptureRecord
	require.Eventually(t, func() bool {
		record, err = service.GetContentCaptureRecord(context.Background(), "req-1")
		return err == nil && record != nil
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, requestBody[:1024], record.RequestBody)
	assert.Equal(t, int64(len(requestBody)), record.RequestSize)
	assert.True(t, record.RequestTruncated)
	assert.Len(t, record.ResponseBody, 1024)
	assert.Equal(t, int64(1500), record.ResponseSize)
	assert.True(t, record.ResponseTruncated)
	assert.True(t, record.IsStream)
	assert.Equal(t, 3, record.TokenId)
	assert.Equal(t, "gpt-test", record.Model)

	items, err := service.ListContentCaptureSummaries(context.Background())
	require.NoError(t, err)
	assert.Len(t, items, 1, "routes without the relay tag are not captured")
}
