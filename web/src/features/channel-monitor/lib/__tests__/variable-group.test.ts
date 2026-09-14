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

describe('共享请求与变量的引用契约', () => {
  test('渠道保存共享引用时不复制请求或变量值，重新打开保留引用', () => {
    const values = customVariableFormValues()
    values.customConfig.variableGroupId = 12
    const request = createChannelMonitorCustomRequestConfig(values.customConfig)
    expect(request.variable_group_id).toBe(12)
    expect(request.variable_requests).toEqual([])
    const reopened = createChannelMonitorCustomFormConfig(request)
    expect(reopened.variableGroupId).toBe(12)
    expect(reopened.variableRequests).toEqual([])
  })

  test('引用共享变量可保存合法模板，取消引用后缺失变量会阻止保存', () => {
    const values = customVariableFormValues()
    values.customConfig.variableGroupId = 12
    values.customConfig.variableRequests = []
    const schema = createUpstreamConfigSchema(null)
    expect(schema.safeParse(values).success).toBe(true)
    values.customConfig.variableGroupId = 0
    expect(schema.safeParse(values).success).toBe(false)
  })

  test('使用共享配置时仍拒绝格式错误的变量模板', () => {
    const values = customVariableFormValues()
    values.customConfig.variableGroupId = 12
    values.customConfig.variableRequests = []
    values.customConfig.ratio.request.headers[0].valueTemplate = '{{token}'
    expect(createUpstreamConfigSchema(null).safeParse(values).success).toBe(
      false
    )
  })
})
