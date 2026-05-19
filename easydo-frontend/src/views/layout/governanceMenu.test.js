import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  canAccessRouteScope,
  filterGovernanceMenuItems,
  resolveGovernanceFallback
} from './governanceMenu.js'

const currentDir = dirname(fileURLToPath(import.meta.url))
const layoutSource = readFileSync(resolve(currentDir, 'index.vue'), 'utf8')
const routerSource = readFileSync(resolve(currentDir, '../../router/index.js'), 'utf8')

const menuItems = [
  { name: '工作台', path: '/', scope: 'shared' },
  { name: '流水线', path: '/pipeline', scope: 'workspace-business', permission: 'pipeline.read' },
  { name: '项目', path: '/project', scope: 'workspace-business', permission: 'project.read' },
  { name: '工作区治理', path: '/workspace-governance', scope: 'workspace-governance' },
  { name: '平台治理', path: '/platform-governance', scope: 'platform-governance' },
  { name: '设置', path: '/settings', scope: 'shared', permission: 'workspace.read' }
]

const allowAll = () => true

test('hides platform governance in normal workspace', () => {
  const visible = filterGovernanceMenuItems(menuItems, {
    isAdminWorkspace: false,
    canAccessWorkspaceGovernance: true,
    canAccessPlatformGovernance: false,
    hasPermission: allowAll
  }).map(item => item.path)

  assert.deepEqual(visible, ['/', '/pipeline', '/project', '/workspace-governance', '/settings'])
  assert.equal(canAccessRouteScope('platform-governance', {
    isAdminWorkspace: false,
    canAccessWorkspaceGovernance: true,
    canAccessPlatformGovernance: false
  }), false)
  assert.equal(resolveGovernanceFallback('platform-governance', {
    canAccessWorkspaceGovernance: true,
    canAccessPlatformGovernance: false
  }), '/workspace-governance')
})

test('hides business menus in admin workspace', () => {
  const visible = filterGovernanceMenuItems(menuItems, {
    isAdminWorkspace: true,
    canAccessWorkspaceGovernance: false,
    canAccessPlatformGovernance: true,
    hasPermission: allowAll
  }).map(item => item.path)

  assert.deepEqual(visible, ['/', '/platform-governance', '/settings'])
  assert.equal(canAccessRouteScope('workspace-business', {
    isAdminWorkspace: true,
    canAccessWorkspaceGovernance: false,
    canAccessPlatformGovernance: true
  }), false)
  assert.equal(resolveGovernanceFallback('workspace-business', {
    canAccessWorkspaceGovernance: false,
    canAccessPlatformGovernance: true
  }), '/platform-governance')
})

test('still respects permission checks inside an allowed scope', () => {
  const visible = filterGovernanceMenuItems(menuItems, {
    isAdminWorkspace: false,
    canAccessWorkspaceGovernance: true,
    canAccessPlatformGovernance: false,
    hasPermission: permission => permission !== 'project.read'
  }).map(item => item.path)

  assert.deepEqual(visible, ['/', '/pipeline', '/workspace-governance', '/settings'])
})

test('marks dashboard and settings as workspace business scope in layout and router', () => {
  assert.match(layoutSource, /\{ name: '工作台', path: '\/', icon: House, scope: 'workspace-business' \}/)
  assert.match(layoutSource, /\{ name: '设置', path: '\/settings', icon: Setting, permission: 'workspace.read', scope: 'workspace-business' \}/)
  assert.match(routerSource, /path: '',\s*name: 'Dashboard',\s*component: \(\) => import\('\@\/views\/dashboard\/index\.vue'\),\s*meta: \{ scope: 'workspace-business' \}/)
  assert.match(routerSource, /path: 'settings',\s*name: 'Settings',\s*component: \(\) => import\('\@\/views\/settings\/index\.vue'\),\s*meta: \{ permission: 'workspace.read', scope: 'workspace-business' \}/)
})
