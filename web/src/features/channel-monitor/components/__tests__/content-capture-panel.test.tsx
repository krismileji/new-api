import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type {
  ContentCaptureState,
  ContentCaptureSummary,
} from '../../lib/content-capture'
import { ContentCapturePanel } from '../content-capture-panel'

const { getContentCaptureMock, getRecordMock, saveContentCaptureMock } =
  vi.hoisted(() => ({
    getContentCaptureMock: vi.fn(),
    getRecordMock: vi.fn(),
    saveContentCaptureMock: vi.fn(),
  }))

vi.mock('../../lib/content-capture', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/content-capture')>()),
  getContentCapture: getContentCaptureMock,
  getContentCaptureRecord: getRecordMock,
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
    capture_raw: false,
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
    capture_raw: false,
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
    getRecordMock.mockReset()
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
        {
          enabled: true,
          limit: 200,
          max_body_kb: 64,
          hours: 24,
          capture_raw: false,
        },
        expect.anything()
      )
    )
  })

  test('updating capture can opt into retaining raw bodies', async () => {
    getContentCaptureMock.mockResolvedValue(enabled)
    saveContentCaptureMock.mockResolvedValue(enabled)
    const user = userEvent.setup()
    renderPanel()

    const checkbox = await screen.findByRole('checkbox', {
      name: '同时保留原始报文',
    })
    expect(checkbox).not.toBeChecked()
    await user.click(checkbox)
    await user.click(screen.getByRole('button', { name: '更新设置' }))

    await waitFor(() =>
      expect(saveContentCaptureMock).toHaveBeenCalledWith(
        expect.objectContaining({ enabled: true, capture_raw: true }),
        expect.anything()
      )
    )
  })

  test('new detail records show dialogue and disclose raw data on demand', async () => {
    getContentCaptureMock.mockResolvedValue(enabled)
    getRecordMock.mockResolvedValue({
      ...enabled.items[0],
      content_extracted: true,
      request_text: '翻译这一句',
      response_text: 'Translation\nwith a newline',
      request_body: '{"tools":["raw definition"]}',
      response_body: 'data: [DONE]',
    })
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: '查看请求 r1' }))
    const detail = await screen.findByRole('dialog', { name: '请求内容' })
    expect(await within(detail).findByText('翻译这一句')).toBeVisible()
    expect(
      within(detail).getByText(/Translation\s+with a newline/)
    ).toBeVisible()
    const raw = within(detail).getByRole('button', { name: '原始报文' })
    expect(raw).toHaveAttribute('aria-expanded', 'false')
    expect(within(detail).queryByText(/raw definition/)).not.toBeInTheDocument()

    raw.focus()
    await user.keyboard('{Enter}')
    expect(raw).toHaveAttribute('aria-expanded', 'true')
    expect(await within(detail).findByText(/raw definition/)).toBeVisible()
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
