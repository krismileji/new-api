package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPrimaryManagementPreserveUntilDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			databaseType := common.DatabaseTypeSQLite
			switch engine {
			case "mysql":
				dsn := os.Getenv("PIN_MANAGEMENT_MYSQL_DSN")
				if dsn == "" {
					t.Skip("PIN_MANAGEMENT_MYSQL_DSN is not set")
				}
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_pin_management_test", config.DBName)
				dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("PIN_MANAGEMENT_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("PIN_MANAGEMENT_POSTGRES_DSN is not set")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "/new_api_pin_management_test", parsed.Path)
				dialector, databaseType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
			default:
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "pins.db"))
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
			oldDB, oldLog := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldMemory, oldRedis, oldMaster := common.MemoryCacheEnabled, common.RedisEnabled, common.IsMasterNode
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(databaseType, databaseType)
			common.MemoryCacheEnabled, common.RedisEnabled, common.IsMasterNode = false, false, false
			t.Setenv("LOG_SQL_DSN", "")
			require.NoError(t, model.InitLogDB())
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLog
				common.SetDatabaseTypes(oldMainType, oldLogType)
				assert.NoError(t, model.InitLogDB())
				model.LOG_DB = oldLog
				common.SetDatabaseTypes(oldMainType, oldLogType)
				common.MemoryCacheEnabled, common.RedisEnabled, common.IsMasterNode = oldMemory, oldRedis, oldMaster
			})
			t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "false")
			useChannelMonitorOptionMap(t, map[string]string{channelMonitorSmartScheduleEnabledOption: "false"})
			gin.SetMode(gin.TestMode)
			tables := []any{&model.Channel{}, &model.Ability{}, &model.ChannelSmartScheduleRouteState{},
				&model.ChannelSmartScheduleChannelPrimaryRule{}, &model.ChannelSmartScheduleGroupPause{},
				&model.ChannelSmartScheduleModelSampleState{}, &model.Option{}, &model.User{}, &model.AuditLog{}}
			for _, table := range tables {
				require.False(t, db.Migrator().HasTable(table), "use an empty disposable database")
			}
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			require.NoError(t, db.AutoMigrate(tables...))
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)

			for index, kind := range []string{"route", "channel"} {
				for number, permanent := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/permanent=%t", kind, permanent), func(t *testing.T) {
						id := 9001 + index*10 + number
						modelName := fmt.Sprintf("model-%d", id)
						priority, weight := int64(80), uint(100)
						require.NoError(t, db.Create(&model.Channel{Id: id, Name: "固定渠道", Status: common.ChannelStatusEnabled,
							Group: "vip", Models: modelName, Priority: &priority, Weight: &weight}).Error)
						require.NoError(t, db.Create(&model.Ability{ChannelId: id, Group: "vip", Model: modelName,
							Enabled: true, Priority: &priority, Weight: weight}).Error)
						require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
							ChannelId: id, GroupName: "vip", ModelName: modelName, ParticipationSet: true}).Error)
						update := UpdateChannelMonitorSmartScheduleRoutePrimary
						target := fmt.Sprintf("/api/channel_monitor/channel/%d/schedule/route/primary", id)
						if kind == "channel" {
							update = UpdateChannelMonitorSmartScheduleChannelPrimaryRule
							target = fmt.Sprintf("/api/channel_monitor/channel/%d/schedule/primary-rule", id)
						}
						request := map[string]any{"group": "vip", "model": modelName, "duration_minutes": 60}
						ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, target, request)
						ctx.AddParam("id", fmt.Sprint(id))
						update(ctx)
						var response struct {
							Success bool
							Message string
							Code    string
							Data    struct {
								ManualPrimaryUntil int64 `json:"manual_primary_until"`
							}
						}
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
						require.True(t, response.Success, recorder.Body.String())
						until := common.GetTimestamp() + 3671
						if permanent {
							until = common.ChannelMonitorSmartSchedulePermanentUntil
						}
						var targetModel any = &model.ChannelSmartScheduleRouteState{}
						if kind == "channel" {
							targetModel = &model.ChannelSmartScheduleChannelPrimaryRule{}
						}
						require.NoError(t, db.Model(targetModel).Where("channel_id = ?", id).Update("manual_primary_until", until).Error)
						request["preserve_until"] = true
						// Contradictory renewal/removal requests must fail before mutating state.
						for _, duration := range []int{60, 0} {
							request["duration_minutes"] = duration
							ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, target, request)
							ctx.AddParam("id", fmt.Sprint(id))
							update(ctx)
							assert.Equal(t, http.StatusBadRequest, recorder.Code)
						}
						delete(request, "duration_minutes")
						if kind == "channel" {
							request["group"] = ""
							ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, target, request)
							ctx.AddParam("id", fmt.Sprint(id))
							update(ctx)
							require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
							assert.False(t, response.Success, "preserve_until must not change a rule's scope")
							request["group"] = "vip"
						}
						request["allow_stability_degrade"] = false
						ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, target, request)
						ctx.AddParam("id", fmt.Sprint(id))
						update(ctx)
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
						require.True(t, response.Success, recorder.Body.String())
						assert.Equal(t, until, response.Data.ManualPrimaryUntil)
						var state model.ChannelSmartScheduleRouteState
						require.NoError(t, db.Where("channel_id = ?", id).First(&state).Error)
						assert.Equal(t, until, state.ManualPrimaryUntil)
						assert.False(t, state.ManualPrimaryAllowStabilityDegrade)

						// Preserving expiry must not bypass the existing protection confirmation.
						protection := map[string]any{"stability_state": model.ChannelSmartScheduleStabilityDegraded}
						if kind == "channel" {
							// A newly eligible protected route has not yet inherited this rule.
							protection["manual_primary_until"] = 0
							protection["manual_primary_channel_rule"] = false
							protection["manual_primary_saved"] = false
						}
						require.NoError(t, db.Model(&state).Updates(protection).Error)
						ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, target, request)
						ctx.AddParam("id", fmt.Sprint(id))
						update(ctx)
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
						assert.False(t, response.Success)
						assert.Contains(t, response.Code, "stability_confirmation_required")
						request["confirm_stability_override"] = true
						ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, target, request)
						ctx.AddParam("id", fmt.Sprint(id))
						update(ctx)
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
						require.True(t, response.Success, recorder.Body.String())
						assert.Equal(t, until, response.Data.ManualPrimaryUntil)
						for _, expiredUntil := range []int64{common.GetTimestamp() - 1, 0} {
							require.NoError(t, db.Model(targetModel).Where("channel_id = ?", id).Update("manual_primary_until", expiredUntil).Error)
							ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, target, request)
							ctx.AddParam("id", fmt.Sprint(id))
							update(ctx)
							require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
							assert.False(t, response.Success, "expired or cleared pins cannot be revived by preserve_until")
							var storedUntil int64
							require.NoError(t, db.Model(targetModel).Where("channel_id = ?", id).Select("manual_primary_until").Scan(&storedUntil).Error)
							assert.Equal(t, expiredUntil, storedUntil)
						}
					})
				}
			}
		})
	}
}
