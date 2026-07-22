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
  createBatchTestChannelOption,
  getChannelsSupportingModels,
  getSelectableChannelIds,
  getSelectableModelNames,
  retainCompatibleChannelIds,
} from '../channel-batch-test-selection'

const channels = [
  { id: 1, status: 1, models: 'gpt-4o, claude-3-5-sonnet' },
  { id: 2, status: 0, models: 'gpt-4o-mini' },
  { id: 3, status: 1, models: '' },
  { id: 4, status: 1 },
]

describe('channel batch test selection', () => {
  test('shows the configured channel remark in selection text', () => {
    assert.deepEqual(
      createBatchTestChannelOption({
        id: 8,
        name: '主渠道',
        remark: '  仅用于生产流量  ',
      }),
      {
        value: '8',
        label: '#8 主渠道 · 备注：仅用于生产流量',
        channelLabel: '#8 主渠道',
        remark: '仅用于生产流量',
      }
    )
  })

  test('uses the channel remark field supplied by the monitor view', () => {
    assert.equal(
      createBatchTestChannelOption({
        id: 9,
        name: '备用渠道',
        remark: '倍率调整记录',
        channel_remark: '低峰期使用',
      }).label,
      '#9 备用渠道 · 备注：低峰期使用'
    )
  })

  test('does not treat a monitor ratio remark as a channel remark', () => {
    assert.equal(
      createBatchTestChannelOption({
        id: 10,
        name: '测试渠道',
        remark: '倍率调整记录',
        channel_remark: '',
      }).label,
      '#10 测试渠道'
    )
  })

  test('only offers priced models configured by at least one channel', () => {
    assert.deepEqual(
      getSelectableModelNames(
        ['missing-model', 'gpt-4o-mini', 'gpt-4o', 'gpt-4o'],
        channels
      ),
      ['gpt-4o-mini', 'gpt-4o']
    )
  })

  test('matches complete model names instead of substrings', () => {
    assert.deepEqual(
      getChannelsSupportingModels(
        [{ id: 1, status: 1, models: 'gpt-4o-mini' }],
        ['gpt-4o']
      ),
      []
    )
  })

  test('requires channels to support every selected model', () => {
    assert.deepEqual(
      getChannelsSupportingModels(channels, [
        'gpt-4o',
        'claude-3-5-sonnet',
      ]).map((channel) => channel.id),
      [1]
    )
  })

  test('does not offer channels with missing or empty model configuration', () => {
    assert.deepEqual(
      getChannelsSupportingModels(
        [
          { id: 3, status: 1, models: '' },
          { id: 4, status: 1 },
        ],
        ['gpt-4o']
      ),
      []
    )
  })

  test('does not offer channels before a model is selected', () => {
    assert.deepEqual(getChannelsSupportingModels(channels, []), [])
  })

  test('removes selected channels that no longer support the model', () => {
    const compatibleChannels = getChannelsSupportingModels(channels, [
      'gpt-4o-mini',
    ])

    assert.deepEqual(
      retainCompatibleChannelIds(['1', '2', '4'], compatibleChannels),
      ['2']
    )
  })

  test('select all stays within compatible channels and can exclude disabled channels', () => {
    const compatibleChannels = getChannelsSupportingModels(channels, [
      'gpt-4o-mini',
    ])

    assert.deepEqual(getSelectableChannelIds(compatibleChannels, 'all'), ['2'])
    assert.deepEqual(getSelectableChannelIds(compatibleChannels, 'enabled'), [])
  })
})
