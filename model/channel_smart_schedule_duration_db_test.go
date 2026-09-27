package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSmartSchedulePermanentDurationDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			databaseType := common.DatabaseTypeSQLite
			switch engine {
			case "mysql":
				dsn := os.Getenv("SCHEDULE_FIX_MYSQL_DSN")
				if dsn == "" {
					t.Skip("SCHEDULE_FIX_MYSQL_DSN is not set")
				}
				dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("SCHEDULE_FIX_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("SCHEDULE_FIX_POSTGRES_DSN is not set")
				}
				dialector, databaseType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
			default:
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "permanent-duration.db"))
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			originalDB, originalMemory := DB, common.MemoryCacheEnabled
			originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
			common.SetDatabaseTypes(databaseType, originalLog)
			initCol()
			common.MemoryCacheEnabled = false
			t.Cleanup(func() {
				DB, common.MemoryCacheEnabled = originalDB, originalMemory
				common.SetDatabaseTypes(originalMain, originalLog)
				initCol()
			})
			t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")
			useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"duration-test","models":["model-a"]}]`)
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &ChannelSmartScheduleRouteState{},
				&ChannelSmartScheduleGroupPause{}, &ChannelSmartScheduleModelSampleState{}))
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)

			db = db.Begin()
			require.NoError(t, db.Error)
			DB = db
			t.Cleanup(func() { require.NoError(t, db.Rollback().Error) })
			priority := int64(80)
			weight := uint(100)
			channel := Channel{
				Name: "permanent duration", Status: common.ChannelStatusEnabled,
				Group: "duration-test", Models: "model-a", Priority: &priority, Weight: &weight,
			}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&Ability{
				ChannelId: channel.Id, Group: channel.Group, Model: channel.Models,
				Enabled: true, Priority: &priority, Weight: weight,
			}).Error)
			require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
				ChannelId: channel.Id, GroupName: channel.Group, ModelName: channel.Models, ParticipationSet: true,
			}).Error)

			t.Run("primary_survives_expiry_cleanup_and_can_be_released", func(t *testing.T) {
				_, err := SaveChannelSmartScheduleRoutePrimary(channel.Id, channel.Group, channel.Models,
					ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: ChannelSmartSchedulePermanentDurationMinutes})
				require.NoError(t, err)
				future := common.GetTimestamp() + int64(ChannelSmartScheduleManualPrimaryMaxMinutes)*120
				changed, err := ClearExpiredChannelSmartScheduleRoutePrimaries(future)
				require.NoError(t, err)
				assert.False(t, changed)
				var state ChannelSmartScheduleRouteState
				require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&state).Error)
				assert.Equal(t, common.ChannelMonitorSmartSchedulePermanentUntil, state.ManualPrimaryUntil)
				assert.True(t, state.ManualPrimarySaved)
				_, err = SaveChannelSmartScheduleRoutePrimary(channel.Id, channel.Group, channel.Models,
					ChannelSmartScheduleRoutePrimaryOptions{})
				require.NoError(t, err)
				require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&state).Error)
				assert.Zero(t, state.ManualPrimaryUntil)
				assert.False(t, state.ManualPrimarySaved)
				var ability Ability
				require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
				require.NotNil(t, ability.Priority)
				assert.Equal(t, priority, *ability.Priority)
				assert.Equal(t, weight, ability.Weight)
			})

			t.Run("pause_stays_active_until_explicitly_resumed", func(t *testing.T) {
				_, err := SaveChannelSmartScheduleGroupPause(channel.Id, channel.Group, channel.Models,
					ChannelSmartSchedulePermanentDurationMinutes)
				require.NoError(t, err)
				future := common.GetTimestamp() + int64(ChannelSmartScheduleGroupPauseMaxMinutes)*120
				paused, err := loadActiveChannelSmartSchedulePausedChannelIds(
					db, channel.Group, channel.Models, []int{channel.Id}, future)
				require.NoError(t, err)
				assert.Contains(t, paused, channel.Id)
				var storedPause ChannelSmartScheduleGroupPause
				require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&storedPause).Error)
				assert.Equal(t, common.ChannelMonitorSmartSchedulePermanentUntil, storedPause.PausedUntil)
				assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
					ChannelSmartScheduleAffinityEligibility(channel.Group, channel.Models, channel.Id, "/v1/chat/completions"))
				_, err = SaveChannelSmartScheduleGroupPause(channel.Id, channel.Group, channel.Models, 0)
				require.NoError(t, err)
				paused, err = loadActiveChannelSmartSchedulePausedChannelIds(
					db, channel.Group, channel.Models, []int{channel.Id}, future)
				require.NoError(t, err)
				assert.Empty(t, paused)
				assert.Equal(t, ChannelSmartScheduleAffinityEligible,
					ChannelSmartScheduleAffinityEligibility(channel.Group, channel.Models, channel.Id, "/v1/chat/completions"))
			})
		})
	}
}
