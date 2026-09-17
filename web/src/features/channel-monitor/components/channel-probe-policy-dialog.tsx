import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import { getChannelProbePolicy, updateChannelProbePolicy } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  probePolicySchema,
  probePolicyThresholdTokens,
  type ProbePolicyFormValues,
} from '../lib/probe-policy-schema'
import type { ChannelMonitorItem, ChannelProbePolicy } from '../types'

type Props = {
  channel: ChannelMonitorItem
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ChannelProbePolicyDialog(props: Props) {
  const query = useQuery({
    queryKey: ['channel-monitor', 'probe-policy', props.channel.id],
    queryFn: () => getChannelProbePolicy(props.channel.id),
    enabled: props.open,
    refetchOnWindowFocus: false,
  })
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[90dvh] overflow-y-auto sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>渠道探测策略</DialogTitle>
          <DialogDescription>
            {props.channel.name} · ID {props.channel.id} · 仅影响此物理渠道
          </DialogDescription>
        </DialogHeader>
        {query.isPending && (
          <div role='status' className='flex items-center gap-2'>
            <Spinner />
            正在加载配置
          </div>
        )}
        {query.isError && (
          <div role='alert' className='space-y-2'>
            <p>配置加载失败，请重试。</p>
            <Button variant='outline' onClick={() => query.refetch()}>
              重新加载
            </Button>
          </div>
        )}
        {query.data && (
          <ChannelProbePolicyForm
            key={`${props.channel.id}:${query.data.probe_policy_revision}`}
            channelId={props.channel.id}
            policy={query.data}
            onSaved={() => props.onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function ChannelProbePolicyForm(props: {
  channelId: number
  policy: ChannelProbePolicy
  onSaved: () => void
}) {
  const queryClient = useQueryClient()
  const form = useForm<ProbePolicyFormValues>({
    resolver: zodResolver(probePolicySchema),
    defaultValues: {
      autoProbeDisabled: props.policy.auto_probe_disabled,
      smallInputResponseEnabled: props.policy.small_input_response_enabled,
      thresholdK: String(
        (props.policy.small_input_threshold_tokens || 1000) / 1000
      ),
      responseText: props.policy.small_input_response_text,
    },
  })
  const autoDisabled = form.watch('autoProbeDisabled')
  const smallEnabled = form.watch('smallInputResponseEnabled')
  const mutation = useMutation({
    mutationFn: updateChannelProbePolicy,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('渠道探测策略已保存')
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      queryClient.invalidateQueries({ queryKey: ['group-monitor'] })
      props.onSaved()
    },
  })
  const submit = form.handleSubmit((values) =>
    mutation.mutate({
      channelId: props.channelId,
      policy: {
        ...props.policy,
        auto_probe_disabled: values.autoProbeDisabled,
        small_input_response_enabled:
          values.autoProbeDisabled && values.smallInputResponseEnabled,
        small_input_threshold_tokens:
          probePolicyThresholdTokens(values.thresholdK) ??
          props.policy.small_input_threshold_tokens,
        small_input_response_text: values.responseText,
      },
    })
  )
  return (
    <Form {...form}>
      <form className='space-y-5' onSubmit={submit}>
        <FormField
          control={form.control}
          name='autoProbeDisabled'
          render={({ field }) => (
            <FormItem className='rounded-lg border p-4'>
              <div className='flex items-center justify-between gap-4'>
                <FormLabel>禁止自动探测</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={mutation.isPending}
                  />
                </FormControl>
              </div>
              <FormDescription>
                停止状态、分组、智能采样、健康检查和模型检测的自动请求。手动检测仍会真实请求上游。
              </FormDescription>
            </FormItem>
          )}
        />
        {autoDisabled && (
          <>
            <p className='text-muted-foreground text-sm'>
              状态监测和分组监控开启后，此渠道按各自周期使用 Redis
              中的真实业务数据。自动禁用的渠道仍需手动检测并决定是否启用。
            </p>
            <FormField
              control={form.control}
              name='smallInputResponseEnabled'
              render={({ field }) => (
                <FormItem>
                  <div className='flex items-center justify-between gap-4'>
                    <FormLabel>小输入请求返回自定义内容</FormLabel>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={mutation.isPending}
                      />
                    </FormControl>
                  </div>
                  <FormDescription>
                    符合条件的用户请求在本地响应，用户收费为零；不占用该渠道的上游并发与
                    RPM。
                  </FormDescription>
                </FormItem>
              )}
            />
            {smallEnabled && (
              <>
                <FormField
                  control={form.control}
                  name='thresholdK'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>输入阈值（k tokens）</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          inputMode='decimal'
                          disabled={mutation.isPending}
                        />
                      </FormControl>
                      <FormDescription>
                        1k = 1000
                        tokens；完整输入估算严格小于阈值时命中，包含系统提示、历史消息和工具定义。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='responseText'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>自定义响应内容</FormLabel>
                      <FormControl>
                        <Textarea
                          {...field}
                          rows={5}
                          disabled={mutation.isPending}
                        />
                      </FormControl>
                      <FormDescription>
                        按纯文本返回，保留换行；遵守客户端输出 token 上限。
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <p className='text-muted-foreground text-xs'>
                  支持 Chat Completions、Responses、Claude Messages、Gemini
                  文本请求及流式响应。引用上下文、无法完整计数的多模态、强制工具调用和结构化输出等请求继续正常转发。
                </p>
              </>
            )}
          </>
        )}
        {mutation.isError && (
          <p role='alert' className='text-destructive text-sm'>
            保存失败，内容已保留。若配置已被修改，请关闭后重新打开以加载最新版本。
          </p>
        )}
        <div className='flex justify-end'>
          <Button type='submit' disabled={mutation.isPending}>
            {mutation.isPending && <Spinner />}保存策略
          </Button>
        </div>
      </form>
    </Form>
  )
}
