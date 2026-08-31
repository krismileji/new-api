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

export type ChannelMonitorBatchExecutionResult<TItem, TValue> =
  | {
      item: TItem
      status: 'fulfilled'
      value: TValue
    }
  | {
      item: TItem
      status: 'rejected'
      reason: unknown
    }

export async function runChannelMonitorBatchExecution<TItem, TValue>(
  items: readonly TItem[],
  execute: (item: TItem) => Promise<TValue>
): Promise<Array<ChannelMonitorBatchExecutionResult<TItem, TValue>>> {
  return Promise.all(
    items.map(async (item) => {
      try {
        return {
          item,
          status: 'fulfilled',
          value: await execute(item),
        } satisfies ChannelMonitorBatchExecutionResult<TItem, TValue>
      } catch (reason) {
        return {
          item,
          status: 'rejected',
          reason,
        } satisfies ChannelMonitorBatchExecutionResult<TItem, TValue>
      }
    })
  )
}
