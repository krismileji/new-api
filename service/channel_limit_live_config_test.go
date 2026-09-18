package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedLimitLivePriorityAndReservationChangesKeepUsage(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			held, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			group.ConcurrencyLimit = 2
			group.Tiers[0].Priority = 50
			group.Tiers[0].ReservedConcurrency = 1
			group.Members[0].Priority = 0
			group.Members[1].Priority = 50
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "reserved_concurrency", status.Shared.Reason)
			assert.Equal(t, 1, status.Shared.Active)
			assert.Equal(t, 1, status.Shared.RPM)
			held.Release()
			fresh, ok, status, err := AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(fresh.Release)
			assert.Equal(t, 1, status.Shared.Active)
			assert.Equal(t, 2, status.Shared.RPM)
		})
	}
}

func TestSharedLimitLiveQueueUsesCurrentPriority(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.ConcurrencyLimit = 1
			group.Tiers[0].ReservedConcurrency = 0
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			held, ok, _, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			firstCtx, first := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			secondCtx, second := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			first.Waiting, second.Waiting = true, true
			t.Cleanup(first.Close)
			t.Cleanup(second.Close)
			_, ok, _, err = AcquireChannelConcurrency(firstCtx, 1)
			require.NoError(t, err)
			require.False(t, ok)
			_, ok, _, err = AcquireChannelConcurrency(secondCtx, 3)
			require.NoError(t, err)
			require.False(t, ok)
			group.Members[0].Priority = 0
			group.Members[2].Priority = 100
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			held.Release()
			_, ok, status, err := AcquireChannelConcurrency(firstCtx, 1)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "priority_wait", status.Shared.Reason)
			// Removing the preferred waiter must unblock the remaining member immediately.
			group.Members = append(group.Members[:2], group.Members[3:]...)
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			fresh, ok, _, err := AcquireChannelConcurrency(firstCtx, 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(fresh.Release)
		})
	}
}

func TestSharedLimitRegistryReloadAndResumeDoNotResetUsage(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.ConcurrencyLimit = 3
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			held, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			if common.RedisEnabled {
				require.NoError(t, common.RDB.Del(t.Context(), channelLimitRegistryKey).Err())
			} else {
				channelLimitConfig.Lock()
				channelLimitConfig.db = nil
				channelLimitConfig.Unlock()
			}
			views, err := ListChannelLimitGroupViews(t.Context())
			require.NoError(t, err)
			require.Len(t, views, 1)
			assert.Empty(t, views[0].Runtime.Reason)
			assert.Equal(t, 1, views[0].Runtime.Active)
			assert.Equal(t, 1, views[0].Runtime.RPM)
			group.Enabled = false
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			group.Enabled = true
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			fresh, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(fresh.Release)
			assert.Equal(t, 2, status.Shared.Active)
			assert.Equal(t, 2, status.Shared.RPM)
		})
	}
}

func TestSharedLimitLiveCreationMembershipAndDeletionKeepChannelUsage(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			// Return channels to independent limiting while existing traffic is recorded.
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, true))
			held, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			group = &model.ChannelLimitGroup{Name: "在线新建", Enabled: true, ConcurrencyLimit: 1, RPMLimit: 2,
				Tiers: []model.ChannelLimitGroupTier{{Priority: 0}}, Members: []model.ChannelLimitGroupMember{{ChannelID: 1}, {ChannelID: 2}}}
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_concurrency", status.Shared.Reason)
			assert.Equal(t, 1, status.Shared.RPM)
			// Moving membership out and back must not reset the underlying counters.
			group.Members = group.Members[1:]
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			other := &model.ChannelLimitGroup{Name: "另一上游组", Enabled: true, ConcurrencyLimit: 1, RPMLimit: 1,
				Tiers: []model.ChannelLimitGroupTier{{Priority: 0}}, Members: []model.ChannelLimitGroupMember{{ChannelID: 1}}}
			require.NoError(t, SaveChannelLimitGroup(t.Context(), other, false))
			_, ok, status, err = AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, other.ID, status.Shared.GroupID)
			assert.Equal(t, 1, status.Shared.Active)
			assert.Equal(t, 1, status.Shared.RPM)
			require.NoError(t, SaveChannelLimitGroup(t.Context(), other, true))
			group.Members = append(group.Members, model.ChannelLimitGroupMember{ChannelID: 1})
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			held.Release()
			fresh, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			fresh.Release()
			assert.Equal(t, 2, status.Shared.RPM)
			_, ok, status, err = AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_rpm", status.Shared.Reason)
		})
	}
}

func TestSharedLimitLiveLowerAndRaiseLimitsPreserveInflightAndRPM(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			first, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(first.Release)
			second, ok, _, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(second.Release)
			group.Tiers[0].ReservedConcurrency = 0
			group.ConcurrencyLimit = 1
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, 2, status.Shared.Active)
			assert.Equal(t, "group_concurrency", status.Shared.Reason)
			assert.NoError(t, first.Context.Err())
			assert.NoError(t, second.Context.Err())
			first.Release()
			second.Release()
			group.Tiers[0].ReservedRPM = 0
			group.RPMLimit = 2
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			_, ok, status, err = AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_rpm", status.Shared.Reason)
			assert.Equal(t, 2, status.Shared.RPM)
			group.RPMLimit = 3
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			fresh, ok, status, err := AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(fresh.Release)
			assert.Equal(t, 3, status.Shared.RPM)
		})
	}
}
