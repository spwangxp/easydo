import { mergeRuntimeEvents, stableStringify } from './aiRuntimeEvents.js'

const TERMINAL_ACTION_STATUSES = new Set([
  'approved',
  'executed',
  'completed',
  'failed',
  'rejected',
  'cancelled',
  'expired'
])
const APPROVED_ACTION_STATUSES = new Set(['approved', 'executing', 'executed', 'completed'])
const REJECTED_ACTION_STATUSES = new Set(['rejected', 'failed', 'cancelled', 'expired'])
const EXPLICIT_LLM_REQUEST_START_EVENTS = new Set([
  'model.call_started',
  'model.continuation_started',
  'provider.call_started',
  'provider.continuation_started'
])
const FALLBACK_LLM_REQUEST_START_EVENTS = new Set([
  'session.step.started'
])
const MODEL_TOKEN_USAGE_EVENTS = new Set([
  'model.call_completed',
  'model.continuation_completed',
  'provider.call_completed',
  'provider.continuation_completed'
])
const FALLBACK_TOKEN_USAGE_EVENTS = new Set([
  'session.step.ended'
])
const MODEL_STATE_EVENTS = new Set([
  'model.selected',
  'session.model.switched',
  'session.step.started',
  'model.request_prepared',
  'model.call_started',
  'model.continuation_started',
  'provider.call_started',
  'provider.continuation_started'
])

function normalizeAction(data) {
  const agentAction = data?.action || data?.agent_action || data
  const id = agentAction?.id || agentAction?.action_id || data?.action_id
  if (!id) return null
  const inputJson = agentAction.input_json || data?.input_json || {}
  const targetJson = agentAction.target_json || data?.target_json || {}
  const policyJson = agentAction.policy_json || data?.policy_json || {}
  const displayJson = agentAction.display_json || data?.display_json || {}
  return {
    id,
    action_id: agentAction.action_id || id,
    action_kind: agentAction.action_kind || data?.action_kind || '',
    capability_id: agentAction.capability_id || data?.capability_id || '',
    runtime_run_id: agentAction.runtime_run_id || data?.runtime_run_id || '',
    input_json: inputJson,
    target_json: targetJson,
    policy_json: policyJson,
    display_json: displayJson,
    result_json: agentAction.result_json || data?.result_json || {},
    status: agentAction.status || data?.status || 'awaiting_decision',
    decision: firstString(
      agentAction.decision,
      data?.decision,
      agentAction.result_json?.approval_decision?.decision,
      data?.result_json?.approval_decision?.decision
    ),
    pi_approval: Boolean(agentAction.pi_approval || data?.pi_approval)
  }
}

function normalizeReplayEvents(payload) {
  if (Array.isArray(payload?.data?.events)) return payload.data.events
  if (Array.isArray(payload?.events)) return payload.events
  if (Array.isArray(payload)) return payload
  return []
}

export function runtimeEventsFromReplayResponse(payload) {
  // Full run replays can contain thousands of streaming deltas. Collapse them
  // before Vue ever sees the array so session switch / approval UI stay responsive.
  return compactReplayDeltaEvents(normalizeReplayEvents(payload).map((event) => {
    const eventType = event.event_type || event.type || event.event || event.payload?.event_type
    const payloadJson = event.payload_json || event.payload || event.data || {}
    const displayJson = event.display_json || payloadJson.display_json || {}
    const normalizedPayload = {
      ...payloadJson,
      event_id: event.event_id || payloadJson.event_id,
      event_seq: event.event_seq || payloadJson.event_seq,
      runtime_run_id: event.runtime_run_id || payloadJson.runtime_run_id,
      action_id: event.action_id || payloadJson.action_id,
      display_json: displayJson
    }
    return {
      type: eventType,
      event: eventType,
      event_id: normalizedPayload.event_id,
      payload: normalizedPayload,
      data: normalizedPayload,
      display_json: displayJson,
      timestamp: event.created_at || event.timestamp
    }
  }).map(compactToolResultReplayEvent))
}

const REPLAY_DELTA_EVENTS = new Set([
  'session.reasoning.delta',
  'session.text.delta',
  'session.tool.input.delta',
  'answer_delta',
  'reasoning_delta'
])

function compactReplayDeltaEvents(events = []) {
  const compacted = []
  events.forEach((event) => {
    const type = String(event?.type || event?.event || '')
    if (!REPLAY_DELTA_EVENTS.has(type)) {
      compacted.push(event)
      return
    }
    const previous = compacted[compacted.length - 1]
    const previousType = String(previous?.type || previous?.event || '')
    if (!previous || previousType !== type || replayDeltaKey(previous) !== replayDeltaKey(event)) {
      compacted.push(event)
      return
    }
    const previousPayload = previous.payload || previous.data || {}
    const nextPayload = event.payload || event.data || {}
    const mergedPayload = {
      ...previousPayload,
      ...nextPayload,
      delta: `${String(previousPayload.delta || '')}${String(nextPayload.delta || '')}`
    }
    previous.payload = mergedPayload
    previous.data = mergedPayload
    previous.event_id = event.event_id || previous.event_id
    previous.timestamp = event.timestamp || previous.timestamp
  })
  return compacted
}

function replayDeltaKey(event = {}) {
  const payload = event.payload || event.data || {}
  return [
    payload.reasoning_id || '',
    payload.text_id || '',
    payload.tool_call_id || payload.call_id || payload.provider_tool_call_id || '',
    payload.assistant_message_id || ''
  ].join('|')
}

function compactToolResultReplayEvent(event = {}) {
  const type = String(event?.type || event?.event || '')
  if (!['session.tool.success', 'session.tool.failed', 'tool.executed', 'tool.failed', 'action.execution_succeeded', 'action.execution_failed'].includes(type)) {
    return event
  }
  const payload = { ...(event.payload || event.data || {}) }
  let changed = false
  for (const key of ['result', 'structured', 'output', 'content', 'data', 'tool_result', 'raw']) {
    if (!(key in payload)) continue
    const next = compactToolResultValue(payload[key])
    if (next !== payload[key]) {
      payload[key] = next
      changed = true
    }
  }
  if (!changed) return event
  return {
    ...event,
    payload,
    data: payload
  }
}

function compactToolResultValue(value) {
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

export function actionSemanticKey(agentAction = {}) {
  const stableID = actionStableIdentity(agentAction)
  if (stableID) return `id:${stableID}`
  return [
    agentAction.runtime_run_id || '',
    agentAction.action_kind || '',
    agentAction.capability_id || '',
    stableStringify(agentAction.target_json || {}),
    stableStringify(agentAction.input_json?.arguments || {})
  ].join('|')
}

function actionID(agentAction = {}) {
  return String(agentAction.action_id || agentAction.id || '').trim()
}

function actionStableIdentity(agentAction = {}) {
  const input = agentAction.input_json && typeof agentAction.input_json === 'object' ? agentAction.input_json : {}
  const display = agentAction.display_json && typeof agentAction.display_json === 'object' ? agentAction.display_json : {}
  const approval = display.approval_request && typeof display.approval_request === 'object' ? display.approval_request : {}
  return firstString(
    agentAction.action_id,
    agentAction.id,
    input.provider_tool_call_id,
    input.tool_call_id,
    input.call_id,
    approval.provider_tool_call_id,
    approval.tool_call_id,
    approval.call_id,
    approval.approval_id
  )
}

export function derivePendingActions(entries = []) {
  const byID = new Map()
  const bySemanticKey = new Map()
  const resolvedActions = actionResolutionsFromEntries(entries)
  entries.forEach((entry) => {
    normalizePiApprovals(entry).forEach((piApproval) => {
      addResolvedAction(byID, bySemanticKey, applyResolvedActionState(piApproval, resolvedActions))
    })
    const actions = Array.isArray(entry?.output?.agent_actions) ? entry.output.agent_actions : []
    actions.map(normalizeAction).filter(Boolean).forEach((action) => {
      addResolvedAction(byID, bySemanticKey, applyResolvedActionState(action, resolvedActions))
    })
    runtimePiApprovalsFromEntry(entry).forEach((action) => {
      addResolvedAction(byID, bySemanticKey, applyResolvedActionState(action, resolvedActions))
    })
  })
  const awaitingActions = [...bySemanticKey.values()].filter((action) => {
    const liveAction = byID.get(actionID(action)) || action
    return isActionAwaitingDecision(liveAction)
  })
  // Secondary dedup: for PI tool approvals with the same tool+args, keep only
  // the latest instance. This catches the case where an old permission.asked
  // from a previous entry wasn't properly resolved (e.g. permission.resolved
  // was in a transient entry not persisted during replay), preventing the same
  // approval card from appearing twice across entry boundaries.
  const piByContent = new Map()
  const nonPi = []
  for (const action of awaitingActions) {
    if (action.action_kind === 'pi.tool_approval' || action.pi_approval) {
      const toolName = firstString(
        action.capability_id,
        action.display_json?.approval_request?.tool_name
      )
      const args = action.input_json?.arguments || action.target_json || {}
      const contentKey = `${action.runtime_run_id}:${toolName}:${stableStringify(args)}`
      // Later iteration wins (entries are processed in chronological order,
      // so the last one with the same content key is the most recent).
      piByContent.set(contentKey, action)
    } else {
      nonPi.push(action)
    }
  }
  return [...nonPi, ...piByContent.values()]
}

function runtimePiApprovalsFromEntry(entry = {}) {
  const events = Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : []
  return events.map((event) => normalizePiApprovalEvent(event, entry)).filter(Boolean)
}

function normalizePiApprovalEvent(event = {}, entry = {}) {
  const eventName = runtimeEventName(event)
  const data = runtimeEventData(event)
  if (eventName === 'subagent.blocked_approval') {
    const approval = data.awaiting_approval && typeof data.awaiting_approval === 'object'
      ? data.awaiting_approval
      : {}
    return normalizePiApproval({
      runtime_run_id: firstString(data.child_runtime_run_id, approval.runtime_run_id),
      output: {
        awaiting_approval: {
          ...approval,
          runtime_run_id: firstString(data.child_runtime_run_id, approval.runtime_run_id)
        }
      }
    })
  }
  if (eventName !== 'permission.asked') return null
  const output = entry?.output || {}
  const runtimeRunId = firstString(data.runtime_run_id, entry.runtime_run_id, output.runtime_run_id, output.runtime_session_id)
  const approvalId = firstString(data.request_id, data.approval_id)
  const callId = firstString(data.call_id, data.tool_call_id, data.provider_tool_call_id)
  const toolName = firstString(data.tool_name, data.tool)
  // runtime_run_id is optional for live drafts; approval id/call/tool are required.
  if (!approvalId || !callId || !toolName) return null
  const input = data.input && typeof data.input === 'object' && !Array.isArray(data.input) ? data.input : {}
  return normalizePiApproval({
    runtime_run_id: runtimeRunId,
    output: {
      awaiting_approval: {
        approval_id: approvalId,
        call_id: callId,
        tool_name: toolName,
        reason: firstString(data.reason, data.message),
        input,
        runtime_run_id: runtimeRunId
      }
    }
  })
}

function addResolvedAction(byID, bySemanticKey, action) {
  const id = actionID(action)
  const semanticKey = actionSemanticKey(action)
  if (id) byID.set(id, action)
  bySemanticKey.set(semanticKey, action)
}

function actionResolutionsFromEntries(entries = []) {
  const resolutions = new Map()
  entries.forEach((entry) => {
    const events = Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : []
    events.forEach((event) => {
      const resolution = actionResolutionFromEvent(event)
      if (!resolution) return
      actionResolutionIds(event).forEach((id) => {
        resolutions.set(id, resolution)
      })
    })
    stalePiApprovalIdsFromEvents(events).forEach((id) => {
      resolutions.set(id, { status: 'completed', decision: 'bypassed' })
    })
  })
  return resolutions
}

function stalePiApprovalIdsFromEvents(events = []) {
  const staleIDs = new Set()
  const pendingEventsByID = new Map()
  let hasApprovalResolution = false
  events.forEach((event) => {
    const name = runtimeEventName(event)
    if (name === 'permission.asked') {
      const data = runtimeEventData(event)
      const approvalID = firstString(data.request_id, data.approval_id)
      const callID = firstString(data.call_id, data.tool_call_id, data.provider_tool_call_id)
      ;[approvalID, callID].filter(Boolean).forEach((id) => pendingEventsByID.set(id, event))
      return
    }
    if (name === 'permission.resolved') {
      const data = runtimeEventData(event)
      const approvalID = firstString(data.request_id, data.approval_id)
      const callID = firstString(data.call_id, data.tool_call_id, data.provider_tool_call_id)
      ;[approvalID, callID].filter(Boolean).forEach((id) => pendingEventsByID.delete(id))
      hasApprovalResolution = true
      return
    }
    if (!pendingEventsByID.size || !isPiApprovalBypassBoundaryEvent(event, hasApprovalResolution)) return
    pendingEventsByID.forEach((pendingEvent) => {
      actionResolutionIds(pendingEvent).forEach((id) => staleIDs.add(id))
    })
    pendingEventsByID.clear()
  })
  return staleIDs
}

function isPiApprovalBypassBoundaryEvent(event = {}, hasApprovalResolution = false) {
  const name = runtimeEventName(event)
  if (['session.step.started', 'model.call_started', 'model.continuation_started', 'provider.call_started', 'provider.continuation_started'].includes(name)) return true
  if (!hasApprovalResolution) return false
  if (['session.tool.called', 'session.tool.success', 'tool.executed', 'action.execution_started', 'action.execution_succeeded'].includes(name)) return true
  if (name === 'session.tool.failed' || name === 'tool.failed' || name === 'action.execution_failed') {
    return !isApprovalRequiredRuntimeFailure(event)
  }
  return false
}

function isApprovalRequiredRuntimeFailure(event = {}) {
  const data = runtimeEventData(event)
  const error = data.error && typeof data.error === 'object' && !Array.isArray(data.error) ? data.error : {}
  const message = firstString(data.message, error.message, typeof data.error === 'string' ? data.error : '')
  return /requires approval|需要.*审批|需要.*确认/i.test(message)
}

function actionResolutionFromEvent(event) {
  const name = runtimeEventName(event)
  const data = runtimeEventData(event)
  if (name === 'subagent.completed') return { status: 'completed', decision: 'approve_once' }
  if (name === 'subagent.failed') return { status: 'failed', decision: 'approve_once' }
  if (name === 'subagent.cancelled' || name === 'subagent.interrupted' || name === 'subagent.timeout') {
    return { status: 'cancelled', decision: 'reject' }
  }
  if (name === 'permission.resolved') {
    const decision = firstString(data.decision, data.result)
    return {
      status: isRejectDecision(decision) ? 'rejected' : 'approved',
      decision: normalizeApprovalDecision(decision)
    }
  }
  if (name === 'action.approved' || name === 'approval.session_granted') {
    return { status: 'approved', decision: normalizeApprovalDecision(firstString(data.decision, 'approve_once')) }
  }
  if (name === 'action.rejected' || name === 'tool.rejected') {
    return { status: 'rejected', decision: normalizeApprovalDecision(firstString(data.decision, 'reject')) }
  }
  if (name === 'action.decision') {
    const decision = firstString(data.decision, data.result)
    return {
      status: isRejectDecision(decision) ? 'rejected' : 'approved',
      decision: normalizeApprovalDecision(decision)
    }
  }
  if (name === 'action.execution_started') return { status: 'executing', decision: 'approve_once' }
  if (name === 'action.execution_succeeded' || name === 'tool.executed') return { status: 'executed', decision: 'approve_once' }
  if (name === 'action.execution_failed' || name === 'tool.failed') return { status: 'failed', decision: 'approve_once' }
  return null
}

function actionResolutionIds(event) {
  const data = runtimeEventData(event)
  const action = data.action && typeof data.action === 'object' ? data.action : {}
  const actionInput = action.input_json && typeof action.input_json === 'object' ? action.input_json : {}
  const approval = data.approval_request && typeof data.approval_request === 'object'
    ? data.approval_request
    : action.display_json?.approval_request && typeof action.display_json.approval_request === 'object'
      ? action.display_json.approval_request
      : {}
  return [...new Set([
    data.action_id,
    data.id,
    action.action_id,
    action.id,
    data.request_id,
    data.approval_id,
    approval.action_id,
    approval.approval_id,
    data.provider_tool_call_id,
    data.call_id,
    data.tool_call_id,
    data.child_runtime_run_id,
    data.child_run_link_id,
    actionInput.provider_tool_call_id,
    actionInput.tool_call_id,
    actionInput.call_id,
    approval.provider_tool_call_id,
    approval.tool_call_id,
    approval.call_id
  ].map((value) => String(value || '').trim()).filter(Boolean))]
}

function applyResolvedActionState(action, resolutions) {
  const resolution = [
    resolutions.get(String(action.runtime_run_id || '').trim()),
    ...actionResolutionIds({ data: action })
    .map((id) => resolutions.get(id))
  ].find(Boolean)
  if (!resolution) return action
  return {
    ...action,
    status: resolution.status || action.status,
    decision: resolution.decision || action.decision
  }
}

function normalizePiApproval(entry = {}) {
  const output = entry?.output || {}
  const approval = output.awaiting_approval || output.awaitingApproval
  return normalizePiApprovalRecord(entry, approval)
}

function normalizePiApprovals(entry = {}) {
  const output = entry?.output || {}
  const approvals = Array.isArray(output.awaiting_approvals)
    ? output.awaiting_approvals
    : Array.isArray(output.awaitingApprovals)
      ? output.awaitingApprovals
      : []
  const normalized = approvals.map((approval) => normalizePiApprovalRecord(entry, approval)).filter(Boolean)
  if (normalized.length > 0) return normalized
  const single = normalizePiApproval(entry)
  return single ? [single] : []
}

function normalizePiApprovalRecord(entry = {}, approval = null) {
  const output = entry?.output || {}
  if (!approval || typeof approval !== 'object') return null
  // Live SSE drafts may receive permission.asked before runtime_run_id is known.
  // Still surface the approval panel; continuePiApproval will no-op until the run id arrives.
  const runtimeRunId = firstString(
    entry.runtime_run_id,
    output.runtime_run_id,
    output.runtime_session_id,
    approval.runtime_run_id
  )
  const callId = approval.call_id || approval.callID || approval.tool_call_id || approval.provider_tool_call_id
  const toolName = approval.tool_name || approval.toolName || approval.tool || 'tool'
  const approvalId = approval.approval_id || approval.request_id || `approval:${callId || runtimeRunId || 'pending'}`
  if (!approvalId || !callId || !toolName) return null
  const input = approval.input && typeof approval.input === 'object' ? approval.input : {}
  return {
    id: approvalId,
    action_id: approvalId,
    action_kind: 'pi.tool_approval',
    capability_id: toolName,
    runtime_run_id: runtimeRunId,
    input_json: {
      provider_tool_call_id: callId,
      tool_name: toolName,
      arguments: input
    },
    target_json: {
      target_type: 'tool',
      target_id: toolName
    },
    policy_json: {
      requires_decision: true,
      risk_summary: approval.reason || '',
      operation_type: input.operation_type || ''
    },
    display_json: {
      title: `需要确认 ${toolName}`,
      name: toolName,
      summary: approval.reason || '',
      approval_request: {
        approval_id: approvalId,
        provider_tool_call_id: callId,
        tool_name: toolName,
        reason: approval.reason || '',
        input_preview: { tool_name: toolName, arguments: input }
      }
    },
    result_json: {},
    status: 'awaiting_decision',
    decision: '',
    pi_approval: true
  }
}

export function mergeReplayEventsIntoEntries(entryList = [], runtimeRunId, replayEvents = []) {
  if (!runtimeRunId || !replayEvents.length) return entryList
  const target = [...entryList]
    .reverse()
    .find((entry) => entry.runtime_run_id === runtimeRunId && entry.role === 'assistant')
  if (!target) return entryList
  target.output = target.output || {}
  const existingEvents = Array.isArray(target.output.runtime_events) ? target.output.runtime_events : []
  target.output.runtime_events = mergeRuntimeEvents(existingEvents, replayEvents)
  const existingActions = Array.isArray(target.output.agent_actions) ? target.output.agent_actions : []
  const existingByID = new Map(existingActions.map((item) => [actionID(item), item]).filter(([id]) => id))
  const replayActions = replayEvents
    .filter((event) => event.type === 'action.decision_required')
    .map((event) => normalizeAction(event.data || event.payload || {}))
    .filter(Boolean)
    .filter((action) => {
      const existing = existingByID.get(actionID(action))
      return !existing || !TERMINAL_ACTION_STATUSES.has(existing.status)
    })
  if (replayActions.length > 0) {
    const mergedByID = new Map(existingActions.map((item) => [actionID(item), item]).filter(([id]) => id))
    replayActions.forEach((action) => mergedByID.set(actionID(action), action))
    target.output.agent_actions = [...mergedByID.values()]
  }
  return entryList
}

export function mergeReplayEventsIntoFirstAssistantEntry(entryList = [], runtimeRunId, replayEvents = []) {
  if (!runtimeRunId || !replayEvents.length) return entryList
  const target = entryList.find((entry) => entry.runtime_run_id === runtimeRunId && entry.role === 'assistant')
  if (!target) return entryList
  target.output = target.output || {}
  const existingEvents = Array.isArray(target.output.runtime_events) ? target.output.runtime_events : []
  target.output.runtime_events = mergeRuntimeEvents(existingEvents, replayEvents)
  const existingActions = Array.isArray(target.output.agent_actions) ? target.output.agent_actions : []
  const existingByID = new Map(existingActions.map((item) => [actionID(item), item]).filter(([id]) => id))
  const replayActions = replayEvents
    .filter((event) => event.type === 'action.decision_required')
    .map((event) => normalizeAction(event.data || event.payload || {}))
    .filter(Boolean)
    .filter((action) => {
      const existing = existingByID.get(actionID(action))
      return !existing || !TERMINAL_ACTION_STATUSES.has(existing.status)
    })
  if (replayActions.length > 0) {
    const mergedByID = new Map(existingActions.map((item) => [actionID(item), item]).filter(([id]) => id))
    replayActions.forEach((action) => mergedByID.set(actionID(action), action))
    target.output.agent_actions = [...mergedByID.values()]
  }
  return entryList
}

export function entryNeedsRuntimeEventReplay(entry) {
  if (!entry || entry.role !== 'assistant') return false
  const runtimeRunId = firstString(entry.runtime_run_id, entry.output?.runtime_run_id)
  if (!runtimeRunId) return false
  const status = firstString(entry.status)
  if (['completed', 'failed', 'cancelled', 'rejected', 'expired'].includes(status)) return false
  return ['streaming', 'running', 'awaiting_decision', 'awaiting_input', 'queued'].includes(status)
    || Boolean(entry.output?.awaiting_approval)
    || (Array.isArray(entry.output?.awaiting_approvals) && entry.output.awaiting_approvals.length > 0)
}

export function activeRuntimeRunFromState(session = {}, entries = []) {
  const sessionRun = session?.active_run && typeof session.active_run === 'object' ? session.active_run : {}
  if (firstString(sessionRun.runtime_run_id)) return sessionRun

  for (let index = entries.length - 1; index >= 0; index -= 1) {
    const entry = entries[index]
    if (!entryNeedsRuntimeEventReplay(entry)) continue
    return {
      runtime_run_id: firstString(entry.runtime_run_id, entry.output?.runtime_run_id),
      status: firstString(entry.output?.awaiting_approval ? 'awaiting_decision' : '', entry.status, 'running'),
      output_entry_id: entry.id,
      started_at: entry.created_at,
      updated_at: entry.updated_at
    }
  }
  return null
}

export function runtimeEventKey(event) {
  const eventId = event?.event_id || event?.payload?.event_id || event?.data?.event_id
  if (eventId) return `id:${eventId}`
  const type = event?.event || event?.type || event?.payload?.event_type || 'event'
  const seq = event?.payload?.event_seq || event?.data?.event_seq
  if (seq) return `seq:${type}:${seq}`
  return `${type}:${JSON.stringify(event?.payload || event?.data || event)}`
}

export function displayRuntimeEventsForEntry(entry, allEntries = []) {
  const events = Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : []
  if (!entry?.runtime_run_id || entry.role !== 'assistant') return events
  const sameRunAssistants = allEntries.filter((item) => item.role === 'assistant' && item.runtime_run_id === entry.runtime_run_id)
  if (sameRunAssistants.length <= 1) return events
  const latestAssistant = sameRunAssistants[sameRunAssistants.length - 1]
  const entryKey = String(entry.id || entry.idempotency_key || '')
  const latestKey = String(latestAssistant.id || latestAssistant.idempotency_key || '')
  if (entryKey !== latestKey) return []
  const previousEvents = sameRunAssistants
    .slice(0, -1)
    .flatMap((item) => Array.isArray(item?.output?.runtime_events) ? item.output.runtime_events : [])
  const previousKeys = new Set(previousEvents.map(runtimeEventKey))
  const incrementalEvents = events.filter((event) => !previousKeys.has(runtimeEventKey(event)))
  return incrementalEvents.length ? events : []
}

export function agentRunMetricsForEntry(entry, allEntries = []) {
  if (entry?.role !== 'assistant') return []
  const events = displayRuntimeEventsForEntry(entry, allEntries)
  const output = entry?.output || {}
  const timings = output.timings || {}
  const tokenUsage = tokenUsageForEntry(events, output)
  const llmRequests = countLlmRequests(events) || (entry.content || output.text ? 1 : 0)
  const toolCalls = countUniqueToolCalls(events)
  const skills = countUniqueSkills(events, output)
  // Prefer Runtime-issued active duration (P2-03 scheme B); fall back to local algorithm.
  const durationMetric = resolveDurationMetric(timings, events)
  return [
    { key: 'tokens', label: 'Token', value: formatTokenUsage(tokenUsage) },
    { key: 'llm_requests', label: 'LLM 请求', value: `${llmRequests}次` },
    { key: 'tool_calls', label: 'Tool 调用', value: `${toolCalls}次` },
    { key: 'skills', label: 'Skill', value: `${skills}个` },
    durationMetric
  ]
}

function isNumericIdentifier(value) {
  return /^[0-9]+$/.test(String(value || '').trim())
}

function providerMatchesSnapshot(provider = {}, snapshot = {}) {
  const providerId = firstString(snapshot.provider_id, snapshot.id)
  const providerType = firstString(snapshot.provider_type, snapshot.type, !isNumericIdentifier(providerId) ? providerId : '')
  const baseUrl = firstString(snapshot.base_url, snapshot.baseUrl, snapshot.api_base_url, snapshot.apiBaseUrl)
  return Boolean(
    (providerId && [provider.id, provider.provider_id].some((value) => String(value || '').trim() === providerId)) ||
    (baseUrl && firstString(provider.base_url, provider.baseUrl, provider.api_base_url, provider.apiBaseUrl) === baseUrl) ||
    (providerType && firstString(provider.provider_type, provider.type) === providerType)
  )
}

export function resolveProviderDisplayName(provider = {}, providers = []) {
  const snapshot = provider && typeof provider === 'object' && !Array.isArray(provider) ? provider : { provider_id: provider }
  const directName = firstString(snapshot.display_name, snapshot.displayName)
  if (directName) return directName
  const namedValue = firstString(snapshot.name)
  if (namedValue && !isNumericIdentifier(namedValue)) return namedValue
  const matchedProvider = providers.find((item) => providerMatchesSnapshot(item, snapshot)) || {}
  const matchedName = firstString(matchedProvider.display_name, matchedProvider.displayName, matchedProvider.name)
  if (matchedName) return matchedName
  const rawProviderID = firstString(snapshot.provider_id, snapshot.id)
  const providerType = firstString(snapshot.provider_type, snapshot.type, !isNumericIdentifier(rawProviderID) ? rawProviderID : '')
  if (providerType) return providerType
  return firstString(rawProviderID, '-')
}

export function runtimeModelSnapshotFromEvent(event = {}) {
  const name = runtimeEventName(event)
  if (!MODEL_STATE_EVENTS.has(name)) return {}
  const data = runtimeEventData(event)
  const nestedModel = data.model && typeof data.model === 'object' && !Array.isArray(data.model) ? data.model : {}
  const modelObject = Object.keys(nestedModel).length > 0 || name !== 'model.selected' ? nestedModel : data
  const modelText = firstString(
    modelObject.provider_model_key,
    modelObject.model,
    modelObject.id,
    modelObject.model_id,
    data.provider_model_key,
    data.model_id,
    typeof data.model === 'string' ? data.model : '',
    modelObject.name
  )
  const providerID = firstString(
    modelObject.provider_id,
    modelObject.provider,
    data.provider_id,
    data.provider,
    modelObject.provider_type,
    data.provider_type
  )
  const snapshot = {
    ...modelObject
  }
  if (modelText) {
    snapshot.id = firstString(snapshot.id, snapshot.model_id, modelText)
    snapshot.model = firstString(snapshot.model, modelText)
    snapshot.provider_model_key = firstString(snapshot.provider_model_key, modelText)
  }
  if (providerID) snapshot.provider_id = firstString(snapshot.provider_id, providerID)
  const providerType = firstString(modelObject.provider_type, data.provider_type, !isNumericIdentifier(providerID) ? providerID : '')
  if (providerType) snapshot.provider_type = firstString(snapshot.provider_type, providerType)
  const baseUrl = firstString(modelObject.base_url, modelObject.baseUrl, data.base_url, data.baseUrl, data.api_base_url, data.apiBaseUrl)
  if (baseUrl) snapshot.base_url = baseUrl
  const contextWindow = numberValue(
    modelObject.context_window,
    modelObject.contextWindow,
    modelObject.context_tokens,
    data.context_window,
    data.contextWindow,
    data.context_tokens
  )
  if (contextWindow) snapshot.context_window = contextWindow
  const maxTokens = numberValue(
    modelObject.max_tokens,
    modelObject.maxTokens,
    modelObject.max_output_tokens,
    data.max_tokens,
    data.maxTokens,
    data.max_output_tokens
  )
  if (maxTokens) snapshot.max_tokens = maxTokens
  const thinkingLevel = firstString(
    modelObject.thinking_level,
    modelObject.inference?.thinking_level,
    modelObject.inference?.reasoning_effort,
    data.thinking_level,
    data.inference?.thinking_level,
    data.inference?.reasoning_effort
  )
  if (thinkingLevel) snapshot.thinking_level = thinkingLevel
  const inference = {
    ...(modelObject.inference && typeof modelObject.inference === 'object' && !Array.isArray(modelObject.inference) ? modelObject.inference : {}),
    ...(data.inference && typeof data.inference === 'object' && !Array.isArray(data.inference) ? data.inference : {})
  }
  if (thinkingLevel) inference.thinking_level = thinkingLevel
  if (Object.keys(inference).length > 0) snapshot.inference = inference
  return Object.keys(snapshot).length > 0 ? snapshot : {}
}

export function mergeRuntimeModelSnapshots(current = {}, next = {}) {
  const currentSnapshot = current && typeof current === 'object' && !Array.isArray(current) ? current : {}
  const nextSnapshot = next && typeof next === 'object' && !Array.isArray(next) ? next : {}
  if (Object.keys(nextSnapshot).length === 0) return currentSnapshot
  const currentProvider = firstString(currentSnapshot.provider_id, currentSnapshot.provider, currentSnapshot.provider_type)
  const nextProvider = firstString(nextSnapshot.provider_id, nextSnapshot.provider, nextSnapshot.provider_type)
  const sameProvider = !currentProvider || !nextProvider || currentProvider === nextProvider
  const base = sameProvider ? currentSnapshot : {}
  const inference = {
    ...(base.inference && typeof base.inference === 'object' && !Array.isArray(base.inference) ? base.inference : {}),
    ...(nextSnapshot.inference && typeof nextSnapshot.inference === 'object' && !Array.isArray(nextSnapshot.inference) ? nextSnapshot.inference : {})
  }
  const merged = {
    ...base,
    ...nextSnapshot
  }
  if (Object.keys(inference).length > 0) merged.inference = inference
  return merged
}

export function latestRuntimeModelFromEvents(events = []) {
  return events.reduce((snapshot, event) => {
    return mergeRuntimeModelSnapshots(snapshot, runtimeModelSnapshotFromEvent(event))
  }, {})
}

function countEventsByName(events, names) {
  return events.filter((event) => names.has(runtimeEventName(event))).length
}

function countLlmRequests(events) {
  const explicit = countUniqueEvents(events, EXPLICIT_LLM_REQUEST_START_EVENTS, modelRequestKey)
  if (explicit > 0) return explicit
  return countUniqueEvents(events, FALLBACK_LLM_REQUEST_START_EVENTS, modelRequestKey)
}

function countUniqueEvents(events, names, keyFactory) {
  const keys = new Set()
  events.forEach((event, index) => {
    if (!names.has(runtimeEventName(event))) return
    keys.add(keyFactory(event, index))
  })
  return keys.size
}

function modelRequestKey(event, index) {
  const data = runtimeEventData(event)
  const eventId = firstString(data.request_id, data.call_id, data.event_id)
  const phase = firstString(data.phase, data.display_json?.phase)
  const round = firstString(data.round, data.display_json?.round)
  const model = firstString(data.model, data.provider_model_key)
  if (eventId) return `id:${eventId}`
  if (phase || round) return ['model', phase || 'phase', round || 'round', model].join(':')
  return `${runtimeEventName(event)}:${index}`
}

function countUniqueToolCalls(events) {
  // Count actual tool invocations only. Success/failed/detected events are
  // lifecycle mirrors of the same call and must not inflate the footer metric.
  const ids = new Set()
  events.forEach((event, index) => {
    const name = runtimeEventName(event)
    if (!['session.tool.called', 'action.execution_started', 'tool.executed'].includes(name)) return
    const data = runtimeEventData(event)
    const id = data.call_id || data.tool_call_id || data.provider_tool_call_id || data.action_id || `${data.tool_name || data.tool || name}:${index}`
    ids.add(String(id))
  })
  return ids.size
}

function countUniqueSkills(events, output = {}) {
  // Skill catalogs (available / pi_resources) are capability directories, not usage.
  // Only count skills that were actually loaded or used during the run.
  const ids = new Set()
  events.forEach((event, index) => {
    const name = runtimeEventName(event)
    if (!['skill.used', 'skill.loaded', 'skill.invoked'].includes(name)) return
    const data = runtimeEventData(event)
    ids.add(String(data.name || data.skill_name || data.skill || `${name}:${index}`))
  })
  const loadedSkills = Array.isArray(output.loaded_skills) ? output.loaded_skills : []
  loadedSkills.forEach((skill, index) => {
    const data = runtimeEventData(skill)
    ids.add(String(data.name || data.skill_name || data.skill || `loaded_skills:${index}`))
  })
  return ids.size
}

function tokenTotalFromEvent(event) {
  const data = runtimeEventData(event)
  return tokenTotalFromValue(data.tokens) || tokenTotalFromValue(data.usage)
}

function tokenUsageForEntry(events, output = {}) {
  const explicitUsage = tokenUsageFromNamedEvents(events, MODEL_TOKEN_USAGE_EVENTS, modelRequestKey)
  if (explicitUsage.total > 0) return explicitUsage
  const secondaryUsage = tokenUsageFromNamedEvents(events, FALLBACK_TOKEN_USAGE_EVENTS)
  if (secondaryUsage.total > 0) return secondaryUsage
  return tokenUsageFromValue(output.usage)
}

function tokenUsageFromNamedEvents(events, names, keyFactory = null) {
  const usage = zeroTokenUsage()
  const seenKeys = new Set()
  events.forEach((event, index) => {
    if (!names.has(runtimeEventName(event))) return
    const key = keyFactory
      ? keyFactory(event, index)
      : firstString(runtimeEventData(event).event_id, event?.event_id, `${runtimeEventName(event)}:${index}`)
    if (seenKeys.has(key)) return
    seenKeys.add(key)
    addTokenUsage(usage, tokenUsageFromEvent(event))
  })
  return usage
}

function tokenUsageFromEvent(event) {
  const data = runtimeEventData(event)
  const tokens = tokenUsageFromValue(data.tokens)
  if (tokens.total > 0) return tokens
  return tokenUsageFromValue(data.usage)
}

function tokenTotalFromValue(value) {
  return tokenUsageFromValue(value).total
}

function tokenUsageFromValue(value) {
  const usage = zeroTokenUsage()
  if (!value || typeof value !== 'object') return usage
  const cache = value.cache && typeof value.cache === 'object' ? value.cache : {}
  const total = numberValue(value.total_tokens, value.totalTokens, value.total)
  usage.input = numberValue(value.input, value.input_tokens, value.prompt_tokens)
  usage.output = numberValue(value.output, value.output_tokens, value.completion_tokens)
  usage.reasoning = numberValue(value.reasoning, value.reasoning_tokens, value.reasoning_output_tokens)
  usage.cacheRead = numberValue(cache.read, value.cacheRead, value.cache_read, value.cache_read_tokens, value.cached_tokens)
  usage.cacheWrite = numberValue(cache.write, value.cacheWrite, value.cache_write, value.cache_write_tokens)
  usage.total = total || usage.input + usage.output + usage.reasoning + usage.cacheRead + usage.cacheWrite
  return usage
}

function zeroTokenUsage() {
  return { input: 0, output: 0, reasoning: 0, cacheRead: 0, cacheWrite: 0, total: 0 }
}

function addTokenUsage(target, source) {
  if (!source || typeof source !== 'object') return target
  target.input += numberValue(source.input)
  target.output += numberValue(source.output)
  target.reasoning += numberValue(source.reasoning)
  target.cacheRead += numberValue(source.cacheRead)
  target.cacheWrite += numberValue(source.cacheWrite)
  target.total += numberValue(source.total)
  return target
}

function formatTokenUsage(usage) {
  if (usage.input > 0 || usage.output > 0) {
    const reasoning = usage.reasoning > 0 ? ` / 思考 ${formatInteger(usage.reasoning)}` : ''
    const cache = usage.cacheRead > 0 ? ` / 缓存 ${formatInteger(usage.cacheRead)}` : ''
    return `输入 ${formatInteger(usage.input)} / 输出 ${formatInteger(usage.output)}${reasoning}${cache}`
  }
  return usage.total > 0 ? formatInteger(usage.total) : '-'
}

const AGENT_ACTIVE_DURATION_ALGORITHM = 'agent_active_duration_v2'
const RUN_TERMINAL_EVENTS = new Set([
  'run.completed',
  'run.failed',
  'run.cancelled',
  'run.timeout',
  'run.interrupted'
])

function resolveDurationMetric(timings = {}, events = []) {
  const runtimeActive = numberValue(timings.active_duration_ms)
  if (runtimeActive > 0 || timings.metric_quality || timings.algorithm_version) {
    const quality = firstString(timings.metric_quality) === 'exact' ? 'exact' : (firstString(timings.metric_quality) || 'degraded')
    const valueMs = runtimeActive > 0
      ? runtimeActive
      : numberValue(timings.total_ms, timings.total, timings.completed_ms, timings.completed)
    return {
      key: 'duration',
      label: '耗时',
      value: valueMs > 0 ? formatDuration(valueMs) : '-',
      quality: quality === 'exact' ? 'exact' : 'degraded',
      algorithm_version: firstString(timings.algorithm_version) || AGENT_ACTIVE_DURATION_ALGORITHM
    }
  }

  const computed = computeActiveAgentDuration(events)
  if (computed.active_duration_ms > 0) {
    return {
      key: 'duration',
      label: '耗时',
      value: formatDuration(computed.active_duration_ms),
      quality: computed.metric_quality,
      algorithm_version: AGENT_ACTIVE_DURATION_ALGORITHM
    }
  }

  const wall = numberValue(timings.total_ms, timings.total, timings.completed_ms, timings.completed)
  if (wall > 0) {
    return {
      key: 'duration',
      label: '耗时',
      value: formatDuration(wall),
      quality: 'degraded',
      algorithm_version: AGENT_ACTIVE_DURATION_ALGORITHM
    }
  }

  const timestamps = events
    .map((event) => Date.parse(runtimeEventData(event).timestamp || event?.timestamp || ''))
    .filter((value) => Number.isFinite(value))
  if (timestamps.length >= 2) {
    return {
      key: 'duration',
      label: '耗时',
      value: formatDuration(Math.max(...timestamps) - Math.min(...timestamps)),
      quality: 'degraded',
      algorithm_version: AGENT_ACTIVE_DURATION_ALGORITHM
    }
  }

  return {
    key: 'duration',
    label: '耗时',
    value: '-',
    quality: 'degraded',
    algorithm_version: AGENT_ACTIVE_DURATION_ALGORITHM
  }
}

function eventTimestampMs(event = {}) {
  const value = Date.parse(runtimeEventData(event).timestamp || event?.timestamp || event?.created_at || '')
  return Number.isFinite(value) ? value : NaN
}

function computeActiveAgentDuration(events = []) {
  const stepWindows = []
  let openStepStart = NaN
  let quality = 'exact'

  events.forEach((event) => {
    const name = runtimeEventName(event)
    const ts = eventTimestampMs(event)
    if (!Number.isFinite(ts)) return
    if (name === 'session.step.started') {
      openStepStart = ts
      return
    }
    if ((name === 'session.step.ended' || name === 'session.step.failed') && Number.isFinite(openStepStart)) {
      stepWindows.push([openStepStart, ts])
      if (name === 'session.step.failed') quality = 'degraded'
      openStepStart = NaN
      return
    }
    if (RUN_TERMINAL_EVENTS.has(name) && Number.isFinite(openStepStart)) {
      stepWindows.push([openStepStart, ts])
      quality = 'degraded'
      openStepStart = NaN
    }
  })

  if (stepWindows.length === 0) {
    return { active_duration_ms: 0, metric_quality: 'degraded' }
  }

  const pauseWindows = []
  const openAsks = new Map()
  events.forEach((event) => {
    const name = runtimeEventName(event)
    const ts = eventTimestampMs(event)
    if (!Number.isFinite(ts)) return
    const data = runtimeEventData(event)
    const callId = firstString(data.call_id, data.tool_call_id, data.provider_tool_call_id, data.request_id, data.approval_id)
    if (name === 'permission.asked' && callId && !openAsks.has(callId)) {
      openAsks.set(callId, ts)
      return
    }
    if (name === 'permission.resolved' && callId && openAsks.has(callId)) {
      pauseWindows.push([openAsks.get(callId), ts])
      openAsks.delete(callId)
    }
  })

  let total = 0
  stepWindows.forEach(([start, end]) => {
    let active = Math.max(0, end - start)
    pauseWindows.forEach(([pauseStart, pauseEnd]) => {
      const overlapStart = Math.max(start, pauseStart)
      const overlapEnd = Math.min(end, pauseEnd)
      if (overlapEnd > overlapStart) active -= (overlapEnd - overlapStart)
    })
    total += Math.max(0, active)
  })
  return {
    active_duration_ms: Math.max(0, Math.round(total)),
    metric_quality: quality
  }
}

function formatDuration(value) {
  if (value < 1000) return `${Math.max(0, Math.round(value))}ms`
  return `${(value / 1000).toFixed(1)}s`
}

function formatInteger(value) {
  const number = Number(value)
  if (!Number.isFinite(number)) return '0'
  return String(Math.max(0, Math.round(number)))
}

function numberValue(...values) {
  for (const value of values) {
    const number = Number(value)
    if (Number.isFinite(number) && number > 0) return number
  }
  return 0
}

function firstString(...values) {
  for (const value of values) {
    if (value !== undefined && value !== null && String(value).trim()) return String(value).trim()
  }
  return ''
}

function isRejectDecision(decision) {
  return ['reject', 'rejected', 'steer', 'expired'].includes(String(decision || '').trim().toLowerCase())
}

function normalizeApprovalDecision(decision) {
  const normalized = String(decision || '').trim().toLowerCase()
  if (normalized === 'approve_session') return 'approve_session'
  if (normalized === 'reject' || normalized === 'rejected' || normalized === 'steer') return 'reject'
  if (normalized === 'approved' || normalized === 'approve_once') return 'approve_once'
  return normalized || ''
}

export function isActionAwaitingDecision(action = {}) {
  return String(action.status || '').trim() === 'awaiting_decision'
}

export function approvalDecisionForAction(action = {}) {
  const explicit = normalizeApprovalDecision(firstString(
    action.decision,
    action.result_json?.approval_decision?.decision,
    action.output?.decision
  ))
  if (explicit) return explicit
  const status = String(action.status || '').trim()
  if (REJECTED_ACTION_STATUSES.has(status)) return 'reject'
  if (APPROVED_ACTION_STATUSES.has(status)) return 'approve_once'
  return ''
}

export function isApprovalDecisionButtonSelected(action = {}, decision) {
  if (isActionAwaitingDecision(action)) return false
  const selected = approvalDecisionForAction(action)
  if (decision === 'reject') return selected === 'reject'
  if (decision === 'approve_session') return selected === 'approve_session'
  return selected === 'approve_once' || (selected && selected !== 'approve_session' && selected !== 'reject')
}

function runtimeEventName(event = {}) {
  return event?.type || event?.event || event?.event_type || event?.payload?.event_type || event?.data?.event_type || ''
}

function runtimeEventData(event = {}) {
  const data = event?.payload || event?.data || event?.payload_json || {}
  return data && typeof data === 'object' && !Array.isArray(data) ? { ...event, ...data } : { ...event }
}

export { normalizeAction }
