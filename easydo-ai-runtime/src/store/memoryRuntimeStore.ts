import type {
  AgentProfileDraft,
  AgentProfileVersion,
  AgentResource,
  AgentResourceVersion,
  ActionEvent,
  ActionDecision,
  ActionExecution,
  AgentAction,
  AIRuntimeRun,
  AISession,
  AISessionEntry,
  RuntimeArtifact,
  ChildRunLink,
  OrchestrationContextPack,
  OrchestrationPlan,
  OrchestrationTask,
  SessionPermissionGrant,
  SessionQueueItem,
  RuntimeOperationsSummary
} from '../domain/runtime.js'
import type { SessionFollowUpStartedEvent, SessionQueueRuntimeEvent } from '../agent-runtime/events.js'
import {
  ACTIVE_RUNTIME_RUN_STATUSES,
  agentProfileSnapshotHash,
  agentResourceSnapshotDigest,
  assertAgentResourceVersionMatchesResource,
  assertAgentResourceVersionSnapshotSafe,
  normalizeRuntimeRunActiveSlot,
  sameSessionQueueItemSet,
  sessionQueueItemBusinessID
} from '../domain/runtime.js'
import { RuntimeDomainError } from '../services/runtimeErrors.js'
import type { AIRuntimeWorkspace, AIRuntimeWorkspaceAudit } from '../workspace/types.js'

export interface RuntimeStore {
  nextProfileId(): Promise<number>
  nextProfileVersionId(): Promise<number>
  nextAgentResourceId(): Promise<number>
  nextAgentResourceVersionId(): Promise<number>
  nextSessionId(): Promise<number>
  nextEntryId(): Promise<number>
  nextRunId(): Promise<number>
  nextActionId(): Promise<number>
  nextRunEventSeq(runtimeRunID: string): Promise<number>
  nextRunArtifactSeq(runtimeRunID: string): Promise<number>
  nextActionChildSeq(actionID: number): Promise<number>
  nextActionDecisionSeq(actionID: number): Promise<number>
  nextSessionPermissionGrantSeq(sessionID: number): Promise<number>
  nextActionExecutionAttempt(actionID: number): Promise<number>
  nextAgentWorkspaceId(): Promise<number>
  saveProfile(profile: AgentProfileDraft): Promise<AgentProfileDraft>
  getProfile(workspaceID: number, profileID: number): Promise<AgentProfileDraft | undefined>
  listProfiles(workspaceID: number): Promise<AgentProfileDraft[]>
  publishProfileVersion(profile: AgentProfileDraft, version: AgentProfileVersion, expectedUpdatedAt: string): Promise<AgentProfileVersion>
  deleteProfileGuarded(workspaceID: number, profileID: number): Promise<'deleted' | 'not_found' | 'in_use'>
  saveAgentResource(resource: AgentResource): Promise<AgentResource>
  createAgentResourceWithVersion(resource: AgentResource, version: AgentResourceVersion): Promise<{ resource: AgentResource, version: AgentResourceVersion }>
  updateAgentResourceWithVersion(resource: AgentResource, version: AgentResourceVersion): Promise<{ resource: AgentResource, version: AgentResourceVersion }>
  getAgentResource(workspaceID: number, resourceID: number): Promise<AgentResource | undefined>
  listAgentResources(workspaceID: number): Promise<AgentResource[]>
  deleteAgentResource(workspaceID: number, resourceID: number): Promise<boolean>
  deleteAgentResourceGuarded(workspaceID: number, resourceID: number): Promise<'deleted' | 'not_found' | 'in_use'>
  appendAgentResourceVersion(version: AgentResourceVersion): Promise<AgentResourceVersion>
  getAgentResourceVersion(workspaceID: number, resourceVersionID: number): Promise<AgentResourceVersion | undefined>
  listAgentResourceVersions(workspaceID: number, resourceID: number): Promise<AgentResourceVersion[]>
  saveProfileVersion(version: AgentProfileVersion): Promise<AgentProfileVersion>
  listProfileVersions(workspaceID: number, profileID: number): Promise<AgentProfileVersion[]>
  getProfileVersion(workspaceID: number, profileVersionID: number): Promise<AgentProfileVersion | undefined>
  saveSession(session: AISession): Promise<AISession>
  getSession(workspaceID: number, sessionID: number): Promise<AISession | undefined>
  listSessions(workspaceID: number, filter?: SessionListFilter): Promise<AISession[]>
  findCurrentSession(key: string): Promise<AISession | undefined>
  rememberCurrentSession(key: string, sessionID: number): Promise<void>
  saveEntry(entry: AISessionEntry): Promise<AISessionEntry>
  listEntries(workspaceID: number, sessionID: number): Promise<AISessionEntry[]>
  enqueueSessionQueueItem(input: SessionQueueEnqueueInput, activeItemLimit: number): Promise<SessionQueueEnqueueResult>
  listSessionQueueItems(workspaceID: number, sessionID: number): Promise<SessionQueueItem[]>
  cancelSessionQueueItem(input: SessionQueueCancelInput): Promise<SessionQueueCancelResult>
  reorderSessionQueueItems(workspaceID: number, sessionID: number, queueItemIDs: string[], updatedAt: string): Promise<SessionQueueReorderResult>
  claimNextSessionQueueItem(input: SessionQueueClaimInput): Promise<SessionQueueClaimResult>
  applySessionQueueSteer(input: SessionQueueSteerApplyInput): Promise<SessionQueueSteerApplyResult>
  transitionSessionQueueItem(input: SessionQueueTransitionInput): Promise<SessionQueueTransitionResult>
  consumeSessionQueueItem(input: SessionQueueConsumeInput): Promise<SessionQueueConsumeResult>
  consumeSessionQueueItemWithRun(input: SessionQueueConsumeWithRunInput): Promise<SessionQueueConsumeWithRunResult>
  updateSessionQueueItem(item: SessionQueueItem): Promise<SessionQueueItem>
  saveRun(run: AIRuntimeRun): Promise<AIRuntimeRun>
  getRun(workspaceID: number, runtimeRunID: string): Promise<AIRuntimeRun | undefined>
  findActiveRun(workspaceID: number, sessionID: number): Promise<AIRuntimeRun | undefined>
  claimRunLease(workspaceID: number, runtimeRunID: string, instanceID: string, leaseExpiresAt: string, observedAt?: string): Promise<AIRuntimeRun | undefined>
  renewRunLease(workspaceID: number, runtimeRunID: string, instanceID: string, ownerEpoch: number, leaseExpiresAt: string): Promise<AIRuntimeRun | undefined>
  releaseRunLease(workspaceID: number, runtimeRunID: string, instanceID: string, ownerEpoch: number): Promise<boolean>
  interruptExpiredRunLeases(observedAt: string): Promise<AIRuntimeRun[]>
  getRuntimeOperationsSummary(observedAt: string): Promise<RuntimeOperationsSummary>
  saveAction(action: AgentAction): Promise<AgentAction>
  createToolAction(action: AgentAction): Promise<ToolActionCreateResult>
  decideAwaitingAction(input: ActionDecisionInput): Promise<ActionDecisionResult>
  claimApprovedActionExecution(input: ActionExecutionClaimInput): Promise<ActionExecutionClaimResult>
  getAction(workspaceID: number, actionID: number): Promise<AgentAction | undefined>
  getActionByBusinessID(workspaceID: number, actionBusinessID: string): Promise<AgentAction | undefined>
  appendActionEvent(event: ActionEvent): Promise<ActionEvent>
  listActionEvents(workspaceID: number, runtimeRunID: string, afterEventID?: string, limit?: number): Promise<ActionEvent[]>
  saveRuntimeArtifact(artifact: RuntimeArtifact): Promise<RuntimeArtifact>
  getRuntimeArtifact(workspaceID: number, artifactID: string): Promise<RuntimeArtifact | undefined>
  listActionArtifacts(workspaceID: number, actionID: string): Promise<RuntimeArtifact[]>
  saveChildRunLink(link: ChildRunLink): Promise<ChildRunLink>
  listChildRunLinks(workspaceID: number, parentRuntimeRunID: string): Promise<ChildRunLink[]>
  getParentRunLink(workspaceID: number, childRuntimeRunID: string): Promise<ChildRunLink | undefined>
  saveOrchestrationPlan(plan: OrchestrationPlan): Promise<OrchestrationPlan>
  getOrchestrationPlan(workspaceID: number, orchestrationID: string): Promise<OrchestrationPlan | undefined>
  getOrchestrationPlanByParentRun(workspaceID: number, parentRuntimeRunID: string): Promise<OrchestrationPlan | undefined>
  claimOrchestrationPlan(input: { workspace_id: number, orchestration_id: string, owner: string, claim_expires_at: string }): Promise<OrchestrationPlan | undefined>
  saveOrchestrationContextPack(pack: OrchestrationContextPack): Promise<OrchestrationContextPack>
  getOrchestrationContextPack(workspaceID: number, contextPackID: string): Promise<OrchestrationContextPack | undefined>
  saveOrchestrationTask(task: OrchestrationTask): Promise<OrchestrationTask>
  listOrchestrationTasks(workspaceID: number, orchestrationID: string): Promise<OrchestrationTask[]>
  getOrchestrationTask(workspaceID: number, taskID: string): Promise<OrchestrationTask | undefined>
  claimOrchestrationTask(input: { workspace_id: number, task_id: string, owner: string, claim_expires_at: string }): Promise<OrchestrationTask | undefined>
  saveActionDecision(decision: ActionDecision): Promise<ActionDecision>
  listActionDecisions(workspaceID: number, actionID: number): Promise<ActionDecision[]>
  saveSessionPermissionGrant(grant: SessionPermissionGrant): Promise<SessionPermissionGrant>
  listSessionPermissionGrants(workspaceID: number, sessionID: number): Promise<SessionPermissionGrant[]>
  saveActionExecution(execution: ActionExecution): Promise<ActionExecution>
  getActionExecution(workspaceID: number, actionID: number, attempt: number): Promise<ActionExecution | undefined>
  listActionExecutions(workspaceID: number, actionID: number): Promise<ActionExecution[]>
  saveAgentWorkspace(workspace: AIRuntimeWorkspace): Promise<AIRuntimeWorkspace>
  createOrGetDefaultAgentWorkspace(workspace: AIRuntimeWorkspace): Promise<AgentWorkspaceCreateResult>
  claimAgentWorkspaceProvision(input: AgentWorkspaceProvisionClaimInput): Promise<AIRuntimeWorkspace | undefined>
  completeAgentWorkspaceProvision(input: AgentWorkspaceProvisionCompleteInput): Promise<AIRuntimeWorkspace | undefined>
  failAgentWorkspaceProvision(input: AgentWorkspaceProvisionFailInput): Promise<AIRuntimeWorkspace | undefined>
  getAgentWorkspace(workspaceID: number, workspaceRuntimeID: string): Promise<AIRuntimeWorkspace | undefined>
  listAgentWorkspaces(workspaceID: number, ownerUserID?: number): Promise<AIRuntimeWorkspace[]>
  beginAgentWorkspaceRecycle(workspaceID: number, workspaceRuntimeID: string, ownerUserID: number, operationID: string, leaseExpiresAt: string, observedAt?: string): Promise<AIRuntimeWorkspace | undefined>
  claimAgentWorkspaceLease(workspaceID: number, workspaceRuntimeID: string, instanceID: string, leaseExpiresAt: string, observedAt?: string): Promise<AIRuntimeWorkspace | undefined>
  renewAgentWorkspaceLease(workspaceID: number, workspaceRuntimeID: string, instanceID: string, leaseEpoch: number, leaseExpiresAt: string): Promise<AIRuntimeWorkspace | undefined>
  releaseAgentWorkspaceLease(workspaceID: number, workspaceRuntimeID: string, instanceID: string, leaseEpoch: number): Promise<boolean>
  appendAgentWorkspaceAudit(audit: AIRuntimeWorkspaceAudit): Promise<AIRuntimeWorkspaceAudit>
  listAgentWorkspaceAudits(workspaceID: number, workspaceRuntimeID: string): Promise<AIRuntimeWorkspaceAudit[]>
  resolveProviderCredentialRef(workspaceID: number, providerID: string | number, credentialID: string | number): Promise<Record<string, unknown> | undefined>
}

export interface SessionListFilter {
  context_tag?: string
  context_tags?: string[]
  business_type?: string
  business_id?: string
  status?: AISession['status']
}

export interface SessionQueueEnqueueInput {
  workspace_id: number
  session_id: number
  mode: SessionQueueItem['mode']
  content: string
  attachments: Record<string, unknown>[]
  context_ref: Record<string, unknown>
  target_run_id?: string
  expires_at?: string
  client_item_id: string
  created_by: number
  created_at: string
  lifecycle_event?: SessionQueueEventDraft
}

export type SessionQueueEventDraft =
  | { type: 'session.queue.added', session_id: string, marker: 'created' }
  | { type: 'session.queue.claimed', session_id: string }
  | { type: 'session.queue.cancelled', session_id: string, marker: 'cancelled' }
  | { type: 'session.queue.expired', session_id: string, marker: 'expired' }
  | { type: 'session.queue.failed', session_id: string, marker: string }
  | { type: 'session.steer.applied', session_id: string, marker: 'applied', runtime_run_id: string }

export interface QueueTransitionCommit {
  event?: SessionQueueRuntimeEvent
  event_committed?: boolean
}

export type SessionQueueEnqueueResult =
  | ({ outcome: 'created', item: SessionQueueItem } & QueueTransitionCommit)
  | { outcome: 'existing', item: SessionQueueItem }
  | { outcome: 'capacity_exceeded' }

export interface SessionQueueCancelInput {
  workspace_id: number
  session_id: number
  queue_item_id: string
  updated_at: string
  lifecycle_event?: SessionQueueEventDraft
}

export type SessionQueueCancelResult =
  | ({ outcome: 'cancelled', item: SessionQueueItem } & QueueTransitionCommit)
  | { outcome: 'already_cancelled', item: SessionQueueItem }
  | { outcome: 'not_found' }
  | { outcome: 'not_cancellable' }

export type SessionQueueReorderResult =
  | { outcome: 'reordered', items: SessionQueueItem[] }
  | { outcome: 'conflict' }

export interface SessionQueueClaimInput {
  workspace_id: number
  session_id: number
  claimed_by: string
  claim_expires_at: string
  updated_at: string
  queue_item_id?: string
  lifecycle_event?: SessionQueueEventDraft
}

export type SessionQueueClaimResult =
  | ({ outcome: 'claimed', item: SessionQueueItem } & QueueTransitionCommit)
  | { outcome: 'empty' }

export interface SessionQueueSteerApplyInput {
  workspace_id: number
  session_id: number
  queue_item_id: string
  claimed_by: string
  claim_epoch: number
  runtime_run_id: string
  run_result: Record<string, unknown>
  updated_at: string
  lifecycle_event?: SessionQueueEventDraft
}

export type SessionQueueSteerApplyResult =
  | ({ outcome: 'applied', item: SessionQueueItem, run: AIRuntimeRun } & QueueTransitionCommit)
  | { outcome: 'already_applied', item: SessionQueueItem, run: AIRuntimeRun }
  | { outcome: 'stale_claim' }
  | { outcome: 'target_not_active' }

export interface SessionQueueTransitionInput {
  item: SessionQueueItem
  claimed_by: string
  claim_epoch: number
  lifecycle_event?: SessionQueueEventDraft
}

export type SessionQueueTransitionResult =
  | ({ outcome: 'transitioned', item: SessionQueueItem } & QueueTransitionCommit)
  | { outcome: 'stale_claim' }

function sessionQueueLifecycleEvent(draft: SessionQueueEventDraft, item: SessionQueueItem): SessionQueueRuntimeEvent {
  const marker = draft.type === 'session.queue.claimed' ? item.claim_epoch : draft.marker
  const eventBase = {
    session_id: draft.session_id,
    event_id: [draft.session_id, draft.type, item.queue_item_id, marker]
      .map((part) => encodeURIComponent(String(part).trim() || 'event'))
      .join(':'),
    seq: 0,
    timestamp: item.updated_at,
    queue_item: structuredClone(item)
  }
  switch (draft.type) {
    case 'session.queue.added':
      return { ...eventBase, type: 'session.queue.added' }
    case 'session.queue.claimed':
      return { ...eventBase, type: 'session.queue.claimed' }
    case 'session.queue.cancelled':
      return { ...eventBase, type: 'session.queue.cancelled' }
    case 'session.queue.expired':
      return { ...eventBase, type: 'session.queue.expired' }
    case 'session.queue.failed':
      return { ...eventBase, type: 'session.queue.failed' }
    case 'session.steer.applied':
      return { ...eventBase, type: 'session.steer.applied', runtime_run_id: draft.runtime_run_id }
  }
}

export interface SessionQueueConsumeInput {
  workspace_id: number
  session_id: number
  queue_item_id: string
  claimed_by: string
  claim_epoch: number
  consumed_runtime_run_id: string
  updated_at: string
}

export type SessionQueueConsumeResult =
  | { outcome: 'consumed', item: SessionQueueItem, run: AIRuntimeRun }
  | { outcome: 'already_consumed', item: SessionQueueItem, run: AIRuntimeRun }
  | { outcome: 'stale_claim' }
  | { outcome: 'active_run_conflict' }

export interface SessionQueueConsumeWithRunInput {
  workspace_id: number
  session_id: number
  queue_item_id: string
  claimed_by: string
  claim_epoch: number
  user_entry: AISessionEntry
  assistant_entry: AISessionEntry
  run: AIRuntimeRun
  updated_session_progress: AISession
  follow_up_started_event: SessionFollowUpStartedEvent
}

export type SessionQueueConsumeWithRunResult =
  | {
      outcome: 'consumed'
      item: SessionQueueItem
      run: AIRuntimeRun
      user_entry: AISessionEntry
      assistant_entry: AISessionEntry
      follow_up_started_event: SessionFollowUpStartedEvent
      event_committed: boolean
    }
  | { outcome: 'already_consumed', item: SessionQueueItem, run: AIRuntimeRun }
  | { outcome: 'stale_claim' }
  | { outcome: 'active_run_conflict' }

export type ToolActionCreateResult =
  | { outcome: 'created', action: AgentAction }
  | { outcome: 'existing', action: AgentAction }

export interface ActionDecisionInput {
  workspace_id: number
  action_id: number
  decision: ActionDecision
  session_grant?: SessionPermissionGrant
  decided_at: string
}

export type ActionDecisionResult =
  | { outcome: 'decided', action: AgentAction }
  | { outcome: 'already_decided', action: AgentAction }
  | { outcome: 'not_found' }

export interface ActionExecutionClaimInput {
  workspace_id: number
  action_id: number
  input_digest: string
  claimed_at: string
}

export type ActionExecutionClaimResult =
  | { outcome: 'claimed', action: AgentAction, execution: ActionExecution }
  | { outcome: 'already_claimed', action: AgentAction }
  | { outcome: 'input_mismatch' }
  | { outcome: 'not_approved' }

export type AgentWorkspaceCreateResult =
  | { outcome: 'created', workspace: AIRuntimeWorkspace }
  | { outcome: 'existing', workspace: AIRuntimeWorkspace }

export interface AgentWorkspaceProvisionClaimInput {
  workspace_id: number
  workspace_runtime_id: string
  owner_user_id: number
  instance_id: string
  lease_expires_at: string
  observed_at: string
}

export interface AgentWorkspaceProvisionCompleteInput {
  workspace_id: number
  workspace_runtime_id: string
  instance_id: string
  provision_epoch: number
  sandbox_id: string
  updated_at: string
}

export interface AgentWorkspaceProvisionFailInput {
  workspace_id: number
  workspace_runtime_id: string
  instance_id: string
  provision_epoch: number
  error_code: string
  error_msg: string
  updated_at: string
}

export class MemoryRuntimeStore implements RuntimeStore {
  private profileSeq = 1
  private profileVersionSeq = 1
  private agentResourceSeq = 1
  private agentResourceVersionSeq = 1
  private sessionSeq = 1
  private entrySeq = 1
  private runSeq = 1
  private actionSeq = 1
  private agentWorkspaceSeq = 1
  private profiles = new Map<number, AgentProfileDraft>()
  private profileVersions = new Map<number, AgentProfileVersion>()
  private agentResources = new Map<number, AgentResource>()
  private agentResourceVersions = new Map<number, AgentResourceVersion>()
  private sessions = new Map<number, AISession>()
  private currentSessions = new Map<string, number>()
  private entries = new Map<number, AISessionEntry>()
  private sessionQueueItems = new Map<string, SessionQueueItem>()
  private sessionQueueSeqs = new Map<string, number>()
  private runs = new Map<string, AIRuntimeRun>()
  private actions = new Map<number, AgentAction>()
  private actionBusinessIndex = new Map<string, number>()
  private runEventSeqs = new Map<string, number>()
  private runArtifactSeqs = new Map<string, number>()
  private actionChildSeqs = new Map<number, number>()
  private actionEvents = new Map<string, ActionEvent>()
  private runtimeArtifacts = new Map<string, RuntimeArtifact>()
  private childRunLinks = new Map<string, ChildRunLink>()
  private orchestrationPlans = new Map<string, OrchestrationPlan>()
  private orchestrationContextPacks = new Map<string, OrchestrationContextPack>()
  private orchestrationTasks = new Map<string, OrchestrationTask>()
  private actionDecisionSeqs = new Map<number, number>()
  private sessionPermissionGrantSeqs = new Map<number, number>()
  private actionExecutionAttempts = new Map<number, number>()
  private actionDecisions = new Map<string, ActionDecision>()
  private sessionPermissionGrants = new Map<string, SessionPermissionGrant>()
  private actionExecutions = new Map<string, ActionExecution>()
  private agentWorkspaces = new Map<string, AIRuntimeWorkspace>()
  private agentWorkspaceAudits = new Map<string, AIRuntimeWorkspaceAudit>()

  async nextProfileId() {
    return this.profileSeq++
  }

  async nextProfileVersionId() {
    return this.profileVersionSeq++
  }

  async nextAgentResourceId() {
    return this.agentResourceSeq++
  }

  async nextAgentResourceVersionId() {
    return this.agentResourceVersionSeq++
  }

  async nextSessionId() {
    return this.sessionSeq++
  }

  async nextEntryId() {
    return this.entrySeq++
  }

  async nextRunId() {
    return this.runSeq++
  }

  async nextActionId() {
    return this.actionSeq++
  }

  async nextAgentWorkspaceId() {
    return this.agentWorkspaceSeq++
  }

  async nextRunEventSeq(runtimeRunID: string) {
    const nextSeq = (this.runEventSeqs.get(runtimeRunID) || 0) + 1
    this.runEventSeqs.set(runtimeRunID, nextSeq)
    return nextSeq
  }

  async nextRunArtifactSeq(runtimeRunID: string) {
    const nextSeq = (this.runArtifactSeqs.get(runtimeRunID) || 0) + 1
    this.runArtifactSeqs.set(runtimeRunID, nextSeq)
    return nextSeq
  }

  async nextActionChildSeq(actionID: number) {
    const nextSeq = (this.actionChildSeqs.get(actionID) || 0) + 1
    this.actionChildSeqs.set(actionID, nextSeq)
    return nextSeq
  }

  async nextActionDecisionSeq(actionID: number) {
    const nextSeq = (this.actionDecisionSeqs.get(actionID) || 0) + 1
    this.actionDecisionSeqs.set(actionID, nextSeq)
    return nextSeq
  }

  async nextSessionPermissionGrantSeq(sessionID: number) {
    const nextSeq = (this.sessionPermissionGrantSeqs.get(sessionID) || 0) + 1
    this.sessionPermissionGrantSeqs.set(sessionID, nextSeq)
    return nextSeq
  }

  async nextActionExecutionAttempt(actionID: number) {
    const nextAttempt = (this.actionExecutionAttempts.get(actionID) || 0) + 1
    this.actionExecutionAttempts.set(actionID, nextAttempt)
    return nextAttempt
  }

  async enqueueSessionQueueItem(input: SessionQueueEnqueueInput, activeItemLimit: number): Promise<SessionQueueEnqueueResult> {
    const existing = [...this.sessionQueueItems.values()].find((item) =>
      item.workspace_id === input.workspace_id &&
      item.session_id === input.session_id &&
      item.client_item_id === input.client_item_id
    )
    if (existing) return { outcome: 'existing', item: structuredClone(existing) }

    const activeItems = [...this.sessionQueueItems.values()].filter((item) =>
      item.workspace_id === input.workspace_id &&
      item.session_id === input.session_id &&
      ['pending', 'claimed'].includes(item.status)
    )
    if (activeItems.length >= activeItemLimit) return { outcome: 'capacity_exceeded' }

    const sequenceKey = `${input.workspace_id}:${input.session_id}`
    const itemSeq = (this.sessionQueueSeqs.get(sequenceKey) || 0) + 1
    this.sessionQueueSeqs.set(sequenceKey, itemSeq)
    const position = activeItems.reduce((maximum, item) => Math.max(maximum, item.position), 0) + 1
    const item: SessionQueueItem = {
      queue_item_id: sessionQueueItemBusinessID(input.workspace_id, input.session_id, itemSeq),
      workspace_id: input.workspace_id,
      session_id: input.session_id,
      item_seq: itemSeq,
      position,
      mode: input.mode,
      content: input.content,
      attachments: structuredClone(input.attachments),
      context_ref: structuredClone(input.context_ref),
      target_run_id: input.target_run_id,
      expires_at: input.expires_at,
      status: 'pending',
      client_item_id: input.client_item_id,
      claim_epoch: 0,
      created_by: input.created_by,
      created_at: input.created_at,
      updated_at: input.created_at
    }
    this.sessionQueueItems.set(`${item.workspace_id}:${item.queue_item_id}`, structuredClone(item))
    return input.lifecycle_event
      ? {
          outcome: 'created',
          item: structuredClone(item),
          event: sessionQueueLifecycleEvent(input.lifecycle_event, item),
          event_committed: false
        }
      : { outcome: 'created', item: structuredClone(item) }
  }

  async listSessionQueueItems(workspaceID: number, sessionID: number) {
    return [...this.sessionQueueItems.values()]
      .filter((item) => item.workspace_id === workspaceID && item.session_id === sessionID)
      .sort(sessionQueueItemOrder)
      .map((item) => structuredClone(item))
  }

  async cancelSessionQueueItem(input: SessionQueueCancelInput): Promise<SessionQueueCancelResult> {
    const key = `${input.workspace_id}:${input.queue_item_id}`
    const item = this.sessionQueueItems.get(key)
    if (!item || item.session_id !== input.session_id) return { outcome: 'not_found' }
    if (item.status === 'cancelled') return { outcome: 'already_cancelled', item: structuredClone(item) }
    if (item.status !== 'pending') return { outcome: 'not_cancellable' }
    const cancelled: SessionQueueItem = { ...item, status: 'cancelled', updated_at: input.updated_at }
    this.sessionQueueItems.set(key, cancelled)
    return input.lifecycle_event
      ? {
          outcome: 'cancelled',
          item: structuredClone(cancelled),
          event: sessionQueueLifecycleEvent(input.lifecycle_event, cancelled),
          event_committed: false
        }
      : { outcome: 'cancelled', item: structuredClone(cancelled) }
  }

  async reorderSessionQueueItems(workspaceID: number, sessionID: number, queueItemIDs: string[], updatedAt: string): Promise<SessionQueueReorderResult> {
    const pending = [...this.sessionQueueItems.values()]
      .filter((item) => item.workspace_id === workspaceID && item.session_id === sessionID && item.status === 'pending')
    if (!sameSessionQueueItemSet(pending.map((item) => item.queue_item_id), queueItemIDs)) return { outcome: 'conflict' }
    const byID = new Map(pending.map((item) => [item.queue_item_id, item]))
    const reordered = queueItemIDs.map((queueItemID, index) => ({
      ...(byID.get(queueItemID) as SessionQueueItem),
      position: index + 1,
      updated_at: updatedAt
    }))
    for (const item of reordered) this.sessionQueueItems.set(`${workspaceID}:${item.queue_item_id}`, item)
    return { outcome: 'reordered', items: reordered.map((item) => structuredClone(item)) }
  }

  async claimNextSessionQueueItem(input: SessionQueueClaimInput): Promise<SessionQueueClaimResult> {
    const claimComparisonTime = Date.parse(input.updated_at)
    const candidates = [...this.sessionQueueItems.values()]
      .filter((item) =>
        item.workspace_id === input.workspace_id &&
        item.session_id === input.session_id &&
        (
          item.status === 'pending' ||
          (
            item.status === 'claimed' &&
            Boolean(item.claim_expires_at) &&
            Date.parse(item.claim_expires_at || '') <= claimComparisonTime
          )
        ) &&
        (!input.queue_item_id || item.queue_item_id === input.queue_item_id)
      )
      .sort(sessionQueueItemOrder)
    const next = candidates[0]
    if (!next) return { outcome: 'empty' }
    const claimed: SessionQueueItem = {
      ...next,
      status: 'claimed',
      claimed_by: input.claimed_by,
      claim_epoch: Number(next.claim_epoch || 0) + 1,
      claim_expires_at: input.claim_expires_at,
      updated_at: input.updated_at
    }
    this.sessionQueueItems.set(`${claimed.workspace_id}:${claimed.queue_item_id}`, claimed)
    return input.lifecycle_event
      ? {
          outcome: 'claimed',
          item: structuredClone(claimed),
          event: sessionQueueLifecycleEvent(input.lifecycle_event, claimed),
          event_committed: false
        }
      : { outcome: 'claimed', item: structuredClone(claimed) }
  }

  async updateSessionQueueItem(item: SessionQueueItem) {
    const key = `${item.workspace_id}:${item.queue_item_id}`
    const existing = this.sessionQueueItems.get(key)
    if (!existing || existing.session_id !== item.session_id) {
      throw new RuntimeDomainError('queue_item_not_found', 'Session queue item not found', 404)
    }
    const stored = structuredClone(item)
    this.sessionQueueItems.set(key, stored)
    return structuredClone(stored)
  }

  async applySessionQueueSteer(input: SessionQueueSteerApplyInput): Promise<SessionQueueSteerApplyResult> {
    const item = this.sessionQueueItems.get(`${input.workspace_id}:${input.queue_item_id}`)
    const run = this.runs.get(input.runtime_run_id)
    if (!item || item.session_id !== input.session_id) return { outcome: 'stale_claim' }
    if (!run || run.workspace_id !== input.workspace_id || run.session_id !== input.session_id || !isActiveRunStatus(run.status)) {
      return { outcome: 'target_not_active' }
    }
    if (item.status === 'applied') return { outcome: 'already_applied', item: structuredClone(item), run: structuredClone(run) }
    if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
      return { outcome: 'stale_claim' }
    }
    const updatedRun: AIRuntimeRun = { ...run, result: structuredClone(input.run_result), updated_at: input.updated_at }
    const applied: SessionQueueItem = { ...item, status: 'applied', updated_at: input.updated_at }
    this.runs.set(updatedRun.runtime_run_id, updatedRun)
    this.sessionQueueItems.set(`${applied.workspace_id}:${applied.queue_item_id}`, applied)
    return input.lifecycle_event
      ? {
          outcome: 'applied',
          item: structuredClone(applied),
          run: structuredClone(updatedRun),
          event: sessionQueueLifecycleEvent(input.lifecycle_event, applied),
          event_committed: false
        }
      : { outcome: 'applied', item: structuredClone(applied), run: structuredClone(updatedRun) }
  }

  async transitionSessionQueueItem(input: SessionQueueTransitionInput): Promise<SessionQueueTransitionResult> {
    const key = `${input.item.workspace_id}:${input.item.queue_item_id}`
    const current = this.sessionQueueItems.get(key)
    if (
      !current ||
      current.session_id !== input.item.session_id ||
      current.status !== 'claimed' ||
      current.claimed_by !== input.claimed_by ||
      current.claim_epoch !== input.claim_epoch
    ) return { outcome: 'stale_claim' }
    const stored = structuredClone(input.item)
    this.sessionQueueItems.set(key, stored)
    return input.lifecycle_event
      ? {
          outcome: 'transitioned',
          item: structuredClone(stored),
          event: sessionQueueLifecycleEvent(input.lifecycle_event, stored),
          event_committed: false
        }
      : { outcome: 'transitioned', item: structuredClone(stored) }
  }

  async consumeSessionQueueItem(input: SessionQueueConsumeInput): Promise<SessionQueueConsumeResult> {
    const item = this.sessionQueueItems.get(`${input.workspace_id}:${input.queue_item_id}`)
    const run = this.runs.get(input.consumed_runtime_run_id)
    if (!item || item.session_id !== input.session_id || !run || run.workspace_id !== input.workspace_id || run.session_id !== input.session_id) {
      return { outcome: 'stale_claim' }
    }
    if (item.status === 'consumed') return { outcome: 'already_consumed', item: structuredClone(item), run: structuredClone(run) }
    if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
      return { outcome: 'stale_claim' }
    }
    const activeConflict = [...this.runs.values()].some((candidate) =>
      candidate.workspace_id === input.workspace_id &&
      candidate.session_id === input.session_id &&
      candidate.runtime_run_id !== input.consumed_runtime_run_id &&
      isActiveRunStatus(candidate.status)
    )
    if (activeConflict) return { outcome: 'active_run_conflict' }
    const consumed: SessionQueueItem = {
      ...item,
      status: 'consumed',
      consumed_runtime_run_id: input.consumed_runtime_run_id,
      updated_at: input.updated_at
    }
    this.sessionQueueItems.set(`${consumed.workspace_id}:${consumed.queue_item_id}`, consumed)
    return { outcome: 'consumed', item: structuredClone(consumed), run: structuredClone(run) }
  }

  async consumeSessionQueueItemWithRun(input: SessionQueueConsumeWithRunInput): Promise<SessionQueueConsumeWithRunResult> {
    const itemKey = `${input.workspace_id}:${input.queue_item_id}`
    const item = this.sessionQueueItems.get(itemKey)
    if (!item || item.session_id !== input.session_id) return { outcome: 'stale_claim' }
    if (item.status === 'consumed') {
      const existingRun = item.consumed_runtime_run_id ? this.runs.get(item.consumed_runtime_run_id) : undefined
      return existingRun
        ? { outcome: 'already_consumed', item: structuredClone(item), run: structuredClone(existingRun) }
        : { outcome: 'stale_claim' }
    }
    if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
      return { outcome: 'stale_claim' }
    }
    const activeConflict = [...this.runs.values()].some((candidate) =>
      candidate.workspace_id === input.workspace_id &&
      candidate.session_id === input.session_id &&
      candidate.runtime_run_id !== input.run.runtime_run_id &&
      isActiveRunStatus(candidate.status)
    )
    if (activeConflict) return { outcome: 'active_run_conflict' }

    const consumed: SessionQueueItem = {
      ...item,
      status: 'consumed',
      consumed_runtime_run_id: input.run.runtime_run_id,
      updated_at: input.follow_up_started_event.timestamp
    }
    const event: SessionFollowUpStartedEvent = {
      ...input.follow_up_started_event,
      runtime_run_id: input.run.runtime_run_id,
      consumed_runtime_run_id: input.run.runtime_run_id,
      queue_item: consumed
    }
    const run = normalizeRuntimeRunActiveSlot(structuredClone(input.run))
    this.runs.set(run.runtime_run_id, run)
    this.entries.set(input.user_entry.id, structuredClone(input.user_entry))
    this.entries.set(input.assistant_entry.id, structuredClone(input.assistant_entry))
    this.sessions.set(input.updated_session_progress.id, structuredClone(input.updated_session_progress))
    this.sessionQueueItems.set(itemKey, consumed)
    return {
      outcome: 'consumed',
      item: structuredClone(consumed),
      run: structuredClone(run),
      user_entry: structuredClone(input.user_entry),
      assistant_entry: structuredClone(input.assistant_entry),
      follow_up_started_event: structuredClone(event),
      event_committed: false
    }
  }

  async saveProfile(profile: AgentProfileDraft) {
    const nameConflict = [...this.profiles.values()].find(
      (candidate) => candidate.workspace_id === profile.workspace_id && candidate.name === profile.name && candidate.id !== profile.id
    )
    if (nameConflict) throw new RuntimeDomainError('agent_profile_name_exists', 'Agent profile name already exists', 409)
    const stored = structuredClone(profile)
    this.profiles.set(stored.id, stored)
    return structuredClone(stored)
  }

  async getProfile(workspaceID: number, profileID: number) {
    const profile = this.profiles.get(profileID)
    return profile && profile.workspace_id === workspaceID ? profile : undefined
  }

  async listProfiles(workspaceID: number) {
    return [...this.profiles.values()]
      .filter((profile) => profile.workspace_id === workspaceID)
      .sort((left, right) => right.id - left.id)
  }

  async deleteProfileGuarded(workspaceID: number, profileID: number) {
    const profile = await this.getProfile(workspaceID, profileID)
    if (!profile) return 'not_found' as const
    const hasPublishedVersion = [...this.profileVersions.values()].some(
      (version) => version.workspace_id === workspaceID && version.profile_id === profileID
    )
    const hasParentRef = [...this.profiles.values()].some((parent) =>
      parent.workspace_id === workspaceID && parent.id !== profileID &&
      parent.subagents.some((ref) => ref.resource_type === 'subagent_profile' && Number(ref.resource_id) === profileID)
    )
    const hasSession = [...this.sessions.values()].some(
      (session) => session.workspace_id === workspaceID && session.agent_profile_id === profileID
    )
    if (hasPublishedVersion || hasParentRef || hasSession) return 'in_use' as const
    this.profiles.delete(profileID)
    return 'deleted' as const
  }

  async saveAgentResource(resource: AgentResource) {
    const existing = [...this.agentResources.values()].find(
      (item) =>
        item.workspace_id === resource.workspace_id &&
        item.resource_kind === resource.resource_kind &&
        item.resource_key === resource.resource_key &&
        item.id !== resource.id
    )
    if (existing) {
      this.agentResources.delete(existing.id)
    }
    this.agentResources.set(resource.id, resource)
    return resource
  }

  private assertAgentResourceIdentityAvailable(resource: AgentResource) {
    const conflict = [...this.agentResources.values()].find(
      (item) => item.workspace_id === resource.workspace_id && item.resource_kind === resource.resource_kind &&
        item.resource_key === resource.resource_key && item.id !== resource.id
    )
    if (conflict) throw new RuntimeDomainError('agent_resource_key_exists', 'Agent resource key already exists', 409)
  }

  private saveAgentResourceRevision(resource: AgentResource, version: AgentResourceVersion, create: boolean) {
    assertAgentResourceVersionMatchesResource(resource, version)
    this.assertAgentResourceIdentityAvailable(resource)
    const existingHead = this.agentResources.get(resource.id)
    if (create ? existingHead !== undefined : existingHead === undefined || existingHead.workspace_id !== resource.workspace_id) {
      throw new RuntimeDomainError(
        create ? 'agent_resource_key_exists' : 'agent_resource_not_found',
        create ? 'Agent resource already exists' : 'Agent resource not found',
        create ? 409 : 404
      )
    }
    const duplicateID = this.agentResourceVersions.has(version.resource_version_id)
    const duplicateRevision = [...this.agentResourceVersions.values()].some((item) =>
      item.workspace_id === version.workspace_id &&
      item.resource_id === version.resource_id &&
      item.revision === version.revision
    )
    if (duplicateID || duplicateRevision) {
      throw new Error(`Agent resource revision ${version.resource_id}:${version.revision} already exists`)
    }

    const storedResource = structuredClone(resource)
    const storedVersion = structuredClone(version)
    this.agentResources.set(storedResource.id, storedResource)
    this.agentResourceVersions.set(storedVersion.resource_version_id, storedVersion)
    return {
      resource: structuredClone(storedResource),
      version: structuredClone(storedVersion)
    }
  }

  async createAgentResourceWithVersion(resource: AgentResource, version: AgentResourceVersion) {
    return this.saveAgentResourceRevision(resource, version, true)
  }

  async updateAgentResourceWithVersion(resource: AgentResource, version: AgentResourceVersion) {
    return this.saveAgentResourceRevision(resource, version, false)
  }

  async getAgentResource(workspaceID: number, resourceID: number) {
    const resource = this.agentResources.get(resourceID)
    return resource && resource.workspace_id === workspaceID ? resource : undefined
  }

  async listAgentResources(workspaceID: number) {
    return [...this.agentResources.values()]
      .filter((resource) => resource.workspace_id === workspaceID)
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at) || right.id - left.id)
  }

  async deleteAgentResource(workspaceID: number, resourceID: number) {
    const resource = await this.getAgentResource(workspaceID, resourceID)
    if (!resource) return false
    return this.agentResources.delete(resourceID)
  }

  async deleteAgentResourceGuarded(workspaceID: number, resourceID: number) {
    const resource = await this.getAgentResource(workspaceID, resourceID)
    if (!resource) return 'not_found' as const
    const versionIDs = new Set([...this.agentResourceVersions.values()]
      .filter((version) => version.workspace_id === workspaceID && version.resource_id === resourceID)
      .map((version) => version.resource_version_id))
    const section = resource.resource_kind === 'skill' ? 'skills' : 'mcp_servers'
    const hasLiveRef = [...this.profiles.values()].some((profile) =>
      profile.workspace_id === workspaceID && profile[section].some((ref) =>
        versionIDs.has(Number(ref.resource_version_id || 0))
      )
    )
    const hasActiveExecution = [...this.runs.values()].some((run) => {
      if (run.workspace_id !== workspaceID || !ACTIVE_RUNTIME_RUN_STATUSES.has(run.status)) return false
      const frozen = run.profile_snapshot?.frozen_resources?.[section] || []
      return frozen.some((item) => item.id === resourceID)
    })
    if (hasLiveRef || hasActiveExecution) return 'in_use' as const
    this.agentResources.delete(resourceID)
    for (const [versionID, version] of this.agentResourceVersions.entries()) {
      if (version.workspace_id === workspaceID && version.resource_id === resourceID) {
        this.agentResourceVersions.delete(versionID)
      }
    }
    return 'deleted' as const
  }

  async appendAgentResourceVersion(version: AgentResourceVersion) {
    assertAgentResourceVersionSnapshotSafe(version)
    const duplicateID = this.agentResourceVersions.has(version.resource_version_id)
    const duplicateRevision = [...this.agentResourceVersions.values()].some((item) =>
      item.workspace_id === version.workspace_id &&
      item.resource_id === version.resource_id &&
      item.revision === version.revision
    )
    if (duplicateID || duplicateRevision) {
      throw new Error(`Agent resource revision ${version.resource_id}:${version.revision} already exists`)
    }
    const stored = structuredClone(version)
    this.agentResourceVersions.set(stored.resource_version_id, stored)
    return structuredClone(stored)
  }

  async getAgentResourceVersion(workspaceID: number, resourceVersionID: number) {
    const version = this.agentResourceVersions.get(resourceVersionID)
    return version && version.workspace_id === workspaceID ? this.verifiedAgentResourceVersion(version) : undefined
  }

  async listAgentResourceVersions(workspaceID: number, resourceID: number) {
    return [...this.agentResourceVersions.values()]
      .filter((version) => version.workspace_id === workspaceID && version.resource_id === resourceID)
      .sort((left, right) => right.revision - left.revision || right.resource_version_id - left.resource_version_id)
      .map((version) => this.verifiedAgentResourceVersion(version))
  }

  async resolveProviderCredentialRef(_workspaceID: number, _providerID: string | number, _credentialID: string | number) {
    return undefined
  }

  async saveProfileVersion(version: AgentProfileVersion) {
    const duplicate = [...this.profileVersions.values()].find((candidate) =>
      candidate.profile_version_id === version.profile_version_id ||
      (candidate.profile_id === version.profile_id && candidate.version === version.version) ||
      (candidate.workspace_id === version.workspace_id && candidate.snapshot_hash === version.snapshot_hash)
    )
    if (duplicate) throw new RuntimeDomainError('agent_profile_version_conflict', 'Agent profile version already exists', 409)
    const stored = structuredClone(version)
    this.profileVersions.set(stored.profile_version_id, stored)
    return structuredClone(stored)
  }

  async publishProfileVersion(profile: AgentProfileDraft, version: AgentProfileVersion, expectedUpdatedAt: string) {
    const current = await this.getProfile(profile.workspace_id, profile.id)
    if (!current) throw new RuntimeDomainError('agent_profile_not_found', 'Agent profile not found', 404)
    if (current.updated_at !== expectedUpdatedAt) {
      throw new RuntimeDomainError('agent_profile_publish_conflict', 'Agent profile changed during publish', 409)
    }
    const existing = [...this.profileVersions.values()].find(
      (candidate) => candidate.workspace_id === version.workspace_id && candidate.snapshot_hash === version.snapshot_hash
    )
    if (existing) {
      if (existing.profile_id !== profile.id) {
        throw new RuntimeDomainError('agent_profile_version_conflict', 'Agent profile snapshot hash already exists', 409)
      }
      this.profiles.set(profile.id, structuredClone(profile))
      return structuredClone(existing)
    }
    const saved = await this.saveProfileVersion(version)
    this.profiles.set(profile.id, structuredClone(profile))
    return saved
  }

  async listProfileVersions(workspaceID: number, profileID: number) {
    return [...this.profileVersions.values()]
      .filter((version) => version.workspace_id === workspaceID && version.profile_id === profileID)
      .sort((left, right) => right.version - left.version)
      .map((version) => this.verifiedProfileVersion(version))
  }

  async getProfileVersion(workspaceID: number, profileVersionID: number) {
    const version = this.profileVersions.get(profileVersionID)
    return version && version.workspace_id === workspaceID ? this.verifiedProfileVersion(version) : undefined
  }

  private verifiedAgentResourceVersion(version: AgentResourceVersion) {
    if (agentResourceSnapshotDigest(version.snapshot) !== version.snapshot_digest) {
      throw new RuntimeDomainError(
        'agent_resource_version_integrity_failed',
        'Agent resource version snapshot integrity verification failed',
        500
      )
    }
    return structuredClone(version)
  }

  private verifiedProfileVersion(version: AgentProfileVersion) {
    if (agentProfileSnapshotHash(version.snapshot) !== version.snapshot_hash) {
      throw new RuntimeDomainError(
        'agent_profile_version_integrity_failed',
        'Agent profile version snapshot integrity verification failed',
        500
      )
    }
    return structuredClone(version)
  }

  async saveSession(session: AISession) {
    this.sessions.set(session.id, session)
    return session
  }

  async getSession(workspaceID: number, sessionID: number) {
    const session = this.sessions.get(sessionID)
    return session && session.workspace_id === workspaceID ? session : undefined
  }

  async listSessions(workspaceID: number, filter: SessionListFilter = {}) {
    return [...this.sessions.values()]
      .filter((session) => session.workspace_id === workspaceID)
      .filter((session) => !filter.context_tag || session.context_tags.includes(filter.context_tag))
      .filter((session) => !filter.context_tags?.length || filter.context_tags.every((tag) => session.context_tags.includes(tag)))
      .filter((session) => !filter.business_type || session.business_type === filter.business_type)
      .filter((session) => !filter.business_id || session.business_id === filter.business_id)
      .filter((session) => !filter.status || session.status === filter.status)
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at) || right.id - left.id)
  }

  async findCurrentSession(key: string) {
    const sessionID = this.currentSessions.get(key)
    if (!sessionID) return undefined
    return this.sessions.get(sessionID)
  }

  async rememberCurrentSession(key: string, sessionID: number) {
    this.currentSessions.set(key, sessionID)
  }

  async saveEntry(entry: AISessionEntry) {
    this.entries.set(entry.id, entry)
    return entry
  }

  async listEntries(workspaceID: number, sessionID: number) {
    return [...this.entries.values()]
      .filter((entry) => entry.workspace_id === workspaceID && entry.session_id === sessionID)
      .sort((left, right) => left.seq - right.seq)
  }

  async saveRun(run: AIRuntimeRun) {
    const normalized = normalizeRuntimeRunActiveSlot(run)
    if (ACTIVE_RUNTIME_RUN_STATUSES.has(normalized.status)) {
      const existingActiveRun = [...this.runs.values()].find((candidate) =>
        candidate.workspace_id === normalized.workspace_id &&
        candidate.session_id === normalized.session_id &&
        candidate.runtime_run_id !== normalized.runtime_run_id &&
        ACTIVE_RUNTIME_RUN_STATUSES.has(candidate.status)
      )
      if (existingActiveRun) {
        throw new RuntimeDomainError(
          'active_run_conflict',
          `Session already has active runtime run ${existingActiveRun.runtime_run_id} in status ${existingActiveRun.status}`,
          409,
          {
            active_run: {
              runtime_run_id: existingActiveRun.runtime_run_id,
              status: existingActiveRun.status,
              next_steps: existingActiveRun.status === 'awaiting_decision' ? ['approve_once', 'approve_session', 'reject', 'cancel'] : ['wait', 'cancel']
            }
          }
        )
      }
    }
    this.runs.set(normalized.runtime_run_id, normalized)
    return normalized
  }

  async getRun(workspaceID: number, runtimeRunID: string) {
    const run = this.runs.get(runtimeRunID)
    return run && run.workspace_id === workspaceID ? run : undefined
  }

  async findActiveRun(workspaceID: number, sessionID: number) {
    const activeStatuses = new Set<AIRuntimeRun['status']>(['queued', 'running', 'awaiting_decision', 'awaiting_input'])
    return [...this.runs.values()]
      .filter((run) => run.workspace_id === workspaceID && run.session_id === sessionID && activeStatuses.has(run.status))
      .sort((left, right) => right.created_at.localeCompare(left.created_at) || right.id - left.id)[0]
  }

  async claimRunLease(workspaceID: number, runtimeRunID: string, instanceID: string, leaseExpiresAt: string, observedAt = new Date().toISOString()) {
    const run = await this.getRun(workspaceID, runtimeRunID)
    if (!run || !ACTIVE_RUNTIME_RUN_STATUSES.has(run.status)) return undefined
    const leaseIsLive = Boolean(run.owner_instance_id && run.owner_lease_expires_at && run.owner_lease_expires_at > observedAt)
    if (leaseIsLive && run.owner_instance_id !== instanceID) return undefined
    const ownerChanged = run.owner_instance_id !== instanceID
    const claimed = normalizeRuntimeRunActiveSlot({
      ...run,
      owner_instance_id: instanceID,
      owner_epoch: ownerChanged ? Number(run.owner_epoch || 0) + 1 : Math.max(1, Number(run.owner_epoch || 0)),
      owner_lease_expires_at: leaseExpiresAt,
      updated_at: observedAt
    })
    this.runs.set(runtimeRunID, claimed)
    return claimed
  }

  async renewRunLease(workspaceID: number, runtimeRunID: string, instanceID: string, ownerEpoch: number, leaseExpiresAt: string) {
    const run = await this.getRun(workspaceID, runtimeRunID)
    if (!run || !ACTIVE_RUNTIME_RUN_STATUSES.has(run.status)) return undefined
    if (run.owner_instance_id !== instanceID || Number(run.owner_epoch || 0) !== ownerEpoch) return undefined
    const renewed = { ...run, owner_lease_expires_at: leaseExpiresAt, updated_at: new Date().toISOString() }
    this.runs.set(runtimeRunID, renewed)
    return renewed
  }

  async releaseRunLease(workspaceID: number, runtimeRunID: string, instanceID: string, ownerEpoch: number) {
    const run = await this.getRun(workspaceID, runtimeRunID)
    if (!run || run.owner_instance_id !== instanceID || Number(run.owner_epoch || 0) !== ownerEpoch) return false
    this.runs.set(runtimeRunID, {
      ...run,
      owner_instance_id: undefined,
      owner_lease_expires_at: undefined,
      updated_at: new Date().toISOString()
    })
    return true
  }

  async interruptExpiredRunLeases(observedAt: string) {
    const interrupted: AIRuntimeRun[] = []
    for (const run of this.runs.values()) {
      if (!ACTIVE_RUNTIME_RUN_STATUSES.has(run.status) || run.status === 'awaiting_decision') continue
      if (!run.owner_instance_id || !run.owner_lease_expires_at || run.owner_lease_expires_at > observedAt) continue
      const next = normalizeRuntimeRunActiveSlot({
        ...run,
        status: 'interrupted',
        owner_instance_id: undefined,
        owner_lease_expires_at: undefined,
        error_code: 'runtime_owner_lease_expired',
        error_msg: `Runtime owner ${run.owner_instance_id} lease expired`,
        finished_at: observedAt,
        updated_at: observedAt
      })
      this.runs.set(run.runtime_run_id, next)
      interrupted.push(next)
    }
    return interrupted
  }

  async getRuntimeOperationsSummary(observedAt: string): Promise<RuntimeOperationsSummary> {
    const runs = [...this.runs.values()]
    const fiveMinutesAgo = new Date(Date.parse(observedAt) - 5 * 60_000).toISOString()
    const oneHourAgo = new Date(Date.parse(observedAt) - 60 * 60_000).toISOString()
    const terminalStatuses = ['completed', 'failed', 'cancelled', 'timeout', 'interrupted'] as const
    const terminalCounts = (threshold: string) => Object.fromEntries(terminalStatuses.map((status) => [
      status,
      runs.filter((run) => {
        const terminalAt = firstString(run.finished_at, run.updated_at)
        return run.status === status && terminalAt >= threshold && terminalAt <= observedAt
      }).length
    ])) as RuntimeOperationsSummary['terminal_5m']
    const failures = new Map<string, { category: string, code: string, count: number }>()
    for (const run of runs) {
      if (!['failed', 'timeout', 'interrupted'].includes(run.status)) continue
      const terminalAt = firstString(run.finished_at, run.updated_at)
      if (terminalAt < oneHourAgo || terminalAt > observedAt) continue
      const error = asRecord(run.result.error)
      const category = firstString(error.category, run.status === 'timeout' ? 'provider_timeout' : 'internal')
      const code = firstString(error.code, run.error_code, 'runtime_error')
      const key = `${category}:${code}`
      const existing = failures.get(key)
      if (existing) existing.count += 1
      else failures.set(key, { category, code, count: 1 })
    }
    const activeRuns = runs.filter((run) => ACTIVE_RUNTIME_RUN_STATUSES.has(run.status))
    return {
      observed_at: observedAt,
      runs: {
        active: activeRuns.length,
        awaiting_approval: activeRuns.filter((run) => run.status === 'awaiting_decision').length,
        awaiting_input: activeRuns.filter((run) => run.status === 'awaiting_input').length,
        stale: activeRuns.filter((run) => ['queued', 'running'].includes(run.status)
          && Boolean(run.owner_lease_expires_at && run.owner_lease_expires_at <= observedAt)).length,
        orphaned: activeRuns.filter((run) => ['queued', 'running'].includes(run.status)
          && (!run.owner_instance_id || !run.owner_lease_expires_at)).length
      },
      terminal_5m: terminalCounts(fiveMinutesAgo),
      terminal_1h: terminalCounts(oneHourAgo),
      failures_1h: [...failures.values()].sort((left, right) => left.category.localeCompare(right.category) || left.code.localeCompare(right.code))
    }
  }

  async saveAction(action: AgentAction) {
    this.actions.set(action.id, action)
    this.actionBusinessIndex.set(`${action.workspace_id}:${action.action_id}`, action.id)
    return action
  }

  async createToolAction(action: AgentAction): Promise<ToolActionCreateResult> {
    const existing = this.findActionByIdempotency(action.workspace_id, action.idempotency_key)
      || this.findActionByBusinessID(action.workspace_id, action.action_id)
    if (existing) return { outcome: 'existing', action: cloneAction(existing) }
    const stored = cloneAction(action)
    this.actions.set(stored.id, stored)
    this.actionBusinessIndex.set(`${stored.workspace_id}:${stored.action_id}`, stored.id)
    return { outcome: 'created', action: cloneAction(stored) }
  }

  async decideAwaitingAction(input: ActionDecisionInput): Promise<ActionDecisionResult> {
    const action = this.actions.get(input.action_id)
    if (!action || action.workspace_id !== input.workspace_id) return { outcome: 'not_found' }
    if (action.status !== 'awaiting_decision') return { outcome: 'already_decided', action: cloneAction(action) }
    const decided = cloneAction({
      ...action,
      status: statusForDecision(input.decision.decision),
      decided_by: input.decision.actor_user_id,
      decided_at: input.decided_at,
      updated_at: input.decided_at
    })
    this.actionDecisions.set(input.decision.decision_id, structuredClone(input.decision))
    if (input.session_grant) {
      this.sessionPermissionGrants.set(`${input.session_grant.workspace_id}:${input.session_grant.grant_id}`, structuredClone(input.session_grant))
    }
    this.actions.set(decided.id, decided)
    return { outcome: 'decided', action: cloneAction(decided) }
  }

  async claimApprovedActionExecution(input: ActionExecutionClaimInput): Promise<ActionExecutionClaimResult> {
    const action = this.actions.get(input.action_id)
    if (!action || action.workspace_id !== input.workspace_id) return { outcome: 'not_approved' }
    if (action.input_digest !== input.input_digest) return { outcome: 'input_mismatch' }
    if (action.status === 'executing' || action.status === 'executed' || action.status === 'failed') {
      return { outcome: 'already_claimed', action: cloneAction(action) }
    }
    if (action.status !== 'approved') return { outcome: 'not_approved' }
    const attempt = (this.actionExecutionAttempts.get(action.id) || 0) + 1
    this.actionExecutionAttempts.set(action.id, attempt)
    const execution: ActionExecution = {
      execution_id: actionExecutionBusinessID(action, attempt),
      workspace_id: action.workspace_id,
      session_id: action.session_id,
      runtime_run_id: action.runtime_run_id,
      action_id: action.id,
      attempt,
      executor_type: executorTypeForActionKind(action.action_kind),
      executor_ref_json: { action_kind: action.action_kind, capability_id: action.capability_id },
      idempotency_key: `execution:${action.action_id}:attempt${seq36(attempt)}`,
      status: 'running',
      result_json: {},
      started_at: input.claimed_at,
      created_at: input.claimed_at,
      updated_at: input.claimed_at
    }
    const claimed = cloneAction({ ...action, status: 'executing', executed_at: input.claimed_at, updated_at: input.claimed_at })
    this.actions.set(claimed.id, claimed)
    this.actionExecutions.set(`${execution.workspace_id}:${execution.action_id}:${execution.attempt}`, execution)
    return { outcome: 'claimed', action: cloneAction(claimed), execution: structuredClone(execution) }
  }

  private findActionByIdempotency(workspaceID: number, idempotencyKey: string) {
    return [...this.actions.values()].find((action) => action.workspace_id === workspaceID && action.idempotency_key === idempotencyKey)
  }

  private findActionByBusinessID(workspaceID: number, actionBusinessID: string) {
    const actionID = this.actionBusinessIndex.get(`${workspaceID}:${actionBusinessID}`)
    return actionID ? this.actions.get(actionID) : undefined
  }

  async getAction(workspaceID: number, actionID: number) {
    const action = this.actions.get(actionID)
    return action && action.workspace_id === workspaceID ? action : undefined
  }

  async getActionByBusinessID(workspaceID: number, actionBusinessID: string) {
    const actionID = this.actionBusinessIndex.get(`${workspaceID}:${actionBusinessID}`)
    if (!actionID) return undefined
    return this.getAction(workspaceID, actionID)
  }

  async appendActionEvent(event: ActionEvent) {
    if (this.actionEvents.has(event.event_id)) {
      return this.actionEvents.get(event.event_id) as ActionEvent
    }
    this.actionEvents.set(event.event_id, event)
    return event
  }

  async listActionEvents(workspaceID: number, runtimeRunID: string, afterEventID = '', limit = 0) {
    const events = [...this.actionEvents.values()]
      .filter((event) => event.workspace_id === workspaceID && event.runtime_run_id === runtimeRunID)
      .sort((left, right) => left.event_seq - right.event_seq)
    let filtered = events
    if (afterEventID) {
      const after = events.find((event) => event.event_id === afterEventID)
      filtered = after ? events.filter((event) => event.event_seq > after.event_seq) : events
    }
    const pageLimit = Number.isFinite(Number(limit)) && Number(limit) > 0 ? Math.floor(Number(limit)) : 0
    return pageLimit > 0 ? filtered.slice(0, pageLimit) : filtered
  }

  async saveRuntimeArtifact(artifact: RuntimeArtifact) {
    this.runtimeArtifacts.set(`${artifact.workspace_id}:${artifact.artifact_id}`, artifact)
    return artifact
  }

  async getRuntimeArtifact(workspaceID: number, artifactID: string) {
    return this.runtimeArtifacts.get(`${workspaceID}:${artifactID}`)
  }

  async listActionArtifacts(workspaceID: number, actionID: string) {
    return [...this.runtimeArtifacts.values()]
      .filter((artifact) => artifact.workspace_id === workspaceID && artifact.action_id === actionID)
      .sort((left, right) => left.artifact_seq - right.artifact_seq)
  }

  async saveChildRunLink(link: ChildRunLink) {
    this.childRunLinks.set(`${link.workspace_id}:${link.child_run_link_id}`, link)
    return link
  }

  async listChildRunLinks(workspaceID: number, parentRuntimeRunID: string) {
    return [...this.childRunLinks.values()]
      .filter((link) => link.workspace_id === workspaceID && link.parent_runtime_run_id === parentRuntimeRunID)
      .sort((left, right) => left.child_seq - right.child_seq)
  }

  async getParentRunLink(workspaceID: number, childRuntimeRunID: string) {
    return [...this.childRunLinks.values()]
      .find((link) => link.workspace_id === workspaceID && link.child_runtime_run_id === childRuntimeRunID)
  }

  async saveOrchestrationPlan(plan: OrchestrationPlan) {
    this.orchestrationPlans.set(plan.orchestration_id, { ...plan })
    return this.orchestrationPlans.get(plan.orchestration_id)!
  }

  async getOrchestrationPlan(workspaceID: number, orchestrationID: string) {
    const plan = this.orchestrationPlans.get(orchestrationID)
    if (!plan) return undefined
    if (workspaceID > 0 && plan.workspace_id !== workspaceID) return undefined
    return plan
  }

  async getOrchestrationPlanByParentRun(workspaceID: number, parentRuntimeRunID: string) {
    return [...this.orchestrationPlans.values()]
      .find((plan) => plan.workspace_id === workspaceID && plan.parent_runtime_run_id === parentRuntimeRunID)
  }

  async claimOrchestrationPlan(input: { workspace_id: number, orchestration_id: string, owner: string, claim_expires_at: string }) {
    const plan = await this.getOrchestrationPlan(input.workspace_id, input.orchestration_id)
    if (!plan) return undefined
    if (plan.status === 'completed' || plan.status === 'failed' || plan.status === 'cancelled') return plan
    const claimed: OrchestrationPlan = {
      ...plan,
      claim_owner: input.owner,
      claim_epoch: Number(plan.claim_epoch || 0) + 1,
      claim_expires_at: input.claim_expires_at,
      updated_at: new Date().toISOString()
    }
    return this.saveOrchestrationPlan(claimed)
  }

  async saveOrchestrationContextPack(pack: OrchestrationContextPack) {
    this.orchestrationContextPacks.set(pack.context_pack_id, { ...pack })
    return this.orchestrationContextPacks.get(pack.context_pack_id)!
  }

  async getOrchestrationContextPack(workspaceID: number, contextPackID: string) {
    const pack = this.orchestrationContextPacks.get(contextPackID)
    if (!pack) return undefined
    if (workspaceID > 0 && pack.workspace_id !== workspaceID) return undefined
    return pack
  }

  async saveOrchestrationTask(task: OrchestrationTask) {
    this.orchestrationTasks.set(task.task_id, { ...task, dependency_ids: [...task.dependency_ids], artifact_refs: [...task.artifact_refs] })
    return this.orchestrationTasks.get(task.task_id)!
  }

  async listOrchestrationTasks(workspaceID: number, orchestrationID: string) {
    return [...this.orchestrationTasks.values()]
      .filter((task) => task.workspace_id === workspaceID && task.orchestration_id === orchestrationID)
      .sort((left, right) => left.task_seq - right.task_seq)
  }

  async getOrchestrationTask(workspaceID: number, taskID: string) {
    const task = this.orchestrationTasks.get(taskID)
    if (!task) return undefined
    if (workspaceID > 0 && task.workspace_id !== workspaceID) return undefined
    return task
  }

  async claimOrchestrationTask(input: { workspace_id: number, task_id: string, owner: string, claim_expires_at: string }) {
    const task = await this.getOrchestrationTask(input.workspace_id, input.task_id)
    if (!task) return undefined
    if (!['pending', 'queued'].includes(task.status)) return undefined
    const claimed: OrchestrationTask = {
      ...task,
      status: 'claimed',
      claim_owner: input.owner,
      claim_epoch: Number(task.claim_epoch || 0) + 1,
      claim_expires_at: input.claim_expires_at,
      updated_at: new Date().toISOString()
    }
    return this.saveOrchestrationTask(claimed)
  }

  async saveActionDecision(decision: ActionDecision) {
    this.actionDecisions.set(decision.decision_id, decision)
    return decision
  }

  async listActionDecisions(workspaceID: number, actionID: number) {
    return [...this.actionDecisions.values()]
      .filter((decision) => decision.workspace_id === workspaceID && decision.action_id === actionID)
      .sort((left, right) => left.decision_seq - right.decision_seq)
  }

  async saveSessionPermissionGrant(grant: SessionPermissionGrant) {
    this.sessionPermissionGrants.set(`${grant.workspace_id}:${grant.grant_id}`, grant)
    return grant
  }

  async listSessionPermissionGrants(workspaceID: number, sessionID: number) {
    return [...this.sessionPermissionGrants.values()]
      .filter((grant) => grant.workspace_id === workspaceID && grant.session_id === sessionID)
      .sort((left, right) => left.grant_seq - right.grant_seq)
  }

  async saveActionExecution(execution: ActionExecution) {
    this.actionExecutions.set(`${execution.workspace_id}:${execution.action_id}:${execution.attempt}`, execution)
    return execution
  }

  async getActionExecution(workspaceID: number, actionID: number, attempt: number) {
    return this.actionExecutions.get(`${workspaceID}:${actionID}:${attempt}`)
  }

  async listActionExecutions(workspaceID: number, actionID: number) {
    return [...this.actionExecutions.values()]
      .filter((execution) => execution.workspace_id === workspaceID && execution.action_id === actionID)
      .sort((left, right) => left.attempt - right.attempt)
  }

  async saveAgentWorkspace(workspace: AIRuntimeWorkspace) {
    this.agentWorkspaces.set(`${workspace.workspace_id}:${workspace.workspace_runtime_id}`, workspace)
    return workspace
  }

  async createOrGetDefaultAgentWorkspace(workspace: AIRuntimeWorkspace): Promise<AgentWorkspaceCreateResult> {
    const existing = [...this.agentWorkspaces.values()].find((item) =>
      item.workspace_id === workspace.workspace_id &&
      item.owner_user_id === workspace.owner_user_id &&
      item.workspace_key === workspace.workspace_key)
    if (existing) return { outcome: 'existing', workspace: structuredClone(existing) }
    const stored = structuredClone(workspace)
    this.agentWorkspaces.set(`${stored.workspace_id}:${stored.workspace_runtime_id}`, stored)
    return { outcome: 'created', workspace: structuredClone(stored) }
  }

  async claimAgentWorkspaceProvision(input: AgentWorkspaceProvisionClaimInput) {
    const key = `${input.workspace_id}:${input.workspace_runtime_id}`
    const workspace = this.agentWorkspaces.get(key)
    if (!workspace || workspace.owner_user_id !== input.owner_user_id) return undefined
    const active = workspace.provision_owner_instance_id && workspace.provision_expires_at && workspace.provision_expires_at > input.observed_at
    if (active) return undefined
    if (!['provisioning', 'failed', 'recycled'].includes(workspace.status)) return undefined
    const claimed: AIRuntimeWorkspace = {
      ...workspace,
      status: 'provisioning',
      provision_owner_instance_id: input.instance_id,
      provision_epoch: Number(workspace.provision_epoch || 0) + 1,
      provision_expires_at: input.lease_expires_at,
      state_version: Number(workspace.state_version || 0) + 1,
      error_code: undefined,
      error_msg: undefined,
      updated_at: input.observed_at
    }
    this.agentWorkspaces.set(key, claimed)
    return structuredClone(claimed)
  }

  async completeAgentWorkspaceProvision(input: AgentWorkspaceProvisionCompleteInput) {
    const key = `${input.workspace_id}:${input.workspace_runtime_id}`
    const workspace = this.agentWorkspaces.get(key)
    if (!workspace || workspace.status !== 'provisioning') return undefined
    if (workspace.provision_owner_instance_id !== input.instance_id || Number(workspace.provision_epoch || 0) !== input.provision_epoch) return undefined
    const ready: AIRuntimeWorkspace = {
      ...workspace,
      sandbox_id: input.sandbox_id,
      status: 'ready',
      provision_owner_instance_id: undefined,
      provision_expires_at: undefined,
      state_version: Number(workspace.state_version || 0) + 1,
      error_code: undefined,
      error_msg: undefined,
      updated_at: input.updated_at
    }
    this.agentWorkspaces.set(key, ready)
    return structuredClone(ready)
  }

  async failAgentWorkspaceProvision(input: AgentWorkspaceProvisionFailInput) {
    const key = `${input.workspace_id}:${input.workspace_runtime_id}`
    const workspace = this.agentWorkspaces.get(key)
    if (!workspace || workspace.status !== 'provisioning') return undefined
    if (workspace.provision_owner_instance_id !== input.instance_id || Number(workspace.provision_epoch || 0) !== input.provision_epoch) return undefined
    const failed: AIRuntimeWorkspace = {
      ...workspace,
      status: 'failed',
      provision_owner_instance_id: undefined,
      provision_expires_at: undefined,
      state_version: Number(workspace.state_version || 0) + 1,
      error_code: input.error_code,
      error_msg: input.error_msg,
      updated_at: input.updated_at
    }
    this.agentWorkspaces.set(key, failed)
    return structuredClone(failed)
  }

  async getAgentWorkspace(workspaceID: number, workspaceRuntimeID: string) {
    return this.agentWorkspaces.get(`${workspaceID}:${workspaceRuntimeID}`)
  }

  async listAgentWorkspaces(workspaceID: number, ownerUserID?: number) {
    return [...this.agentWorkspaces.values()]
      .filter((workspace) => workspace.workspace_id === workspaceID)
      .filter((workspace) => ownerUserID === undefined || workspace.owner_user_id === ownerUserID)
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at) || right.id - left.id)
  }

  async beginAgentWorkspaceRecycle(
    workspaceID: number,
    workspaceRuntimeID: string,
    ownerUserID: number,
    operationID: string,
    leaseExpiresAt: string,
    observedAt = new Date().toISOString()
  ) {
    const key = `${workspaceID}:${workspaceRuntimeID}`
    const workspace = this.agentWorkspaces.get(key)
    if (!workspace || workspace.owner_user_id !== ownerUserID) return undefined
    if (!['ready', 'paused', 'recycled', 'failed'].includes(workspace.status)) return undefined
    const leaseIsLive = Boolean(
      workspace.lease_owner_instance_id &&
      workspace.lease_expires_at &&
      workspace.lease_expires_at > observedAt
    )
    if (leaseIsLive) return undefined
    const claimed: AIRuntimeWorkspace = {
      ...workspace,
      status: 'recycling',
      lease_owner_instance_id: operationID,
      lease_epoch: Number(workspace.lease_epoch || 0) + 1,
      lease_expires_at: leaseExpiresAt,
      paused_at: undefined,
      error_code: undefined,
      error_msg: undefined,
      updated_at: observedAt
    }
    this.agentWorkspaces.set(key, claimed)
    return claimed
  }

  async claimAgentWorkspaceLease(
    workspaceID: number,
    workspaceRuntimeID: string,
    instanceID: string,
    leaseExpiresAt: string,
    observedAt = new Date().toISOString()
  ) {
    const workspace = await this.getAgentWorkspace(workspaceID, workspaceRuntimeID)
    if (!workspace || workspace.status !== 'ready') return undefined
    const leaseIsLive = Boolean(
      workspace.lease_owner_instance_id &&
      workspace.lease_expires_at &&
      workspace.lease_expires_at > observedAt
    )
    if (leaseIsLive && workspace.lease_owner_instance_id !== instanceID) return undefined
    const keepsLiveLease = leaseIsLive && workspace.lease_owner_instance_id === instanceID
    const claimed: AIRuntimeWorkspace = {
      ...workspace,
      lease_owner_instance_id: instanceID,
      lease_epoch: keepsLiveLease ? Math.max(1, Number(workspace.lease_epoch || 0)) : Number(workspace.lease_epoch || 0) + 1,
      lease_expires_at: leaseExpiresAt,
      last_connected_at: observedAt,
      updated_at: observedAt
    }
    this.agentWorkspaces.set(`${workspaceID}:${workspaceRuntimeID}`, claimed)
    return claimed
  }

  async renewAgentWorkspaceLease(
    workspaceID: number,
    workspaceRuntimeID: string,
    instanceID: string,
    leaseEpoch: number,
    leaseExpiresAt: string
  ) {
    const workspace = await this.getAgentWorkspace(workspaceID, workspaceRuntimeID)
    if (!workspace || workspace.status !== 'ready') return undefined
    if (workspace.lease_owner_instance_id !== instanceID || Number(workspace.lease_epoch || 0) !== leaseEpoch) return undefined
    const renewed = { ...workspace, lease_expires_at: leaseExpiresAt, updated_at: new Date().toISOString() }
    this.agentWorkspaces.set(`${workspaceID}:${workspaceRuntimeID}`, renewed)
    return renewed
  }

  async releaseAgentWorkspaceLease(workspaceID: number, workspaceRuntimeID: string, instanceID: string, leaseEpoch: number) {
    const workspace = await this.getAgentWorkspace(workspaceID, workspaceRuntimeID)
    if (!workspace || workspace.lease_owner_instance_id !== instanceID || Number(workspace.lease_epoch || 0) !== leaseEpoch) return false
    this.agentWorkspaces.set(`${workspaceID}:${workspaceRuntimeID}`, {
      ...workspace,
      lease_owner_instance_id: undefined,
      lease_expires_at: undefined,
      updated_at: new Date().toISOString()
    })
    return true
  }

  async appendAgentWorkspaceAudit(audit: AIRuntimeWorkspaceAudit) {
    this.agentWorkspaceAudits.set(`${audit.workspace_id}:${audit.audit_id}`, audit)
    return audit
  }

  async listAgentWorkspaceAudits(workspaceID: number, workspaceRuntimeID: string) {
    return [...this.agentWorkspaceAudits.values()]
      .filter((audit) => audit.workspace_id === workspaceID && audit.workspace_runtime_id === workspaceRuntimeID)
      .sort((left, right) => left.created_at.localeCompare(right.created_at) || left.audit_id.localeCompare(right.audit_id))
  }
}

export function createMemoryRuntimeStore(): RuntimeStore {
  return new MemoryRuntimeStore()
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function sessionQueueItemOrder(left: SessionQueueItem, right: SessionQueueItem) {
  const leftTerminal = ['cancelled', 'applied', 'failed'].includes(left.status)
  const rightTerminal = ['cancelled', 'applied', 'failed'].includes(right.status)
  if (leftTerminal !== rightTerminal) return leftTerminal ? 1 : -1
  return left.position - right.position || left.item_seq - right.item_seq
}

function cloneAction(action: AgentAction): AgentAction {
  return structuredClone(action)
}

function statusForDecision(decision: ActionDecision['decision']): AgentAction['status'] {
  switch (decision) {
    case 'approve_once':
    case 'approve_session':
      return 'approved'
    case 'reject':
    case 'steer':
      return 'rejected'
    case 'expired':
      return 'expired'
  }
}

function executorTypeForActionKind(actionKind: string): ActionExecution['executor_type'] {
  switch (actionKind) {
    case 'pipeline.trigger':
      return 'pipeline'
    case 'subagent.spawn':
      return 'subagent'
    default:
      return 'mcp'
  }
}

function isActiveRunStatus(status: AIRuntimeRun['status']) {
  return status === 'queued' || status === 'running' || status === 'awaiting_decision' || status === 'awaiting_input'
}

function actionExecutionBusinessID(action: AgentAction, attempt: number) {
  return `${action.action_id.replace(/^a_/, 'x_')}_${seq36(attempt)}`
}

function seq36(value: number) {
  return Math.max(0, Math.floor(value)).toString(36).padStart(6, '0')
}
