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
import {
  Alert02Icon,
  Calculator01Icon,
  FingerPrintScanIcon,
  PlayIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  channelModelDetectionClaimedModelLabel,
  channelModelDetectionPresetLabel,
} from '../lib/model-detection'
import {
  estimateChannelModelDetectionCost,
  isChannelModelDetectionInfrastructureConflict,
  startChannelModelDetectionRun,
} from '../lib/model-detection-channel-api'
import {
  CHANNEL_MODEL_DETECTION_MANUAL_ESTIMATE_EMPTY_VALUES,
  channelModelDetectionManualEstimateSchema,
  createChannelModelDetectionEstimateRequest,
  type ChannelModelDetectionManualEstimateFormValues,
} from '../lib/model-detection-channel-schema'
import { channelModelDetectionRequestErrorMessage } from '../lib/model-detection-settings-api'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionEstimateResult,
  ChannelModelDetectionPreset,
  ChannelModelDetectionRunAccepted,
  ChannelModelDetectionTargetEstimate,
} from '../types-model-detection'

const PRESET_OPTIONS = [
  { value: 'low', label: '低档', description: '请求较少' },
  { value: 'medium', label: '中档', description: '平衡档位' },
  { value: 'high', label: '高档', description: '请求和成本更高' },
] as const satisfies ReadonlyArray<{
  value: ChannelModelDetectionPreset
  label: string
  description: string
}>

export type ChannelModelDetectionRunDialogProps = {
  channel: ChannelModelDetectionChannel | null
  open: boolean
  onOpenChange: (open: boolean) => void
  hasUnsavedConfig?: boolean
  onEstimate?: (estimate: ChannelModelDetectionEstimateResult) => void
  onRunAccepted?: (
    run: ChannelModelDetectionRunAccepted,
    channelId: number
  ) => void
  onRefreshRequested?: () => void
}

function TargetEstimateRow(props: {
  target: ChannelModelDetectionTargetEstimate
}) {
  return (
    <TableRow>
      <TableCell className='min-w-52 whitespace-normal'>
        <div className='font-medium break-all'>
          {props.target.request_model}
        </div>
        <div className='text-muted-foreground mt-0.5 text-xs'>
          申报{' '}
          {channelModelDetectionClaimedModelLabel(props.target.claimed_model)}
        </div>
      </TableCell>
      <TableCell className='text-right'>
        {props.target.estimated_logical_requests.toLocaleString('zh-CN')}
      </TableCell>
      <TableCell className='text-right'>
        {props.target.estimated_http_attempts.toLocaleString('zh-CN')}
      </TableCell>
    </TableRow>
  )
}

export function ChannelModelDetectionRunDialog(
  props: ChannelModelDetectionRunDialogProps
) {
  const [estimatedRevision, setEstimatedRevision] = useState<number | null>(
    null
  )
  const form = useForm<ChannelModelDetectionManualEstimateFormValues>({
    resolver: zodResolver(
      channelModelDetectionManualEstimateSchema
    ) as Resolver<ChannelModelDetectionManualEstimateFormValues>,
    defaultValues: CHANNEL_MODEL_DETECTION_MANUAL_ESTIMATE_EMPTY_VALUES,
  })
  const estimateMutation = useMutation({
    mutationFn: (variables: {
      channelId: number
      preset: ChannelModelDetectionPreset
      revision: number
    }) => {
      if (!props.channel) throw new Error('未选择需要预览请求量的渠道')
      return estimateChannelModelDetectionCost(
        variables.channelId,
        createChannelModelDetectionEstimateRequest(variables.preset)
      )
    },
    onSuccess: (estimate, variables) => {
      setEstimatedRevision(variables.revision)
      toast.success('手动检测请求量已更新')
      props.onEstimate?.(estimate)
    },
    onError: (error) => {
      toast.error(channelModelDetectionRequestErrorMessage(error))
    },
  })
  const runMutation = useMutation({
    mutationFn: (variables: {
      channelId: number
      preset: ChannelModelDetectionPreset
      confirmHighCost: boolean
    }) =>
      startChannelModelDetectionRun(variables.channelId, {
        preset: variables.preset,
        confirm_high_cost: variables.confirmHighCost,
      }),
    onSuccess: (run, variables) => {
      toast.success('模型检测任务已进入队列')
      props.onRunAccepted?.(run, variables.channelId)
      props.onRefreshRequested?.()
      props.onOpenChange(false)
    },
    onError: (error) => {
      estimateMutation.reset()
      setEstimatedRevision(null)
      props.onRefreshRequested?.()
      if (isChannelModelDetectionInfrastructureConflict(error)) {
        toast.error(
          `模型检测任务状态发生冲突：${channelModelDetectionRequestErrorMessage(error)}`
        )
        return
      }
      toast.error(
        `启动结果未确认，请刷新任务状态后重新获取请求量：${channelModelDetectionRequestErrorMessage(error)}`
      )
    },
  })
  const resetEstimate = estimateMutation.reset
  const resetRun = runMutation.reset

  useEffect(() => {
    form.reset(CHANNEL_MODEL_DETECTION_MANUAL_ESTIMATE_EMPTY_VALUES)
    resetEstimate()
    resetRun()
    setEstimatedRevision(null)
  }, [form, props.channel?.id, props.open, resetEstimate, resetRun])

  const preset = form.watch('preset')
  const confirmHighCost = form.watch('confirmHighCost')
  const requiresHighConfirmation = preset === 'high'
  const estimate = estimateMutation.data
  const currentRevision = props.channel?.config?.revision ?? null
  const estimateMatchesSelection = Boolean(
    estimate && preset && estimate.preset === preset
  )
  const estimateMatchesRevision = Boolean(
    estimate &&
    estimatedRevision != null &&
    currentRevision === estimatedRevision
  )
  const validEstimate =
    estimateMatchesSelection &&
    estimateMatchesRevision &&
    !props.hasUnsavedConfig
      ? estimate
      : null
  const staleEstimate = Boolean(estimate && !validEstimate)
  const hasSavedTargets = Boolean(
    props.channel?.config &&
    props.channel.targets.some((target) => target.enabled)
  )
  const busy = estimateMutation.isPending || runMutation.isPending

  const handleSubmit = form.handleSubmit((values) => {
    const config = props.channel?.config
    if (!values.preset || !props.channel || !config || props.hasUnsavedConfig) {
      return
    }
    estimateMutation.reset()
    setEstimatedRevision(null)
    estimateMutation.mutate({
      channelId: props.channel.id,
      preset: values.preset,
      revision: config.revision,
    })
  })

  const handleStart = () => {
    const config = props.channel?.config
    if (!props.channel || !config || !validEstimate || !preset) return
    if (
      validEstimate.preset !== preset ||
      estimatedRevision !== config.revision
    ) {
      estimateMutation.reset()
      setEstimatedRevision(null)
      props.onRefreshRequested?.()
      toast.error('渠道配置或检测档位已变化，请刷新后重新获取请求量')
      return
    }
    runMutation.mutate({
      channelId: props.channel.id,
      preset: validEstimate.preset,
      confirmHighCost: validEstimate.preset === 'high' && confirmHighCost,
    })
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && busy) return
        props.onOpenChange(open)
      }}
    >
      <DialogContent
        className='max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-3xl'
        showCloseButton={!busy}
      >
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <HugeiconsIcon icon={FingerPrintScanIcon} aria-hidden='true' />
            手动模型检测
          </DialogTitle>
          <DialogDescription>
            {props.channel
              ? `${props.channel.name} #${props.channel.id} · 先确认请求量，再启动本次检测`
              : '选择渠道后查看请求量并启动手动检测'}
          </DialogDescription>
        </DialogHeader>

        {props.hasUnsavedConfig ? (
          <Alert variant='destructive'>
            <HugeiconsIcon icon={Alert02Icon} />
            <AlertTitle>存在未保存的渠道目标修改</AlertTitle>
            <AlertDescription>
              后端预览只读取已保存目标。请先保存或放弃修改，再重新打开请求量预览。
            </AlertDescription>
          </Alert>
        ) : null}

        {!hasSavedTargets ? (
          <Alert variant='destructive'>
            <HugeiconsIcon icon={Alert02Icon} />
            <AlertTitle>尚未保存检测目标</AlertTitle>
            <AlertDescription>
              配置并保存至少一个目标后才能查看手动检测请求量。
            </AlertDescription>
          </Alert>
        ) : null}

        <Form {...form}>
          <form
            id='channel-model-detection-estimate-form'
            className='flex min-w-0 flex-col gap-4'
            onSubmit={handleSubmit}
          >
            <FormField
              control={form.control}
              name='preset'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>本次手动检测档位</FormLabel>
                  <FormControl>
                    <ToggleGroup
                      value={field.value ? [field.value] : []}
                      onValueChange={(values) => {
                        const selected = values.at(-1) as
                          | ChannelModelDetectionPreset
                          | undefined
                        field.onChange(selected ?? '')
                        form.setValue('confirmHighCost', false, {
                          shouldValidate: false,
                        })
                        estimateMutation.reset()
                        setEstimatedRevision(null)
                      }}
                      variant='outline'
                      spacing={2}
                      disabled={
                        busy || props.hasUnsavedConfig || !hasSavedTargets
                      }
                      className='grid w-full grid-cols-3'
                      aria-label='选择本次手动模型检测档位'
                    >
                      {PRESET_OPTIONS.map((option) => (
                        <ToggleGroupItem
                          key={option.value}
                          value={option.value}
                          className='h-auto min-h-14 w-full flex-col gap-0.5 px-2 py-2 whitespace-normal'
                          aria-label={`${option.label}：${option.description}`}
                        >
                          <span>{option.label}</span>
                          <span className='text-muted-foreground text-[11px] font-normal'>
                            {option.description}
                          </span>
                        </ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            {requiresHighConfirmation ? (
              <Alert variant='destructive'>
                <HugeiconsIcon icon={Alert02Icon} />
                <AlertTitle>高档会产生更多请求和成本</AlertTitle>
                <AlertDescription>
                  本确认只对当前手动检测有效，与统一定时高档确认互不复用。
                </AlertDescription>
                <FormField
                  control={form.control}
                  name='confirmHighCost'
                  render={({ field }) => (
                    <FormItem className='col-span-full mt-2 flex flex-row items-start gap-2'>
                      <FormControl>
                        <Checkbox
                          checked={field.value}
                          disabled={busy}
                          onCheckedChange={(checked) =>
                            field.onChange(checked === true)
                          }
                          aria-label='确认本次高档手动检测成本风险'
                        />
                      </FormControl>
                      <div className='space-y-1'>
                        <FormLabel>我确认本次高档手动检测成本风险</FormLabel>
                        <FormMessage />
                      </div>
                    </FormItem>
                  )}
                />
              </Alert>
            ) : null}
          </form>
        </Form>

        {staleEstimate ? (
          <Alert variant='destructive'>
            <HugeiconsIcon icon={Alert02Icon} />
            <AlertTitle>请求量预览已失效</AlertTitle>
            <AlertDescription>
              渠道配置或本次档位已发生变化，请刷新渠道数据后重新获取请求量。
            </AlertDescription>
          </Alert>
        ) : null}

        {validEstimate ? (
          <section
            className='flex min-w-0 flex-col gap-3'
            aria-label='请求量预览结果'
          >
            <div className='grid gap-2 sm:grid-cols-2'>
              <div className='border-border/60 rounded-lg border p-3'>
                <div className='text-muted-foreground text-xs'>检测档位</div>
                <div className='mt-1 font-medium'>
                  {channelModelDetectionPresetLabel(validEstimate.preset)}
                </div>
              </div>
              <div className='border-border/60 rounded-lg border p-3'>
                <div className='text-muted-foreground text-xs'>
                  预计总逻辑请求
                </div>
                <div className='mt-1 font-medium tabular-nums'>
                  {validEstimate.official_estimate.logical_requests?.toLocaleString(
                    'zh-CN'
                  ) ?? '暂不可用'}
                </div>
              </div>
            </div>

            <div className='border-border/60 overflow-hidden rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>目标</TableHead>
                    <TableHead className='text-right'>逻辑请求</TableHead>
                    <TableHead className='text-right'>HTTP 尝试</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {validEstimate.targets.map((target) => (
                    <TargetEstimateRow
                      key={target.target_key}
                      target={target}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>

            <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
              <Badge variant='outline'>
                官方请求{' '}
                {validEstimate.official_estimate.logical_requests?.toLocaleString(
                  'zh-CN'
                ) ?? '暂不可用'}
              </Badge>
              <span>
                这里只预览请求量；实际成本仅按请求完成后的上游 Usage 结算。
              </span>
            </div>
          </section>
        ) : null}

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={busy}
            onClick={() => props.onOpenChange(false)}
          >
            关闭
          </Button>
          <Button
            form='channel-model-detection-estimate-form'
            type='submit'
            disabled={
              busy ||
              props.hasUnsavedConfig ||
              !hasSavedTargets ||
              !preset ||
              (requiresHighConfirmation && !confirmHighCost)
            }
          >
            {estimateMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={Calculator01Icon} data-icon='inline-start' />
            )}
            查看请求量
          </Button>
          <Button
            type='button'
            disabled={
              busy ||
              !validEstimate ||
              (validEstimate.preset === 'high' && !confirmHighCost)
            }
            onClick={handleStart}
          >
            {runMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={PlayIcon} data-icon='inline-start' />
            )}
            开始检测
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
