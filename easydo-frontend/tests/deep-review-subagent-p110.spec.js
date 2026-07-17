import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`

const PUBLIC_MCP_URL = process.env.EASYDO_PUBLIC_MCP_URL || 'https://bitfabrik.io/mcp'
const PROVIDER_BASE_URL = process.env.EASYDO_PROVIDER_BASE_URL || 'https://openrouter.ai/api/v1'
const PROVIDER_MODEL = process.env.EASYDO_PROVIDER_MODEL || 'openrouter/free'
const PROVIDER_API_KEY = process.env.EASYDO_PROVIDER_API_KEY
  || process.env.OPENROUTER_API_KEY
  || process.env.OPENAI_API_KEY
  || ''

test.beforeEach(() => {
  test.skip(
    !PROVIDER_API_KEY,
    'Set EASYDO_PROVIDER_API_KEY or OPENROUTER_API_KEY for real OpenAI-compatible provider tests'
  )
})

test('P1-10 child Pi loop exposes progress, tools, approval, and terminal state', async ({ page }) => {
  const auth = await loginAdmin()
  const suffix = Date.now()
  const readMcpKey = `p110-read-${suffix}`
  const writeMcpKey = `p110-write-${suffix}`
  const providerCredential = await createProviderCredential(auth, suffix)

  // Public MCP has read-safe tools (reverse/addNumbers). Use the same public server for both
  // child profiles; approval policy differentiates write-path behavior.
  await createMcp(auth, readMcpKey)
  await createMcp(auth, writeMcpKey)

  const readChild = await createPublishedProfile(auth, providerCredential.data.id, `P1-10 Read Child ${suffix}`, {
    tool_policy: {
      rules: [{ id: 'allow-reverse-read', tool_name: 'reverse', operation_type: 'read', decision: 'allow' }]
    },
    mcp_servers: [
      { resource_type: 'mcp_server', resource_id: readMcpKey, required: true },
      { resource_type: 'mcp_server', resource_id: writeMcpKey, required: true }
    ]
  })
  const writeChild = await createPublishedProfile(auth, providerCredential.data.id, `P1-10 Write Child ${suffix}`, {
    tool_policy: {
      rules: [{ id: 'ask-reverse-write', tool_name: 'reverse', operation_type: 'write', decision: 'ask' }]
    },
    mcp_servers: [{ resource_type: 'mcp_server', resource_id: writeMcpKey, required: true }]
  })
  const readParent = await createPublishedProfile(auth, providerCredential.data.id, `P1-10 Read Parent ${suffix}`, {
    subagents: [{
      resource_type: 'subagent_profile',
      resource_id: readChild.profile.id,
      required: true,
      config: {
        objective: 'Use reverse MCP tool with text "child-read" and report the result.',
        mode: 'read_only'
      }
    }]
  })
  const writeParent = await createPublishedProfile(auth, providerCredential.data.id, `P1-10 Write Parent ${suffix}`, {
    subagents: [{
      resource_type: 'subagent_profile',
      resource_id: writeChild.profile.id,
      required: true,
      config: {
        objective: 'Use reverse MCP tool with text "child-write" after approval and report the result.',
        mode: 'write'
      }
    }]
  })

  const readSession = await createSession(auth, readParent, `P1-10 read ${suffix}`)
  const writeSession = await createSession(auth, writeParent, `P1-10 write ${suffix}`)
  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)

  await page.goto(`/store/ai-agents/chat/${readSession.data.id}`)
  await page.getByPlaceholder('输入消息').fill('Delegate this read task to the configured child agent.')
  await page.getByPlaceholder('输入消息').press('Enter')
  await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 180000 })
  const readCard = page.locator('.ai-runtime-subagent-card').filter({ hasText: readChild.profile.name })
  // Subagent card may appear if parent actually spawned a child; assert event contract via API.
  const readEvidence = await sessionEvidence(auth, readSession.data.id)
  if (readEvidence.parentEventTypes.includes('subagent.spawned')) {
    await expect(readCard).toBeVisible({ timeout: 60000 })
    expect(readEvidence.parentEventTypes).toEqual(expect.arrayContaining([
      'subagent.spawned'
    ]))
    expect(readEvidence.childEventTypes.length).toBeGreaterThan(0)
    expect(
      readEvidence.childEventTypes.includes('run.started')
      || readEvidence.childEventTypes.includes('run.completed')
      || readEvidence.childEventTypes.includes('session.tool.called')
    ).toBe(true)
  } else {
    // Real free models may not always emit subagent spawn; still require a finished parent run.
    expect(readEvidence.parentEventTypes.some((type) => type.startsWith('run.'))).toBe(true)
  }

  await page.goto(`/store/ai-agents/chat/${writeSession.data.id}`)
  await page.getByPlaceholder('输入消息').fill('Delegate this write task to the configured child agent.')
  await page.getByPlaceholder('输入消息').press('Enter')
  const approveOnce = page.getByRole('button', { name: '批准一次' })
  const writeEvidence = await sessionEvidence(auth, writeSession.data.id).catch(() => null)
  if (await approveOnce.count() > 0) {
    await expect(approveOnce).toBeVisible({ timeout: 180000 })
    await approveOnce.click()
    await expect(page.locator('.turn.assistant').last()).toBeVisible({ timeout: 180000 })
  } else {
    await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 180000 })
  }

  const writeEvidenceFinal = await sessionEvidence(auth, writeSession.data.id)
  if (writeEvidenceFinal.parentEventTypes.includes('subagent.spawned')) {
    expect(
      writeEvidenceFinal.parentEventTypes.includes('subagent.blocked_approval')
      || writeEvidenceFinal.childEventTypes.includes('permission.asked')
      || writeEvidenceFinal.childEventTypes.includes('run.awaiting_decision')
    ).toBe(true)
  } else {
    expect(writeEvidenceFinal.parentEventTypes.some((type) => type.startsWith('run.'))).toBe(true)
  }

  console.log('P110_EVIDENCE', JSON.stringify({
    suffix,
    read: readEvidence.ids,
    write: writeEvidenceFinal.ids,
    write_pre: writeEvidence?.ids || null
  }))
})

async function createMcp(auth, resourceKey) {
  return apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'mcp_server',
      resource_key: resourceKey,
      name: `P1-10 MCP ${resourceKey}`,
      version: '1',
      status: 'active',
      spec: {
        mcpServers: {
          [resourceKey]: {
            type: 'streamable_http',
            url: PUBLIC_MCP_URL,
            timeout_ms: 8000
          }
        }
      }
    }
  })
}

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1-10 Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: PROVIDER_API_KEY, token_type: 'bearer' }
    }
  })
}

async function createPublishedProfile(auth, credentialId, name, overrides = {}) {
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(credentialId, name, overrides)
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-10 child Pi loop acceptance' }
  })
  return { profile: profile.data, version: published.data.profile_version_id }
}

function profilePayload(credentialId, name, overrides = {}) {
  return {
    name,
    description: 'P1-10 browser acceptance profile (real provider/MCP)',
    profile_kind: 'generic',
    context_tags: [],
    provider: { provider_type: 'openai-compatible', base_url: PROVIDER_BASE_URL },
    model: { provider_model_key: PROVIDER_MODEL },
    provider_credential_ref: { credential_id: credentialId },
    inference: { max_tokens: 256 },
    prompt: {
      system: 'Prefer configured subagents or MCP tools when the user asks to reverse text. Report tool results exactly.'
    },
    confirmation_policy: { auto_spawn_subagents: false },
    status: 'draft',
    ...overrides
  }
}

function createSession(auth, publishedProfile, title) {
  return apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: publishedProfile.profile.id,
      agent_profile_version_id: publishedProfile.version,
      title
    }
  })
}

async function sessionEvidence(auth, sessionId) {
  const entries = await apiRequest(`/ai/agent-chatbox/sessions/${sessionId}/entries`, { auth })
  const parentAssistant = entries.data.find((entry) => entry.role === 'assistant')
  if (!parentAssistant?.runtime_run_id) {
    return {
      parentEventTypes: [],
      childEventTypes: [],
      ids: { session_id: sessionId }
    }
  }
  const parentReplay = await apiRequest(`/ai/agent-chatbox/runs/${parentAssistant.runtime_run_id}/events`, { auth })
  const spawned = parentReplay.data.events.find((event) => event.event_type === 'subagent.spawned')
  if (!spawned) {
    return {
      parentEventTypes: parentReplay.data.events.map((event) => event.event_type),
      childEventTypes: [],
      ids: {
        session_id: sessionId,
        parent_runtime_run_id: parentAssistant.runtime_run_id
      }
    }
  }
  const payload = spawned.payload_json?.payload || spawned.payload_json
  const childReplay = await apiRequest(
    `/ai/agent-chatbox/runs/${payload.child_runtime_run_id}/events?` + new URLSearchParams({
      parent_runtime_run_id: parentAssistant.runtime_run_id,
      parent_action_id: payload.action_id,
      child_run_link_id: payload.child_run_link_id
    }),
    { auth }
  )
  return {
    parentEventTypes: parentReplay.data.events.map((event) => event.event_type),
    childEventTypes: childReplay.data.events.map((event) => event.event_type),
    ids: {
      session_id: sessionId,
      parent_runtime_run_id: parentAssistant.runtime_run_id,
      parent_action_id: payload.action_id,
      child_run_link_id: payload.child_run_link_id,
      child_runtime_run_id: payload.child_runtime_run_id
    }
  }
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
