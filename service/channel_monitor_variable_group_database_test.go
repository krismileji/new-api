package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestChannelMonitorVariableGroupDatabaseMatrix(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "shared.db"))
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
			dbConfig := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("shared_%x_", time.Now().UnixNano())}}
			db, err := gorm.Open(dialector, dbConfig)
			require.NoError(t, err)
			previousDB, previousType := model.DB, common.MainDatabaseType()
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseType(engine))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorVariableGroup{}, &model.ChannelRatioMonitor{}, &model.Channel{}))
				model.DB = previousDB
				common.SetMainDatabaseType(previousType)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			var version string
			versionSQL := "SELECT version()"
			if engine == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)

			// The previous schema has only per-channel JSON. Adding the shared
			// table must preserve legacy configurations and their primary keys.
			require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.Channel{}))
			legacy := customVariableTestConfig("https://legacy.example", "on_failure", "legacy-token")
			legacyRaw, err := MarshalChannelMonitorCustomUpstreamConfig(legacy.CustomConfig)
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 90, UpstreamType: CustomUpstreamType, UpstreamBaseURL: legacy.BaseURL, CustomUpstreamConfig: legacyRaw}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.ChannelMonitorVariableGroup{}, &model.ChannelRatioMonitor{}))
			}
			savedLegacy, err := model.GetChannelRatioMonitor(90)
			require.NoError(t, err)
			assert.Equal(t, legacyRaw, savedLegacy.CustomUpstreamConfig)
			require.Error(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 90}).Error)

			var refreshCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					refreshCalls.Add(1)
					assert.Equal(t, "login-password", r.FormValue("password"))
					_, _ = w.Write([]byte(`{"data":{"token":"shared-token"}}`))
					return
				}
				if r.Header.Get("Authorization") != "Bearer shared-token" && r.URL.Query().Get("ticket") != "shared-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.URL.Path == "/ratio" {
					assert.Contains(t, []string{"channel-key-11", "channel-key-12"}, r.Header.Get("X-Api-Key"))
				}
				_, _ = w.Write([]byte(`{"data":{"ratio":2,"balance":40}}`))
			}))
			defer server.Close()
			config := customVariableTestConfig(server.URL, "on_failure", "old-token")
			config.CustomConfig.VariableRequests[0].Request.Form[0].Secret = true
			input := ChannelMonitorVariableGroupConfig{Name: "共用登录", BaseURL: server.URL, RequestTimeout: 30, VariableRequests: config.CustomConfig.VariableRequests}
			view, err := SaveChannelMonitorVariableGroup(t.Context(), input)
			require.NoError(t, err)
			require.Positive(t, view.ID)
			assert.Empty(t, view.VariableRequests[0].Variables[0].Value)
			assert.True(t, view.VariableRequests[0].Variables[0].HasValue)
			assert.Empty(t, view.VariableRequests[0].Request.Form[0].Value)
			assert.True(t, view.VariableRequests[0].Request.Form[0].HasValue)
			initial, err := model.GetChannelMonitorVariableGroup(t.Context(), view.ID)
			require.NoError(t, err)
			invalid := config.CustomConfig
			invalid.VariableGroupID = view.ID
			_, err = NormalizeChannelMonitorCustomUpstreamConfig(invalid)
			require.Error(t, err, "不能把共享引用与渠道独立请求一起保存")

			config.CustomConfig.VariableRequests = nil
			config.CustomConfig.VariableGroupID = view.ID
			results := make(chan error, 2)
			for _, channelID := range []int{11, 12} {
				channelConfig := config
				channelConfig.CredentialID, channelConfig.Revision = channelID, 1
				metric := *config.CustomConfig.Ratio.Request
				metric.Headers = append(append([]ChannelMonitorCustomKeyValue(nil), metric.Headers...), ChannelMonitorCustomKeyValue{Key: "X-Api-Key", Value: fmt.Sprintf("channel-key-%d", channelID)})
				channelConfig.CustomConfig.Ratio.Request = &metric
				raw, err := MarshalChannelMonitorCustomUpstreamConfig(channelConfig.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "共享渠道", Key: "channel-key", Group: "default"}).Error)
				_, err = model.SaveChannelRatioUpstreamConfig(channelID, CustomUpstreamType, server.URL, "", CustomUpstreamAuthType, 0, "", model.ChannelRatioUpstreamOptions{CustomUpstreamConfig: raw})
				require.NoError(t, err)
				go func() { _, err := FetchChannelMonitorUpstreamGroupRatio(t.Context(), channelConfig); results <- err }()
			}
			require.NoError(t, <-results)
			require.NoError(t, <-results)
			assert.EqualValues(t, 1, refreshCalls.Load(), "两个渠道共用一次刷新后的凭据")
			for _, channelID := range []int{11, 12} {
				monitor, err := model.GetChannelRatioMonitor(channelID)
				require.NoError(t, err)
				parsed, err := ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
				require.NoError(t, err)
				assert.Equal(t, view.ID, parsed.VariableGroupID)
				assert.Empty(t, parsed.VariableRequests)
				assert.NotContains(t, monitor.CustomUpstreamConfig, "shared-token")
			}
			refreshed, err := model.GetChannelMonitorVariableGroup(t.Context(), view.ID)
			require.NoError(t, err)
			assert.Contains(t, refreshed.Config, "shared-token")
			assert.Equal(t, initial.Revision, refreshed.Revision)
			require.ErrorIs(t, model.RefreshChannelMonitorVariableGroup(t.Context(), initial, initial.Config), model.ErrChannelMonitorVariableGroupChanged)

			t.Run("独立编辑保留刷新值并拒绝破坏引用", func(t *testing.T) {
				view.Name = "改名后的共享登录"
				saved, err := SaveChannelMonitorVariableGroup(t.Context(), view)
				require.NoError(t, err)
				stored, err := model.GetChannelMonitorVariableGroup(t.Context(), view.ID)
				require.NoError(t, err)
				assert.Contains(t, stored.Config, "shared-token")
				monitor, err := model.GetChannelRatioMonitor(11)
				require.NoError(t, err)
				assert.EqualValues(t, 2, monitor.UpstreamRevision)
				_, err = model.SaveChannelRatioUpstreamConfig(11, CustomUpstreamType, server.URL, "", CustomUpstreamAuthType, 0, "", model.ChannelRatioUpstreamOptions{CustomUpstreamConfig: monitor.CustomUpstreamConfig, VariableGroupRevision: initial.Revision})
				require.ErrorIs(t, err, model.ErrChannelMonitorVariableGroupChanged, "共享配置在校验后变更时，渠道不能保存过期引用")
				_, err = SaveChannelMonitorVariableGroup(t.Context(), view)
				require.ErrorIs(t, err, model.ErrChannelMonitorVariableGroupChanged)
				broken := saved
				broken.VariableRequests = append([]ChannelMonitorCustomVariableRequest(nil), saved.VariableRequests...)
				broken.VariableRequests[0].Variables = []ChannelMonitorCustomVariable{{Name: "renamed", ValuePath: "data.token"}}
				_, err = SaveChannelMonitorVariableGroup(t.Context(), broken)
				require.ErrorContains(t, err, "仍在使用")
				require.ErrorContains(t, model.DeleteChannelMonitorVariableGroup(t.Context(), saved.ID, saved.Revision), "仍被渠道引用")
				view = saved
			})

			t.Run("草稿提取保留隐藏密码且不写共享值", func(t *testing.T) {
				before, err := model.GetChannelMonitorVariableGroup(t.Context(), view.ID)
				require.NoError(t, err)
				variables, err := FetchChannelMonitorVariableGroupDraft(t.Context(), view, "login")
				require.NoError(t, err)
				assert.Equal(t, "shared-token", variables[0].Value)
				after, err := model.GetChannelMonitorVariableGroup(t.Context(), view.ID)
				require.NoError(t, err)
				assert.Equal(t, before, after)
				changedHost := view
				changedHost.BaseURL = "https://other.example"
				_, _, err = PrepareChannelMonitorVariableGroup(t.Context(), changedHost)
				require.Error(t, err, "换地址不能沿用隐藏的登录密码")
			})
			t.Run("触发规则使用共享变量且保留渠道独立请求参数", func(t *testing.T) {
				monitor, err := model.GetChannelRatioMonitor(11)
				require.NoError(t, err)
				action := ChannelMonitorCustomAction{Request: ChannelMonitorCustomRequestConfig{
					Method: "POST", Path: "/trigger",
					Headers: []ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}, {Key: "X-Api-Key", Value: "action-channel-key"}},
				}}
				request, err := prepareChannelMonitorCustomActionRequest(t.Context(), server.Client(), monitor, action)
				require.NoError(t, err)
				assert.Equal(t, "Bearer shared-token", request.Headers[0].Value)
				assert.Equal(t, "action-channel-key", request.Headers[1].Value)
				assert.Empty(t, request.Headers[0].ValueTemplate)
			})

			reopened, err := gorm.Open(dialector, dbConfig)
			require.NoError(t, err)
			reopenedSQL, err := reopened.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reopenedSQL.Close()) })
			for range 2 {
				require.NoError(t, reopened.AutoMigrate(&model.ChannelMonitorVariableGroup{}, &model.ChannelRatioMonitor{}))
			}
			var retained model.ChannelMonitorVariableGroup
			require.NoError(t, reopened.First(&retained, view.ID).Error)
			assert.Equal(t, view.Name, retained.Name)
			assert.Contains(t, retained.Config, "shared-token")
			require.NoError(t, db.Where("channel_id IN ?", []int{11, 12}).Delete(&model.ChannelRatioMonitor{}).Error)
			require.NoError(t, model.DeleteChannelMonitorVariableGroup(t.Context(), view.ID, view.Revision))
			_, err = model.SaveChannelRatioUpstreamConfig(11, CustomUpstreamType, server.URL, "", CustomUpstreamAuthType, 0, "", model.ChannelRatioUpstreamOptions{CustomUpstreamConfig: fmt.Sprintf(`{"variable_group_id":%d}`, view.ID)})
			require.ErrorContains(t, err, "不存在")
		})
	}
}
