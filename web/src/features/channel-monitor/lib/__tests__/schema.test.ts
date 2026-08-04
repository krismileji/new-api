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

import { DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES } from '../email-notification'
import {
  createChannelConcurrencyLimitSchema,
  createChannelMonitorSettingsSchema,
  createChannelMonitorSmartSchedulePolicySchema,
  MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MAX_CHANNEL_CONCURRENCY_LIMIT,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
  MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
  MAX_SMART_SCHEDULE_WINDOW_MINUTES,
  MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  MIN_SMART_SCHEDULE_WINDOW_MINUTES,
} from '../schema'
import { CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE } from '../smart-schedule-group-policy'

describe('smart schedule policy schema', () => {
  const schema = createChannelMonitorSmartSchedulePolicySchema()

  test('normalizes empty hidden controls when their branches are disabled', () => {
    const result = schema.safeParse({
      ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
      stabilityEnabled: false,
      jitterEnabled: false,
      applyMode: 'weight',
      sampleMode: 'off',
      scoring: {
        ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.scoring,
        stabilityPercent: '',
      },
      jitterTolerancePercent: '',
      jitterAbsoluteToleranceSeconds: '',
      jitterBaselineMinutes: '',
      minSamples: '',
      degradeStabilityScore: '',
      recoveryStabilityScore: '',
      fastFailurePenaltyPercent: '',
      fastFailureSeconds: '',
      slowFailureSeconds: '',
      cooldownMinutes: '',
      explorationTrafficPercent: '',
      probeIntervalMinutes: '',
      prioritySamplingEnabled: true,
      prioritySamplingIntervalMinutes: '',
      prioritySamplingBasePercent: '',
      prioritySamplingDecayPercent: '',
      prioritySamplingMinPercent: '',
    })

    assert.equal(result.success, true)
    if (!result.success) return
    assert.equal(result.data.prioritySamplingEnabled, false)
    assert.equal(result.data.scoring.stabilityPercent, 50)
  })

  test('continues to reject empty controls in active branches', () => {
    const basePolicy = CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE
    const activeCases = [
      { ...basePolicy, explorationTrafficPercent: '', sampleMode: 'traffic' },
      { ...basePolicy, probeIntervalMinutes: '', sampleMode: 'probe' },
      { ...basePolicy, prioritySamplingBasePercent: '' },
      { ...basePolicy, minSamples: '' },
      { ...basePolicy, jitterBaselineMinutes: '' },
    ]

    for (const policy of activeCases) {
      assert.equal(schema.safeParse(policy).success, false)
    }
  })
})

describe('channel concurrency limit schema', () => {
  const schema = createChannelConcurrencyLimitSchema()

  test('accepts unlimited and the maximum configured limit', () => {
    assert.equal(schema.parse({ concurrencyLimit: 0 }).concurrencyLimit, 0)
    assert.equal(
      schema.parse({ concurrencyLimit: MAX_CHANNEL_CONCURRENCY_LIMIT })
        .concurrencyLimit,
      MAX_CHANNEL_CONCURRENCY_LIMIT
    )
  })

  test('rejects empty, fractional, negative, and oversized values', () => {
    for (const concurrencyLimit of [
      '',
      1.5,
      -1,
      MAX_CHANNEL_CONCURRENCY_LIMIT + 1,
    ]) {
      assert.equal(schema.safeParse({ concurrencyLimit }).success, false)
    }
  })
})

describe('channel monitor settings schema', () => {
  test('keeps the cost ratio recovery switch in parsed settings', () => {
    const settings = createChannelMonitorSettingsSchema().parse({
      autoUpdateIntervalMinutes: 10,
      autoUpdateRetryCount: 2,
      upstreamRequestTimeoutSeconds: 30,
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: true,
      autoEnableOnCostRatioRecovery: true,
      autoEnableOnBalanceRecovery: true,
      costRetentionDays: 120,
      emailNotificationEnabled: false,
      notificationEmail: '',
      emailNotificationTypes: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
      probeResponseEnabled: true,
      relayResponseHeaderTimeoutSeconds: 60,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartScheduleIntervalMinutes: 10,
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleStabilityWindowMinutes: 120,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    })

    assert.equal(settings.autoEnableOnCostRatioRecovery, true)
    assert.equal(settings.autoEnableOnBalanceRecovery, true)
    assert.equal(settings.upstreamRequestTimeoutSeconds, 30)
    assert.equal(settings.autoUpdateConsecutiveFailureLimit, 2)
    assert.equal(settings.costRetentionDays, 120)
    assert.equal(settings.probeResponseEnabled, true)
    assert.equal(settings.relayResponseHeaderTimeoutSeconds, 60)
    assert.equal(settings.smartSchedulePerformanceWindowMinutes, 60)
    assert.equal(settings.smartScheduleStabilityWindowMinutes, 120)
    assert.equal(settings.smartScheduleRateLimitCooldownSeconds, 30)
  })

  test('requires at least one selected notification type while email is enabled', () => {
    const result = createChannelMonitorSettingsSchema().safeParse({
      autoUpdateIntervalMinutes: 10,
      autoUpdateRetryCount: 2,
      upstreamRequestTimeoutSeconds: 30,
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: false,
      autoEnableOnCostRatioRecovery: false,
      autoEnableOnBalanceRecovery: false,
      costRetentionDays: 120,
      emailNotificationEnabled: true,
      notificationEmail: 'alerts@example.com',
      emailNotificationTypes: [],
      probeResponseEnabled: false,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartScheduleIntervalMinutes: 10,
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleStabilityWindowMinutes: 60,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    })

    assert.equal(result.success, false)
    if (result.success) return
    assert.ok(
      result.error.issues.some(
        (issue) => issue.path.join('.') === 'emailNotificationTypes'
      )
    )
  })

  test('accepts bounded general settings and rejects values outside their ranges', () => {
    const baseSettings = {
      autoUpdateIntervalMinutes: 10,
      autoUpdateRetryCount: 2,
      upstreamRequestTimeoutSeconds: 30,
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: false,
      autoEnableOnCostRatioRecovery: false,
      autoEnableOnBalanceRecovery: false,
      costRetentionDays: 120,
      emailNotificationEnabled: false,
      notificationEmail: '',
      emailNotificationTypes: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
      probeResponseEnabled: false,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartScheduleIntervalMinutes: 10,
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleStabilityWindowMinutes: 60,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    }
    const schema = createChannelMonitorSettingsSchema()
    for (const autoUpdateConsecutiveFailureLimit of [
      MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
      MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
    ]) {
      assert.equal(
        schema.parse({
          ...baseSettings,
          autoUpdateConsecutiveFailureLimit,
        }).autoUpdateConsecutiveFailureLimit,
        autoUpdateConsecutiveFailureLimit
      )
    }
    for (const autoUpdateConsecutiveFailureLimit of [
      MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT - 1,
      1.5,
      MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT + 1,
    ]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          autoUpdateConsecutiveFailureLimit,
        }).success,
        false
      )
    }

    for (const upstreamRequestTimeoutSeconds of [
      MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
      MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
    ]) {
      assert.equal(
        schema.parse({ ...baseSettings, upstreamRequestTimeoutSeconds })
          .upstreamRequestTimeoutSeconds,
        upstreamRequestTimeoutSeconds
      )
    }
    for (const upstreamRequestTimeoutSeconds of [
      MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS - 1,
      1.5,
      MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS + 1,
    ]) {
      assert.equal(
        schema.safeParse({ ...baseSettings, upstreamRequestTimeoutSeconds })
          .success,
        false
      )
    }

    for (const costRetentionDays of [
      MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
      MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
    ]) {
      assert.equal(
        schema.parse({ ...baseSettings, costRetentionDays }).costRetentionDays,
        costRetentionDays
      )
    }
    for (const costRetentionDays of [
      MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS - 1,
      1.5,
      MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS + 1,
    ]) {
      assert.equal(
        schema.safeParse({ ...baseSettings, costRetentionDays }).success,
        false
      )
    }

    for (const relayResponseHeaderTimeoutSeconds of [
      0,
      MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
    ]) {
      assert.equal(
        schema.parse({ ...baseSettings, relayResponseHeaderTimeoutSeconds })
          .relayResponseHeaderTimeoutSeconds,
        relayResponseHeaderTimeoutSeconds
      )
    }
    for (const relayResponseHeaderTimeoutSeconds of [
      -1,
      1.5,
      MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS + 1,
    ]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          relayResponseHeaderTimeoutSeconds,
        }).success,
        false
      )
    }

    for (const windowMinutes of [
      MIN_SMART_SCHEDULE_WINDOW_MINUTES,
      MAX_SMART_SCHEDULE_WINDOW_MINUTES,
    ]) {
      const parsed = schema.parse({
        ...baseSettings,
        smartSchedulePerformanceWindowMinutes: windowMinutes,
        smartScheduleStabilityWindowMinutes: windowMinutes,
      })
      assert.equal(parsed.smartSchedulePerformanceWindowMinutes, windowMinutes)
      assert.equal(parsed.smartScheduleStabilityWindowMinutes, windowMinutes)
    }
    for (const windowMinutes of [
      MIN_SMART_SCHEDULE_WINDOW_MINUTES - 1,
      1.5,
      MAX_SMART_SCHEDULE_WINDOW_MINUTES + 1,
    ]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartSchedulePerformanceWindowMinutes: windowMinutes,
        }).success,
        false
      )
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleStabilityWindowMinutes: windowMinutes,
        }).success,
        false
      )
    }

    for (const smartScheduleRateLimitCooldownSeconds of [
      0,
      MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
    ]) {
      assert.equal(
        schema.parse({
          ...baseSettings,
          smartScheduleRateLimitCooldownSeconds,
        }).smartScheduleRateLimitCooldownSeconds,
        smartScheduleRateLimitCooldownSeconds
      )
    }
    for (const smartScheduleRateLimitCooldownSeconds of [
      -1,
      1.5,
      MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS + 1,
    ]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleRateLimitCooldownSeconds,
        }).success,
        false
      )
    }
  })

  test('requires a complete explicit policy for every scheduled group', () => {
    const scoring = {
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
    }
    const groupPolicy = {
      group: 'vip',
      strategy: 'smart' as const,
      stabilityEnabled: true,
      jitterEnabled: true,
      jitterTolerancePercent: 5,
      jitterAbsoluteToleranceSeconds: 10,
      jitterBaselineMinutes: 60,
      scoring,
      applyMode: 'priority_weight' as const,
      models: [],
      minSamples: 5,
      degradeStabilityScore: 90,
      recoveryStabilityScore: 95,
      fastFailurePenaltyPercent: 40,
      fastFailureSeconds: 1,
      slowFailureSeconds: 10,
      cooldownMinutes: 30,
      sampleMode: 'traffic' as const,
      explorationTrafficPercent: 3,
      probeIntervalMinutes: 10,
      prioritySamplingEnabled: true,
      prioritySamplingIntervalMinutes: 10,
      prioritySamplingBasePercent: 3,
      prioritySamplingDecayPercent: 70,
      prioritySamplingMinPercent: 0.5,
    }
    const baseSettings = {
      autoUpdateIntervalMinutes: 10,
      autoUpdateRetryCount: 2,
      upstreamRequestTimeoutSeconds: 30,
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: false,
      autoEnableOnCostRatioRecovery: false,
      autoEnableOnBalanceRecovery: false,
      costRetentionDays: 120,
      emailNotificationEnabled: false,
      notificationEmail: '',
      emailNotificationTypes: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
      probeResponseEnabled: false,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: true,
      smartScheduleGroupPolicies: [groupPolicy],
      smartScheduleIntervalMinutes: 10,
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleStabilityWindowMinutes: 60,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    }
    const schema = createChannelMonitorSettingsSchema()

    assert.equal(schema.safeParse(baseSettings).success, true)
    for (const jitterTolerancePercent of [0, 50]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterTolerancePercent },
          ],
        }).success,
        true
      )
    }
    for (const jitterTolerancePercent of [-0.1, 50.1]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterTolerancePercent },
          ],
        }).success,
        false
      )
    }
    for (const jitterAbsoluteToleranceSeconds of [0, 1.5, 60]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterAbsoluteToleranceSeconds },
          ],
        }).success,
        true
      )
    }
    for (const jitterAbsoluteToleranceSeconds of [-1, 61]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterAbsoluteToleranceSeconds },
          ],
        }).success,
        false
      )
    }
    for (const jitterBaselineMinutes of [1, 43_200]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterBaselineMinutes },
          ],
        }).success,
        true
      )
    }
    for (const jitterBaselineMinutes of [0, 1.5, 43_201]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterBaselineMinutes },
          ],
        }).success,
        false
      )
    }
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          {
            ...groupPolicy,
            scoring: {
              ...scoring,
              smart: {
                costRatioPercent: 50,
                firstTokenPercent: 30,
                tpsPercent: 30,
              },
            },
          },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, recoveryStabilityScore: 90 },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, fastFailureSeconds: 10, slowFailureSeconds: 10 },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, fastFailureSeconds: 59.9, slowFailureSeconds: 60 },
        ],
      }).success,
      true
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, fastFailureSeconds: 60 },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, applyMode: 'weight', sampleMode: 'traffic' },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          {
            ...groupPolicy,
            applyMode: 'weight',
            sampleMode: 'probe',
            probeIntervalMinutes: 15,
          },
        ],
      }).success,
      true
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, sampleMode: 'probe', probeIntervalMinutes: 0 },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, scoring: { ...scoring, stabilityPercent: 101 } },
        ],
      }).success,
      false
    )
    for (const primaryTrafficPercent of [51, 51.5, 99]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            {
              ...groupPolicy,
              scoring: { ...scoring, primaryTrafficPercent },
            },
          ],
        }).success,
        true
      )
    }
    for (const primaryTrafficPercent of [50, 100]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            {
              ...groupPolicy,
              scoring: { ...scoring, primaryTrafficPercent },
            },
          ],
        }).success,
        false
      )
    }
    for (const primarySwitchThresholdPercent of [0, 100]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            {
              ...groupPolicy,
              scoring: { ...scoring, primarySwitchThresholdPercent },
            },
          ],
        }).success,
        true
      )
    }
    for (const primarySwitchThresholdPercent of [-0.1, 100.1]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            {
              ...groupPolicy,
              scoring: { ...scoring, primarySwitchThresholdPercent },
            },
          ],
        }).success,
        false
      )
    }
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          {
            ...groupPolicy,
            scoring: {
              ...scoring,
              ratio: {
                costRatioPercent: 0,
                firstTokenPercent: 50,
                tpsPercent: 50,
              },
            },
          },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          {
            ...groupPolicy,
            strategy: 'ratio',
            stabilityEnabled: false,
            models: [],
          },
        ],
      }).success,
      true
    )
    const missingPolicyResult = schema.safeParse({
      ...baseSettings,
      smartScheduleGroupPolicies: [],
    })
    assert.equal(missingPolicyResult.success, false)
    if (!missingPolicyResult.success) {
      assert.equal(
        missingPolicyResult.error.issues[0]?.message,
        '启用智能调度前请至少配置一个分组策略'
      )
    }
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleEnabled: false,
        smartScheduleGroupPolicies: [],
      }).success,
      true
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          groupPolicy,
          { ...groupPolicy, group: ' vip ', applyMode: 'priority_weight' },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [{ ...groupPolicy, minSamples: 0 }],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, explorationTrafficPercent: 0 },
        ],
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, explorationTrafficPercent: 20.1 },
        ],
      }).success,
      false
    )
    for (const prioritySamplingIntervalMinutes of [1, 1440]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingIntervalMinutes },
          ],
        }).success,
        true
      )
    }
    for (const prioritySamplingIntervalMinutes of [0, 1.5, 1441]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingIntervalMinutes },
          ],
        }).success,
        false
      )
    }
    for (const prioritySamplingBasePercent of [0.1, 20]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingBasePercent },
          ],
        }).success,
        true
      )
    }
    for (const prioritySamplingBasePercent of [0.09, 20.1]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingBasePercent },
          ],
        }).success,
        false
      )
    }
    for (const prioritySamplingDecayPercent of [1, 100]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingDecayPercent },
          ],
        }).success,
        true
      )
    }
    for (const prioritySamplingDecayPercent of [0.9, 100.1]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingDecayPercent },
          ],
        }).success,
        false
      )
    }
    for (const prioritySamplingMinPercent of [0.01, 5]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingMinPercent },
          ],
        }).success,
        true
      )
    }
    for (const prioritySamplingMinPercent of [0, 5.1]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, prioritySamplingMinPercent },
          ],
        }).success,
        false
      )
    }
  })
})
