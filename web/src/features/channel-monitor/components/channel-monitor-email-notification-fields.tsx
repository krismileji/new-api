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
import { CheckListIcon, MailOpen01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

import { previewChannelMonitorNotificationEmail } from '../api'
import {
  CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPE_OPTIONS,
  DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
} from '../lib/email-notification'
import { handleChannelMonitorMutationError } from '../lib/error'
import type { ChannelMonitorSettingsFormValues } from '../lib/schema'

type ChannelMonitorEmailNotificationFieldsProps = {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}

export function ChannelMonitorEmailNotificationFields(
  props: ChannelMonitorEmailNotificationFieldsProps
) {
  const [previewOpen, setPreviewOpen] = useState(false)
  const emailNotificationEnabled = useWatch({
    control: props.form.control,
    name: 'emailNotificationEnabled',
  })
  const selectedTypes = useWatch({
    control: props.form.control,
    name: 'emailNotificationTypes',
  })
  const previewMutation = useMutation({
    mutationFn: previewChannelMonitorNotificationEmail,
    onError: handleChannelMonitorMutationError,
  })

  const openPreview = () => {
    if (!emailNotificationEnabled || selectedTypes.length === 0) return

    previewMutation.reset()
    setPreviewOpen(true)
    previewMutation.mutate({ notificationTypes: selectedTypes })
  }

  return (
    <>
      <FormField
        control={props.form.control}
        name='emailNotificationEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between gap-4'>
            <div className='flex flex-col gap-1'>
              <FormLabel>邮件通知</FormLabel>
              <FormDescription>
                开启后，仅在选定类型出现时发送渠道监控邮件
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                aria-label='开启渠道监控邮件通知'
              />
            </FormControl>
          </FormItem>
        )}
      />

      <FormField
        control={props.form.control}
        name='notificationEmail'
        render={({ field }) => (
          <FormItem>
            <FormLabel>通知邮箱</FormLabel>
            <FormControl>
              <Input
                type='email'
                disabled={!emailNotificationEnabled}
                autoComplete='email'
                placeholder='name@example.com'
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(
                  props.form.formState.errors.notificationEmail
                )}
              />
            </FormControl>
            <FormDescription>
              关闭通知后仍会保留邮箱地址；邮件发送使用系统 SMTP 设置
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={props.form.control}
        name='emailNotificationTypes'
        render={({ field, fieldState }) => {
          const selected = field.value
          return (
            <FormItem>
              <FieldSet
                data-disabled={!emailNotificationEnabled || undefined}
                data-invalid={fieldState.invalid || undefined}
                className='gap-3 rounded-md border p-3'
              >
                <FieldLegend variant='label'>接收类型</FieldLegend>
                <FieldDescription>
                  只发送已勾选的分类；未勾选的事件仍会保留在执行记录中
                </FieldDescription>

                <FieldGroup
                  data-slot='checkbox-group'
                  className='grid gap-2 sm:grid-cols-2'
                >
                  {CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPE_OPTIONS.map(
                    (option) => {
                      const checkboxId = `channel-monitor-email-type-${option.value}`
                      const checked = selected.includes(option.value)
                      return (
                        <Field
                          key={option.value}
                          orientation='horizontal'
                          data-disabled={!emailNotificationEnabled || undefined}
                          data-invalid={fieldState.invalid || undefined}
                          data-notification-type={option.value}
                          className='hover:bg-muted/50 items-start rounded-sm p-2'
                        >
                          <Checkbox
                            id={checkboxId}
                            checked={checked}
                            disabled={!emailNotificationEnabled}
                            aria-invalid={fieldState.invalid || undefined}
                            onCheckedChange={(nextChecked) => {
                              if (nextChecked === true) {
                                field.onChange([...selected, option.value])
                                return
                              }
                              field.onChange(
                                selected.filter(
                                  (value) => value !== option.value
                                )
                              )
                            }}
                          />
                          <FieldContent>
                            <FieldLabel htmlFor={checkboxId}>
                              {option.label}
                            </FieldLabel>
                            <FieldDescription>
                              {option.description}
                            </FieldDescription>
                          </FieldContent>
                        </Field>
                      )
                    }
                  )}
                </FieldGroup>

                <div className='flex flex-wrap justify-end gap-2'>
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    disabled={
                      !emailNotificationEnabled ||
                      selected.length ===
                        DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES.length
                    }
                    onClick={() =>
                      field.onChange([
                        ...DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
                      ])
                    }
                  >
                    <HugeiconsIcon
                      icon={CheckListIcon}
                      data-icon='inline-start'
                    />
                    全选
                  </Button>
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    disabled={
                      !emailNotificationEnabled ||
                      selected.length === 0 ||
                      previewMutation.isPending
                    }
                    onClick={openPreview}
                  >
                    {previewMutation.isPending ? (
                      <Spinner data-icon='inline-start' />
                    ) : (
                      <HugeiconsIcon
                        icon={MailOpen01Icon}
                        data-icon='inline-start'
                      />
                    )}
                    预览邮件
                  </Button>
                </div>
              </FieldSet>
              <FormMessage />
            </FormItem>
          )
        }}
      />

      <Dialog
        open={previewOpen}
        onOpenChange={(open) => {
          setPreviewOpen(open)
          if (!open) previewMutation.reset()
        }}
      >
        <DialogContent className='max-h-[min(92dvh,48rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>渠道监控邮件预览</DialogTitle>
            <DialogDescription>
              以下内容使用当前勾选类型和真实邮件模板生成
            </DialogDescription>
          </DialogHeader>

          <div className='flex min-h-0 flex-col gap-3 overflow-hidden'>
            {previewMutation.isPending ? (
              <div
                className='text-muted-foreground flex min-h-72 items-center justify-center gap-2'
                role='status'
              >
                <Spinner />
                正在生成预览
              </div>
            ) : null}
            {previewMutation.isError ? (
              <div
                className='text-destructive flex min-h-72 items-center justify-center text-sm'
                role='alert'
              >
                预览生成失败，请关闭后重试
              </div>
            ) : null}
            {previewMutation.data ? (
              <>
                <div className='grid shrink-0 gap-1 border-b pb-3'>
                  <span className='text-muted-foreground text-xs'>
                    邮件主题
                  </span>
                  <span className='font-medium break-words'>
                    {previewMutation.data.data.subject}
                  </span>
                </div>
                <iframe
                  title='渠道监控邮件内容预览'
                  sandbox=''
                  referrerPolicy='no-referrer'
                  srcDoc={previewMutation.data.data.html}
                  className='bg-background min-h-72 w-full flex-1 rounded-md border'
                />
              </>
            ) : null}
          </div>

          <DialogFooter>
            <DialogClose render={<Button variant='outline' />}>
              关闭
            </DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
