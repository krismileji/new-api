import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { toast } from 'sonner'
import type { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  requestStatsAutoDisableSchema,
  requestStatsQueryKey,
  requestStatsThresholdSchema,
  saveRequestStatsAutoDisable,
  saveRequestStatsSettings,
  type RequestStatsSettings,
} from '../lib/request-statistics'

export function RequestStatisticsSettings({
  settings,
}: {
  settings: RequestStatsSettings
}) {
  const queryClient = useQueryClient()
  const [confirmation, setConfirmation] = useState<z.infer<
    typeof requestStatsAutoDisableSchema
  > | null>(null)
  const threshold = useForm<z.infer<typeof requestStatsThresholdSchema>>({
    resolver: zodResolver(requestStatsThresholdSchema),
    defaultValues: settings,
  })
  const automatic = useForm<z.infer<typeof requestStatsAutoDisableSchema>>({
    resolver: zodResolver(requestStatsAutoDisableSchema),
    defaultValues: settings,
  })
  const saveThreshold = useMutation({
    mutationFn: saveRequestStatsSettings,
    onSuccess: async (value) => {
      queryClient.setQueryData([...requestStatsQueryKey, 'settings'], value)
      await queryClient.invalidateQueries({ queryKey: requestStatsQueryKey })
      toast.success('输入阈值已保存，统计已重新计算')
    },
  })
  const saveAutomatic = useMutation({
    mutationFn: saveRequestStatsAutoDisable,
    onSuccess: async (value) => {
      setConfirmation(null)
      queryClient.setQueryData([...requestStatsQueryKey, 'settings'], value)
      await queryClient.invalidateQueries({ queryKey: requestStatsQueryKey })
      toast.success('每日自动禁用配置已保存')
    },
  })
  const pending = saveThreshold.isPending || saveAutomatic.isPending
  return (
    <div className='space-y-6'>
      <form
        onSubmit={threshold.handleSubmit((values) =>
          saveThreshold.mutate({ ...values, revision: settings.revision })
        )}
      >
        <fieldset
          disabled={pending}
          className='space-y-3 rounded-lg border p-4'
        >
          <legend className='px-1 font-medium'>输入阈值</legend>
          <Field>
            <FieldLabel htmlFor='request-stats-threshold'>
              输入 Token 阈值（K，1K = 1000 Token）
            </FieldLabel>
            <Input
              id='request-stats-threshold'
              type='number'
              min={1}
              max={1000}
              step={1}
              aria-invalid={Boolean(
                threshold.formState.errors.low_input_threshold_k
              )}
              {...threshold.register('low_input_threshold_k', {
                valueAsNumber: true,
              })}
            />
            {threshold.formState.errors.low_input_threshold_k && (
              <p role='alert'>
                {threshold.formState.errors.low_input_threshold_k.message}
              </p>
            )}
          </Field>
          <p className='text-muted-foreground text-sm'>
            严格小于阈值才计入低输入。保存后立即重算当天及历史占比；已冻结的每日判定不受影响。
          </p>
          <Button type='submit' disabled={pending}>
            保存输入阈值
          </Button>
        </fieldset>
      </form>
      <form
        onSubmit={automatic.handleSubmit((values) => {
          if (values.enabled && !settings.enabled) setConfirmation(values)
          else saveAutomatic.mutate({ ...values, revision: settings.revision })
        })}
      >
        <fieldset
          disabled={pending}
          className='space-y-4 rounded-lg border p-4'
        >
          <legend className='px-1 font-medium'>每日自动禁用</legend>
          <Controller
            control={automatic.control}
            name='enabled'
            render={({ field }) => (
              <Field orientation='horizontal'>
                <FieldLabel htmlFor='request-stats-auto-enabled'>
                  启用每日自动禁用
                </FieldLabel>
                <Switch
                  id='request-stats-auto-enabled'
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </Field>
            )}
          />
          <Field>
            <FieldLabel htmlFor='request-stats-disable-rate'>
              低输入请求占比达到（%）
            </FieldLabel>
            <Input
              id='request-stats-disable-rate'
              type='number'
              min={0}
              max={100}
              step={1}
              aria-invalid={Boolean(
                automatic.formState.errors.disable_rate_percent
              )}
              {...automatic.register('disable_rate_percent', {
                valueAsNumber: true,
              })}
            />
            {automatic.formState.errors.disable_rate_percent && (
              <p role='alert'>
                {automatic.formState.errors.disable_rate_percent.message}
              </p>
            )}
          </Field>
          <Field>
            <FieldLabel htmlFor='request-stats-minimum'>
              每天最少有效输入样本数
            </FieldLabel>
            <Input
              id='request-stats-minimum'
              type='number'
              min={1}
              max={1000000000}
              step={1}
              aria-invalid={Boolean(
                automatic.formState.errors.minimum_known_input_requests
              )}
              {...automatic.register('minimum_known_input_requests', {
                valueAsNumber: true,
              })}
            />
            {automatic.formState.errors.minimum_known_input_requests && (
              <p role='alert'>
                {
                  automatic.formState.errors.minimum_known_input_requests
                    .message
                }
              </p>
            )}
          </Field>
          <p className='text-muted-foreground text-sm'>
            北京时间每日 00:00 起按前一天每枚 Key 的所有渠道汇总判定，调度误差约
            1
            分钟。未知输入不参与占比。处理积压时重试，超过次日窗口则跳过。用户可以在现有
            Key 页面自行恢复，邮件失败不恢复 Key。
          </p>
          <Button type='submit' disabled={pending}>
            保存自动禁用配置
          </Button>
        </fieldset>
      </form>
      <ConfirmDialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null)
        }}
        title='启用每日自动禁用'
        desc='达到占比和样本数条件的 API Key 将变为普通禁用状态，并向已验证邮箱发送通知。用户可自行恢复。'
        confirmText='启用并保存'
        cancelBtnText='取消'
        isLoading={saveAutomatic.isPending}
        handleConfirm={() => {
          if (confirmation) {
            saveAutomatic.mutate({
              ...confirmation,
              revision: settings.revision,
            })
          }
        }}
      />
    </div>
  )
}
