package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestChannelMonitorCustomVariableDatabaseMatrix(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "variables.db"))
			case "mysql":
				if os.Getenv("TEST_CUSTOM_VARIABLE_MYSQL_DSN") == "" {
					t.Skip("TEST_CUSTOM_VARIABLE_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(os.Getenv("TEST_CUSTOM_VARIABLE_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_CUSTOM_VARIABLE_POSTGRES_DSN") == "" {
					t.Skip("TEST_CUSTOM_VARIABLE_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(os.Getenv("TEST_CUSTOM_VARIABLE_POSTGRES_DSN"))
			}
			databaseConfig := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("variable_%x_", time.Now().UnixNano())}}
			db, err := gorm.Open(dialector, databaseConfig)
			require.NoError(t, err)
			previousDB, previousType := model.DB, common.MainDatabaseType()
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseType(engine))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.ChannelRatioMonitor{}))
				model.DB = previousDB
				common.SetMainDatabaseType(previousType)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			var version string
			if engine == "sqlite" {
				require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
			} else {
				require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
			}
			t.Logf("database version: %s", version)
			require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}))

			t.Run("旧配置可保存变量且重新打开后保留", func(t *testing.T) {
				config := customVariableTestConfig("https://upstream.example", "on_failure", "saved-token")
				legacy := customVariableTestConfig("https://upstream.example", "on_failure", "saved-token").CustomConfig
				legacy.VariableRequests = nil
				legacy.Ratio.Request.Headers = nil
				legacy.Balance.Request.Query = nil
				oldRaw, err := MarshalChannelMonitorCustomUpstreamConfig(legacy)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 11, UpstreamType: CustomUpstreamType, UpstreamRevision: 4, CustomUpstreamConfig: oldRaw, Ratio: 3}).Error)
				newRaw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, model.UpdateChannelMonitorCustomVariableConfig(t.Context(), 11, 4, oldRaw, newRaw))
				reopened, err := gorm.Open(dialector, databaseConfig)
				require.NoError(t, err)
				reopenedSQL, err := reopened.DB()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, reopenedSQL.Close()) })
				require.NoError(t, reopened.AutoMigrate(&model.ChannelRatioMonitor{}))
				var saved model.ChannelRatioMonitor
				require.NoError(t, reopened.Where("channel_id = ?", 11).First(&saved).Error)
				assert.Equal(t, newRaw, saved.CustomUpstreamConfig)
				assert.EqualValues(t, 4, saved.UpstreamRevision)
				assert.Equal(t, 3.0, saved.Ratio)
			})

			t.Run("并发刷新共用结果并持久保存", func(t *testing.T) {
				var refreshCalls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/token" {
						refreshCalls.Add(1)
						_, _ = w.Write([]byte(`{"data":{"token":"new-token"}}`))
						return
					}
					if r.Header.Get("Authorization") != "Bearer new-token" && r.URL.Query().Get("ticket") != "new-token" {
						w.WriteHeader(401)
						return
					}
					_, _ = w.Write([]byte(`{"data":{"ratio":2,"balance":40}}`))
				}))
				defer server.Close()
				config := customVariableTestConfig(server.URL, "on_failure", "old-token")
				config.CredentialID, config.Revision = 12, 5
				raw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 12, UpstreamType: CustomUpstreamType, UpstreamBaseURL: server.URL, UpstreamRevision: 5, CustomUpstreamConfig: raw}).Error)
				results := make(chan error, 2)
				go func() { _, err := FetchChannelMonitorUpstreamGroupRatio(t.Context(), config); results <- err }()
				go func() { _, err := FetchChannelMonitorUpstreamBalance(t.Context(), config); results <- err }()
				require.NoError(t, <-results)
				require.NoError(t, <-results)
				assert.EqualValues(t, 1, refreshCalls.Load())
				saved, err := model.GetChannelRatioMonitor(12)
				require.NoError(t, err)
				parsed, err := ParseChannelMonitorCustomUpstreamConfig(saved.CustomUpstreamConfig)
				require.NoError(t, err)
				assert.Equal(t, "new-token", parsed.VariableRequests[0].Variables[0].Value)
				assert.EqualValues(t, 5, saved.UpstreamRevision)
				assert.Equal(t, "Bearer {{token}}", parsed.Ratio.Request.Headers[0].ValueTemplate)
				err = model.UpdateChannelMonitorCustomVariableConfig(t.Context(), 12, 5, raw, raw)
				require.ErrorIs(t, err, model.ErrChannelRatioMonitorConfigChanged)
				caseChanged := strings.ReplaceAll(saved.CustomUpstreamConfig, "new-token", "NEW-TOKEN")
				err = model.UpdateChannelMonitorCustomVariableConfig(t.Context(), 12, 5, caseChanged, raw)
				require.ErrorIs(t, err, model.ErrChannelRatioMonitorConfigChanged, "大小写不同的凭据不能视为相同")
			})

			t.Run("获取失败保留原值", func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
				defer server.Close()
				config := customVariableTestConfig(server.URL, "always", "old-token")
				config.CredentialID, config.Revision = 13, 6
				raw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 13, UpstreamType: CustomUpstreamType, UpstreamBaseURL: server.URL, UpstreamRevision: 6, CustomUpstreamConfig: raw}).Error)
				_, err = FetchChannelMonitorUpstreamBalance(t.Context(), config)
				require.Error(t, err)
				saved, err := model.GetChannelRatioMonitor(13)
				require.NoError(t, err)
				assert.Equal(t, raw, saved.CustomUpstreamConfig)
			})

			t.Run("多请求只更新选中请求且映射一起保存", func(t *testing.T) {
				var incomplete atomic.Bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/token" {
						if incomplete.Load() {
							_, _ = w.Write([]byte(`{"data":{"token":"discarded-token"}}`))
							return
						}
						_, _ = w.Write([]byte(`{"data":{"token":"new-token","user":42}}`))
						return
					}
					assert.Equal(t, "Bearer new-token", r.Header.Get("Authorization"))
					_, _ = w.Write([]byte(`{"data":{"ratio":2}}`))
				}))
				defer server.Close()
				config := customVariableTestConfig(server.URL, "always", "old-token")
				config.CredentialID, config.Revision, config.SkipBalance = 15, 8, true
				config.CustomConfig.VariableRequests[0].Variables = append(config.CustomConfig.VariableRequests[0].Variables, ChannelMonitorCustomVariable{Name: "user_id", ValuePath: "data.user", Value: "old-id"})
				other := ChannelMonitorCustomVariableRequest{ID: "other", Name: "其他认证", RefreshPolicy: "on_failure", Request: ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/must-not-request"}, ResponseType: "json", Variables: []ChannelMonitorCustomVariable{{Name: "other_token", ValuePath: "data.token", Value: "keep-token"}}}
				config.CustomConfig.VariableRequests = append(config.CustomConfig.VariableRequests, other)
				raw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 15, UpstreamType: CustomUpstreamType, UpstreamBaseURL: server.URL, UpstreamRevision: 8, CustomUpstreamConfig: raw}).Error)
				_, err = FetchChannelMonitorUpstreamGroupRatio(t.Context(), config)
				require.NoError(t, err)
				saved, err := model.GetChannelRatioMonitor(15)
				require.NoError(t, err)
				parsed, err := ParseChannelMonitorCustomUpstreamConfig(saved.CustomUpstreamConfig)
				require.NoError(t, err)
				assert.Equal(t, "new-token", parsed.VariableRequests[0].Variables[0].Value)
				assert.Equal(t, "42", parsed.VariableRequests[0].Variables[1].Value)
				assert.Equal(t, "keep-token", parsed.VariableRequests[1].Variables[0].Value)
				assert.Equal(t, "on_failure", parsed.VariableRequests[1].RefreshPolicy)
				incomplete.Store(true)
				_, err = FetchChannelMonitorUpstreamGroupRatio(t.Context(), config)
				require.ErrorContains(t, err, "user_id")
				afterFailure, err := model.GetChannelRatioMonitor(15)
				require.NoError(t, err)
				assert.Equal(t, saved.CustomUpstreamConfig, afterFailure.CustomUpstreamConfig)
			})

			t.Run("丢弃刷新期间的旧配置结果", func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/token", r.URL.Path)
					assert.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 14).Update("upstream_revision", 8).Error)
					_, _ = w.Write([]byte(`{"data":{"token":"discarded-token"}}`))
				}))
				defer server.Close()
				config := customVariableTestConfig(server.URL, "always", "old-token")
				config.CredentialID, config.Revision = 14, 7
				raw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 14, UpstreamType: CustomUpstreamType, UpstreamBaseURL: server.URL, UpstreamRevision: 7, CustomUpstreamConfig: raw}).Error)
				_, err = FetchChannelMonitorUpstreamBalance(t.Context(), config)
				require.ErrorIs(t, err, model.ErrChannelRatioMonitorConfigChanged)
				saved, err := model.GetChannelRatioMonitor(14)
				require.NoError(t, err)
				assert.Equal(t, raw, saved.CustomUpstreamConfig)
				assert.EqualValues(t, 8, saved.UpstreamRevision)
			})
		})
	}
}
