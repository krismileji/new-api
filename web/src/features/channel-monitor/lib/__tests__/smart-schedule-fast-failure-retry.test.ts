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

import { createChannelMonitorSmartSchedulePolicySchema } from '../schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
  channelMonitorSmartScheduleGroupPoliciesToApi,
} from '../smart-schedule-group-policy'

describe('fast failure retry policy', () => {
  test.each([
    { stabilityEnabled: true, seconds: 2.5, count: 2, delay: 750 },
    { stabilityEnabled: false, seconds: 2.5, count: 2, delay: 750 },
    { stabilityEnabled: false, seconds: 1, count: 0, delay: 0 },
    { stabilityEnabled: false, seconds: 59.9, count: 10, delay: 60_000 },
  ])(
    'preserves retry settings in the API payload with stability=$stabilityEnabled, threshold=$seconds, count=$count, delay=$delay',
    ({ stabilityEnabled, seconds, count, delay }) => {
      const policy = createChannelMonitorSmartSchedulePolicySchema().parse({
        ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
        stabilityEnabled,
        fastFailureSeconds: seconds,
        fastFailureSameChannelRetryCount: count,
        fastFailureSameChannelRetryDelayMs: delay,
      })
      const [payload] = channelMonitorSmartScheduleGroupPoliciesToApi([
        { ...policy, group: 'vip' },
      ])

      expect(payload).toMatchObject({
        stability_enabled: stabilityEnabled,
        fast_failure_seconds: seconds,
        fast_failure_same_channel_retry_count: count,
        fast_failure_same_channel_retry_delay_ms: delay,
      })
      expect(payload.slow_failure_seconds).toBeGreaterThan(seconds)
      expect(payload.slow_failure_seconds).toBeLessThanOrEqual(60)
    }
  )

  test.each([
    ['fastFailureSeconds', ''],
    ['fastFailureSeconds', 0],
    ['fastFailureSeconds', 60],
    ['fastFailureSameChannelRetryCount', ''],
    ['fastFailureSameChannelRetryCount', -1],
    ['fastFailureSameChannelRetryCount', 1.5],
    ['fastFailureSameChannelRetryCount', 11],
    ['fastFailureSameChannelRetryDelayMs', ''],
    ['fastFailureSameChannelRetryDelayMs', -1],
    ['fastFailureSameChannelRetryDelayMs', 0.5],
    ['fastFailureSameChannelRetryDelayMs', 60_001],
  ])(
    'rejects invalid %s=%s even when stability protection is disabled',
    (field, value) => {
      const result = createChannelMonitorSmartSchedulePolicySchema().safeParse({
        ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
        stabilityEnabled: false,
        [field]: value,
      })

      expect(result.success).toBe(false)
      if (result.success) return
      expect(result.error.issues).toEqual(
        expect.arrayContaining([expect.objectContaining({ path: [field] })])
      )
    }
  )
})
