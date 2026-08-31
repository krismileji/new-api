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

import { test } from 'vitest'

import { runChannelMonitorBatchExecution } from '../batch-execution'

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason: unknown) => void
}

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, resolve, reject }
}

test('批量监测同时启动全部渠道并按渠道顺序保留成功与失败结果', async () => {
  const first = createDeferred<string>()
  const second = createDeferred<string>()
  const third = createDeferred<string>()
  const deferred = new Map([
    [1, first],
    [2, second],
    [3, third],
  ])
  const started: number[] = []

  const execution = runChannelMonitorBatchExecution([1, 2, 3], (item) => {
    started.push(item)
    const pending = deferred.get(item)
    if (!pending) throw new Error(`缺少渠道 ${item} 的测试任务`)
    return pending.promise
  })

  assert.deepEqual(started, [1, 2, 3])
  second.reject(new Error('渠道 2 提交失败'))
  first.resolve('渠道 1 已提交')
  third.resolve('渠道 3 已提交')

  const results = await execution
  assert.deepEqual(
    results.map((result) => ({ item: result.item, status: result.status })),
    [
      { item: 1, status: 'fulfilled' },
      { item: 2, status: 'rejected' },
      { item: 3, status: 'fulfilled' },
    ]
  )
})
