import { test, expect } from '@playwright/test'

const TEST_USER = {
  username: 'admin',
  password: '1qaz2WSX'
}

async function loginAndWaitWorkspace(page) {
  await page.goto('/login')
  await page.waitForLoadState('networkidle')

  await page.fill('input[placeholder="请输入邮箱或手机号码、用户名"]', TEST_USER.username)
  await page.fill('input[placeholder="请输入密码"]', TEST_USER.password)
  await page.getByRole('button', { name: '登 录' }).click()

  await page.waitForURL((url) => !url.pathname.includes('/login'))
  await page.waitForFunction(() => Boolean(localStorage.getItem('token')) && Boolean(localStorage.getItem('current_workspace_id')))
}

async function createPipeline(page, definition, namePrefix = 'pipeline-refactor-ui') {
  const result = await page.evaluate(async ({ definition, namePrefix }) => {
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
        description: 'pipeline refactor browser verification',
        environment: 'development',
        definition_json: JSON.stringify(definition)
      })
    })
    const payload = await response.json()
    return {
      ok: response.ok,
      status: response.status,
      payload
    }
  }, { definition, namePrefix })

  expect(result.ok, JSON.stringify(result.payload)).toBeTruthy()
  expect(result.payload?.data?.id).toBeTruthy()
  return result.payload.data
}

async function waitForRunStatus(page, pipelineId, runId, expectedStatuses, timeout = 30000) {
  await page.waitForFunction(async ({ currentPipelineId, currentRunId, allowedStatuses }) => {
    const token = localStorage.getItem('token')
    const workspaceId = localStorage.getItem('current_workspace_id')
    const response = await fetch(`/api/pipelines/${currentPipelineId}/runs/${currentRunId}`, {
      headers: {
        Authorization: `Bearer ${token}`,
        'X-Workspace-ID': workspaceId
      }
    })
    if (!response.ok) return false
    const payload = await response.json()
    return allowedStatuses.includes(payload?.data?.status)
  }, {
    currentPipelineId: pipelineId,
    currentRunId: runId,
    allowedStatuses: expectedStatuses
  }, {
    timeout
  })
}

test('pipeline detail uses definition snapshot flow in multi-replica environment', async ({ page }) => {
  await loginAndWaitWorkspace(page)

  const pipeline = await createPipeline(page, {
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
            value: 'echo ui-definition-default',
            is_flexible: true
          }
        ],
        credential_bindings: {},
        resource_bindings: {},
        metadata: { x: 160, y: 120 }
      }
    ],
    edges: [],
    triggers: [
      {
        trigger_type: 'manual',
        enabled: true,
        config: {}
      }
    ],
    metadata: { version: '2.0' }
  })
  const pipelineId = pipeline.id

  await page.goto(`/pipeline/${pipelineId}`)
  await page.waitForLoadState('networkidle')

  await expect(page.getByRole('heading', { name: pipeline.name })).toBeVisible()

  await page.getByRole('button', { name: '运行流水线' }).click()
  await expect(page.getByText('Build（node_1）')).toBeVisible()

  const dialog = page.locator('.el-dialog:visible').last()
  const scriptInput = dialog.locator('input, textarea').first()
  await expect(scriptInput).toBeVisible()
  await scriptInput.fill('echo ui-definition-override')

  const runResponsePromise = page.waitForResponse((response) => (
    response.url().includes(`/api/pipelines/${pipelineId}/run`) &&
    response.request().method() === 'POST'
  ))
  await dialog.getByRole('button', { name: '运行' }).click()
  const runResponse = await runResponsePromise
  const runPayload = await runResponse.json()
  expect(runPayload.code).toBe(200)

  const runId = runPayload?.data?.run_id
  expect(runId).toBeTruthy()

  await expect(page.getByText('执行过程').first()).toBeVisible()

  await page.locator('.tab-item').filter({ hasText: '历史' }).first().click()
  await page.waitForLoadState('networkidle')
  await expect(page.locator('.build-number').first()).toBeVisible()

  const runDetail = await page.evaluate(async ({ currentPipelineId, currentRunId }) => {
    const token = localStorage.getItem('token')
    const workspaceId = localStorage.getItem('current_workspace_id')
    const response = await fetch(`/api/pipelines/${currentPipelineId}/runs/${currentRunId}`, {
      headers: {
        Authorization: `Bearer ${token}`,
        'X-Workspace-ID': workspaceId
      }
    })
    const payload = await response.json()
    return {
      ok: response.ok,
      status: response.status,
      payload
    }
  }, { currentPipelineId: pipelineId, currentRunId: runId })

  expect(runDetail.ok, JSON.stringify(runDetail.payload)).toBeTruthy()
  expect(runDetail.payload?.data?.run_config_json?.inputs?.node_1?.script).toBe('echo ui-definition-override')
  expect(runDetail.payload?.data?.pipeline_snapshot_json?.nodes?.[0]?.params?.[0]?.value).toBe('echo ui-definition-default')
  expect(Array.isArray(runDetail.payload?.data?.resolved_nodes_json)).toBeTruthy()
  expect(runDetail.payload?.data?.bindings_snapshot_json).toBeTruthy()
  expect(Array.isArray(runDetail.payload?.data?.events_json)).toBeTruthy()

  await waitForRunStatus(page, pipelineId, runId, ['success', 'failed', 'cancelled'])
})

test('pipeline detail streams task logs and status without manual refresh in multi-replica environment', async ({ page }) => {
  await loginAndWaitWorkspace(page)

  const pipeline = await createPipeline(page, {
    version: '2.0',
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Realtime Build',
        task_key: 'shell',
        task_version: 1,
        params: [
          {
            key: 'script',
            label: '脚本',
            value: 'echo first-line && echo second-line',
            is_flexible: true
          }
        ],
        credential_bindings: {},
        resource_bindings: {},
        metadata: { x: 160, y: 120 }
      }
    ],
    edges: [],
    triggers: [
      {
        trigger_type: 'manual',
        enabled: true,
        config: {}
      }
    ],
    metadata: { version: '2.0' }
  }, 'pipeline-realtime-ui')

  await page.goto(`/pipeline/${pipeline.id}`)
  await page.waitForLoadState('networkidle')

  await page.getByRole('button', { name: '运行流水线' }).click()
  const dialog = page.locator('.el-dialog:visible').last()
  await dialog.locator('input, textarea').first().fill('echo first-line && echo second-line')
  await dialog.getByRole('button', { name: '运行' }).click()

  const executionPanel = page.locator('.execution-panel')
  await expect(executionPanel.getByText('Realtime Build')).toBeVisible({ timeout: 20000 })

  await expect(page.locator('.execution-panel').getByText('成功').first()).toBeVisible({ timeout: 30000 })

  const logButton = executionPanel.getByRole('button', { name: '查看日志' }).first()
  await expect(logButton).toBeEnabled({ timeout: 10000 })
  await logButton.click()

  const logPanel = page.locator('.execution-logs-card')
  await expect(logPanel).toBeVisible()
  await expect(logPanel.getByText('first-line', { exact: true })).toBeVisible({ timeout: 10000 })
  await expect(logPanel.getByText('second-line', { exact: true })).toBeVisible({ timeout: 10000 })
})
