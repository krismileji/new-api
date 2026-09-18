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
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { Button } from '@/components/ui/button'
import { FieldLegend, FieldSet } from '@/components/ui/field'
import {
  FormControl,
  FormDescription,
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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import { useUpstreamAccounts } from '../api-upstream-accounts'
import { getChannelMonitorConversionFactor } from '../lib/cost-conversion'
import {
  MAX_CUSTOM_UPSTREAM_BALANCE,
  type UpstreamConfigFormValues,
} from '../lib/schema'
import { ChannelMonitorCustomRequestFields } from './channel-monitor-custom-request-fields'

type CustomMetricName = 'ratio' | 'balance'

type ChannelMonitorCustomUpstreamFieldsProps = {
  form: UseFormReturn<UpstreamConfigFormValues>
  independent?: boolean
  hideRatio?: boolean
  hideBalance?: boolean
  allowAccountBalance?: boolean
}

type CustomMetricFieldsProps = ChannelMonitorCustomUpstreamFieldsProps & {
  metric: CustomMetricName
  reuseRequest: boolean
}

type CustomRequestFieldsProps = ChannelMonitorCustomUpstreamFieldsProps & {
  metric: CustomMetricName
  showRequest: boolean
}

function CustomRequestFields(props: CustomRequestFieldsProps) {
  const prefix = `customConfig.${props.metric}` as const
  const responseType = useWatch({
    control: props.form.control,
    name: `${prefix}.result.responseType`,
  })

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      {props.showRequest ? (
        <ChannelMonitorCustomRequestFields
          form={props.form}
          prefix={`${prefix}.request`}
          allowVariables
        />
      ) : null}

      <div className='grid min-w-0 items-start gap-4 sm:grid-cols-[10rem_minmax(0,1fr)_10rem]'>
        <FormField
          control={props.form.control}
          name={`${prefix}.result.responseType`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>响应格式</FormLabel>
              <FormControl>
                <ToggleGroup
                  value={[field.value]}
                  onValueChange={(values) => {
                    const value = values.find((item) => item !== field.value)
                    if (value !== 'json' && value !== 'text') return
                    field.onChange(value)
                  }}
                  variant='outline'
                  spacing={2}
                  className='grid w-full grid-cols-2'
                >
                  <ToggleGroupItem value='json' className='w-full'>
                    JSON
                  </ToggleGroupItem>
                  <ToggleGroupItem value='text' className='w-full'>
                    文本
                  </ToggleGroupItem>
                </ToggleGroup>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.result.valuePath`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>JSON 取值路径 / 表达式</FormLabel>
              <FormControl>
                <Input
                  placeholder={`data.${props.metric}`}
                  disabled={responseType === 'text'}
                  value={field.value}
                  onBlur={field.onBlur}
                  onChange={field.onChange}
                  name={field.name}
                  ref={field.ref}
                />
              </FormControl>
              <FormDescription className='min-w-0 break-words'>
                {responseType === 'text' ? (
                  '文本响应直接取数字，再乘结果乘数。'
                ) : (
                  <>
                    直接填写路径，或以 = 开头计算；用 json("路径") 取值，支持
                    +、-、*、/ 和括号，计算后再乘结果乘数。
                    <code className='mt-1 block break-all'>
                      =json("data.total") - json("data.used")
                    </code>
                  </>
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name={`${prefix}.result.multiplier`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>结果乘数</FormLabel>
              <InputGroup className='ring-inset'>
                <InputGroupAddon>×</InputGroupAddon>
                <FormControl data-slot='input-group-control'>
                  <InputGroupInput
                    type='number'
                    min={0}
                    max={1_000_000}
                    step='any'
                    inputMode='decimal'
                    value={field.value}
                    onBlur={field.onBlur}
                    onChange={field.onChange}
                    name={field.name}
                    ref={field.ref}
                  />
                </FormControl>
              </InputGroup>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
    </div>
  )
}

function CustomMetricFields(props: CustomMetricFieldsProps) {
  const prefix = `customConfig.${props.metric}` as const
  const source = useWatch({
    control: props.form.control,
    name: `${prefix}.source`,
  })
  const isRatio = props.metric === 'ratio'

  return (
    <FieldSet className='min-w-0 rounded-md border p-4'>
      <FieldLegend variant='label' className='px-1'>
        {isRatio ? '上游倍率来源' : '上游余额来源'}
      </FieldLegend>
      <FormField
        control={props.form.control}
        name={`${prefix}.source`}
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <ToggleGroup
                value={[field.value]}
                onValueChange={(values) => {
                  const value = values.find((item) => item !== field.value)
                  if (
                    value !== 'fixed' &&
                    value !== 'http' &&
                    value !== 'account'
                  ) {
                    return
                  }
                  field.onChange(value)
                  if (value !== 'http') {
                    props.form.setValue(
                      'customConfig.balanceReuseRatioRequest',
                      false,
                      { shouldValidate: true }
                    )
                  }
                }}
                variant='outline'
                spacing={2}
                className='flex w-full flex-wrap'
              >
                <ToggleGroupItem value='fixed' className='flex-1'>
                  固定输入
                </ToggleGroupItem>
                <ToggleGroupItem value='http' className='flex-1'>
                  接口查询
                </ToggleGroupItem>
                {!isRatio && props.allowAccountBalance ? (
                  <ToggleGroupItem value='account' className='flex-1'>
                    关联上游账户
                  </ToggleGroupItem>
                ) : null}
              </ToggleGroup>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {source === 'fixed' ? (
        <FormField
          control={props.form.control}
          name={`${prefix}.fixedValue`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{isRatio ? '固定倍率' : '固定余额'}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={isRatio ? 0 : -MAX_CUSTOM_UPSTREAM_BALANCE}
                  max={isRatio ? 1_000_000 : MAX_CUSTOM_UPSTREAM_BALANCE}
                  step='any'
                  inputMode='decimal'
                  value={field.value}
                  onBlur={field.onBlur}
                  onChange={field.onChange}
                  name={field.name}
                  ref={field.ref}
                />
              </FormControl>
              <FormDescription>
                {props.independent
                  ? '在任务检查时作为指标值使用。'
                  : '保存后立即写入渠道监控。'}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      ) : null}
      {source === 'account' ? <AccountBalanceSource form={props.form} /> : null}
      {source === 'http' ? (
        <CustomRequestFields
          form={props.form}
          metric={props.metric}
          showRequest={!props.reuseRequest}
        />
      ) : null}
    </FieldSet>
  )
}

function AccountBalanceSource(props: {
  form: UseFormReturn<UpstreamConfigFormValues>
}) {
  const accounts = useUpstreamAccounts()
  const accountId = useWatch({
    control: props.form.control,
    name: 'customConfig.balance.accountId',
  })
  const selected = accounts.data?.find((account) => account.id === accountId)
  const accountOptions = [
    {
      value: 0,
      label: accounts.isPending ? '正在加载账户…' : '请选择上游账户',
    },
    ...(accountId && !selected
      ? [{ value: accountId, label: `账户 #${accountId}（暂不可用）` }]
      : []),
    ...(accounts.data ?? []).map((account) => ({
      value: account.id,
      label: `${account.name} · #${account.id}`,
    })),
  ]
  return (
    <>
      <FormField
        control={props.form.control}
        name='customConfig.balance.accountId'
        render={({ field }) => (
          <FormItem>
            <FormLabel>上游余额账户</FormLabel>
            <Select
              items={accountOptions}
              name={field.name}
              value={field.value ?? 0}
              disabled={accounts.isPending || accounts.isError}
              onValueChange={(value) => {
                if (value !== null) field.onChange(value)
              }}
            >
              <FormControl>
                <SelectTrigger
                  className='w-full min-w-0'
                  ref={field.ref}
                  onBlur={field.onBlur}
                >
                  <SelectValue className='min-w-0 truncate' />
                </SelectTrigger>
              </FormControl>
              <SelectContent
                alignItemWithTrigger={false}
                className='max-h-[min(20rem,var(--available-height))]'
              >
                <SelectGroup>
                  {accountOptions.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      <span className='break-all whitespace-normal'>
                        {item.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FormDescription>
              仅复用账户余额和换算方式，当前倍率、请求地址、认证及自定义变量保持独立。
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      {accounts.isError ? (
        <p role='alert' className='text-destructive text-sm'>
          账户加载失败。
          <Button
            type='button'
            variant='link'
            onClick={() => void accounts.refetch()}
          >
            重试加载账户
          </Button>
        </p>
      ) : null}
      {accounts.data?.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          尚无上游账户，请先在渠道监控的“上游账户”中创建，或改用固定输入、接口查询。
        </p>
      ) : null}
      {selected ? (
        <p className='text-muted-foreground text-sm'>
          当前账户余额：{selected.balance ?? '尚未同步'}；换算系数：
          {getChannelMonitorConversionFactor(
            selected.upstream.cost_conversion ?? { mode: 'none' }
          ) ?? '未配置'}
          。保存后持续使用账户最新配置。
        </p>
      ) : null}
    </>
  )
}

export function ChannelMonitorCustomUpstreamFields(
  props: ChannelMonitorCustomUpstreamFieldsProps
) {
  const ratioSource = useWatch({
    control: props.form.control,
    name: 'customConfig.ratio.source',
  })
  const balanceSource = useWatch({
    control: props.form.control,
    name: 'customConfig.balance.source',
  })
  const reuseRequest = useWatch({
    control: props.form.control,
    name: 'customConfig.balanceReuseRatioRequest',
  })
  const canReuseRequest =
    !props.hideRatio &&
    !props.hideBalance &&
    ratioSource === 'http' &&
    balanceSource === 'http'

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      {!props.hideRatio ? (
        <CustomMetricFields
          form={props.form}
          metric='ratio'
          independent={props.independent}
          reuseRequest={false}
        />
      ) : null}
      {canReuseRequest ? (
        <FormField
          control={props.form.control}
          name='customConfig.balanceReuseRatioRequest'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-4 rounded-md border px-4 py-3'>
              <div className='flex min-w-0 flex-col gap-1'>
                <FormLabel>余额复用倍率接口</FormLabel>
                <FormDescription>
                  只发送一次请求，余额使用独立的取值路径或表达式和结果乘数。
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  aria-label='余额复用倍率接口'
                />
              </FormControl>
            </FormItem>
          )}
        />
      ) : null}
      {!props.hideBalance ? (
        <CustomMetricFields
          form={props.form}
          metric='balance'
          allowAccountBalance={props.allowAccountBalance}
          independent={props.independent}
          reuseRequest={reuseRequest && canReuseRequest}
        />
      ) : null}
    </div>
  )
}
