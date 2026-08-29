/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import type { Channel } from '../../../types'
import {
  LogicalGroupsDialog,
  LogicalGroupsPanel,
} from '../logical-groups-dialog'

const {
  getChannels,
  getLogicalChannelGroups,
  precheckLogicalChannelGroup,
  createLogicalChannelGroup,
  replaceLogicalChannelGroupMembers,
  updateLogicalChannelGroupStatus,
  deleteLogicalChannelGroup,
} = vi.hoisted(() => ({
  getChannels: vi.fn(),
  getLogicalChannelGroups: vi.fn(),
  precheckLogicalChannelGroup: vi.fn(),
  createLogicalChannelGroup: vi.fn(),
  replaceLogicalChannelGroupMembers: vi.fn(),
  updateLogicalChannelGroupStatus: vi.fn(),
  deleteLogicalChannelGroup: vi.fn(),
}))

vi.mock('@/features/channels/api', () => ({ getChannels }))
vi.mock('@/features/channels/logical-groups-api', () => ({
  getLogicalChannelGroups,
  precheckLogicalChannelGroup,
  createLogicalChannelGroup,
  replaceLogicalChannelGroupMembers,
  updateLogicalChannelGroupStatus,
  deleteLogicalChannelGroup,
}))
const channels = [
  {
    id: 1,
    name: '主渠道',
    key: 'secret-key-should-not-render',
  },
  {
    id: 2,
    name: '备用渠道',
    key: 'another-secret-key',
  },
] as Channel[]

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, resolve, reject }
}

function resetMocks() {
  vi.resetAllMocks()
  getChannels.mockResolvedValue({
    success: true,
    data: { items: channels, total: channels.length, page: 1, page_size: 100 },
  })
  getLogicalChannelGroups.mockResolvedValue({ success: true, data: [] })
  precheckLogicalChannelGroup.mockResolvedValue({
    success: true,
    data: {
      compatible: true,
      normalized_address: 'https://api.example.com/v1',
      members: [],
    },
  })
  createLogicalChannelGroup.mockResolvedValue({
    success: true,
    data: {
      id: 9,
      name: '同源渠道',
      revision: 1,
      status: 1,
      members: [],
    },
  })
  updateLogicalChannelGroupStatus.mockResolvedValue({
    success: true,
    data: {
      id: 9,
      name: '同源渠道',
      revision: 2,
      status: 2,
      members: [],
    },
  })
}

describe('LogicalGroupsDialog', () => {
  test('renders the logical groups manager as an embeddable settings panel', async () => {
    resetMocks()
    render(<LogicalGroupsPanel open canEdit />)

    expect(await screen.findByText('暂无同渠道配置')).toBeInTheDocument()
    expect(
      screen.queryByTestId('logical-groups-dialog-content')
    ).not.toBeInTheDocument()
  })

  test('keeps the newest groups and channels when an earlier open resolves later', async () => {
    resetMocks()
    const oldGroups = deferred<unknown>()
    const oldChannels = deferred<unknown>()
    getLogicalChannelGroups
      .mockReturnValueOnce(oldGroups.promise)
      .mockResolvedValueOnce({
        success: true,
        data: [
          {
            id: 2,
            name: '新逻辑组',
            revision: 1,
            status: 1,
            members: [],
          },
        ],
      })
    getChannels.mockReturnValueOnce(oldChannels.promise).mockResolvedValueOnce({
      success: true,
      data: {
        items: [{ id: 22, name: '新渠道' }],
        total: 1,
        page: 1,
        page_size: 100,
      },
    })

    const onOpenChange = vi.fn()
    const { rerender } = render(
      <LogicalGroupsDialog open onOpenChange={onOpenChange} canEdit />
    )
    await waitFor(() => expect(getChannels).toHaveBeenCalledTimes(1))
    rerender(
      <LogicalGroupsDialog open={false} onOpenChange={onOpenChange} canEdit />
    )
    rerender(<LogicalGroupsDialog open onOpenChange={onOpenChange} canEdit />)

    expect(await screen.findByText('新逻辑组')).toBeInTheDocument()
    await act(async () => {
      oldGroups.resolve({
        success: true,
        data: [
          {
            id: 1,
            name: '旧逻辑组',
            revision: 1,
            status: 1,
            members: [],
          },
        ],
      })
      oldChannels.resolve({
        success: true,
        data: {
          items: [{ id: 11, name: '旧渠道' }],
          total: 1,
          page: 1,
          page_size: 100,
        },
      })
      await Promise.all([oldGroups.promise, oldChannels.promise])
    })

    expect(screen.getByText('新逻辑组')).toBeInTheDocument()
    expect(screen.queryByText('旧逻辑组')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '新建配置' }))
    expect(
      screen.getByRole('checkbox', { name: '选择渠道 新渠道' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('checkbox', { name: '选择渠道 旧渠道' })
    ).not.toBeInTheDocument()
  })

  test('ignores an earlier load error without clearing the newer loading state', async () => {
    resetMocks()
    const oldGroups = deferred<unknown>()
    const newGroups = deferred<unknown>()
    getLogicalChannelGroups
      .mockReturnValueOnce(oldGroups.promise)
      .mockReturnValueOnce(newGroups.promise)

    const onOpenChange = vi.fn()
    const { rerender } = render(
      <LogicalGroupsDialog open onOpenChange={onOpenChange} canEdit />
    )
    await waitFor(() =>
      expect(getLogicalChannelGroups).toHaveBeenCalledTimes(1)
    )
    rerender(
      <LogicalGroupsDialog open={false} onOpenChange={onOpenChange} canEdit />
    )
    rerender(<LogicalGroupsDialog open onOpenChange={onOpenChange} canEdit />)
    await waitFor(() =>
      expect(getLogicalChannelGroups).toHaveBeenCalledTimes(2)
    )

    await act(async () => {
      oldGroups.reject(new Error('旧请求失败'))
      await Promise.resolve()
    })
    expect(screen.getByText('正在加载同渠道配置…')).toBeInTheDocument()
    expect(screen.queryByText('旧请求失败')).not.toBeInTheDocument()

    await act(async () => {
      newGroups.resolve({ success: true, data: [] })
      await newGroups.promise
    })
    expect(await screen.findByText('暂无同渠道配置')).toBeInTheDocument()
    expect(screen.queryByText('旧请求失败')).not.toBeInTheDocument()
  })

  test('constrains the dialog height and makes its main body scrollable', async () => {
    resetMocks()
    render(<LogicalGroupsDialog open onOpenChange={vi.fn()} canEdit />)

    await screen.findByText('暂无同渠道配置')
    expect(screen.getByTestId('logical-groups-dialog-content')).toHaveClass(
      'flex',
      'max-h-[calc(100dvh-2rem)]',
      'overflow-hidden'
    )
    expect(screen.getByTestId('logical-groups-dialog-body')).toHaveClass(
      'min-h-0',
      'flex-1',
      'overflow-y-auto'
    )
  })

  test('loads every remaining channel page concurrently after the first page', async () => {
    resetMocks()
    const secondPage = deferred<unknown>()
    const thirdPage = deferred<unknown>()
    getChannels.mockImplementation((params: { p?: number }) => {
      if (params.p === 1) {
        return Promise.resolve({
          success: true,
          data: {
            items: [{ id: 1, name: '第一页渠道' }],
            total: 201,
            page: 1,
            page_size: 100,
          },
        })
      }
      if (params.p === 2) return secondPage.promise
      return thirdPage.promise
    })

    render(<LogicalGroupsDialog open onOpenChange={vi.fn()} canEdit />)
    await waitFor(() => {
      expect(getChannels).toHaveBeenCalledWith({ p: 2, page_size: 100 })
      expect(getChannels).toHaveBeenCalledWith({ p: 3, page_size: 100 })
    })

    await act(async () => {
      secondPage.resolve({
        success: true,
        data: {
          items: [{ id: 2, name: '第二页渠道' }],
          total: 201,
          page: 2,
          page_size: 100,
        },
      })
      thirdPage.resolve({
        success: true,
        data: {
          items: [{ id: 3, name: '第三页渠道' }],
          total: 201,
          page: 3,
          page_size: 100,
        },
      })
      await Promise.all([secondPage.promise, thirdPage.promise])
    })
    await screen.findByText('暂无同渠道配置')
    fireEvent.click(screen.getByRole('button', { name: '新建配置' }))
    expect(
      screen.getByRole('checkbox', { name: '选择渠道 第一页渠道' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('checkbox', { name: '选择渠道 第二页渠道' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('checkbox', { name: '选择渠道 第三页渠道' })
    ).toBeInTheDocument()
  })

  test('loads groups without rendering channel keys and saves selected weights after address precheck', async () => {
    resetMocks()
    render(<LogicalGroupsDialog open onOpenChange={vi.fn()} canEdit />)

    await waitFor(() =>
      expect(screen.getByText('暂无同渠道配置')).toBeInTheDocument()
    )
    expect(
      screen.queryByText('secret-key-should-not-render')
    ).not.toBeInTheDocument()
    expect(screen.queryByText('another-secret-key')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建配置' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '选择渠道 主渠道' }))
    fireEvent.change(screen.getByLabelText('名称'), {
      target: { value: '同源渠道' },
    })

    await waitFor(() =>
      expect(
        screen.getByText('地址一致：https://api.example.com/v1')
      ).toBeInTheDocument()
    )
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => expect(createLogicalChannelGroup).toHaveBeenCalled())
    expect(createLogicalChannelGroup).toHaveBeenCalledWith({
      name: expect.any(String),
      remark: '',
      members: [{ channel_id: 1, weight: 1 }],
    })
    expect(precheckLogicalChannelGroup).toHaveBeenCalledWith([1])
    expect(
      screen.getByText('选择一个逻辑渠道进行编辑，或新建配置')
    ).toBeInTheDocument()
  })

  test('shows address mismatch errors and does not submit an incompatible group', async () => {
    resetMocks()
    precheckLogicalChannelGroup.mockResolvedValue({
      success: true,
      data: {
        compatible: false,
        members: [{ channel_id: 2, error: '请求地址不一致' }],
        error: '请求地址不一致',
      },
    })
    render(<LogicalGroupsDialog open onOpenChange={vi.fn()} canEdit />)
    await waitFor(() =>
      expect(screen.getByText('暂无同渠道配置')).toBeInTheDocument()
    )
    fireEvent.click(screen.getByRole('button', { name: '新建配置' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '选择渠道 备用渠道' }))
    await waitFor(() =>
      expect(screen.getByText('请求地址不一致')).toBeInTheDocument()
    )
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    await waitFor(() => expect(precheckLogicalChannelGroup).toHaveBeenCalled())
    expect(createLogicalChannelGroup).not.toHaveBeenCalled()
  })

  test('disables sharing for one configured group without deleting its members', async () => {
    resetMocks()
    getLogicalChannelGroups.mockResolvedValue({
      success: true,
      data: [
        {
          id: 9,
          name: '同源渠道',
          revision: 1,
          status: 1,
          members: [
            {
              id: 91,
              channel_id: 1,
              channel_name: '主渠道',
              weight: 1,
              created_at: 1,
              updated_at: 1,
            },
          ],
        },
      ],
    })
    updateLogicalChannelGroupStatus.mockResolvedValue({
      success: true,
      data: {
        id: 9,
        name: '同源渠道',
        revision: 2,
        status: 2,
        members: [
          {
            id: 91,
            channel_id: 1,
            channel_name: '主渠道',
            weight: 1,
            created_at: 1,
            updated_at: 1,
          },
        ],
      },
    })
    render(<LogicalGroupsDialog open onOpenChange={vi.fn()} canEdit />)

    const statusSwitch = await screen.findByLabelText('同源渠道共享功能')
    expect(statusSwitch).toBeChecked()
    fireEvent.click(statusSwitch)

    await waitFor(() =>
      expect(updateLogicalChannelGroupStatus).toHaveBeenCalledWith({
        id: 9,
        revision: 1,
        status: 2,
      })
    )
    expect(await screen.findByText('共享功能已停用')).toBeInTheDocument()
    expect(screen.getByText('主渠道')).toBeInTheDocument()
  })
})
