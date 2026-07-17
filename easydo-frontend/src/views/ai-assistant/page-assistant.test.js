import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { mergeReplayEventsIntoFirstAssistantEntry } from '../../stores/agentChatboxState.js'
import { buildSessionModelOverride } from '../../stores/agentConversationShared.js'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('layout mounts the floating page assistant', async () => {
  const layoutSource = await readFile(join(currentDir, '../layout/index.vue'), 'utf8')

  assert.match(layoutSource, /<PageAssistant\s*\/>/)
  assert.match(layoutSource, /import PageAssistant from '@\/views\/ai-assistant\/PageAssistant.vue'/)
})

test('page assistant uses current route context and generic runtime sessions', async () => {
  const [source, storeSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(source, /usePageAiContext/)
  assert.match(source, /ensureCurrentSession/)
  assert.match(source, /assistantStore\.sendMessage/)
  assert.match(storeSource, /sendPageAssistantMessageStream/)
  assert.match(storeSource, /PAGE_ASSISTANT_PROFILE_NAME/)
  assert.match(storeSource, /PAGE_ASSISTANT_CONTEXT_TAG\s*=\s*'page-assistant'/)
  assert.match(storeSource, /resolvePageAssistantProfile/)
  assert.match(storeSource, /profile_id:\s*assistantProfile\.id/)
  assert.doesNotMatch(storeSource, /agent_profile_id:\s*assistantProfile\.id/)
  assert.match(storeSource, /context_ref:\s*contextRef/)
  assert.doesNotMatch(storeSource, /\bscene\s*:/)
  assert.doesNotMatch(storeSource, /page-ai-assistant:global/)
  assert.doesNotMatch(storeSource, /include_current_page/)
  assert.doesNotMatch(source, /runtime_profile/i)
})

test('page assistant floating window can be dragged and persists position', async () => {
  const source = await readFile(join(currentDir, 'PageAssistant.vue'), 'utf8')

  assert.match(source, /assistantPositionStyle/)
  assert.match(source, /startAssistantDrag/)
  assert.match(source, /page_ai_assistant_position/)
  assert.match(source, /localStorage\.setItem/)
  assert.doesNotMatch(source, /\.page-assistant\s*\{[\s\S]*right:\s*24px/)
})

test('page assistant keeps manual scroll position during streaming and approval updates', async () => {
  const source = await readFile(join(currentDir, 'PageAssistant.vue'), 'utf8')

  assert.match(source, /useStickyScroll/)
  assert.match(source, /captureStickyScrollState/)
  assert.match(source, /restoreStickyScrollPosition/)
  assert.match(source, /requestScrollToBottom\(\)/)
  assert.doesNotMatch(source, /scrollBox\.value\.scrollTop\s*=\s*scrollBox\.value\.scrollHeight/)
})

test('page assistant shows user question immediately before model response', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  const pushIndex = storeSource.indexOf('entries.value.push(optimisticEntry)')
  const ensureIndex = storeSource.indexOf('await ensureCurrentSession(contextRef)')

  assert.match(storeSource, /createOptimisticUserEntry/)
  assert.match(storeSource, /entries\.value\.push\(optimisticEntry\)/)
  assert.match(storeSource, /replaceOptimisticEntry/)
  assert.match(storeSource, /status:\s*'sending'/)
  assert.match(storeSource, /localPendingEntries/)
  assert.ok(pushIndex > 0 && ensureIndex > 0 && pushIndex < ensureIndex)
})

test('page assistant uses readable bounded client ids without time or random suffixes', async () => {
  const [storeSource, idSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/aiRuntimeClientIds.js'), 'utf8')
  ])

  assert.match(storeSource, /createRuntimeClientIdFactory/)
  assert.match(storeSource, /nextClientEntryId/)
  assert.match(idSource, /readableSegment/)
  assert.match(idSource, /MAX_RUNTIME_CLIENT_ID_LENGTH/)
  assert.doesNotMatch(storeSource, /Date\.now\(\)/)
  assert.doesNotMatch(storeSource, /Math\.random/)
})

test('page assistant renders assistant reasoning through runtime trace instead of a separate block', async () => {
  const source = await readFile(join(currentDir, 'PageAssistant.vue'), 'utf8')

  assert.match(source, /assistantReasoningText/)
  assert.match(source, /reasoning_details/)
  assert.match(source, /:reasoning="assistantReasoningText\(entry\)"/)
  // Completed answers render in the dedicated final-answer block; streaming still
  // feeds RuntimeTrace via assistantAnswerText to avoid duplicate markdown.
  assert.match(source, /:answer="runtimeTraceAnswer\(entry\)"/)
  assert.match(source, /function runtimeTraceAnswer\(/)
  assert.match(source, /showFinalAnswer\(/)
  assert.match(source, /:timings="entry\.output\?\.timings \|\| \{\}"/)
  assert.doesNotMatch(source, /class="assistant-reasoning"/)
})

test('page assistant message header shows user send timestamp before sender label', async () => {
  const source = await readFile(join(currentDir, 'PageAssistant.vue'), 'utf8')

  assert.match(source, /messageHeaderText/)
  assert.match(source, /formatEntryTimestamp/)
  assert.match(source, /YYYY\/MM\/DD HH:mm:ss/)
  assert.match(source, /\$\{formatEntryTimestamp\(entry\)\} 你/)
})

test('page assistant streams thinking before answer and displays phase durations', async () => {
  const [source, storeSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(storeSource, /sendPageAssistantMessageStream/)
  assert.match(storeSource, /reasoning_delta/)
  assert.match(storeSource, /answer_delta/)
  assert.match(storeSource, /run\.step_announced/)
  assert.match(storeSource, /isPersistentRuntimeEvent/)
  assert.match(storeSource, /phaseTimings/)
  assert.match(source, /assistant-stage-timings/)
  assert.match(source, /assistantStatusText/)
  assert.match(source, /耗时/)
})

test('page assistant uses the chatbox runtime display contract', async () => {
  const [source, storeSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(source, /contentBlocksFromEntry/)
  assert.match(source, /renderMarkdownToHtml/)
  assert.match(source, /buildHtmlPreviewDocument/)
  assert.match(source, /displayRuntimeEventsForEntry/)
  assert.match(source, /agentRunMetricsForEntry/)
  assert.match(source, /assistantAgentRunMetrics/)
  assert.match(source, /assistant-agent-footer/)
  assert.match(source, /assistant-agent-metrics/)
  assert.match(source, /openHtmlPreview/)
  assert.match(source, /assistant-html-preview-frame/)
  assert.match(source, /approvalActionTooltip/)
  assert.match(source, /actionInputPreview/)
  assert.match(source, /class="assistant-model-row"/)
  assert.match(source, /assistantStore\.currentSessionModel/)
  assert.match(source, /assistantStore\.switchSessionModel/)
  assert.match(source, /assistantStore\.loadModelCatalog/)
  assert.match(storeSource, /getAIProviders/)
  assert.match(storeSource, /getAIModelBindings/)
  assert.match(storeSource, /updatePageAssistantSessionModel/)
  assert.match(storeSource, /currentSessionModel/)
  assert.match(storeSource, /modelSwitchOptions/)
  assert.match(storeSource, /canSwitchSessionModel/)
  assert.match(storeSource, /switchSessionModel/)
})

test('page assistant model override keeps provider display name separate from provider type', async () => {
  const override = buildSessionModelOverride({
    id: 8,
    name: 'page-provider',
    provider_type: 'anthropic'
  }, {
    provider_model_key: 'page-model'
  }, 'medium')

  assert.equal(override.provider.provider_id, '8')
  assert.equal(override.provider.name, 'page-provider')
  assert.equal(override.provider.provider_type, 'anthropic')
})

test('agent chatbox and page assistant stores share pure conversation helpers', async () => {
  const [chatboxSource, pageAssistantSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/agentChatbox.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  ])
  const centralizedHelpers = [
    'firstString',
    'asRecord',
    'piApprovalRequestPayload',
    'modelKeyFromBinding',
    'normalizeThinkingLevel',
    'buildSessionModelOverride',
    'appendEntryIfMissing',
    'replaceOptimisticEntry',
    'patchConversationEntry',
    'createStreamingAssistantEntry',
    'nextPendingActionDecisionKeys',
    'hasPendingActionDecision'
  ]
  const internalSharedHelpers = ['parseRecord', 'firstPositiveNumber', 'actionDecisionKey']

  for (const source of [chatboxSource, pageAssistantSource]) {
    assert.match(source, /from '@\/stores\/agentConversationShared'/)
    centralizedHelpers.forEach((helper) => {
      assert.match(source, new RegExp(`\\b${helper}\\b`))
      assert.doesNotMatch(source, new RegExp(`function\\s+${helper}\\s*\\(`))
    })
    internalSharedHelpers.forEach((helper) => {
      assert.doesNotMatch(source, new RegExp(`function\\s+${helper}\\s*\\(`))
    })
  }
})

test('page assistant model switch labels prefer provider display name over provider type', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  const currentModelStart = storeSource.indexOf('const currentSessionModel = computed')
  const currentModelEnd = storeSource.indexOf('const canSwitchSessionModel', currentModelStart)
  const switchOptionsStart = storeSource.indexOf('const modelSwitchOptions = computed')
  const switchOptionsEnd = storeSource.indexOf('function nextClientEntryId', switchOptionsStart)
  const currentModelSource = storeSource.slice(currentModelStart, currentModelEnd)
  const switchOptionsSource = storeSource.slice(switchOptionsStart, switchOptionsEnd)

  assert.match(currentModelSource, /provider:\s*firstString\(provider\.display_name,\s*provider\.displayName,\s*provider\.name,/)
  assert.match(switchOptionsSource, /provider_label:\s*firstString\(provider\.display_name,\s*provider\.displayName,\s*provider\.name,/)
})

test('page assistant applies stream deltas to the reactive entry in the list', async () => {
  const [storeSource, sharedSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentConversationShared.js'), 'utf8')
  ])

  assert.match(storeSource, /patchStreamingAssistantEntry/)
  assert.match(storeSource, /patchConversationEntry as patchStreamingAssistantEntry/)
  assert.match(sharedSource, /entriesRef\.value\.findIndex/)
  assert.match(sharedSource, /entriesRef\.value\.splice/)
  assert.doesNotMatch(storeSource, /updateStreamingAssistantEntry\(assistantDraft/)
})

test('page assistant consumes L5 runtime events and renders trace details', async () => {
  const [source, storeSource, runtimeEventSource, traceSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/aiRuntimeEvents.js'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  ])

  assert.match(storeSource, /capability\.snapshot/)
  assert.match(storeSource, /skill\.loaded/)
  assert.match(runtimeEventSource, /subagent\.spawned/)
  assert.match(runtimeEventSource, /action\.execution_started/)
  assert.match(runtimeEventSource, /action\.execution_succeeded/)
  assert.match(runtimeEventSource, /display_json/)
  assert.match(storeSource, /output_schema\.validated/)
  assert.match(storeSource, /appendRuntimeEventToEntry/)
  assert.match(source, /assistantRuntimeEvents/)
  assert.match(source, /<RuntimeTrace/)
  assert.match(traceSource, /process-timeline/)
  assert.match(traceSource, /buildRuntimeProcessItems/)
  assert.match(traceSource, /ai-runtime-event-line/)
  assert.match(traceSource, /approval/)
  assert.match(traceSource, /tool-result/)
  assert.match(traceSource, /subagentStatusLabel/)
  assert.match(traceSource, /openArtifact/)
  assert.match(traceSource, /openChildRun/)
  assert.match(traceSource, /loadRunEvents/)
  assert.match(traceSource, /artifactRefs/)
  assert.match(traceSource, /childRunLinkId/)
  assert.match(runtimeEventSource, /approval\.session_granted/)
  assert.match(traceSource, /approval-actions/)
  assert.match(traceSource, /运行产物/)
  assert.match(traceSource, /子运行/)
  assert.match(traceSource, /子 Agent 事件时间线/)
  assert.doesNotMatch(traceSource, /data\.tool_name/)
  assert.match(source, /assistantStructuredOutput/)
  assert.match(source, /结构化结果/)
})

test('page assistant exposes a visible replay refresh control for runtime traces', async () => {
  const [source, storeSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(source, /刷新轨迹/)
  assert.match(source, /refreshRuntimeTrace\(entry\)/)
  assert.match(source, /assistantStore\.refreshRuntimeEvents\(runtimeRunId\)/)
  assert.match(source, /assistantStore\.isRuntimeEventsRefreshing\(entryRuntimeRunId\(entry\)\)/)
  assert.match(storeSource, /function refreshRuntimeEvents\(runtimeRunId\)/)
  assert.match(storeSource, /listPageAssistantRunEvents\(normalizedRuntimeRunId\)/)
  assert.match(storeSource, /mergeReplayEventsIntoFirstAssistantEntry\(entries\.value,\s*normalizedRuntimeRunId,\s*replayEvents\)/)
  assert.match(storeSource, /pendingActions\.value = derivePendingActions\(entries\.value\)/)
})

test('page assistant renders failed and cancelled assistant entries through the shared failure card', async () => {
  const [source, failureCardSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/AssistantFailureCard.vue'), 'utf8')
  ])

  assert.match(source, /import AssistantFailureCard from '@\/components\/ai-runtime\/AssistantFailureCard\.vue'/)
  assert.match(source, /<AssistantFailureCard/)
  assert.match(source, /v-if="entry\.role === 'assistant' && isAssistantFailureEntry\(entry\)"/)
  assert.match(source, /@refresh="refreshRuntimeTrace\(entry\)"/)
  assert.match(source, /@continue="continueAfterFailure"/)
  assert.match(source, /@retry="retryFailedEntry\(entry\)"/)
  assert.match(source, /assistant-partial-answer/)
  assert.match(source, /precedingUserPrompt/)
  assert.match(source, /attachments:\s*prompt\.attachments/)
  assert.match(source, /assistantStore\.sendMessage\('继续',\s*assistantContext\.value,\s*\{ mode: 'follow_up' \}\)/)
  assert.match(source, /function showFinalAnswer\(entry\)[\s\S]*?if \(isAssistantFailureEntry\(entry\)\) return false/s)
  assert.doesNotMatch(source, /function assistantReasoningText\(entry\)[\s\S]*?if \(isAssistantFailureEntry\(entry\)\) return ''[\s\S]*?function assistantAnswerText/)
  assert.doesNotMatch(source, /function assistantAnswerText\(entry\)[\s\S]*?if \(isAssistantFailureEntry\(entry\)\) return ''[\s\S]*?function isCompletedAssistantEntry/)
  assert.match(failureCardSource, /assistantFailureDetails/)
  assert.match(failureCardSource, /var\(--status-danger-soft\)/)
  assert.match(failureCardSource, /var\(--danger-color\)/)
  assert.doesNotMatch(failureCardSource, /#[\da-f]{3,8}\b/i)
})

test('page assistant store forwards retry attachments into stream and queue payloads', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')

  assert.match(storeSource, /const attachments = normalizeSendAttachments\(options\.attachments\)/)
  assert.match(storeSource, /queueController\.enqueue\(content,\s*mode,\s*\{\s*attachments\s*\}\)/)
  assert.match(storeSource, /\.\.\.\(attachments\.length > 0 \? \{ attachments \} : \{\}\)/)
  assert.match(storeSource, /createOptimisticUserEntry\(content,\s*contextRef,\s*clientEntryId,\s*attachments\)/)
})

test('page assistant exposes stop generation and action decision controls', async () => {
  const [source, storeSource, runtimeEventSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/aiRuntimeEvents.js'), 'utf8')
  ])

  assert.match(storeSource, /activeStreamController/)
  assert.match(storeSource, /stopGeneration/)
  assert.match(storeSource, /pendingActions/)
  assert.match(storeSource, /agent_actions/)
  assert.match(storeSource, /uniquePendingActions/)
  assert.match(storeSource, /actionSemanticKey/)
  assert.match(storeSource, /streamPageAssistantActionDecision/)
  assert.match(storeSource, /continueAction/)
  assert.match(storeSource, /pendingActionDecisionKeys/)
  assert.match(source, /approvalButtonDisabled\(agentAction\)/)
  assert.match(runtimeEventSource, /tool\.executed/)
  assert.match(runtimeEventSource, /tool\.failed/)
  assert.match(runtimeEventSource, /tool\.rejected/)
  assert.match(source, /assistantStore\.stopGeneration/)
  assert.match(source, /v-if="assistantStore\.sending \|\| assistantStore\.hasActiveRun"/)
  assert.match(source, /:pending-actions="assistantStore\.pendingActions"/)
  assert.match(source, /#approval-actions="\{ agentAction \}"/)
  assert.doesNotMatch(source, /assistant-agentActions/)
  assert.match(source, /assistantStore\.approveAction/)
  assert.match(source, /assistantStore\.approveActionForSession/)
  assert.match(source, /本会话批准/)
  assert.match(source, /assistantStore\.rejectAction/)
  assert.match(source, /approvalButtonDisabled/)
  assert.match(source, /approvalButtonSelected/)
  assert.match(source, /const inputDisabled = computed\(\(\) => assistantStore\.sending \|\| assistantStore\.loading\)/)
  assert.doesNotMatch(source, /const inputDisabled = computed\(\(\) =>[^)]*hasActiveRun/)
  assert.doesNotMatch(source, /const inputDisabled = computed\(\(\) =>[^)]*pendingActions/)
})

test('page assistant reuses shared approval state derivation instead of local pending logic', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')

  assert.match(storeSource, /derivePendingActions/)
  assert.doesNotMatch(storeSource, /function derivePendingActions\(entryList = \[\]\)/)
  assert.doesNotMatch(storeSource, /function normalizePiApproval\(entry = \{\}\)/)
})

test('page assistant unwraps Pi runtime event approvals through the shared session projection', async () => {
  const [storeSource, stateSource, projectionSource] = await Promise.all([
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/agentChatboxState.js'), 'utf8'),
    readFile(join(currentDir, '../../stores/runtimeSessionStreamProjection.js'), 'utf8')
  ])
  const continueActionStart = storeSource.indexOf('async function continueAction')
  const piBranchIndex = storeSource.indexOf('if (isPiApprovalAction(agentAction))', continueActionStart)
  const legacyPendingIndex = storeSource.indexOf('setActionDecisionPending(id, runtimeDecision, true)', continueActionStart)
  const pageSource = await readFile(join(currentDir, 'PageAssistant.vue'), 'utf8')

  assert.match(storeSource, /event\s*===\s*'runtime_event'/)
  assert.match(storeSource, /sessionController\.handleRuntimeEventPayload/)
  assert.match(projectionSource, /runtimeEventName\s*===\s*'permission\.asked'/)
  assert.match(projectionSource, /entry\.output\.awaiting_approval/)
  assert.match(storeSource, /derivePendingActions/)
  assert.match(stateSource, /pi_approval:\s*true/)
  assert.match(storeSource, /continuePiApproval\(agentAction,\s*runtimeDecision,\s*decisionPayload\)/)
  assert.match(storeSource, /resolveAgentActionRef\(/)
  assert.match(pageSource, /assistantStore\.approveAction\(agentAction\)/)
  assert.match(pageSource, /assistantStore\.approveActionForSession\(agentAction\)/)
  assert.match(pageSource, /assistantStore\.rejectAction\(agentAction\)/)
  assert.doesNotMatch(pageSource, /assistantStore\.approveAction\(agentAction\.id\)/)
  assert.ok(piBranchIndex > continueActionStart)
  assert.ok(legacyPendingIndex > continueActionStart)
  assert.ok(piBranchIndex < legacyPendingIndex)
})

test('page assistant streams Pi approval continuations into the existing runtime entry', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  const importIndex = storeSource.indexOf('streamPageAssistantPiApproval')
  const continuePiStart = storeSource.indexOf('async function continuePiApproval')
  const streamIndex = storeSource.indexOf('await streamPageAssistantPiApproval(runtimeRunId, decision', continuePiStart)
  const handlerIndex = storeSource.indexOf('handleStreamEvent(assistantDraftId, event, data, finalRef)', streamIndex)
  const existingEntryIndex = storeSource.indexOf('findAssistantEntryForRuntimeRun(entries.value, runtimeRunId)', continuePiStart)

  assert.ok(importIndex > 0)
  assert.ok(continuePiStart > 0)
  assert.ok(existingEntryIndex > continuePiStart)
  assert.ok(streamIndex > continuePiStart)
  assert.ok(handlerIndex > streamIndex)
  assert.match(storeSource, /const assistantDraftId = existingAssistantEntry\?\.id \|\| createdAssistantEntry\.id/)
  assert.doesNotMatch(storeSource, /activeRuntimeRunId/)
  assert.doesNotMatch(storeSource.slice(continuePiStart, streamIndex), /decidePageAssistantPiApproval/)
})

test('page assistant refreshes pending approvals after final assistant entry replacement', async () => {
  const storeSource = await readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  const assistantEntryIndex = storeSource.indexOf("if (event === 'assistant_entry')")
  const replaceIndex = storeSource.indexOf('replaceOptimisticEntry(entries.value, assistantDraftId, finalEntry)', assistantEntryIndex)
  const deriveIndex = storeSource.indexOf('pendingActions.value = derivePendingActions(entries.value)', replaceIndex)
  const nextBranchIndex = storeSource.indexOf("if (event === 'error')", assistantEntryIndex)

  assert.ok(assistantEntryIndex > 0)
  assert.ok(replaceIndex > assistantEntryIndex)
  assert.ok(deriveIndex > replaceIndex)
  assert.ok(deriveIndex < nextBranchIndex)
})

test('page assistant inline trace approvals can match Pi permission request ids', async () => {
  const [source, traceSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/RuntimeTrace.vue'), 'utf8')
  ])

  assert.match(source, /:pending-actions="assistantStore\.pendingActions"/)
  assert.match(source, /#approval-actions="\{ agentAction \}"/)
  assert.match(traceSource, /item\?\.data\?\.request_id/)
  assert.match(traceSource, /item\?\.data\?\.approval_id/)
})

test('page assistant trace renders typed action payloads only', async () => {
  const [source, runtimeProcessSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../components/ai-runtime/runtimeProcessEvents.js'), 'utf8')
  ])
  const combinedSource = `${source}\n${runtimeProcessSource}`

  assert.match(source, /<RuntimeTrace/)
  assert.match(source, /:pending-actions="assistantStore\.pendingActions"/)
  assert.match(source, /pendingActionToRuntimeEvent/)
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

test('page assistant renders action continuation entries and decision events', async () => {
  const [source, storeSource] = await Promise.all([
    readFile(join(currentDir, 'PageAssistant.vue'), 'utf8'),
    readFile(join(currentDir, '../../stores/pageAiAssistant.js'), 'utf8')
  ])

  assert.match(storeSource, /createActionDecisionEntry/)
  assert.match(storeSource, /action\.approved/)
  assert.match(storeSource, /action\.rejected/)
  assert.match(storeSource, /handleStreamEvent/)
  assert.match(source, /entry\?\.role === 'tool'/)
  assert.match(source, /#approval-actions="\{ agentAction \}"/)
})

test('page assistant replay does not revive actions that already reached terminal state', () => {
  const entries = [{
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
  }]
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

  const merged = mergeReplayEventsIntoFirstAssistantEntry(entries, 'r_w1_000001_000001', replayEvents)

  assert.equal(merged[0].output.agent_actions[0].status, 'executed')
  assert.equal(merged[0].output.agent_actions.length, 1)
  assert.equal(merged[0].output.runtime_events.length, 1)
})
