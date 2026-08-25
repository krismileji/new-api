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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactElement } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { ChannelBatchTestDialog } from '../channel-batch-test-dialog'
import { ChannelTestDialogForChannel } from '../channel-test-dialog'

const { getPricing, handleTestChannel } = vi.hoisted(() => ({
  getPricing: vi.fn(),
  handleTestChannel: vi.fn(),
}))

vi.mock('@/features/channels/api', () => ({
  getChannels: vi.fn(),
  updateChannel: vi.fn(),
}))
vi.mock('@/features/channels/lib', () => ({
  CHANNEL_TEST_DEFAULTS: {
    endpointType: 'auto',
    stream: false,
    recordSample: false,
  },
  channelsQueryKeys: {
    lists: () => ['channels', 'list'],
  },
  handleTestChannel,
}))
vi.mock('@/features/pricing/api', () => ({ getPricing }))
vi.mock('sonner', () => ({
  toast: {
    dismiss: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    loading: vi.fn(() => 'toast-id'),
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

function renderWithQueryClient(element: ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>{element}</QueryClientProvider>
  )
}

beforeEach(() => {
  getPricing.mockResolvedValue({
    success: true,
    data: [{ model_name: 'gpt-5' }],
  })
  handleTestChannel.mockImplementation(
    async (
      _channelId: number,
      _options: unknown,
      complete: (
        success: boolean,
        responseTime?: number,
        error?: string,
        errorCode?: string,
        response?: unknown
      ) => void
    ) => {
      complete(true, 25)
    }
  )
})

describe('channel test completion', () => {
  test('notifies the caller once after a single-channel test completes', async () => {
    const onTestComplete = vi.fn()
    renderWithQueryClient(
      <ChannelTestDialogForChannel
        channel={{
          id: 7,
          name: '测试渠道',
          models: 'gpt-5',
          test_model: 'gpt-5',
        }}
        open
        onOpenChange={() => {}}
        onTestComplete={onTestComplete}
      />
    )

    fireEvent.click(
      await screen.findByRole('button', { name: 'Test Connection' })
    )

    await waitFor(() => expect(onTestComplete).toHaveBeenCalledTimes(1))
  })

  test('notifies the caller once after a batch test completes', async () => {
    const onTestComplete = vi.fn()
    renderWithQueryClient(
      <ChannelBatchTestDialog
        open
        onOpenChange={() => {}}
        channels={[
          {
            id: 7,
            name: '测试渠道',
            status: 1,
            models: 'gpt-5',
          },
        ]}
        onTestComplete={onTestComplete}
      />
    )

    const selectModelsButton = await screen.findByRole('button', {
      name: '全选',
    })
    await waitFor(() => expect(selectModelsButton).toBeEnabled())
    fireEvent.click(selectModelsButton)
    const selectChannelsButton = await screen.findByRole('button', {
      name: '全选启用渠道',
    })
    await waitFor(() => expect(selectChannelsButton).toBeEnabled())
    fireEvent.click(selectChannelsButton)
    const startButton = screen.getByRole('button', { name: '开始测试' })
    await waitFor(() => expect(startButton).toBeEnabled())
    fireEvent.click(startButton)

    await waitFor(() => expect(onTestComplete).toHaveBeenCalledTimes(1))
  })
})
