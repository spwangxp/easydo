import { test, expect } from '@playwright/test'

const TEST_USER = {
  username: 'demo',
  password: '1qaz2WSX'
}

const PIPELINE_NAME = `并发调度验证流水线`

async function apiLogin(request) {
  const resp = await request.post('/api/auth/login', {
    data: {
      username: TEST_USER.username,
      password: TEST_USER.password
    }
  })
  expect(resp.ok()).toBeTruthy()
  const data = await resp.json()
  expect(data.code).toBe(200)
  return data.data.token
}

async function apiGet(request, token, url) {
  const resp = await request.get(url, {
    headers: {
      Authorization: `Bearer ${token}`
    }
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json()
}

async function apiPost(request, token, url, body) {
  const resp = await request.post(url, {
    headers: {
      Authorization: `Bearer ${token}`
    },
    data: body
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json()
}

async function apiPut(request, token, url, body) {
  const resp = await request.put(url, {
    headers: {
      Authorization: `Bearer ${token}`
    },
    data: body
  })
  expect(resp.ok()).toBeTruthy()
  return resp.json()
}

function buildSleepPipelineConfig(seconds = 12) {
  return JSON.stringify({
    version: '2.0',
    nodes: [
      {
        id: 'sleep-node-1',
        type: 'sleep',
        name: 'sleep task',
        config: {
          seconds
        },
        timeout: seconds + 60,
        ignore_failure: false
      }
    ],
    edges: []
  })
}

async function ensurePipeline(request, token) {
  const projects = await apiGet(request, token, '/api/projects?page=1&page_size=20')
  expect(projects.code).toBe(200)
  const projectID = projects?.data?.list?.[0]?.id || 0

  const list = await apiGet(
    request,
    token,
    `/api/pipelines?page=1&page_size=100&keyword=${encodeURIComponent(PIPELINE_NAME)}`
  )
  expect(list.code).toBe(200)

  let pipeline = (list?.data?.list || []).find((p) => p.name === PIPELINE_NAME)
  const config = buildSleepPipelineConfig(12)

  if (!pipeline) {
    const createResp = await apiPost(request, token, '/api/pipelines', {
      name: PIPELINE_NAME,
      description: '用于验证 agent 并发调度与排队状态',
      project_id: projectID,
      environment: 'testing',
      config
    })
    expect(createResp.code).toBe(200)
    pipeline = createResp.data
  } else {
    const updateResp = await apiPut(request, token, `/api/pipelines/${pipeline.id}`, {
      config
    })
    expect(updateResp.code).toBe(200)
  }

  return pipeline.id
}

async function pickTargetAgent(request, token) {
  const listResp = await apiGet(request, token, '/api/agents?page=1&page_size=100')
  expect(listResp.code).toBe(200)

  const agents = listResp?.data?.list || []
  const approved = agents.filter((a) => a.registration_status === 'approved')
  expect(approved.length).toBeGreaterThan(0)

  const onlineOrBusy = approved.find((a) => a.status === 'online' || a.status === 'busy')
  return onlineOrBusy || approved[0]
}

async function uiLogin(page) {
  await page.goto('/login')
  await page.waitForLoadState('networkidle')

  const loginBtn = page.locator('button:has-text("登 录")')
  if (await loginBtn.isVisible()) {
    const usernameInput = page
      .locator('input[placeholder*="用户名"], input[placeholder*="邮箱"]')
      .first()
    const passwordInput = page
      .locator('input[placeholder*="密码"]')
      .first()

    await expect(usernameInput).toBeVisible({ timeout: 10000 })
    await expect(passwordInput).toBeVisible({ timeout: 10000 })
    await usernameInput.fill(TEST_USER.username)
    await passwordInput.fill(TEST_USER.password)
    await loginBtn.click()
    await page.waitForURL((url) => !url.pathname.includes('/login'), { timeout: 15000 })
  }

  await expect(page.locator('body')).toBeVisible()
}

async function setAgentConcurrencyByUI(page, agentName, value) {
  await page.goto('/agent')
  await page.waitForLoadState('networkidle')

  const searchInput = page.locator('.agent-filters input[placeholder*="搜索"], .agent-filters input').first()
  await searchInput.fill(agentName)
  await page.waitForTimeout(500)

  const row = page.locator('.el-table__body-wrapper tbody tr').filter({ hasText: agentName }).first()
  await expect(row).toBeVisible({ timeout: 15000 })

  await row.locator('.more-icon').click()
  await page.locator('.el-dropdown-menu__item:has-text("编辑")').first().click()

  const editDialog = page.locator('.el-dialog:has-text("编辑执行器")').last()
  await expect(editDialog).toBeVisible({ timeout: 10000 })

  const concurrencyInput = editDialog
    .locator('.el-form-item:has-text("流水线并发") input')
    .first()
  await concurrencyInput.click()
  await concurrencyInput.press('Control+a')
  await concurrencyInput.fill(String(value))

  await editDialog.locator('button:has-text("确定")').click()
  await expect(page.locator('.el-message:has-text("更新成功")').first()).toBeVisible({ timeout: 10000 })

  await row.locator('.more-icon').click()
  await page.locator('.el-dropdown-menu__item:has-text("详情")').first().click()

  const detailDialog = page.locator('.el-dialog:has-text("执行器详情")').last()
  await expect(detailDialog).toBeVisible({ timeout: 10000 })
  const concurrencyLabel = detailDialog
    .locator('td.el-descriptions__label')
    .filter({ hasText: '流水线并发上限' })
    .first()
  await expect(concurrencyLabel).toBeVisible()
  const concurrencyValue = concurrencyLabel.locator('xpath=following-sibling::td[1]')
  await expect(concurrencyValue).toContainText(String(value))

  await detailDialog.locator('button:has-text("关闭")').click()
}

async function triggerRun(page) {
  await page.locator('button:has-text("运行流水线")').first().click()
  const runDialog = page.locator('.el-dialog:has-text("运行流水线")').last()
  await expect(runDialog).toBeVisible({ timeout: 10000 })

  const runRequestPromise = page.waitForResponse(
    (resp) =>
      resp.request().method() === 'POST' &&
      /\/api\/pipelines\/\d+\/run(?:\?|$)/.test(resp.url()),
    { timeout: 15000 }
  )

  await runDialog.locator('.el-dialog__footer .el-button--primary').first().click()

  const runResponse = await runRequestPromise
  const runStatus = runResponse.status()
  const runURL = runResponse.url()
  const runText = await runResponse.text()
  expect(
    runStatus,
    `run endpoint failed: status=${runStatus}, url=${runURL}, body=${runText}`
  ).toBe(200)
  const runBody = JSON.parse(runText)
  expect(runBody.code).toBe(200)

  await expect(runDialog).toBeHidden({ timeout: 15000 })
}

async function openHistory(page) {
  await page.locator('.detail-tabs .tab-item:has-text("历史")').click()
  await expect(page.locator('.history-panel .panel-header:has-text("执行历史")')).toBeVisible({ timeout: 10000 })
}

async function refreshHistory(page) {
  const refreshBtn = page.locator('.history-panel .panel-header .el-button').first()
  await refreshBtn.click()
  await page.waitForTimeout(1000)
}

test.describe.serial('Agent 并发与排队调度', () => {
  test.setTimeout(240000)

  test('UI 验证：并发上限实时调整 + 队列自动调度', async ({ page, request }) => {
    const token = await apiLogin(request)
    const pipelineID = await ensurePipeline(request, token)
    const targetAgent = await pickTargetAgent(request, token)

    await uiLogin(page)

    await setAgentConcurrencyByUI(page, targetAgent.name, 1)

    await page.goto(`/pipeline/${pipelineID}`)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('h1.pipeline-name')).toContainText(PIPELINE_NAME)

    await triggerRun(page)
    await page.waitForTimeout(400)
    await triggerRun(page)
    await page.waitForTimeout(400)
    await triggerRun(page)

    await openHistory(page)
    await refreshHistory(page)

    const queuedTag = page.locator('.history-panel .el-tag:has-text("排队中")')
    await expect(queuedTag.first()).toBeVisible({ timeout: 20000 })

    await page.goto('/agent')
    await page.waitForLoadState('networkidle')
    const searchInput = page.locator('.agent-filters input[placeholder*="搜索"], .agent-filters input').first()
    await searchInput.fill(targetAgent.name)
    await page.waitForTimeout(500)
    const row = page.locator('.el-table__body-wrapper tbody tr').filter({ hasText: targetAgent.name }).first()
    await expect(row).toBeVisible({ timeout: 15000 })
    await expect(row.locator('.el-tag:has-text("忙碌")')).toBeVisible({ timeout: 15000 })

    await page.goto(`/pipeline/${pipelineID}`)
    await page.waitForLoadState('networkidle')
    await openHistory(page)

    let queuedSeen = false
    let queueDrained = false

    for (let i = 0; i < 18; i += 1) {
      await refreshHistory(page)
      const queuedCount = await page.locator('.history-panel .el-tag:has-text("排队中")').count()
      if (queuedCount > 0) {
        queuedSeen = true
      }
      if (queuedSeen && queuedCount === 0) {
        queueDrained = true
        break
      }
      await page.waitForTimeout(4000)
    }

    expect(queuedSeen).toBeTruthy()
    expect(queueDrained).toBeTruthy()

    const successCount = await page.locator('.history-panel .el-tag:has-text("成功")').count()
    expect(successCount).toBeGreaterThan(0)
  })
})
