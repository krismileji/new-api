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

func TestChannelPassiveMonitorReadMeansCoverageAndOutage(t *testing.T) {
	address := os.Getenv("TEST_PROBE_POLICY_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_PROBE_POLICY_REDIS_ADDR not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 13})
	require.NoError(t, client.Ping(t.Context()).Err())
	previousClient, previousEnabled := common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorRead, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDBMonitorRead, common.RedisEnabled = previousClient, previousEnabled
		require.NoError(t, client.Close())
	})
	now := time.Now().Unix()
	end := now/45*45 - 45
	start := end - 45
	target := model.ChannelPassiveTarget{Scope: "status", ChannelID: 97, ModelName: "read-" + common.GetUUID(), IntervalSeconds: 45, ConfigRevision: 1, EffectiveAt: start - 45}
	target.ID = channelPassiveTargetID(target)
	key := channelPassivePrefix + target.ID + ":period:" + strconv.FormatInt(start, 10)
	sinceKey, untilKey := channelPassivePrefix+"since:"+target.ID, channelPassivePrefix+"until:"+target.ID
	pipe := client.TxPipeline()
	pipe.Set(t.Context(), sinceKey, start-45, time.Hour)
	pipe.Set(t.Context(), untilKey, end, time.Hour)
	for second := start; second < end; second++ {
		day := second / 86400 * 86400
		pipe.SetBit(t.Context(), fmt.Sprintf("%scoverage:%d", channelPassivePrefix, day), second-day, 1)
	}
	pipe.HSet(t.Context(), key, map[string]any{"success": 2, "failure": 1, "first_sum": 400, "first_n": 2, "output": 100, "generation_ms": 4000, "tps_n": 2, "duration_sum": 7000, "duration_n": 2, "processed_at": now, "version": 1})
	_, err := pipe.Exec(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		client.Del(context.Background(), key, sinceKey, untilKey)
		channelPassiveLastResults.Lock()
		delete(channelPassiveLastResults.Periods, target.ID)
		channelPassiveLastResults.Unlock()
	})
	view := ChannelPassiveMonitorView{Target: target, Periods: []ChannelPassivePeriod{{PeriodStart: start, PeriodEnd: end, Resolution: "period"}}}
	views := []ChannelPassiveMonitorView{view}
	readChannelPassivePeriods(t.Context(), views, now)
	period := views[0].Periods[0]
	assert.Equal(t, "complete", period.Coverage)
	require.NotNil(t, period.AverageFirstTokenMs)
	assert.Equal(t, 200.0, *period.AverageFirstTokenMs)
	require.NotNil(t, period.AverageTPS)
	assert.Equal(t, 25.0, *period.AverageTPS)
	require.NotNil(t, period.AverageDurationMs)
	assert.Equal(t, 3500.0, *period.AverageDurationMs)
	require.NotNil(t, period.SuccessRate)
	assert.InDelta(t, 2.0/3, *period.SuccessRate, 0.00001)
	common.RedisEnabled = false
	readChannelPassivePeriods(t.Context(), views, now+45)
	assert.Equal(t, "unavailable", views[0].Periods[0].Coverage)
	assert.Equal(t, start, views[0].Periods[0].PeriodStart)
	assert.Equal(t, period.AverageTPS, views[0].Periods[0].AverageTPS)
	common.RedisEnabled = true
	require.NoError(t, client.Del(t.Context(), key).Err())
	require.NoError(t, client.HSet(t.Context(), key, "local", 100).Err())
	readChannelPassivePeriods(t.Context(), views, now)
	assert.EqualValues(t, 100, views[0].Periods[0].LocalResponses)
	assert.Zero(t, views[0].Periods[0].Success)
	assert.Nil(t, views[0].Periods[0].SuccessRate)
	assert.Nil(t, views[0].Periods[0].AverageTPS)
	require.NoError(t, markChannelPassiveCoverageGap(t.Context(), client, start+1, now))
	readChannelPassivePeriods(t.Context(), views, now)
	assert.Equal(t, "partial", views[0].Periods[0].Coverage, "late missing event invalidates original window")
	require.NoError(t, client.HSet(t.Context(), key, "first_sum", "nan", "first_n", 1).Err())
	readChannelPassivePeriods(t.Context(), views, now)
	assert.Equal(t, "partial", views[0].Periods[0].Coverage)
	_, err = common.Marshal(views)
	require.NoError(t, err, "invalid Redis floats cannot leak NaN JSON")
	old := target
	old.ConfigRevision = 2
	old.IntervalSeconds = 60
	old.ID = channelPassiveTargetID(old)
	require.NoError(t, registerChannelPassiveTarget(t.Context(), client, target, now))
	require.NoError(t, registerChannelPassiveTarget(t.Context(), client, old, now-60))
	versions, err := ReadChannelPassiveMonitorVersions(t.Context(), target.ID)
	require.NoError(t, err)
	assert.Contains(t, versions, target)
	assert.Contains(t, versions, old)
	t.Cleanup(func() {
		for _, item := range []model.ChannelPassiveTarget{target, old} {
			client.Del(context.Background(), channelPassivePrefix+"meta:"+item.ID, channelPassivePrefix+"since:"+item.ID)
			client.ZRem(context.Background(), channelPassivePrefix+"index:"+channelPassiveSubject(item), item.ID)
		}
	})
}

func TestChannelPassiveMonitorFreezesMatchingConfiguration(t *testing.T) {
	previous := channelPassiveTargets.Load()
	t.Cleanup(func() { channelPassiveTargets.Store(previous) })
	status := model.ChannelPassiveTarget{Scope: "status", ChannelID: 7, ModelName: "model-a", IntervalSeconds: 45, ConfigRevision: 1, EffectiveAt: 1}
	status.ID = channelPassiveTargetID(status)
	group := status
	group.Scope = "group_member"
	group.GroupName = "group-a"
	group.ID = channelPassiveTargetID(group)
	other := status
	other.ModelName = "model-b"
	other.ID = channelPassiveTargetID(other)
	channelPassiveTargets.Store(&channelPassiveTargetSnapshot{LoadedAt: time.Now().Unix(), ByChannel: map[int][]model.ChannelPassiveTarget{7: {status, group, other}}})
	event := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, time.Now().Unix())
	event.ModelName = "model-a"
	event.GroupName = "group-b"
	captureChannelPassiveTargets(&event)
	assert.Equal(t, []model.ChannelPassiveTarget{status}, event.PassiveTargets)
	groupEvent := event.Clone()
	groupEvent.PassiveConfigReady = false
	groupEvent.PassiveTargets = nil
	groupEvent.GroupName = "group-a"
	captureChannelPassiveTargets(&groupEvent)
	assert.Equal(t, []model.ChannelPassiveTarget{status, group}, groupEvent.PassiveTargets)
	channelPassiveTargets.Store(&channelPassiveTargetSnapshot{LoadedAt: time.Now().Unix(), ByChannel: map[int][]model.ChannelPassiveTarget{}})
	captureChannelPassiveTargets(&groupEvent)
	assert.Equal(t, []model.ChannelPassiveTarget{status, group}, groupEvent.PassiveTargets, "queued event keeps its original revision after a switch")
	manual := event.Clone()
	manual.Source = model.ChannelMonitorEventSourceManualTest
	manual.PassiveConfigReady = false
	manual.PassiveTargets = nil
	captureChannelPassiveTargets(&manual)
	assert.False(t, manual.PassiveConfigReady)
	assert.Empty(t, manual.PassiveTargets)
}
