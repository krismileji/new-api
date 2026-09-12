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
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import type { ChannelMonitorRealtimeMetadata } from '../../types'
import type { ChannelMonitorRecovery } from '../../types-recovery'
import { ChannelMonitorRealtimeStatus } from '../channel-monitor-realtime-status'

const checkedAt = Date.parse('2026-09-08T23:29:49+08:00') / 1000
const metadata: ChannelMonitorRealtimeMetadata = {
  generated_at: checkedAt,
  data_cutoff_at: checkedAt - 1,
  processed_at: checkedAt,
  event_watermark: 42,
  queue_depth: 0,
  redis_available: true,
  redis_consumer_running: true,
  quarantine_count: 252,
  last_quarantined_at: 1788877979,
  writer_dropped_events: 0,
  cost_publish_failed_count: 0,
  cost_dead_letter_count: 0,
  degraded_reasons: ['daily_replay_incomplete'],
  realtime_degraded: true,
}
const recovery: ChannelMonitorRecovery = {
  status: 'healthy',
  recovery_status: 'data_incomplete',
  node_id: 'node-a',
  checked_at: checkedAt,
  recovered_at: checkedAt - 3600,
  pending_count: 0,
  message: '运行正常，部分历史统计不完整',
  action: '请复核丢弃或隔离记录。',
  data_gap_reasons: ['daily_replay_incomplete', 'events_quarantined'],
}

beforeEach(() => {
  localStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
})

afterEach(() => {
  localStorage.clear()
  useAuthStore.getState().auth.reset()
})

describe('channel monitor history acknowledgment', () => {
  test('已知晓只收起历史提示，详情中的隔离数量和缺口仍保留', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )

    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))

    expect(
      screen.queryByRole('list', { name: '监控历史提示' })
    ).not.toBeInTheDocument()
    expect(screen.getByText('历史提示已知晓')).toBeVisible()
    expect(
      screen.getByRole('button', { name: '重新显示历史提示' })
    ).toHaveFocus()
    await user.click(screen.getByRole('button', { name: '运行详情' }))
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      within(dialog).getByRole('group', { name: '异常隔离（累计）' })
    ).toHaveTextContent('252 条')
    expect(
      within(dialog).getByRole('group', { name: '历史记录' })
    ).toHaveTextContent('日统计恢复不完整、存在历史隔离记录')
  })

  test('同一批历史在刷新及重新进入后保持收起，并可主动重新显示', async () => {
    const user = userEvent.setup()
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))
    view.unmount()

    render(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...metadata,
          generated_at: checkedAt + 60,
          event_watermark: 60,
        }}
        recovery={{
          ...recovery,
          checked_at: checkedAt + 60,
          data_gap_reasons: [...recovery.data_gap_reasons].reverse(),
        }}
      />
    )
    expect(
      screen.queryByRole('list', { name: '监控历史提示' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重新显示历史提示' }))
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
  })

  test.each([
    { quarantine_count: 253, last_quarantined_at: checkedAt + 1 },
    { writer_dropped_events: 1 },
    { cost_publish_failed_count: 1 },
    { quarantine_count: 1, last_quarantined_at: checkedAt + 1 },
  ])('新增异常或计数重置后重新提示：%j', async (change) => {
    const user = userEvent.setup()
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))

    view.rerender(
      <ChannelMonitorRealtimeStatus
        metadata={{ ...metadata, ...change }}
        recovery={recovery}
      />
    )
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
  })

  test('没有新增缺口的再次运行恢复不会要求重复确认历史提示', async () => {
    const user = userEvent.setup()
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))
    view.rerender(
      <ChannelMonitorRealtimeStatus
        metadata={metadata}
        recovery={{ ...recovery, recovered_at: recovery.recovered_at + 300 }}
      />
    )
    expect(
      screen.queryByRole('list', { name: '监控历史提示' })
    ).not.toBeInTheDocument()
    expect(screen.getByText('历史提示已知晓')).toBeVisible()
  })

  test('升级后沿用已知晓的历史记录，新增隔离仍重新提示', () => {
    localStorage.setItem(
      'channel-monitor:history-acknowledgment:v1:[1,"node-a"]',
      JSON.stringify({
        fingerprint: JSON.stringify([
          [...recovery.data_gap_reasons].sort(),
          recovery.recovered_at,
          metadata.quarantine_count,
          metadata.last_quarantined_at,
          metadata.writer_dropped_events,
          metadata.cost_publish_failed_count,
          metadata.cost_dead_letter_count,
        ]),
        dailyGapDay: Math.floor((checkedAt + 8 * 3600) / 86400),
      })
    )
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    expect(screen.getByText('历史提示已知晓')).toBeVisible()
    view.rerender(
      <ChannelMonitorRealtimeStatus
        metadata={{ ...metadata, quarantine_count: 253 }}
        recovery={recovery}
      />
    )
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
  })

  test('新的一天没有新增缺口时保持收起，新增日统计缺口时重新提示', async () => {
    const user = userEvent.setup()
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))
    const nextDay = { ...metadata, generated_at: checkedAt + 3600 }

    view.rerender(
      <ChannelMonitorRealtimeStatus
        metadata={{
          ...nextDay,
          degraded_reasons: [],
          realtime_degraded: false,
        }}
        recovery={recovery}
      />
    )
    expect(
      screen.queryByRole('list', { name: '监控历史提示' })
    ).not.toBeInTheDocument()
    view.rerender(
      <ChannelMonitorRealtimeStatus metadata={nextDay} recovery={recovery} />
    )
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
  })

  test('已知晓不会屏蔽后续的 Redis 故障', async () => {
    const user = userEvent.setup()
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))

    view.rerender(
      <ChannelMonitorRealtimeStatus
        metadata={{ ...metadata, redis_available: false }}
        recovery={recovery}
      />
    )
    expect(
      screen.getByRole('list', { name: '监控异常提示' })
    ).toHaveTextContent('Redis 故障')
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).toHaveTextContent('监控异常')
  })

  test('已知晓记录按当前账号及节点隔离', async () => {
    const user = userEvent.setup()
    const view = render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )
    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))

    act(() =>
      useAuthStore
        .getState()
        .auth.setUser({ id: 2, username: 'root-2', role: 100 })
    )
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
    act(() =>
      useAuthStore
        .getState()
        .auth.setUser({ id: 1, username: 'root', role: 100 })
    )
    expect(
      screen.queryByRole('list', { name: '监控历史提示' })
    ).not.toBeInTheDocument()
    view.rerender(
      <ChannelMonitorRealtimeStatus
        metadata={metadata}
        recovery={{ ...recovery, node_id: 'node-b' }}
      />
    )
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
  })

  test('保存失败时保留提示并显示错误', async () => {
    const user = userEvent.setup()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage unavailable')
    })
    render(
      <ChannelMonitorRealtimeStatus metadata={metadata} recovery={recovery} />
    )

    await user.click(screen.getByRole('button', { name: '已知晓本次历史缺口' }))
    expect(screen.getByRole('list', { name: '监控历史提示' })).toBeVisible()
    expect(screen.getByRole('alert')).toHaveTextContent('无法保存已知晓状态')
  })

  test('诊断数据缺失时不允许确认历史缺口', () => {
    render(<ChannelMonitorRealtimeStatus recovery={recovery} />)
    expect(
      screen.getByRole('button', { name: '已知晓本次历史缺口' })
    ).toBeDisabled()
  })
})
