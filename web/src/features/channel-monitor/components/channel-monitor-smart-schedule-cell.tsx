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
import { ShieldMinusIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorItem } from '../types'

type ChannelMonitorSmartScheduleCellProps = {
  channel: ChannelMonitorItem
  pending: boolean
  clearPending: boolean
  onUpdate: (excluded: boolean) => void
  onClearStability: () => void
}

export function ChannelMonitorSmartScheduleCell(
  props: ChannelMonitorSmartScheduleCellProps
) {
  const [clearConfirmationOpen, setClearConfirmationOpen] = useState(false)
  const participating = !props.channel.smart_schedule_excluded
  const busy = props.pending || props.clearPending
  const stabilityState = props.channel.smart_schedule_stability_state
  const hasStabilityProtection =
    stabilityState === 'degraded' || stabilityState === 'probing'

  return (
    <div className='flex w-full min-w-0 flex-col items-start gap-2'>
      <div className='flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums'>
        <span>
          优先级 <strong>{props.channel.priority}</strong>
        </span>
        <span>
          权重 <strong>{props.channel.weight}</strong>
        </span>
        {busy && <Spinner className='size-3.5' />}
      </div>

      <div className='flex flex-wrap items-center gap-2'>
        <div className='flex items-center gap-2'>
          <Switch
            checked={participating}
            disabled={busy}
            onCheckedChange={(checked) => props.onUpdate(!checked)}
            aria-label={`${participating ? '停止' : '启用'} ${props.channel.name} 的智能调度`}
          />
          <span className='text-xs'>参与调度</span>
        </div>
        {participating &&
        !props.channel.smart_schedule_stability_state &&
        props.channel.last_schedule_score != null ? (
          <span className='text-xs tabular-nums'>
            得分 {(props.channel.last_schedule_score * 100).toFixed(1)}
          </span>
        ) : null}
      </div>

      {hasStabilityProtection ? (
        <div className='flex flex-wrap items-center gap-2'>
          {stabilityState === 'degraded' ? (
            <span
              data-slot='smart-schedule-stability-status'
              className='text-destructive text-xs'
            >
              低成功率降级
              {props.channel.smart_schedule_stability_until
                ? `至 ${formatTimestampToDate(props.channel.smart_schedule_stability_until)}`
                : ''}
            </span>
          ) : (
            <span
              data-slot='smart-schedule-stability-status'
              className='text-xs text-amber-600 dark:text-amber-400'
            >
              稳定性试放
            </span>
          )}
          <Button
            type='button'
            variant='outline'
            size='xs'
            disabled={busy}
            onClick={() => setClearConfirmationOpen(true)}
            aria-label={`手动解除 ${props.channel.name} 的稳定性保护`}
          >
            <HugeiconsIcon icon={ShieldMinusIcon} data-icon='inline-start' />
            手动解除
          </Button>
        </div>
      ) : null}

      <AlertDialog
        open={clearConfirmationOpen}
        onOpenChange={setClearConfirmationOpen}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认手动解除稳定性保护？</AlertDialogTitle>
            <AlertDialogDescription>
              将立即清除“{props.channel.name}”的
              {stabilityState === 'degraded' ? '低成功率降级' : '稳定性试放'}
              状态，并恢复进入保护前保存的优先级和完整权重。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={props.clearPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={props.clearPending}
              onClick={() => {
                props.onClearStability()
                setClearConfirmationOpen(false)
              }}
            >
              {props.clearPending ? '解除中...' : '确认解除'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
