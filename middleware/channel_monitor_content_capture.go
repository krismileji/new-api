package middleware

import (
	"context"
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

// ChannelMonitorContentCapture records user input and client-visible replies
// for relay POST requests while a temporary capture session is active.
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
		record := service.ContentCaptureRecord{}
		record.ContentExtracted = true
		record.RequestSize = c.Request.ContentLength
		record.RequestText = "（请求体未读取）"
		mediaType := captureMediaType(c.GetHeader("Content-Type"))
		if !captureJSON(mediaType) && !strings.HasPrefix(mediaType, "text/") {
			record.RequestText = "（非文本请求，仅记录大小）"
		}
		common.ObserveInitialBodyStorage(c, func(storage common.BodyStorage) {
			recordCaptureRequest(&record, storage, mediaType, limit, config.CaptureRaw)
		})
		writer := &capturedResponseWriter{ResponseWriter: c.Writer, limit: limit, rawEnabled: config.CaptureRaw}
		c.Writer = writer
		startedAt := time.Now()
		c.Next()
		if c.Writer == writer {
			c.Writer = writer.ResponseWriter
		}
		if c.GetString(RouteTagKey) != "relay" {
			return
		}

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
		record.DurationMs = time.Since(startedAt).Milliseconds()
		writer.recordResponse(&record)

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
