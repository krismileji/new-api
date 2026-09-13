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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import {
  getChannelMonitorDiagnostics,
  resetChannelMonitorDiagnostics,
} from '../api'
import {
  CHANNEL_MONITOR_DIAGNOSTICS_QUERY_KEY,
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
} from '../lib/query-options'
import type { ChannelMonitorDiagnosticsInput } from '../types-diagnostics'

export function useChannelMonitorDiagnostics(): ChannelMonitorDiagnosticsInput {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: CHANNEL_MONITOR_DIAGNOSTICS_QUERY_KEY,
    queryFn: ({ signal }) => getChannelMonitorDiagnostics(signal),
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const reset = useMutation({
    mutationFn: resetChannelMonitorDiagnostics,
    onMutate: () =>
      client.cancelQueries({
        queryKey: CHANNEL_MONITOR_DIAGNOSTICS_QUERY_KEY,
        exact: true,
      }),
    onSuccess: (response) => {
      client.setQueryData(CHANNEL_MONITOR_DIAGNOSTICS_QUERY_KEY, response)
    },
    onError: () => {
      void client.invalidateQueries({
        queryKey: CHANNEL_MONITOR_DIAGNOSTICS_QUERY_KEY,
        exact: true,
      })
    },
  })
  return {
    data: query.data?.data,
    loading: query.isPending,
    failed: query.isError,
    onReset: reset.mutateAsync,
  }
}
