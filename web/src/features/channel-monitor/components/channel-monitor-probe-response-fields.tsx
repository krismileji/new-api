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
import type { UseFormReturn } from 'react-hook-form'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import type { ChannelMonitorSettingsFormValues } from '../lib/schema'

const probeResponseRules = [
  { label: '匹配输入', value: 'hi' },
  { label: '固定响应', value: 'Hi. What are you working on?' },
  { label: '随机延迟', value: '0.5-2 秒' },
  { label: '支持接口', value: '/v1/responses、/v1/chat/completions' },
] as const

export function ChannelMonitorProbeResponseFields(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <div className='flex flex-col gap-5'>
      <FormField
        control={props.form.control}
        name='probeResponseEnabled'
        render={({ field }) => (
          <FormItem className='flex items-start justify-between gap-4 rounded-md border p-4'>
            <div className='flex min-w-0 flex-col gap-1'>
              <FormLabel>启用本地探针响应</FormLabel>
              <FormDescription>
                命中后由本机直接完成请求，不选择渠道，也不产生消费或渠道成本记录
              </FormDescription>
              <FormMessage />
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                aria-label='启用本地探针响应'
              />
            </FormControl>
          </FormItem>
        )}
      />

      <dl
        className='divide-y rounded-md border text-sm'
        aria-label='探针响应规则'
      >
        {probeResponseRules.map((rule) => (
          <div
            key={rule.label}
            className='grid grid-cols-[6rem_minmax(0,1fr)] gap-3 px-4 py-3'
          >
            <dt className='text-muted-foreground'>{rule.label}</dt>
            <dd className='min-w-0 font-medium break-words'>{rule.value}</dd>
          </div>
        ))}
      </dl>

      <p className='text-muted-foreground text-sm leading-relaxed'>
        仅匹配单轮纯文本
        hi。历史对话、其他文本、图片和工具结果仍按正常流程请求上游；渠道连通性测试不经过此拦截。
      </p>
    </div>
  )
}
