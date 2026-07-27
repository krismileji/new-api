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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { renderToStaticMarkup } from 'react-dom/server'
import { useForm } from 'react-hook-form'

import { Form } from '@/components/ui/form'

import type { ChannelMonitorSettingsFormValues } from '../../lib/schema'
import { ChannelMonitorProbeResponseFields } from '../channel-monitor-probe-response-fields'
import {
  ChannelMonitorConsecutiveFailureLimitField,
  ChannelMonitorCostRetentionField,
} from '../channel-monitor-settings-dialog'
import { ChannelMonitorSmartScheduleFields } from '../channel-monitor-smart-schedule-fields'

function CostRetentionFieldFixture() {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: { costRetentionDays: 120 },
  })
  return (
    <Form {...form}>
      <ChannelMonitorCostRetentionField form={form} />
    </Form>
  )
}

function ConsecutiveFailureLimitFieldFixture() {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: { autoUpdateConsecutiveFailureLimit: 3 },
  })
  return (
    <Form {...form}>
      <ChannelMonitorConsecutiveFailureLimitField form={form} />
    </Form>
  )
}

function ProbeResponseFieldsFixture() {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: { probeResponseEnabled: true },
  })
  return (
    <Form {...form}>
      <ChannelMonitorProbeResponseFields form={form} />
    </Form>
  )
}

type SmartScheduleFieldsFixtureProps = {
  strategy?: 'smart' | 'ratio'
  stabilityEnabled?: boolean
  relativeWeightEnabled?: boolean
  ratioPercentages?: {
    costRatioPercent: number | string
    firstTokenPercent: number | string
    tpsPercent: number | string
  }
}

function SmartScheduleFieldsFixture(props: SmartScheduleFieldsFixtureProps) {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: {
      relayResponseHeaderTimeoutSeconds: 60,
      smartScheduleEnabled: false,
      smartScheduleIntervalMinutes: 10,
      smartScheduleStrategy: props.strategy ?? 'smart',
      smartScheduleStabilityEnabled: props.stabilityEnabled ?? false,
      smartScheduleScoring: {
        stabilityPercent: 50,
        curveExponent: 1,
        relativeWeightEnabled: props.relativeWeightEnabled ?? true,
        relativeWeightStartPercent: 3,
        relativeWeightFullPercent: 10,
        smart: {
          costRatioPercent: 40,
          firstTokenPercent: 40,
          tpsPercent: 20,
        },
        ratio: props.ratioPercentages ?? {
          costRatioPercent: 70,
          firstTokenPercent: 20,
          tpsPercent: 10,
        },
      },
      smartScheduleApplyMode: 'weight',
      smartSchedulePerformanceMinutes: 60,
      smartScheduleModels: [],
      smartScheduleMinSamples: 10,
      smartScheduleMinSuccessRate: 80,
      smartScheduleCooldownMinutes: 30,
      smartScheduleForceReset: false,
    } as unknown as ChannelMonitorSettingsFormValues,
  })
  return (
    <Form {...form}>
      <ChannelMonitorSmartScheduleFields form={form} modelOptions={[]} />
    </Form>
  )
}

describe('channel monitor settings dialog', () => {
  test('shows the configured consecutive failure stop limit', () => {
    const markup = renderToStaticMarkup(<ConsecutiveFailureLimitFieldFixture />)

    assert.ok(markup.includes('连续失败停止次数'))
    assert.match(markup, /type="number"[^>]*min="1"[^>]*max="100"/)
    assert.match(markup, /value="3"/)
    assert.ok(markup.includes('倍率和余额分别连续失败'))
  })

  test('shows persisted cost retention days with bounded numeric input', () => {
    const markup = renderToStaticMarkup(<CostRetentionFieldFixture />)

    assert.ok(markup.includes('成本数据保留天数'))
    assert.match(markup, /type="number"[^>]*min="1"[^>]*max="3650"/)
    assert.match(markup, /value="120"/)
    assert.ok(markup.includes('删除后不可恢复'))
  })

  test('shows the enabled local probe response contract', () => {
    const markup = renderToStaticMarkup(<ProbeResponseFieldsFixture />)

    assert.ok(markup.includes('启用本地探针响应'))
    assert.ok(markup.includes('aria-label="启用本地探针响应"'))
    assert.ok(markup.includes('data-checked'))
    assert.ok(markup.includes('Hi. What are you working on?'))
    assert.ok(markup.includes('0.5-2 秒'))
    assert.ok(markup.includes('/v1/responses'))
    assert.ok(markup.includes('/v1/chat/completions'))
    assert.ok(markup.includes('渠道连通性测试不经过此拦截'))
  })

  test('shows the bounded upstream response wait setting in smart scheduling', () => {
    const markup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)

    assert.ok(markup.includes('上游响应等待时间'))
    assert.match(markup, /type="number"[^>]*min="0"[^>]*max="600"/)
    assert.match(markup, /value="60"/)
    assert.ok(markup.includes('0 表示不限制'))
    assert.ok(markup.includes('收到响应头后停止计时'))
    assert.ok(markup.includes('不限制后续流式输出'))
  })

  test('shows configurable scoring percentages with stability defaulting to half', () => {
    const markup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)
    const ratioMarkup = renderToStaticMarkup(
      <SmartScheduleFieldsFixture strategy='ratio' />
    )

    assert.ok(markup.includes('智能调度指标占比'))
    assert.ok(markup.includes('当前合计：100%'))
    assert.ok(markup.includes('启用后占最终得分的 50%'))
    assert.ok(markup.includes('得分曲线指数'))
    assert.ok(markup.includes('大于 1 会进一步压低中低分渠道'))
    assert.match(
      markup,
      /id="channel-monitor-smartScheduleScoring-smart-firstTokenPercent"[^>]*value="40"/
    )
    assert.match(
      markup,
      /id="channel-monitor-smartScheduleScoring-smart-tpsPercent"[^>]*value="20"/
    )
    assert.match(
      ratioMarkup,
      /id="channel-monitor-smartScheduleScoring-ratio-firstTokenPercent"[^>]*value="20"/
    )
    assert.match(
      ratioMarkup,
      /id="channel-monitor-smartScheduleScoring-ratio-tpsPercent"[^>]*value="10"/
    )
  })

  test('shows configurable relative weight thresholds only when enabled', () => {
    const enabledMarkup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)
    const disabledMarkup = renderToStaticMarkup(
      <SmartScheduleFieldsFixture relativeWeightEnabled={false} />
    )

    assert.ok(enabledMarkup.includes('相对权重拉伸'))
    assert.ok(enabledMarkup.includes('aria-label="相对权重拉伸"'))
    assert.ok(enabledMarkup.includes('开始拉伸分差'))
    assert.ok(enabledMarkup.includes('完整拉伸分差'))
    assert.match(
      enabledMarkup,
      /id="channel-monitor-smartScheduleScoring-relativeWeightStartPercent"[^>]*value="3"/
    )
    assert.match(
      enabledMarkup,
      /id="channel-monitor-smartScheduleScoring-relativeWeightFullPercent"[^>]*value="10"/
    )
    assert.ok(enabledMarkup.includes('任何分差都会开始拉伸'))
    assert.equal(disabledMarkup.includes('开始拉伸分差'), false)
    assert.equal(disabledMarkup.includes('完整拉伸分差'), false)
  })

  test('describes low-flow probing before restoring the full saved weight', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleFieldsFixture stabilityEnabled />
    )

    assert.ok(markup.includes('以不超过 10 的小流量权重用新样本试放'))
    assert.ok(markup.includes('达标后再恢复完整权重'))
  })

  test('adds temporary string percentages numerically in the displayed total', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleFieldsFixture
        strategy='ratio'
        ratioPercentages={{
          costRatioPercent: '70',
          firstTokenPercent: '15',
          tpsPercent: '25',
        }}
      />
    )

    assert.ok(markup.includes('当前合计：110%'))
  })
})
