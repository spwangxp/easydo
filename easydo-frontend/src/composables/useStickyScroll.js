const DEFAULT_BOTTOM_THRESHOLD = 48

function numericScrollValue(value) {
  return Number.isFinite(value) ? value : 0
}

export function isNearScrollBottom(element, threshold = DEFAULT_BOTTOM_THRESHOLD) {
  if (!element) return false
  const scrollHeight = numericScrollValue(element.scrollHeight)
  const scrollTop = numericScrollValue(element.scrollTop)
  const clientHeight = numericScrollValue(element.clientHeight)
  return scrollHeight - scrollTop - clientHeight <= threshold
}

export function scrollElementToBottom(element) {
  if (!element) return
  element.scrollTop = numericScrollValue(element.scrollHeight)
}

export function captureStickyScrollState(element, options = {}) {
  return {
    shouldFollow: Boolean(options.force) || isNearScrollBottom(element, options.threshold),
    previousScrollTop: element ? numericScrollValue(element.scrollTop) : 0
  }
}

export function restoreStickyScrollPosition(element, state = {}) {
  if (!element || !state.shouldFollow) return
  scrollElementToBottom(element)
}

export function useStickyScroll(scrollRef, options = {}) {
  let forceNextScroll = false

  function requestScrollToBottom() {
    forceNextScroll = true
  }

  function captureCurrentStickyScrollState() {
    const state = captureStickyScrollState(scrollRef.value, {
      ...options,
      force: forceNextScroll
    })
    forceNextScroll = false
    return state
  }

  function restoreCurrentStickyScrollPosition(state) {
    restoreStickyScrollPosition(scrollRef.value, state)
  }

  return {
    captureStickyScrollState: captureCurrentStickyScrollState,
    restoreStickyScrollPosition: restoreCurrentStickyScrollPosition,
    requestScrollToBottom
  }
}
