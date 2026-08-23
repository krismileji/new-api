/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import { LogicalGroupsEntry } from '@/features/channels/components/logical-groups-entry'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

describe('Channel monitor logical groups entry', () => {
  beforeEach(() => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'admin',
      role: ROLE.SUPER_ADMIN,
    })
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
  })

  test('renders an icon-only accessible action for the monitor toolbar', () => {
    render(<LogicalGroupsEntry />)

    const button = screen.getByRole('button', { name: '同渠道配置' })
    expect(button).toHaveAccessibleName('同渠道配置')
    expect(button).toHaveClass('size-8')
  })
})
