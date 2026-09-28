import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { useChannelMonitorPrivacyStore } from '@/stores/channel-monitor-privacy-store'

import type {
  ChannelMonitorItem,
  ChannelMonitorSuccessSummary,
} from '../../types'
import { ChannelMonitorChannelView } from '../channel-monitor-channel-view'
import { ChannelMonitorGroupView } from '../channel-monitor-group-view'
import { ChannelMonitorModelPerformanceView } from '../channel-monitor-model-performance-view'
import {
  ChannelMonitorPrivacyProvider,
  ChannelMonitorPrivacyToggle,
} from '../channel-monitor-privacy'

const channel: ChannelMonitorItem = {
  id: 7,
  name: '私有上游渠道',
  type: 1,
  status: 3,
  status_reason: '私有账户余额不足',
  priority: 0,
  weight: 0,
  base_url: 'https://upstream.example.com',
  models: 'test-model',
  test_model: 'test-model',
  groups: ['私有分组'],
  ratio: 1.25,
  previous_ratio: 1,
  cost_ratio: 1.25,
  previous_cost_ratio: 1,
  conversion_factor: 1,
  remark: '私有监控备注',
  channel_remark: '私有渠道备注',
  updated_time: 0,
  updated_by: 1,
  updated_by_username: '私有操作用户',
  last_fetch_status: 'succeeded',
  last_fetch_error: '',
  last_fetch_time: 0,
  consecutive_failures: 0,
  upstream_balance: 42.5,
  last_balance_time: 0,
  last_balance_error: '',
  today_cost_cny: 1.23456,
  today_cost_configured: true,
  today_cost_complete: true,
  today_cost_unresolved_count: 0,
  concurrency_limit: 10,
  concurrency_active: 2,
  current_rpm: 9,
  rpm_limit: 100,
  upstream: {
    type: 'new_api',
    base_url: 'https://upstream.example.com',
    group: '私有上游组',
    auth_type: 'api_key',
    user_id: 0,
    has_access_token: true,
    account: '',
    has_password: false,
    single_channel_action: 'update_group_ratio',
    multiple_channels_action: 'disable_channel',
    balance_warning_threshold: null,
    balance_auto_disable_threshold: null,
    ratio_sync_enabled: true,
    balance_sync_enabled: true,
    cost_conversion: { mode: 'none' },
  },
}
const success: ChannelMonitorSuccessSummary = {
  actual_success_count: 9,
  actual_failure_count: 1,
  actual_sample_count: 10,
  actual_success_rate: 0.9,
  final_success_count: 9,
  final_failure_count: 1,
  final_sample_count: 10,
  final_success_rate: 0.9,
  cache_hit_count: 1,
  cache_sample_count: 2,
  cache_hit_rate: 0.5,
  cache_read_tokens: 50,
  input_tokens: 100,
  cache_utilization_rate: 0.5,
}
const performance = {
  sample_count: 10,
  first_token_sample_count: 10,
  tps_sample_count: 10,
  average_first_token_ms: 500,
  average_tps: 24,
  last_used_time: 0,
}
const noop = () => {}
const clients: QueryClient[] = []

function renderPrivateView(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <ChannelMonitorPrivacyProvider>
        <ChannelMonitorPrivacyToggle />
        {children}
      </ChannelMonitorPrivacyProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  for (const client of clients) client.clear()
  clients.length = 0
  useChannelMonitorPrivacyStore.setState({ enabled: false })
  localStorage.clear()
})

test('channel screenshot hides names, remarks, money, ratios, groups and hover details while keeping operating metrics', async () => {
  const user = userEvent.setup()
  const onOpenPerformanceDetail = vi.fn()
  renderPrivateView(
    <ChannelMonitorChannelView
      channels={[channel]}
      groupRatios={{ 私有分组: 2.5 }}
      groupCoefficients={{ 私有分组: 1.5 }}
      performanceByChannel={new Map([[7, performance]])}
      successByChannel={new Map([[7, success]])}
      successMetricsAvailable
      performanceRangeLabel='15 分钟'
      performanceLoading={false}
      performanceError={false}
      smartScheduleRoutesByChannel={new Map()}
      smartScheduleSelectedGroupModel={null}
      smartScheduleUpdatePending={false}
      onUpdateSmartSchedule={noop}
      onFetchUpstreamBalance={noop}
      onFetchUpstreamRatio={noop}
      onToggleStatus={noop}
      onTestConnection={noop}
      onEditConcurrency={noop}
      onEditGroups={noop}
      onConfigureUpstream={noop}
      onViewHistory={noop}
      onOpenCostHistory={noop}
      onOpenProfitHistory={noop}
      onOpenSuccessDetail={noop}
      onOpenPerformanceDetail={onOpenPerformanceDetail}
      fetchingBalanceChannelId={null}
      fetchingRatioChannelId={null}
      updatingStatusChannelId={null}
      profitByChannel={
        new Map([
          [
            7,
            {
              ...success,
              cache_write_request_count: 0,
              cost_nano_cny: 2e9,
              settled_count: 1,
              unresolved_count: 0,
              income_nano_cny: 5e9,
              profit_nano_cny: 3e9,
              profit_confirmed: true,
            },
          ],
        ])
      }
    />
  )
  expect(screen.getByText('私有上游渠道')).toBeVisible()
  expect(screen.getByText('¥3.0000')).toBeVisible()
  await user.click(screen.getByRole('button', { name: '隐藏敏感信息' }))
  const table = screen.getByRole('table')
  expect(table).not.toHaveTextContent(
    /私有|42\.5|1\.23456|1\.25|2\.5|3\.0000|5\.0000|ID 7/
  )
  expect(screen.queryByTitle('私有上游渠道')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /私有/ })).not.toBeInTheDocument()
  expect(within(table).getByText('90%')).toBeVisible()
  expect(within(table).getByText('当前并发：2 / 10')).toBeVisible()
  expect(within(table).getByText('当前 RPM：9 / 100')).toBeVisible()
  const detail = screen.getByRole('button', { name: '查看性能明细' })
  expect(detail).not.toHaveAttribute('title', expect.stringContaining('私有'))
  await user.click(detail)
  expect(onOpenPerformanceDetail).toHaveBeenCalledWith(channel)
  await user.tab({ shift: true })
  expect(screen.getByLabelText('系统禁用，原因：敏感信息已隐藏')).toHaveFocus()
  expect(await screen.findByText('系统禁用原因：敏感信息已隐藏')).toBeVisible()
  await user.click(screen.getByRole('button', { name: '显示敏感信息' }))
  expect(screen.getByText('私有上游渠道')).toBeVisible()
  expect(screen.getByText('备注：私有渠道备注')).toBeVisible()
  expect(screen.getByText('¥3.0000')).toBeVisible()
})

test('group screenshot hides group coefficients and related channel hints while retaining success rates', async () => {
  renderPrivateView(
    <ChannelMonitorGroupView
      groups={[
        { name: '私有分组', ratio: 2.5, coefficient: 1.5, channels: [channel] },
      ]}
      successByGroup={
        new Map([['私有分组', { ...success, group: '私有分组' }]])
      }
      successMetricsAvailable
      successLoading={false}
      successError={false}
      successRangeLabel='15 分钟'
      onOpenSuccessDetail={noop}
      onOpenScheduleSettings={noop}
      onEditChannels={noop}
      onEditGroup={noop}
      onSyncGroup={noop}
    />
  )
  expect(screen.getByText('私有分组')).toBeVisible()
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: '隐藏敏感信息' }))
  expect(screen.getByRole('table')).not.toHaveTextContent(
    /私有|1\.25|2\.5|1\.5/
  )
  expect(screen.queryByTitle(/私有/)).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /私有/ })).not.toBeInTheDocument()
  expect(screen.getAllByText('90%')).toHaveLength(2)
})

test('model screenshot masks channel identity and ratio without losing latency, TPS or success details', async () => {
  renderPrivateView(
    <ChannelMonitorModelPerformanceView
      channels={[channel]}
      selectedModel='test-model'
      search=''
      isLoading={false}
      isError={false}
      metrics={[
        {
          ...performance,
          channel_id: 7,
          model_name: 'test-model',
          tps_output_tokens: 24,
          tps_generation_duration_ms: 1000,
          latest_first_token_ms: 500,
          latest_tps: 24,
        },
      ]}
      successMetrics={[{ ...success, channel_id: 7, model_name: 'test-model' }]}
      successMetricsAvailable
      onOpenSuccessDetail={noop}
      onOpenPerformanceDetail={noop}
    />
  )
  expect(screen.getByText('私有上游渠道')).toBeVisible()
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: '隐藏敏感信息' }))
  expect(screen.getByRole('table')).not.toHaveTextContent(/私有|ID 7|1\.25/)
  expect(screen.queryByTitle(/私有/)).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /私有/ })).not.toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: '查看性能明细' })).toHaveLength(
    2
  )
  expect(screen.getByText('90%')).toBeVisible()
})
