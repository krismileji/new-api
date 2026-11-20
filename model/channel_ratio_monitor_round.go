package model

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// RecordChannelMonitorSyncRound records the final errors once for each metric
// and advances the channel's failure streak once per bulk update.
func RecordChannelMonitorSyncRound(ctx context.Context, channelID int, revision, taskID int64, ratioErr, balanceErr error, allSucceeded bool) (monitor ChannelRatioMonitor, applied bool, err error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&monitor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if monitor.UpstreamRevision != revision || (taskID > 0 && monitor.LastSyncRoundTaskID >= taskID) {
			return nil
		}
		updates := make(map[string]any)
		if ratioErr != nil {
			monitor.LastFetchStatus = ChannelRatioFetchStatusFailed
			monitor.LastFetchError = channelMonitorSyncFailureMessage(ratioErr)
			monitor.LastFetchTime = common.GetTimestamp()
			monitor.ConsecutiveFailures = nextChannelMonitorFailureCount(monitor.ConsecutiveFailures)
			updates["last_fetch_status"] = monitor.LastFetchStatus
			updates["last_fetch_error"] = monitor.LastFetchError
			updates["last_fetch_time"] = monitor.LastFetchTime
			updates["consecutive_failures"] = monitor.ConsecutiveFailures
		}
		if balanceErr != nil {
			monitor.LastBalanceError = channelMonitorSyncFailureMessage(balanceErr)
			monitor.BalanceConsecutiveFailures = nextChannelMonitorFailureCount(monitor.BalanceConsecutiveFailures)
			updates["last_balance_error"] = monitor.LastBalanceError
			updates["balance_consecutive_failures"] = monitor.BalanceConsecutiveFailures
		}
		if monitor.SyncFailureRoundsRevision != revision || allSucceeded {
			monitor.SyncFailureRounds = 0
		}
		if ratioErr != nil || balanceErr != nil {
			monitor.SyncFailureRounds = nextChannelMonitorFailureCount(monitor.SyncFailureRounds)
		}
		updates["sync_failure_rounds"] = monitor.SyncFailureRounds
		updates["sync_failure_rounds_revision"] = revision
		if taskID > 0 {
			updates["last_sync_round_task_id"] = taskID
		}
		if err := tx.Model(&monitor).Updates(updates).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return monitor, applied, err
}

func channelMonitorSyncFailureMessage(err error) string {
	message := []rune(strings.TrimSpace(err.Error()))
	return string(message[:min(len(message), 255)])
}

func nextChannelMonitorFailureCount(count int) int {
	if count == math.MaxInt {
		return count
	}
	return max(0, count) + 1
}
