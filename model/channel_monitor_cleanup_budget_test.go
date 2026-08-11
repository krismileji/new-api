package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChannelMonitorCleanupBudgetSlice(t *testing.T) {
	unbounded := ChannelMonitorCleanupBudget{}
	assert.True(t, unbounded.Slice(3).deadline.IsZero())

	deadline := time.Now().Add(30 * time.Second)
	budget := ChannelMonitorCleanupBudget{deadline: deadline}
	assert.Equal(t, deadline, budget.Slice(1).deadline)

	sliced := budget.Slice(3)
	wantDeadline := time.Now().Add(10 * time.Second)
	assert.WithinDuration(t, wantDeadline, sliced.deadline, time.Second)
}
