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
import { Switch } from '@/components/ui/switch'

import {
  MAX_AUTO_UPDATE_INTERVAL_MINUTES,
  MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
  MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
  MAX_SMART_SCHEDULE_WINDOW_MINUTES,
  MIN_SMART_SCHEDULE_WINDOW_MINUTES,
  type ChannelMonitorSettingsFormValues,
} from '../lib/schema'
import { ChannelMonitorSettingLabel } from './channel-monitor-setting-label'
import { ChannelMonitorSmartScheduleGroupPolicies } from './channel-monitor-smart-schedule-group-policies'

type ChannelMonitorSmartScheduleFieldsProps = {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  modelOptionsByGroup: ReadonlyMap<string, string[]>
  groupOptions: string[]
}

function ChannelMonitorSmartScheduleWindowField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  name:
    | 'smartSchedulePerformanceWindowMinutes'
    | 'smartScheduleStabilityWindowMinutes'
  label: string
  description: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <ChannelMonitorSettingLabel
            label={props.label}
            helpKey='performanceRange'
          />
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={MIN_SMART_SCHEDULE_WINDOW_MINUTES}
                max={MAX_SMART_SCHEDULE_WINDOW_MINUTES}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={props.form.getFieldState(props.name).invalid}
              />
              <InputGroupAddon align='inline-end'>分钟</InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormDescription>{props.description}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
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

function ChannelMonitorRateLimitCooldownField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='smartScheduleRateLimitCooldownSeconds'
      render={({ field }) => (
        <FormItem>
          <ChannelMonitorSettingLabel
            label='429 冷却时间'
            helpKey='rateLimitCooldown'
          />
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={0}
                max={MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors
                    .smartScheduleRateLimitCooldownSeconds
                )}
              />
              <InputGroupAddon align='inline-end'>秒</InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormDescription>
            上游返回 429 后临时停止该渠道对应模型进入新请求；到期自动恢复，0
            秒表示关闭
          </FormDescription>
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
    <div className='flex min-w-0 flex-col gap-5'>
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
            控制所有已配置分组的执行频率、统计窗口、429 临时冷却和响应等待时间
          </p>
        </div>
        <div
          data-slot='smart-schedule-runtime-fields'
          className='grid items-start gap-4 md:grid-cols-2 xl:grid-cols-5'
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

          <ChannelMonitorSmartScheduleWindowField
            form={props.form}
            name='smartSchedulePerformanceWindowMinutes'
            label='性能窗口'
            description='用于首字、TPS 和业务性能评分'
          />

          <ChannelMonitorSmartScheduleWindowField
            form={props.form}
            name='smartScheduleStabilityWindowMinutes'
            label='稳定性评分窗口'
            description='用于成功率、失败耗时和首字抖动的软评分'
          />

          <ChannelMonitorRateLimitCooldownField form={props.form} />

          <ChannelMonitorRelayResponseHeaderTimeoutField form={props.form} />
        </div>
      </section>

      <FormField
        control={props.form.control}
        name='smartScheduleGroupPolicies'
        render={() => (
          <FormItem className='min-w-0'>
            <ChannelMonitorSmartScheduleGroupPolicies
              form={props.form}
              groupOptions={props.groupOptions}
              modelOptionsByGroup={props.modelOptionsByGroup}
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
