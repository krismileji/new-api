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
import { Spinner } from '@/components/ui/spinner'

import {
  automationsQueryKey,
  fetchUpstreamAutomationVariables,
  resetUpstreamAutomationAttempts,
  saveUpstreamAutomation,
  testUpstreamAutomation,
  type UpstreamAutomation,
} from '../api-automations'
import { useUpstreamAccounts } from '../api-upstream-accounts'
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
import {
  UpstreamEditorLayout,
  UpstreamEditorSection,
  upstreamEditorFooterClassName,
} from './upstream-editor-layout'
import { UpstreamEditorVariableReference } from './upstream-editor-variable-reference'

export function UpstreamAutomationEditor(props: {
  task: UpstreamAutomation
  channels: ChannelMonitorItem[]
  onSaved: () => void
  onCancel: () => void
}) {
  const queryClient = useQueryClient()
  const accounts = useUpstreamAccounts()
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
  const accountId = useWatch({ control: metadata.control, name: 'account_id' })
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
      className='flex min-h-0 flex-1 flex-col'
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
        className='flex min-h-0 min-w-0 flex-1 flex-col'
      >
        <UpstreamEditorLayout
          sections={[
            {
              id: 'schedule',
              label: '任务与调度',
              detail: '配置来源、运行时间与渠道',
            },
            ...(!accountId
              ? [
                  {
                    id: 'variables',
                    label: '请求与变量',
                    detail: '基础地址与认证变量',
                  },
                  {
                    id: 'metrics',
                    label: '指标来源',
                    detail: '余额与倍率查询',
                  },
                ]
              : []),
            {
              id: 'actions',
              label: '触发规则',
              detail: '触发条件、执行接口与限额',
            },
          ]}
          reference={
            accountId ? (
              <div className='flex flex-col gap-2 text-sm'>
                <h3 className='font-medium'>继承账户配置</h3>
                <p className='break-all'>
                  {accounts.data?.find((account) => account.id === accountId)
                    ?.name ?? `账户 #${accountId}`}
                </p>
                <p className='text-muted-foreground text-xs'>
                  余额查询与认证使用账户配置。倍率规则使用所选渠道。
                </p>
              </div>
            ) : (
              <UpstreamEditorVariableReference form={form} />
            )
          }
        >
          <UpstreamEditorSection id='schedule' title='任务与调度'>
            <Form {...metadata}>
              <UpstreamAutomationMetadata
                form={metadata}
                channels={props.channels}
                accounts={accounts.data}
                onAccountChange={(account) => {
                  if (!account) return
                  metadata.setValue('channel_ids', account.channel_ids)
                  metadata.setValue('proxy', account.proxy)
                  const values = upstreamAutomationFormValues({
                    ...task,
                    base_url: account.upstream.base_url,
                    custom_config:
                      account.upstream.custom_config ?? task.custom_config,
                  })
                  const actions = form.getValues('customConfig.actions')
                  form.setValue('baseUrl', values.baseUrl)
                  form.setValue('customConfig', {
                    ...values.customConfig,
                    actions,
                  })
                }}
              />
            </Form>
          </UpstreamEditorSection>
          <Form {...form}>
            {!accountId ? (
              <>
                <UpstreamEditorSection id='variables' title='请求与变量'>
                  <FormField
                    control={form.control}
                    name='baseUrl'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>上游基础地址</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            placeholder='https://upstream.example'
                          />
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
                      workspace
                      form={form}
                      pending={pending}
                      fetchingRequestId={
                        variables.isPending
                          ? variables.variables.requestId
                          : undefined
                      }
                      onFetch={(id) => {
                        void fetchVariables(id).catch(() => undefined)
                      }}
                    />
                  ) : null}
                </UpstreamEditorSection>
                <UpstreamEditorSection id='metrics' title='指标来源'>
                  <ChannelMonitorCustomUpstreamFields
                    workspace
                    form={form}
                    independent
                    allowAccountBalance
                  />
                </UpstreamEditorSection>
              </>
            ) : (
              <Alert>
                <AlertDescription>
                  余额查询与认证使用账户配置。以下规则由账户统一执行，倍率规则使用所选渠道。
                </AlertDescription>
              </Alert>
            )}
            <UpstreamEditorSection
              id='actions'
              title='触发规则'
              description='先设置何时触发，再配置执行接口；多条规则可直接定位。'
            >
              <ChannelMonitorCustomActionFields
                workspace
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
            </UpstreamEditorSection>
          </Form>
          {test.data ? (
            <Alert>
              <AlertDescription>
                测试获取成功：倍率 {test.data.ratio}，余额{' '}
                {test.data.balance.amount ?? '未返回'}。未执行触发接口。
              </AlertDescription>
            </Alert>
          ) : null}
        </UpstreamEditorLayout>
      </fieldset>
      <div className={upstreamEditorFooterClassName}>
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
