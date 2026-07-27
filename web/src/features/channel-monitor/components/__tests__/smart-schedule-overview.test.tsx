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

import { renderToStaticMarkup } from 'react-dom/server'

import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteResult,
} from '../../types'
import { ChannelMonitorSmartScheduleOverviewCard } from '../channel-monitor-smart-schedule-overview'

function createRoute(
  channelId: number,
  channelStatus: number
): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: channelId,
    channel_name: `渠道 ${channelId}`,
    channel_status: channelStatus,
    channel_priority: 100,
    channel_weight: 100,
    group: 'default',
    model: 'model-a',
    enabled: true,
    priority: 80,
    weight: 60,
    state: {
      id: channelId,
      channel_id: channelId,
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
    },
  }
}

describe('smart schedule overview card', () => {
  test('shows participation separately from current schedulable routes', () => {
    const result = {
      generated_at: 1_752_777_845,
      range_minutes: 60,
      enabled: true,
      routes: [createRoute(1, 1), createRoute(2, 2)],
      performance_items: [],
      stability_metrics_available: true,
      stability_items: [],
    } satisfies ChannelMonitorSmartScheduleRouteResult
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleOverviewCard
        result={result}
        isLoading={false}
        isError={false}
        onOpen={() => {}}
      />
    )

    assert.ok(markup.includes('2/2'))
    assert.ok(markup.includes('当前可调度 1'))
    assert.ok(markup.includes('查看智能调度总览，参与配置 2/2'))
  })
})
