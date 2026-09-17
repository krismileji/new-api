import { describe, expect, it } from 'vitest'

import {
  probePolicySchema,
  probePolicyThresholdTokens,
} from '../probe-policy-schema'

describe('输入阈值', () => {
  it('以整数 token 精确换算三位小数的 k 值', () => {
    expect(probePolicyThresholdTokens('1.001')).toBe(1001)
    expect(probePolicyThresholdTokens('0.001')).toBe(1)
    expect(probePolicyThresholdTokens('1000')).toBe(1_000_000)
  })
  it('拒绝负值、非整数 token 和超限值', () => {
    for (const value of [
      '0',
      '-1',
      '1.0001',
      '1000.001',
      'NaN',
      'Infinity',
      '1e3',
      '',
    ]) {
      expect(probePolicyThresholdTokens(value)).toBeNull()
    }
  })
  it('开启小输入响应时要求内容，关闭时保留草稿', () => {
    const values = {
      autoProbeDisabled: true,
      smallInputResponseEnabled: true,
      thresholdK: '1',
      responseText: ' ',
    }
    expect(probePolicySchema.safeParse(values).success).toBe(false)
    expect(
      probePolicySchema.safeParse({
        ...values,
        smallInputResponseEnabled: false,
      }).success
    ).toBe(true)
  })
})
