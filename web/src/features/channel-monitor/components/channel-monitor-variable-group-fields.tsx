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
import { useState } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  useChannelMonitorVariableGroups,
  type ChannelMonitorVariableGroup,
} from '../api-variable-groups'
import { createChannelMonitorCustomRequestConfig } from '../lib/custom-upstream'
import type { UpstreamConfigFormValues } from '../lib/schema'
import { emptyVariableGroup } from '../lib/variable-group'
import { ChannelMonitorVariableGroupsDialog } from './channel-monitor-variable-groups-dialog'

type Props = {
  independent?: boolean
  form: UseFormReturn<UpstreamConfigFormValues>
  channelId: number
  channelName: string
  disabled: boolean
}

export function ChannelMonitorVariableGroupFields(props: Props) {
  const groups = useChannelMonitorVariableGroups()
  const groupId = useWatch({
    control: props.form.control,
    name: 'customConfig.variableGroupId',
  })
  const requestCount = useWatch({
    control: props.form.control,
    name: 'customConfig.variableRequests',
    compute: (requests) => requests.length,
  })
  const [managerOpen, setManagerOpen] = useState(false)
  const [initialGroup, setInitialGroup] =
    useState<ChannelMonitorVariableGroup>()
  const selected = groups.data?.find((group) => group.id === groupId)
  const options = [
    { value: '0', label: '不使用共享配置' },
    ...(groups.data ?? []).map((group) => ({
      value: String(group.id),
      label: group.name,
    })),
  ]
  if (groupId && !selected) {
    options.push({ value: String(groupId), label: `共享配置 #${groupId}` })
  }

  const selectGroup = (id: number) => {
    if (id === (props.form.getValues('customConfig.variableGroupId') || 0)) {
      return
    }
    props.form.setValue('customConfig.variableRequests', [], {
      shouldDirty: true,
    })
    props.form.setValue('customConfig.variableGroupId', id, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  return (
    <section
      aria-label='共享请求与变量引用'
      className='flex min-w-0 flex-col gap-3'
    >
      <FormField
        control={props.form.control}
        name='customConfig.variableGroupId'
        render={({ field }) => (
          <FormItem>
            <FormLabel>共享请求与变量</FormLabel>
            <div className='flex flex-col gap-2 sm:flex-row'>
              <Select
                items={options}
                value={String(field.value || 0)}
                onValueChange={(value) => {
                  if (value !== null) selectGroup(Number(value))
                }}
              >
                <FormControl>
                  <SelectTrigger
                    disabled={props.disabled || groups.isPending}
                    className='min-w-0 flex-1'
                  >
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectGroup>
                    {options.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                type='button'
                variant='outline'
                disabled={props.disabled}
                onClick={() => {
                  setInitialGroup(undefined)
                  setManagerOpen(true)
                }}
              >
                管理共享配置
              </Button>
            </div>
            <FormMessage />
          </FormItem>
        )}
      />
      <p className='text-muted-foreground text-sm'>
        {props.independent
          ? '可引用共享凭据；任务保留自己的指标查询、触发规则和调度。'
          : '配置一次即可供多个渠道共用；各渠道保留自己的倍率、余额接口和策略。'}
      </p>
      {groups.isError ? (
        <Alert variant='destructive'>
          <AlertDescription>
            共享配置加载失败
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void groups.refetch()}
            >
              重试
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {groupId && groups.isSuccess && !selected ? (
        <Alert variant='destructive'>
          <AlertDescription>
            引用的共享配置不存在，请重新选择。
          </AlertDescription>
        </Alert>
      ) : null}
      {selected ? (
        <p className='text-muted-foreground text-sm break-all'>
          共享地址：{selected.base_url} ·{' '}
          {selected.variable_requests
            .flatMap((request) =>
              request.variables.map((variable) => variable.name)
            )
            .join('、')}
        </p>
      ) : null}
      {!props.independent && !groupId && requestCount > 0 ? (
        <div className='flex flex-col items-start gap-2'>
          <p className='text-muted-foreground text-sm'>
            此渠道当前使用独立配置，可将请求和已保存的凭据转为共享配置。
          </p>
          <Button
            type='button'
            variant='outline'
            disabled={props.disabled}
            onClick={() => {
              setInitialGroup({
                ...emptyVariableGroup(),
                name: `${props.channelName}共享变量`.slice(0, 80),
                base_url: props.form.getValues('baseUrl'),
                source_channel_id: props.channelId,
                variable_requests:
                  createChannelMonitorCustomRequestConfig(
                    props.form.getValues('customConfig')
                  ).variable_requests ?? [],
              })
              setManagerOpen(true)
            }}
          >
            另存为共享配置
          </Button>
        </div>
      ) : null}
      {managerOpen ? (
        <ChannelMonitorVariableGroupsDialog
          initialGroup={initialGroup}
          onOpenChange={setManagerOpen}
          onSelect={(group) => selectGroup(group.id)}
        />
      ) : null}
    </section>
  )
}
