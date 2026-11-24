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

	config, err := service.EnableContentCapture(ctx, 2, 4, 1, false)
	require.NoError(t, err)
	require.True(t, config.Enabled)
	assert.False(t, config.CaptureRaw)
	for _, id := range []string{"a", "b", "c"} {
		record := service.ContentCaptureRecord{}
		record.RequestBody, record.ResponseBody = "req-"+id, "resp-"+id
		record.Id = id
		require.NoError(t, service.SaveContentCaptureRecord(ctx, config.Session, record))
	}
	items, err := service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{"c", "b"}, []string{items[0].Id, items[1].Id})
	legacy, err := service.GetContentCaptureRecord(ctx, "c")
	require.NoError(t, err)
	require.NotNil(t, legacy)
	assert.False(t, legacy.ContentExtracted)
	assert.Equal(t, "req-c", legacy.RequestBody)
	assert.Equal(t, "resp-c", legacy.ResponseBody)
	trimmed, err := service.GetContentCaptureRecord(ctx, "a")
	require.NoError(t, err)
	assert.Nil(t, trimmed)
	assert.Greater(t, server.TTL("channelMonitor:contentCapture:v1:bodies"), time.Duration(0))

	updated, err := service.EnableContentCapture(ctx, 1, 4, 1, true)
	require.NoError(t, err)
	assert.Equal(t, config.Session, updated.Session, "updating limits keeps the active session")
	assert.True(t, updated.CaptureRaw)
	items, err = service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, service.DisableContentCapture(ctx))
	stale := service.ContentCaptureRecord{}
	stale.Id = "late"
	require.NoError(t, service.SaveContentCaptureRecord(ctx, config.Session, stale))
	assert.Empty(t, server.Keys(), "disable clears every key and late writes are dropped")

	next, err := service.EnableContentCapture(ctx, 5, 4, 1, false)
	require.NoError(t, err)
	assert.NotEqual(t, config.Session, next.Session)
	require.NoError(t, service.SaveContentCaptureRecord(ctx, config.Session, stale))
	items, err = service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, items, "a write from the previous session is rejected")
}

func TestContentCaptureMiddlewareRecordsOriginalInputAndVisibleReply(t *testing.T) {
	useContentCaptureRedis(t)
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	_, err := service.EnableContentCapture(ctx, 10, 1, 1, false)
	require.NoError(t, err)

	router := gin.New()
	router.Use(BodyStorageCleanup(), ChannelMonitorContentCapture())
	responseParts := []string{
		": ping\r\n\r\n",
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"` + strings.Repeat("y", 1500) + `"}}]}` + "\r\n\r\n",
		"da",
		`ta: {"choices":[{"index":0,"delta":{"content":"第一段"}}]}` + "\n\n",
		`data: {"choices":[{"index":1,"delta":{"content":"备用答案"}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"content":"\n第二段"}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}
	router.POST("/v1/chat/completions", RouteTag("relay"), func(c *gin.Context) {
		c.Set(common.RequestIdKey, c.GetHeader("X-Test-Request"))
		c.Set("token_id", 3)
		c.Set("original_model", "gpt-test")
		original, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		require.Equal(t, c.GetHeader("X-Test-Request") == "raw-opt-in", original.IsDisk())
		modified, err := common.CreateBodyStorage([]byte(`{"messages":[{"role":"user","content":"rewritten"}]}`))
		require.NoError(t, err)
		require.NoError(t, original.Close())
		c.Set(common.KeyBodyStorage, modified)
		c.Request.ContentLength = modified.Size()

		c.Header("Content-Type", "text/event-stream")
		for _, part := range responseParts {
			_, err := c.Writer.WriteString(part)
			require.NoError(t, err)
		}
	})
	router.POST("/v1/other", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/other", strings.NewReader("{}")))

	prompt := strings.Repeat("问", 400)
	requestBody := `{"tools":[{"description":"` + strings.Repeat("x", 1500) + `"}],"messages":[{"role":"user","content":"old"},{"role":"assistant","content":"old reply"},{"role":"user","content":"` + prompt + `"}]}`
	for _, tc := range []struct {
		id  string
		raw bool
	}{{"default", false}, {"raw-opt-in", true}} {
		t.Run(tc.id, func(t *testing.T) {
			diskConfig := common.GetDiskCacheConfig()
			t.Cleanup(func() { common.SetDiskCacheConfig(diskConfig) })
			common.SetDiskCacheConfig(common.DiskCacheConfig{
				Enabled: tc.raw, ThresholdMB: 0, MaxSizeMB: 16, Path: t.TempDir(),
			})
			_, err := service.EnableContentCapture(ctx, 10, 1, 1, tc.raw)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Test-Request", tc.id)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			rawResponse := strings.Join(responseParts, "")
			require.Equal(t, rawResponse, response.Body.String())
			var record *service.ContentCaptureRecord
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				record, err = service.GetContentCaptureRecord(ctx, tc.id)
				require.NoError(collect, err)
				require.NotNil(collect, record)
			}, 2*time.Second, 10*time.Millisecond)
			assert.True(t, record.ContentExtracted)
			assert.Equal(t, strings.Repeat("问", 341), record.RequestText)
			assert.True(t, record.RequestTextTruncated)
			assert.Equal(t, strings.Repeat("问", 100), record.RequestPreview)
			assert.Equal(t, "第一段\n第二段", record.ResponseText)
			assert.False(t, record.ResponseTextTruncated)
			assert.Equal(t, int64(len(requestBody)), record.RequestSize)
			assert.Equal(t, int64(len(rawResponse)), record.ResponseSize)
			assert.True(t, record.IsStream)
			assert.Equal(t, 3, record.TokenId)
			assert.Equal(t, "gpt-test", record.Model)
			if tc.raw {
				assert.Equal(t, requestBody[:1024], record.RequestBody)
				assert.Equal(t, rawResponse[:1024], record.ResponseBody)
				assert.True(t, record.RequestTruncated)
				assert.True(t, record.ResponseTruncated)
			} else {
				assert.Empty(t, record.RequestBody)
				assert.Empty(t, record.ResponseBody)
				assert.False(t, record.RequestTruncated)
				assert.False(t, record.ResponseTruncated)
			}
		})
	}

	items, err := service.ListContentCaptureSummaries(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 2, "routes without the relay tag are not captured")
}

func TestContentCaptureConversationFormats(t *testing.T) {
	for _, tc := range []struct {
		name     string
		request  string
		response string
		stream   bool
		status   int
		wantIn   string
		wantOut  string
		partial  bool
	}{
		{
			name:     "Responses retains typed user input and falls back to the final event",
			request:  `{"instructions":"hidden","input":[{"role":"user","content":[{"type":"input_text","text":"Read this"},{"type":"input_file","file_data":"secret"}]},{"role":"user","content":"Summarize"}]}`,
			response: "event: response.completed\n" + `data: {"type":"response.completed","response":{"output":[{"type":"reasoning","summary":[{"text":"hidden"}]},{"type":"message","content":[{"type":"output_text","text":"Summary"}]}]}}` + "\n\n",
			stream:   true,
			wantIn:   "Read this\nSummarize\n［文件 × 1］",
			wantOut:  "Summary",
		},
		{
			name:    "Responses keeps text deltas without duplicating the completed snapshot",
			request: `{"input":"问好"}`,
			response: `data: {"type":"response.output_text.delta","delta":"你"}` + "\n\n" +
				`data: {"type":"response.output_text.delta","delta":"好"}` + "\n\n" +
				`data: {"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"你好"}]}]}}` + "\n\n",
			stream:  true,
			wantIn:  "问好",
			wantOut: "你好",
		},
		{
			name:     "Claude tool follow-up with a tool-only response",
			request:  `{"messages":[{"role":"user","content":"Old question"},{"role":"assistant","content":[{"type":"tool_use","name":"fetch"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"id","content":"secret"}]}]}`,
			response: `{"type":"message","content":[{"type":"thinking","thinking":"hidden"},{"type":"tool_use","name":"lookup","input":{"secret":true}}]}`,
			wantIn:   "（本次仅提交工具结果）",
			wantOut:  "调用工具：lookup",
		},
		{
			name:    "Claude SSE preserves visible text and reports a midstream error",
			request: `{"messages":[{"role":"user","content":[{"type":"text","text":"问"}]}]}`,
			response: "event: content_block_delta\r\n" + `data: {"type":"content_block_delta",` + "\r\n" +
				`data: "delta":{"type":"text_delta","text":"部分回复"}}` + "\r\n\r\n" +
				`data: {"type":"error","error":{"type":"overloaded_error","message":"please retry"}}`,
			stream:  true,
			wantIn:  "问",
			wantOut: "部分回复\n错误：please retry",
		},
		{
			name:     "Gemini attachments are counted and thought parts are skipped",
			request:  `{"systemInstruction":{"parts":[{"text":"hidden"}]},"contents":[{"parts":[{"text":"Describe"},{"inlineData":{"mimeType":"image/png","data":"secret"}},{"fileData":{"mimeType":"image/jpeg","fileUri":"secret"}}]}]}`,
			response: `{"candidates":[{"index":0,"content":{"parts":[{"text":"hidden","thought":true},{"text":"Description"}]}}]}`,
			wantIn:   "Describe\n［图片 × 2］",
			wantOut:  "Description",
		},
		{
			name:     "Gemini tool results do not replay the earlier question",
			request:  `{"contents":[{"role":"user","parts":[{"text":"Old question"}]},{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"secret":"hidden"}}}]}]}`,
			response: `{"candidates":[{"index":0,"content":{"parts":[{"text":"Found it"}]}}]}`,
			wantIn:   "（本次仅提交工具结果）",
			wantOut:  "Found it",
		},
		{
			name:     "failed JSON replies show the returned error message",
			request:  `{"input":"Hello"}`,
			response: `{"message":"too many requests","details":{"secret":"hidden"}}`,
			status:   http.StatusTooManyRequests,
			wantIn:   "Hello",
			wantOut:  "错误：too many requests",
		},
		{
			name:    "SSE skips oversized events and still examines subsequent events",
			request: `{"input":"Hello"}`,
			response: `data: {"choices":[{"delta":{"reasoning_content":"` + strings.Repeat("x", captureResponseJSONLimit) + `"}}]}` + "\n\n" +
				`data: {"choices":[{"delta":{"content":"Hello"}}]}` + "\n\n",
			stream:  true,
			wantIn:  "Hello",
			wantOut: "Hello",
			partial: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage, err := common.CreateBodyStorage([]byte(tc.request))
			require.NoError(t, err)
			defer storage.Close()

			var record service.ContentCaptureRecord
			recordCaptureRequest(&record, storage, "application/json", 1024, false)
			assert.Equal(t, tc.wantIn, record.RequestText)

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Header("Content-Type", "application/json")
			if tc.stream {
				c.Header("Content-Type", "text/event-stream")
			}
			w := capturedResponseWriter{ResponseWriter: c.Writer, limit: 1024}
			if tc.status != 0 {
				w.WriteHeader(tc.status)
			}
			_, err = w.WriteString(tc.response)
			require.NoError(t, err)
			w.recordResponse(&record)
			assert.Equal(t, tc.wantOut, record.ResponseText)
			assert.Equal(t, tc.partial, record.ResponseTextTruncated)
		})
	}
}
