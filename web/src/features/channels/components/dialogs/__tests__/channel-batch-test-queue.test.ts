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

import { runChannelBatchTestQueue } from '../channel-batch-test-queue'

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
}

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve
  })
  return { promise, resolve }
}

describe('channel batch test queue', () => {
  test('publishes each completed result before slower requests finish', async () => {
    const slow = createDeferred<string>()
    const fast = createDeferred<string>()
    const completed: string[] = []
    let queueFinished = false

    const queue = runChannelBatchTestQueue(['slow', 'fast'], {
      concurrency: 2,
      shouldStop: () => false,
      runTask: (task) => (task === 'slow' ? slow.promise : fast.promise),
      onTaskStart: () => {},
      onTaskComplete: (result) => completed.push(result),
    }).then(() => {
      queueFinished = true
    })

    fast.resolve('fast')
    await Promise.resolve()

    assert.deepEqual(completed, ['fast'])
    assert.equal(queueFinished, false)

    slow.resolve('slow')
    await queue

    assert.deepEqual(completed, ['fast', 'slow'])
  })

  test('starts the next task when any concurrency slot becomes available', async () => {
    const first = createDeferred<string>()
    const second = createDeferred<string>()
    const third = createDeferred<string>()
    const deferred = new Map([
      ['first', first],
      ['second', second],
      ['third', third],
    ])
    const started: string[] = []
    const completed: string[] = []

    const queue = runChannelBatchTestQueue(['first', 'second', 'third'], {
      concurrency: 2,
      shouldStop: () => false,
      runTask: (task) => {
        started.push(task)
        const pending = deferred.get(task)
        if (!pending) throw new Error(`Missing deferred task: ${task}`)
        return pending.promise
      },
      onTaskStart: () => {},
      onTaskComplete: (result) => completed.push(result),
    })

    assert.deepEqual(started, ['first', 'second'])

    second.resolve('second')
    await Promise.resolve()
    assert.deepEqual(started, ['first', 'second', 'third'])

    first.resolve('first')
    third.resolve('third')
    await queue

    assert.deepEqual(completed, ['second', 'first', 'third'])
  })

  test('does not start queued tasks after a stop request', async () => {
    const first = createDeferred<string>()
    const second = createDeferred<string>()
    const third = createDeferred<string>()
    const deferred = new Map([
      ['first', first],
      ['second', second],
      ['third', third],
    ])
    const started: string[] = []
    let stopped = false

    const queue = runChannelBatchTestQueue(['first', 'second', 'third'], {
      concurrency: 2,
      shouldStop: () => stopped,
      runTask: (task) => {
        started.push(task)
        const pending = deferred.get(task)
        if (!pending) throw new Error(`Missing deferred task: ${task}`)
        return pending.promise
      },
      onTaskStart: () => {},
      onTaskComplete: () => {},
    })

    stopped = true
    first.resolve('first')
    second.resolve('second')
    await queue

    assert.deepEqual(started, ['first', 'second'])
  })
})
