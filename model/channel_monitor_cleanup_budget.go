package model

import "time"

// ChannelMonitorCleanupBudget bounds one retention run without interrupting
// an in-flight database batch. A zero value leaves cleanup unbounded.
type ChannelMonitorCleanupBudget struct {
	deadline time.Time
}

func NewChannelMonitorCleanupBudget(duration time.Duration) ChannelMonitorCleanupBudget {
	if duration <= 0 {
		return ChannelMonitorCleanupBudget{}
	}
	return ChannelMonitorCleanupBudget{deadline: time.Now().Add(duration)}
}

func (budget ChannelMonitorCleanupBudget) Exhausted() bool {
	return !budget.deadline.IsZero() && !time.Now().Before(budget.deadline)
}

// Slice reserves an equal share of the remaining deadline for the next cleanup
// category while preserving an unbounded zero-value budget.
func (budget ChannelMonitorCleanupBudget) Slice(remainingCategories int) ChannelMonitorCleanupBudget {
	if budget.deadline.IsZero() || remainingCategories <= 1 {
		return budget
	}
	remaining := time.Until(budget.deadline)
	if remaining <= 0 {
		return ChannelMonitorCleanupBudget{deadline: budget.deadline}
	}
	return NewChannelMonitorCleanupBudget(remaining / time.Duration(remainingCategories))
}
