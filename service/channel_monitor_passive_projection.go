package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

const channelPassivePrefix = "channel_monitor:v1:passive:"
const channelPassiveRetention = int64(32 * 86400)

const channelPassiveWriteScript = `
local payload = cjson.decode(ARGV[1])
local attempt = redis.call('EXISTS', KEYS[3]) == 0
local final = redis.call('EXISTS', KEYS[4]) == 0
for _, bucket in ipairs({KEYS[1], KEYS[2]}) do
  local delta = {}
  if attempt then for k,v in pairs(payload.attempt) do delta[k] = v end end
  if final then for k,v in pairs(payload.final) do delta[k] = (delta[k] or 0) + v end end
  local valid = true
  for field,value in pairs(delta) do
    local current = tonumber(redis.call('HGET', bucket, field) or '0')
    if value < 0 or value > 9007199254740991-current then valid = false end
  end
  if valid then
    for field,value in pairs(delta) do redis.call('HINCRBYFLOAT', bucket, field, value) end
  else redis.call('HSET', bucket, 'invalid', '1') end
  redis.call('HSET', bucket, 'processed_at', ARGV[2])
  if attempt or final then redis.call('HINCRBY', bucket, 'version', 1) end
end
redis.call('EXPIREAT', KEYS[1], ARGV[3])
redis.call('EXPIREAT', KEYS[2], ARGV[4])
redis.call('SET', KEYS[3], '1')
redis.call('EXPIREAT', KEYS[3], ARGV[3])
redis.call('SET', KEYS[4], '1')
redis.call('EXPIREAT', KEYS[4], ARGV[3])
return 1`

// The Lua operation is independent of the shared projection's markers, so a
// crash between projections can safely replay without missing or doubling data.
func projectChannelPassiveEvents(ctx context.Context, client *redis.Client, events []model.ChannelMonitorEvent, now int64) error {
	if client == nil {
		return ErrChannelMonitorEventRedisUnavailable
	}
	registered := make(map[string]bool)
	for _, event := range events {
		if event.Source != model.ChannelMonitorEventSourceBusiness && event.Source != model.ChannelMonitorEventSourceLocalResponse {
			continue
		}
		if !event.PassiveConfigReady {
			if err := markChannelPassiveCoverageGap(ctx, client, event.OccurredAt, now); err != nil {
				return err
			}
			continue
		}
		for _, target := range event.PassiveTargets {
			if target.IntervalSeconds < 30 || target.IntervalSeconds > 86400 || target.ID != channelPassiveTargetID(target) {
				continue
			}
			start := event.OccurredAt - event.OccurredAt%int64(target.IntervalSeconds)
			end := start + int64(target.IntervalSeconds)
			expires := end + 48*3600
			if event.OccurredAt < target.EffectiveAt || now >= expires || event.OccurredAt > now+5 {
				if err := markChannelPassiveCoverageGap(ctx, client, event.OccurredAt, now); err != nil {
					return err
				}
				continue
			}
			attempt := map[string]float64{}
			final := map[string]float64{}
			local := event.Source == model.ChannelMonitorEventSourceLocalResponse
			if local {
				final["local"] = 1
			} else if (event.RequestDispatched || event.FinalRetrySummary) && event.Outcome != model.ChannelMonitorEventOutcomeCanceled {
				if !event.FinalRetrySummary {
					if target.Scope != "group_final" {
						if event.Outcome == model.ChannelMonitorEventOutcomeSuccess {
							attempt["success"] = 1
						} else {
							attempt["failure"] = 1
						}
					}
					if event.FirstTokenMs != nil && *event.FirstTokenMs >= 0 && !math.IsInf(*event.FirstTokenMs, 0) && !math.IsNaN(*event.FirstTokenMs) {
						attempt["first_sum"], attempt["first_n"] = *event.FirstTokenMs, 1
					}
					if event.AttemptDurationMs != nil && *event.AttemptDurationMs >= 0 {
						attempt["duration_sum"], attempt["duration_n"] = float64(*event.AttemptDurationMs), 1
					}
					if output, duration, ok := event.TPSMeasurement(); ok {
						attempt["output"], attempt["generation_ms"], attempt["tps_n"] = float64(output), float64(duration), 1
					}
				}
				if event.IsFinalAttempt && target.Scope == "group_final" {
					if event.Outcome == model.ChannelMonitorEventOutcomeSuccess {
						final["success"] = 1
					} else {
						final["failure"] = 1
					}
				}
			}
			if len(attempt)+len(final) == 0 {
				continue
			}
			attemptID := channelMonitorRealtimeCostEventId(event)
			if attemptID == "" {
				attemptID = event.EventId
			}
			finalID := event.RequestId
			if finalID == "" {
				finalID = event.EventId
			}
			// Non-final events must not reserve the request's final-result marker.
			if len(final) == 0 {
				finalID = "attempt-only:" + event.EventId
			}
			attemptHash := sha256.Sum256([]byte(attemptID))
			finalHash := sha256.Sum256([]byte(finalID))
			base := channelPassivePrefix + target.ID + ":"
			hour := (end - 1) / 3600 * 3600
			keys := []string{base + "period:" + strconv.FormatInt(start, 10), base + "hour:" + strconv.FormatInt(hour, 10), base + "attempt:" + hex.EncodeToString(attemptHash[:]), base + "final:" + hex.EncodeToString(finalHash[:])}
			payload, err := common.Marshal(map[string]any{"attempt": attempt, "final": final})
			if err != nil {
				return err
			}
			if err := client.Eval(ctx, channelPassiveWriteScript, keys, string(payload), now, expires, hour+3600+channelPassiveRetention).Err(); err != nil {
				return err
			}
			if !registered[target.ID] {
				if err := registerChannelPassiveTarget(ctx, client, target, now); err != nil {
					return err
				}
				registered[target.ID] = true
			}
		}
	}
	return nil
}

// Invalidate the event's original second as well as the live watermark. A late
// event must not leave a historical hour looking completely observed.
func markChannelPassiveCoverageGap(ctx context.Context, client *redis.Client, occurredAt, now int64) error {
	pipe := client.TxPipeline()
	pipe.Set(ctx, channelPassivePrefix+"config_gap", now, 30*time.Second)
	if occurredAt > now-channelPassiveRetention && occurredAt <= now {
		day := occurredAt / 86400 * 86400
		key := fmt.Sprintf("%scoverage:%d", channelPassivePrefix, day)
		pipe.SetBit(ctx, key, occurredAt-day, 0)
		pipe.ExpireAt(ctx, key, time.Unix(day+86400+channelPassiveRetention, 0))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func registerChannelPassiveTarget(ctx context.Context, client *redis.Client, target model.ChannelPassiveTarget, now int64) error {
	pipe := client.TxPipeline()
	if err := queueChannelPassiveTargetRegistration(ctx, pipe, target, now); err != nil {
		return err
	}
	_, err := pipe.Exec(ctx)
	return err
}

func queueChannelPassiveTargetRegistration(ctx context.Context, pipe redis.Pipeliner, target model.ChannelPassiveTarget, now int64) error {
	data, err := common.Marshal(target)
	if err != nil {
		return err
	}
	pipe.SetNX(ctx, channelPassivePrefix+"meta:"+target.ID, string(data), time.Duration(channelPassiveRetention)*time.Second)
	pipe.Expire(ctx, channelPassivePrefix+"meta:"+target.ID, time.Duration(channelPassiveRetention)*time.Second)
	pipe.SetNX(ctx, channelPassivePrefix+"since:"+target.ID, now, time.Duration(channelPassiveRetention)*time.Second)
	pipe.Expire(ctx, channelPassivePrefix+"since:"+target.ID, time.Duration(channelPassiveRetention)*time.Second)
	key := channelPassivePrefix + "index:" + channelPassiveSubject(target)
	pipe.ZAdd(ctx, key, &redis.Z{Score: float64(now), Member: target.ID})
	pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(now-channelPassiveRetention, 10))
	pipe.Expire(ctx, key, time.Duration(channelPassiveRetention)*time.Second)
	return nil
}

// Coverage is recorded even with no traffic. Missing seconds remain unknown;
// startup, outage, loss and config-cache failure never create false empty data.
func runChannelPassiveMonitor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var previous int64
	var previousHealthy bool
	var losses int64
	registered := make(map[string]int64)
	node := common.GetUUID()
	for {
		opCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		err := RefreshChannelPassiveTargets(opCtx)
		client := common.RedisMonitorConsumerClient()
		now := time.Now().Unix()
		if err == nil && client != nil {
			snapshot := channelPassiveTargets.Load()
			active := make(map[string]int64, len(snapshot.Targets))
			activePipe := client.Pipeline()
			for _, target := range snapshot.Targets {
				activePipe.Set(opCtx, channelPassivePrefix+"until:"+target.ID, now, time.Duration(channelPassiveRetention)*time.Second)
				lastRegistered := registered[target.ID]
				if lastRegistered == 0 || now-lastRegistered >= 3600 {
					if registerErr := queueChannelPassiveTargetRegistration(opCtx, activePipe, target, now); registerErr != nil {
						err = registerErr
						break
					}
					lastRegistered = now
				}
				active[target.ID] = lastRegistered
			}
			for id := range registered {
				if _, exists := active[id]; !exists {
					activePipe.Set(opCtx, channelPassivePrefix+"until:"+id, now-15, time.Duration(channelPassiveRetention)*time.Second)
				}
			}
			if _, activeErr := activePipe.Exec(opCtx); activeErr != nil {
				err = activeErr
			}
			registered = active
			status := getChannelMonitorRedisRealtimeStatus(opCtx, client, time.Unix(now, 0))
			gap, gapErr := client.Exists(opCtx, channelPassivePrefix+"config_gap").Result()
			currentLosses := status.WriterDroppedEvents + status.QuarantineCount
			healthy := err == nil && gapErr == nil && gap == 0 && status.RedisAvailable && status.RedisConsumerRunning && !status.RealtimeDegraded && status.PendingCount == 0 && status.WriterQueueDepth == 0 && currentLosses == losses
			// Every upgraded request node advertises its queue/config health. Only
			// one lease holder certifies coverage, and it requires all recently
			// registered producers to be healthy and current.
			producerKey := channelPassivePrefix + "producer:" + node
			producerData, _ := common.Marshal(struct {
				At      int64 `json:"at"`
				Healthy bool  `json:"healthy"`
			}{now, healthy})
			producerPipe := client.TxPipeline()
			producerPipe.Set(opCtx, producerKey, string(producerData), 2*time.Minute)
			producerPipe.ZAdd(opCtx, channelPassivePrefix+"producers", &redis.Z{Score: float64(now), Member: node})
			producerPipe.ZRemRangeByScore(opCtx, channelPassivePrefix+"producers", "-inf", strconv.FormatInt(now-120, 10))
			producerPipe.Expire(opCtx, channelPassivePrefix+"producers", 3*time.Minute)
			_, producerErr := producerPipe.Exec(opCtx)
			lease, leaseErr := client.Eval(opCtx, `if redis.call('GET',KEYS[1]) == ARGV[1] or redis.call('EXISTS',KEYS[1]) == 0 then redis.call('SET',KEYS[1],ARGV[1],'EX',15); return 1 end; return 0`, []string{channelPassivePrefix + "coverage_owner"}, node).Int()
			healthy = healthy && producerErr == nil && leaseErr == nil && lease == 1
			if healthy {
				producers, readErr := client.ZRange(opCtx, channelPassivePrefix+"producers", 0, 1000).Result()
				if readErr != nil || len(producers) > 1000 {
					healthy = false
				} else {
					keys := make([]string, 0, len(producers))
					for _, producer := range producers {
						keys = append(keys, channelPassivePrefix+"producer:"+producer)
					}
					values, readErr := client.MGet(opCtx, keys...).Result()
					if readErr != nil {
						healthy = false
					} else {
						for _, value := range values {
							var state struct {
								At      int64 `json:"at"`
								Healthy bool  `json:"healthy"`
							}
							raw, _ := value.(string)
							if common.UnmarshalJsonStr(raw, &state) != nil || !state.Healthy || now-state.At > 10 {
								healthy = false
								break
							}
						}
					}
				}
			}
			// Allow writer/consumer queues to settle before certifying seconds.
			cutoff := now - 10
			if healthy && previousHealthy && cutoff > previous && cutoff-previous <= 10 {
				pipe := client.TxPipeline()
				for second := previous; second < cutoff; second++ {
					day := second / 86400 * 86400
					key := fmt.Sprintf("%scoverage:%d", channelPassivePrefix, day)
					pipe.SetBit(opCtx, key, second-day, 1)
					pipe.ExpireAt(opCtx, key, time.Unix(day+86400+channelPassiveRetention, 0))
				}
				_, err = pipe.Exec(opCtx)
			}
			previous, previousHealthy, losses = cutoff, healthy && err == nil, currentLosses
		} else {
			previousHealthy = false
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
