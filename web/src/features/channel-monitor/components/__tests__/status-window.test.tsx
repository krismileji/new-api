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

import { afterEach, describe, test } from 'vitest'

import { domWindow } from './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelMonitorStatusWindow, ChannelMonitorStatusWindowDetails } =
  await import('../channel-monitor-status-window')
const { formatChannelMonitorStatusWindowRange } =
  await import('../../lib/status-window')

type TestBucket = {
  started_at: number
  state: 'success' | 'idle'
}

const mounted: Array<{
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}> = []

async function waitForDetails(expectedText: string) {
  const current = document.body.querySelector<HTMLElement>(
    '[data-slot="channel-monitor-status-window-details"]'
  )
  if (current?.textContent?.includes(expectedText)) return current

  return new Promise<HTMLElement>((resolve, reject) => {
    const timeout = setTimeout(() => {
      observer.disconnect()
      reject(new Error(`状态格详情未显示：${expectedText}`))
    }, 2_000)
    const observer = new MutationObserver(() => {
      const details = document.body.querySelector<HTMLElement>(
        '[data-slot="channel-monitor-status-window-details"]'
      )
      if (!details?.textContent?.includes(expectedText)) return
      clearTimeout(timeout)
      observer.disconnect()
      resolve(details)
    })
    observer.observe(document.body, { childList: true, subtree: true })
  })
}

afterEach(async () => {
  for (const item of mounted.splice(0)) {
    await act(async () => item.root.unmount())
    item.container.remove()
  }
})

describe('渠道监测状态格详情', () => {
  test('时间范围同时覆盖同日和跨日边界', () => {
    const sameDayStartedAt = new Date(2025, 7, 1, 6, 13, 20).getTime() / 1_000
    const fullDayStartedAt = new Date(2025, 7, 1).getTime() / 1_000

    assert.equal(
      formatChannelMonitorStatusWindowRange(sameDayStartedAt, 'minute'),
      '2025-08-01 06:13:20 - 06:14:20'
    )
    assert.equal(
      formatChannelMonitorStatusWindowRange(fullDayStartedAt, 'day'),
      '2025-08-01 00:00:00 - 2025-08-02 00:00:00'
    )
  })

  test('悬停显示详情并可用方向键切换时间格', async () => {
    const buckets: TestBucket[] = [
      { started_at: 1_754_000_000, state: 'success' },
      { started_at: 1_754_000_060, state: 'idle' },
    ]
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    mounted.push({ container, root })

    await act(async () => {
      root.render(
        <ChannelMonitorStatusWindow
          buckets={buckets}
          bucketSlot='test-status-bucket'
          bucketStateDataAttribute='data-test-bucket-state'
          gridProps={{ 'aria-label': '测试状态时间格' }}
          getBucketPresentation={(bucket) => ({
            ariaLabel:
              bucket.state === 'success' ? '成功时间格' : '未执行时间格',
            className: bucket.state === 'success' ? 'bg-success' : 'bg-muted',
            state: bucket.state,
          })}
          renderDetails={(bucket) => (
            <ChannelMonitorStatusWindowDetails
              timeRange={formatChannelMonitorStatusWindowRange(
                bucket.started_at,
                'minute'
              )}
              status={bucket.state === 'success' ? '成功' : '未执行'}
              statusVariant={
                bucket.state === 'success' ? 'secondary' : 'outline'
              }
              description={
                bucket.state === 'idle' ? '本时间格内没有执行。' : undefined
              }
              details={
                bucket.state === 'success'
                  ? [
                      { label: '成功', value: 3 },
                      { label: '上游失败', value: 1 },
                    ]
                  : undefined
              }
            />
          )}
        />
      )
    })

    const triggers = container.querySelectorAll<HTMLElement>(
      '[data-channel-monitor-status-window-trigger]'
    )
    assert.equal(triggers.length, 2)
    assert.equal(triggers[0]?.tabIndex, -1)
    assert.equal(triggers[1]?.tabIndex, 0)

    await act(async () => {
      triggers[0]?.dispatchEvent(
        new domWindow.MouseEvent('mouseenter') as unknown as MouseEvent
      )
    })
    const successDetails = await waitForDetails('上游失败1')
    assert.match(successDetails.textContent ?? '', /时间范围/)
    assert.match(successDetails.textContent ?? '', /成功3/)

    await act(async () => triggers[0]?.focus())
    await act(async () => {
      triggers[0]?.dispatchEvent(
        new domWindow.KeyboardEvent('keydown', {
          key: 'ArrowRight',
          bubbles: true,
        }) as unknown as KeyboardEvent
      )
    })
    const idleDetails = await waitForDetails('本时间格内没有执行。')
    assert.equal(document.activeElement, triggers[1])
    assert.equal(triggers[0]?.tabIndex, -1)
    assert.equal(triggers[1]?.tabIndex, 0)
    assert.match(idleDetails.textContent ?? '', /未执行/)
  })
})
