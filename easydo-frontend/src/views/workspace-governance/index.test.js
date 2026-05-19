import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

const readSource = (relativePath) => {
  return readFileSync(resolve(currentDir, relativePath), 'utf8')
}

test('renders member, invitation, agent, runtime profile tabs', () => {
  const indexSource = readSource('index.vue')

  assert.match(indexSource, /<el-tab-pane label="成员管理"/)
  assert.match(indexSource, /<el-tab-pane label="邀请管理"/)
  assert.match(indexSource, /<el-tab-pane label="AI Agent"/)
  assert.match(indexSource, /<el-tab-pane label="运行策略"/)
  assert.match(indexSource, /<MemberManagement\s*\/>/)
  assert.match(indexSource, /<InvitationManagement\s*\/>/)
  assert.match(indexSource, /<WorkspaceAgentManagement\s*\/>/)
  assert.match(indexSource, /<RuntimeProfileManagement\s*\/>/)
})

test('asks for confirmation before destructive workspace governance actions', () => {
  const memberSource = readSource('components/MemberManagement.vue')
  const invitationSource = readSource('components/InvitationManagement.vue')
  const agentSource = readSource('components/WorkspaceAgentManagement.vue')
  const runtimeProfileSource = readSource('components/RuntimeProfileManagement.vue')

  assert.match(memberSource, /ElMessageBox\.confirm\(`确认移除成员 \$\{row\.username\} 吗？`/)
  assert.match(memberSource, /ElMessageBox\.confirm\(`确认将 \$\{row\.username\} 提升为 Owner 吗？`/)
  assert.match(invitationSource, /ElMessageBox\.confirm\(`确认撤销发往 \$\{row\.email\} 的邀请吗？`/)
  assert.match(agentSource, /ElMessageBox\.confirm\(`确认删除 AI Agent \$\{row\.name\} 吗？`/)
  assert.match(runtimeProfileSource, /ElMessageBox\.confirm\(`确认删除运行策略 \$\{row\.name\} 吗？`/)
})

test('does not render a duplicate page title header in workspace governance page', () => {
  const indexSource = readSource('index.vue')

  assert.doesNotMatch(indexSource, /<h1 class="page-title">工作区治理<\/h1>/)
  assert.match(indexSource, /当前工作区：\{\{ userStore\.currentWorkspace\?\.name \|\| '-' \}\} · 当前角色：\{\{ roleText\(userStore\.currentWorkspace\?\.role\) \}\}/)
})

test('provides workspace-scoped create user entry in member management', () => {
  const memberSource = readSource('components/MemberManagement.vue')

  assert.match(memberSource, /<el-button v-if="canManageMembers" type="primary" @click="openCreateDialog">创建用户<\/el-button>/)
  assert.match(memberSource, /<el-dialog v-model="dialogVisible" title="创建工作区用户" width="520px">/)
  assert.match(memberSource, /import \{ createUser \} from '@\/api\/user'/)
  assert.match(memberSource, /workspace_role: 'viewer'/)
  assert.match(memberSource, /workspace_role: form\.workspace_role/)
  assert.doesNotMatch(memberSource, /system_role: form\./)
})
