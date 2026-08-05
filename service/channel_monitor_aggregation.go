package service

import (
	"context"
	"fmt"
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
	channelMonitorAggregationFreshWait     = 10 * time.Second
	channelMonitorAggregationFreshPoll     = 250 * time.Millisecond
	channelMonitorAggregationLockPoll      = 50 * time.Millisecond
	channelMonitorAggregationRepairReserve = 5 * time.Second
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
				if err := runChannelMonitorAggregationAt(context.Background(), targetEnd, startup); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("渠道监控分钟聚合失败: %v", err))
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
	key := channelMonitorAggregationDatabaseKey{db: model.DB, logDB: model.LOG_DB}
	start, catchUp, err := channelMonitorAggregationStart(ctx, key, start, targetEnd)
	if err != nil {
		return err
	}
	if catchUp {
		mode += "_catch_up"
	}
	if err := rebuildChannelMonitorAggregationRange(ctx, key, start, targetEnd, mode, true); err != nil {
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
			start, catchUp, err := channelMonitorAggregationStart(ctx, key, start, targetEnd)
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
				err = rebuildChannelMonitorAggregationRange(ctx, key, start, targetEnd, mode, true)
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
) error {
	startedAt := time.Now()
	result, err := model.AggregateChannelMonitorMinuteRangeWithState(ctx, start, targetEnd, publishWatermark)
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
