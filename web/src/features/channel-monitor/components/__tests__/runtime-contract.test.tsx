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
