import assert from 'node:assert/strict'
import { pickNextWorkspace, shouldRecoverFromWorkspaceError } from './workspaceRecovery.js'

const activeWorkspaces = [
  { id: 11, name: 'alpha' },
  { id: 22, name: 'beta' }
]

assert.equal(shouldRecoverFromWorkspaceError({ response: { status: 403 } }), true)
assert.equal(shouldRecoverFromWorkspaceError({ response: { status: 500 } }), true)
assert.equal(shouldRecoverFromWorkspaceError({ response: { status: 400 } }), false)
assert.equal(shouldRecoverFromWorkspaceError(null), false)

assert.deepEqual(pickNextWorkspace(activeWorkspaces, 22, null), activeWorkspaces[1])
assert.deepEqual(pickNextWorkspace(activeWorkspaces, 999, activeWorkspaces[0]), activeWorkspaces[0])
assert.deepEqual(pickNextWorkspace(activeWorkspaces, 999, null), activeWorkspaces[0])
assert.equal(pickNextWorkspace([], 0, null), null)

console.log('workspace recovery tests passed')
