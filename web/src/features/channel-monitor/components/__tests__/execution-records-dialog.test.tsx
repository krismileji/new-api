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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import type { ChannelMonitorTask } from '../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'CustomEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}
Object.defineProperty(domWindow.Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { ChannelMonitorTaskHistoryDialog } =
  await import('../channel-monitor-task-history-dialog')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function createTask(
  type: ChannelMonitorTask['type'],
  taskId: string
): ChannelMonitorTask {
  return {
    id: 1,
    task_id: taskId,
    type,
    status: 'succeeded',
    state: null,
    result:
      type === 'channel_smart_schedule'
        ? {
            total: 1,
            updated: 1,
            unchanged: 0,
            skipped: 0,
            failed: 0,
            performance_minutes: 60,
            adjustments: [
              {
                channel_id: 7,
                channel_name: '高速稳定渠道',
                group: 'default',
                model: 'gpt-5',
                action: 'updated',
                old_priority: 80,
                new_priority: 100,
                old_weight: 20,
                new_weight: 90,
                score: 0.96,
                reason: '综合评分最高，提升为主渠道',
              },
            ],
          }
        : {
            total: 4,
            updated: 4,
            changed: 2,
            balance_updated: 3,
            failed: 0,
          },
    error: '',
    created_at: 1_752_777_845,
    updated_at: 1_752_777_848,
  }
}

describe('channel monitor execution records dialog', () => {
  test('uses one record center with clear ratio and smart schedule views', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(['channel-monitor-task-history', 'ratio', 1, 20], {
      success: true,
      message: '',
      data: {
        page: 1,
        page_size: 20,
        total: 1,
        items: [createTask('channel_ratio_monitor', 'ratio-1')],
      },
    })
    queryClient.setQueryData(['channel-monitor-smart-schedule-executions', 1], {
      success: true,
      message: '',
      data: {
        page: 1,
        page_size: 20,
        total: 1,
        items: [createTask('channel_smart_schedule', 'schedule-1')],
      },
    })

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <ChannelMonitorTaskHistoryDialog
            initialKind='ratio'
            open
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      )
    })

    const dialog = document.body.querySelector<HTMLElement>(
      '[data-slot="dialog-content"]'
    )
    assert.ok(dialog)
    assert.ok(dialog.textContent?.includes('执行记录'))
    assert.equal(dialog.querySelectorAll('[role="tab"]').length, 2)
    assert.ok(dialog.textContent?.includes('倍率与余额更新'))
    assert.ok(dialog.textContent?.includes('智能调度'))
    assert.ok(dialog.textContent?.includes('当前页'))
    assert.ok(dialog.textContent?.includes('联动操作'))

    const ratioTable = dialog.querySelector<HTMLElement>('[data-slot="table"]')
    const ratioTableHeader = dialog.querySelector<HTMLElement>(
      '[data-slot="table-header"]'
    )
    const ratioTaskRow = dialog.querySelector<HTMLElement>(
      '[data-slot="table-body"] > [data-slot="table-row"]'
    )
    assert.ok(ratioTable)
    assert.ok(ratioTable.className.includes('min-w-0'))
    assert.ok(ratioTable.className.includes('sm:min-w-[920px]'))
    assert.ok(ratioTableHeader?.className.includes('hidden'))
    assert.ok(ratioTableHeader?.className.includes('sm:table-header-group'))
    assert.ok(ratioTaskRow?.className.includes('grid'))
    assert.ok(ratioTaskRow?.className.includes('sm:table-row'))
    assert.ok(ratioTaskRow?.textContent?.includes('执行结果'))
    assert.ok(ratioTaskRow?.textContent?.includes('联动操作'))

    const initialButtons = [...dialog.querySelectorAll('button')]
    assert.ok(
      initialButtons.some((button) =>
        button.textContent?.includes('立即更新倍率和余额')
      )
    )
    assert.equal(
      initialButtons.some((button) =>
        button.textContent?.includes('执行智能调度')
      ),
      false
    )

    const scheduleTab = [
      ...dialog.querySelectorAll<HTMLElement>('[role="tab"]'),
    ].find((tab) => tab.textContent?.includes('智能调度'))
    assert.ok(scheduleTab)
    await act(async () => scheduleTab.click())

    const scheduleButtons = [...dialog.querySelectorAll('button')]
    assert.ok(
      scheduleButtons.some((button) =>
        button.textContent?.includes('执行智能调度')
      )
    )
    assert.equal(
      scheduleButtons.some((button) =>
        button.textContent?.includes('立即更新倍率和余额')
      ),
      false
    )
    assert.ok(dialog.textContent?.includes('执行批次（共 1 条）'))
    assert.ok(dialog.textContent?.includes('智能调度执行详情'))
    assert.ok(dialog.textContent?.includes('高速稳定渠道'))
    assert.ok(dialog.textContent?.includes('综合评分最高，提升为主渠道'))

    await act(async () => root.unmount())
    container.remove()
    queryClient.clear()
  })
})

after(() => {
  domWindow.close()
})
