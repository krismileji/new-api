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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { getChannelMonitorTodaySuccess } from '../api'
import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import { CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS } from '../lib/query-options'

export function useChannelMonitorDailyInsight(open: boolean) {
  const [days, setDays] = useState(30)
  const [selectedDate, setSelectedDate] = useState(() =>
    formatChannelMonitorBeijingDate(new Date())
  )
  const query = useQuery({
    queryKey: ['channel-monitor', 'success', 'daily', days, selectedDate],
    queryFn: () => getChannelMonitorTodaySuccess({ days, date: selectedDate }),
    enabled: open,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
  })

  const changeDays = (nextDays: number) => {
    setDays(nextDays)
    setSelectedDate(formatChannelMonitorBeijingDate(new Date()))
  }

  return {
    days,
    selectedDate,
    query,
    changeDays,
    changeDate: setSelectedDate,
  }
}
