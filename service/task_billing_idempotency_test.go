package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistedTaskBillingConcurrentRefundIsIdempotent(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 61, 61, 61
	const initialUserQuota, preConsumed, tokenRemain = 10000, 4000, 7000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-task-refund-cas", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_billing_refund_cas"
	require.NoError(t, model.DB.Create(task).Error)

	var mu sync.Mutex
	var firstErrs []error
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var copyTask model.Task
			if err := model.DB.First(&copyTask, task.ID).Error; err != nil {
				mu.Lock()
				firstErrs = append(firstErrs, err)
				mu.Unlock()
				return
			}
			if ok := RefundTaskQuota(context.Background(), &copyTask, "concurrent failure"); !ok {
				mu.Lock()
				firstErrs = append(firstErrs, assert.AnError)
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Empty(t, firstErrs)
	assert.Equal(t, initialUserQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestEphemeralTaskBillingStateCleanupExpiresAndBoundsIdleEntries(t *testing.T) {
	ephemeralTaskBilling.Lock()
	originalStates := ephemeralTaskBilling.states
	ephemeralTaskBilling.states = make(map[ephemeralTaskBillingKey]*ephemeralTaskBillingEntry)
	ephemeralTaskBilling.Unlock()
	t.Cleanup(func() {
		ephemeralTaskBilling.Lock()
		ephemeralTaskBilling.states = originalStates
		ephemeralTaskBilling.Unlock()
	})

	now := time.Unix(1_800_000_000, 0)
	staleKey := ephemeralTaskBillingKey{taskID: "stale", userID: 1, channelID: 1}
	activeKey := ephemeralTaskBillingKey{taskID: "active", userID: 1, channelID: 1}
	ephemeralTaskBilling.Lock()
	ephemeralTaskBilling.states[staleKey] = &ephemeralTaskBillingEntry{
		lastSeen: now.Add(-ephemeralTaskBillingStateTTL - time.Second),
	}
	ephemeralTaskBilling.states[activeKey] = &ephemeralTaskBillingEntry{
		lastSeen: now.Add(-ephemeralTaskBillingStateTTL - time.Second),
		refs:     1,
	}
	for i := 0; i < ephemeralTaskBillingStateCap+8; i++ {
		ephemeralTaskBilling.states[ephemeralTaskBillingKey{
			taskID:    "idle-" + fmt.Sprint(i),
			userID:    2,
			channelID: 2,
		}] = &ephemeralTaskBillingEntry{lastSeen: now}
	}
	cleanupEphemeralTaskBillingStatesLocked(now)
	_, staleExists := ephemeralTaskBilling.states[staleKey]
	_, activeExists := ephemeralTaskBilling.states[activeKey]
	stateCount := len(ephemeralTaskBilling.states)
	ephemeralTaskBilling.Unlock()

	assert.False(t, staleExists)
	assert.True(t, activeExists)
	assert.LessOrEqual(t, stateCount, ephemeralTaskBillingStateCap+1)
}

func TestPersistedTaskBillingConcurrentSettlementIsIdempotent(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 62, 62, 62
	const initialUserQuota, preConsumed, actualQuota, tokenRemain = 10000, 5000, 3000, 7000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-task-settle-cas", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_billing_settle_cas"
	require.NoError(t, model.DB.Create(task).Error)

	var mu sync.Mutex
	var failures int
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var copyTask model.Task
			if err := model.DB.First(&copyTask, task.ID).Error; err != nil {
				mu.Lock()
				failures++
				mu.Unlock()
				return
			}
			RecalculateTaskQuota(context.Background(), &copyTask, actualQuota, "concurrent settlement")
		}()
	}
	close(start)
	wg.Wait()
	assert.Zero(t, failures)
	assert.Equal(t, initialUserQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestUnsavedTaskBillingConcurrentCopiesAreIdempotent(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 63, 63, 63
	const initialUserQuota, preConsumed, actualQuota, tokenRemain = 10000, 5000, 3000, 7000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-task-unsaved-cas", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)
	base := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	base.TaskID = "task_billing_unsaved_cas"

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			copyTask := *base
			RecalculateTaskQuota(context.Background(), &copyTask, actualQuota, "unsaved settlement")
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, initialUserQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestPersistedTaskBillingPersistsCostResolutionMarker(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 64, 64, 64
	const preConsumed, actualQuota = 5000, 3000
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-task-cost-marker", 7000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_billing_cost_marker"
	task.PrivateData.BillingContext.ChannelCostEventId = "task:" + task.TaskID
	require.NoError(t, model.DB.Create(task).Error)

	dayStart := model.ChannelDailyCostDayStart(task.SubmitTime)
	require.NoError(t, model.DB.Create(&model.ChannelDailyCost{
		ChannelId:    channelID,
		DayStart:     dayStart,
		CostNanoCNY:  1000,
		SettledCount: 1,
		CreatedAt:    task.SubmitTime,
		UpdatedAt:    task.SubmitTime,
	}).Error)
	require.NoError(t, model.DB.Create(&model.ChannelTaskCostEvent{
		CostEventId:        task.PrivateData.BillingContext.ChannelCostEventId,
		RegistrationToken:  "seed-marker",
		ChannelId:          channelID,
		DayStart:           dayStart,
		OccurredAt:         task.SubmitTime,
		InitialQuota:       preConsumed,
		InitialCostNanoCNY: 1000,
		CostNanoCNY:        1000,
		CreatedAt:          task.SubmitTime,
		UpdatedAt:          task.SubmitTime,
	}).Error)

	RecalculateTaskQuota(context.Background(), task, actualQuota, "persist cost marker")

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.NotNil(t, reloaded.PrivateData.BillingContext)
	assert.True(t, reloaded.PrivateData.BillingContext.ChannelCostResolved)
	assert.Equal(t, int64(600), reloaded.PrivateData.BillingContext.ChannelCostNanoCNY)
	assert.Equal(t, actualQuota, reloaded.Quota)
}

func TestPersistedTaskBillingRollsBackAccountingWhenCostLedgerFails(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 65, 65, 65
	const preConsumed, actualQuota = 5000, 3000
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-task-cost-rollback", 7000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_billing_cost_rollback"
	task.PrivateData.BillingContext.ChannelCostEventId = "task:" + task.TaskID
	require.NoError(t, model.DB.Create(task).Error)
	// The event exists, but its daily ledger row is intentionally absent. The
	// correction must fail and roll back funding, token, usage, and task quota
	// together rather than leaving a partially settled account.
	require.NoError(t, model.DB.Create(&model.ChannelTaskCostEvent{
		CostEventId:        task.PrivateData.BillingContext.ChannelCostEventId,
		RegistrationToken:  "seed-rollback",
		ChannelId:          channelID,
		DayStart:           model.ChannelDailyCostDayStart(task.SubmitTime),
		OccurredAt:         task.SubmitTime,
		InitialQuota:       preConsumed,
		InitialCostNanoCNY: 1000,
		CostNanoCNY:        1000,
		CreatedAt:          task.SubmitTime,
		UpdatedAt:          task.SubmitTime,
	}).Error)

	RecalculateTaskQuota(context.Background(), task, actualQuota, "cost ledger failure")

	assert.Equal(t, 10000, getUserQuota(t, userID))
	assert.Equal(t, 7000, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, preConsumed, getTaskQuota(t, task.ID))
	assert.Equal(t, int64(0), countLogs(t))
}
