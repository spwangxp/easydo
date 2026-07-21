import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  archiveAgentChatboxSession,
  cancelAgentChatboxSession,
  continueAgentChatboxSessionStream,
  createAgentChatboxSession,
  getAgentChatboxSession,
  listAgentChatboxEntries,
  listAgentChatboxRunEvents,
  listAgentChatboxProfiles,
  listAgentChatboxSessions,
  sendAgentChatboxMessageStream,
  streamAgentChatboxActionDecision,
  streamAgentChatboxPiApproval,
  streamAgentChatboxRunEvents,
  updateAgentChatboxSessionModel
} from '@/api/agentChatbox'
import { getAIModelBindings, getAIProviders } from '@/api/store'
import {
  isPersistentRuntimeEvent,
  isProcessRuntimeEvent,
  mergeRuntimeEvents,
  normalizeRuntimeEvent,
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
  patchConversationEntry as patchEntry,
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
  latestRuntimeModelFromEvents,
  mergeRuntimeModelSnapshots,
  mergeReplayEventsIntoEntries,
  normalizeAction,
  resolveProviderDisplayName,
  runtimeModelSnapshotFromEvent,
  runtimeEventsFromReplayResponse
} from '@/stores/agentChatboxState'

const agentChatboxClientIds = createRuntimeClientIdFactory('acb')

function normalizeEntries(payload) {
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload)) return payload
  return []
}

function extractArray(payload) {
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload)) return payload
  return []
}

function sleep(ms) {
  return new Promise((resolve) => {
    globalThis.setTimeout(resolve, ms)
  })
}

function normalizeSendAttachments(value) {
  if (!Array.isArray(value)) return []
  return value.filter((item) => item && (typeof item === 'object' || typeof item === 'string'))
}

function createOptimisticUserEntry(content, clientEntryId, attachments = []) {
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
    input: normalizedAttachments.length > 0 ? { attachments: normalizedAttachments } : {},
    output: {},
    created_at: now,
    updated_at: now
  }
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

function appendRuntimeEvent(entriesRef, entryId, event, data) {
  patchEntry(entriesRef, entryId, (entry) => {
    const events = Array.isArray(entry.output.runtime_events) ? entry.output.runtime_events : []
    entry.output.runtime_events = mergeRuntimeEvents(events, [
      normalizeRuntimeEvent(event, data)
    ])
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

function runtimeEventsFromEntries(entryList = []) {
  return entryList.flatMap((entry) => Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : [])
}

function mergeLatestRuntimeModel(latestRuntimeModelRef, event) {
  const snapshot = runtimeModelSnapshotFromEvent(event)
  if (Object.keys(snapshot).length > 0) {
    latestRuntimeModelRef.value = mergeRuntimeModelSnapshots(latestRuntimeModelRef.value, snapshot)
  }
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
    const response = await listAgentChatboxRunEvents(runtimeRunId)
    const replayEvents = runtimeEventsFromReplayResponse(response)
    if (replayEvents.length === 0) continue
    replayedEvents.push(...replayEvents)
    mergeReplayEventsIntoEntries(entryList, runtimeRunId, replayEvents)
  }
  return replayedEvents
}

function markOptimisticFailed(entryList, clientEntryId) {
  const entry = entryList.find((item) => item.id === clientEntryId || item.idempotency_key === clientEntryId)
  if (entry) {
    entry.status = 'failed'
    entry.updated_at = new Date().toISOString()
  }
}

function profileVersionKey(session) {
  return session?.agent_profile_version_key || session?.agent_profile_version_id || 'latest'
}

function findAssistantEntryForRuntimeRun(entryList, runtimeRunId) {
  return entryList.find((entry) =>
    String(entry?.runtime_run_id || '') === String(runtimeRunId) && entry?.role === 'assistant'
  ) || null
}

function findLatestAssistantRuntimeRunId(entryList = []) {
  for (let index = entryList.length - 1; index >= 0; index -= 1) {
    const entry = entryList[index]
    if (entry?.role !== 'assistant') continue
    const runtimeRunId = firstString(entry.runtime_run_id, entry.output?.runtime_run_id)
    if (runtimeRunId) return runtimeRunId
  }
  return ''
}

export const useAgentChatboxStore = defineStore('agentChatbox', () => {
  const profiles = ref([])
  const session = ref(null)
  const sessions = ref([])
  const entries = ref([])
  const loading = ref(false)
  const sending = ref(false)
  const stopping = ref(false)
  const reconnectAttempt = ref(0)
  const error = ref('')
  const runtimeError = ref(null)
  const phaseTimings = ref({})
  const pendingActions = ref([])
  const pendingActionDecisionKeys = ref(new Set())
  const activeStreamController = ref(null)
  const aiProviders = ref([])
  const modelBindingsByProvider = ref({})
  const switchingModel = ref(false)
  const latestRuntimeModel = ref({})
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
  const currentProfile = computed(() => {
    const profileId = session.value?.agent_profile_id
    return profiles.value.find((profile) => String(profile.id) === String(profileId)) || null
  })
  const sessionModelOverride = computed(() => asRecord(session.value?.model_override))
  const providerDisplayCandidates = computed(() => [
    ...aiProviders.value,
    asRecord(sessionModelOverride.value.provider),
    asRecord(currentProfile.value?.provider)
  ].filter((provider) => Object.keys(provider).length > 0))
  const currentSessionModel = computed(() => {
    const override = sessionModelOverride.value
    const profile = currentProfile.value || {}
    const runtimeModel = latestRuntimeModel.value
    const hasRuntimeModel = Object.keys(asRecord(runtimeModel)).length > 0
    const provider = asRecord(hasRuntimeModel ? runtimeModel : (override.provider || profile.provider))
    const model = asRecord(hasRuntimeModel ? runtimeModel : (override.model || profile.model))
    const inference = asRecord(hasRuntimeModel ? (runtimeModel.inference || runtimeModel) : (override.inference || profile.inference))
    return {
      provider: resolveProviderDisplayName(provider, providerDisplayCandidates.value),
      model: firstString(model.provider_model_key, model.model, model.name, model.id, model.model_id, '-'),
      thinking_level: normalizeThinkingLevel(firstString(inference.thinking_level, inference.reasoning, inference.reasoning_effort, 'medium'))
    }
  })
  const canSwitchSessionModel = computed(() => Boolean(session.value?.id) && !sending.value && !hasActiveRun.value && !loading.value)
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

  function nextClientEntryId(purpose = 'msg') {
    return agentChatboxClientIds.nextClientEntryId({
      workspace_id: session.value?.workspace_id || currentProfile.value?.workspace_id,
      session_id: session.value?.id || 'new'
    }, purpose)
  }

  const sessionController = createRuntimeSessionController({
    getSessionId: () => session.value?.id,
    getContextRef: () => ({}),
    setRuntimeError,
    clearRuntimeError,
    nextClientId: nextClientEntryId,
    patchEntry,
    entriesRef: entries,
    pendingActionsRef: pendingActions,
    appendRuntimeEvent,
    onRuntimeEvent: (runtimeEvent) => mergeLatestRuntimeModel(latestRuntimeModel, runtimeEvent),
    initialConnectionState: 'terminal'
  })
  const queueController = sessionController.queueController
  const queueItems = sessionController.queueItems
  const queueMode = sessionController.queueMode
  const queueLoading = sessionController.queueLoading
  const loadQueueItems = sessionController.loadQueueItems
  const cancelQueueItem = sessionController.cancelQueueItem
  const reorderQueueItems = sessionController.reorderQueueItems
  const lastEventIdByRun = sessionController.lastEventIdByRun
  const connectionState = sessionController.connectionState
  const refreshingRuntimeRunIds = sessionController.refreshingRuntimeRunIds
  const setRuntimeEventsRefreshing = sessionController.setRuntimeEventsRefreshing
  const isRuntimeEventsRefreshing = sessionController.isRuntimeEventsRefreshing

  async function loadProfiles() {
    const response = await listAgentChatboxProfiles()
    profiles.value = extractArray(response)
    return profiles.value
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

  async function loadSessions(params = {}) {
    const response = await listAgentChatboxSessions(params)
    sessions.value = extractArray(response)
    return sessions.value
  }

  async function loadSession(sessionId) {
    if (!sessionId) return null
    loading.value = true
    streamEpoch += 1
    activeStreamController.value?.abort()
    activeStreamController.value = null
    clearRuntimeError()
    latestRuntimeModel.value = {}
    try {
      const [sessionResponse] = await Promise.all([
        getAgentChatboxSession(sessionId),
        loadProfiles().catch(() => [])
      ])
      session.value = sessionResponse?.data || sessionResponse
      await Promise.all([
        loadEntries(session.value?.id),
        loadQueueItems(session.value?.id).catch(() => []),
        loadSessions({
          agent_profile_id: session.value?.agent_profile_id,
          agent_profile_version_id: profileVersionKey(session.value),
          status: 'active'
        }).catch(() => [])
      ])
      if (activeRuntimeRun.value?.runtime_run_id && !sending.value) {
        void followActiveRun(session.value.id).catch(() => null)
      }
      return session.value
    } catch (err) {
      setRuntimeError(err, 'Agent Chatbox 会话加载失败')
      throw err
    } finally {
      loading.value = false
    }
  }

  async function refreshSessionSummary(sessionId = session.value?.id) {
    if (!sessionId) return null
    const response = await getAgentChatboxSession(sessionId)
    const nextSession = response?.data || response
    if (!nextSession?.id) return null

    if (session.value?.id && String(session.value.id) === String(nextSession.id)) {
      session.value = {
        ...session.value,
        ...nextSession
      }
    }

    const index = sessions.value.findIndex((item) => String(item.id) === String(nextSession.id))
    if (index >= 0) {
      sessions.value.splice(index, 1, {
        ...sessions.value[index],
        ...nextSession
      })
    }
    return nextSession
  }

  async function refreshSessionSummaryAfterRun(sessionId = session.value?.id) {
    if (!sessionId) return null
    let latest = null
    for (const delay of [0, 400, 900, 1600, 2500, 4000, 7000, 12000]) {
      if (delay) await sleep(delay)
      if (!session.value?.id || String(session.value.id) !== String(sessionId)) return latest
      latest = await refreshSessionSummary(sessionId).catch(() => null)
      if (latest?.title_source && latest.title_source !== 'fallback') return latest
    }
    return latest
  }

  async function archiveSession(sessionId = session.value?.id) {
    if (!sessionId) return null
    clearRuntimeError()
    try {
      const response = await archiveAgentChatboxSession(sessionId)
      const archived = response?.data || response
      sessions.value = sessions.value.filter((item) => String(item.id) !== String(sessionId))
      if (session.value?.id && String(session.value.id) === String(sessionId)) {
        session.value = archived?.id ? { ...session.value, ...archived } : { ...session.value, status: 'archived' }
      }
      return archived
    } catch (err) {
      setRuntimeError(err, 'Agent Chatbox 会话归档失败')
      throw err
    }
  }

  async function createNewSession(profile = currentProfile.value || session.value) {
    const profileId = profile?.id || profile?.agent_profile_id
    if (!profileId) return null
    loading.value = true
    clearRuntimeError()
    latestRuntimeModel.value = {}
    try {
      const response = await createAgentChatboxSession({
        agent_profile_id: profileId,
        agent_profile_version_id: 'draft'
      })
      session.value = response?.data || response
      entries.value = []
      queueController.reset()
      await loadSessions({
        agent_profile_id: session.value?.agent_profile_id,
        agent_profile_version_id: profileVersionKey(session.value),
        status: 'active'
      }).catch(() => [])
      return session.value
    } catch (err) {
      setRuntimeError(err, 'Agent Chatbox 会话创建失败')
      throw err
    } finally {
      loading.value = false
    }
  }

  async function loadEntries(sessionId = session.value?.id) {
    if (!sessionId) return []
    const response = await listAgentChatboxEntries(sessionId)
    const nextEntries = normalizeEntries(response)
    try {
      const replayedEvents = await replayEventsIntoEntries(nextEntries)
      latestRuntimeModel.value = latestRuntimeModelFromEvents([
        ...runtimeEventsFromEntries(nextEntries),
        ...replayedEvents
      ])
    } catch (err) {
      setRuntimeError(err, 'Agent Chatbox 事件回放失败')
    }
    entries.value = nextEntries
    const nextCursors = { ...lastEventIdByRun.value }
    for (const entry of nextEntries) {
      const runtimeRunId = firstString(entry.runtime_run_id, entry.output?.runtime_run_id)
      if (!runtimeRunId) continue
      const latestEvent = runtimeEventsFromEntries([entry])
        .filter((event) => firstString(event.event_id))
        .sort((left, right) => Number(left.event_seq || left.seq || 0) - Number(right.event_seq || right.seq || 0))
        .at(-1)
      if (latestEvent?.event_id) nextCursors[runtimeRunId] = latestEvent.event_id
    }
    lastEventIdByRun.value = nextCursors
    agentChatboxClientIds.observeExistingCount(entries.value.length)
    pendingActions.value = derivePendingActions(entries.value)
    return entries.value
  }

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
      const response = await listAgentChatboxRunEvents(normalizedRuntimeRunId)
      const replayEvents = runtimeEventsFromReplayResponse(response)
      if (replayEvents.length > 0) {
        mergeReplayEventsIntoEntries(entries.value, normalizedRuntimeRunId, replayEvents)
        latestRuntimeModel.value = mergeRuntimeModelSnapshots(latestRuntimeModel.value, latestRuntimeModelFromEvents(replayEvents))
        pendingActions.value = derivePendingActions(entries.value)
      }
      return replayEvents
    } catch (err) {
      setRuntimeError(err, 'Agent Chatbox 事件回放失败')
      throw err
    } finally {
      setRuntimeEventsRefreshing(normalizedRuntimeRunId, false)
    }
  }

  function handleStreamEvent(assistantDraftId, event, data, finalRef) {
    if (event === 'error' || event === 'stream_protocol_error') {
      const normalized = setRuntimeError(
        data,
        event === 'stream_protocol_error' ? 'Agent Chatbox 收到无法解析的事件' : 'Agent Chatbox 消息发送失败'
      )
      connectionState.value = shouldRetryAgentRuntimeConnection(normalized, session.value) ? 'stale' : 'terminal'
      if (event === 'error') {
        patchEntry(entries, assistantDraftId, (entry) => {
          entry.status = 'failed'
          entry.entry_type = 'error'
          entry.content = `模型调用失败：${error.value}`
          entry.output.runtime_error = normalized.toJSON()
        })
      }
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
    const runtimeRunId = firstString(data?.runtime_run_id, data?.run?.runtime_run_id, data?.entry?.runtime_run_id)
    const eventId = firstString(data?.event_id)
    void queueController.applyRuntimeEvent({ type: event, ...data }).catch(() => null)
    return handleRuntimeEventWithCursor(() => {
      if (isPersistentRuntimeEvent(event) || isProcessRuntimeEvent(event)) {
        appendRuntimeEvent(entries, assistantDraftId, event, data)
      }
      mergeLatestRuntimeModel(latestRuntimeModel, { type: event, payload: data })
      if (event === 'capability.snapshot') {
        patchEntry(entries, assistantDraftId, (entry) => {
          entry.output.capabilities = data || {}
        })
      }
      if (event === 'skill.available') {
        patchEntry(entries, assistantDraftId, (entry) => {
          const skills = Array.isArray(entry.output.available_skills) ? entry.output.available_skills : []
          entry.output.available_skills = [...skills, data]
        })
      }
      if (event === 'skill.loaded') {
        patchEntry(entries, assistantDraftId, (entry) => {
          const skills = Array.isArray(entry.output.loaded_skills) ? entry.output.loaded_skills : []
          entry.output.loaded_skills = [...skills, data]
        })
      }
      if (event === 'subagent.completed') {
        patchEntry(entries, assistantDraftId, (entry) => {
          const subagents = Array.isArray(entry.output.subagent_results) ? entry.output.subagent_results : []
          entry.output.subagent_results = [...subagents, data]
        })
      }
      if (event === 'action.decision_required') {
        const agentAction = normalizeAction(data)
        if (agentAction) {
          pendingActions.value = uniquePendingActions([
            ...pendingActions.value.filter((item) => String(item.id) !== String(agentAction.id)),
            agentAction
          ])
          patchEntry(entries, assistantDraftId, (entry) => {
            const agentActions = Array.isArray(entry.output.agent_actions) ? entry.output.agent_actions : []
            entry.output.agent_actions = uniquePendingActions([
              ...agentActions.filter((item) => String(item.id) !== String(agentAction.id)),
              agentAction
            ])
          })
        }
      }
      if (event === 'run.step_announced') {
        const stage = data?.stage
        if (stage) {
          phaseTimings.value = {
            ...phaseTimings.value,
            [stage]: data?.elapsed_ms
          }
          patchEntry(entries, assistantDraftId, (entry) => {
            entry.output.timings = { ...(entry.output.timings || {}), [stage]: data?.elapsed_ms }
          })
        }
      }
      if (event === 'reasoning_delta') {
        patchEntry(entries, assistantDraftId, (entry) => {
          entry.output.reasoning = `${entry.output.reasoning || ''}${data?.delta || ''}`
        })
      }
      if (event === 'answer_delta') {
        patchEntry(entries, assistantDraftId, (entry) => {
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
        const assistantRuntimeRunId = firstString(
          finalEntry?.runtime_run_id,
          data?.entry?.runtime_run_id,
          data?.run?.runtime_run_id
        )
        if (finalEntry && assistantRuntimeRunId) {
          finalEntry.runtime_run_id = finalEntry.runtime_run_id || assistantRuntimeRunId
          finalEntry.output = {
            ...(finalEntry.output || {}),
            runtime_run_id: finalEntry.output?.runtime_run_id || assistantRuntimeRunId
          }
        }
        finalRef.value = { ...data, entry: finalEntry }
        replaceOptimisticEntry(entries.value, assistantDraftId, finalEntry)
        pendingActions.value = derivePendingActions(entries.value)
        if (!['streaming', 'running', 'queued', 'awaiting_decision', 'awaiting_input'].includes(String(finalEntry?.status || ''))) {
          connectionState.value = 'terminal'
        }
      }
      if (event === 'done') {
        phaseTimings.value = data?.timings || phaseTimings.value
        connectionState.value = 'terminal'
      }
    }, () => {
      if (runtimeRunId && eventId) {
        sessionController.rememberEventCursor(runtimeRunId, eventId)
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
        connectionState.value = attempt === 0 ? 'connecting' : 'reconnecting'
        reconnectAttempt.value = attempt
        summary = await refreshSessionSummary(sessionId)
        if (!summary?.active_run?.runtime_run_id) {
          await loadEntries(sessionId)
          connectionState.value = 'terminal'
          reconnectAttempt.value = 0
          void refreshSessionSummaryAfterRun(sessionId)
          return finalRef.value
        }
        const runtimeRunId = summary.active_run.runtime_run_id
        await loadEntries(sessionId)
        const assistantEntry = findAssistantEntryForRuntimeRun(entries.value, runtimeRunId)
        if (!assistantEntry) throw new Error('Active Run Assistant Entry is unavailable')
        const controller = new AbortController()
        activeStreamController.value = controller
        let streamError = null
        await streamAgentChatboxRunEvents(
          runtimeRunId,
          lastEventIdByRun.value[runtimeRunId] || '',
          ({ event, data }) => {
            if (event === 'error' || event === 'stream_protocol_error') {
              streamError = normalizeAgentRuntimeError(data, 'Agent Chatbox 事件续传失败')
              return
            }
            handleStreamEvent(assistantEntry.id, event, data, finalRef)
          },
          { signal: controller.signal }
        )
        if (streamError) throw streamError
        await Promise.all([refreshSessionSummary(sessionId), loadEntries(sessionId)])
        if (!activeRuntimeRun.value?.runtime_run_id) {
          connectionState.value = 'terminal'
          reconnectAttempt.value = 0
          void refreshSessionSummaryAfterRun(sessionId)
          return finalRef.value
        }
      } catch (err) {
        if (err?.name === 'AbortError' && (epoch !== streamEpoch || stopping.value)) return finalRef.value
        const normalized = setRuntimeError(err, 'Agent Chatbox 实时连接已中断')
        summary = await refreshSessionSummary(sessionId).catch(() => (
          summary
          || (session.value?.active_run || activeRuntimeRun.value
            ? { active_run: session.value?.active_run || activeRuntimeRun.value }
            : null)
        ))
        const retry = shouldRetryAgentRuntimeConnection(normalized, summary)
        if (!retry) {
          connectionState.value = 'terminal'
          reconnectAttempt.value = 0
          await loadEntries(sessionId).catch(() => null)
          return finalRef.value
        }
        connectionState.value = 'stale'
        attempt += 1
        reconnectAttempt.value = attempt
        await sleep(Math.min(5000, 250 * (2 ** Math.min(attempt - 1, 4))))
      }
    }
    return finalRef.value
  }

  async function enqueueQueueMessage(content, mode = queueMode.value, options = {}) {
    if (!session.value?.id) return null
    sending.value = true
    try {
      const response = await queueController.enqueue(content, mode, options)
      if (mode === 'stop_and_run' || mode === 'follow_up') {
        await refreshSessionSummary(session.value.id).catch(() => null)
      }
      return response
    } finally {
      sending.value = false
    }
  }

  async function sendMessage(content, options = {}) {
    if (!session.value?.id) return null
    const mode = String(options.mode || queueMode.value || 'follow_up')
    const attachments = normalizeSendAttachments(options.attachments)
    if (hasActiveRun.value) {
      return enqueueQueueMessage(content, mode, { attachments })
    }
    if (mode === 'steer') {
      const normalized = normalizeAgentRuntimeError(
        { code: 'active_run_required', message: 'steer 需要当前有进行中的 Run' },
        '队列消息发送失败'
      )
      setRuntimeError(normalized, '队列消息发送失败')
      throw normalized
    }
    const epoch = ++streamEpoch
    const clientEntryId = nextClientEntryId('msg')
    const optimisticEntry = createOptimisticUserEntry(content, clientEntryId, attachments)
    const assistantDraft = createStreamingAssistantEntry(clientEntryId)
    entries.value.push(optimisticEntry)
    appendEntryIfMissing(entries.value, assistantDraft)
    sending.value = true
    clearRuntimeError()
    phaseTimings.value = {}
    const finalRef = ref(null)
    const runningSessionId = session.value.id
    try {
      connectionState.value = 'connecting'
      activeStreamController.value = new AbortController()
      void refreshSessionSummaryAfterRun(runningSessionId)
      await sendAgentChatboxMessageStream(session.value.id, {
        content,
        client_entry_id: clientEntryId,
        runtime_engine: 'pi',
        ...(attachments.length > 0 ? { attachments } : {})
      }, ({ event, data }) => {
        if (event === 'user_entry') {
          replaceOptimisticEntry(entries.value, clientEntryId, data?.entry)
          return
        }
        handleStreamEvent(assistantDraft.id, event, data, finalRef)
      }, { signal: activeStreamController.value.signal })
      const summaryAfterInitialStream = await refreshSessionSummary(runningSessionId)
      if (summaryAfterInitialStream?.active_run?.runtime_run_id) {
        return followActiveRun(runningSessionId, finalRef)
      }
      await loadEntries(runningSessionId)
      await loadQueueItems(runningSessionId)
      connectionState.value = 'terminal'
      void refreshSessionSummaryAfterRun(runningSessionId)
      return finalRef.value
    } catch (err) {
      let normalized
      let summary
      if (err?.name === 'AbortError') {
        const resolution = await resolveAgentRuntimeFollowError(err, {
          followState: {
            epoch,
            currentEpoch: streamEpoch,
            sessionId: runningSessionId,
            currentSessionId: session.value?.id,
            stopping: stopping.value
          },
          fallbackMessage: 'Agent Chatbox 消息发送连接已中断',
          previousSummary: null,
          refreshSummary: () => refreshSessionSummary(runningSessionId)
        })
        if (resolution.silent) return finalRef.value
        normalized = resolution.error
        summary = resolution.summary
      } else {
        normalized = normalizeAgentRuntimeError(err, 'Agent Chatbox 消息发送失败')
        summary = await refreshSessionSummary(runningSessionId).catch(() => (
          session.value?.active_run || activeRuntimeRun.value
            ? { active_run: session.value?.active_run || activeRuntimeRun.value }
            : null
        ))
      }
      if (shouldRetryAgentRuntimeConnection(normalized, summary)) {
        return followActiveRun(runningSessionId, finalRef)
      }
      markOptimisticFailed(entries.value, clientEntryId)
      patchEntry(entries, assistantDraft.id, (entry) => {
        entry.status = 'failed'
        entry.entry_type = 'error'
        entry.content = agentRuntimeErrorMessage(normalized, 'Agent Chatbox 消息发送失败')
        entry.output.runtime_error = normalized.toJSON()
      })
      setRuntimeError(normalized, 'Agent Chatbox 消息发送失败')
      throw normalized
    } finally {
      sending.value = false
      activeStreamController.value = null
    }
  }

  async function continueAction(actionOrId, decision, decisionPayload = {}) {
    if (!session.value?.id) return null
    const agentAction = resolveAgentActionRef(actionOrId, pendingActions.value)
    const id = firstString(agentAction.id, agentAction.action_id, actionIdFromRef(actionOrId))
    if (!id || isActionDecisionPending(id)) return null
    const runtimeDecision = ['approve_once', 'approve_session', 'reject', 'steer'].includes(decision) ? decision : 'approve_once'
    if (isPiApprovalAction(agentAction)) {
      return continuePiApproval(agentAction, runtimeDecision, decisionPayload)
    }
    setActionDecisionPending(id, runtimeDecision, true)
    const semanticKey = actionSemanticKey(agentAction)
    const assistantDraft = createStreamingAssistantEntry(nextClientEntryId(`act-${runtimeDecision}-${id}`))
    entries.value.push(assistantDraft)
    const finalRef = ref(null)
    sending.value = true
    try {
      activeStreamController.value = new AbortController()
      const runningSessionId = session.value.id
      await streamAgentChatboxActionDecision(id, runtimeDecision, ({ event, data }) => {
        handleStreamEvent(assistantDraft.id, event, data, finalRef)
      }, { signal: activeStreamController.value.signal })
      pendingActions.value = pendingActions.value.filter((item) =>
        String(item.id) !== String(id) && actionSemanticKey(item) !== semanticKey
      )
      void refreshSessionSummaryAfterRun(runningSessionId)
      return finalRef.value
    } catch (err) {
      patchEntry(entries, assistantDraft.id, (entry) => {
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
    const runtimeRunId = firstString(
      agentAction.runtime_run_id,
      findLatestAssistantRuntimeRunId(entries.value),
      session.value?.runtime_run_id
    )
    if (!runtimeRunId) {
      error.value = '当前审批缺少 runtime_run_id，请刷新会话后重试'
      return null
    }
    const id = agentAction.id
    if (isActionDecisionPending(id)) return null
    setActionDecisionPending(id, decision, true)
    const existingAssistantEntry = findAssistantEntryForRuntimeRun(entries.value, runtimeRunId)
    const createdAssistantEntry = createStreamingAssistantEntry(nextClientEntryId(`pi-${decision}-${id}`))
    const assistantDraftId = existingAssistantEntry?.id || createdAssistantEntry.id
    if (!existingAssistantEntry) {
      createdAssistantEntry.runtime_run_id = runtimeRunId
      appendEntryIfMissing(entries.value, createdAssistantEntry)
    } else if (!existingAssistantEntry.runtime_run_id) {
      existingAssistantEntry.runtime_run_id = runtimeRunId
    }
    agentAction.runtime_run_id = runtimeRunId
    const finalRef = ref(null)
    sending.value = true
    try {
      activeStreamController.value = new AbortController()
      await streamAgentChatboxPiApproval(runtimeRunId, decision, ({ event, data }) => {
        handleStreamEvent(assistantDraftId, event, data, finalRef)
      }, {
        signal: activeStreamController.value.signal,
        clientDecisionId: `pi-${id}-${decision}`,
        approval: piApprovalRequestPayload(agentAction),
        steerText: decisionPayload.steerText || ''
      })
      await loadEntries(session.value.id)
      pendingActions.value = derivePendingActions(entries.value)
      return finalRef.value
    } catch (err) {
      patchEntry(entries, assistantDraftId, (entry) => {
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

  async function continueSession(content = '继续') {
    if (!session.value?.id) return null
    const clientEntryId = nextClientEntryId('continue')
    const assistantDraft = createStreamingAssistantEntry(clientEntryId)
    entries.value.push(assistantDraft)
    const finalRef = ref(null)
    sending.value = true
    try {
      activeStreamController.value = new AbortController()
      const runningSessionId = session.value.id
      await continueAgentChatboxSessionStream(session.value.id, {
        content,
        client_entry_id: clientEntryId,
        runtime_engine: 'pi'
      }, ({ event, data }) => {
        handleStreamEvent(assistantDraft.id, event, data, finalRef)
      }, { signal: activeStreamController.value.signal })
      void refreshSessionSummaryAfterRun(runningSessionId)
      return finalRef.value
    } catch (err) {
      patchEntry(entries, assistantDraft.id, (entry) => {
        entry.status = 'failed'
        entry.entry_type = 'error'
        entry.content = agentRuntimeErrorMessage(err, '继续生成失败')
      })
      setRuntimeError(err, '继续生成失败')
      throw err
    } finally {
      sending.value = false
      activeStreamController.value = null
    }
  }

  async function stopGeneration() {
    const sessionId = session.value?.id
    const runtimeRunId = firstString(
      activeRuntimeRun.value?.runtime_run_id,
      findLatestAssistantRuntimeRunId(entries.value)
    )
    if (!sessionId || !runtimeRunId || stopping.value) return null
    stopping.value = true
    clearRuntimeError()
    try {
      const response = await cancelAgentChatboxSession(sessionId, {
        runtime_run_id: runtimeRunId,
        reason: 'user_stopped_generation'
      })
      activeStreamController.value?.abort()
      await Promise.all([
        refreshSessionSummary(sessionId),
        loadEntries(sessionId)
      ])
      connectionState.value = 'terminal'
      return response?.data || response
    } catch (err) {
      setRuntimeError(err, 'Agent Chatbox 停止生成失败')
      throw err
    } finally {
      stopping.value = false
    }
  }

  function disconnectStream() {
    streamEpoch += 1
    activeStreamController.value?.abort()
    activeStreamController.value = null
    if (activeRuntimeRun.value?.runtime_run_id) connectionState.value = 'stale'
  }

  async function switchSessionModel(option, thinkingLevel = currentSessionModel.value.thinking_level) {
    if (!session.value?.id || !option?.provider || !option?.binding || switchingModel.value) return null
    if (!canSwitchSessionModel.value) {
      throw new Error('Agent 正在运行，停止或等待输入后才能切换模型')
    }
    switchingModel.value = true
    clearRuntimeError()
    try {
      const modelOverride = buildSessionModelOverride(option.provider, option.binding, thinkingLevel)
      const response = await updateAgentChatboxSessionModel(session.value.id, modelOverride)
      const data = response?.data || response
      session.value = data?.session || {
        ...session.value,
        model_override: modelOverride,
        updated_at: new Date().toISOString()
      }
      latestRuntimeModel.value = {}
      const index = sessions.value.findIndex((item) => String(item.id) === String(session.value.id))
      if (index >= 0) sessions.value.splice(index, 1, session.value)
      return data
    } catch (err) {
      setRuntimeError(err, '会话模型切换失败')
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
    sessions.value = []
    entries.value = []
    clearRuntimeError()
    phaseTimings.value = {}
    pendingActions.value = []
    pendingActionDecisionKeys.value = new Set()
    reconnectAttempt.value = 0
    activeStreamController.value?.abort()
    activeStreamController.value = null
    sessionController.reset('terminal')
  }

  return {
    profiles,
    session,
    sessions,
    entries,
    loading,
    sending,
    stopping,
    connectionState,
    reconnectAttempt,
    lastEventIdByRun,
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
    queueMode,
    queueLoading,
    hasSession,
    activeRuntimeRun,
    hasActiveRun,
    currentProfile,
    currentSessionModel,
    canSwitchSessionModel,
    modelSwitchOptions,
    isActionDecisionPending,
    isRuntimeEventsRefreshing,
    loadProfiles,
    loadModelCatalog,
    loadSessions,
    loadSession,
    refreshSessionSummary,
    createNewSession,
    archiveSession,
    loadEntries,
    loadQueueItems,
    refreshRuntimeEvents,
    sendMessage,
    enqueueQueueMessage,
    cancelQueueItem,
    reorderQueueItems,
    continueSession,
    stopGeneration,
    disconnectStream,
    switchSessionModel,
    approveAction,
    approveActionForSession,
    rejectAction,
    steerAction,
    reset
  }
})
