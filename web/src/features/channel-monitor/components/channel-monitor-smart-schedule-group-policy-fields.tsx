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
import { Refresh01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  MAX_SMART_SCHEDULE_COOLDOWN_MINUTES,
  MAX_SMART_SCHEDULE_EXPLORATION_TRAFFIC_PERCENT,
  MAX_SMART_SCHEDULE_JITTER_ABSOLUTE_TOLERANCE_SECONDS,
  MAX_SMART_SCHEDULE_JITTER_BASELINE_MINUTES,
  MAX_SMART_SCHEDULE_JITTER_THRESHOLD_MULTIPLIER,
  MAX_SMART_SCHEDULE_JITTER_TOLERANCE_PERCENT,
  MAX_SMART_SCHEDULE_MIN_SAMPLES,
  MAX_SMART_SCHEDULE_PRIMARY_SWITCH_THRESHOLD_PERCENT,
  MAX_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT,
  MAX_SMART_SCHEDULE_PROBE_INTERVAL_MINUTES,
  MIN_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT,
  type ChannelMonitorSmartSchedulePolicyFormValues,
} from '../lib/schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS,
  CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS,
} from '../lib/smart-schedule-options'
import {
  ChannelMonitorSettingLabel,
  type ChannelMonitorSettingHelpKey,
} from './channel-monitor-setting-label'
import { ChannelMonitorSmartScheduleModelOrder } from './channel-monitor-smart-schedule-model-order'

type ChannelMonitorSmartScheduleGroupPolicyFieldsProps = {
  form: UseFormReturn<ChannelMonitorSmartSchedulePolicyFormValues>
  modelOptions: string[]
}

type SmartScheduleMetricGroup = 'smart' | 'ratio'

function GroupPolicyPercentField(props: {
  form: UseFormReturn<ChannelMonitorSmartSchedulePolicyFormValues>
  name:
    | 'scoring.stabilityPercent'
    | 'degradeStabilityScore'
    | 'recoveryStabilityScore'
    | 'fastFailurePenaltyPercent'
    | `scoring.${SmartScheduleMetricGroup}.costRatioPercent`
    | `scoring.${SmartScheduleMetricGroup}.firstTokenPercent`
    | `scoring.${SmartScheduleMetricGroup}.tpsPercent`
  label: string
  helpKey: ChannelMonitorSettingHelpKey
}) {
  const inputId = `channel-monitor-group-policy-${props.name.replaceAll('.', '-')}`
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <ChannelMonitorSettingLabel
            label={props.label}
            helpKey={props.helpKey}
            htmlFor={inputId}
          />
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
          helpKey='costRatioPercent'
        />
        <GroupPolicyPercentField
          form={props.form}
          name={`scoring.${props.group}.firstTokenPercent`}
          label='首字时间'
          helpKey='firstTokenPercent'
        />
        <GroupPolicyPercentField
          form={props.form}
          name={`scoring.${props.group}.tpsPercent`}
          label='TPS'
          helpKey='tpsPercent'
        />
      </div>
    </div>
  )
}

export function ChannelMonitorSmartScheduleGroupPolicyFields(
  props: ChannelMonitorSmartScheduleGroupPolicyFieldsProps
) {
  const modelSelectOptions = useMemo(
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
  const jitterEnabled = useWatch({
    control: props.form.control,
    name: 'jitterEnabled',
  })
  const applyMode = useWatch({
    control: props.form.control,
    name: 'applyMode',
  })
  const sampleMode = useWatch({
    control: props.form.control,
    name: 'sampleMode',
  })
  const selectedModels = useWatch({
    control: props.form.control,
    name: 'models',
  })
  let sampleModeDescription = '不主动补充样本，样本不足的渠道保持当前路由'
  if (sampleMode === 'traffic') {
    sampleModeDescription =
      '从真实业务请求中分配少量流量，逐步补足当前分组和模型的样本'
  } else if (sampleMode === 'probe') {
    sampleModeDescription =
      '仅探测文本模型，固定使用 /v1/responses 流式请求，不改变真实业务请求的路由'
  }

  return (
    <div className='flex flex-col gap-5'>
      <div className='grid gap-4 md:grid-cols-2'>
        <FormField
          control={props.form.control}
          name='strategy'
          render={({ field }) => (
            <FormItem>
              <ChannelMonitorSettingLabel label='调度方式' helpKey='strategy' />
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
              <ChannelMonitorSettingLabel
                label='调整方式'
                helpKey='applyMode'
              />
              <Select
                items={CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS}
                value={field.value}
                onValueChange={(value) => {
                  if (value === null) return
                  field.onChange(value)
                  if (value === 'weight' && sampleMode === 'traffic') {
                    props.form.setValue('sampleMode', 'off', {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                }}
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
            <ChannelMonitorSettingLabel label='参与模型' helpKey='models' />
            <FormControl>
              <MultiSelect
                options={modelSelectOptions}
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

      <FormField
        control={props.form.control}
        name='modelOrder'
        render={({ field }) => (
          <FormItem className='min-w-0'>
            <div className='flex min-h-8 flex-wrap items-center justify-between gap-2'>
              <ChannelMonitorSettingLabel
                label='模型卡片顺序'
                helpKey='modelOrder'
              />
              {field.value.length > 0 ? (
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={() => field.onChange([])}
                >
                  <HugeiconsIcon
                    icon={Refresh01Icon}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                  恢复名称排序
                </Button>
              ) : null}
            </div>
            <ChannelMonitorSmartScheduleModelOrder
              models={
                selectedModels.length > 0 ? selectedModels : props.modelOptions
              }
              value={field.value}
              onChange={field.onChange}
            />
            <FormDescription>
              仅影响智能调度看板展示，不改变模型范围和调度计算
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className='bg-muted/30 flex flex-col gap-4 rounded-md border p-4'>
        <FormField
          control={props.form.control}
          name='sampleMode'
          render={({ field }) => (
            <FormItem>
              <div>
                <ChannelMonitorSettingLabel
                  label='样本补充方式'
                  helpKey='sampleMode'
                />
                <FormDescription className='mt-1'>
                  为样本不足的渠道选择补充数据的方式
                </FormDescription>
              </div>
              <FormControl>
                <ToggleGroup
                  value={[field.value]}
                  onValueChange={(values) => {
                    const value = values.find((item) => item !== field.value)
                    if (
                      value === 'off' ||
                      value === 'traffic' ||
                      value === 'probe'
                    ) {
                      field.onChange(value)
                    }
                  }}
                  variant='outline'
                  spacing={2}
                  aria-label='分组样本补充方式'
                  className='grid w-full grid-cols-3'
                >
                  <ToggleGroupItem value='off' className='w-full'>
                    关闭
                  </ToggleGroupItem>
                  <ToggleGroupItem
                    value='traffic'
                    disabled={applyMode === 'weight'}
                    className='w-full'
                  >
                    探索流量
                  </ToggleGroupItem>
                  <ToggleGroupItem value='probe' className='w-full'>
                    定时探测
                  </ToggleGroupItem>
                </ToggleGroup>
              </FormControl>
              {applyMode === 'weight' ? (
                <FormDescription>
                  “探索流量”需要先将调整方式设为“优先级分层 + 权重”
                </FormDescription>
              ) : null}
              <FormDescription>{sampleModeDescription}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        {sampleMode === 'traffic' ? (
          <FormField
            control={props.form.control}
            name='explorationTrafficPercent'
            render={({ field }) => (
              <FormItem className='max-w-72'>
                <ChannelMonitorSettingLabel
                  label='目标探索流量'
                  helpKey='explorationTraffic'
                />
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={0.1}
                      max={MAX_SMART_SCHEDULE_EXPLORATION_TRAFFIC_PERCENT}
                      step={0.1}
                      inputMode='decimal'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.explorationTrafficPercent
                      )}
                    />
                    <InputGroupAddon align='inline-end'>%</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormDescription>
                  每个分组和模型同时只探索一个渠道，实际比例会受整数权重影响
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        ) : null}

        {sampleMode === 'probe' ? (
          <FormField
            control={props.form.control}
            name='probeIntervalMinutes'
            render={({ field }) => (
              <FormItem className='max-w-72'>
                <ChannelMonitorSettingLabel
                  label='探测间隔'
                  helpKey='probeInterval'
                />
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={1}
                      max={MAX_SMART_SCHEDULE_PROBE_INTERVAL_MINUTES}
                      step={1}
                      inputMode='numeric'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.probeIntervalMinutes
                      )}
                    />
                    <InputGroupAddon align='inline-end'>分钟</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormDescription>
                  非文本模型会跳过；文本探测按配置间隔持续运行并滚动更新窗口内样本
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        ) : null}
      </div>

      <Separator />

      <FormField
        control={props.form.control}
        name='stabilityEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div className='flex flex-col gap-1'>
              <ChannelMonitorSettingLabel
                label='稳定性保护'
                helpKey='stability'
              />
              <FormDescription>
                同时考虑原始成功率与重试失败耗时，快速报错轻罚，慢失败完整惩罚
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
        <div className='flex flex-col gap-4'>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            <GroupPolicyPercentField
              form={props.form}
              name='scoring.stabilityPercent'
              label='稳定性占比'
              helpKey='stabilityPercent'
            />
            <GroupPolicyPercentField
              form={props.form}
              name='degradeStabilityScore'
              label='降级稳定性得分'
              helpKey='degradeStabilityScore'
            />
            <GroupPolicyPercentField
              form={props.form}
              name='recoveryStabilityScore'
              label='恢复稳定性得分'
              helpKey='recoveryStabilityScore'
            />
            <FormField
              control={props.form.control}
              name='minSamples'
              render={({ field }) => (
                <FormItem>
                  <ChannelMonitorSettingLabel
                    label='最少样本'
                    helpKey='minSamples'
                  />
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
          </div>
          <div className='grid items-start gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            <GroupPolicyPercentField
              form={props.form}
              name='fastFailurePenaltyPercent'
              label='快速失败惩罚'
              helpKey='fastFailurePenalty'
            />
            <FormField
              control={props.form.control}
              name='fastFailureSeconds'
              render={({ field }) => (
                <FormItem>
                  <ChannelMonitorSettingLabel
                    label='快速失败界限'
                    helpKey='fastFailureThreshold'
                  />
                  <FormControl>
                    <InputGroup>
                      <InputGroupInput
                        type='number'
                        min={0.1}
                        max={59.9}
                        step={0.1}
                        inputMode='decimal'
                        {...field}
                        aria-invalid={Boolean(
                          props.form.formState.errors.fastFailureSeconds
                        )}
                      />
                      <InputGroupAddon align='inline-end'>秒</InputGroupAddon>
                    </InputGroup>
                  </FormControl>
                  <FormDescription>以内按快速失败惩罚计算</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={props.form.control}
              name='slowFailureSeconds'
              render={({ field }) => (
                <FormItem>
                  <ChannelMonitorSettingLabel
                    label='慢失败界限'
                    helpKey='slowFailureThreshold'
                  />
                  <FormControl>
                    <InputGroup>
                      <InputGroupInput
                        type='number'
                        min={0.1}
                        max={60}
                        step={0.1}
                        inputMode='decimal'
                        {...field}
                        aria-invalid={Boolean(
                          props.form.formState.errors.slowFailureSeconds
                        )}
                      />
                      <InputGroupAddon align='inline-end'>秒</InputGroupAddon>
                    </InputGroup>
                  </FormControl>
                  <FormDescription>达到后按完整失败计算</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={props.form.control}
              name='cooldownMinutes'
              render={({ field }) => (
                <FormItem>
                  <ChannelMonitorSettingLabel
                    label='降级时长'
                    helpKey='cooldown'
                  />
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
          <div className='border-border/60 bg-muted/30 flex flex-col gap-4 rounded-md border p-4'>
            <FormField
              control={props.form.control}
              name='jitterEnabled'
              render={({ field }) => (
                <FormItem className='flex items-center justify-between gap-4'>
                  <div className='flex flex-col gap-1'>
                    <ChannelMonitorSettingLabel
                      label='成功延迟抖动'
                      helpKey='jitter'
                    />
                    <FormDescription>
                      允许偶发的慢成功；超过容忍范围的慢请求会降低稳定性得分
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      aria-label='成功延迟抖动'
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            {jitterEnabled ? (
              <>
                <div className='grid items-start gap-4 sm:grid-cols-2 lg:grid-cols-4'>
                  <FormField
                    control={props.form.control}
                    name='jitterTolerancePercent'
                    render={({ field }) => (
                      <FormItem>
                        <ChannelMonitorSettingLabel
                          label='允许抖动'
                          helpKey='jitterTolerance'
                        />
                        <FormControl>
                          <InputGroup>
                            <InputGroupInput
                              type='number'
                              min={0}
                              max={MAX_SMART_SCHEDULE_JITTER_TOLERANCE_PERCENT}
                              step={0.1}
                              inputMode='decimal'
                              value={field.value}
                              onBlur={field.onBlur}
                              onChange={field.onChange}
                              name={field.name}
                              ref={field.ref}
                              aria-invalid={Boolean(
                                props.form.formState.errors
                                  .jitterTolerancePercent
                              )}
                            />
                            <InputGroupAddon align='inline-end'>
                              %
                            </InputGroupAddon>
                          </InputGroup>
                        </FormControl>
                        <FormDescription>
                          慢成功免罚比例，窗口内至少容忍 1 次
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={props.form.control}
                    name='jitterThresholdMultiplier'
                    render={({ field }) => (
                      <FormItem>
                        <ChannelMonitorSettingLabel
                          label='判定倍率'
                          helpKey='jitterMultiplier'
                        />
                        <FormControl>
                          <InputGroup>
                            <InputGroupInput
                              type='number'
                              min={1.01}
                              max={
                                MAX_SMART_SCHEDULE_JITTER_THRESHOLD_MULTIPLIER
                              }
                              step={0.1}
                              inputMode='decimal'
                              value={field.value}
                              onBlur={field.onBlur}
                              onChange={field.onChange}
                              name={field.name}
                              ref={field.ref}
                              aria-invalid={Boolean(
                                props.form.formState.errors
                                  .jitterThresholdMultiplier
                              )}
                            />
                            <InputGroupAddon align='inline-end'>
                              倍
                            </InputGroupAddon>
                          </InputGroup>
                        </FormControl>
                        <FormDescription>相对基线的慢请求界限</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={props.form.control}
                    name='jitterAbsoluteToleranceSeconds'
                    render={({ field }) => (
                      <FormItem>
                        <ChannelMonitorSettingLabel
                          label='绝对容差'
                          helpKey='jitterAbsoluteTolerance'
                        />
                        <FormControl>
                          <InputGroup>
                            <InputGroupInput
                              type='number'
                              min={0}
                              max={
                                MAX_SMART_SCHEDULE_JITTER_ABSOLUTE_TOLERANCE_SECONDS
                              }
                              step={0.1}
                              inputMode='decimal'
                              value={field.value}
                              onBlur={field.onBlur}
                              onChange={field.onChange}
                              name={field.name}
                              ref={field.ref}
                              aria-invalid={Boolean(
                                props.form.formState.errors
                                  .jitterAbsoluteToleranceSeconds
                              )}
                            />
                            <InputGroupAddon align='inline-end'>
                              秒
                            </InputGroupAddon>
                          </InputGroup>
                        </FormControl>
                        <FormDescription>
                          允许高于基线的固定延迟
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={props.form.control}
                    name='jitterBaselineMinutes'
                    render={({ field }) => (
                      <FormItem>
                        <ChannelMonitorSettingLabel
                          label='基线学习周期'
                          helpKey='jitterBaseline'
                        />
                        <FormControl>
                          <InputGroup>
                            <InputGroupInput
                              type='number'
                              min={1}
                              max={MAX_SMART_SCHEDULE_JITTER_BASELINE_MINUTES}
                              step={1}
                              inputMode='numeric'
                              value={field.value}
                              onBlur={field.onBlur}
                              onChange={field.onChange}
                              name={field.name}
                              ref={field.ref}
                              aria-invalid={Boolean(
                                props.form.formState.errors
                                  .jitterBaselineMinutes
                              )}
                            />
                            <InputGroupAddon align='inline-end'>
                              分钟
                            </InputGroupAddon>
                          </InputGroup>
                        </FormControl>
                        <FormDescription>平滑学习近期首字 P50</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
                <p className='text-muted-foreground text-xs'>
                  慢请求阈值取“基线 × 判定倍率”和“基线 + 绝对容差”中的较大值
                </p>
              </>
            ) : null}
          </div>
        </div>
      )}

      <Separator />

      <div className='flex flex-col gap-4'>
        <div>
          <h3 className='text-sm font-medium'>评分与流量</h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            当前分组的评分参数、主渠道流量和切换规则
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

        <div className='grid items-start gap-4 sm:grid-cols-2'>
          {applyMode === 'weight' ? (
            <FormField
              control={props.form.control}
              name='scoring.primaryTrafficPercent'
              render={({ field }) => (
                <FormItem>
                  <ChannelMonitorSettingLabel
                    label='主渠道目标流量'
                    helpKey='primaryTraffic'
                  />
                  <FormControl>
                    <InputGroup>
                      <InputGroupInput
                        type='number'
                        min={MIN_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT}
                        max={MAX_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT}
                        step={0.1}
                        inputMode='decimal'
                        value={field.value}
                        onBlur={field.onBlur}
                        onChange={field.onChange}
                        name={field.name}
                        ref={field.ref}
                        aria-invalid={Boolean(
                          props.form.formState.errors.scoring
                            ?.primaryTrafficPercent
                        )}
                      />
                      <InputGroupAddon align='inline-end'>%</InputGroupAddon>
                    </InputGroup>
                  </FormControl>
                  <FormDescription>
                    仅调整权重时，最高分渠道的目标流量占比
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          <FormField
            control={props.form.control}
            name='scoring.primarySwitchThresholdPercent'
            render={({ field }) => (
              <FormItem>
                <ChannelMonitorSettingLabel
                  label='主渠道切换分差'
                  helpKey='primarySwitchThreshold'
                />
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      type='number'
                      min={0}
                      max={MAX_SMART_SCHEDULE_PRIMARY_SWITCH_THRESHOLD_PERCENT}
                      step={0.1}
                      inputMode='decimal'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                      aria-invalid={Boolean(
                        props.form.formState.errors.scoring
                          ?.primarySwitchThresholdPercent
                      )}
                    />
                    <InputGroupAddon align='inline-end'>%</InputGroupAddon>
                  </InputGroup>
                </FormControl>
                <FormDescription>
                  新渠道至少领先当前主渠道这些百分点才切换
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      </div>
    </div>
  )
}
