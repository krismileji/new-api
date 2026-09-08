package common

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

const redisCommandHealthRecoveryWindow = 30 * time.Second

// A timeout stays active until the same client succeeds after a quiet window.
// Cumulative diagnostic counters are independent of this recovery state.
type redisCommandHealth struct {
	sync.Mutex
	lastFailure time.Time
	poolTimeout bool
	deadline    bool
}

func (health *redisCommandHealth) observe(err error, now time.Time) {
	health.Lock()
	defer health.Unlock()
	if err == nil || errors.Is(err, redis.Nil) {
		if !health.lastFailure.IsZero() && now.Sub(health.lastFailure) >= redisCommandHealthRecoveryWindow {
			health.poolTimeout = false
			health.deadline = false
		}
		return
	}
	health.lastFailure = now
	if errors.Is(err, context.DeadlineExceeded) {
		health.deadline = true
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection pool timeout") {
		health.poolTimeout = true
	}
}

func (health *redisCommandHealth) degradedReason() string {
	health.Lock()
	defer health.Unlock()
	if health.poolTimeout {
		return RedisClientPoolDegradedReasonPoolTimeout
	}
	if health.deadline {
		return RedisClientPoolDegradedReasonContextDeadline
	}
	return ""
}
