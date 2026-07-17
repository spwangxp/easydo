import test from 'node:test'
import assert from 'node:assert/strict'
import { waitForBrowserPaint } from './streamPaint.js'

test('stream paint wait uses a timer instead of requestAnimationFrame while the page is hidden', async () => {
  const previousWindow = globalThis.window
  let rafCalled = false
  let timeoutDelay = null
  try {
    globalThis.window = {
      document: { hidden: true, visibilityState: 'hidden' },
      requestAnimationFrame: () => {
        rafCalled = true
      },
      setTimeout: (callback, delay) => {
        timeoutDelay = delay
        callback()
        return 1
      },
      clearTimeout: () => {}
    }

    await waitForBrowserPaint()

    assert.equal(rafCalled, false)
    assert.equal(timeoutDelay, 0)
  } finally {
    if (previousWindow === undefined) {
      delete globalThis.window
    } else {
      globalThis.window = previousWindow
    }
  }
})

test('stream paint wait races requestAnimationFrame with a timer fallback for visible pages', async () => {
  const previousWindow = globalThis.window
  let rafCalled = false
  let timeoutDelay = null
  try {
    globalThis.window = {
      document: { hidden: false, visibilityState: 'visible' },
      requestAnimationFrame: () => {
        rafCalled = true
        return 1
      },
      setTimeout: (callback, delay) => {
        timeoutDelay = delay
        callback()
        return 2
      },
      clearTimeout: () => {}
    }

    await waitForBrowserPaint()

    assert.equal(rafCalled, true)
    assert.equal(timeoutDelay > 0, true)
  } finally {
    if (previousWindow === undefined) {
      delete globalThis.window
    } else {
      globalThis.window = previousWindow
    }
  }
})
