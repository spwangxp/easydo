import { createHash } from 'node:crypto'

export type JSONPrimitive = string | number | boolean | null
export type JSONValue = JSONPrimitive | JSONValue[] | { [key: string]: JSONValue }
export type JSONObject = { [key: string]: JSONValue }

export interface RuntimeActor {
  user_id: number
  username: string
  system_role: string
  workspace_id: number
  workspace_role: string
  auth_session_id: string
}

export interface RuntimeAuth {
  delegated_user_token?: string
  user_token?: string
  server_internal_token: string
}

export interface RuntimeEnvelope<TPayload extends Record<string, unknown> = Record<string, unknown>> {
  request_id: string
  idempotency_key: string
  actor: RuntimeActor
  auth: RuntimeAuth
  payload: TPayload
}

export interface AgentResourceRef {
  resource_type: string
  resource_id: string | number
  resource_version_id?: number
  resource_version?: string
  snapshot_digest?: string
  config?: Record<string, unknown>
  required?: boolean
}

export type AgentResourceKind = 'skill' | 'mcp_server'

export interface AgentResource {
  id: number
  workspace_id: number
  resource_kind: AgentResourceKind
  resource_key: string
  resource_id: string
  name: string
  description: string
  version: string
  status: 'draft' | 'active' | 'disabled' | 'archived'
  spec: Record<string, unknown>
  endpoint: Record<string, unknown>
  secret_ref: Record<string, unknown>
  tags: string[]
  created_by: number
  created_at: string
  updated_at: string
  resource_version_id?: number
  snapshot_digest?: string
}

export type AgentResourceSnapshot = Omit<AgentResource, 'created_at' | 'updated_at'>

export interface AgentResourceVersion {
  resource_version_id: number
  resource_id: number
  workspace_id: number
  revision: number
  source_version: string
  snapshot_digest: string
  snapshot: AgentResourceSnapshot
  created_by: number
  created_at: string
}

export function stableJSONStringify(value: unknown): string {
  if (value === undefined) return 'null'
  if (value === null || typeof value !== 'object') return JSON.stringify(value)
  if (Array.isArray(value)) return `[${value.map((item) => stableJSONStringify(item)).join(',')}]`
  const record = value as Record<string, unknown>
  return `{${Object.keys(record)
    .filter((key) => record[key] !== undefined)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableJSONStringify(record[key])}`)
    .join(',')}}`
}

function isSensitiveSnapshotField(key: string) {
  return /^(authorization|proxy-authorization|cookie|set-cookie|token|secret)$/i.test(key) ||
    /(api[_-]?key|access[_-]?token|bearer[_-]?token|password|private[_-]?key|client[_-]?secret|auth[_-]?token|[_-]token|[_-]secret)/i.test(key)
}

function isReferenceIdentityField(key: string) {
  return key === 'id' || key === 'reference' || key === 'ref' || /_(id|ref)$/.test(key)
}

function sanitizeSecretReference(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  const source = value as Record<string, unknown>
  const sanitized: Record<string, unknown> = {}
  for (const [key, item] of Object.entries(source)) {
    if ((key === 'configured' || key === 'masked') && typeof item === 'boolean') {
      sanitized[key] = item
      continue
    }
    if (isReferenceIdentityField(key) && (typeof item === 'string' || typeof item === 'number')) {
      sanitized[key] = item
    }
  }
  if (Object.keys(source).length > 0) {
    sanitized.configured = true
    sanitized.masked = true
  }
  return sanitized
}

function sanitizeUnresolvedSnapshotValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map((item) => sanitizeUnresolvedSnapshotValue(item))
  if (!value || typeof value !== 'object') return value
  const source = value as Record<string, unknown>
  const sanitized: Record<string, unknown> = {}
  for (const [key, item] of Object.entries(source)) {
    if (key === 'secret_ref') {
      sanitized[key] = sanitizeSecretReference(item)
    } else if (isSensitiveSnapshotField(key)) {
      sanitized[key] = '[REDACTED]'
    } else {
      sanitized[key] = sanitizeUnresolvedSnapshotValue(item)
    }
  }
  return sanitized
}

export function unresolvedAgentResourceSnapshot(resource: AgentResource): AgentResourceSnapshot {
  const { created_at: _createdAt, updated_at: _updatedAt, ...snapshot } = resource
  return sanitizeUnresolvedSnapshotValue(snapshot) as AgentResourceSnapshot
}

export function agentResourceSnapshotDigest(snapshot: AgentResourceSnapshot) {
  return `sha256:${createHash('sha256').update(stableJSONStringify(snapshot)).digest('hex')}`
}

export function createAgentResourceVersion(params: {
  resourceVersionID: number
  revision: number
  resource: AgentResource
  createdBy?: number
  createdAt?: string
}): AgentResourceVersion {
  const snapshot = unresolvedAgentResourceSnapshot(params.resource)
  return {
    resource_version_id: params.resourceVersionID,
    resource_id: params.resource.id,
    workspace_id: params.resource.workspace_id,
    revision: params.revision,
    source_version: params.resource.version,
    snapshot_digest: agentResourceSnapshotDigest(snapshot),
    snapshot,
    created_by: params.createdBy ?? params.resource.created_by,
    created_at: params.createdAt || new Date().toISOString()
  }
}

export function assertAgentResourceVersionMatchesResource(resource: AgentResource, version: AgentResourceVersion) {
  const snapshot = unresolvedAgentResourceSnapshot(resource)
  const digest = agentResourceSnapshotDigest(snapshot)
  if (
    version.resource_id !== resource.id ||
    version.workspace_id !== resource.workspace_id ||
    version.source_version !== resource.version ||
    version.snapshot_digest !== digest ||
    agentResourceSnapshotDigest(version.snapshot) !== digest
  ) {
    throw new Error('Agent resource revision does not match the resource head snapshot')
  }
}

export function assertAgentResourceVersionSnapshotSafe(version: AgentResourceVersion) {
  const sanitized = sanitizeUnresolvedSnapshotValue(version.snapshot) as AgentResourceSnapshot
  const digest = agentResourceSnapshotDigest(sanitized)
  if (stableJSONStringify(sanitized) !== stableJSONStringify(version.snapshot) || version.snapshot_digest !== digest) {
    throw new Error('Agent resource revision snapshot must be unresolved, secret-safe, and digest verified')
  }
}

export interface AgentProfileDraft {
  id: number
  workspace_id: number
  name: string
  description: string
  profile_kind: string
  context_tags: string[]
  provider: Record<string, unknown>
  binding: Record<string, unknown>
  model: Record<string, unknown>
  provider_credential_ref: Record<string, unknown>
  inference: Record<string, unknown>
  prompt: Record<string, unknown>
  skills: AgentResourceRef[]
  subagents: AgentResourceRef[]
  mcp_servers: AgentResourceRef[]
  context_contract: Record<string, unknown>
  input_schema: Record<string, unknown>
  output_schema: Record<string, unknown>
  tool_policy: Record<string, unknown>
  memory_policy: Record<string, unknown>
  confirmation_policy: Record<string, unknown>
  response_mode: 'text' | 'json' | 'schema' | 'mixed'
  status: 'draft' | 'active' | 'disabled' | 'archived'
  created_by: number
  created_at: string
  updated_at: string
}

export interface AgentProfileVersion {
  profile_version_id: number
  profile_id: number
  workspace_id: number
  version: number
  snapshot_hash: string
  snapshot: AgentProfileSnapshot
  status: 'published' | 'deprecated' | 'archived'
  published_by: number
  published_at: string
  change_summary: string
}

export interface FrozenSubagentProfile {
  ref: AgentResourceRef
  profile_version_id: number
  snapshot_hash: string
  snapshot: AgentProfileSnapshot
}

export interface FrozenProfileResources {
  skills: AgentResource[]
  mcp_servers: AgentResource[]
  subagents: FrozenSubagentProfile[]
}

export type AgentProfileSnapshot = Omit<AgentProfileDraft, 'created_at' | 'updated_at'> & {
  frozen_resources?: FrozenProfileResources
}

export function agentProfileSnapshotHash(snapshot: AgentProfileSnapshot) {
  return `sha256:${createHash('sha256').update(stableJSONStringify(snapshot)).digest('hex')}`
}

export interface ProfileValidationIssue {
  code: string
  message: string
  path?: string
}

export interface ResourceHealthResult {
  resource_type: string
  resource_id: string | number
  status: 'healthy' | 'warning' | 'failed'
  message: string
}

export interface ProfileValidationResult {
  status: 'passed' | 'failed'
  errors: ProfileValidationIssue[]
  warnings: ProfileValidationIssue[]
  resource_health: ResourceHealthResult[]
}

export interface AISession {
  id: number
  workspace_id: number
  context_tags: string[]
  session_kind: 'chat' | 'task_run' | 'workflow' | 'test_run'
  user_id: number
  auth_session_id: string
  business_type: string
  business_id: string
  status: 'active' | 'archived' | 'deleted'
  agent_profile_id: number
  agent_profile_version_id: number
  agent_profile_version_key?: string
  agent_profile_snapshot_hash: string
  agent_workspace_runtime_id?: string
  agent_workspace_snapshot_hash?: string
  session_key?: string
  first_session_timestamp?: string
  source?: string
  model_override?: SessionModelOverride
  title: string
  title_source?: 'fallback' | 'generated' | 'manual' | 'user_fallback'
  title_generated_at?: string
  title_generation_error?: string
  entry_count: number
  last_entry_at?: string
  created_at: string
  updated_at: string
}

export interface SessionModelOverride {
  provider?: Record<string, unknown>
  binding?: Record<string, unknown>
  model?: Record<string, unknown>
  provider_credential_ref?: Record<string, unknown>
  inference?: Record<string, unknown>
}

export interface AISessionEntry {
  id: number
  session_id: number
  workspace_id: number
  user_id: number
  parent_entry_id?: number
  seq: number
  entry_type: 'message' | 'tool_call' | 'tool_result' | 'run_event' | 'summary' | 'clear_marker' | 'branch_marker' | 'error'
  role: 'user' | 'assistant' | 'tool' | 'system' | 'custom'
  status: 'queued' | 'running' | 'streaming' | 'completed' | 'failed' | 'cancelled'
  content: string
  content_blocks: Record<string, unknown>[]
  input: Record<string, unknown>
  output: Record<string, unknown>
  runtime_run_id?: string
  event_seq?: number
  idempotency_key: string
  created_at: string
  updated_at: string
}

export type SessionQueueMode = 'steer' | 'follow_up' | 'stop_and_run'

export type SessionQueueItemStatus =
  | 'pending'
  | 'claimed'
  | 'applied'
  | 'consumed'
  | 'cancelled'
  | 'expired'
  | 'failed'

export interface SessionQueueItem {
  queue_item_id: string
  workspace_id: number
  session_id: number
  item_seq: number
  position: number
  mode: SessionQueueMode
  content: string
  attachments: Record<string, unknown>[]
  context_ref: Record<string, unknown>
  target_run_id?: string
  status: SessionQueueItemStatus
  client_item_id: string
  claimed_by?: string
  claim_epoch: number
  claim_expires_at?: string
  expires_at?: string
  consumed_runtime_run_id?: string
  error_code?: string
  error_msg?: string
  created_by: number
  created_at: string
  updated_at: string
}

export const SESSION_QUEUE_ACTIVE_STATUSES: ReadonlySet<SessionQueueItemStatus> = new Set([
  'pending',
  'claimed'
])

export const SESSION_QUEUE_TERMINAL_STATUSES: ReadonlySet<SessionQueueItemStatus> = new Set([
  'applied',
  'consumed',
  'cancelled',
  'expired',
  'failed'
])

export function sessionQueueItemBusinessID(workspaceID: number, sessionID: number, itemSeq: number) {
  return `qi_w${workspaceID.toString(36)}_${sessionID.toString(36).padStart(6, '0')}_${itemSeq.toString(36).padStart(6, '0')}`
}

export function sameSessionQueueItemSet(current: string[], requested: string[]) {
  if (current.length !== requested.length || new Set(requested).size !== requested.length) return false
  const expected = new Set(current)
  return requested.every((queueItemID) => expected.has(queueItemID))
}

export interface AIRuntimeRun {
  id: number
  runtime_run_id: string
  session_id: number
  workspace_id: number
  context_tags: string[]
  agent_profile_id: number
  agent_profile_version_id: number
  agent_profile_version_key?: string
  agent_profile_snapshot_hash: string
  agent_workspace_runtime_id?: string
  agent_workspace_snapshot_hash?: string
  profile_snapshot?: AgentProfileSnapshot
  status: 'queued' | 'running' | 'awaiting_decision' | 'awaiting_input' | 'completed' | 'failed' | 'cancelled' | 'timeout' | 'interrupted'
  active_slot?: 'active'
  owner_instance_id?: string
  owner_epoch?: number
  owner_lease_expires_at?: string
  input_entry_id: number
  output_entry_id?: number
  request: Record<string, unknown>
  result: Record<string, unknown>
  usage: Record<string, unknown>
  error_code?: string
  error_msg?: string
  started_at: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export const ACTIVE_RUNTIME_RUN_STATUSES: ReadonlySet<AIRuntimeRun['status']> = new Set([
  'queued',
  'running',
  'awaiting_decision',
  'awaiting_input'
])

export function activeSlotForRunStatus(status: AIRuntimeRun['status']): AIRuntimeRun['active_slot'] | undefined {
  return ACTIVE_RUNTIME_RUN_STATUSES.has(status) ? 'active' : undefined
}

export function normalizeRuntimeRunActiveSlot(run: AIRuntimeRun): AIRuntimeRun {
  const activeSlot = activeSlotForRunStatus(run.status)
  if (activeSlot) {
    return { ...run, active_slot: activeSlot }
  }
  const { active_slot: _activeSlot, ...rest } = run
  return rest
}

export interface RuntimeOperationsSummary {
  observed_at: string
  runs: {
    active: number
    awaiting_approval: number
    awaiting_input: number
    stale: number
    orphaned: number
  }
  terminal_5m: Record<'completed' | 'failed' | 'cancelled' | 'timeout' | 'interrupted', number>
  terminal_1h: Record<'completed' | 'failed' | 'cancelled' | 'timeout' | 'interrupted', number>
  failures_1h: Array<{ category: string, code: string, count: number }>
}

export interface AgentAction {
  id: number
  action_id: string
  workspace_id: number
  context_tags: string[]
  session_id: number
  entry_id?: number
  runtime_run_id: string
  action_kind: string
  idempotency_key: string
  source: 'model' | 'policy' | 'runtime' | 'user'
  capability_id: string
  input_json: Record<string, unknown>
  input_digest: string
  target_json: Record<string, unknown>
  policy_json: Record<string, unknown>
  display_json: Record<string, unknown>
  status: 'awaiting_decision' | 'approved' | 'rejected' | 'executing' | 'executed' | 'failed' | 'expired' | 'cancelled'
  requested_by: number
  decided_by?: number
  decided_at?: string
  executed_at?: string
  result_json: Record<string, unknown>
  error_msg?: string
  created_at: string
  updated_at: string
}

export interface ActionEvent {
  event_id: string
  event_seq: number
  workspace_id: number
  session_id: number
  runtime_run_id: string
  action_id?: string
  entry_id?: number
  event_type: string
  visibility: 'public' | 'internal'
  payload_json: Record<string, unknown>
  display_json: Record<string, unknown>
  created_at: string
}

export interface ActionDecision {
  decision_id: string
  decision_seq: number
  workspace_id: number
  session_id: number
  runtime_run_id: string
  action_id: number
  decision_type: 'user' | 'policy' | 'system'
  decision: 'approve_once' | 'approve_session' | 'reject' | 'steer' | 'expired'
  actor_user_id?: number
  reason: string
  input_patch_json: Record<string, unknown>
  client_decision_id: string
  idempotency_key: string
  created_at: string
}

export interface SessionPermissionGrant {
  grant_id: string
  grant_seq: number
  workspace_id: number
  session_id: number
  runtime_run_id: string
  action_id: number
  permission_key: string
  tool_name: string
  executor_type: string
  resource_type: string
  resource_id: string
  status: 'active' | 'revoked' | 'expired'
  granted_by: number
  created_at: string
  expires_at: string
  revoked_at?: string
}

export interface ActionExecution {
  execution_id: string
  workspace_id: number
  session_id: number
  runtime_run_id: string
  action_id: number
  attempt: number
  executor_type: 'mcp' | 'pipeline' | 'subagent' | 'shell' | 'http'
  executor_ref_json: Record<string, unknown>
  idempotency_key: string
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  external_request_id?: string
  result_json: Record<string, unknown>
  error_code?: string
  error_msg?: string
  started_at: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export interface RuntimeArtifact {
  artifact_id: string
  artifact_seq: number
  workspace_id: number
  session_id: number
  runtime_run_id: string
  action_id?: string
  event_id?: string
  visibility: 'user_visible' | 'parent_visible' | 'internal_only'
  artifact_type: 'tool_input' | 'tool_output' | 'provider_raw' | 'subagent_transcript' | 'file_snippet' | 'log' | 'diagnostic'
  mime_type: string
  size_bytes: number
  storage_ref: string
  preview_json: Record<string, unknown>
  created_at: string
}

export interface ChildRunLink {
  child_run_link_id: string
  workspace_id: number
  parent_runtime_run_id: string
  parent_action_id: string
  parent_action_internal_id: number
  child_seq: number
  child_session_id: number
  child_runtime_run_id: string
  child_profile_id: number
  child_profile_version_id: number
  snapshot_digest: string
  status: 'queued' | 'running' | 'blocked_approval' | 'completed' | 'failed' | 'cancelled'
  created_at: string
  completed_at?: string
}

export type OrchestrationPhase =
  | 'assemble_context'
  | 'dispatch_tasks'
  | 'collect_status'
  | 'synthesize_results'
  | 'review_task_outputs'
  | 'dispatch_followup_context'
  | 'finalize_summary'
  | 'completed'
  | 'failed'

export type OrchestrationTaskKind = 'worker' | 'review' | 'followup' | 'synthesis'
export type OrchestrationTaskStatus =
  | 'pending'
  | 'queued'
  | 'claimed'
  | 'dispatched'
  | 'running'
  | 'blocked_approval'
  | 'completed'
  | 'failed'
  | 'cancelled'

export interface OrchestrationPlan {
  orchestration_id: string
  workspace_id: number
  parent_runtime_run_id: string
  parent_session_id: number
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  phase: OrchestrationPhase
  context_pack_digest: string
  synthesis_summary: string
  final_summary: string
  error_code?: string
  error_msg?: string
  claim_owner?: string | null
  claim_epoch: number
  claim_expires_at?: string | null
  created_at: string
  updated_at: string
  completed_at?: string
}

export interface OrchestrationContextPack {
  context_pack_id: string
  workspace_id: number
  orchestration_id: string
  parent_runtime_run_id: string
  source: string
  trim_rules: Record<string, unknown>
  resource_versions: Record<string, unknown>
  token_estimate: number
  content: Record<string, unknown>
  digest: string
  created_at: string
}

export interface OrchestrationTask {
  task_id: string
  workspace_id: number
  orchestration_id: string
  parent_runtime_run_id: string
  task_seq: number
  task_kind: OrchestrationTaskKind
  objective: string
  assigned_profile_id: number
  assigned_profile_version_id: number
  assigned_subagent_name: string
  mode: string
  dependency_ids: string[]
  context_pack_digest: string
  status: OrchestrationTaskStatus
  claim_owner?: string | null
  claim_epoch: number
  claim_expires_at?: string | null
  attempt: number
  child_run_link_id?: string
  child_runtime_run_id?: string
  result_summary: string
  artifact_refs: unknown[]
  error_code?: string
  error_msg?: string
  created_at: string
  updated_at: string
  completed_at?: string
}
