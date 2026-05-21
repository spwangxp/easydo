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
  { name: '执行器', path: '/agent', scope: 'workspace-business', allowInAdminWorkspace: true, permission: 'agent.read' },
  { name: '待接纳执行器', path: '/agent/pending', scope: 'platform-governance', permission: 'agent.approve' },
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

  assert.deepEqual(visible, ['/', '/pipeline', '/project', '/agent', '/workspace-governance', '/settings'])
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

test('shows only executor business menus in admin workspace when explicitly allowed', () => {
  const visible = filterGovernanceMenuItems(menuItems, {
    isAdminWorkspace: true,
    canAccessWorkspaceGovernance: false,
    canAccessPlatformGovernance: true,
    canAccessAdminWorkspaceExecutors: true,
    hasPermission: allowAll
  }).map(item => item.path)

  assert.deepEqual(visible, ['/', '/agent', '/agent/pending', '/platform-governance', '/settings'])
  assert.equal(canAccessRouteScope({
    scope: 'workspace-business',
    allowInAdminWorkspace: true
  }, {
    isAdminWorkspace: true,
    canAccessWorkspaceGovernance: false,
    canAccessPlatformGovernance: true,
    canAccessAdminWorkspaceExecutors: true
  }), true)
  assert.equal(canAccessRouteScope({
    scope: 'workspace-business'
  }, {
    isAdminWorkspace: true,
    canAccessWorkspaceGovernance: false,
    canAccessPlatformGovernance: true,
    canAccessAdminWorkspaceExecutors: true
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

  assert.deepEqual(visible, ['/', '/pipeline', '/agent', '/workspace-governance', '/settings'])
})

test('marks only executor list as admin-workspace reusable business route', () => {
  assert.match(layoutSource, /\{ name: '执行器', path: '\/agent', icon: Monitor, permission: 'agent.read', scope: 'workspace-business', allowInAdminWorkspace: true \}/)
  assert.match(layoutSource, /\{ name: '待接纳执行器', path: '\/agent\/pending', icon: Monitor, permission: 'agent.approve', scope: 'platform-governance' \}/)
  assert.doesNotMatch(layoutSource, /\{ name: '流水线', path: '\/pipeline', icon: Connection, permission: 'pipeline.read', scope: 'workspace-business', allowInAdminWorkspace: true \}/)
  assert.match(routerSource, /path: 'agent',\s*name: 'Agent',\s*component: \(\) => import\('\@\/views\/agent\/index\.vue'\),\s*meta: \{ permission: 'agent.read', scope: 'workspace-business', allowInAdminWorkspace: true \}/)
  assert.match(routerSource, /path: 'agent\/pending',\s*name: 'AgentPending',\s*component: \(\) => import\('\@\/views\/agent\/pending\.vue'\),\s*meta: \{ permission: 'agent.approve', scope: 'platform-governance' \}/)
})
