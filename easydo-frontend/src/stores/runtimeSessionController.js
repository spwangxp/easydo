import { ref } from 'vue'
import { firstString } from './agentConversationShared.js'
import { createRuntimeSessionQueueController } from './runtimeSessionQueueController.js'
import {
  applyRuntimeStreamEventToAssistantEntry,
  shouldAppendRuntimeEvent
} from './runtimeSessionStreamProjection.js'

export function createRuntimeSessionController({
  getSessionId,
  getContextRef,
  setRuntimeError,
  clearRuntimeError,
  nextClientId,
  patchEntry,
  entriesRef,
  pendingActionsRef,
  appendRuntimeEvent,
  onConnectionLive,
  onRuntimeEvent,
  initialConnectionState = 'idle',
  queueApi
}) {
  const lastEventIdByRun = ref({})
  const refreshingRuntimeRunIds = ref(new Set())
  const connectionState = ref(initialConnectionState)

  const queueController = createRuntimeSessionQueueController({
    getSessionId,
    getContextRef,
    setRuntimeError,
    clearRuntimeError,
    nextClientId,
    api: queueApi
  })

  function rememberEventCursor(runtimeRunId, eventId) {
    if (!runtimeRunId || !eventId) return
    lastEventIdByRun.value = {
      ...lastEventIdByRun.value,
      [runtimeRunId]: eventId
    }
  }

  function setRuntimeEventsRefreshing(runtimeRunId, refreshing) {
    if (!runtimeRunId) return
    const next = new Set(refreshingRuntimeRunIds.value)
    if (refreshing) next.add(String(runtimeRunId))
    else next.delete(String(runtimeRunId))
    refreshingRuntimeRunIds.value = next
  }

  function isRuntimeEventsRefreshing(runtimeRunId) {
    return refreshingRuntimeRunIds.value.has(String(runtimeRunId || '').trim())
  }

  function handleRuntimeEventPayload(assistantDraftId, data = {}) {
    const runtimeEvent = data?.event || data
    const runtimeEventName = runtimeEvent?.type || runtimeEvent?.event
    const runtimeRunId = firstString(
      runtimeEvent?.runtime_run_id,
      runtimeEvent?.run?.runtime_run_id,
      data?.runtime_run_id
    )
    const eventId = firstString(runtimeEvent?.event_id)

    void queueController.applyRuntimeEvent(runtimeEvent).catch(() => null)
    onConnectionLive?.()
    connectionState.value = 'live'
    onRuntimeEvent?.(runtimeEvent)

    if (shouldAppendRuntimeEvent(runtimeEventName)) {
      appendRuntimeEvent?.(entriesRef, assistantDraftId, runtimeEventName, runtimeEvent)
    }

    applyRuntimeStreamEventToAssistantEntry({
      patchEntry,
      entriesRef,
      assistantDraftId,
      runtimeEventName,
      runtimeEvent,
      data,
      onPendingActions: (actions) => {
        if (pendingActionsRef) pendingActionsRef.value = actions
      }
    })

    rememberEventCursor(runtimeRunId, eventId)
  }

  function handleHeartbeat(data = {}) {
    const runtimeRunId = firstString(data?.runtime_run_id)
    if (runtimeRunId && data?.last_event_id) {
      rememberEventCursor(runtimeRunId, data.last_event_id)
    }
    connectionState.value = 'live'
    onConnectionLive?.()
  }

  function reset(nextConnectionState = initialConnectionState) {
    lastEventIdByRun.value = {}
    refreshingRuntimeRunIds.value = new Set()
    connectionState.value = nextConnectionState
    queueController.reset?.()
  }

  return {
    lastEventIdByRun,
    connectionState,
    refreshingRuntimeRunIds,
    queueController,
    queueItems: queueController.items,
    queueLoading: queueController.loading,
    queueMode: queueController.mode,
    loadQueueItems: queueController.load,
    cancelQueueItem: queueController.cancel,
    reorderQueueItems: queueController.reorder,
    enqueueQueueMessage: queueController.enqueue,
    handleRuntimeEventPayload,
    handleHeartbeat,
    rememberEventCursor,
    setRuntimeEventsRefreshing,
    isRuntimeEventsRefreshing,
    reset
  }
}
