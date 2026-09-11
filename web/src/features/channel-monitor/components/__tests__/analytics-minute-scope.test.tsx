import { fireEvent, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import {
  analyticsItem,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

test('minute analysis preserves the active range, channel and model at every level', async () => {
  const { requests } = renderAnalyticsQuery(
    'success',
    (params) => {
      const rows = {
        channel: analyticsItem('7', { channel_id: 7 }),
        model: analyticsItem('model-a', {
          model_name: 'model-a',
          model_key: 'model-a-key',
        }),
        user: analyticsItem('31', { user_id: 31, user_name: 'alice' }),
        api_key: analyticsItem('201', {
          api_key_id: 201,
          api_key_name: '生产 Key',
        }),
      }
      return analyticsResponse(
        params,
        [rows[params.group_by as keyof typeof rows]],
        { source: 'redis_minutes', range_minutes: 30 }
      )
    },
    7,
    { rangeMinutes: 30, initialModel: 'model-a' }
  )

  expect(screen.getByLabelText('统计时间范围')).toHaveTextContent('近30分钟')
  expect(
    screen.queryByRole('button', { name: '统计日期范围' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('dialog')).toHaveAccessibleDescription(
    expect.stringContaining('model-a')
  )
  for (const name of ['渠道 A', 'model-a', 'alice']) {
    fireEvent.click(
      await screen.findByRole('button', { name: `查看${name}明细` })
    )
  }
  expect(await screen.findByText('生产 Key')).toBeVisible()
  for (const request of requests) {
    expect(request).toMatchObject({
      minutes: 30,
      channel_id: 7,
      model: 'model-a',
    })
    expect(request.from).toBeUndefined()
    expect(request.to).toBeUndefined()
  }
  expect(requests).toContainEqual(
    expect.objectContaining({
      group_by: 'api_key',
      user_id: 31,
      model_key: 'model-a-key',
    })
  )
})

test('group final-result analysis retains the group and final counts after switching tabs', async () => {
  const { requests } = renderAnalyticsQuery(
    'success',
    (params) =>
      analyticsResponse(params, [analyticsItem('7', { channel_id: 7 })], {
        source: 'redis_minutes',
        range_minutes: 60,
      }),
    undefined,
    { rangeMinutes: 60, initialGroup: 'vip', successMode: 'final' }
  )

  const channel = await screen.findByRole('button', { name: '查看渠道 A明细' })
  const channelRow = channel.closest('tr')
  expect(channelRow).not.toBeNull()
  if (!channelRow) throw new Error('渠道缺少所在行')
  const row = within(channelRow)
  expect(row.getByText('9 / 9 次')).toBeVisible()
  expect(row.getByText('100.0%')).toBeVisible()
  expect(screen.getByRole('columnheader', { name: /最终成功率/ })).toBeVisible()
  expect(screen.getByRole('dialog')).toHaveAccessibleDescription(
    expect.stringContaining('vip')
  )
  fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
  await screen.findByRole('columnheader', { name: '用户' })
  for (const request of requests) {
    expect(request).toMatchObject({
      minutes: 60,
      group: 'vip',
      success_mode: 'final',
    })
  }
})
