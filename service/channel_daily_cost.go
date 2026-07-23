package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	channelDailyCostSnapshotContextKey = "channel_daily_cost_snapshot"
	channelDailyCostSnapshotTTL        = 5 * time.Second
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
}

var channelDailyCostSnapshotCache sync.Map

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

func InvalidateChannelDailyCostSnapshot(channelId int) {
	channelDailyCostSnapshotCache.Delete(channelId)
}

func ResetChannelDailyCostSnapshotCache() {
	channelDailyCostSnapshotCache.Range(func(key any, _ any) bool {
		channelDailyCostSnapshotCache.Delete(key)
		return true
	})
}

func getChannelDailyCostSnapshot(channelId int) (channelDailyCostSnapshot, error) {
	if cached, ok := channelDailyCostSnapshotCache.Load(channelId); ok {
		entry := cached.(channelDailyCostSnapshotCacheEntry)
		if time.Now().Before(entry.ExpiresAt) {
			return entry.Snapshot, nil
		}
		channelDailyCostSnapshotCache.Delete(channelId)
	}

	snapshot := channelDailyCostSnapshot{
		ChannelId:    channelId,
		QuotaPerUnit: common.QuotaPerUnit,
	}
	monitor, err := model.GetChannelRatioMonitor(channelId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		channelDailyCostSnapshotCache.Store(channelId, channelDailyCostSnapshotCacheEntry{
			Snapshot:  snapshot,
			ExpiresAt: time.Now().Add(channelDailyCostSnapshotTTL),
		})
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if monitor.UpdatedTime <= 0 {
		channelDailyCostSnapshotCache.Store(channelId, channelDailyCostSnapshotCacheEntry{
			Snapshot:  snapshot,
			ExpiresAt: time.Now().Add(channelDailyCostSnapshotTTL),
		})
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
	channelDailyCostSnapshotCache.Store(channelId, channelDailyCostSnapshotCacheEntry{
		Snapshot:  snapshot,
		ExpiresAt: time.Now().Add(channelDailyCostSnapshotTTL),
	})
	return snapshot, nil
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
	if snapshot.APIKeyId > 0 && snapshot.APIKeyName == "" {
		if token, err := model.GetTokenById(snapshot.APIKeyId); err == nil {
			snapshot.APIKeyName = strings.TrimSpace(token.Name)
		}
	}
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
	if !snapshot.Configured || math.IsNaN(quotaBeforeGroup) || math.IsInf(quotaBeforeGroup, 0) || quotaBeforeGroup < 0 {
		recordChannelDailyCostEvent(ctx, snapshot, 0, 0, 1)
		return
	}

	costNano := decimal.NewFromFloat(quotaBeforeGroup).
		Div(decimal.NewFromFloat(snapshot.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(snapshot.CostRatioCNY)).
		Mul(decimal.NewFromInt(model.ChannelDailyCostNanoPerCNY)).
		Round(0)
	if costNano.IsNegative() || costNano.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		recordChannelDailyCostEvent(ctx, snapshot, 0, 0, 1)
		return
	}
	recordChannelDailyCostEvent(ctx, snapshot, costNano.IntPart(), 1, 0)
}

func recordChannelDailyCostEvent(ctx *gin.Context, snapshot channelDailyCostSnapshot, costNanoCNY int64, settledDelta int64, unresolvedDelta int64) {
	if snapshot.ChannelId <= 0 {
		return
	}
	dbContext := context.Background()
	if ctx != nil && ctx.Request != nil {
		dbContext = context.WithoutCancel(ctx.Request.Context())
	}
	if err := model.AddChannelDailyCostWithAPIKeyAndToken(dbContext, snapshot.ChannelId, common.GetTimestamp(), costNanoCNY, settledDelta, unresolvedDelta, snapshot.APIKeyId, snapshot.APIKeyName, snapshot.KeyFingerprint, snapshot.KeyDisplay); err != nil {
		logger.LogError(ctx, fmt.Sprintf("记录渠道 #%d 每日成本失败: %s", snapshot.ChannelId, err.Error()))
	}
}

func recordTextChannelDailyCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, billingUsage *dto.Usage, originUsage *dto.Usage, summary textQuotaSummary, tieredBillingApplied bool, tieredResult *billingexpr.TieredResult) {
	if relayInfo == nil {
		return
	}
	if !channelDailyCostUsageIsAuthoritative(ctx, originUsage) {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if tieredBillingApplied {
		if tieredResult == nil {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		quotaBeforeGroup := tieredResult.ActualQuotaBeforeGroup
		copiedInfo := *relayInfo
		copiedInfo.PriceData = relayInfo.PriceData
		copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
		copiedInfo.QuotaClamp = nil
		baseSummary := calculateTextQuotaSummary(ctx, &copiedInfo, billingUsage)
		quotaBeforeGroup += baseSummary.ToolCallSurchargeQuota.InexactFloat64()
		recordChannelDailyCostFromQuota(ctx, relayInfo.ChannelId, quotaBeforeGroup)
		return
	}
	if billingUsage == nil || summary.TotalTokens <= 0 {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}

	copiedInfo := *relayInfo
	copiedInfo.PriceData = relayInfo.PriceData
	copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
	copiedInfo.QuotaClamp = nil
	baseSummary := calculateTextQuotaSummary(ctx, &copiedInfo, billingUsage)
	if copiedInfo.QuotaClamp != nil {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	recordChannelDailyCostFromQuota(ctx, relayInfo.ChannelId, float64(baseSummary.Quota))
}

func recordAudioChannelDailyCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quotaInfo QuotaInfo, totalTokens int, authoritativeUsage bool, tieredBillingApplied bool, tieredResult *billingexpr.TieredResult) {
	if relayInfo == nil {
		return
	}
	if !authoritativeUsage {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if tieredBillingApplied {
		if tieredResult == nil {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		recordChannelDailyCostFromQuota(ctx, relayInfo.ChannelId, tieredResult.ActualQuotaBeforeGroup)
		return
	}
	if totalTokens <= 0 {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	quotaInfo.GroupRatio = 1
	quotaBeforeGroup, clamp := calculateAudioQuota(quotaInfo)
	if clamp != nil {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	recordChannelDailyCostFromQuota(ctx, relayInfo.ChannelId, float64(quotaBeforeGroup))
}

func RecordChannelTestDailyCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quota int, tieredResult *billingexpr.TieredResult, usage *dto.Usage, authoritativeUsage bool) {
	if relayInfo == nil {
		return
	}
	if !authoritativeUsage || !channelDailyCostUsageIsAuthoritative(ctx, usage) {
		recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
		return
	}
	if relayInfo.TieredBillingSnapshot != nil {
		if tieredResult == nil {
			recordChannelDailyCostUnresolved(ctx, relayInfo.ChannelId)
			return
		}
		recordChannelDailyCostFromQuota(ctx, relayInfo.ChannelId, tieredResult.ActualQuotaBeforeGroup)
		return
	}
	recordChannelDailyCostFromQuota(ctx, relayInfo.ChannelId, float64(quota))
}

// RecordPerCallChannelDailyCost records successful task and Midjourney calls.
func RecordPerCallChannelDailyCost(ctx *gin.Context, channelId int, modelName string, priceData types.PriceData) {
	snapshot := channelDailyCostSnapshotFromContext(ctx, channelId)
	if !snapshot.Configured {
		recordChannelDailyCostUnresolved(ctx, channelId)
		return
	}

	quotaBeforeGroup := priceData.ModelPrice * snapshot.QuotaPerUnit
	if !priceData.UsePrice {
		quotaBeforeGroup = priceData.ModelRatio / 2 * snapshot.QuotaPerUnit
	}
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		quotaBeforeGroup = priceData.ApplyOtherRatiosToFloat(quotaBeforeGroup)
	}
	recordChannelDailyCostWithSnapshot(ctx, snapshot, quotaBeforeGroup)
}
