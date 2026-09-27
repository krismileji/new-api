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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CHANNEL_MONITOR_SMART_SCHEDULE_PERMANENT_UNTIL } from '../../lib/smart-schedule-display'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
  channelMonitorSmartScheduleGroupPoliciesToApi,
  createChannelMonitorSmartScheduleGroupPolicy,
} from '../../lib/smart-schedule-group-policy'
import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteResult,
} from '../../types'
import { ChannelMonitorSmartScheduleBoard } from '../channel-monitor-smart-schedule-board'
import { ChannelMonitorSmartScheduleGroupPause } from '../channel-monitor-smart-schedule-group-pause'
import { createSmartScheduleCellRoute } from './smart-schedule-cell-test-data'

afterEach(() => vi.restoreAllMocks())

function renderDurationBoard(route = createSmartScheduleCellRoute()) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  const result: ChannelMonitorSmartScheduleRouteResult = {
    generated_at: 1_752_777_845,
    data_cutoff_at: 1_752_777_845,
    processed_at: 1_752_777_845,
    event_watermark: 0,
    queue_depth: 0,
    realtime_degraded: false,
    performance_window_minutes: 60,
    stability_window_minutes: 120,
    sample_scope: 'channel_model',
    enabled: true,
    routes: [route],
    performance_items: [],
    stability_metrics_available: true,
    stability_items: [],
    route_snapshot: {
      available: true,
      revision: 1,
      generated_at: 1_752_777_845,
      source_watermark: 0,
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
  }
  const onActionComplete = vi.fn(async () => {})
  render(
    <QueryClientProvider client={client}>
      <ChannelMonitorSmartScheduleBoard
        active
        result={result}
        channels={[]}
        groupPolicies={channelMonitorSmartScheduleGroupPoliciesToApi([
          createChannelMonitorSmartScheduleGroupPolicy(
            route.group,
            CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE
          ),
        ])}
        groupRatios={{ default: 1 }}
        isLoading={false}
        isError={false}
        onOpenSettings={() => {}}
        onOpenHistory={() => {}}
        onActionComplete={onActionComplete}
      />
    </QueryClientProvider>
  )
  return { onActionComplete }
}

const dialogCases = [
  {
    action: 'fixed primary',
    trigger: '固定 测试渠道 为主渠道',
    input: '固定时长（分钟）',
    submit: '固定主渠道',
    endpoint: 'primary',
    max: 525600,
  },
  {
    action: '429 pause',
    trigger: '暂停 测试渠道 在 default 分组使用 model-a 模型的 429 限制',
    input: '429 限制暂停时长（分钟）',
    submit: '更新限制暂停时间',
    endpoint: 'rate-limit-cooldown',
    max: 300,
  },
] as const

describe.each(dialogCases)('$action optional duration', (scenario) => {
  test.each([null, 30])(
    'submits duration %s without turning an empty field into a restore',
    async (duration) => {
      const put = vi.spyOn(api, 'put').mockResolvedValue({
        data: {
          success: true,
          data: {
            group: 'default',
            model: 'model-a',
            duration_minutes: duration,
          },
        },
      })
      const rendered = renderDurationBoard()
      const user = userEvent.setup()
      await user.click(
        within(screen.getByRole('table')).getByRole('button', {
          name: scenario.trigger,
        })
      )
      const dialog = screen.getByRole('dialog')
      const input = within(dialog).getByRole('spinbutton', {
        name: scenario.input,
      })
      await user.clear(input)
      if (duration !== null) await user.type(input, String(duration))
      const submit = within(dialog).getByRole('button', {
        name: scenario.submit,
      })
      expect(submit).toBeEnabled()
      await user.click(submit)

      await waitFor(() =>
        expect(put).toHaveBeenCalledWith(
          `/api/channel_monitor/channel/7/schedule/route/${scenario.endpoint}`,
          expect.objectContaining({
            group: 'default',
            model: 'model-a',
            duration_minutes: duration,
          }),
          expect.any(Object)
        )
      )
      await waitFor(() =>
        expect(rendered.onActionComplete).toHaveBeenCalledOnce()
      )
    }
  )

  test('blocks zero, negative, fractional and over-limit durations', async () => {
    const put = vi.spyOn(api, 'put')
    renderDurationBoard()
    const user = userEvent.setup()
    await user.click(
      within(screen.getByRole('table')).getByRole('button', {
        name: scenario.trigger,
      })
    )
    const dialog = screen.getByRole('dialog')
    const input = within(dialog).getByRole('spinbutton', {
      name: scenario.input,
    })
    const submit = within(dialog).getByRole('button', { name: scenario.submit })
    for (const value of ['0', '-1', '1.5', String(scenario.max + 1)]) {
      fireEvent.change(input, { target: { value } })
      expect(submit).toBeDisabled()
    }
    expect(put).not.toHaveBeenCalled()
  })
})

test('reopens a permanent 429 pause with an empty field and restores it explicitly', async () => {
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: true,
      data: { group: 'default', model: 'model-a', duration_minutes: 0 },
    },
  })
  const rendered = renderDurationBoard(
    createSmartScheduleCellRoute({
      rate_limit_bypass_until: CHANNEL_MONITOR_SMART_SCHEDULE_PERMANENT_UNTIL,
    })
  )
  const user = userEvent.setup()
  await user.click(
    within(screen.getByRole('table')).getByRole('button', {
      name: '恢复 测试渠道 在 default 分组使用 model-a 模型的 429 限制',
    })
  )
  const dialog = screen.getByRole('dialog')
  expect(
    within(dialog).getByRole('spinbutton', { name: '429 限制暂停时长（分钟）' })
  ).toHaveValue(null)
  await user.click(
    within(dialog).getByRole('button', { name: '恢复 429 限制' })
  )
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/channel_monitor/channel/7/schedule/route/rate-limit-cooldown',
      { group: 'default', model: 'model-a', duration_minutes: 0 },
      expect.any(Object)
    )
  )
  await waitFor(() => expect(rendered.onActionComplete).toHaveBeenCalledOnce())
})

test('keeps the explicit clear action available for a permanently fixed primary', async () => {
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, data: { duration_minutes: 0 } },
  })
  const rendered = renderDurationBoard(
    createSmartScheduleCellRoute({
      state: {
        manual_primary_until: CHANNEL_MONITOR_SMART_SCHEDULE_PERMANENT_UNTIL,
      },
    })
  )
  const user = userEvent.setup()
  await user.click(
    within(screen.getByRole('table')).getByRole('button', {
      name: '取消固定 测试渠道',
    })
  )
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/channel_monitor/channel/7/schedule/route/primary',
      expect.objectContaining({ duration_minutes: 0 }),
      expect.any(Object)
    )
  )
  await waitFor(() => expect(rendered.onActionComplete).toHaveBeenCalledOnce())
})

describe('traffic pause optional duration', () => {
  test.each([null, 30])(
    'submits duration %s from the traffic form',
    async (duration) => {
      const onUpdate =
        vi.fn<
          (
            route: ChannelMonitorSmartScheduleRoute,
            duration: number | null
          ) => void
        >()
      const route = createSmartScheduleCellRoute()
      render(
        <ChannelMonitorSmartScheduleGroupPause
          route={route}
          pending={false}
          disabled={false}
          onUpdate={onUpdate}
        />
      )
      const user = userEvent.setup()
      const input = screen.getByRole('spinbutton', { name: '暂停时长（分钟）' })
      expect(input).not.toBeRequired()
      await user.clear(input)
      if (duration !== null) await user.type(input, String(duration))
      await user.click(screen.getByRole('button', { name: '暂停路由流量' }))
      expect(onUpdate).toHaveBeenCalledWith(route, duration)
    }
  )

  test('shows a permanent pause as permanent, leaves its field empty and allows resuming', async () => {
    const onUpdate = vi.fn()
    const route = createSmartScheduleCellRoute({
      traffic_paused_until: CHANNEL_MONITOR_SMART_SCHEDULE_PERMANENT_UNTIL,
    })
    render(
      <ChannelMonitorSmartScheduleGroupPause
        route={route}
        pending={false}
        disabled={false}
        onUpdate={onUpdate}
      />
    )
    expect(screen.getByText('暂停至 永久')).toBeInTheDocument()
    expect(
      screen.getByRole('spinbutton', { name: '暂停时长（分钟）' })
    ).toHaveValue(null)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '立即恢复' }))
    expect(onUpdate).toHaveBeenCalledWith(route, 0)
  })
})
