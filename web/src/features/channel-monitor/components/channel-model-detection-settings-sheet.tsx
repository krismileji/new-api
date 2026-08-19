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
  CalendarClockIcon,
  Delete02Icon,
  Link01Icon,
  Refresh01Icon,
  Settings02Icon,
  TestTube01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { formatTimestampToDate } from '@/lib/format'

import { channelModelDetectionPresetLabel } from '../lib/model-detection'
import {
  channelModelDetectionRequestErrorMessage,
  channelModelDetectionServiceFromError,
  getChannelModelDetectionSettings,
  isChannelModelDetectionSettingsConflict,
  testChannelModelDetectionService,
  updateChannelModelDetectionSettings,
} from '../lib/model-detection-settings-api'
import {
  CHANNEL_MODEL_DETECTION_DISPLAY_LIMITS,
  CHANNEL_MODEL_DETECTION_SETTINGS_EMPTY_VALUES,
  CHANNEL_MODEL_DETECTION_SETTINGS_QUERY_KEY,
  channelModelDetectionSettingsSchema,
  channelModelDetectionSettingsToFormValues,
  createChannelModelDetectionSettingsUpdateRequest,
  type ChannelModelDetectionSettingsFormValues,
} from '../lib/model-detection-settings-schema'
import { CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS } from '../lib/query-options'
import type {
  ChannelModelDetectionDetectorService,
  ChannelModelDetectionDisplayUnit,
  ChannelModelDetectionPreset,
  ChannelModelDetectionSettings,
} from '../types-model-detection'

const PRESET_OPTIONS = [
  { value: 'low', label: '低档', description: '更少请求，适合高频巡检' },
  { value: 'medium', label: '中档', description: '默认平衡档位' },
  { value: 'high', label: '高档', description: '请求更多，成本更高' },
] as const satisfies ReadonlyArray<{
  value: ChannelModelDetectionPreset
  label: string
  description: string
}>

const DISPLAY_UNIT_OPTIONS = [
  { value: 'minute', label: '分钟' },
  { value: 'hour', label: '小时' },
  { value: 'day', label: '天' },
] as const satisfies ReadonlyArray<{
  value: ChannelModelDetectionDisplayUnit
  label: string
}>

export type ChannelModelDetectionSettingsSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved?: (settings: ChannelModelDetectionSettings) => void
}

function detectorStateLabel(service: ChannelModelDetectionDetectorService) {
  if (service.state === 'available') return '连接正常'
  if (service.state === 'degraded') return '部分能力不可用'
  if (service.state === 'offline') return '连接失败'
  if (service.state === 'incompatible') return '接口不兼容'
  if (service.state === 'unconfigured') return '尚未配置'
  return '状态未知'
}

function SettingSummary(props: {
  label: string
  value: string
  valueTitle?: string
}) {
  return (
    <div className='grid min-w-0 grid-cols-[112px_minmax(0,1fr)] gap-3 text-sm'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span
        className='min-w-0 text-right font-medium break-words'
        title={props.valueTitle}
      >
        {props.value}
      </span>
    </div>
  )
}

export function ChannelModelDetectionSettingsSheet(
  props: ChannelModelDetectionSettingsSheetProps
) {
  const queryClient = useQueryClient()
  const [conflictMessage, setConflictMessage] = useState('')
  const [connectionResult, setConnectionResult] =
    useState<ChannelModelDetectionDetectorService | null>(null)
  const form = useForm<ChannelModelDetectionSettingsFormValues>({
    resolver: zodResolver(
      channelModelDetectionSettingsSchema
    ) as Resolver<ChannelModelDetectionSettingsFormValues>,
    defaultValues: CHANNEL_MODEL_DETECTION_SETTINGS_EMPTY_VALUES,
  })
  const query = useQuery({
    queryKey: CHANNEL_MODEL_DETECTION_SETTINGS_QUERY_KEY,
    queryFn: getChannelModelDetectionSettings,
    enabled: props.open,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  })
  const settings = query.data
  const detectorURL = form.watch('detectorURL')
  const clearDetectorURL = form.watch('clearDetectorURL')
  const clearRelayURL = form.watch('clearRelayURL')
  const scheduleEnabled = form.watch('scheduleEnabled')
  const scheduledPreset = form.watch('scheduledPreset')
  const displayValue = form.watch('displayValue')
  const displayUnit = form.watch('displayUnit')
  const displayLimit = CHANNEL_MODEL_DETECTION_DISPLAY_LIMITS[displayUnit]
  const requiresHighConfirmation = scheduleEnabled && scheduledPreset === 'high'
  const normalizedDetectorURL = detectorURL.trim()
  const savedDetectorURL = (
    settings?.pending_detector_url ||
    settings?.detector_url ||
    ''
  ).trim()
  const testedAddressNeedsSave =
    connectionResult !== null &&
    normalizedDetectorURL !== '' &&
    normalizedDetectorURL !== savedDetectorURL
  const successfulTestNeedsSave =
    testedAddressNeedsSave &&
    (connectionResult?.state === 'available' ||
      connectionResult?.state === 'degraded')

  let detectorConfigurationStatus = '尚未保存地址'
  if (settings?.pending_detector_url_configured) {
    detectorConfigurationStatus = '待切换地址已保存'
  } else if (settings?.detector_url_configured) {
    detectorConfigurationStatus = settings.connection_test_required
      ? '地址已保存，需测试连接'
      : '地址已保存'
  }

  let connectionResultTitle = ''
  if (connectionResult) {
    connectionResultTitle = detectorStateLabel(connectionResult)
    if (successfulTestNeedsSave) {
      connectionResultTitle += '，地址尚未保存'
    }
  }

  useEffect(() => {
    if (!props.open) {
      setConflictMessage('')
      setConnectionResult(null)
      form.reset(CHANNEL_MODEL_DETECTION_SETTINGS_EMPTY_VALUES)
      return
    }
    if (!settings) return
    form.reset(channelModelDetectionSettingsToFormValues(settings))
    setConflictMessage('')
    setConnectionResult(null)
  }, [form, props.open, settings])

  const savedServiceTestMutation = useMutation({
    mutationFn: (_saved: ChannelModelDetectionSettings) =>
      testChannelModelDetectionService(),
    onSuccess: (service, saved) => {
      setConnectionResult(service)
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'overview'],
      })
      if (service.state === 'available') {
        toast.success('模型检测统一设置已保存，检测器连接正常')
      } else {
        toast.success('模型检测统一设置已保存，检测器连接检查完成')
      }
      props.onSaved?.(saved)
    },
    onError: (error, saved) => {
      setConnectionResult(channelModelDetectionServiceFromError(error))
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'overview'],
      })
      toast.error(
        `模型检测统一设置已保存，但检测器连接检查失败：${channelModelDetectionRequestErrorMessage(error)}`
      )
      props.onSaved?.(saved)
    },
  })
  const saveMutation = useMutation({
    mutationFn: updateChannelModelDetectionSettings,
    onSuccess: (saved) => {
      queryClient.setQueryData(
        CHANNEL_MODEL_DETECTION_SETTINGS_QUERY_KEY,
        saved
      )
      form.reset(channelModelDetectionSettingsToFormValues(saved))
      setConflictMessage('')
      setConnectionResult(null)
      if (
        saved.connection_test_required &&
        saved.detector_url_configured &&
        !saved.detector_url_switch_pending
      ) {
        savedServiceTestMutation.mutate(saved)
        return
      }
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'overview'],
      })
      toast.success('模型检测统一设置已保存')
      props.onSaved?.(saved)
    },
    onError: (error) => {
      if (isChannelModelDetectionSettingsConflict(error)) {
        setConflictMessage(
          channelModelDetectionRequestErrorMessage(error) ||
            '设置已被其他管理员更新，请刷新后重试'
        )
        return
      }
      toast.error(channelModelDetectionRequestErrorMessage(error))
    },
  })
  const testMutation = useMutation({
    mutationFn: testChannelModelDetectionService,
    onSuccess: (service) => {
      setConnectionResult(service)
      toast.success('检测器连接测试完成')
    },
    onError: (error) => {
      setConnectionResult(channelModelDetectionServiceFromError(error))
      toast.error(channelModelDetectionRequestErrorMessage(error))
    },
  })

  const pending =
    saveMutation.isPending ||
    savedServiceTestMutation.isPending ||
    testMutation.isPending
  const controlsDisabled = pending || query.isFetching

  async function refreshSettings() {
    const result = await query.refetch()
    if (!result.isSuccess || !result.data) {
      toast.error(channelModelDetectionRequestErrorMessage(result.error))
      return
    }
    form.reset(channelModelDetectionSettingsToFormValues(result.data))
    setConflictMessage('')
    setConnectionResult(null)
  }

  const handleSubmit = form.handleSubmit((values) => {
    if (conflictMessage) return
    saveMutation.mutate(
      createChannelModelDetectionSettingsUpdateRequest(values)
    )
  })

  async function handleTestConnection() {
    if (!(await form.trigger('detectorURL'))) return
    const value = form.getValues('detectorURL').trim()
    if (!value) return
    testMutation.mutate(value)
  }

  let content
  if (query.isPending && !settings) {
    content = (
      <div className='flex flex-1 items-center justify-center gap-2 px-6 text-sm'>
        <Spinner />
        正在加载统一设置
      </div>
    )
  } else if (query.isError && !settings) {
    content = (
      <div className='flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center'>
        <IconBadge tone='destructive' size='lg'>
          <HugeiconsIcon icon={Alert02Icon} />
        </IconBadge>
        <div>
          <p className='font-medium'>统一设置加载失败</p>
          <p className='text-muted-foreground mt-1 text-sm'>
            {channelModelDetectionRequestErrorMessage(query.error)}
          </p>
        </div>
        <Button variant='outline' onClick={() => void refreshSettings()}>
          <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
          重试
        </Button>
      </div>
    )
  } else if (settings) {
    content = (
      <Form {...form}>
        <form
          id='channel-model-detection-settings-form'
          className={sideDrawerFormClassName('min-w-0 gap-6')}
          onSubmit={handleSubmit}
        >
          {conflictMessage ? (
            <Alert variant='destructive'>
              <HugeiconsIcon icon={Alert02Icon} />
              <AlertTitle>设置已发生冲突</AlertTitle>
              <AlertDescription>{conflictMessage}</AlertDescription>
              <Button
                type='button'
                size='sm'
                variant='outline'
                className='col-span-full mt-2 w-fit'
                disabled={query.isFetching}
                onClick={() => void refreshSettings()}
              >
                {query.isFetching ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon
                    icon={Refresh01Icon}
                    data-icon='inline-start'
                  />
                )}
                刷新设置
              </Button>
            </Alert>
          ) : null}

          <SideDrawerSection>
            <SideDrawerSectionHeader
              title='检测器服务'
              description='显示当前配置的完整地址，并支持手动测试连接'
              icon={<HugeiconsIcon icon={Link01Icon} />}
              iconTone='info'
            />
            <div className='border-border/60 bg-muted/20 space-y-2 rounded-lg border p-3'>
              <SettingSummary
                label='当前地址'
                value={
                  settings.detector_url_configured
                    ? settings.detector_url_masked
                    : '未配置'
                }
                valueTitle={settings.detector_url_masked || undefined}
              />
              {settings.pending_detector_url_configured ? (
                <SettingSummary
                  label='待切换地址'
                  value={settings.pending_detector_url_masked}
                  valueTitle={settings.pending_detector_url_masked}
                />
              ) : null}
              <SettingSummary
                label='配置状态'
                value={detectorConfigurationStatus}
              />
              <SettingSummary
                label='内部 Relay'
                value={
                  settings.relay_url_configured ? settings.relay_url : '未配置'
                }
                valueTitle={settings.relay_url || undefined}
              />
            </div>

            <FormField
              control={form.control}
              name='detectorURL'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>新检测器地址</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type='url'
                      inputMode='url'
                      autoComplete='off'
                      spellCheck={false}
                      disabled={controlsDisabled || clearDetectorURL}
                      placeholder='http://127.0.0.1:8000'
                      aria-label='新检测器地址'
                      onChange={(event) => {
                        field.onChange(event)
                        setConnectionResult(null)
                        if (event.target.value) {
                          form.setValue('clearDetectorURL', false, {
                            shouldValidate: true,
                          })
                        }
                      }}
                    />
                  </FormControl>
                  <FormDescription>
                    保持为空会保留当前地址；仅允许后端校验通过的回环或私网目标
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='clearDetectorURL'
              render={({ field }) => (
                <FormItem className='border-border/60 flex flex-row items-center justify-between gap-3 rounded-lg border p-3'>
                  <div className='min-w-0 space-y-1'>
                    <FormLabel>清除已保存地址</FormLabel>
                    <FormDescription>
                      保存后停止新检测；不会中断已冻结地址的运行任务
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      disabled={controlsDisabled}
                      onCheckedChange={(checked) => {
                        field.onChange(checked === true)
                        setConnectionResult(null)
                        if (checked === true) {
                          form.setValue('detectorURL', '', {
                            shouldValidate: true,
                          })
                        }
                      }}
                      aria-label='清除已保存检测器地址'
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
              <Button
                type='button'
                variant='outline'
                disabled={
                  controlsDisabled || clearDetectorURL || !detectorURL.trim()
                }
                onClick={() => void handleTestConnection()}
              >
                {testMutation.isPending ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon
                    icon={TestTube01Icon}
                    data-icon='inline-start'
                  />
                )}
                测试连接
              </Button>
              <p className='text-muted-foreground text-xs'>
                测试输入框中的地址，不会保存设置
              </p>
            </div>
            {connectionResult ? (
              <Alert
                variant={
                  connectionResult.state === 'available'
                    ? 'default'
                    : 'destructive'
                }
              >
                <HugeiconsIcon icon={TestTube01Icon} />
                <AlertTitle>{connectionResultTitle}</AlertTitle>
                <AlertDescription className='flex flex-col gap-1'>
                  <span>
                    {connectionResult.compatibility_message ||
                      connectionResult.last_error ||
                      `已检查 ${Object.keys(connectionResult.estimates).length} 个档位`}
                  </span>
                  {successfulTestNeedsSave ? (
                    <span>
                      本次只测试了输入地址；点击“保存设置”后，模型检测才会使用该地址。
                    </span>
                  ) : null}
                </AlertDescription>
              </Alert>
            ) : null}

            <div className='border-border/60 space-y-4 border-t pt-4'>
              <div className='space-y-1'>
                <p className='text-sm font-medium'>检测器访问平台</p>
                <p className='text-muted-foreground text-xs'>
                  检测器通过内部 Relay 请求指定渠道，平台据此统计真实上游成本
                </p>
              </div>
              <FormField
                control={form.control}
                name='relayURL'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>内部 Relay 地址</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        type='url'
                        inputMode='url'
                        autoComplete='off'
                        spellCheck={false}
                        disabled={controlsDisabled || clearRelayURL}
                        placeholder='https://api.example.com/internal/model-detector/v1'
                        aria-label='内部 Relay 地址'
                        onChange={(event) => {
                          field.onChange(event)
                          if (event.target.value) {
                            form.setValue('clearRelayURL', false, {
                              shouldValidate: true,
                            })
                          }
                        }}
                      />
                    </FormControl>
                    <FormDescription>
                      必须以 /internal/model-detector/v1
                      结尾；留空会保留当前设置
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='clearRelayURL'
                render={({ field }) => (
                  <FormItem className='border-border/60 flex flex-row items-center justify-between gap-3 rounded-lg border p-3'>
                    <div className='min-w-0 space-y-1'>
                      <FormLabel>清除已保存 Relay 地址</FormLabel>
                      <FormDescription>
                        清除后将无法启动新的模型检测任务
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Checkbox
                        checked={field.value}
                        disabled={controlsDisabled}
                        onCheckedChange={(checked) => {
                          field.onChange(checked === true)
                          if (checked === true) {
                            form.setValue('relayURL', '', {
                              shouldValidate: true,
                            })
                          }
                        }}
                        aria-label='清除已保存内部 Relay 地址'
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </div>
          </SideDrawerSection>

          <SideDrawerSection>
            <SideDrawerSectionHeader
              title='定时检测'
              description='全部渠道共用同一档位和检测周期'
              icon={<HugeiconsIcon icon={CalendarClockIcon} />}
              iconTone='primary'
            />
            <FormField
              control={form.control}
              name='scheduleEnabled'
              render={({ field }) => (
                <FormItem
                  className={sideDrawerSwitchItemClassName('border-t-0')}
                >
                  <div className='min-w-0 space-y-1'>
                    <FormLabel>启用统一定时检测</FormLabel>
                    <FormDescription>
                      仅影响已在各渠道配置中参加定时检测的目标
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      disabled={controlsDisabled}
                      onCheckedChange={field.onChange}
                      aria-label='启用统一定时检测'
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='scheduledPreset'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>定时检测档位</FormLabel>
                  <FormControl>
                    <ToggleGroup
                      value={[field.value]}
                      onValueChange={(values) => {
                        const selected = values.find(
                          (value) => value !== field.value
                        ) as ChannelModelDetectionPreset | undefined
                        if (!selected) return
                        field.onChange(selected)
                        form.setValue('confirmHighCost', false, {
                          shouldValidate: false,
                        })
                      }}
                      variant='outline'
                      spacing={2}
                      disabled={controlsDisabled}
                      className='grid w-full grid-cols-3'
                      aria-label='选择定时检测档位'
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

            <FormField
              control={form.control}
              name='intervalValue'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>检测周期</FormLabel>
                  <div className='flex min-w-0 gap-2'>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        value={Number.isFinite(field.value) ? field.value : ''}
                        disabled={controlsDisabled}
                        className='min-w-0 font-mono tabular-nums'
                        aria-label='统一检测周期数值'
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormField
                      control={form.control}
                      name='intervalUnit'
                      render={({ field: unitField }) => (
                        <ToggleGroup
                          value={[unitField.value]}
                          onValueChange={(values) => {
                            const selected = values[0]
                            if (selected) unitField.onChange(selected)
                          }}
                          variant='outline'
                          spacing={0}
                          disabled={controlsDisabled}
                          aria-label='统一检测周期单位'
                        >
                          <ToggleGroupItem value='minute'>分钟</ToggleGroupItem>
                          <ToggleGroupItem value='hour'>小时</ToggleGroupItem>
                        </ToggleGroup>
                      )}
                    />
                  </div>
                  <FormDescription>
                    周期按整分钟保存；小时会自动换算为分钟
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            {requiresHighConfirmation ? (
              <Alert variant='destructive'>
                <HugeiconsIcon icon={Alert02Icon} />
                <AlertTitle>高档定时检测会产生更多请求和成本</AlertTitle>
                <AlertDescription>
                  每次保存高档定时设置都必须重新确认。本确认不会保存为长期偏好。
                </AlertDescription>
                <FormField
                  control={form.control}
                  name='confirmHighCost'
                  render={({ field }) => (
                    <FormItem className='col-span-full mt-2 flex flex-row items-start gap-2'>
                      <FormControl>
                        <Checkbox
                          checked={field.value}
                          disabled={controlsDisabled}
                          onCheckedChange={(checked) =>
                            field.onChange(checked === true)
                          }
                          aria-label='确认高档定时检测成本风险'
                        />
                      </FormControl>
                      <div className='space-y-1'>
                        <FormLabel>我确认本次保存使用高档定时检测</FormLabel>
                        <FormMessage />
                      </div>
                    </FormItem>
                  )}
                />
              </Alert>
            ) : null}
          </SideDrawerSection>

          <SideDrawerSection>
            <SideDrawerSectionHeader
              title='结果展示'
              description='分钟、小时或天'
              icon={<HugeiconsIcon icon={Settings02Icon} />}
              iconTone='info'
            />
            <FormField
              control={form.control}
              name='displayValue'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>模型检测展示范围</FormLabel>
                  <FormDescription>
                    分钟最多 60、小时最多 24、天最多 30
                  </FormDescription>
                  <FormControl>
                    <div className='flex min-w-0 flex-wrap items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={displayLimit}
                        step={1}
                        value={Number.isFinite(field.value) ? field.value : ''}
                        disabled={controlsDisabled}
                        className='w-28 font-mono tabular-nums'
                        aria-label='模型检测展示范围数值'
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                      <FormField
                        control={form.control}
                        name='displayUnit'
                        render={({ field: unitField }) => (
                          <ToggleGroup
                            value={[unitField.value]}
                            onValueChange={(values) => {
                              const selected = values[0] as
                                | ChannelModelDetectionDisplayUnit
                                | undefined
                              if (!selected) return
                              unitField.onChange(selected)
                              const nextLimit =
                                CHANNEL_MODEL_DETECTION_DISPLAY_LIMITS[selected]
                              if (displayValue > nextLimit) {
                                form.setValue('displayValue', nextLimit, {
                                  shouldDirty: true,
                                  shouldValidate: true,
                                })
                              }
                            }}
                            variant='outline'
                            spacing={0}
                            disabled={controlsDisabled}
                            aria-label='模型检测展示范围单位'
                          >
                            {DISPLAY_UNIT_OPTIONS.map((option) => (
                              <ToggleGroupItem
                                key={option.value}
                                value={option.value}
                                aria-label={`模型检测展示范围单位：${option.label}`}
                              >
                                {option.label}
                              </ToggleGroupItem>
                            ))}
                          </ToggleGroup>
                        )}
                      />
                    </div>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SideDrawerSection>

          <SideDrawerSection>
            <SideDrawerSectionHeader
              title='当前计划摘要'
              icon={<HugeiconsIcon icon={Settings02Icon} />}
              iconTone='neutral'
            />
            <div className='space-y-2'>
              <SettingSummary
                label='定时状态'
                value={settings.schedule_enabled ? '已启用' : '已停用'}
              />
              <SettingSummary
                label='当前档位'
                value={channelModelDetectionPresetLabel(
                  settings.scheduled_preset
                )}
              />
              <SettingSummary
                label='检测周期'
                value={
                  settings.interval_minutes % 60 === 0
                    ? `每 ${settings.interval_minutes / 60} 小时`
                    : `每 ${settings.interval_minutes} 分钟`
                }
              />
              <SettingSummary
                label='展示范围'
                value={`近 ${settings.display_value} ${DISPLAY_UNIT_OPTIONS.find((option) => option.value === settings.display_unit)?.label ?? '天'}`}
              />
              <SettingSummary
                label='下一批次'
                value={
                  settings.next_batch_at > 0
                    ? formatTimestampToDate(settings.next_batch_at)
                    : '尚未排期'
                }
              />
              <SettingSummary
                label='最近更新'
                value={formatTimestampToDate(settings.updated_at)}
              />
            </div>
            {settings.detector_url_switch_pending ? (
              <Badge variant='warning' className='w-fit'>
                当前任务结束后切换检测器地址
              </Badge>
            ) : null}
          </SideDrawerSection>
        </form>
      </Form>
    )
  }

  let saveStartIcon = null
  if (saveMutation.isPending || savedServiceTestMutation.isPending) {
    saveStartIcon = <Spinner data-icon='inline-start' />
  } else if (clearDetectorURL || clearRelayURL) {
    saveStartIcon = (
      <HugeiconsIcon icon={Delete02Icon} data-icon='inline-start' />
    )
  }

  return (
    <Sheet
      open={props.open}
      onOpenChange={(open) => {
        if (!open && pending) return
        props.onOpenChange(open)
      }}
    >
      <SheetContent
        className={sideDrawerContentClassName('sm:max-w-xl')}
        showCloseButton={!pending}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='flex items-center gap-3'>
            <IconBadge tone='info' size='title'>
              <HugeiconsIcon icon={Settings02Icon} />
            </IconBadge>
            <span>模型检测统一设置</span>
          </SheetTitle>
          <SheetDescription className='mt-1'>
            配置独立检测器服务以及全部渠道共用的定时检测策略
          </SheetDescription>
        </SheetHeader>

        {content}

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' disabled={pending} />}>
            取消
          </SheetClose>
          <Button
            form='channel-model-detection-settings-form'
            type='submit'
            disabled={
              !settings ||
              controlsDisabled ||
              Boolean(conflictMessage) ||
              (requiresHighConfirmation && !form.watch('confirmHighCost'))
            }
          >
            {saveStartIcon}
            {savedServiceTestMutation.isPending ? '验证检测器' : '保存设置'}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
