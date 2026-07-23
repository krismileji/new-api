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
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorItem } from '../types'

type ChannelMonitorSmartScheduleCellProps = {
  channel: ChannelMonitorItem
  pending: boolean
  onUpdate: (excluded: boolean) => void
}

export function ChannelMonitorSmartScheduleCell(
  props: ChannelMonitorSmartScheduleCellProps
) {
  const [resetConfirmationOpen, setResetConfirmationOpen] = useState(false)
  const participating = !props.channel.smart_schedule_excluded

  return (
    <div className='flex w-full min-w-0 flex-col items-start gap-2'>
      <div className='flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums'>
        <span>
          优先级 <strong>{props.channel.priority}</strong>
        </span>
        <span>
          权重 <strong>{props.channel.weight}</strong>
        </span>
        {props.pending && <Spinner className='size-3.5' />}
      </div>

      <div className='flex flex-wrap items-center gap-2'>
        <div className='flex items-center gap-2'>
          <Switch
            checked={participating}
            disabled={props.pending}
            onCheckedChange={(checked) => {
              if (checked) {
                setResetConfirmationOpen(true)
              } else {
                props.onUpdate(true)
              }
            }}
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
        {participating &&
        props.channel.smart_schedule_stability_state === 'degraded' ? (
          <span className='text-destructive text-xs'>
            低成功率降级
            {props.channel.smart_schedule_stability_until
              ? `至 ${formatTimestampToDate(props.channel.smart_schedule_stability_until)}`
              : ''}
          </span>
        ) : null}
        {participating &&
        props.channel.smart_schedule_stability_state === 'probing' ? (
          <span className='text-xs text-amber-600 dark:text-amber-400'>
            稳定性试放
          </span>
        ) : null}
      </div>

      <AlertDialog
        open={resetConfirmationOpen}
        onOpenChange={setResetConfirmationOpen}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认参与调度？</AlertDialogTitle>
            <AlertDialogDescription>
              启用“{props.channel.name}”参与智能调度将把优先级重置为
              80、权重重置为 10。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                props.onUpdate(false)
                setResetConfirmationOpen(false)
              }}
            >
              确认
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
