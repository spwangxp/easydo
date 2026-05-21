import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { deriveGovernanceMode } from './userGovernance.js'

const currentDir = dirname(fileURLToPath(import.meta.url))
const userStoreSource = readFileSync(resolve(currentDir, './user.js'), 'utf8')

assert.deepEqual(
  deriveGovernanceMode({
    userInfo: { role: 'admin' },
    currentWorkspace: { id: 1, kind: 'normal', role: 'maintainer', capabilities: ['user.write', 'workspace.member.manage'] }
  }),
  {
    currentWorkspaceKind: 'normal',
    currentSystemRole: 'admin',
    currentWorkspaceRole: 'maintainer',
    isAdminWorkspace: false,
    isNormalWorkspace: true,
    isPlatformAdmin: true,
    canAccessPlatformGovernance: false,
    canAccessWorkspaceGovernance: true,
    canAccessAdminWorkspaceExecutors: false
  }
)

assert.deepEqual(
  deriveGovernanceMode({
    userInfo: { system_role: 'admin' },
    currentWorkspace: { id: 2, kind: 'admin', role: 'owner', capabilities: ['workspace.member.manage'] }
  }),
  {
    currentWorkspaceKind: 'admin',
    currentSystemRole: 'admin',
    currentWorkspaceRole: 'owner',
    isAdminWorkspace: true,
    isNormalWorkspace: false,
    isPlatformAdmin: true,
    canAccessPlatformGovernance: true,
    canAccessWorkspaceGovernance: false,
    canAccessAdminWorkspaceExecutors: true
  }
)

assert.deepEqual(
  deriveGovernanceMode({
    userInfo: { role: 'user' },
    currentWorkspace: { id: 3, kind: 'normal', role: 'owner' }
  }),
  {
    currentWorkspaceKind: 'normal',
    currentSystemRole: 'user',
    currentWorkspaceRole: 'owner',
    isAdminWorkspace: false,
    isNormalWorkspace: true,
    isPlatformAdmin: false,
    canAccessPlatformGovernance: false,
    canAccessWorkspaceGovernance: true,
    canAccessAdminWorkspaceExecutors: false
  }
)

assert.deepEqual(
  deriveGovernanceMode({
    userInfo: { role: 'user' },
    currentWorkspace: { id: 4, kind: 'normal', role: 'maintainer', capabilities: ['workspace.member.manage', 'user.write'] }
  }),
  {
    currentWorkspaceKind: 'normal',
    currentSystemRole: 'user',
    currentWorkspaceRole: 'maintainer',
    isAdminWorkspace: false,
    isNormalWorkspace: true,
    isPlatformAdmin: false,
    canAccessPlatformGovernance: false,
    canAccessWorkspaceGovernance: false,
    canAccessAdminWorkspaceExecutors: false
  }
)

assert.deepEqual(
  deriveGovernanceMode({
    userInfo: { role: 'user' },
    currentWorkspace: { id: 5, kind: 'admin', role: 'owner', capabilities: ['workspace.member.manage', 'user.write'] }
  }),
  {
    currentWorkspaceKind: 'admin',
    currentSystemRole: 'user',
    currentWorkspaceRole: 'owner',
    isAdminWorkspace: true,
    isNormalWorkspace: false,
    isPlatformAdmin: false,
    canAccessPlatformGovernance: false,
    canAccessWorkspaceGovernance: false,
    canAccessAdminWorkspaceExecutors: false
  }
)

assert.deepEqual(
  deriveGovernanceMode({
    userInfo: { role: 'user' },
    currentWorkspace: null
  }),
  {
    currentWorkspaceKind: '',
    currentSystemRole: 'user',
    currentWorkspaceRole: '',
    isAdminWorkspace: false,
    isNormalWorkspace: false,
    isPlatformAdmin: false,
    canAccessPlatformGovernance: false,
    canAccessWorkspaceGovernance: false,
    canAccessAdminWorkspaceExecutors: false
  }
)

assert.match(userStoreSource, /canAccessAdminWorkspaceExecutors = computed\(\(\) => governanceMode\.value\.canAccessAdminWorkspaceExecutors\)/)
assert.match(userStoreSource, /return \{[\s\S]*canAccessAdminWorkspaceExecutors[\s\S]*\}/)

console.log('user governance mode tests passed')
