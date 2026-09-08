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
import { CheckmarkCircle02Icon, ViewIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useAuthStore } from '@/stores/auth-store'

import type { ChannelMonitorRuntimeInput } from '../lib/runtime-status'

const acknowledgmentSchema = z.object({
  fingerprint: z.string(),
  dailyGapDay: z.number().int().nonnegative(),
})

type HistoryAcknowledgment = z.infer<typeof acknowledgmentSchema>
type ChannelMonitorHistoryNoticeProps = ChannelMonitorRuntimeInput & {
  notices: string[]
  healthy: boolean
}

export function ChannelMonitorHistoryNotice(
  props: ChannelMonitorHistoryNoticeProps
) {
  const userId = useAuthStore((state) => state.auth.user?.id)
  if (props.notices.length === 0) return null
  const storageKey = `channel-monitor:history-acknowledgment:v1:${JSON.stringify(
    [userId ?? null, props.recovery?.node_id ?? null]
  )}`
  const canAcknowledge =
    props.healthy &&
    props.metadata !== undefined &&
    !props.recoveryFailed &&
    !props.recoveryLoading &&
    !!props.recovery?.node_id &&
    userId !== undefined

  return (
    <ChannelMonitorHistoryAcknowledgment
      key={storageKey}
      {...props}
      storageKey={storageKey}
      canAcknowledge={canAcknowledge}
    />
  )
}

function ChannelMonitorHistoryAcknowledgment(
  props: ChannelMonitorHistoryNoticeProps & {
    storageKey: string
    canAcknowledge: boolean
  }
) {
  const [saved, setSaved] = useState<HistoryAcknowledgment | null>(() => {
    try {
      const stored: unknown = JSON.parse(
        localStorage.getItem(props.storageKey) ?? 'null'
      )
      const parsed = acknowledgmentSchema.safeParse(stored)
      return parsed.success ? parsed.data : null
    } catch {
      return null
    }
  })
  const [saveFailed, setSaveFailed] = useState(false)
  const metadata = props.metadata
  const gapReasons = new Set(props.recovery?.data_gap_reasons ?? [])
  const dailyGap = metadata?.degraded_reasons?.includes(
    'daily_replay_incomplete'
  )
  if (dailyGap) gapReasons.add('daily_replay_incomplete')
  const fingerprint = JSON.stringify([
    [...gapReasons].sort(),
    props.recovery?.recovered_at,
    metadata?.quarantine_count,
    metadata?.last_quarantined_at,
    metadata?.writer_dropped_events,
    metadata?.cost_publish_failed_count,
    metadata?.cost_dead_letter_count,
  ])
  // Daily statistics follow Beijing time. Only a newly affected day renews this notice.
  const observedAt = metadata?.generated_at ?? props.recovery?.checked_at ?? 0
  let dailyGapDay = 0
  if (dailyGap && Number.isFinite(observedAt) && observedAt > 0) {
    dailyGapDay = Math.floor((observedAt + 8 * 3600) / 86400)
  }
  const acknowledged =
    saved?.fingerprint === fingerprint && saved.dailyGapDay >= dailyGapDay
  const actionLabel = acknowledged ? '重新显示历史提示' : '已知晓本次历史缺口'

  return (
    <div className='flex min-w-0 flex-wrap items-start gap-x-3 gap-y-1'>
      {acknowledged ? (
        <span className='text-muted-foreground text-xs'>历史提示已知晓</span>
      ) : (
        <ul
          className='text-muted-foreground flex min-w-0 flex-1 flex-wrap gap-x-4 gap-y-1 text-xs font-normal'
          aria-label='监控历史提示'
        >
          {props.notices.map((notice) => (
            <li key={notice} className='min-w-0 break-words'>
              {notice}
            </li>
          ))}
        </ul>
      )}
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='xs'
              className='shrink-0'
              aria-label={actionLabel}
              disabled={!acknowledged && !props.canAcknowledge}
              onClick={() => {
                try {
                  const next = acknowledged
                    ? null
                    : { fingerprint, dailyGapDay }
                  if (next) {
                    localStorage.setItem(props.storageKey, JSON.stringify(next))
                  } else {
                    localStorage.removeItem(props.storageKey)
                  }
                  setSaved(next)
                  setSaveFailed(false)
                } catch {
                  setSaveFailed(true)
                }
              }}
            />
          }
        >
          <HugeiconsIcon
            icon={acknowledged ? ViewIcon : CheckmarkCircle02Icon}
            data-icon='inline-start'
          />
          {acknowledged ? '重新显示' : '已知晓'}
        </TooltipTrigger>
        <TooltipContent>
          {acknowledged ? actionLabel : '在本浏览器收起本次历史提示'}
        </TooltipContent>
      </Tooltip>
      {saveFailed ? (
        <p className='text-destructive w-full text-xs' role='alert'>
          无法保存已知晓状态，请检查浏览器存储权限后重试。
        </p>
      ) : null}
    </div>
  )
}
