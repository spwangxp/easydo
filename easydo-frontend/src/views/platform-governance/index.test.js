import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { deriveGovernanceMode } from '../../stores/userGovernance.js'

const currentDir = dirname(fileURLToPath(import.meta.url))
const workspaceRoot = resolve(currentDir, '..', '..')

const readSource = (relativePath) => {
  return readFileSync(resolve(currentDir, relativePath), 'utf8')
}

test('renders platform governance only in admin workspace', () => {
  const indexSource = readSource('index.vue')
  const routerSource = readFileSync(resolve(workspaceRoot, 'router/index.js'), 'utf8')
  const mode = deriveGovernanceMode({
    currentWorkspace: { kind: 'admin', role: 'owner' },
    userInfo: { system_role: 'admin' }
  })

  assert.match(indexSource, /<el-tab-pane label="平台用户"/)
  assert.match(indexSource, /<el-tab-pane label="工作区管理"/)
  assert.match(indexSource, /<el-tab-pane label="模型与供应商"/)
  assert.match(indexSource, /<el-tab-pane label="默认运行策略"/)
  assert.match(indexSource, /<PlatformUserManagement\s*\/>/)
  assert.match(indexSource, /<WorkspaceCatalogManagement\s*\/>/)
  assert.match(indexSource, /<PlatformModelManagement\s*\/>/)
  assert.match(indexSource, /<PlatformRuntimePolicyManagement\s*\/>/)
  assert.match(routerSource, /component: \(\) => import\('@\/views\/platform-governance\/index\.vue'\)/)
  assert.equal(mode.canAccessPlatformGovernance, true)
})

test('hides platform governance outside admin workspace', () => {
  const indexSource = readSource('index.vue')
  const adminInNormalWorkspace = deriveGovernanceMode({
    currentWorkspace: { kind: 'normal', role: 'owner' },
    userInfo: { system_role: 'admin' }
  })
  const userInAdminWorkspace = deriveGovernanceMode({
    currentWorkspace: { kind: 'admin', role: 'owner' },
    userInfo: { system_role: 'user' }
  })

  assert.equal(adminInNormalWorkspace.canAccessPlatformGovernance, false)
  assert.equal(userInAdminWorkspace.canAccessPlatformGovernance, false)
  assert.doesNotMatch(indexSource, /审计日志/)
  assert.doesNotMatch(indexSource, /平台配置/)
})

test('does not rely on missing workspace kind field for governance entry', () => {
  const workspaceCatalogSource = readSource('components/WorkspaceCatalogManagement.vue')

  assert.doesNotMatch(workspaceCatalogSource, /row\.kind/)
  assert.match(workspaceCatalogSource, /isCurrentAdminWorkspace/)
  assert.match(workspaceCatalogSource, /canEnterWorkspaceGovernance\(row\)/)
})

test('workspace management keeps only one visible identifier and enforces strict alphanumeric names', () => {
  const workspaceCatalogSource = readSource('components/WorkspaceCatalogManagement.vue')

  assert.match(workspaceCatalogSource, /<el-form-item label="名称">/)
  assert.match(workspaceCatalogSource, /只允许英文字母和数字/)
  assert.doesNotMatch(workspaceCatalogSource, /<el-form-item label="Slug"/)
  assert.doesNotMatch(workspaceCatalogSource, /<el-table-column prop="slug"/)
  assert.doesNotMatch(workspaceCatalogSource, /slug: form\.slug/)
  assert.match(workspaceCatalogSource, /if \(!\/\^\[A-Za-z0-9\]\+\$\/\.test\(form\.name\)\) \{/)
})

test('normalizes optional provider credential id before submit', () => {
  const modelManagementSource = readSource('components/PlatformModelManagement.vue')

  assert.match(modelManagementSource, /<el-input-number v-model="providerForm\.credential_id" :min="1"/)
  assert.doesNotMatch(modelManagementSource, /credential_id: providerForm\.credential_id/)
  assert.match(modelManagementSource, /if \(providerForm\.credential_id\) \{\s*payload\.credential_id = providerForm\.credential_id\s*\}/)
})
