import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { expect, test } from 'vitest'

import type { ChannelMonitorAnalyticsResponse } from '../../types-analytics'
import {
  analyticsItem,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

test('background refresh keeps the expanded descendants while loading and after it completes', async () => {
  let refreshing = false
  const pending: Array<() => void> = []
  const { client } = renderAnalyticsQuery('success', (params) => {
    const items =
      params.group_by === 'channel'
        ? [analyticsItem('7', { channel_id: 7 })]
        : [
            analyticsItem('model-a', {
              model_name: 'model-a',
              model_key: 'model-a-key',
            }),
          ]
    const result = analyticsResponse(params, items)
    if (!refreshing) return result
    return new Promise<ChannelMonitorAnalyticsResponse>((resolve) => {
      pending.push(() => resolve(result))
    })
  })
  fireEvent.click(await screen.findByRole('button', { name: '查看渠道 A明细' }))
  expect(
    await screen.findByRole('button', { name: '查看model-a明细' })
  ).toBeInTheDocument()
  refreshing = true
  let refresh: Promise<void> | undefined
  act(() => {
    refresh = client.refetchQueries({
      queryKey: ['channel-monitor', 'analytics'],
      type: 'active',
    })
  })
  await waitFor(() => expect(pending.length).toBeGreaterThan(0))
  const keptWhileLoading =
    screen.queryByRole('button', { name: '查看model-a明细' }) !== null
  await act(async () => {
    refreshing = false
    for (const release of pending) release()
    await refresh
  })
  expect(keptWhileLoading).toBe(true)
  expect(
    screen.getByRole('button', { name: '查看渠道 A明细' })
  ).toHaveAttribute('aria-expanded', 'true')
  expect(
    screen.getByRole('button', { name: '查看model-a明细' })
  ).toBeInTheDocument()
})

test('changing the date range resets expanded rows even when the new range has the same channel', async () => {
  renderAnalyticsQuery('cost', (params) =>
    analyticsResponse(
      params,
      params.group_by === 'channel'
        ? [analyticsItem('7', { channel_id: 7 })]
        : [analyticsItem('model-a', { model_name: 'model-a' })]
    )
  )
  fireEvent.click(await screen.findByRole('button', { name: '查看渠道 A明细' }))
  expect(
    await screen.findByRole('button', { name: '查看model-a明细' })
  ).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '统计日期范围' }))
  fireEvent.change(screen.getByLabelText('开始日期'), {
    target: { value: '2026-08-01' },
  })
  fireEvent.change(screen.getByLabelText('结束日期'), {
    target: { value: '2026-08-02' },
  })
  fireEvent.click(screen.getByRole('button', { name: '应用' }))
  const channel = await screen.findByRole('button', { name: '查看渠道 A明细' })
  expect(channel).toHaveAttribute('aria-expanded', 'false')
  expect(
    screen.queryByRole('button', { name: '查看model-a明细' })
  ).not.toBeInTheDocument()
})

test('a failed background refresh keeps the expanded data and can be retried', async () => {
  let unavailable = false
  const { client } = renderAnalyticsQuery('success', (params) => {
    if (unavailable) throw new Error('网络中断')
    return analyticsResponse(
      params,
      params.group_by === 'channel'
        ? [analyticsItem('7', { channel_id: 7 })]
        : [
            analyticsItem('model-a', {
              model_name: 'model-a',
              model_key: 'model-a-key',
            }),
          ]
    )
  })
  fireEvent.click(await screen.findByRole('button', { name: '查看渠道 A明细' }))
  expect(
    await screen.findByRole('button', { name: '查看model-a明细' })
  ).toBeInTheDocument()
  unavailable = true
  await act(async () => {
    await client.refetchQueries({
      queryKey: ['channel-monitor', 'analytics'],
      type: 'active',
    })
  })
  expect(
    await screen.findByText('统计更新失败，保留上次结果')
  ).toBeInTheDocument()
  expect(screen.getByText('明细更新失败，保留上次结果')).toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: '查看model-a明细' })
  ).toBeInTheDocument()
  unavailable = false
  for (const button of screen.getAllByRole('button', { name: '重试' })) {
    fireEvent.click(button)
  }
  await waitFor(() => {
    expect(
      screen.queryByText('统计更新失败，保留上次结果')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByText('明细更新失败，保留上次结果')
    ).not.toBeInTheDocument()
  })
  expect(
    screen.getByRole('button', { name: '查看渠道 A明细' })
  ).toHaveAttribute('aria-expanded', 'true')
  expect(
    screen.getByRole('button', { name: '查看model-a明细' })
  ).toBeInTheDocument()
})
