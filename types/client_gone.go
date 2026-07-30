package types

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

func IsClientGoneError(err *NewAPIError) bool {
	return err != nil && err.GetErrorCode() == ErrorCodeClientGone
}
