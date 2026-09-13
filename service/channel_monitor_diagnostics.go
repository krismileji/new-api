package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const channelMonitorDiagnosticsTodayKey = ChannelMonitorRedisKeyPrefix + ":diagnostics:today"

var ErrChannelMonitorDiagnosticsDayChanged = errors.New("统计日期已变化，请刷新后重新重置今日计数")

// One rolling hash holds the current Beijing day's diagnostic counters. Redis
// time keeps all nodes on the same day. Lifetime health counters and active
// faults are deliberately independent of the resettable reporting window.
const channelMonitorDiagnosticsScript = `
local operation = ARGV[1]
local now = tonumber(redis.call('TIME')[1])
local day_start = math.floor((now + 28800) / 86400) * 86400 - 28800
if operation == 'reset' and tonumber(ARGV[2]) ~= day_start then
  return {'day_changed'}
end
if operation == 'increment' then
  -- Preserve health observation even if the optional daily hash is damaged.
  if ARGV[4] ~= '' then
    redis.call('HSET', KEYS[1], ARGV[4], 1)
  end
  redis.call('HINCRBY', KEYS[1], ARGV[2], ARGV[3])
end
local previous_day = tonumber(redis.call('HGET', KEYS[2], 'day_start') or '0')
if previous_day ~= day_start or operation == 'reset' then
  local counted_since = now
  if previous_day > 0 and previous_day < day_start and operation ~= 'reset' then
    counted_since = day_start
  end
  redis.call('DEL', KEYS[2])
  redis.call('HSET', KEYS[2],
    'day_start', day_start, 'counted_since', counted_since,
    'last_reset_at', operation == 'reset' and now or 0,
    'retry_count', 0, 'takeover_count', 0, 'quarantine_count', 0,
    'marker_release_failure_count', 0, 'stream_trim_failure_count', 0,
    'last_quarantined_at', 0)
end
if operation == 'increment' then
  redis.call('HINCRBY', KEYS[2], ARGV[2], ARGV[3])
  if ARGV[2] == 'quarantine_count' then
    redis.call('HSET', KEYS[2], 'last_quarantined_at', now)
  end
end
redis.call('HSET', KEYS[2], 'observed_at', now)
-- A dormant hash expires after the following day; no per-day keys accumulate.
redis.call('EXPIREAT', KEYS[2], day_start + 172800)
if operation == 'increment' then
  return 1
end
return redis.call('HGETALL', KEYS[2])
`

type ChannelMonitorDiagnostics struct {
	DayStart                  int64 `json:"day_start"`
	CountedSince              int64 `json:"counted_since"`
	LastResetAt               int64 `json:"last_reset_at"`
	ObservedAt                int64 `json:"observed_at"`
	RetryCount                int64 `json:"retry_count"`
	TakeoverCount             int64 `json:"takeover_count"`
	QuarantineCount           int64 `json:"quarantine_count"`
	LastQuarantinedAt         int64 `json:"last_quarantined_at"`
	MarkerReleaseFailureCount int64 `json:"marker_release_failure_count"`
	StreamTrimFailureCount    int64 `json:"stream_trim_failure_count"`
}

func GetChannelMonitorDiagnostics(ctx context.Context) (ChannelMonitorDiagnostics, error) {
	if !common.RedisEnabled {
		return ChannelMonitorDiagnostics{}, errors.New("今日诊断暂不可用，请检查 Redis 连接")
	}
	return queryChannelMonitorDiagnostics(ctx, common.RedisMonitorWriteClient(), 0)
}

func ResetChannelMonitorDiagnostics(ctx context.Context, dayStart int64) (ChannelMonitorDiagnostics, error) {
	if dayStart <= 0 {
		return ChannelMonitorDiagnostics{}, errors.New("请提供有效的统计日期")
	}
	if !common.RedisEnabled {
		return ChannelMonitorDiagnostics{}, errors.New("今日诊断暂不可用，请检查 Redis 连接")
	}
	return queryChannelMonitorDiagnostics(ctx, common.RedisMonitorWriteClient(), dayStart)
}

func queryChannelMonitorDiagnostics(ctx context.Context, client *redis.Client, resetDayStart int64) (ChannelMonitorDiagnostics, error) {
	if client == nil {
		return ChannelMonitorDiagnostics{}, errors.New("今日诊断暂不可用，请检查 Redis 连接")
	}
	operation := "read"
	if resetDayStart > 0 {
		operation = "reset"
	}
	ctx, cancel := context.WithTimeout(ctx, channelMonitorRedisObservabilityTimeout)
	defer cancel()
	values, err := client.Eval(ctx, channelMonitorDiagnosticsScript,
		[]string{ChannelMonitorRedisObservabilityKey, channelMonitorDiagnosticsTodayKey}, operation, resetDayStart,
	).StringSlice()
	if err != nil {
		return ChannelMonitorDiagnostics{}, err
	}
	if len(values) == 1 && values[0] == "day_changed" {
		return ChannelMonitorDiagnostics{}, ErrChannelMonitorDiagnosticsDayChanged
	}
	if len(values) == 0 || len(values)%2 != 0 {
		return ChannelMonitorDiagnostics{}, errors.New("今日诊断数据格式无效")
	}
	fields := make(map[string]int64, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		value, err := strconv.ParseInt(values[index+1], 10, 64)
		if err != nil || value < 0 {
			return ChannelMonitorDiagnostics{}, errors.New("今日诊断计数无效")
		}
		fields[values[index]] = value
	}
	return ChannelMonitorDiagnostics{
		DayStart: fields["day_start"], CountedSince: fields["counted_since"],
		LastResetAt: fields["last_reset_at"], ObservedAt: fields["observed_at"],
		RetryCount: fields["retry_count"], TakeoverCount: fields["takeover_count"],
		QuarantineCount: fields["quarantine_count"], LastQuarantinedAt: fields["last_quarantined_at"],
		MarkerReleaseFailureCount: fields["marker_release_failure_count"],
		StreamTrimFailureCount:    fields["stream_trim_failure_count"],
	}, nil
}
