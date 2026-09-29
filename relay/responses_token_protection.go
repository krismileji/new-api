package relay

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
)

// Cancellation interrupts I/O, but the request worker retains settlement and
// error-event ownership. The session closes only after those workers finish.
type responsesWSTokenProtection struct {
	cause       atomic.Pointer[service.TokenAutoDisableCause]
	interrupted chan struct{}
	notified    bool // request worker; read by session only after workers.Wait
}

func (s *responsesWSSession) protectTokenSession() func() {
	s.tokenProtection = &responsesWSTokenProtection{interrupted: make(chan struct{})}
	stop := context.AfterFunc(s.ctx, func() {
		defer close(s.tokenProtection.interrupted)
		if s.tokenProtectionCause() == nil {
			return
		}
		s.closeTarget()
		_ = s.client.UnderlyingConn().SetReadDeadline(time.Now())
		_ = s.client.UnderlyingConn().SetWriteDeadline(time.Now())
	})
	return func() {
		if !stop() {
			<-s.tokenProtection.interrupted
		}
		if cause := s.tokenProtectionCause(); cause != nil {
			if !s.tokenProtection.notified {
				payload, err := buildResponsesWSErrorPayload("", "", types.NewErrorWithStatusCode(cause, service.TokenAutoDisabledCode, cause.Record.ResponseStatus, types.ErrOptionWithSkipRetry()))
				if err == nil {
					_ = s.client.SetWriteDeadline(time.Now().Add(time.Second))
					_ = s.client.WriteMessage(websocket.TextMessage, payload)
				}
			}
			_ = s.client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "API Key 已被自动禁用"), time.Now().Add(time.Second))
		}
		_ = s.client.Close()
	}
}

func (s *responsesWSSession) tokenProtectionCause() *service.TokenAutoDisableCause {
	if s.tokenProtection == nil {
		return nil
	}
	if cause := s.tokenProtection.cause.Load(); cause != nil {
		return cause
	}
	return service.TokenAutoDisableFromContext(s.ctx)
}

func (s *responsesWSSession) tokenProtectionError(apiErr *types.NewAPIError) *types.NewAPIError {
	var cause *service.TokenAutoDisableCause
	if apiErr != nil && errors.As(apiErr, &cause) {
		s.tokenProtection.cause.CompareAndSwap(nil, cause)
	}
	cause = s.tokenProtectionCause()
	if cause == nil {
		return nil
	}
	s.cancel()
	// Let the cancellation callback expire any blocked write before resetting
	// its deadline for the final error. No callback writes protocol data.
	<-s.tokenProtection.interrupted
	s.tokenProtection.notified = true
	return types.NewErrorWithStatusCode(cause, service.TokenAutoDisabledCode, cause.Record.ResponseStatus, types.ErrOptionWithSkipRetry())
}
