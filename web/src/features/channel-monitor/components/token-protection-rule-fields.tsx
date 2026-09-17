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
import { Controller, type UseFormReturn } from 'react-hook-form'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import type { TokenProtectionForm } from '../lib/token-protection'

type Props = {
  form: UseFormReturn<TokenProtectionForm>
  index: number
  count: number
  onMove: (to: number) => void
  onRemove: () => void
}

export function TokenProtectionRuleFields(props: Props) {
  const errors = props.form.formState.errors.rules?.[props.index]
  const prefix = `token-rule-${props.index}`
  return (
    <FieldSet className='rounded-lg border p-4'>
      <FieldLegend>规则 {props.index + 1}</FieldLegend>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Controller
          control={props.form.control}
          name={`rules.${props.index}.enabled`}
          render={({ field }) => (
            <Field orientation='horizontal' className='w-auto'>
              <Switch
                id={`${prefix}-enabled`}
                checked={field.value}
                onCheckedChange={field.onChange}
              />
              <FieldLabel htmlFor={`${prefix}-enabled`}>启用规则</FieldLabel>
            </Field>
          )}
        />
        <div className='flex gap-2'>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={props.index === 0}
            onClick={() => props.onMove(props.index - 1)}
            aria-label={`上移规则 ${props.index + 1}`}
          >
            上移
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={props.index === props.count - 1}
            onClick={() => props.onMove(props.index + 1)}
            aria-label={`下移规则 ${props.index + 1}`}
          >
            下移
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={props.onRemove}
            aria-label={`移除规则 ${props.index + 1}`}
          >
            移除
          </Button>
        </div>
      </div>
      <FieldGroup className='grid gap-4 sm:grid-cols-2'>
        <Field data-invalid={!!errors?.name}>
          <FieldLabel htmlFor={`${prefix}-name`}>规则名称</FieldLabel>
          <Input
            id={`${prefix}-name`}
            aria-invalid={!!errors?.name}
            {...props.form.register(`rules.${props.index}.name`)}
          />
          <FieldError>{errors?.name?.message}</FieldError>
        </Field>
        <Field data-invalid={!!errors?.channels}>
          <FieldLabel htmlFor={`${prefix}-channels`}>适用渠道编号</FieldLabel>
          <Input
            id={`${prefix}-channels`}
            placeholder='留空表示全部渠道'
            aria-invalid={!!errors?.channels}
            {...props.form.register(`rules.${props.index}.channels`)}
          />
          <FieldDescription>
            多个编号用逗号分隔；命中后停止该 Key 在所有渠道的请求。
          </FieldDescription>
          <FieldError>{errors?.channels?.message}</FieldError>
        </Field>
        <Field data-invalid={!!errors?.statuses}>
          <FieldLabel htmlFor={`${prefix}-statuses`}>上游状态码</FieldLabel>
          <Input
            id={`${prefix}-statuses`}
            placeholder='403, 429'
            aria-invalid={!!errors?.statuses}
            {...props.form.register(`rules.${props.index}.statuses`)}
          />
          <FieldDescription>
            与错误关键词同时满足才会触发。200 仅匹配明确的错误响应。
          </FieldDescription>
          <FieldError>{errors?.statuses?.message}</FieldError>
        </Field>
        <Field data-invalid={!!errors?.response_status}>
          <FieldLabel htmlFor={`${prefix}-response-status`}>
            返回状态码
          </FieldLabel>
          <Input
            id={`${prefix}-response-status`}
            type='number'
            min={400}
            max={599}
            aria-invalid={!!errors?.response_status}
            {...props.form.register(`rules.${props.index}.response_status`, {
              valueAsNumber: true,
            })}
          />
          <FieldError>{errors?.response_status?.message}</FieldError>
        </Field>
        <Field data-invalid={!!errors?.keywords}>
          <FieldLabel htmlFor={`${prefix}-keywords`}>错误信息包含</FieldLabel>
          <Textarea
            id={`${prefix}-keywords`}
            rows={3}
            placeholder='每行一个关键词'
            aria-invalid={!!errors?.keywords}
            {...props.form.register(`rules.${props.index}.keywords`)}
          />
          <FieldError>{errors?.keywords?.message}</FieldError>
        </Field>
        <Field data-invalid={!!errors?.response_message}>
          <FieldLabel htmlFor={`${prefix}-message`}>返回错误信息</FieldLabel>
          <Textarea
            id={`${prefix}-message`}
            rows={3}
            aria-invalid={!!errors?.response_message}
            {...props.form.register(`rules.${props.index}.response_message`)}
          />
          <FieldError>{errors?.response_message?.message}</FieldError>
        </Field>
        <Controller
          control={props.form.control}
          name={`rules.${props.index}.match_all`}
          render={({ field }) => (
            <Field orientation='horizontal'>
              <Switch
                id={`${prefix}-all`}
                checked={field.value}
                onCheckedChange={field.onChange}
              />
              <FieldLabel htmlFor={`${prefix}-all`}>
                必须包含全部关键词
              </FieldLabel>
            </Field>
          )}
        />
        <Controller
          control={props.form.control}
          name={`rules.${props.index}.case_sensitive`}
          render={({ field }) => (
            <Field orientation='horizontal'>
              <Switch
                id={`${prefix}-case`}
                checked={field.value}
                onCheckedChange={field.onChange}
              />
              <FieldLabel htmlFor={`${prefix}-case`}>区分大小写</FieldLabel>
            </Field>
          )}
        />
      </FieldGroup>
    </FieldSet>
  )
}
