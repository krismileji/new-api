import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import type { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { formatTimestamp } from '@/lib/format'

import {
  contentCaptureQueryKey,
  contentCaptureSchema,
  saveContentCapture,
  type ContentCaptureConfig,
} from '../lib/content-capture'

type Values = z.infer<typeof contentCaptureSchema>

const fields = [
  ['limit', '保留最新记录条数', 1, 1000],
  ['max_body_kb', '单侧正文上限（KB）', 1, 256],
  ['hours', '自动关闭并清空（小时）', 1, 72],
] as const

export function ContentCaptureSettings(props: {
  config: ContentCaptureConfig
}) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<'start' | 'stop' | null>(null)
  const [pendingValues, setPendingValues] = useState<Values | null>(null)
  const form = useForm<Values>({
    resolver: zodResolver(contentCaptureSchema),
    defaultValues: {
      limit: props.config.enabled ? props.config.limit : 200,
      max_body_kb: props.config.enabled ? props.config.max_body_kb : 64,
      hours: props.config.enabled
        ? Math.max(
            1,
            Math.min(
              72,
              Math.ceil((props.config.expires_at - Date.now() / 1000) / 3600)
            )
          )
        : 24,
    },
  })
  const save = useMutation({
    mutationFn: saveContentCapture,
    onSuccess: async (value, variables) => {
      setConfirm(null)
      queryClient.setQueryData(contentCaptureQueryKey, value)
      await queryClient.invalidateQueries({ queryKey: contentCaptureQueryKey })
      if (!variables.enabled) toast.success('已关闭抓取并清空全部记录')
      else if (props.config.enabled) toast.success('抓取设置已更新')
      else toast.success('已开始抓取请求内容')
    },
  })
  const submit = (values: Values) => {
    if (props.config.enabled) {
      save.mutate({ enabled: true, ...values })
      return
    }
    setPendingValues(values)
    setConfirm('start')
  }
  return (
    <form onSubmit={form.handleSubmit(submit)}>
      <fieldset
        disabled={save.isPending}
        className='space-y-4 rounded-lg border p-4'
      >
        <legend className='px-1 font-medium'>临时请求内容抓取</legend>
        <p className='text-muted-foreground text-sm'>
          {props.config.enabled
            ? `抓取中：开始于 ${formatTimestamp(props.config.started_at)}，将于 ${formatTimestamp(props.config.expires_at)} 自动关闭并清空。`
            : '未开启。开启后记录中继请求的请求正文和返回给用户的响应正文，只保存在 Redis 中。'}
        </p>
        <div className='grid gap-3 sm:grid-cols-3'>
          {fields.map(([name, label, min, max]) => (
            <Field key={name}>
              <FieldLabel htmlFor={`content-capture-${name}`}>
                {label}
              </FieldLabel>
              <Input
                id={`content-capture-${name}`}
                type='number'
                min={min}
                max={max}
                step={1}
                aria-invalid={Boolean(form.formState.errors[name])}
                {...form.register(name, { valueAsNumber: true })}
              />
              {form.formState.errors[name] && (
                <p role='alert' className='text-destructive text-sm'>
                  {form.formState.errors[name]?.message}
                </p>
              )}
            </Field>
          ))}
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button type='submit'>
            {props.config.enabled ? '更新设置' : '开始抓取'}
          </Button>
          {props.config.enabled && (
            <Button
              type='button'
              variant='destructive'
              onClick={() => setConfirm('stop')}
            >
              停止并清空
            </Button>
          )}
        </div>
      </fieldset>
      <ConfirmDialog
        open={confirm === 'start'}
        onOpenChange={(open) => {
          if (!open) setConfirm(null)
        }}
        title='开始抓取请求内容'
        desc='抓取期间所有用户的请求和响应正文都会明文保存在 Redis 中，仅 Root 可以查看。请只在排查期间短时开启，并确认符合服务条款与隐私要求。'
        confirmText='开始抓取'
        cancelBtnText='取消'
        isLoading={save.isPending}
        handleConfirm={() => {
          if (pendingValues) save.mutate({ enabled: true, ...pendingValues })
        }}
      />
      <ConfirmDialog
        open={confirm === 'stop'}
        onOpenChange={(open) => {
          if (!open) setConfirm(null)
        }}
        title='停止并清空'
        desc='停止抓取并删除 Redis 中已记录的全部请求内容，删除后无法恢复。'
        confirmText='停止并清空'
        cancelBtnText='取消'
        destructive
        isLoading={save.isPending}
        handleConfirm={() => save.mutate({ enabled: false })}
      />
    </form>
  )
}
