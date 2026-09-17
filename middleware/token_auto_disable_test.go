package middleware

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTokenProtectionCommittedStreamUsesProtocolError(t *testing.T) {
	for _, test := range []struct {
		path  string
		event string
		body  string
	}{
		{"/v1/chat/completions", "", `{"error":{"code":"api_key_auto_disabled","type":"permission_error","message":"此 Key 已禁用"}}`},
		{"/v1/messages", "event: error\n", `{"type":"error","error":{"code":"api_key_auto_disabled","type":"permission_error","message":"此 Key 已禁用"}}`},
		{"/v1/responses", "event: error\n", `{"type":"error","code":"api_key_auto_disabled","message":"此 Key 已禁用","param":null}`},
		{"/v1beta/models/gemini:streamGenerateContent", "", `{"error":{"code":451,"status":"PERMISSION_DENIED","message":"此 Key 已禁用"}}`},
	} {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			c.Header("Content-Type", "text/event-stream")
			c.Writer.WriteHeaderNow()
			writeTokenProtectionResponse(c, c.Writer, &service.TokenAutoDisableCause{Record: model.TokenAutoDisableRecord{ResponseStatus: 451, ResponseMessage: "此 Key 已禁用"}})
			assert.Equal(t, http.StatusOK, recorder.Code, "committed HTTP headers cannot be replaced")
			require.True(t, strings.HasPrefix(recorder.Body.String(), test.event+"data: "))
			assert.JSONEq(t, test.body, strings.TrimSpace(strings.TrimPrefix(recorder.Body.String(), test.event+"data: ")))
		})
	}
}

func TestTokenProtectionTerminatesLiveHTTPAndSSE(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		name := "HTTP1"
		if http2 {
			name = "HTTP2"
		}
		t.Run(name, func(t *testing.T) { testTokenProtectionLiveHTTP(t, http2) })
	}
}

func testTokenProtectionLiveHTTP(t *testing.T, http2 bool) {
	t.Setenv("TOKEN_AUTO_DISABLE_JOURNAL_DIR", t.TempDir())
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/protection.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	oldDB, oldRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenAutoDisableConfig{}, &model.TokenAutoDisableRecord{}))
	token := &model.Token{Id: 97001, UserId: 42, Key: "test-protection-http", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(token).Error)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	require.NoError(t, service.InitTokenAutoDisable(workerCtx))
	t.Cleanup(func() {
		stopWorker()
		// Leave an empty manager for other authentication tests in this package.
		require.NoError(t, db.Where("1 = 1").Delete(&model.TokenAutoDisableRecord{}).Error)
		require.NoError(t, db.Where("1 = 1").Delete(&model.TokenAutoDisableConfig{}).Error)
		resetCtx, cancelReset := context.WithCancel(context.Background())
		require.NoError(t, service.InitTokenAutoDisable(resetCtx))
		cancelReset()
		model.DB, common.RedisEnabled = oldDB, oldRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	_, err = service.SaveTokenAutoDisableSettings(t.Context(), service.TokenAutoDisableSettings{Enabled: true, Rules: []service.TokenAutoDisableRule{{Id: "test", Name: "测试", Enabled: true, StatusCodes: []int{403}, Keywords: []string{"policy"}, ResponseStatus: 451, ResponseMessage: "此 Key 已禁用"}}})
	require.NoError(t, err)
	upstreamStarted := make(chan struct{})
	upstreamStopped := make(chan struct{})
	uploadStarted := make(chan struct{})
	uploadStopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(upstreamStarted)
		<-r.Context().Done()
		close(upstreamStopped)
	}))
	defer upstream.Close()
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		requestToken := token
		if c.GetHeader("X-Test-Other-Key") != "" {
			requestToken = &model.Token{Id: token.Id + 1, UserId: token.UserId}
		}
		finish, denied := protectTokenRequest(c, requestToken)
		defer finish()
		if !denied {
			c.Next()
		}
	})
	engine.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: started\n\n")
		c.Writer.Flush()
		<-c.Request.Context().Done()
		_, _ = c.Writer.WriteString("data: should-not-arrive\n\n")
	})
	engine.GET("/pending", func(c *gin.Context) {
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream.URL, nil)
		if err != nil {
			c.Status(500)
			return
		}
		resp, err := upstream.Client().Do(request)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			c.JSON(502, gin.H{"error": "wrong-response"})
		}
	})
	engine.GET("/trigger", func(c *gin.Context) { service.ObserveTokenAutoDisableError(c.Request.Context(), 7, 403, "policy") })
	engine.POST("/upload", func(c *gin.Context) {
		var first [1]byte
		_, _ = c.Request.Body.Read(first[:])
		close(uploadStarted)
		_, _ = io.Copy(io.Discard, c.Request.Body)
		close(uploadStopped)
	})
	engine.GET("/new", func(c *gin.Context) { c.String(200, "must not execute") })
	engine.GET("/other", func(c *gin.Context) { c.String(200, "other key remains available") })
	server := httptest.NewUnstartedServer(engine)
	server.EnableHTTP2 = http2
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	stream, err := client.Get(server.URL + "/stream")
	require.NoError(t, err)
	if http2 {
		assert.Equal(t, 2, stream.ProtoMajor)
	}
	defer stream.Body.Close()
	reader := bufio.NewReader(stream.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "data: started\n", line)
	type response struct {
		status int
		data   []byte
		err    error
	}
	pending := make(chan response, 1)
	upload := make(chan response, 1)
	requestReader, requestWriter := io.Pipe()
	defer requestWriter.Close()
	go func() {
		resp, err := client.Post(server.URL+"/upload", "application/octet-stream", requestReader)
		if err != nil {
			upload <- response{err: err}
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		upload <- response{resp.StatusCode, data, err}
	}()
	_, err = requestWriter.Write([]byte("x"))
	require.NoError(t, err)
	go func() {
		resp, err := client.Get(server.URL + "/pending")
		if err != nil {
			pending <- response{err: err}
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		pending <- response{resp.StatusCode, data, err}
	}()
	select {
	case <-upstreamStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request did not start")
	}
	select {
	case <-uploadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("upload request did not start")
	}
	trigger, err := client.Get(server.URL + "/trigger")
	require.NoError(t, err)
	data, err := io.ReadAll(trigger.Body)
	require.NoError(t, err)
	trigger.Body.Close()
	assert.Equal(t, 451, trigger.StatusCode)
	assert.Contains(t, string(data), "此 Key 已禁用")
	select {
	case <-uploadStopped:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked upload read was not interrupted")
	}
	// The request reader was canceled while the client still had its body open.
	// Closing the sender now lets both HTTP transports finish their cleanup.
	require.NoError(t, requestWriter.Close())
	select {
	case result := <-upload:
		assert.False(t, errors.Is(result.err, context.DeadlineExceeded), "revoked uploads must not wait for the client timeout")
		if result.err == nil {
			assert.Equal(t, 451, result.status)
			assert.Contains(t, string(result.data), "api_key_auto_disabled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload connection did not finish")
	}
	select {
	case result := <-pending:
		require.NoError(t, result.err)
		assert.Equal(t, 451, result.status)
		assert.Contains(t, string(result.data), "api_key_auto_disabled")
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not finish")
	}
	select {
	case <-upstreamStopped:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not canceled")
	}
	tail, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, 200, stream.StatusCode)
	assert.Contains(t, string(tail), "api_key_auto_disabled")
	assert.NotContains(t, string(tail), "should-not-arrive")
	newRequest, err := client.Get(server.URL + "/new")
	require.NoError(t, err)
	defer newRequest.Body.Close()
	data, err = io.ReadAll(newRequest.Body)
	require.NoError(t, err)
	assert.Equal(t, 451, newRequest.StatusCode)
	assert.Contains(t, string(data), "此 Key 已禁用")
	assert.NotContains(t, string(data), "must not execute")
	otherReq, err := http.NewRequest(http.MethodGet, server.URL+"/other", nil)
	require.NoError(t, err)
	otherReq.Header.Set("X-Test-Other-Key", "1")
	otherResp, err := client.Do(otherReq)
	require.NoError(t, err)
	defer otherResp.Body.Close()
	assert.Equal(t, 200, otherResp.StatusCode)
}
