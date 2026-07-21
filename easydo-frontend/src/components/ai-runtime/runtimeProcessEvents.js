const AUDIT_EVENT_PREFIXES = [
  'provider.'
]

const AUDIT_EVENTS = new Set([
  'model.call_started',
  'model.call_completed',
  'model.continuation_started',
  'model.continuation_completed',
  'model.answer_synthesized',
  'provider.empty_output_detected',
  'model.tool_result_submitted',
  'provider.raw_stored',
  'run.step_announced'
])

const ACTION_EVENTS = new Set([
  'action.decision_required',
  'action.approved',
  'action.rejected'
])

const SUPPORT_EVENTS = new Set([
  'model.tool_call_detected',
  'action.permission_evaluated'
])

const TOOL_CALL_EVENTS = new Set([
  'action.execution_started'
])

const TOOL_RESULT_EVENTS = new Set([
  'tool.result_prepared',
  'action.execution_succeeded',
  'action.execution_failed',
  'tool.executed',
  'tool.failed',
  'tool.rejected',
  'model_provider.failed'
])

const MODEL_CALL_END_EVENTS = new Set([
  'model.call_completed',
  'model.continuation_completed'
])

const RUN_STATE_EVENTS = new Set([
  'run.completed',
  'run.failed',
  'run.cancelled',
  'run.timeout',
  'run.awaiting_decision',
  'run.interrupted'
])

const COMPACT_DELTA_EVENTS = new Set([
  'reasoning_delta',
  'answer_delta',
  'session.reasoning.delta',
  'session.text.delta',
  'session.tool.input.delta'
])

export function visibleRuntimeProcessEvents(events = []) {
  return normalizeInputEvents(events).filter((item) => classifyRuntimeEvent(item).lineKind !== 'audit')
}

export function buildRuntimeProcessItems({ events = [], reasoning = '', answer = '', timings = {}, limit = 18, now = Date.now() } = {}) {
  // Strict timeline: sort first and only compact adjacent streaming deltas.
  const normalizedEvents = compactStreamingDeltaEvents(normalizeInputEvents(events))
  const items = []
  const reasoningText = firstString(reasoning)
  const answerText = firstString(answer)
  const hasReasoningEvent = normalizedEvents.some((item) =>
    item.eventName === 'reasoning_delta' ||
    item.eventName === 'session.reasoning.delta' ||
    item.eventName === 'session.reasoning.ended'
  )
  const hasAnswerEvent = normalizedEvents.some((item) =>
    item.eventName === 'answer_delta' ||
    item.eventName === 'session.text.delta' ||
    item.eventName === 'session.text.ended'
  )
  const toolDrafts = new Map()

  normalizedEvents.forEach((event, index) => {
    if (event.eventName === 'runtime_event') {
      event = normalizeRuntimeEventWrapper(event)
    }
    // Skip user input events — the trace is for model output only.
    if (event.eventName === 'session.prompted') return
    if (event.eventName === 'session.step.started') {
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'waiting', event, now)
      items.push(item)
      return
    }
    if (event.eventName === 'session.reasoning.started' || event.eventName === 'session.text.started') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, event.eventName === 'session.reasoning.started' ? 'reasoning' : 'answer', event, now)
      item.lineTitle = event.eventName === 'session.reasoning.started' ? '开始思考' : '开始生成回复'
      items.push(item)
      return
    }
    if (event.eventName === 'session.tool.input.started') {
      closeOpenModelWaits(items, event, now)
      toolDrafts.set(toolContextKey(event), event)
      const item = toProcessItem(event, { lineKind: 'tool', status: 'running' }, index)
      attachInheritedReason(item, items)
      items.push(item)
      return
    }
    if (event.eventName === 'session.tool.input.delta') {
      const key = toolContextKey(event)
      const draft = toolDrafts.get(key)
      const deltaEvent = {
        ...event,
        data: { ...event.data, input_draft: firstString(event.data.delta) }
      }
      const item = toProcessItem(deltaEvent, { lineKind: 'tool', status: 'running' }, index)
      if (draft) mergeDraftIntoItem(item, draft)
      toolDrafts.set(key, deltaEvent)
      items.push(item)
      return
    }
    if (event.eventName === 'session.tool.input.ended') {
      const key = toolContextKey(event)
      const draft = toolDrafts.get(key)
      const item = toProcessItem(event, { lineKind: 'tool', status: 'success' }, index)
      if (draft) mergeDraftIntoItem(item, draft)
      toolDrafts.set(key, event)
      items.push(item)
      return
    }
    if (event.eventName === 'session.reasoning.delta') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'reasoning', event, now)
      appendThoughtText(item, 'reasoning', deltaText(event.data.delta, event.display.summary), event)
      items.push(item)
      return
    }
    if (event.eventName === 'session.reasoning.ended') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'reasoning', event, now)
      setThoughtText(item, 'reasoning', deltaText(event.data.text, event.data.delta, event.display.summary), event)
      item.status = 'success'
      item.lineTitle = '思考完成'
      items.push(item)
      return
    }
    if (event.eventName === 'session.text.delta') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'answer', event, now)
      appendThoughtText(item, 'answer', deltaText(event.data.delta, event.display.summary), event)
      items.push(item)
      return
    }
    if (event.eventName === 'session.text.ended') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'answer', event, now)
      setThoughtText(item, 'answer', deltaText(event.data.text, event.data.delta, event.display.summary), event)
      item.status = 'success'
      item.lineTitle = '回复生成完成'
      items.push(item)
      return
    }
    if (event.eventName === 'model.continuation_started') {
      items.push(toProcessItem(event, { lineKind: 'model', status: 'running' }, index))
      return
    }
    if (event.eventName === 'model.call_started') {
      items.push(toProcessItem(event, { lineKind: 'model', status: 'running' }, index))
      return
    }
    if (event.eventName === 'reasoning_delta') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'reasoning', event, now)
      appendThoughtText(item, 'reasoning', deltaText(event.data.delta, event.display.summary), event)
      items.push(item)
      return
    }
    if (event.eventName === 'answer_delta') {
      closeOpenModelWaits(items, event, now)
      const item = createThoughtItem(event, index)
      setThoughtPhase(item, 'answer', event, now)
      appendThoughtText(item, 'answer', deltaText(event.data.delta, event.display.summary), event)
      items.push(item)
      return
    }
    if (event.eventName === 'session.step.failed' || event.eventName === 'session.step.ended') {
      closeOpenModelWaits(items, event, now, event.eventName === 'session.step.failed' ? 'failed' : 'completed')
      items.push(toProcessItem(event, classifyRuntimeEvent(event), index))
      return
    }
    if (MODEL_CALL_END_EVENTS.has(event.eventName)) {
      closeOpenModelWaits(items, event, now)
      items.push(toProcessItem(event, { lineKind: 'model', status: 'success' }, index))
      return
    }
    if (event.eventName === 'provider.empty_output_detected') {
      items.push(toProcessItem(event, { lineKind: 'model', status: 'running' }, index))
      return
    }
    if (event.eventName === 'model.answer_synthesized') {
      items.push(toProcessItem(event, { lineKind: 'model', status: 'success' }, index))
      return
    }
    if (event.eventName === 'model.tool_call_detected') {
      toolDrafts.set(toolContextKey(event), event)
      return
    }
    if (event.eventName === 'approval.requested' || event.eventName === 'permission.asked') {
      closeOpenModelWaits(items, event, now)
      const classification = { lineKind: 'action', status: 'waiting' }
      const item = toProcessItem(event, classification, index)
      attachInheritedReason(item, items)
      const draft = toolDrafts.get(toolContextKey(event))
      if (draft) mergeDraftIntoItem(item, draft)
      // Waiting approval panel is intentionally minimal: title + target + buttons only.
      item.lineSummary = ''
      item.resultSummary = ''
      item.resultStatus = ''
      item.resultTitle = ''
      sanitizeActionApprovalCopy(item)
      items.push(item)
      return
    }
    if (
      event.eventName === 'permission.resolved' ||
      event.eventName === 'action.decision' ||
      event.eventName === 'approval.session_granted' ||
      event.eventName === 'action.approved' ||
      event.eventName === 'action.rejected'
    ) {
      closeOpenModelWaits(items, event, now)
      const status = actionStatus(event.data, event.display)
      const decision = firstString(event.data.decision, event.display.decision, event.data.result, event.display.result)
      const target = findRelatedProcessItem(items, event)
      // One approval = one panel. Merge decision into the waiting row; do not open a second panel.
      if (target?.lineKind === 'action') {
        target.status = status
        target.event = event.eventName
        target.raw = event.raw || target.raw
        target.data = {
          ...target.data,
          ...event.data,
          decision: firstString(decision, target.data.decision),
          result: firstString(event.data.result, event.display.result, target.data.result)
        }
        target.display = {
          ...target.display,
          ...event.display,
          decision: firstString(decision, target.display.decision)
        }
        target.resultStatus = status
        target.resultTitle = approvalDecisionTitle(status)
        target.resultSummary = approvalDecisionSummary(event.data, event.display) || approvalDecisionTitle(status)
        target.lineTitle = compactJoin([approvalDecisionTitle(status), toolName(target.data, target.display)], ' ')
        target.lineSummary = firstString(target.resultSummary, target.lineSummary)
        sanitizeActionApprovalCopy(target)
        return
      }
      const item = toProcessItem(event, { lineKind: 'action', status }, index)
      item.resultStatus = status
      item.resultTitle = approvalDecisionTitle(status)
      item.resultSummary = approvalDecisionSummary(event.data, event.display) || approvalDecisionTitle(status)
      if (!item.lineSummary) item.lineSummary = item.resultSummary
      items.push(item)
      return
    }
    if (event.eventName === 'session.tool.failed' && isApprovalPauseToolFailure(event, items)) {
      // Pi approval pause emits a synthetic tool.failed; keep the waiting action panel only.
      closeOpenModelWaits(items, event, now)
      return
    }
    if (SUPPORT_EVENTS.has(event.eventName)) {
      return
    }
    if (RUN_STATE_EVENTS.has(event.eventName)) {
      // Avoid a second "awaiting decision" banner when an action panel already shows the same wait.
      if (event.eventName === 'run.awaiting_decision' && items.some((item) => item.lineKind === 'action' && item.status === 'waiting')) {
        return
      }
      const classification = classifyRuntimeEvent(event)
      items.push(toProcessItem(event, classification, index))
      return
    }
    if (event.eventName === 'session.tool.called' || event.eventName === 'session.tool.success' || event.eventName === 'session.tool.failed') {
      closeOpenModelWaits(items, event, now)
    }
    const classification = classifyRuntimeEvent(event)
    if (classification.lineKind === 'audit') return
    const item = toProcessItem(event, classification, index)
    attachInheritedReason(item, items)
    if (item.lineKind === 'subagent') appendSubagentProgress(item, event)
    const draft = toolDrafts.get(toolContextKey(event))
    if (draft && (item.lineKind === 'mcp' || item.lineKind === 'tool' || item.lineKind === 'action')) {
      mergeDraftIntoItem(item, draft)
    }
    items.push(item)
  })

  // Never unshift synthetic content ahead of real events — only append or fill an existing thought.
  if (reasoningText && !hasReasoningEvent) {
    appendThoughtText(
      appendSyntheticThought(items, 'reasoning-synthetic'),
      'reasoning',
      reasoningText,
      { data: { elapsed_ms: timings.first_reasoning_ms ?? timings.first_reasoning }, display: {} }
    )
  }
  if (answerText && !hasAnswerEvent) {
    appendThoughtText(
      appendSyntheticThought(items, 'answer-synthetic'),
      'answer',
      answerText,
      { data: { elapsed_ms: timings.first_answer_ms ?? timings.first_answer }, display: {} }
    )
  }

  const finalizedItems = items
    .map(finalizeThoughtItem)
    .filter((item) => item.lineKind !== 'thought' || item.sections.length > 0 || item.modelCall || item.raw)
  const safeLimit = Number.isFinite(Number(limit)) ? Number(limit) : 18
  return applyProcessItemLimit(finalizedItems, safeLimit)
}

function isProtectedTimelineItem(item = {}) {
  if (item.lineKind === 'thought') return true
  if (item.lineKind === 'action' || item.lineKind === 'result') return true
  if (['failed', 'cancelled', 'timeout', 'interrupted', 'waiting'].includes(String(item.status || ''))) return true
  return [
    'session.step.started',
    'session.step.ended',
    'session.step.failed',
    'session.error',
    'session.tool.called',
    'permission.asked',
    'permission.resolved',
    'run.failed',
    'run.cancelled',
    'run.timeout',
    'run.interrupted',
    'run.awaiting_decision',
    'run.completed'
  ].includes(String(item.event || ''))
}

function applyProcessItemLimit(items, limit) {
  if (limit <= 0 || items.length <= limit) return items
  const protectedItems = items.filter((item) => isProtectedTimelineItem(item))
  if (protectedItems.length >= limit) return protectedItems.slice(-limit)
  const remaining = limit - protectedItems.length
  const optional = items.filter((item) => !isProtectedTimelineItem(item))
  const keptOptional = new Set(optional.slice(-remaining))
  const keptProtected = new Set(protectedItems)
  return items.filter((item) => keptProtected.has(item) || keptOptional.has(item))
}

function toProcessItem(event, classification, index) {
  const { data, display, eventName } = event
  const artifactRefs = eventArtifactRefs(data, display)
  const childRunLinkId = firstString(
    display.child_run_link_id,
    data.child_run_link_id,
    artifactRefs[0]?.preview_json?.child_run_link_id
  )
  const childRuntimeRunId = firstString(
    display.child_runtime_run_id,
    data.child_runtime_run_id,
    artifactRefs[0]?.preview_json?.child_runtime_run_id
  )

  return {
    key: `${firstString(event.raw?.event_id, data.event_id, eventName)}-${index}`,
    event: eventName,
    lineKind: classification.lineKind,
    status: classification.status,
    lineMeta: lineMeta(classification.lineKind, eventName, data, display),
    lineTitle: lineTitle(classification.lineKind, eventName, data, display),
    lineTarget: lineTarget(classification.lineKind, eventName, data, display),
    lineSummary: lineSummary(classification.lineKind, eventName, data, display, artifactRefs),
    data,
    display,
    raw: event.raw,
    artifactRefs,
    childRunLinkId,
    childRuntimeRunId,
    inputPreviewText: processInputPreview(classification.lineKind, data),
      reasonText: processReasonText(classification.lineKind, data, display),
    subagentProgress: [],
    resultSummary: '',
    resultStatus: '',
    resultTitle: ''
  }
}

function mergeDraftIntoItem(item, draft) {
  item.data = { ...draft.data, ...item.data }
  item.display = { ...draft.display, ...item.display }
  refreshProcessItemPresentation(item)
}

function mergeProcessEventIntoItem(item, event, classification) {
  item.event = event.eventName
  item.status = classification.status || item.status
  item.data = { ...item.data, ...event.data }
  item.display = { ...item.display, ...event.display }
  item.raw = event.raw || item.raw
  if (item.lineKind === 'subagent') {
    item.childRunLinkId = firstString(item.childRunLinkId, event.display.child_run_link_id, event.data.child_run_link_id)
    item.childRuntimeRunId = firstString(item.childRuntimeRunId, event.display.child_runtime_run_id, event.data.child_runtime_run_id)
    const nextArtifactRefs = normalizeArtifactRefs(event.display.artifact_refs).length
      ? normalizeArtifactRefs(event.display.artifact_refs)
      : normalizeArtifactRefs(event.data.artifact_refs)
    item.artifactRefs = mergeArtifactRefs(item.artifactRefs, nextArtifactRefs)
    appendSubagentProgress(item, event)
    if (['success', 'failed', 'cancelled', 'timeout', 'interrupted'].includes(classification.status)) {
      item.resultSummary = firstString(event.display.summary, event.data.summary, event.data.result, item.resultSummary)
      item.resultStatus = classification.status
    }
  }
  refreshProcessItemPresentation(item)
}

function refreshProcessItemPresentation(item) {
  item.lineMeta = lineMeta(item.lineKind, item.event, item.data, item.display)
  item.lineTitle = lineTitle(item.lineKind, item.event, item.data, item.display)
  item.lineTarget = lineTarget(item.lineKind, item.event, item.data, item.display)
  item.lineSummary = lineSummary(item.lineKind, item.event, item.data, item.display, item.artifactRefs)
  item.inputPreviewText = processInputPreview(item.lineKind, item.data)
  item.reasonText = processReasonText(item.lineKind, item.data, item.display, item.reasonText)
  if (item.lineKind === 'action') sanitizeActionApprovalCopy(item)
}

function attachInheritedReason(item, items = []) {
  if (!['mcp', 'tool', 'action', 'subagent'].includes(item.lineKind)) return
  if (item.reasonText && !isGenericToolApprovalText(item.reasonText)) {
    if (item.lineKind === 'action') sanitizeActionApprovalCopy(item)
    return
  }
  const thought = lastThoughtItem(items)
  const inherited = firstMeaningfulApprovalText(thought?.reasoningText)
  if (inherited) item.reasonText = inherited
  else if (isGenericToolApprovalText(item.reasonText)) item.reasonText = ''
  if (item.lineKind === 'action') sanitizeActionApprovalCopy(item)
}

// Clear generic boilerplate ("Tool X requires approval") from action copy.
// We DO NOT collapse a meaningful reason/summary that happens to be identical —
// the RuntimeTrace template renders reasonText and lineSummary in different
// partitions and the approval card may legitimately want to surface the same
// phrasing twice (once as a reason block, once inside the approval panel).
function sanitizeActionApprovalCopy(item = {}) {
  if (item.lineKind !== 'action') return
  if (isGenericToolApprovalText(item.reasonText)) item.reasonText = ''
  if (isGenericToolApprovalText(item.lineSummary)) item.lineSummary = ''
  if (isGenericToolApprovalText(item.resultSummary)) {
    item.resultSummary = item.resultSummary
      .split(' · ')
      .map((part) => part.trim())
      .filter((part) => part && !isGenericToolApprovalText(part))
      .join(' · ')
  }
}

function findRelatedProcessItem(items, event) {
  const subagentKey = subagentContextKey(event)
  if (subagentKey) {
    const subagentItem = [...items].reverse().find((item) =>
      item.lineKind === 'subagent' &&
      subagentContextKey({ data: item.data, display: item.display, eventName: item.event }) === subagentKey
    )
    if (subagentItem) return subagentItem
  }
  const key = toolContextKey(event)
  return [...items].reverse().find((item) => {
    if (!['mcp', 'tool', 'action', 'subagent'].includes(item.lineKind)) return false
    return toolContextKey({ data: item.data, display: item.display, eventName: item.event }) === key
  })
}

function toolContextKey(event = {}) {
  const data = asRecord(event.data)
  const display = asRecord(event.display)
  const action = asRecord(data.action)
  const input = asRecord(data.input_json || action.input_json)
  const actionDisplay = asRecord(action.display_json)
  const approval = asRecord(data.approval_request || display.approval_request || actionDisplay.approval_request)
  return firstString(
    data.action_id,
    action.action_id,
    action.id,
    approval.action_id,
    data.action_internal_id,
    action.internal_id,
    data.provider_tool_call_id,
    input.provider_tool_call_id,
    action.input_json?.provider_tool_call_id,
    approval.provider_tool_call_id,
    data.call_id,
    data.tool_call_id,
    input.tool_call_id,
    toolName(data, display),
    event.eventName
  )
}

function mergeArtifactRefs(current = [], next = []) {
  const byId = new Map()
  ;[...current, ...next].forEach((item) => {
    const id = firstString(item.artifact_id)
    if (id && !byId.has(id)) byId.set(id, item)
  })
  return [...byId.values()]
}

function classifyRuntimeEvent({ eventName, data, display }) {
  if (eventName === 'reasoning_delta' || eventName === 'answer_delta') return { lineKind: 'thought', status: 'info' }
  if (eventName === 'context.build_started') return { lineKind: 'context', status: 'running' }
  if (eventName === 'context_tags.resolved') return { lineKind: 'context', status: 'success' }
  if (eventName === 'context.build_completed') return { lineKind: 'context', status: 'success' }
  if (eventName === 'capability.snapshot') return { lineKind: 'context', status: 'success' }
  if (eventName === 'mcp.tools.available') return { lineKind: 'context', status: 'success' }
  if (eventName === 'mcp.tools_discovery_failed') return { lineKind: 'context', status: 'failed' }
  if (eventName === 'output_schema.available') return { lineKind: 'context', status: 'success' }
  if (eventName === 'output_schema.validated') return { lineKind: 'context', status: 'success' }
  if (eventName === 'output_schema.invalid') return { lineKind: 'context', status: 'failed' }
  // session.prompted is intentionally excluded — the trace shows model output only,
  // not the user's input prompt.
  // if (eventName === 'session.prompted') return { lineKind: 'prompt', status: 'info' }
  if (eventName === 'session.step.started') return { lineKind: 'thought', status: 'info' }
  if (eventName === 'session.reasoning.delta' || eventName === 'session.reasoning.ended') return { lineKind: 'thought', status: 'info' }
  if (eventName === 'session.text.delta' || eventName === 'session.text.ended') return { lineKind: 'thought', status: 'info' }
  if (eventName === 'session.reasoning.started' || eventName === 'session.text.started') return { lineKind: 'thought', status: 'info' }
  if (eventName === 'session.tool.input.started' || eventName === 'session.tool.input.delta' || eventName === 'session.tool.input.ended') return { lineKind: 'tool', status: 'running' }
  if (eventName === 'permission.asked') return { lineKind: 'action', status: 'waiting' }
  if (eventName === 'permission.resolved') return { lineKind: 'action', status: actionStatus(data, display) }
  if (eventName === 'approval.session_granted' || eventName === 'action.decision') return { lineKind: 'action', status: actionStatus(data, display) }
  if (eventName === 'session.steered' || eventName === 'session.steer.applied' || eventName === 'session.follow_up.started') return { lineKind: 'action', status: 'success' }
  if (eventName === 'session.queue.added' || eventName === 'session.queue.claimed') return { lineKind: 'action', status: 'running' }
  if (eventName === 'session.queue.cancelled') return { lineKind: 'action', status: 'cancelled' }
  if (eventName === 'session.queue.expired') return { lineKind: 'action', status: 'timeout' }
  if (eventName === 'session.queue.failed') return { lineKind: 'action', status: 'failed' }
  if (eventName === 'session.tool.called') return { lineKind: isMcpEvent(eventName, data, display) ? 'mcp' : 'tool', status: 'running' }
  if (eventName === 'session.tool.progress') return { lineKind: isMcpEvent(eventName, data, display) ? 'mcp' : 'tool', status: 'running' }
  if (eventName === 'session.tool.success' || eventName === 'session.tool.failed') return { lineKind: 'result', status: resultStatus(eventName, data, display) }
  if (eventName === 'session.step.ended') return { lineKind: 'result', status: 'success' }
  if (eventName === 'session.step.failed' || eventName === 'session.error') return { lineKind: 'result', status: 'failed' }
  if (eventName === 'run.started') return { lineKind: 'model', status: 'running' }
  if (eventName === 'run.completed') return { lineKind: 'model', status: 'success' }
  if (eventName === 'run.failed') return { lineKind: 'model', status: 'failed' }
  if (eventName === 'run.cancelled') return { lineKind: 'model', status: 'cancelled' }
  if (eventName === 'run.timeout') return { lineKind: 'model', status: 'timeout' }
  if (eventName === 'run.awaiting_decision') return { lineKind: 'model', status: 'waiting' }
  if (eventName === 'run.interrupted') return { lineKind: 'model', status: 'interrupted' }
  if (eventName === 'run.error') return { lineKind: 'result', status: 'failed' }
  if (eventName === 'model.request_prepared') return { lineKind: 'model', status: 'running' }
  if (eventName === 'model.tool_result_submitted') return { lineKind: 'model', status: 'success' }
  if (eventName === 'context.budget.evaluated') return { lineKind: 'model', status: 'info' }
  if (eventName === 'session.compaction.started' || eventName === 'context.compaction.started') return { lineKind: 'model', status: 'running' }
  if (eventName === 'session.compaction.ended' || eventName === 'context.compaction.completed') return { lineKind: 'model', status: 'success' }
  if (eventName === 'context.compaction.failed') return { lineKind: 'model', status: 'failed' }
  if (eventName === 'orchestration.context.assembled') return { lineKind: 'subagent', status: 'info' }
  if (eventName === 'orchestration.task.dispatched' || eventName === 'orchestration.followup.dispatched') return { lineKind: 'subagent', status: 'running' }
  if (eventName === 'orchestration.task.status_changed') return { lineKind: 'subagent', status: subagentStatus(data, display, 'running') }
  if (eventName === 'orchestration.task.completed' || eventName === 'orchestration.completed') return { lineKind: 'subagent', status: 'success' }
  if (eventName === 'orchestration.task.failed') return { lineKind: 'subagent', status: 'failed' }
  if (eventName === 'orchestration.synthesis.started' || eventName === 'orchestration.review.started') return { lineKind: 'subagent', status: 'running' }
  if (eventName === 'session.agent.switched' || eventName === 'session.model.switched') return { lineKind: 'model', status: 'success' }
  if (eventName.startsWith('skill.')) {
    return { lineKind: 'skill', status: 'success' }
  }
  if (eventName === 'subagent.started' || eventName === 'subagent.spawned') return { lineKind: 'subagent', status: 'running' }
  if (eventName === 'subagent.progress') return { lineKind: 'subagent', status: subagentStatus(data, display, 'running') }
  if (eventName === 'subagent.blocked_approval') return { lineKind: 'subagent', status: 'waiting' }
  if (eventName === 'subagent.completed') return { lineKind: 'subagent', status: 'success' }
  if (eventName === 'subagent.failed') return { lineKind: 'subagent', status: 'failed' }
  if (eventName === 'subagent.cancelled') return { lineKind: 'subagent', status: 'cancelled' }
  if (eventName === 'subagent.interrupted') return { lineKind: 'subagent', status: 'interrupted' }
  if (eventName === 'subagent.timeout') return { lineKind: 'subagent', status: 'timeout' }
  if (eventName === 'approval.requested') return { lineKind: 'action', status: 'waiting' }
  if (SUPPORT_EVENTS.has(eventName)) return { lineKind: 'audit', status: 'info' }
  if (ACTION_EVENTS.has(eventName)) return { lineKind: 'action', status: actionStatus(data, display) }
  if (eventName === 'provider.empty_output_detected') return { lineKind: 'model', status: 'running' }
  if (eventName === 'provider.continuation_completed') return { lineKind: 'model', status: 'success' }
  if (eventName === 'model.answer_synthesized') return { lineKind: 'model', status: 'success' }
  if (TOOL_CALL_EVENTS.has(eventName)) {
    return { lineKind: isMcpEvent(eventName, data, display) ? 'mcp' : 'tool', status: 'running' }
  }
  if (TOOL_RESULT_EVENTS.has(eventName)) return { lineKind: 'result', status: resultStatus(eventName, data, display) }
  if (AUDIT_EVENTS.has(eventName) || AUDIT_EVENT_PREFIXES.some((prefix) => eventName.startsWith(prefix))) {
    return { lineKind: 'audit', status: 'info' }
  }
  return { lineKind: 'audit', status: 'info' }
}

function createThoughtItem(event = {}, index = 0, fallbackKey = 'thought') {
  const data = asRecord(event.data)
  const display = asRecord(event.display)
  const round = firstString(data.round, display.round)
  const phase = firstString(data.phase, display.phase)
  return {
    key: `${firstString(event.raw?.event_id, data.event_id, event.eventName, fallbackKey)}-${index}`,
    event: firstString(event.eventName, fallbackKey),
    lineKind: 'thought',
    status: event.eventName === 'session.step.started' ? 'running' : 'info',
    lineMeta: round
      ? compactJoin(['LLM', readablePhase(phase), `#${round}`], ' ')
      : event.eventName === 'session.step.started' ? 'LLM' : 'Thought',
    lineTitle: '',
    lineTarget: event.eventName === 'session.step.started' ? modelTarget(data, display) : '',
    lineSummary: '',
    reasoningText: '',
    answerText: '',
    sections: [],
    data,
    display,
    raw: event.raw || null,
    artifactRefs: [],
    childRunLinkId: '',
    childRuntimeRunId: '',
    modelCall: event.eventName === 'session.step.started' || event.eventName === 'model.call_started' || event.eventName === 'model.continuation_started'
  }
}

function setThoughtPhase(item, phase, event = {}, currentTime = Date.now()) {
  if (!item || item.lineKind !== 'thought') return
  const data = asRecord(event.data)
  const display = asRecord(event.display)
  if (phase === 'waiting') {
    if (item.reasoningText || item.answerText || item.sections?.length || isTerminalThoughtStatus(item.status) || item.thoughtPhase === 'reasoning' || item.thoughtPhase === 'answer' || item.modelWaitClosed) {
      return
    }
    item.thoughtPhase = 'waiting'
    item.status = 'running'
    item.lineMeta = 'LLM'
    item.lineTitle = '正在请求模型'
    item.lineTarget = firstString(item.lineTarget, modelTarget(data, display))
    item.modelWaitStartedAt = firstString(item.modelWaitStartedAt, event.raw?.timestamp, data.timestamp, display.timestamp)
    item.lineSummary = modelWaitSummary(item.modelWaitStartedAt, currentTime)
    return
  }
  if (phase === 'reasoning') {
    item.thoughtPhase = 'reasoning'
    item.status = 'running'
    item.lineTitle = '模型正在思考'
    if (isModelWaitSummary(item.lineSummary) || (!item.reasoningText && !item.answerText)) {
      item.lineSummary = ''
    }
    return
  }
  if (phase === 'answer') {
    item.thoughtPhase = 'answer'
    item.status = 'running'
    item.lineTitle = '正在生成回复'
    if (isModelWaitSummary(item.lineSummary) || (!item.reasoningText && !item.answerText)) {
      item.lineSummary = ''
    }
    return
  }
  if (phase === 'failed') {
    item.thoughtPhase = 'failed'
    item.status = 'failed'
    item.lineTitle = '模型请求失败'
    const failureSummary = firstString(asRecord(data.error).message, display.summary, data.message)
    item.lineSummary = failureSummary || (isModelWaitSummary(item.lineSummary) ? '' : item.lineSummary)
    return
  }
  if (phase === 'completed') {
    item.thoughtPhase = 'completed'
    item.status = 'success'
    item.lineTitle = '模型响应完成'
    if (isModelWaitSummary(item.lineSummary)) item.lineSummary = ''
  }
}

// Freeze historical "等待首个响应 N 秒" once the model produced any follow-up signal.
// Without this, RuntimeTrace's 1s clock keeps inflating elapsed time forever.
function closeOpenModelWaits(items = [], event = {}, currentTime = Date.now(), phase = 'completed') {
  const endAt = firstString(event.raw?.timestamp, event.data?.timestamp, event.display?.timestamp, currentTime)
  items.forEach((item) => {
    if (item.lineKind !== 'thought') return
    if (item.thoughtPhase !== 'waiting' || item.modelWaitClosed) return
    const startedAt = firstString(item.modelWaitStartedAt, item.raw?.timestamp, item.data?.timestamp, item.display?.timestamp)
    const frozenSummary = modelWaitSummary(startedAt, endAt)
    item.modelWaitClosed = true
    item.modelWaitEndedAt = endAt
    if (phase === 'failed') {
      item.thoughtPhase = 'failed'
      item.status = 'failed'
      item.lineTitle = '模型请求失败'
      item.lineSummary = firstString(asRecord(event.data?.error).message, event.display?.summary, event.data?.message, frozenSummary)
      return
    }
    item.thoughtPhase = 'completed'
    item.status = 'success'
    item.lineTitle = '模型响应完成'
    // Keep the frozen wait duration as a short historical note, not a live counter.
    item.lineSummary = frozenSummary && frozenSummary !== '等待首个响应' ? frozenSummary : ''
  })
}

function modelWaitSummary(startedAt = '', currentTime = Date.now()) {
  const startedMs = Date.parse(firstString(startedAt))
  const currentMs = typeof currentTime === 'number' ? currentTime : Date.parse(firstString(currentTime))
  if (!Number.isFinite(startedMs) || !Number.isFinite(currentMs)) return '等待首个响应'
  const elapsedSeconds = Math.max(0, Math.floor((currentMs - startedMs) / 1000))
  return elapsedSeconds > 0 ? `等待首个响应 ${elapsedSeconds} 秒` : '等待首个响应'
}

function isModelWaitSummary(value) {
  return /^等待首个响应(?:\s+\d+\s*秒)?$/.test(String(value || '').trim())
}

function isTerminalThoughtStatus(status) {
  return ['success', 'failed', 'cancelled', 'timeout', 'interrupted'].includes(String(status || ''))
}

function modelTarget(data = {}, display = {}) {
  const model = asRecord(data.model || display.model)
  const provider = firstString(model.provider_id, model.provider, data.provider_id, display.provider_id)
  const modelName = firstString(model.id, model.model, model.name, data.model_id, display.model_id)
  return provider && modelName ? `${provider}/${modelName}` : firstString(modelName, provider)
}

function buildThoughtSections(item) {
  return [
    item.reasoningText ? { kind: 'reasoning', label: 'reasoning', text: item.reasoningText } : null,
    item.answerText ? { kind: 'answer', label: 'answer', text: item.answerText } : null
  ].filter(Boolean)
}

function appendThoughtText(item, kind, text, event = {}) {
  writeThoughtText(item, kind, text, event, 'append')
}

function setThoughtText(item, kind, text, event = {}) {
  writeThoughtText(item, kind, text, event, 'set')
}

function writeThoughtText(item, kind, text, event = {}, mode = 'append') {
  const value = deltaText(text)
  if (!value) return
  if (kind === 'answer') {
    item.answerText = mode === 'set' ? value : `${item.answerText || ''}${value}`
    item.data = { ...item.data, answer: item.answerText }
  } else {
    item.reasoningText = mode === 'set' ? value : `${item.reasoningText || ''}${value}`
    item.data = { ...item.data, reasoning: item.reasoningText, delta: item.reasoningText }
  }
  item.lineSummary = item.reasoningText || item.answerText
  const timing = event.display?.elapsed_ms ?? event.data?.elapsed_ms
  const formattedTiming = formatTiming(timing)
  if (formattedTiming && (kind === 'reasoning' || item.lineMeta === 'Thought')) {
    item.lineMeta = `Thought: ${formattedTiming}`
  }
  // Update sections in real-time so markdown rendering works during streaming,
  // not only after finalizeThoughtItem runs at the end.
  item.sections = buildThoughtSections(item)
}

function deltaText(...values) {
  for (const value of values) {
    if (value === undefined || value === null) continue
    const text = String(value)
    if (text.trim()) return text
  }
  return ''
}

function finalizeThoughtItem(item) {
  if (item.lineKind !== 'thought') return item
  const sections = buildThoughtSections(item)
  let lineSummary = item.lineSummary
  // Keep frozen wait duration only when the wait row never got content and was closed by a later signal.
  if (isModelWaitSummary(lineSummary) && !item.modelWaitClosed && (sections.length > 0 || item.reasoningText || item.answerText || isTerminalThoughtStatus(item.status))) {
    lineSummary = ''
  }
  if (item.reasoningText || item.answerText) {
    if (!lineSummary || lineSummary === item.answerText || lineSummary === item.reasoningText || isModelWaitSummary(lineSummary)) {
      lineSummary = ''
    }
  }
  return {
    ...item,
    sections,
    lineSummary
  }
}

function lastThoughtItem(items) {
  return [...items].reverse().find((item) => item.lineKind === 'thought') || null
}

function appendSyntheticThought(items, key) {
  const eventName = String(key).startsWith('answer') ? 'answer' : 'reasoning'
  const item = createThoughtItem({ eventName, data: {}, display: {}, raw: null }, items.length, key)
  item.key = key
  item.event = eventName
  items.push(item)
  return item
}

function lineMeta(lineKind, eventName, data = {}, display = {}) {
  if (lineKind === 'prompt') return 'User'
  if (lineKind === 'context') return 'Context'
  if (lineKind === 'thought') return compactJoin(['Thought:', formatTiming(display.elapsed_ms ?? data.elapsed_ms)], ' ')
  if (lineKind === 'model') {
    const phase = readablePhase(firstString(data.phase, display.phase))
    const round = firstString(data.round, display.round)
    return compactJoin(['LLM', phase, round ? `#${round}` : ''], ' ')
  }
  if (lineKind === 'mcp') return '→ mcp'
  if (lineKind === 'tool') return '→ tool'
  if (lineKind === 'skill') return '→ skill'
  if (lineKind === 'subagent') return eventName === 'subagent.completed' ? '← subagent' : '→ subagent'
  if (lineKind === 'result') return '← result'
  if (lineKind === 'action') return 'Action'
  if (lineKind === 'answer') return 'Answer'
  return eventName
}

function lineTitle(lineKind, eventName, data = {}, display = {}) {
  if (lineKind === 'prompt') return '用户输入'
  if (lineKind === 'context') return contextEventTitle(eventName)
  if (lineKind === 'thought') return ''
  if (lineKind === 'answer') return 'Answer'
  if (lineKind === 'skill') {
    return compactJoin([
      firstString(display.title, display.name, data.name, data.skill_name, 'Skill'),
      firstString(display.operation, data.operation)
    ], ' ')
  }
  if (lineKind === 'subagent') {
    if (String(eventName || '').startsWith('orchestration.')) return orchestrationEventTitle(eventName)
    return compactJoin([
      firstString(display.title, display.name, data.name, data.agent_name, data.assigned_subagent_name, 'Subagent'),
      firstString(display.task, data.task, data.task_id)
    ], ' ')
  }
  if (lineKind === 'model') return modelEventTitle(eventName, data, display)
  if (lineKind === 'action') {
    if (eventName === 'session.steered') return '已调整执行方向'
    if (eventName === 'session.steer.applied') return '已应用运行中指令'
    if (eventName === 'session.follow_up.started') return '已启动队列跟进'
    if (eventName === 'session.queue.added') return '已加入队列'
    if (eventName === 'session.queue.claimed') return '队列处理中'
    if (eventName === 'session.queue.cancelled') return '队列项已取消'
    if (eventName === 'session.queue.expired') return '队列项已过期'
    if (eventName === 'session.queue.failed') return '队列项失败'
    if (eventName === 'permission.asked') return compactJoin(['需要确认', toolName(data, display)], ' ')
    if (eventName === 'permission.resolved' || eventName === 'action.decision') {
      return compactJoin([approvalDecisionTitle(actionStatus(data, display)), toolName(data, display)], ' ')
    }
    if (eventName === 'approval.session_granted') return compactJoin(['本会话已批准', toolName(data, display)], ' ')
    if (eventName === 'action.decision_required') return compactJoin(['需要确认', actionTitle(data)], ' ')
    if (eventName === 'action.approved') return compactJoin(['已批准', actionTitle(data)], ' ')
    if (eventName === 'action.rejected') return compactJoin(['已拒绝', actionTitle(data)], ' ')
    return firstString(display.title, display.name, actionTitle(data), approvalTitle(data), '等待确认')
  }
  if (lineKind === 'result') {
    if (isCancelledResult(eventName, data, display)) return '已停止生成'
    return firstString(display.title, toolName(data, display), resultTitle(eventName))
  }
  return compactJoin([toolName(data, display), inputPreview(data)], ' ')
}

function lineTarget(lineKind, eventName, data = {}, display = {}) {
  if (lineKind === 'prompt') return promptTarget(data, display)
  if (lineKind === 'context') return contextEventTarget(eventName, data, display)
  if (lineKind === 'thought' || lineKind === 'answer') return ''
  if (lineKind === 'skill') return firstString(display.version, data.version)
  if (lineKind === 'subagent') {
    return firstString(data.child_runtime_run_id, data.child_run_link_id, display.child_runtime_run_id, display.child_run_link_id)
  }
  if (lineKind === 'action') return firstString(targetPreview(data, display), data.action_id, display.action_id)
  if (lineKind === 'model') return modelEventTarget(data, display)
  if (lineKind === 'result') return firstString(errorTarget(data, display), targetPreview(data, display), data.execution_id, display.execution_id)
  return firstString(targetPreview(data, display), data.execution_id, display.execution_id)
}

function lineSummary(lineKind, eventName, data = {}, display = {}, artifactRefs = []) {
  if (lineKind === 'prompt') return promptSummary(data, display)
  if (lineKind === 'context') return contextEventSummary(eventName, data, display)
  if (lineKind === 'thought') return firstString(display.summary, data.delta, data.reasoning)
  if (lineKind === 'answer') return firstString(display.summary, data.answer)
  if (lineKind === 'model') return modelEventSummary(eventName, data, display)
  if (lineKind === 'action') {
    if (eventName === 'session.steered') return firstString(display.summary, data.instruction, data.message)
    if (eventName === 'session.steer.applied') return firstString(display.summary, queueItemContent(data), data.instruction, data.message)
    if (eventName === 'session.follow_up.started') return firstString(display.summary, queueItemContent(data), data.consumed_runtime_run_id, data.runtime_run_id)
    if (String(eventName || '').startsWith('session.queue.')) return firstString(display.summary, queueItemContent(data), data.error_code, data.status)
    return firstMeaningfulApprovalText(
      display.summary,
      data.reason,
      approvalSummary(data),
      data.risk_level,
      data.message
    )
  }
  if (lineKind === 'result') return resultSummary(eventName, data, display, artifactRefs)
  if (lineKind === 'subagent') {
    if (String(eventName || '').startsWith('orchestration.')) {
      return firstString(display.summary, formatOrchestrationSummary(eventName, data, display), data.result_summary, data.final_summary, data.error_msg, data.summary)
    }
    return firstString(display.summary, data.summary, data.progress, data.reason, data.message, data.result, data.child_run_link_id)
  }
  return firstString(display.summary, data.summary, data.status, data.name)
}

function resultSummary(eventName, data = {}, display = {}, artifactRefs = []) {
  const action = asRecord(data.action)
  const actionResult = asRecord(action.result_json)
  const outputPaths = normalizeOutputPaths(data.output_paths || display.output_paths)
  if (outputPaths.length > 0) {
    const preview = outputPaths.slice(0, 3).join(', ')
    const suffix = outputPaths.length > 3 ? ` 等 ${outputPaths.length} 个` : `${outputPaths.length} 个`
    return `生成文件 ${suffix}：${preview}`
  }
  const failure = failureSummary(eventName, data, display)
  if (failure && isFailureResultEvent(eventName)) return failure
  if (actionResult.structured_content && typeof actionResult.structured_content === 'object') {
    return objectPreview(actionResult.structured_content)
  }
  if (actionResult.content) return readableToolContent(actionResult.content)
  if (actionResult.result_json && typeof actionResult.result_json === 'object') return objectPreview(actionResult.result_json)
  if (display.summary && !isGenericToolExecutionSummary(display.summary)) return String(display.summary)
  if (display.status) return String(display.status)
  if (data.structured && typeof data.structured === 'object') return objectPreview(data.structured)
  if (Array.isArray(data.content)) return readableContentParts(data.content)
  if (data.error || data.message || data.code) {
    const errorMsg = asRecord(data.error)
    return firstString(errorMsg.message, data.message, typeof data.error === 'string' ? data.error : '', data.code)
  }
  if (data.content) return readableToolContent(data.content)
  if (artifactRefs.length > 0) return `${artifactRefs.length} 个产物`
  if (data.structured_content && typeof data.structured_content === 'object') return objectPreview(data.structured_content)
  if (data.result_json && typeof data.result_json === 'object') return objectPreview(data.result_json)
  if (data.output_json && typeof data.output_json === 'object') return objectPreview(data.output_json)
  if (data.status) return String(data.status)
  if (eventName === 'tool.rejected') return '工具调用已拒绝'
  return objectPreview(data)
}

function isGenericToolExecutionSummary(value) {
  const text = firstString(value)
  return text === '模型请求执行 EasyDo 只读操作' || text === '模型请求执行需要确认的 EasyDo 操作'
}

function readableToolContent(value) {
  const text = firstString(value)
  if (!text) return ''
  if (text.startsWith('{') || text.startsWith('[')) {
    try {
      const parsed = JSON.parse(text)
      if (parsed && typeof parsed === 'object') return objectPreview(parsed)
    } catch {
      return shortValue(text)
    }
  }
  return shortValue(text)
}

function actionTitle(data = {}) {
  const action = asRecord(data.action)
  return compactJoin([
    firstString(data.action_kind, data.capability_id, action.capability_id, data.tool_name),
    inputPreview(data)
  ], ' ')
}

function approvalTitle(data = {}) {
  const approval = asRecord(data.approval_request)
  return firstString(approval.tool_name, approval.capability_id)
}

function approvalSummary(data = {}) {
  const approval = asRecord(data.approval_request)
  return firstString(approval.reason, approval.summary, approval.risk_level)
}

function resultTitle(eventName) {
  if (eventName === 'model_provider.failed') return '模型调用失败'
  if (eventName === 'run.error') return '运行失败'
  if (eventName === 'session.error') return '运行异常'
  if (eventName === 'session.step.ended') return '步骤完成'
  if (eventName === 'session.step.failed') return '步骤失败'
  if (eventName === 'tool.rejected') return '工具已拒绝'
  return 'Result'
}

function isFailureResultEvent(eventName) {
  return [
    'session.step.failed',
    'session.error',
    'run.error',
    'session.tool.failed',
    'tool.failed',
    'action.execution_failed',
    'model_provider.failed'
  ].includes(eventName)
}

function failureSummary(eventName, data = {}, display = {}) {
  const error = asRecord(data.error || display.error)
  // Prefer raw error message over generic display titles so UI never masks upstream failures.
  const message = firstString(
    error.message,
    data.message,
    typeof data.error === 'string' ? data.error : '',
    display.summary,
    data.code,
    error.code,
    error.type
  )
  if (!message) return ''
  if (isCancelledResult(eventName, data, display)) return message
  const retryable = firstBoolean(display.retryable, data.retryable, error.retryable)
  if (retryable === null) return message
  return `${message}${retryable ? '（可重试）' : '（不可重试）'}`
}

function errorTarget(data = {}, display = {}) {
  const error = asRecord(data.error || display.error)
  return firstString(display.code, data.code, error.code, error.type)
}

function isCancelledResult(eventName, data = {}, display = {}) {
  const error = asRecord(data.error)
  const status = firstString(display.status, data.status, error.type, data.code).toLowerCase()
  return (eventName === 'session.error' && data.code === 'user_cancelled') ||
    (eventName === 'session.step.failed' && ['cancelled', 'canceled', 'user_cancelled'].includes(status))
}

function actionStatus(data = {}, display = {}) {
  const decision = firstString(display.decision, data.decision, data.result).toLowerCase()
  if (decision === 'reject' || decision === 'rejected' || decision === 'steer') return 'rejected'
  if (decision === 'approved' || decision === 'approve_once' || decision === 'approve_session' || decision || data.grant_id) return 'success'
  return 'waiting'
}

function approvalDecisionTitle(status) {
  if (status === 'rejected') return '已拒绝'
  if (status === 'success') return '已批准'
  return '已处理'
}

function approvalDecisionSummary(data = {}, display = {}) {
  const decision = firstString(display.decision, data.decision, data.result)
  const reason = firstMeaningfulApprovalText(display.summary, data.reason, data.summary, data.message)
  const actor = firstString(display.decided_by, data.decided_by, data.actor, data.user)
  return compactJoin([approvalDecisionTitle(actionStatus(data, display)), reason, actor, decision], ' · ')
}

function isApprovalPauseToolFailure(event = {}, items = []) {
  if (event.eventName !== 'session.tool.failed' && event.event !== 'session.tool.failed') return false
  const callID = eventCallID(event)
  if (!callID) return false
  // Match same-call approval rows even after they were resolved, so late pause failures stay hidden.
  const relatedApproval = [...items].reverse().find((item) => {
    if (item.lineKind !== 'action') return false
    const itemCallID = firstString(
      item.data?.call_id,
      item.data?.tool_call_id,
      item.data?.provider_tool_call_id,
      item.display?.call_id,
      item.display?.provider_tool_call_id,
      asRecord(item.data?.approval_request).call_id,
      asRecord(item.data?.approval_request).provider_tool_call_id
    )
    return itemCallID === callID
  })
  if (relatedApproval) return true
  const error = asRecord(event.data?.error || event.display?.error)
  const code = firstString(error.code, event.data?.code, event.display?.code)
  return code === 'tool_approval_required' || code === 'approval_required'
}

function eventCallID(event = {}) {
  const data = asRecord(event.data)
  const display = asRecord(event.display)
  const action = asRecord(data.action)
  const input = asRecord(data.input_json || action.input_json)
  const approval = asRecord(data.approval_request || display.approval_request || asRecord(action.display_json).approval_request)
  return firstString(
    data.call_id,
    data.tool_call_id,
    data.provider_tool_call_id,
    input.provider_tool_call_id,
    input.tool_call_id,
    approval.call_id,
    approval.tool_call_id,
    approval.provider_tool_call_id
  )
}

function isGenericToolApprovalText(value) {
  const text = String(value || '').trim()
  if (!text) return true
  if (/^Tool\s+\S+\s+requires approval\.?$/i.test(text)) return true
  if (/^Tool execution requires approval\.?$/i.test(text)) return true
  if (/^模型请求执行需要确认的 EasyDo 操作$/.test(text)) return true
  if (/^需要用户审批$|^等待用户审批$|^待用户批准$/.test(text)) return true
  return false
}

function firstMeaningfulApprovalText(...values) {
  for (const value of values) {
    const text = firstString(value)
    if (text && !isGenericToolApprovalText(text)) return text
  }
  return ''
}

function resultStatus(eventName, data = {}, display = {}) {
  if (eventName === 'action.execution_failed' || eventName === 'tool.failed' || eventName === 'session.tool.failed' || eventName === 'model_provider.failed') return 'failed'
  if (eventName === 'tool.rejected') return 'rejected'
  const status = firstString(display.status, data.status).toLowerCase()
  if (['failed', 'error', 'rejected'].includes(status)) return 'failed'
  return 'success'
}

function subagentStatus(data = {}, display = {}, fallback = 'running') {
  const status = firstString(display.status, data.status).toLowerCase()
  if (['blocked_approval', 'awaiting_approval', 'waiting', 'blocked'].includes(status)) return 'waiting'
  if (['completed', 'success', 'succeeded'].includes(status)) return 'success'
  if (['failed', 'error'].includes(status)) return 'failed'
  if (['cancelled', 'canceled'].includes(status)) return 'cancelled'
  if (status === 'interrupted') return 'interrupted'
  if (['timeout', 'timed_out'].includes(status)) return 'timeout'
  return fallback
}

function isMcpEvent(eventName, data = {}, display = {}) {
  const action = asRecord(data.action)
  const input = asRecord(data.input_json || action.input_json)
  const values = [
    eventName,
    data.source,
    display.source,
    data.server,
    data.mcp_server,
    display.mcp_server,
    data.provider,
    data.tool,
    data.tool_name,
    display.tool_name,
    data.capability_id,
    action.capability_id,
    input.tool_name
  ].map((item) => firstString(item).toLowerCase()).filter(Boolean)
  return values.some((value) =>
    value.includes('mcp') ||
    value.startsWith('easydo.') ||
    value.startsWith('easydo_') ||
    value.includes('/mcp/')
  )
}

function toolName(data = {}, display = {}) {
  const action = asRecord(data.action)
  const input = asRecord(data.input_json || action.input_json)
  const actionDisplay = asRecord(action.display_json)
  const approval = asRecord(data.approval_request || display.approval_request || actionDisplay.approval_request)
  const inputPreview = asRecord(approval.input_preview)
  return firstString(
    display.tool_name,
    data.tool_name,
    data.tool,
    input.tool_name,
    action.capability_id,
    data.capability_id,
    actionDisplay.name,
    approval.tool_name,
    approval.capability_id,
    inputPreview.tool_name,
    display.title
  )
}

function readableContentParts(value) {
  return value
    .map((item) => asRecord(item))
    .map((item) => firstString(item.text, item.content, item.uri))
    .filter(Boolean)
    .map(shortValue)
    .join(' ')
}

function normalizeRuntimeEventWrapper(event) {
  const nested = asRecord(event.data?.event || event.payload?.event)
  if (!nested.__empty) {
    const eventName = firstString(nested.event, nested.type, 'event')
    const display = asRecord(nested.display_json)
    return { raw: nested, data: nested, display, event: eventName, eventName }
  }
  return event
}

function subagentContextKey(event = {}) {
  const data = asRecord(event.data)
  const display = asRecord(event.display)
  return firstString(
    display.child_runtime_run_id,
    data.child_runtime_run_id,
    display.child_run_link_id,
    data.child_run_link_id,
    display.child_run_id,
    data.child_run_id,
    data.parent_action_id,
    data.action_id
  )
}

function appendSubagentProgress(item, event = {}) {
  const progress = Array.isArray(item.subagentProgress) ? [...item.subagentProgress] : []
  const childEventType = firstString(event.data?.child_event_type, event.display?.child_event_type)
  const summary = firstString(
    event.display?.summary,
    event.data?.summary,
    childEventType ? `事件 ${childEventType}` : '',
    event.data?.reason,
    event.data?.result,
    event.data?.status
  )
  if (summary && !progress.includes(summary)) progress.push(summary)
  const maxLines = 12
  item.subagentProgress = progress.length > maxLines ? progress.slice(progress.length - maxLines) : progress
}

function inputPreview(data = {}) {
  const action = asRecord(data.action)
  const input = asRecord(data.input_json || action.input_json)
  const actionDisplay = asRecord(action.display_json)
  const approval = asRecord(data.approval_request || actionDisplay.approval_request)
  const approvalInput = asRecord(approval.input_preview)
  const streamedInput = parseJsonRecord(firstString(data.input_text, data.input_draft))
  const piInput = asRecord(data.input)
  const inputSource = !asRecord(input).__empty ? input : (!streamedInput.__empty ? streamedInput : (!piInput.__empty ? piInput : approvalInput))
  const args = asRecord(inputSource.arguments).__empty ? inputSource : asRecord(inputSource.arguments)
  return keyValuePreview(args)
}

function processInputPreview(lineKind, data = {}) {
  if (!['mcp', 'tool', 'action'].includes(lineKind)) return ''
  return inputPreview(data)
}

function processReasonText(lineKind, data = {}, display = {}, fallback = '') {
  if (!['mcp', 'tool', 'action', 'subagent'].includes(lineKind)) return ''
  const action = asRecord(data.action)
  const actionDisplay = asRecord(action.display_json)
  const approval = asRecord(data.approval_request || display.approval_request || actionDisplay.approval_request)
  const explicit = firstMeaningfulApprovalText(
    display.reason,
    data.invocation_reason,
    data.call_reason,
    data.spawn_reason,
    data.decision_reason,
    data.reason,
    approval.reason,
    approval.summary,
    data.tool_description,
    display.tool_description,
    display.summary,
    data.summary,
    fallback
  )
  if (explicit) return explicit
  if (lineKind === 'action' || lineKind === 'subagent') {
    return firstMeaningfulApprovalText(display.summary, data.summary, fallback)
  }
  return firstMeaningfulApprovalText(fallback)
}

function parseJsonRecord(value) {
  const text = firstString(value)
  if (!text) return { __empty: true }
  try {
    return asRecord(JSON.parse(text))
  } catch {
    return { __empty: true }
  }
}

function targetPreview(data = {}, display = {}) {
  const action = asRecord(data.action)
  const target = asRecord(data.target_json || action.target_json)
  const affected = Array.isArray(data.affected_resources) ? asRecord(data.affected_resources[0]) : {}
  const actionDisplay = asRecord(action.display_json)
  const approval = asRecord(data.approval_request || display.approval_request || actionDisplay.approval_request)
  const approvalAffected = Array.isArray(approval.affected_resources) ? asRecord(approval.affected_resources[0]) : {}
  return compactJoin([
    firstString(display.resource_type, data.resource_type, affected.resource_type, approvalAffected.resource_type, target.target_type, target.type),
    firstString(display.resource_id, data.resource_id, affected.resource_id, approvalAffected.resource_id, target.target_id, target.id)
  ], ' ')
}

function promptSummary(data = {}, display = {}) {
  return shortValue(firstString(display.summary, display.content, data.content, data.text, data.prompt, data.message))
}

function promptTarget(data = {}, display = {}) {
  const attachmentCount = normalizeArray(display.attachments || data.attachments).length
  if (attachmentCount > 0) return `${attachmentCount} 个附件`
  return firstString(display.message_id, data.message_id)
}

function contextEventTitle(eventName) {
  if (eventName === 'context.build_started') return '开始准备上下文'
  if (eventName === 'context_tags.resolved') return '已解析上下文标签'
  if (eventName === 'context.build_completed') return '上下文准备完成'
  if (eventName === 'capability.snapshot') return '运行能力已准备'
  if (eventName === 'mcp.tools.available') return 'MCP 工具已准备'
  if (eventName === 'mcp.tools_discovery_failed') return 'MCP 工具发现失败'
  if (eventName === 'output_schema.available') return '结构化输出约束已准备'
  if (eventName === 'output_schema.validated') return '结构化输出已验证'
  if (eventName === 'output_schema.invalid') return '结构化输出未通过'
  return '上下文'
}

function contextEventTarget(eventName, data = {}, display = {}) {
  if (eventName === 'context.build_started') return firstString(display.profile_name, data.profile_name, display.profile_id, data.profile_id)
  if (eventName === 'context_tags.resolved') {
    const fragments = normalizeArray(display.fragments || data.fragments)
    if (fragments.length > 0) return `${fragments.length} 个片段`
    const tags = normalizeArray(display.tags || data.tags)
    if (tags.length > 0) return `${tags.length} 个标签`
  }
  if (eventName === 'capability.snapshot') {
    const toolCount = resourceCount(display.mcp_tools || data.mcp_tools)
    if (toolCount > 0) return `${toolCount} 个工具`
    const skillCount = resourceCount(display.skills || data.skills)
    if (skillCount > 0) return `${skillCount} 个技能`
  }
  if (eventName === 'mcp.tools.available') {
    const count = Number(display.tool_count ?? data.tool_count)
    const tools = resourceCount(display.tools || data.tools)
    const toolCount = Number.isFinite(count) && count > 0 ? count : tools
    if (toolCount > 0) return `${toolCount} 个工具`
  }
  if (eventName === 'mcp.tools_discovery_failed') {
    return firstString(display.mcp_server, data.mcp_server)
  }
  if (eventName === 'output_schema.available') {
    const required = outputSchemaRequiredFields(data, display)
    if (required.length > 0) return `${required.length} 个必填字段`
    const properties = outputSchemaPropertyNames(data, display)
    if (properties.length > 0) return `${properties.length} 个字段`
  }
  if (eventName === 'output_schema.validated') return firstString(display.status, data.status, '通过')
  if (eventName === 'output_schema.invalid') {
    const errors = normalizeStringArray(display.errors || data.errors)
    if (errors.length > 0) return `${errors.length} 个错误`
  }
  return ''
}

function contextEventSummary(eventName, data = {}, display = {}) {
  if (eventName === 'context.build_started') {
    const tags = normalizeStringArray(display.context_tags || data.context_tags)
    return tags.length ? `标签 ${tags.join(', ')}` : firstString(display.summary, data.summary)
  }
  if (eventName === 'context_tags.resolved') {
    const fragments = normalizeArray(display.fragments || data.fragments)
      .map((item) => asRecord(item))
      .map((item) => compactJoin([item.tag, firstString(item.title, item.name)], ': '))
      .filter(Boolean)
    const warnings = normalizeStringArray(display.warnings || data.warnings)
    return compactJoin([
      fragments.join('；'),
      warnings.length ? `警告 ${warnings.join('；')}` : ''
    ], '；')
  }
  if (eventName === 'context.build_completed') {
    return contextBuildCompletedSummary(data, display)
  }
  if (eventName === 'capability.snapshot') return capabilitySnapshotSummary(data, display)
  if (eventName === 'mcp.tools.available') return mcpToolsAvailableSummary(data, display)
  if (eventName === 'mcp.tools_discovery_failed') {
    return firstString(display.message, data.message, display.error, data.error, 'MCP 工具发现失败')
  }
  if (eventName === 'output_schema.available') return outputSchemaAvailableSummary(data, display)
  if (eventName === 'output_schema.validated') return firstString(display.summary, data.summary, '结构化输出符合约束')
  if (eventName === 'output_schema.invalid') {
    const errors = normalizeStringArray(display.errors || data.errors)
    return errors.length ? errors.slice(0, 3).join('；') : firstString(display.summary, data.summary, '结构化输出不符合约束')
  }
  return firstString(display.summary, data.summary)
}

function contextBuildCompletedSummary(data = {}, display = {}) {
  const parts = []
  const tagCount = Number(data.context_tag_count ?? display.context_tag_count)
  const fragmentCount = Number(data.context_fragment_count ?? display.context_fragment_count)
  const skillCount = Number(data.skill_count ?? display.skill_count)
  const mcpToolCount = Number(data.mcp_tool_count ?? display.mcp_tool_count)
  const subagentResultCount = Number(data.subagent_result_count ?? display.subagent_result_count)
  const modelContentChars = Number(data.model_content_chars ?? display.model_content_chars)
  if (Number.isFinite(tagCount) && tagCount > 0) parts.push(`标签 ${tagCount} 个`)
  if (Number.isFinite(fragmentCount) && fragmentCount > 0) parts.push(`片段 ${fragmentCount} 个`)
  if (Number.isFinite(skillCount) && skillCount > 0) parts.push(`技能 ${skillCount} 个`)
  if (Number.isFinite(mcpToolCount) && mcpToolCount > 0) parts.push(`MCP 工具 ${mcpToolCount} 个`)
  if (Number.isFinite(subagentResultCount) && subagentResultCount > 0) parts.push(`子结果 ${subagentResultCount} 个`)
  if (Number.isFinite(modelContentChars) && modelContentChars > 0) parts.push(`模型上下文 ${modelContentChars} 字符`)
  if (data.has_output_schema ?? display.has_output_schema) parts.push('结构化输出')
  return parts.join('，') || '上下文已准备'
}

function capabilitySnapshotSummary(data = {}, display = {}) {
  const parts = []
  const skillCount = resourceCount(display.skills || data.skills)
  const serverCount = resourceCount(display.mcp_servers || data.mcp_servers)
  const toolCount = resourceCount(display.mcp_tools || data.mcp_tools)
  const subagentCount = resourceCount(display.subagents || data.subagents)
  const controls = asRecord(display.l5_controls || data.l5_controls)
  if (skillCount > 0) parts.push(`技能 ${skillCount} 个`)
  if (serverCount > 0) parts.push(`MCP 服务 ${serverCount} 个`)
  if (toolCount > 0) parts.push(`工具 ${toolCount} 个`)
  if (subagentCount > 0) parts.push(`子 Agent ${subagentCount} 个`)
  if (controls.output_schema_validation) parts.push('结构化输出校验')
  if (controls.write_tool_approval) parts.push('审批控制')
  return parts.join('，') || '运行能力已准备'
}

function mcpToolsAvailableSummary(data = {}, display = {}) {
  const tools = normalizeArray(display.tools || data.tools)
    .map((item) => asRecord(item))
    .map((item) => firstString(item.name, item.tool_name, item.id))
    .filter(Boolean)
  if (tools.length > 0) {
    const preview = tools.slice(0, 4).join(', ')
    return tools.length > 4 ? `${preview} 等 ${tools.length} 个工具` : preview
  }
  const count = Number(display.tool_count ?? data.tool_count)
  if (Number.isFinite(count) && count > 0) return `可调用 MCP 工具 ${count} 个`
  return firstString(display.summary, data.summary, 'MCP 工具已准备')
}

function outputSchemaAvailableSummary(data = {}, display = {}) {
  const required = outputSchemaRequiredFields(data, display)
  if (required.length > 0) return `必填 ${required.slice(0, 5).join(', ')}`
  const properties = outputSchemaPropertyNames(data, display)
  if (properties.length > 0) return `字段 ${properties.slice(0, 5).join(', ')}`
  return firstString(display.summary, data.summary, '模型回复需要符合结构化输出约束')
}

function outputSchemaRequiredFields(data = {}, display = {}) {
  const schema = asRecord(display.schema || data.schema)
  return normalizeStringArray(schema.required)
}

function outputSchemaPropertyNames(data = {}, display = {}) {
  const schema = asRecord(display.schema || data.schema)
  const properties = asRecord(schema.properties)
  return Object.keys(properties).filter((key) => key !== '__empty')
}

function resourceCount(value) {
  return normalizeArray(value).filter((item) => item !== undefined && item !== null).length
}

function modelEventTitle(eventName, data = {}, display = {}) {
  if (eventName === 'model.call_started') return '模型请求已开始'
  if (eventName === 'model.call_completed') return '模型请求已完成'
  if (eventName === 'model.continuation_started') return '模型续写已开始'
  if (eventName === 'model.continuation_completed') return '模型续写已完成'
  if (eventName === 'run.started') return '运行已开始'
  if (eventName === 'run.completed') return '运行已完成'
  if (eventName === 'run.failed') return '运行失败'
  if (eventName === 'run.cancelled') return '运行已取消'
  if (eventName === 'run.timeout') return '运行超时'
  if (eventName === 'run.awaiting_decision') return '等待确认'
  if (eventName === 'run.interrupted') return '运行已中断'
  if (eventName === 'context.budget.evaluated') return '已评估上下文预算'
  if (eventName === 'session.compaction.started' || eventName === 'context.compaction.started') return '正在整理上下文'
  if (eventName === 'session.compaction.ended' || eventName === 'context.compaction.completed') return '已整理上下文'
  if (eventName === 'context.compaction.failed') return '上下文整理失败'
  if (eventName === 'orchestration.context.assembled') return '已组装多 Agent 上下文'
  if (eventName === 'orchestration.task.dispatched') return '已派发子任务'
  if (eventName === 'orchestration.task.status_changed') return '子任务状态更新'
  if (eventName === 'orchestration.task.completed') return '子任务已完成'
  if (eventName === 'orchestration.task.failed') return '子任务失败'
  if (eventName === 'orchestration.synthesis.started') return '正在汇总子任务结果'
  if (eventName === 'orchestration.review.started') return '正在检查子任务结果'
  if (eventName === 'orchestration.followup.dispatched') return '已派发 follow-up 子任务'
  if (eventName === 'orchestration.completed') return '多 Agent 编排完成'
  if (eventName === 'session.agent.switched') return '已切换 Agent'
  if (eventName === 'session.model.switched') return '已切换模型'
  if (eventName === 'provider.empty_output_detected') return '模型空输出重试'
  if (eventName === 'provider.continuation_completed') return '模型重试已恢复'
  if (eventName === 'model.answer_synthesized') return '已根据工具结果生成回复'
  if (eventName === 'model.tool_result_submitted') return '工具结果已提交给模型'
  if (eventName === 'model.request_prepared') {
    const phase = readablePhase(firstString(data.phase, display.phase))
    const round = firstString(data.round, display.round)
    return compactJoin(['已准备上下文', phase, round ? `#${round}` : ''], ' ')
  }
  return '模型事件'
}

function modelEventSummary(eventName, data = {}, display = {}) {
  if (eventName === 'model.call_started' || eventName === 'model.continuation_started') {
    return firstString(display.summary, modelEventTarget(data, display))
  }
  if (eventName === 'model.call_completed' || eventName === 'model.continuation_completed') {
    const toolCallCount = Number(data.tool_call_count ?? display.tool_call_count)
    const textChars = Number(data.text_chars ?? display.text_chars)
    const reasoningChars = Number(data.reasoning_chars ?? display.reasoning_chars)
    const status = firstString(display.status, data.status).toLowerCase()
    if (Number.isFinite(toolCallCount) && toolCallCount > 0) return `请求工具 ${toolCallCount} 个`
    if (status === 'empty' && Number.isFinite(reasoningChars) && reasoningChars > 0) return '模型仅返回内部推理'
    if (Number.isFinite(textChars) && textChars > 0) return `生成 ${textChars} 字符`
    return firstString(display.summary, data.status)
  }
  if (eventName === 'run.started') return firstString(display.summary, data.summary, compactJoin(['runtime', data.status || display.status], ' '), 'runtime running')
  if (RUN_STATE_EVENTS.has(eventName)) return firstString(display.summary, data.summary, data.message, data.code, data.status, display.status)
  if (eventName === 'context.budget.evaluated') return firstString(display.summary, formatContextBudgetSummary(data, display), '已按当前绑定模型窗口评估上下文预算')
  if (eventName === 'session.compaction.started' || eventName === 'context.compaction.started') {
    return firstString(display.summary, formatContextCompactionSummary(data, display, 'start'), data.reason, '上下文过长，正在压缩历史信息')
  }
  if (eventName === 'session.compaction.ended' || eventName === 'context.compaction.completed') {
    return firstString(display.summary, formatContextCompactionSummary(data, display, 'end'), data.summary, data.recent, '上下文整理完成')
  }
  if (eventName === 'context.compaction.failed') {
    return firstString(display.summary, data.error, data.message, '上下文整理失败')
  }
  if (eventName.startsWith('orchestration.')) {
    return firstString(display.summary, formatOrchestrationSummary(eventName, data, display), data.result_summary, data.final_summary, data.error_msg)
  }
  if (eventName === 'session.agent.switched') {
    const agent = firstString(display.agent, data.agent)
    return firstString(display.summary, agent ? `后续步骤由 ${agent} 处理` : '后续步骤已切换到新的 Agent')
  }
  if (eventName === 'session.model.switched') {
    const model = modelEventTarget(data, display)
    return firstString(display.summary, model ? `后续模型调用使用 ${model}` : '后续模型调用已切换')
  }
  if (eventName === 'provider.empty_output_detected') {
    const status = firstString(display.status, data.status)
    return status === 'retrying' ? '模型仅返回内部推理，正在重新请求可见回复' : '模型返回为空'
  }
  if (eventName === 'provider.continuation_completed') {
    const attempt = Number(data.attempt ?? display.attempt)
    if (Number.isFinite(attempt) && attempt > 0) return `第 ${attempt} 次请求已返回可见回复`
    return firstString(display.summary, data.status, '模型重试已返回可见回复')
  }
  if (eventName === 'model.answer_synthesized') {
    const count = Number(data.tool_result_count ?? display.tool_result_count)
    const countText = Number.isFinite(count) && count > 0 ? `${count} 个` : '已有'
    return `模型未返回可见文本，已用 ${countText}工具结果生成最终回复`
  }
  if (eventName === 'model.tool_result_submitted') {
    const count = Number(data.tool_result_count ?? display.tool_result_count)
    const countText = Number.isFinite(count) && count > 0 ? `${count} 个` : '已有'
    return `已提交 ${countText}工具结果，等待模型继续处理`
  }
  if (eventName === 'model.request_prepared') return preparedContextSummary(data, display)
  return firstString(display.summary, data.status)
}

function preparedContextSummary(data = {}, display = {}) {
  const parts = []
  const contentChars = Number(data.content_chars ?? display.content_chars)
  const historyCount = Number(data.history_count ?? display.history_count)
  const toolCount = Number(data.tool_count ?? display.tool_count)
  const skillCount = Number(data.loaded_skill_count ?? display.loaded_skill_count)
  const subagentResultCount = Number(data.subagent_result_count ?? display.subagent_result_count)
  if (Number.isFinite(contentChars) && contentChars > 0) parts.push(`上下文 ${contentChars} 字符`)
  if (Number.isFinite(historyCount) && historyCount > 0) parts.push(`历史 ${historyCount} 条`)
  if (Number.isFinite(toolCount) && toolCount > 0) parts.push(`工具 ${toolCount} 个`)
  if (Number.isFinite(skillCount) && skillCount > 0) parts.push(`技能 ${skillCount} 个`)
  if (Number.isFinite(subagentResultCount) && subagentResultCount > 0) parts.push(`子结果 ${subagentResultCount} 个`)
  if (data.has_output_schema ?? display.has_output_schema) parts.push('结构化输出')
  return parts.join('，') || '上下文已准备'
}

function formatContextBudgetSummary(data = {}, display = {}) {
  const provider = firstString(data.provider_id, display.provider_id)
  const model = firstString(data.model_key, display.model_key)
  const windowTokens = Number(data.context_window_tokens ?? display.context_window_tokens)
  const currentTokens = Number(data.current_context_tokens ?? display.current_context_tokens)
  const thresholdTokens = Number(data.threshold_tokens ?? display.threshold_tokens)
  const parts = []
  if (provider || model) parts.push(compactJoin([provider, model], '/'))
  if (Number.isFinite(windowTokens) && windowTokens > 0) parts.push(`窗口 ${windowTokens}`)
  if (Number.isFinite(currentTokens) && currentTokens > 0) parts.push(`当前 ${currentTokens}`)
  if (Number.isFinite(thresholdTokens) && thresholdTokens > 0) parts.push(`阈值 ${thresholdTokens}`)
  if (data.should_compact ?? display.should_compact) parts.push('需压缩')
  return parts.join(' · ')
}

function formatContextCompactionSummary(data = {}, display = {}, phase = 'end') {
  const before = Number(data.before_tokens ?? display.before_tokens)
  const after = Number(data.after_tokens ?? display.after_tokens)
  const windowTokens = Number(data.context_window_tokens ?? display.context_window_tokens)
  const compactedCount = Number(data.compacted_message_count ?? display.compacted_message_count)
  const provider = firstString(data.provider_id, display.provider_id)
  const model = firstString(data.model_key, display.model_key)
  const parts = []
  if (provider || model) parts.push(compactJoin([provider, model], '/'))
  if (Number.isFinite(windowTokens) && windowTokens > 0) parts.push(`窗口 ${windowTokens}`)
  if (Number.isFinite(before) && before > 0 && Number.isFinite(after) && after >= 0) {
    parts.push(phase === 'start' ? `预计 ${before}→${after}` : `${before}→${after} tokens`)
  } else if (Number.isFinite(before) && before > 0) {
    parts.push(`当前 ${before} tokens`)
  }
  if (Number.isFinite(compactedCount) && compactedCount > 0) parts.push(`压缩 ${compactedCount} 条`)
  return parts.join(' · ')
}

function formatOrchestrationSummary(eventName, data = {}, display = {}) {
  const taskID = firstString(data.task_id, display.task_id)
  const status = firstString(data.status, display.status)
  const name = firstString(data.assigned_subagent_name, display.assigned_subagent_name, data.name)
  const count = Number(data.task_count ?? display.task_count)
  const completed = Number(data.completed_task_count ?? display.completed_task_count)
  const failed = Number(data.failed_task_count ?? display.failed_task_count)
  const parts = []
  if (taskID) parts.push(taskID)
  if (name) parts.push(name)
  if (status) parts.push(status)
  if (eventName === 'orchestration.context.assembled' && Number.isFinite(count) && count > 0) parts.push(`${count} 个任务`)
  if (eventName === 'orchestration.completed') {
    if (Number.isFinite(completed)) parts.push(`完成 ${completed}`)
    if (Number.isFinite(failed)) parts.push(`失败 ${failed}`)
  }
  return parts.join(' · ')
}

function orchestrationEventTitle(eventName) {
  if (eventName === 'orchestration.context.assembled') return '已组装多 Agent 上下文'
  if (eventName === 'orchestration.task.dispatched') return '已派发子任务'
  if (eventName === 'orchestration.task.status_changed') return '子任务状态更新'
  if (eventName === 'orchestration.task.completed') return '子任务已完成'
  if (eventName === 'orchestration.task.failed') return '子任务失败'
  if (eventName === 'orchestration.synthesis.started') return '正在汇总子任务结果'
  if (eventName === 'orchestration.review.started') return '正在检查子任务结果'
  if (eventName === 'orchestration.followup.dispatched') return '已派发 follow-up 子任务'
  if (eventName === 'orchestration.completed') return '多 Agent 编排完成'
  return '多 Agent 编排'
}

function modelEventTarget(data = {}, display = {}) {
  if (data.runtime_run_id || display.runtime_run_id) return firstString(data.runtime_run_id, display.runtime_run_id)
  const model = asRecord(data.model || display.model)
  const modelName = firstString(model.id, data.model, display.model)
  const provider = firstString(model.provider_id, data.provider, display.provider)
  return firstString(
    firstString(display.agent, data.agent),
    provider && modelName ? `${provider}/${modelName}` : modelName,
    provider
  )
}

function readablePhase(value) {
  const text = firstString(value)
  if (!text) return ''
  return text.replace(/_/g, ' ')
}

function keyValuePreview(value = {}) {
  const entries = Object.entries(value)
    .filter(([key, entryValue]) => key !== '__empty' && entryValue !== undefined && entryValue !== null && String(entryValue).trim())
    .slice(0, 4)
  return entries.map(([key, entryValue]) => `${key}=${shortValue(entryValue)}`).join(' ')
}

function objectPreview(value = {}) {
  const preview = keyValuePreview(value)
  if (preview) return preview
  return Object.keys(value).slice(0, 3).join(', ')
}

function normalizeInputEvents(events = []) {
  if (!Array.isArray(events)) return []
  return events
    .map((item, sourceIndex) => {
      const data = asRecord(item?.data || item?.payload || item)
      const display = asRecord(item?.display_json || data.display_json)
      const eventName = firstString(item?.event, item?.type, display.event_type, data.event_type, 'event')
      return {
        raw: item,
        data,
        display,
        event: eventName,
        eventName,
        sourceIndex,
        eventSeq: eventSequenceValue(item, data, display),
        eventTime: eventTimestampValue(item, data, display)
      }
    })
    .sort((left, right) => {
      if (left.eventSeq !== right.eventSeq) {
        if (left.eventSeq === null) return 1
        if (right.eventSeq === null) return -1
        return left.eventSeq - right.eventSeq
      }
      if (left.eventTime !== right.eventTime) {
        if (left.eventTime === null) return 1
        if (right.eventTime === null) return -1
        return left.eventTime - right.eventTime
      }
      return left.sourceIndex - right.sourceIndex
    })
}

function eventSequenceValue(item = {}, data = {}, display = {}) {
  const value = Number(
    firstString(
      item.event_seq,
      item.seq,
      data.event_seq,
      data.seq,
      display.event_seq,
      display.seq
    )
  )
  return Number.isFinite(value) ? value : null
}

function eventTimestampValue(item = {}, data = {}, display = {}) {
  const raw = firstString(item.timestamp, item.created_at, data.timestamp, data.created_at, display.timestamp)
  const value = Date.parse(raw)
  return Number.isFinite(value) ? value : null
}

function compactStreamingDeltaEvents(events = []) {
  const compacted = []
  events.forEach((event) => {
    if (!COMPACT_DELTA_EVENTS.has(event.eventName)) {
      compacted.push(event)
      return
    }
    const previous = compacted[compacted.length - 1]
    if (!previous || previous.eventName !== event.eventName || streamingDeltaKey(previous) !== streamingDeltaKey(event)) {
      compacted.push(event)
      return
    }
    previous.data = {
      ...previous.data,
      ...event.data,
      delta: `${rawString(previous.data.delta)}${rawString(event.data.delta)}`
    }
    previous.display = { ...previous.display, ...event.display }
    previous.raw = event.raw || previous.raw
  })
  return compacted
}

function streamingDeltaKey(event = {}) {
  const data = asRecord(event.data)
  return firstString(
    data.reasoning_id,
    data.text_id,
    data.tool_call_id,
    data.call_id,
    data.provider_tool_call_id,
    data.assistant_message_id,
    event.eventName
  )
}

function normalizeArtifactRefs(value) {
  if (!Array.isArray(value)) return []
  return value.map((item) => asRecord(item)).filter((item) => firstString(item.artifact_id))
}

function eventArtifactRefs(data = {}, display = {}) {
  const explicitRefs = normalizeArtifactRefs(display.artifact_refs).length
    ? normalizeArtifactRefs(display.artifact_refs)
    : normalizeArtifactRefs(data.artifact_refs)
  return mergeArtifactRefs(explicitRefs, outputPathArtifactRefs(data.output_paths || display.output_paths))
}

function outputPathArtifactRefs(value) {
  return normalizeOutputPaths(value).map((path) => ({
    artifact_id: `file:${path}`,
    artifact_type: 'file_output',
    preview_json: { path }
  }))
}

function normalizeOutputPaths(value) {
  if (!Array.isArray(value)) return []
  return [...new Set(value.map((item) => firstString(item)).filter(Boolean))]
}

function normalizeArray(value) {
  if (Array.isArray(value)) return value
  if (value === undefined || value === null) return []
  return [value]
}

function normalizeStringArray(value) {
  return normalizeArray(value).map((item) => firstString(item)).filter(Boolean)
}

function rawString(value) {
  return value === undefined || value === null ? '' : String(value)
}

function queueItemContent(data = {}) {
  const queueItem = asRecord(data.queue_item)
  return firstString(queueItem.content, data.content, data.message, data.queue_item_id)
}

function asRecord(value) {
  if (value && typeof value === 'object' && !Array.isArray(value)) return value
  return { __empty: true }
}

function firstString(...values) {
  for (const value of values) {
    if (value !== undefined && value !== null && String(value).trim()) return String(value).trim()
  }
  return ''
}

function firstBoolean(...values) {
  for (const value of values) {
    if (typeof value === 'boolean') return value
    const text = firstString(value).toLowerCase()
    if (text === 'true') return true
    if (text === 'false') return false
  }
  return null
}

function compactJoin(values, separator = ' · ') {
  return values.map((item) => firstString(item)).filter(Boolean).join(separator)
}

function shortValue(value) {
  const text = typeof value === 'object' ? JSON.stringify(value) : String(value)
  return text.length > 48 ? `${text.slice(0, 45)}...` : text
}

function formatTiming(value) {
  const number = Number(value)
  if (!Number.isFinite(number)) return ''
  return formatDuration(number)
}

function formatDuration(value) {
  if (!Number.isFinite(value)) return ''
  if (value < 1000) return `${Math.max(0, Math.round(value))}ms`
  return `${(value / 1000).toFixed(1)}s`
}
