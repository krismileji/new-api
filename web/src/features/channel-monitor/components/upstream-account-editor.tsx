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
import { useForm, useWatch } from 'react-hook-form'

import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel, FieldSet, FieldLegend } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'

import {
  previewUpstreamAccount,
  saveUpstreamAccount,
  type UpstreamAccount,
  type UpstreamAccountDifference,
  type UpstreamAccountInput,
} from '../api-upstream-accounts'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  upstreamAccountSchema,
  type UpstreamAccountFormValues,
} from '../lib/upstream-account'
import type { ChannelMonitorItem } from '../types'
import {
  UpstreamEditorLayout,
  UpstreamEditorSection,
  upstreamEditorFooterClassName,
} from './upstream-editor-layout'

export function UpstreamAccountEditor(props: {
  account?: UpstreamAccount
  channels: ChannelMonitorItem[]
  onClose: () => void
  onSaved: () => void
}) {
  const id = useId()
  const form = useForm<UpstreamAccountFormValues>({
    resolver: zodResolver(upstreamAccountSchema),
    defaultValues: {
      id: props.account?.id ?? 0,
      revision: props.account?.revision ?? 0,
      name: props.account?.name ?? '',
      source_channel_id: 0,
      channel_ids: props.account?.channel_ids ?? [],
      channel_revisions: props.account?.channel_revisions ?? {},
      refresh_interval_minutes: props.account?.refresh_interval_minutes ?? 5,
      proxy: props.account?.proxy,
      balance_key: '',
    },
  })
  const input = useWatch({ control: form.control }) as UpstreamAccountFormValues
  const [search, setSearch] = useState('')
  const [selectedOnly, setSelectedOnly] = useState(false)
  const [preview, setPreview] = useState<{
    input: string
    rows: UpstreamAccountDifference[]
  } | null>(null)
  const inspect = useMutation({
    mutationFn: previewUpstreamAccount,
    onError: handleChannelMonitorMutationError,
    onSuccess: (rows, submitted) =>
      setPreview({ input: JSON.stringify(submitted), rows }),
  })
  const save = useMutation({
    mutationFn: saveUpstreamAccount,
    onError: handleChannelMonitorMutationError,
    onSuccess: props.onSaved,
  })
  const busy = inspect.isPending || save.isPending
  const sourceChannels = props.channels.filter(
    (channel) => channel.upstream && !channel.upstream.upstream_account_id
  )
  const sourceType =
    props.account?.upstream.type ??
    sourceChannels.find((channel) => channel.id === input.source_channel_id)
      ?.upstream?.type
  const available = props.channels.filter(
    (channel) =>
      sourceType &&
      (props.account?.channel_ids.includes(channel.id) ||
        !channel.upstream ||
        (channel.upstream.type === sourceType &&
          (!channel.upstream.upstream_account_id ||
            channel.upstream.upstream_account_id === props.account?.id)))
  )
  const previewCurrent =
    preview?.input === JSON.stringify({ ...input, name: input.name.trim() })
  const errors = Object.entries(form.formState.errors)
  const keyword = search.trim().toLowerCase()
  const visibleChannels = available.filter(
    (channel) =>
      `${channel.id} ${channel.name}`.toLowerCase().includes(keyword) &&
      (!selectedOnly || input.channel_ids.includes(channel.id))
  )
  const confirm = form.handleSubmit((values) => {
    if (!previewCurrent || !preview) return
    const submitted: UpstreamAccountInput = {
      ...values,
      channel_revisions: {
        ...values.channel_revisions,
        ...Object.fromEntries(
          preview.rows.map((row) => [row.channel_id, row.revision])
        ),
      },
    }
    save.mutate(submitted)
  })
  return (
    <form
      className='flex min-h-0 flex-1 flex-col'
      onSubmit={form.handleSubmit((values) => inspect.mutate(values))}
    >
      <UpstreamEditorLayout
        sections={[
          { id: 'account', label: '账户设置', detail: '账户名称、刷新与认证' },
          {
            id: 'channels',
            label: '关联渠道',
            detail: `已选择 ${input.channel_ids.length} 个渠道`,
          },
          { id: 'preview', label: '配置差异', detail: '确认本次关联影响' },
        ]}
      >
        <div className='grid min-w-0 items-start gap-8 xl:grid-cols-[minmax(16rem,1fr)_minmax(0,2fr)]'>
          <UpstreamEditorSection id='account' title='账户设置'>
            <Field>
              <FieldLabel htmlFor={`${id}-name`}>账户名称</FieldLabel>
              <Input
                id={`${id}-name`}
                {...form.register('name')}
                maxLength={80}
                disabled={busy}
                aria-invalid={Boolean(form.formState.errors.name)}
              />
            </Field>
            {!props.account ? (
              <Field>
                <FieldLabel htmlFor={`${id}-source`}>配置来源渠道</FieldLabel>
                <NativeSelect
                  id={`${id}-source`}
                  value={input.source_channel_id}
                  disabled={busy}
                  onChange={(event) => {
                    const selected = Number(event.target.value)
                    form.setValue('source_channel_id', selected)
                    form.setValue('channel_ids', selected ? [selected] : [])
                  }}
                >
                  <NativeSelectOption value={0}>
                    选择已配置的渠道
                  </NativeSelectOption>
                  {sourceChannels.map((channel) => (
                    <NativeSelectOption key={channel.id} value={channel.id}>
                      {channel.name} · #{channel.id}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
            ) : null}
            <Field>
              <FieldLabel htmlFor={`${id}-interval`}>
                余额自动刷新间隔（分钟）
              </FieldLabel>
              <Input
                id={`${id}-interval`}
                type='number'
                min={0}
                max={10080}
                {...form.register('refresh_interval_minutes', {
                  valueAsNumber: true,
                })}
                disabled={busy}
              />
              <p className='text-muted-foreground text-sm'>
                0 表示关闭定时刷新。手动查询和自动任务仍可按需读取余额。
              </p>
            </Field>
            {props.account ? (
              <Field>
                <FieldLabel htmlFor={`${id}-proxy`}>账户请求代理</FieldLabel>
                <Input
                  id={`${id}-proxy`}
                  {...form.register('proxy')}
                  placeholder='留空为直接连接'
                  disabled={busy}
                />
              </Field>
            ) : null}
            {props.account?.upstream.auth_type === 'api_key' ? (
              <Field>
                <FieldLabel htmlFor={`${id}-key`}>
                  账户余额查询 API Key
                </FieldLabel>
                <PasswordInput
                  id={`${id}-key`}
                  {...form.register('balance_key')}
                  placeholder={
                    props.account.has_balance_key
                      ? '留空保留已保存的 Key'
                      : '请输入余额查询 Key'
                  }
                  disabled={busy}
                />
              </Field>
            ) : null}
          </UpstreamEditorSection>
          <div className='flex min-w-0 flex-col gap-6'>
            <UpstreamEditorSection
              id='channels'
              title='关联渠道'
              description='共用账户配置，各渠道的倍率保持独立。'
            >
              <div className='flex flex-wrap items-center gap-2'>
                <Input
                  type='search'
                  aria-label='筛选关联渠道'
                  placeholder='按名称或 ID 搜索渠道'
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  className='min-w-0 flex-1 basis-48'
                />
                <Button
                  type='button'
                  variant={selectedOnly ? 'secondary' : 'outline'}
                  aria-pressed={selectedOnly}
                  onClick={() => setSelectedOnly(!selectedOnly)}
                >
                  仅看已选（{input.channel_ids.length}）
                </Button>
              </div>
              <FieldSet>
                <FieldLegend variant='label'>关联渠道</FieldLegend>
                <div className='grid max-h-[50dvh] gap-3 overflow-y-auto rounded-md border p-3 2xl:grid-cols-2'>
                  {available.length === 0 ? (
                    <p className='text-muted-foreground text-sm'>
                      请选择配置来源，或先保存渠道的上游配置。
                    </p>
                  ) : (
                    visibleChannels.map((channel) => (
                      <Field key={channel.id} orientation='horizontal'>
                        <Checkbox
                          id={`${id}-channel-${channel.id}`}
                          disabled={
                            busy ||
                            channel.upstream?.custom_config?.balance.source ===
                              'account' ||
                            (!props.account &&
                              input.source_channel_id === channel.id)
                          }
                          checked={input.channel_ids.includes(channel.id)}
                          onCheckedChange={(checked) =>
                            form.setValue(
                              'channel_ids',
                              checked
                                ? [...input.channel_ids, channel.id]
                                : input.channel_ids.filter(
                                    (value) => value !== channel.id
                                  )
                            )
                          }
                        />
                        <FieldLabel
                          htmlFor={`${id}-channel-${channel.id}`}
                          className='min-w-0 break-all'
                        >
                          {channel.name} · 倍率 {channel.ratio ?? '未配置'}
                          {channel.upstream?.custom_config?.balance.source ===
                          'account'
                            ? ' · 仅关联余额，请在渠道上游配置中切换来源'
                            : null}
                        </FieldLabel>
                      </Field>
                    ))
                  )}
                  {available.length > 0 && visibleChannels.length === 0 ? (
                    <p className='text-muted-foreground text-sm'>
                      没有匹配的渠道，请调整筛选条件。
                    </p>
                  ) : null}
                </div>
              </FieldSet>
            </UpstreamEditorSection>
            <UpstreamEditorSection id='preview' title='配置差异'>
              {errors.length ? (
                <Alert variant='destructive'>
                  <AlertDescription>
                    {errors.map(([field, error]) => (
                      <p key={field}>
                        {typeof error.message === 'string'
                          ? error.message
                          : '请检查输入参数'}
                      </p>
                    ))}
                  </AlertDescription>
                </Alert>
              ) : null}
              {previewCurrent && preview ? (
                <Alert>
                  <AlertDescription>
                    <p>保存后以下渠道使用账户配置，各渠道倍率保持独立：</p>
                    {preview.rows.map((row) => (
                      <p key={row.channel_id}>
                        {props.channels.find(
                          (channel) => channel.id === row.channel_id
                        )?.name ?? '渠道'}{' '}
                        · #{row.channel_id}：
                        {row.fields.length
                          ? `统一${row.fields.join('、')}`
                          : '共享配置一致'}
                      </p>
                    ))}
                    {input.channel_ids.length === 0 ? (
                      <p>将解除全部关联，渠道保留当前配置独立运行。</p>
                    ) : null}
                  </AlertDescription>
                </Alert>
              ) : (
                <p className='text-muted-foreground text-sm'>
                  {preview
                    ? '配置已修改，请重新预览差异后再确认关联。'
                    : '选择渠道后，点击“预览配置差异”查看本次变更。'}
                </p>
              )}
            </UpstreamEditorSection>
          </div>
        </div>
      </UpstreamEditorLayout>
      <div className={upstreamEditorFooterClassName}>
        <Button
          type='button'
          variant='outline'
          disabled={busy}
          onClick={props.onClose}
        >
          返回列表
        </Button>
        <Button type='submit' variant='outline' disabled={busy}>
          预览配置差异
        </Button>
        <Button
          type='button'
          disabled={busy || !previewCurrent}
          onClick={() => void confirm()}
        >
          {save.isPending ? <Spinner /> : null}确认关联
        </Button>
      </div>
    </form>
  )
}
