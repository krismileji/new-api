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
import { ArrowDown01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'

import {
  formatMonitorRuntimeCount,
  formatMonitorRuntimeTime,
  type ChannelMonitorRuntimeInput,
} from '../lib/runtime-status'

const recoveryGapLabels: Record<string, string> = {
  samples_dropped: '监控事件曾被丢弃',
  events_quarantined: '存在历史隔离记录',
  writer_queue_full: '监控写入队列曾满',
  cost_publish_failure: '成本事件发布失败',
  cost_dead_letter: '存在待复核的成本异常事件',
  daily_replay_incomplete: '日统计恢复不完整',
}

export function ChannelMonitorRuntimeHistory(
  props: Pick<ChannelMonitorRuntimeInput, 'metadata' | 'recovery'>
) {
  const metadata = props.metadata
  const recovery = props.recovery
  const fields: [label: string, value: string, description?: string][] = [
    [
      '历史记录',
      recovery
        ? recovery.data_gap_reasons
            .map((reason) => recoveryGapLabels[reason] ?? reason)
            .join('、') || '无'
        : '未提供',
    ],
    ['最近隔离', formatMonitorRuntimeTime(metadata?.last_quarantined_at)],
    [
      '事件处理重试（累计）',
      formatMonitorRuntimeCount(metadata?.retry_count, '次'),
      '包含后台事件重试和消费者自动恢复；同一事件可多次重试。',
    ],
    [
      '自动接管（累计）',
      formatMonitorRuntimeCount(metadata?.takeover_count, '次'),
      '后台重新领取闲置待处理事件的次数，同一事件可重复接管。',
    ],
    [
      '异常隔离（累计）',
      formatMonitorRuntimeCount(metadata?.quarantine_count, '条'),
      '曾进入隔离队列的事件总数，不等于请求失败数或统计丢失数。',
    ],
    [
      '标记清理失败（累计）',
      formatMonitorRuntimeCount(metadata?.marker_release_failure_count, '次'),
    ],
    [
      '事件清理失败（累计）',
      formatMonitorRuntimeCount(metadata?.stream_trim_failure_count, '次'),
    ],
  ]
  if (
    recovery?.status === 'healthy' &&
    recovery.data_gap_reasons.length > 0 &&
    recovery.action
  ) {
    fields.push(['历史处理建议', recovery.action])
  }

  return (
    <Collapsible className='col-span-full min-w-0'>
      <CollapsibleTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='group w-full justify-between'
          />
        }
      >
        历史诊断（累计）
        <HugeiconsIcon
          icon={ArrowDown01Icon}
          data-icon='inline-end'
          className='transition-transform group-aria-expanded:rotate-180'
          aria-hidden='true'
        />
      </CollapsibleTrigger>
      <CollapsibleContent
        className='pt-4'
        role='region'
        aria-label='历史诊断（累计）'
      >
        <p className='text-muted-foreground mb-4 text-xs leading-relaxed'>
          累计值不会随恢复或应用重启自动清零；没有新增重试、接管或故障时保持不变。
          今日重置也不会改变这些历史累计。累计数字不等于明细数量，隔离明细另有约一万条的保留上限。
          判断是否仍有问题，应关注当前状态、处理延迟，以及计数和最近隔离时间是否继续变化。
        </p>
        <dl className='grid min-w-0 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3'>
          {fields.map(([label, value, description]) => (
            <div
              key={label}
              className='grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] content-start gap-x-3 gap-y-1 text-xs'
              role='group'
              aria-label={label}
            >
              <dt className='text-muted-foreground min-w-0 break-words'>
                {label}
              </dt>
              <dd className='min-w-0 text-right break-words tabular-nums'>
                {value}
              </dd>
              {description ? (
                <dd className='text-muted-foreground col-span-full leading-relaxed'>
                  {description}
                </dd>
              ) : null}
            </div>
          ))}
        </dl>
      </CollapsibleContent>
    </Collapsible>
  )
}
