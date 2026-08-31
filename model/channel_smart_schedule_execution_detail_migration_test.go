package model

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacyChannelSmartScheduleExecutionDetail struct {
	Id              int64  `gorm:"primaryKey"`
	TaskId          string `gorm:"type:varchar(64);not null;uniqueIndex:idx_schedule_execution_detail_task_item;index"`
	AdjustmentIndex int    `gorm:"not null;uniqueIndex:idx_schedule_execution_detail_task_item"`
	Payload         string `gorm:"type:text;not null"`
	CreatedAt       int64  `gorm:"bigint;not null;index"`
}

func (legacyChannelSmartScheduleExecutionDetail) TableName() string {
	return "channel_smart_schedule_execution_details"
}

func TestPrepareChannelSmartScheduleExecutionDetailMigrationConvertsLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "execution-details-migration.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.AutoMigrate(&legacyChannelSmartScheduleExecutionDetail{}))
	require.NoError(t, db.Create(&[]legacyChannelSmartScheduleExecutionDetail{
		{TaskId: "task-1", AdjustmentIndex: 0, Payload: `{"route":"a"}`, CreatedAt: 10},
		{TaskId: "task-1", AdjustmentIndex: 1, Payload: `{"route":"b"}`, CreatedAt: 11},
		{TaskId: "task-2", AdjustmentIndex: 0, Payload: `{"route":"c"}`, CreatedAt: 12},
	}).Error)

	require.NoError(t, prepareChannelSmartScheduleExecutionDetailMigration(db))
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	var rows []ChannelSmartScheduleExecutionDetail
	require.NoError(t, db.Order("task_id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "task-1", rows[0].TaskId)
	assert.Equal(t, int64(11), rows[0].CreatedAt)
	assert.Equal(t, "task-2", rows[1].TaskId)
	assert.True(t, db.Migrator().HasIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailTaskIndex))
	assert.True(t, db.Migrator().HasIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailCreatedAtIndex))
	assert.False(t, db.Migrator().HasTable("channel_smart_schedule_execution_details_legacy"))

	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	details, err := GetChannelSmartScheduleExecutionDetails([]string{"task-1"})
	require.NoError(t, err)
	require.Len(t, details["task-1"], 2)
	assert.Equal(t, 0, details["task-1"][0].AdjustmentIndex)
	assert.JSONEq(t, `{"route":"a"}`, details["task-1"][0].Payload)
	assert.Equal(t, 1, details["task-1"][1].AdjustmentIndex)
	assert.JSONEq(t, `{"route":"b"}`, details["task-1"][1].Payload)
}
