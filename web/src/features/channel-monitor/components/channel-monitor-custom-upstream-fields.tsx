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

import { FieldLegend, FieldSet } from '@/components/ui/field'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  MAX_CUSTOM_UPSTREAM_BALANCE,
  type UpstreamConfigFormValues,
} from '../lib/schema'
import { ChannelMonitorCustomRequestFields } from './channel-monitor-custom-request-fields'

type CustomMetricName = 'ratio' | 'balance'

type ChannelMonitorCustomUpstreamFieldsProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
}

type CustomMetricFieldsProps = ChannelMonitorCustomUpstreamFieldsProps & {
  metric: CustomMetricName
  reuseRequest: boolean
}

type CustomRequestFieldsProps = ChannelMonitorCustomUpstreamFieldsProps & {
  metric: CustomMetricName
  showRequest: boolean
}

function CustomRequestFields(props: CustomRequestFieldsProps) {
  const prefix = `customConfig.${props.metric}` as const
  const responseType = useWatch({
    control: props.form.control,
    name: `${prefix}.result.responseType`,
  })

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      {props.showRequest ? (
        <ChannelMonitorCustomRequestFields
          form={props.form}
          prefix={`${prefix}.request`}
          allowVariables
        />
      ) : null}

      <div className='grid min-w-0 gap-4 sm:grid-cols-[10rem_minmax(0,1fr)_10rem]'>
        <FormField
          control={props.form.control}
          name={`${prefix}.result.responseType`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>响应格式</FormLabel>
              <FormControl>
                <ToggleGroup
                  value={[field.value]}
                  onValueChange={(values) => {
                    const value = values.find((item) => item !== field.value)
                    if (value !== 'json' && value !== 'text') return
                    field.onChange(value)
                  }}
                  variant='outline'
                  spacing={2}
                  className='grid w-full grid-cols-2'
                >
                  <ToggleGroupItem value='json' className='w-full'>
                    JSON
                  </ToggleGroupItem>
                  <ToggleGroupItem value='text' className='w-full'>
                    文本
                  </ToggleGroupItem>
                </ToggleGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.result.valuePath`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>JSON 取值路径</FormLabel>
              <FormControl>
                <Input
                  placeholder='data.ratio'
                  disabled={responseType === 'text'}
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
          name={`${prefix}.result.multiplier`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>结果乘数</FormLabel>
              <FormControl>
                <InputGroup className='ring-inset'>
                  <InputGroupAddon>×</InputGroupAddon>
                  <InputGroupInput
                    type='number'
                    min={0}
                    max={1_000_000}
                    step='any'
                    inputMode='decimal'
                    value={field.value}
                    onBlur={field.onBlur}
                    onChange={field.onChange}
                    name={field.name}
                    ref={field.ref}
                  />
                </InputGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
    </div>
  )
}

function CustomMetricFields(props: CustomMetricFieldsProps) {
  const prefix = `customConfig.${props.metric}` as const
  const source = useWatch({
    control: props.form.control,
    name: `${prefix}.source`,
  })
  const isRatio = props.metric === 'ratio'

  return (
    <FieldSet className='min-w-0 rounded-md border p-4'>
      <FieldLegend variant='label' className='px-1'>
        {isRatio ? '上游倍率来源' : '上游余额来源'}
      </FieldLegend>
      <FormField
        control={props.form.control}
        name={`${prefix}.source`}
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <ToggleGroup
                value={[field.value]}
                onValueChange={(values) => {
                  const value = values.find((item) => item !== field.value)
                  if (value !== 'fixed' && value !== 'http') return
                  field.onChange(value)
                  if (value === 'fixed') {
                    props.form.setValue(
                      'customConfig.balanceReuseRatioRequest',
                      false,
                      { shouldValidate: true }
                    )
                  }
                }}
                variant='outline'
                spacing={2}
                className='grid w-full grid-cols-2'
              >
                <ToggleGroupItem value='fixed' className='w-full'>
                  固定输入
                </ToggleGroupItem>
                <ToggleGroupItem value='http' className='w-full'>
                  接口查询
                </ToggleGroupItem>
              </ToggleGroup>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {source === 'fixed' ? (
        <FormField
          control={props.form.control}
          name={`${prefix}.fixedValue`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{isRatio ? '固定倍率' : '固定余额'}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={isRatio ? 0 : -MAX_CUSTOM_UPSTREAM_BALANCE}
                  max={isRatio ? 1_000_000 : MAX_CUSTOM_UPSTREAM_BALANCE}
                  step='any'
                  inputMode='decimal'
                  value={field.value}
                  onBlur={field.onBlur}
                  onChange={field.onChange}
                  name={field.name}
                  ref={field.ref}
                />
              </FormControl>
              <FormDescription>保存后立即写入渠道监控。</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      ) : (
        <CustomRequestFields
          form={props.form}
          metric={props.metric}
          showRequest={!props.reuseRequest}
        />
      )}
    </FieldSet>
  )
}

export function ChannelMonitorCustomUpstreamFields(
  props: ChannelMonitorCustomUpstreamFieldsProps
) {
  const ratioSource = useWatch({
    control: props.form.control,
    name: 'customConfig.ratio.source',
  })
  const balanceSource = useWatch({
    control: props.form.control,
    name: 'customConfig.balance.source',
  })
  const reuseRequest = useWatch({
    control: props.form.control,
    name: 'customConfig.balanceReuseRatioRequest',
  })
  const canReuseRequest = ratioSource === 'http' && balanceSource === 'http'

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <CustomMetricFields
        form={props.form}
        metric='ratio'
        reuseRequest={false}
      />
      {canReuseRequest ? (
        <FormField
          control={props.form.control}
          name='customConfig.balanceReuseRatioRequest'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-4 rounded-md border px-4 py-3'>
              <div className='flex min-w-0 flex-col gap-1'>
                <FormLabel>余额复用倍率接口</FormLabel>
                <FormDescription>
                  只发送一次请求，余额使用独立的取值路径和结果乘数。
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  aria-label='余额复用倍率接口'
                />
              </FormControl>
            </FormItem>
          )}
        />
      ) : null}
      <CustomMetricFields
        form={props.form}
        metric='balance'
        reuseRequest={reuseRequest && canReuseRequest}
      />
    </div>
  )
}
