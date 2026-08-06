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

  return (
    <AlertDialog open={props.route != null} onOpenChange={props.onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>该路由正在稳定性保护中</AlertDialogTitle>
          <AlertDialogDescription>
            “{props.route?.channel_name} / {props.route?.group} /{' '}
            {props.route?.model}”当前处于{protectionLabel}
            状态。继续后会立即解除当前保护并固定为主渠道；如果保留“允许稳定性降级”，后续再次达到保护失败阈值时仍会重新进入保护。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.pending}>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={props.pending || props.route == null}
            onClick={props.onConfirm}
          >
            {props.pending ? '处理中...' : '解除保护并固定'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
