import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import { useChannelMonitorAnalytics } from './use-channel-monitor-analytics'

export function useChannelMonitorTodayProfit(
  channelId?: number,
  enabled = true
) {
  const today = formatChannelMonitorBeijingDate(new Date())
  const tomorrow = formatChannelMonitorBeijingDate(
    new Date(new Date(`${today}T00:00:00+08:00`).getTime() + 86400000)
  )
  return useChannelMonitorAnalytics(
    {
      metric: 'profit',
      groupBy: 'channel',
      from: today,
      to: tomorrow,
      channelId,
      sort: 'profit',
      direction: 'desc',
      pageSize: 200,
    },
    enabled
  )
}
