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
import { Alert02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorPerformanceMetricCoverage } from '../types'

type ChannelMonitorPerformanceCoverageAlertProps = {
  coverage?: ChannelMonitorPerformanceMetricCoverage
  rangeLabel: string
}

export function ChannelMonitorPerformanceCoverageAlert(
  props: ChannelMonitorPerformanceCoverageAlertProps
) {
  if (!props.coverage?.aggregation_enabled || props.coverage.window_complete) {
    return null
  }

  const coveredFrom =
    props.coverage.aggregated_from > 0
      ? formatTimestampToDate(props.coverage.aggregated_from)
      : '尚未建立'
  const coveredThrough =
    props.coverage.aggregated_through > 0
      ? formatTimestampToDate(props.coverage.aggregated_through)
      : '尚未建立'

  return (
    <Alert>
      <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
      <AlertTitle>{props.rangeLabel}统计窗口数据尚未覆盖完整</AlertTitle>
      <AlertDescription>
        当前分钟汇总覆盖从 {coveredFrom} 到 {coveredThrough}
        ，当前请求数、成功率和性能数据可能偏低。
      </AlertDescription>
    </Alert>
  )
}
