import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  getCurrentAiSession,
  getAiSession,
  listPageAssistantProfiles,
  PAGE_ASSISTANT_PROFILE_NAME,
  cancelPageAssistantSession,
  listAiSessionEntries,
  listPageAssistantRunEvents,
  sendPageAssistantMessageStream,
  streamPageAssistantActionDecision,
  streamPageAssistantPiApproval,
  streamPageAssistantRunEvents,
  updatePageAssistantSessionModel
} from '@/api/pageAiAssistant'
import { getAIModelBindings, getAIProviders } from '@/api/store'
import {
  isPersistentRuntimeEvent,
  isProcessRuntimeEvent,
  mergeRuntimeEvents,
  normalizeRuntimeEvent
} from '@/stores/aiRuntimeEvents'
import { createRuntimeClientIdFactory } from '@/stores/aiRuntimeClientIds'
import {
  actionIdFromRef,
  appendEntryIfMissing,
  asRecord,
  buildSessionModelOverride,
  createStreamingAssistantEntry,
  firstString,
  hasPendingActionDecision,
  isPiApprovalAction,
  modelKeyFromBinding,
  nextPendingActionDecisionKeys,
  normalizeThinkingLevel,
  patchConversationEntry as patchStreamingAssistantEntry,
  piApprovalRequestPayload,
  replaceOptimisticEntry,
  resolveAgentActionRef
} from '@/stores/agentConversationShared'
import {
  agentRuntimeErrorMessage,
  normalizeAgentRuntimeError
} from '@/utils/agentRuntimeError'
import {
  handleRuntimeEventWithCursor,
  isAgentRuntimeFollowCurrent,
  resolveAgentRuntimeFollowError,
  shouldRetryAgentRuntimeConnection
} from '@/stores/agentRuntimeConnectionState'
import { createRuntimeSessionController } from '@/stores/runtimeSessionController'

import {
  activeRuntimeRunFromState,
  actionSemanticKey,
  derivePendingActions,
  entryNeedsRuntimeEventReplay,
  mergeReplayEventsIntoFirstAssistantEntry,
  normalizeAction,
  runtimeEventsFromReplayResponse
} from '@/stores/agentChatboxState'

const PAGE_ASSISTANT_CONTEXT_TAG = 'page-assistant'
const pageAssistantClientIds = createRuntimeClientIdFactory('pa')

function normalizeEntries(payload) {
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload)) return payload
  return []
}

function extractArray(payload) {
  if (Array.isArray(payload?.data?.items)) return payload.data.items
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload)) return payload
  return []
}

function normalizeStringArray(value) {
  if (Array.isArray(value)) {
    return [...new Set(value.map((item) => String(item).trim()).filter(Boolean))]
  }
  if (typeof value === 'string') {
    return [...new Set(value.split(',').map((item) => item.trim()).filter(Boolean))]
  }
  return []
}

function pageAssistantProfileTags(profile = {}) {
  return normalizeStringArray(profile.context_tags)
}

function localPendingEntries(entryList) {
  return entryList.filter((entry) => {
    const id = String(entry?.id || '')
    return id.startsWith('page-assistant-') && ['sending', 'failed'].includes(entry?.status)
  })
}

function mergeEntriesWithLocalPending(serverEntries, pendingEntries) {
  const merged = [...serverEntries]
  pendingEntries.forEach((entry) => appendEntryIfMissing(merged, entry))
  return merged
}

function normalizeSendAttachments(value) {
  if (!Array.isArray(value)) return []
  return value.filter((item) => item && (typeof item === 'object' || typeof item === 'string'))
}

function createOptimisticUserEntry(content, contextRef, clientEntryId, attachments = []) {
  const now = new Date().toISOString()
  const normalizedAttachments = normalizeSendAttachments(attachments)
  return {
    id: clientEntryId,
    idempotency_key: clientEntryId,
    role: 'user',
    entry_type: 'message',
    status: 'sending',
    content,
    content_blocks: [{ type: 'text', text: content }],
    ...(normalizedAttachments.length > 0 ? { attachments: normalizedAttachments } : {}),
    input: {
      context_ref: contextRef,
      ...(normalizedAttachments.length > 0 ? { attachments: normalizedAttachments } : {})
    },
    output: {},
    created_at: now,
    updated_at: now
  }
}

function createActionDecisionEntry(agentAction = {}, decision = 'approve_once') {
  const now = new Date().toISOString()
  const approved = decision === 'approve_once' || decision === 'approve_session'
  const actionName = actionDisplayTitle(agentAction)
  const event = approved ? 'action.approved' : 'action.rejected'
  const actionId = String(agentAction.action_id || agentAction.id || 'unknown-action')
  const entryId = `page-assistant-action-${actionId}-${decision}`
  const content = `${approved ? '已确认' : '已拒绝'} ${actionName}`
  return {
    id: entryId,
    idempotency_key: `${agentAction.runtime_run_id || 'runtime'}:${actionId}:${decision}:decision`,
    role: 'user',
    entry_type: 'run_event',
    status: 'completed',
    content,
    content_blocks: [{ type: 'text', text: content }],
    input: {
      action_id: actionId,
      action_kind: agentAction.action_kind || '',
      display_json: agentAction.display_json || {},
      decision
    },
    output: {
      display_json: agentAction.display_json || {},
      decision,
      runtime_events: [{
        event,
        data: {
          action_id: actionId,
          action_kind: agentAction.action_kind || '',
          display_json: agentAction.display_json || {}
        },
        timestamp: now
      }]
    },
    created_at: now,
    updated_at: now
  }
}

function appendRuntimeEventToEntry(entriesRef, entryId, event, data) {
  patchStreamingAssistantEntry(entriesRef, entryId, (entry) => {
    const events = Array.isArray(entry.output.runtime_events) ? entry.output.runtime_events : []
    entry.output.runtime_events = [
      ...events,
      normalizeRuntimeEvent(event, data)
    ]
  })
}

function mergeAssistantRuntimeEvents(entry, runtimeEvents = []) {
  if (!entry || !runtimeEvents.length) return entry
  const output = entry.output || {}
  const existingEvents = Array.isArray(output.runtime_events) ? output.runtime_events : []
  return {
    ...entry,
    output: {
      ...output,
      runtime_events: mergeRuntimeEvents(existingEvents, runtimeEvents)
    }
  }
}

function processRuntimeEvents(entry) {
  const events = Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : []
  return events.filter((item) => isProcessRuntimeEvent(item?.event || item?.type || item?.payload?.event_type))
}

async function replayEventsIntoEntries(entryList) {
  const replayedEvents = []
  const runIds = [...new Set(
    entryList
      .filter(entryNeedsRuntimeEventReplay)
      .map((entry) => String(entry?.runtime_run_id || entry?.output?.runtime_run_id || '').trim())
      .filter(Boolean)
  )]
  for (const runtimeRunId of runIds) {
    const response = await listPageAssistantRunEvents(runtimeRunId)
    const replayEvents = runtimeEventsFromReplayResponse(response)
    if (replayEvents.length === 0) continue
    replayedEvents.push(...replayEvents)
    mergeReplayEventsIntoFirstAssistantEntry(entryList, runtimeRunId, replayEvents)
  }
  return replayedEvents
}

function runtimeEventsFromEntries(entryList = []) {
  return entryList.flatMap((entry) => Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : [])
}

function actionDisplayTitle(agentAction = {}) {
  return String(
    agentAction.display_json?.title ||
      agentAction.display_json?.name ||
      agentAction.capability_id ||
      agentAction.action_kind ||
      '动作决策'
  )
}

function uniquePendingActions(agentActions) {
  const bySemanticKey = new Map()
  agentActions
    .map(normalizeAction)
    .filter((item) => item && item.status === 'awaiting_decision')
    .forEach((item) => {
      const key = actionSemanticKey(item)
      if (!bySemanticKey.has(key)) bySemanticKey.set(key, item)
  })
  return [...bySemanticKey.values()]
}

function findAssistantEntryForRuntimeRun(entryList, runtimeRunId) {
  return entryList.find((entry) =>
    String(entry?.runtime_run_id || '') === String(runtimeRunId) && entry?.role === 'assistant'
  ) || null
}

function markOptimisticEntryFailed(entryList, clientEntryId) {
  const entry = entryList.find((item) => item.id === clientEntryId || item.idempotency_key === clientEntryId)
  if (entry) {
    entry.status = 'failed'
    entry.updated_at = new Date().toISOString()
  }
}

function findActionById(entryList, id) {
  for (const entry of entryList) {
    const agentActions = Array.isArray(entry?.output?.agent_actions) ? entry.output.agent_actions : []
    const matched = agentActions.find((item) => String(item?.id || item?.action_id || '') === String(id))
    if (matched) return normalizeAction(matched)
  }
  return null
}

function markActionStatus(entryList, id, status) {
  entryList.forEach((entry) => {
    const agentActions = Array.isArray(entry?.output?.agent_actions) ? entry.output.agent_actions : []
    agentActions.forEach((item) => {
      if (String(item?.id || item?.action_id || '') === String(id)) {
        item.status = status
      }
    })
  })
}

export const usePageAiAssistantStore = defineStore('pageAiAssistant', () => {
  const session = ref(null)
  const assistantProfile = ref(null)
  const entries = ref([])
  const loading = ref(false)
  const sending = ref(false)
  const stopping = ref(false)
  const error = ref('')
  const phaseTimings = ref({})
  const pendingActions = ref([])
  const pendingActionDecisionKeys = ref(new Set())
  const activeStreamController = ref(null)
  const aiProviders = ref([])
  const modelBindingsByProvider = ref({})
  const switchingModel = ref(false)
  const runtimeError = ref(null)
  const queueContextRef = ref({})
  let streamEpoch = 0

  function clearRuntimeError() {
    error.value = ''
    runtimeError.value = null
  }

  function setRuntimeError(value, fallbackMessage) {
    const normalized = normalizeAgentRuntimeError(value, fallbackMessage)
    runtimeError.value = normalized.toJSON()
    error.value = agentRuntimeErrorMessage(normalized, fallbackMessage)
    return normalized
  }

  const hasSession = computed(() => Boolean(session.value?.id))
  const activeRuntimeRun = computed(() => activeRuntimeRunFromState(session.value, entries.value))
  const hasActiveRun = computed(() => Boolean(activeRuntimeRun.value?.runtime_run_id))
  const sessionModelOverride = computed(() => asRecord(session.value?.model_override))
  const currentSessionModel = computed(() => {
    const override = sessionModelOverride.value
    const profile = assistantProfile.value || {}
    const provider = asRecord(override.provider || profile.provider)
    const model = asRecord(override.model || profile.model)
    const inference = asRecord(override.inference || profile.inference)
    return {
      provider: firstString(provider.display_name, provider.displayName, provider.name, provider.provider_id, provider.id, provider.provider_type, '-'),
      model: firstString(model.provider_model_key, model.model, model.name, model.id, model.model_id, '-'),
      thinking_level: normalizeThinkingLevel(firstString(inference.thinking_level, inference.reasoning, inference.reasoning_effort, 'medium'))
    }
  })
  const canSwitchSessionModel = computed(() =>
    Boolean(session.value?.id) &&
      !sending.value &&
      !loading.value &&
      !hasActiveRun.value &&
      pendingActions.value.length === 0 &&
      !switchingModel.value
  )
  const modelSwitchOptions = computed(() => aiProviders.value.flatMap((provider) => {
    const bindings = modelBindingsByProvider.value[String(provider.id)] || []
    return bindings.map((binding) => ({
      key: `${provider.id}:${binding.id || modelKeyFromBinding(binding)}`,
      provider,
      binding,
      provider_label: firstString(provider.display_name, provider.displayName, provider.name, provider.provider_id, provider.id, provider.provider_type),
      model_label: modelKeyFromBinding(binding)
    }))
  }).filter((item) => item.provider_label && item.model_label))

  function nextClientEntryId(contextRef = {}, purpose = 'msg') {
    return pageAssistantClientIds.nextClientEntryId({
      workspace_id: contextRef.workspace_id || session.value?.workspace_id || assistantProfile.value?.workspace_id,
      session_id: session.value?.id || 'new'
    }, purpose)
  }

  const sessionController = createRuntimeSessionController({
    getSessionId: () => session.value?.id,
    getContextRef: () => queueContextRef.value,
    setRuntimeError,
    clearRuntimeError,
    nextClientId: () => nextClientEntryId(queueContextRef.value, 'queue'),
    patchEntry: patchStreamingAssistantEntry,
    entriesRef: entries,
    pendingActionsRef: pendingActions,
    appendRuntimeEvent: appendRuntimeEventToEntry,
    initialConnectionState: 'idle'
  })
  const queueController = sessionController.queueController
  const queueItems = sessionController.queueItems
  const queueLoading = sessionController.queueLoading
  const queueMode = sessionController.queueMode
  const loadQueueItems = sessionController.loadQueueItems
  const cancelQueueItem = sessionController.cancelQueueItem
  const reorderQueueItems = sessionController.reorderQueueItems
  const lastEventIdByRun = sessionController.lastEventIdByRun
  const refreshingRuntimeRunIds = sessionController.refreshingRuntimeRunIds
  const setRuntimeEventsRefreshing = sessionController.setRuntimeEventsRefreshing
  const isRuntimeEventsRefreshing = sessionController.isRuntimeEventsRefreshing

  function setActionDecisionPending(id, decision, pending) {
    pendingActionDecisionKeys.value = nextPendingActionDecisionKeys(pendingActionDecisionKeys.value, id, decision, pending)
  }

  function isActionDecisionPending(id) {
    return hasPendingActionDecision(pendingActionDecisionKeys.value, id)
  }

  async function refreshRuntimeEvents(runtimeRunId) {
    const normalizedRuntimeRunId = String(runtimeRunId || '').trim()
    if (!normalizedRuntimeRunId || isRuntimeEventsRefreshing(normalizedRuntimeRunId)) return []
    setRuntimeEventsRefreshing(normalizedRuntimeRunId, true)
    clearRuntimeError()
    try {
      const response = await listPageAssistantRunEvents(normalizedRuntimeRunId)
      const replayEvents = runtimeEventsFromReplayResponse(response)
      if (replayEvents.length > 0) {
        mergeReplayEventsIntoFirstAssistantEntry(entries.value, normalizedRuntimeRunId, replayEvents)
        pendingActions.value = derivePendingActions(entries.value)
      }
      return replayEvents
    } catch (err) {
      setRuntimeError(err, '页面助手事件回放失败')
      throw err
    } finally {
      setRuntimeEventsRefreshing(normalizedRuntimeRunId, false)
    }
  }

  async function loadModelCatalog() {
    const providerResponse = await getAIProviders()
    aiProviders.value = extractArray(providerResponse).filter((provider) => String(provider.status || 'active') === 'active')
    const pairs = await Promise.all(aiProviders.value.map(async (provider) => {
      const response = await getAIModelBindings(provider.id).catch(() => ({ data: [] }))
      return [String(provider.id), extractArray(response).filter((binding) => String(binding.status || 'active') === 'active')]
    }))
    modelBindingsByProvider.value = Object.fromEntries(pairs)
    return modelSwitchOptions.value
  }

  async function resolvePageAssistantProfile() {
    if (assistantProfile.value?.id) return assistantProfile.value
    const response = await listPageAssistantProfiles()
    const profile = extractArray(response).find((item) => String(item?.name || '').trim() === PAGE_ASSISTANT_PROFILE_NAME)
    if (!profile?.id) {
      throw new Error(`页面助手 Profile ${PAGE_ASSISTANT_PROFILE_NAME} 未配置`)
    }
    if (['disabled', 'archived'].includes(String(profile.status || '').trim())) {
      throw new Error(`页面助手 Profile ${PAGE_ASSISTANT_PROFILE_NAME} 已禁用`)
    }
    if (!pageAssistantProfileTags(profile).includes(PAGE_ASSISTANT_CONTEXT_TAG)) {
      throw new Error(`页面助手 Profile ${PAGE_ASSISTANT_PROFILE_NAME} 必须包含 ${PAGE_ASSISTANT_CONTEXT_TAG} 上下文标签`)
    }
    assistantProfile.value = profile
    return profile
  }

  async function ensureCurrentSession(contextRef = {}) {
    queueContextRef.value = contextRef
    if (session.value?.id) return session.value
    loading.value = true
    clearRuntimeError()
    try {
      const assistantProfile = await resolvePageAssistantProfile()
      const response = await getCurrentAiSession({
        profile_id: assistantProfile.id,
        session_kind: 'chat',
        business_type: 'workspace',
        business_id: String(contextRef.workspace_id || ''),
        source: PAGE_ASSISTANT_PROFILE_NAME,
        title: 'Page Assistant'
      })
      session.value = response?.data || response
      if (session.value?.id) {
          await Promise.all([
            loadEntries(session.value.id),
            loadQueueItems(session.value.id).catch(() => [])
          ])
        if (activeRuntimeRun.value?.runtime_run_id && !sending.value) {
          void followActiveRun(session.value.id).catch(() => null)
        }
      }
      return session.value
    } catch (err) {
      setRuntimeError(err, '页面助手会话创建失败')
      throw err
    } finally {
      loading.value = false
    }
  }

  async function refreshSessionSummary(sessionId = session.value?.id) {
    if (!sessionId) return null
    const response = await getAiSession(sessionId)
    const nextSession = response?.data || response
    if (nextSession?.id && String(session.value?.id || '') === String(nextSession.id)) {
      session.value = { ...session.value, ...nextSession }
    }
    return nextSession
  }

  async function loadEntries(sessionId = session.value?.id) {
    if (!sessionId) return []
    const pendingEntries = localPendingEntries(entries.value)
    const response = await listAiSessionEntries(sessionId)
    const nextEntries = mergeEntriesWithLocalPending(normalizeEntries(response), pendingEntries)
    try {
      await replayEventsIntoEntries(nextEntries)
    } catch (err) {
      setRuntimeError(err, '页面助手事件回放失败')
    }
    entries.value = nextEntries
    const nextCursors = { ...lastEventIdByRun.value }
    for (const entry of nextEntries) {
      const runtimeRunId = firstString(entry.runtime_run_id, entry.output?.runtime_run_id)
      if (!runtimeRunId) continue
      const latestEvent = runtimeEventsFromEntries([entry])
        .filter((event) => firstString(event?.event_id, event?.payload?.event_id, event?.data?.event_id))
        .sort((left, right) => Number(left?.event_seq || left?.payload?.event_seq || 0) - Number(right?.event_seq || right?.payload?.event_seq || 0))
        .at(-1)
      const eventId = firstString(latestEvent?.event_id, latestEvent?.payload?.event_id, latestEvent?.data?.event_id)
      if (eventId) nextCursors[runtimeRunId] = eventId
    }
    lastEventIdByRun.value = nextCursors
    pageAssistantClientIds.observeExistingCount(entries.value.length)
    pendingActions.value = derivePendingActions(entries.value)
    return entries.value
  }

  function handleStreamEvent(assistantDraftId, event, data, finalRef, clientEntryId = '') {
    if (event === 'stream_protocol_error') {
      setRuntimeError(data, '页面助手收到无法解析的事件')
      return
    }
    if (event === 'heartbeat') {
      sessionController.handleHeartbeat(data)
      return
    }
    if (event === 'runtime_event') {
      return handleRuntimeEventWithCursor(() => {
        sessionController.handleRuntimeEventPayload(assistantDraftId, data)
      }, () => {})
    }
    const runtimeRunId = String(data?.runtime_run_id || data?.run?.runtime_run_id || data?.entry?.runtime_run_id || '').trim()
    void queueController.applyRuntimeEvent({ type: event, ...data }).catch(() => null)
    return handleRuntimeEventWithCursor(() => {
      if (isPersistentRuntimeEvent(event) || isProcessRuntimeEvent(event)) {
        appendRuntimeEventToEntry(entries, assistantDraftId, event, data)
      }
      if (event === 'capability.snapshot') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          entry.output.capabilities = data || {}
        })
      }
      if (event === 'skill.available') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          const skills = Array.isArray(entry.output.available_skills) ? entry.output.available_skills : []
          entry.output.available_skills = [...skills, data]
        })
      }
      if (event === 'skill.loaded') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          const skills = Array.isArray(entry.output.loaded_skills) ? entry.output.loaded_skills : []
          entry.output.loaded_skills = [...skills, data]
        })
      }
      if (event === 'subagent.completed') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          const subagents = Array.isArray(entry.output.subagent_results) ? entry.output.subagent_results : []
          entry.output.subagent_results = [...subagents, data]
        })
      }
      if (event === 'output_schema.validated' || event === 'output_schema.invalid') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          entry.output.output_schema_valid = data?.valid !== false
          entry.output.output_schema_errors = Array.isArray(data?.errors) ? data.errors : []
        })
      }
      if (event === 'action.decision_required') {
        const agentAction = normalizeAction(data)
        if (agentAction) {
          pendingActions.value = uniquePendingActions([
            ...pendingActions.value.filter((item) => String(item.id) !== String(agentAction.id)),
            agentAction
          ])
          patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
            const agentActions = Array.isArray(entry.output.agent_actions) ? entry.output.agent_actions : []
            entry.output.agent_actions = uniquePendingActions([
              ...agentActions.filter((item) => String(item.id) !== String(agentAction.id)),
              agentAction
            ])
          })
        }
      }
      if (event === 'user_entry' && clientEntryId) {
        replaceOptimisticEntry(entries.value, clientEntryId, data?.entry)
      }
      if (event === 'run.step_announced') {
        const stage = data?.stage
        if (stage) {
          phaseTimings.value = {
            ...phaseTimings.value,
            [stage]: data?.elapsed_ms
          }
          patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
            entry.output.timings = { ...(entry.output.timings || {}), [stage]: data?.elapsed_ms }
          })
        }
      }
      if (event === 'reasoning_delta') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          entry.output.reasoning = `${entry.output.reasoning || ''}${data?.delta || ''}`
        })
      }
      if (event === 'answer_delta') {
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          entry.content = `${entry.content || ''}${data?.delta || ''}`
          entry.content_blocks = [{ type: 'text', text: entry.content }]
        })
      }
      if (event === 'tool_entry') {
        appendEntryIfMissing(entries.value, data?.entry)
      }
      if (event === 'action_entry') {
        appendEntryIfMissing(entries.value, data?.entry)
      }
      if (event === 'assistant_entry') {
        const draftEntry = entries.value.find((entry) => String(entry?.id || entry?.idempotency_key) === String(assistantDraftId))
        const finalEntry = mergeAssistantRuntimeEvents(data?.entry, processRuntimeEvents(draftEntry))
        finalRef.value = { ...data, entry: finalEntry }
        if (finalEntry?.output?.runtime_events?.length) {
          patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
            entry.output.runtime_events = finalEntry.output.runtime_events
          })
        }
        replaceOptimisticEntry(entries.value, assistantDraftId, finalEntry)
        pendingActions.value = derivePendingActions(entries.value)
      }
      if (event === 'error') {
        const normalized = setRuntimeError(data, '页面助手消息发送失败')
        patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
          entry.status = 'failed'
          entry.entry_type = 'error'
          entry.content = `模型调用失败：${error.value}`
          entry.output.runtime_error = normalized.toJSON()
        })
      }
      if (event === 'done') {
        phaseTimings.value = data?.timings || phaseTimings.value
      }
    }, () => {
      if (runtimeRunId && data?.event_id) {
        sessionController.rememberEventCursor(runtimeRunId, data.event_id)
      }
    })
  }

  async function followActiveRun(sessionId, finalRef = ref(null)) {
    const epoch = ++streamEpoch
    let attempt = 0
    let summary = null
    while (isAgentRuntimeFollowCurrent({
      epoch,
      currentEpoch: streamEpoch,
      sessionId,
      currentSessionId: session.value?.id,
      stopping: stopping.value
    })) {
      try {
        summary = await refreshSessionSummary(sessionId)
        if (!summary?.active_run?.runtime_run_id) {
          await loadEntries(sessionId)
          return finalRef.value
        }
        const runtimeRunId = summary.active_run.runtime_run_id
        await loadEntries(sessionId)
        const assistantEntry = findAssistantEntryForRuntimeRun(entries.value, runtimeRunId)
        if (!assistantEntry) throw new Error('页面助手 active Run Assistant Entry 不存在')
        const controller = new AbortController()
        activeStreamController.value = controller
        let streamError = null
        await streamPageAssistantRunEvents(runtimeRunId, lastEventIdByRun.value[runtimeRunId] || '', ({ event, data }) => {
          if (event === 'error' || event === 'stream_protocol_error') {
            streamError = normalizeAgentRuntimeError(data, '页面助手事件续传失败')
            return
          }
          handleStreamEvent(assistantEntry.id, event, data, finalRef)
        }, { signal: controller.signal })
        if (streamError) throw streamError
        await Promise.all([refreshSessionSummary(sessionId), loadEntries(sessionId)])
        if (!activeRuntimeRun.value?.runtime_run_id) return finalRef.value
      } catch (err) {
        const resolution = await resolveAgentRuntimeFollowError(err, {
          followState: {
            epoch,
            currentEpoch: streamEpoch,
            sessionId,
            currentSessionId: session.value?.id,
            stopping: stopping.value
          },
          fallbackMessage: '页面助手实时连接已中断',
          previousSummary: summary,
          refreshSummary: () => refreshSessionSummary(sessionId)
        })
        if (resolution.silent) return finalRef.value
        setRuntimeError(resolution.error, '页面助手实时连接已中断')
        summary = resolution.summary
        if (!resolution.retry) {
          await loadEntries(sessionId).catch(() => null)
          return finalRef.value
        }
        attempt += 1
        await new Promise((resolve) => globalThis.setTimeout(resolve, Math.min(5000, 250 * (2 ** Math.min(attempt - 1, 4)))))
      }
    }
    return finalRef.value
  }

  async function sendMessage(content, contextRef = {}, options = {}) {
    queueContextRef.value = contextRef
    const epoch = ++streamEpoch
    const mode = String(options.mode || 'follow_up')
    const attachments = normalizeSendAttachments(options.attachments)
    const clientEntryId = nextClientEntryId(contextRef, 'msg')
    const optimisticEntry = createOptimisticUserEntry(content, contextRef, clientEntryId, attachments)
    const assistantDraft = createStreamingAssistantEntry(clientEntryId)
    entries.value.push(optimisticEntry)
    sending.value = true
    clearRuntimeError()
    const finalRef = ref(null)
    let runningSessionId = ''
    try {
      const currentSession = await ensureCurrentSession(contextRef)
      if (!currentSession?.id) {
        markOptimisticEntryFailed(entries.value, clientEntryId)
        return null
      }
      runningSessionId = currentSession.id
      if (hasActiveRun.value) {
        await queueController.enqueue(content, mode, { attachments })
        await refreshSessionSummary(currentSession.id).catch(() => null)
        await loadEntries(currentSession.id).catch(() => null)
        return { queued: true, mode }
      }
      if (mode === 'steer') {
        throw normalizeAgentRuntimeError(
          { code: 'active_run_required', message: 'steer 需要当前有进行中的 Run' },
          '页面助手队列消息发送失败'
        )
      }
      appendEntryIfMissing(entries.value, assistantDraft)
      phaseTimings.value = {}
      activeStreamController.value = new AbortController()
      await sendPageAssistantMessageStream(currentSession.id, {
        content,
        context_ref: contextRef,
        client_entry_id: clientEntryId,
        runtime_engine: 'pi',
        ...(attachments.length > 0 ? { attachments } : {})
      }, ({ event, data }) => {
        handleStreamEvent(assistantDraft.id, event, data, finalRef, clientEntryId)
      }, { signal: activeStreamController.value.signal })
      const summaryAfterInitialStream = await refreshSessionSummary(currentSession.id)
      if (summaryAfterInitialStream?.active_run?.runtime_run_id) {
        return followActiveRun(currentSession.id, finalRef)
      }
      await loadEntries(currentSession.id)
      return finalRef.value
    } catch (err) {
      const currentSessionId = session.value?.id
      let normalized
      let summary = null
      if (err?.name === 'AbortError') {
        const resolution = await resolveAgentRuntimeFollowError(err, {
          followState: {
            epoch,
            currentEpoch: streamEpoch,
            sessionId: runningSessionId,
            currentSessionId,
            stopping: stopping.value
          },
          fallbackMessage: '页面助手消息发送连接已中断',
          previousSummary: null,
          refreshSummary: () => refreshSessionSummary(runningSessionId)
        })
        if (resolution.silent) return finalRef.value
        normalized = resolution.error
        summary = resolution.summary
      } else {
        normalized = normalizeAgentRuntimeError(err, '页面助手消息发送失败')
        summary = currentSessionId ? await refreshSessionSummary(currentSessionId).catch(() => null) : null
      }
      if (runningSessionId && shouldRetryAgentRuntimeConnection(normalized, summary)) {
        return followActiveRun(runningSessionId, finalRef)
      }
      if (optimisticEntry) markOptimisticEntryFailed(entries.value, clientEntryId)
      patchStreamingAssistantEntry(entries, assistantDraft.id, (entry) => {
        entry.status = 'failed'
        entry.entry_type = 'error'
        entry.content = agentRuntimeErrorMessage(normalized, '页面助手消息发送失败')
        entry.output.runtime_error = normalized.toJSON()
      })
      setRuntimeError(normalized, '页面助手消息发送失败')
      throw normalized
    } finally {
      sending.value = false
      activeStreamController.value = null
    }
  }

  async function stopGeneration() {
    const sessionId = session.value?.id
    const runtimeRunId = activeRuntimeRun.value?.runtime_run_id
    if (!sessionId || !runtimeRunId) return null
    stopping.value = true
    streamEpoch += 1
    activeStreamController.value?.abort()
    try {
      const response = await cancelPageAssistantSession(sessionId, {
        runtime_run_id: runtimeRunId,
        reason: 'user_stopped_generation'
      })
      await Promise.all([refreshSessionSummary(sessionId), loadEntries(sessionId)])
      return response?.data || response
    } catch (err) {
      setRuntimeError(err, '页面助手停止生成失败')
      throw err
    } finally {
      stopping.value = false
    }
  }

  async function continueAction(actionOrId, decision, decisionPayload = {}) {
    const agentAction = resolveAgentActionRef(
      actionOrId,
      pendingActions.value,
      (id) => findActionById(entries.value, id)
    )
    const id = firstString(agentAction.id, agentAction.action_id, actionIdFromRef(actionOrId))
    if (!id || isActionDecisionPending(id)) return null
    const runtimeDecision = ['approve_once', 'approve_session', 'reject', 'steer'].includes(decision) ? decision : 'approve_once'
    if (isPiApprovalAction(agentAction)) {
      return continuePiApproval(agentAction, runtimeDecision, decisionPayload)
    }
    setActionDecisionPending(id, runtimeDecision, true)
    const semanticKey = actionSemanticKey(agentAction)
    const decisionStatus = runtimeDecision === 'reject' ? 'rejected' : 'approved'
    entries.value.push(createActionDecisionEntry(agentAction, runtimeDecision))
    pendingActions.value = pendingActions.value.filter((item) =>
      String(item.id) !== String(id) && actionSemanticKey(item) !== semanticKey
    )
    markActionStatus(entries.value, id, decisionStatus)

    const assistantDraft = createStreamingAssistantEntry(nextClientEntryId({}, `act-${runtimeDecision}-${id}`))
    appendEntryIfMissing(entries.value, assistantDraft)
    const finalRef = ref(null)
    sending.value = true
    clearRuntimeError()
    phaseTimings.value = {}
    try {
      activeStreamController.value = new AbortController()
      await streamPageAssistantActionDecision(id, runtimeDecision, ({ event, data }) => {
        handleStreamEvent(assistantDraft.id, event, data, finalRef)
      }, { signal: activeStreamController.value.signal })
      return finalRef.value
    } catch (err) {
      patchStreamingAssistantEntry(entries, assistantDraft.id, (entry) => {
        entry.status = 'failed'
        entry.entry_type = 'error'
        entry.content = agentRuntimeErrorMessage(err, '动作决策后继续生成失败')
      })
      setRuntimeError(err, '动作决策后继续生成失败')
      throw err
    } finally {
      sending.value = false
      activeStreamController.value = null
      setActionDecisionPending(id, runtimeDecision, false)
    }
  }

  async function continuePiApproval(agentAction, decision, decisionPayload = {}) {
    const runtimeRunId = agentAction.runtime_run_id
    if (!runtimeRunId) return null
    const id = agentAction.id
    if (isActionDecisionPending(id)) return null
    setActionDecisionPending(id, decision, true)
    const existingAssistantEntry = findAssistantEntryForRuntimeRun(entries.value, runtimeRunId)
    const createdAssistantEntry = createStreamingAssistantEntry(nextClientEntryId({}, `pi-${decision}-${id}`))
    const assistantDraftId = existingAssistantEntry?.id || createdAssistantEntry.id
    if (!existingAssistantEntry) {
      createdAssistantEntry.runtime_run_id = runtimeRunId
      appendEntryIfMissing(entries.value, createdAssistantEntry)
    }
    const finalRef = ref(null)
    sending.value = true
    clearRuntimeError()
    try {
      activeStreamController.value = new AbortController()
      await streamPageAssistantPiApproval(runtimeRunId, decision, ({ event, data }) => {
        handleStreamEvent(assistantDraftId, event, data, finalRef)
      }, {
        signal: activeStreamController.value.signal,
        clientDecisionId: `pi-${id}-${decision}`,
        approval: piApprovalRequestPayload(agentAction),
        steerText: decisionPayload.steerText || ''
      })
      if (session.value?.id) await loadEntries(session.value.id)
      pendingActions.value = derivePendingActions(entries.value)
      return finalRef.value
    } catch (err) {
      patchStreamingAssistantEntry(entries, assistantDraftId, (entry) => {
        entry.status = 'failed'
        entry.entry_type = 'error'
        entry.content = agentRuntimeErrorMessage(err, 'Pi 工具审批失败')
      })
      setRuntimeError(err, 'Pi 工具审批失败')
      throw err
    } finally {
      sending.value = false
      activeStreamController.value = null
      setActionDecisionPending(id, decision, false)
    }
  }

  async function switchSessionModel(option, thinkingLevel = currentSessionModel.value.thinking_level) {
    if (!session.value?.id || !option?.provider || !option?.binding || switchingModel.value) return null
    if (!canSwitchSessionModel.value) {
      throw new Error('Agent 正在运行或等待审批，停止、完成或处理审批后才能切换模型')
    }
    switchingModel.value = true
    clearRuntimeError()
    try {
      const modelOverride = buildSessionModelOverride(option.provider, option.binding, thinkingLevel)
      const response = await updatePageAssistantSessionModel(session.value.id, modelOverride)
      const data = response?.data || response
      session.value = data?.session || {
        ...session.value,
        model_override: modelOverride,
        updated_at: new Date().toISOString()
      }
      return data
    } catch (err) {
      setRuntimeError(err, '页面助手模型切换失败')
      throw err
    } finally {
      switchingModel.value = false
    }
  }

  function approveAction(actionOrId) {
    return continueAction(actionOrId, 'approve_once')
  }

  function approveActionForSession(actionOrId) {
    return continueAction(actionOrId, 'approve_session')
  }

  function rejectAction(actionOrId) {
    return continueAction(actionOrId, 'reject')
  }

  function steerAction(actionOrId, instruction) {
    return continueAction(actionOrId, 'steer', { steerText: String(instruction || '').trim() })
  }

  function reset() {
    streamEpoch += 1
    session.value = null
    assistantProfile.value = null
    entries.value = []
    clearRuntimeError()
    phaseTimings.value = {}
    pendingActions.value = []
    pendingActionDecisionKeys.value = new Set()
    activeStreamController.value?.abort()
    activeStreamController.value = null
    queueContextRef.value = {}
    sessionController.reset('idle')
  }

  return {
    session,
    assistantProfile,
    entries,
    loading,
    sending,
    error,
    runtimeError,
    phaseTimings,
    pendingActions,
    pendingActionDecisionKeys,
    refreshingRuntimeRunIds,
    aiProviders,
    modelBindingsByProvider,
    switchingModel,
    queueItems,
    queueLoading,
    queueMode,
    hasSession,
    activeRuntimeRun,
    hasActiveRun,
    currentSessionModel,
    canSwitchSessionModel,
    modelSwitchOptions,
    isActionDecisionPending,
    isRuntimeEventsRefreshing,
    resolvePageAssistantProfile,
    ensureCurrentSession,
    loadEntries,
    loadQueueItems,
    refreshRuntimeEvents,
    loadModelCatalog,
    sendMessage,
    cancelQueueItem,
    reorderQueueItems,
    stopGeneration,
    approveAction,
    approveActionForSession,
    rejectAction,
    steerAction,
    switchSessionModel,
    reset
  }
})
