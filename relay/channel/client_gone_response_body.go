package channel

import (
	"context"
	"io"

	"github.com/QuantumNous/new-api/relaykit/types"
)

type clientGoneResponseBody struct {
	io.ReadCloser
	requestContext context.Context
}

func (body *clientGoneResponseBody) Read(p []byte) (int, error) {
	n, err := body.ReadCloser.Read(p)
	// Classify before provider error wrappers can discard the original error
	// chain. Deadlines and explicit server cancellation causes stay unchanged.
	if err == nil || context.Cause(body.requestContext) != context.Canceled {
		return n, err
	}
	if clientGoneErr := types.NewClientGoneErrorFromContext(body.requestContext, err); clientGoneErr != nil {
		return n, clientGoneErr
	}
	return n, err
}
