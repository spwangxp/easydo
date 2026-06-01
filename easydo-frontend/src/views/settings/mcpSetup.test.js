import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildMcpUrl,
  buildMcpSseUrl,
  buildMaskedToken,
  hasReadableMcpToken,
  buildClaudeCodeMcpSnippet,
  buildOpenCodeMcpSnippet,
  buildCodexMcpSnippet,
  buildMcpEnvironmentSnippet,
  buildDisplayMcpEnvironmentSnippet,
  copyMcpValue
} from './mcpSetup.js'

test('buildMcpUrl trims origin and appends /mcp', () => {
  assert.equal(buildMcpUrl(' https://easydo.example.com/// '), 'https://easydo.example.com/mcp')
})

test('buildMcpSseUrl trims origin and appends /mcp/sse', () => {
  assert.equal(buildMcpSseUrl(' https://easydo.example.com/// '), 'https://easydo.example.com/mcp/sse')
})

test('buildMaskedToken hides readable tokens and keeps blank tokens empty', () => {
  assert.equal(buildMaskedToken('token-123'), '********')
  assert.equal(buildMaskedToken('   '), '')
})

test('hasReadableMcpToken returns false for empty token', () => {
  assert.equal(hasReadableMcpToken(''), false)
  assert.equal(hasReadableMcpToken('   '), false)
  assert.equal(hasReadableMcpToken('token-123'), true)
})

test('buildClaudeCodeMcpSnippet emits .mcp.json server config', () => {
  assert.equal(
    buildClaudeCodeMcpSnippet({ origin: 'https://easydo.example.com' }),
    '{\n  "mcpServers": {\n    "easydo": {\n      "type": "http",\n      "url": "https://easydo.example.com/mcp",\n      "headers": {\n        "Authorization": "Bearer ${EASYDO_MCP_TOKEN}"\n      }\n    }\n  }\n}'
  )
})

test('buildOpenCodeMcpSnippet emits remote server config with streamable HTTP endpoint', () => {
  assert.equal(
    buildOpenCodeMcpSnippet({ origin: 'https://easydo.example.com' }),
    '{\n  "mcp": {\n    "easydo": {\n      "enabled": true,\n      "type": "remote",\n      "url": "https://easydo.example.com/mcp",\n      "oauth": false,\n      "headers": {\n        "Authorization": "Bearer {env:EASYDO_MCP_TOKEN}"\n      }\n    }\n  }\n}'
  )
})

test('buildCodexMcpSnippet emits config.toml server table', () => {
  assert.equal(
    buildCodexMcpSnippet({ origin: 'https://easydo.example.com' }),
    '[mcp_servers.easydo]\nurl = "https://easydo.example.com/mcp"\nbearer_token_env_var = "EASYDO_MCP_TOKEN"'
  )
})

test('environment snippet copies real token and current workspace id', () => {
  assert.equal(
    buildMcpEnvironmentSnippet({
      origin: 'https://easydo.example.com',
      token: 'token-123',
      workspaceId: 23
    }),
    'export EASYDO_MCP_URL="https://easydo.example.com/mcp"\nexport EASYDO_MCP_SSE_URL="https://easydo.example.com/mcp/sse"\nexport EASYDO_MCP_TOKEN="token-123"\nexport EASYDO_WORKSPACE_ID="23"'
  )
})

test('display environment snippet does not expose readable token', () => {
  const envSnippet = buildDisplayMcpEnvironmentSnippet({
    origin: 'https://easydo.example.com',
    token: 'token-123',
    workspaceId: 23
  })

  assert.equal(envSnippet.includes('token-123'), false)
  assert.match(envSnippet, /EASYDO_MCP_TOKEN="\*\*\*\*\*\*\*\*"/)
})

test('client config snippets do not expose readable tokens', () => {
  const snippets = [
    buildClaudeCodeMcpSnippet({ origin: 'https://easydo.example.com' }),
    buildOpenCodeMcpSnippet({ origin: 'https://easydo.example.com' }),
    buildCodexMcpSnippet({ origin: 'https://easydo.example.com' })
  ]

  for (const snippet of snippets) {
    assert.equal(snippet.includes('token-123'), false)
    assert.equal(snippet.includes('********'), false)
  }
})

test('copyMcpValue writes full config snippet and reports success', async () => {
  const calls = []
  await copyMcpValue({
    value: '{"mcpServers":{}}',
    writeText: async (value) => calls.push(value),
    onSuccess: (message) => calls.push(message),
    onError: (message) => calls.push(`error:${message}`)
  })

  assert.deepEqual(calls, ['{"mcpServers":{}}', '复制成功'])
})

test('copyMcpValue falls back and reports success', async () => {
  const calls = []
  await copyMcpValue({
    value: 'token-123',
    writeText: async () => {
      throw new Error('clipboard unavailable')
    },
    fallbackWriteText: async (value) => calls.push(`fallback:${value}`),
    onSuccess: (message) => calls.push(message),
    onError: (message) => calls.push(`error:${message}`)
  })

  assert.deepEqual(calls, ['fallback:token-123', '复制成功'])
})

test('copyMcpValue reports failure for full environment snippet when both copy paths fail', async () => {
  const calls = []
  await copyMcpValue({
    value: 'export EASYDO_WORKSPACE_ID="23"',
    writeText: async () => {
      throw new Error('clipboard unavailable')
    },
    fallbackWriteText: async () => {
      throw new Error('fallback unavailable')
    },
    onSuccess: (message) => calls.push(message),
    onError: (message) => calls.push(message)
  })

  assert.deepEqual(calls, ['复制失败'])
})
