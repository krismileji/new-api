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
import {
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
  LOG_TYPE_FILTERS,
} from '../constants'
import type { LogsViewScope } from '../types'

const USER_VISIBLE_LOG_TYPE_VALUES = new Set<string>([
  LOG_TYPE_ALL_VALUE,
  String(LOG_TYPE_ENUM.CONSUME),
  String(LOG_TYPE_ENUM.ERROR),
])

export const LOGS_VIEW_SCOPES: readonly LogsViewScope[] = [
  'all',
  'user-visible',
  'self',
]

export function isLogsViewScope(value: string): value is LogsViewScope {
  return LOGS_VIEW_SCOPES.includes(value as LogsViewScope)
}

export function resolveLogsViewScope(
  requestedScope: LogsViewScope,
  canManageScope: boolean
): LogsViewScope {
  return canManageScope ? requestedScope : 'self'
}

export function getLogsViewTypeFilters(scope: LogsViewScope) {
  if (scope === 'all') return LOG_TYPE_FILTERS

  return LOG_TYPE_FILTERS.filter((type) =>
    USER_VISIBLE_LOG_TYPE_VALUES.has(type.value)
  )
}

export function normalizeLogsViewType(
  scope: LogsViewScope,
  value: unknown
): string {
  const rawValue = Array.isArray(value) && value.length === 1 ? value[0] : value
  if (typeof rawValue !== 'string') return LOG_TYPE_ALL_VALUE

  return getLogsViewTypeFilters(scope).some((type) => type.value === rawValue)
    ? rawValue
    : LOG_TYPE_ALL_VALUE
}

export function getLogsViewCapabilities(scope: LogsViewScope) {
  const isAllUsersView = scope !== 'self'

  return {
    isAdminView: scope === 'all',
    isAllUsersView,
    // The aggregate views need an identity column so an administrator can
    // distinguish rows from different users. User-visible data remains
    // sanitized by the server even though the table uses the same layout.
    showUserColumn: isAllUsersView,
    showChannelColumn: isAllUsersView,
  }
}
