import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { api } from '@/lib/api'

import type { ChannelMonitorApiResponse } from '../types'
import type {
  ChannelPassiveOverview,
  ChannelPassivePeriod,
  ChannelPassiveView,
} from '../types-passive'

const COVERAGE = {
  complete: '完整覆盖',
  partial: '覆盖不完整',
  collecting: '采集中',
  unavailable: '数据不可用',
}

export function ChannelPassivePeriodMetrics(props: {
  period: ChannelPassivePeriod
}) {
  const period = props.period
  const format = (value: number | null, unit: string) =>
    value == null ? '暂无有效样本' : `${value.toFixed(1)} ${unit}`
  let status = '暂无业务请求'
  if (period.success + period.failure > 0) {
    status = period.failure === 0 ? '业务请求正常' : '业务请求存在失败'
  }
  if (period.coverage !== 'complete') status = COVERAGE[period.coverage]
  return (
    <div className='space-y-2 text-sm'>
      <p title={period.reason}>
        {status} · {COVERAGE[period.coverage]}
      </p>
      <p className='text-muted-foreground text-xs'>
        {new Date(period.period_start * 1000).toLocaleString()} ～{' '}
        {new Date(period.period_end * 1000).toLocaleString()}
      </p>
      {period.resolution === 'hour' &&
        period.sample_window_start != null &&
        period.sample_window_end != null &&
        period.sample_window_end > period.sample_window_start && (
          <p className='text-muted-foreground text-xs'>
            业务采样范围：
            {new Date(period.sample_window_start * 1000).toLocaleString()} ～{' '}
            {new Date(period.sample_window_end * 1000).toLocaleString()}
          </p>
        )}
      <dl className='grid grid-cols-2 gap-x-4 gap-y-2'>
        <div>
          <dt className='text-muted-foreground'>成功率 / 请求数</dt>
          <dd>
            {period.success_rate == null
              ? '—'
              : `${(period.success_rate * 100).toFixed(1)}%`}{' '}
            / {period.success + period.failure}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>本地响应次数</dt>
          <dd>{period.local_responses}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            平均首字 · {period.first_token_samples} 个样本
          </dt>
          <dd>{format(period.avg_first_token_ms, 'ms')}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            加权 TPS · {period.tps_samples} 个样本
          </dt>
          <dd>{format(period.avg_tps, 'tokens/s')}</dd>
        </div>
        <div className='col-span-2'>
          <dt className='text-muted-foreground'>
            平均上游尝试耗时 · {period.duration_samples} 个样本
          </dt>
          <dd>{format(period.avg_duration_ms, 'ms')}</dd>
        </div>
      </dl>
      {period.reason && (
        <p className='text-muted-foreground text-xs'>{period.reason}</p>
      )}
    </div>
  )
}

export function ChannelPassiveMonitorPanel(props: {
  scope: 'status' | 'group'
  model?: string
  group?: string
  channelId?: number
}) {
  const [page, setPage] = useState(1)
  const [history, setHistory] = useState<ChannelPassiveView | null>(null)
  const query = useQuery({
    queryKey: [
      'channel-monitor',
      'passive',
      props.scope,
      props.model,
      props.group,
      props.channelId,
      page,
    ],
    queryFn: async () => {
      const response = await api.get<
        ChannelMonitorApiResponse<ChannelPassiveOverview>
      >('/api/channel_monitor/passive', {
        params: {
          scope: props.scope,
          model: props.model,
          group: props.group,
          channel_id: props.channelId,
          page,
        },
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      if (!response.data.success) {
        throw new Error(response.data.message || '读取业务周期监测失败')
      }
      return response.data.data
    },
    refetchInterval: 15_000,
  })
  if (query.isPending) {
    return (
      <div role='status' className='flex items-center gap-2 text-sm'>
        <Spinner />
        正在读取业务周期监测
      </div>
    )
  }
  if (query.isError) {
    return (
      <p role='alert'>
        业务周期监测读取失败。
        <Button variant='link' onClick={() => query.refetch()}>
          重试
        </Button>
      </p>
    )
  }
  const data = query.data
  if (!data || (data.total === 0 && !data.unavailable_reason)) return null
  return (
    <section aria-label='禁自动探测渠道的业务周期监测' className='space-y-3'>
      <div className='space-y-1'>
        <h3 className='font-semibold'>业务周期监测 · Redis</h3>
        <p className='text-muted-foreground text-sm'>
          仅统计禁自动探测渠道的真实上游业务；手动检测、本地响应与实际探测单独显示。
        </p>
      </div>
      {data.unavailable_reason && (
        <p role='status'>{data.unavailable_reason}</p>
      )}
      <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'>
        {data.items.map((item) => (
          <Card key={item.target.id}>
            <CardHeader className='gap-2'>
              <CardTitle className='text-sm break-words'>
                {item.target.group_name ? `${item.target.group_name} · ` : ''}
                {item.target.channel_id
                  ? `渠道 ${item.target.channel_id} · `
                  : ''}
                {item.target.model_name}
              </CardTitle>
              <p className='text-muted-foreground text-xs'>
                {item.target.scope === 'group_final'
                  ? '全禁探测分组 · 最终请求成功率'
                  : '成员实际尝试成功率'}{' '}
                · 每 {item.target.interval_seconds} 秒 · 配置 v
                {item.target.config_revision}
              </p>
            </CardHeader>
            <CardContent className='space-y-3'>
              {item.periods[0] && (
                <ChannelPassivePeriodMetrics period={item.periods[0]} />
              )}
              {item.periods[1] && (
                <p className='text-muted-foreground text-xs'>
                  当前周期（{COVERAGE[item.periods[1].coverage]}）：
                  {item.periods[1].success + item.periods[1].failure}{' '}
                  个上游样本，{item.periods[1].local_responses} 次本地响应
                </p>
              )}
              <Button
                size='sm'
                variant='outline'
                onClick={() => setHistory(item)}
              >
                查看历史汇总
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>
      {data.total > 50 && (
        <div className='flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            disabled={page <= 1}
            onClick={() => setPage(page - 1)}
          >
            上一页
          </Button>
          <span>
            {page} / {Math.ceil(data.total / 50)}
          </span>
          <Button
            variant='outline'
            disabled={page * 50 >= data.total}
            onClick={() => setPage(page + 1)}
          >
            下一页
          </Button>
        </div>
      )}
      {history && (
        <ChannelPassiveHistory
          item={history}
          onClose={() => setHistory(null)}
        />
      )}
    </section>
  )
}

function ChannelPassiveHistory(props: {
  item: ChannelPassiveView
  onClose: () => void
}) {
  const [days, setDays] = useState(1)
  const [targetId, setTargetId] = useState(props.item.target.id)
  const versions = useQuery({
    queryKey: ['channel-monitor', 'passive-versions', props.item.target.id],
    queryFn: async () => {
      const response = await api.get<
        ChannelMonitorApiResponse<ChannelPassiveView['target'][]>
      >(`/api/channel_monitor/passive/${props.item.target.id}/versions`, {
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      if (!response.data.success) {
        throw new Error(response.data.message || '历史配置读取失败')
      }
      return response.data.data
    },
  })
  const query = useQuery({
    queryKey: ['channel-monitor', 'passive-history', targetId, days],
    queryFn: async () => {
      const response = await api.get<
        ChannelMonitorApiResponse<ChannelPassiveView>
      >(`/api/channel_monitor/passive/${targetId}/history`, {
        params: { days },
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      if (!response.data.success) {
        throw new Error(response.data.message || '历史数据不可用')
      }
      return response.data.data
    },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='max-h-[90dvh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            业务周期历史 · {props.item.target.model_name}
          </DialogTitle>
          <DialogDescription>
            按周期结束时间归入小时汇总；每格使用原始总量重新计算均值，不拆分跨小时长周期。
          </DialogDescription>
        </DialogHeader>
        <NativeSelect
          aria-label='历史配置版本'
          value={targetId}
          onChange={(event) => setTargetId(event.target.value)}
        >
          {[
            props.item.target,
            ...(versions.data ?? []).filter(
              (target) => target.id !== props.item.target.id
            ),
          ].map((target) => (
            <NativeSelectOption key={target.id} value={target.id}>
              {target.model_name} · {target.interval_seconds} 秒 · 配置 v
              {target.config_revision} / 策略 v{target.policy_revision ?? 0} ·{' '}
              {new Date(target.effective_at * 1000).toLocaleString()}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {versions.isError && (
          <p role='alert'>历史配置读取失败，当前配置仍可查看。</p>
        )}
        <NativeSelect
          aria-label='历史展示范围'
          value={days}
          onChange={(event) => setDays(Number(event.target.value))}
        >
          <NativeSelectOption value={1}>最近 1 天</NativeSelectOption>
          <NativeSelectOption value={7}>最近 7 天</NativeSelectOption>
          <NativeSelectOption value={30}>最近 30 天</NativeSelectOption>
        </NativeSelect>
        {query.isPending && <p role='status'>正在加载历史数据</p>}
        {query.isError && <p role='alert'>历史数据读取失败</p>}
        {query.data && (
          <div className='space-y-4'>
            {[...query.data.periods].reverse().map((period) => (
              <div key={period.period_start} className='border-b pb-4'>
                <ChannelPassivePeriodMetrics period={period} />
              </div>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
