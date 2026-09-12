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
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../custom-upstream'
import { createUpstreamConfigSchema } from '../schema'
import { customVariableFormValues } from './custom-variable.fixture'

describe('独立请求的表单和接口契约', () => {
  test('旧版单变量配置转为请求卡片时保留已有值和策略', () => {
    const modern = createChannelMonitorCustomRequestConfig(
      customVariableFormValues().customConfig
    )
    const request = modern.variable_requests?.[0]
    if (!request) throw new Error('测试配置缺少独立请求')
    const legacy = {
      ...modern,
      variable_requests: undefined,
      variable_request: {
        name: 'token',
        value: '',
        has_value: true,
        base_url: '',
        refresh_policy: 'on_failure' as const,
        request: request.request,
        result: {
          response_type: 'json' as const,
          value_path: 'data.token',
          multiplier: 1,
        },
      },
    }
    const form = createChannelMonitorCustomFormConfig(legacy)
    expect(form.variableRequests).toHaveLength(1)
    expect(form.variableRequests[0]).toMatchObject({
      id: 'legacy-variable',
      refreshPolicy: 'on_failure',
      variables: [
        { name: 'token', valuePath: 'data.token', value: '', hasValue: true },
      ],
    })
    expect(
      createChannelMonitorCustomRequestConfig(form).variable_request
    ).toBeUndefined()
  })

  test('不同请求重复定义变量名会在对应变量处阻止保存', () => {
    const form = customVariableFormValues()
    form.customConfig.variableRequests.push({
      ...form.customConfig.variableRequests[0],
      id: 'other',
      name: '其他请求',
    })
    const parsed = createUpstreamConfigSchema(null).safeParse(form)
    expect(parsed.success).toBe(false)
    if (parsed.success) throw new Error('重复变量名未被拒绝')
    expect(parsed.error.issues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          path: ['customConfig', 'variableRequests', 1, 'variables', 0, 'name'],
        }),
      ])
    )
  })
  test('手填初始值和变量模板可保存并重新编辑', () => {
    const form = customVariableFormValues()
    form.customConfig.variableRequests[0].variables[0].value = 'manual-token'
    expect(createUpstreamConfigSchema(null).safeParse(form).success).toBe(true)
    const request = createChannelMonitorCustomRequestConfig(form.customConfig)
    expect(request.variable_requests?.[0].variables[0].value).toBe(
      'manual-token'
    )
    expect(request.variable_requests?.[0].refresh_policy).toBe('on_failure')
    expect(request.ratio.request?.headers[0].value_template).toBe(
      'Bearer {{token}}'
    )
    expect(request.balance.request?.query[0].value_template).toBe('{{token}}')
    expect(createChannelMonitorCustomFormConfig(request)).toEqual(
      form.customConfig
    )
  })

  test('隐藏的已保存变量值可留空，模板仍保持可编辑', () => {
    const form = customVariableFormValues()
    form.customConfig.variableRequests[0].variables[0].hasValue = true
    const request = createChannelMonitorCustomRequestConfig(form.customConfig)
    const reopened = createChannelMonitorCustomFormConfig(request)
    expect(reopened.variableRequests[0].variables[0].value).toBe('')
    expect(reopened.variableRequests[0].variables[0].hasValue).toBe(true)
    expect(reopened.ratio.request.headers[0].valueTemplate).toBe(
      'Bearer {{token}}'
    )
    expect(
      createUpstreamConfigSchema(null).safeParse({
        ...form,
        customConfig: reopened,
      }).success
    ).toBe(true)
  })

  test.each(['{{unknown}}', '{{token}', 'literal'])(
    '无效变量模板 %s 阻止保存',
    (template) => {
      const form = customVariableFormValues()
      form.customConfig.ratio.request.headers[0].valueTemplate = template
      const result = createUpstreamConfigSchema(null).safeParse(form)
      expect(result.success).toBe(false)
      if (!result.success) {
        expect(result.error.issues).toEqual(
          expect.arrayContaining([
            expect.objectContaining({
              path: [
                'customConfig',
                'ratio',
                'request',
                'headers',
                0,
                'valueTemplate',
              ],
            }),
          ])
        )
      }
    }
  )

  test('关闭独立请求后仍引用变量会阻止保存', () => {
    const form = customVariableFormValues()
    form.customConfig.variableRequests = []
    expect(createUpstreamConfigSchema(null).safeParse(form).success).toBe(false)
  })

  test('没有初始值仍可保存，首次更新从接口获取', () => {
    expect(
      createUpstreamConfigSchema(null).safeParse(customVariableFormValues())
        .success
    ).toBe(true)
  })
})
