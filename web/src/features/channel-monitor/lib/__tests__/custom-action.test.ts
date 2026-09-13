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
import { describe, expect, test } from 'vitest'

import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../custom-upstream'
import { createUpstreamConfigSchema } from '../schema'
import { customVariableFormValues } from './custom-variable.fixture'

describe('条件触发规则配置', () => {
  test('旧配置无规则，新规则默认关闭且限制北京时间 23 点前每日一次', () => {
    expect(createChannelMonitorCustomFormConfig(undefined).actions).toEqual([])
    expect(createChannelMonitorCustomAction()).toMatchObject({
      enabled: false,
      metric: 'balance',
      operator: 'lt',
      timezone: 'Asia/Shanghai',
      startTime: '00:05',
      endTime: '23:00',
      dailyLimit: 1,
    })
  })

  test('规则请求及已保存敏感参数在编辑往返时保留', () => {
    const form = customVariableFormValues()
    const action = createChannelMonitorCustomAction()
    action.enabled = true
    action.threshold = 0
    action.request.headers = [
      {
        key: 'Authorization',
        value: '',
        hasValue: true,
        secret: true,
        valueTemplate: 'Bearer {{token}}',
      },
    ]
    action.request.bodyType = 'json'
    action.request.bodySecret = true
    action.request.hasBody = true
    action.successPath = 'success'
    action.successValue = 'true'
    form.customConfig.actions = [action]
    expect(createUpstreamConfigSchema(null).safeParse(form).success).toBe(true)
    const request = createChannelMonitorCustomRequestConfig(form.customConfig)
    expect(request.actions?.[0]).toMatchObject({
      threshold: 0,
      end_time: '23:00',
      success_path: 'success',
      success_value: 'true',
      request: { body_secret: true, has_body: true },
    })
    expect(createChannelMonitorCustomFormConfig(request)).toEqual(
      form.customConfig
    )
  })

  test.each([
    ['endTime', '00:05', 'endTime'],
    ['timezone', 'Local', 'timezone'],
    ['threshold', '', 'threshold'],
    ['dailyLimit', 0, 'dailyLimit'],
    ['baseUrl', 'https://user:password@other.example', 'baseUrl'],
  ])('无效 %s 阻止保存并显示字段错误', (key, value, errorKey) => {
    const form = customVariableFormValues()
    form.customConfig.actions = [
      Object.assign(createChannelMonitorCustomAction(), { [key]: value }),
    ]
    const result = createUpstreamConfigSchema(null).safeParse(form)
    expect(result.success).toBe(false)
    if (result.success) throw new Error('无效配置未被拒绝')
    expect(result.error.issues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          path: ['customConfig', 'actions', 0, errorKey],
        }),
      ])
    )
  })

  test('触发接口引用已删除变量时定位到规则中的参数', () => {
    const form = customVariableFormValues()
    const action = createChannelMonitorCustomAction()
    action.request.headers = [
      {
        key: 'Authorization',
        value: '',
        secret: true,
        hasValue: false,
        valueTemplate: 'Bearer {{removed}}',
      },
    ]
    form.customConfig.actions = [action]
    const parsed = createUpstreamConfigSchema(null).safeParse(form)
    expect(parsed.success).toBe(false)
    if (parsed.success) throw new Error('未配置的变量未被拒绝')
    expect(parsed.error.issues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          path: [
            'customConfig',
            'actions',
            0,
            'request',
            'headers',
            0,
            'valueTemplate',
          ],
        }),
      ])
    )
  })
})
