export const PAINT_WAIT_FALLBACK_MS = 50

function browserWindow() {
  return typeof window === 'undefined' ? null : window
}

function browserDocument(win) {
  return win?.document || (typeof document === 'undefined' ? null : document)
}

function timer(win, callback, delay) {
  if (typeof win?.setTimeout === 'function') {
    return win.setTimeout(callback, delay)
  }
  return setTimeout(callback, delay)
}

function clearTimer(win, timerId) {
  if (timerId == null) return
  if (typeof win?.clearTimeout === 'function') {
    win.clearTimeout(timerId)
    return
  }
  clearTimeout(timerId)
}

function isHiddenDocument(win) {
  const document = browserDocument(win)
  return Boolean(document && (
    document.hidden === true ||
    document.visibilityState === 'hidden' ||
    document.visibilityState === 'prerender'
  ))
}

export function waitForBrowserPaint() {
  const win = browserWindow()
  if (!win || isHiddenDocument(win) || typeof win.requestAnimationFrame !== 'function') {
    return new Promise((resolve) => timer(win, resolve, 0))
  }

  return new Promise((resolve) => {
    let settled = false
    let timeoutId = null
    const finish = () => {
      if (settled) return
      settled = true
      clearTimer(win, timeoutId)
      resolve()
    }

    win.requestAnimationFrame(finish)
    timeoutId = timer(win, finish, PAINT_WAIT_FALLBACK_MS)
  })
}
