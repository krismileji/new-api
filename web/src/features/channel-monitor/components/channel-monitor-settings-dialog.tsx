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
import { zodResolver } from '@hookform/resolvers/zod'
import { Route01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm, type Resolver, type UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { JsonEditor } from '@/components/json-editor'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { IconBadge } from '@/components/ui/icon-badge'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { updateChannelMonitorSettings } from '../api'
import {
  handleChannelMonitorMutationError,
  shouldReloadChannelMonitorSettings,
} from '../lib/error'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
  CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
} from '../lib/query-options'
import {
  createChannelMonitorSettingsSchema,
  DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  DEFAULT_PROBE_RESPONSE_CACHE_WRITE_TOKENS,
  DEFAULT_PROBE_RESPONSE_CACHED_TOKENS,
  DEFAULT_PROBE_RESPONSE_INPUT_TOKENS,
  DEFAULT_PROBE_RESPONSE_MATCH_INPUT,
  DEFAULT_PROBE_RESPONSE_MAX_DELAY_MS,
  DEFAULT_PROBE_RESPONSE_MIN_DELAY_MS,
  DEFAULT_PROBE_RESPONSE_OUTPUT_TOKENS,
  DEFAULT_PROBE_RESPONSE_TEXT,
  DEFAULT_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
  MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MAX_AUTO_UPDATE_INTERVAL_MINUTES,
  MAX_AUTO_UPDATE_RETRY_COUNT,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  type ChannelMonitorSettingsFormValues,
} from '../lib/schema'
import {
  createChannelMonitorSettingsUpdatePayload,
  type ChannelMonitorSettingsUpdateMode,
} from '../lib/settings-update'
import { channelMonitorSmartScheduleGroupPoliciesToForm } from '../lib/smart-schedule-group-policy'
import type { ChannelMonitorSettings } from '../types'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { ChannelMonitorEmailNotificationFields } from './channel-monitor-email-notification-fields'
import { ChannelMonitorProbeResponseFields } from './channel-monitor-probe-response-fields'
import { ChannelMonitorSmartScheduleFields } from './channel-monitor-smart-schedule-fields'

export type ChannelMonitorSettingsSection = 'monitor' | 'probe'

type ChannelMonitorSettingsDialogProps = {
  settings: ChannelMonitorSettings
  initialSection?: ChannelMonitorSettingsSection
  open: boolean
  onOpenChange: (open: boolean) => void
}

type ChannelMonitorSmartScheduleSettingsSheetProps = {
  settings: ChannelMonitorSettings
  modelOptionsByGroup: ReadonlyMap<string, string[]>
  groupOptions: string[]
  open: boolean
  onOpenChange: (open: boolean) => void
  onOpenChangeComplete?: (open: boolean) => void
}

type ChannelMonitorSettingsFormProps = {
  settings: ChannelMonitorSettings
  modelOptionsByGroup: ReadonlyMap<string, string[]>
  groupOptions: string[]
  initialSection: ChannelMonitorSettingsSection
  mode: ChannelMonitorSettingsUpdateMode
  open: boolean
  onOpenChange: (open: boolean) => void
  onOpenChangeComplete?: (open: boolean) => void
}

const EMPTY_OPTIONS: string[] = []
const EMPTY_MODEL_OPTIONS_BY_GROUP: ReadonlyMap<string, string[]> = new Map()

type ChannelMonitorRetentionFieldName =
  | 'costRetentionDays'
  | 'executionDetailRetentionDays'
  | 'taskRetentionDays'
  | 'ratioHistoryRetentionDays'

function ChannelMonitorRetentionDayField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  name: ChannelMonitorRetentionFieldName
  label: string
  description: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <InputGroup>
            <FormControl>
              <InputGroupInput
                type='number'
                min={MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS}
                max={MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(props.form.formState.errors[props.name])}
              />
            </FormControl>
            <InputGroupAddon align='inline-end'>天</InputGroupAddon>
          </InputGroup>
          <FormDescription>{props.description}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorCostRetentionField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <ChannelMonitorRetentionDayField
      form={props.form}
      name='costRetentionDays'
      label='成本与指标保留天数'
      description='保留分钟指标、延迟分桶及渠道和 API Key 日成本'
    />
  )
}

export function ChannelMonitorRetentionFields(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <section className='space-y-4' aria-labelledby='channel-monitor-retention'>
      <div className='space-y-1'>
        <h3 id='channel-monitor-retention' className='text-sm font-medium'>
          数据保留
        </h3>
        <p className='text-muted-foreground text-sm'>
          每天按北京时间分批清理到期数据；删除后不可恢复
        </p>
      </div>
      <div className='grid gap-4 sm:grid-cols-2'>
        <ChannelMonitorCostRetentionField form={props.form} />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='executionDetailRetentionDays'
          label='调度执行明细保留天数'
          description='保留每次自动调度的渠道评分、采样与决策明细'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='taskRetentionDays'
          label='监控任务保留天数'
          description='不能短于调度执行明细；仅清理已结束任务，各类始终保留最近 100 条'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='ratioHistoryRetentionDays'
          label='倍率历史保留天数'
          description='保留渠道成本倍率的历史变更记录'
        />
      </div>
    </section>
  )
}

export function ChannelMonitorConsecutiveFailureLimitField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='autoUpdateConsecutiveFailureLimit'
      render={({ field }) => (
        <FormItem>
          <FormLabel>连续失败停止次数</FormLabel>
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT}
                max={MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors.autoUpdateConsecutiveFailureLimit
                )}
              />
              <InputGroupAddon align='inline-end'>次</InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormDescription>
            倍率和余额分别连续失败达到该次数后停止自动更新；手动更新成功后恢复
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorUpstreamRequestTimeoutField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='upstreamRequestTimeoutSeconds'
      render={({ field }) => (
        <FormItem>
          <FormLabel>上游请求超时</FormLabel>
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS}
                max={MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors.upstreamRequestTimeoutSeconds
                )}
              />
              <InputGroupAddon align='inline-end'>秒</InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormDescription>
            单次倍率或余额更新超过该时间会终止；自动更新随后按失败重试规则处理
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorSettingsDialog(
  props: ChannelMonitorSettingsDialogProps
) {
  return (
    <ChannelMonitorSettingsForm
      {...props}
      modelOptionsByGroup={EMPTY_MODEL_OPTIONS_BY_GROUP}
      groupOptions={EMPTY_OPTIONS}
      initialSection={props.initialSection ?? 'monitor'}
      mode='general'
    />
  )
}

export function ChannelMonitorSmartScheduleSettingsSheet(
  props: ChannelMonitorSmartScheduleSettingsSheetProps
) {
  return (
    <ChannelMonitorSettingsForm
      {...props}
      initialSection='monitor'
      mode='schedule'
    />
  )
}

function ChannelMonitorSettingsForm(props: ChannelMonitorSettingsFormProps) {
  const queryClient = useQueryClient()
  const form = useForm<ChannelMonitorSettingsFormValues>({
    resolver: zodResolver(
      createChannelMonitorSettingsSchema()
    ) as Resolver<ChannelMonitorSettingsFormValues>,
    defaultValues: {
      autoUpdateIntervalMinutes: props.settings.auto_update_interval_minutes,
      autoUpdateRetryCount: props.settings.auto_update_retry_count,
      upstreamRequestTimeoutSeconds:
        props.settings.upstream_request_timeout_seconds ??
        DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
      autoUpdateConsecutiveFailureLimit:
        props.settings.auto_update_consecutive_failure_limit ??
        DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
      autoDisableOnUpdateFailure:
        props.settings.auto_disable_on_update_failure ?? false,
      autoEnableOnCostRatioRecovery:
        props.settings.auto_enable_on_cost_ratio_recovery ?? false,
      autoEnableOnBalanceRecovery:
        props.settings.auto_enable_on_balance_recovery ?? false,
      costRetentionDays:
        props.settings.cost_retention_days ??
        DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
      executionDetailRetentionDays:
        props.settings.execution_detail_retention_days ??
        DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS,
      taskRetentionDays:
        props.settings.task_retention_days ??
        DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS,
      ratioHistoryRetentionDays:
        props.settings.ratio_history_retention_days ??
        DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS,
      emailNotificationEnabled: props.settings.email_notification_enabled,
      notificationEmail: props.settings.notification_email,
      emailNotificationTypes: props.settings.email_notification_types,
      errorMessageMapping: props.settings.error_message_mapping ?? '',
      probeResponseEnabled: props.settings.probe_response_enabled ?? false,
      probeResponseMatchInput:
        props.settings.probe_response_match_input ??
        DEFAULT_PROBE_RESPONSE_MATCH_INPUT,
      probeResponseText:
        props.settings.probe_response_text ?? DEFAULT_PROBE_RESPONSE_TEXT,
      probeResponseMinDelayMs:
        props.settings.probe_response_min_delay_ms ??
        DEFAULT_PROBE_RESPONSE_MIN_DELAY_MS,
      probeResponseMaxDelayMs:
        props.settings.probe_response_max_delay_ms ??
        DEFAULT_PROBE_RESPONSE_MAX_DELAY_MS,
      probeResponseInputTokens:
        props.settings.probe_response_input_tokens ??
        DEFAULT_PROBE_RESPONSE_INPUT_TOKENS,
      probeResponseCacheWriteTokens:
        props.settings.probe_response_cache_write_tokens ??
        DEFAULT_PROBE_RESPONSE_CACHE_WRITE_TOKENS,
      probeResponseCachedTokens:
        props.settings.probe_response_cached_tokens ??
        DEFAULT_PROBE_RESPONSE_CACHED_TOKENS,
      probeResponseOutputTokens:
        props.settings.probe_response_output_tokens ??
        DEFAULT_PROBE_RESPONSE_OUTPUT_TOKENS,
      relayResponseHeaderTimeoutSeconds:
        props.settings.relay_response_header_timeout_seconds ?? 0,
      smartScheduleEnabled: props.settings.smart_schedule_enabled,
      smartScheduleGroupPolicies:
        channelMonitorSmartScheduleGroupPoliciesToForm(
          props.settings.smart_schedule_group_policies
        ),
      smartScheduleIntervalMinutes:
        props.settings.smart_schedule_interval_minutes,
      smartSchedulePerformanceWindowMinutes:
        props.settings.smart_schedule_performance_window_minutes,
      smartScheduleStabilityWindowMinutes:
        props.settings.smart_schedule_stability_window_minutes,
      smartScheduleRateLimitCooldownSeconds:
        props.settings.smart_schedule_rate_limit_cooldown_seconds ??
        DEFAULT_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
      smartScheduleForceReset: false,
    },
  })
  const mutation = useMutation({
    mutationFn: updateChannelMonitorSettings,
    onError: (error) => {
      handleChannelMonitorMutationError(error)
      if (!shouldReloadChannelMonitorSettings(error)) return
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
      })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      })
      props.onOpenChange(false)
    },
    onSuccess: (response) => {
      if (response.data.smart_schedule_force_reset_task_error) {
        toast.error(
          `设置已保存，但无法创建重算任务：${response.data.smart_schedule_force_reset_task_error}`
        )
      } else if (
        response.data.smart_schedule_force_reset_task_created === true
      ) {
        toast.success('设置已保存，强制重算任务已创建')
      } else if (
        response.data.smart_schedule_force_reset_task_created === false
      ) {
        toast.warning(
          '设置已保存，但已有智能调度任务正在运行，本次强制重算未排队'
        )
      } else {
        toast.success(
          props.mode === 'schedule'
            ? '智能调度设置已保存'
            : '渠道监控设置已保存'
        )
      }
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
      })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      })
      props.onOpenChange(false)
    },
  })
  const handleSubmit = form.handleSubmit((values) => {
    mutation.mutate(
      createChannelMonitorSettingsUpdatePayload(
        props.mode,
        values,
        props.settings.smart_schedule_control_revision
      )
    )
  })

  if (props.mode === 'schedule') {
    return (
      <Sheet
        open={props.open}
        onOpenChange={(open) => {
          if (!open && mutation.isPending) return
          props.onOpenChange(open)
        }}
        onOpenChangeComplete={props.onOpenChangeComplete}
      >
        <SheetContent
          className={sideDrawerContentClassName('sm:max-w-[72em]')}
          showCloseButton={!mutation.isPending}
        >
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle className='flex items-center gap-3'>
              <IconBadge tone='info' size='title'>
                <HugeiconsIcon icon={Route01Icon} aria-hidden='true' />
              </IconBadge>
              <span>智能调度设置</span>
            </SheetTitle>
            <SheetDescription className='mt-1'>
              按分组配置独立的调度方式、模型范围和稳定性规则
            </SheetDescription>
          </SheetHeader>

          <Form {...form}>
            <form
              id='channel-monitor-smart-schedule-settings-form'
              className={sideDrawerFormClassName(
                'min-w-0 gap-5 overflow-x-hidden'
              )}
              onSubmit={handleSubmit}
            >
              <ChannelMonitorSmartScheduleFields
                form={form}
                modelOptionsByGroup={props.modelOptionsByGroup}
                groupOptions={props.groupOptions}
              />
            </form>
          </Form>

          <SheetFooter className={sideDrawerFooterClassName()}>
            <SheetClose
              render={
                <Button variant='outline' disabled={mutation.isPending} />
              }
            >
              取消
            </SheetClose>
            <Button
              form='channel-monitor-smart-schedule-settings-form'
              type='submit'
              disabled={mutation.isPending}
            >
              {mutation.isPending && <Spinner data-icon='inline-start' />}
              保存
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && mutation.isPending) return
        props.onOpenChange(open)
      }}
    >
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'grid-rows-[auto_minmax(0,1fr)] sm:max-w-2xl'
        )}
        showCloseButton={!mutation.isPending}
      >
        <DialogHeader>
          <DialogTitle>渠道监控设置</DialogTitle>
          <DialogDescription>
            设置上游倍率更新、通知、错误信息和探针响应规则
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form className='flex min-h-0 flex-col gap-5' onSubmit={handleSubmit}>
            <Tabs
              defaultValue={props.initialSection}
              className='min-h-0 flex-1 gap-5'
            >
              <TabsList className='grid h-auto w-full shrink-0 grid-cols-2'>
                <TabsTrigger value='monitor' className='h-auto px-2 text-wrap'>
                  倍率、通知与错误
                </TabsTrigger>
                <TabsTrigger value='probe'>探针响应</TabsTrigger>
              </TabsList>

              <TabsContent
                value='monitor'
                className='mt-0 flex min-h-0 flex-col gap-5 overflow-y-auto pr-1'
              >
                <FormField
                  control={form.control}
                  name='autoUpdateIntervalMinutes'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>更新间隔</FormLabel>
                      <FormControl>
                        <InputGroup>
                          <InputGroupInput
                            type='number'
                            min={0}
                            max={MAX_AUTO_UPDATE_INTERVAL_MINUTES}
                            step={1}
                            inputMode='numeric'
                            value={field.value}
                            onBlur={field.onBlur}
                            onChange={field.onChange}
                            name={field.name}
                            ref={field.ref}
                            aria-invalid={Boolean(
                              form.formState.errors.autoUpdateIntervalMinutes
                            )}
                          />
                          <InputGroupAddon align='inline-end'>
                            分钟
                          </InputGroupAddon>
                        </InputGroup>
                      </FormControl>
                      <FormDescription>
                        设置为 0 时关闭自动更新；保存后自动生效
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='autoUpdateRetryCount'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>失败重试次数</FormLabel>
                      <FormControl>
                        <InputGroup>
                          <InputGroupInput
                            type='number'
                            min={0}
                            max={MAX_AUTO_UPDATE_RETRY_COUNT}
                            step={1}
                            inputMode='numeric'
                            value={field.value}
                            onBlur={field.onBlur}
                            onChange={field.onChange}
                            name={field.name}
                            ref={field.ref}
                            aria-invalid={Boolean(
                              form.formState.errors.autoUpdateRetryCount
                            )}
                          />
                          <InputGroupAddon align='inline-end'>
                            次
                          </InputGroupAddon>
                        </InputGroup>
                      </FormControl>
                      <FormDescription>
                        首次失败后最多再尝试的次数；设置为 0 时不重试
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <ChannelMonitorUpstreamRequestTimeoutField form={form} />

                <ChannelMonitorConsecutiveFailureLimitField form={form} />

                <FormField
                  control={form.control}
                  name='autoDisableOnUpdateFailure'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4'>
                      <div className='space-y-1'>
                        <FormLabel>更新失败自动禁用渠道</FormLabel>
                        <FormDescription>
                          开启后，倍率或余额更新在重试后仍失败时自动禁用对应渠道
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          aria-label='更新失败自动禁用渠道'
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='autoEnableOnCostRatioRecovery'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4'>
                      <div className='space-y-1'>
                        <FormLabel>成本倍率恢复后自动启用渠道</FormLabel>
                        <FormDescription>
                          开启后，因成本倍率过高被系统禁用的渠道，在按分组系数换算后严格低于全部所属分组倍率时自动启用
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          aria-label='成本倍率恢复后自动启用渠道'
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='autoEnableOnBalanceRecovery'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4'>
                      <div className='space-y-1'>
                        <FormLabel>余额恢复后自动启用渠道</FormLabel>
                        <FormDescription>
                          开启后，因余额低于阈值被系统禁用的渠道，在余额恢复且按分组系数换算后的成本倍率不高于全部所属分组倍率时自动启用
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          aria-label='余额恢复后自动启用渠道'
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                <ChannelMonitorRetentionFields form={form} />

                <ChannelMonitorEmailNotificationFields form={form} />

                <FormField
                  control={form.control}
                  name='errorMessageMapping'
                  render={({ field }) => (
                    <FormItem className='space-y-3 border-t pt-4'>
                      <div className='space-y-1'>
                        <FormLabel>错误信息映射</FormLabel>
                        <FormDescription>
                          统一应用于所有渠道的用户请求和使用日志；按上游错误码优先、最终
                          HTTP 状态码其次匹配，用户请求仅在响应尚未开始时替换
                        </FormDescription>
                      </div>
                      <FormControl>
                        <JsonEditor
                          value={field.value || ''}
                          onChange={field.onChange}
                          disabled={mutation.isPending}
                          keyPlaceholder='429 或 insufficient_quota'
                          valuePlaceholder='请求过于频繁，请稍后再试'
                          keyLabel='错误码 / 状态码'
                          valueLabel='用户可见信息'
                          emptyMessage='未配置错误信息映射。'
                          template={{
                            '429': '请求过于频繁，请稍后再试',
                            insufficient_quota: '额度不足，请联系管理员',
                          }}
                          valueType='string'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </TabsContent>

              <TabsContent
                value='probe'
                className='mt-0 min-h-0 overflow-y-auto pr-1'
              >
                <ChannelMonitorProbeResponseFields form={form} />
              </TabsContent>
            </Tabs>

            <DialogFooter className='shrink-0'>
              <Button
                type='button'
                variant='outline'
                onClick={() => props.onOpenChange(false)}
                disabled={mutation.isPending}
              >
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending && <Spinner data-icon='inline-start' />}
                保存
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
