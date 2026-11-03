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
import { useMutation } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { Power, PowerOff, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { manageUsers } from '../api'
import { ERROR_MESSAGES } from '../constants'
import type { BulkUserManageAction, User } from '../types'
import { useUsers } from './users-provider'

type DataTableBulkActionsProps = {
  table: Table<User>
  selectedCount?: number
}

type UserManageRequest = {
  action: BulkUserManageAction
  ids: number[]
}

const MAX_BULK_USER_COUNT = 500

export function DataTableBulkActions({
  table,
  selectedCount,
}: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const [targets, setTargets] = useState<UserManageRequest | null>(null)

  const mutation = useMutation({
    mutationFn: async (request: UserManageRequest) => {
      const result = await manageUsers(request.ids, request.action)
      if (!result.success || !result.data) throw createServerError(result)
      return { request, result: result.data }
    },
    onSuccess: ({ request, result }) => {
      if (result.failed_ids.length > 0) {
        toast.error(
          t('Processed {{succeeded}} of {{total}} users; {{failed}} failed.', {
            succeeded: result.succeeded_ids.length,
            total: request.ids.length,
            failed: result.failed_ids.length,
          })
        )
      } else {
        let successMessage: string
        switch (request.action) {
          case 'disable':
            successMessage = t('Disabled {{count}} users', {
              count: result.succeeded_ids.length,
            })
            break
          case 'enable':
            successMessage = t('Enabled {{count}} users', {
              count: result.succeeded_ids.length,
            })
            break
          case 'delete':
            successMessage = t('Deleted {{count}} users', {
              count: result.succeeded_ids.length,
            })
            break
        }
        toast.success(successMessage)
      }

      table.setRowSelection((previous) => {
        const next = { ...previous }
        for (const id of result.succeeded_ids) delete next[String(id)]
        return next
      })
      setTargets(null)
      triggerRefresh()
    },
    onError: (error) => {
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    },
  })

  const selectedIds = Object.entries(table.getState().rowSelection)
    .filter(([, selected]) => selected)
    .map(([id]) => Number(id))
    .filter((id) => Number.isSafeInteger(id) && id > 0)

  const openConfirmation = (action: BulkUserManageAction) => {
    if (selectedIds.length > MAX_BULK_USER_COUNT) {
      toast.error(t('Select no more than 500 users at a time.'))
      return
    }
    if (selectedIds.length > 0) setTargets({ action, ids: selectedIds })
  }

  let confirmationTitle = ''
  let confirmationDescription = ''
  if (targets?.action === 'disable') {
    confirmationTitle = t('Disable access for {{count}} selected users?', {
      count: targets.ids.length,
    })
    confirmationDescription = t(
      'These users will lose access until they are enabled again.'
    )
  } else if (targets?.action === 'enable') {
    confirmationTitle = t('Enable access for {{count}} selected users?', {
      count: targets.ids.length,
    })
    confirmationDescription = t('These users will regain access.')
  } else if (targets?.action === 'delete') {
    confirmationTitle = t('Delete {{count}} selected users?', {
      count: targets.ids.length,
    })
    confirmationDescription = t(
      'Permanently delete selected users and their data. This action cannot be undone.'
    )
  }

  return (
    <>
      <BulkActionsToolbar
        table={table}
        entityName={t('user')}
        selectedCount={selectedCount}
      >
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                className='size-8'
                aria-label={t('Disable selected users')}
                disabled={mutation.isPending}
                onClick={() => openConfirmation('disable')}
              />
            }
          >
            <PowerOff aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>{t('Disable selected users')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                className='size-8'
                aria-label={t('Enable selected users')}
                disabled={mutation.isPending}
                onClick={() => openConfirmation('enable')}
              />
            }
          >
            <Power aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>{t('Enable selected users')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='destructive'
                size='icon'
                className='size-8'
                aria-label={t('Delete selected users')}
                disabled={mutation.isPending}
                onClick={() => openConfirmation('delete')}
              />
            }
          >
            <Trash2 aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>{t('Delete selected users')}</TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>
      <ConfirmDialog
        open={targets !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setTargets(null)
        }}
        title={confirmationTitle}
        desc={confirmationDescription}
        confirmText={mutation.isPending ? t('Processing...') : t('Confirm')}
        destructive={targets?.action === 'delete'}
        isLoading={mutation.isPending}
        disabled={!targets?.ids.length}
        handleConfirm={() => {
          if (targets && !mutation.isPending) mutation.mutate(targets)
        }}
      />
    </>
  )
}
