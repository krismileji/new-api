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
  ChannelMonitorCustomKeyValue,
  ChannelMonitorCustomMetricConfig,
  ChannelMonitorCustomRequestConfig,
  ChannelMonitorCustomUpstreamConfig,
  ChannelMonitorCustomVariableRequest,
} from '../types'
import type { UpstreamConfigFormValues } from './schema'

type CustomMetricFormValue = UpstreamConfigFormValues['customConfig']['ratio']
type CustomRequestFormValue = CustomMetricFormValue['request']

function createCustomRequestFormValue(
  request: ChannelMonitorCustomRequestConfig | undefined
): CustomRequestFormValue {
  return {
    method: request?.method ?? 'GET',
    path: request?.path ?? '/api/monitor',
    query: (request?.query ?? []).map(toFormKeyValue),
    headers: (request?.headers ?? []).map(toFormKeyValue),
    bodyType: request?.body_type ?? 'none',
    body: request?.body ?? '',
    bodySecret: request?.body_secret ?? false,
    hasBody: request?.has_body ?? false,
    form: (request?.form ?? []).map(toFormKeyValue),
  }
}

function createCustomMetricFormValue(
  metric: ChannelMonitorCustomMetricConfig | undefined,
  fixedValue: number,
  valuePath: string
): CustomMetricFormValue {
  return {
    source: metric?.source ?? 'fixed',
    accountId: metric?.account_id ?? 0,
    fixedValue: metric?.fixed_value ?? fixedValue,
    request: createCustomRequestFormValue(metric?.request),
    result: {
      responseType: metric?.result?.response_type ?? 'json',
      valuePath: metric?.result?.value_path ?? valuePath,
      multiplier: metric?.result?.multiplier ?? 1,
    },
  }
}

function toFormKeyValue(value: ChannelMonitorCustomKeyValue) {
  return {
    key: value.key,
    value: value.value ?? '',
    valueTemplate: value.value_template || undefined,
    secret: value.secret ?? false,
    hasValue: value.has_value ?? false,
  }
}

function toAPIKeyValue(
  value: CustomRequestFormValue['query'][number]
): ChannelMonitorCustomKeyValue {
  return {
    key: value.key.trim(),
    value: value.valueTemplate === undefined ? value.value : '',
    value_template: value.valueTemplate,
    secret: value.secret,
    has_value: value.hasValue,
  }
}

function toAPIRequest(
  request: CustomRequestFormValue
): ChannelMonitorCustomRequestConfig {
  return {
    method: request.method,
    path: request.path.trim(),
    query: request.query.map(toAPIKeyValue),
    headers: request.headers.map(toAPIKeyValue),
    body_type: request.bodyType,
    body: request.body,
    body_secret: request.bodySecret,
    has_body: request.hasBody,
    form: request.form.map(toAPIKeyValue),
  }
}

function toAPIMetric(
  metric: CustomMetricFormValue,
  omitRequest: boolean
): ChannelMonitorCustomMetricConfig {
  if (metric.source === 'account') {
    return { source: 'account', account_id: metric.accountId }
  }
  if (metric.source === 'fixed') {
    return { source: 'fixed', fixed_value: metric.fixedValue }
  }
  return {
    source: 'http',
    request: omitRequest ? undefined : toAPIRequest(metric.request),
    result: {
      response_type: metric.result.responseType,
      value_path: metric.result.valuePath.trim(),
      multiplier: metric.result.multiplier,
    },
  }
}

export function createChannelMonitorCustomFormConfig(
  config: ChannelMonitorCustomUpstreamConfig | undefined
): UpstreamConfigFormValues['customConfig'] {
  const legacy = config?.variable_request
  const requests =
    config?.variable_requests ??
    (legacy
      ? [
          {
            id: 'legacy-variable',
            name: `获取 ${legacy.name}`,
            base_url: legacy.base_url,
            refresh_policy: legacy.refresh_policy,
            request: legacy.request,
            response_type: legacy.result.response_type,
            variables: [
              {
                name: legacy.name,
                value: legacy.value,
                has_value: legacy.has_value,
                value_path: legacy.result.value_path,
              },
            ],
          },
        ]
      : [])
  return {
    version: 1,
    ratio: createCustomMetricFormValue(config?.ratio, 1, 'data.ratio'),
    balance: createCustomMetricFormValue(config?.balance, 0, 'data.balance'),
    balanceReuseRatioRequest: config?.balance_reuse_ratio_request ?? false,
    variableGroupId: config?.variable_group_id ?? 0,
    actions: (config?.actions ?? []).map((action) => ({
      triggerMode: action.trigger_mode,
      id: action.id,
      name: action.name,
      enabled: action.enabled,
      metric: action.metric,
      operator: action.operator,
      threshold: action.threshold,
      timezone: action.timezone,
      startTime: action.start_time,
      endTime: action.end_time,
      dailyLimit: action.daily_limit,
      cooldownMinutes: action.cooldown_minutes,
      baseUrl: action.base_url ?? '',
      request: createCustomRequestFormValue(action.request),
      successPath: action.success_path ?? '',
      successValue: action.success_value ?? '',
    })),
    variableRequests: requests.map((request) => ({
      id: request.id,
      name: request.name,
      baseUrl: request.base_url ?? '',
      refreshPolicy: request.refresh_policy,
      request: createCustomRequestFormValue(request.request),
      responseType: request.response_type,
      variables: request.variables.map((variable) => ({
        name: variable.name,
        value: variable.value ?? '',
        valuePath: variable.value_path ?? '',
        hasValue: variable.has_value ?? false,
      })),
    })),
  }
}

export function createChannelMonitorCustomRequestConfig(
  config: UpstreamConfigFormValues['customConfig']
): ChannelMonitorCustomUpstreamConfig {
  return {
    version: 1,
    ratio: toAPIMetric(config.ratio, false),
    balance: toAPIMetric(config.balance, config.balanceReuseRatioRequest),
    balance_reuse_ratio_request: config.balanceReuseRatioRequest,
    variable_group_id: config.variableGroupId || undefined,
    variable_requests: config.variableGroupId
      ? []
      : config.variableRequests.map(toAPIVariableRequest),
    actions: config.actions.map((action) => ({
      ...(action.triggerMode ? { trigger_mode: action.triggerMode } : {}),
      id: action.id,
      name: action.name.trim(),
      enabled: action.enabled,
      metric: action.metric,
      operator: action.operator,
      threshold: action.threshold,
      timezone: action.timezone.trim(),
      start_time: action.startTime,
      end_time: action.endTime,
      daily_limit: action.dailyLimit,
      cooldown_minutes: action.cooldownMinutes,
      base_url: action.baseUrl.trim(),
      request: toAPIRequest(action.request),
      success_path: action.successPath.trim(),
      success_value: action.successValue,
    })),
  }
}

function toAPIVariableRequest(
  request: UpstreamConfigFormValues['customConfig']['variableRequests'][number]
): ChannelMonitorCustomVariableRequest {
  return {
    id: request.id,
    name: request.name.trim(),
    base_url: request.baseUrl.trim(),
    refresh_policy: request.refreshPolicy,
    request: toAPIRequest(request.request),
    response_type: request.responseType,
    variables: request.variables.map((variable) => ({
      name: variable.name.trim(),
      value_path: variable.valuePath.trim(),
      value: variable.value,
      has_value: variable.hasValue,
    })),
  }
}

export function createChannelMonitorVariableRequest(
  id: string,
  name: string
): UpstreamConfigFormValues['customConfig']['variableRequests'][number] {
  return {
    id,
    name,
    baseUrl: '',
    refreshPolicy: 'on_failure',
    request: createCustomRequestFormValue(undefined),
    responseType: 'json',
    variables: [{ name: '', value: '', hasValue: false, valuePath: '' }],
  }
}

export function createChannelMonitorCustomAction(): UpstreamConfigFormValues['customConfig']['actions'][number] {
  return {
    id: crypto.randomUUID(),
    name: '余额不足时重置',
    enabled: false,
    metric: 'balance',
    operator: 'lt',
    threshold: 1,
    timezone: 'Asia/Shanghai',
    startTime: '00:05',
    endTime: '23:00',
    dailyLimit: 1,
    cooldownMinutes: 60,
    baseUrl: '',
    request: {
      ...createCustomRequestFormValue(undefined),
      method: 'POST',
      path: '/api/reset',
    },
    successPath: '',
    successValue: '',
  }
}
