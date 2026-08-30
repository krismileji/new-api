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
import type { QueryClient } from '@tanstack/react-query'
import { Loader2, Power, PowerOff } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { handleToggleChannelStatus } from '@/features/channels/lib/channel-actions'

type ChannelMonitorDisableChannelActionProps = {
  channelId: number
  channelStatus: number
  queryClient?: QueryClient
  disabled?: boolean
  onStatusChanged?: () => void | Promise<void>
}

export function ChannelMonitorDisableChannelAction(
  props: ChannelMonitorDisableChannelActionProps
) {
  const [pending, setPending] = useState(false)
  const channelEnabled = props.channelStatus === CHANNEL_STATUS.ENABLED
  const actionLabel = channelEnabled ? '禁用渠道' : '启用渠道'

  async function toggleStatus() {
    setPending(true)
    let statusChanged = false
    try {
      await handleToggleChannelStatus(
        props.channelId,
        props.channelStatus,
        props.queryClient,
        () => {
          statusChanged = true
        }
      )
      if (statusChanged) await props.onStatusChanged?.()
    } finally {
      setPending(false)
    }
  }

  const disabled = props.disabled || pending
  let triggerIcon = <Power className='size-4' />
  if (pending) {
    triggerIcon = <Loader2 className='size-4 animate-spin' />
  } else if (channelEnabled) {
    triggerIcon = <PowerOff className='size-4' />
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            disabled={disabled}
            onClick={() => void toggleStatus()}
            aria-label={actionLabel}
            className={
              channelEnabled
                ? 'text-destructive hover:text-destructive'
                : 'text-success hover:text-success'
            }
          />
        }
      >
        {triggerIcon}
      </TooltipTrigger>
      <TooltipContent>{actionLabel}</TooltipContent>
    </Tooltip>
  )
}
