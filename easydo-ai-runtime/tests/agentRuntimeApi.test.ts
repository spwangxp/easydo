import { describe, expect, it } from 'vitest'
import { createApp } from '../src/server.js'
import { createMemoryRuntimeStore } from '../src/store/memoryRuntimeStore.js'
import { InMemoryAgentEventStore } from '../src/agent-runtime/eventStore.js'
import type { AgentHarnessRunInput, AgentHarnessRunResult } from '../src/agent-runtime/harnessRunner.js'
import { AgentWorkspaceService } from '../src/workspace/workspaceService.js'
import type { AIRuntimeWorkspace } from '../src/workspace/types.js'

let envelopeSeq = 0

function nextEnvelopeID(prefix: string) {
  envelopeSeq += 1
  return `agent-runtime-${prefix}-${envelopeSeq.toString(36).padStart(4, '0')}`
}

function envelope(payload: Record<string, unknown> = {}) {
  return {
    request_id: nextEnvelopeID('req'),
    idempotency_key: nextEnvelopeID('idem'),
    actor: {
      user_id: 12,
      username: 'demo',
      system_role: 'user',
      workspace_id: 3,
      workspace_role: 'owner',
      auth_session_id: 'auth-session-1'
    },
    auth: {
      server_internal_token: 'secret',
      delegated_user_token: 'delegated-user-token'
    },
    payload
  }
}

function envelopeAs(payload: Record<string, unknown> = {}, actor: Record<string, unknown> = {}) {
  const base = envelope(payload)
  return {
    ...base,
    actor: {
      ...base.actor,
      ...actor
    }
  }
}

function jsonRequest(method: string, payload: Record<string, unknown>) {
  return jsonRequestAs(method, payload, {})
}

function jsonRequestAs(method: string, payload: Record<string, unknown>, actor: Record<string, unknown>) {
  return {
    method,
    headers: {
      'content-type': 'application/json',
      'x-internal-token': 'secret'
    },
    body: JSON.stringify(envelopeAs(payload, actor))
  }
}

function parseSse(raw: string) {
  return raw
    .split(/\r?\n\r?\n/)
    .map((chunk) => chunk.trim())
    .filter(Boolean)
    .map((chunk) => {
      const lines = chunk.split(/\r?\n/)
      const id = lines.find((line) => line.startsWith('id:'))?.replace(/^id:\s?/, '').trim() || ''
      const event = lines.find((line) => line.startsWith('event:'))?.replace(/^event:\s?/, '').trim() || 'message'
      const data = lines
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.replace(/^data:\s?/, ''))
        .join('\n')
      return { id, event, data: JSON.parse(data) }
    })
}

function agentTaskModelCalls(calls: Array<Record<string, unknown>>) {
  return calls.filter((call) => {
    const contextRef = call.context_ref && typeof call.context_ref === 'object' && !Array.isArray(call.context_ref)
      ? call.context_ref as Record<string, unknown>
      : {}
    return contextRef.purpose !== 'session_title_generation'
  })
}

describe('agent runtime management API', () => {
  it('manages the current user Agent workspace lifecycle and audits ownership', async () => {
    const store = createMemoryRuntimeStore()
    const lifecycle: string[] = []
    const workspaceService = new AgentWorkspaceService(store, {
      async provision(workspace: AIRuntimeWorkspace) { lifecycle.push('provision'); return { sandbox_id: workspace.workspace_runtime_id } },
      async connect() { lifecycle.push('connect') },
      async pause() { lifecycle.push('pause') },
      async resume() { lifecycle.push('resume') },
      async recycle() { lifecycle.push('recycle') }
    })
    const app = createApp({ internalToken: 'secret', agentWorkspaceService: workspaceService })

    const createResponse = await app.request('/v1/agent-workspaces', jsonRequest('POST', {}))
    expect(createResponse.status).toBe(200)
    const created = await createResponse.json()
    const workspaceRuntimeID = created.data.workspace_runtime_id
    expect(created.data.status).toBe('ready')

    expect((await app.request(`/v1/agent-workspaces/${workspaceRuntimeID}/connect`, jsonRequest('POST', {}))).status).toBe(200)
    expect((await app.request(`/v1/agent-workspaces/${workspaceRuntimeID}/pause`, jsonRequest('POST', {}))).status).toBe(200)
    expect((await app.request(`/v1/agent-workspaces/${workspaceRuntimeID}/resume`, jsonRequest('POST', {}))).status).toBe(200)

    const denied = await app.request(`/v1/agent-workspaces/${workspaceRuntimeID}/query`, jsonRequestAs('POST', {}, { user_id: 99 }))
    expect(denied.status).toBe(403)

    const audits = await app.request(`/v1/agent-workspaces/${workspaceRuntimeID}/audits/query`, jsonRequest('POST', {}))
    expect(audits.status).toBe(200)
    expect((await audits.json()).data.map((item: { operation: string }) => item.operation))
      .toEqual(['created', 'connected', 'paused', 'resumed'])

    const recycled = await app.request(`/v1/agent-workspaces/${workspaceRuntimeID}/recycle`, jsonRequest('POST', {}))
    expect(recycled.status).toBe(200)
    expect((await recycled.json()).data.status).toBe('ready')
    expect(lifecycle).toEqual(['provision', 'connect', 'pause', 'resume', 'recycle', 'provision'])
  })

  it('manages mcp and skill resources and derives subagent profile options', async () => {
    const app = createApp({ internalToken: 'secret' })

    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Worker Agent',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_id: 1 },
      model: { model_id: 1 },
      response_mode: 'json'
    }))

    const skillResponse = await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'skill',
      resource_key: 'explain-page',
      name: 'Explain Page',
      description: 'Summarize current page context',
      version: '1.0.0',
      spec: { input_schema: { type: 'object' } },
      status: 'active'
    }))
    expect(skillResponse.status).toBe(200)
    const skill = await skillResponse.json()
    expect(skill.data.id).toBe(1)
    expect(skill.data.resource_kind).toBe('skill')

    const duplicateSkillResponse = await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'skill',
      resource_key: 'explain-page',
      name: 'Duplicate Explain Page'
    }))
    expect(duplicateSkillResponse.status).toBe(409)

    const mcpResponse = await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'mcp_server',
      resource_key: 'easydo-mcp',
      name: 'EasyDo MCP',
      endpoint: { transport: 'streamable-http', url: '/mcp' },
      status: 'active'
    }))
    expect(mcpResponse.status).toBe(200)

    for (const resourceKind of ['credential', 'context_provider', 'memory_store', 'artifact_store']) {
      const removedKindResponse = await app.request('/v1/agent-resources', jsonRequest('POST', {
        resource_kind: resourceKind,
        resource_key: `${resourceKind}-1`,
        name: `${resourceKind} should not be managed as an agent resource`,
        status: 'active'
      }))
      expect(removedKindResponse.status).toBe(400)
      const removedKind = await removedKindResponse.json()
      expect(removedKind.code).toBe('agent_resource_kind_invalid')
    }

    const listResponse = await app.request('/v1/agent-resources/query', jsonRequest('POST', {}))
    expect(listResponse.status).toBe(200)
    const listed = await listResponse.json()
    expect(listed.data.skills[0].resource_id).toBe('explain-page')
    expect(listed.data.mcp_servers[0].resource_id).toBe('easydo-mcp')
    expect(listed.data.credentials).toBeUndefined()
    expect(listed.data.context_providers).toBeUndefined()
    expect(listed.data.memory_stores).toBeUndefined()
    expect(listed.data.subagent_profiles[0].id).toBe(1)
    expect(listed.data.subagent_profiles[0].name).toBe('Worker Agent')

    const updateResponse = await app.request('/v1/agent-resources/1', jsonRequest('PUT', {
      name: 'Explain Current Page',
      version: '1.1.0'
    }))
    expect(updateResponse.status).toBe(200)
    const updated = await updateResponse.json()
    expect(updated.data.name).toBe('Explain Current Page')
    expect(updated.data.version).toBe('1.1.0')

    const deleteResponse = await app.request('/v1/agent-resources/1', jsonRequest('DELETE', {}))
    expect(deleteResponse.status).toBe(200)
    const deleted = await deleteResponse.json()
    expect(deleted.data.deleted).toBe(true)
  })

  it('rejects local Skill repositories from the multi-tenant scan API', async () => {
    const app = createApp({ internalToken: 'secret' })
    const createResponse = await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'skill',
      resource_key: 'workspace-skills',
      name: 'Workspace Skills',
      spec: {
        resource_subtype: 'skill_repository',
        repository: {
          url: 'file:///tmp/workspace-skills',
          commit: 'a'.repeat(40),
          subdirectory: 'skills'
        }
      },
      tags: ['skill-repository'],
      status: 'active'
    }))
    expect(createResponse.status).toBe(200)

    const scanResponse = await app.request('/v1/agent-resources/1/scan', jsonRequest('POST', {}))
    expect(scanResponse.status).toBe(400)
    const failed = await scanResponse.json()
    expect(failed.message).toContain('Local Skill repositories are not supported')
  })

  it('creates, publishes, and validates agent profiles with skills and subagents', async () => {
    const app = createApp({ internalToken: 'secret' })

    expect((await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'skill',
      resource_key: 'explain-page',
      name: 'Explain Page',
      version: '1.0.0',
      status: 'active',
      spec: { instructions: 'Explain the current EasyDo page.' }
    }))).status).toBe(200)
    expect((await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'mcp_server',
      resource_key: 'easydo-mcp',
      name: 'EasyDo MCP',
      version: '1.0.0',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_workspace_list' }] }
    }))).status).toBe(200)

    const createResponse = await app.request(
      '/v1/agent-profiles',
      jsonRequest('POST', {
        name: 'page-ai-assistant',
        description: 'Workspace page assistant profile',
        profile_kind: 'assistant',
        context_tags: ['page-assistant'],
        provider: { provider_id: 7 },
        binding: { binding_id: 9 },
        model: { model_id: 11, provider_model_key: 'qwen2.5' },
        provider_credential_ref: { credential_id: 19 },
        inference: {
          temperature: 0.2,
          max_tokens: 4096,
          thinking_level: 'medium',
          fallback: { enabled: false }
        },
        prompt: {
          system: 'Help the current EasyDo user.',
          user_template: '{{input}}'
        },
        skills: [{ resource_type: 'skill', resource_id: 'explain-page', resource_version: '1.0.0' }],
        subagents: [],
        mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'easydo-mcp' }],
        context_contract: { accepts: ['current-page'], explicit_reference_required: true },
        input_schema: { type: 'object' },
        output_schema: { type: 'object' },
        memory_policy: { short_term: true },
        confirmation_policy: { write_tools_require_confirmation: true },
        response_mode: 'mixed'
      })
    )

    expect(createResponse.status).toBe(200)
    const created = await createResponse.json()
    expect(created.data.id).toBe(1)
    expect(created.data.skills).toHaveLength(1)
    expect(created.data.provider_credential_ref.credential_id).toBe(19)
    expect(created.data.model_adapter).toBeUndefined()

    const publishResponse = await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {
      change_summary: 'initial release'
    }))
    expect(publishResponse.status).toBe(200)
    const published = await publishResponse.json()
    expect(published.data.profile_id).toBe(1)
    expect(published.data.version).toBe(1)
    expect(published.data.snapshot_hash).toMatch(/^sha256:/)
    expect(published.data.snapshot.skills).toHaveLength(1)

    const validateResponse = await app.request('/v1/agent-profiles/1/validate', jsonRequest('POST', {}))
    expect(validateResponse.status).toBe(200)
    const validation = await validateResponse.json()
    expect(validation.data.status).toBe('passed')
    expect(validation.data.errors).toEqual([])

    const bindingResponse = await app.request('/v1/scene-bindings', jsonRequest('PUT', {
      bindings: []
    }))
    expect(bindingResponse.status).toBe(404)
  })

  it('rejects subagent cycles before publishing a profile', async () => {
    const app = createApp({ internalToken: 'secret' })

    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Main',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Worker',
      profile_kind: 'generic',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      subagents: [{ resource_type: 'subagent_profile', resource_id: 1 }],
      response_mode: 'json'
    }))
    await app.request('/v1/agent-profiles/1', jsonRequest('PUT', {
      subagents: [{ resource_type: 'subagent_profile', resource_id: 2 }]
    }))

    const validateResponse = await app.request('/v1/agent-profiles/1/validate', jsonRequest('POST', {}))
    expect(validateResponse.status).toBe(200)
    const validation = await validateResponse.json()
    expect(validation.data.status).toBe('failed')
    expect(validation.data.errors[0].code).toBe('subagent_cycle')
  })
})

describe('agent runtime session API', () => {
  it('updates provider, model, and thinking level for a single session', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Switchable Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 'openrouter', base_url: 'https://openrouter.ai/api/v1' },
      model: { provider_model_key: 'openrouter/default-model' },
      provider_credential_ref: { credential_id: '123' },
      inference: { thinking_level: 'low' },
      response_mode: 'mixed'
    }))
    const publishResponse = await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    const published = await publishResponse.json()
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: '1:draft',
      profile_selection: { agent_profile_version_id: published.data.profile_version_id }
    }))

    const response = await app.request('/v1/sessions/1/model', jsonRequest('PUT', {
      provider: { provider_id: 'anthropic', provider_type: 'anthropic', base_url: 'https://anthropic.example' },
      model: { provider_model_key: 'claude-sonnet-4-5' },
      provider_credential_ref: { credential_id: '456' },
      inference: { thinking_level: 'high' }
    }))

    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body.data.session.model_override.model).toMatchObject({ provider_model_key: 'claude-sonnet-4-5' })
    expect(body.data.model_config).toMatchObject({
      provider_id: 'anthropic',
      id: 'claude-sonnet-4-5',
      base_url: 'https://anthropic.example',
      has_api_key: false,
      thinking_level: 'high'
    })
  })

  it('creates a scene session and appends entries through a runtime run', async () => {
    const modelCalls: Array<Record<string, unknown>> = []
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete(request: Record<string, unknown>) {
          modelCalls.push(request)
          return { text: '模型已读取当前页面上下文。', usage: { total_tokens: 24 } }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openrouter', base_url: 'https://openrouter.ai/api/v1' },
      binding: { binding_id: 1 },
      model: { provider_model_key: 'openrouter/auto' },
      provider_credential_ref: {
        credential_id: 'openrouter-api-key'
      },
      response_mode: 'mixed'
    }))
    const publishResponse = await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    const published = await publishResponse.json()

    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_version_id: published.data.profile_version_id }
    }))
    expect(sessionResponse.status).toBe(200)
    const session = await sessionResponse.json()
    expect(session.data.id).toBe(1)
    expect(session.data.agent_profile_version_id).toBe(published.data.profile_version_id)

    const entryResponse = await app.request('/v1/sessions/1/entries', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '解释当前页面 @当前页面',
      context_ref: {
        kind: 'current-page',
        route_path: '/pipeline/12',
        object_type: 'pipeline',
        object_id: '12'
      },
      include_current_page: true,
      client_entry_id: 'entry-client-1'
    }))

    expect(entryResponse.status).toBe(200)
    const entryResult = await entryResponse.json()
    expect(entryResult.data.run.status).toBe('completed')
    expect(entryResult.data.user_entry.role).toBe('user')
    expect(entryResult.data.assistant_entry.role).toBe('assistant')
    expect(entryResult.data.assistant_entry.content).toBe('模型已读取当前页面上下文。')
    expect(entryResult.data.run.usage.total_tokens).toBe(24)
    const taskModelCalls = agentTaskModelCalls(modelCalls)
    expect(taskModelCalls).toHaveLength(1)
    expect(taskModelCalls[0].profile).toMatchObject({
      provider: { type: 'openrouter' },
      model: { provider_model_key: 'openrouter/auto' },
      provider_credential_ref: {
        credential_id: 'openrouter-api-key'
      }
    })

    const listSessionsResponse = await app.request('/v1/sessions/query', jsonRequest('POST', {
      context_tag: 'page-assistant'
    }))
    expect(listSessionsResponse.status).toBe(200)
    const listedSessions = await listSessionsResponse.json()
    expect(listedSessions.data).toHaveLength(1)
    expect(listedSessions.data[0].id).toBe(session.data.id)

    const listEntriesResponse = await app.request('/v1/sessions/1/entries/query', jsonRequest('POST', {}))
    expect(listEntriesResponse.status).toBe(200)
    const listedEntries = await listEntriesResponse.json()
    expect(listedEntries.data).toHaveLength(2)
    expect(listedEntries.data.map((entry: { role: string }) => entry.role)).toEqual(['user', 'assistant'])
  })

  it('streams an L5 page assistant run with capabilities, skills, subagents, and output schema validation', async () => {
    const modelCalls: Array<Record<string, unknown>> = []
    const agentEventStore = new InMemoryAgentEventStore()
    const app = createApp({
      internalToken: 'secret',
      agentEventStore,
      agentHarnessRunner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'l5-child-user'
          const assistantMessageID = 'l5-child-assistant'
          const timestamp = '2026-07-11T00:00:00.000Z'
          await agentEventStore.append({
            type: 'session.prompted', session_id: input.sessionID, runtime_run_id: input.runtimeRunID,
            event_id: '', seq: 0, timestamp, message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt'
          })
          await agentEventStore.append({
            type: 'session.step.started', session_id: input.sessionID, runtime_run_id: input.runtimeRunID,
            event_id: '', seq: 0, timestamp, assistant_message_id: assistantMessageID,
            parent_message_id: userMessageID, agent: input.agent, model: input.model
          })
          await agentEventStore.append({
            type: 'session.text.ended', session_id: input.sessionID, runtime_run_id: input.runtimeRunID,
            event_id: '', seq: 0, timestamp, assistant_message_id: assistantMessageID,
            text_id: 'l5-child-text', text: '{"summary":"子任务已检查页面上下文。"}'
          })
          await agentEventStore.append({
            type: 'session.step.ended', session_id: input.sessionID, runtime_run_id: input.runtimeRunID,
            event_id: '', seq: 0, timestamp, assistant_message_id: assistantMessageID, finish_reason: 'stop',
            tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } }, cost: 0, files: []
          })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      chatModelClient: {
        async complete(request: Record<string, unknown>) {
          modelCalls.push(request)
          return { text: '{"summary":"子任务已检查页面上下文。"}', usage: { total_tokens: 8 } }
        },
        async *stream(request: Record<string, unknown>) {
          modelCalls.push(request)
          yield { type: 'reasoning_delta' as const, delta: '先检查能力。' }
          yield { type: 'answer_delta' as const, delta: '{"summary":"页面助手已完成 L5 闭环。","risk":"low"}' }
        }
      }
    })

    await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'skill',
      resource_key: 'page-skill-repo',
      name: 'Page Skill Repo',
      description: 'Explain EasyDo page context',
      spec: {
        instructions: 'Use route and object context before answering.'
      },
      status: 'active'
    }))
    await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'mcp_server',
      resource_key: 'easydo-mcp',
      name: 'EasyDo MCP',
      spec: { capabilities: ['read_pipeline'] },
      endpoint: { type: 'streamable_http', url: '/mcp' },
      secret_ref: { credential_id: 'mcp-credential-1' },
      status: 'active'
    }))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Page Worker',
      profile_kind: 'worker',
      context_tags: [],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      prompt: { system: 'You are a child page reviewer.' },
      output_schema: {
        type: 'object',
        required: ['summary'],
        properties: { summary: { type: 'string' } }
      },
      response_mode: 'schema'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      prompt: {
        system: 'Help EasyDo users.',
        developer: 'Return schema-valid JSON when output_schema is present.'
      },
      skills: [{ resource_type: 'skill', resource_id: 'page-skill-repo', config: { auto_load: true } }],
      subagents: [{ resource_type: 'subagent_profile', resource_id: 1 }],
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'easydo-mcp' }],
      context_contract: { accepts: ['current-page'] },
      output_schema: {
        type: 'object',
        required: ['summary', 'risk'],
        properties: {
          summary: { type: 'string' },
          risk: { type: 'string', enum: ['low', 'medium', 'high'] }
        }
      },
      confirmation_policy: { write_tools_require_confirmation: true },
      response_mode: 'schema'
    }))
    await app.request('/v1/agent-profiles/2/publish', jsonRequest('POST', {}))

    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 2, agent_profile_version_id: 'latest' }
    }))
    expect(sessionResponse.status).toBe(200)

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '检查当前页面',
      context_ref: {
        kind: 'current-page',
        route_path: '/pipeline/12',
        route_name: 'PipelineDetail',
        object_type: 'pipeline',
        object_id: '12'
      },
      include_current_page: true,
      client_entry_id: 'l5-entry-1',
      subagent_tasks: [{ task_id: 'page-worker', agent_profile_id: 1, objective: 'Review page context completeness.' }]
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    expect(events.map((event) => event.event)).toEqual(expect.arrayContaining([
      'user_entry',
      'capability.snapshot',
      'skill.loaded',
      'subagent.spawned',
      'subagent.completed',
      'reasoning_delta',
      'answer_delta',
      'output_schema.validated',
      'assistant_entry',
      'done'
    ]))
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry
    expect(assistant.output.output_schema_valid).toBe(true)
    expect(assistant.output.structured_output.summary).toBe('页面助手已完成 L5 闭环。')
    expect(assistant.output.loaded_skills[0].name).toBe('Page Skill Repo')
    expect(assistant.output.subagent_results[0].status).toBe('completed')
    expect(assistant.output.subagent_results[0].child_runtime_run_id).toMatch(/^r_w[0-9a-z]+_[0-9a-z]{6}_[0-9a-z]{6}$/)
    expect(assistant.output.subagent_results[0].child_run_link_id).toMatch(/^cl_w[0-9a-z]+_[0-9a-z]{6}_[0-9a-z]{6}_[0-9a-z]{6}_[0-9a-z]{6}$/)
    expect(assistant.output.subagent_results[0].artifact_refs[0].artifact_id).toMatch(/^art_w[0-9a-z]+_[0-9a-z]{6}_[0-9a-z]{6}_[0-9a-z]{6}$/)
    expect(assistant.output.agent_actions).toEqual(expect.arrayContaining([
      expect.objectContaining({
        id: expect.stringMatching(/^a_w[0-9a-z]+_[0-9a-z]{6}_[0-9a-z]{6}_[0-9a-z]{6}$/),
        internal_id: expect.any(Number),
        action_kind: 'subagent.spawn',
        status: 'executed',
        display_json: expect.objectContaining({ name: 'Page Worker' })
      })
    ]))
    expect(assistant.output.agent_actions[0]).not.toHaveProperty('tool_name')
    expect(assistant.output.agent_actions[0]).not.toHaveProperty('tool_call_id')
    const subagentSpawned = events.find((event) => event.event === 'subagent.spawned')?.data
    expect(subagentSpawned.action_id).toBe(assistant.output.agent_actions[0].id)
    expect(subagentSpawned.child_runtime_run_id).toBe(assistant.output.subagent_results[0].child_runtime_run_id)
    const childRuntimeRunID = assistant.output.subagent_results[0].child_runtime_run_id
    const childEventsWithoutParentResponse = await app.request(`/v1/runs/${childRuntimeRunID}/events/query`, jsonRequest('POST', {}))
    expect(childEventsWithoutParentResponse.status).toBe(404)
    const childEventsResponse = await app.request(`/v1/runs/${childRuntimeRunID}/events/query`, jsonRequest('POST', {
      child_run_link_id: assistant.output.subagent_results[0].child_run_link_id,
      parent_runtime_run_id: assistant.output.agent_actions[0].runtime_run_id,
      parent_action_id: assistant.output.agent_actions[0].id
    }))
    expect(childEventsResponse.status).toBe(200)
    const childEvents = await childEventsResponse.json()
    expect(childEvents.data.events.map((event: { event_type: string }) => event.event_type)).toEqual(expect.arrayContaining([
      'subagent.started',
      'subagent.completed'
    ]))
    const artifactID = assistant.output.subagent_results[0].artifact_refs[0].artifact_id
    const artifactWithoutParentResponse = await app.request(`/v1/artifacts/${artifactID}/query`, jsonRequest('POST', {}))
    expect(artifactWithoutParentResponse.status).toBe(404)
    const artifactResponse = await app.request(`/v1/artifacts/${artifactID}/query`, jsonRequest('POST', {
      child_run_link_id: assistant.output.subagent_results[0].child_run_link_id,
      parent_runtime_run_id: assistant.output.agent_actions[0].runtime_run_id,
      parent_action_id: assistant.output.agent_actions[0].id
    }))
    expect(artifactResponse.status).toBe(200)
    const artifact = await artifactResponse.json()
    expect(artifact.data.artifact_type).toBe('subagent_transcript')
    expect(artifact.data.storage_ref).toBeUndefined()
    expect(artifact.data.preview_json.summary).toContain('子任务已检查页面上下文')
    expect(assistant.output.capabilities.mcp_servers[0].resource_key).toBe('easydo-mcp')
    expect(assistant.output.capabilities.mcp_servers[0].spec.api_key).toBeUndefined()
    expect(assistant.output.capabilities.mcp_servers[0].endpoint.headers).toBeUndefined()
    const taskModelCalls = agentTaskModelCalls(modelCalls)
    expect(taskModelCalls).toHaveLength(1)
    expect(taskModelCalls[0].capabilities.l5_controls.subagent_scheduler).toBe(true)
    expect(taskModelCalls[0].loaded_skills[0].instructions).toContain('Use route')
  })

  it('does not load an unrelated skill unless the profile marks it as required or auto-load', async () => {
    const modelCalls: Array<Record<string, unknown>> = []
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async *stream(request: Record<string, unknown>) {
          modelCalls.push(request)
          yield { type: 'answer_delta' as const, delta: '页面已检查。' }
        },
        async complete() {
          return { text: '页面已检查。' }
        }
      }
    })
    await app.request('/v1/agent-resources', jsonRequest('POST', {
      resource_kind: 'skill',
      resource_key: 'danger-deploy',
      name: 'Danger Deploy',
      description: 'Deploy production resources',
      status: 'active',
      spec: { instructions: 'Only deploy when the user explicitly requests it.' }
    }))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      skills: [{ resource_type: 'skill', resource_id: 'danger-deploy' }],
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '检查当前页面',
      client_entry_id: 'unrelated-skill-stream',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    expect(events.map((event) => event.event)).not.toContain('skill.loaded')
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry
    expect(assistant.output.loaded_skills).toEqual([])
    expect(modelCalls[0].loaded_skills).toEqual([])
  })

  it('records an invalid requested subagent instead of dispatching to another profile by index', async () => {
    const modelCalls: Array<Record<string, unknown>> = []
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete(request: Record<string, unknown>) {
          modelCalls.push(request)
          return { text: '{"summary":"main answer"}' }
        },
        async *stream(request: Record<string, unknown>) {
          modelCalls.push(request)
          yield { type: 'answer_delta' as const, delta: '{"summary":"main answer"}' }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Available Worker',
      profile_kind: 'worker',
      context_tags: [],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      subagents: [{ resource_type: 'subagent_profile', resource_id: 1 }],
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/2/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 2, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '检查当前页面',
      client_entry_id: 'invalid-subagent-stream',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true,
      subagent_tasks: [{ task_id: 'bad-worker', agent_profile_id: 999, objective: 'wrong worker' }]
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    const failedSubagent = events.find((event) => event.event === 'subagent.failed')?.data
    expect(failedSubagent.status).toBe('failed')
    expect(failedSubagent.agent_profile_id).toBe('999')
    expect(agentTaskModelCalls(modelCalls)).toHaveLength(1)
  })

  it('streams model tool calls as pending tool actions for the page assistant', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          return { text: '已请求确认。' }
        },
        async *stream() {
          yield {
            type: 'tool_call' as const,
            tool_call: {
              id: 'tool-call-1',
              name: 'easydo_pipeline_trigger',
              arguments: {
                pipeline_id: 12,
                operation_type: 'execute',
                target_type: 'pipeline',
                target_id: '12',
                risk_summary: '将触发流水线执行'
              }
            }
          }
          yield { type: 'answer_delta' as const, delta: '需要确认后继续。' }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      confirmation_policy: { write_tools_require_confirmation: true },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '触发当前页面流水线',
      client_entry_id: 'stream-pipeline-action',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    const actionEvent = events.find((event) => event.event === 'action.decision_required')
    expect(actionEvent?.data.action.input_json.tool_name).toBe('easydo_pipeline_trigger')
    expect(actionEvent?.data.action.action_kind).toBe('pipeline.trigger')
    expect(actionEvent?.data.action.status).toBe('awaiting_decision')
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry
    expect(assistant.output.agent_actions[0].input_json.tool_name).toBe('easydo_pipeline_trigger')
    expect(assistant.output.agent_actions[0]).not.toHaveProperty('tool_name')
    expect(assistant.output.runtime_events.map((item: { type: string }) => item.type)).toContain('action.decision_required')
  })

  it('persists runtime action events and replays them by event cursor', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          return { text: '等待确认。' }
        },
        async *stream() {
          yield {
            type: 'tool_call' as const,
            tool_call: {
              id: 'tool-call-replay-1',
              name: 'easydo_pipeline_trigger',
              arguments: {
                pipeline_id: 12,
                operation_type: 'execute',
                target_type: 'pipeline',
                target_id: '12',
                risk_summary: '将触发流水线执行'
              }
            }
          }
          yield { type: 'answer_delta' as const, delta: '需要确认后继续。' }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      confirmation_policy: { write_tools_require_confirmation: true },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '触发当前页面流水线',
      client_entry_id: 'stream-event-replay',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    }))
    const events = parseSse(await streamResponse.text())
    const actionEvent = events.find((event) => event.event === 'action.decision_required')
    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const runtimeRunID = assistantEvent?.data.run.runtime_run_id

    expect(runtimeRunID).toBe('r_w3_000001_000001')
    expect(actionEvent?.id).toMatch(/^r_w3_000001_000001:ev[0-9a-z]{6}$/)
    expect(actionEvent?.data.event_id).toBe(actionEvent?.id)
    expect(actionEvent?.data.runtime_run_id).toBe(runtimeRunID)
    expect(actionEvent?.data.display_json).toMatchObject({
      event_type: 'action.decision_required',
      status: 'awaiting_decision'
    })

    const replayResponse = await app.request(`/v1/runs/${runtimeRunID}/events`, {
      method: 'GET',
      headers: { 'x-internal-token': 'secret' }
    })
    expect(replayResponse.status).toBe(200)
    const replayBody = await replayResponse.json()
    const replayedEvents = replayBody.data.events as Array<{ event_id: string, event_type: string, payload_json: { event_id?: string } }>
    expect(replayedEvents.map((event) => event.event_id)).toContain(actionEvent?.id)
    const replayedActionEvent = replayedEvents.find((event) => event.event_id === actionEvent?.id)
    expect(replayedActionEvent?.payload_json.event_id).toBe(actionEvent?.id)

    const afterResponse = await app.request(`/v1/runs/${runtimeRunID}/events?after_event_id=${encodeURIComponent(String(actionEvent?.id))}`, {
      method: 'GET',
      headers: { 'x-internal-token': 'secret' }
    })
    expect(afterResponse.status).toBe(200)
    const afterBody = await afterResponse.json()
    const afterEvents = afterBody.data.events as Array<{ event_id: string }>
    expect(afterEvents.some((event) => event.event_id === actionEvent?.id)).toBe(false)
  })

  it('converts completion tool calls into stream actions when the provider has no native stream', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          return {
            text: '需要确认后继续。',
            tool_calls: [{
              id: 'completion-tool-call-1',
              name: 'easydo_pipeline_trigger',
              arguments: {
                operation_type: 'execute',
                target_type: 'pipeline',
                target_id: '12'
              }
            }]
          }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      confirmation_policy: { write_tools_require_confirmation: true },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '触发当前页面流水线',
      client_entry_id: 'completion-tool-action-stream',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    expect(events.map((event) => event.event)).toContain('action.decision_required')
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry
    expect(assistant.output.agent_actions[0].input_json.provider_tool_call_id).toBe('completion-tool-call-1')
    expect(assistant.output.agent_actions[0]).not.toHaveProperty('tool_call_id')
  })

  it('persists L5 output metadata when stream falls back to a completion request', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          return {
            text: '{"summary":"fallback completion kept structured output"}',
            usage: { total_tokens: 13 },
            raw: { provider: 'fallback-complete' }
          }
        },
        async *stream() {
          // Simulate a provider stream that opens successfully but emits no answer chunks.
        }
      }
    })

    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      output_schema: {
        type: 'object',
        required: ['summary'],
        properties: { summary: { type: 'string' } }
      },
      response_mode: 'schema'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '解释当前页面',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true,
      client_entry_id: 'fallback-stream-entry'
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry
    const run = events.find((event) => event.event === 'assistant_entry')?.data.run
    expect(assistant.output.structured_output.summary).toBe('fallback completion kept structured output')
    expect(assistant.output.output_schema_valid).toBe(true)
    expect(assistant.output.runtime_events.map((item: { type: string }) => item.type)).toContain('output_schema.validated')
    expect(assistant.output).not.toHaveProperty('raw')
    expect(assistant.output.provider_raw_refs[0].artifact_type).toBe('provider_raw')
    expect(assistant.output.runtime_events.map((item: { type: string }) => item.type)).toContain('provider.raw_stored')
    expect(run.usage.total_tokens).toBe(13)
  })

  it('rejects page assistant entries that omit current page context', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const response = await app.request('/v1/sessions/1/entries', jsonRequest('POST', {
      content: '解释',
      context_ref: { kind: 'current-page' },
      include_current_page: true
    }))
    expect(response.status).toBe(400)
    const body = await response.json()
    expect(body.code).toBe('page_context_route_required')

    const missingContextResponse = await app.request('/v1/sessions/1/entries', jsonRequest('POST', {
      content: '解释',
      context_ref: {},
      include_current_page: false
    }))
    expect(missingContextResponse.status).toBe(400)
    const missingContext = await missingContextResponse.json()
    expect(missingContext.code).toBe('page_context_required')
  })

  it('persists failed stream entries when output schema validation fails', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          return { text: '{"risk":"low"}' }
        },
        async *stream() {
          yield { type: 'answer_delta' as const, delta: '{"risk":"low"}' }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      output_schema: {
        type: 'object',
        required: ['summary'],
        properties: { summary: { type: 'string' } }
      },
      response_mode: 'schema'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '解释当前页面',
      client_entry_id: 'invalid-schema-stream',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    }))

    expect(streamResponse.status).toBe(200)
    const events = parseSse(await streamResponse.text())
    expect(events.map((event) => event.event)).toContain('output_schema.invalid')
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry
    const run = events.find((event) => event.event === 'assistant_entry')?.data.run
    expect(assistant.status).toBe('failed')
    expect(assistant.entry_type).toBe('error')
    expect(assistant.output.output_schema_valid).toBe(false)
    expect(assistant.output.output_schema_errors[0]).toContain('$.summary is required')
    expect(run.status).toBe('failed')
  })

  it('reuses completed entries for duplicate page assistant client entry ids', async () => {
    const modelCalls: Array<Record<string, unknown>> = []
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete(request: Record<string, unknown>) {
          modelCalls.push(request)
          return { text: '同一个请求只处理一次。' }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))
    const payload = {
      runtime_engine: 'legacy',
      content: '解释当前页面',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true,
      client_entry_id: 'same-client-entry'
    }
    const first = await app.request('/v1/sessions/1/entries', jsonRequest('POST', payload))
    const second = await app.request('/v1/sessions/1/entries', jsonRequest('POST', payload))
    const firstBody = await first.json()
    const secondBody = await second.json()

    expect(first.status).toBe(200)
    expect(second.status).toBe(200)
    expect(agentTaskModelCalls(modelCalls)).toHaveLength(1)
    expect(secondBody.data.idempotent).toBe(true)
    expect(secondBody.data.assistant_entry.id).toBe(firstBody.data.assistant_entry.id)
  })

  it('requires explicit profile selection instead of resolving sessions by scene tags', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Common Workspace Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    const publishResponse = await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    const published = await publishResponse.json()

    const missingSelectionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3'
    }))
    expect(missingSelectionResponse.status).toBe(400)
    const missingSelection = await missingSelectionResponse.json()
    expect(missingSelection.code).toBe('agent_profile_selection_required')

    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_version_id: published.data.profile_version_id }
    }))

    expect(sessionResponse.status).toBe(200)
    const session = await sessionResponse.json()
    expect(session.data.context_tags).toEqual([])
    expect(session.data.agent_profile_version_id).toBe(published.data.profile_version_id)
  })

  it('accepts explicit profile versions without scene compatibility checks', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Common Only',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    const publishResponse = await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    const published = await publishResponse.json()

    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_version_id: published.data.profile_version_id }
    }))

    expect(sessionResponse.status).toBe(200)
    const session = await sessionResponse.json()
    expect(session.data.agent_profile_version_id).toBe(published.data.profile_version_id)
    expect(session.data.context_tags).toEqual([])
  })

  it('does not treat context tags as runtime permission gates', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Common Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/agent-profiles/2/publish', jsonRequest('POST', {}))

    const commonDeveloperResponse = await app.request('/v1/sessions/current', jsonRequestAs('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }, { workspace_role: 'developer' }))
    expect(commonDeveloperResponse.status).toBe(200)

    const commonViewerResponse = await app.request('/v1/sessions/current', jsonRequestAs('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest', first_session_timestamp: 'viewer-session' }
    }, { workspace_role: 'viewer' }))
    expect(commonViewerResponse.status).toBe(200)

    const pageDeveloperResponse = await app.request('/v1/sessions/current', jsonRequestAs('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 2, agent_profile_version_id: 'latest' }
    }, { workspace_role: 'developer' }))
    expect(pageDeveloperResponse.status).toBe(200)
  })

  it('filters session reads by context tag without scene permission filtering', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Common Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/agent-profiles/2/publish', jsonRequest('POST', {}))

    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 2, agent_profile_version_id: 'latest' }
    }))

    const developerListResponse = await app.request('/v1/sessions/query', jsonRequestAs('POST', {}, {
      workspace_role: 'developer'
    }))
    expect(developerListResponse.status).toBe(200)
    const developerList = await developerListResponse.json()
    expect(developerList.data.map((session: { context_tags: string[] }) => session.context_tags)).toEqual([['page-assistant'], []])

    const pageTagListResponse = await app.request('/v1/sessions/query', jsonRequestAs('POST', {
      context_tag: 'page-assistant'
    }, { workspace_role: 'developer' }))
    expect(pageTagListResponse.status).toBe(200)
    const pageTagList = await pageTagListResponse.json()
    expect(pageTagList.data).toHaveLength(1)
    expect(pageTagList.data[0].context_tags).toEqual(['page-assistant'])

    const pageTagsListResponse = await app.request('/v1/sessions/query', jsonRequestAs('POST', {
      context_tags: ['page-assistant']
    }, { workspace_role: 'developer' }))
    expect(pageTagsListResponse.status).toBe(200)
    const pageTagsList = await pageTagsListResponse.json()
    expect(pageTagsList.data).toHaveLength(1)
    expect(pageTagsList.data[0].context_tags).toEqual(['page-assistant'])

    const viewerListResponse = await app.request('/v1/sessions/query', jsonRequestAs('POST', {}, {
      workspace_role: 'viewer'
    }))
    expect(viewerListResponse.status).toBe(200)
    const viewerList = await viewerListResponse.json()
    expect(viewerList.data).toHaveLength(2)

    const developerEntriesResponse = await app.request('/v1/sessions/2/entries/query', jsonRequestAs('POST', {}, {
      workspace_role: 'developer'
    }))
    expect(developerEntriesResponse.status).toBe(200)
  })

  it('keeps agent chatbox sessions private to their owner', async () => {
    const app = createApp({ internalToken: 'secret' })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Common Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      response_mode: 'mixed'
    }))

    const ownerSessionResponse = await app.request('/v1/sessions/current', jsonRequestAs('POST', {
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: '1:draft',
      source: 'agent_chatbox',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'draft' }
    }, { user_id: 21, username: 'owner-user', workspace_role: 'developer', auth_session_id: 'auth-owner' }))
    expect(ownerSessionResponse.status).toBe(200)
    const ownerSession = await ownerSessionResponse.json()
    expect(ownerSession.data.user_id).toBe(21)

    const otherActor = { user_id: 22, username: 'other-user', workspace_role: 'owner', auth_session_id: 'auth-other' }
    const listResponse = await app.request('/v1/sessions/query', jsonRequestAs('POST', {
      business_type: 'agent_profile',
      business_id: '1:draft',
      status: 'active'
    }, otherActor))
    expect(listResponse.status).toBe(200)
    const listBody = await listResponse.json()
    expect(listBody.data).toEqual([])

    const getResponse = await app.request('/v1/sessions/1/query', jsonRequestAs('POST', {}, otherActor))
    expect(getResponse.status).toBe(404)

    const entriesResponse = await app.request('/v1/sessions/1/entries/query', jsonRequestAs('POST', {}, otherActor))
    expect(entriesResponse.status).toBe(404)

    const sendResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequestAs('POST', {
      content: 'try to read someone else chatbox',
      client_entry_id: 'cross-user-msg'
    }, otherActor))
    expect(sendResponse.status).toBe(404)

    const ownerGetResponse = await app.request('/v1/sessions/1/query', jsonRequestAs('POST', {}, {
      user_id: 21,
      username: 'owner-user',
      workspace_role: 'developer',
      auth_session_id: 'auth-owner'
    }))
    expect(ownerGetResponse.status).toBe(200)

    const ownerListResponse = await app.request('/v1/sessions/query', jsonRequestAs('POST', {
      business_type: 'agent_profile',
      business_id: '1:draft',
      status: 'active'
    }, { user_id: 21, username: 'owner-user', workspace_role: 'developer', auth_session_id: 'auth-owner' }))
    expect(ownerListResponse.status).toBe(200)
    const ownerList = await ownerListResponse.json()
    expect(ownerList.data.map((session: { id: number }) => session.id)).toEqual([1])
  })

  it('does not expose standalone action creation routes', async () => {
    const app = createApp({ internalToken: 'secret' })

    const businessCreateResponse = await app.request('/v1/actions', jsonRequest('POST', {
      context_tags: ['page-assistant'],
      session_id: 1,
      runtime_run_id: 'run-1',
      action_kind: 'pipeline.update',
      capability_id: 'easydo_pipeline_update',
      input_json: {
        provider_tool_call_id: 'tool-call-1',
        arguments: { name: 'updated' }
      },
      target_json: {
        target_type: 'pipeline',
        target_id: '12'
      },
      policy_json: {
        operation_type: 'write',
        risk_summary: 'Will update pipeline metadata.'
      },
      display_json: {
        title: 'Update pipeline 12',
        summary: 'Will update pipeline metadata.'
      }
    }))
    expect(businessCreateResponse.status).toBe(404)
  })

  it('streams reasoning before answer deltas and persists final timing state', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          throw new Error('stream endpoint must not call non-stream completion')
        },
        async *stream() {
          yield { type: 'reasoning_delta', delta: '先分析 eBPF 的内核挂载点。' }
          yield { type: 'answer_delta', delta: '1. 网络观测\\n' }
          yield { type: 'answer_delta', delta: '2. 性能追踪\\n3. 安全控制' }
        }
      }
    })

    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: { type: 'openrouter', base_url: 'https://openrouter.ai/api/v1' },
      binding: { binding_id: 1 },
      model: { provider_model_key: 'openrouter/auto' },
      provider_credential_ref: {
        credential_id: 'openrouter-api-key'
      },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'latest' }
    }))

    const streamResponse = await app.request('/v1/sessions/1/entries/stream', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: 'ebpf 有什么功能',
      context_ref: { kind: 'current-page', route_path: '/store/ai-agents' },
      include_current_page: true,
      client_entry_id: 'entry-client-stream'
    }))

    expect(streamResponse.status).toBe(200)
    expect(streamResponse.headers.get('content-type')).toContain('text/event-stream')
    const body = await streamResponse.text()
    expect(body.indexOf('event: reasoning_delta')).toBeGreaterThan(body.indexOf('event: user_entry'))
    expect(body.indexOf('event: answer_delta')).toBeGreaterThan(body.indexOf('event: reasoning_delta'))
    expect(body).toContain('event: assistant_entry')
    expect(body).toContain('event: done')
    expect(body).toContain('run.step_announced')
    expect(body).toContain('先分析 eBPF')

    const listEntriesResponse = await app.request('/v1/sessions/1/entries/query', jsonRequest('POST', {}))
    const listedEntries = await listEntriesResponse.json()
    expect(listedEntries.data).toHaveLength(2)
    expect(listedEntries.data[1].content).toContain('网络观测')
    expect(listedEntries.data[1].output.reasoning).toContain('内核挂载点')
    expect(listedEntries.data[1].output.timings.total_ms).toBeGreaterThanOrEqual(0)

    const runtimeRunID = listedEntries.data[1].runtime_run_id
    const replayResponse = await app.request(`/v1/runs/${runtimeRunID}/events`, {
      method: 'GET',
      headers: { 'x-internal-token': 'secret' }
    })
    const replayBody = await replayResponse.json()
    expect(replayResponse.status).toBe(200)
    const stepEvents = replayBody.data.events.filter((event: { event_type: string, event_id: string }) => event.event_type === 'run.step_announced')
    expect(stepEvents.map((event: { payload_json: { stage: string } }) => event.payload_json.stage)).toEqual(expect.arrayContaining([
      'accepted',
      'profile_resolved',
      'first_reasoning',
      'first_answer',
      'completed'
    ]))
    expect(stepEvents.every((event: { event_id: string }) => /^r_w[0-9a-z]+_[0-9a-z]{6}_[0-9a-z]{6}:ev[0-9a-z]{6}$/.test(event.event_id))).toBe(true)
  })

  it('sanitizes internal Pi event persistence failures across direct and scoped run event replay', async () => {
    const store = createMemoryRuntimeStore()
    const agentEventStore = new InMemoryAgentEventStore()
    const app = createApp({ internalToken: 'secret', store, agentEventStore })
    const timestamp = '2026-06-29T00:00:00.000Z'

    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Pi Replay Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { provider_id: 'test' },
      model: { provider_model_key: 'test/fake' },
      prompt: { system: 'test' },
      status: 'draft'
    }))
    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: '1:draft',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'draft' }
    }))
    const sessionBody = await sessionResponse.json()
    const session = sessionBody.data
    const parentRuntimeRunID = 'r_w3_000001_000001'
    const parentActionID = 'a_w3_000001_000001_000001'
    const childRunLinkID = 'cl_w3_000001_000001_000001_000001'
    const runtimeRunID = 'r_w3_000001_000002'

    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: parentRuntimeRunID,
      session_id: session.id,
      workspace_id: 3,
      context_tags: [],
      agent_profile_id: 1,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'draft',
      agent_profile_snapshot_hash: 'sha256:parent-test',
      status: 'completed',
      input_entry_id: 0,
      request: { runtime_engine: 'legacy' },
      result: { runtime_engine: 'legacy' },
      usage: {},
      started_at: timestamp,
      finished_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })
    const parentAction = await store.saveAction({
      id: await store.nextActionId(),
      action_id: parentActionID,
      workspace_id: 3,
      context_tags: [],
      session_id: session.id,
      runtime_run_id: parentRuntimeRunID,
      action_kind: 'subagent.spawn',
      idempotency_key: 'pi-replay-parent-action',
      source: 'runtime',
      capability_id: 'subagent:1',
      input_json: { objective: 'Replay child events' },
      target_json: { agent_profile_id: 1 },
      policy_json: {},
      display_json: { name: 'Pi Replay Agent' },
      status: 'executed',
      requested_by: 12,
      executed_at: timestamp,
      result_json: { child_runtime_run_id: runtimeRunID },
      created_at: timestamp,
      updated_at: timestamp
    })
    const piRun = await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: 3,
      context_tags: [],
      agent_profile_id: 1,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'completed',
      input_entry_id: 0,
      request: { runtime_engine: 'pi', runtime_session_id: 's_w3_000001' },
      result: {
        runtime_engine: 'pi',
        runtime_session_id: 's_w3_000001',
        runtime_events: [{
          type: 'session.prompted',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:10',
          seq: 10,
          timestamp,
          message_id: 'user-1',
          prompt: 'hello pi',
          delivery: 'prompt'
        }, {
          type: 'session.tool.failed',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:11',
          seq: 11,
          timestamp,
          assistant_message_id: 'assistant-1',
          call_id: 'call-internal-persistence',
          tool_name: 'easydo_pipeline_get',
          error: { message: "Duplicate entry 's_w3_000001-11' for key 'uk_ai_agent_runtime_events_session_seq'" },
          result: { content: [{ type: 'text', text: "Duplicate entry 's_w3_000001-11' for key 'uk_ai_agent_runtime_events_session_seq'" }] }
        }, {
          type: 'session.reasoning.started',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:12',
          seq: 12,
          timestamp,
          assistant_message_id: 'assistant-1',
          reasoning_id: 'reasoning-internal-persistence',
        }, {
          type: 'session.reasoning.delta',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:13',
          seq: 13,
          timestamp,
          assistant_message_id: 'assistant-1',
          reasoning_id: 'reasoning-internal-persistence',
          delta: '注意：pipeline_get 返回了 Dupli'
        }, {
          type: 'session.reasoning.delta',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:14',
          seq: 14,
          timestamp,
          assistant_message_id: 'assistant-1',
          reasoning_id: 'reasoning-internal-persistence',
          delta: 'cate entry 错误，这是服务端日志'
        }, {
          type: 'session.reasoning.delta',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:15',
          seq: 15,
          timestamp,
          assistant_message_id: 'assistant-1',
          reasoning_id: 'reasoning-internal-persistence',
          delta: '写入的 bug'
        }, {
          type: 'session.reasoning.ended',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:16',
          seq: 16,
          timestamp,
          assistant_message_id: 'assistant-1',
          reasoning_id: 'reasoning-internal-persistence',
          text: ''
        }, {
          type: 'session.reasoning.ended',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:17',
          seq: 17,
          timestamp,
          assistant_message_id: 'assistant-1',
          reasoning_id: 'reasoning-domain-duplicate',
          text: "业务导入返回 Duplicate entry 'customer-42' for key 'uk_customers_external_id'，请检查输入。"
        }, {
          type: 'session.tool.failed',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:18',
          seq: 18,
          timestamp,
          assistant_message_id: 'assistant-1',
          call_id: 'call-domain-duplicate',
          tool_name: 'easydo_pipeline_create',
          error: { message: "Duplicate entry 'pipeline-42' for key 'uk_pipelines_name'" },
          result: { content: [{ type: 'text', text: "Duplicate entry 'pipeline-42' for key 'uk_pipelines_name'" }] }
        }, {
          type: 'session.tool.failed',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:19',
          seq: 19,
          timestamp,
          assistant_message_id: 'assistant-1',
          call_id: 'call-genuine-failure',
          tool_name: 'easydo_pipeline_get',
          error: { message: 'Tool easydo_pipeline_get returned 500' },
          result: { content: [{ type: 'text', text: 'Tool easydo_pipeline_get returned 500' }] }
        }, {
          type: 'session.text.ended',
          session_id: 's_w3_000001',
          event_id: 's_w3_000001:20',
          seq: 20,
          timestamp,
          assistant_message_id: 'assistant-1',
          text_id: 'text-1',
          text: 'done'
        }]
      },
      usage: {},
      started_at: timestamp,
      finished_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })
    for (let seq = 1; seq < 10; seq += 1) {
      await agentEventStore.append({
        type: 'session.model.switched',
        session_id: 's_w3_000001',
        event_id: `s_w3_000001:${seq}`,
        seq: 0,
        timestamp,
        message_id: `model-switch-${seq}`,
        model: { provider_id: 'test', id: 'fake' }
      })
    }
    for (const event of piRun.result.runtime_events as Array<Record<string, unknown>>) {
      await agentEventStore.append({ ...event, runtime_run_id: runtimeRunID } as never)
    }
    await store.saveChildRunLink({
      child_run_link_id: childRunLinkID,
      workspace_id: 3,
      parent_runtime_run_id: parentRuntimeRunID,
      parent_action_id: parentActionID,
      parent_action_internal_id: parentAction.id,
      child_seq: 1,
      child_session_id: session.id,
      child_runtime_run_id: runtimeRunID,
      child_profile_id: 1,
      child_profile_version_id: 0,
      snapshot_digest: 'sha256:child-test',
      status: 'completed',
      created_at: timestamp,
      completed_at: timestamp
    })

    const replayResponse = await app.request(`/v1/runs/${runtimeRunID}/events`, {
      method: 'GET',
      headers: { 'x-internal-token': 'secret' }
    })
    const replayBody = await replayResponse.json()

    expect(replayResponse.status).toBe(200)
    expect(replayBody.data.events.map((event: { event_id: string }) => event.event_id)).toEqual([
      's_w3_000001:10',
      's_w3_000001:17',
      's_w3_000001:18',
      's_w3_000001:19',
      's_w3_000001:20',
      's_w3_000001:21'
    ])
    expect(replayBody.data.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([10, 17, 18, 19, 20, 21])
    expect(replayBody.data.events.at(-1)).toMatchObject({ event_type: 'run.completed' })
    const renderedReplay = JSON.stringify(replayBody.data.events)
    expect(renderedReplay).not.toContain('uk_ai_agent_runtime_events_session_seq')
    expect(renderedReplay).not.toContain('服务端日志写入')
    expect(renderedReplay).toContain('uk_customers_external_id')
    expect(renderedReplay).toContain('uk_pipelines_name')
    expect(renderedReplay).toContain('Tool easydo_pipeline_get returned 500')
    const reconstructedReplay = replayBody.data.events.flatMap((event: { payload_json?: Record<string, unknown> }) => {
      const payload = event.payload_json || {}
      return [payload.text, payload.delta, payload.message].filter((value): value is string => typeof value === 'string')
    }).join('')
    expect(reconstructedReplay).not.toContain('Duplicate entry 错误，这是服务端日志写入的 bug')
    expect(replayBody.data.events[1]).toMatchObject({
      event_id: 's_w3_000001:17',
      event_seq: 17,
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      event_type: 'session.reasoning.ended',
      payload_json: expect.objectContaining({
        runtime_session_id: 's_w3_000001',
        runtime_run_id: runtimeRunID,
        session_id: session.id,
        event_id: 's_w3_000001:17',
        event_seq: 17
      }),
      display_json: expect.objectContaining({ event_type: 'session.reasoning.ended' })
    })

    const afterResponse = await app.request(`/v1/runs/${runtimeRunID}/events?after_event_id=${encodeURIComponent('s_w3_000001:11')}`, {
      method: 'GET',
      headers: { 'x-internal-token': 'secret' }
    })
    const afterBody = await afterResponse.json()

    expect(afterResponse.status).toBe(200)
    expect(afterBody.data.events.map((event: { event_id: string }) => event.event_id)).toEqual([
      's_w3_000001:17',
      's_w3_000001:18',
      's_w3_000001:19',
      's_w3_000001:20',
      's_w3_000001:21'
    ])

    const afterHiddenDeltaResponse = await app.request(`/v1/runs/${runtimeRunID}/events?after_event_id=${encodeURIComponent('s_w3_000001:14')}`, {
      method: 'GET',
      headers: { 'x-internal-token': 'secret' }
    })
    const afterHiddenDeltaBody = await afterHiddenDeltaResponse.json()
    expect(afterHiddenDeltaResponse.status).toBe(200)
    expect(afterHiddenDeltaBody.data.events.map((event: { event_id: string }) => event.event_id)).toEqual([
      's_w3_000001:17',
      's_w3_000001:18',
      's_w3_000001:19',
      's_w3_000001:20',
      's_w3_000001:21'
    ])

    const deniedScopedResponse = await app.request(`/v1/runs/${runtimeRunID}/events/query`, jsonRequest('POST', {}))
    expect(deniedScopedResponse.status).toBe(404)

    const otherUserScopedResponse = await app.request(`/v1/runs/${runtimeRunID}/events/query`, jsonRequestAs('POST', {
      child_run_link_id: childRunLinkID,
      parent_runtime_run_id: parentRuntimeRunID,
      parent_action_id: parentActionID
    }, {
      user_id: 99,
      username: 'other-user',
      auth_session_id: 'auth-session-other'
    }))
    expect(otherUserScopedResponse.status).toBe(404)

    const scopedResponse = await app.request(`/v1/runs/${runtimeRunID}/events/query`, jsonRequest('POST', {
      child_run_link_id: childRunLinkID,
      parent_runtime_run_id: parentRuntimeRunID,
      parent_action_id: parentActionID,
      after_event_id: 's_w3_000001:11'
    }))
    const scopedBody = await scopedResponse.json()

    expect(scopedResponse.status).toBe(200)
    expect(scopedBody.data.events.map((event: { event_id: string }) => event.event_id)).toEqual([
      's_w3_000001:17',
      's_w3_000001:18',
      's_w3_000001:19',
      's_w3_000001:20',
      's_w3_000001:21'
    ])
    const renderedScopedReplay = JSON.stringify(scopedBody.data.events)
    expect(renderedScopedReplay).not.toContain('uk_ai_agent_runtime_events_session_seq')
    expect(renderedScopedReplay).not.toContain('服务端日志写入')
    expect(renderedScopedReplay).toContain('uk_customers_external_id')
    expect(renderedScopedReplay).toContain('uk_pipelines_name')
    expect(renderedScopedReplay).toContain('Tool easydo_pipeline_get returned 500')
  })

  it('does not implement action decision stream by replaying a completed continuation payload', async () => {
    const serverSource = await import('node:fs/promises').then((fs) => fs.readFile(new URL('../src/server.ts', import.meta.url), 'utf8'))

    expect(serverSource).not.toContain('function continuationSSE')
    expect(serverSource).not.toContain('continuationSSE(result as Record<string, unknown>)')
    expect(serverSource).toContain('decision/stream')
    expect(serverSource).toContain('createSseResponse(')
    expect(serverSource).toContain('actionService.decideAndContinueStream(')
  })

  it('resolves provider credential references before calling the page assistant model', async () => {
    const modelCalls: Array<Record<string, unknown>> = []
    const store = createMemoryRuntimeStore()
    store.resolveProviderCredentialRef = async (workspaceID, providerID, credentialID) => {
      expect(workspaceID).toBe(3)
      expect(String(providerID)).toBe('1')
      expect(String(credentialID)).toBe('7')
      return {
        credential_id: '7',
        secret_ref: { api_key: 'provider-level-openrouter-key' }
      }
    }
    const app = createApp({
      internalToken: 'secret',
      store,
      chatModelClient: {
        async complete(request: Record<string, unknown>) {
          modelCalls.push(request)
          return { text: 'GLM 已接入页面助手。', usage: { total_tokens: 31 } }
        }
      }
    })

    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'page-ai-assistant',
      profile_kind: 'assistant',
      context_tags: ['page-assistant'],
      provider: {
        provider_id: 1,
        provider_type: 'openrouter',
        base_url: 'https://openrouter.ai/api/v1'
      },
      binding: {
        binding_id: 3,
        model_provider_id: 3,
        provider_model_key: 'z-ai/glm-4.5-air:free'
      },
      model: {
        model_id: 3,
        provider_model_key: 'z-ai/glm-4.5-air:free'
      },
      provider_credential_ref: { credential_id: 7 },
      response_mode: 'mixed'
    }))
    await app.request('/v1/agent-profiles/1/publish', jsonRequest('POST', {}))

    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'draft' }
    }))
    expect(sessionResponse.status).toBe(200)

    const entryResponse = await app.request('/v1/sessions/1/entries', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: '测试 GLM 页面助手链路',
      context_ref: { kind: 'current-page', route_path: '/store/ai-agents' },
      include_current_page: true,
      client_entry_id: 'entry-client-provider-credential'
    }))
    expect(entryResponse.status).toBe(200)

    const taskModelCalls = agentTaskModelCalls(modelCalls)
    expect(taskModelCalls).toHaveLength(1)
    expect(taskModelCalls[0].profile).toMatchObject({
      provider: { provider_id: 1, provider_type: 'openrouter' },
      model: { provider_model_key: 'z-ai/glm-4.5-air:free' },
      provider_credential_ref: {
        credential_id: '7'
      }
    })
  })

  it('rejects the removed pipeline-task context capability', async () => {
    const app = createApp({ internalToken: 'secret' })
    const profileResponse = await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Pipeline Agent',
      profile_kind: 'task',
      context_tags: ['pipeline-task'],
      provider: { provider_id: 1 },
      binding: { binding_id: 1 },
      model: { model_id: 1 },
      input_schema: { type: 'object' },
      output_schema: { type: 'object' },
      response_mode: 'json'
    }))
    expect(profileResponse.status).toBe(400)
    await expect(profileResponse.json()).resolves.toMatchObject({ code: 'context_tag_unknown' })
  })

  it('approves pending actions through the action decision API', async () => {
    const app = createApp({
      internalToken: 'secret',
      chatModelClient: {
        async complete() {
          return {
            text: 'Needs action approval.',
            tool_calls: [{
              id: 'tool-call-1',
              name: 'easydo_pipeline_update',
              arguments: { name: 'updated' },
              operation_type: 'write',
              target_type: 'pipeline',
              target_id: '12'
            }]
          }
        }
      }
    })
    await app.request('/v1/agent-profiles', jsonRequest('POST', {
      name: 'Action Agent',
      profile_kind: 'assistant',
      context_tags: [],
      provider: { type: 'openai-compatible' },
      model: { provider_model_key: 'test-model' },
      provider_credential_ref: { env: 'EASYDO_TEST_PROVIDER_API_KEY' },
      confirmation_policy: { write_tools_require_confirmation: true },
      response_mode: 'mixed'
    }))
    await app.request('/v1/sessions/current', jsonRequest('POST', {
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '3',
      profile_selection: { agent_profile_id: 1, agent_profile_version_id: 'draft' }
    }))
    const runResponse = await app.request('/v1/sessions/1/entries', jsonRequest('POST', {
      runtime_engine: 'legacy',
      content: 'update pipeline',
      client_entry_id: 'approve-action-entry'
    }))
    const runBody = await runResponse.json()
    const action = runBody.data.assistant_entry.output.agent_actions[0]
    expect(action.status).toBe('awaiting_decision')

    const approveResponse = await app.request(`/v1/actions/${action.id}/decision`, jsonRequest('POST', {
      decision: 'approve_once',
      client_decision_id: 'approve-action-api'
    }))
    expect(approveResponse.status).toBe(200)
    const approved = await approveResponse.json()
    expect(approved.data.status).toBe('approved')
    expect(approved.data.decided_by).toBe(12)
  })
})
