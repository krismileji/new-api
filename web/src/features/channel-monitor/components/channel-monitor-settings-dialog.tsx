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
import { useState } from 'react'
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
import { Textarea } from '@/components/ui/textarea'
import { LogicalGroupsPanel } from '@/features/channels/components/dialogs/logical-groups-dialog'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

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
  DEFAULT_AUTO_UPDATE_RETRY_DELAY_SECONDS,
  DEFAULT_CHANNEL_CONCURRENCY_WAIT_SECONDS,
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
  MAX_AUTO_UPDATE_RETRY_DELAY_SECONDS,
  MAX_CHANNEL_CONCURRENCY_WAIT_SECONDS,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS as MAX_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS as MAX_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS as MAX_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  MAX_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  MAX_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  MAX_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS as MAX_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS as MAX_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS as MAX_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MIN_CHANNEL_CONCURRENCY_WAIT_SECONDS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS as MIN_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS as MIN_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS as MIN_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  MIN_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  MIN_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  MIN_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS as MIN_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS as MIN_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS as MIN_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
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
import { ChannelMonitorFieldInfo } from './channel-monitor-field-info'
import { ChannelMonitorProbeResponseFields } from './channel-monitor-probe-response-fields'
import { ChannelMonitorSmartScheduleFields } from './channel-monitor-smart-schedule-fields'

export type ChannelMonitorSettingsSection = 'monitor' | 'retention' | 'probe'

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
  | 'routeMetricRetentionDays'
  | 'durationBucketRetentionDays'
  | 'apiKeyMetricRetentionDays'
  | 'executionDetailRetentionDays'
  | 'taskRetentionDays'
  | 'ratioMonitorTaskRetentionDays'
  | 'smartScheduleTaskRetentionDays'
  | 'smartScheduleProbeTaskRetentionDays'
  | 'cleanupTaskRetentionDays'
  | 'modelDetectionTaskRetentionDays'
  | 'channelTestTaskRetentionDays'
  | 'modelUpdateTaskRetentionDays'
  | 'ratioHistoryRetentionDays'
  | 'statusProbeHistoryRetentionDays'
  | 'groupMonitorRetentionDays'
  | 'modelDetectionRetentionDays'

type ChannelMonitorCleanupNumberFieldName =
  | 'cleanupBatchSize'
  | 'cleanupBudgetSeconds'
  | 'cleanupContinuationSeconds'
  | 'cleanupIntervalMinutes'

function ChannelMonitorRetentionDayField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  name: ChannelMonitorRetentionFieldName
  label: string
  min?: number
  description: string
  max?: number
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center gap-1'>
            <FormLabel>{props.label}</FormLabel>
            <ChannelMonitorFieldInfo
              label={props.label}
              description={props.description}
            />
          </div>
          <InputGroup>
            <FormControl>
              <InputGroupInput
                type='number'
                min={props.min ?? MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS}
                max={props.max ?? MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS}
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
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function ChannelMonitorCleanupNumberField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  name: ChannelMonitorCleanupNumberFieldName
  label: string
  description: string
  min: number
  max: number
  unit: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center gap-1'>
            <FormLabel>{props.label}</FormLabel>
            <ChannelMonitorFieldInfo
              label={props.label}
              description={props.description}
            />
          </div>
          <InputGroup>
            <FormControl>
              <InputGroupInput
                type='number'
                min={props.min}
                max={props.max}
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
            <InputGroupAddon align='inline-end'>{props.unit}</InputGroupAddon>
          </InputGroup>
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
      label='日成本保留天数'
      description='按天保留渠道和 API Key 的成本聚合记录；过期记录会被自动删除，不影响官方 logs 日志。'
    />
  )
}

export function ChannelMonitorRetentionFields(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <section className='space-y-4' aria-labelledby='channel-monitor-retention'>
      <div className='space-y-1'>
        <div className='flex items-center gap-1'>
          <h3 id='channel-monitor-retention' className='text-sm font-medium'>
            数据保留
          </h3>
          <ChannelMonitorFieldInfo
            label='数据保留'
            description='按配置周期分批清理到期数据；删除后不可恢复。'
          />
        </div>
      </div>
      <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
        <ChannelMonitorCostRetentionField form={props.form} />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='routeMetricRetentionDays'
          label='路由分钟指标保留天数'
          description='保留路由维度的分钟成功、失败、缓存和请求统计；智能调度使用的最长窗口会额外保护这段数据。'
          min={MIN_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='durationBucketRetentionDays'
          label='延迟分桶保留天数'
          description='保留首字延迟分布的分桶统计，用于观察延迟趋势；智能调度使用的最长窗口会额外保护这段数据。'
          min={MIN_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='apiKeyMetricRetentionDays'
          label='API Key 分钟指标保留天数'
          description='保留 API Key 维度的分钟成功、失败、缓存和 token 统计；只影响聚合指标，不影响请求日志。'
          min={MIN_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='executionDetailRetentionDays'
          label='调度执行明细保留天数'
          description='保留智能调度每次执行的渠道评分、采样数据和最终决策，过期后删除完整明细。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='taskRetentionDays'
          label='未分类监控任务保留天数（兼容）'
          description='仅用于兼容旧版本选项的兜底配置：只有 system_tasks.type 没有独立保留配置时才使用它；当前已列出的渠道监控任务都有各自的保留天数。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='ratioMonitorTaskRetentionDays'
          label='渠道比例监控任务保留天数'
          description='保留 channel_ratio_monitor 类型的系统任务记录；任务结束并超过天数后，任务及其关联明细会被删除。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='smartScheduleTaskRetentionDays'
          label='智能调度任务保留天数'
          description='保留 channel_smart_schedule 类型的自动调度任务记录；只清理已结束且超过保留期的历史任务。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='smartScheduleProbeTaskRetentionDays'
          label='智能调度探测任务保留天数'
          description='保留 channel_smart_schedule_probe 类型的探测任务记录；只清理已结束且超过保留期的历史任务。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='cleanupTaskRetentionDays'
          label='清理任务保留天数'
          description='保留 channel_monitor_cost_retention 清理任务本身的执行记录；不会改变清理周期或清理范围。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='modelDetectionTaskRetentionDays'
          label='模型检测任务保留天数'
          description='保留 channel_model_detection 类型的系统任务记录；它只控制任务记录，不等同于下方的模型检测历史保留期。'
          min={MIN_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='channelTestTaskRetentionDays'
          label='渠道测试任务保留天数'
          description='保留 channel_test 类型的渠道测试任务记录；只清理已结束且超过保留期的历史任务。'
          min={MIN_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='modelUpdateTaskRetentionDays'
          label='模型更新任务保留天数'
          description='保留 model_update 类型的上游模型更新任务记录；只清理已结束且超过保留期的历史任务。'
          min={MIN_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='ratioHistoryRetentionDays'
          label='倍率历史保留天数'
          description='保留渠道成本倍率的变更历史，用于追溯倍率何时被调整；超过保留期的历史变更会被删除。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='statusProbeHistoryRetentionDays'
          label='状态探测记录保留天数'
          description='保留渠道状态探测的执行记录，用于查看探测结果和失败原因；允许范围最多 90 天。'
          max={MAX_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS}
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='groupMonitorRetentionDays'
          label='分组监控记录保留天数'
          description='保留分组监控每次执行的记录，用于查看分组状态变化和探测结果。'
        />
        <ChannelMonitorRetentionDayField
          form={props.form}
          name='modelDetectionRetentionDays'
          label='模型检测历史保留天数'
          description='保留模型检测轮次、执行结果和成本事件历史；这是模型检测业务历史，不是 system_tasks 任务记录。'
          min={MIN_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS}
          max={MAX_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS}
        />
      </div>
      <FormField
        control={props.form.control}
        name='cleanupEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div className='space-y-1'>
              <div className='flex items-center gap-1'>
                <FormLabel>启用自动清理</FormLabel>
                <ChannelMonitorFieldInfo
                  label='启用自动清理'
                  description='开启后按上面的保留天数定期清理历史数据；关闭后不会创建或续排清理任务，已经排队的清理任务会直接结束。'
                />
              </div>
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                aria-label='启用自动清理'
              />
            </FormControl>
          </FormItem>
        )}
      />
      <div className='space-y-3' aria-labelledby='channel-monitor-cleanup'>
        <div className='space-y-1'>
          <div className='flex items-center gap-1'>
            <h4 id='channel-monitor-cleanup' className='text-sm font-medium'>
              高级清理设置
            </h4>
            <ChannelMonitorFieldInfo
              label='高级清理设置'
              description='控制单轮删除压力与未完成清理的续跑速度。'
            />
          </div>
        </div>
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
          <ChannelMonitorCleanupNumberField
            form={props.form}
            name='cleanupBatchSize'
            label='单批删除数'
            description='每次删除操作最多处理的记录数；数值越大清理越快，但单次数据库压力也越高。'
            min={MIN_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE}
            max={MAX_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE}
            unit='条'
          />
          <ChannelMonitorCleanupNumberField
            form={props.form}
            name='cleanupBudgetSeconds'
            label='单轮清理预算'
            description='单次清理任务最多运行的时间；超出预算会标记未完成，并按续跑间隔继续清理。'
            min={MIN_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS}
            max={MAX_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS}
            unit='秒'
          />
          <ChannelMonitorCleanupNumberField
            form={props.form}
            name='cleanupIntervalMinutes'
            label='清理周期'
            description='自动清理任务的调度间隔；它只控制清理任务频率，不影响渠道监控每分钟执行。保存后从下一次调度周期开始生效。'
            min={MIN_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES}
            max={MAX_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES}
            unit='分钟'
          />
          <ChannelMonitorCleanupNumberField
            form={props.form}
            name='cleanupContinuationSeconds'
            label='续跑间隔'
            description='单轮清理因预算不足未完成时，等待多长时间再续跑；不会改变数据的保留天数。'
            min={MIN_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS}
            max={MAX_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS}
            unit='秒'
          />
        </div>
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
          <div className='flex items-center gap-1'>
            <FormLabel>连续失败停止次数</FormLabel>
            <ChannelMonitorFieldInfo
              label='连续失败停止次数'
              description='倍率和余额分别连续失败达到该次数后停止自动更新；手动更新成功后恢复。'
            />
          </div>
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
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorRetryDelayField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='autoUpdateRetryDelaySeconds'
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center gap-1'>
            <FormLabel>失败重试等待时间</FormLabel>
            <ChannelMonitorFieldInfo
              label='失败重试等待时间'
              description='每次重试前等待的时间；设置为 0 时立即重试。'
            />
          </div>
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={0}
                max={MAX_AUTO_UPDATE_RETRY_DELAY_SECONDS}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors.autoUpdateRetryDelaySeconds
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

export function ChannelMonitorUpstreamRequestTimeoutField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='upstreamRequestTimeoutSeconds'
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center gap-1'>
            <FormLabel>上游请求超时</FormLabel>
            <ChannelMonitorFieldInfo
              label='上游请求超时'
              description='单次倍率或余额更新超过该时间会终止；自动更新随后按失败重试规则处理。'
            />
          </div>
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
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorConcurrencyWaitField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  return (
    <FormField
      control={props.form.control}
      name='channelConcurrencyWaitSeconds'
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center gap-1'>
            <FormLabel>渠道满载等待时间</FormLabel>
            <ChannelMonitorFieldInfo
              label='渠道满载等待时间'
              description='当前渠道并发或 RPM 达到上限时，自动尝试同组其它渠道；无可用渠道时最多等待该时间。设置为 0 时不等待。'
            />
          </div>
          <FormControl>
            <InputGroup>
              <InputGroupInput
                type='number'
                min={MIN_CHANNEL_CONCURRENCY_WAIT_SECONDS}
                max={MAX_CHANNEL_CONCURRENCY_WAIT_SECONDS}
                step={1}
                inputMode='numeric'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors.channelConcurrencyWaitSeconds
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
  const [activeSection, setActiveSection] = useState<string>(
    props.initialSection
  )
  const currentUser = useAuthStore((state) => state.auth.user)
  const canReadLogicalGroups = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  const canEditLogicalGroups = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.WRITE
  )
  const canDeleteLogicalGroups = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const queryClient = useQueryClient()
  const form = useForm<ChannelMonitorSettingsFormValues>({
    resolver: zodResolver(
      createChannelMonitorSettingsSchema()
    ) as Resolver<ChannelMonitorSettingsFormValues>,
    defaultValues: {
      autoUpdateIntervalMinutes: props.settings.auto_update_interval_minutes,
      autoUpdateRetryCount: props.settings.auto_update_retry_count,
      autoUpdateRetryDelaySeconds:
        props.settings.auto_update_retry_delay_seconds ??
        DEFAULT_AUTO_UPDATE_RETRY_DELAY_SECONDS,
      channelConcurrencyWaitSeconds:
        props.settings.channel_concurrency_wait_seconds ??
        DEFAULT_CHANNEL_CONCURRENCY_WAIT_SECONDS,
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
      costRetentionDays: props.settings.cost_retention_days,
      routeMetricRetentionDays: props.settings.route_metric_retention_days,
      durationBucketRetentionDays:
        props.settings.duration_bucket_retention_days,
      apiKeyMetricRetentionDays: props.settings.api_key_metric_retention_days,
      executionDetailRetentionDays:
        props.settings.execution_detail_retention_days,
      taskRetentionDays: props.settings.task_retention_days,
      ratioMonitorTaskRetentionDays:
        props.settings.ratio_monitor_task_retention_days,
      smartScheduleTaskRetentionDays:
        props.settings.smart_schedule_task_retention_days,
      smartScheduleProbeTaskRetentionDays:
        props.settings.smart_schedule_probe_task_retention_days,
      cleanupTaskRetentionDays: props.settings.cleanup_task_retention_days,
      modelDetectionTaskRetentionDays:
        props.settings.model_detection_task_retention_days,
      channelTestTaskRetentionDays:
        props.settings.channel_test_task_retention_days,
      modelUpdateTaskRetentionDays:
        props.settings.model_update_task_retention_days,
      ratioHistoryRetentionDays: props.settings.ratio_history_retention_days,
      statusProbeHistoryRetentionDays:
        props.settings.status_probe_history_retention_days,
      groupMonitorRetentionDays: props.settings.group_monitor_retention_days,
      modelDetectionRetentionDays:
        props.settings.model_detection_retention_days,
      cleanupEnabled: props.settings.cleanup_enabled,
      cleanupBatchSize: props.settings.cleanup_batch_size,
      cleanupBudgetSeconds: props.settings.cleanup_budget_seconds,
      cleanupContinuationSeconds: props.settings.cleanup_continuation_seconds,
      cleanupIntervalMinutes: props.settings.cleanup_interval_minutes,
      emailNotificationEnabled: props.settings.email_notification_enabled,
      notificationEmail: props.settings.notification_email,
      emailNotificationTypes: props.settings.email_notification_types,
      errorMessageMapping: props.settings.error_message_mapping ?? '',
      errorMessageWhitelist: props.settings.error_message_whitelist ?? '',
      errorMessageKeywords: props.settings.error_message_keywords ?? '',
      retrySkipErrorCodes: props.settings.retry_skip_error_codes ?? '',
      retrySkipErrorMessages: props.settings.retry_skip_error_messages ?? '',
      probeResponseEnabled: props.settings.probe_response_enabled ?? false,
      probeResponseAllowedIPs: props.settings.probe_response_allowed_ips ?? '',
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
      smartSchedulePerformanceWindowMinutes:
        props.settings.smart_schedule_performance_window_minutes,
      smartScheduleRealtimeRetentionMinutes:
        props.settings.smart_schedule_realtime_retention_minutes,
      smartScheduleRealtimeSampleLimit:
        props.settings.smart_schedule_realtime_sample_limit,
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
          'grid-rows-[auto_minmax(0,1fr)] sm:max-w-4xl'
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
              onValueChange={setActiveSection}
              className='min-h-0 flex-1 gap-4'
            >
              <TabsList className='grid h-auto w-full shrink-0 grid-cols-2 sm:grid-cols-5'>
                <TabsTrigger
                  value='monitor'
                  className='h-auto px-2 text-wrap'
                >
                  倍率与通知
                </TabsTrigger>
                <TabsTrigger
                  value='error-handling'
                  className='h-auto px-2 text-wrap'
                >
                  错误处理
                </TabsTrigger>
                <TabsTrigger
                  value='probe'
                  className='h-auto px-2 text-wrap'
                >
                  探针响应
                </TabsTrigger>
                <TabsTrigger
                  value='logical-groups'
                  className='h-auto px-2 text-wrap'
                  disabled={!canReadLogicalGroups}
                >
                  同渠道配置
                </TabsTrigger>
                <TabsTrigger
                  value='retention'
                  className='h-auto px-2 text-wrap'
                >
                  数据保留
                </TabsTrigger>
              </TabsList>

              <TabsContent
                value='monitor'
                className='mt-0 min-h-0 overflow-x-hidden overflow-y-auto pr-1'
              >
                <div className='space-y-5'>
                  <section className='space-y-3'>
                    <div className='flex items-center gap-2'>
                      <h3 className='text-sm font-medium'>自动更新</h3>
                      <ChannelMonitorFieldInfo
                        label='自动更新'
                        description='控制渠道倍率、余额和状态的自动更新节奏，以及更新失败时的重试行为。'
                      />
                    </div>
                    <div className='grid gap-4 sm:grid-cols-2'>
                      <FormField
                        control={form.control}
                        name='autoUpdateIntervalMinutes'
                        render={({ field }) => (
                          <FormItem>
                            <div className='flex items-center gap-1'>
                              <FormLabel>更新间隔</FormLabel>
                              <ChannelMonitorFieldInfo
                                label='更新间隔'
                                description='设置为 0 时关闭自动更新；保存后自动生效。'
                              />
                            </div>
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
                                    form.formState.errors
                                      .autoUpdateIntervalMinutes
                                  )}
                                />
                                <InputGroupAddon align='inline-end'>
                                  分钟
                                </InputGroupAddon>
                              </InputGroup>
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <FormField
                        control={form.control}
                        name='autoUpdateRetryCount'
                        render={({ field }) => (
                          <FormItem>
                            <div className='flex items-center gap-1'>
                              <FormLabel>失败重试次数</FormLabel>
                              <ChannelMonitorFieldInfo
                                label='失败重试次数'
                                description='首次失败后最多再尝试的次数；设置为 0 时不重试。'
                              />
                            </div>
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
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <ChannelMonitorRetryDelayField form={form} />
                      <ChannelMonitorConcurrencyWaitField form={form} />
                      <ChannelMonitorUpstreamRequestTimeoutField form={form} />
                      <ChannelMonitorConsecutiveFailureLimitField form={form} />
                    </div>
                  </section>

                  <section className='space-y-3 border-t pt-5'>
                    <div className='flex items-center gap-2'>
                      <h3 className='text-sm font-medium'>渠道状态</h3>
                      <ChannelMonitorFieldInfo
                        label='渠道状态'
                        description='设置自动更新失败、成本倍率恢复和余额恢复时，系统是否自动切换渠道状态。'
                      />
                    </div>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <FormField
                        control={form.control}
                        name='autoDisableOnUpdateFailure'
                        render={({ field }) => (
                          <FormItem className='flex min-h-16 items-center justify-between gap-4 rounded-lg border p-3'>
                            <div className='flex min-w-0 items-center gap-1'>
                              <FormLabel>更新失败自动禁用渠道</FormLabel>
                              <ChannelMonitorFieldInfo
                                label='更新失败自动禁用渠道'
                                description='开启后，倍率或余额更新在重试后仍失败时自动禁用对应渠道。'
                              />
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
                          <FormItem className='flex min-h-16 items-center justify-between gap-4 rounded-lg border p-3'>
                            <div className='flex min-w-0 items-center gap-1'>
                              <FormLabel>成本倍率恢复后自动启用渠道</FormLabel>
                              <ChannelMonitorFieldInfo
                                label='成本倍率恢复后自动启用渠道'
                                description='开启后，因成本倍率过高被系统禁用的渠道，在按分组系数换算后严格低于全部所属分组倍率时自动启用。'
                              />
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
                          <FormItem className='flex min-h-16 items-center justify-between gap-4 rounded-lg border p-3'>
                            <div className='flex min-w-0 items-center gap-1'>
                              <FormLabel>余额恢复后自动启用渠道</FormLabel>
                              <ChannelMonitorFieldInfo
                                label='余额恢复后自动启用渠道'
                                description='开启后，因余额低于阈值被系统禁用的渠道，在余额恢复且按分组系数换算后的成本倍率不高于全部所属分组倍率时自动启用。'
                              />
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
                    </div>
                  </section>

                  <ChannelMonitorEmailNotificationFields form={form} />
                </div>
              </TabsContent>

              <TabsContent
                value='error-handling'
                className='mt-0 min-h-0 overflow-x-hidden overflow-y-auto pr-1'
              >
                <div className='space-y-4'>
                  <div className='flex items-center gap-2'>
                    <h3 className='text-sm font-medium'>错误规则</h3>
                    <ChannelMonitorFieldInfo
                      label='错误规则'
                      description='统一控制上游错误的用户可见文案、屏蔽关键字和渠道监控重试行为。'
                    />
                  </div>
                  <div className='flex flex-col gap-5'>
                    <FormField
                      control={form.control}
                      name='errorMessageMapping'
                      render={({ field }) => (
                        <FormItem className='space-y-3'>
                          <div className='flex items-center gap-1'>
                            <FormLabel>错误信息映射</FormLabel>
                            <ChannelMonitorFieldInfo
                              label='错误信息映射'
                              description='统一应用于所有渠道的用户请求和使用日志；按上游错误码优先、最终 HTTP 状态码其次匹配，用户请求仅在响应尚未开始时替换。'
                            />
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
                    <FormField
                      control={form.control}
                      name='errorMessageWhitelist'
                      render={({ field }) => (
                        <FormItem className='space-y-3 border-t pt-4'>
                          <div className='flex items-center gap-1'>
                            <FormLabel>错误码白名单</FormLabel>
                            <ChannelMonitorFieldInfo
                              label='错误码白名单'
                              description='命中的上游错误码或 HTTP 状态码不会进行错误映射和关键字屏蔽，原样返回给用户；每行填写一个，也支持逗号分隔，最多 32 个。'
                            />
                          </div>
                          <FormControl>
                            <Textarea
                              rows={4}
                              placeholder='例如：provider_specific_error\n503'
                              {...field}
                              disabled={mutation.isPending}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='errorMessageKeywords'
                      render={({ field }) => (
                        <FormItem className='space-y-3 border-t pt-4'>
                          <div className='flex items-center gap-1'>
                            <FormLabel>错误屏蔽关键字</FormLabel>
                            <ChannelMonitorFieldInfo
                              label='错误屏蔽关键字'
                              description='全局应用于所有渠道；每行填写一个关键字，最多 32 个、每个不超过 128 个字符。匹配后只从用户可见错误中删除，管理员日志仍保留完整原始错误，匹配不区分大小写。'
                            />
                          </div>
                          <FormControl>
                            <Textarea
                              rows={4}
                              placeholder='每行填写一个关键字'
                              {...field}
                              disabled={mutation.isPending}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='retrySkipErrorCodes'
                      render={({ field }) => (
                        <FormItem className='space-y-3 border-t pt-4'>
                          <div className='flex items-center gap-1'>
                            <FormLabel>命中错误码时跳过重试</FormLabel>
                            <ChannelMonitorFieldInfo
                              label='命中错误码时跳过重试'
                              description='每行填写一个错误码，也支持逗号分隔；命中上游错误码或 HTTP 状态码时，本次渠道监控直接结束，不再重试，最多 32 个。'
                            />
                          </div>
                          <FormControl>
                            <Textarea
                              rows={4}
                              placeholder='例如：insufficient_quota\n429\n500'
                              {...field}
                              disabled={mutation.isPending}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='retrySkipErrorMessages'
                      render={({ field }) => (
                        <FormItem className='space-y-3 border-t pt-4'>
                          <div className='flex items-center gap-1'>
                            <FormLabel>命中执行错误信息时跳过重试</FormLabel>
                            <ChannelMonitorFieldInfo
                              label='命中执行错误信息时跳过重试'
                              description='每行填写一个错误信息关键字，也支持逗号分隔；按不区分大小写的包含匹配，命中后本次渠道监控不再重试，最多 32 个。'
                            />
                          </div>
                          <FormControl>
                            <Textarea
                              rows={4}
                              placeholder='例如：额度不足\ninvalid api key'
                              {...field}
                              disabled={mutation.isPending}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                </div>
              </TabsContent>

              <TabsContent
                value='probe'
                className='mt-0 min-h-0 overflow-x-hidden overflow-y-auto pr-1'
              >
                <ChannelMonitorProbeResponseFields form={form} />
              </TabsContent>

              <TabsContent
                value='retention'
                className='mt-0 min-h-0 overflow-x-hidden overflow-y-auto pr-1'
              >
                <ChannelMonitorRetentionFields form={form} />
              </TabsContent>

              <TabsContent
                value='logical-groups'
                className='mt-0 min-h-0 overflow-x-hidden overflow-y-auto pr-1'
              >
                <div className='mb-4 flex items-center gap-1'>
                  <h3 className='text-base leading-none font-medium'>
                    同渠道配置
                  </h3>
                  <ChannelMonitorFieldInfo
                    label='同渠道配置'
                    description='将请求地址一致的物理渠道配置为同一逻辑渠道，组内仅共享调度、状态探测和模型检测。'
                  />
                </div>
                <LogicalGroupsPanel
                  open={props.open && activeSection === 'logical-groups'}
                  canRead={canReadLogicalGroups}
                  canEdit={canEditLogicalGroups}
                  canDelete={canDeleteLogicalGroups}
                />
              </TabsContent>
            </Tabs>

            {activeSection !== 'logical-groups' ? (
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
            ) : null}
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
