import { Type } from '@earendil-works/pi-ai'
import type { AgentTool, ExecutionEnv, FileError, ExecutionError, Result } from '@earendil-works/pi-agent-core'
import { filterToolsByMode, type SubagentMode } from '../services/subagentMode.js'
import { evaluateToolPermission } from '../services/toolPermissionPolicy.js'
import { PiToolApprovalRequiredError, type PiToolApprovalDecision } from './piResources.js'

export type ModeAwareAgentTool = AgentTool & { readonly operation_type: string }

export interface WorkspaceToolPermissionHooks {
  toolPolicy?: Record<string, unknown>
  actorRole?: string
  runMode?: SubagentMode
  recordEvent?: (event: Record<string, unknown>) => Promise<void> | void
  decideTool?: (input: {
    approvalID: string
    callID: string
    toolName: string
    args: Record<string, unknown>
    reason: string
    operationType: string
    permissionKey: string
  }) => Promise<PiToolApprovalDecision | undefined>
}

export function createWorkspaceTools(
  env: ExecutionEnv,
  runMode: SubagentMode = 'write',
  hooks: WorkspaceToolPermissionHooks = {}
): ModeAwareAgentTool[] {
  const tools: ModeAwareAgentTool[] = [
    {
      name: 'bash',
      operation_type: 'execute',
      label: 'Run shell command',
      description: 'Run a Bash command inside the isolated Agent workspace. Use this for repository inspection, builds, tests, Git, and command-line tools.',
      parameters: Type.Object({
        command: Type.String({ description: 'Bash command to execute' }),
        cwd: Type.Optional(Type.String({ description: 'Workspace-relative working directory' })),
        timeout_seconds: Type.Optional(Type.Integer({ minimum: 1, maximum: 3600 }))
      }),
      async execute(_toolCallID, params) {
        const input = asRecord(params)
        const result = await env.exec(requiredString(input.command, 'command'), {
          ...(optionalString(input.cwd) ? { cwd: optionalString(input.cwd) } : {}),
          timeout: optionalPositiveNumber(input.timeout_seconds)
        })
        const value = resultOrThrow(result)
        const output = [value.stdout, value.stderr].filter(Boolean).join(value.stdout && value.stderr ? '\n' : '')
        return {
          content: [{ type: 'text', text: output || `(command exited ${value.exitCode} with no output)` }],
          details: { exit_code: value.exitCode, stdout: value.stdout, stderr: value.stderr }
        }
      }
    },
    {
      name: 'read_file',
      operation_type: 'read',
      label: 'Read workspace file',
      description: 'Read a UTF-8 text file from the isolated Agent workspace.',
      parameters: Type.Object({
        path: Type.String(),
        max_lines: Type.Optional(Type.Integer({ minimum: 1, maximum: 100000 }))
      }),
      async execute(_toolCallID, params) {
        const input = asRecord(params)
        const maxLines = optionalPositiveNumber(input.max_lines)
        const value = maxLines
          ? resultOrThrow(await env.readTextLines(requiredString(input.path, 'path'), { maxLines }))
          : resultOrThrow(await env.readTextFile(requiredString(input.path, 'path')))
        const text = Array.isArray(value) ? value.join('\n') : value
        return { content: [{ type: 'text', text }], details: { path: input.path, max_lines: maxLines } }
      }
    },
    {
      name: 'write_file',
      operation_type: 'write',
      label: 'Write workspace file',
      description: 'Create or overwrite a UTF-8 text file in the isolated Agent workspace. Parent directories are created by the workspace backend.',
      parameters: Type.Object({
        path: Type.String(),
        content: Type.String(),
        append: Type.Optional(Type.Boolean())
      }),
      async execute(_toolCallID, params) {
        const input = asRecord(params)
        const target = requiredString(input.path, 'path')
        const content = requiredString(input.content, 'content', true)
        resultOrThrow(input.append === true ? await env.appendFile(target, content) : await env.writeFile(target, content))
        return { content: [{ type: 'text', text: `Wrote ${Buffer.byteLength(content)} bytes to ${target}` }], details: { path: target, append: input.append === true } }
      }
    },
    {
      name: 'edit_file',
      operation_type: 'write',
      label: 'Edit workspace file',
      description: 'Replace exactly one matching text block in a UTF-8 workspace file. Fails if the old text is absent or occurs more than once.',
      parameters: Type.Object({
        path: Type.String(),
        old_text: Type.String(),
        new_text: Type.String()
      }),
      async execute(_toolCallID, params) {
        const input = asRecord(params)
        const target = requiredString(input.path, 'path')
        const oldText = requiredString(input.old_text, 'old_text')
        const newText = requiredString(input.new_text, 'new_text', true)
        const source = resultOrThrow(await env.readTextFile(target))
        const occurrences = source.split(oldText).length - 1
        if (occurrences !== 1) throw new Error(`edit_file expected exactly one match in ${target}, found ${occurrences}`)
        resultOrThrow(await env.writeFile(target, source.replace(oldText, newText)))
        return { content: [{ type: 'text', text: `Edited ${target}` }], details: { path: target } }
      }
    },
    {
      name: 'list_directory',
      operation_type: 'read',
      label: 'List workspace directory',
      description: 'List direct children of a directory in the isolated Agent workspace.',
      parameters: Type.Object({ path: Type.Optional(Type.String()) }),
      async execute(_toolCallID, params) {
        const target = optionalString(asRecord(params).path) || '.'
        const entries = resultOrThrow(await env.listDir(target))
        return { content: [{ type: 'text', text: JSON.stringify(entries, null, 2) }], details: { path: target, entries } }
      }
    },
    {
      name: 'file_info',
      operation_type: 'read',
      label: 'Inspect workspace path',
      description: 'Return metadata for a file, directory, or symlink in the isolated Agent workspace.',
      parameters: Type.Object({ path: Type.String() }),
      async execute(_toolCallID, params) {
        const target = requiredString(asRecord(params).path, 'path')
        const info = resultOrThrow(await env.fileInfo(target))
        return { content: [{ type: 'text', text: JSON.stringify(info, null, 2) }], details: info }
      }
    }
  ]
  const modeFiltered = filterToolsByMode(runMode, tools, (tool) => tool.operation_type)
  return modeFiltered.map((tool) => withWorkspaceToolPermission(tool, hooks))
}

/**
 * Keep tools listed in workspace_tools (when present) and drop explicit deny decisions.
 * request/allow tools remain available; deny is enforced again at execute time.
 */
export function filterWorkspaceToolsByPolicy<T extends { name?: string }>(tools: T[], toolPolicy: unknown): T[] {
  const allowed = new Set(workspaceToolNamesFromPolicy(toolPolicy))
  const decisions = workspaceToolDecisionsFromPolicy(toolPolicy)
  return tools.filter((tool) => {
    const name = String(tool.name || '').trim()
    if (!name) return false
    if (allowed.size > 0 && !allowed.has(name)) return false
    return decisionForWorkspaceTool(name, decisions, toolPolicy) !== 'deny'
  })
}

export function workspaceToolNamesFromPolicy(toolPolicy: unknown): string[] {
  const policy = toolPolicy && typeof toolPolicy === 'object' && !Array.isArray(toolPolicy)
    ? toolPolicy as Record<string, unknown>
    : {}
  const raw = policy.workspace_tools
  if (!Array.isArray(raw)) return []
  return raw
    .map((item) => String(item || '').trim())
    .filter(Boolean)
}

export function workspaceToolDecisionsFromPolicy(toolPolicy: unknown): Record<string, string> {
  const policy = asRecord(toolPolicy)
  const fromDecisions = asRecord(policy.workspace_tool_decisions)
  const fromTools = asRecord(policy.tools)
  const merged: Record<string, string> = {}
  for (const [name, value] of Object.entries({ ...fromTools, ...fromDecisions })) {
    const decision = normalizeWorkspaceDecision(value)
    if (decision) merged[name] = decision
  }
  return merged
}

function withWorkspaceToolPermission(tool: ModeAwareAgentTool, hooks: WorkspaceToolPermissionHooks): ModeAwareAgentTool {
  const originalExecute = tool.execute.bind(tool)
  return {
    ...tool,
    async execute(toolCallId, params, signal, onUpdate) {
      const args = asRecord(params)
      const permission = evaluateToolPermission({
        policy: effectiveWorkspaceToolPolicy(hooks.toolPolicy),
        toolName: tool.name,
        operationType: tool.operation_type,
        actorRole: hooks.actorRole,
        args,
        runMode: hooks.runMode
      })
      await hooks.recordEvent?.({
        type: 'action.permission_evaluated',
        call_id: toolCallId,
        tool_name: tool.name,
        decision: permission.decision,
        matched_rule: permission.matched_rule,
        scope: permission.scope,
        permission_key: permission.permission_key,
        operation_type: permission.operation_type,
        reason: permission.reason,
        executor_type: 'workspace',
        resource_type: 'workspace_tool',
        resource_id: tool.name,
        capability: 'tool'
      })
      if (permission.decision === 'deny') {
        await hooks.recordEvent?.({
          type: 'permission.resolved',
          request_id: `approval:${toolCallId}`,
          approval_id: `approval:${toolCallId}`,
          call_id: toolCallId,
          tool_name: tool.name,
          decision: 'deny',
          result: 'rejected',
          reason: permission.reason,
          executor_type: 'workspace',
          resource_type: 'workspace_tool',
          resource_id: tool.name
        })
        throw new Error(permission.reason)
      }
      if (permission.decision === 'ask') {
        const approvalID = `approval:${toolCallId}`
        const reason = firstString(
          asRecord(args).risk_summary,
          asRecord(args).reason,
          asRecord(args).invocation_reason,
          permission.reason,
          tool.description,
          `Tool ${tool.name} requires approval`
        )
        const decision = await hooks.decideTool?.({
          approvalID,
          callID: toolCallId,
          toolName: tool.name,
          args,
          reason,
          operationType: permission.operation_type,
          permissionKey: permission.permission_key
        })
        if (!(decision?.approved && decision.source === 'session_grant')) {
          await hooks.recordEvent?.({
            type: 'permission.asked',
            request_id: approvalID,
            approval_id: approvalID,
            call_id: toolCallId,
            tool_name: tool.name,
            reason,
            input: args,
            message: reason,
            tool_description: tool.description,
            operation_type: permission.operation_type,
            permission_key: permission.permission_key,
            matched_rule: permission.matched_rule,
            scope: permission.scope,
            executor_type: 'workspace',
            resource_type: 'workspace_tool',
            resource_id: tool.name,
            capability: 'tool'
          })
          if (!decision) {
            throw new PiToolApprovalRequiredError(approvalID, toolCallId, tool.name, reason)
          }
          if (!decision.approved) {
            await hooks.recordEvent?.({
              type: 'permission.resolved',
              request_id: approvalID,
              approval_id: approvalID,
              call_id: toolCallId,
              tool_name: tool.name,
              decision: 'reject',
              result: 'rejected',
              reason: decision.reason || 'Tool execution requires approval',
              executor_type: 'workspace',
              resource_type: 'workspace_tool',
              resource_id: tool.name
            })
            throw new Error(decision.reason || 'Tool execution requires approval')
          }
          await hooks.recordEvent?.({
            type: 'permission.resolved',
            request_id: approvalID,
            approval_id: approvalID,
            call_id: toolCallId,
            tool_name: tool.name,
            decision: 'approve_once',
            result: 'approved',
            reason: decision.reason || '',
            executor_type: 'workspace',
            resource_type: 'workspace_tool',
            resource_id: tool.name
          })
        }
      }
      return originalExecute(toolCallId, params, signal, onUpdate)
    }
  }
}

function effectiveWorkspaceToolPolicy(toolPolicy: unknown): Record<string, unknown> {
  const policy = asRecord(toolPolicy)
  const decisions = workspaceToolDecisionsFromPolicy(policy)
  const tools: Record<string, string> = {
    ...asStringDecisionMap(asRecord(policy.tools)),
    ...decisions
  }
  return {
    ...policy,
    tools,
    // Workspace tools without an explicit decision fall back to profile default, then request.
    default_decision: firstString(policy.default_decision, policy.defaultDecision, 'request'),
    default_matched_rule: firstString(policy.default_matched_rule, policy.defaultMatchedRule, 'workspace.default_request'),
    default_scope: firstString(policy.default_scope, policy.defaultScope, 'workspace')
  }
}

function decisionForWorkspaceTool(toolName: string, decisions: Record<string, string>, toolPolicy: unknown): string {
  const explicit = normalizeWorkspaceDecision(decisions[toolName])
  if (explicit) return explicit
  const policy = asRecord(toolPolicy)
  return normalizeWorkspaceDecision(policy.default_decision ?? policy.defaultDecision)
}

function asStringDecisionMap(value: Record<string, unknown>): Record<string, string> {
  const result: Record<string, string> = {}
  for (const [name, raw] of Object.entries(value)) {
    const decision = normalizeWorkspaceDecision(raw)
    if (decision) result[name] = decision
  }
  return result
}

function normalizeWorkspaceDecision(value: unknown): string {
  if (typeof value === 'string') {
    const decision = value.trim().toLowerCase()
    if (decision === 'request') return 'ask'
    return decision === 'allow' || decision === 'ask' || decision === 'deny' ? decision : ''
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return ''
  const record = value as Record<string, unknown>
  const nested = record.decision ?? record.policy ?? record.mode
  if (nested === value) return ''
  return normalizeWorkspaceDecision(nested)
}

function resultOrThrow<T>(result: Result<T, FileError | ExecutionError>) {
  if (!result.ok) throw result.error
  return result.value
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function requiredString(value: unknown, name: string, allowEmpty = false) {
  if (typeof value !== 'string' || (!allowEmpty && !value)) throw new Error(`${name} is required`)
  return value
}

function optionalString(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : ''
}

function optionalPositiveNumber(value: unknown) {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? number : undefined
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const item = String(value || '').trim()
    if (item) return item
  }
  return ''
}
