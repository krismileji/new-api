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
import { PauseIcon, PlayIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'

import {
  channelMonitorSmartScheduleUntilIsPermanent,
  formatChannelMonitorSmartScheduleUntil,
} from '../lib/smart-schedule-display'
import { channelMonitorSmartScheduleRouteIsTrafficPaused } from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

const MAX_ROUTE_PAUSE_MINUTES = 525_600

type ChannelMonitorSmartScheduleGroupPauseProps = {
  route: ChannelMonitorSmartScheduleRoute
  pending: boolean
  disabled: boolean
  onUpdate: (
    route: ChannelMonitorSmartScheduleRoute,
    durationMinutes: number | null
  ) => void
}

export function ChannelMonitorSmartScheduleGroupPause(
  props: ChannelMonitorSmartScheduleGroupPauseProps
) {
  const paused = channelMonitorSmartScheduleRouteIsTrafficPaused(props.route)
  const inputId = `route-pause-duration-${props.route.channel_id}`
  const descriptionId = `${inputId}-description`

  return (
    <section className='border-t px-4 py-4' aria-label='路由流量暂停'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='min-w-0 flex-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <h3 className='text-sm font-medium'>路由流量暂停</h3>
            {paused ? <Badge variant='warning'>流量已暂停</Badge> : null}
          </div>
          <p className='text-muted-foreground mt-1 text-xs leading-5'>
            {`暂停后，该渠道在“${props.route.group}”分组使用“${props.route.model}”模型的路由不会承接流量。填写时长则到期自动恢复，留空则保持暂停，直到手动恢复。`}
          </p>
          {paused ? (
            <p className='text-warning mt-1 text-xs font-medium tabular-nums'>
              暂停至{' '}
              {formatChannelMonitorSmartScheduleUntil(
                props.route.traffic_paused_until
              )}
            </p>
          ) : null}
        </div>
        {paused ? (
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => props.onUpdate(props.route, 0)}
          >
            {props.pending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={PlayIcon} data-icon='inline-start' />
            )}
            立即恢复
          </Button>
        ) : null}
      </div>

      <form
        key={`${props.route.channel_id}\u0000${props.route.group}\u0000${props.route.model}`}
        className='mt-4 flex flex-col gap-3 sm:flex-row sm:items-end'
        aria-label={`${props.route.channel_name} ${props.route.group} ${props.route.model} 路由流量暂停设置`}
        onSubmit={(event) => {
          event.preventDefault()
          const formData = new FormData(event.currentTarget)
          const rawDuration = String(
            formData.get('duration_minutes') ?? ''
          ).trim()
          const durationMinutes =
            rawDuration === '' ? null : Number(rawDuration)
          if (
            durationMinutes !== null &&
            (!Number.isInteger(durationMinutes) ||
              durationMinutes < 1 ||
              durationMinutes > MAX_ROUTE_PAUSE_MINUTES)
          ) {
            return
          }
          props.onUpdate(props.route, durationMinutes)
        }}
      >
        <FieldGroup className='flex-1'>
          <Field>
            <FieldLabel htmlFor={inputId}>暂停时长（分钟）</FieldLabel>
            <Input
              id={inputId}
              name='duration_minutes'
              type='number'
              min={1}
              max={MAX_ROUTE_PAUSE_MINUTES}
              step={1}
              defaultValue={
                channelMonitorSmartScheduleUntilIsPermanent(
                  props.route.traffic_paused_until
                )
                  ? ''
                  : 60
              }
              placeholder='留空表示永久'
              disabled={props.disabled}
              aria-describedby={descriptionId}
            />
            <FieldDescription id={descriptionId} className='text-xs'>
              最长可设置 525600 分钟，留空表示永久暂停
            </FieldDescription>
          </Field>
        </FieldGroup>
        <Button type='submit' disabled={props.disabled}>
          {props.pending ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <HugeiconsIcon icon={PauseIcon} data-icon='inline-start' />
          )}
          {paused ? '更新暂停时间' : '暂停路由流量'}
        </Button>
      </form>
    </section>
  )
}
