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
import type { ChannelMonitorVariableGroup } from '../api-variable-groups'
import {
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from './custom-upstream'
import type { UpstreamConfigFormValues } from './schema'

// Reuse the request editor and its validation; metric/policy fields are inert
// defaults and are never included in the shared configuration API payload.
export function variableGroupFormValues(
  group: ChannelMonitorVariableGroup
): UpstreamConfigFormValues {
  return {
    upstreamType: 'custom',
    baseUrl: group.base_url,
    group: '',
    authType: 'custom',
    userId: 0,
    accessToken: '',
    refreshToken: '',
    account: '',
    password: '',
    singleChannelAction: 'none',
    multipleChannelsAction: 'none',
    ratioSyncEnabled: false,
    balanceSyncEnabled: false,
    balanceWarningThreshold: null,
    balanceAutoDisableThreshold: null,
    costConversionMode: 'none',
    rechargePaidCny: 1,
    rechargeCreditedUsd: 1,
    subscriptionPeriod: 'month',
    subscriptionPriceCny: 1,
    subscriptionDailyUsd: 1,
    customConfig: createChannelMonitorCustomFormConfig({
      version: 1,
      ratio: { source: 'fixed', fixed_value: 1 },
      balance: { source: 'fixed', fixed_value: 0 },
      balance_reuse_ratio_request: false,
      variable_requests: group.variable_requests,
    }),
  }
}

export function variableGroupPayload(
  group: ChannelMonitorVariableGroup,
  values: UpstreamConfigFormValues
): ChannelMonitorVariableGroup {
  return {
    ...group,
    name: group.name.trim(),
    base_url: values.baseUrl.trim(),
    variable_requests:
      createChannelMonitorCustomRequestConfig(values.customConfig)
        .variable_requests ?? [],
  }
}

export function emptyVariableGroup(): ChannelMonitorVariableGroup {
  return {
    id: 0,
    name: '',
    base_url: '',
    proxy: '',
    request_timeout: 30,
    revision: 0,
    variable_requests: [],
  }
}
