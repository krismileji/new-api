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

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionOverview,
} from '../../types-model-detection'
import { domWindow } from './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelModelDetectionView } =
  await import('../channel-model-detection-view')

function channel(
  id: number,
  name: string,
  groups: string[],
  models: string[],
  configured = true
): ChannelModelDetectionChannel {
  return {
    id,
    name,
    type: 1,
    channel_status: 1,
    remark: '',
    groups,
    cost_ratio: null,
    supported_models: models,
    health_status: configured ? 'pending' : 'unconfigured',
    config: configured
      ? {
          channel_id: id,
          schedule_enabled: id === 1,
          revision: 1,
          created_at: 1,
          updated_at: 1,
        }
      : null,
    active_run: null,
    targets: configured
      ? [
          {
            target_key: `target-${id}`,
            request_model: models[0],
            claimed_model: 'gpt-5.6-sol',
            enabled: true,
            position: 0,
            latest: null,
            recent_window: [],
          },
        ]
      : [],
    latest_run_cost: null,
  }
}

const overview: ChannelModelDetectionOverview = {
  server_now: 1_700_000_000,
  settings: {
    detector_url_configured: true,
    detector_url_masked: 'http://gpt-check:8080',
    scheduled_preset: 'medium',
    schedule_enabled: false,
    interval_minutes: 1440,
    display_value: 30,
    display_unit: 'day',
    next_batch_at: 0,
    revision: 1,
  },
  detector: {
    state: 'available',
    detector_url_configured: true,
    detector_url_masked: 'http://gpt-check:8080',
    busy: false,
    active_session_owned: false,
    deployment_id: null,
    last_checked_at: 1_700_000_000,
    last_error: '',
    compatibility_message: '',
    estimates: {},
  },
  summary: {
    unconfigured: 1,
    paused: 0,
    pending: 2,
    running: 0,
    healthy: 0,
    attention: 0,
    unhealthy: 0,
    detector_unavailable: 0,
    stale: 0,
  },
  groups: ['default', 'vip'],
  models: ['model-a', 'model-b', 'model-c'],
  models_by_group: {
    default: ['model-a'],
    vip: ['model-b', 'model-c'],
  },
  channels: [
    channel(1, '默认渠道', ['default'], ['model-a']),
    channel(2, 'VIP 渠道', ['vip'], ['model-b', 'model-c']),
    channel(3, '未配置渠道', ['default'], ['model-a'], false),
  ],
}

async function chooseOption(trigger: HTMLButtonElement, label: string) {
  await act(async () => {
    trigger.focus()
    trigger.dispatchEvent(
      new domWindow.KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      }) as unknown as KeyboardEvent
    )
  })
  const option = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ].find((candidate) => candidate.textContent?.trim() === label)
  assert.ok(option)
  await act(async () => option.click())
}

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)

try {
  await act(async () => {
    root.render(<ChannelModelDetectionView overview={overview} />)
  })

  const onlyEnabled = container.querySelector<HTMLElement>(
    '[aria-label="仅展示已启用的模型检测卡片"]'
  )
  assert.ok(onlyEnabled)
  assert.equal(onlyEnabled.getAttribute('aria-checked'), 'true')
  assert.ok(container.textContent?.includes('默认渠道'))
  assert.equal(container.textContent?.includes('VIP 渠道'), false)
  assert.equal(container.textContent?.includes('未配置渠道'), false)
  await act(async () => onlyEnabled.click())
  assert.equal(onlyEnabled.getAttribute('aria-checked'), 'false')
  assert.ok(container.textContent?.includes('VIP 渠道'))
  assert.ok(container.textContent?.includes('未配置渠道'))

  const groupTrigger = container.querySelector<HTMLButtonElement>(
    '[aria-label="选择模型检测分组"]'
  )
  const modelTrigger = container.querySelector<HTMLButtonElement>(
    '[aria-label="选择模型检测请求模型"]'
  )
  assert.ok(groupTrigger)
  assert.ok(modelTrigger)

  await chooseOption(groupTrigger, 'vip')
  assert.ok(groupTrigger.textContent?.includes('vip'))
  assert.equal(container.textContent?.includes('默认渠道'), false)
  assert.ok(container.textContent?.includes('VIP 渠道'))

  await act(async () => {
    modelTrigger.focus()
    modelTrigger.dispatchEvent(
      new domWindow.KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      }) as unknown as KeyboardEvent
    )
  })
  const vipModelOptions = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ]
  assert.deepEqual(
    vipModelOptions.map((option) => option.textContent?.trim()),
    ['全部模型', 'model-b', 'model-c']
  )
  const modelB = vipModelOptions.find(
    (option) => option.textContent?.trim() === 'model-b'
  )
  assert.ok(modelB)
  await act(async () => modelB.click())
  assert.ok(modelTrigger.textContent?.includes('model-b'))
  assert.ok(container.textContent?.includes('VIP 渠道'))

  await chooseOption(groupTrigger, 'default')
  assert.ok(modelTrigger.textContent?.includes('全部模型'))
  await act(async () => {
    modelTrigger.focus()
    modelTrigger.dispatchEvent(
      new domWindow.KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      }) as unknown as KeyboardEvent
    )
  })
  const defaultModelOptions = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ]
  assert.deepEqual(
    defaultModelOptions.map((option) => option.textContent?.trim()),
    ['全部模型', 'model-a']
  )
} finally {
  await act(async () => root.unmount())
  container.remove()
}

process.stdout.write('ok\n')
