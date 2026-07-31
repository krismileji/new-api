/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ChannelMonitorSmartScheduleRoute } from '../../types'
import { createChannelMonitorSmartSchedulePrimaryFormState } from '../smart-schedule-primary'

function createRoute(
  stateOverrides: Partial<ChannelMonitorSmartScheduleRoute['state']> = {}
): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: 7,
    channel_name: '缓存主渠道',
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 900,
    group: 'vip',
    model: 'cache-model',
    enabled: true,
    priority: 100,
    weight: 900,
    shared_samples: {
      id: 0,
      channel_id: 7,
      model: 'cache-model',
      window_start: 0,
      last_time: 0,
      last_success: false,
      last_error: '',
      sample_count: 0,
      success_count: 0,
      failure_duration_sample_count: 0,
      average_failure_duration_ms: null,
      first_token_sample_count: 0,
      average_first_token_ms: null,
      tps_sample_count: 0,
      average_tps: null,
    },
    state: {
      id: 1,
      channel_id: 7,
      group: 'vip',
      model: 'cache-model',
      participation_set: true,
      excluded: false,
      last_schedule_status: 'succeeded',
      last_schedule_error: '',
      last_schedule_score: 0.98,
      last_schedule_priority: 100,
      last_schedule_weight: 900,
      last_schedule_time: 1_752_777_845,
      stability_state: '',
      stability_until: 0,
      stability_since: 0,
      stability_saved_priority: 0,
      stability_saved_weight: 0,
      exploration_active: false,
      exploration_since: 0,
      exploration_saved_priority: 0,
      exploration_saved_weight: 0,
      manual_primary_until: 0,
      manual_primary_allow_stability_degrade: false,
      ...stateOverrides,
    },
  }
}

describe('smart schedule primary form state', () => {
  test('defaults a new fixed primary channel to allow stability degradation', () => {
    const state = createChannelMonitorSmartSchedulePrimaryFormState(
      createRoute(),
      1_000
    )

    assert.deepEqual(state, {
      durationMinutes: '60',
      allowStabilityDegrade: true,
    })
  })

  test('restores the current duration and disabled degradation setting when editing', () => {
    const state = createChannelMonitorSmartSchedulePrimaryFormState(
      createRoute({
        manual_primary_until: 1_601,
        manual_primary_allow_stability_degrade: false,
      }),
      1_000
    )

    assert.deepEqual(state, {
      durationMinutes: '11',
      allowStabilityDegrade: false,
    })
  })

  test('restores an enabled degradation setting when editing', () => {
    const state = createChannelMonitorSmartSchedulePrimaryFormState(
      createRoute({
        manual_primary_until: 1_600,
        manual_primary_allow_stability_degrade: true,
      }),
      1_000
    )

    assert.equal(state.allowStabilityDegrade, true)
  })
})
