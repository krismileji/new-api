package service

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/tiktoken-go/tokenizer"
	"github.com/tiktoken-go/tokenizer/codec"
)

const MaxInputContextTokens = 272000

var (
	enableInputContextLimit = common.GetEnvOrDefaultBool("ENABLE_272K_CONTEXT_LIMIT", true)
	contextEncodersOnce     sync.Once
	contextCl100kEncoder    tokenizer.Codec
	contextO200kEncoder     tokenizer.Codec
)

func InputContextLimitEnabled() bool {
	return enableInputContextLimit
}

func IsInputContextLimitFormat(format types.RelayFormat) bool {
	switch format {
	case types.RelayFormatOpenAI,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAIResponsesCompaction:
		return true
	default:
		return false
	}
}

// EnforceInputContextLimit checks the complete JSON body. It deliberately uses
// the larger cl100k/o200k count so new model names cannot silently fall back to
// a tokenizer that undercounts their input.
func EnforceInputContextLimit(body []byte) (int, *types.NewAPIError) {
	if !InputContextLimitEnabled() || len(body) == 0 {
		return 0, nil
	}

	contextEncodersOnce.Do(func() {
		contextCl100kEncoder = codec.NewCl100kBase()
		contextO200kEncoder = codec.NewO200kBase()
	})

	text := string(body)
	cl100kTokens, err := contextCl100kEncoder.Count(text)
	if err != nil {
		return 0, types.NewError(fmt.Errorf("count input context with cl100k: %w", err), types.ErrorCodeCountTokenFailed, types.ErrOptionWithSkipRetry())
	}
	o200kTokens, err := contextO200kEncoder.Count(text)
	if err != nil {
		return 0, types.NewError(fmt.Errorf("count input context with o200k: %w", err), types.ErrorCodeCountTokenFailed, types.ErrOptionWithSkipRetry())
	}

	contextTokens := max(cl100kTokens, o200kTokens)
	return contextTokens, EnforceInputContextTokenLimit(contextTokens)
}

func EnforceInputContextTokenLimit(contextTokens int) *types.NewAPIError {
	if !InputContextLimitEnabled() || contextTokens <= MaxInputContextTokens {
		return nil
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("input context length %d exceeds the maximum allowed context length of %d tokens", contextTokens, MaxInputContextTokens),
		types.ErrorCodeInvalidRequest,
		http.StatusOK,
		types.ErrOptionWithSkipRetry(),
	)
}
