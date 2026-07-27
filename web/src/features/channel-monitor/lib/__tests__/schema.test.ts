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

import {
  createChannelConcurrencyLimitSchema,
  createChannelMonitorSettingsSchema,
  MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MAX_CHANNEL_CONCURRENCY_LIMIT,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
  MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
} from '../schema'

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
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: true,
      autoEnableOnCostRatioRecovery: true,
      autoEnableOnBalanceRecovery: true,
      costRetentionDays: 120,
      emailNotificationEnabled: false,
      notificationEmail: '',
      probeResponseEnabled: true,
      relayResponseHeaderTimeoutSeconds: 60,
      smartScheduleEnabled: false,
      smartScheduleIntervalMinutes: 10,
      smartScheduleStrategy: 'smart',
      smartScheduleStabilityEnabled: false,
      smartScheduleScoring: {
        stabilityPercent: 50,
        curveExponent: 1,
        relativeWeightEnabled: true,
        relativeWeightStartPercent: 3,
        relativeWeightFullPercent: 10,
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
      smartScheduleApplyMode: 'weight',
      smartSchedulePerformanceMinutes: 60,
      smartScheduleModels: [],
      smartScheduleMinSamples: 5,
      smartScheduleMinSuccessRate: 80,
      smartScheduleCooldownMinutes: 30,
      smartScheduleForceReset: false,
    })

    assert.equal(settings.autoEnableOnCostRatioRecovery, true)
    assert.equal(settings.autoEnableOnBalanceRecovery, true)
    assert.equal(settings.autoUpdateConsecutiveFailureLimit, 2)
    assert.equal(settings.costRetentionDays, 120)
    assert.equal(settings.probeResponseEnabled, true)
    assert.equal(settings.relayResponseHeaderTimeoutSeconds, 60)
  })

  test('accepts retention boundaries and rejects invalid retention days', () => {
    const baseSettings = {
      autoUpdateIntervalMinutes: 10,
      autoUpdateRetryCount: 2,
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: false,
      autoEnableOnCostRatioRecovery: false,
      autoEnableOnBalanceRecovery: false,
      costRetentionDays: 120,
      emailNotificationEnabled: false,
      notificationEmail: '',
      probeResponseEnabled: false,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: false,
      smartScheduleIntervalMinutes: 10,
      smartScheduleStrategy: 'smart' as const,
      smartScheduleStabilityEnabled: false,
      smartScheduleScoring: {
        stabilityPercent: 50,
        curveExponent: 1,
        relativeWeightEnabled: true,
        relativeWeightStartPercent: 3,
        relativeWeightFullPercent: 10,
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
      smartScheduleApplyMode: 'weight' as const,
      smartSchedulePerformanceMinutes: 60 as const,
      smartScheduleModels: [],
      smartScheduleMinSamples: 5,
      smartScheduleMinSuccessRate: 80,
      smartScheduleCooldownMinutes: 30,
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
  })

  test('requires valid configurable smart schedule percentages', () => {
    const baseSettings = {
      autoUpdateIntervalMinutes: 10,
      autoUpdateRetryCount: 2,
      autoUpdateConsecutiveFailureLimit: 2,
      autoDisableOnUpdateFailure: false,
      autoEnableOnCostRatioRecovery: false,
      autoEnableOnBalanceRecovery: false,
      costRetentionDays: 120,
      emailNotificationEnabled: false,
      notificationEmail: '',
      probeResponseEnabled: false,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: true,
      smartScheduleIntervalMinutes: 10,
      smartScheduleStrategy: 'smart' as const,
      smartScheduleStabilityEnabled: true,
      smartScheduleScoring: {
        stabilityPercent: 50,
        curveExponent: 1,
        relativeWeightEnabled: true,
        relativeWeightStartPercent: 3,
        relativeWeightFullPercent: 10,
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
      smartScheduleApplyMode: 'weight' as const,
      smartSchedulePerformanceMinutes: 60 as const,
      smartScheduleModels: [],
      smartScheduleMinSamples: 5,
      smartScheduleMinSuccessRate: 80,
      smartScheduleCooldownMinutes: 30,
      smartScheduleForceReset: false,
    }
    const schema = createChannelMonitorSettingsSchema()
    const groupPolicy = {
      group: 'vip',
      strategy: baseSettings.smartScheduleStrategy,
      stabilityEnabled: baseSettings.smartScheduleStabilityEnabled,
      scoring: baseSettings.smartScheduleScoring,
      applyMode: baseSettings.smartScheduleApplyMode,
      models: baseSettings.smartScheduleModels,
      minSamples: baseSettings.smartScheduleMinSamples,
      minSuccessRate: baseSettings.smartScheduleMinSuccessRate,
      cooldownMinutes: baseSettings.smartScheduleCooldownMinutes,
    }

    assert.equal(schema.safeParse(baseSettings).success, true)
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          smart: {
            costRatioPercent: 50,
            firstTokenPercent: 30,
            tpsPercent: 30,
          },
        },
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          stabilityPercent: 101,
        },
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          curveExponent: 5.1,
        },
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          relativeWeightStartPercent: -0.1,
        },
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          relativeWeightStartPercent: 10,
          relativeWeightFullPercent: 10,
        },
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          relativeWeightStartPercent: 0,
          relativeWeightFullPercent: 100,
        },
      }).success,
      true
    )
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleScoring: {
          ...baseSettings.smartScheduleScoring,
          ratio: {
            costRatioPercent: 0,
            firstTokenPercent: 50,
            tpsPercent: 50,
          },
        },
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
  })
})
