import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { ChannelMonitorProfitStatus } from '../channel-monitor-profit-status'
import { analyticsMetrics, analyticsResponse } from './analytics-query.fixture'

test.each([
  ['2026-09-28T12:00:00+08:00', '2026-09-28', '2026-09-29', undefined],
  ['2026-09-27T12:00:00+08:00', '2026-09-28', '2026-09-28', '2026-09-28'],
  ['2026-09-28T00:00:00+08:00', '2026-09-28', '2026-09-28', '2026-09-28'],
  ['2026-01-01T12:00:00+08:00', '2026-09-28', '2026-01-02', '2026-07-01'],
])(
  'income started at %s offers only available full dates within 90 days',
  (started, today, firstDay, selectable) => {
    const onSelectCompleteDates = vi.fn()
    render(
      <ChannelMonitorProfitStatus
        response={analyticsResponse({}, [], {
          scope_summary: {
            ...analyticsMetrics,
            income_started_at: new Date(started).getTime() / 1000,
          },
          coverage: {
            status: 'partial',
            covered_from: 0,
            covered_through: 0,
            reasons: ['income_history_unavailable'],
          },
        })}
        today={today}
        refreshing={false}
        onRefresh={async () => undefined}
        onSelectCompleteDates={onSelectCompleteDates}
      />
    )
    expect(
      screen.getByText(
        `首个完整统计日为 ${firstDay}（北京时间）；当日仍需等待收入和成本正常入账。`
      )
    ).toBeInTheDocument()
    const button = screen.queryByRole('button', { name: '仅看完整日期' })
    if (selectable) {
      expect(button).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: '仅看完整日期' }))
      expect(onSelectCompleteDates).toHaveBeenCalledWith(selectable)
    } else {
      expect(button).not.toBeInTheDocument()
    }
  }
)

test('recording gaps remain actionable independently of the launch history warning', () => {
  render(
    <ChannelMonitorProfitStatus
      response={analyticsResponse({}, [], {
        coverage: {
          status: 'partial',
          covered_from: 0,
          covered_through: 0,
          reasons: ['income_recording_gap', 'profit_cost_not_durable'],
        },
      })}
      today='2026-09-28'
      refreshing
      onRefresh={async () => undefined}
      onSelectCompleteDates={() => undefined}
    />
  )
  expect(screen.getByText(/收入或成本曾写入失败/)).toBeInTheDocument()
  expect(screen.getByText(/请启用可靠成本记录/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '正在核对…' })).toBeDisabled()
  expect(
    screen.queryByRole('button', { name: '仅看完整日期' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByText('历史缺失不会通过等待或刷新补齐。')
  ).not.toBeInTheDocument()
})
