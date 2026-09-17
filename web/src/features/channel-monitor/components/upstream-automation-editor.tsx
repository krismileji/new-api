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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Spinner } from '@/components/ui/spinner'

import {
  automationsQueryKey,
  fetchUpstreamAutomationVariables,
  resetUpstreamAutomationAttempts,
  saveUpstreamAutomation,
  testUpstreamAutomation,
  type UpstreamAutomation,
} from '../api-automations'
import {
  automationMetadataSchema,
  upstreamAutomationFormValues,
  upstreamAutomationPayload,
  type AutomationMetadata,
} from '../lib/automation'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  createUpstreamConfigSchema,
  type UpstreamConfigFormValues,
} from '../lib/schema'
import type { ChannelMonitorItem } from '../types'
import { ChannelMonitorCustomActionFields } from './channel-monitor-custom-action-fields'
import { ChannelMonitorCustomUpstreamFields } from './channel-monitor-custom-upstream-fields'
import { ChannelMonitorCustomVariableFields } from './channel-monitor-custom-variable-fields'
import { ChannelMonitorVariableGroupFields } from './channel-monitor-variable-group-fields'
import { UpstreamAutomationMetadata } from './upstream-automation-metadata'

export function UpstreamAutomationEditor(props: {
  task: UpstreamAutomation
  channels: ChannelMonitorItem[]
  onSaved: () => void
  onCancel: () => void
}) {
  const queryClient = useQueryClient()
  const [task, setTask] = useState(props.task)
  const metadata = useForm<AutomationMetadata>({
    defaultValues: task,
    resolver: zodResolver(
      automationMetadataSchema
    ) as Resolver<AutomationMetadata>,
  })
  const form = useForm<UpstreamConfigFormValues>({
    defaultValues: upstreamAutomationFormValues(task),
    resolver: zodResolver(
      createUpstreamConfigSchema(null)
    ) as Resolver<UpstreamConfigFormValues>,
  })
  const groupId = useWatch({
    control: form.control,
    name: 'customConfig.variableGroupId',
  })
  const save = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: saveUpstreamAutomation,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('上游自动任务已保存')
      props.onSaved()
    },
  })
  const test = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: testUpstreamAutomation,
    onError: handleChannelMonitorMutationError,
  })
  const variables = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: (request: { task: UpstreamAutomation; requestId: string }) =>
      fetchUpstreamAutomationVariables(request.task, request.requestId),
    onError: handleChannelMonitorMutationError,
  })
  const reset = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: resetUpstreamAutomationAttempts,
    onError: handleChannelMonitorMutationError,
  })
  const pending =
    save.isPending || test.isPending || variables.isPending || reset.isPending

  const payload = (values: UpstreamConfigFormValues) =>
    upstreamAutomationPayload({ ...task, ...metadata.getValues() }, values)
  const fetchVariables = async (requestId: string) => {
    if (!(await metadata.trigger())) return
    const submitted = payload(form.getValues())
    const response = await variables.mutateAsync({ task: submitted, requestId })
    const current = payload(form.getValues())
    const index =
      current.custom_config.variable_requests?.findIndex(
        (request) => request.id === requestId
      ) ?? -1
    if (
      index < 0 ||
      JSON.stringify(current.custom_config.variable_requests?.[index]) !==
        JSON.stringify(
          submitted.custom_config.variable_requests?.find(
            (request) => request.id === requestId
          )
        ) ||
      current.base_url !== submitted.base_url ||
      current.proxy !== submitted.proxy
    ) {
      toast.warning('配置已修改，请重新获取变量')
      return
    }
    form
      .getValues(`customConfig.variableRequests.${index}.variables`)
      .forEach((variable, variableIndex) => {
        const value = response.find((item) => item.name === variable.name)
        if (value) {
          form.setValue(
            `customConfig.variableRequests.${index}.variables.${variableIndex}.value`,
            value.value ?? '',
            { shouldDirty: true }
          )
          form.setValue(
            `customConfig.variableRequests.${index}.variables.${variableIndex}.hasValue`,
            true,
            { shouldDirty: true }
          )
        }
      })
    toast.success('变量已回填，保存后生效')
  }

  return (
    <form
      className='flex min-h-0 flex-1 flex-col gap-4'
      onSubmit={async (event) => {
        event.preventDefault()
        if (!(await metadata.trigger())) return
        await form.handleSubmit((values) => {
          if (values.customConfig.actions.length === 0) {
            form.setError('customConfig.actions', {
              message: '请至少添加一条触发规则',
            })
            return
          }
          save.mutate(payload(values))
        })(event)
      }}
    >
      <fieldset
        disabled={pending}
        className='min-h-0 flex-1 space-y-6 overflow-y-auto px-1 pb-4'
      >
        <Form {...metadata}>
          <UpstreamAutomationMetadata
            form={metadata}
            channels={props.channels}
          />
        </Form>
        <Separator />
        <Form {...form}>
          <FormField
            control={form.control}
            name='baseUrl'
            render={({ field }) => (
              <FormItem>
                <FormLabel>上游基础地址</FormLabel>
                <FormControl>
                  <Input {...field} placeholder='https://upstream.example' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <ChannelMonitorVariableGroupFields
            form={form}
            channelId={0}
            channelName={task.name}
            disabled={pending}
            independent
          />
          {!groupId ? (
            <ChannelMonitorCustomVariableFields
              form={form}
              pending={pending}
              fetchingRequestId={
                variables.isPending ? variables.variables.requestId : undefined
              }
              onFetch={(id) => {
                void fetchVariables(id).catch(() => undefined)
              }}
            />
          ) : null}
          <ChannelMonitorCustomUpstreamFields form={form} independent />
          <Separator />
          <ChannelMonitorCustomActionFields
            form={form}
            disabled={pending}
            independent
            states={task.state.actions}
            savedActions={task.custom_config.actions}
            onReset={async (actionId, state) => {
              await reset.mutateAsync({ task, actionId, state })
              setTask({
                ...task,
                state: {
                  ...task.state,
                  actions: {
                    ...task.state.actions,
                    [actionId]: { ...state, attempts: 0 },
                  },
                },
              })
              void queryClient.invalidateQueries({
                queryKey: automationsQueryKey,
              })
            }}
          />
          {form.formState.errors.customConfig?.actions?.message ? (
            <p role='alert' className='text-destructive text-sm'>
              {form.formState.errors.customConfig.actions.message}
            </p>
          ) : null}
        </Form>
        {test.data ? (
          <Alert>
            <AlertDescription>
              测试获取成功：倍率 {test.data.ratio}，余额{' '}
              {test.data.balance.amount ?? '未返回'}。未执行触发接口。
            </AlertDescription>
          </Alert>
        ) : null}
      </fieldset>
      <div className='flex flex-wrap justify-end gap-2 border-t pt-4'>
        <Button
          type='button'
          variant='outline'
          disabled={pending}
          onClick={props.onCancel}
        >
          返回任务列表
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={pending}
          onClick={async () => {
            if (await metadata.trigger()) {
              await form.handleSubmit((values) =>
                test.mutate(payload(values))
              )()
            }
          }}
        >
          测试获取指标
        </Button>
        <Button type='submit' disabled={pending}>
          {pending ? <Spinner /> : null}保存任务
        </Button>
      </div>
    </form>
  )
}
