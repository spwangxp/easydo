import test from 'node:test'
import assert from 'node:assert/strict'
import {
  captureStickyScrollState,
  isNearScrollBottom,
  restoreStickyScrollPosition
} from './useStickyScroll.js'

test('sticky scroll detects when the viewport is close enough to the bottom', () => {
  assert.equal(isNearScrollBottom({ scrollHeight: 1000, scrollTop: 552, clientHeight: 400 }), true)
  assert.equal(isNearScrollBottom({ scrollHeight: 1000, scrollTop: 300, clientHeight: 400 }), false)
})

test('sticky scroll follows new content only when the user was already near the bottom', () => {
  const element = { scrollHeight: 1000, scrollTop: 552, clientHeight: 400 }
  const shouldFollow = captureStickyScrollState(element)

  element.scrollHeight = 1200
  restoreStickyScrollPosition(element, shouldFollow)

  assert.equal(element.scrollTop, 1200)
})

test('sticky scroll preserves a manually scrolled position during streaming updates', () => {
  const element = { scrollHeight: 1000, scrollTop: 240, clientHeight: 400 }
  const shouldFollow = captureStickyScrollState(element)

  element.scrollHeight = 1200
  restoreStickyScrollPosition(element, shouldFollow)

  assert.equal(element.scrollTop, 240)
})

test('sticky scroll can be explicitly forced for send and session load actions', () => {
  const element = { scrollHeight: 1000, scrollTop: 240, clientHeight: 400 }
  const shouldFollow = captureStickyScrollState(element, { force: true })

  element.scrollHeight = 1200
  restoreStickyScrollPosition(element, shouldFollow)

  assert.equal(element.scrollTop, 1200)
})
