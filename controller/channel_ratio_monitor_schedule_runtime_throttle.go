package controller

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var channelSmartScheduleAdaptiveRefreshMinInterval = time.Duration(
	channelMonitorRuntimeEnvInt("CHANNEL_MONITOR_ADAPTIVE_REFRESH_MIN_INTERVAL_SECONDS", 10, 0, 3600),
) * time.Second

type channelSmartScheduleAdaptiveRefreshThrottleKey struct {
	database any
	pool     channelSmartScheduleRoutePoolKey
}

var channelSmartScheduleAdaptiveRefreshThrottle = struct {
	sync.Mutex
	database  any
	lastRun   map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time
	scheduled map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time
}{
	lastRun:   make(map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time),
	scheduled: make(map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time),
}

func channelMonitorRuntimeEnvInt(name string, fallback int, minimum int, maximum int) int {
	value := common.GetEnvOrDefault(name, fallback)
	if value < minimum || value > maximum {
		common.SysError(fmt.Sprintf("%s 超出范围，使用默认值 %d", name, fallback))
		return fallback
	}
	return value
}

func reserveChannelSmartScheduleAdaptiveRefresh(
	database any,
	pool channelSmartScheduleRoutePoolKey,
) bool {
	if channelSmartScheduleAdaptiveRefreshMinInterval <= 0 {
		return true
	}
	now := time.Now()
	key := channelSmartScheduleAdaptiveRefreshThrottleKey{database: database, pool: pool}
	channelSmartScheduleAdaptiveRefreshThrottle.Lock()
	if channelSmartScheduleAdaptiveRefreshThrottle.database != model.DB {
		channelSmartScheduleAdaptiveRefreshThrottle.database = model.DB
		channelSmartScheduleAdaptiveRefreshThrottle.lastRun = make(
			map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
		)
		channelSmartScheduleAdaptiveRefreshThrottle.scheduled = make(
			map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
		)
	}
	lastRun := channelSmartScheduleAdaptiveRefreshThrottle.lastRun[key]
	if lastRun.IsZero() || now.Sub(lastRun) >= channelSmartScheduleAdaptiveRefreshMinInterval {
		channelSmartScheduleAdaptiveRefreshThrottle.lastRun[key] = now
		delete(channelSmartScheduleAdaptiveRefreshThrottle.scheduled, key)
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
		return true
	}
	dueAt := lastRun.Add(channelSmartScheduleAdaptiveRefreshMinInterval)
	if scheduledAt, scheduled := channelSmartScheduleAdaptiveRefreshThrottle.scheduled[key]; scheduled && !scheduledAt.Before(dueAt) {
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
		return false
	}
	channelSmartScheduleAdaptiveRefreshThrottle.scheduled[key] = dueAt
	channelSmartScheduleAdaptiveRefreshThrottle.Unlock()

	time.AfterFunc(time.Until(dueAt), func() {
		channelSmartScheduleAdaptiveRefreshThrottle.Lock()
		scheduledAt, scheduled := channelSmartScheduleAdaptiveRefreshThrottle.scheduled[key]
		if !scheduled || !scheduledAt.Equal(dueAt) {
			channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
			return
		}
		delete(channelSmartScheduleAdaptiveRefreshThrottle.scheduled, key)
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
		if database == model.DB {
			enqueueChannelSmartScheduleAdaptivePoolRefresh(pool.group, pool.model)
		}
	})
	return false
}
