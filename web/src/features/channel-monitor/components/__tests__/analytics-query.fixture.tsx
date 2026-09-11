import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { afterEach } from 'vitest'

import { api } from '@/lib/api'

import type {
  ChannelMonitorAnalyticsItem,
  ChannelMonitorAnalyticsMetric,
  ChannelMonitorAnalyticsResponse,
} from '../../types-analytics'
import { ChannelMonitorAnalyticsDialog } from '../channel-monitor-analytics-dialog'

export type AnalyticsParams = Record<string, string | number | undefined>

const originalAdapter = api.defaults.adapter
const clients: QueryClient[] = []

export const analyticsMetrics = {
  actual_success_count: 9,
  actual_failure_count: 1,
  actual_sample_count: 10,
  actual_success_rate: 0.9,
  final_success_count: 9,
  final_failure_count: 0,
  final_sample_count: 9,
  final_success_rate: 1,
  cache_hit_count: 4,
  cache_sample_count: 8,
  cache_hit_rate: 0.5,
  cache_read_tokens: 40,
  input_tokens: 100,
  cache_utilization_rate: 0.4,
  cache_write_request_count: 2,
  cost_nano_cny: 900_000_000,
  settled_count: 9,
  unresolved_count: 1,
}

export function analyticsItem(
  key: string,
  fields: Partial<ChannelMonitorAnalyticsItem> = {}
): ChannelMonitorAnalyticsItem {
  return { ...analyticsMetrics, key, ...fields }
}

export function analyticsResponse(
  params: AnalyticsParams,
  items: ChannelMonitorAnalyticsItem[],
  fields: Partial<ChannelMonitorAnalyticsResponse> = {}
): ChannelMonitorAnalyticsResponse {
  return {
    source: 'redis_daily',
    group_by: params.group_by as ChannelMonitorAnalyticsResponse['group_by'],
    coverage: {
      status: 'complete',
      covered_from: 1,
      covered_through: 2,
      reasons: [],
    },
    summary: analyticsMetrics,
    scope_summary: analyticsMetrics,
    items,
    page: Number(params.page ?? 1),
    page_size: Number(params.page_size ?? 20),
    total: items.length,
    ...fields,
  }
}

export function renderAnalyticsQuery(
  metric: ChannelMonitorAnalyticsMetric,
  respond: (
    params: AnalyticsParams
  ) =>
    | ChannelMonitorAnalyticsResponse
    | Promise<ChannelMonitorAnalyticsResponse>,
  initialChannelId?: number,
  scope: Pick<
    ComponentProps<typeof ChannelMonitorAnalyticsDialog>,
    'rangeMinutes' | 'initialModel' | 'initialGroup' | 'successMode'
  > = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  clients.push(client)
  const requests: AnalyticsParams[] = []
  api.defaults.adapter = async (config) => {
    const params = config.params as AnalyticsParams
    requests.push(params)
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data: await respond(params) },
    }
  }
  const view = render(
    <QueryClientProvider client={client}>
      <ChannelMonitorAnalyticsDialog
        open
        metric={metric}
        channels={[{ id: 7, name: '渠道 A' }]}
        initialChannelId={initialChannelId}
        {...scope}
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
  return { ...view, client, requests }
}

afterEach(() => {
  cleanup()
  api.defaults.adapter = originalAdapter
  for (const client of clients) client.clear()
  clients.length = 0
})
