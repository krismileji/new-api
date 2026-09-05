import assert from 'node:assert/strict'

import { test } from 'vitest'

import { shouldHandleChannelMonitorAnalyticsBackspace } from '../../lib/analytics-navigation'

function keyboardEventForTarget(target: EventTarget) {
  const event = new KeyboardEvent('keydown', { key: 'Backspace' })
  Object.defineProperty(event, 'target', { configurable: true, value: target })
  return event
}

test('handles Backspace when an analytics drill-down selection exists', () => {
  const event = keyboardEventForTarget(document.body)

  assert.equal(shouldHandleChannelMonitorAnalyticsBackspace(event, true), true)
})

test('keeps Backspace available for search and editable controls', () => {
  const input = document.createElement('input')
  const textarea = document.createElement('textarea')
  const contentEditable = document.createElement('div')
  Object.defineProperty(contentEditable, 'isContentEditable', { value: true })

  assert.equal(
    shouldHandleChannelMonitorAnalyticsBackspace(
      keyboardEventForTarget(input),
      true
    ),
    false
  )
  assert.equal(
    shouldHandleChannelMonitorAnalyticsBackspace(
      keyboardEventForTarget(textarea),
      true
    ),
    false
  )
  assert.equal(
    shouldHandleChannelMonitorAnalyticsBackspace(
      keyboardEventForTarget(contentEditable),
      true
    ),
    false
  )
})

test('does not consume Backspace at the analytics root', () => {
  const event = keyboardEventForTarget(document.body)

  assert.equal(
    shouldHandleChannelMonitorAnalyticsBackspace(event, false),
    false
  )
})
