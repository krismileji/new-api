package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	channelMonitorAggregationInterval      = time.Minute
	channelMonitorAggregationBoundaryDelay = time.Second
	channelMonitorAggregationNormalTail    = time.Minute
	channelMonitorAggregationStartupTail   = 5 * time.Minute
	channelMonitorAggregationBackfillChunk = time.Hour
	channelMonitorDirtyRepairBatchSize     = 20
	channelMonitorDirtyRepairLease         = 2 * time.Minute

	channelMonitorAggregationBackfillDefaultMaxChunks     = 1
	channelMonitorAggregationBackfillMaxChunks            = 24
	channelMonitorAggregationBackfillDefaultBudgetSeconds = 10
	channelMonitorAggregationBackfillMaxBudgetSeconds     = 300
	channelMonitorAggregationBackfillDefaultYieldMillis   = 50
	channelMonitorAggregationBackfillMaxYieldMillis       = 5000
)

// Kept as a variable so deterministic tests can force a renewal without
// waiting for the production interval.
var channelMonitorDirtyRepairLeaseDuration = channelMonitorDirtyRepairLease
var channelMonitorDirtyRepairRenewInterval = channelMonitorDirtyRepairLease / 3

func isChannelMonitorAggregationSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked")
}

func waitChannelMonitorAggregationRetry(ctx context.Context, attempt int) error {
	delay := 10 * time.Millisecond * time.Duration(1<<attempt)
	if delay > 250*time.Millisecond {
		delay = 250 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withChannelMonitorAggregationSQLiteRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		err = fn()
		if err == nil || !isChannelMonitorAggregationSQLiteBusy(err) {
			return err
		}
		if attempt+1 < 6 {
			if waitErr := waitChannelMonitorAggregationRetry(ctx, attempt); waitErr != nil {
				return waitErr
			}
		}
	}
	return err
}

type channelMonitorAggregationDatabaseKey struct {
	db    *gorm.DB
	logDB *gorm.DB
}

var (
	channelMonitorAggregationOnce                  sync.Once
	channelMonitorAggregationRunMu                 sync.Mutex
	channelMonitorAggregationStateMu               sync.RWMutex
	channelMonitorAggregationLocalCompletedThrough = make(map[channelMonitorAggregationDatabaseKey]int64)
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
	if start < targetEnd {
		if err := rebuildChannelMonitorAggregationRange(ctx, key, start, targetEnd, mode, true, true); err != nil {
			return err
		}
	}
	return repairChannelMonitorDirtyMinutes(ctx, key, targetEnd)
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
			"渠道监控缓存利用率升级完成: start=%d end=%d scanned_logs=%d route_metric_rows=%d api_key_metric_rows=%d generated_rows=%d",
			result.StartTimestamp,
			result.EndTimestamp,
			result.ScannedLogRows,
			result.MetricRows,
			result.APIKeyMetricRows,
			result.GeneratedRows(),
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
	if channelMonitorAggregationLocalCompletedThrough[key] < completedThrough {
		channelMonitorAggregationLocalCompletedThrough[key] = completedThrough
	}
	channelMonitorAggregationStateMu.Unlock()
	if completedThrough <= 0 {
		return start, false, nil
	}
	if completedThrough >= targetEnd {
		return targetEnd, false, nil
	}
	return completedThrough, completedThrough < start, nil
}

func repairChannelMonitorDirtyMinutes(
	ctx context.Context,
	key channelMonitorAggregationDatabaseKey,
	targetEnd int64,
) error {
	if err := model.RetryChannelMonitorDirtyMinutePending(ctx, channelMonitorDirtyRepairBatchSize); err != nil {
		return fmt.Errorf("重试渠道监控脏分钟标记失败: %w", err)
	}
	claimer := fmt.Sprintf("%s:%s", common.NodeName, common.GetRandomString(8))
	claims, err := model.ClaimChannelMonitorDirtyMinutes(
		ctx,
		channelMonitorDirtyRepairBatchSize,
		claimer,
		common.GetTimestamp()+int64(channelMonitorDirtyRepairLeaseDuration/time.Second),
	)
	if err != nil {
		return fmt.Errorf("领取渠道监控脏分钟失败: %w", err)
	}
	if len(claims) == 0 {
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var leaseMu sync.Mutex
	activeClaims := append([]model.ChannelMonitorDirtyMinute(nil), claims...)
	var leaseErrMu sync.Mutex
	var leaseErr error
	getLeaseErr := func() error {
		leaseErrMu.Lock()
		defer leaseErrMu.Unlock()
		return leaseErr
	}
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		interval := channelMonitorDirtyRepairRenewInterval
		if interval <= 0 {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				leaseMu.Lock()
				snapshot := append([]model.ChannelMonitorDirtyMinute(nil), activeClaims...)
				leaseMu.Unlock()
				if len(snapshot) == 0 {
					return
				}
				lockUntil := common.GetTimestamp() + int64(channelMonitorDirtyRepairLeaseDuration/time.Second)
				if err := model.RenewChannelMonitorDirtyMinutes(workCtx, claimer, snapshot, lockUntil); err != nil {
					// A rebuild may hold the SQLite writer lock while replacing metric
					// rows. Treat transient renewal failures as retryable; only a
					// confirmed fencing loss should cancel the in-flight rebuild.
					if errors.Is(err, model.ErrChannelMonitorDirtyMinuteLeaseLost) || workCtx.Err() != nil {
						if workCtx.Err() == nil {
							leaseErrMu.Lock()
							leaseErr = err
							leaseErrMu.Unlock()
							cancel()
						}
						return
					}
					continue
				}
			}
		}
	}()
	removeActiveClaim := func(claim model.ChannelMonitorDirtyMinute) {
		leaseMu.Lock()
		defer leaseMu.Unlock()
		for index := range activeClaims {
			if activeClaims[index].Id == claim.Id && activeClaims[index].ClaimedAt == claim.ClaimedAt {
				activeClaims = append(activeClaims[:index], activeClaims[index+1:]...)
				return
			}
		}
	}
	for index, claim := range claims {
		if claim.MinuteStart >= targetEnd {
			cancel()
			<-renewDone
			if err := model.ReleaseChannelMonitorDirtyMinutes(ctx, claimer, claims[index:]); err != nil {
				return fmt.Errorf("释放未完成的渠道监控脏分钟失败: %w", err)
			}
			return getLeaseErr()
		}
		minuteEnd := claim.MinuteStart + int64(channelMonitorAggregationInterval/time.Second)
		if err := rebuildChannelMonitorAggregationRange(
			workCtx,
			key,
			claim.MinuteStart,
			minuteEnd,
			"dirty_minute_repair",
			false,
			false,
		); err != nil {
			cancel()
			<-renewDone
			releaseErr := model.ReleaseChannelMonitorDirtyMinutes(ctx, claimer, claims[index:])
			return errors.Join(err, releaseErr, getLeaseErr())
		}
		if err := model.CompleteChannelMonitorDirtyMinutes(ctx, claimer, []model.ChannelMonitorDirtyMinute{claim}); err != nil {
			cancel()
			<-renewDone
			releaseErr := model.ReleaseChannelMonitorDirtyMinutes(ctx, claimer, claims[index:])
			return errors.Join(fmt.Errorf("完成渠道监控脏分钟失败: %w", err), releaseErr, getLeaseErr())
		}
		removeActiveClaim(claim)
		if getLeaseErr() != nil {
			cancel()
			<-renewDone
			releaseErr := model.ReleaseChannelMonitorDirtyMinutes(ctx, claimer, claims[index+1:])
			return errors.Join(getLeaseErr(), releaseErr)
		}
	}
	cancel()
	<-renewDone
	if err := getLeaseErr(); err != nil {
		return err
	}
	return nil
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
	err = withChannelMonitorAggregationSQLiteRetry(ctx, func() error {
		if extendCoverage && !publishWatermark {
			result, err = model.BackfillChannelMonitorMinuteRangeWithState(ctx, start, targetEnd)
		} else {
			result, err = model.AggregateChannelMonitorMinuteRangeWithState(ctx, start, targetEnd, publishWatermark)
		}
		return err
	})
	elapsed := time.Since(startedAt)
	if err != nil {
		return fmt.Errorf(
			"重建失败: mode=%s start=%d end=%d scanned_logs=%d route_metric_rows=%d api_key_metric_rows=%d duration_bucket_rows=%d generated_rows=%d elapsed_ms=%d: %w",
			mode,
			result.StartTimestamp,
			result.EndTimestamp,
			result.ScannedLogRows,
			result.MetricRows,
			result.APIKeyMetricRows,
			result.DurationBucketRows,
			result.GeneratedRows(),
			elapsed.Milliseconds(),
			err,
		)
	}
	if err := withChannelMonitorAggregationSQLiteRetry(ctx, func() error {
		return model.UpdateChannelMonitorDailySuccessForMinuteRange(ctx, start, targetEnd)
	}); err != nil {
		return fmt.Errorf("更新渠道监控日成功率汇总失败: %w", err)
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
		"渠道监控分钟聚合完成: mode=%s start=%d end=%d scanned_logs=%d route_metric_rows=%d api_key_metric_rows=%d duration_bucket_rows=%d generated_rows=%d elapsed_ms=%d",
		mode,
		result.StartTimestamp,
		result.EndTimestamp,
		result.ScannedLogRows,
		result.MetricRows,
		result.APIKeyMetricRows,
		result.DurationBucketRows,
		result.GeneratedRows(),
		elapsed.Milliseconds(),
	)
	if mode == "startup_repair" || mode == "dirty_minute_repair" {
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
				"缓存利用率历史补齐失败: start=%d end=%d scanned_logs=%d route_metric_rows=%d api_key_metric_rows=%d generated_rows=%d elapsed_ms=%d: %w",
				result.StartTimestamp,
				result.EndTimestamp,
				result.ScannedLogRows,
				result.MetricRows,
				result.APIKeyMetricRows,
				result.GeneratedRows(),
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
	rawGroupPolicies := common.OptionMap[model.ChannelMonitorSmartScheduleGroupPoliciesOption]
	common.OptionMapRWMutex.RUnlock()

	performanceWindow, err := strconv.Atoi(rawPerformanceWindow)
	if err != nil || performanceWindow <= 0 || performanceWindow > model.ChannelMonitorSmartScheduleMaxWindowMinutes {
		performanceWindow = model.ChannelMonitorSmartScheduleDefaultPerformanceWindowMinutes
	}
	return max(performanceWindow, model.ChannelMonitorSmartScheduleMaxPolicyStabilityWindowMinutes(rawGroupPolicies))
}

func channelMonitorAggregationWindow(now int64, startup bool) (int64, int64, string) {
	targetEnd := now - now%int64(channelMonitorAggregationInterval/time.Second)
	tail := channelMonitorAggregationNormalTail
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

func channelMonitorAggregationReadyEnd(now time.Time) int64 {
	readyAt := now.Add(-channelMonitorAggregationBoundaryDelay).Unix()
	return readyAt - readyAt%int64(channelMonitorAggregationInterval/time.Second)
}

func nextChannelMonitorAggregationRun(now time.Time) time.Time {
	next := now.Truncate(channelMonitorAggregationInterval).Add(channelMonitorAggregationBoundaryDelay)
	if !next.After(now) {
		next = next.Add(channelMonitorAggregationInterval)
	}
	return next
}
