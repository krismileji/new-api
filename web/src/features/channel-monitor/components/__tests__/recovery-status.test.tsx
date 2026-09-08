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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { refetchChannelMonitorQueries } from '../../lib/query-options'
import {
  ChannelMonitorHealthStatus,
  ChannelMonitorRecoverySummary,
} from '../channel-monitor-health-status'

describe('channel monitor recovery status', () => {
  test('health status waits for manual refresh without polling', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const client = new QueryClient()
    const originalAdapter = api.defaults.adapter
    let requests = 0
    api.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        message: '',
        data: {
          status: 'degraded',
          recovery_status: 'recovering',
          node_id: 'node-a',
          checked_at: 100,
          recovered_at: 0,
          pending_count: ++requests,
          message: '正在自动恢复',
          action: '',
          data_gap_reasons: [],
        },
      },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <ChannelMonitorHealthStatus />
      </QueryClientProvider>
    )
    try {
      expect(await screen.findByText('恢复待处理 1 条')).toBeVisible()
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000)
      })
      expect(requests).toBe(1)
      await act(async () => {
        await refetchChannelMonitorQueries(client, { view: 'channels' })
      })
      expect(await screen.findByText('恢复待处理 2 条')).toBeVisible()
    } finally {
      view.unmount()
      client.clear()
      api.defaults.adapter = originalAdapter
      vi.useRealTimers()
    }
  })

  test('恢复后仍有缺口时保留历史统计不完整提示', () => {
    render(
      <ChannelMonitorRecoverySummary
        data={{
          status: 'healthy',
          recovery_status: 'data_incomplete',
          node_id: 'node-a',
          checked_at: 100,
          recovered_at: 90,
          pending_count: 0,
          message: '运行正常，部分历史统计不完整',
          action: '请复核丢弃或隔离记录。',
          data_gap_reasons: ['samples_dropped'],
        }}
      />
    )
    expect(
      screen.getByRole('status', { name: '监控恢复状态' })
    ).toHaveTextContent('监控运行正常')
    const history = screen.getByRole('list', { name: '监控历史提示' })
    expect(history).toHaveTextContent('部分历史统计不完整')
    expect(history).toHaveTextContent('请复核丢弃或隔离记录。')
    expect(
      screen.queryByRole('list', { name: '监控异常提示' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText('数据已全部补齐')).not.toBeInTheDocument()
  })

  test('状态请求失败时不继续显示缓存中的正常状态', () => {
    render(
      <ChannelMonitorRecoverySummary
        failed
        data={{
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
    expect(screen.getByText('监控状态暂不可用')).toBeVisible()
    expect(screen.queryByText('运行已恢复')).not.toBeInTheDocument()
  })

  test('补偿进行中展示待处理数量和邮件投递错误', () => {
    render(
      <ChannelMonitorRecoverySummary
        data={{
          status: 'degraded',
          recovery_status: 'recovering',
          node_id: 'node-a',
          checked_at: 100,
          recovered_at: 0,
          pending_count: 128,
          message: '正在自动恢复',
          action: '后台正在处理积压事件。',
          data_gap_reasons: [],
          notification_error: '邮件发送失败，将自动重试',
        }}
      />
    )
    expect(screen.getByText('正在自动恢复')).toBeVisible()
    expect(screen.getByText('恢复待处理 128 条')).toBeVisible()
    expect(screen.getByText('邮件发送失败，将自动重试')).toBeVisible()
  })

  test('首次加载不提前宣告正常', () => {
    render(<ChannelMonitorRecoverySummary />)
    expect(screen.getByText('监控状态检查中')).toBeVisible()
  })

  test('恢复信息并入同一个居中详情弹窗，保留节点和时间记录', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorRecoverySummary
        data={{
          status: 'healthy',
          recovery_status: 'recovered',
          node_id: 'monitor-node-with-a-long-instance-name-1234567890',
          checked_at: 1_788_856_200,
          recovered_at: 1_788_856_100,
          pending_count: 0,
          message: '运行已恢复',
          action: '',
          data_gap_reasons: [],
        }}
      />
    )
    expect(screen.getByText('运行已恢复')).toBeVisible()
    expect(screen.queryByText(/节点：/)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: '恢复详情' })
    ).not.toBeInTheDocument()
    const trigger = screen.getByRole('button', { name: '运行详情' })
    await user.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: '监控运行详情' })
    expect(
      screen.getByText(/monitor-node-with-a-long-instance-name/)
    ).toBeVisible()
    expect(screen.getByText(/检查于/)).toBeVisible()
    expect(
      within(dialog).getByRole('group', { name: '最近恢复' })
    ).not.toHaveTextContent('未提供')
  })
})
