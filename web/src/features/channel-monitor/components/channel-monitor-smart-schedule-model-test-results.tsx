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
import { Refresh01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type {
  ChannelMonitorSmartScheduleModelTestItem,
  ChannelMonitorSmartScheduleModelTestResult,
  ChannelMonitorSmartScheduleModelTestStatus,
} from '../types'

const STATUS_LABELS: Record<
  ChannelMonitorSmartScheduleModelTestStatus,
  string
> = {
  success: '正常',
  failure: '失败',
  skipped: '已跳过',
}

function statusVariant(status: ChannelMonitorSmartScheduleModelTestStatus) {
  if (status === 'failure') return 'destructive' as const
  if (status === 'success') return 'secondary' as const
  return 'outline' as const
}

function formatMilliseconds(value: number | undefined) {
  if (value == null || !Number.isFinite(value) || value < 0) return '-'
  if (value < 1000) return `${Math.round(value)} ms`
  return `${(value / 1000).toFixed(2)} s`
}

function ModelTestResultRow(props: {
  item: ChannelMonitorSmartScheduleModelTestItem
  showStreamMetrics: boolean
  pending: boolean
  testingDisabled: boolean
  onRetry: () => void
}) {
  return (
    <TableRow>
      <TableCell className='min-w-44'>
        <div
          className='max-w-56 truncate font-medium'
          title={props.item.channel_name}
        >
          {props.item.channel_name || `渠道 ${props.item.channel_id}`}
        </div>
        <div className='text-muted-foreground mt-0.5 text-xs'>
          ID {props.item.channel_id}
        </div>
      </TableCell>
      <TableCell>
        <div className='flex flex-wrap gap-1'>
          <Badge variant={props.item.participates ? 'secondary' : 'outline'}>
            {props.item.participates ? '参与调度' : '未参与'}
          </Badge>
          <Badge variant={props.item.available ? 'secondary' : 'outline'}>
            {props.item.available ? '渠道可用' : '渠道不可用'}
          </Badge>
        </div>
      </TableCell>
      <TableCell>
        <Badge variant={statusVariant(props.item.status)}>
          {STATUS_LABELS[props.item.status]}
        </Badge>
      </TableCell>
      <TableCell className='font-mono tabular-nums'>
        {formatMilliseconds(props.item.total_ms)}
      </TableCell>
      {props.showStreamMetrics ? (
        <>
          <TableCell className='font-mono tabular-nums'>
            {formatMilliseconds(props.item.first_token_ms)}
          </TableCell>
          <TableCell className='font-mono tabular-nums'>
            {props.item.tps == null ? '-' : props.item.tps.toFixed(2)}
          </TableCell>
        </>
      ) : null}
      <TableCell className='max-w-72 whitespace-normal'>
        {props.item.error ? (
          <div className='text-destructive break-words'>
            {props.item.error}
            {props.item.error_code ? (
              <span className='mt-0.5 block font-mono text-xs'>
                {props.item.error_code}
              </span>
            ) : null}
          </div>
        ) : (
          <span className='text-muted-foreground'>-</span>
        )}
      </TableCell>
      <TableCell className='text-right'>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.testingDisabled}
          onClick={props.onRetry}
          aria-label={`重新测试 ${props.item.channel_name || `渠道 ${props.item.channel_id}`}`}
          title='重新测试'
        >
          {props.pending ? <Spinner /> : <HugeiconsIcon icon={Refresh01Icon} />}
        </Button>
      </TableCell>
    </TableRow>
  )
}

type ChannelMonitorSmartScheduleModelTestResultsProps = {
  result: ChannelMonitorSmartScheduleModelTestResult
  pendingChannelId: number | null
  testing: boolean
  onRetry: (channelId: number) => void
}

export function ChannelMonitorSmartScheduleModelTestResults(
  props: ChannelMonitorSmartScheduleModelTestResultsProps
) {
  return (
    <>
      <div className='bg-muted/20 flex flex-wrap items-center gap-2 border-b px-3 py-2 text-xs'>
        <Badge variant='outline'>共 {props.result.total} 条</Badge>
        <Badge variant='secondary'>正常 {props.result.succeeded}</Badge>
        <Badge variant={props.result.failed > 0 ? 'destructive' : 'outline'}>
          失败 {props.result.failed}
        </Badge>
        <Badge variant='outline'>跳过 {props.result.skipped}</Badge>
        <span className='text-muted-foreground ml-auto'>
          {props.result.stream ? '流式' : '非流式'} ·{' '}
          {props.result.endpoint_type}
        </span>
      </div>
      <Table className='min-w-[56rem]'>
        <TableHeader>
          <TableRow>
            <TableHead>渠道</TableHead>
            <TableHead>调度与可用性</TableHead>
            <TableHead>结果</TableHead>
            <TableHead>总耗时</TableHead>
            {props.result.stream ? (
              <>
                <TableHead>首字</TableHead>
                <TableHead>TPS</TableHead>
              </>
            ) : null}
            <TableHead>错误</TableHead>
            <TableHead className='text-right'>操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.result.results.map((item) => (
            <ModelTestResultRow
              key={item.channel_id}
              item={item}
              showStreamMetrics={props.result.stream}
              pending={
                props.testing && props.pendingChannelId === item.channel_id
              }
              testingDisabled={props.testing}
              onRetry={() => props.onRetry(item.channel_id)}
            />
          ))}
        </TableBody>
      </Table>
    </>
  )
}
