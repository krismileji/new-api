package model

import (
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelSmartScheduleExecutionDetailFixture struct {
	ChannelId int                               `json:"channel_id"`
	Action    string                            `json:"action"`
	Reason    string                            `json:"reason"`
	Details   *ChannelSmartScheduleScoreDetails `json:"score_details"`
}

func TestChannelSmartScheduleExecutionDetailsPreserveOrderedRuntimePayloads(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	firstScore := 0.92
	secondScore := 0.61
	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-task-1",
		[]ChannelSmartScheduleExecutionDetailInput{
			{
				AdjustmentIndex: 0,
				Payload: channelSmartScheduleExecutionDetailFixture{
					ChannelId: 11,
					Action:    "updated",
					Reason:    "评分最高，设为主渠道",
					Details: &ChannelSmartScheduleScoreDetails{
						Version:    ChannelSmartScheduleScoreDetailsVersion,
						Strategy:   "smart",
						FinalScore: &firstScore,
					},
				},
			},
			{
				AdjustmentIndex: 1,
				Payload: channelSmartScheduleExecutionDetailFixture{
					ChannelId: 12,
					Action:    "unchanged",
					Reason:    "评分较低，保留备用流量",
					Details: &ChannelSmartScheduleScoreDetails{
						Version:    ChannelSmartScheduleScoreDetailsVersion,
						Strategy:   "smart",
						FinalScore: &secondScore,
					},
				},
			},
		},
	))

	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-task-1", "missing"})
	require.NoError(t, err)
	require.Len(t, loaded["schedule-task-1"], 2)
	assert.Equal(t, 0, loaded["schedule-task-1"][0].AdjustmentIndex)
	assert.Equal(t, 1, loaded["schedule-task-1"][1].AdjustmentIndex)
	assert.NotContains(t, loaded, "missing")

	var first channelSmartScheduleExecutionDetailFixture
	require.NoError(t, common.UnmarshalJsonStr(loaded["schedule-task-1"][0].Payload, &first))
	assert.Equal(t, 11, first.ChannelId)
	assert.Equal(t, "updated", first.Action)
	assert.Equal(t, "评分最高，设为主渠道", first.Reason)
	require.NotNil(t, first.Details)
	assert.InDelta(t, firstScore, *first.Details.FinalScore, 1e-9)
	var second channelSmartScheduleExecutionDetailFixture
	require.NoError(t, common.UnmarshalJsonStr(loaded["schedule-task-1"][1].Payload, &second))
	assert.Equal(t, 12, second.ChannelId)
	assert.Equal(t, "unchanged", second.Action)
}

func TestChannelSmartScheduleExecutionDetailsReplaceARepeatedTaskSnapshot(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-task-2",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 21,
				Reason:    "旧快照",
			},
		}},
	))
	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-task-2",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 22,
				Reason:    "新快照",
			},
		}},
	))

	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-task-2"})
	require.NoError(t, err)
	require.Len(t, loaded["schedule-task-2"], 1)
	var replacement channelSmartScheduleExecutionDetailFixture
	require.NoError(t, common.UnmarshalJsonStr(loaded["schedule-task-2"][0].Payload, &replacement))
	assert.Equal(t, 22, replacement.ChannelId)
	assert.Equal(t, "新快照", replacement.Reason)

	var rows []ChannelSmartScheduleExecutionDetail
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
}

func TestFinishChannelSmartScheduleTaskCommitsResultDetailsAndLeaseTogether(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&SystemTask{},
		&SystemTaskLock{},
		&ChannelSmartScheduleExecutionDetail{},
	))
	task, err := CreateSystemTask("channel_smart_schedule", nil, nil)
	require.NoError(t, err)
	const runnerID = "schedule-runner-a"
	_, claimed, err := ClaimSystemTask(task.ID, task.Type, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, FinishChannelSmartScheduleTaskWithExecutionDetails(
		task.TaskID,
		runnerID,
		SystemTaskStatusSucceeded,
		map[string]int{"updated": 1},
		"",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 31,
				Reason:    "已应用本轮结果",
			},
		}},
	))

	storedTask, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, storedTask)
	assert.Equal(t, SystemTaskStatusSucceeded, storedTask.Status)
	assert.Nil(t, storedTask.ActiveKey)
	var result map[string]int
	require.NoError(t, common.UnmarshalJsonStr(storedTask.Result, &result))
	assert.Equal(t, 1, result["updated"])
	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{task.TaskID})
	require.NoError(t, err)
	require.Len(t, loaded[task.TaskID], 1)
	var lockCount int64
	require.NoError(t, db.Model(&SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	assert.Zero(t, lockCount)
}

func TestFinishChannelSmartScheduleTaskRollsBackResultWhenDetailWriteFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&SystemTask{},
		&SystemTaskLock{},
		&ChannelSmartScheduleExecutionDetail{},
	))
	task, err := CreateSystemTask("channel_smart_schedule", nil, nil)
	require.NoError(t, err)
	const runnerID = "schedule-runner-b"
	_, claimed, err := ClaimSystemTask(task.ID, task.Type, runnerID, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		task.TaskID,
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 41,
				Reason:    "事务前快照",
			},
		}},
	))

	forcedErr := errors.New("forced execution detail insert failure")
	callbackName := "test:fail_schedule_execution_detail_insert"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_smart_schedule_execution_details" {
			tx.AddError(forcedErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	err = FinishChannelSmartScheduleTaskWithExecutionDetails(
		task.TaskID,
		runnerID,
		SystemTaskStatusSucceeded,
		map[string]int{"updated": 1},
		"",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 42,
				Reason:    "不应提交",
			},
		}},
	)
	require.ErrorIs(t, err, forcedErr)
	require.NoError(t, db.Callback().Create().Remove(callbackName))

	storedTask, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, storedTask)
	assert.Equal(t, SystemTaskStatusRunning, storedTask.Status)
	require.NotNil(t, storedTask.ActiveKey)
	assert.Empty(t, storedTask.Result)
	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{task.TaskID})
	require.NoError(t, err)
	require.Len(t, loaded[task.TaskID], 1)
	var retained channelSmartScheduleExecutionDetailFixture
	require.NoError(t, common.UnmarshalJsonStr(loaded[task.TaskID][0].Payload, &retained))
	assert.Equal(t, 41, retained.ChannelId)
	var lockCount int64
	require.NoError(t, db.Model(&SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	assert.Equal(t, int64(1), lockCount)
}

func TestChannelSmartScheduleExecutionDetailsSaveEmptySnapshot(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	require.NoError(t, SaveChannelSmartScheduleExecutionDetails("schedule-empty", nil))
	var row ChannelSmartScheduleExecutionDetail
	require.NoError(t, db.Where("task_id = ?", "schedule-empty").First(&row).Error)
	assert.Equal(t, 0, row.ItemCount)
	assert.NotEmpty(t, row.PayloadBlob)

	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-empty"})
	require.NoError(t, err)
	require.Contains(t, loaded, "schedule-empty")
	assert.Empty(t, loaded["schedule-empty"])
}

func TestChannelSmartScheduleExecutionDetailsRejectsLimitsAndCorruption(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	tooMany := make([]ChannelSmartScheduleExecutionDetailInput, ChannelSmartScheduleExecutionDetailMaxItems+1)
	for index := range tooMany {
		tooMany[index] = ChannelSmartScheduleExecutionDetailInput{
			AdjustmentIndex: index,
			Payload:         channelSmartScheduleExecutionDetailFixture{ChannelId: index},
		}
	}
	assert.Error(t, SaveChannelSmartScheduleExecutionDetails("schedule-too-many", tooMany))
	assert.Error(t, SaveChannelSmartScheduleExecutionDetails("schedule-negative-index", []ChannelSmartScheduleExecutionDetailInput{{
		AdjustmentIndex: -1,
		Payload:         channelSmartScheduleExecutionDetailFixture{ChannelId: 1},
	}}))
	assert.ErrorContains(t, SaveChannelSmartScheduleExecutionDetails("schedule-duplicate-index", []ChannelSmartScheduleExecutionDetailInput{
		{AdjustmentIndex: 0, Payload: channelSmartScheduleExecutionDetailFixture{ChannelId: 1}},
		{AdjustmentIndex: 0, Payload: channelSmartScheduleExecutionDetailFixture{ChannelId: 2}},
	}), "索引重复")
	assert.ErrorContains(t, SaveChannelSmartScheduleExecutionDetails("schedule-non-contiguous-index", []ChannelSmartScheduleExecutionDetailInput{
		{AdjustmentIndex: 0, Payload: channelSmartScheduleExecutionDetailFixture{ChannelId: 1}},
		{AdjustmentIndex: 2, Payload: channelSmartScheduleExecutionDetailFixture{ChannelId: 2}},
	}), "必须按输入顺序从 0 开始连续递增")
	assert.ErrorContains(t, SaveChannelSmartScheduleExecutionDetails("schedule-out-of-order-index", []ChannelSmartScheduleExecutionDetailInput{
		{AdjustmentIndex: 1, Payload: channelSmartScheduleExecutionDetailFixture{ChannelId: 2}},
		{AdjustmentIndex: 0, Payload: channelSmartScheduleExecutionDetailFixture{ChannelId: 1}},
	}), "必须按输入顺序从 0 开始连续递增")

	largePayload := strings.Repeat("x", channelSmartScheduleExecutionDetailMaxJSON)
	assert.Error(t, SaveChannelSmartScheduleExecutionDetails("schedule-too-large", []ChannelSmartScheduleExecutionDetailInput{{
		AdjustmentIndex: 0,
		Payload:         largePayload,
	}}))

	require.NoError(t, db.Create(&ChannelSmartScheduleExecutionDetail{
		TaskId:      "schedule-corrupt",
		PayloadBlob: []byte("not-gzip"),
		ItemCount:   1,
		CreatedAt:   common.GetTimestamp(),
	}).Error)
	_, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-corrupt"})
	assert.Error(t, err)

	require.NoError(t, db.Create(&ChannelSmartScheduleExecutionDetail{
		TaskId:      "schedule-count-mismatch",
		PayloadBlob: channelSmartScheduleExecutionDetailTestGzip(t, []byte("[]")),
		ItemCount:   1,
		CreatedAt:   common.GetTimestamp(),
	}).Error)
	_, err = GetChannelSmartScheduleExecutionDetails([]string{"schedule-count-mismatch"})
	assert.Error(t, err)
}

func TestChannelSmartScheduleExecutionDetailsRejectsDecompressedDataOverLimit(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))
	decoded := bytes.Repeat([]byte{'x'}, channelSmartScheduleExecutionDetailMaxJSON+1)
	require.NoError(t, db.Create(&ChannelSmartScheduleExecutionDetail{
		TaskId:      "schedule-over-decompressed-limit",
		PayloadBlob: channelSmartScheduleExecutionDetailTestGzip(t, decoded),
		ItemCount:   0,
		CreatedAt:   common.GetTimestamp(),
	}).Error)
	_, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-over-decompressed-limit"})
	assert.Error(t, err)
}

func TestChannelSmartScheduleExecutionDetailIndexes(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))
	assert.True(t, db.Migrator().HasIndex(&ChannelSmartScheduleExecutionDetail{}, "idx_channel_smart_schedule_execution_details_task_id"))
	assert.True(t, db.Migrator().HasIndex(&ChannelSmartScheduleExecutionDetail{}, "idx_channel_smart_schedule_execution_details_created_at"))
	require.NoError(t, db.Create(&ChannelSmartScheduleExecutionDetail{
		TaskId:      "schedule-unique-index",
		PayloadBlob: []byte{1},
		ItemCount:   0,
		CreatedAt:   common.GetTimestamp(),
	}).Error)
	assert.Error(t, db.Create(&ChannelSmartScheduleExecutionDetail{
		TaskId:      "schedule-unique-index",
		PayloadBlob: []byte{2},
		ItemCount:   0,
		CreatedAt:   common.GetTimestamp(),
	}).Error)
}

func channelSmartScheduleExecutionDetailTestGzip(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func TestChannelSmartScheduleExecutionDetailMetricsTrackSnapshotAndDecodeFailures(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))
	before := GetChannelSmartScheduleExecutionDetailMetrics()
	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-metrics",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload:         channelSmartScheduleExecutionDetailFixture{ChannelId: 1},
		}},
	))
	afterSave := GetChannelSmartScheduleExecutionDetailMetrics()
	assert.Equal(t, before.Rounds+1, afterSave.Rounds)
	assert.Equal(t, before.AdjustmentCount+1, afterSave.AdjustmentCount)
	assert.Greater(t, afterSave.UncompressedBytes, before.UncompressedBytes)
	assert.Greater(t, afterSave.CompressedBytes, before.CompressedBytes)
	assert.GreaterOrEqual(t, afterSave.CompressionDurationMicros, before.CompressionDurationMicros)

	require.NoError(t, db.Create(&ChannelSmartScheduleExecutionDetail{
		TaskId:      "schedule-metrics-corrupt",
		PayloadBlob: []byte("not-gzip"),
		ItemCount:   1,
		CreatedAt:   common.GetTimestamp(),
	}).Error)
	_, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-metrics-corrupt"})
	assert.Error(t, err)
	afterFailure := GetChannelSmartScheduleExecutionDetailMetrics()
	assert.Equal(t, afterSave.DecompressionFailures+1, afterFailure.DecompressionFailures)
}
