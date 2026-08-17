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

import { renderToStaticMarkup } from 'react-dom/server'

import { Tabs } from '@/components/ui/tabs'

import { ChannelMonitorViewTabs } from '../channel-monitor-view-tabs'

describe('渠道监控视图导航', () => {
  test('按渠道、智能调度、状态监测、模型检测、分组、模型性能的顺序展示', () => {
    const markup = renderToStaticMarkup(
      <Tabs defaultValue='channels'>
        <ChannelMonitorViewTabs
          channelCount={12}
          groupCount={3}
          performanceModelCount={5}
          smartSchedulePoolCount={4}
          smartScheduleHasCriticalIssue={false}
          smartScheduleHasProbing={false}
        />
      </Tabs>
    )
    const labels = [
      '渠道 12',
      '智能调度 4',
      '状态监测',
      '模型检测',
      '分组 3',
      '模型性能 5',
    ]
    let previousIndex = -1

    for (const label of labels) {
      const index = markup.indexOf(label)
      assert.ok(index > previousIndex, `${label} 应按指定顺序展示`)
      previousIndex = index
    }
  })
})
