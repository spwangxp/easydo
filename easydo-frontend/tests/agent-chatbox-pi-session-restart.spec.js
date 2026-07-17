import { expect, test } from '@playwright/test'
import { composeServiceContainer, restartContainer } from './helpers/easydoDocker.js'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`
const providerBaseURL = process.env.EASYDO_P103_PROVIDER_BASE_URL || ''

test('Agent Chatbox rebuilds Pi conversation context after the Runtime process restarts', async ({ page }) => {
  test.skip(!providerBaseURL, 'EASYDO_P103_PROVIDER_BASE_URL must point to the context-checking Docker-network provider')

  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const firstPrompt = `remember alpha ${suffix}`
  const secondPrompt = `what did I ask before ${suffix}`
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: {
      name: `P1-03 Pi Restart ${suffix}`,
      description: 'Playwright validation for canonical Pi session reconstruction',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_type: 'openai-compatible', base_url: providerBaseURL },
      model: { provider_model_key: 'p103-context-model' },
      provider_credential_ref: { credential_id: providerCredential.data.id },
      inference: { max_tokens: 32 },
      prompt: { system: 'Return the provider response without tools.' },
      status: 'draft'
    }
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-03 Pi restart acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-03 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(firstPrompt)
  await composer.press('Enter')
  await expect(page.locator('.turn.assistant .final-answer').last()).toContainText('P1-03 first response', { timeout: 15000 })

  await restartContainer(await composeServiceContainer('ai-runtime'))

  await composer.fill(secondPrompt)
  await composer.press('Enter')
  await expect(page.locator('.turn.assistant .final-answer').last()).toContainText('P1-03 CONTEXT_OK', { timeout: 15000 })
  const latestTrace = page.locator('.turn.assistant').last()
  await expect(latestTrace).toContainText('运行已完成')
  await expect(latestTrace.locator('.ai-runtime-event-line.running')).toHaveCount(0)

  const entries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  expect(entries.data.filter((entry) => entry.role === 'user').map((entry) => entry.content)).toEqual([firstPrompt, secondPrompt])
  expect(entries.data.filter((entry) => entry.role === 'assistant').map((entry) => entry.content)).toEqual([
    'P1-03 first response',
    'P1-03 CONTEXT_OK'
  ])
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1-03 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: 'p103-test-key', token_type: 'bearer' }
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
