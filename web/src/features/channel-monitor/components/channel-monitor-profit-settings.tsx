import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import type { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { MultiSelect } from '@/components/multi-select'
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
import { handleServerError } from '@/lib/handle-server-error'

import {
  getChannelMonitorProfitSettings,
  saveChannelMonitorProfitSettings,
  type ChannelMonitorProfitSettings,
} from '../api'
import { channelMonitorProfitSettingsSchema } from '../lib/profit-settings'

function ChannelMonitorProfitSettingsForm(props: {
  settings: ChannelMonitorProfitSettings
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  const form = useForm<z.infer<typeof channelMonitorProfitSettingsSchema>>({
    resolver: zodResolver(channelMonitorProfitSettingsSchema),
    defaultValues: {
      apiKeyIds: props.settings.cost_only_api_key_ids.map(String),
    },
  })
  const mutation = useMutation({
    mutationFn: saveChannelMonitorProfitSettings,
    onError: (error) => handleServerError(error, '自用 API Key 保存失败'),
    onSuccess: async () => {
      await queryClient.cancelQueries({ queryKey: ['channel-monitor'] })
      await queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      toast.success('自用 API Key 已保存，利润统计已更新')
      props.onClose()
    },
  })

  return (
    <Form {...form}>
      <form
        className='flex flex-col gap-4'
        onSubmit={form.handleSubmit((values) =>
          mutation.mutate({
            cost_only_api_key_ids: values.apiKeyIds.map(Number),
          })
        )}
      >
        <FormField
          control={form.control}
          name='apiKeyIds'
          render={({ field }) => (
            <FormItem>
              <FormLabel>仅计成本的 API Key ID</FormLabel>
              <FormDescription>
                在「API Key 明细」中查看 Key ID，输入数字后按回车添加。支持多个
                ID，可用逗号分隔；移除全部 ID 后保存即可恢复正常统计。
              </FormDescription>
              <FormControl>
                <MultiSelect
                  options={[]}
                  selected={field.value}
                  onChange={field.onChange}
                  allowCreate
                  createLabel='添加 Key ID {{value}}'
                  placeholder='输入 Key ID，例如 12'
                  emptyText='输入 API Key 的数字 ID'
                  disabled={mutation.isPending}
                  maxVisibleChips={8}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <p className='text-muted-foreground text-sm'>
          保存后对所有日期、渠道和用户的利润统计生效，包括总览、趋势及下钻明细。
          这些 Key 的扣费不计入收入，上游成本仍从总利润中扣除，请求量照常统计。
          实际扣费、余额和原始账目不变。
        </p>
        {mutation.isError ? (
          <p role='alert' className='text-destructive text-sm'>
            保存失败，请重试。输入的 Key ID 已保留。
          </p>
        ) : null}
        <div className='flex justify-end gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={props.onClose}
          >
            取消
          </Button>
          <Button type='submit' disabled={mutation.isPending}>
            {mutation.isPending ? '保存中…' : '保存'}
          </Button>
        </div>
      </form>
    </Form>
  )
}

function ChannelMonitorProfitSettingsDialog(props: { onClose: () => void }) {
  const query = useQuery({
    queryKey: ['channel-monitor', 'profit-settings'],
    queryFn: getChannelMonitorProfitSettings,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title='自用 API Key'
      description='指定仅计成本、不计收入的入站 API Key'
      contentClassName='sm:max-w-xl'
    >
      {query.isPending ? (
        <LoadingState message='正在读取自用 API Key 配置…' />
      ) : null}
      {query.isError ? (
        <ErrorState
          title='自用 API Key 配置加载失败'
          description='重试成功后可编辑配置'
          onRetry={() => query.refetch()}
        />
      ) : null}
      {query.data && !query.isError ? (
        <ChannelMonitorProfitSettingsForm
          settings={query.data}
          onClose={props.onClose}
        />
      ) : null}
    </Dialog>
  )
}

export function ChannelMonitorProfitSettingsButton() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => setOpen(true)}
      >
        自用 API Key
      </Button>
      {open ? (
        <ChannelMonitorProfitSettingsDialog onClose={() => setOpen(false)} />
      ) : null}
    </>
  )
}
