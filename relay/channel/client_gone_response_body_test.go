package channel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/iotest"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseBodyOnlyClassifiesClientCancellation(t *testing.T) {
	canceledContext, cancelClient := context.WithCancel(t.Context())
	cancelClient()
	deadlineContext, cancelDeadline := context.WithDeadline(t.Context(), time.Unix(0, 0))
	t.Cleanup(cancelDeadline)
	require.ErrorIs(t, deadlineContext.Err(), context.DeadlineExceeded)
	serverContext, cancelServer := context.WithCancelCause(t.Context())
	cancelServer(errors.New("server stopped this request"))

	tests := []struct {
		name       string
		ctx        context.Context
		readErr    error
		clientGone bool
	}{
		{
			name:       "wrapped client cancellation",
			ctx:        canceledContext,
			readErr:    fmt.Errorf("read response: %w", context.Canceled),
			clientGone: true,
		},
		{
			name:    "upstream cancellation with active client",
			ctx:     t.Context(),
			readErr: context.Canceled,
		},
		{
			name:    "request deadline",
			ctx:     deadlineContext,
			readErr: context.DeadlineExceeded,
		},
		{
			name:    "explicit server cancellation",
			ctx:     serverContext,
			readErr: context.Canceled,
		},
		{
			name:    "upstream timeout despite client cancellation",
			ctx:     canceledContext,
			readErr: context.DeadlineExceeded,
		},
		{
			name:    "completed body despite client cancellation",
			ctx:     canceledContext,
			readErr: io.EOF,
		},
		{
			name:    "truncated upstream body despite client cancellation",
			ctx:     canceledContext,
			readErr: io.ErrUnexpectedEOF,
		},
		{
			name:    "upstream error with cancellation text",
			ctx:     canceledContext,
			readErr: errors.New("context canceled"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &clientGoneResponseBody{
				ReadCloser:     io.NopCloser(iotest.ErrReader(tt.readErr)),
				requestContext: tt.ctx,
			}
			_, err := body.Read(make([]byte, 1))
			if tt.clientGone {
				require.True(t, types.IsClientGoneError(err))
				assert.ErrorIs(t, err, context.Canceled)
				return
			}
			assert.Equal(t, tt.readErr, err)
			assert.False(t, types.IsClientGoneError(err))
		})
	}
}
