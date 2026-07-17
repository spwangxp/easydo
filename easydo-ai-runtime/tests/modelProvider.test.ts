import { describe, expect, it, vi } from 'vitest'
import { CompositeChatModelClient, ModelProviderError, ProviderAdapterChatModelClient } from '../src/services/modelProvider.js'
import type { AgentProfileSnapshot, AISession } from '../src/domain/runtime.js'

function profile(overrides: Partial<AgentProfileSnapshot> = {}): AgentProfileSnapshot {
  return {
    id: 1,
    workspace_id: 3,
    name: 'Page Assistant',
    description: '',
    profile_kind: 'assistant',
    context_tags: ['page-assistant'],
    provider: { type: 'openrouter', base_url: 'https://openrouter.ai/api/v1/' },
    binding: {},
    model: { provider_model_key: 'openrouter/auto' },
    provider_credential_ref: {
      credential_id: 'openrouter-api-key',
      secret_ref: { api_key: 'test-openrouter-key' }
    },
    inference: { temperature: 0.3, max_tokens: 512 },
    prompt: { system: 'Help EasyDo users from current page context.' },
    skills: [],
    subagents: [],
    mcp_servers: [],
    context_contract: {},
    input_schema: {},
    output_schema: {},
    memory_policy: {},
    confirmation_policy: {},
    response_mode: 'mixed',
    status: 'active',
    created_by: 12,
    ...overrides
  }
}

function session(): AISession {
  return {
    id: 7,
    workspace_id: 3,
    context_tags: ['page-assistant'],
    session_kind: 'chat',
    user_id: 12,
    auth_session_id: 'auth-session-1',
    business_type: 'workspace',
    business_id: '3',
    status: 'active',
    agent_profile_id: 1,
    agent_profile_version_id: 2,
    agent_profile_snapshot_hash: 'sha256:test',
    title: 'Page Assistant',
    entry_count: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString()
  }
}

describe('provider adapter chat model client', () => {
  it('uses provider credential secret and calls OpenRouter as an OpenAI-compatible chat completion', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        choices: [{ message: { content: 'OpenRouter answer' } }],
        usage: { total_tokens: 18 }
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile(),
      session: session(),
      history: [],
      content: '解释当前页面',
      context_ref: { kind: 'current-page', route_path: '/store/ai-agents' },
      include_current_page: true
    })

    expect(result?.text).toBe('OpenRouter answer')
    expect(result?.usage?.total_tokens).toBe(18)
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('https://openrouter.ai/api/v1/chat/completions')
    expect(init?.headers).toMatchObject({
      Authorization: 'Bearer test-openrouter-key',
      'Content-Type': 'application/json'
    })
    const body = JSON.parse(String(init?.body))
    expect(body.model).toBe('openrouter/auto')
    expect(body.temperature).toBe(0.3)
    expect(body.max_tokens).toBe(512)
    expect(body.messages.map((message: { role: string }) => message.role)).toEqual(['system', 'system', 'user'])
    expect(body.messages[1].content).toContain('/store/ai-agents')
  })

  it('honors raw token auth scheme from provider credentials', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        choices: [{ message: { content: 'raw auth answer' } }]
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    await client.complete({
      profile: profile({
        provider_credential_ref: {
          credential_id: 'custom-token',
          secret_ref: {
            token: 'Token raw-provider-token',
            token_type: 'raw'
          }
        }
      }),
      session: session(),
      history: [],
      content: '解释当前页面',
      context_ref: {},
      include_current_page: false
    })

    const [, init] = fetchImpl.mock.calls[0]
    expect(init?.headers).toMatchObject({
      Authorization: 'Token raw-provider-token'
    })
  })

  it('returns an actionable authentication error when the provider rejects credentials', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        error: { message: 'Missing Authentication header' }
      }), { status: 401 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    await expect(client.complete({
      profile: profile(),
      session: session(),
      history: [],
      content: '解释当前页面',
      context_ref: {},
      include_current_page: false
    })).rejects.toMatchObject({
      code: 'provider_auth_failed',
      message: expect.stringContaining('请检查 AI Provider')
    })
  })

  it('passes output schemas as OpenAI-compatible json_schema response format', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        choices: [{ message: { content: '{"summary":"ok"}' } }]
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    await client.complete({
      profile: profile({ response_mode: 'schema' }),
      session: session(),
      history: [],
      content: '解释当前页面',
      context_ref: { kind: 'current-page', route_path: '/store/ai-agents' },
      include_current_page: true,
      output_schema: {
        type: 'object',
        required: ['summary'],
        properties: { summary: { type: 'string' } }
      }
    })

    const [, init] = fetchImpl.mock.calls[0]
    const body = JSON.parse(String(init?.body))
    expect(body.response_format).toEqual({
      type: 'json_schema',
      json_schema: {
        name: 'easydo_output',
        strict: true,
        schema: {
          type: 'object',
          required: ['summary'],
          properties: { summary: { type: 'string' } }
        }
      }
    })
    expect(body.messages.find((message: { content: string }) => message.content.includes('Final answer must satisfy'))).toBeTruthy()
  })

  it('extracts OpenAI-compatible tool calls from non-streaming responses', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        choices: [{
          message: {
            content: '需要确认。',
            tool_calls: [{
              id: 'tool-call-1',
              type: 'function',
              function: {
                name: 'easydo_pipeline_trigger',
                arguments: '{"pipeline_id":12,"operation_type":"execute"}'
              }
            }]
          }
        }]
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile(),
      session: session(),
      history: [],
      content: '触发流水线',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    })

    expect(result?.tool_calls?.[0]).toMatchObject({
      id: 'tool-call-1',
      name: 'easydo_pipeline_trigger',
      arguments: { pipeline_id: 12, operation_type: 'execute' }
    })
  })

  it('compiles user decisions, tool results, and subagent results into subsequent model history', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        choices: [{ message: { content: 'continued answer' } }]
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)
    const timestamp = new Date().toISOString()

    await client.complete({
      profile: profile(),
      session: session(),
      history: [
        {
          id: 1,
          session_id: 7,
          workspace_id: 3,
          user_id: 12,
          seq: 1,
          entry_type: 'message',
          role: 'assistant',
          status: 'completed',
          content: '需要确认。',
          content_blocks: [],
          input: {},
          output: {
            subagent_results: [{ child_runtime_run_id: 'r_w3_000002_000001', summary: 'child checked context' }]
          },
          idempotency_key: 'entry-history-assistant',
          created_at: timestamp,
          updated_at: timestamp
        },
        {
          id: 2,
          session_id: 7,
          workspace_id: 3,
          user_id: 12,
          seq: 2,
          entry_type: 'run_event',
          role: 'user',
          status: 'completed',
          content: '已确认 触发流水线 12',
          content_blocks: [],
          input: { action_id: 'a_w3_000001_000001_000001', decision: 'approve_once' },
          output: { decision: 'approve_once' },
          idempotency_key: 'entry-history-decision',
          created_at: timestamp,
          updated_at: timestamp
        },
        {
          id: 3,
          session_id: 7,
          workspace_id: 3,
          user_id: 12,
          seq: 3,
          entry_type: 'tool_result',
          role: 'tool',
          status: 'completed',
          content: 'pipeline triggered',
          content_blocks: [],
          input: { action_id: 'a_w3_000001_000001_000001' },
          output: { structured_content: { run_id: 99 } },
          idempotency_key: 'entry-history-tool',
          created_at: timestamp,
          updated_at: timestamp
        }
      ],
      content: '继续',
      context_ref: {},
      include_current_page: false
    })

    const [, init] = fetchImpl.mock.calls[0]
    const body = JSON.parse(String(init?.body))
    const content = body.messages.map((message: { content: string }) => message.content).join('\n')
    expect(content).toContain('User action decision')
    expect(content).toContain('Tool/action result')
    expect(content).toContain('Sub-agent results from prior assistant turn')
    expect(content).toContain('run_id')
  })

  it('requests provider reasoning stream when thinking level is enabled', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response([
        'data: {"choices":[{"delta":{"reasoning":"先分析。"}}]}',
        '',
        'data: {"choices":[{"delta":{"content":"答案"}}]}',
        '',
        'data: [DONE]',
        '',
        ''
      ].join('\n'), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const events = []
    for await (const event of client.stream({
      profile: profile({ inference: { temperature: 0.3, max_tokens: 512, thinking_level: 'medium' } }),
      session: session(),
      history: [],
      content: '解释 eBPF',
      context_ref: { kind: 'current-page' },
      include_current_page: true
    })) {
      events.push(event)
    }

    const [, init] = fetchImpl.mock.calls[0]
    const body = JSON.parse(String(init?.body))
    expect(body.stream).toBe(true)
    expect(body.include_reasoning).toBe(true)
    expect(body.reasoning).toEqual({ effort: 'medium' })
    expect(events.map((event) => event.type)).toEqual(['reasoning_delta', 'answer_delta'])
  })

  it('streams OpenAI-compatible tool calls as structured events', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response([
        'data: {"choices":[{"delta":{"tool_calls":[{"id":"tool-call-1","function":{"name":"easydo_pipeline_trigger","arguments":"{\\"pipeline_id\\":12,\\"operation_type\\":\\"execute\\"}"}}]}}]}',
        '',
        'data: [DONE]',
        '',
        ''
      ].join('\n'), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const events = []
    for await (const event of client.stream({
      profile: profile(),
      session: session(),
      history: [],
      content: '触发流水线',
      context_ref: { kind: 'current-page', route_path: '/pipeline/12' },
      include_current_page: true
    })) {
      events.push(event)
    }

    expect(events[0]).toMatchObject({
      type: 'tool_call',
      tool_call: {
        id: 'tool-call-1',
        name: 'easydo_pipeline_trigger',
        arguments: { pipeline_id: 12, operation_type: 'execute' }
      }
    })
  })

  it('assembles streamed OpenAI-compatible tool call argument deltas before emitting the tool event', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response([
        'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tool-call-gpu","function":{"name":"easydo_resource_gpu_usage","arguments":"{\\"workspace_id\\":"}}]}}]}',
        '',
        'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"3,\\"resource_id\\":1}"}}]}}]}',
        '',
        'data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}',
        '',
        'data: [DONE]',
        '',
        ''
      ].join('\n'), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const events = []
    for await (const event of client.stream({
      profile: profile(),
      session: session(),
      history: [],
      content: '查询 GPU',
      context_ref: { kind: 'current-page', route_path: '/resources/1' },
      include_current_page: true
    })) {
      events.push(event)
    }

    expect(events).toEqual([expect.objectContaining({
      type: 'tool_call',
      tool_call: expect.objectContaining({
        id: 'tool-call-gpu',
        name: 'easydo_resource_gpu_usage',
        arguments: { workspace_id: 3, resource_id: 1 }
      })
    })])
  })

  it('parses a final SSE delta even when the provider omits the trailing blank line', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response('data: {"choices":[{"delta":{"content":"最后一段"}}]}', { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const events = []
    for await (const event of client.stream({
      profile: profile(),
      session: session(),
      history: [],
      content: '解释',
      context_ref: { kind: 'current-page' },
      include_current_page: true
    })) {
      events.push(event)
    }

    expect(events).toEqual([{ type: 'answer_delta', delta: '最后一段', raw: { choices: [{ delta: { content: '最后一段' } }] } }])
  })

  it('fails instead of returning a fake answer when no provider supports the profile', async () => {
    const client = new CompositeChatModelClient([new ProviderAdapterChatModelClient()])

    await expect(client.complete({
      profile: profile({ provider: { type: 'unsupported' } }),
      session: session(),
      history: [],
      content: '解释',
      context_ref: { kind: 'current-page' },
      include_current_page: true
    })).rejects.toMatchObject({
      name: 'ModelProviderError',
      source: 'provider',
      code: 'model_provider_unsupported',
      http_status: 400,
      retryable: false
    } satisfies Partial<ModelProviderError>)
  })

  it('uses OpenAI Responses API for OpenAI provider and parses function calls from output parts', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        output: [
          { type: 'reasoning', summary: [{ type: 'summary_text', text: '需要读取流水线参数。' }] },
          {
            type: 'function_call',
            call_id: 'call-e1',
            name: 'easydo_pipeline_trigger',
            arguments: '{"pipeline_id":13,"operation_type":"execute"}'
          },
          {
            type: 'message',
            content: [{ type: 'output_text', text: '我将触发 e1。' }]
          }
        ],
        usage: { total_tokens: 42 }
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile({
        provider: { provider_type: 'openai', base_url: 'https://api.openai.com/v1' },
        model: { provider_model_key: 'gpt-5' }
      }),
      session: session(),
      history: [],
      content: '触发 e1',
      context_ref: { kind: 'current-page', route_path: '/pipelines/13' },
      include_current_page: true,
      tools: [{ name: 'easydo_pipeline_trigger', input_schema: { type: 'object' } }]
    })

    expect(fetchImpl).toHaveBeenCalledTimes(1)
    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('https://api.openai.com/v1/responses')
    const body = JSON.parse(String(init?.body))
    expect(body.model).toBe('gpt-5')
    expect(body.input).toEqual(expect.arrayContaining([expect.objectContaining({ role: 'user' })]))
    expect(result?.text).toBe('我将触发 e1。')
    expect(result?.usage?.total_tokens).toBe(42)
    expect(result?.tool_calls).toEqual([expect.objectContaining({
      id: 'call-e1',
      name: 'easydo_pipeline_trigger',
      arguments: { pipeline_id: 13, operation_type: 'execute' }
    })])
    expect(result?.parts?.map((part) => part.type)).toEqual(['reasoning', 'tool_call', 'text'])
  })

  it('generates readable bounded tool call ids for OpenAI Responses output parts without call ids', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        output: [
          { type: 'reasoning', summary: [{ type: 'summary_text', text: '需要读取流水线参数。' }] },
          {
            type: 'function_call',
            name: 'easydo_pipeline_trigger',
            arguments: '{"pipeline_id":13,"operation_type":"execute"}'
          },
          {
            type: 'function_call',
            name: 'easydo_resource_gpu_usage',
            arguments: '{"workspace_id":3,"resource_id":1}'
          }
        ]
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile({
        provider: { provider_type: 'openai', base_url: 'https://api.openai.com/v1' },
        model: { provider_model_key: 'gpt-5' }
      }),
      session: session(),
      history: [],
      content: '触发 e1 并查 GPU',
      context_ref: {},
      include_current_page: false,
      tools: [
        { name: 'easydo_pipeline_trigger', input_schema: { type: 'object' } },
        { name: 'easydo_resource_gpu_usage', input_schema: { type: 'object' } }
      ]
    })

    expect(result?.tool_calls?.map((toolCall) => toolCall.id)).toEqual([
      'tc_easydo_pipeline_trigger_0001',
      'tc_easydo_resource_gpu_usage_0002'
    ])
  })

  it('uses Anthropic Messages API and parses tool_use content blocks', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        content: [
          { type: 'text', text: '需要查询资源。' },
          {
            type: 'tool_use',
            id: 'toolu-gpu',
            name: 'easydo_resource_gpu_usage',
            input: { workspace_id: 3, resource_id: 1 }
          }
        ],
        usage: { input_tokens: 12, output_tokens: 9 },
        stop_reason: 'tool_use'
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile({
        provider: { provider_type: 'anthropic', base_url: 'https://api.anthropic.com/v1' },
        model: { provider_model_key: 'claude-sonnet-4-5' }
      }),
      session: session(),
      history: [],
      content: '查 GPU',
      context_ref: {},
      include_current_page: false,
      tools: [{ name: 'easydo_resource_gpu_usage', input_schema: { type: 'object' } }]
    })

    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('https://api.anthropic.com/v1/messages')
    expect(init?.headers).toMatchObject({
      'x-api-key': 'test-openrouter-key',
      'anthropic-version': expect.any(String)
    })
    const body = JSON.parse(String(init?.body))
    expect(body.system).toContain('Help EasyDo users')
    expect(body.tools[0]).toMatchObject({ name: 'easydo_resource_gpu_usage', input_schema: { type: 'object' } })
    expect(result?.text).toBe('需要查询资源。')
    expect(result?.tool_calls?.[0]).toMatchObject({
      id: 'toolu-gpu',
      name: 'easydo_resource_gpu_usage',
      arguments: { workspace_id: 3, resource_id: 1 }
    })
    expect(result?.parts?.map((part) => part.type)).toEqual(['text', 'tool_call'])
  })

  it('generates readable bounded tool call ids for Anthropic tool_use blocks without ids', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        content: [
          { type: 'text', text: '需要查询资源。' },
          {
            type: 'tool_use',
            name: 'easydo_resource_gpu_usage',
            input: { workspace_id: 3, resource_id: 1 }
          }
        ],
        stop_reason: 'tool_use'
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile({
        provider: { provider_type: 'anthropic', base_url: 'https://api.anthropic.com/v1' },
        model: { provider_model_key: 'claude-sonnet-4-5' }
      }),
      session: session(),
      history: [],
      content: '查 GPU',
      context_ref: {},
      include_current_page: false,
      tools: [{ name: 'easydo_resource_gpu_usage', input_schema: { type: 'object' } }]
    })

    expect(result?.tool_calls?.[0]?.id).toBe('tc_easydo_resource_gpu_usage_0001')
  })

  it('uses Gemini generateContent API and parses functionCall parts', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response(JSON.stringify({
        candidates: [{
          content: {
            role: 'model',
            parts: [
              { text: '准备触发流水线。' },
              {
                functionCall: {
                  name: 'easydo_pipeline_trigger',
                  args: { pipeline_id: 13, operation_type: 'execute' }
                }
              }
            ]
          },
          finishReason: 'STOP'
        }],
        usageMetadata: { totalTokenCount: 37 }
      }), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const result = await client.complete({
      profile: profile({
        provider: { provider_type: 'google', base_url: 'https://generativelanguage.googleapis.com/v1beta' },
        model: { provider_model_key: 'gemini-2.5-pro' }
      }),
      session: session(),
      history: [],
      content: '触发 e1',
      context_ref: {},
      include_current_page: false,
      tools: [{ name: 'easydo_pipeline_trigger', input_schema: { type: 'object' } }]
    })

    const [url, init] = fetchImpl.mock.calls[0]
    expect(url).toBe('https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent')
    expect(init?.headers).toMatchObject({ 'x-goog-api-key': 'test-openrouter-key' })
    const body = JSON.parse(String(init?.body))
    expect(body.tools[0].functionDeclarations[0].name).toBe('easydo_pipeline_trigger')
    expect(result?.text).toBe('准备触发流水线。')
    expect(result?.usage?.totalTokenCount).toBe(37)
    expect(result?.tool_calls?.[0]).toMatchObject({
      id: 'tc_easydo_pipeline_trigger_0001',
      name: 'easydo_pipeline_trigger',
      arguments: { pipeline_id: 13, operation_type: 'execute' }
    })
    expect(result?.parts?.map((part) => part.type)).toEqual(['text', 'tool_call'])
  })

  it('generates readable bounded tool call ids for streamed tool calls without ids', async () => {
    const fetchImpl = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) =>
      new Response([
        'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"easydo_pipeline_trigger","arguments":"{\\"pipeline_id\\":13}"}}]}}]}',
        '',
        'data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"name":"easydo_resource_gpu_usage","arguments":"{\\"workspace_id\\":3,\\"resource_id\\":1}"}}]}}]}',
        '',
        'data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}',
        '',
        'data: [DONE]',
        '',
        ''
      ].join('\n'), { status: 200 })
    ) as unknown as typeof fetch
    const client = new ProviderAdapterChatModelClient(fetchImpl)

    const events = []
    for await (const event of client.stream({
      profile: profile(),
      session: session(),
      history: [],
      content: '触发 e1 并查 GPU',
      context_ref: {},
      include_current_page: false
    })) {
      events.push(event)
    }

    expect(events.filter((event) => event.type === 'tool_call').map((event) => event.tool_call.id)).toEqual([
      'tc_easydo_pipeline_trigger_0001',
      'tc_easydo_resource_gpu_usage_0002'
    ])
  })
})
