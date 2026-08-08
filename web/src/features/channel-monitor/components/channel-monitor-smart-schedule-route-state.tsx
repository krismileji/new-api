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

import {
  channelMonitorSmartScheduleRouteIsAvailable,
  channelMonitorSmartScheduleRouteIsTrafficPaused,
  channelMonitorSmartScheduleRouteParticipates,
} from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartScheduleRouteStateProps = {
  route: ChannelMonitorSmartScheduleRoute
  onProtectedStatusClick: () => void
}

export function ChannelMonitorSmartScheduleRouteState(
  props: ChannelMonitorSmartScheduleRouteStateProps
) {
  const route = props.route
  let clearProtectionLabel: string | undefined
  if (route.state.stability_state === 'degraded') {
    clearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性降级保护`
  } else if (route.state.stability_state === 'probing') {
    clearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性试放`
  } else if (route.state.temporary_traffic_kind === 'insufficient_samples') {
    clearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的探索流量`
  } else if (route.state.temporary_traffic_kind === 'adaptive_sampling') {
    clearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的健康应急采样`
  }
  if (
    route.channel_status === CHANNEL_STATUS.ENABLED &&
    route.enabled &&
    channelMonitorSmartScheduleRouteIsTrafficPaused(route)
  ) {
    return <Badge variant='warning'>流量已暂停</Badge>
  }
  if (!channelMonitorSmartScheduleRouteIsAvailable(route)) {
    const unavailableLabel =
      route.channel_status !== CHANNEL_STATUS.ENABLED ? '渠道禁用' : '路由禁用'
    if (clearProtectionLabel) {
      return (
        <Badge
          render={<button type='button' />}
          variant='destructive'
          className='cursor-pointer'
          title={clearProtectionLabel}
          aria-label={clearProtectionLabel}
          onClick={props.onProtectedStatusClick}
        >
          {unavailableLabel}
        </Badge>
      )
    }
    if (route.channel_status !== CHANNEL_STATUS.ENABLED) {
      return <Badge variant='destructive'>渠道禁用</Badge>
    }
    return <Badge variant='destructive'>路由禁用</Badge>
  }
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
  if (route.state.temporary_traffic_kind === 'insufficient_samples') {
    return (
      <Badge
        render={<button type='button' />}
        variant='warning'
        className='cursor-pointer'
        onClick={props.onProtectedStatusClick}
        aria-label={`解除 ${route.channel_name} ${route.group} ${route.model} 的探索流量`}
      >
        样本不足补量
      </Badge>
    )
  }
  if (route.state.temporary_traffic_kind === 'adaptive_sampling') {
    return (
      <Badge
        render={<button type='button' />}
        variant='warning'
        className='cursor-pointer'
        onClick={props.onProtectedStatusClick}
        aria-label={`解除 ${route.channel_name} ${route.group} ${route.model} 的健康应急采样`}
      >
        健康应急采样
      </Badge>
    )
  }
  if (route.state.temporary_traffic_kind === 'priority_sampling') {
    return <Badge variant='warning'>低优先级轮转</Badge>
  }
  if (!channelMonitorSmartScheduleRouteParticipates(route)) {
    return <Badge variant='outline'>未参与</Badge>
  }
  if (route.state.last_schedule_status === 'failed') {
    return <Badge variant='destructive'>调度失败</Badge>
  }
  if (
    route.state.adaptive_health_state === 'high_risk' ||
    route.state.adaptive_health_state === 'pressure'
  ) {
    return (
      <Badge variant='warning'>
        {route.state.adaptive_health_state === 'high_risk'
          ? '主渠道高风险'
          : '主渠道降压'}
      </Badge>
    )
  }
  if (route.state.adaptive_health_state === 'observation') {
    return <Badge variant='outline'>主渠道观察</Badge>
  }
  return <Badge variant='secondary'>参与调度</Badge>
}
