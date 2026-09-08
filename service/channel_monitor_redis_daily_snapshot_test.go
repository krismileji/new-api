package service

import (
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dailySnapshotConcurrentUpdate struct {
	server *miniredis.Miniredis
	key    string
	once   sync.Once
}

func (hook *dailySnapshotConcurrentUpdate) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (hook *dailySnapshotConcurrentUpdate) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if cmd.Name() == "hscan" {
		hook.once.Do(func() {
			hook.server.HSet(hook.key, "global:actual_success_count", "2", "channel:7:actual_success_count", "2", "meta:revision", "2")
		})
	}
	return nil
}
func (hook *dailySnapshotConcurrentUpdate) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (hook *dailySnapshotConcurrentUpdate) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestDailySnapshotReadRetriesConcurrentUpdateInsteadOfMixingTotals(t *testing.T) {
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	key := ChannelMonitorRedisSuccessDayKey(1750000000)
	require.NoError(t, client.HSet(ctx, key, "global:actual_success_count", 1, "channel:7:actual_success_count", 1, "meta:revision", 1).Err())
	client.AddHook(&dailySnapshotConcurrentUpdate{server: server, key: key})
	values, err := readChannelMonitorRedisDailyHash(ctx, client, key, []string{"global:*", "channel:*", "meta:*"}, 20)
	require.NoError(t, err)
	assert.Equal(t, "2", values["global:actual_success_count"])
	assert.Equal(t, "2", values["channel:7:actual_success_count"])
	assert.Equal(t, "2", values["meta:revision"])
}

func TestDailySnapshotReadReportsMissingAndOversizedData(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	_, err := readChannelMonitorRedisDailyHash(ctx, client, "missing-daily", nil, 2)
	assert.ErrorIs(t, err, ErrChannelMonitorRedisSharedProjectionUnavailable)
	require.NoError(t, client.HSet(ctx, "large-daily", "channel:1:a", 1, "channel:2:a", 2, "channel:3:a", 3).Err())
	_, err = readChannelMonitorRedisDailyHash(ctx, client, "large-daily", nil, 2)
	var limitErr *ChannelMonitorRedisSharedProjectionLimitError
	require.ErrorAs(t, err, &limitErr)
	assert.Equal(t, int64(3), limitErr.Actual)
	require.NoError(t, client.HSet(ctx, "corrupt-daily", "global:settled_cost_nano_cny", "invalid").Err())
	_, err = readChannelMonitorRedisDailyHash(ctx, client, "corrupt-daily", nil, 2)
	assert.ErrorContains(t, err, "整数指标无效", "invalid cost values must not silently become zero")
}
