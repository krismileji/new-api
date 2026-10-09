package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func useChannelSmartScheduleTrafficPolicy(t *testing.T, enabled bool, policies string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled, hadEnabled := common.OptionMap[channelMonitorSmartScheduleEnabledOption]
	originalPolicies, hadPolicies := common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption]
	common.OptionMap[channelMonitorSmartScheduleEnabledOption] = map[bool]string{true: "true", false: "false"}[enabled]
	common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption] = policies
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if hadEnabled {
			common.OptionMap[channelMonitorSmartScheduleEnabledOption] = originalEnabled
		} else {
			delete(common.OptionMap, channelMonitorSmartScheduleEnabledOption)
		}
		if hadPolicies {
			common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption] = originalPolicies
		} else {
			delete(common.OptionMap, channelMonitorSmartScheduleGroupPoliciesOption)
		}
		common.OptionMapRWMutex.Unlock()
		channelSmartScheduleTrafficPolicyCache.Store(nil)
	})
	channelSmartScheduleTrafficPolicyCache.Store(nil)
}

func useChannelSmartScheduleScoreRetention(t *testing.T, minutes string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previous := make(map[string]string)
	for key, value := range map[string]string{
		ChannelMonitorSmartScheduleRealtimeRetentionOption: minutes,
		ChannelMonitorSmartSchedulePerformanceWindowOption: "5",
	} {
		if old, ok := common.OptionMap[key]; ok {
			previous[key] = old
		}
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		for _, key := range []string{ChannelMonitorSmartScheduleRealtimeRetentionOption, ChannelMonitorSmartSchedulePerformanceWindowOption} {
			if value, ok := previous[key]; ok {
				common.OptionMap[key] = value
			} else {
				delete(common.OptionMap, key)
			}
		}
	})
}

func TestChannelSmartScheduleTrafficPolicyDatabaseSelectionScopesParticipationToManagedPools(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)

	highPriority := int64(100)
	participatingPriority := int64(10)
	rejectedPriority := int64(200)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5201, Name: "未参与高优先级", Status: common.ChannelStatusEnabled},
		{Id: 5202, Name: "参与低优先级", Status: common.ChannelStatusEnabled},
		{Id: 5203, Name: "已取消参与", Status: common.ChannelStatusEnabled},
		{Id: 5204, Name: "未配置分组", Status: common.ChannelStatusEnabled},
		{Id: 5205, Name: "策略外模型", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5201, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 1000},
		{ChannelId: 5202, Group: "vip", Model: "model-a", Enabled: true, Priority: &participatingPriority, Weight: 100},
		{ChannelId: 5203, Group: "vip", Model: "model-a", Enabled: true, Priority: &rejectedPriority, Weight: 1000},
		{ChannelId: 5204, Group: "unconfigured", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 1000},
		{ChannelId: 5205, Group: "vip", Model: "model-b", Enabled: true, Priority: &highPriority, Weight: 1000},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 5202, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 5203, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5202, channel.Id)

	channel, err = GetRandomSatisfiedChannel("unconfigured", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5204, channel.Id)

	channel, err = GetRandomSatisfiedChannel("vip", "model-b", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5205, channel.Id)
}

func TestChannelSmartScheduleManagedPoolIgnoresChannelDefaultRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
	defaultPriority := int64(1000)
	defaultWeight := uint(1000)
	lowDefaultPriority := int64(1)
	lowDefaultWeight := uint(1)
	abilityPriority := int64(0)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5221, Name: "高默认值", Status: common.ChannelStatusEnabled, Priority: &defaultPriority, Weight: &defaultWeight},
		{Id: 5222, Name: "智能调度值", Status: common.ChannelStatusEnabled, Priority: &lowDefaultPriority, Weight: &lowDefaultWeight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5221, Group: "vip", Model: "model-a", Enabled: true},
		{ChannelId: 5222, Group: "vip", Model: "model-a", Enabled: true, Priority: &abilityPriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 5221, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 5222, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5222, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicyCacheSelectionFailsClosedAndFallsBackToEligibleWildcard(t *testing.T) {
	setupChannelSmartScheduleRouteTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const wildcardModel = "gemini-2.5-pro-thinking-*"
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["gemini-2.5-pro-thinking-2048","gemini-2.5-pro-thinking-*"]}]`)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupCache := group2model2channels
	originalChannelCache := channelsIDM
	originalAdvancedCustomCache := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	originalLogicalRuntime := logicalChannelRuntimeCache
	originalLogicalDirty := logicalChannelRuntimeDirty
	logicalChannelRuntimeCache = nil
	logicalChannelRuntimeDirty = false
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupCache
		channelsIDM = originalChannelCache
		channel2advancedCustomConfig = originalAdvancedCustomCache
		channelSmartScheduleRouteCache = originalRouteCache
		logicalChannelRuntimeCache = originalLogicalRuntime
		logicalChannelRuntimeDirty = originalLogicalDirty
		channelSyncLock.Unlock()
	})

	highPriority := int64(100)
	channelsIDM = map[int]*Channel{
		5211: {Id: 5211, Name: "未参与精确模型", Status: common.ChannelStatusEnabled},
		5212: {Id: 5212, Name: "参与通配模型", Status: common.ChannelStatusEnabled},
		5213: {Id: 5213, Name: "未配置分组", Status: common.ChannelStatusEnabled},
		5214: {Id: 5214, Name: "策略外模型", Status: common.ChannelStatusEnabled},
	}
	group2model2channels = map[string]map[string][]int{
		"vip":          {exactModel: {5211}, wildcardModel: {5212}, "model-b": {5214}},
		"unconfigured": {"model-a": {5213}},
	}
	channel2advancedCustomConfig = nil
	channelSmartScheduleRouteCache = buildChannelSmartScheduleRouteCacheFromStates(
		[]*Ability{
			{ChannelId: 5211, Group: "vip", Model: exactModel, Enabled: true, Priority: &highPriority, Weight: 1000},
			{ChannelId: 5212, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &highPriority, Weight: 100},
			{ChannelId: 5213, Group: "unconfigured", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
			{ChannelId: 5214, Group: "vip", Model: "model-b", Enabled: true, Priority: &highPriority, Weight: 100},
		},
		channelsIDM,
		[]ChannelSmartScheduleRouteState{
			{ChannelId: 5212, GroupName: "vip", ModelName: wildcardModel, ParticipationSet: true},
		},
	)

	channel, err := GetRandomSatisfiedChannel("vip", exactModel, 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5212, channel.Id)

	channel, err = GetRandomSatisfiedChannel("unconfigured", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5213, channel.Id)

	channel, err = GetRandomSatisfiedChannel("vip", "model-b", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5214, channel.Id)

	channelSmartScheduleRouteCache = nil
	channel, err = GetRandomSatisfiedChannel("vip", exactModel, 0, nil)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotUnavailable)
	assert.Nil(t, channel)

	channel, err = GetRandomSatisfiedChannel("unconfigured", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5213, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicyDisabledRestoresOfficialCandidates(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, false, `[]`)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 5221, Name: "官方候选", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 5221, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100,
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5221, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicyInvalidConfigFailsClosedToParticipatingRoutes(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, true, `{`)
	priority := int64(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5226, Name: "未参与渠道", Status: common.ChannelStatusEnabled},
		{Id: 5227, Name: "已有参与状态渠道", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5226, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 1000},
		{ChannelId: 5227, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 5227, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5227, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicySelectionUsesOnlyDegradedRouteAsFallback(t *testing.T) {
	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "cache"}[memoryCacheEnabled], func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
			originalMemoryCacheEnabled := common.MemoryCacheEnabled
			channelSyncLock.Lock()
			originalGroupCache := group2model2channels
			originalChannelCache := channelsIDM
			originalAdvancedCustomCache := channel2advancedCustomConfig
			originalRouteCache := channelSmartScheduleRouteCache
			channelSyncLock.Unlock()
			common.MemoryCacheEnabled = memoryCacheEnabled
			t.Cleanup(func() {
				common.MemoryCacheEnabled = originalMemoryCacheEnabled
				channelSyncLock.Lock()
				group2model2channels = originalGroupCache
				channelsIDM = originalChannelCache
				channel2advancedCustomConfig = originalAdvancedCustomCache
				channelSmartScheduleRouteCache = originalRouteCache
				channelSyncLock.Unlock()
			})

			degradedPriority := int64(0)
			require.NoError(t, db.Create(&Channel{
				Id: 5222, Name: "稳定性降级渠道", Status: common.ChannelStatusEnabled,
				Group: "vip", Models: "model-a",
			}).Error)
			require.NoError(t, db.Create(&Ability{
				ChannelId: 5222, Group: "vip", Model: "model-a", Enabled: true,
				Priority: &degradedPriority, Weight: 0,
			}).Error)
			require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
				ChannelId: 5222, GroupName: "vip", ModelName: "model-a",
				ParticipationSet: true, StabilityState: ChannelSmartScheduleStabilityDegraded,
			}).Error)
			if memoryCacheEnabled {
				InitChannelCache()
			}

			assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
				ChannelSmartScheduleAffinityEligibility("vip", "model-a", 5222, ""))

			channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 5222, channel.Id)

			channel, err = GetRandomSatisfiedChannel("vip", "model-a", 1, nil)
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 5222, channel.Id)
		})
	}
}

func reloadChannelSmartScheduleFallbackSnapshot(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	version := GetChannelSmartScheduleRouteSnapshotStatus().Revision
	raw, err := common.RedisMonitorReadClient().Get(ctx, channelSmartScheduleRouteSnapshotVersionKey(version)).Bytes()
	require.NoError(t, err)
	snapshot, err := unmarshalChannelSmartScheduleRouteSnapshot(raw)
	require.NoError(t, err)
	channelSmartScheduleRouteCache = nil
	channelSmartScheduleLocalSnapshotMetadataCache = nil
	// Simulate the routing payload arriving before its score companion, then
	// recover the companion without publishing a new routing version.
	require.True(t, applyChannelSmartScheduleRouteSnapshot(snapshot))
	require.NoError(t, loadChannelSmartScheduleRouteSnapshot(ctx))
}

func TestChannelSmartScheduleDegradedFallbackDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			t.Cleanup(initCol)
			db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
			useChannelSmartScheduleScoreRetention(t, "60")
			var dialector gorm.Dialector
			databaseType := common.DatabaseTypeSQLite
			switch engine {
			case "mysql":
				dsn := os.Getenv("SCHEDULE_FIX_MYSQL_DSN")
				if dsn == "" {
					t.Skip("SCHEDULE_FIX_MYSQL_DSN is not set")
				}
				dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("SCHEDULE_FIX_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("SCHEDULE_FIX_POSTGRES_DSN is not set")
				}
				dialector, databaseType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
			default:
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "fallback.db"))
			}
			var err error
			db, err = gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			DB, LOG_DB = db, db
			common.MemoryCacheEnabled = false
			common.SetDatabaseTypes(databaseType, databaseType)
			initCol()
			require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Ability{}, &ChannelRatioMonitor{},
				&ChannelSmartScheduleRouteState{}, &ChannelSmartScheduleGroupPause{}, &ChannelSmartScheduleModelSampleState{},
				&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}, &ChannelLogicalSmartScheduleRouteState{},
				&ChannelLogicalSmartScheduleSampleState{}))
			// All writes stay in the test transaction, including score preservation.
			db = db.Begin()
			require.NoError(t, db.Error)
			DB, LOG_DB = db, db
			t.Cleanup(func() { require.NoError(t, db.Rollback().Error) })
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)

			channels := []Channel{
				{Id: 5251, Name: "low", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
				{Id: 5252, Name: "best", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
				{Id: 5253, Name: "next", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
			}
			require.NoError(t, db.Create(&channels).Error)
			now := common.GetTimestamp()
			states := make([]ChannelSmartScheduleRouteState, len(channels))
			for i, channel := range channels {
				require.NoError(t, db.Create(&Ability{
					ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
					Priority: common.GetPointer(int64(80)), Weight: 100,
				}).Error)
				states[i] = ChannelSmartScheduleRouteState{
					ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
				}
				require.NoError(t, db.Create(&states[i]).Error)
				_, err = ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
					ChannelId: channel.Id, Group: "vip", Model: "model-a", Status: ChannelSmartScheduleStatusSucceeded,
					Score: common.GetPointer([]float64{0.2, 0.8, 0.5}[i]), Time: now - 120,
				}})
				require.NoError(t, err)
				require.NoError(t, db.First(&states[i], states[i].Id).Error)
				assert.Equal(t, now-120, states[i].LastScheduleScoreAt)
			}
			revision, err := GetChannelSmartScheduleControlRevision()
			require.NoError(t, err)
			for _, channel := range channels {
				result, err := ProtectChannelSmartScheduleRouteOnShortTermFailure(channel.Id, "vip", "model-a", now+300, "连续失败", revision)
				require.NoError(t, err)
				require.True(t, result.Handled)
			}
			// A schedule skipped during degradation must not erase the usable score.
			_, err = ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
				ChannelId: channels[1].Id, Group: "vip", Model: "model-a", Status: ChannelSmartScheduleStatusSkipped,
			}})
			require.NoError(t, err)
			var protected ChannelSmartScheduleRouteState
			require.NoError(t, db.Where("channel_id = ?", channels[1].Id).First(&protected).Error)
			require.NotNil(t, protected.LastScheduleScore)
			assert.Equal(t, 0.8, *protected.LastScheduleScore)
			assert.Equal(t, now-120, protected.LastScheduleScoreAt)

			for _, mode := range []string{"database", "cache", "redis"} {
				t.Run(mode, func(t *testing.T) {
					common.MemoryCacheEnabled = mode != "database"
					if common.MemoryCacheEnabled {
						InitChannelCache()
					}
					if mode == "redis" {
						require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
						reloadChannelSmartScheduleFallbackSnapshot(t)
					}
					for _, tc := range []struct {
						name     string
						excluded []int
						want     int
					}{
						{"first request", nil, channels[1].Id},
						{"failed best", []int{channels[1].Id}, channels[2].Id},
						{"last candidate", []int{channels[1].Id, channels[2].Id}, channels[0].Id},
						{"all failed", []int{channels[0].Id, channels[1].Id, channels[2].Id}, 0},
					} {
						t.Run(tc.name, func(t *testing.T) {
							selected, err := GetRandomSatisfiedChannel("vip", "model-a", len(tc.excluded), nil,
								ChannelSelectionOptions{ExcludedChannelIds: tc.excluded})
							require.NoError(t, err)
							if tc.want == 0 {
								assert.Nil(t, selected)
								return
							}
							require.NotNil(t, selected)
							assert.Equal(t, tc.want, selected.Id)
						})
					}
					rows, err := GetChannelSmartScheduleRouteSummariesWithContext(context.Background())
					require.NoError(t, err)
					_, views, _, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(context.Background(), rows)
					require.NoError(t, err)
					for i, channel := range channels {
						view := views[channelSmartScheduleRouteKey(channel.Id, "vip", "model-a")]
						assert.Equal(t, i == 1, view.DegradedFallback, "monitoring must identify the request's fallback")
						assert.Equal(t, ChannelSmartScheduleStabilityDegraded, view.State.StabilityState)
					}
				})
			}
			var afterFallback ChannelSmartScheduleRouteState
			require.NoError(t, db.Where("channel_id = ?", channels[1].Id).First(&afterFallback).Error)
			assert.Equal(t, protected, afterFallback, "fallback requests must not clear protection")

			t.Run("probe lifecycle retains fallback score", func(t *testing.T) {
				common.MemoryCacheEnabled = false
				released, err := AdvanceExpiredChannelSmartScheduleDegradedRoutes(now+301, revision, []ChannelSmartScheduleStabilityReleasePool{{
					Group: "vip", Model: "model-a", StabilityReleaseMaxPromptTokens: 4096,
				}})
				require.NoError(t, err)
				require.Len(t, released.Released, 3)
				var probing ChannelSmartScheduleRouteState
				require.NoError(t, db.Where("channel_id = ?", channels[1].Id).First(&probing).Error)
				assert.Equal(t, ChannelSmartScheduleStabilityProbing, probing.StabilityState)
				require.NotNil(t, probing.LastScheduleScore)
				assert.Equal(t, 0.8, *probing.LastScheduleScore)
				assert.Equal(t, now-120, probing.LastScheduleScoreAt)
				recovery := &ChannelSmartScheduleProbeRecoveryRequest{
					ExpectedControlRevision: revision,
					Routes: []ChannelSmartScheduleProbeRecoveryRoute{{
						Group: "vip", Model: "model-a", RecoverySuccessThreshold: 2, CooldownUntil: now + 600,
					}},
				}
				_, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
					ChannelId: channels[1].Id, Model: "model-a", Source: ChannelSmartScheduleSampleSourceScheduledProbe,
					SampleId: "fallback-probe-failed", WindowStart: now, Time: now + 302, Success: false, ProbeRecovery: recovery,
				})
				require.NoError(t, err)
				require.Len(t, recovery.Result.Renewed, 1)
				var renewed ChannelSmartScheduleRouteState
				require.NoError(t, db.Where("channel_id = ?", channels[1].Id).First(&renewed).Error)
				assert.Equal(t, ChannelSmartScheduleStabilityDegraded, renewed.StabilityState)
				require.NotNil(t, renewed.LastScheduleScore)
				assert.Equal(t, 0.8, *renewed.LastScheduleScore)
				assert.Equal(t, now-120, renewed.LastScheduleScoreAt)
				_, err = ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
					ChannelId: channels[1].Id, Group: "vip", Model: "model-a", Status: ChannelSmartScheduleStatusSucceeded,
					Score: common.GetPointer(0.7), Time: now,
				}})
				require.NoError(t, err)
				require.NoError(t, db.First(&renewed, renewed.Id).Error)
				assert.Equal(t, now, renewed.LastScheduleScoreAt, "a new score starts its own lifetime")
				assert.Equal(t, 0.7, *renewed.LastScheduleScore)
			})

			for _, tc := range []struct {
				name      string
				scores    [3]*float64
				rolling   [3]*float64
				scoreAt   *[3]int64
				rollingAt *[3]int64
				stability [3]string
				blocked   string
				cooling   []int
				want      int
			}{
				{name: "healthy beats higher degraded score", scores: [3]*float64{common.GetPointer(0.1), common.GetPointer(0.9), common.GetPointer(0.6)},
					stability: [3]string{"", "degraded", "degraded"}, want: 0},
				{name: "healthy cooling prefers uncooled degraded", scores: [3]*float64{common.GetPointer(0.1), common.GetPointer(0.9), common.GetPointer(0.6)},
					stability: [3]string{"", "degraded", "degraded"}, cooling: []int{channels[0].Id}, want: 1},
				{name: "zero is a valid score", scores: [3]*float64{nil, common.GetPointer(0.0), nil}, want: 1},
				{name: "missing totals use rolling score", rolling: [3]*float64{common.GetPointer(0.9), common.GetPointer(0.1), common.GetPointer(0.5)}, want: 0},
				{name: "invalid total uses rolling score", scores: [3]*float64{common.GetPointer(-1.0), common.GetPointer(0.6), nil},
					rolling: [3]*float64{common.GetPointer(0.7), nil, common.GetPointer(2.0)}, want: 0},
				{name: "expired total loses to fresh score", scores: [3]*float64{common.GetPointer(0.9), common.GetPointer(0.2), nil},
					scoreAt: &[3]int64{now - 3601, now, 0}, want: 1},
				{name: "expired total uses fresh rolling score", scores: [3]*float64{common.GetPointer(0.9), common.GetPointer(0.2), nil},
					scoreAt: &[3]int64{now - 3601, now, 0}, rolling: [3]*float64{common.GetPointer(0.7), nil, nil}, want: 0},
				{name: "expired rolling score is ignored", rolling: [3]*float64{common.GetPointer(0.9), common.GetPointer(0.2), nil},
					rollingAt: &[3]int64{now - 3601, now, 0}, want: 1},
				{name: "legacy and future timestamps are ignored", scores: [3]*float64{common.GetPointer(0.9), common.GetPointer(0.2), common.GetPointer(0.8)},
					scoreAt: &[3]int64{0, now, now + 3600}, want: 1},
				{name: "all scores expired still falls back", scores: [3]*float64{common.GetPointer(0.9), common.GetPointer(0.8), common.GetPointer(0.1)},
					scoreAt: &[3]int64{now - 3601, now - 3601, now - 3601}, want: 2},
				{name: "missing scores use saved priority", want: 2},
				{name: "tied scores use saved priority", scores: [3]*float64{common.GetPointer(0.5), common.GetPointer(0.5), common.GetPointer(0.5)}, want: 2},
				{name: "disabled winner", blocked: "disabled", want: 2},
				{name: "paused winner", blocked: "paused", want: 2},
				{name: "nonparticipating winner", blocked: "excluded", want: 2},
				{name: "disabled ability", blocked: "ability", want: 2},
				{name: "cooled winner", blocked: "cooldown", cooling: []int{channels[1].Id}, want: 2},
				{name: "all cooling still falls back", blocked: "cooldown", cooling: []int{channels[0].Id, channels[1].Id, channels[2].Id}, want: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for i, channel := range channels {
						state := states[i]
						state.StabilityState = ChannelSmartScheduleStabilityDegraded
						state.LastScheduleScore = tc.scores[i]
						state.RollingStabilityScore = tc.rolling[i]
						state.RollingStabilityUpdatedAt = now - 120
						if tc.scoreAt != nil {
							state.LastScheduleScoreAt = tc.scoreAt[i]
						}
						if tc.rollingAt != nil {
							state.RollingStabilityUpdatedAt = tc.rollingAt[i]
						}
						state.StabilitySavedPriority = int64(80 + i)
						state.StabilitySavedWeight = 100
						if tc.stability != [3]string{} {
							state.StabilityState = tc.stability[i]
						}
						if tc.blocked != "" {
							state.LastScheduleScore = states[i].LastScheduleScore
						}
						state.Excluded = i == 1 && tc.blocked == "excluded"
						require.NoError(t, db.Save(&state).Error)
						status := common.ChannelStatusEnabled
						if i == 1 && tc.blocked == "disabled" {
							status = common.ChannelStatusManuallyDisabled
						}
						require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("status", status).Error)
						require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", !(i == 1 && tc.blocked == "ability")).Error)
					}
					require.NoError(t, db.Where("channel_id = ?", channels[1].Id).Delete(&ChannelSmartScheduleGroupPause{}).Error)
					if tc.blocked == "paused" {
						require.NoError(t, db.Create(&ChannelSmartScheduleGroupPause{
							ChannelId: channels[1].Id, GroupName: "vip", ModelName: "model-a", PausedUntil: now + 300,
						}).Error)
					}
					for _, mode := range []string{"database", "cache", "redis"} {
						common.MemoryCacheEnabled = mode != "database"
						if common.MemoryCacheEnabled {
							InitChannelCache()
						}
						if mode == "redis" {
							require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
							channelSmartScheduleRouteCache = nil
							channelSmartScheduleLocalSnapshotMetadataCache = nil
							require.NoError(t, loadChannelSmartScheduleRouteSnapshot(context.Background()))
						}
						selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil, ChannelSelectionOptions{ExcludedChannelIds: tc.cooling})
						require.NoError(t, err)
						if selected == nil && len(tc.cooling) > 0 {
							selected, err = GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
							require.NoError(t, err)
						}
						require.NotNil(t, selected)
						assert.Equal(t, channels[tc.want].Id, selected.Id, "mode=%s", mode)
						rows, err := GetChannelSmartScheduleRouteSummariesWithContext(context.Background())
						require.NoError(t, err)
						_, views, _, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(context.Background(), rows, map[string][]int{"model-a": tc.cooling})
						require.NoError(t, err)
						for i, channel := range channels {
							wantFallback := i == tc.want && (tc.stability == [3]string{} || tc.stability[i] == ChannelSmartScheduleStabilityDegraded)
							assert.Equal(t, wantFallback, views[channelSmartScheduleRouteKey(channel.Id, "vip", "model-a")].DegradedFallback, "mode=%s channel=%d", mode, channel.Id)
						}
					}
				})
			}

			t.Run("logical score overrides member scores", func(t *testing.T) {
				common.MemoryCacheEnabled = false
				logical := ChannelLogicalGroup{Name: fmt.Sprintf("fallback-%s", engine), Status: ChannelLogicalGroupStatusEnabled, Revision: 1}
				require.NoError(t, db.Create(&logical).Error)
				for i, channel := range channels {
					require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("status", common.ChannelStatusEnabled).Error)
					require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", true).Error)
					state := states[i]
					state.StabilityState = ChannelSmartScheduleStabilityDegraded
					state.LastScheduleScore = common.GetPointer([]float64{0.1, 0.2, 0.5}[i])
					require.NoError(t, db.Save(&state).Error)
					if i == 2 {
						continue
					}
					require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("logical_channel_id", logical.Id).Error)
					require.NoError(t, db.Create(&ChannelLogicalGroupMember{
						LogicalGroupID: logical.Id, ChannelID: channel.Id, Weight: uint(i), AddressFingerprint: strings.Repeat("a", 64),
					}).Error)
				}
				payload, err := encodeLogicalSmartScheduleRouteStateWithRouting(ChannelSmartScheduleRouteState{
					ParticipationSet: true, LastScheduleScore: common.GetPointer(0.95), LastScheduleScoreAt: now - 600,
				}, 80, 100)
				require.NoError(t, err)
				require.NoError(t, db.Create(&ChannelLogicalSmartScheduleRouteState{
					LogicalGroupID: logical.Id, LogicalRevision: 1, GroupName: "vip", ModelName: "model-a", StateJSON: payload,
				}).Error)
				result, err := ProtectChannelSmartScheduleRouteOnShortTermFailure(channels[0].Id, "vip", "model-a", now+300, "逻辑失败", revision)
				require.NoError(t, err)
				require.True(t, result.Handled)
				var shared ChannelLogicalSmartScheduleRouteState
				require.NoError(t, db.Where("logical_group_id = ?", logical.Id).First(&shared).Error)
				sharedState, err := decodeLogicalSmartScheduleRouteState(shared.StateJSON)
				require.NoError(t, err)
				assert.Equal(t, ChannelSmartScheduleStabilityDegraded, sharedState.StabilityState)
				assert.Equal(t, now-600, sharedState.LastScheduleScoreAt)
				for _, mode := range []string{"database", "cache", "redis"} {
					common.MemoryCacheEnabled = mode != "database"
					if common.MemoryCacheEnabled {
						InitChannelCache()
					}
					if mode == "redis" {
						require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
						reloadChannelSmartScheduleFallbackSnapshot(t)
					}
					selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
					require.NoError(t, err)
					require.NotNil(t, selected)
					assert.Equal(t, channels[1].Id, selected.Id, "%s must use the logical score", mode)
					rows, err := GetChannelSmartScheduleRouteSummariesWithContext(context.Background())
					require.NoError(t, err)
					_, views, _, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(context.Background(), rows)
					require.NoError(t, err)
					for i, channel := range channels {
						assert.Equal(t, i == 1, views[channelSmartScheduleRouteKey(channel.Id, "vip", "model-a")].DegradedFallback, "zero-weight logical member must not show traffic")
					}
					t.Run(mode+" retention change without cache rebuild", func(t *testing.T) {
						useChannelSmartScheduleScoreRetention(t, "5")
						selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
						require.NoError(t, err)
						require.NotNil(t, selected)
						assert.Equal(t, channels[2].Id, selected.Id, "expired logical score must not reuse fresh member scores")
						_, views, _, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(context.Background(), rows)
						require.NoError(t, err)
						for i, channel := range channels {
							assert.Equal(t, i == 2, views[channelSmartScheduleRouteKey(channel.Id, "vip", "model-a")].DegradedFallback, "monitoring must follow score expiry without cache rebuild")
						}
					})
				}
				t.Run("members retain protection before shared state exists", func(t *testing.T) {
					require.NoError(t, db.Delete(&shared).Error)
					require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
						Where("channel_id = ?", channels[0].Id).Update("stability_state", "").Error)
					for _, mode := range []string{"database", "cache", "redis"} {
						common.MemoryCacheEnabled = mode != "database"
						if common.MemoryCacheEnabled {
							InitChannelCache()
						}
						if mode == "redis" {
							require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
							reloadChannelSmartScheduleFallbackSnapshot(t)
						}
						selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
						require.NoError(t, err)
						require.NotNil(t, selected)
						assert.Equal(t, channels[0].Id, selected.Id, "only the healthy logical member can receive initial traffic")
						rows, err := GetChannelSmartScheduleRouteSummariesWithContext(context.Background())
						require.NoError(t, err)
						_, views, _, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(context.Background(), rows)
						require.NoError(t, err)
						for i, channel := range channels {
							view := views[channelSmartScheduleRouteKey(channel.Id, "vip", "model-a")]
							assert.False(t, view.DegradedFallback, "mode=%s", mode)
							assert.Equal(t, i != 0, view.State.StabilityState == ChannelSmartScheduleStabilityDegraded, "mode=%s channel=%d", mode, channel.Id)
						}
					}
				})
			})
		})
	}
}

func TestChannelSmartScheduleDegradedFallbackScoreExpiryBoundary(t *testing.T) {
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
	useChannelSmartScheduleScoreRetention(t, "5")
	const now int64 = 1700000000
	routes := []channelSmartScheduleCachedRoute{
		{channelId: 1, stabilityState: ChannelSmartScheduleStabilityDegraded, degradedRank: channelSmartScheduleDegradedRankFromState(ChannelSmartScheduleRouteState{
			LastScheduleScore: common.GetPointer(0.9), LastScheduleScoreAt: now - 299,
		})},
		{channelId: 2, stabilityState: ChannelSmartScheduleStabilityDegraded, degradedRank: channelSmartScheduleDegradedRankFromState(ChannelSmartScheduleRouteState{
			LastScheduleScore: common.GetPointer(0.0), LastScheduleScoreAt: now,
		})},
	}
	selected := bestChannelSmartScheduleDegradedRoute(routes, now)
	require.Len(t, selected, 1)
	assert.Equal(t, 1, selected[0].channelId)
	selected = bestChannelSmartScheduleDegradedRoute(routes, now+1)
	require.Len(t, selected, 1)
	assert.Equal(t, 2, selected[0].channelId, "score expires exactly at the retention boundary, even in the same cached routes")
}

func TestAddAbilitiesClearsNonparticipatingRouteOverride(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(70)
	channelWeight := uint(30)
	stalePriority := int64(900)
	channel := Channel{
		Id: 5231, Name: "未参与路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &stalePriority, Weight: 900,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true,
	}).Error)

	require.NoError(t, channel.AddAbilities(nil))
	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)
}
