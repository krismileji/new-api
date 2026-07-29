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

import { Checkbox } from '@/components/ui/checkbox'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import {
  MAX_AUTO_UPDATE_INTERVAL_MINUTES,
  MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
  type ChannelMonitorSettingsFormValues,
} from '../lib/schema'
import { ChannelMonitorSettingLabel } from './channel-monitor-setting-label'
import { ChannelMonitorSmartScheduleGroupPolicies } from './channel-monitor-smart-schedule-group-policies'

const PERFORMANCE_RANGE_OPTIONS = [
  { value: '15', label: '近 15 分钟' },
  { value: '60', label: '近 1 小时' },
  { value: '360', label: '近 6 小时' },
  { value: '1440', label: '近 24 小时' },
]

type ChannelMonitorSmartScheduleFieldsProps = {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  modelOptions: string[]
  groupOptions: string[]
}

function ChannelMonitorRelayResponseHeaderTimeoutField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='relayResponseHeaderTimeoutSeconds'
      render={({ field }) => (
        <FormItem>
          <ChannelMonitorSettingLabel
            label='上游响应等待时间'
            helpKey='responseHeaderTimeout'
          />
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={0}
                max={MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors.relayResponseHeaderTimeoutSeconds
                )}
              />
              <InputGroupAddon align='inline-end'>秒</InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorSmartScheduleFields(
  props: ChannelMonitorSmartScheduleFieldsProps
) {
  return (
    <div className='flex flex-col gap-5'>
      <FormField
        control={props.form.control}
        name='smartScheduleEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div className='flex flex-col gap-1'>
              <ChannelMonitorSettingLabel label='智能调度' helpKey='enabled' />
              <FormDescription>
                只调度下方已配置策略的分组；未配置分组不会参与智能调度
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                aria-label='开启智能调度'
              />
            </FormControl>
          </FormItem>
        )}
      />

      <section
        className='flex flex-col gap-4'
        aria-labelledby='channel-monitor-smart-schedule-runtime-title'
      >
        <div>
          <h3
            id='channel-monitor-smart-schedule-runtime-title'
            className='text-sm font-medium'
          >
            运行设置
          </h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            控制所有已配置分组的执行频率、统计窗口和响应等待时间
          </p>
        </div>
        <div
          data-slot='smart-schedule-runtime-fields'
          className='grid items-start gap-4 md:grid-cols-2 lg:grid-cols-3'
        >
          <FormField
            control={props.form.control}
            name='smartScheduleIntervalMinutes'
            render={({ field }) => (
              <FormItem>
                <ChannelMonitorSettingLabel
                  label='调度间隔'
                  helpKey='interval'
                />
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={1}
                      max={MAX_AUTO_UPDATE_INTERVAL_MINUTES}
                      step={1}
                      inputMode='numeric'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.smartScheduleIntervalMinutes
                      )}
                    />
                    <InputGroupAddon align='inline-end'>分钟</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={props.form.control}
            name='smartSchedulePerformanceMinutes'
            render={({ field }) => (
              <FormItem>
                <ChannelMonitorSettingLabel
                  label='统计范围'
                  helpKey='performanceRange'
                />
                <Select
                  items={PERFORMANCE_RANGE_OPTIONS}
                  value={String(field.value)}
                  onValueChange={(value) => {
                    if (value !== null) field.onChange(Number(value))
                  }}
                >
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {PERFORMANCE_RANGE_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          <ChannelMonitorRelayResponseHeaderTimeoutField form={props.form} />
        </div>
      </section>

      <FormField
        control={props.form.control}
        name='smartScheduleGroupPolicies'
        render={() => (
          <FormItem>
            <ChannelMonitorSmartScheduleGroupPolicies
              form={props.form}
              groupOptions={props.groupOptions}
              modelOptions={props.modelOptions}
            />
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={props.form.control}
        name='smartScheduleForceReset'
        render={({ field }) => (
          <FormItem className='flex items-start gap-3 rounded-lg border p-3'>
            <FormControl>
              <Checkbox
                id='channel-monitor-force-smart-schedule-reset'
                checked={field.value}
                onCheckedChange={(checked) => field.onChange(checked === true)}
                aria-label='强制重置优先级和权重'
              />
            </FormControl>
            <div className='flex flex-col gap-1'>
              <ChannelMonitorSettingLabel
                label='强制重置优先级和权重'
                helpKey='forceReset'
                htmlFor='channel-monitor-force-smart-schedule-reset'
              />
              <FormDescription>
                保存后，根据当前日志重新计算所有已配置分组中的参与渠道，并立即应用优先级和权重。此操作仅执行一次。
              </FormDescription>
            </div>
          </FormItem>
        )}
      />
    </div>
  )
}
