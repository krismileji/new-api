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

import type {
  ChannelMonitorSettingsFormValues,
  ChannelMonitorSmartSchedulePolicyFormValues,
} from '../../lib/schema'
import { ChannelMonitorProbeResponseFields } from '../channel-monitor-probe-response-fields'
import {
  ChannelMonitorConsecutiveFailureLimitField,
  ChannelMonitorCostRetentionField,
  ChannelMonitorUpstreamRequestTimeoutField,
} from '../channel-monitor-settings-dialog'
import { ChannelMonitorSmartScheduleFields } from '../channel-monitor-smart-schedule-fields'
import { ChannelMonitorSmartScheduleGroupPolicies } from '../channel-monitor-smart-schedule-group-policies'
import { ChannelMonitorSmartScheduleGroupPolicyFields } from '../channel-monitor-smart-schedule-group-policy-fields'

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

function UpstreamRequestTimeoutFieldFixture() {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: { upstreamRequestTimeoutSeconds: 30 },
  })
  return (
    <Form {...form}>
      <ChannelMonitorUpstreamRequestTimeoutField form={form} />
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

function SmartScheduleFieldsFixture() {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: {
      relayResponseHeaderTimeoutSeconds: 60,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartScheduleIntervalMinutes: 10,
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleStabilityWindowMinutes: 120,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    } as unknown as ChannelMonitorSettingsFormValues,
  })
  return (
    <Form {...form}>
      <ChannelMonitorSmartScheduleFields
        form={form}
        modelOptionsByGroup={new Map()}
        groupOptions={['default', 'vip']}
      />
    </Form>
  )
}

function SmartScheduleGroupPoliciesFixture(props: {
  configured: boolean
  sampleMode?: ChannelMonitorSmartSchedulePolicyFormValues['sampleMode']
}) {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: {
      smartScheduleGroupPolicies: props.configured
        ? [
            {
              group: 'vip',
              strategy: 'ratio',
              stabilityEnabled: false,
              jitterEnabled: true,
              jitterTolerancePercent: 5,
              jitterThresholdMultiplier: 5,
              jitterAbsoluteToleranceSeconds: 10,
              jitterBaselineMinutes: 60,
              scoring: {
                stabilityPercent: 50,
                primaryTrafficPercent: 90,
                primarySwitchThresholdPercent: 3,
                smart: {
                  costRatioPercent: 40,
                  firstTokenPercent: 40,
                  tpsPercent: 20,
                },
                ratio: {
                  costRatioPercent: 70,
                  firstTokenPercent: 20,
                  tpsPercent: 10,
                },
              },
              applyMode: 'priority_weight',
              models: [],
              minSamples: 5,
              degradeStabilityScore: 90,
              recoveryStabilityScore: 95,
              fastFailurePenaltyPercent: 40,
              fastFailureSeconds: 1,
              slowFailureSeconds: 10,
              cooldownMinutes: 30,
              sampleMode: props.sampleMode ?? 'traffic',
              explorationTrafficPercent: 3,
              probeIntervalMinutes: 10,
              prioritySamplingEnabled: true,
              prioritySamplingIntervalMinutes: 10,
              prioritySamplingBasePercent: 3,
              prioritySamplingDecayPercent: 70,
              prioritySamplingMinPercent: 0.5,
            },
          ]
        : [],
    } as unknown as ChannelMonitorSettingsFormValues,
  })
  return (
    <Form {...form}>
      <ChannelMonitorSmartScheduleGroupPolicies
        form={form}
        groupOptions={['default', 'vip']}
        modelOptionsByGroup={
          new Map([
            ['default', ['model-a']],
            ['vip', ['model-b']],
          ])
        }
      />
    </Form>
  )
}

function SmartScheduleGroupPolicyFieldsFixture(props: {
  applyMode: ChannelMonitorSmartSchedulePolicyFormValues['applyMode']
  sampleMode: ChannelMonitorSmartSchedulePolicyFormValues['sampleMode']
  jitterEnabled?: boolean
  prioritySamplingEnabled?: boolean
}) {
  const form = useForm<ChannelMonitorSmartSchedulePolicyFormValues>({
    defaultValues: {
      strategy: 'smart',
      stabilityEnabled: true,
      jitterEnabled: props.jitterEnabled ?? true,
      jitterTolerancePercent: 5,
      jitterThresholdMultiplier: 5,
      jitterAbsoluteToleranceSeconds: 10,
      jitterBaselineMinutes: 60,
      scoring: {
        stabilityPercent: 50,
        primaryTrafficPercent: 90,
        primarySwitchThresholdPercent: 3,
        smart: {
          costRatioPercent: 40,
          firstTokenPercent: 40,
          tpsPercent: 20,
        },
        ratio: {
          costRatioPercent: 70,
          firstTokenPercent: 20,
          tpsPercent: 10,
        },
      },
      applyMode: props.applyMode,
      models: [],
      modelOrder: [],
      minSamples: 5,
      degradeStabilityScore: 90,
      recoveryStabilityScore: 95,
      fastFailurePenaltyPercent: 40,
      fastFailureSeconds: 1,
      slowFailureSeconds: 10,
      cooldownMinutes: 30,
      sampleMode: props.sampleMode,
      explorationTrafficPercent: 3,
      probeIntervalMinutes: 15,
      prioritySamplingEnabled: props.prioritySamplingEnabled ?? true,
      prioritySamplingIntervalMinutes: 10,
      prioritySamplingBasePercent: 3,
      prioritySamplingDecayPercent: 70,
      prioritySamplingMinPercent: 0.5,
    },
  })
  return (
    <Form {...form}>
      <ChannelMonitorSmartScheduleGroupPolicyFields
        form={form}
        modelOptions={['model-a']}
      />
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

  test('shows the configured upstream request timeout for ratio and balance updates', () => {
    const markup = renderToStaticMarkup(<UpstreamRequestTimeoutFieldFixture />)

    assert.ok(markup.includes('上游请求超时'))
    assert.match(markup, /type="number"[^>]*min="1"[^>]*max="600"/)
    assert.match(markup, /value="30"/)
    assert.ok(markup.includes('单次倍率或余额更新超过该时间会终止'))
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

  test('top-aligns runtime settings without repeating response wait help', () => {
    const markup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)

    assert.ok(markup.includes('上游响应等待时间'))
    assert.match(markup, /type="number"[^>]*min="0"[^>]*max="600"/)
    assert.match(markup, /value="60"/)
    assert.match(
      markup,
      /<input(?=[^>]*name="smartSchedulePerformanceWindowMinutes")(?=[^>]*min="1")(?=[^>]*max="43200")(?=[^>]*value="60")[^>]*>/
    )
    assert.match(
      markup,
      /<input(?=[^>]*name="smartScheduleStabilityWindowMinutes")(?=[^>]*min="1")(?=[^>]*max="43200")(?=[^>]*value="120")[^>]*>/
    )
    assert.match(
      markup,
      /<input(?=[^>]*name="smartScheduleRateLimitCooldownSeconds")(?=[^>]*min="0")(?=[^>]*max="300")(?=[^>]*value="30")[^>]*>/
    )
    assert.ok(markup.includes('429 冷却时间'))
    assert.ok(
      markup.includes(
        '上游返回 429 后临时停止该渠道对应模型进入新请求；到期自动恢复，0 秒表示关闭'
      )
    )
    assert.ok(markup.includes('用于首字、TPS 和业务性能评分'))
    assert.ok(markup.includes('用于成功率、失败耗时和首字抖动'))
    assert.match(
      markup,
      /<div(?=[^>]*data-slot="smart-schedule-runtime-fields")(?=[^>]*class="[^"]*\bitems-start\b[^"]*")[^>]*>/
    )
    assert.equal(markup.includes('0 表示不限制'), false)
    assert.ok(markup.includes('aria-label="查看“上游响应等待时间”说明"'))
  })

  test('separates global runtime controls from explicit group policies', () => {
    const markup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)
    const runtimeSettingsIndex = markup.indexOf('运行设置')
    const groupPolicyIndex = markup.indexOf('分组策略')
    const forceResetIndex = markup.indexOf('强制重置优先级和权重')

    assert.ok(runtimeSettingsIndex >= 0)
    assert.ok(runtimeSettingsIndex < groupPolicyIndex)
    assert.ok(markup.includes('控制所有已配置分组的执行频率'))
    assert.ok(forceResetIndex > groupPolicyIndex)
  })

  test('only exposes explicitly configured groups to smart scheduling', () => {
    const markup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)

    assert.ok(markup.includes('只调度下方已配置策略的分组'))
    assert.ok(markup.includes('未配置分组不会参与智能调度'))
    assert.equal(markup.includes('参与分组'), false)
    assert.equal(markup.includes('默认策略'), false)
  })

  test('provides accessible help for every visible smart schedule setting', () => {
    const globalMarkup = renderToStaticMarkup(<SmartScheduleFieldsFixture />)
    const trafficPolicyMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='traffic'
      />
    )
    const probePolicyMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='probe'
      />
    )
    const weightPolicyMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='weight'
        sampleMode='probe'
      />
    )

    for (const label of [
      '智能调度',
      '调度间隔',
      '性能窗口',
      '稳定性窗口',
      '429 冷却时间',
      '上游响应等待时间',
      '强制重置优先级和权重',
    ]) {
      assert.ok(globalMarkup.includes(`aria-label="查看“${label}”说明"`))
    }
    for (const label of [
      '调度方式',
      '调整方式',
      '参与模型',
      '模型卡片顺序',
      '样本补充方式',
      '稳定性保护',
      '稳定性占比',
      '降级稳定性得分',
      '恢复稳定性得分',
      '最少样本',
      '快速失败惩罚',
      '快速失败界限',
      '慢失败界限',
      '降级时长',
      '成功延迟抖动',
      '允许抖动',
      '判定倍率',
      '绝对容差',
      '基线学习周期',
      '成本倍率',
      '首字时间',
      'TPS',
      '主渠道切换分差',
    ]) {
      assert.ok(trafficPolicyMarkup.includes(`aria-label="查看“${label}”说明"`))
    }
    assert.ok(
      trafficPolicyMarkup.includes('aria-label="查看“目标探索流量”说明"')
    )
    assert.ok(probePolicyMarkup.includes('aria-label="查看“探测间隔”说明"'))
    assert.ok(
      weightPolicyMarkup.includes('aria-label="查看“主渠道目标流量”说明"')
    )
  })

  test('shows primary traffic only for weight mode and switch threshold for both modes', () => {
    const weightMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='weight'
        sampleMode='probe'
      />
    )
    const priorityWeightMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='probe'
      />
    )

    assert.match(
      weightMarkup,
      /<input(?=[^>]*name="scoring.primaryTrafficPercent")(?=[^>]*min="51")(?=[^>]*max="99")(?=[^>]*value="90")[^>]*>/
    )
    assert.equal(
      priorityWeightMarkup.includes('name="scoring.primaryTrafficPercent"'),
      false
    )
    for (const markup of [weightMarkup, priorityWeightMarkup]) {
      assert.match(
        markup,
        /<input(?=[^>]*name="scoring.primarySwitchThresholdPercent")(?=[^>]*min="0")(?=[^>]*max="100")(?=[^>]*value="3")[^>]*>/
      )
      assert.equal(markup.includes('得分曲线指数'), false)
      assert.equal(markup.includes('相对权重拉伸'), false)
    }
  })

  test('shows the empty state before any group scheduling policy is configured', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPoliciesFixture configured={false} />
    )

    assert.ok(markup.includes('尚未配置分组调度策略'))
    assert.ok(markup.includes('智能调度不会处理任何分组'))
    assert.ok(markup.includes('新增分组策略'))
  })

  test('summarizes each explicitly configured group policy in a table row', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPoliciesFixture configured />
    )

    assert.ok(markup.includes('vip'))
    assert.ok(markup.includes('按成本倍率'))
    assert.ok(markup.includes('优先级分层 + 权重'))
    assert.ok(markup.includes('探索流量 3%'))
    assert.ok(markup.includes('每 10 分钟 · 3% 起'))
    assert.ok(markup.includes('全部模型'))
    assert.equal(markup.includes('未参与调度'), false)
    assert.ok(markup.includes('aria-label="编辑分组策略 vip"'))
    assert.ok(markup.includes('aria-label="删除分组调度策略 vip"'))
  })

  test('summarizes timed probing with its configured interval', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPoliciesFixture configured sampleMode='probe' />
    )

    assert.ok(markup.includes('样本补充'))
    assert.ok(markup.includes('每 10 分钟文本探测'))
    assert.equal(markup.includes('探索流量 3%'), false)
  })

  test('shows traffic percentage only while traffic exploration is selected', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='traffic'
      />
    )

    assert.ok(markup.includes('aria-label="分组样本补充方式"'))
    assert.ok(markup.includes('关闭'))
    assert.ok(markup.includes('探索流量'))
    assert.ok(markup.includes('定时探测'))
    assert.ok(markup.includes('目标探索流量'))
    assert.equal(markup.includes('探测间隔'), false)
  })

  test('shows the probe interval and disables traffic exploration in weight-only mode', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='weight'
        sampleMode='probe'
      />
    )

    assert.ok(markup.includes('探测间隔'))
    assert.ok(markup.includes('value="15"'))
    assert.equal(markup.includes('目标探索流量'), false)
    assert.match(markup, /<button[^>]*disabled=""[^>]*>探索流量<\/button>/)
    assert.ok(markup.includes('不改变真实业务请求的路由'))
    assert.ok(markup.includes('固定使用 /v1/responses 流式请求'))
    assert.ok(markup.includes('非文本模型会跳过'))
    assert.ok(markup.includes('需要先将调整方式设为'))
  })

  test('shows bounded low-priority rotation fields only in priority mode while enabled', () => {
    const priorityMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='off'
      />
    )
    const disabledMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='off'
        prioritySamplingEnabled={false}
      />
    )
    const weightMarkup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='weight'
        sampleMode='probe'
      />
    )

    assert.ok(priorityMarkup.includes('低优先级轮转采样'))
    assert.match(
      priorityMarkup,
      /<input(?=[^>]*name="prioritySamplingIntervalMinutes")(?=[^>]*min="1")(?=[^>]*max="1440")(?=[^>]*value="10")[^>]*>/
    )
    assert.match(
      priorityMarkup,
      /<input(?=[^>]*name="prioritySamplingBasePercent")(?=[^>]*min="0.1")(?=[^>]*max="20")(?=[^>]*value="3")[^>]*>/
    )
    assert.match(
      priorityMarkup,
      /<input(?=[^>]*name="prioritySamplingDecayPercent")(?=[^>]*min="1")(?=[^>]*max="100")(?=[^>]*value="70")[^>]*>/
    )
    assert.match(
      priorityMarkup,
      /<input(?=[^>]*name="prioritySamplingMinPercent")(?=[^>]*min="0.01")(?=[^>]*max="5")(?=[^>]*value="0.5")[^>]*>/
    )
    assert.equal(
      disabledMarkup.includes('name="prioritySamplingIntervalMinutes"'),
      false
    )
    assert.ok(weightMarkup.includes('当前配置不会生效'))
    assert.equal(
      weightMarkup.includes('name="prioritySamplingIntervalMinutes"'),
      false
    )
    const samplingSwitchMarkup =
      weightMarkup.match(
        /<[^>]*(?=[^>]*role="switch")(?=[^>]*aria-label="低优先级轮转采样")[^>]*>/
      )?.[0] ?? ''
    assert.ok(samplingSwitchMarkup)
    assert.match(samplingSwitchMarkup, /\sdata-disabled=""/)
    assert.match(samplingSwitchMarkup, /\saria-disabled="true"/)
  })

  test('shows bounded successful latency jitter controls while enabled', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='off'
      />
    )

    assert.ok(markup.includes('成功延迟抖动'))
    assert.ok(markup.includes('aria-label="成功延迟抖动"'))
    assert.ok(markup.includes('允许偶发的慢成功'))
    assert.match(
      markup,
      /<input(?=[^>]*name="jitterTolerancePercent")(?=[^>]*min="0")(?=[^>]*max="50")(?=[^>]*value="5")[^>]*>/
    )
    assert.match(
      markup,
      /<input(?=[^>]*name="jitterThresholdMultiplier")(?=[^>]*min="1.01")(?=[^>]*max="20")(?=[^>]*value="5")[^>]*>/
    )
    assert.match(
      markup,
      /<input(?=[^>]*name="jitterAbsoluteToleranceSeconds")(?=[^>]*min="0")(?=[^>]*max="60")(?=[^>]*value="10")[^>]*>/
    )
    assert.match(
      markup,
      /<input(?=[^>]*name="jitterBaselineMinutes")(?=[^>]*min="1")(?=[^>]*max="43200")(?=[^>]*value="60")[^>]*>/
    )
    assert.ok(markup.includes('慢请求阈值取'))
  })

  test('hides successful latency jitter tuning while disabled', () => {
    const markup = renderToStaticMarkup(
      <SmartScheduleGroupPolicyFieldsFixture
        applyMode='priority_weight'
        sampleMode='off'
        jitterEnabled={false}
      />
    )

    assert.ok(markup.includes('成功延迟抖动'))
    assert.equal(markup.includes('name="jitterTolerancePercent"'), false)
    assert.equal(markup.includes('name="jitterThresholdMultiplier"'), false)
    assert.equal(
      markup.includes('name="jitterAbsoluteToleranceSeconds"'),
      false
    )
    assert.equal(markup.includes('name="jitterBaselineMinutes"'), false)
  })
})
