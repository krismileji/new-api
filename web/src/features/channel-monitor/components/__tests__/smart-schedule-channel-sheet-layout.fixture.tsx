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

import { Window } from 'happy-dom'

import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteStability,
} from '../../types'

function createRoute(model: string): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: 7,
    channel_name: '测试渠道',
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 10,
    group: 'vip',
    model,
    enabled: true,
    priority: 100,
    weight: 10,
    state: {
      id: model === 'model-a' ? 1 : 2,
      channel_id: 7,
      group: 'vip',
      model,
      participation_set: true,
      excluded: false,
      last_schedule_status: 'succeeded',
      last_schedule_error: '',
      last_schedule_score: 0.97,
      last_schedule_priority: 100,
      last_schedule_weight: 10,
      last_schedule_time: 0,
      stability_state: '',
      stability_until: 0,
      stability_since: 0,
      stability_saved_priority: 0,
      stability_saved_weight: 0,
      exploration_active: false,
      exploration_since: 0,
      exploration_saved_priority: 0,
      exploration_saved_weight: 0,
      probe_window_start: 0,
      probe_last_time: 0,
      probe_last_success: false,
      probe_last_error: '',
      probe_sample_count: 0,
      probe_success_count: 0,
      probe_failure_duration_sample_count: 0,
      probe_average_failure_duration_ms: null,
      probe_first_token_sample_count: 0,
      probe_average_first_token_ms: null,
      probe_tps_sample_count: 0,
      probe_average_tps: null,
    },
  }
}

function createStability(
  model: string,
  jitterAvailable: boolean
): ChannelMonitorSmartScheduleRouteStability {
  return {
    channel_id: 7,
    group: 'vip',
    model,
    success_count: 99,
    failure_count: 1,
    final_failure_count: 0,
    retry_failure_count: 1,
    sample_count: 100,
    success_rate: 0.99,
    stability_score: 0.97,
    average_retry_failure_duration_ms: 250,
    retry_failure_duration_buckets: [],
    jitter_available: jitterAvailable,
    first_token_baseline_ms: jitterAvailable ? 300 : null,
    first_token_p50_ms: jitterAvailable ? 320 : null,
    first_token_p95_ms: jitterAvailable ? 950 : null,
    jitter_threshold_ms: jitterAvailable ? 1000 : null,
    jitter_sample_count: jitterAvailable ? 100 : 0,
    jitter_slow_count: jitterAvailable ? 7 : 0,
    jitter_allowed_count: jitterAvailable ? 5 : 0,
    jitter_penalty: jitterAvailable ? 2 : 0,
  }
}

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'CustomEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { ChannelMonitorSmartScheduleChannelSheet } =
  await import('../channel-monitor-smart-schedule-channel-sheet')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)
const queryClient = new QueryClient()

await act(async () => {
  root.render(
    <QueryClientProvider client={queryClient}>
      <ChannelMonitorSmartScheduleChannelSheet
        channel={null}
        channelId={7}
        routes={[createRoute('model-a'), createRoute('model-b')]}
        groupRatios={{ vip: 1 }}
        placements={new Map()}
        performanceItems={[]}
        stabilityItems={[
          createStability('model-a', true),
          createStability('model-b', false),
        ]}
        stabilityMetricsAvailable
        rangeMinutes={60}
        open
        onOpenChange={() => {}}
      />
    </QueryClientProvider>
  )
})

const sheet = document.body.querySelector('[data-slot="sheet-content"]')
assert.ok(sheet)
const header = sheet.querySelector('[data-slot="sheet-header"]')
const scrollBody = [...sheet.children].find((element) =>
  element.classList.contains('overflow-y-auto')
)
const footer = sheet.querySelector('[data-slot="sheet-footer"]')
const jitterDetailCount = sheet.querySelectorAll(
  '[data-slot="smart-schedule-jitter-detail"]'
).length
const usesChannelDrawerLayout =
  sheet.classList.contains('h-dvh') &&
  sheet.classList.contains('w-full') &&
  sheet.classList.contains('overflow-hidden') &&
  sheet.classList.contains('p-0') &&
  sheet.classList.contains('sm:max-w-5xl') &&
  header?.classList.contains('border-b') === true &&
  header.classList.contains('sm:px-6') &&
  scrollBody?.classList.contains('sm:px-6') === true &&
  footer?.classList.contains('border-t') === true &&
  footer.classList.contains('sm:px-6')

process.stdout.write(
  `${JSON.stringify({
    side: sheet.getAttribute('data-side'),
    title: sheet.textContent ?? '',
    jitterDetailCount,
    usesChannelDrawerLayout,
  })}\n`
)

await act(async () => root.unmount())
container.remove()
domWindow.close()
