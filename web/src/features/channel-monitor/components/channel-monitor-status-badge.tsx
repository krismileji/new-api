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
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { cn } from '@/lib/utils'

import { getChannelMonitorStatusLabel } from '../constants'

type ChannelMonitorStatusBadgeProps = {
  status: number
  reason?: string | null
  className?: string
}

export function ChannelMonitorStatusBadge(
  props: ChannelMonitorStatusBadgeProps
) {
  const label = getChannelMonitorStatusLabel(props.status)

  if (props.status === CHANNEL_STATUS.MANUAL_DISABLED) {
    return (
      <Badge variant='destructive' className={props.className}>
        {label}
      </Badge>
    )
  }

  if (props.status === CHANNEL_STATUS.AUTO_DISABLED) {
    const reason = props.reason?.trim() || '未记录系统禁用原因'

    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <Badge
              variant='outline'
              className={cn(
                'border-warning/40 bg-warning/10 text-warning',
                props.className
              )}
              tabIndex={0}
              aria-label={`${label}，原因：${reason}`}
            />
          }
        >
          {label}
        </TooltipTrigger>
        <TooltipContent className='max-w-xs whitespace-normal break-words'>
          系统禁用原因：{reason}
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <Badge variant='secondary' className={props.className}>
      {label}
    </Badge>
  )
}
