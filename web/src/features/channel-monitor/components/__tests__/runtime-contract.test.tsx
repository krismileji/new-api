import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import type { ChannelMonitorRealtimeMetadata } from '../../types'
import type { ChannelMonitorRecovery } from '../../types-recovery'
import { ChannelMonitorRealtimeStatus } from '../channel-monitor-realtime-status'

const recovery: ChannelMonitorRecovery = {
  status: 'healthy',
  recovery_status: 'data_incomplete',
  node_id: 'node-a',
  checked_at: 100,
  recovered_at: 90,
  pending_count: 0,
  message: '运行正常，存在历史隔离记录',
  action: '当前事件由后台自动处理。',
  data_gap_reasons: ['events_quarantined'],
}
const metadata: ChannelMonitorRealtimeMetadata = {
  generated_at: 100,
  data_cutoff_at: 100,
  processed_at: 100,
  event_watermark: 1,
  queue_depth: 0,
  redis_available: true,
  redis_consumer_running: true,
  pending_count: 0,
  consumer_lag_seconds: 0,
  realtime_degraded: false,
  degraded_reasons: [],
  quarantine_count: 2604,
}

describe('后台恢复状态与页面摘要一致', () => {
  test('运行正常时累计诊断默认收起，键盘展开后保留计数与含义', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...metadata,
          retry_count: 131365,
          takeover_count: 130,
          marker_release_failure_count: 162,
          marker_release_failure_active: false,
          stream_trim_failure_count: 0,
          stream_trim_failure_active: false,
        }}
        recovery={{ ...recovery, pending_count: 1 }}
      />
    )
    await user.click(screen.getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: '恢复状态' })
    ).toHaveTextContent('监控运行正常')
    expect(
      within(dialog).getByRole('group', { name: '当前异常' })
    ).toHaveTextContent('无')
    expect(
      within(dialog).getByRole('group', { name: '后台待处理' })
    ).toHaveTextContent('1 条')
    expect(
      within(dialog).getByRole('group', { name: '事件标记清理' })
    ).toHaveTextContent('正常')
    expect(
      within(dialog).queryByRole('group', { name: '异常隔离（累计）' })
    ).not.toBeInTheDocument()
    const historyTrigger = within(dialog).getByRole('button', {
      name: '历史诊断（累计）',
    })
    expect(historyTrigger).toHaveAttribute('aria-expanded', 'false')
    historyTrigger.focus()
    await user.keyboard('{Enter}')
    expect(historyTrigger).toHaveAttribute('aria-expanded', 'true')
    expect(
      within(dialog).getByRole('group', { name: '事件处理重试（累计）' })
    ).toHaveTextContent('131,365 次')
    expect(
      within(dialog).getByRole('group', { name: '异常隔离（累计）' })
    ).toHaveTextContent('2,604 条')
    expect(
      within(dialog).getByText(/没有新增重试、接管或故障时保持不变/)
    ).toBeVisible()
    expect(within(dialog).getByText(/同一事件可多次重试/)).toBeVisible()
    await user.keyboard(' ')
    expect(historyTrigger).toHaveAttribute('aria-expanded', 'false')
    expect(
      within(dialog).queryByRole('group', { name: '事件处理重试（累计）' })
    ).not.toBeInTheDocument()
  })

  test('仅有历史隔离时不宣称统计丢失', () => {
    render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    expect(
      screen.getByRole('list', { name: '监控历史提示' })
    ).toHaveTextContent('存在历史隔离记录')
    expect(screen.queryByText('部分历史统计不完整')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('list', { name: '监控异常提示' })
    ).not.toBeInTheDocument()
  })

  test('后台确认正常的短暂队列不被重新标记为数据不完整', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...metadata,
          pending_count: 3,
          consumer_lag_seconds: 1,
          realtime_degraded: true,
          degraded_reasons: ['event_backlog'],
        }}
        recovery={{ ...recovery, pending_count: 3 }}
      />
    )
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).toHaveTextContent('监控运行正常')
    expect(
      screen.queryByRole('list', { name: '监控异常提示' })
    ).not.toBeInTheDocument()
    expect(screen.getByText('1 秒')).not.toHaveClass('text-warning')
  })

  test('新的超时积压仍覆盖先前正常的恢复结果', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...metadata,
          pending_count: 3,
          consumer_lag_seconds: 45,
          realtime_degraded: true,
          degraded_reasons: ['event_backlog'],
        }}
        recovery={recovery}
      />
    )
    expect(
      screen.getByRole('list', { name: '监控异常提示' })
    ).toHaveTextContent('实时事件存在积压')
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).not.toHaveTextContent('监控运行正常')
  })

  test('历史失败计数不覆盖已恢复状态', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...metadata,
          writer_dropped_events: 2,
          cost_publish_failed_count: 3,
          cost_dead_letter_count: 4,
          realtime_degraded: true,
          degraded_reasons: [
            'writer_queue_full',
            'cost_publish_failure',
            'cost_dead_letter',
          ],
        }}
        recovery={{
          ...recovery,
          dropped_sample_count: 2,
          cost_publish_failed_count: 3,
          cost_dead_letter_count: 4,
          data_gap_reasons: [
            'samples_dropped',
            'cost_publish_failure',
            'cost_dead_letter',
          ],
        }}
      />
    )
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).toHaveTextContent('监控运行正常')
    expect(
      screen.queryByRole('list', { name: '监控异常提示' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('list', { name: '监控历史提示' })
    ).toHaveTextContent('部分历史统计不完整')
  })

  test('新增隔离不能被尚未更新的正常健康结果遮盖', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{ ...metadata, quarantine_count: 2605 }}
        recovery={{ ...recovery, quarantine_count: 2604 }}
      />
    )
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).toHaveTextContent('监控异常')
    expect(
      screen.getByRole('list', { name: '监控异常提示' })
    ).toHaveTextContent('新增监控事件隔离')
    await user.click(screen.getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: '恢复状态' })
    ).toHaveTextContent('监控异常')
    expect(
      within(dialog).getByRole('group', { name: '当前异常' })
    ).toHaveTextContent('新增监控事件隔离')
    expect(
      within(dialog).queryByRole('group', { name: '异常隔离（累计）' })
    ).not.toBeInTheDocument()
    await user.click(
      within(dialog).getByRole('button', { name: '历史诊断（累计）' })
    )
    expect(
      within(dialog).getByRole('group', { name: '历史记录' })
    ).toHaveTextContent('存在历史隔离记录')
    expect(
      within(dialog).getByRole('group', { name: '异常隔离（累计）' })
    ).toHaveTextContent('2,605 条')
  })

  test('过期的正常观测不能掩盖后来产生的积压', () => {
    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...metadata,
          generated_at: 140,
          realtime_degraded: true,
          degraded_reasons: ['event_backlog'],
          pending_count: 3,
        }}
        recovery={recovery}
      />
    )
    expect(
      screen.getByRole('list', { name: '监控异常提示' })
    ).toHaveTextContent('实时事件存在积压')
  })
})
