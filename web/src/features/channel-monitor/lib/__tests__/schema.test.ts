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

import { describe, test } from 'vitest'

import { createChannelMonitorCustomFormConfig } from '../custom-upstream'
import { DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES } from '../email-notification'
import {
  createChannelConcurrencyLimitSchema,
  createChannelMonitorSettingsSchema,
  createChannelMonitorSmartSchedulePolicySchema,
  DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_ENABLED,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_RATIO_MONITOR_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_PROBE_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_GROUP_MONITOR_RETENTION_DAYS,
  createUpstreamConfigSchema,
  MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MAX_CHANNEL_CONCURRENCY_LIMIT,
  MAX_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  MAX_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  MAX_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  MAX_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
  MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  MAX_PROBE_RESPONSE_DELAY_MS,
  MAX_PROBE_RESPONSE_TOKEN_COUNT,
  MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
  MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
  MAX_SMART_SCHEDULE_REALTIME_RETENTION_MINUTES,
  MAX_SMART_SCHEDULE_REALTIME_SAMPLE_LIMIT,
  MAX_SMART_SCHEDULE_WINDOW_MINUTES,
  MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  MIN_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  MIN_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  MIN_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  MIN_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  MIN_SMART_SCHEDULE_WINDOW_MINUTES,
  MIN_SMART_SCHEDULE_REALTIME_RETENTION_MINUTES,
  MIN_SMART_SCHEDULE_REALTIME_SAMPLE_LIMIT,
} from '../schema'
import { CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE } from '../smart-schedule-group-policy'

const upstreamConfigBase = {
  upstreamType: 'sub2api' as const,
  baseUrl: 'https://upstream.example',
  group: 'vip',
  authType: 'token' as const,
  userId: 0,
  accessToken: '',
  refreshToken: '',
  account: '',
  password: '',
  singleChannelAction: 'none' as const,
  multipleChannelsAction: 'none' as const,
  ratioSyncEnabled: true,
  balanceSyncEnabled: true,
  balanceWarningThreshold: null,
  balanceAutoDisableThreshold: null,
  costConversionMode: 'none' as const,
  rechargePaidCny: 1,
  rechargeCreditedUsd: 1,
  subscriptionPeriod: 'month' as const,
  subscriptionPriceCny: 1,
  subscriptionDailyUsd: 1,
  customConfig: createChannelMonitorCustomFormConfig(undefined),
}

describe('Sub2API token schema', () => {
  test('requires a manual token for a new configuration', () => {
    const schema = createUpstreamConfigSchema(null)
    const result = schema.safeParse(upstreamConfigBase)

    assert.equal(result.success, false)
    if (result.success) return
    assert.ok(
      result.error.issues.some(
        (issue) =>
          issue.path.join('.') === 'accessToken' &&
          issue.message === '请输入 Sub2API Token（旧版访问令牌）'
      )
    )
  })

  test('allows an empty manual token field when the same upstream already stores it', () => {
    const schema = createUpstreamConfigSchema({
      type: 'sub2api',
      baseUrl: upstreamConfigBase.baseUrl,
      authType: 'token',
      hasAccessToken: true,
      hasRefreshToken: true,
      account: '',
      hasPassword: false,
    })

    assert.equal(
      schema.safeParse({
        ...upstreamConfigBase,
        authType: 'token',
        accessToken: '',
      }).success,
      true
    )
  })

  test('validates manual and refresh Token fields independently', () => {
    const schema = createUpstreamConfigSchema(null)
    const manualTokenResult = schema.safeParse({
      ...upstreamConfigBase,
      authType: 'token',
      accessToken: 'access-token',
      refreshToken: '',
    })
    const refreshTokenResult = schema.safeParse({
      ...upstreamConfigBase,
      authType: 'token',
      accessToken: 'access-token',
      refreshToken: 'refresh-token',
    })

    assert.equal(manualTokenResult.success, true)
    assert.equal(refreshTokenResult.success, true)
  })
})

describe('smart schedule policy schema', () => {
  const schema = createChannelMonitorSmartSchedulePolicySchema()

  test('normalizes empty hidden controls when their branches are disabled', () => {
    const result = schema.safeParse({
      ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
      stabilityEnabled: false,
      jitterEnabled: false,
      applyMode: 'weight',
      sampleMode: 'off',
      adaptiveSamplingEnabled: false,
      scoring: {
        ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.scoring,
        stabilityPercent: '',
      },
      jitterTolerancePercent: '',
      jitterSlowThresholdSeconds: '',
      minSamples: '',
      recoveryStabilityScore: '',
      fastFailurePenaltyPercent: '',
      fastFailureSeconds: '',
      fastFailureSameChannelRetryCount: '',
      fastFailureSameChannelRetryDelayMs: '',
      slowFailureSeconds: '',
      cooldownMinutes: '',
      explorationTrafficPercent: '',
      explorationMaxPromptKTokens: '',
      stabilityReleaseMaxPromptKTokens: '',
      probeIntervalMinutes: '',
      degradedProbeEnabled: true,
    })

    assert.equal(result.success, true)
    if (!result.success) return
    assert.equal(result.data.degradedProbeEnabled, false)
    assert.equal(result.data.scoring.stabilityPercent, 50)
  })

  test('rejects policies that omit current adaptive controls', () => {
    const incompletePolicy: Record<string, unknown> = {
      ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
    }
    delete incompletePolicy.adaptiveSamplingMaxPercent

    assert.equal(schema.safeParse(incompletePolicy).success, false)
  })

  test('does not accept the legacy adaptive entry threshold', () => {
    const legacyPolicy: Record<string, unknown> = {
      ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
      adaptiveSamplingEnterRequestPercent: 10,
    }
    delete legacyPolicy.adaptiveSamplingFirstTokenWarningRequestPercent

    assert.equal(schema.safeParse(legacyPolicy).success, false)
  })

  test('continues to reject empty controls in active branches', () => {
    const basePolicy = CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE
    const activeCases = [
      { ...basePolicy, explorationTrafficPercent: '', sampleMode: 'traffic' },
      { ...basePolicy, explorationMaxPromptKTokens: '', sampleMode: 'traffic' },
      { ...basePolicy, stabilityReleaseMaxPromptKTokens: '' },
      { ...basePolicy, probeIntervalMinutes: '', sampleMode: 'probe' },
      { ...basePolicy, degradedProbeEnabled: true, probeIntervalMinutes: '' },
      { ...basePolicy, minSamples: '' },
      { ...basePolicy, fastFailureSameChannelRetryCount: '' },
      { ...basePolicy, fastFailureSameChannelRetryDelayMs: '' },
      { ...basePolicy, jitterSlowThresholdSeconds: '' },
      { ...basePolicy, adaptiveSamplingBasePercent: '' },
      { ...basePolicy, adaptiveSamplingWindowMinutes: '' },
      { ...basePolicy, adaptiveSamplingWindowRequests: '' },
      { ...basePolicy, adaptiveSamplingFirstTokenWarningRequestPercent: '' },
      { ...basePolicy, adaptiveSamplingMinComparableChannels: '' },
    ]

    for (const policy of activeCases) {
      assert.equal(schema.safeParse(policy).success, false)
    }
  })

  test('validates the adaptive time and request windows and percentage hysteresis', () => {
    const basePolicy = CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE
    assert.equal(
      schema.safeParse({
        ...basePolicy,
        adaptiveSamplingMaxPercent: 49,
        adaptiveSamplingWindowMinutes: 1,
        adaptiveSamplingWindowRequests: 1,
        adaptiveSamplingFirstTokenWarningRequestPercent: 10,
        adaptiveSamplingRecoverRequestPercent: 95,
        adaptiveSamplingSwitchConfirmRequestPercent: 95,
      }).success,
      true
    )
    for (const value of [0, 61, 1.5]) {
      assert.equal(
        schema.safeParse({
          ...basePolicy,
          adaptiveSamplingWindowMinutes: value,
        }).success,
        false
      )
    }
    for (const value of [0, 1_001, 1.5]) {
      assert.equal(
        schema.safeParse({
          ...basePolicy,
          adaptiveSamplingWindowRequests: value,
        }).success,
        false
      )
    }
    assert.equal(
      schema.safeParse({
        ...basePolicy,
        adaptiveSamplingFirstTokenWarningRequestPercent: 20,
        adaptiveSamplingRecoverRequestPercent: 80,
      }).success,
      false
    )
    assert.equal(
      schema.safeParse({
        ...basePolicy,
        adaptiveSamplingSwitchConfirmRequestPercent: 90,
      }).success,
      false
    )
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
  test('exposes the retention defaults used when options are missing', () => {
    assert.deepEqual(
      [
        DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_RATIO_MONITOR_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_PROBE_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_CLEANUP_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_GROUP_MONITOR_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
        DEFAULT_CHANNEL_MONITOR_CLEANUP_ENABLED,
        DEFAULT_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
        DEFAULT_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
        DEFAULT_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
        DEFAULT_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
      ],
      [
        30,
        30,
        30,
        7,
        3,
        7,
        7,
        7,
        3,
        7,
        7,
        7,
        7,
        365,
        7,
        7,
        30,
        true,
        1000,
        10,
        60,
        1440,
      ]
    )
  })

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
      executionDetailRetentionDays: 14,
      taskRetentionDays: 90,
      ratioHistoryRetentionDays: 365,
      statusProbeHistoryRetentionDays: 7,
      emailNotificationEnabled: false,
      notificationEmail: '',
      emailNotificationTypes: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
      errorMessageMapping: '{"429":"请求过于频繁，请稍后再试"}',
      errorMessageKeywords: '',
      probeResponseEnabled: true,
      probeResponseMatchInput: ' health check ',
      probeResponseText: ' healthy ',
      probeResponseMinDelayMs: 125,
      probeResponseMaxDelayMs: 875,
      probeResponseInputTokens: 7,
      probeResponseCacheWriteTokens: 1,
      probeResponseCachedTokens: 2,
      probeResponseOutputTokens: 11,
      relayResponseHeaderTimeoutSeconds: 60,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleRealtimeRetentionMinutes: 120,
      smartScheduleRealtimeSampleLimit: 20_000,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    })

    assert.equal(settings.autoEnableOnCostRatioRecovery, true)
    assert.equal(settings.autoEnableOnBalanceRecovery, true)
    assert.equal(settings.upstreamRequestTimeoutSeconds, 30)
    assert.equal(settings.autoUpdateConsecutiveFailureLimit, 2)
    assert.equal(settings.costRetentionDays, 120)
    assert.equal(settings.executionDetailRetentionDays, 14)
    assert.equal(settings.taskRetentionDays, 90)
    assert.equal(settings.ratioHistoryRetentionDays, 365)
    assert.equal(settings.statusProbeHistoryRetentionDays, 7)
    assert.equal(settings.modelDetectionRetentionDays, 30)
    assert.equal(settings.cleanupEnabled, true)
    assert.equal(settings.cleanupBatchSize, 1000)
    assert.equal(settings.cleanupBudgetSeconds, 10)
    assert.equal(settings.cleanupContinuationSeconds, 60)
    assert.equal(settings.cleanupIntervalMinutes, 1440)
    assert.equal(
      settings.errorMessageMapping,
      '{"429":"请求过于频繁，请稍后再试"}'
    )
    assert.equal(settings.probeResponseEnabled, true)
    assert.equal(settings.probeResponseMatchInput, 'health check')
    assert.equal(settings.probeResponseText, 'healthy')
    assert.equal(settings.probeResponseMinDelayMs, 125)
    assert.equal(settings.probeResponseMaxDelayMs, 875)
    assert.equal(settings.probeResponseInputTokens, 7)
    assert.equal(settings.probeResponseCacheWriteTokens, 1)
    assert.equal(settings.probeResponseCachedTokens, 2)
    assert.equal(settings.probeResponseOutputTokens, 11)
    assert.equal(settings.relayResponseHeaderTimeoutSeconds, 60)
    assert.equal(settings.smartSchedulePerformanceWindowMinutes, 60)
    assert.equal(settings.smartScheduleRealtimeRetentionMinutes, 120)
    assert.equal(settings.smartScheduleRealtimeSampleLimit, 20_000)
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
      executionDetailRetentionDays: 14,
      taskRetentionDays: 90,
      ratioHistoryRetentionDays: 365,
      statusProbeHistoryRetentionDays: 7,
      emailNotificationEnabled: true,
      notificationEmail: 'alerts@example.com',
      emailNotificationTypes: [],
      probeResponseEnabled: false,
      probeResponseMatchInput: 'hi',
      probeResponseText: 'Hi. What are you working on?',
      probeResponseMinDelayMs: 500,
      probeResponseMaxDelayMs: 2000,
      probeResponseInputTokens: 4387,
      probeResponseCacheWriteTokens: 172,
      probeResponseCachedTokens: 4001,
      probeResponseOutputTokens: 12,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleRealtimeRetentionMinutes: 60,
      smartScheduleRealtimeSampleLimit: 20_000,
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
      executionDetailRetentionDays: 14,
      taskRetentionDays: 90,
      ratioHistoryRetentionDays: 365,
      statusProbeHistoryRetentionDays: 7,
      emailNotificationEnabled: false,
      notificationEmail: '',
      emailNotificationTypes: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
      errorMessageMapping: '',
      errorMessageKeywords: '',
      probeResponseEnabled: false,
      probeResponseMatchInput: 'hi',
      probeResponseText: 'Hi. What are you working on?',
      probeResponseMinDelayMs: 500,
      probeResponseMaxDelayMs: 2000,
      probeResponseInputTokens: 4387,
      probeResponseCacheWriteTokens: 172,
      probeResponseCachedTokens: 4001,
      probeResponseOutputTokens: 12,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: false,
      smartScheduleGroupPolicies: [],
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleRealtimeRetentionMinutes: 60,
      smartScheduleRealtimeSampleLimit: 20_000,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    }
    const schema = createChannelMonitorSettingsSchema()
    for (const patch of [
      { probeResponseMatchInput: '' },
      { probeResponseText: '' },
      { probeResponseMinDelayMs: -1 },
      { probeResponseMaxDelayMs: MAX_PROBE_RESPONSE_DELAY_MS + 1 },
      { probeResponseMinDelayMs: 2001, probeResponseMaxDelayMs: 2000 },
      { probeResponseInputTokens: -1 },
      { probeResponseOutputTokens: MAX_PROBE_RESPONSE_TOKEN_COUNT + 1 },
      { errorMessageMapping: '{"429":429}' },
      { errorMessageMapping: '[]' },
      {
        errorMessageKeywords: Array.from({ length: 33 }, () => 'keyword').join(
          '\n'
        ),
      },
      { errorMessageKeywords: 'x'.repeat(129) },
    ]) {
      assert.equal(
        schema.safeParse({ ...baseSettings, ...patch }).success,
        false
      )
    }
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

    for (const field of [
      'executionDetailRetentionDays',
      'taskRetentionDays',
      'ratioHistoryRetentionDays',
    ] as const) {
      for (const retentionDays of [
        MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
        MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
      ]) {
        const candidate = { ...baseSettings, [field]: retentionDays }
        if (field === 'executionDetailRetentionDays') {
          candidate.taskRetentionDays = Math.max(
            candidate.taskRetentionDays,
            retentionDays
          )
        } else if (field === 'taskRetentionDays') {
          candidate.executionDetailRetentionDays = Math.min(
            candidate.executionDetailRetentionDays,
            retentionDays
          )
        }
        assert.equal(schema.parse(candidate)[field], retentionDays)
      }
      for (const retentionDays of [
        MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS - 1,
        1.5,
        MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS + 1,
      ]) {
        assert.equal(
          schema.safeParse({ ...baseSettings, [field]: retentionDays }).success,
          false
        )
      }
    }
    for (const retentionDays of [
      MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
      MAX_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
    ]) {
      assert.equal(
        schema.parse({
          ...baseSettings,
          statusProbeHistoryRetentionDays: retentionDays,
        }).statusProbeHistoryRetentionDays,
        retentionDays
      )
    }
    for (const retentionDays of [0, 1.5, 91]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          statusProbeHistoryRetentionDays: retentionDays,
        }).success,
        false
      )
    }
    for (const retentionDays of [
      MIN_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
      MAX_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
    ]) {
      assert.equal(
        schema.parse({
          ...baseSettings,
          modelDetectionRetentionDays: retentionDays,
        }).modelDetectionRetentionDays,
        retentionDays
      )
    }
    for (const retentionDays of [6, 1.5, 181]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          modelDetectionRetentionDays: retentionDays,
        }).success,
        false
      )
    }
    for (const [field, minimum, maximum] of [
      [
        'cleanupBatchSize',
        MIN_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
        MAX_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
      ],
      [
        'cleanupBudgetSeconds',
        MIN_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
        MAX_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
      ],
      [
        'cleanupContinuationSeconds',
        MIN_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
        MAX_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
      ],
      [
        'cleanupIntervalMinutes',
        MIN_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
        MAX_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
      ],
    ] as const) {
      for (const value of [minimum, maximum]) {
        assert.equal(
          schema.parse({ ...baseSettings, [field]: value })[field],
          value
        )
      }
      for (const value of [minimum - 1, 1.5, maximum + 1]) {
        assert.equal(
          schema.safeParse({ ...baseSettings, [field]: value }).success,
          false
        )
      }
    }
    const invalidRetentionRelationship = schema.safeParse({
      ...baseSettings,
      executionDetailRetentionDays: 91,
      taskRetentionDays: 90,
    })
    assert.equal(invalidRetentionRelationship.success, false)
    if (!invalidRetentionRelationship.success) {
      assert.ok(
        invalidRetentionRelationship.error.issues.some(
          (issue) => issue.path.join('.') === 'taskRetentionDays'
        )
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
        smartScheduleRealtimeRetentionMinutes: Math.max(
          windowMinutes,
          MIN_SMART_SCHEDULE_REALTIME_RETENTION_MINUTES
        ),
      })
      assert.equal(parsed.smartSchedulePerformanceWindowMinutes, windowMinutes)
    }

    for (const retentionMinutes of [
      MIN_SMART_SCHEDULE_REALTIME_RETENTION_MINUTES,
      MAX_SMART_SCHEDULE_REALTIME_RETENTION_MINUTES,
    ]) {
      const windowMinutes = Math.min(retentionMinutes, 60)
      const parsed = schema.parse({
        ...baseSettings,
        smartSchedulePerformanceWindowMinutes: windowMinutes,
        smartScheduleRealtimeRetentionMinutes: retentionMinutes,
      })
      assert.equal(
        parsed.smartScheduleRealtimeRetentionMinutes,
        retentionMinutes
      )
    }
    for (const sampleLimit of [
      MIN_SMART_SCHEDULE_REALTIME_SAMPLE_LIMIT,
      MAX_SMART_SCHEDULE_REALTIME_SAMPLE_LIMIT,
    ]) {
      assert.equal(
        schema.parse({
          ...baseSettings,
          smartScheduleRealtimeSampleLimit: sampleLimit,
        }).smartScheduleRealtimeSampleLimit,
        sampleLimit
      )
    }
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartSchedulePerformanceWindowMinutes: 120,
        smartScheduleRealtimeRetentionMinutes: 60,
      }).success,
      false
    )
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
      stabilityWindowMinutes: 60,
      jitterEnabled: true,
      jitterTolerancePercent: 5,
      jitterSlowThresholdSeconds: 10,
      scoring,
      applyMode: 'priority_weight' as const,
      models: [],
      minSamples: 5,
      recoveryStabilityScore: 95,
      fastFailurePenaltyPercent: 40,
      fastFailureSeconds: 1,
      fastFailureSameChannelRetryCount: 2,
      fastFailureSameChannelRetryDelayMs: 750,
      slowFailureSeconds: 10,
      burstFailureWindowMinutes: 1,
      burstFailureWindowRequests: 100,
      burstFailureThresholdPercent: 3,
      consecutiveFailureThreshold: 2,
      recoverySuccessThreshold: 2,
      cooldownMinutes: 30,
      sampleMode: 'traffic' as const,
      samplingOrder: 'priority_weight' as const,
      explorationTrafficPercent: 3,
      explorationMaxPromptKTokens: 50,
      stabilityReleaseMaxPromptKTokens: 0,
      probeIntervalMinutes: 10,
      adaptiveSamplingEnabled: true,
      adaptiveSamplingBasePercent: 3,
      adaptiveSamplingMaxPercent: 30,
      adaptiveSamplingErrorWarningPercent: 5,
      adaptiveSamplingErrorCriticalPercent: 15,
      adaptiveSamplingFirstTokenWarningSeconds: 5,
      adaptiveSamplingFirstTokenCriticalSeconds: 10,
      adaptiveSamplingWindowMinutes: 10,
      adaptiveSamplingWindowRequests: 100,
      adaptiveSamplingFirstTokenWarningRequestPercent: 10,
      adaptiveSamplingRecoverRequestPercent: 95,
      adaptiveSamplingSwitchConfirmRequestPercent: 95,
      adaptiveSamplingMinComparableChannels: 2,
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
      executionDetailRetentionDays: 14,
      taskRetentionDays: 90,
      ratioHistoryRetentionDays: 365,
      statusProbeHistoryRetentionDays: 7,
      emailNotificationEnabled: false,
      notificationEmail: '',
      emailNotificationTypes: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
      probeResponseEnabled: false,
      probeResponseMatchInput: 'hi',
      probeResponseText: 'Hi. What are you working on?',
      probeResponseMinDelayMs: 500,
      probeResponseMaxDelayMs: 2000,
      probeResponseInputTokens: 4387,
      probeResponseCacheWriteTokens: 172,
      probeResponseCachedTokens: 4001,
      probeResponseOutputTokens: 12,
      relayResponseHeaderTimeoutSeconds: 0,
      smartScheduleEnabled: true,
      smartScheduleGroupPolicies: [groupPolicy],
      smartSchedulePerformanceWindowMinutes: 60,
      smartScheduleRealtimeRetentionMinutes: 60,
      smartScheduleRealtimeSampleLimit: 20_000,
      smartScheduleRateLimitCooldownSeconds: 30,
      smartScheduleForceReset: false,
    }
    const schema = createChannelMonitorSettingsSchema()

    assert.equal(schema.safeParse(baseSettings).success, true)
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          { ...groupPolicy, stabilityWindowMinutes: 120 },
        ],
      }).success,
      false
    )
    for (const stabilityWindowMinutes of [
      MIN_SMART_SCHEDULE_WINDOW_MINUTES - 1,
      1.5,
      MAX_SMART_SCHEDULE_WINDOW_MINUTES + 1,
    ]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, stabilityWindowMinutes },
          ],
        }).success,
        false
      )
    }
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
    for (const jitterSlowThresholdSeconds of [0, 1.5, 60]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterSlowThresholdSeconds },
          ],
        }).success,
        true
      )
    }
    for (const jitterSlowThresholdSeconds of [-1, 61]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, jitterSlowThresholdSeconds },
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
      true
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
    for (const fastFailureSameChannelRetryCount of [0, 10]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, fastFailureSameChannelRetryCount },
          ],
        }).success,
        true
      )
    }
    for (const fastFailureSameChannelRetryCount of [-1, 1.5, 11]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, fastFailureSameChannelRetryCount },
          ],
        }).success,
        false
      )
    }
    for (const fastFailureSameChannelRetryDelayMs of [0, 60_000]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, fastFailureSameChannelRetryDelayMs },
          ],
        }).success,
        true
      )
    }
    for (const fastFailureSameChannelRetryDelayMs of [-1, 1.5, 60_001]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, fastFailureSameChannelRetryDelayMs },
          ],
        }).success,
        false
      )
    }
    for (const burstFailureWindowMinutes of [1, 60]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, burstFailureWindowMinutes },
          ],
        }).success,
        true
      )
    }
    for (const burstFailureWindowMinutes of [0, 1.5, 61]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, burstFailureWindowMinutes },
          ],
        }).success,
        false
      )
    }
    for (const burstFailureWindowRequests of [1, 1_000]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, burstFailureWindowRequests },
          ],
        }).success,
        true
      )
    }
    for (const burstFailureWindowRequests of [0, 1.5, 1_001]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, burstFailureWindowRequests },
          ],
        }).success,
        false
      )
    }
    for (const burstFailureThresholdPercent of [0.1, 100]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, burstFailureThresholdPercent },
          ],
        }).success,
        true
      )
    }
    for (const burstFailureThresholdPercent of [0, 101]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, burstFailureThresholdPercent },
          ],
        }).success,
        false
      )
    }
    for (const field of [
      'consecutiveFailureThreshold',
      'recoverySuccessThreshold',
    ] as const) {
      for (const value of [1, 100]) {
        assert.equal(
          schema.safeParse({
            ...baseSettings,
            smartScheduleGroupPolicies: [{ ...groupPolicy, [field]: value }],
          }).success,
          true
        )
      }
      for (const value of [0, 1.5, 101]) {
        assert.equal(
          schema.safeParse({
            ...baseSettings,
            smartScheduleGroupPolicies: [{ ...groupPolicy, [field]: value }],
          }).success,
          false
        )
      }
    }
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [
          {
            ...groupPolicy,
            applyMode: 'weight',
            sampleMode: 'traffic',
            adaptiveSamplingEnabled: false,
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
            applyMode: 'weight',
            sampleMode: 'probe',
            probeIntervalMinutes: 15,
            adaptiveSamplingEnabled: false,
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
    for (const explorationMaxPromptKTokens of [0, 1, 50, 1_000]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, explorationMaxPromptKTokens },
          ],
        }).success,
        true
      )
    }
    for (const explorationMaxPromptKTokens of [-1, 1_001, 1.5]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, explorationMaxPromptKTokens },
          ],
        }).success,
        false
      )
    }
    for (const stabilityReleaseMaxPromptKTokens of [0, 1, 50, 1_000]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, stabilityReleaseMaxPromptKTokens },
          ],
        }).success,
        true
      )
    }
    for (const stabilityReleaseMaxPromptKTokens of [-1, 1_001, 1.5]) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [
            { ...groupPolicy, stabilityReleaseMaxPromptKTokens },
          ],
        }).success,
        false
      )
    }
    for (const samplingOrder of ['priority_weight', 'ratio'] as const) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [{ ...groupPolicy, samplingOrder }],
        }).success,
        true
      )
    }
    for (const samplingOrder of ['', 'weight', 'priority'] as const) {
      assert.equal(
        schema.safeParse({
          ...baseSettings,
          smartScheduleGroupPolicies: [{ ...groupPolicy, samplingOrder }],
        }).success,
        false
      )
    }
    const incompletePolicy: Record<string, unknown> = { ...groupPolicy }
    delete incompletePolicy.samplingOrder
    assert.equal(
      schema.safeParse({
        ...baseSettings,
        smartScheduleGroupPolicies: [incompletePolicy],
      }).success,
      false
    )
  })
})
