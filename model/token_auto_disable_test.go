package model

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTokenAutoDisableDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialect gorm.Dialector
			switch engine {
			case "sqlite":
				dialect = sqlite.Open(t.TempDir() + "/tokens.db")
			case "mysql":
				dsn := os.Getenv("TEST_TOKEN_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_TOKEN_MYSQL_DSN 未设置")
				}
				require.Contains(t, dsn, "127.0.0.1:33379")
				require.True(t, strings.Contains(dsn, "/new_api_token_test"))
				dialect = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_TOKEN_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_TOKEN_POSTGRES_DSN 未设置")
				}
				require.Contains(t, dsn, "127.0.0.1:35439")
				require.Contains(t, dsn, "/new_api_token_test")
				dialect = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			oldType := common.MainDatabaseType()
			common.SetMainDatabaseType(common.DatabaseType(engine))
			oldDB, oldRedis := DB, common.RedisEnabled
			DB, common.RedisEnabled = db, false
			t.Cleanup(func() {
				common.SetMainDatabaseType(oldType)
				DB, common.RedisEnabled = oldDB, oldRedis
				connection, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.Migrator().DropTable(&VerifiedNotificationEmail{}, &ChannelMonitorRequestStatsDisable{}, &ChannelMonitorRequestStatsDay{}, &ChannelMonitorRequestStatsConfig{}, &TokenAutoDisableRecord{}, &TokenAutoDisableConfig{}, &Token{}))
			for range 2 {
				require.NoError(t, db.AutoMigrate(&Token{}, &TokenAutoDisableConfig{}, &TokenAutoDisableRecord{}))
			}
			var version string
			query := "SELECT version()"
			if engine == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)
			token := Token{UserId: 42, Key: "matrix-key", Name: "保留名称", Status: common.TokenStatusEnabled, RemainQuota: 12345, UsedQuota: 789, ExpiredTime: -1}
			require.NoError(t, db.Create(&token).Error)
			statsToken := Token{UserId: 42, Key: "matrix-request-stats-key", Name: "统计 Key", Status: common.TokenStatusEnabled, RemainQuota: 500, ExpiredTime: -1}
			require.NoError(t, db.Create(&statsToken).Error)
			// Additive upgrade over an existing token database, followed by a second
			// migration below to verify schema idempotency.
			require.NoError(t, db.AutoMigrate(&ChannelMonitorRequestStatsConfig{}, &ChannelMonitorRequestStatsDay{}, &ChannelMonitorRequestStatsDisable{}, &VerifiedNotificationEmail{}))
			config, err := GetChannelMonitorRequestStatsConfig(t.Context())
			require.NoError(t, err)
			assert.Equal(t, 10, config.LowInputThresholdK)
			config, err = SaveChannelMonitorRequestStatsConfig(t.Context(), config.Revision, map[string]any{
				"low_input_threshold_k": 12, "enabled": true, "disable_rate_percent": 80, "minimum_known_input_requests": int64(100),
			})
			require.NoError(t, err)
			day := ChannelDailyCostDayStart(time.Now().Unix()) + 86400
			decision, frozenConfig, err := FreezeRequestStatsDay(t.Context(), day, config)
			require.NoError(t, err)
			assert.Equal(t, "pending", decision.Status)
			assert.Equal(t, 12, frozenConfig.LowInputThresholdK)
			config, err = SaveChannelMonitorRequestStatsConfig(t.Context(), config.Revision, map[string]any{"low_input_threshold_k": 13})
			require.NoError(t, err)
			decision, frozenConfig, err = FreezeRequestStatsDay(t.Context(), day, config)
			require.NoError(t, err)
			assert.Equal(t, 12, frozenConfig.LowInputThresholdK, "a retry must retain its original policy snapshot")
			require.NoError(t, RecordVerifiedNotificationEmail(db, 42, "  USER@Example.COM "))
			require.NoError(t, RecordVerifiedNotificationEmail(db, 42, "new@example.com"))
			var verified VerifiedNotificationEmail
			require.NoError(t, db.First(&verified, "user_id = ?", 42).Error)
			assert.Equal(t, "new@example.com", verified.Email)
			assert.Positive(t, verified.VerifiedAt)

			changed, err := DisableRequestStatsToken(t.Context(), ChannelMonitorRequestStatsDisable{
				DayStart: day, APIKeyId: statsToken.Id, UserId: statsToken.UserId, ConfigRevision: config.Revision,
				ThresholdK: 13, DisableRatePercent: 80, MinimumKnownInputRequests: 100, KnownInputRequests: 100, LowInputCount: 90,
			})
			require.NoError(t, err)
			assert.True(t, changed)
			changed, err = DisableRequestStatsToken(t.Context(), ChannelMonitorRequestStatsDisable{
				DayStart: day, APIKeyId: statsToken.Id, UserId: statsToken.UserId,
			})
			require.NoError(t, err)
			assert.False(t, changed)
			var requestStatsRecord ChannelMonitorRequestStatsDisable
			require.NoError(t, db.First(&requestStatsRecord, "day_start = ? AND api_key_id = ?", day, statsToken.Id).Error)
			var persistedStatsToken Token
			require.NoError(t, db.First(&persistedStatsToken, statsToken.Id).Error)
			assert.Equal(t, common.TokenStatusDisabled, persistedStatsToken.Status)
			statsToken.Status = common.TokenStatusEnabled
			require.NoError(t, UpdateTokenWithRequestStats(&statsToken, RequestStatsTokenMutation{StatusOnly: true, RestoreSource: "user"}))
			require.NoError(t, db.First(&requestStatsRecord, requestStatsRecord.Id).Error)
			assert.Positive(t, requestStatsRecord.RestoredAt)
			assert.Equal(t, "user", requestStatsRecord.RestoredBy)
			require.NoError(t, db.First(&persistedStatsToken, statsToken.Id).Error)
			assert.Equal(t, common.TokenStatusEnabled, persistedStatsToken.Status)
			changed, err = DisableRequestStatsToken(t.Context(), ChannelMonitorRequestStatsDisable{DayStart: day, APIKeyId: statsToken.Id, UserId: statsToken.UserId})
			require.NoError(t, err)
			assert.False(t, changed, "a restored key must not be disabled twice for the same day")

			record := TokenAutoDisableRecord{Id: "matrix-incident", TokenId: token.Id, UserId: token.UserId, RuleName: "测试规则", RuleSnapshot: `{"keywords":["policy"]}`, ResponseStatus: 451, ResponseMessage: "禁止访问", CreatedAt: 1}
			require.NoError(t, PersistTokenAutoDisable(t.Context(), record))
			require.NoError(t, PersistTokenAutoDisable(t.Context(), record))
			var persisted Token
			require.NoError(t, db.First(&persisted, token.Id).Error)
			assert.Equal(t, common.TokenStatusDisabled, persisted.Status)
			assert.Equal(t, token.RemainQuota, persisted.RemainQuota)
			assert.Equal(t, token.UsedQuota, persisted.UsedQuota)
			records, total, err := ListTokenAutoDisables(t.Context(), 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, records, 1)
			require.NoError(t, SaveTokenAutoDisableConfig(t.Context(), TokenAutoDisableConfig{Revision: 0, Enabled: true, Rules: "[]"}))
			require.ErrorIs(t, SaveTokenAutoDisableConfig(t.Context(), TokenAutoDisableConfig{Revision: 0, Rules: "[]"}), ErrTokenAutoDisableConflict)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&Token{}, &TokenAutoDisableConfig{}, &TokenAutoDisableRecord{}, &ChannelMonitorRequestStatsConfig{}, &ChannelMonitorRequestStatsDay{}, &ChannelMonitorRequestStatsDisable{}, &VerifiedNotificationEmail{}))
			}
			active, err := LoadActiveTokenAutoDisables(t.Context())
			require.NoError(t, err)
			require.Len(t, active, 1)
			assert.Equal(t, "禁止访问", active[0].ResponseMessage)
			assert.Equal(t, record.RuleSnapshot, active[0].RuleSnapshot)
			require.NoError(t, ReleaseTokenAutoDisable(t.Context(), record.Id, 99))
			require.ErrorIs(t, ReleaseTokenAutoDisable(t.Context(), record.Id, 99), ErrTokenAutoDisableConflict)
			require.NoError(t, db.First(&persisted, token.Id).Error)
			assert.Equal(t, common.TokenStatusEnabled, persisted.Status)
			require.Error(t, db.Create(&Token{Key: "matrix-key"}).Error, "token key uniqueness must survive migration")
			require.Error(t, db.Create(&record).Error, "incident identity must remain unique")
			require.NoError(t, db.First(&requestStatsRecord, requestStatsRecord.Id).Error)
			assert.Equal(t, "user", requestStatsRecord.RestoredBy)
		})
	}
}
