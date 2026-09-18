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
import { useFieldArray, useForm } from 'react-hook-form'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldSet,
  FieldLegend,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'

import {
  saveChannelLimitGroup,
  channelLimitGroupsKey,
  channelLimitErrorMessage,
  type ChannelLimitGroup,
} from '../api-limit-groups'
import {
  channelLimitGroupSchema,
  type ChannelLimitForm,
} from '../lib/limit-group'
import type { ChannelMonitorItem } from '../types'

type Props = {
  group: ChannelLimitGroup
  groups: ChannelLimitGroup[]
  channels: ChannelMonitorItem[]
  onDone: () => void
}

export function ChannelLimitGroupEditor(props: Props) {
  const queryClient = useQueryClient()
  const form = useForm<ChannelLimitForm>({
    resolver: zodResolver(channelLimitGroupSchema),
    defaultValues: {
      name: props.group.name,
      concurrency_limit: props.group.concurrency_limit,
      rpm_limit: props.group.rpm_limit,
      tiers: props.group.tiers,
      members: props.group.members,
    },
  })
  const tiers = useFieldArray({ control: form.control, name: 'tiers' })
  const values = form.watch()
  const priorityOptions = values.tiers.map((tier) => ({
    value: tier.priority,
    label: String(tier.priority),
  }))
  const mutation = useMutation({
    mutationFn: saveChannelLimitGroup,
    onSuccess: () => {
      toast.success('共享限流配置已保存')
      void queryClient.invalidateQueries({ queryKey: channelLimitGroupsKey })
      props.onDone()
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: channelLimitGroupsKey })
    },
  })
  const boundElsewhere = new Set(
    props.groups
      .filter((group) => group.id !== props.group.id)
      .flatMap((group) => group.members.map((member) => member.channel_id))
  )
  const unavailableMembers = values.members.filter(
    (member) =>
      !props.channels.some((channel) => channel.id === member.channel_id)
  )
  const validation = form.formState.isSubmitted
    ? channelLimitGroupSchema.safeParse(values)
    : null
  const errors =
    validation && !validation.success
      ? validation.error.issues.map((issue) => issue.message)
      : []
  const submit = form.handleSubmit((value) =>
    mutation.mutate({ ...props.group, ...value })
  )
  return (
    <form onSubmit={submit} className='flex min-h-0 flex-col gap-4'>
      <div className='min-h-0 overflow-y-auto px-1'>
        <FieldGroup>
          <Alert>
            <AlertDescription>
              <p>
                严格预留：低等级不能借用高等级的空闲额度。同级成员共享一份预留。
              </p>
              <p>
                所有配置保存后在线生效，无需暂停。已有请求继续执行，并发和最近
                60 秒的 RPM 计数保留；新请求按最新成员、资源优先级和额度判断。
              </p>
            </AlertDescription>
          </Alert>
          <Field data-invalid={!!form.formState.errors.name}>
            <FieldLabel htmlFor='limit-group-name'>组名称</FieldLabel>
            <Input
              id='limit-group-name'
              aria-invalid={!!form.formState.errors.name}
              {...form.register('name')}
            />
          </Field>
          <div className='grid gap-4 sm:grid-cols-2'>
            <Field>
              <FieldLabel htmlFor='group-concurrency'>总并发限制</FieldLabel>
              <Input
                id='group-concurrency'
                type='number'
                min={0}
                max={100000}
                step={1}
                {...form.register('concurrency_limit', { valueAsNumber: true })}
              />
              <FieldDescription>0 表示不限制该维度。</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor='group-rpm'>总 RPM 限制</FieldLabel>
              <Input
                id='group-rpm'
                type='number'
                min={0}
                max={100000}
                step={1}
                {...form.register('rpm_limit', { valueAsNumber: true })}
              />
              <FieldDescription>最近 60 秒的准入尝试数。</FieldDescription>
            </Field>
          </div>
          <FieldSet disabled={mutation.isPending}>
            <FieldLegend>等级预留</FieldLegend>
            {tiers.fields.map((tier, index) => (
              <FieldGroup
                key={tier.id}
                className='grid gap-3 sm:grid-cols-[1fr_1fr_1fr_auto]'
              >
                <Field>
                  <FieldLabel htmlFor={`priority-${index}`}>
                    资源优先级
                  </FieldLabel>
                  <Input
                    id={`priority-${index}`}
                    type='number'
                    min={0}
                    max={1000}
                    step={1}
                    {...form.register(`tiers.${index}.priority`, {
                      valueAsNumber: true,
                    })}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor={`reserve-c-${index}`}>
                    预留并发
                  </FieldLabel>
                  <Input
                    id={`reserve-c-${index}`}
                    type='number'
                    min={0}
                    max={100000}
                    step={1}
                    {...form.register(`tiers.${index}.reserved_concurrency`, {
                      valueAsNumber: true,
                    })}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor={`reserve-r-${index}`}>
                    预留 RPM
                  </FieldLabel>
                  <Input
                    id={`reserve-r-${index}`}
                    type='number'
                    min={0}
                    max={100000}
                    step={1}
                    {...form.register(`tiers.${index}.reserved_rpm`, {
                      valueAsNumber: true,
                    })}
                  />
                </Field>
                <Button
                  type='button'
                  variant='outline'
                  className='self-end'
                  disabled={tiers.fields.length === 1}
                  aria-label={`移除第 ${index + 1} 个等级`}
                  onClick={() => tiers.remove(index)}
                >
                  移除
                </Button>
              </FieldGroup>
            ))}
            <Button
              type='button'
              variant='outline'
              disabled={tiers.fields.length >= 8}
              onClick={() =>
                tiers.append({
                  priority: 0,
                  reserved_concurrency: 0,
                  reserved_rpm: 0,
                })
              }
            >
              添加等级
            </Button>
            <FieldDescription>
              数值越大越优先。同级成员共享预留，修改后立即按新规则判断可用额度。
            </FieldDescription>
            {[...values.tiers]
              .sort((a, b) => b.priority - a.priority)
              .map((tier) => {
                const higher = values.tiers.filter(
                  (item) => item.priority > tier.priority
                )
                return (
                  <p
                    className='text-muted-foreground text-xs'
                    key={tier.priority}
                  >
                    等级 ≤ {tier.priority} 合计上限：并发{' '}
                    {values.concurrency_limit > 0
                      ? values.concurrency_limit -
                        higher.reduce(
                          (sum, item) => sum + item.reserved_concurrency,
                          0
                        )
                      : '不限'}{' '}
                    / RPM{' '}
                    {values.rpm_limit > 0
                      ? values.rpm_limit -
                        higher.reduce((sum, item) => sum + item.reserved_rpm, 0)
                      : '不限'}
                  </p>
                )
              })}
          </FieldSet>
          <FieldSet disabled={mutation.isPending}>
            <FieldLegend>成员渠道（{values.members.length} / 64）</FieldLegend>
            <FieldDescription>
              原有单渠道上限仍然生效。每个渠道最多加入一个共享限流组。
            </FieldDescription>
            <div className='flex max-h-64 flex-col gap-3 overflow-y-auto'>
              {props.channels.map((channel) => {
                const member = values.members.find(
                  (item) => item.channel_id === channel.id
                )
                const bound = boundElsewhere.has(channel.id)
                return (
                  <Field key={channel.id} orientation='horizontal'>
                    <Checkbox
                      id={`member-${channel.id}`}
                      checked={!!member}
                      disabled={bound || mutation.isPending}
                      onCheckedChange={(checked) =>
                        form.setValue(
                          'members',
                          checked
                            ? [
                                ...values.members,
                                {
                                  channel_id: channel.id,
                                  priority: values.tiers.at(-1)?.priority ?? 0,
                                },
                              ]
                            : values.members.filter(
                                (item) => item.channel_id !== channel.id
                              ),
                          { shouldValidate: true }
                        )
                      }
                    />
                    <FieldLabel
                      htmlFor={`member-${channel.id}`}
                      className='min-w-0 flex-1 break-all'
                    >
                      {channel.name} · #{channel.id}
                      {bound ? '（已加入其他组）' : ''}
                    </FieldLabel>
                    {member && (
                      <Select
                        items={priorityOptions}
                        value={member.priority}
                        disabled={bound || mutation.isPending}
                        onValueChange={(priority) => {
                          if (priority === null) return
                          form.setValue(
                            'members',
                            values.members.map((item) =>
                              item.channel_id === channel.id
                                ? {
                                    ...item,
                                    priority,
                                  }
                                : item
                            ),
                            { shouldValidate: true }
                          )
                        }}
                      >
                        <SelectTrigger
                          aria-label={`${channel.name} 的资源优先级`}
                          className='w-24 shrink-0'
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {values.tiers.map((tier, index) => (
                              <SelectItem
                                key={tiers.fields[index].id}
                                value={tier.priority}
                              >
                                {tier.priority}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    )}
                  </Field>
                )
              })}
              {unavailableMembers.map((member) => (
                <p key={member.channel_id}>
                  渠道 #{member.channel_id} 当前不在列表中，绑定会保留。
                </p>
              ))}
            </div>
          </FieldSet>
          {(errors.length > 0 || mutation.isError) && (
            <Alert variant='destructive'>
              <AlertDescription>
                {mutation.isError
                  ? channelLimitErrorMessage(mutation.error)
                  : errors.join('；')}
              </AlertDescription>
            </Alert>
          )}
        </FieldGroup>
      </div>
      <div className='flex shrink-0 justify-end gap-2'>
        <Button
          type='button'
          variant='outline'
          disabled={mutation.isPending}
          onClick={props.onDone}
        >
          返回
        </Button>
        <Button type='submit' disabled={mutation.isPending}>
          {mutation.isPending && <Spinner />}保存配置
        </Button>
      </div>
    </form>
  )
}
