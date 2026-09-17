package model

import (
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

func TestChannelProbePolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  ChannelProbePolicy
		invalid bool
	}{
		{"default", ChannelProbePolicy{}, false},
		{"zero enabled", ChannelProbePolicy{AutoProbeDisabled: true, SmallInputResponseEnabled: true, SmallInputResponseText: "hello"}, true},
		{"blank enabled", ChannelProbePolicy{AutoProbeDisabled: true, SmallInputResponseEnabled: true, SmallInputThresholdTokens: 1000, SmallInputResponseText: " \n"}, true},
		{"negative", ChannelProbePolicy{SmallInputThresholdTokens: -1}, true},
		{"overflow", ChannelProbePolicy{SmallInputThresholdTokens: 1_000_001}, true},
		{"long text", ChannelProbePolicy{SmallInputResponseText: strings.Repeat("好", 16385)}, true},
		{"revision", ChannelProbePolicy{ProbePolicyRevision: -1}, true},
		{"restore automatic", ChannelProbePolicy{SmallInputResponseEnabled: true, SmallInputThresholdTokens: 1000, SmallInputResponseText: "hello"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Normalize()
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if !tc.policy.AutoProbeDisabled {
				assert.False(t, tc.policy.SmallInputResponseEnabled)
			}
		})
	}
}

// Dedicated databases are required: this suite only owns the tables below.
func TestChannelProbePolicyDatabase(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var dialector gorm.Dialector
			switch dialect {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "policy.db"))
			case "mysql":
				dsn := os.Getenv("TEST_PROBE_POLICY_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_PROBE_POLICY_MYSQL_DSN not configured")
				}
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_PROBE_POLICY_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_PROBE_POLICY_POSTGRES_DSN not configured")
				}
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			var version string
			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
			previousDB := DB
			previousType := common.MainDatabaseType()
			DB = db
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.LogDatabaseType())
			t.Cleanup(func() {
				DB = previousDB
				common.SetDatabaseTypes(previousType, common.LogDatabaseType())
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			for _, scenario := range []string{"fresh", "downstream upgrade", "released upgrade"} {
				// Each independent upgrade starts with a new process/connection.
				// Do not reuse pgx statement plans across dropped fixture schemas.
				if scenario != "fresh" {
					sqlDB, closeErr := db.DB()
					require.NoError(t, closeErr)
					require.NoError(t, sqlDB.Close())
					db, err = gorm.Open(dialector, &gorm.Config{})
					require.NoError(t, err)
					DB = db
				}
				upgrade := scenario == "downstream upgrade"
				require.NoError(t, db.Migrator().DropTable(&ChannelRatioMonitor{}, &Channel{}, &Ability{}))
				if scenario == "released upgrade" {
					require.NoError(t, db.AutoMigrate(&channelProbeReleasedChannel{}))
					require.NoError(t, db.Create(&channelProbeReleasedChannel{Id: 17, Name: "policy", Key: "test", UsedQuota: 25}).Error)
				} else {
					require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
					require.NoError(t, db.Create(&Channel{Id: 17, Name: "policy", Key: "test", UsedQuota: 25}).Error)
				}
				if upgrade {
					require.NoError(t, db.AutoMigrate(&channelProbePolicyLegacyMonitor{}))
					require.NoError(t, db.Create(&channelProbePolicyLegacyMonitor{
						ChannelId: 17, Ratio: 1.5, Remark: "preserved", UpstreamAccessToken: "private", ConcurrencyLimit: 7,
					}).Error)
				}
				for range 2 {
					require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &ChannelRatioMonitor{}))
				}
				var preserved Channel
				require.NoError(t, db.First(&preserved, 17).Error)
				assert.EqualValues(t, 25, preserved.UsedQuota, scenario)
				assert.Equal(t, "test", preserved.Key, scenario)
				policy, err := GetChannelProbePolicy(t.Context(), 17)
				require.NoError(t, err)
				assert.Equal(t, ChannelProbePolicy{}, policy)
				_, err = GetChannelProbePolicy(t.Context(), 999)
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				policy.AutoProbeDisabled = true
				policy.SmallInputResponseEnabled = true
				policy.SmallInputThresholdTokens = 1000
				policy.SmallInputResponseText = " 第一行\n第二行 "
				before, saved, err := SaveChannelProbePolicy(t.Context(), 17, policy)
				require.NoError(t, err)
				assert.Equal(t, ChannelProbePolicy{}, before)
				assert.EqualValues(t, 1, saved.ProbePolicyRevision)
				assert.Positive(t, saved.ProbePolicyUpdatedAt)
				assert.Equal(t, policy.SmallInputResponseText, saved.SmallInputResponseText)
				clone := &Channel{Name: "policy clone", Key: "cloned", Group: "default", Models: "gpt-4o", Status: common.ChannelStatusEnabled}
				require.NoError(t, InsertChannelWithProbePolicyFrom(clone, 17))
				clonedPolicy, cloneErr := GetChannelProbePolicy(t.Context(), clone.Id)
				require.NoError(t, cloneErr)
				assert.True(t, clonedPolicy.AutoProbeDisabled)
				assert.Equal(t, saved.SmallInputResponseText, clonedPolicy.SmallInputResponseText)
				_, _, err = SaveChannelProbePolicy(t.Context(), 17, policy)
				require.ErrorIs(t, err, ErrChannelProbePolicyConflict)
				saved.AutoProbeDisabled = false
				_, saved, err = SaveChannelProbePolicy(t.Context(), 17, saved)
				require.NoError(t, err)
				assert.False(t, saved.SmallInputResponseEnabled)
				loaded, err := GetChannelProbePolicy(t.Context(), 17)
				require.NoError(t, err)
				assert.Equal(t, saved, loaded)
				var monitor ChannelRatioMonitor
				require.NoError(t, db.Where("channel_id = ?", 17).First(&monitor).Error)
				if upgrade {
					assert.Equal(t, 1.5, monitor.Ratio)
					assert.Equal(t, "preserved", monitor.Remark)
					assert.Equal(t, "private", monitor.UpstreamAccessToken)
					assert.Equal(t, 7, monitor.ConcurrencyLimit)
				}
				require.Error(t, db.Create(&ChannelRatioMonitor{ChannelId: 17}).Error, "unique channel constraint survives migration")
			}
		})
	}
}
