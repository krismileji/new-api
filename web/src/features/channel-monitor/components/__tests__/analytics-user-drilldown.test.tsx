import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test } from 'vitest'

import {
  analyticsItem,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

test.each(['cost', 'success'] as const)(
  '%s starts with users and preserves the user, key and channel scope through every level',
  async (metric) => {
    const { requests } = renderAnalyticsQuery(
      metric,
      (params) => {
        if (params.group_by === 'user') {
          return analyticsResponse(params, [
            analyticsItem('31', { user_id: 31, user_name: 'alice' }),
            analyticsItem('32', { user_id: 32, user_name: 'bob' }),
          ])
        }
        if (params.group_by === 'api_key') {
          return analyticsResponse(params, [
            analyticsItem('201:fingerprint', {
              user_id: 31,
              api_key_id: 201,
              api_key_key: 'fingerprint',
              api_key_name: '生产 Key',
            }),
          ])
        }
        if (params.group_by === 'model') {
          return analyticsResponse(params, [
            analyticsItem('model-a', {
              model_name: 'model-a',
              model_key: 'model-a-key',
            }),
          ])
        }
        return analyticsResponse(params, [
          analyticsItem('7', { channel_id: 7 }),
        ])
      },
      7
    )

    fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
    const user = await screen.findByRole('button', { name: '查看alice明细' })
    expect(screen.queryByText('生产 Key')).not.toBeInTheDocument()
    user.focus()
    await userEvent.keyboard('{Enter}')
    fireEvent.click(
      await screen.findByRole('button', { name: '查看生产 Key明细' })
    )
    fireEvent.click(
      await screen.findByRole('button', { name: '查看model-a明细' })
    )
    expect(await screen.findByText('渠道 A')).toBeInTheDocument()
    expect(user).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('button', { name: '查看bob明细' })).toHaveAttribute(
      'aria-expanded',
      'false'
    )
    expect(requests).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          metric,
          group_by: 'api_key',
          user_id: 31,
          channel_id: 7,
        }),
        expect.objectContaining({
          metric,
          group_by: 'model',
          user_id: 31,
          api_key_id: 201,
          api_key_key: 'fingerprint',
          channel_id: 7,
        }),
        expect.objectContaining({
          metric,
          group_by: 'channel',
          user_id: 31,
          api_key_id: 201,
          api_key_key: 'fingerprint',
          model: 'model-a',
          channel_id: 7,
        }),
      ])
    )
    expect(requests.some((request) => request.user_id === 32)).toBe(false)
  }
)

test('unknown ownership keeps explicit zero IDs and the empty key identity when expanding', async () => {
  const { requests } = renderAnalyticsQuery('cost', (params) => {
    if (params.group_by === 'user') {
      return analyticsResponse(params, [analyticsItem('0', { user_id: 0 })])
    }
    if (params.group_by === 'api_key') {
      return analyticsResponse(params, [
        analyticsItem('0:', { user_id: 0, api_key_id: 0, api_key_key: '' }),
      ])
    }
    return analyticsResponse(params, [])
  })
  fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
  fireEvent.click(
    await screen.findByRole('button', { name: '查看未归属用户明细' })
  )
  fireEvent.click(
    await screen.findByRole('button', { name: '查看未识别 API Key明细' })
  )
  expect(await screen.findByText('暂无可展开的明细')).toBeInTheDocument()
  expect(requests).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ group_by: 'api_key', user_id: 0 }),
      expect.objectContaining({
        group_by: 'model',
        user_id: 0,
        api_key_id: 0,
        api_key_key: '',
      }),
    ])
  )
})

test('a failed child page can return to the previous loaded page', async () => {
  renderAnalyticsQuery('cost', (params) => {
    if (params.group_by === 'user') {
      return analyticsResponse(params, [
        analyticsItem('31', { user_id: 31, user_name: 'alice' }),
      ])
    }
    if (params.group_by === 'api_key') {
      if (params.page === 2) throw new Error('网络中断')
      return analyticsResponse(
        params,
        Array.from({ length: 20 }, (_, index) =>
          analyticsItem(String(index + 1), {
            api_key_id: index + 1,
            api_key_name: `Key ${index + 1}`,
          })
        ),
        { total: 21 }
      )
    }
    return analyticsResponse(params, [])
  })
  fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
  fireEvent.click(await screen.findByRole('button', { name: '查看alice明细' }))
  fireEvent.click(
    await screen.findByRole('button', { name: 'alice明细下一页' })
  )
  expect(await screen.findByText('明细加载失败')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'alice明细上一页' }))
  expect(await screen.findByText('Key 1')).toBeInTheDocument()
  expect(screen.queryByText('明细加载失败')).not.toBeInTheDocument()
})

test('a user with more than one page of keys can reach the last key without changing the parent total', async () => {
  renderAnalyticsQuery('cost', (params) => {
    if (params.group_by === 'user') {
      return analyticsResponse(params, [
        analyticsItem('31', { user_id: 31, user_name: 'alice' }),
      ])
    }
    if (params.group_by === 'api_key') {
      const keys =
        params.page === 2
          ? [
              analyticsItem('21', {
                api_key_id: 21,
                api_key_name: '最后一个 Key',
              }),
            ]
          : Array.from({ length: 20 }, (_, index) =>
              analyticsItem(String(index + 1), {
                api_key_id: index + 1,
                api_key_name: `Key ${index + 1}`,
              })
            )
      return analyticsResponse(params, keys, { total: 21 })
    }
    return analyticsResponse(params, [])
  })
  fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
  fireEvent.click(await screen.findByRole('button', { name: '查看alice明细' }))
  expect(await screen.findByText('Key 1')).toBeInTheDocument()
  const parentRow = screen
    .getByRole('button', { name: '查看alice明细' })
    .closest('tr')
  const parentText = parentRow?.textContent
  fireEvent.click(
    await screen.findByRole('button', { name: 'alice明细下一页' })
  )
  expect(await screen.findByText('最后一个 Key')).toBeInTheDocument()
  expect(screen.queryByText('Key 1')).not.toBeInTheDocument()
  expect(parentRow).toHaveTextContent(parentText ?? '')
  expect(screen.getByRole('button', { name: 'alice明细下一页' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: 'alice明细上一页' }))
  expect(await screen.findByText('Key 1')).toBeInTheDocument()
})
