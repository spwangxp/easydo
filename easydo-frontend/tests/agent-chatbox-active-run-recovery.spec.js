import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P002_PROVIDER_BASE_URL || ''

test('Agent Chatbox restores the same active Pi run after refresh', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P002_PROVIDER_BASE_URL must point to a delayed Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const prompt = `P0-02 refresh recovery ${suffix}`
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P0-02 Active Run Recovery ${suffix}`,
      description: 'Playwright validation for durable active-run recovery',
      profile_kind: 'generic',
      context_tags: [],
      provider: {
        provider_type: 'openai-compatible',
        base_url: providerBaseURL
      },
      model: { provider_model_key: 'p002-delayed-model' },
      provider_credential_ref: { credential_id: providerCredential.data.id },
      inference: { max_tokens: 32 },
      prompt: { system: 'Return the provider response without tools.' },
      status: 'draft'
    }
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P0-02 active run recovery acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P0-02 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(prompt)
  await composer.press('Enter')

  await expect.poll(async () => {
    const [sessionResponse, entriesResponse] = await Promise.all([
      apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth }),
      apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
    ])
    const assistantEntries = entriesResponse.data.filter((entry) => entry.role === 'assistant')
    const activeRun = sessionResponse.data.active_run
    if (!activeRun?.runtime_run_id || assistantEntries.length !== 1 || assistantEntries[0].status !== 'streaming') {
      return null
    }
    return {
      runtimeRunID: activeRun.runtime_run_id,
      outputEntryID: activeRun.output_entry_id,
      assistantEntryID: assistantEntries[0].id,
      assistantRuntimeRunID: assistantEntries[0].runtime_run_id
    }
  }, { timeout: 10000 }).not.toBeNull()

  // Read the durable identifiers once the active state has been observed.
  const beforeRefreshSession = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
  const beforeRefreshEntries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const beforeAssistant = beforeRefreshEntries.data.find((entry) => entry.role === 'assistant')
  const beforeActiveRun = beforeRefreshSession.data.active_run
  expect(beforeActiveRun.output_entry_id).toBe(beforeAssistant.id)
  expect(beforeAssistant.runtime_run_id).toBe(beforeActiveRun.runtime_run_id)

  await page.reload()

  await expect(page.locator('.turn.user')).toHaveCount(1, { timeout: 2000 })
  await expect(page.locator('.turn.assistant')).toHaveCount(1, { timeout: 2000 })
  await expect(page.locator('.turn.user')).toContainText(prompt)
  await expect(page.locator('.turn.assistant .ai-runtime-event-line').first()).toBeVisible({ timeout: 2000 })
  await expect(page.getByRole('button', { name: '停止' })).toBeVisible({ timeout: 2000 })
  await expect(composer).toBeEnabled()
  await expect(page.locator('.chatbox-model-trigger')).toBeDisabled()

  const afterRefreshSession = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
  const afterRefreshEntries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const afterAssistantEntries = afterRefreshEntries.data.filter((entry) => entry.role === 'assistant')
  expect(afterRefreshSession.data.active_run.runtime_run_id).toBe(beforeActiveRun.runtime_run_id)
  expect(afterRefreshSession.data.active_run.output_entry_id).toBe(beforeAssistant.id)
  expect(afterAssistantEntries).toHaveLength(1)
  expect(afterAssistantEntries[0].id).toBe(beforeAssistant.id)
  expect(afterAssistantEntries[0].runtime_run_id).toBe(beforeActiveRun.runtime_run_id)

  await expect.poll(async () => {
    const response = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    return response.data.active_run
  }, { timeout: 30000 }).toBeNull()

  const completedEntries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const completedAssistants = completedEntries.data.filter((entry) => entry.role === 'assistant')
  expect(completedAssistants).toHaveLength(1)
  expect(completedAssistants[0].id).toBe(beforeAssistant.id)
  expect(completedAssistants[0].runtime_run_id).toBe(beforeActiveRun.runtime_run_id)
  expect(completedAssistants[0].status).toBe('completed')
  expect(completedAssistants[0].content).toContain('P0-02 delayed response')
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P0-02 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p002-test-key', token_type: 'bearer' }
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
