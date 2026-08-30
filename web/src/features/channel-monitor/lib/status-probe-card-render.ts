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
import type { QueryClient } from '@tanstack/react-query'

import type { ChannelStatusProbeChannel } from '../types'

export type ChannelStatusProbeCardRenderProps = {
  channel: ChannelStatusProbeChannel
  channelQueryClient?: QueryClient
  serverNow: number
  actionPending: boolean
  onOpenHistory: (channelId: number) => void
  onOpenConfig: (channelId: number) => void
  onRun: (channel: ChannelStatusProbeChannel) => void
  onToggleEnabled: (channel: ChannelStatusProbeChannel) => void
  onChannelStatusChanged?: () => void | Promise<void>
}

export function formatChannelStatusProbeNextRun(
  target: number,
  serverNow: number
) {
  if (target <= 0) return '-'
  const seconds = Math.max(0, target - serverNow)
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.ceil(seconds / 60)} 分钟`
  return `${Math.ceil(seconds / 3600)} 小时`
}

export function areChannelStatusProbeCardPropsEqual(
  previous: Readonly<ChannelStatusProbeCardRenderProps>,
  next: Readonly<ChannelStatusProbeCardRenderProps>
) {
  if (
    previous.channel !== next.channel ||
    previous.channelQueryClient !== next.channelQueryClient ||
    previous.actionPending !== next.actionPending ||
    previous.onOpenHistory !== next.onOpenHistory ||
    previous.onOpenConfig !== next.onOpenConfig ||
    previous.onRun !== next.onRun ||
    previous.onToggleEnabled !== next.onToggleEnabled ||
    previous.onChannelStatusChanged !== next.onChannelStatusChanged
  ) {
    return false
  }

  return (
    formatChannelStatusProbeNextRun(
      previous.channel.config?.next_run_at ?? 0,
      previous.serverNow
    ) ===
    formatChannelStatusProbeNextRun(
      next.channel.config?.next_run_at ?? 0,
      next.serverNow
    )
  )
}
