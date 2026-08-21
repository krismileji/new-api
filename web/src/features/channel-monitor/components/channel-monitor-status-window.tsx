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
import type {
  ComponentProps,
  ComponentPropsWithoutRef,
  KeyboardEvent,
  ReactNode,
} from 'react'

import { Badge } from '@/components/ui/badge'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'
import { cn } from '@/lib/utils'

type ChannelMonitorStatusWindowBucket = {
  started_at: number
}

type ChannelMonitorStatusWindowGridProps = ComponentPropsWithoutRef<'div'> & {
  [attribute: `data-${string}`]: string | number | boolean | undefined
}

export type ChannelMonitorStatusWindowPresentation = {
  ariaLabel: string
  className: string
  state: string
}

export type ChannelMonitorStatusWindowDetail = {
  label: string
  value: number | string
}

type ChannelMonitorStatusWindowProps<
  TBucket extends ChannelMonitorStatusWindowBucket,
> = {
  buckets: readonly TBucket[]
  bucketSlot: string
  bucketStateDataAttribute: string
  gridProps: ChannelMonitorStatusWindowGridProps
  getBucketPresentation: (
    bucket: TBucket
  ) => ChannelMonitorStatusWindowPresentation
  renderDetails: (bucket: TBucket) => ReactNode
}

type ChannelMonitorStatusWindowDetailsProps = {
  timeRange: string
  status: string
  statusVariant: NonNullable<ComponentProps<typeof Badge>['variant']>
  description?: string
  details?: readonly ChannelMonitorStatusWindowDetail[]
  footerLabel?: string
  footerValue?: string
}

function setRovingBucket(trigger: HTMLElement) {
  const triggers = trigger.parentElement?.querySelectorAll<HTMLElement>(
    '[data-channel-monitor-status-window-trigger]'
  )
  if (!triggers) return
  for (const candidate of triggers) candidate.tabIndex = -1
  trigger.tabIndex = 0
}

function moveRovingBucket(
  event: KeyboardEvent<HTMLElement>,
  index: number,
  bucketCount: number
) {
  let nextIndex = index
  if (event.key === 'ArrowLeft') nextIndex = Math.max(0, index - 1)
  else if (event.key === 'ArrowRight') {
    nextIndex = Math.min(bucketCount - 1, index + 1)
  } else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = bucketCount - 1
  else return

  event.preventDefault()
  const triggers =
    event.currentTarget.parentElement?.querySelectorAll<HTMLElement>(
      '[data-channel-monitor-status-window-trigger]'
    )
  const nextTrigger = triggers?.item(nextIndex)
  if (!nextTrigger) return
  setRovingBucket(nextTrigger)
  nextTrigger.focus()
}

export function ChannelMonitorStatusWindowDetails(
  props: ChannelMonitorStatusWindowDetailsProps
) {
  return (
    <div data-slot='channel-monitor-status-window-details'>
      <div className='flex items-start justify-between gap-3 border-b px-3 py-2.5'>
        <div className='min-w-0'>
          <div className='text-muted-foreground text-[10px] leading-4'>
            时间范围
          </div>
          <div className='font-mono text-[11px] leading-4 tabular-nums'>
            {props.timeRange}
          </div>
        </div>
        <Badge variant={props.statusVariant}>{props.status}</Badge>
      </div>

      {props.description ? (
        <p className='text-muted-foreground px-3 py-3 text-xs leading-5'>
          {props.description}
        </p>
      ) : null}

      {props.details?.length ? (
        <dl className='grid grid-cols-[minmax(0,1fr)_auto] gap-x-5 gap-y-1.5 px-3 py-3 text-xs'>
          {props.details.map((detail) => (
            <div key={detail.label} className='contents'>
              <dt className='text-muted-foreground'>{detail.label}</dt>
              <dd className='text-right font-mono font-medium tabular-nums'>
                {typeof detail.value === 'number'
                  ? detail.value.toLocaleString('zh-CN')
                  : detail.value}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}

      {props.footerLabel ? (
        <div className='border-t px-3 py-2.5 text-xs'>
          <div className='text-muted-foreground'>{props.footerLabel}</div>
          <div className='mt-1 max-h-20 overflow-y-auto leading-5 break-all'>
            {props.footerValue || '无'}
          </div>
        </div>
      ) : null}
    </div>
  )
}

export function ChannelMonitorStatusWindow<
  TBucket extends ChannelMonitorStatusWindowBucket,
>(props: ChannelMonitorStatusWindowProps<TBucket>) {
  return (
    <HoverCard>
      {({ payload }) => {
        const activeBucket = payload as TBucket | undefined
        return (
          <>
            <div
              {...props.gridProps}
              className={cn(
                'grid h-2.5 min-w-0 gap-px overflow-hidden rounded-sm',
                props.gridProps.className
              )}
              style={{
                gridTemplateColumns: `repeat(${Math.max(1, props.buckets.length)}, minmax(0, 1fr))`,
                ...props.gridProps.style,
              }}
            >
              {props.buckets.map((bucket, index) => {
                const presentation = props.getBucketPresentation(bucket)
                const stateDataAttribute = {
                  [props.bucketStateDataAttribute]: presentation.state,
                }
                return (
                  <HoverCardTrigger
                    key={bucket.started_at}
                    payload={bucket}
                    delay={0}
                    closeDelay={100}
                    render={
                      <button
                        type='button'
                        className={cn(
                          'focus-visible:ring-ring/70 h-2.5 min-w-0 cursor-help outline-none focus-visible:ring-2 focus-visible:ring-inset',
                          presentation.className
                        )}
                        tabIndex={index === props.buckets.length - 1 ? 0 : -1}
                        aria-label={presentation.ariaLabel}
                        data-slot={props.bucketSlot}
                        data-channel-monitor-status-window-trigger=''
                        {...stateDataAttribute}
                        onClick={(event) => event.stopPropagation()}
                        onFocus={(event) =>
                          setRovingBucket(event.currentTarget)
                        }
                        onKeyDown={(event) =>
                          moveRovingBucket(event, index, props.buckets.length)
                        }
                      />
                    }
                  />
                )
              })}
            </div>
            {activeBucket ? (
              <HoverCardContent
                side='top'
                align='center'
                className='w-80 overflow-hidden p-0'
                onClick={(event) => event.stopPropagation()}
              >
                {props.renderDetails(activeBucket)}
              </HoverCardContent>
            ) : null}
          </>
        )
      }}
    </HoverCard>
  )
}
