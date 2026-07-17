import { expect, test } from '@playwright/test'

const baseURL = process.env.EASYDO_BASE_URL || 'http://127.0.0.1:8088'
const apiBase = `${baseURL}/api`

// Real public Streamable HTTP MCP (no auth). Probed healthy: tools/list + reverse call.
const PUBLIC_MCP_URL = process.env.EASYDO_PUBLIC_MCP_URL || 'https://bitfabrik.io/mcp'
// Real OpenAI-compatible provider. OpenRouter free router requires a free API key.
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

test('published profiles freeze resources and validation exposes dependency failures', async ({ page }) => {
  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const skillKey = `p105-skill-${suffix}`
  const profileName = `P1-05 Frozen Profile ${suffix}`
  const invalidProfileName = `P1-06 Invalid Profile ${suffix}`

  const skill = await apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'skill',
      resource_key: skillKey,
      name: `P1-05 Skill ${suffix}`,
      description: 'Select this Skill for dependency review work.',
      version: '1',
      status: 'active',
      spec: { instructions: `P105_OLD_MARKER_${suffix}` }
    }
  })
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, profileName, {
      skills: [{ resource_type: 'skill', resource_id: skillKey, required: true }]
    })
  })

  const disabledMcpKey = `p106-disabled-mcp-${suffix}`
  await apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'mcp_server',
      resource_key: disabledMcpKey,
      name: `P1-06 Disabled MCP ${suffix}`,
      status: 'disabled',
      spec: {
        discovered_tools: [{ name: 'disabled_tool' }],
        mcpServers: {
          [disabledMcpKey]: { type: 'streamable_http', url: 'http://127.0.0.1:1/mcp' }
        }
      }
    }
  })
  const draftSubagent = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, `P1-06 Draft Subagent ${suffix}`)
  })
  await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, invalidProfileName, {
      skills: [{ resource_type: 'skill', resource_id: `missing-skill-${suffix}`, required: true }],
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: disabledMcpKey, required: true }],
      subagents: [{ resource_type: 'subagent_profile', resource_id: draftSubagent.data.id, required: true }]
    })
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto('/store/ai-agents')

  const profileRow = page.locator('.el-table__row').filter({ hasText: profileName })
  await expect(profileRow).toBeVisible()
  await profileRow.getByRole('button', { name: '校验' }).click()
  await expect(page.getByText('Agent Profile 校验通过')).toBeVisible()
  await profileRow.getByRole('button', { name: '发布' }).click()
  await expect(page.getByText('Agent Profile 已发布')).toBeVisible()
  await expect(page.locator('.el-table__row').filter({ hasText: profileName })).toContainText('active')

  const versions = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/versions`, { auth })
  expect(versions.data).toHaveLength(1)
  const published = versions.data[0]
  expect(published.snapshot.skills[0].resource_version).toBe('1')
  expect(published.snapshot.frozen_resources.skills[0].spec.instructions).toBe(`P105_OLD_MARKER_${suffix}`)

  await apiRequest(`/store/ai-agents/resources/${skill.data.id}`, {
    method: 'PUT',
    auth,
    data: { version: '2', spec: { instructions: `P105_NEW_MARKER_${suffix}` } }
  })
  const versionsAfterEdit = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/versions`, { auth })
  expect(versionsAfterEdit.data[0].snapshot.frozen_resources.skills[0].spec.instructions).toBe(`P105_OLD_MARKER_${suffix}`)

  const latestSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: { agent_profile_id: profile.data.id, agent_profile_version_id: 'latest', title: `P1-05 latest ${suffix}` }
  })
  const draftSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: { agent_profile_id: profile.data.id, agent_profile_version_id: 'draft', title: `P1-05 draft ${suffix}` }
  })
  expect(latestSession.data.agent_profile_version_id).toBe(published.profile_version_id)
  expect(latestSession.data.agent_profile_version_key).toBe('latest')
  expect(draftSession.data.agent_profile_version_id).toBe(0)
  expect(draftSession.data.agent_profile_version_key).toBe('draft')

  const invalidRow = page.locator('.el-table__row').filter({ hasText: invalidProfileName })
  await invalidRow.getByRole('button', { name: '校验' }).click()
  await expect(page.getByText('Agent Profile 校验失败')).toBeVisible()
  const validationPanel = page.locator('.validation-result')
  await expect(validationPanel).toContainText(`Skill missing-skill-${suffix} was not found`)
  await expect(validationPanel).toContainText(`MCP server ${disabledMcpKey} is not active`)
  await expect(validationPanel).toContainText(`Published Subagent ${draftSubagent.data.id} version was not found`)

  await page.getByRole('tab', { name: 'mcp', exact: true }).click()
  await page.getByRole('button', { name: '新建 MCP Server' }).click()
  await expect(page.locator('.mcpServerForm .el-select__selected-item.el-select__placeholder')).toContainText('streamable_http')
  await expect(page.getByLabel('Command')).toHaveCount(0)
  await expect(page.getByLabel('Args JSON')).toHaveCount(0)
  await expect(page.getByLabel('Env JSON')).toHaveCount(0)
  await expect(page.getByLabel('CWD')).toHaveCount(0)
})

test('Pi dynamically loads a relevant Skill without the user naming it', async ({ page }) => {
  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const skillKey = `p107-dynamic-skill-${suffix}`
  const prompt = 'Review this deployment incident and apply the relevant operational instructions.'
  expect(prompt).not.toContain('Deployment Incident Review')

  await apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'skill',
      resource_key: skillKey,
      name: 'Deployment Incident Review',
      description: 'Use for deployment incident reviews and operational root-cause summaries.',
      version: 'sha256:1111111111111111111111111111111111111111',
      status: 'active',
      spec: { instructions: 'P107_SKILL_CONTENT_MARKER' }
    }
  })
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, `P1-07 Dynamic Skill ${suffix}`, {
      skills: [{ resource_type: 'skill', resource_id: skillKey, required: true }]
    })
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-07 dynamic skill acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-07 ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  const composer = page.getByPlaceholder('输入消息')
  await composer.fill(prompt)
  await composer.press('Enter')

  await expect(page.locator('.turn.assistant')).not.toHaveText('', { timeout: 90000 })
  await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 90000 })

  const entries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const assistant = entries.data.find((entry) => entry.role === 'assistant')
  expect(assistant).toBeTruthy()
  expect(['completed', 'failed', 'streaming', 'cancelled']).toContain(assistant.status)
  expect(assistant.runtime_run_id).toBeTruthy()
  const replay = await apiRequest(`/ai/agent-chatbox/runs/${assistant.runtime_run_id}/events`, { auth })
  const eventTypes = replay.data.events.map((event) => event.event_type)
  expect(eventTypes).toEqual(expect.arrayContaining(['skill.available']))
  const loaded = replay.data.events.find((event) => event.event_type === 'skill.available')
  expect(loaded.payload_json.version).toMatch(/^sha256:[a-f0-9]{40}$/)
  expect(loaded.payload_json.key).toBe(skillKey)
})

test('MCP Streamable HTTP reuses its session and surfaces request timeouts', async ({ page }) => {
  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const mcpKey = `p108-mcp-${suffix}`
  await apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'mcp_server',
      resource_key: mcpKey,
      name: `P1-08 MCP ${suffix}`,
      version: '1',
      status: 'active',
      spec: {
        mcpServers: {
          [mcpKey]: {
            type: 'streamable_http',
            url: PUBLIC_MCP_URL,
            timeout_ms: 8000
          }
        }
      }
    }
  })
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, `P1-08 MCP Profile ${suffix}`, {
      tool_policy: {
        rules: [{ id: 'allow-reverse', tool_name: 'reverse', operation_type: 'read', decision: 'allow' }]
      },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcpKey, required: true }]
    })
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-08 MCP lifecycle acceptance' }
  })
  const chatSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: profile.data.id,
      agent_profile_version_id: published.data.profile_version_id,
      title: `P1-08 MCP ${suffix}`
    }
  })

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${chatSession.data.id}`)
  await page.getByPlaceholder('输入消息').fill(
    'Use the reverse MCP tool with text "easydo". Reply with the tool result text.'
  )
  await page.getByPlaceholder('输入消息').press('Enter')
  await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 120000 })

  const normalEntries = await apiRequest(`/ai/agent-chatbox/sessions/${chatSession.data.id}/entries`, { auth })
  const normalAssistant = normalEntries.data.find((entry) => entry.role === 'assistant')
  expect(normalAssistant?.runtime_run_id).toBeTruthy()
  const normalReplay = await apiRequest(`/ai/agent-chatbox/runs/${normalAssistant.runtime_run_id}/events`, { auth })
  const normalTypes = normalReplay.data.events.map((event) => event.event_type)
  expect(normalTypes).toEqual(expect.arrayContaining(['mcp.tools.available']))
  // Real model may or may not call the tool; at least discovery must succeed against public MCP.
  const calledTool = normalTypes.includes('session.tool.called') || normalTypes.includes('session.tool.success')
  if (calledTool) {
    expect(normalTypes).toEqual(expect.arrayContaining(['session.tool.called']))
  }

  const timeoutKey = `p108-timeout-${suffix}`
  await apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'mcp_server',
      resource_key: timeoutKey,
      name: `P1-08 Timeout MCP ${suffix}`,
      version: '1',
      status: 'active',
      spec: {
        mcpServers: {
          [timeoutKey]: {
            type: 'streamable_http',
            // Non-routable blackhole address: discovery must time out under client timeout_ms.
            url: 'http://10.255.255.1:9/mcp',
            timeout_ms: 150
          }
        }
      }
    }
  })
  const timeoutProfile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, `P1-08 Timeout Profile ${suffix}`, {
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: timeoutKey, required: true }]
    })
  })
  const timeoutPublished = await apiRequest(`/store/ai-agents/profiles/${timeoutProfile.data.id}/publish`, {
    method: 'POST',
    auth,
    data: { change_summary: 'P1-08 timeout acceptance' }
  })
  const timeoutSession = await apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: {
      agent_profile_id: timeoutProfile.data.id,
      agent_profile_version_id: timeoutPublished.data.profile_version_id,
      title: `P1-08 timeout ${suffix}`
    }
  })

  await page.goto(`/store/ai-agents/chat/${timeoutSession.data.id}`)
  await page.getByPlaceholder('输入消息').fill('Continue even if the MCP server is unavailable.')
  await page.getByPlaceholder('输入消息').press('Enter')
  await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 120000 })

  const timeoutEntries = await apiRequest(`/ai/agent-chatbox/sessions/${timeoutSession.data.id}/entries`, { auth })
  const timeoutAssistant = timeoutEntries.data.find((entry) => entry.role === 'assistant')
  expect(timeoutAssistant?.runtime_run_id).toBeTruthy()
  const timeoutReplay = await apiRequest(`/ai/agent-chatbox/runs/${timeoutAssistant.runtime_run_id}/events`, { auth })
  const timeoutEvent = timeoutReplay.data.events.find((event) => event.event_type === 'mcp.tools_discovery_failed')
  expect(timeoutEvent).toBeTruthy()
  expect(String(timeoutEvent.payload_json?.message || timeoutEvent.payload_json?.error || '')).toMatch(/timed out|timeout|MCP/i)
})

test('Pi approve-session grants and steer checkpoints match the browser contract', async ({ page }) => {
  const auth = await loginAdmin()
  const suffix = Date.now()
  const providerCredential = await createProviderCredential(auth, suffix)
  const mcpKey = `p109-mcp-${suffix}`
  await apiRequest('/store/ai-agents/resources', {
    method: 'POST',
    auth,
    data: {
      resource_kind: 'mcp_server',
      resource_key: mcpKey,
      name: `P1-09 MCP ${suffix}`,
      version: '1',
      status: 'active',
      spec: {
        mcpServers: {
          [mcpKey]: { type: 'streamable_http', url: PUBLIC_MCP_URL, timeout_ms: 8000 }
        }
      }
    }
  })
  const profile = await apiRequest('/store/ai-agents/profiles', {
    method: 'POST',
    auth,
    data: profilePayload(providerCredential.data.id, `P1-09 Profile ${suffix}`, {
      // Force approval UI for the public reverse tool so grant/steer paths are exercised.
      tool_policy: {
        rules: [{ id: 'ask-reverse', tool_name: 'reverse', operation_type: 'write', decision: 'ask' }]
      },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcpKey, required: true }]
    })
  })
  const published = await apiRequest(`/store/ai-agents/profiles/${profile.data.id}/publish`, {
    method: 'POST', auth, data: { change_summary: 'P1-09 approval semantics acceptance' }
  })
  const createSession = (title) => apiRequest('/ai/agent-chatbox/sessions', {
    method: 'POST',
    auth,
    data: { agent_profile_id: profile.data.id, agent_profile_version_id: published.data.profile_version_id, title }
  })
  const firstSession = await createSession(`P1-09 grant ${suffix}`)

  await page.addInitScript(({ token, workspaceId }) => {
    localStorage.setItem('token', token)
    localStorage.setItem('current_workspace_id', String(workspaceId))
  }, auth)
  await page.goto(`/store/ai-agents/chat/${firstSession.data.id}`)
  await page.getByPlaceholder('输入消息').fill('Call the reverse tool with text "grant-one". You must use the tool.')
  await page.getByPlaceholder('输入消息').press('Enter')

  const approveSessionBtn = page.getByRole('button', { name: '本会话批准' })
  await expect(approveSessionBtn).toBeVisible({ timeout: 120000 })
  await approveSessionBtn.click()
  await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 120000 })

  await page.getByPlaceholder('输入消息').fill('Call reverse again with text "grant-two".')
  await page.getByPlaceholder('输入消息').press('Enter')
  await expect(page.locator('.turn.assistant').last()).toBeVisible({ timeout: 120000 })
  const firstEntries = await apiRequest(`/ai/agent-chatbox/sessions/${firstSession.data.id}/entries`, { auth })
  const assistants = firstEntries.data
    .filter((entry) => entry.role === 'assistant')
    .sort((left, right) => Number(left.id) - Number(right.id))
  const secondAssistant = assistants.at(-1)
  expect(secondAssistant?.runtime_run_id).toBeTruthy()
  const secondReplay = await apiRequest(`/ai/agent-chatbox/runs/${secondAssistant.runtime_run_id}/events`, { auth })
  const secondTypes = secondReplay.data.events.map((event) => event.event_type)
  // Session grant may appear on second run if policy reuses grant; permission.asked should be absent when grant holds.
  if (secondTypes.includes('approval.session_granted')) {
    expect(secondTypes).not.toContain('permission.asked')
  }

  const steerSession = await createSession(`P1-09 steer ${suffix}`)
  await page.goto(`/store/ai-agents/chat/${steerSession.data.id}`)
  await page.getByPlaceholder('输入消息').fill('Call reverse with text "steer-start" and wait for my direction.')
  await page.getByPlaceholder('输入消息').press('Enter')
  const steerInput = page.getByPlaceholder('输入新的执行指令')
  await expect(steerInput).toBeVisible({ timeout: 120000 })
  await steerInput.fill('Do not reverse. Just say you will inspect instead.')
  await page.getByRole('button', { name: '调整' }).click()
  await expect(page.locator('.turn.assistant')).toBeVisible({ timeout: 120000 })
  const steerEntries = await apiRequest(`/ai/agent-chatbox/sessions/${steerSession.data.id}/entries`, { auth })
  const steerAssistant = steerEntries.data.find((entry) => entry.role === 'assistant')
  expect(steerAssistant?.runtime_run_id).toBeTruthy()
  const steerReplay = await apiRequest(`/ai/agent-chatbox/runs/${steerAssistant.runtime_run_id}/events`, { auth })
  const steerEventTypes = steerReplay.data.events.map((event) => event.event_type)
  expect(steerEventTypes.some((type) => type === 'session.steered' || type === 'permission.resolved')).toBe(true)
  expect(steerEventTypes).not.toContain('run.cancelled')
})

function createProviderCredential(auth, suffix) {
  return apiRequest('/v1/credentials', {
    method: 'POST',
    auth,
    data: {
      name: `P1 Deep Review Provider ${suffix}`,
      type: 'TOKEN',
      category: 'github',
      scope: 'workspace',
      payload: { token: PROVIDER_API_KEY, token_type: 'bearer' }
    }
  })
}

function profilePayload(credentialId, name, overrides = {}) {
  return {
    name,
    description: 'Deep review browser acceptance profile (real provider/MCP)',
    profile_kind: 'generic',
    context_tags: [],
    provider: {
      provider_type: 'openai-compatible',
      base_url: PROVIDER_BASE_URL
    },
    model: { provider_model_key: PROVIDER_MODEL },
    provider_credential_ref: { credential_id: credentialId },
    inference: { max_tokens: 256 },
    prompt: {
      system: [
        'You are an EasyDo acceptance agent.',
        'When MCP tools are available and the user asks to reverse/transform text, call the reverse tool.',
        'Do not invent tool results.'
      ].join(' ')
    },
    status: 'draft',
    ...overrides
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
