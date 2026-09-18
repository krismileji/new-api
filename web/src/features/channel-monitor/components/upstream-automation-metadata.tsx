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
import { useId, useState } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { Checkbox } from '@/components/ui/checkbox'
import { FieldDescription, FieldLegend, FieldSet } from '@/components/ui/field'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import type { UpstreamAccount } from '../api-upstream-accounts'
import type { AutomationMetadata } from '../lib/automation'
import type { ChannelMonitorItem } from '../types'

export function UpstreamAutomationMetadata(props: {
  form: UseFormReturn<AutomationMetadata>
  channels: ChannelMonitorItem[]
  accounts?: UpstreamAccount[]
  onAccountChange?: (account: UpstreamAccount | undefined) => void
}) {
  const id = useId()
  const [search, setSearch] = useState('')
  const accountId = useWatch({
    control: props.form.control,
    name: 'account_id',
  })
  const account = props.accounts?.find((item) => item.id === accountId)
  const accountOptions = [
    { value: 0, label: '自定义（可单独关联账户余额）' },
    ...(props.accounts ?? []).map((item) => ({
      value: item.id,
      label: `继承整套账户配置：${item.name}`,
    })),
  ]
  const ratioChannelOptions = [
    { value: 0, label: '仅使用账户余额' },
    ...props.channels
      .filter((channel) => account?.channel_ids.includes(channel.id))
      .map((channel) => ({ value: channel.id, label: channel.name })),
  ]
  const channels = props.channels.filter((channel) =>
    `${channel.id} ${channel.name}`.toLowerCase().includes(search.toLowerCase())
  )
  return (
    <FieldSet>
      <FieldLegend>任务与调度</FieldLegend>
      <FormField
        control={props.form.control}
        name='account_id'
        render={({ field }) => (
          <FormItem>
            <FormLabel>任务配置方式</FormLabel>
            <Select
              items={accountOptions}
              name={field.name}
              value={field.value ?? 0}
              onValueChange={(value) => {
                if (value === null) return
                field.onChange(value)
                props.form.setValue('ratio_channel_id', 0)
                props.onAccountChange?.(
                  props.accounts?.find((item) => item.id === value)
                )
              }}
            >
              <FormControl>
                <SelectTrigger
                  className='w-full min-w-0'
                  ref={field.ref}
                  onBlur={field.onBlur}
                >
                  <SelectValue className='min-w-0 truncate' />
                </SelectTrigger>
              </FormControl>
              <SelectContent
                alignItemWithTrigger={false}
                className='max-h-[min(20rem,var(--available-height))]'
              >
                <SelectGroup>
                  {accountOptions.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      <span className='break-all whitespace-normal'>
                        {item.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />
      {accountId ? (
        <>
          <FieldDescription>
            认证和余额查询继承账户配置，关联渠道随账户自动更新。
          </FieldDescription>
          <FormField
            control={props.form.control}
            name='ratio_channel_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>倍率来源渠道（倍率规则必选）</FormLabel>
                <Select
                  items={ratioChannelOptions}
                  name={field.name}
                  value={field.value ?? 0}
                  onValueChange={(value) => {
                    if (value !== null) field.onChange(value)
                  }}
                >
                  <FormControl>
                    <SelectTrigger
                      className='w-full min-w-0'
                      ref={field.ref}
                      onBlur={field.onBlur}
                    >
                      <SelectValue className='min-w-0 truncate' />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent
                    alignItemWithTrigger={false}
                    className='max-h-[min(20rem,var(--available-height))]'
                  >
                    <SelectGroup>
                      {ratioChannelOptions.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          <span className='break-all whitespace-normal'>
                            {item.label}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        </>
      ) : null}
      <FormField
        control={props.form.control}
        name='name'
        render={({ field }) => (
          <FormItem>
            <FormLabel>任务名称 / 上游账户</FormLabel>
            <FormControl>
              <Input {...field} placeholder='例如 主账户每日额度重置' />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={props.form.control}
        name='enabled'
        render={({ field }) => (
          <FormItem className='flex items-center gap-3'>
            <FormControl>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </FormControl>
            <FormLabel>启用独立任务</FormLabel>
          </FormItem>
        )}
      />
      <div className='grid gap-4 sm:grid-cols-2'>
        {(
          [
            ['interval_minutes', '检查间隔（分钟）', 10080],
            ['request_timeout', '请求超时（秒）', 120],
          ] as const
        ).map(([name, label, max]) => (
          <FormField
            key={name}
            control={props.form.control}
            name={name}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{label}</FormLabel>
                <FormControl>
                  <Input {...field} type='number' min={1} max={max} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        ))}
      </div>
      {!accountId ? (
        <FormField
          control={props.form.control}
          name='proxy'
          render={({ field }) => (
            <FormItem>
              <FormLabel>任务请求代理（可选）</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder='http:// 或 socks5://，留空直连'
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      ) : null}
      {!accountId ? (
        <FieldSet>
          <FieldLegend variant='label'>关联渠道（可选）</FieldLegend>
          <FieldDescription>
            每次成功检查后重新获取关联渠道的指标，并按既有策略恢复。禁用渠道也会刷新，手动禁用不会自动启用；不关联渠道也能运行。
          </FieldDescription>
          <Label htmlFor={`${id}-search`} className='sr-only'>
            搜索关联渠道
          </Label>
          <Input
            id={`${id}-search`}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder='按名称或 ID 搜索'
          />
          <FormField
            control={props.form.control}
            name='channel_ids'
            render={({ field }) => (
              <FormItem>
                <div className='max-h-40 overflow-y-auto rounded-md border p-3'>
                  {channels.length === 0 ? (
                    <p className='text-muted-foreground text-sm'>
                      没有匹配的渠道
                    </p>
                  ) : (
                    channels.map((channel) => (
                      <div
                        key={channel.id}
                        className='flex items-center gap-2 py-1.5'
                      >
                        <Checkbox
                          id={`${id}-${channel.id}`}
                          checked={field.value.includes(channel.id)}
                          onCheckedChange={(checked) =>
                            field.onChange(
                              checked
                                ? [...field.value, channel.id]
                                : field.value.filter(
                                    (value) => value !== channel.id
                                  )
                            )
                          }
                        />
                        <Label
                          htmlFor={`${id}-${channel.id}`}
                          className='min-w-0 break-all'
                        >
                          {channel.name} · #{channel.id}
                        </Label>
                      </div>
                    ))
                  )}
                  {field.value
                    .filter(
                      (value) =>
                        !props.channels.some((channel) => channel.id === value)
                    )
                    .map((value) => (
                      <div
                        key={value}
                        className='flex items-center gap-2 py-1.5'
                      >
                        <Checkbox
                          id={`${id}-${value}`}
                          checked
                          onCheckedChange={() =>
                            field.onChange(
                              field.value.filter((item) => item !== value)
                            )
                          }
                        />
                        <Label htmlFor={`${id}-${value}`}>
                          渠道 #{value}（已删除，可取消关联）
                        </Label>
                      </div>
                    ))}
                </div>
                <FormMessage />
              </FormItem>
            )}
          />
        </FieldSet>
      ) : null}
    </FieldSet>
  )
}
