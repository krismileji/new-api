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
import type { ReactNode } from 'react'

import { ScrollArea } from '@/components/ui/scroll-area'

type ChannelMonitorSmartScheduleExecutionLayoutProps = {
  taskList: ReactNode
  children: ReactNode
}

export function ChannelMonitorSmartScheduleExecutionLayout(
  props: ChannelMonitorSmartScheduleExecutionLayoutProps
) {
  return (
    <div
      className='bg-background grid h-full min-h-0 grid-rows-[13rem_minmax(0,1fr)] gap-0 lg:grid-cols-[18rem_minmax(0,1fr)] lg:grid-rows-1'
      data-schedule-execution-layout
    >
      <ScrollArea className='bg-muted/[0.04] min-h-0 border-b lg:border-r lg:border-b-0'>
        {props.taskList}
      </ScrollArea>
      <div
        className='bg-background min-h-0 overflow-y-auto lg:flex lg:flex-col lg:overflow-hidden'
        data-schedule-execution-details
      >
        {props.children}
      </div>
    </div>
  )
}

export function ChannelMonitorSmartScheduleExecutionAdjustments(props: {
  children: ReactNode
}) {
  return (
    <div
      className='shrink-0 lg:min-h-0 lg:flex-1 lg:overflow-y-auto'
      data-schedule-execution-adjustments
    >
      {props.children}
    </div>
  )
}
