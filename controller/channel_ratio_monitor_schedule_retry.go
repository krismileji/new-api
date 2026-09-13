package controller

import "time"

// A conflict retry remains a durable, coalesced system task. Its deadline and
// attempt survive a process restart; only this task type waits for the backoff.
func newChannelSmartScheduleConflictRetry(previous channelSmartScheduleTaskPayload, now time.Time) channelSmartScheduleTaskPayload {
	attempt := min(max(previous.ConflictRetryAttempt, 0), 4) + 1
	delay := min(5*time.Second<<uint(attempt-1), time.Minute)
	payload := newChannelSmartScheduleTaskPayload("channel_monitor.schedule_conflict", "route_configuration_conflict")
	payload.ConflictRetryAttempt = attempt
	payload.RetryNotBefore = now.Add(delay).Unix()
	return payload
}
