import { createHash } from 'node:crypto'
import type {
  AIRuntimeRun,
  AISession,
  AISessionEntry,
  ActionEvent,
  ActionDecision,
  ActionExecution,
  AgentAction,
  AgentProfileDraft,
  AgentProfileSnapshot,
  AgentResource,
  RuntimeArtifact,
  ChildRunLink,
  RuntimeAuth,
  RuntimeActor,
  SessionPermissionGrant,
  SessionModelOverride,
  SessionQueueItem,
  SessionQueueMode
} from '../domain/runtime.js'
import { agentProfileSnapshotHash, stableJSONStringify } from '../domain/runtime.js'
import type { QueueTransitionCommit, RuntimeStore, SessionQueueEventDraft } from '../store/memoryRuntimeStore.js'
import type { AgentEventStore } from '../agent-runtime/eventStore.js'
import {
  isTerminalRunStateEventType,
  projectRunState,
  runStateEventTypeForStatus,
  type AgentRuntimeEvent,
  type SessionQueueRuntimeEvent
} from '../agent-runtime/events.js'
import type { AgentHarnessRunner, AgentHarnessRunInput, AgentHarnessRunResult } from '../agent-runtime/harnessRunner.js'
import type { ExecutionEnv } from '@earendil-works/pi-agent-core'
import type { AIRuntimeWorkspace } from '../workspace/types.js'
import { modelSelectionFromRuntimeConfig } from '../agent-runtime/piHarnessFactory.js'
import { buildPiHarnessResources, PiToolApprovalRequiredError, type PiHarnessResourceBuildResult } from '../agent-runtime/piResources.js'
import { AgentProfileService } from './profileService.js'
import { OrchestrationService } from './orchestrationService.js'
import { evaluateToolPermission, toolPermissionRules, type ToolPermissionEvaluation } from './toolPermissionPolicy.js'
import {
  composeTaggedUserContent,
  ContextTagRegistry,
  normalizeContextTags,
  type ContextTagAssembly
} from './contextTags.js'
import { RuntimeDomainError } from './runtimeErrors.js'
import { classifyRuntimeError, runtimeErrorPublicPayload, type RuntimeErrorDescriptor } from './runtimeErrorClassifier.js'
import { createMcpClient, resolveMcpClientConfigSecrets, type McpClient } from './mcpStreamableHttpClient.js'
import {
  createDefaultChatModelClient,
  ModelProviderError,
  type ChatModelClient,
  type ChatModelRequest,
  type ChatModelStreamEvent,
  type ChatModelResult,
  type ChatModelToolCall,
  type ChatModelToolDefinition
} from './modelProvider.js'
import { readSkillRepositoryEntry } from './skillScanner.js'
import { approvalRequestSchema } from '../schemas/runtime.js'
import { runtimeLogger, type RuntimeLogger, type RuntimeLogInput } from '../observability/runtimeLogger.js'
import type { RuntimeMetrics } from '../observability/runtimeMetrics.js'
import { assertReadOnlyAllowsOperation, normalizeSubagentMode, resolveSubagentMode, type SubagentMode } from './subagentMode.js'
import { attachActiveDurationTimings } from './activeAgentDuration.js'
import { CAPACITY_BUDGET, paginateEventsByCursor, selectRetainedRunEvents } from './capacityBudget.js'

export interface RuntimeStreamEvent {
  event: string
  data: Record<string, unknown>
}

type UserActionDecision = 'approve_once' | 'approve_session' | 'reject' | 'steer'

function now() {
  return new Date().toISOString()
}

function elapsedMs(start: number) {
  return Math.max(0, Math.round(performance.now() - start))
}

const IDEMPOTENCY_KEY_MAX_LENGTH = 191
const BUSINESS_ID_SEQ_WIDTH = 6
const BUSINESS_ID_SEQ_LIMIT = 36 ** BUSINESS_ID_SEQ_WIDTH
const CLIENT_ID_PATTERN = /^[a-z0-9_.:-]{1,64}$/
const TOOL_RESULT_PREVIEW_MAX_BYTES = 4096
const APPROVAL_TTL_MS = 15 * 60 * 1000
const SESSION_PERMISSION_GRANT_TTL_MS = 8 * 60 * 60 * 1000
const SESSION_TITLE_MAX_CHARS = 48
const SUBAGENT_PROGRESS_MIN_INTERVAL_MS = 250
const SUBAGENT_SUMMARY_MAX_CHARS = 8_000
export const SESSION_QUEUE_ACTIVE_ITEM_LIMIT = 50
export const SESSION_QUEUE_STEER_TTL_MS = 15 * 60 * 1000
export const SESSION_QUEUE_CLAIM_TTL_MS = 30_000

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? { ...(value as Record<string, unknown>) } : {}
}

function asString(value: unknown) {
  return value === undefined || value === null ? '' : String(value)
}

function sessionQueueMode(value: unknown): SessionQueueMode {
  if (value === 'steer' || value === 'follow_up' || value === 'stop_and_run') return value
  throw new RuntimeDomainError('queue_mode_invalid', 'Queue mode must be steer, follow_up, or stop_and_run')
}

function sessionQueueAttachments(value: unknown) {
  if (value === undefined) return []
  if (!Array.isArray(value) || value.some((item) => !isRecord(item))) {
    throw new RuntimeDomainError('session_queue_attachments_invalid', 'Queue attachments must be an array of objects')
  }
  return value.map((item) => ({ ...item }))
}

function entryAttachmentsFromPayload(payload: Record<string, unknown>) {
  if (payload.attachments === undefined) return []
  return sessionQueueAttachments(payload.attachments)
}

function sessionQueueItemPayload(item: Pick<SessionQueueItem, 'mode' | 'content' | 'attachments' | 'context_ref' | 'target_run_id'>) {
  return {
    mode: item.mode,
    content: item.content,
    attachments: item.attachments,
    context_ref: item.context_ref,
    target_run_id: item.target_run_id || ''
  }
}

type SessionQueueEventAppendInput =
  | { type: 'session.queue.added', item: SessionQueueItem, marker: 'created' }
  | { type: 'session.queue.claimed', item: SessionQueueItem, marker: number }
  | { type: 'session.queue.cancelled', item: SessionQueueItem, marker: 'cancelled' }
  | { type: 'session.queue.expired', item: SessionQueueItem, marker: 'expired' }
  | { type: 'session.queue.failed', item: SessionQueueItem, marker: string }
  | { type: 'session.steer.applied', item: SessionQueueItem, marker: 'applied', runtime_run_id: string }
  | {
      type: 'session.follow_up.started'
      item: SessionQueueItem
      marker: string
      runtime_run_id: string
      consumed_runtime_run_id: string
    }

function stableEventID(...parts: unknown[]) {
  return parts
    .map((part) => encodeURIComponent(asString(part).trim() || 'event'))
    .join(':')
}

function boundedReadableIDSegment(value: unknown, fallback: string, maxLength = 32) {
  const normalized = asString(value)
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_.-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
  return (normalized || fallback).slice(0, maxLength)
}

function readableIDSegment(value: unknown, name: string, maxLength = 48) {
  const normalized = asString(value)
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_.-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
  if (!normalized) {
    throw new RuntimeDomainError(`${name}_invalid`, `${name} is invalid`)
  }
  if (normalized.length > maxLength) {
    throw new RuntimeDomainError(`${name}_too_long`, `${name} is too long`)
  }
  return normalized
}

function base36(value: unknown) {
  const number = Number(value)
  if (!Number.isSafeInteger(number) || number < 0) {
    throw new RuntimeDomainError('business_id_invalid_sequence', 'Business ID sequence must be a non-negative safe integer')
  }
  return number.toString(36)
}

function seq36(value: unknown) {
  const number = Number(value)
  if (!Number.isSafeInteger(number) || number < 0) {
    throw new RuntimeDomainError('business_id_invalid_sequence', 'Business ID sequence must be a non-negative safe integer')
  }
  if (number >= BUSINESS_ID_SEQ_LIMIT) {
    throw new RuntimeDomainError('id_sequence_exhausted', 'Business ID sequence exhausted')
  }
  return number.toString(36).padStart(BUSINESS_ID_SEQ_WIDTH, '0')
}

function sessionBusinessID(workspaceID: number, sessionID: number) {
  return `s_w${base36(workspaceID)}_${seq36(sessionID)}`
}

function fallbackSessionTitle(timestamp: string) {
  const date = new Date(timestamp)
  const safeDate = Number.isFinite(date.getTime()) ? date : new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  return `新会话 - ${pad(safeDate.getMonth() + 1)}-${pad(safeDate.getDate())} ${pad(safeDate.getHours())}:${pad(safeDate.getMinutes())}`
}

function sanitizeGeneratedSessionTitle(value: unknown) {
  let title = firstString(value)
  if (!title) return ''
  title = title
    .replace(/```[\s\S]*?```/g, '')
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find(Boolean) || ''
  title = title
    .replace(/^#+\s*/, '')
    .replace(/^(会话)?标题\s*[:：]\s*/i, '')
    .replace(/^[`"'“”‘’「」『』【】[\]()（）]+|[`"'“”‘’「」『』【】[\]()（）]+$/g, '')
    .replace(/\*\*/g, '')
    .replace(/https?:\/\/\S+/gi, '[链接]')
    .replace(/\b(sk|ak|token|key)-[a-z0-9._-]{12,}\b/gi, '[凭证]')
    .replace(/\s+/g, ' ')
    .replace(/[。！？!?；;，,、.]+$/g, '')
    .trim()
  const chars = Array.from(title)
  return chars.slice(0, SESSION_TITLE_MAX_CHARS).join('').trim()
}

function deterministicSessionTitleFromUserContent(userContent: string) {
  return sanitizeGeneratedSessionTitle(userContent)
    .replace(/^(帮我|请帮我|请|麻烦|麻烦你)\s*/i, '')
    .trim()
}

function sessionTitleRequestContent(userContent: string) {
  return [
    '请为下面用户第一条消息生成一个简洁的中文会话标题。',
    '要求：8 到 24 个汉字左右；不要 Markdown；不要引号；不要句号；不要暴露密钥、Token、完整 URL 或长 ID。',
    '',
    '用户消息：',
    userContent
  ].join('\n')
}

function runBusinessID(workspaceID: number, sessionID: number, runID: number) {
  return `r_w${base36(workspaceID)}_${seq36(sessionID)}_${seq36(runID)}`
}

function workspaceIDFromRuntimeRunID(runtimeRunID: string) {
  const match = runtimeRunID.match(/^r_w([0-9a-z]+)_/)
  if (!match) return 0
  const parsed = Number.parseInt(match[1], 36)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0
}

function workspaceIDFromArtifactID(artifactID: string) {
  const match = artifactID.match(/^art_w([0-9a-z]+)_/)
  if (!match) return 0
  const parsed = Number.parseInt(match[1], 36)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0
}

function entryRuntimeIdempotencyKey(sessionID: string, runtimeRunID: string, role: string, entrySeq: number) {
  return runtimeIdempotencyKey('entry', sessionID, 'runtime', runtimeRunID, role, seq36(entrySeq))
}

function requireClientID(field: 'client_entry_id' | 'client_decision_id' | 'client_item_id', value: unknown) {
  const raw = asString(value).trim()
  if (!raw) {
    throw new RuntimeDomainError(`${field}_required`, `${field} is required`)
  }
  if (!CLIENT_ID_PATTERN.test(raw)) {
    throw new RuntimeDomainError(`${field}_invalid`, `${field} must match [a-z0-9_.:-]{1,64}`)
  }
  return raw
}

function requireCapabilityID(value: unknown) {
  const raw = asString(value).trim()
  if (!CLIENT_ID_PATTERN.test(raw)) {
    throw new RuntimeDomainError('capability_id_invalid', 'Capability ID must match [a-z0-9_.:-]{1,64}')
  }
  return raw
}

const ACTIVE_RUN_STATUSES = new Set<AIRuntimeRun['status']>(['queued', 'running', 'awaiting_decision', 'awaiting_input'])
const TERMINAL_RUN_STATUSES = new Set<AIRuntimeRun['status']>(['completed', 'failed', 'cancelled', 'timeout', 'interrupted'])

function hasAwaitingDecisionAction(actions: Record<string, unknown>[]) {
  return actions.some((action) => asString(action.status) === 'awaiting_decision')
}

function isApprovingDecision(decision: UserActionDecision | ActionDecision['decision']) {
  return decision === 'approve_once' || decision === 'approve_session'
}

function parsePiApprovalDecision(payload: Record<string, unknown>): UserActionDecision {
  const raw = firstString(payload.decision, payload.action).toLowerCase()
  if (raw === 'approve_once' || raw === 'approve_session' || raw === 'reject' || raw === 'steer') return raw
  throw new RuntimeDomainError('pi_approval_decision_invalid', 'Pi approval decision must be approve_once, approve_session, reject, or steer')
}

function isTerminalRunStatus(status: AIRuntimeRun['status']) {
  return TERMINAL_RUN_STATUSES.has(status)
}

function subagentModeForRun(run: AIRuntimeRun): SubagentMode {
  return firstString(run.request.mode) ? normalizeSubagentMode(run.request.mode) : 'write'
}

function terminalRunEntryStatus(status: AIRuntimeRun['status']): AISessionEntry['status'] {
  if (status === 'failed' || status === 'timeout' || status === 'interrupted') return 'failed'
  if (status === 'cancelled') return 'cancelled'
  return 'completed'
}

function runStateEventPayload(runtimeRunID: string, status: AIRuntimeRun['status'], code = '', message = '') {
  return {
    runtime_run_id: runtimeRunID,
    status,
    code: firstString(code) || undefined,
    message: firstString(message) || undefined,
    run: { runtime_run_id: runtimeRunID, status }
  }
}

function runtimeIdempotencyKey(prefix: string, ...parts: unknown[]) {
  const key = [prefix, ...parts].map((part) => asString(part).trim()).join(':')
  if (!key || key.length > IDEMPOTENCY_KEY_MAX_LENGTH) {
    throw new RuntimeDomainError('idempotency_key_too_long', 'Idempotency key is empty or too long')
  }
  return key
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const stringValue = asString(value).trim()
    if (stringValue) return stringValue
  }
  return ''
}

function queuedRunStartOutcome(value: unknown): 'consumed' | 'already_consumed' | 'stale_claim' | 'active_run_conflict' | undefined {
  switch (firstString(value)) {
    case 'consumed':
      return 'consumed'
    case 'already_consumed':
      return 'already_consumed'
    case 'stale_claim':
      return 'stale_claim'
    case 'active_run_conflict':
      return 'active_run_conflict'
    default:
      return undefined
  }
}

function asStringOrNumber(value: unknown): string | number | undefined {
  if (typeof value === 'string' || typeof value === 'number') return value
  return undefined
}

function firstPositiveNumber(...values: unknown[]) {
  for (const value of values) {
    const numberValue = Number(value)
    if (Number.isFinite(numberValue) && numberValue > 0) return numberValue
  }
  return undefined
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function jsonObject(value: unknown): Record<string, unknown> {
  return isRecord(value) ? { ...value } : {}
}

function textFromRuntimeEvents(events: unknown[]) {
  const ended = [...events].reverse().map(asRecord).find((event) => event.type === 'session.text.ended')
  const endedText = firstString(ended?.text)
  if (endedText && !isApprovalWaitNarrationText(endedText)) return endedText
  const streamed = events
    .map(asRecord)
    .filter((event) => event.type === 'session.text.delta')
    .map((event) => firstString(event.delta))
    .join('')
  if (streamed && !isApprovalWaitNarrationText(streamed)) return streamed
  // Approval pauses often leave model meta-prose ("waiting for user approval").
  // Prefer empty content so the UI shows the permission panel instead of that text.
  return ''
}

function isApprovalWaitNarrationText(text: string) {
  const normalized = text.trim()
  if (!normalized) return false
  return /等待用户审批|需要用户审批|已触发审批|待用户批准|requires approval|waiting for (user )?approval|approval queue|我不能自己批准/i.test(normalized)
}

function piApprovalFromPermissionEvent(event: Record<string, unknown>): Record<string, unknown> | null {
  const approvalID = firstString(event.request_id, event.approval_id)
  const callID = firstString(event.call_id, event.tool_call_id, event.provider_tool_call_id)
  const toolName = firstString(event.tool_name, event.tool)
  if (!approvalID || !callID || !toolName) return null
  return {
    approval_id: approvalID,
    call_id: callID,
    tool_name: toolName,
    reason: firstString(event.reason, event.message),
    input: asRecord(event.input),
    input_preview: {
      tool_name: toolName,
      arguments: asRecord(event.input),
      ...(firstString(event.mcp_server_key) ? { mcp_server_key: firstString(event.mcp_server_key) } : {}),
      ...(firstString(event.capability) ? { capability: firstString(event.capability) } : {})
    },
    operation_type: firstString(event.operation_type),
    permission_key: firstString(event.permission_key),
    executor_type: firstString(event.executor_type),
    resource_type: firstString(event.resource_type),
    resource_id: firstString(event.resource_id),
    mcp_server_key: firstString(event.mcp_server_key),
    capability: firstString(event.capability)
  }
}

function pendingPiApprovalsFromRuntimeEvents(events: unknown[]) {
  const pendingByID = new Map<string, Record<string, unknown>>()
  let hasApprovalResolution = false
  for (const event of events.map(asRecord)) {
    if (event.type === 'permission.asked') {
      const approvalID = firstString(event.request_id, event.approval_id)
      const callID = firstString(event.call_id, event.tool_call_id, event.provider_tool_call_id)
      for (const id of [approvalID, callID].filter(Boolean)) pendingByID.set(id, event)
      continue
    }
    if (event.type === 'permission.resolved') {
      const approvalID = firstString(event.request_id, event.approval_id)
      const callID = firstString(event.call_id, event.tool_call_id, event.provider_tool_call_id)
      for (const id of [approvalID, callID].filter(Boolean)) pendingByID.delete(id)
      hasApprovalResolution = true
      continue
    }
    if (pendingByID.size > 0 && isPiApprovalBypassBoundaryEvent(event, hasApprovalResolution)) {
      pendingByID.clear()
    }
  }
  return [...new Set(pendingByID.values())].map(piApprovalFromPermissionEvent).filter((approval): approval is Record<string, unknown> => approval !== null)
}

function isApprovalRequiredToolFailureEvent(event: Record<string, unknown>) {
  if (event.type !== 'session.tool.failed') return false
  const error = asRecord(event.error)
  const message = firstString(event.message, error.message, typeof event.error === 'string' ? event.error : '')
  return /requires approval|需要.*审批|需要.*确认/i.test(message)
}

function isPiApprovalBypassBoundaryEvent(event: Record<string, unknown>, hasApprovalResolution: boolean) {
  if (['session.step.started', 'model.call_started', 'model.continuation_started', 'provider.call_started', 'provider.continuation_started'].includes(firstString(event.type))) return true
  if (!hasApprovalResolution) return false
  if (['session.tool.called', 'session.tool.success', 'tool.executed', 'action.execution_started', 'action.execution_succeeded'].includes(firstString(event.type))) return true
  if (['session.tool.failed', 'tool.failed', 'action.execution_failed'].includes(firstString(event.type))) {
    return !isApprovalRequiredToolFailureEvent(event)
  }
  return false
}

function hasPiApprovalRequestEvents(events: unknown[]) {
  return events.map(asRecord).some((event) => event.type === 'permission.asked')
}

function piApprovalResolutionResult(event: Record<string, unknown>) {
  const result = firstString(event.result, event.decision).toLowerCase()
  if (result === 'approved' || result === 'approve_once' || result === 'approve_session') return 'approved'
  if (result === 'rejected' || result === 'reject') return 'rejected'
  if (result === 'steer') return 'steer'
  return ''
}

function rejectedPiApprovalsFromRuntimeEvents(events: unknown[]) {
  const askedByID = new Map<string, Record<string, unknown>>()
  const rejectedIDs = new Set<string>()
  for (const event of events.map(asRecord)) {
    if (event.type === 'permission.asked') {
      const approvalID = firstString(event.request_id, event.approval_id)
      if (approvalID) askedByID.set(approvalID, event)
      continue
    }
    if (event.type === 'permission.resolved' && piApprovalResolutionResult(event) === 'rejected') {
      const approvalID = firstString(event.request_id, event.approval_id)
      if (approvalID) rejectedIDs.add(approvalID)
    }
  }
  return [...rejectedIDs]
    .map((approvalID) => piApprovalFromPermissionEvent(askedByID.get(approvalID) || { approval_id: approvalID }))
    .filter((approval): approval is Record<string, unknown> => approval !== null)
}

function approvedPiApprovalsReadyForExecution(events: unknown[]) {
  const askedByID = new Map<string, Record<string, unknown>>()
  const approvedIDs = new Set<string>()
  const executedCallIDs = new Set<string>()
  for (const event of events.map(asRecord)) {
    if (event.type === 'permission.asked') {
      const approvalID = firstString(event.request_id, event.approval_id)
      if (approvalID && !askedByID.has(approvalID)) askedByID.set(approvalID, event)
      continue
    }
    if (event.type === 'permission.resolved') {
      const approvalID = firstString(event.request_id, event.approval_id)
      if (!approvalID) continue
      const result = piApprovalResolutionResult(event)
      if (result === 'approved') approvedIDs.add(approvalID)
      if (result === 'rejected') approvedIDs.delete(approvalID)
      continue
    }
    if (event.type === 'session.tool.success' || (event.type === 'session.tool.failed' && !isApprovalRequiredToolFailureEvent(event))) {
      const callID = firstString(event.call_id, event.tool_call_id, event.provider_tool_call_id)
      if (callID) executedCallIDs.add(callID)
    }
  }
  return [...askedByID.entries()]
    .filter(([approvalID]) => approvedIDs.has(approvalID))
    .map(([, event]) => piApprovalFromPermissionEvent(event))
    .filter((approval): approval is Record<string, unknown> => approval !== null)
    .filter((approval) => !executedCallIDs.has(firstString(approval.call_id, approval.tool_call_id)))
}

function normalizePiAwaitingApprovals(value: unknown, fallback?: Record<string, unknown> | null) {
  const approvals = Array.isArray(value)
    ? value.map(asRecord).filter((approval) => firstString(approval.approval_id, approval.request_id) && firstString(approval.call_id, approval.tool_call_id, approval.provider_tool_call_id))
    : []
  if (approvals.length > 0) return approvals
  return fallback ? [fallback] : []
}

function withoutPiAwaitingApprovalFields(value: unknown) {
  const {
    awaiting_approval: _awaitingApproval,
    awaiting_approvals: _awaitingApprovals,
    ...rest
  } = asRecord(value)
  return rest
}

function runtimeEventsUntilPendingPiApprovals(events: AgentRuntimeEvent[], awaitingApprovals: Record<string, unknown>[] = []) {
  if (awaitingApprovals.length === 0) return events
  const approvalIDs = new Set(awaitingApprovals.map((approval) => firstString(approval.approval_id, approval.request_id)).filter(Boolean))
  const callIDs = new Set(awaitingApprovals.map((approval) => firstString(approval.call_id, approval.tool_call_id, approval.provider_tool_call_id)).filter(Boolean))
  if (approvalIDs.size === 0 && callIDs.size === 0) return events
  let approvalIndex = -1
  events.forEach((event, index) => {
    const record = asRecord(event)
    if (record.type !== 'permission.asked') return
    const eventApprovalID = firstString(record.request_id, record.approval_id)
    const eventCallID = firstString(record.call_id, record.tool_call_id, record.provider_tool_call_id)
    if ((eventApprovalID && approvalIDs.has(eventApprovalID)) || (eventCallID && callIDs.has(eventCallID))) {
      approvalIndex = approvalIndex < 0 ? index : Math.min(approvalIndex, index)
    }
  })
  if (approvalIndex < 0) return events
  return events.slice(0, approvalIndex + 1)
}

function profileSnapshotHash(profile: AgentProfileSnapshot) {
  return agentProfileSnapshotHash(profile)
}

function hasSchema(schema: Record<string, unknown>) {
  const keys = Object.keys(schema)
  if (keys.length === 0) return false
  // UI defaults often create { "type": "object" } as a placeholder. Treat it as
  // unconstrained chat output; only schemas with real constraints enforce JSON.
  if (keys.length === 1 && asString(schema.type) === 'object') return false
  return true
}

function normalizeResourceID(value: unknown) {
  return asString(value).toLowerCase()
}

function normalizedResourceIDSet(values: Iterable<unknown>) {
  const set = new Set<string>()
  for (const value of values) {
    const normalized = normalizeResourceID(value)
    if (normalized) set.add(normalized)
  }
  return set
}

function resourceRefConfig(ref: unknown) {
  return isRecord(ref) ? asRecord(ref.config) : {}
}

function refMatchesResource(ref: { resource_type?: string, resource_id?: string | number }, resource: { id: number, resource_key: string, resource_id: string }) {
  if (typeof ref.resource_id === 'number') return resource.id === ref.resource_id
  const expected = normalizeResourceID(ref.resource_id)
  if (!expected) return false
  return expected === normalizeResourceID(resource.resource_key) ||
    expected === normalizeResourceID(resource.resource_id)
}

async function resourcesForProfile(_store: RuntimeStore, _workspaceID: number, profile: AgentProfileSnapshot) {
  if (!profile.frozen_resources) {
    throw new RuntimeDomainError(
      'runtime_profile_resources_not_frozen',
      'Runtime profile resources are not frozen',
      409
    )
  }
  return [
    ...profile.frozen_resources.skills,
    ...profile.frozen_resources.mcp_servers
  ]
}

function extractJSONFromText(text: string): Record<string, unknown> | null {
  const trimmed = text.trim()
  if (!trimmed) return null
  const candidates = [trimmed]
  const fenced = trimmed.match(/```(?:json)?\s*([\s\S]*?)\s*```/i)
  if (fenced?.[1]) candidates.unshift(fenced[1].trim())
  for (const candidate of candidates) {
    try {
      const parsed = JSON.parse(candidate)
      if (isRecord(parsed)) return parsed
    } catch {
      // Try the next candidate.
    }
  }
  return null
}

function coerceStructuredOutput(text: string, schema: Record<string, unknown>) {
  const parsed = extractJSONFromText(text)
  if (parsed) return parsed
  if (!hasSchema(schema)) return undefined
  const properties = asRecord(schema.properties)
  const output: Record<string, unknown> = {}
  if ('summary' in properties) output.summary = text
  if ('answer' in properties) output.answer = text
  if ('text' in properties) output.text = text
  return Object.keys(output).length > 0 ? output : undefined
}

function valueTypeMatches(expected: string, value: unknown) {
  switch (expected) {
    case 'string':
      return typeof value === 'string'
    case 'number':
      return typeof value === 'number' && Number.isFinite(value)
    case 'integer':
      return Number.isInteger(value)
    case 'boolean':
      return typeof value === 'boolean'
    case 'array':
      return Array.isArray(value)
    case 'object':
      return isRecord(value)
    case 'null':
      return value === null
    default:
      return true
  }
}

function validateAgainstSchema(schema: Record<string, unknown>, value: unknown, path = '$'): string[] {
  if (!hasSchema(schema)) return []
  const errors: string[] = []
  const schemaType = asString(schema.type)
  if (schemaType && !valueTypeMatches(schemaType, value)) {
    errors.push(`${path} should be ${schemaType}`)
    return errors
  }
  const enumValues = Array.isArray(schema.enum) ? schema.enum : []
  if (enumValues.length > 0 && !enumValues.some((item) => item === value)) {
    errors.push(`${path} should be one of ${enumValues.map((item) => JSON.stringify(item)).join(', ')}`)
  }
  if (schemaType === 'object' || isRecord(value)) {
    const record = jsonObject(value)
    const required = Array.isArray(schema.required) ? schema.required.map((item) => asString(item)).filter(Boolean) : []
    for (const key of required) {
      if (!(key in record)) errors.push(`${path}.${key} is required`)
    }
    const properties = asRecord(schema.properties)
    for (const [key, propertySchema] of Object.entries(properties)) {
      if (!(key in record) || !isRecord(propertySchema)) continue
      errors.push(...validateAgainstSchema(propertySchema, record[key], `${path}.${key}`))
    }
  }
  if ((schemaType === 'array' || Array.isArray(value)) && Array.isArray(value) && isRecord(schema.items)) {
    value.forEach((item, index) => {
      errors.push(...validateAgainstSchema(schema.items as Record<string, unknown>, item, `${path}[${index}]`))
    })
  }
  return errors
}

function publicResource(resource: { id: number, resource_kind: string, resource_key: string, resource_id: string, name: string, description: string, version: string, spec: Record<string, unknown>, endpoint: Record<string, unknown>, status: string }) {
  return {
    id: resource.id,
    resource_kind: resource.resource_kind,
    resource_key: resource.resource_key,
    resource_id: resource.resource_id,
    name: resource.name,
    description: resource.description,
    version: resource.version,
    status: resource.status,
    spec: resource.spec,
    endpoint: resource.endpoint
  }
}

function redactPublicRuntimeConfig(value: unknown): unknown {
  if (Array.isArray(value)) return value.map((item) => redactPublicRuntimeConfig(item))
  if (!isRecord(value)) return value
  const output: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(value)) {
    if (/secret|token|password|api[_-]?key|authorization|headers?/i.test(key)) {
      output[key] = '[redacted]'
      continue
    }
    output[key] = redactPublicRuntimeConfig(child)
  }
  return output
}

function isPersistedSecretReferenceKey(key: string) {
  return /(^|_)(credential|resource)?_?(id|ref)$/.test(key) || /(_env|_var)$/.test(key) || key === 'env'
}

function containsResolvedSecret(value: unknown, credentialContext = false): boolean {
  if (Array.isArray(value)) return value.some((item) => containsResolvedSecret(item, credentialContext))
  if (!value || typeof value !== 'object') return false
  for (const [rawKey, child] of Object.entries(value as Record<string, unknown>)) {
    const key = rawKey.toLowerCase()
    if (isPersistedSecretReferenceKey(key) && (typeof child === 'string' || typeof child === 'number')) continue
    const nestedCredentialContext = credentialContext || key === 'provider_credential_ref' || key === 'secret_ref'
    const sensitive = /^(authorization|proxy-authorization|cookie|set-cookie|api[_-]?key|access[_-]?token|bearer[_-]?token|password|private[_-]?key|client[_-]?secret|token|secret)$/.test(key) ||
      (nestedCredentialContext && key === 'key')
    if (sensitive && child !== undefined && child !== null && String(child).trim()) return true
    if (containsResolvedSecret(child, nestedCredentialContext)) return true
  }
  return false
}

function assertUnresolvedProfileSnapshot(profile: AgentProfileSnapshot | SessionModelOverride) {
  if (containsResolvedSecret(profile)) {
    throw new RuntimeDomainError(
      'provider_credential_reference_required',
      'Provider and MCP credentials must use an unresolved credential reference',
      400
    )
  }
}

function publicMcpResource(resource: { id: number, resource_kind: string, resource_key: string, resource_id: string, name: string, description: string, version: string, spec: Record<string, unknown>, endpoint: Record<string, unknown>, status: string }) {
  return {
    ...publicResource(resource),
    spec: redactPublicRuntimeConfig(resource.spec) as Record<string, unknown>,
    endpoint: redactPublicRuntimeConfig(resource.endpoint) as Record<string, unknown>
  }
}

function normalizeToolArguments(value: unknown) {
  if (isRecord(value)) return asRecord(value)
  if (typeof value !== 'string') return {}
  const trimmed = value.trim()
  if (!trimmed) return {}
  try {
    const parsed = JSON.parse(trimmed)
    return isRecord(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function normalizeRuntimeToolCall(value: unknown, index = 0): RuntimeToolCall | null {
  const raw = asRecord(value)
  const functionValue = raw.function
  const fn = asRecord(functionValue)
  const args = normalizeToolArguments(raw.arguments ?? fn.arguments ?? raw.input ?? raw.params)
  const toolName = firstString(raw.name, raw.tool_name, fn.name, typeof functionValue === 'string' ? functionValue : '')
  if (!toolName) return null
  const fallbackToolCallID = `tc_${readableIDSegment(toolName, 'tool_name')}_${seq36(index + 1)}`
  return {
    tool_call_id: firstString(raw.id, raw.tool_call_id, fallbackToolCallID),
    tool_name: toolName,
    arguments: args,
    operation_type: firstString(raw.operation_type, raw.operation, args.operation_type),
    target_type: firstString(raw.target_type, args.target_type),
    target_id: firstString(raw.target_id, args.target_id),
    risk_summary: firstString(raw.risk_summary, args.risk_summary),
    requires_confirmation: raw.requires_confirmation === true || args.requires_confirmation === true,
    mcp_server_key: firstString(raw.mcp_server_key, raw.mcp_server, raw.server_key, args.mcp_server_key),
    mcp_server_id: asStringOrNumber(raw.mcp_server_id ?? args.mcp_server_id),
    capability: firstString(raw.capability, args.capability)
  }
}

function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map((item) => canonicalJSON(item)).join(',')}]`
  }
  if (isRecord(value)) {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value ?? null)
}

function actionInputDigest(input: Record<string, unknown>) {
  return `sha256:${createHash('sha256').update(canonicalJSON(input)).digest('hex')}`
}

function toolCallSemanticKey(call: RuntimeToolCall) {
  return [
    call.tool_name,
    call.operation_type || '',
    call.target_type || '',
    call.target_id || '',
    call.mcp_server_key || '',
    call.mcp_server_id === undefined ? '' : String(call.mcp_server_id),
    call.capability || '',
    canonicalJSON(call.arguments || {})
  ].join('|')
}

function actionInput(action: AgentAction) {
  return asRecord(action.input_json)
}

function actionTarget(action: AgentAction) {
  return asRecord(action.target_json)
}

function actionPolicy(action: AgentAction) {
  return asRecord(action.policy_json)
}

function actionDisplay(action: AgentAction) {
  return asRecord(action.display_json)
}

function actionToolCallID(action: AgentAction) {
  return firstString(actionInput(action).provider_tool_call_id, actionInput(action).tool_call_id, action.input_json.id)
}

function actionToolName(action: AgentAction) {
  return firstString(actionInput(action).tool_name, action.capability_id, actionDisplay(action).name, action.action_kind)
}

function actionArguments(action: AgentAction) {
  return asRecord(actionInput(action).arguments)
}

function actionOperationType(action: AgentAction) {
  return firstString(actionPolicy(action).operation_type, actionTarget(action).operation_type)
}

function actionTargetType(action: AgentAction) {
  return firstString(actionTarget(action).target_type, actionTarget(action).type)
}

function actionTargetID(action: AgentAction) {
  return firstString(actionTarget(action).target_id, actionTarget(action).id)
}

function actionRiskSummary(action: AgentAction) {
  return firstString(actionPolicy(action).risk_summary, actionDisplay(action).summary)
}

function actionMcpServerKey(action: AgentAction) {
  return firstString(actionInput(action).mcp_server_key, actionPolicy(action).mcp_server_key)
}

function actionMcpServerID(action: AgentAction) {
  return actionInput(action).mcp_server_id ?? actionPolicy(action).mcp_server_id
}

function actionCapability(action: AgentAction) {
  return firstString(actionInput(action).capability, actionPolicy(action).capability)
}

function actionResult(action: AgentAction) {
  return asRecord(action.result_json)
}

function publicAction(action: AgentAction) {
  return {
    id: action.action_id,
    action_id: action.action_id,
    internal_id: action.id,
    action_kind: action.action_kind,
    runtime_run_id: action.runtime_run_id,
    source: action.source,
    capability_id: action.capability_id,
    input_json: action.input_json,
    input_digest: action.input_digest,
    target_json: action.target_json,
    policy_json: action.policy_json,
    display_json: action.display_json,
    status: action.status,
    decided_by: action.decided_by,
    decided_at: action.decided_at,
    executed_at: action.executed_at,
    result_json: action.result_json,
    error_msg: action.error_msg
  }
}

const ENTRY_RUNTIME_EVENT_SUMMARY_TYPES = new Set([
  'session.prompted',
  'session.step.started',
  'session.step.ended',
  'session.step.failed',
  'session.text.started',
  'session.text.ended',
  'session.reasoning.started',
  'session.reasoning.ended',
  'session.tool.input.started',
  'session.tool.input.delta',
  'session.tool.input.ended',
  'session.tool.called',
  'session.tool.progress',
  'session.tool.success',
  'session.tool.failed',
  'permission.asked',
  'permission.resolved',
  'session.compaction.started',
  'session.compaction.ended',
  'context.budget.evaluated',
  'context.compaction.started',
  'context.compaction.completed',
  'context.compaction.failed',
  'session.model.switched',
  'model.selected',
  'session.agent.switched',
  'session.steered',
  'session.error',
  'run.started',
  'run.completed',
  'run.failed',
  'run.cancelled',
  'run.timeout',
  'run.awaiting_decision',
  'run.interrupted',
  'run.step_announced',
  'context.build_started',
  'context_tags.resolved',
  'capability.snapshot',
  'mcp.tools.available',
  'mcp.tools_discovery_failed',
  'context.build_completed',
  'model.request_prepared',
  'model.call_started',
  'model.call_completed',
  'model.tool_call_detected',
  'action.permission_evaluated',
  'approval.requested',
  'action.decision_required',
  'action.decision',
  'approval.session_granted',
  'action.execution_started',
  'action.execution_succeeded',
  'action.execution_failed',
  'tool.result_prepared',
  'tool.executed',
  'tool.failed',
  'tool.rejected',
  'model.tool_result_submitted',
  'model.continuation_started',
  'model.continuation_completed',
  'model_provider.failed',
  'model.answer_synthesized',
  'provider.raw_stored',
  'output_schema.validated',
  'output_schema.invalid',
  'run.error',
  'skill.available',
  'skill.loaded',
  'skill.used',
  'subagent.spawned',
  'subagent.progress',
  'subagent.blocked_approval',
  'subagent.completed',
  'subagent.failed',
  'subagent.cancelled',
  'subagent.interrupted',
  'subagent.timeout',
  'orchestration.context.assembled',
  'orchestration.task.dispatched',
  'orchestration.task.status_changed',
  'orchestration.task.completed',
  'orchestration.task.failed',
  'orchestration.synthesis.started',
  'orchestration.review.started',
  'orchestration.followup.dispatched',
  'orchestration.completed'
])

const INTERNAL_RUNTIME_EVENT_FAILURE_TYPES = new Set([
  'session.tool.failed',
  'session.step.failed',
  'session.error',
  'tool.failed',
  'action.execution_failed',
  'model_provider.failed',
  'run.error',
  'run.failed',
  'run.timeout',
  'run.interrupted'
])

const INTERNAL_RUNTIME_EVENT_NARRATION_TYPES = new Set([
  'session.reasoning.delta',
  'session.reasoning.ended',
  'session.text.delta',
  'session.text.ended'
])

function toolResultTextValues(value: unknown, depth = 0): string[] {
  if (depth > 4 || value == null) return []
  if (typeof value === 'string') return [value]
  if (Array.isArray(value)) return value.flatMap((item) => toolResultTextValues(item, depth + 1))
  const record = asRecord(value)
  const error = asRecord(record.error)
  return [
    firstString(record.text),
    firstString(record.message),
    firstString(error.message, typeof record.error === 'string' ? record.error : ''),
    ...toolResultTextValues(record.content, depth + 1),
    ...toolResultTextValues(record.result, depth + 1)
  ].filter(Boolean)
}

function isInternalRuntimeEventPersistenceText(text: string) {
  if (!text) return false
  if (text.includes('uk_ai_agent_runtime_events_session_seq')) return true
  if (text.includes('Duplicate entry') && text.includes('ai_agent_runtime_events')) return true
  const normalized = text.toLowerCase()
  return text.includes('Duplicate entry') && (
    text.includes('服务端日志写入') ||
    (normalized.includes('runtime event') && (normalized.includes('persist') || normalized.includes('logging')))
  )
}

function isInternalRuntimeEventPersistenceEvent(event: unknown) {
  const record = asRecord(event)
  const type = firstString(record.type, record.event)
  if (INTERNAL_RUNTIME_EVENT_FAILURE_TYPES.has(type)) {
    const error = asRecord(record.error)
    const result = asRecord(record.result)
    const constraint = firstString(record.constraint, error.constraint, result.constraint)
    const table = firstString(record.table, error.table, result.table)
    const code = firstString(record.code, error.code, result.code)
    if (constraint === 'uk_ai_agent_runtime_events_session_seq') return true
    if (table === 'ai_agent_runtime_events' && code === 'ER_DUP_ENTRY') return true
    const texts = [
      firstString(record.message),
      firstString(error.message, typeof record.error === 'string' ? record.error : ''),
      ...toolResultTextValues(record.result),
      ...toolResultTextValues(record.content)
    ]
    return texts.some((text) => isInternalRuntimeEventPersistenceText(text))
  }
  if (!INTERNAL_RUNTIME_EVENT_NARRATION_TYPES.has(type)) return false
  return [record.text, record.delta, record.message]
    .map((value) => firstString(value))
    .some((text) => isInternalRuntimeEventPersistenceText(text))
}

function runtimeEventNarrationSpan(event: unknown) {
  const type = firstString(asRecord(event).type, asRecord(event).event)
  const match = /^session\.(reasoning|text)\.(started|delta|ended)$/.exec(type)
  if (!match) return undefined
  return { family: match[1], phase: match[2] }
}

function runtimeEventNarrationText(event: unknown) {
  const record = asRecord(event)
  return [record.text, record.delta, record.message]
    .map((value) => firstString(value))
    .filter(Boolean)
    .join('')
}

function runtimeEventPersistenceHiddenFlags(events: unknown[]) {
  const hidden = events.map((event) => isInternalRuntimeEventPersistenceEvent(event))
  const active: Record<string, number[]> = {
    reasoning: [],
    text: []
  }
  const flush = (family: string) => {
    const indexes = active[family]
    if (indexes.length === 0) return
    const narration = indexes.map((index) => runtimeEventNarrationText(events[index])).join('')
    if (isInternalRuntimeEventPersistenceText(narration)) {
      for (const index of indexes) hidden[index] = true
    }
    active[family] = []
  }

  for (const [index, event] of events.entries()) {
    const span = runtimeEventNarrationSpan(event)
    if (!span) continue
    if (span.phase === 'started') flush(span.family)
    active[span.family].push(index)
    if (span.phase === 'ended') flush(span.family)
  }
  flush('reasoning')
  flush('text')
  return hidden
}

function summarizeRuntimeEventsForEntry(events: unknown) {
  if (!Array.isArray(events)) return []
  const hidden = runtimeEventPersistenceHiddenFlags(events)
  return events
    .filter((event, index) => ENTRY_RUNTIME_EVENT_SUMMARY_TYPES.has(firstString(asRecord(event).type, asRecord(event).event)) && !hidden[index])
    .filter((event) => !isStreamingDeltaRuntimeEvent(event))
    .map((event) => compactRuntimeEventForEntry(event))
}

function isStreamingDeltaRuntimeEvent(event: unknown) {
  const type = firstString(asRecord(event).type, asRecord(event).event)
  return type.endsWith('.delta')
}

function compactRuntimeEventForEntry(event: unknown) {
  const record = asRecord(event)
  const type = firstString(record.type, record.event)
  if (!['session.tool.success', 'session.tool.failed', 'tool.executed', 'tool.failed', 'action.execution_succeeded', 'action.execution_failed'].includes(type)) {
    return event
  }
  // Tool result payloads can be 100KB+ (resource inventories). Keep identity and
  // a short preview so session switch / entry list stay responsive.
  const next = { ...record }
  for (const key of ['result', 'structured', 'output', 'content', 'data', 'tool_result', 'raw']) {
    if (key in next) next[key] = compactToolResultValue(next[key])
  }
  return next
}

function compactToolResultValue(value: unknown) {
  if (value == null) return value
  if (typeof value === 'string') {
    return value.length > 500 ? `${value.slice(0, 500)}…` : value
  }
  try {
    const text = JSON.stringify(value)
    if (text.length <= 1200) return value
    return { preview: `${text.slice(0, 500)}…`, truncated: true, bytes: text.length }
  } catch {
    return { preview: String(value).slice(0, 200), truncated: true }
  }
}

function truncateSubagentSummary(value: unknown, maxChars = SUBAGENT_SUMMARY_MAX_CHARS) {
  const text = asString(value)
  if (!text) return ''
  if (text.length <= maxChars) return text
  return `${text.slice(0, maxChars)}…`
}

function createSubagentProgressSampler(options: {
  minIntervalMs?: number
  onEmit: (payload: Record<string, unknown>) => Promise<void> | void
}) {
  const minIntervalMs = Math.max(50, Number(options.minIntervalMs) || SUBAGENT_PROGRESS_MIN_INTERVAL_MS)
  let lastEmitAt = 0
  let lastChildEventType = ''
  let suppressed = 0
  let pending: Record<string, unknown> | null = null
  let flushTimer: ReturnType<typeof setTimeout> | null = null

  const emitNow = async (payload: Record<string, unknown>) => {
    lastEmitAt = Date.now()
    lastChildEventType = firstString(payload.child_event_type, lastChildEventType)
    const suppressedCount = suppressed
    suppressed = 0
    pending = null
    await options.onEmit({
      ...payload,
      ...(suppressedCount > 0 ? { suppressed_count: suppressedCount } : {})
    })
  }

  const scheduleFlush = () => {
    if (flushTimer || !pending) return
    const waitMs = Math.max(0, minIntervalMs - (Date.now() - lastEmitAt))
    flushTimer = setTimeout(() => {
      flushTimer = null
      const next = pending
      if (!next) return
      void emitNow(next)
    }, waitMs)
    flushTimer.unref?.()
  }

  return {
    async push(payload: Record<string, unknown>) {
      const childEventType = firstString(payload.child_event_type)
      const nowMs = Date.now()
      if (lastEmitAt > 0 && nowMs - lastEmitAt < minIntervalMs && childEventType === lastChildEventType) {
        suppressed += 1
        pending = payload
        scheduleFlush()
        return
      }
      if (flushTimer) {
        clearTimeout(flushTimer)
        flushTimer = null
      }
      await emitNow(payload)
    },
    async flush() {
      if (flushTimer) {
        clearTimeout(flushTimer)
        flushTimer = null
      }
      if (!pending) return
      await emitNow(pending)
    }
  }
}

function projectRuntimeEventsForEntry(events: unknown[], run?: AIRuntimeRun) {
  if (!run || run.status !== 'awaiting_decision') return events
  if (firstString(run.result.runtime_engine, run.request.runtime_engine) !== 'pi') return events
  const storedAwaitingApproval = asRecord(run.result.awaiting_approval)
  const storedAwaitingApprovals = normalizePiAwaitingApprovals(run.result.awaiting_approvals, storedAwaitingApproval)
  const pendingApprovals = pendingPiApprovalsFromRuntimeEvents(events)
  const awaitingApprovals = pendingApprovals.length > 0 ? pendingApprovals : storedAwaitingApprovals
  return runtimeEventsUntilPendingPiApprovals(events as AgentRuntimeEvent[], awaitingApprovals)
}

function projectEntryForList(entry: AISessionEntry, run?: AIRuntimeRun): AISessionEntry {
  const output = asRecord(entry.output)
  const runResult = asRecord(run?.result)
  const toolResults = Array.isArray(output.tool_results)
    ? output.tool_results as RuntimeToolResultRecord[]
    : Array.isArray(runResult.tool_results)
      ? runResult.tool_results as RuntimeToolResultRecord[]
      : []
  const runText = firstString(runResult.text)
  const currentContentIsInternalPlan = isInternalContinuationPlan(entry.content)
  const projectedContent = (!entry.content.trim() || currentContentIsInternalPlan)
    ? firstString(
        runText && !isInternalContinuationPlan(runText) ? runText : '',
        toolResults.length > 0 ? toolResultsFallbackSummary(toolResults) : ''
      )
    : ''
  const outputText = firstString(output.text)
  const projectedOutputText = isInternalContinuationPlan(outputText)
    ? firstString(projectedContent, entry.content, toolResults.length > 0 ? toolResultsFallbackSummary(toolResults) : '')
    : ''
  const projectedStatus = entry.status === 'streaming' && run && isTerminalRunStatus(run.status)
    ? terminalRunEntryStatus(run.status)
    : entry.status
  const projectedEntry = projectedContent
    ? {
        ...entry,
        status: projectedStatus,
        content: projectedContent,
        content_blocks: [{ type: 'text', text: projectedContent }],
        output: {
          ...output,
          ...runResult,
          text: firstString(projectedOutputText, output.text, runText, projectedContent)
        }
      }
    : projectedStatus !== entry.status || projectedOutputText
      ? {
          ...entry,
          status: projectedStatus,
          output: {
            ...output,
            ...runResult,
            text: firstString(projectedOutputText, output.text, runText)
          }
        }
      : entry
  const projectedOutput = asRecord(projectedEntry.output)
  if (!Array.isArray(projectedOutput.runtime_events)) return projectedEntry
  return {
    ...projectedEntry,
    output: {
      ...projectedOutput,
      runtime_events: summarizeRuntimeEventsForEntry(projectRuntimeEventsForEntry(projectedOutput.runtime_events, run))
    }
  }
}

function isInternalContinuationPlan(text: string) {
  const normalized = text.trim().toLowerCase()
  if (!normalized) return false
  return [
    'now i will call the tool',
    'i will call the tool',
    'i will call ',
    'i will attempt ',
    "i'll attempt ",
    "i'll call ",
    'we will call ',
    'we will attempt ',
    "we'll attempt ",
    "we'll call ",
    'we need to call ',
    'we need to query ',
    'next i will call ',
    'next i will attempt ',
    'i need to call '
  ].some((prefix) => normalized.startsWith(prefix))
}

function prepareActionContinuationAnswer(
  assistantContent: string,
  completion: ChatModelResult | null,
  fallbackSummary: string,
  emitSynthesizedEvent = false,
  answerElapsedMs = 0
) {
  if (assistantContent.trim() && !isInternalContinuationPlan(assistantContent)) {
    return { assistantContent, completion, events: [] as RuntimeEventRecord[] }
  }
  const nextContent = fallbackSummary
  const nextCompletion = {
    ...(completion || {}),
    text: nextContent
  }
  const events: RuntimeEventRecord[] = []
  if (emitSynthesizedEvent) {
    events.push({
      type: 'model.answer_synthesized',
      payload: {
        reason: assistantContent.trim() ? 'internal_action_continuation_plan' : 'empty_action_continuation',
        text_chars: nextContent.length
      }
    })
  }
  events.push({
    type: 'answer_delta',
    payload: {
      delta: nextContent,
      elapsed_ms: answerElapsedMs
    }
  })
  return { assistantContent: nextContent, completion: nextCompletion, events }
}

function conciseJSON(value: unknown) {
  const record = asRecord(value)
  if (Object.keys(record).length === 0) return ''
  return JSON.stringify(record)
}

function actionResultSummary(action: AgentAction, toolName: string, toolStatus: AISessionEntry['status'], toolContent: string, toolOutput: Record<string, unknown>) {
  const result = asRecord(action.result_json)
  const structured = asRecord(result.structured_content)
  const structuredText = conciseJSON(Object.keys(toolOutput).length > 0 ? toolOutput : structured)
  const content = firstString(toolContent, result.content)
  if (toolStatus === 'failed' || action.status === 'failed') {
    return `工具 ${toolName} 执行失败：${firstString(action.error_msg, content, '未返回错误详情')}`
  }
  if (action.status === 'rejected') {
    return `已拒绝执行工具 ${toolName}。`
  }
  const resultText = firstString(structuredText, content, '已完成')
  return `工具 ${toolName} 已执行，结果：${resultText}`
}

function toolResultsFallbackSummary(toolResults: RuntimeToolResultRecord[]) {
  if (toolResults.length === 0) return ''
  const lines = toolResults.map((result) => {
    const structuredText = conciseJSON(result.structured_content)
    const resultText = firstString(structuredText, result.content, result.error, '未返回结果详情')
    if (result.status === 'failed') {
      return `工具 ${result.tool_name} 执行失败：${resultText}`
    }
    return `工具 ${result.tool_name} 已执行，结果：${resultText}`
  })
  return lines.join('\n')
}

function eventBusinessID(runtimeRunID: string, eventSeq: number) {
  return `${runtimeRunID}:ev${seq36(eventSeq)}`
}

function runtimeRunCoordinate(runtimeRunID: string) {
  const match = runtimeRunID.match(/^r_w([0-9a-z]+)_([0-9a-z]{6})_([0-9a-z]{6})$/)
  if (!match) {
    throw new RuntimeDomainError('runtime_run_id_invalid', 'Runtime run id is invalid')
  }
  return {
    workspace: match[1],
    session: match[2],
    run: match[3]
  }
}

function actionBusinessID(action: AgentAction) {
  return actionBusinessIDFor(action.runtime_run_id, action.id)
}

function actionBusinessIDFor(runtimeRunID: string, actionID: number) {
  const coordinate = runtimeRunCoordinate(runtimeRunID)
  return `a_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${seq36(actionID)}`
}

function artifactBusinessID(runtimeRunID: string, artifactSeq: number) {
  const coordinate = runtimeRunCoordinate(runtimeRunID)
  return `art_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${seq36(artifactSeq)}`
}

function sessionPermissionGrantBusinessID(workspaceID: number, sessionID: number, grantSeq: number) {
  return `grant_w${base36(workspaceID)}_${seq36(sessionID)}_${seq36(grantSeq)}`
}

function approvalBusinessIDFor(runtimeRunID: string, actionID: number) {
  const coordinate = runtimeRunCoordinate(runtimeRunID)
  return `ap_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${seq36(actionID)}`
}

function childRunLinkBusinessID(action: AgentAction, childSeq: number) {
  const coordinate = runtimeRunCoordinate(action.runtime_run_id)
  return `cl_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${seq36(action.id)}_${seq36(childSeq)}`
}

function decisionBusinessID(action: AgentAction, decisionSeq: number) {
  const coordinate = runtimeRunCoordinate(action.runtime_run_id)
  return `d_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${seq36(action.id)}_${seq36(decisionSeq)}`
}

function executionBusinessID(action: AgentAction, attempt: number) {
  const coordinate = runtimeRunCoordinate(action.runtime_run_id)
  return `x_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${seq36(action.id)}_${seq36(attempt)}`
}

function actionExecutionClaimError(outcome: 'already_claimed' | 'input_mismatch' | 'not_approved') {
  switch (outcome) {
    case 'already_claimed':
      return new RuntimeDomainError('action_execution_already_claimed', 'Action execution was already claimed', 409)
    case 'input_mismatch':
      return new RuntimeDomainError('action_input_integrity_failed', 'Action input digest no longer matches the persisted action', 409)
    case 'not_approved':
      return new RuntimeDomainError('action_not_approved', 'Action is not approved for execution', 409)
  }
}

function mcpToolCallRequestID(runtimeRunID: string, toolName: string, actionInternalID: unknown) {
  const coordinate = runtimeRunCoordinate(runtimeRunID)
  const tool = boundedReadableIDSegment(toolName, 'tool')
  return `mcp_call_w${coordinate.workspace}_${coordinate.session}_${coordinate.run}_${tool}_${seq36(actionInternalID || 0)}`
}

function mcpToolsListRequestID(resource: AgentResource) {
  const key = boundedReadableIDSegment(resource.resource_key, 'mcp-server')
  return `mcp_tools_w${base36(resource.workspace_id)}_${seq36(resource.id)}_${key}`
}

function mcpServerScopedRequestID(resource: AgentResource, serverKey: string) {
  const base = mcpToolsListRequestID(resource)
  if (!serverKey || normalizeResourceID(serverKey) === normalizeResourceID(resource.resource_key)) return base
  return `${base}_${boundedReadableIDSegment(serverKey, 'mcp-server')}`
}


function actionBusinessIDFromEventPayload(payload: Record<string, unknown>) {
  const directID = firstString(payload.action_id)
  if (directID) return directID
  const action = asRecord(payload.action)
  return firstString(action.action_id, action.id) || undefined
}

function eventDisplayJSON(eventType: string, payload: Record<string, unknown>) {
  const action = asRecord(payload.action)
  const actionDisplayJSON = asRecord(action.display_json)
  const actionPolicyJSON = asRecord(action.policy_json)
  const actionTargetJSON = asRecord(action.target_json)
  const actionLabel = firstString(
    payload.title,
    actionDisplayJSON.title,
    actionDisplayJSON.name,
    action.capability_id,
    action.action_kind
  )
  const stage = firstString(payload.stage)
  const elapsed = payload.elapsed_ms === undefined ? undefined : Number(payload.elapsed_ms)
  const status = firstString(
    payload.status,
    action.status,
    eventType === 'action.decision_required' ? 'awaiting_decision' : '',
    eventType === 'tool.executed' ? 'succeeded' : '',
    eventType === 'tool.failed' ? 'failed' : '',
    eventType === 'tool.rejected' ? 'rejected' : '',
    eventType === 'session.step.failed' || eventType === 'session.error' ? 'failed' : '',
    eventType === 'session.compaction.started' ? 'running' : '',
    eventType === 'session.compaction.ended' ? 'success' : ''
  )
  const childRunLinkID = firstString(payload.child_run_link_id, actionDisplayJSON.child_run_link_id)
  const childRuntimeRunID = firstString(payload.child_runtime_run_id, actionDisplayJSON.child_runtime_run_id)
  const childStatus = firstString(
    status,
      eventType === 'subagent.spawned' ? 'running' : '',
      eventType === 'subagent.started' ? 'running' : '',
      eventType === 'subagent.progress' ? 'running' : '',
    eventType === 'subagent.blocked_approval' ? 'blocked_approval' : '',
    eventType === 'subagent.completed' ? 'completed' : '',
    eventType === 'subagent.failed' ? 'failed' : '',
    eventType === 'subagent.cancelled' ? 'cancelled' : '',
    eventType === 'subagent.interrupted' ? 'interrupted' : '',
    eventType === 'subagent.timeout' ? 'timeout' : ''
  )
  return {
    event_type: eventType,
    title: firstString(
      payload.title,
      eventType.startsWith('subagent.') ? firstString(payload.name, actionDisplayJSON.name, actionDisplayJSON.title) : '',
      eventType === 'run.step_announced' && stage ? `步骤 ${stage}` : '',
      eventType === 'action.decision_required' && actionLabel ? `需要确认 ${actionLabel}` : '',
      eventType === 'action.execution_started' && actionLabel ? `开始执行 ${actionLabel}` : '',
      eventType === 'action.execution_succeeded' && actionLabel ? `已执行 ${actionLabel}` : '',
      eventType === 'action.execution_failed' && actionLabel ? `执行失败 ${actionLabel}` : '',
      eventType === 'tool.executed' && actionLabel ? `已执行 ${actionLabel}` : '',
      eventType === 'tool.failed' && actionLabel ? `执行失败 ${actionLabel}` : '',
      eventType === 'tool.rejected' && actionLabel ? `已拒绝 ${actionLabel}` : '',
      eventType
    ),
    summary: firstString(payload.summary, payload.error, actionDisplayJSON.summary, actionPolicyJSON.risk_summary, payload.message),
    status: childStatus || status,
    action_id: actionBusinessIDFromEventPayload(payload),
    child_run_link_id: childRunLinkID || undefined,
    child_runtime_run_id: childRuntimeRunID || undefined,
    parent_runtime_run_id: firstString(payload.parent_runtime_run_id) || undefined,
    parent_action_id: firstString(payload.parent_action_id) || undefined,
    stage: stage || undefined,
    elapsed_ms: Number.isFinite(elapsed) ? elapsed : undefined,
    action_kind: firstString(action.action_kind) || undefined,
    operation_type: firstString(actionPolicyJSON.operation_type, actionTargetJSON.operation_type) || undefined,
    target_type: firstString(actionTargetJSON.target_type, actionTargetJSON.type) || undefined,
    target_id: firstString(actionTargetJSON.target_id, actionTargetJSON.id) || undefined,
    artifact_refs: Array.isArray(payload.artifact_refs) ? payload.artifact_refs : []
  }
}

function publicActionEvent(event: ActionEvent) {
  return {
    event_id: event.event_id,
    event_seq: event.event_seq,
    runtime_run_id: event.runtime_run_id,
    session_id: event.session_id,
    action_id: event.action_id,
    entry_id: event.entry_id,
    event_type: event.event_type,
    type: event.event_type,
    visibility: event.visibility,
    payload_json: event.payload_json,
    display_json: event.display_json,
    created_at: event.created_at
  }
}

function storedActionEventRuntimeRecord(event: ActionEvent): RuntimeEventRecord {
  return {
    type: event.event_type,
    persisted: true,
    payload: {
      ...event.payload_json,
      event_id: event.event_id,
      event_seq: event.event_seq,
      runtime_run_id: event.runtime_run_id,
      session_id: event.session_id,
      action_id: event.action_id,
      entry_id: event.entry_id,
      display_json: event.display_json,
      created_at: event.created_at
    }
  }
}

function publicActiveRun(run: AIRuntimeRun | undefined) {
  if (!run || !ACTIVE_RUN_STATUSES.has(run.status)) return null
  return {
    runtime_run_id: run.runtime_run_id,
    status: run.status,
    input_entry_id: run.input_entry_id,
    output_entry_id: run.output_entry_id,
    runtime_engine: firstString(run.result.runtime_engine, run.request.runtime_engine),
    runtime_session_id: firstString(run.result.runtime_session_id, run.request.runtime_session_id),
    model: asRecord(run.request.model_config),
    awaiting_approval: asRecord(run.result.awaiting_approval),
    awaiting_approvals: Array.isArray(run.result.awaiting_approvals) ? run.result.awaiting_approvals : [],
    started_at: run.started_at,
    updated_at: run.updated_at,
    next_steps: run.status === 'awaiting_decision'
      ? ['approve_once', 'approve_session', 'reject', 'cancel']
      : ['wait', 'cancel']
  }
}

function publicPiRuntimeEvent(event: AgentRuntimeEvent, run: AIRuntimeRun, index: number) {
  const eventSeq = Number.isSafeInteger(Number(event.seq)) && Number(event.seq) > 0 ? Number(event.seq) : index + 1
  const eventID = firstString(event.event_id, `${run.runtime_run_id}:pi${seq36(eventSeq)}`)
  const runtimeRunID = firstString(event.runtime_run_id)
  if (!runtimeRunID || runtimeRunID !== run.runtime_run_id) {
    throw new RuntimeDomainError('runtime_event_run_mismatch', 'Runtime event does not belong to the requested run', 500)
  }
  const payload: Record<string, unknown> = {
    ...event,
    runtime_session_id: event.session_id,
    runtime_run_id: runtimeRunID,
    session_id: run.session_id,
    event_id: eventID,
    event_seq: eventSeq
  }
  const displayJSON = eventDisplayJSON(event.type, payload)
  return {
    event_id: eventID,
    event_seq: eventSeq,
    runtime_run_id: runtimeRunID,
    session_id: run.session_id,
    action_id: undefined,
    entry_id: undefined,
    event_type: event.type,
    type: event.type,
    visibility: 'public',
    payload_json: {
      ...payload,
      display_json: displayJSON
    },
    display_json: displayJSON,
    created_at: firstString(event.timestamp, run.updated_at, run.created_at, now())
  }
}

function publicPiRuntimeEvents(run: AIRuntimeRun, afterEventID = '', eventSource?: AgentRuntimeEvent[]) {
  const runtimeEvents = eventSource || (Array.isArray(run.result.runtime_events) ? run.result.runtime_events : [])
  const hidden = runtimeEventPersistenceHiddenFlags(runtimeEvents)
  const projectedEvents = runtimeEvents
    .map((source, index) => ({
      hidden: hidden[index],
      event: publicPiRuntimeEvent(source as AgentRuntimeEvent, run, index)
    }))
    .sort((left, right) => left.event.event_seq - right.event.event_seq)
  const after = afterEventID
    ? projectedEvents.find((item) => item.event.event_id === afterEventID)
    : undefined
  const visibleEvents = projectedEvents
    .filter((item) => !item.hidden)
    .filter((item) => !after || item.event.event_seq > after.event.event_seq)
    .map((item) => item.event)
  const { retained } = selectRetainedRunEvents(visibleEvents, CAPACITY_BUDGET.max_run_events_retained)
  const page = paginateEventsByCursor(retained, {
    page_size: CAPACITY_BUDGET.replay_page_size
  })
  return page.items
}

function publicRuntimeArtifact(artifact: RuntimeArtifact) {
  return {
    artifact_id: artifact.artifact_id,
    artifact_seq: artifact.artifact_seq,
    workspace_id: artifact.workspace_id,
    session_id: artifact.session_id,
    runtime_run_id: artifact.runtime_run_id,
    action_id: artifact.action_id,
    event_id: artifact.event_id,
    visibility: artifact.visibility,
    artifact_type: artifact.artifact_type,
    mime_type: artifact.mime_type,
    size_bytes: artifact.size_bytes,
    preview_json: artifact.preview_json,
    created_at: artifact.created_at
  }
}

function normalizeRuntimeToolCalls(values: unknown[]) {
  const calls: RuntimeToolCall[] = []
  const seen = new Set<string>()
  for (const [index, value] of values.entries()) {
    const call = normalizeRuntimeToolCall(value, index)
    if (!call) continue
    const semanticKey = toolCallSemanticKey(call)
    if (seen.has(call.tool_call_id) || seen.has(semanticKey)) continue
    seen.add(call.tool_call_id)
    seen.add(semanticKey)
    calls.push(call)
  }
  return calls
}

function runtimeToolDefinitionsByName(values: ChatModelToolDefinition[] = []) {
  const output = new Map<string, ChatModelToolDefinition>()
  for (const value of values) {
    if (!value?.name) continue
    const qualified = `${firstString(value.mcp_server_key)}:${value.name}`
    if (!output.has(qualified)) output.set(qualified, value)
    if (!output.has(value.name)) output.set(value.name, value)
    else if (output.get(value.name)?.mcp_server_key !== value.mcp_server_key) {
      output.delete(value.name)
    }
  }
  return output
}

function applyToolDefinitionMetadata(call: RuntimeToolCall, definitions: Map<string, ChatModelToolDefinition>) {
  const qualified = `${firstString(call.mcp_server_key)}:${call.tool_name}`
  const definition = definitions.get(qualified) || definitions.get(call.tool_name)
  if (!definition) return call
  return {
    ...call,
    operation_type: firstString(call.operation_type, definition.operation_type),
    target_type: firstString(call.target_type, definition.target_type),
    requires_confirmation: call.requires_confirmation || definition.requires_confirmation === true,
    mcp_server_key: firstString(call.mcp_server_key, definition.mcp_server_key),
    mcp_server_id: call.mcp_server_id ?? definition.mcp_server_id,
    capability: firstString(call.capability, definition.capability)
  }
}

function extractToolCallsFromText(text: string) {
  const parsed = extractJSONFromText(text)
  if (!parsed) return []
  const rawCalls = [
    ...(Array.isArray(parsed.tool_calls) ? parsed.tool_calls : []),
    ...(isRecord(parsed.tool_call) ? [parsed.tool_call] : [])
  ]
  return normalizeRuntimeToolCalls(rawCalls)
}

function isToolCallPayloadText(text: string) {
  const parsed = extractJSONFromText(text)
  if (!parsed) return false
  return Array.isArray(parsed.tool_calls) || isRecord(parsed.tool_call)
}

function buildToolResultContinuationContent(toolResults: RuntimeToolResultRecord[]) {
  return `tool_result:\n${JSON.stringify({
    results: toolResults.map((result) => ({
      tool_call_id: result.tool_call_id,
      tool_name: result.tool_name,
      status: result.status,
      content: result.content,
      structured_content: result.truncated ? { truncated: true } : result.structured_content,
      metadata: result.metadata,
      error: result.error,
      truncated: result.truncated,
      model_notice: result.model_notice,
      artifact_refs: result.artifact_refs
    }))
  })}`
}

function hasUsableModelOutput(completion: ChatModelResult | null | undefined) {
  const hasUsablePart = Array.isArray(completion?.parts) && completion.parts.some((part) => {
    if (part.type === 'tool_call') return true
    if (part.type === 'reasoning') return false
    return typeof part.text === 'string' && part.text.trim()
  })
  return Boolean(
    (typeof completion?.text === 'string' && completion.text.trim()) ||
    (Array.isArray(completion?.tool_calls) && completion.tool_calls.length > 0) ||
    hasUsablePart
  )
}

function toolPermissionForCall(profile: AgentProfileSnapshot, call: RuntimeToolCall, actorRole = '') {
  return evaluateToolPermission({
    policy: effectiveProfileToolPolicy(profile, call),
    toolName: call.tool_name,
    mcpServerKey: call.mcp_server_key,
    capability: call.capability,
    operationType: call.operation_type,
    requiresConfirmation: call.requires_confirmation,
    actorRole,
    args: call.arguments
  })
}

function effectiveProfileToolPolicy(profile: AgentProfileSnapshot, call?: RuntimeToolCall) {
  const profilePolicy = asRecord(profile.tool_policy)
  const refRules = (profile.mcp_servers || []).flatMap((ref) => {
    if (call && !mcpRefMatchesToolCall(ref, call)) return []
    const refPolicy = asRecord(asRecord(ref.config).tool_permissions)
    const rules = [
      ...toolPermissionRules(refPolicy, 'profile_ref'),
      ...(Array.isArray(refPolicy.rules) ? refPolicy.rules.map(asRecord) : [])
    ]
    return rules.map((rule) => scopedMcpPermissionRule(rule, ref, call, 'profile_ref'))
  })
  const resourceRules = (profile.frozen_resources?.mcp_servers || []).flatMap((resource) => {
    if (call && !runtimeToolCallMatchesMcpResource(call, resource)) return []
    const resourcePolicy = asRecord(asRecord(resource.spec).tool_permissions)
    const rules = [
      ...toolPermissionRules(resourcePolicy, 'resource'),
      ...(Array.isArray(resourcePolicy.rules) ? resourcePolicy.rules.map(asRecord) : [])
    ]
    return rules.map((rule) => scopedMcpResourcePermissionRule(rule, resource, call, 'resource'))
  })
  return {
    ...profilePolicy,
    rules: [
      ...refRules,
      ...resourceRules,
      ...(Array.isArray(profilePolicy.rules) ? profilePolicy.rules.map(asRecord) : [])
    ],
    default_decision: firstString(profilePolicy.default_decision, profilePolicy.defaultDecision, 'request'),
    default_matched_rule: firstString(profilePolicy.default_matched_rule, profilePolicy.defaultMatchedRule, 'profile.default_request'),
    default_scope: firstString(profilePolicy.default_scope, profilePolicy.defaultScope, 'profile')
  }
}

function mcpRefMatchesToolCall(ref: AgentProfileDraft['mcp_servers'][number], call: RuntimeToolCall) {
  const callServerIDs = normalizedResourceIDSet([
    call.mcp_server_key,
    call.mcp_server_id
  ])
  if (callServerIDs.size === 0) return true
  return callServerIDs.has(normalizeResourceID(ref.resource_id))
}

function scopedMcpPermissionRule(
  rule: Record<string, unknown>,
  ref: AgentProfileDraft['mcp_servers'][number],
  call: RuntimeToolCall | undefined,
  scope: string
) {
  const serverKeys = [
    call?.mcp_server_key,
    call?.mcp_server_id,
    ref.resource_id
  ].map(normalizeResourceID).filter(Boolean)
  return {
    ...rule,
    scope: firstString(rule.scope, scope),
    capability: firstString(rule.capability, 'tools'),
    ...(serverKeys.length > 0 && !rule.mcp_server_key && !rule.mcp_server_keys && !rule.mcp_servers
      ? { mcp_server_keys: [...new Set(serverKeys)] }
      : {})
  }
}

function scopedMcpResourcePermissionRule(
  rule: Record<string, unknown>,
  resource: AgentResource,
  call: RuntimeToolCall | undefined,
  scope: string
) {
  const serverKeys = [
    call?.mcp_server_key,
    call?.mcp_server_id,
    resource.resource_key,
    resource.resource_id,
    resource.id
  ].map(normalizeResourceID).filter(Boolean)
  return {
    ...rule,
    scope: firstString(rule.scope, scope),
    capability: firstString(rule.capability, 'tools'),
    ...(serverKeys.length > 0 && !rule.mcp_server_key && !rule.mcp_server_keys && !rule.mcp_servers
      ? { mcp_server_keys: [...new Set(serverKeys)] }
      : {})
  }
}

function actionKindForToolCall(call: RuntimeToolCall) {
  const toolName = call.tool_name.toLowerCase()
  if (
    toolName === 'easydo_pipeline_trigger' ||
    toolName === 'easydo_pipeline_run' ||
    toolName === 'pipeline.trigger' ||
    toolName === 'easydo.pipeline.trigger'
  ) {
    return 'pipeline.trigger'
  }
  if (
    toolName === 'subagent.spawn' ||
    toolName === 'easydo_subagent_spawn' ||
    toolName === 'easydo.subagent.spawn'
  ) {
    return 'subagent.spawn'
  }
  return 'mcp.tool'
}

function executorTypeForActionKind(actionKind: string) {
  if (actionKind === 'pipeline.trigger') return 'pipeline'
  if (actionKind === 'subagent.spawn') return 'subagent'
  return 'mcp'
}

function actionSourceForToolCall(_call: RuntimeToolCall): AgentAction['source'] {
  return 'model'
}

function actionTargetForToolCall(call: RuntimeToolCall) {
  const args = asRecord(call.arguments)
  if (actionKindForToolCall(call) === 'pipeline.trigger') {
    const pipelineID = firstString(args.pipeline_id, args.pipelineId, call.target_id)
    return {
      target_type: 'pipeline',
      target_id: pipelineID,
      pipeline_id: pipelineID
    }
  }
  if (actionKindForToolCall(call) === 'subagent.spawn') {
    const profileID = firstString(args.agent_profile_id, args.profile_id, call.target_id)
    return {
      target_type: 'agent_profile',
      target_id: profileID,
      agent_profile_id: profileID
    }
  }
  return {
    target_type: firstString(call.target_type, args.target_type),
    target_id: firstString(call.target_id, args.target_id)
  }
}

function actionPolicyForToolCall(call: RuntimeToolCall, permission: ToolPermissionEvaluation) {
  const operationType = permission.operation_type
  return {
    operation_type: operationType,
    risk_summary: call.risk_summary || permission.reason,
    requires_decision: permission.decision === 'ask',
    decision: permission.decision,
    matched_rule: permission.matched_rule,
    scope: permission.scope,
    risk_level: riskLevelForOperation(operationType),
    permission_key: permission.permission_key,
    mcp_server_key: call.mcp_server_key,
    mcp_server_id: call.mcp_server_id,
    capability: firstString(call.capability, 'tools'),
    policy_sources: [permission.scope, permission.matched_rule]
  }
}

function riskLevelForOperation(operationType: string) {
  const normalized = operationType.toLowerCase()
  if (!normalized || normalized === 'unknown') return 'write'
  if (['delete', 'destructive', 'drop', 'destroy'].includes(normalized)) return 'destructive'
  if (['admin', 'impersonate', 'grant', 'revoke'].includes(normalized)) return 'admin'
  if (['network', 'http', 'fetch'].includes(normalized)) return 'network'
  if (['write', 'create', 'update', 'mutate', 'mutation', 'execute', 'deploy', 'run'].includes(normalized)) return 'write'
  return 'read'
}

function actionDisplayForToolCall(call: RuntimeToolCall, actionKind: string, policy: Record<string, unknown>, target: Record<string, unknown>) {
  const title = actionKind === 'pipeline.trigger'
    ? `触发流水线 ${firstString(target.pipeline_id, target.target_id)}`
    : actionKind === 'subagent.spawn'
      ? `启动子 Agent ${firstString(target.agent_profile_id, target.target_id)}`
      : `调用工具 ${call.tool_name}`
  return {
    title,
    name: call.tool_name,
    summary: firstString(policy.risk_summary),
    risk_level: firstString(policy.risk_level),
    permission_key: firstString(policy.permission_key),
    input_preview: {
      tool_name: call.tool_name,
      mcp_server_key: call.mcp_server_key,
      capability: call.capability,
      arguments: call.arguments
    },
    target
  }
}

function runtimeToolCallFromAction(action: AgentAction): RuntimeToolCall {
  return {
    tool_call_id: actionToolCallID(action),
    tool_name: actionToolName(action),
    arguments: actionArguments(action),
    operation_type: actionOperationType(action),
    target_type: actionTargetType(action),
    target_id: actionTargetID(action),
    risk_summary: actionRiskSummary(action),
    requires_confirmation: actionPolicy(action).requires_decision === true,
    mcp_server_key: actionMcpServerKey(action),
    mcp_server_id: asStringOrNumber(actionMcpServerID(action)),
    capability: actionCapability(action)
  }
}

interface RuntimeEventRecord {
  type: string
  payload: Record<string, unknown>
  persisted?: boolean
}

interface ModelCallEventOptions {
  phase: 'initial' | 'fallback' | 'tool_continuation' | 'provider_continuation' | 'action_continuation'
  round?: number
}

interface ExecutionContext {
  capabilities: Record<string, unknown>
  contextTagAssembly: ContextTagAssembly
  modelContent: string
  loadedSkills: Record<string, unknown>[]
  subagentResults: Record<string, unknown>[]
  agentActions: Record<string, unknown>[]
  runtimeEvents: RuntimeEventRecord[]
  outputSchema: Record<string, unknown>
  developerInstructions: string
  mcpTools: ChatModelToolDefinition[]
}

function easyDoRuntimeDeveloperInstructions(baseInstructions: string, tools: ChatModelToolDefinition[] = []) {
  const toolNames = tools
    .map((tool) => firstString(tool.name))
    .filter(Boolean)
    .slice(0, 48)
  const toolHint = toolNames.length > 0
    ? `Available EasyDo tool names include: ${toolNames.join(', ')}.`
    : ''
  const runtimeInstructions = [
    'EasyDo runtime tool policy:',
    '- When the user asks to inspect, query, refresh, trigger, update, delete, execute, deploy, or otherwise operate EasyDo state and a matching tool is available, emit a tool_call. Do not replace that with a prose confirmation request.',
    '- For write/execute/refresh/update/delete actions, still emit the tool_call immediately. The EasyDo runtime will create the approval.requested and action.decision_required events and wait for the user approval UI.',
    '- If the user uses a resource label such as 7022 or 7023, do not assume it is the internal resource_id. Resolve it first with a read/list resource tool, then pass the resolved internal id to write or refresh tools.',
    '- If a resource tool returns not found, resolve the resource with the resource list/search tool before retrying or explaining the failure.',
    '- Final assistant text should summarize what happened after tool results are available; do not hide a required tool call inside markdown.',
    toolHint
  ].filter(Boolean).join('\n')
  return [baseInstructions, runtimeInstructions].filter((item) => firstString(item)).join('\n\n')
}

interface OutputValidationResult {
  structuredOutput?: Record<string, unknown>
  valid: boolean
  errors: string[]
}

interface RuntimeOutputPayload {
  structured_output: Record<string, unknown>
  output_schema_valid: boolean
  output_schema_errors: string[]
  runtime_events: RuntimeEventRecord[]
  capabilities: Record<string, unknown>
  loaded_skills: Record<string, unknown>[]
  subagent_results: Record<string, unknown>[]
  agent_actions: Record<string, unknown>[]
  tool_results?: RuntimeToolResultRecord[]
  provider_raw_refs?: Record<string, unknown>[]
  provider_error?: Record<string, unknown>
}

export interface RuntimeToolCall {
  tool_call_id: string
  tool_name: string
  arguments: Record<string, unknown>
  operation_type: string
  target_type: string
  target_id: string
  risk_summary: string
  requires_confirmation: boolean
  mcp_server_key?: string
  mcp_server_id?: string | number
  capability?: string
}

export interface RuntimeToolExecutionResult {
  content: string
  structured_content?: Record<string, unknown>
  metadata?: Record<string, unknown>
}

export interface RuntimeToolExecutionContext {
  actor: RuntimeActor
  session: AISession
  runtime_run_id: string
  action?: AgentAction
  auth?: RuntimeAuth
}

export interface RuntimeToolExecutor {
  execute(call: RuntimeToolCall, context: RuntimeToolExecutionContext): Promise<RuntimeToolExecutionResult>
}

export interface SessionServiceOptions {
  easydoServerURL?: string
  runStreamHeartbeatMs?: number
  runStreamPollMs?: number
  runtimeInstanceID?: string
  runLeaseMs?: number
  autoDrainSessionQueue?: boolean
  logger?: RuntimeLogger
  metrics?: RuntimeMetrics
}

export interface AgentRuntimeEngineOptions {
  runner: Pick<AgentHarnessRunner, 'prompt' | 'abort' | 'pauseForApproval' | 'steer'> | {
    prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult>
    abort?(sessionID: string): Promise<boolean>
    pauseForApproval?(runtimeRunID: string, approval: { approvalID: string, callID: string, toolName: string, reason: string }): Promise<boolean>
    steer?(runtimeRunID: string, queueItemID: string, instruction: string): Promise<'accepted' | 'already_accepted' | 'run_not_active'>
  }
  eventStore: Pick<AgentEventStore, 'append' | 'replay' | 'replayRun' | 'subscribe'>
  workspace?: {
    ensureDefault(actor: RuntimeActor): Promise<AIRuntimeWorkspace>
    acquire(run: AIRuntimeRun): Promise<{
      executionEnv: ExecutionEnv
      renew(): Promise<boolean>
      release(): Promise<void>
    }>
  }
}

interface RuntimeToolResultRecord {
  tool_call_id: string
  tool_name: string
  status: 'completed' | 'failed'
  content: string
  structured_content: Record<string, unknown>
  metadata: Record<string, unknown>
  error?: string
  truncated: boolean
  model_notice?: string
  artifact_refs: Record<string, unknown>[]
}

type RuntimeEventSink = (events: RuntimeEventRecord[]) => Promise<void> | void

function modelRequestEventPayload(request: ChatModelRequest, options: ModelCallEventOptions): Record<string, unknown> {
  const provider = asRecord(request.profile.provider)
  const binding = asRecord(request.profile.binding)
  const model = asRecord(request.profile.model)
  const inference = asRecord(request.profile.inference)
  // Prefer frozen Model Binding capability snapshot; never invent provider-independent defaults here.
  const contextWindow = firstPositiveNumber(
    binding.context_window_tokens,
    binding.context_window,
    model.context_window_tokens,
    model.context_window,
    model.contextWindow,
    model.context_tokens,
    provider.context_window,
    provider.contextWindow
  )
  const maxTokens = firstPositiveNumber(
    binding.max_output_tokens,
    model.max_output_tokens,
    model.max_tokens,
    model.maxTokens,
    inference.max_tokens,
    inference.maxTokens,
    inference.max_output_tokens
  )
  const thinkingLevel = firstString(
    inference.thinking_level,
    inference.thinkingLevel,
    inference.reasoning_effort,
    inference.reasoningEffort,
    inference.reasoning,
    model.thinking_level,
    model.thinkingLevel,
    model.reasoning
  )
  return {
    phase: options.phase,
    round: options.round || 1,
    provider: firstString(request.profile.provider.provider_type, request.profile.provider.id, request.profile.provider.name),
    model: firstString(request.profile.model.provider_model_key, request.profile.model.model, request.profile.model.name),
    provider_id: firstString(provider.provider_id, provider.id, provider.name, provider.provider_type, provider.type),
    provider_type: firstString(provider.provider_type, provider.type),
    base_url: firstString(model.base_url, model.baseUrl, provider.base_url, provider.baseUrl),
    context_window: contextWindow,
    context_window_tokens: contextWindow,
    max_tokens: maxTokens,
    max_output_tokens: maxTokens,
    capability_source: firstString(binding.capability_source, model.capability_source),
    capability_snapshot_hash: firstString(binding.capability_snapshot_hash, model.capability_snapshot_hash),
    thinking_level: thinkingLevel || undefined,
    content_chars: request.content.length,
    history_count: request.history.length,
    tool_count: request.tools?.length || 0,
    loaded_skill_count: request.loaded_skills?.length || 0,
    subagent_result_count: request.subagent_results?.length || 0,
    has_output_schema: hasSchema(request.output_schema || {})
  }
}

function modelCompletionEventPayload(completion: ChatModelResult | null, options: ModelCallEventOptions): Record<string, unknown> {
  const toolCalls = completion?.tool_calls || []
  return {
    phase: options.phase,
    round: options.round || 1,
    status: hasUsableModelOutput(completion) ? 'completed' : 'empty',
    text_chars: completion?.text?.length || 0,
    tool_call_count: toolCalls.length,
    finish_reason: firstString(completion?.finish?.reason)
  }
}

function toolCallEventPayload(call: RuntimeToolCall): Record<string, unknown> {
  return {
    provider_tool_call_id: call.tool_call_id,
    tool_name: call.tool_name,
    operation_type: call.operation_type,
    target_type: call.target_type,
    target_id: call.target_id,
    mcp_server_key: call.mcp_server_key,
    mcp_server_id: call.mcp_server_id,
    capability: call.capability,
    requires_confirmation: call.requires_confirmation
  }
}

function toolResultEventPayload(toolResult: RuntimeToolResultRecord): Record<string, unknown> {
  return {
    provider_tool_call_id: toolResult.tool_call_id,
    tool_name: toolResult.tool_name,
    status: toolResult.status,
    truncated: toolResult.truncated,
    content_chars: toolResult.content.length,
    artifact_refs: toolResult.artifact_refs,
    has_structured_content: Object.keys(toolResult.structured_content || {}).length > 0,
    model_notice: toolResult.model_notice || undefined
  }
}

function discoveredToolNames(resource: AgentResource) {
  return mcpToolDefinitionsFromValues(mcpRawTools(resource)).map((tool) => tool.name)
}

function mcpRawTools(resource: AgentResource) {
  const spec = asRecord(resource.spec)
  return [
    ...(Array.isArray(spec.discovered_tools) ? spec.discovered_tools : []),
    ...(Array.isArray(spec.tools) ? spec.tools : [])
  ]
}

function normalizeMcpToolDefinition(value: unknown, resource?: AgentResource): ChatModelToolDefinition | null {
  const record = asRecord(value)
  const name = firstString(record.name, record.tool_name, typeof value === 'string' ? value : '')
  if (!name) return null
  const inputSchema = asRecord(record.input_schema ?? record.inputSchema ?? record.parameters ?? record.schema)
  const definition: ChatModelToolDefinition = {
    name,
    description: firstString(record.description),
    input_schema: Object.keys(inputSchema).length > 0 ? inputSchema : { type: 'object', properties: {} }
  }
  const operationType = firstString(record.operation_type, record.operationType, record.operation)
  const targetType = firstString(record.target_type, record.targetType)
  if (operationType) definition.operation_type = operationType
  if (targetType) definition.target_type = targetType
  if (record.requires_confirmation === true || record.requiresConfirmation === true) {
    definition.requires_confirmation = true
  }
  const mcpServerKey = firstString(record.mcp_server_key, record.mcp_server, record.server_key, resource?.resource_key)
  if (mcpServerKey) definition.mcp_server_key = mcpServerKey
  const mcpServerID = record.mcp_server_id ?? resource?.id
  if (mcpServerID !== undefined && mcpServerID !== null && String(mcpServerID).trim()) definition.mcp_server_id = mcpServerID as string | number
  definition.capability = firstString(record.capability, 'tools')
  return definition
}

function mcpToolDefinitionsFromValues(values: unknown[], resource?: AgentResource) {
  const definitions: ChatModelToolDefinition[] = []
  const seen = new Set<string>()
  for (const value of values) {
    const definition = normalizeMcpToolDefinition(value, resource)
    const key = `${definition?.mcp_server_key || ''}:${definition?.name || ''}`
    if (!definition || seen.has(key)) continue
    seen.add(key)
    definitions.push(definition)
  }
  return definitions
}

function uniqueMcpToolDefinitions(values: ChatModelToolDefinition[]) {
  return mcpToolDefinitionsFromValues(values)
}

function publicMcpToolDefinition(tool: ChatModelToolDefinition) {
  const output: Record<string, unknown> = {
    name: tool.name,
    description: tool.description,
    input_schema: tool.input_schema
  }
  if (tool.operation_type) output.operation_type = tool.operation_type
  if (tool.target_type) output.target_type = tool.target_type
  if (tool.requires_confirmation === true) output.requires_confirmation = true
  if (tool.mcp_server_key) output.mcp_server_key = tool.mcp_server_key
  if (tool.mcp_server_id !== undefined) output.mcp_server_id = tool.mcp_server_id
  if (tool.capability) output.capability = tool.capability
  return output
}

function publicPiModelSelection(selection: ReturnType<typeof modelSelectionFromRuntimeConfig>) {
  if (!selection) return {}
  return {
    provider_id: selection.provider_id,
    id: selection.id,
    api: selection.api,
    base_url: selection.base_url,
    has_api_key: Boolean(selection.api_key),
    headers: Object.keys(selection.headers ?? {}),
    context_window: selection.context_window,
    max_tokens: selection.max_tokens,
    thinking_level: selection.thinking_level,
    inference: selection.inference ? Object.fromEntries(
      Object.entries(selection.inference).filter(([key]) => !/key|token|secret|password/i.test(key))
    ) : undefined
  }
}

function mcpServerConfigs(resource: AgentResource) {
  const specServers = asRecord(asRecord(resource.spec).mcpServers)
  const configs = Object.entries(specServers)
    .map(([name, config]) => ({ name, config: asRecord(config) }))
    .filter(({ config }) => asString(config.disabled) !== 'true' && config.disabled !== true)
  if (configs.length > 0) return configs
  const endpoint = asRecord(resource.endpoint)
  if (firstString(endpoint.url, endpoint.command)) {
    return [{ name: resource.resource_key, config: endpoint }]
  }
  return []
}

function selectMcpServerConfig(resource: AgentResource, call: RuntimeToolCall) {
  const configs = mcpServerConfigs(resource)
  if (configs.length === 0) return undefined
  const wanted = firstString(call.mcp_server_key, call.mcp_server_id).toLowerCase()
  if (wanted) {
    const exact = configs.find((entry) => {
      const keys = [
        entry.name,
        resource.resource_key,
        String(resource.id || ''),
        firstString(asRecord(entry.config).server_key)
      ].map((item) => normalizeResourceID(item))
      return keys.includes(normalizeResourceID(wanted))
    })
    if (exact) return exact
  }
  if (configs.length === 1) return configs[0]
  const toolOwners = mcpToolDefinitionsFromValues(mcpRawTools(resource), resource)
    .filter((tool) => tool.name === call.tool_name)
  if (toolOwners.length === 1) {
    const ownerKey = firstString(toolOwners[0].mcp_server_key)
    const owned = configs.find((entry) => normalizeResourceID(entry.name) === normalizeResourceID(ownerKey))
    if (owned) return owned
  }
  if (toolOwners.length > 1 && !wanted) {
    throw new RuntimeDomainError(
      'mcp_tool_ambiguous',
      `Tool ${call.tool_name} is available from multiple MCP servers; specify mcp_server_key`,
      400
    )
  }
  return configs[0]
}

function resourceSupportsTool(resource: AgentResource, toolName: string) {
  const tools = discoveredToolNames(resource)
  return tools.length === 0 || tools.includes(toolName)
}

function runtimeToolCallMatchesMcpResource(call: RuntimeToolCall, resource: AgentResource) {
  const wanted = normalizedResourceIDSet([call.mcp_server_key, call.mcp_server_id])
  if (wanted.size === 0) return true
  return wanted.has(normalizeResourceID(resource.resource_key)) ||
    wanted.has(normalizeResourceID(resource.resource_id)) ||
    wanted.has(normalizeResourceID(resource.id))
}

function mcpTextContent(value: unknown) {
  if (Array.isArray(value)) {
    return value
      .map((item) => {
        const record = asRecord(item)
        if (firstString(record.text)) return firstString(record.text)
        if (Object.keys(record).length > 0) return JSON.stringify(record)
        return asString(item)
      })
      .filter(Boolean)
      .join('\n')
  }
  if (typeof value === 'string') return value
  if (isRecord(value)) return JSON.stringify(value)
  return ''
}

const mcpClients = new Map<string, McpClient>()

function mcpClient(resource: AgentResource, config: Record<string, unknown>, auth: RuntimeAuth | undefined, logger: RuntimeLogger) {
  const resolvedConfig = resolveMcpClientConfigSecrets(config, resource.secret_ref)
  const headers = Object.fromEntries(Object.entries(asRecord(resolvedConfig.headers)).map(([key, value]) => [key, String(value)]))
  const delegatedToken = firstString(auth?.delegated_user_token, auth?.user_token)
  if (delegatedToken && !headers.Authorization && !headers.authorization) headers.Authorization = delegatedToken
  const clientConfig = {
    ...resolvedConfig,
    type: firstString(resolvedConfig.type, resource.endpoint.type, resolvedConfig.command ? 'stdio' : 'streamable_http'),
    headers,
    timeout_ms: firstPositiveNumber(resolvedConfig.timeout_ms, resolvedConfig.timeoutMs, resource.endpoint.timeout_ms, resource.endpoint.timeout) || 30_000
  }
  const cacheKey = createHash('sha256').update(stableJSONStringify(clientConfig)).digest('hex')
  const existing = mcpClients.get(cacheKey)
  if (existing) return existing
  const client = createMcpClient(clientConfig, logger)
  mcpClients.set(cacheKey, client)
  return client
}

class McpRuntimeToolExecutor implements RuntimeToolExecutor {
  constructor(
    private readonly store: RuntimeStore,
    private readonly easydoServerURL = '',
    private readonly logger: RuntimeLogger = runtimeLogger
  ) {}

  async execute(call: RuntimeToolCall, context: RuntimeToolExecutionContext): Promise<RuntimeToolExecutionResult> {
    const runtimeRunID = firstString(context.runtime_run_id, context.action?.runtime_run_id)
    if (!runtimeRunID) {
      throw new RuntimeDomainError('mcp_runtime_run_required', 'Runtime run id is required for MCP tool execution', 500)
    }
    const run = await this.store.getRun(context.actor.workspace_id, runtimeRunID)
    const profile = run?.profile_snapshot
    if (!profile) {
      throw new RuntimeDomainError('mcp_profile_snapshot_missing', 'Runtime run profile snapshot is missing', 500)
    }
    const resource = await this.selectMcpResource(context.actor, profile, call, context.auth)
    const selected = selectMcpServerConfig(resource, call)
    if (!selected) throw new RuntimeDomainError('mcp_server_not_callable', 'MCP server endpoint is not callable by easydo-ai-runtime')
    const serverKey = firstString(call.mcp_server_key, selected.name, resource.resource_key)
    const requestID = mcpToolCallRequestID(runtimeRunID, call.tool_name, context.action?.id)
    const result = await mcpClient(resource, selected.config, context.auth, this.logger).request('tools/call', {
      name: call.tool_name,
      arguments: call.arguments
    }, requestID, {
      request_id: firstString(run.request.request_id),
      workspace_id: run.workspace_id,
      session_id: run.session_id,
      runtime_run_id: runtimeRunID,
      parent_runtime_run_id: firstString(run.request.parent_runtime_run_id),
      mcp_server_id: serverKey
    })
    const structuredContent = asRecord(result.structuredContent ?? result.structured_content ?? result.data)
    return {
      content: firstString(mcpTextContent(result.content), mcpTextContent(result.text), JSON.stringify(result)),
      structured_content: structuredContent,
      metadata: {
        ...asRecord(result.metadata),
        request_id: firstString(asRecord(result.metadata).request_id, requestID),
        mcp_server: serverKey,
        mcp_server_key: serverKey,
        capability: firstString(call.capability, 'tools')
      }
    }
  }

  private async selectMcpResource(actor: RuntimeActor, profile: AgentProfileSnapshot, call: RuntimeToolCall, auth?: RuntimeAuth) {
    const resources = await resourcesForProfile(this.store, actor.workspace_id, profile)
    const candidates: AgentResource[] = []
    for (const ref of profile.mcp_servers || []) {
      if (ref.resource_type !== 'mcp_server') continue
      const resource = resources.find((item) => item.resource_kind === 'mcp_server' && refMatchesResource(ref, item))
      if (resource) {
        candidates.push(resource)
        continue
      }
      if (normalizeResourceID(ref.resource_id) === 'easydo') {
        const builtin = this.builtinEasyDoResource(actor, auth)
        if (builtin) candidates.push(builtin)
      }
    }
    const matchingCandidates = candidates.filter((resource) => runtimeToolCallMatchesMcpResource(call, resource))
    const callable = (matchingCandidates.length > 0 ? matchingCandidates : candidates).find((resource) => {
      if (resource.status === 'disabled' || resource.status === 'archived') return false
      if (!resourceSupportsTool(resource, call.tool_name)) return false
      return mcpServerConfigs(resource).length > 0
    })
    if (!callable) {
      throw new RuntimeDomainError('mcp_tool_not_bound', 'Requested MCP tool is not available from bound Agent Profile MCP servers', 403)
    }
    return callable
  }

  private builtinEasyDoResource(actor: RuntimeActor, auth?: RuntimeAuth): AgentResource | null {
    const baseURL = firstString(this.easydoServerURL, process.env.EASYDO_SERVER_URL, process.env.SERVER_INTERNAL_URL)
    const delegatedToken = firstString(auth?.delegated_user_token, auth?.user_token)
    if (!baseURL || !delegatedToken) return null
    const timestamp = now()
    return {
      id: 0,
      workspace_id: actor.workspace_id,
      resource_kind: 'mcp_server',
      resource_key: 'easydo',
      resource_id: 'easydo',
      name: 'easydo',
      description: 'Built-in EasyDo MCP Server',
      version: 'latest',
      status: 'active',
      spec: {
        builtin: true,
        mcpServers: {
          easydo: {
            type: 'streamable_http',
            url: `${baseURL.replace(/\/+$/, '')}/mcp`,
            headers: {
              Authorization: delegatedToken
            }
          }
        }
      },
      endpoint: {
        type: 'streamable_http',
        url: `${baseURL.replace(/\/+$/, '')}/mcp`
      },
      secret_ref: {},
      tags: ['mcp-server', 'builtin', 'easydo'],
      created_by: actor.user_id,
      created_at: timestamp,
      updated_at: timestamp
    }
  }
}

function sessionKind(value: unknown): AISession['session_kind'] {
  switch (value) {
    case 'task_run':
    case 'workflow':
    case 'test_run':
      return value
    default:
      return 'chat'
  }
}

function currentSessionKey(actor: RuntimeActor, payload: Record<string, unknown>) {
  const profileSelection = asRecord(payload.profile_selection)
  const selectedProfileID = firstString(
    profileSelection.profile_id,
    profileSelection.agent_profile_id,
    payload.profile_id,
    payload.agent_profile_id
  )
  if (selectedProfileID) {
    const selectedVersionKey = firstString(
      profileSelection.profile_version_id,
      profileSelection.agent_profile_version_id,
      payload.profile_version_id,
      payload.agent_profile_version_id,
      'latest'
    )
    const firstSessionTimestamp = firstString(profileSelection.first_session_timestamp, payload.first_session_timestamp)
    const sessionScope = firstSessionTimestamp || [
      asString(payload.source),
      asString(payload.business_type),
      asString(payload.business_id)
    ].join(':')
    return [
      actor.workspace_id,
      actor.user_id,
      selectedProfileID,
      selectedVersionKey,
      sessionScope
    ].join(':')
  }
  const profileName = firstString(payload.profile_name)
  if (profileName) {
    return [
      actor.workspace_id,
      actor.user_id,
      actor.auth_session_id,
      profileName,
      asString(payload.business_type),
      asString(payload.business_id)
    ].join(':')
  }
  return [
    actor.workspace_id,
    actor.user_id,
    actor.auth_session_id,
    asString(payload.session_kind),
    asString(payload.business_type),
    asString(payload.business_id)
  ].join(':')
}

function isLatestProfileSelection(value: unknown) {
  return asString(value).trim().toLowerCase() === 'latest'
}

function isDraftProfileSelection(value: unknown) {
  return asString(value).trim().toLowerCase() === 'draft'
}

function profileVersionKey(value: unknown) {
  const raw = asString(value).trim()
  return raw || 'latest'
}

function assertSnapshotCallable(profile: AgentProfileSnapshot) {
  if (profile.status === 'disabled' || profile.status === 'archived') {
    throw new RuntimeDomainError('agent_profile_not_callable', 'Agent profile cannot be called from chatbox')
  }
}

function estimateInputTokens(content: string) {
  const words = content.trim().split(/\s+/).filter(Boolean).length
  const charEstimate = Math.ceil(content.length / 4)
  return Math.max(words, charEstimate)
}

function enforceChatboxInputLimit(profile: AgentProfileSnapshot, content: string) {
  const maxTokens = Number(asRecord(profile.inference).max_tokens || 0)
  if (!Number.isFinite(maxTokens) || maxTokens <= 0) return
  const hardLimit = Math.floor(maxTokens / 2)
  if (hardLimit <= 0) return
  if (estimateInputTokens(content) > hardLimit) {
    throw new RuntimeDomainError(
      'chatbox_input_too_large',
      `Input exceeds Agent Profile Max Tokens / 2 hard limit (${hardLimit} tokens)`,
      400
    )
  }
}

interface ResolvedSessionProfile {
  profile: AgentProfileSnapshot
  profileVersionID: number
  profileVersionKey: string
  snapshotHash: string
}

const RUNNING_MODEL_SWITCH_STATUSES = new Set<AIRuntimeRun['status']>(['queued', 'running'])

function normalizeSessionModelOverride(value: unknown): SessionModelOverride {
  const input = asRecord(value)
  return {
    provider: asRecord(input.provider),
    binding: asRecord(input.binding),
    model: asRecord(input.model),
    provider_credential_ref: asRecord(input.provider_credential_ref),
    inference: asRecord(input.inference)
  }
}

function hasSessionModelOverride(override: SessionModelOverride | undefined) {
  return Boolean(override && [
    override.provider,
    override.binding,
    override.model,
    override.provider_credential_ref,
    override.inference
  ].some((item) => item && Object.keys(item).length > 0))
}

function mergeSessionModelOverride(profile: AgentProfileSnapshot, override: SessionModelOverride | undefined): AgentProfileSnapshot {
  if (!hasSessionModelOverride(override)) return profile
  return {
    ...profile,
    provider: { ...asRecord(profile.provider), ...asRecord(override?.provider) },
    binding: { ...asRecord(profile.binding), ...asRecord(override?.binding) },
    model: { ...asRecord(profile.model), ...asRecord(override?.model) },
    provider_credential_ref: { ...asRecord(profile.provider_credential_ref), ...asRecord(override?.provider_credential_ref) },
    inference: { ...asRecord(profile.inference), ...asRecord(override?.inference) }
  }
}

export class SessionService {
  private readonly toolExecutor: RuntimeToolExecutor
  private readonly piApprovalLocks = new Map<string, Promise<void>>()
  private lastOrphanReconcileAt = 0
  private readonly logger: RuntimeLogger

  constructor(
    private readonly store: RuntimeStore,
    private readonly profileService: AgentProfileService,
    private readonly chatModelClient: ChatModelClient = createDefaultChatModelClient(),
    toolExecutor?: RuntimeToolExecutor,
    private readonly options: SessionServiceOptions = {},
    private readonly contextTagRegistry = new ContextTagRegistry(),
    private readonly agentRuntimeEngine?: AgentRuntimeEngineOptions
  ) {
    this.logger = options.logger ?? runtimeLogger
    this.toolExecutor = toolExecutor || new McpRuntimeToolExecutor(store, options.easydoServerURL, this.logger)
  }

  private runtimeInstanceID() {
    return firstString(this.options.runtimeInstanceID, process.env.AI_RUNTIME_INSTANCE_ID, process.env.HOSTNAME, 'runtime-local')
  }

  private runLeaseMs() {
    return Math.max(3_000, Number(this.options.runLeaseMs) || 15_000)
  }

  private async appendSessionQueueEvent(input: SessionQueueEventAppendInput) {
    const eventStore = this.agentRuntimeEngine?.eventStore
    if (!eventStore) return
    const runtimeSessionID = sessionBusinessID(input.item.workspace_id, input.item.session_id)
    const eventBase = {
      session_id: runtimeSessionID,
      event_id: stableEventID(runtimeSessionID, input.type, input.item.queue_item_id, input.marker),
      seq: 0,
      timestamp: input.item.updated_at,
      queue_item: input.item
    }
    let event: SessionQueueRuntimeEvent
    switch (input.type) {
      case 'session.queue.added':
        event = { ...eventBase, type: 'session.queue.added' }
        break
      case 'session.queue.claimed':
        event = { ...eventBase, type: 'session.queue.claimed' }
        break
      case 'session.queue.cancelled':
        event = { ...eventBase, type: 'session.queue.cancelled' }
        break
      case 'session.queue.expired':
        event = { ...eventBase, type: 'session.queue.expired' }
        break
      case 'session.queue.failed':
        event = { ...eventBase, type: 'session.queue.failed' }
        break
      case 'session.steer.applied':
        event = { ...eventBase, type: 'session.steer.applied', runtime_run_id: input.runtime_run_id }
        break
      case 'session.follow_up.started':
        event = {
          ...eventBase,
          type: 'session.follow_up.started',
          runtime_run_id: input.runtime_run_id,
          consumed_runtime_run_id: input.consumed_runtime_run_id
        }
        break
    }
    await eventStore.append(event)
  }

  private async appendUncommittedQueueEvent(result: QueueTransitionCommit) {
    if (!result.event || result.event_committed) return
    await this.agentRuntimeEngine?.eventStore.append(result.event)
  }

  private runLogContext(run: AIRuntimeRun, operation: string): RuntimeLogInput {
    return {
      component: 'session-service',
      operation,
      request_id: firstString(run.request.request_id),
      workspace_id: run.workspace_id,
      session_id: run.session_id,
      runtime_run_id: run.runtime_run_id,
      parent_runtime_run_id: firstString(run.request.parent_runtime_run_id)
    }
  }

  private logTerminalRun(run: AIRuntimeRun) {
    if (!isTerminalRunStatus(run.status)) return
    const context = this.runLogContext(run, 'run-terminal')
    if (run.status === 'completed' || run.status === 'cancelled') {
      this.logger.info({
        ...context,
        outcome: run.status,
        code: firstString(run.error_code) || undefined,
        category: run.status === 'cancelled' ? 'cancelled' : undefined
      })
    } else {
      const descriptor = classifyRuntimeError({
        code: firstString(run.error_code),
        message: firstString(run.error_msg),
        ...asRecord(run.result.error)
      })
      this.logger.error({
        ...context,
        outcome: run.status,
        code: descriptor.code,
        category: descriptor.category
      })
    }
    void this.drainSessionQueueAfterTerminalRun(run).catch((error) => {
      this.logger.error({
        ...context,
        operation: 'session-queue-drain',
        outcome: 'failed',
        code: error instanceof RuntimeDomainError ? error.code : 'session_queue_drain_failed',
        category: 'internal'
      })
    })
  }

  private async drainSessionQueueAfterTerminalRun(run: AIRuntimeRun) {
    const session = await this.store.getSession(run.workspace_id, run.session_id)
    if (!session || session.status !== 'active') return
    const actor: RuntimeActor = {
      user_id: session.user_id,
      username: 'queue-worker',
      system_role: 'user',
      workspace_id: session.workspace_id,
      workspace_role: 'developer',
      auth_session_id: session.auth_session_id
    }
    let guard = 0
    while (guard < SESSION_QUEUE_ACTIVE_ITEM_LIMIT) {
      guard += 1
      const result = await this.processSessionQueue(actor, session.id, {
        safe_checkpoint: true,
        runtime_run_id: run.runtime_run_id,
        claimed_by: this.runtimeInstanceID()
      })
      if (result.applied + result.consumed + result.failed + result.cancelled_runs === 0) break
      if (result.cancelled_runs > 0 && result.consumed === 0) break
      const active = await this.store.findActiveRun(session.workspace_id, session.id)
      if (active && ACTIVE_RUN_STATUSES.has(active.status) && result.consumed === 0 && result.applied === 0) break
    }
  }

  private leaseExpiry() {
    return new Date(Date.now() + this.runLeaseMs()).toISOString()
  }

  private async runPiHarnessWithLease(run: AIRuntimeRun, input: AgentHarnessRunInput) {
    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Pi runtime engine is not configured', 500)
    }
    const instanceID = this.runtimeInstanceID()
    const claimed = await this.store.claimRunLease(run.workspace_id, run.runtime_run_id, instanceID, this.leaseExpiry())
    if (!claimed) {
      throw new RuntimeDomainError('runtime_run_owned_elsewhere', `Runtime run ${run.runtime_run_id} is owned by another Runtime instance`, 409)
    }
    const ownerEpoch = Number(claimed.owner_epoch || 0)
    const workspaceLease = await this.agentRuntimeEngine.workspace?.acquire(run)
    let stopped = false
    let wakeMonitor: () => void = () => {}
    const monitor = (async () => {
      const intervalMs = Math.max(500, Math.floor(this.runLeaseMs() / 3))
      while (!stopped) {
        await new Promise<void>((resolve) => {
          const timer = setTimeout(resolve, intervalMs)
          wakeMonitor = () => {
            clearTimeout(timer)
            resolve()
          }
        })
        if (stopped) break
        const durableRun = await this.store.getRun(run.workspace_id, run.runtime_run_id)
        if (!durableRun || isTerminalRunStatus(durableRun.status)) {
          this.logger.info({
            component: 'session-service',
            operation: 'lease-monitor-abort',
            outcome: 'aborted',
            workspace_id: run.workspace_id,
            session_id: run.session_id,
            runtime_run_id: run.runtime_run_id,
            reason: !durableRun ? 'run_missing' : 'run_already_terminal',
            durable_status: durableRun?.status,
            code: 'runtime_cancelled',
            category: 'cancelled'
          })
          await this.agentRuntimeEngine?.runner.abort?.(run.runtime_run_id)
          break
        }
        const renewed = await this.store.renewRunLease(run.workspace_id, run.runtime_run_id, instanceID, ownerEpoch, this.leaseExpiry())
        const workspaceRenewed = workspaceLease ? await workspaceLease.renew() : true
        if (!renewed || !workspaceRenewed) {
          this.logger.warn({
            component: 'session-service',
            operation: 'lease-monitor-abort',
            outcome: 'aborted',
            workspace_id: run.workspace_id,
            session_id: run.session_id,
            runtime_run_id: run.runtime_run_id,
            reason: !renewed ? 'lease_renew_failed' : 'workspace_renew_failed',
            owner_instance_id: instanceID,
            owner_epoch: ownerEpoch,
            code: 'runtime_cancelled',
            category: 'cancelled'
          })
          await this.agentRuntimeEngine?.runner.abort?.(run.runtime_run_id)
          break
        }
      }
    })()
    try {
      const parentRuntimeRunID = firstString(run.request.parent_runtime_run_id)
      const session = parentRuntimeRunID ? undefined : await this.store.getSession(run.workspace_id, run.session_id)
      const queueActor: RuntimeActor | undefined = session
        ? {
            user_id: session.user_id,
            username: 'queue-worker',
            system_role: 'user',
            workspace_id: session.workspace_id,
            workspace_role: 'developer',
            auth_session_id: session.auth_session_id
          }
        : undefined
      const onSafeCheckpoint: AgentHarnessRunInput['onSafeCheckpoint'] = queueActor && session
        ? async () => {
            await this.processSessionQueue(queueActor, session.id, {
              safe_checkpoint: true,
              runtime_run_id: run.runtime_run_id,
              claimed_by: instanceID
            })
          }
        : undefined
      const runtimeSessionID = firstString(run.request.runtime_session_id, `s_w${run.workspace_id}_${run.session_id}`)
      const recordEvent: AgentHarnessRunInput['recordEvent'] = async (event) => {
        await this.agentRuntimeEngine?.eventStore.append({
          ...event,
          session_id: runtimeSessionID,
          runtime_run_id: run.runtime_run_id,
          event_id: '',
          seq: 0,
          timestamp: now()
        } as AgentRuntimeEvent)
        if (event.type === 'permission.asked') {
          await this.agentRuntimeEngine?.runner.pauseForApproval?.(run.runtime_run_id, {
            approvalID: firstString(event.approval_id, event.request_id),
            callID: firstString(event.call_id),
            toolName: firstString(event.tool_name),
            reason: firstString(event.reason, event.message, 'Tool execution requires approval')
          })
        }
      }
      const decideTool: AgentHarnessRunInput['decideTool'] = session
        ? async ({ toolName, permissionKey }) => {
            const grant = await this.findMatchingPiSessionGrant({
              actor: {
                user_id: session.user_id,
                username: 'workspace-tool',
                system_role: 'user',
                workspace_id: session.workspace_id,
                workspace_role: 'developer',
                auth_session_id: session.auth_session_id
              },
              session,
              toolName,
              permissionKey,
              executorType: 'workspace',
              resourceType: 'workspace_tool',
              resourceID: toolName
            })
            if (!grant) return undefined
            await this.agentRuntimeEngine?.eventStore.append({
              type: 'approval.session_granted',
              session_id: runtimeSessionID,
              runtime_run_id: run.runtime_run_id,
              event_id: '',
              seq: 0,
              timestamp: now(),
              grant_id: grant.grant_id,
              permission_key: grant.permission_key,
              tool_name: grant.tool_name,
              resource_type: grant.resource_type,
              resource_id: grant.resource_id,
              expires_at: grant.expires_at
            } as unknown as AgentRuntimeEvent)
            return { approved: true, source: 'session_grant', reason: 'Allowed by active Session grant' }
          }
        : undefined
      return await this.agentRuntimeEngine.runner.prompt({
        ...input,
        runMode: subagentModeForRun(run),
        toolPolicy: asRecord(asRecord(run.profile_snapshot).tool_policy),
        actorRole: queueActor?.workspace_role || firstString(asRecord(run.request).workspace_role, 'developer'),
        recordEvent,
        decideTool,
        requestID: firstString(run.request.request_id),
        workspaceID: run.workspace_id,
        parentRuntimeRunID,
        onSafeCheckpoint,
        ...(workspaceLease ? { executionEnv: workspaceLease.executionEnv } : {})
      })
    } finally {
      stopped = true
      wakeMonitor()
      await monitor
      await this.store.releaseRunLease(run.workspace_id, run.runtime_run_id, instanceID, ownerEpoch)
      await workspaceLease?.release()
    }
  }

  async reconcileExpiredRunOwners(force = false) {
    const currentTime = Date.now()
    const throttleMs = Math.max(100, Number(this.options.runStreamPollMs) || 500)
    if (!force && currentTime - this.lastOrphanReconcileAt < throttleMs) return []
    this.lastOrphanReconcileAt = currentTime
    const interruptedRuns = await this.store.interruptExpiredRunLeases(new Date(currentTime).toISOString())
    for (const run of interruptedRuns) {
      const runtimeEvents = await this.ensureRunStateEvent(run)
      const entries = await this.store.listEntries(run.workspace_id, run.session_id)
      const assistantEntry = run.output_entry_id
        ? entries.find((entry) => entry.id === run.output_entry_id)
        : entries.find((entry) => entry.runtime_run_id === run.runtime_run_id && entry.role === 'assistant')
      if (assistantEntry && !['completed', 'failed', 'cancelled'].includes(assistantEntry.status)) {
        const message = '运行时实例中断，本次生成已停止。'
        await this.store.saveEntry({
          ...assistantEntry,
          entry_type: 'error',
          status: 'failed',
          content: assistantEntry.content || message,
          content_blocks: assistantEntry.content_blocks.length > 0 ? assistantEntry.content_blocks : [{ type: 'text', text: message }],
          output: { ...assistantEntry.output, runtime_events: summarizeRuntimeEventsForEntry(runtimeEvents) },
          updated_at: run.updated_at
        })
      }
      await this.store.saveRun({
        ...run,
        result: { ...run.result, runtime_events: summarizeRuntimeEventsForEntry(runtimeEvents) }
      })
    }
    return interruptedRuns
  }

  private async withPiApprovalLock<T>(actor: RuntimeActor, runtimeRunID: string, operation: () => Promise<T>) {
    const lockKey = `${actor.workspace_id}:${runtimeRunID}`
    const previous = this.piApprovalLocks.get(lockKey) || Promise.resolve()
    let release: () => void = () => {}
    const current = new Promise<void>((resolve) => {
      release = resolve
    })
    const chained = previous.catch(() => undefined).then(() => current)
    this.piApprovalLocks.set(lockKey, chained)
    await previous.catch(() => undefined)
    let claimedOwner: { instanceID: string, ownerEpoch: number } | undefined
    try {
      const run = await this.store.getRun(actor.workspace_id, runtimeRunID)
      if (run && ACTIVE_RUN_STATUSES.has(run.status)) {
        const instanceID = this.runtimeInstanceID()
        const claimed = await this.store.claimRunLease(actor.workspace_id, runtimeRunID, instanceID, this.leaseExpiry())
        if (!claimed) {
          throw new RuntimeDomainError('runtime_run_owned_elsewhere', `Runtime run ${runtimeRunID} is owned by another Runtime instance`, 409)
        }
        claimedOwner = { instanceID, ownerEpoch: Number(claimed.owner_epoch || 0) }
      }
      return await operation()
    } finally {
      if (claimedOwner) {
        await this.store.releaseRunLease(actor.workspace_id, runtimeRunID, claimedOwner.instanceID, claimedOwner.ownerEpoch)
      }
      release()
      if (this.piApprovalLocks.get(lockKey) === chained) {
        this.piApprovalLocks.delete(lockKey)
      }
    }
  }

  private providerEmptyOutputDetails(source: ChatModelResult | unknown, attempt: number) {
    if (source instanceof ModelProviderError) {
      return {
        attempt,
        code: source.code,
        status: source.status,
        message: source.message,
        details: source.details
      }
    }
    const completion = source as ChatModelResult | null | undefined
    const parts = Array.isArray(completion?.parts) ? completion.parts : []
    return {
      attempt,
      text_length: typeof completion?.text === 'string' ? completion.text.trim().length : 0,
      tool_calls_count: Array.isArray(completion?.tool_calls) ? completion.tool_calls.length : 0,
      parts_count: parts.length,
      reasoning_parts_count: parts.filter((part) => part.type === 'reasoning').length,
      reasoning_length: parts
        .filter((part) => part.type === 'reasoning')
        .reduce((total, part) => total + part.text.length, 0),
      finish_reason: firstString(completion?.finish?.reason),
      raw_keys: Object.keys(asRecord(completion?.raw)).slice(0, 24)
    }
  }

  private providerContinuationRequest(request: ChatModelRequest, details: Record<string, unknown>): ChatModelRequest {
    return {
      ...request,
      content: [
        'provider_empty_output_detected:',
        JSON.stringify(details),
        '',
        'Continue the same user request now. Return assistant-visible text or tool_calls. Do not return reasoning-only output.',
        '',
        'Original request content:',
        request.content
      ].join('\n'),
      developer_instructions: [
        request.developer_instructions,
        'The previous provider turn produced no assistant-visible text or tool calls. Produce the final answer or explicit tool_calls in this continuation. Do not answer with reasoning-only content.'
      ].filter(Boolean).join('\n\n')
    }
  }

  private async completeWithBoundedContinuation(
    request: ChatModelRequest,
    options: ModelCallEventOptions = { phase: 'initial', round: 1 }
  ): Promise<{ completion: ChatModelResult, events: RuntimeEventRecord[] }> {
    let firstEmpty: Record<string, unknown> | null = null
    const startedEvent: RuntimeEventRecord = {
      type: 'model.call_started',
      payload: modelRequestEventPayload(request, options)
    }
    try {
      const completion = await this.chatModelClient.complete(request)
      if (completion && hasUsableModelOutput(completion)) {
        return {
          completion,
          events: [
            startedEvent,
            {
              type: 'model.call_completed',
              payload: modelCompletionEventPayload(completion, options)
            }
          ]
        }
      }
      firstEmpty = this.providerEmptyOutputDetails(completion, 1)
    } catch (error) {
      if (!(error instanceof ModelProviderError) || error.code !== 'provider_response_empty') {
        throw error
      }
      firstEmpty = this.providerEmptyOutputDetails(error, 1)
    }

    const emptyEvent: RuntimeEventRecord = {
      type: 'provider.empty_output_detected',
      payload: {
        status: 'retrying',
        details: firstEmpty
      }
    }
    const continuationRequest = this.providerContinuationRequest(request, firstEmpty || {})
    const continuationOptions: ModelCallEventOptions = {
      phase: 'provider_continuation',
      round: (options.round || 1) + 1
    }
    try {
      const continuation = await this.chatModelClient.complete(continuationRequest)
      if (continuation && hasUsableModelOutput(continuation)) {
        return {
          completion: {
            ...continuation,
            raw: {
              ...(continuation.raw || {}),
              bounded_continuation: true
            }
          },
          events: [
            startedEvent,
            {
              type: 'model.call_completed',
              payload: modelCompletionEventPayload(null, options)
            },
            emptyEvent,
            {
              type: 'model.request_prepared',
              payload: modelRequestEventPayload(continuationRequest, continuationOptions)
            },
            {
              type: 'model.call_started',
              payload: modelRequestEventPayload(continuationRequest, continuationOptions)
            },
            {
              type: 'model.call_completed',
              payload: modelCompletionEventPayload(continuation, continuationOptions)
            },
            {
              type: 'provider.continuation_completed',
              payload: {
                status: 'completed',
                attempt: 2
              }
            }
          ]
        }
      }
      const secondEmpty = this.providerEmptyOutputDetails(continuation, 2)
      throw new ModelProviderError(
        'provider_response_empty',
        'Provider response did not include assistant text, refusal, warning, or tool calls',
        502,
        {
          first_attempt: firstEmpty,
          continuation_attempt: secondEmpty
        }
      )
    } catch (error) {
      if (error instanceof ModelProviderError && error.code === 'provider_response_empty') {
        if (!('first_attempt' in error.details)) {
          throw new ModelProviderError(
            error.code,
            error.message,
            error.status,
            {
              first_attempt: firstEmpty,
              continuation_attempt: this.providerEmptyOutputDetails(error, 2)
            }
          )
        }
      }
      throw error
    }
  }

  private async *streamBoundedProviderContinuation(
    request: ChatModelRequest,
    options: ModelCallEventOptions = { phase: 'fallback', round: 2 }
  ): AsyncGenerator<RuntimeEventRecord, ChatModelResult, void> {
    const firstCompletion = await this.chatModelClient.complete(request).catch((error) => {
      if (error instanceof ModelProviderError && error.code === 'provider_response_empty') return error
      throw error
    })
    if (!(firstCompletion instanceof ModelProviderError) && firstCompletion && hasUsableModelOutput(firstCompletion)) {
      yield {
        type: 'model.call_started',
        payload: modelRequestEventPayload(request, options)
      }
      yield {
        type: 'model.call_completed',
        payload: modelCompletionEventPayload(firstCompletion, options)
      }
      return firstCompletion
    }

    const firstEmpty = this.providerEmptyOutputDetails(firstCompletion, 1)
    yield {
      type: 'model.call_started',
      payload: modelRequestEventPayload(request, options)
    }
    yield {
      type: 'model.call_completed',
      payload: modelCompletionEventPayload(null, options)
    }
    yield {
      type: 'provider.empty_output_detected',
      payload: {
        status: 'retrying',
        details: firstEmpty
      }
    }

    const continuationRequest = this.providerContinuationRequest(request, firstEmpty)
    const continuationOptions: ModelCallEventOptions = {
      phase: 'provider_continuation',
      round: (options.round || 1) + 1
    }
    yield {
      type: 'model.request_prepared',
      payload: modelRequestEventPayload(continuationRequest, continuationOptions)
    }
    yield {
      type: 'model.call_started',
      payload: modelRequestEventPayload(continuationRequest, continuationOptions)
    }

    const continuation = await this.chatModelClient.complete(continuationRequest).catch((error) => {
      if (error instanceof ModelProviderError && error.code === 'provider_response_empty') {
        throw new ModelProviderError(
          error.code,
          error.message,
          error.status,
          {
            first_attempt: firstEmpty,
            continuation_attempt: this.providerEmptyOutputDetails(error, 2)
          }
        )
      }
      throw error
    })
    if (!continuation || !hasUsableModelOutput(continuation)) {
      throw new ModelProviderError(
        'provider_response_empty',
        'Provider response did not include assistant text, refusal, warning, or tool calls',
        502,
        {
          first_attempt: firstEmpty,
          continuation_attempt: this.providerEmptyOutputDetails(continuation, 2)
        }
      )
    }

    const completed: ChatModelResult = {
      ...continuation,
      raw: {
        ...(continuation.raw || {}),
        bounded_continuation: true
      }
    }
    yield {
      type: 'model.call_completed',
      payload: modelCompletionEventPayload(completed, continuationOptions)
    }
    yield {
      type: 'provider.continuation_completed',
      payload: {
        status: 'completed',
        attempt: 2
      }
    }
    return completed
  }

  async getCurrentSession(actor: RuntimeActor, payload: Record<string, unknown>) {
    const key = currentSessionKey(actor, payload)
    const existing = await this.store.findCurrentSession(key)
    if (existing && existing.status === 'active') {
      return existing
    }
    const resolvedProfile = await this.resolveProfileForSession(actor, payload)
    const agentWorkspace = await this.agentRuntimeEngine?.workspace?.ensureDefault(actor)
    const timestamp = now()
    const profileSelection = asRecord(payload.profile_selection)
    const firstSessionTimestamp = firstString(profileSelection.first_session_timestamp, payload.first_session_timestamp, timestamp)
    const explicitTitle = asString(payload.title).trim()
    const session: AISession = {
      id: await this.store.nextSessionId(),
      workspace_id: actor.workspace_id,
      context_tags: [...resolvedProfile.profile.context_tags],
      session_kind: sessionKind(payload.session_kind),
      user_id: actor.user_id,
      auth_session_id: actor.auth_session_id,
      business_type: asString(payload.business_type),
      business_id: asString(payload.business_id),
      status: 'active',
      agent_profile_id: resolvedProfile.profile.id,
      agent_profile_version_id: resolvedProfile.profileVersionID,
      agent_profile_version_key: resolvedProfile.profileVersionKey,
      agent_profile_snapshot_hash: resolvedProfile.snapshotHash,
      agent_workspace_runtime_id: agentWorkspace?.workspace_runtime_id,
      agent_workspace_snapshot_hash: agentWorkspace?.snapshot_hash,
      session_key: key,
      first_session_timestamp: firstSessionTimestamp,
      source: asString(payload.source) || (asString(payload.business_type) === 'agent_profile' ? 'agent_chatbox' : ''),
      title: explicitTitle || fallbackSessionTitle(firstSessionTimestamp),
      title_source: explicitTitle ? 'manual' : 'fallback',
      entry_count: 0,
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveSession(session)
    await this.store.rememberCurrentSession(key, session.id)
    return session
  }

  private async resolveProfileForSession(actor: RuntimeActor, payload: Record<string, unknown>): Promise<ResolvedSessionProfile> {
    const profileSelection = asRecord(payload.profile_selection)
    const selectedProfileID = Number(firstString(
      profileSelection.profile_id,
      profileSelection.agent_profile_id,
      payload.profile_id,
      payload.agent_profile_id
    ) || 0)
    const selectedProfileVersion =
      profileSelection.profile_version_id ??
      profileSelection.agent_profile_version_id ??
      payload.profile_version_id ??
      payload.agent_profile_version_id ??
      'latest'
    if (selectedProfileID > 0 && isDraftProfileSelection(selectedProfileVersion)) {
      const compiled = await this.profileService.compileDraftProfileSnapshot(actor, selectedProfileID)
      const snapshot = compiled.snapshot
      assertSnapshotCallable(snapshot)
      return {
        profile: snapshot,
        profileVersionID: 0,
        profileVersionKey: 'draft',
        snapshotHash: compiled.snapshot_hash
      }
    }
    const selectedProfileVersionID = Number(selectedProfileVersion || 0)
    const profileName = firstString(payload.profile_name)
    const version = selectedProfileID > 0 && isLatestProfileSelection(selectedProfileVersion)
      ? await this.profileService.getLatestPublishedVersion(actor, selectedProfileID)
      : selectedProfileVersionID > 0
        ? await this.profileService.getVersion(actor, selectedProfileVersionID)
      : profileName
        ? await this.profileService.getPublishedVersionByName(actor, profileName)
        : (() => {
            throw new RuntimeDomainError('agent_profile_selection_required', 'Agent profile id, version id, or profile name is required')
          })()
    this.profileService.assertVersionCallable(version)
    assertSnapshotCallable(version.snapshot)
    return {
      profile: version.snapshot,
      profileVersionID: version.profile_version_id,
      profileVersionKey: isLatestProfileSelection(selectedProfileVersion) ? 'latest' : profileVersionKey(version.profile_version_id),
      snapshotHash: version.snapshot_hash
    }
  }

  private async resolveProfileForRun(actor: RuntimeActor, session: AISession): Promise<ResolvedSessionProfile> {
    if (session.agent_profile_version_key === 'draft' || session.agent_profile_version_id <= 0) {
      const compiled = await this.profileService.compileDraftProfileSnapshot(actor, session.agent_profile_id)
      const profile = mergeSessionModelOverride(compiled.snapshot, session.model_override)
      assertSnapshotCallable(profile)
      return {
        profile,
        profileVersionID: 0,
        profileVersionKey: 'draft',
        snapshotHash: profileSnapshotHash(profile)
      }
    }
    const version = await this.store.getProfileVersion(actor.workspace_id, session.agent_profile_version_id)
    if (!version) {
      throw new RuntimeDomainError('agent_profile_version_not_found', 'Agent profile version not found', 404)
    }
    this.profileService.assertVersionCallable(version)
    const profile = mergeSessionModelOverride(version.snapshot, session.model_override)
    assertSnapshotCallable(profile)
    return {
      profile,
      profileVersionID: version.profile_version_id,
      profileVersionKey: session.agent_profile_version_key || profileVersionKey(version.profile_version_id),
      snapshotHash: profileSnapshotHash(profile)
    }
  }

  private async profileSnapshotsForNewRun(actor: RuntimeActor, session: AISession) {
    const resolvedProfile = await this.resolveProfileForRun(actor, session)
    const persistedProfileSnapshot = resolvedProfile.profile
    assertUnresolvedProfileSnapshot(persistedProfileSnapshot)
    const persistedProfileSnapshotHash = profileSnapshotHash(persistedProfileSnapshot)
    const executionProfileSnapshot = await this.resolveProfileSecrets(actor.workspace_id, persistedProfileSnapshot)
    return {
      resolvedProfile: { ...resolvedProfile, snapshotHash: persistedProfileSnapshotHash },
      persistedProfileSnapshot,
      persistedProfileSnapshotHash,
      executionProfileSnapshot
    }
  }

  private effectiveContinuationProfile(run: AIRuntimeRun): AgentProfileSnapshot {
    if (!run.profile_snapshot) {
      throw new RuntimeDomainError('runtime_profile_snapshot_missing', 'Runtime run profile snapshot is missing', 500)
    }
    const profile = run.profile_snapshot
    assertSnapshotCallable(profile)
    return profile
  }

  async assertContinuationRunProfileIntegrity(actor: RuntimeActor, runOrRuntimeID: AIRuntimeRun | string) {
    const run = typeof runOrRuntimeID === 'string'
      ? await this.store.getRun(actor.workspace_id, runOrRuntimeID)
      : runOrRuntimeID
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    if (!run.profile_snapshot || agentProfileSnapshotHash(run.profile_snapshot) !== run.agent_profile_snapshot_hash) {
      throw new RuntimeDomainError(
        'runtime_profile_snapshot_integrity_failed',
        'Runtime run profile snapshot integrity verification failed',
        500
      )
    }
    return run
  }

  private async executionProfileForContinuation(actor: RuntimeActor, run: AIRuntimeRun) {
    return this.resolveProfileSecrets(actor.workspace_id, this.effectiveContinuationProfile(run))
  }

  async updateSessionModelOverride(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)
    if (activeRun && RUNNING_MODEL_SWITCH_STATUSES.has(activeRun.status)) {
      throw new RuntimeDomainError(
        'session_model_switch_running',
        'Session model can only be switched while the agent is stopped or waiting for user action',
        409,
        {
          active_run: {
            runtime_run_id: activeRun.runtime_run_id,
            status: activeRun.status
          }
        }
      )
    }
    const modelOverride = normalizeSessionModelOverride(payload.model_override || payload)
    assertUnresolvedProfileSnapshot(modelOverride)
    const effectiveProfile = mergeSessionModelOverride(
      (await this.resolveProfileForRun(actor, { ...session, model_override: undefined })).profile,
      modelOverride
    )
    const modelConfig = modelSelectionFromRuntimeConfig({
      provider: effectiveProfile.provider,
      model: effectiveProfile.model,
      binding: effectiveProfile.binding,
      credential: effectiveProfile.provider_credential_ref,
      inference: effectiveProfile.inference
    })
    if (!modelConfig?.provider_id || !modelConfig.id) {
      throw new RuntimeDomainError('session_model_selection_invalid', 'Provider and model are required for session model switching', 400)
    }
    const timestamp = now()
    const saved = await this.store.saveSession({
      ...session,
      model_override: modelOverride,
      updated_at: timestamp
    })
    await this.agentRuntimeEngine?.eventStore.append({
      type: 'session.model.switched',
      session_id: sessionBusinessID(actor.workspace_id, session.id),
      event_id: '',
      seq: 0,
      timestamp,
      model: publicPiModelSelection(modelConfig),
      source: 'user'
    } as unknown as AgentRuntimeEvent)
    return {
      session: saved,
      model_config: publicPiModelSelection(modelConfig)
    }
  }

  private enqueueSessionTitleGeneration(
    actor: RuntimeActor,
    session: AISession,
    profile: AgentProfileSnapshot,
    userContent: string,
    existingEntryCount: number
  ) {
    if (existingEntryCount !== 0) return
    if (session.title_source !== 'fallback') return
    const content = userContent.trim()
    if (!content) return
    void this.generateSessionTitle(actor, session, profile, content)
  }

  private async generateSessionTitle(actor: RuntimeActor, session: AISession, profile: AgentProfileSnapshot, userContent: string) {
    try {
      const latest = await this.store.getSession(actor.workspace_id, session.id)
      if (!latest || latest.title_source !== 'fallback') return
      const titleProfile: AgentProfileSnapshot = {
        ...profile,
        inference: {
          ...asRecord(profile.inference),
          max_tokens: 64,
          temperature: 0.2
        },
        output_schema: {}
      }
      const completion = await this.chatModelClient.complete({
        profile: titleProfile,
        session: latest,
        history: [],
        content: sessionTitleRequestContent(userContent),
        context_ref: { purpose: 'session_title_generation' },
        include_current_page: false,
        tools: [],
        capabilities: {},
        loaded_skills: [],
        subagent_results: [],
        output_schema: {},
        developer_instructions: 'Generate only the final session title text. Do not include explanations.'
      })
      const title = sanitizeGeneratedSessionTitle(completion?.text)
      if (!title) {
        await this.recordSessionTitleGenerationError(latest, 'empty_title')
        return
      }
      const current = await this.store.getSession(actor.workspace_id, session.id)
      if (!current || current.title_source !== 'fallback') return
      await this.store.saveSession({
        ...current,
        title,
        title_source: 'generated',
        title_generated_at: now(),
        title_generation_error: undefined,
        updated_at: now()
      })
    } catch (error) {
      const latest = await this.store.getSession(actor.workspace_id, session.id)
      if (!latest || latest.title_source !== 'fallback') return
      const deterministicTitle = deterministicSessionTitleFromUserContent(userContent)
      if (deterministicTitle) {
        await this.store.saveSession({
          ...latest,
          title: deterministicTitle,
          title_source: 'generated',
          title_generated_at: now(),
          title_generation_error: undefined,
          updated_at: now()
        })
        return
      }
      await this.recordSessionTitleGenerationError(latest, error instanceof Error ? error.message : String(error))
    }
  }

  private async recordSessionTitleGenerationError(session: AISession, message: string) {
    const boundedMessage = message.trim().slice(0, 500) || 'session_title_generation_failed'
    await this.store.saveSession({
      ...session,
      title_generation_error: boundedMessage,
      updated_at: now()
    })
  }

  private async retryFailedSessionTitleGeneration(actor: RuntimeActor, session: AISession) {
    if (session.title_source !== 'fallback' || !firstString(session.title_generation_error)) return session
    if (!session.agent_profile_id) return session
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const firstUserEntry = entries.find((entry) => entry.role === 'user' && entry.entry_type === 'message' && firstString(entry.content))
    if (!firstUserEntry) return session
    try {
      const resolvedProfile = await this.resolveProfileForRun(actor, session)
      const profileSnapshot = await this.resolveProfileSecrets(actor.workspace_id, resolvedProfile.profile)
      await this.generateSessionTitle(actor, session, profileSnapshot, firstUserEntry.content)
      return await this.store.getSession(actor.workspace_id, session.id) || session
    } catch {
      return await this.store.getSession(actor.workspace_id, session.id) || session
    }
  }

  private async saveSessionProgress(actor: RuntimeActor, session: AISession, patch: Partial<AISession>) {
    const latest = await this.store.getSession(actor.workspace_id, session.id)
    return this.store.saveSession({
      ...(latest || session),
      ...patch,
      updated_at: patch.updated_at || now()
    })
  }

  async listSessions(actor: RuntimeActor, payload: Record<string, unknown>) {
    const filter = {
      context_tag: firstString(payload.context_tag),
      context_tags: normalizeContextTags(payload.context_tags),
      business_type: asString(payload.business_type),
      business_id: asString(payload.business_id),
      status: asString(payload.status) as AISession['status']
    }
    const sessions = await this.store.listSessions(actor.workspace_id, filter)
    const ownedSessions = sessions.filter((session) => this.isActorOwnedSession(actor, session))
    return Promise.all(ownedSessions.map(async (session) => ({
      ...session,
      active_run: publicActiveRun(await this.store.findActiveRun(actor.workspace_id, session.id))
    })))
  }

  async archiveSession(actor: RuntimeActor, sessionID: number) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)
    if (activeRun && ACTIVE_RUN_STATUSES.has(activeRun.status)) {
      throw new RuntimeDomainError(
        'active_run_conflict',
        `Session already has active runtime run ${activeRun.runtime_run_id} in status ${activeRun.status}`,
        409,
        {
          active_run: {
            runtime_run_id: activeRun.runtime_run_id,
            status: activeRun.status
          }
        }
      )
    }
    const timestamp = now()
    const archived = await this.store.saveSession({
      ...session,
      status: 'archived',
      updated_at: timestamp
    })
    return {
      ...archived,
      active_run: null
    }
  }

  async listEntries(actor: RuntimeActor, sessionID: number) {
    await this.assertSessionReadable(actor, sessionID, 'active_or_inactive')
    const entries = await this.store.listEntries(actor.workspace_id, sessionID)
    const hydrated = await this.hydrateEntryActions(actor.workspace_id, entries)
    const runtimeRunIDs = [...new Set(hydrated.map((entry) => firstString(entry.runtime_run_id)).filter(Boolean))]
    const runs = new Map<string, AIRuntimeRun>()
    await Promise.all(runtimeRunIDs.map(async (runtimeRunID) => {
      const run = await this.store.getRun(actor.workspace_id, runtimeRunID)
      if (run) runs.set(runtimeRunID, run)
    }))
    return hydrated
      .filter((entry) => {
        if (entry.role !== 'assistant' || !entry.runtime_run_id) return true
        const run = runs.get(entry.runtime_run_id)
        if (!run?.output_entry_id) return true
        if (run.output_entry_id === entry.id) return true
        return !(entry.status === 'streaming' && !entry.content.trim())
      })
      .map((entry) => projectEntryForList(entry, entry.runtime_run_id ? runs.get(entry.runtime_run_id) : undefined))
  }

  async enqueueSessionQueueItem(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const mode = sessionQueueMode(payload.mode)
    const content = asString(payload.content).trim()
    if (!content) throw new RuntimeDomainError('session_queue_content_required', 'Queue item content is required')
    const clientItemID = requireClientID('client_item_id', payload.client_item_id)
    const attachments = sessionQueueAttachments(payload.attachments)
    const contextRef = asRecord(payload.context_ref)
    let targetRunID = firstString(payload.target_run_id, payload.runtime_run_id) || undefined
    const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)
    if (mode === 'steer') {
      if (!activeRun || !ACTIVE_RUN_STATUSES.has(activeRun.status)) {
        throw new RuntimeDomainError('active_run_required', 'steer requires an active Run', 409)
      }
      if (activeRun.status === 'awaiting_decision') {
        throw new RuntimeDomainError('run_requires_decision', 'steer is not allowed while the run awaits decision', 409)
      }
      if (activeRun.status === 'awaiting_input') {
        throw new RuntimeDomainError('run_requires_input', 'steer is not allowed while the run awaits input', 409)
      }
      if (targetRunID && targetRunID !== activeRun.runtime_run_id) {
        const targetRun = await this.store.getRun(actor.workspace_id, targetRunID)
        if (!targetRun || targetRun.session_id !== session.id || !ACTIVE_RUN_STATUSES.has(targetRun.status)) {
          throw new RuntimeDomainError('queue_target_not_active', 'Queue target Run is not active', 409)
        }
      } else {
        targetRunID = activeRun.runtime_run_id
      }
    } else if (targetRunID) {
      const targetRun = await this.store.getRun(actor.workspace_id, targetRunID)
      if (!targetRun || targetRun.session_id !== sessionID) {
        throw new RuntimeDomainError('session_queue_target_run_not_found', 'Queue target Run was not found', 404)
      }
    }
    const createdAt = now()
    const runtimeSessionID = sessionBusinessID(actor.workspace_id, sessionID)
    const requestedPayload = { mode, content, attachments, context_ref: contextRef, target_run_id: targetRunID }
    const result = await this.store.enqueueSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: sessionID,
      mode,
      content,
      attachments,
      context_ref: contextRef,
      target_run_id: targetRunID,
      expires_at: mode === 'steer' ? new Date(Date.now() + SESSION_QUEUE_STEER_TTL_MS).toISOString() : undefined,
      client_item_id: clientItemID,
      created_by: actor.user_id,
      created_at: createdAt,
      lifecycle_event: { type: 'session.queue.added', session_id: runtimeSessionID, marker: 'created' }
    }, SESSION_QUEUE_ACTIVE_ITEM_LIMIT)
    if (result.outcome === 'capacity_exceeded') {
      throw new RuntimeDomainError(
        'session_queue_full',
        `Session queue already has ${SESSION_QUEUE_ACTIVE_ITEM_LIMIT} active items`,
        409,
        { limit: SESSION_QUEUE_ACTIVE_ITEM_LIMIT }
      )
    }
    if (
      result.outcome === 'existing' &&
      stableJSONStringify(sessionQueueItemPayload(result.item)) !== stableJSONStringify(sessionQueueItemPayload(requestedPayload))
    ) {
      throw new RuntimeDomainError(
        'session_queue_idempotency_conflict',
        'client_item_id was already used for a different queue item',
        409,
        { queue_item_id: result.item.queue_item_id, client_item_id: clientItemID }
      )
    }
    const item = result.item
    if (result.outcome === 'created') {
      await this.appendUncommittedQueueEvent(result)
    }
    if (this.agentRuntimeEngine && this.options.autoDrainSessionQueue !== false) {
      await this.drainSessionQueueAfterEnqueue(actor, session.id, item)
    }
    const latestItems = await this.store.listSessionQueueItems(actor.workspace_id, session.id)
    const latestItem = latestItems.find((row) => row.queue_item_id === item.queue_item_id) || item
    return { item: latestItem, idempotent: result.outcome === 'existing' }
  }

  private async drainSessionQueueAfterEnqueue(actor: RuntimeActor, sessionID: number, item: SessionQueueItem) {
    const activeRun = await this.store.findActiveRun(actor.workspace_id, sessionID)
    const canApplySteerNow = Boolean(
      item.mode === 'steer'
      && activeRun
      && ACTIVE_RUN_STATUSES.has(activeRun.status)
      && activeRun.status !== 'awaiting_decision'
      && activeRun.status !== 'awaiting_input'
    )
    if (canApplySteerNow) {
      await this.processSessionQueue(actor, sessionID, {
        claimed_by: this.runtimeInstanceID(),
        safe_checkpoint: true,
        runtime_run_id: activeRun?.runtime_run_id
      })
      return
    }
    this.scheduleSessionQueueDrain(actor, sessionID)
  }

  private scheduleSessionQueueDrain(actor: RuntimeActor, sessionID: number) {
    void this.processSessionQueue(actor, sessionID, { claimed_by: this.runtimeInstanceID() }).catch((error) => {
      this.logger.error({
        component: 'session-service',
        operation: 'session-queue-drain',
        outcome: 'failed',
        workspace_id: actor.workspace_id,
        session_id: sessionID,
        code: error instanceof RuntimeDomainError ? error.code : 'session_queue_drain_failed',
        category: 'internal'
      })
    })
  }

  async listSessionQueueItems(actor: RuntimeActor, sessionID: number) {
    await this.assertSessionReadable(actor, sessionID, 'active_or_inactive')
    const items = await this.store.listSessionQueueItems(actor.workspace_id, sessionID)
    return {
      items,
      pending_count: items.filter((item) => ['pending', 'claimed'].includes(item.status)).length,
      limit: SESSION_QUEUE_ACTIVE_ITEM_LIMIT
    }
  }

  async cancelSessionQueueItem(actor: RuntimeActor, sessionID: number, queueItemID: string) {
    await this.assertSessionReadable(actor, sessionID, 'active_only')
    const normalizedQueueItemID = queueItemID.trim()
    if (!normalizedQueueItemID) throw new RuntimeDomainError('session_queue_item_id_required', 'queue_item_id is required')
    const result = await this.store.cancelSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: sessionID,
      queue_item_id: normalizedQueueItemID,
      updated_at: now(),
      lifecycle_event: {
        type: 'session.queue.cancelled',
        session_id: sessionBusinessID(actor.workspace_id, sessionID),
        marker: 'cancelled'
      }
    })
    if (result.outcome === 'not_found') {
      throw new RuntimeDomainError('queue_item_not_found', 'Session queue item not found', 404)
    }
    if (result.outcome === 'not_cancellable') {
      throw new RuntimeDomainError('queue_item_not_cancellable', 'Session queue item is no longer pending', 409)
    }
    if (result.outcome === 'cancelled') {
      await this.appendUncommittedQueueEvent(result)
    }
    return { item: result.item, idempotent: result.outcome === 'already_cancelled' }
  }

  async reorderSessionQueueItems(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>) {
    await this.assertSessionReadable(actor, sessionID, 'active_only')
    if (!Array.isArray(payload.queue_item_ids)) {
      throw new RuntimeDomainError('session_queue_order_invalid', 'queue_item_ids must be an array')
    }
    const queueItemIDs = payload.queue_item_ids.map((value) => asString(value).trim())
    if (queueItemIDs.some((value) => !value) || queueItemIDs.length > SESSION_QUEUE_ACTIVE_ITEM_LIMIT) {
      throw new RuntimeDomainError('session_queue_order_invalid', 'queue_item_ids contains an invalid queue item')
    }
    const result = await this.store.reorderSessionQueueItems(actor.workspace_id, sessionID, queueItemIDs, now())
    if (result.outcome === 'conflict') {
      throw new RuntimeDomainError(
        'session_queue_reorder_conflict',
        'Queue order no longer matches the current pending item set',
        409
      )
    }
    return { items: result.items }
  }

  async processSessionQueue(
    actor: RuntimeActor,
    sessionID: number,
    options: Record<string, unknown> | { safe_checkpoint?: boolean, runtime_run_id?: string, claimed_by?: string } = {}
  ) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const optionRecord = asRecord(options)
    const safeCheckpoint = Boolean(optionRecord.safe_checkpoint)
    const optionRuntimeRunID = firstString(optionRecord.runtime_run_id)
    const claimedBy = firstString(optionRecord.claimed_by, this.runtimeInstanceID(), 'queue-worker')
    const timestamp = now()
    const summary = { applied: 0, consumed: 0, failed: 0, cancelled_runs: 0, skipped: 0 }
    const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)

    if (activeRun && safeCheckpoint) {
      const steerClaim = await this.store.claimNextSessionQueueItem({
        workspace_id: actor.workspace_id,
        session_id: session.id,
        claimed_by: claimedBy,
        claim_expires_at: new Date(Date.now() + SESSION_QUEUE_CLAIM_TTL_MS).toISOString(),
        updated_at: timestamp,
        queue_item_id: undefined,
        lifecycle_event: {
          type: 'session.queue.claimed',
          session_id: sessionBusinessID(actor.workspace_id, session.id)
        }
      })
      if (steerClaim.outcome === 'claimed') {
        await this.appendUncommittedQueueEvent(steerClaim)
      }
      if (steerClaim.outcome === 'claimed' && steerClaim.item.mode === 'steer') {
        const applied = await this.applyQueueSteerItem(actor, session, activeRun, steerClaim.item, {
          safeCheckpoint,
          optionRuntimeRunID,
          claimedBy,
          timestamp
        })
        if (applied.outcome === 'applied') {
          summary.applied += 1
          return summary
        }
        if (applied.outcome === 'failed') {
          summary.failed += 1
          return summary
        }
        summary.skipped += 1
        return summary
      }
      if (steerClaim.outcome === 'claimed') {
        await this.store.updateSessionQueueItem({
          ...steerClaim.item,
          status: 'pending',
          claimed_by: undefined,
          claim_expires_at: undefined,
          updated_at: timestamp
        })
      }
    }

    const claim = await this.store.claimNextSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      claimed_by: claimedBy,
      claim_expires_at: new Date(Date.now() + SESSION_QUEUE_CLAIM_TTL_MS).toISOString(),
      updated_at: timestamp,
      lifecycle_event: {
        type: 'session.queue.claimed',
        session_id: sessionBusinessID(actor.workspace_id, session.id)
      }
    })
    if (claim.outcome === 'empty') return summary
    const item = claim.item
    await this.appendUncommittedQueueEvent(claim)

    if (item.mode === 'steer') {
      const applied = await this.applyQueueSteerItem(actor, session, activeRun, item, {
        safeCheckpoint,
        optionRuntimeRunID,
        claimedBy,
        timestamp
      })
      if (applied.outcome === 'applied') summary.applied += 1
      else if (applied.outcome === 'failed') summary.failed += 1
      else summary.skipped += 1
      return summary
    }

    if (item.mode === 'follow_up') {
      if (activeRun && ACTIVE_RUN_STATUSES.has(activeRun.status)) {
        await this.store.updateSessionQueueItem({
          ...item,
          status: 'pending',
          claimed_by: undefined,
          claim_expires_at: undefined,
          updated_at: timestamp
        })
        summary.skipped += 1
        return summary
      }
      const started = await this.startQueuedFollowUpRun(actor, session, item)
      if (started.queue_start_outcome === 'consumed') {
        summary.consumed += 1
        return summary
      }
      if (started.queue_start_outcome === 'already_consumed') return summary
      if (started.queue_start_outcome === 'stale_claim' || started.queue_start_outcome === 'active_run_conflict') {
        summary.failed += 1
        return summary
      }
      const consumed = await this.store.consumeSessionQueueItem({
        workspace_id: actor.workspace_id,
        session_id: session.id,
        queue_item_id: item.queue_item_id,
        claimed_by: item.claimed_by || claimedBy,
        claim_epoch: item.claim_epoch,
        consumed_runtime_run_id: started.runtime_run_id,
        updated_at: now()
      })
      if (consumed.outcome === 'consumed' || consumed.outcome === 'already_consumed') {
        summary.consumed += 1
        await this.appendSessionQueueEvent({
          type: 'session.follow_up.started',
          item: consumed.item,
          marker: started.runtime_run_id,
          runtime_run_id: started.runtime_run_id,
          consumed_runtime_run_id: started.runtime_run_id
        })
      } else summary.failed += 1
      return summary
    }

    if (item.mode === 'stop_and_run') {
      const currentActive = activeRun || await this.store.findActiveRun(actor.workspace_id, session.id)
      if (currentActive && ACTIVE_RUN_STATUSES.has(currentActive.status)) {
        await this.cancelSessionRun(actor, session.id, {
          runtime_run_id: currentActive.runtime_run_id,
          reason: 'stop_and_run queue item'
        })
        summary.cancelled_runs += 1
        await this.store.updateSessionQueueItem({
          ...item,
          status: 'pending',
          claimed_by: undefined,
          claim_expires_at: undefined,
          updated_at: now()
        })
        return summary
      }
      const started = await this.startQueuedFollowUpRun(actor, session, item)
      if (started.queue_start_outcome === 'consumed') {
        summary.consumed += 1
        return summary
      }
      if (started.queue_start_outcome === 'already_consumed') return summary
      if (started.queue_start_outcome === 'stale_claim' || started.queue_start_outcome === 'active_run_conflict') {
        summary.failed += 1
        return summary
      }
      const consumed = await this.store.consumeSessionQueueItem({
        workspace_id: actor.workspace_id,
        session_id: session.id,
        queue_item_id: item.queue_item_id,
        claimed_by: item.claimed_by || claimedBy,
        claim_epoch: item.claim_epoch,
        consumed_runtime_run_id: started.runtime_run_id,
        updated_at: now()
      })
      if (consumed.outcome === 'consumed' || consumed.outcome === 'already_consumed') {
        summary.consumed += 1
        await this.appendSessionQueueEvent({
          type: 'session.follow_up.started',
          item: consumed.item,
          marker: started.runtime_run_id,
          runtime_run_id: started.runtime_run_id,
          consumed_runtime_run_id: started.runtime_run_id
        })
      } else summary.failed += 1
      return summary
    }

    const failedItem = {
      ...item,
      status: 'failed',
      error_code: 'queue_mode_invalid',
      error_msg: `Unsupported queue mode ${item.mode}`,
      updated_at: timestamp
    } satisfies SessionQueueItem
    await this.commitClaimedQueueLifecycle(failedItem, item, {
      type: 'session.queue.failed',
      session_id: sessionBusinessID(actor.workspace_id, session.id),
      marker: failedItem.error_code || 'failed'
    })
    summary.failed += 1
    return summary
  }

  private async applyQueueSteerItem(
    actor: RuntimeActor,
    session: AISession,
    activeRun: AIRuntimeRun | undefined,
    item: SessionQueueItem,
    options: {
      safeCheckpoint: boolean
      optionRuntimeRunID: string
      claimedBy: string
      timestamp: string
    }
  ): Promise<{ outcome: 'applied' | 'failed' | 'skipped' }> {
    if (item.expires_at && Date.parse(item.expires_at) <= Date.now()) {
      const expiredItem = {
        ...item,
        status: 'expired',
        error_code: 'steer_checkpoint_timeout',
        error_msg: 'steer expired before a safe checkpoint',
        updated_at: options.timestamp
      } satisfies SessionQueueItem
      await this.commitClaimedQueueLifecycle(expiredItem, item, {
        type: 'session.queue.expired',
        session_id: sessionBusinessID(actor.workspace_id, session.id),
        marker: 'expired'
      })
      return { outcome: 'failed' }
    }
    if (!options.safeCheckpoint) {
      await this.store.updateSessionQueueItem({
        ...item,
        status: 'pending',
        claimed_by: undefined,
        claim_expires_at: undefined,
        updated_at: options.timestamp
      })
      return { outcome: 'skipped' }
    }
    const targetRunID = firstString(item.target_run_id, options.optionRuntimeRunID, activeRun?.runtime_run_id)
    if (
      !activeRun ||
      targetRunID !== activeRun.runtime_run_id ||
      !ACTIVE_RUN_STATUSES.has(activeRun.status) ||
      activeRun.status === 'awaiting_decision' ||
      activeRun.status === 'awaiting_input'
    ) {
      await this.store.updateSessionQueueItem({
        ...item,
        status: 'pending',
        claimed_by: undefined,
        claim_expires_at: undefined,
        error_code: undefined,
        error_msg: undefined,
        updated_at: options.timestamp
      })
      return { outcome: 'skipped' }
    }

    const runResult = this.queueSteerAppliedResult(activeRun, item, options.timestamp)
    const applied = await this.store.applySessionQueueSteer({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: item.queue_item_id,
      claimed_by: item.claimed_by || options.claimedBy,
      claim_epoch: item.claim_epoch,
      runtime_run_id: activeRun.runtime_run_id,
      run_result: runResult,
      updated_at: options.timestamp,
      lifecycle_event: {
        type: 'session.steer.applied',
        session_id: sessionBusinessID(actor.workspace_id, session.id),
        marker: 'applied',
        runtime_run_id: activeRun.runtime_run_id
      }
    })
    if (applied.outcome === 'applied' || applied.outcome === 'already_applied') {
      if (applied.outcome === 'applied') {
        await this.appendUncommittedQueueEvent(applied)
        await this.agentRuntimeEngine?.runner.steer?.(
          activeRun.runtime_run_id,
          item.queue_item_id,
          item.content
        )
      }
      return { outcome: 'applied' }
    }
    if (applied.outcome === 'target_not_active') {
      const failedItem = {
        ...item,
        status: 'failed',
        claimed_by: undefined,
        claim_expires_at: undefined,
        error_code: 'queue_target_not_active',
        error_msg: 'steer target Run is no longer active',
        updated_at: options.timestamp
      } satisfies SessionQueueItem
      await this.commitClaimedQueueLifecycle(failedItem, item, {
        type: 'session.queue.failed',
        session_id: sessionBusinessID(actor.workspace_id, session.id),
        marker: failedItem.error_code || 'failed'
      })
      return { outcome: 'failed' }
    }
    return { outcome: 'skipped' }
  }

  private async commitClaimedQueueLifecycle(
    item: SessionQueueItem,
    claimedItem: Pick<SessionQueueItem, 'claimed_by' | 'claim_epoch'>,
    lifecycleEvent: SessionQueueEventDraft
  ) {
    const result = await this.store.transitionSessionQueueItem({
      item,
      claimed_by: claimedItem.claimed_by || '',
      claim_epoch: claimedItem.claim_epoch,
      lifecycle_event: lifecycleEvent
    })
    if (result.outcome === 'transitioned') await this.appendUncommittedQueueEvent(result)
    return result
  }

  private queueSteerAppliedResult(run: AIRuntimeRun, item: SessionQueueItem, timestamp: string) {
    const existingSteers = Array.isArray(run.result.queue_steers)
      ? run.result.queue_steers.map((value) => asRecord(value))
      : []
    return {
      ...run.result,
      queue_steers: [
        ...existingSteers,
        {
          queue_item_id: item.queue_item_id,
          content: item.content,
          applied_at: timestamp,
          status: 'applied'
        }
      ],
      last_queue_steer: item.content
    }
  }

  private async startQueuedFollowUpRun(
    actor: RuntimeActor,
    session: AISession,
    item: SessionQueueItem
  ): Promise<{
    runtime_run_id: string
    run: AIRuntimeRun | null
    queue_start_outcome: 'consumed' | 'already_consumed' | 'stale_claim' | 'active_run_conflict' | undefined
  }> {
    const payload: Record<string, unknown> = {
      content: item.content,
      client_entry_id: `queue:${item.client_item_id}`,
      context_ref: item.context_ref,
      queue_item_id: item.queue_item_id,
      queue_mode: item.mode,
      queue_claimed_by: item.claimed_by,
      queue_claim_epoch: item.claim_epoch
    }
    if (this.agentRuntimeEngine) payload.runtime_engine = 'pi'
    const started = await this.createEntryRun(actor, session.id, payload)
    const queueStartOutcome = queuedRunStartOutcome(asRecord(started).queue_start_outcome)
    if (queueStartOutcome === 'stale_claim' || queueStartOutcome === 'active_run_conflict') {
      return { runtime_run_id: '', run: null, queue_start_outcome: queueStartOutcome }
    }
    const runtimeRunID = firstString(started.run?.runtime_run_id)
    if (!runtimeRunID || !started.run) {
      throw new RuntimeDomainError('session_queue_consume_failed', 'Failed to start Run for queued follow_up')
    }
    let durableQueueStartOutcome = queueStartOutcome
    if (!durableQueueStartOutcome && this.agentRuntimeEngine) {
      const durableItem = (await this.store.listSessionQueueItems(actor.workspace_id, session.id))
        .find((candidate) => candidate.queue_item_id === item.queue_item_id)
      if (durableItem?.status === 'consumed' && durableItem.consumed_runtime_run_id === runtimeRunID) {
        durableQueueStartOutcome = 'consumed'
      }
    }
    return {
      runtime_run_id: runtimeRunID,
      run: started.run,
      queue_start_outcome: durableQueueStartOutcome
    }
  }

  private async hydrateEntryActions(workspaceID: number, entries: AISessionEntry[]) {
    const internalIDs = new Set<number>()
    const businessIDs = new Set<string>()
    for (const entry of entries) {
      const actions = Array.isArray(entry.output?.agent_actions) ? entry.output.agent_actions : []
      for (const item of actions) {
        const record = asRecord(item)
        const internalID = Number(record.internal_id || 0)
        if (Number.isFinite(internalID) && internalID > 0) internalIDs.add(internalID)
        const businessID = firstString(record.action_id, record.id)
        if (businessID) businessIDs.add(businessID)
      }
    }
    if (internalIDs.size === 0 && businessIDs.size === 0) return entries

    const liveActions = new Map<number, AgentAction>()
    const liveActionsByBusinessID = new Map<string, AgentAction>()
    await Promise.all([...internalIDs].map(async (id) => {
      const action = await this.store.getAction(workspaceID, id)
      if (action) {
        liveActions.set(id, action)
        liveActionsByBusinessID.set(action.action_id, action)
      }
    }))
    await Promise.all([...businessIDs].map(async (actionID) => {
      const action = await this.store.getActionByBusinessID(workspaceID, actionID)
      if (action) {
        liveActions.set(action.id, action)
        liveActionsByBusinessID.set(action.action_id, action)
      }
    }))
    if (liveActions.size === 0 && liveActionsByBusinessID.size === 0) return entries

    return entries.map((entry) => {
      const actions = Array.isArray(entry.output?.agent_actions) ? entry.output.agent_actions : []
      if (actions.length === 0) return entry
      let changed = false
      const hydratedActions = actions.map((item) => {
        const record = asRecord(item)
        const internalID = Number(record.internal_id || 0)
        const businessID = firstString(record.action_id, record.id)
        const live = liveActions.get(internalID) || liveActionsByBusinessID.get(businessID)
        if (!live) return item
        changed = true
        return { ...record, ...publicAction(live) }
      })
      if (!changed) return entry
      return {
        ...entry,
        output: {
          ...(entry.output || {}),
          agent_actions: hydratedActions
        }
      }
    })
  }

  async getSession(actor: RuntimeActor, sessionID: number) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_or_inactive')
    const current = await this.retryFailedSessionTitleGeneration(actor, session)
    return {
      ...current,
      active_run: publicActiveRun(await this.store.findActiveRun(actor.workspace_id, current.id))
    }
  }

  private sessionBelongsToActor(actor: RuntimeActor, session: AISession) {
    return session.user_id === actor.user_id
  }

  private isActorOwnedSession(actor: RuntimeActor, session: AISession) {
    // Agent Chatbox 会话是用户直接从独立 Chatbox URL 打开的个人会话。即使同一 workspace
    // 内有更高治理角色，也不能通过猜测 URL 读取或继续操作其他用户的 Chatbox 轨迹。
    if (session.source !== 'agent_chatbox') return true
    return this.sessionBelongsToActor(actor, session)
  }

  private async assertSessionReadable(actor: RuntimeActor, sessionID: number, statusMode: 'active_only' | 'active_or_inactive') {
    const session = await this.store.getSession(actor.workspace_id, sessionID)
    if (!session || session.status === 'deleted' || (statusMode === 'active_only' && session.status !== 'active')) {
      throw new RuntimeDomainError('ai_session_not_found', 'AI session not found', 404)
    }
    if (!this.isActorOwnedSession(actor, session)) {
      throw new RuntimeDomainError('ai_session_not_found', 'AI session not found', 404)
    }
    return session
  }

  async assertSessionCanAcceptEntry(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>) {
    await this.assertSessionReadable(actor, sessionID, 'active_only')
    const content = asString(payload.content).trim()
    if (!content) {
      throw new RuntimeDomainError('entry_content_required', 'Entry content is required')
    }
    requireClientID('client_entry_id', payload.client_entry_id)
  }

  private async assertSessionHasNoActiveRun(actor: RuntimeActor, session: AISession) {
    const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)
    if (!activeRun || !ACTIVE_RUN_STATUSES.has(activeRun.status)) return
    const pendingActions = Array.isArray(activeRun.result.agent_actions)
      ? activeRun.result.agent_actions.map((item) => asRecord(item)).filter((item) => asString(item.status) === 'awaiting_decision')
      : []
    const pendingAction = pendingActions[0]
    throw new RuntimeDomainError(
      'active_run_conflict',
      `Session already has active runtime run ${activeRun.runtime_run_id} in status ${activeRun.status}`,
      409,
      {
        active_run: {
          runtime_run_id: activeRun.runtime_run_id,
          status: activeRun.status,
          pending_action_id: pendingAction ? firstString(pendingAction.action_id, pendingAction.id) || undefined : undefined,
          pending_actions: pendingActions.map((action) => ({
            id: firstString(action.action_id, action.id) || undefined,
            action_id: firstString(action.action_id, action.id) || undefined,
            action_kind: asString(action.action_kind),
            title: firstString(asRecord(action.display_json).title, asRecord(action.display_json).name, action.capability_id),
            status: asString(action.status),
            summary: firstString(asRecord(action.display_json).summary, asRecord(action.policy_json).risk_summary)
          })),
          next_steps: activeRun.status === 'awaiting_decision' ? ['approve_once', 'approve_session', 'reject', 'cancel'] : ['wait', 'cancel']
        }
      }
    )
  }

  private async persistRuntimeEvents(
    actor: RuntimeActor,
    session: AISession,
    runtimeRunID: string,
    events: RuntimeEventRecord[]
  ): Promise<RuntimeEventRecord[]> {
    const persisted: RuntimeEventRecord[] = []
    const requestID = firstString((await this.store.getRun(actor.workspace_id, runtimeRunID))?.request.request_id)
    for (const event of events) {
      if (event.persisted) {
        persisted.push(event)
        continue
      }
      const payload = asRecord(event.payload)
      const eventSeq = await this.store.nextRunEventSeq(runtimeRunID)
      const eventID = eventBusinessID(runtimeRunID, eventSeq)
      const actionID = actionBusinessIDFromEventPayload(payload)
      const entryID = Number(payload.entry_id || 0) > 0 ? Number(payload.entry_id) : undefined
      const storedPayload: Record<string, unknown> = {
        ...payload,
        event_id: eventID,
        event_seq: eventSeq,
        runtime_run_id: runtimeRunID,
        session_id: session.id,
        ...(requestID ? { request_id: requestID } : {})
      }
      if (actionID) storedPayload.action_id = actionID
      if (entryID) storedPayload.entry_id = entryID
      const stored = await this.store.appendActionEvent({
        event_id: eventID,
        event_seq: eventSeq,
        workspace_id: actor.workspace_id,
        session_id: session.id,
        runtime_run_id: runtimeRunID,
        action_id: actionID,
        entry_id: entryID,
        event_type: event.type,
        visibility: asString(payload.visibility) === 'internal' ? 'internal' : 'public',
        payload_json: storedPayload,
        display_json: eventDisplayJSON(event.type, storedPayload),
        created_at: now()
      })
      persisted.push({
        type: stored.event_type,
        persisted: true,
        payload: {
          ...stored.payload_json,
          event_id: stored.event_id,
          event_seq: stored.event_seq,
          runtime_run_id: stored.runtime_run_id,
          session_id: stored.session_id,
          action_id: stored.action_id,
          entry_id: stored.entry_id,
          display_json: stored.display_json,
          created_at: stored.created_at
        }
      })
    }
    return persisted
  }

  async listRunEvents(actor: RuntimeActor, runtimeRunID: string, afterEventID = '') {
    return this.listRunEventsScoped(actor, runtimeRunID, {}, afterEventID)
  }

  async listRunEventsScoped(actor: RuntimeActor, runtimeRunID: string, payload: Record<string, unknown> = {}, afterEventID = '') {
    const workspaceID = workspaceIDFromRuntimeRunID(runtimeRunID)
    if (!workspaceID || workspaceID !== actor.workspace_id) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    const run = await this.store.getRun(workspaceID, runtimeRunID)
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    await this.assertSessionReadable(actor, run.session_id, 'active_or_inactive')
    const parentLink = await this.store.getParentRunLink(workspaceID, runtimeRunID)
    if (parentLink) {
      await this.assertChildRunLinkAccess(actor, parentLink, payload, run.session_id, runtimeRunID)
    }
    if (firstString(run.result.runtime_engine, run.request.runtime_engine) === 'pi') {
      await this.ensurePiRunStateEvent(run)
      const runtimeEvents = await this.replayPiRuntimeEventsForRun(run)
      return {
        runtime_run_id: runtimeRunID,
        events: publicPiRuntimeEvents(run, afterEventID, runtimeEvents)
      }
    }
    await this.ensureStoredRunStateEvent(run)
    const events = await this.store.listActionEvents(workspaceID, runtimeRunID, afterEventID)
    return {
      runtime_run_id: runtimeRunID,
      events: events.map(publicActionEvent)
    }
  }

  async listRunEventsByRuntimeID(runtimeRunID: string, afterEventID = '') {
    const workspaceID = workspaceIDFromRuntimeRunID(runtimeRunID)
    if (!workspaceID) {
      throw new RuntimeDomainError('runtime_run_id_invalid', 'Runtime run id is invalid')
    }
    const run = await this.store.getRun(workspaceID, runtimeRunID)
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    if (firstString(run.result.runtime_engine, run.request.runtime_engine) === 'pi') {
      await this.ensurePiRunStateEvent(run)
      const runtimeEvents = await this.replayPiRuntimeEventsForRun(run)
      return {
        runtime_run_id: runtimeRunID,
        events: publicPiRuntimeEvents(run, afterEventID, runtimeEvents)
      }
    }
    await this.ensureStoredRunStateEvent(run)
    const events = await this.store.listActionEvents(workspaceID, runtimeRunID, afterEventID)
    return {
      runtime_run_id: runtimeRunID,
      events: events.map(publicActionEvent)
    }
  }

  async *streamRunEvents(
    actor: RuntimeActor,
    runtimeRunID: string,
    payload: Record<string, unknown> = {}
  ): AsyncIterable<RuntimeStreamEvent> {
    const startedAt = performance.now()
    const workspaceID = workspaceIDFromRuntimeRunID(runtimeRunID)
    if (!workspaceID || workspaceID !== actor.workspace_id) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    const initialRun = await this.store.getRun(workspaceID, runtimeRunID)
    if (!initialRun) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    await this.assertSessionReadable(actor, initialRun.session_id, 'active_or_inactive')
    if (firstString(initialRun.result.runtime_engine, initialRun.request.runtime_engine) !== 'pi') {
      throw new RuntimeDomainError('runtime_event_stream_unsupported', 'Live event resume is only available for Pi runtime runs', 409)
    }
    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Pi runtime engine is not configured', 500)
    }
    const runtimeSessionID = firstString(initialRun.result.runtime_session_id, initialRun.request.runtime_session_id)
    if (!runtimeSessionID) {
      throw new RuntimeDomainError('pi_runtime_session_missing', 'Pi runtime session is missing', 409)
    }
    const afterEventID = firstString(payload.after_event_id)
    const existingEvents = await this.agentRuntimeEngine.eventStore.replayRun(runtimeRunID)
    if (afterEventID && !existingEvents.some((event) => firstString(event.event_id) === afterEventID)) {
      throw new RuntimeDomainError('runtime_event_cursor_not_found', 'Runtime event cursor was not found for this run', 409)
    }

    let notified = false
    let wake: (() => void) | undefined
    const unsubscribe = this.agentRuntimeEngine.eventStore.subscribe(runtimeSessionID, (event) => {
      if (firstString(event.runtime_run_id) !== runtimeRunID) return
      notified = true
      wake?.()
      wake = undefined
    })
    const heartbeatMs = Math.max(1, Number(this.options.runStreamHeartbeatMs) || 10_000)
    const pollMs = Math.max(100, Number(this.options.runStreamPollMs) || 500)
    let lastHeartbeatAt = Date.now()
    let lastEventID = afterEventID
    let initialReplayObserved = false
    try {
      while (true) {
        await this.reconcileExpiredRunOwners()
        const replay = await this.listRunEventsScoped(actor, runtimeRunID, payload, lastEventID)
        for (const event of replay.events) {
          lastEventID = firstString(event.event_id, lastEventID)
          yield this.streamEvent('runtime_event', startedAt, { event })
        }
        if (!initialReplayObserved) {
          initialReplayObserved = true
          if (replay.events.length > 0) {
            this.options.metrics?.observe('ai_runtime_replay_delay_seconds', (performance.now() - startedAt) / 1000, { outcome: 'completed' })
          }
        }

        const currentRun = await this.store.getRun(workspaceID, runtimeRunID)
        if (!currentRun) {
          throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
        }
        if (isTerminalRunStatus(currentRun.status)) {
          const entries = await this.listEntries(actor, currentRun.session_id)
          const assistantEntry = currentRun.output_entry_id
            ? entries.find((entry) => entry.id === currentRun.output_entry_id)
            : undefined
          if (assistantEntry) {
            yield this.streamEvent('assistant_entry', startedAt, {
              entry: assistantEntry,
              run: currentRun,
              last_event_id: lastEventID
            })
          }
          yield this.streamEvent('done', startedAt, {
            runtime_run_id: runtimeRunID,
            status: currentRun.status,
            last_event_id: lastEventID
          })
          return
        }

        if (notified) {
          notified = false
          continue
        }
        const signal = await new Promise<'event' | 'poll'>((resolve) => {
          const timer = setTimeout(() => {
            wake = undefined
            resolve('poll')
          }, pollMs)
          wake = () => {
            clearTimeout(timer)
            resolve('event')
          }
        })
        notified = false
        if (signal === 'poll' && Date.now() - lastHeartbeatAt >= heartbeatMs) {
          lastHeartbeatAt = Date.now()
          yield this.streamEvent('heartbeat', startedAt, {
            runtime_run_id: runtimeRunID,
            status: currentRun.status,
            last_event_id: lastEventID,
            timestamp: now()
          })
        }
      }
    } finally {
      unsubscribe()
    }
  }

  private async replayPiRuntimeEventsForRun(run: AIRuntimeRun): Promise<AgentRuntimeEvent[]> {
    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Pi runtime engine is not configured', 500)
    }
    const replayedEvents = await this.agentRuntimeEngine.eventStore.replayRun(run.runtime_run_id)
    return this.truncatePiRuntimeEventsAtPendingApproval(run, replayedEvents)
  }

  private truncatePiRuntimeEventsAtPendingApproval(run: AIRuntimeRun, events: AgentRuntimeEvent[]) {
    if (run.status !== 'awaiting_decision') return events
    const storedAwaitingApproval = asRecord(run.result.awaiting_approval)
    const storedAwaitingApprovals = normalizePiAwaitingApprovals(run.result.awaiting_approvals, storedAwaitingApproval)
    const pendingApprovals = pendingPiApprovalsFromRuntimeEvents(events)
    const awaitingApprovals = pendingApprovals.length > 0 ? pendingApprovals : storedAwaitingApprovals
    const truncated = runtimeEventsUntilPendingPiApprovals(events, awaitingApprovals)
    const stateEvents = events.filter((event) => event.type === 'run.awaiting_decision')
    const includedIDs = new Set(truncated.map((event) => event.event_id))
    return [...truncated, ...stateEvents.filter((event) => !includedIDs.has(event.event_id))]
      .sort((left, right) => Number(left.seq || 0) - Number(right.seq || 0))
  }

  async getRuntimeArtifactByID(artifactID: string) {
    const workspaceID = workspaceIDFromArtifactID(artifactID)
    if (!workspaceID) {
      throw new RuntimeDomainError('runtime_artifact_id_invalid', 'Runtime artifact id is invalid')
    }
    const artifact = await this.store.getRuntimeArtifact(workspaceID, artifactID)
    if (!artifact || artifact.visibility === 'internal_only') {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
    return publicRuntimeArtifact(artifact)
  }

  async getRuntimeArtifact(actor: RuntimeActor, artifactID: string, payload: Record<string, unknown> = {}) {
    const workspaceID = workspaceIDFromArtifactID(artifactID)
    if (!workspaceID) {
      throw new RuntimeDomainError('runtime_artifact_id_invalid', 'Runtime artifact id is invalid')
    }
    if (workspaceID !== actor.workspace_id) {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
    const artifact = await this.store.getRuntimeArtifact(workspaceID, artifactID)
    if (!artifact || artifact.visibility === 'internal_only') {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
    if (artifact.visibility === 'parent_visible') {
      await this.assertParentVisibleArtifactAccess(actor, artifact, payload)
    }
    return publicRuntimeArtifact(artifact)
  }

  private async assertParentVisibleArtifactAccess(actor: RuntimeActor, artifact: RuntimeArtifact, payload: Record<string, unknown>) {
    const childRunLinkID = firstString(payload.child_run_link_id)
    const parentRuntimeRunID = firstString(payload.parent_runtime_run_id)
    const parentActionID = firstString(payload.parent_action_id)
    if (!childRunLinkID || !parentRuntimeRunID || !parentActionID) {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
    const link = await this.store.getParentRunLink(actor.workspace_id, artifact.runtime_run_id)
    await this.assertChildRunLinkAccess(actor, link, payload, artifact.session_id, artifact.runtime_run_id)
  }

  private async assertChildRunLinkAccess(
    actor: RuntimeActor,
    link: ChildRunLink | undefined,
    payload: Record<string, unknown>,
    childSessionID: number,
    childRuntimeRunID: string
  ) {
    const childRunLinkID = firstString(payload.child_run_link_id)
    const parentRuntimeRunID = firstString(payload.parent_runtime_run_id)
    const parentActionID = firstString(payload.parent_action_id)
    if (!link ||
      !childRunLinkID ||
      !parentRuntimeRunID ||
      !parentActionID ||
      link.child_run_link_id !== childRunLinkID ||
      link.parent_runtime_run_id !== parentRuntimeRunID ||
      link.parent_action_id !== parentActionID ||
      link.child_session_id !== childSessionID ||
      link.child_runtime_run_id !== childRuntimeRunID) {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
    const parentAction = await this.store.getActionByBusinessID(actor.workspace_id, parentActionID)
    if (!parentAction ||
      parentAction.runtime_run_id !== parentRuntimeRunID ||
      parentAction.session_id <= 0 ||
      parentAction.action_id !== link.parent_action_id) {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
    await this.assertSessionReadable(actor, parentAction.session_id, 'active_or_inactive')
    const parentEntryID = Number(payload.parent_entry_id || 0)
    if (parentEntryID > 0 && parentAction.entry_id && parentAction.entry_id !== parentEntryID) {
      throw new RuntimeDomainError('runtime_artifact_not_found', 'Runtime artifact not found', 404)
    }
  }

  private async saveRuntimeArtifact(params: {
    workspaceID: number
    sessionID: number
    runtimeRunID: string
    actionID?: string
    eventID?: string
    visibility?: RuntimeArtifact['visibility']
    artifactType: RuntimeArtifact['artifact_type']
    mimeType?: string
    storageRef: string
    preview: Record<string, unknown>
  }) {
    const artifactSeq = await this.store.nextRunArtifactSeq(params.runtimeRunID)
    const artifact: RuntimeArtifact = {
      artifact_id: artifactBusinessID(params.runtimeRunID, artifactSeq),
      artifact_seq: artifactSeq,
      workspace_id: params.workspaceID,
      session_id: params.sessionID,
      runtime_run_id: params.runtimeRunID,
      action_id: params.actionID,
      event_id: params.eventID,
      visibility: params.visibility || 'user_visible',
      artifact_type: params.artifactType,
      mime_type: params.mimeType || 'application/json',
      size_bytes: Buffer.byteLength(params.storageRef, 'utf8'),
      storage_ref: params.storageRef,
      preview_json: params.preview,
      created_at: now()
    }
    return this.store.saveRuntimeArtifact(artifact)
  }

  private async persistProviderRawArtifact(params: {
    actor: RuntimeActor
    session: AISession
    runtimeRunID: string
    raw: unknown
    providerCallIndex?: number
  }): Promise<{ refs: Record<string, unknown>[], events: RuntimeEventRecord[] }> {
    const raw = asRecord(params.raw)
    if (Object.keys(raw).length === 0) {
      return { refs: [], events: [] }
    }
    const providerCallIndex = params.providerCallIndex || 1
    const artifact = await this.saveRuntimeArtifact({
      workspaceID: params.actor.workspace_id,
      sessionID: params.session.id,
      runtimeRunID: params.runtimeRunID,
      visibility: 'internal_only',
      artifactType: 'provider_raw',
      mimeType: 'application/json',
      storageRef: JSON.stringify(raw),
      preview: {
        provider_call_index: providerCallIndex,
        has_raw: true,
        raw_keys: Object.keys(raw).slice(0, 24)
      }
    })
    const ref = {
      artifact_id: artifact.artifact_id,
      artifact_type: artifact.artifact_type,
      provider_call_index: providerCallIndex,
      created_at: artifact.created_at
    }
    return {
      refs: [ref],
      events: [{
        type: 'provider.raw_stored',
        payload: {
          artifact_id: artifact.artifact_id,
          artifact_type: artifact.artifact_type,
          visibility: artifact.visibility,
          provider_call_index: providerCallIndex
        }
      }]
    }
  }

  private async persistProviderRawFromCompletion(
    actor: RuntimeActor,
    session: AISession,
    runtimeRunID: string,
    completion: ChatModelResult | null | undefined,
    providerCallIndex = 1
  ) {
    return this.persistProviderRawArtifact({
      actor,
      session,
      runtimeRunID,
      raw: completion?.raw,
      providerCallIndex
    })
  }

  private async createToolResultRecord(params: {
    actor: RuntimeActor
    session: AISession
    runtimeRunID: string
    actionID: string
    toolCall: RuntimeToolCall
    status: RuntimeToolResultRecord['status']
    content: string
    structuredContent?: Record<string, unknown>
    metadata?: Record<string, unknown>
    error?: string
  }): Promise<RuntimeToolResultRecord> {
    const content = params.content
    const contentBytes = Buffer.byteLength(content, 'utf8')
    const structuredContent = params.structuredContent || {}
    const artifactRefs: Record<string, unknown>[] = []
    let visibleContent = content
    let visibleStructuredContent = structuredContent
    let truncated = false
    let modelNotice: string | undefined

    if (contentBytes > TOOL_RESULT_PREVIEW_MAX_BYTES) {
      truncated = true
      visibleContent = Buffer.from(content, 'utf8').subarray(0, TOOL_RESULT_PREVIEW_MAX_BYTES).toString('utf8')
      modelNotice = `Tool result was truncated to ${TOOL_RESULT_PREVIEW_MAX_BYTES} bytes. Use the artifact reference if full content is required.`
      visibleStructuredContent = {
        truncated: true,
        preview: visibleContent,
        original_size_bytes: contentBytes
      }
      const artifact = await this.saveRuntimeArtifact({
        workspaceID: params.actor.workspace_id,
        sessionID: params.session.id,
        runtimeRunID: params.runtimeRunID,
        actionID: params.actionID,
        visibility: 'user_visible',
        artifactType: 'tool_output',
        mimeType: 'text/plain',
        storageRef: content,
        preview: {
          truncated: true,
          preview: visibleContent,
          original_size_bytes: contentBytes,
          tool_name: params.toolCall.tool_name
        }
      })
      artifactRefs.push({
        artifact_id: artifact.artifact_id,
        artifact_type: artifact.artifact_type,
        preview_json: artifact.preview_json
      })
    }

    return {
      tool_call_id: params.toolCall.tool_call_id,
      tool_name: params.toolCall.tool_name,
      status: params.status,
      content: visibleContent,
      structured_content: visibleStructuredContent,
      metadata: params.metadata || {},
      error: params.error,
      truncated,
      model_notice: modelNotice,
      artifact_refs: artifactRefs
    }
  }

  private approvalRequestForAction(action: AgentAction, expiresAt: string) {
    const input = actionInput(action)
    const policy = actionPolicy(action)
    const target = actionTarget(action)
    const display = asRecord(action.display_json)
    const mcpServerKey = firstString(
      actionMcpServerKey(action),
      input.mcp_server_key,
      policy.mcp_server_key,
      display.mcp_server_key
    )
    const capability = firstString(input.capability, policy.capability, display.capability, 'tools')
    const inputPreview = {
      tool_name: actionToolName(action),
      arguments: asRecord(input.arguments),
      ...(mcpServerKey ? { mcp_server_key: mcpServerKey } : {}),
      ...(capability ? { capability } : {})
    }
    return approvalRequestSchema.parse({
      approval_id: approvalBusinessIDFor(action.runtime_run_id, action.id),
      run_id: action.runtime_run_id,
      action_id: action.action_id,
      tool_name: actionToolName(action),
      permission_key: firstString(policy.permission_key, `tool:${action.capability_id}:${firstString(policy.operation_type, 'read')}`),
      risk_level: firstString(policy.risk_level, riskLevelForOperation(firstString(policy.operation_type, 'read'))),
      reason: firstString(policy.risk_summary),
      input_preview: inputPreview,
      affected_resources: [{
        resource_type: firstString(target.target_type, target.type, mcpServerKey ? 'mcp_server' : ''),
        resource_id: firstString(target.target_id, target.id, mcpServerKey),
        operation_type: firstString(policy.operation_type)
      }],
      options: ['approve_once', 'approve_session', 'reject'],
      steer_supported: true,
      created_at: action.created_at,
      expires_at: expiresAt
    })
  }

  private async findMatchingSessionGrant(params: {
    actor: RuntimeActor
    session: AISession
    toolCall: RuntimeToolCall
    actionKind: string
    target: Record<string, unknown>
    policy: Record<string, unknown>
  }) {
    const permissionKey = firstString(params.policy.permission_key)
    const toolName = params.toolCall.tool_name
    const executorType = executorTypeForActionKind(params.actionKind)
    const resourceType = firstString(params.target.target_type, params.target.type)
    const resourceID = firstString(params.target.target_id, params.target.id)
    if (!permissionKey || !toolName || !resourceType || !resourceID) return undefined
    const nowMs = Date.now()
    const grants = await this.store.listSessionPermissionGrants(params.actor.workspace_id, params.session.id)
    return grants.find((grant) =>
      grant.status === 'active' &&
      Date.parse(grant.expires_at) > nowMs &&
      grant.permission_key === permissionKey &&
      grant.tool_name === toolName &&
      grant.executor_type === executorType &&
      grant.resource_type === resourceType &&
      grant.resource_id === resourceID
    )
  }

  private async findMatchingPiSessionGrant(params: {
    actor: RuntimeActor
    session: AISession
    toolName: string
    permissionKey: string
    resource?: AgentResource
    executorType?: string
    resourceType?: string
    resourceID?: string
  }) {
    const nowMs = Date.now()
    const executorType = firstString(params.executorType, params.resource ? 'mcp' : 'workspace')
    const resourceType = firstString(params.resourceType, params.resource?.resource_kind, 'workspace_tool')
    const resourceID = firstString(params.resourceID, params.resource?.resource_key, params.toolName)
    const grants = await this.store.listSessionPermissionGrants(params.actor.workspace_id, params.session.id)
    return grants.find((grant) =>
      grant.status === 'active' &&
      Date.parse(grant.expires_at) > nowMs &&
      grant.permission_key === params.permissionKey &&
      grant.tool_name === params.toolName &&
      grant.executor_type === executorType &&
      grant.resource_type === resourceType &&
      grant.resource_id === resourceID
    )
  }

  private async createPiSessionPermissionGrant(params: {
    actor: RuntimeActor
    session: AISession
    run: AIRuntimeRun
    approval: Record<string, unknown>
  }) {
    const permissionKey = firstString(params.approval.permission_key)
    const toolName = firstString(params.approval.tool_name)
    const executorType = firstString(params.approval.executor_type, 'mcp')
    const resourceType = firstString(params.approval.resource_type)
    const resourceID = firstString(params.approval.resource_id)
    if (!permissionKey || !toolName || !resourceType || !resourceID) {
      throw new RuntimeDomainError('pi_approval_scope_missing', 'Pi approval cannot create a Session grant without an explicit tool and resource scope', 409)
    }
    const existing = await this.store.listSessionPermissionGrants(params.actor.workspace_id, params.session.id)
    const active = existing.find((grant) =>
      grant.status === 'active' &&
      Date.parse(grant.expires_at) > Date.now() &&
      grant.permission_key === permissionKey &&
      grant.tool_name === toolName &&
      grant.executor_type === executorType &&
      grant.resource_type === resourceType &&
      grant.resource_id === resourceID
    )
    if (active) return active
    const grantSeq = await this.store.nextSessionPermissionGrantSeq(params.session.id)
    const createdAt = now()
    return this.store.saveSessionPermissionGrant({
      grant_id: sessionPermissionGrantBusinessID(params.actor.workspace_id, params.session.id, grantSeq),
      grant_seq: grantSeq,
      workspace_id: params.actor.workspace_id,
      session_id: params.session.id,
      runtime_run_id: params.run.runtime_run_id,
      action_id: 0,
      permission_key: permissionKey,
      tool_name: toolName,
      executor_type: executorType,
      resource_type: resourceType,
      resource_id: resourceID,
      status: 'active',
      granted_by: params.actor.user_id,
      created_at: createdAt,
      expires_at: new Date(Date.now() + SESSION_PERMISSION_GRANT_TTL_MS).toISOString()
    })
  }

  private eventRecorder(target: RuntimeEventRecord[], emit?: (event: RuntimeEventRecord) => Promise<RuntimeEventRecord[] | RuntimeEventRecord | void> | RuntimeEventRecord[] | RuntimeEventRecord | void) {
    return (type: string, payload: Record<string, unknown> = {}) => {
      const event: RuntimeEventRecord = { type, payload }
      target.push(event)
      const emitted = emit?.(event)
      const applyPersisted = (result: RuntimeEventRecord[] | RuntimeEventRecord | void) => {
        const persistedEvent = Array.isArray(result) ? result[0] : result
        if (persistedEvent) {
          event.type = persistedEvent.type
          event.payload = persistedEvent.payload
          event.persisted = persistedEvent.persisted
        }
      }
      if (emitted && typeof (emitted as Promise<unknown>).then === 'function') {
        return (emitted as Promise<RuntimeEventRecord[] | RuntimeEventRecord | void>).then((result) => {
          applyPersisted(result)
          return result
        })
      }
      const emittedValue = emitted as RuntimeEventRecord[] | RuntimeEventRecord | void
      applyPersisted(emittedValue)
      return emittedValue
    }
  }

  private async resolveSubagentProfiles(_workspaceID: number, profile: AgentProfileSnapshot) {
    if (profile.frozen_resources) {
      return profile.frozen_resources.subagents.map((item) => ({
        ref: item.ref as unknown as Record<string, unknown>,
        profile: item.snapshot,
        profileVersionID: item.profile_version_id,
        snapshotHash: item.snapshot_hash
      }))
    }
    throw new RuntimeDomainError(
      'runtime_profile_subagents_not_frozen',
      'Runtime profile Subagents are not frozen',
      409
    )
  }

  private builtinEasyDoCapabilityResource(actor: RuntimeActor, auth?: RuntimeAuth): AgentResource | null {
    const baseURL = firstString(this.options.easydoServerURL, process.env.EASYDO_SERVER_URL, process.env.SERVER_INTERNAL_URL)
    if (!baseURL) return null
    const timestamp = now()
    const delegatedToken = firstString(auth?.delegated_user_token, auth?.user_token)
    const headers = delegatedToken ? { Authorization: delegatedToken } : undefined
    return {
      id: 0,
      workspace_id: actor.workspace_id,
      resource_kind: 'mcp_server',
      resource_key: 'easydo',
      resource_id: 'easydo',
      name: 'easydo',
      description: 'Built-in EasyDo MCP Server',
      version: 'latest',
      status: 'active',
      spec: {
        builtin: true,
        readonly: true,
        mcpServers: {
          easydo: {
            type: 'streamable_http',
            url: `${baseURL.replace(/\/+$/, '')}/mcp`,
            ...(headers ? { headers } : {})
          }
        }
      },
      endpoint: {
        type: 'streamable_http',
        url: `${baseURL.replace(/\/+$/, '')}/mcp`
      },
      secret_ref: { configured: true, scope: 'current_user_workspace' },
      tags: ['mcp-server', 'builtin', 'easydo'],
      created_by: actor.user_id,
      created_at: timestamp,
      updated_at: timestamp
    }
  }

  private async discoverMcpTools(
    resource: AgentResource,
    auth: RuntimeAuth | undefined,
    record: (type: string, payload?: Record<string, unknown>) => Promise<unknown> | unknown
  ) {
    const configuredTools = mcpToolDefinitionsFromValues(mcpRawTools(resource), resource)
    if (configuredTools.length > 0) return configuredTools

    const configs = mcpServerConfigs(resource)
    if (configs.length === 0) return []
    const discovered: ChatModelToolDefinition[] = []
    for (const entry of configs) {
      const serverKey = firstString(entry.name, resource.resource_key)
      try {
        const requestID = mcpServerScopedRequestID(resource, serverKey)
        const result = await mcpClient(resource, entry.config, auth, this.logger).request('tools/list', {}, requestID, {
          mcp_server_id: serverKey
        })
        const tools = Array.isArray(result.tools) ? result.tools : []
        discovered.push(...mcpToolDefinitionsFromValues(
          tools.map((tool) => ({ ...asRecord(tool), mcp_server_key: serverKey })),
          resource
        ))
      } catch (error) {
        const message = error instanceof Error ? error.message : 'MCP tools/list failed'
        await record('mcp.tools_discovery_failed', {
          mcp_server: serverKey,
          error: message,
          message
        })
      }
    }
    return uniqueMcpToolDefinitions(discovered)
  }

  private skillShouldLoad(skill: Record<string, unknown>, ref: unknown, content: string) {
    const config = resourceRefConfig(ref)
    if (config.auto_load === true || config.required === true) return true
    const haystack = content.toLowerCase()
    if (!haystack.trim()) return false
    const configuredKeywords = Array.isArray(config.keywords)
      ? config.keywords.map((item) => asString(item)).filter(Boolean)
      : []
    const skillText = [
      ...configuredKeywords,
      asString(skill.name),
      asString(skill.description),
      asString(skill.key),
      asString(skill.resource_key)
    ].join(' ').toLowerCase()
    return skillText
      .split(/[-_\s,，.。/]+/)
      .some((part) => part.length >= 3 && haystack.includes(part))
  }

  private skillInstructions(resource: Record<string, unknown>) {
    const spec = asRecord(resource.spec)
    const discovered = Array.isArray(spec.discovered_skills) ? spec.discovered_skills : []
    return {
      id: resource.id,
      key: resource.resource_key,
      name: resource.name,
      description: resource.description,
      version: resource.version,
      instructions: firstString(spec.instructions, spec.markdown, spec.content, spec.prompt),
      discovered_skills: discovered.map((item) => asRecord(item)).map((item) => ({
        key: item.key,
        name: item.name,
        description: item.description,
        entry: item.entry
      }))
    }
  }

  private async prepareExecutionContext(
    actor: RuntimeActor,
    session: AISession,
    profile: AgentProfileSnapshot,
    content: string,
    payload: Record<string, unknown>,
    runtimeRunID: string,
    auth?: RuntimeAuth,
    emit?: (event: RuntimeEventRecord) => Promise<RuntimeEventRecord[] | RuntimeEventRecord | void> | RuntimeEventRecord[] | RuntimeEventRecord | void
  ): Promise<ExecutionContext> {
    const runtimeEvents: RuntimeEventRecord[] = []
    const record = this.eventRecorder(runtimeEvents, emit)
    await record('run.started', { status: 'running' })
    await record('context.build_started', {
      profile_id: profile.id,
      profile_name: profile.name,
      context_tags: profile.context_tags
    })
    const contextTagAssembly = await this.contextTagRegistry.resolve(profile.context_tags, {
      actor,
      profile,
      content,
      payload
    })
    await record('context_tags.resolved', {
      tags: contextTagAssembly.tags,
      fragments: contextTagAssembly.fragments.map((fragment) => ({
        tag: fragment.tag,
        order: fragment.order,
        required: fragment.required,
        title: fragment.title,
        data: fragment.data
      })),
      warnings: contextTagAssembly.warnings
    })
    const modelContent = composeTaggedUserContent(content, contextTagAssembly.fragments)

    const resources = await resourcesForProfile(this.store, actor.workspace_id, profile)
    const skillResources = (profile.skills || [])
      .map((ref) => {
        const resource = resources.find((item) => item.resource_kind === 'skill' && refMatchesResource(ref, item))
        return resource ? { ref, resource } : null
      })
      .filter(Boolean) as Array<{ ref: AgentProfileDraft['skills'][number], resource: typeof resources[number] }>
    const mcpResources = (profile.mcp_servers || [])
      .map((ref) => {
        const resource = resources.find((item) => item.resource_kind === 'mcp_server' && refMatchesResource(ref, item))
        if (!resource && normalizeResourceID(ref.resource_id) === 'easydo') {
          const builtin = this.builtinEasyDoCapabilityResource(actor, auth)
          return builtin ? { ref, resource: builtin } : null
        }
        return resource ? { ref, resource } : null
      })
      .filter(Boolean) as Array<{ ref: AgentProfileDraft['mcp_servers'][number], resource: typeof resources[number] }>
    const subagentProfiles = await this.resolveSubagentProfiles(actor.workspace_id, profile)
    const mcpTools = uniqueMcpToolDefinitions((await Promise.all(
      mcpResources.map(({ resource }) => this.discoverMcpTools(resource, auth, record))
    )).flat())

    const capabilities = {
      context_tags: contextTagAssembly.tags,
      context_tag_context: contextTagAssembly.fragments.map((fragment) => ({
        tag: fragment.tag,
        order: fragment.order,
        required: fragment.required,
        title: fragment.title,
        data: fragment.data
      })),
      session: {
        kind: session.session_kind
      },
      workspace: {
        id: actor.workspace_id,
        role: actor.workspace_role
      },
      skills: skillResources.map(({ resource }) => ({
        id: resource.id,
        key: resource.resource_key,
        name: resource.name,
        description: resource.description,
        version: resource.version
      })),
      mcp_servers: mcpResources.map(({ resource }) => publicMcpResource(resource)),
      mcp_tools: mcpTools.map((tool) => publicMcpToolDefinition(tool)),
      subagents: subagentProfiles.map(({ profile: subagent }) => ({
        id: subagent.id,
        name: subagent.name,
        description: subagent.description,
        profile_kind: subagent.profile_kind,
        context_tags: subagent.context_tags
      })),
      tool_policy: profile.tool_policy,
      confirmation_policy: profile.confirmation_policy,
      memory_policy: profile.memory_policy,
      response_mode: profile.response_mode,
      l5_controls: {
        event_stream: true,
        output_schema_validation: hasSchema(profile.output_schema),
        skill_progressive_loading: true,
        mcp_capability_snapshot: true,
        subagent_scheduler: true,
        write_tool_approval: true
      }
    }
    await record('capability.snapshot', capabilities)
    if (mcpTools.length > 0) {
      await record('mcp.tools.available', {
        tool_count: mcpTools.length,
        tools: mcpTools.map((tool) => ({ name: tool.name, description: tool.description }))
      })
    }

    const loadedSkills = skillResources
      .map(({ ref, resource }) => ({ ref, skill: this.skillInstructions(publicResource(resource)) }))
      .filter(({ ref, skill }) => this.skillShouldLoad(skill, ref, content))
      .map(({ skill }) => skill)
    for (const skill of loadedSkills) {
      await record('skill.loaded', {
        key: skill.key,
        name: skill.name,
        version: skill.version,
        discovered_count: Array.isArray(skill.discovered_skills) ? skill.discovered_skills.length : 0
      })
    }

    const subagentRun = await this.runSubagentTasks(actor, session, profile, content, payload, asRecord(payload.context_ref), subagentProfiles, runtimeRunID, record)
    const subagentResults = subagentRun.results
    const agentActions = subagentRun.actions
    const outputSchema = asRecord(profile.output_schema)
    if (hasSchema(outputSchema)) {
      await record('output_schema.available', { schema: outputSchema })
    }
    await record('context.build_completed', {
      context_tag_count: contextTagAssembly.tags.length,
      context_fragment_count: contextTagAssembly.fragments.length,
      skill_count: loadedSkills.length,
      mcp_tool_count: mcpTools.length,
      subagent_result_count: subagentResults.length,
      model_content_chars: modelContent.length,
      has_output_schema: hasSchema(outputSchema)
    })

    return {
      capabilities,
      contextTagAssembly,
      modelContent,
      loadedSkills,
      subagentResults,
      agentActions,
      runtimeEvents,
      outputSchema,
      developerInstructions: easyDoRuntimeDeveloperInstructions(firstString(
        asRecord(profile.prompt).developer,
        asRecord(profile.prompt).developer_instructions,
        asRecord(profile.prompt).instructions
      ), mcpTools),
      mcpTools
    }
  }

  private async runSubagentTasks(
    actor: RuntimeActor,
    session: AISession,
    parentProfile: AgentProfileSnapshot,
    content: string,
    payload: Record<string, unknown>,
    contextRef: Record<string, unknown>,
    subagentProfiles: Array<{ ref: Record<string, unknown>, profile: AgentProfileSnapshot, profileVersionID: number, snapshotHash: string }>,
    runtimeRunID: string,
    record: (type: string, payload?: Record<string, unknown>) => void
  ) {
    const requested = Array.isArray(payload.subagent_tasks)
      ? payload.subagent_tasks.map((item) => asRecord(item))
      : []
    const autoSpawn = Boolean(payload.spawn_subagents) || Boolean(asRecord(parentProfile.confirmation_policy).auto_spawn_subagents)
    const tasks = requested.length > 0
      ? requested
      : autoSpawn
        ? subagentProfiles.map(({ ref, profile }) => ({
          agent_profile_id: profile.id,
          objective: firstString(asRecord(ref.config).objective, profile.description, `Assist with ${content}`),
          mode: asString(asRecord(ref.config).mode) || 'read_only'
        }))
        : []
    const limitedTasks = tasks.slice(0, 8)
    if (limitedTasks.length >= 2) {
      return this.runOrchestratedSubagentTasks({
        actor,
        session,
        parentProfile,
        content,
        payload,
        contextRef,
        subagentProfiles,
        runtimeRunID,
        record,
        limitedTasks
      })
    }
    const results: Record<string, unknown>[] = []
    const actions: Record<string, unknown>[] = []
    for (const [index, rawTask] of limitedTasks.entries()) {
      const task = asRecord(rawTask)
      const taskID = firstString(task.task_id, task.id, `subagent-${index + 1}`)
      const rawProfileID = firstString(task.agent_profile_id, task.profile_id, task.resource_id)
      const profileID = Number(rawProfileID)
      const match = rawProfileID
        ? subagentProfiles.find((item) => item.profile.id === profileID)
        : subagentProfiles[index]
      const mode = firstString(task.mode, 'read_only')
      const operationType = mode.toLowerCase().includes('write') ? 'write' : 'read'
      const objective = firstString(task.objective, match?.profile.description, content)
      const subagentLogContext: RuntimeLogInput = {
        component: 'session-service',
        operation: 'subagent',
        request_id: firstString(payload.runtime_request_id, payload.request_id),
        workspace_id: actor.workspace_id,
        session_id: session.id,
        parent_runtime_run_id: runtimeRunID,
        subagent_id: taskID
      }
      this.logger.info({ ...subagentLogContext, outcome: 'started' })
      const timestamp = now()
      const actionID = await this.store.nextActionId()
      const action: AgentAction = {
        id: actionID,
        action_id: actionBusinessIDFor(runtimeRunID, actionID),
        workspace_id: actor.workspace_id,
        context_tags: [...session.context_tags],
        session_id: session.id,
        runtime_run_id: runtimeRunID,
        action_kind: 'subagent.spawn',
        idempotency_key: runtimeIdempotencyKey('action', runtimeRunID, `turn${seq36(0)}`, `part${seq36(actionID)}`, 'subagent.spawn'),
        source: requested.length > 0 ? 'user' : 'policy',
        capability_id: 'subagent.spawn',
        input_json: {
          task_id: taskID,
          objective,
          mode,
          requested_profile_id: rawProfileID || undefined
        },
        input_digest: '',
        target_json: {
          target_type: 'agent_profile',
          target_id: match ? String(match.profile.id) : rawProfileID,
          agent_profile_id: match ? String(match.profile.id) : rawProfileID
        },
        policy_json: {
          operation_type: operationType,
          risk_summary: 'Spawn a bounded EasyDo sub-agent task',
          requires_decision: false,
          policy_sources: requested.length > 0 ? ['user_request'] : ['profile']
        },
        display_json: {
          title: `启动子 Agent ${match?.profile.name || rawProfileID || taskID}`,
          name: match?.profile.name || 'subagent.spawn',
          summary: 'Spawn a bounded EasyDo sub-agent task',
          input_preview: {
            task_id: taskID,
            objective,
            mode
          }
        },
        result_json: {},
        requested_by: actor.user_id,
        status: match ? 'executing' : 'failed',
        error_msg: match ? undefined : (rawProfileID ? 'Requested sub-agent profile is not available to this parent profile' : 'No sub-agent profile is available for this task'),
        created_at: timestamp,
        updated_at: timestamp
      }
      action.input_json = {
        ...action.input_json,
        task_id: taskID,
        objective,
        mode,
        parent_profile_id: parentProfile.id,
        requested_profile_id: rawProfileID || undefined
      }
      action.input_digest = actionInputDigest(action.input_json)
      let savedAction = await this.store.saveAction(action)
      if (!match) {
        const result = {
          action_id: savedAction.action_id,
          action_internal_id: savedAction.id,
          task_id: taskID,
          status: 'failed',
          agent_profile_id: rawProfileID || '',
          error: savedAction.error_msg || 'Sub-agent task failed'
        }
        savedAction = await this.store.saveAction({
          ...savedAction,
          result_json: result,
          updated_at: now()
        })
        actions.push(publicAction(savedAction))
        results.push(result)
        await record('subagent.failed', result)
        this.logger.error({ ...subagentLogContext, outcome: 'failed', code: 'subagent_profile_unavailable', category: 'validation' })
        continue
      }
      try {
        const result = await this.executeSubagentChildRun({
          actor,
          parentSession: session,
          parentProfile,
          childProfile: match.profile,
          childProfileVersionID: match.profileVersionID,
          parentRuntimeRunID: runtimeRunID,
          action: savedAction,
          taskID,
          objective,
          mode,
          contextRef,
          includeCurrentPage: Boolean(payload.include_current_page),
          recordParentEvent: record
        })
        if (result.status === 'blocked_approval') {
          savedAction = await this.store.saveAction({
            ...savedAction,
            status: 'awaiting_decision',
            result_json: result,
            updated_at: now()
          })
          actions.push(publicAction(savedAction))
          results.push(result)
          this.logger.info({
            ...subagentLogContext,
            runtime_run_id: firstString(result.child_runtime_run_id),
            outcome: 'awaiting_approval',
            code: 'subagent_approval_required',
            category: 'permission'
          })
          continue
        }
        if (result.status === 'cancelled') {
          const syncedAction = await this.store.getActionByBusinessID(actor.workspace_id, savedAction.action_id)
          actions.push(publicAction(syncedAction || savedAction))
          results.push(result)
          this.logger.info({
            ...subagentLogContext,
            runtime_run_id: firstString(result.child_runtime_run_id),
            outcome: 'cancelled',
            code: 'subagent_cancelled',
            category: 'cancelled'
          })
          continue
        }
        await record('subagent.progress', {
          action_id: savedAction.action_id,
          action_internal_id: savedAction.id,
          task_id: taskID,
          agent_profile_id: match.profile.id,
          name: match.profile.name,
          status: result.status === 'completed' ? 'running' : 'failed',
          summary: `子 Agent ${match.profile.name} 已返回结果摘要`,
          child_run_link_id: result.child_run_link_id,
          child_session_id: result.child_session_id,
          child_runtime_run_id: result.child_runtime_run_id
        })
        savedAction = await this.store.saveAction({
          ...savedAction,
          status: result.status === 'completed' ? 'executed' : 'failed',
          executed_at: now(),
          result_json: result,
          error_msg: result.status === 'completed' ? undefined : firstString(asRecord(result).error, 'Sub-agent failed'),
          updated_at: now()
        })
        actions.push(publicAction(savedAction))
        results.push(result)
        await record(result.status === 'completed' ? 'subagent.completed' : 'subagent.failed', result)
        const subagentOutcome = result.status === 'completed' ? 'completed' : 'failed'
        const logInput = {
          ...subagentLogContext,
          runtime_run_id: firstString(result.child_runtime_run_id),
          outcome: subagentOutcome,
          ...(subagentOutcome === 'failed' ? { code: 'subagent_failed', category: 'internal' } : {})
        }
        if (subagentOutcome === 'completed') this.logger.info(logInput)
        else this.logger.error(logInput)
      } catch (error) {
        const descriptor = classifyRuntimeError(error)
        const result = {
          action_id: savedAction.action_id,
          action_internal_id: savedAction.id,
          task_id: taskID,
          status: 'failed',
          agent_profile_id: match.profile.id,
          name: match.profile.name,
          error: descriptor.user_message
        }
        savedAction = await this.store.saveAction({
          ...savedAction,
          status: 'failed',
          result_json: result,
          error_msg: asString(result.error),
          updated_at: now()
        })
        actions.push(publicAction(savedAction))
        results.push(result)
        await record('subagent.failed', result)
        this.logger.error({
          ...subagentLogContext,
          outcome: descriptor.terminal_status,
          code: descriptor.code,
          category: descriptor.category
        })
      }
    }
    return { results, actions }
  }

  private async runOrchestratedSubagentTasks(params: {
    actor: RuntimeActor
    session: AISession
    parentProfile: AgentProfileSnapshot
    content: string
    payload: Record<string, unknown>
    contextRef: Record<string, unknown>
    subagentProfiles: Array<{ ref: Record<string, unknown>, profile: AgentProfileSnapshot, profileVersionID: number, snapshotHash: string }>
    runtimeRunID: string
    record: (type: string, payload?: Record<string, unknown>) => void
    limitedTasks: Record<string, unknown>[]
  }) {
    const results: Record<string, unknown>[] = []
    const actions: Record<string, unknown>[] = []
    const orchestration = new OrchestrationService(this.store)
    const taskSpecs = params.limitedTasks.map((rawTask, index) => {
      const task = asRecord(rawTask)
      const taskID = firstString(task.task_id, task.id, `subagent-${index + 1}`)
      const rawProfileID = firstString(task.agent_profile_id, task.profile_id, task.resource_id)
      const profileID = Number(rawProfileID)
      const match = rawProfileID
        ? params.subagentProfiles.find((item) => item.profile.id === profileID)
        : params.subagentProfiles[index]
      return {
        task_id: taskID,
        objective: firstString(task.objective, match?.profile.description, params.content),
        assigned_profile_id: match?.profile.id || profileID || 0,
        assigned_profile_version_id: match?.profileVersionID || 0,
        assigned_subagent_name: match?.profile.name || '',
        mode: firstString(task.mode, 'read_only'),
        dependency_ids: Array.isArray(task.dependency_ids) ? task.dependency_ids.map(String) : []
      }
    }).filter((task) => task.objective)

    await orchestration.assembleAndRun({
      workspace_id: params.actor.workspace_id,
      parent_runtime_run_id: params.runtimeRunID,
      parent_session_id: params.session.id,
      parent_summary: params.content,
      source: 'session_service.subagent_tasks',
      resource_versions: {
        parent_profile_id: params.parentProfile.id,
        parent_profile_version_key: firstString(asRecord(params.payload).agent_profile_version_key)
      },
      tasks: taskSpecs
    }, {
      owner: firstString(process.env.AI_RUNTIME_INSTANCE_ID, 'ai-runtime'),
      emit: async (type, payload) => {
        params.record(type, payload)
      },
      executeTask: async (task) => {
        const match = params.subagentProfiles.find((item) => item.profile.id === task.assigned_profile_id)
          || params.subagentProfiles.find((item) => item.profile.name === task.assigned_subagent_name)
        if (!match) {
          return {
            status: 'failed' as const,
            error_code: 'subagent_profile_unavailable',
            error_msg: `Sub-agent profile unavailable for task ${task.task_id}`
          }
        }
        const savedAction = await this.createSubagentSpawnAction({
          actor: params.actor,
          session: params.session,
          parentProfile: params.parentProfile,
          runtimeRunID: params.runtimeRunID,
          taskID: task.task_id,
          objective: task.objective,
          mode: task.mode,
          childProfile: match.profile,
          source: 'policy'
        })
        actions.push(publicAction(savedAction))
        try {
          const result = await this.executeSubagentChildRun({
            actor: params.actor,
            parentSession: params.session,
            parentProfile: params.parentProfile,
            childProfile: match.profile,
            childProfileVersionID: match.profileVersionID,
            parentRuntimeRunID: params.runtimeRunID,
            action: savedAction,
            taskID: task.task_id,
            objective: task.objective,
            mode: task.mode,
            contextRef: params.contextRef,
            includeCurrentPage: Boolean(params.payload.include_current_page),
            recordParentEvent: params.record
          })
          results.push(result)
          const resultRecord = asRecord(result)
          const nextAction = await this.store.saveAction({
            ...savedAction,
            status: result.status === 'completed'
              ? 'executed'
              : result.status === 'blocked_approval'
                ? 'awaiting_decision'
                : result.status === 'cancelled'
                  ? 'cancelled'
                  : 'failed',
            result_json: result,
            error_msg: result.status === 'completed' ? undefined : firstString(resultRecord.error, resultRecord.error_msg, 'Sub-agent failed'),
            executed_at: result.status === 'completed' ? now() : undefined,
            updated_at: now()
          })
          const actionIndex = actions.findIndex((item) => asRecord(item).action_id === savedAction.action_id)
          if (actionIndex >= 0) actions[actionIndex] = publicAction(nextAction)
          else actions.push(publicAction(nextAction))
          if (result.status === 'completed') params.record('subagent.completed', result)
          else if (result.status === 'blocked_approval') params.record('subagent.blocked_approval', result)
          else if (result.status === 'cancelled') params.record('subagent.cancelled', result)
          else params.record('subagent.failed', result)
          return {
            status: result.status === 'completed' || result.status === 'blocked_approval' || result.status === 'cancelled'
              ? result.status
              : 'failed',
            child_run_link_id: firstString(result.child_run_link_id),
            child_runtime_run_id: firstString(result.child_runtime_run_id),
            result_summary: firstString(resultRecord.summary, resultRecord.result_summary, `${match.profile.name} ${result.status}`),
            artifact_refs: Array.isArray(resultRecord.artifact_refs) ? resultRecord.artifact_refs : [],
            error_code: result.status === 'completed' ? undefined : firstString(resultRecord.error_code, 'subagent_failed'),
            error_msg: result.status === 'completed' ? undefined : firstString(resultRecord.error, resultRecord.error_msg)
          }
        } catch (error) {
          const descriptor = classifyRuntimeError(error)
          const failed = {
            action_id: savedAction.action_id,
            task_id: task.task_id,
            status: 'failed',
            agent_profile_id: match.profile.id,
            name: match.profile.name,
            error: descriptor.user_message
          }
          results.push(failed)
          params.record('subagent.failed', failed)
          await this.store.saveAction({
            ...savedAction,
            status: 'failed',
            result_json: failed,
            error_msg: descriptor.user_message,
            updated_at: now()
          })
          return {
            status: 'failed' as const,
            error_code: descriptor.code,
            error_msg: descriptor.user_message
          }
        }
      }
    })

    return { results, actions }
  }

  private async createSubagentSpawnAction(params: {
    actor: RuntimeActor
    session: AISession
    parentProfile: AgentProfileSnapshot
    runtimeRunID: string
    taskID: string
    objective: string
    mode: string
    childProfile: AgentProfileSnapshot
    source: AgentAction['source']
  }) {
    const timestamp = now()
    const actionID = await this.store.nextActionId()
    const operationType = params.mode.toLowerCase().includes('write') ? 'write' : 'read'
    const action: AgentAction = {
      id: actionID,
      action_id: actionBusinessIDFor(params.runtimeRunID, actionID),
      workspace_id: params.actor.workspace_id,
      context_tags: [...params.session.context_tags],
      session_id: params.session.id,
      runtime_run_id: params.runtimeRunID,
      action_kind: 'subagent.spawn',
      idempotency_key: runtimeIdempotencyKey('action', params.runtimeRunID, `turn${seq36(0)}`, `part${seq36(actionID)}`, 'subagent.spawn'),
      source: params.source,
      capability_id: 'subagent.spawn',
      input_json: {
        task_id: params.taskID,
        objective: params.objective,
        mode: params.mode,
        parent_profile_id: params.parentProfile.id,
        requested_profile_id: params.childProfile.id
      },
      input_digest: '',
      target_json: {
        target_type: 'agent_profile',
        target_id: String(params.childProfile.id),
        agent_profile_id: String(params.childProfile.id)
      },
      policy_json: {
        operation_type: operationType,
        risk_summary: 'Spawn a bounded EasyDo sub-agent task',
        requires_decision: false,
        policy_sources: ['model_tool_call']
      },
      display_json: {
        title: `启动子 Agent ${params.childProfile.name}`,
        name: params.childProfile.name,
        summary: 'Spawn a bounded EasyDo sub-agent task',
        input_preview: {
          task_id: params.taskID,
          objective: params.objective,
          mode: params.mode
        }
      },
      result_json: {},
      requested_by: params.actor.user_id,
      status: 'executing',
      created_at: timestamp,
      updated_at: timestamp
    }
    action.input_digest = actionInputDigest(action.input_json)
    return this.store.saveAction(action)
  }

  private async executeSubagentChildRun(params: {
    actor: RuntimeActor
    parentSession: AISession
    parentProfile: AgentProfileSnapshot
    childProfile: AgentProfileSnapshot
    childProfileVersionID: number
    parentRuntimeRunID: string
    action: AgentAction
    taskID: string
    objective: string
    mode: string
    contextRef: Record<string, unknown>
    includeCurrentPage: boolean
    recordParentEvent?: (type: string, payload?: Record<string, unknown>) => Promise<unknown> | unknown
  }) {
    assertUnresolvedProfileSnapshot(params.childProfile)
    const timestamp = now()
    const childSessionID = await this.store.nextSessionId()
    const childRunID = await this.store.nextRunId()
    const childRuntimeRunID = runBusinessID(params.actor.workspace_id, childSessionID, childRunID)
    const childSessionBusinessID = sessionBusinessID(params.actor.workspace_id, childSessionID)
    const persistedChildProfile = params.childProfile
    const executionChildProfile = await this.resolveProfileSecrets(params.actor.workspace_id, persistedChildProfile)
    const snapshotDigest = profileSnapshotHash(persistedChildProfile)
    const childSession: AISession = {
      id: childSessionID,
      workspace_id: params.actor.workspace_id,
      context_tags: [...params.childProfile.context_tags],
      session_kind: 'task_run',
      user_id: params.actor.user_id,
      auth_session_id: params.actor.auth_session_id,
      business_type: 'subagent',
      business_id: `${params.action.action_id}:${params.taskID}`,
      status: 'active',
      agent_profile_id: params.childProfile.id,
      agent_profile_version_id: params.childProfileVersionID,
      agent_profile_version_key: params.childProfileVersionID > 0 ? `v${params.childProfileVersionID}` : 'draft',
      agent_profile_snapshot_hash: snapshotDigest,
      agent_workspace_runtime_id: params.parentSession.agent_workspace_runtime_id,
      agent_workspace_snapshot_hash: params.parentSession.agent_workspace_snapshot_hash,
      session_key: runtimeIdempotencyKey('session', 'subagent', params.action.action_id, params.taskID),
      first_session_timestamp: timestamp,
      source: 'subagent',
      title: `${params.childProfile.name}: ${params.taskID}`,
      entry_count: 0,
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveSession(childSession)

    const childUserEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: childSession.id,
      workspace_id: params.actor.workspace_id,
      user_id: params.actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content: params.objective,
      content_blocks: [{ type: 'text', text: params.objective }],
      input: {
        parent_session_id: params.parentSession.id,
        parent_runtime_run_id: params.parentRuntimeRunID,
        parent_action_id: params.action.action_id,
        task_id: params.taskID,
        mode: params.mode,
        context_ref: params.contextRef
      },
      output: {},
      runtime_run_id: childRuntimeRunID,
      event_seq: 1,
      idempotency_key: runtimeIdempotencyKey('entry', childSessionBusinessID, 'subagent', params.action.action_id, 'user'),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(childUserEntry)

    let childRun: AIRuntimeRun = {
      id: childRunID,
      runtime_run_id: childRuntimeRunID,
      session_id: childSession.id,
      workspace_id: params.actor.workspace_id,
      context_tags: [...params.childProfile.context_tags],
      agent_profile_id: params.childProfile.id,
      agent_profile_version_id: params.childProfileVersionID,
      agent_profile_version_key: childSession.agent_profile_version_key,
      agent_profile_snapshot_hash: snapshotDigest,
      agent_workspace_runtime_id: childSession.agent_workspace_runtime_id,
      agent_workspace_snapshot_hash: childSession.agent_workspace_snapshot_hash,
      profile_snapshot: persistedChildProfile,
      status: 'running',
      input_entry_id: childUserEntry.id,
      request: {
        runtime_engine: 'pi',
        runtime_session_id: childSessionBusinessID,
        content: params.objective,
        parent_session_id: params.parentSession.id,
        parent_runtime_run_id: params.parentRuntimeRunID,
        parent_action_id: params.action.action_id,
        parent_action_internal_id: params.action.id,
        parent_profile_id: params.parentProfile.id,
        task_id: params.taskID,
        objective: params.objective,
        mode: params.mode,
        context_ref: params.contextRef
      },
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveRun(childRun)

    const childSeq = await this.store.nextActionChildSeq(params.action.id)
    let childRunLink: ChildRunLink = {
      child_run_link_id: childRunLinkBusinessID(params.action, childSeq),
      workspace_id: params.actor.workspace_id,
      parent_runtime_run_id: params.parentRuntimeRunID,
      parent_action_id: params.action.action_id,
      parent_action_internal_id: params.action.id,
      child_seq: childSeq,
      child_session_id: childSession.id,
      child_runtime_run_id: childRuntimeRunID,
      child_profile_id: params.childProfile.id,
      child_profile_version_id: params.childProfileVersionID,
      snapshot_digest: snapshotDigest,
      status: 'running',
      created_at: timestamp
    }
    childRunLink = await this.store.saveChildRunLink(childRunLink)

    await params.recordParentEvent?.('subagent.spawned', {
      action_id: params.action.action_id,
      action_internal_id: params.action.id,
      task_id: params.taskID,
      agent_profile_id: params.childProfile.id,
      name: params.childProfile.name,
      summary: `已启动子 Agent ${params.childProfile.name}`,
      mode: params.mode,
      child_run_link_id: childRunLink.child_run_link_id,
      child_session_id: childSession.id,
      child_runtime_run_id: childRuntimeRunID,
      parent_runtime_run_id: params.parentRuntimeRunID,
      parent_action_id: params.action.action_id
    })

    const attempt = await this.store.nextActionExecutionAttempt(params.action.id)
    let execution: ActionExecution = await this.store.saveActionExecution({
      execution_id: executionBusinessID(params.action, attempt),
      workspace_id: params.actor.workspace_id,
      session_id: params.parentSession.id,
      runtime_run_id: params.parentRuntimeRunID,
      action_id: params.action.id,
      attempt,
      executor_type: 'subagent',
      executor_ref_json: {
        child_run_link_id: childRunLink.child_run_link_id,
        child_session_id: childSession.id,
        child_runtime_run_id: childRuntimeRunID,
        child_profile_id: params.childProfile.id
      },
      idempotency_key: runtimeIdempotencyKey('execution', params.action.action_id, `attempt${seq36(attempt)}`),
      status: 'running',
      result_json: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })

    const durableParentRun = await this.store.getRun(params.actor.workspace_id, params.parentRuntimeRunID)
    if (!durableParentRun || isTerminalRunStatus(durableParentRun.status)) {
      const reason = `Parent run is already ${durableParentRun?.status || 'unavailable'}`
      const cancelled = await this.cancelSessionRun(params.actor, childSession.id, {
        runtime_run_id: childRuntimeRunID,
        reason
      })
      await this.syncParentAfterChildTerminal(params.actor, cancelled.run, reason)
      return {
        action_id: params.action.action_id,
        action_internal_id: params.action.id,
        execution_id: execution.execution_id,
        task_id: params.taskID,
        status: 'cancelled' as const,
        agent_profile_id: params.childProfile.id,
        agent_profile_version_id: params.childProfileVersionID,
        name: params.childProfile.name,
        summary: reason,
        error: reason,
        child_run_link_id: childRunLink.child_run_link_id,
        child_session_id: childSession.id,
        child_runtime_run_id: childRuntimeRunID,
        artifact_refs: []
      }
    }

    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Subagent execution requires the Pi runtime engine', 500)
    }
    const childEventStore = this.agentRuntimeEngine.eventStore
    await childEventStore.append({
      type: 'run.started',
      session_id: childSessionBusinessID,
      runtime_run_id: childRuntimeRunID,
      event_id: '',
      seq: 0,
      timestamp: now(),
      status: 'running',
      run: { runtime_run_id: childRuntimeRunID, status: 'running' }
    } as unknown as AgentRuntimeEvent)
    await childEventStore.append({
      type: 'subagent.started',
      session_id: childSessionBusinessID,
      runtime_run_id: childRuntimeRunID,
      event_id: '',
      seq: 0,
      timestamp: now(),
      payload: {
        child_run_link_id: childRunLink.child_run_link_id,
        child_runtime_run_id: childRuntimeRunID,
        parent_runtime_run_id: params.parentRuntimeRunID,
        parent_action_id: params.action.action_id,
        parent_action_internal_id: params.action.id,
        task_id: params.taskID,
        agent_profile_id: params.childProfile.id,
        name: params.childProfile.name,
        objective: params.objective,
        mode: params.mode
      }
    } as unknown as AgentRuntimeEvent)

    let assistantContent = ''
    let resultStatus: 'completed' | 'failed' | 'blocked_approval' = 'completed'
    let errorMessage = ''
    let validation: OutputValidationResult = { valid: true, errors: [], structuredOutput: {} }
    const providerRawRefs: Record<string, unknown>[] = []
    const progressSampler = createSubagentProgressSampler({
      onEmit: async (payload) => {
        await params.recordParentEvent?.('subagent.progress', payload)
      }
    })
    const unsubscribeChildProgress = childEventStore.subscribe(childSessionBusinessID, async (event) => {
      const childEventType = String(event.type)
      if (childEventType === 'run.started' || childEventType === 'subagent.started') return
      if (childEventType.endsWith('.delta')) return
      await progressSampler.push({
        action_id: params.action.action_id,
        action_internal_id: params.action.id,
        task_id: params.taskID,
        agent_profile_id: params.childProfile.id,
        name: params.childProfile.name,
        status: 'running',
        summary: `子 Agent ${params.childProfile.name}: ${childEventType}`,
        child_event_type: childEventType,
        child_event_id: event.event_id,
        child_event_seq: event.seq,
        child_run_link_id: childRunLink.child_run_link_id,
        child_session_id: childSession.id,
        child_runtime_run_id: childRuntimeRunID
      })
    })
    try {
      const childResources = await this.buildPiContinuationResources({
        actor: params.actor,
        session: childSession,
        profileSnapshot: executionChildProfile,
        run: childRun,
        runtimeSessionID: childSessionBusinessID
      })
      const modelConfig = modelSelectionFromRuntimeConfig({
        provider: executionChildProfile.provider,
        model: executionChildProfile.model,
        binding: executionChildProfile.binding,
        credential: executionChildProfile.provider_credential_ref,
        inference: executionChildProfile.inference
      })
      const promptConfig = asRecord(executionChildProfile.prompt)
      await this.runPiHarnessWithLease(childRun, {
        sessionID: childSessionBusinessID,
        runtimeRunID: childRuntimeRunID,
        prompt: params.objective,
        history: [],
        agent: executionChildProfile.name,
        systemPrompt: firstString(promptConfig.system, promptConfig.system_prompt, promptConfig.systemPrompt),
        model: modelConfig ? { provider_id: modelConfig.provider_id, id: modelConfig.id } : undefined,
        modelConfig,
        tools: childResources.piResources.tools,
        skills: childResources.piResources.skills,
        skillInvocation: childResources.skillInvocation
      })
      assistantContent = textFromRuntimeEvents(await childEventStore.replayRun(childRuntimeRunID))
      validation = this.validateOutput(executionChildProfile, assistantContent)
      if (!validation.valid) {
        resultStatus = 'failed'
        errorMessage = validation.errors.join('; ')
      }
    } catch (error) {
      if (error instanceof PiToolApprovalRequiredError) {
        resultStatus = 'blocked_approval'
        assistantContent = ''
        validation = { valid: true, errors: [], structuredOutput: {} }
      } else {
        resultStatus = 'failed'
        errorMessage = classifyRuntimeError(error).user_message
        assistantContent = `子 Agent 调用失败：${errorMessage}`
        validation = { valid: false, errors: [errorMessage], structuredOutput: {} }
      }
    } finally {
      await progressSampler.flush()
      unsubscribeChildProgress()
    }

    // Parent cancellation is durable and may race with the child harness rejecting after abort.
    // Preserve that terminal state instead of rewriting the child as failed from the abort error.
    const durableChildRun = await this.store.getRun(params.actor.workspace_id, childRuntimeRunID)
    if (durableChildRun?.status === 'cancelled') {
      return {
        action_id: params.action.action_id,
        action_internal_id: params.action.id,
        execution_id: execution.execution_id,
        task_id: params.taskID,
        status: 'cancelled' as const,
        agent_profile_id: params.childProfile.id,
        agent_profile_version_id: params.childProfileVersionID,
        name: params.childProfile.name,
        summary: firstString(durableChildRun.error_msg, 'Sub-agent cancelled'),
        error: firstString(durableChildRun.error_msg, 'Sub-agent cancelled'),
        child_run_link_id: childRunLink.child_run_link_id,
        child_session_id: childSession.id,
        child_runtime_run_id: childRuntimeRunID,
        artifact_refs: Array.isArray(durableChildRun.result.artifact_refs) ? durableChildRun.result.artifact_refs : []
      }
    }

    let childEvents = await childEventStore.replayRun(childRuntimeRunID)

    const childAssistantEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: childSession.id,
      workspace_id: params.actor.workspace_id,
      user_id: params.actor.user_id,
      parent_entry_id: childUserEntry.id,
      seq: 2,
      entry_type: resultStatus === 'failed' ? 'error' : 'message',
      role: 'assistant',
      status: resultStatus === 'completed' ? 'completed' : resultStatus === 'blocked_approval' ? 'streaming' : 'failed',
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: {
        parent_action_id: params.action.action_id
      },
      output: {
        text: assistantContent,
        structured_output: validation.structuredOutput || {},
        output_schema_valid: validation.valid,
        output_schema_errors: validation.errors,
        runtime_events: childEvents,
        provider_raw_refs: providerRawRefs
      },
      runtime_run_id: childRuntimeRunID,
      event_seq: 2,
      idempotency_key: runtimeIdempotencyKey('entry', childSessionBusinessID, 'subagent', params.action.action_id, 'assistant'),
      created_at: timestamp,
      updated_at: now()
    }
    await this.store.saveEntry(childAssistantEntry)

    const transcript = {
      child_run_link_id: childRunLink.child_run_link_id,
      child_session_id: childSession.id,
      child_runtime_run_id: childRuntimeRunID,
      parent_runtime_run_id: params.parentRuntimeRunID,
      parent_action_id: params.action.action_id,
      task: {
        task_id: params.taskID,
        objective: params.objective,
        mode: params.mode
      },
      result: {
        status: resultStatus,
        summary: truncateSubagentSummary(assistantContent),
        structured_output: validation.structuredOutput || {},
        validation_errors: validation.errors,
        error: errorMessage || undefined
      },
      provider_raw_refs: providerRawRefs
    }
    const artifact = await this.saveRuntimeArtifact({
      workspaceID: params.actor.workspace_id,
      sessionID: childSession.id,
      runtimeRunID: childRuntimeRunID,
      actionID: params.action.action_id,
      visibility: 'parent_visible',
      artifactType: 'subagent_transcript',
      mimeType: 'application/json',
      storageRef: JSON.stringify(transcript),
      preview: {
        child_runtime_run_id: childRuntimeRunID,
        child_run_link_id: childRunLink.child_run_link_id,
        status: resultStatus,
        summary: truncateSubagentSummary(assistantContent, 500),
        validation_errors: validation.errors
      }
    })

    if (resultStatus === 'blocked_approval') {
      const pendingApprovals = pendingPiApprovalsFromRuntimeEvents(childEvents)
      const awaitingApproval = pendingApprovals[0] || {}
      await childEventStore.append({
        ...runStateEventPayload(childRuntimeRunID, 'awaiting_decision'),
        type: 'run.awaiting_decision',
        session_id: childSessionBusinessID,
        runtime_run_id: childRuntimeRunID,
        event_id: '',
        seq: 0,
        timestamp: now()
      } as unknown as AgentRuntimeEvent)
      childEvents = await childEventStore.replayRun(childRuntimeRunID)
      const embeddedChildEvents = summarizeRuntimeEventsForEntry(childEvents)
      const blockedAssistantEntry = await this.store.saveEntry({
        ...childAssistantEntry,
        output: {
          ...childAssistantEntry.output,
          runtime_events: embeddedChildEvents,
          awaiting_approval: awaitingApproval,
          awaiting_approvals: pendingApprovals,
          artifact_refs: [{
            artifact_id: artifact.artifact_id,
            artifact_type: artifact.artifact_type,
            preview_json: artifact.preview_json
          }]
        },
        event_seq: childEvents.length,
        updated_at: now()
      })
      childRun = await this.store.saveRun({
        ...childRun,
        status: 'awaiting_decision',
        output_entry_id: blockedAssistantEntry.id,
        result: {
          text: '',
          runtime_session_id: childSessionBusinessID,
          runtime_engine: 'pi',
          runtime_events: embeddedChildEvents,
          awaiting_approval: awaitingApproval,
          awaiting_approvals: pendingApprovals,
          artifact_refs: [{
            artifact_id: artifact.artifact_id,
            artifact_type: artifact.artifact_type,
            preview_json: artifact.preview_json
          }]
        },
        updated_at: now()
      })
      await this.store.saveSession({
        ...childSession,
        entry_count: 2,
        last_entry_at: childAssistantEntry.created_at,
        updated_at: now()
      })
      childRunLink = await this.store.saveChildRunLink({ ...childRunLink, status: 'blocked_approval' })
      execution = await this.store.saveActionExecution({
        ...execution,
        result_json: {
          child_run_link_id: childRunLink.child_run_link_id,
          child_session_id: childSession.id,
          child_runtime_run_id: childRuntimeRunID,
          status: 'blocked_approval',
          awaiting_approval: awaitingApproval
        },
        updated_at: now()
      })
      const blockedPayload = {
        action_id: params.action.action_id,
        action_internal_id: params.action.id,
        execution_id: execution.execution_id,
        task_id: params.taskID,
        status: 'blocked_approval',
        agent_profile_id: params.childProfile.id,
        agent_profile_version_id: params.childProfileVersionID,
        name: params.childProfile.name,
        summary: `子 Agent ${params.childProfile.name} 等待工具审批`,
        awaiting_approval: awaitingApproval,
        child_run_link_id: childRunLink.child_run_link_id,
        child_session_id: childSession.id,
        child_runtime_run_id: childRuntimeRunID,
        artifact_refs: [{
          artifact_id: artifact.artifact_id,
          artifact_type: artifact.artifact_type,
          preview_json: artifact.preview_json
        }]
      }
      await this.store.saveAction({
        ...params.action,
        status: 'awaiting_decision',
        result_json: blockedPayload,
        updated_at: now()
      })
      await params.recordParentEvent?.('subagent.blocked_approval', blockedPayload)
      return blockedPayload
    }

    const childRunStateEventType = runStateEventTypeForStatus(resultStatus)
    await childEventStore.append({
      type: resultStatus === 'completed' ? 'subagent.completed' : 'subagent.failed',
      session_id: childSessionBusinessID,
      runtime_run_id: childRuntimeRunID,
      event_id: '',
      seq: 0,
      timestamp: now(),
      payload: {
        child_run_link_id: childRunLink.child_run_link_id,
        child_runtime_run_id: childRuntimeRunID,
        parent_runtime_run_id: params.parentRuntimeRunID,
        parent_action_id: params.action.action_id,
        parent_action_internal_id: params.action.id,
        status: resultStatus,
        summary: truncateSubagentSummary(assistantContent),
        error: errorMessage || undefined,
        artifact_refs: [{
          artifact_id: artifact.artifact_id,
          artifact_type: artifact.artifact_type,
          preview_json: artifact.preview_json
        }]
      }
    } as unknown as AgentRuntimeEvent)
    await childEventStore.append({
      ...runStateEventPayload(childRuntimeRunID, resultStatus, resultStatus === 'failed' ? 'subagent_failed' : '', errorMessage),
      type: childRunStateEventType,
      session_id: childSessionBusinessID,
      runtime_run_id: childRuntimeRunID,
      event_id: '',
      seq: 0,
      timestamp: now()
    } as unknown as AgentRuntimeEvent)
    childEvents = await childEventStore.replayRun(childRuntimeRunID)
    const allChildEvents = summarizeRuntimeEventsForEntry(childEvents)
    const completedChildAssistantEntry = await this.store.saveEntry({
      ...childAssistantEntry,
      output: {
        ...childAssistantEntry.output,
        runtime_events: allChildEvents
      },
      updated_at: now()
    })

    childRun = await this.store.saveRun({
      ...childRun,
      status: resultStatus,
      output_entry_id: completedChildAssistantEntry.id,
      result: {
        text: assistantContent,
        structured_output: validation.structuredOutput || {},
        output_schema_valid: validation.valid,
        output_schema_errors: validation.errors,
        runtime_events: allChildEvents,
        artifact_refs: [{
          artifact_id: artifact.artifact_id,
          artifact_type: artifact.artifact_type,
          preview_json: artifact.preview_json
        }],
        provider_raw_refs: providerRawRefs
      },
      usage: {},
      error_code: resultStatus === 'failed' ? 'subagent_failed' : undefined,
      error_msg: resultStatus === 'failed' ? errorMessage : undefined,
      finished_at: now(),
      updated_at: now()
    })
    await this.store.saveSession({
      ...childSession,
      entry_count: 2,
      last_entry_at: childAssistantEntry.created_at,
      updated_at: now()
    })
    childRunLink = await this.store.saveChildRunLink({
      ...childRunLink,
      status: resultStatus,
      completed_at: now()
    })
    execution = await this.store.saveActionExecution({
      ...execution,
      status: resultStatus === 'completed' ? 'succeeded' : 'failed',
      result_json: {
        child_run_link_id: childRunLink.child_run_link_id,
        child_session_id: childSession.id,
        child_runtime_run_id: childRuntimeRunID,
        summary: truncateSubagentSummary(assistantContent),
        artifact_id: artifact.artifact_id
      },
      error_code: resultStatus === 'failed' ? 'subagent_failed' : undefined,
      error_msg: resultStatus === 'failed' ? errorMessage : undefined,
      finished_at: now(),
      updated_at: now()
    })

    const terminalPayload = {
      action_id: params.action.action_id,
      action_internal_id: params.action.id,
      execution_id: execution.execution_id,
      task_id: params.taskID,
      status: resultStatus,
      agent_profile_id: params.childProfile.id,
      agent_profile_version_id: params.childProfileVersionID,
      name: params.childProfile.name,
      summary: truncateSubagentSummary(assistantContent),
      structured_output: validation.structuredOutput || {},
      validation_errors: validation.errors,
      error: errorMessage || undefined,
      child_run_link_id: childRunLink.child_run_link_id,
      child_session_id: childSession.id,
      child_runtime_run_id: childRuntimeRunID,
      artifact_refs: [{
        artifact_id: artifact.artifact_id,
        artifact_type: artifact.artifact_type,
        preview_json: artifact.preview_json
      }]
    }
    await this.store.saveAction({
      ...params.action,
      status: resultStatus === 'completed' ? 'executed' : 'failed',
      executed_at: now(),
      result_json: terminalPayload,
      error_msg: resultStatus === 'completed' ? undefined : firstString(errorMessage, 'Sub-agent failed'),
      updated_at: now()
    })
    return terminalPayload
  }

  private validateOutput(profile: AgentProfileSnapshot, text: string): OutputValidationResult {
    const schema = asRecord(profile.output_schema)
    const structuredOutput = coerceStructuredOutput(text, schema)
    if (!hasSchema(schema)) {
      return { structuredOutput, valid: true, errors: [] }
    }
    if (!structuredOutput) {
      return { valid: false, errors: ['structured output is required'] }
    }
    const errors = validateAgainstSchema(schema, structuredOutput)
    return {
      structuredOutput,
      valid: errors.length === 0,
      errors
    }
  }

  private async processToolCalls(
    actor: RuntimeActor,
    session: AISession,
    runtimeRunID: string,
    profile: AgentProfileSnapshot,
    toolCalls: Array<RuntimeToolCall | ChatModelToolCall>,
    existingToolCallIDs: Set<string> = new Set(),
    auth?: RuntimeAuth,
    emitEvents?: RuntimeEventSink,
    toolDefinitions: ChatModelToolDefinition[] = []
  ) {
    const actions: Record<string, unknown>[] = []
    const pendingActions: Record<string, unknown>[] = []
    const toolResults: RuntimeToolResultRecord[] = []
    const events: RuntimeEventRecord[] = []
    const definitions = runtimeToolDefinitionsByName(toolDefinitions)
    for (const rawCall of normalizeRuntimeToolCalls(toolCalls)) {
      const call = applyToolDefinitionMetadata(rawCall, definitions)
      const semanticKey = toolCallSemanticKey(call)
      if (existingToolCallIDs.has(call.tool_call_id) || existingToolCallIDs.has(semanticKey)) continue
      existingToolCallIDs.add(call.tool_call_id)
      existingToolCallIDs.add(semanticKey)
      events.push({
        type: 'model.tool_call_detected',
        payload: toolCallEventPayload(call)
      })
      if (emitEvents) await emitEvents([events[events.length - 1]])
      const timestamp = now()
      const actionID = await this.store.nextActionId()
      const capabilityID = requireCapabilityID(call.tool_name)
      const actionKind = actionKindForToolCall(call)
      const targetJSON = actionTargetForToolCall(call)
      const permission = toolPermissionForCall(profile, call, actor.workspace_role)
      let needsDecision = permission.decision === 'ask'
      const policyJSON: Record<string, unknown> = actionPolicyForToolCall(call, permission)
      if (needsDecision) {
        const sessionGrant = await this.findMatchingSessionGrant({
          actor,
          session,
          toolCall: call,
          actionKind,
          target: targetJSON,
          policy: policyJSON
        })
        if (sessionGrant) {
          needsDecision = false
          policyJSON.decision = 'allow'
          policyJSON.requires_decision = false
          policyJSON.session_grant_id = sessionGrant.grant_id
          policyJSON.policy_sources = [...(Array.isArray(policyJSON.policy_sources) ? policyJSON.policy_sources : []), 'session_grant']
        }
      }
      const displayJSON = actionDisplayForToolCall(call, actionKind, policyJSON, targetJSON)
      const action: AgentAction = {
        id: actionID,
        action_id: actionBusinessIDFor(runtimeRunID, actionID),
        workspace_id: actor.workspace_id,
        context_tags: [...session.context_tags],
        session_id: session.id,
        runtime_run_id: runtimeRunID,
        action_kind: actionKind,
        idempotency_key: runtimeIdempotencyKey('action', runtimeRunID, `turn${seq36(1)}`, `part${seq36(actionID)}`, capabilityID),
        source: actionSourceForToolCall(call),
        capability_id: capabilityID,
        input_json: {
          provider_tool_call_id: call.tool_call_id,
          tool_name: call.tool_name,
          mcp_server_key: call.mcp_server_key,
          mcp_server_id: call.mcp_server_id,
          capability: call.capability,
          arguments: call.arguments,
          raw_tool_call: {
            operation_type: call.operation_type,
            target_type: call.target_type,
            target_id: call.target_id,
            mcp_server_key: call.mcp_server_key,
            mcp_server_id: call.mcp_server_id,
            capability: call.capability
          }
        },
        input_digest: '',
        target_json: targetJSON,
        policy_json: policyJSON,
        display_json: displayJSON,
        status: permission.decision === 'deny' ? 'rejected' : needsDecision ? 'awaiting_decision' : 'executing',
        requested_by: actor.user_id,
        result_json: {},
        created_at: timestamp,
        updated_at: timestamp
      }
      action.input_digest = actionInputDigest(action.input_json)
      let saved = await this.store.saveAction(action)
      if (permission.decision === 'deny') {
        const publicAgentAction = publicAction(saved)
        actions.push(publicAgentAction)
        const deniedResult = await this.createToolResultRecord({
          actor,
          session,
          runtimeRunID,
          actionID: saved.action_id,
          toolCall: call,
          status: 'failed',
          content: permission.reason,
          error: permission.reason
        })
        toolResults.push(deniedResult)
        events.push({
          type: 'action.permission_evaluated',
          payload: {
            action_id: saved.action_id,
            action_internal_id: saved.id,
            decision: 'deny',
            permission_key: permission.permission_key,
            matched_rule: permission.matched_rule,
            scope: permission.scope,
            mcp_server_key: call.mcp_server_key,
            mcp_server_id: call.mcp_server_id,
            capability: call.capability,
            reason: permission.reason
          }
        })
        if (emitEvents) await emitEvents([events[events.length - 1]])
        events.push({
          type: 'tool.rejected',
          payload: {
            ...toolResultEventPayload(deniedResult),
            action_id: saved.action_id,
            action_internal_id: saved.id,
            action: publicAgentAction
          }
        })
        if (emitEvents) await emitEvents([events[events.length - 1]])
        continue
      }
      if (!needsDecision) {
        const attempt = await this.store.nextActionExecutionAttempt(saved.id)
        let execution: ActionExecution = await this.store.saveActionExecution({
          execution_id: executionBusinessID(saved, attempt),
          workspace_id: actor.workspace_id,
          session_id: session.id,
          runtime_run_id: runtimeRunID,
          action_id: saved.id,
          attempt,
          executor_type: actionKind === 'pipeline.trigger' ? 'pipeline' : actionKind === 'subagent.spawn' ? 'subagent' : 'mcp',
          executor_ref_json: {
            action_kind: actionKind,
            capability_id: capabilityID,
            tool_name: call.tool_name,
            target: targetJSON
          },
          idempotency_key: runtimeIdempotencyKey('execution', saved.action_id, `attempt${seq36(attempt)}`),
          status: 'running',
          result_json: {},
          started_at: timestamp,
          created_at: timestamp,
          updated_at: timestamp
        })
        events.push({
          type: 'action.execution_started',
          payload: {
            action_id: saved.action_id,
            action_internal_id: saved.id,
            action: publicAction(saved),
            execution_id: execution.execution_id,
            attempt: execution.attempt,
            status: execution.status
          }
        })
        if (emitEvents) await emitEvents([events[events.length - 1]])
        try {
          const result = await this.toolExecutor.execute(call, { actor, session, runtime_run_id: runtimeRunID, action: saved, auth })
          execution = await this.store.saveActionExecution({
            ...execution,
            status: 'succeeded',
            external_request_id: firstString(result.metadata?.request_id, result.metadata?.mcp_request_id),
            result_json: result.structured_content || {},
            finished_at: now(),
            updated_at: now()
          })
          saved = await this.store.saveAction({
            ...saved,
            status: 'executed',
            executed_at: now(),
            result_json: {
              structured_content: result.structured_content || {},
              metadata: result.metadata || {},
              external_request_id: firstString(result.metadata?.request_id, result.metadata?.mcp_request_id)
            },
            updated_at: now()
          })
          const publicAgentAction = publicAction(saved)
          actions.push(publicAgentAction)
          const toolResult = await this.createToolResultRecord({
            actor,
            session,
            runtimeRunID,
            actionID: saved.action_id,
            toolCall: call,
            status: 'completed',
            content: result.content,
            structuredContent: result.structured_content || {},
            metadata: result.metadata || {}
          })
          toolResults.push(toolResult)
          events.push({
            type: 'tool.executed',
            payload: {
              event_id: stableEventID(runtimeRunID, 'action', saved.id, 'tool', 'executed'),
              action_id: saved.action_id,
              action_internal_id: saved.id,
              action: publicAgentAction,
              provider_tool_call_id: call.tool_call_id,
              result: toolResult.structured_content,
              content: toolResult.content,
              metadata: toolResult.metadata
            }
          })
          if (emitEvents) await emitEvents([events[events.length - 1]])
          events.push({
            type: 'tool.result_prepared',
            payload: {
              ...toolResultEventPayload(toolResult),
              action_id: saved.action_id,
              action_internal_id: saved.id
            }
          })
          if (emitEvents) await emitEvents([events[events.length - 1]])
          events.push({
            type: 'action.execution_succeeded',
            payload: {
              action_id: saved.action_id,
              action_internal_id: saved.id,
              action: publicAgentAction,
              execution_id: execution.execution_id,
              attempt: execution.attempt,
              status: execution.status,
              result: execution.result_json
            }
          })
          if (emitEvents) await emitEvents([events[events.length - 1]])
        } catch (error) {
          const message = error instanceof Error ? error.message : 'Tool execution failed'
          execution = await this.store.saveActionExecution({
            ...execution,
            status: 'failed',
            error_code: 'tool_execution_failed',
            error_msg: message,
            finished_at: now(),
            updated_at: now()
          })
          saved = await this.store.saveAction({
            ...saved,
            status: 'failed',
            error_msg: message,
            updated_at: now()
          })
          const publicAgentAction = publicAction(saved)
          actions.push(publicAgentAction)
          const toolResult = await this.createToolResultRecord({
            actor,
            session,
            runtimeRunID,
            actionID: saved.action_id,
            toolCall: call,
            status: 'failed',
            content: message,
            structuredContent: {},
            metadata: {},
            error: message
          })
          toolResults.push(toolResult)
          events.push({
            type: 'tool.result_prepared',
            payload: {
              ...toolResultEventPayload(toolResult),
              action_id: saved.action_id,
              action_internal_id: saved.id
            }
          })
          if (emitEvents) await emitEvents([events[events.length - 1]])
          events.push({
            type: 'tool.failed',
            payload: {
              event_id: stableEventID(runtimeRunID, 'action', saved.id, 'tool', 'failed'),
              action_id: saved.action_id,
              action_internal_id: saved.id,
              action: publicAgentAction,
              provider_tool_call_id: call.tool_call_id,
              error: message
            }
          })
          if (emitEvents) await emitEvents([events[events.length - 1]])
          events.push({
            type: 'action.execution_failed',
            payload: {
              action_id: saved.action_id,
              action_internal_id: saved.id,
              action: publicAgentAction,
              execution_id: execution.execution_id,
              attempt: execution.attempt,
              status: execution.status,
              error: message
            }
          })
          if (emitEvents) await emitEvents([events[events.length - 1]])
        }
        continue
      }
      const publicAgentAction = publicAction(saved)
      actions.push(publicAgentAction)
      pendingActions.push(publicAgentAction)
      const approvalRequest = this.approvalRequestForAction(saved, new Date(Date.now() + APPROVAL_TTL_MS).toISOString())
      saved = await this.store.saveAction({
        ...saved,
        display_json: {
          ...displayJSON,
          approval_request: approvalRequest
        },
        updated_at: now()
      })
      const updatedPublicAction = publicAction(saved)
      actions[actions.length - 1] = updatedPublicAction
      pendingActions[pendingActions.length - 1] = updatedPublicAction
      events.push({
        type: 'action.permission_evaluated',
        payload: {
          action_id: saved.action_id,
          action_internal_id: saved.id,
          decision: 'ask',
          permission_key: approvalRequest.permission_key,
          risk_level: approvalRequest.risk_level,
          mcp_server_key: call.mcp_server_key,
          mcp_server_id: call.mcp_server_id,
          capability: call.capability,
          reason: approvalRequest.reason
        }
      })
      if (emitEvents) await emitEvents([events[events.length - 1]])
      events.push({
        type: 'approval.requested',
        payload: {
          event_id: stableEventID(runtimeRunID, 'action', saved.id, 'approval', 'requested'),
          approval_id: approvalRequest.approval_id,
          action_id: saved.action_id,
          action_internal_id: saved.id,
          approval_request: approvalRequest
        }
      })
      if (emitEvents) await emitEvents([events[events.length - 1]])
      events.push({
        type: 'action.decision_required',
        payload: {
          event_id: stableEventID(runtimeRunID, 'action', saved.id, 'decision_required'),
          action_id: saved.action_id,
          action_internal_id: saved.id,
          action: updatedPublicAction
        }
      })
      if (emitEvents) await emitEvents([events[events.length - 1]])
    }
    return { actions, pendingActions, toolResults, events }
  }

  private async continueAutomaticToolLoop(params: {
    actor: RuntimeActor
    session: AISession
    runtimeRunID: string
    requestID?: string
    parentRuntimeRunID?: string
    profile: AgentProfileSnapshot
    history: AISessionEntry[]
    executionContext: ExecutionContext
    assistantContent: string
    completion: ChatModelResult | null
    initialToolCalls: Array<RuntimeToolCall | ChatModelToolCall>
    existingToolCallIDs?: Set<string>
    auth?: RuntimeAuth
    emitEvents?: RuntimeEventSink
  }) {
    let assistantContent = params.assistantContent
    let completion = params.completion
    const agentActions: Record<string, unknown>[] = []
    const toolResults: RuntimeToolResultRecord[] = []
    const events: RuntimeEventRecord[] = []
    const existingToolCallIDs = params.existingToolCallIDs || new Set<string>()
    let lastContinuationRound = 1

    for (let round = 0; round < 4; round += 1) {
      const modelToolCalls = round === 0
        ? params.initialToolCalls
        : [
            ...(completion?.tool_calls || []),
            ...extractToolCallsFromText(assistantContent)
          ]
      if (modelToolCalls.length === 0) break
      if (isToolCallPayloadText(assistantContent)) {
        assistantContent = ''
      }
      const processed = await this.processToolCalls(
        params.actor,
        params.session,
        params.runtimeRunID,
        params.profile,
        modelToolCalls,
        existingToolCallIDs,
        params.auth,
        params.emitEvents,
        params.executionContext.mcpTools
      )
      agentActions.push(...processed.actions)
      toolResults.push(...processed.toolResults)
      events.push(...processed.events)
      if (processed.pendingActions.length > 0 || processed.toolResults.length === 0) break

      const continuationRequest: ChatModelRequest = {
        profile: params.profile,
        session: params.session,
        history: params.history,
        content: buildToolResultContinuationContent(processed.toolResults),
        request_id: params.requestID,
        runtime_run_id: params.runtimeRunID,
        parent_runtime_run_id: params.parentRuntimeRunID,
        context_ref: {},
        include_current_page: false,
        tools: params.executionContext.mcpTools,
        capabilities: params.executionContext.capabilities,
        loaded_skills: params.executionContext.loadedSkills,
        subagent_results: params.executionContext.subagentResults,
        output_schema: params.executionContext.outputSchema,
        developer_instructions: [
          params.executionContext.developerInstructions,
          'Continue after the automatic read-only MCP tool result. Use the tool result to answer the user. If another tool is needed, request it with tool_calls; otherwise provide the final answer.'
        ].filter(Boolean).join('\n\n')
      }
      const continuationOptions: ModelCallEventOptions = { phase: 'tool_continuation', round: round + 2 }
      lastContinuationRound = continuationOptions.round || lastContinuationRound
      events.push({
        type: 'model.tool_result_submitted',
        payload: {
          ...modelRequestEventPayload(continuationRequest, continuationOptions),
          tool_result_count: processed.toolResults.length,
          tool_results: processed.toolResults.map((toolResult) => toolResultEventPayload(toolResult))
        }
      })
      if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
      events.push({
        type: 'model.continuation_started',
        payload: {
          ...modelRequestEventPayload(continuationRequest, continuationOptions),
          tool_result_count: processed.toolResults.length
        }
      })
      if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
      const continuationTurn = await this.completeWithBoundedContinuation(continuationRequest, continuationOptions)
      completion = continuationTurn.completion
      events.push(...continuationTurn.events)
      if (params.emitEvents && continuationTurn.events.length > 0) {
        await params.emitEvents(continuationTurn.events)
      }
      events.push({
        type: 'model.continuation_completed',
        payload: {
          ...modelCompletionEventPayload(completion, continuationOptions),
          tool_result_count: processed.toolResults.length
        }
      })
      if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
      assistantContent = completion?.text || ''
    }
    const unresolvedToolCalls = [
      ...(completion?.tool_calls || []),
      ...extractToolCallsFromText(assistantContent)
    ]
    if (!assistantContent.trim() && toolResults.length > 0 && unresolvedToolCalls.length > 0) {
      const finalAnswerRequest: ChatModelRequest = {
        profile: params.profile,
        session: params.session,
        history: params.history,
        content: [
          buildToolResultContinuationContent(toolResults),
          '',
          'automatic_tool_loop_recovery:',
          JSON.stringify({
            reason: 'model_returned_tool_calls_without_visible_answer',
            unresolved_tool_call_count: unresolvedToolCalls.length
          })
        ].join('\n'),
        request_id: params.requestID,
        runtime_run_id: params.runtimeRunID,
        parent_runtime_run_id: params.parentRuntimeRunID,
        context_ref: {},
        include_current_page: false,
        tools: [],
        capabilities: params.executionContext.capabilities,
        loaded_skills: params.executionContext.loadedSkills,
        subagent_results: params.executionContext.subagentResults,
        output_schema: params.executionContext.outputSchema,
        developer_instructions: [
          params.executionContext.developerInstructions,
          'The automatic read-only tool loop has already executed the available tool calls. Do not request more tools in this turn. Use the tool results above to provide the final assistant-visible answer. If a tool failed or found no records, say that clearly.'
        ].filter(Boolean).join('\n\n')
      }
      const finalAnswerOptions: ModelCallEventOptions = {
        phase: 'tool_continuation',
        round: lastContinuationRound + 1
      }
      events.push({
        type: 'model.tool_result_submitted',
        payload: {
          ...modelRequestEventPayload(finalAnswerRequest, finalAnswerOptions),
          tool_result_count: toolResults.length,
          recovery_reason: 'unresolved_tool_calls',
          tool_results: toolResults.map((toolResult) => toolResultEventPayload(toolResult))
        }
      })
      if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
      events.push({
        type: 'model.continuation_started',
        payload: {
          ...modelRequestEventPayload(finalAnswerRequest, finalAnswerOptions),
          tool_result_count: toolResults.length,
          recovery_reason: 'unresolved_tool_calls'
        }
      })
      if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
      try {
        const finalAnswerTurn = await this.completeWithBoundedContinuation(finalAnswerRequest, finalAnswerOptions)
        completion = finalAnswerTurn.completion
        events.push(...finalAnswerTurn.events)
        if (params.emitEvents && finalAnswerTurn.events.length > 0) {
          await params.emitEvents(finalAnswerTurn.events)
        }
        events.push({
          type: 'model.continuation_completed',
          payload: {
            ...modelCompletionEventPayload(completion, finalAnswerOptions),
            tool_result_count: toolResults.length,
            recovery_reason: 'unresolved_tool_calls'
          }
        })
        if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
        assistantContent = completion?.text || ''
        if (isInternalContinuationPlan(assistantContent)) {
          assistantContent = ''
        }
      } catch (error) {
        if (!(error instanceof ModelProviderError && error.code === 'provider_response_empty')) {
          throw error
        }
        events.push({
          type: 'provider.empty_output_detected',
          payload: {
            status: 'fallback_summary',
            details: this.providerEmptyOutputDetails(error, 2),
            recovery_reason: 'unresolved_tool_calls'
          }
        })
        if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
      }
    }
    if (!assistantContent.trim() && toolResults.length > 0) {
      assistantContent = toolResultsFallbackSummary(toolResults)
      events.push({
        type: 'model.answer_synthesized',
        payload: {
          source: 'tool_results',
          reason: 'empty_assistant_after_tool_loop',
          tool_result_count: toolResults.length,
          text_chars: assistantContent.length
        }
      })
      if (params.emitEvents) await params.emitEvents([events[events.length - 1]])
    }

    return { assistantContent, completion, agentActions, toolResults, events }
  }

  async createEntryRun(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>, auth?: RuntimeAuth) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const content = asString(payload.content).trim()
    if (!content) {
      throw new RuntimeDomainError('entry_content_required', 'Entry content is required')
    }
    const {
      resolvedProfile,
      persistedProfileSnapshot,
      persistedProfileSnapshotHash,
      executionProfileSnapshot: profileSnapshot
    } = await this.profileSnapshotsForNewRun(actor, session)
    await this.contextTagRegistry.resolve(profileSnapshot.context_tags, { actor, profile: profileSnapshot, content, payload })
    enforceChatboxInputLimit(profileSnapshot, content)
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const publicSessionID = sessionBusinessID(actor.workspace_id, session.id)
    const clientEntryID = requireClientID('client_entry_id', payload.client_entry_id)
    const requestID = firstString(payload.runtime_request_id)
    const userEntryIdempotencyKey = runtimeIdempotencyKey('entry', publicSessionID, 'client', clientEntryID)
    const existingUserEntry = entries.find((entry) => entry.idempotency_key === userEntryIdempotencyKey)
    const existingAssistantEntry = existingUserEntry
      ? entries.find((entry) => entry.parent_entry_id === existingUserEntry.id && entry.role === 'assistant')
      : undefined
    if (existingUserEntry && existingAssistantEntry) {
      const existingRun = existingAssistantEntry.runtime_run_id
        ? await this.store.getRun(actor.workspace_id, existingAssistantEntry.runtime_run_id)
        : undefined
      return {
        run: existingRun || null,
        user_entry: existingUserEntry,
        assistant_entry: existingAssistantEntry,
        idempotent: true
      }
    }
    if (this.shouldUsePiRuntime(payload)) {
      if (!firstString(payload.queue_item_id)) await this.assertSessionHasNoActiveRun(actor, session)
      return this.createPiEntryRun(actor, session, persistedProfileSnapshot, profileSnapshot, resolvedProfile, entries, publicSessionID, content, payload, auth)
    }
    await this.assertSessionHasNoActiveRun(actor, session)
    const seqBase = entries.length
    const timestamp = now()
    const userEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: seqBase + 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content,
      content_blocks: [{ type: 'text', text: content }],
      input: {
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        ...(requestID ? { request_id: requestID } : {})
      },
      output: {},
      idempotency_key: userEntryIdempotencyKey,
      created_at: timestamp,
      updated_at: timestamp
    }

    const runID = await this.store.nextRunId()
    const runtimeRunID = runBusinessID(actor.workspace_id, session.id, runID)
    let run: AIRuntimeRun = {
      id: runID,
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [...profileSnapshot.context_tags],
      agent_profile_id: resolvedProfile.profile.id,
      agent_profile_version_id: resolvedProfile.profileVersionID,
      agent_profile_version_key: resolvedProfile.profileVersionKey,
      agent_profile_snapshot_hash: persistedProfileSnapshotHash,
      agent_workspace_runtime_id: session.agent_workspace_runtime_id,
      agent_workspace_snapshot_hash: session.agent_workspace_snapshot_hash,
      profile_snapshot: persistedProfileSnapshot,
      status: 'running',
      input_entry_id: userEntry.id,
      request: {
        content,
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        agent_profile_version_key: resolvedProfile.profileVersionKey,
        ...(requestID ? { request_id: requestID } : {}),
        ...(firstString(payload.parent_runtime_run_id) ? { parent_runtime_run_id: firstString(payload.parent_runtime_run_id) } : {})
      },
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    }
    run = await this.store.saveRun(run)
    await this.store.saveEntry(userEntry)
    this.enqueueSessionTitleGeneration(actor, session, profileSnapshot, content, entries.length)
    const executionContext = await this.prepareExecutionContext(actor, session, profileSnapshot, content, payload, runtimeRunID, auth)
    run = await this.store.saveRun({
      ...run,
      request: {
        ...run.request,
        model_content: executionContext.modelContent,
        context_tags: executionContext.contextTagAssembly.tags,
        context_tag_context: executionContext.contextTagAssembly.fragments.map((fragment) => ({
          tag: fragment.tag,
          order: fragment.order,
          required: fragment.required,
          title: fragment.title,
          data: fragment.data
        })),
        profile_snapshot: persistedProfileSnapshot
      },
      updated_at: now()
    })
    executionContext.runtimeEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, executionContext.runtimeEvents)

    let completion: ChatModelResult | null = null
    let assistantContent = ''
    let assistantStatus: AISessionEntry['status'] = 'completed'
    let assistantEntryType: AISessionEntry['entry_type'] = 'message'
    let runStatus: AIRuntimeRun['status'] = 'completed'
    let errorCode = ''
    let errorMessage = ''
    let agentActions: Record<string, unknown>[] = [...executionContext.agentActions]
    let providerRawRefs: Record<string, unknown>[] = []
    try {
      const initialModelRequest: ChatModelRequest = {
        profile: profileSnapshot,
        session,
        history: entries,
        content: executionContext.modelContent,
        request_id: requestID,
        runtime_run_id: runtimeRunID,
        parent_runtime_run_id: firstString(payload.parent_runtime_run_id),
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        tools: executionContext.mcpTools,
        capabilities: executionContext.capabilities,
        loaded_skills: executionContext.loadedSkills,
        subagent_results: executionContext.subagentResults,
        output_schema: executionContext.outputSchema,
        developer_instructions: executionContext.developerInstructions
      }
      executionContext.runtimeEvents.push(...await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
        type: 'model.request_prepared',
        payload: modelRequestEventPayload(initialModelRequest, { phase: 'initial', round: 1 })
      }]))
      const modelTurn = await this.completeWithBoundedContinuation(initialModelRequest, { phase: 'initial', round: 1 })
      completion = modelTurn.completion
      if (modelTurn.events.length > 0) {
        executionContext.runtimeEvents.push(...await this.persistRuntimeEvents(actor, session, runtimeRunID, modelTurn.events))
      }
      const providerRaw = await this.persistProviderRawFromCompletion(actor, session, runtimeRunID, completion, 1)
      providerRawRefs = [...providerRawRefs, ...providerRaw.refs]
      if (providerRaw.events.length > 0) {
        executionContext.runtimeEvents.push(...await this.persistRuntimeEvents(actor, session, runtimeRunID, providerRaw.events))
      }
      assistantContent = completion?.text || ''
    } catch (error) {
      runStatus = 'failed'
      assistantStatus = 'failed'
      assistantEntryType = 'error'
      errorCode = error instanceof ModelProviderError ? error.code : 'model_provider_error'
      errorMessage = error instanceof Error ? error.message : 'Model provider failed'
      assistantContent = `模型调用失败：${errorMessage}`
    }
    let toolResults: RuntimeToolResultRecord[] = []
    if (runStatus === 'completed') {
      const toolLoop = await this.continueAutomaticToolLoop({
        actor,
        session,
        runtimeRunID,
        requestID,
        parentRuntimeRunID: firstString(payload.parent_runtime_run_id),
        profile: profileSnapshot,
        history: [...entries, userEntry],
        executionContext,
        assistantContent,
        completion,
        initialToolCalls: [
          ...(completion?.tool_calls || []),
          ...extractToolCallsFromText(assistantContent)
        ],
        auth
      })
      completion = toolLoop.completion
      assistantContent = toolLoop.assistantContent
      agentActions = toolLoop.agentActions
      toolResults = toolLoop.toolResults
      if ((toolLoop.events.length > 0 || toolLoop.toolResults.length > 0) && toolLoop.completion) {
        const providerRaw = await this.persistProviderRawFromCompletion(actor, session, runtimeRunID, toolLoop.completion, 2)
        providerRawRefs = [...providerRawRefs, ...providerRaw.refs]
        executionContext.runtimeEvents.push(...await this.persistRuntimeEvents(actor, session, runtimeRunID, providerRaw.events))
      }
      executionContext.runtimeEvents.push(...await this.persistRuntimeEvents(actor, session, runtimeRunID, toolLoop.events))
    }
    const outputValidation = runStatus === 'completed'
      ? this.validateOutput(profileSnapshot, assistantContent)
      : { valid: false, errors: [errorMessage || errorCode], structuredOutput: undefined }
    const validationEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
      type: outputValidation.valid ? 'output_schema.validated' : 'output_schema.invalid',
      payload: {
        valid: outputValidation.valid,
        errors: outputValidation.errors,
        has_schema: hasSchema(executionContext.outputSchema)
      }
    }])
    executionContext.runtimeEvents.push(...validationEvents)
    if (!outputValidation.valid && hasSchema(executionContext.outputSchema)) {
      runStatus = 'failed'
      assistantStatus = 'failed'
      assistantEntryType = 'error'
      errorCode = 'output_schema_invalid'
      errorMessage = outputValidation.errors.join('; ')
    }
    if (runStatus === 'completed' && hasAwaitingDecisionAction(agentActions)) {
      runStatus = 'awaiting_decision'
    }
    const runStateEventType = runStateEventTypeForStatus(runStatus)
    if (runStateEventType) {
      executionContext.runtimeEvents.push(...await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
        type: runStateEventType,
        payload: runStateEventPayload(runtimeRunID, runStatus, errorCode, errorMessage)
      }]))
    }
    const assistantEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: seqBase + 2,
      entry_type: assistantEntryType,
      role: 'assistant',
      status: assistantStatus,
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: {},
      output: {
        text: assistantContent,
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: executionContext.runtimeEvents,
        capabilities: executionContext.capabilities,
        loaded_skills: executionContext.loadedSkills,
        subagent_results: executionContext.subagentResults,
        agent_actions: agentActions,
        tool_results: toolResults,
        provider_raw_refs: providerRawRefs
      },
      runtime_run_id: runtimeRunID,
      event_seq: 1,
      idempotency_key: entryRuntimeIdempotencyKey(publicSessionID, runtimeRunID, 'assistant', seqBase + 2),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(assistantEntry)

    const completedRun: AIRuntimeRun = {
      ...run,
      status: runStatus,
      output_entry_id: assistantEntry.id,
      result: {
        text: assistantContent,
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: executionContext.runtimeEvents,
        capabilities: executionContext.capabilities,
        loaded_skills: executionContext.loadedSkills,
        subagent_results: executionContext.subagentResults,
        agent_actions: agentActions,
        tool_results: toolResults,
        provider_raw_refs: providerRawRefs
      },
      usage: completion?.usage || {},
      error_code: errorCode || undefined,
      error_msg: errorMessage || undefined,
      finished_at: isTerminalRunStatus(runStatus) ? now() : undefined,
      updated_at: now()
    }
    const savedCompletedRun = await this.store.saveRun(completedRun)
    await this.saveSessionProgress(actor, session, {
      entry_count: seqBase + 2,
      last_entry_at: assistantEntry.created_at
    })

    return {
      run: savedCompletedRun,
      user_entry: userEntry,
      assistant_entry: assistantEntry
    }
  }

  private async enrichPiSkillResources(profile: AgentProfileSnapshot, resources: AgentResource[], skillNames: Iterable<string> = []) {
    const selectedSkillIDs = new Set(
      (profile.skills || [])
        .map((ref) => normalizeResourceID(ref.resource_id))
        .filter(Boolean)
    )
    if (selectedSkillIDs.size === 0) return resources
    const requestedSkillIDs = new Set(
      Array.from(skillNames)
        .map((name) => normalizeResourceID(name))
        .filter(Boolean)
    )
    if (requestedSkillIDs.size === 0) return resources

    return Promise.all(resources.map(async (resource) => {
      if (resource.resource_kind !== 'skill') return resource
      const isSelected = selectedSkillIDs.has(normalizeResourceID(resource.id)) ||
        selectedSkillIDs.has(normalizeResourceID(resource.resource_key)) ||
        selectedSkillIDs.has(normalizeResourceID(resource.resource_id))
      if (!isSelected) return resource
      const isRequested = requestedSkillIDs.has(normalizeResourceID(resource.name)) ||
        requestedSkillIDs.has(normalizeResourceID(resource.resource_key)) ||
        requestedSkillIDs.has(normalizeResourceID(resource.resource_id)) ||
        requestedSkillIDs.has(normalizeResourceID(resource.id))
      if (!isRequested) return resource

      const spec = asRecord(resource.spec)
      if (firstString(spec.instructions, spec.markdown, spec.content, spec.prompt)) return resource

      const repositoryKey = firstString(spec.source_repository)
      const entry = firstString(spec.entry, spec.path)
      const repository = resources.find((candidate) =>
        candidate.resource_kind === 'skill' &&
        normalizeResourceID(candidate.resource_key) === normalizeResourceID(repositoryKey) &&
        firstString(asRecord(candidate.spec).resource_subtype) === 'skill_repository'
      )
      const discoveredContent = this.skillContentFromRepositoryScan(repository, resource)
      if (discoveredContent) {
        return { ...resource, spec: { ...spec, content: discoveredContent, instructions: discoveredContent } }
      }
      if (!repository || !entry) return resource

      try {
        const skillContent = await readSkillRepositoryEntry(repository, entry)
        return { ...resource, spec: { ...spec, content: skillContent, instructions: skillContent } }
      } catch {
        return resource
      }
    }))
  }

  private async loadPiSkillResource(profile: AgentProfileSnapshot, resources: AgentResource[], resource: AgentResource) {
    const enriched = await this.enrichPiSkillResources(profile, resources, [resource.name, resource.resource_key, resource.resource_id])
    const loaded = enriched.find((candidate) => candidate.id === resource.id) || resource
    const spec = asRecord(loaded.spec)
    const content = firstString(spec.instructions, spec.markdown, spec.content, spec.prompt)
    if (!content) {
      throw new RuntimeDomainError('skill_content_unavailable', `Skill ${resource.name} content could not be loaded`)
    }
    return {
      content,
      digest: `sha256:${createHash('sha256').update(content).digest('hex')}`
    }
  }

  private skillContentFromRepositoryScan(repository: AgentResource | undefined, skill: AgentResource) {
    if (!repository) return ''
    const discovered = Array.isArray(repository.spec.discovered_skills) ? repository.spec.discovered_skills : []
    const skillSpec = asRecord(skill.spec)
    const match = discovered.map(asRecord).find((item) =>
      normalizeResourceID(item.key) === normalizeResourceID(skill.resource_key) ||
      normalizeResourceID(item.name) === normalizeResourceID(skill.name) ||
      normalizeResourceID(item.entry) === normalizeResourceID(firstString(skillSpec.entry, skillSpec.path))
    )
    return firstString(match?.content, match?.markdown, match?.instructions, match?.prompt)
  }

  private async enrichPiMcpResources(
    actor: RuntimeActor,
    profile: AgentProfileSnapshot,
    resources: AgentResource[],
    auth: RuntimeAuth | undefined,
    record: (type: string, payload?: Record<string, unknown>) => Promise<unknown> | unknown
  ) {
    const mcpRefs = (profile.mcp_servers || []).filter((ref) => ref.resource_type === 'mcp_server')
    if (mcpRefs.length === 0) return resources

    const resourcesWithBuiltin = [...resources]
    for (const ref of mcpRefs) {
      const hasResource = resourcesWithBuiltin.some((resource) => resource.resource_kind === 'mcp_server' && refMatchesResource(ref, resource))
      if (hasResource || normalizeResourceID(ref.resource_id) !== 'easydo') continue
      const builtin = this.builtinEasyDoCapabilityResource(actor, auth)
      if (builtin) resourcesWithBuiltin.push(builtin)
    }

    const selectedMcpResources = mcpRefs
      .map((ref) => resourcesWithBuiltin.find((resource) => resource.resource_kind === 'mcp_server' && refMatchesResource(ref, resource)))
      .filter((resource): resource is AgentResource => Boolean(resource))
    if (selectedMcpResources.length === 0) return resourcesWithBuiltin

    const discoveredToolsByResource = new Map<string, ChatModelToolDefinition[]>()
    await Promise.all(selectedMcpResources.map(async (resource) => {
      const tools = await this.discoverMcpTools(resource, auth, record)
      discoveredToolsByResource.set(normalizeResourceID(resource.resource_key), tools)
    }))

    return resourcesWithBuiltin.map((resource) => {
      if (resource.resource_kind !== 'mcp_server') return resource
      const tools = discoveredToolsByResource.get(normalizeResourceID(resource.resource_key))
      if (!tools || tools.length === 0) return resource
      return {
        ...resource,
        spec: {
          ...asRecord(resource.spec),
          discovered_tools: tools
        }
      }
    })
  }

  private selectPiSkillInvocation(content: string, skills: PiHarnessResourceBuildResult['skills']) {
    const prompt = content.toLowerCase()
    if (!prompt.trim()) return undefined
    const wantsSkill = /(^|[\s,，。:：])skill($|[\s,，。:：])|技能|使用|调用|激活|use|invoke|activate|apply/i.test(content)
    const listOnly = /列一下|列出|有哪些|有什么|list|show|available/i.test(content) && !/使用|调用|激活|use|invoke|activate|apply/i.test(content)
    if (!wantsSkill || listOnly) return undefined
    for (const skill of skills) {
      const name = skill.name.toLowerCase()
      if (!name) continue
      const normalizedName = name.replace(/[-_.]+/g, ' ')
      const explicitName = prompt.includes(name) || prompt.includes(normalizedName)
      if (explicitName) {
        return { name: skill.name, instructions: content }
      }
    }
    return undefined
  }

  private async recordPiResourcePreparationEvents(
    runtimeSessionID: string,
    runtimeRunID: string,
    profile: AgentProfileSnapshot,
    summary: PiHarnessResourceBuildResult['summary'],
    content: string
  ) {
    const append = async (type: string, payload: Record<string, unknown>) => {
      await this.agentRuntimeEngine?.eventStore.append({
        type,
        session_id: runtimeSessionID,
        runtime_run_id: runtimeRunID,
        event_id: '',
        seq: 0,
        timestamp: now(),
        ...payload
      } as unknown as AgentRuntimeEvent)
    }

    await append('context.build_started', {
      profile_id: profile.id,
      profile_name: profile.name,
      context_tags: profile.context_tags
    })
    await append('capability.snapshot', {
      context_tags: profile.context_tags,
      skills: summary.skills,
      mcp_servers: summary.mcp_servers,
      mcp_tools: summary.mcp_tools,
      subagents: summary.subagents,
      l5_controls: {
        event_stream: true,
        skill_progressive_loading: true,
        mcp_capability_snapshot: true,
        subagent_scheduler: true,
        write_tool_approval: true
      }
    })
    if (summary.mcp_tools.length > 0) {
      await append('mcp.tools.available', {
        tool_count: summary.mcp_tools.length,
        tools: summary.mcp_tools
      })
    }
    for (const skill of summary.skills) {
      await append('skill.available', {
        key: skill.key,
        name: skill.name,
        operation: 'available',
        version: skill.version,
        description: skill.description,
        file_path: skill.file_path
      })
    }
    await append('context.build_completed', {
      context_tag_count: profile.context_tags.length,
      context_fragment_count: 0,
      skill_count: summary.skill_count,
      mcp_tool_count: summary.mcp_tool_count,
      subagent_result_count: 0,
      subagent_count: summary.subagent_count,
      model_content_chars: content.length,
      has_output_schema: hasSchema(asRecord(profile.output_schema))
    })
  }

  private async recordPiSkillLoadedEvents(
    runtimeSessionID: string,
    runtimeRunID: string,
    summary: PiHarnessResourceBuildResult['summary'],
    loadedSkillNames: Iterable<string>
  ) {
    const loadedIDs = new Set(Array.from(loadedSkillNames).map((name) => normalizeResourceID(name)).filter(Boolean))
    if (loadedIDs.size === 0) return
    for (const skill of summary.skills) {
      const isLoaded = loadedIDs.has(normalizeResourceID(skill.name)) ||
        loadedIDs.has(normalizeResourceID(skill.key)) ||
        loadedIDs.has(normalizeResourceID(skill.id))
      if (!isLoaded) continue
      await this.agentRuntimeEngine?.eventStore.append({
        type: 'skill.loaded',
        session_id: runtimeSessionID,
        runtime_run_id: runtimeRunID,
        event_id: '',
        seq: 0,
        timestamp: now(),
        key: skill.key,
        name: skill.name,
        operation: 'loaded',
        version: skill.version,
        description: skill.description,
        file_path: skill.file_path
      } as unknown as AgentRuntimeEvent)
    }
  }

  private async createPiEntryRun(
    actor: RuntimeActor,
    session: AISession,
    persistedProfileSnapshot: AgentProfileSnapshot,
    profileSnapshot: AgentProfileSnapshot,
    resolvedProfile: ResolvedSessionProfile,
    entries: AISessionEntry[],
    publicSessionID: string,
    content: string,
    payload: Record<string, unknown>,
    auth?: RuntimeAuth
  ) {
    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Pi runtime engine is not configured', 500)
    }
    const seqBase = entries.length
    const timestamp = now()
    const runtimeSessionID = firstString(payload.runtime_session_id, publicSessionID)
    const clientEntryID = requireClientID('client_entry_id', payload.client_entry_id)
    const requestID = firstString(payload.runtime_request_id)
    const attachments = entryAttachmentsFromPayload(payload)
    const userEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: seqBase + 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content,
      content_blocks: [{ type: 'text', text: content }],
      input: {
        runtime_engine: 'pi',
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        ...(attachments.length > 0 ? { attachments } : {}),
        ...(requestID ? { request_id: requestID } : {})
      },
      output: {},
      idempotency_key: runtimeIdempotencyKey('entry', publicSessionID, 'client', clientEntryID),
      created_at: timestamp,
      updated_at: timestamp
    }

    const runID = await this.store.nextRunId()
    const runtimeRunID = runBusinessID(actor.workspace_id, session.id, runID)
    const streamingAssistantEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: seqBase + 2,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: { runtime_engine: 'pi', ...(requestID ? { request_id: requestID } : {}) },
      output: {
        text: '',
        runtime_engine: 'pi',
        runtime_session_id: runtimeSessionID,
        runtime_run_id: runtimeRunID,
        runtime_events: [],
        ...(requestID ? { request_id: requestID } : {})
      },
      runtime_run_id: runtimeRunID,
      event_seq: 0,
      idempotency_key: entryRuntimeIdempotencyKey(publicSessionID, runtimeRunID, 'assistant', seqBase + 2),
      created_at: timestamp,
      updated_at: timestamp
    }
    const pendingRun: AIRuntimeRun = {
      id: runID,
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [...profileSnapshot.context_tags],
      agent_profile_id: resolvedProfile.profile.id,
      agent_profile_version_id: resolvedProfile.profileVersionID,
      agent_profile_version_key: resolvedProfile.profileVersionKey,
      agent_profile_snapshot_hash: resolvedProfile.snapshotHash,
      agent_workspace_runtime_id: session.agent_workspace_runtime_id,
      agent_workspace_snapshot_hash: session.agent_workspace_snapshot_hash,
      profile_snapshot: persistedProfileSnapshot,
      status: 'running',
      input_entry_id: userEntry.id,
      output_entry_id: streamingAssistantEntry.id,
      request: {
        runtime_engine: 'pi',
        runtime_session_id: runtimeSessionID,
        content,
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        agent_profile_version_key: resolvedProfile.profileVersionKey,
        ...(requestID ? { request_id: requestID } : {}),
        ...(firstString(payload.parent_runtime_run_id) ? { parent_runtime_run_id: firstString(payload.parent_runtime_run_id) } : {})
      },
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    }
    const queueItemID = firstString(payload.queue_item_id)
    let run: AIRuntimeRun
    let queueStartOutcome: 'consumed' | undefined
    if (queueItemID) {
      const claimedBy = firstString(payload.queue_claimed_by)
      const claimEpoch = Number(payload.queue_claim_epoch || 0)
      const queueMode = firstString(payload.queue_mode) === 'stop_and_run' ? 'stop_and_run' as const : 'follow_up' as const
      const followUpStartedEvent: SessionQueueRuntimeEvent = {
        type: 'session.follow_up.started',
        session_id: runtimeSessionID,
        runtime_run_id: runtimeRunID,
        consumed_runtime_run_id: runtimeRunID,
        event_id: stableEventID(runtimeSessionID, 'session.follow_up.started', queueItemID, runtimeRunID),
        seq: 0,
        timestamp,
        queue_item: {
          queue_item_id: queueItemID,
          workspace_id: actor.workspace_id,
          session_id: session.id,
          item_seq: 0,
          position: 0,
          mode: queueMode,
          content,
          attachments,
          context_ref: asRecord(payload.context_ref),
          status: 'consumed',
          client_item_id: clientEntryID.replace(/^queue:/, ''),
          claimed_by: claimedBy,
          claim_epoch: claimEpoch,
          consumed_runtime_run_id: runtimeRunID,
          created_by: actor.user_id,
          created_at: timestamp,
          updated_at: timestamp
        }
      }
      const queuedStart = await this.store.consumeSessionQueueItemWithRun({
        workspace_id: actor.workspace_id,
        session_id: session.id,
        queue_item_id: queueItemID,
        claimed_by: claimedBy,
        claim_epoch: claimEpoch,
        user_entry: userEntry,
        assistant_entry: streamingAssistantEntry,
        run: pendingRun,
        updated_session_progress: {
          ...session,
          entry_count: seqBase + 2,
          last_entry_at: streamingAssistantEntry.created_at,
          updated_at: timestamp
        },
        follow_up_started_event: followUpStartedEvent
      })
      if (queuedStart.outcome === 'already_consumed') {
        const currentEntries = await this.store.listEntries(actor.workspace_id, session.id)
        const existingUserEntry = currentEntries.find((entry) => entry.id === queuedStart.run.input_entry_id)
        const existingAssistantEntry = queuedStart.run.output_entry_id
          ? currentEntries.find((entry) => entry.id === queuedStart.run.output_entry_id)
          : undefined
        if (!existingUserEntry || !existingAssistantEntry) {
          throw new RuntimeDomainError('session_queue_consume_failed', 'Consumed queue item is missing its persisted Pi entries', 500)
        }
        return {
          run: queuedStart.run,
          user_entry: existingUserEntry,
          assistant_entry: existingAssistantEntry,
          queue_start_outcome: 'already_consumed' as const
        }
      }
      if (queuedStart.outcome !== 'consumed') {
        return {
          run: null,
          user_entry: userEntry,
          assistant_entry: streamingAssistantEntry,
          queue_start_outcome: queuedStart.outcome
        }
      }
      run = queuedStart.run
      queueStartOutcome = 'consumed'
      if (!queuedStart.event_committed) await this.agentRuntimeEngine.eventStore.append(queuedStart.follow_up_started_event)
    } else {
      run = await this.store.saveRun(pendingRun)
      await this.store.saveEntry(userEntry)
      await this.store.saveEntry(streamingAssistantEntry)
      await this.saveSessionProgress(actor, session, {
        entry_count: seqBase + 2,
        last_entry_at: streamingAssistantEntry.created_at
      })
    }
    this.enqueueSessionTitleGeneration(actor, session, profileSnapshot, content, entries.length)

    // Emit run.started immediately so the chatbox can attach runtime_run_id to the
    // streaming draft before permission.asked arrives (approval panel needs it).
    await this.agentRuntimeEngine.eventStore.append({
      type: 'run.started',
      session_id: runtimeSessionID,
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp: now(),
      status: 'running',
      run: { runtime_run_id: runtimeRunID, status: 'running' }
    } as unknown as AgentRuntimeEvent)

    const recordPiPreparationIssue = async (type: string, eventPayload: Record<string, unknown> = {}) => {
      await this.agentRuntimeEngine?.eventStore.append({
        type,
        session_id: runtimeSessionID,
        runtime_run_id: runtimeRunID,
        event_id: '',
        seq: 0,
        timestamp: now(),
        ...eventPayload
      } as unknown as AgentRuntimeEvent)
    }
    const allResources = await this.enrichPiMcpResources(
      actor,
      profileSnapshot,
      await resourcesForProfile(this.store, actor.workspace_id, profileSnapshot),
      auth,
      recordPiPreparationIssue
    )
    const subagentProfiles = await this.resolveSubagentProfiles(actor.workspace_id, profileSnapshot)
    const subagentsForPi = subagentProfiles.map(({ profile: subagent }) => ({
      id: subagent.id,
      name: subagent.name,
      description: subagent.description,
      profile_kind: subagent.profile_kind,
      context_tags: subagent.context_tags
    }))
    const buildResources = (resources: AgentResource[], loadedSkillNames: string[] = []) => buildPiHarnessResources({
      profile: profileSnapshot,
      actorRole: actor.workspace_role,
      resources,
      loadedSkillNames,
      subagents: subagentsForPi,
      loadSkill: ({ resource }) => this.loadPiSkillResource(profileSnapshot, allResources, resource),
      decideTool: async ({ toolName, resource, permissionKey }) => {
        const grant = await this.findMatchingPiSessionGrant({ actor, session, toolName, permissionKey, resource })
        if (!grant) return undefined
        await this.agentRuntimeEngine?.eventStore.append({
          type: 'approval.session_granted',
          session_id: runtimeSessionID,
          runtime_run_id: runtimeRunID,
          event_id: '',
          seq: 0,
          timestamp: now(),
          grant_id: grant.grant_id,
          permission_key: grant.permission_key,
          tool_name: grant.tool_name,
          resource_type: grant.resource_type,
          resource_id: grant.resource_id,
          expires_at: grant.expires_at
        } as unknown as AgentRuntimeEvent)
        return { approved: true, source: 'session_grant', reason: 'Allowed by active Session grant' }
      },
      executeTool: async ({ callID, toolName, args, mcpServerKey }) => this.toolExecutor.execute({
        tool_call_id: callID,
        tool_name: toolName,
        arguments: args,
        mcp_server_key: firstString(mcpServerKey),
        operation_type: firstString(args.operation_type),
        target_type: firstString(args.target_type),
        target_id: firstString(args.target_id),
        risk_summary: firstString(args.risk_summary),
        requires_confirmation: args.requires_confirmation === true
      }, { actor, session, runtime_run_id: runtimeRunID, auth }),
      executeSubagent: async ({ callID, subagent, task, args }) => {
        const match = subagentProfiles.find((item) => item.profile.id === subagent.id)
        if (!match) throw new Error(`Subagent profile ${subagent.id} is not available to this parent profile`)
        const mode = resolveSubagentMode(asRecord(match.ref.config).mode, args.mode)
        const objective = firstString(task, args.objective, match.profile.description, content)
        const taskID = firstString(args.task_id, args.id, callID)
        const action = await this.createSubagentSpawnAction({
          actor,
          session,
          parentProfile: profileSnapshot,
          runtimeRunID,
          taskID,
          objective,
          mode,
          childProfile: match.profile,
          source: 'model'
        })
        const result = await this.executeSubagentChildRun({
          actor,
          parentSession: session,
          parentProfile: profileSnapshot,
          childProfile: match.profile,
          childProfileVersionID: match.profileVersionID,
          parentRuntimeRunID: runtimeRunID,
          action,
          taskID,
          objective,
          mode,
          contextRef: asRecord(payload.context_ref),
          includeCurrentPage: Boolean(payload.include_current_page),
          recordParentEvent: async (type, eventPayload = {}) => {
            await this.agentRuntimeEngine?.eventStore.append({
              type,
              session_id: runtimeSessionID,
              runtime_run_id: runtimeRunID,
              event_id: '',
              seq: 0,
              timestamp: now(),
              payload: eventPayload
            } as unknown as AgentRuntimeEvent)
          }
        })
        if (result.status === 'blocked_approval' || result.status === 'cancelled') return result
        await this.agentRuntimeEngine?.eventStore.append({
          type: 'subagent.progress',
          session_id: runtimeSessionID,
          runtime_run_id: runtimeRunID,
          event_id: '',
          seq: 0,
          timestamp: now(),
          payload: {
            action_id: action.action_id,
            action_internal_id: action.id,
            task_id: taskID,
            agent_profile_id: match.profile.id,
            name: match.profile.name,
            status: result.status === 'completed' ? 'running' : 'failed',
            summary: `子 Agent ${match.profile.name} 已返回结果摘要`,
            child_run_link_id: result.child_run_link_id,
            child_session_id: result.child_session_id,
            child_runtime_run_id: result.child_runtime_run_id
          }
        } as unknown as AgentRuntimeEvent)
        await this.agentRuntimeEngine?.eventStore.append({
          type: result.status === 'completed' ? 'subagent.completed' : 'subagent.failed',
          session_id: runtimeSessionID,
          runtime_run_id: runtimeRunID,
          event_id: '',
          seq: 0,
          timestamp: now(),
          payload: result
        } as unknown as AgentRuntimeEvent)
        return result
      },
      recordEvent: async (event) => {
        await this.agentRuntimeEngine?.eventStore.append({
          ...event,
          session_id: runtimeSessionID,
          runtime_run_id: runtimeRunID,
          event_id: '',
          seq: 0,
          timestamp: now()
        } as AgentRuntimeEvent)
        if (event.type === 'permission.asked') {
          await this.agentRuntimeEngine?.runner.pauseForApproval?.(runtimeRunID, {
            approvalID: firstString(event.approval_id, event.request_id),
            callID: firstString(event.call_id),
            toolName: firstString(event.tool_name),
            reason: firstString(event.reason, event.message, 'Tool execution requires approval')
          })
        }
      }
    })
    let piResources = buildResources(allResources)
    const skillInvocation = this.selectPiSkillInvocation(content, piResources.skills)
    const loadedSkillNames = skillInvocation?.name ? [skillInvocation.name] : []
    if (loadedSkillNames.length > 0) {
      piResources = buildResources(
        await this.enrichPiSkillResources(profileSnapshot, allResources, loadedSkillNames),
        loadedSkillNames
      )
    }
    const modelConfig = modelSelectionFromRuntimeConfig({
      provider: profileSnapshot.provider,
      model: profileSnapshot.model,
      binding: profileSnapshot.binding,
      credential: profileSnapshot.provider_credential_ref,
      inference: profileSnapshot.inference
    })
    const publicModelConfig = publicPiModelSelection(modelConfig)
    await this.recordPiResourcePreparationEvents(runtimeSessionID, runtimeRunID, profileSnapshot, piResources.summary, content)
    await this.recordPiSkillLoadedEvents(runtimeSessionID, runtimeRunID, piResources.summary, loadedSkillNames)
    await this.agentRuntimeEngine?.eventStore.append({
      type: 'model.selected',
      session_id: runtimeSessionID,
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp: now(),
      model: publicModelConfig
    } as unknown as AgentRuntimeEvent)
    run = await this.store.saveRun({
      ...run,
      request: {
        ...run.request,
        pi_resources: piResources.summary,
        model_config: publicModelConfig
      },
      updated_at: now()
    })

    const cancelledBeforePrompt = await this.cancelledPiEntryRunResult(
      actor,
      session,
      runtimeRunID,
      userEntry,
      streamingAssistantEntry
    )
    if (cancelledBeforePrompt) return cancelledBeforePrompt

    const model = modelConfig ? { provider_id: modelConfig.provider_id, id: modelConfig.id } : undefined
    const promptConfig = asRecord(profileSnapshot.prompt)
    const systemPrompt = firstString(promptConfig.system, promptConfig.system_prompt, promptConfig.systemPrompt)
    let harnessResult: AgentHarnessRunResult
    try {
      harnessResult = await this.runPiHarnessWithLease(run, {
        sessionID: runtimeSessionID,
        runtimeRunID,
        prompt: content,
        history: piSessionHistoryFromEntries(entries),
        agent: profileSnapshot.name,
        systemPrompt,
        model,
        modelConfig,
        tools: piResources.tools,
        skills: piResources.skills,
        skillInvocation
      })
    } catch (error) {
      if (!(error instanceof PiToolApprovalRequiredError)) {
        const cancelled = await this.cancelledPiEntryRunResult(actor, session, runtimeRunID, userEntry, streamingAssistantEntry)
        if (cancelled) return cancelled
        return this.finalizeFailedPiEntryRun({
          actor,
          session,
          run,
          userEntry,
          assistantEntry: streamingAssistantEntry,
          seqBase,
          timestamp,
          runtimeSessionID,
          error,
          piResourcesSummary: piResources.summary
        })
      }
      const rawRuntimeEvents = await this.agentRuntimeEngine.eventStore.replayRun(runtimeRunID)
      const permissionEvent = [...rawRuntimeEvents]
        .reverse()
        .map(asRecord)
        .find((event) => event.type === 'permission.asked' && firstString(event.request_id, event.approval_id) === error.approvalID)
      const awaitingApproval = {
        approval_id: error.approvalID,
        call_id: error.callID,
        tool_name: error.toolName,
        reason: error.reason,
        input: asRecord(permissionEvent?.input)
      }
      const awaitingApprovals = normalizePiAwaitingApprovals(pendingPiApprovalsFromRuntimeEvents(rawRuntimeEvents), awaitingApproval)
      const runtimeEvents = runtimeEventsUntilPendingPiApprovals(rawRuntimeEvents, awaitingApprovals)
      const assistantContent = textFromRuntimeEvents(runtimeEvents)
      return this.finalizePiAwaitingApprovalRun({
        actor,
        session,
        run,
        userEntry,
        assistantEntry: streamingAssistantEntry,
        seqBase,
        runtimeSessionID,
        runtimeEvents,
        assistantContent,
        awaitingApproval,
        awaitingApprovals,
        piResourcesSummary: piResources.summary
      })
    }
    const cancelledAfterPrompt = await this.cancelledPiEntryRunResult(
      actor,
      session,
      runtimeRunID,
      userEntry,
      streamingAssistantEntry
    )
    if (cancelledAfterPrompt) return cancelledAfterPrompt
    const rawRuntimeEvents = await this.agentRuntimeEngine.eventStore.replayRun(runtimeRunID)
    const pendingApprovals = pendingPiApprovalsFromRuntimeEvents(rawRuntimeEvents)
    const pendingApproval = pendingApprovals[0] || null
    if (pendingApproval) {
      const runtimeEvents = runtimeEventsUntilPendingPiApprovals(rawRuntimeEvents, pendingApprovals)
      const assistantContent = textFromRuntimeEvents(runtimeEvents)
      return this.finalizePiAwaitingApprovalRun({
        actor,
        session,
        run,
        userEntry,
        assistantEntry: streamingAssistantEntry,
        seqBase,
        runtimeSessionID,
        runtimeEvents,
        assistantContent,
        awaitingApproval: pendingApproval,
        awaitingApprovals: pendingApprovals,
        piResourcesSummary: piResources.summary
      })
    }
    const completedAt = now()
    await this.ensurePiRunStateEvent({
      ...run,
      status: 'completed',
      finished_at: completedAt,
      updated_at: completedAt
    })
    const runtimeEvents = await this.agentRuntimeEngine.eventStore.replayRun(runtimeRunID)
    const embeddedRuntimeEvents = summarizeRuntimeEventsForEntry(runtimeEvents)
    const assistantContent = textFromRuntimeEvents(runtimeEvents)
    const startedMs = Date.parse(String(run.started_at || timestamp || completedAt))
    const finishedMs = Date.parse(completedAt)
    const wallMs = Number.isFinite(startedMs) && Number.isFinite(finishedMs) ? Math.max(0, finishedMs - startedMs) : 0
    const piTimings = attachActiveDurationTimings(
      { total_ms: wallMs },
      runtimeEvents
    )
    const assistantEntry: AISessionEntry = {
      ...streamingAssistantEntry,
      entry_type: 'message',
      status: 'completed',
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: { runtime_engine: 'pi', harness_result: harnessResult },
      output: {
        text: assistantContent,
        runtime_engine: 'pi',
        runtime_session_id: runtimeSessionID,
        runtime_events: embeddedRuntimeEvents,
        pi_resources: piResources.summary,
        timings: piTimings
      },
      event_seq: runtimeEvents.length,
      updated_at: now()
    }
    await this.store.saveEntry(assistantEntry)

    run = await this.store.saveRun({
      ...run,
      status: 'completed',
      output_entry_id: assistantEntry.id,
      result: {
        text: assistantContent,
        runtime_engine: 'pi',
        runtime_session_id: runtimeSessionID,
        runtime_events: embeddedRuntimeEvents,
        harness_result: harnessResult,
        pi_resources: piResources.summary,
        timings: piTimings
      },
      usage: {},
      finished_at: completedAt,
      updated_at: completedAt
    })
    await this.saveSessionProgress(actor, session, {
      entry_count: seqBase + 2,
      last_entry_at: assistantEntry.created_at
    })
    return { run, user_entry: userEntry, assistant_entry: assistantEntry, queue_start_outcome: queueStartOutcome }
  }

  private async cancelledPiEntryRunResult(
    actor: RuntimeActor,
    session: AISession,
    runtimeRunID: string,
    userEntry: AISessionEntry,
    streamingAssistantEntry: AISessionEntry
  ) {
    const cancelledRun = await this.store.getRun(actor.workspace_id, runtimeRunID)
    if (!cancelledRun || !['cancelled', 'interrupted'].includes(cancelledRun.status)) return undefined
    const currentEntries = await this.store.listEntries(actor.workspace_id, session.id)
    const cancelledAssistantEntry = currentEntries.find((entry) => entry.id === streamingAssistantEntry.id)
    return {
      run: cancelledRun,
      user_entry: userEntry,
      assistant_entry: cancelledAssistantEntry || {
        ...streamingAssistantEntry,
        status: cancelledRun.status === 'cancelled' ? 'cancelled' as const : 'failed' as const,
        content: streamingAssistantEntry.content || (cancelledRun.status === 'cancelled' ? '已停止生成' : '运行时实例中断，本次生成已停止。'),
        content_blocks: streamingAssistantEntry.content_blocks.length > 0
          ? streamingAssistantEntry.content_blocks
          : [{ type: 'text', text: cancelledRun.status === 'cancelled' ? '已停止生成' : '运行时实例中断，本次生成已停止。' }],
        updated_at: now()
      }
    }
  }

  private async finalizePiAwaitingApprovalRun(params: {
    actor: RuntimeActor
    session: AISession
    run: AIRuntimeRun
    userEntry: AISessionEntry
    assistantEntry: AISessionEntry
    seqBase: number
    runtimeSessionID: string
    runtimeEvents: AgentRuntimeEvent[]
    assistantContent: string
    awaitingApproval: Record<string, unknown>
    awaitingApprovals?: Record<string, unknown>[]
    piResourcesSummary: Record<string, unknown>
  }) {
    const awaitingApprovals = normalizePiAwaitingApprovals(params.awaitingApprovals, params.awaitingApproval)
    const awaitingApproval = awaitingApprovals[0] || params.awaitingApproval
    const stateTimestamp = now()
    const stateEvents = await this.ensurePiRunStateEvent({
      ...params.run,
      status: 'awaiting_decision',
      updated_at: stateTimestamp
    })
    const includedEventIDs = new Set(params.runtimeEvents.map((event) => event.event_id))
    const runtimeEvents = [
      ...params.runtimeEvents,
      ...stateEvents.filter((event) => event.type === 'run.awaiting_decision' && !includedEventIDs.has(event.event_id))
    ].sort((left, right) => Number(left.seq || 0) - Number(right.seq || 0))
    const embeddedRuntimeEvents = summarizeRuntimeEventsForEntry(runtimeEvents)
    const assistantEntry: AISessionEntry = {
      ...params.assistantEntry,
      entry_type: 'message',
      status: 'streaming',
      content: params.assistantContent,
      content_blocks: [{ type: 'text', text: params.assistantContent }],
      input: { runtime_engine: 'pi', awaiting_approval: awaitingApproval, awaiting_approvals: awaitingApprovals },
      output: {
        text: params.assistantContent,
        runtime_engine: 'pi',
        runtime_session_id: params.runtimeSessionID,
        runtime_events: embeddedRuntimeEvents,
        awaiting_approval: awaitingApproval,
        awaiting_approvals: awaitingApprovals,
        pi_resources: params.piResourcesSummary
      },
      event_seq: runtimeEvents.length,
      updated_at: stateTimestamp
    }
    await this.store.saveEntry(assistantEntry)
    const run = await this.store.saveRun({
      ...params.run,
      status: 'awaiting_decision',
      output_entry_id: assistantEntry.id,
      result: {
        text: params.assistantContent,
        runtime_engine: 'pi',
        runtime_session_id: params.runtimeSessionID,
        runtime_events: embeddedRuntimeEvents,
        awaiting_approval: awaitingApproval,
        awaiting_approvals: awaitingApprovals,
        pi_resources: params.piResourcesSummary
      },
      usage: {},
      updated_at: stateTimestamp
    })
    await this.saveSessionProgress(params.actor, params.session, {
      entry_count: params.seqBase + 2,
      last_entry_at: assistantEntry.created_at
    })
    return { run, user_entry: params.userEntry, assistant_entry: assistantEntry }
  }

  private async finalizeFailedPiEntryRun(params: {
    actor: RuntimeActor
    session: AISession
    run: AIRuntimeRun
    userEntry: AISessionEntry
    assistantEntry: AISessionEntry
    seqBase: number
    timestamp: string
    runtimeSessionID: string
    error: unknown
    piResourcesSummary?: Record<string, unknown>
  }) {
    const errorDescriptor = classifyRuntimeError(params.error)
    const requestID = firstString(params.run.request.request_id)
    const errorPayload = {
      ...runtimeErrorPublicPayload(errorDescriptor),
      ...(requestID ? { request_id: requestID } : {})
    }
    const runStatus: AIRuntimeRun['status'] = errorDescriptor.terminal_status
    const cancelled = runStatus === 'cancelled'
    await this.recordPiFailureEvents(params.runtimeSessionID, params.run.runtime_run_id, params.timestamp, errorDescriptor, requestID)
    const finishedAt = now()
    const runtimeEvents = await this.ensurePiRunStateEvent({
      ...params.run,
      status: runStatus,
      error_code: errorDescriptor.code,
      error_msg: errorDescriptor.user_message,
      finished_at: finishedAt,
      updated_at: finishedAt
    })
    const embeddedRuntimeEvents = summarizeRuntimeEventsForEntry(runtimeEvents)
    const assistantContent = cancelled
      ? `已停止生成：${errorDescriptor.user_message}`
      : `Pi 运行失败：${errorDescriptor.user_message}`
    const failedStartedMs = Date.parse(String(params.run.started_at || params.timestamp || finishedAt))
    const failedFinishedMs = Date.parse(finishedAt)
    const failedWallMs = Number.isFinite(failedStartedMs) && Number.isFinite(failedFinishedMs)
      ? Math.max(0, failedFinishedMs - failedStartedMs)
      : 0
    const failedTimings = attachActiveDurationTimings(
      { total_ms: failedWallMs },
      runtimeEvents
    )
    const assistantEntry: AISessionEntry = {
      ...params.assistantEntry,
      entry_type: cancelled ? 'message' : 'error',
      status: cancelled ? 'cancelled' : 'failed',
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: { runtime_engine: 'pi', error: errorPayload },
      output: {
        text: assistantContent,
        runtime_engine: 'pi',
        runtime_session_id: params.runtimeSessionID,
        runtime_events: embeddedRuntimeEvents,
        pi_resources: params.piResourcesSummary || {},
        error: errorPayload,
        timings: failedTimings
      },
      event_seq: runtimeEvents.length,
      updated_at: now()
    }
    await this.store.saveEntry(assistantEntry)
    const failedRun = await this.store.saveRun({
      ...params.run,
      status: runStatus,
      output_entry_id: assistantEntry.id,
      result: {
        text: assistantContent,
        runtime_engine: 'pi',
        runtime_session_id: params.runtimeSessionID,
        runtime_events: embeddedRuntimeEvents,
        pi_resources: params.piResourcesSummary || {},
        error: errorPayload,
        timings: failedTimings
      },
      usage: {},
      error_code: errorDescriptor.code,
      error_msg: errorDescriptor.user_message,
      finished_at: finishedAt,
      updated_at: finishedAt
    })
    await this.saveSessionProgress(params.actor, params.session, {
      entry_count: params.seqBase + 2,
      last_entry_at: assistantEntry.created_at
    })
    return { run: failedRun, user_entry: params.userEntry, assistant_entry: assistantEntry }
  }

  private async buildPiContinuationResources(params: {
    actor: RuntimeActor
    session: AISession
    profileSnapshot: AgentProfileSnapshot
    run: AIRuntimeRun
    runtimeSessionID: string
    auth?: RuntimeAuth
  }) {
    const content = firstString(params.run.request.content, params.run.result.text, params.session.title)
    const recordPiPreparationIssue = async (type: string, eventPayload: Record<string, unknown> = {}) => {
      await this.agentRuntimeEngine?.eventStore.append({
        type,
        session_id: params.runtimeSessionID,
        runtime_run_id: params.run.runtime_run_id,
        event_id: '',
        seq: 0,
        timestamp: now(),
        ...eventPayload
      } as unknown as AgentRuntimeEvent)
    }
    const allResources = await this.enrichPiMcpResources(
      params.actor,
      params.profileSnapshot,
      await resourcesForProfile(this.store, params.actor.workspace_id, params.profileSnapshot),
      params.auth,
      recordPiPreparationIssue
    )
    const subagentProfiles = await this.resolveSubagentProfiles(params.actor.workspace_id, params.profileSnapshot)
    const subagentsForPi = subagentProfiles.map(({ profile: subagent }) => ({
      id: subagent.id,
      name: subagent.name,
      description: subagent.description,
      profile_kind: subagent.profile_kind,
      context_tags: subagent.context_tags
    }))
    const buildResources = (resources: AgentResource[], loadedSkillNames: string[] = []) => buildPiHarnessResources({
      runMode: subagentModeForRun(params.run),
      profile: params.profileSnapshot,
      actorRole: params.actor.workspace_role,
      resources,
      loadedSkillNames,
      subagents: subagentsForPi,
      loadSkill: ({ resource }) => this.loadPiSkillResource(params.profileSnapshot, allResources, resource),
      decideTool: async ({ toolName, resource, permissionKey }) => {
        const grant = await this.findMatchingPiSessionGrant({
          actor: params.actor,
          session: params.session,
          toolName,
          permissionKey,
          resource
        })
        if (!grant) return undefined
        await this.agentRuntimeEngine?.eventStore.append({
          type: 'approval.session_granted',
          session_id: params.runtimeSessionID,
          runtime_run_id: params.run.runtime_run_id,
          event_id: '',
          seq: 0,
          timestamp: now(),
          grant_id: grant.grant_id,
          permission_key: grant.permission_key,
          tool_name: grant.tool_name,
          resource_type: grant.resource_type,
          resource_id: grant.resource_id,
          expires_at: grant.expires_at
        } as unknown as AgentRuntimeEvent)
        return { approved: true, source: 'session_grant', reason: 'Allowed by active Session grant' }
      },
      executeTool: async ({ callID, toolName, args, mcpServerKey }) => this.toolExecutor.execute({
        tool_call_id: callID,
        tool_name: toolName,
        arguments: args,
        mcp_server_key: firstString(mcpServerKey),
        operation_type: firstString(args.operation_type),
        target_type: firstString(args.target_type),
        target_id: firstString(args.target_id),
        risk_summary: firstString(args.risk_summary),
        requires_confirmation: args.requires_confirmation === true
      }, { actor: params.actor, session: params.session, runtime_run_id: params.run.runtime_run_id, auth: params.auth }),
      executeSubagent: async ({ callID, subagent, task, args }) => {
        const match = subagentProfiles.find((item) => item.profile.id === subagent.id)
        if (!match) throw new Error(`Subagent profile ${subagent.id} is not available to this parent profile`)
        const mode = resolveSubagentMode(asRecord(match.ref.config).mode, args.mode)
        const objective = firstString(task, args.objective, match.profile.description, content)
        const taskID = firstString(args.task_id, args.id, callID)
        const action = await this.createSubagentSpawnAction({
          actor: params.actor,
          session: params.session,
          parentProfile: params.profileSnapshot,
          runtimeRunID: params.run.runtime_run_id,
          taskID,
          objective,
          mode,
          childProfile: match.profile,
          source: 'model'
        })
        const result = await this.executeSubagentChildRun({
          actor: params.actor,
          parentSession: params.session,
          parentProfile: params.profileSnapshot,
          childProfile: match.profile,
          childProfileVersionID: match.profileVersionID,
          parentRuntimeRunID: params.run.runtime_run_id,
          action,
          taskID,
          objective,
          mode,
          contextRef: asRecord(params.run.request.context_ref),
          includeCurrentPage: Boolean(params.run.request.include_current_page),
          recordParentEvent: async (type, eventPayload = {}) => {
            await this.agentRuntimeEngine?.eventStore.append({
              type,
              session_id: params.runtimeSessionID,
              runtime_run_id: params.run.runtime_run_id,
              event_id: '',
              seq: 0,
              timestamp: now(),
              payload: eventPayload
            } as unknown as AgentRuntimeEvent)
          }
        })
        if (result.status === 'blocked_approval' || result.status === 'cancelled') return result
        await this.agentRuntimeEngine?.eventStore.append({
          type: 'subagent.progress',
          session_id: params.runtimeSessionID,
          runtime_run_id: params.run.runtime_run_id,
          event_id: '',
          seq: 0,
          timestamp: now(),
          payload: {
            action_id: action.action_id,
            action_internal_id: action.id,
            task_id: taskID,
            agent_profile_id: match.profile.id,
            name: match.profile.name,
            status: result.status === 'completed' ? 'running' : 'failed',
            summary: `子 Agent ${match.profile.name} 已返回结果摘要`,
            child_run_link_id: result.child_run_link_id,
            child_session_id: result.child_session_id,
            child_runtime_run_id: result.child_runtime_run_id
          }
        } as unknown as AgentRuntimeEvent)
        await this.agentRuntimeEngine?.eventStore.append({
          type: result.status === 'completed' ? 'subagent.completed' : 'subagent.failed',
          session_id: params.runtimeSessionID,
          runtime_run_id: params.run.runtime_run_id,
          event_id: '',
          seq: 0,
          timestamp: now(),
          payload: result
        } as unknown as AgentRuntimeEvent)
        return result
      },
      recordEvent: async (event) => {
        await this.agentRuntimeEngine?.eventStore.append({
          ...event,
          session_id: params.runtimeSessionID,
          runtime_run_id: params.run.runtime_run_id,
          event_id: '',
          seq: 0,
          timestamp: now()
        } as AgentRuntimeEvent)
        if (event.type === 'permission.asked') {
          await this.agentRuntimeEngine?.runner.pauseForApproval?.(params.run.runtime_run_id, {
            approvalID: firstString(event.approval_id, event.request_id),
            callID: firstString(event.call_id),
            toolName: firstString(event.tool_name),
            reason: firstString(event.reason, event.message, 'Tool execution requires approval')
          })
        }
      }
    })
    let piResources = buildResources(allResources)
    const skillInvocation = this.selectPiSkillInvocation(content, piResources.skills)
    const loadedSkillNames = skillInvocation?.name ? [skillInvocation.name] : []
    if (loadedSkillNames.length > 0) {
      piResources = buildResources(
        await this.enrichPiSkillResources(params.profileSnapshot, allResources, loadedSkillNames),
        loadedSkillNames
      )
    }
    return { piResources, skillInvocation }
  }

  private async syncParentAfterChildTerminal(actor: RuntimeActor, childRun: AIRuntimeRun, summary: string) {
    const link = await this.store.getParentRunLink(actor.workspace_id, childRun.runtime_run_id)
    if (!link) return
    const parentRun = await this.store.getRun(actor.workspace_id, link.parent_runtime_run_id)
    const parentSession = parentRun ? await this.store.getSession(actor.workspace_id, parentRun.session_id) : undefined
    const action = await this.store.getActionByBusinessID(actor.workspace_id, link.parent_action_id)
    if (!parentRun || !parentSession || !action) return
    const childCompleted = childRun.status === 'completed'
    const childLinkStatus: ChildRunLink['status'] = childCompleted ? 'completed' : childRun.status === 'cancelled' ? 'cancelled' : 'failed'
    await this.store.saveChildRunLink({ ...link, status: childLinkStatus, completed_at: now() })
    const updatedAction = await this.store.saveAction({
      ...action,
      status: childCompleted ? 'executed' : 'failed',
      executed_at: now(),
      result_json: {
        ...action.result_json,
        status: childLinkStatus,
        summary,
        child_run_link_id: link.child_run_link_id,
        child_session_id: link.child_session_id,
        child_runtime_run_id: childRun.runtime_run_id
      },
      error_msg: childCompleted ? undefined : firstString(childRun.error_msg, summary),
      updated_at: now()
    })
    const executions = await this.store.listActionExecutions(actor.workspace_id, action.id)
    const execution = executions.at(-1)
    if (execution) {
      await this.store.saveActionExecution({
        ...execution,
        status: childCompleted ? 'succeeded' : 'failed',
        result_json: updatedAction.result_json,
        error_code: childCompleted ? undefined : firstString(childRun.error_code, 'subagent_failed'),
        error_msg: childCompleted ? undefined : firstString(childRun.error_msg, summary),
        finished_at: now(),
        updated_at: now()
      })
    }
    const eventType = childCompleted ? 'subagent.completed' : childRun.status === 'cancelled' ? 'subagent.cancelled' : 'subagent.failed'
    const eventPayload = {
      action_id: action.action_id,
      action_internal_id: action.id,
      task_id: firstString(action.input_json.task_id),
      status: childLinkStatus,
      agent_profile_id: link.child_profile_id,
      summary,
      child_run_link_id: link.child_run_link_id,
      child_session_id: link.child_session_id,
      child_runtime_run_id: childRun.runtime_run_id,
      artifact_refs: Array.isArray(childRun.result.artifact_refs) ? childRun.result.artifact_refs : []
    }
    let runtimeEvents: unknown[]
    if (firstString(parentRun.result.runtime_engine, parentRun.request.runtime_engine) === 'pi' && this.agentRuntimeEngine) {
      const parentRuntimeSessionID = firstString(parentRun.result.runtime_session_id, parentRun.request.runtime_session_id)
      const existingEvents = await this.agentRuntimeEngine.eventStore.replayRun(parentRun.runtime_run_id)
      const alreadyRecorded = existingEvents.some((event) =>
        String(event.type) === eventType && firstString(asRecord(asRecord(event).payload).child_runtime_run_id) === childRun.runtime_run_id
      )
      if (!alreadyRecorded) {
        await this.agentRuntimeEngine.eventStore.append({
          type: eventType,
          session_id: parentRuntimeSessionID,
          runtime_run_id: parentRun.runtime_run_id,
          event_id: '',
          seq: 0,
          timestamp: now(),
          payload: eventPayload
        } as unknown as AgentRuntimeEvent)
      }
      runtimeEvents = summarizeRuntimeEventsForEntry(
        await this.agentRuntimeEngine.eventStore.replayRun(parentRun.runtime_run_id)
      )
    } else {
      const [parentEvent] = await this.persistRuntimeEvents(actor, parentSession, parentRun.runtime_run_id, [{
        type: eventType,
        payload: eventPayload
      }])
      runtimeEvents = [
        ...(Array.isArray(parentRun.result.runtime_events) ? parentRun.result.runtime_events : []),
        parentEvent
      ]
    }
    await this.store.saveRun({
      ...parentRun,
      result: { ...parentRun.result, runtime_events: runtimeEvents },
      updated_at: now()
    })
    if (parentRun.output_entry_id) {
      const entries = await this.store.listEntries(actor.workspace_id, parentSession.id)
      const parentEntry = entries.find((entry) => entry.id === parentRun.output_entry_id)
      if (parentEntry) {
        await this.store.saveEntry({
          ...parentEntry,
          output: { ...parentEntry.output, runtime_events: runtimeEvents },
          updated_at: now()
        })
      }
    }
  }

  async decidePiApproval(actor: RuntimeActor, runtimeRunID: string, payload: Record<string, unknown> = {}, auth?: RuntimeAuth) {
    return this.withPiApprovalLock(actor, runtimeRunID, () => this.decidePiApprovalLocked(actor, runtimeRunID, payload, auth))
  }

  private async decidePiApprovalLocked(actor: RuntimeActor, runtimeRunID: string, payload: Record<string, unknown> = {}, auth?: RuntimeAuth) {
    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Pi runtime engine is not configured', 500)
    }
    const engine = this.agentRuntimeEngine
    const decision = parsePiApprovalDecision(payload)
    const run = await this.store.getRun(actor.workspace_id, runtimeRunID)
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    await this.assertContinuationRunProfileIntegrity(actor, run)
    const session = await this.store.getSession(actor.workspace_id, run.session_id)
    if (!session || session.status !== 'active') {
      throw new RuntimeDomainError('ai_session_not_found', 'AI session not found', 404)
    }
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const assistantEntry = run.output_entry_id
      ? entries.find((entry) => entry.id === run.output_entry_id)
      : entries.find((entry) => entry.runtime_run_id === run.runtime_run_id && entry.role === 'assistant')
    if (!assistantEntry) {
      throw new RuntimeDomainError('pi_approval_entry_missing', 'Pi approval assistant entry is missing', 409)
    }
    const runtimeSessionID = firstString(run.result.runtime_session_id, run.request.runtime_session_id, sessionBusinessID(actor.workspace_id, session.id))
    const replayCurrentRunEvents = () => engine.eventStore.replayRun(runtimeRunID)
    const storedAwaitingApproval = asRecord(run.result.awaiting_approval)
    const storedAwaitingApprovals = normalizePiAwaitingApprovals(run.result.awaiting_approvals, storedAwaitingApproval)
    const currentRunEventsBeforeDecision = await replayCurrentRunEvents()
    const currentPendingApprovals = pendingPiApprovalsFromRuntimeEvents(currentRunEventsBeforeDecision)
    if ((run.status !== 'awaiting_decision' || hasPiApprovalRequestEvents(currentRunEventsBeforeDecision)) && currentPendingApprovals.length === 0) {
      throw new RuntimeDomainError('pi_approval_not_awaiting_decision', 'Pi runtime run is not awaiting approval', 409)
    }
    const requestedApprovalID = firstString(payload.approval_id, payload.request_id, payload.action_id)
    const awaitingApprovals = currentPendingApprovals.length > 0 ? currentPendingApprovals : storedAwaitingApprovals
    const requestedApproval = requestedApprovalID
      ? awaitingApprovals.find((approval) => firstString(approval.approval_id, approval.request_id) === requestedApprovalID)
      : undefined
    if (requestedApprovalID && !requestedApproval) {
      throw new RuntimeDomainError('pi_approval_not_awaiting_decision', 'Pi runtime run is not awaiting approval', 409)
    }
    const awaitingApproval = requestedApproval || awaitingApprovals.find((approval) =>
      firstString(approval.approval_id, approval.request_id) === firstString(storedAwaitingApproval.approval_id, storedAwaitingApproval.request_id)
    ) || awaitingApprovals[0] || {}
    const approvalID = firstString(awaitingApproval.approval_id, awaitingApproval.request_id)
    const callID = firstString(awaitingApproval.call_id)
    const toolName = firstString(awaitingApproval.tool_name)
    if (!approvalID || !callID || !toolName) {
      throw new RuntimeDomainError('pi_approval_missing', 'Pi runtime run has no pending approval details', 409)
    }
    const eventBase = { session_id: runtimeSessionID, runtime_run_id: runtimeRunID, event_id: '', seq: 0, timestamp: now() }
    if (isApprovingDecision(decision)) {
      try {
        assertReadOnlyAllowsOperation(
          subagentModeForRun(run),
          firstString(awaitingApproval.operation_type, asRecord(awaitingApproval.input).operation_type)
        )
      } catch (error) {
        if (!(error instanceof RuntimeDomainError) || error.code !== 'subagent_read_only_violation') throw error
        await engine.eventStore.append({
          ...eventBase,
          type: 'action.permission_evaluated',
          call_id: callID,
          tool_name: toolName,
          decision: 'deny',
          matched_rule: 'subagent.read_only',
          scope: 'run',
          permission_key: `tool:${toolName}:${firstString(awaitingApproval.operation_type, asRecord(awaitingApproval.input).operation_type, 'unknown')}`,
          operation_type: firstString(awaitingApproval.operation_type, asRecord(awaitingApproval.input).operation_type, 'unknown'),
          code: error.code,
          reason: error.message
        } as AgentRuntimeEvent)
        throw error
      }
    }
    const resolvedResult = isApprovingDecision(decision) ? 'approved' : decision === 'steer' ? 'steer' : 'rejected'
    await engine.eventStore.append({
      ...eventBase,
      type: 'permission.resolved',
      request_id: approvalID,
      approval_id: approvalID,
      call_id: callID,
      tool_name: toolName,
      result: resolvedResult,
      decision
    } as AgentRuntimeEvent)
    this.logger.info({
      ...this.runLogContext(run, 'approval-decision'),
      outcome: resolvedResult,
      code: decision,
      category: 'permission'
    })
    if (decision === 'approve_session') {
      const grant = await this.createPiSessionPermissionGrant({ actor, session, run, approval: awaitingApproval })
      await engine.eventStore.append({
        ...eventBase,
        type: 'approval.session_granted',
        grant_id: grant.grant_id,
        permission_key: grant.permission_key,
        tool_name: grant.tool_name,
        executor_type: grant.executor_type,
        resource_type: grant.resource_type,
        resource_id: grant.resource_id,
        granted_by: grant.granted_by,
        created_at: grant.created_at,
        expires_at: grant.expires_at
      } as unknown as AgentRuntimeEvent)
    }

    const timestamp = now()
    const runtimeEventsAfterDecision = await replayCurrentRunEvents()
    const pendingApprovalsAfterDecision = pendingPiApprovalsFromRuntimeEvents(runtimeEventsAfterDecision)
    if (pendingApprovalsAfterDecision.length > 0) {
      const allStateEvents = await this.ensurePiRunStateEvent({ ...run, status: 'awaiting_decision', updated_at: timestamp })
      const boundaryEvents = runtimeEventsUntilPendingPiApprovals(runtimeEventsAfterDecision, pendingApprovalsAfterDecision)
      const boundaryEventIDs = new Set(boundaryEvents.map((event) => event.event_id))
      const stateEvents = await replayCurrentRunEvents()
      const projectedStateEvents = [
        ...boundaryEvents,
        ...allStateEvents.filter((event) => event.type === 'run.awaiting_decision' && !boundaryEventIDs.has(event.event_id))
      ].sort((left, right) => Number(left.seq || 0) - Number(right.seq || 0))
      const embeddedRuntimeEvents = summarizeRuntimeEventsForEntry(projectedStateEvents)
      const assistantContent = textFromRuntimeEvents(projectedStateEvents)
      const nextAwaitingApproval = pendingApprovalsAfterDecision[0]
      const updatedEntry = await this.store.saveEntry({
        ...assistantEntry,
        status: 'streaming',
        content: assistantContent,
        content_blocks: [{ type: 'text', text: assistantContent }],
        input: {
          ...assistantEntry.input,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision },
          awaiting_approval: nextAwaitingApproval,
          awaiting_approvals: pendingApprovalsAfterDecision
        },
        output: {
          ...assistantEntry.output,
          text: assistantContent,
          runtime_events: embeddedRuntimeEvents,
          awaiting_approval: nextAwaitingApproval,
          awaiting_approvals: pendingApprovalsAfterDecision,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision }
        },
        event_seq: stateEvents.length,
        updated_at: timestamp
      })
      const savedRun = await this.store.saveRun({
        ...run,
        status: 'awaiting_decision',
        result: {
          ...run.result,
          text: assistantContent,
          runtime_events: embeddedRuntimeEvents,
          awaiting_approval: nextAwaitingApproval,
          awaiting_approvals: pendingApprovalsAfterDecision,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision }
        },
        updated_at: timestamp
      })
      return { run: savedRun, assistant_entry: updatedEntry }
    }
    const rejectedApprovalsAfterDecision = rejectedPiApprovalsFromRuntimeEvents(runtimeEventsAfterDecision)
    if (rejectedApprovalsAfterDecision.length > 0) {
      const rejectedNames = rejectedApprovalsAfterDecision.map((approval) => firstString(approval.tool_name, approval.call_id)).filter(Boolean)
      const rejectedText = `已拒绝执行工具 ${rejectedNames.join('、') || toolName}。`
      await this.ensurePiRunStateEvent({
        ...run,
        status: 'cancelled',
        error_code: 'approval_rejected',
        error_msg: rejectedText,
        finished_at: timestamp,
        updated_at: timestamp
      })
      const runtimeEvents = await replayCurrentRunEvents()
      const embeddedRuntimeEvents = summarizeRuntimeEventsForEntry(runtimeEvents)
      const updatedEntry = await this.store.saveEntry({
        ...assistantEntry,
        status: 'cancelled',
        content: rejectedText,
        content_blocks: [{ type: 'text', text: rejectedText }],
        input: {
          ...withoutPiAwaitingApprovalFields(assistantEntry.input),
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision }
        },
        output: {
          ...withoutPiAwaitingApprovalFields(assistantEntry.output),
          text: rejectedText,
          runtime_events: embeddedRuntimeEvents,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision }
        },
        event_seq: runtimeEvents.length,
        updated_at: timestamp
      })
      const { awaiting_approval: _awaitingApproval, awaiting_approvals: _awaitingApprovals, ...resultWithoutApproval } = run.result
      const savedRun = await this.store.saveRun({
        ...run,
        status: 'cancelled',
        result: {
          ...resultWithoutApproval,
          text: rejectedText,
          runtime_events: embeddedRuntimeEvents,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision }
        },
        finished_at: timestamp,
        updated_at: timestamp
      })
      await this.syncParentAfterChildTerminal(actor, savedRun, rejectedText)
      return { run: savedRun, assistant_entry: updatedEntry }
    }

    const steering = decision === 'steer'
    const steerText = steering
      ? firstString(payload.steer_text, payload.instruction, payload.reason, asRecord(payload.input_patch).instruction)
      : ''
    if (steering && !steerText) {
      throw new RuntimeDomainError('pi_steer_text_required', 'Steer requires a non-empty instruction')
    }
    if (steering) {
      await engine.eventStore.append({
        ...eventBase,
        timestamp: now(),
        type: 'session.steered',
        request_id: approvalID,
        approval_id: approvalID,
        call_id: callID,
        tool_name: toolName,
        instruction: steerText,
        message: steerText
      } as unknown as AgentRuntimeEvent)
    }
    const runningRun = await this.store.saveRun({ ...run, status: 'running', updated_at: timestamp })
    const approvalsToExecute = steering ? [] : approvedPiApprovalsReadyForExecution(await replayCurrentRunEvents())
    if (!steering && approvalsToExecute.length === 0) approvalsToExecute.push(awaitingApproval)
    const executedToolResults: RuntimeToolResultRecord[] = []
    let hasToolFailure = false
    let lastToolError = ''
    for (const approvalToExecute of approvalsToExecute) {
      const approvalCallID = firstString(approvalToExecute.call_id)
      const approvalToolName = firstString(approvalToExecute.tool_name)
      const approvalInput = asRecord(approvalToExecute.input)
      if (!approvalCallID || !approvalToolName) continue
      assertReadOnlyAllowsOperation(
        subagentModeForRun(runningRun),
        firstString(approvalToExecute.operation_type, approvalInput.operation_type)
      )
      await engine.eventStore.append({
        ...eventBase,
        timestamp: now(),
        type: 'session.tool.called',
        assistant_message_id: firstString(approvalToExecute.assistant_message_id, `assistant:${approvalCallID}`),
        call_id: approvalCallID,
        tool: approvalToolName,
        tool_name: approvalToolName,
        input: approvalInput
      } as AgentRuntimeEvent)
      let toolStatus: RuntimeToolResultRecord['status'] = 'completed'
      let toolContent = ''
      let toolStructured: Record<string, unknown> = {}
      let toolMetadata: Record<string, unknown> = {}
      let toolError = ''
      try {
        const result = await this.toolExecutor.execute({
          tool_call_id: approvalCallID,
          tool_name: approvalToolName,
          arguments: approvalInput,
          operation_type: firstString(approvalInput.operation_type),
          target_type: firstString(approvalInput.target_type),
          target_id: firstString(approvalInput.target_id),
          risk_summary: firstString(approvalInput.risk_summary, approvalToExecute.reason),
          requires_confirmation: true
        }, { actor, session, runtime_run_id: runtimeRunID, auth })
        toolContent = result.content
        toolStructured = result.structured_content || {}
        toolMetadata = result.metadata || {}
        await engine.eventStore.append({
          ...eventBase,
          timestamp: now(),
          type: 'session.tool.success',
          assistant_message_id: firstString(approvalToExecute.assistant_message_id, `assistant:${approvalCallID}`),
          call_id: approvalCallID,
          tool: approvalToolName,
          tool_name: approvalToolName,
          content: [{ type: 'text', text: toolContent }],
          structured: toolStructured,
          output: { content: toolContent, structured_content: toolStructured, metadata: toolMetadata }
        } as AgentRuntimeEvent)
      } catch (error) {
        toolStatus = 'failed'
        hasToolFailure = true
        toolError = classifyRuntimeError(error).user_message
        lastToolError = toolError
        toolContent = toolError
        await engine.eventStore.append({
          ...eventBase,
          timestamp: now(),
          type: 'session.tool.failed',
          assistant_message_id: firstString(approvalToExecute.assistant_message_id, `assistant:${approvalCallID}`),
          call_id: approvalCallID,
          tool: approvalToolName,
          tool_name: approvalToolName,
          error: { type: 'tool_error', message: toolError }
        } as AgentRuntimeEvent)
      }
      executedToolResults.push({
        tool_call_id: approvalCallID,
        tool_name: approvalToolName,
        status: toolStatus,
        content: toolContent,
        structured_content: toolStructured,
        metadata: toolMetadata,
        error: toolError || undefined,
        truncated: false,
        artifact_refs: []
      })
    }
    const existingToolResults = Array.isArray(runningRun.result.tool_results)
      ? runningRun.result.tool_results as RuntimeToolResultRecord[]
      : []
    const toolResults = [...existingToolResults, ...executedToolResults]
    const profileSnapshot = await this.executionProfileForContinuation(actor, run)
    const continuationResources = await this.buildPiContinuationResources({
      actor,
      session,
      profileSnapshot,
      run: runningRun,
      runtimeSessionID,
      auth
    })
    const modelConfig = modelSelectionFromRuntimeConfig({
      provider: profileSnapshot.provider,
      model: profileSnapshot.model,
      binding: profileSnapshot.binding,
      credential: profileSnapshot.provider_credential_ref,
      inference: profileSnapshot.inference
    })
    const model = modelConfig ? { provider_id: modelConfig.provider_id, id: modelConfig.id } : undefined
    const promptConfig = asRecord(profileSnapshot.prompt)
    const systemPrompt = firstString(promptConfig.system, promptConfig.system_prompt, promptConfig.systemPrompt)
    const continuationPrompt = steering
      ? `user_steer:\n${steerText}`
      : toolResults.length === 1
      ? `tool_result:\n${JSON.stringify({
        call_id: toolResults[0].tool_call_id,
        tool_name: toolResults[0].tool_name,
        status: toolResults[0].status,
        content: toolResults[0].content,
        structured_content: toolResults[0].structured_content,
        metadata: toolResults[0].metadata
      })}`
      : `tool_results:\n${JSON.stringify({ results: toolResults.map((result) => ({
        call_id: result.tool_call_id,
        tool_name: result.tool_name,
        status: result.status,
        content: result.content,
        structured_content: result.structured_content,
        metadata: result.metadata,
        error: result.error
      })) })}`
    let harnessResult: AgentHarnessRunResult | undefined
    let assistantContent = ''
    let continuationError: string | undefined
    let continuationAwaitingApproval = false
    try {
      harnessResult = await this.runPiHarnessWithLease(runningRun, {
        sessionID: runtimeSessionID,
        runtimeRunID,
        prompt: continuationPrompt,
        history: piSessionHistoryFromEntries(entries, runtimeRunID),
        agent: profileSnapshot.name,
        systemPrompt,
        model,
        modelConfig,
        tools: continuationResources.piResources.tools,
        skills: continuationResources.piResources.skills,
        skillInvocation: continuationResources.skillInvocation
      })
      assistantContent = textFromRuntimeEvents(await replayCurrentRunEvents())
    } catch (error) {
      if (error instanceof PiToolApprovalRequiredError) {
        continuationAwaitingApproval = true
        assistantContent = textFromRuntimeEvents(await replayCurrentRunEvents())
      } else {
        continuationError = classifyRuntimeError(error).user_message
        assistantContent = hasToolFailure
          ? `工具执行失败：${lastToolError || continuationError}`
          : `工具已执行，结果：${firstString(JSON.stringify(toolResults.map((result) => result.structured_content)), toolResults.map((result) => result.content).join('\n'), '已完成')}`
      }
    }
    const runtimeEvents = await replayCurrentRunEvents()
    const pendingApprovalsAfterContinuation = continuationError || hasToolFailure
      ? []
      : pendingPiApprovalsFromRuntimeEvents(runtimeEvents)
    if (continuationAwaitingApproval || pendingApprovalsAfterContinuation.length > 0) {
      const awaitingTimestamp = now()
      const allStateEvents = await this.ensurePiRunStateEvent({ ...runningRun, status: 'awaiting_decision', updated_at: awaitingTimestamp })
      const boundaryEvents = pendingApprovalsAfterContinuation.length > 0
        ? runtimeEventsUntilPendingPiApprovals(runtimeEvents, pendingApprovalsAfterContinuation)
        : runtimeEvents
      const boundaryEventIDs = new Set(boundaryEvents.map((event) => event.event_id))
      const awaitingRuntimeEvents = [
        ...boundaryEvents,
        ...allStateEvents.filter((event) => event.type === 'run.awaiting_decision' && !boundaryEventIDs.has(event.event_id))
      ].sort((left, right) => Number(left.seq || 0) - Number(right.seq || 0))
      const awaitingEmbeddedRuntimeEvents = summarizeRuntimeEventsForEntry(awaitingRuntimeEvents)
      const nextAwaitingApproval = pendingApprovalsAfterContinuation[0]
      const updatedEntry = await this.store.saveEntry({
        ...assistantEntry,
        status: 'streaming',
        content: assistantContent,
        content_blocks: [{ type: 'text', text: assistantContent }],
        input: {
          ...assistantEntry.input,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision },
          awaiting_approval: nextAwaitingApproval,
          awaiting_approvals: pendingApprovalsAfterContinuation
        },
        output: {
          ...assistantEntry.output,
          text: assistantContent,
          runtime_events: awaitingEmbeddedRuntimeEvents,
          awaiting_approval: nextAwaitingApproval,
          awaiting_approvals: pendingApprovalsAfterContinuation,
          tool_results: toolResults,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision },
          harness_result: harnessResult,
          pi_resources: continuationResources.piResources.summary
        },
        event_seq: awaitingRuntimeEvents.length,
        updated_at: awaitingTimestamp
      })
      const savedRun = await this.store.saveRun({
        ...runningRun,
        status: 'awaiting_decision',
        result: {
          ...runningRun.result,
          text: assistantContent,
          runtime_events: awaitingEmbeddedRuntimeEvents,
          awaiting_approval: nextAwaitingApproval,
          awaiting_approvals: pendingApprovalsAfterContinuation,
          tool_results: toolResults,
          approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision },
          harness_result: harnessResult,
          pi_resources: continuationResources.piResources.summary
        },
        updated_at: awaitingTimestamp
      })
      return { run: savedRun, assistant_entry: updatedEntry }
    }
    const finalStatus: AIRuntimeRun['status'] = continuationError || hasToolFailure ? 'failed' : 'completed'
    const finalTimestamp = now()
    const finalErrorCode = finalStatus === 'failed' ? 'pi_approval_continuation_failed' : undefined
    const finalErrorMessage = continuationError || lastToolError || undefined
    await this.ensurePiRunStateEvent({
      ...runningRun,
      status: finalStatus,
      error_code: finalErrorCode,
      error_msg: finalErrorMessage,
      finished_at: finalTimestamp,
      updated_at: finalTimestamp
    })
    const finalRuntimeEvents = await replayCurrentRunEvents()
    const finalEmbeddedRuntimeEvents = summarizeRuntimeEventsForEntry(finalRuntimeEvents)
    const updatedEntry = await this.store.saveEntry({
      ...assistantEntry,
      status: finalStatus === 'failed' ? 'failed' : 'completed',
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: {
        ...withoutPiAwaitingApprovalFields(assistantEntry.input),
        approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision }
      },
      output: {
        ...withoutPiAwaitingApprovalFields(assistantEntry.output),
        text: assistantContent,
        runtime_events: finalEmbeddedRuntimeEvents,
        tool_results: toolResults,
        approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision },
        harness_result: harnessResult
      },
      event_seq: finalRuntimeEvents.length,
      updated_at: finalTimestamp
    })
    const { awaiting_approval: _awaitingApproval, awaiting_approvals: _awaitingApprovals, ...resultWithoutApproval } = runningRun.result
    const savedRun = await this.store.saveRun({
      ...runningRun,
      status: finalStatus,
      result: {
        ...resultWithoutApproval,
        text: assistantContent,
        runtime_events: finalEmbeddedRuntimeEvents,
        tool_results: toolResults,
        approval_decision: { approval_id: approvalID, call_id: callID, tool_name: toolName, decision },
        harness_result: harnessResult
      },
      error_code: finalErrorCode,
      error_msg: finalErrorMessage,
      finished_at: finalTimestamp,
      updated_at: finalTimestamp
    })
    await this.syncParentAfterChildTerminal(actor, savedRun, assistantContent)
    return { run: savedRun, assistant_entry: updatedEntry }
  }

  async *streamPiApproval(actor: RuntimeActor, runtimeRunID: string, payload: Record<string, unknown> = {}, auth?: RuntimeAuth): AsyncIterable<RuntimeStreamEvent> {
    const startedAt = performance.now()
    if (!this.agentRuntimeEngine) {
      throw new RuntimeDomainError('pi_runtime_engine_unavailable', 'Pi runtime engine is not configured', 500)
    }
    const run = await this.store.getRun(actor.workspace_id, runtimeRunID)
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    const runtimeSessionID = firstString(run.result.runtime_session_id, run.request.runtime_session_id)
    if (!runtimeSessionID) {
      throw new RuntimeDomainError('pi_runtime_session_missing', 'Pi runtime session is missing', 409)
    }
    const pendingEvents: AgentRuntimeEvent[] = []
    let wake: (() => void) | undefined
    const unsubscribe = this.agentRuntimeEngine.eventStore.subscribe(runtimeSessionID, (event) => {
      pendingEvents.push(event)
      wake?.()
      wake = undefined
    })
    const resultPromise = this.decidePiApproval(actor, runtimeRunID, payload, auth)
    let result: Awaited<ReturnType<typeof this.decidePiApproval>> | undefined
    let runError: unknown
    resultPromise.then((value) => {
      result = value
      wake?.()
      wake = undefined
    }, (error) => {
      runError = error
      wake?.()
      wake = undefined
    })
    try {
      while (!result && !runError) {
        while (pendingEvents.length > 0) {
          yield this.streamEvent('runtime_event', startedAt, { event: pendingEvents.shift() as AgentRuntimeEvent })
        }
        if (result || runError) break
        await new Promise<void>((resolve) => { wake = resolve })
      }
      while (pendingEvents.length > 0) {
        yield this.streamEvent('runtime_event', startedAt, { event: pendingEvents.shift() as AgentRuntimeEvent })
      }
      if (runError) throw runError
    } finally {
      unsubscribe()
    }
    if (!result) {
      throw new RuntimeDomainError('pi_approval_result_missing', 'Pi approval did not return a result', 500)
    }
    yield this.streamEvent('assistant_entry', startedAt, { entry: result.assistant_entry, run: result.run, timings: { total_ms: elapsedMs(startedAt) } })
    yield this.streamEvent('done', startedAt, { timings: { total_ms: elapsedMs(startedAt) } })
  }

  async *streamEntryRun(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>, auth?: RuntimeAuth): AsyncIterable<RuntimeStreamEvent> {
    const startedAt = performance.now()
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const content = asString(payload.content).trim()
    if (!content) {
      throw new RuntimeDomainError('entry_content_required', 'Entry content is required')
    }
    const {
      resolvedProfile,
      persistedProfileSnapshot,
      persistedProfileSnapshotHash,
      executionProfileSnapshot: profileSnapshot
    } = await this.profileSnapshotsForNewRun(actor, session)
    await this.contextTagRegistry.resolve(profileSnapshot.context_tags, { actor, profile: profileSnapshot, content, payload })
    enforceChatboxInputLimit(profileSnapshot, content)
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const publicSessionID = sessionBusinessID(actor.workspace_id, session.id)
    const clientEntryID = requireClientID('client_entry_id', payload.client_entry_id)
    const requestID = firstString(payload.runtime_request_id)
    const userEntryIdempotencyKey = runtimeIdempotencyKey('entry', publicSessionID, 'client', clientEntryID)
    const existingUserEntry = entries.find((entry) => entry.idempotency_key === userEntryIdempotencyKey)
    const existingAssistantEntry = existingUserEntry
      ? entries.find((entry) => entry.parent_entry_id === existingUserEntry.id && entry.role === 'assistant')
      : undefined
    if (existingUserEntry && existingAssistantEntry) {
      const existingRun = existingAssistantEntry.runtime_run_id
        ? await this.store.getRun(actor.workspace_id, existingAssistantEntry.runtime_run_id)
        : undefined
      const replayRuntimeEvents = Array.isArray(existingAssistantEntry.output.runtime_events)
        ? existingAssistantEntry.output.runtime_events
        : []
      for (const event of replayRuntimeEvents) {
        yield this.streamEvent('runtime_event', startedAt, { event: event as AgentRuntimeEvent, idempotent: true })
      }
      yield this.streamEvent('user_entry', startedAt, { entry: existingUserEntry, idempotent: true })
      yield this.streamEvent('assistant_entry', startedAt, { entry: existingAssistantEntry, run: existingRun || null, idempotent: true })
      yield this.streamEvent('done', startedAt, { idempotent: true, timings: { total_ms: elapsedMs(startedAt) } })
      return
    }
    await this.assertSessionHasNoActiveRun(actor, session)
    if (this.shouldUsePiRuntime(payload)) {
      const runtimeSessionID = firstString(payload.runtime_session_id, publicSessionID)
      const pendingEvents: AgentRuntimeEvent[] = []
      let wake: (() => void) | undefined
      const unsubscribe = this.agentRuntimeEngine?.eventStore.subscribe(runtimeSessionID, (event) => {
        pendingEvents.push(event)
        wake?.()
        wake = undefined
      })
      const resultPromise = this.createPiEntryRun(actor, session, persistedProfileSnapshot, profileSnapshot, resolvedProfile, entries, publicSessionID, content, payload, auth)
      let result: Awaited<ReturnType<typeof this.createPiEntryRun>> | undefined
      let runError: unknown
      resultPromise.then((value) => {
        result = value
        wake?.()
        wake = undefined
      }, (error) => {
        runError = error
        wake?.()
        wake = undefined
      })
      try {
        while (!result && !runError) {
          while (pendingEvents.length > 0) {
            yield this.streamEvent('runtime_event', startedAt, { event: pendingEvents.shift() as AgentRuntimeEvent })
          }
          if (result || runError) break
          await new Promise<void>((resolve) => { wake = resolve })
        }
        while (pendingEvents.length > 0) {
          yield this.streamEvent('runtime_event', startedAt, { event: pendingEvents.shift() as AgentRuntimeEvent })
        }
        if (runError) throw runError
      } finally {
        unsubscribe?.()
      }
      if (!result) {
        throw new RuntimeDomainError('pi_runtime_result_missing', 'Pi runtime did not return a result', 500)
      }
      yield this.streamEvent('user_entry', startedAt, { entry: result.user_entry })
      yield this.streamEvent('assistant_entry', startedAt, { entry: result.assistant_entry, run: result.run, timings: { total_ms: elapsedMs(startedAt) } })
      yield this.streamEvent('done', startedAt, { timings: { total_ms: elapsedMs(startedAt) } })
      return
    }
    const seqBase = entries.length
    const timestamp = now()
    const attachments = entryAttachmentsFromPayload(payload)
    const userEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: seqBase + 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content,
      content_blocks: [{ type: 'text', text: content }],
      input: {
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        ...(attachments.length > 0 ? { attachments } : {}),
        ...(requestID ? { request_id: requestID } : {})
      },
      output: {},
      idempotency_key: userEntryIdempotencyKey,
      created_at: timestamp,
      updated_at: timestamp
    }

    const runID = await this.store.nextRunId()
    const runtimeRunID = runBusinessID(actor.workspace_id, session.id, runID)
    let run: AIRuntimeRun = {
      id: runID,
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [...profileSnapshot.context_tags],
      agent_profile_id: resolvedProfile.profile.id,
      agent_profile_version_id: resolvedProfile.profileVersionID,
      agent_profile_version_key: resolvedProfile.profileVersionKey,
      agent_profile_snapshot_hash: persistedProfileSnapshotHash,
      agent_workspace_runtime_id: session.agent_workspace_runtime_id,
      agent_workspace_snapshot_hash: session.agent_workspace_snapshot_hash,
      profile_snapshot: persistedProfileSnapshot,
      status: 'running',
      input_entry_id: userEntry.id,
      request: {
        content,
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        agent_profile_version_key: resolvedProfile.profileVersionKey,
        ...(attachments.length > 0 ? { attachments } : {}),
        ...(requestID ? { request_id: requestID } : {}),
        ...(firstString(payload.parent_runtime_run_id) ? { parent_runtime_run_id: firstString(payload.parent_runtime_run_id) } : {})
      },
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    }
    run = await this.store.saveRun(run)
    await this.store.saveEntry(userEntry)
    this.enqueueSessionTitleGeneration(actor, session, profileSnapshot, content, entries.length)
    yield this.streamEvent('user_entry', startedAt, { entry: userEntry })
    const initialStepEvents: RuntimeEventRecord[] = []
    const acceptedStepEvent = await this.persistStepEvent(actor, session, runtimeRunID, 'accepted', startedAt)
    initialStepEvents.push(acceptedStepEvent)
    yield this.streamEvent(acceptedStepEvent.type, startedAt, acceptedStepEvent.payload)

    const livePreparedEvents: RuntimeEventRecord[] = []
    let livePreparedEventNotify: (() => void) | null = null
    const notifyLivePreparedEvent = () => {
      if (livePreparedEventNotify) {
        livePreparedEventNotify()
        livePreparedEventNotify = null
      }
    }
    const waitForLivePreparedEvent = () => new Promise<void>((resolve) => {
      livePreparedEventNotify = resolve
    })
    let prepareCompleted = false
    const prepareContextPromise = this.prepareExecutionContext(actor, session, profileSnapshot, content, payload, runtimeRunID, auth, async (event) => {
      const persisted = await this.persistRuntimeEvents(actor, session, runtimeRunID, [event])
      livePreparedEvents.push(...persisted)
      notifyLivePreparedEvent()
      return persisted
    })
    prepareContextPromise.then(() => {
      prepareCompleted = true
      notifyLivePreparedEvent()
    }, () => {
      prepareCompleted = true
      notifyLivePreparedEvent()
    })
    while (!prepareCompleted || livePreparedEvents.length > 0) {
      while (livePreparedEvents.length > 0) {
        const runtimeEvent = livePreparedEvents.shift()
        if (!runtimeEvent) continue
        yield this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload)
      }
      if (!prepareCompleted) {
        await waitForLivePreparedEvent()
      }
    }
    const executionContext = await prepareContextPromise
    run = await this.store.saveRun({
      ...run,
      request: {
        ...run.request,
        model_content: executionContext.modelContent,
        context_tags: executionContext.contextTagAssembly.tags,
        context_tag_context: executionContext.contextTagAssembly.fragments.map((fragment) => ({
          tag: fragment.tag,
          order: fragment.order,
          required: fragment.required,
          title: fragment.title,
          data: fragment.data
        })),
        profile_snapshot: persistedProfileSnapshot
      },
      updated_at: now()
    })
    const preparedRuntimeEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, executionContext.runtimeEvents)
    executionContext.runtimeEvents = [...initialStepEvents, ...preparedRuntimeEvents]

    const assistantEntryDraft: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: seqBase + 2,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: {},
      output: {
        text: '',
        timings: {}
      },
      runtime_run_id: runtimeRunID,
      event_seq: 1,
      idempotency_key: entryRuntimeIdempotencyKey(publicSessionID, runtimeRunID, 'assistant', seqBase + 2),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(assistantEntryDraft)
    await this.saveSessionProgress(actor, session, {
      entry_count: seqBase + 2,
      last_entry_at: assistantEntryDraft.created_at
    })

    const timings: Record<string, number> = {
      accepted_ms: elapsedMs(startedAt)
    }
    let completion: ChatModelResult | null = null
    let assistantContent = ''
    let reasoning = ''
    let assistantStatus: AISessionEntry['status'] = 'completed'
    let assistantEntryType: AISessionEntry['entry_type'] = 'message'
    let runStatus: AIRuntimeRun['status'] = 'completed'
    let errorCode = ''
    let errorMessage = ''
    let agentActions: Record<string, unknown>[] = [...executionContext.agentActions]
    let toolResults: RuntimeToolResultRecord[] = []
    let providerRawRefs: Record<string, unknown>[] = []
    const handledToolCallIDs = new Set<string>()
    const streamToolCalls: Array<RuntimeToolCall | ChatModelToolCall> = []
    let answerStreamMode: 'undecided' | 'streaming' | 'buffering_json' = 'undecided'
    let streamedAnswerDelta = false
    let runtimeOutput: RuntimeOutputPayload = {
      structured_output: {},
      output_schema_valid: false,
      output_schema_errors: [],
      runtime_events: [],
      capabilities: {},
      loaded_skills: [],
      subagent_results: [],
      agent_actions: [],
      tool_results: []
    }
    try {
      timings.profile_resolved_ms = elapsedMs(startedAt)
      const profileResolvedStepEvent = await this.persistStepEvent(actor, session, runtimeRunID, 'profile_resolved', startedAt)
      executionContext.runtimeEvents.push(profileResolvedStepEvent)
      yield this.streamEvent(profileResolvedStepEvent.type, startedAt, profileResolvedStepEvent.payload)
      const initialStreamRequest: ChatModelRequest = {
        profile: profileSnapshot,
        session,
        history: entries,
        content: executionContext.modelContent,
        request_id: requestID,
        runtime_run_id: runtimeRunID,
        parent_runtime_run_id: firstString(payload.parent_runtime_run_id),
        context_ref: asRecord(payload.context_ref),
        include_current_page: Boolean(payload.include_current_page),
        tools: executionContext.mcpTools,
        capabilities: executionContext.capabilities,
        loaded_skills: executionContext.loadedSkills,
        subagent_results: executionContext.subagentResults,
        output_schema: executionContext.outputSchema,
        developer_instructions: executionContext.developerInstructions
      }
      const initialStreamOptions: ModelCallEventOptions = { phase: 'initial', round: 1 }
      const initialModelEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, [
        {
          type: 'model.request_prepared',
          payload: modelRequestEventPayload(initialStreamRequest, initialStreamOptions)
        },
        {
          type: 'model.call_started',
          payload: modelRequestEventPayload(initialStreamRequest, initialStreamOptions)
        }
      ])
      executionContext.runtimeEvents.push(...initialModelEvents)
      for (const runtimeEvent of initialModelEvents) {
        yield this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload)
      }
      for await (const event of this.streamModel(initialStreamRequest)) {
        if (event.type === 'reasoning_delta') {
          if (timings.first_reasoning_ms === undefined) {
            timings.first_reasoning_ms = elapsedMs(startedAt)
            const firstReasoningStepEvent = await this.persistStepEvent(actor, session, runtimeRunID, 'first_reasoning', startedAt)
            executionContext.runtimeEvents.push(firstReasoningStepEvent)
            yield this.streamEvent(firstReasoningStepEvent.type, startedAt, firstReasoningStepEvent.payload)
          }
          reasoning += event.delta
          const [reasoningEvent] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
            type: 'reasoning_delta',
            payload: {
              delta: event.delta,
              elapsed_ms: elapsedMs(startedAt)
            }
          }])
          executionContext.runtimeEvents.push(reasoningEvent)
          yield this.streamEvent(reasoningEvent.type, startedAt, reasoningEvent.payload)
        }
        if (event.type === 'tool_call') {
          streamToolCalls.push(event.tool_call)
        }
        if (event.type === 'answer_delta') {
          assistantContent += event.delta
          if (answerStreamMode === 'undecided') {
            const trimmed = assistantContent.trimStart()
            if (trimmed) {
              answerStreamMode = trimmed.startsWith('{') || trimmed.startsWith('[') || trimmed.startsWith('```')
                ? 'buffering_json'
                : 'streaming'
            }
          }
          if (answerStreamMode === 'streaming') {
            if (timings.first_answer_ms === undefined) {
              timings.first_answer_ms = elapsedMs(startedAt)
              const firstAnswerStepEvent = await this.persistStepEvent(actor, session, runtimeRunID, 'first_answer', startedAt)
              executionContext.runtimeEvents.push(firstAnswerStepEvent)
              yield this.streamEvent(firstAnswerStepEvent.type, startedAt, firstAnswerStepEvent.payload)
            }
            streamedAnswerDelta = true
            const [answerEvent] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
              type: 'answer_delta',
              payload: {
                delta: event.delta,
                elapsed_ms: elapsedMs(startedAt)
              }
            }])
            executionContext.runtimeEvents.push(answerEvent)
            yield this.streamEvent(answerEvent.type, startedAt, answerEvent.payload)
          }
        }
      }
      const initialModelCompletedEvent = (await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
        type: 'model.call_completed',
        payload: {
          phase: initialStreamOptions.phase,
          round: initialStreamOptions.round,
          status: assistantContent.trim() || streamToolCalls.length > 0 ? 'completed' : 'empty',
          text_chars: assistantContent.length,
          tool_call_count: streamToolCalls.length,
          reasoning_chars: reasoning.length
        }
      }]))[0]
      executionContext.runtimeEvents.push(initialModelCompletedEvent)
      yield this.streamEvent(initialModelCompletedEvent.type, startedAt, initialModelCompletedEvent.payload)
      if (!assistantContent.trim() && streamToolCalls.length === 0) {
        const fallbackRequest: ChatModelRequest = {
          profile: profileSnapshot,
          session,
          history: entries,
          content: executionContext.modelContent,
          request_id: requestID,
          runtime_run_id: runtimeRunID,
          parent_runtime_run_id: firstString(payload.parent_runtime_run_id),
          context_ref: asRecord(payload.context_ref),
          include_current_page: Boolean(payload.include_current_page),
          tools: executionContext.mcpTools,
          capabilities: executionContext.capabilities,
          loaded_skills: executionContext.loadedSkills,
          subagent_results: executionContext.subagentResults,
          output_schema: executionContext.outputSchema,
          developer_instructions: executionContext.developerInstructions
        }
        const fallbackOptions: ModelCallEventOptions = { phase: 'fallback', round: 2 }
        const [fallbackRequestEvent] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
          type: 'model.request_prepared',
          payload: modelRequestEventPayload(fallbackRequest, fallbackOptions)
        }])
        executionContext.runtimeEvents.push(fallbackRequestEvent)
        yield this.streamEvent(fallbackRequestEvent.type, startedAt, fallbackRequestEvent.payload)
        const fallbackTurn = await this.completeWithBoundedContinuation(fallbackRequest, fallbackOptions)
        completion = fallbackTurn.completion
        if (fallbackTurn.events.length > 0) {
          const persistedProviderEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, fallbackTurn.events)
          executionContext.runtimeEvents.push(...persistedProviderEvents)
          for (const runtimeEvent of persistedProviderEvents) {
            yield this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload)
          }
        }
        const providerRaw = await this.persistProviderRawFromCompletion(actor, session, runtimeRunID, completion, 1)
        providerRawRefs = [...providerRawRefs, ...providerRaw.refs]
        if (providerRaw.events.length > 0) {
          const persistedRawEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, providerRaw.events)
          executionContext.runtimeEvents.push(...persistedRawEvents)
          for (const runtimeEvent of persistedRawEvents) {
            yield this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload)
          }
        }
        assistantContent = completion?.text || ''
      }
      const assistantContentBeforeToolLoop = assistantContent
      const liveToolEvents: RuntimeEventRecord[] = []
      let liveToolEventNotify: (() => void) | null = null
      const notifyLiveToolEvent = () => {
        if (liveToolEventNotify) {
          liveToolEventNotify()
          liveToolEventNotify = null
        }
      }
      const waitForLiveToolEvent = () => new Promise<void>((resolve) => {
        liveToolEventNotify = resolve
      })
      let toolLoopCompleted = false
      const emitLiveToolEvents = async (events: RuntimeEventRecord[]) => {
        if (events.length === 0) return
        const persistedEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, events)
        executionContext.runtimeEvents.push(...persistedEvents)
        liveToolEvents.push(...persistedEvents)
        notifyLiveToolEvent()
      }
      const toolLoopPromise = this.continueAutomaticToolLoop({
        actor,
        session,
        runtimeRunID,
        requestID,
        parentRuntimeRunID: firstString(payload.parent_runtime_run_id),
        profile: profileSnapshot,
        history: [...entries, userEntry],
        executionContext,
        assistantContent,
        completion,
        initialToolCalls: [
          ...streamToolCalls,
          ...(completion?.tool_calls || []),
          ...extractToolCallsFromText(assistantContent)
        ],
        existingToolCallIDs: handledToolCallIDs,
        auth,
        emitEvents: emitLiveToolEvents
      })
      toolLoopPromise.then(() => {
        toolLoopCompleted = true
        notifyLiveToolEvent()
      }, () => {
        toolLoopCompleted = true
        notifyLiveToolEvent()
      })
      while (!toolLoopCompleted || liveToolEvents.length > 0) {
        while (liveToolEvents.length > 0) {
          const runtimeEvent = liveToolEvents.shift()
          if (!runtimeEvent) continue
          yield this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload)
        }
        if (!toolLoopCompleted) {
          await waitForLiveToolEvent()
        }
      }
      const toolLoop = await toolLoopPromise
      if (toolLoop.events.length > 0) {
        agentActions = [...agentActions, ...toolLoop.agentActions]
        toolResults = [...toolResults, ...toolLoop.toolResults]
        assistantContent = toolLoop.assistantContent
        completion = toolLoop.completion
        if (toolLoop.completion) {
          const providerRaw = await this.persistProviderRawFromCompletion(actor, session, runtimeRunID, toolLoop.completion, providerRawRefs.length + 1)
          providerRawRefs = [...providerRawRefs, ...providerRaw.refs]
          const persistedRawEvents = await this.persistRuntimeEvents(actor, session, runtimeRunID, providerRaw.events)
          executionContext.runtimeEvents.push(...persistedRawEvents)
          for (const runtimeEvent of persistedRawEvents) {
            yield this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload)
          }
        }
      }
      const shouldEmitBufferedAnswer =
        !streamedAnswerDelta &&
        assistantContent &&
        (toolLoop.events.length === 0 || assistantContent !== assistantContentBeforeToolLoop)
      if (shouldEmitBufferedAnswer) {
        if (timings.first_answer_ms === undefined) {
          timings.first_answer_ms = elapsedMs(startedAt)
          const firstAnswerStepEvent = await this.persistStepEvent(actor, session, runtimeRunID, 'first_answer', startedAt)
          executionContext.runtimeEvents.push(firstAnswerStepEvent)
          yield this.streamEvent(firstAnswerStepEvent.type, startedAt, firstAnswerStepEvent.payload)
        }
        streamedAnswerDelta = true
        const [answerEvent] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
          type: 'answer_delta',
          payload: {
            delta: assistantContent,
            elapsed_ms: elapsedMs(startedAt)
          }
        }])
        executionContext.runtimeEvents.push(answerEvent)
        yield this.streamEvent(answerEvent.type, startedAt, answerEvent.payload)
      }
      const outputValidation = this.validateOutput(profileSnapshot, assistantContent)
      const [validationEvent] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
        type: outputValidation.valid ? 'output_schema.validated' : 'output_schema.invalid',
        payload: {
          valid: outputValidation.valid,
          errors: outputValidation.errors,
          has_schema: hasSchema(executionContext.outputSchema)
        }
      }])
      executionContext.runtimeEvents.push(validationEvent)
      yield this.streamEvent(validationEvent.type, startedAt, validationEvent.payload)
      runtimeOutput = {
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: executionContext.runtimeEvents,
        capabilities: executionContext.capabilities,
        loaded_skills: executionContext.loadedSkills,
        subagent_results: executionContext.subagentResults,
        agent_actions: agentActions,
        tool_results: toolResults,
        provider_raw_refs: providerRawRefs
      }
      if (!outputValidation.valid && hasSchema(executionContext.outputSchema)) {
        runStatus = 'failed'
        assistantStatus = 'failed'
        assistantEntryType = 'error'
        errorCode = 'output_schema_invalid'
        errorMessage = outputValidation.errors.join('; ')
      }
      if (runStatus === 'completed' && hasAwaitingDecisionAction(agentActions)) {
        runStatus = 'awaiting_decision'
      }
    } catch (error) {
      const errorDescriptor = classifyRuntimeError(error, error instanceof ModelProviderError ? { source: 'provider' } : {})
      const publicError = {
        ...runtimeErrorPublicPayload(errorDescriptor),
        ...(requestID ? { request_id: requestID } : {})
      }
      runStatus = errorDescriptor.terminal_status
      assistantStatus = 'failed'
      assistantEntryType = 'error'
      errorCode = errorDescriptor.code
      errorMessage = errorDescriptor.user_message
      assistantContent = `模型调用失败：${errorDescriptor.user_message}`
      const errorEvent = {
        type: 'run.error',
        payload: publicError
      }
      const persistedErrorEvents = executionContext
        ? await this.persistRuntimeEvents(actor, session, runtimeRunID, [errorEvent])
        : [errorEvent]
      if (executionContext) {
        executionContext.runtimeEvents.push(...persistedErrorEvents)
      }
      runtimeOutput = {
        structured_output: {},
        output_schema_valid: false,
        output_schema_errors: [errorDescriptor.user_message],
        runtime_events: executionContext?.runtimeEvents || persistedErrorEvents,
        capabilities: executionContext?.capabilities || {},
        loaded_skills: executionContext?.loadedSkills || [],
        subagent_results: executionContext?.subagentResults || [],
        agent_actions: agentActions,
        tool_results: toolResults,
        provider_raw_refs: providerRawRefs,
        provider_error: publicError
      }
      yield this.streamEvent('error', startedAt, publicError)
    }
    timings.total_ms = elapsedMs(startedAt)
    const completedStepEvent = await this.persistStepEvent(actor, session, runtimeRunID, 'completed', startedAt)
    executionContext.runtimeEvents.push(completedStepEvent)
    yield this.streamEvent(completedStepEvent.type, startedAt, completedStepEvent.payload)
    const runStateEventType = runStateEventTypeForStatus(runStatus)
    if (runStateEventType) {
      const [runStateEvent] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
        type: runStateEventType,
        payload: runStateEventPayload(runtimeRunID, runStatus, errorCode, errorMessage)
      }])
      executionContext.runtimeEvents.push(runStateEvent)
      runtimeOutput.runtime_events = executionContext.runtimeEvents
      yield this.streamEvent(runStateEvent.type, startedAt, runStateEvent.payload)
    }
    const finalizedTimings = attachActiveDurationTimings(timings, executionContext.runtimeEvents)
    Object.assign(timings, finalizedTimings)
    const assistantEntry: AISessionEntry = {
      ...assistantEntryDraft,
      entry_type: assistantEntryType,
      status: assistantStatus,
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      output: {
        text: assistantContent,
        reasoning,
        timings,
        ...runtimeOutput
      },
      updated_at: now()
    }
    await this.store.saveEntry(assistantEntry)

    const completedRun: AIRuntimeRun = {
      ...run,
      status: runStatus,
      output_entry_id: assistantEntry.id,
      result: {
        text: assistantContent,
        reasoning,
        timings,
        ...runtimeOutput
      },
      usage: completion?.usage || {},
      error_code: errorCode || undefined,
      error_msg: errorMessage || undefined,
      finished_at: isTerminalRunStatus(runStatus) ? now() : undefined,
      updated_at: now()
    }
    const savedCompletedRun = await this.store.saveRun(completedRun)
    await this.saveSessionProgress(actor, session, {
      entry_count: seqBase + 2,
      last_entry_at: assistantEntry.created_at
    })

    yield this.streamEvent('assistant_entry', startedAt, { entry: assistantEntry, run: savedCompletedRun, timings })
    yield this.streamEvent('done', startedAt, { timings })
  }

  async cancelSessionRun(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown> = {}) {
    const session = await this.assertSessionReadable(actor, sessionID, 'active_only')
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const requestedRunID = firstString(payload.runtime_run_id)
    if (!requestedRunID) {
      throw new RuntimeDomainError('runtime_run_id_required', 'runtime_run_id is required to cancel generation', 400)
    }
    const runtimeRunID = requestedRunID
    const run = await this.store.getRun(actor.workspace_id, runtimeRunID)
    if (!run || run.session_id !== session.id) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    if (!ACTIVE_RUN_STATUSES.has(run.status)) {
      throw new RuntimeDomainError('runtime_run_not_cancellable', `Runtime run is already ${run.status}`, 409)
    }
    const timestamp = now()
    const reason = firstString(payload.reason, 'User cancelled generation')
    const cancellableEntries = entries.filter((entry) =>
      entry.runtime_run_id === runtimeRunID && ['queued', 'running', 'streaming'].includes(entry.status)
    )
    for (const entry of cancellableEntries) {
      await this.store.saveEntry({
        ...entry,
        status: 'cancelled',
        content: entry.content || '已停止生成',
        content_blocks: entry.content_blocks.length > 0 ? entry.content_blocks : [{ type: 'text', text: '已停止生成' }],
        updated_at: timestamp
      })
    }
    const cancelledRun: AIRuntimeRun = {
      ...run,
      status: 'cancelled',
      error_code: 'user_cancelled',
      error_msg: reason,
      finished_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveRun(cancelledRun)
    await this.cancelActiveChildRuns(actor, runtimeRunID, reason)
    await this.abortPiHarnessRun(run)
    await this.recordPiCancellationEvents(run, timestamp, reason)
    const cancellationEvents = await this.ensureRunStateEvent(cancelledRun)
    const embeddedCancellationEvents = summarizeRuntimeEventsForEntry(cancellationEvents)
    for (const entry of cancellableEntries) {
      if (cancellationEvents.length === 0) continue
      await this.store.saveEntry({
        ...entry,
        status: 'cancelled',
        content: entry.content || '已停止生成',
        content_blocks: entry.content_blocks.length > 0 ? entry.content_blocks : [{ type: 'text', text: '已停止生成' }],
        output: {
          ...entry.output,
          runtime_events: embeddedCancellationEvents
        },
        updated_at: timestamp
      })
    }
    const cancelledRunWithEvents: AIRuntimeRun = {
      ...run,
      status: 'cancelled',
      error_code: 'user_cancelled',
      error_msg: reason,
      result: cancellationEvents.length > 0 ? {
        ...run.result,
        runtime_engine: firstString(run.result.runtime_engine, run.request.runtime_engine),
        runtime_session_id: firstString(run.result.runtime_session_id, run.request.runtime_session_id),
        runtime_events: embeddedCancellationEvents
      } : run.result,
      finished_at: timestamp,
      updated_at: timestamp
    }
    const savedCancelledRun = await this.store.saveRun(cancelledRunWithEvents)
    await this.saveSessionProgress(actor, session, {
      updated_at: timestamp
    })
    return {
      runtime_run_id: runtimeRunID,
      status: savedCancelledRun.status,
      run: savedCancelledRun,
      cancelled_entries: cancellableEntries.map((entry) => entry.id)
    }
  }

  private async cancelActiveChildRuns(actor: RuntimeActor, parentRuntimeRunID: string, reason: string) {
    const links = await this.store.listChildRunLinks(actor.workspace_id, parentRuntimeRunID)
    for (const link of links) {
      if (!['queued', 'running', 'blocked_approval'].includes(link.status)) continue
      const childRun = await this.store.getRun(actor.workspace_id, link.child_runtime_run_id)
      if (!childRun || !ACTIVE_RUN_STATUSES.has(childRun.status)) continue
      const childSession = await this.store.getSession(actor.workspace_id, childRun.session_id)
      if (!childSession) continue
      const cancelled = await this.cancelSessionRun(actor, childSession.id, {
        runtime_run_id: childRun.runtime_run_id,
        reason: `Parent run cancelled: ${reason}`
      })
      await this.syncParentAfterChildTerminal(actor, cancelled.run, cancelled.run.error_msg || reason)
    }
  }

  private async recordPiCancellationEvents(run: AIRuntimeRun, timestamp: string, reason: string) {
    if (!this.agentRuntimeEngine || firstString(run.result.runtime_engine, run.request.runtime_engine) !== 'pi') return []
    const engine = this.agentRuntimeEngine
    const runtimeSessionID = firstString(run.result.runtime_session_id, run.request.runtime_session_id)
    if (!runtimeSessionID) return []
    const existingEvents = await engine.eventStore.replayRun(run.runtime_run_id)
    const awaitingApproval = asRecord(run.result.awaiting_approval)
    const approvalID = firstString(awaitingApproval.approval_id, awaitingApproval.request_id)
    if (approvalID && !existingEvents.some((event) => event.type === 'permission.resolved' && firstString(asRecord(event).request_id) === approvalID)) {
      await engine.eventStore.append({
        type: 'permission.resolved',
        session_id: runtimeSessionID,
        runtime_run_id: run.runtime_run_id,
        event_id: '',
        seq: 0,
        timestamp,
        request_id: approvalID,
        result: 'rejected'
      })
    }
    const harnessResult = asRecord(run.result.harness_result)
    const assistantMessageID = firstString(awaitingApproval.assistant_message_id, harnessResult.assistant_message_id, 'assistant:cancelled')
    await engine.eventStore.append({
      type: 'session.step.failed',
      session_id: runtimeSessionID,
      runtime_run_id: run.runtime_run_id,
      event_id: '',
      seq: 0,
      timestamp,
      assistant_message_id: assistantMessageID,
      error: { type: 'cancelled', message: reason }
    })
    await engine.eventStore.append({
      type: 'session.error',
      session_id: runtimeSessionID,
      runtime_run_id: run.runtime_run_id,
      event_id: '',
      seq: 0,
      timestamp,
      level: 'info',
      code: 'user_cancelled',
      message: reason,
      retryable: false
    })
    return engine.eventStore.replayRun(run.runtime_run_id)
  }

  private async recordPiFailureEvents(runtimeSessionID: string, runtimeRunID: string, timestamp: string, descriptor: RuntimeErrorDescriptor, requestID = '') {
    if (!this.agentRuntimeEngine || !runtimeSessionID) return []
    const currentRunEvents = await this.agentRuntimeEngine.eventStore.replayRun(runtimeRunID)
    const startedStep = [...currentRunEvents]
      .reverse()
      .map(asRecord)
      .find((event) => event.type === 'session.step.started')
    const assistantMessageID = firstString(startedStep?.assistant_message_id, startedStep?.message_id, 'assistant:failed')
    const hasStepFailed = currentRunEvents
      .map(asRecord)
      .some((event) => event.type === 'session.step.failed' && firstString(event.assistant_message_id, assistantMessageID) === assistantMessageID)
    if (!hasStepFailed) {
      await this.agentRuntimeEngine.eventStore.append({
        type: 'session.step.failed',
        session_id: runtimeSessionID,
        runtime_run_id: runtimeRunID,
        event_id: '',
        seq: 0,
        timestamp,
        assistant_message_id: assistantMessageID,
        summary: descriptor.user_message,
        error: {
          type: descriptor.category,
          message: descriptor.user_message,
          code: descriptor.code,
          category: descriptor.category,
          retryable: descriptor.retryable,
          http_status: descriptor.http_status,
          source: descriptor.source,
          terminal_status: descriptor.terminal_status,
          ...(requestID ? { request_id: requestID } : {})
        }
      } as AgentRuntimeEvent)
    }
    await this.agentRuntimeEngine.eventStore.append({
      type: 'session.error',
      session_id: runtimeSessionID,
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp,
      level: 'error',
      ...descriptor,
      message: descriptor.user_message,
      summary: descriptor.user_message,
      ...(requestID ? { request_id: requestID } : {})
    } as AgentRuntimeEvent)
    return this.agentRuntimeEngine.eventStore.replayRun(runtimeRunID)
  }

  private async ensurePiRunStateEvent(run: AIRuntimeRun) {
    if (!this.agentRuntimeEngine) return []
    const runtimeSessionID = firstString(run.result.runtime_session_id, run.request.runtime_session_id)
    const eventType = runStateEventTypeForStatus(run.status)
    if (!runtimeSessionID || !eventType) return this.agentRuntimeEngine.eventStore.replayRun(run.runtime_run_id)
    const events = await this.agentRuntimeEngine.eventStore.replayRun(run.runtime_run_id)
    const projection = projectRunState(events)
    const existingTerminal = events.find((event) => isTerminalRunStateEventType(event.type))
    if (isTerminalRunStatus(run.status) && existingTerminal) {
      if (projection.hasTerminalConflict || existingTerminal.type !== eventType) {
        throw new RuntimeDomainError(
          'runtime_run_terminal_event_conflict',
          `Runtime run ${run.runtime_run_id} is ${run.status} but replay terminal state conflicts`,
          500
        )
      }
      return events
    }
    if (events.some((event) => event.type === eventType)) return events
    await this.agentRuntimeEngine.eventStore.append({
      type: eventType,
      session_id: runtimeSessionID,
      runtime_run_id: run.runtime_run_id,
      event_id: '',
      seq: 0,
      timestamp: firstString(run.finished_at, run.updated_at, now()),
      status: run.status,
      code: firstString(run.error_code) || undefined,
      message: firstString(run.error_msg) || undefined,
      request_id: firstString(run.request.request_id) || undefined,
      run: { runtime_run_id: run.runtime_run_id, status: run.status }
    } as AgentRuntimeEvent)
    this.logTerminalRun(run)
    return this.agentRuntimeEngine.eventStore.replayRun(run.runtime_run_id)
  }

  private async ensureStoredRunStateEvent(run: AIRuntimeRun): Promise<RuntimeEventRecord[]> {
    const eventType = runStateEventTypeForStatus(run.status)
    const events = await this.store.listActionEvents(run.workspace_id, run.runtime_run_id)
    if (!eventType) return events.map(storedActionEventRuntimeRecord)

    const projection = projectRunState(events.map((event) => ({ type: event.event_type })))
    const existingTerminal = events.find((event) => isTerminalRunStateEventType(event.event_type))
    if (isTerminalRunStatus(run.status) && existingTerminal) {
      if (projection.hasTerminalConflict || existingTerminal.event_type !== eventType) {
        throw new RuntimeDomainError(
          'runtime_run_terminal_event_conflict',
          `Runtime run ${run.runtime_run_id} is ${run.status} but replay terminal state conflicts`,
          500
        )
      }
      return events.map(storedActionEventRuntimeRecord)
    }
    if (events.some((event) => event.event_type === eventType)) {
      return events.map(storedActionEventRuntimeRecord)
    }

    const eventSeq = await this.store.nextRunEventSeq(run.runtime_run_id)
    const eventID = eventBusinessID(run.runtime_run_id, eventSeq)
    const payload = {
      event_id: eventID,
      event_seq: eventSeq,
      runtime_run_id: run.runtime_run_id,
      session_id: run.session_id,
      status: run.status,
      code: firstString(run.error_code) || undefined,
      message: firstString(run.error_msg) || undefined,
      run: { runtime_run_id: run.runtime_run_id, status: run.status }
    }
    const stored = await this.store.appendActionEvent({
      event_id: eventID,
      event_seq: eventSeq,
      workspace_id: run.workspace_id,
      session_id: run.session_id,
      runtime_run_id: run.runtime_run_id,
      event_type: eventType,
      visibility: 'public',
      payload_json: payload,
      display_json: eventDisplayJSON(eventType, payload),
      created_at: firstString(run.finished_at, run.updated_at, now())
    })
    this.logTerminalRun(run)
    return [...events, stored].map(storedActionEventRuntimeRecord)
  }

  private async ensureRunStateEvent(run: AIRuntimeRun) {
    if (firstString(run.result.runtime_engine, run.request.runtime_engine) === 'pi') {
      return this.ensurePiRunStateEvent(run)
    }
    return this.ensureStoredRunStateEvent(run)
  }


  private async abortPiHarnessRun(run: AIRuntimeRun) {
    if (!this.agentRuntimeEngine || firstString(run.result.runtime_engine, run.request.runtime_engine) !== 'pi') return false
    if (typeof this.agentRuntimeEngine.runner.abort !== 'function') return false
    return this.agentRuntimeEngine.runner.abort(run.runtime_run_id)
  }

  continueSessionStream(actor: RuntimeActor, sessionID: number, payload: Record<string, unknown>, auth?: RuntimeAuth): AsyncIterable<RuntimeStreamEvent> {
    const content = firstString(payload.content, '继续')
    return this.streamEntryRun(actor, sessionID, {
      ...payload,
      content,
      client_entry_id: payload.client_entry_id
    }, auth)
  }

  private shouldUsePiRuntime(payload: Record<string, unknown>) {
    const requestedEngine = firstString(payload.runtime_engine)
    if (requestedEngine) return requestedEngine === 'pi'
    return Boolean(this.agentRuntimeEngine)
  }

  async *continueAfterActionStream(
    actor: RuntimeActor,
    action: AgentAction,
    decision: UserActionDecision,
    auth?: RuntimeAuth,
    sessionGrant?: SessionPermissionGrant
  ): AsyncIterable<RuntimeStreamEvent> {
    const startedAt = performance.now()
    const session = await this.store.getSession(actor.workspace_id, action.session_id)
    if (!session || session.status !== 'active') {
      throw new RuntimeDomainError('ai_session_not_found', 'AI session not found', 404)
    }
    const run = await this.store.getRun(actor.workspace_id, action.runtime_run_id)
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    await this.assertContinuationRunProfileIntegrity(actor, run)
    const runningRun = await this.store.saveRun({
      ...run,
      status: 'running',
      finished_at: undefined,
      updated_at: now()
    })
    const profileSnapshot = await this.executionProfileForContinuation(actor, runningRun)
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const seqBase = entries.length
    const timestamp = now()
    const existingEvents = Array.isArray(runningRun.result.runtime_events)
      ? runningRun.result.runtime_events.map((item): RuntimeEventRecord => {
        const record = asRecord(item)
        return {
          type: firstString(record.type, record.event, 'runtime.event'),
          payload: asRecord(record.payload || record.data)
        }
      })
      : []
    const runtimeEvents: RuntimeEventRecord[] = [...existingEvents]
    const emitRuntimeEvents = async (events: RuntimeEventRecord[]) => {
      const persisted = await this.persistRuntimeEvents(actor, session, action.runtime_run_id, events)
      runtimeEvents.push(...persisted)
      return persisted.map((runtimeEvent) => this.streamEvent(runtimeEvent.type, startedAt, runtimeEvent.payload))
    }
    const call = runtimeToolCallFromAction(action)
    const approved = isApprovingDecision(decision)
    const actionLabel = firstString(actionDisplay(action).title, actionDisplay(action).name, actionToolName(action), action.action_kind)
    const decisionContent = `${approved ? '已确认' : decision === 'steer' ? '已调整' : '已拒绝'} ${actionLabel}`
    const actionEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: runningRun.output_entry_id,
      seq: seqBase + 1,
      entry_type: 'run_event',
      role: 'user',
      status: 'completed',
      content: decisionContent,
      content_blocks: [{ type: 'text', text: decisionContent }],
      input: {
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        action_display: action.display_json,
        decision
      },
      output: {
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        decision
      },
      runtime_run_id: runningRun.runtime_run_id,
      event_seq: Number(runningRun.result.event_seq || 1) + 1,
      idempotency_key: runtimeIdempotencyKey('entry', runningRun.runtime_run_id, action.action_id, decision, 'decision'),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(actionEntry)
    yield this.streamEvent('action_entry', startedAt, { entry: actionEntry })

    const decisionEvent: RuntimeEventRecord = {
      type: 'action.decision',
      payload: {
        event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'decision'),
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        decision,
        action: publicAction(action),
        summary: decisionContent
      }
    }
    for (const item of await emitRuntimeEvents([decisionEvent])) yield item
    if (sessionGrant) {
      for (const item of await emitRuntimeEvents([{
        type: 'approval.session_granted',
        payload: {
          event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'approval', 'session_granted'),
          grant_id: sessionGrant.grant_id,
          action_id: action.action_id,
          action_internal_id: action.id,
          permission_key: sessionGrant.permission_key,
          tool_name: sessionGrant.tool_name,
          executor_type: sessionGrant.executor_type,
          resource_type: sessionGrant.resource_type,
          resource_id: sessionGrant.resource_id,
          granted_by: sessionGrant.granted_by,
          created_at: sessionGrant.created_at,
          expires_at: sessionGrant.expires_at
        }
      }])) yield item
    }

    let continuedAction = action
    let toolContent = ''
    let toolOutput: Record<string, unknown> = {}
    let toolStatus: AISessionEntry['status'] = 'completed'
    let toolResultEvent: RuntimeEventRecord
    if (approved) {
      const claim = await this.store.claimApprovedActionExecution({
        workspace_id: actor.workspace_id,
        action_id: action.id,
        input_digest: action.input_digest,
        claimed_at: timestamp
      })
      if (claim.outcome !== 'claimed') {
        throw actionExecutionClaimError(claim.outcome)
      }
      continuedAction = claim.action
      let execution = claim.execution
      for (const item of await emitRuntimeEvents([{
        type: 'action.execution_started',
        payload: {
          action_id: action.action_id,
          action_internal_id: action.id,
          execution_id: execution.execution_id,
          attempt: execution.attempt,
          action: publicAction(continuedAction),
          status: execution.status
        }
      }])) yield item
      try {
        const result = await this.toolExecutor.execute(call, {
          actor,
          session,
          runtime_run_id: action.runtime_run_id,
          action: continuedAction,
          auth
        })
        toolContent = result.content
        toolOutput = {
          structured_content: result.structured_content || {},
          metadata: result.metadata || {}
        }
        execution = await this.store.saveActionExecution({
          ...execution,
          status: 'succeeded',
          external_request_id: firstString(result.metadata?.request_id, result.metadata?.mcp_request_id),
          result_json: result.structured_content || {},
          finished_at: now(),
          updated_at: now()
        })
        continuedAction = await this.store.saveAction({
          ...continuedAction,
          status: 'executed',
          executed_at: timestamp,
          result_json: {
            structured_content: result.structured_content || {},
            metadata: result.metadata || {},
            external_request_id: firstString(result.metadata?.request_id, result.metadata?.mcp_request_id)
          },
          updated_at: timestamp
        })
        toolResultEvent = {
          type: 'tool.executed',
          payload: {
            event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'tool', 'executed'),
            action_id: action.action_id,
            action_internal_id: action.id,
            action: publicAction(continuedAction),
            provider_tool_call_id: call.tool_call_id,
            result: actionResult(continuedAction),
            content: toolContent,
            metadata: result.metadata || {}
          }
        }
        for (const item of await emitRuntimeEvents([
          toolResultEvent,
          {
            type: 'action.execution_succeeded',
            payload: {
              action_id: action.action_id,
              action_internal_id: action.id,
              execution_id: execution.execution_id,
              attempt: execution.attempt,
              action: publicAction(continuedAction),
              status: execution.status,
              result: execution.result_json
            }
          }
        ])) yield item
      } catch (error) {
        toolStatus = 'failed'
        toolContent = error instanceof Error ? error.message : 'Tool execution failed'
        execution = await this.store.saveActionExecution({
          ...execution,
          status: 'failed',
          error_code: 'tool_execution_failed',
          error_msg: toolContent,
          finished_at: now(),
          updated_at: now()
        })
        continuedAction = await this.store.saveAction({
          ...continuedAction,
          status: 'failed',
          error_msg: toolContent,
          updated_at: timestamp
        })
        toolResultEvent = {
          type: 'tool.failed',
          payload: {
            event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'tool', 'failed'),
            action_id: action.action_id,
            action_internal_id: action.id,
            action: publicAction(continuedAction),
            provider_tool_call_id: call.tool_call_id,
            error: toolContent
          }
        }
        for (const item of await emitRuntimeEvents([
          toolResultEvent,
          {
            type: 'action.execution_failed',
            payload: {
              action_id: action.action_id,
              action_internal_id: action.id,
              execution_id: execution.execution_id,
              attempt: execution.attempt,
              action: publicAction(continuedAction),
              status: execution.status,
              error: toolContent
            }
          }
        ])) yield item
      }
    } else {
      toolStatus = 'cancelled'
      toolContent = `action_rejected: ${actionLabel}`
      toolOutput = { rejected: true }
      toolResultEvent = {
        type: 'tool.rejected',
        payload: {
          event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'tool', 'rejected'),
          action_id: action.action_id,
          action_internal_id: action.id,
          action: publicAction(action)
        }
      }
      for (const item of await emitRuntimeEvents([toolResultEvent])) yield item
    }

    const toolEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: actionEntry.id,
      seq: seqBase + 2,
      entry_type: 'tool_result',
      role: 'tool',
      status: toolStatus,
      content: toolContent,
      content_blocks: [{ type: 'text', text: toolContent }],
      input: {
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        action_display: action.display_json,
        decision
      },
      output: toolOutput,
      runtime_run_id: runningRun.runtime_run_id,
      event_seq: Number(actionEntry.event_seq || 1) + 1,
      idempotency_key: runtimeIdempotencyKey('entry', runningRun.runtime_run_id, action.action_id, decision, 'tool'),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(toolEntry)
    yield this.streamEvent('tool_entry', startedAt, { entry: toolEntry })

    const continuationContent = approved
      ? `action_result:\n${JSON.stringify({ action_id: action.action_id, action_kind: action.action_kind, result: toolOutput, content: toolContent })}`
      : `action_rejected:\n${JSON.stringify({
          action_id: action.action_id,
          action_kind: action.action_kind,
          reason: decision === 'steer' ? firstString(action.result_json.steer_text, 'User steered the action') : 'User rejected the action',
          decision
        })}`
    const continuationRequest: ChatModelRequest = {
      profile: profileSnapshot,
      session,
      history: [...entries, toolEntry],
      content: continuationContent,
      request_id: firstString(runningRun.request.request_id),
      runtime_run_id: runningRun.runtime_run_id,
      parent_runtime_run_id: firstString(runningRun.request.parent_runtime_run_id),
      context_ref: {},
      include_current_page: false,
      tools: [],
      capabilities: asRecord(runningRun.result.capabilities),
      loaded_skills: Array.isArray(runningRun.result.loaded_skills) ? runningRun.result.loaded_skills.map((item) => asRecord(item)) : [],
      subagent_results: Array.isArray(runningRun.result.subagent_results) ? runningRun.result.subagent_results.map((item) => asRecord(item)) : [],
      output_schema: asRecord(profileSnapshot.output_schema),
      developer_instructions: 'Continue the prior EasyDo assistant answer after the user action decision. Incorporate the action result or rejection explicitly.'
    }
    const continuationOptions: ModelCallEventOptions = { phase: 'action_continuation', round: 1 }
    for (const item of await emitRuntimeEvents([
      {
        type: 'tool.result_prepared',
        payload: {
          provider_tool_call_id: call.tool_call_id,
          tool_name: call.tool_name,
          status: approved && toolStatus !== 'failed' ? 'completed' : 'failed',
          truncated: false,
          content_chars: toolContent.length,
          artifact_refs: [],
          has_structured_content: Object.keys(toolOutput).length > 0,
          action_id: action.action_id,
          action_internal_id: action.id
        }
      },
      {
        type: 'model.request_prepared',
        payload: modelRequestEventPayload(continuationRequest, continuationOptions)
      },
      {
        type: 'model.tool_result_submitted',
        payload: {
          ...modelRequestEventPayload(continuationRequest, continuationOptions),
          tool_result_count: 1,
          decision
        }
      },
      {
        type: 'model.continuation_started',
        payload: {
          ...modelRequestEventPayload(continuationRequest, continuationOptions),
          tool_result_count: 1,
          decision
        }
      },
      {
        type: 'model.call_started',
        payload: modelRequestEventPayload(continuationRequest, continuationOptions)
      }
    ])) yield item

    let completion: ChatModelResult | null = null
    let assistantContent = ''
    let reasoning = ''
    let continuationErrorCode = ''
    let continuationErrorMessage = ''
    let answerStreamMode: 'undecided' | 'streaming' | 'buffering_json' = 'undecided'
    const streamToolCalls: ChatModelToolCall[] = []
    const bufferedAnswerDeltas: RuntimeEventRecord[] = []
    let providerRawRefs = Array.isArray(runningRun.result.provider_raw_refs)
      ? runningRun.result.provider_raw_refs.map((item) => asRecord(item))
      : []
    try {
      for await (const event of this.streamModel(continuationRequest)) {
        if (event.type === 'reasoning_delta') {
          reasoning += event.delta
          for (const item of await emitRuntimeEvents([{
            type: 'reasoning_delta',
            payload: {
              delta: event.delta,
              elapsed_ms: elapsedMs(startedAt)
            }
          }])) yield item
        }
        if (event.type === 'tool_call') {
          streamToolCalls.push(event.tool_call)
        }
        if (event.type === 'answer_delta') {
          assistantContent += event.delta
          if (answerStreamMode === 'undecided') {
            const trimmed = assistantContent.trimStart()
            if (trimmed) {
              answerStreamMode = trimmed.startsWith('{') || trimmed.startsWith('[') || trimmed.startsWith('```')
                ? 'buffering_json'
                : 'streaming'
            }
          }
          if (answerStreamMode === 'streaming') {
            bufferedAnswerDeltas.push({
              type: 'answer_delta',
              payload: {
                delta: event.delta,
                elapsed_ms: elapsedMs(startedAt)
              }
            })
          }
        }
      }
      completion = {
        text: assistantContent,
        tool_calls: streamToolCalls,
        usage: {},
        raw: {}
      }
      for (const item of await emitRuntimeEvents([{
        type: 'model.call_completed',
        payload: {
          phase: continuationOptions.phase,
          round: continuationOptions.round,
          status: assistantContent.trim() || streamToolCalls.length > 0 ? 'completed' : 'empty',
          text_chars: assistantContent.length,
          tool_call_count: streamToolCalls.length,
          reasoning_chars: reasoning.length
        }
      }])) yield item
      if (!assistantContent.trim() && streamToolCalls.length === 0) {
        const fallbackStream = this.streamBoundedProviderContinuation(continuationRequest, { phase: 'fallback', round: 2 })
        while (true) {
          const next = await fallbackStream.next()
          if (next.done) {
            completion = next.value
            break
          }
          for (const item of await emitRuntimeEvents([next.value])) yield item
        }
        assistantContent = completion?.text || ''
      }
      const fallbackSummary = actionResultSummary(continuedAction, call.tool_name, toolStatus, toolContent, toolOutput)
      const preparedAnswer = prepareActionContinuationAnswer(
        assistantContent,
        completion,
        fallbackSummary,
        true,
        elapsedMs(startedAt)
      )
      assistantContent = preparedAnswer.assistantContent
      completion = preparedAnswer.completion
      const answerEvents = preparedAnswer.events.length > 0
        ? preparedAnswer.events
        : bufferedAnswerDeltas.length > 0
          ? bufferedAnswerDeltas
          : assistantContent
            ? [{
          type: 'answer_delta',
          payload: {
            delta: assistantContent,
            elapsed_ms: elapsedMs(startedAt)
          }
              }]
            : []
      if (answerEvents.length > 0) {
        for (const item of await emitRuntimeEvents(answerEvents)) yield item
      }
      for (const item of await emitRuntimeEvents([{
        type: 'model.continuation_completed',
        payload: {
          ...modelCompletionEventPayload(completion, continuationOptions),
          tool_result_count: 1,
          decision
        }
      }])) yield item
      const providerRaw = await this.persistProviderRawFromCompletion(actor, session, runningRun.runtime_run_id, completion, providerRawRefs.length + 1)
      providerRawRefs = [...providerRawRefs, ...providerRaw.refs]
      if (providerRaw.events.length > 0) {
        for (const item of await emitRuntimeEvents(providerRaw.events)) yield item
      }
    } catch (error) {
      continuationErrorCode = error instanceof ModelProviderError ? error.code : 'model_provider_error'
      continuationErrorMessage = error instanceof Error ? error.message : 'Model provider failed'
      assistantContent = `模型调用失败：${continuationErrorMessage}`
      for (const item of await emitRuntimeEvents([{
        type: 'model_provider.failed',
        payload: {
          code: continuationErrorCode,
          message: continuationErrorMessage
        }
      }])) yield item
      for (const item of await emitRuntimeEvents([{
        type: 'answer_delta',
        payload: {
          delta: assistantContent,
          elapsed_ms: elapsedMs(startedAt)
        }
      }])) yield item
    }
    const outputValidation = continuationErrorCode
      ? { valid: false, errors: [continuationErrorMessage || continuationErrorCode], structuredOutput: undefined }
      : this.validateOutput(profileSnapshot, assistantContent)
    for (const item of await emitRuntimeEvents([{
      type: outputValidation.valid ? 'output_schema.validated' : 'output_schema.invalid',
      payload: {
        valid: outputValidation.valid,
        errors: outputValidation.errors,
        has_schema: hasSchema(asRecord(profileSnapshot.output_schema))
      }
    }])) yield item

    const continuationRunStatus = outputValidation.valid && continuedAction.status !== 'failed' ? 'completed' : 'failed'
    for (const item of await emitRuntimeEvents([{
      type: runStateEventTypeForStatus(continuationRunStatus),
      payload: runStateEventPayload(
        runningRun.runtime_run_id,
        continuationRunStatus,
        continuationRunStatus === 'failed' ? continuationErrorCode || 'output_schema_invalid' : '',
        continuationRunStatus === 'failed' ? continuationErrorMessage || outputValidation.errors.join('; ') : ''
      )
    }])) yield item

    const previousAgentActions = Array.isArray(runningRun.result.agent_actions)
      ? runningRun.result.agent_actions.map((item) => asRecord(item))
      : []
    const nextAgentActions = previousAgentActions.map((item) =>
      firstString(item.action_id, item.id) === continuedAction.action_id ? publicAction(continuedAction) : item
    )
    const assistantEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: toolEntry.id,
      seq: seqBase + 3,
      entry_type: outputValidation.valid ? 'message' : 'error',
      role: 'assistant',
      status: outputValidation.valid ? 'completed' : 'failed',
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: {
        continuation_for_action_id: action.action_id,
        continuation_for_action_internal_id: action.id
      },
      output: {
        text: assistantContent,
        reasoning,
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: runtimeEvents,
        capabilities: asRecord(runningRun.result.capabilities),
        loaded_skills: Array.isArray(runningRun.result.loaded_skills) ? runningRun.result.loaded_skills : [],
        subagent_results: Array.isArray(runningRun.result.subagent_results) ? runningRun.result.subagent_results : [],
        agent_actions: nextAgentActions,
        tool_results: [{
          tool_call_id: call.tool_call_id,
          tool_name: call.tool_name,
          status: toolStatus === 'completed' ? 'completed' : 'failed',
          content: toolContent,
          structured_content: toolOutput,
          metadata: {},
          truncated: false,
          artifact_refs: []
        }],
        provider_raw_refs: providerRawRefs
      },
      runtime_run_id: runningRun.runtime_run_id,
      event_seq: toolEntry.event_seq ? toolEntry.event_seq + 1 : undefined,
      idempotency_key: runtimeIdempotencyKey('entry', runningRun.runtime_run_id, action.action_id, decision, 'assistant'),
      created_at: timestamp,
      updated_at: now()
    }
    await this.store.saveEntry(assistantEntry)
    const completedRun: AIRuntimeRun = {
      ...runningRun,
      status: continuationRunStatus,
      output_entry_id: assistantEntry.id,
      result: {
        ...runningRun.result,
        text: assistantContent,
        reasoning,
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: runtimeEvents,
        action_result: actionResult(continuedAction),
        agent_actions: nextAgentActions,
        provider_raw_refs: providerRawRefs
      },
      usage: completion?.usage || runningRun.usage,
      error_code: outputValidation.valid ? undefined : continuationErrorCode || 'output_schema_invalid',
      error_msg: outputValidation.valid ? undefined : continuationErrorMessage || outputValidation.errors.join('; '),
      finished_at: now(),
      updated_at: now()
    }
    const savedCompletedRun = await this.store.saveRun(completedRun)
    await this.saveSessionProgress(actor, session, {
      entry_count: seqBase + 3,
      last_entry_at: assistantEntry.created_at
    })
    yield this.streamEvent('assistant_entry', startedAt, { entry: assistantEntry, run: savedCompletedRun })
    yield this.streamEvent('done', startedAt, { action: publicAction(continuedAction), timings: { total_ms: elapsedMs(startedAt) } })
  }

  async continueAfterAction(actor: RuntimeActor, action: AgentAction, decision: UserActionDecision, auth?: RuntimeAuth, sessionGrant?: SessionPermissionGrant) {
    const session = await this.store.getSession(actor.workspace_id, action.session_id)
    if (!session || session.status !== 'active') {
      throw new RuntimeDomainError('ai_session_not_found', 'AI session not found', 404)
    }
    const run = await this.store.getRun(actor.workspace_id, action.runtime_run_id)
    if (!run) {
      throw new RuntimeDomainError('runtime_run_not_found', 'Runtime run not found', 404)
    }
    await this.assertContinuationRunProfileIntegrity(actor, run)
    const runningRun = await this.store.saveRun({
      ...run,
      status: 'running',
      finished_at: undefined,
      updated_at: now()
    })
    const profileSnapshot = await this.executionProfileForContinuation(actor, runningRun)
    const entries = await this.store.listEntries(actor.workspace_id, session.id)
    const seqBase = entries.length
    const timestamp = now()
    const existingEvents = Array.isArray(runningRun.result.runtime_events)
      ? runningRun.result.runtime_events.map((item): RuntimeEventRecord => {
        const record = asRecord(item)
        return {
          type: firstString(record.type, 'runtime.event'),
          payload: asRecord(record.payload)
        }
      })
      : []
    const call = runtimeToolCallFromAction(action)
    const approved = isApprovingDecision(decision)
    const actionLabel = firstString(actionDisplay(action).title, actionDisplay(action).name, actionToolName(action), action.action_kind)
    const decisionContent = `${approved ? '已确认' : decision === 'steer' ? '已调整' : '已拒绝'} ${actionLabel}`
    const actionEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: runningRun.output_entry_id,
      seq: seqBase + 1,
      entry_type: 'run_event',
      role: 'user',
      status: 'completed',
      content: decisionContent,
      content_blocks: [{ type: 'text', text: decisionContent }],
      input: {
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        action_display: action.display_json,
        decision
      },
      output: {
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        decision
      },
      runtime_run_id: runningRun.runtime_run_id,
      event_seq: Number(runningRun.result.event_seq || 1) + 1,
      idempotency_key: runtimeIdempotencyKey('entry', runningRun.runtime_run_id, action.action_id, decision, 'decision'),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(actionEntry)

    let continuedAction = action
    let toolContent = ''
    let toolOutput: Record<string, unknown> = {}
    let toolStatus: AISessionEntry['status'] = 'completed'
    let event: RuntimeEventRecord
    const executionEvents: RuntimeEventRecord[] = []
    if (approved) {
      const claim = await this.store.claimApprovedActionExecution({
        workspace_id: actor.workspace_id,
        action_id: action.id,
        input_digest: action.input_digest,
        claimed_at: timestamp
      })
      if (claim.outcome !== 'claimed') {
        throw actionExecutionClaimError(claim.outcome)
      }
      continuedAction = claim.action
      let execution = claim.execution
      executionEvents.push({
        type: 'action.execution_started',
        payload: {
          action_id: action.action_id,
          action_internal_id: action.id,
          execution_id: execution.execution_id,
          attempt: execution.attempt,
          action: publicAction(action),
          status: execution.status
        }
      })
      try {
        const result = await this.toolExecutor.execute(call, {
          actor,
          session,
          runtime_run_id: action.runtime_run_id,
          action: continuedAction,
          auth
        })
        toolContent = result.content
        toolOutput = {
          structured_content: result.structured_content || {},
          metadata: result.metadata || {}
        }
        execution = await this.store.saveActionExecution({
          ...execution,
          status: 'succeeded',
          external_request_id: firstString(result.metadata?.request_id, result.metadata?.mcp_request_id),
          result_json: result.structured_content || {},
          finished_at: now(),
          updated_at: now()
        })
        continuedAction = await this.store.saveAction({
          ...continuedAction,
          status: 'executed',
          executed_at: timestamp,
          result_json: {
            structured_content: result.structured_content || {},
            metadata: result.metadata || {},
            external_request_id: firstString(result.metadata?.request_id, result.metadata?.mcp_request_id)
          },
          updated_at: timestamp
        })
        event = {
          type: 'tool.executed',
          payload: {
            event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'tool', 'executed'),
            action_id: action.action_id,
            action_internal_id: action.id,
            action: publicAction(continuedAction),
            result: actionResult(continuedAction)
          }
        }
        executionEvents.push({
          type: 'action.execution_succeeded',
          payload: {
            action_id: action.action_id,
            action_internal_id: action.id,
            execution_id: execution.execution_id,
            attempt: execution.attempt,
            action: publicAction(continuedAction),
            status: execution.status,
            result: execution.result_json
          }
        })
      } catch (error) {
        toolStatus = 'failed'
        toolContent = error instanceof Error ? error.message : 'Tool execution failed'
        execution = await this.store.saveActionExecution({
          ...execution,
          status: 'failed',
          error_code: 'tool_execution_failed',
          error_msg: toolContent,
          finished_at: now(),
          updated_at: now()
        })
        continuedAction = await this.store.saveAction({
          ...continuedAction,
          status: 'failed',
          error_msg: toolContent,
          updated_at: timestamp
        })
        event = {
          type: 'tool.failed',
          payload: {
            event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'tool', 'failed'),
            action_id: action.action_id,
            action_internal_id: action.id,
            action: publicAction(continuedAction),
            error: toolContent
          }
        }
        executionEvents.push({
          type: 'action.execution_failed',
          payload: {
            action_id: action.action_id,
            action_internal_id: action.id,
            execution_id: execution.execution_id,
            attempt: execution.attempt,
            action: publicAction(continuedAction),
            status: execution.status,
            error: toolContent
          }
        })
      }
    } else {
      toolStatus = 'cancelled'
      toolContent = `action_rejected: ${actionLabel}`
      toolOutput = { rejected: true }
      event = {
        type: 'tool.rejected',
        payload: {
          event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'tool', 'rejected'),
          action_id: action.action_id,
          action_internal_id: action.id,
          action: publicAction(action)
        }
      }
    }

    const toolEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: actionEntry.id,
      seq: seqBase + 2,
      entry_type: 'tool_result',
      role: 'tool',
      status: toolStatus,
      content: toolContent,
      content_blocks: [{ type: 'text', text: toolContent }],
      input: {
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        action_display: action.display_json,
        decision
      },
      output: toolOutput,
      runtime_run_id: runningRun.runtime_run_id,
      event_seq: Number(actionEntry.event_seq || 1) + 1,
      idempotency_key: runtimeIdempotencyKey('entry', runningRun.runtime_run_id, action.action_id, decision, 'tool'),
      created_at: timestamp,
      updated_at: timestamp
    }
    await this.store.saveEntry(toolEntry)

    const continuationContent = approved
      ? `action_result:\n${JSON.stringify({ action_id: action.action_id, action_kind: action.action_kind, result: toolOutput, content: toolContent })}`
      : `action_rejected:\n${JSON.stringify({
          action_id: action.action_id,
          action_kind: action.action_kind,
          reason: decision === 'steer' ? firstString(action.result_json.steer_text, 'User steered the action') : 'User rejected the action',
          decision
        })}`
    let completion: ChatModelResult | null = null
    let assistantContent = ''
    let continuationErrorCode = ''
    let continuationErrorMessage = ''
    let providerContinuationEvents: RuntimeEventRecord[] = []
    let providerRawRefs = Array.isArray(runningRun.result.provider_raw_refs)
      ? runningRun.result.provider_raw_refs.map((item) => asRecord(item))
      : []
    try {
      const continuationRequest: ChatModelRequest = {
        profile: profileSnapshot,
        session,
        history: [...entries, toolEntry],
        content: continuationContent,
        request_id: firstString(runningRun.request.request_id),
        runtime_run_id: runningRun.runtime_run_id,
        parent_runtime_run_id: firstString(runningRun.request.parent_runtime_run_id),
        context_ref: {},
        include_current_page: false,
        tools: [],
        capabilities: asRecord(runningRun.result.capabilities),
        loaded_skills: Array.isArray(runningRun.result.loaded_skills) ? runningRun.result.loaded_skills.map((item) => asRecord(item)) : [],
        subagent_results: Array.isArray(runningRun.result.subagent_results) ? runningRun.result.subagent_results.map((item) => asRecord(item)) : [],
        output_schema: asRecord(profileSnapshot.output_schema),
        developer_instructions: 'Continue the prior EasyDo assistant answer after the user action decision. Incorporate the action result or rejection explicitly.'
      }
      const continuationOptions: ModelCallEventOptions = { phase: 'action_continuation', round: 1 }
      providerContinuationEvents = [
        {
          type: 'tool.result_prepared',
          payload: {
            provider_tool_call_id: call.tool_call_id,
            tool_name: call.tool_name,
            status: approved && toolStatus !== 'failed' ? 'completed' : 'failed',
            truncated: false,
            content_chars: toolContent.length,
            artifact_refs: [],
            has_structured_content: Object.keys(toolOutput).length > 0,
            action_id: action.action_id,
            action_internal_id: action.id
          }
        },
        {
          type: 'model.request_prepared',
          payload: modelRequestEventPayload(continuationRequest, continuationOptions)
        },
        {
          type: 'model.tool_result_submitted',
          payload: {
            ...modelRequestEventPayload(continuationRequest, continuationOptions),
            tool_result_count: 1,
            decision
          }
        },
        {
          type: 'model.continuation_started',
          payload: {
            ...modelRequestEventPayload(continuationRequest, continuationOptions),
            tool_result_count: 1,
            decision
          }
        }
      ]
      const continuationTurn = await this.completeWithBoundedContinuation(continuationRequest, continuationOptions)
      completion = continuationTurn.completion
      providerContinuationEvents = [
        ...providerContinuationEvents,
        ...continuationTurn.events,
        {
          type: 'model.continuation_completed',
          payload: {
            ...modelCompletionEventPayload(completion, continuationOptions),
            tool_result_count: 1,
            decision
          }
        }
      ]
      const providerRaw = await this.persistProviderRawFromCompletion(actor, session, runningRun.runtime_run_id, completion, providerRawRefs.length + 1)
      providerRawRefs = [...providerRawRefs, ...providerRaw.refs]
      providerContinuationEvents = [...providerContinuationEvents, ...providerRaw.events]
      assistantContent = completion?.text || ''
      const fallbackSummary = actionResultSummary(continuedAction, call.tool_name, toolStatus, toolContent, toolOutput)
      const preparedAnswer = prepareActionContinuationAnswer(assistantContent, completion, fallbackSummary, true)
      assistantContent = preparedAnswer.assistantContent
      completion = preparedAnswer.completion
      providerContinuationEvents.push(...preparedAnswer.events)
    } catch (error) {
      continuationErrorCode = error instanceof ModelProviderError ? error.code : 'model_provider_error'
      continuationErrorMessage = error instanceof Error ? error.message : 'Model provider failed'
      assistantContent = `模型调用失败：${continuationErrorMessage}`
    }
    const outputValidation = continuationErrorCode
      ? { valid: false, errors: [continuationErrorMessage || continuationErrorCode], structuredOutput: undefined }
      : this.validateOutput(profileSnapshot, assistantContent)
    const decisionEvent = {
      type: 'action.decision',
      payload: {
        event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'decision'),
        action_id: action.action_id,
        action_internal_id: action.id,
        action_kind: action.action_kind,
        decision
      }
    }
    const sessionGrantEvent = sessionGrant
      ? [{
          type: 'approval.session_granted',
          payload: {
            event_id: stableEventID(action.runtime_run_id, 'action', action.id, 'approval', 'session_granted'),
            grant_id: sessionGrant.grant_id,
            action_id: action.action_id,
            action_internal_id: action.id,
            permission_key: sessionGrant.permission_key,
            tool_name: sessionGrant.tool_name,
            executor_type: sessionGrant.executor_type,
            resource_type: sessionGrant.resource_type,
            resource_id: sessionGrant.resource_id,
            granted_by: sessionGrant.granted_by,
            created_at: sessionGrant.created_at,
            expires_at: sessionGrant.expires_at
          }
        }]
      : []
    const continuationRunStatus = outputValidation.valid && continuedAction.status !== 'failed' ? 'completed' : 'failed'
    const continuationEvents = [
      decisionEvent,
      ...sessionGrantEvent,
      ...executionEvents,
      event,
      ...providerContinuationEvents,
      ...(continuationErrorCode
        ? [{
            type: 'model_provider.failed',
            payload: {
              code: continuationErrorCode,
              message: continuationErrorMessage
            }
          }]
        : []),
      {
        type: outputValidation.valid ? 'output_schema.validated' : 'output_schema.invalid',
        payload: {
          valid: outputValidation.valid,
          errors: outputValidation.errors,
          has_schema: hasSchema(asRecord(profileSnapshot.output_schema))
        }
      },
      {
        type: runStateEventTypeForStatus(continuationRunStatus),
        payload: runStateEventPayload(
          runningRun.runtime_run_id,
          continuationRunStatus,
          continuationRunStatus === 'failed' ? continuationErrorCode || 'output_schema_invalid' : '',
          continuationRunStatus === 'failed' ? continuationErrorMessage || outputValidation.errors.join('; ') : ''
        )
      }
    ]
    const persistedContinuationEvents = await this.persistRuntimeEvents(actor, session, action.runtime_run_id, continuationEvents)
    const runtimeEvents = [
      ...existingEvents,
      ...persistedContinuationEvents
    ]
    const assistantEntry: AISessionEntry = {
      id: await this.store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: toolEntry.id,
      seq: seqBase + 3,
      entry_type: outputValidation.valid ? 'message' : 'error',
      role: 'assistant',
      status: outputValidation.valid ? 'completed' : 'failed',
      content: assistantContent,
      content_blocks: [{ type: 'text', text: assistantContent }],
      input: {
        continuation_for_action_id: action.action_id,
        continuation_for_action_internal_id: action.id
      },
      output: {
        text: assistantContent,
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: runtimeEvents,
        provider_raw_refs: providerRawRefs
      },
      runtime_run_id: runningRun.runtime_run_id,
      event_seq: toolEntry.event_seq ? toolEntry.event_seq + 1 : undefined,
      idempotency_key: runtimeIdempotencyKey('entry', runningRun.runtime_run_id, action.action_id, decision, 'assistant'),
      created_at: timestamp,
      updated_at: now()
    }
    await this.store.saveEntry(assistantEntry)
    const completedRun: AIRuntimeRun = {
      ...runningRun,
      status: continuationRunStatus,
      output_entry_id: assistantEntry.id,
      result: {
        ...runningRun.result,
        text: assistantContent,
        structured_output: outputValidation.structuredOutput || {},
        output_schema_valid: outputValidation.valid,
        output_schema_errors: outputValidation.errors,
        runtime_events: runtimeEvents,
        action_result: actionResult(continuedAction),
        provider_raw_refs: providerRawRefs
      },
      usage: completion?.usage || runningRun.usage,
      error_code: outputValidation.valid ? undefined : continuationErrorCode || 'output_schema_invalid',
      error_msg: outputValidation.valid ? undefined : continuationErrorMessage || outputValidation.errors.join('; '),
      finished_at: now(),
      updated_at: now()
    }
    const savedCompletedRun = await this.store.saveRun(completedRun)
    await this.saveSessionProgress(actor, session, {
      entry_count: seqBase + 3,
      last_entry_at: assistantEntry.created_at
    })
    return {
      action: continuedAction,
      action_entry: actionEntry,
      tool_entry: toolEntry,
      assistant_entry: assistantEntry,
      run: savedCompletedRun
    }
  }

  private async *streamModel(request: Parameters<NonNullable<ChatModelClient['stream']>>[0]): AsyncIterable<ChatModelStreamEvent> {
    if (this.chatModelClient.stream) {
      yield* this.chatModelClient.stream(request)
      return
    }
    const completion = await this.chatModelClient.complete(request)
    for (const toolCall of completion?.tool_calls || []) {
      yield { type: 'tool_call', tool_call: toolCall, raw: completion?.raw }
    }
    if (completion?.text) {
      yield { type: 'answer_delta', delta: completion.text, raw: completion.raw }
    }
  }

  private streamEvent(event: string, start: number, data: Record<string, unknown>): RuntimeStreamEvent {
    return {
      event,
      data: {
        ...data,
        elapsed_ms: data.elapsed_ms === undefined ? elapsedMs(start) : data.elapsed_ms
      }
    }
  }

  private async persistStepEvent(
    actor: RuntimeActor,
    session: AISession,
    runtimeRunID: string,
    stage: string,
    start: number
  ): Promise<RuntimeEventRecord> {
    const [event] = await this.persistRuntimeEvents(actor, session, runtimeRunID, [{
      type: 'run.step_announced',
      payload: {
        stage,
        elapsed_ms: elapsedMs(start)
      }
    }])
    return event
  }

  private async resolveProfileSecrets(workspaceID: number, profile: AgentProfileSnapshot): Promise<AgentProfileSnapshot> {
    const credentialRef = asRecord(profile.provider_credential_ref)
    const existingSecretRef = asRecord(credentialRef.secret_ref)
    if (
      firstString(
        credentialRef.api_key,
        credentialRef.token,
        credentialRef.bearer_token,
        credentialRef.env,
        credentialRef.env_var,
        existingSecretRef.api_key,
        existingSecretRef.token,
        existingSecretRef.bearer_token,
        existingSecretRef.env,
        existingSecretRef.env_var
      )
    ) {
      return profile
    }

    const credentialID = firstString(
      credentialRef.credential_id,
      credentialRef.resource_id,
      credentialRef.resource_key,
      credentialRef.id
    )
    if (!credentialID) return profile

    const provider = asRecord(profile.provider)
    const binding = asRecord(profile.binding)
    const providerID = firstString(
      provider.provider_id,
      provider.id,
      binding.provider_id
    )
    const resolvedProviderCredential = await this.store.resolveProviderCredentialRef(workspaceID, providerID, credentialID)
    if (!resolvedProviderCredential) return profile

    return {
      ...profile,
      provider_credential_ref: {
        ...credentialRef,
        ...resolvedProviderCredential
      }
    }
  }
}

function piSessionHistoryFromEntries(entries: AISessionEntry[], excludedRuntimeRunID = ''): AgentHarnessRunInput['history'] {
  return entries
    .filter((entry) => ['user', 'assistant'].includes(entry.role))
    .filter((entry) => !excludedRuntimeRunID || entry.runtime_run_id !== excludedRuntimeRunID)
    .filter((entry) => entry.role === 'user' || !['streaming', 'running', 'queued'].includes(entry.status))
    .map((entry) => ({
      role: entry.role as 'user' | 'assistant',
      content: firstString(entry.content, entry.output.text),
      timestamp: canonicalEntryTimestamp(entry),
      status: entry.status
    }))
    .filter((entry) => Boolean(entry.content))
}

function canonicalEntryTimestamp(entry: AISessionEntry) {
  const timestamp = Date.parse(firstString(entry.created_at, entry.updated_at))
  return Number.isFinite(timestamp) ? timestamp : 0
}


export class ActionService {
  private readonly actionDecisionLocks = new Map<string, Promise<void>>()

  constructor(
    private readonly store: RuntimeStore,
    private readonly sessionService?: SessionService
  ) {}

  async decide(actor: RuntimeActor, actionID: string, decision: UserActionDecision, payload: Record<string, unknown> = {}) {
    return this.withActionDecisionLock(actor, actionID, async () => {
      const action = await this.getAction(actor, actionID)
      if (action.status !== 'awaiting_decision') {
        this.rejectAlreadyDecidedAction(action)
      }
      const decisionRecord = await this.recordActionDecision(actor, action, decision, payload)
      const sessionGrant = decision === 'approve_session'
        ? await this.createSessionPermissionGrant(actor, action)
        : undefined
      return this.applyAwaitingActionDecision(actor, action, decisionRecord, sessionGrant)
    })
  }

  async decideAndContinue(
    actor: RuntimeActor,
    actionID: string,
    decision: UserActionDecision,
    payload: Record<string, unknown> = {},
    auth?: RuntimeAuth
  ) {
    return this.withActionDecisionLock(actor, actionID, async () => {
      if (!this.sessionService) {
        throw new RuntimeDomainError('action_continuation_unavailable', 'Action continuation is unavailable', 500)
      }
      const action = await this.getAction(actor, actionID)
      if (action.status !== 'awaiting_decision') {
        this.rejectAlreadyDecidedAction(action)
      }
      await this.sessionService.assertContinuationRunProfileIntegrity(actor, action.runtime_run_id)
      const decisionRecord = await this.recordActionDecision(actor, action, decision, payload)
      const sessionGrant = decision === 'approve_session'
        ? await this.createSessionPermissionGrant(actor, action)
        : undefined
      const updated = await this.applyAwaitingActionDecision(actor, action, decisionRecord, sessionGrant)
      return this.sessionService.continueAfterAction(actor, updated, decision, auth, sessionGrant)
    })
  }

  async *decideAndContinueStream(
    actor: RuntimeActor,
    actionID: string,
    decision: UserActionDecision,
    payload: Record<string, unknown> = {},
    auth?: RuntimeAuth
  ): AsyncIterable<RuntimeStreamEvent> {
    const key = `${actor.workspace_id}:${actionID}`
    const previous = this.actionDecisionLocks.get(key) || Promise.resolve()
    let releaseCurrent: () => void = () => {}
    const current = new Promise<void>((resolve) => {
      releaseCurrent = resolve
    })
    const queued = previous.catch(() => undefined).then(() => current)
    this.actionDecisionLocks.set(key, queued)
    await previous.catch(() => undefined)
    try {
      if (!this.sessionService) {
        throw new RuntimeDomainError('action_continuation_unavailable', 'Action continuation is unavailable', 500)
      }
      const action = await this.getAction(actor, actionID)
      if (action.status !== 'awaiting_decision') {
        this.rejectAlreadyDecidedAction(action)
      }
      await this.sessionService.assertContinuationRunProfileIntegrity(actor, action.runtime_run_id)
      const decisionRecord = await this.recordActionDecision(actor, action, decision, payload)
      const sessionGrant = decision === 'approve_session'
        ? await this.createSessionPermissionGrant(actor, action)
        : undefined
      const updated = await this.applyAwaitingActionDecision(actor, action, decisionRecord, sessionGrant)
      yield* this.sessionService.continueAfterActionStream(actor, updated, decision, auth, sessionGrant)
    } finally {
      releaseCurrent()
      if (this.actionDecisionLocks.get(key) === queued) {
        this.actionDecisionLocks.delete(key)
      }
    }
  }

  private async withActionDecisionLock<T>(actor: RuntimeActor, actionID: string, operation: () => Promise<T>) {
    const key = `${actor.workspace_id}:${actionID}`
    const previous = this.actionDecisionLocks.get(key) || Promise.resolve()
    let releaseCurrent: () => void = () => {}
    const current = new Promise<void>((resolve) => {
      releaseCurrent = resolve
    })
    const queued = previous.catch(() => undefined).then(() => current)
    this.actionDecisionLocks.set(key, queued)
    await previous.catch(() => undefined)
    try {
      return await operation()
    } finally {
      releaseCurrent()
      if (this.actionDecisionLocks.get(key) === queued) {
        this.actionDecisionLocks.delete(key)
      }
    }
  }

  private async getAction(actor: RuntimeActor, actionID: string) {
    const action = await this.store.getActionByBusinessID(actor.workspace_id, actionID)
    if (!action) {
      throw new RuntimeDomainError('action_not_found', 'Action not found', 404)
    }
    return action
  }

  private async recordActionDecision(
    actor: RuntimeActor,
    action: AgentAction,
    decision: UserActionDecision,
    payload: Record<string, unknown>
  ) {
    const clientDecisionID = requireClientID('client_decision_id', payload.client_decision_id)
    const existing = (await this.store.listActionDecisions(actor.workspace_id, action.id))
      .find((item) => item.client_decision_id === clientDecisionID && item.actor_user_id === actor.user_id)
    if (existing) {
      if (existing.decision !== decision || canonicalJSON(existing.input_patch_json) !== canonicalJSON(asRecord(payload.input_patch_json ?? payload.input_patch))) {
        throw new RuntimeDomainError('action_decision_conflict', 'Action decision payload conflicts with existing client decision', 409)
      }
      return existing
    }
    const decisionSeq = await this.store.nextActionDecisionSeq(action.id)
    const actionID = actionBusinessID(action)
    const timestamp = now()
    const record: ActionDecision = {
      decision_id: decisionBusinessID(action, decisionSeq),
      decision_seq: decisionSeq,
      workspace_id: actor.workspace_id,
      session_id: action.session_id,
      runtime_run_id: action.runtime_run_id,
      action_id: action.id,
      decision_type: 'user',
      decision,
      actor_user_id: actor.user_id,
      reason: firstString(payload.reason),
      input_patch_json: asRecord(payload.input_patch_json ?? payload.input_patch),
      client_decision_id: clientDecisionID,
      idempotency_key: runtimeIdempotencyKey('decision', actionID, `user${actor.user_id}`, 'client', clientDecisionID),
      created_at: timestamp
    }
    return record
  }

  private async applyAwaitingActionDecision(
    actor: RuntimeActor,
    action: AgentAction,
    decision: ActionDecision,
    sessionGrant?: SessionPermissionGrant
  ) {
    const result = await this.store.decideAwaitingAction({
      workspace_id: actor.workspace_id,
      action_id: action.id,
      decision,
      session_grant: sessionGrant,
      decided_at: now()
    })
    if (result.outcome === 'decided') return result.action
    if (result.outcome === 'already_decided') this.rejectAlreadyDecidedAction(result.action)
    throw new RuntimeDomainError('action_not_found', 'Action not found', 404)
  }

  private async createSessionPermissionGrant(actor: RuntimeActor, action: AgentAction): Promise<SessionPermissionGrant> {
    const decision = actionPolicy(action)
    const target = actionTarget(action)
    const grantSeq = await this.store.nextSessionPermissionGrantSeq(action.session_id)
    const createdAt = now()
    const expiresAt = new Date(Date.now() + SESSION_PERMISSION_GRANT_TTL_MS).toISOString()
    return {
      grant_id: sessionPermissionGrantBusinessID(actor.workspace_id, action.session_id, grantSeq),
      grant_seq: grantSeq,
      workspace_id: actor.workspace_id,
      session_id: action.session_id,
      runtime_run_id: action.runtime_run_id,
      action_id: action.id,
      permission_key: firstString(decision.permission_key, `tool:${action.capability_id}:${firstString(decision.operation_type, 'read')}`),
      tool_name: actionToolName(action),
      executor_type: executorTypeForActionKind(action.action_kind),
      resource_type: firstString(target.target_type, target.type),
      resource_id: firstString(target.target_id, target.id),
      status: 'active',
      granted_by: actor.user_id,
      created_at: createdAt,
      expires_at: expiresAt
    }
  }

  private rejectAlreadyDecidedAction(action: AgentAction): never {
    const existingDecision = this.decisionForStatus(action.status)
    throw new RuntimeDomainError(
      'action_already_decided',
      `Action already ${isApprovingDecision(existingDecision) ? 'approved' : 'rejected'}`,
      409
    )
  }

  private decisionForStatus(status: AgentAction['status']): UserActionDecision {
    if (status === 'rejected' || status === 'cancelled' || status === 'expired') return 'reject'
    return 'approve_once'
  }
}
