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
  Refresh01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { toast } from 'sonner'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
import { Input } from '@/components/ui/input'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  runChannelGroupMonitorNow,
  updateChannelGroupMonitorSettings,
} from '@/features/group-monitor/api'
import {
  CHANNEL_GROUP_MONITOR_DEFAULT_INTERVAL_SECONDS,
  CHANNEL_GROUP_MONITOR_DISPLAY_LIMITS,
  channelGroupMonitorConfigSchema,
  type ChannelGroupMonitorConfigFormValues,
} from '@/features/group-monitor/lib/config-schema'
import type {
  ChannelGroupMonitorSettingsResponse,
  ChannelGroupMonitorDisplayUnit,
} from '@/features/group-monitor/types'

import { ChannelGroupMonitorCategoryEditor } from './channel-group-monitor-category-editor'

const OVERVIEW_QUERY_KEY = [
  'channel-monitor',
  'group-monitor',
  'overview',
] as const
const SETTINGS_QUERY_KEY = [
  'channel-monitor',
  'group-monitor',
  'settings',
] as const
const QUICK_INTERVALS = [60, 300, 900, 3600]
const EMPTY_CANDIDATE_MODELS_BY_GROUP: Record<string, string[]> = {}
const DISPLAY_UNITS: Array<{
  value: ChannelGroupMonitorDisplayUnit
  label: string
}> = [
  { value: 'minute', label: '分钟' },
  { value: 'hour', label: '小时' },
  { value: 'day', label: '天' },
]

export type ChannelGroupMonitorSettingsSheetProps = {
  data: ChannelGroupMonitorSettingsResponse | undefined
  groupOrder: readonly string[]
  open: boolean
  onOpenChange: (open: boolean) => void
}

function dataToFormValues(
  data: ChannelGroupMonitorSettingsResponse | undefined
): ChannelGroupMonitorConfigFormValues {
  const settings = data?.settings
  const names = new Set(settings?.categories ?? [])
  for (const group of settings?.groups ?? []) {
    names.add(group.category?.trim() || '未分类')
  }
  const categories = [...names].map((name) => ({
    categoryId: `saved:${name}`,
    name,
  }))
  return {
    enabled: settings?.enabled ?? false,
    showCacheRate: settings?.show_cache_rate ?? false,
    categories,
    groups:
      settings?.groups.map((group) => ({
        groupName: group.group_name,
        probeModel: group.probe_model,
        displayInitial: group.display_initial ?? '',
        categoryId: `saved:${group.category?.trim() || '未分类'}`,
      })) ?? [],
    intervalSeconds:
      settings?.interval_seconds ??
      CHANNEL_GROUP_MONITOR_DEFAULT_INTERVAL_SECONDS,
    displayValue: settings?.display_value ?? 60,
    displayUnit: settings?.display_unit ?? 'minute',
    revision: settings?.revision ?? 0,
  }
}

function isGroupMonitorConfigConflict(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error != null &&
    'response' in error &&
    (error as { response?: { status?: unknown } }).response?.status === 409
  )
}

export function ChannelGroupMonitorSettingsSheet(
  props: ChannelGroupMonitorSettingsSheetProps
) {
  const queryClient = useQueryClient()
  const initializedRevisionRef = useRef<number | null>(null)
  const [conflictMessage, setConflictMessage] = useState('')
  const form = useForm<ChannelGroupMonitorConfigFormValues>({
    resolver: zodResolver(
      channelGroupMonitorConfigSchema
    ) as Resolver<ChannelGroupMonitorConfigFormValues>,
    defaultValues: dataToFormValues(props.data),
  })
  const displayUnit = form.watch('displayUnit')
  const displayValue = form.watch('displayValue')
  const intervalSeconds = form.watch('intervalSeconds')
  const groupValues = form.watch('groups')
  const displayLimit = CHANNEL_GROUP_MONITOR_DISPLAY_LIMITS[displayUnit]
  const candidateModelsByGroup =
    props.data?.candidate_models_by_group ?? EMPTY_CANDIDATE_MODELS_BY_GROUP

  useEffect(() => {
    if (!props.open) {
      initializedRevisionRef.current = null
      setConflictMessage('')
      return
    }
    const revision = props.data?.settings.revision
    if (revision == null || initializedRevisionRef.current === revision) return
    form.reset(dataToFormValues(props.data))
    initializedRevisionRef.current = revision
    setConflictMessage('')
  }, [form, props.data, props.open])

  const saveMutation = useMutation({
    mutationFn: (values: ChannelGroupMonitorConfigFormValues) =>
      updateChannelGroupMonitorSettings({
        enabled: values.enabled,
        showCacheRate: values.showCacheRate,
        categories: values.categories.map((category) => category.name),
        groups: values.categories.flatMap((category) =>
          values.groups
            .filter((group) => group.categoryId === category.categoryId)
            .map((group) => ({
              group_name: group.groupName,
              probe_model: group.probeModel,
              display_initial: group.displayInitial.trim(),
              category: category.name,
            }))
        ),
        intervalSeconds: values.intervalSeconds,
        displayValue: values.displayValue,
        displayUnit: values.displayUnit,
        revision: values.revision,
      }),
    onSuccess: (response) => {
      form.reset({
        ...dataToFormValues({
          settings: response.data,
          candidate_models_by_group: candidateModelsByGroup,
        }),
      })
      initializedRevisionRef.current = response.data.revision
      setConflictMessage('')
      toast.success('分组监控配置已保存')
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: SETTINGS_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: ['pricing', 'group-monitor'] })
    },
    onError: (error) => {
      if (isGroupMonitorConfigConflict(error)) {
        setConflictMessage('配置已被其他管理员更新，请刷新后重试')
        return
      }
      toast.error(
        error instanceof Error ? error.message : '分组监控配置保存失败'
      )
    },
  })
  const runMutation = useMutation({
    mutationFn: runChannelGroupMonitorNow,
    onSuccess: () => {
      toast.success('已提交立即探测任务')
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: SETTINGS_QUERY_KEY })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : '立即探测提交失败')
    },
  })
  const controlsDisabled =
    !props.data || saveMutation.isPending || Boolean(conflictMessage)
  const canRunSavedConfiguration =
    (props.data?.settings.groups.length ?? 0) > 0 && !form.formState.isDirty
  const requestsPerHour =
    intervalSeconds > 0 ? (groupValues.length * 3600) / intervalSeconds : 0

  const handleSubmit = form.handleSubmit((values) => {
    if (conflictMessage) return
    saveMutation.mutate(values)
  })

  function refreshConfiguration() {
    void queryClient.refetchQueries({ queryKey: SETTINGS_QUERY_KEY })
    void queryClient.refetchQueries({ queryKey: OVERVIEW_QUERY_KEY })
  }

  return (
    <Sheet
      open={props.open}
      onOpenChange={(open) => {
        if (!open && (saveMutation.isPending || runMutation.isPending)) return
        props.onOpenChange(open)
      }}
    >
      <SheetContent
        className={sideDrawerContentClassName('sm:max-w-2xl')}
        showCloseButton={!saveMutation.isPending && !runMutation.isPending}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='flex items-center gap-3'>
            <IconBadge tone='info' size='title'>
              <HugeiconsIcon icon={Settings02Icon} />
            </IconBadge>
            <span className='min-w-0 truncate'>配置分组监控</span>
          </SheetTitle>
          <SheetDescription className='mt-1'>
            保存的分类和监控分组将展示在用户页面，模型调用权限保持不变。
          </SheetDescription>
        </SheetHeader>

        <Form {...form}>
          <form
            id='channel-group-monitor-settings-form'
            className={sideDrawerFormClassName('min-w-0')}
            onSubmit={handleSubmit}
          >
            {conflictMessage ? (
              <Alert variant='destructive'>
                <HugeiconsIcon icon={Alert02Icon} />
                <AlertTitle>配置发生冲突</AlertTitle>
                <AlertDescription>{conflictMessage}</AlertDescription>
              </Alert>
            ) : null}

            <SideDrawerSection>
              <SideDrawerSectionHeader
                title='定时探测'
                description='每个分组使用正常路由选择发起一次真实文本请求'
                icon={<HugeiconsIcon icon={Settings02Icon} />}
                iconTone='primary'
              />
              <FormField
                control={form.control}
                name='enabled'
                render={({ field }) => (
                  <FormItem
                    className={sideDrawerSwitchItemClassName('border-t-0')}
                  >
                    <div className='min-w-0 space-y-1'>
                      <FormLabel>启用周期探测</FormLabel>
                      <FormDescription>
                        关闭后保留配置和历史，仍可由管理员手动探测
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={controlsDisabled}
                        aria-label='启用分组周期探测'
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='intervalSeconds'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>探测间隔</FormLabel>
                    <FormDescription>
                      单位为秒，范围 30 到 86400
                    </FormDescription>
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
                        disabled={controlsDisabled}
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
                          disabled={controlsDisabled}
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
                      成功率按此范围内的有效逻辑探测统计
                    </FormDescription>
                    <FormControl>
                      <div className='flex min-w-0 flex-wrap items-center gap-2'>
                        <Input
                          type='number'
                          min={1}
                          max={displayLimit}
                          step={1}
                          value={
                            Number.isFinite(field.value) ? field.value : ''
                          }
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                          disabled={controlsDisabled}
                          className='w-28 font-mono tabular-nums'
                          aria-label='状态展示数值'
                        />
                        <ToggleGroup
                          value={[displayUnit]}
                          onValueChange={(values) => {
                            const selected = values[0] as
                              | ChannelGroupMonitorDisplayUnit
                              | undefined
                            if (!selected) return
                            form.setValue('displayUnit', selected, {
                              shouldDirty: true,
                              shouldValidate: true,
                            })
                            const nextLimit =
                              CHANNEL_GROUP_MONITOR_DISPLAY_LIMITS[selected]
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
                              disabled={controlsDisabled}
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
            </SideDrawerSection>

            <SideDrawerSection>
              <SideDrawerSectionHeader title='展示设置' />
              <FormField
                control={form.control}
                name='showCacheRate'
                render={({ field }) => (
                  <FormItem
                    className={sideDrawerSwitchItemClassName('border-t-0')}
                  >
                    <div className='min-w-0 space-y-1'>
                      <FormLabel>显示缓存率</FormLabel>
                      <FormDescription>
                        在分组监控页展示近 24
                        小时实际请求的缓存命中率，无有效样本时显示暂无数据
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={controlsDisabled}
                        aria-label='显示缓存率'
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            <ChannelGroupMonitorCategoryEditor
              disabled={controlsDisabled}
              candidateModelsByGroup={candidateModelsByGroup}
              groupOrder={props.groupOrder}
            />
          </form>
        </Form>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <Button
            type='button'
            variant='outline'
            onClick={() => runMutation.mutate()}
            disabled={
              controlsDisabled ||
              runMutation.isPending ||
              !canRunSavedConfiguration ||
              groupValues.length === 0
            }
          >
            {runMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            )}
            立即探测
          </Button>
          {conflictMessage ? (
            <Button
              type='button'
              variant='outline'
              onClick={refreshConfiguration}
              disabled={saveMutation.isPending}
            >
              刷新配置
            </Button>
          ) : (
            <SheetClose
              render={
                <Button
                  variant='outline'
                  disabled={saveMutation.isPending || runMutation.isPending}
                />
              }
            >
              取消
            </SheetClose>
          )}
          <Button
            form='channel-group-monitor-settings-form'
            type='submit'
            disabled={controlsDisabled}
          >
            {saveMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : null}
            保存配置
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
