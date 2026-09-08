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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import { ChannelMonitorPerformanceCoverageAlert } from '../channel-monitor-performance-coverage-alert'

const incompleteCoverage = {
  aggregation_enabled: true,
  aggregated_from: 1_752_776_000,
  aggregated_through: 1_752_777_800,
  window_start: 1_752_774_200,
  window_complete: false,
}

describe('channel monitor performance coverage alert', () => {
  test('窗口不完整时保留影响说明，范围和原因展开后可见', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorPerformanceCoverageAlert
        coverage={incompleteCoverage}
        rangeLabel='近60分钟'
      />
    )

    expect(screen.getByText('近60分钟监控数据暂不完整')).toBeVisible()
    expect(
      screen.getByText(/请求数可能偏低，成功率和性能指标可能暂时不准确/)
    ).toBeVisible()
    expect(screen.queryByText('查询范围：')).not.toBeInTheDocument()
    const trigger = screen.getByRole('button', { name: '查看统计详情' })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    await user.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('查询范围：')).toBeVisible()
    expect(screen.getByText('已汇总范围：')).toBeVisible()
    expect(screen.getByText(/不影响实际渠道请求/)).toBeVisible()
    expect(screen.getByText(/系统检测到实时统计链路异常/)).toBeVisible()
  })

  test('展开详情后保留每个异常原因和积压数量', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorPerformanceCoverageAlert
        coverage={incompleteCoverage}
        metadata={{
          data_cutoff_at: 1_752_777_800,
          processed_at: 1_752_777_810,
          event_watermark: 42,
          queue_depth: 3,
          redis_status: 'available',
          redis_available: true,
          redis_consumer_running: true,
          pending_count: 3,
          oldest_pending_at: 1_752_777_755,
          consumer_lag_seconds: 45,
          degraded_reasons: [
            'event_backlog',
            'publisher_unavailable',
            'marker_release_failure',
          ],
          realtime_degraded: true,
        }}
        rangeLabel='近60分钟'
      />
    )

    await user.click(screen.getByRole('button', { name: '查看统计详情' }))
    expect(screen.getByText(/其中 3 条已交付但尚未确认/)).toBeVisible()
    expect(screen.getByText(/当前延迟 45 秒/)).toBeVisible()
    expect(screen.getByText(/最近的实时事件没有成功发布/)).toBeVisible()
    expect(screen.getByText(/事件处理完成后的清理步骤失败/)).toBeVisible()
  })

  test('stays hidden after the requested window is fully covered', () => {
    const { container } = render(
      <ChannelMonitorPerformanceCoverageAlert
        coverage={{ ...incompleteCoverage, window_complete: true }}
        rangeLabel='近60分钟'
      />
    )

    expect(container).toBeEmptyDOMElement()
  })
})
