import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P002_PROVIDER_BASE_URL || ''

test('Agent Chatbox cancels the exact active Pi run without late completion', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P002_PROVIDER_BASE_URL must point to a delayed Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const prompt = `P0-03 exact cancellation ${suffix}`
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P0-03 Run Cancellation ${suffix}`,
      description: 'Playwright validation for exact runtime-run cancellation',
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
    data: { change_summary: 'P0-03 run cancellation acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P0-03 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  let cancelPayload = null
  page.on('request', (request) => {
    if (!request.url().endsWith(`/api/ai/agent-chatbox/sessions/${chatSession.data.id}/cancel`)) return
    cancelPayload = request.postDataJSON()
  })

  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(prompt)
  await composer.press('Enter')

  await expect.poll(async () => {
    const response = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    return response.data.active_run?.runtime_run_id || ''
  }, { timeout: 10000 }).not.toBe('')
  const activeSession = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
  const activeEntries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const runtimeRunID = activeSession.data.active_run.runtime_run_id
  const assistantEntry = activeEntries.data.find((entry) => entry.role === 'assistant')
  expect(activeSession.data.active_run.output_entry_id).toBe(assistantEntry.id)

  await page.getByRole('button', { name: '停止' }).click()

  await expect.poll(() => cancelPayload).not.toBeNull()
  expect(cancelPayload).toEqual({
    runtime_run_id: runtimeRunID,
    reason: 'user_stopped_generation'
  })
  await expect.poll(async () => {
    const response = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    return response.data.active_run
  }, { timeout: 10000 }).toBeNull()
  await expect(composer).toBeEnabled()
  await expect(page.getByRole('button', { name: '停止' })).toHaveCount(0)

  // Wait beyond the delayed provider's normal completion window. A cancelled
  // execution must not race back and overwrite the durable terminal state.
  await page.waitForTimeout(13000)

  const finalEntries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const finalAssistants = finalEntries.data.filter((entry) => entry.role === 'assistant')
  expect(finalAssistants).toHaveLength(1)
  expect(finalAssistants[0]).toMatchObject({
    id: assistantEntry.id,
    runtime_run_id: runtimeRunID,
    status: 'cancelled'
  })
  expect(finalAssistants[0].content).not.toContain('P0-02 delayed response')

  const replay = await apiRequest(`/ai/agent-chatbox/runs/${runtimeRunID}/events`, { auth })
  expect(replay.data.events.every((event) => event.runtime_run_id === runtimeRunID)).toBe(true)
  expect(replay.data.events).toEqual(expect.arrayContaining([
    expect.objectContaining({ type: 'session.step.failed' }),
    expect.objectContaining({
      type: 'session.error',
      payload_json: expect.objectContaining({ code: 'user_cancelled' })
    })
  ]))
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P0-03 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p003-test-key', token_type: 'bearer' }
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
