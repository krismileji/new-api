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
  Add01Icon,
  Activity01Icon,
  Alert02Icon,
  ArrowDown01Icon,
  ArrowUp01Icon,
  Delete02Icon,
  Refresh01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useFieldArray, useForm, type Resolver } from 'react-hook-form'
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
import { Badge } from '@/components/ui/badge'
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
  open: boolean
  onOpenChange: (open: boolean) => void
}

function dataToFormValues(
  data: ChannelGroupMonitorSettingsResponse | undefined
): ChannelGroupMonitorConfigFormValues {
  const settings = data?.settings
  return {
    enabled: settings?.enabled ?? false,
    groups:
      settings?.groups.map((group) => ({
        groupName: group.group_name,
        probeModel: group.probe_model,
        displayInitial: group.display_initial ?? '',
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
  const groups = useFieldArray({ control: form.control, name: 'groups' })
  const displayUnit = form.watch('displayUnit')
  const displayValue = form.watch('displayValue')
  const intervalSeconds = form.watch('intervalSeconds')
  const groupValues = form.watch('groups')
  const displayLimit = CHANNEL_GROUP_MONITOR_DISPLAY_LIMITS[displayUnit]
  const candidateModelsByGroup =
    props.data?.candidate_models_by_group ?? EMPTY_CANDIDATE_MODELS_BY_GROUP
  const availableGroupItems = useMemo(
    () =>
      Object.keys(candidateModelsByGroup)
        .filter(
          (groupName) =>
            !groupValues.some((group) => group.groupName === groupName)
        )
        .map((groupName) => ({ value: groupName, label: groupName })),
    [candidateModelsByGroup, groupValues]
  )

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
        groups: values.groups.map((group) => ({
          group_name: group.groupName,
          probe_model: group.probeModel,
          display_initial: group.displayInitial.trim(),
        })),
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

  function moveGroup(index: number, direction: -1 | 1) {
    const targetIndex = index + direction
    if (targetIndex < 0 || targetIndex >= groups.fields.length) return
    groups.move(index, targetIndex)
  }

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
            通过真实请求验证用户可见分组的当前可用性
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
              <SideDrawerSectionHeader
                title='监控分组'
                description='按列表顺序执行；每个分组需要指定一个当前可用的具体文本模型'
                icon={<HugeiconsIcon icon={Activity01Icon} />}
                iconTone='info'
              />
              <div className='flex items-center justify-between gap-3'>
                <Badge variant='outline'>{groups.fields.length} / 100</Badge>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={
                    controlsDisabled ||
                    availableGroupItems.length === 0 ||
                    groups.fields.length >= 100
                  }
                  onClick={() => {
                    const groupName = availableGroupItems[0]?.value
                    if (!groupName) return
                    groups.append({
                      groupName,
                      probeModel: candidateModelsByGroup[groupName]?.[0] ?? '',
                      displayInitial: '',
                    })
                  }}
                >
                  <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
                  添加分组
                </Button>
              </div>
              {groups.fields.length === 0 ? (
                <Alert>
                  <HugeiconsIcon icon={Alert02Icon} />
                  <AlertTitle>尚未配置监控分组</AlertTitle>
                  <AlertDescription>
                    保存后用户侧才会展示已配置且有效的分组。
                  </AlertDescription>
                </Alert>
              ) : null}
              <div className='flex min-w-0 flex-col gap-3'>
                {groups.fields.map((group, index) => {
                  const currentGroupName = groupValues[index]?.groupName ?? ''
                  const groupItems = [
                    { value: currentGroupName, label: currentGroupName },
                    ...availableGroupItems,
                  ].filter(
                    (option, optionIndex, options) =>
                      option.value &&
                      options.findIndex(
                        (candidate) => candidate.value === option.value
                      ) === optionIndex
                  )
                  const availableModelNames =
                    candidateModelsByGroup[currentGroupName] ?? []
                  const configuredProbeModel =
                    groupValues[index]?.probeModel?.trim() ?? ''
                  const modelNames =
                    configuredProbeModel &&
                    !availableModelNames.includes(configuredProbeModel)
                      ? [...availableModelNames, configuredProbeModel]
                      : availableModelNames
                  const modelItems = modelNames.map((modelName) => ({
                    value: modelName,
                    label: modelName,
                  }))
                  return (
                    <article
                      key={group.id}
                      className='border-border/60 bg-muted/10 grid min-w-0 gap-3 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_5rem_auto]'
                    >
                      <FormField
                        control={form.control}
                        name={`groups.${index}.groupName`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>分组</FormLabel>
                            <Select
                              items={groupItems}
                              value={field.value || null}
                              disabled={controlsDisabled}
                              onValueChange={(value) => {
                                if (value == null) return
                                field.onChange(value)
                                form.setValue(
                                  `groups.${index}.probeModel`,
                                  candidateModelsByGroup[value]?.[0] ?? '',
                                  { shouldDirty: true, shouldValidate: true }
                                )
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`第 ${index + 1} 个监控分组`}
                                >
                                  <SelectValue placeholder='选择分组' />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {groupItems.map((item) => (
                                    <SelectItem
                                      key={item.value}
                                      value={item.value}
                                    >
                                      {item.label}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`groups.${index}.probeModel`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>探测模型</FormLabel>
                            <Select
                              items={modelItems}
                              value={field.value || null}
                              disabled={
                                controlsDisabled || modelItems.length === 0
                              }
                              onValueChange={(value) => {
                                if (value !== null) field.onChange(value)
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`${currentGroupName || '当前分组'}的探测模型`}
                                >
                                  <SelectValue placeholder='选择具体模型' />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {modelItems.map((item) => (
                                    <SelectItem
                                      key={item.value}
                                      value={item.value}
                                    >
                                      {item.label}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`groups.${index}.displayInitial`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>展示字</FormLabel>
                            <FormControl>
                              <Input
                                value={field.value}
                                maxLength={2}
                                placeholder='默认'
                                disabled={controlsDisabled}
                                aria-label={`${currentGroupName || '当前分组'}的展示字`}
                                onChange={(event) => {
                                  const value = event.target.value.trim()
                                  field.onChange(
                                    [...value].slice(0, 1).join('')
                                  )
                                }}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <div className='flex items-end justify-end gap-1'>
                        <Button
                          type='button'
                          variant='outline'
                          size='icon-sm'
                          onClick={() => moveGroup(index, -1)}
                          disabled={controlsDisabled || index === 0}
                          aria-label={`上移 ${currentGroupName}`}
                        >
                          <HugeiconsIcon icon={ArrowUp01Icon} />
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='icon-sm'
                          onClick={() => moveGroup(index, 1)}
                          disabled={
                            controlsDisabled ||
                            index === groups.fields.length - 1
                          }
                          aria-label={`下移 ${currentGroupName}`}
                        >
                          <HugeiconsIcon icon={ArrowDown01Icon} />
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='icon-sm'
                          onClick={() => groups.remove(index)}
                          disabled={controlsDisabled}
                          aria-label={`移除 ${currentGroupName}`}
                        >
                          <HugeiconsIcon icon={Delete02Icon} />
                        </Button>
                      </div>
                    </article>
                  )
                })}
              </div>
              <FormField
                control={form.control}
                name='groups'
                render={() => (
                  <FormItem>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>
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
              groups.fields.length === 0
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
