package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelMonitorCostBackfillStatusProcessing = "processing"
	ChannelMonitorCostBackfillStatusComplete   = "complete"
	ChannelMonitorCostBackfillStatusFailed     = "failed"
	ChannelMonitorCostReconciliationMatched    = "matched"
	ChannelMonitorCostReconciliationMismatch   = "mismatch"
	channelMonitorCostBackfillDefaultMaxDays   = 31
	channelMonitorCostBackfillMaxDays          = 366
	channelMonitorCostBackfillSource           = "historical_backfill"
)

// ChannelMonitorCostBackfillCheckpoint records one durable unit of history
// backfill. The batch/day key makes retries idempotent without replaying cost
// events into the authoritative ledger.
type ChannelMonitorCostBackfillCheckpoint struct {
	Id          int64  `gorm:"primaryKey"`
	BatchId     string `gorm:"size:64;not null;uniqueIndex:idx_cm_cost_backfill_batch_day"`
	DayStart    int64  `gorm:"not null;uniqueIndex:idx_cm_cost_backfill_batch_day;index:idx_cm_cost_backfill_day"`
	Status      string `gorm:"size:16;not null"`
	RowsWritten int64  `gorm:"not null"`
	StartedAt   int64  `gorm:"not null"`
	CompletedAt int64  `gorm:"not null"`
	LastError   string `gorm:"size:512;not null"`
	UpdatedAt   int64  `gorm:"not null"`
}

func (ChannelMonitorCostBackfillCheckpoint) TableName() string {
	return "channel_monitor_cost_backfill_checkpoints"
}

// ChannelMonitorCostReconciliation stores the result produced in the same
// transaction as a completed day. Cost and counts are reconciled separately;
// probe category gaps remain visible instead of being silently allocated.
type ChannelMonitorCostReconciliation struct {
	Id                    int64  `gorm:"primaryKey"`
	BatchId               string `gorm:"size:64;not null;uniqueIndex:idx_cm_cost_reconcile_batch_day"`
	DayStart              int64  `gorm:"not null;uniqueIndex:idx_cm_cost_reconcile_batch_day;index:idx_cm_cost_reconcile_day"`
	Status                string `gorm:"size:16;not null"`
	LedgerCostNanoCNY     int64  `gorm:"not null"`
	DetailCostNanoCNY     int64  `gorm:"not null"`
	LedgerSettledCount    int64  `gorm:"not null"`
	DetailSettledCount    int64  `gorm:"not null"`
	LedgerUnresolvedCount int64  `gorm:"not null"`
	DetailUnresolvedCount int64  `gorm:"not null"`
	ProbeCategoryGap      int64  `gorm:"not null"`
	GroupProbeCategoryGap int64  `gorm:"not null"`
	UnknownResidualCost   int64  `gorm:"not null"`
	CompletedAt           int64  `gorm:"not null"`
	ErrorMessage          string `gorm:"size:512;not null"`
}

func (ChannelMonitorCostReconciliation) TableName() string {
	return "channel_monitor_cost_reconciliations"
}

type ChannelMonitorCostBackfillResult struct {
	BatchId               string
	From                  int64
	To                    int64
	Days                  int
	CompletedDays         int
	SkippedDays           int
	RowsWritten           int64
	UnknownResidualCost   int64
	ProbeCategoryGap      int64
	GroupProbeCategoryGap int64
}

type channelMonitorCostBackfillAmount struct {
	CostNanoCNY     int64
	SettledCount    int64
	UnresolvedCount int64
}

type channelMonitorCostBackfillChannelAmount struct {
	channelMonitorCostBackfillAmount
	ProbeCostNanoCNY      int64
	GroupProbeCostNanoCNY int64
}

type channelMonitorCostBackfillKey struct {
	ChannelId int
	APIKeyId  int
	APIKeyKey string
}

type channelMonitorCostBackfillDayResult struct {
	Skipped               bool
	RowsWritten           int64
	UnknownResidualCost   int64
	ProbeCategoryGap      int64
	GroupProbeCategoryGap int64
}

// BackfillChannelMonitorCostDetails rebuilds the drill-down projection from
// existing daily ledgers. It never writes ChannelDailyCost or
// ChannelDailyAPIKeyCost, so it cannot double-charge a historical event.
// Ranges are calendar-day based and bounded to keep a manual retry cheap.
func BackfillChannelMonitorCostDetails(
	ctx context.Context,
	batchId string,
	fromTimestamp int64,
	toTimestamp int64,
	maxDays int,
) (ChannelMonitorCostBackfillResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	batchId = strings.TrimSpace(batchId)
	if batchId == "" || len(batchId) > 64 {
		return ChannelMonitorCostBackfillResult{}, errors.New("成本回填批次 ID 无效")
	}
	if fromTimestamp <= 0 || toTimestamp <= fromTimestamp {
		return ChannelMonitorCostBackfillResult{}, errors.New("成本回填时间范围无效")
	}
	fromTimestamp = ChannelDailyCostDayStart(fromTimestamp)
	if toTimestamp != ChannelDailyCostDayStart(toTimestamp) {
		return ChannelMonitorCostBackfillResult{}, errors.New("成本回填结束时间必须为北京时间日边界")
	}
	toTimestamp = ChannelDailyCostDayStart(toTimestamp)
	if toTimestamp <= fromTimestamp {
		return ChannelMonitorCostBackfillResult{}, errors.New("成本回填时间范围无效")
	}
	if maxDays <= 0 {
		maxDays = channelMonitorCostBackfillDefaultMaxDays
	}
	if maxDays > channelMonitorCostBackfillMaxDays {
		return ChannelMonitorCostBackfillResult{}, fmt.Errorf("成本回填最多支持 %d 天", channelMonitorCostBackfillMaxDays)
	}
	days64 := (toTimestamp - fromTimestamp) / channelDailyCostDaySeconds
	if days64 <= 0 || days64 > int64(maxDays) {
		return ChannelMonitorCostBackfillResult{}, fmt.Errorf("成本回填天数必须在 1 到 %d 天之间", maxDays)
	}
	if DB == nil {
		return ChannelMonitorCostBackfillResult{}, errors.New("成本回填数据库不可用")
	}

	result := ChannelMonitorCostBackfillResult{
		BatchId: batchId, From: fromTimestamp, To: toTimestamp, Days: int(days64),
	}
	for dayStart := fromTimestamp; dayStart < toTimestamp; dayStart += channelDailyCostDaySeconds {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		dayResult, err := backfillChannelMonitorCostDetailDay(ctx, batchId, dayStart)
		if err != nil {
			return result, err
		}
		if dayResult.Skipped {
			result.SkippedDays++
		} else {
			result.CompletedDays++
		}
		if err := addChannelMonitorCostBackfillResult(&result, dayResult); err != nil {
			return result, err
		}
	}
	return result, nil
}

func backfillChannelMonitorCostDetailDay(ctx context.Context, batchId string, dayStart int64) (result channelMonitorCostBackfillDayResult, resultErr error) {
	now := time.Now().Unix()
	checkpoint := ChannelMonitorCostBackfillCheckpoint{
		BatchId: batchId, DayStart: dayStart, Status: ChannelMonitorCostBackfillStatusProcessing,
		StartedAt: now, UpdatedAt: now,
	}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&checkpoint).Error; err != nil {
		return result, err
	}
	resultErr = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint = ChannelMonitorCostBackfillCheckpoint{}
		err := tx.Where("batch_id = ? AND day_start = ?", batchId, dayStart).First(&checkpoint).Error
		if err != nil {
			return err
		}
		if checkpoint.Status == ChannelMonitorCostBackfillStatusComplete {
			result.Skipped = true
			return nil
		}

		if err := tx.Model(&ChannelMonitorCostBackfillCheckpoint{}).
			Where("id = ?", checkpoint.Id).
			Updates(map[string]any{
				"status":     ChannelMonitorCostBackfillStatusProcessing,
				"last_error": "",
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("day_start = ? AND source_kind = ?", dayStart, channelMonitorCostBackfillSource).
			Delete(&ChannelMonitorDailyCostDetail{}).Error; err != nil {
			return err
		}

		var channelRows []ChannelDailyCost
		if err := tx.Where("day_start = ?", dayStart).Order("channel_id ASC").Find(&channelRows).Error; err != nil {
			return err
		}
		var keyRows []ChannelDailyAPIKeyCost
		if err := tx.Where("day_start = ?", dayStart).Order("channel_id ASC, api_key_id ASC, key_fingerprint ASC").Find(&keyRows).Error; err != nil {
			return err
		}
		var detailRows []ChannelMonitorDailyCostDetail
		if err := tx.Where("day_start = ?", dayStart).Find(&detailRows).Error; err != nil {
			return err
		}
		channelAmounts := make(map[int]channelMonitorCostBackfillChannelAmount, len(channelRows))
		for _, row := range channelRows {
			amount := channelMonitorCostBackfillChannelAmount{
				channelMonitorCostBackfillAmount: channelMonitorCostBackfillAmount{
					CostNanoCNY: row.CostNanoCNY, SettledCount: row.SettledCount, UnresolvedCount: row.UnresolvedCount,
				}, ProbeCostNanoCNY: row.ProbeCostNanoCNY, GroupProbeCostNanoCNY: row.GroupProbeCostNanoCNY,
			}
			if err := validateChannelMonitorCostBackfillChannelAmount(amount); err != nil {
				return fmt.Errorf("渠道 %d 日成本无效: %w", row.ChannelId, err)
			}
			channelAmounts[row.ChannelId] = amount
		}
		detailByChannel := make(map[int]channelMonitorCostBackfillChannelAmount, len(detailRows))
		detailByKey := make(map[channelMonitorCostBackfillKey]channelMonitorCostBackfillAmount, len(detailRows))
		for _, row := range detailRows {
			amount := channelMonitorCostBackfillAmount{
				CostNanoCNY: row.CostNanoCNY, SettledCount: row.SettledCount, UnresolvedCount: row.UnresolvedCount,
			}
			if err := validateChannelMonitorCostBackfillAmount(amount); err != nil {
				return fmt.Errorf("渠道 %d 成本明细无效: %w", row.ChannelId, err)
			}
			channelAmount := detailByChannel[row.ChannelId]
			if err := addChannelMonitorCostBackfillChannelAmount(&channelAmount, channelMonitorCostBackfillChannelAmount{
				channelMonitorCostBackfillAmount: amount,
				ProbeCostNanoCNY:                 row.ProbeCostNanoCNY, GroupProbeCostNanoCNY: row.GroupProbeCostNanoCNY,
			}); err != nil {
				return err
			}
			detailByChannel[row.ChannelId] = channelAmount
			key := channelMonitorCostBackfillKey{ChannelId: row.ChannelId, APIKeyId: row.APIKeyId, APIKeyKey: row.APIKeyKey}
			keyAmount := detailByKey[key]
			if err := addChannelMonitorCostBackfillAmount(&keyAmount, amount); err != nil {
				return err
			}
			detailByKey[key] = keyAmount
		}

		userIdsByToken, err := channelMonitorCostBackfillTokenOwners(ctx, tx, keyRows)
		if err != nil {
			return err
		}
		for _, row := range keyRows {
			if _, exists := channelAmounts[row.ChannelId]; !exists {
				return fmt.Errorf("渠道 %d 存在 API Key 日成本但缺少渠道日总账", row.ChannelId)
			}
			key := channelMonitorCostBackfillKey{ChannelId: row.ChannelId, APIKeyId: row.APIKeyId, APIKeyKey: row.KeyFingerprint}
			residual, err := subtractChannelMonitorCostBackfillAmount(
				channelMonitorCostBackfillAmount{CostNanoCNY: row.CostNanoCNY, SettledCount: row.SettledCount, UnresolvedCount: row.UnresolvedCount},
				detailByKey[key],
			)
			if err != nil {
				return fmt.Errorf("渠道 %d API Key %d 历史明细对账失败: %w", row.ChannelId, row.APIKeyId, err)
			}
			if residual == (channelMonitorCostBackfillAmount{}) {
				continue
			}
			userId := userIdsByToken[row.APIKeyId]
			attribution := string(ChannelMonitorEventUserAttributionUnknown)
			if userId > 0 {
				attribution = string(ChannelMonitorEventUserAttributionInferred)
			}
			if err := addChannelMonitorDailyCostDetail(tx, ChannelDailyCostDelta{
				ChannelId: row.ChannelId, OccurredAt: dayStart, CostNanoCNY: residual.CostNanoCNY,
				SettledDelta: residual.SettledCount, UnresolvedDelta: residual.UnresolvedCount,
				APIKeyId: row.APIKeyId, APIKeyName: row.APIKeyName, KeyFingerprint: row.KeyFingerprint,
				KeyDisplay: row.KeyDisplay, UserId: userId, UserAttribution: attribution,
				ModelName: "unknown", SourceKind: channelMonitorCostBackfillSource,
			}); err != nil {
				return err
			}
			result.RowsWritten++
			keyDetail := detailByKey[key]
			if err := addChannelMonitorCostBackfillAmount(&keyDetail, residual); err != nil {
				return err
			}
			detailByKey[key] = keyDetail
			channelDetail := detailByChannel[row.ChannelId]
			if err := addChannelMonitorCostBackfillAmount(&channelDetail.channelMonitorCostBackfillAmount, residual); err != nil {
				return err
			}
			detailByChannel[row.ChannelId] = channelDetail
		}

		var reconciliation ChannelMonitorCostReconciliation
		reconciliation = ChannelMonitorCostReconciliation{BatchId: batchId, DayStart: dayStart, Status: ChannelMonitorCostReconciliationMatched}
		for channelId, ledger := range channelAmounts {
			detail := detailByChannel[channelId]
			residual, err := subtractChannelMonitorCostBackfillAmount(ledger.channelMonitorCostBackfillAmount, detail.channelMonitorCostBackfillAmount)
			if err != nil {
				return fmt.Errorf("渠道 %d 日成本明细对账失败: %w", channelId, err)
			}
			if residual != (channelMonitorCostBackfillAmount{}) {
				if err := addChannelMonitorDailyCostDetail(tx, ChannelDailyCostDelta{
					ChannelId: channelId, OccurredAt: dayStart, CostNanoCNY: residual.CostNanoCNY,
					SettledDelta: residual.SettledCount, UnresolvedDelta: residual.UnresolvedCount,
					ModelName: "unknown", SourceKind: channelMonitorCostBackfillSource,
					UserAttribution: string(ChannelMonitorEventUserAttributionUnknown),
				}); err != nil {
					return err
				}
				result.RowsWritten++
				result.UnknownResidualCost, err = addNonNegativeInt64(result.UnknownResidualCost, residual.CostNanoCNY)
				if err != nil {
					return err
				}
				reconciliation.UnknownResidualCost, err = addNonNegativeInt64(reconciliation.UnknownResidualCost, residual.CostNanoCNY)
				if err != nil {
					return err
				}
				detail.CostNanoCNY += residual.CostNanoCNY
				detail.SettledCount += residual.SettledCount
				detail.UnresolvedCount += residual.UnresolvedCount
				detailByChannel[channelId] = detail
			}
			reconciliation.LedgerCostNanoCNY, err = addNonNegativeInt64(reconciliation.LedgerCostNanoCNY, ledger.CostNanoCNY)
			if err != nil {
				return err
			}
			reconciliation.DetailCostNanoCNY, err = addNonNegativeInt64(reconciliation.DetailCostNanoCNY, detail.CostNanoCNY)
			if err != nil {
				return err
			}
			reconciliation.LedgerSettledCount, err = addNonNegativeInt64(reconciliation.LedgerSettledCount, ledger.SettledCount)
			if err != nil {
				return err
			}
			reconciliation.DetailSettledCount, err = addNonNegativeInt64(reconciliation.DetailSettledCount, detail.SettledCount)
			if err != nil {
				return err
			}
			reconciliation.LedgerUnresolvedCount, err = addNonNegativeInt64(reconciliation.LedgerUnresolvedCount, ledger.UnresolvedCount)
			if err != nil {
				return err
			}
			reconciliation.DetailUnresolvedCount, err = addNonNegativeInt64(reconciliation.DetailUnresolvedCount, detail.UnresolvedCount)
			if err != nil {
				return err
			}
			probeGap := ledger.ProbeCostNanoCNY - detail.ProbeCostNanoCNY
			groupProbeGap := ledger.GroupProbeCostNanoCNY - detail.GroupProbeCostNanoCNY
			if probeGap < 0 || groupProbeGap < 0 {
				return fmt.Errorf("渠道 %d 探测成本明细超过渠道总账", channelId)
			}
			reconciliation.ProbeCategoryGap, err = addNonNegativeInt64(reconciliation.ProbeCategoryGap, probeGap)
			if err != nil {
				return err
			}
			reconciliation.GroupProbeCategoryGap, err = addNonNegativeInt64(reconciliation.GroupProbeCategoryGap, groupProbeGap)
			if err != nil {
				return err
			}
		}
		if reconciliation.LedgerCostNanoCNY != reconciliation.DetailCostNanoCNY ||
			reconciliation.LedgerSettledCount != reconciliation.DetailSettledCount ||
			reconciliation.LedgerUnresolvedCount != reconciliation.DetailUnresolvedCount {
			reconciliation.Status = ChannelMonitorCostReconciliationMismatch
			reconciliation.ErrorMessage = "成本明细与渠道日总账不一致"
		}
		reconciliation.CompletedAt = now
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "batch_id"}, {Name: "day_start"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "ledger_cost_nano_cny", "detail_cost_nano_cny", "ledger_settled_count", "detail_settled_count",
				"ledger_unresolved_count", "detail_unresolved_count", "probe_category_gap", "group_probe_category_gap",
				"unknown_residual_cost", "completed_at", "error_message",
			}),
		}).Create(&reconciliation).Error; err != nil {
			return err
		}
		if reconciliation.Status == ChannelMonitorCostReconciliationMismatch {
			return errors.New(reconciliation.ErrorMessage)
		}
		if err := tx.Model(&ChannelMonitorCostBackfillCheckpoint{}).
			Where("id = ?", checkpoint.Id).
			Updates(map[string]any{
				"status": ChannelMonitorCostBackfillStatusComplete, "rows_written": result.RowsWritten,
				"completed_at": now, "updated_at": now, "last_error": "",
			}).Error; err != nil {
			return err
		}
		result.ProbeCategoryGap = reconciliation.ProbeCategoryGap
		result.GroupProbeCategoryGap = reconciliation.GroupProbeCategoryGap
		return nil
	})
	if resultErr != nil {
		_ = DB.WithContext(ctx).Model(&ChannelMonitorCostBackfillCheckpoint{}).
			Where("batch_id = ? AND day_start = ? AND status <> ?", batchId, dayStart, ChannelMonitorCostBackfillStatusComplete).
			Updates(map[string]any{
				"status": ChannelMonitorCostBackfillStatusFailed, "last_error": truncateCostBackfillError(resultErr.Error()),
				"updated_at": time.Now().Unix(),
			}).Error
	}
	return result, resultErr
}

func channelMonitorCostBackfillTokenOwners(ctx context.Context, tx *gorm.DB, rows []ChannelDailyAPIKeyCost) (map[int]int, error) {
	ids := make([]int, 0)
	seen := make(map[int]struct{})
	for _, row := range rows {
		if row.APIKeyId > 0 {
			if _, exists := seen[row.APIKeyId]; !exists {
				seen[row.APIKeyId] = struct{}{}
				ids = append(ids, row.APIKeyId)
			}
		}
	}
	owners := make(map[int]int, len(ids))
	if len(ids) == 0 {
		return owners, nil
	}
	var tokens []Token
	if err := tx.WithContext(ctx).Model(&Token{}).Unscoped().Select("id, user_id").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
		return nil, err
	}
	for _, token := range tokens {
		if token.Id > 0 && token.UserId > 0 {
			owners[token.Id] = token.UserId
		}
	}
	return owners, nil
}

func addChannelMonitorCostBackfillResult(result *ChannelMonitorCostBackfillResult, dayResult channelMonitorCostBackfillDayResult) error {
	if result == nil {
		return errors.New("成本回填结果不可用")
	}
	var err error
	result.RowsWritten, err = addNonNegativeInt64(result.RowsWritten, dayResult.RowsWritten)
	if err != nil {
		return err
	}
	result.UnknownResidualCost, err = addNonNegativeInt64(result.UnknownResidualCost, dayResult.UnknownResidualCost)
	if err != nil {
		return err
	}
	result.ProbeCategoryGap, err = addNonNegativeInt64(result.ProbeCategoryGap, dayResult.ProbeCategoryGap)
	if err != nil {
		return err
	}
	result.GroupProbeCategoryGap, err = addNonNegativeInt64(result.GroupProbeCategoryGap, dayResult.GroupProbeCategoryGap)
	return err
}

func validateChannelMonitorCostBackfillAmount(amount channelMonitorCostBackfillAmount) error {
	if amount.CostNanoCNY < 0 || amount.SettledCount < 0 || amount.UnresolvedCount < 0 {
		return errors.New("成本或计数不能为负数")
	}
	return nil
}

func validateChannelMonitorCostBackfillChannelAmount(amount channelMonitorCostBackfillChannelAmount) error {
	if err := validateChannelMonitorCostBackfillAmount(amount.channelMonitorCostBackfillAmount); err != nil {
		return err
	}
	if amount.ProbeCostNanoCNY < 0 || amount.GroupProbeCostNanoCNY < 0 || amount.GroupProbeCostNanoCNY > amount.ProbeCostNanoCNY || amount.ProbeCostNanoCNY > amount.CostNanoCNY {
		return errors.New("探测成本分类无效")
	}
	return nil
}

func addChannelMonitorCostBackfillAmount(target *channelMonitorCostBackfillAmount, delta channelMonitorCostBackfillAmount) error {
	if target == nil {
		return errors.New("成本累计目标不可用")
	}
	if err := validateChannelMonitorCostBackfillAmount(delta); err != nil {
		return err
	}
	var err error
	target.CostNanoCNY, err = addNonNegativeInt64(target.CostNanoCNY, delta.CostNanoCNY)
	if err != nil {
		return err
	}
	target.SettledCount, err = addNonNegativeInt64(target.SettledCount, delta.SettledCount)
	if err != nil {
		return err
	}
	target.UnresolvedCount, err = addNonNegativeInt64(target.UnresolvedCount, delta.UnresolvedCount)
	return err
}

func addChannelMonitorCostBackfillChannelAmount(target *channelMonitorCostBackfillChannelAmount, delta channelMonitorCostBackfillChannelAmount) error {
	if target == nil {
		return errors.New("渠道成本累计目标不可用")
	}
	if err := validateChannelMonitorCostBackfillChannelAmount(delta); err != nil {
		return err
	}
	if err := addChannelMonitorCostBackfillAmount(&target.channelMonitorCostBackfillAmount, delta.channelMonitorCostBackfillAmount); err != nil {
		return err
	}
	var err error
	target.ProbeCostNanoCNY, err = addNonNegativeInt64(target.ProbeCostNanoCNY, delta.ProbeCostNanoCNY)
	if err != nil {
		return err
	}
	target.GroupProbeCostNanoCNY, err = addNonNegativeInt64(target.GroupProbeCostNanoCNY, delta.GroupProbeCostNanoCNY)
	return err
}

func subtractChannelMonitorCostBackfillAmount(total, part channelMonitorCostBackfillAmount) (channelMonitorCostBackfillAmount, error) {
	if err := validateChannelMonitorCostBackfillAmount(total); err != nil {
		return channelMonitorCostBackfillAmount{}, err
	}
	if err := validateChannelMonitorCostBackfillAmount(part); err != nil {
		return channelMonitorCostBackfillAmount{}, err
	}
	if total.CostNanoCNY < part.CostNanoCNY || total.SettledCount < part.SettledCount || total.UnresolvedCount < part.UnresolvedCount {
		return channelMonitorCostBackfillAmount{}, errors.New("成本明细超过日总账")
	}
	return channelMonitorCostBackfillAmount{
		CostNanoCNY: total.CostNanoCNY - part.CostNanoCNY, SettledCount: total.SettledCount - part.SettledCount,
		UnresolvedCount: total.UnresolvedCount - part.UnresolvedCount,
	}, nil
}

func addNonNegativeInt64(current, delta int64) (int64, error) {
	if current < 0 || delta < 0 || current > math.MaxInt64-delta {
		return 0, errors.New("成本回填累计超过 int64 范围")
	}
	return current + delta, nil
}

func truncateCostBackfillError(message string) string {
	if len(message) > 512 {
		return message[:512]
	}
	return message
}
