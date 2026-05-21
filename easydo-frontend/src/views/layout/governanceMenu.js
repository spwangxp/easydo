export function canAccessRouteScope(routeLike, context = {}) {
  const scope = typeof routeLike === 'string' ? routeLike : routeLike?.scope
  const allowInAdminWorkspace = Boolean(typeof routeLike === 'object' && routeLike?.allowInAdminWorkspace)
  const isAdminWorkspace = Boolean(context.isAdminWorkspace)
  const canAccessWorkspaceGovernance = Boolean(context.canAccessWorkspaceGovernance)
  const canAccessPlatformGovernance = Boolean(context.canAccessPlatformGovernance)
  const canAccessAdminWorkspaceExecutors = Boolean(context.canAccessAdminWorkspaceExecutors)

  switch (scope) {
    case 'workspace-business':
      if (!isAdminWorkspace) {
        return true
      }
      return allowInAdminWorkspace && canAccessAdminWorkspaceExecutors
    case 'workspace-governance':
      return canAccessWorkspaceGovernance
    case 'platform-governance':
      return canAccessPlatformGovernance
    default:
      return true
  }
}

export function filterGovernanceMenuItems(items = [], context = {}) {
  const hasPermission = typeof context.hasPermission === 'function'
    ? context.hasPermission
    : () => true

  return items.filter(item => {
    if (!canAccessRouteScope(item, context)) {
      return false
    }
    return !item.permission || hasPermission(item.permission)
  })
}

export function resolveGovernanceFallback(scope, context = {}) {
  if (scope === 'platform-governance') {
    return context.canAccessWorkspaceGovernance ? '/workspace-governance' : '/'
  }
  if (scope === 'workspace-governance' || scope === 'workspace-business') {
    return context.canAccessPlatformGovernance ? '/platform-governance' : '/'
  }
  return '/'
}
