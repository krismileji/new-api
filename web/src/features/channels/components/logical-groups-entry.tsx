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
import { Settings02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { LogicalGroupsDialog } from './dialogs/logical-groups-dialog'

export function LogicalGroupsEntry() {
  const [open, setOpen] = useState(false)
  const currentUser = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  const canEdit = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.WRITE
  )
  const canDelete = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )

  return (
    <>
      <Tooltip>
        <TooltipTrigger render={<span className='inline-flex' />}>
          <Button
            variant='outline'
            size='icon'
            onClick={() => {
              if (canRead) setOpen(true)
            }}
            disabled={!canRead}
            aria-label='同渠道配置'
          >
            <HugeiconsIcon icon={Settings02Icon} data-icon='inline-start' />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {canRead ? '配置请求地址一致的渠道组' : '没有渠道查看权限'}
        </TooltipContent>
      </Tooltip>

      <LogicalGroupsDialog
        open={open}
        onOpenChange={setOpen}
        canEdit={canEdit}
        canDelete={canDelete}
      />
    </>
  )
}
