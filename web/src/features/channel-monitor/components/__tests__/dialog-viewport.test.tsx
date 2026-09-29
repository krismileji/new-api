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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { channelLimitGroupsKey } from '../../api-limit-groups'
import type { ChannelMonitorItem, ChannelProbePolicy } from '../../types'
import type { ChannelPassiveView } from '../../types-passive'
import { ChannelLimitGroupsDialog } from '../channel-limit-groups-dialog'
import { ChannelModelDetectionRunDialog } from '../channel-model-detection-run-dialog'
import { ChannelMonitorOrderDialog } from '../channel-monitor-order-dialog'
import { ChannelPassiveMonitorPanel } from '../channel-passive-monitor-panel'
import { ChannelProbePolicyDialog } from '../channel-probe-policy-dialog'

let client: QueryClient

afterEach(() => {
  cleanup()
  client.clear()
})

test.each(['排序', '共享限流', '手动检测', '探测策略', '业务历史'])(
  '%s 弹窗保留安全区域高度限制和外层滚动，关闭按钮可通过键盘使用',
  async (kind) => {
    client = new QueryClient({
      defaultOptions: {
        queries: { enabled: false, retry: false, staleTime: Infinity },
      },
    })
    client.setQueryData(channelLimitGroupsKey, [])
    client.setQueryData<ChannelProbePolicy>(
      ['channel-monitor', 'probe-policy', 17],
      {
        auto_probe_disabled: false,
        small_input_response_enabled: false,
        small_input_threshold_tokens: 1000,
        small_input_response_text: '',
        probe_policy_revision: 1,
        probe_policy_updated_at: 1,
      }
    )
    const onOpenChange = vi.fn()
    let content = (
      <ChannelMonitorOrderDialog
        open
        channels={[]}
        channelOrder={[]}
        onOpenChange={onOpenChange}
      />
    )
    if (kind === '共享限流') {
      content = (
        <ChannelLimitGroupsDialog channels={[]} onOpenChange={onOpenChange} />
      )
    } else if (kind === '手动检测') {
      content = (
        <ChannelModelDetectionRunDialog
          open
          channel={null}
          onOpenChange={onOpenChange}
        />
      )
    } else if (kind === '探测策略') {
      content = (
        <ChannelProbePolicyDialog
          open
          channel={{ id: 17 } as ChannelMonitorItem}
          onOpenChange={onOpenChange}
        />
      )
    } else if (kind === '业务历史') {
      const item: ChannelPassiveView = {
        target: {
          id: 'target-17',
          scope: 'status',
          channel_id: 17,
          model_name: 'test-model',
          interval_seconds: 60,
          config_revision: 1,
          effective_at: 1,
        },
        periods: [],
      }
      client.setQueryData(
        [
          'channel-monitor',
          'passive',
          'status',
          undefined,
          undefined,
          undefined,
          1,
        ],
        { items: [item], total: 1, page: 1, server_now: 60 }
      )
      content = <ChannelPassiveMonitorPanel scope='status' />
    }
    render(
      <QueryClientProvider client={client}>{content}</QueryClientProvider>
    )
    const user = userEvent.setup()
    if (kind === '业务历史') {
      await user.click(screen.getByRole('button', { name: '查看历史汇总' }))
    }
    const dialog = screen.getByRole('dialog')
    // Share the primitive's safe-area bound and dvh fallback. Keep an outer
    // scroll path when the header and actions alone exceed that available height.
    expect(dialog).toHaveClass(
      'max-h-[var(--dialog-available-height,calc(100dvh-2rem))]',
      'top-[var(--dialog-viewport-center,50dvh)]',
      'overflow-y-auto'
    )
    expect(dialog).not.toHaveClass('overflow-hidden')
    screen.getByRole('button', { name: 'Close' }).focus()
    await user.keyboard('{Enter}')
    if (kind === '业务历史') {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    } else {
      expect(onOpenChange).toHaveBeenCalledOnce()
      expect(onOpenChange.mock.calls[0][0]).toBe(false)
    }
  }
)
