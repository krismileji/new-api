package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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

func TestChannelMonitorCustomActionDatabaseMatrix(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "actions.db"))
			case "mysql":
				dsn := os.Getenv("TEST_CUSTOM_ACTION_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_CUSTOM_ACTION_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_CUSTOM_ACTION_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_CUSTOM_ACTION_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(dsn)
			}
			cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("action_%x_", time.Now().UnixNano())}}
			db, err := gorm.Open(dialector, cfg)
			require.NoError(t, err)
			previousDB, previousType := model.DB, common.MainDatabaseType()
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseType(engine))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.SystemTask{}, &model.ChannelRatioMonitor{}))
				model.DB = previousDB
				common.SetMainDatabaseType(previousType)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			var version string
			query := "SELECT version()"
			if engine == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("数据库版本: %s", version)
			require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.SystemTask{}))

			t.Run("旧配置升级并发触发及重启后次数仍受限", func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, "POST", r.Method)
					assert.Equal(t, "/reset", r.URL.Path)
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.JSONEq(t, `{"reset":true}`, string(body))
					_, _ = w.Write([]byte(`{"success":true}`))
				}))
				defer server.Close()
				config := customVariableTestConfig(server.URL, "on_failure", "saved-token").CustomConfig
				raw, err := MarshalChannelMonitorCustomUpstreamConfig(config)
				require.NoError(t, err)
				balance := 5.0
				monitor := model.ChannelRatioMonitor{ChannelId: 1, UpstreamType: CustomUpstreamType, UpstreamBaseURL: server.URL, UpstreamRevision: 1, CustomUpstreamConfig: raw, UpstreamBalance: &balance}
				require.NoError(t, db.Create(&monitor).Error)
				config.Actions = []ChannelMonitorCustomAction{customActionTestRule()}
				updated, err := MarshalChannelMonitorCustomUpstreamConfig(config)
				require.NoError(t, err)
				require.NoError(t, model.UpdateChannelMonitorCustomVariableConfig(t.Context(), 1, 1, raw, updated))
				monitor.CustomUpstreamConfig = updated
				now, err := time.Parse(time.RFC3339, "2026-09-13T04:00:00Z")
				require.NoError(t, err)
				clock := func() time.Time { return now }
				errors := make(chan error, 2)
				var wg sync.WaitGroup
				for range 2 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, err := runChannelMonitorCustomAction(t.Context(), monitor, config.Actions[0], 5, "", time.Second, true, clock)
						errors <- err
					}()
				}
				wg.Wait()
				require.NoError(t, <-errors)
				require.NoError(t, <-errors)
				assert.EqualValues(t, 1, calls.Load())
				states, err := model.GetChannelMonitorCustomActionStates(1)
				require.NoError(t, err)
				assert.Equal(t, "succeeded", states["reset"].Status)
				reopened, err := gorm.Open(dialector, cfg)
				require.NoError(t, err)
				sqlDB, err := reopened.DB()
				require.NoError(t, err)
				defer sqlDB.Close()
				require.NoError(t, reopened.AutoMigrate(&model.ChannelRatioMonitor{}, &model.SystemTask{}))
				model.DB = reopened
				defer func() { model.DB = db }()
				succeeded, err := runChannelMonitorCustomAction(t.Context(), monitor, config.Actions[0], 5, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.False(t, succeeded)
				assert.EqualValues(t, 1, calls.Load(), "持续满足不重复触发")
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 1).Update("upstream_balance", 20).Error)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, config.Actions[0], 20, "", time.Second, true, clock)
				require.NoError(t, err)
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 1).Update("upstream_balance", 5).Error)
				now = now.Add(2 * time.Hour)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, config.Actions[0], 5, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load(), "恢复后再触发仍受每日次数限制")
				now = now.Add(24 * time.Hour)
				succeeded, err = runChannelMonitorCustomAction(t.Context(), monitor, config.Actions[0], 5, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.True(t, succeeded)
				assert.EqualValues(t, 2, calls.Load())
				states, err = model.GetChannelMonitorCustomActionStates(1)
				require.NoError(t, err)
				assert.Equal(t, 1, states["reset"].Attempts)
				assert.Equal(t, "2026-09-14", states["reset"].Day)
			})

			t.Run("失败不重试且满足冷却后恢复才可再次执行", func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"token":"secret-response"}`))
				}))
				defer server.Close()
				action := customActionTestRule()
				action.DailyLimit = 3
				monitor := createCustomActionTestMonitor(t, db, 2, server.URL, action)
				now, err := time.Parse(time.RFC3339, "2026-09-13T04:00:00Z")
				require.NoError(t, err)
				clock := func() time.Time { return now }
				succeeded, err := runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.ErrorContains(t, err, "401")
				assert.False(t, succeeded)
				assert.NotContains(t, err.Error(), "secret-response")
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.NoError(t, err)
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 2).Update("upstream_balance", 20).Error)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 20, "", time.Second, true, clock)
				require.NoError(t, err)
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 2).Update("upstream_balance", 5).Error)
				now = now.Add(59 * time.Minute)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load())
				now = now.Add(time.Minute)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.Error(t, err)
				assert.EqualValues(t, 2, calls.Load())
				states, err := model.GetChannelMonitorCustomActionStates(2)
				require.NoError(t, err)
				assert.Equal(t, 2, states[action.ID].Attempts)
				assert.Equal(t, "failed", states[action.ID].Status)
			})

			t.Run("截止时间和旧结果不执行", func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(`{"success":true}`)) }))
				defer server.Close()
				action := customActionTestRule()
				monitor := createCustomActionTestMonitor(t, db, 3, server.URL, action)
				now, err := time.Parse(time.RFC3339, "2026-09-13T15:00:00Z")
				require.NoError(t, err)
				clock := func() time.Time { return now }
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.Zero(t, calls.Load())
				now = now.Add(-time.Hour)
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 3).Update("upstream_balance", 20).Error)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.Zero(t, calls.Load())
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 3).Update("upstream_revision", 2).Error)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, clock)
				require.ErrorIs(t, err, model.ErrChannelRatioMonitorConfigChanged)
			})

			t.Run("凭据准备跨过截止时间不发起重置", func(t *testing.T) {
				var calls atomic.Int32
				var timestamp atomic.Int64
				beforeCutoff, err := time.Parse(time.RFC3339, "2026-09-13T14:59:59Z")
				require.NoError(t, err)
				timestamp.Store(beforeCutoff.Unix())
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/token" {
						timestamp.Store(beforeCutoff.Add(time.Second).Unix())
						_, _ = w.Write([]byte(`{"data":{"token":"fresh-token"}}`))
						return
					}
					calls.Add(1)
					_, _ = w.Write([]byte(`{"success":true}`))
				}))
				defer server.Close()
				action := customActionTestRule()
				action.Request.Headers = []ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}}
				monitor := createCustomActionTestMonitor(t, db, 4, server.URL, action)
				config := customVariableTestConfig(server.URL, "always", "").CustomConfig
				config.Actions = []ChannelMonitorCustomAction{action}
				monitor.CustomUpstreamConfig, err = MarshalChannelMonitorCustomUpstreamConfig(config)
				require.NoError(t, err)
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 4).Update("custom_upstream_config", monitor.CustomUpstreamConfig).Error)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, func() time.Time { return time.Unix(timestamp.Load(), 0) })
				require.ErrorContains(t, err, "允许执行时段")
				assert.Zero(t, calls.Load())
			})

			t.Run("倍率触发使用变量且成功判定失败不重试", func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					assert.Equal(t, "Bearer saved-token", r.Header.Get("Authorization"))
					_, _ = w.Write([]byte(`{"success":false}`))
				}))
				defer server.Close()
				action := customActionTestRule()
				action.Metric, action.Operator = "ratio", "gte"
				threshold := 2.0
				action.Threshold = &threshold
				action.Request.Headers = []ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}}
				monitor := createCustomActionTestMonitor(t, db, 5, server.URL, action)
				now, err := time.Parse(time.RFC3339, "2026-09-13T04:00:00Z")
				require.NoError(t, err)
				clock := func() time.Time { return now }
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 2, "", time.Second, true, clock)
				require.ErrorContains(t, err, "成功判定")
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 2, "", time.Second, true, clock)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load())
				states, err := model.GetChannelMonitorCustomActionStates(5)
				require.NoError(t, err)
				assert.Equal(t, "failed", states[action.ID].Status)
			})

			t.Run("GET接口结果未知不被连接池自动重放", func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/prime" {
						_, _ = w.Write([]byte("ok"))
						return
					}
					if calls.Add(1) == 1 {
						conn, _, err := w.(http.Hijacker).Hijack()
						assert.NoError(t, err)
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					_, _ = w.Write([]byte(`{"success":true}`))
				}))
				defer server.Close()
				response, err := httpClient.Get(server.URL + "/prime")
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				action := customActionTestRule()
				action.Request = ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/reset"}
				monitor := createCustomActionTestMonitor(t, db, 6, server.URL, action)
				now, err := time.Parse(time.RFC3339, "2026-09-13T04:00:00Z")
				require.NoError(t, err)
				_, err = runChannelMonitorCustomAction(t.Context(), monitor, action, 5, "", time.Second, true, func() time.Time { return now })
				require.ErrorContains(t, err, "结果未知")
				assert.EqualValues(t, 1, calls.Load())
			})
		})
	}
}

func createCustomActionTestMonitor(t *testing.T, db *gorm.DB, channelID int, baseURL string, action ChannelMonitorCustomAction) model.ChannelRatioMonitor {
	t.Helper()
	config := customVariableTestConfig(baseURL, "on_failure", "saved-token").CustomConfig
	config.Actions = []ChannelMonitorCustomAction{action}
	raw, err := MarshalChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	balance := 5.0
	monitor := model.ChannelRatioMonitor{ChannelId: channelID, UpstreamType: CustomUpstreamType, UpstreamBaseURL: baseURL, UpstreamRevision: 1, CustomUpstreamConfig: raw, UpstreamBalance: &balance, Ratio: 2, LastFetchStatus: model.ChannelRatioFetchStatusSucceeded}
	require.NoError(t, db.Create(&monitor).Error)
	return monitor
}
