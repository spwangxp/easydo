import { expect, test } from '@playwright/test'
import { runtimeContainerForRun, startContainer, stopContainer } from './helpers/easydoDocker.js'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P002_PROVIDER_BASE_URL || ''

test('Agent Chatbox marks an active Run interrupted when its Runtime owner dies', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P002_PROVIDER_BASE_URL must point to a delayed Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P1-04 Owner Failover ${suffix}`,
      description: 'Playwright validation for durable Runtime owner lease expiry',
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
    data: { change_summary: 'P1-04 runtime owner failover acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-04 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(`P1-04 owner failover ${suffix}`)
  await composer.press('Enter')

  let activeRun
  await expect.poll(async () => {
    const session = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    activeRun = session.data.active_run
    return activeRun?.runtime_run_id || ''
  }, { timeout: 10000 }).not.toBe('')

  const ownerContainer = await runtimeContainerForRun(activeRun.runtime_run_id)
  await stopContainer(ownerContainer)

  try {
    const assistant = page.locator('.turn.assistant').last()
    await expect(assistant).toContainText('运行已中断', { timeout: 35000 })
    await expect(assistant.locator('.ai-runtime-event-line.running')).toHaveCount(0)
    await expect(composer).toBeEnabled()

    const entries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
    const repaired = entries.data.find((entry) => entry.runtime_run_id === activeRun.runtime_run_id && entry.role === 'assistant')
    expect(repaired).toMatchObject({ status: 'failed', entry_type: 'error' })
    const replay = await apiRequest(`/ai/agent-chatbox/runs/${activeRun.runtime_run_id}/events`, { auth })
    const terminal = replay.data.events.filter((event) => ['run.completed', 'run.failed', 'run.cancelled', 'run.timeout', 'run.interrupted'].includes(event.event_type))
    expect(terminal).toHaveLength(1)
    expect(terminal[0]).toMatchObject({ event_type: 'run.interrupted' })
    expect(replay.data.events.at(-1)).toMatchObject({ event_type: 'run.interrupted' })
  } finally {
    await startContainer(ownerContainer)
  }
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1-04 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p104-test-key', token_type: 'bearer' }
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
