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
import type { ComponentProps } from 'react'
import { expect, test, vi } from 'vitest'

import { ChannelMonitorSmartScheduleOverview } from '../channel-monitor-smart-schedule-overview'

function createProps(): ComponentProps<
  typeof ChannelMonitorSmartScheduleOverview
> {
  return {
    enabled: true,
    summary: {
      routeCount: 6,
      participatingCount: 5,
      activeCount: 4,
      pausedCount: 0,
      channelCount: 3,
      groupCount: 2,
      poolCount: 3,
      healthyPoolCount: 2,
      lastScheduleTime: 1_752_777_800,
      degradedCount: 1,
      probingCount: 0,
      insufficientSampleCount: 0,
      failedCount: 0,
    },
    snapshot: {
      available: true,
      revision: 7,
      generated_at: 1_752_777_845,
      source_watermark: 7,
      snapshot_age_seconds: 0,
      max_age_seconds: 300,
      redis_backed: true,
      dirty: false,
      stale: false,
      degraded: false,
      protection_mode: false,
      last_redis_success_at: 1_752_777_845,
      last_redis_failure_at: 0,
    },
    refreshFailed: false,
    stale: false,
    runDisabled: false,
    running: false,
    onOpenHistory: vi.fn(),
    onOpenSettings: vi.fn(),
    onRun: vi.fn(),
  }
}

test.each(['missing', 'unavailable', 'stale', 'protection'] as const)(
  'when the route snapshot is %s, current availability is unknown',
  (state) => {
    const props = createProps()
    if (state === 'missing') {
      props.snapshot = undefined
    } else if (props.snapshot) {
      props.snapshot.available = state !== 'unavailable'
      props.snapshot.stale = state === 'stale'
      props.snapshot.protection_mode = state === 'protection'
    }
    render(<ChannelMonitorSmartScheduleOverview {...props} />)

    const availability = within(
      screen.getByRole('group', { name: '当前可调度' })
    )
    expect(availability.getByText('未知')).toBeVisible()
    expect(availability.queryByText('4')).not.toBeInTheDocument()
    expect(screen.getByRole('group', { name: '参与路由' })).toHaveTextContent(
      '5/6'
    )
  }
)

test('when scheduling is disabled, hide previous runtime metrics and keep settings accessible', () => {
  const props = createProps()
  props.enabled = false
  props.stale = true
  render(<ChannelMonitorSmartScheduleOverview {...props} />)

  expect(screen.getByText('已禁用')).toBeVisible()
  expect(screen.queryByRole('group', { name: '参与路由' })).toBeNull()
  expect(screen.queryByText('路由已生效')).not.toBeInTheDocument()
  expect(screen.queryByText('调度数据可能已过期')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '立即调度' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '调度设置' })).toBeEnabled()
})

test.each(['running', 'refresh-failed'] as const)(
  'when scheduling is %s, prevent another run and keep the last summary',
  async (state) => {
    const user = userEvent.setup()
    const props = createProps()
    props.running = state === 'running'
    props.refreshFailed = state === 'refresh-failed'
    props.runDisabled = props.refreshFailed
    render(<ChannelMonitorSmartScheduleOverview {...props} />)

    const run = screen.getByRole('button', { name: '立即调度' })
    expect(run).toBeDisabled()
    expect(run).toHaveAttribute('aria-busy', String(props.running))
    await user.click(run)
    expect(props.onRun).not.toHaveBeenCalled()
    expect(screen.getByRole('group', { name: '参与路由' })).toHaveTextContent(
      '5/6'
    )
    if (props.refreshFailed) {
      expect(screen.getByText('刷新失败，显示上次结果')).toBeVisible()
    }
  }
)

test('when no route has execution history, show an empty history state', () => {
  const props = createProps()
  props.summary.lastScheduleTime = 0
  render(<ChannelMonitorSmartScheduleOverview {...props} />)

  const execution = within(screen.getByRole('group', { name: '最近执行' }))
  expect(execution.getByText('暂无执行记录')).toBeVisible()
})

test('the history, settings and run actions remain keyboard accessible', async () => {
  const user = userEvent.setup()
  const props = createProps()
  render(<ChannelMonitorSmartScheduleOverview {...props} />)

  const history = screen.getByRole('button', { name: '智能调度记录' })
  const settings = screen.getByRole('button', { name: '调度设置' })
  const run = screen.getByRole('button', { name: '立即调度' })
  await user.tab()
  expect(history).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(props.onOpenHistory).toHaveBeenCalledOnce()
  await user.tab()
  expect(settings).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(props.onOpenSettings).toHaveBeenCalledOnce()
  await user.tab()
  expect(run).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(props.onRun).toHaveBeenCalledOnce()
})

test('hovering the history icon exposes its tooltip', async () => {
  const user = userEvent.setup()
  render(<ChannelMonitorSmartScheduleOverview {...createProps()} />)

  await user.hover(screen.getByRole('button', { name: '智能调度记录' }))

  expect(await screen.findByRole('tooltip')).toHaveTextContent('智能调度记录')
})
