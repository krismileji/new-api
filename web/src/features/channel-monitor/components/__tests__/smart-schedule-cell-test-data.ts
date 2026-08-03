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
import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteState,
} from '../../types'

type RouteOverrides = Omit<
  Partial<ChannelMonitorSmartScheduleRoute>,
  'state'
> & {
  state?: Partial<ChannelMonitorSmartScheduleRouteState>
}

export function createSmartScheduleCellRoute(
  overrides: RouteOverrides = {}
): ChannelMonitorSmartScheduleRoute {
  const defaultState: ChannelMonitorSmartScheduleRouteState = {
    id: 1,
    channel_id: 7,
    group: 'default',
    model: 'model-a',
    participation_set: true,
    excluded: false,
    last_schedule_status: 'succeeded',
    last_schedule_error: '',
    last_schedule_score: 0.8,
    last_schedule_priority: 80,
    last_schedule_weight: 60,
    last_schedule_time: 1_752_777_845,
    stability_state: '',
    stability_until: 0,
    stability_since: 0,
    stability_saved_priority: 0,
    stability_saved_weight: 0,
    runtime_protection_until: 0,
    base_rank: 1,
    base_priority: 80,
    base_weight: 60,
    temporary_traffic_kind: '',
    temporary_traffic_since: 0,
    temporary_traffic_target_percent: 0,
    last_priority_sample_time: 0,
    manual_primary_until: 0,
    manual_primary_allow_stability_degrade: false,
  }
  return {
    channel_id: 7,
    channel_name: '测试渠道',
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 100,
    group: 'default',
    model: 'model-a',
    enabled: true,
    priority: 80,
    weight: 60,
    shared_samples: {
      id: 0,
      channel_id: 7,
      model: 'model-a',
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
    ...overrides,
    state: { ...defaultState, ...overrides.state },
  }
}
