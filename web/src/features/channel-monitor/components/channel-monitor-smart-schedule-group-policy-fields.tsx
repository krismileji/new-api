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
import { useMemo } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { MultiSelect } from '@/components/multi-select'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  MAX_SMART_SCHEDULE_COOLDOWN_MINUTES,
  MAX_SMART_SCHEDULE_MIN_SAMPLES,
  type ChannelMonitorSmartSchedulePolicyFormValues,
} from '../lib/schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS,
  CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS,
} from '../lib/smart-schedule-options'

type ChannelMonitorSmartScheduleGroupPolicyFieldsProps = {
  form: UseFormReturn<ChannelMonitorSmartSchedulePolicyFormValues>
  modelOptions: string[]
}

type SmartScheduleMetricGroup = 'smart' | 'ratio'

function GroupPolicyPercentField(props: {
  form: UseFormReturn<ChannelMonitorSmartSchedulePolicyFormValues>
  name:
    | 'scoring.stabilityPercent'
    | 'scoring.relativeWeightStartPercent'
    | 'scoring.relativeWeightFullPercent'
    | `scoring.${SmartScheduleMetricGroup}.costRatioPercent`
    | `scoring.${SmartScheduleMetricGroup}.firstTokenPercent`
    | `scoring.${SmartScheduleMetricGroup}.tpsPercent`
  label: string
}) {
  const inputId = `channel-monitor-group-policy-${props.name.replaceAll('.', '-')}`
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel htmlFor={inputId}>{props.label}</FormLabel>
          <FormControl>
            <InputGroup>
              <InputGroupInput
                id={inputId}
                type='number'
                min={0}
                max={100}
                step={0.1}
                inputMode='decimal'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={props.form.getFieldState(props.name).invalid}
              />
              <InputGroupAddon align='inline-end'>%</InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function GroupPolicyMetricFields(props: {
  form: UseFormReturn<ChannelMonitorSmartSchedulePolicyFormValues>
  group: SmartScheduleMetricGroup
  title: string
}) {
  const percentages = useWatch({
    control: props.form.control,
    name: `scoring.${props.group}`,
  })
  const total =
    Number(percentages.costRatioPercent) +
    Number(percentages.firstTokenPercent) +
    Number(percentages.tpsPercent)

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-baseline justify-between gap-2'>
        <h4 className='text-sm font-medium'>{props.title}</h4>
        <span className='text-muted-foreground text-xs'>合计 {total}%</span>
      </div>
      <div className='grid gap-4 sm:grid-cols-3'>
        <GroupPolicyPercentField
          form={props.form}
          name={`scoring.${props.group}.costRatioPercent`}
          label='成本倍率'
        />
        <GroupPolicyPercentField
          form={props.form}
          name={`scoring.${props.group}.firstTokenPercent`}
          label='首字时间'
        />
        <GroupPolicyPercentField
          form={props.form}
          name={`scoring.${props.group}.tpsPercent`}
          label='TPS'
        />
      </div>
    </div>
  )
}

export function ChannelMonitorSmartScheduleGroupPolicyFields(
  props: ChannelMonitorSmartScheduleGroupPolicyFieldsProps
) {
  const modelOptions = useMemo(
    () => props.modelOptions.map((model) => ({ value: model, label: model })),
    [props.modelOptions]
  )
  const strategy = useWatch({
    control: props.form.control,
    name: 'strategy',
  })
  const stabilityEnabled = useWatch({
    control: props.form.control,
    name: 'stabilityEnabled',
  })
  const relativeWeightEnabled = useWatch({
    control: props.form.control,
    name: 'scoring.relativeWeightEnabled',
  })

  return (
    <div className='flex flex-col gap-5'>
      <div className='grid gap-4 md:grid-cols-2'>
        <FormField
          control={props.form.control}
          name='strategy'
          render={({ field }) => (
            <FormItem>
              <FormLabel>调度方式</FormLabel>
              <Select
                items={CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS}
                value={field.value}
                onValueChange={(value) =>
                  value !== null && field.onChange(value)
                }
              >
                <FormControl>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS.map(
                      (option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      )
                    )}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormDescription>
                {
                  CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS.find(
                    (option) => option.value === field.value
                  )?.description
                }
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='applyMode'
          render={({ field }) => (
            <FormItem>
              <FormLabel>调整方式</FormLabel>
              <Select
                items={CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS}
                value={field.value}
                onValueChange={(value) =>
                  value !== null && field.onChange(value)
                }
              >
                <FormControl>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS.map(
                      (option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      )
                    )}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormDescription>
                {
                  CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS.find(
                    (option) => option.value === field.value
                  )?.description
                }
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>

      <FormField
        control={props.form.control}
        name='models'
        render={({ field }) => (
          <FormItem className='min-w-0'>
            <FormLabel>参与模型</FormLabel>
            <FormControl>
              <MultiSelect
                options={modelOptions}
                selected={field.value}
                onChange={field.onChange}
                placeholder='全部模型'
                emptyText='没有匹配的模型'
                maxVisibleChips={4}
              />
            </FormControl>
            <FormDescription>不选择表示该分组的全部模型</FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <Separator />

      <FormField
        control={props.form.control}
        name='stabilityEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div className='flex flex-col gap-1'>
              <FormLabel>稳定性保护</FormLabel>
              <FormDescription>
                独立控制该分组的低成功率降级和试放恢复
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                aria-label='分组稳定性保护'
              />
            </FormControl>
          </FormItem>
        )}
      />

      {stabilityEnabled && (
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
          <GroupPolicyPercentField
            form={props.form}
            name='scoring.stabilityPercent'
            label='稳定性占比'
          />
          <FormField
            control={props.form.control}
            name='minSuccessRate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>最低成功率</FormLabel>
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={0}
                      max={100}
                      step={1}
                      inputMode='decimal'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.minSuccessRate
                      )}
                    />
                    <InputGroupAddon align='inline-end'>%</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={props.form.control}
            name='minSamples'
            render={({ field }) => (
              <FormItem>
                <FormLabel>最少样本</FormLabel>
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={1}
                      max={MAX_SMART_SCHEDULE_MIN_SAMPLES}
                      step={1}
                      inputMode='numeric'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.minSamples
                      )}
                    />
                    <InputGroupAddon align='inline-end'>次</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={props.form.control}
            name='cooldownMinutes'
            render={({ field }) => (
              <FormItem>
                <FormLabel>降级时长</FormLabel>
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={1}
                      max={MAX_SMART_SCHEDULE_COOLDOWN_MINUTES}
                      step={1}
                      inputMode='numeric'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.cooldownMinutes
                      )}
                    />
                    <InputGroupAddon align='inline-end'>分钟</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      )}

      <Separator />

      <div className='flex flex-col gap-4'>
        <div>
          <h3 className='text-sm font-medium'>评分与权重</h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            当前分组的评分参数和权重配置
          </p>
        </div>

        {strategy === 'smart' && (
          <GroupPolicyMetricFields
            form={props.form}
            group='smart'
            title='智能调度指标占比'
          />
        )}
        {strategy === 'ratio' && (
          <GroupPolicyMetricFields
            form={props.form}
            group='ratio'
            title='按成本倍率调度指标占比'
          />
        )}

        <div className='grid gap-4 sm:grid-cols-2'>
          <FormField
            control={props.form.control}
            name='scoring.curveExponent'
            render={({ field }) => (
              <FormItem>
                <FormLabel>得分曲线指数</FormLabel>
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={0.1}
                      max={5}
                      step={0.1}
                      inputMode='decimal'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.scoring?.curveExponent
                      )}
                    />
                  </InputGroup>
                </FormControl>
                <FormDescription>大于 1 会进一步拉开权重</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={props.form.control}
            name='scoring.relativeWeightEnabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <div className='flex flex-col gap-1'>
                  <FormLabel>相对权重拉伸</FormLabel>
                  <FormDescription>按组内分差渐进拉开流量</FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    aria-label='分组相对权重拉伸'
                  />
                </FormControl>
              </FormItem>
            )}
          />
        </div>

        {relativeWeightEnabled && (
          <div className='grid gap-4 sm:grid-cols-2'>
            <GroupPolicyPercentField
              form={props.form}
              name='scoring.relativeWeightStartPercent'
              label='开始拉伸分差'
            />
            <GroupPolicyPercentField
              form={props.form}
              name='scoring.relativeWeightFullPercent'
              label='完整拉伸分差'
            />
          </div>
        )}
      </div>
    </div>
  )
}
