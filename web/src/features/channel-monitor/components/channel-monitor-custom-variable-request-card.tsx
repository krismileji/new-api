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
import { ArrowDown01Icon, Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useFormState, useWatch, type UseFormReturn } from 'react-hook-form'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { FieldSet } from '@/components/ui/field'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Spinner } from '@/components/ui/spinner'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import type { UpstreamConfigFormValues } from '../lib/schema'
import { ChannelMonitorCustomRequestFields } from './channel-monitor-custom-request-fields'
import { ChannelMonitorCustomVariableMappings } from './channel-monitor-custom-variable-mappings'

type ChannelMonitorCustomVariableRequestCardProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  index: number
  open: boolean
  pending: boolean
  fetching: boolean
  canAddVariable: boolean
  onOpenChange: (open: boolean) => void
  onFetch: () => void
  onRemove: () => void
}

export function ChannelMonitorCustomVariableRequestCard(
  props: ChannelMonitorCustomVariableRequestCardProps
) {
  const prefix = `customConfig.variableRequests.${props.index}` as const
  const request = useWatch({ control: props.form.control, name: prefix })
  const formState = useFormState({ control: props.form.control, name: prefix })
  const hasError = Boolean(
    formState.errors.customConfig?.variableRequests?.[props.index]
  )
  const requestName = request.name || `请求 ${props.index + 1}`
  const filled = request.variables.filter(
    (variable) => variable.value || variable.hasValue
  ).length
  const variableNames = [
    ...new Set(
      request.variables.map((variable) => variable.name || '未命名变量')
    ),
  ]

  return (
    <Collapsible
      open={props.open || hasError}
      onOpenChange={props.onOpenChange}
      className='min-w-0 rounded-lg border'
    >
      <div className='flex min-w-0 items-start gap-2 p-3'>
        <CollapsibleTrigger
          className='group focus-visible:ring-ring flex min-w-0 flex-1 items-start gap-3 rounded-md text-left outline-none focus-visible:ring-2'
          aria-label={`配置请求 ${requestName}`}
        >
          <HugeiconsIcon
            icon={ArrowDown01Icon}
            aria-hidden='true'
            className='mt-1 size-4 shrink-0 -rotate-90 transition-transform group-aria-expanded:rotate-0'
          />
          <span className='flex min-w-0 flex-1 flex-col gap-2'>
            <span className='flex flex-wrap items-center gap-2'>
              <span className='font-medium break-all'>{requestName}</span>
              <Badge variant='secondary'>
                {request.refreshPolicy === 'always'
                  ? '每次更新前'
                  : '更新失败时'}
              </Badge>
              {hasError ? (
                <Badge variant='destructive'>配置待完善</Badge>
              ) : null}
            </span>
            <span className='text-muted-foreground flex min-w-0 items-center gap-2 text-xs'>
              <span className='font-mono font-medium'>
                {request.request.method}
              </span>
              <span className='truncate font-mono'>
                {request.request.path || '待填写接口路径'}
              </span>
            </span>
            <span className='flex flex-wrap items-center gap-1.5'>
              {variableNames.map((name) => (
                <Badge
                  key={name}
                  variant='outline'
                  className='max-w-full font-mono text-xs'
                >
                  <span className='truncate'>{name}</span>
                </Badge>
              ))}
              <span className='text-muted-foreground text-xs'>
                {filled}/{request.variables.length} 已有值
              </span>
            </span>
          </span>
        </CollapsibleTrigger>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.pending}
          onClick={props.onRemove}
          aria-label={`删除请求 ${requestName}`}
        >
          <HugeiconsIcon icon={Delete02Icon} aria-hidden='true' />
        </Button>
      </div>
      <CollapsibleContent>
        <Separator />
        <FieldSet
          aria-label={`独立请求 ${requestName}`}
          disabled={props.pending}
          className='min-w-0 p-4'
        >
          <FormField
            control={props.form.control}
            name={`${prefix}.name`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>请求名称</FormLabel>
                <FormControl>
                  <Input {...field} placeholder='例如：登录获取凭据' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={props.form.control}
            name={`${prefix}.refreshPolicy`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>刷新策略</FormLabel>
                <FormControl>
                  <ToggleGroup
                    disabled={props.pending}
                    value={[field.value]}
                    onValueChange={(values) => {
                      const value = values.find((item) => item !== field.value)
                      if (value === 'always' || value === 'on_failure') {
                        field.onChange(value)
                      }
                    }}
                    variant='outline'
                    spacing={2}
                    className='grid w-full grid-cols-2'
                    aria-label='刷新策略'
                  >
                    <ToggleGroupItem value='always'>
                      每次更新前获取
                    </ToggleGroupItem>
                    <ToggleGroupItem value='on_failure'>
                      更新失败时获取
                    </ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
                <FormDescription>
                  仅作用于本请求产生的变量。首次使用缺少值的变量时会先获取；失败刷新后最多重试一次。
                </FormDescription>
              </FormItem>
            )}
          />
          <FormField
            control={props.form.control}
            name={`${prefix}.baseUrl`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>独立请求基础地址</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='留空使用上方的自定义接口基础地址'
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <ChannelMonitorCustomRequestFields
            form={props.form}
            prefix={`${prefix}.request`}
            disabled={props.pending}
          />
          <Separator />
          <FormField
            control={props.form.control}
            name={`${prefix}.responseType`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>响应格式</FormLabel>
                <FormControl>
                  <ToggleGroup
                    disabled={props.pending}
                    value={[field.value]}
                    onValueChange={(values) => {
                      const value = values.find((item) => item !== field.value)
                      if (value === 'json' || value === 'text') {
                        field.onChange(value)
                      }
                    }}
                    variant='outline'
                    spacing={2}
                    className='w-fit'
                  >
                    <ToggleGroupItem value='json'>JSON</ToggleGroupItem>
                    <ToggleGroupItem value='text'>文本</ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
              </FormItem>
            )}
          />
          <ChannelMonitorCustomVariableMappings
            form={props.form}
            requestIndex={props.index}
            disabled={props.pending}
            canAddVariable={props.canAddVariable}
          />
          <div className='flex flex-wrap items-center gap-3'>
            <Button
              type='button'
              variant='outline'
              disabled={props.pending}
              onClick={props.onFetch}
            >
              {props.fetching ? (
                <Spinner data-icon='inline-start' aria-hidden='true' />
              ) : null}
              {props.fetching ? '正在获取变量…' : '请求并回填变量'}
            </Button>
            <span className='text-muted-foreground text-xs'>
              同时回填本请求的全部变量，保存后生效。
            </span>
          </div>
        </FieldSet>
      </CollapsibleContent>
    </Collapsible>
  )
}
