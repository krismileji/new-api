export function shouldHandleChannelMonitorAnalyticsBackspace(
  event: KeyboardEvent,
  hasSelection: boolean
) {
  if (!hasSelection || event.key !== 'Backspace') return false

  const target = event.target
  if (!(target instanceof HTMLElement)) return true
  if (target.isContentEditable) return false

  return !['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)
}
