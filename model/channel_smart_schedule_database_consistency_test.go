package model

import (
	"context"
	"os"
	"path/filepath"
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

func TestSmartScheduleRoutingDatabaseMatrix(t *testing.T) {
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
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "routing.db"))
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			originalDB, originalMemory := DB, common.MemoryCacheEnabled
			originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB, common.MemoryCacheEnabled = db, false
			common.SetDatabaseTypes(databaseType, originalLog)
			initCol()
			t.Cleanup(func() {
				DB, common.MemoryCacheEnabled = originalDB, originalMemory
				common.SetDatabaseTypes(originalMain, originalLog)
				initCol()
			})
			useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
			t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &ChannelSmartScheduleRouteState{},
				&ChannelSmartScheduleGroupPause{}, &ChannelLogicalGroup{}, &ChannelLogicalGroupMember{},
				&ChannelLogicalSmartScheduleRouteState{}, &ChannelSmartScheduleModelSampleState{}))
			db = db.Begin()
			require.NoError(t, db.Error)
			DB = db
			t.Cleanup(func() { require.NoError(t, db.Rollback().Error) })
			logical := ChannelLogicalGroup{Id: 90000, Name: "routing-consistency", Status: ChannelLogicalGroupStatusEnabled, Revision: 1}
			require.NoError(t, db.Create(&logical).Error)
			channels := []Channel{
				{Name: "member-a", Status: common.ChannelStatusEnabled, LogicalChannelID: &logical.Id},
				{Name: "member-b", Status: common.ChannelStatusEnabled, LogicalChannelID: &logical.Id},
				{Name: "independent", Status: common.ChannelStatusEnabled},
			}
			require.NoError(t, db.Create(&channels).Error)
			require.NoError(t, db.Create(&[]ChannelLogicalGroupMember{
				{LogicalGroupID: logical.Id, ChannelID: channels[0].Id, Weight: 100, AddressFingerprint: strings.Repeat("a", 64)},
				{LogicalGroupID: logical.Id, ChannelID: channels[1].Id, Weight: 0, AddressFingerprint: strings.Repeat("a", 64)},
			}).Error)
			score := 0.84
			details := &ChannelSmartScheduleScoreDetails{
				Version: ChannelSmartScheduleScoreDetailsVersion, FinalScore: &score,
				Decision: ChannelSmartScheduleScoreDecision{SelectedPrimaryChannelId: channels[0].Id},
			}
			encodedDetails, err := EncodeChannelSmartScheduleScoreDetails(details)
			require.NoError(t, err)
			rows := make([]ChannelSmartScheduleRoute, 0, 3)
			for index, channel := range channels {
				priority := int64(100)
				if index == 2 {
					priority = 50
				}
				state := ChannelSmartScheduleRouteState{
					ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
					LastScheduleScoreDetails: encodedDetails,
				}
				require.NoError(t, db.Create(&Ability{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100}).Error)
				require.NoError(t, db.Create(&state).Error)
				rows = append(rows, ChannelSmartScheduleRoute{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
					ChannelStatus: common.ChannelStatusEnabled, Priority: priority, Weight: 100, State: state})
			}
			var storedState ChannelSmartScheduleRouteState
			require.NoError(t, db.Where("channel_id = ?", channels[0].Id).First(&storedState).Error)
			storedDetails, err := storedState.LastScheduleScoreDetails.Decode()
			require.NoError(t, err)
			assert.Equal(t, details, storedDetails)
			payload, err := common.Marshal(channelLogicalSmartScheduleRoutePayload{
				State:               ChannelSmartScheduleRouteState{ParticipationSet: true, LastScheduleScoreDetails: encodedDetails},
				EffectiveRoutingSet: true, EffectivePriority: 10, EffectiveWeight: 100,
			})
			require.NoError(t, err)
			require.NoError(t, db.Create(&ChannelLogicalSmartScheduleRouteState{LogicalGroupID: logical.Id, LogicalRevision: 1,
				GroupName: "vip", ModelName: "model-a", StateJSON: ChannelSmartScheduleSamplesJSON(payload)}).Error)
			selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, channels[2].Id, selected.Id)
			assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
				ChannelSmartScheduleAffinityCandidateEligibilityExcluding("vip", "model-a", channels[0].Id, "", nil))
			views, err := GetChannelSmartScheduleRouteRuntimeViewsWithContext(context.Background(), rows)
			require.NoError(t, err)
			assert.Equal(t, int64(10), views[channelSmartScheduleRouteKey(channels[0].Id, "vip", "model-a")].Priority)
			viewState := views[channelSmartScheduleRouteKey(channels[0].Id, "vip", "model-a")].State
			require.NotNil(t, viewState)
			viewDetails, err := viewState.LastScheduleScoreDetails.Decode()
			require.NoError(t, err)
			assert.Equal(t, details, viewDetails)
			selected, err = SelectChannelSmartScheduleAffinityMemberExcluding("vip", "model-a", channels[0].Id, "",
				map[int]struct{}{channels[2].Id: {}})
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, channels[0].Id, selected.Id)
			t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")
			views, err = GetChannelSmartScheduleRouteRuntimeViewsWithContext(context.Background(), rows)
			require.NoError(t, err)
			assert.Equal(t, int64(100), views[channelSmartScheduleRouteKey(channels[0].Id, "vip", "model-a")].Priority)
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
		})
	}
}
