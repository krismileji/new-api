package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnavailableAffinityBindingOnlyBlocksExplicitStrictMode(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	createChannelSelectAutoGroupsChannel(t, db, 2291, "default", "legacy-affinity-model")
	model.InitChannelCache()
	setting := operation_setting.GetChannelAffinitySetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })

	for _, mode := range []string{"", "strict"} {
		t.Run("mode="+mode, func(t *testing.T) {
			rule := operation_setting.ChannelAffinityRule{
				Name: t.Name(), ModelRegex: []string{"^legacy-affinity-model$"},
				KeySources:  []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}},
				SessionMode: mode, SkipRetryOnFailure: true, IncludeRuleName: true,
			}
			setting.Enabled = true
			setting.Rules = []operation_setting.ChannelAffinityRule{rule}
			key := buildChannelAffinityCacheKeySuffix(rule, "legacy-affinity-model", "default", t.Name())
			cache := getChannelAffinityCache()
			require.NoError(t, cache.SetWithTTL(key, 999999, time.Minute))
			t.Cleanup(func() { _, err := cache.DeleteMany([]string{key}); assert.NoError(t, err) })
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Request.Header.Set("X-Session", t.Name())
			selected, group, selectionErr := SelectChannelForRequest(ctx, "legacy-affinity-model", &RetryParam{
				Ctx: ctx, TokenGroup: "default", ModelName: "legacy-affinity-model", RequestPath: ctx.Request.URL.Path,
			})
			if mode == "strict" {
				require.NotNil(t, selectionErr)
				assert.Equal(t, "strict_session_binding_unavailable", selectionErr.Message)
				assert.Nil(t, selected)
				return
			}
			require.Nil(t, selectionErr)
			require.NotNil(t, selected)
			assert.Equal(t, 2291, selected.Id)
			assert.Equal(t, "default", group)
		})
	}
}
