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
/* eslint-disable react/only-export-components */
import assert from 'node:assert/strict'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'CustomEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'PointerEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { useForm } = await import('react-hook-form')
const { Form, FormField, FormItem } = await import('@/components/ui/form')
const { ChannelMonitorSettingLabel } =
  await import('../channel-monitor-setting-label')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type SettingHelpKey = Parameters<
  typeof ChannelMonitorSettingLabel
>[0]['helpKey']

type SettingHelpScenario = {
  label: string
  helpKey: SettingHelpKey
  expectedPhrases: string[]
  expectedMetadata: string[]
}

const defaultScenario: SettingHelpScenario = {
  label: '首字告警请求占比',
  helpKey: 'adaptiveSamplingFirstTokenWarningRequestPercent',
  expectedPhrases: ['成功且首字达到告警秒数', '失败请求由错误阈值独立判断'],
  expectedMetadata: [
    '单位：%',
    '范围：>0–100',
    '默认值：10',
    '生效时机：',
    '更新方式：',
    '组合约束：',
  ],
}

const scenarios: Record<string, SettingHelpScenario> = {
  'first-token-warning': defaultScenario,
  'sampling-order': {
    label: '统一采样顺序',
    helpKey: 'samplingOrder',
    expectedPhrases: [
      '探索流量选择样本欠账候选',
      '自适应备援沿用同一顺序',
      '仅在探索流量配置中展示',
      '关闭常规探索后仍保留',
    ],
    expectedMetadata: [
      '单位：枚举',
      '范围：按基础优先级和权重、按成本倍率',
      '默认值：按基础优先级和权重',
      '生效时机：',
      '更新方式：',
      '组合约束：',
    ],
  },
  'exploration-prompt-k-tokens': {
    label: '探索请求上限',
    helpKey: 'explorationMaxPromptKTokens',
    expectedPhrases: ['1K 等于 1000 Token', '0 表示无限制'],
    expectedMetadata: [
      '单位：K Token',
      '范围：0–1000 的整数',
      '默认值：50',
      '生效时机：',
      '更新方式：',
      '组合约束：',
    ],
  },
}

const scenario = scenarios[process.argv[2] ?? ''] ?? defaultScenario

function SettingHelpFixture() {
  const form = useForm<{ value: string }>({
    defaultValues: { value: '' },
  })

  return (
    <Form {...form}>
      <FormField
        control={form.control}
        name='value'
        render={() => (
          <FormItem>
            <ChannelMonitorSettingLabel
              label={scenario.label}
              helpKey={scenario.helpKey}
            />
          </FormItem>
        )}
      />
    </Form>
  )
}

function waitForTooltip(): Promise<HTMLElement> {
  const current = document.body.querySelector<HTMLElement>(
    '[data-slot="tooltip-content"]'
  )
  if (current) return Promise.resolve(current)

  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      observer.disconnect()
      reject(new Error('说明 Tooltip 未在预期时间内显示'))
    }, 2000)
    const observer = new MutationObserver(() => {
      const tooltip = document.body.querySelector<HTMLElement>(
        '[data-slot="tooltip-content"]'
      )
      if (!tooltip) return
      clearTimeout(timeout)
      observer.disconnect()
      resolve(tooltip)
    })
    observer.observe(document.body, { childList: true, subtree: true })
  })
}

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)

await act(async () => root.render(<SettingHelpFixture />))

const trigger = container.querySelector<HTMLButtonElement>(
  `button[aria-label="查看“${scenario.label}”说明"]`
)
assert.ok(trigger)
const triggerType = trigger.type

await act(async () => trigger.focus())
const tooltip = await waitForTooltip()
const tooltipText = tooltip.textContent ?? ''
const focusShowsExplanation = scenario.expectedPhrases.every((item) =>
  tooltipText.includes(item)
)
const coversRequiredMetadata = scenario.expectedMetadata.every((item) =>
  tooltipText.includes(item)
)

await act(async () => root.unmount())
container.remove()

process.stdout.write(
  `${JSON.stringify({
    focusShowsExplanation,
    coversRequiredMetadata,
    triggerAriaLabel: trigger.getAttribute('aria-label'),
    triggerType,
  })}\n`
)

domWindow.close()
