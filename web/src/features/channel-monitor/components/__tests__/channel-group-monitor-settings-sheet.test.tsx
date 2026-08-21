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

import type { ChannelGroupMonitorSettingsResponse } from '@/features/group-monitor/types'

import { ChannelGroupMonitorSettingsSheet } from '../channel-group-monitor-settings-sheet'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')

function settingsResponse(
  groups: ChannelGroupMonitorSettingsResponse['settings']['groups']
): ChannelGroupMonitorSettingsResponse {
  return {
    settings: {
      enabled: false,
      groups,
      interval_seconds: 60,
      display_value: 60,
      display_unit: 'minute',
      next_run_at: 0,
      manual_request_id: '',
      manual_requested_at: 0,
      revision: groups.length > 0 ? 1 : 0,
      running_trigger: '',
      running_run_id: '',
      running_started_at: 0,
      updated_at: 0,
    },
    candidate_models_by_group: {
      default: ['gpt-4.1'],
    },
  }
}

async function renderSheet(data: ChannelGroupMonitorSettingsResponse) {
  const queryClient = new QueryClient()
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <ChannelGroupMonitorSettingsSheet
          data={data}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
  })
  return { host, root, queryClient }
}

describe('分组监控配置', () => {
  test('首次配置时未保存分组不能提交立即探测请求', async () => {
    const rendered = await renderSheet(settingsResponse([]))
    const addGroupButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('添加分组')
    )
    assert.ok(addGroupButton)

    await act(async () => addGroupButton.click())

    const immediateProbeButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('立即探测')
    )
    assert.ok(immediateProbeButton)
    assert.equal(immediateProbeButton.disabled, true)

    await act(async () => rendered.root.unmount())
    rendered.queryClient.clear()
    rendered.host.remove()
  })

  test('已保存配置且没有未保存修改时允许立即探测', async () => {
    const rendered = await renderSheet(
      settingsResponse([{ group_name: 'default', probe_model: 'gpt-4.1' }])
    )
    const immediateProbeButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('立即探测')
    )
    assert.ok(immediateProbeButton)
    assert.equal(immediateProbeButton.disabled, false)

    await act(async () => rendered.root.unmount())
    rendered.queryClient.clear()
    rendered.host.remove()
  })
})
