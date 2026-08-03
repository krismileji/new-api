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
import {
  formatChannelTestDuration,
  type ChannelTestMetricValues,
} from './channel-test-metric-values'

function formatChannelTestRate(value?: number): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return Math.max(0, value).toFixed(2)
}

function formatChannelTestToken(value: number | undefined, available: boolean) {
  if (!available || typeof value !== 'number' || !Number.isFinite(value)) {
    return '-'
  }
  return Math.max(0, Math.trunc(value)).toLocaleString('zh-CN')
}

function ChannelTestMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground truncate text-[11px] leading-4'>
        {props.label}
      </dt>
      <dd className='text-foreground truncate font-mono text-xs font-medium tabular-nums'>
        {props.value}
      </dd>
    </div>
  )
}

export function ChannelTestMetrics(props: {
  metrics: ChannelTestMetricValues
}) {
  const metrics = props.metrics
  const usageAvailable = metrics.usageAvailable === true

  return (
    <div className='min-w-72 space-y-1.5' data-slot='channel-test-metrics'>
      <dl aria-label='性能指标' className='grid grid-cols-3 gap-x-3 gap-y-1'>
        <ChannelTestMetric
          label='总耗时'
          value={formatChannelTestDuration(metrics.responseTime)}
        />
        <ChannelTestMetric
          label='首字时间'
          value={formatChannelTestDuration(metrics.firstTokenMs)}
        />
        <ChannelTestMetric
          label='TPS'
          value={formatChannelTestRate(metrics.tokensPerSecond)}
        />
      </dl>
      <dl
        aria-label='Token 指标'
        className='border-border/70 grid grid-cols-3 gap-x-3 gap-y-1 border-t pt-1.5'
      >
        <ChannelTestMetric
          label='输入 Token'
          value={formatChannelTestToken(metrics.inputTokens, usageAvailable)}
        />
        <ChannelTestMetric
          label='输出 Token'
          value={formatChannelTestToken(metrics.outputTokens, usageAvailable)}
        />
        <ChannelTestMetric
          label='总 Token'
          value={formatChannelTestToken(metrics.totalTokens, usageAvailable)}
        />
        <ChannelTestMetric
          label='缓存读取'
          value={formatChannelTestToken(metrics.cachedTokens, usageAvailable)}
        />
        <ChannelTestMetric
          label='缓存写入'
          value={formatChannelTestToken(
            metrics.cacheWriteTokens,
            usageAvailable
          )}
        />
        <ChannelTestMetric
          label='推理 Token'
          value={formatChannelTestToken(
            metrics.reasoningTokens,
            usageAvailable
          )}
        />
      </dl>
      {!usageAvailable && (
        <p className='text-muted-foreground text-[11px] leading-4'>
          上游未返回 Usage，Token 指标不可用
        </p>
      )}
      {typeof metrics.smartScheduleSampleRecorded === 'boolean' && (
        <p
          className='text-muted-foreground text-[11px] leading-4'
          title={metrics.smartScheduleSampleMessage}
        >
          {metrics.smartScheduleSampleRecorded
            ? '已计入渠道样本'
            : '未计入渠道样本'}
        </p>
      )}
    </div>
  )
}
