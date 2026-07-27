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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderToStaticMarkup } from 'react-dom/server'

import type { ChannelMonitorSmartScheduleRouteResult } from '../../types'
import { ChannelMonitorSmartScheduleTab } from '../channel-monitor-smart-schedule-tab'

describe('channel monitor smart schedule tab', () => {
  test('shows only group-model routing and clickable protection state', () => {
    const queryClient = new QueryClient()
    const routeResult = {
      generated_at: 1_752_777_845,
      range_minutes: 60,
      enabled: true,
      routes: [
        {
          channel_id: 7,
          channel_name: '测试渠道',
          channel_status: 2,
          channel_priority: 80,
          channel_weight: 50,
          group: 'vip',
          model: 'model-a',
          enabled: false,
          priority: 0,
          weight: 0,
          state: {
            id: 1,
            channel_id: 7,
            group: 'vip',
            model: 'model-a',
            participation_set: true,
            excluded: true,
            last_schedule_status: 'succeeded',
            last_schedule_error: '成功率过低',
            last_schedule_score: null,
            last_schedule_priority: 0,
            last_schedule_weight: 0,
            last_schedule_time: 1_752_777_845,
            stability_state: 'degraded',
            stability_until: 1_752_777_845,
            stability_since: 1_752_700_000,
            stability_saved_priority: 95,
            stability_saved_weight: 70,
          },
        },
      ],
      performance_items: [],
      stability_metrics_available: true,
      stability_items: [
        {
          channel_id: 7,
          group: 'vip',
          model: 'model-a',
          success_count: 3,
          failure_count: 2,
          sample_count: 5,
          success_rate: 0.6,
        },
      ],
    } satisfies ChannelMonitorSmartScheduleRouteResult
    queryClient.setQueryData(['channel-monitor', 'smart-schedule', 'routes'], {
      success: true,
      message: '',
      data: routeResult,
    })
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <ChannelMonitorSmartScheduleTab active onOpenSettings={() => {}} />
      </QueryClientProvider>
    )

    assert.ok(markup.includes('按分组和模型'))
    assert.ok(markup.includes('实际优先级 / 权重'))
    assert.ok(markup.includes('P0 / W0'))
    assert.ok(markup.includes('渠道默认 P80 / W50'))
    assert.ok(markup.includes('60.0% · 5 次'))
    assert.ok(markup.includes('低成功率'))
    assert.ok(markup.includes('overflow-x-auto'))
    assert.ok(markup.includes('role="switch"'))
    assert.ok(markup.includes('解除 测试渠道 vip model-a 的低成功率保护'))
    assert.equal(markup.includes('按渠道'), false)
    assert.equal(markup.includes('手动解除'), false)
  })
})
