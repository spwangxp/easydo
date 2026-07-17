import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_LONG_RUN_PROVIDER_BASE_URL || ''
const holdMs = Number(process.env.EASYDO_LONG_RUN_HOLD_MS || 610000)

test('Agent Chatbox keeps one Run SSE connection alive beyond ten minutes', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_LONG_RUN_PROVIDER_BASE_URL must point to a delayed Docker-network provider')
  test.setTimeout(holdMs + 120000)

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P0-06 Long Run SSE ${suffix}`,
      description: 'Long-lived frontend proxy validation',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
      model: { provider_model_key: 'p006-long-run-model' },
      provider_credential_ref: { credential_id: providerCredential.data.id },
      inference: { max_tokens: 32 },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'easydo' }],
      confirmation_policy: { default: 'always' },
      prompt: { system: 'Return the provider response without tools.' },
      status: 'draft'
    }
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P0-06 long run SSE acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P0-06 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(`P0-06 long SSE ${suffix}`)
  await composer.press('Enter')

  let runtimeRunID = ''
  await expect.poll(async () => {
    const session = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    runtimeRunID = session.data.active_run?.runtime_run_id || ''
    return runtimeRunID
  }, { timeout: 10000 }).not.toBe('')
  await expect.poll(async () => {
    const session = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    return session.data.active_run?.status
  }, { timeout: 30000 }).toBe('awaiting_decision')

  let runStreamRequests = 0
  page.on('request', (request) => {
    if (request.url().includes(`/api/ai/agent-chatbox/runs/${runtimeRunID}/events/stream`)) {
      runStreamRequests += 1
    }
  })
  await page.reload()
  await expect(page.getByRole('button', { name: '停止' })).toBeVisible({ timeout: 10000 })
  await expect(composer).toBeEnabled()

  await page.waitForTimeout(holdMs)

  const active = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
  expect(active.data.active_run?.runtime_run_id).toBe(runtimeRunID)
  expect(runStreamRequests).toBe(1)
  await page.getByRole('button', { name: '停止' }).click()
  await expect(composer).toBeEnabled({ timeout: 10000 })
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P0-06 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p006-test-key', token_type: 'bearer' }
    }
  })
}

async function loginAdmin() {
  const login = await apiRequest('/auth/login', {
    method: 'POST',
    data: { username: 'admin', password: '1qaz2WSX' }
  })
  const userinfo = await apiRequest('/auth/userinfo', { token: login.data.token })
  return { token: login.data.token, workspaceId: userinfo.data.workspaces[0].id }
}

function requestHeaders(auth = {}) {
  return {
    'Content-Type': 'application/json',
    ...(auth.token ? { Authorization: `Bearer ${auth.token}` } : {}),
    ...(auth.workspaceId ? { 'X-Workspace-ID': String(auth.workspaceId) } : {})
  }
}

async function apiRequest(path, options = {}) {
  const auth = options.auth || (options.token ? options : {})
  const response = await fetch(`${apiBase}${path}`, {
    method: options.method || 'GET',
    headers: requestHeaders(auth),
    body: options.data ? JSON.stringify(options.data) : undefined
  })
  const payload = await response.json()
  if (!response.ok) throw new Error(payload?.message || `Request failed: ${response.status}`)
  return payload
}
