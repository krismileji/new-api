import { act, cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createPortal } from 'react-dom'
import { Toaster, toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { useChannelMonitorPrivacyStore } from '@/stores/channel-monitor-privacy-store'

import {
  ChannelMonitorPrivate,
  ChannelMonitorPrivacyDialogGuard,
  ChannelMonitorPrivacyProvider,
  ChannelMonitorPrivacyToggle,
} from '../channel-monitor-privacy'

afterEach(() => {
  cleanup()
  useChannelMonitorPrivacyStore.setState({ enabled: false })
  localStorage.clear()
  toast.dismiss()
  vi.restoreAllMocks()
})

test('privacy toggle hides both content and portal hints, retains metrics and restores values by keyboard', async () => {
  const user = userEvent.setup()
  render(
    <ChannelMonitorPrivacyProvider>
      <ChannelMonitorPrivacyToggle />
      <ChannelMonitorPrivate>
        <span title='私有上游地址'>私有渠道备注</span>
        {createPortal(<span>私有弹出提示</span>, document.body)}
      </ChannelMonitorPrivate>
      <span>成功率 99%</span>
    </ChannelMonitorPrivacyProvider>
  )
  await user.click(screen.getByRole('button', { name: '隐藏敏感信息' }))
  expect(screen.queryByText('私有渠道备注')).not.toBeInTheDocument()
  expect(screen.queryByTitle('私有上游地址')).not.toBeInTheDocument()
  expect(screen.queryByText('私有弹出提示')).not.toBeInTheDocument()
  expect(screen.getByText('成功率 99%')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '显示敏感信息' })).toHaveAttribute(
    'aria-pressed',
    'true'
  )
  expect(localStorage.getItem('channel-monitor-screenshot-privacy')).toBe(
    'true'
  )
  await user.keyboard('{Enter}')
  expect(screen.getByText('私有渠道备注')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '隐藏敏感信息' })).toHaveAttribute(
    'aria-pressed',
    'false'
  )
  expect(localStorage.getItem('channel-monitor-screenshot-privacy')).toBe(
    'false'
  )
})

test('enabled privacy masks immediately on remount but does not affect components outside the monitor', () => {
  useChannelMonitorPrivacyStore.setState({ enabled: true })
  render(
    <>
      <ChannelMonitorPrivacyProvider>
        <ChannelMonitorPrivate>私有名称</ChannelMonitorPrivate>
      </ChannelMonitorPrivacyProvider>
      <ChannelMonitorPrivate>其他页面内容</ChannelMonitorPrivate>
    </>
  )
  expect(screen.queryByText('私有名称')).not.toBeInTheDocument()
  expect(screen.getByText('其他页面内容')).toBeInTheDocument()
})

test('privacy still toggles when browser storage is unavailable', async () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('storage disabled')
  })
  render(
    <ChannelMonitorPrivacyProvider>
      <ChannelMonitorPrivacyToggle />
      <ChannelMonitorPrivate>隐藏金额</ChannelMonitorPrivate>
    </ChannelMonitorPrivacyProvider>
  )
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: '隐藏敏感信息' }))
  expect(screen.queryByText('隐藏金额')).not.toBeInTheDocument()
})

test('opening private details shows a dismissible notice without mounting sensitive contents', async () => {
  useChannelMonitorPrivacyStore.setState({ enabled: true })
  const onClose = vi.fn()
  render(
    <ChannelMonitorPrivacyProvider>
      <ChannelMonitorPrivacyDialogGuard open onClose={onClose}>
        <div>上游密钥</div>
      </ChannelMonitorPrivacyDialogGuard>
    </ChannelMonitorPrivacyProvider>
  )
  expect(screen.queryByText('上游密钥')).not.toBeInTheDocument()
  expect(screen.getByRole('dialog')).toHaveAccessibleName('敏感详情已隐藏')
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: '返回监控' }))
  expect(onClose).toHaveBeenCalledOnce()
})

test('late notifications stay invisible in screenshot mode and notifications return after leaving the monitor', async () => {
  useChannelMonitorPrivacyStore.setState({ enabled: true })
  const view = render(
    <>
      <Toaster />
      <ChannelMonitorPrivacyProvider>
        <span>监控</span>
      </ChannelMonitorPrivacyProvider>
    </>
  )
  act(() => {
    toast.error('私有上游地址请求失败', { duration: Infinity })
  })
  expect(await screen.findByText('私有上游地址请求失败')).not.toBeVisible()
  view.rerender(<Toaster />)
  expect(screen.getByText('私有上游地址请求失败')).toBeVisible()
})
