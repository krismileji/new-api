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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm, type Resolver } from 'react-hook-form'
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

import { updateChannelMonitorConcurrencyLimit } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  createChannelConcurrencyLimitSchema,
  MAX_CHANNEL_CONCURRENCY_LIMIT,
  type ChannelConcurrencyLimitFormValues,
} from '../lib/schema'
import type { ChannelMonitorItem } from '../types'

type EditChannelConcurrencyLimitDialogProps = {
  channel: ChannelMonitorItem
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function EditChannelConcurrencyLimitDialog(
  props: EditChannelConcurrencyLimitDialogProps
) {
  const queryClient = useQueryClient()
  const form = useForm<ChannelConcurrencyLimitFormValues>({
    resolver: zodResolver(
      createChannelConcurrencyLimitSchema()
    ) as Resolver<ChannelConcurrencyLimitFormValues>,
    defaultValues: {
      concurrencyLimit: props.channel.concurrency_limit ?? 0,
    },
  })

  const mutation = useMutation({
    mutationFn: updateChannelMonitorConcurrencyLimit,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('渠道并发限制已保存')
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      props.onOpenChange(false)
    },
  })

  const handleSubmit = form.handleSubmit((values) => {
    mutation.mutate({
      channelId: props.channel.id,
      concurrencyLimit: values.concurrencyLimit,
    })
  })

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>设置渠道并发限制</DialogTitle>
          <DialogDescription>
            {props.channel.name} · ID {props.channel.id}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-5' onSubmit={handleSubmit}>
            <FormField
              control={form.control}
              name='concurrencyLimit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>最大并发请求数</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={MAX_CHANNEL_CONCURRENCY_LIMIT}
                      step={1}
                      inputMode='numeric'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={field.onChange}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    设置为 0
                    表示不限；达到上限时该渠道会暂停接收新请求，优先尝试其它可用渠道。
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='flex justify-end gap-2'>
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
      </DialogContent>
    </Dialog>
  )
}
