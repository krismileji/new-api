import { fireEvent, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import {
  analyticsItem,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

test('an incomplete child explains its own coverage even when the channel total is complete', async () => {
  renderAnalyticsQuery('cost', (params) => {
    if (params.group_by === 'channel') {
      return analyticsResponse(params, [analyticsItem('7', { channel_id: 7 })])
    }
    return analyticsResponse(params, [], {
      coverage: {
        status: 'partial',
        covered_from: 1,
        covered_through: 2,
        reasons: ['cost_attribution_incomplete'],
      },
    })
  })
  fireEvent.click(await screen.findByRole('button', { name: '查看渠道 A明细' }))
  expect(await screen.findByText('渠道 A明细覆盖不完整')).toBeInTheDocument()
  expect(
    screen.getByText(/历史或未归属成本明细尚未与渠道日账对平/)
  ).toBeInTheDocument()
  expect(
    screen.queryByText('cost_attribution_incomplete')
  ).not.toBeInTheDocument()
  expect(screen.queryByText('统计覆盖不完整')).not.toBeInTheDocument()
})

test('an unavailable source is explained instead of presenting its response as an empty complete result', async () => {
  renderAnalyticsQuery('success', (params) =>
    analyticsResponse(params, [], {
      coverage: {
        status: 'unavailable',
        covered_from: 0,
        covered_through: 0,
        reasons: ['data_source_unavailable'],
      },
    })
  )
  expect(await screen.findByText('统计暂不可用')).toBeInTheDocument()
  expect(screen.queryByText('暂无统计数据')).not.toBeInTheDocument()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
})
