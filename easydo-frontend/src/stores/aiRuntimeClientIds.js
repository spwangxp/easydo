export const MAX_RUNTIME_CLIENT_ID_LENGTH = 96

function seq36(value, width = 4) {
  const number = Number(value)
  const safe = Number.isFinite(number) && number > 0 ? Math.floor(number) : 1
  return safe.toString(36).padStart(width, '0')
}

export function readableSegment(value, fallback = 'x', maxLength = 24) {
  const text = String(value ?? '').trim().toLowerCase()
  const normalized = text
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  const segment = normalized || fallback
  return segment.slice(0, maxLength)
}

function boundedRuntimeClientId(parts) {
  const id = parts.filter(Boolean).join('_')
  if (id.length <= MAX_RUNTIME_CLIENT_ID_LENGTH) return id
  const [namespace, workspace, session, purpose, sequence] = parts
  return [
    readableSegment(namespace, 'rt', 8),
    readableSegment(workspace, 'w0', 16),
    readableSegment(session, 'snew', 16),
    readableSegment(purpose, 'msg', 24),
    readableSegment(sequence, '0001', 8)
  ].join('_').slice(0, MAX_RUNTIME_CLIENT_ID_LENGTH)
}

export function createRuntimeClientIdFactory(namespace) {
  let sequence = 0
  const namespaceSegment = readableSegment(namespace, 'rt', 12)

  function observeExistingCount(count) {
    const value = Number(count)
    if (Number.isFinite(value) && value > sequence) {
      sequence = Math.floor(value)
    }
  }

  function nextClientEntryId(scope = {}, purpose = 'msg') {
    sequence += 1
    const workspace = `w${readableSegment(scope.workspace_id, '0', 12)}`
    const session = `s${readableSegment(scope.session_id, 'new', 12)}`
    const purposeSegment = readableSegment(purpose, 'msg', 24)
    return boundedRuntimeClientId([
      namespaceSegment,
      workspace,
      session,
      purposeSegment,
      seq36(sequence)
    ])
  }

  return {
    observeExistingCount,
    nextClientEntryId
  }
}
