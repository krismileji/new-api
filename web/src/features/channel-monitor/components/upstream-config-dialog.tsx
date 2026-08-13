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
import {
  ClipboardPasteIcon,
  Copy01Icon,
  LinkSquare01Icon,
  Refresh01Icon,
  TestTubeIcon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { toast } from 'sonner'

import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Combobox,
  ComboboxCollection,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@/components/ui/combobox'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import {
  applyChannelMonitorUpstreamGroup,
  fetchChannelMonitorSub2APIUpstreamVersion,
  listChannelMonitorUpstreamGroups,
  saveChannelMonitorUpstreamConfig,
  testChannelMonitorUpstreamConfig,
} from '../api'
import { createChannelMonitorCustomFormConfig } from '../lib/custom-upstream'
import { handleChannelMonitorMutationError } from '../lib/error'
import { formatMonitorRatio } from '../lib/format'
import {
  createUpstreamConfigSchema,
  MAX_BALANCE_THRESHOLD,
  type UpstreamConfigFormValues,
} from '../lib/schema'
import { createChannelMonitorUpstreamRequest } from '../lib/upstream-request'
import type {
  ChannelMonitorItem,
  ChannelMonitorCostConversion,
  ChannelMonitorPolicyAction,
  ChannelMonitorUpstreamGroup,
  ChannelMonitorUpstreamAuthType,
  NewAPIGroupRatioResult,
} from '../types'
import { ChannelMonitorCostConversionFields } from './channel-monitor-cost-conversion-fields'
import { ChannelMonitorCustomUpstreamFields } from './channel-monitor-custom-upstream-fields'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { EditChannelRatioDialog } from './edit-channel-ratio-dialog'

type UpstreamConfigDialogProps = {
  channel: ChannelMonitorItem
  open: boolean
  onOpenChange: (open: boolean) => void
}

const SINGLE_CHANNEL_ACTION_OPTIONS = [
  { value: 'none', label: '仅记录' },
  { value: 'update_group_ratio', label: '更新分组倍率' },
  { value: 'disable_channel', label: '禁用此渠道' },
] satisfies Array<{ value: ChannelMonitorPolicyAction; label: string }>

const MULTIPLE_CHANNELS_ACTION_OPTIONS = [
  { value: 'none', label: '仅记录' },
  { value: 'update_group_ratio', label: '参与更新分组倍率' },
  { value: 'disable_channel', label: '禁用此渠道' },
  { value: 'remove_from_group', label: '移除当前渠道' },
] satisfies Array<{ value: ChannelMonitorPolicyAction; label: string }>

const MULTIPLE_CHANNELS_ACTION_DESCRIPTIONS: Record<
  ChannelMonitorPolicyAction,
  string
> = {
  none: '目标倍率高于当前分组倍率时仅记录结果',
  update_group_ratio: '更新时采用参与渠道中的最高目标倍率',
  disable_channel: '目标倍率高于当前分组倍率时禁用此渠道',
  remove_from_group: '仅解除当前分组关联；若这是渠道的唯一分组则不会移除',
}

const SUB2API_ACCESS_TOKEN_COMMAND =
  "copy(localStorage.getItem('auth_token') || '')"
const SUB2API_REFRESH_TOKEN_COMMAND =
  "copy(localStorage.getItem('refresh_token') || '')"

export function UpstreamConfigDialog(props: UpstreamConfigDialogProps) {
  const queryClient = useQueryClient()
  const { copyToClipboard } = useCopyToClipboard({
    successMessage: '提取 Token 命令已复制',
    errorMessage: '复制提取命令失败',
  })
  const [testResult, setTestResult] = useState<NewAPIGroupRatioResult | null>(
    null
  )
  const [testedAuthType, setTestedAuthType] =
    useState<ChannelMonitorUpstreamAuthType | null>(null)
  const [ratioEditorOpen, setRatioEditorOpen] = useState(false)
  const [upstreamVersion, setUpstreamVersion] = useState<string | null>(null)
  const savedUpstream = props.channel.upstream
  const savedCostConversion: ChannelMonitorCostConversion =
    savedUpstream?.cost_conversion ?? { mode: 'none' }
  const initialGroup = savedUpstream?.group || ''
  const [upstreamGroups, setUpstreamGroups] = useState<
    ChannelMonitorUpstreamGroup[]
  >([])
  const [groupInputValue, setGroupInputValue] = useState(initialGroup)
  const [groupComboboxOpen, setGroupComboboxOpen] = useState(false)
  const savedCredential: Parameters<typeof createUpstreamConfigSchema>[0] =
    savedUpstream
      ? {
          type: savedUpstream.type,
          baseUrl: savedUpstream.base_url,
          authType: savedUpstream.auth_type,
          hasAccessToken: savedUpstream.has_access_token,
          account: savedUpstream.account || '',
          hasPassword: savedUpstream.has_password,
        }
      : null
  const schema = createUpstreamConfigSchema(savedCredential)
  const form = useForm<UpstreamConfigFormValues>({
    resolver: zodResolver(schema) as Resolver<UpstreamConfigFormValues>,
    defaultValues: {
      upstreamType: savedUpstream?.type || 'new_api',
      baseUrl: props.channel.upstream?.base_url || props.channel.base_url,
      group: initialGroup,
      authType: props.channel.upstream?.auth_type || 'public',
      userId: props.channel.upstream?.user_id || 0,
      accessToken: '',
      refreshToken: '',
      account: savedUpstream?.account || '',
      password: '',
      singleChannelAction: savedUpstream?.single_channel_action || 'none',
      multipleChannelsAction: savedUpstream?.multiple_channels_action || 'none',
      ratioSyncEnabled: savedUpstream?.ratio_sync_enabled ?? true,
      balanceSyncEnabled: savedUpstream?.balance_sync_enabled ?? true,
      balanceWarningThreshold: savedUpstream?.balance_warning_threshold ?? null,
      balanceAutoDisableThreshold:
        savedUpstream?.balance_auto_disable_threshold ?? null,
      costConversionMode: savedCostConversion.mode,
      rechargePaidCny:
        savedCostConversion.mode === 'recharge'
          ? savedCostConversion.paid_cny
          : 1,
      rechargeCreditedUsd:
        savedCostConversion.mode === 'recharge'
          ? savedCostConversion.credited_usd
          : 1,
      subscriptionPeriod:
        savedCostConversion.mode === 'subscription'
          ? savedCostConversion.subscription_period
          : 'month',
      subscriptionPriceCny:
        savedCostConversion.mode === 'subscription'
          ? savedCostConversion.subscription_price_cny
          : 1,
      subscriptionDailyUsd:
        savedCostConversion.mode === 'subscription'
          ? savedCostConversion.subscription_daily_usd
          : 1,
      customConfig: createChannelMonitorCustomFormConfig(
        savedUpstream?.custom_config
      ),
    },
  })
  const upstreamType = useWatch({ control: form.control, name: 'upstreamType' })
  const baseUrl = useWatch({ control: form.control, name: 'baseUrl' })
  const authType = useWatch({ control: form.control, name: 'authType' })
  const accessToken = useWatch({ control: form.control, name: 'accessToken' })
  const refreshToken = useWatch({ control: form.control, name: 'refreshToken' })
  const account = useWatch({ control: form.control, name: 'account' })
  const password = useWatch({ control: form.control, name: 'password' })
  const ratioSyncEnabled = useWatch({
    control: form.control,
    name: 'ratioSyncEnabled',
  })
  const balanceSyncEnabled = useWatch({
    control: form.control,
    name: 'balanceSyncEnabled',
  })
  const multipleChannelsAction = useWatch({
    control: form.control,
    name: 'multipleChannelsAction',
  })
  const needsUserAuthentication =
    upstreamType === 'new_api' && authType === 'user'
  const isSub2API = upstreamType === 'sub2api'
  const isCustom = upstreamType === 'custom'
  const needsSub2APIToken = isSub2API && authType === 'token'
  const needsSub2APIRefreshToken = isSub2API && authType === 'refresh_token'
  const usesSub2APITokenCredential =
    needsSub2APIToken || needsSub2APIRefreshToken
  const needsSub2APIAccount = isSub2API && authType === 'account'
  const hasMatchingSavedAccessToken =
    savedCredential?.hasAccessToken === true &&
    savedCredential.type === upstreamType &&
    savedCredential.authType === authType
  const hasSavedSub2APIToken =
    savedCredential?.hasAccessToken === true &&
    savedCredential.type === 'sub2api' &&
    savedCredential.authType === 'token' &&
    savedCredential.baseUrl === baseUrl
  const hasSavedSub2APIRefreshToken =
    savedCredential?.hasAccessToken === true &&
    savedCredential.type === 'sub2api' &&
    savedCredential.authType === 'refresh_token' &&
    savedCredential.baseUrl === baseUrl
  const hasSub2APIToken = hasSavedSub2APIToken || accessToken.trim().length > 0
  const hasSub2APIRefreshToken =
    hasSavedSub2APIRefreshToken || refreshToken.trim().length > 0
  const hasMatchingSavedPassword =
    savedCredential?.hasPassword === true &&
    savedCredential.type === upstreamType &&
    savedCredential.baseUrl === baseUrl &&
    savedCredential.authType === authType &&
    savedCredential.account === account.trim()
  const hasSub2APIAccountCredential =
    account.trim().length > 0 &&
    (hasMatchingSavedPassword || password.length > 0)
  const canApplyGroup =
    !isCustom &&
    (needsUserAuthentication ||
      (needsSub2APIToken && hasSub2APIToken) ||
      (needsSub2APIRefreshToken && hasSub2APIRefreshToken) ||
      (needsSub2APIAccount && hasSub2APIAccountCredential))
  const canLoadGroups =
    !isCustom &&
    (!isSub2API ||
      (needsSub2APIToken && hasSub2APIToken) ||
      (needsSub2APIRefreshToken && hasSub2APIRefreshToken) ||
      (needsSub2APIAccount && hasSub2APIAccountCredential))
  const authDescription =
    authType === 'public'
      ? '无需账号，读取公开分组倍率'
      : '读取指定用户的实际分组倍率'
  let sub2APIAuthDescription = '使用当前渠道配置的 API Key 读取新版倍率和余额'
  if (authType === 'account') {
    sub2APIAuthDescription = '使用登录邮箱和密码自动获取并缓存访问 Token'
  } else if (authType === 'token') {
    sub2APIAuthDescription = '使用手动获取的旧版 Token 读取倍率、余额和分组'
  } else if (authType === 'refresh_token') {
    sub2APIAuthDescription = '使用 Refresh Token 自动换取并刷新访问 Token'
  }
  let applyGroupDescription =
    '应用分组会保存配置，并将当前渠道的全部上游令牌切换到该分组'
  if (!canApplyGroup) {
    if (needsSub2APIAccount) {
      applyGroupDescription = '应用分组需要先填写登录邮箱和密码'
    } else if (needsSub2APIRefreshToken) {
      applyGroupDescription = '应用分组需要先填写 Refresh Token'
    } else if (isSub2API) {
      applyGroupDescription = '应用分组需要先填写手动 Token'
    } else {
      applyGroupDescription = '应用分组需要先选择用户认证'
    }
  }
  let upstreamTypeDescription = '读取 New API 分组倍率'
  if (isSub2API) {
    if (authType === 'api_key') {
      upstreamTypeDescription = '使用当前渠道 API Key 读取新版倍率和余额'
    } else if (authType === 'account') {
      upstreamTypeDescription = '自动登录 Sub2API 后读取倍率、余额和分组'
    } else {
      upstreamTypeDescription =
        authType === 'refresh_token'
          ? '使用 Refresh Token 自动读取倍率、余额和分组'
          : '使用手动 Token 读取倍率、余额和分组'
    }
  } else if (isCustom) {
    upstreamTypeDescription = '通过固定值或自定义接口读取倍率和余额'
  }
  let groupSourceDescription = '从 New API 获取可用分组，也可直接填写名称'
  if (isSub2API) {
    if (authType === 'api_key') {
      groupSourceDescription =
        'API Key 认证不提供分组列表，请直接填写分组名称或数字 ID'
    } else if (authType === 'account') {
      groupSourceDescription =
        '账号密码会自动换取 Token，可获取可用分组，也可直接填写分组名称或数字 ID'
    } else {
      groupSourceDescription =
        authType === 'refresh_token'
          ? 'Refresh Token 可自动获取可用分组，也可直接填写分组名称或数字 ID'
          : '手动 Token 可获取可用分组，也可直接填写分组名称或数字 ID'
    }
  } else if (isCustom) {
    groupSourceDescription = '自定义上游分组为可选项，仅用于展示和记录'
  }
  let testResultTitle = '测试成功'
  if (testedAuthType === 'token') {
    testResultTitle = '手动 Token 测试成功'
  } else if (testedAuthType === 'refresh_token') {
    testResultTitle = 'Refresh Token 测试成功'
  }
  const upstreamGroupByName = useMemo(
    () => new Map(upstreamGroups.map((group) => [group.name, group])),
    [upstreamGroups]
  )
  const upstreamGroupItems = useMemo(() => {
    const names = upstreamGroups.map((group) => group.name)
    const customGroup = groupInputValue.trim()
    if (customGroup && !names.includes(customGroup)) names.push(customGroup)
    return names
  }, [groupInputValue, upstreamGroups])

  const saveMutation = useMutation({
    mutationFn: saveChannelMonitorUpstreamConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('上游配置已保存')
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      props.onOpenChange(false)
    },
  })
  const testMutation = useMutation({
    mutationFn: testChannelMonitorUpstreamConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response, variables) => {
      setTestResult(response.data)
      setTestedAuthType(variables.config.auth_type)
      if (response.data.balance.error) {
        toast.warning('上游倍率获取成功，但余额获取失败')
      } else {
        toast.success('上游倍率获取成功')
      }
    },
  })
  const versionMutation = useMutation({
    mutationFn: fetchChannelMonitorSub2APIUpstreamVersion,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      setUpstreamVersion(response.data.version)
      toast.success(`上游版本：${response.data.version}`)
    },
  })
  const groupsMutation = useMutation({
    mutationFn: (values: UpstreamConfigFormValues) => {
      const config = createChannelMonitorUpstreamRequest(values)
      return listChannelMonitorUpstreamGroups({
        channelId: props.channel.id,
        config,
      })
    },
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      setUpstreamGroups(response.data.groups)
      const appliedGroup = response.data.applied_group?.trim()
      if (appliedGroup) {
        form.setValue('group', appliedGroup, {
          shouldDirty: true,
          shouldValidate: true,
        })
        setGroupInputValue(appliedGroup)
      }
      toast.success(
        appliedGroup
          ? `已获取 ${response.data.groups.length} 个上游分组，并自动选中 ${appliedGroup}`
          : `已获取 ${response.data.groups.length} 个上游分组`
      )
      if (response.data.applied_group_error) {
        toast.warning(response.data.applied_group_error)
      }
    },
  })
  const applyGroupMutation = useMutation({
    mutationFn: async (values: UpstreamConfigFormValues) => {
      await saveChannelMonitorUpstreamConfig({
        channelId: props.channel.id,
        config: createChannelMonitorUpstreamRequest(values),
      })
      try {
        const response = await applyChannelMonitorUpstreamGroup(
          props.channel.id
        )
        return { success: true as const, response }
      } catch (applyError) {
        return { success: false as const, applyError }
      }
    },
    onError: handleChannelMonitorMutationError,
    onSuccess: (result, values) => {
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      if (!result.success) {
        const errorMessage =
          result.applyError instanceof Error && result.applyError.message
            ? `：${result.applyError.message}`
            : ''
        toast.error(`上游配置已保存，但切换上游令牌分组失败${errorMessage}`)
        return
      }

      queryClient.invalidateQueries({
        queryKey: ['channel-monitor-history', props.channel.id],
      })
      toast.success(
        `已将 ${result.response.data.keys_updated} 个上游令牌切换到分组 ${values.group.trim()}，上游倍率 ${formatMonitorRatio(result.response.data.result.ratio)}，成本倍率 ${formatMonitorRatio(result.response.data.result.cost_ratio)}`
      )
      props.onOpenChange(false)
    },
  })

  const requireGroup = (values: UpstreamConfigFormValues) => {
    if (values.upstreamType === 'custom') return true
    if (values.group.trim()) return true
    form.setError('group', {
      type: 'manual',
      message: '请输入上游分组',
    })
    return false
  }

  const handleSave = form.handleSubmit((values) => {
    if (!requireGroup(values)) return
    saveMutation.mutate({
      channelId: props.channel.id,
      config: createChannelMonitorUpstreamRequest(values),
    })
  })
  const handleTest = form.handleSubmit((values) => {
    if (!requireGroup(values)) return
    setTestResult(null)
    setTestedAuthType(values.authType)
    testMutation.mutate({
      channelId: props.channel.id,
      config: createChannelMonitorUpstreamRequest(values),
    })
  })
  const handleTestSub2APICredential = async (
    testAuthType: 'token' | 'refresh_token'
  ) => {
    const fieldName =
      testAuthType === 'refresh_token' ? 'refreshToken' : 'accessToken'
    const values = form.getValues()
    if (!requireGroup(values)) return

    const credential = values[fieldName].trim()
    const hasSavedCredential =
      testAuthType === 'refresh_token'
        ? hasSavedSub2APIRefreshToken
        : hasSavedSub2APIToken
    if (!credential && !hasSavedCredential) {
      form.setError(fieldName, {
        type: 'manual',
        message:
          testAuthType === 'refresh_token'
            ? '请输入 Sub2API Refresh Token'
            : '请输入 Sub2API Token（旧版访问令牌）',
      })
      return
    }
    if (credential.length > 4096) {
      form.setError(fieldName, {
        type: 'manual',
        message:
          testAuthType === 'refresh_token'
            ? 'Refresh Token 过长'
            : '访问令牌过长',
      })
      return
    }

    setTestResult(null)
    setTestedAuthType(testAuthType)
    testMutation.mutate({
      channelId: props.channel.id,
      config: createChannelMonitorUpstreamRequest(values, testAuthType),
    })
  }
  const handleLoadGroups = form.handleSubmit((values) => {
    groupsMutation.mutate(values)
  })
  const handleApplyGroup = form.handleSubmit((values) => {
    if (!requireGroup(values)) return
    applyGroupMutation.mutate(values)
  })
  const handleOpenSub2APILogin = () => {
    const value = form.getValues('baseUrl').trim()
    try {
      const loginUrl = new URL(value)
      if (loginUrl.protocol !== 'http:' && loginUrl.protocol !== 'https:') {
        throw new Error('invalid protocol')
      }
      let basePath = loginUrl.pathname.replace(/\/+$/, '')
      if (basePath.endsWith('/v1')) {
        basePath = basePath.slice(0, -3)
      }
      loginUrl.pathname = `${basePath}/login`
      loginUrl.search = ''
      loginUrl.hash = ''
      form.clearErrors('baseUrl')
      window.open(loginUrl.toString(), '_blank', 'noopener,noreferrer')
    } catch {
      form.setError('baseUrl', { message: '请输入有效的面板地址' })
    }
  }
  const handlePasteToken = async (
    fieldName: 'accessToken' | 'refreshToken',
    tokenLabel: string
  ) => {
    if (!navigator.clipboard?.readText) {
      toast.error('当前浏览器不支持读取剪贴板')
      return
    }
    try {
      const token = (await navigator.clipboard.readText()).trim()
      if (!token) {
        toast.error(`剪贴板中没有${tokenLabel}`)
        return
      }
      form.setValue(fieldName, token, {
        shouldDirty: true,
        shouldValidate: true,
      })
      setTestResult(null)
      setTestedAuthType(null)
      toast.success(`${tokenLabel} 已粘贴`)
    } catch {
      toast.error('读取剪贴板失败，请手动粘贴')
    }
  }
  const handleFetchVersion = () => {
    const value = baseUrl.trim()
    if (!value) return
    setUpstreamVersion(null)
    versionMutation.mutate({ channelId: props.channel.id, baseUrl: value })
  }
  const pending =
    saveMutation.isPending ||
    testMutation.isPending ||
    groupsMutation.isPending ||
    applyGroupMutation.isPending ||
    versionMutation.isPending

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'flex flex-col sm:max-w-3xl'
        )}
      >
        <DialogHeader className='shrink-0 pr-10'>
          <DialogTitle>上游配置与策略</DialogTitle>
          <DialogDescription>
            {props.channel.name} · ID {props.channel.id}
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 min-w-0 flex-1 [scrollbar-gutter:stable] overflow-x-hidden overflow-y-auto overscroll-contain pr-2'>
          <Form {...form}>
            <form className='flex min-w-0 flex-col gap-5' onSubmit={handleSave}>
              <FormField
                control={form.control}
                name='upstreamType'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>上游类型</FormLabel>
                    <FormControl>
                      <ToggleGroup
                        value={[field.value]}
                        onValueChange={(values) => {
                          const nextValue = values.find(
                            (value) => value !== field.value
                          )
                          if (
                            nextValue !== 'new_api' &&
                            nextValue !== 'sub2api' &&
                            nextValue !== 'custom'
                          ) {
                            return
                          }
                          field.onChange(nextValue)
                          let nextAuthType: UpstreamConfigFormValues['authType'] =
                            'public'
                          if (nextValue === 'sub2api') {
                            nextAuthType = 'api_key'
                          } else if (nextValue === 'custom') {
                            nextAuthType = 'custom'
                          }
                          form.setValue('authType', nextAuthType, {
                            shouldValidate: true,
                          })
                          form.setValue('accessToken', '')
                          form.setValue('refreshToken', '')
                          form.setValue('account', '')
                          form.setValue('password', '')
                          setUpstreamGroups([])
                          setTestResult(null)
                          setTestedAuthType(null)
                          setUpstreamVersion(null)
                        }}
                        variant='outline'
                        spacing={2}
                        className='grid w-full grid-cols-3'
                      >
                        <ToggleGroupItem value='new_api' className='w-full'>
                          New API
                        </ToggleGroupItem>
                        <ToggleGroupItem value='sub2api' className='w-full'>
                          Sub2API
                        </ToggleGroupItem>
                        <ToggleGroupItem value='custom' className='w-full'>
                          自定义
                        </ToggleGroupItem>
                      </ToggleGroup>
                    </FormControl>
                    <FormDescription>{upstreamTypeDescription}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='baseUrl'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {isCustom ? '接口基础地址' : '面板地址'}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='url'
                        placeholder='https://api.example.com'
                        autoComplete='url'
                        value={field.value}
                        onBlur={field.onBlur}
                        onChange={(event) => {
                          field.onChange(event)
                          setUpstreamGroups([])
                          setTestResult(null)
                          setTestedAuthType(null)
                          setUpstreamVersion(null)
                        }}
                        name={field.name}
                        ref={field.ref}
                      />
                    </FormControl>
                    <FormDescription>
                      {isCustom
                        ? '倍率和余额接口路径会拼接到该地址，渠道代理同样生效'
                        : '填写面板根地址，末尾的 /v1 会自动移除'}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='group'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>上游分组</FormLabel>
                    <Combobox
                      items={upstreamGroupItems}
                      value={field.value}
                      inputValue={groupInputValue}
                      open={groupComboboxOpen}
                      onOpenChange={(open) => {
                        setGroupComboboxOpen(open)
                        setGroupInputValue(open ? '' : field.value)
                      }}
                      onInputValueChange={setGroupInputValue}
                      onValueChange={(value) => {
                        if (value === null) return
                        field.onChange(value)
                        setGroupInputValue(value)
                      }}
                    >
                      <div className='flex flex-wrap gap-2'>
                        <FormControl>
                          <ComboboxInput
                            className='min-w-0 flex-1 basis-48'
                            placeholder='选择或输入上游分组'
                            maxLength={64}
                            onBlur={() => {
                              const customGroup = groupInputValue.trim()
                              if (customGroup) {
                                field.onChange(customGroup)
                                setGroupInputValue(customGroup)
                              } else {
                                setGroupInputValue(field.value)
                              }
                              field.onBlur()
                            }}
                          />
                        </FormControl>
                        {!isCustom ? (
                          <>
                            <Button
                              type='button'
                              variant='outline'
                              onClick={handleLoadGroups}
                              disabled={pending || !canLoadGroups}
                            >
                              {groupsMutation.isPending ? (
                                <Spinner data-icon='inline-start' />
                              ) : (
                                <HugeiconsIcon
                                  icon={Refresh01Icon}
                                  data-icon='inline-start'
                                />
                              )}
                              获取分组
                            </Button>
                            <Button
                              type='button'
                              variant='secondary'
                              onClick={handleApplyGroup}
                              disabled={pending || !canApplyGroup}
                            >
                              {applyGroupMutation.isPending ? (
                                <Spinner data-icon='inline-start' />
                              ) : (
                                <HugeiconsIcon
                                  icon={Tick02Icon}
                                  data-icon='inline-start'
                                />
                              )}
                              应用分组
                            </Button>
                          </>
                        ) : null}
                      </div>
                      <ComboboxContent>
                        <ComboboxList>
                          <ComboboxCollection>
                            {(groupName: string) => {
                              const group = upstreamGroupByName.get(groupName)
                              return (
                                <ComboboxItem key={groupName} value={groupName}>
                                  <span className='flex min-w-0 flex-1 items-center justify-between gap-3'>
                                    <span className='truncate'>
                                      {group
                                        ? group.name
                                        : `使用“${groupName}”`}
                                    </span>
                                    {group && (
                                      <span className='text-muted-foreground shrink-0 font-mono text-xs'>
                                        × {formatMonitorRatio(group.ratio)}
                                      </span>
                                    )}
                                  </span>
                                </ComboboxItem>
                              )
                            }}
                          </ComboboxCollection>
                        </ComboboxList>
                        <ComboboxEmpty>没有可选分组，可直接输入</ComboboxEmpty>
                      </ComboboxContent>
                    </Combobox>
                    <FormDescription>
                      {groupSourceDescription}
                      {!isCustom ? `；${applyGroupDescription}` : ''}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {isCustom ? (
                <ChannelMonitorCustomUpstreamFields form={form} />
              ) : null}

              <ChannelMonitorCostConversionFields
                form={form}
                upstreamRatio={testResult?.ratio ?? props.channel.ratio}
                onEditRatio={() => {
                  setTestResult(null)
                  setRatioEditorOpen(true)
                }}
                editRatioDisabled={pending}
              />

              <div className='grid min-w-0 gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='ratioSyncEnabled'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                      <div className='flex min-w-0 flex-col gap-1'>
                        <FormLabel>倍率同步</FormLabel>
                        <FormDescription>
                          关闭后，定时任务和渠道列表不再获取上游倍率
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          aria-label='开启上游倍率同步'
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='balanceSyncEnabled'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                      <div className='flex min-w-0 flex-col gap-1'>
                        <FormLabel>余额同步</FormLabel>
                        <FormDescription>
                          关闭后，定时任务和渠道列表不再获取上游余额
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          aria-label='开启上游余额同步'
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              </div>

              <div className='grid min-w-0 gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='balanceWarningThreshold'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>余额预警值</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={MAX_BALANCE_THRESHOLD}
                          step='any'
                          placeholder='留空关闭余额预警'
                          disabled={!balanceSyncEnabled}
                          value={field.value ?? ''}
                          onBlur={field.onBlur}
                          onChange={(event) => {
                            const value = event.target.value
                            field.onChange(value === '' ? null : Number(value))
                          }}
                          name={field.name}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormDescription>
                        {balanceSyncEnabled
                          ? '定时更新余额低于此值时标红；开启邮件通知后首次进入低余额状态会发送预警，余额恢复后可再次预警'
                          : '余额同步已关闭，不会请求上游余额或触发余额预警'}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='balanceAutoDisableThreshold'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>余额自动禁用阈值</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={MAX_BALANCE_THRESHOLD}
                          step='any'
                          placeholder='留空关闭余额自动禁用'
                          disabled={!balanceSyncEnabled}
                          value={field.value ?? ''}
                          onBlur={field.onBlur}
                          onChange={(event) => {
                            const value = event.target.value
                            field.onChange(value === '' ? null : Number(value))
                          }}
                          name={field.name}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormDescription>
                        {balanceSyncEnabled
                          ? '余额更新成功后，启用中的渠道余额低于此值会被自动禁用；余额恢复后不会自动启用'
                          : '余额同步已关闭，不会触发余额自动禁用'}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <div className='grid min-w-0 gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='singleChannelAction'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>仅剩此渠道时</FormLabel>
                      <Select
                        items={SINGLE_CHANNEL_ACTION_OPTIONS}
                        value={field.value}
                        onValueChange={(value) =>
                          value !== null && field.onChange(value)
                        }
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {SINGLE_CHANNEL_ACTION_OPTIONS.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                              >
                                {option.label}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        目标倍率高于当前分组倍率时执行
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='multipleChannelsAction'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>存在多个渠道时</FormLabel>
                      <Select
                        items={MULTIPLE_CHANNELS_ACTION_OPTIONS}
                        value={field.value}
                        onValueChange={(value) =>
                          value !== null && field.onChange(value)
                        }
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {MULTIPLE_CHANNELS_ACTION_OPTIONS.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                              >
                                {option.label}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {
                          MULTIPLE_CHANNELS_ACTION_DESCRIPTIONS[
                            multipleChannelsAction
                          ]
                        }
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              {upstreamType === 'new_api' ? (
                <FormField
                  control={form.control}
                  name='authType'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>认证方式</FormLabel>
                      <FormControl>
                        <ToggleGroup
                          value={[field.value]}
                          onValueChange={(values) => {
                            const nextValue = values.find(
                              (value) => value !== field.value
                            )
                            if (
                              nextValue === 'public' ||
                              nextValue === 'user'
                            ) {
                              field.onChange(nextValue)
                              form.setValue('accessToken', '')
                              setUpstreamGroups([])
                              setTestResult(null)
                              setTestedAuthType(null)
                            }
                          }}
                          variant='outline'
                          spacing={2}
                          className='grid w-full grid-cols-2'
                        >
                          <ToggleGroupItem value='public' className='w-full'>
                            公开接口
                          </ToggleGroupItem>
                          <ToggleGroupItem value='user' className='w-full'>
                            用户认证
                          </ToggleGroupItem>
                        </ToggleGroup>
                      </FormControl>
                      <FormDescription>{authDescription}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ) : null}

              {isSub2API ? (
                <FormField
                  control={form.control}
                  name='authType'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>认证方式</FormLabel>
                      <FormControl>
                        <ToggleGroup
                          value={[field.value]}
                          onValueChange={(values) => {
                            const nextValue = values.find(
                              (value) => value !== field.value
                            )
                            if (
                              nextValue !== 'api_key' &&
                              nextValue !== 'account' &&
                              nextValue !== 'token' &&
                              nextValue !== 'refresh_token'
                            ) {
                              return
                            }
                            field.onChange(nextValue)
                            form.setValue('password', '')
                            setUpstreamGroups([])
                            setTestResult(null)
                            setTestedAuthType(null)
                            setUpstreamVersion(null)
                          }}
                          variant='outline'
                          spacing={2}
                          className='grid w-full grid-cols-2 sm:grid-cols-4'
                        >
                          <ToggleGroupItem value='api_key' className='w-full'>
                            API Key（新版）
                          </ToggleGroupItem>
                          <ToggleGroupItem value='account' className='w-full'>
                            账号密码
                          </ToggleGroupItem>
                          <ToggleGroupItem
                            value='refresh_token'
                            className='w-full'
                          >
                            Refresh Token
                          </ToggleGroupItem>
                          <ToggleGroupItem value='token' className='w-full'>
                            手动 Token
                          </ToggleGroupItem>
                        </ToggleGroup>
                      </FormControl>
                      <FormDescription>
                        {sub2APIAuthDescription}
                      </FormDescription>
                      <div className='flex flex-wrap items-center gap-2'>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={handleFetchVersion}
                          disabled={pending || !baseUrl.trim()}
                        >
                          {versionMutation.isPending ? (
                            <Spinner data-icon='inline-start' />
                          ) : (
                            <HugeiconsIcon
                              icon={Refresh01Icon}
                              data-icon='inline-start'
                            />
                          )}
                          获取上游版本
                        </Button>
                        {usesSub2APITokenCredential ? (
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={handleOpenSub2APILogin}
                            disabled={pending || !baseUrl.trim()}
                          >
                            <HugeiconsIcon
                              icon={LinkSquare01Icon}
                              data-icon='inline-start'
                            />
                            打开上游登录
                          </Button>
                        ) : null}
                        {upstreamVersion ? (
                          <span className='text-muted-foreground text-sm'>
                            当前版本：{upstreamVersion}
                          </span>
                        ) : null}
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ) : null}

              {needsUserAuthentication ? (
                <div className='grid min-w-0 gap-4 sm:grid-cols-[8rem_minmax(0,1fr)]'>
                  <FormField
                    control={form.control}
                    name='userId'
                    render={({ field }) => (
                      <FormItem className='min-w-0'>
                        <FormLabel>上游用户 ID</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            step={1}
                            value={field.value}
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
                    control={form.control}
                    name='accessToken'
                    render={({ field }) => (
                      <FormItem className='min-w-0'>
                        <FormLabel>管理面板访问令牌</FormLabel>
                        <FormControl>
                          <PasswordInput
                            className='w-full min-w-0'
                            placeholder={
                              hasMatchingSavedAccessToken
                                ? '留空保留原访问令牌'
                                : '输入管理面板访问令牌'
                            }
                            autoComplete='new-password'
                            {...field}
                            onChange={(event) => {
                              field.onChange(event)
                              setTestResult(null)
                              setTestedAuthType(null)
                            }}
                          />
                        </FormControl>
                        <FormDescription>
                          不是 sk- 开头的渠道 API 密钥
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              ) : null}

              {needsSub2APIAccount ? (
                <div className='grid min-w-0 gap-4 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='account'
                    render={({ field }) => (
                      <FormItem className='min-w-0'>
                        <FormLabel>Sub2API 登录邮箱</FormLabel>
                        <FormControl>
                          <Input
                            type='email'
                            autoComplete='username'
                            placeholder='name@example.com'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='password'
                    render={({ field }) => (
                      <FormItem className='min-w-0'>
                        <FormLabel>Sub2API 登录密码</FormLabel>
                        <FormControl>
                          <PasswordInput
                            className='w-full min-w-0'
                            placeholder={
                              hasMatchingSavedPassword
                                ? '留空保留原登录密码'
                                : '输入 Sub2API 登录密码'
                            }
                            autoComplete='new-password'
                            {...field}
                            onChange={(event) => {
                              field.onChange(event)
                              setTestResult(null)
                              setTestedAuthType(null)
                            }}
                          />
                        </FormControl>
                        <FormDescription>
                          Token
                          缺失或过期时会自动登录；密码作为敏感配置保存在服务端，接口不会回传明文。上游开启
                          Turnstile、Cloudflare 人机验证或 TOTP
                          时无法无人值守登录，请改用 Refresh Token 或手动 Token
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              ) : null}

              {usesSub2APITokenCredential ? (
                <div className='grid min-w-0 gap-4 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='accessToken'
                    render={({ field }) => (
                      <FormItem className='min-w-0'>
                        <FormLabel>Sub2API 手动 Token</FormLabel>
                        <FormControl>
                          <PasswordInput
                            className='w-full min-w-0'
                            placeholder={
                              hasSavedSub2APIToken
                                ? '留空使用已保存 Token'
                                : '输入登录后的 JWT Token'
                            }
                            autoComplete='new-password'
                            {...field}
                            onChange={(event) => {
                              field.onChange(event)
                              setTestResult(null)
                              setTestedAuthType(null)
                            }}
                          />
                        </FormControl>
                        <div className='flex flex-wrap gap-2'>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() =>
                              void handlePasteToken('accessToken', '手动 Token')
                            }
                            disabled={pending}
                          >
                            <HugeiconsIcon
                              icon={ClipboardPasteIcon}
                              data-icon='inline-start'
                            />
                            粘贴
                          </Button>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() =>
                              void copyToClipboard(SUB2API_ACCESS_TOKEN_COMMAND)
                            }
                            disabled={pending}
                          >
                            <HugeiconsIcon
                              icon={Copy01Icon}
                              data-icon='inline-start'
                            />
                            提取命令
                          </Button>
                          <Button
                            type='button'
                            variant='secondary'
                            size='sm'
                            onClick={() =>
                              void handleTestSub2APICredential('token')
                            }
                            disabled={pending || !hasSub2APIToken}
                          >
                            {testMutation.isPending &&
                            testedAuthType === 'token' ? (
                              <Spinner data-icon='inline-start' />
                            ) : (
                              <HugeiconsIcon
                                icon={TestTubeIcon}
                                data-icon='inline-start'
                              />
                            )}
                            测试手动 Token
                          </Button>
                        </div>
                        <FormDescription>
                          短期 JWT；仅在选择“手动 Token”时保存并用于监控。
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='refreshToken'
                    render={({ field }) => (
                      <FormItem className='min-w-0'>
                        <FormLabel>Sub2API Refresh Token</FormLabel>
                        <FormControl>
                          <PasswordInput
                            className='w-full min-w-0'
                            placeholder={
                              hasSavedSub2APIRefreshToken
                                ? '留空使用已保存 Refresh Token'
                                : '输入 Sub2API Refresh Token'
                            }
                            autoComplete='new-password'
                            {...field}
                            onChange={(event) => {
                              field.onChange(event)
                              setTestResult(null)
                              setTestedAuthType(null)
                            }}
                          />
                        </FormControl>
                        <div className='flex flex-wrap gap-2'>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() =>
                              void handlePasteToken(
                                'refreshToken',
                                'Refresh Token'
                              )
                            }
                            disabled={pending}
                          >
                            <HugeiconsIcon
                              icon={ClipboardPasteIcon}
                              data-icon='inline-start'
                            />
                            粘贴
                          </Button>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() =>
                              void copyToClipboard(
                                SUB2API_REFRESH_TOKEN_COMMAND
                              )
                            }
                            disabled={pending}
                          >
                            <HugeiconsIcon
                              icon={Copy01Icon}
                              data-icon='inline-start'
                            />
                            提取命令
                          </Button>
                          <Button
                            type='button'
                            variant='secondary'
                            size='sm'
                            onClick={() =>
                              void handleTestSub2APICredential('refresh_token')
                            }
                            disabled={pending || !hasSub2APIRefreshToken}
                          >
                            {testMutation.isPending &&
                            testedAuthType === 'refresh_token' ? (
                              <Spinner data-icon='inline-start' />
                            ) : (
                              <HugeiconsIcon
                                icon={TestTubeIcon}
                                data-icon='inline-start'
                              />
                            )}
                            测试 Refresh Token
                          </Button>
                        </div>
                        <FormDescription>
                          长期凭据；测试会实际换取 Access
                          Token。仅在选择“Refresh Token”时保存并用于监控。
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              ) : null}

              {testResult && (
                <Alert>
                  <HugeiconsIcon icon={Tick02Icon} />
                  <AlertTitle>{testResultTitle}</AlertTitle>
                  <AlertDescription className='flex min-w-0 flex-col gap-2 text-left break-all'>
                    <span>
                      上游倍率 {formatMonitorRatio(testResult.ratio)} · 换算系数{' '}
                      {formatMonitorRatio(testResult.conversion_factor)} ·
                      成本倍率 {formatMonitorRatio(testResult.cost_ratio)} ·{' '}
                      {testResult.endpoint}
                    </span>
                    {isCustom && testResult.balance.amount != null ? (
                      <span>
                        上游余额 {formatMonitorRatio(testResult.balance.amount)}{' '}
                        · {testResult.balance.endpoint || '固定输入'}
                      </span>
                    ) : null}
                    {isCustom && testResult.balance.error ? (
                      <span className='text-destructive'>
                        余额获取失败：{testResult.balance.error}
                      </span>
                    ) : null}
                    {isCustom && testResult.debug ? (
                      <span>
                        HTTP {testResult.debug.status_code} ·{' '}
                        {testResult.debug.duration_ms} ms
                      </span>
                    ) : null}
                    {isCustom && testResult.debug?.response_preview ? (
                      <pre className='bg-muted max-h-32 overflow-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap'>
                        {testResult.debug.response_preview}
                      </pre>
                    ) : null}
                    {isCustom &&
                    testResult.balance.debug &&
                    testResult.balance.endpoint !== testResult.endpoint ? (
                      <>
                        <span>
                          余额接口 HTTP {testResult.balance.debug.status_code} ·{' '}
                          {testResult.balance.debug.duration_ms} ms
                        </span>
                        {testResult.balance.debug.response_preview ? (
                          <pre className='bg-muted max-h-32 overflow-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap'>
                            {testResult.balance.debug.response_preview}
                          </pre>
                        ) : null}
                      </>
                    ) : null}
                  </AlertDescription>
                </Alert>
              )}

              <div className='flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:justify-end'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => props.onOpenChange(false)}
                  disabled={pending}
                >
                  取消
                </Button>
                {(ratioSyncEnabled || isCustom) &&
                !usesSub2APITokenCredential ? (
                  <Button
                    type='button'
                    variant='secondary'
                    onClick={handleTest}
                    disabled={pending}
                  >
                    {testMutation.isPending ? (
                      <Spinner data-icon='inline-start' />
                    ) : (
                      <HugeiconsIcon
                        icon={TestTubeIcon}
                        data-icon='inline-start'
                      />
                    )}
                    测试获取
                  </Button>
                ) : null}
                <Button type='submit' disabled={pending}>
                  {saveMutation.isPending && (
                    <Spinner data-icon='inline-start' />
                  )}
                  保存
                </Button>
              </div>
            </form>
          </Form>
        </div>
      </DialogContent>
      {ratioEditorOpen ? (
        <EditChannelRatioDialog
          channel={props.channel}
          open={ratioEditorOpen}
          onOpenChange={setRatioEditorOpen}
        />
      ) : null}
    </Dialog>
  )
}
