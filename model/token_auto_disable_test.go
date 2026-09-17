package model

import (
	"os"
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
			require.NoError(t, db.Migrator().DropTable(&TokenAutoDisableRecord{}, &TokenAutoDisableConfig{}, &Token{}))
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
				require.NoError(t, db.AutoMigrate(&Token{}, &TokenAutoDisableConfig{}, &TokenAutoDisableRecord{}))
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
		})
	}
}
