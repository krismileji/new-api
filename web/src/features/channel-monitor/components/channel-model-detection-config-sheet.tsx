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
  Alert02Icon,
  Delete02Icon,
  FingerPrintAddIcon,
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

import { channelModelDetectionClaimedModelLabel } from '../lib/model-detection'
import {
  isChannelModelDetectionConfigConflict,
  updateChannelModelDetectionConfig,
} from '../lib/model-detection-channel-api'
import {
  CHANNEL_MODEL_DETECTION_CLAIMED_MODELS,
  channelModelDetectionChannelToConfigFormValues,
  channelModelDetectionConfigResultToFormValues,
  channelModelDetectionExactModels,
  createChannelModelDetectionConfigSchema,
  createChannelModelDetectionConfigUpdateRequest,
  type ChannelModelDetectionConfigFormValues,
} from '../lib/model-detection-channel-schema'
import { channelModelDetectionRequestErrorMessage } from '../lib/model-detection-settings-api'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionChannelConfigResult,
  ChannelModelDetectionClaimedModel,
} from '../types-model-detection'

const OVERVIEW_QUERY_KEY = [
  'channel-monitor',
  'model-detection',
  'overview',
] as const

const CLAIMED_MODEL_ITEMS = CHANNEL_MODEL_DETECTION_CLAIMED_MODELS.map(
  (model) => ({
    value: model,
    label: channelModelDetectionClaimedModelLabel(model),
  })
)

export type ChannelModelDetectionConfigSheetProps = {
  channel: ChannelModelDetectionChannel | null
  detectorURLConfigured: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved?: (config: ChannelModelDetectionChannelConfigResult) => void
  onRefreshChannel?: (channelId: number) => void | Promise<void>
}

function claimedModelForRequest(
  requestModel: string
): ChannelModelDetectionClaimedModel {
  if (
    CHANNEL_MODEL_DETECTION_CLAIMED_MODELS.includes(
      requestModel as ChannelModelDetectionClaimedModel
    )
  ) {
    return requestModel as ChannelModelDetectionClaimedModel
  }
  return 'gpt-5.6-sol'
}

export function ChannelModelDetectionConfigSheet(
  props: ChannelModelDetectionConfigSheetProps
) {
  const queryClient = useQueryClient()
  const [conflictMessage, setConflictMessage] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const initializedChannelRef = useRef<number | null>(null)
  const supportedModels = useMemo(
    () =>
      channelModelDetectionExactModels(props.channel?.supported_models ?? []),
    [props.channel?.supported_models]
  )
  const requestModelItems = useMemo(
    () => supportedModels.map((model) => ({ value: model, label: model })),
    [supportedModels]
  )
  const schema = useMemo(
    () =>
      createChannelModelDetectionConfigSchema(
        supportedModels,
        props.detectorURLConfigured
      ),
    [props.detectorURLConfigured, supportedModels]
  )
  const form = useForm<ChannelModelDetectionConfigFormValues>({
    resolver: zodResolver(
      schema
    ) as Resolver<ChannelModelDetectionConfigFormValues>,
    defaultValues: props.channel
      ? channelModelDetectionChannelToConfigFormValues(props.channel)
      : {
          scheduleEnabled: false,
          targets: [
            {
              targetKey: '',
              requestModel: '',
              claimedModel: 'gpt-5.6-sol',
            },
          ],
          revision: 0,
        },
  })
  const targets = useFieldArray({ control: form.control, name: 'targets' })

  useEffect(() => {
    if (!props.open) {
      setConflictMessage('')
      initializedChannelRef.current = null
      return
    }
    if (!props.channel) return
    if (initializedChannelRef.current === props.channel.id) return
    form.reset(channelModelDetectionChannelToConfigFormValues(props.channel))
    initializedChannelRef.current = props.channel.id
    setConflictMessage('')
  }, [form, props.channel, props.open])

  const saveMutation = useMutation({
    mutationFn: (values: ChannelModelDetectionConfigFormValues) => {
      if (!props.channel) throw new Error('未选择需要配置的渠道')
      return updateChannelModelDetectionConfig(
        props.channel.id,
        createChannelModelDetectionConfigUpdateRequest(values)
      )
    },
    onSuccess: (saved) => {
      form.reset(channelModelDetectionConfigResultToFormValues(saved))
      setConflictMessage('')
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      toast.success('渠道模型检测配置已保存')
      props.onSaved?.(saved)
    },
    onError: (error) => {
      if (isChannelModelDetectionConfigConflict(error)) {
        setConflictMessage(
          channelModelDetectionRequestErrorMessage(error) ||
            '渠道配置已被其他管理员更新，请刷新后重试'
        )
        return
      }
      toast.error(channelModelDetectionRequestErrorMessage(error))
    },
  })

  const controlsDisabled = saveMutation.isPending || Boolean(conflictMessage)
  const targetCount = form.watch('targets').length

  async function refreshChannel() {
    if (!props.channel) return
    setRefreshing(true)
    try {
      await queryClient.refetchQueries({ queryKey: OVERVIEW_QUERY_KEY })
      await props.onRefreshChannel?.(props.channel.id)
      setConflictMessage('')
      props.onOpenChange(false)
    } finally {
      setRefreshing(false)
    }
  }

  const handleSubmit = form.handleSubmit((values) => {
    if (conflictMessage) return
    saveMutation.mutate(values)
  })

  return (
    <Sheet
      open={props.open}
      onOpenChange={(open) => {
        if (!open && saveMutation.isPending) return
        props.onOpenChange(open)
      }}
    >
      <SheetContent
        className={sideDrawerContentClassName('sm:max-w-2xl')}
        showCloseButton={!saveMutation.isPending}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='flex items-center gap-3'>
            <IconBadge tone='info' size='title'>
              <HugeiconsIcon icon={Settings02Icon} />
            </IconBadge>
            <span className='min-w-0 truncate'>配置模型检测目标</span>
          </SheetTitle>
          <SheetDescription className='mt-1'>
            {props.channel
              ? `${props.channel.name} #${props.channel.id} · 目标会固定到当前渠道执行`
              : '选择渠道后配置检测目标'}
          </SheetDescription>
        </SheetHeader>

        {props.channel ? (
          <Form {...form}>
            <form
              id='channel-model-detection-config-form'
              className={sideDrawerFormClassName('min-w-0 gap-6')}
              onSubmit={handleSubmit}
            >
              {conflictMessage ? (
                <Alert variant='destructive'>
                  <HugeiconsIcon icon={Alert02Icon} />
                  <AlertTitle>渠道配置已发生冲突</AlertTitle>
                  <AlertDescription>
                    {conflictMessage}
                    。为避免覆盖其他管理员的目标修改，本弹层不会自动合并。
                  </AlertDescription>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='col-span-full mt-2 w-fit'
                    disabled={refreshing}
                    onClick={() => void refreshChannel()}
                  >
                    {refreshing ? (
                      <Spinner data-icon='inline-start' />
                    ) : (
                      <HugeiconsIcon
                        icon={Refresh01Icon}
                        data-icon='inline-start'
                      />
                    )}
                    刷新渠道后重新打开
                  </Button>
                </Alert>
              ) : null}

              <SideDrawerSection>
                <SideDrawerSectionHeader
                  title='定时参与'
                  description='档位、周期、执行时间和时区由模型检测统一设置控制'
                  icon={<HugeiconsIcon icon={FingerPrintAddIcon} />}
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
                        <FormLabel>参加统一定时检测</FormLabel>
                        <FormDescription>
                          关闭后仍可手动选择档位并预览成本
                        </FormDescription>
                        {!props.detectorURLConfigured ? (
                          <p className='text-warning text-xs'>
                            尚未配置检测器地址，暂不能开启定时参与
                          </p>
                        ) : null}
                        <FormMessage />
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          disabled={
                            controlsDisabled ||
                            (!props.detectorURLConfigured && !field.value)
                          }
                          onCheckedChange={field.onChange}
                          aria-label='参加模型检测统一定时'
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              </SideDrawerSection>

              <SideDrawerSection>
                <SideDrawerSectionHeader
                  title='检测目标'
                  description='每个目标选择渠道实际请求模型，以及期望验证的 GPT-5.6 型号'
                  icon={<HugeiconsIcon icon={FingerPrintAddIcon} />}
                  iconTone='info'
                />

                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center gap-2'>
                    <span className='text-sm font-medium'>目标列表</span>
                    <Badge variant='outline'>{targetCount} / 10</Badge>
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    disabled={
                      controlsDisabled ||
                      targetCount >= 10 ||
                      supportedModels.length === 0
                    }
                    onClick={() => {
                      const requestModel = supportedModels[0] ?? ''
                      targets.append({
                        targetKey: '',
                        requestModel,
                        claimedModel: claimedModelForRequest(requestModel),
                      })
                    }}
                  >
                    <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
                    添加目标
                  </Button>
                </div>

                {supportedModels.length === 0 ? (
                  <Alert variant='destructive'>
                    <HugeiconsIcon icon={Alert02Icon} />
                    <AlertTitle>渠道没有可用的精确模型</AlertTitle>
                    <AlertDescription>
                      通配模型不能用于检测目标，请先在渠道配置中添加精确模型。
                    </AlertDescription>
                  </Alert>
                ) : null}

                <div className='flex min-w-0 flex-col gap-3'>
                  {targets.fields.map((target, index) => (
                    <article
                      key={target.id}
                      className='border-border/60 bg-muted/10 grid min-w-0 gap-3 rounded-lg border p-3 sm:grid-cols-[minmax(0,1.35fr)_minmax(0,0.85fr)_auto]'
                      data-slot='model-detection-config-target'
                    >
                      <FormField
                        control={form.control}
                        name={`targets.${index}.requestModel`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>请求模型</FormLabel>
                            <Select
                              items={requestModelItems}
                              value={field.value}
                              disabled={controlsDisabled}
                              onValueChange={(value) => {
                                if (value === null) return
                                field.onChange(value)
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`目标 ${index + 1} 请求模型`}
                                >
                                  <SelectValue placeholder='选择精确模型' />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {requestModelItems.map((item) => (
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
                        name={`targets.${index}.claimedModel`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>申报型号</FormLabel>
                            <Select
                              items={CLAIMED_MODEL_ITEMS}
                              value={field.value}
                              disabled={controlsDisabled}
                              onValueChange={(value) => {
                                if (value !== null) field.onChange(value)
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`目标 ${index + 1} 申报型号`}
                                >
                                  <SelectValue />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {CLAIMED_MODEL_ITEMS.map((item) => (
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

                      <div className='flex items-start justify-end pt-6'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          disabled={controlsDisabled || targetCount <= 1}
                          onClick={() => targets.remove(index)}
                          aria-label={`删除检测目标 ${index + 1}`}
                        >
                          <HugeiconsIcon
                            icon={Delete02Icon}
                            aria-hidden='true'
                          />
                        </Button>
                      </div>
                    </article>
                  ))}
                </div>
                <FormField
                  control={form.control}
                  name='targets'
                  render={() => (
                    <FormItem>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SideDrawerSection>

              <SideDrawerSection>
                <SideDrawerSectionHeader title='保存说明' />
                <div className='text-muted-foreground space-y-1 text-xs leading-5'>
                  <p>已有目标的内部标识会原样保留；新目标由服务端生成标识。</p>
                  <p>成本估算只读取保存后的目标，未保存修改不会进入估算。</p>
                </div>
              </SideDrawerSection>
            </form>
          </Form>
        ) : (
          <div className='flex flex-1 items-center justify-center px-6 text-sm'>
            未选择渠道
          </div>
        )}

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={
              <Button variant='outline' disabled={saveMutation.isPending} />
            }
          >
            取消
          </SheetClose>
          <Button
            form='channel-model-detection-config-form'
            type='submit'
            disabled={
              !props.channel || controlsDisabled || supportedModels.length === 0
            }
          >
            {saveMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : null}
            保存渠道配置
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
