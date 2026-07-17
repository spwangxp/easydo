import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P002_PROVIDER_BASE_URL || ''

test('Page Assistant restores and cancels the same durable active Run after reload', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P002_PROVIDER_BASE_URL must point to a delayed Docker-network provider')

  const auth = await loginAdmin()
  const providerCredential = await createProviderCredential(auth, Date.now())
  const profiles = await apiRequest('/store/ai-agents/profiles?profile_name=page-ai-assistant', { auth })
  const profile = (profiles.data?.items || profiles.data || []).find((item) => item.name === 'page-ai-assistant')
  expect(profile?.id).toBeTruthy()
  await apiRequest(`/store/ai-agents/profiles/${profile.id}`, {
    method: 'PUT',
    auth,
    data: {
      name: 'page-ai-assistant',
      description: 'Page Assistant active-run acceptance profile',
      profile_kind: 'generic',
      context_tags: ['page-assistant'],
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
      model: { provider_model_key: 'p002-delayed-model' },
      provider_credential_ref: { credential_id: providerCredential.data.id },
      inference: { max_tokens: 32 },
      prompt: { system: 'Answer using the current EasyDo page context.' },
      status: 'draft'
    }
  })
  await apiRequest(`/store/ai-agents/profiles/${profile.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P0-05 page assistant active run acceptance' }
  })

  const current = await apiRequest('/ai/sessions/current', {
    method: 'POST',
    auth,
    data: {
      profile_id: profile.id,
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: String(auth.workspaceId),
      source: 'page-ai-assistant',
      context_tags: ['page-assistant'],
      title: 'Page Assistant'
    }
  })
  await apiRequest(`/ai/sessions/${current.data.id}/model`, {
    method: 'PUT',
    auth,
    data: {
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
      model: { provider_model_key: 'p002-delayed-model' },
      provider_credential_ref: { credential_id: providerCredential.data.id },
      inference: { max_tokens: 32 }
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto('/')
  await page.locator('.assistant-fab').click()
  const composer = page.getByPlaceholder('输入问题')
  await composer.fill(`P0-05 page assistant ${Date.now()}`)
  await composer.press('Enter')

  let runtimeRunID = ''
  await expect.poll(async () => {
    const session = await apiRequest(`/ai/sessions/${current.data.id}`, { auth })
    runtimeRunID = session.data.active_run?.runtime_run_id || ''
    return runtimeRunID
  }, { timeout: 10000 }).not.toBe('')

  await page.reload()
  await page.locator('.assistant-fab').click()
  await expect(page.locator('.assistant-stop')).toBeVisible({ timeout: 10000 })
  await expect(composer).toBeEnabled()
  await page.locator('.assistant-stop').click()

  await expect(composer).toBeEnabled({ timeout: 10000 })
  await expect.poll(async () => {
    const response = await apiRequest(`/ai/sessions/${current.data.id}`, { auth })
    return response.data.active_run
  }, { timeout: 10000 }).toBeFalsy()
  const session = await apiRequest(`/ai/sessions/${current.data.id}`, { auth })
  expect(session.data.active_run).toBeFalsy()
  const entries = await apiRequest(`/ai/sessions/${current.data.id}/entries`, { auth })
  const assistant = entries.data.find((entry) => entry.runtime_run_id === runtimeRunID && entry.role === 'assistant')
  expect(assistant).toMatchObject({ status: 'cancelled' })
  const events = await apiRequest(`/ai/runs/${runtimeRunID}/events`, { auth })
  const terminal = events.data.events.filter((event) => event.event_type.startsWith('run.') && event.event_type !== 'run.started')
  expect(terminal.at(-1)).toMatchObject({ event_type: 'run.cancelled' })
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P0-05 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p005-test-key', token_type: 'bearer' }
    }
  })
}

async function loginAdmin() {
  const login = await apiRequest('/auth/login', {
    method: 'POST',
    data: { username: 'admin', password: '1qaz2WSX' }
  })
  const info = await apiRequest('/auth/userinfo', { token: login.data.token })
  return { token: login.data.token, workspaceId: info.data.workspaces[0].id }
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
