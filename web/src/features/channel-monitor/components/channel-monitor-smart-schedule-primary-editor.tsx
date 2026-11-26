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
import { useMutation } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import {
  ChannelMonitorSmartScheduleChannelPrimaryRuleStabilityConfirmationRequiredError,
  ChannelMonitorSmartScheduleStabilityConfirmationRequiredError,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { formatChannelMonitorSmartScheduleUntil } from '../lib/smart-schedule-display'
import {
  updatePrimaryEntry,
  type PrimaryEdit,
  type PrimaryEntry,
} from '../lib/smart-schedule-primary-management'
import { ChannelMonitorSmartSchedulePrimaryStabilityField } from './channel-monitor-smart-schedule-primary-controls'

const DURATIONS = [
  ['keep', '保持不变'],
  ['60', '1 小时'],
  ['360', '6 小时'],
  ['1440', '24 小时'],
  ['permanent', '永久'],
  ['custom', '自定义'],
] as const

type Props = {
  entry: PrimaryEntry
  onValidityChange: (valid: boolean) => void
  onSaved: () => Promise<void>
  onPendingChange: (pending: boolean) => void
  onDirtyChange: (dirty: boolean) => void
}

export function ChannelMonitorSmartSchedulePrimaryEditor(props: Props) {
  const [duration, setDuration] = useState('keep')
  const [customMinutes, setCustomMinutes] = useState('120')
  const [allowDegrade, setAllowDegrade] = useState(
    props.entry.allow_stability_degrade
  )
  const [confirmation, setConfirmation] = useState<PrimaryEdit | null>(null)
  const parsed = Number(duration === 'custom' ? customMinutes : duration)
  const valid =
    duration === 'keep' ||
    duration === 'permanent' ||
    (Number.isInteger(parsed) && parsed >= 1 && parsed <= 525600)
  const dirty =
    duration !== 'keep' || allowDegrade !== props.entry.allow_stability_degrade
  const onValidityChange = props.onValidityChange
  useEffect(() => onValidityChange(valid), [valid, onValidityChange])
  let durationHelp = '保存成功后，从当前时间重新计时。'
  if (!valid) durationHelp = '请输入 1 至 525600 之间的整数分钟。'
  else if (duration === 'keep') {
    durationHelp = '保持原到期时间，调整降级设置不会重新计时。'
  } else if (duration === 'permanent') durationHelp = '持续固定，直到手动解除。'
  const mutation = useMutation({
    mutationFn: updatePrimaryEntry,
    onMutate: () => props.onPendingChange(true),
    onError: (error, edit) => {
      if (
        error instanceof
          ChannelMonitorSmartScheduleChannelPrimaryRuleStabilityConfirmationRequiredError ||
        error instanceof
          ChannelMonitorSmartScheduleStabilityConfirmationRequiredError
      ) {
        setConfirmation(edit)
        return
      }
      setConfirmation(null)
      handleChannelMonitorMutationError(error)
    },
    onSuccess: async () => {
      setConfirmation(null)
      props.onDirtyChange(false)
      toast.success('固定项已更新')
      await props.onSaved()
    },
    onSettled: () => props.onPendingChange(false),
  })

  return (
    <>
      <form
        id='primary-editor'
        className='space-y-5'
        onSubmit={(event) => {
          event.preventDefault()
          if (!valid || !dirty || mutation.isPending) return
          mutation.mutate({
            entry: props.entry,
            durationMinutes:
              duration === 'keep' || duration === 'permanent' ? null : parsed,
            preserveUntil: duration === 'keep',
            allowStabilityDegrade: allowDegrade,
          })
        }}
      >
        <div className='bg-muted/30 space-y-2 rounded-lg border p-4 text-sm'>
          <p className='font-medium break-words'>
            {props.entry.channel_name} · #{props.entry.channel_id}
          </p>
          <p className='break-words'>
            固定范围：{props.entry.group || '全部分组'}
            {props.entry.kind === 'route'
              ? ` / ${props.entry.model}`
              : ' · 当前及后续符合条件的模型'}
          </p>
          <p className='text-muted-foreground'>
            当前固定至：
            {formatChannelMonitorSmartScheduleUntil(
              props.entry.manual_primary_until
            )}
          </p>
        </div>
        <fieldset disabled={mutation.isPending} className='space-y-3'>
          <legend className='mb-2 text-sm font-medium'>有效期</legend>
          <div className='flex flex-wrap gap-2'>
            {DURATIONS.map(([value, label]) => (
              <Button
                key={value}
                autoFocus={value === 'keep'}
                type='button'
                size='sm'
                variant={duration === value ? 'secondary' : 'outline'}
                aria-pressed={duration === value}
                onClick={() => {
                  setDuration(value)
                  props.onDirtyChange(
                    value !== 'keep' ||
                      allowDegrade !== props.entry.allow_stability_degrade
                  )
                }}
              >
                {label}
              </Button>
            ))}
          </div>
          {duration === 'custom' ? (
            <div className='space-y-2'>
              <Label htmlFor='primary-custom-duration'>固定时长（分钟）</Label>
              <Input
                id='primary-custom-duration'
                type='number'
                min={1}
                max={525600}
                step={1}
                value={customMinutes}
                onChange={(event) => setCustomMinutes(event.target.value)}
                aria-invalid={!valid}
                aria-describedby='primary-duration-help'
              />
            </div>
          ) : null}
          <p
            id='primary-duration-help'
            className='text-muted-foreground text-xs'
            role={valid ? undefined : 'alert'}
          >
            {durationHelp}
          </p>
        </fieldset>
        <fieldset disabled={mutation.isPending}>
          <ChannelMonitorSmartSchedulePrimaryStabilityField
            checked={allowDegrade}
            onCheckedChange={(checked) => {
              setAllowDegrade(checked)
              props.onDirtyChange(
                duration !== 'keep' ||
                  checked !== props.entry.allow_stability_degrade
              )
            }}
          />
        </fieldset>
        {props.entry.kind === 'channel' ? (
          <p className='text-warning text-xs leading-relaxed'>
            保存会更新渠道规则的先后顺序，可能接管其他渠道的固定；单模型固定仍优先。涉及稳定性保护时，需要再次确认解除保护。
          </p>
        ) : null}
      </form>
      <ConfirmDialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setConfirmation(null)
        }}
        title='解除稳定性保护并固定？'
        desc='所选固定项涉及处于稳定性保护中的路由。继续会解除当前保护；保留“允许稳定性降级”时，后续达到失败阈值仍可再次降级。'
        confirmText='解除保护并保存'
        cancelBtnText='取消'
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (confirmation) {
            mutation.mutate({ ...confirmation, confirmStabilityOverride: true })
          }
        }}
      />
    </>
  )
}
