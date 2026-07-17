import request from './request'
import { waitForBrowserPaint } from './streamPaint'
import { fetchAgentRuntime, readAgentRuntimeSseResponse } from './agentRuntimeSse'
import { normalizeAgentRuntimeError } from '../utils/agentRuntimeError'

export {
  cancelRuntimeSessionQueueItem as cancelAgentChatboxQueueItem,
  enqueueRuntimeSessionQueueItem as enqueueAgentChatboxQueueItem,
  listRuntimeSessionQueueItems as listAgentChatboxQueueItems,
  reorderRuntimeSessionQueueItems as reorderAgentChatboxQueueItems
} from './runtimeSessionQueue.js'

const AGENT_CHATBOX_SEND_TIMEOUT = 180000
// Batch several visible SSE events per paint frame. Tool-heavy Pi runs emit
// hundreds of deltas; painting after every event freezes the chat UI.
const STREAM_EVENTS_PER_FRAME = 12
const SUPPORTED_ACTION_DECISIONS = new Set(['approve_once', 'approve_session', 'reject', 'steer'])
const VISIBLE_PROCESS_EVENTS = new Set([
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
  'action.execution_started',
  'tool.result_prepared',
  'action.execution_succeeded',
  'action.execution_failed',
  'tool.executed',
  'tool.failed',
  'tool.rejected',
  'model_provider.failed',
  'action.decision_required',
  'action.decision',
  'action.approved',
  'action.rejected',
  'approval.requested',
  'approval.session_granted',
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
    emptyMessage: 'Agent Chatbox 流式响应为空',
    protocolMessage: 'Agent Chatbox 收到无法解析的事件',
    isVisibleEvent: isVisibleStreamEvent,
    visibleEventName: visibleStreamEventName,
    waitForPaint: waitForBrowserPaint,
    eventsPerFrame: STREAM_EVENTS_PER_FRAME
  })
}

export function listAgentChatboxProfiles(params) {
  return request({
    url: '/ai/agent-chatbox/profiles',
    method: 'get',
    params
  })
}

export function openAgentChatboxSession(data) {
  return request({
    url: '/ai/agent-chatbox/sessions/open',
    method: 'post',
    data
  })
}

export function createAgentChatboxSession(data) {
  return request({
    url: '/ai/agent-chatbox/sessions',
    method: 'post',
    data
  })
}

export function listAgentChatboxSessions(params) {
  return request({
    url: '/ai/agent-chatbox/sessions',
    method: 'get',
    params
  })
}

export function getAgentChatboxSession(sessionId) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}`,
    method: 'get'
  })
}

export function listAgentChatboxEntries(sessionId) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/entries`,
    method: 'get'
  })
}

export function cancelAgentChatboxSession(sessionId, data = {}) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/cancel`,
    method: 'post',
    data
  })
}

export function archiveAgentChatboxSession(sessionId) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/archive`,
    method: 'post'
  })
}

export function updateAgentChatboxSessionModel(sessionId, data = {}) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/model`,
    method: 'put',
    data
  })
}

export async function sendAgentChatboxMessageStream(sessionId, data, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const response = await runtimeFetch(`/api/ai/agent-chatbox/sessions/${sessionId}/entries/stream`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(data),
    signal
  }, 'Agent Chatbox 流式请求失败', 'stream_open')
  await readSseResponse(response, onEvent, 'Agent Chatbox 流式请求失败')
  return {
    abort: () => controller.abort()
  }
}

export async function continueAgentChatboxSessionStream(sessionId, data, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const response = await runtimeFetch(`/api/ai/agent-chatbox/sessions/${sessionId}/continue/stream`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(data || {}),
    signal
  }, 'Agent Chatbox 继续生成失败', 'stream_open')
  await readSseResponse(response, onEvent, 'Agent Chatbox 继续生成失败')
  return {
    abort: () => controller.abort()
  }
}

export async function streamAgentChatboxActionDecision(id, decision, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const runtimeDecision = normalizeActionDecision(decision)
  const clientDecisionId = options.clientDecisionId || `action-${id}-${runtimeDecision}`
  const path = `/api/ai/agent-chatbox/actions/${id}/decision/stream`
  const response = await runtimeFetch(path, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ decision: runtimeDecision, client_decision_id: clientDecisionId }),
    signal
  }, 'Agent Chatbox 动作决策失败', 'stream_open')
  await readSseResponse(response, onEvent, 'Agent Chatbox 动作决策失败')
  return {
    abort: () => controller.abort()
  }
}

export async function streamAgentChatboxPiApproval(runtimeRunId, decision, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const runtimeDecision = normalizeActionDecision(decision)
  const clientDecisionId = options.clientDecisionId || `pi-${runtimeRunId}-${runtimeDecision}`
  const path = `/api/ai/agent-chatbox/runs/${encodeURIComponent(runtimeRunId)}/pi-approval/decision/stream`
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
  }, 'Agent Chatbox Pi 工具审批失败', 'stream_open')
  await readSseResponse(response, onEvent, 'Agent Chatbox Pi 工具审批失败')
  return {
    abort: () => controller.abort()
  }
}

export async function streamAgentChatboxRunEvents(runtimeRunId, afterEventId, onEvent, options = {}) {
  const controller = new AbortController()
  const signal = options.signal || controller.signal
  const path = `/api/ai/agent-chatbox/runs/${encodeURIComponent(runtimeRunId)}/events/stream`
  const response = await runtimeFetch(path, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ after_event_id: afterEventId || '' }),
    signal
  }, 'Agent Chatbox 事件续传失败', 'stream_open')
  await readSseResponse(response, onEvent, 'Agent Chatbox 事件续传失败')
  return {
    abort: () => controller.abort()
  }
}

export async function decideAgentChatboxPiApproval(runtimeRunId, decision, options = {}) {
  const runtimeDecision = normalizeActionDecision(decision)
  const clientDecisionId = options.clientDecisionId || `pi-${runtimeRunId}-${runtimeDecision}`
  return request({
    url: `/ai/agent-chatbox/runs/${encodeURIComponent(runtimeRunId)}/pi-approval/decision`,
    method: 'post',
    data: { decision: runtimeDecision, client_decision_id: clientDecisionId }
  })
}

export async function listAgentChatboxRunEvents(runtimeRunId, afterEventId = '', context = {}) {
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
  const response = await runtimeFetch(`/api/ai/agent-chatbox/runs/${encodeURIComponent(runtimeRunId)}/events${query ? `?${query}` : ''}`, {
    method: 'GET',
    headers: authHeaders()
  }, 'Agent Chatbox 事件回放失败', 'event_replay')
  if (!response.ok) {
    const text = await response.text()
    throw normalizeAgentRuntimeError(text, 'Agent Chatbox 事件回放失败', {
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

export async function getAgentChatboxArtifact(artifactId, context = {}) {
  const response = await runtimeFetch(`/api/ai/agent-chatbox/artifacts/${encodeURIComponent(artifactId)}${artifactContextParams(context)}`, {
    method: 'GET',
    headers: authHeaders()
  }, 'Agent Chatbox 产物加载失败', 'artifact_load')
  if (!response.ok) {
    const text = await response.text()
    throw normalizeAgentRuntimeError(text, 'Agent Chatbox 产物加载失败', {
      category: response.status === 429 || response.status >= 500 ? 'transport' : 'http',
      retryable: response.status === 429 || response.status >= 500,
      http_status: response.status,
      phase: 'artifact_load'
    })
  }
  return response.json()
}

export { AGENT_CHATBOX_SEND_TIMEOUT }
