import { afterEach, expect, test, vi } from 'vitest'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
  vi.resetModules()
})

test('reloading the store restores browser privacy before first render', async () => {
  localStorage.setItem('channel-monitor-screenshot-privacy', 'true')
  vi.resetModules()
  const { useChannelMonitorPrivacyStore } =
    await import('../channel-monitor-privacy-store')
  expect(useChannelMonitorPrivacyStore.getState().enabled).toBe(true)
  useChannelMonitorPrivacyStore.getState().setEnabled(false)
  vi.resetModules()
  const reloaded = await import('../channel-monitor-privacy-store')
  expect(reloaded.useChannelMonitorPrivacyStore.getState().enabled).toBe(false)
})

test('blocked browser storage still permits enabling privacy for the current page', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('storage disabled')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('storage disabled')
  })
  vi.resetModules()
  const { useChannelMonitorPrivacyStore } =
    await import('../channel-monitor-privacy-store')
  useChannelMonitorPrivacyStore.getState().setEnabled(true)
  expect(useChannelMonitorPrivacyStore.getState().enabled).toBe(true)
})
