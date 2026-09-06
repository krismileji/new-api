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
    useChannelMonitorAnalyticsMock.mockImplementation(
      (
        request: { groupBy: ChannelMonitorAnalyticsResponse['group_by'] },
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
        if (request.groupBy === 'channel') {
          data = response('channel', [item('7', { channel_id: 7 })])
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
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(channelRowButton).toBeInTheDocument()
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(useChannelMonitorAnalyticsMock).toHaveBeenCalledWith(
      expect.objectContaining({ groupBy: 'user', channelId: 7 }),
      true
    )

    fireEvent.click(screen.getByRole('button', { name: '查看Alice明细' }))
    expect(
      screen.getByRole('button', { name: '查看生产 Key明细' })
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '查看生产 Key明细' }))
    expect(screen.getByText(/模型 model-a/)).toBeInTheDocument()
    expect(screen.getAllByRole('table')).toHaveLength(1)

    fireEvent.click(channelRowButton)
    expect(channelRowButton).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
  })
})
