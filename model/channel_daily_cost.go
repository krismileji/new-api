package model

import (
	"context"
	"errors"
	"fmt"
	"math"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelDailyCostNanoPerCNY int64 = 1_000_000_000
	channelDailyCostDaySeconds int64 = 24 * 60 * 60
	channelDailyCostUTC8Offset int64 = 8 * 60 * 60
)

// ChannelDailyCost stores the immutable cost facts settled for one channel on
// one Beijing calendar day. Monetary values use CNY nanos to avoid float
// accumulation drift.
type ChannelDailyCost struct {
	Id                        int64 `gorm:"primaryKey"`
	ChannelId                 int   `gorm:"not null;uniqueIndex:idx_channel_daily_cost_day"`
	DayStart                  int64 `gorm:"not null;uniqueIndex:idx_channel_daily_cost_day;index:idx_channel_daily_cost_day_start"`
	CostNanoCNY               int64 `gorm:"not null"`
	ProbeCostNanoCNY          int64 `gorm:"not null;default:0"`
	GroupProbeCostNanoCNY     int64 `gorm:"not null;default:0"`
	ModelDetectionCostNanoCNY int64 `gorm:"not null;default:0"`
	SettledCount              int64 `gorm:"not null"`
	UnresolvedCount           int64 `gorm:"not null"`
	CreatedAt                 int64 `gorm:"not null"`
	UpdatedAt                 int64 `gorm:"not null"`
}

// ChannelDailyCostDayTotal is a database-aggregated daily total. Keeping the
// aggregation in the query lets callers page calendar ranges without loading
// every channel row for the whole range into memory.
type ChannelDailyCostDayTotal struct {
	DayStart                  int64 `gorm:"column:day_start"`
	CostNanoCNY               int64 `gorm:"column:cost_nano_cny"`
	ProbeCostNanoCNY          int64 `gorm:"column:probe_cost_nano_cny"`
	GroupProbeCostNanoCNY     int64 `gorm:"column:group_probe_cost_nano_cny"`
	ModelDetectionCostNanoCNY int64 `gorm:"column:model_detection_cost_nano_cny"`
	SettledCount              int64 `gorm:"column:settled_count"`
	UnresolvedCount           int64 `gorm:"column:unresolved_count"`
}

// ChannelDailyCostBaseline is the cumulative channel cost captured at one
// balance observation. The timestamp identifies the Beijing calendar day to
// which CostNanoCNY belongs.
type ChannelDailyCostBaseline struct {
	Timestamp   int64
	CostNanoCNY int64
}

// ChannelDailyCostDayStart returns the UTC timestamp at which the containing
// Beijing calendar day starts.
func ChannelDailyCostDayStart(timestamp int64) int64 {
	return ((timestamp+channelDailyCostUTC8Offset)/channelDailyCostDaySeconds)*channelDailyCostDaySeconds - channelDailyCostUTC8Offset
}

// GetChannelDailyCostDelta returns a new cumulative baseline and the exact
// cost added after the previous baseline. Daily rows before the baseline are
// excluded even when the previous observation occurred partway through a day.
func GetChannelDailyCostDelta(ctx context.Context, channelId int, capturedAt int64, previous *ChannelDailyCostBaseline) (ChannelDailyCostBaseline, int64, error) {
	if channelId <= 0 {
		return ChannelDailyCostBaseline{}, 0, errors.New("channel id must be positive")
	}
	if capturedAt <= 0 {
		return ChannelDailyCostBaseline{}, 0, errors.New("cost baseline timestamp must be positive")
	}

	currentDayStart := ChannelDailyCostDayStart(capturedAt)
	startTimestamp := currentDayStart
	if previous != nil {
		if previous.Timestamp <= 0 || previous.CostNanoCNY < 0 {
			return ChannelDailyCostBaseline{}, 0, errors.New("previous cost baseline is invalid")
		}
		if previous.Timestamp > capturedAt {
			previous = nil
		}
	}
	if previous != nil {
		previousDayStart := ChannelDailyCostDayStart(previous.Timestamp)
		if previousDayStart > currentDayStart {
			previous = nil
		} else {
			startTimestamp = previousDayStart
		}
	}

	endTimestamp := capturedAt
	if endTimestamp < math.MaxInt64 {
		endTimestamp++
	}
	costs, err := GetChannelDailyCostsForChannel(ctx, startTimestamp, endTimestamp, channelId)
	if err != nil {
		return ChannelDailyCostBaseline{}, 0, err
	}

	currentCostNanoCNY := int64(0)
	for _, cost := range costs {
		if cost.CostNanoCNY < 0 {
			return ChannelDailyCostBaseline{}, 0, errors.New("channel daily cost must not be negative")
		}
		if cost.DayStart == currentDayStart {
			currentCostNanoCNY = cost.CostNanoCNY
		}
	}
	current := ChannelDailyCostBaseline{
		Timestamp:   capturedAt,
		CostNanoCNY: currentCostNanoCNY,
	}
	if previous == nil {
		return current, 0, nil
	}

	previousDayStart := ChannelDailyCostDayStart(previous.Timestamp)
	previousDayFound := false
	deltaNanoCNY := int64(0)
	for _, cost := range costs {
		if cost.DayStart < previousDayStart || cost.DayStart > currentDayStart {
			continue
		}
		increment := cost.CostNanoCNY
		if cost.DayStart == previousDayStart {
			previousDayFound = true
			if cost.CostNanoCNY < previous.CostNanoCNY {
				return current, 0, errors.New("channel daily cost is lower than its saved baseline")
			}
			increment -= previous.CostNanoCNY
		}
		if increment > math.MaxInt64-deltaNanoCNY {
			return current, 0, errors.New("channel daily cost delta exceeds int64")
		}
		deltaNanoCNY += increment
	}
	if !previousDayFound && previous.CostNanoCNY > 0 {
		return current, 0, errors.New("saved channel daily cost baseline no longer exists")
	}
	return current, deltaNanoCNY, nil
}

// AddChannelDailyCost atomically adds one settled or unresolved event to the
// single daily row for a channel.
func AddChannelDailyCost(ctx context.Context, channelId int, occurredAt int64, costNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	return AddChannelDailyCostWithProbe(ctx, channelId, occurredAt, costNanoCNY, 0, settledDelta, unresolvedDelta)
}

func AddChannelDailyCostWithProbe(ctx context.Context, channelId int, occurredAt int64, costNanoCNY int64, probeCostNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return addChannelDailyCostWithCategories(tx, channelId, occurredAt, costNanoCNY, probeCostNanoCNY, 0, 0, settledDelta, unresolvedDelta)
	})
}

func addChannelDailyCost(tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, probeCostNanoCNY int64, groupProbeCostNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	return addChannelDailyCostWithCategories(tx, channelId, occurredAt, costNanoCNY, probeCostNanoCNY, groupProbeCostNanoCNY, 0, settledDelta, unresolvedDelta)
}

// AddChannelDailyCostWithModelDetection records a settled model-detection
// request in the channel daily total while keeping its cost distinguishable
// from ordinary traffic and status probes. The caller may pass a transaction
// so the category update commits together with the source settlement event.
func AddChannelDailyCostWithModelDetection(ctx context.Context, tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, modelDetectionCostNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	return AddChannelDailyCostWithModelDetectionAndModel(ctx, tx, channelId, occurredAt, costNanoCNY, modelDetectionCostNanoCNY, settledDelta, unresolvedDelta, "unknown")
}

// AddChannelDailyCostWithModelDetectionAndModel records the model-detection
// category and its drill-down fact in one transaction. Model detection has no
// inbound user/API Key, so those dimensions intentionally remain unknown.
func AddChannelDailyCostWithModelDetectionAndModel(ctx context.Context, tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, modelDetectionCostNanoCNY int64, settledDelta int64, unresolvedDelta int64, modelName string) error {
	if tx == nil {
		tx = DB
	}
	if tx == nil {
		return errors.New("channel daily cost database is unavailable")
	}
	return tx.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := addChannelDailyCostWithCategories(tx, channelId, occurredAt, costNanoCNY, 0, 0, modelDetectionCostNanoCNY, settledDelta, unresolvedDelta); err != nil {
			return err
		}
		if !tx.Migrator().HasTable(&ChannelMonitorDailyCostDetail{}) {
			return nil
		}
		return addChannelMonitorDailyCostDetail(tx, ChannelDailyCostDelta{
			ChannelId: channelId, OccurredAt: occurredAt, CostNanoCNY: costNanoCNY,
			SettledDelta: settledDelta, UnresolvedDelta: unresolvedDelta,
			UserAttribution: string(ChannelMonitorEventUserAttributionUnknown),
			ModelName:       modelName, SourceKind: string(ChannelMonitorEventSourceModelDetection),
		})
	})
}

// SettleUnresolvedChannelDailyModelDetectionCost replaces one unresolved
// model-detection fact with its settled amount on the fact's original day.
func SettleUnresolvedChannelDailyModelDetectionCost(ctx context.Context, tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64) error {
	return SettleUnresolvedChannelDailyModelDetectionCostWithModel(ctx, tx, channelId, occurredAt, costNanoCNY, "unknown")
}

// SettleUnresolvedChannelDailyModelDetectionCostWithModel replaces the
// unresolved model-detection detail at the same time as the authoritative
// channel ledger replacement.
func SettleUnresolvedChannelDailyModelDetectionCostWithModel(ctx context.Context, tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, modelName string) error {
	if channelId <= 0 {
		return errors.New("channel id must be positive")
	}
	if costNanoCNY < 0 {
		return errors.New("daily cost must not be negative")
	}
	if tx == nil {
		tx = DB
	}
	if tx == nil {
		return errors.New("channel daily cost database is unavailable")
	}

	return tx.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dayStart := ChannelDailyCostDayStart(occurredAt)
		updated := tx.Model(&ChannelDailyCost{}).
			Where("channel_id = ? AND day_start = ?", channelId, dayStart).
			Where("cost_nano_cny >= 0 AND cost_nano_cny <= ?", math.MaxInt64-costNanoCNY).
			Where("model_detection_cost_nano_cny >= 0 AND model_detection_cost_nano_cny <= ?", math.MaxInt64-costNanoCNY).
			Where("settled_count >= 0 AND settled_count < ?", int64(math.MaxInt64)).
			Where("unresolved_count > 0").
			Updates(map[string]interface{}{
				"cost_nano_cny":                 gorm.Expr("cost_nano_cny + ?", costNanoCNY),
				"model_detection_cost_nano_cny": gorm.Expr("model_detection_cost_nano_cny + ?", costNanoCNY),
				"settled_count":                 gorm.Expr("settled_count + ?", 1),
				"unresolved_count":              gorm.Expr("unresolved_count - ?", 1),
				"updated_at":                    occurredAt,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 1 {
			return settleChannelMonitorModelDetectionDetail(tx, channelId, dayStart, costNanoCNY, modelName, occurredAt)
		}

		var existingId int64
		err := tx.Model(&ChannelDailyCost{}).
			Select("id").
			Where("channel_id = ? AND day_start = ?", channelId, dayStart).
			Take(&existingId).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("模型检测未解析日成本记录不存在")
		}
		if err != nil {
			return err
		}
		return errors.New("模型检测日成本结算超过 int64 范围或未解析计数不足")
	})
}

func settleChannelMonitorModelDetectionDetail(tx *gorm.DB, channelId int, dayStart int64, costNanoCNY int64, modelName string, updatedAt int64) error {
	if !tx.Migrator().HasTable(&ChannelMonitorDailyCostDetail{}) {
		return nil
	}
	modelKey := ChannelMonitorDailyCostModelKey(modelName)
	updated := tx.Model(&ChannelMonitorDailyCostDetail{}).
		Where("day_start = ? AND channel_id = ? AND user_id = 0 AND api_key_id = 0 AND api_key_key = ? AND model_key = ? AND source_kind = ?", dayStart, channelId, "", modelKey, string(ChannelMonitorEventSourceModelDetection)).
		Where("cost_nano_cny >= 0 AND cost_nano_cny <= ? AND settled_count >= 0 AND settled_count < ? AND unresolved_count > 0", math.MaxInt64-costNanoCNY, int64(math.MaxInt64)).
		Updates(map[string]any{
			"cost_nano_cny":    gorm.Expr("cost_nano_cny + ?", costNanoCNY),
			"settled_count":    gorm.Expr("settled_count + ?", 1),
			"unresolved_count": gorm.Expr("unresolved_count - ?", 1),
			"updated_at":       updatedAt,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 1 {
		return nil
	}
	return addChannelMonitorDailyCostDetail(tx, ChannelDailyCostDelta{
		ChannelId: channelId, OccurredAt: updatedAt, CostNanoCNY: costNanoCNY, SettledDelta: 1,
		UserAttribution: string(ChannelMonitorEventUserAttributionUnknown), ModelName: modelName,
		SourceKind: string(ChannelMonitorEventSourceModelDetection),
	})
}

func addChannelDailyCostWithCategories(tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, probeCostNanoCNY int64, groupProbeCostNanoCNY int64, modelDetectionCostNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	if channelId <= 0 {
		return errors.New("channel id must be positive")
	}
	if costNanoCNY < 0 {
		return errors.New("daily cost must not be negative")
	}
	if probeCostNanoCNY < 0 || probeCostNanoCNY > costNanoCNY {
		return errors.New("daily probe cost must be between zero and total cost")
	}
	if groupProbeCostNanoCNY < 0 || groupProbeCostNanoCNY > probeCostNanoCNY {
		return errors.New("daily group probe cost must be between zero and probe cost")
	}
	if modelDetectionCostNanoCNY < 0 || modelDetectionCostNanoCNY > costNanoCNY {
		return errors.New("daily model detection cost must be between zero and total cost")
	}
	if probeCostNanoCNY > math.MaxInt64-modelDetectionCostNanoCNY || probeCostNanoCNY+modelDetectionCostNanoCNY > costNanoCNY {
		return errors.New("daily category cost must be between zero and total cost")
	}
	if settledDelta < 0 || unresolvedDelta < 0 || (settledDelta == 0 && unresolvedDelta == 0) {
		return errors.New("daily cost event count must be positive")
	}

	record := ChannelDailyCost{
		ChannelId: channelId,
		DayStart:  ChannelDailyCostDayStart(occurredAt),
		CreatedAt: occurredAt,
		UpdatedAt: occurredAt,
	}
	// Multiple requests can create the first row for the same
	// channel/day concurrently. Try the conflict-safe insert before a missing-row
	// update can acquire competing MySQL gap locks, then apply this event through
	// one bounded atomic increment regardless of which request won the race.
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
	if created.Error != nil {
		return created.Error
	}
	updated, err := updateChannelDailyCostIfWithinBounds(
		tx, channelId, record.DayStart, occurredAt,
		costNanoCNY, probeCostNanoCNY, groupProbeCostNanoCNY, modelDetectionCostNanoCNY, settledDelta, unresolvedDelta,
	)
	if err != nil || updated {
		return err
	}
	return fmt.Errorf("%w: 渠道日成本累计超过 int64 范围", ErrChannelDailyCostLedgerOverflow)
}

func updateChannelDailyCostIfWithinBounds(tx *gorm.DB, channelId int, dayStart int64, occurredAt int64, costNanoCNY int64, probeCostNanoCNY int64, groupProbeCostNanoCNY int64, modelDetectionCostNanoCNY int64, settledDelta int64, unresolvedDelta int64) (bool, error) {
	update := tx.Model(&ChannelDailyCost{}).
		Where("channel_id = ? AND day_start = ?", channelId, dayStart).
		Where("cost_nano_cny >= 0 AND probe_cost_nano_cny >= 0 AND group_probe_cost_nano_cny >= 0 AND model_detection_cost_nano_cny >= 0").
		Where("settled_count >= 0 AND unresolved_count >= 0").
		Where("cost_nano_cny <= ?", math.MaxInt64-costNanoCNY).
		Where("probe_cost_nano_cny <= ?", math.MaxInt64-probeCostNanoCNY).
		Where("group_probe_cost_nano_cny <= ?", math.MaxInt64-groupProbeCostNanoCNY).
		Where("model_detection_cost_nano_cny <= ?", math.MaxInt64-modelDetectionCostNanoCNY).
		Where("settled_count <= ?", math.MaxInt64-settledDelta).
		Where("unresolved_count <= ?", math.MaxInt64-unresolvedDelta).
		Updates(map[string]interface{}{
			"cost_nano_cny":                 gorm.Expr("cost_nano_cny + ?", costNanoCNY),
			"probe_cost_nano_cny":           gorm.Expr("probe_cost_nano_cny + ?", probeCostNanoCNY),
			"group_probe_cost_nano_cny":     gorm.Expr("group_probe_cost_nano_cny + ?", groupProbeCostNanoCNY),
			"model_detection_cost_nano_cny": gorm.Expr("model_detection_cost_nano_cny + ?", modelDetectionCostNanoCNY),
			"settled_count":                 gorm.Expr("settled_count + ?", settledDelta),
			"unresolved_count":              gorm.Expr("unresolved_count + ?", unresolvedDelta),
			"updated_at":                    occurredAt,
		})
	if update.Error != nil {
		return false, update.Error
	}
	return update.RowsAffected == 1, nil
}

func GetChannelDailyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelDailyCost, error) {
	return getChannelDailyCosts(ctx, startTimestamp, endTimestamp, 0)
}

func GetChannelDailyCostsForChannel(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCost, error) {
	return getChannelDailyCosts(ctx, startTimestamp, endTimestamp, channelId)
}

func GetChannelDailyCostsForStatusProbeOverview(
	ctx context.Context,
	db *gorm.DB,
	startTimestamp int64,
	endTimestamp int64,
	channelIDs []int,
) ([]ChannelDailyCost, error) {
	if startTimestamp >= endTimestamp || channelIDs != nil && len(channelIDs) == 0 {
		return []ChannelDailyCost{}, nil
	}
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	query := queryDB.Select("channel_id", "probe_cost_nano_cny").
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp)
	if channelIDs != nil {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	var costs []ChannelDailyCost
	err = query.Order("channel_id ASC").Find(&costs).Error
	return costs, err
}

// GetChannelDailyCostDayTotals returns one aggregated row per calendar day in
// the requested range. The range should be limited to the requested page by
// the caller when displaying paginated date details.
func GetChannelDailyCostDayTotals(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCostDayTotal, error) {
	return GetChannelDailyCostDayTotalsWithDB(ctx, DB, startTimestamp, endTimestamp, channelId)
}

// GetChannelDailyCostDayTotalsWithDB lets a caller combine this aggregation
// with related ledger reads in one request-scoped consistent transaction.
func GetChannelDailyCostDayTotalsWithDB(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, db, startTimestamp, endTimestamp, channelId, 0, 0)
}

// GetChannelDailyCostDayTotalsPage applies a database-side limit to an
// already-bounded calendar page. The caller still supplies the page's date
// range so days without a recorded row can be filled by the presentation
// layer without changing the page shape.
func GetChannelDailyCostDayTotalsPage(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, pageSize int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, DB, startTimestamp, endTimestamp, channelId, pageSize, 0)
}

// GetChannelDailyCostDayTotalsPageWithOffset is useful to callers that page
// over rows that are guaranteed to exist for every calendar day. The monitor
// controller uses a bounded calendar window (so missing days can be filled
// with zeroes) and therefore leaves offset at zero.
func GetChannelDailyCostDayTotalsPageWithOffset(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, pageSize int, offset int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, DB, startTimestamp, endTimestamp, channelId, pageSize, offset)
}

// ChannelDailyCostChannelTotal is a database-aggregated total for one
// channel in a bounded range. Keeping this aggregation in SQL avoids loading
// every daily row when the monitor needs channel-level detail.
type ChannelDailyCostChannelTotal struct {
	ChannelId                 int   `gorm:"column:channel_id"`
	CostNanoCNY               int64 `gorm:"column:cost_nano_cny"`
	ProbeCostNanoCNY          int64 `gorm:"column:probe_cost_nano_cny"`
	GroupProbeCostNanoCNY     int64 `gorm:"column:group_probe_cost_nano_cny"`
	ModelDetectionCostNanoCNY int64 `gorm:"column:model_detection_cost_nano_cny"`
	SettledCount              int64 `gorm:"column:settled_count"`
	UnresolvedCount           int64 `gorm:"column:unresolved_count"`
	// Detail* fields are populated only when a detail day is requested. They
	// let the controller derive the selected-day channel rows from the same
	// grouped query that supplies range totals and coverage.
	DetailCostNanoCNY               int64 `gorm:"column:detail_cost_nano_cny"`
	DetailProbeCostNanoCNY          int64 `gorm:"column:detail_probe_cost_nano_cny"`
	DetailGroupProbeCostNanoCNY     int64 `gorm:"column:detail_group_probe_cost_nano_cny"`
	DetailModelDetectionCostNanoCNY int64 `gorm:"column:detail_model_detection_cost_nano_cny"`
	DetailSettledCount              int64 `gorm:"column:detail_settled_count"`
	DetailUnresolvedCount           int64 `gorm:"column:detail_unresolved_count"`
}

// GetChannelDailyCostChannelTotals aggregates daily rows in the database and
// returns one row per channel. The optional channelId narrows the range to a
// single channel while retaining the same SQL shape on all supported
// dialects.
func GetChannelDailyCostChannelTotals(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCostChannelTotal, error) {
	return GetChannelDailyCostChannelTotalsWithDB(ctx, DB, startTimestamp, endTimestamp, channelId)
}

// GetChannelDailyCostChannelTotalsWithDB lets a caller combine this
// aggregation with related ledger reads in one request-scoped consistent
// transaction.
func GetChannelDailyCostChannelTotalsWithDB(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCostChannelTotal, error) {
	return getChannelDailyCostChannelTotals(ctx, db, startTimestamp, endTimestamp, channelId, 0)
}

// GetChannelDailyCostChannelTotalsWithDetail aggregates the complete range
// and, when detailDayStart is positive, the selected calendar day in one SQL
// GROUP BY query. Keeping both views together avoids scanning an overlapping
// range twice for a detail request.
func GetChannelDailyCostChannelTotalsWithDetail(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, detailDayStart int64) ([]ChannelDailyCostChannelTotal, error) {
	return GetChannelDailyCostChannelTotalsWithDetailAndDB(ctx, DB, startTimestamp, endTimestamp, channelId, detailDayStart)
}

// GetChannelDailyCostChannelTotalsWithDetailAndDB reads range and selected-day
// totals from a caller-provided transaction.
func GetChannelDailyCostChannelTotalsWithDetailAndDB(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int, detailDayStart int64) ([]ChannelDailyCostChannelTotal, error) {
	return getChannelDailyCostChannelTotals(ctx, db, startTimestamp, endTimestamp, channelId, detailDayStart)
}

func getChannelDailyCostChannelTotals(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int, detailDayStart int64) ([]ChannelDailyCostChannelTotal, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyCostChannelTotal{}, nil
	}
	queryDB, err := channelMonitorCostQueryDB(ctx, db)
	if err != nil {
		return nil, err
	}
	selectColumns := "channel_id, SUM(cost_nano_cny) AS cost_nano_cny, SUM(probe_cost_nano_cny) AS probe_cost_nano_cny, SUM(group_probe_cost_nano_cny) AS group_probe_cost_nano_cny, SUM(model_detection_cost_nano_cny) AS model_detection_cost_nano_cny, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count"
	// Keep the CASE expressions parameterized. CASE/SUM is supported by
	// SQLite, MySQL, and PostgreSQL without dialect-specific date functions.
	args := make([]interface{}, 0, 6)
	if detailDayStart > 0 {
		selectColumns += ", SUM(CASE WHEN day_start = ? THEN cost_nano_cny ELSE 0 END) AS detail_cost_nano_cny"
		selectColumns += ", SUM(CASE WHEN day_start = ? THEN probe_cost_nano_cny ELSE 0 END) AS detail_probe_cost_nano_cny"
		selectColumns += ", SUM(CASE WHEN day_start = ? THEN group_probe_cost_nano_cny ELSE 0 END) AS detail_group_probe_cost_nano_cny"
		selectColumns += ", SUM(CASE WHEN day_start = ? THEN model_detection_cost_nano_cny ELSE 0 END) AS detail_model_detection_cost_nano_cny"
		selectColumns += ", SUM(CASE WHEN day_start = ? THEN settled_count ELSE 0 END) AS detail_settled_count"
		selectColumns += ", SUM(CASE WHEN day_start = ? THEN unresolved_count ELSE 0 END) AS detail_unresolved_count"
		for i := 0; i < 6; i++ {
			args = append(args, detailDayStart)
		}
	}
	query := queryDB.Model(&ChannelDailyCost{}).
		Select(selectColumns, args...).
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp).
		Group("channel_id").Order("channel_id ASC")
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	var totals []ChannelDailyCostChannelTotal
	if err := query.Scan(&totals).Error; err != nil {
		return nil, fmt.Errorf("渠道日成本渠道汇总超过 int64 范围或查询失败: %w", err)
	}
	for _, total := range totals {
		if total.CostNanoCNY < 0 || total.ProbeCostNanoCNY < 0 || total.GroupProbeCostNanoCNY < 0 || total.ModelDetectionCostNanoCNY < 0 || total.SettledCount < 0 || total.UnresolvedCount < 0 {
			return nil, errors.New("渠道日成本汇总包含负数")
		}
		if detailDayStart > 0 && (total.DetailCostNanoCNY < 0 || total.DetailProbeCostNanoCNY < 0 || total.DetailGroupProbeCostNanoCNY < 0 || total.DetailModelDetectionCostNanoCNY < 0 || total.DetailSettledCount < 0 || total.DetailUnresolvedCount < 0) {
			return nil, errors.New("渠道日成本详情汇总包含负数")
		}
	}
	return totals, nil
}

func getChannelDailyCostDayTotals(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int, pageSize int, offset int) ([]ChannelDailyCostDayTotal, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyCostDayTotal{}, nil
	}
	queryDB, err := channelMonitorCostQueryDB(ctx, db)
	if err != nil {
		return nil, err
	}
	query := queryDB.Model(&ChannelDailyCost{}).
		Select("day_start, SUM(cost_nano_cny) AS cost_nano_cny, SUM(probe_cost_nano_cny) AS probe_cost_nano_cny, SUM(group_probe_cost_nano_cny) AS group_probe_cost_nano_cny, SUM(model_detection_cost_nano_cny) AS model_detection_cost_nano_cny, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count").
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp).
		Group("day_start").Order("day_start ASC")
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if pageSize > 0 {
		query = query.Limit(pageSize)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	var totals []ChannelDailyCostDayTotal
	if err := query.Scan(&totals).Error; err != nil {
		return nil, fmt.Errorf("渠道日成本日汇总超过 int64 范围或查询失败: %w", err)
	}
	for _, total := range totals {
		if total.CostNanoCNY < 0 || total.ProbeCostNanoCNY < 0 || total.GroupProbeCostNanoCNY < 0 || total.ModelDetectionCostNanoCNY < 0 || total.SettledCount < 0 || total.UnresolvedCount < 0 {
			return nil, errors.New("渠道日成本汇总包含负数")
		}
	}
	return totals, nil
}

func addChannelDailyCostTotal(total *ChannelDailyCostDayTotal, row ChannelDailyCost) error {
	values := []struct {
		target *int64
		delta  int64
	}{
		{&total.CostNanoCNY, row.CostNanoCNY},
		{&total.ProbeCostNanoCNY, row.ProbeCostNanoCNY},
		{&total.GroupProbeCostNanoCNY, row.GroupProbeCostNanoCNY},
		{&total.ModelDetectionCostNanoCNY, row.ModelDetectionCostNanoCNY},
		{&total.SettledCount, row.SettledCount},
		{&total.UnresolvedCount, row.UnresolvedCount},
	}
	for _, value := range values {
		if value.delta < 0 || *value.target > math.MaxInt64-value.delta {
			return errors.New("渠道日成本汇总超过 int64 范围")
		}
		*value.target += value.delta
	}
	return nil
}

func getChannelDailyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCost, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyCost{}, nil
	}
	query := DB.WithContext(ctx).
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp)
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	var costs []ChannelDailyCost
	err := query.Order("day_start ASC, channel_id ASC").Find(&costs).Error
	return costs, err
}
