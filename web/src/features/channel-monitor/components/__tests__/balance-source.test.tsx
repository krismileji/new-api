import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UpstreamAccount } from '../../api-upstream-accounts'
import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { emptyUpstreamAutomation } from '../../lib/automation'
import {
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../../lib/custom-upstream'
import { UpstreamAccountEditor } from '../upstream-account-editor'
import { UpstreamAutomationEditor } from '../upstream-automation-editor'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

function balanceSourceFixture() {
  const channel = customVariableChannel()
  if (!channel.upstream) throw new Error('fixture requires upstream')
  const config = createChannelMonitorCustomFormConfig(undefined)
  config.ratio.fixedValue = 2.5
  config.balance.fixedValue = 42
  channel.upstream.custom_config =
    createChannelMonitorCustomRequestConfig(config)
  const account: UpstreamAccount = {
    id: 8,
    name: '共享钱包',
    revision: 3,
    channel_ids: [],
    channel_revisions: {},
    balance: 70,
    has_balance_key: false,
    last_balance_time: 1,
    last_balance_error: '',
    proxy: '',
    refresh_interval_minutes: 5,
    upstream: {
      ...channel.upstream,
      base_url: 'https://wallet.example',
      cost_conversion: { mode: 'recharge', paid_cny: 6, credited_usd: 2 },
    },
  }
  return { channel, account }
}

function balanceSourceClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

describe('自定义上游余额来源', () => {
  test('不同上游类型的余额成员仍显示在账户关联列表中', () => {
    const { channel, account } = balanceSourceFixture()
    if (!channel.upstream?.custom_config) {
      throw new Error('fixture requires custom config')
    }
    channel.upstream.upstream_account_id = account.id
    channel.upstream.custom_config.balance = {
      source: 'account',
      account_id: account.id,
    }
    account.upstream = { ...account.upstream, type: 'new_api' }
    account.channel_ids = [channel.id]
    render(
      <QueryClientProvider client={balanceSourceClient()}>
        <UpstreamAccountEditor
          account={account}
          channels={[channel]}
          onClose={() => undefined}
          onSaved={() => undefined}
        />
      </QueryClientProvider>
    )
    expect(
      screen.getByRole('checkbox', { name: /测试渠道.*仅关联余额/ })
    ).toHaveAttribute('aria-checked', 'true')
    expect(
      screen.getByRole('checkbox', { name: /测试渠道.*仅关联余额/ })
    ).toHaveAttribute('aria-disabled', 'true')
  })
  test('关联账户后保留独立地址和倍率，切换来源保留输入，保存账户引用', async () => {
    const { channel, account } = balanceSourceFixture()
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: String(url).endsWith('/upstream_accounts') ? [account] : [],
      },
    }))
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={balanceSourceClient()}>
        <UpstreamConfigDialog
          channel={channel}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
    await user.click(screen.getByRole('button', { name: '关联上游账户' }))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: '上游余额账户' })
      ).toBeEnabled()
    )
    await user.click(screen.getByRole('combobox', { name: '上游余额账户' }))
    await user.click(
      await screen.findByRole('option', { name: '共享钱包 · #8' })
    )
    expect(screen.getByLabelText('接口基础地址')).toHaveValue(
      'https://upstream.example'
    )
    expect(screen.getByLabelText('接口基础地址')).toBeEnabled()
    expect(screen.getByLabelText('固定倍率')).toHaveValue(2.5)
    expect(screen.getByLabelText('余额预警值')).toBeEnabled()
    expect(screen.getByText(/换算系数：3/)).toBeInTheDocument()
    const balance = screen.getByRole('group', { name: '上游余额来源' })
    await user.click(within(balance).getByRole('button', { name: '固定输入' }))
    expect(screen.getByLabelText('固定余额')).toHaveValue(42)
    await user.click(screen.getByRole('button', { name: '关联上游账户' }))
    expect(
      screen.getByRole('combobox', { name: '上游余额账户' })
    ).toHaveTextContent('共享钱包 · #8')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalledOnce())
    expect(put.mock.calls[0][1]).toMatchObject({
      base_url: 'https://upstream.example',
      custom_config: {
        ratio: { source: 'fixed', fixed_value: 2.5 },
        balance: { source: 'account', account_id: 8 },
      },
    })
  })

  test('已关联余额的渠道重新编辑时保留账户选择并允许切回自定义', async () => {
    const { channel, account } = balanceSourceFixture()
    if (!channel.upstream?.custom_config) {
      throw new Error('fixture requires custom config')
    }
    channel.upstream.upstream_account_id = account.id
    channel.upstream.custom_config.balance = {
      source: 'account',
      account_id: account.id,
    }
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: String(url).endsWith('/upstream_accounts') ? [account] : [],
      },
    }))
    render(
      <QueryClientProvider client={balanceSourceClient()}>
        <UpstreamConfigDialog
          channel={channel}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: '上游余额账户' })
      ).toHaveTextContent('共享钱包 · #8')
    )
    expect(screen.getByLabelText('接口基础地址')).toBeEnabled()
    expect(
      screen.getByRole('combobox', { name: '上游余额账户' })
    ).toHaveTextContent('共享钱包 · #8')
    const user = userEvent.setup()
    await user.click(
      within(screen.getByRole('group', { name: '上游余额来源' })).getByRole(
        'button',
        { name: '接口查询' }
      )
    )
    expect(screen.queryByLabelText('上游余额账户')).not.toBeInTheDocument()
    expect(screen.getByLabelText('接口基础地址')).toBeEnabled()
  })

  test('独立自动任务可只关联余额，保存时保留规则及任务地址', async () => {
    const { account } = balanceSourceFixture()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [account] },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    const task = {
      ...emptyUpstreamAutomation(),
      name: '独立重置任务',
      base_url: 'https://actions.example',
    }
    render(
      <QueryClientProvider client={balanceSourceClient()}>
        <UpstreamAutomationEditor
          task={task}
          channels={[]}
          onCancel={() => undefined}
          onSaved={() => undefined}
        />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '关联上游账户' }))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: '上游余额账户' })
      ).toBeEnabled()
    )
    await user.click(screen.getByRole('combobox', { name: '上游余额账户' }))
    await user.click(
      await screen.findByRole('option', { name: '共享钱包 · #8' })
    )
    await user.click(screen.getByRole('button', { name: '添加触发规则' }))
    await user.click(screen.getByRole('button', { name: '保存任务' }))
    await waitFor(() => expect(put).toHaveBeenCalledOnce())
    expect(put.mock.calls[0][1]).toMatchObject({
      base_url: 'https://actions.example',
      custom_config: {
        balance: { source: 'account', account_id: 8 },
        actions: [{ name: '余额不足时重置' }],
      },
    })
    expect(put.mock.calls[0][1]).not.toHaveProperty('account_id', 8)
  })

  test('账户列表失败可重试，空列表和未选择账户会提示', async () => {
    const { channel } = balanceSourceFixture()
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: !String(url).endsWith('/upstream_accounts'),
        data: [],
        message: '暂时不可用',
      },
    }))
    const put = vi.spyOn(api, 'put')
    render(
      <QueryClientProvider client={balanceSourceClient()}>
        <UpstreamConfigDialog
          channel={channel}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '关联上游账户' }))
    await screen.findByText('账户加载失败。')
    expect(screen.getByLabelText('上游余额账户')).toBeDisabled()
    get.mockResolvedValue({ data: { success: true, data: [] } })
    await user.click(screen.getByRole('button', { name: '重试加载账户' }))
    await screen.findByText(/尚无上游账户/)
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByText('请选择上游余额账户')).toBeInTheDocument()
    expect(put).not.toHaveBeenCalled()
  })
})
