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
import { useMutation } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { toast } from 'sonner'

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
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'

import {
  fetchChannelMonitorVariableGroupDraft,
  saveChannelMonitorVariableGroup,
  variableGroupsQueryKey,
  type ChannelMonitorVariableGroup,
} from '../api-variable-groups'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  createUpstreamConfigSchema,
  type UpstreamConfigFormValues,
} from '../lib/schema'
import {
  variableGroupFormValues,
  variableGroupPayload,
} from '../lib/variable-group'
import { ChannelMonitorCustomVariableFields } from './channel-monitor-custom-variable-fields'
import {
  UpstreamEditorLayout,
  UpstreamEditorSection,
  upstreamEditorFooterClassName,
} from './upstream-editor-layout'
import { UpstreamEditorVariableReference } from './upstream-editor-variable-reference'

type Props = {
  group: ChannelMonitorVariableGroup
  onSaved: (group: ChannelMonitorVariableGroup) => void
  onCancel: () => void
}

export function ChannelMonitorVariableGroupEditor(props: Props) {
  const id = useId()
  const [group, setGroup] = useState(props.group)
  const form = useForm<UpstreamConfigFormValues>({
    defaultValues: variableGroupFormValues(props.group),
    resolver: zodResolver(
      createUpstreamConfigSchema(null)
    ) as Resolver<UpstreamConfigFormValues>,
  })
  const save = useMutation({
    mutationKey: variableGroupsQueryKey,
    mutationFn: saveChannelMonitorVariableGroup,
    onError: handleChannelMonitorMutationError,
    onSuccess: (saved) => {
      toast.success('共享配置已保存，引用它的渠道会共用最新变量')
      props.onSaved(saved)
    },
  })
  const fetch = useMutation({
    mutationKey: variableGroupsQueryKey,
    mutationFn: fetchChannelMonitorVariableGroupDraft,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response, submitted) => {
      const current = variableGroupPayload(group, form.getValues())
      const index = current.variable_requests.findIndex(
        (request) => request.id === submitted.requestId
      )
      const request = current.variable_requests[index]
      if (
        !request ||
        current.base_url !== submitted.group.base_url ||
        current.proxy !== submitted.group.proxy ||
        JSON.stringify(request) !==
          JSON.stringify(
            submitted.group.variable_requests.find(
              (item) => item.id === submitted.requestId
            )
          )
      ) {
        toast.warning('独立请求配置已修改，请重新获取变量')
        return
      }
      const values = new Map(
        response.variables.map((variable) => [variable.name, variable.value])
      )
      if (request.variables.some((variable) => !values.has(variable.name))) {
        toast.error('返回的变量映射不完整，请重新获取')
        return
      }
      request.variables.forEach((variable, variableIndex) => {
        form.setValue(
          `customConfig.variableRequests.${index}.variables.${variableIndex}.value`,
          values.get(variable.name) ?? '',
          { shouldDirty: true, shouldValidate: true }
        )
        form.setValue(
          `customConfig.variableRequests.${index}.variables.${variableIndex}.hasValue`,
          true,
          { shouldDirty: true }
        )
      })
      toast.success('变量已回填，保存共享配置后生效')
    },
  })
  const pending = save.isPending || fetch.isPending

  return (
    <Form {...form}>
      <form
        className='flex min-h-0 flex-1 flex-col'
        onSubmit={(event) => {
          event.stopPropagation()
          void form.handleSubmit((values) => {
            if (values.customConfig.variableRequests.length === 0) {
              form.setError('customConfig.variableRequests', {
                message: '共享配置至少需要一个独立请求',
              })
              return
            }
            save.mutate(variableGroupPayload(group, values))
          })(event)
        }}
      >
        <fieldset
          disabled={save.isPending}
          className='flex min-h-0 min-w-0 flex-1 flex-col'
        >
          <UpstreamEditorLayout
            sections={[
              {
                id: 'connection',
                label: '基本设置',
                detail: '名称、地址与连接方式',
              },
              {
                id: 'variables',
                label: '请求与变量',
                detail: '请求参数、变量映射与刷新',
              },
            ]}
            reference={<UpstreamEditorVariableReference form={form} />}
          >
            <UpstreamEditorSection
              id='connection'
              title='基本设置'
              description='请求、变量值和刷新策略由所有引用渠道共用。单个请求未填写基础地址时，使用这里的共享地址。'
            >
              <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor={`${id}-name`}>共享配置名称</Label>
                  <Input
                    id={`${id}-name`}
                    required
                    maxLength={80}
                    value={group.name}
                    onChange={(event) =>
                      setGroup({ ...group, name: event.target.value })
                    }
                    placeholder='例如：上游 A 登录凭据'
                  />
                </div>
                <FormField
                  control={form.control}
                  name='baseUrl'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>共享请求基础地址</FormLabel>
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
                <div className='flex flex-col gap-2'>
                  <Label htmlFor={`${id}-proxy`}>请求代理（可选）</Label>
                  <Input
                    id={`${id}-proxy`}
                    value={group.proxy}
                    onChange={(event) =>
                      setGroup({ ...group, proxy: event.target.value })
                    }
                    placeholder='留空直连'
                  />
                </div>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor={`${id}-timeout`}>请求超时（秒）</Label>
                  <Input
                    id={`${id}-timeout`}
                    type='number'
                    min={1}
                    max={120}
                    required
                    value={group.request_timeout}
                    onChange={(event) =>
                      setGroup({
                        ...group,
                        request_timeout: event.target.valueAsNumber,
                      })
                    }
                  />
                </div>
              </div>
            </UpstreamEditorSection>
            <UpstreamEditorSection
              id='variables'
              title='请求与变量'
              description='同一共享配置内的变量名不能重复（包含不同请求）；不同共享配置可以使用相同变量名，渠道只读取所选配置的变量。'
            >
              <ChannelMonitorCustomVariableFields
                workspace
                form={form}
                pending={pending}
                fetchingRequestId={
                  fetch.isPending ? fetch.variables.requestId : undefined
                }
                onFetch={async (requestId) => {
                  const index = form
                    .getValues('customConfig.variableRequests')
                    .findIndex((request) => request.id === requestId)
                  if (!group.name.trim()) {
                    toast.error('请先填写共享配置名称')
                    return
                  }
                  if (
                    index < 0 ||
                    !(await form.trigger([
                      'baseUrl',
                      `customConfig.variableRequests.${index}`,
                    ]))
                  ) {
                    return
                  }
                  fetch.mutate({
                    group: variableGroupPayload(group, form.getValues()),
                    requestId,
                  })
                }}
              />
            </UpstreamEditorSection>
          </UpstreamEditorLayout>
        </fieldset>
        <div className={upstreamEditorFooterClassName}>
          <Button
            type='button'
            variant='outline'
            disabled={pending}
            onClick={props.onCancel}
          >
            返回列表
          </Button>
          <Button type='submit' disabled={pending}>
            {save.isPending ? <Spinner /> : null}保存共享配置
          </Button>
        </div>
      </form>
    </Form>
  )
}
