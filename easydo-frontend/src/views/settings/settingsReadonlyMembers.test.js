import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(currentDir, 'index.vue'), 'utf8')

test('shows workspace members as readonly in settings', () => {
  assert.match(source, /<h2 class="section-title">工作空间成员<\/h2>/)
  assert.match(source, /<el-tag size="small">\{\{ roleText\(row\.role\) \}\}<\/el-tag>/)
  assert.doesNotMatch(source, /<el-select\s+v-if="canManageMembers"/)
  assert.doesNotMatch(source, /<el-table-column label="操作" width="120">/)
})

test('does not show governance actions in settings', () => {
  assert.doesNotMatch(source, /创建用户/)
  assert.doesNotMatch(source, /创建工作空间/)
  assert.doesNotMatch(source, /邀请成员/)
  assert.doesNotMatch(source, /AI Agent 管理/)
  assert.doesNotMatch(source, /新建 AI Agent/)
  assert.doesNotMatch(source, /运行策略/)
  assert.doesNotMatch(source, /openCreateUserDialog/)
  assert.doesNotMatch(source, /openCreateWorkspaceDialog/)
  assert.doesNotMatch(source, /loadAIManagementData/)
  assert.doesNotMatch(source, /canManageAI/)
})

test('does not expose platform name or logo editing in basic settings', () => {
  assert.match(source, /<h2 class="section-title">基本设置<\/h2>/)
  assert.match(source, /系统主题/)
  assert.doesNotMatch(source, /系统名称/)
  assert.doesNotMatch(source, /请输入系统名称/)
  assert.doesNotMatch(source, /systemName/)
  assert.doesNotMatch(source, /系统 Logo/)
  assert.doesNotMatch(source, /点击上传 Logo/)
  assert.doesNotMatch(source, /logo-upload/)
  assert.doesNotMatch(source, /<Upload/)
  assert.doesNotMatch(source, /saveSettings/)
})
