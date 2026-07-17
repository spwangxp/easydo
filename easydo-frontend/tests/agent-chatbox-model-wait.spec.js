import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P102_PROVIDER_BASE_URL || ''

test('Agent Chatbox keeps a 30-second pre-token model wait visible and closes the same phase', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P102_PROVIDER_BASE_URL must point to a 35-second delayed Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P1-02 Model Wait ${suffix}`,
      description: 'Playwright validation for the visible pre-token model phase',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
      model: { provider_model_key: 'p102-delayed-model' },
      provider_credential_ref: { credential_id: providerCredential.data.id },
      inference: { max_tokens: 32 },
      prompt: { system: 'Return the provider response without tools.' },
      status: 'draft'
    }
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-02 model wait acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-02 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(`P1-02 delayed first token ${suffix}`)
  await composer.press('Enter')

  const modelPhase = page.locator('.ai-runtime-event-line--thought').filter({ hasText: '正在请求模型' })
  await expect(modelPhase).toBeVisible({ timeout: 10000 })
  await expect(modelPhase).toContainText('p102-delayed-model')
  await expect(modelPhase).toContainText(/等待首个响应 (?:3[0-9]|[4-9][0-9]) 秒/, { timeout: 33000 })

  await expect(page.locator('.turn.assistant .final-answer')).toContainText('P1-02 delayed response', { timeout: 15000 })
  await expect(composer).toBeEnabled()
  await expect(page.locator('.ai-runtime-event-line--thought.ai-runtime-event-line--running')).toHaveCount(0)
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1-02 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p102-test-key', token_type: 'bearer' }
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
