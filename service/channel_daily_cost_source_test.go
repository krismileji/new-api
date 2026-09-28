package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDailyCostSystemProbeSource(t *testing.T) {
	for _, test := range []struct {
		name       string
		probe      bool
		tokenName  string
		settled    int64
		unresolved int64
		wantSource string
	}{
		{"settled smart probe retains its source", true, "智能调度探测", 1, 0, "smart_probe"},
		{"unresolved smart probe retains its source", true, "智能调度探测", 0, 1, "smart_probe"},
		{"settled model test retains its source", false, "模型测试", 1, 0, "manual_test"},
		{"unresolved model test retains its source", false, "模型测试", 0, 1, "manual_test"},
		{"business key with the probe name remains business", false, "智能调度探测", 1, 0, "business"},
		{"business key with the model test name remains business", false, "模型测试", 1, 0, "business"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "false")
			var written []model.ChannelDailyCostDelta
			resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
				MaxPending: 4, MaxBatchSize: 4, FlushInterval: time.Hour,
				DBTimeout: time.Second, MaxAttempts: 1, AutoFlush: false,
			}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
				written = append(written, deltas...)
				return nil
			})
			t.Cleanup(func() {
				resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
			})
			ctx := newChannelDailyCostTestContext()
			ctx.Set(model.ChannelMonitorSmartScheduleProbeLogKey, test.probe)
			ctx.Set("channel_test", test.wantSource != "business")
			keyID := 0
			if test.wantSource == "business" {
				keyID = 201
			}
			require.True(t, recordChannelDailyCostEvent(ctx, channelDailyCostSnapshot{
				ChannelId: 7, UserId: 31, APIKeyId: keyID, APIKeyName: test.tokenName, ModelName: "model-a",
			}, 10*test.settled, test.settled, test.unresolved))
			require.NoError(t, flushChannelDailyCostEventsForTest())
			require.Len(t, written, 1)
			assert.Equal(t, test.wantSource, written[0].SourceKind)
			assert.Equal(t, keyID, written[0].APIKeyId)
			assert.Equal(t, "model-a", written[0].ModelName)
			assert.Equal(t, 10*test.settled, written[0].CostNanoCNY)
			assert.Equal(t, test.settled, written[0].SettledDelta)
			assert.Equal(t, test.unresolved, written[0].UnresolvedDelta)
		})
	}
}
