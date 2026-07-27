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

import type {
  ChannelMonitorItem,
  ChannelMonitorSettings,
  ChannelMonitorSmartScheduleRouteResult,
} from '../../types'
import { ChannelMonitorSmartScheduleTab } from '../channel-monitor-smart-schedule-tab'

const settings = {
  auto_update_interval_minutes: 0,
  auto_update_retry_count: 2,
  auto_update_consecutive_failure_limit: 2,
  auto_disable_on_update_failure: false,
  auto_enable_on_cost_ratio_recovery: false,
  auto_enable_on_balance_recovery: false,
  cost_retention_days: 120,
  email_notification_enabled: false,
  notification_email: '',
  probe_response_enabled: false,
  smart_schedule_enabled: false,
  smart_schedule_scope: 'channel',
  smart_schedule_groups: [],
  smart_schedule_interval_minutes: 10,
  smart_schedule_strategy: 'smart',
  smart_schedule_stability_enabled: true,
  smart_schedule_apply_mode: 'weight',
  smart_schedule_performance_minutes: 60,
  smart_schedule_model: 'model-a',
  smart_schedule_models: ['model-a'],
  smart_schedule_min_samples: 5,
  smart_schedule_min_success_rate: 80,
  smart_schedule_cooldown_minutes: 30,
} satisfies ChannelMonitorSettings

const channel = {
  id: 7,
  name: '测试渠道',
  type: 1,
  status: 1,
  priority: 90,
  weight: 40,
  base_url: 'https://example.com',
  models: 'model-a',
  test_model: 'model-a',
  groups: ['default'],
  ratio: 1,
  previous_ratio: null,
  cost_ratio: 1,
  previous_cost_ratio: null,
  conversion_factor: 1,
  remark: '',
  channel_remark: '',
  updated_time: 0,
  updated_by: 0,
  updated_by_username: '',
  last_fetch_status: '',
  last_fetch_error: '',
  last_fetch_time: 0,
  consecutive_failures: 0,
  upstream_balance: null,
  last_balance_time: 0,
  last_balance_error: '',
  today_cost_cny: 0,
  today_cost_configured: false,
  today_cost_complete: true,
  today_cost_unresolved_count: 0,
  concurrency_limit: 0,
  concurrency_active: 0,
  smart_schedule_excluded: false,
  last_schedule_status: 'succeeded',
  last_schedule_error: '低成功率降级中',
  last_schedule_score: null,
  last_schedule_priority: 0,
  last_schedule_weight: 0,
  last_schedule_time: 1_752_777_845,
  smart_schedule_stability_state: 'degraded',
  smart_schedule_stability_until: 1_752_777_845,
  smart_schedule_stability_since: 1_752_700_000,
  upstream: null,
} satisfies ChannelMonitorItem

describe('channel monitor smart schedule tab', () => {
  test('keeps channel-level controls in the standalone tab for compatibility mode', () => {
    const queryClient = new QueryClient()
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <ChannelMonitorSmartScheduleTab
          active
          channels={[channel]}
          settings={settings}
          onOpenSettings={() => {}}
        />
      </QueryClientProvider>
    )

    assert.ok(markup.includes('按渠道兼容模式'))
    assert.ok(markup.includes('参与渠道'))
    assert.ok(markup.includes('低成功率'))
    assert.ok(markup.includes('手动解除'))
    assert.ok(markup.includes('aria-label="手动解除 测试渠道 的稳定性保护"'))
    assert.match(markup, /role="switch" tabindex="0"/)
    assert.equal(markup.includes('实际优先级 / 权重'), false)
  })

  test('shows route-specific routing and protection state in isolated mode', () => {
    const queryClient = new QueryClient()
    const routeResult = {
      generated_at: 1_752_777_845,
      range_minutes: 60,
      scope: 'group_model',
      enabled: true,
      routes: [
        {
          channel_id: 7,
          channel_name: '测试渠道',
          channel_status: 1,
          channel_priority: 80,
          channel_weight: 50,
          group: 'vip',
          model: 'model-a',
          enabled: true,
          priority: 0,
          weight: 0,
          state: {
            id: 1,
            channel_id: 7,
            group: 'vip',
            model: 'model-a',
            participation_set: true,
            excluded: false,
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
    const isolatedSettings = {
      ...settings,
      smart_schedule_enabled: true,
      smart_schedule_scope: 'group_model',
    } satisfies ChannelMonitorSettings

    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <ChannelMonitorSmartScheduleTab
          active
          channels={[channel]}
          settings={isolatedSettings}
          onOpenSettings={() => {}}
        />
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
    assert.ok(markup.includes('解除 测试渠道 vip model-a 的稳定性保护'))
  })
})
