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
import { Alert02Icon, TestTubeIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

import { testChannelMonitorSmartScheduleModel } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  CHANNEL_MONITOR_MODEL_TEST_ENDPOINT_OPTIONS,
  CHANNEL_MONITOR_MODEL_TEST_STREAM_INCOMPATIBLE_ENDPOINTS,
  mergeChannelMonitorModelTestRetry,
} from '../lib/smart-schedule-model-test'
import type { ChannelMonitorSmartScheduleModelTestResult } from '../types'
import { ChannelMonitorSmartScheduleModelTestResults } from './channel-monitor-smart-schedule-model-test-results'

type ChannelMonitorSmartScheduleModelTestDialogProps = {
  open: boolean
  group: string
  model: string
  onOpenChange: (open: boolean) => void
}

export function ChannelMonitorSmartScheduleModelTestDialog(
  props: ChannelMonitorSmartScheduleModelTestDialogProps
) {
  const [endpointType, setEndpointType] = useState('auto')
  const [stream, setStream] = useState(true)
  const [result, setResult] =
    useState<ChannelMonitorSmartScheduleModelTestResult | null>(null)
  const [pendingChannelId, setPendingChannelId] = useState<number | null>(null)
  const streamDisabled =
    CHANNEL_MONITOR_MODEL_TEST_STREAM_INCOMPATIBLE_ENDPOINTS.has(endpointType)
  const effectiveStream = stream && !streamDisabled
  const mutation = useMutation({
    mutationFn: testChannelMonitorSmartScheduleModel,
    onError: handleChannelMonitorMutationError,
    onSettled: () => setPendingChannelId(null),
  })

  const runTest = (channelIds?: number[]) => {
    setPendingChannelId(channelIds?.[0] ?? null)
    mutation.mutate(
      {
        group: props.group,
        model: props.model,
        stream: effectiveStream,
        endpointType,
        channelIds,
      },
      {
        onSuccess: (response) => {
          setResult((current) =>
            channelIds?.length === 1
              ? mergeChannelMonitorModelTestRetry(current, response.data)
              : response.data
          )
        },
      }
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='grid max-h-[90dvh] grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-6xl'>
        <DialogHeader>
          <DialogTitle>模型测试</DialogTitle>
          <DialogDescription className='break-all'>
            {props.group} / {props.model}
          </DialogDescription>
        </DialogHeader>

        <div className='grid gap-3 border-y py-3 md:grid-cols-[minmax(16rem,1fr)_12rem_auto] md:items-end'>
          <Field>
            <FieldLabel htmlFor='smart-schedule-model-test-endpoint'>
              端点类型
            </FieldLabel>
            <Select
              items={CHANNEL_MONITOR_MODEL_TEST_ENDPOINT_OPTIONS}
              value={endpointType}
              disabled={mutation.isPending}
              onValueChange={(value) => {
                if (!value) return
                setEndpointType(value)
                setResult(null)
                if (
                  CHANNEL_MONITOR_MODEL_TEST_STREAM_INCOMPATIBLE_ENDPOINTS.has(
                    value
                  )
                ) {
                  setStream(false)
                }
              }}
            >
              <SelectTrigger
                id='smart-schedule-model-test-endpoint'
                className='w-full min-w-0'
              >
                <SelectValue className='min-w-0 truncate' />
              </SelectTrigger>
              <SelectContent
                alignItemWithTrigger={false}
                className='w-[460px] max-w-[calc(100vw-2rem)]'
              >
                <SelectGroup>
                  {CHANNEL_MONITOR_MODEL_TEST_ENDPOINT_OPTIONS.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value}
                      className='items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'
                    >
                      <span className='min-w-0 leading-snug break-words whitespace-normal'>
                        {option.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel htmlFor='smart-schedule-model-test-stream'>
              流式测试
            </FieldLabel>
            <div className='flex h-8 items-center gap-2'>
              <Switch
                id='smart-schedule-model-test-stream'
                checked={effectiveStream}
                disabled={mutation.isPending || streamDisabled}
                onCheckedChange={(checked) => {
                  setStream(checked)
                  setResult(null)
                }}
                aria-label='启用流式模型测试'
              />
              <span className='text-sm'>
                {effectiveStream ? '已启用' : '已关闭'}
              </span>
            </div>
            <FieldDescription>
              {streamDisabled
                ? '当前端点不支持流式请求'
                : '流式结果包含首字和 TPS'}
            </FieldDescription>
          </Field>

          <Button
            type='button'
            disabled={mutation.isPending}
            onClick={() => runTest()}
          >
            {mutation.isPending && pendingChannelId == null ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={TestTubeIcon} data-icon='inline-start' />
            )}
            测试全部渠道
          </Button>
        </div>

        <div
          className='min-h-0 overflow-auto rounded-md border'
          aria-busy={mutation.isPending}
        >
          {mutation.isError ? (
            <Alert variant='destructive' className='m-3'>
              <HugeiconsIcon icon={Alert02Icon} />
              <AlertTitle>模型测试请求失败</AlertTitle>
              <AlertDescription>
                {mutation.error instanceof Error
                  ? mutation.error.message
                  : '请稍后重试'}
              </AlertDescription>
            </Alert>
          ) : null}

          {result ? (
            <ChannelMonitorSmartScheduleModelTestResults
              result={result}
              pendingChannelId={pendingChannelId}
              testing={mutation.isPending}
              onRetry={(channelId) => runTest([channelId])}
            />
          ) : (
            <Empty className='min-h-64 border-0'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <HugeiconsIcon icon={TestTubeIcon} />
                </EmptyMedia>
                <EmptyTitle>尚未执行模型测试</EmptyTitle>
                <EmptyDescription>
                  当前调度池共有待验证渠道，执行后会逐渠道显示结果。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
