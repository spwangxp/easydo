export function canAccessRouteScope(scope, context = {}) {
  const isAdminWorkspace = Boolean(context.isAdminWorkspace)
  const canAccessWorkspaceGovernance = Boolean(context.canAccessWorkspaceGovernance)
  const canAccessPlatformGovernance = Boolean(context.canAccessPlatformGovernance)

  switch (scope) {
    case 'workspace-business':
      return !isAdminWorkspace
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
    if (!canAccessRouteScope(item.scope, context)) {
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
