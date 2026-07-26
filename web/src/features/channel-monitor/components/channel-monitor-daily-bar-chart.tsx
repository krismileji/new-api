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
import { VChart } from '@visactor/react-vchart'
import type { EventParamsDefinition } from '@visactor/vchart'

import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { getChannelMonitorChartEventDate } from '../lib/daily-chart'

type ChannelMonitorDailyBarChartProps = {
  ariaLabel: string
  chartKey: string
  spec: Record<string, unknown>
  onDateChange: (date: string) => void
}

type VChartClickEvent = EventParamsDefinition['click']

export function ChannelMonitorDailyBarChart(
  props: ChannelMonitorDailyBarChartProps
) {
  const { resolvedTheme, themeReady } = useChartTheme()

  return (
    <div
      className='h-48 overflow-hidden rounded-md border sm:h-56'
      role='img'
      aria-label={props.ariaLabel}
    >
      {themeReady ? (
        <VChart
          key={`${props.chartKey}:${resolvedTheme}`}
          spec={{
            ...props.spec,
            theme: resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
          onClick={(event: VChartClickEvent) => {
            const date = getChannelMonitorChartEventDate(event)
            if (date) props.onDateChange(date)
          }}
        />
      ) : null}
    </div>
  )
}
