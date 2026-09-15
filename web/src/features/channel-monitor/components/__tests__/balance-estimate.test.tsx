import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { ChannelMonitorBalanceEstimate } from '../../types'
import { ChannelMonitorBalanceCell } from '../channel-monitor-balance-cell'

const estimate: ChannelMonitorBalanceEstimate = {
  available: true,
  complete: true,
  upstream_balance: 24.374499,
  estimated_balance: 20.874499,
  completed_consumption: 2,
  in_flight_consumption: 1.5,
  uncertain_consumption: 0,
  in_flight_count: 1,
  average_count: 1,
  budget_count: 0,
  unknown_count: 0,
  synced_at: 1750000000,
  last_sample_count: 5,
  last_estimate_model: 'model-a',
  last_estimate_amount: 1.5,
  last_estimate_source: 'average',
}

describe('渠道余额预估展示', () => {
  it('进行中预估变化时只更新估算可用，保留上游原始余额', () => {
    const props = { balance: 24.374499, enabled: true, warning: 30, estimate }
    const view = render(<ChannelMonitorBalanceCell {...props} />)
    expect(screen.getByLabelText('上游余额')).toHaveTextContent('24.374499')
    expect(screen.getByLabelText('估算可用余额')).toHaveTextContent('20.874499')
    view.rerender(
      <ChannelMonitorBalanceCell
        {...props}
        estimate={{
          ...estimate,
          in_flight_consumption: 3,
          estimated_balance: 19.374499,
        }}
      />
    )
    expect(screen.getByLabelText('上游余额')).toHaveTextContent('24.374499')
    expect(screen.getByLabelText('估算可用余额')).toHaveTextContent('19.374499')
  })

  it('未知或缺失预估显示不可用，不把它显示成零消费', () => {
    render(<ChannelMonitorBalanceCell balance={24} enabled />)
    expect(screen.getByLabelText('上游余额')).toHaveTextContent('24')
    expect(screen.getByText('预估不可用')).toBeVisible()
    expect(screen.queryByLabelText('估算可用余额')).not.toBeInTheDocument()
  })

  it('数据不完整时明确标记，展开详情显示查询窗口费用和参考样本', async () => {
    const user = userEvent.setup()
    render(
      <ChannelMonitorBalanceCell
        balance={24.374499}
        enabled
        estimate={{
          ...estimate,
          complete: false,
          unknown_count: 1,
          uncertain_consumption: 2,
        }}
      />
    )
    expect(screen.getByText('预估不完整')).toBeVisible()
    expect(screen.queryByText(/查询期间待确认/)).not.toBeInTheDocument()
    await user.tab()
    expect(screen.getByRole('button', { name: /估算可用/ })).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(screen.getByText(/查询期间待确认/)).toBeVisible()
    expect(screen.getByRole('button', { name: /估算可用/ })).toHaveAttribute(
      'aria-expanded',
      'true'
    )
    expect(screen.getByText(/查询期间待确认/)).toHaveTextContent('2')
    expect(screen.getByText(/未确认请求/)).toHaveTextContent('1')
    expect(screen.getByText(/最近一次预估参考/)).toHaveTextContent(
      'model-a，5 笔样本'
    )
    expect(screen.getByText(/最近一次预估参考/)).toHaveTextContent(
      '平均单笔费用：1.5'
    )
  })

  it('上游刷新失败时保留原余额并显示失败原因', () => {
    render(<ChannelMonitorBalanceCell balance={24} enabled error='上游超时' />)
    expect(screen.getByLabelText('上游余额')).toHaveTextContent('24')
    expect(screen.getByText('更新失败')).toHaveAttribute('title', '上游超时')
  })

  it('关闭余额同步后保留上游快照并隐藏预估', () => {
    render(
      <ChannelMonitorBalanceCell
        balance={24}
        enabled={false}
        estimate={estimate}
      />
    )
    expect(screen.getByLabelText('上游余额')).toHaveTextContent('24')
    expect(screen.queryByLabelText('估算可用余额')).not.toBeInTheDocument()
    expect(screen.queryByText('预估不可用')).not.toBeInTheDocument()
  })
})
