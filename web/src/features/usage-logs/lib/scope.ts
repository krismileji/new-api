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
import type { LogsViewScope } from '../types'

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

export function getLogsViewCapabilities(scope: LogsViewScope) {
  return {
    isAdminView: scope === 'all',
    isAllUsersView: scope !== 'self',
    showUserColumn: scope !== 'self',
    showChannelColumn: scope === 'all',
  }
}
