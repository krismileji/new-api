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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { assert, describe, expect, test, vi } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorItem } from '../../types'
import { ChannelMonitorChannelView } from '../channel-monitor-channel-view'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

function createChannel(): ChannelMonitorItem {
  return {
    id: 7,
    name: '测试渠道',
    type: 1,
    status: 1,
    priority: 0,
    weight: 0,
    base_url: 'https://upstream.example.com',
    models: 'test-model',
    test_model: 'test-model',
    groups: [],
    ratio: 1.25,
    previous_ratio: 1.25,
    cost_ratio: 1.25,
    previous_cost_ratio: 1.25,
    conversion_factor: 1,
    remark: '',
    channel_remark: '',
    updated_time: 1_752_777_845,
    updated_by: 1,
    updated_by_username: '',
    last_fetch_status: 'succeeded',
    last_fetch_error: '',
    last_fetch_time: 1_752_777_845,
    consecutive_failures: 0,
    upstream_balance: 42.5,
    last_balance_time: 1_752_691_445,
    last_balance_error: '',
    today_cost_cny: 0,
    today_cost_configured: true,
    today_cost_complete: true,
    today_cost_unresolved_count: 0,
    concurrency_limit: 0,
    concurrency_active: 0,
    current_rpm: 0,
    upstream: {
      type: 'new_api',
      base_url: 'https://upstream.example.com',
      group: 'default',
      auth_type: 'user',
      user_id: 7,
      has_access_token: true,
      account: '',
      has_password: false,
      single_channel_action: 'none',
      multiple_channels_action: 'none',
      balance_warning_threshold: 50,
      balance_auto_disable_threshold: 50,
      ratio_sync_enabled: false,
      balance_sync_enabled: false,
      cost_conversion: { mode: 'none' },
    },
  }
}

function renderView(
  channel: ChannelMonitorItem,
  overrides: Partial<ComponentProps<typeof ChannelMonitorChannelView>> = {}
) {
  const props = {
    channels: [channel],
    groupRatios: {},
    groupCoefficients: {},
    performanceByChannel: new Map(),
    successByChannel: new Map(),
    successMetricsAvailable: false,
    performanceRangeLabel: '24 小时',
    performanceLoading: false,
    performanceError: false,
    smartScheduleRoutesByChannel: new Map(),
    smartScheduleSelectedGroupModel: null,
    smartScheduleUpdatePending: false,
    onUpdateSmartSchedule: vi.fn(),
    onFetchUpstreamBalance: vi.fn(),
    onFetchUpstreamRatio: vi.fn(),
    onToggleStatus: vi.fn(),
    onTestConnection: vi.fn(),
    onEditConcurrency: vi.fn(),
    onEditGroups: vi.fn(),
    onConfigureUpstream: vi.fn(),
    onViewHistory: vi.fn(),
    onOpenCostHistory: vi.fn(),
    onOpenSuccessDetail: vi.fn(),
    onOpenPerformanceDetail: vi.fn(),
    fetchingBalanceChannelId: null,
    fetchingRatioChannelId: null,
    updatingStatusChannelId: null,
    ...overrides,
  }
  render(<ChannelMonitorChannelView {...props} />)
  return props
}

describe('渠道视图的同步暂停状态', () => {
  test.each([42.5, 0])('余额同步暂停时保留余额 %s 并置灰', (balance) => {
    const channel = createChannel()
    channel.upstream_balance = balance
    renderView(channel)

    const balanceCell = screen.getAllByRole('cell')[1]
    expect(within(balanceCell).getByText(String(balance))).toHaveClass(
      'text-muted-foreground'
    )
    expect(
      within(balanceCell).getByText(
        `更新：${formatTimestampToDate(channel.last_balance_time)}`
      )
    ).toBeVisible()
    expect(screen.queryByText('余额同步已关闭')).not.toBeInTheDocument()
    expect(screen.queryByText('低于预警值')).not.toBeInTheDocument()
  })

  test('余额同步暂停且尚无余额时显示暂无并允许首次手动刷新', () => {
    const channel = createChannel()
    channel.upstream_balance = null
    channel.last_balance_time = 0
    renderView(channel)

    expect(
      within(screen.getAllByRole('cell')[1]).getByText('暂无')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: '更新上游余额' })).toBeEnabled()
    expect(screen.queryByText('余额同步已关闭')).not.toBeInTheDocument()
  })

  test('余额和倍率同步暂停时点击刷新仍调用对应渠道的刷新操作', async () => {
    const channel = createChannel()
    const props = renderView(channel)
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: '更新上游余额' }))
    expect(props.onFetchUpstreamBalance).toHaveBeenCalledWith(channel)
    await user.click(screen.getByRole('button', { name: '更新上游倍率' }))
    expect(props.onFetchUpstreamRatio).toHaveBeenCalledWith(channel)
  })

  test('同步暂停时可以通过键盘手动刷新余额', async () => {
    const channel = createChannel()
    const props = renderView(channel)
    const user = userEvent.setup()

    await user.tab()
    expect(screen.getByRole('button', { name: '更新上游余额' })).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(props.onFetchUpstreamBalance).toHaveBeenCalledWith(channel)
  })

  test.each([
    { metric: 'balance', otherEnabled: true },
    { metric: 'balance', otherEnabled: false },
    { metric: 'ratio', otherEnabled: true },
    { metric: 'ratio', otherEnabled: false },
  ] as const)(
    '共享请求手动刷新 $metric 时根据另一项同步状态 $otherEnabled 显示加载中',
    ({ metric, otherEnabled }) => {
      const channel = createChannel()
      assert(channel.upstream)
      channel.upstream = {
        ...channel.upstream,
        type: 'custom',
        balance_sync_enabled: metric === 'ratio' && otherEnabled,
        ratio_sync_enabled: metric === 'balance' && otherEnabled,
        custom_config: {
          version: 1,
          ratio: { source: 'fixed', fixed_value: 1.25 },
          balance: { source: 'fixed', fixed_value: 42.5 },
          balance_reuse_ratio_request: true,
        },
      }
      renderView(channel, {
        fetchingBalanceChannelId: metric === 'balance' ? channel.id : null,
        fetchingRatioChannelId: metric === 'ratio' ? channel.id : null,
      })
      const activeLabel = metric === 'balance' ? '更新上游余额' : '更新上游倍率'
      const otherLabel = metric === 'balance' ? '更新上游倍率' : '更新上游余额'
      expect(
        within(screen.getByRole('button', { name: activeLabel })).getByRole(
          'status'
        )
      ).toBeInTheDocument()
      const otherLoading = within(
        screen.getByRole('button', { name: otherLabel })
      ).queryByRole('status')
      if (otherEnabled) {
        expect(otherLoading).toBeInTheDocument()
      } else {
        expect(otherLoading).not.toBeInTheDocument()
      }
    }
  )

  test.each(['balance', 'ratio'] as const)(
    '%s 手动刷新进行中时禁止重复提交两种刷新',
    async (metric) => {
      const channel = createChannel()
      const props = renderView(channel, {
        fetchingBalanceChannelId: metric === 'balance' ? channel.id : null,
        fetchingRatioChannelId: metric === 'ratio' ? channel.id : null,
      })
      const user = userEvent.setup()
      const balanceButton = screen.getByRole('button', { name: '更新上游余额' })
      const ratioButton = screen.getByRole('button', { name: '更新上游倍率' })

      expect(balanceButton).toBeDisabled()
      expect(ratioButton).toBeDisabled()
      await user.click(balanceButton)
      await user.click(ratioButton)
      expect(props.onFetchUpstreamBalance).not.toHaveBeenCalled()
      expect(props.onFetchUpstreamRatio).not.toHaveBeenCalled()
    }
  )

  test('未配置上游时不提供手动刷新入口', () => {
    const channel = createChannel()
    channel.upstream = null
    renderView(channel)

    expect(
      screen.queryByRole('button', { name: '更新上游余额' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: '更新上游倍率' })
    ).not.toBeInTheDocument()
  })
})

test('上游配置暂停余额同步时仅置灰阈值且重新开启后可编辑', async () => {
  const queryClient = new QueryClient()
  const channel = createChannel()
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <UpstreamConfigDialog open channel={channel} onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
  try {
    const warning = screen.getByRole('spinbutton', { name: '余额预警值' })
    const autoDisable = screen.getByRole('spinbutton', {
      name: '余额自动禁用阈值',
    })
    const sync = screen.getByRole('switch', { name: '余额同步' })
    expect(sync).not.toBeChecked()
    expect(warning).toBeDisabled()
    expect(autoDisable).toBeDisabled()
    expect(screen.queryByText(/余额同步已关闭/)).not.toBeInTheDocument()
    expect(screen.getAllByText(/仍可在渠道列表手动刷新/)).toHaveLength(2)

    await user.click(sync)
    expect(sync).toBeChecked()
    expect(warning).toBeEnabled()
    expect(autoDisable).toBeEnabled()
  } finally {
    view.unmount()
    queryClient.clear()
  }
})
