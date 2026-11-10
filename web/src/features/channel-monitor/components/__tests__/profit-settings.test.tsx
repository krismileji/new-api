import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosAdapter } from 'axios'
import { expect, test } from 'vitest'

import { api } from '@/lib/api'

import type { ChannelMonitorProfitSettings } from '../../api'
import {
  analyticsItem,
  analyticsMetrics,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

function renderProfitSettings() {
  const server = {
    settings: { cost_only_api_key_ids: [] } as ChannelMonitorProfitSettings,
    writes: [] as ChannelMonitorProfitSettings[],
    failRead: false,
    failSave: false,
    readGate: undefined as Promise<void> | undefined,
    saveGate: undefined as Promise<void> | undefined,
  }
  const view = renderAnalyticsQuery(
    'profit',
    (params) => {
      const excluded = server.settings.cost_only_api_key_ids.includes(201)
      const summary = {
        ...analyticsMetrics,
        income_nano_cny: excluded ? 0 : 10e9,
        excluded_income_nano_cny: excluded ? 10e9 : 0,
        cost_nano_cny: 6e9,
        profit_nano_cny: excluded ? -6e9 : 4e9,
        profit_confirmed: true,
      }
      return analyticsResponse(
        params,
        params.group_by === 'day'
          ? []
          : [analyticsItem('7', { ...summary, channel_id: 7 })],
        { summary, scope_summary: summary }
      )
    },
    undefined,
    { showRequestOverview: true }
  )
  const analyticsAdapter = api.defaults.adapter as AxiosAdapter
  api.defaults.adapter = async (config) => {
    if (!config.url?.endsWith('/profit-settings')) {
      return analyticsAdapter(config)
    }
    if (config.method === 'get') await server.readGate
    if (config.method === 'get' && server.failRead) throw new Error('读取失败')
    if (config.method === 'put') {
      const settings: ChannelMonitorProfitSettings = JSON.parse(
        config.data as string
      )
      server.writes.push(settings)
      await server.saveGate
      if (!server.failSave) server.settings = settings
    }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: config.method !== 'put' || !server.failSave,
        message: server.failSave ? '保存失败' : '',
        data: server.settings,
      },
    }
  }
  return { ...view, server, user: userEvent.setup() }
}

test('saving multiple self-use keys refreshes profit while retaining costs and request metrics, and clearing restores income', async () => {
  const { server, user } = renderProfitSettings()
  await screen.findByRole('row', { name: /渠道 A/ })
  await user.click(screen.getByRole('button', { name: '自用 API Key' }))
  const input = await screen.findByLabelText('仅计成本的 API Key ID')
  await user.type(input, '201')
  expect(input).toHaveValue('201')
  await screen.findByRole('option', { name: '添加 Key ID 201' })
  await user.keyboard('{Enter}')
  expect(input).toHaveValue('')
  await user.type(input, '202{Enter}')
  await user.tab()
  expect(
    within(screen.getByRole('dialog', { name: '自用 API Key' })).getByText(
      '201'
    )
  ).toBeVisible()
  expect(
    within(screen.getByRole('dialog', { name: '自用 API Key' })).getByText(
      '202'
    )
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: '自用 API Key' })
    ).not.toBeInTheDocument()
  )
  expect(server.writes).toEqual([{ cost_only_api_key_ids: [201, 202] }])
  const row = within(screen.getByRole('row', { name: /渠道 A/ }))
  expect(row.getByText('-¥6.0000')).toBeVisible()
  expect(row.getByText('¥6.0000')).toBeVisible()
  expect(row.getByText('90.0%')).toBeVisible()
  expect(screen.getByText(/已排除自用扣费/)).toHaveTextContent('¥10.0000')

  await user.click(screen.getByRole('button', { name: '自用 API Key' }))
  const dialog = await screen.findByRole('dialog', { name: '自用 API Key' })
  await screen.findByLabelText('仅计成本的 API Key ID')
  expect(within(dialog).getByText('201')).toBeVisible()
  expect(within(dialog).getByText('202')).toBeVisible()
  await user.click(screen.getByLabelText('仅计成本的 API Key ID'))
  await user.keyboard('{Backspace}{Backspace}{Backspace}{Backspace}')
  await user.tab()
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() =>
    expect(server.writes.at(-1)).toEqual({ cost_only_api_key_ids: [] })
  )
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: '自用 API Key' })
    ).not.toBeInTheDocument()
  )
  expect(
    within(screen.getByRole('row', { name: /渠道 A/ })).getByText('¥4.0000')
  ).toBeVisible()
  expect(screen.queryByText(/已排除自用扣费/)).not.toBeInTheDocument()
})

test('a failed settings read offers retry without allowing an empty replacement to be saved', async () => {
  const { server, user } = renderProfitSettings()
  server.failRead = true
  await user.click(screen.getByRole('button', { name: '自用 API Key' }))
  expect(await screen.findByText('自用 API Key 配置加载失败')).toBeVisible()
  expect(screen.queryByRole('button', { name: '保存' })).not.toBeInTheDocument()
  server.failRead = false
  server.settings = { cost_only_api_key_ids: [201] }
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByLabelText('仅计成本的 API Key ID')).toBeEnabled()
  expect(
    within(screen.getByRole('dialog', { name: '自用 API Key' })).getByText(
      '201'
    )
  ).toBeVisible()
  expect(server.writes).toHaveLength(0)
})

test('loading hides the form and a pending save disables editing until the server responds', async () => {
  const { server, user } = renderProfitSettings()
  let finishRead: () => void = () => undefined
  let finishSave: () => void = () => undefined
  server.readGate = new Promise<void>((resolve) => {
    finishRead = resolve
  })
  server.saveGate = new Promise<void>((resolve) => {
    finishSave = resolve
  })
  await user.click(screen.getByRole('button', { name: '自用 API Key' }))
  expect(await screen.findByText('正在读取自用 API Key 配置…')).toBeVisible()
  expect(screen.queryByRole('button', { name: '保存' })).not.toBeInTheDocument()
  finishRead()
  const input = await screen.findByLabelText('仅计成本的 API Key ID')
  await user.type(input, '201{Enter}')
  await user.tab()
  await user.click(screen.getByRole('button', { name: '保存' }))
  expect(await screen.findByRole('button', { name: '保存中…' })).toBeDisabled()
  expect(input).toBeDisabled()
  finishSave()
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: '自用 API Key' })
    ).not.toBeInTheDocument()
  )
  expect(server.writes).toEqual([{ cost_only_api_key_ids: [201] }])
})

test('invalid key IDs show validation and do not save', async () => {
  const { server, user } = renderProfitSettings()
  await user.click(screen.getByRole('button', { name: '自用 API Key' }))
  const input = await screen.findByLabelText('仅计成本的 API Key ID')
  await user.type(input, '0{Enter}')
  await user.tab()
  await user.click(screen.getByRole('button', { name: '保存' }))
  expect(
    await screen.findByText('API Key ID 必须为 1–2147483647 的整数')
  ).toBeVisible()
  expect(input).toHaveAttribute('aria-invalid', 'true')
  expect(input).toHaveAccessibleDescription(
    /API Key ID 必须为 1–2147483647 的整数/
  )
  expect(server.writes).toHaveLength(0)
})

test('a failed save preserves draft IDs for retry and leaves the overview unchanged', async () => {
  const { server, user } = renderProfitSettings()
  server.failSave = true
  await user.click(screen.getByRole('button', { name: '自用 API Key' }))
  const input = await screen.findByLabelText('仅计成本的 API Key ID')
  await user.type(input, '201{Enter}')
  await user.tab()
  await user.click(screen.getByRole('button', { name: '保存' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    '输入的 Key ID 已保留'
  )
  expect(screen.getByRole('dialog', { name: '自用 API Key' })).toBeVisible()
  expect(server.settings.cost_only_api_key_ids).toEqual([])
  server.failSave = false
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: '自用 API Key' })
    ).not.toBeInTheDocument()
  )
  expect(server.writes).toEqual([
    { cost_only_api_key_ids: [201] },
    { cost_only_api_key_ids: [201] },
  ])
})
