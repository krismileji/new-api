package service

import (
	"context"
	"math"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCM07ChannelDailyCostOutboxTest(t *testing.T) (*gorm.DB, *miniredis.Miniredis, *redis.Client) {
	t.Helper()

	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousWriteClient := common.RDBMonitorWrite
	previousReadClient := common.RDBMonitorRead
	previousConsumerClient := common.RDBMonitorConsumer
	previousActive := channelDailyCostReliableOutboxActive.Load()
	previousStats := GetChannelDailyCostReliableStats()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cm07-channel-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelDailyCost{},
		&model.ChannelDailyAPIKeyCost{},
		&model.ChannelDailyCostOutbox{},
	))
	sqlDB, err := db.DB()
	require.NoError(t, err)

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	model.DB = db
	common.RedisEnabled = true
	common.RDB = client
	common.RDBMonitorWrite = client
	common.RDBMonitorRead = client
	common.RDBMonitorConsumer = client
	channelDailyCostReliableOutboxActive.Store(false)
	setCM07ChannelDailyCostReliableStats(ChannelDailyCostReliableStats{})
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "true")

	t.Cleanup(func() {
		channelDailyCostReliableOutboxActive.Store(previousActive)
		setCM07ChannelDailyCostReliableStats(previousStats)
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.RDBMonitorWrite = previousWriteClient
		common.RDBMonitorRead = previousReadClient
		common.RDBMonitorConsumer = previousConsumerClient
		model.DB = previousDB
		require.NoError(t, client.Close())
		require.NoError(t, sqlDB.Close())
	})

	return db, server, client
}

func setCM07ChannelDailyCostReliableStats(stats ChannelDailyCostReliableStats) {
	channelDailyCostReliableStatsState.streamPublished.Store(stats.StreamPublished)
	channelDailyCostReliableStatsState.dbFallbackStored.Store(stats.DBFallbackStored)
	channelDailyCostReliableStatsState.publishFailed.Store(stats.PublishFailed)
	channelDailyCostReliableStatsState.streamRecovered.Store(stats.StreamRecovered)
	channelDailyCostReliableStatsState.ledgerApplied.Store(stats.LedgerApplied)
	channelDailyCostReliableStatsState.ledgerFailed.Store(stats.LedgerFailed)
	channelDailyCostReliableStatsState.deadLettered.Store(stats.DeadLettered)
	channelDailyCostReliableStatsState.outboxPending.Store(stats.OutboxPending)
	channelDailyCostReliableStatsState.outboxOldestAt.Store(stats.OutboxOldestAt)
	channelDailyCostReliableStatsState.outboxRetryCount.Store(stats.OutboxRetryCount)
}

func newCM07ChannelDailyCostDelta(eventID string, channelID int, costNanoCNY int64) model.ChannelDailyCostDelta {
	fingerprint, display := model.ChannelDailyCostAPIKeyIdentityForToken(701, "sk-cm07-upstream")
	return model.ChannelDailyCostDelta{
		EventId:        eventID,
		ChannelId:      channelID,
		OccurredAt:     1_700_000_000,
		CostNanoCNY:    costNanoCNY,
		SettledDelta:   1,
		APIKeyId:       701,
		APIKeyName:     "CM-07 Key",
		KeyFingerprint: fingerprint,
		KeyDisplay:     display,
	}
}

func newCM07ChannelDailyCostRuntime(client *redis.Client, consumerName string) *ChannelDailyCostOutboxRuntime {
	return &ChannelDailyCostOutboxRuntime{
		consumerName: consumerName,
		redisClient:  client,
		redisEnabled: true,
	}
}

func assertCM07ChannelDailyCostLedger(t *testing.T, db *gorm.DB, delta model.ChannelDailyCostDelta) {
	t.Helper()

	var totals []model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", delta.ChannelId).Find(&totals).Error)
	require.Len(t, totals, 1)
	assert.Equal(t, delta.CostNanoCNY, totals[0].CostNanoCNY)
	assert.Equal(t, delta.SettledDelta, totals[0].SettledCount)
	assert.Equal(t, delta.UnresolvedDelta, totals[0].UnresolvedCount)

	var keyTotals []model.ChannelDailyAPIKeyCost
	require.NoError(t, db.Where("channel_id = ? AND key_fingerprint = ?", delta.ChannelId, delta.KeyFingerprint).Find(&keyTotals).Error)
	require.Len(t, keyTotals, 1)
	assert.Equal(t, delta.CostNanoCNY, keyTotals[0].CostNanoCNY)
	assert.Equal(t, delta.SettledDelta, keyTotals[0].SettledCount)
}

func TestCM07ChannelDailyCostStreamPersistsThroughOutboxBeforeLedger(t *testing.T) {
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-stream-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	delta := newCM07ChannelDailyCostDelta("cm07-stream-outbox-ledger", 71, 125)

	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))
	streamLength, err := client.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), streamLength)
	var outboxCount int64
	require.NoError(t, db.Model(&model.ChannelDailyCostOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)

	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	outboxStats, err := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), outboxStats.PendingCount)
	var ledgerCount int64
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
	pending, err := client.XPending(context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)

	require.NoError(t, recoverChannelDailyCostOutboxBatch(context.Background(), "cm07-ledger-owner"))
	assertCM07ChannelDailyCostLedger(t, db, delta)
	outboxStats, err = model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Zero(t, outboxStats.PendingCount)
	assert.Equal(t, ChannelDailyCostReliableStats{
		StreamPublished: 1,
		StreamRecovered: 1,
		LedgerApplied:   1,
	}, GetChannelDailyCostReliableStats())
}

func TestCM07OrdinaryRequestCostUsesReliableStreamBeforeLedger(t *testing.T) {
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	channelDailyCostReliableOutboxActive.Store(true)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-request-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	ctx, _ := gin.CreateTestContext(nil)
	BeginChannelDailyCostAttempt(ctx, 81)

	require.True(t, recordChannelDailyCostEvent(ctx, channelDailyCostSnapshot{ChannelId: 81}, 350, 1, 0))
	streamLength, err := client.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), streamLength)
	assert.Zero(t, GetChannelDailyCostPendingCount())
	var ledgerCount int64
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)

	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	require.NoError(t, recoverChannelDailyCostOutboxBatch(context.Background(), runtime.consumerName))
	var ledger model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", 81).First(&ledger).Error)
	assert.Equal(t, int64(350), ledger.CostNanoCNY)
	assert.Equal(t, int64(1), ledger.SettledCount)
}

func TestCM07ChannelDailyCostRedisRestartRecoversStreamWithNewConsumer(t *testing.T) {
	db, server, client := setupCM07ChannelDailyCostOutboxTest(t)
	initialRuntime := newCM07ChannelDailyCostRuntime(client, "cm07-before-redis-restart")
	require.NoError(t, initialRuntime.initRedisStream(context.Background()))
	delta := newCM07ChannelDailyCostDelta("cm07-redis-restart", 79, 175)
	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))

	redisAddress := server.Addr()
	server.Close()
	require.NoError(t, server.Restart())
	restartedClient := redis.NewClient(&redis.Options{Addr: redisAddress})
	t.Cleanup(func() { require.NoError(t, restartedClient.Close()) })
	restartedRuntime := newCM07ChannelDailyCostRuntime(restartedClient, "cm07-after-redis-restart")
	require.NoError(t, restartedRuntime.initRedisStream(context.Background()))
	streamLength, err := restartedClient.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), streamLength)
	restartedMessages, err := restartedRuntime.readRedisGroup(context.Background(), ">", -1)
	require.NoError(t, err)
	require.Len(t, restartedMessages, 1)

	require.NoError(t, restartedRuntime.consumeRedisBatch(context.Background()))
	require.NoError(t, recoverChannelDailyCostOutboxBatch(context.Background(), restartedRuntime.consumerName))
	assertCM07ChannelDailyCostLedger(t, db, delta)
	pending, err := restartedClient.XPending(
		context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup,
	).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
}

func TestCM07ChannelDailyCostRedisFailureFallsBackToDBAndFlushes(t *testing.T) {
	db, server, _ := setupCM07ChannelDailyCostOutboxTest(t)
	failedClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, failedClient.Close())
	common.RDBMonitorWrite = failedClient
	delta := newCM07ChannelDailyCostDelta("cm07-redis-fallback", 72, 225)

	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))
	outboxStats, err := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), outboxStats.PendingCount)
	refreshChannelDailyCostOutboxStats(context.Background())
	stats := GetChannelDailyCostReliableStats()
	assert.Equal(t, int64(1), stats.DBFallbackStored)
	assert.Equal(t, int64(1), stats.OutboxPending)
	assert.NotZero(t, stats.OutboxOldestAt)

	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	refreshChannelDailyCostOutboxStats(context.Background())
	assertCM07ChannelDailyCostLedger(t, db, delta)
	assert.Equal(t, ChannelDailyCostReliableStats{
		DBFallbackStored: 1,
		LedgerApplied:    1,
	}, GetChannelDailyCostReliableStats())
}

func TestCM07ChannelDailyCostUnavailableBuffersFailAndIncrementMetric(t *testing.T) {
	db, server, client := setupCM07ChannelDailyCostOutboxTest(t)
	failedClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, failedClient.Close())
	common.RDBMonitorWrite = failedClient
	model.DB = nil
	delta := newCM07ChannelDailyCostDelta("cm07-no-reliable-buffer", 78, 275)

	publishErr := publishChannelDailyCostReliableEvent(context.Background(), delta)
	model.DB = db
	require.Error(t, publishErr)
	streamLength, err := client.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	assert.Zero(t, streamLength)
	assert.Equal(t, ChannelDailyCostReliableStats{PublishFailed: 1}, GetChannelDailyCostReliableStats())
}

type cm07AmbiguousRedisTimeout struct{}

func (cm07AmbiguousRedisTimeout) Error() string   { return "Redis XADD acknowledgement timed out" }
func (cm07AmbiguousRedisTimeout) Timeout() bool   { return true }
func (cm07AmbiguousRedisTimeout) Temporary() bool { return true }

type cm07PublishDeadlineHook struct {
	seen      atomic.Bool
	remaining atomic.Int64
}

func (hook *cm07PublishDeadlineHook) BeforeProcess(ctx context.Context, command redis.Cmder) (context.Context, error) {
	if command.Name() != "xadd" {
		return ctx, nil
	}
	deadline, ok := ctx.Deadline()
	hook.seen.Store(ok)
	if ok {
		hook.remaining.Store(time.Until(deadline).Nanoseconds())
	}
	return ctx, assert.AnError
}

func (hook *cm07PublishDeadlineHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *cm07PublishDeadlineHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook *cm07PublishDeadlineHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

type cm07ZeroStreamLengthHook struct{}

func (*cm07ZeroStreamLengthHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*cm07ZeroStreamLengthHook) AfterProcess(_ context.Context, command redis.Cmder) error {
	if command.Name() == "xlen" {
		command.(*redis.IntCmd).SetVal(0)
	}
	return nil
}

func (*cm07ZeroStreamLengthHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*cm07ZeroStreamLengthHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestCM07ChannelDailyCostReliableAcceptUsesBoundedOperationContexts(t *testing.T) {
	db, server, _ := setupCM07ChannelDailyCostOutboxTest(t)
	hook := &cm07PublishDeadlineHook{}
	failedClient := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	failedClient.AddHook(hook)
	t.Cleanup(func() { require.NoError(t, failedClient.Close()) })
	common.RDBMonitorWrite = failedClient
	var fallbackSeen atomic.Bool
	var fallbackRemaining atomic.Int64
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("cm07_capture_fallback_deadline", func(tx *gorm.DB) {
		deadline, ok := tx.Statement.Context.Deadline()
		fallbackSeen.Store(ok)
		if ok {
			fallbackRemaining.Store(time.Until(deadline).Nanoseconds())
		}
	}))

	delta := newCM07ChannelDailyCostDelta("cm07-bounded-accept", 87, 350)
	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))
	assert.True(t, hook.seen.Load())
	assert.Positive(t, hook.remaining.Load())
	assert.LessOrEqual(t, hook.remaining.Load(), channelDailyCostOutboxPublishTimeout.Nanoseconds())
	assert.True(t, fallbackSeen.Load())
	assert.Positive(t, fallbackRemaining.Load())
	assert.LessOrEqual(t, fallbackRemaining.Load(), channelDailyCostOutboxDBFallbackTimeout.Nanoseconds())
	stats, err := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.PendingCount)
}

type cm07AmbiguousRedisConn struct {
	net.Conn
	failed *atomic.Bool
}

func (conn *cm07AmbiguousRedisConn) Read(buffer []byte) (int, error) {
	read, err := conn.Conn.Read(buffer)
	if read > 0 && conn.failed.CompareAndSwap(false, true) {
		return 0, cm07AmbiguousRedisTimeout{}
	}
	return read, err
}

func TestCM07ChannelDailyCostAmbiguousRedisTimeoutAppliesEventOnce(t *testing.T) {
	db, server, consumerClient := setupCM07ChannelDailyCostOutboxTest(t)
	var failed atomic.Bool
	ambiguousClient := redis.NewClient(&redis.Options{
		Addr:       server.Addr(),
		MaxRetries: -1,
		Dialer: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &cm07AmbiguousRedisConn{Conn: connection, failed: &failed}, nil
		},
	})
	t.Cleanup(func() { require.NoError(t, ambiguousClient.Close()) })
	common.RDBMonitorWrite = ambiguousClient
	delta := newCM07ChannelDailyCostDelta("cm07-ambiguous-xadd", 73, 325)

	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))
	assert.True(t, failed.Load())
	streamLength, err := consumerClient.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), streamLength)
	outboxStats, err := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), outboxStats.PendingCount)

	runtime := newCM07ChannelDailyCostRuntime(consumerClient, "cm07-ambiguous-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	refreshChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	refreshChannelDailyCostOutboxStats(context.Background())
	assertCM07ChannelDailyCostLedger(t, db, delta)

	var outboxRows []model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", delta.EventId).Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	assert.NotZero(t, outboxRows[0].ProcessedAt)
	assert.Equal(t, ChannelDailyCostReliableStats{
		DBFallbackStored: 1,
		StreamRecovered:  1,
		LedgerApplied:    1,
	}, GetChannelDailyCostReliableStats())
}

func TestCM07ChannelDailyCostRestartRecoversExpiredLeaseWithNewOwner(t *testing.T) {
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	oldRuntime := newCM07ChannelDailyCostRuntime(client, "cm07-old-owner")
	newRuntime := newCM07ChannelDailyCostRuntime(client, "cm07-new-owner")
	require.NoError(t, oldRuntime.initRedisStream(context.Background()))
	delta := newCM07ChannelDailyCostDelta("cm07-restart-recovery", 74, 425)
	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))
	require.NoError(t, oldRuntime.consumeRedisBatch(context.Background()))

	oldTimestamp := time.Now().Add(-time.Minute).Unix()
	claimed, err := model.ClaimChannelDailyCostOutboxEvents(
		context.Background(), oldRuntime.consumerName, oldTimestamp, oldTimestamp, 10*time.Second, 1,
	)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, oldRuntime.consumerName, claimed[0].LeaseOwner)

	require.NoError(t, recoverChannelDailyCostOutboxBatch(context.Background(), newRuntime.consumerName))
	assertCM07ChannelDailyCostLedger(t, db, delta)
	require.NoError(t, recoverChannelDailyCostOutboxBatch(context.Background(), oldRuntime.consumerName))
	assertCM07ChannelDailyCostLedger(t, db, delta)

	var row model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", delta.EventId).First(&row).Error)
	assert.NotZero(t, row.ProcessedAt)
	assert.Empty(t, row.LeaseOwner)
	assert.Equal(t, ChannelDailyCostReliableStats{
		StreamPublished: 1,
		StreamRecovered: 1,
		LedgerApplied:   1,
	}, GetChannelDailyCostReliableStats())
}

func TestCM07ChannelDailyCostRestartClaimsPendingStreamWithNewOwner(t *testing.T) {
	db, server, client := setupCM07ChannelDailyCostOutboxTest(t)
	baseTime := time.Unix(1_800_000_000, 0)
	server.SetTime(baseTime)
	oldRuntime := newCM07ChannelDailyCostRuntime(client, "cm07-old-stream-owner")
	require.NoError(t, oldRuntime.initRedisStream(context.Background()))
	delta := newCM07ChannelDailyCostDelta("cm07-pending-stream-takeover", 80, 475)
	require.NoError(t, publishChannelDailyCostReliableEvent(context.Background(), delta))

	messages, err := oldRuntime.readRedisGroup(context.Background(), ">", -1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	pending, err := client.XPending(context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending.Count)

	server.SetTime(baseTime.Add(channelDailyCostOutboxClaimMinIdle + time.Second))
	newRuntime := newCM07ChannelDailyCostRuntime(client, "cm07-new-stream-owner")
	require.NoError(t, newRuntime.consumeRedisBatch(context.Background()))
	require.NoError(t, recoverChannelDailyCostOutboxBatch(context.Background(), newRuntime.consumerName))
	assertCM07ChannelDailyCostLedger(t, db, delta)
	pending, err = client.XPending(context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
}

func TestCM07ChannelDailyCostMalformedStreamEventIsDeadLettered(t *testing.T) {
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-dead-letter-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	messageID, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelDailyCostRedisStream,
		Values: map[string]interface{}{
			channelDailyCostRedisFieldEventID: "cm07-malformed",
			channelDailyCostRedisFieldPayload: "{invalid-json",
		},
	}).Result()
	require.NoError(t, err)

	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	deadLetters, err := client.XRange(context.Background(), ChannelDailyCostRedisDeadLetter, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, deadLetters, 1)
	assert.Equal(t, messageID, deadLetters[0].Values["original_message_id"])
	assert.Equal(t, "cm07-malformed", deadLetters[0].Values["event_id"])
	assert.NotEmpty(t, deadLetters[0].Values["error"])
	streamLength, err := client.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	assert.Zero(t, streamLength)
	pending, err := client.XPending(context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
	var outboxCount int64
	require.NoError(t, db.Model(&model.ChannelDailyCostOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
	assert.Equal(t, ChannelDailyCostReliableStats{DeadLettered: 1}, GetChannelDailyCostReliableStats())
}

func TestCM07ChannelDailyCostSemanticallyInvalidStreamEventIsDeadLettered(t *testing.T) {
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-invalid-payload-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	payload, err := common.Marshal(channelDailyCostOutboxPayload{ChannelId: 0, OccurredAt: 1_700_000_000, SettledDelta: 1})
	require.NoError(t, err)
	_, err = client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelDailyCostRedisStream,
		Values: map[string]interface{}{
			channelDailyCostRedisFieldEventID: "cm07-invalid-payload",
			channelDailyCostRedisFieldPayload: string(payload),
		},
	}).Result()
	require.NoError(t, err)

	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	deadLetters, err := client.XRange(context.Background(), ChannelDailyCostRedisDeadLetter, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, deadLetters, 1)
	assert.Equal(t, "cm07-invalid-payload", deadLetters[0].Values["event_id"])
	assert.Contains(t, deadLetters[0].Values["error"], "channel id")
	var outboxCount int64
	require.NoError(t, db.Model(&model.ChannelDailyCostOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
	pending, err := client.XPending(context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
}

func TestCM07ReliableProducerRejectsSemanticallyInvalidEvent(t *testing.T) {
	_, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	channelDailyCostReliableOutboxActive.Store(true)
	delta := newCM07ChannelDailyCostDelta("cm07-invalid-producer", 84, 1)
	delta.SettledDelta = 0
	delta.UnresolvedDelta = 0
	err := publishChannelDailyCostReliableEvent(context.Background(), delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "内容无效")
	streamLength, streamErr := client.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, streamErr)
	assert.Zero(t, streamLength)
	stats := GetChannelDailyCostReliableStats()
	assert.Zero(t, stats.PublishFailed)
	assert.Zero(t, stats.DBFallbackStored)
}

func TestCM07OversizedPayloadIsDeadLetteredWithBoundedForensics(t *testing.T) {
	_, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-oversized-payload-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	messageID, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelDailyCostRedisStream,
		Values: map[string]interface{}{
			channelDailyCostRedisFieldEventID: "cm07-oversized-payload",
			channelDailyCostRedisFieldPayload: strings.Repeat("x", channelDailyCostOutboxPayloadMaxLength+1),
		},
	}).Result()
	require.NoError(t, err)
	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	deadLetters, err := client.XRange(context.Background(), ChannelDailyCostRedisDeadLetter, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, deadLetters, 1)
	assert.Equal(t, messageID, deadLetters[0].Values["original_message_id"])
	deadPayload, ok := redisStringValue(deadLetters[0].Values["payload"])
	require.True(t, ok)
	assert.Len(t, deadPayload, channelDailyCostOutboxPayloadMaxLength)
	assert.Equal(t, ChannelDailyCostReliableStats{DeadLettered: 1}, GetChannelDailyCostReliableStats())
}

func TestCM07OversizedEventIDIsDeadLetteredWithBoundedForensics(t *testing.T) {
	_, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-oversized-event-id")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	oversizedEventID := strings.Repeat("e", model.ChannelDailyCostOutboxEventIDMaxLength+1_024)
	messageID, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelDailyCostRedisStream,
		Values: map[string]interface{}{
			channelDailyCostRedisFieldEventID: oversizedEventID,
			channelDailyCostRedisFieldPayload: "{}",
		},
	}).Result()
	require.NoError(t, err)
	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	deadLetters, err := client.XRange(context.Background(), ChannelDailyCostRedisDeadLetter, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, deadLetters, 1)
	assert.Equal(t, messageID, deadLetters[0].Values["original_message_id"])
	deadEventID, ok := redisStringValue(deadLetters[0].Values["event_id"])
	require.True(t, ok)
	assert.Len(t, deadEventID, model.ChannelDailyCostOutboxEventIDMaxLength)
}

func TestCM07ChannelDailyCostCollisionDoesNotBlockValidStreamEvents(t *testing.T) {
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-collision-consumer")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	existing := newCM07ChannelDailyCostDelta("cm07-collision", 83, 100)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{existing}))
	refreshChannelDailyCostOutboxStats(context.Background())
	collision := existing
	collision.CostNanoCNY = 999
	valid := newCM07ChannelDailyCostDelta("cm07-after-collision", 84, 200)
	for _, delta := range []model.ChannelDailyCostDelta{collision, valid} {
		payload, err := common.Marshal(channelDailyCostOutboxPayloadFromDelta(delta))
		require.NoError(t, err)
		_, err = client.XAdd(context.Background(), &redis.XAddArgs{
			Stream: ChannelDailyCostRedisStream,
			Values: map[string]interface{}{
				channelDailyCostRedisFieldEventID: delta.EventId,
				channelDailyCostRedisFieldPayload: string(payload),
			},
		}).Result()
		require.NoError(t, err)
	}

	require.NoError(t, runtime.consumeRedisBatch(context.Background()))
	deadLetters, err := client.XRange(context.Background(), ChannelDailyCostRedisDeadLetter, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, deadLetters, 1)
	assert.Equal(t, collision.EventId, deadLetters[0].Values["event_id"])
	streamLength, err := client.XLen(context.Background(), ChannelDailyCostRedisStream).Result()
	require.NoError(t, err)
	assert.Zero(t, streamLength)

	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	assertCM07ChannelDailyCostLedger(t, db, existing)
	assertCM07ChannelDailyCostLedger(t, db, valid)
	var collisionLedger model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", collision.ChannelId).First(&collisionLedger).Error)
	assert.Equal(t, existing.CostNanoCNY, collisionLedger.CostNanoCNY)
	stats := GetChannelDailyCostReliableStats()
	assert.Equal(t, int64(1), stats.DeadLettered)
	assert.Equal(t, int64(1), stats.StreamRecovered)
	assert.Equal(t, int64(2), stats.LedgerApplied)
}

func TestCM07ChannelDailyCostDeadLetterWriteFailureKeepsSourcePending(t *testing.T) {
	_, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-dead-letter-failure")
	require.NoError(t, runtime.initRedisStream(context.Background()))
	messageID, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelDailyCostRedisStream,
		Values: map[string]interface{}{
			channelDailyCostRedisFieldEventID: "cm07-dead-letter-failure",
			channelDailyCostRedisFieldPayload: "{invalid-json",
		},
	}).Result()
	require.NoError(t, err)
	messages, err := runtime.readNewRedisMessages(context.Background())
	require.NoError(t, err)
	require.Len(t, messages, 1)

	require.NoError(t, client.Set(context.Background(), ChannelDailyCostRedisDeadLetter, "wrong-type", 0).Err())
	deadLetterErr := runtime.deadLetterRedisMessage(context.Background(), messages[0], assert.AnError)
	require.Error(t, deadLetterErr)
	assert.Zero(t, GetChannelDailyCostReliableStats().DeadLettered)

	source, err := client.XRange(context.Background(), ChannelDailyCostRedisStream, "-", "+").Result()
	require.NoError(t, err)
	pending, err := client.XPending(context.Background(), ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
	require.NoError(t, err)
	deadLetterType, err := client.Type(context.Background(), ChannelDailyCostRedisDeadLetter).Result()
	require.NoError(t, err)
	assert.Len(t, source, 1)
	if len(source) == 1 {
		assert.Equal(t, messageID, source[0].ID)
	}
	assert.Equal(t, int64(1), pending.Count)
	assert.Equal(t, "string", deadLetterType)
}

func TestCM07ChannelDailyCostRealtimeStatusSeparatesStreamAndOutboxBacklogs(t *testing.T) {
	_, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	ctx := context.Background()
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-observability")
	require.NoError(t, runtime.initRedisStream(ctx))
	for index := 0; index < 2; index++ {
		_, err := client.XAdd(ctx, &redis.XAddArgs{
			Stream: ChannelDailyCostRedisStream,
			Values: map[string]interface{}{
				channelDailyCostRedisFieldEventID: "cm07-observability-event",
				channelDailyCostRedisFieldPayload: "{}",
			},
		}).Result()
		require.NoError(t, err)
	}
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: ChannelDailyCostRedisConsumerGroup, Consumer: runtime.consumerName,
		Streams: []string{ChannelDailyCostRedisStream, ">"}, Count: 1, Block: -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Messages, 1)

	channelDailyCostReliableOutboxActive.Store(true)
	setCM07ChannelDailyCostReliableStats(ChannelDailyCostReliableStats{
		OutboxPending: 3, OutboxOldestAt: 1_700_000_000, OutboxRetryCount: 4,
		LedgerFailed: 8, PublishFailed: 5, DeadLettered: 6,
	})
	status := ChannelMonitorRedisRealtimeStatus{DegradedReasons: make([]string, 0)}
	applyChannelDailyCostReliableStatus(&status, client, ctx)

	assert.Equal(t, int64(1), status.CostStreamPendingCount)
	assert.Equal(t, int64(1), status.CostStreamUnreadCount)
	assert.Equal(t, int64(3), status.CostOutboxPendingCount)
	assert.Equal(t, int64(1_700_000_000), status.CostOutboxOldestPendingAt)
	assert.Equal(t, int64(4), status.CostOutboxRetryCount)
	assert.Equal(t, int64(8), status.CostLedgerFailedCount)
	assert.Equal(t, int64(5), status.CostPublishFailedCount)
	assert.Equal(t, int64(6), status.CostDeadLetterCount)
	assert.ElementsMatch(t, []string{
		ChannelMonitorRedisDegradedReasonCostStreamBacklog,
		ChannelMonitorRedisDegradedReasonCostOutboxBacklog,
		ChannelMonitorRedisDegradedReasonCostPublishFailure,
		ChannelMonitorRedisDegradedReasonCostDeadLetter,
	}, status.DegradedReasons)
	assert.True(t, status.RealtimeDegraded)
}

func TestCM07ChannelDailyCostRealtimeStatusQueriesPendingPELWhenStreamLengthIsZero(t *testing.T) {
	_, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	ctx := context.Background()
	runtime := newCM07ChannelDailyCostRuntime(client, "cm07-observability-deleted-entry")
	require.NoError(t, runtime.initRedisStream(ctx))
	_, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: ChannelDailyCostRedisStream,
		Values: map[string]interface{}{
			channelDailyCostRedisFieldEventID: "cm07-deleted-pending",
			channelDailyCostRedisFieldPayload: "{}",
		},
	}).Result()
	require.NoError(t, err)
	_, err = client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: ChannelDailyCostRedisConsumerGroup, Consumer: runtime.consumerName,
		Streams: []string{ChannelDailyCostRedisStream, ">"}, Count: 1, Block: -1,
	}).Result()
	require.NoError(t, err)
	client.AddHook(&cm07ZeroStreamLengthHook{})

	channelDailyCostReliableOutboxActive.Store(true)
	status := ChannelMonitorRedisRealtimeStatus{DegradedReasons: make([]string, 0)}
	applyChannelDailyCostReliableStatus(&status, client, ctx)

	assert.Zero(t, status.CostStreamUnreadCount)
	assert.Equal(t, int64(1), status.CostStreamPendingCount)
	assert.Contains(t, status.DegradedReasons, ChannelMonitorRedisDegradedReasonCostStreamBacklog)
}

func TestCM07ChannelDailyCostRuntimeStopAndFlush(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	common.RedisEnabled = false
	common.RDBMonitorWrite = nil
	common.RDBMonitorConsumer = nil

	firstRuntime, err := StartChannelDailyCostOutboxRuntime()
	require.NoError(t, err)
	assert.True(t, channelDailyCostReliableOutboxIsActive())
	require.NoError(t, firstRuntime.Stop(context.Background()))
	assert.False(t, channelDailyCostReliableOutboxIsActive())
	require.NoError(t, firstRuntime.Stop(context.Background()))

	secondRuntime, err := StartChannelDailyCostOutboxRuntime()
	require.NoError(t, err)
	assert.True(t, channelDailyCostReliableOutboxIsActive())
	require.NoError(t, secondRuntime.Stop(context.Background()))
	assert.False(t, channelDailyCostReliableOutboxIsActive())

	deltas := []model.ChannelDailyCostDelta{
		newCM07ChannelDailyCostDelta("cm07-flush-a", 75, 525),
		newCM07ChannelDailyCostDelta("cm07-flush-b", 76, 625),
	}
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), deltas))
	refreshChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	assertCM07ChannelDailyCostLedger(t, db, deltas[0])
	assertCM07ChannelDailyCostLedger(t, db, deltas[1])
	assert.Equal(t, int64(2), GetChannelDailyCostReliableStats().LedgerApplied)
}

func TestCM07ChannelDailyCostRuntimeStartResetsStaleActiveStateWhenDisabled(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "false")
	channelDailyCostReliableOutboxActive.Store(true)
	delta := newCM07ChannelDailyCostDelta("cm07-disabled-drain", 80, 500)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))

	runtime, err := StartChannelDailyCostOutboxRuntime()
	require.NoError(t, err)
	require.NotNil(t, runtime)
	assert.False(t, channelDailyCostReliableOutboxIsActive())
	require.Eventually(t, func() bool {
		stats, statsErr := model.GetChannelDailyCostOutboxStats(context.Background())
		return statsErr == nil && stats.PendingCount == 0
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, runtime.Stop(context.Background()))
	// A rollback must leave the reliable consumer alive long enough to drain
	// events that were accepted before the switch, even though new events use
	// the legacy batcher.
	assertCM07ChannelDailyCostLedger(t, db, delta)
}

func TestCM07ChannelDailyCostRuntimeBoundsLeaseOwnerIdentity(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	common.RedisEnabled = false
	common.RDBMonitorWrite = nil
	common.RDBMonitorConsumer = nil
	t.Setenv("CHANNEL_MONITOR_CONSUMER_ID", strings.Repeat("oversized-instance-", 32))
	delta := newCM07ChannelDailyCostDelta("cm07-bounded-lease-owner", 88, 550)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))

	runtime, err := StartChannelDailyCostOutboxRuntime()
	require.NoError(t, err)
	require.LessOrEqual(t, len(runtime.consumerName), 128)
	require.Eventually(t, func() bool {
		stats, statsErr := model.GetChannelDailyCostOutboxStats(context.Background())
		return statsErr == nil && stats.PendingCount == 0
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, runtime.Stop(context.Background()))
	assertCM07ChannelDailyCostLedger(t, db, delta)
}

func TestCM07ChannelDailyCostReliableStatusRemainsVisibleDuringDisabledDrain(t *testing.T) {
	setupCM07ChannelDailyCostOutboxTest(t)
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "false")
	delta := newCM07ChannelDailyCostDelta("cm07-disabled-status", 81, 525)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))
	refreshChannelDailyCostOutboxStats(context.Background())

	status := ChannelMonitorRedisRealtimeStatus{DegradedReasons: make([]string, 0)}
	applyChannelDailyCostReliableStatus(&status, nil, context.Background())

	assert.Equal(t, int64(1), status.CostOutboxPendingCount)
	assert.Contains(t, status.DegradedReasons, ChannelMonitorRedisDegradedReasonCostOutboxBacklog)
}

func TestCM07ChannelDailyCostRuntimeRefusesMissingDurableOutbox(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	require.NoError(t, db.Migrator().DropTable(&model.ChannelDailyCostOutbox{}))

	runtime, err := StartChannelDailyCostOutboxRuntime()
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.False(t, channelDailyCostReliableOutboxIsActive())
}

func TestCM07ChannelDailyCostFlushRetriesDeferredOutbox(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	delta := newCM07ChannelDailyCostDelta("cm07-deferred-flush", 82, 725)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))
	now := time.Now().Unix()
	claimed, err := model.ClaimChannelDailyCostOutboxEvents(
		context.Background(), "cm07-deferred-owner", now, now, time.Minute, 1,
	)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, model.FailClaimedChannelDailyCostOutboxEvents(
		context.Background(), "cm07-deferred-owner", []int64{claimed[0].Id},
		time.Now().Add(channelDailyCostOutboxMaximumRetryDelay).Unix(), assert.AnError,
	))

	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	assertCM07ChannelDailyCostLedger(t, db, delta)
	stats, err := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Zero(t, stats.PendingCount)
}

func TestCM07ChannelDailyCostLedgerFailureIsRetriedAndCounted(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	seed := newCM07ChannelDailyCostDelta("", 77, 1)
	require.NoError(t, model.AddChannelDailyCostBatch(context.Background(), []model.ChannelDailyCostDelta{seed}))
	delta := newCM07ChannelDailyCostDelta("cm07-ledger-overflow", 77, math.MaxInt64)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))
	refreshChannelDailyCostOutboxStats(context.Background())

	err := recoverChannelDailyCostOutboxBatch(context.Background(), "cm07-failing-ledger-owner")
	require.Error(t, err)
	refreshChannelDailyCostOutboxStats(context.Background())
	stats := GetChannelDailyCostReliableStats()
	assert.Zero(t, stats.LedgerApplied)
	assert.Equal(t, int64(1), stats.LedgerFailed)
	assert.Equal(t, int64(1), stats.OutboxPending)
	assert.NotZero(t, stats.OutboxOldestAt)
	assert.Equal(t, int64(1), stats.OutboxRetryCount)
	outboxStats, statsErr := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, statsErr)
	assert.Equal(t, int64(1), outboxStats.PendingCount)
	assert.Equal(t, int64(1), outboxStats.RetryCount)

	var row model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", delta.EventId).First(&row).Error)
	assert.Empty(t, row.LeaseOwner)
	assert.NotZero(t, row.NextAttemptAt)
	assert.NotEmpty(t, row.LastError)
	var ledger model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", delta.ChannelId).First(&ledger).Error)
	assert.Equal(t, int64(1), ledger.CostNanoCNY)
}

func TestCM07ChannelDailyCostOverflowDoesNotBlockValidOutboxEvents(t *testing.T) {
	db, _, _ := setupCM07ChannelDailyCostOutboxTest(t)
	seed := newCM07ChannelDailyCostDelta("", 85, math.MaxInt64)
	require.NoError(t, model.AddChannelDailyCostBatch(context.Background(), []model.ChannelDailyCostDelta{seed}))
	overflow := newCM07ChannelDailyCostDelta("cm07-isolated-overflow", 85, 1)
	valid := newCM07ChannelDailyCostDelta("cm07-valid-after-overflow", 86, 300)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{overflow, valid}))
	refreshChannelDailyCostOutboxStats(context.Background())

	err := recoverChannelDailyCostOutboxBatch(context.Background(), "cm07-overflow-isolation-owner")
	require.ErrorIs(t, err, model.ErrChannelDailyCostLedgerOverflow)
	assertCM07ChannelDailyCostLedger(t, db, valid)
	var seedLedger model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", seed.ChannelId).First(&seedLedger).Error)
	assert.Equal(t, int64(math.MaxInt64), seedLedger.CostNanoCNY)

	stats, statsErr := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, statsErr)
	assert.Equal(t, int64(1), stats.PendingCount)
	var remaining model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("processed_at = ?", 0).First(&remaining).Error)
	assert.Equal(t, overflow.EventId, remaining.EventId)
	assert.Empty(t, remaining.LeaseOwner)
	assert.NotZero(t, remaining.NextAttemptAt)
	reliableStats := GetChannelDailyCostReliableStats()
	assert.Equal(t, int64(1), reliableStats.LedgerApplied)
	assert.Equal(t, int64(1), reliableStats.LedgerFailed)
	assert.Equal(t, int64(1), reliableStats.OutboxPending)
}
