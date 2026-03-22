const resolveResource = (resourceId, resourcesById = {}) => {
  return resourcesById[resourceId] || resourcesById[String(resourceId)] || null
}

const toTimestamp = (value) => {
  if (!value) return 0
  if (typeof value === 'number') {
    return value > 9999999999 ? value : value * 1000
  }
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : 0
}

export function normalizeTerminalSession(session = {}, resourcesById = {}) {
  const resourceId = Number(session.resource_id || session.resourceId || 0)
  const resource = resolveResource(resourceId, resourcesById)

  return {
    id: Number(session.id || 0),
    sessionId: session.session_id || session.sessionId || '',
    resourceId,
    resourceName: resource?.name || `VM #${resourceId || '-'}`,
    resourceType: session.resource_type || session.resourceType || resource?.type || 'vm',
    endpoint: session.endpoint || resource?.endpoint || '-',
    status: session.status || 'active',
    createdAt: session.created_at || session.createdAt || '',
    createdAtMs: toTimestamp(session.created_at || session.createdAt),
    shortSessionId: String(session.session_id || session.sessionId || '').slice(0, 8)
  }
}

export function upsertTerminalSession(sessions = [], nextSession) {
  if (!nextSession?.sessionId) {
    return [...sessions]
  }

  const deduped = sessions.filter(item => item.sessionId !== nextSession.sessionId)
  deduped.push(nextSession)

  return deduped.sort((left, right) => {
    if (right.createdAtMs !== left.createdAtMs) {
      return right.createdAtMs - left.createdAtMs
    }
    return right.sessionId.localeCompare(left.sessionId)
  })
}

export function buildSessionLabel(session, sessions = []) {
  if (!session?.sessionId) {
    return '终端会话'
  }

  const sameResourceSessions = sessions
    .filter(item => item.resourceId === session.resourceId)
    .sort((left, right) => {
      if (left.createdAtMs !== right.createdAtMs) {
        return left.createdAtMs - right.createdAtMs
      }
      return left.sessionId.localeCompare(right.sessionId)
    })

  if (sameResourceSessions.length <= 1) {
    return session.resourceName
  }

  const index = sameResourceSessions.findIndex(item => item.sessionId === session.sessionId)
  return `${session.resourceName} · 会话 ${index + 1}`
}

export function pickNextActiveSessionId(sessions = [], removedSessionId, activeSessionId) {
  const remainingSessions = sessions.filter(item => item.sessionId !== removedSessionId)
  if (remainingSessions.length === 0) {
    return ''
  }

  if (activeSessionId !== removedSessionId) {
    return remainingSessions.some(item => item.sessionId === activeSessionId)
      ? activeSessionId
      : remainingSessions[0].sessionId
  }

  const removedIndex = sessions.findIndex(item => item.sessionId === removedSessionId)
  const previousSession = removedIndex > 0 ? sessions[removedIndex - 1] : null
  if (previousSession && previousSession.sessionId !== removedSessionId) {
    return previousSession.sessionId
  }

  return remainingSessions[0].sessionId
}

export function createTerminalLaunchPath(resourceId) {
  if (!resourceId) {
    return '/terminal'
  }
  return `/terminal?resourceId=${encodeURIComponent(resourceId)}`
}

export function closeTerminalSessionsOnPageUnload(sessions = [], options = {}) {
  const token = options.token ?? (typeof localStorage !== 'undefined' ? localStorage.getItem('token') || '' : '')
  const workspaceId = options.workspaceId ?? (typeof localStorage !== 'undefined' ? localStorage.getItem('current_workspace_id') || '' : '')
  const origin = options.origin ?? (typeof window !== 'undefined' ? window.location.origin : '')
  const fetchImpl = options.fetchImpl ?? (typeof fetch === 'function' ? fetch.bind(globalThis) : null)

  if (!token || !workspaceId || !origin || !fetchImpl) {
    return 0
  }

  const activeSessions = sessions.filter(session => session?.status === 'active' && session?.sessionId && session?.resourceId)
  const dedupedSessions = activeSessions.filter((session, index) => {
    return activeSessions.findIndex(item => item.sessionId === session.sessionId) === index
  })

  dedupedSessions.forEach(session => {
    fetchImpl(`${origin}/api/resources/${encodeURIComponent(session.resourceId)}/terminal-sessions/${encodeURIComponent(session.sessionId)}/close`, {
      method: 'POST',
      keepalive: true,
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
        'X-Workspace-ID': String(workspaceId)
      },
      body: JSON.stringify({ reason: 'tab_closed' })
    }).catch(() => {})
  })

  return dedupedSessions.length
}
