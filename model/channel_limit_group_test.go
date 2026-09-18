package model

import (
	"fmt"
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

func TestChannelLimitGroupDatabaseMatrix(t *testing.T) {
	backends := []struct {
		name, env string
		open      func(string) gorm.Dialector
	}{
		{"sqlite", "", func(dsn string) gorm.Dialector { return sqlite.Open(dsn) }},
		{"mysql", "TEST_SHARED_LIMIT_MYSQL_DSN", func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
		{"postgres", "TEST_SHARED_LIMIT_POSTGRES_DSN", func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}
	for _, backend := range backends {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%t", backend.name, upgrade), func(t *testing.T) {
				dsn := os.Getenv(backend.env)
				if backend.env != "" && dsn == "" {
					t.Skip(backend.env + " 未配置")
				}
				if backend.name == "sqlite" {
					dsn = filepath.Join(t.TempDir(), "migration.db")
				}
				db, err := gorm.Open(backend.open(dsn), &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(1)
				defer sqlDB.Close()
				originalDB := DB
				originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
				databaseType := common.DatabaseTypeSQLite
				if backend.name == "mysql" {
					databaseType = common.DatabaseTypeMySQL
				}
				if backend.name == "postgres" {
					databaseType = common.DatabaseTypePostgreSQL
				}
				common.SetDatabaseTypes(databaseType, originalLog)
				defer common.SetDatabaseTypes(originalMain, originalLog)
				DB = db
				defer func() { DB = originalDB }()
				models := []any{&ChannelLimitGroupRevision{}, &ChannelLimitGroup{}, &ChannelLimitGroupTier{}, &ChannelLimitGroupMember{}}
				// These DSNs must point to disposable databases dedicated to this matrix.
				for _, item := range append(models, &sharedLimitReleasedChannel{}, &ChannelRatioMonitor{}) {
					require.NoError(t, db.Migrator().DropTable(item))
				}
				t.Cleanup(func() {
					cleanup, openErr := gorm.Open(backend.open(dsn), &gorm.Config{})
					require.NoError(t, openErr)
					for _, item := range append(models, &sharedLimitReleasedChannel{}, &ChannelRatioMonitor{}) {
						require.NoError(t, cleanup.Migrator().DropTable(item))
					}
					connection, connectionErr := cleanup.DB()
					require.NoError(t, connectionErr)
					require.NoError(t, connection.Close())
				})
				if upgrade {
					require.NoError(t, db.AutoMigrate(&sharedLimitReleasedChannel{}))
				} else {
					require.NoError(t, db.AutoMigrate(&Channel{}))
				}
				fixture := sharedLimitReleasedChannel{Id: 91, Name: "原有渠道", Key: "fixture-key", Group: "default", UsedQuota: 12345}
				require.NoError(t, db.Create(&fixture).Error)
				// Preserve existing downstream per-channel limits when adding shared groups.
				require.NoError(t, db.AutoMigrate(&ChannelRatioMonitor{}))
				require.NoError(t, db.Create(&ChannelRatioMonitor{ChannelId: 91, ConcurrencyLimit: 5, RPMLimit: 100, ConcurrencyRevision: 7}).Error)
				require.NoError(t, db.AutoMigrate(models...))
				require.NoError(t, db.AutoMigrate(models...))
				var preserved sharedLimitReleasedChannel
				require.NoError(t, db.First(&preserved, 91).Error)
				assert.Equal(t, fixture.Key, preserved.Key)
				assert.Equal(t, fixture.Group, preserved.Group)
				assert.Equal(t, fixture.UsedQuota, preserved.UsedQuota)
				var monitor ChannelRatioMonitor
				require.NoError(t, db.Where("channel_id = ?", 91).First(&monitor).Error)
				assert.Equal(t, 5, monitor.ConcurrencyLimit)
				assert.Equal(t, 100, monitor.RPMLimit)
				assert.EqualValues(t, 7, monitor.ConcurrencyRevision)
				assert.True(t, db.Migrator().HasIndex(&sharedLimitReleasedChannel{}, "Name"))
				group := ChannelLimitGroup{Name: "共享测试", ConcurrencyLimit: 10, RPMLimit: 300, Tiers: []ChannelLimitGroupTier{{Priority: 100, ReservedConcurrency: 3, ReservedRPM: 90}, {Priority: 0}}, Members: []ChannelLimitGroupMember{{ChannelID: 91, Priority: 100}}}
				published := 0
				publish := func(old, next []ChannelLimitGroup, revision int64) error {
					published++
					require.Greater(t, revision, int64(0))
					return nil
				}
				require.NoError(t, MutateChannelLimitGroup(t.Context(), &group, false, publish))
				groups, revision, err := ReadChannelLimitGroups(db)
				require.NoError(t, err)
				require.Len(t, groups, 1)
				assert.Equal(t, group.Revision, revision)
				assert.Equal(t, 3, groups[0].Tiers[0].ReservedConcurrency)
				assert.Equal(t, 91, groups[0].Members[0].ChannelID)
				require.Error(t, db.Create(&ChannelLimitGroupMember{GroupID: 999, ChannelID: 91, Priority: 0}).Error)
				require.Error(t, db.Create(&ChannelLimitGroupTier{GroupID: group.ID, Priority: 100}).Error)
				stale := group
				group.RPMLimit = 400
				require.NoError(t, MutateChannelLimitGroup(t.Context(), &group, false, publish))
				require.ErrorIs(t, MutateChannelLimitGroup(t.Context(), &stale, false, publish), ErrChannelLimitConflict)
				require.NoError(t, db.AutoMigrate(models...))
				var remaining int64
				require.NoError(t, db.Model(&ChannelLimitGroupMember{}).Count(&remaining).Error)
				assert.EqualValues(t, 1, remaining)
				require.NoError(t, MutateChannelLimitGroup(t.Context(), &group, true, publish))
				assert.Equal(t, 3, published)
				var version string
				query := "SELECT VERSION()"
				if backend.name == "sqlite" {
					query = "SELECT sqlite_version()"
				}
				require.NoError(t, db.Raw(query).Scan(&version).Error)
				t.Logf("database=%s version=%s upgrade=%t", backend.name, version, upgrade)
			})
		}
	}
}

func TestChannelLimitGroupValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ChannelLimitGroup)
	}{
		{"duplicate priority", func(g *ChannelLimitGroup) { g.Tiers = append(g.Tiers, g.Tiers[0]) }},
		{"over-reserved concurrency", func(g *ChannelLimitGroup) { g.Tiers[0].ReservedConcurrency = 11 }},
		{"unlimited with rpm reservation", func(g *ChannelLimitGroup) { g.RPMLimit = 0 }},
		{"unknown member priority", func(g *ChannelLimitGroup) { g.Members[0].Priority = 3 }},
		{"duplicate channel", func(g *ChannelLimitGroup) { g.Members = append(g.Members, g.Members[0]) }},
		{"negative upper limit", func(g *ChannelLimitGroup) { g.ConcurrencyLimit = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			group := ChannelLimitGroup{Name: "共享", ConcurrencyLimit: 10, RPMLimit: 300, Tiers: []ChannelLimitGroupTier{{Priority: 100, ReservedConcurrency: 3, ReservedRPM: 90}}, Members: []ChannelLimitGroupMember{{ChannelID: 1, Priority: 100}}}
			require.NoError(t, group.Validate())
			test.change(&group)
			require.Error(t, group.Validate())
		})
	}
}
