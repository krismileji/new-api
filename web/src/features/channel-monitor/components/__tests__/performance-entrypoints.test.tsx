import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type {
  ChannelMonitorItem,
  ChannelMonitorPerformanceMetric,
} from '../../types'
import { ChannelMonitorChannelView } from '../channel-monitor-channel-view'
import { ChannelMonitorModelPerformanceView } from '../channel-monitor-model-performance-view'

const channel: ChannelMonitorItem = {
  id: 7,
  name: '生产渠道',
  type: 1,
  status: 1,
  priority: 0,
  weight: 0,
  base_url: '',
  models: 'model-a',
  test_model: 'model-a',
  groups: ['default'],
  ratio: 1,
  previous_ratio: 1,
  cost_ratio: 1,
  previous_cost_ratio: 1,
  conversion_factor: 1,
  remark: '',
  channel_remark: '',
  updated_time: 0,
  updated_by: 0,
  updated_by_username: '',
  last_fetch_status: '',
  last_fetch_error: '',
  last_fetch_time: 0,
  consecutive_failures: 0,
  upstream_balance: null,
  last_balance_time: 0,
  last_balance_error: '',
  today_cost_cny: 0,
  today_cost_configured: false,
  today_cost_complete: false,
  today_cost_unresolved_count: 0,
  concurrency_limit: 0,
  concurrency_active: 0,
  current_rpm: 0,
  upstream: null,
}
const metric: ChannelMonitorPerformanceMetric = {
  channel_id: 7,
  model_name: 'model-a',
  sample_count: 10,
  first_token_sample_count: 8,
  tps_sample_count: 6,
  average_first_token_ms: 500,
  average_tps: 50,
  tps_output_tokens: 3000,
  tps_generation_duration_ms: 60000,
  latest_first_token_ms: 500,
  latest_tps: 50,
  last_used_time: 1,
}
const noop = () => undefined

afterEach(cleanup)

test('channel performance opens the selected channel with the keyboard', async () => {
  const open = vi.fn()
  render(
    <ChannelMonitorChannelView
      channels={[channel]}
      groupRatios={{}}
      groupCoefficients={{}}
      performanceByChannel={new Map([[7, metric]])}
      successByChannel={new Map()}
      successMetricsAvailable
      performanceRangeLabel='近30分钟'
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
      onOpenSuccessDetail={noop}
      onOpenPerformanceDetail={open}
      fetchingBalanceChannelId={null}
      fetchingRatioChannelId={null}
      updatingStatusChannelId={null}
    />
  )
  const trigger = screen.getByRole('button', {
    name: '查看 生产渠道 的性能明细',
  })
  expect(trigger).toHaveAttribute('aria-haspopup', 'dialog')
  trigger.focus()
  await userEvent.keyboard('{Enter}')
  expect(open).toHaveBeenCalledWith(channel)
})

test('model latency and TPS open the selected model and disable when its measurements are absent', async () => {
  const open = vi.fn()
  const props = {
    channels: [channel],
    metrics: [metric],
    successMetrics: [],
    successMetricsAvailable: true,
    selectedModel: 'model-a',
    search: '',
    isLoading: false,
    isError: false,
    onOpenSuccessDetail: noop,
    onOpenPerformanceDetail: open,
  }
  const view = render(<ChannelMonitorModelPerformanceView {...props} />)
  const user = userEvent.setup()
  for (const label of ['首字', 'TPS ']) {
    const trigger = screen.getByRole('button', {
      name: `查看 生产渠道 的 model-a ${label}性能明细`,
    })
    await user.click(trigger)
    expect(open).toHaveBeenLastCalledWith(channel, 'model-a')
  }
  view.rerender(<ChannelMonitorModelPerformanceView {...props} metrics={[]} />)
  for (const trigger of screen.getAllByRole('button', { name: /性能明细/ })) {
    expect(trigger).toBeDisabled()
  }
})
