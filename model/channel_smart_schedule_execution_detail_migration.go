package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	channelSmartScheduleExecutionDetailTaskIndex      = "idx_channel_smart_schedule_execution_details_task_id"
	channelSmartScheduleExecutionDetailCreatedAtIndex = "idx_channel_smart_schedule_execution_details_created_at"
)

type channelSmartScheduleExecutionDetailLegacyRow struct {
	Id              int64  `gorm:"column:id"`
	TaskId          string `gorm:"column:task_id"`
	AdjustmentIndex int    `gorm:"column:adjustment_index"`
	Payload         string `gorm:"column:payload"`
	CreatedAt       int64  `gorm:"column:created_at"`
}

type channelSmartScheduleExecutionDetailLegacyTask struct {
	TaskId    string
	Rows      []channelSmartScheduleExecutionDetailLegacyRow
	CreatedAt int64
}

func prepareChannelSmartScheduleExecutionDetailMigration(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&ChannelSmartScheduleExecutionDetail{}) {
		return nil
	}

	indexes, err := db.Migrator().GetIndexes(&ChannelSmartScheduleExecutionDetail{})
	if err != nil {
		if db.Dialector.Name() != "sqlite" {
			return fmt.Errorf("inspect smart schedule execution detail indexes: %w", err)
		}
		// The SQLite migrator used by this project does not implement GetIndexes.
		// A legacy table always has both columns below, so defer to the schema
		// check and rebuild it when necessary; a current SQLite table needs no
		// preparation at all.
		if !db.Migrator().HasColumn(&ChannelSmartScheduleExecutionDetail{}, "payload") ||
			!db.Migrator().HasColumn(&ChannelSmartScheduleExecutionDetail{}, "adjustment_index") {
			return nil
		}
		indexes = nil
	}
	taskIndex, createdAtIndex := findChannelSmartScheduleExecutionDetailIndexes(indexes)
	if db.Dialector.Name() == "sqlite" && len(indexes) == 0 {
		taskIndex.exists = db.Migrator().HasIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailTaskIndex)
		taskIndex.legacy = taskIndex.exists
		createdAtIndex.exists = db.Migrator().HasIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailCreatedAtIndex)
		createdAtIndex.valid = createdAtIndex.exists
	}
	if taskIndex.exists && taskIndex.valid {
		if createdAtIndex.exists && !createdAtIndex.valid {
			return fmt.Errorf("smart schedule execution detail created-at index %q has an unexpected definition", channelSmartScheduleExecutionDetailCreatedAtIndex)
		}
		return nil
	}
	if taskIndex.exists && !taskIndex.legacy {
		return fmt.Errorf("smart schedule execution detail task index %q has an unexpected definition", channelSmartScheduleExecutionDetailTaskIndex)
	}
	if createdAtIndex.exists && !createdAtIndex.valid {
		return fmt.Errorf("smart schedule execution detail created-at index %q has an unexpected definition", channelSmartScheduleExecutionDetailCreatedAtIndex)
	}

	hasLegacyPayload := db.Migrator().HasColumn(&ChannelSmartScheduleExecutionDetail{}, "payload")
	hasLegacyAdjustmentIndex := db.Migrator().HasColumn(&ChannelSmartScheduleExecutionDetail{}, "adjustment_index")
	if !hasLegacyPayload || !hasLegacyAdjustmentIndex {
		if taskIndex.exists {
			if err := db.Migrator().DropIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailTaskIndex); err != nil {
				return fmt.Errorf("drop legacy smart schedule task index: %w", err)
			}
		}
		return nil
	}

	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&ChannelSmartScheduleExecutionDetail{}); err != nil {
		return fmt.Errorf("parse smart schedule execution detail schema: %w", err)
	}
	tableName := statement.Schema.Table
	legacyTasks, err := loadLegacyChannelSmartScheduleExecutionDetails(db, tableName)
	if err != nil {
		return err
	}
	snapshots, err := buildLegacyChannelSmartScheduleExecutionSnapshots(legacyTasks)
	if err != nil {
		return err
	}

	if taskIndex.exists {
		if err := db.Migrator().DropIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailTaskIndex); err != nil {
			return fmt.Errorf("drop legacy smart schedule task index: %w", err)
		}
	}
	if createdAtIndex.exists {
		if err := db.Migrator().DropIndex(&ChannelSmartScheduleExecutionDetail{}, channelSmartScheduleExecutionDetailCreatedAtIndex); err != nil {
			return fmt.Errorf("drop legacy smart schedule created-at index: %w", err)
		}
	}

	tempTableName, backupTableName, err := smartScheduleExecutionDetailMigrationTableNames(db, tableName)
	if err != nil {
		return err
	}
	if err := db.Table(tempTableName).Migrator().CreateTable(&ChannelSmartScheduleExecutionDetail{}); err != nil {
		return fmt.Errorf("create smart schedule execution detail migration table: %w", err)
	}
	cleanupTempTable := true
	defer func() {
		if cleanupTempTable {
			_ = db.Migrator().DropTable(tempTableName)
		}
	}()

	for _, snapshot := range snapshots {
		if err := db.Table(tempTableName).Create(snapshot).Error; err != nil {
			return fmt.Errorf("copy smart schedule execution detail snapshot: %w", err)
		}
	}

	if err := db.Migrator().RenameTable(tableName, backupTableName); err != nil {
		return fmt.Errorf("rename legacy smart schedule execution detail table: %w", err)
	}
	if err := db.Migrator().RenameTable(tempTableName, tableName); err != nil {
		_ = db.Migrator().RenameTable(backupTableName, tableName)
		return fmt.Errorf("activate smart schedule execution detail migration table: %w", err)
	}
	cleanupTempTable = false
	if err := db.Migrator().DropTable(backupTableName); err != nil {
		return fmt.Errorf("drop legacy smart schedule execution detail table: %w", err)
	}
	return nil
}

type channelSmartScheduleExecutionDetailIndexState struct {
	exists bool
	valid  bool
	legacy bool
}

func findChannelSmartScheduleExecutionDetailIndexes(
	indexes []gorm.Index,
) (task, createdAt channelSmartScheduleExecutionDetailIndexState) {
	for _, index := range indexes {
		name := index.Name()
		unique, uniqueOK := index.Unique()
		columns := index.Columns()
		switch name {
		case channelSmartScheduleExecutionDetailTaskIndex:
			task.exists = true
			task.valid = uniqueOK && unique && len(columns) == 1 && strings.EqualFold(columns[0], "task_id")
			task.legacy = uniqueOK && !unique && len(columns) == 1 && strings.EqualFold(columns[0], "task_id")
		case channelSmartScheduleExecutionDetailCreatedAtIndex:
			createdAt.exists = true
			createdAt.valid = len(columns) == 1 && strings.EqualFold(columns[0], "created_at")
		}
	}
	return task, createdAt
}

func loadLegacyChannelSmartScheduleExecutionDetails(
	db *gorm.DB,
	tableName string,
) ([]channelSmartScheduleExecutionDetailLegacyTask, error) {
	var rows []channelSmartScheduleExecutionDetailLegacyRow
	if err := db.Table(tableName).
		Order("task_id ASC, adjustment_index ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read legacy smart schedule execution details: %w", err)
	}

	tasks := make([]channelSmartScheduleExecutionDetailLegacyTask, 0)
	for _, row := range rows {
		taskId := strings.TrimSpace(row.TaskId)
		if taskId == "" {
			return nil, fmt.Errorf("legacy smart schedule execution detail has an empty task_id")
		}
		if len(tasks) == 0 || tasks[len(tasks)-1].TaskId != taskId {
			tasks = append(tasks, channelSmartScheduleExecutionDetailLegacyTask{TaskId: taskId})
		}
		task := &tasks[len(tasks)-1]
		task.Rows = append(task.Rows, row)
		if row.CreatedAt > task.CreatedAt {
			task.CreatedAt = row.CreatedAt
		}
	}
	return tasks, nil
}

func buildLegacyChannelSmartScheduleExecutionSnapshots(
	tasks []channelSmartScheduleExecutionDetailLegacyTask,
) ([]*ChannelSmartScheduleExecutionDetail, error) {
	snapshots := make([]*ChannelSmartScheduleExecutionDetail, 0, len(tasks))
	for _, task := range tasks {
		inputs := make([]ChannelSmartScheduleExecutionDetailInput, 0, len(task.Rows))
		for index, row := range task.Rows {
			inputs = append(inputs, ChannelSmartScheduleExecutionDetailInput{
				AdjustmentIndex: index,
				Payload:         json.RawMessage([]byte(row.Payload)),
			})
		}
		snapshot, err := channelSmartScheduleExecutionDetailSnapshot(task.TaskId, inputs, task.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("convert legacy smart schedule execution detail task %q: %w", task.TaskId, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func smartScheduleExecutionDetailMigrationTableNames(db *gorm.DB, tableName string) (string, string, error) {
	baseName := strings.ReplaceAll(strings.TrimSpace(tableName), ".", "_")
	if baseName == "" {
		return "", "", fmt.Errorf("smart schedule execution detail table name is empty")
	}
	for attempt := 0; attempt < 10; attempt++ {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano()+int64(attempt))
		maxBaseNameLength := 60 - len("_migration_") - len(suffix)
		if backupLength := 60 - len("_legacy_") - len(suffix); backupLength < maxBaseNameLength {
			maxBaseNameLength = backupLength
		}
		if maxBaseNameLength <= 0 {
			return "", "", fmt.Errorf("smart schedule execution detail table name is too long for migration")
		}
		shortBaseName := baseName
		if len(shortBaseName) > maxBaseNameLength {
			shortBaseName = shortBaseName[:maxBaseNameLength]
		}
		tempTableName := fmt.Sprintf("%s_migration_%s", shortBaseName, suffix)
		backupTableName := fmt.Sprintf("%s_legacy_%s", shortBaseName, suffix)
		if !db.Migrator().HasTable(tempTableName) && !db.Migrator().HasTable(backupTableName) {
			return tempTableName, backupTableName, nil
		}
	}
	return "", "", fmt.Errorf("cannot allocate temporary smart schedule execution detail table name")
}
