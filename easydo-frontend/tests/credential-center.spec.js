import { expect, test } from '@playwright/test'

const username = `cred-e2e-${Date.now()}`
const apiBase = `${process.env.EASYDO_BASE_URL || 'http://127.0.0.1'}/api`

test('credentials page supports basic lifecycle', async ({ page }) => {
  await page.goto('/login')
  await page.getByPlaceholder('请输入邮箱或手机号码、用户名').fill('admin')
  await page.getByPlaceholder('请输入密码').fill('1qaz2WSX')
  await page.getByRole('button', { name: '登 录' }).click()
  await expect(page).toHaveURL(/\/$/)

  await page.goto('/credentials')
  await expect(page).toHaveURL(/\/credentials$/)
  await expect(page.locator('.credentials-page h2').filter({ hasText: '凭据管理' })).toBeVisible()

  await page.getByRole('button', { name: '新建凭据' }).click()
  await page.getByLabel('凭据名称').fill(username)
  await openSelectAndChoose(page, '凭据类型', 'API 令牌')
  await openSelectAndChoose(page, '分类', 'GitHub')
  await openSelectAndChoose(page, '范围', '工作空间')
  await page.getByLabel('令牌值').fill('ghp_e2e_token_value')
  await openSelectAndChoose(page, '令牌类型', 'Bearer')
  const listRequest = page.waitForResponse(resp => resp.url().includes('/api/v1/credentials') && resp.request().method() === 'GET')
  await page.getByRole('button', { name: '创建凭据' }).click()
  await listRequest

  const row = page.getByRole('row').filter({ hasText: username })
  await expect(row).toBeVisible({ timeout: 10000 })

  await row.getByRole('button', { name: '查看敏感载荷' }).click()
  await expect(page.locator('.payload-preview')).toContainText('ghp_e2e_token_value')
  await page.keyboard.press('Escape')

  await row.getByRole('button', { name: '验证' }).click()
  await expect(page.getByText('凭据验证通过')).toBeVisible()

  await row.getByRole('button', { name: '删除' }).click()
  await page.locator('.el-message-box__btns .el-button--primary').click()
  await expect(page.getByRole('row').filter({ hasText: username })).toHaveCount(0)
})

test('credential delete warns about pipeline references and cleanup', async ({ page }) => {
  const boundCredential = `cred-bound-${Date.now()}`
  const pipelineName = `pipe-bound-${Date.now()}`

  await page.goto('/login')
  await page.getByPlaceholder('请输入邮箱或手机号码、用户名').fill('admin')
  await page.getByPlaceholder('请输入密码').fill('1qaz2WSX')
  await page.getByRole('button', { name: '登 录' }).click()
  await expect(page).toHaveURL(/\/$/)

  await createCredentialViaApi(boundCredential)
  await createPipelineWithCredential(boundCredential, pipelineName)

  await page.goto('/credentials')
  const row = page.getByRole('row').filter({ hasText: boundCredential })
  await expect(row).toBeVisible({ timeout: 10000 })

  await row.getByRole('button', { name: '删除' }).click()
  await expect(page.getByText('删除前确认流水线影响')).toBeVisible()
  await expect(page.getByText(pipelineName)).toBeVisible()
  await expect(page.getByText('请前往对应流水线设计页，手动清理或重新配置当前凭据。')).toBeVisible()
  await page.locator('.el-message-box__btns .el-button--primary').click()
  await expect(page.getByRole('row').filter({ hasText: boundCredential })).toHaveCount(0)
})

async function openSelectAndChoose(page, label, optionText) {
  const field = page.locator('.el-form-item').filter({ hasText: label }).first()
  await field.locator('.el-select').click()
  await page.getByRole('option', { name: optionText }).click()
}

async function apiRequest(path, options = {}) {
  const method = options.method || 'GET'
  const body = options.data ? JSON.stringify(options.data) : undefined
  const response = await fetch(`${apiBase}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}),
      ...(options.workspaceId ? { 'X-Workspace-ID': String(options.workspaceId) } : {})
    },
    body
  })
  const payload = await response.json()
  if (!response.ok) {
    throw new Error(payload?.message || `Request failed: ${response.status}`)
  }
  return payload
}

async function getAdminSession() {
  const login = await apiRequest('/auth/login', {
    method: 'POST',
    data: { username: 'admin', password: '1qaz2WSX' }
  })
  const token = login.data.token
  const userinfo = await apiRequest('/auth/userinfo', { token })
  const workspaceId = userinfo.data.workspaces[0].id
  return { token, workspaceId }
}

async function createCredentialViaApi(name) {
  const { token, workspaceId } = await getAdminSession()
  return apiRequest('/v1/credentials', {
    method: 'POST',
    token,
    workspaceId,
    data: {
      name,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'ghp_bound_delete_token', token_type: 'bearer' }
    }
  })
}

async function createPipelineWithCredential(credentialName, pipelineName) {
  const { token, workspaceId } = await getAdminSession()
  const projects = await apiRequest('/projects?page=1&page_size=200', { token, workspaceId })
  const credentials = await apiRequest('/v1/credentials?page=1&size=100', { token, workspaceId })
  const credential = credentials.data.list.find(item => item.name === credentialName)
  const projectId = projects.data.list[0].id
  return apiRequest('/pipelines', {
    method: 'POST',
    token,
    workspaceId,
    data: {
      name: pipelineName,
      project_id: projectId,
      environment: 'development',
      config: JSON.stringify({
        version: '2.0',
        nodes: [{
          id: 'node-1',
          type: 'git_clone',
          name: 'Clone Repo',
          config: {
            repository: { url: 'https://example.com/repo.git' },
            credentials: { repo_auth: { credential_id: credential.id } }
          }
        }],
        edges: []
      })
    }
  })
}
