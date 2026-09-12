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
import { Add01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import {
  useFieldArray,
  useFormState,
  useWatch,
  type UseFormReturn,
} from 'react-hook-form'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'

import { createChannelMonitorVariableRequest } from '../lib/custom-upstream'
import type { UpstreamConfigFormValues } from '../lib/schema'
import { ChannelMonitorCustomVariableRequestCard } from './channel-monitor-custom-variable-request-card'

type ChannelMonitorCustomVariableFieldsProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  pending: boolean
  fetchingRequestId?: string
  onFetch: (requestId: string) => void
}

export function ChannelMonitorCustomVariableFields(
  props: ChannelMonitorCustomVariableFieldsProps
) {
  const requests = useFieldArray({
    control: props.form.control,
    name: 'customConfig.variableRequests',
    keyName: 'fieldKey',
  })
  const values = useWatch({
    control: props.form.control,
    name: 'customConfig.variableRequests',
  })
  const formState = useFormState({
    control: props.form.control,
    name: 'customConfig.variableRequests',
  })
  const [openRequestId, setOpenRequestId] = useState<string | undefined>(
    () => props.form.getValues('customConfig.variableRequests')[0]?.id
  )
  const variableCount = values.reduce(
    (count, request) => count + request.variables.length,
    0
  )
  const rootError =
    formState.errors.customConfig?.variableRequests?.root?.message ||
    formState.errors.customConfig?.variableRequests?.message

  return (
    <section
      aria-label='独立请求与变量'
      className='flex min-w-0 flex-col gap-3'
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='flex min-w-0 flex-col gap-1'>
          <span className='text-sm font-medium'>独立请求与变量</span>
          <span className='text-muted-foreground text-xs'>
            {values.length} 个请求 · {variableCount} 个变量
          </span>
          <p className='text-muted-foreground text-sm'>
            每个请求独立设置刷新策略，可产出多个变量供倍率和余额共用。
          </p>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.pending || values.length >= 8 || variableCount >= 32}
          onClick={() => {
            const id = crypto.randomUUID()
            requests.append(
              createChannelMonitorVariableRequest(
                id,
                `请求 ${values.length + 1}`
              )
            )
            setOpenRequestId(id)
          }}
        >
          <HugeiconsIcon icon={Add01Icon} aria-hidden='true' />
          添加独立请求
        </Button>
      </div>
      {rootError ? (
        <p role='alert' className='text-destructive text-sm'>
          {rootError}
        </p>
      ) : null}
      {requests.fields.length === 0 ? (
        <Empty className='border py-5'>
          <EmptyHeader>
            <EmptyTitle>尚未配置独立请求</EmptyTitle>
            <EmptyDescription>
              需要自动获取 Token 等参数时，添加请求并配置变量映射。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        requests.fields.map((request, index) => (
          <ChannelMonitorCustomVariableRequestCard
            key={request.fieldKey}
            form={props.form}
            index={index}
            open={request.id === openRequestId}
            pending={props.pending}
            fetching={props.fetchingRequestId === request.id}
            canAddVariable={variableCount < 32}
            onOpenChange={(open) =>
              setOpenRequestId(open ? request.id : undefined)
            }
            onFetch={() => props.onFetch(request.id)}
            onRemove={() => {
              requests.remove(index)
              if (openRequestId === request.id) {
                setOpenRequestId(values[index + 1]?.id || values[index - 1]?.id)
              }
            }}
          />
        ))
      )}
    </section>
  )
}
