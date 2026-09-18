package service

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

//go:embed channel_limit_runtime.lua
var channelLimitRuntimeScript string

type sharedChannelLimitLeaseKey struct{}

// HasSharedChannelLimitLease lets SDK adapters keep one admission to one
// upstream attempt. SDK-internal retries must return to gateway admission.
func HasSharedChannelLimitLease(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	held, _ := ctx.Value(sharedChannelLimitLeaseKey{}).(bool)
	return held
}

type channelLimitReply struct {
	Members []int                   `json:"members"`
	Tiers   []ChannelLimitTierUsage `json:"tiers"`
	ChannelConcurrencyStatus
	Acquired    bool   `json:"acquired"`
	GroupID     int64  `json:"group_id"`
	Priority    int    `json:"priority"`
	GroupActive int    `json:"group_active"`
	GroupRPM    int    `json:"group_rpm"`
	Waiting     int    `json:"waiting"`
	Reason      string `json:"reason"`
}

func acquireChannelLimitRedis(ctx context.Context, client *redis.Client, channelID int) (*ChannelConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
	if client == nil {
		return nil, false, ChannelConcurrencyStatus{}, errors.New("Redis 客户端未初始化")
	}
	admission, _ := ctx.Value(channelAdmissionContextKey{}).(*ChannelAdmission)
	member, waiting := common.GetUUID(), int64(0)
	if admission != nil {
		member = admission.ID
		if admission.Waiting {
			waiting = max(0, time.Until(admission.Deadline).Milliseconds())
		}
		if admission.channelID != 0 && admission.channelID != channelID {
			admission.Close()
		}
		admission.channelID = channelID
	}
	var reply channelLimitReply
	for retry := 0; retry < 2; retry++ {
		raw, err := client.Eval(ctx, channelLimitRuntimeScript, []string{channelLimitRegistryKey}, "acquire", 0, channelID, member, waiting, ChannelProbeTrigger(ctx) != "").Text()
		if err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
		if err := common.UnmarshalJsonStr(raw, &reply); err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
		if reply.Reason != "uninitialized" {
			break
		}
		if _, err := loadChannelConcurrencyLimits(ctx, true); err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
		if err := ensureChannelConcurrencyRedisConfig(ctx, client, getChannelConcurrencyConfigsSnapshot()); err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
	}
	if reply.Reason == "uninitialized" || reply.Reason == "unavailable" || reply.Reason == "attempt_finished" {
		return nil, false, ChannelConcurrencyStatus{}, fmt.Errorf("共享限流运行状态不可用（%s）", reply.Reason)
	}
	status := reply.ChannelConcurrencyStatus
	if reply.GroupID > 0 {
		status.Shared = &ChannelLimitStatus{GroupID: reply.GroupID, Priority: reply.Priority, Active: reply.GroupActive, RPM: reply.GroupRPM, Waiting: reply.Waiting, Reason: reply.Reason, Members: reply.Members, Tiers: reply.Tiers}
	}
	if admission != nil && admission.groupID != reply.GroupID {
		admission.Close()
		admission.groupID = reply.GroupID
	}
	if !reply.Acquired {
		return nil, false, status, nil
	}
	activeKey := channelConcurrencyRedisActivePrefix + strconv.Itoa(channelID)
	var extraKeys []string
	if ChannelProbeTrigger(ctx) != "" {
		extraKeys = append(extraKeys, activeKey+":probes")
	}
	lease := newChannelConcurrencyRedisLease(client, activeKey, member, extraKeys...)
	lease.Context = ctx
	if reply.GroupID > 0 {
		lease.Context = context.WithValue(ctx, sharedChannelLimitLeaseKey{}, true)
	}
	return lease, true, status, nil
}

type channelLimitUsage struct {
	started int64
	active  bool
	probe   bool
}
type channelLimitWaiter struct {
	channelID int
	priority  int
	probe     bool
	order     uint64
	until     int64
}
type channelLimitLocalPool struct {
	waiters  map[string]channelLimitWaiter
	sequence uint64
}

// Caller holds the channel counter lock. Requests remain attached to their
// physical channel, so editing groups never needs a counter migration.
func channelLimitChannelUsage(channelID int, now int64) (int, int, int, int) {
	requests := channelConcurrency.rpm[channelID]
	first := 0
	for first < len(requests) && requests[first] <= now-60000 {
		first++
	}
	channelConcurrency.rpm[channelID] = requests[first:]
	probeActive, probeRPM := 0, 0
	for id, request := range channelConcurrency.attempts[channelID] {
		if request.probe {
			if request.active {
				probeActive++
			}
			if request.started > now-60000 {
				probeRPM++
			}
		}
		if !request.active && request.started <= now-60000 {
			delete(channelConcurrency.attempts[channelID], id)
		}
	}
	return channelConcurrency.active[channelID], len(requests) - first, probeActive, probeRPM
}

// Caller holds the registry lock followed by the channel counter lock.
func channelLimitLocalCounts(group model.ChannelLimitGroup, now int64) (map[int]int, map[int]int, int, int) {
	active, rpm := make(map[int]int), make(map[int]int)
	priorities := make(map[int]int, len(group.Members))
	totalActive, totalRPM := 0, 0
	for _, member := range group.Members {
		priorities[member.ChannelID] = member.Priority
		a, r, pa, pr := channelLimitChannelUsage(member.ChannelID, now)
		active[member.Priority] += a - pa
		rpm[member.Priority] += r - pr
		active[-1] += pa
		rpm[-1] += pr
		totalActive, totalRPM = totalActive+a, totalRPM+r
	}
	pool := channelLimitConfig.local[group.ID]
	if pool == nil {
		pool = &channelLimitLocalPool{waiters: make(map[string]channelLimitWaiter)}
		channelLimitConfig.local[group.ID] = pool
	}
	for id, waiter := range pool.waiters {
		priority, member := priorities[waiter.channelID]
		if waiter.until <= now || !member {
			delete(pool.waiters, id)
			continue
		}
		waiter.priority = priority
		if waiter.probe {
			waiter.priority = -1
		}
		pool.waiters[id] = waiter
	}
	return active, rpm, totalActive, totalRPM
}

func channelLimitLocalEligible(group model.ChannelLimitGroup, priority, channelID int, active, rpm map[int]int, totalActive, totalRPM int, now int64) string {
	config := channelConcurrency.configs[channelID]
	if config.Limit > 0 && channelConcurrency.active[channelID] >= config.Limit {
		return "channel_concurrency"
	}
	currentRPM := 0
	for _, ts := range channelConcurrency.rpm[channelID] {
		if ts > now-60000 {
			currentRPM++
		}
	}
	if config.RPMLimit > 0 && currentRPM >= config.RPMLimit {
		return "channel_rpm"
	}
	if group.ConcurrencyLimit > 0 && totalActive >= group.ConcurrencyLimit {
		return "group_concurrency"
	}
	if group.RPMLimit > 0 && totalRPM >= group.RPMLimit {
		return "group_rpm"
	}
	reservedC, reservedR := 0, 0
	for _, tier := range group.Tiers {
		if priority >= tier.Priority {
			continue
		}
		reservedC += tier.ReservedConcurrency
		reservedR += tier.ReservedRPM
		lowerC, lowerR := 0, 0
		for p, count := range active {
			if p < tier.Priority {
				lowerC += count
			}
		}
		for p, count := range rpm {
			if p < tier.Priority {
				lowerR += count
			}
		}
		if group.ConcurrencyLimit > 0 && lowerC+1 > group.ConcurrencyLimit-reservedC {
			return "reserved_concurrency"
		}
		if group.RPMLimit > 0 && lowerR+1 > group.RPMLimit-reservedR {
			return "reserved_rpm"
		}
	}
	return ""
}

func acquireChannelLimitLocal(ctx context.Context, channelID int) (*ChannelConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
	channelLimitConfig.Lock()
	defer channelLimitConfig.Unlock()
	groupID := channelLimitConfig.registry.Members[strconv.Itoa(channelID)]
	admission, _ := ctx.Value(channelAdmissionContextKey{}).(*ChannelAdmission)
	if admission != nil && admission.groupID != 0 && (admission.groupID != groupID || admission.channelID != channelID) {
		if old := channelLimitConfig.local[admission.groupID]; old != nil {
			delete(old.waiters, admission.ID)
		}
		admission.groupID = 0
	}
	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()
	now := time.Now().UnixMilli()
	channelActive, channelRPM, _, _ := channelLimitChannelUsage(channelID, now)
	config := channelConcurrency.configs[channelID]
	status := ChannelConcurrencyStatus{Active: channelActive, Limit: config.Limit, CurrentRPM: channelRPM, RPMLimit: config.RPMLimit}
	group := channelLimitConfig.registry.Groups[strconv.FormatInt(groupID, 10)]
	priority := -1
	var pool *channelLimitLocalPool
	var active, rpm map[int]int
	totalActive, totalRPM := 0, 0
	probe := ChannelProbeTrigger(ctx) != ""
	if groupID > 0 {
		active, rpm, totalActive, totalRPM = channelLimitLocalCounts(group, now)
		pool = channelLimitConfig.local[groupID]
		status.Shared = &ChannelLimitStatus{GroupID: groupID, Active: totalActive, RPM: totalRPM, Waiting: len(pool.waiters)}
		for _, member := range group.Members {
			status.Shared.Members = append(status.Shared.Members, member.ChannelID)
			if !probe && member.ChannelID == channelID {
				priority = member.Priority
			}
		}
		status.Shared.Priority = priority
		if !group.Enabled {
			status.Shared.Reason = "paused"
			return nil, false, status, nil
		}
	}
	id := common.GetUUID()
	if admission != nil {
		id = admission.ID
		admission.groupID, admission.channelID = groupID, channelID
	}
	if channelConcurrency.attempts[channelID] == nil {
		channelConcurrency.attempts[channelID] = make(map[string]*channelLimitUsage)
	}
	if previous := channelConcurrency.attempts[channelID][id]; previous != nil {
		if !previous.active {
			return nil, false, status, errors.New("渠道请求尝试已结束")
		}
		return newChannelLimitLocalLease(ctx, previous, channelID, groupID), true, status, nil
	}
	if pool != nil && admission != nil && admission.Waiting && time.Now().Before(admission.Deadline) {
		old, exists := pool.waiters[id]
		if !exists {
			if len(pool.waiters) >= channelLimitQueueSize {
				status.Shared.Reason = "queue_full"
				return nil, false, status, nil
			}
			pool.sequence++
			old = channelLimitWaiter{channelID: channelID, order: pool.sequence}
		}
		old.priority, old.probe = priority, probe
		old.until = min(admission.Deadline.UnixMilli(), now+15000)
		pool.waiters[id] = old
	}
	reason := channelLimitLocalEligible(group, priority, channelID, active, rpm, totalActive, totalRPM, now)
	if pool != nil {
		winner, bestPriority, bestOrder := "", -2, ^uint64(0)
		for queuedID, waiter := range pool.waiters {
			if channelLimitLocalEligible(group, waiter.priority, waiter.channelID, active, rpm, totalActive, totalRPM, now) != "" {
				continue
			}
			if waiter.priority > bestPriority || (waiter.priority == bestPriority && waiter.order < bestOrder) {
				winner, bestPriority, bestOrder = queuedID, waiter.priority, waiter.order
			}
		}
		if reason == "" && winner != "" && winner != id && bestPriority >= priority {
			reason = "priority_wait"
		}
		status.Shared.Reason, status.Shared.Waiting = reason, len(pool.waiters)
	}
	if reason != "" {
		return nil, false, status, nil
	}
	request := &channelLimitUsage{started: now, active: true, probe: probe}
	channelConcurrency.attempts[channelID][id] = request
	channelConcurrency.active[channelID]++
	channelConcurrency.rpm[channelID] = append(channelConcurrency.rpm[channelID], now)
	status.Active++
	status.CurrentRPM++
	if pool != nil {
		delete(pool.waiters, id)
		status.Shared.Active++
		status.Shared.RPM++
		status.Shared.Waiting = len(pool.waiters)
	}
	return newChannelLimitLocalLease(ctx, request, channelID, groupID), true, status, nil
}

func newChannelLimitLocalLease(ctx context.Context, request *channelLimitUsage, channelID int, groupID int64) *ChannelConcurrencyLease {
	if groupID > 0 {
		ctx = context.WithValue(ctx, sharedChannelLimitLeaseKey{}, true)
	}
	return &ChannelConcurrencyLease{Context: ctx, release: func() {
		channelConcurrency.Lock()
		defer channelConcurrency.Unlock()
		if !request.active {
			return
		}
		request.active = false
		if channelConcurrency.active[channelID] > 0 {
			channelConcurrency.active[channelID]--
		}
	}}
}
