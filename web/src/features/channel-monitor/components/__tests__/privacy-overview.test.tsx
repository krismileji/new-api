import { act, cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { useChannelMonitorPrivacyStore } from '@/stores/channel-monitor-privacy-store'

import type { ChannelMonitorTodaySuccessResult } from '../../types'
import {
  ChannelMonitorPrivacyProvider,
  ChannelMonitorPrivacyToggle,
} from '../channel-monitor-privacy'
import { ChannelMonitorTodaySuccessCard } from '../channel-monitor-today-success-card'

const summary = {
  actual_success_count: 9,
  actual_failure_count: 1,
  actual_sample_count: 10,
  actual_success_rate: 0.9,
  final_success_count: 9,
  final_failure_count: 1,
  final_sample_count: 10,
  final_success_rate: 0.9,
  cache_hit_count: 1,
  cache_sample_count: 2,
  cache_hit_rate: 0.5,
  cache_read_tokens: 50,
  input_tokens: 100,
  cache_utilization_rate: 0.5,
}
const result: ChannelMonitorTodaySuccessResult = {
  days: 1,
  generated_at: 0,
  data_cutoff_at: 0,
  processed_at: 0,
  event_watermark: 0,
  queue_depth: 0,
  realtime_degraded: false,
  day_start: 0,
  detail_date: '2026-09-28',
  success_metrics_available: true,
  cache_write_metrics_available: true,
  summary,
  channel_items: [],
  cache_write_items: [],
  chart_items: [],
  api_key_items: [
    {
      ...summary,
      api_key_id: 21,
      api_key_name: '私有生产密钥',
      cache_read_tokens: 25,
      cache_utilization_rate: 0.25,
    },
  ],
}

afterEach(() => {
  cleanup()
  useChannelMonitorPrivacyStore.setState({ enabled: false })
  localStorage.clear()
})

test('hiding a selected API Key removes names and open options while keeping aggregate metrics, then restores selection', async () => {
  const user = userEvent.setup()
  render(
    <ChannelMonitorPrivacyProvider>
      <ChannelMonitorPrivacyToggle />
      <ChannelMonitorTodaySuccessCard
        result={result}
        isLoading={false}
        isError={false}
        onOpen={vi.fn()}
      />
    </ChannelMonitorPrivacyProvider>
  )
  await user.click(
    screen.getByRole('combobox', { name: '选择缓存利用率 API Key' })
  )
  await user.click(
    await screen.findByRole('option', { name: '私有生产密钥 · ID 21' })
  )
  expect(screen.getByText('25%')).toBeVisible()
  await user.click(
    screen.getByRole('combobox', { name: '选择缓存利用率 API Key' })
  )
  act(() => useChannelMonitorPrivacyStore.getState().setEnabled(true))
  expect(screen.queryAllByRole('option')).toHaveLength(0)
  expect(screen.queryByText(/私有生产密钥/)).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: /私有生产密钥/ })
  ).not.toBeInTheDocument()
  expect(screen.getByText('50%')).toBeVisible()
  expect(screen.getByText('90%')).toBeVisible()
  await user.click(screen.getByRole('button', { name: '显示敏感信息' }))
  expect(
    screen.getByRole('combobox', { name: '选择缓存利用率 API Key' })
  ).toHaveTextContent('私有生产密钥')
  expect(screen.getByText('25%')).toBeVisible()
})
