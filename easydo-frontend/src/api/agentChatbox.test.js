import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('agent chatbox api uses dedicated server facade routes', async () => {
  const [source, queueSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'runtimeSessionQueue.js'), 'utf8')
  ])

  assert.match(source, /url:\s*'\/ai\/agent-chatbox\/profiles'/)
  assert.match(source, /url:\s*'\/ai\/agent-chatbox\/sessions\/open'/)
  assert.match(source, /url:\s*'\/ai\/agent-chatbox\/sessions'/)
  assert.match(source, /url:\s*`\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}`/)
  assert.match(source, /url:\s*`\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}\/entries`/)
  assert.match(source, /url:\s*`\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}\/cancel`/)
  assert.match(source, /url:\s*`\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}\/archive`/)
  assert.match(source, /export function archiveAgentChatboxSession/)
  assert.match(source, /url:\s*`\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}\/model`/)
  assert.match(source, /method:\s*'put'/)
  assert.match(source, /\/agent-chatbox\/sessions\/\$\{sessionId\}\/entries\/stream/)
  assert.match(source, /\/agent-chatbox\/sessions\/\$\{sessionId\}\/continue\/stream/)
  assert.match(source, /\/agent-chatbox\/actions\/\$\{id\}\/decision\/stream/)
  assert.match(source, /\/agent-chatbox\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/pi-approval\/decision\/stream/)
  assert.match(source, /\/agent-chatbox\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/events\/stream/)
  assert.doesNotMatch(source, /tool-confirmations/)
  assert.doesNotMatch(source, /page-ai-assistant/)
  assert.doesNotMatch(source, /context_ref/)
  assert.match(source, /listRuntimeSessionQueueItems as listAgentChatboxQueueItems/)
  assert.match(source, /enqueueRuntimeSessionQueueItem as enqueueAgentChatboxQueueItem/)
  assert.match(source, /cancelRuntimeSessionQueueItem as cancelAgentChatboxQueueItem/)
  assert.match(source, /reorderRuntimeSessionQueueItems as reorderAgentChatboxQueueItems/)
  assert.match(queueSource, /\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}\/queue-items/)
  assert.match(queueSource, /queue-items\/reorder/)
})

test('agent chatbox api exposes an sse parser for visible reasoning and answer deltas', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /sendAgentChatboxMessageStream/)
  assert.match(source, /continueAgentChatboxSessionStream/)
  assert.match(source, /cancelAgentChatboxSession/)
  assert.match(source, /streamAgentChatboxActionDecision/)
  assert.match(source, /streamAgentChatboxPiApproval/)
  assert.match(source, /streamAgentChatboxRunEvents/)
  assert.match(source, /readAgentRuntimeSseResponse/)
  assert.match(sseSource, /response\.body\.getReader\(\)/)
  assert.match(sseSource, /TextDecoder/)
  assert.match(source, /AbortController/)
  assert.match(source, /waitForBrowserPaint/)
  assert.match(source, /STREAM_EVENTS_PER_FRAME/)
  assert.match(source, /STREAM_EVENTS_PER_FRAME\s*=\s*(?:[8-9]|[1-9]\d+)/)
  assert.match(sseSource, /stream_protocol_error/)
})

test('agent chatbox api uses shared stream paint wait that cannot stall hidden tabs', async () => {
  const [source, paintSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'streamPaint.js'), 'utf8')
  ])

  assert.match(source, /import\s+\{\s*waitForBrowserPaint\s*\}\s+from\s+'\.\/streamPaint'/)
  assert.doesNotMatch(source, /function waitForBrowserPaint/)
  assert.match(paintSource, /visibilityState/)
  assert.match(paintSource, /document\.hidden/)
  assert.match(paintSource, /requestAnimationFrame/)
  assert.match(paintSource, /setTimeout/)
  assert.match(paintSource, /PAINT_WAIT_FALLBACK_MS/)
})

test('agent chatbox streams Pi approval continuations through run-level SSE', async () => {
  const source = await readFile(join(currentDir, 'agentChatbox.js'), 'utf8')

  assert.match(source, /export async function streamAgentChatboxPiApproval\(runtimeRunId,\s*decision,\s*onEvent,\s*options\s*=\s*\{\}\)/)
  assert.match(source, /const path = `\/api\/ai\/agent-chatbox\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/pi-approval\/decision\/stream`/)
  assert.match(source, /const approval = options\.approval \|\| \{\}/)
  assert.match(source, /approval_id:\s*approval\.approval_id/)
  assert.match(source, /request_id:\s*approval\.request_id/)
  assert.match(source, /call_id:\s*approval\.call_id/)
  assert.match(source, /tool_name:\s*approval\.tool_name/)
  assert.match(source, /await readSseResponse\(response,\s*onEvent,\s*'Agent Chatbox Pi 工具审批失败'\)/)
})

test('agent chatbox sse parser yields browser paint for all visible process events', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /VISIBLE_PROCESS_EVENT_PREFIXES/)
  assert.match(source, /skill\./)
  assert.match(source, /subagent\./)
  assert.match(source, /'subagent\.spawned'/)
  assert.match(source, /'subagent\.completed'/)
  assert.match(source, /model\.tool_call_detected/)
  assert.match(source, /tool\.result_prepared/)
  assert.match(source, /action\.execution_started/)
  assert.match(source, /action\.execution_succeeded/)
  assert.match(source, /action\.decision_required/)
  assert.match(sseSource, /options\.isVisibleEvent\(options\.visibleEventName\(event,\s*payload\)\)/)
})

test('agent chatbox sse parser yields browser paint for wrapped runtime process events', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /runtime_event/)
  assert.match(source, /visibleStreamEventName/)
  assert.match(source, /payload\?\.event\?\.type/)
  assert.match(sseSource, /options\.isVisibleEvent\(options\.visibleEventName\(event,\s*payload\)\)/)
})

test('agent chatbox action decisions preserve approve session intent', async () => {
  const [apiSource, storeSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../stores/agentChatbox.js'), 'utf8')
  ])

  assert.match(apiSource, /SUPPORTED_ACTION_DECISIONS\s*=\s*new Set\(\[\s*'approve_once',\s*'approve_session',\s*'reject',\s*'steer'\s*\]\)/)
  assert.match(apiSource, /runtimeDecision\s*=\s*normalizeActionDecision\(decision\)/)
  assert.doesNotMatch(apiSource, /decision\s*===\s*'reject'\s*\?\s*'reject'\s*:\s*'approve_once'/)
  assert.match(storeSource, /approveActionForSession/)
  assert.match(storeSource, /continueAction\(id,\s*'approve_session'\)/)
  assert.doesNotMatch(storeSource, /decision\s*===\s*'reject'\s*\?\s*'reject'\s*:\s*'approve_once'/)
})

test('agent chatbox store imports runtime event merge helper for final assistant entries', async () => {
  const storeSource = await readFile(join(currentDir, '../stores/agentChatbox.js'), 'utf8')

  assert.match(storeSource, /mergeRuntimeEvents/)
  assert.match(storeSource, /mergeAssistantRuntimeEvents/)
})

test('agent chatbox restores controls from durable active run state', async () => {
  const source = await readFile(join(currentDir, '../stores/agentChatbox.js'), 'utf8')
  assert.match(source, /const activeRuntimeRun = computed\(/)
  assert.match(source, /const hasActiveRun = computed\(\(\) => Boolean\(activeRuntimeRun\.value\?\.runtime_run_id\)\)/)
  assert.match(source, /activeRuntimeRunFromState\(session\.value,\s*entries\.value\)/)
  assert.match(source, /hasActiveRun,/)
})

test('agent chatbox follows a still-active Run after the initial POST stream closes', async () => {
  const source = await readFile(join(currentDir, '../stores/agentChatbox.js'), 'utf8')

  assert.match(source, /const summaryAfterInitialStream = await refreshSessionSummary\(runningSessionId\)/)
  assert.match(source, /if \(summaryAfterInitialStream\?\.active_run\?\.runtime_run_id\) \{\s*return followActiveRun\(runningSessionId, finalRef\)\s*\}/s)
})

test('agent chatbox store treats Pi reasoning ended as final text instead of another delta', async () => {
  const storeSource = await readFile(join(currentDir, '../stores/agentChatbox.js'), 'utf8')

  assert.match(storeSource, /runtimeEventName === 'session\.reasoning\.delta'/)
  assert.match(storeSource, /runtimeEventName === 'session\.reasoning\.ended'/)
  assert.doesNotMatch(storeSource, /runtimeEventName === 'session\.reasoning\.delta'\s*\|\|\s*runtimeEventName === 'session\.reasoning\.ended'/)
})

test('agent chatbox artifact requests include parent runtime context', async () => {
  const source = await readFile(join(currentDir, 'agentChatbox.js'), 'utf8')

  assert.match(source, /getAgentChatboxArtifact\(artifactId,\s*context\s*=\s*\{\}\)/)
  assert.match(source, /artifactContextParams/)
  assert.match(source, /parent_runtime_run_id/)
  assert.match(source, /parent_action_id/)
  assert.match(source, /child_run_link_id/)
})

test('agent chatbox run event replay accepts parent runtime context', async () => {
  const source = await readFile(join(currentDir, 'agentChatbox.js'), 'utf8')

  assert.match(source, /listAgentChatboxRunEvents\(runtimeRunId,\s*afterEventId\s*=\s*'',\s*context\s*=\s*\{\}\)/)
  assert.match(source, /typeof afterEventId === 'object'/)
  assert.match(source, /parent_runtime_run_id/)
  assert.match(source, /parent_action_id/)
  assert.match(source, /child_run_link_id/)
})

test('agent chatbox api preserves structured runtime errors for HTTP, SSE, and replay', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /from '\.\.\/utils\/agentRuntimeError'/)
  assert.match(source, /normalizeAgentRuntimeError/)
  assert.match(sseSource, /agentRuntimeErrorPayload/)
  assert.match(sseSource, /http_status:\s*response\.status/)
  assert.match(sseSource, /category:\s*'transport'/)
  assert.match(sseSource, /retryable:\s*true/)
  assert.match(sseSource, /phase:\s*'stream_decode'/)
  assert.match(sseSource, /event === 'error' \|\| event === 'stream_protocol_error'/)
  assert.match(sseSource, /if \(streamError\) throw streamError/)
  assert.doesNotMatch(source, /throw new Error\(text \|\| 'Agent Chatbox 事件回放失败'\)/)
  assert.doesNotMatch(source, /raw:\s*data/)
})
