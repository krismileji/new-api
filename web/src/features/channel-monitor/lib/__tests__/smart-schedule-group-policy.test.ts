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

import type { ChannelMonitorSmartSchedulePolicyFormValues } from '../schema'
import {
  channelMonitorSmartScheduleGroupPoliciesToApi,
  channelMonitorSmartScheduleGroupPoliciesToForm,
  createChannelMonitorSmartScheduleGroupPolicy,
  resolveChannelMonitorSmartScheduleGroupPolicy,
} from '../smart-schedule-group-policy'

const defaultPolicy: ChannelMonitorSmartSchedulePolicyFormValues = {
  strategy: 'smart',
  stabilityEnabled: true,
  scoring: {
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
  applyMode: 'weight',
  models: ['model-a'],
  minSamples: 5,
  minSuccessRate: 80,
  cooldownMinutes: 30,
}

describe('smart schedule group policy', () => {
  test('resolves omitted fields from the current default policy', () => {
    const effective = resolveChannelMonitorSmartScheduleGroupPolicy(
      defaultPolicy,
      {
        strategy: 'ratio',
        stabilityEnabled: false,
      }
    )

    assert.equal(effective.strategy, 'ratio')
    assert.equal(effective.stabilityEnabled, false)
    assert.equal(effective.applyMode, 'weight')
    assert.deepEqual(effective.models, ['model-a'])
    assert.equal(effective.minSuccessRate, 80)
  })

  test('creates a complete independent policy even when values match defaults', () => {
    assert.deepEqual(
      createChannelMonitorSmartScheduleGroupPolicy('vip', defaultPolicy),
      {
        group: 'vip',
        ...defaultPolicy,
      }
    )

    assert.deepEqual(
      createChannelMonitorSmartScheduleGroupPolicy('standard', {
        ...defaultPolicy,
        strategy: 'ratio',
        stabilityEnabled: false,
        models: [],
      }),
      {
        group: 'standard',
        ...defaultPolicy,
        strategy: 'ratio',
        stabilityEnabled: false,
        models: [],
      }
    )
  })

  test('keeps a saved group policy unchanged after defaults change', () => {
    const savedPolicy = createChannelMonitorSmartScheduleGroupPolicy(
      'vip',
      defaultPolicy
    )
    const effective = resolveChannelMonitorSmartScheduleGroupPolicy(
      {
        ...defaultPolicy,
        strategy: 'ratio',
        minSuccessRate: 95,
        models: ['model-b'],
      },
      savedPolicy
    )

    assert.deepEqual(effective, defaultPolicy)
  })

  test('fills legacy partial policies from defaults when loading the form', () => {
    const formPolicies = channelMonitorSmartScheduleGroupPoliciesToForm(
      [
        {
          group: 'vip',
          strategy: 'ratio',
          stability_enabled: false,
          models: [],
        },
      ],
      defaultPolicy
    )

    assert.deepEqual(formPolicies[0], {
      group: 'vip',
      ...defaultPolicy,
      strategy: 'ratio',
      stabilityEnabled: false,
      models: [],
    })
  })

  test('preserves a complete independent policy across API mapping', () => {
    const formPolicies = channelMonitorSmartScheduleGroupPoliciesToForm(
      [
        {
          group: 'vip',
          stability_enabled: false,
          models: [],
        },
      ],
      defaultPolicy
    )
    const apiPolicies =
      channelMonitorSmartScheduleGroupPoliciesToApi(formPolicies)

    assert.equal(formPolicies[0]?.stabilityEnabled, false)
    assert.deepEqual(formPolicies[0]?.models, [])
    assert.equal(apiPolicies[0]?.strategy, 'smart')
    assert.equal(apiPolicies[0]?.stability_enabled, false)
    assert.deepEqual(apiPolicies[0]?.models, [])
    assert.equal(apiPolicies[0]?.min_samples, 5)
    assert.equal(apiPolicies[0]?.min_success_rate, 80)
  })
})
