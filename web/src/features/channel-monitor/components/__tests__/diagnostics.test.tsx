import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { ChannelMonitorHealthStatus } from '../channel-monitor-health-status'

const dayStart = Date.parse('2026-09-13T00:00:00+08:00') / 1000
const observedAt = dayStart + 3600
const originalAdapter = api.defaults.adapter
let client: QueryClient
let readFailure: boolean
let resetFailure: string
let resetWait: Promise<void> | undefined
let resetDays: number[]
let snapshot: {
  day_start: number
  counted_since: number
  observed_at: number
  last_reset_at: number
  retry_count: number
  takeover_count: number
  quarantine_count: number
  marker_release_failure_count: number
  stream_trim_failure_count: number
  last_quarantined_at: number
}

beforeEach(() => {
  client = new QueryClient()
  readFailure = false
  resetFailure = ''
  resetWait = undefined
  resetDays = []
  snapshot = {
    day_start: dayStart,
    counted_since: observedAt - 600,
    observed_at: observedAt,
    last_reset_at: 0,
    retry_count: 6,
    takeover_count: 2,
    quarantine_count: 1,
    marker_release_failure_count: 0,
    stream_trim_failure_count: 0,
    last_quarantined_at: observedAt - 300,
  }
  api.defaults.adapter = async (config) => {
    let data: unknown = {
      status: 'healthy',
      recovery_status: 'data_incomplete',
      node_id: 'node-a',
      checked_at: observedAt,
      pending_count: 0,
      recovered_at: 0,
      message: '监控运行正常',
      action: '',
      data_gap_reasons: ['events_quarantined'],
      quarantine_count: 2795,
    }
    let message = ''
    if (config.url === '/api/channel_monitor/diagnostics') {
      data = { ...snapshot }
      if (readFailure) message = '今日诊断读取失败'
    }
    if (config.url === '/api/channel_monitor/diagnostics/reset') {
      const request = JSON.parse(String(config.data)) as { day_start: number }
      resetDays.push(request.day_start)
      await resetWait
      message = resetFailure
      if (!message) {
        snapshot = {
          ...snapshot,
          counted_since: observedAt,
          last_reset_at: observedAt,
          retry_count: 0,
          takeover_count: 0,
          quarantine_count: 0,
          marker_release_failure_count: 0,
          stream_trim_failure_count: 0,
          last_quarantined_at: 0,
        }
      }
      data = { ...snapshot }
    }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: !message, message, data },
    }
  }
})

afterEach(() => {
  client.clear()
  api.defaults.adapter = originalAdapter
})

function renderDiagnosticsPage() {
  return render(
    <QueryClientProvider client={client}>
      <ChannelMonitorHealthStatus
        metadata={{
          generated_at: observedAt,
          data_cutoff_at: observedAt,
          processed_at: observedAt,
          event_watermark: 1,
          queue_depth: 0,
          redis_available: true,
          redis_consumer_running: true,
          realtime_degraded: false,
          retry_count: 131365,
          quarantine_count: 2795,
        }}
      />
    </QueryClientProvider>
  )
}

describe('今日诊断与重置', () => {
  test('默认显示当天计数，取消重置不改动数据', async () => {
    const user = userEvent.setup()
    renderDiagnosticsPage()
    await user.click(await screen.findByRole('button', { name: '运行详情' }))
    const today = await screen.findByRole('region', { name: '今日诊断' })
    expect(
      within(today).getByRole('group', { name: '事件处理重试' })
    ).toHaveTextContent('6 次')
    expect(
      within(today).getByRole('group', { name: '异常隔离' })
    ).toHaveTextContent('1 条')
    expect(today).not.toHaveTextContent('131,365')
    expect(within(today).getByText(/北京时间/)).toBeVisible()
    await user.click(
      within(today).getByRole('button', { name: '重置今日计数' })
    )
    const confirmation = await screen.findByRole('alertdialog')
    expect(confirmation).toHaveTextContent('不会删除事件记录或改变故障状态')
    await user.click(within(confirmation).getByRole('button', { name: '取消' }))
    expect(resetDays).toEqual([])
    expect(
      within(today).getByRole('group', { name: '事件处理重试' })
    ).toHaveTextContent('6 次')
  })

  test('确认重置时阻止重复提交，成功后显示服务端返回的新计数', async () => {
    const user = userEvent.setup()
    let completeReset: () => void = () => undefined
    resetWait = new Promise<void>((resolve) => {
      completeReset = resolve
    })
    renderDiagnosticsPage()
    await user.click(await screen.findByRole('button', { name: '运行详情' }))
    const today = await screen.findByRole('region', { name: '今日诊断' })
    await user.click(
      within(today).getByRole('button', { name: '重置今日计数' })
    )
    const confirmation = await screen.findByRole('alertdialog')
    const confirm = within(confirmation).getByRole('button', {
      name: '确认重置',
    })
    await user.click(confirm)
    expect(confirm).toBeDisabled()
    expect(resetDays).toEqual([dayStart])
    await act(async () => {
      completeReset()
    })
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(
      within(today).getByRole('group', { name: '事件处理重试' })
    ).toHaveTextContent('0 次')
    expect(
      within(today).getByRole('group', { name: '异常隔离' })
    ).toHaveTextContent('0 条')
    expect(within(today).getByText(/重置后/)).toBeVisible()
  })

  test('重置失败保留原计数并显示失败原因', async () => {
    const user = userEvent.setup()
    resetFailure = '统计日期已变化，请刷新后重新重置今日计数'
    renderDiagnosticsPage()
    await user.click(await screen.findByRole('button', { name: '运行详情' }))
    const today = await screen.findByRole('region', { name: '今日诊断' })
    await user.click(
      within(today).getByRole('button', { name: '重置今日计数' })
    )
    const confirmation = await screen.findByRole('alertdialog')
    await user.click(
      within(confirmation).getByRole('button', { name: '确认重置' })
    )
    expect(await within(today).findByRole('alert')).toHaveTextContent(
      resetFailure
    )
    expect(
      within(today).getByRole('group', { name: '事件处理重试' })
    ).toHaveTextContent('6 次')
    expect(resetDays).toEqual([dayStart])
  })

  test('刷新失败明确标记旧计数并禁用重置', async () => {
    const user = userEvent.setup()
    renderDiagnosticsPage()
    await user.click(await screen.findByRole('button', { name: '运行详情' }))
    const today = await screen.findByRole('region', { name: '今日诊断' })
    await waitFor(() =>
      expect(
        within(today).getByRole('button', { name: '重置今日计数' })
      ).toBeEnabled()
    )
    readFailure = true
    await act(async () => {
      await client.invalidateQueries({
        queryKey: ['channel-monitor', 'diagnostics'],
        exact: true,
      })
    })
    expect(await within(today).findByRole('alert')).toHaveTextContent(
      '显示上次记录'
    )
    expect(
      within(today).getByRole('button', { name: '重置今日计数' })
    ).toBeDisabled()
    expect(
      within(today).getByRole('group', { name: '事件处理重试' })
    ).toHaveTextContent('6 次')
  })
})
