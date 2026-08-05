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
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'

import type {
  ChannelMonitorPerformanceRangeMinutes,
  ChannelMonitorPerformanceRangeSource,
} from '../types'

type ChannelMonitorPerformanceRangeControlProps = {
  source: ChannelMonitorPerformanceRangeSource
  rangeMinutes: ChannelMonitorPerformanceRangeMinutes
  inputValue: string
  inputValid: boolean
  minMinutes: number
  maxMinutes: number
  onInputChange: (value: string) => void
  onApply: () => void
}

export function ChannelMonitorPerformanceRangeControl(
  props: ChannelMonitorPerformanceRangeControlProps
) {
  if (props.source === 'smart_schedule') {
    return (
      <Badge
        variant='outline'
        className='h-8 w-full justify-center px-3 sm:w-36'
        aria-label={`性能与成功率统计范围：近 ${props.rangeMinutes} 分钟，由智能调度性能窗口决定`}
      >
        近 {props.rangeMinutes} 分钟
      </Badge>
    )
  }

  return (
    <InputGroup className='w-full sm:w-36'>
      <InputGroupAddon>近</InputGroupAddon>
      <InputGroupInput
        type='number'
        min={props.minMinutes}
        max={props.maxMinutes}
        step={1}
        value={props.inputValue}
        onChange={(event) => props.onInputChange(event.target.value)}
        onBlur={props.onApply}
        onKeyDown={(event) => {
          if (event.key === 'Enter') event.currentTarget.blur()
        }}
        aria-label='性能与成功率统计范围（分钟）'
        aria-invalid={!props.inputValid}
        className='min-w-0 text-right font-mono'
      />
      <InputGroupAddon align='inline-end'>分钟</InputGroupAddon>
    </InputGroup>
  )
}
