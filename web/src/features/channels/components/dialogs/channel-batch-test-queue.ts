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

type ChannelBatchTestQueueOptions<TTask, TResult> = {
  concurrency: number
  shouldStop: () => boolean
  runTask: (task: TTask) => Promise<TResult>
  onTaskStart?: (task: TTask) => void
  onTaskComplete: (result: TResult) => void
}

export async function runChannelBatchTestQueue<TTask, TResult>(
  tasks: readonly TTask[],
  options: ChannelBatchTestQueueOptions<TTask, TResult>
): Promise<void> {
  if (tasks.length === 0) return

  const workerCount = Math.min(
    tasks.length,
    Math.max(1, Math.floor(options.concurrency))
  )
  let nextTaskIndex = 0

  const runWorker = async () => {
    while (!options.shouldStop()) {
      const taskIndex = nextTaskIndex
      nextTaskIndex += 1
      if (taskIndex >= tasks.length) return

      const task = tasks[taskIndex]
      options.onTaskStart?.(task)
      const result = await options.runTask(task)
      options.onTaskComplete(result)
    }
  }

  await Promise.all(Array.from({ length: workerCount }, () => runWorker()))
}
