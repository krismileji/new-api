package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func setupMonitorRecoveryRedis(t *testing.T) *redis.Client {
	t.Helper()
	address := os.Getenv("TEST_MONITOR_REDIS_ADDR")
	if address == "" {
		address = miniredis.RunT(t).Addr()
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 15})
	require.NoError(t, client.Ping(t.Context()).Err())
	oldUser, oldRead, oldWrite, oldConsumer, oldEnabled := common.RDB, common.RDBMonitorRead, common.RDBMonitorWrite, common.RDBMonitorConsumer, common.RedisEnabled
	common.RDB, common.RDBMonitorRead, common.RDBMonitorWrite, common.RDBMonitorConsumer, common.RedisEnabled = client, client, client, client, true
	t.Cleanup(func() {
		common.RDB, common.RDBMonitorRead, common.RDBMonitorWrite, common.RDBMonitorConsumer, common.RedisEnabled = oldUser, oldRead, oldWrite, oldConsumer, oldEnabled
		_ = client.Close()
	})
	return client
}

func TestChannelMonitorHealthObservationDatabaseMatrix(t *testing.T) {
	resetChannelMonitorEventPublishStatsForTest()
	t.Cleanup(resetChannelMonitorEventPublishStatsForTest)
	previousStats := GetChannelDailyCostReliableStats()
	setCM07ChannelDailyCostReliableStats(ChannelDailyCostReliableStats{})
	t.Cleanup(func() { setCM07ChannelDailyCostReliableStats(previousStats) })
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "monitor.db"))
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
			}
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("health_%x_", time.Now().UnixNano())}})
			require.NoError(t, err)
			var version string
			if engine == "sqlite" {
				require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
			} else {
				require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
			}
			t.Logf("database version: %s", version)
			previousDB := model.DB
			model.DB = db
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorEventOutbox{}, &model.ChannelDailyCostOutbox{}))
				model.DB = previousDB
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorEventOutbox{}, &model.ChannelDailyCostOutbox{}))
			client := setupMonitorRecoveryRedis(t)
			keys := []string{ChannelMonitorRedisEventStream, ChannelMonitorRedisObservabilityKey, ChannelMonitorRedisConsumerHeartbeatKey, ChannelDailyCostRedisStream, channelMonitorReliableCostStatusKey, ChannelMonitorRedisSuccessDayKey(model.ChannelDailyCostDayStart(time.Now().Unix()))}
			require.NoError(t, client.Del(t.Context(), keys...).Err())
			t.Cleanup(func() { _ = client.Del(context.Background(), keys...).Err() })
			require.NoError(t, client.XGroupCreateMkStream(t.Context(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup, "0").Err())
			require.NoError(t, client.XGroupCreateMkStream(t.Context(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup, "0").Err())
			require.NoError(t, client.Set(t.Context(), ChannelMonitorRedisConsumerHeartbeatKey, "node-test", time.Minute).Err())
			payload, err := common.Marshal(ChannelMonitorReliableCostStatus{CheckedAt: time.Now().Unix()})
			require.NoError(t, err)
			require.NoError(t, client.Set(t.Context(), channelMonitorReliableCostStatusKey, payload, time.Minute).Err())
			channelMonitorEventWriterState.Lock()
			previousWriter := channelMonitorEventWriterState.writer
			channelMonitorEventWriterState.writer = newChannelMonitorEventWriter(client, channelMonitorEventWriterConfig{})
			channelMonitorEventWriterState.writer.runStarted.Store(true)
			channelMonitorEventWriterState.Unlock()
			t.Cleanup(func() {
				channelMonitorEventWriterState.Lock()
				channelMonitorEventWriterState.writer = previousWriter
				channelMonitorEventWriterState.Unlock()
			})
			runtime := &ChannelDailyCostOutboxRuntime{}
			runtime.lastDBRecoveryAt.Store(time.Now().Unix())
			runtime.lastRedisConsumerAt.Store(time.Now().Unix())
			input := runtime.observeMonitoringHealth(t.Context(), "node-test")
			require.True(t, input.ObservationComplete)
			state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
			assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
			row := model.ChannelMonitorEventOutbox{EventId: common.GetUUID(), Payload: "{}", CreatedAt: time.Now().Unix() - 180}
			require.NoError(t, db.Create(&row).Error)
			t.Cleanup(func() { _ = db.Delete(&model.ChannelMonitorEventOutbox{}, row.Id).Error })
			input = runtime.observeMonitoringHealth(t.Context(), "node-test")
			require.True(t, input.ObservationComplete)
			state = deriveChannelMonitorRecovery(input, state)
			assert.Equal(t, int64(1), state.Snapshot.PendingCount)
			assert.Contains(t, state.Snapshot.DegradedReasons, ChannelMonitorRedisDegradedReasonEventBacklog)
			runtime.lastDBRecoveryAt.Store(time.Now().Unix() - 121)
			input = runtime.observeMonitoringHealth(t.Context(), "node-test")
			state = deriveChannelMonitorRecovery(input, state)
			assert.Equal(t, "manual_required", state.Snapshot.RecoveryStatus)
			assert.Contains(t, state.Snapshot.DegradedReasons, "cost_worker_stopped")

			channelMonitorHealthWorkerState.Lock()
			previousHealth := channelMonitorHealthWorkerState.state
			channelMonitorHealthWorkerState.state = channelMonitorRecoveryState{}
			channelMonitorHealthWorkerState.Unlock()
			channelMonitorHealthNotificationConfigProvider.Lock()
			previousProvider := channelMonitorHealthNotificationConfigProvider.provider
			channelMonitorHealthNotificationConfigProvider.provider = nil
			channelMonitorHealthNotificationConfigProvider.Unlock()
			workerCtx, stopWorker := context.WithCancel(t.Context())
			workerDone := make(chan struct{})
			t.Cleanup(func() {
				stopWorker()
				<-workerDone
				channelMonitorHealthWorkerState.Lock()
				channelMonitorHealthWorkerState.state = previousHealth
				channelMonitorHealthWorkerState.Unlock()
				channelMonitorHealthNotificationConfigProvider.Lock()
				channelMonitorHealthNotificationConfigProvider.provider = previousProvider
				channelMonitorHealthNotificationConfigProvider.Unlock()
			})
			t.Setenv("CHANNEL_MONITOR_CONSUMER_ID", "background-health-test-"+common.GetUUID())
			go func() { defer close(workerDone); runtime.runHealthMonitor(workerCtx, 0) }()
			require.Eventually(t, func() bool { return GetChannelMonitorRecovery().CheckedAt > 0 }, 5*time.Second, 10*time.Millisecond)
			assert.Equal(t, "manual_required", GetChannelMonitorRecovery().RecoveryStatus, "background sampling works without a page-triggered Redis query")
		})
	}
}

func TestChannelMonitorRecoveryNoticeSurvivesProcessStateReset(t *testing.T) {
	setupMonitorRecoveryRedis(t)
	channelMonitorRecoveryNotices.Lock()
	previousLocal := channelMonitorRecoveryNotices.local
	channelMonitorRecoveryNotices.local = make(map[string]channelMonitorRecoveryNotice)
	channelMonitorRecoveryNotices.Unlock()
	t.Cleanup(func() {
		channelMonitorRecoveryNotices.Lock()
		channelMonitorRecoveryNotices.local = previousLocal
		channelMonitorRecoveryNotices.Unlock()
	})
	previousSend := sendChannelMonitorRecoveryEmail
	t.Cleanup(func() { sendChannelMonitorRecoveryEmail = previousSend })
	subjects := []string{}
	sendChannelMonitorRecoveryEmail = func(subject, receiver, content string) error { subjects = append(subjects, subject); return nil }
	snapshot := ChannelMonitorRecovery{ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthDegraded, DegradedReasons: []string{"redis_context_deadline"}}, NodeID: "test-" + common.GetUUID(), CheckedAt: time.Now().Unix()}
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	require.Len(t, subjects, 1)
	channelMonitorRecoveryNotices.Lock()
	channelMonitorRecoveryNotices.local = make(map[string]channelMonitorRecoveryNotice)
	channelMonitorRecoveryNotices.Unlock()
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	assert.Len(t, subjects, 1, "a second process uses the persisted cooldown")
	snapshot.Status = ChannelMonitorHealthHealthy
	snapshot.RecoveryConfirmed = false
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	assert.Len(t, subjects, 1, "one healthy check after restart is not recovery")
	snapshot.RecoveryConfirmed = true
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	require.Len(t, subjects, 2)
	assert.Equal(t, "渠道监控运行已恢复", subjects[1])
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	assert.Len(t, subjects, 2)
}

func TestChannelMonitorRecoveryNoticeRedisLeasePreventsConcurrentDelivery(t *testing.T) {
	setupMonitorRecoveryRedis(t)
	previousSend := sendChannelMonitorRecoveryEmail
	t.Cleanup(func() { sendChannelMonitorRecoveryEmail = previousSend })
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	sendChannelMonitorRecoveryEmail = func(subject, receiver, content string) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	}
	snapshot := ChannelMonitorRecovery{ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthDegraded}, NodeID: "lease-test-" + common.GetUUID(), CheckedAt: time.Now().Unix()}
	go func() { defer close(done); deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test") }()
	<-started
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	assert.Equal(t, int32(1), calls.Load())
	close(release)
	<-done
	deliverChannelMonitorRecoveryNotice(snapshot, "monitor@example.test")
	assert.Equal(t, int32(1), calls.Load())
}
