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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorRealtimeMetadata } from '../../types'
import { ChannelMonitorRealtimeStatus } from '../channel-monitor-realtime-status'

const alertMetadata = {
  generated_at: 1_752_777_845,
  data_cutoff_at: 1_752_777_840,
  processed_at: 1_752_777_845,
  event_watermark: 42,
  queue_depth: 6,
  redis_status: 'unavailable',
  redis_available: false,
  redis_consumer_running: false,
  pending_count: 6,
  writer_queue_depth: 2,
  writer_queue_capacity: 100,
  writer_queue_age_seconds: 9,
  cost_queue_pending_count: 3,
  cost_stream_pending_count: 2,
  cost_stream_unread_count: 4,
  cost_outbox_pending_count: 5,
  cost_outbox_oldest_pending_at: 1_752_777_790,
  cost_outbox_retry_count: 7,
  cost_ledger_failed_count: 3,
  cost_publish_failed_count: 1,
  cost_dead_letter_count: 2,
  oldest_pending_at: 1_752_777_800,
  consumer_lag_seconds: 45,
  last_published_at: 1_752_777_830,
  last_processed_at: 1_752_777_845,
  retry_count: 3,
  takeover_count: 2,
  quarantine_count: 1,
  last_quarantined_at: 1_752_777_820,
  marker_release_failure_count: 4,
  marker_release_failure_active: true,
  stream_trim_failure_count: 5,
  stream_trim_failure_active: true,
  realtime_degraded: true,
} satisfies ChannelMonitorRealtimeMetadata

describe('channel monitor realtime status', () => {
  test('移动端摘要直接显示基础状态和严重告警，并隐藏长诊断', () => {
    const { container } = render(
      <ChannelMonitorRealtimeStatus metadata={alertMetadata} />
    )

    const status = container.querySelector(
      '[data-channel-monitor-realtime-status]'
    )
    expect(status).not.toBeNull()
    const summary = within(status as HTMLElement).getByRole('group', {
      name: '运行状态摘要',
    })

    expect(within(summary).getByText('Redis 故障')).toBeInTheDocument()
    expect(within(summary).getByText('事件处理 已停止')).toBeInTheDocument()
    expect(within(summary).getByText('实时数据已降级')).toBeInTheDocument()
    expect(within(summary).getByText('实时事件待处理 6')).toBeInTheDocument()
    expect(
      within(summary).getByText('监控写入队列 2/100（9 秒）')
    ).toBeInTheDocument()
    expect(
      within(summary).getByText('成本事件未读取 4 / 待确认 2')
    ).toBeInTheDocument()
    expect(within(summary).getByText('成本事件排队失败')).toBeInTheDocument()
    expect(within(summary).getByText('成本账本写入失败')).toBeInTheDocument()
    expect(
      within(summary).getByText('成本事件进入异常队列')
    ).toBeInTheDocument()
    expect(
      within(summary).queryByText(/已处理事件序号/)
    ).not.toBeInTheDocument()
    expect(
      within(summary).getByRole('button', { name: '运行详情' })
    ).toBeInTheDocument()
    expect(summary.className).toContain('lg:hidden')
  })

  test('桌面端直接显示完整运行诊断', () => {
    const { container } = render(
      <ChannelMonitorRealtimeStatus metadata={alertMetadata} />
    )

    const status = container.querySelector(
      '[data-channel-monitor-realtime-status]'
    )
    expect(status).not.toBeNull()
    const details = within(status as HTMLElement).getByRole('group', {
      name: '完整运行状态',
    })

    expect(details).toHaveTextContent(
      `查询时间 ${formatTimestampToDate(alertMetadata.generated_at)}`
    )
    expect(details).toHaveTextContent('已处理事件序号 42')
    expect(details).toHaveTextContent('处理延迟 45 秒')
    expect(details).toHaveTextContent('成本账本写入重试 7 次')
    expect(details.className).toContain('hidden')
    expect(details.className).toContain('lg:inline-flex')
  })

  test('点击运行详情后在底部面板读取完整诊断', async () => {
    const user = userEvent.setup()
    const { container } = render(
      <ChannelMonitorRealtimeStatus metadata={alertMetadata} />
    )

    const status = container.querySelector(
      '[data-channel-monitor-realtime-status]'
    )
    expect(status).not.toBeNull()
    const summary = within(status as HTMLElement).getByRole('group', {
      name: '运行状态摘要',
    })
    await user.click(within(summary).getByRole('button', { name: '运行详情' }))

    const dialog = await screen.findByRole('dialog', {
      name: '实时运行详情',
    })
    const diagnostics = within(dialog).getByRole('region', {
      name: '实时运行完整诊断',
    })

    expect(dialog).toHaveAttribute('data-side', 'bottom')
    expect(
      within(dialog).getByText('事件处理、队列与成本链路的完整诊断信息')
    ).toBeInTheDocument()
    expect(diagnostics).toHaveTextContent('已处理事件序号 42')
    expect(diagnostics).toHaveTextContent(
      `数据截至 ${formatTimestampToDate(alertMetadata.data_cutoff_at)}`
    )
    expect(diagnostics).toHaveTextContent('最早待记账成本')
    expect(diagnostics).toHaveTextContent('成本异常事件 2 条')
    expect(diagnostics.className).toContain('overflow-y-auto')
  })

  test('尚无已投影事件时摘要隐藏空闲队列告警且完整状态保留零值诊断', () => {
    const { container } = render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          data_cutoff_at: 0,
          processed_at: 0,
          event_watermark: 0,
          queue_depth: 0,
          redis_status: 'unavailable',
          redis_available: false,
          redis_consumer_running: false,
          pending_count: 0,
          writer_queue_depth: 0,
          writer_queue_capacity: 100,
          cost_queue_pending_count: 0,
          cost_stream_pending_count: 0,
          cost_stream_unread_count: 0,
          cost_outbox_pending_count: 0,
          cost_ledger_failed_count: 0,
          cost_publish_failed_count: 0,
          cost_dead_letter_count: 0,
          realtime_degraded: false,
        }}
      />
    )

    const status = container.querySelector(
      '[data-channel-monitor-realtime-status]'
    )
    expect(status).not.toBeNull()
    const summary = within(status as HTMLElement).getByRole('group', {
      name: '运行状态摘要',
    })
    const details = within(status as HTMLElement).getByRole('group', {
      name: '完整运行状态',
    })

    expect(within(summary).getByText('Redis 故障')).toBeInTheDocument()
    expect(within(summary).getByText('事件处理 已停止')).toBeInTheDocument()
    expect(
      within(summary).queryByText('实时数据已降级')
    ).not.toBeInTheDocument()
    expect(
      within(summary).queryByText(/实时事件待处理/)
    ).not.toBeInTheDocument()
    expect(within(summary).queryByText(/监控写入队列/)).not.toBeInTheDocument()
    expect(
      within(summary).queryByText(/已聚合成本待写入/)
    ).not.toBeInTheDocument()
    expect(
      within(summary).queryByText(/成本事件未读取/)
    ).not.toBeInTheDocument()
    expect(
      within(summary).queryByText(/待记入成本账本/)
    ).not.toBeInTheDocument()
    expect(
      within(summary).queryByText('成本事件排队失败')
    ).not.toBeInTheDocument()
    expect(
      within(summary).queryByText('成本账本写入失败')
    ).not.toBeInTheDocument()
    expect(
      within(summary).queryByText('成本事件进入异常队列')
    ).not.toBeInTheDocument()

    expect(details).toHaveTextContent('数据截至 暂无已处理事件')
    expect(details).toHaveTextContent('已处理事件序号 0')
    expect(details).toHaveTextContent('监控写入队列 0/100')
    expect(details).toHaveTextContent('已聚合成本待写入 0')
    expect(details).toHaveTextContent('成本事件未读取 0 / 待确认 0')
    expect(details).toHaveTextContent('待记入成本账本 0')
    expect(details).toHaveTextContent('成本账本写入失败 0 次')
    expect(details).toHaveTextContent('成本事件排队失败 0 次')
    expect(details).toHaveTextContent('成本异常事件 0 条')
    expect(
      within(details).queryByText('成本事件排队失败')
    ).not.toBeInTheDocument()
    expect(
      within(details).queryByText('成本账本写入失败')
    ).not.toBeInTheDocument()
    expect(
      within(details).queryByText('成本事件进入异常队列')
    ).not.toBeInTheDocument()
  })
})
