import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UpstreamAccount } from '../../api-upstream-accounts'
import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { emptyUpstreamAutomation } from '../../lib/automation'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../../lib/custom-upstream'
import UpstreamAccountsDialog from '../upstream-accounts-dialog'
import UpstreamAutomationsDialog from '../upstream-automations-dialog'

function renderList(kind: '账户' | '任务', name = '主账户') {
  const channel = customVariableChannel()
  if (!channel.upstream) throw new Error('fixture requires upstream')
  const account: UpstreamAccount = {
    id: 8,
    name,
    revision: 1,
    channel_ids: [channel.id, 42],
    channel_revisions: {},
    upstream: channel.upstream,
    balance: 70,
    has_balance_key: false,
    last_balance_time: 0,
    last_balance_error: '',
    proxy: '',
    refresh_interval_minutes: 5,
  }
  const config = createChannelMonitorCustomFormConfig(undefined)
  config.actions = [createChannelMonitorCustomAction()]
  const task = {
    ...emptyUpstreamAutomation(),
    id: 'daily',
    name,
    channel_ids: account.channel_ids,
    base_url: 'https://upstream.example',
    custom_config: createChannelMonitorCustomRequestConfig(config),
    state: {
      ...emptyUpstreamAutomation().state,
      message: '余额充足，无需重置',
      history: [
        {
          id: 'check',
          time: 100,
          status: 'checked',
          message: '最近一次检查成功',
        },
      ],
    },
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data:
        kind === '账户'
          ? [
              account,
              {
                ...account,
                id: 9,
                name: '备用账户',
                channel_ids: [],
                upstream: {
                  ...account.upstream,
                  base_url: 'https://backup.example',
                },
              },
            ]
          : [
              task,
              {
                ...task,
                id: 'backup',
                name: '备用账户',
                channel_ids: [],
                base_url: 'https://backup.example',
              },
            ],
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      {kind === '账户' ? (
        <UpstreamAccountsDialog
          channels={[channel]}
          onOpenChange={() => undefined}
        />
      ) : (
        <UpstreamAutomationsDialog
          channels={[channel]}
          onOpenChange={() => undefined}
        />
      )}
    </QueryClientProvider>
  )
}

test('账户列表默认收起渠道明细，键盘展开后可查看已删除渠道的 ID', async () => {
  renderList('账户')
  const user = userEvent.setup()
  await screen.findByText('主账户')
  expect(screen.queryByText('测试渠道')).not.toBeInTheDocument()
  const expand = screen.getByRole('button', { name: '关联渠道（2）' })
  expect(expand).toHaveAttribute('aria-expanded', 'false')
  expect(screen.getAllByLabelText('上游余额')[0]).toHaveTextContent('70')
  expand.focus()
  await user.keyboard('{Enter}')
  expect(expand).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByText('测试渠道')).toBeVisible()
  expect(screen.getByText('#42')).toBeVisible()
})

test('任务列表默认收起规则和历史，展开后可查看完整详情', async () => {
  renderList('任务')
  const user = userEvent.setup()
  await screen.findByText('主账户')
  expect(screen.getAllByRole('status')[0]).toHaveTextContent(
    '余额充足，无需重置'
  )
  expect(screen.queryByText(/余额不足时重置/)).not.toBeInTheDocument()
  const expand = screen.getAllByRole('button', { name: '规则与记录（1）' })[0]
  expect(expand).toHaveAttribute('aria-expanded', 'false')
  await user.click(expand)
  expect(expand).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByText(/余额不足时重置/)).toBeVisible()
  await user.click(screen.getByText('最近检查记录'))
  expect(screen.getByText('最近一次检查成功')).toBeVisible()
})

test.each(['账户', '任务'] as const)(
  '%s 列表可按关联渠道名称、ID 和地址搜索，无匹配后可清空恢复',
  async (kind) => {
    renderList(kind)
    const user = userEvent.setup()
    await screen.findByText('主账户')
    const search = screen.getByRole('searchbox', { name: `搜索上游${kind}` })
    for (const keyword of ['测试渠道', '42', 'UPSTREAM.EXAMPLE', '主账户']) {
      await user.clear(search)
      await user.type(search, keyword)
      expect(screen.getByText('主账户')).toBeVisible()
      expect(screen.queryByText('备用账户')).not.toBeInTheDocument()
    }
    await user.clear(search)
    await user.type(search, '不存在的名称')
    expect(screen.getByText(`没有匹配的${kind}`)).toBeVisible()
    await user.click(screen.getByRole('button', { name: '清空搜索' }))
    const list = screen.getByRole('list', {
      name: kind === '账户' ? '上游账户列表' : '上游自动任务列表',
    })
    expect(within(list).getAllByRole('listitem')).toHaveLength(2)
  }
)

test.each(['账户', '任务'] as const)(
  '%s 长名称保持单行省略并可查看完整名称',
  async (kind) => {
    const name = '超长上游账户名称'.repeat(12)
    renderList(kind, name)
    const title = await screen.findByTitle(name)
    expect(title).toHaveTextContent(name)
    expect(title).toHaveClass('truncate')
  }
)
