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

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, test } from 'vitest'

import { CHANNEL_TEST_DEFAULTS } from '../../../lib/channel-actions'
import { ChannelTestSampleToggle } from '../channel-test-sample-toggle'

function renderSampleToggle(checked: boolean) {
  return renderToStaticMarkup(
    <ChannelTestSampleToggle
      id='test-record-sample'
      checked={checked}
      onCheckedChange={() => undefined}
    />
  )
}

describe('channel test sample toggle', () => {
  test('renders the shared default as enabled and clearly explains eligibility', () => {
    const markup = renderSampleToggle(CHANNEL_TEST_DEFAULTS.recordSample)

    assert.equal(CHANNEL_TEST_DEFAULTS.recordSample, true)
    assert.ok(markup.includes('aria-checked="true"'))
    assert.ok(markup.includes('已开启'))
    assert.ok(markup.includes('渠道模型参与智能调度时'))
  })

  test('renders an explicit disabled choice as not recording samples', () => {
    const markup = renderSampleToggle(false)

    assert.ok(markup.includes('aria-checked="false"'))
    assert.ok(markup.includes('已关闭'))
  })
})
