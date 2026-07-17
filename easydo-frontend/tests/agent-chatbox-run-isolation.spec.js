import { expect, test } from '@playwright/test'

const apiBase = `${process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'}/api`

test('Agent Chatbox keeps historical Pi events isolated by runtime run', async ({ page }) => {
  const session = await loginAdmin()
  const suffix = Date.now()
  const firstPrompt = `P0-01 first prompt ${suffix}`
  const secondPrompt = `P0-01 second prompt ${suffix}`
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    token: session.token,
    workspaceId: session.workspaceId,
    data: {
      name: `P0-01 Replay Isolation ${suffix}`,
      description: 'Playwright validation for runtime run event ownership',
      profile_kind: 'generic',
      context_tags: [],
      provider: {},
      model: {},
      inference: {},
      prompt: { system: 'Return a concise answer.' },
      status: 'draft'
    }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    token: session.token,
    workspaceId: session.workspaceId,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: 'latest',
      title: `P0-01 ${suffix}`
    }
  })

  await createFailedPiRun(session, chatSession.data.id, firstPrompt, `p001-first-${suffix}`)
  await createFailedPiRun(session, chatSession.data.id, secondPrompt, `p001-second-${suffix}`)

  const entries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, {
    token: session.token,
    workspaceId: session.workspaceId
  })
  const assistantEntries = entries.data.filter((entry) => entry.role === 'assistant')
  expect(assistantEntries).toHaveLength(2)
  const expectedRunIDs = assistantEntries.map((entry) => entry.runtime_run_id)
  expect(new Set(expectedRunIDs).size).toBe(2)

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, session)

  const replayResponses = new Map()
  page.on('response', async (response) => {
    const match = response.url().match(/\/api\/ai\/agent-chatbox\/runs\/([^/?]+)\/events/)
    if (!match || !response.ok()) return
    replayResponses.set(decodeURIComponent(match[1]), await response.json())
  })

  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  await expect(page.locator('.turn.user')).toHaveCount(2)
  await expect(page.locator('.turn.assistant')).toHaveCount(2)
  await expect(page.locator('.turn.user').nth(0)).toContainText(firstPrompt)
  await expect(page.locator('.turn.user').nth(1)).toContainText(secondPrompt)

  const assistantTurns = page.locator('.turn.assistant')
  await assistantTurns.nth(0).getByRole('button', { name: '刷新轨迹' }).click()
  await assistantTurns.nth(1).getByRole('button', { name: '刷新轨迹' }).click()
  await expect.poll(() => replayResponses.size).toBe(2)

  for (const [index, runtimeRunID] of expectedRunIDs.entries()) {
    const response = replayResponses.get(runtimeRunID)
    expect(response?.data?.runtime_run_id).toBe(runtimeRunID)
    expect(response.data.events.length).toBeGreaterThan(0)
    expect(response.data.events.every((event) => event.runtime_run_id === runtimeRunID)).toBe(true)
    expect(JSON.stringify(response.data.events)).toContain(index === 0 ? firstPrompt : secondPrompt)
    expect(JSON.stringify(response.data.events)).not.toContain(index === 0 ? secondPrompt : firstPrompt)
    await expect(assistantTurns.nth(index).locator('.ai-runtime-event-line')).not.toHaveCount(0)
  }
})

async function createFailedPiRun(session, sessionId, content, clientEntryId) {
  const response = await fetch(`${apiBase}/ai/agent-chatbox/sessions/${sessionId}/entries/stream`, {
    method: 'POST',
    headers: requestHeaders(session),
    body: JSON.stringify({ content, client_entry_id: clientEntryId })
  })
  const body = await response.text()
  expect(response.ok).toBeTruthy()
  expect(body).toContain('assistant_entry')
}

async function loginAdmin() {
  const login = await apiRequest('/auth/login', {
    method: 'POST',
    data: { username: 'admin', password: '1qaz2WSX' }
  })
  const userinfo = await apiRequest('/auth/userinfo', { token: login.data.token })
  return { token: login.data.token, workspaceId: userinfo.data.workspaces[0].id }
}

function requestHeaders(session) {
  return {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${session.token}`,
    'X-Workspace-ID': String(session.workspaceId)
  }
}

async function apiRequest(path, options = {}) {
  const response = await fetch(`${apiBase}${path}`, {
    method: options.method || 'GET',
    headers: {
      'Content-Type': 'application/json',
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}),
      ...(options.workspaceId ? { 'X-Workspace-ID': String(options.workspaceId) } : {})
    },
    body: options.data ? JSON.stringify(options.data) : undefined
  })
  const payload = await response.json()
  if (!response.ok) throw new Error(payload?.message || `Request failed: ${response.status}`)
  return payload
}
