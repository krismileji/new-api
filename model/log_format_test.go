package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatUserLogsShowsOnlyStatusCodeForRelayErrors(t *testing.T) {
	logs := []*Log{
		{
			Type:    LogTypeError,
			Content: "status_code=503, 服务暂时不可用，请稍后重试",
			Other:   common.MapToJsonStr(map[string]interface{}{"status_code": 503}),
		},
		{
			Type:    LogTypeError,
			Content: "status_code=524, upstream timeout",
			Other:   "{}",
		},
		{
			Type:    LogTypeError,
			Content: "status_code=429, upstream rate limit",
			Other: common.MapToJsonStr(map[string]interface{}{
				"status_code":                429,
				"user_visible_error_message": "请求过于频繁，请稍后再试",
			}),
		},
		{
			Type:    LogTypeError,
			Content: "provider diagnostic without status code",
			Other:   "{}",
		},
		{
			Type:    LogTypeConsume,
			Content: "正常消费日志",
			Other:   "{}",
		},
	}

	formatUserLogs(logs, 0)

	require.Equal(t, "status_code=503", logs[0].Content)
	require.Equal(t, "status_code=524", logs[1].Content)
	require.Equal(t, "请求过于频繁，请稍后再试", logs[2].Content)
	require.Equal(t, "请求失败", logs[3].Content)
	require.Equal(t, "正常消费日志", logs[4].Content)
}

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestTaskPluginLogVisibilityIsRoleSeparated(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 1.25,
		"admin_info": map[string]interface{}{
			"task_plugin": map[string]interface{}{
				"key":     "document-parser",
				"name":    "Document Parser",
				"version": "1.2.3",
			},
		},
		"root_info": map[string]interface{}{
			"upstream_task_id": "upstream-private",
			"task_plugin": map[string]interface{}{
				"generation": 42,
			},
		},
	})

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		formatUserLogs(logs, 0)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.NotContains(t, parsed, "admin_info")
		assert.NotContains(t, parsed, "root_info")
		assert.Equal(t, 1.25, parsed["model_price"])
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		FormatAdminLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "admin_info")
		assert.NotContains(t, parsed, "root_info")
	})

	t.Run("root", func(t *testing.T) {
		parsed, err := common.StrToMap(other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "admin_info")
		assert.Contains(t, parsed, "root_info")
	})
}
