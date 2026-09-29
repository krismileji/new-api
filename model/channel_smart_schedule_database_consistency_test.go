package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestChannelModifierRoutingDatabaseMatrix(t *testing.T) {
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
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "modifiers.db"))
			}
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("mod%x_", time.Now().UnixNano())}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			models := []any{&Channel{}, &Ability{}, &ChannelSmartScheduleRouteState{}, &ChannelSmartScheduleGroupPause{}}
			require.NoError(t, db.AutoMigrate(models...))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(models...)) })
			originalDB, originalCache, originalRedis := DB, common.MemoryCacheEnabled, common.RedisEnabled
			originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
			originalRoutes, originalChannels := channelSmartScheduleRouteCache, channelsIDM
			DB, common.MemoryCacheEnabled, common.RedisEnabled = db, true, false
			common.SetDatabaseTypes(databaseType, originalLog)
			initCol()
			t.Cleanup(func() {
				DB, common.MemoryCacheEnabled, common.RedisEnabled = originalDB, originalCache, originalRedis
				channelSmartScheduleRouteCache, channelsIDM = originalRoutes, originalChannels
				common.SetDatabaseTypes(originalMain, originalLog)
				initCol()
			})
			t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")
			settings := model_setting.GetGlobalSettings()
			originalBlacklist := settings.ThinkingModelBlacklist
			settings.ThinkingModelBlacklist = append(append([]string(nil), originalBlacklist...), "re:.*@sha256:.*")
			t.Cleanup(func() { settings.ThinkingModelBlacklist = originalBlacklist })
			for _, tc := range []struct {
				name, request, base              string
				exact, excludeExact, unavailable bool
			}{
				{name: "effort modifier", request: "gpt-5@effort:high", base: "gpt-5"},
				{name: "thinking modifier", request: "claude-3-7-sonnet@thinking:on", base: "claude-3-7-sonnet"},
				{name: "legacy alias", request: "claude-3-7-sonnet-thinking", base: "claude-3-7-sonnet"},
				{name: "explicit route wins", request: "gpt-5@effort:high", base: "gpt-5", exact: true},
				{name: "excluded explicit route falls back", request: "gpt-5@effort:high", base: "gpt-5", exact: true, excludeExact: true},
				{name: "opaque family name", request: "qwen-max", base: "qwen", unavailable: true},
				{name: "exempt at name", request: "opaque@sha256:deadbeef", base: "opaque", unavailable: true},
				{name: "legacy wildcard", request: "gpt-4o-gizmo-example", base: "gpt-4o-gizmo-*"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					group := strings.ReplaceAll(tc.name, " ", "_")
					priority, weight := int64(10), uint(100)
					base := &Channel{Name: "base", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight, Models: tc.base, Group: group}
					require.NoError(t, db.Create(base).Error)
					channelsIDM = map[int]*Channel{base.Id: base}
					abilities := []*Ability{{ChannelId: base.Id, Group: group, Model: tc.base, Enabled: true, Priority: &priority, Weight: weight}}
					states := []ChannelSmartScheduleRouteState{{ChannelId: base.Id, GroupName: group, ModelName: tc.base, ParticipationSet: true}}
					options := ChannelSelectionOptions{}
					wantID := base.Id
					if tc.exact {
						exact := &Channel{Name: "exact", Status: common.ChannelStatusEnabled, Models: tc.request, Group: group}
						require.NoError(t, db.Create(exact).Error)
						channelsIDM[exact.Id] = exact
						abilities = append(abilities, &Ability{ChannelId: exact.Id, Group: group, Model: tc.request, Enabled: true, Weight: 1})
						states = append(states, ChannelSmartScheduleRouteState{ChannelId: exact.Id, GroupName: group, ModelName: tc.request, ParticipationSet: true})
						wantID = exact.Id
						if tc.excludeExact {
							options.ExcludedChannelIds, wantID = []int{exact.Id}, base.Id
						}
					}
					require.NoError(t, db.Create(&abilities).Error)
					require.NoError(t, db.Create(&states).Error)
					channelSmartScheduleRouteCache = buildChannelSmartScheduleRouteCacheFromStates(abilities, channelsIDM, states)
					for _, managed := range []bool{false, true} {
						t.Run(fmt.Sprintf("managed=%t", managed), func(t *testing.T) {
							policies, err := common.Marshal([]map[string]any{{"group": group, "models": []string{tc.base}}})
							require.NoError(t, err)
							useChannelSmartScheduleTrafficPolicy(t, managed, string(policies))
							selected, err := GetRandomSatisfiedChannel(group, tc.request, 0, nil, options)
							require.NoError(t, err)
							if tc.unavailable {
								assert.Nil(t, selected)
								assert.False(t, IsChannelEnabledForGroupModel(group, tc.request, base.Id))
								return
							}
							require.NotNil(t, selected)
							assert.Equal(t, wantID, selected.Id)
							assert.True(t, IsChannelEnabledForGroupModel(group, tc.request, selected.Id))
							assert.Equal(t, ChannelSmartScheduleAffinityEligible, ChannelSmartScheduleAffinityEligibility(group, tc.request, selected.Id, "", options))
						})
					}
				})
			}
			query := "SELECT version()"
			if engine == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)
		})
	}
}

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
