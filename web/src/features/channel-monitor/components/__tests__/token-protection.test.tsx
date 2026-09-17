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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import {
  tokenProtectionFormSchema,
  tokenProtectionFormValues,
  type TokenProtectionSettings,
} from '../../lib/token-protection'
import TokenProtectionDialog from '../token-protection-dialog'

const originalAdapter = api.defaults.adapter
let client: QueryClient | undefined

afterEach(() => {
  api.defaults.adapter = originalAdapter
  client?.clear()
})

function renderProtection(
  settings: TokenProtectionSettings = { revision: 1, enabled: false, rules: [] }
) {
  const requests: Array<{ method?: string; url?: string; data: unknown }> = []
  const network = { saveFails: false, released: false }
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  api.defaults.adapter = async (config) => {
    if (config.method !== 'get') {
      requests.push({
        method: config.method,
        url: config.url,
        data: JSON.parse(String(config.data)),
      })
    }
    let data: unknown = settings
    if (config.method === 'put') {
      if (network.saveFails) throw new Error('保存失败')
      const body = JSON.parse(String(config.data)) as TokenProtectionSettings
      settings = { ...body, revision: body.revision + 1 }
      data = settings
    }
    if (config.method === 'post') {
      network.released = true
      data = null
    }
    if (config.url?.endsWith('/records')) {
      data = {
        total: 1,
        pending: [],
        records: [
          {
            id: 'incident',
            token_id: 42,
            user_id: 7,
            token_name: '测试 Key',
            rule_name: '策略拦截',
            channel_id: 3,
            request_id: 'request-test',
            upstream_status: 403,
            error_summary: 'policy violation',
            response_status: 451,
            response_message: '此 Key 已禁用',
            canceled_requests: 2,
            created_at: 1726540000,
            released_at: network.released ? 1726540001 : 0,
            released_by: network.released ? 1 : 0,
          },
        ],
      }
    }
    return {
      data: { success: true, data },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  render(
    <QueryClientProvider client={client}>
      <TokenProtectionDialog onOpenChange={() => {}} />
    </QueryClientProvider>
  )
  return { requests, network }
}

test('新增规则时拒绝空关键词并保存状态码、关键词和全局开关', async () => {
  const user = userEvent.setup()
  const { requests } = renderProtection()
  await user.click(await screen.findByRole('button', { name: '添加规则' }))
  await user.type(screen.getByLabelText('规则名称'), '策略拦截')
  await user.click(screen.getByRole('button', { name: '保存规则' }))
  expect(
    await screen.findByText('请输入 1 到 32 个关键词，每个最多 512 字节')
  ).toBeInTheDocument()
  expect(requests).toHaveLength(0)
  await user.type(screen.getByLabelText('错误信息包含'), 'policy violation')
  await user.click(screen.getByRole('switch', { name: '自动禁用用户 API Key' }))
  await user.clear(screen.getByLabelText('返回状态码'))
  await user.type(screen.getByLabelText('返回状态码'), '451')
  await user.click(screen.getByRole('button', { name: '保存规则' }))
  await waitFor(() => expect(requests).toHaveLength(1))
  expect(requests[0].data).toMatchObject({
    enabled: true,
    revision: 1,
    rules: [
      {
        name: '策略拦截',
        status_codes: [403],
        keywords: ['policy violation'],
        channel_ids: [],
        response_status: 451,
      },
    ],
  })
})

test('保存失败时保留规则草稿并允许再次提交', async () => {
  const user = userEvent.setup()
  const { requests, network } = renderProtection()
  network.saveFails = true
  await user.click(await screen.findByRole('button', { name: '添加规则' }))
  await user.type(screen.getByLabelText('规则名称'), '保留草稿')
  await user.type(screen.getByLabelText('错误信息包含'), 'policy')
  await user.click(screen.getByRole('button', { name: '保存规则' }))
  await waitFor(() => expect(requests).toHaveLength(1))
  expect(screen.getByLabelText('规则名称')).toHaveValue('保留草稿')
  expect(screen.getByRole('button', { name: '保存规则' })).toBeEnabled()
  network.saveFails = false
  await user.click(screen.getByRole('button', { name: '保存规则' }))
  await waitFor(() => expect(requests).toHaveLength(2))
})

test('禁用记录必须确认后解除且刷新为已解除状态', async () => {
  const user = userEvent.setup()
  const { requests } = renderProtection()
  await user.click(await screen.findByRole('tab', { name: '禁用记录' }))
  await user.click(await screen.findByRole('button', { name: '解除禁用' }))
  expect(requests).toHaveLength(0)
  const confirmation = screen.getByRole('alertdialog')
  await user.click(
    within(confirmation).getByRole('button', { name: '解除禁用' })
  )
  await waitFor(() => expect(requests).toHaveLength(1))
  expect(requests[0].url).toBe(
    '/api/channel_monitor/token_protection/records/incident/release'
  )
  expect(await screen.findByText('已解除')).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: '解除禁用' })
  ).not.toBeInTheDocument()
})

test('规则校验拒绝成功返回码和非数字渠道编号', () => {
  const settings: TokenProtectionSettings = {
    enabled: true,
    revision: 1,
    rules: [
      {
        id: 'rule',
        name: '规则',
        enabled: true,
        channel_ids: [],
        status_codes: [403],
        keywords: ['policy'],
        match_all: false,
        case_sensitive: false,
        response_status: 403,
        response_message: '禁用',
      },
    ],
  }
  const form = tokenProtectionFormValues(settings)
  form.rules[0].response_status = 200
  expect(tokenProtectionFormSchema.safeParse(form).success).toBe(false)
  form.rules[0].response_status = 403
  form.rules[0].channels = 'not-a-channel'
  expect(tokenProtectionFormSchema.safeParse(form).success).toBe(false)
})
