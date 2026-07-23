import { expect, test } from '@playwright/test'

const apiBase = process.env.EASYDO_API_BASE_URL || 'http://127.0.0.1:8080/api'

test.beforeEach(async ({ page }) => {
  const session = await getAdminSession()
  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, session)
})

test('ai agent page aligns page actions with the other store toolbars', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 900 })

  const modelActions = await openStoreAndMeasureActions(page, '/store/ai')
  const appActions = await openStoreAndMeasureActions(page, '/store/apps')
  const agentActions = await openStoreAndMeasureActions(page, '/store/ai-agents')

  expect(Math.abs(rightEdge(agentActions) - rightEdge(modelActions))).toBeLessThanOrEqual(2)
  expect(Math.abs(rightEdge(agentActions) - rightEdge(appActions))).toBeLessThanOrEqual(2)
  expect(Math.abs(verticalCenter(agentActions) - verticalCenter(modelActions))).toBeLessThanOrEqual(4)
  expect(Math.abs(verticalCenter(agentActions) - verticalCenter(appActions))).toBeLessThanOrEqual(4)

  await expect(page.getByRole('button', { name: '新建 Agent', exact: true })).toHaveCount(1)
  await page.getByRole('button', { name: '新建 Agent', exact: true }).click()
  await closeDialog(page, '新建 Agent Profile')

  await page.getByRole('tab', { name: 'mcp', exact: true }).click()
  await expect(page.getByRole('button', { name: '新建 MCP Server', exact: true })).toHaveCount(1)
  await page.getByRole('button', { name: '新建 MCP Server', exact: true }).click()
  await closeDialog(page, '新建 MCP Server')

  await page.getByRole('tab', { name: 'skills', exact: true }).click()
  await expect(page.getByRole('button', { name: '添加仓库', exact: true })).toHaveCount(1)
  await page.getByRole('button', { name: '添加仓库', exact: true }).click()
  await closeDialog(page, '添加 Skills 仓库')

  await page.getByRole('tab', { name: 'tools', exact: true }).click()
  await expect(page.getByRole('button', { name: /新建 Agent|新建 MCP Server|添加仓库/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeVisible()
})

test('ai agent toolbar stacks without horizontal overflow on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/store/ai-agents')

  const toolbar = page.locator('.agent-toolbar')
  const start = page.locator('.agent-toolbar-start')
  const actions = page.locator('.agent-toolbar-actions')
  await expect(toolbar).toBeVisible()
  await expect(page.getByRole('button', { name: '新建 Agent', exact: true })).toBeVisible()

  const toolbarBox = await toolbar.boundingBox()
  const startBox = await start.boundingBox()
  const actionsBox = await actions.boundingBox()
  expect(toolbarBox).not.toBeNull()
  expect(startBox).not.toBeNull()
  expect(actionsBox).not.toBeNull()
  expect(actionsBox.y).toBeGreaterThanOrEqual(startBox.y + startBox.height - 1)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(1)

  for (const width of [768, 1024, 1280]) {
    await page.setViewportSize({ width, height: 844 })
    const intermediateOverflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(intermediateOverflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(1)
  }
})

test('ai agent and sibling store toolbar data endpoints remain available', async ({ page }) => {
  const session = await getAdminSession()
  const headers = {
    Authorization: `Bearer ${session.token}`,
    'X-Workspace-ID': String(session.workspaceId)
  }

  for (const path of ['/store/ai-agents/profiles', '/store/ai-agents/resources', '/store/ai-models', '/store/templates']) {
    const response = await page.request.get(`${apiBase}${path}`, { headers })
    expect(response.ok(), `${path} returned ${response.status()}`).toBeTruthy()
  }
})

async function openStoreAndMeasureActions(page, path) {
  await page.goto(path)
  const actions = page.locator('.store-page-toolbar > .content-toolbar__actions')
  await expect(actions).toBeVisible()
  const box = await actions.boundingBox()
  expect(box).not.toBeNull()
  return box
}

async function closeDialog(page, title) {
  const dialog = page.locator('.el-dialog').filter({ hasText: title })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).toBeHidden()
}

function rightEdge(box) {
  return box.x + box.width
}

function verticalCenter(box) {
  return box.y + box.height / 2
}

async function getAdminSession() {
  const loginResponse = await fetch(`${apiBase}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'admin', password: '1qaz2WSX' })
  })
  expect(loginResponse.ok).toBeTruthy()
  const login = await loginResponse.json()
  const token = login.data.token

  const userResponse = await fetch(`${apiBase}/auth/userinfo`, {
    headers: { Authorization: `Bearer ${token}` }
  })
  expect(userResponse.ok).toBeTruthy()
  const userinfo = await userResponse.json()
  return { token, workspaceId: userinfo.data.workspaces[0].id }
}
