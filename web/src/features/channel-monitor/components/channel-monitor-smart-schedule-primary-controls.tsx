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
import { Cancel01Icon, PinIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
} from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'

import { channelMonitorSmartScheduleRouteParticipates } from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartSchedulePrimaryControlsProps = {
  route: ChannelMonitorSmartScheduleRoute
  disabled: boolean
  onEdit: (route: ChannelMonitorSmartScheduleRoute) => void
  onClear: (route: ChannelMonitorSmartScheduleRoute) => void
}

type ChannelMonitorSmartSchedulePrimaryStabilityFieldProps = {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}

export function ChannelMonitorSmartSchedulePrimaryStabilityField(
  props: ChannelMonitorSmartSchedulePrimaryStabilityFieldProps
) {
  return (
    <Field orientation='horizontal' className='rounded-md border p-3'>
      <FieldContent>
        <FieldLabel htmlFor='channel-monitor-manual-primary-stability-degrade'>
          允许稳定性降级
        </FieldLabel>
        <FieldDescription>
          开启后，连续失败或保护失败窗口达到阈值时会临时进入降级保护，固定到期时间仍保留，恢复后继续作为固定主渠道。关闭时固定优先，仍保留稳定性样本和评分。
        </FieldDescription>
      </FieldContent>
      <Switch
        id='channel-monitor-manual-primary-stability-degrade'
        checked={props.checked}
        onCheckedChange={props.onCheckedChange}
        aria-label='固定期间允许稳定性降级'
      />
    </Field>
  )
}

export function ChannelMonitorSmartSchedulePrimaryControls(
  props: ChannelMonitorSmartSchedulePrimaryControlsProps
) {
  if (props.route.state.manual_primary_until <= 0) {
    return (
      <Button
        type='button'
        variant='ghost'
        size='icon-xs'
        onClick={() => props.onEdit(props.route)}
        disabled={
          props.disabled ||
          !channelMonitorSmartScheduleRouteParticipates(props.route)
        }
        aria-label={`固定 ${props.route.channel_name} 为主渠道`}
        title='固定为主渠道'
      >
        <HugeiconsIcon icon={PinIcon} aria-hidden='true' />
      </Button>
    )
  }

  const fixedUntil = formatTimestampToDate(
    props.route.state.manual_primary_until
  )
  return (
    <div className='flex flex-wrap items-center justify-end gap-1.5'>
      <Badge variant='secondary' title={`固定至 ${fixedUntil}`}>
        <HugeiconsIcon icon={PinIcon} data-icon='inline-start' />
        管理员固定至 {fixedUntil}
      </Badge>
      <Badge variant='outline'>
        {props.route.state.manual_primary_allow_stability_degrade
          ? '允许稳定性降级'
          : '固定期间不降级'}
      </Badge>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => props.onEdit(props.route)}
        disabled={props.disabled}
        aria-label={`重新设置 ${props.route.channel_name} 的固定时长`}
      >
        <HugeiconsIcon icon={PinIcon} data-icon='inline-start' />
        重新设置
      </Button>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => props.onClear(props.route)}
        disabled={props.disabled}
        aria-label={`解除 ${props.route.channel_name} 的主渠道固定`}
      >
        <HugeiconsIcon icon={Cancel01Icon} data-icon='inline-start' />
        解除固定
      </Button>
    </div>
  )
}
