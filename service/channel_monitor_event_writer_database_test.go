package service

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External cases only use disposable local databases with this dedicated name.
func TestChannelMonitorEventOutboxDatabaseMatrix(t *testing.T) {
	redisAddr := os.Getenv("TEST_EVENT_OUTBOX_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("设置 TEST_EVENT_OUTBOX_REDIS_ADDR 运行真实数据库和 Redis 补发验证")
	}
	require.True(t, strings.HasPrefix(redisAddr, "127.0.0.1:"))
	useChannelMonitorEventPublishStatsIsolation(t)
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			versionQuery := "SELECT version()"
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "outbox.db"))
				versionQuery = "SELECT sqlite_version()"
			case "mysql":
				dsn := os.Getenv("TEST_EVENT_OUTBOX_MYSQL_DSN")
				require.NotEmpty(t, dsn, "真实数据库矩阵必须同时配置 MySQL 和 PostgreSQL")
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_outbox_poll_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_EVENT_OUTBOX_POSTGRES_DSN")
				require.NotEmpty(t, dsn, "真实数据库矩阵必须同时配置 MySQL 和 PostgreSQL")
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_outbox_poll_test", parsed.Path)
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			previousDB, previousType := model.DB, common.MainDatabaseType()
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseType(engine))
			t.Cleanup(func() {
				model.DB = previousDB
				common.SetMainDatabaseType(previousType)
				assert.NoError(t, sqlDB.Close())
			})
			require.False(t, db.Migrator().HasTable(&model.ChannelMonitorEventOutbox{}), "验证数据库必须为空")
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorEventOutbox{})) })
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorEventOutbox{}))
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			client := redis.NewClient(&redis.Options{Addr: redisAddr})
			t.Cleanup(func() { assert.NoError(t, client.Close()) })
			require.NoError(t, client.Ping(ctx).Err())
			group := "outbox-polling-" + common.GetUUID()
			require.NoError(t, client.XGroupCreateMkStream(ctx, ChannelMonitorRedisEventStream, group, "$").Err())
			t.Cleanup(func() {
				assert.NoError(t, client.XGroupDestroy(context.Background(), ChannelMonitorRedisEventStream, group).Err())
			})

			// Another worker's valid lease must survive this worker's scans.
			leased := newChannelMonitorPublisherTestEvent("outbox-leased-" + engine)
			payload, err := leased.Marshal()
			require.NoError(t, err)
			_, err = model.StoreChannelMonitorEventOutbox(ctx, leased.EventId, payload)
			require.NoError(t, err)
			claimed, err := model.ClaimChannelMonitorEventOutbox(ctx, "other-worker", time.Now().Unix(), time.Minute, 1)
			require.NoError(t, err)
			require.Len(t, claimed, 1)
			pending := newChannelMonitorPublisherTestEvent("outbox-pending-" + engine)
			payload, err = pending.Marshal()
			require.NoError(t, err)
			_, err = model.StoreChannelMonitorEventOutbox(ctx, pending.EventId, payload)
			require.NoError(t, err)

			writer := newChannelMonitorEventWriter(channelMonitorEventWriterAlwaysFailAppender{}, channelMonitorEventWriterConfig{})
			defer writer.cancelRun()
			writer.outboxCtx, writer.outboxOwner = ctx, "matrix-worker"
			require.True(t, writer.replayOutboxBatch())
			var failed model.ChannelMonitorEventOutbox
			require.NoError(t, db.First(&failed, "event_id = ?", pending.EventId).Error)
			assert.Zero(t, failed.ProcessedAt)
			assert.EqualValues(t, 1, failed.AttemptCount)
			assert.Empty(t, failed.LeaseOwner)
			assert.NotEmpty(t, failed.LastError)
			// Make the retry due explicitly, without a wall-clock sleep.
			require.NoError(t, db.Model(&failed).Update("next_attempt_at", time.Now().Unix()).Error)
			writer.client = client
			require.True(t, writer.replayOutboxBatch())
			assert.False(t, writer.replayOutboxBatch())
			var held model.ChannelMonitorEventOutbox
			require.NoError(t, db.First(&held, claimed[0].Id).Error)
			assert.Zero(t, held.ProcessedAt)
			assert.Equal(t, "other-worker", held.LeaseOwner)
			require.NoError(t, model.FailChannelMonitorEventOutbox(ctx, "other-worker", []int64{held.Id}, time.Now().Unix(), assert.AnError))
			require.True(t, writer.replayOutboxBatch())
			assert.False(t, writer.replayOutboxBatch())

			// Successful replay goes back through an actual Redis Stream consumer
			// group; no duplicate delivery is produced by an additional DB scan.
			streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group: group, Consumer: "matrix-consumer", Streams: []string{ChannelMonitorRedisEventStream, ">"}, Count: 10, Block: -1,
			}).Result()
			require.NoError(t, err)
			require.Len(t, streams, 1)
			require.Len(t, streams[0].Messages, 2)
			var eventIDs, messageIDs []string
			for _, message := range streams[0].Messages {
				eventIDs = append(eventIDs, message.Values[ChannelMonitorRedisEventFieldEventID].(string))
				messageIDs = append(messageIDs, message.ID)
				var decoded model.ChannelMonitorEvent
				require.NoError(t, common.UnmarshalJsonStr(message.Values[ChannelMonitorRedisEventFieldPayload].(string), &decoded))
				assert.Equal(t, message.Values[ChannelMonitorRedisEventFieldEventID], decoded.EventId)
			}
			assert.Equal(t, []string{pending.EventId, leased.EventId}, eventIDs)
			require.NoError(t, client.XAck(ctx, ChannelMonitorRedisEventStream, group, messageIDs...).Err())
			require.NoError(t, client.XDel(ctx, ChannelMonitorRedisEventStream, messageIDs...).Err())
			var rows []model.ChannelMonitorEventOutbox
			require.NoError(t, db.Find(&rows).Error)
			require.Len(t, rows, 2)
			for _, row := range rows {
				assert.NotZero(t, row.ProcessedAt)
				assert.Empty(t, row.LeaseOwner)
			}
		})
	}
}
