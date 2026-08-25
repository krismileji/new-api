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

import type { AxiosAdapter } from 'axios'
import { Window } from 'happy-dom'

import { api } from '@/lib/api'

import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteResult,
} from '../../types'

const domWindow = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'MouseEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}
Object.defineProperty(domWindow.Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})

const { act, useState } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { ChannelMonitorSmartScheduleBoard } =
  await import('../channel-monitor-smart-schedule-board')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const route: ChannelMonitorSmartScheduleRoute = {
  channel_id: 1,
  channel_name: '测试渠道',
  channel_status: 1,
  channel_priority: 100,
  channel_weight: 100,
  group: 'vip',
  model: 'model-a',
  enabled: true,
  priority: 100,
  weight: 100,
  current_window_score: null,
  state: {
    id: 1,
    channel_id: 1,
    group: 'vip',
    model: 'model-a',
    participation_set: true,
    excluded: false,
    last_schedule_status: 'succeeded',
    last_schedule_error: '',
    last_schedule_score: null,
    last_schedule_priority: 100,
    last_schedule_weight: 100,
    last_schedule_time: 100,
    stability_state: '',
    stability_until: 0,
    stability_since: 0,
    stability_saved_priority: 0,
    stability_saved_weight: 0,
    runtime_protection_until: 0,
    base_rank: 1,
    base_priority: 100,
    base_weight: 100,
    temporary_traffic_kind: '',
    temporary_traffic_since: 0,
    temporary_traffic_target_percent: 0,
    manual_primary_until: 0,
    manual_primary_allow_stability_degrade: true,
    rolling_stability_score: null,
    rolling_stability_sample_count: 0,
    rolling_stability_slow_count: 0,
    rolling_stability_allowed_slow_count: 0,
    rolling_stability_updated_at: 0,
    sampling_debt: 0,
    sampling_candidate: false,
    sampling_order: '',
    last_sampling_at: 0,
  },
}

const result: ChannelMonitorSmartScheduleRouteResult = {
  generated_at: 100,
  data_cutoff_at: 100,
  processed_at: 100,
  event_watermark: 1,
  queue_depth: 0,
  realtime_degraded: false,
  performance_window_minutes: 60,
  stability_window_minutes: 120,
  sample_scope: 'channel_model',
  enabled: true,
  routes: [route],
  performance_items: [],
  stability_metrics_available: false,
  stability_items: [],
}

const channel: ChannelMonitorItem = {
  id: 1,
  name: '测试渠道',
  type: 1,
  status: 1,
  status_reason: '',
  priority: 100,
  weight: 100,
  base_url: '',
  models: 'model-a',
  test_model: 'model-a',
  groups: ['vip'],
  ratio: 1,
  previous_ratio: 1,
  cost_ratio: 1,
  previous_cost_ratio: 1,
  conversion_factor: 1,
  remark: '',
  channel_remark: '',
  updated_time: 100,
  updated_by: 1,
  updated_by_username: '管理员',
  last_fetch_status: 'succeeded',
  last_fetch_error: '',
  last_fetch_time: 100,
  consecutive_failures: 0,
  upstream_balance: 0,
  last_balance_time: 100,
  last_balance_error: '',
  today_cost_cny: 0,
  today_cost_configured: false,
  today_cost_complete: true,
  today_cost_unresolved_count: 0,
  concurrency_limit: 0,
  concurrency_active: 0,
  upstream: null,
}

const policy: ChannelMonitorSmartScheduleGroupPolicy = {
  group: 'vip',
  strategy: 'ratio',
  stability_enabled: false,
  stability_window_minutes: 15,
  jitter_enabled: false,
  jitter_tolerance_percent: 5,
  jitter_slow_threshold_seconds: 10,
  scoring: {
    stability_percent: 50,
    primary_traffic_percent: 90,
    primary_switch_threshold_percent: 3,
    smart: {
      cost_ratio_percent: 40,
      first_token_percent: 40,
      tps_percent: 20,
    },
    ratio: {
      cost_ratio_percent: 70,
      first_token_percent: 20,
      tps_percent: 10,
    },
  },
  apply_mode: 'priority_weight',
  models: [],
  min_samples: 5,
  recovery_stability_score: 95,
  fast_failure_penalty_percent: 40,
  fast_failure_seconds: 1,
  slow_failure_seconds: 10,
  cooldown_minutes: 30,
  sample_mode: 'traffic',
  sampling_order: 'ratio',
  exploration_traffic_percent: 3,
  probe_interval_minutes: 10,
  adaptive_sampling_enabled: false,
  adaptive_sampling_base_percent: 3,
  adaptive_sampling_max_percent: 30,
  adaptive_sampling_error_warning_percent: 5,
  adaptive_sampling_error_critical_percent: 15,
  adaptive_sampling_first_token_warning_seconds: 5,
  adaptive_sampling_first_token_critical_seconds: 10,
  adaptive_sampling_first_token_warning_request_percent: 10,
  adaptive_sampling_recover_request_percent: 95,
  adaptive_sampling_switch_confirm_request_percent: 95,
  adaptive_sampling_min_comparable_channels: 2,
}

const originalAdapter = api.defaults.adapter
const adapter: AxiosAdapter = async (config) => ({
  data: {
    success: true,
    message: '',
    data: {
      channel_id: 1,
      group: 'vip',
      model: 'model-a',
      duration_minutes: 60,
      allow_stability_degrade: true,
      manual_primary_until: 1_900_000_000,
      stability_protection_cleared: false,
      routing_changed: true,
      task: null,
    },
  },
  status: 200,
  statusText: 'OK',
  headers: {},
  config,
})
api.defaults.adapter = adapter

let refreshCount = 0
export function TestBoard() {
  const [currentResult, setCurrentResult] = useState(result)
  return (
    <ChannelMonitorSmartScheduleBoard
      active
      result={currentResult}
      channels={[channel]}
      groupPolicies={[policy]}
      groupRatios={{ vip: 1 }}
      isLoading={false}
      isError={false}
      onOpenSettings={() => {}}
      onOpenHistory={() => {}}
      onActionComplete={async () => {
        refreshCount += 1
        setCurrentResult((current) => ({
          ...current,
          routes: current.routes.map((currentRoute) => ({
            ...currentRoute,
            state: {
              ...currentRoute.state,
              manual_primary_until: 1_900_000_000,
              manual_primary_allow_stability_degrade: true,
            },
          })),
        }))
      }}
    />
  )
}

const container = document.createElement('div')
document.body.append(container)
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { gcTime: Number.POSITIVE_INFINITY },
    mutations: { gcTime: Number.POSITIVE_INFINITY },
  },
})
const root = createRoot(container)
await act(async () => {
  root.render(
    <QueryClientProvider client={queryClient}>
      <TestBoard />
    </QueryClientProvider>
  )
})

const pinButton = container.querySelector<HTMLButtonElement>(
  '[aria-label="固定 测试渠道 为主渠道"]'
)
assert.ok(pinButton)
await act(async () => pinButton.click())
const submitButton = [
  ...document.querySelectorAll<HTMLButtonElement>('button'),
].find((button) => button.textContent?.trim() === '固定主渠道')
assert.ok(submitButton)
await act(async () => submitButton.click())

for (let attempt = 0; attempt < 20 && refreshCount === 0; attempt += 1) {
  await act(() => Promise.resolve())
}
assert.equal(refreshCount, 1)
assert.ok(
  container.querySelector<HTMLButtonElement>('[aria-label="取消固定 测试渠道"]')
)

await act(async () => root.unmount())
queryClient.clear()
container.remove()
api.defaults.adapter = originalAdapter
domWindow.close()
