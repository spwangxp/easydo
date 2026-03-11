export function shouldRecoverFromWorkspaceError(error) {
  const status = Number(error?.response?.status || 0)
  return status === 403 || status === 500
}

export function pickNextWorkspace(workspaces = [], storedWorkspaceId = 0, responseWorkspace = null) {
  const normalized = Array.isArray(workspaces) ? workspaces.filter(item => item && item.id) : []
  const stored = normalized.find(item => Number(item.id) === Number(storedWorkspaceId)) || null
  return stored || responseWorkspace || normalized[0] || null
}
