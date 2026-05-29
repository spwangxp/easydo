import { test, expect } from '@playwright/test'

const TEST_USER = {
  username: 'admin',
  password: '1qaz2WSX'
}

async function login(page) {
  await page.goto('/login')
  await page.waitForLoadState('networkidle')
  await page.fill('input[placeholder="请输入邮箱或手机号码、用户名"]', TEST_USER.username)
  await page.fill('input[placeholder="请输入密码"]', TEST_USER.password)
  await page.getByRole('button', { name: '登 录' }).click()
  await page.waitForURL((url) => !url.pathname.includes('/login'))
  await page.waitForFunction(() => Boolean(localStorage.getItem('token')) && Boolean(localStorage.getItem('current_workspace_id')))
}

async function createPipeline(page, definition, namePrefix = 'pipeline-regression') {
  return page.evaluate(async ({ definition, namePrefix }) => {
    const token = localStorage.getItem('token')
    const workspaceId = localStorage.getItem('current_workspace_id')
    const response = await fetch('/api/pipelines', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
        'X-Workspace-ID': workspaceId
      },
      body: JSON.stringify({
        name: `${namePrefix}-${Date.now()}`,
        description: 'pipeline regression test',
        environment: 'development',
        definition_json: JSON.stringify(definition)
      })
    })
    const payload = await response.json()
    return payload.data
  }, { definition, namePrefix })
}

async function updatePipelineDefinition(page, pipelineId, definition) {
  return page.evaluate(async ({ pipelineId, definition }) => {
    const token = localStorage.getItem('token')
    const workspaceId = localStorage.getItem('current_workspace_id')
    const response = await fetch(`/api/pipelines/${pipelineId}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
        'X-Workspace-ID': workspaceId
      },
      body: JSON.stringify({
        definition_json: JSON.stringify(definition)
      })
    })
    return {
      status: response.status,
      payload: await response.json()
    }
  }, { pipelineId, definition })
}

function buildSingleNodeDefinition(scriptValue) {
  return {
    version: '2.0',
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        task_version: 1,
        params: [
          {
            key: 'script',
            label: '脚本',
            value: scriptValue,
            is_flexible: true
          }
        ],
        credential_bindings: {},
        resource_bindings: {},
        metadata: { x: 160, y: 120 }
      }
    ],
    edges: [],
    triggers: [{ trigger_type: 'manual', enabled: true, config: {} }],
    metadata: { version: '2.0' }
  }
}

test.describe.serial('pipeline regressions', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
  })

  test('design reload preserves canonical node ids, names and execution order', async ({ page }) => {
    const definition = {
      version: '2.0',
      nodes: [
        {
          node_id: 'node_1',
          node_name: 'Start',
          task_key: 'shell',
          task_version: 1,
          params: [{ key: 'script', label: '脚本', value: 'echo start', is_flexible: false }],
          credential_bindings: {},
          resource_bindings: {},
          metadata: { x: 160, y: 120 }
        },
        {
          node_id: 'node_2',
          node_name: 'Build',
          task_key: 'shell',
          task_version: 1,
          params: [{ key: 'script', label: '脚本', value: 'echo build', is_flexible: true }],
          credential_bindings: {},
          resource_bindings: {},
          metadata: { x: 420, y: 120 }
        }
      ],
      edges: [{ from_node_id: 'node_1', to_node_id: 'node_2' }],
      triggers: [{ trigger_type: 'manual', enabled: true, config: {} }],
      metadata: { version: '2.0' }
    }

    const pipeline = await createPipeline(page, definition, 'pipeline-design-canonical')
    await page.goto(`/pipeline/${pipeline.id}`)
    await page.waitForSelector('.pipeline-design-container')

    const saveResponsePromise = page.waitForResponse((response) => (
      response.url().includes(`/api/pipelines/${pipeline.id}`) &&
      response.request().method() === 'PUT'
    ))
    await page.getByRole('button', { name: '保存' }).click()
    const saveResponse = await saveResponsePromise
    expect(saveResponse.ok()).toBeTruthy()

    await page.reload()
    await page.waitForSelector('.pipeline-design-container')

    await expect(page.locator('.pipeline-node', { hasText: 'Start' })).toBeVisible()
    await expect(page.locator('.pipeline-node', { hasText: 'Build' })).toBeVisible()
    await expect(page.locator('.node-order-badge', { hasText: '#1' })).toBeVisible()
    await expect(page.locator('.node-order-badge', { hasText: '#2' })).toBeVisible()
  })

  test('list page run action shows runtime parameter dialog', async ({ page }) => {
    const pipeline = await createPipeline(page, buildSingleNodeDefinition('echo list-default'), 'pipeline-list-run-dialog')

    await page.goto('/pipeline')
    await page.waitForLoadState('networkidle')

    const row = page.locator('.el-table__body tr', { hasText: pipeline.name }).first()
    await expect(row).toBeVisible()
    await row.locator('.action-icon').first().click()

    const dialog = page.locator('.el-dialog:visible').last()
    await expect(dialog.getByText('Build（node_1）')).toBeVisible()
    await expect(dialog.locator('input, textarea').first()).toBeVisible()
  })

  test('detail run dialog reads latest saved definition', async ({ page }) => {
    const pipeline = await createPipeline(page, buildSingleNodeDefinition('echo before-save'), 'pipeline-detail-run-refresh')

    await page.goto(`/pipeline/${pipeline.id}`)
    await page.waitForLoadState('networkidle')

    const updatedDefinition = buildSingleNodeDefinition('echo after-save')
    const updateResult = await updatePipelineDefinition(page, pipeline.id, updatedDefinition)
    expect(updateResult.status, JSON.stringify(updateResult.payload)).toBe(200)

    await page.getByRole('button', { name: '运行流水线' }).click()
    const dialog = page.locator('.el-dialog:visible').last()
    const input = dialog.locator('input, textarea').first()
    await expect(input).toBeVisible()
    await expect(input).toHaveValue('echo after-save')
  })
})
