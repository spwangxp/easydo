import { Type } from '@earendil-works/pi-ai'
import { createHash } from 'node:crypto'
import type { AgentTool, Skill } from '@earendil-works/pi-agent-core/node'
import type { TSchema } from 'typebox'
import type { AgentProfileSnapshot, AgentResource, AgentResourceRef } from '../domain/runtime.js'
import { evaluateToolPermission } from '../services/toolPermissionPolicy.js'
import { filterToolsByMode, normalizeSubagentMode, operationTypeFromToolMeta, type SubagentMode } from '../services/subagentMode.js'

export interface PiHarnessResourceBuildInput {
  profile: AgentProfileSnapshot
  actorRole?: string
  resources: AgentResource[]
  subagents?: Array<{ id: number; name: string; description: string; profile_kind: string; context_tags: string[] }>
  loadedSkillNames?: Iterable<string | number>
  executeTool?: (input: { callID: string; toolName: string; args: Record<string, unknown>; resource: AgentResource; mcpServerKey?: string }) => Promise<PiToolExecutionResult>
  executeSubagent?: (input: { callID: string; subagent: NonNullable<PiHarnessResourceBuildInput['subagents']>[number]; task: string; args: Record<string, unknown> }) => Promise<PiSubagentExecutionResult>
  decideTool?: (input: { approvalID: string; callID: string; toolName: string; args: Record<string, unknown>; resource: AgentResource; reason: string; operationType: string; permissionKey: string; mcpServerKey?: string }) => Promise<PiToolApprovalDecision | undefined>
  loadSkill?: (input: { resource: AgentResource }) => Promise<{ content: string; digest?: string }>
  recordEvent?: (event: Record<string, unknown>) => Promise<void> | void
  runMode?: SubagentMode
}

export interface PiToolExecutionResult {
  content: string
  structured_content?: Record<string, unknown>
  metadata?: Record<string, unknown>
}

export interface PiToolApprovalDecision {
  approved: boolean
  reason?: string
  source?: 'session_grant' | 'policy'
}

export interface PiSubagentExecutionResult {
  task_id?: string
  status: string
  agent_profile_id?: number
  agent_profile_version_id?: number
  name?: string
  summary: string
  structured_output?: Record<string, unknown>
  validation_errors?: string[]
  error?: string
  child_run_link_id?: string
  child_session_id?: number
  child_runtime_run_id?: string
  artifact_refs?: unknown[]
}

export class PiToolApprovalRequiredError extends Error {
  constructor(
    readonly approvalID: string,
    readonly callID: string,
    readonly toolName: string,
    readonly reason: string
  ) {
    super(reason)
    this.name = 'PiToolApprovalRequiredError'
  }
}

export interface PiHarnessResourceBuildResult {
  skills: Skill[]
  tools: AgentTool[]
  summary: {
    skill_count: number
    mcp_tool_count: number
    subagent_count: number
    skills: Array<{ id: number; key: string; name: string; description: string; version: string; file_path: string }>
    mcp_servers: Array<{ id: number; key: string; name: string; description: string }>
    mcp_tools: Array<{ name: string; description: string }>
    subagents: Array<{ id: number; name: string; description: string; profile_kind: string; context_tags: string[] }>
  }
}

export function buildPiHarnessResources(input: PiHarnessResourceBuildInput): PiHarnessResourceBuildResult {
  const runMode = input.runMode ?? 'write'
  const skillResources = selectedSkillResources(input.profile, input.resources)
  const loadedSkillIDs = normalizedResourceIDSet(input.loadedSkillNames)
  const skills = skillResources.map((resource) => skillFromResource(resource, skillIsLoaded(resource, loadedSkillIDs)))
  const mcpResources = selectedMcpResources(input.profile, input.resources)
  const mcpTools = mcpResources.flatMap(({ resource, ref }) => mcpToolsFromResource(resource, ref, input))
  const subagents = input.subagents || []
  const subagentTools = normalizeSubagentMode(runMode) === 'write' && subagents.length > 0 ? [subagentSpawnTool(subagents, input)] : []
  const skillLoaderTools = skillResources.length > 0 ? [skillLoaderTool(skillResources, input)] : []
  return {
    skills,
    tools: [...mcpTools, ...subagentTools, ...skillLoaderTools],
    summary: {
      skill_count: skills.length,
      mcp_tool_count: mcpTools.length,
      subagent_count: subagents.length,
      skills: skillResources.map((resource, index) => ({
        id: resource.id,
        key: resource.resource_key,
        name: skills[index]?.name || resource.name,
        description: skills[index]?.description || resource.description,
        version: resource.version,
        file_path: skills[index]?.filePath || ''
      })),
      mcp_servers: mcpResources.map(({ resource }) => ({
        id: resource.id,
        key: resource.resource_key,
        name: resource.name,
        description: resource.description
      })),
      mcp_tools: mcpTools.map((tool) => ({
        name: tool.name,
        description: tool.description || ''
      })),
      subagents: subagents.map((subagent) => ({
        id: subagent.id,
        name: subagent.name,
        description: subagent.description,
        profile_kind: subagent.profile_kind,
        context_tags: subagent.context_tags
      }))
    }
  }
}

function skillLoaderTool(resources: AgentResource[], input: PiHarnessResourceBuildInput): AgentTool {
  return {
    name: 'easydo_skill_load',
    label: 'Load Skill instructions',
    description: 'Load the full instructions for one allowed Skill from the catalog when its description matches the current task.',
    parameters: Type.Object({
      skill_name: Type.String({ description: 'Allowed Skill name or key from the catalog' })
    }),
    async execute(toolCallId, params) {
      const requested = normalizeResourceID(asRecord(params).skill_name)
      const resource = resources.find((candidate) => [candidate.name, candidate.resource_key, candidate.resource_id, candidate.id]
        .some((value) => normalizeResourceID(value) === requested))
      if (!resource) {
        const message = `Skill ${asRecord(params).skill_name || ''} is not available to this Agent`
        await input.recordEvent?.({ type: 'skill.load_failed', call_id: toolCallId, message, source: 'dynamic_tool' })
        throw new Error(message)
      }
      await input.recordEvent?.({
        type: 'skill.load_started',
        call_id: toolCallId,
        name: resource.name,
        key: resource.resource_key,
        version: resource.version,
        source: 'dynamic_tool'
      })
      try {
        const loaded = input.loadSkill
          ? await input.loadSkill({ resource })
          : { content: skillInstructionContent(resource) }
        if (!loaded.content.trim()) throw new Error(`Skill ${resource.name} has no readable instructions`)
        const digest = loaded.digest || `sha256:${createHash('sha256').update(loaded.content).digest('hex')}`
        await input.recordEvent?.({
          type: 'skill.loaded',
          call_id: toolCallId,
          name: resource.name,
          key: resource.resource_key,
          version: resource.version,
          digest,
          source: 'dynamic_tool'
        })
        return {
          content: [{ type: 'text', text: loaded.content }],
          details: { skill_name: resource.name, resource_key: resource.resource_key, version: resource.version, digest }
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        await input.recordEvent?.({
          type: 'skill.load_failed',
          call_id: toolCallId,
          name: resource.name,
          key: resource.resource_key,
          version: resource.version,
          message,
          source: 'dynamic_tool'
        })
        throw error
      }
    }
  }
}

function skillInstructionContent(resource: AgentResource) {
  const spec = asRecord(resource.spec)
  return firstString(spec.instructions, spec.markdown, spec.content, spec.prompt)
}

function selectedSkillResources(profile: AgentProfileSnapshot, resources: AgentResource[]) {
  return (profile.skills || [])
    .map((ref) => resources.find((resource) => resource.resource_kind === 'skill' && refMatchesResource(ref, resource)))
    .filter((resource): resource is AgentResource => Boolean(resource))
}

function selectedMcpResources(profile: AgentProfileSnapshot, resources: AgentResource[]) {
  return (profile.mcp_servers || [])
    .map((ref) => {
      const resource = resources.find((item) => item.resource_kind === 'mcp_server' && refMatchesResource(ref, item))
      return resource ? { ref, resource } : null
    })
    .filter((item): item is { ref: AgentResourceRef, resource: AgentResource } => Boolean(item))
}

function skillFromResource(resource: AgentResource, includeContent: boolean): Skill {
  const spec = asRecord(resource.spec)
  return {
    name: resource.name || resource.resource_key,
    description: resource.description || '',
    content: includeContent ? firstString(spec.instructions, spec.markdown, spec.content, spec.prompt) : '',
    filePath: firstString(spec.file_path, spec.filePath, `/easydo/agent-resources/${resource.resource_key}/SKILL.md`)
  }
}

function mcpToolsFromResource(resource: AgentResource, ref: AgentResourceRef, input: PiHarnessResourceBuildInput): AgentTool[] {
  const definitions = filterToolsByMode(input.runMode ?? 'write', mcpRawTools(resource), (tool) => operationTypeFromToolMeta(asRecord(tool)))
  return definitions.map((tool) => agentToolFromMcpDefinition(resource, ref, tool, input)).filter((tool): tool is AgentTool => Boolean(tool))
}

function agentToolFromMcpDefinition(resource: AgentResource, ref: AgentResourceRef, value: unknown, input: PiHarnessResourceBuildInput): AgentTool | null {
  const record = asRecord(value)
  const name = firstString(record.name, record.tool_name, typeof value === 'string' ? value : '')
  if (!name) return null
  const description = firstString(record.description, `${resource.name} tool ${name}`)
  const mcpServerKey = firstString(record.mcp_server_key, record.mcp_server, record.server_key, resource.resource_key)
  return {
    name,
    label: description || name,
    description,
    parameters: jsonSchemaToPiParameters(record.input_schema || record.inputSchema || record.parameters),
    async execute(toolCallId, params) {
      if (!input.executeTool) throw new Error(`Pi MCP tool adapter is not wired yet: ${name}`)
      const args = asRecord(params)
      const assistantMessageID = `assistant:${toolCallId}`
      const permission = evaluateToolPermission({
        policy: effectiveMcpToolPolicy(input.profile, resource, ref),
        toolName: name,
        mcpServerKey,
        capability: 'tools',
        operationType: firstString(record.operation_type, record.operationType, record.operation),
        requiresConfirmation: record.requires_confirmation === true || record.requiresConfirmation === true,
        actorRole: input.actorRole,
        args,
        runMode: input.runMode
      })
      await input.recordEvent?.({
        type: 'action.permission_evaluated',
        call_id: toolCallId,
        tool_name: name,
        decision: permission.decision,
        matched_rule: permission.matched_rule,
        scope: permission.scope,
        permission_key: permission.permission_key,
        operation_type: permission.operation_type,
        reason: permission.reason,
        mcp_server_key: mcpServerKey
      })
      if (permission.decision === 'deny') {
        await input.recordEvent?.({
          type: 'permission.resolved',
          request_id: `approval:${toolCallId}`,
          approval_id: `approval:${toolCallId}`,
          call_id: toolCallId,
          tool_name: name,
          decision: 'deny',
          result: 'rejected',
          reason: permission.reason,
          mcp_server_key: mcpServerKey
        })
        throw new Error(permission.reason)
      }
      if (permission.decision === 'ask') {
        const approvalID = `approval:${toolCallId}`
        const reason = firstString(record.risk_summary, record.riskSummary, `Tool ${name} requires approval`)
        const decision = await input.decideTool?.({
          approvalID,
          callID: toolCallId,
          toolName: name,
          args,
          resource,
          reason,
          operationType: permission.operation_type,
          permissionKey: permission.permission_key,
          mcpServerKey
        })
        if (decision?.approved && decision.source === 'session_grant') {
          // The durable grant was already audited when it was created. A matching
          // later call executes without opening another approval checkpoint.
        } else {
        await input.recordEvent?.({
          type: 'permission.asked',
          request_id: approvalID,
          approval_id: approvalID,
          call_id: toolCallId,
          tool_name: name,
          reason,
          input: args,
          message: reason,
          operation_type: permission.operation_type,
          permission_key: permission.permission_key,
          matched_rule: permission.matched_rule,
          scope: permission.scope,
          executor_type: 'mcp',
          resource_type: resource.resource_kind,
          resource_id: resource.resource_key,
          mcp_server_key: mcpServerKey,
          capability: 'tools'
        })
        if (!decision) {
          throw new PiToolApprovalRequiredError(approvalID, toolCallId, name, reason)
        }
        if (!decision.approved) {
          await input.recordEvent?.({
            type: 'permission.resolved',
            request_id: approvalID,
            approval_id: approvalID,
            call_id: toolCallId,
            tool_name: name,
            decision: 'reject',
            result: 'rejected',
            reason: decision?.reason || 'Tool execution requires approval',
            mcp_server_key: mcpServerKey
          })
          throw new Error(decision.reason || 'Tool execution requires approval')
        }
        await input.recordEvent?.({
          type: 'permission.resolved',
          request_id: approvalID,
          approval_id: approvalID,
          call_id: toolCallId,
          tool_name: name,
          decision: 'approve_once',
          result: 'approved',
          reason: decision.reason || '',
          mcp_server_key: mcpServerKey
        })
        }
      }
      await input.recordEvent?.({
        type: 'session.tool.called',
        assistant_message_id: assistantMessageID,
        call_id: toolCallId,
        tool: name,
        tool_name: name,
        input: args,
        mcp_server_key: mcpServerKey
      })
      try {
        const result = await input.executeTool({ callID: toolCallId, toolName: name, args, resource, mcpServerKey })
        await input.recordEvent?.({
          type: 'session.tool.success',
          assistant_message_id: assistantMessageID,
          call_id: toolCallId,
          tool: name,
          tool_name: name,
          mcp_server_key: mcpServerKey,
          content: [{ type: 'text', text: result.content }],
          structured: result.structured_content || {},
          output: {
            content: result.content,
            structured_content: result.structured_content || {},
            metadata: result.metadata || {}
          }
        })
        return {
          content: [{ type: 'text', text: result.content }],
          details: {
            structured_content: result.structured_content || {},
            metadata: result.metadata || {}
          }
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        await input.recordEvent?.({
          type: 'session.tool.failed',
          assistant_message_id: assistantMessageID,
          call_id: toolCallId,
          tool: name,
          tool_name: name,
          mcp_server_key: mcpServerKey,
          error: { type: 'tool_error', message }
        })
        throw error
      }
    }
  }
}

function effectiveMcpToolPolicy(profile: AgentProfileSnapshot, resource: AgentResource, ref: AgentResourceRef) {
  const profilePolicy = asRecord(profile.tool_policy)
  const resourcePolicy = asRecord(asRecord(resource.spec).tool_permissions)
  const refPolicy = asRecord(asRecord(ref.config).tool_permissions)
  const rules = [
    ...toolPermissionRules(refPolicy, 'profile_ref').map((rule) => scopedMcpPermissionRule(rule, resource, ref, 'profile_ref')),
    ...toolPermissionRules(resourcePolicy, 'resource').map((rule) => scopedMcpPermissionRule(rule, resource, ref, 'resource')),
    ...(Array.isArray(profilePolicy.rules) ? profilePolicy.rules.map(asRecord) : [])
  ]
  return {
    ...profilePolicy,
    rules,
    default_decision: firstString(refPolicy.default_decision, refPolicy.defaultDecision, resourcePolicy.default_decision, resourcePolicy.defaultDecision, 'ask'),
    default_matched_rule: 'mcp.default_request',
    default_scope: 'mcp'
  }
}

function scopedMcpPermissionRule(rule: Record<string, unknown>, resource: AgentResource, ref: AgentResourceRef, scope: string) {
  const serverKeys = [
    resource.resource_key,
    resource.resource_id,
    resource.id,
    ref.resource_id
  ].map(normalizeResourceID).filter(Boolean)
  return {
    ...rule,
    scope: firstString(rule.scope, scope),
    capability: firstString(rule.capability, 'tools'),
    ...(serverKeys.length > 0 && !rule.match && !rule.matches && !rule.mcp_server_key && !rule.mcp_server_keys && !rule.mcp_servers
      ? { mcp_server_keys: [...new Set(serverKeys)] }
      : {})
  }
}

function toolPermissionRules(policy: Record<string, unknown>, scope: string) {
  const toolMap = asRecord(policy.tools)
  const inlineMap = Object.fromEntries(
    Object.entries(policy).filter(([key]) => !['tools', 'rules', 'default_decision', 'defaultDecision', 'default_matched_rule', 'defaultMatchedRule', 'default_scope', 'defaultScope'].includes(key))
  )
  const entries = Object.entries({ ...inlineMap, ...toolMap })
  const rules = entries.flatMap(([toolName, value]) => {
    const record = typeof value === 'string' ? { decision: value } : asRecord(value)
    const decision = normalizeToolPermissionDecision(record.decision ?? record.policy ?? record.mode)
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
  if (Array.isArray(policy.rules)) rules.push(...policy.rules.map(asRecord))
  return rules
}

function normalizeToolPermissionDecision(value: unknown) {
  const decision = firstString(value).toLowerCase()
  if (decision === 'request') return 'ask'
  return decision === 'allow' || decision === 'ask' || decision === 'deny' ? decision : ''
}

function jsonSchemaToPiParameters(value: unknown): TSchema {
  const schema = asRecord(value)
  if (Object.keys(schema).length === 0) return Type.Any()
  return normalizeJSONSchema(schema) as TSchema
}

function normalizeJSONSchema(schema: Record<string, unknown>): TSchema {
  const normalized: Record<string, unknown> = {}
  const type = firstString(schema.type)
  if (type) normalized.type = type
  if (Array.isArray(schema.enum)) normalized.enum = [...schema.enum]
  if (firstString(schema.description)) normalized.description = firstString(schema.description)
  for (const key of ['minimum', 'maximum', 'minLength', 'maxLength', 'pattern', 'format', 'default']) {
    if (schema[key] !== undefined) normalized[key] = schema[key]
  }
  const required = Array.isArray(schema.required) ? schema.required.map((item) => firstString(item)).filter(Boolean) : []
  if (required.length > 0) normalized.required = required
  const properties = asRecord(schema.properties)
  if (Object.keys(properties).length > 0) {
    normalized.type = firstString(normalized.type, 'object')
    normalized.properties = Object.fromEntries(
      Object.entries(properties).map(([key, property]) => [key, normalizeJSONSchema(asRecord(property))])
    )
  }
  const items = asRecord(schema.items)
  if (Object.keys(items).length > 0) normalized.items = normalizeJSONSchema(items)
  return Object.keys(normalized).length > 0 ? normalized as TSchema : Type.Any()
}

function subagentSpawnTool(subagents: NonNullable<PiHarnessResourceBuildInput['subagents']>, input: PiHarnessResourceBuildInput): AgentTool {
  return {
    name: 'easydo_subagent_spawn',
    label: 'Start subagent',
    description: `Start one of the configured subagents: ${subagents.map((subagent) => subagent.name).join(', ')}`,
    parameters: Type.Object({
      subagent_id: Type.Optional(Type.Number()),
      task: Type.String()
    }),
    async execute(toolCallId, params) {
      const args = asRecord(params)
      const requestedID = Number(args.subagent_id || 0)
      const selected = subagents.find((subagent) => subagent.id === requestedID) || subagents[0]
      const task = firstString(args.task)
      const assistantMessageID = `assistant:${toolCallId}`
      await input.recordEvent?.({
        type: 'session.tool.called',
        assistant_message_id: assistantMessageID,
        call_id: toolCallId,
        tool: 'subagent.spawn',
        tool_name: 'subagent.spawn',
        input: { subagent_id: selected.id, task }
      })
      if (!input.executeSubagent) throw new Error('Pi subagent adapter is not wired yet: subagent.spawn')
      const result = await input.executeSubagent({ callID: toolCallId, subagent: selected, task, args })
      const content = result.summary || `Subagent ${selected.name} ${result.status}`
      const structured = {
        subagent_id: selected.id,
        subagent_name: selected.name,
        profile_kind: selected.profile_kind,
        context_tags: selected.context_tags,
        task,
        task_id: result.task_id,
        status: result.status,
        agent_profile_id: result.agent_profile_id,
        agent_profile_version_id: result.agent_profile_version_id,
        name: result.name,
        summary: result.summary,
        structured_output: result.structured_output || {},
        validation_errors: result.validation_errors || [],
        error: result.error,
        child_run_link_id: result.child_run_link_id,
        child_session_id: result.child_session_id,
        child_runtime_run_id: result.child_runtime_run_id,
        artifact_refs: result.artifact_refs || []
      }
      await input.recordEvent?.({
        type: 'session.tool.success',
        assistant_message_id: assistantMessageID,
        call_id: toolCallId,
        tool: 'subagent.spawn',
        tool_name: 'subagent.spawn',
        content: [{ type: 'text', text: content }],
        structured,
        output: { content, structured_content: structured, metadata: {} }
      })
      return {
        content: [{ type: 'text', text: content }],
        details: structured
      }
    }
  }
}

function mcpRawTools(resource: AgentResource) {
  const spec = asRecord(resource.spec)
  return [
    ...(Array.isArray(spec.discovered_tools) ? spec.discovered_tools : []),
    ...(Array.isArray(spec.tools) ? spec.tools : [])
  ]
}

function refMatchesResource(ref: { resource_id?: string | number }, resource: AgentResource) {
  if (typeof ref.resource_id === 'number') return resource.id === ref.resource_id
  const expected = normalizeResourceID(ref.resource_id)
  return Boolean(expected) && (
    expected === normalizeResourceID(resource.resource_key) ||
    expected === normalizeResourceID(resource.resource_id)
  )
}

function normalizeResourceID(value: unknown) {
  return value === undefined || value === null ? '' : String(value).trim().toLowerCase()
}

function normalizedResourceIDSet(values: Iterable<unknown> | undefined) {
  const set = new Set<string>()
  for (const value of values ?? []) {
    const normalized = normalizeResourceID(value)
    if (normalized) set.add(normalized)
  }
  return set
}

function skillIsLoaded(resource: AgentResource, loadedSkillIDs: Set<string>) {
  return loadedSkillIDs.has(normalizeResourceID(resource.name)) ||
    loadedSkillIDs.has(normalizeResourceID(resource.resource_key)) ||
    loadedSkillIDs.has(normalizeResourceID(resource.resource_id)) ||
    loadedSkillIDs.has(normalizeResourceID(resource.id))
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const stringValue = value === undefined || value === null ? '' : String(value).trim()
    if (stringValue) return stringValue
  }
  return ''
}
