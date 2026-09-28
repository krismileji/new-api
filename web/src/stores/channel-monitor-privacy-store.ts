import { create } from 'zustand'

const storageKey = 'channel-monitor-screenshot-privacy'

export const useChannelMonitorPrivacyStore = create<{
  enabled: boolean
  setEnabled: (enabled: boolean) => void
}>((set) => {
  let enabled = false
  try {
    enabled = localStorage.getItem(storageKey) === 'true'
  } catch {}
  return {
    enabled,
    setEnabled: (next) => {
      set({ enabled: next })
      try {
        localStorage.setItem(storageKey, String(next))
      } catch {}
    },
  }
})
