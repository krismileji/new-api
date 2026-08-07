package relay

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
)

const imageGenerationNotEnabledForGroup = "image generation is not enabled for this group"

func applyResponsesUpstreamErrorPolicy(err *types.NewAPIError) {
	if err == nil || err.StatusCode != http.StatusForbidden {
		return
	}
	if !strings.Contains(strings.ToLower(err.Error()), imageGenerationNotEnabledForGroup) {
		return
	}

	// Missing optional upstream capability is not a channel-health failure.
	types.ErrOptionWithSkipRetry()(err)
	types.ErrOptionWithNoRecordErrorLog()(err)
}
