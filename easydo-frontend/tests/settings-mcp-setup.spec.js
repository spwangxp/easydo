import { expect, test } from '@playwright/test'

test('settings integrations shows MCP onboarding block for the current workspace', async ({ page }) => {
  await mockAuthenticatedApi(page)
  await seedSession(page, { token: 'mock-token', workspaceId: 23 })

  await page.goto('/settings')
  await page.getByText('第三方集成').click()

  await expect(page.getByRole('heading', { name: '第三方集成' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'EasyDo MCP 接入' })).toBeVisible()
  await expect(page.getByText('当前工作空间 ID')).toBeVisible()
  await expect(page.getByText('********')).toBeVisible()
  await expect(page.getByRole('button', { name: '显示' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Claude Code' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'OpenCode' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Codex' })).toBeVisible()
  await expect(page.getByRole('button', { name: '环境变量' })).toBeVisible()
  await expect(page.getByRole('button', { name: '复制完整片段' })).toBeVisible()

  await page.getByRole('button', { name: '复制完整片段' }).click()
  let copiedTexts = await page.evaluate(() => window.__copiedTexts)
  expect(copiedTexts.at(-1)).toContain('"mcpServers"')
  expect(copiedTexts.at(-1)).toContain('Bearer ${EASYDO_MCP_TOKEN}')
  expect(copiedTexts.at(-1)).not.toContain('mock-token')

  await page.getByRole('button', { name: 'OpenCode' }).click()
  await page.getByRole('button', { name: '复制完整片段' }).click()
  copiedTexts = await page.evaluate(() => window.__copiedTexts)
  expect(copiedTexts.at(-1)).toContain('"type": "remote"')
  expect(copiedTexts.at(-1)).toContain('/mcp/sse')
  expect(copiedTexts.at(-1)).toContain('Bearer {env:EASYDO_MCP_TOKEN}')
  expect(copiedTexts.at(-1)).not.toContain('mock-token')

  await page.getByRole('button', { name: '环境变量' }).click()
  await page.getByRole('button', { name: '复制完整片段' }).click()
  copiedTexts = await page.evaluate(() => window.__copiedTexts)
  expect(copiedTexts.at(-1)).toContain('EASYDO_MCP_TOKEN="mock-token"')
  expect(copiedTexts.at(-1)).toContain('EASYDO_WORKSPACE_ID="23"')
})

test('settings integrations shows empty state when no workspace is available', async ({ page }) => {
  await mockAuthenticatedApi(page, { workspaces: [], currentWorkspace: null, permissions: ['workspace.read'] })
  await seedSession(page, { token: 'mock-token' })

  await page.goto('/settings')
  await page.getByText('第三方集成').click()

  await expect(page.getByText('请先在顶部切换到一个工作空间')).toBeVisible()
  await expect(page.getByRole('button', { name: '复制完整片段' })).toHaveCount(0)
})

test('settings integrations shows degraded state when token is unreadable', async ({ page }) => {
  await mockAuthenticatedApi(page)
  await seedSession(page, { token: '   ', workspaceId: 23 })

  await page.goto('/settings')
  await page.getByText('第三方集成').click()

  await expect(page.getByText('当前 token 获取失败')).toBeVisible()
  await expect(page.getByRole('button', { name: '复制完整片段' })).toHaveCount(0)
})

test('settings integrations refreshes workspace_id and snippet when workspace changes', async ({ page }) => {
  await mockAuthenticatedApi(page)
  await seedSession(page, { token: 'mock-token', workspaceId: 23 })

  await page.goto('/settings')
  await page.getByText('第三方集成').click()
  await page.getByRole('button', { name: '环境变量' }).click()
  await expect(page.locator('pre')).toContainText('EASYDO_WORKSPACE_ID="23"')

  await page.locator('.workspace-select').click()
  await page.getByRole('option', { name: 'research-lab' }).click()

  await expect(page.locator('pre')).toContainText('EASYDO_WORKSPACE_ID="57"')
})

test('settings integrations keeps action buttons visible with long MCP values', async ({ page }) => {
  const longToken = `token.${'a'.repeat(220)}.${'signature'.repeat(8)}`
  await page.setViewportSize({ width: 900, height: 768 })
  await mockAuthenticatedApi(page, {
    workspaces: [
      { id: 23, name: 'team-alpha-with-a-long-workspace-name-for-layout-check', role: 'owner', capabilities: ['workspace.read'] }
    ]
  })
  await seedSession(page, { token: longToken, workspaceId: 23 })

  await page.goto('/settings')
  await page.getByText('第三方集成').click()
  await expect(page.getByRole('heading', { name: 'EasyDo MCP 接入' })).toBeVisible()

  const layout = await page.evaluate(() => {
    const content = document.querySelector('.settings-content')
    const fields = Array.from(document.querySelectorAll('.mcp-field-item'))
    const buttons = Array.from(document.querySelectorAll('.mcp-field-item .el-button'))
    const viewportWidth = document.documentElement.clientWidth
    return {
      contentWidth: Math.round(content.getBoundingClientRect().width),
      maxFieldRight: Math.max(...fields.map(item => item.getBoundingClientRect().right)),
      maxButtonRight: Math.max(...buttons.map(item => item.getBoundingClientRect().right)),
      viewportWidth
    }
  })

  expect(layout.contentWidth).toBeLessThan(layout.viewportWidth)
  expect(layout.maxFieldRight).toBeLessThanOrEqual(layout.viewportWidth)
  expect(layout.maxButtonRight).toBeLessThanOrEqual(layout.viewportWidth)
})

async function seedSession(page, { token, workspaceId }) {
  await page.addInitScript(({ token, workspaceId }) => {
    window.__copiedTexts = []
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (text) => {
          window.__copiedTexts.push(text)
        }
      }
    })
    localStorage.setItem('token', token)
    if (workspaceId) {
      localStorage.setItem('current_workspace_id', String(workspaceId))
    } else {
      localStorage.removeItem('current_workspace_id')
    }
  }, { token, workspaceId })
}

async function mockAuthenticatedApi(page, options = {}) {
  const workspaces = options.workspaces ?? [
    { id: 23, name: 'team-alpha', role: 'owner', capabilities: ['workspace.read'] },
    { id: 57, name: 'research-lab', role: 'owner', capabilities: ['workspace.read'] }
  ]
  const currentWorkspace = options.currentWorkspace === undefined ? workspaces[0] : options.currentWorkspace
  const permissions = options.permissions ?? ['workspace.read']

  await page.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    if (url.pathname.endsWith('/api/auth/userinfo')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 200,
          data: {
            id: 1,
            username: 'demo',
            permissions,
            current_workspace: currentWorkspace,
            workspaces
          }
        })
      })
      return
    }

    if (url.pathname.endsWith('/api/notifications/inbox/unread-count')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 200, data: { unread_count: 0 } })
      })
      return
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, data: {} })
    })
  })
}
