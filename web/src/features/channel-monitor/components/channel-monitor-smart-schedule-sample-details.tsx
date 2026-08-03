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
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { formatTimestampToDate } from '@/lib/format'

import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteStability,
} from '../types'

type ChannelMonitorSmartScheduleSampleDetailsProps = {
  route: ChannelMonitorSmartScheduleRoute
  performance: ChannelMonitorSmartScheduleRoutePerformance | undefined
  stability: ChannelMonitorSmartScheduleRouteStability | undefined
}

function SampleMetric(props: { label: string; value: string }) {
  return (
    <div className='bg-background min-w-0 rounded-md border px-3 py-2'>
      <div className='text-muted-foreground text-xs leading-4'>
        {props.label}
      </div>
      <div
        className='mt-1 font-mono text-sm leading-5 font-medium break-words tabular-nums'
        title={props.value}
      >
        {props.value}
      </div>
    </div>
  )
}

export function ChannelMonitorSmartScheduleSampleDetails(
  props: ChannelMonitorSmartScheduleSampleDetailsProps
) {
  const samples = props.route.shared_samples
  const businessGroupCount = Math.max(
    props.performance?.group_count ?? 0,
    props.stability?.group_count ?? 0
  )
  const stabilitySampleCount = props.stability
    ? `${props.stability.sample_count} 次`
    : '-'
  const stabilitySuccessAndFailure = props.stability
    ? `${props.stability.success_count} / ${props.stability.failure_count}`
    : '-'
  const stabilityFinalFailureCount = props.stability
    ? `${props.stability.final_failure_count} 次`
    : '-'
  const stabilityRetryFailureCount = props.stability
    ? `${props.stability.retry_failure_count} 次`
    : '-'
  const stabilitySuccessRate =
    props.stability && props.stability.sample_count > 0
      ? `${(props.stability.success_rate * 100).toFixed(2)}%`
      : '-'
  const stabilityScore =
    props.stability?.stability_score == null
      ? '-'
      : `${(props.stability.stability_score * 100).toFixed(1)} 分`

  const performanceSampleCount = props.performance
    ? `${props.performance.sample_count} 次`
    : '-'
  const performanceGroupCount = props.performance
    ? `${props.performance.group_count} 个`
    : '-'
  const performanceFirstTokenSampleCount = props.performance
    ? `${props.performance.first_token_duration_sample_count} 次`
    : '-'
  const performanceFirstTokenPercentiles = props.performance
    ? `${props.performance.first_token_p50_ms?.toFixed(0) ?? '-'} / ${props.performance.first_token_p95_ms?.toFixed(0) ?? '-'} ms`
    : '-'
  const performanceTPSSampleCount = props.performance
    ? `${props.performance.tps_sample_count} 次`
    : '-'
  const performanceAverageTPS =
    props.performance?.average_tps == null
      ? '-'
      : props.performance.average_tps.toFixed(2)

  const probeFailureCount = Math.max(
    samples.sample_count - samples.success_count,
    0
  )
  const probeFirstTokenAverage =
    samples.average_first_token_ms == null
      ? '-'
      : `${samples.average_first_token_ms.toFixed(0)} ms`
  const probeAverageTPS =
    samples.average_tps == null ? '-' : samples.average_tps.toFixed(2)

  return (
    <section className='border-t px-4 py-4' aria-label='渠道与模型共享窗口数据'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <div className='min-w-0'>
          <h3 className='text-sm font-medium'>渠道 + 模型共享数据</h3>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            同一渠道和模型在所有关联分组产生的数据统一汇总；稳定性和性能按当前配置窗口统计。
          </p>
        </div>
        {businessGroupCount > 0 ? (
          <Badge variant='secondary'>
            覆盖 {businessGroupCount} 个业务分组
          </Badge>
        ) : (
          <Badge variant='outline'>暂无业务窗口样本</Badge>
        )}
      </div>

      <div className='mt-4'>
        <div>
          <h4 className='text-sm font-medium'>稳定性窗口</h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            有效业务请求与窗口内测试、探测合并后的稳定性结果
          </p>
          <div className='mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3'>
            <SampleMetric label='有效样本' value={stabilitySampleCount} />
            <SampleMetric
              label='成功 / 失败'
              value={stabilitySuccessAndFailure}
            />
            <SampleMetric label='成功率' value={stabilitySuccessRate} />
            <SampleMetric label='稳定性得分' value={stabilityScore} />
            <SampleMetric label='最终失败' value={stabilityFinalFailureCount} />
            <SampleMetric label='重试失败' value={stabilityRetryFailureCount} />
          </div>
        </div>

        <Separator className='my-4' />

        <div>
          <h4 className='text-sm font-medium'>性能窗口</h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            所有关联分组产生的真实业务请求性能数据
          </p>
          <div className='mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3'>
            <SampleMetric label='业务请求样本' value={performanceSampleCount} />
            <SampleMetric label='覆盖业务分组' value={performanceGroupCount} />
            <SampleMetric
              label='首字有效样本'
              value={performanceFirstTokenSampleCount}
            />
            <SampleMetric
              label='首字 P50 / P95'
              value={performanceFirstTokenPercentiles}
            />
            <SampleMetric
              label='TPS 有效样本'
              value={performanceTPSSampleCount}
            />
            <SampleMetric label='平均 TPS' value={performanceAverageTPS} />
          </div>
          {props.performance?.last_used_time ? (
            <p className='text-muted-foreground mt-2 text-xs'>
              最近业务请求{' '}
              {formatTimestampToDate(props.performance.last_used_time)}
            </p>
          ) : null}
        </div>

        <Separator className='my-4' />

        <div>
          <h4 className='text-sm font-medium'>测试 / 探测</h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            手动测试和定时探测独立保留；调度时仅取对应窗口内的样本与业务数据合并
          </p>
          <div className='mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3'>
            <SampleMetric
              label='测试 / 探测样本'
              value={`${samples.sample_count} 次`}
            />
            <SampleMetric
              label='成功 / 失败'
              value={`${samples.success_count} / ${probeFailureCount}`}
            />
            <SampleMetric
              label='首字有效样本'
              value={`${samples.first_token_sample_count} 次`}
            />
            <SampleMetric label='首字均值' value={probeFirstTokenAverage} />
            <SampleMetric
              label='TPS 有效样本'
              value={`${samples.tps_sample_count} 次`}
            />
            <SampleMetric label='平均 TPS' value={probeAverageTPS} />
          </div>
          {samples.last_time > 0 ? (
            <p className='text-muted-foreground mt-2 text-xs'>
              最近测试或探测 {formatTimestampToDate(samples.last_time)}
            </p>
          ) : null}
          {samples.last_error ? (
            <p className='text-destructive mt-2 text-xs break-words'>
              {samples.last_error}
            </p>
          ) : null}
        </div>
      </div>
    </section>
  )
}
