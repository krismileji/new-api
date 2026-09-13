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

import { getChannelMonitorRecovery } from '../api'
import { useChannelMonitorDiagnostics } from '../hooks/use-channel-monitor-diagnostics'
import { CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS } from '../lib/query-options'
import type { ChannelMonitorRealtimeMetadata } from '../types'
import type { ChannelMonitorDiagnosticsInput } from '../types-diagnostics'
import type { ChannelMonitorRecovery } from '../types-recovery'
import { ChannelMonitorRealtimeStatus } from './channel-monitor-realtime-status'

export function ChannelMonitorHealthStatus(props: {
  metadata?: ChannelMonitorRealtimeMetadata
}) {
  const diagnostics = useChannelMonitorDiagnostics()
  const query = useQuery({
    queryKey: ['channel-monitor', 'health'],
    queryFn: getChannelMonitorRecovery,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
  })

  return (
    <ChannelMonitorRecoverySummary
      data={query.data?.data}
      failed={query.isError}
      metadata={props.metadata}
      diagnostics={diagnostics}
    />
  )
}

export function ChannelMonitorRecoverySummary(props: {
  data?: ChannelMonitorRecovery
  failed?: boolean
  metadata?: ChannelMonitorRealtimeMetadata
  diagnostics?: ChannelMonitorDiagnosticsInput
}) {
  return (
    <ChannelMonitorRealtimeStatus
      metadata={props.metadata}
      recovery={props.data}
      recoveryFailed={props.failed}
      recoveryLoading={!props.failed && !props.data}
      diagnostics={props.diagnostics}
    />
  )
}
