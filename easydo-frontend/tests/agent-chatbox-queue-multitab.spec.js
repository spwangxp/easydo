import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`

test('Agent Chatbox synchronizes one durable queue across two tabs', async ({ browser }) => {
  const auth = await loginAdmin()
  const suffix = Date.now()
  const credential = await createProviderCredential(auth, suffix)
  const profile = await createProfile(auth, credential.data.id, `P1-11 Queue Multitab ${suffix}`)
  const published = await publishProfile(auth, profile.data.id)
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-11 queue multitab ${suffix}`
    }
  })

  const firstContext = await browser.newContext()
  const secondContext = await browser.newContext()
  try {
    await seedAuth(firstContext, auth)
    await seedAuth(secondContext, auth)
    const firstPage = await firstContext.newPage()
    const secondPage = await secondContext.newPage()
    await Promise.all([
      firstPage.goto(`/store/ai-agents/chat/${chatSession.data.id}`),
      secondPage.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
    ])

    const clientItemID = `queue-multitab-${suffix}`
    const content = `browser queue sync ${suffix}`
    await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/queue-items`, {
      method: 'POST',
      auth,
      data: { mode: 'follow_up', content, client_item_id: clientItemID }
    })

    await expect.poll(async () => queueItems(auth, chatSession.data.id, clientItemID).then((items) => items.length), { timeout: 10000 }).toBe(1)
    await Promise.all([firstPage.reload(), secondPage.reload()])
    await Promise.all([
      expect(firstPage.locator('.chatbox-queue-panel')).toContainText(content, { timeout: 10000 }),
      expect(secondPage.locator('.chatbox-queue-panel')).toContainText(content, { timeout: 10000 })
    ])

    await expect.poll(async () => {
      const [item] = await queueItems(auth, chatSession.data.id, clientItemID)
      return item?.status || ''
    }, { timeout: 10000 }).toBe('consumed')
    const [queued] = await queueItems(auth, chatSession.data.id, clientItemID)
    expect(queued).toMatchObject({ content, status: 'consumed', client_item_id: clientItemID })
    await Promise.all([firstPage.reload(), secondPage.reload()])
    await Promise.all([
      expect(firstPage.locator('.chatbox-queue-panel')).toContainText('已消费', { timeout: 10000 }),
      expect(secondPage.locator('.chatbox-queue-panel')).toContainText('已消费', { timeout: 10000 })
    ])
  } finally {
    await Promise.all([firstContext.close(), secondContext.close()])
  }
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1-11 Queue Multitab Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p111-key', token_type: 'bearer' }
    }
  })
}

async function createProfile(auth, credentialID, name) {
  return apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name,
      description: 'Playwright validation for durable queue synchronization',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_type: 'openai-compatible', base_url: 'http://queue-multitab.invalid/v1' },
      model: { provider_model_key: 'queue-multitab/model' },
      provider_credential_ref: { credential_id: credentialID },
      inference: { max_tokens: 32 },
      prompt: { system: 'Queue multitab validation profile.' },
      status: 'draft'
    }
  })
}

function publishProfile(auth, profileID) {
  return apiRequest(`/store/ai-agents/profiles/${profileID}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-11 queue acceptance' }
  })
}

async function queueItems(auth, sessionID, clientItemID) {
  const queue = await apiRequest(`/ai/agent-chatbox/sessions/${sessionID}/queue-items`, { auth })
  return queue.data.items.filter((item) => item.client_item_id === clientItemID)
}

async function seedAuth(context, auth) {
  await context.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
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
