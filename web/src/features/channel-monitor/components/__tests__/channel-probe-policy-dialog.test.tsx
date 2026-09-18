import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { ChannelTestDialogForChannel } from '@/features/channels/components/dialogs/channel-test-dialog'
import { api } from '@/lib/api'

import type { ChannelMonitorItem, ChannelProbePolicy } from '../../types'
import type { ChannelPassivePeriod } from '../../types-passive'
import { ChannelPassivePeriodMetrics } from '../channel-passive-monitor-panel'
import {
  ChannelProbePolicyAction,
  ChannelProbePolicyDialog,
} from '../channel-probe-policy-dialog'

const originalAdapter = api.defaults.adapter
let client: QueryClient
afterEach(() => {
  api.defaults.adapter = originalAdapter
  client?.clear()
})

test('在测试连接中保存策略后保留测试弹窗和筛选条件，关闭自动禁用会关闭小输入响应并保留文本', async () => {
  const policy: ChannelProbePolicy = {
    auto_probe_disabled: true,
    small_input_response_enabled: true,
    small_input_threshold_tokens: 1001,
    small_input_response_text: '第一行\n第二行',
    probe_policy_revision: 4,
    probe_policy_updated_at: 1,
  }
  const saved: ChannelProbePolicy[] = []
  api.defaults.adapter = async (config) => {
    if (config.method === 'put') {
      saved.push(JSON.parse(config.data as string) as ChannelProbePolicy)
    }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data: policy },
    }
  }
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const close = vi.fn()
  const channel = {
    id: 17,
    name: '禁探测渠道',
    models: 'test-model,another-model',
    test_model: 'test-model',
  } as ChannelMonitorItem
  render(
    <QueryClientProvider client={client}>
      <ChannelTestDialogForChannel
        open
        channel={channel}
        onOpenChange={close}
        footerActions={<ChannelProbePolicyAction channel={channel} />}
      />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  const testDialog = screen.getByRole('dialog', {
    name: 'Test Channel Connection:禁探测渠道',
  })
  await user.type(
    within(testDialog).getByPlaceholderText('Filter models...'),
    'test-model'
  )
  const policyButton = within(testDialog).getByRole('button', {
    name: '探测策略',
  })
  policyButton.focus()
  await user.keyboard('{Enter}')
  expect(await screen.findByLabelText('输入阈值（k tokens）')).toHaveValue(
    '1.001'
  )
  await user.click(screen.getByRole('switch', { name: '禁止自动探测' }))
  expect(screen.queryByLabelText('自定义响应内容')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '保存策略' }))
  await waitFor(() => expect(saved).toHaveLength(1))
  expect(saved[0]).toMatchObject({
    auto_probe_disabled: false,
    small_input_response_enabled: false,
    small_input_response_text: '第一行\n第二行',
    probe_policy_revision: 4,
  })
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: '渠道探测策略' })
    ).not.toBeInTheDocument()
  )
  expect(testDialog).toBeVisible()
  expect(
    within(testDialog).getByPlaceholderText('Filter models...')
  ).toHaveValue('test-model')
  expect(
    within(testDialog).getByRole('button', { name: 'Test Connection' })
  ).toBeEnabled()
  expect(close).not.toHaveBeenCalled()
  await waitFor(() => expect(policyButton).toHaveFocus())
})

test('配置读取失败时显示重试入口，不提交默认值覆盖已有配置', async () => {
  api.defaults.adapter = async () => {
    throw new Error('读取失败')
  }
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ChannelProbePolicyDialog
        open
        channel={{ id: 17, name: '禁探测渠道' } as ChannelMonitorItem}
        onOpenChange={() => {}}
      />
    </QueryClientProvider>
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('配置加载失败')
  expect(screen.getByRole('button', { name: '重新加载' })).toBeEnabled()
  expect(
    screen.queryByRole('button', { name: '保存策略' })
  ).not.toBeInTheDocument()
})

test('只有本地响应时上游保持无业务样本，不显示虚假的成功率', () => {
  const period: ChannelPassivePeriod = {
    source: 'redis_business',
    period_start: 60,
    period_end: 120,
    resolution: 'period',
    coverage: 'complete',
    success: 0,
    failure: 0,
    local_responses: 100,
    first_token_samples: 0,
    duration_samples: 0,
    tps_samples: 0,
    avg_first_token_ms: null,
    avg_duration_ms: null,
    avg_tps: null,
    success_rate: null,
    processed_at: 125,
    data_cutoff_at: 120,
    version: 100,
  }
  render(<ChannelPassivePeriodMetrics period={period} />)
  expect(screen.getByText('暂无业务请求 · 完整覆盖')).toBeVisible()
  expect(screen.getByText('100')).toBeVisible()
  expect(screen.queryByText('100.0%')).not.toBeInTheDocument()
})
