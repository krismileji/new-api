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
import { Button } from '@/components/ui/button'

import type {
  ChannelMonitorCustomAction,
  ChannelMonitorCustomActionState,
} from '../types'

type ChannelMonitorCustomActionQuotaProps = {
  savedAction?: ChannelMonitorCustomAction
  state?: ChannelMonitorCustomActionState
  stateError?: string
  dailyLimit: number
  disabled: boolean
  onReset: (
    actionId: string,
    state: ChannelMonitorCustomActionState
  ) => Promise<void>
}

export function ChannelMonitorCustomActionQuota(
  props: ChannelMonitorCustomActionQuotaProps
) {
  const [confirmation, setConfirmation] =
    useState<ChannelMonitorCustomActionState>()
  const [resetError, setResetError] = useState('')
  let today = ''
  if (props.savedAction) {
    try {
      today = new Intl.DateTimeFormat('sv-SE', {
        timeZone: props.savedAction.timezone,
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
      }).format(new Date())
    } catch {
      // Never interpret a saved timezone using the browser's local date.
    }
  }
  const unavailable = !!props.stateError || !today
  const used = props.state?.day === today ? props.state.attempts : 0
  const limit = props.savedAction?.daily_limit ?? 0
  const running = props.state?.status === 'running'
  const canReset = !!props.savedAction && !unavailable && used > 0 && !running

  return (
    <div className='grid gap-2 text-sm'>
      {props.savedAction && unavailable && (
        <p className='text-muted-foreground'>
          今日调用次数暂不可用，请刷新后重试。
        </p>
      )}
      {props.savedAction && !unavailable && (
        <p className='font-medium tabular-nums' aria-live='polite'>
          今日已调用 {used} / {limit} 次，剩余 {Math.max(0, limit - used)} 次
        </p>
      )}
      {props.savedAction ? (
        <>
          <p className='text-muted-foreground text-xs'>
            按已保存规则统计（{props.savedAction.timezone}）；修改配置不会清零。
          </p>
          {Number.isInteger(props.dailyLimit) &&
            props.dailyLimit >= 1 &&
            props.dailyLimit <= 100 &&
            props.dailyLimit !== limit && (
              <p className='text-muted-foreground text-xs'>
                保存后上限为 {props.dailyLimit} 次，已调用次数保留。
              </p>
            )}
        </>
      ) : (
        <p className='text-muted-foreground'>保存规则后开始统计调用次数。</p>
      )}
      <Button
        type='button'
        variant='outline'
        size='sm'
        className='justify-self-start'
        disabled={props.disabled || !canReset}
        onClick={() => {
          setResetError('')
          setConfirmation(props.state)
        }}
      >
        重置今日次数
      </Button>
      {running && (
        <p className='text-muted-foreground text-xs'>
          接口正在执行或结果尚未确认，暂不能重置次数。
        </p>
      )}
      <AlertDialog
        open={!!confirmation}
        onOpenChange={(open) => {
          if (!open && !props.disabled) setConfirmation(undefined)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重置今日调用次数</AlertDialogTitle>
            <AlertDialogDescription>
              将“{props.savedAction?.name}”在 {confirmation?.day} 的{' '}
              {confirmation?.attempts}{' '}
              次调用清零。冷却时间、已触发状态和执行记录均保留；不会立即调用上游接口，也不会保存正在编辑的配置。
            </AlertDialogDescription>
          </AlertDialogHeader>
          {resetError && (
            <p role='alert' className='text-destructive text-sm'>
              {resetError}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={props.disabled}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              type='button'
              disabled={props.disabled || !canReset}
              onClick={async () => {
                if (!props.savedAction || !confirmation) return
                setResetError('')
                try {
                  await props.onReset(props.savedAction.id, confirmation)
                  setConfirmation(undefined)
                } catch (error) {
                  setResetError(
                    error instanceof Error ? error.message : '重置失败，请重试'
                  )
                }
              }}
            >
              确认重置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
