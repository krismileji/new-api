import { zodResolver } from '@hookform/resolvers/zod'
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
import { useState } from 'react'
import { useForm } from 'react-hook-form'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { api } from '@/lib/api'

import { automationsQueryKey, useUpstreamAutomations } from '../api-automations'
import type { UpstreamAccount } from '../api-upstream-accounts'
import { handleChannelMonitorMutationError } from '../lib/error'
import { upstreamAccountMergeSchema } from '../lib/upstream-account'
import type { ChannelMonitorApiResponse, ChannelMonitorItem } from '../types'

type MergePreview = {
  target_id: string
  task_names: string[]
  rule_names: string[]
  needs_confirmation: boolean
}

export function UpstreamAccountTaskMerge(props: {
  account: UpstreamAccount
  channels: ChannelMonitorItem[]
  onClose: () => void
}) {
  const query = useUpstreamAutomations()
  const client = useQueryClient()
  const form = useForm({
    resolver: zodResolver(upstreamAccountMergeSchema),
    defaultValues: { target: '', ratioChannel: 0 },
  })
  const target = form.watch('target')
  const ratioChannel = form.watch('ratioChannel')
  const [preview, setPreview] = useState<{
    signature: string
    data: MergePreview
  } | null>(null)
  const tasks = (query.data?.tasks ?? []).filter(
    (task) =>
      !task.merged_into &&
      task.custom_config.balance.source !== 'account' &&
      (task.account_id === props.account.id ||
        (!task.account_id &&
          task.channel_ids.length > 0 &&
          task.channel_ids.every((id) =>
            props.account.channel_ids.includes(id)
          )))
  )
  const input = {
    account_id: props.account.id,
    account_revision: props.account.revision,
    target_id: target,
    ratio_channel_id: ratioChannel,
    task_revisions: Object.fromEntries(
      tasks.map((task) => [task.id, task.revision])
    ),
  }
  const signature = JSON.stringify(input)
  const merge = useMutation({
    mutationFn: async (request: typeof input & { preview: boolean }) => {
      const response = await api.post<ChannelMonitorApiResponse<MergePreview>>(
        '/api/channel_monitor/automations/merge',
        request,
        { skipBusinessError: true, skipErrorHandler: true }
      )
      if (!response.data.success) {
        throw new Error(response.data.message || '任务合并失败')
      }
      return response.data.data
    },
    onError: handleChannelMonitorMutationError,
    onSuccess: (data, request) => {
      if (request.preview) {
        const { preview: _preview, ...submitted } = request
        setPreview({ signature: JSON.stringify(submitted), data })
        return
      }
      void client.invalidateQueries({ queryKey: automationsQueryKey })
      props.onClose()
    },
  })
  const needsRatio = tasks.some((task) =>
    task.custom_config.actions?.some((action) => action.metric === 'ratio')
  )
  return (
    <div className='flex flex-col gap-3 rounded-md border p-4'>
      <p className='font-medium'>合并账户关联任务</p>
      <p className='text-muted-foreground text-sm'>
        以下任务将合并，完全相同的规则只保留一份并合计执行次数。目标任务保留调度设置，其余任务停用并保留历史。
      </p>
      {query.isError ? (
        <Alert variant='destructive'>
          <AlertDescription>
            任务加载失败。
            <Button variant='link' onClick={() => void query.refetch()}>
              重试
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor={`merge-target-${props.account.id}`}>
            保留的目标任务
          </FieldLabel>
          <NativeSelect
            id={`merge-target-${props.account.id}`}
            value={target}
            disabled={merge.isPending}
            onChange={(event) => form.setValue('target', event.target.value)}
          >
            <NativeSelectOption value=''>选择目标任务</NativeSelectOption>
            {tasks.map((task) => (
              <NativeSelectOption key={task.id} value={task.id}>
                {task.name} · 每 {task.interval_minutes} 分钟 ·{' '}
                {task.enabled ? '已启用' : '已暂停'}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        {needsRatio ? (
          <Field>
            <FieldLabel htmlFor={`merge-ratio-${props.account.id}`}>
              倍率规则来源渠道
            </FieldLabel>
            <NativeSelect
              id={`merge-ratio-${props.account.id}`}
              value={ratioChannel}
              disabled={merge.isPending}
              onChange={(event) =>
                form.setValue('ratioChannel', Number(event.target.value))
              }
            >
              <NativeSelectOption value={0}>
                选择倍率来源渠道
              </NativeSelectOption>
              {props.channels
                .filter((channel) =>
                  props.account.channel_ids.includes(channel.id)
                )
                .map((channel) => (
                  <NativeSelectOption key={channel.id} value={channel.id}>
                    {channel.name}
                  </NativeSelectOption>
                ))}
            </NativeSelect>
          </Field>
        ) : null}
      </FieldGroup>
      {preview?.signature === signature ? (
        <Alert>
          <AlertDescription>
            <p>合并任务：{preview.data.task_names.join('、')}</p>
            <p>保留规则：{preview.data.rule_names.join('、')}</p>
            <p>原有冷却、当日次数和首次触发状态全部保留。</p>
            {preview.data.needs_confirmation ? (
              <p>存在未确认执行，合并后仍需核对上游后解除暂停。</p>
            ) : null}
          </AlertDescription>
        </Alert>
      ) : null}
      {!query.isPending && tasks.length === 0 ? (
        <p>没有可合并的关联任务。可在「上游自动任务」中选择此账户新建任务。</p>
      ) : null}
      <div className='flex flex-wrap justify-end gap-2'>
        <Button
          variant='outline'
          disabled={merge.isPending}
          onClick={props.onClose}
        >
          取消
        </Button>
        <Button
          variant='outline'
          disabled={merge.isPending || !target || (needsRatio && !ratioChannel)}
          onClick={() =>
            void form.handleSubmit(() =>
              merge.mutate({ ...input, preview: true })
            )()
          }
        >
          预览任务合并
        </Button>
        <Button
          disabled={merge.isPending || preview?.signature !== signature}
          onClick={() =>
            void form.handleSubmit(() =>
              merge.mutate({ ...input, preview: false })
            )()
          }
        >
          确认合并任务
        </Button>
      </div>
    </div>
  )
}
