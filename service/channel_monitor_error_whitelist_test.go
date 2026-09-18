package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorWhitelistedFailuresSkipSchedulingButKeepSuccessMetrics(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	})
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitBypasses)

	for _, test := range []struct {
		name              string
		whitelist         string
		statusCode        int
		retry             bool
		finalRetrySummary bool
		eligible          bool
		actualFailures    int64
		finalFailures     int64
	}{
		{name: "upstream error code", whitelist: " BAD_RESPONSE ", statusCode: 502, actualFailures: 1, finalFailures: 1},
		{name: "HTTP status", whitelist: "503", statusCode: 503, actualFailures: 1, finalFailures: 1},
		{name: "rate limit", whitelist: "429", statusCode: 429, actualFailures: 1, finalFailures: 1},
		{name: "retry attempt", whitelist: "503", statusCode: 503, retry: true, actualFailures: 1},
		{name: "final retry summary", whitelist: "503", statusCode: 503, finalRetrySummary: true, finalFailures: 1},
		{name: "unmatched error", whitelist: "503", statusCode: 502, eligible: true, actualFailures: 1, finalFailures: 1},
		{name: "empty whitelist", statusCode: 503, eligible: true, actualFailures: 1, finalFailures: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			common.OptionMap[ErrorMessageWhitelistOptionKey] = test.whitelist
			common.OptionMapRWMutex.Unlock()
			useChannelMonitorEventPublishStatsIsolation(t)
			writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{QueueCapacity: 1, MaxAttempts: 1})
			setChannelMonitorEventWriterForTest(t, writer)
			t.Cleanup(writer.cancelRun)
			ctx, _ := gin.CreateTestContext(nil)
			apiErr := relaytypes.NewErrorWithStatusCode(errors.New("upstream unavailable"), relaytypes.ErrorCodeBadResponse, test.statusCode)
			status := EmitChannelMonitorFailureEvent(
				ctx, 21, "model-a", apiErr, test.retry, !test.retry, test.finalRetrySummary, true, true, nil,
			)
			require.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
			require.Len(t, writer.queue, 1)
			event := (<-writer.queue).event
			assert.Equal(t, model.ChannelMonitorEventOutcomeFailure, event.Outcome)
			assert.Equal(t, test.eligible, event.SchedulingEligible)
			assert.Equal(t, test.eligible, event.RuntimeProtectionEligible)
			require.NotNil(t, event.StatusCode)
			assert.Equal(t, test.statusCode, *event.StatusCode)
			assert.Equal(t, string(relaytypes.ErrorCodeBadResponse), event.ErrorCode)

			server, client := useChannelMonitorRedisConsumerTestClient(t)
			server.SetTime(time.Unix(event.OccurredAt, 0))
			addChannelMonitorRedisConsumerTestEvent(t, client, event)
			var runtimeEvents []model.ChannelMonitorEvent
			aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(client,
				func(_ context.Context, events []model.ChannelMonitorEvent) error {
					runtimeEvents = append(runtimeEvents, events...)
					return nil
				}, nil)
			require.NoError(t, err)
			consumer := newChannelMonitorRedisConsumerForTest(t, client, "whitelist",
				aggregator.HandleChannelMonitorEvents, channelMonitorRedisConsumerTestConfig())
			processed, acquired, err := consumer.consumeOnce(context.Background())
			require.NoError(t, err)
			require.True(t, acquired)
			assert.Equal(t, 1, processed)
			if test.eligible {
				assert.Len(t, runtimeEvents, 1)
			} else {
				assert.Empty(t, runtimeEvents)
			}
			shared := NewChannelMonitorRedisSharedProjectionWithClient(client)
			view, err := shared.Query(context.Background(), event.OccurredAt-60, event.OccurredAt+60)
			require.NoError(t, err)
			assert.Equal(t, test.actualFailures, view.Summary.ActualFailureCount)
			assert.Equal(t, test.finalFailures, view.Summary.FinalFailureCount)
			assert.Zero(t, view.Summary.ActualSuccessCount)
			assert.Zero(t, view.Summary.FinalSuccessCount)
			route, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
			require.NoError(t, err)
			window, available, err := route.GetRouteHealthWindow(context.Background(), event.ChannelId, event.ModelName)
			require.NoError(t, err)
			require.True(t, available)
			if test.eligible {
				assert.Equal(t, int64(1), window.Snapshot.ActualFailureCount)
			} else {
				assert.Zero(t, window.Snapshot.EventCount)
				assert.Zero(t, window.Snapshot.ActualFailureCount)
			}
		})
	}
}
