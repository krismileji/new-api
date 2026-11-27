package openai

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func newEmptyResponseError() *types.NewAPIError {
	return types.NewOpenAIError(errors.New("上游输出 Token 数为 0"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
}
