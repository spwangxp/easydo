import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  buildHtmlPreviewDocument,
  contentBlocksFromEntry,
  renderMarkdownToHtml
} from '../../components/ai-runtime/messageContent.js'
import {
  buildRuntimeProcessItems,
  visibleRuntimeProcessEvents
} from '../../components/ai-runtime/runtimeProcessEvents.js'
import {
  mergeRuntimeEvents,
  normalizeRuntimeEvent
} from '../../stores/aiRuntimeEvents.js'
import {
  activeRuntimeRunFromState,
  agentRunMetricsForEntry,
  approvalDecisionForAction,
  derivePendingActions,
  displayRuntimeEventsForEntry,
  isActionAwaitingDecision,
  isApprovalDecisionButtonSelected,
  latestRuntimeModelFromEvents,
  mergeRuntimeModelSnapshots,
  mergeReplayEventsIntoEntries,
  resolveProviderDisplayName,
  runtimeModelSnapshotFromEvent,
  runtimeEventsFromReplayResponse
} from '../../stores/agentChatboxState.js'
import { buildSessionModelOverride } from '../../stores/agentConversationShared.js'
import {
  assistantFailureDetails,
  assistantPartialContent,
  hasAssistantPartialContent,
  isAssistantFailureEntry,
  precedingUserPrompt
} from '../../components/ai-runtime/assistantFailure.js'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('shared runtime state projection prefers Session active_run and falls back to live Assistant Entries', () => {
  const sessionRun = { runtime_run_id: 'run-session', status: 'running' }
  assert.deepEqual(activeRuntimeRunFromState({ active_run: sessionRun }, []), sessionRun)

  assert.deepEqual(activeRuntimeRunFromState({}, [{
    id: 2,
    role: 'assistant',
    status: 'streaming',
    runtime_run_id: 'run-entry',
    output: {}
  }]), {
    runtime_run_id: 'run-entry',
    status: 'streaming',
    output_entry_id: 2,
    started_at: undefined,
    updated_at: undefined
  })
})

test('agent chatbox route is registered under ai agent store URL space', async () => {
  const routerSource = await readFile(join(currentDir, '../../router/index.js'), 'utf8')

  assert.match(routerSource, /path:\s*'store\/ai-agents\/chat\/:session_id'/)
  assert.match(routerSource, /name:\s*'AIAgentChatbox'/)
  assert.match(routerSource, /path:\s*'store\/ai-agents\/chat\/:session_id\/subagents\/:child_run_id'/)
  assert.match(routerSource, /name:\s*'AIAgentChatboxChildThread'/)
  assert.match(routerSource, /views\/ai-agent-chatbox\/AgentChatbox\.vue/)
})

test('agent chatbox page uses vm terminal style information architecture', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /class="agent-chatbox-shell"/)
  assert.match(source, /class="agent-chatbox-sidebar"/)
  assert.match(source, /class="agent-chatbox-workspace"/)
  assert.match(source, /chatboxStore\.sessions/)
  assert.match(source, /loadSessionFromRoute/)
  assert.match(source, /createNewSession/)
  assert.match(source, /copySessionUrl/)
  assert.match(source, /class="chatbox-model-row"/)
  assert.match(source, /chatboxStore\.currentSessionModel\.provider/)
  assert.match(source, /chatboxStore\.currentSessionModel\.model/)
  assert.match(source, /chatboxStore\.currentSessionModel\.thinking_level/)
  assert.match(source, /chatboxStore\.switchSessionModel/)
  assert.match(source, /class="chatbox-agent-metrics"/)
  assert.match(source, /agentRunMetricsForEntry/)
  assert.match(source, /width:\s*min\(320px,\s*100%\)/)
  assert.match(source, /class="chatbox-approval-button"/)
  assert.match(source, /plain/)
  assert.match(source, /text/)
  assert.doesNotMatch(source, /\.chatbox-model-trigger\s*\{[^}]*width:\s*100%;/s)
  assert.doesNotMatch(source, /usePageAiContext/)
  assert.doesNotMatch(source, /page_ai_assistant/)
})


test('agent chatbox exposes session archive lifecycle controls', async () => {
  const [source, storeSource, apiSource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../api/agentChatbox.js'), 'utf8')
  ])

  assert.match(apiSource, /export function archiveAgentChatboxSession\(sessionId\)/)
  assert.match(apiSource, /url:\s*`\/ai\/agent-chatbox\/sessions\/\$\{sessionId\}\/archive`/)
  assert.match(storeSource, /async function archiveSession\(/)
  assert.match(storeSource, /archiveAgentChatboxSession\(sessionId\)/)
  assert.match(storeSource, /sessions\.value = sessions\.value\.filter/)
  assert.match(source, /archiveCurrentSession/)
  assert.match(source, /chatboxStore\.archiveSession/)
  assert.match(source, />归档</)
})

test('agent chatbox keeps manual scroll position during streaming and approval updates', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /useStickyScroll/)
  assert.match(source, /captureStickyScrollState/)
  assert.match(source, /restoreStickyScrollPosition/)
  assert.match(source, /requestScrollToBottom\(\)/)
  assert.doesNotMatch(source, /scrollBox\.value\.scrollTop\s*=\s*scrollBox\.value\.scrollHeight/)
})

test('agent chatbox model override keeps provider display name separate from provider type', async () => {
  const override = buildSessionModelOverride({
    id: 7,
    name: 'display-provider',
    provider_type: 'openrouter'
  }, {
    provider_model_key: 'model-key'
  }, 'medium')

  assert.equal(override.provider.provider_id, '7')
  assert.equal(override.provider.name, 'display-provider')
  assert.equal(override.provider.provider_type, 'openrouter')
})

test('agent chatbox model switch labels prefer provider display name over provider type', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')
  const currentModelStart = storeSource.indexOf('const currentSessionModel = computed')
  const currentModelEnd = storeSource.indexOf('const canSwitchSessionModel', currentModelStart)
  const switchOptionsStart = storeSource.indexOf('const modelSwitchOptions = computed')
  const switchOptionsEnd = storeSource.indexOf('function nextClientEntryId', switchOptionsStart)
  const currentModelSource = storeSource.slice(currentModelStart, currentModelEnd)
  const switchOptionsSource = storeSource.slice(switchOptionsStart, switchOptionsEnd)

  assert.match(currentModelSource, /const runtimeModel = latestRuntimeModel\.value/)
  assert.match(currentModelSource, /provider:\s*resolveProviderDisplayName\(provider,\s*providerDisplayCandidates\.value\)/)
  assert.match(switchOptionsSource, /provider_label:\s*firstString\(provider\.display_name,\s*provider\.displayName,\s*provider\.name,/)
})

test('agent chatbox resolves numeric provider ids to provider names for current model display', () => {
  const providers = [
    { id: 1, name: 'opproxy', provider_type: 'openrouter', base_url: 'http://10.159.69.8:8081/v1' },
    { id: 2, name: 'nova', provider_type: 'anthropic', base_url: 'https://token.sensenova.cn' }
  ]

  assert.equal(resolveProviderDisplayName({ provider_id: '1' }, providers), 'opproxy')
  assert.equal(resolveProviderDisplayName({ provider_id: 'openrouter', base_url: 'http://10.159.69.8:8081/v1' }, providers), 'opproxy')
  assert.equal(resolveProviderDisplayName({ provider_id: 'openrouter' }, providers), 'opproxy')
})

test('agent chatbox derives current model display from replayed runtime model events', () => {
  const replayModel = latestRuntimeModelFromEvents([
    {
      type: 'model.selected',
      payload: {
        model: {
          provider_id: 'openrouter',
          id: 'tencent/hy3:free',
          base_url: 'http://10.159.69.8:8081/v1',
          thinking_level: 'medium'
        }
      }
    },
    {
      type: 'session.step.started',
      payload: {
        model: {
          provider_id: 'openrouter',
          id: 'tencent/hy3:free'
        }
      }
    }
  ])
  const providerSnapshot = { provider_id: '1', name: 'opproxy', provider_type: 'openrouter', base_url: 'http://10.159.69.8:8081/v1' }

  assert.equal(replayModel.provider_id, 'openrouter')
  assert.equal(replayModel.provider_model_key, 'tencent/hy3:free')
  assert.equal(replayModel.base_url, 'http://10.159.69.8:8081/v1')
  assert.equal(replayModel.thinking_level, 'medium')
  assert.equal(resolveProviderDisplayName(replayModel, [providerSnapshot]), 'opproxy')
})

test('agent chatbox merges runtime model switches without losing provider metadata', () => {
  const selected = runtimeModelSnapshotFromEvent({
    type: 'model.selected',
    payload: {
      model: {
        provider_id: 'openrouter',
        id: 'tencent/hy3:free',
        base_url: 'http://10.159.69.8:8081/v1',
        inference: { thinking_level: 'medium' }
      }
    }
  })
  const switched = runtimeModelSnapshotFromEvent({
    type: 'session.model.switched',
    payload: {
      model: {
        provider_id: 'openrouter',
        id: 'poolside/laguna-xs-2.1:free'
      }
    }
  })

  assert.deepEqual(mergeRuntimeModelSnapshots(selected, switched), {
    provider_id: 'openrouter',
    id: 'poolside/laguna-xs-2.1:free',
    model: 'poolside/laguna-xs-2.1:free',
    provider_model_key: 'poolside/laguna-xs-2.1:free',
    base_url: 'http://10.159.69.8:8081/v1',
    inference: { thinking_level: 'medium' },
    provider_type: 'openrouter',
    thinking_level: 'medium'
  })
})

test('agent chatbox model switch promotes catalog context window into runtime model override', async () => {
  const override = buildSessionModelOverride({}, {
    provider_model_key: 'model-key',
    metadata_json: JSON.stringify({ context_window: 131072 })
  }, 'high')

  assert.equal(override.model.context_window, 131072)
  assert.equal(override.inference.thinking_level, 'high')
})

test('agent chatbox lets runtime generate new session titles', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')
  const createStart = storeSource.indexOf('async function createNewSession')
  const createEnd = storeSource.indexOf('async function loadEntries', createStart)
  const createNewSessionSource = storeSource.slice(createStart, createEnd)

  assert.ok(createStart > 0)
  assert.ok(createEnd > createStart)
  assert.match(createNewSessionSource, /createAgentChatboxSession\(\{/)
  assert.match(createNewSessionSource, /agent_profile_id:\s*profileId/)
  assert.doesNotMatch(createNewSessionSource, /\btitle\s*:/)
})

test('agent chatbox refreshes session summary after runs so generated titles become visible', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')

  assert.match(storeSource, /async function refreshSessionSummary/)
  assert.match(storeSource, /async function refreshSessionSummaryAfterRun/)
  assert.match(storeSource, /title_source[^]*!==\s*'fallback'/)
  assert.match(storeSource, /void refreshSessionSummaryAfterRun\(runningSessionId\)/)
})

test('agent chatbox topbar shows generated session title before profile name', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /<h1>{{ currentSessionTitle }}<\/h1>/)
  assert.match(source, /const currentSessionTitle = computed\(\(\) => \{/)
  assert.match(source, /chatboxStore\.session\?\.title/)
  assert.match(source, /chatboxStore\.currentProfile\?\.name/)
})

test('agent chatbox keeps polling long enough for delayed generated session titles', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')
  const refreshStart = storeSource.indexOf('async function refreshSessionSummaryAfterRun')
  const refreshEnd = storeSource.indexOf('async function createNewSession', refreshStart)
  const refreshSource = storeSource.slice(refreshStart, refreshEnd)
  const followStart = storeSource.indexOf('async function followActiveRun')
  const followEnd = storeSource.indexOf('async function enqueueQueueMessage', followStart)
  const followSource = storeSource.slice(followStart, followEnd)
  const sendStart = storeSource.indexOf('async function sendMessage')
  const sendEnd = storeSource.indexOf('async function continueAction', sendStart)
  const sendSource = storeSource.slice(sendStart, sendEnd)

  assert.ok(refreshStart > 0)
  assert.ok(refreshEnd > refreshStart)
  assert.match(refreshSource, /\[0,\s*400,\s*900,\s*1600,\s*2500,\s*4000,\s*7000,\s*12000\]/)
  assert.match(followSource, /void refreshSessionSummaryAfterRun\(sessionId\)/)
  assert.match(sendSource, /void refreshSessionSummaryAfterRun\(runningSessionId\)/)
})

test('runtime model snapshots preserve context window from generic runtime events', () => {
  const snapshot = latestRuntimeModelFromEvents([
    {
      type: 'model.call_started',
      payload: {
        provider: 'openrouter',
        provider_type: 'openrouter',
        model: 'qwen/qwen3-coder',
        base_url: 'https://openrouter.ai/api/v1',
        context_window: 262144,
        max_tokens: 8192,
        thinking_level: 'high'
      }
    }
  ])

  assert.equal(snapshot.provider_id, 'openrouter')
  assert.equal(snapshot.provider_type, 'openrouter')
  assert.equal(snapshot.provider_model_key, 'qwen/qwen3-coder')
  assert.equal(snapshot.context_window, 262144)
  assert.equal(snapshot.max_tokens, 8192)
  assert.equal(snapshot.thinking_level, 'high')
})

test('agent chatbox page renders streaming reasoning trace action decisions and structured output', async () => {
  const [source, traceSource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  ])

  assert.match(source, /chatboxReasoningText/)
  assert.match(traceSource, /process-timeline/)
  assert.match(traceSource, /event-item/)
  assert.match(traceSource, /buildRuntimeProcessItems/)
  assert.match(source, /:reasoning="chatboxReasoningText\(entry\)"/)
  assert.match(source, /:answer="runtimeTraceAnswer\(entry\)"/)
  assert.match(source, /function runtimeTraceAnswer\(/)
  assert.match(source, /:timings="entry\.output\?\.timings \|\| \{\}"/)
  assert.doesNotMatch(source, /class="chatbox-reasoning chatbox-reasoning--compact"/)
  assert.match(source, /<RuntimeTrace/)
  assert.match(traceSource, /openArtifact/)
  assert.match(traceSource, /openChildRun/)
  assert.match(traceSource, /loadRunEvents/)
  assert.match(traceSource, /loadRunEvents\(item\.childRuntimeRunId,\s*'',\s*childRunContext\(item\)\)/)
  assert.match(traceSource, /openChildRunByRuntimeId/)
  assert.match(traceSource, /defineExpose/)
  assert.match(source, /runtimeTraceRefs/)
  assert.match(source, /route\.params\.child_run_id/)
  assert.match(source, /openRouteChildThread/)
  assert.match(traceSource, /运行产物/)
  assert.match(traceSource, /子运行/)
  assert.match(source, /chatboxRuntimeEvents/)
  assert.match(source, /v-if="chatboxRuntimeEvents\(entry,\s*chatboxStore\.entries\)\.length \|\| chatboxReasoningText\(entry\)"/)
  assert.match(source, /:events="chatboxRuntimeEvents\(entry,\s*chatboxStore\.entries\)"/)
  assert.match(source, /chatboxStructuredOutput/)
  assert.match(source, /结构化结果/)
  assert.match(source, /chatboxStore\.pendingActions/)
  assert.match(source, /chatboxStore\.approveAction/)
  assert.match(source, /chatboxStore\.approveActionForSession/)
  assert.match(source, /本会话批准/)
  assert.match(source, /chatboxStore\.rejectAction/)
  assert.match(source, /chatboxStore\.isActionDecisionPending/)
  assert.match(source, /approvalButtonDisabled/)
  assert.match(source, /approvalButtonSelected/)
  assert.match(source, /inputDisabled/)
  assert.match(source, /:disabled="inputDisabled"/)
  assert.match(source, /const inputDisabled = computed\(\(\) => chatboxStore\.sending \|\| chatboxStore\.loading \|\| !chatboxStore\.session\?\.id\)/)
  assert.doesNotMatch(source, /const inputDisabled = computed\(\(\) =>[^)]*hasActiveRun/)
  assert.match(source, /v-if="chatboxStore\.sending \|\| chatboxStore\.hasActiveRun"[^>]*@click="chatboxStore\.stopGeneration"/s)
  assert.doesNotMatch(source, /const inputDisabled = computed\(\(\) =>[^)]*pendingActions/)
  assert.match(source, /chatboxStore\.stopGeneration/)
  assert.match(source, /chatboxStore\.disconnectStream/)
  assert.doesNotMatch(source, /onBeforeUnmount\(\(\) => \{\s*chatboxStore\.stopGeneration\(\)/s)
})

test('agent chatbox exposes a visible replay refresh control for runtime traces', async () => {
  const [source, storeSource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')
  ])

  assert.match(source, /刷新轨迹/)
  assert.match(source, /refreshRuntimeTrace\(entry\)/)
  assert.match(source, /chatboxStore\.refreshRuntimeEvents\(runtimeRunId\)/)
  assert.match(source, /chatboxStore\.isRuntimeEventsRefreshing\(entryRuntimeRunId\(entry\)\)/)
  assert.match(storeSource, /function refreshRuntimeEvents\(runtimeRunId\)/)
  assert.match(storeSource, /listAgentChatboxRunEvents\(normalizedRuntimeRunId\)/)
  assert.match(storeSource, /mergeReplayEventsIntoEntries\(entries\.value,\s*normalizedRuntimeRunId,\s*replayEvents\)/)
  assert.match(storeSource, /pendingActions\.value = derivePendingActions\(entries\.value\)/)
})

test('agent chatbox renders failure recovery actions inside terminal timeline rows', async () => {
  const [source, traceSource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  ])

  assert.doesNotMatch(source, /AssistantFailureCard/)
  assert.doesNotMatch(source, /chatbox-partial-answer/)
  assert.match(source, /<template #terminal-actions="\{ item \}">/)
  assert.match(source, /isFailureTerminalItem\(item, entry\)/)
  assert.match(source, /continueAfterFailure\(entry\)/)
  assert.match(source, /retryFailedEntry\(entry\)/)
  assert.match(source, /precedingUserPrompt/)
  assert.match(source, /function showFinalAnswer\(entry\)[\s\S]*?chatboxRuntimeEvents\(entry, chatboxStore\.entries\)\.length \|\| chatboxReasoningText\(entry\)/s)
  assert.doesNotMatch(source, /function chatboxReasoningText\(entry\)[\s\S]*?if \(isAssistantFailureEntry\(entry\)\) return ''[\s\S]*?function chatboxAnswerText/)
  assert.doesNotMatch(source, /function chatboxAnswerText\(entry\)[\s\S]*?if \(isAssistantFailureEntry\(entry\)\) return ''[\s\S]*?function isCompletedAssistantEntry/)
  assert.match(traceSource, /slot name="terminal-actions"/)
  assert.match(traceSource, /isRuntimeTerminalItem\(item\)/)
  assert.doesNotMatch(traceSource, /AssistantFailureCard/)
})

test('shared assistant failure details prefer structured runtime errors and preserve diagnostic labels', () => {
  const entry = {
    role: 'assistant',
    status: 'failed',
    content: 'fallback failure text',
    output: {
      runtime_error: {
        user_message: '模型服务暂时不可用',
        message: 'provider returned 503',
        code: 'provider_unavailable',
        category: 'provider',
        retryable: true,
        runtime_run_id: 'run-failed-1'
      }
    }
  }

  assert.equal(isAssistantFailureEntry(entry), true)
  const details = assistantFailureDetails(entry)
  assert.equal(details.status, 'failed')
  assert.equal(details.statusLabel, '生成失败')
  assert.equal(details.reason, '模型服务暂时不可用')
  assert.equal(details.user_message, '模型服务暂时不可用')
  assert.equal(details.code, 'provider_unavailable')
  assert.equal(details.category, 'provider')
  assert.equal(details.retryable, true)
  assert.equal(details.runtimeRunId, 'run-failed-1')
  assert.equal(details.runtime_run_id, 'run-failed-1')
})

test('shared assistant failure details fall back from error payload to entry content for cancelled runs', () => {
  const details = assistantFailureDetails({
    role: 'assistant',
    status: 'cancelled',
    content: '用户停止了本次生成',
    output: {
      error: {
        code: 'user_cancelled',
        category: 'cancelled'
      }
    }
  })
  assert.equal(details.status, 'cancelled')
  assert.equal(details.statusLabel, '已停止')
  assert.equal(details.reason, '用户停止了本次生成')
  assert.equal(details.code, 'user_cancelled')
  assert.equal(details.category, 'cancelled')
  assert.equal(details.retryable, false)
  assert.equal(details.runtimeRunId, '')
})

test('assistant failure preserves partial answer separately from error reason', () => {
  const entry = {
    role: 'assistant',
    status: 'failed',
    content: 'partial answer body',
    output: {
      text: 'partial answer body',
      reasoning: 'thinking so far',
      runtime_error: {
        user_message: '模型服务暂时不可用',
        code: 'provider_unavailable',
        category: 'provider',
        retryable: true,
        request_id: 'req-1',
        runtime_run_id: 'run-partial-1'
      }
    },
    updated_at: '2026-07-16T00:00:05.000Z'
  }
  const details = assistantFailureDetails(entry)
  assert.equal(details.reason, '模型服务暂时不可用')
  assert.equal(details.request_id, 'req-1')
  assert.equal(details.occurred_at, '2026-07-16T00:00:05.000Z')
  assert.equal(hasAssistantPartialContent(entry), true)
  assert.deepEqual(assistantPartialContent(entry), {
    answer: 'partial answer body',
    reasoning: 'thinking so far'
  })
})

test('precedingUserPrompt recovers original user content and attachments for retry', () => {
  const entries = [
    {
      id: 'u1',
      role: 'user',
      content: 'original prompt',
      attachments: [{ name: 'note.md' }]
    },
    {
      id: 'a1',
      role: 'assistant',
      status: 'failed',
      parent_entry_id: 'u1',
      content: 'partial',
      output: { runtime_error: { code: 'x', retryable: true } }
    }
  ]
  const prompt = precedingUserPrompt(entries, entries[1])
  assert.equal(prompt.content, 'original prompt')
  assert.deepEqual(prompt.attachments, [{ name: 'note.md' }])
})

test('stored runtime events normalize for replay refresh merging', () => {
  const events = runtimeEventsFromReplayResponse({
    data: {
      events: [{
        event_type: 'session.tool.success',
        event_id: 'r1:ev000001',
        event_seq: 1,
        runtime_run_id: 'r1',
        action_id: 'a1',
        payload_json: { tool_name: 'write_file' },
        display_json: { title: '写入文件' },
        created_at: '2026-07-02T00:00:00Z'
      }]
    }
  })

  assert.equal(events.length, 1)
  assert.equal(events[0].type, 'session.tool.success')
  assert.equal(events[0].event_id, 'r1:ev000001')
  assert.equal(events[0].data.runtime_run_id, 'r1')
  assert.equal(events[0].data.action_id, 'a1')
  assert.deepEqual(events[0].display_json, { title: '写入文件' })
})

test('live public runtime events flatten payload_json and stay ordered after final entry merge', () => {
  const liveDelta = normalizeRuntimeEvent('session.text.delta', {
    event_id: 'run:9',
    event_seq: 9,
    runtime_run_id: 'run-1',
    payload_json: {
      event_id: 'run:9',
      event_seq: 9,
      runtime_run_id: 'run-1',
      assistant_message_id: 'assistant-1',
      text_id: 'text-1',
      delta: 'delayed response'
    }
  })
  const finalSummary = [
    { type: 'session.step.started', event_id: 'run:7', seq: 7, event_seq: 7 },
    { type: 'session.text.started', event_id: 'run:8', seq: 8, event_seq: 8 },
    { type: 'session.text.ended', event_id: 'run:10', seq: 10, event_seq: 10, text: 'delayed response' },
    { type: 'session.step.ended', event_id: 'run:11', seq: 11, event_seq: 11 }
  ]

  assert.equal(liveDelta.payload.delta, 'delayed response')
  assert.equal(liveDelta.payload.assistant_message_id, 'assistant-1')
  assert.deepEqual(
    mergeRuntimeEvents(finalSummary, [liveDelta]).map((event) => event.event_id),
    ['run:7', 'run:8', 'run:9', 'run:10', 'run:11']
  )
})

test('runtime replay response collapses streaming deltas and truncates huge tool results', () => {
  const huge = { items: Array.from({ length: 200 }, (_, index) => ({ id: index, name: `resource-${index}` })) }
  const events = runtimeEventsFromReplayResponse({
    data: {
      events: [
        { event_type: 'session.text.delta', event_id: 'd1', payload_json: { text_id: 't1', delta: 'hello ' } },
        { event_type: 'session.text.delta', event_id: 'd2', payload_json: { text_id: 't1', delta: 'world' } },
        { event_type: 'session.tool.success', event_id: 's1', payload_json: { call_id: 'c1', structured: huge } }
      ]
    }
  })

  assert.equal(events.length, 2)
  assert.equal(events[0].payload.delta, 'hello world')
  assert.equal(events[1].payload.structured.truncated, true)
  assert.ok(events[1].payload.structured.bytes > 1200)
})

test('agent chatbox uses compact message and runtime trace layout', async () => {
  const [source, traceSource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  ])

  assert.match(source, /class="chatbox-message__body"/)
  assert.match(source, /class="chatbox-answer"/)
  assert.doesNotMatch(source, /chatbox-reasoning--compact/)
  assert.match(source, /chatbox-message__meta-row/)
  assert.match(traceSource, /process-timeline/)
  assert.match(traceSource, /tool-result/)
  assert.match(traceSource, /approval/)
  assert.match(traceSource, /ai-runtime-event-line/)
  assert.match(traceSource, /ai-runtime-event-line__meta/)
  assert.match(traceSource, /ai-runtime-event-line__content/)
  assert.match(traceSource, /ai-runtime-event-line--mcp/)
  assert.match(traceSource, /ai-runtime-event-line--skill/)
  assert.match(traceSource, /ai-runtime-event-line--subagent/)
  assert.doesNotMatch(traceSource, /class="ai-runtime-timeline__body"/)
})

test('agent chatbox and runtime trace use global theme tokens for dark mode', async () => {
  const [source, traceSource, themeSource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/theme.js'), 'utf8')
  ])

  assert.match(themeSource, /html\.classList\.add\('dark'\)/)
  assert.match(source, /var\(--surface-base\)/)
  assert.match(source, /var\(--surface-sidebar\)/)
  assert.match(source, /var\(--surface-overlay\)/)
  assert.match(source, /var\(--terminal-bg\)/)
  assert.match(source, /var\(--danger-color\)/)
  assert.doesNotMatch(source, /background:\s*#(?:fff|fafafa|fbfcfe|f4f6f8)\b/i)
  assert.doesNotMatch(source, /color:\s*\$danger-color\b/)

  assert.match(traceSource, /--runtime-trace-bg:\s*var\(--surface-subtle\)/)
  assert.match(traceSource, /--runtime-trace-card-bg:\s*var\(--surface-overlay\)/)
  assert.match(traceSource, /--runtime-trace-danger-bg:\s*var\(--status-danger-soft\)/)
  assert.match(traceSource, /--runtime-trace-approval-bg:\s*var\(--status-warning-soft\)/)
  assert.match(traceSource, /--runtime-trace-subagent-bg:\s*var\(--primary-lighter\)/)
  assert.doesNotMatch(traceSource, /background:\s*#(?:fff|fafafa|fff5f5|fff9ec|f4f1ff)\b/i)
})

test('runtime trace renders subagent compact card and child thread controls', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')

  assert.match(traceSource, /ai-runtime-subagent-card/)
  assert.match(traceSource, /subagentProgress/)
  assert.match(traceSource, /查看子线程/)
  assert.match(traceSource, /child thread/)
  assert.match(traceSource, /回到父会话/)
  assert.match(traceSource, /blocked_approval/)
  assert.doesNotMatch(traceSource, /子运行事件流/)
})

test('runtime trace labels file output artifacts by generated path', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')

  assert.match(traceSource, /artifactButtonLabel\(artifactRef\)/)
  assert.match(traceSource, /function artifactButtonLabel\(artifactRef\)/)
  assert.match(traceSource, /artifactRef\?\.artifact_type === 'file_output'/)
  assert.match(traceSource, /isPatchArtifact\(artifactRef\)/)
  assert.match(traceSource, /artifactRef\?\.preview_json\?\.path/)
  assert.match(traceSource, /`文件 \$\{filePath\}`/)
  assert.match(traceSource, /`Patch \$\{patchPath\}`/)
  assert.match(traceSource, /`产物 \$\{shortId\(artifactRef\?\.artifact_id\)\}`/)
})

test('runtime trace can preview event-only file output artifacts', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')

  assert.match(traceSource, /const fallbackDetail = fallbackArtifactDetail\(artifactRef\)/)
  assert.match(traceSource, /artifactDetail\.value = fallbackDetail/)
  assert.match(traceSource, /function fallbackArtifactDetail\(artifactRef\)/)
  assert.match(traceSource, /artifact_type: 'file_output'/)
  assert.match(traceSource, /artifact_type: 'patch'/)
  assert.match(traceSource, /visibility: 'runtime_event'/)
  assert.match(traceSource, /source: 'runtime_event'/)
})

test('agent chatbox page follows process demo turn and agent card structure', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /class="turn"/)
  assert.match(source, /class="user-card"/)
  assert.match(source, /class="agent-card"/)
  assert.match(source, /class="final-answer"/)
  assert.match(source, /details[^>]+class="raw"/)
  assert.doesNotMatch(source, /class="chatbox-agentActions"/)
  assert.doesNotMatch(source, /chatboxStore\.pendingActions\.length/)
})

test('agent chatbox renders text and markdown inline but gates html behind preview', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /messageContentBlocks\(entry\)/)
  assert.match(source, /block\.kind === 'markdown'/)
  assert.match(source, /v-html="renderMessageMarkdown\(block\.text\)"/)
  assert.match(source, /block\.kind === 'html'/)
  assert.match(source, /openHtmlPreview\(block\)/)
  assert.match(source, /htmlPreviewDialogOpen/)
  assert.match(source, /:srcdoc="htmlPreviewDocument"/)
  assert.doesNotMatch(source, /v-html="entry\.content"/)

  const markdownHtml = renderMarkdownToHtml('# 标题\n\n- **重点**\n\n`code`')
  assert.match(markdownHtml, /<h1>标题<\/h1>/)
  assert.match(markdownHtml, /<ul><li><strong>重点<\/strong><\/li><\/ul>/)
  assert.match(markdownHtml, /<code>code<\/code>/)

  const tableHtml = renderMarkdownToHtml('| 精度 | 单参数字节数 |\n| --- | --- |\n| BF16/FP16 | 2 bytes |')
  assert.match(tableHtml, /<table>/)
  assert.match(tableHtml, /<th>精度<\/th>/)
  assert.match(tableHtml, /<td>BF16\/FP16<\/td>/)

  const blocks = contentBlocksFromEntry({
    content_blocks: [
      { type: 'text/markdown', text: '**直接渲染**' },
      { type: 'text/html', html: '<main>预览</main>' }
    ]
  })
  assert.deepEqual(blocks.map((block) => block.kind), ['markdown', 'html'])
  assert.equal(blocks[1].text, '<main>预览</main>')
  assert.match(buildHtmlPreviewDocument('<strong>ok</strong>'), /<iframe|<strong>ok<\/strong>|<!doctype html>/)
})

test('agent chatbox renders assistant plain text answers as markdown by default', () => {
  const blocks = contentBlocksFromEntry({
    role: 'assistant',
    content_blocks: [
      { type: 'text', text: '## GPU 占用\n\n- **服务**: easydo-agent\n- 状态: `running`' }
    ]
  })

  assert.equal(blocks.length, 1)
  assert.equal(blocks[0].kind, 'markdown')
  assert.match(renderMarkdownToHtml(blocks[0].text), /<h2>GPU 占用<\/h2>/)
  assert.match(renderMarkdownToHtml(blocks[0].text), /<strong>服务<\/strong>/)
})

test('runtime trace main process hides raw audit details', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')

  assert.doesNotMatch(traceSource, /class="ai-runtime-event-line__diagnostics"/)
  assert.doesNotMatch(traceSource, /eventDetailText\(item\)/)
  assert.doesNotMatch(traceSource, /raw events/)
  assert.doesNotMatch(traceSource, /line_kind/)
})

test('runtime trace renders codex and opencode style agent event lines', async () => {
  const [traceSource, processSource] = await Promise.all([
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/runtimeProcessEvents.js'), 'utf8')
  ])

  assert.match(processSource, /model\.tool_call_detected/)
  assert.match(processSource, /tool\.result_prepared/)
  assert.match(traceSource, /buildRuntimeProcessItems/)
  assert.match(traceSource, /process-timeline/)
  assert.match(processSource, /Answer/)
  assert.match(processSource, /reasoning_delta/)
  assert.match(processSource, /AUDIT_EVENTS/)
  assert.doesNotMatch(traceSource, /function eventLineKind/)
  assert.doesNotMatch(traceSource, /准备 Prompt/)
  assert.doesNotMatch(traceSource, /调用模型/)
  assert.doesNotMatch(traceSource, /时间线 \{\{ traceItems\.length \}\} 个节点/)
})

test('runtime process lines exclude user prompt — trace is for model output only', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      {
        type: 'session.prompted',
        payload: {
          event_id: 'prompt-1',
          message_id: 'msg-1',
          content: '帮我梳理 runtime 事件',
          attachments: [{ name: 'trace.json' }]
        }
      },
      { type: 'session.step.started', payload: { event_id: 'step-1', message_id: 'msg-1' } },
      { type: 'session.reasoning.ended', payload: { event_id: 'reason-1', text: '先读取事件定义。' } }
    ]
  })

  // session.prompted must NOT appear in the trace
  assert.equal(lines.find((item) => item.event === 'session.prompted'), undefined)
  // The first visible item should be the thought/step, not the user prompt
  assert.equal(lines[0].event, 'session.step.started')
  assert.equal(lines[0].lineKind, 'thought')
  assert.equal(visibleRuntimeProcessEvents([{ type: 'session.prompted', payload: { content: 'hello' } }]).length, 0)
})

test('runtime process lines show run start and tool result submission lifecycle', () => {
  const events = [
    { type: 'run.started', payload: { event_id: 'run-start-1', status: 'running', runtime_run_id: 'r_w1_001' } },
    { type: 'model.tool_result_submitted', payload: { event_id: 'submit-1', round: 2, tool_result_count: 2, status: 'submitted' } }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), ['run.started', 'model.tool_result_submitted'])

  const lines = buildRuntimeProcessItems({ events })
  assert.deepEqual(lines.map((item) => item.lineKind), ['model', 'model'])
  assert.equal(lines[0].lineTitle, '运行已开始')
  assert.equal(lines[0].lineTarget, 'r_w1_001')
  assert.equal(lines[0].lineSummary, 'runtime running')
  assert.equal(lines[1].lineTitle, '工具结果已提交给模型')
  assert.equal(lines[1].lineSummary, '已提交 2 个工具结果，等待模型继续处理')
})

test('runtime process lines append every explicit run terminal after the original run row', () => {
  const cases = [
    ['run.completed', 'success', '运行已完成'],
    ['run.failed', 'failed', '运行失败'],
    ['run.cancelled', 'cancelled', '运行已取消'],
    ['run.timeout', 'timeout', '运行超时'],
    ['run.awaiting_decision', 'waiting', '等待确认'],
    ['run.interrupted', 'interrupted', '运行已中断']
  ]

  cases.forEach(([eventType, status, title], index) => {
    const lines = buildRuntimeProcessItems({
      events: [
        { type: 'run.started', payload: { event_id: `run-state-${index}-1`, runtime_run_id: `run-${index}`, status: 'running' } },
        { type: eventType, payload: { event_id: `run-state-${index}-2`, runtime_run_id: `run-${index}`, status } }
      ]
    })

    assert.deepEqual(lines.map((item) => item.event), ['run.started', eventType])
    assert.equal(lines[0].status, 'running')
    assert.equal(lines[0].lineTitle, '运行已开始')
    assert.equal(lines[1].status, status)
    assert.equal(lines[1].lineTitle, title)
    assert.equal(lines[1].lineTarget, `run-${index}`)
  })
})

test('runtime interruption appends after the original run and Pi model wait events', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'run.started', payload: { event_id: 'interrupt-wait-1', runtime_run_id: 'run-interrupted', status: 'running' } },
      { type: 'session.step.started', payload: { event_id: 'interrupt-wait-2', model: { provider_id: 'openai', id: 'slow-model' } } },
      { type: 'run.interrupted', payload: { event_id: 'interrupt-wait-3', runtime_run_id: 'run-interrupted', status: 'interrupted' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), ['run.started', 'session.step.started', 'run.interrupted'])
  assert.equal(lines[0].status, 'running')
  assert.equal(lines[1].lineTitle, '正在请求模型')
  assert.equal(lines[2].status, 'interrupted')
})

test('runtime process lines only use real reasoning as thought and classify actions precisely', () => {
  const events = [
    {
      event: 'model.request_prepared',
      payload: {
        event_id: 'audit-1',
        phase: 'initial',
        display_json: { summary: 'prepare prompt audit' }
      }
    },
    {
      event: 'model.call_started',
      payload: {
        event_id: 'audit-2',
        display_json: { summary: 'call started audit' }
      }
    },
    {
      event: 'skill.loaded',
      payload: {
        event_id: 'skill-1',
        name: 'easydo-pipeline-diagnosis',
        display_json: { summary: '加载失败排查顺序' }
      }
    },
    {
      event: 'action.execution_started',
      payload: {
        event_id: 'mcp-1',
        tool_name: 'easydo.pipeline.list',
        input_json: { query: 'e1' },
        display_json: { summary: '查询 e1 pipeline' }
      }
    },
    {
      event: 'tool.result_prepared',
      payload: {
        event_id: 'result-1',
        tool_name: 'easydo.pipeline.list',
        display_json: { summary: '找到 pipeline #13' }
      }
    },
    {
      event: 'action.decision_required',
      payload: {
        event_id: 'action-1',
        action_id: 'act_1',
        display_json: {
          title: '等待确认: update parameters + trigger run',
          summary: '更新 e1 server 节点配置，并触发一次新运行。'
        }
      }
    },
    {
      event: 'subagent.spawned',
      payload: {
        event_id: 'subagent-1',
        name: 'Run Watcher',
        display_json: { summary: 'watch run_id=29 until terminal state' }
      }
    },
    {
      event: 'output_schema.validated',
      payload: {
        event_id: 'audit-3',
        valid: true
      }
    }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), [
    'model.request_prepared',
    'skill.loaded',
    'action.execution_started',
    'tool.result_prepared',
    'action.decision_required',
    'subagent.spawned',
    'output_schema.validated'
  ])

  const lines = buildRuntimeProcessItems({
    events,
    reasoning: '我需要先解析 e1，再看最近一次 run；不能直接重新运行。',
    answer: '失败原因是 server 节点缺少 OPENAI_BASE_URL。',
    timings: { first_reasoning_ms: 1300 }
  })

  assert.deepEqual(lines.map((item) => item.lineKind), [
    'model',
    'model',
    'skill',
    'mcp',
    'result',
    'action',
    'subagent',
    'context',
    'thought',
    'thought'
  ])
  assert.equal(lines[0].lineTitle, '已准备上下文 initial')
  assert.equal(lines[1].event, 'model.call_started')
  assert.equal(lines[3].lineMeta, '→ mcp')
  assert.equal(lines[4].lineSummary, '找到 pipeline #13')
  assert.equal(lines[5].lineMeta, 'Action')
  assert.equal(lines[7].lineTitle, '结构化输出已验证')
  assert.equal(lines[8].lineMeta, 'Thought: 1.3s')
  assert.equal(lines[8].reasoningText, '我需要先解析 e1，再看最近一次 run；不能直接重新运行。')
  assert.deepEqual(lines[8].sections.map((section) => section.kind), ['reasoning'])
  assert.equal(lines[9].answerText, '失败原因是 server 节点缺少 OPENAI_BASE_URL。')
  assert.deepEqual(lines[9].sections.map((section) => section.kind), ['answer'])
  assert.ok(!lines.some((item) => item.lineSummary.includes('prepare prompt audit')))
  assert.ok(lines.every((item) => item.lineKind !== 'thought' || item.sections.length === 1))
})

test('runtime process lines preserve one thought per model request in event order', () => {
  const lines = buildRuntimeProcessItems({
    reasoning: '这个聚合 reasoning 不应该覆盖流式过程事件。',
    answer: '这个聚合 answer 不应该覆盖流式过程事件。',
    events: [
      {
        event: 'model.call_started',
        payload: { event_id: 'model-1-start', phase: 'initial', round: 1 }
      },
      {
        event: 'reasoning_delta',
        payload: { event_id: 'think-1a', delta: '先定位 pipeline。', elapsed_ms: 800 }
      },
      {
        event: 'reasoning_delta',
        payload: { event_id: 'think-1b', delta: '再读取最近 run。', elapsed_ms: 1200 }
      },
      {
        event: 'answer_delta',
        payload: { event_id: 'answer-1', delta: '需要读取最近一次运行。', elapsed_ms: 1300 }
      },
      {
        event: 'model.call_completed',
        payload: { event_id: 'model-1-end', phase: 'initial', round: 1 }
      },
      {
        event: 'action.execution_started',
        payload: {
          event_id: 'mcp-1',
          tool_name: 'easydo.pipeline.runs',
          input_json: { pipeline_id: 13, limit: 1 }
        }
      },
      {
        event: 'tool.result_prepared',
        payload: {
          event_id: 'result-1',
          tool_name: 'easydo.pipeline.runs',
          display_json: { summary: '最近 run #28 failed' }
        }
      },
      {
        event: 'model.continuation_started',
        payload: { event_id: 'model-2-start', phase: 'tool_continuation', round: 2 }
      },
      {
        event: 'reasoning_delta',
        payload: { event_id: 'think-2', delta: '失败节点明确后继续读日志。', elapsed_ms: 1900 }
      },
      {
        event: 'answer_delta',
        payload: { event_id: 'answer-2', delta: '最终原因是镜像拉取失败。', elapsed_ms: 2500 }
      },
      {
        event: 'model.continuation_completed',
        payload: { event_id: 'model-2-end', phase: 'tool_continuation', round: 2 }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'model.call_started',
    'reasoning_delta',
    'answer_delta',
    'model.call_completed',
    'action.execution_started',
    'tool.result_prepared',
    'model.continuation_started',
    'reasoning_delta',
    'answer_delta',
    'model.continuation_completed'
  ])
  assert.equal(lines[1].reasoningText, '先定位 pipeline。再读取最近 run。')
  assert.equal(lines[1].lineMeta, 'Thought: 1.2s')
  assert.equal(lines[2].answerText, '需要读取最近一次运行。')
  assert.equal(lines[5].lineSummary, '最近 run #28 failed')
  assert.equal(lines[7].reasoningText, '失败节点明确后继续读日志。')
  assert.equal(lines[8].answerText, '最终原因是镜像拉取失败。')
  assert.ok(!lines.some((item) => item.lineSummary.includes('聚合 reasoning')))
  assert.ok(!lines.some((item) => item.lineSummary.includes('聚合 answer')))
})

test('runtime process lines preserve reasoning and answer delta whitespace', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { event: 'model.call_started', payload: { event_id: 'model-1-start' } },
      { event: 'reasoning_delta', payload: { event_id: 'think-1', delta: 'The user says:', elapsed_ms: 300 } },
      { event: 'reasoning_delta', payload: { event_id: 'think-2', delta: ' "add a pipeline".\nI should ask questions.' } },
      { event: 'answer_delta', payload: { event_id: 'answer-1', delta: '先确认：' } },
      { event: 'answer_delta', payload: { event_id: 'answer-2', delta: ' **目标** 和触发方式。' } },
      { event: 'model.call_completed', payload: { event_id: 'model-1-end' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'model.call_started',
    'reasoning_delta',
    'answer_delta',
    'model.call_completed'
  ])
  assert.equal(lines[1].reasoningText, 'The user says: "add a pipeline".\nI should ask questions.')
  assert.equal(lines[2].answerText, '先确认： **目标** 和触发方式。')
})

test('runtime process lines compact streamed delta bursts before building history items', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/runtimeProcessEvents.js'), 'utf8')
  const buildStart = traceSource.indexOf('export function buildRuntimeProcessItems')
  const buildEnd = traceSource.indexOf('function applyProcessItemLimit', buildStart)
  const buildSource = traceSource.slice(buildStart, buildEnd)

  assert.match(traceSource, /function compactStreamingDeltaEvents/)
  assert.match(buildSource, /compactStreamingDeltaEvents\(normalizeInputEvents\(events\)\)/)
})

test('runtime process lines keep aggregate model output as a single thought with reasoning and answer', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { event: 'skill.loaded', payload: { event_id: 'skill-1', name: 'grill-me' } },
      { event: 'model.call_started', payload: { event_id: 'model-1-start', phase: 'initial', round: 1 } },
      { event: 'model.call_completed', payload: { event_id: 'model-1-end', phase: 'initial', round: 1 } }
    ],
    reasoning: [
      '用户说 grill-me 添加一个新流水线。我需要先识别这是一个需求澄清任务。',
      '这不是立即创建流水线，而是要用 grill-me 方式追问目标、触发方式、参数和阶段。',
      '接下来应该组织问题，覆盖资源、凭证、产物、通知和回滚。',
      '最后给用户一个可直接逐项回答的问题清单。'
    ].join(''),
    answer: '请先回答流水线目标、触发方式、输入参数、任务阶段、资源、产物、通知和回滚策略。'
  })

  const thoughtLines = lines.filter((item) => item.lineKind === 'thought')
  assert.equal(thoughtLines.length, 2)
  assert.deepEqual(lines.map((item) => item.event), [
    'skill.loaded',
    'model.call_started',
    'model.call_completed',
    'reasoning',
    'answer'
  ])
  assert.match(thoughtLines[0].reasoningText, /需求澄清任务/)
  assert.match(thoughtLines[0].reasoningText, /回滚/)
  assert.equal(thoughtLines[1].answerText, '请先回答流水线目标、触发方式、输入参数、任务阶段、资源、产物、通知和回滚策略。')
  assert.deepEqual(thoughtLines[0].sections.map((section) => section.kind), ['reasoning'])
  assert.deepEqual(thoughtLines[1].sections.map((section) => section.kind), ['answer'])
  assert.ok(lines.every((item) => !item.lineSummary.includes('undefined')))
})

test('runtime process lines keep model thoughts visible when limiting later tool events', () => {
  const events = [
    { event: 'model.call_started', payload: { event_id: 'model-1-start' } },
    { event: 'reasoning_delta', payload: { event_id: 'think-1', delta: '先确认资源和工具。', elapsed_ms: 900 } },
    { event: 'answer_delta', payload: { event_id: 'answer-1', delta: '我会先查询资源列表。' } },
    { event: 'model.call_completed', payload: { event_id: 'model-1-end' } },
    ...Array.from({ length: 24 }, (_, index) => ({
      event: index % 2 === 0 ? 'action.execution_started' : 'tool.result_prepared',
      payload: {
        event_id: `tool-${index}`,
        tool_name: `easydo_resource_${index}`,
        display_json: { summary: `step ${index}` }
      }
    }))
  ]

  const lines = buildRuntimeProcessItems({ events, limit: 18 })
  const thoughts = lines.filter((item) => item.lineKind === 'thought')

  assert.equal(lines.length, 18)
  assert.equal(thoughts.length, 2)
  assert.equal(thoughts[0].reasoningText, '先确认资源和工具。')
  assert.equal(thoughts[1].answerText, '我会先查询资源列表。')
  assert.equal(lines[0].lineKind, 'thought')
})

test('runtime process lines show every model request even when it only emits tool calls', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { event: 'model.call_started', payload: { event_id: 'model-1-start', phase: 'initial', round: 1 } },
      { event: 'reasoning_delta', payload: { event_id: 'think-1', delta: '先查资源。', elapsed_ms: 900 } },
      { event: 'model.call_completed', payload: { event_id: 'model-1-end', phase: 'initial', round: 1, tool_call_count: 1 } },
      { event: 'model.tool_call_detected', payload: { event_id: 'tool-call-1', tool_name: 'easydo_resource_list' } },
      { event: 'action.execution_started', payload: { event_id: 'mcp-1', tool_name: 'easydo_resource_list' } },
      { event: 'tool.result_prepared', payload: { event_id: 'result-1', tool_name: 'easydo_resource_list' } },
      { event: 'model.tool_result_submitted', payload: { event_id: 'submit-1' } },
      { event: 'model.continuation_started', payload: { event_id: 'model-2-cont', phase: 'tool_continuation', round: 2 } },
      { event: 'model.call_started', payload: { event_id: 'model-2-start', phase: 'tool_continuation', round: 2 } },
      { event: 'model.call_completed', payload: { event_id: 'model-2-end', phase: 'tool_continuation', round: 2, tool_call_count: 1 } },
      { event: 'model.continuation_completed', payload: { event_id: 'model-2-cont-end', phase: 'tool_continuation', round: 2 } },
      { event: 'model.tool_call_detected', payload: { event_id: 'tool-call-2', tool_name: 'easydo_resource_gpu_usage' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'model.call_started',
    'reasoning_delta',
    'model.call_completed',
    'action.execution_started',
    'tool.result_prepared',
    'model.tool_result_submitted',
    'model.continuation_started',
    'model.call_started',
    'model.call_completed',
    'model.continuation_completed'
  ])
  assert.equal(lines[1].lineMeta, 'Thought: 900ms')
  assert.equal(lines[1].reasoningText, '先查资源。')
  assert.match(lines[4].lineSummary, /easydo_resource_list/)
  assert.equal(lines[5].lineTitle, '工具结果已提交给模型')
  assert.equal(lines[8].lineMeta, 'LLM tool continuation #2')
  assert.equal(lines[8].lineSummary, '请求工具 1 个')
})

test('runtime process lines keep detected tool calls as drafts until execution or approval', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { event: 'model.tool_call_detected', payload: { event_id: 'tool-call-1', tool_name: 'easydo_resource_get' } }
    ]
  })

  assert.deepEqual(lines, [])
})

test('runtime process lines keep low-level tool and approval lifecycle events in order', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { event: 'model.call_started', payload: { event_id: 'model-1-start', round: 1 } },
      { event: 'reasoning_delta', payload: { event_id: 'think-1', delta: '先解析 7022 到内部资源。', elapsed_ms: 1200 } },
      { event: 'model.call_completed', payload: { event_id: 'model-1-end', tool_call_count: 1 } },
      { event: 'model.tool_call_detected', payload: { event_id: 'detect-get', provider_tool_call_id: 'call-get', tool_name: 'easydo_resource_get' } },
      { event: 'action.execution_started', payload: { event_id: 'start-get', provider_tool_call_id: 'call-get', action_id: 'act-get', tool_name: 'easydo_resource_get', input_json: { arguments: { workspace_id: 1, resource_id: 7022 } } } },
      {
        event: 'tool.executed',
        payload: {
          event_id: 'executed-get',
          provider_tool_call_id: 'call-get',
          action_id: 'act-get',
          tool_name: 'easydo_resource_get',
          action: {
            action_id: 'act-get',
            result_json: {
              structured_content: { id: 2, name: '7022', status: 'online' }
            }
          },
          display_json: { summary: '模型请求执行 EasyDo 只读操作' }
        }
      },
      { event: 'tool.result_prepared', payload: { event_id: 'prepared-get', provider_tool_call_id: 'call-get', action_id: 'act-get', tool_name: 'easydo_resource_get', status: 'completed' } },
      { event: 'action.execution_succeeded', payload: { event_id: 'success-get', provider_tool_call_id: 'call-get', action_id: 'act-get', tool_name: 'easydo_resource_get' } },
      { event: 'model.continuation_started', payload: { event_id: 'model-2-cont', round: 2 } },
      { event: 'model.call_started', payload: { event_id: 'model-2-start', round: 2 } },
      { event: 'model.call_completed', payload: { event_id: 'model-2-end', tool_call_count: 1 } },
      { event: 'model.tool_call_detected', payload: { event_id: 'detect-refresh', provider_tool_call_id: 'call-refresh', tool_name: 'easydo_resource_base_info_refresh' } },
      { event: 'action.permission_evaluated', payload: { event_id: 'permission-refresh', provider_tool_call_id: 'call-refresh', action_id: 'act-refresh' } },
      { event: 'approval.requested', payload: { event_id: 'approval-refresh', provider_tool_call_id: 'call-refresh', action_id: 'act-refresh' } },
      { event: 'action.decision_required', payload: { event_id: 'decision-refresh', provider_tool_call_id: 'call-refresh', action_id: 'act-refresh', action: { action_id: 'act-refresh', capability_id: 'easydo_resource_base_info_refresh', input_json: { arguments: { workspace_id: 1, resource_id: 2 } } }, display_json: { summary: '刷新 7022 的显卡采集信息' } } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'model.call_started',
    'reasoning_delta',
    'model.call_completed',
    'action.execution_started',
    'tool.executed',
    'tool.result_prepared',
    'action.execution_succeeded',
    'model.continuation_started',
    'model.call_started',
    'model.call_completed',
    'approval.requested',
    'action.decision_required'
  ])
  assert.equal(lines[3].lineTitle, 'easydo_resource_get workspace_id=1 resource_id=7022')
  assert.equal(lines[3].reasonText, '先解析 7022 到内部资源。')
  assert.match(lines[4].lineSummary, /id=2/)
  assert.equal(lines[10].status, 'waiting')
  assert.equal(lines[11].lineMeta, 'Action')
  assert.match(lines[11].lineTitle, /easydo_resource_base_info_refresh/)
  assert.equal(lines[11].reasonText, '刷新 7022 的显卡采集信息')
  assert.match(lines[11].lineSummary, /刷新 7022/)
  assert.ok(!lines.some((item) => ['model.tool_call_detected', 'action.permission_evaluated'].includes(item.event)))
})

test('runtime process lines show approval requests immediately before decision event arrives', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { event: 'model.call_started', payload: { event_id: 'model-1-start', round: 1 } },
      { event: 'model.call_completed', payload: { event_id: 'model-1-end', tool_call_count: 1 } },
      { event: 'model.tool_call_detected', payload: { event_id: 'detect-refresh', provider_tool_call_id: 'call-refresh', tool_name: 'easydo_resource_base_info_refresh' } },
      {
        event: 'approval.requested',
        payload: {
          event_id: 'approval-refresh',
          provider_tool_call_id: 'call-refresh',
          action_id: 'act-refresh',
          approval_request: {
            approval_id: 'ap-refresh',
            reason: '刷新资源 7022 采集信息',
            permission_key: 'resource.refresh',
            risk_level: 'medium'
          }
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), ['model.call_started', 'model.call_completed', 'approval.requested'])
  assert.equal(lines[2].status, 'waiting')
  assert.match(lines[2].lineTitle, /easydo_resource_base_info_refresh|等待确认|需要确认/)
  // Waiting approval panel is title/target only; reason is not duplicated as summary noise.
  assert.equal(lines[2].lineSummary, '')
})

test('runtime process lines keep multiple approval panels for repeated tool calls with different call ids', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'permission.asked', payload: { event_id: 'pi-multi-approval-1', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', reason: 'Tool easydo_write requires approval', input: { value: 'same' } } },
      { type: 'permission.asked', payload: { event_id: 'pi-multi-approval-2', request_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_write', reason: 'Tool easydo_write requires approval', input: { value: 'same' } } }
    ]
  })

  assert.equal(lines.filter((item) => item.lineKind === 'action').length, 2)
  assert.deepEqual(lines.map((item) => item.data.call_id), ['call-1', 'call-2'])
})

test('runtime process lines keep persisted approval and decision events as separate rows', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      {
        type: 'model.call_started',
        payload: {
          event_id: 'r_w1_000018_00001b:ev0000m0',
          phase: 'tool_continuation',
          round: 2
        }
      },
      {
        type: 'model.call_completed',
        payload: {
          event_id: 'r_w1_000018_00001b:ev0000m1',
          phase: 'tool_continuation',
          round: 2,
          status: 'completed',
          tool_call_count: 1
        }
      },
      {
        type: 'approval.requested',
        payload: {
          event_id: 'r_w1_000018_00001b:ev0000m7',
          approval_id: 'ap_w1_000018_00001b_00001p',
          action_id: 'a_w1_000018_00001b_00001p',
          action_internal_id: 61,
          approval_request: {
            approval_id: 'ap_w1_000018_00001b_00001p',
            run_id: 'r_w1_000018_00001b',
            action_id: 'a_w1_000018_00001b_00001p',
            tool_name: 'easydo_resource_base_info_refresh',
            permission_key: 'tool:easydo_resource_base_info_refresh:write',
            risk_level: 'write',
            reason: '模型请求执行需要确认的 EasyDo 操作',
            input_preview: {
              tool_name: 'easydo_resource_base_info_refresh',
              arguments: { resource_id: 2, workspace_id: 1 }
            },
            affected_resources: [{ resource_type: 'resource', resource_id: '', operation_type: 'write' }]
          }
        },
        display_json: {
          event_type: 'approval.requested',
          title: 'approval.requested',
          summary: '',
          status: '',
          artifact_refs: []
        }
      },
      {
        type: 'action.decision_required',
        payload: {
          event_id: 'r_w1_000018_00001b:ev0000m8',
          action_id: 'a_w1_000018_00001b_00001p',
          action_internal_id: 61,
          action: {
            id: 'a_w1_000018_00001b_00001p',
            action_id: 'a_w1_000018_00001b_00001p',
            internal_id: 61,
            action_kind: 'mcp.tool',
            runtime_run_id: 'r_w1_000018_00001b',
            capability_id: 'easydo_resource_base_info_refresh',
            input_json: {
              provider_tool_call_id: 'chatcmpl-tool-a6362292320bd4cb',
              tool_name: 'easydo_resource_base_info_refresh',
              arguments: { resource_id: 2, workspace_id: 1 }
            },
            target_json: { target_type: 'resource', target_id: '' },
            policy_json: {
              operation_type: 'write',
              risk_summary: '模型请求执行需要确认的 EasyDo 操作',
              requires_decision: true,
              risk_level: 'write',
              permission_key: 'tool:easydo_resource_base_info_refresh:write'
            },
            display_json: {
              title: '调用工具 easydo_resource_base_info_refresh',
              name: 'easydo_resource_base_info_refresh',
              summary: '模型请求执行需要确认的 EasyDo 操作',
              input_preview: {
                tool_name: 'easydo_resource_base_info_refresh',
                arguments: { resource_id: 2, workspace_id: 1 }
              },
              approval_request: {
                approval_id: 'ap_w1_000018_00001b_00001p',
                action_id: 'a_w1_000018_00001b_00001p',
                tool_name: 'easydo_resource_base_info_refresh',
                reason: '模型请求执行需要确认的 EasyDo 操作'
              }
            },
            status: 'awaiting_decision'
          }
        },
        display_json: {
          event_type: 'action.decision_required',
          title: '需要确认 调用工具 easydo_resource_base_info_refresh',
          summary: '模型请求执行需要确认的 EasyDo 操作',
          status: 'awaiting_decision',
          artifact_refs: []
        }
      }
    ]
  })

  const actions = lines.filter((item) => item.lineKind === 'action')
  assert.equal(actions.length, 2)
  assert.deepEqual(actions.map((item) => item.event), ['approval.requested', 'action.decision_required'])
  assert.deepEqual(actions.map((item) => item.status), ['waiting', 'waiting'])
  assert.equal(actions[1].lineTitle, '需要确认 easydo_resource_base_info_refresh resource_id=2 workspace_id=1')
  assert.equal(actions[1].lineTarget, 'resource')
  assert.equal(actions[1].lineSummary, '')
})

test('runtime process lines keep approval decision result and reason on its event row', () => {
  const approvedLines = buildRuntimeProcessItems({
    events: [
      {
        event: 'approval.requested',
        payload: {
          event_id: 'approval-1',
          action_id: 'act-refresh',
          approval_request: {
            action_id: 'act-refresh',
            tool_name: 'easydo_resource_base_info_refresh',
            reason: '刷新资源 7022 采集信息',
            risk_level: 'write'
          }
        }
      },
      {
        event: 'action.decision',
        payload: {
          event_id: 'decision-1',
          action_id: 'act-refresh',
          decision: 'approve_session',
          decided_by: 'demo',
          reason: '本会话允许刷新资源状态'
        },
        display_json: {
          decision: 'approve_session',
          summary: '本会话允许刷新资源状态'
        }
      }
    ]
  })

  assert.equal(approvedLines.length, 1)
  assert.equal(approvedLines[0].event, 'action.decision')
  assert.equal(approvedLines[0].status, 'success')
  assert.match(String(approvedLines[0].lineTitle || ''), /已批准/)
  assert.match(String(approvedLines[0].lineSummary || approvedLines[0].resultSummary || ''), /本会话允许刷新资源状态|approve_session|已批准/)

  const rejectedLines = buildRuntimeProcessItems({
    events: [
      {
        event: 'permission.asked',
        payload: {
          event_id: 'permission-1',
          request_id: 'approval:call-1',
          call_id: 'call-1',
          tool_name: 'easydo_write',
          reason: 'Tool easydo_write requires approval'
        }
      },
      {
        event: 'permission.resolved',
        payload: {
          event_id: 'permission-2',
          request_id: 'approval:call-1',
          call_id: 'call-1',
          tool_name: 'easydo_write',
          result: 'rejected',
          reason: '用户拒绝写入工作区'
        }
      }
    ]
  })

  assert.equal(rejectedLines.length, 1)
  assert.equal(rejectedLines[0].event, 'permission.resolved')
  assert.equal(rejectedLines[0].status, 'rejected')
  assert.equal(rejectedLines[0].lineTitle, '已拒绝 easydo_write')
  assert.match(String(rejectedLines[0].lineSummary || rejectedLines[0].resultSummary || ''), /用户拒绝写入工作区|已拒绝/)
})

test('runtime process lines describe model request retries with user-facing labels', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'model.call_started', payload: { event_id: 'call-1', phase: 'action_continuation', round: 1, provider: 'openrouter', model: 'openai/gpt-oss-120b:free' } },
      { type: 'model.call_completed', payload: { event_id: 'call-1-end', phase: 'action_continuation', round: 1, status: 'empty', text_chars: 0, tool_call_count: 0, reasoning_chars: 2907 } },
      { type: 'provider.empty_output_detected', payload: { event_id: 'empty-1', status: 'retrying' }, display_json: { status: 'retrying' } },
      { type: 'model.request_prepared', payload: { event_id: 'request-2', phase: 'provider_continuation', round: 2, provider: 'openrouter', model: 'openai/gpt-oss-120b:free' } },
      { type: 'model.call_started', payload: { event_id: 'call-2', phase: 'provider_continuation', round: 2, provider: 'openrouter', model: 'openai/gpt-oss-120b:free' } },
      { type: 'answer_delta', payload: { event_id: 'answer-2', delta: '已触发采集。' } },
      { type: 'model.call_completed', payload: { event_id: 'call-2-end', phase: 'provider_continuation', round: 2, status: 'completed', text_chars: 6, tool_call_count: 0 } }
    ]
  })

  assert.equal(lines[0].lineMeta, 'LLM action continuation #1')
  assert.equal(lines[0].lineTitle, '模型请求已开始')
  assert.equal(lines[1].lineKind, 'model')
  assert.equal(lines[1].lineTitle, '模型请求已完成')
  assert.equal(lines[1].lineSummary, '模型仅返回内部推理')
  assert.equal(lines[2].lineKind, 'model')
  assert.equal(lines[2].lineTitle, '模型空输出重试')
  assert.equal(lines[3].lineTitle, '已准备上下文 provider continuation #2')
  assert.equal(lines[4].lineMeta, 'LLM provider continuation #2')
  assert.equal(lines[5].sections[0].text, '已触发采集。')
  assert.equal(lines[6].lineTitle, '模型请求已完成')
})

test('runtime process lines show provider continuation completion events', () => {
  const events = [
    { type: 'provider.empty_output_detected', payload: { event_id: 'empty-1', status: 'retrying' } },
    { type: 'provider.continuation_completed', payload: { event_id: 'recover-1', status: 'completed', attempt: 2 } }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), ['provider.empty_output_detected', 'provider.continuation_completed'])

  const lines = buildRuntimeProcessItems({ events })
  assert.deepEqual(lines.map((item) => item.lineKind), ['model', 'model'])
  assert.equal(lines[1].status, 'success')
  assert.equal(lines[1].lineTitle, '模型重试已恢复')
  assert.equal(lines[1].lineSummary, '第 2 次请求已返回可见回复')
})

test('runtime process lines show persisted run errors as failure results', () => {
  const events = [
    {
      type: 'run.error',
      payload: {
        event_id: 'run-error-1',
        code: 'provider_response_empty',
        message: 'Provider response did not include assistant text',
        retryable: false,
        details: { endpoint_dialect: 'openai-chat' }
      }
    }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), ['run.error'])

  const lines = buildRuntimeProcessItems({ events })
  assert.deepEqual(lines.map((item) => item.lineKind), ['result'])
  assert.equal(lines[0].status, 'failed')
  assert.equal(lines[0].lineTitle, '运行失败')
  assert.equal(lines[0].lineTarget, 'provider_response_empty')
  assert.equal(lines[0].lineSummary, 'Provider response did not include assistant text（不可重试）')
})

test('runtime process lines show prepared context before model calls without raw prompt details', () => {
  const events = [
    {
      type: 'model.request_prepared',
      payload: {
        event_id: 'request-context-1',
        phase: 'initial',
        round: 1,
        provider: 'openrouter',
        model: 'openai/gpt-oss-120b:free',
        content_chars: 128,
        history_count: 4,
        tool_count: 3,
        loaded_skill_count: 2,
        subagent_result_count: 1,
        has_output_schema: true,
        raw_prompt: 'do not render this raw prompt'
      }
    }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), ['model.request_prepared'])

  const lines = buildRuntimeProcessItems({ events })
  assert.deepEqual(lines.map((item) => item.lineKind), ['model'])
  assert.equal(lines[0].status, 'running')
  assert.equal(lines[0].lineMeta, 'LLM initial #1')
  assert.equal(lines[0].lineTitle, '已准备上下文 initial #1')
  assert.equal(lines[0].lineTarget, 'openrouter/openai/gpt-oss-120b:free')
  assert.equal(lines[0].lineSummary, '上下文 128 字符，历史 4 条，工具 3 个，技能 2 个，子结果 1 个，结构化输出')
  assert.ok(!lines[0].lineSummary.includes('raw prompt'))
})

test('runtime process lines show context build tags and fragments', () => {
  const events = [
    {
      type: 'context.build_started',
      payload: {
        event_id: 'context-1',
        profile_name: '页面助手',
        context_tags: ['workspace', 'page-assistant']
      }
    },
    {
      type: 'context_tags.resolved',
      payload: {
        event_id: 'context-2',
        tags: ['workspace', 'page-assistant'],
        fragments: [
          { tag: 'workspace', title: '工作区' },
          { tag: 'page-assistant', title: '当前页面' }
        ],
        warnings: ['page metadata partial']
      }
    },
    {
      type: 'context.build_completed',
      payload: {
        event_id: 'context-3',
        context_tag_count: 2,
        context_fragment_count: 2,
        skill_count: 1,
        mcp_tool_count: 3,
        subagent_result_count: 1,
        model_content_chars: 256,
        has_output_schema: true
      }
    }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), ['context.build_started', 'context_tags.resolved', 'context.build_completed'])

  const lines = buildRuntimeProcessItems({ events })
  assert.deepEqual(lines.map((item) => item.lineKind), ['context', 'context', 'context'])
  assert.equal(lines[0].lineTitle, '开始准备上下文')
  assert.equal(lines[0].status, 'running')
  assert.equal(lines[0].lineSummary, '标签 workspace, page-assistant')
  assert.equal(lines[1].lineTitle, '已解析上下文标签')
  assert.equal(lines[1].lineTarget, '2 个片段')
  assert.equal(lines[1].lineSummary, 'workspace: 工作区；page-assistant: 当前页面；警告 page metadata partial')
  assert.equal(lines[2].lineTitle, '上下文准备完成')
  assert.equal(lines[2].status, 'success')
  assert.equal(lines[2].lineSummary, '标签 2 个，片段 2 个，技能 1 个，MCP 工具 3 个，子结果 1 个，模型上下文 256 字符，结构化输出')
})

test('runtime process lines show prepared capabilities tools and output schema constraints', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      {
        type: 'capability.snapshot',
        payload: {
          event_id: 'capability-1',
          skills: [{ name: 'diagnose' }],
          mcp_servers: [{ name: 'easydo' }],
          mcp_tools: [{ name: 'easydo_resource_list' }, { name: 'easydo_resource_gpu_usage' }],
          subagents: [{ name: 'Run Watcher' }],
          l5_controls: { output_schema_validation: true, write_tool_approval: true }
        }
      },
      {
        type: 'mcp.tools.available',
        payload: {
          event_id: 'mcp-tools-1',
          tool_count: 2,
          tools: [
            { name: 'easydo_resource_list', description: '列出资源' },
            { name: 'easydo_resource_gpu_usage', description: '查看 GPU' }
          ]
        }
      },
      {
        type: 'output_schema.available',
        payload: {
          event_id: 'schema-1',
          schema: {
            type: 'object',
            required: ['summary', 'status'],
            properties: {
              summary: { type: 'string' },
              status: { type: 'string' }
            }
          }
        }
      },
      {
        type: 'output_schema.validated',
        payload: {
          event_id: 'schema-valid-1',
          valid: true
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'capability.snapshot',
    'mcp.tools.available',
    'output_schema.available',
    'output_schema.validated'
  ])
  assert.deepEqual(lines.map((item) => item.lineKind), ['context', 'context', 'context', 'context'])
  assert.equal(lines[0].lineTitle, '运行能力已准备')
  assert.equal(lines[0].lineTarget, '2 个工具')
  assert.match(lines[0].lineSummary, /技能 1 个/)
  assert.match(lines[0].lineSummary, /MCP 服务 1 个/)
  assert.match(lines[0].lineSummary, /子 Agent 1 个/)
  assert.match(lines[0].lineSummary, /审批控制/)
  assert.equal(lines[1].lineTitle, 'MCP 工具已准备')
  assert.equal(lines[1].lineTarget, '2 个工具')
  assert.match(lines[1].lineSummary, /easydo_resource_list/)
  assert.match(lines[1].lineSummary, /easydo_resource_gpu_usage/)
  assert.equal(lines[2].lineTitle, '结构化输出约束已准备')
  assert.equal(lines[2].lineTarget, '2 个必填字段')
  assert.match(lines[2].lineSummary, /summary/)
  assert.match(lines[2].lineSummary, /status/)
  assert.equal(lines[3].lineTitle, '结构化输出已验证')
  assert.equal(lines[3].status, 'success')
})

test('runtime process lines surface MCP discovery failures before context preparation', () => {
  const events = [
    {
      type: 'run.started',
      payload: { event_id: 'run-started-1', runtime_run_id: 'run-1' }
    },
    {
      type: 'mcp.tools_discovery_failed',
      payload: {
        event_id: 'mcp-failed-1',
        mcp_server: 'remote-tools',
        message: 'MCP request timed out after 150ms'
      }
    },
    {
      type: 'context.build_started',
      payload: { event_id: 'context-started-1' }
    }
  ]

  const visible = visibleRuntimeProcessEvents(events)
  assert.deepEqual(visible.map((item) => item.event), [
    'run.started',
    'mcp.tools_discovery_failed',
    'context.build_started'
  ])

  const lines = buildRuntimeProcessItems({ events })
  const failure = lines.find((item) => item.event === 'mcp.tools_discovery_failed')
  assert.equal(failure?.lineKind, 'context')
  assert.equal(failure?.status, 'failed')
  assert.equal(failure?.lineTitle, 'MCP 工具发现失败')
  assert.equal(failure?.lineTarget, 'remote-tools')
  assert.equal(failure?.lineSummary, 'MCP request timed out after 150ms')
})

test('runtime process lines render steer checkpoints as continued user guidance', () => {
  const lines = buildRuntimeProcessItems({
    events: [{
      type: 'session.steered',
      payload: {
        event_id: 'steer-1',
        instruction: 'Inspect the current state instead.'
      }
    }]
  })

  assert.equal(lines.length, 1)
  assert.equal(lines[0].lineKind, 'action')
  assert.equal(lines[0].status, 'success')
  assert.equal(lines[0].lineTitle, '已调整执行方向')
  assert.equal(lines[0].lineSummary, 'Inspect the current state instead.')
})

test('runtime process lines show synthesized answers after unresolved tool calls', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'model.call_started', payload: { event_id: 'call-1', phase: 'tool_continuation', round: 3, provider: 'openrouter', model: 'openai/gpt-oss-120b:free' } },
      { type: 'model.call_completed', payload: { event_id: 'call-1-end', phase: 'tool_continuation', round: 3, status: 'completed', text_chars: 0, tool_call_count: 1, finish_reason: 'tool_calls' } },
      { type: 'model.answer_synthesized', payload: { event_id: 'synth-1', source: 'tool_results', reason: 'empty_assistant_after_tool_loop', tool_result_count: 1, text_chars: 70 } }
    ],
    answer: '工具 easydo_resource_list 已执行，结果：{"list":[],"total":0,"page":1,"limit":20}'
  })

  assert.equal(lines[0].lineMeta, 'LLM tool continuation #3')
  assert.equal(lines[0].lineTitle, '模型请求已开始')
  assert.equal(lines[1].lineKind, 'model')
  assert.equal(lines[1].lineTitle, '模型请求已完成')
  assert.equal(lines[1].lineSummary, '请求工具 1 个')
  assert.equal(lines[2].lineTitle, '已根据工具结果生成回复')
  assert.equal(lines[2].lineSummary, '模型未返回可见文本，已用 1 个工具结果生成最终回复')
  assert.equal(lines[3].sections[0].text, '工具 easydo_resource_list 已执行，结果：{"list":[],"total":0,"page":1,"limit":20}')
})

test('runtime process lines keep subagent lifecycle as chronological child thread rows', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      {
        event: 'subagent.spawned',
        payload: {
          event_id: 'subagent-1',
          child_run_link_id: 'link_w1_000001',
          child_runtime_run_id: 'r_child_w1_000001',
          agent_name: 'Run Watcher',
          task: 'watch run #29 until terminal state',
          display_json: {
            title: 'Run Watcher',
            summary: '观察 run #29 的节点状态',
            child_run_link_id: 'link_w1_000001',
            child_runtime_run_id: 'r_child_w1_000001'
          }
        }
      },
      {
        event: 'subagent.progress',
        payload: {
          event_id: 'subagent-2',
          child_run_link_id: 'link_w1_000001',
          child_runtime_run_id: 'r_child_w1_000001',
          status: 'running',
          display_json: {
            summary: 'server 已通过启动校验'
          }
        }
      },
      {
        event: 'subagent.progress',
        payload: {
          event_id: 'subagent-2-reason',
          child_run_link_id: 'link_w1_000001',
          child_runtime_run_id: 'r_child_w1_000001',
          reason: '主会话需要持续监听长任务状态'
        }
      },
      {
        event: 'subagent.blocked_approval',
        payload: {
          event_id: 'subagent-3',
          child_run_link_id: 'link_w1_000001',
          child_runtime_run_id: 'r_child_w1_000001',
          parent_action_id: 'act_health',
          status: 'blocked_approval',
          display_json: {
            summary: '健康检查需要在 parent 会话确认'
          }
        }
      },
      {
        event: 'subagent.completed',
        payload: {
          event_id: 'subagent-4',
          child_run_link_id: 'link_w1_000001',
          child_runtime_run_id: 'r_child_w1_000001',
          status: 'completed',
          display_json: {
            summary: 'run #29 所有节点成功，健康检查 200',
            artifact_refs: [{ artifact_id: 'art_run_29_summary' }]
          }
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'subagent.spawned',
    'subagent.progress',
    'subagent.progress',
    'subagent.blocked_approval',
    'subagent.completed'
  ])
  assert.deepEqual(lines.map((item) => item.lineKind), ['subagent', 'subagent', 'subagent', 'subagent', 'subagent'])
  assert.deepEqual(lines.map((item) => item.status), ['running', 'running', 'running', 'waiting', 'success'])
  assert.equal(lines[0].childRunLinkId, 'link_w1_000001')
  assert.equal(lines[0].childRuntimeRunId, 'r_child_w1_000001')
  assert.equal(lines[0].lineTitle, 'Run Watcher watch run #29 until terminal state')
  assert.deepEqual(lines.map((item) => item.lineSummary), [
    '观察 run #29 的节点状态',
    'server 已通过启动校验',
    '主会话需要持续监听长任务状态',
    '健康检查需要在 parent 会话确认',
    'run #29 所有节点成功，健康检查 200'
  ])
  assert.deepEqual(lines[4].artifactRefs.map((item) => item.artifact_id), ['art_run_29_summary'])
})

test('runtime process lines render Pi OpenCode-style tool approval and result events', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.prompted', payload: { event_id: 'pi-1', prompt: 'call write tool' } },
      { type: 'session.step.started', payload: { event_id: 'pi-2', agent: 'Draft Common Agent', model: { provider_id: 'test', id: 'fake' } } },
      { type: 'session.reasoning.ended', payload: { event_id: 'pi-3', text: '需要调用写工具。' } },
      { type: 'permission.asked', payload: { event_id: 'pi-4', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', reason: 'Tool easydo_write requires approval', input: { value: 'x' } } },
      { type: 'permission.resolved', payload: { event_id: 'pi-5', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', result: 'approved' } },
      { type: 'session.tool.called', payload: { event_id: 'pi-6', call_id: 'call-1', tool: 'easydo_write', input: { value: 'x' } } },
      { type: 'session.tool.success', payload: { event_id: 'pi-7', call_id: 'call-1', tool: 'easydo_write', content: [{ type: 'text', text: 'write ok' }], structured: { ok: true } } },
      { type: 'session.text.ended', payload: { event_id: 'pi-8', text: '写入完成。' } },
      { type: 'session.step.ended', payload: { event_id: 'pi-9', finish_reason: 'stop' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.ended',
    'permission.resolved',
    'session.tool.called',
    'session.tool.success',
    'session.text.ended',
    'session.step.ended'
  ])
  assert.equal(lines[1].sections[0].text, '需要调用写工具。')
  assert.equal(lines[2].status, 'success')
  assert.match(String(lines[2].lineTitle || ''), /已批准/)
  assert.equal(lines[3].event, 'session.tool.called')
  assert.equal(lines[4].event, 'session.tool.success')
  assert.equal(lines[4].lineSummary, 'ok=true')
  assert.equal(lines[5].sections[0].text, '写入完成。')
})

test('runtime process lines keep a real same-call failure after the waiting approval row', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 's1', agent: 'Draft Common Agent', model: { provider_id: 'openai-compatible', id: 'slow-model' } } },
      { type: 'session.reasoning.started', payload: { event_id: 'r1', assistant_message_id: 'a1', reasoning_id: 'r1' } },
      { type: 'session.tool.input.started', payload: { event_id: 'tis1', call_id: 'call-504C', tool_name: 'easydo_resource_list' } },
      { type: 'session.reasoning.ended', payload: { event_id: 're1', text: 'Let me list resources.' } },
      { type: 'session.tool.input.ended', payload: { event_id: 'tie1', call_id: 'call-504C', tool_name: 'easydo_resource_list' } },
      { type: 'session.tool.called', payload: { event_id: 'tc1', call_id: 'call-504C', tool: 'easydo_resource_list', tool_name: 'easydo_resource_list' } },
      { type: 'action.permission_evaluated', payload: { event_id: 'pe1', call_id: 'call-504C', tool_name: 'easydo_resource_list', decision: 'ask', reason: 'ask by mcp.default_request', permission_key: 'mcp:easydo:tools:easydo_resource_list:read', operation_type: 'read' } },
      { type: 'permission.asked', payload: { event_id: 'pa1', request_id: 'approval:call-504C', approval_id: 'approval:call-504C', call_id: 'call-504C', tool_name: 'easydo_resource_list', reason: 'List resources in one workspace', message: 'List resources in one workspace', tool_description: 'List resources in one workspace', input: { limit: 20, workspace_id: 1 }, resource_type: 'mcp_server', resource_id: 'easydo', mcp_server_key: 'easydo', executor_type: 'mcp', capability: 'tools' } },
      { type: 'session.tool.failed', payload: { event_id: 'tf1', call_id: 'call-504C', tool: 'easydo_resource_list', tool_name: 'easydo_resource_list', error: { type: 'tool_error', message: 'List resources in one workspace' }, result: { content: [{ type: 'text', text: 'List resources in one workspace' }], details: {} } } }
    ]
  })

  const actionLine = lines.find((item) => item.lineKind === 'action')
  assert.ok(actionLine, 'action card should still exist')
  assert.equal(actionLine.status, 'waiting')
  assert.equal(actionLine.event, 'permission.asked')
  assert.equal(actionLine.lineTitle, '需要确认 easydo_resource_list')
  assert.equal(actionLine.lineSummary, '')
  assert.equal(lines.some((item) => item.event === 'session.tool.failed'), false)
})

test('runtime process lines keep the Pi model wait visible until the first token or failure', () => {
  const waiting = buildRuntimeProcessItems({
    events: [{
      type: 'session.step.started',
      timestamp: '2026-07-11T10:00:00.000Z',
      payload: {
        event_id: 'pi-wait-1',
        agent: 'Draft Common Agent',
        model: { provider_id: 'openai-compatible', id: 'slow-model' }
      }
    }],
    now: '2026-07-11T10:00:30.000Z'
  })

  assert.equal(waiting.length, 1)
  assert.equal(waiting[0].lineKind, 'thought')
  assert.equal(waiting[0].status, 'running')
  assert.equal(waiting[0].modelCall, true)
  assert.equal(waiting[0].lineMeta, 'LLM')
  assert.equal(waiting[0].lineTitle, '正在请求模型')
  assert.equal(waiting[0].lineTarget, 'openai-compatible/slow-model')
  assert.equal(waiting[0].lineSummary, '等待首个响应 30 秒')

  const thinking = buildRuntimeProcessItems({
    events: [
      waiting[0].raw,
      {
        type: 'session.reasoning.started',
        timestamp: '2026-07-11T10:00:31.000Z',
        payload: { event_id: 'pi-wait-2', assistant_message_id: 'assistant-1', reasoning_id: 'reasoning-1' }
      }
    ],
    now: '2026-07-11T10:00:31.000Z'
  })

  assert.equal(thinking.length, 2)
  assert.equal(thinking[0].key, waiting[0].key)
  assert.equal(thinking[0].status, 'success')
  assert.equal(thinking[0].lineTitle, '模型响应完成')
  assert.equal(thinking[0].lineSummary, '等待首个响应 31 秒')
  assert.equal(thinking[1].event, 'session.reasoning.started')
  assert.equal(thinking[1].status, 'running')
  assert.equal(thinking[1].lineTitle, '开始思考')

  const timedOut = buildRuntimeProcessItems({
    events: [
      waiting[0].raw,
      {
        type: 'session.step.failed',
        timestamp: '2026-07-11T10:01:00.000Z',
        payload: {
          event_id: 'pi-wait-3',
          error: { type: 'provider_timeout', message: 'Request timed out.' }
        }
      }
    ],
    now: '2026-07-11T10:01:00.000Z'
  })

  assert.equal(timedOut[0].key, waiting[0].key)
  assert.equal(timedOut[0].status, 'failed')
  assert.equal(timedOut[0].lineTitle, '模型请求失败')
  assert.equal(timedOut[0].lineSummary, 'Request timed out.')
  assert.equal(timedOut[1].event, 'session.step.failed')
  assert.equal(timedOut[1].status, 'failed')
  assert.equal(timedOut[1].lineSummary, 'Request timed out.')

  const completed = buildRuntimeProcessItems({
    events: [
      {
        type: 'session.step.started',
        timestamp: '2026-07-11T10:00:00.000Z',
        payload: {
          event_id: 'pi-done-1',
          model: { provider_id: 'openai-compatible', id: 'fast-model' }
        }
      },
      {
        type: 'session.text.delta',
        timestamp: '2026-07-11T10:00:02.000Z',
        payload: { event_id: 'pi-done-2', delta: '最终回复' }
      },
      {
        type: 'session.step.ended',
        timestamp: '2026-07-11T10:00:03.000Z',
        payload: { event_id: 'pi-done-3' }
      }
    ],
    now: '2026-07-11T10:05:00.000Z'
  })
  assert.equal(completed.length, 3)
  assert.equal(completed[0].status, 'success')
  assert.equal(completed[0].lineTitle, '模型响应完成')
  assert.equal(completed[0].lineSummary, '等待首个响应 2 秒')
  assert.equal(completed[1].event, 'session.text.delta')
  assert.equal(completed[1].sections.some((section) => section.kind === 'answer' && section.text.includes('最终回复')), true)
  assert.equal(completed[2].event, 'session.step.ended')
  assert.equal(completed[2].status, 'success')
})

test('runtime process lines keep chronological tool/step actions around approval pauses', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-approval-pause-1', agent: 'Draft Common Agent' } },
      { type: 'session.tool.called', payload: { event_id: 'pi-approval-pause-2', call_id: 'call-1', tool: 'easydo_write', tool_name: 'easydo_write', input: { value: 'x' } } },
      { type: 'permission.asked', payload: { event_id: 'pi-approval-pause-3', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', reason: 'Tool easydo_write requires approval', input: { value: 'x' } } },
      { type: 'session.tool.failed', payload: { event_id: 'pi-approval-pause-4', call_id: 'call-1', tool: 'easydo_write', tool_name: 'easydo_write', error: { message: 'Tool easydo_write requires approval' } } },
      { type: 'session.text.ended', payload: { event_id: 'pi-approval-pause-5', text: '需要用户审批后继续。' } },
      { type: 'session.step.ended', payload: { event_id: 'pi-approval-pause-6', finish_reason: 'stop' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.tool.called',
    'permission.asked',
    'session.text.ended',
    'session.step.ended'
  ])
  assert.equal(lines[2].lineKind, 'action')
  assert.equal(lines[2].status, 'waiting')
  assert.equal(lines[2].lineTitle, '需要确认 easydo_write')
  assert.equal(lines[2].lineSummary, '')
  assert.equal(lines.some((item) => item.event === 'session.tool.failed'), false)
  assert.equal(lines[4].status, 'success')
})

test('runtime process lines mark resolved approvals and keep decision visible', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'permission.asked', payload: { event_id: 'ask-1', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', reason: 'confirm write' } },
      { type: 'permission.resolved', payload: { event_id: 'resolve-1', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', decision: 'approve_once', result: 'approved' } },
      { type: 'session.tool.called', payload: { event_id: 'call-after', call_id: 'call-1', tool: 'easydo_write', input: { value: 'x' } } },
      { type: 'session.tool.success', payload: { event_id: 'ok-1', call_id: 'call-1', tool: 'easydo_write', structured: { ok: true } } }
    ]
  })

  const resolved = lines.find((item) => item.event === 'permission.resolved')
  assert.ok(resolved)
  assert.equal(lines.filter((item) => item.lineKind === 'action').length, 1)
  assert.equal(resolved.status, 'success')
  assert.match(String(resolved.lineTitle || ''), /已批准/)
  assert.equal(String(resolved.data?.decision || resolved.display?.decision || '').trim(), 'approve_once')
  assert.ok(lines.some((item) => item.event === 'session.tool.success' && item.status === 'success'))
})

test('runtime process lines sort by event_seq and keep failures after earlier actions', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.failed', payload: { event_seq: 40, event_id: 'fail-last', error: { message: 'later failure' } } },
      { type: 'session.step.started', payload: { event_seq: 10, event_id: 'step-first' } },
      { type: 'session.tool.called', payload: { event_seq: 20, event_id: 'tool-mid', call_id: 'call-1', tool: 'easydo_write' } },
      { type: 'permission.asked', payload: { event_seq: 30, event_id: 'ask-mid', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', reason: 'confirm write' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.tool.called',
    'permission.asked',
    'session.step.failed'
  ])
  assert.equal(lines[lines.length - 1].lineSummary.includes('later failure') || lines[lines.length - 1].status === 'failed', true)
})

test('runtime process lines keep approval-wait narration at its original position', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-approval-narration-1', agent: 'kk' } },
      {
        type: 'session.reasoning.ended',
        payload: {
          event_id: 'pi-approval-narration-2',
          text: '两个 refresh 都需要审批。我需要等待用户审批。我应该告诉用户已触发审批，等待确认。'
        }
      },
      {
        type: 'session.text.ended',
        payload: {
          event_id: 'pi-approval-narration-3',
          text: '两个刷新请求已提交审批，待用户批准后我会查询结果。'
        }
      },
      {
        type: 'permission.asked',
        payload: {
          event_id: 'pi-approval-narration-4',
          request_id: 'approval:call-1',
          call_id: 'call-1',
          tool_name: 'easydo_resource_base_info_refresh',
          reason: 'Tool easydo_resource_base_info_refresh requires approval',
          input: { resource_id: 2 }
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.ended',
    'session.text.ended',
    'permission.asked'
  ])
  assert.ok(JSON.stringify(lines[1]).includes('等待用户审批'))
  assert.equal(lines[3].status, 'waiting')
})

test('runtime process lines keep real step answers that arrive after permission.asked', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'keep-answer-1', agent: 'kk' } },
      { type: 'session.reasoning.ended', payload: { event_id: 'keep-answer-2', text: '需要刷新两台机器。' } },
      {
        type: 'permission.asked',
        payload: {
          event_id: 'keep-answer-3',
          request_id: 'approval:call-1',
          call_id: 'call-1',
          tool_name: 'easydo_resource_base_info_refresh',
          reason: 'Tool easydo_resource_base_info_refresh requires approval'
        }
      },
      {
        type: 'session.reasoning.ended',
        payload: {
          event_id: 'keep-answer-4',
          text: '两个刷新操作都需要审批。根据 capability disclosure policy，我应该停止，等待审批。不要叙述等待。'
        }
      },
      {
        type: 'session.text.ended',
        payload: {
          event_id: 'keep-answer-5',
          text: '我来帮你重新采集非沐曦环境的 GPU 使用情况。先定位资源，再触发刷新。'
        }
      },
      { type: 'session.step.ended', payload: { event_id: 'keep-answer-6', finish_reason: 'stop' } },
      {
        type: 'permission.resolved',
        payload: { event_id: 'keep-answer-7', request_id: 'approval:call-1', call_id: 'call-1', result: 'approved' }
      },
      { type: 'session.step.started', payload: { event_id: 'keep-answer-8', agent: 'kk' } },
      { type: 'session.text.ended', payload: { event_id: 'keep-answer-9', text: '刷新任务已提交。' } }
    ]
  })

  const answers = lines
    .filter((item) => item.lineKind === 'thought')
    .flatMap((item) => item.sections || [])
    .filter((section) => section.kind === 'answer')
    .map((section) => section.text)

  assert.deepEqual(answers, [
    '我来帮你重新采集非沐曦环境的 GPU 使用情况。先定位资源，再触发刷新。',
    '刷新任务已提交。'
  ])
  assert.ok(JSON.stringify(lines).includes('不要叙述等待'))
})

test('agent chatbox metrics sum billed step tokens without double-counting duplicate step.ended events', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_steps',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'session.step.started', event_id: 's1', timestamp: '2026-07-06T00:00:00.000Z' },
        { type: 'session.step.ended', event_id: 'e1', tokens: { input: 100, output: 20, cache: { read: 500, write: 0 } }, timestamp: '2026-07-06T00:00:02.000Z' },
        { type: 'session.step.ended', event_id: 'e1', tokens: { input: 100, output: 20, cache: { read: 500, write: 0 } }, timestamp: '2026-07-06T00:00:02.000Z' },
        { type: 'session.step.started', event_id: 's2', timestamp: '2026-07-06T00:00:10.000Z' },
        { type: 'session.step.ended', event_id: 'e2', tokens: { input: 800, output: 40, cache: { read: 0, write: 0 } }, timestamp: '2026-07-06T00:00:14.000Z' }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  // Dedup by event_id so replay merges do not inflate totals.
  assert.equal(byKey.tokens, '输入 900 / 输出 60 / 缓存 500')
  assert.equal(byKey.llm_requests, '2次')
})

test('agent chatbox stream handlers reset reasoning and answer accumulators on each new step', async () => {
  const [storeSource, controllerSource, projectionSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/runtimeSessionController.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/runtimeSessionStreamProjection.js'), 'utf8')
  ])
  assert.match(storeSource, /sessionController\.handleRuntimeEventPayload/)
  assert.match(controllerSource, /applyRuntimeStreamEventToAssistantEntry/)
  assert.match(projectionSource, /session\.step\.started/)
  assert.match(projectionSource, /entry\.output\.reasoning = ''/)
  assert.match(projectionSource, /session\.text\.ended[\s\S]*entry\.content = finalText/)
})

test('runtime process lines do not reinject previous answer while a later step is still streaming', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-redisplay-1', agent: 'kk' } },
      { type: 'session.text.ended', payload: { event_id: 'pi-redisplay-2', text: '第一轮结论。' } },
      { type: 'session.step.ended', payload: { event_id: 'pi-redisplay-3', finish_reason: 'stop' } },
      { type: 'session.step.started', payload: { event_id: 'pi-redisplay-4', agent: 'kk' } },
      { type: 'session.reasoning.delta', payload: { event_id: 'pi-redisplay-5', delta: '继续处理。' } }
    ],
    answer: '第一轮结论。'
  })

  const answers = lines
    .filter((item) => item.lineKind === 'thought')
    .flatMap((item) => item.sections || [])
    .filter((section) => section.kind === 'answer')
    .map((section) => section.text)

  assert.deepEqual(answers, ['第一轮结论。'])
})


test('runtime process lines keep reasoning before interleaved tool input in Pi event order', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-interleave-1', agent: 'k1', model: { provider_id: 'openrouter', id: 'm1' } } },
      { type: 'session.reasoning.started', payload: { event_id: 'pi-interleave-2', assistant_message_id: 'assistant-1', reasoning_id: 'assistant-1:reasoning:0' } },
      { type: 'session.tool.input.started', payload: { event_id: 'pi-interleave-3', call_id: 'call-1', tool_name: 'easydo_resource_base_info_refresh', assistant_message_id: 'assistant-1' } },
      {
        type: 'session.reasoning.ended',
        payload: {
          event_id: 'pi-interleave-4',
          assistant_message_id: 'assistant-1',
          reasoning_id: 'assistant-1:reasoning:0',
          text: 'User wants GPU collection for resource 7022.'
        }
      },
      {
        type: 'session.tool.input.ended',
        payload: { event_id: 'pi-interleave-5', call_id: 'call-1', text: '{"resource_id":7022,"workspace_id":0}' }
      },
      {
        type: 'session.tool.called',
        payload: { event_id: 'pi-interleave-6', call_id: 'call-1', tool: 'easydo_resource_base_info_refresh', input: { resource_id: 7022, workspace_id: 0 } }
      },
      {
        type: 'session.tool.failed',
        payload: {
          event_id: 'pi-interleave-7',
          call_id: 'call-1',
          tool: 'easydo_resource_base_info_refresh',
          error: { message: 'workspace_id: must be >= 1' }
        }
      },
      {
        type: 'session.reasoning.started',
        payload: { event_id: 'pi-interleave-8', assistant_message_id: 'assistant-1', reasoning_id: 'assistant-1:reasoning:0' }
      },
      {
        type: 'session.reasoning.ended',
        payload: {
          event_id: 'pi-interleave-9',
          assistant_message_id: 'assistant-1',
          reasoning_id: 'assistant-1:reasoning:0',
          text: 'Need valid workspace_id before retrying.'
        }
      },
      { type: 'session.step.ended', payload: { event_id: 'pi-interleave-10', finish_reason: 'stop' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.started',
    'session.tool.input.started',
    'session.reasoning.ended',
    'session.tool.input.ended',
    'session.tool.called',
    'session.tool.failed',
    'session.reasoning.started',
    'session.reasoning.ended',
    'session.step.ended'
  ])
  assert.match(lines[3].reasoningText, /GPU collection/)
  assert.equal(lines[5].status, 'running')
  assert.equal(lines[6].status, 'failed')
  assert.match(lines[6].lineSummary, /workspace_id/)
  assert.match(lines[8].reasoningText, /valid workspace_id/)
})

test('runtime process lines keep Pi streamed answer after tools in SSE order', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-order-1', agent: 'kk' } },
      { type: 'session.reasoning.delta', payload: { event_id: 'pi-order-2', delta: '需要先读文件。' } },
      { type: 'session.tool.called', payload: { event_id: 'pi-order-3', call_id: 'call-read', tool: 'read_file', input: { path: 'README.md' } } },
      { type: 'session.tool.success', payload: { event_id: 'pi-order-4', call_id: 'call-read', tool: 'read_file', structured: { ok: true } } },
      { type: 'session.text.delta', payload: { event_id: 'pi-order-5', delta: '**结论**' } },
      { type: 'session.text.delta', payload: { event_id: 'pi-order-6', delta: '：已读取。' } },
      { type: 'session.text.ended', payload: { event_id: 'pi-order-7', text: '**结论**：已读取。' } },
      { type: 'session.step.ended', payload: { event_id: 'pi-order-8', finish_reason: 'stop' } }
    ],
    answer: '**结论**：已读取。'
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.delta',
    'session.tool.called',
    'session.tool.success',
    'session.text.delta',
    'session.text.ended',
    'session.step.ended'
  ])
  assert.equal(lines[1].reasoningText, '需要先读文件。')
  assert.equal(lines[2].lineTitle, 'read_file path=README.md')
  assert.equal(lines[4].answerText, '**结论**：已读取。')
  assert.equal(lines[5].answerText, '**结论**：已读取。')
})

test('runtime process lines do not duplicate Pi streamed reasoning and markdown answer from aggregate entry fields', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-dup-1', agent: 'kk' } },
      { type: 'session.reasoning.delta', payload: { event_id: 'pi-dup-2', delta: '先分析' } },
      { type: 'session.reasoning.delta', payload: { event_id: 'pi-dup-3', delta: '需求。' } },
      { type: 'session.reasoning.ended', payload: { event_id: 'pi-dup-4', text: '先分析需求。' } },
      { type: 'session.text.delta', payload: { event_id: 'pi-dup-5', delta: '# 标题' } },
      { type: 'session.text.delta', payload: { event_id: 'pi-dup-6', delta: '\n\n- **重点**' } },
      { type: 'session.text.ended', payload: { event_id: 'pi-dup-7', text: '# 标题\n\n- **重点**' } },
      { type: 'session.step.ended', payload: { event_id: 'pi-dup-8', finish_reason: 'stop' } }
    ],
    reasoning: '先分析需求。',
    answer: '# 标题\n\n- **重点**'
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.delta',
    'session.reasoning.ended',
    'session.text.delta',
    'session.text.ended',
    'session.step.ended'
  ])
  assert.equal(lines[1].reasoningText, '先分析需求。')
  assert.equal(lines[3].answerText, '# 标题\n\n- **重点**')
  const answerSection = lines[3].sections.find((section) => section.kind === 'answer')
  assert.ok(answerSection)
  assert.match(renderMarkdownToHtml(answerSection.text), /<h1>标题<\/h1>/)
  assert.match(renderMarkdownToHtml(answerSection.text), /<strong>重点<\/strong>/)
})

test('runtime process lines show Pi tool input streaming before execution and call rows in order', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-input-1' } },
      { type: 'session.tool.input.started', payload: { event_id: 'pi-input-2', call_id: 'call-read', tool_name: 'read_file' } },
      { type: 'session.tool.input.delta', payload: { event_id: 'pi-input-3', call_id: 'call-read', tool_name: 'read_file', delta: '{"path":"src/' } },
      { type: 'session.tool.input.delta', payload: { event_id: 'pi-input-4', call_id: 'call-read', tool_name: 'read_file', delta: 'main.ts"}' } },
      { type: 'session.tool.input.ended', payload: { event_id: 'pi-input-5', call_id: 'call-read', text: '{"path":"src/main.ts"}' } },
      { type: 'session.tool.called', payload: { event_id: 'pi-input-6', call_id: 'call-read', tool: 'read_file', input: { path: 'src/main.ts' } } },
      { type: 'session.tool.success', payload: { event_id: 'pi-input-7', call_id: 'call-read', tool: 'read_file', content: [{ type: 'text', text: 'ok' }], structured: { ok: true } } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.tool.input.started',
    'session.tool.input.delta',
    'session.tool.input.ended',
    'session.tool.called',
    'session.tool.success'
  ])
  assert.equal(lines[2].inputPreviewText, 'path=src/main.ts')
  assert.equal(lines[3].inputPreviewText, 'path=src/main.ts')
  assert.equal(lines[4].lineTitle, 'read_file path=src/main.ts')
  assert.equal(lines[5].lineSummary, 'ok=true')
})

test('runtime process lines append successful Pi tool results without rewriting the call row', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.tool.input.started', call_id: 'c1', tool_name: 'easydo_pipeline_list' },
      { type: 'session.tool.called', call_id: 'c1', tool: 'easydo_pipeline_list', input: { workspace_id: 1 } },
      { type: 'session.tool.success', call_id: 'c1', tool: 'easydo_pipeline_list', structured: { total: 0 } }
    ]
  })

  assert.equal(lines.length, 3)
  assert.equal(lines[0].lineKind, 'tool')
  assert.equal(lines[0].status, 'running')
  assert.equal(lines[1].lineKind, 'mcp')
  assert.equal(lines[1].event, 'session.tool.called')
  assert.equal(lines[1].status, 'running')
  assert.equal(lines[2].lineKind, 'result')
  assert.equal(lines[2].event, 'session.tool.success')
  assert.equal(lines[2].status, 'success')
})

test('runtime trace template renders tool input separately from the compact title', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')

  assert.match(traceSource, /ai-runtime-event-line__input/)
  assert.match(traceSource, /ai-runtime-event-line__reason/)
  assert.match(traceSource, /item\.reasonText/)
  assert.match(traceSource, /调用理由/)
  assert.match(traceSource, /item\.inputPreviewText/)
  assert.match(traceSource, /输入/)
})

test('runtime process lines surface Pi tool output paths as generated files', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.tool.called', payload: { event_id: 'file-1', call_id: 'call-write', tool: 'write_file', input: { path: 'src/app.ts' } } },
      {
        type: 'session.tool.success',
        payload: {
          event_id: 'file-2',
          call_id: 'call-write',
          tool: 'write_file',
          content: [{ type: 'text', text: 'wrote files' }],
          structured: { ok: true },
          output_paths: ['src/app.ts', 'src/app.test.ts']
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.lineKind), ['tool', 'result'])
  assert.equal(lines[0].lineTitle, 'write_file path=src/app.ts')
  assert.equal(lines[1].lineSummary, '生成文件 2 个：src/app.ts, src/app.test.ts')
  assert.deepEqual(lines[1].artifactRefs.map((item) => item.artifact_id), ['file:src/app.ts', 'file:src/app.test.ts'])
  assert.deepEqual(lines[1].artifactRefs.map((item) => item.artifact_type), ['file_output', 'file_output'])
  assert.deepEqual(lines[1].artifactRefs.map((item) => item.preview_json.path), ['src/app.ts', 'src/app.test.ts'])
})

test('runtime process lines render Pi compaction lifecycle as visible model steps', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.compaction.started', payload: { event_id: 'compact-1', reason: 'auto' } },
      { type: 'session.compaction.ended', payload: { event_id: 'compact-2', reason: 'auto', summary: 'Earlier context summarized.', recent: 'Current request remains active.' } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.lineKind), ['model', 'model'])
  assert.equal(lines[0].status, 'running')
  assert.match(lines[0].lineTitle, /上下文/)
  assert.equal(lines[1].status, 'success')
  assert.equal(lines[1].lineSummary, 'Earlier context summarized.')
})

test('runtime process lines render multi-agent orchestration task lifecycle', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      {
        type: 'orchestration.context.assembled',
        payload: { event_id: 'orch-1', orchestration_id: 'orch_1', task_count: 3, context_pack_digest: 'sha256:abc' }
      },
      {
        type: 'orchestration.task.dispatched',
        payload: { event_id: 'orch-2', task_id: 't-deploy', assigned_subagent_name: 'Deploy Agent', status: 'dispatched' }
      },
      {
        type: 'orchestration.task.completed',
        payload: { event_id: 'orch-3', task_id: 't-deploy', assigned_subagent_name: 'Deploy Agent', status: 'completed', result_summary: 'deploy ok' }
      },
      {
        type: 'orchestration.completed',
        payload: { event_id: 'orch-4', status: 'completed', completed_task_count: 3, failed_task_count: 0, final_summary: 'all done' }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.lineKind), ['subagent', 'subagent', 'subagent', 'subagent'])
  assert.equal(lines[0].status, 'info')
  assert.match(lines[0].lineTitle, /多 Agent|上下文/)
  assert.match(lines[0].lineSummary, /3 个任务/)
  assert.equal(lines[1].status, 'running')
  assert.match(lines[1].lineSummary, /t-deploy/)
  assert.equal(lines[2].status, 'success')
  assert.equal(lines[3].status, 'success')
  assert.match(lines[3].lineSummary, /完成 3/)
})

test('runtime process lines render provider-specific context budget and compaction token deltas', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      {
        type: 'context.budget.evaluated',
        payload: {
          event_id: 'budget-1',
          provider_id: 'ollama',
          model_key: 'qwen/qwen3',
          context_window_tokens: 8192,
          current_context_tokens: 9000,
          threshold_tokens: 6000,
          should_compact: true,
          reason: 'over_threshold'
        }
      },
      {
        type: 'context.compaction.started',
        payload: {
          event_id: 'compact-start-1',
          provider_id: 'ollama',
          model_key: 'qwen/qwen3',
          context_window_tokens: 8192,
          before_tokens: 9000,
          after_tokens: 2200,
          compacted_message_count: 12,
          reason: 'over_threshold'
        }
      },
      {
        type: 'context.compaction.completed',
        payload: {
          event_id: 'compact-end-1',
          provider_id: 'ollama',
          model_key: 'qwen/qwen3',
          context_window_tokens: 8192,
          before_tokens: 9000,
          after_tokens: 2200,
          compacted_message_count: 12,
          summary: 'Earlier context summarized for ollama/qwen3 window 8192.'
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.lineKind), ['model', 'model', 'model'])
  assert.equal(lines[0].status, 'info')
  assert.match(lines[0].lineTitle, /预算/)
  assert.match(lines[0].lineSummary, /ollama\/qwen\/qwen3/)
  assert.match(lines[0].lineSummary, /8192/)
  assert.equal(lines[1].status, 'running')
  assert.match(lines[1].lineSummary, /9000→2200/)
  assert.equal(lines[2].status, 'success')
  assert.match(lines[2].lineSummary, /9000→2200 tokens/)
})

test('runtime process lines render Pi agent and model switches as visible model steps', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.agent.switched', payload: { event_id: 'switch-1', message_id: 'msg-1', agent: 'reviewer-agent' } },
      { type: 'session.model.switched', payload: { event_id: 'switch-2', message_id: 'msg-1', model: { provider_id: 'openai', id: 'gpt-5' } } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.lineKind), ['model', 'model'])
  assert.equal(lines[0].status, 'success')
  assert.equal(lines[0].lineTitle, '已切换 Agent')
  assert.equal(lines[0].lineTarget, 'reviewer-agent')
  assert.equal(lines[0].lineSummary, '后续步骤由 reviewer-agent 处理')
  assert.equal(lines[1].status, 'success')
  assert.equal(lines[1].lineTitle, '已切换模型')
  assert.equal(lines[1].lineTarget, 'openai/gpt-5')
})

test('runtime process lines show Pi step failures without requiring a separate session error', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-fail-1' } },
      { type: 'session.reasoning.ended', payload: { event_id: 'pi-fail-2', text: '准备写文件。' } },
      {
        type: 'session.step.failed',
        payload: {
          event_id: 'pi-fail-3',
          error: { type: 'tool_error', message: 'write_file failed' },
          retryable: true
        }
      }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), ['session.step.started', 'session.reasoning.ended', 'session.step.failed'])
  assert.equal(lines[0].status, 'success')
  assert.equal(lines[0].lineTitle, '模型响应完成')
  assert.equal(lines[2].status, 'failed')
  assert.equal(lines[2].lineTitle, '步骤失败')
  assert.equal(lines[2].lineTarget, 'tool_error')
  assert.equal(lines[2].lineSummary, 'write_file failed（可重试）')
})

test('runtime process lines surface real upstream provider failures instead of generic internal text', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-upstream-1' } },
      {
        type: 'session.step.failed',
        payload: {
          event_id: 'pi-upstream-2',
          error: {
            type: 'provider_rate_limit',
            code: 'provider_rate_limit',
            message: 'Upstream error from Nvidia: ResourceExhausted: Worker local total request limit reached (274/32)',
            retryable: true,
            http_status: 429,
            source: 'provider'
          }
        }
      }
    ]
  })

  const failed = lines.find((item) => item.lineKind === 'result' || item.status === 'failed')
  assert.ok(failed)
  assert.match(String(failed.lineSummary || failed.resultSummary || ''), /ResourceExhausted|request limit reached/)
  assert.doesNotMatch(String(failed.lineSummary || failed.resultSummary || ''), /内部错误|internal error|temporarily unavailable/i)
})

test('runtime process lines render Pi cancellation as an explicit stopped step', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 'pi-cancel-1' } },
      { type: 'session.reasoning.ended', payload: { event_id: 'pi-cancel-2', text: '需要等待工具确认。' } },
      { type: 'permission.asked', payload: { event_id: 'pi-cancel-3', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', reason: 'Tool easydo_write requires approval' } },
      { type: 'permission.resolved', payload: { event_id: 'pi-cancel-4', request_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_write', result: 'rejected' } },
      { type: 'session.step.failed', payload: { event_id: 'pi-cancel-5', error: { type: 'cancelled', message: 'User stopped generation' } } },
      { type: 'session.error', payload: { event_id: 'pi-cancel-6', code: 'user_cancelled', message: 'User stopped generation', retryable: false } }
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.ended',
    'permission.resolved',
    'session.step.failed',
    'session.error'
  ])
  assert.equal(lines[2].status, 'rejected')
  assert.match(String(lines[2].lineTitle || ''), /已拒绝/)
  assert.equal(lines[3].status, 'failed')
  assert.equal(lines[3].lineTitle, '已停止生成')
  assert.equal(lines[3].lineSummary, 'User stopped generation')
})

test('agent chatbox derives pending Pi approvals from awaiting approval output', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_pi_1',
    status: 'streaming',
    output: {
      runtime_engine: 'pi',
      awaiting_approval: {
        approval_id: 'approval:call-1',
        call_id: 'call-1',
        tool_name: 'easydo_write',
        reason: 'Tool easydo_write requires approval',
        input: { value: 'x' }
      }
    }
  }])

  assert.equal(pending.length, 1)
  assert.equal(pending[0].action_kind, 'pi.tool_approval')
  assert.equal(pending[0].runtime_run_id, 'r_pi_1')
  assert.equal(pending[0].display_json.title, '需要确认 easydo_write')
  assert.equal(pending[0].input_json.arguments.value, 'x')
})

test('agent chatbox derives live Pi approvals before runtime_run_id is attached to the draft entry', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    status: 'streaming',
    output: {
      runtime_events: [
        {
          type: 'permission.asked',
          request_id: 'approval:call-live-1',
          approval_id: 'approval:call-live-1',
          call_id: 'call-live-1',
          tool_name: 'easydo_write',
          reason: 'Tool easydo_write requires approval',
          input: { value: 'live' }
        }
      ],
      awaiting_approval: {
        approval_id: 'approval:call-live-1',
        call_id: 'call-live-1',
        tool_name: 'easydo_write',
        reason: 'Tool easydo_write requires approval',
        input: { value: 'live' }
      }
    }
  }])

  assert.equal(pending.length, 1)
  assert.equal(pending[0].id, 'approval:call-live-1')
  assert.equal(pending[0].status, 'awaiting_decision')
  assert.equal(pending[0].pi_approval, true)
  assert.equal(pending[0].input_json.arguments.value, 'live')
})

test('agent chatbox store attaches runtime_run_id from streamed run.started events', async () => {
  const [storeSource, controllerSource, projectionSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/runtimeSessionController.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/runtimeSessionStreamProjection.js'), 'utf8')
  ])
  assert.match(storeSource, /sessionController\.handleRuntimeEventPayload/)
  assert.match(controllerSource, /applyRuntimeStreamEventToAssistantEntry/)
  assert.match(projectionSource, /runtimeEventName === 'run\.started'/)
  assert.match(projectionSource, /entry\.runtime_run_id\s*=/)
  assert.match(projectionSource, /data\.run\?\.runtime_run_id/)
})

test('runtime trace synthesizes approval buttons for waiting action rows without pendingActions', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  const helperStart = traceSource.indexOf('function approvalActionForItem')
  const helperEnd = traceSource.indexOf('\nfunction ', helperStart + 1)
  const helperSource = traceSource.slice(helperStart, helperEnd > helperStart ? helperEnd : undefined)

  assert.match(helperSource, /item\.status === 'waiting'/)
  assert.match(helperSource, /awaiting_decision/)
  assert.match(helperSource, /pi_approval:/)
  assert.doesNotMatch(helperSource, /if \(!id \|\| item\.status === 'waiting'\) return null/)
})

test('agent chatbox suppresses final-answer when timeline already has process events', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')
  assert.match(source, /class="final-answer"/)
  assert.match(source, /showFinalAnswer\(|isCompletedAssistantEntry\(|entry\.status === 'completed'/)
  assert.match(source, /function showFinalAnswer\(entry\)[\s\S]*?chatboxRuntimeEvents\(entry, chatboxStore\.entries\)\.length \|\| chatboxReasoningText\(entry\)/s)
  assert.match(source, /:limit="runtimeTraceLimit\(/)
  assert.match(source, /if \(entry\?\.status === 'streaming'\) return 100/)
  assert.match(source, /return 500/)
})

test('agent chatbox keeps repeated Pi approval requests separate when provider call ids differ', () => {
  const entries = [
    {
      role: 'assistant',
      runtime_run_id: 'r_pi_multi_approval',
      status: 'streaming',
      output: {
        awaiting_approval: {
          approval_id: 'approval:call-1',
          call_id: 'call-1',
          tool_name: 'easydo_write',
          reason: 'Tool easydo_write requires approval',
          input: { value: 'same' }
        }
      }
    },
    {
      role: 'assistant',
      runtime_run_id: 'r_pi_multi_approval',
      status: 'streaming',
      output: {
        awaiting_approval: {
          approval_id: 'approval:call-2',
          call_id: 'call-2',
          tool_name: 'easydo_write',
          reason: 'Tool easydo_write requires approval',
          input: { value: 'same' }
        }
      }
    }
  ]

  const pending = derivePendingActions(entries)

  assert.deepEqual(pending.map((action) => action.id).sort(), ['approval:call-1', 'approval:call-2'])
})

test('agent chatbox derives multiple Pi approvals from one awaiting approvals payload', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_pi_multi_payload',
    status: 'streaming',
    output: {
      awaiting_approvals: [
        {
          approval_id: 'approval:call-1',
          call_id: 'call-1',
          tool_name: 'easydo_pipeline_run_list',
          reason: 'Tool easydo_pipeline_run_list requires approval',
          input: { pipeline_id: 14 }
        },
        {
          approval_id: 'approval:call-2',
          call_id: 'call-2',
          tool_name: 'easydo_pipeline_run_list',
          reason: 'Tool easydo_pipeline_run_list requires approval',
          input: { pipeline_id: 13 }
        }
      ]
    }
  }])

  assert.deepEqual(pending.map((action) => ({
    id: action.id,
    callId: action.input_json.provider_tool_call_id,
    pipelineId: action.input_json.arguments.pipeline_id
  })), [
    { id: 'approval:call-1', callId: 'call-1', pipelineId: 14 },
    { id: 'approval:call-2', callId: 'call-2', pipelineId: 13 }
  ])
})

test('agent chatbox derives unresolved Pi approvals from runtime permission events after refresh', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_pi_replay_approval',
    status: 'completed',
    output: {
      runtime_events: [
        { type: 'permission.asked', payload: { request_id: 'approval:call-1', approval_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { workspace_id: 1, pipeline_id: 14 } } },
        { type: 'permission.asked', payload: { request_id: 'approval:call-2', approval_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { workspace_id: 1, pipeline_id: 13 } } },
        { type: 'permission.resolved', payload: { request_id: 'approval:call-2', approval_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list', result: 'approved', decision: 'approve_once' } }
      ]
    }
  }])

  assert.deepEqual(pending.map((action) => ({
    id: action.id,
    callId: action.input_json.provider_tool_call_id,
    pipelineId: action.input_json.arguments.pipeline_id
  })), [{ id: 'approval:call-1', callId: 'call-1', pipelineId: 14 }])
})

test('agent chatbox derives Pi approvals when permission events only expose provider call ids', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_pi_provider_call_only',
    status: 'streaming',
    output: {
      runtime_events: [
        {
          type: 'permission.asked',
          payload: {
            request_id: 'approval:provider-call-1',
            approval_id: 'approval:provider-call-1',
            provider_tool_call_id: 'provider-call-1',
            tool_name: 'easydo_pipeline_run_list',
            reason: 'Tool easydo_pipeline_run_list requires approval',
            input: { workspace_id: 1, pipeline_id: 14 }
          }
        }
      ]
    }
  }])

  assert.deepEqual(pending.map((action) => ({
    id: action.id,
    callId: action.input_json.provider_tool_call_id,
    pipelineId: action.input_json.arguments.pipeline_id
  })), [{ id: 'approval:provider-call-1', callId: 'provider-call-1', pipelineId: 14 }])
})

test('agent chatbox derives child Pi approvals from parent subagent blocked events', () => {
  const pending = derivePendingActions([{
    id: 10,
    role: 'assistant',
    runtime_run_id: 'r_parent',
    output: {
      runtime_events: [{
        type: 'subagent.blocked_approval',
        payload: {
          child_runtime_run_id: 'r_child',
          awaiting_approval: {
            approval_id: 'approval:child-call-1',
            call_id: 'child-call-1',
            tool_name: 'child_write',
            reason: 'Child write requires approval',
            input: { target_id: 'child-resource' }
          }
        }
      }]
    }
  }])

  assert.equal(pending.length, 1)
  assert.equal(pending[0].runtime_run_id, 'r_child')
  assert.equal(pending[0].pi_approval, true)
  assert.equal(pending[0].capability_id, 'child_write')
})

test('runtime trace renders child Pi approval controls inside the blocked subagent card', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  const subagentStart = traceSource.indexOf('<section v-if="item.lineKind === \'subagent\'"')
  const subagentEnd = traceSource.indexOf('</section>', subagentStart)
  const subagentSource = traceSource.slice(subagentStart, subagentEnd)
  const idsStart = traceSource.indexOf('function itemActionIds')
  const idsEnd = traceSource.indexOf('function compactIds', idsStart)
  const idsSource = traceSource.slice(idsStart, idsEnd)

  assert.ok(subagentStart > 0)
  assert.ok(subagentEnd > subagentStart)
  assert.match(subagentSource, /approvalActionForItem\(item\)/)
  assert.match(subagentSource, /slot name="approval-actions"/)
  assert.match(idsSource, /awaiting_approval/)
  assert.match(idsSource, /approval_id/)
  assert.match(idsSource, /call_id/)
})

test('agent chatbox clears a child Pi approval after the child reaches a terminal state', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_parent',
    output: {
      runtime_events: [{
        type: 'subagent.blocked_approval',
        payload: {
          child_runtime_run_id: 'r_child',
          child_run_link_id: 'cl_child',
          awaiting_approval: {
            approval_id: 'approval:child-call-1',
            call_id: 'child-call-1',
            tool_name: 'child_write',
            reason: 'Child write requires approval'
          }
        }
      }, {
        type: 'subagent.completed',
        payload: {
          child_runtime_run_id: 'r_child',
          child_run_link_id: 'cl_child',
          status: 'completed'
        }
      }]
    }
  }])

  assert.deepEqual(pending, [])
})

test('runtime trace matches Pi approval actions by provider call id as well as approval id', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  const helperStart = traceSource.indexOf('function pendingActionForItem')
  const helperEnd = traceSource.indexOf('function approvalActionForItem', helperStart)
  const helperSource = traceSource.slice(helperStart, helperEnd)
  const approvalStart = helperEnd
  const approvalEnd = traceSource.indexOf('function itemActionId', approvalStart)
  const approvalSource = traceSource.slice(approvalStart, approvalEnd)

  assert.ok(helperStart > 0)
  assert.ok(helperEnd > helperStart)
  assert.match(helperSource, /provider_tool_call_id/)
  assert.match(helperSource, /approval_request/)
  assert.match(helperSource, /call_id/)
  assert.match(approvalSource, /pendingActionForItem\(item\)/)
  assert.match(approvalSource, /awaiting_decision|status === 'waiting'/)
})

test('agent chatbox does not keep stale Pi approvals after the agent loop already continued', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_pi_bypassed_approval',
    status: 'completed',
    output: {
      runtime_events: [
        { type: 'session.step.started', payload: { event_seq: 1, event_id: 'step-1' } },
        { type: 'permission.asked', payload: { event_seq: 10, event_id: 'ask-1', request_id: 'approval:call-1', approval_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { workspace_id: 1, pipeline_id: 1 } } },
        { type: 'permission.asked', payload: { event_seq: 11, event_id: 'ask-2', request_id: 'approval:call-2', approval_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { workspace_id: 1, pipeline_id: 13 } } },
        { type: 'session.tool.failed', payload: { event_seq: 12, event_id: 'failed-1', call_id: 'call-1', tool_name: 'easydo_pipeline_run_list', error: { message: 'Tool easydo_pipeline_run_list requires approval' } } },
        { type: 'session.tool.failed', payload: { event_seq: 13, event_id: 'failed-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list', error: { message: 'Tool easydo_pipeline_run_list requires approval' } } },
        { type: 'permission.resolved', payload: { event_seq: 20, event_id: 'resolve-2', request_id: 'approval:call-2', approval_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list', result: 'approved', decision: 'approve_once' } },
        { type: 'session.tool.called', payload: { event_seq: 21, event_id: 'called-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list' } },
        { type: 'session.tool.success', payload: { event_seq: 22, event_id: 'success-2', call_id: 'call-2', tool_name: 'easydo_pipeline_run_list' } },
        { type: 'session.step.started', payload: { event_seq: 23, event_id: 'step-2' } }
      ]
    }
  }])

  assert.deepEqual(pending, [])
})

test('agent chatbox keeps sequential Pi approvals pending when pause failures only carry tool descriptions', () => {
  const pending = derivePendingActions([{
    role: 'assistant',
    runtime_run_id: 'r_pi_seq_description_only',
    status: 'streaming',
    output: {
      runtime_events: [
        { type: 'session.step.started', payload: { event_seq: 1, event_id: 'step-1' } },
        { type: 'permission.asked', payload: { event_seq: 10, event_id: 'ask-1', request_id: 'approval:call-1', approval_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_workspace_list', reason: 'List workspaces the actor can access', input: {} } },
        { type: 'session.tool.failed', payload: { event_seq: 11, event_id: 'failed-1', call_id: 'call-1', tool_name: 'easydo_workspace_list', error: { type: 'unknown', message: 'List workspaces the actor can access' } } },
        { type: 'permission.resolved', payload: { event_seq: 12, event_id: 'resolve-1', request_id: 'approval:call-1', approval_id: 'approval:call-1', call_id: 'call-1', tool_name: 'easydo_workspace_list', result: 'approved', decision: 'approve_once' } },
        { type: 'session.tool.called', payload: { event_seq: 13, event_id: 'called-1', call_id: 'call-1', tool_name: 'easydo_workspace_list' } },
        { type: 'session.tool.success', payload: { event_seq: 14, event_id: 'success-1', call_id: 'call-1', tool_name: 'easydo_workspace_list' } },
        { type: 'session.step.started', payload: { event_seq: 20, event_id: 'step-2' } },
        { type: 'permission.asked', payload: { event_seq: 21, event_id: 'ask-2', request_id: 'approval:call-2', approval_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_pipeline_list', reason: 'List pipelines in one workspace', input: { workspace_id: 1, query: 'e1' } } },
        { type: 'session.tool.failed', payload: { event_seq: 22, event_id: 'failed-2', call_id: 'call-2', tool_name: 'easydo_pipeline_list', error: { type: 'unknown', message: 'List pipelines in one workspace' } } }
      ]
    }
  }])

  assert.equal(pending.length, 1)
  assert.equal(pending[0].status, 'awaiting_decision')
  assert.equal(pending[0].input_json?.provider_tool_call_id, 'call-2')
  assert.equal(pending[0].capability_id, 'easydo_pipeline_list')
})

test('runtime process lines keep waiting approval when same-call failure only carries tool description', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      { type: 'session.step.started', payload: { event_id: 's1', agent: 'k1' } },
      { type: 'permission.asked', payload: { event_id: 'pa1', request_id: 'approval:call-2', approval_id: 'approval:call-2', call_id: 'call-2', tool_name: 'easydo_pipeline_list', reason: 'List pipelines in one workspace', message: 'List pipelines in one workspace', input: { workspace_id: 1, query: 'e1' } } },
      { type: 'session.tool.failed', payload: { event_id: 'tf1', call_id: 'call-2', tool: 'easydo_pipeline_list', tool_name: 'easydo_pipeline_list', error: { type: 'unknown', message: 'List pipelines in one workspace' } } }
    ]
  })

  const actionLine = lines.find((item) => item.lineKind === 'action')
  assert.ok(actionLine, 'approval action row should remain')
  assert.equal(actionLine.status, 'waiting')
  assert.equal(actionLine.event, 'permission.asked')
  assert.equal(actionLine.lineTitle, '需要确认 easydo_pipeline_list')
  assert.equal(actionLine.lineSummary, '')
  // Same-call approval-pause tool.failed is suppressed; one waiting panel is enough.
  assert.equal(lines.some((item) => item.event === 'session.tool.failed'), false)
})

test('agent chatbox computes agent run footer metrics from runtime events', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_1',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
        { type: 'session.tool.called', call_id: 'call-1', tool_name: 'easydo_workspace_list', timestamp: '2026-07-06T00:00:01.000Z' },
        { type: 'session.tool.success', call_id: 'call-1', tool_name: 'easydo_workspace_list', timestamp: '2026-07-06T00:00:02.000Z' },
        { type: 'skill.used', name: 'grill-me', timestamp: '2026-07-06T00:00:03.000Z' },
        { type: 'session.step.ended', tokens: { input: 10, output: 7, reasoning: 3, cache: { read: 2, write: 1 } }, timestamp: '2026-07-06T00:00:04.200Z' }
      ]
    }
  }

  const metrics = agentRunMetricsForEntry(entry, [entry])
  const byKey = Object.fromEntries(metrics.map((item) => [item.key, item.value]))

  assert.equal(byKey.tokens, '输入 10 / 输出 7 / 思考 3 / 缓存 2')
  assert.equal(byKey.llm_requests, '1次')
  assert.equal(byKey.tool_calls, '1次')
  assert.equal(byKey.skills, '1个')
  assert.equal(byKey.duration, '4.2s')
})

test('agent chatbox metrics ignore skill catalogs and only count actual tool calls', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_catalog',
    content: 'done',
    output: {
      pi_resources: {
        skills: [{ name: 'teach' }, { name: 'grilling' }, { name: 'loop-me' }]
      },
      available_skills: [{ name: 'teach' }],
      runtime_events: [
        { type: 'skill.available', name: 'teach' },
        { type: 'skill.available', name: 'grilling' },
        { type: 'session.tool.called', call_id: 'call-1', tool_name: 'easydo_workspace_list' },
        { type: 'session.tool.success', call_id: 'call-1', tool_name: 'easydo_workspace_list' },
        { type: 'session.tool.failed', call_id: 'call-2', tool_name: 'easydo_write', error: { message: 'requires approval' } },
        { type: 'model.tool_call_detected', call_id: 'call-2', tool_name: 'easydo_write' },
        { type: 'session.tool.called', call_id: 'call-3', tool_name: 'easydo_resource_gpu_usage' }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  assert.equal(byKey.tool_calls, '2次')
  assert.equal(byKey.skills, '0个')
})

test('agent chatbox metrics exclude approval waiting gaps from duration', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_wait',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
        { type: 'permission.asked', call_id: 'call-1', timestamp: '2026-07-06T00:00:02.000Z' },
        { type: 'permission.resolved', call_id: 'call-1', timestamp: '2026-07-06T00:10:02.000Z' },
        { type: 'session.step.ended', timestamp: '2026-07-06T00:10:05.000Z' }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  assert.equal(byKey.duration, '5.0s')
})

test('agent chatbox metrics close open steps on failure terminals and mark degraded quality', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_failed_step',
    content: 'partial answer',
    output: {
      runtime_events: [
        { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
        { type: 'session.reasoning.delta', timestamp: '2026-07-06T00:00:02.000Z', delta: 'thinking' },
        { type: 'session.step.failed', timestamp: '2026-07-06T00:00:05.000Z', error: { message: 'provider failed' } }
      ]
    }
  }

  const duration = agentRunMetricsForEntry(entry, [entry]).find((item) => item.key === 'duration')

  assert.equal(duration.value, '5.0s')
  assert.equal(duration.quality, 'degraded')
  assert.equal(duration.algorithm_version, 'agent_active_duration_v2')
})

test('agent chatbox only replays run events for active assistant entries', async () => {
  const [storeSource, stateSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentChatboxState.js'), 'utf8')
  ])
  assert.match(stateSource, /export function entryNeedsRuntimeEventReplay/)
  const replayStart = storeSource.indexOf('async function replayEventsIntoEntries')
  const replayEnd = storeSource.indexOf('function markOptimisticFailed', replayStart)
  const replaySource = storeSource.slice(replayStart, replayEnd)
  assert.match(replaySource, /\.filter\(entryNeedsRuntimeEventReplay\)/)
  assert.match(replaySource, /listAgentChatboxRunEvents\(runtimeRunId\)/)
})

test('agent chatbox counts distinct model request ids even when phase and round match', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_request_ids',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'model.call_started', payload: { request_id: 'req-1', phase: 'initial', round: 1 } },
        { type: 'model.call_completed', payload: { request_id: 'req-1', phase: 'initial', round: 1, usage: { input: 4, output: 2 } } },
        { type: 'model.call_started', payload: { request_id: 'req-2', phase: 'initial', round: 1 } },
        { type: 'model.call_completed', payload: { request_id: 'req-2', phase: 'initial', round: 1, usage: { input: 5, output: 3 } } }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  assert.equal(byKey.llm_requests, '2次')
  assert.equal(byKey.tokens, '输入 9 / 输出 5')
})

test('agent chatbox does not double count llm requests or token usage across step and model events', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_mixed',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
        { type: 'model.call_started', payload: { phase: 'initial', round: 1 }, timestamp: '2026-07-06T00:00:00.100Z' },
        { type: 'model.call_completed', payload: { phase: 'initial', round: 1, usage: { input: 10, output: 5 } }, timestamp: '2026-07-06T00:00:01.000Z' },
        { type: 'model.continuation_started', payload: { phase: 'action_continuation', round: 2 }, timestamp: '2026-07-06T00:00:02.000Z' },
        { type: 'model.call_started', payload: { phase: 'action_continuation', round: 2 }, timestamp: '2026-07-06T00:00:02.100Z' },
        { type: 'model.call_completed', payload: { phase: 'action_continuation', round: 2, usage: { input: 3, output: 2 } }, timestamp: '2026-07-06T00:00:03.000Z' },
        { type: 'model.continuation_completed', payload: { phase: 'action_continuation', round: 2, usage: { input: 3, output: 2 } }, timestamp: '2026-07-06T00:00:03.100Z' },
        { type: 'session.step.ended', tokens: { input: 13, output: 7 }, timestamp: '2026-07-06T00:00:04.000Z' }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  assert.equal(byKey.llm_requests, '2次')
  assert.equal(byKey.tokens, '输入 13 / 输出 7')
})

test('agent chatbox shows unknown token usage as a dash instead of zero', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_unknown',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'session.step.started' },
        { type: 'session.step.ended', tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } } }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  assert.equal(byKey.tokens, '-')
})

test('agent chatbox reads Pi camelCase token totals in runtime metrics', () => {
  const entry = {
    role: 'assistant',
    runtime_run_id: 'r_pi_metrics_camel',
    content: 'done',
    output: {
      runtime_events: [
        { type: 'session.step.started' },
        { type: 'session.step.ended', tokens: { totalTokens: 42, cacheRead: 4, cacheWrite: 2 } }
      ]
    }
  }

  const byKey = Object.fromEntries(agentRunMetricsForEntry(entry, [entry]).map((item) => [item.key, item.value]))

  assert.equal(byKey.tokens, '42')
})

test('agent chatbox removes resolved Pi approvals from pending actions', () => {
  const entries = [{
    role: 'assistant',
    runtime_run_id: 'r_pi_resolved_1',
    status: 'completed',
    output: {
      awaiting_approval: {
        approval_id: 'approval:call-resolved',
        call_id: 'call-resolved',
        tool_name: 'easydo_write',
        reason: 'Tool easydo_write requires approval',
        input: { value: 'x' }
      },
      runtime_events: [
        { type: 'permission.asked', payload: { request_id: 'approval:call-resolved', call_id: 'call-resolved', tool_name: 'easydo_write' } },
        { type: 'permission.resolved', payload: { request_id: 'approval:call-resolved', call_id: 'call-resolved', tool_name: 'easydo_write', result: 'approved', decision: 'approve_once' } }
      ]
    }
  }]

  assert.deepEqual(derivePendingActions(entries), [])
})

test('agent chatbox approval button helpers disable old approvals and highlight the selected result', () => {
  const approvedAction = {
    id: 'a_approved',
    action_id: 'a_approved',
    status: 'approved',
    decision: 'approve_session'
  }
  const rejectedAction = {
    id: 'a_rejected',
    action_id: 'a_rejected',
    status: 'rejected',
    decision: 'reject'
  }

  assert.equal(isActionAwaitingDecision(approvedAction), false)
  assert.equal(approvalDecisionForAction(approvedAction), 'approve_session')
  assert.equal(isApprovalDecisionButtonSelected(approvedAction, 'approve_session'), true)
  assert.equal(isApprovalDecisionButtonSelected(approvedAction, 'approve_once'), false)
  assert.equal(isApprovalDecisionButtonSelected(rejectedAction, 'reject'), true)
})

test('agent chatbox submits Pi approvals before marking legacy action decisions pending', async () => {
  const [storeSource, pageSource, sharedSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentConversationShared.js'), 'utf8')
  ])
  const continueActionStart = storeSource.indexOf('async function continueAction')
  const piBranchIndex = storeSource.indexOf('if (isPiApprovalAction(agentAction))', continueActionStart)
  const legacyPendingIndex = storeSource.indexOf('setActionDecisionPending(id, runtimeDecision, true)', continueActionStart)

  assert.ok(continueActionStart > 0)
  assert.ok(piBranchIndex > continueActionStart)
  assert.ok(legacyPendingIndex > continueActionStart)
  assert.ok(piBranchIndex < legacyPendingIndex)
  assert.match(storeSource, /resolveAgentActionRef\(/)
  assert.match(sharedSource, /export function resolveAgentActionRef/)
  assert.match(sharedSource, /export function isPiApprovalAction/)
  assert.match(pageSource, /chatboxStore\.approveAction\(agentAction\)/)
  assert.match(pageSource, /chatboxStore\.approveActionForSession\(agentAction\)/)
  assert.match(pageSource, /chatboxStore\.rejectAction\(agentAction\)/)
  assert.doesNotMatch(pageSource, /chatboxStore\.approveAction\(agentAction\.id\)/)
})

test('agent chatbox streams Pi approval continuations into the existing runtime entry', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')
  const importIndex = storeSource.indexOf('streamAgentChatboxPiApproval')
  const continuePiStart = storeSource.indexOf('async function continuePiApproval')
  const streamIndex = storeSource.indexOf('await streamAgentChatboxPiApproval(runtimeRunId, decision', continuePiStart)
  const handlerIndex = storeSource.indexOf('handleStreamEvent(assistantDraftId, event, data, finalRef)', streamIndex)
  const existingEntryIndex = storeSource.indexOf('findAssistantEntryForRuntimeRun(entries.value, runtimeRunId)', continuePiStart)

  assert.ok(importIndex > 0)
  assert.ok(continuePiStart > 0)
  assert.ok(existingEntryIndex > continuePiStart)
  assert.ok(streamIndex > continuePiStart)
  assert.ok(handlerIndex > streamIndex)
  assert.match(storeSource, /const assistantDraftId = existingAssistantEntry\?\.id \|\| createdAssistantEntry\.id/)
  assert.doesNotMatch(storeSource.slice(continuePiStart, streamIndex), /decideAgentChatboxPiApproval/)
})

test('agent chatbox sends the selected Pi approval id with approval decisions', async () => {
  const [chatboxStoreSource, pageAssistantStoreSource, sharedSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentConversationShared.js'), 'utf8')
  ])

  assert.match(sharedSource, /function piApprovalRequestPayload\(agentAction = \{\}\)/)
  assert.match(chatboxStoreSource, /approval:\s*piApprovalRequestPayload\(agentAction\)/)
  assert.match(pageAssistantStoreSource, /approval:\s*piApprovalRequestPayload\(agentAction\)/)
})

test('agent chatbox runtime event gates include all process events needed for progressive timeline', async () => {
  const [storeSource, runtimeEventSource, apiSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/aiRuntimeEvents.js'), 'utf8'),
    readFile(join(currentDir, '../../api/agentChatbox.js'), 'utf8')
  ])

  for (const eventName of [
    'run.started',
    'run.completed',
    'run.failed',
    'run.cancelled',
    'run.timeout',
    'run.awaiting_decision',
    'run.interrupted',
    'run.step_announced',
    'capability.snapshot',
    'mcp.tools.available',
    'mcp.tools_discovery_failed',
    'model.request_prepared',
    'model.answer_synthesized',
    'model.tool_result_submitted',
    'model_provider.failed',
    'output_schema.available',
    'output_schema.validated',
    'output_schema.invalid',
    'approval.requested',
    'subagent.started',
    'subagent.progress',
    'subagent.blocked_approval',
    'subagent.failed',
    'subagent.cancelled'
  ]) {
    assert.match(runtimeEventSource, new RegExp(eventName.replaceAll('.', '\\.')), `${eventName} must be retained in runtime events`)
    assert.match(apiSource, new RegExp(eventName.replaceAll('.', '\\.')), `${eventName} must yield browser paint when streamed`)
  }
  assert.match(storeSource, /isProcessRuntimeEvent\(event\)/)
})

test('runtime trace template renders thought reasoning and answer sections', async () => {
  const traceSource = await readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')

  assert.match(traceSource, /item\.sections/)
  assert.match(traceSource, /section\.kind/)
  assert.match(traceSource, /section\.text/)
  assert.match(traceSource, /renderThoughtSectionMarkdown\(section\)/)
  assert.match(traceSource, /isMarkdownThoughtSection\(section\)/)
  assert.match(traceSource, /ai-runtime-event-line__section-markdown/)
  assert.match(traceSource, /renderMarkdownToHtml/)
  assert.doesNotMatch(traceSource, /<p v-if="section\.kind === 'answer'"/)
  assert.match(renderMarkdownToHtml('**目标**\n\n- 触发方式'), /<strong>目标<\/strong>/)
  assert.match(renderMarkdownToHtml('| 阶段 | 内容 |\n| --- | --- |\n| reasoning | **分析** |'), /<td><strong>分析<\/strong><\/td>/)
})

test('agent chatbox store keeps streaming reasoning deltas in process event order', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')
  const runtimeEventSource = await readFile(join(currentDir, '../../stores/aiRuntimeEvents.js'), 'utf8')

  assert.match(runtimeEventSource, /isProcessRuntimeEvent/)
  assert.match(storeSource, /isProcessRuntimeEvent\(event\)/)
  assert.match(storeSource, /appendRuntimeEvent\(entries,\s*assistantDraftId,\s*event,\s*data\)/)
  assert.match(storeSource, /entry\.output\.runtime_events\s*=\s*mergeRuntimeEvents\(events,\s*\[\s*normalizeRuntimeEvent\(event,\s*data\)\s*\]\s*\)/s)
  assert.match(storeSource, /mergeAssistantRuntimeEvents/)
  assert.match(runtimeEventSource, /model\.call_started/)
  assert.match(runtimeEventSource, /approval\.requested/)
})

test('agent chatbox trace uses incremental runtime events for visibility and rendering', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /v-if="chatboxRuntimeEvents\(entry, chatboxStore\.entries\)\.length \|\| chatboxReasoningText\(entry\)"/)
  assert.match(source, /:events="chatboxRuntimeEvents\(entry, chatboxStore\.entries\)"/)
  assert.match(source, /displayRuntimeEventsForEntry/)
  assert.doesNotMatch(source, /v-if="chatboxRuntimeEvents\(entry\)\.length/)
})

test('agent chatbox action card renders typed action payloads only', async () => {
  const [source, actionDisplaySource] = await Promise.all([
    readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/actionDisplay.js'), 'utf8')
  ])
  const combinedSource = `${source}\n${actionDisplaySource}`

  assert.match(source, /actionTitle/)
  assert.match(source, /actionSummary/)
  assert.match(source, /actionMeta/)
  assert.match(source, /actionInputPreview/)
  assert.match(combinedSource, /display_json/)
  assert.match(combinedSource, /input_json/)
  assert.match(combinedSource, /target_json/)
  assert.match(combinedSource, /policy_json/)
  assert.doesNotMatch(source, /agentAction\.tool_name/)
  assert.doesNotMatch(source, /agentAction\.arguments/)
  assert.doesNotMatch(source, /agentAction\.risk_summary/)
  assert.doesNotMatch(source, /agentAction\.operation_type/)
  assert.doesNotMatch(source, /agentAction\.target_type/)
  assert.doesNotMatch(source, /agentAction\.target_id/)
})

test('agent chatbox page displays profile context tags as metadata', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.match(source, /currentContextTags/)
  assert.match(source, /profileContextTags/)
  assert.match(source, /context_tags/)
  assert.match(source, /class="chatbox-context-tags"/)
  assert.match(source, /v-for="tag in currentContextTags"/)
})

test('agent chatbox treats missing profile and session as empty context tags', async () => {
  const source = await readFile(join(currentDir, 'AgentChatbox.vue'), 'utf8')

  assert.doesNotMatch(source, /function profileContextTags\(profile\s*=\s*\{\}\)/)
  assert.match(source, /const sourceProfile = profile \|\| \{\}/)
  assert.match(source, /normalizeStringArray\(sourceProfile\.context_tags\)/)
})

test('agent chatbox store restores url sessions and consumes l5 stream events', async () => {
  const [storeSource, runtimeEventSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/aiRuntimeEvents.js'), 'utf8')
  ])

  assert.match(storeSource, /defineStore\('agentChatbox'/)
  assert.match(storeSource, /getAgentChatboxSession/)
  assert.match(storeSource, /listAgentChatboxEntries/)
  assert.match(storeSource, /listAgentChatboxSessions/)
  assert.match(storeSource, /sendAgentChatboxMessageStream/)
  assert.match(storeSource, /continueAgentChatboxSessionStream/)
  assert.match(storeSource, /client_entry_id:\s*clientEntryId/)
  assert.match(storeSource, /cancelAgentChatboxSession/)
  assert.match(storeSource, /streamAgentChatboxRunEvents/)
  assert.match(storeSource, /connectionState/)
  assert.match(storeSource, /lastEventIdByRun/)
  assert.match(storeSource, /reconnectAttempt/)
  assert.match(storeSource, /heartbeat/)
  assert.match(storeSource, /runtime_run_id:\s*runtimeRunId/)
  assert.match(storeSource, /reason:\s*'user_stopped_generation'/)
  assert.doesNotMatch(storeSource, /cancelAgentChatboxSession\([^)]*\)\.catch\(\(\)\s*=>\s*null\)/)
  assert.match(storeSource, /reasoning_delta/)
  assert.match(storeSource, /answer_delta/)
  assert.match(storeSource, /action\.decision_required/)
  assert.match(runtimeEventSource, /action\.execution_started/)
  assert.match(runtimeEventSource, /action\.execution_succeeded/)
  assert.match(runtimeEventSource, /display_json/)
  assert.match(storeSource, /isPersistentRuntimeEvent/)
  assert.match(storeSource, /pendingActionDecisionKeys/)
  assert.match(storeSource, /uniquePendingActions/)
  assert.match(storeSource, /actionSemanticKey/)
  assert.match(runtimeEventSource, /tool\.executed/)
  assert.match(storeSource, /capability\.snapshot/)
  assert.match(runtimeEventSource, /subagent\.completed/)
  assert.match(storeSource, /streamAgentChatboxActionDecision/)
  assert.doesNotMatch(storeSource, /include_current_page/)
})

test('agent chatbox uses readable bounded client ids without time or random suffixes', async () => {
  const [storeSource, idSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/aiRuntimeClientIds.js'), 'utf8')
  ])

  assert.match(storeSource, /createRuntimeClientIdFactory/)
  assert.match(storeSource, /nextClientEntryId/)
  assert.match(idSource, /readableSegment/)
  assert.match(idSource, /MAX_RUNTIME_CLIENT_ID_LENGTH/)
  assert.doesNotMatch(storeSource, /Date\.now\(\)/)
  assert.doesNotMatch(storeSource, /Math\.random/)
})

test('agent chatbox store routes new messages through the Pi runtime harness by default', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')

  assert.match(storeSource, /runtime_engine:\s*'pi'/)
  assert.match(storeSource, /sendAgentChatboxMessageStream\(session\.value\.id,\s*\{[^}]*runtime_engine:\s*'pi'[^}]*\}/s)
})

test('agent chatbox store forwards retry attachments into stream and queue payloads', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8')

  assert.match(storeSource, /const attachments = normalizeSendAttachments\(options\.attachments\)/)
  assert.match(storeSource, /enqueueQueueMessage\(content,\s*mode,\s*\{\s*attachments\s*\}\)/)
  assert.match(storeSource, /queueController\.enqueue\(content,\s*mode,\s*options\)/)
  assert.match(storeSource, /\.\.\.\(attachments\.length > 0 \? \{ attachments \} : \{\}\)/)
  assert.match(storeSource, /createOptimisticUserEntry\(content,\s*clientEntryId,\s*attachments\)/)
})

test('page assistant store routes new messages through the Pi runtime harness by default', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')

  assert.match(storeSource, /runtime_engine:\s*'pi'/)
})

test('agent chatbox replay does not revive actions that already reached terminal state', () => {
  const entries = [
    {
      id: 1,
      role: 'assistant',
      runtime_run_id: 'r_w1_000001_000001',
      output: {
        agent_actions: [{
          id: 'a_w1_000001_000001_000001',
          action_id: 'a_w1_000001_000001_000001',
          action_kind: 'mcp.tool',
          capability_id: 'easydo_resource_base_info_refresh',
          status: 'executed',
          runtime_run_id: 'r_w1_000001_000001'
        }],
        runtime_events: []
      }
    }
  ]
  const replayEvents = [{
    type: 'action.decision_required',
    event_id: 'r_w1_000001_000001:ev000001',
    payload: {
      action_id: 'a_w1_000001_000001_000001',
      runtime_run_id: 'r_w1_000001_000001',
      action: {
        id: 'a_w1_000001_000001_000001',
        action_id: 'a_w1_000001_000001_000001',
        action_kind: 'mcp.tool',
        capability_id: 'easydo_resource_base_info_refresh'
      }
    },
    data: {
      action_id: 'a_w1_000001_000001_000001',
      runtime_run_id: 'r_w1_000001_000001'
    }
  }]

  const merged = mergeReplayEventsIntoEntries(entries, 'r_w1_000001_000001', replayEvents)

  assert.deepEqual(derivePendingActions(merged), [])
  assert.equal(merged[0].output.agent_actions[0].status, 'executed')
})

test('agent chatbox only renders full runtime trace on the latest assistant entry for one run', () => {
  const entries = [
    {
      id: 1,
      role: 'assistant',
      runtime_run_id: 'r_w1_000001_000001',
      output: {
        runtime_events: [
          { type: 'model.call_started', payload: { event_id: 'ev1' } },
          { type: 'reasoning_delta', payload: { event_id: 'ev2', delta: '先查资源。' } }
        ]
      }
    },
    {
      id: 2,
      role: 'tool',
      runtime_run_id: 'r_w1_000001_000001',
      output: {}
    },
    {
      id: 3,
      role: 'assistant',
      runtime_run_id: 'r_w1_000001_000001',
      output: {
        runtime_events: [
          { type: 'model.call_started', payload: { event_id: 'ev1' } },
          { type: 'reasoning_delta', payload: { event_id: 'ev2', delta: '先查资源。' } },
          { type: 'tool.result_prepared', payload: { event_id: 'ev3', display_json: { summary: '采集任务已排队' } } },
          { type: 'answer_delta', payload: { event_id: 'ev4', delta: '已触发采集。' } }
        ]
      }
    }
  ]

  assert.deepEqual(displayRuntimeEventsForEntry(entries[0], entries), [])
  assert.deepEqual(
    displayRuntimeEventsForEntry(entries[2], entries).map((event) => event.type),
    ['model.call_started', 'reasoning_delta', 'tool.result_prepared', 'answer_delta']
  )
})
