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
  formatMonitorRuntimeCount,
  formatMonitorRuntimeTime,
  type getChannelMonitorRuntimeStatus,
  type ChannelMonitorRuntimeInput,
} from '../lib/runtime-status'
import { ChannelMonitorRuntimeDiagnostics } from './channel-monitor-runtime-diagnostics'
import { ChannelMonitorRuntimeHistory } from './channel-monitor-runtime-history'

type RuntimeDiagnosticGroup = {
  title: string
  fields: [label: string, value: string][]
}

export function ChannelMonitorRuntimeDetails(
  props: ChannelMonitorRuntimeInput & {
    status: ReturnType<typeof getChannelMonitorRuntimeStatus>
  }
) {
  const metadata = props.metadata
  const recovery = props.recovery
  let redisLabel = '未提供'
  if (props.status.redisAvailable !== undefined) {
    redisLabel = props.status.redisAvailable ? '正常' : '故障'
  }
  let consumerLabel = '未提供'
  if (props.status.consumerRunning !== undefined) {
    consumerLabel = props.status.consumerRunning ? '运行中' : '已停止'
  }
  let markerLabel = '未提供'
  if (metadata?.marker_release_failure_active !== undefined) {
    markerLabel = metadata.marker_release_failure_active ? '故障' : '正常'
  }
  let trimLabel = '未提供'
  if (metadata?.stream_trim_failure_active !== undefined) {
    trimLabel = metadata.stream_trim_failure_active ? '故障' : '正常'
  }
  let actionLabel = recovery ? recovery.action || '无' : '未提供'
  if (props.status.healthy) {
    actionLabel = '后台自动处理新事件，少量待处理不代表故障。'
  }

  const groups: RuntimeDiagnosticGroup[] = [
    {
      title: '实时事件',
      fields: [
        ['Redis', redisLabel],
        ['事件处理', consumerLabel],
        ['数据截至', formatMonitorRuntimeTime(metadata?.data_cutoff_at)],
        [
          '处理延迟',
          formatMonitorRuntimeCount(metadata?.consumer_lag_seconds, '秒'),
        ],
        [
          '事件待处理',
          formatMonitorRuntimeCount(props.status.pendingCount, '条'),
        ],
        [
          '监控写入队列',
          `${formatMonitorRuntimeCount(metadata?.writer_queue_depth)} / ${formatMonitorRuntimeCount(metadata?.writer_queue_capacity)}`,
        ],
        [
          '写入等待',
          formatMonitorRuntimeCount(metadata?.writer_queue_age_seconds, '秒'),
        ],
        [
          '已处理事件序号',
          formatMonitorRuntimeCount(metadata?.event_watermark),
        ],
        ['数据处理时间', formatMonitorRuntimeTime(metadata?.processed_at)],
        ['最近发布', formatMonitorRuntimeTime(metadata?.last_published_at)],
        ['最近处理', formatMonitorRuntimeTime(metadata?.last_processed_at)],
        ['最早待处理', formatMonitorRuntimeTime(metadata?.oldest_pending_at)],
        [
          '监控写入重试',
          formatMonitorRuntimeCount(metadata?.writer_retry_events, '次'),
        ],
        [
          '监控事件丢弃',
          formatMonitorRuntimeCount(metadata?.writer_dropped_events, '条'),
        ],
      ],
    },
    {
      title: '成本处理',
      fields: [
        ['汇总状态', props.status.costLabel],
        [
          '汇总检查时间',
          formatMonitorRuntimeTime(metadata?.cost_projection?.checked_at),
        ],
        [
          '已聚合待写入',
          formatMonitorRuntimeCount(metadata?.cost_queue_pending_count, '条'),
        ],
        [
          '事件未读取',
          formatMonitorRuntimeCount(metadata?.cost_stream_unread_count, '条'),
        ],
        [
          '事件待确认',
          formatMonitorRuntimeCount(metadata?.cost_stream_pending_count, '条'),
        ],
        [
          '待记入成本账本',
          formatMonitorRuntimeCount(metadata?.cost_outbox_pending_count, '条'),
        ],
        [
          '最早待记账成本',
          formatMonitorRuntimeTime(metadata?.cost_outbox_oldest_pending_at),
        ],
        [
          '账本写入重试',
          formatMonitorRuntimeCount(metadata?.cost_outbox_retry_count, '次'),
        ],
        [
          '账本写入失败（累计）',
          formatMonitorRuntimeCount(metadata?.cost_ledger_failed_count, '次'),
        ],
        [
          '事件排队失败（累计）',
          formatMonitorRuntimeCount(metadata?.cost_publish_failed_count, '次'),
        ],
        [
          '成本异常事件',
          formatMonitorRuntimeCount(metadata?.cost_dead_letter_count, '条'),
        ],
      ],
    },
    {
      title: '后台恢复',
      fields: [
        ['恢复状态', props.status.label],
        [
          '当前异常',
          props.status.alerts.join('；') ||
            (props.status.healthy ? '无' : '未确认'),
        ],
        [
          '后台待处理',
          formatMonitorRuntimeCount(recovery?.pending_count, '条'),
        ],
        ['最近恢复', formatMonitorRuntimeTime(recovery?.recovered_at)],
        ['处理建议', actionLabel],
        ['事件标记清理', markerLabel],
        ['实时事件清理', trimLabel],
        ['通知错误', recovery ? recovery.notification_error || '无' : '未提供'],
      ],
    },
  ]

  return (
    <div
      className='grid min-w-0 grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3'
      data-slot='runtime-diagnostic-groups'
    >
      <ChannelMonitorRuntimeDiagnostics diagnostics={props.diagnostics} />
      {groups.map((group) => (
        <section key={group.title} className='min-w-0' aria-label={group.title}>
          <h3 className='mb-3 text-sm font-medium'>{group.title}</h3>
          {group.title === '后台恢复' && props.recoveryFailed ? (
            <p className='text-warning mb-3 text-xs'>
              恢复状态获取失败，以下恢复信息为上次记录。
            </p>
          ) : null}
          <dl className='flex min-w-0 flex-col gap-2'>
            {group.fields.map(([label, value]) => (
              <div
                key={label}
                className='grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] items-start gap-x-3 text-xs'
                role='group'
                aria-label={label}
              >
                <dt className='text-muted-foreground min-w-0 break-words'>
                  {label}
                </dt>
                <dd className='min-w-0 text-right break-words tabular-nums'>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
      <ChannelMonitorRuntimeHistory metadata={metadata} recovery={recovery} />
    </div>
  )
}
