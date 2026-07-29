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
import { CHANNEL_STATUS } from '@/features/channels/constants'

import { channelMonitorSmartScheduleRouteParticipates } from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartScheduleRouteStateProps = {
  route: ChannelMonitorSmartScheduleRoute
  onProtectedStatusClick: () => void
}

export function ChannelMonitorSmartScheduleRouteState(
  props: ChannelMonitorSmartScheduleRouteStateProps
) {
  const route = props.route
  if (route.state.stability_state === 'degraded') {
    return (
      <Badge
        render={<button type='button' />}
        variant='destructive'
        className='cursor-pointer'
        onClick={props.onProtectedStatusClick}
        aria-label={`解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性降级保护`}
      >
        稳定性降级
      </Badge>
    )
  }
  if (route.state.stability_state === 'probing') {
    return (
      <Badge
        render={<button type='button' />}
        variant='warning'
        className='cursor-pointer'
        onClick={props.onProtectedStatusClick}
        aria-label={`解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性试放`}
      >
        稳定性试放
      </Badge>
    )
  }
  if (route.state.exploration_active) {
    return <Badge variant='warning'>探索采样</Badge>
  }
  if (route.channel_status !== CHANNEL_STATUS.ENABLED) {
    return <Badge variant='destructive'>渠道禁用</Badge>
  }
  if (!route.enabled) return <Badge variant='destructive'>路由禁用</Badge>
  if (!channelMonitorSmartScheduleRouteParticipates(route)) {
    return <Badge variant='outline'>未参与</Badge>
  }
  if (route.state.last_schedule_status === 'failed') {
    return <Badge variant='destructive'>调度失败</Badge>
  }
  return <Badge variant='secondary'>参与调度</Badge>
}
