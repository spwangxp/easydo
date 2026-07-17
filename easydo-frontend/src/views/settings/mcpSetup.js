export function normalizeOrigin(origin = '') {
  return String(origin || '').trim().replace(/\/+$/, '')
}

export function buildMcpUrl(origin = '') {
  const normalized = normalizeOrigin(origin)
  return normalized ? `${normalized}/mcp` : ''
}

export function buildMcpSseUrl(origin = '') {
  const normalized = normalizeOrigin(origin)
  return normalized ? `${normalized}/mcp/sse` : ''
}

export function buildMaskedToken(token = '') {
  return hasReadableMcpToken(token) ? '********' : ''
}

export function hasReadableMcpToken(token = '') {
  return String(token || '').trim().length > 0
}

export const MCP_TOKEN_ENV_NAME = 'EASYDO_MCP_TOKEN'

export function buildClaudeCodeMcpSnippet({ origin = '' } = {}) {
  return JSON.stringify({
    mcpServers: {
      easydo: {
        type: 'streamable_http',
        url: buildMcpUrl(origin),
        headers: {
          Authorization: `Bearer \${${MCP_TOKEN_ENV_NAME}}`
        }
      }
    }
  }, null, 2)
}

export function buildOpenCodeMcpSnippet({ origin = '' } = {}) {
  return JSON.stringify({
    mcp: {
      easydo: {
        enabled: true,
        type: 'remote',
        url: buildMcpUrl(origin),
        oauth: false,
        headers: {
          Authorization: `Bearer {env:${MCP_TOKEN_ENV_NAME}}`
        }
      }
    }
  }, null, 2)
}

export function buildCodexMcpSnippet({ origin = '' } = {}) {
  return [
    '[mcp_servers.easydo]',
    `url = "${buildMcpUrl(origin)}"`,
    `bearer_token_env_var = "${MCP_TOKEN_ENV_NAME}"`
  ].join('\n')
}

export function buildMcpEnvironmentSnippet({ origin = '', token = '', workspaceId = 0 } = {}) {
  return [
    `export EASYDO_MCP_URL="${buildMcpUrl(origin)}"`,
    `export EASYDO_MCP_SSE_URL="${buildMcpSseUrl(origin)}"`,
    `export ${MCP_TOKEN_ENV_NAME}="${String(token || '').trim()}"`,
    `export EASYDO_WORKSPACE_ID="${Number(workspaceId)}"`
  ].join('\n')
}

export function buildDisplayMcpEnvironmentSnippet({ origin = '', token = '', workspaceId = 0 } = {}) {
  return buildMcpEnvironmentSnippet({ origin, token: buildMaskedToken(token), workspaceId })
}

export async function copyMcpValue({ value, writeText, fallbackWriteText, onSuccess, onError } = {}) {
  const text = String(value || '')
  if (!text.trim()) {
    return false
  }

  try {
    await writeText(text)
    onSuccess?.('复制成功')
    return true
  } catch {
    if (fallbackWriteText) {
      try {
        await fallbackWriteText(text)
        onSuccess?.('复制成功')
        return true
      } catch {
      }
    }
    onError?.('复制失败')
    return false
  }
}
