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
import { Add01Icon, Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useFieldArray, useWatch, type UseFormReturn } from 'react-hook-form'

import { Button } from '@/components/ui/button'
import { FieldGroup, FieldSet } from '@/components/ui/field'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  MAX_PROBE_RESPONSE_ALLOWED_IPS_LENGTH,
  MAX_PROBE_RESPONSE_DELAY_MS,
  MAX_PROBE_RESPONSE_MATCH_INPUT_LENGTH,
  MAX_PROBE_RESPONSE_TEXT_LENGTH,
  MAX_PROBE_RESPONSE_TOKEN_COUNT,
  MAX_PROBE_RESPONSE_RULES,
  type ChannelMonitorSettingsFormValues,
} from '../lib/schema'
import { ChannelMonitorFieldInfo } from './channel-monitor-field-info'

type ProbeResponseNumberFieldName =
  | 'probeResponseMinDelayMs'
  | 'probeResponseMaxDelayMs'
  | 'probeResponseInputTokens'
  | 'probeResponseCacheWriteTokens'
  | 'probeResponseCachedTokens'
  | 'probeResponseOutputTokens'

function ProbeResponseNumberField(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  label: string
  max: number
  name: ProbeResponseNumberFieldName
  suffix: string
  description: string
  disabled?: boolean
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center gap-1'>
            <FormLabel>{props.label}</FormLabel>
            <ChannelMonitorFieldInfo
              label={props.label}
              description={props.description}
            />
          </div>
          <FormControl>
            <InputGroup className='ring-inset'>
              <InputGroupInput
                type='number'
                min={0}
                max={props.max}
                step={1}
                inputMode='numeric'
                disabled={props.disabled}
                value={field.value}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
                aria-invalid={Boolean(props.form.formState.errors[props.name])}
              />
              <InputGroupAddon align='inline-end'>
                {props.suffix}
              </InputGroupAddon>
            </InputGroup>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function ChannelMonitorProbeResponseFields(props: {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
}) {
  const enabled = useWatch({
    control: props.form.control,
    name: 'probeResponseEnabled',
  })
  const rules = useFieldArray({
    control: props.form.control,
    name: 'probeResponseRules',
  })

  return (
    <div className='flex flex-col gap-5'>
      <FormField
        control={props.form.control}
        name='probeResponseEnabled'
        render={({ field }) => (
          <FormItem className='flex items-start justify-between gap-4 rounded-lg border p-3'>
            <div className='flex min-w-0 flex-col gap-1'>
              <div className='flex items-center gap-1'>
                <FormLabel>启用本地探针响应</FormLabel>
                <ChannelMonitorFieldInfo
                  label='启用本地探针响应'
                  description='命中后由本机直接完成请求，不选择渠道，也不产生消费或渠道成本记录。'
                />
              </div>
              <FormMessage />
            </div>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                aria-label='启用本地探针响应'
              />
            </FormControl>
          </FormItem>
        )}
      />

      <fieldset
        className='flex flex-col gap-5 data-[disabled=true]:opacity-60'
        aria-disabled={!enabled}
        data-disabled={!enabled || undefined}
        aria-label='探针响应配置'
      >
        <div className='flex min-w-0 flex-col gap-3'>
          <div className='flex items-center justify-between gap-3'>
            <div className='flex items-center gap-1'>
              <p className='text-sm font-medium'>输入输出</p>
              <ChannelMonitorFieldInfo
                label='输入输出'
                description='每组匹配输入对应一段响应文本，最多 32 组。去除首尾空白后完整匹配，不区分大小写，匹配输入不能重复。'
              />
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={
                !enabled || rules.fields.length >= MAX_PROBE_RESPONSE_RULES
              }
              onClick={() => rules.append({ matchInput: '', responseText: '' })}
            >
              <HugeiconsIcon icon={Add01Icon} aria-hidden='true' />
              添加输入输出
            </Button>
          </div>
          <FieldGroup className='gap-3'>
            {rules.fields.map((rule, index) => (
              <FieldSet
                key={rule.id}
                className='min-w-0 gap-3 rounded-lg border p-3'
                aria-label={`输入输出 ${index + 1}`}
              >
                <div className='flex items-center justify-between gap-3'>
                  <p className='text-sm font-medium'>第 {index + 1} 组</p>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    disabled={!enabled || rules.fields.length === 1}
                    onClick={() => rules.remove(index)}
                    aria-label={`删除输入输出 ${index + 1}`}
                  >
                    <HugeiconsIcon icon={Delete02Icon} aria-hidden='true' />
                  </Button>
                </div>
                <FieldGroup className='gap-3'>
                  <FormField
                    control={props.form.control}
                    name={`probeResponseRules.${index}.matchInput`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>匹配输入</FormLabel>
                        <FormControl>
                          <Input
                            maxLength={MAX_PROBE_RESPONSE_MATCH_INPUT_LENGTH}
                            autoComplete='off'
                            disabled={!enabled}
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={props.form.control}
                    name={`probeResponseRules.${index}.responseText`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>响应文本</FormLabel>
                        <FormControl>
                          <Textarea
                            className='min-h-24 resize-y'
                            maxLength={MAX_PROBE_RESPONSE_TEXT_LENGTH}
                            disabled={!enabled}
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </FieldGroup>
              </FieldSet>
            ))}
          </FieldGroup>
          {props.form.formState.errors.probeResponseRules?.root?.message ? (
            <p role='alert' className='text-destructive text-sm'>
              {props.form.formState.errors.probeResponseRules.root.message}
            </p>
          ) : null}
        </div>

        <FormField
          control={props.form.control}
          name='probeResponseAllowedIPs'
          render={({ field }) => (
            <FormItem>
              <div className='flex items-center gap-1'>
                <FormLabel>生效 IP（可选）</FormLabel>
                <ChannelMonitorFieldInfo
                  label='生效 IP（可选）'
                  description='每行一个 IPv4 或 IPv6，也可用逗号分隔；留空时对所有来源生效。'
                />
              </div>
              <FormControl>
                <Textarea
                  className='min-h-20 resize-y font-mono'
                  maxLength={MAX_PROBE_RESPONSE_ALLOWED_IPS_LENGTH}
                  placeholder={'203.0.113.10\n2001:db8::10'}
                  autoCapitalize='none'
                  autoComplete='off'
                  spellCheck={false}
                  disabled={!enabled}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className='grid gap-4 sm:grid-cols-2'>
          <ProbeResponseNumberField
            form={props.form}
            name='probeResponseMinDelayMs'
            label='最小延迟'
            max={MAX_PROBE_RESPONSE_DELAY_MS}
            suffix='毫秒'
            description='返回探针响应前随机等待的最小时长。'
            disabled={!enabled}
          />
          <ProbeResponseNumberField
            form={props.form}
            name='probeResponseMaxDelayMs'
            label='最大延迟'
            max={MAX_PROBE_RESPONSE_DELAY_MS}
            suffix='毫秒'
            description='返回探针响应前随机等待的最大时长。'
            disabled={!enabled}
          />
        </div>

        <div className='flex flex-col gap-3'>
          <div className='flex items-center gap-1'>
            <p className='text-sm font-medium'>Usage</p>
            <ChannelMonitorFieldInfo
              label='Usage'
              description='总 Token 自动按输入 Token 与输出 Token 相加。缓存写入和缓存命中 Token 按上游用量字段返回。'
            />
          </div>
          <div className='grid gap-4 sm:grid-cols-2'>
            <ProbeResponseNumberField
              form={props.form}
              name='probeResponseInputTokens'
              label='输入 Token'
              max={MAX_PROBE_RESPONSE_TOKEN_COUNT}
              suffix='Token'
              description='本地探针响应计费中使用的输入 Token 数。'
              disabled={!enabled}
            />
            <ProbeResponseNumberField
              form={props.form}
              name='probeResponseOutputTokens'
              label='输出 Token'
              max={MAX_PROBE_RESPONSE_TOKEN_COUNT}
              suffix='Token'
              description='本地探针响应计费中使用的输出 Token 数。'
              disabled={!enabled}
            />
            <ProbeResponseNumberField
              form={props.form}
              name='probeResponseCacheWriteTokens'
              label='缓存写 Token'
              max={MAX_PROBE_RESPONSE_TOKEN_COUNT}
              suffix='Token'
              description='本地探针响应计费中使用的缓存写入 Token 数。'
              disabled={!enabled}
            />
            <ProbeResponseNumberField
              form={props.form}
              name='probeResponseCachedTokens'
              label='缓存命中 Token'
              max={MAX_PROBE_RESPONSE_TOKEN_COUNT}
              suffix='Token'
              description='本地探针响应计费中使用的缓存命中 Token 数。'
              disabled={!enabled}
            />
          </div>
        </div>
      </fieldset>

      <p className='text-muted-foreground text-sm leading-relaxed'>
        支持 /v1/responses 和 /v1/chat/completions
        的单轮纯文本请求；历史对话、图片和工具结果仍请求上游，渠道连通性测试不经过此拦截。
      </p>
    </div>
  )
}
