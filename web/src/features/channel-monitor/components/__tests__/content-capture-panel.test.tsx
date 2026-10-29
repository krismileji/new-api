import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type {
  ContentCaptureState,
  ContentCaptureSummary,
} from '../../lib/content-capture'
import { ContentCapturePanel } from '../content-capture-panel'

const { getContentCaptureMock, saveContentCaptureMock } = vi.hoisted(() => ({
  getContentCaptureMock: vi.fn(),
  saveContentCaptureMock: vi.fn(),
}))

vi.mock('../../lib/content-capture', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/content-capture')>()),
  getContentCapture: getContentCaptureMock,
  saveContentCapture: saveContentCaptureMock,
}))

function summary(
  id: string,
  userId: number,
  username: string,
  preview: string
): ContentCaptureSummary {
  return {
    id,
    created_at: 1_790_000_000,
    user_id: userId,
    username,
    token_id: userId * 10,
    token_name: `${username}-key`,
    group: 'default',
    model: 'gpt-test',
    channel_id: 1,
    path: '/v1/chat/completions',
    ip: '203.0.113.1',
    status_code: 200,
    is_stream: true,
    duration_ms: 1200,
    request_size: 100,
    response_size: 200,
    request_truncated: false,
    response_truncated: false,
    request_preview: preview,
  }
}

const disabled: ContentCaptureState = {
  config: {
    enabled: false,
    limit: 0,
    max_body_kb: 0,
    started_at: 0,
    expires_at: 0,
  },
  items: [],
}

const enabled: ContentCaptureState = {
  config: {
    enabled: true,
    limit: 200,
    max_body_kb: 64,
    started_at: 1_790_000_000,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
  },
  items: [
    summary('r1', 7, 'alice', 'translate this'),
    summary('r2', 7, 'alice', 'translate that'),
    summary('r3', 8, 'bob', 'hello world'),
  ],
}

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ContentCapturePanel />
    </QueryClientProvider>
  )
}

describe('ContentCapturePanel', () => {
  beforeEach(() => {
    getContentCaptureMock.mockReset()
    saveContentCaptureMock.mockReset()
  })

  test('starting capture requires confirmation and sends the form limits', async () => {
    getContentCaptureMock.mockResolvedValue(disabled)
    saveContentCaptureMock.mockResolvedValue(enabled)
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: '开始抓取' }))
    expect(saveContentCaptureMock).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: '开始抓取' }))

    await waitFor(() =>
      expect(saveContentCaptureMock).toHaveBeenCalledWith(
        { enabled: true, limit: 200, max_body_kb: 64, hours: 24 },
        expect.anything()
      )
    )
  })

  test('clicking a user summary filters records to that user', async () => {
    getContentCaptureMock.mockResolvedValue(enabled)
    const user = userEvent.setup()
    renderPanel()

    expect(await screen.findByText('hello world')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /alice（#7）2 条/ }))

    expect(screen.getByLabelText('筛选抓取记录')).toHaveValue('7')
    expect(screen.queryByText('hello world')).not.toBeInTheDocument()
    expect(screen.getByText('translate this')).toBeInTheDocument()
  })

  test('stopping capture asks before clearing records', async () => {
    getContentCaptureMock.mockResolvedValue(enabled)
    saveContentCaptureMock.mockResolvedValue(disabled)
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: '停止并清空' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: '停止并清空' }))

    await waitFor(() =>
      expect(saveContentCaptureMock).toHaveBeenCalledWith(
        { enabled: false },
        expect.anything()
      )
    )
  })
})
