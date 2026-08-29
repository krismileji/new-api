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
import { InformationCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

export function ChannelMonitorFieldInfo(props: {
  label: string
  description: string
}) {
  return (
    <TooltipProvider delay={150}>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type='button'
              className='text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex size-5 shrink-0 cursor-help items-center justify-center rounded-sm transition-colors focus-visible:ring-2 focus-visible:outline-none'
              aria-label={`查看“${props.label}”说明`}
            >
              <HugeiconsIcon
                icon={InformationCircleIcon}
                className='size-3.5'
                aria-hidden='true'
              />
            </button>
          }
        />
        <TooltipContent
          side='top'
          className='max-w-80 text-left leading-5 whitespace-normal'
        >
          {props.description}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
