package middleware

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Keep response ownership with the request goroutine. Revocation signals work
// and interrupts blocked I/O; it never writes JSON from the cancellation callback.
func protectTokenRequest(c *gin.Context, token *model.Token) (func(), bool) {
	clientCtx := c.Request.Context()
	workCtx, unregister, denied := service.RegisterTokenProtection(clientCtx, token, c.GetString(common.RequestIdKey))
	if denied != nil {
		writeTokenProtectionResponse(c, c.Writer, denied)
		c.Abort()
		return unregister, true
	}
	if workCtx == clientCtx {
		return unregister, false
	}
	writer := c.Writer
	c.Request = c.Request.WithContext(workCtx)
	protectedWriter := &tokenProtectionWriter{ResponseWriter: writer, ctx: workCtx}
	c.Writer = protectedWriter
	response := http.NewResponseController(writer)
	body := &tokenProtectionRequestBody{ReadCloser: c.Request.Body, ctx: workCtx}
	if c.Request.Body != nil {
		c.Request.Body = body
	}
	interrupted := make(chan struct{})
	stopInterrupt := context.AfterFunc(workCtx, func() {
		defer close(interrupted)
		if service.TokenAutoDisableFromContext(workCtx) == nil {
			return
		}
		// Expiring an idle HTTP/2 stream would reset it before we can send its
		// custom error. Interrupt only I/O that is actually blocked.
		if body.reading.Load() > 0 {
			_ = response.SetReadDeadline(time.Now())
		}
		if protectedWriter.writing.Load() > 0 {
			_ = response.SetWriteDeadline(time.Now())
		}
	})
	return func() {
		if !stopInterrupt() {
			<-interrupted
		}
		cause := service.TokenAutoDisableFromContext(workCtx)
		if cause != nil && !protectedWriter.hijacked.Load() {
			_ = response.SetReadDeadline(time.Time{})
			_ = response.SetWriteDeadline(time.Now().Add(time.Second))
			writeTokenProtectionResponse(c, writer, cause)
			_ = response.SetReadDeadline(time.Time{})
			_ = response.SetWriteDeadline(time.Time{})
		}
		unregister()
	}, false
}

type tokenProtectionWriter struct {
	gin.ResponseWriter
	ctx      context.Context
	writing  atomic.Int32
	hijacked atomic.Bool
}

func (writer *tokenProtectionWriter) Write(p []byte) (int, error) {
	writer.writing.Add(1)
	defer writer.writing.Add(-1)
	if cause := service.TokenAutoDisableFromContext(writer.ctx); cause != nil {
		return 0, cause
	}
	return writer.ResponseWriter.Write(p)
}
func (writer *tokenProtectionWriter) WriteString(s string) (int, error) {
	return writer.Write([]byte(s))
}
func (writer *tokenProtectionWriter) WriteHeader(code int) {
	if service.TokenAutoDisableFromContext(writer.ctx) == nil {
		writer.ResponseWriter.WriteHeader(code)
	}
}
func (writer *tokenProtectionWriter) WriteHeaderNow() {
	if service.TokenAutoDisableFromContext(writer.ctx) == nil {
		writer.ResponseWriter.WriteHeaderNow()
	}
}
func (writer *tokenProtectionWriter) Flush() {
	writer.writing.Add(1)
	defer writer.writing.Add(-1)
	if service.TokenAutoDisableFromContext(writer.ctx) == nil {
		writer.ResponseWriter.Flush()
	}
}
func (writer *tokenProtectionWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *tokenProtectionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if cause := service.TokenAutoDisableFromContext(writer.ctx); cause != nil {
		return nil, nil, cause
	}
	conn, buffer, err := writer.ResponseWriter.Hijack()
	if err == nil {
		writer.hijacked.Store(true)
	}
	return conn, buffer, err
}

type tokenProtectionRequestBody struct {
	io.ReadCloser
	ctx     context.Context
	reading atomic.Int32
}

func (body *tokenProtectionRequestBody) Read(p []byte) (int, error) {
	body.reading.Add(1)
	defer body.reading.Add(-1)
	if cause := service.TokenAutoDisableFromContext(body.ctx); cause != nil {
		return 0, cause
	}
	return body.ReadCloser.Read(p)
}

func writeTokenProtectionResponse(c *gin.Context, writer gin.ResponseWriter, cause *service.TokenAutoDisableCause) {
	errorBody := gin.H{"error": gin.H{"code": service.TokenAutoDisabledCode, "type": "permission_error", "message": cause.Record.ResponseMessage}}
	claude := strings.HasSuffix(c.Request.URL.Path, "/messages")
	if claude {
		errorBody["type"] = "error"
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") || strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		errorBody = gin.H{"error": gin.H{"code": cause.Record.ResponseStatus, "status": "PERMISSION_DENIED", "message": cause.Record.ResponseMessage}}
	}
	responses := strings.Contains(c.Request.URL.Path, "/responses")
	if responses && writer.Written() {
		errorBody = gin.H{"type": "error", "code": service.TokenAutoDisabledCode, "message": cause.Record.ResponseMessage, "param": nil}
	}
	data, err := common.Marshal(errorBody)
	if err != nil {
		return
	}
	if !writer.Written() {
		for _, header := range []string{"Content-Length", "Content-Encoding", "Transfer-Encoding", "Connection", "X-Accel-Buffering"} {
			writer.Header().Del(header)
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(cause.Record.ResponseStatus)
		_, _ = writer.Write(data)
		return
	}
	if strings.HasPrefix(writer.Header().Get("Content-Type"), "text/event-stream") {
		event := ""
		if claude || responses {
			event = "event: error\n"
		}
		_, _ = fmt.Fprintf(writer, "%sdata: %s\n\n", event, data)
		writer.Flush()
	}
}
