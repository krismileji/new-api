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
import { describe, expect, test } from 'vitest'

import type { ChannelMonitorSmartScheduleTraffic } from '../../types'
import {
  ChannelMonitorSmartScheduleTrafficTable,
  ChannelMonitorSmartScheduleSnapshotStatus,
} from '../channel-monitor-smart-schedule-traffic'

function traffic(
  channelId: number,
  attempts: number,
  success: number
): ChannelMonitorSmartScheduleTraffic {
  return {
    channel_id: channelId,
    group: 'vip',
    model: 'model-a',
    available: true,
    complete: true,
    window_start: 100,
    window_end: 200,
    coverage_start: 90,
    data_cutoff_at: 180,
    event_watermark: 10,
    attempt_count: attempts,
    final_success_count: success,
    retry_request_count: 0,
    retry_count_known: true,
    source_counts: { smart_schedule: attempts },
    logical_member_count: 0,
    rate_limit_fallback_count: 0,
  }
}

describe('实际请求分布', () => {
  test('尝试与最终成功按各自总数显示，包含备用渠道与来源', () => {
    const first = traffic(1, 2, 1)
    const second = { ...traffic(2, 1, 1), source_counts: { retry: 1 } }
    render(
      <ChannelMonitorSmartScheduleTrafficTable
        items={[first, second]}
        routes={[
          { channel_id: 1, channel_name: '主渠道' },
          { channel_id: 2, channel_name: '备用渠道' },
        ]}
        group='vip'
        model='model-a'
      />
    )
    const row = screen.getByRole('row', { name: /备用渠道/ })
    expect(within(row).getByText('33.3%')).toBeDefined()
    expect(within(row).getByText('50.0%')).toBeDefined()
    expect(within(row).getByText(/重试 1/)).toBeDefined()
  })

  test('统计不可用时显示未知，不显示百分之零', () => {
    render(
      <ChannelMonitorSmartScheduleTrafficTable
        routes={[]}
        group='vip'
        model='model-a'
      />
    )
    expect(screen.getByText('实际请求统计暂不可用')).toBeDefined()
    expect(screen.queryByText('0.0%')).toBeNull()
  })

  test('任一渠道覆盖不完整时显示计数，隐藏整体占比', () => {
    render(
      <ChannelMonitorSmartScheduleTrafficTable
        items={[traffic(1, 2, 1), { ...traffic(2, 0, 0), complete: false }]}
        routes={[]}
        group='vip'
        model='model-a'
      />
    )
    expect(screen.getByText('统计窗口不完整')).toBeDefined()
    expect(screen.queryByText('100.0%')).toBeNull()
  })
})

test('缺少路由快照时明确显示运行态不可用', () => {
  render(<ChannelMonitorSmartScheduleSnapshotStatus />)
  expect(screen.getByText('路由快照不可用')).toBeDefined()
})
