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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import { mergeChannelMonitorRealtimeMetadata } from '../../lib/realtime-metadata'
import type { ChannelMonitorRealtimeMetadata } from '../../types'
import type { ChannelMonitorRecovery } from '../../types-recovery'
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

const healthyMetadata: ChannelMonitorRealtimeMetadata = {
  generated_at: alertMetadata.generated_at,
  data_cutoff_at: alertMetadata.data_cutoff_at,
  processed_at: alertMetadata.processed_at,
  event_watermark: 42,
  queue_depth: 0,
  redis_available: true,
  redis_consumer_running: true,
  pending_count: 0,
  consumer_lag_seconds: 0,
  cost_projection: {
    checked_at: alertMetadata.generated_at,
    pending: false,
    failed: false,
  },
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
}

const recoveredWithGaps: ChannelMonitorRecovery = {
  status: 'healthy',
  recovery_status: 'data_incomplete',
  node_id: 'node-a',
  checked_at: alertMetadata.generated_at,
  recovered_at: 1_752_777_800,
  pending_count: 1,
  message: '运行正常，部分历史统计不完整',
  action: '请复核丢弃或隔离记录；运行恢复不代表历史数据已补齐。',
  data_gap_reasons: ['daily_replay_incomplete', 'events_quarantined'],
}

describe('channel monitor realtime status', () => {
  test('运行恢复后历史缺口与正常成本批次不再触发运行警告，诊断记录仍可查看', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...healthyMetadata,
          cost_outbox_pending_count: 1,
          cost_projection: {
            checked_at: recoveredWithGaps.checked_at,
            pending: true,
            failed: false,
          },
          degraded_reasons: [
            'daily_replay_incomplete',
            'cost_projection_pending',
          ],
          realtime_degraded: true,
          retry_count: 12532,
          takeover_count: 130,
          quarantine_count: 252,
          marker_release_failure_count: 161,
          marker_release_failure_active: false,
        }}
        recovery={recoveredWithGaps}
      />
    )
    const status = screen.getByRole('status', { name: '监控恢复状态' })
    expect(within(status).getByText('监控运行正常')).toHaveAttribute(
      'data-variant',
      'outline'
    )
    expect(
      screen.queryByRole('list', { name: '监控异常提示' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('list', { name: '监控历史提示' })
    ).toHaveTextContent('部分历史统计不完整')
    expect(screen.queryByText('恢复待处理 1 条')).not.toBeInTheDocument()
    expect(screen.getByRole('group', { name: '成本汇总' })).toHaveTextContent(
      '更新中'
    )
    expect(screen.getByText('待记账 1 条')).toBeVisible()
    await user.click(screen.getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: '历史记录' })
    ).toHaveTextContent('日统计恢复不完整、存在历史隔离记录')
    expect(
      within(dialog).getByRole('group', { name: '异常隔离（累计）' })
    ).toHaveTextContent('252 条')
    expect(
      within(dialog).getByRole('group', { name: '标记清理失败（累计）' })
    ).toHaveTextContent('161 次')
    expect(
      within(dialog).getByRole('group', { name: '处理建议' })
    ).toHaveTextContent(recoveredWithGaps.action)
  })

  test('后台确认成本处理持续积压时继续显示运行警告', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...healthyMetadata,
          degraded_reasons: ['cost_projection_pending'],
          realtime_degraded: true,
        }}
        recovery={{
          ...recoveredWithGaps,
          status: 'degraded',
          recovery_status: 'recovering',
          message: '正在自动恢复',
          action: '后台正在处理积压事件。',
        }}
      />
    )
    expect(screen.getByText('正在自动恢复')).toHaveAttribute(
      'data-variant',
      'warning'
    )
    expect(
      screen.getByRole('list', { name: '监控异常提示' })
    ).toHaveTextContent('成本汇总更新中')
    expect(screen.getByText('恢复待处理 1 条')).toBeVisible()
  })

  test('未说明原因的数据降级不能被已恢复状态覆盖', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{ ...healthyMetadata, realtime_degraded: true }}
        recovery={recoveredWithGaps}
      />
    )
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).toHaveTextContent('监控数据不完整')
    expect(screen.getByRole('list', { name: '监控异常提示' })).toBeVisible()
  })

  test('多接口合并后历史缺口不能掩盖另一个接口未说明原因的数据降级', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={mergeChannelMonitorRealtimeMetadata([
          { ...healthyMetadata, realtime_degraded: true },
          {
            ...healthyMetadata,
            realtime_degraded: true,
            degraded_reasons: ['daily_replay_incomplete'],
          },
        ])}
        recovery={recoveredWithGaps}
      />
    )
    const status = screen.getByRole('status', { name: '监控恢复状态' })
    expect(within(status).getByText('监控数据不完整')).toHaveAttribute(
      'data-variant',
      'warning'
    )
    expect(
      screen.getByRole('list', { name: '监控历史提示' })
    ).toHaveTextContent('部分历史统计不完整')
  })

  test('顶部保留关键指标和当前故障，详细队列与历史次数不占用摘要', () => {
    render(<ChannelMonitorRealtimeStatus metadata={alertMetadata} />)
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(within(summary).getByText('监控异常')).toBeVisible()
    expect(
      within(summary).getByRole('group', { name: '数据截至' })
    ).toHaveTextContent(formatTimestampToDate(alertMetadata.data_cutoff_at))
    expect(
      within(summary).getByRole('group', { name: '处理延迟' })
    ).toHaveTextContent('45 秒')
    expect(
      within(summary).getByRole('group', { name: '事件待处理' })
    ).toHaveTextContent('6 条')
    expect(
      within(summary).getByRole('group', { name: '成本汇总' })
    ).toHaveTextContent('更新中')
    expect(within(summary).getByText('待记账 5 条')).toBeVisible()
    expect(within(summary).getByText('Redis 故障')).toBeVisible()
    expect(within(summary).getByText('事件处理已停止')).toBeVisible()
    expect(within(summary).getByText('监控数据不完整')).toBeVisible()
    expect(within(summary).getByText('成本异常事件 2 条')).toBeVisible()
    expect(within(summary).getByText('事件标记清理故障')).toBeVisible()
    expect(within(summary).getByText('实时事件清理故障')).toBeVisible()
    expect(summary).not.toHaveTextContent(
      /已处理事件序号|监控写入队列|成本事件未读取|累计|查询于/
    )
  })

  test('正常状态仍显示零值，历史失败不当作当前故障', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...healthyMetadata,
          cost_publish_failed_count: 2,
          cost_ledger_failed_count: 3,
        }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(within(summary).getByText('监控正常')).toBeVisible()
    expect(
      within(summary).getByRole('group', { name: '处理延迟' })
    ).toHaveTextContent('0 秒')
    expect(
      within(summary).getByRole('group', { name: '事件待处理' })
    ).toHaveTextContent('0 条')
    expect(
      within(summary).getByRole('group', { name: '成本汇总' })
    ).toHaveTextContent('已更新')
    expect(summary).not.toHaveTextContent(/Redis|累计|失败/)
    await user.click(within(summary).getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: 'Redis' })
    ).toHaveTextContent('正常')
    expect(
      within(dialog).getByRole('group', { name: '事件处理' })
    ).toHaveTextContent('运行中')
    expect(
      within(dialog).getByRole('group', { name: '事件排队失败（累计）' })
    ).toHaveTextContent('2 次')
    expect(
      within(dialog).getByRole('group', { name: '账本写入失败（累计）' })
    ).toHaveTextContent('3 次')
  })

  test('键盘打开居中弹窗后可读取三组完整数据，关闭后焦点回到入口', async () => {
    const user = userEvent.setup()
    render(<ChannelMonitorRealtimeStatus metadata={alertMetadata} />)
    const trigger = screen.getByRole('button', { name: '运行详情' })
    await user.tab()
    expect(trigger).toHaveFocus()
    await user.keyboard('{Enter}')
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(dialog).not.toHaveAttribute('data-side', 'bottom')
    const diagnostics = within(dialog).getByRole('region', {
      name: '实时运行完整诊断',
    })
    const events = within(diagnostics).getByRole('region', { name: '实时事件' })
    const cost = within(diagnostics).getByRole('region', { name: '成本处理' })
    const history = within(diagnostics).getByRole('region', {
      name: '恢复与历史',
    })
    expect(
      within(events).getByRole('group', { name: '已处理事件序号' })
    ).toHaveTextContent('42')
    expect(
      within(events).getByRole('group', { name: '数据截至' })
    ).toHaveTextContent(formatTimestampToDate(alertMetadata.data_cutoff_at))
    expect(
      within(events).getByRole('group', { name: '监控写入队列' })
    ).toHaveTextContent('2 / 100')
    expect(
      within(events).getByRole('group', { name: '写入等待' })
    ).toHaveTextContent('9 秒')
    expect(
      within(cost).getByRole('group', { name: '已聚合待写入' })
    ).toHaveTextContent('3 条')
    expect(
      within(cost).getByRole('group', { name: '事件未读取' })
    ).toHaveTextContent('4 条')
    expect(
      within(cost).getByRole('group', { name: '事件待确认' })
    ).toHaveTextContent('2 条')
    expect(
      within(cost).getByRole('group', { name: '待记入成本账本' })
    ).toHaveTextContent('5 条')
    expect(
      within(cost).getByRole('group', { name: '账本写入重试' })
    ).toHaveTextContent('7 次')
    expect(
      within(cost).getByRole('group', { name: '成本异常事件' })
    ).toHaveTextContent('2 条')
    expect(
      within(history).getByRole('group', { name: '事件处理重试（累计）' })
    ).toHaveTextContent('3 次')
    expect(
      within(history).getByRole('group', { name: '自动接管（累计）' })
    ).toHaveTextContent('2 次')
    expect(
      within(history).getByRole('group', { name: '异常隔离（累计）' })
    ).toHaveTextContent('1 条')
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    await waitFor(() => expect(trigger).toHaveFocus())
  })

  test('尚无事件与真实零值保持可区分', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...healthyMetadata,
          data_cutoff_at: 0,
          event_watermark: 0,
          processed_at: 0,
        }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(
      within(summary).getByRole('group', { name: '数据截至' })
    ).toHaveTextContent('暂无已处理事件')
    await user.click(within(summary).getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: '已处理事件序号' })
    ).toHaveTextContent(/已处理事件序号\s*0$/)
    expect(
      within(dialog).getByRole('group', { name: '监控写入队列' })
    ).toHaveTextContent('0 / 100')
    expect(
      within(dialog).getByRole('group', { name: '事件未读取' })
    ).toHaveTextContent('0 条')
    expect(
      within(dialog).getByRole('group', { name: '事件待确认' })
    ).toHaveTextContent('0 条')
    expect(
      within(dialog).getByRole('group', { name: '待记入成本账本' })
    ).toHaveTextContent('0 条')
    expect(
      within(dialog).getByRole('group', { name: '账本写入失败（累计）' })
    ).toHaveTextContent('0 次')
    expect(
      within(dialog).getByRole('group', { name: '数据处理时间' })
    ).toHaveTextContent('暂无')
  })

  test('可选指标缺失时显示未提供，不宣告正常或用零值代替', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          data_cutoff_at: 0,
          processed_at: 0,
          event_watermark: 0,
          queue_depth: 0,
          realtime_degraded: false,
        }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(within(summary).getByText('监控状态未确认')).toBeVisible()
    expect(
      within(summary).getByRole('group', { name: '处理延迟' })
    ).toHaveTextContent('未提供')
    expect(
      within(summary).getByRole('group', { name: '成本汇总' })
    ).toHaveTextContent('未提供')
    await user.click(within(summary).getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: 'Redis' })
    ).toHaveTextContent('未提供')
    expect(
      within(dialog).getByRole('group', { name: '事件处理' })
    ).toHaveTextContent('未提供')
    expect(
      within(dialog).getByRole('group', { name: '监控写入重试' })
    ).toHaveTextContent('未提供')
    expect(
      within(dialog).getByRole('group', { name: '通知错误' })
    ).toHaveTextContent('未提供')
  })

  test.each([
    { cost_projection: { checked_at: 1, pending: true, failed: true } },
    {
      degraded_reasons: [
        'cost_projection_unavailable',
        'cost_projection_pending',
      ] as const,
    },
  ])('成本汇总失败时不被更新中状态覆盖：%j', (metadata) => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...alertMetadata,
          ...metadata,
          degraded_reasons: [...(metadata.degraded_reasons ?? [])],
        }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(
      within(summary).getByRole('group', { name: '成本汇总' })
    ).toHaveTextContent('暂不可用')
    expect(summary).not.toHaveTextContent('成本汇总更新中')
  })

  test('成本队列为空但没有汇总检查结果时不宣告已更新', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{ ...healthyMetadata, cost_projection: undefined }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(
      within(summary).getByRole('group', { name: '成本汇总' })
    ).toHaveTextContent('无待处理')
  })

  test('已恢复但实时链路再次故障时优先展示当前故障', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={alertMetadata}
        recovery={{
          status: 'healthy',
          recovery_status: 'recovered',
          node_id: 'node-a',
          checked_at: 100,
          recovered_at: 90,
          pending_count: 0,
          message: '运行已恢复',
          action: '',
          data_gap_reasons: [],
        }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(within(summary).getByText('监控异常')).toBeVisible()
    expect(within(summary).getByText('Redis 故障')).toBeVisible()
    expect(summary).not.toHaveTextContent('运行已恢复')
  })

  test('当前故障不会遮住已确认的历史统计缺口', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={alertMetadata}
        recovery={{
          status: 'healthy',
          recovery_status: 'data_incomplete',
          node_id: 'node-a',
          checked_at: 100,
          recovered_at: 90,
          pending_count: 0,
          message: '运行正常，部分历史统计不完整',
          action: '',
          data_gap_reasons: ['samples_dropped'],
        }}
      />
    )
    const summary = screen.getByRole('group', { name: '运行状态摘要' })
    expect(within(summary).getByText('监控异常')).toBeVisible()
    expect(within(summary).getByText('部分历史统计不完整')).toBeVisible()
  })

  test('没有元数据时不展示占位诊断', () => {
    const { container } = render(
      <ChannelMonitorRealtimeStatus metadata={undefined} />
    )
    expect(container).toBeEmptyDOMElement()
  })
})
