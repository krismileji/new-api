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
import { Add01Icon, Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useFieldArray, useWatch, type UseFormReturn } from 'react-hook-form'

import { Button } from '@/components/ui/button'
import {
  FieldDescription,
  FieldGroup,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import type { UpstreamConfigFormValues } from '../lib/schema'

type ChannelMonitorCustomVariableMappingsProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  requestIndex: number
  disabled: boolean
  canAddVariable: boolean
}

export function ChannelMonitorCustomVariableMappings(
  props: ChannelMonitorCustomVariableMappingsProps
) {
  const prefix = `customConfig.variableRequests.${props.requestIndex}` as const
  const mappings = useFieldArray({
    control: props.form.control,
    name: `${prefix}.variables`,
  })
  const responseType = useWatch({
    control: props.form.control,
    name: `${prefix}.responseType`,
  })
  const variables = useWatch({
    control: props.form.control,
    name: `${prefix}.variables`,
  })

  return (
    <FieldSet className='min-w-0' disabled={props.disabled}>
      <FieldLegend variant='label'>变量映射</FieldLegend>
      <div className='flex items-start justify-between gap-3'>
        <div>
          <FieldDescription>
            一次请求提取多个值。当前值可手填，也可通过请求回填；保存后按本请求的策略刷新。
          </FieldDescription>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled || !props.canAddVariable}
          onClick={() =>
            mappings.append({
              name: '',
              valuePath: '',
              value: '',
              hasValue: false,
            })
          }
        >
          <HugeiconsIcon icon={Add01Icon} aria-hidden='true' />
          添加变量
        </Button>
      </div>
      <FieldGroup className='gap-3'>
        {mappings.fields.map((mapping, index) => (
          <div
            key={mapping.id}
            className='grid min-w-0 gap-3 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)_minmax(0,1.2fr)_auto]'
          >
            <FormField
              control={props.form.control}
              name={`${prefix}.variables.${index}.name`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>变量名</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      autoComplete='off'
                      placeholder='token'
                      className='font-mono'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={props.form.control}
              name={`${prefix}.variables.${index}.valuePath`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>JSON 取值路径</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      disabled={responseType === 'text'}
                      placeholder={
                        responseType === 'text'
                          ? '使用完整文本响应'
                          : 'data.access_token'
                      }
                      className='font-mono'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={props.form.control}
              name={`${prefix}.variables.${index}.value`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>当前值</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type='password'
                      autoComplete='off'
                      placeholder={
                        variables[index]?.hasValue
                          ? '已配置，留空保持不变'
                          : '可手填，或从接口获取'
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              className='justify-self-end sm:mt-6'
              disabled={props.disabled || mappings.fields.length === 1}
              onClick={() => mappings.remove(index)}
              aria-label={`删除变量 ${variables[index]?.name || index + 1}`}
            >
              <HugeiconsIcon icon={Delete02Icon} aria-hidden='true' />
            </Button>
          </div>
        ))}
      </FieldGroup>
    </FieldSet>
  )
}
