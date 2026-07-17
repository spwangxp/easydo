const STREAM_CONTROL_EVENTS = new Set([
  'user_entry',
  'assistant_entry',
  'tool_entry',
  'action_entry',
  'answer_delta',
  'reasoning_delta',
  'done',
  'error'
])

const OPENCODE_PROCESS_EVENTS = [
  'session.prompted',
  'session.step.started',
  'session.step.ended',
  'session.step.failed',
  'session.reasoning.started',
  'session.reasoning.delta',
  'session.reasoning.ended',
  'session.text.started',
  'session.text.delta',
  'session.text.ended',
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
  'orchestration.context.assembled',
  'orchestration.task.dispatched',
  'orchestration.task.status_changed',
  'orchestration.task.completed',
  'orchestration.task.failed',
  'orchestration.synthesis.started',
  'orchestration.review.started',
  'orchestration.followup.dispatched',
  'orchestration.completed',
  'session.agent.switched',
  'session.model.switched',
  'session.steered',
  'session.queue.added',
  'session.queue.claimed',
  'session.queue.cancelled',
  'session.queue.expired',
  'session.queue.failed',
  'session.steer.applied',
  'session.follow_up.started',
  'session.error'
]

const PROCESS_STREAM_EVENTS = new Set([
  'reasoning_delta',
  'answer_delta',
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
  'output_schema.available',
  'context.build_completed',
  'model.request_prepared',
  'model.call_started',
  'model.call_completed',
  'model.continuation_started',
  'model.continuation_completed',
  'model.tool_result_submitted',
  'model.answer_synthesized',
  'provider.empty_output_detected',
  'provider.continuation_completed',
  'run.error',
  'model.tool_call_detected',
  'action.permission_evaluated',
  'action.decision_required',
  'action.decision',
  'action.approved',
  'action.rejected',
  'approval.requested',
  'approval.session_granted',
  'action.execution_started',
  'action.execution_succeeded',
  'action.execution_failed',
  'tool.result_prepared',
  'tool.executed',
  'tool.failed',
  'tool.rejected',
  'provider.empty_output_detected',
  'model_provider.failed',
  'output_schema.validated',
  'output_schema.invalid',
  'skill.available',
  'skill.loaded',
  'skill.used',
  'subagent.started',
  'subagent.spawned',
  'subagent.progress',
  'subagent.blocked_approval',
  'subagent.completed',
  'subagent.failed',
  'subagent.cancelled',
  'subagent.interrupted',
  'subagent.timeout',
  ...OPENCODE_PROCESS_EVENTS
])

export const L5_RUNTIME_EVENT_EXAMPLES = [
  'run.step_announced',
  'action.decision_required',
  'action.decision',
  'approval.session_granted',
  'action.execution_started',
  'action.execution_succeeded',
  'action.execution_failed',
  'tool.executed',
  'tool.failed',
  'tool.rejected',
  'skill.available',
  'skill.loaded',
  'skill.used',
  'subagent.started',
  'subagent.spawned',
  'subagent.progress',
  'subagent.blocked_approval',
  'subagent.completed',
  'subagent.failed',
  'subagent.cancelled',
  'subagent.interrupted',
  'subagent.timeout'
]

export function isPersistentRuntimeEvent(event) {
  return Boolean(event) && !STREAM_CONTROL_EVENTS.has(event)
}

export function isProcessRuntimeEvent(event) {
  return Boolean(event) && PROCESS_STREAM_EVENTS.has(event)
}

export function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map((item) => stableStringify(item)).join(',')}]`
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value ?? null)
}

export function normalizeRuntimeEvent(event, data = {}, timestamp = new Date().toISOString()) {
  const nestedPayload = data?.payload_json && typeof data.payload_json === 'object' && !Array.isArray(data.payload_json)
    ? data.payload_json
    : {}
  const display = data?.display_json || nestedPayload.display_json || {}
  const payload = {
    ...nestedPayload,
    ...data,
    display_json: display
  }
  return {
    type: event,
    event,
    event_id: data?.event_id || nestedPayload.event_id,
    payload,
    data: payload,
    display_json: display,
    timestamp: data?.created_at || data?.timestamp || nestedPayload.timestamp || timestamp
  }
}

export function mergeRuntimeEvents(existingEvents, replayEvents) {
  const byKey = new Map()
  ;[...existingEvents, ...replayEvents].forEach((item) => {
    const eventId = item?.event_id || item?.data?.event_id || item?.payload?.event_id
    const key = eventId || `${item?.type || item?.event || 'event'}:${stableStringify(item?.payload || item?.data || item)}`
    if (!byKey.has(key)) byKey.set(key, item)
  })
  return [...byKey.values()]
    .map((event, index) => ({ event, index }))
    .sort((left, right) => compareRuntimeEventOrder(left, right))
    .map(({ event }) => event)
}

function compareRuntimeEventOrder(left, right) {
  const leftSeq = runtimeEventSequence(left.event)
  const rightSeq = runtimeEventSequence(right.event)
  if (Number.isFinite(leftSeq) && Number.isFinite(rightSeq) && leftSeq !== rightSeq) return leftSeq - rightSeq
  const leftTime = runtimeEventTimestamp(left.event)
  const rightTime = runtimeEventTimestamp(right.event)
  if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) return leftTime - rightTime
  return left.index - right.index
}

function runtimeEventSequence(event = {}) {
  const payload = event?.payload || event?.data || {}
  const value = Number(event?.event_seq ?? event?.seq ?? payload.event_seq ?? payload.seq)
  return Number.isFinite(value) && value > 0 ? value : Number.NaN
}

function runtimeEventTimestamp(event = {}) {
  const payload = event?.payload || event?.data || {}
  const value = Date.parse(event?.created_at || event?.timestamp || payload.timestamp || '')
  return Number.isFinite(value) ? value : Number.NaN
}
