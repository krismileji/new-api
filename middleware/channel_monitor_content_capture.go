package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// ChannelMonitorContentCapture copies the client request body and the
// response written to the client for relay POST requests while a temporary
// capture session is active. Bodies are capped; headers are never stored.
func ChannelMonitorContentCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		config := service.CachedContentCaptureConfig()
		if !config.Enabled {
			c.Next()
			return
		}
		limit := config.MaxBodyKB << 10
		writer := &contentCaptureWriter{ResponseWriter: c.Writer, limit: limit}
		c.Writer = writer
		startedAt := time.Now()
		c.Next()
		if c.Writer == writer {
			c.Writer = writer.ResponseWriter
		}
		if c.GetString(RouteTagKey) != "relay" {
			return
		}

		record := service.ContentCaptureRecord{}
		record.Id = c.GetString(common.RequestIdKey)
		if record.Id == "" {
			record.Id = common.NewRequestId()
		}
		record.CreatedAt = startedAt.Unix()
		record.UserId = c.GetInt("id")
		record.TokenId = c.GetInt("token_id")
		record.TokenName = c.GetString("token_name")
		record.Group = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		record.Model = c.GetString("original_model")
		record.ChannelId = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		record.Path = c.Request.URL.Path
		record.Ip = c.ClientIP()
		record.StatusCode = writer.Status()
		record.IsStream = strings.HasPrefix(writer.Header().Get("Content-Type"), "text/event-stream")
		record.DurationMs = time.Since(startedAt).Milliseconds()
		record.ResponseBody = writer.body.String()
		record.ResponseSize = writer.size
		record.ResponseTruncated = writer.size > int64(limit)
		record.RequestBody, record.RequestSize, record.RequestTruncated = contentCaptureRequestBody(c, limit)

		gopool.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if record.UserId > 0 {
				if user, err := model.GetUserCache(record.UserId); err == nil {
					record.Username = user.Username
				}
			}
			if err := service.SaveContentCaptureRecord(ctx, config.Session, record); err != nil {
				logger.LogWarn(ctx, "save content capture record failed: %v", err)
			}
		})
	}
}

// contentCaptureRequestBody reads the cached body storage before the outer
// BodyStorageCleanup middleware closes it.
func contentCaptureRequestBody(c *gin.Context, limit int) (string, int64, bool) {
	contentType := c.Request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "json") && !strings.HasPrefix(contentType, "text/") {
		return "[非文本请求体，未记录]", c.Request.ContentLength, false
	}
	// Requests rejected before any handler read the body are not read here.
	value, _ := c.Get(common.KeyBodyStorage)
	storage, ok := value.(common.BodyStorage)
	if !ok || storage == nil {
		return "[请求体未读取]", c.Request.ContentLength, false
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return "[请求体读取失败]", storage.Size(), false
	}
	head, _ := io.ReadAll(io.LimitReader(storage, int64(limit)))
	_, _ = storage.Seek(0, io.SeekStart)
	return string(head), storage.Size(), storage.Size() > int64(limit)
}

type contentCaptureWriter struct {
	gin.ResponseWriter
	limit int
	body  bytes.Buffer
	size  int64
}

func (writer *contentCaptureWriter) Write(p []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(p)
	writer.capture(p[:n])
	return n, err
}

func (writer *contentCaptureWriter) WriteString(s string) (int, error) {
	n, err := writer.ResponseWriter.WriteString(s)
	writer.capture([]byte(s[:n]))
	return n, err
}

func (writer *contentCaptureWriter) capture(p []byte) {
	writer.size += int64(len(p))
	if room := writer.limit - writer.body.Len(); room > 0 {
		writer.body.Write(p[:min(room, len(p))])
	}
}

func (writer *contentCaptureWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
