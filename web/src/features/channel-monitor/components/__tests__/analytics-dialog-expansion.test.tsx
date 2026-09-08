import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type {
  ChannelMonitorAnalyticsItem,
  ChannelMonitorAnalyticsResponse,
} from '../../types-analytics'
import { ChannelMonitorAnalyticsDialog } from '../channel-monitor-analytics-dialog'

const { useChannelMonitorAnalyticsMock } = vi.hoisted(() => ({
  useChannelMonitorAnalyticsMock: vi.fn(),
}))

vi.mock('../../hooks/use-channel-monitor-analytics', () => ({
  useChannelMonitorAnalytics: useChannelMonitorAnalyticsMock,
}))

const summary = {
  actual_success_count: 9,
  actual_failure_count: 1,
  actual_sample_count: 10,
  actual_success_rate: 0.9,
  final_success_count: 9,
  final_failure_count: 1,
  final_sample_count: 10,
  final_success_rate: 0.9,
  cache_hit_count: 4,
  cache_sample_count: 8,
  cache_hit_rate: 0.5,
  cache_read_tokens: 40,
  input_tokens: 100,
  cache_utilization_rate: 0.4,
  cache_write_request_count: 2,
}

function response(
  groupBy: ChannelMonitorAnalyticsResponse['group_by'],
  items: ChannelMonitorAnalyticsItem[]
) {
  return {
    data: {
      source: 'redis_daily',
      group_by: groupBy,
      coverage: {
        status: 'complete',
        covered_from: 1,
        covered_through: 2,
        reasons: [],
      },
      summary,
      scope_summary: summary,
      items,
      page: 1,
      page_size: 20,
      total: items.length,
    },
  } satisfies { data: ChannelMonitorAnalyticsResponse }
}

function item(key: string, fields: Partial<ChannelMonitorAnalyticsItem> = {}) {
  return { ...summary, key, ...fields }
}

describe('ChannelMonitorAnalyticsDialog expansion', () => {
  beforeEach(() => {
    useChannelMonitorAnalyticsMock.mockClear()
    useChannelMonitorAnalyticsMock.mockImplementation(
      (
        request: {
          groupBy: ChannelMonitorAnalyticsResponse['group_by']
          channelId?: number
          apiKeyId?: number
          model?: string
        },
        enabled: boolean
      ) => {
        if (!enabled) {
          return {
            data: undefined,
            isError: false,
            isFetching: false,
            isLoading: false,
            refetch: vi.fn(),
          }
        }
        let data: ReturnType<typeof response>
        if (request.groupBy === 'channel' && request.apiKeyId === 201) {
          data = response('channel', [
            item('7:model-a', {
              api_key_id: 201,
              channel_id: 7,
              model_name: 'model-a',
            }),
          ])
        } else if (request.groupBy === 'channel') {
          data = response('channel', [item('7', { channel_id: 7 })])
        } else if (request.groupBy === 'model') {
          data = response('model', [
            item('model-a', {
              api_key_id: request.apiKeyId,
              api_key_name: request.apiKeyId ? '生产 Key' : undefined,
              channel_id: request.channelId,
              model_key: 'model-a',
              model_name: 'model-a',
            }),
          ])
        } else if (request.groupBy === 'user') {
          data = response('user', [
            item('31', {
              user_id: 31,
              user_name: 'alice',
              user_display_name: 'Alice',
            }),
          ])
        } else if (request.groupBy === 'api_key') {
          data = response('api_key', [
            item('201', { api_key_id: 201, api_key_name: '生产 Key' }),
          ])
        } else {
          data = response('api_key_channel_model', [
            item('201:7:model-a', {
              api_key_id: 201,
              api_key_name: '生产 Key',
              channel_id: 7,
              model_name: 'model-a',
            }),
          ])
        }
        return {
          data,
          isError: false,
          isFetching: false,
          isLoading: false,
          refetch: vi.fn(),
        }
      }
    )
  })

  test('expands and loads child dimensions without replacing the root table', () => {
    render(
      <ChannelMonitorAnalyticsDialog
        open
        metric='success'
        channels={[{ id: 7, name: '渠道 A' }]}
        onOpenChange={() => undefined}
      />
    )

    const channelRowButton = screen.getByRole('button', {
      name: '查看渠道 A明细',
    })
    fireEvent.click(channelRowButton)

    expect(channelRowButton).toHaveAttribute('aria-expanded', 'true')
    const modelRowButton = screen.getByRole('button', {
      name: '查看model-a明细',
    })
    expect(channelRowButton).toBeInTheDocument()
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table').parentElement?.parentElement).toHaveClass(
      'shrink-0'
    )
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({ groupBy: 'model', channelId: 7 }),
      true
    )

    fireEvent.click(modelRowButton)
    const userRowButton = screen.getByRole('button', { name: '查看alice明细' })
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'user',
        channelId: 7,
        model: 'model-a',
      }),
      true
    )

    fireEvent.click(userRowButton)
    expect(screen.getByText('生产 Key')).toBeInTheDocument()
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'api_key',
        channelId: 7,
        model: 'model-a',
        userId: 31,
      }),
      true
    )
    expect(screen.getAllByRole('table')).toHaveLength(1)

    fireEvent.click(channelRowButton)
    expect(channelRowButton).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
  })

  test('expands users into API keys, models and then channels', () => {
    render(
      <ChannelMonitorAnalyticsDialog
        open
        metric='success'
        channels={[{ id: 7, name: '渠道 A' }]}
        onOpenChange={() => undefined}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))

    fireEvent.click(screen.getByRole('button', { name: '查看alice明细' }))

    const apiKeyRowButton = screen.getByRole('button', {
      name: '查看生产 Key明细',
    })
    fireEvent.click(apiKeyRowButton)

    const modelRowButton = screen.getByRole('button', {
      name: '查看model-a明细',
    })
    expect(modelRowButton).toBeInTheDocument()
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({ groupBy: 'model', userId: 31, apiKeyId: 201 }),
      true
    )

    fireEvent.click(modelRowButton)

    expect(screen.getByText('渠道 A')).toBeInTheDocument()
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'channel',
        userId: 31,
        apiKeyId: 201,
        model: 'model-a',
      }),
      true
    )
  })

  test('sorts success analysis by call count by default and toggles header sorting', () => {
    render(
      <ChannelMonitorAnalyticsDialog
        open
        metric='success'
        channels={[{ id: 7, name: '渠道 A' }]}
        onOpenChange={() => undefined}
      />
    )

    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'channel',
        sort: 'samples',
        direction: 'desc',
      }),
      true
    )

    fireEvent.click(
      screen.getByRole('button', { name: '按上游成功率排序（当前未排序）' })
    )
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'channel',
        sort: 'success_rate',
        direction: 'desc',
      }),
      true
    )
  })

  test('sorts cost analysis by cost descending by default and toggles direction', () => {
    render(
      <ChannelMonitorAnalyticsDialog
        open
        metric='cost'
        channels={[{ id: 7, name: '渠道 A' }]}
        onOpenChange={() => undefined}
      />
    )

    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'channel',
        sort: 'cost',
        direction: 'desc',
      }),
      true
    )

    fireEvent.click(
      screen.getByRole('button', { name: '按成本排序（当前降序）' })
    )
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'channel',
        sort: 'cost',
        direction: 'asc',
      }),
      true
    )
  })

  test('defaults to today and accepts a selected date range', () => {
    render(
      <ChannelMonitorAnalyticsDialog
        open
        metric='cost'
        channels={[{ id: 7, name: '渠道 A' }]}
        onOpenChange={() => undefined}
      />
    )

    const rangeTrigger = screen.getByRole('button', { name: '统计日期范围' })
    expect(rangeTrigger).toHaveTextContent('当日')
    fireEvent.click(rangeTrigger)
    fireEvent.change(screen.getByLabelText('开始日期'), {
      target: { value: '2026-09-01' },
    })
    fireEvent.change(screen.getByLabelText('结束日期'), {
      target: { value: '2026-09-03' },
    })
    fireEvent.click(screen.getByRole('button', { name: '应用' }))

    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        groupBy: 'channel',
        from: '2026-09-01',
        to: '2026-09-04',
      }),
      true
    )
  })
})
