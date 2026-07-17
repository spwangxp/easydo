import type { SessionQueueItem } from '../domain/runtime.js'

export type AgentRuntimeRole = 'user' | 'assistant' | 'system'

export interface ModelRef {
  provider_id: string
  id: string
  variant?: string
}

export interface FileRef {
  path: string
  mime?: string
}

export interface ContentPart {
  type: string
  text?: string
  uri?: string
  mime?: string
}

export interface TokenUsage {
  input: number
  output: number
  reasoning: number
  cache: {
    read: number
    write: number
  }
}

export interface RuntimeErrorInfo {
  type: string
  message: string
  code?: string
  category?: string
  retryable?: boolean
  http_status?: number
  source?: string
  terminal_status?: string
}

export interface RuntimeEventBase {
  type: string
  session_id: string
  runtime_run_id?: string
  event_id: string
  seq: number
  timestamp: string
  request_id?: string
}

export interface PromptedEvent extends RuntimeEventBase {
  type: 'session.prompted'
  message_id: string
  prompt: string
  files?: FileRef[]
  delivery: string
}

export interface StepStartedEvent extends RuntimeEventBase {
  type: 'session.step.started'
  assistant_message_id: string
  parent_message_id?: string
  agent?: string
  model?: ModelRef
  snapshot?: string
}

export interface StepEndedEvent extends RuntimeEventBase {
  type: 'session.step.ended'
  assistant_message_id: string
  finish_reason?: string
  tokens?: TokenUsage
  cost?: number
  files?: string[]
}

export interface StepFailedEvent extends RuntimeEventBase {
  type: 'session.step.failed'
  assistant_message_id: string
  error: RuntimeErrorInfo
}

export interface TextStartedEvent extends RuntimeEventBase {
  type: 'session.text.started'
  assistant_message_id: string
  text_id: string
}

export interface TextDeltaEvent extends RuntimeEventBase {
  type: 'session.text.delta'
  assistant_message_id: string
  text_id: string
  delta: string
}

export interface TextEndedEvent extends RuntimeEventBase {
  type: 'session.text.ended'
  assistant_message_id: string
  text_id: string
  text: string
}

export interface ReasoningStartedEvent extends RuntimeEventBase {
  type: 'session.reasoning.started'
  assistant_message_id: string
  reasoning_id: string
}

export interface ReasoningDeltaEvent extends RuntimeEventBase {
  type: 'session.reasoning.delta'
  assistant_message_id: string
  reasoning_id: string
  delta: string
}

export interface ReasoningEndedEvent extends RuntimeEventBase {
  type: 'session.reasoning.ended'
  assistant_message_id: string
  reasoning_id: string
  text: string
}

export interface ToolInputStartedEvent extends RuntimeEventBase {
  type: 'session.tool.input.started'
  assistant_message_id: string
  call_id: string
  tool_name: string
}

export interface ToolInputDeltaEvent extends RuntimeEventBase {
  type: 'session.tool.input.delta'
  assistant_message_id: string
  call_id: string
  tool_name: string
  delta: string
}

export interface ToolInputEndedEvent extends RuntimeEventBase {
  type: 'session.tool.input.ended'
  assistant_message_id: string
  call_id: string
  text: string
}

export interface ToolCalledEvent extends RuntimeEventBase {
  type: 'session.tool.called'
  assistant_message_id: string
  call_id: string
  tool: string
  input: Record<string, unknown>
}

export interface ToolProgressEvent extends RuntimeEventBase {
  type: 'session.tool.progress'
  assistant_message_id: string
  call_id: string
  content?: ContentPart[]
  structured?: Record<string, unknown>
}

export interface ToolSuccessEvent extends RuntimeEventBase {
  type: 'session.tool.success'
  assistant_message_id: string
  call_id: string
  content: ContentPart[]
  structured?: Record<string, unknown>
  output_paths?: string[]
  result?: unknown
}

export interface ToolFailedEvent extends RuntimeEventBase {
  type: 'session.tool.failed'
  assistant_message_id: string
  call_id: string
  error: RuntimeErrorInfo
  result?: unknown
}

export interface PermissionAskedEvent extends RuntimeEventBase {
  type: 'permission.asked'
  request_id: string
  call_id?: string
  tool_name: string
  input: Record<string, unknown>
  reason?: string
  message: string
}

export interface PermissionEvaluatedEvent extends RuntimeEventBase {
  type: 'action.permission_evaluated'
  call_id: string
  tool_name: string
  decision: 'allow' | 'ask' | 'deny'
  matched_rule: string
  scope: string
  operation_type: string
  permission_key?: string
  code?: string
  reason: string
}

export interface PermissionResolvedEvent extends RuntimeEventBase {
  type: 'permission.resolved'
  request_id: string
  result: 'approved' | 'rejected' | 'steer' | string
}

export interface ContextBudgetSnapshot {
  provider_id?: string
  model_key?: string
  capability_source?: string
  capability_snapshot_hash?: string
  context_window_tokens?: number
  usable_context_tokens?: number
  current_context_tokens?: number
  threshold_tokens?: number
  before_tokens?: number
  after_tokens?: number
  compacted_message_count?: number
  retained_message_count?: number
  compacted_tokens?: number
  retained_tokens?: number
}

export interface ContextBudgetEvaluatedEvent extends RuntimeEventBase, ContextBudgetSnapshot {
  type: 'context.budget.evaluated'
  message_id: string
  should_compact: boolean
  reason: 'within_budget' | 'over_threshold' | 'history_message_cap' | string
}

export interface CompactionStartedEvent extends RuntimeEventBase, ContextBudgetSnapshot {
  type: 'session.compaction.started' | 'context.compaction.started'
  message_id: string
  reason: 'auto' | 'manual' | 'over_threshold' | 'history_message_cap' | string
}

export interface CompactionEndedEvent extends RuntimeEventBase, ContextBudgetSnapshot {
  type: 'session.compaction.ended' | 'context.compaction.completed' | 'context.compaction.failed'
  message_id: string
  reason: 'auto' | 'manual' | 'over_threshold' | 'history_message_cap' | string
  summary: string
  recent: string
  error?: string
}

export interface ModelSwitchedEvent extends RuntimeEventBase {
  type: 'session.model.switched'
  message_id: string
  model: ModelRef
}

export interface AgentSwitchedEvent extends RuntimeEventBase {
  type: 'session.agent.switched'
  message_id: string
  agent: string
}

export interface SessionErrorEvent extends RuntimeEventBase {
  type: 'session.error'
  level: string
  code: string
  message: string
  user_message?: string
  category?: string
  retryable: boolean
  http_status?: number
  source?: string
  terminal_status?: string
}

export interface RunStateEvent extends RuntimeEventBase {
  type: 'run.completed' | 'run.failed' | 'run.cancelled' | 'run.timeout' | 'run.awaiting_decision' | 'run.interrupted'
  status: 'completed' | 'failed' | 'cancelled' | 'timeout' | 'awaiting_decision' | 'interrupted'
  code?: string
  message?: string
  run: {
    runtime_run_id: string
    status: string
  }
}

export interface SessionQueueAddedEvent extends RuntimeEventBase {
  type: 'session.queue.added'
  queue_item: SessionQueueItem
}

export interface SessionQueueClaimedEvent extends RuntimeEventBase {
  type: 'session.queue.claimed'
  queue_item: SessionQueueItem
}

export interface SessionQueueCancelledEvent extends RuntimeEventBase {
  type: 'session.queue.cancelled'
  queue_item: SessionQueueItem
}

export interface SessionQueueExpiredEvent extends RuntimeEventBase {
  type: 'session.queue.expired'
  queue_item: SessionQueueItem
}

export interface SessionQueueFailedEvent extends RuntimeEventBase {
  type: 'session.queue.failed'
  queue_item: SessionQueueItem
}

export interface SessionSteerAppliedEvent extends RuntimeEventBase {
  type: 'session.steer.applied'
  runtime_run_id: string
  queue_item: SessionQueueItem
}

export interface SessionFollowUpStartedEvent extends RuntimeEventBase {
  type: 'session.follow_up.started'
  runtime_run_id: string
  consumed_runtime_run_id: string
  queue_item: SessionQueueItem
}

export type SessionQueueRuntimeEvent =
  | SessionQueueAddedEvent
  | SessionQueueClaimedEvent
  | SessionQueueCancelledEvent
  | SessionQueueExpiredEvent
  | SessionQueueFailedEvent
  | SessionSteerAppliedEvent
  | SessionFollowUpStartedEvent

export type AgentRuntimeEvent =
  | PromptedEvent
  | StepStartedEvent
  | StepEndedEvent
  | StepFailedEvent
  | TextStartedEvent
  | TextDeltaEvent
  | TextEndedEvent
  | ReasoningStartedEvent
  | ReasoningDeltaEvent
  | ReasoningEndedEvent
  | ToolInputStartedEvent
  | ToolInputDeltaEvent
  | ToolInputEndedEvent
  | ToolCalledEvent
  | ToolProgressEvent
  | ToolSuccessEvent
  | ToolFailedEvent
  | PermissionEvaluatedEvent
  | PermissionAskedEvent
  | PermissionResolvedEvent
  | ContextBudgetEvaluatedEvent
  | CompactionStartedEvent
  | CompactionEndedEvent
  | ModelSwitchedEvent
  | AgentSwitchedEvent
  | SessionErrorEvent
  | RunStateEvent
  | SessionQueueRuntimeEvent

export type AgentRuntimeEventType = AgentRuntimeEvent['type']

export type RunStateEventType = RunStateEvent['type']
export type ProjectedRunStatus = RunStateEvent['status']

const RUN_STATE_BY_EVENT: Readonly<Record<RunStateEventType, ProjectedRunStatus>> = {
  'run.completed': 'completed',
  'run.failed': 'failed',
  'run.cancelled': 'cancelled',
  'run.timeout': 'timeout',
  'run.awaiting_decision': 'awaiting_decision',
  'run.interrupted': 'interrupted'
}

const TERMINAL_RUN_STATE_EVENTS = new Set<RunStateEventType>([
  'run.completed',
  'run.failed',
  'run.cancelled',
  'run.timeout',
  'run.interrupted'
])

export function isRunStateEventType(type: string): type is RunStateEventType {
  return Object.prototype.hasOwnProperty.call(RUN_STATE_BY_EVENT, type)
}

export function isTerminalRunStateEventType(type: string): type is RunStateEventType {
  return isRunStateEventType(type) && TERMINAL_RUN_STATE_EVENTS.has(type)
}

export function runStateEventTypeForStatus(status: string): RunStateEventType | '' {
  const match = Object.entries(RUN_STATE_BY_EVENT)
    .find(([, projectedStatus]) => projectedStatus === status)
  return match?.[0] as RunStateEventType | undefined || ''
}

export function projectRunState(events: ReadonlyArray<{ type: string }>) {
  const stateEvents = events.filter((event) => isRunStateEventType(event.type))
  const terminalEvents = stateEvents.filter((event) => isTerminalRunStateEventType(event.type))
  const latest = stateEvents.at(-1)
  return {
    status: latest && isRunStateEventType(latest.type) ? RUN_STATE_BY_EVENT[latest.type] : undefined,
    terminalEventCount: terminalEvents.length,
    hasTerminalConflict: new Set(terminalEvents.map((event) => event.type)).size > 1
  }
}

export function isDurableRuntimeEvent(type: AgentRuntimeEventType): boolean {
  return Boolean(type)
}
