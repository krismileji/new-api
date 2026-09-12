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
import {
  useFieldArray,
  useWatch,
  type FieldPath,
  type UseFormReturn,
} from 'react-hook-form'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  MAX_CUSTOM_UPSTREAM_ENTRIES,
  type UpstreamConfigFormValues,
} from '../lib/schema'

type CustomMetricName = 'ratio' | 'balance' | `variableRequests.${number}`
type CustomKeyValueArrayName =
  | `customConfig.${CustomMetricName}.request.query`
  | `customConfig.${CustomMetricName}.request.headers`
  | `customConfig.${CustomMetricName}.request.form`

type ChannelMonitorCustomKeyValueEditorProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  name: CustomKeyValueArrayName
  label: string
  allowVariables?: boolean
  disabled?: boolean
}

type ChannelMonitorCustomKeyValueRowProps =
  ChannelMonitorCustomKeyValueEditorProps & {
    index: number
    onRemove: () => void
  }

function fieldName(value: string): FieldPath<UpstreamConfigFormValues> {
  return value as FieldPath<UpstreamConfigFormValues>
}

function ChannelMonitorCustomKeyValueRow(
  props: ChannelMonitorCustomKeyValueRowProps
) {
  const secret = useWatch({
    control: props.form.control,
    name: fieldName(`${props.name}.${props.index}.secret`),
  })
  const hasValue = useWatch({
    control: props.form.control,
    name: fieldName(`${props.name}.${props.index}.hasValue`),
  })
  const valueTemplate = useWatch({
    control: props.form.control,
    name: fieldName(`${props.name}.${props.index}.valueTemplate`),
  })
  const usesVariable = props.allowVariables && valueTemplate !== undefined
  const requests = useWatch({
    control: props.form.control,
    name: 'customConfig.variableRequests',
  })
  const availableRequests = requests.filter((request) =>
    request.variables.some((variable) =>
      /^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(variable.name)
    )
  )
  let placeholder = hasValue === true ? '已配置，留空保持不变' : '值'
  if (usesVariable) placeholder = 'Bearer {{token}}'

  return (
    <div className='grid min-w-0 grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_auto_auto] items-start gap-2'>
      <FormField
        control={props.form.control}
        name={fieldName(`${props.name}.${props.index}.key`)}
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <Input
                placeholder='名称'
                aria-label={`${props.label} ${props.index + 1} 名称`}
                value={typeof field.value === 'string' ? field.value : ''}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={props.form.control}
        name={fieldName(
          `${props.name}.${props.index}.${usesVariable ? 'valueTemplate' : 'value'}`
        )}
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <Input
                type={!usesVariable && secret === true ? 'password' : 'text'}
                placeholder={placeholder}
                aria-label={`${props.label} ${props.index + 1} ${usesVariable ? '变量模板' : '值'}`}
                autoComplete='off'
                value={typeof field.value === 'string' ? field.value : ''}
                onBlur={field.onBlur}
                onChange={field.onChange}
                name={field.name}
                ref={field.ref}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={props.form.control}
        name={fieldName(`${props.name}.${props.index}.secret`)}
        render={({ field }) => (
          <FormItem className='flex h-9 items-center gap-1.5'>
            <FormControl>
              <Switch
                checked={usesVariable || field.value === true}
                disabled={props.disabled || usesVariable}
                onCheckedChange={field.onChange}
                aria-label={`${props.label} ${props.index + 1} 使用敏感值`}
              />
            </FormControl>
            <span className='text-muted-foreground text-xs'>敏感</span>
          </FormItem>
        )}
      />
      <Button
        type='button'
        variant='ghost'
        size='icon-sm'
        onClick={props.onRemove}
        disabled={props.disabled}
        aria-label={`删除${props.label} ${props.index + 1}`}
      >
        <HugeiconsIcon icon={Delete02Icon} aria-hidden='true' />
      </Button>
      {props.allowVariables ? (
        <div className='col-span-4 flex items-center gap-2'>
          <Switch
            checked={usesVariable === true}
            disabled={props.disabled}
            onCheckedChange={(checked) => {
              const name =
                availableRequests[0]?.variables.find((variable) =>
                  /^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(variable.name)
                )?.name || 'token'
              props.form.setValue(
                fieldName(`${props.name}.${props.index}.valueTemplate`),
                checked ? `{{${name}}}` : undefined,
                { shouldDirty: true, shouldValidate: true }
              )
            }}
            aria-label={`${props.label} ${props.index + 1} 使用变量模板`}
          />
          <span className='text-muted-foreground text-xs'>
            {usesVariable
              ? '变量值按敏感信息处理，可添加 Bearer 等前缀'
              : '使用独立请求的变量'}
          </span>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  disabled={props.disabled || availableRequests.length === 0}
                />
              }
            >
              插入变量
            </DropdownMenuTrigger>
            <DropdownMenuContent align='start'>
              {availableRequests.map((request) => (
                <DropdownMenuGroup key={request.id}>
                  <DropdownMenuLabel>{request.name}</DropdownMenuLabel>
                  {request.variables
                    .filter((variable) =>
                      /^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(variable.name)
                    )
                    .map((variable) => (
                      <DropdownMenuItem
                        key={variable.name}
                        onClick={() => {
                          const template =
                            typeof valueTemplate === 'string'
                              ? valueTemplate
                              : ''
                          props.form.setValue(
                            fieldName(
                              `${props.name}.${props.index}.valueTemplate`
                            ),
                            `${template}{{${variable.name}}}`,
                            { shouldDirty: true, shouldValidate: true }
                          )
                        }}
                      >
                        <code>{`{{${variable.name}}}`}</code>
                      </DropdownMenuItem>
                    ))}
                </DropdownMenuGroup>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ) : null}
    </div>
  )
}

export function ChannelMonitorCustomKeyValueEditor(
  props: ChannelMonitorCustomKeyValueEditorProps
) {
  const entries = useFieldArray<
    UpstreamConfigFormValues,
    CustomKeyValueArrayName
  >({
    control: props.form.control,
    name: props.name,
  })

  return (
    <div className='flex min-w-0 flex-col gap-2'>
      <div className='flex items-center justify-between gap-3'>
        <span className='text-sm font-medium'>{props.label}</span>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={
            props.disabled ||
            entries.fields.length >= MAX_CUSTOM_UPSTREAM_ENTRIES
          }
          onClick={() =>
            entries.append({
              key: '',
              value: '',
              secret: false,
              hasValue: false,
            })
          }
        >
          <HugeiconsIcon
            icon={Add01Icon}
            data-icon='inline-start'
            aria-hidden='true'
          />
          添加
        </Button>
      </div>
      {entries.fields.length === 0 ? (
        <span className='text-muted-foreground text-sm'>未配置</span>
      ) : (
        <div className='flex flex-col gap-2'>
          {entries.fields.map((entry, index) => (
            <ChannelMonitorCustomKeyValueRow
              key={entry.id}
              form={props.form}
              name={props.name}
              label={props.label}
              allowVariables={props.allowVariables}
              disabled={props.disabled}
              index={index}
              onRemove={() => entries.remove(index)}
            />
          ))}
        </div>
      )}
    </div>
  )
}
