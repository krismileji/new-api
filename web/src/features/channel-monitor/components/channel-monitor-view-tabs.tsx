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
import {
  Analytics01Icon,
  ChartLineData01Icon,
  Layers01Icon,
  Route01Icon,
  TestTubeIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { TabsList, TabsTrigger } from '@/components/ui/tabs'

type ChannelMonitorViewTabsProps = {
  channelCount: number
  groupCount: number
  performanceModelCount: number
  smartSchedulePoolCount: number
  smartScheduleHasCriticalIssue: boolean
  smartScheduleHasProbing: boolean
}

export function ChannelMonitorViewTabs(props: ChannelMonitorViewTabsProps) {
  return (
    <TabsList className='no-scrollbar flex h-8 w-full flex-nowrap justify-start overflow-x-auto sm:w-fit'>
      <TabsTrigger value='channels'>
        <HugeiconsIcon icon={Analytics01Icon} data-icon='inline-start' />
        渠道 {props.channelCount}
      </TabsTrigger>
      <TabsTrigger value='status-probe'>
        <HugeiconsIcon icon={TestTubeIcon} data-icon='inline-start' />
        状态监测
      </TabsTrigger>
      <TabsTrigger value='smart-schedule'>
        <HugeiconsIcon icon={Route01Icon} data-icon='inline-start' />
        智能调度 {props.smartSchedulePoolCount}
        {props.smartScheduleHasCriticalIssue ? (
          <>
            <span
              className='bg-destructive size-1.5 shrink-0 rounded-full'
              aria-hidden='true'
            />
            <span className='sr-only'>存在需要关注的调度状态</span>
          </>
        ) : null}
        {props.smartScheduleHasProbing ? (
          <>
            <span
              className='bg-warning size-1.5 shrink-0 rounded-full'
              aria-hidden='true'
            />
            <span className='sr-only'>存在稳定性试放中的调度状态</span>
          </>
        ) : null}
      </TabsTrigger>
      <TabsTrigger value='groups'>
        <HugeiconsIcon icon={Layers01Icon} data-icon='inline-start' />
        分组 {props.groupCount}
      </TabsTrigger>
      <TabsTrigger value='models'>
        <HugeiconsIcon icon={ChartLineData01Icon} data-icon='inline-start' />
        模型性能 {props.performanceModelCount}
      </TabsTrigger>
    </TabsList>
  )
}
