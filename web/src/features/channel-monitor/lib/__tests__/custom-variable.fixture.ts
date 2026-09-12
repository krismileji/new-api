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
import type { ChannelMonitorItem } from '../../types'
import {
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
  createChannelMonitorVariableRequest,
} from '../custom-upstream'
import type { UpstreamConfigFormValues } from '../schema'

export function customVariableFormValues(): UpstreamConfigFormValues {
  const customConfig = createChannelMonitorCustomFormConfig(undefined)
  const loginRequest = createChannelMonitorVariableRequest(
    'login',
    '登录获取凭据'
  )
  loginRequest.request.path = '/auth/token'
  loginRequest.variables = [
    { name: 'token', valuePath: 'data.token', value: '', hasValue: false },
  ]
  customConfig.variableRequests = [loginRequest]
  customConfig.ratio.source = 'http'
  customConfig.ratio.request.headers = [
    {
      key: 'Authorization',
      value: '',
      valueTemplate: 'Bearer {{token}}',
      secret: true,
      hasValue: false,
    },
  ]
  customConfig.balance.source = 'http'
  customConfig.balance.request.query = [
    {
      key: 'access_token',
      value: '',
      valueTemplate: '{{token}}',
      secret: true,
      hasValue: false,
    },
  ]
  return {
    upstreamType: 'custom',
    baseUrl: 'https://upstream.example',
    group: '',
    authType: 'custom',
    userId: 0,
    accessToken: '',
    refreshToken: '',
    account: '',
    password: '',
    singleChannelAction: 'none',
    multipleChannelsAction: 'none',
    ratioSyncEnabled: true,
    balanceSyncEnabled: true,
    balanceWarningThreshold: null,
    balanceAutoDisableThreshold: null,
    costConversionMode: 'none',
    rechargePaidCny: 1,
    rechargeCreditedUsd: 1,
    subscriptionPeriod: 'month',
    subscriptionPriceCny: 1,
    subscriptionDailyUsd: 1,
    customConfig,
  }
}

export function customVariableChannel(): ChannelMonitorItem {
  return {
    id: 7,
    name: '测试渠道',
    type: 1,
    status: 1,
    status_reason: '',
    priority: 0,
    weight: 0,
    base_url: 'https://upstream.example',
    models: 'test-model',
    test_model: 'test-model',
    groups: ['default'],
    ratio: 1,
    previous_ratio: 1,
    cost_ratio: 1,
    previous_cost_ratio: 1,
    conversion_factor: 1,
    remark: '',
    channel_remark: '',
    updated_time: 0,
    updated_by: 0,
    updated_by_username: '',
    last_fetch_status: '',
    last_fetch_error: '',
    last_fetch_time: 0,
    consecutive_failures: 0,
    upstream_balance: null,
    last_balance_time: 0,
    last_balance_error: '',
    today_cost_cny: 0,
    today_cost_configured: false,
    today_cost_complete: true,
    today_cost_unresolved_count: 0,
    concurrency_limit: 0,
    concurrency_active: 0,
    current_rpm: 0,
    upstream: {
      type: 'custom',
      base_url: 'https://upstream.example',
      group: '',
      auth_type: 'custom',
      user_id: 0,
      has_access_token: false,
      has_refresh_token: false,
      account: '',
      has_password: false,
      single_channel_action: 'none',
      multiple_channels_action: 'none',
      balance_warning_threshold: null,
      balance_auto_disable_threshold: null,
      ratio_sync_enabled: true,
      balance_sync_enabled: true,
      cost_conversion: { mode: 'none' },
      custom_config: createChannelMonitorCustomRequestConfig(
        customVariableFormValues().customConfig
      ),
    },
  }
}
