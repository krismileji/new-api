package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelModelDetectionCostStatusPending    = "pending"
	ChannelModelDetectionCostStatusNotStarted = "not_started"
	ChannelModelDetectionCostStatusSettled    = "settled"
	ChannelModelDetectionCostStatusUnresolved = "unresolved"
	ChannelModelDetectionCostStatusPartial    = "partial"

	channelModelDetectionCostErrorSnapshotUnavailable = "cost_snapshot_unavailable"
	channelModelDetectionCostErrorConversionFailed    = "cost_conversion_failed"
)

var (
	ErrChannelModelDetectionCostConflict = errors.New("模型检测成本事件与已有事实冲突")
	ErrChannelModelDetectionCostOverflow = errors.New("模型检测成本金额超过可表示范围")
)

// ChannelModelDetectionCostSnapshot is the immutable upstream-cost snapshot
// captured before one HTTP attempt. A nil field means that part of the cost is
// unknown; it never means zero.
type ChannelModelDetectionCostSnapshot struct {
	CostRatioCNY *string
	QuotaPerUnit *int64
}

type ChannelModelDetectionCostAttemptInput struct {
	CostEventId            string
	RunId                  string
	TargetId               int64
	ExecutionId            int64
	ChannelId              int
	RequestModel           string
	ClaimedModel           string
	Preset                 string
	DetectorRequestId      string
	AttemptNo              int
	RequestId              string
	UpstreamKeyId          string
	UpstreamKeyFingerprint string
	UpstreamKeyDisplay     string
	EstimatedQuota         int64
	Snapshot               ChannelModelDetectionCostSnapshot
	CreatedAt              int64
}

type ChannelModelDetectionCostSettlementInput struct {
	CostEventId       string
	SettledQuota      int64
	CostBasisQuota    int64
	InputTokens       int64
	OutputTokens      int64
	TotalTokens       int64
	UsageSource       string
	UsageAvailable    bool
	UpstreamRequestId string
	SettledAt         int64
}

// ChannelModelDetectionCostUnresolvedInput records the best evidence that is
// available after a dispatched request cannot be fully settled. When
// UsageSource is empty, the frozen estimate determines local_estimate versus
// unavailable. Authoritative usage may still be stored when only the cost
// snapshot is unavailable.
type ChannelModelDetectionCostUnresolvedInput struct {
	CostEventId           string
	UsageSource           string
	UsageAvailable        bool
	InputTokens           int64
	OutputTokens          int64
	TotalTokens           int64
	SettledQuota          *int64
	CostBasisQuota        *int64
	UpstreamRequestId     string
	ErrorCode             string
	SanitizedErrorMessage string
	UpdatedAt             int64
}

type ChannelModelDetectionCostFilter struct {
	RunId       string
	RunIds      []string
	ExecutionId int64
	TargetId    int64
	ChannelId   int
}

type ChannelModelDetectionCostAggregate struct {
	EstimatedQuota             int64
	EstimatedCostNanoCNY       *int64
	CostEstimateUnknownCount   int64
	SettledQuota               int64
	CostBasisQuota             int64
	SettledCostNanoCNY         *int64
	UnresolvedCostNanoCNY      *int64
	UnresolvedCostUnknownCount int64
	SettledRequestCount        int64
	UnresolvedRequestCount     int64
	PendingRequestCount        int64
	NotStartedRequestCount     int64
	InputTokens                int64
	OutputTokens               int64
	TotalTokens                int64
	UsageAvailable             bool
	Status                     string
}

type channelModelDetectionCostEventKey struct {
	CostEventId            string
	RunId                  string
	TargetId               int64
	ExecutionId            int64
	ChannelId              int
	RequestModel           string
	ClaimedModel           string
	Preset                 string
	DetectorRequestId      string
	AttemptNo              int
	RequestId              string
	UpstreamKeyId          string
	UpstreamKeyFingerprint string
	UpstreamKeyDisplay     string
	EstimatedQuota         int64
	EstimatedCostNanoCNY   *int64
	CostRatioCNY           *string
	QuotaPerUnit           *int64
	CostScope              string
}

// CaptureChannelModelDetectionCostSnapshot reuses the channel-cost snapshot
// source used by ChannelDailyCost without writing that aggregate. A missing
// channel ratio is returned as a partial snapshot so the attempt can retain a
// null cost instead of being misreported as free.
func CaptureChannelModelDetectionCostSnapshot(channelId int) (ChannelModelDetectionCostSnapshot, error) {
	if channelId <= 0 {
		return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
	}
	snapshot, err := getChannelDailyCostSnapshot(channelId)
	if err != nil {
		return ChannelModelDetectionCostSnapshot{}, err
	}
	if math.IsNaN(snapshot.QuotaPerUnit) || math.IsInf(snapshot.QuotaPerUnit, 0) || snapshot.QuotaPerUnit <= 0 {
		return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
	}
	quotaPerUnitDecimal := decimal.NewFromFloat(snapshot.QuotaPerUnit)
	if !quotaPerUnitDecimal.Equal(quotaPerUnitDecimal.Truncate(0)) || quotaPerUnitDecimal.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
	}
	quotaPerUnit := quotaPerUnitDecimal.IntPart()
	result := ChannelModelDetectionCostSnapshot{QuotaPerUnit: &quotaPerUnit}
	if !snapshot.Configured {
		return result, nil
	}
	if math.IsNaN(snapshot.CostRatioCNY) || math.IsInf(snapshot.CostRatioCNY, 0) || snapshot.CostRatioCNY < 0 {
		return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
	}
	ratio := decimal.NewFromFloat(snapshot.CostRatioCNY).String()
	result.CostRatioCNY = &ratio
	return normalizeChannelModelDetectionCostSnapshot(result)
}

func CalculateChannelModelDetectionSettledCostNanoCNY(costBasisQuota int64, costRatioCNY string, quotaPerUnit int64) (int64, error) {
	return calculateChannelModelDetectionCostNanoCNY(costBasisQuota, costRatioCNY, quotaPerUnit, false)
}

func CalculateChannelModelDetectionUnresolvedCostNanoCNY(estimatedQuota int64, costRatioCNY string, quotaPerUnit int64) (int64, error) {
	return calculateChannelModelDetectionCostNanoCNY(estimatedQuota, costRatioCNY, quotaPerUnit, true)
}

func calculateChannelModelDetectionCostNanoCNY(quota int64, costRatioCNY string, quotaPerUnit int64, roundUp bool) (int64, error) {
	if quota < 0 || quotaPerUnit <= 0 {
		return 0, model.ErrChannelModelDetectionInvalidCost
	}
	ratio, err := parseChannelModelDetectionCostRatio(costRatioCNY)
	if err != nil {
		return 0, err
	}
	cost := decimal.NewFromInt(quota).
		Div(decimal.NewFromInt(quotaPerUnit)).
		Mul(ratio).
		Mul(decimal.NewFromInt(model.ChannelModelDetectionNanoPerCNY))
	if roundUp {
		cost = cost.Ceil()
	} else {
		cost = cost.Round(0)
	}
	if cost.IsNegative() {
		return 0, model.ErrChannelModelDetectionInvalidCost
	}
	if cost.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrChannelModelDetectionCostOverflow
	}
	return cost.IntPart(), nil
}

func parseChannelModelDetectionCostRatio(value string) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return decimal.Zero, model.ErrChannelModelDetectionInvalidCost
	}
	ratio, err := decimal.NewFromString(value)
	if err != nil || ratio.IsNegative() || ratio.GreaterThan(decimal.NewFromInt(maxUpstreamGroupRatio)) {
		return decimal.Zero, model.ErrChannelModelDetectionInvalidCost
	}
	return ratio, nil
}

func normalizeChannelModelDetectionCostSnapshot(snapshot ChannelModelDetectionCostSnapshot) (ChannelModelDetectionCostSnapshot, error) {
	result := ChannelModelDetectionCostSnapshot{}
	if snapshot.QuotaPerUnit != nil {
		if *snapshot.QuotaPerUnit <= 0 {
			return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
		}
		quotaPerUnit := *snapshot.QuotaPerUnit
		result.QuotaPerUnit = &quotaPerUnit
	}
	if snapshot.CostRatioCNY != nil {
		ratio, err := parseChannelModelDetectionCostRatio(*snapshot.CostRatioCNY)
		if err != nil {
			return ChannelModelDetectionCostSnapshot{}, err
		}
		canonical := ratio.String()
		result.CostRatioCNY = &canonical
	}
	return result, nil
}

// PrepareChannelModelDetectionCostEvent writes the immutable prepared fact.
// Replaying the same cost_event_id returns the stored row; reusing an attempt
// identity with different facts is rejected.
func PrepareChannelModelDetectionCostEvent(ctx context.Context, tx *gorm.DB, input ChannelModelDetectionCostAttemptInput) (model.ChannelModelDetectionCostEvent, bool, error) {
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, false, err
	}
	if err := validateChannelModelDetectionCostAttemptInput(input); err != nil {
		return model.ChannelModelDetectionCostEvent{}, false, err
	}
	snapshot, err := normalizeChannelModelDetectionCostSnapshot(input.Snapshot)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, false, err
	}
	var estimatedCost *int64
	if snapshot.CostRatioCNY != nil && snapshot.QuotaPerUnit != nil {
		cost, err := CalculateChannelModelDetectionUnresolvedCostNanoCNY(input.EstimatedQuota, *snapshot.CostRatioCNY, *snapshot.QuotaPerUnit)
		if err != nil {
			return model.ChannelModelDetectionCostEvent{}, false, err
		}
		estimatedCost = &cost
	}
	event := model.ChannelModelDetectionCostEvent{
		CostEventId:            strings.TrimSpace(input.CostEventId),
		RunId:                  strings.TrimSpace(input.RunId),
		TargetId:               input.TargetId,
		ExecutionId:            input.ExecutionId,
		ChannelId:              input.ChannelId,
		RequestModel:           strings.TrimSpace(input.RequestModel),
		ClaimedModel:           strings.TrimSpace(input.ClaimedModel),
		Preset:                 strings.TrimSpace(input.Preset),
		DetectorRequestId:      strings.TrimSpace(input.DetectorRequestId),
		AttemptNo:              input.AttemptNo,
		RequestId:              strings.TrimSpace(input.RequestId),
		UpstreamKeyId:          strings.TrimSpace(input.UpstreamKeyId),
		UpstreamKeyFingerprint: strings.TrimSpace(input.UpstreamKeyFingerprint),
		UpstreamKeyDisplay:     strings.TrimSpace(input.UpstreamKeyDisplay),
		DispatchState:          model.ChannelModelDetectionDispatchPrepared,
		SettlementStatus:       model.ChannelModelDetectionSettlementPending,
		UsageSource:            model.ChannelModelDetectionUsageUnavailable,
		EstimatedQuota:         input.EstimatedQuota,
		EstimatedCostNanoCNY:   estimatedCost,
		CostRatioCNY:           snapshot.CostRatioCNY,
		QuotaPerUnit:           snapshot.QuotaPerUnit,
		CostScope:              model.ChannelModelDetectionCostScopeChannelUpstreamAPI,
		CreatedAt:              input.CreatedAt,
		UpdatedAt:              input.CreatedAt,
	}
	created := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
	if created.Error != nil {
		return model.ChannelModelDetectionCostEvent{}, false, created.Error
	}
	if created.RowsAffected == 1 {
		return event, true, nil
	}

	var existing model.ChannelModelDetectionCostEvent
	if err := useDB.Where("cost_event_id = ?", event.CostEventId).First(&existing).Error; err == nil {
		if !channelModelDetectionPreparedFactsEqual(existing, event) {
			return model.ChannelModelDetectionCostEvent{}, false, ErrChannelModelDetectionCostConflict
		}
		return existing, false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ChannelModelDetectionCostEvent{}, false, err
	}
	if err := useDB.Where("detector_request_id = ? AND attempt_no = ?", event.DetectorRequestId, event.AttemptNo).First(&existing).Error; err == nil {
		return model.ChannelModelDetectionCostEvent{}, false, ErrChannelModelDetectionCostConflict
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ChannelModelDetectionCostEvent{}, false, err
	}
	return model.ChannelModelDetectionCostEvent{}, false, errors.New("模型检测成本事件未能持久化")
}

func validateChannelModelDetectionCostAttemptInput(input ChannelModelDetectionCostAttemptInput) error {
	if strings.TrimSpace(input.CostEventId) == "" || len(strings.TrimSpace(input.CostEventId)) > 64 ||
		strings.TrimSpace(input.RunId) == "" || len(strings.TrimSpace(input.RunId)) > 64 ||
		strings.TrimSpace(input.DetectorRequestId) == "" || len(strings.TrimSpace(input.DetectorRequestId)) > 128 ||
		input.TargetId <= 0 || input.ExecutionId <= 0 || input.ChannelId <= 0 || input.AttemptNo <= 0 || input.EstimatedQuota < 0 {
		return model.ErrChannelModelDetectionInvalidCost
	}
	if strings.TrimSpace(input.RequestModel) == "" || len(strings.TrimSpace(input.RequestModel)) > 255 ||
		!model.IsChannelModelDetectionClaimedModel(strings.TrimSpace(input.ClaimedModel)) ||
		!model.IsChannelModelDetectionPreset(strings.TrimSpace(input.Preset)) {
		return model.ErrChannelModelDetectionInvalidCost
	}
	for value, limit := range map[string]int{
		strings.TrimSpace(input.RequestId):              128,
		strings.TrimSpace(input.UpstreamKeyId):          128,
		strings.TrimSpace(input.UpstreamKeyFingerprint): 128,
		strings.TrimSpace(input.UpstreamKeyDisplay):     128,
	} {
		if len(value) > limit {
			return model.ErrChannelModelDetectionInvalidCost
		}
	}
	return nil
}

func channelModelDetectionPreparedFactsEqual(existing, expected model.ChannelModelDetectionCostEvent) bool {
	existingKey := channelModelDetectionCostEventKeyFromEvent(existing)
	expectedKey := channelModelDetectionCostEventKeyFromEvent(expected)
	return existingKey.CostEventId == expectedKey.CostEventId &&
		existingKey.RunId == expectedKey.RunId && existingKey.TargetId == expectedKey.TargetId &&
		existingKey.ExecutionId == expectedKey.ExecutionId && existingKey.ChannelId == expectedKey.ChannelId &&
		existingKey.RequestModel == expectedKey.RequestModel && existingKey.ClaimedModel == expectedKey.ClaimedModel &&
		existingKey.Preset == expectedKey.Preset && existingKey.DetectorRequestId == expectedKey.DetectorRequestId &&
		existingKey.AttemptNo == expectedKey.AttemptNo && existingKey.RequestId == expectedKey.RequestId &&
		existingKey.UpstreamKeyId == expectedKey.UpstreamKeyId && existingKey.UpstreamKeyFingerprint == expectedKey.UpstreamKeyFingerprint &&
		existingKey.UpstreamKeyDisplay == expectedKey.UpstreamKeyDisplay && existingKey.EstimatedQuota == expectedKey.EstimatedQuota &&
		equalChannelModelDetectionInt64Pointer(existingKey.EstimatedCostNanoCNY, expectedKey.EstimatedCostNanoCNY) &&
		equalChannelModelDetectionStringPointer(existingKey.CostRatioCNY, expectedKey.CostRatioCNY) &&
		equalChannelModelDetectionInt64Pointer(existingKey.QuotaPerUnit, expectedKey.QuotaPerUnit) &&
		existingKey.CostScope == expectedKey.CostScope
}

func channelModelDetectionCostEventKeyFromEvent(event model.ChannelModelDetectionCostEvent) channelModelDetectionCostEventKey {
	return channelModelDetectionCostEventKey{
		CostEventId:            event.CostEventId,
		RunId:                  event.RunId,
		TargetId:               event.TargetId,
		ExecutionId:            event.ExecutionId,
		ChannelId:              event.ChannelId,
		RequestModel:           event.RequestModel,
		ClaimedModel:           event.ClaimedModel,
		Preset:                 event.Preset,
		DetectorRequestId:      event.DetectorRequestId,
		AttemptNo:              event.AttemptNo,
		RequestId:              event.RequestId,
		UpstreamKeyId:          event.UpstreamKeyId,
		UpstreamKeyFingerprint: event.UpstreamKeyFingerprint,
		UpstreamKeyDisplay:     event.UpstreamKeyDisplay,
		EstimatedQuota:         event.EstimatedQuota,
		EstimatedCostNanoCNY:   event.EstimatedCostNanoCNY,
		CostRatioCNY:           event.CostRatioCNY,
		QuotaPerUnit:           event.QuotaPerUnit,
		CostScope:              event.CostScope,
	}
}

func MarkChannelModelDetectionCostEventDispatched(ctx context.Context, tx *gorm.DB, costEventId string, now int64) (model.ChannelModelDetectionCostEvent, error) {
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	costEventId = strings.TrimSpace(costEventId)
	if costEventId == "" || len(costEventId) > 64 {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	updated := useDB.Model(&model.ChannelModelDetectionCostEvent{}).
		Where("cost_event_id = ? AND dispatch_state = ? AND settlement_status = ?", costEventId, model.ChannelModelDetectionDispatchPrepared, model.ChannelModelDetectionSettlementPending).
		Updates(map[string]any{
			"dispatch_state":    model.ChannelModelDetectionDispatchDispatched,
			"settlement_status": model.ChannelModelDetectionSettlementPending,
			"updated_at":        now,
		})
	if updated.Error != nil {
		return model.ChannelModelDetectionCostEvent{}, updated.Error
	}
	event, err := getChannelModelDetectionCostEvent(useDB, costEventId)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if event.DispatchState != model.ChannelModelDetectionDispatchDispatched {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	return event, nil
}

func MarkChannelModelDetectionCostEventNotStarted(ctx context.Context, tx *gorm.DB, costEventId string, now int64) (model.ChannelModelDetectionCostEvent, error) {
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	costEventId = strings.TrimSpace(costEventId)
	if costEventId == "" || len(costEventId) > 64 {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	updated := useDB.Model(&model.ChannelModelDetectionCostEvent{}).
		Where("cost_event_id = ? AND dispatch_state = ? AND settlement_status = ?", costEventId, model.ChannelModelDetectionDispatchPrepared, model.ChannelModelDetectionSettlementPending).
		Updates(map[string]any{
			"dispatch_state":    model.ChannelModelDetectionDispatchNotStarted,
			"settlement_status": model.ChannelModelDetectionSettlementNotApplicable,
			"updated_at":        now,
		})
	if updated.Error != nil {
		return model.ChannelModelDetectionCostEvent{}, updated.Error
	}
	event, err := getChannelModelDetectionCostEvent(useDB, costEventId)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if event.DispatchState != model.ChannelModelDetectionDispatchNotStarted || event.SettlementStatus != model.ChannelModelDetectionSettlementNotApplicable {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	return event, nil
}

// SettleChannelModelDetectionCostEvent uses only the snapshot frozen on the
// event. If that snapshot is missing or the amount cannot be represented, the
// already-dispatched request is conservatively moved to unresolved instead of
// being recorded as a zero-cost settlement.
func SettleChannelModelDetectionCostEvent(ctx context.Context, tx *gorm.DB, input ChannelModelDetectionCostSettlementInput) (model.ChannelModelDetectionCostEvent, error) {
	if err := validateChannelModelDetectionUsage(input.SettledQuota, input.CostBasisQuota, input.InputTokens, input.OutputTokens, input.TotalTokens); err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if input.UsageSource == "" {
		input.UsageSource = model.ChannelModelDetectionUsageUpstreamAuthoritative
		input.UsageAvailable = true
	}
	if (input.UsageSource != model.ChannelModelDetectionUsageUpstreamAuthoritative && input.UsageSource != model.ChannelModelDetectionUsageLocalEstimate) ||
		(input.UsageSource == model.ChannelModelDetectionUsageUpstreamAuthoritative) != input.UsageAvailable {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	input.CostEventId = strings.TrimSpace(input.CostEventId)
	input.UpstreamRequestId = strings.TrimSpace(input.UpstreamRequestId)
	if input.CostEventId == "" || len(input.CostEventId) > 64 || len(input.UpstreamRequestId) > 128 {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	event, err := getChannelModelDetectionCostEvent(useDB, input.CostEventId)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if event.DispatchState != model.ChannelModelDetectionDispatchDispatched || event.SettlementStatus == model.ChannelModelDetectionSettlementNotApplicable {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	if event.UpstreamRequestId != "" && input.UpstreamRequestId != "" && event.UpstreamRequestId != input.UpstreamRequestId {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	upstreamRequestId := event.UpstreamRequestId
	if upstreamRequestId == "" {
		upstreamRequestId = input.UpstreamRequestId
	}
	if event.CostRatioCNY == nil || event.QuotaPerUnit == nil {
		settledQuota := input.SettledQuota
		costBasisQuota := input.CostBasisQuota
		return MarkChannelModelDetectionCostEventUnresolved(ctx, useDB, ChannelModelDetectionCostUnresolvedInput{
			CostEventId:           input.CostEventId,
			UsageSource:           input.UsageSource,
			UsageAvailable:        input.UsageAvailable,
			InputTokens:           input.InputTokens,
			OutputTokens:          input.OutputTokens,
			TotalTokens:           input.TotalTokens,
			SettledQuota:          &settledQuota,
			CostBasisQuota:        &costBasisQuota,
			UpstreamRequestId:     upstreamRequestId,
			ErrorCode:             channelModelDetectionCostErrorSnapshotUnavailable,
			SanitizedErrorMessage: "模型检测成本快照不完整",
			UpdatedAt:             input.SettledAt,
		})
	}
	costNanoCNY, err := CalculateChannelModelDetectionSettledCostNanoCNY(input.CostBasisQuota, *event.CostRatioCNY, *event.QuotaPerUnit)
	if err != nil {
		settledQuota := input.SettledQuota
		costBasisQuota := input.CostBasisQuota
		return MarkChannelModelDetectionCostEventUnresolved(ctx, useDB, ChannelModelDetectionCostUnresolvedInput{
			CostEventId:           input.CostEventId,
			UsageSource:           input.UsageSource,
			UsageAvailable:        input.UsageAvailable,
			InputTokens:           input.InputTokens,
			OutputTokens:          input.OutputTokens,
			TotalTokens:           input.TotalTokens,
			SettledQuota:          &settledQuota,
			CostBasisQuota:        &costBasisQuota,
			UpstreamRequestId:     upstreamRequestId,
			ErrorCode:             channelModelDetectionCostErrorConversionFailed,
			SanitizedErrorMessage: "模型检测成本换算失败",
			UpdatedAt:             input.SettledAt,
		})
	}
	if input.SettledAt <= 0 {
		input.SettledAt = common.GetTimestamp()
	}
	if event.SettlementStatus == model.ChannelModelDetectionSettlementSettled {
		if event.UsageSource != input.UsageSource || event.UsageAvailable != input.UsageAvailable ||
			event.SettledQuota == nil || *event.SettledQuota != input.SettledQuota ||
			event.CostBasisQuota == nil || *event.CostBasisQuota != input.CostBasisQuota ||
			event.SettledCostNanoCNY == nil || *event.SettledCostNanoCNY != costNanoCNY ||
			event.InputTokens != input.InputTokens || event.OutputTokens != input.OutputTokens || event.TotalTokens != input.TotalTokens ||
			event.UpstreamRequestId != upstreamRequestId {
			return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
		}
		return event, nil
	}
	if !channelModelDetectionUsageFactsCompatible(event, input.UsageSource, input.UsageAvailable, input.InputTokens, input.OutputTokens, input.TotalTokens, &input.SettledQuota, &input.CostBasisQuota) {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	updated := useDB.Model(&model.ChannelModelDetectionCostEvent{}).
		Where("cost_event_id = ? AND dispatch_state = ? AND settlement_status IN ?", input.CostEventId, model.ChannelModelDetectionDispatchDispatched, []string{model.ChannelModelDetectionSettlementPending, model.ChannelModelDetectionSettlementUnresolved}).
		Updates(map[string]any{
			"settlement_status":     model.ChannelModelDetectionSettlementSettled,
			"usage_source":          input.UsageSource,
			"usage_available":       input.UsageAvailable,
			"input_tokens":          input.InputTokens,
			"output_tokens":         input.OutputTokens,
			"total_tokens":          input.TotalTokens,
			"settled_quota":         input.SettledQuota,
			"cost_basis_quota":      input.CostBasisQuota,
			"settled_cost_nano_cny": costNanoCNY,
			"upstream_request_id":   upstreamRequestId,
			"settled_at":            input.SettledAt,
			"updated_at":            input.SettledAt,
		})
	if updated.Error != nil {
		return model.ChannelModelDetectionCostEvent{}, updated.Error
	}
	if updated.RowsAffected == 0 {
		current, err := getChannelModelDetectionCostEvent(useDB, input.CostEventId)
		if err != nil {
			return model.ChannelModelDetectionCostEvent{}, err
		}
		if current.SettlementStatus == model.ChannelModelDetectionSettlementSettled {
			if current.UsageSource == input.UsageSource && current.UsageAvailable == input.UsageAvailable &&
				current.SettledQuota != nil && *current.SettledQuota == input.SettledQuota &&
				current.CostBasisQuota != nil && *current.CostBasisQuota == input.CostBasisQuota &&
				current.SettledCostNanoCNY != nil && *current.SettledCostNanoCNY == costNanoCNY &&
				current.InputTokens == input.InputTokens && current.OutputTokens == input.OutputTokens && current.TotalTokens == input.TotalTokens &&
				current.UpstreamRequestId == upstreamRequestId {
				return current, nil
			}
		}
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	settled, err := getChannelModelDetectionCostEvent(useDB, input.CostEventId)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if settled.SettlementStatus != model.ChannelModelDetectionSettlementSettled {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	return settled, nil
}

func MarkChannelModelDetectionCostEventUnresolved(ctx context.Context, tx *gorm.DB, input ChannelModelDetectionCostUnresolvedInput) (model.ChannelModelDetectionCostEvent, error) {
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	input.CostEventId = strings.TrimSpace(input.CostEventId)
	input.UpstreamRequestId = strings.TrimSpace(input.UpstreamRequestId)
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	if input.CostEventId == "" || len(input.CostEventId) > 64 || len(input.UpstreamRequestId) > 128 || len(input.ErrorCode) > 128 || len(input.SanitizedErrorMessage) > 512 {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	for _, value := range []*int64{input.SettledQuota, input.CostBasisQuota} {
		if value != nil && *value < 0 {
			return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
		}
	}
	if err := validateChannelModelDetectionUsageTokens(input.InputTokens, input.OutputTokens, input.TotalTokens); err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	event, err := getChannelModelDetectionCostEvent(useDB, input.CostEventId)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if event.DispatchState != model.ChannelModelDetectionDispatchDispatched || event.SettlementStatus == model.ChannelModelDetectionSettlementSettled || event.SettlementStatus == model.ChannelModelDetectionSettlementNotApplicable {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	if input.UsageSource == "" {
		if event.EstimatedQuota > 0 || event.EstimatedCostNanoCNY != nil {
			input.UsageSource = model.ChannelModelDetectionUsageLocalEstimate
		} else {
			input.UsageSource = model.ChannelModelDetectionUsageUnavailable
		}
	}
	if !model.IsChannelModelDetectionUsageSource(input.UsageSource) ||
		(input.UsageSource == model.ChannelModelDetectionUsageUpstreamAuthoritative) != input.UsageAvailable {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	if !input.UsageAvailable && (input.InputTokens != 0 || input.OutputTokens != 0 || input.TotalTokens != 0) {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	if !input.UsageAvailable && (input.SettledQuota != nil || input.CostBasisQuota != nil) {
		return model.ChannelModelDetectionCostEvent{}, model.ErrChannelModelDetectionInvalidCost
	}
	if event.UpstreamRequestId != "" && input.UpstreamRequestId != "" && event.UpstreamRequestId != input.UpstreamRequestId {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	if event.ErrorCode != "" && input.ErrorCode != "" && event.ErrorCode != input.ErrorCode {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	if event.ErrorMessage != "" && input.SanitizedErrorMessage != "" && event.ErrorMessage != input.SanitizedErrorMessage {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	if !channelModelDetectionUsageFactsCompatible(event, input.UsageSource, input.UsageAvailable, input.InputTokens, input.OutputTokens, input.TotalTokens, input.SettledQuota, input.CostBasisQuota) {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	upstreamRequestId := event.UpstreamRequestId
	if upstreamRequestId == "" {
		upstreamRequestId = input.UpstreamRequestId
	}
	errorCode := event.ErrorCode
	if errorCode == "" {
		errorCode = input.ErrorCode
	}
	errorMessage := event.ErrorMessage
	if errorMessage == "" {
		errorMessage = input.SanitizedErrorMessage
	}
	if input.UpdatedAt <= 0 {
		input.UpdatedAt = common.GetTimestamp()
	}
	values := map[string]any{
		"settlement_status":   model.ChannelModelDetectionSettlementUnresolved,
		"usage_source":        input.UsageSource,
		"usage_available":     input.UsageAvailable,
		"input_tokens":        input.InputTokens,
		"output_tokens":       input.OutputTokens,
		"total_tokens":        input.TotalTokens,
		"upstream_request_id": upstreamRequestId,
		"error_code":          errorCode,
		"error_message":       errorMessage,
		"updated_at":          input.UpdatedAt,
	}
	if input.SettledQuota != nil {
		values["settled_quota"] = *input.SettledQuota
	}
	if input.CostBasisQuota != nil {
		values["cost_basis_quota"] = *input.CostBasisQuota
	}
	updated := useDB.Model(&model.ChannelModelDetectionCostEvent{}).
		Where("cost_event_id = ? AND dispatch_state = ? AND settlement_status IN ?", input.CostEventId, model.ChannelModelDetectionDispatchDispatched, []string{model.ChannelModelDetectionSettlementPending, model.ChannelModelDetectionSettlementUnresolved}).
		Updates(values)
	if updated.Error != nil {
		return model.ChannelModelDetectionCostEvent{}, updated.Error
	}
	if updated.RowsAffected == 0 {
		current, err := getChannelModelDetectionCostEvent(useDB, input.CostEventId)
		if err != nil {
			return model.ChannelModelDetectionCostEvent{}, err
		}
		if current.SettlementStatus == model.ChannelModelDetectionSettlementUnresolved &&
			channelModelDetectionUsageFactsCompatible(current, input.UsageSource, input.UsageAvailable, input.InputTokens, input.OutputTokens, input.TotalTokens, input.SettledQuota, input.CostBasisQuota) &&
			current.UpstreamRequestId == upstreamRequestId && current.ErrorCode == errorCode && current.ErrorMessage == errorMessage {
			return current, nil
		}
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	unresolved, err := getChannelModelDetectionCostEvent(useDB, input.CostEventId)
	if err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	if unresolved.SettlementStatus != model.ChannelModelDetectionSettlementUnresolved {
		return model.ChannelModelDetectionCostEvent{}, ErrChannelModelDetectionCostConflict
	}
	return unresolved, nil
}

func channelModelDetectionUsageFactsCompatible(event model.ChannelModelDetectionCostEvent, source string, available bool, inputTokens, outputTokens, totalTokens int64, settledQuota, costBasisQuota *int64) bool {
	if event.SettlementStatus == model.ChannelModelDetectionSettlementPending {
		return true
	}
	if event.UsageSource == source && event.UsageAvailable == available && event.InputTokens == inputTokens && event.OutputTokens == outputTokens && event.TotalTokens == totalTokens &&
		equalChannelModelDetectionInt64Pointer(event.SettledQuota, settledQuota) && equalChannelModelDetectionInt64Pointer(event.CostBasisQuota, costBasisQuota) {
		return true
	}
	return event.SettlementStatus == model.ChannelModelDetectionSettlementUnresolved && !event.UsageAvailable && available &&
		event.InputTokens == 0 && event.OutputTokens == 0 && event.TotalTokens == 0 &&
		(event.SettledQuota == nil || equalChannelModelDetectionInt64Pointer(event.SettledQuota, settledQuota)) &&
		(event.CostBasisQuota == nil || equalChannelModelDetectionInt64Pointer(event.CostBasisQuota, costBasisQuota))
}

func validateChannelModelDetectionUsage(settledQuota, costBasisQuota, inputTokens, outputTokens, totalTokens int64) error {
	if settledQuota < 0 || costBasisQuota < 0 {
		return model.ErrChannelModelDetectionInvalidCost
	}
	return validateChannelModelDetectionUsageTokens(inputTokens, outputTokens, totalTokens)
}

func validateChannelModelDetectionUsageTokens(inputTokens, outputTokens, totalTokens int64) error {
	if inputTokens < 0 || outputTokens < 0 || totalTokens < 0 || inputTokens > totalTokens || outputTokens > totalTokens || inputTokens > math.MaxInt64-outputTokens || inputTokens+outputTokens > totalTokens {
		return model.ErrChannelModelDetectionInvalidCost
	}
	return nil
}

func AggregateChannelModelDetectionCostEvents(ctx context.Context, tx *gorm.DB, filter ChannelModelDetectionCostFilter) (ChannelModelDetectionCostAggregate, error) {
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	query := useDB.Model(&model.ChannelModelDetectionCostEvent{})
	filtered := false
	if filter.RunId != "" {
		query = query.Where("run_id = ?", filter.RunId)
		filtered = true
	}
	if len(filter.RunIds) > 0 {
		query = query.Where("run_id IN ?", filter.RunIds)
		filtered = true
	}
	if filter.ExecutionId > 0 {
		query = query.Where("execution_id = ?", filter.ExecutionId)
		filtered = true
	}
	if filter.TargetId > 0 {
		query = query.Where("target_id = ?", filter.TargetId)
		filtered = true
	}
	if filter.ChannelId > 0 {
		query = query.Where("channel_id = ?", filter.ChannelId)
		filtered = true
	}
	if !filtered {
		return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
	}
	var events []model.ChannelModelDetectionCostEvent
	if err := query.Order("id ASC").Find(&events).Error; err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	return aggregateChannelModelDetectionCostEventList(events)
}

func aggregateChannelModelDetectionCostEventList(events []model.ChannelModelDetectionCostEvent) (ChannelModelDetectionCostAggregate, error) {
	result := ChannelModelDetectionCostAggregate{Status: ChannelModelDetectionCostStatusNotStarted}
	estimatedKnownCount := int64(0)
	unresolvedKnownCount := int64(0)
	dispatchedCount := int64(0)
	dispatchedUsageCount := int64(0)
	for i := range events {
		event := &events[i]
		if err := event.Validate(); err != nil {
			return ChannelModelDetectionCostAggregate{}, err
		}
		if event.UsageAvailable {
			for target, value := range map[*int64]int64{
				&result.InputTokens:  event.InputTokens,
				&result.OutputTokens: event.OutputTokens,
				&result.TotalTokens:  event.TotalTokens,
			} {
				if err := addChannelModelDetectionCostValue(target, value); err != nil {
					return ChannelModelDetectionCostAggregate{}, err
				}
			}
		}
		switch event.DispatchState {
		case model.ChannelModelDetectionDispatchPrepared:
			if err := addChannelModelDetectionCostEstimate(&result, event, &estimatedKnownCount); err != nil {
				return ChannelModelDetectionCostAggregate{}, err
			}
			result.PendingRequestCount++
			continue
		case model.ChannelModelDetectionDispatchNotStarted:
			result.NotStartedRequestCount++
			continue
		case model.ChannelModelDetectionDispatchDispatched:
			if err := addChannelModelDetectionCostEstimate(&result, event, &estimatedKnownCount); err != nil {
				return ChannelModelDetectionCostAggregate{}, err
			}
			dispatchedCount++
			if event.UsageAvailable {
				dispatchedUsageCount++
			}
		default:
			return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
		}
		switch event.SettlementStatus {
		case model.ChannelModelDetectionSettlementPending:
			result.PendingRequestCount++
		case model.ChannelModelDetectionSettlementSettled:
			if event.SettledQuota == nil || event.CostBasisQuota == nil || event.SettledCostNanoCNY == nil {
				return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
			}
			for target, value := range map[*int64]int64{
				&result.SettledQuota:   *event.SettledQuota,
				&result.CostBasisQuota: *event.CostBasisQuota,
			} {
				if err := addChannelModelDetectionCostValue(target, value); err != nil {
					return ChannelModelDetectionCostAggregate{}, err
				}
			}
			if result.SettledCostNanoCNY == nil {
				zero := int64(0)
				result.SettledCostNanoCNY = &zero
			}
			if err := addChannelModelDetectionCostValue(result.SettledCostNanoCNY, *event.SettledCostNanoCNY); err != nil {
				return ChannelModelDetectionCostAggregate{}, err
			}
			result.SettledRequestCount++
		case model.ChannelModelDetectionSettlementUnresolved:
			result.UnresolvedRequestCount++
			if event.EstimatedCostNanoCNY == nil {
				result.UnresolvedCostUnknownCount++
				continue
			}
			if result.UnresolvedCostNanoCNY == nil {
				zero := int64(0)
				result.UnresolvedCostNanoCNY = &zero
			}
			if err := addChannelModelDetectionCostValue(result.UnresolvedCostNanoCNY, *event.EstimatedCostNanoCNY); err != nil {
				return ChannelModelDetectionCostAggregate{}, err
			}
			unresolvedKnownCount++
		default:
			return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
		}
	}
	if estimatedKnownCount == 0 {
		if result.CostEstimateUnknownCount > 0 {
			result.EstimatedCostNanoCNY = nil
		} else {
			zero := int64(0)
			result.EstimatedCostNanoCNY = &zero
		}
	}
	if result.SettledRequestCount == 0 {
		zero := int64(0)
		result.SettledCostNanoCNY = &zero
	}
	if result.UnresolvedRequestCount == 0 {
		zero := int64(0)
		result.UnresolvedCostNanoCNY = &zero
	} else if unresolvedKnownCount == 0 {
		result.UnresolvedCostNanoCNY = nil
	}
	result.UsageAvailable = dispatchedCount > 0 && dispatchedUsageCount == dispatchedCount
	switch {
	case result.PendingRequestCount > 0:
		result.Status = ChannelModelDetectionCostStatusPending
	case dispatchedCount == 0:
		result.Status = ChannelModelDetectionCostStatusNotStarted
	case result.SettledRequestCount == dispatchedCount:
		result.Status = ChannelModelDetectionCostStatusSettled
	case result.UnresolvedRequestCount == dispatchedCount:
		result.Status = ChannelModelDetectionCostStatusUnresolved
	default:
		result.Status = ChannelModelDetectionCostStatusPartial
	}
	return result, nil
}

func addChannelModelDetectionCostEstimate(result *ChannelModelDetectionCostAggregate, event *model.ChannelModelDetectionCostEvent, knownCount *int64) error {
	if err := addChannelModelDetectionCostValue(&result.EstimatedQuota, event.EstimatedQuota); err != nil {
		return err
	}
	if event.EstimatedCostNanoCNY == nil {
		result.CostEstimateUnknownCount++
		return nil
	}
	if result.EstimatedCostNanoCNY == nil {
		zero := int64(0)
		result.EstimatedCostNanoCNY = &zero
	}
	if err := addChannelModelDetectionCostValue(result.EstimatedCostNanoCNY, *event.EstimatedCostNanoCNY); err != nil {
		return err
	}
	*knownCount++
	return nil
}

func RebuildChannelModelDetectionExecutionCost(ctx context.Context, tx *gorm.DB, executionId int64) (ChannelModelDetectionCostAggregate, error) {
	if executionId <= 0 {
		return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
	}
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	aggregate, err := AggregateChannelModelDetectionCostEvents(ctx, useDB, ChannelModelDetectionCostFilter{ExecutionId: executionId})
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	updated := useDB.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", executionId).Updates(channelModelDetectionExecutionCostValues(aggregate))
	if updated.Error != nil {
		return ChannelModelDetectionCostAggregate{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return ChannelModelDetectionCostAggregate{}, gorm.ErrRecordNotFound
	}
	return aggregate, nil
}

func RebuildChannelModelDetectionRunCost(ctx context.Context, tx *gorm.DB, runId string) (ChannelModelDetectionCostAggregate, error) {
	runId = strings.TrimSpace(runId)
	if runId == "" || len(runId) > 64 {
		return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
	}
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	aggregate, err := AggregateChannelModelDetectionCostEvents(ctx, useDB, ChannelModelDetectionCostFilter{RunId: runId})
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	updated := useDB.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", runId).Updates(channelModelDetectionRunCostValues(aggregate))
	if updated.Error != nil {
		return ChannelModelDetectionCostAggregate{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return ChannelModelDetectionCostAggregate{}, gorm.ErrRecordNotFound
	}
	return aggregate, nil
}

func RebuildChannelModelDetectionBatchCost(ctx context.Context, tx *gorm.DB, batchId string) (ChannelModelDetectionCostAggregate, error) {
	batchId = strings.TrimSpace(batchId)
	if batchId == "" || len(batchId) > 64 {
		return ChannelModelDetectionCostAggregate{}, model.ErrChannelModelDetectionInvalidCost
	}
	useDB, err := channelModelDetectionCostDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	var runIds []string
	if err := useDB.Model(&model.ChannelModelDetectionRun{}).Where("batch_id = ?", batchId).Pluck("run_id", &runIds).Error; err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	aggregate := ChannelModelDetectionCostAggregate{Status: ChannelModelDetectionCostStatusNotStarted}
	zero := int64(0)
	aggregate.EstimatedCostNanoCNY = &zero
	aggregate.SettledCostNanoCNY = &zero
	aggregate.UnresolvedCostNanoCNY = &zero
	if len(runIds) > 0 {
		aggregate, err = AggregateChannelModelDetectionCostEvents(ctx, useDB, ChannelModelDetectionCostFilter{RunIds: runIds})
		if err != nil {
			return ChannelModelDetectionCostAggregate{}, err
		}
	}
	updated := useDB.Model(&model.ChannelModelDetectionBatch{}).Where("batch_id = ?", batchId).Updates(channelModelDetectionRunCostValues(aggregate))
	if updated.Error != nil {
		return ChannelModelDetectionCostAggregate{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return ChannelModelDetectionCostAggregate{}, gorm.ErrRecordNotFound
	}
	return aggregate, nil
}

func channelModelDetectionExecutionCostValues(aggregate ChannelModelDetectionCostAggregate) map[string]any {
	values := channelModelDetectionRunCostValues(aggregate)
	values["input_tokens"] = aggregate.InputTokens
	values["output_tokens"] = aggregate.OutputTokens
	values["total_tokens"] = aggregate.TotalTokens
	values["usage_available"] = aggregate.UsageAvailable
	return values
}

func channelModelDetectionRunCostValues(aggregate ChannelModelDetectionCostAggregate) map[string]any {
	return map[string]any{
		"estimated_quota":               aggregate.EstimatedQuota,
		"estimated_cost_nano_cny":       aggregate.EstimatedCostNanoCNY,
		"cost_estimate_unknown_count":   aggregate.CostEstimateUnknownCount,
		"settled_quota":                 aggregate.SettledQuota,
		"cost_basis_quota":              aggregate.CostBasisQuota,
		"settled_cost_nano_cny":         aggregate.SettledCostNanoCNY,
		"unresolved_cost_nano_cny":      aggregate.UnresolvedCostNanoCNY,
		"unresolved_cost_unknown_count": aggregate.UnresolvedCostUnknownCount,
		"settled_request_count":         aggregate.SettledRequestCount,
		"unresolved_request_count":      aggregate.UnresolvedRequestCount,
		"updated_at":                    common.GetTimestamp(),
	}
}

func addChannelModelDetectionCostValue(total *int64, value int64) error {
	if total == nil || value < 0 || *total < 0 {
		return model.ErrChannelModelDetectionInvalidCost
	}
	if value > math.MaxInt64-*total {
		return ErrChannelModelDetectionCostOverflow
	}
	*total += value
	return nil
}

func equalChannelModelDetectionInt64Pointer(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func equalChannelModelDetectionStringPointer(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func channelModelDetectionCostDB(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	if tx == nil {
		tx = model.DB
	}
	if tx == nil {
		return nil, errors.New("模型检测成本数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return tx.WithContext(ctx), nil
}

func getChannelModelDetectionCostEvent(tx *gorm.DB, costEventId string) (model.ChannelModelDetectionCostEvent, error) {
	var event model.ChannelModelDetectionCostEvent
	if err := tx.Where("cost_event_id = ?", costEventId).First(&event).Error; err != nil {
		return model.ChannelModelDetectionCostEvent{}, err
	}
	return event, nil
}

func FormatChannelModelDetectionCostCNY(costNanoCNY *int64) *string {
	if costNanoCNY == nil {
		return nil
	}
	if *costNanoCNY < 0 {
		return nil
	}
	formatted := decimal.NewFromInt(*costNanoCNY).Div(decimal.NewFromInt(model.ChannelModelDetectionNanoPerCNY)).StringFixed(9)
	return &formatted
}

func ValidateChannelModelDetectionCostAggregate(aggregate ChannelModelDetectionCostAggregate) error {
	for _, value := range []int64{
		aggregate.EstimatedQuota, aggregate.CostEstimateUnknownCount, aggregate.SettledQuota,
		aggregate.CostBasisQuota, aggregate.UnresolvedCostUnknownCount, aggregate.SettledRequestCount,
		aggregate.UnresolvedRequestCount, aggregate.PendingRequestCount, aggregate.NotStartedRequestCount,
		aggregate.InputTokens, aggregate.OutputTokens, aggregate.TotalTokens,
	} {
		if value < 0 {
			return model.ErrChannelModelDetectionInvalidCost
		}
	}
	for _, value := range []*int64{aggregate.EstimatedCostNanoCNY, aggregate.SettledCostNanoCNY, aggregate.UnresolvedCostNanoCNY} {
		if value != nil && *value < 0 {
			return model.ErrChannelModelDetectionInvalidCost
		}
	}
	switch aggregate.Status {
	case ChannelModelDetectionCostStatusPending, ChannelModelDetectionCostStatusNotStarted, ChannelModelDetectionCostStatusSettled, ChannelModelDetectionCostStatusUnresolved, ChannelModelDetectionCostStatusPartial:
		return nil
	default:
		return fmt.Errorf("%w: 成本汇总状态无效", model.ErrChannelModelDetectionInvalidCost)
	}
}
