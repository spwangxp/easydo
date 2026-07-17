import { expect, test } from '@playwright/test'
import { composeServiceContainer, startContainer, stopContainer } from './helpers/easydoDocker.js'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P002_PROVIDER_BASE_URL || ''

test('Agent Chatbox resumes a Pi run after its proxy connection is interrupted', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P002_PROVIDER_BASE_URL must point to a delayed Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const prompt = `P0-04 reconnect ${suffix}`
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P0-04 Stream Reconnect ${suffix}`,
      description: 'Playwright validation for cursor-based Run stream recovery',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
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
    data: { change_summary: 'P0-04 stream reconnect acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P0-04 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  const streamRequests = []
  page.on('request', (request) => {
    if (!/\/api\/ai\/agent-chatbox\/runs\/[^/]+\/events\/stream$/.test(request.url())) return
    streamRequests.push({ url: request.url(), payload: request.postDataJSON() })
  })

  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(prompt)
  await composer.press('Enter')
  await expect(page.locator('.turn.assistant .ai-runtime-event-line').first()).toBeVisible({ timeout: 10000 })
  await expect(page.getByText('实时', { exact: true })).toBeVisible()

  let activeRunID = ''
  await expect.poll(async () => {
    const session = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    activeRunID = session.data.active_run?.runtime_run_id || ''
    return activeRunID
  }, { timeout: 10000 }).not.toBe('')

  const streamRequestsBeforeInterrupt = streamRequests.length
  const frontendContainer = await composeServiceContainer('frontend')
  await stopContainer(frontendContainer)
  await expect.poll(async () => {
    try {
      await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
      return 'up'
    } catch {
      return 'down'
    }
  }, { timeout: 15000 }).toBe('down')
  await startContainer(frontendContainer)
  await expect.poll(async () => {
    try {
      await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
      return 'up'
    } catch {
      return 'down'
    }
  }, { timeout: 30000 }).toBe('up')

  await page.reload()
  await expect(page.locator('.turn.assistant .ai-runtime-event-line').first()).toBeVisible({ timeout: 15000 })
  await expect.poll(() => streamRequests.length, { timeout: 45000 }).toBeGreaterThan(streamRequestsBeforeInterrupt)
  const resumeRequests = streamRequests.slice(streamRequestsBeforeInterrupt)
  expect(resumeRequests.some((request) => Boolean(request.payload?.after_event_id))).toBe(true)
  await expect(page.getByText('实时', { exact: true })).toBeVisible({ timeout: 30000 })
  await expect(page.locator('.turn.assistant .final-answer')).toContainText('P0-02 delayed response', { timeout: 60000 })
  await expect(page.getByPlaceholder('输入消息')).toBeEnabled()

  const entries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const assistants = entries.data.filter((entry) => entry.role === 'assistant')
  expect(assistants).toHaveLength(1)
  expect(assistants[0]).toMatchObject({ status: 'completed', content: 'P0-02 delayed response', runtime_run_id: activeRunID })
  const replay = await apiRequest(`/ai/agent-chatbox/runs/${assistants[0].runtime_run_id}/events`, { auth })
  const eventIDs = replay.data.events.map((event) => event.event_id)
  expect(new Set(eventIDs).size).toBe(eventIDs.length)
  expect(replay.data.events.every((event) => event.runtime_run_id === assistants[0].runtime_run_id)).toBe(true)
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P0-04 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p004-test-key', token_type: 'bearer' }
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
