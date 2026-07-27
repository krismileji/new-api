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
import type { ChannelMonitorItem, ChannelMonitorSettings } from '../types'
import { ChannelMonitorSmartScheduleChannelPanel } from './channel-monitor-smart-schedule-channel-panel'
import { ChannelMonitorSmartSchedulePanel } from './channel-monitor-smart-schedule-panel'

type ChannelMonitorSmartScheduleTabProps = {
  active: boolean
  channels: ChannelMonitorItem[]
  settings: ChannelMonitorSettings
  onOpenSettings: () => void
}

export function ChannelMonitorSmartScheduleTab(
  props: ChannelMonitorSmartScheduleTabProps
) {
  if (props.settings.smart_schedule_scope === 'group_model') {
    return (
      <ChannelMonitorSmartSchedulePanel
        active={props.active}
        onOpenSettings={props.onOpenSettings}
      />
    )
  }

  return (
    <ChannelMonitorSmartScheduleChannelPanel
      channels={props.channels}
      enabled={props.settings.smart_schedule_enabled}
      onOpenSettings={props.onOpenSettings}
    />
  )
}
