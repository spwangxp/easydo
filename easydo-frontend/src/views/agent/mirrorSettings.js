export function normalizeMirrorList(value) {
  if (!value) return []
  return String(value)
    .split(/\r?\n|,/)
    .map(item => item.trim())
    .filter(Boolean)
}

export function formatMirrorTextarea(mirrors) {
  return Array.isArray(mirrors) ? mirrors.join('\n') : ''
}

export function deriveMirrorEditorText(detail = {}) {
  const customMirrors = Array.isArray(detail.dockerhub_mirrors) ? detail.dockerhub_mirrors : []
  const systemDefaults = Array.isArray(detail.system_default_dockerhub_mirrors) ? detail.system_default_dockerhub_mirrors : []
  const source = detail.dockerhub_mirrors_configured ? customMirrors : systemDefaults
  return formatMirrorTextarea(source)
}

export function buildMirrorSubmitPayload(formData = {}) {
  const mirrors = normalizeMirrorList(formData.dockerhub_mirrors_text)
  const defaults = Array.isArray(formData.system_default_dockerhub_mirrors) ? formData.system_default_dockerhub_mirrors : []
  const defaultText = formatMirrorTextarea(defaults)
  const currentText = formatMirrorTextarea(mirrors)
  const inheritsDefaults = currentText === defaultText

  return {
    dockerhub_mirrors_configured: !inheritsDefaults,
    dockerhub_mirrors: inheritsDefaults ? [] : mirrors
  }
}
