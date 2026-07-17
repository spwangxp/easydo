import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

const readSource = (relativePath) => {
  return readFileSync(resolve(currentDir, relativePath), 'utf8')
}

test('renders workspace governance tabs without AI runtime management', () => {
  const indexSource = readSource('index.vue')

  assert.match(indexSource, /<el-tab-pane label="成员管理"/)
  assert.match(indexSource, /<el-tab-pane label="邀请管理"/)
  assert.match(indexSource, /<el-tab-pane label="邮件配置"/)
  assert.match(indexSource, /<el-tab-pane label="审计日志"/)
  assert.match(indexSource, /<MemberManagement\s*\/>/)
  assert.match(indexSource, /<InvitationManagement\s*\/>/)
  assert.match(indexSource, /<NotificationSenderConfigPanel scope="workspace" :workspace-id="userStore\.currentWorkspaceId"\s*\/>/)
  assert.match(indexSource, /<AuditLogPanel scope="workspace" :workspace-id="userStore\.currentWorkspaceId"\s*\/>/)
  assert.doesNotMatch(indexSource, /WorkspaceAgentManagement/)
  assert.doesNotMatch(indexSource, /RuntimeProfileManagement/)
})

test('syncs workspace governance ai tabs with route query', () => {
  const indexSource = readSource('index.vue')

  assert.match(indexSource, /import \{ useRoute, useRouter \} from 'vue-router'/)
  assert.match(indexSource, /const governanceTabs = \['members', 'invitations', 'notification-sender', 'audit-logs'\]/)
  assert.match(indexSource, /const activeTab = ref\(normalizeGovernanceTab\(route\.query\.tab\)\)/)
  assert.match(indexSource, /watch\(\(\) => route\.query\.tab/)
  assert.match(indexSource, /router\.replace\(\{ query \}\)/)
})

test('asks for confirmation before destructive workspace governance actions', () => {
  const memberSource = readSource('components/MemberManagement.vue')
  const invitationSource = readSource('components/InvitationManagement.vue')

  assert.match(memberSource, /ElMessageBox\.confirm\(`确认移除成员 \$\{row\.username\} 吗？`/)
  assert.match(memberSource, /ElMessageBox\.confirm\(`确认将 \$\{row\.username\} 提升为 Owner 吗？`/)
  assert.match(invitationSource, /ElMessageBox\.confirm\(`确认撤销发往 \$\{row\.email\} 的邀请吗？`/)
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

test('member management can add existing users and requires email for new users', () => {
  const memberSource = readSource('components/MemberManagement.vue')
  const workspaceApiSource = readSource('../../api/workspace.js')

  assert.match(memberSource, /import \{ addWorkspaceMember, getWorkspaceMembers, removeWorkspaceMember, searchWorkspaceMemberCandidates, updateWorkspaceMember \} from '@\/api\/workspace'/)
  assert.match(memberSource, /<el-button v-if="canManageMembers" @click="openAddExistingDialog">添加已有用户<\/el-button>/)
  assert.match(memberSource, /<el-dialog v-model="addExistingDialogVisible" title="添加已有用户" width="720px">/)
  assert.doesNotMatch(memberSource, /用户 ID/)
  assert.doesNotMatch(memberSource, /addExistingForm\.user_id/)
  assert.match(memberSource, /<el-input\s+v-model="memberCandidateKeyword"[\s\S]*placeholder="输入邮箱 \/ 手机号 \/ 用户名搜索"/)
  assert.match(memberSource, /<el-radio-group v-model="selectedCandidateUserId"/)
  assert.match(memberSource, /memberCandidateRows/)
  assert.match(memberSource, /await searchWorkspaceMemberCandidates\(userStore\.currentWorkspaceId, \{/)
  assert.match(memberSource, /user_id: selectedCandidateUserId\.value,/)
  assert.match(memberSource, /await addWorkspaceMember\(userStore\.currentWorkspaceId, \{/)
  assert.match(memberSource, /role: addExistingForm\.role/)
  assert.match(workspaceApiSource, /export function searchWorkspaceMemberCandidates\(id, params\)/)
  assert.match(workspaceApiSource, /url: `\/workspaces\/\$\{id\}\/members\/candidates`/)
  assert.match(memberSource, /if \(!form\.username \|\| !form\.password \|\| !form\.email\) \{/)
})

test('invitation management supports batch invite, copy, and regeneration', () => {
  const invitationSource = readSource('components/InvitationManagement.vue')

  assert.match(invitationSource, /import \{ createWorkspaceInvitation, getWorkspaceInvitations, regenerateWorkspaceInvitation, revokeWorkspaceInvitation \} from '@\/api\/workspace'/)
  assert.match(invitationSource, /<el-input v-model="inviteForm\.emails" type="textarea"/)
  assert.match(invitationSource, /placeholder="每行一个邮箱"/)
  assert.match(invitationSource, /const splitInviteEmails = \(\) =>/)
  assert.match(invitationSource, /const emails = splitInviteEmails\(\)/)
  assert.match(invitationSource, /await createWorkspaceInvitation\(userStore\.currentWorkspaceId, \{/)
  assert.match(invitationSource, /emails,/)
  assert.match(invitationSource, /<el-button v-if="row\.status === 'pending'" link size="small" @click="copyInvitationLink\(row\)">复制链接<\/el-button>/)
  assert.match(invitationSource, /<el-button v-if="canManageMembers && row\.status === 'pending'" link size="small" @click="handleRegenerateInvitation\(row\)">重新生成<\/el-button>/)
  assert.match(invitationSource, /await regenerateWorkspaceInvitation\(userStore\.currentWorkspaceId, row\.id\)/)
})
