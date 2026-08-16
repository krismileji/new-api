package service

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	channelDailyCostSnapshotContextKey = "channel_daily_cost_snapshot"
	channelDailyCostAttemptContextKey  = "channel_daily_cost_attempt"
	channelDailyCostSnapshotTTL        = time.Minute
)

type channelDailyCostSnapshot struct {
	ChannelId      int
	CostRatioCNY   float64
	QuotaPerUnit   float64
	Configured     bool
	APIKeyId       int
	APIKeyName     string
	KeyFingerprint string
	KeyDisplay     string
}

type channelDailyCostSnapshotCacheEntry struct {
	Snapshot  channelDailyCostSnapshot
	ExpiresAt time.Time
	Version   uint64
}

type channelDailyCostAttemptState struct {
	mu                 sync.Mutex
	ChannelId          int
	Dispatched         bool
	Recording          bool
	Recorded           bool
	SettledCostNanoCNY *int64
}

var (
	channelDailyCostSnapshotCache    sync.Map
	channelDailyCostSnapshotVersions sync.Map
	channelDailyCostSnapshotLoads    singleflight.Group
)

// CaptureChannelDailyCostSnapshot freezes the channel's upstream cost ratio
// before an upstream request starts. Settlement uses this snapshot even if an
// administrator updates the ratio while the request is in flight.
func CaptureChannelDailyCostSnapshot(ctx *gin.Context, channelId int) {
	snapshot, err := getChannelDailyCostSnapshot(channelId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("读取渠道 #%d 成本配置失败: %s", channelId, err.Error()))
	}
	snapshot = channelDailyCostSnapshotWithCurrentKey(ctx, snapshot)
	ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
}

// BeginChannelDailyCostAttempt resets request-local accounting state for one
// selected channel attempt. Local setup failures remain unrecorded until the
// transport marks the attempt as dispatched.
func BeginChannelDailyCostAttempt(ctx *gin.Context, channelId int) {
	if ctx == nil || channelId <= 0 {
		return
	}
	ctx.Set(channelDailyCostAttemptContextKey, &channelDailyCostAttemptState{ChannelId: channelId})
}

// MarkChannelDailyCostRequestDispatched marks the exact boundary where a
// request enters an upstream HTTP, WebSocket, or provider SDK transport.
func MarkChannelDailyCostRequestDispatched(ctx *gin.Context) {
	if ctx == nil {
		return
	}
	logChannelModelDetectionDispatchError(ctx, markChannelModelDetectionRequestDispatched(ctx))
	value, exists := ctx.Get(channelDailyCostAttemptContextKey)
	if !exists {
		return
	}
	state, ok := value.(*channelDailyCostAttemptState)
	if !ok || state == nil {
		return
	}
	state.mu.Lock()
	state.Dispatched = true
	state.mu.Unlock()
	if ctx.GetBool("channel_test") {
		ctx.Set("channel_test_request_dispatched", true)
	}
}

// WasChannelDailyCostRequestDispatched reports whether the current channel
// attempt crossed the upstream transport boundary. Task submission uses this
// to distinguish a safe setup failure from an uncertain transport failure:
// once dispatched, retrying may create a duplicate asynchronous task.
func WasChannelDailyCostRequestDispatched(ctx *gin.Context) bool {
	if ctx == nil {
		return false
	}
	value, exists := ctx.Get(channelDailyCostAttemptContextKey)
	if !exists {
		return false
	}
	state, ok := value.(*channelDailyCostAttemptState)
	if !ok || state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.Dispatched
}

// FinalizeChannelDailyCostAttempt records one unresolved event when an
// upstream attempt was dispatched but no settlement event classified it.
func FinalizeChannelDailyCostAttempt(ctx *gin.Context, channelId int, requestDispatched bool) {
	if ctx == nil || channelId <= 0 {
		return
	}
	value, exists := ctx.Get(channelDailyCostAttemptContextKey)
	if !exists {
		return
	}
	state, ok := value.(*channelDailyCostAttemptState)
	if !ok || state == nil || state.ChannelId != channelId {
		return
	}

	state.mu.Lock()
	if requestDispatched {
		state.Dispatched = true
	}
	if !state.Dispatched || state.Recording || state.Recorded {
		state.mu.Unlock()
		return
	}
	state.Recording = true
	state.mu.Unlock()
	recordChannelDailyCostUnresolved(ctx, channelId)
	state.mu.Lock()
	state.Recording = false
	state.mu.Unlock()
}

func markChannelDailyCostAttemptRecorded(ctx *gin.Context, channelId int) {
	if ctx == nil {
		return
	}
	value, exists := ctx.Get(channelDailyCostAttemptContextKey)
	if !exists {
		return
	}
	state, ok := value.(*channelDailyCostAttemptState)
	if !ok || state == nil || state.ChannelId != channelId {
		return
	}
	state.mu.Lock()
	state.Recorded = true
	state.mu.Unlock()
}

func setChannelDailyCostAttemptSettledCost(ctx *gin.Context, channelId int, costNanoCNY int64) {
	if ctx == nil {
		return
	}
	value, exists := ctx.Get(channelDailyCostAttemptContextKey)
	if !exists {
		return
	}
	state, ok := value.(*channelDailyCostAttemptState)
	if !ok || state == nil || state.ChannelId != channelId {
		return
	}
	state.mu.Lock()
	settledCostNanoCNY := costNanoCNY
	state.SettledCostNanoCNY = &settledCostNanoCNY
	state.mu.Unlock()
}

func ChannelDailyCostAttemptSettledCost(ctx *gin.Context, channelId int) *int64 {
	if ctx == nil {
		return nil
	}
	value, exists := ctx.Get(channelDailyCostAttemptContextKey)
	if !exists {
		return nil
	}
	state, ok := value.(*channelDailyCostAttemptState)
	if !ok || state == nil || state.ChannelId != channelId {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.SettledCostNanoCNY == nil {
		return nil
	}
	settledCostNanoCNY := *state.SettledCostNanoCNY
	return &settledCostNanoCNY
}

func InvalidateChannelDailyCostSnapshot(channelId int) {
	channelDailyCostSnapshotVersion(channelId).Add(1)
	channelDailyCostSnapshotCache.Delete(channelId)
	channelDailyCostSnapshotLoads.Forget(strconv.Itoa(channelId))
}

func ResetChannelDailyCostSnapshotCache() {
	channelDailyCostSnapshotVersions.Range(func(key any, value any) bool {
		value.(*atomic.Uint64).Add(1)
		channelDailyCostSnapshotLoads.Forget(strconv.Itoa(key.(int)))
		return true
	})
	channelDailyCostSnapshotCache.Range(func(key any, _ any) bool {
		channelDailyCostSnapshotCache.Delete(key)
		return true
	})
}

func getChannelDailyCostSnapshot(channelId int) (channelDailyCostSnapshot, error) {
	version := channelDailyCostSnapshotVersion(channelId)
	if snapshot, ok := loadChannelDailyCostSnapshotFromCache(channelId, version.Load()); ok {
		return snapshot, nil
	}
	value, err, _ := channelDailyCostSnapshotLoads.Do(strconv.Itoa(channelId), func() (any, error) {
		loadVersion := version.Load()
		if snapshot, ok := loadChannelDailyCostSnapshotFromCache(channelId, loadVersion); ok {
			return snapshot, nil
		}
		snapshot := channelDailyCostSnapshot{
			ChannelId:    channelId,
			QuotaPerUnit: common.QuotaPerUnit,
		}
		monitor, err := model.GetChannelRatioMonitor(channelId)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			storeChannelDailyCostSnapshot(channelId, version, loadVersion, snapshot)
			return snapshot, nil
		}
		if err != nil {
			return snapshot, err
		}
		if monitor.UpdatedTime <= 0 {
			storeChannelDailyCostSnapshot(channelId, version, loadVersion, snapshot)
			return snapshot, nil
		}

		conversion, err := ParseChannelMonitorCostConversion(monitor.CostConversion)
		if err != nil {
			return snapshot, err
		}
		if math.IsNaN(snapshot.QuotaPerUnit) || math.IsInf(snapshot.QuotaPerUnit, 0) || snapshot.QuotaPerUnit <= 0 {
			return snapshot, errors.New("额度单位配置无效")
		}
		costRatio, _, err := CalculateChannelMonitorCostRatio(monitor.Ratio, conversion)
		if err != nil {
			return snapshot, err
		}
		snapshot.CostRatioCNY = costRatio
		snapshot.Configured = true
		storeChannelDailyCostSnapshot(channelId, version, loadVersion, snapshot)
		return snapshot, nil
	})
	if err != nil {
		return channelDailyCostSnapshot{
			ChannelId:    channelId,
			QuotaPerUnit: common.QuotaPerUnit,
		}, err
	}
	snapshot, ok := value.(channelDailyCostSnapshot)
	if !ok {
		return channelDailyCostSnapshot{
			ChannelId:    channelId,
			QuotaPerUnit: common.QuotaPerUnit,
		}, errors.New("渠道成本快照结果无效")
	}
	return snapshot, nil
}

func channelDailyCostSnapshotVersion(channelId int) *atomic.Uint64 {
	value, _ := channelDailyCostSnapshotVersions.LoadOrStore(channelId, &atomic.Uint64{})
	return value.(*atomic.Uint64)
}

func loadChannelDailyCostSnapshotFromCache(channelId int, version uint64) (channelDailyCostSnapshot, bool) {
	cached, ok := channelDailyCostSnapshotCache.Load(channelId)
	if !ok {
		return channelDailyCostSnapshot{}, false
	}
	entry := cached.(channelDailyCostSnapshotCacheEntry)
	if entry.Version != version || !time.Now().Before(entry.ExpiresAt) {
		return channelDailyCostSnapshot{}, false
	}
	snapshot := entry.Snapshot
	snapshot.QuotaPerUnit = common.QuotaPerUnit
	if math.IsNaN(snapshot.QuotaPerUnit) || math.IsInf(snapshot.QuotaPerUnit, 0) || snapshot.QuotaPerUnit <= 0 {
		return channelDailyCostSnapshot{}, false
	}
	return snapshot, true
}

func storeChannelDailyCostSnapshot(channelId int, version *atomic.Uint64, loadVersion uint64, snapshot channelDailyCostSnapshot) {
	if version.Load() != loadVersion {
		return
	}
	channelDailyCostSnapshotCache.Store(channelId, channelDailyCostSnapshotCacheEntry{
		Snapshot:  snapshot,
		ExpiresAt: time.Now().Add(channelDailyCostSnapshotTTL),
		Version:   loadVersion,
	})
}

func channelDailyCostSnapshotFromContext(ctx *gin.Context, channelId int) channelDailyCostSnapshot {
	if ctx != nil {
		if value, exists := ctx.Get(channelDailyCostSnapshotContextKey); exists {
			if snapshot, ok := value.(channelDailyCostSnapshot); ok && snapshot.ChannelId == channelId {
				snapshot = channelDailyCostSnapshotWithCurrentKey(ctx, snapshot)
				ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
				return snapshot
			}
		}
	}
	snapshot, err := getChannelDailyCostSnapshot(channelId)
	snapshot = channelDailyCostSnapshotWithCurrentKey(ctx, snapshot)
	if err != nil {
		if ctx != nil {
			logger.LogWarn(ctx, fmt.Sprintf("读取渠道 #%d 成本配置失败: %s", channelId, err.Error()))
		}
		return snapshot
	}
	if ctx != nil {
		ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
	}
	return snapshot
}

func channelDailyCostSnapshotWithCurrentKey(ctx *gin.Context, snapshot channelDailyCostSnapshot) channelDailyCostSnapshot {
	if ctx == nil {
		return snapshot
	}
	snapshot.APIKeyId = ctx.GetInt("token_id")
	snapshot.APIKeyName = strings.TrimSpace(ctx.GetString("token_name"))
	value, exists := common.GetContextKey(ctx, constant.ContextKeyChannelKey)
	if !exists {
		snapshot.KeyFingerprint, snapshot.KeyDisplay = model.ChannelDailyCostAPIKeyIdentityForToken(snapshot.APIKeyId, "")
		return snapshot
	}
	key, ok := value.(string)
	if !ok {
		snapshot.KeyFingerprint, snapshot.KeyDisplay = model.ChannelDailyCostAPIKeyIdentityForToken(snapshot.APIKeyId, "")
		return snapshot
	}
	snapshot.KeyFingerprint, snapshot.KeyDisplay = model.ChannelDailyCostAPIKeyIdentityForToken(snapshot.APIKeyId, key)
	return snapshot
}

// recordChannelDailyCostFromQuota records a successful upstream settlement.
// quotaBeforeGroup must exclude the local user/group multiplier.
func recordChannelDailyCostFromQuota(ctx *gin.Context, channelId int, quotaBeforeGroup float64) {
	snapshot := channelDailyCostSnapshotFromContext(ctx, channelId)
	recordChannelDailyCostWithSnapshot(ctx, snapshot, quotaBeforeGroup)
}

func recordChannelDailyCostUnresolved(ctx *gin.Context, channelId int) {
	snapshot := channelDailyCostSnapshotFromContext(ctx, channelId)
	recordChannelDailyCostEvent(ctx, snapshot, 0, 0, 1)
}

func channelDailyCostUsageIsAuthoritative(ctx *gin.Context, usage *dto.Usage) bool {
	if usage == nil {
		return false
	}
	if ctx != nil && common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens) {
		return false
	}
	return usage.BillingUsage == nil || !usage.BillingUsage.Estimated
}

func recordChannelDailyCostWithSnapshot(ctx *gin.Context, snapshot channelDailyCostSnapshot, quotaBeforeGroup float64) {
	snapshot = channelDailyCostSnapshotWithCurrentKey(ctx, snapshot)
	costNanoCNY, ok := calculateChannelDailyCost(snapshot, quotaBeforeGroup)
	if !ok {
		recordChannelDailyCostEvent(ctx, snapshot, 0, 0, 1)
		return
	}
	recordChannelDailyCostEvent(ctx, snapshot, costNanoCNY, 1, 0)
}

func calculateChannelDailyCost(snapshot channelDailyCostSnapshot, quotaBeforeGroup float64) (int64, bool) {
	if !snapshot.Configured || math.IsNaN(snapshot.QuotaPerUnit) || math.IsInf(snapshot.QuotaPerUnit, 0) || snapshot.QuotaPerUnit <= 0 ||
		math.IsNaN(quotaBeforeGroup) || math.IsInf(quotaBeforeGroup, 0) || quotaBeforeGroup < 0 {
		return 0, false
	}
	costNano := decimal.NewFromFloat(quotaBeforeGroup).
		Div(decimal.NewFromFloat(snapshot.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(snapshot.CostRatioCNY)).
		Mul(decimal.NewFromInt(model.ChannelDailyCostNanoPerCNY)).
		Round(0)
	if costNano.IsNegative() || costNano.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, false
	}
	return costNano.IntPart(), true
}

func recordChannelDailyCostEvent(ctx *gin.Context, snapshot channelDailyCostSnapshot, costNanoCNY int64, settledDelta int64, unresolvedDelta int64) bool {
	if snapshot.ChannelId <= 0 {
		return false
	}
	isStatusProbe := ctx != nil && ctx.GetBool(model.ChannelMonitorStatusProbeLogKey)
	probeCostNanoCNY := int64(0)
	if isStatusProbe && settledDelta > 0 {
		probeCostNanoCNY = costNanoCNY
	}
	delta := model.ChannelDailyCostDelta{
		ChannelId:        snapshot.ChannelId,
		OccurredAt:       common.GetTimestamp(),
		CostNanoCNY:      costNanoCNY,
		ProbeCostNanoCNY: probeCostNanoCNY,
		SettledDelta:     settledDelta,
		UnresolvedDelta:  unresolvedDelta,
		APIKeyId:         snapshot.APIKeyId,
		APIKeyName:       snapshot.APIKeyName,
		KeyFingerprint:   snapshot.KeyFingerprint,
		KeyDisplay:       snapshot.KeyDisplay,
	}
	var persisted bool
	if isStatusProbe {
		persisted = writeChannelDailyCostSynchronously(delta)
	} else {
		persisted = enqueueChannelDailyCost(delta)
	}
	if !persisted {
		logger.LogError(ctx, fmt.Sprintf("记录渠道 #%d 每日成本失败，本次请求未标记为已记录", snapshot.ChannelId))
		return false
	}
	if settledDelta > 0 {
		setChannelDailyCostAttemptSettledCost(ctx, snapshot.ChannelId, costNanoCNY)
	}
	markChannelDailyCostAttemptRecorded(ctx, snapshot.ChannelId)
	return true
}

func recordTextChannelDailyCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, billingUsage *dto.Usage, originUsage *dto.Usage, summary textQuotaSummary, tieredBillingApplied bool, tieredResult *billingexpr.TieredResult) {
	if relayInfo == nil {
		return
	}
	snapshot := channelDailyCostSnapshotFromContext(ctx, relayInfo.ChannelId)
	quotaPerUnit := snapshot.QuotaPerUnit
	if tieredBillingApplied {
		if !channelDailyCostUsageIsAuthoritative(ctx, originUsage) {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		if tieredResult == nil {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		if tieredSnapshot := relayInfo.TieredBillingSnapshot; tieredSnapshot != nil {
			quotaPerUnit = tieredSnapshot.QuotaPerUnit
			snapshot.QuotaPerUnit = quotaPerUnit
		}
		quotaBeforeGroup := tieredResult.ActualQuotaBeforeGroup
		copiedInfo := *relayInfo
		copiedInfo.PriceData = relayInfo.PriceData
		copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
		copiedInfo.QuotaClamp = nil
		baseSummary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, &copiedInfo, billingUsage, quotaPerUnit)
		quotaBeforeGroup += baseSummary.ToolCallSurchargeQuota.InexactFloat64()
		recordChannelDailyCostWithSnapshot(ctx, snapshot, quotaBeforeGroup)
		return
	}
	if !relayInfo.PriceData.UsePrice && !channelDailyCostUsageIsAuthoritative(ctx, originUsage) {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if !relayInfo.PriceData.UsePrice && (billingUsage == nil || !summary.hasBillableUsage()) {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}

	copiedInfo := *relayInfo
	copiedInfo.PriceData = relayInfo.PriceData
	copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
	copiedInfo.QuotaClamp = nil
	baseSummary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, &copiedInfo, billingUsage, quotaPerUnit)
	if copiedInfo.QuotaClamp != nil {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	recordChannelDailyCostWithSnapshot(ctx, snapshot, float64(baseSummary.Quota))
}

func recordAudioChannelDailyCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quotaInfo QuotaInfo, totalTokens int, authoritativeUsage bool, tieredBillingApplied bool, tieredResult *billingexpr.TieredResult) {
	if relayInfo == nil {
		return
	}
	snapshot := channelDailyCostSnapshotFromContext(ctx, relayInfo.ChannelId)
	if tieredBillingApplied {
		if !authoritativeUsage {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		if tieredResult == nil {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		if tieredSnapshot := relayInfo.TieredBillingSnapshot; tieredSnapshot != nil {
			snapshot.QuotaPerUnit = tieredSnapshot.QuotaPerUnit
		}
		recordChannelDailyCostWithSnapshot(ctx, snapshot, tieredResult.ActualQuotaBeforeGroup)
		return
	}
	if !quotaInfo.UsePrice && !authoritativeUsage {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if !quotaInfo.UsePrice && totalTokens <= 0 {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	quotaInfo.GroupRatio = 1
	var quotaBeforeGroup int
	var clamp *common.QuotaClamp
	if quotaInfo.UsePrice {
		quotaBeforeGroup, clamp = common.QuotaFromDecimalChecked(
			decimal.NewFromFloat(quotaInfo.ModelPrice).Mul(decimal.NewFromFloat(snapshot.QuotaPerUnit)),
		)
	} else {
		quotaBeforeGroup, clamp = calculateAudioQuota(quotaInfo)
	}
	if clamp != nil {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	recordChannelDailyCostWithSnapshot(ctx, snapshot, float64(quotaBeforeGroup))
}

func RecordChannelTestDailyCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, _ int, _ *billingexpr.TieredResult, usage *dto.Usage, authoritativeUsage bool) {
	if relayInfo == nil {
		return
	}
	snapshot := channelDailyCostSnapshotFromContext(ctx, relayInfo.ChannelId)
	quotaPerUnit := snapshot.QuotaPerUnit
	if relayInfo.TieredBillingSnapshot != nil {
		quotaPerUnit = relayInfo.TieredBillingSnapshot.QuotaPerUnit
		snapshot.QuotaPerUnit = quotaPerUnit
	}
	billingUsage := effectiveBillingUsage(usage)
	copiedInfo := *relayInfo
	copiedInfo.PriceData = relayInfo.PriceData
	copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
	copiedInfo.QuotaClamp = nil
	baseSummary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, &copiedInfo, billingUsage, quotaPerUnit)
	if copiedInfo.QuotaClamp != nil {
		if relayInfo.QuotaClamp == nil {
			relayInfo.QuotaClamp = copiedInfo.QuotaClamp
		}
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if relayInfo.TieredBillingSnapshot != nil {
		if !authoritativeUsage || !channelDailyCostUsageIsAuthoritative(ctx, usage) {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		usedVars := billingexpr.UsedVars(relayInfo.TieredBillingSnapshot.ExprString)
		ok, _, tieredResult := TryTieredSettle(
			relayInfo,
			BuildTieredTokenParams(billingUsage, baseSummary.IsClaudeUsageSemantic, usedVars),
		)
		if !ok || tieredResult == nil ||
			math.IsNaN(tieredResult.ActualQuotaBeforeGroup) ||
			math.IsInf(tieredResult.ActualQuotaBeforeGroup, 0) ||
			tieredResult.ActualQuotaBeforeGroup < 0 {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		quotaBeforeGroup := decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).Add(baseSummary.ToolCallSurchargeQuota)
		if _, clamp := common.QuotaFromDecimalChecked(quotaBeforeGroup); clamp != nil {
			noteQuotaClamp(relayInfo, clamp)
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		recordChannelDailyCostWithSnapshot(ctx, snapshot, quotaBeforeGroup.InexactFloat64())
		return
	}
	if !relayInfo.PriceData.UsePrice && (!authoritativeUsage || !channelDailyCostUsageIsAuthoritative(ctx, usage)) {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if !relayInfo.PriceData.UsePrice && (billingUsage == nil || !baseSummary.hasBillableUsage()) {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	recordChannelDailyCostWithSnapshot(ctx, snapshot, float64(baseSummary.Quota))
}

// RecordPerCallChannelDailyCost records successful task and Midjourney calls.
func RecordPerCallChannelDailyCost(ctx *gin.Context, channelId int, modelName string, priceData types.PriceData) {
	snapshot := channelDailyCostSnapshotFromContext(ctx, channelId)
	quotaBeforeGroup := priceData.ModelPrice * snapshot.QuotaPerUnit
	if !priceData.UsePrice {
		quotaBeforeGroup = priceData.ModelRatio / 2 * snapshot.QuotaPerUnit
	}
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		quotaBeforeGroup = priceData.ApplyOtherRatiosToFloat(quotaBeforeGroup)
	}
	recordChannelDailyCostWithSnapshot(ctx, snapshot, quotaBeforeGroup)
}

// RecordTaskChannelDailyCost synchronously registers an asynchronous task's
// initial cost so later refunds and recalculations can correct the original
// submission day without racing the generic daily-cost batcher.
func RecordTaskChannelDailyCost(ctx *gin.Context, channelId int, occurredAt int64, costEventId string, initialQuota int64, modelName string, priceData types.PriceData) (int64, bool, error) {
	snapshot := channelDailyCostSnapshotWithCurrentKey(ctx, channelDailyCostSnapshotFromContext(ctx, channelId))
	quotaBeforeGroup := priceData.ModelPrice * snapshot.QuotaPerUnit
	if !priceData.UsePrice {
		quotaBeforeGroup = priceData.ModelRatio / 2 * snapshot.QuotaPerUnit
	}
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		quotaBeforeGroup = priceData.ApplyOtherRatiosToFloat(quotaBeforeGroup)
	}
	costNanoCNY, resolved := calculateChannelDailyCost(snapshot, quotaBeforeGroup)
	if !resolved {
		recordChannelDailyCostEvent(ctx, snapshot, 0, 0, 1)
		return 0, false, nil
	}

	storedCost, err := model.RegisterChannelTaskCostEvent(channelMonitorPublishContext(ctx), model.ChannelTaskCostEventInput{
		CostEventId:    costEventId,
		ChannelId:      snapshot.ChannelId,
		OccurredAt:     occurredAt,
		APIKeyId:       snapshot.APIKeyId,
		APIKeyName:     snapshot.APIKeyName,
		KeyFingerprint: snapshot.KeyFingerprint,
		KeyDisplay:     snapshot.KeyDisplay,
		InitialQuota:   initialQuota,
		CostNanoCNY:    costNanoCNY,
	})
	if err != nil {
		return 0, false, err
	}
	setChannelDailyCostAttemptSettledCost(ctx, snapshot.ChannelId, storedCost)
	markChannelDailyCostAttemptRecorded(ctx, snapshot.ChannelId)
	return storedCost, true, nil
}
