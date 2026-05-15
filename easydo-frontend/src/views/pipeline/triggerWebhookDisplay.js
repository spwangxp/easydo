export const PLACEHOLDER_VALUE = '-'

export function buildDisplayedWebhookURL({ origin = '', webhookToken = '', fallbackURL = '' } = {}) {
  const normalizedOrigin = String(origin || '').trim().replace(/\/+$/, '')
  const normalizedToken = String(webhookToken || '').trim()
  const normalizedFallback = String(fallbackURL || '').trim()

  if (normalizedOrigin && normalizedToken) {
    return `${normalizedOrigin}/api/pipeline/run/webhook/${normalizedToken}`
  }

  if (normalizedFallback) {
    return normalizedFallback
  }

  return PLACEHOLDER_VALUE
}

export function buildDisplayedSecretToken({ secretToken = '', webhookToken = '' } = {}) {
  return String(secretToken || '').trim() || String(webhookToken || '').trim() || PLACEHOLDER_VALUE
}

export function canCopyDisplayValue(value) {
  return String(value || '').trim() !== PLACEHOLDER_VALUE
}

export async function copyDisplayedValue({ value, canCopy, writeText, onSuccess, onError } = {}) {
  if (!canCopy(value)) {
    return false
  }

  try {
    await writeText(value)
    onSuccess('复制成功')
    return true
  } catch {
    onError('复制失败')
    return false
  }
}
