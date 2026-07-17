import { describe, expect, it } from 'vitest'
import { createRuntimeLogger } from './runtimeLogger.js'

describe('runtimeLogger', () => {
  it('writes one JSON line with only the allowlisted correlation context', () => {
    const lines: string[] = []
    const logger = createRuntimeLogger({
      sink: (line) => lines.push(line),
      now: () => '2026-07-12T10:00:00.000Z'
    })

    logger.info({
      component: 'runtime-server',
      operation: 'request',
      outcome: 'completed',
      request_id: 'req-1',
      workspace_id: 11,
      session_id: 's_wb_000001',
      runtime_run_id: 'r_wb_000001_000001',
      parent_runtime_run_id: 'r_wb_000001_000000',
      provider_id: 'openrouter',
      mcp_server_id: 'filesystem',
      subagent_id: 'reviewer',
      code: 'ok',
      category: 'success',
      // Runtime callers must not be able to append arbitrary request payloads.
      payload: { content: 'must not appear' }
    })

    expect(lines).toHaveLength(1)
    expect(JSON.parse(lines[0])).toEqual({
      timestamp: '2026-07-12T10:00:00.000Z',
      level: 'info',
      component: 'runtime-server',
      operation: 'request',
      outcome: 'completed',
      request_id: 'req-1',
      workspace_id: 11,
      session_id: 's_wb_000001',
      runtime_run_id: 'r_wb_000001_000001',
      parent_runtime_run_id: 'r_wb_000001_000000',
      provider_id: 'openrouter',
      mcp_server_id: 'filesystem',
      subagent_id: 'reviewer',
      code: 'ok',
      category: 'success'
    })
  })

  it('never accepts arbitrary raw error objects or forbidden runtime payloads', () => {
    const lines: string[] = []
    const logger = createRuntimeLogger({
      sink: (line) => lines.push(line),
      now: () => '2026-07-12T10:00:00.000Z'
    })

    logger.error({
      component: 'model-provider',
      operation: 'complete',
      outcome: 'failed',
      request_id: 'req-secret',
      runtime_run_id: 'run-secret',
      error: {
        message: 'upstream failed with Authorization: Bearer live-bearer-token',
        authorization: 'Bearer live-authorization-token',
        nested: {
          token: 'live-token',
          accessToken: 'live-access-token',
          refresh_token: 'live-refresh-token',
          apiKey: 'live-api-key',
          x_api_key: 'live-x-api-key',
          key: 'live-key-alias',
          access_key: 'live-access-key',
          password: 'live-password',
          clientSecret: 'live-client-secret',
          provider_credentials: { api_key: 'live-provider-key' },
          credentialRef: { secret: 'live-credential-ref' },
          prompt: 'full user prompt',
          system_prompt: 'full system prompt',
          toolResult: { content: 'full tool result' },
          profile_snapshot: { prompt: { system: 'full profile snapshot' } },
          request: { content: 'full request prompt' },
          profile: { provider_credential_ref: { key: 'full profile credential' } }
        }
      }
    })

    const output = lines[0]
    const entry = JSON.parse(output)
    expect(entry).not.toHaveProperty('error')
    for (const secret of [
      'live-bearer-token',
      'live-authorization-token',
      'live-token',
      'live-access-token',
      'live-refresh-token',
      'live-api-key',
      'live-x-api-key',
      'live-key-alias',
      'live-access-key',
      'live-password',
      'live-client-secret',
      'live-provider-key',
      'live-credential-ref',
      'full user prompt',
      'full system prompt',
      'full tool result',
      'full profile snapshot',
      'full request prompt',
      'full profile credential'
    ]) {
      expect(output).not.toContain(secret)
    }
  })

  it('drops Error values including messages and stack traces', () => {
    const lines: string[] = []
    const logger = createRuntimeLogger({ sink: (line) => lines.push(line) })
    const error = new Error('Bearer live-error-token failed')
    error.stack = 'stack contains live-stack-secret'

    logger.warn({
      component: 'mcp',
      operation: 'tools/call',
      outcome: 'failed',
      error
    })

    const output = lines[0]
    expect(JSON.parse(output)).not.toHaveProperty('error')
    expect(output).not.toContain('live-error-token')
    expect(output).not.toContain('live-stack-secret')
  })
})
