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
  formatChannelDefaultRoutingBatchConfirmation,
  formatChannelDefaultRoutingUpdateMessage,
  getChannelDefaultRoutingCopy,
} from '../channel-default-routing-copy'

describe('channel default routing copy', () => {
  test('identifies priority and weight as channel defaults', () => {
    assert.equal(getChannelDefaultRoutingCopy('priority').label, '默认优先级')
    assert.equal(getChannelDefaultRoutingCopy('weight').label, '默认权重')
  })

  test('explains that smart-scheduled route values are preserved', () => {
    const help = getChannelDefaultRoutingCopy('priority').help

    assert.match(help, /分组 \+ 模型/)
    assert.match(help, /保留各自的实际优先级/)
    assert.match(help, /取消对应路由参与智能调度/)
  })

  test('confirms a single-channel update without implying route overwrite', () => {
    assert.equal(
      formatChannelDefaultRoutingUpdateMessage('weight', 80),
      '渠道默认权重已更新为 80。参与智能调度的路由仍使用各自的实际权重。'
    )
  })

  test('names the affected tag when confirming a batch update', () => {
    assert.equal(
      formatChannelDefaultRoutingUpdateMessage('priority', 90, '生产'),
      '标签“生产”下的渠道默认优先级已更新为 90。参与智能调度的路由仍使用各自的实际优先级。'
    )
  })

  test('warns before a tag batch update that route values stay unchanged', () => {
    assert.equal(
      formatChannelDefaultRoutingBatchConfirmation('priority', 90, 3, '生产'),
      '将把标签“生产”下 3 个渠道的默认优先级更新为 90。参与智能调度的“分组 + 模型”路由仍保留各自的实际优先级，不会被本次更新覆盖。是否继续？'
    )
  })
})
