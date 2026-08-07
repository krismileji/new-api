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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

import {
  clearChannelMonitorSmartScheduleRouteExploration,
  clearChannelMonitorSmartScheduleRouteStability,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY } from '../lib/query-options'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartScheduleClearDialogProps = {
  route: ChannelMonitorSmartScheduleRoute | null
  onOpenChange: (open: boolean) => void
}

type ChannelMonitorSmartScheduleClearRequest = {
  channelId: number
  group: string
  model: string
  kind: 'stability' | 'exploration'
}

export function ChannelMonitorSmartScheduleClearDialog(
  props: ChannelMonitorSmartScheduleClearDialogProps
) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: async (request: ChannelMonitorSmartScheduleClearRequest) => {
      const routeRequest = {
        channelId: request.channelId,
        group: request.group,
        model: request.model,
      }
      if (request.kind === 'exploration') {
        return await clearChannelMonitorSmartScheduleRouteExploration(
          routeRequest
        )
      }
      return await clearChannelMonitorSmartScheduleRouteStability(routeRequest)
    },
    onError: handleChannelMonitorMutationError,
    onSuccess: (response, request) => {
      props.onOpenChange(false)
      let message = '当前路由没有需要解除的保护'
      if (request.kind === 'exploration') {
        message = response.data.cleared
          ? `已解除探索流量，恢复 P${response.data.priority} / W${response.data.weight}`
          : '当前路由没有需要解除的探索流量'
      } else if (response.data.cleared) {
        message = `已解除保护，恢复 P${response.data.priority} / W${response.data.weight}`
      }
      toast.success(message)
    },
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
      })
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })

  const clearingExploration =
    props.route?.state.stability_state === '' &&
    props.route.state.temporary_traffic_kind === 'insufficient_samples'
  let title = '确认解除智能调度保护？'
  let stateLabel = '稳定性试放'
  if (props.route?.state.stability_state === 'degraded') {
    stateLabel = '稳定性降级'
  }
  if (clearingExploration) {
    title = '确认解除探索流量？'
    stateLabel = '探索流量'
  }

  return (
    <AlertDialog
      open={props.route != null}
      onOpenChange={(open) => {
        if (!open && mutation.isPending) return
        props.onOpenChange(open)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>
            将立即清除“{props.route?.channel_name} / {props.route?.group} /{' '}
            {props.route?.model}”的{stateLabel}
            状态，并恢复调整前保存的优先级和权重。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            取消
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={mutation.isPending || props.route == null}
            onClick={() => {
              if (!props.route) return
              mutation.mutate({
                channelId: props.route.channel_id,
                group: props.route.group,
                model: props.route.model,
                kind: clearingExploration ? 'exploration' : 'stability',
              })
            }}
          >
            {mutation.isPending ? '解除中...' : '确认解除'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
