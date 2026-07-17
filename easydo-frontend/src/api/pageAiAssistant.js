import request from './request'
import { waitForBrowserPaint } from './streamPaint'
import { fetchAgentRuntime, readAgentRuntimeSseResponse } from './agentRuntimeSse'
import { normalizeAgentRuntimeError } from '../utils/agentRuntimeError'

export const PAGE_ASSISTANT_PROFILE_NAME = 'page-ai-assistant'
const PAGE_ASSISTANT_SEND_TIMEOUT = 180000
// Batch several visible SSE events per paint frame to keep tool-heavy runs responsive.
const STREAM_EVENTS_PER_FRAME = 12
const SUPPORTED_ACTION_DECISIONS = new Set(['approve_once', 'approve_session', 'reject', 'steer'])
const VISIBLE_PROCESS_EVENTS = new Set([
  'reasoning_delta',
  'answer_delta',
  'run.started',
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
  'action.execution_started',
  'tool.result_prepared',
  'action.execution_succeeded',
  'action.execution_failed',
  'tool.executed',
  'tool.failed',
  'tool.rejected',
  'action.decision_required',
  'action.decision',
  'action.approved',
  'action.rejected',
  'approval.requested',
  'approval.session_granted',
  'model_provider.failed',
  'output_schema.validated',
  'output_schema.invalid',
  'subagent.started',
  'subagent.spawned',
  'subagent.progress',
  'subagent.blocked_approval',
  'subagent.completed',
  'subagent.failed',
  'subagent.cancelled',
  'subagent.interrupted',
  'subagent.timeout'
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
OPENCODE_PROCESS_EVENTS.forEach((eventName) => VISIBLE_PROCESS_EVENTS.add(eventName))
const VISIBLE_PROCESS_EVENT_PREFIXES = [
  'skill.',
  'subagent.'
]

function authHeaders() {
  const headers = {
    'Content-Type': 'application/json'
  }
  const token = localStorage.getItem('token')
  const workspaceId = localStorage.getItem('current_workspace_id')
  if (token) headers.Authorization = `Bearer ${token}`
  if (workspaceId) headers['X-Workspace-ID'] = workspaceId
  return headers
}

function isVisibleStreamEvent(event) {
  return VISIBLE_PROCESS_EVENTS.has(event) || VISIBLE_PROCESS_EVENT_PREFIXES.some((prefix) => event.startsWith(prefix))
}

function visibleStreamEventName(event, payload = {}) {
  if (event !== 'runtime_event') return event
  return payload?.event?.type || payload?.event?.event || event
}

function normalizeActionDecision(decision) {
  const normalized = String(decision || 'approve_once').trim()
  return SUPPORTED_ACTION_DECISIONS.has(normalized) ? normalized : 'approve_once'
}

function runtimeFetch(input, init, fallbackMessage, phase = 'fetch') {
  return fetchAgentRuntime(input, init, { fallbackMessage, phase })
}

async function readSseResponse(response, onEvent, fallbackMessage) {
  return readAgentRuntimeSseResponse(response, onEvent, {
    fallbackMessage,
    emptyMessage: '页面助手流式响应为空',
    protocolMessage: '页面助手收到无法解析的事件',
    isVisibleEvent: isVisibleStreamEvent,
    visibleEventName: visibleStreamEventName,
    waitForPaint: waitForBrowserPaint,
    eventsPerFrame: STREAM_EVENTS_PER_FRAME
  })
}

export function getCurrentAiSession(data) {
  return request({
    url: '/ai/sessions/current',
    method: 'post',
    data
  })
}

export function getAiSession(sessionId) {
  return request({
    url: `/ai/sessions/${sessionId}`,
    method: 'get'
  })
}

export function listPageAssistantProfiles(params = {}) {
  return request({
    url: '/store/ai-agents/profiles',
    method: 'get',
    params: {
      ...params,
      profile_name: PAGE_ASSISTANT_PROFILE_NAME
    }
  })
}

export function listAiSessions(params) {
  return request({
    url: '/ai/sessions',
    method: 'get',
    params
  })
}

export function listAiSessionEntries(sessionId) {
  return request({
    url: `/ai/sessions/${sessionId}/entries`,
    method: 'get'
  })
}

export function sendPageAssistantMessage(sessionId, data) {
  return request({
    url: `/ai/sessions/${sessionId}/entries`,
    method: 'post',
    data,
    timeout: PAGE_ASSISTANT_SEND_TIMEOUT
  })
}

export function cancelPageAssistantSession(sessionId, data = {}) {
  return request({
    url: `/ai/sessions/${sessionId}/cancel`,
    method: 'post',
    data
  })
}

export function updatePageAssistantSessionModel(sessionId, data = {}) {
  return request({
    url: `/ai/sessions/${sessionId}/model`,
    method: 'put',
    data
  })
}

export async function sendPageAssistantMessageStream(sessionId, data, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const response = await runtimeFetch(`/api/ai/sessions/${sessionId}/entries/stream`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(data),
    signal
  }, '页面助手流式请求失败', 'stream_open')
  await readSseResponse(response, onEvent, '页面助手流式请求失败')
  return {
    abort: () => controller.abort()
  }
}

export async function streamPageAssistantActionDecision(id, decision, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const runtimeDecision = normalizeActionDecision(decision)
  const clientDecisionId = options.clientDecisionId || `action-${id}-${runtimeDecision}`
  const path = `/api/ai/actions/${id}/decision/stream`
  const response = await runtimeFetch(path, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ decision: runtimeDecision, client_decision_id: clientDecisionId }),
    signal
  }, '页面助手动作决策失败', 'stream_open')
  await readSseResponse(response, onEvent, '页面助手动作决策失败')
  return {
    abort: () => controller.abort()
  }
}

export async function streamPageAssistantPiApproval(runtimeRunId, decision, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const runtimeDecision = normalizeActionDecision(decision)
  const clientDecisionId = options.clientDecisionId || `pi-${runtimeRunId}-${runtimeDecision}`
  const path = `/api/ai/runs/${encodeURIComponent(runtimeRunId)}/pi-approval/decision/stream`
  const approval = options.approval || {}
  const response = await runtimeFetch(path, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({
      decision: runtimeDecision,
      client_decision_id: clientDecisionId,
      approval_id: approval.approval_id,
      request_id: approval.request_id,
      call_id: approval.call_id,
      tool_name: approval.tool_name,
      steer_text: options.steerText || ''
    }),
    signal
  }, '页面助手 Pi 工具审批失败', 'stream_open')
  await readSseResponse(response, onEvent, '页面助手 Pi 工具审批失败')
  return {
    abort: () => controller.abort()
  }
}

export async function streamPageAssistantRunEvents(runtimeRunId, afterEventId, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const path = `/api/ai/runs/${encodeURIComponent(runtimeRunId)}/events/stream`
  const response = await runtimeFetch(path, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ after_event_id: afterEventId || '' }),
    signal
  }, '页面助手事件续传失败', 'stream_open')
  await readSseResponse(response, onEvent, '页面助手事件续传失败')
  return {
    abort: () => controller.abort()
  }
}

export async function decidePageAssistantPiApproval(runtimeRunId, decision, options = {}) {
  const runtimeDecision = normalizeActionDecision(decision)
  const clientDecisionId = options.clientDecisionId || `pi-${runtimeRunId}-${runtimeDecision}`
  return request({
    url: `/ai/runs/${encodeURIComponent(runtimeRunId)}/pi-approval/decision`,
    method: 'post',
    data: { decision: runtimeDecision, client_decision_id: clientDecisionId }
  })
}

export async function listPageAssistantRunEvents(runtimeRunId, afterEventId = '', context = {}) {
  if (afterEventId && typeof afterEventId === 'object') {
    context = afterEventId
    afterEventId = ''
  }
  const params = new URLSearchParams()
  if (afterEventId) params.set('after_event_id', afterEventId)
  for (const key of ['parent_runtime_run_id', 'parent_action_id', 'child_run_link_id', 'parent_entry_id']) {
    const value = context?.[key]
    if (value !== undefined && value !== null && String(value).trim()) {
      params.set(key, String(value).trim())
    }
  }
  const query = params.toString()
  const response = await runtimeFetch(`/api/ai/runs/${encodeURIComponent(runtimeRunId)}/events${query ? `?${query}` : ''}`, {
    method: 'GET',
    headers: authHeaders()
  }, '页面助手事件回放失败', 'event_replay')
  if (!response.ok) {
    const text = await response.text()
    throw normalizeAgentRuntimeError(text, '页面助手事件回放失败', {
      category: response.status === 429 || response.status >= 500 ? 'transport' : 'http',
      retryable: response.status === 429 || response.status >= 500,
      http_status: response.status,
      phase: 'event_replay'
    })
  }
  return response.json()
}

function artifactContextParams(context = {}) {
  const params = new URLSearchParams()
  for (const key of ['parent_runtime_run_id', 'parent_action_id', 'child_run_link_id', 'parent_entry_id']) {
    const value = context?.[key]
    if (value !== undefined && value !== null && String(value).trim()) {
      params.set(key, String(value).trim())
    }
  }
  const query = params.toString()
  return query ? `?${query}` : ''
}

export async function getPageAssistantArtifact(artifactId, context = {}) {
  const response = await runtimeFetch(`/api/ai/artifacts/${encodeURIComponent(artifactId)}${artifactContextParams(context)}`, {
    method: 'GET',
    headers: authHeaders()
  }, '页面助手产物加载失败', 'artifact_load')
  if (!response.ok) {
    const text = await response.text()
    throw normalizeAgentRuntimeError(text, '页面助手产物加载失败', {
      category: response.status === 429 || response.status >= 500 ? 'transport' : 'http',
      retryable: response.status === 429 || response.status >= 500,
      http_status: response.status,
      phase: 'artifact_load'
    })
  }
  return response.json()
}
