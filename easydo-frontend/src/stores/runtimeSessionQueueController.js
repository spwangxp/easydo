import { ref } from 'vue'
import * as runtimeSessionQueueApi from '../api/runtimeSessionQueue.js'

const REFRESH_EVENTS = new Set([
  'session.queue.added',
  'session.queue.claimed',
  'session.queue.cancelled',
  'session.queue.expired',
  'session.queue.failed'
])

const IMMEDIATE_STATUS_BY_EVENT = {
  'session.steer.applied': 'applied',
  'session.follow_up.started': 'consumed'
}

let generatedClientId = 0

function responseData(response) {
  return response?.data || response
}

function responseItems(response) {
  const payload = responseData(response) || {}
  return Array.isArray(payload.items) ? payload.items : []
}

function eventName(event) {
  return event?.type || event?.event || event?.payload?.type || event?.payload?.event || ''
}

function eventQueueItemId(event) {
  return String(
    event?.queue_item_id ||
    event?.queueItemId ||
    event?.queue_item?.queue_item_id ||
    event?.queue_item?.queueItemId ||
    event?.payload?.queue_item_id ||
    event?.payload?.queueItemId ||
    event?.payload?.queue_item?.queue_item_id ||
    event?.payload?.queue_item?.queueItemId ||
    event?.data?.queue_item_id ||
    event?.data?.queueItemId ||
    event?.data?.queue_item?.queue_item_id ||
    event?.data?.queue_item?.queueItemId ||
    ''
  ).trim()
}

function nextInternalClientId() {
  generatedClientId += 1
  return `runtime-queue-${Date.now()}-${generatedClientId}`
}

export function createRuntimeSessionQueueController({
  getSessionId,
  getContextRef,
  setRuntimeError,
  clearRuntimeError,
  nextClientId = nextInternalClientId,
  api = runtimeSessionQueueApi
}) {
  const items = ref([])
  const loading = ref(false)
  const mode = ref('follow_up')
  let loadSequence = 0

  async function load(sessionId = getSessionId()) {
    const requestSequence = ++loadSequence
    if (!sessionId) {
      items.value = []
      loading.value = false
      return []
    }
    loading.value = true
    try {
      const response = await api.listRuntimeSessionQueueItems(sessionId)
      if (requestSequence === loadSequence && String(sessionId) === String(getSessionId() || '')) {
        items.value = responseItems(response)
      }
      return items.value
    } catch (error) {
      if (requestSequence === loadSequence) setRuntimeError(error, '队列加载失败')
      throw error
    } finally {
      if (requestSequence === loadSequence) loading.value = false
    }
  }

  async function enqueue(content, selectedMode = mode.value, options = {}) {
    const sessionId = getSessionId()
    if (!sessionId) return null
    clearRuntimeError()
    try {
      const contextRef = getContextRef?.() || {}
      const data = {
        content,
        mode: selectedMode,
        client_item_id: nextClientId('queue')
      }
      if (Object.keys(contextRef).length > 0) data.context_ref = contextRef
      const attachments = Array.isArray(options.attachments)
        ? options.attachments.filter((item) => item && (typeof item === 'object' || typeof item === 'string'))
        : []
      if (attachments.length > 0) data.attachments = attachments
      const response = await api.enqueueRuntimeSessionQueueItem(sessionId, data)
      await load(sessionId)
      return responseData(response)
    } catch (error) {
      setRuntimeError(error, '队列消息发送失败')
      throw error
    }
  }

  async function cancel(queueItemId) {
    const sessionId = getSessionId()
    if (!sessionId || !queueItemId) return null
    clearRuntimeError()
    try {
      const response = await api.cancelRuntimeSessionQueueItem(sessionId, queueItemId)
      await load(sessionId)
      return responseData(response)
    } catch (error) {
      setRuntimeError(error, '队列消息取消失败')
      throw error
    }
  }

  async function reorder(queueItemIds = []) {
    const sessionId = getSessionId()
    if (!sessionId) return null
    clearRuntimeError()
    try {
      const response = await api.reorderRuntimeSessionQueueItems(sessionId, {
        queue_item_ids: queueItemIds
      })
      await load(sessionId)
      return responseData(response)
    } catch (error) {
      setRuntimeError(error, '队列排序失败')
      throw error
    }
  }

  async function applyRuntimeEvent(event) {
    const name = eventName(event)
    if (REFRESH_EVENTS.has(name)) return load()
    const immediateStatus = IMMEDIATE_STATUS_BY_EVENT[name]
    if (!immediateStatus) return items.value
    const queueItemId = eventQueueItemId(event)
    if (queueItemId) {
      items.value = items.value.map((item) => (
        String(item?.queue_item_id || '') === queueItemId
          ? { ...item, status: immediateStatus }
          : item
      ))
    }
    return load()
  }

  function reset() {
    loadSequence += 1
    items.value = []
    loading.value = false
    mode.value = 'follow_up'
  }

  return {
    items,
    loading,
    mode,
    load,
    enqueue,
    cancel,
    reorder,
    applyRuntimeEvent,
    reset
  }
}
