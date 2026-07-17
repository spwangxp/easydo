import { expect, test } from '@playwright/test'
import { runtimeContainerForRun, runtimeRunResult, startContainer, stopContainer } from './helpers/easydoDocker.js'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P002_PROVIDER_BASE_URL || ''

test('Agent Chatbox queue steer is injected once across Runtime owner failover', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P002_PROVIDER_BASE_URL must point to a delayed Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const credential = await createProviderCredential(auth, suffix)
  const profile = await createDelayedProfile(auth, credential.data.id, `P1-11 Queue Owner Failover ${suffix}`)
  const published = await publishProfile(auth, profile.data.id)
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-11 owner failover ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(`P1-11 active run ${suffix}`)
  await composer.press('Enter')

  let activeRun
  await expect.poll(async () => {
    const session = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
    activeRun = session.data.active_run
    return activeRun?.runtime_run_id || ''
  }, { timeout: 10000 }).not.toBe('')

  const clientItemID = `queue-owner-failover-${suffix}`
  const steerContent = `owner failover steer ${suffix}`
  const enqueued = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/queue-items`, {
    method: 'POST',
    auth,
    data: { mode: 'steer', content: steerContent, client_item_id: clientItemID }
  })
  const queueItemID = enqueued.data.item.queue_item_id

  await expect.poll(async () => {
    const [item] = await queueItems(auth, chatSession.data.id, clientItemID)
    return item?.status || ''
  }, { timeout: 15000 }).toBe('applied')

  const ownerContainer = await runtimeContainerForRun(activeRun.runtime_run_id)
  await stopContainer(ownerContainer)

  try {
    await expect.poll(async () => {
      const session = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}`, { auth })
      return session.data.active_run
    }, { timeout: 45000 }).toBeFalsy()

    const [itemAfterFailover] = await queueItems(auth, chatSession.data.id, clientItemID)
    expect(itemAfterFailover).toMatchObject({ status: 'applied', client_item_id: clientItemID })
    const run = await runtimeRunResult(activeRun.runtime_run_id)
    const queueSteers = Array.isArray(run.queue_steers) ? run.queue_steers : []
    expect(queueSteers.filter((item) => item.queue_item_id === queueItemID)).toHaveLength(1)
    expect(queueSteers.find((item) => item.queue_item_id === queueItemID)).toMatchObject({ content: steerContent, status: 'applied' })
    const replay = await apiRequest(`/ai/agent-chatbox/runs/${activeRun.runtime_run_id}/events`, { auth })
    const appliedEvents = replay.data.events.filter((event) => {
      if (event.event_type !== 'session.steer.applied') return false
      const payload = event.payload_json || event
      return payload?.queue_item?.queue_item_id === queueItemID
    })
    expect(appliedEvents).toHaveLength(1)
  } finally {
    await startContainer(ownerContainer)
  }
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1-11 Queue Owner Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p111-key', token_type: 'bearer' }
    }
  })
}

async function createDelayedProfile(auth, credentialID, name) {
  return apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name,
      description: 'Playwright validation for queue owner failover',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
      model: { provider_model_key: 'p002-delayed-model' },
      provider_credential_ref: { credential_id: credentialID },
      inference: { max_tokens: 32 },
      prompt: { system: 'Return the provider response without tools.' },
      status: 'draft'
    }
  })
}

function publishProfile(auth, profileID) {
  return apiRequest(`/store/ai-agents/profiles/${profileID}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-11 queue owner failover acceptance' }
  })
}

async function queueItems(auth, sessionID, clientItemID) {
  const queue = await apiRequest(`/ai/agent-chatbox/sessions/${sessionID}/queue-items`, { auth })
  return queue.data.items.filter((item) => item.client_item_id === clientItemID)
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
