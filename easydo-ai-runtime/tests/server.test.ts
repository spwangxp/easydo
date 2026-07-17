import { afterEach, describe, expect, it, vi } from 'vitest'
import { createServer } from 'node:http'
import type { ChatModelClient } from '../src/services/modelProvider.js'
import { createApp, createSseResponse, startRunOwnerSweeper } from '../src/server.js'
import { createMemoryRuntimeStore } from '../src/store/memoryRuntimeStore.js'
import { InMemoryAgentEventStore } from '../src/agent-runtime/eventStore.js'
import { RuntimeSourceError } from '../src/services/runtimeErrorClassifier.js'
import { createRuntimeLogger } from '../src/observability/runtimeLogger.js'
import { RuntimeMetrics } from '../src/observability/runtimeMetrics.js'

const actor = {
  user_id: 7,
  username: 'developer',
  system_role: 'user',
  workspace_id: 11,
  workspace_role: 'developer',
  auth_session_id: 'auth-1'
}

afterEach(() => {
  vi.useRealTimers()
})

function envelope(payload: Record<string, unknown> = {}) {
  return {
    request_id: 'req-1',
    idempotency_key: 'idem-1',
    actor,
    auth: { delegated_user_token: 'Bearer jwt-token', server_internal_token: 'secret' },
    payload
  }
}

function jsonRequest(body: unknown) {
  return new Request('http://runtime.local', {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      'x-internal-token': 'secret'
    },
    body: JSON.stringify(body)
  })
}

async function withMcpServer() {
  const server = createServer(async (req, res) => {
    let body = ''
    for await (const chunk of req) body += chunk
    const rpc = JSON.parse(body)
    res.setHeader('content-type', 'application/json')
    if (rpc.method === 'initialize') {
      res.setHeader('mcp-session-id', 'server-test-session')
      res.end(JSON.stringify({ jsonrpc: '2.0', id: rpc.id, result: { protocolVersion: '2025-06-18', capabilities: {} } }))
      return
    }
    if (rpc.method === 'notifications/initialized') {
      res.statusCode = 202
      res.end()
      return
    }
    res.end(JSON.stringify({
      jsonrpc: '2.0',
      id: rpc.id,
      result: {
        content: [{ type: 'text', text: 'server mcp executed' }],
        structuredContent: { ok: true },
        metadata: { request_id: 'server-mcp-1' }
      }
    }))
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('server address unavailable')
  return {
    url: `http://127.0.0.1:${address.port}`,
    close: () => new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
  }
}

describe('runtime HTTP server', () => {
  it('reconciles expired Run owners without incoming HTTP requests', async () => {
    vi.useFakeTimers()
    const reconcile = vi.fn(async () => [])

    const sweeper = startRunOwnerSweeper(reconcile, 250)
    await vi.advanceTimersByTimeAsync(750)
    sweeper.stop()

    expect(reconcile).toHaveBeenCalledTimes(3)
    expect(reconcile).toHaveBeenCalledWith(true)
  })

  it('reports owner sweeper failures through the structured logger', async () => {
    vi.useFakeTimers()
    const logs: string[] = []
    const logger = createRuntimeLogger({ sink: (line) => logs.push(line) })
    const sweeper = startRunOwnerSweeper(async () => { throw new Error('lease secret password=hunter2') }, 100, undefined, logger)

    await vi.advanceTimersByTimeAsync(100)
    sweeper.stop()

    expect(logs.map((line) => JSON.parse(line))).toEqual([
      expect.objectContaining({
        level: 'error',
        component: 'run-owner-sweeper',
        operation: 'reconcile',
        outcome: 'failed',
        code: 'runtime_internal_error',
        category: 'internal'
      })
    ])
    expect(logs.join('\n')).not.toContain('hunter2')
  })

  it('reports durable owner lease replica health', async () => {
    const app = createApp({ internalToken: 'secret', runtimeInstanceID: 'runtime-test-1' })

    const response = await app.request('/healthz')
    const body = await response.json()

    expect(response.status).toBe(200)
    expect(body).toEqual({
      status: 'ok',
      service: 'easydo-ai-runtime',
      replica_mode: 'durable_owner_lease',
      instance_id: 'runtime-test-1'
    })
  })

  it('reports database readiness separately from runtime and external dependency checks', async () => {
    const store = createMemoryRuntimeStore() as ReturnType<typeof createMemoryRuntimeStore> & { ping: () => Promise<void> }
    store.ping = async () => {}
    const app = createApp({ internalToken: 'secret', store })

    const response = await app.request('/readyz')
    const body = await response.json()

    expect(response.status).toBe(200)
    expect(body).toEqual({
      status: 'ok',
      service: 'easydo-ai-runtime',
      checks: {
        database: { status: 'ok' },
        pi_runtime: { status: 'ok' },
        model_provider: { status: 'per_profile_runtime_check' },
        mcp: { status: 'per_resource_runtime_check' }
      }
    })
  })

  it('fails readiness when the configured database is unavailable', async () => {
    const store = createMemoryRuntimeStore() as ReturnType<typeof createMemoryRuntimeStore> & { ping: () => Promise<void> }
    store.ping = async () => { throw new Error('database unavailable') }
    const app = createApp({ internalToken: 'secret', store })

    const response = await app.request('/readyz')
    const body = await response.json()

    expect(response.status).toBe(503)
    expect(body.status).toBe('unavailable')
    expect(body.checks.database.status).toBe('unavailable')
  })

  it('rejects internal API requests without server token', async () => {
    const app = createApp({ internalToken: 'secret' })

    const response = await app.request('/v1/sessions')
    const body = await response.json()

    expect(response.status).toBe(401)
    expect(body.error).toBe('runtime_internal_token_required')
  })

  it('streams action decision continuation events after approve', async () => {
    const mcp = await withMcpServer()
    const store = createMemoryRuntimeStore()
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'tool finished answer' }
        }
        return {
          text: 'needs tool',
          tool_calls: [{
            id: 'call-1',
            name: 'easydo_test_write',
            arguments: { value: 'x' },
            operation_type: 'write',
            target_type: 'test',
            target_id: '1'
          }]
        }
      }
    }
    const app = createApp({ internalToken: 'secret', store, chatModelClient: client })
    try {
      await app.request('/v1/agent-resources', jsonRequest(envelope({
        resource_kind: 'mcp_server',
        resource_key: 'server-mcp',
        name: 'Server MCP',
        status: 'active',
        spec: {
          discovered_tools: [{ name: 'easydo_test_write' }],
          mcpServers: {
            server: {
              type: 'streamable_http',
              url: `${mcp.url}/mcp`
            }
          }
        }
      })))
      const profileResponse = await app.request('/v1/agent-profiles', jsonRequest(envelope({
        name: 'Common Agent',
        profile_kind: 'assistant',
        context_tags: [],
        model: { provider_model_key: 'test-model' },
        inference: { max_tokens: 100 },
        prompt: { system: 'test' },
        status: 'draft',
        mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'server-mcp' }]
      })))
      const profileBody = await profileResponse.json()
      const profile = profileBody.data
      const sessionResponse = await app.request('/v1/sessions/current', jsonRequest(envelope({
        session_kind: 'chat',
        business_type: 'agent_profile',
        business_id: `${profile.id}:draft`,
        profile_selection: {
          agent_profile_id: profile.id,
          agent_profile_version_id: 'draft',
          first_session_timestamp: '2026-06-05T00:00:00.000Z'
        }
      })))
      const sessionBody = await sessionResponse.json()
      const session = sessionBody.data
      const runResponse = await app.request(`/v1/sessions/${session.id}/entries`, jsonRequest(envelope({
        runtime_engine: 'legacy',
        content: 'please write',
        client_entry_id: 'server-confirm-stream-1'
      })))
      const runBody = await runResponse.json()
      const action = runBody.data.assistant_entry.output.agent_actions[0]

      const response = await app.request(`/v1/actions/${action.id}/decision/stream`, jsonRequest(envelope({
        decision: 'approve_once',
        client_decision_id: 'server-confirm-approve'
      })))
      const body = await response.text()

      expect(response.status).toBe(200)
      expect(response.headers.get('content-type')).toContain('text/event-stream')
      expect(body).toContain('event: tool.executed')
      expect(body).toContain('event: action.decision')
      expect(body).toMatch(/^id: r_w[0-9a-z]+_[0-9a-z]{6}_[0-9a-z]{6}:ev[0-9a-z]{6}$/m)
      expect(body).toContain('event: answer_delta')
      expect(body).toContain('tool finished answer')
      expect(body).toContain('event: action_entry')
      expect(body).toContain('event: assistant_entry')
      expect(body).toContain('event: done')
    } finally {
      await mcp.close()
    }
  })
})

describe('runtime event projection API', () => {
  it('ingests OpenCode-style agent runtime events and exposes replay plus projection', async () => {
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore() })
    const event = {
      type: 'session.prompted',
      session_id: 'sess-api-1',
      event_id: '',
      seq: 0,
      timestamp: '2026-06-29T00:00:00.000Z',
      message_id: 'user-api-1',
      prompt: 'hello runtime',
      files: [],
      delivery: 'prompt'
    }

    const ingest = await app.request('/v1/runtime-events', jsonRequest(envelope({ event })))
    expect(ingest.status).toBe(200)
    expect(await ingest.json()).toMatchObject({ data: { event_id: 'sess-api-1:1', seq: 1 } })

    const replay = await app.request('/v1/runtime-events/query', jsonRequest(envelope({ session_id: 'sess-api-1' })))
    expect(replay.status).toBe(200)
    expect(await replay.json()).toMatchObject({ data: { events: [{ type: 'session.prompted', seq: 1 }] } })

    const projection = await app.request('/v1/runtime-events/projection', jsonRequest(envelope({ session_id: 'sess-api-1' })))
    expect(projection.status).toBe(200)
    expect(await projection.json()).toMatchObject({
      data: {
        session: { id: 'sess-api-1', status: 'busy' },
        messages: [{ id: 'user-api-1', role: 'user', text: 'hello runtime' }]
      }
    })
  })

  it('uses the injected agent runtime event store for replay and projection APIs', async () => {
    const eventStore = new InMemoryAgentEventStore()
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore(), agentEventStore: eventStore })
    await eventStore.append({
      type: 'session.prompted',
      session_id: 'sess-injected-store',
      event_id: '',
      seq: 0,
      timestamp: '2026-06-30T00:00:00.000Z',
      message_id: 'user-injected-store',
      prompt: 'from injected store',
      files: [],
      delivery: 'prompt'
    })

    const replay = await app.request('/v1/runtime-events/query', jsonRequest(envelope({ session_id: 'sess-injected-store' })))
    const projection = await app.request('/v1/runtime-events/projection', jsonRequest(envelope({ session_id: 'sess-injected-store' })))

    expect(replay.status).toBe(200)
    expect(await replay.json()).toMatchObject({ data: { events: [{ prompt: 'from injected store' }] } })
    expect(projection.status).toBe(200)
    expect(await projection.json()).toMatchObject({ data: { messages: [{ text: 'from injected store' }] } })
  })

  it('exposes a run-level Pi approval decision endpoint', async () => {
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore() })

    const response = await app.request('/v1/runs/r_missing/pi-approval/decision', jsonRequest(envelope({
      decision: 'approve_once',
      client_decision_id: 'server-pi-approval-1'
    })))
    const body = await response.json()

    expect(response.status).toBe(404)
    expect(body).toMatchObject({ code: 'runtime_run_not_found' })
  })

  it('exposes a run-level Pi approval decision stream endpoint', async () => {
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore(), runtimeInstanceID: 'runtime-stream-test' })

    const response = await app.request('/v1/runs/r_missing/pi-approval/decision/stream', jsonRequest(envelope({
      decision: 'approve_once',
      client_decision_id: 'server-pi-approval-stream-1'
    })))
    const body = await response.json()

    expect(response.status).toBe(404)
    expect(response.headers.get('content-type')).toContain('application/json')
    expect(response.headers.get('x-request-id')).toBe('req-1')
    expect(body).toMatchObject({
      code: 'runtime_run_not_found',
      category: 'validation',
      retryable: false,
      terminal_status: 'failed',
      request_id: 'req-1'
    })
    const metrics = await (await app.request('/metrics')).text()
    expect(metrics).toContain('ai_runtime_requests_total{instance="runtime-stream-test",operation="pi_approval_stream",outcome="failed"} 1')
    expect(metrics).toContain('ai_runtime_terminal_total{category="validation",code="runtime_run_not_found",instance="runtime-stream-test"} 1')
  })
})

describe('SSE response boundary', () => {
  it('maps a synchronous stream factory failure to correlated JSON and structured error logging', async () => {
    const logs: string[] = []
    const response = await createSseResponse(() => {
      throw new RuntimeSourceError({
        source: 'validation',
        code: 'runtime_factory_invalid',
        message: 'Runtime stream factory is invalid',
        http_status: 400,
        retryable: false
      })
    }, 'req-sync-factory', {
      operation: 'test_sync_factory',
      logger: createRuntimeLogger({ sink: (line) => logs.push(line) })
    })

    expect(response.status).toBe(400)
    expect(response.headers.get('x-request-id')).toBe('req-sync-factory')
    await expect(response.json()).resolves.toMatchObject({
      code: 'runtime_factory_invalid',
      request_id: 'req-sync-factory'
    })
    expect(logs.map((line) => JSON.parse(line))).toContainEqual(expect.objectContaining({
      level: 'error',
      operation: 'test_sync_factory',
      outcome: 'failed',
      request_id: 'req-sync-factory',
      code: 'runtime_factory_invalid',
      category: 'validation'
    }))
  })

  it('returns JSON when the iterator fails before its first event', async () => {
    const response = await createSseResponse(async function * () {
      throw new RuntimeSourceError({
        source: 'validation',
        code: 'runtime_run_not_found',
        message: 'Runtime run not found',
        http_status: 404,
        retryable: false
      })
    }, 'req-before-first')

    expect(response.status).toBe(404)
    expect(response.headers.get('content-type')).toContain('application/json')
    expect(response.headers.get('x-request-id')).toBe('req-before-first')
    await expect(response.json()).resolves.toMatchObject({
      code: 'runtime_run_not_found',
      request_id: 'req-before-first'
    })
  })

  it('emits one correlated terminal error after streaming has started', async () => {
    const logs: string[] = []
    const response = await createSseResponse(async function * () {
      yield { event: 'runtime_event', data: { runtime_run_id: 'run-stream-failed', seq: 1 } }
      throw new RuntimeSourceError({
        source: 'provider',
        code: 'provider_timeout',
        message: 'Request timed out.',
        http_status: 504,
        retryable: true
      })
    }, 'req-after-first', {
      operation: 'test_stream',
      logger: createRuntimeLogger({ sink: (line) => logs.push(line) })
    })
    const body = await response.text()

    expect(response.status).toBe(200)
    expect(response.headers.get('content-type')).toContain('text/event-stream')
    expect(response.headers.get('x-request-id')).toBe('req-after-first')
    expect(body.match(/event: error/g)).toHaveLength(1)
    expect(body).toContain('"request_id":"req-after-first"')
    expect(body).toContain('"runtime_run_id":"run-stream-failed"')
    expect(body).toContain('"category":"provider_timeout"')
    expect(body).toContain('"retryable":true')
    expect(body).toContain('"terminal_status":"timeout"')
    expect(logs.map((line) => JSON.parse(line))).toContainEqual(expect.objectContaining({
      level: 'error',
      component: 'runtime-server',
      operation: 'test_stream',
      outcome: 'failed',
      request_id: 'req-after-first',
      runtime_run_id: 'run-stream-failed',
      code: 'provider_timeout',
      category: 'provider_timeout'
    }))
  })

  it('logs correlated JSON request success and app errors without request payloads', async () => {
    const logs: string[] = []
    const logger = createRuntimeLogger({ sink: (line) => logs.push(line) })
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore(), logger })

    const success = await app.request('/v1/operations/summary', jsonRequest(envelope({ prompt: 'never log this prompt' })))
    const failure = await app.request('/v1/sessions/not-a-number/query', jsonRequest(envelope({ api_key: 'never-log-key' })))

    expect(success.status).toBe(200)
    expect(failure.status).toBe(400)
    expect(logs.map((line) => JSON.parse(line))).toEqual(expect.arrayContaining([
      expect.objectContaining({
        level: 'info',
        component: 'runtime-server',
        operation: 'POST /v1/operations/summary',
        outcome: 'completed',
        request_id: 'req-1',
        workspace_id: 11
      }),
      expect.objectContaining({
        level: 'error',
        component: 'runtime-server',
        operation: 'POST /v1/sessions/not-a-number/query',
        outcome: 'failed',
        request_id: 'req-1',
        workspace_id: 11,
        code: 'session_id_invalid',
        category: 'validation'
      })
    ]))
    expect(logs.join('\n')).not.toContain('never log this prompt')
    expect(logs.join('\n')).not.toContain('never-log-key')
  })
})

describe('runtime metrics endpoint', () => {
  it('exposes Prometheus text without internal authentication', async () => {
    const app = createApp({
      internalToken: 'secret',
      store: createMemoryRuntimeStore(),
      runtimeInstanceID: 'runtime-metrics-test'
    })

    const response = await app.request('/metrics')
    const body = await response.text()

    expect(response.status).toBe(200)
    expect(response.headers.get('content-type')).toContain('text/plain')
    expect(body).toContain('ai_runtime_runtime_info{instance="runtime-metrics-test",status="ready"} 1')
  })

  it('returns the durable operations summary through the internal API', async () => {
    const app = createApp({
      internalToken: 'secret',
      store: createMemoryRuntimeStore(),
      runtimeInstanceID: 'runtime-operations-test'
    })

    const response = await app.request('/v1/operations/summary', jsonRequest(envelope()))

    expect(response.status).toBe(200)
    await expect(response.json()).resolves.toMatchObject({
      data: {
        instance_id: 'runtime-operations-test',
        replica: { instance_id: 'runtime-operations-test', status: 'ready' },
        runs: { active: 0, awaiting_approval: 0, awaiting_input: 0, stale: 0, orphaned: 0 },
        latency_5m: {
          first_response: { count: 0 },
          publish_delay: { count: 0 },
          replay_delay: { count: 0 }
        },
        latency_1h: {
          first_response: { count: 0 },
          publish_delay: { count: 0 },
          replay_delay: { count: 0 }
        },
        failures_1h: []
      }
    })
  })

  it('records first response only for reasoning text or tool output and tracks active SSE connections', async () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-stream-observed' })
    const response = await createSseResponse(async function * () {
      yield { event: 'user_entry', data: { entry: { id: 1 } } }
      yield { event: 'step.accepted', data: { elapsed_ms: 1 } }
      yield { event: 'answer_delta', data: { delta: 'hello' } }
      yield { event: 'done', data: {} }
    }, 'request-observed', { metrics, operation: 'entry_stream' })

    await response.text()
    const rendered = metrics.render()
    expect(rendered).toContain('ai_runtime_first_response_seconds_count{instance="runtime-stream-observed",outcome="started"} 1')
    expect(rendered).toContain('ai_runtime_sse_connections{instance="runtime-stream-observed",operation="entry_stream"} 0')
  })

  it('counts a yielded terminal error exactly once in the taxonomy metric', async () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-yielded-error' })
    const response = await createSseResponse(async function * () {
      yield { event: 'user_entry', data: { entry: { id: 1 } } }
      yield { event: 'error', data: { code: 'provider_timeout', category: 'provider_timeout' } }
    }, 'request-yielded-error', { metrics, operation: 'entry_stream' })

    await response.text()
    expect(metrics.render()).toContain('ai_runtime_terminal_total{category="provider_timeout",code="provider_timeout",instance="runtime-yielded-error"} 1')
  })
})

describe('runtime harness prompt API', () => {
  it('passes request, workspace, and parent Run correlation to the direct harness', async () => {
    const prompt = vi.fn(async () => ({
      session_id: 'sess-direct',
      user_message_id: 'user-direct',
      assistant_message_id: 'assistant-direct'
    }))
    const app = createApp({
      internalToken: 'secret',
      store: createMemoryRuntimeStore(),
      agentHarnessRunner: { prompt }
    })

    const response = await app.request('/v1/runtime-harness/prompt', jsonRequest(envelope({
      session_id: 'sess-direct',
      runtime_run_id: 'run-direct',
      parent_runtime_run_id: 'run-parent-direct',
      prompt: 'hello'
    })))

    expect(response.status).toBe(200)
    expect(prompt).toHaveBeenCalledWith(expect.objectContaining({
      requestID: 'req-1',
      workspaceID: 11,
      sessionID: 'sess-direct',
      runtimeRunID: 'run-direct',
      parentRuntimeRunID: 'run-parent-direct'
    }))
  })

  it('fails explicitly when Pi model configuration is missing instead of using a fake fallback', async () => {
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore() })

    const response = await app.request('/v1/runtime-harness/prompt', jsonRequest(envelope({
      session_id: 'sess-no-model',
      runtime_run_id: 'run-no-model',
      prompt: 'hello'
    })))
    const body = await response.json()

    expect(response.status).toBe(400)
    expect(body).toMatchObject({
      code: 'runtime_validation_failed',
      category: 'validation',
      retryable: false
    })
    expect(body.message).toContain('Pi harness requires model.provider_id and model.id')
  })

  it('routes session entry runs with runtime_engine pi through the configured Pi harness runner', async () => {
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore() })

    const profileResponse = await app.request('/v1/agent-profiles', jsonRequest(envelope({
      name: 'Pi Agent',
      profile_kind: 'assistant',
      context_tags: [],
      model: { provider_model_key: 'test-model' },
      inference: { max_tokens: 100 },
      prompt: { system: 'test' },
      status: 'draft'
    })))
    const profileBody = await profileResponse.json()
    const profile = profileBody.data
    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest(envelope({
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: `${profile.id}:draft`,
      profile_selection: {
        agent_profile_id: profile.id,
        agent_profile_version_id: 'draft',
        first_session_timestamp: '2026-06-29T00:00:00.000Z'
      }
    })))
    const sessionBody = await sessionResponse.json()
    const session = sessionBody.data

    const response = await app.request(`/v1/sessions/${session.id}/entries`, jsonRequest(envelope({
      runtime_engine: 'pi',
      content: 'hello through pi',
      client_entry_id: 'server-pi-entry-1'
    })))
    const body = await response.json()

    expect(response.status).toBe(200)
    expect(body.data.run).toMatchObject({
      status: 'failed',
      error_code: 'runtime_validation_failed',
      error_msg: expect.stringContaining('Pi harness requires model.provider_id and model.id'),
      request: expect.objectContaining({ request_id: 'req-1' }),
      result: expect.objectContaining({
        runtime_engine: 'pi',
        runtime_events: expect.arrayContaining([
          expect.objectContaining({ type: 'session.step.failed' }),
          expect.objectContaining({ type: 'session.error', code: 'runtime_validation_failed', category: 'validation' })
        ])
      })
    })
    expect(body.data.assistant_entry).toMatchObject({
      entry_type: 'error',
      status: 'failed',
      output: expect.objectContaining({ runtime_engine: 'pi' })
    })
  })

  it('routes session entry runs through Pi by default when runtime_engine is omitted', async () => {
    const app = createApp({ internalToken: 'secret', store: createMemoryRuntimeStore() })

    const profileResponse = await app.request('/v1/agent-profiles', jsonRequest(envelope({
      name: 'Default Pi Agent',
      profile_kind: 'assistant',
      context_tags: [],
      model: { provider_model_key: 'test-model' },
      inference: { max_tokens: 100 },
      prompt: { system: 'test' },
      status: 'draft'
    })))
    const profileBody = await profileResponse.json()
    const profile = profileBody.data
    const sessionResponse = await app.request('/v1/sessions/current', jsonRequest(envelope({
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: `${profile.id}:draft`,
      profile_selection: {
        agent_profile_id: profile.id,
        agent_profile_version_id: 'draft',
        first_session_timestamp: '2026-06-29T00:00:00.000Z'
      }
    })))
    const sessionBody = await sessionResponse.json()
    const session = sessionBody.data

    const response = await app.request(`/v1/sessions/${session.id}/entries`, jsonRequest(envelope({
      content: 'hello default pi',
      client_entry_id: 'server-pi-default-entry-1'
    })))
    const body = await response.json()

    expect(response.status).toBe(200)
    expect(body.data.run).toMatchObject({
      status: 'failed',
      error_code: 'runtime_validation_failed',
      error_msg: expect.stringContaining('Pi harness requires model.provider_id and model.id'),
      result: expect.objectContaining({
        runtime_engine: 'pi',
        runtime_events: expect.arrayContaining([
          expect.objectContaining({ type: 'session.step.failed' }),
          expect.objectContaining({ type: 'session.error', code: 'runtime_validation_failed', category: 'validation' })
        ])
      })
    })
    expect(body.data.assistant_entry).toMatchObject({
      entry_type: 'error',
      status: 'failed',
      output: expect.objectContaining({ runtime_engine: 'pi' })
    })
  })
})
