package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelPassiveMonitorRedisPeriodsAndIdempotence(t *testing.T) {
	address := os.Getenv("TEST_PROBE_POLICY_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_PROBE_POLICY_REDIS_ADDR not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 13})
	require.NoError(t, client.Ping(t.Context()).Err())
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	now := time.Now().Unix()
	for _, interval := range []int{30, 45, 60, 300, 86400} {
		t.Run(strconv.Itoa(interval), func(t *testing.T) {
			start := now/int64(interval)*int64(interval) - int64(interval)
			target := model.ChannelPassiveTarget{Scope: "status", ChannelID: 91, ModelName: "gpt-4o-" + common.GetUUID(), IntervalSeconds: interval, ConfigRevision: 1, PolicyRevision: 1, EffectiveAt: start - int64(interval)}
			target.ID = channelPassiveTargetID(target)
			base := channelPassivePrefix + target.ID + ":"
			t.Cleanup(func() {
				iter := client.Scan(context.Background(), 0, base+"*", 100).Iterator()
				var keys []string
				for iter.Next(context.Background()) {
					keys = append(keys, iter.Val())
				}
				require.NoError(t, iter.Err())
				keys = append(keys, channelPassivePrefix+"meta:"+target.ID, channelPassivePrefix+"since:"+target.ID)
				if len(keys) > 0 {
					require.NoError(t, client.Del(context.Background(), keys...).Err())
				}
				client.ZRem(context.Background(), channelPassivePrefix+"index:"+channelPassiveSubject(target), target.ID)
			})
			first := model.NewChannelMonitorEvent(91, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, start+1)
			first.RequestId = "request-a"
			first.ModelName = target.ModelName
			first.RequestDispatched = true
			first.IsFinalAttempt = true
			first.PassiveConfigReady = true
			first.PassiveTargets = []model.ChannelPassiveTarget{target}
			first.FirstTokenMs = common.GetPointer(100.0)
			first.CompletionTokens = common.GetPointer(int64(10))
			first.TPS = common.GetPointer(10.0)
			first.AttemptDurationMs = common.GetPointer(int64(1100))
			first.OtherJson = `{"cost_event_id":"attempt-a"}`
			second := first.Clone()
			second.EventId = common.GetUUID()
			second.RequestId = "request-b"
			second.FirstTokenMs = common.GetPointer(300.0)
			second.CompletionTokens = common.GetPointer(int64(90))
			second.TPS = common.GetPointer(30.0)
			second.AttemptDurationMs = common.GetPointer(int64(3300))
			second.OtherJson = `{"cost_event_id":"attempt-b"}`
			local := first.Clone()
			local.EventId = common.GetUUID()
			local.Source = model.ChannelMonitorEventSourceLocalResponse
			local.RequestId = "local"
			local.RequestDispatched = false
			local.OtherJson = ""
			probe := first.Clone()
			probe.EventId = common.GetUUID()
			probe.Source = model.ChannelMonitorEventSourceManualTest
			missing := first.Clone()
			missing.EventId = common.GetUUID()
			missing.RequestId = "missing-performance"
			missing.OtherJson = `{"cost_event_id":"attempt-c"}`
			missing.FirstTokenMs = nil
			missing.TPS = nil
			missing.CompletionTokens = nil
			missing.AttemptDurationMs = nil
			boundary := first.Clone()
			boundary.EventId = common.GetUUID()
			boundary.RequestId = "boundary"
			boundary.OtherJson = `{"cost_event_id":"attempt-boundary"}`
			boundary.OccurredAt = start + int64(interval)
			events := []model.ChannelMonitorEvent{first, second, local, probe, missing, boundary}
			require.NoError(t, projectChannelPassiveEvents(t.Context(), client, events, now))
			require.NoError(t, projectChannelPassiveEvents(t.Context(), client, events, now))
			settlement := first.Clone()
			settlement.EventId = common.GetUUID()
			settlement.CostStatus = model.ChannelMonitorEventCostSettled
			require.NoError(t, projectChannelPassiveEvents(t.Context(), client, []model.ChannelMonitorEvent{settlement}, now))
			values, err := client.HGetAll(t.Context(), base+"period:"+strconv.FormatInt(start, 10)).Result()
			require.NoError(t, err)
			assert.Equal(t, "3", values["success"])
			assert.Equal(t, "1", values["local"])
			assert.Equal(t, "400", values["first_sum"])
			assert.Equal(t, "2", values["first_n"])
			assert.Equal(t, "100", values["output"])
			assert.Equal(t, "4000", values["generation_ms"])
			assert.Equal(t, "1", client.HGet(t.Context(), base+"period:"+strconv.FormatInt(start+int64(interval), 10), "success").Val())
			assert.Greater(t, client.TTL(t.Context(), base+"period:"+strconv.FormatInt(start, 10)).Val(), 48*time.Hour-time.Duration(interval)*time.Second-2*time.Second)
			assert.Greater(t, client.TTL(t.Context(), base+"hour:"+strconv.FormatInt((start+int64(interval)-1)/3600*3600, 10)).Val(), 30*24*time.Hour)
			late := first.Clone()
			late.EventId = common.GetUUID()
			late.OtherJson = `{"cost_event_id":"too-late"}`
			require.NoError(t, projectChannelPassiveEvents(t.Context(), client, []model.ChannelMonitorEvent{late}, start+int64(interval)+48*3600))
			assert.Equal(t, "3", client.HGet(t.Context(), base+"period:"+strconv.FormatInt(start, 10), "success").Val())
		})
	}
}

func TestChannelPassiveMonitorFinalRequestAndCoverage(t *testing.T) {
	address := os.Getenv("TEST_PROBE_POLICY_REDIS_ADDR")
	if address == "" {
		t.Skip("Redis not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 13})
	require.NoError(t, client.Ping(t.Context()).Err())
	defer client.Close()
	now := time.Now().Unix()
	start := now/60*60 - 60
	target := model.ChannelPassiveTarget{Scope: "group_final", GroupName: "passive-test-" + common.GetUUID(), ModelName: "gpt-4o", IntervalSeconds: 60, ConfigRevision: 1, EffectiveAt: start - 60}
	target.ID = channelPassiveTargetID(target)
	first := model.NewChannelMonitorEvent(91, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, start+1)
	first.PassiveConfigReady = true
	first.PassiveTargets = []model.ChannelPassiveTarget{target}
	first.RequestId = "retry-request"
	first.RequestDispatched = true
	first.OtherJson = `{"cost_event_id":"fail-a"}`
	second := first.Clone()
	second.EventId = common.GetUUID()
	second.ChannelId = 92
	second.Outcome = model.ChannelMonitorEventOutcomeSuccess
	second.IsFinalAttempt = true
	second.OtherJson = `{"cost_event_id":"success-b"}`
	summary := second.Clone()
	summary.EventId = common.GetUUID()
	summary.FinalRetrySummary = true
	require.NoError(t, projectChannelPassiveEvents(t.Context(), client, []model.ChannelMonitorEvent{first, second, summary}, now))
	key := channelPassivePrefix + target.ID + ":period:" + strconv.FormatInt(start, 10)
	assert.Equal(t, "1", client.HGet(t.Context(), key, "success").Val())
	assert.Empty(t, client.HGet(t.Context(), key, "failure").Val())
	coverageKey := fmt.Sprintf("%scoverage-test:%s", channelPassivePrefix, common.GetUUID())
	defer client.Del(context.Background(), coverageKey)
	for _, second := range []int64{7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17} {
		require.NoError(t, client.SetBit(t.Context(), coverageKey, second, 1).Err())
	}
	count, err := client.Eval(t.Context(), channelPassiveCoverageScript, []string{coverageKey}, 7, 18).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 11, count)
	count, err = client.Eval(t.Context(), channelPassiveCoverageScript, []string{coverageKey}, 6, 18).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 11, count, "missing second remains incomplete")
}
