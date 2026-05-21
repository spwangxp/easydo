export function normalizeWorkspaceKind(kind) {
  const value = String(kind || '').trim().toLowerCase()
  if (!value) {
    return ''
  }
  return value === 'admin' ? 'admin' : 'normal'
}

export function normalizeSystemRole(role) {
  return String(role || '').trim().toLowerCase() === 'admin' ? 'admin' : 'user'
}

export function normalizeWorkspaceRole(role) {
  return String(role || '').trim().toLowerCase()
}

export function resolveCurrentSystemRole(userInfo = {}) {
  return normalizeSystemRole(userInfo.system_role || userInfo.role)
}

export function deriveGovernanceMode({ currentWorkspace = null, userInfo = {} } = {}) {
  const currentWorkspaceKind = normalizeWorkspaceKind(currentWorkspace?.kind)
  const currentSystemRole = resolveCurrentSystemRole(userInfo)
  const currentWorkspaceRole = normalizeWorkspaceRole(currentWorkspace?.role)
  const isAdminWorkspace = currentWorkspaceKind === 'admin'
  const isNormalWorkspace = currentWorkspaceKind === 'normal'
  const isPlatformAdmin = currentSystemRole === 'admin'
  const canAccessPlatformGovernance = isPlatformAdmin && isAdminWorkspace
  const canAccessWorkspaceGovernance = isNormalWorkspace && (isPlatformAdmin || currentWorkspaceRole === 'owner')
  const canAccessAdminWorkspaceExecutors = isPlatformAdmin && isAdminWorkspace

  return {
    currentWorkspaceKind,
    currentSystemRole,
    currentWorkspaceRole,
    isAdminWorkspace,
    isNormalWorkspace,
    isPlatformAdmin,
    canAccessPlatformGovernance,
    canAccessWorkspaceGovernance,
    canAccessAdminWorkspaceExecutors
  }
}
