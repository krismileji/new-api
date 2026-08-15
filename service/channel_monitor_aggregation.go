package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	channelMonitorAggregationInterval      = time.Minute
	channelMonitorAggregationBoundaryDelay = time.Second
	channelMonitorAggregationRecentTail    = 2 * time.Minute
	channelMonitorAggregationStartupTail   = 5 * time.Minute
	channelMonitorAggregationRepairTail    = time.Hour + 5*time.Minute
	channelMonitorAggregationRepairEvery   = time.Hour
	channelMonitorAggregationBackfillChunk = time.Hour
	channelMonitorAggregationFreshWait     = 10 * time.Second
	channelMonitorAggregationFreshPoll     = 250 * time.Millisecond
	channelMonitorAggregationLockPoll      = 50 * time.Millisecond
	channelMonitorAggregationRepairReserve = 5 * time.Second

	channelMonitorAggregationBackfillDefaultMaxChunks     = 1
	channelMonitorAggregationBackfillMaxChunks            = 24
	channelMonitorAggregationBackfillDefaultBudgetSeconds = 10
	channelMonitorAggregationBackfillMaxBudgetSeconds     = 300
	channelMonitorAggregationBackfillDefaultYieldMillis   = 50
	channelMonitorAggregationBackfillMaxYieldMillis       = 5000
)

type channelMonitorAggregationDatabaseKey struct {
	db    *gorm.DB
	logDB *gorm.DB
}

var (
	channelMonitorAggregationOnce                  sync.Once
	channelMonitorAggregationRunMu                 sync.Mutex
	channelMonitorAggregationStateMu               sync.RWMutex
	channelMonitorAggregationLocalCompletedThrough = make(map[channelMonitorAggregationDatabaseKey]int64)
	channelMonitorAggregationWaitGroup             singleflight.Group
)

// StartChannelMonitorAggregationWorker refreshes the persisted minute
// aggregates in the background. Only the master node starts the worker.
func StartChannelMonitorAggregationWorker() {
	channelMonitorAggregationOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			run := func(startup bool) int64 {
				targetEnd := channelMonitorAggregationReadyEnd(time.Now())
				ctx := context.Background()
				if err := runChannelMonitorAggregationAt(ctx, targetEnd, startup); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("渠道监控分钟聚合失败: %v", err))
				} else if err := runChannelMonitorAggregationBackfill(ctx, targetEnd); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("渠道监控历史分钟汇总补齐失败: %v", err))
				} else if err := runChannelMonitorCacheUtilizationBackfill(ctx, targetEnd); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("渠道监控缓存利用率历史补齐失败: %v", err))
				}
				return targetEnd
			}

			lastAttemptedEnd := run(true)
			for {
				now := time.Now()
				if readyEnd := channelMonitorAggregationReadyEnd(now); readyEnd > lastAttemptedEnd {
					lastAttemptedEnd = run(false)
					continue
				}
				timer := time.NewTimer(time.Until(nextChannelMonitorAggregationRun(now)))
				<-timer.C
				lastAttemptedEnd = run(false)
			}
		})
	})
}

func runChannelMonitorAggregationOnce(ctx context.Context) error {
	return runChannelMonitorAggregationAt(ctx, common.GetTimestamp(), false)
}

func runChannelMonitorAggregationAt(ctx context.Context, now int64, startup bool) error {
	channelMonitorAggregationRunMu.Lock()
	defer channelMonitorAggregationRunMu.Unlock()

	routingChanged, err := model.ClearExpiredChannelSmartScheduleRoutePrimaries(now)
	if err != nil {
		return fmt.Errorf("清理到期的固定主渠道失败: %w", err)
	}
	if routingChanged && common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	if !common.LogConsumeEnabled && !constant.ErrorLogEnabled {
		return nil
	}
	start, targetEnd, mode := channelMonitorAggregationWindow(now, startup)
	if err := upgradeChannelMonitorCacheUtilizationForCurrentDay(ctx, targetEnd); err != nil {
		return err
	}

	key := channelMonitorAggregationDatabaseKey{db: model.DB, logDB: model.LOG_DB}
	start, catchUp, err := channelMonitorAggregationStart(ctx, key, start, targetEnd)
	if err != nil {
		return err
	}
	if catchUp {
		mode += "_catch_up"
	}
	if err := rebuildChannelMonitorAggregationRange(ctx, key, start, targetEnd, mode, true, true); err != nil {
		return err
	}
	repairStart, repairEnd, repair := channelMonitorAggregationRepairWindow(targetEnd)
	if startup || !repair {
		return nil
	}
	repairDeadline := nextChannelMonitorAggregationRun(time.Now()).Add(-channelMonitorAggregationRepairReserve)
	if !repairDeadline.After(time.Now()) {
		logger.LogDebug(ctx, "渠道监控整点修复因接近下一分钟而跳过")
		return nil
	}
	repairCtx, cancel := context.WithDeadline(ctx, repairDeadline)
	defer cancel()
	return rebuildChannelMonitorAggregationRange(
		repairCtx,
		key,
		repairStart,
		repairEnd,
		"hourly_repair",
		false,
		false,
	)
}

// EnsureChannelMonitorAggregationFresh prevents monitor and scheduler reads
// from using an aggregate that misses the latest safely completed minute.
func EnsureChannelMonitorAggregationFresh(ctx context.Context, now time.Time) error {
	if !common.LogConsumeEnabled && !constant.ErrorLogEnabled {
		return nil
	}
	targetEnd, err := channelMonitorAggregationFreshTarget(ctx, now)
	if err != nil {
		return err
	}
	if targetEnd <= 0 {
		return nil
	}
	key := channelMonitorAggregationDatabaseKey{db: model.DB, logDB: model.LOG_DB}
	if !common.IsMasterNode {
		channelMonitorAggregationStateMu.RLock()
		localCompletedThrough := channelMonitorAggregationLocalCompletedThrough[key]
		channelMonitorAggregationStateMu.RUnlock()
		if localCompletedThrough >= targetEnd {
			return nil
		}

		waitKey := fmt.Sprintf("%p:%p:%d", key.db, key.logDB, targetEnd)
		resultChannel := channelMonitorAggregationWaitGroup.DoChan(waitKey, func() (any, error) {
			return waitForChannelMonitorAggregationFresh(context.Background(), targetEnd)
		})
		var completedThrough int64
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-resultChannel:
			if result.Err != nil {
				return result.Err
			}
			completedThrough = result.Val.(int64)
		}
		channelMonitorAggregationStateMu.Lock()
		if channelMonitorAggregationLocalCompletedThrough[key] < completedThrough {
			model.InvalidateChannelMonitorAggregateCaches()
			channelMonitorAggregationLocalCompletedThrough[key] = completedThrough
		}
		channelMonitorAggregationStateMu.Unlock()
		return nil
	}

	channelMonitorAggregationStateMu.RLock()
	localCompletedThrough := channelMonitorAggregationLocalCompletedThrough[key]
	channelMonitorAggregationStateMu.RUnlock()
	if localCompletedThrough >= targetEnd {
		return nil
	}
	ticker := time.NewTicker(channelMonitorAggregationLockPoll)
	defer ticker.Stop()
	for {
		channelMonitorAggregationStateMu.RLock()
		localCompletedThrough = channelMonitorAggregationLocalCompletedThrough[key]
		channelMonitorAggregationStateMu.RUnlock()
		if localCompletedThrough >= targetEnd {
			return nil
		}
		if channelMonitorAggregationRunMu.TryLock() {
			channelMonitorAggregationStateMu.RLock()
			localCompletedThrough = channelMonitorAggregationLocalCompletedThrough[key]
			channelMonitorAggregationStateMu.RUnlock()
			if localCompletedThrough >= targetEnd {
				channelMonitorAggregationRunMu.Unlock()
				return nil
			}
			start := targetEnd - int64(channelMonitorAggregationRecentTail/time.Second)
			if start < 0 {
				start = 0
			}
			err := upgradeChannelMonitorCacheUtilizationForCurrentDay(ctx, targetEnd)
			var catchUp bool
			if err == nil {
				start, catchUp, err = channelMonitorAggregationStart(ctx, key, start, targetEnd)
			}
			if err == nil {
				channelMonitorAggregationStateMu.RLock()
				localCompletedThrough = channelMonitorAggregationLocalCompletedThrough[key]
				channelMonitorAggregationStateMu.RUnlock()
				if localCompletedThrough >= targetEnd {
					channelMonitorAggregationRunMu.Unlock()
					return nil
				}
				mode := "on_demand_freshness"
				if catchUp {
					mode += "_catch_up"
				}
				err = rebuildChannelMonitorAggregationRange(ctx, key, start, targetEnd, mode, true, true)
			}
			channelMonitorAggregationRunMu.Unlock()
			if err != nil {
				return fmt.Errorf("确保渠道监控使用最新完整分钟失败: %w", err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func upgradeChannelMonitorCacheUtilizationForCurrentDay(ctx context.Context, targetEnd int64) error {
	upgradeStart := model.ChannelDailyCostDayStart(targetEnd)
	result, upgraded, err := model.UpgradeChannelMonitorCacheUtilizationMetrics(
		ctx, upgradeStart, targetEnd,
	)
	if err != nil {
		return fmt.Errorf("升级缓存利用率分钟汇总失败: %w", err)
	}
	if upgraded {
		logger.LogInfo(ctx, fmt.Sprintf(
			"渠道监控缓存利用率升级完成: start=%d end=%d scanned_logs=%d metric_rows=%d",
			result.StartTimestamp,
			result.EndTimestamp,
			result.ScannedLogRows,
			result.MetricRows,
		))
	}
	return nil
}

func channelMonitorAggregationStart(
	ctx context.Context,
	key channelMonitorAggregationDatabaseKey,
	start int64,
	targetEnd int64,
) (int64, bool, error) {
	completedThrough, err := model.GetChannelMonitorAggregationCompletedThrough(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("读取渠道监控聚合水位失败: %w", err)
	}
	channelMonitorAggregationStateMu.Lock()
	observedSharedAdvance := channelMonitorAggregationLocalCompletedThrough[key] < completedThrough
	if observedSharedAdvance {
		channelMonitorAggregationLocalCompletedThrough[key] = completedThrough
	}
	channelMonitorAggregationStateMu.Unlock()
	if observedSharedAdvance {
		model.InvalidateChannelMonitorAggregateCaches()
	}
	if completedThrough <= 0 || completedThrough >= start || completedThrough >= targetEnd {
		return start, false, nil
	}
	catchUpStart := completedThrough - int64(channelMonitorAggregationRecentTail/time.Second)
	if catchUpStart < 0 {
		catchUpStart = 0
	}
	return catchUpStart, true, nil
}

func waitForChannelMonitorAggregationFresh(ctx context.Context, targetEnd int64) (int64, error) {
	timer := time.NewTimer(channelMonitorAggregationFreshWait)
	defer timer.Stop()
	ticker := time.NewTicker(channelMonitorAggregationFreshPoll)
	defer ticker.Stop()
	for {
		completedThrough, err := model.GetChannelMonitorAggregationCompletedThrough(ctx)
		if err != nil {
			return 0, fmt.Errorf("读取渠道监控聚合水位失败: %w", err)
		}
		if completedThrough >= targetEnd {
			return completedThrough, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C:
			return 0, fmt.Errorf("等待主节点聚合到 %d 超时", targetEnd)
		case <-ticker.C:
		}
	}
}

func rebuildChannelMonitorAggregationRange(
	ctx context.Context,
	key channelMonitorAggregationDatabaseKey,
	start int64,
	targetEnd int64,
	mode string,
	publishWatermark bool,
	extendCoverage bool,
) error {
	startedAt := time.Now()
	var result model.ChannelMonitorMinuteAggregationResult
	var err error
	if extendCoverage && !publishWatermark {
		result, err = model.BackfillChannelMonitorMinuteRangeWithState(ctx, start, targetEnd)
	} else {
		result, err = model.AggregateChannelMonitorMinuteRangeWithState(ctx, start, targetEnd, publishWatermark)
	}
	elapsed := time.Since(startedAt)
	if err != nil {
		return fmt.Errorf(
			"重建失败: mode=%s start=%d end=%d scanned_logs=%d metric_rows=%d duration_bucket_rows=%d elapsed_ms=%d: %w",
			mode,
			result.StartTimestamp,
			result.EndTimestamp,
			result.ScannedLogRows,
			result.MetricRows,
			result.DurationBucketRows,
			elapsed.Milliseconds(),
			err,
		)
	}
	if publishWatermark {
		channelMonitorAggregationStateMu.Lock()
		channelMonitorAggregationLocalCompletedThrough[key] = max(
			channelMonitorAggregationLocalCompletedThrough[key],
			targetEnd,
		)
		channelMonitorAggregationStateMu.Unlock()
	}
	message := fmt.Sprintf(
		"渠道监控分钟聚合完成: mode=%s start=%d end=%d scanned_logs=%d metric_rows=%d duration_bucket_rows=%d generated_rows=%d elapsed_ms=%d",
		mode,
		result.StartTimestamp,
		result.EndTimestamp,
		result.ScannedLogRows,
		result.MetricRows,
		result.DurationBucketRows,
		result.MetricRows+result.DurationBucketRows,
		elapsed.Milliseconds(),
	)
	if mode == "startup_repair" || mode == "hourly_repair" {
		logger.LogInfo(ctx, message)
	} else {
		logger.LogDebug(ctx, message)
	}
	return nil
}

func runChannelMonitorAggregationBackfill(ctx context.Context, targetEnd int64) error {
	if !common.LogConsumeEnabled && !constant.ErrorLogEnabled {
		return nil
	}
	maxChunks, budget, yield := channelMonitorAggregationBackfillLimits()
	budgetCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	windowMinutes := channelMonitorAggregationBackfillWindowMinutes()
	desiredStart := targetEnd - int64(windowMinutes*60)
	if desiredStart < 0 {
		desiredStart = 0
	}
	key := channelMonitorAggregationDatabaseKey{db: model.DB, logDB: model.LOG_DB}
	backfilled := false
	completedChunks := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-budgetCtx.Done():
			return nil
		default:
		}
		if completedChunks >= maxChunks {
			return nil
		}

		channelMonitorAggregationRunMu.Lock()
		coverage, err := model.GetChannelMonitorAggregationCoverage(budgetCtx)
		if err != nil {
			channelMonitorAggregationRunMu.Unlock()
			if budgetCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return fmt.Errorf("读取渠道监控分钟汇总覆盖范围失败: %w", err)
		}
		coveredFrom := coverage.CoveredFrom
		if coveredFrom <= 0 {
			coveredFrom = coverage.CompletedThrough
		}
		if coveredFrom <= 0 || coveredFrom <= desiredStart {
			channelMonitorAggregationRunMu.Unlock()
			if backfilled {
				logger.LogInfo(ctx, fmt.Sprintf(
					"渠道监控历史分钟汇总补齐完成: covered_from=%d completed_through=%d window_minutes=%d",
					coverage.CoveredFrom,
					coverage.CompletedThrough,
					windowMinutes,
				))
			}
			return nil
		}
		chunkStart := coveredFrom - int64(channelMonitorAggregationBackfillChunk/time.Second)
		if chunkStart < desiredStart {
			chunkStart = desiredStart
		}
		err = rebuildChannelMonitorAggregationRange(
			budgetCtx, key, chunkStart, coveredFrom, "history_backfill", false, true,
		)
		channelMonitorAggregationRunMu.Unlock()
		if err != nil {
			if budgetCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return err
		}
		backfilled = true
		completedChunks++
		if chunkStart <= desiredStart {
			logger.LogInfo(ctx, fmt.Sprintf(
				"渠道监控历史分钟汇总补齐完成: covered_from=%d completed_through=%d window_minutes=%d",
				chunkStart,
				coverage.CompletedThrough,
				windowMinutes,
			))
			return nil
		}
		if completedChunks >= maxChunks || yield <= 0 {
			continue
		}
		timer := time.NewTimer(yield)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-budgetCtx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func runChannelMonitorCacheUtilizationBackfill(ctx context.Context, targetEnd int64) error {
	if !common.LogConsumeEnabled {
		return nil
	}
	maxChunks, budget, yield := channelMonitorAggregationBackfillLimits()
	budgetCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	windowMinutes := channelMonitorAggregationBackfillWindowMinutes()
	desiredStart := targetEnd - int64(windowMinutes*60)
	if desiredStart < 0 {
		desiredStart = 0
	}
	backfilled := false
	completedChunks := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-budgetCtx.Done():
			return nil
		default:
		}
		if completedChunks >= maxChunks {
			return nil
		}

		channelMonitorAggregationRunMu.Lock()
		coverage, err := model.GetChannelMonitorAggregationCoverage(budgetCtx)
		if err != nil {
			channelMonitorAggregationRunMu.Unlock()
			if budgetCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return fmt.Errorf("读取缓存利用率汇总覆盖范围失败: %w", err)
		}
		coveredFrom := coverage.CacheUtilizationCoveredFrom
		if coverage.CacheUtilizationVersion < model.ChannelMonitorCacheUtilizationVersion ||
			coveredFrom <= 0 || coveredFrom <= desiredStart {
			channelMonitorAggregationRunMu.Unlock()
			if backfilled {
				logger.LogInfo(ctx, fmt.Sprintf(
					"渠道监控缓存利用率历史补齐完成: covered_from=%d window_minutes=%d",
					coveredFrom,
					windowMinutes,
				))
			}
			return nil
		}
		chunkStart := coveredFrom - int64(channelMonitorAggregationBackfillChunk/time.Second)
		if chunkStart < desiredStart {
			chunkStart = desiredStart
		}
		startedAt := time.Now()
		result, err := model.BackfillChannelMonitorCacheUtilizationRangeWithState(
			budgetCtx, chunkStart, coveredFrom,
		)
		channelMonitorAggregationRunMu.Unlock()
		if err != nil {
			if budgetCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return fmt.Errorf(
				"缓存利用率历史补齐失败: start=%d end=%d scanned_logs=%d metric_rows=%d elapsed_ms=%d: %w",
				result.StartTimestamp,
				result.EndTimestamp,
				result.ScannedLogRows,
				result.MetricRows,
				time.Since(startedAt).Milliseconds(),
				err,
			)
		}
		backfilled = true
		completedChunks++
		if chunkStart <= desiredStart {
			logger.LogInfo(ctx, fmt.Sprintf(
				"渠道监控缓存利用率历史补齐完成: covered_from=%d window_minutes=%d",
				chunkStart,
				windowMinutes,
			))
			return nil
		}
		if completedChunks >= maxChunks || yield <= 0 {
			continue
		}
		timer := time.NewTimer(yield)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-budgetCtx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func channelMonitorAggregationBackfillLimits() (int, time.Duration, time.Duration) {
	maxChunks := common.GetEnvOrDefault(
		"CHANNEL_MONITOR_AGGREGATION_BACKFILL_MAX_CHUNKS",
		channelMonitorAggregationBackfillDefaultMaxChunks,
	)
	if maxChunks <= 0 || maxChunks > channelMonitorAggregationBackfillMaxChunks {
		maxChunks = channelMonitorAggregationBackfillDefaultMaxChunks
	}
	budgetSeconds := common.GetEnvOrDefault(
		"CHANNEL_MONITOR_AGGREGATION_BACKFILL_BUDGET_SECONDS",
		channelMonitorAggregationBackfillDefaultBudgetSeconds,
	)
	if budgetSeconds <= 0 || budgetSeconds > channelMonitorAggregationBackfillMaxBudgetSeconds {
		budgetSeconds = channelMonitorAggregationBackfillDefaultBudgetSeconds
	}
	yieldMillis := common.GetEnvOrDefault(
		"CHANNEL_MONITOR_AGGREGATION_BACKFILL_YIELD_MS",
		channelMonitorAggregationBackfillDefaultYieldMillis,
	)
	if yieldMillis < 0 || yieldMillis > channelMonitorAggregationBackfillMaxYieldMillis {
		yieldMillis = channelMonitorAggregationBackfillDefaultYieldMillis
	}
	return maxChunks, time.Duration(budgetSeconds) * time.Second, time.Duration(yieldMillis) * time.Millisecond
}

func channelMonitorAggregationBackfillWindowMinutes() int {
	common.OptionMapRWMutex.RLock()
	rawPerformanceWindow := common.OptionMap[model.ChannelMonitorSmartSchedulePerformanceWindowOption]
	rawStabilityWindow := common.OptionMap[model.ChannelMonitorSmartScheduleStabilityWindowOption]
	common.OptionMapRWMutex.RUnlock()

	performanceWindow, err := strconv.Atoi(rawPerformanceWindow)
	if err != nil || performanceWindow <= 0 || performanceWindow > model.ChannelMonitorSmartScheduleMaxWindowMinutes {
		performanceWindow = model.ChannelMonitorSmartScheduleDefaultPerformanceWindowMinutes
	}
	stabilityWindow, err := strconv.Atoi(rawStabilityWindow)
	if err != nil || stabilityWindow <= 0 || stabilityWindow > model.ChannelMonitorSmartScheduleMaxWindowMinutes {
		stabilityWindow = model.ChannelMonitorSmartScheduleDefaultStabilityWindowMinutes
	}
	return max(performanceWindow, stabilityWindow)
}

func channelMonitorAggregationWindow(now int64, startup bool) (int64, int64, string) {
	targetEnd := now - now%int64(channelMonitorAggregationInterval/time.Second)
	tail := channelMonitorAggregationRecentTail
	mode := "minute"
	if startup {
		tail = channelMonitorAggregationStartupTail
		mode = "startup_repair"
	}
	start := targetEnd - int64(tail/time.Second)
	if start < 0 {
		start = 0
	}
	return start, targetEnd, mode
}

func channelMonitorAggregationRepairWindow(targetEnd int64) (int64, int64, bool) {
	if targetEnd <= 0 || targetEnd%int64(channelMonitorAggregationRepairEvery/time.Second) != 0 {
		return 0, 0, false
	}
	start := targetEnd - int64(channelMonitorAggregationRepairTail/time.Second)
	if start < 0 {
		start = 0
	}
	return start, targetEnd, true
}

func channelMonitorAggregationReadyEnd(now time.Time) int64 {
	readyAt := now.Add(-channelMonitorAggregationBoundaryDelay).Unix()
	return readyAt - readyAt%int64(channelMonitorAggregationInterval/time.Second)
}

func channelMonitorAggregationFreshTarget(ctx context.Context, now time.Time) (int64, error) {
	minuteEnd := now.Truncate(channelMonitorAggregationInterval)
	readyAt := minuteEnd.Add(channelMonitorAggregationBoundaryDelay)
	if now.Before(readyAt) {
		timer := time.NewTimer(time.Until(readyAt))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
	return minuteEnd.Unix(), nil
}

func nextChannelMonitorAggregationRun(now time.Time) time.Time {
	next := now.Truncate(channelMonitorAggregationInterval).Add(channelMonitorAggregationBoundaryDelay)
	if !next.After(now) {
		next = next.Add(channelMonitorAggregationInterval)
	}
	return next
}
