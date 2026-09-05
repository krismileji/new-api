package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorAnalyticsT15HighCardinalityRedisReadModel(t *testing.T) {
	if os.Getenv("CHANNEL_MONITOR_T15") != "1" {
		t.Skip("设置 CHANNEL_MONITOR_T15=1 运行 50 渠道、500 用户、10000 Key 验收")
	}
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr(), PoolSize: 8})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	dayStart := model.ChannelDailyCostDayStart(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC).Unix())
	dayKey := ChannelMonitorRedisSuccessDayKey(dayStart)
	pipe := client.Pipeline()
	pipe.HSet(context.Background(), dayKey, channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricActualSuccess, "10000")
	for channelID := 1; channelID <= 50; channelID++ {
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("channel:%d:%s", channelID, channelMonitorRedisSharedMetricActualSuccess), "200")
	}
	for userID := 1; userID <= 500; userID++ {
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("user:%d:%s", userID, channelMonitorRedisSharedMetricActualSuccess), "20")
	}
	for apiKeyID := 1; apiKeyID <= 10000; apiKeyID++ {
		channelID := (apiKeyID-1)%50 + 1
		userID := (apiKeyID-1)%500 + 1
		modelKey := fmt.Sprintf("model-%02d", apiKeyID%10)
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("apikey:%d:%s", apiKeyID, channelMonitorRedisSharedMetricActualSuccess), "1")
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("apikey:%d:%s", apiKeyID, channelMonitorRedisSharedMetricAPIKeyName), fmt.Sprintf("key-%d", apiKeyID))
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("user_api_key:%d.%d:%s", userID, apiKeyID, channelMonitorRedisSharedMetricActualSuccess), "1")
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("channel_user_api_key:%d.%d.%d:%s", channelID, userID, apiKeyID, channelMonitorRedisSharedMetricActualSuccess), "1")
		pipe.HSet(context.Background(), dayKey, fmt.Sprintf("user_api_key_route:%d.%d.%d.%s:%s", userID, apiKeyID, channelID, modelKey, channelMonitorRedisSharedMetricActualSuccess), "1")
	}
	_, err := pipe.Exec(context.Background())
	require.NoError(t, err)

	startedAt := time.Now()
	view, err := queryChannelMonitorRedisDailySuccessAnalyticsWithClient(context.Background(), client, dayStart)
	require.NoError(t, err)
	t.Logf("t15 hash_fields=%d dimensions=%d read_ms=%d", client.HLen(context.Background(), dayKey).Val(), len(view.Rows), time.Since(startedAt).Milliseconds())
	assert.Equal(t, int64(10000), view.Summary.ActualSuccessCount)

	counts := map[string]int{}
	for _, row := range view.Rows {
		switch {
		case row.ChannelID > 0 && row.UserID == 0 && row.APIKeyID == 0:
			counts["channels"]++
		case row.ChannelID == 0 && row.UserID > 0 && row.APIKeyID == 0:
			counts["users"]++
		case row.ChannelID == 0 && row.UserID > 0 && row.APIKeyID > 0 && row.ModelName == "":
			counts["api_keys"]++
		case row.ChannelID > 0 && row.UserID > 0 && row.APIKeyID > 0 && row.ModelName != "":
			counts["api_key_channel_models"]++
		}
	}
	assert.Equal(t, 50, counts["channels"])
	assert.Equal(t, 500, counts["users"])
	assert.Equal(t, 10000, counts["api_keys"])
	assert.Equal(t, 10000, counts["api_key_channel_models"])
}
