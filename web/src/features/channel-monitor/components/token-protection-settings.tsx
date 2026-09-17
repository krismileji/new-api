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
import { Controller, useFieldArray, useForm } from 'react-hook-form'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'

import { handleChannelMonitorMutationError } from '../lib/error'
import {
  newTokenProtectionRule,
  saveTokenProtectionSettings,
  tokenProtectionFormSchema,
  tokenProtectionFormValues,
  tokenProtectionPayload,
  tokenProtectionQueryKey,
  type TokenProtectionForm,
  type TokenProtectionSettings as Settings,
} from '../lib/token-protection'
import { TokenProtectionRuleFields } from './token-protection-rule-fields'

export function TokenProtectionSettings(props: { settings: Settings }) {
  const queryClient = useQueryClient()
  const form = useForm<TokenProtectionForm>({
    resolver: zodResolver(tokenProtectionFormSchema),
    defaultValues: tokenProtectionFormValues(props.settings),
  })
  const rules = useFieldArray({ control: form.control, name: 'rules' })
  const save = useMutation({
    mutationFn: saveTokenProtectionSettings,
    onError: handleChannelMonitorMutationError,
    onSuccess: (settings) => {
      queryClient.setQueryData(
        [...tokenProtectionQueryKey, 'settings'],
        settings
      )
      toast.success('API Key 自动禁用规则已保存')
    },
  })
  return (
    <form
      onSubmit={form.handleSubmit((values) =>
        save.mutate(tokenProtectionPayload(values, props.settings.revision))
      )}
      className='flex min-h-0 flex-1 flex-col gap-4'
    >
      <fieldset
        disabled={save.isPending}
        className='flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-1'
      >
        <Controller
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <Field orientation='horizontal' className='rounded-lg border p-4'>
              <div className='flex-1'>
                <FieldLabel htmlFor='token-protection-enabled'>
                  自动禁用用户 API Key
                </FieldLabel>
                <FieldDescription>
                  关闭后停止触发新禁用，已有禁用仍需管理员解除。
                </FieldDescription>
              </div>
              <Switch
                id='token-protection-enabled'
                checked={field.value}
                onCheckedChange={field.onChange}
              />
            </Field>
          )}
        />
        <p className='text-muted-foreground text-sm'>
          按顺序匹配，采用首条命中规则。已开始的流式响应会结束，无法更改已发出的
          HTTP 状态码。
        </p>
        {rules.fields.length === 0 && (
          <p className='text-muted-foreground py-6 text-center'>
            尚未配置规则，添加后保存即可生效。
          </p>
        )}
        {rules.fields.map((rule, index) => (
          <TokenProtectionRuleFields
            key={rule.id}
            form={form}
            index={index}
            count={rules.fields.length}
            onMove={(to) => rules.move(index, to)}
            onRemove={() => rules.remove(index)}
          />
        ))}
        <Button
          type='button'
          variant='outline'
          disabled={rules.fields.length >= 64}
          onClick={() => rules.append(newTokenProtectionRule())}
        >
          添加规则
        </Button>
      </fieldset>
      <div className='flex justify-end border-t pt-4'>
        <Button type='submit' disabled={save.isPending}>
          {save.isPending ? '正在保存…' : '保存规则'}
        </Button>
      </div>
    </form>
  )
}
