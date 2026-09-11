import { fireEvent, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import {
  analyticsItem,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

test.each(['success', 'cost'] as const)(
  '%s analysis labels every expanded channel, model, user and API Key level',
  async (metric) => {
    renderAnalyticsQuery(metric, (params) => {
      const rows = {
        channel: analyticsItem('7', { channel_id: 7 }),
        model: analyticsItem('model-a', { model_name: 'model-a' }),
        user: analyticsItem('31', { user_id: 31, user_name: 'alice' }),
        api_key: analyticsItem('201', {
          api_key_id: 201,
          api_key_name: '生产 Key',
        }),
      }
      return analyticsResponse(params, [
        rows[params.group_by as keyof typeof rows],
      ])
    })

    const channel = await screen.findByRole('button', {
      name: '查看渠道 A明细',
    })
    const metricLabel = metric === 'success' ? '上游尝试数' : '成本'
    for (const [parent, dimension] of [
      ['渠道 A', '模型'],
      ['model-a', '用户'],
      ['alice', 'API Key'],
    ]) {
      fireEvent.click(
        await screen.findByRole('button', { name: `查看${parent}明细` })
      )
      const header = await screen.findByRole('columnheader', {
        name: dimension,
      })
      const row = header.closest('tr')
      expect(row).not.toBeNull()
      if (!row) throw new Error('表头缺少所在行')
      expect(
        within(row).getByRole('columnheader', { name: metricLabel })
      ).toBeVisible()
      expect(within(row).getAllByRole('columnheader')).toHaveLength(5)
    }
    expect(await screen.findByText('生产 Key')).toBeVisible()
    expect(screen.getAllByRole('columnheader')).toHaveLength(20)

    fireEvent.click(channel)
    expect(
      screen.queryByRole('columnheader', { name: '模型' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('columnheader', { name: '用户' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('columnheader', { name: 'API Key' })
    ).not.toBeInTheDocument()
  }
)

test('API Key analysis labels user, API Key, model and channel levels', async () => {
  renderAnalyticsQuery('success', (params) => {
    const rows = {
      user: analyticsItem('31', { user_id: 31, user_name: 'alice' }),
      api_key: analyticsItem('201', {
        api_key_id: 201,
        api_key_name: '生产 Key',
      }),
      model: analyticsItem('model-a', { model_name: 'model-a' }),
      channel: analyticsItem('7', { channel_id: 7 }),
    }
    return analyticsResponse(params, [
      rows[params.group_by as keyof typeof rows],
    ])
  })

  fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
  for (const [parent, dimension] of [
    ['alice', 'API Key'],
    ['生产 Key', '模型'],
    ['model-a', '渠道'],
  ]) {
    fireEvent.click(
      await screen.findByRole('button', { name: `查看${parent}明细` })
    )
    expect(
      await screen.findByRole('columnheader', { name: dimension })
    ).toBeVisible()
  }
  expect(await screen.findByText('渠道 A')).toBeVisible()
  expect(screen.getAllByRole('columnheader')).toHaveLength(20)
})

test('an empty expanded level still identifies the child dimension', async () => {
  renderAnalyticsQuery('success', (params) =>
    analyticsResponse(
      params,
      params.group_by === 'channel'
        ? [analyticsItem('7', { channel_id: 7 })]
        : []
    )
  )
  fireEvent.click(await screen.findByRole('button', { name: '查看渠道 A明细' }))
  expect(await screen.findByText('暂无可展开的明细')).toBeVisible()
  expect(screen.getByRole('columnheader', { name: '模型' })).toBeVisible()
})
