import type { SubagentMode } from './subagentMode.js'

export type ToolPermissionDecision = 'allow' | 'ask' | 'deny'

export interface ToolPermissionEvaluation {
  decision: ToolPermissionDecision
  matched_rule: string
  scope: string
  permission_key: string
  operation_type: string
  reason: string
}

export function evaluateToolPermission(input: {
  policy?: Record<string, unknown>
  toolName: string
  mcpServerKey?: string
  capability?: string
  operationType?: string
  requiresConfirmation?: boolean
  actorRole?: string
  args?: Record<string, unknown>
  runMode?: SubagentMode
}): ToolPermissionEvaluation {
  const policy = asRecord(input.policy)
  const operationType = firstString(input.operationType, 'unknown').toLowerCase()
  const capability = firstString(input.capability, input.mcpServerKey ? 'tools' : 'tool').toLowerCase()
  const mcpServerKey = firstString(input.mcpServerKey).toLowerCase()
  const permissionKey = mcpServerKey
    ? `mcp:${mcpServerKey}:${capability}:${input.toolName}:${operationType}`
    : `tool:${input.toolName}:${operationType}`
  if (input.runMode === 'read_only' && operationType !== 'read') {
    return {
      decision: 'deny',
      matched_rule: 'subagent.read_only',
      scope: 'run',
      permission_key: permissionKey,
      operation_type: operationType,
      reason: `deny by subagent.read_only: operation ${operationType} is not read-only`
    }
  }
  const rules = [
    ...toolPermissionRules(policy, 'profile'),
    ...(Array.isArray(policy.rules) ? policy.rules.map(asRecord) : [])
  ]
  const matchedRules: Array<{ rule: Record<string, unknown>, index: number, decision: ToolPermissionDecision }> = []
  for (const [index, rule] of rules.entries()) {
    if (!ruleMatches(rule, {
      toolName: input.toolName,
      operationType,
      actorRole: input.actorRole,
      args: input.args,
      mcpServerKey,
      capability
    })) continue
    const decision = normalizeDecision(rule.decision)
    if (!decision) continue
    matchedRules.push({ rule, index, decision })
  }
  const selectedRule = matchedRules.filter((item) => item.decision === 'deny').at(-1) || matchedRules.at(-1)
  if (selectedRule) {
    const { rule, index, decision } = selectedRule
    const matchedRule = firstString(rule.id, rule.name, `rules[${index}]`)
    return {
      decision,
      matched_rule: matchedRule,
      scope: firstString(rule.scope, 'profile'),
      permission_key: firstString(rule.permission_key, permissionKey),
      operation_type: operationType,
      reason: firstString(rule.reason, `${decision} by ${matchedRule}`)
    }
  }
  const capabilityDecision = normalizeDecision(asRecord(policy.capabilities)[capability])
  if (capabilityDecision === 'deny') return result('deny', `profile.capabilities.${capability}`, 'profile', permissionKey, operationType)
  if (input.requiresConfirmation) return result('ask', 'tool.requires_confirmation', 'tool', permissionKey, operationType)
  if (policy.all_tools_require_confirmation === true) return result('ask', 'profile.all_tools_require_confirmation', 'profile', permissionKey, operationType)
  if (capabilityDecision) return result(capabilityDecision, `profile.capabilities.${capability}`, 'profile', permissionKey, operationType)
  const defaultDecision = normalizeDecision(policy.default_decision ?? policy.defaultDecision)
  if (defaultDecision) {
    return result(
      defaultDecision,
      firstString(policy.default_matched_rule, policy.defaultMatchedRule, 'profile.default_decision'),
      firstString(policy.default_scope, policy.defaultScope, 'profile'),
      permissionKey,
      operationType
    )
  }
  return result('ask', 'default.missing_tool_action', 'default', permissionKey, operationType)
}

export function toolPermissionRules(policy: Record<string, unknown>, scope: string) {
  const toolMap = asRecord(policy.tools)
  const inlineMap = Object.fromEntries(
    Object.entries(policy).filter(([key]) => ![
      'tools',
      'rules',
      'default_decision',
      'defaultDecision',
      'default_matched_rule',
      'defaultMatchedRule',
      'default_scope',
      'defaultScope',
      'capabilities',
      'all_tools_require_confirmation'
    ].includes(key))
  )
  return Object.entries({ ...inlineMap, ...toolMap }).flatMap(([toolName, value]) => {
    const record = typeof value === 'string' ? { decision: value } : asRecord(value)
    const decision = normalizeDecision(record.decision ?? record.policy ?? record.mode)
    if (!decision) return []
    const rule: Record<string, unknown> = {
      id: firstString(record.id, `${scope}.${toolName}.${decision}`),
      scope,
      tool_name: firstString(record.tool_name, record.name, toolName),
      decision,
      reason: firstString(record.reason, `${decision} by ${scope} tool permission`)
    }
    const operationType = firstString(record.operation_type, record.operationType, record.operation)
    if (operationType) rule.operation_type = operationType
    const permissionKey = firstString(record.permission_key, record.permissionKey)
    if (permissionKey) rule.permission_key = permissionKey
    return [rule]
  })
}

function ruleMatches(rule: Record<string, unknown>, input: {
  toolName: string
  operationType: string
  actorRole?: string
  args?: Record<string, unknown>
  mcpServerKey?: string
  capability?: string
}) {
  const { toolName, operationType, mcpServerKey = '', capability = 'tool' } = input
  const matchPatterns = stringList(rule.match ?? rule.matches)
  if (matchPatterns.length > 0 && !matchPatterns.some((pattern) => patternMatches(pattern, permissionCandidates({
    toolName,
    operationType,
    mcpServerKey,
    capability
  })))) return false
  const tools = stringList(rule.tool_names ?? rule.tools ?? rule.tool_name ?? rule.tool)
  if (tools.length > 0 && !tools.includes('*') && !tools.includes(toolName)) return false
  const servers = stringList(rule.mcp_server_keys ?? rule.mcp_servers ?? rule.mcp_server_key ?? rule.server_key).map((item) => item.toLowerCase())
  if (servers.length > 0 && !servers.includes('*') && !servers.includes(mcpServerKey)) return false
  const capabilities = stringList(rule.capabilities ?? rule.capability).map((item) => item.toLowerCase())
  if (capabilities.length > 0 && !capabilities.includes('*') && !capabilities.includes(capability)) return false
  const operations = stringList(rule.operations ?? rule.operation_types ?? rule.operation_type ?? rule.operation).map((item) => item.toLowerCase())
  if (operations.length > 0 && !operations.includes(operationType)) return false
  const roles = stringList(rule.roles ?? rule.actor_roles ?? rule.actor_role)
  if (roles.length > 0 && !roles.includes(firstString(input.actorRole))) return false
  const argumentEquals = asRecord(rule.argument_equals)
  const args = input.args || {}
  return Object.entries(argumentEquals).every(([key, value]) => JSON.stringify(args[key]) === JSON.stringify(value))
}

function permissionCandidates(input: { toolName: string, operationType: string, mcpServerKey?: string, capability?: string }) {
  const capability = firstString(input.capability, 'tool').toLowerCase()
  const server = firstString(input.mcpServerKey).toLowerCase()
  const candidates = [
    input.toolName,
    `tool.${input.toolName}`,
    `tool.${input.toolName}.${input.operationType}`,
    `tool:${input.toolName}:${input.operationType}`
  ]
  if (server) {
    candidates.unshift(
      `mcp.${server}.${capability}.${input.toolName}`,
      `mcp.${server}.${capability}.${input.toolName}.${input.operationType}`,
      `mcp:${server}:${capability}:${input.toolName}:${input.operationType}`
    )
  }
  return candidates
}

function patternMatches(pattern: string, candidates: string[]) {
  const normalizedPattern = pattern.trim().toLowerCase()
  if (!normalizedPattern) return false
  if (normalizedPattern === '*') return true
  const regex = new RegExp(`^${escapeRegExp(normalizedPattern).replace(/\*/g, '.*')}$`)
  return candidates.some((candidate) => regex.test(candidate.toLowerCase()))
}

function escapeRegExp(value: string) {
  return value.replace(/[.+?^${}()|[\]\\]/g, '\\$&')
}

function result(decision: ToolPermissionDecision, matchedRule: string, scope: string, permissionKey: string, operationType: string): ToolPermissionEvaluation {
  return {
    decision,
    matched_rule: matchedRule,
    scope,
    permission_key: permissionKey,
    operation_type: operationType,
    reason: `${decision} by ${matchedRule}`
  }
}

function normalizeDecision(value: unknown): ToolPermissionDecision | '' {
  const decision = String(value || '').trim().toLowerCase()
  if (decision === 'request') return 'ask'
  return decision === 'allow' || decision === 'ask' || decision === 'deny' ? decision : ''
}

function stringList(value: unknown) {
  if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean)
  const item = String(value || '').trim()
  return item ? [item] : []
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const item = String(value || '').trim()
    if (item) return item
  }
  return ''
}
