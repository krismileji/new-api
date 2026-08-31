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
/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, useState, type ReactNode } from 'react'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getLogsViewCapabilities, resolveLogsViewScope } from '../lib/scope'
import type { ChannelAffinityInfo, LogsViewScope } from '../types'

export type LogsViewAccess = 'self' | 'user-visible' | 'admin' | 'root'

export function resolveLogsViewAccess(
  role: number,
  viewScope: LogsViewScope
): LogsViewAccess {
  if (viewScope === 'self' || role < ROLE.ADMIN) return 'self'
  if (viewScope === 'user-visible') return 'user-visible'
  return role === ROLE.SUPER_ADMIN ? 'root' : 'admin'
}

interface UsageLogsContextValue {
  selectedUserId: number | null
  setSelectedUserId: (userId: number | null) => void
  userInfoDialogOpen: boolean
  setUserInfoDialogOpen: (open: boolean) => void
  affinityTarget: ChannelAffinityInfo | null
  setAffinityTarget: (target: ChannelAffinityInfo | null) => void
  affinityDialogOpen: boolean
  setAffinityDialogOpen: (open: boolean) => void
  sensitiveVisible: boolean
  setSensitiveVisible: (visible: boolean) => void
  viewScope: LogsViewScope
  setViewScope: (scope: LogsViewScope) => void
}

const UsageLogsContext = createContext<UsageLogsContextValue | undefined>(
  undefined
)

export function UsageLogsProvider({ children }: { children: ReactNode }) {
  const [selectedUserId, setSelectedUserId] = useState<number | null>(null)
  const [userInfoDialogOpen, setUserInfoDialogOpen] = useState(false)
  const [affinityTarget, setAffinityTarget] =
    useState<ChannelAffinityInfo | null>(null)
  const [affinityDialogOpen, setAffinityDialogOpen] = useState(false)
  const [sensitiveVisible, setSensitiveVisible] = useState(true)
  const [viewScope, setViewScope] = useState<LogsViewScope>('all')

  return (
    <UsageLogsContext.Provider
      value={{
        selectedUserId,
        setSelectedUserId,
        userInfoDialogOpen,
        setUserInfoDialogOpen,
        affinityTarget,
        setAffinityTarget,
        affinityDialogOpen,
        setAffinityDialogOpen,
        sensitiveVisible,
        setSensitiveVisible,
        viewScope,
        setViewScope,
      }}
    >
      {children}
    </UsageLogsContext.Provider>
  )
}

export function useUsageLogsContext() {
  const context = useContext(UsageLogsContext)
  if (!context) {
    throw new Error('useUsageLogsContext must be used within UsageLogsProvider')
  }
  return context
}

/** Resolves the requested scope and its independent data/UI capabilities. */
export function useLogsViewScope() {
  const role = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const { viewScope, setViewScope } = useUsageLogsContext()
  const canManageScope = role >= ROLE.ADMIN
  const effectiveViewScope = resolveLogsViewScope(viewScope, canManageScope)
  const viewAccess = resolveLogsViewAccess(role, effectiveViewScope)
  const isRootView = viewAccess === 'root'

  return {
    canManageScope,
    viewScope: effectiveViewScope,
    setViewScope,
    ...getLogsViewCapabilities(effectiveViewScope),
    isRootView,
    viewAccess,
  }
}
