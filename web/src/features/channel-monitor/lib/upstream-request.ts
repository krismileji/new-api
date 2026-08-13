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
import type {
  ChannelMonitorCostConversion,
  ChannelMonitorUpstreamAuthType,
  ChannelMonitorUpstreamRequest,
} from '../types'
import { createChannelMonitorCustomRequestConfig } from './custom-upstream'
import type { UpstreamConfigFormValues } from './schema'

export function createChannelMonitorUpstreamRequest(
  values: UpstreamConfigFormValues,
  authType: ChannelMonitorUpstreamAuthType = values.authType,
  includeEmptyRefreshToken = false
): ChannelMonitorUpstreamRequest {
  const userAuthentication =
    values.upstreamType === 'new_api' && authType === 'user'
  const sub2APITokenAuthentication =
    values.upstreamType === 'sub2api' && authType === 'token'
  const sub2APIRefreshTokenAuthentication =
    values.upstreamType === 'sub2api' && authType === 'refresh_token'
  const sub2APIAccountAuthentication =
    values.upstreamType === 'sub2api' && authType === 'account'
  let costConversion: ChannelMonitorCostConversion = { mode: 'none' }
  if (values.costConversionMode === 'recharge') {
    costConversion = {
      mode: 'recharge',
      paid_cny: values.rechargePaidCny,
      credited_usd: values.rechargeCreditedUsd,
    }
  } else if (values.costConversionMode === 'subscription') {
    costConversion = {
      mode: 'subscription',
      subscription_period: values.subscriptionPeriod,
      subscription_price_cny: values.subscriptionPriceCny,
      subscription_daily_usd: values.subscriptionDailyUsd,
    }
  }

  let accessToken = ''
  let refreshToken: string | undefined
  if (userAuthentication || sub2APITokenAuthentication) {
    accessToken = values.accessToken.trim()
  } else if (sub2APIRefreshTokenAuthentication) {
    accessToken = values.refreshToken.trim()
  }
  if (values.upstreamType === 'sub2api' && authType === 'token') {
    const value = values.refreshToken.trim()
    if (value || includeEmptyRefreshToken) {
      refreshToken = value
    }
  }

  return {
    type: values.upstreamType,
    base_url: values.baseUrl.trim(),
    group: values.group.trim(),
    auth_type: authType,
    user_id: userAuthentication ? values.userId : 0,
    access_token: accessToken,
    refresh_token: refreshToken,
    account: sub2APIAccountAuthentication ? values.account.trim() : '',
    password: sub2APIAccountAuthentication ? values.password : '',
    single_channel_action: values.singleChannelAction,
    multiple_channels_action: values.multipleChannelsAction,
    balance_warning_threshold: values.balanceWarningThreshold,
    balance_auto_disable_threshold: values.balanceAutoDisableThreshold,
    ratio_sync_enabled: values.ratioSyncEnabled,
    balance_sync_enabled: values.balanceSyncEnabled,
    cost_conversion: costConversion,
    custom_config:
      values.upstreamType === 'custom'
        ? createChannelMonitorCustomRequestConfig(values.customConfig)
        : undefined,
  }
}
