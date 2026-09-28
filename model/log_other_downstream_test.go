package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyLogMetadataKeepsSensitiveFieldsScoped(t *testing.T) {
	other := LogOtherFromLegacyMap(map[string]any{
		"request_path": "/v1/responses", "channel_name": "private-channel",
		"channel_id": 7, "channel_type": 1,
		"admin_info": map[string]any{"is_multi_key": true},
		"root_info":  map[string]any{"upstream_request_id": "private-request"},
	})
	values := other.Snapshot()
	assert.Equal(t, "/v1/responses", values["request_path"])
	assert.NotContains(t, values, "channel_name")
	assert.NotContains(t, values, "channel_id")
	assert.NotContains(t, values, "channel_type")
	admin, ok := values["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "private-channel", admin["channel_name"])
	assert.Equal(t, true, admin["is_multi_key"])
	assert.Equal(t, map[string]any{"upstream_request_id": "private-request"}, values["root_info"])
}
