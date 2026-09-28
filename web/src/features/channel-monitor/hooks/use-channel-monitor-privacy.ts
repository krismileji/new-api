import { createContext, useContext } from 'react'

export const ChannelMonitorPrivacyContext = createContext(false)

export function useChannelMonitorPrivacy() {
  return useContext(ChannelMonitorPrivacyContext)
}
