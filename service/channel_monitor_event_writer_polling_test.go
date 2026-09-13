package service

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type channelMonitorOutboxPollingSQLRecorder struct {
	logger.Interface
	selects atomic.Int64
}

func (recorder *channelMonitorOutboxPollingSQLRecorder) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT ") {
		recorder.selects.Add(1)
	}
}

// synctest.Wait synchronizes the appender with the test's assertions.
type channelMonitorOutboxPollingAppender struct {
	eventIDs []string
	err      error
}

func (appender *channelMonitorOutboxPollingAppender) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "XADD")
	if appender.err != nil {
		cmd.SetErr(appender.err)
		return cmd
	}
	values := args.Values.(map[string]interface{})
	appender.eventIDs = append(appender.eventIDs, values[ChannelMonitorRedisEventFieldEventID].(string))
	cmd.SetVal("1-0")
	return cmd
}

func setupChannelMonitorOutboxPollingTest(t *testing.T) *gorm.DB {
	t.Helper()
	useChannelMonitorEventPublishStatsIsolation(t)
	previousType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(previousType) })
	return setupChannelMonitorEventWriterReliabilityDB(t)
}

func TestChannelMonitorEventOutboxPollingBacksOffAndWakesForLocalWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := setupChannelMonitorOutboxPollingTest(t)
		recorder := &channelMonitorOutboxPollingSQLRecorder{Interface: logger.Default}
		db.Logger = recorder
		appender := &channelMonitorOutboxPollingAppender{}
		writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{})
		setChannelMonitorEventWriterForTest(t, writer)
		defer func() { assert.NoError(t, writer.Stop(context.Background())) }()
		writer.startOutbox()
		writer.startOutbox()
		synctest.Wait()
		assert.EqualValues(t, 2, recorder.selects.Load(), "one startup schema check and one pending-event query")

		// Advance virtual time: idle queries back off to the five-second cap.
		for index, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second} {
			time.Sleep(delay)
			synctest.Wait()
			assert.EqualValues(t, index+3, recorder.selects.Load(), "idle recovery must not repeatedly probe schema metadata")
		}

		// A different instance cannot send this process a local notification.
		remote := newChannelMonitorPublisherTestEvent("outbox-polling-remote")
		payload, err := remote.Marshal()
		require.NoError(t, err)
		_, err = model.StoreChannelMonitorEventOutbox(context.Background(), remote.EventId, payload)
		require.NoError(t, err)
		synctest.Wait()
		assert.Empty(t, appender.eventIDs)
		time.Sleep(5 * time.Second)
		synctest.Wait()
		assert.Equal(t, []string{remote.EventId}, appender.eventIDs)

		// After backing off again, a committed local event wakes replay without
		// waiting for the next timer. Duplicate persistence must not republish it.
		time.Sleep(8 * time.Second)
		synctest.Wait()
		local := newChannelMonitorPublisherTestEvent("outbox-polling-local")
		stored, err := writer.persistOutbox(channelMonitorEventWriterItem{event: local})
		require.NoError(t, err)
		require.True(t, stored)
		synctest.Wait()
		assert.Equal(t, []string{remote.EventId, local.EventId}, appender.eventIDs)
		stored, err = writer.persistOutbox(channelMonitorEventWriterItem{event: local})
		require.NoError(t, err)
		require.True(t, stored)
		synctest.Wait()
		assert.Len(t, appender.eventIDs, 2)

		var rows []model.ChannelMonitorEventOutbox
		require.NoError(t, db.Order("id ASC").Find(&rows).Error)
		require.Len(t, rows, 2)
		for _, row := range rows {
			assert.NotZero(t, row.ProcessedAt)
			assert.Empty(t, row.LeaseOwner)
		}
		require.NoError(t, writer.Stop(context.Background()))
		queriesAtStop := recorder.selects.Load()
		time.Sleep(10 * time.Second)
		synctest.Wait()
		assert.Equal(t, queriesAtStop, recorder.selects.Load(), "stopping must cancel an idle recovery timer")
	})
}

func TestChannelMonitorEventOutboxPollingRecoversAfterDatabaseAndRedisFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := setupChannelMonitorOutboxPollingTest(t)
		var failQuery atomic.Bool
		var queries atomic.Int64
		failQuery.Store(true)
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:outbox_polling_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "channel_monitor_event_outboxes" {
				queries.Add(1)
				if failQuery.Load() {
					tx.AddError(assert.AnError)
				}
			}
		}))
		appender := &channelMonitorOutboxPollingAppender{err: assert.AnError}
		writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{})
		defer func() { assert.NoError(t, writer.Stop(context.Background())) }()
		writer.startOutbox()
		synctest.Wait()
		time.Sleep(12 * time.Second)
		synctest.Wait()
		assert.EqualValues(t, 5, queries.Load(), "database errors must back off rather than spin")
		failQuery.Store(false)
		event := newChannelMonitorPublisherTestEvent("outbox-polling-recovery")
		payload, err := event.Marshal()
		require.NoError(t, err)
		_, err = model.StoreChannelMonitorEventOutbox(context.Background(), event.EventId, payload)
		require.NoError(t, err)
		time.Sleep(5 * time.Second)
		synctest.Wait()
		var row model.ChannelMonitorEventOutbox
		require.NoError(t, db.First(&row).Error)
		assert.Zero(t, row.ProcessedAt)
		assert.EqualValues(t, 1, row.AttemptCount)
		assert.Empty(t, appender.eventIDs)

		appender.err = nil
		time.Sleep(1500 * time.Millisecond)
		synctest.Wait()
		require.NoError(t, db.First(&row).Error)
		assert.NotZero(t, row.ProcessedAt)
		assert.Equal(t, []string{event.EventId}, appender.eventIDs)
	})
}
