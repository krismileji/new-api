package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

func recordChannelMonitorRedisFault(
	client *redis.Client,
	countField string,
	activeField string,
	amount int64,
) {
	if client == nil || amount <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelMonitorRedisConsumerOperationTimeout)
	defer cancel()
	if err := client.Eval(ctx, channelMonitorDiagnosticsScript,
		[]string{ChannelMonitorRedisObservabilityKey, channelMonitorDiagnosticsTodayKey},
		"increment", countField, amount, activeField,
	).Err(); err != nil {
		common.SysError("记录渠道监控 Redis 故障状态失败: " + err.Error())
	}
}

func clearChannelMonitorRedisFault(client *redis.Client, activeField string) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.HSet(
		ctx,
		ChannelMonitorRedisObservabilityKey,
		activeField,
		0,
	).Err(); err != nil {
		common.SysError("清除渠道监控 Redis 故障状态失败: " + err.Error())
	}
}
