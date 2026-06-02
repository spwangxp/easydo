import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

const readSource = (relativePath) => {
  return readFileSync(resolve(currentDir, relativePath), 'utf8')
}

test('exposes personnel governance api endpoints', () => {
  const userSource = readSource('user.js')
  const workspaceSource = readSource('workspace.js')
  const senderSource = readSource('notificationSender.js')
  const auditSource = readSource('audit.js')

  assert.match(userSource, /export function disableUser\(id, data = \{\}\)/)
  assert.match(userSource, /url: `\/users\/\$\{id\}\/reset-password`/)
  assert.match(userSource, /url: `\/users\/\$\{id\}\/system-role`/)
  assert.match(userSource, /export function getUserWorkspaces\(id\)/)
  assert.match(userSource, /url: `\/users\/\$\{id\}\/workspaces`/)
  assert.match(userSource, /export function addUserWorkspace\(id, data\)/)
  assert.match(userSource, /export function removeUserWorkspace\(id, workspaceId\)/)
  assert.match(workspaceSource, /export function addWorkspaceMember\(id, data\)/)
  assert.match(workspaceSource, /export function regenerateWorkspaceInvitation\(id, inviteId\)/)
  assert.match(senderSource, /url: '\/notification-senders\/effective'/)
  assert.match(senderSource, /url: `\/workspaces\/\$\{id\}\/notification-sender`/)
  assert.match(senderSource, /url: '\/notification-senders\/test'/)
  assert.match(auditSource, /url: '\/audit-logs'/)
  assert.match(auditSource, /url: `\/workspaces\/\$\{id\}\/audit-logs`/)
})
