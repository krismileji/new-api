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

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionHealth,
  ChannelModelDetectionOverview,
} from '../../types-model-detection'
import { domWindow } from './test-dom'

const { renderToStaticMarkup } = await import('react-dom/server')
const { ChannelModelDetectionView } =
  await import('../channel-model-detection-view')
const CHANNEL_ORDER = [3, 2, 1]

function createChannel(
  id: number,
  health: ChannelModelDetectionHealth
): ChannelModelDetectionChannel {
  return {
    id,
    name: `渠道 ${id}`,
    type: 1,
    channel_status: 1,
    remark: id === 2 ? '备用线路' : '主线路',
    groups: id === 2 ? ['vip'] : ['default'],
    cost_ratio: null,
    supported_models: ['gpt-5.6'],
    health_status: health,
    config: null,
    active_run: null,
    targets: [],
    latest_run_cost: null,
  }
}

function createOverview(): ChannelModelDetectionOverview {
  const channels = [
    createChannel(1, 'unconfigured'),
    createChannel(2, 'healthy'),
    createChannel(3, 'attention'),
  ]
  return {
    server_now: 1_754_000_000,
    snapshot_version: 1,
    snapshot_revision: 1,
    event_watermark: 1,
    generated_at: 1_754_000_000,
    data_cutoff_at: 1_753_999_999,
    snapshot_age_seconds: 0,
    stale: false,
    settings: {
      detector_url_configured: true,
      detector_url_masked: 'http://127.0.0.1:8000',
      scheduled_preset: 'medium',
      schedule_enabled: true,
      interval_minutes: 1440,
      display_value: 30,
      display_unit: 'day',
      next_batch_at: 1_754_086_400,
      revision: 1,
    },
    detector: {
      state: 'available',
      detector_url_configured: true,
      detector_url_masked: 'http://127.0.0.1:8000',
      busy: false,
      active_session_owned: false,
      deployment_id: null,
      last_checked_at: 1_753_999_940,
      last_error: '',
      compatibility_message: '',
      estimates: {},
    },
    summary: {
      unconfigured: 1,
      paused: 0,
      pending: 0,
      running: 0,
      healthy: 1,
      attention: 1,
      unhealthy: 0,
      detector_unavailable: 0,
      stale: 0,
    },
    groups: ['default', 'vip'],
    models: ['gpt-5.6'],
    models_by_group: { default: ['gpt-5.6'], vip: ['gpt-5.6'] },
    channels,
  }
}

describe('模型检测视图骨架', () => {
  test('总览为每个渠道恰好渲染一张卡片并保持 360px 单列契约', () => {
    domWindow.document.body.innerHTML = renderToStaticMarkup(
      <ChannelModelDetectionView
        channelOrder={CHANNEL_ORDER}
        overview={createOverview()}
        onRefresh={() => {}}
        filters={{
          status: 'all',
          group: '',
          model: '',
          search: '',
          onlyEnabled: false,
        }}
      />
    )

    const cards = domWindow.document.querySelectorAll(
      '[data-testid="channel-model-detection-card"]'
    )
    const grid = domWindow.document.querySelector(
      '[data-slot="model-detection-card-grid"]'
    )
    const controls = domWindow.document.querySelector(
      '[data-slot="model-detection-filter-controls"]'
    )
    assert.equal(cards.length, 3)
    assert.ok(grid)
    assert.match(grid.className, /grid-cols-1/)
    assert.match(grid.className, /md:grid-cols-2/)
    assert.match(grid.className, /xl:grid-cols-3/)
    assert.match(grid.className, /min-w-0/)
    assert.ok(controls)
    assert.match(controls.className, /flex-col/)
    assert.match(controls.className, /sm:flex-row/)
    for (const label of [
      '选择模型检测分组',
      '选择模型检测请求模型',
      '搜索模型检测渠道',
    ]) {
      const control = domWindow.document.querySelector(
        `[aria-label="${label}"]`
      )
      assert.ok(control)
      assert.match(control.className, /w-full/)
      assert.match(control.className, /min-w-0/)
    }
  })

  test('卡片遵循渠道视图顺序且不再提供独立排序控件', () => {
    const overview = createOverview()
    overview.channels = overview.channels.map((channel) => ({
      ...channel,
      config: {
        channel_id: channel.id,
        schedule_enabled: false,
        revision: 1,
        created_at: 1,
        updated_at: 1,
      },
      targets: [
        {
          target_key: `target-${channel.id}`,
          request_model: 'gpt-5.6',
          claimed_model: 'gpt-5.6-sol',
          enabled: true,
          position: 0,
          latest: null,
          recent_window: [],
        },
      ],
    }))
    overview.channels[0] = {
      ...overview.channels[0],
      name: '高倍率渠道',
      cost_ratio: 3,
    } as ChannelModelDetectionChannel
    overview.channels[1] = {
      ...overview.channels[1],
      name: '低倍率渠道',
      cost_ratio: 0.5,
    } as ChannelModelDetectionChannel
    overview.channels[2] = {
      ...overview.channels[2],
      name: '未知倍率渠道',
      cost_ratio: null,
    } as ChannelModelDetectionChannel

    domWindow.document.body.innerHTML = renderToStaticMarkup(
      <ChannelModelDetectionView
        channelOrder={[3, 1, 2]}
        overview={overview}
        filters={{
          status: 'all',
          group: '',
          model: '',
          search: '',
          onlyEnabled: false,
        }}
      />
    )

    const cards = [
      ...domWindow.document.querySelectorAll(
        '[data-testid="channel-model-detection-card"]'
      ),
    ]
    assert.deepEqual(
      cards.map(
        (card) => card.textContent?.match(/(?:高|低|未知)倍率渠道/)?.[0]
      ),
      ['未知倍率渠道', '高倍率渠道', '低倍率渠道']
    )
    assert.equal(
      domWindow.document.querySelector('[aria-label="模型检测卡片排序方式"]'),
      null
    )
  })

  test('离线服务、加载、错误、空数据和筛选无结果状态彼此独立', () => {
    const offline = createOverview()
    offline.detector.state = 'offline'
    offline.detector.last_error = '连接被拒绝'
    const offlineHtml = renderToStaticMarkup(
      <ChannelModelDetectionView
        channelOrder={CHANNEL_ORDER}
        overview={offline}
      />
    )
    assert.match(offlineHtml, /官方检测器离线/)
    assert.match(offlineHtml, /连接被拒绝/)
    assert.match(offlineHtml, /data-detector-state="offline"/)

    const unchecked = createOverview()
    unchecked.detector.state = 'unknown'
    const uncheckedHtml = renderToStaticMarkup(
      <ChannelModelDetectionView
        channelOrder={CHANNEL_ORDER}
        overview={unchecked}
      />
    )
    assert.match(uncheckedHtml, /官方检测器尚未检查/)
    assert.doesNotMatch(uncheckedHtml, /检测器不可用/)

    assert.match(
      renderToStaticMarkup(
        <ChannelModelDetectionView channelOrder={CHANNEL_ORDER} loading />
      ),
      /正在加载模型检测数据/
    )
    assert.match(
      renderToStaticMarkup(
        <ChannelModelDetectionView
          channelOrder={CHANNEL_ORDER}
          error='请求超时'
        />
      ),
      /模型检测数据加载失败/
    )

    const empty = createOverview()
    empty.channels = []
    assert.match(
      renderToStaticMarkup(
        <ChannelModelDetectionView
          channelOrder={CHANNEL_ORDER}
          overview={empty}
        />
      ),
      /暂无渠道/
    )

    assert.match(
      renderToStaticMarkup(
        <ChannelModelDetectionView
          channelOrder={CHANNEL_ORDER}
          overview={createOverview()}
          filters={{
            status: 'healthy',
            group: '',
            model: '',
            search: '不存在',
            onlyEnabled: false,
          }}
        />
      ),
      /没有匹配的渠道/
    )
  })

  test('没有已配置渠道时禁用启用所有和暂停所有按钮', () => {
    domWindow.document.body.innerHTML = renderToStaticMarkup(
      <ChannelModelDetectionView
        channelOrder={CHANNEL_ORDER}
        overview={createOverview()}
        onEnableAll={() => {}}
        onPauseAll={() => {}}
      />
    )

    const enableAll = domWindow.document.querySelector(
      '[aria-label="启用所有模型定时检测"]'
    ) as HTMLButtonElement | null
    const pauseAll = domWindow.document.querySelector(
      '[aria-label="暂停所有模型定时检测"]'
    ) as HTMLButtonElement | null
    assert.ok(enableAll)
    assert.equal(enableAll.disabled, true)
    assert.ok(pauseAll)
    assert.equal(pauseAll.disabled, true)
  })
})
