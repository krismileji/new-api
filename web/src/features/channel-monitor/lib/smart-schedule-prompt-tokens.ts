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

export const SMART_SCHEDULE_TOKENS_PER_K_TOKEN = 1_000

export function channelMonitorSmartScheduleTokensToKTokens(
  tokens: number
): number {
  return tokens / SMART_SCHEDULE_TOKENS_PER_K_TOKEN
}

export function channelMonitorSmartScheduleKTokensToTokens(
  kTokens: number
): number {
  return kTokens * SMART_SCHEDULE_TOKENS_PER_K_TOKEN
}

export function formatChannelMonitorSmartScheduleKTokens(
  kTokens: number
): string {
  return `${kTokens}K Token`
}
