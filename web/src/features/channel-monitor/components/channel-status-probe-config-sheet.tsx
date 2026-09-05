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
import { Alert02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { toast } from 'sonner'

import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useIsMobile } from '@/hooks/use-mobile'

import { updateChannelStatusProbeConfig } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  CHANNEL_STATUS_PROBE_DEFAULT_INTERVAL_SECONDS,
  CHANNEL_STATUS_PROBE_DISPLAY_LIMITS,
  channelStatusProbeConfigSchema,
  type ChannelStatusProbeConfigFormValues,
} from '../lib/status-probe-schema'
import type {
  ChannelStatusProbeChannel,
  ChannelStatusProbeDisplayUnit,
} from '../types'

type ChannelStatusProbeConfigSheetProps = {
  channel: ChannelStatusProbeChannel
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved?: () => void | Promise<void>
}

const QUICK_INTERVALS = [60, 300, 900, 3600]
const DISPLAY_UNITS: Array<{
  value: ChannelStatusProbeDisplayUnit
  label: string
}> = [
  { value: 'minute', label: '分钟' },
  { value: 'hour', label: '小时' },
  { value: 'day', label: '天' },
]

export function ChannelStatusProbeConfigSheet(
  props: ChannelStatusProbeConfigSheetProps
) {
  const isMobile = useIsMobile()
  const queryClient = useQueryClient()
  const defaultModels = useMemo(() => {
    if (props.channel.config) return props.channel.config.models
    const firstConcrete = props.channel.supported_models.find(
      (modelName) => !modelName.includes('*')
    )
    return firstConcrete ? [firstConcrete] : []
  }, [props.channel.config, props.channel.supported_models])
  const form = useForm<ChannelStatusProbeConfigFormValues>({
    resolver: zodResolver(
      channelStatusProbeConfigSchema
    ) as Resolver<ChannelStatusProbeConfigFormValues>,
    defaultValues: {
      enabled: props.channel.config?.enabled ?? false,
      models: defaultModels,
      intervalSeconds:
        props.channel.config?.interval_seconds ??
        CHANNEL_STATUS_PROBE_DEFAULT_INTERVAL_SECONDS,
      displayValue: props.channel.config?.display_value ?? 60,
      displayUnit: props.channel.config?.display_unit ?? 'minute',
      recordSample: props.channel.config?.record_sample ?? false,
    },
  })
  const models = useWatch({ control: form.control, name: 'models' })
  const intervalSeconds = useWatch({
    control: form.control,
    name: 'intervalSeconds',
  })
  const displayUnit = useWatch({ control: form.control, name: 'displayUnit' })
  const displayValue = useWatch({ control: form.control, name: 'displayValue' })
  const displayLimit = CHANNEL_STATUS_PROBE_DISPLAY_LIMITS[displayUnit]
  const requestsPerHour =
    intervalSeconds > 0 ? (models.length * 3600) / intervalSeconds : 0
  const modelOptions = useMemo(
    () =>
      props.channel.supported_models
        .filter((modelName) => !modelName.includes('*'))
        .map((modelName) => ({ label: modelName, value: modelName })),
    [props.channel.supported_models]
  )
  const mutation = useMutation({
    mutationFn: updateChannelStatusProbeConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('状态探测配置已保存')
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'status-probe'],
      })
      void props.onSaved?.()
      props.onOpenChange(false)
    },
  })
  const handleSubmit = form.handleSubmit((values) => {
    mutation.mutate({
      channelId: props.channel.id,
      enabled: values.enabled,
      models: values.models,
      intervalSeconds: values.intervalSeconds,
      displayValue: values.displayValue,
      displayUnit: values.displayUnit,
      recordSample: values.recordSample,
      revision: props.channel.config?.revision ?? 0,
    })
  })

  const formContent = (
    <Form {...form}>
      <form className='flex min-h-0 flex-1 flex-col' onSubmit={handleSubmit}>
        <div className='flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-4 pb-5'>
          <Alert className='bg-warning/5'>
            <HugeiconsIcon icon={Alert02Icon} />
            <AlertTitle>真实请求与计费</AlertTitle>
            <AlertDescription>
              每次探测都会请求上游，并产生上游费用和本地测试消费记录。计入样本后，成功与失败结果还会参与智能调度判断。
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='grid grid-cols-[1fr_auto] items-center gap-x-4 rounded-lg border px-3 py-2.5'>
                <div>
                  <FormLabel>启用周期探测</FormLabel>
                  <FormDescription>
                    关闭后仍保留配置和历史，也可以手动立即检测
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={mutation.isPending}
                    aria-label='启用周期探测'
                  />
                </FormControl>
                <FormMessage className='col-span-2' />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='models'
            render={({ field }) => (
              <FormItem>
                <FormLabel>探测模型</FormLabel>
                <FormDescription>
                  每个渠道最多 20 个，按列表顺序串行检测
                </FormDescription>
                <FormControl>
                  <MultiSelect
                    id={`channel-status-probe-models-${props.channel.id}`}
                    options={modelOptions}
                    selected={field.value}
                    onChange={field.onChange}
                    allowCreate={props.channel.allows_custom_model}
                    createLabel='添加具体模型“{{value}}”'
                    placeholder='选择探测模型'
                    emptyText='没有匹配的模型'
                    maxVisibleChips={6}
                    disabled={mutation.isPending}
                  />
                </FormControl>
                {props.channel.allows_custom_model && (
                  <FormDescription>
                    该渠道含通配模型，可输入一个具体模型名后回车添加
                  </FormDescription>
                )}
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='intervalSeconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>探测间隔</FormLabel>
                <FormDescription>单位为秒，范围 30 到 86400</FormDescription>
                <FormControl>
                  <Input
                    type='number'
                    min={30}
                    max={86400}
                    step={1}
                    value={Number.isFinite(field.value) ? field.value : ''}
                    onChange={(event) =>
                      field.onChange(event.target.valueAsNumber)
                    }
                    disabled={mutation.isPending}
                    className='font-mono tabular-nums'
                  />
                </FormControl>
                <ToggleGroup
                  value={
                    QUICK_INTERVALS.includes(field.value)
                      ? [String(field.value)]
                      : []
                  }
                  onValueChange={(values) => {
                    const selected = values[0]
                    if (selected) field.onChange(Number(selected))
                  }}
                  variant='outline'
                  size='sm'
                  spacing={0}
                  className='max-w-full justify-start overflow-x-auto'
                  aria-label='常用探测间隔'
                >
                  {QUICK_INTERVALS.map((interval) => (
                    <ToggleGroupItem
                      key={interval}
                      value={String(interval)}
                      className='shrink-0'
                      disabled={mutation.isPending}
                    >
                      {interval} 秒
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='displayValue'
            render={({ field }) => (
              <FormItem>
                <FormLabel>状态展示范围</FormLabel>
                <FormDescription>
                  状态条、平均首字和平均 TPS 使用同一时间范围，最多 30 天
                </FormDescription>
                <FormControl>
                  <div className='flex min-w-0 flex-wrap items-center gap-2'>
                    <Input
                      type='number'
                      min={1}
                      max={displayLimit}
                      step={1}
                      value={Number.isFinite(field.value) ? field.value : ''}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                      disabled={mutation.isPending}
                      className='w-28 font-mono tabular-nums'
                      aria-label='状态展示数值'
                    />
                    <ToggleGroup
                      value={[displayUnit]}
                      onValueChange={(values) => {
                        const selected = values[0] as
                          | ChannelStatusProbeDisplayUnit
                          | undefined
                        if (!selected) return
                        form.setValue('displayUnit', selected, {
                          shouldDirty: true,
                          shouldValidate: true,
                        })
                        const nextLimit =
                          CHANNEL_STATUS_PROBE_DISPLAY_LIMITS[selected]
                        if (displayValue > nextLimit) {
                          form.setValue('displayValue', nextLimit, {
                            shouldDirty: true,
                            shouldValidate: true,
                          })
                        }
                      }}
                      variant='outline'
                      size='sm'
                      spacing={0}
                      className='max-w-full justify-start overflow-x-auto'
                      aria-label='状态展示单位'
                    >
                      {DISPLAY_UNITS.map((unit) => (
                        <ToggleGroupItem
                          key={unit.value}
                          value={unit.value}
                          className='shrink-0'
                          disabled={mutation.isPending}
                        >
                          {unit.label}
                        </ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                    <span className='text-muted-foreground text-xs'>
                      上限 {displayLimit}
                    </span>
                  </div>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='recordSample'
            render={({ field }) => (
              <FormItem className='grid grid-cols-[1fr_auto] items-center gap-x-4 rounded-lg border px-3 py-2.5'>
                <div>
                  <FormLabel>计入智能调度样本</FormLabel>
                  <FormDescription>
                    4xx 不计入；其他有效成功和失败可能影响路由与稳定性保护
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={mutation.isPending}
                    aria-label='计入智能调度样本'
                  />
                </FormControl>
                <FormMessage className='col-span-2' />
              </FormItem>
            )}
          />

          <div className='bg-muted/50 flex items-center justify-between gap-4 rounded-lg px-3 py-2.5 text-sm'>
            <span className='text-muted-foreground'>负载预估</span>
            <span className='font-mono font-medium tabular-nums'>
              每小时约{' '}
              {Number.isFinite(requestsPerHour)
                ? requestsPerHour.toFixed(1)
                : '0'}{' '}
              次请求
            </span>
          </div>
          {requestsPerHour > 60 && (
            <p className='text-warning text-sm'>
              当前配置超过每分钟 1 次请求，请确认上游配额和渠道并发容量。
            </p>
          )}
        </div>

        <div className='flex shrink-0 justify-end gap-2 border-t p-4'>
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
        </div>
      </form>
    </Form>
  )

  if (isMobile) {
    return (
      <Drawer open={props.open} onOpenChange={props.onOpenChange}>
        <DrawerContent className='max-h-[95dvh]'>
          <DrawerHeader className='text-left'>
            <DrawerTitle>配置状态探测</DrawerTitle>
            <DrawerDescription>
              {props.channel.name} · ID {props.channel.id}
            </DrawerDescription>
          </DrawerHeader>
          {formContent}
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full sm:max-w-xl'>
        <SheetHeader>
          <SheetTitle>配置状态探测</SheetTitle>
          <SheetDescription>
            {props.channel.name} · ID {props.channel.id}
          </SheetDescription>
        </SheetHeader>
        {formContent}
      </SheetContent>
    </Sheet>
  )
}
