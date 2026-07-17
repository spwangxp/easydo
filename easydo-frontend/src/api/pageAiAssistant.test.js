import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('page ai assistant api uses generic runtime session routes', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  assert.match(source, /PAGE_ASSISTANT_PROFILE_NAME\s*=\s*'page-ai-assistant'/)
  assert.match(source, /listPageAssistantProfiles/)
  assert.match(source, /url:\s*'\/store\/ai-agents\/profiles'/)
  assert.match(source, /profile_name:\s*PAGE_ASSISTANT_PROFILE_NAME/)
  assert.match(source, /url:\s*'\/ai\/sessions\/current'/)
  assert.match(source, /url:\s*`\/ai\/sessions\/\$\{sessionId\}`/)
  assert.match(source, /url:\s*'\/ai\/sessions'/)
  assert.match(source, /url:\s*`\/ai\/sessions\/\$\{sessionId\}\/entries`/)
  assert.match(source, /url:\s*`\/ai\/sessions\/\$\{sessionId\}\/cancel`/)
  assert.match(source, /updatePageAssistantSessionModel/)
  assert.match(source, /url:\s*`\/ai\/sessions\/\$\{sessionId\}\/model`/)
  assert.match(source, /\/api\/ai\/actions\/\$\{id\}\/decision\/stream/)
  assert.match(source, /\/api\/ai\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/pi-approval\/decision\/stream/)
  assert.match(source, /\/api\/ai\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/events\/stream/)
  assert.doesNotMatch(source, /tool-confirmations/)
  assert.doesNotMatch(source, /page_ai_assistant_sessions/)
  assert.doesNotMatch(source, /runtime_profile/i)
  assert.doesNotMatch(source, /supported_scene_types/)
})

test('page assistant store uses the shared runtime session controller', async () => {
  const [source, controllerSource] = await Promise.all([
    readFile(join(currentDir, '../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../stores/runtimeSessionController.js'), 'utf8')
  ])

  assert.match(source, /createRuntimeSessionController/)
  assert.match(controllerSource, /createRuntimeSessionQueueController/)
  assert.match(source, /queueController\.enqueue\(content, mode\)/)
  assert.match(source, /queueController\.applyRuntimeEvent\(runtimeEvent\)/)
  assert.match(source, /queueItems,/)
  assert.match(source, /queueLoading,/)
  assert.match(source, /loadQueueItems,/)
  assert.match(source, /cancelQueueItem,/)
  assert.match(source, /reorderQueueItems,/)
  assert.doesNotMatch(source, /await import\('@\/api\/agentChatbox'\)/)
})

test('page ai assistant stop uses the shared durable active Run projection', async () => {
  const [apiSource, storeSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(apiSource, /cancelPageAssistantSession/)
  assert.match(apiSource, /\/cancel/)
  assert.match(storeSource, /cancelPageAssistantSession/)
  assert.match(storeSource, /activeRuntimeRunFromState/)
  assert.match(storeSource, /const activeRuntimeRun = computed/)
  assert.match(storeSource, /async function stopGeneration/)
  assert.match(storeSource, /runtime_run_id:\s*runtimeRunId/)
  assert.doesNotMatch(storeSource, /cancelPageAssistantSession\([^)]*\)\.catch/)
  assert.doesNotMatch(storeSource, /const activeRuntimeRunId = ref/)
  assert.match(storeSource, /reason:\s*'user_stopped_generation'/)
})

test('page ai assistant resumes a durable active Run after initial stream EOF', async () => {
  const storeSource = await readFile(join(currentDir, '../stores/pageAiAssistant.js'), 'utf8')

  assert.match(storeSource, /streamPageAssistantRunEvents/)
  assert.match(storeSource, /async function followActiveRun/)
  assert.match(storeSource, /const summaryAfterInitialStream = await refreshSessionSummary/)
  assert.match(storeSource, /summaryAfterInitialStream\?\.active_run\?\.runtime_run_id/)
})

test('page ai assistant send request allows slow reasoning models to finish', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  assert.match(source, /PAGE_ASSISTANT_SEND_TIMEOUT/)
  assert.match(source, /timeout:\s*PAGE_ASSISTANT_SEND_TIMEOUT/)
})

test('page ai assistant exposes a streaming entry sender for reasoning and answer deltas', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /sendPageAssistantMessageStream/)
  assert.match(source, /\/entries\/stream/)
  assert.match(source, /readAgentRuntimeSseResponse/)
  assert.match(sseSource, /response\.body\.getReader\(\)/)
  assert.match(source, /AbortController/)
})

test('page ai assistant stream parser yields browser paint time during visible deltas', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /waitForBrowserPaint/)
  assert.match(source, /STREAM_EVENTS_PER_FRAME/)
  assert.match(source, /isVisibleStreamEvent/)
  assert.match(sseSource, /await options\.waitForPaint\(\)/)
})

test('page ai assistant api uses shared stream paint wait that cannot stall hidden tabs', async () => {
  const [source, paintSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
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

test('page ai assistant stream parser yields browser paint for Pi OpenCode process events', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  for (const eventName of [
    'session.reasoning.delta',
    'session.text.delta',
    'session.tool.input.started',
    'session.tool.input.delta',
    'session.tool.called',
    'permission.asked',
    'permission.resolved',
    'session.compaction.started',
    'session.compaction.ended',
    'session.error'
  ]) {
    assert.match(source, new RegExp(eventName.replaceAll('.', '\\.')), `${eventName} must yield browser paint when streamed`)
  }
})

test('page ai assistant stream parser yields browser paint for wrapped runtime process events', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /runtime_event/)
  assert.match(source, /visibleStreamEventName/)
  assert.match(source, /payload\?\.event\?\.type/)
  assert.match(sseSource, /options\.isVisibleEvent\(options\.visibleEventName\(event,\s*payload\)\)/)
})

test('page ai assistant streams action decision continuations', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  assert.match(source, /streamPageAssistantActionDecision/)
  assert.match(source, /streamPageAssistantPiApproval/)
  assert.match(source, /decidePageAssistantPiApproval/)
  assert.match(source, /\/ai\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/pi-approval\/decision/)
  assert.match(source, /SUPPORTED_ACTION_DECISIONS/)
  assert.match(source, /approve_session/)
  assert.match(source, /runtimeDecision\s*=\s*normalizeActionDecision\(decision\)/)
  assert.match(source, /readSseResponse/)
  assert.doesNotMatch(source, /confirmAiToolConfirmation/)
  assert.doesNotMatch(source, /rejectAiToolConfirmation/)
})

test('page ai assistant streams Pi approval continuations through run-level SSE', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  assert.match(source, /export async function streamPageAssistantPiApproval\(runtimeRunId,\s*decision,\s*onEvent,\s*options\s*=\s*\{\}\)/)
  assert.match(source, /const path = `\/api\/ai\/runs\/\$\{encodeURIComponent\(runtimeRunId\)\}\/pi-approval\/decision\/stream`/)
  assert.match(source, /const approval = options\.approval \|\| \{\}/)
  assert.match(source, /approval_id:\s*approval\.approval_id/)
  assert.match(source, /request_id:\s*approval\.request_id/)
  assert.match(source, /call_id:\s*approval\.call_id/)
  assert.match(source, /tool_name:\s*approval\.tool_name/)
  assert.match(source, /await readSseResponse\(response,\s*onEvent,\s*'页面助手 Pi 工具审批失败'\)/)
})

test('page ai assistant action decisions preserve approve session intent', async () => {
  const [apiSource, storeSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(apiSource, /SUPPORTED_ACTION_DECISIONS\s*=\s*new Set\(\[\s*'approve_once',\s*'approve_session',\s*'reject',\s*'steer'\s*\]\)/)
  assert.doesNotMatch(apiSource, /decision\s*===\s*'reject'\s*\?\s*'reject'\s*:\s*'approve_once'/)
  assert.match(storeSource, /approveActionForSession/)
  assert.match(storeSource, /continueAction\(id,\s*'approve_session'\)/)
  assert.match(storeSource, /agentAction\.pi_approval/)
  assert.match(storeSource, /continuePiApproval/)
  assert.doesNotMatch(storeSource, /decision\s*===\s*'reject'\s*\?\s*'reject'\s*:\s*'approve_once'/)
})

test('page ai assistant artifact requests include parent runtime context', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  assert.match(source, /getPageAssistantArtifact\(artifactId,\s*context\s*=\s*\{\}\)/)
  assert.match(source, /artifactContextParams/)
  assert.match(source, /parent_runtime_run_id/)
  assert.match(source, /parent_action_id/)
  assert.match(source, /child_run_link_id/)
})

test('page ai assistant run event replay accepts parent runtime context', async () => {
  const source = await readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8')

  assert.match(source, /listPageAssistantRunEvents\(runtimeRunId,\s*afterEventId\s*=\s*'',\s*context\s*=\s*\{\}\)/)
  assert.match(source, /typeof afterEventId === 'object'/)
  assert.match(source, /parent_runtime_run_id/)
  assert.match(source, /parent_action_id/)
  assert.match(source, /child_run_link_id/)
})

test('page assistant api preserves structured runtime errors and normalizes malformed SSE', async () => {
  const [source, sseSource] = await Promise.all([
    readFile(join(currentDir, 'pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, 'agentRuntimeSse.js'), 'utf8')
  ])

  assert.match(source, /from '\.\.\/utils\/agentRuntimeError'/)
  assert.match(source, /normalizeAgentRuntimeError/)
  assert.match(sseSource, /agentRuntimeErrorPayload/)
  assert.match(sseSource, /stream_protocol_error/)
  assert.match(sseSource, /http_status:\s*response\.status/)
  assert.match(sseSource, /category:\s*'transport'/)
  assert.match(sseSource, /retryable:\s*true/)
  assert.match(sseSource, /phase:\s*'stream_decode'/)
  assert.match(sseSource, /event === 'error' \|\| event === 'stream_protocol_error'/)
  assert.match(sseSource, /if \(streamError\) throw streamError/)
  assert.doesNotMatch(source, /throw new Error\(text \|\| '页面助手事件回放失败'\)/)
  assert.doesNotMatch(source, /raw:\s*data/)
})
