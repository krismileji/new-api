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
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import { createChannelMonitorCustomAction } from '../lib/custom-upstream'
import type { UpstreamConfigFormValues } from '../lib/schema'
import type {
  ChannelMonitorCustomAction,
  ChannelMonitorCustomActionState,
} from '../types'
import { ChannelMonitorCustomActionQuota } from './channel-monitor-custom-action-quota'
import { ChannelMonitorCustomRequestFields } from './channel-monitor-custom-request-fields'

type ActionFieldsProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  disabled: boolean
  states?: Record<string, ChannelMonitorCustomActionState>
  stateError?: string
  savedActions?: ChannelMonitorCustomAction[]
  onReset: (
    actionId: string,
    state: ChannelMonitorCustomActionState
  ) => Promise<void>
}

export function ChannelMonitorCustomActionFields(props: ActionFieldsProps) {
  const actions = useFieldArray({
    control: props.form.control,
    name: 'customConfig.actions',
    keyName: 'fieldKey',
  })
  return (
    <FieldSet className='min-w-0'>
      <FieldLegend>条件触发接口</FieldLegend>
      <FieldDescription>
        保存并启用后，在手动或自动刷新指标成功时检查。条件首次满足时调用一次，指标恢复到条件外后才能再次触发；保存和“测试获取”不会调用。
      </FieldDescription>
      {props.stateError ? (
        <p role='alert' className='text-destructive text-sm'>
          {props.stateError}
        </p>
      ) : null}
      <FieldGroup>
        {actions.fields.map((action, index) => (
          <CustomActionRule
            key={action.fieldKey}
            form={props.form}
            disabled={props.disabled}
            index={index}
            state={props.states?.[action.id]}
            stateError={props.stateError}
            savedAction={props.savedActions?.find(
              (saved) => saved.id === action.id
            )}
            onReset={props.onReset}
            onRemove={() => actions.remove(index)}
          />
        ))}
      </FieldGroup>
      <Button
        type='button'
        variant='outline'
        className='self-start'
        disabled={props.disabled || actions.fields.length >= 8}
        onClick={() => actions.append(createChannelMonitorCustomAction())}
      >
        添加触发规则
      </Button>
    </FieldSet>
  )
}

type CustomActionRuleProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  disabled: boolean
  index: number
  state?: ChannelMonitorCustomActionState
  stateError?: string
  savedAction?: ChannelMonitorCustomAction
  onReset: ActionFieldsProps['onReset']
  onRemove: () => void
}

function CustomActionRule(props: CustomActionRuleProps) {
  const prefix = `customConfig.actions.${props.index}` as const
  const name = useWatch({ control: props.form.control, name: `${prefix}.name` })
  return (
    <FieldSet
      className='bg-muted/20 min-w-0 rounded-lg border p-4'
      aria-label={`触发规则 ${name || props.index + 1}`}
    >
      <div className='flex items-center justify-between gap-3'>
        <FormField
          control={props.form.control}
          name={`${prefix}.enabled`}
          render={({ field }) => (
            <FormItem className='flex items-center gap-2'>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={props.disabled}
                />
              </FormControl>
              <FormLabel>启用规则</FormLabel>
            </FormItem>
          )}
        />
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={props.disabled}
          onClick={props.onRemove}
          aria-label={`删除触发规则 ${name || props.index + 1}`}
        >
          删除规则
        </Button>
      </div>
      <FormField
        control={props.form.control}
        name={`${prefix}.name`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>规则名称</FormLabel>
            <FormControl>
              <Input {...field} disabled={props.disabled} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FieldGroup className='grid min-w-0 gap-4 sm:grid-cols-2'>
        <FormField
          control={props.form.control}
          name={`${prefix}.metric`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>触发指标</FormLabel>
              <FormControl>
                <ToggleGroup
                  value={[field.value]}
                  onValueChange={(values) => {
                    const value = values.find((item) => item !== field.value)
                    if (value === 'balance' || value === 'ratio') {
                      field.onChange(value)
                    }
                  }}
                  disabled={props.disabled}
                  variant='outline'
                  spacing={2}
                  className='w-full'
                >
                  <ToggleGroupItem value='balance'>上游余额</ToggleGroupItem>
                  <ToggleGroupItem value='ratio'>上游倍率</ToggleGroupItem>
                </ToggleGroup>
              </FormControl>
              <FormDescription>
                使用接口提取后的数值，倍率不含成本换算，余额不含本地估算扣减。
              </FormDescription>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.operator`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>触发条件</FormLabel>
              <FormControl>
                <ToggleGroup
                  value={[field.value]}
                  onValueChange={(values) => {
                    const value = values.find((item) => item !== field.value)
                    if (value) field.onChange(value)
                  }}
                  disabled={props.disabled}
                  variant='outline'
                  spacing={1}
                  className='grid w-full grid-cols-2'
                >
                  <ToggleGroupItem value='lt'>小于</ToggleGroupItem>
                  <ToggleGroupItem value='lte'>小于等于</ToggleGroupItem>
                  <ToggleGroupItem value='gt'>大于</ToggleGroupItem>
                  <ToggleGroupItem value='gte'>大于等于</ToggleGroupItem>
                </ToggleGroup>
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.threshold`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>触发阈值</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  type='number'
                  step='any'
                  disabled={props.disabled}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.timezone`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>执行时区</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder='Asia/Shanghai'
                  disabled={props.disabled}
                />
              </FormControl>
              <FormDescription>
                例如 Asia/Shanghai（北京时间）或 UTC。
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        {(
          [
            ['startTime', '开始时间（含）'],
            ['endTime', '截止时间（不含）'],
          ] as const
        ).map(([key, label]) => (
          <FormField
            key={key}
            control={props.form.control}
            name={`${prefix}.${key}`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{label}</FormLabel>
                <FormControl>
                  <Input {...field} type='time' disabled={props.disabled} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        ))}
        {(
          [
            ['dailyLimit', '每日最多调用次数', 100],
            ['cooldownMinutes', '两次调用最短间隔（分钟）', 10080],
          ] as const
        ).map(([key, label, max]) => (
          <FormField
            key={key}
            control={props.form.control}
            name={`${prefix}.${key}`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{label}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='number'
                    min={1}
                    max={max}
                    step={1}
                    disabled={props.disabled}
                  />
                </FormControl>
                <FormMessage />
                {key === 'dailyLimit' && (
                  <ChannelMonitorCustomActionQuota
                    savedAction={props.savedAction}
                    state={props.state}
                    stateError={props.stateError}
                    dailyLimit={Number(field.value)}
                    disabled={props.disabled}
                    onReset={props.onReset}
                  />
                )}
              </FormItem>
            )}
          />
        ))}
      </FieldGroup>
      <FieldDescription>
        例如 00:05–23:00：23:00
        起停止发起调用。请给上游自动重置留出时间；已发出的请求无法保证撤销。失败、超时和结果未知均计入次数，不自动重试。自动检查需要开启对应指标的定时刷新。
      </FieldDescription>
      <FormField
        control={props.form.control}
        name={`${prefix}.baseUrl`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>触发接口基础地址</FormLabel>
            <FormControl>
              <Input
                {...field}
                placeholder='留空使用上游基础地址'
                disabled={props.disabled}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <ChannelMonitorCustomRequestFields
        form={props.form}
        prefix={`${prefix}.request`}
        allowVariables
        disabled={props.disabled}
      />
      <FieldGroup className='grid min-w-0 gap-4 sm:grid-cols-2'>
        <FormField
          control={props.form.control}
          name={`${prefix}.successPath`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>成功判定 JSON 路径（可选）</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder='例如 success'
                  disabled={props.disabled}
                />
              </FormControl>
              <FormDescription>留空时以 HTTP 2xx 判断成功。</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.successValue`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>成功判定期望值</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder='例如 true 或 0'
                  disabled={props.disabled}
                />
              </FormControl>
              <FormDescription>
                字符串无需引号，布尔值填写 true 或 false。
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </FieldGroup>
      {props.state && props.state.last_attempt > 0 ? (
        <div role='status' className='bg-muted rounded-md p-3 text-sm'>
          <p>{props.state.message}</p>
          <p className='text-muted-foreground mt-1'>
            上次登记：
            {new Date(props.state.last_attempt * 1000).toLocaleString(
              'zh-CN'
            )}{' '}
            · 指标值 {props.state.last_value}
          </p>
          {props.state.triggered ? (
            <p className='mt-1'>等待指标恢复到触发条件外后重新检测。</p>
          ) : null}
        </div>
      ) : null}
    </FieldSet>
  )
}
