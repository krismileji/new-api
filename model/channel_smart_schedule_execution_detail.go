package model

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	ChannelSmartScheduleExecutionDetailMaxItems    = 20_000
	ChannelSmartScheduleExecutionDetailMaxItemJSON = 256 * 1024
	channelSmartScheduleExecutionDetailMaxJSON     = 64 * 1024 * 1024
	channelSmartScheduleExecutionDetailMaxBlob     = channelSmartScheduleExecutionDetailMaxJSON + 1024*1024
)

// ChannelSmartScheduleExecutionDetailMetrics contains process-local snapshot
// counters. They are intentionally observational and never affect scheduling.
type ChannelSmartScheduleExecutionDetailMetrics struct {
	Rounds                    int64 `json:"rounds"`
	AdjustmentCount           int64 `json:"adjustment_count"`
	UncompressedBytes         int64 `json:"uncompressed_bytes"`
	CompressedBytes           int64 `json:"compressed_bytes"`
	CompressionDurationMicros int64 `json:"compression_duration_micros"`
	DecompressionFailures     int64 `json:"decompression_failures"`
}

var channelSmartScheduleExecutionDetailMetrics struct {
	rounds                    atomic.Int64
	adjustmentCount           atomic.Int64
	uncompressedBytes         atomic.Int64
	compressedBytes           atomic.Int64
	compressionDurationMicros atomic.Int64
	decompressionFailures     atomic.Int64
}

func GetChannelSmartScheduleExecutionDetailMetrics() ChannelSmartScheduleExecutionDetailMetrics {
	return ChannelSmartScheduleExecutionDetailMetrics{
		Rounds:                    channelSmartScheduleExecutionDetailMetrics.rounds.Load(),
		AdjustmentCount:           channelSmartScheduleExecutionDetailMetrics.adjustmentCount.Load(),
		UncompressedBytes:         channelSmartScheduleExecutionDetailMetrics.uncompressedBytes.Load(),
		CompressedBytes:           channelSmartScheduleExecutionDetailMetrics.compressedBytes.Load(),
		CompressionDurationMicros: channelSmartScheduleExecutionDetailMetrics.compressionDurationMicros.Load(),
		DecompressionFailures:     channelSmartScheduleExecutionDetailMetrics.decompressionFailures.Load(),
	}
}

type ChannelSmartScheduleExecutionDetail struct {
	Id          int64  `gorm:"primaryKey"`
	TaskId      string `gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_smart_schedule_execution_details_task_id"`
	PayloadBlob []byte `gorm:"not null"`
	ItemCount   int    `gorm:"not null"`
	CreatedAt   int64  `gorm:"bigint;not null;index:idx_channel_smart_schedule_execution_details_created_at"`
}

type ChannelSmartScheduleExecutionDetailInput struct {
	AdjustmentIndex int
	Payload         any
}

type ChannelSmartScheduleExecutionDetailPayload struct {
	AdjustmentIndex int
	Payload         string
}

func SaveChannelSmartScheduleExecutionDetails(
	taskId string,
	inputs []ChannelSmartScheduleExecutionDetailInput,
) error {
	taskId = strings.TrimSpace(taskId)
	row, err := channelSmartScheduleExecutionDetailSnapshot(taskId, inputs, common.GetTimestamp())
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskId).
			Delete(&ChannelSmartScheduleExecutionDetail{}).Error; err != nil {
			return err
		}
		return tx.Create(row).Error
	})
}

// FinishChannelSmartScheduleTaskWithExecutionDetails commits the task terminal
// state and its detail snapshot together while the caller still owns the lease.
func FinishChannelSmartScheduleTaskWithExecutionDetails(
	taskId string,
	lockedBy string,
	status SystemTaskStatus,
	resultPayload any,
	errorMessage string,
	inputs []ChannelSmartScheduleExecutionDetailInput,
) error {
	taskId = strings.TrimSpace(taskId)
	if taskId == "" {
		return errors.New("智能调度任务 ID 不能为空")
	}
	now := common.GetTimestamp()
	row, err := channelSmartScheduleExecutionDetailSnapshot(taskId, inputs, now)
	if err != nil {
		return err
	}
	resultText, err := marshalSystemTaskJSON(resultPayload)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&SystemTask{}).
			Where("task_id = ? AND status = ? AND locked_by = ?", taskId, SystemTaskStatusRunning, lockedBy).
			Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
			Updates(map[string]any{
				"status":     status,
				"active_key": nil,
				"result":     resultText,
				"error":      errorMessage,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSystemTaskLockLost
		}
		if err := tx.Where("task_id = ?", taskId).
			Delete(&ChannelSmartScheduleExecutionDetail{}).Error; err != nil {
			return err
		}
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		lockResult := tx.Where("task_id = ? AND locked_by = ?", taskId, lockedBy).
			Delete(&SystemTaskLock{})
		if lockResult.Error != nil {
			return lockResult.Error
		}
		if lockResult.RowsAffected == 0 {
			return ErrSystemTaskLockLost
		}
		return nil
	})
}

func channelSmartScheduleExecutionDetailSnapshot(
	taskId string,
	inputs []ChannelSmartScheduleExecutionDetailInput,
	createdAt int64,
) (*ChannelSmartScheduleExecutionDetail, error) {
	if taskId == "" {
		return nil, errors.New("智能调度任务 ID 不能为空")
	}
	if len(inputs) > ChannelSmartScheduleExecutionDetailMaxItems {
		return nil, fmt.Errorf("智能调度执行明细数量不能超过 %d", ChannelSmartScheduleExecutionDetailMaxItems)
	}

	payloads := make([]json.RawMessage, 0, len(inputs))
	seenIndexes := make(map[int]struct{}, len(inputs))
	for expectedIndex, input := range inputs {
		if input.AdjustmentIndex < 0 {
			return nil, fmt.Errorf("智能调度执行明细索引不能为负数: %d", input.AdjustmentIndex)
		}
		if input.Payload == nil {
			return nil, errors.New("智能调度执行明细 payload 不能为空")
		}
		if _, exists := seenIndexes[input.AdjustmentIndex]; exists {
			return nil, fmt.Errorf("智能调度执行明细索引重复: %d", input.AdjustmentIndex)
		}
		seenIndexes[input.AdjustmentIndex] = struct{}{}
		if input.AdjustmentIndex != expectedIndex {
			return nil, fmt.Errorf(
				"智能调度执行明细索引必须按输入顺序从 0 开始连续递增: 期望 %d，实际 %d",
				expectedIndex,
				input.AdjustmentIndex,
			)
		}
		encoded, err := common.Marshal(input.Payload)
		if err != nil {
			return nil, fmt.Errorf("编码智能调度执行明细失败: %w", err)
		}
		if len(encoded) > ChannelSmartScheduleExecutionDetailMaxItemJSON {
			return nil, fmt.Errorf(
				"单条智能调度执行明细不能超过 %d KiB",
				ChannelSmartScheduleExecutionDetailMaxItemJSON/1024,
			)
		}
		payloads = append(payloads, json.RawMessage(encoded))
	}
	encoded, err := common.Marshal(payloads)
	if err != nil {
		return nil, fmt.Errorf("编码智能调度执行明细数组失败: %w", err)
	}
	if len(encoded) > channelSmartScheduleExecutionDetailMaxJSON {
		return nil, fmt.Errorf("智能调度执行明细未压缩 JSON 不能超过 %d MiB", channelSmartScheduleExecutionDetailMaxJSON/(1024*1024))
	}

	compressionStarted := time.Now()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("压缩智能调度执行明细失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("完成压缩智能调度执行明细失败: %w", err)
	}
	if compressed.Len() > channelSmartScheduleExecutionDetailMaxBlob {
		return nil, fmt.Errorf("智能调度执行明细压缩数据不能超过 %d MiB", channelSmartScheduleExecutionDetailMaxBlob/(1024*1024))
	}
	channelSmartScheduleExecutionDetailMetrics.rounds.Add(1)
	channelSmartScheduleExecutionDetailMetrics.adjustmentCount.Add(int64(len(payloads)))
	channelSmartScheduleExecutionDetailMetrics.uncompressedBytes.Add(int64(len(encoded)))
	channelSmartScheduleExecutionDetailMetrics.compressedBytes.Add(int64(compressed.Len()))
	channelSmartScheduleExecutionDetailMetrics.compressionDurationMicros.Add(time.Since(compressionStarted).Microseconds())
	return &ChannelSmartScheduleExecutionDetail{
		TaskId:      taskId,
		PayloadBlob: compressed.Bytes(),
		ItemCount:   len(payloads),
		CreatedAt:   createdAt,
	}, nil
}

func channelSmartScheduleExecutionDetailPayloads(
	row ChannelSmartScheduleExecutionDetail,
) (payloads []json.RawMessage, err error) {
	defer func() {
		if err != nil {
			channelSmartScheduleExecutionDetailMetrics.decompressionFailures.Add(1)
		}
	}()
	if row.ItemCount < 0 || row.ItemCount > ChannelSmartScheduleExecutionDetailMaxItems {
		return nil, fmt.Errorf("智能调度执行明细数量无效: %d", row.ItemCount)
	}
	reader, err := gzip.NewReader(bytes.NewReader(row.PayloadBlob))
	if err != nil {
		return nil, fmt.Errorf("打开智能调度执行明细压缩数据失败: %w", err)
	}
	decoded, readErr := io.ReadAll(io.LimitReader(reader, channelSmartScheduleExecutionDetailMaxJSON+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("解压智能调度执行明细失败: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("关闭智能调度执行明细压缩数据失败: %w", closeErr)
	}
	if len(decoded) > channelSmartScheduleExecutionDetailMaxJSON {
		return nil, fmt.Errorf("解压后的智能调度执行明细超过 %d MiB", channelSmartScheduleExecutionDetailMaxJSON/(1024*1024))
	}
	if common.GetJsonType(json.RawMessage(decoded)) != "array" {
		return nil, errors.New("智能调度执行明细压缩数据不是数组")
	}
	var decodedPayloads []json.RawMessage
	if err := common.Unmarshal(decoded, &decodedPayloads); err != nil {
		return nil, fmt.Errorf("解码智能调度执行明细数组失败: %w", err)
	}
	if len(decodedPayloads) != row.ItemCount {
		return nil, fmt.Errorf("智能调度执行明细数量校验失败: item_count=%d, decoded=%d", row.ItemCount, len(decodedPayloads))
	}
	for _, payload := range decodedPayloads {
		if len(payload) > ChannelSmartScheduleExecutionDetailMaxItemJSON {
			return nil, fmt.Errorf(
				"单条智能调度执行明细超过 %d KiB",
				ChannelSmartScheduleExecutionDetailMaxItemJSON/1024,
			)
		}
	}
	return decodedPayloads, nil
}

func GetChannelSmartScheduleExecutionDetails(
	taskIds []string,
) (map[string][]ChannelSmartScheduleExecutionDetailPayload, error) {
	return GetChannelSmartScheduleExecutionDetailsWithContext(context.Background(), taskIds)
}

func GetChannelSmartScheduleExecutionDetailsWithContext(
	ctx context.Context,
	taskIds []string,
) (map[string][]ChannelSmartScheduleExecutionDetailPayload, error) {
	result := make(map[string][]ChannelSmartScheduleExecutionDetailPayload)
	if len(taskIds) == 0 {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type executionDetailMetadata struct {
		TaskId      string
		ItemCount   int
		PayloadSize int64
	}
	var metadata []executionDetailMetadata
	if err := DB.WithContext(ctx).Model(&ChannelSmartScheduleExecutionDetail{}).
		Select("task_id", "item_count", "LENGTH(payload_blob) AS payload_size").
		Where("task_id IN ?", taskIds).Find(&metadata).Error; err != nil {
		return nil, err
	}
	boundedTaskIds := make([]string, 0, len(metadata))
	for _, item := range metadata {
		if item.ItemCount < 0 || item.ItemCount > ChannelSmartScheduleExecutionDetailMaxItems {
			return nil, fmt.Errorf("智能调度执行明细数量无效: %d", item.ItemCount)
		}
		if item.PayloadSize < 0 || item.PayloadSize > channelSmartScheduleExecutionDetailMaxBlob {
			return nil, fmt.Errorf("智能调度执行明细压缩数据超过 %d MiB", channelSmartScheduleExecutionDetailMaxBlob/(1024*1024))
		}
		boundedTaskIds = append(boundedTaskIds, item.TaskId)
	}
	if len(boundedTaskIds) == 0 {
		return result, nil
	}
	var rows []ChannelSmartScheduleExecutionDetail
	if err := DB.WithContext(ctx).Where("task_id IN ?", boundedTaskIds).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		payloads, err := channelSmartScheduleExecutionDetailPayloads(row)
		if err != nil {
			return nil, err
		}
		details := make([]ChannelSmartScheduleExecutionDetailPayload, 0, len(payloads))
		for index, payload := range payloads {
			details = append(details, ChannelSmartScheduleExecutionDetailPayload{
				AdjustmentIndex: index,
				Payload:         string(payload),
			})
		}
		result[row.TaskId] = details
	}
	return result, nil
}
