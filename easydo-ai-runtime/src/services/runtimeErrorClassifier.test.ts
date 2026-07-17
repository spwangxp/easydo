import { describe, expect, it } from 'vitest'
import { classifyRuntimeError, RuntimeSourceError } from './runtimeErrorClassifier.js'
import { RuntimeDomainError } from './runtimeErrors.js'

describe('classifyRuntimeError', () => {
  it.each([
    {
      error: Object.assign(new Error('Request timed out.'), { code: 'ETIMEDOUT' }),
      context: { source: 'provider' as const },
      expected: { code: 'provider_timeout', category: 'provider_timeout', retryable: true, terminal_status: 'timeout', http_status: 504 }
    },
    {
      error: Object.assign(new Error('429 rate limit exceeded'), { status: 429 }),
      context: { source: 'provider' as const },
      expected: { code: 'provider_rate_limit', category: 'provider_rate_limit', retryable: true, terminal_status: 'failed', http_status: 429 }
    },
    {
      error: Object.assign(new Error('ECONNRESET upstream connection reset'), { code: 'ECONNRESET' }),
      context: { source: 'provider' as const },
      expected: { code: 'provider_transport_error', category: 'transport', retryable: true, terminal_status: 'failed', http_status: 502 }
    },
    {
      error: Object.assign(new Error('MCP request timed out'), { code: 'mcp_timeout' }),
      context: { source: 'mcp' as const },
      expected: { code: 'mcp_timeout', category: 'mcp', retryable: true, terminal_status: 'failed', http_status: 504 }
    },
    {
      error: Object.assign(new Error('Tool approval denied'), { code: 'tool_permission_denied', status: 403 }),
      context: { source: 'permission' as const },
      expected: { code: 'tool_permission_denied', category: 'permission', retryable: false, terminal_status: 'failed', http_status: 403 }
    },
    {
      error: Object.assign(new Error('User cancelled the run'), { code: 'user_cancelled' }),
      context: { source: 'user' as const },
      expected: { code: 'user_cancelled', category: 'cancelled', retryable: false, terminal_status: 'cancelled', http_status: 409 }
    },
    {
      error: Object.assign(new Error('Agent profile is invalid'), { code: 'profile_validation_failed', status: 422 }),
      context: { source: 'validation' as const },
      expected: { code: 'profile_validation_failed', category: 'validation', retryable: false, terminal_status: 'failed', http_status: 422 }
    }
  ])('classifies $expected.category errors with stable behavior', ({ error, context, expected }) => {
    expect(classifyRuntimeError(error, context)).toMatchObject(expected)
  })

  it('sanitizes unknown internal failures and never exposes secret-bearing input', () => {
    const classified = classifyRuntimeError(
      new Error('provider failed api_key=sk-sensitive password=hunter2 https://example.test?token=secret'),
      { source: 'runtime' }
    )

    expect(classified).toMatchObject({
      code: 'runtime_internal_error',
      category: 'internal',
      retryable: false,
      terminal_status: 'failed',
      http_status: 500,
      source: 'runtime',
      user_message: 'The AI runtime encountered an internal error.'
    })
    expect(classified.message).not.toContain('sk-sensitive')
    expect(classified.message).not.toContain('hunter2')
    expect(classified.message).not.toContain('secret')
  })

  it('keeps an explicit stable code while applying canonical retry semantics', () => {
    expect(classifyRuntimeError(
      Object.assign(new Error('503 service unavailable'), { code: 'openrouter_unavailable', status: 503 }),
      { source: 'provider' }
    )).toMatchObject({
      code: 'openrouter_unavailable',
      category: 'transport',
      retryable: true,
      http_status: 503
    })
  })

  it('normalizes typed source errors without losing their source contract', () => {
    const error = new RuntimeSourceError({
      source: 'mcp',
      code: 'mcp_transport_error',
      message: 'MCP connection reset',
      http_status: 502,
      retryable: true
    })

    expect(classifyRuntimeError(error)).toMatchObject({
      source: 'mcp',
      code: 'mcp_transport_error',
      category: 'mcp',
      retryable: true,
      http_status: 502
    })
  })

  it.each([
    {
      name: 'HTTP 429',
      error: new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_rate_limit',
        message: 'MCP request failed with HTTP 429',
        http_status: 429,
        retryable: true
      }),
      retryable: true
    },
    {
      name: 'JSON-RPC application error carried over HTTP 502 semantics',
      error: new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_rpc_error',
        message: 'Unknown MCP tool',
        http_status: 502,
        retryable: false
      }),
      retryable: false
    }
  ])('preserves the typed MCP retry contract for $name', ({ error, retryable }) => {
    expect(classifyRuntimeError(error)).toMatchObject({
      source: 'mcp',
      code: error.code,
      category: 'mcp',
      http_status: error.http_status,
      retryable
    })
  })

  it('redacts secret-bearing JSON, headers, and Bearer credentials', () => {
    const classified = classifyRuntimeError(new Error(
      'request failed body={"api_key":"json-api-secret","password":"json-password-secret"} '
      + 'headers={"Authorization":"Bearer header-bearer-secret","X-Api-Key":"header-api-secret"} '
      + 'Authorization: Bearer direct-bearer-secret'
    ))

    expect(classified.message).not.toContain('json-api-secret')
    expect(classified.message).not.toContain('json-password-secret')
    expect(classified.message).not.toContain('header-bearer-secret')
    expect(classified.message).not.toContain('header-api-secret')
    expect(classified.message).not.toContain('direct-bearer-secret')
  })

  it('redacts provider credential aliases accepted by Runtime configuration', () => {
    const classified = classifyRuntimeError(new Error(
      'access_token=access-secret bearer_token=bearer-secret refresh_token=refresh-secret '
      + 'client_secret=client-secret body={"access_token":"json-access-secret","client_secret":"json-client-secret"} '
      + 'https://provider.test?refresh_token=query-refresh-secret'
    ))

    for (const secret of [
      'access-secret',
      'bearer-secret',
      'refresh-secret',
      'client-secret',
      'json-access-secret',
      'json-client-secret',
      'query-refresh-secret'
    ]) {
      expect(classified.message).not.toContain(secret)
    }
  })

  it('preserves a typed non-provider rate-limit source and retry contract', () => {
    expect(classifyRuntimeError(new RuntimeSourceError({
      source: 'workspace',
      code: 'workspace_quota_exceeded',
      message: 'Workspace rate limit exceeded',
      http_status: 429,
      retryable: false
    }))).toMatchObject({
      source: 'workspace',
      code: 'workspace_quota_exceeded',
      category: 'transport',
      retryable: false,
      http_status: 429,
      terminal_status: 'failed'
    })
  })

  it('does not misattribute a generic Runtime timeout to the model provider', () => {
    expect(classifyRuntimeError(
      Object.assign(new Error('Runtime operation timed out'), { code: 'runtime_timeout' }),
      { source: 'runtime' }
    )).toMatchObject({
      source: 'runtime',
      code: 'runtime_timeout',
      category: 'transport',
      retryable: true,
      terminal_status: 'timeout'
    })
  })

  it('distinguishes a provider deadline AbortError from explicit user cancellation', () => {
    const providerAbort = Object.assign(new Error('Provider request aborted after its timeout deadline'), {
      name: 'AbortError',
      code: 'ABORT_ERR'
    })
    const userAbort = Object.assign(new Error('The operation was aborted'), {
      name: 'AbortError',
      code: 'ABORT_ERR'
    })

    expect(classifyRuntimeError(providerAbort, { source: 'provider' })).toMatchObject({
      source: 'provider',
      code: 'provider_timeout',
      category: 'provider_timeout',
      retryable: true,
      terminal_status: 'timeout'
    })
    expect(classifyRuntimeError(userAbort, { source: 'user' })).toMatchObject({
      source: 'user',
      category: 'cancelled',
      retryable: false,
      terminal_status: 'cancelled'
    })
  })

  it('classifies harness abort messages as cancelled instead of internal error', () => {
    expect(classifyRuntimeError(new Error('Pi harness stopped with aborted'))).toMatchObject({
      code: 'runtime_cancelled',
      category: 'cancelled',
      terminal_status: 'cancelled',
      retryable: false,
      http_status: 409
    })
    expect(classifyRuntimeError(Object.assign(new Error('stream closed'), { code: 'runtime_cancelled' }))).toMatchObject({
      code: 'runtime_cancelled',
      category: 'cancelled',
      terminal_status: 'cancelled'
    })
  })

  it('preserves Runtime domain permission source metadata', () => {
    const error = new RuntimeDomainError('agent_workspace_access_denied', 'Workspace access denied', 403)

    expect(classifyRuntimeError(error)).toMatchObject({
      source: 'permission',
      code: 'agent_workspace_access_denied',
      category: 'permission',
      retryable: false,
      http_status: 403
    })
  })
})
