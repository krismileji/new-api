package types

import (
	"context"
	"errors"
)

const (
	ErrorCodeClientGone       ErrorCode = "client_gone"
	StatusClientClosedRequest           = 499
)

func NewClientGoneError(err error) *NewAPIError {
	return NewErrorWithStatusCode(
		err,
		ErrorCodeClientGone,
		StatusClientClosedRequest,
		ErrOptionWithSkipRetry(),
		ErrOptionWithNoRecordErrorLog(),
	)
}

func NewClientGoneErrorFromContext(ctx context.Context, err error) *NewAPIError {
	if ctx == nil || err == nil {
		return nil
	}
	contextErr := ctx.Err()
	if contextErr == nil || !errors.Is(err, contextErr) {
		return nil
	}
	return NewClientGoneError(contextErr)
}

func IsClientGoneError(err error) bool {
	var apiErr *NewAPIError
	return errors.As(err, &apiErr) && apiErr.GetErrorCode() == ErrorCodeClientGone
}
