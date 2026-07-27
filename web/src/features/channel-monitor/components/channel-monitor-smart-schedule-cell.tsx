import { ArrowRight01Icon } from '@hugeicons/core-free-icons'
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
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { CHANNEL_STATUS } from '@/features/channels/constants'

import {
  formatChannelMonitorSmartSchedulePriorityWeightRange,
  summarizeChannelMonitorSmartScheduleChannel,
} from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartScheduleCellProps = {
  routes: readonly ChannelMonitorSmartScheduleRoute[]
  pending: boolean
  onUpdate: (excluded: boolean) => void
  onOpen: () => void
}

export function ChannelMonitorSmartScheduleCell(
  props: ChannelMonitorSmartScheduleCellProps
) {
  const summary = summarizeChannelMonitorSmartScheduleChannel(props.routes)
  if (!summary) {
    return <span className='text-muted-foreground text-sm'>暂无路由</span>
  }

  const participating = summary.participatingCount > 0
  const partiallyParticipating =
    participating && summary.participatingCount < summary.routeCount
  const channelEnabled = props.routes.some(
    (route) => route.channel_status === CHANNEL_STATUS.ENABLED
  )
  const visibleGroups = summary.groups.slice(0, 3)
  const hiddenGroupCount = summary.groups.length - visibleGroups.length

  return (
    <div className='flex min-w-[230px] flex-col gap-2'>
      <div className='flex items-start gap-2'>
        <button
          type='button'
          className='group focus-visible:ring-ring min-w-0 flex-1 rounded-md text-left outline-none focus-visible:ring-2'
          onClick={props.onOpen}
          aria-label={`查看 ${summary.channelName} 的智能调度详情`}
        >
          <div className='flex items-center gap-1.5'>
            <span className='font-medium'>
              {summary.participatingCount}/{summary.routeCount} 路由参与
            </span>
            <HugeiconsIcon
              icon={ArrowRight01Icon}
              className='text-muted-foreground transition-transform group-hover:translate-x-0.5'
            />
          </div>
          <div className='mt-1 flex flex-col gap-1'>
            {visibleGroups.map((group) => (
              <div
                key={group.group}
                className='flex min-w-0 items-center justify-between gap-2 text-xs'
              >
                <span className='min-w-0 truncate' title={group.group}>
                  {group.group}
                </span>
                <span className='shrink-0 font-mono tabular-nums'>
                  {formatChannelMonitorSmartSchedulePriorityWeightRange(group)}
                </span>
              </div>
            ))}
            {hiddenGroupCount > 0 ? (
              <span className='text-muted-foreground text-xs'>
                还有 {hiddenGroupCount} 个分组
              </span>
            ) : null}
          </div>
        </button>
        <div
          className='flex shrink-0 items-center gap-1.5 pt-0.5'
          onClick={(event) => event.stopPropagation()}
        >
          {props.pending ? <Spinner /> : null}
          <Switch
            checked={participating}
            disabled={props.pending}
            onCheckedChange={(checked) => props.onUpdate(!checked)}
            aria-label={`${participating ? '取消' : '开启'} ${summary.channelName} 的智能调度参与`}
          />
        </div>
      </div>
      <div className='flex flex-wrap items-center gap-1.5'>
        <Badge variant={channelEnabled ? 'secondary' : 'outline'}>
          {channelEnabled ? '可调度' : '渠道禁用'}
        </Badge>
        {partiallyParticipating ? (
          <Badge variant='outline'>部分参与</Badge>
        ) : null}
        {summary.degradedCount > 0 ? (
          <Badge variant='destructive'>低成功率 {summary.degradedCount}</Badge>
        ) : null}
        {summary.probingCount > 0 ? (
          <Badge variant='warning'>稳定性试放 {summary.probingCount}</Badge>
        ) : null}
      </div>
    </div>
  )
}
