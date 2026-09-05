import assert from 'node:assert/strict'

import { test } from 'vitest'

import { isChannelMonitorAnalyticsCoverageIncomplete } from '../coverage'

test('does not mark analytics coverage incomplete before an API response exists', () => {
  assert.equal(isChannelMonitorAnalyticsCoverageIncomplete(undefined), false)
})

test('marks explicitly partial analytics coverage as incomplete', () => {
  assert.equal(
    isChannelMonitorAnalyticsCoverageIncomplete({
      status: 'partial',
      covered_from: 1,
      covered_through: 2,
      reasons: ['processing_lag'],
    }),
    true
  )
})
