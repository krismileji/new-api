package model

import (
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type channelSmartScheduleRoutePoolIndexFixture ChannelSmartScheduleRouteState

func (channelSmartScheduleRoutePoolIndexFixture) TableName() string {
	return "test_channel_smart_schedule_route_pool_index"
}

func TestChannelSmartScheduleRoutePoolIndexMigrationAndQueries(t *testing.T) {
	tests := []struct {
		name         string
		databaseType common.DatabaseType
		open         func() (*gorm.DB, error)
	}{
		{
			name:         "sqlite",
			databaseType: common.DatabaseTypeSQLite,
			open: func() (*gorm.DB, error) {
				return gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
			},
		},
		{
			name:         "mysql",
			databaseType: common.DatabaseTypeMySQL,
			open: func() (*gorm.DB, error) {
				dsn := os.Getenv("CHANNEL_MONITOR_TEST_MYSQL_DSN")
				if dsn == "" {
					return nil, nil
				}
				return gorm.Open(mysql.Open(dsn), &gorm.Config{})
			},
		},
		{
			name:         "postgres",
			databaseType: common.DatabaseTypePostgreSQL,
			open: func() (*gorm.DB, error) {
				dsn := os.Getenv("CHANNEL_MONITOR_TEST_POSTGRES_DSN")
				if dsn == "" {
					return nil, nil
				}
				return gorm.Open(postgres.Open(dsn), &gorm.Config{})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := test.open()
			require.NoError(t, err)
			if db == nil {
				t.Skipf("%s integration DSN is not configured", test.name)
			}

			originalMainDatabaseType := common.MainDatabaseType()
			originalLogDatabaseType := common.LogDatabaseType()
			common.SetDatabaseTypes(test.databaseType, originalLogDatabaseType)
			t.Cleanup(func() {
				common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
				require.NoError(t, db.Migrator().DropTable(&channelSmartScheduleRoutePoolIndexFixture{}))
				sqlDB, sqlErr := db.DB()
				if sqlErr == nil {
					require.NoError(t, sqlDB.Close())
				}
			})

			require.NoError(t, db.Migrator().DropTable(&channelSmartScheduleRoutePoolIndexFixture{}))
			require.NoError(t, db.AutoMigrate(&channelSmartScheduleRoutePoolIndexFixture{}))
			assert.True(t, db.Migrator().HasIndex(
				&channelSmartScheduleRoutePoolIndexFixture{},
				"idx_channel_smart_schedule_route_pool",
			))

			states := []channelSmartScheduleRoutePoolIndexFixture{
				{ChannelId: 42, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
				{ChannelId: 7, GroupName: "standard", ModelName: "model-a", ParticipationSet: true},
				{ChannelId: 19, GroupName: "vip", ModelName: "model-b", ParticipationSet: true},
				{ChannelId: 3, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
				{ChannelId: 11, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
			}
			require.NoError(t, db.Create(&states).Error)

			var readStates []channelSmartScheduleRoutePoolIndexFixture
			require.NoError(t, db.
				Where("group_name = ? AND model_name = ?", "vip", "model-a").
				Order("channel_id ASC").
				Find(&readStates).Error)
			require.Len(t, readStates, 3)
			assert.Equal(t, []int{3, 11, 42}, []int{
				readStates[0].ChannelId,
				readStates[1].ChannelId,
				readStates[2].ChannelId,
			})

			var lockedStates []channelSmartScheduleRoutePoolIndexFixture
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				return lockForUpdate(tx).
					Where("group_name = ? AND model_name = ?", "vip", "model-a").
					Order("channel_id ASC").
					Find(&lockedStates).Error
			}))
			require.Len(t, lockedStates, 3)
			assert.Equal(t, []int{3, 11, 42}, []int{
				lockedStates[0].ChannelId,
				lockedStates[1].ChannelId,
				lockedStates[2].ChannelId,
			})
		})
	}
}
