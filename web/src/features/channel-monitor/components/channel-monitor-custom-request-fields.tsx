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
import { useWatch, type UseFormReturn } from 'react-hook-form'

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import type { UpstreamConfigFormValues } from '../lib/schema'
import { ChannelMonitorCustomKeyValueEditor } from './channel-monitor-custom-key-value-editor'

export type CustomRequestPrefix =
  | 'customConfig.ratio.request'
  | 'customConfig.balance.request'
  | `customConfig.variableRequests.${number}.request`

type ChannelMonitorCustomRequestFieldsProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  prefix: CustomRequestPrefix
  allowVariables?: boolean
  disabled?: boolean
}

export function ChannelMonitorCustomRequestFields(
  props: ChannelMonitorCustomRequestFieldsProps
) {
  const method = useWatch({
    control: props.form.control,
    name: `${props.prefix}.method`,
  })
  const bodyType = useWatch({
    control: props.form.control,
    name: `${props.prefix}.bodyType`,
  })
  const bodySecret = useWatch({
    control: props.form.control,
    name: `${props.prefix}.bodySecret`,
  })
  const hasBody = props.form.getValues(`${props.prefix}.hasBody`)
  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <div className='grid min-w-0 gap-4 sm:grid-cols-[10rem_minmax(0,1fr)]'>
        <FormField
          control={props.form.control}
          name={`${props.prefix}.method`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>请求方式</FormLabel>
              <FormControl>
                <ToggleGroup
                  disabled={props.disabled}
                  value={[field.value]}
                  onValueChange={(values) => {
                    const value = values.find((item) => item !== field.value)
                    if (value !== 'GET' && value !== 'POST') return
                    field.onChange(value)
                    if (value === 'GET') {
                      props.form.setValue(`${props.prefix}.bodyType`, 'none', {
                        shouldValidate: true,
                      })
                    }
                  }}
                  variant='outline'
                  spacing={2}
                  className='grid w-full grid-cols-2'
                >
                  <ToggleGroupItem value='GET' className='w-full'>
                    GET
                  </ToggleGroupItem>
                  <ToggleGroupItem value='POST' className='w-full'>
                    POST
                  </ToggleGroupItem>
                </ToggleGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${props.prefix}.path`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>接口路径</FormLabel>
              <FormControl>
                <Input
                  placeholder='/api/account'
                  value={field.value}
                  onBlur={field.onBlur}
                  onChange={field.onChange}
                  name={field.name}
                  ref={field.ref}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>

      <ChannelMonitorCustomKeyValueEditor
        form={props.form}
        name={`${props.prefix}.query`}
        label='查询参数'
        allowVariables={props.allowVariables}
        disabled={props.disabled}
      />
      <ChannelMonitorCustomKeyValueEditor
        form={props.form}
        name={`${props.prefix}.headers`}
        label='请求头'
        allowVariables={props.allowVariables}
        disabled={props.disabled}
      />

      {method === 'POST' ? (
        <>
          <FormField
            control={props.form.control}
            name={`${props.prefix}.bodyType`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>请求体</FormLabel>
                <FormControl>
                  <ToggleGroup
                    disabled={props.disabled}
                    value={[field.value]}
                    onValueChange={(values) => {
                      const value = values.find((item) => item !== field.value)
                      if (
                        value !== 'none' &&
                        value !== 'json' &&
                        value !== 'form'
                      ) {
                        return
                      }
                      field.onChange(value)
                    }}
                    variant='outline'
                    spacing={2}
                    className='grid w-full grid-cols-3'
                  >
                    <ToggleGroupItem value='none' className='w-full'>
                      无
                    </ToggleGroupItem>
                    <ToggleGroupItem value='json' className='w-full'>
                      JSON
                    </ToggleGroupItem>
                    <ToggleGroupItem value='form' className='w-full'>
                      表单
                    </ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {bodyType === 'json' ? (
            <div className='flex min-w-0 flex-col gap-2'>
              <FormField
                control={props.form.control}
                name={`${props.prefix}.body`}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>JSON 内容</FormLabel>
                    <FormControl>
                      <Textarea
                        rows={5}
                        className='font-mono text-xs'
                        placeholder={
                          bodySecret && hasBody
                            ? '已配置，留空保持不变'
                            : '{\n  "group": "vip"\n}'
                        }
                        value={field.value}
                        onBlur={field.onBlur}
                        onChange={field.onChange}
                        name={field.name}
                        ref={field.ref}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={props.form.control}
                name={`${props.prefix}.bodySecret`}
                render={({ field }) => (
                  <FormItem className='flex items-center gap-2'>
                    <FormControl>
                      <Switch
                        disabled={props.disabled}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        aria-label='将 JSON 请求体作为敏感信息保存'
                      />
                    </FormControl>
                    <FormLabel className='font-normal'>敏感请求体</FormLabel>
                  </FormItem>
                )}
              />
            </div>
          ) : null}
          {bodyType === 'form' ? (
            <ChannelMonitorCustomKeyValueEditor
              form={props.form}
              name={`${props.prefix}.form`}
              label='表单参数'
              disabled={props.disabled}
            />
          ) : null}
        </>
      ) : null}
    </div>
  )
}
