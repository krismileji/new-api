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
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  MAX_AUTO_UPDATE_INTERVAL_MINUTES,
  MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
  MAX_SMART_SCHEDULE_COOLDOWN_MINUTES,
  MAX_SMART_SCHEDULE_MIN_SAMPLES,
  type ChannelMonitorSettingsFormValues,
} from '../lib/schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS,
  CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS,
} from '../lib/smart-schedule-options'
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

type SmartScheduleMetricGroup = 'smart' | 'ratio'

function SmartSchedulePercentField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  name:
    | 'smartScheduleScoring.stabilityPercent'
    | 'smartScheduleScoring.relativeWeightStartPercent'
    | 'smartScheduleScoring.relativeWeightFullPercent'
    | `smartScheduleScoring.${SmartScheduleMetricGroup}.costRatioPercent`
    | `smartScheduleScoring.${SmartScheduleMetricGroup}.firstTokenPercent`
    | `smartScheduleScoring.${SmartScheduleMetricGroup}.tpsPercent`
  label: string
}) {
  const inputId = `channel-monitor-${props.name.replaceAll('.', '-')}`
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

function SmartScheduleMetricPercentageFields(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  group: SmartScheduleMetricGroup
  title: string
}) {
  const percentages = useWatch({
    control: props.form.control,
    name: `smartScheduleScoring.${props.group}`,
  })
  const total =
    Number(percentages.costRatioPercent) +
    Number(percentages.firstTokenPercent) +
    Number(percentages.tpsPercent)

  return (
    <fieldset className='space-y-3 border-t pt-4'>
      <legend className='text-sm font-medium'>{props.title}</legend>
      <p className='text-muted-foreground text-sm'>
        三项合计必须为 100%；开启稳定性后，本组指标共同使用剩余占比。当前合计：
        {total}%
      </p>
      <div className='grid gap-4 sm:grid-cols-3'>
        <SmartSchedulePercentField
          form={props.form}
          name={`smartScheduleScoring.${props.group}.costRatioPercent`}
          label='成本倍率'
        />
        <SmartSchedulePercentField
          form={props.form}
          name={`smartScheduleScoring.${props.group}.firstTokenPercent`}
          label='首字时间'
        />
        <SmartSchedulePercentField
          form={props.form}
          name={`smartScheduleScoring.${props.group}.tpsPercent`}
          label='TPS'
        />
      </div>
    </fieldset>
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
          <FormLabel>上游响应等待时间</FormLabel>
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
          <FormDescription>
            0
            表示不限制；超时后按现有重试规则处理，收到响应头后停止计时，不限制后续流式输出
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
  const modelOptions = useMemo(
    () => props.modelOptions.map((model) => ({ value: model, label: model })),
    [props.modelOptions]
  )
  const groupOptions = useMemo(
    () => props.groupOptions.map((group) => ({ value: group, label: group })),
    [props.groupOptions]
  )
  const stabilityEnabled = useWatch({
    control: props.form.control,
    name: 'smartScheduleStabilityEnabled',
  })
  const strategy = useWatch({
    control: props.form.control,
    name: 'smartScheduleStrategy',
  })
  const stabilityPercent = useWatch({
    control: props.form.control,
    name: 'smartScheduleScoring.stabilityPercent',
  })
  const relativeWeightEnabled = useWatch({
    control: props.form.control,
    name: 'smartScheduleScoring.relativeWeightEnabled',
  })

  return (
    <div className='flex flex-col gap-5'>
      <FormField
        control={props.form.control}
        name='smartScheduleEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div className='flex flex-col gap-1'>
              <FormLabel>智能调度</FormLabel>
              <FormDescription>
                按分组和模型分别调整参与路由的优先级、权重
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

      <div className='grid items-start gap-4'>
        <FormField
          control={props.form.control}
          name='smartScheduleGroups'
          render={({ field }) => (
            <FormItem className='min-w-0'>
              <FormLabel>参与分组</FormLabel>
              <FormControl>
                <MultiSelect
                  options={groupOptions}
                  selected={field.value}
                  onChange={field.onChange}
                  placeholder='全部分组'
                  emptyText='没有匹配的分组'
                  className='h-8 min-h-8 flex-nowrap overflow-hidden'
                  renderSelectedSummary={
                    field.value.length > 0
                      ? (values) => `已选择 ${values.length} 个分组`
                      : undefined
                  }
                />
              </FormControl>
              <FormDescription>
                不选择表示全部分组；每个分组和模型形成独立调度池
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>

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
            对全部分组统一生效，不随分组策略变化
          </p>
        </div>
        <div className='grid gap-4 md:grid-cols-2 lg:grid-cols-3'>
          <FormField
            control={props.form.control}
            name='smartScheduleIntervalMinutes'
            render={({ field }) => (
              <FormItem>
                <FormLabel>调度间隔</FormLabel>
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
                <FormLabel>统计范围</FormLabel>
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

      <Tabs defaultValue='default-policy' className='gap-4'>
        <TabsList className='grid h-auto w-full grid-cols-2'>
          <TabsTrigger value='default-policy'>默认策略</TabsTrigger>
          <TabsTrigger value='group-policies'>分组策略</TabsTrigger>
        </TabsList>

        <TabsContent
          value='default-policy'
          className='mt-0 flex flex-col gap-5'
        >
          <FormField
            control={props.form.control}
            name='smartScheduleStrategy'
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
            name='smartScheduleStabilityEnabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <div className='flex flex-col gap-1'>
                  <FormLabel>稳定性保护</FormLabel>
                  <FormDescription>
                    启用后占最终得分的 {stabilityPercent}
                    %，同时负责准入、降级和恢复
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    aria-label='稳定性保护'
                  />
                </FormControl>
              </FormItem>
            )}
          />

          {strategy === 'smart' && (
            <SmartScheduleMetricPercentageFields
              form={props.form}
              group='smart'
              title='智能调度指标占比'
            />
          )}

          {strategy === 'ratio' && (
            <SmartScheduleMetricPercentageFields
              form={props.form}
              group='ratio'
              title='按成本倍率调度指标占比'
            />
          )}

          <FormField
            control={props.form.control}
            name='smartScheduleScoring.curveExponent'
            render={({ field }) => (
              <FormItem>
                <FormLabel htmlFor='channel-monitor-smart-schedule-curve-exponent'>
                  得分曲线指数
                </FormLabel>
                <FormControl>
                  <InputGroup>
                    <InputGroupInput
                      id='channel-monitor-smart-schedule-curve-exponent'
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
                        props.form.formState.errors.smartScheduleScoring
                          ?.curveExponent
                      )}
                    />
                  </InputGroup>
                </FormControl>
                <FormDescription>
                  1 为线性；大于 1 会进一步压低中低分渠道。相对拉伸也使用该指数
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={props.form.control}
            name='smartScheduleScoring.relativeWeightEnabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4 border-t pt-4'>
                <div className='flex flex-col gap-1'>
                  <FormLabel>相对权重拉伸</FormLabel>
                  <FormDescription>
                    根据同一调度组内的得分范围渐进拉开权重，减少相近评分渠道平均分流
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    aria-label='相对权重拉伸'
                  />
                </FormControl>
              </FormItem>
            )}
          />

          {relativeWeightEnabled ? (
            <div className='space-y-3'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <SmartSchedulePercentField
                  form={props.form}
                  name='smartScheduleScoring.relativeWeightStartPercent'
                  label='开始拉伸分差'
                />
                <SmartSchedulePercentField
                  form={props.form}
                  name='smartScheduleScoring.relativeWeightFullPercent'
                  label='完整拉伸分差'
                />
              </div>
              <p className='text-muted-foreground text-sm'>
                低于开始值保持绝对得分映射；达到完整值时，组内最低和最高得分映射到权重
                10 和 100，中间按比例混合。开始值设为 0%
                时，任何分差都会开始拉伸
              </p>
            </div>
          ) : null}

          {stabilityEnabled && (
            <div className='grid gap-4 sm:grid-cols-2'>
              <SmartSchedulePercentField
                form={props.form}
                name='smartScheduleScoring.stabilityPercent'
                label='稳定性占比'
              />
              <FormField
                control={props.form.control}
                name='smartScheduleMinSuccessRate'
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
                            props.form.formState.errors
                              .smartScheduleMinSuccessRate
                          )}
                        />
                        <InputGroupAddon align='inline-end'>%</InputGroupAddon>
                      </InputGroup>
                    </FormControl>
                    <FormDescription>
                      样本达到要求且低于该值时降为优先级 0、权重 0
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={props.form.control}
                name='smartScheduleCooldownMinutes'
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
                            props.form.formState.errors
                              .smartScheduleCooldownMinutes
                          )}
                        />
                        <InputGroupAddon align='inline-end'>
                          分钟
                        </InputGroupAddon>
                      </InputGroup>
                    </FormControl>
                    <FormDescription>
                      到期后恢复原优先级，以不超过 10 的小流量权重用新样本试放
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          )}

          <FormField
            control={props.form.control}
            name='smartScheduleApplyMode'
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

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={props.form.control}
              name='smartScheduleMinSamples'
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
                          props.form.formState.errors.smartScheduleMinSamples
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

          <FormField
            control={props.form.control}
            name='smartScheduleModels'
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
                <FormDescription>
                  不选择表示全部模型；每个分组和模型独立调度，选择顺序不生效
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <Alert>
            <AlertTitle>调度规则</AlertTitle>
            <AlertDescription>
              调度得分按上方百分比计算，得分曲线指数决定渠道差距的放大程度；开启稳定性后，成功率按配置占比参与综合得分。稳定性按成功调用数
              ÷（成功调用数 +
              渠道错误数）计算，重试中的渠道错误也会计入；样本达到要求且低于最低成功率时降为优先级
              0、权重 0，冷却到期后恢复原优先级，以不超过 10
              的小流量权重只用新样本试放；达标后再恢复完整权重。指标样本不足的渠道使用优先级
              80、权重 10 进行探索。稳定性保护需要同时开启消费日志和
              ERROR_LOG_ENABLED。
            </AlertDescription>
          </Alert>
        </TabsContent>

        <TabsContent value='group-policies' className='mt-0'>
          <ChannelMonitorSmartScheduleGroupPolicies
            form={props.form}
            groupOptions={props.groupOptions}
            modelOptions={props.modelOptions}
          />
        </TabsContent>
      </Tabs>

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
              <FormLabel htmlFor='channel-monitor-force-smart-schedule-reset'>
                强制重置优先级和权重
              </FormLabel>
              <FormDescription>
                保存后，根据当前日志重新计算所有符合条件的参与渠道，并立即应用优先级和权重。此操作仅执行一次。
              </FormDescription>
            </div>
          </FormItem>
        )}
      />
    </div>
  )
}
