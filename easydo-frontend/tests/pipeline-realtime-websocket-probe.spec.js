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

async function createPipeline(page, definition, namePrefix = 'pipeline-ws-probe') {
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
        description: 'browser websocket probe',
        environment: 'development',
        definition_json: JSON.stringify(definition)
      })
    })
    const payload = await response.json()
    return {
      ok: response.ok,
      payload
    }
  }, { definition, namePrefix })

  expect(result.ok, JSON.stringify(result.payload)).toBeTruthy()
  return result.payload.data
}

test('browser native websocket receives pipeline business events in multi-replica environment', async ({ page }) => {
  await loginAndWaitWorkspace(page)

  const pipeline = await createPipeline(page, {
    version: '2.0',
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'WS Probe',
        task_key: 'shell',
        task_version: 1,
        params: [
          {
            key: 'script',
            label: '脚本',
            value: 'sleep 6 && echo first-line && echo second-line',
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

  await page.goto(`/pipeline/${pipeline.id}`)
  await page.waitForLoadState('networkidle')

  await page.getByRole('button', { name: '运行流水线' }).click()
  const dialog = page.locator('.el-dialog:visible').last()

  const runResponsePromise = page.waitForResponse((response) => (
    response.url().includes(`/api/pipelines/${pipeline.id}/run`) &&
    response.request().method() === 'POST'
  ))
  await dialog.getByRole('button', { name: '运行' }).click()
  const runResponse = await runResponsePromise
  const runPayload = await runResponse.json()
  expect(runPayload.code).toBe(200)

  const runId = runPayload?.data?.run_id
  expect(runId).toBeTruthy()

  const websocketEvents = await page.evaluate(async ({ currentRunId }) => {
    const token = localStorage.getItem('token')
    const url = `${location.origin.replace(/^http/, 'ws')}/ws/frontend/pipeline?run_id=${currentRunId}&token=${token}`
    const events = []

    await new Promise((resolve) => {
      const ws = new WebSocket(url)
      const seenTypes = new Set()
      const finish = () => {
        try {
          ws.close()
        } catch (_) {
          // ignore close errors during probe completion
        }
        resolve()
      }
      const timer = setTimeout(() => {
        finish()
      }, 15000)

      ws.onopen = () => {
        events.push({ type: 'open' })
      }

      ws.onmessage = (event) => {
        try {
          const parsed = JSON.parse(event.data)
          events.push(parsed)
          if (parsed?.type) {
            seenTypes.add(parsed.type)
          }
          if (seenTypes.has('task_status') && seenTypes.has('task_log') && seenTypes.has('run_status')) {
            clearTimeout(timer)
            finish()
          }
        } catch (error) {
          events.push({
            type: 'parse_error',
            payload: String(event.data),
            error: String(error)
          })
        }
      }

      ws.onerror = () => {
        events.push({ type: 'socket_error' })
      }

      ws.onclose = (event) => {
        events.push({ type: 'close', code: event.code, reason: event.reason || '' })
        clearTimeout(timer)
        resolve()
      }
    })

    return events
  }, { currentRunId: runId })

  const messageTypes = websocketEvents
    .map((entry) => entry?.type)
    .filter(Boolean)

  expect(messageTypes, JSON.stringify(websocketEvents, null, 2)).toContain('task_status')
  expect(messageTypes, JSON.stringify(websocketEvents, null, 2)).toContain('task_log')
  expect(messageTypes, JSON.stringify(websocketEvents, null, 2)).toContain('run_status')
})
