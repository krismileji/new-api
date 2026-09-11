import { fireEvent, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import {
  analyticsItem,
  analyticsMetrics,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

const performance = {
  ...analyticsMetrics,
  first_token_sample_count: 6,
  average_first_token_ms: 500,
  tps_sample_count: 4,
  average_tps: 50,
  tps_output_tokens: 3000,
  tps_generation_duration_ms: 60000,
}

test('performance analysis shows measured sample counts and labels all four levels within the minute scope', async () => {
  const { requests } = renderAnalyticsQuery(
    'performance',
    (params) => {
      const dimensions = {
        channel: { channel_id: 7 },
        model: { model_name: 'model-a' },
        user: { user_id: 31, user_name: 'alice' },
        api_key: { api_key_id: 201, api_key_name: '生产 Key' },
      }
      return analyticsResponse(
        params,
        [
          analyticsItem(String(params.group_by), {
            ...performance,
            ...dimensions[params.group_by as keyof typeof dimensions],
          }),
        ],
        {
          source: 'redis_minutes',
          summary: performance,
          scope_summary: performance,
        }
      )
    },
    7,
    { rangeMinutes: 30, initialModel: 'model-a' }
  )

  expect(screen.getByRole('dialog', { name: '性能分析' })).toBeVisible()
  expect(screen.getByLabelText('统计时间范围')).toHaveTextContent('近30分钟')
  for (const [parent, dimension] of [
    ['渠道 A', '模型'],
    ['model-a', '用户'],
    ['alice', 'API Key'],
  ]) {
    fireEvent.click(
      await screen.findByRole('button', { name: `查看${parent}明细` })
    )
    expect(
      await screen.findByRole('columnheader', { name: dimension })
    ).toBeVisible()
  }
  const key = await screen.findByText('生产 Key')
  const keyRow = key.closest('tr')
  if (!keyRow) throw new Error('API Key 明细缺少所在行')
  const row = within(keyRow)
  expect(row.getByText('6 个有效样本')).toBeVisible()
  expect(row.getByText('4 个有效样本')).toBeVisible()
  expect(row.getByText('50 t/s')).toBeVisible()
  expect(
    screen.queryByRole('region', { name: '失败报错分类' })
  ).not.toBeInTheDocument()
  expect(screen.getAllByRole('columnheader')).toHaveLength(20)
  for (const request of requests) {
    expect(request).toMatchObject({
      metric: 'performance',
      minutes: 30,
      channel_id: 7,
      model: 'model-a',
    })
    expect(request.from).toBeUndefined()
    expect(request.to).toBeUndefined()
  }
  fireEvent.click(screen.getByRole('button', { name: /按平均首字排序/ }))
  await screen.findByRole('button', { name: '按平均首字排序（当前降序）' })
  expect(requests.at(-1)).toMatchObject({ sort: 'first_token' })
})

test('performance analysis displays missing measurements without zero latency or throughput', async () => {
  const missing = {
    ...performance,
    first_token_sample_count: 0,
    average_first_token_ms: null,
    tps_sample_count: 0,
    average_tps: null,
    tps_output_tokens: 0,
    tps_generation_duration_ms: 0,
  }
  renderAnalyticsQuery(
    'performance',
    (params) =>
      analyticsResponse(
        params,
        [analyticsItem('7', { channel_id: 7, ...missing })],
        { summary: missing, scope_summary: missing }
      ),
    7,
    { rangeMinutes: 15 }
  )
  const channel = await screen.findByRole('button', { name: '查看渠道 A明细' })
  const channelRow = channel.closest('tr')
  if (!channelRow) throw new Error('渠道明细缺少所在行')
  const row = within(channelRow)
  expect(row.getAllByText('暂无有效样本')).toHaveLength(2)
  expect(row.queryByText('0 t/s')).not.toBeInTheDocument()
})
