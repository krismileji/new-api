package model

import (
	"context"
	"errors"
	"math"
	"sort"

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
	if tx == nil {
		tx = DB
	}
	if tx == nil {
		return errors.New("channel daily cost database is unavailable")
	}
	return tx.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return addChannelDailyCostWithCategories(tx, channelId, occurredAt, costNanoCNY, 0, 0, modelDetectionCostNanoCNY, settledDelta, unresolvedDelta)
	})
}

// SettleUnresolvedChannelDailyModelDetectionCost replaces one unresolved
// model-detection fact with its settled amount on the fact's original day.
func SettleUnresolvedChannelDailyModelDetectionCost(ctx context.Context, tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64) error {
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
			return nil
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
	return errors.New("渠道日成本累计超过 int64 范围")
}

func updateChannelDailyCostIfWithinBounds(tx *gorm.DB, channelId int, dayStart int64, occurredAt int64, costNanoCNY int64, probeCostNanoCNY int64, groupProbeCostNanoCNY int64, modelDetectionCostNanoCNY int64, settledDelta int64, unresolvedDelta int64) (bool, error) {
	update := tx.Model(&ChannelDailyCost{}).
		Where("channel_id = ? AND day_start = ?", channelId, dayStart).
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

// GetChannelDailyCostDayTotals returns one aggregated row per calendar day in
// the requested range. The range should be limited to the requested page by
// the caller when displaying paginated date details.
func GetChannelDailyCostDayTotals(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, startTimestamp, endTimestamp, channelId, 0)
}

// GetChannelDailyCostDayTotalsPage applies a database-side limit to an
// already-bounded calendar page. The caller still supplies the page's date
// range so days without a recorded row can be filled by the presentation
// layer without changing the page shape.
func GetChannelDailyCostDayTotalsPage(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, pageSize int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, startTimestamp, endTimestamp, channelId, pageSize)
}

func getChannelDailyCostDayTotals(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, pageSize int) ([]ChannelDailyCostDayTotal, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyCostDayTotal{}, nil
	}
	rows, err := getChannelDailyCosts(ctx, startTimestamp, endTimestamp, channelId)
	if err != nil {
		return nil, err
	}
	totalsByDay := make(map[int64]*ChannelDailyCostDayTotal)
	for _, row := range rows {
		total := totalsByDay[row.DayStart]
		if total == nil {
			total = &ChannelDailyCostDayTotal{DayStart: row.DayStart}
			totalsByDay[row.DayStart] = total
		}
		if err := addChannelDailyCostTotal(total, row); err != nil {
			return nil, err
		}
	}
	dayStarts := make([]int64, 0, len(totalsByDay))
	for dayStart := range totalsByDay {
		dayStarts = append(dayStarts, dayStart)
	}
	sort.Slice(dayStarts, func(i, j int) bool { return dayStarts[i] < dayStarts[j] })
	if pageSize > 0 && len(dayStarts) > pageSize {
		dayStarts = dayStarts[:pageSize]
	}
	totals := make([]ChannelDailyCostDayTotal, 0, len(dayStarts))
	for _, dayStart := range dayStarts {
		totals = append(totals, *totalsByDay[dayStart])
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
