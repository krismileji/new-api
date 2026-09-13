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

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'

import { formatMonitorRuntimeCount } from '../lib/runtime-status'
import type { ChannelMonitorDiagnosticsInput } from '../types-diagnostics'

const diagnosticTime = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

export function ChannelMonitorRuntimeDiagnostics(props: {
  diagnostics?: ChannelMonitorDiagnosticsInput
}) {
  const data = props.diagnostics?.data
  const [resetDayStart, setResetDayStart] = useState<number | null>(null)
  const [resetting, setResetting] = useState(false)
  const [resetError, setResetError] = useState('')
  const unavailable =
    !data || props.diagnostics?.loading || props.diagnostics?.failed
  const fields: [label: string, value: number | undefined, unit: string][] = [
    ['事件处理重试', data?.retry_count, '次'],
    ['自动接管', data?.takeover_count, '次'],
    ['异常隔离', data?.quarantine_count, '条'],
    ['标记清理失败', data?.marker_release_failure_count, '次'],
    ['事件清理失败', data?.stream_trim_failure_count, '次'],
  ]
  let notice = ''
  if (props.diagnostics?.failed) {
    notice = data
      ? '今日诊断获取失败，以下显示上次记录，请刷新后重试。'
      : '今日诊断获取失败，请刷新后重试。'
  } else if (props.diagnostics?.loading) {
    notice = '正在读取今日诊断。'
  } else if (!data) {
    notice = '尚未提供今日诊断，历史累计不能作为今日计数。'
  }

  return (
    <section className='col-span-full min-w-0 space-y-3' aria-label='今日诊断'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-medium'>今日诊断</h3>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={unavailable || resetting || !props.diagnostics?.onReset}
          onClick={() => {
            if (!data) return
            setResetError('')
            setResetDayStart(data.day_start)
          }}
        >
          重置今日计数
        </Button>
      </div>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        按北京时间每日 00:00
        重新计数，仅保留当前日的一份统计，不会逐日累积记录。
      </p>
      {notice ? (
        <p
          className='text-warning text-xs'
          role={props.diagnostics?.failed ? 'alert' : 'status'}
        >
          {notice}
        </p>
      ) : null}
      {data ? (
        <p className='text-muted-foreground text-xs tabular-nums'>
          统计自 {diagnosticTime.format(data.counted_since * 1000)}，查询于{' '}
          {diagnosticTime.format(data.observed_at * 1000)}
        </p>
      ) : null}
      {data && data.last_reset_at > 0 ? (
        <p className='text-muted-foreground text-xs'>
          显示本次重置后新增的计数。
        </p>
      ) : null}
      {data &&
      data.last_reset_at === 0 &&
      data.counted_since > data.day_start ? (
        <p className='text-muted-foreground text-xs'>
          统计从上述起点开始，不包含此前的历史累计。
        </p>
      ) : null}
      <dl className='grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5'>
        {fields.map(([label, value, unit]) => (
          <div key={label} className='min-w-0' role='group' aria-label={label}>
            <dt className='text-muted-foreground text-xs'>{label}</dt>
            <dd className='mt-1 text-sm font-medium break-words tabular-nums'>
              {formatMonitorRuntimeCount(value, unit)}
            </dd>
          </div>
        ))}
      </dl>
      {resetError ? (
        <p className='text-destructive text-xs' role='alert'>
          {resetError}
        </p>
      ) : null}
      <ConfirmDialog
        open={resetDayStart !== null}
        onOpenChange={(open) => {
          if (!open && !resetting) setResetDayStart(null)
        }}
        title='重置今日诊断计数'
        desc='将今日这五项诊断计数归零，从当前时间继续统计，对所有管理端生效。不会删除事件记录或改变故障状态。历史累计保持保留。'
        confirmText='确认重置'
        cancelBtnText='取消'
        isLoading={resetting}
        disabled={unavailable}
        handleConfirm={async () => {
          if (
            resetDayStart === null ||
            resetting ||
            unavailable ||
            !props.diagnostics?.onReset
          ) {
            return
          }
          setResetting(true)
          setResetError('')
          try {
            await props.diagnostics.onReset(resetDayStart)
          } catch (error) {
            setResetError(
              error instanceof Error
                ? error.message
                : '重置今日诊断失败，请稍后重试。'
            )
          } finally {
            setResetting(false)
            setResetDayStart(null)
          }
        }}
      />
    </section>
  )
}
