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

import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartSchedulePrimaryConfirmDialogProps = {
  route: ChannelMonitorSmartScheduleRoute | null
  channelRule: boolean
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}

export function ChannelMonitorSmartSchedulePrimaryConfirmDialog(
  props: ChannelMonitorSmartSchedulePrimaryConfirmDialogProps
) {
  const protectionLabel =
    props.route?.state.stability_state === 'degraded'
      ? '稳定性降级'
      : '稳定性试放'
  let confirmLabel = '解除保护并固定'
  if (props.pending) {
    confirmLabel = '处理中...'
  } else if (props.channelRule) {
    confirmLabel = '解除保护并固定渠道'
  }

  return (
    <AlertDialog open={props.route != null} onOpenChange={props.onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {props.channelRule
              ? '部分模型路由处于稳定性保护中'
              : '该路由正在稳定性保护中'}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {props.channelRule ? (
              <>
                {props.route?.channel_name}{' '}
                的一个或多个参与调度的模型路由当前处于稳定性保护状态。继续后会解除这些路由的保护，并固定当前及以后新增的符合条件的模型路由。
                如果保留“允许稳定性降级”，后续再次达到保护失败阈值时仍会重新进入保护。
              </>
            ) : (
              <>
                “{props.route?.channel_name} / {props.route?.group} /{' '}
                {props.route?.model}”当前处于{protectionLabel}
                状态。继续后会立即解除当前保护并固定为主渠道；如果保留“允许稳定性降级”，后续再次达到保护失败阈值时仍会重新进入保护。
              </>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={props.pending || props.route == null}
            onClick={props.onConfirm}
          >
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
