import test from 'node:test'
import assert from 'node:assert/strict'
import { ref } from 'vue'

import { createRuntimeSessionQueueController } from './runtimeSessionQueueController.js'

function deferred() {
  let resolve
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function createApi(overrides = {}) {
  return {
    listRuntimeSessionQueueItems: async () => ({ items: [] }),
    enqueueRuntimeSessionQueueItem: async () => ({}),
    cancelRuntimeSessionQueueItem: async () => ({}),
    reorderRuntimeSessionQueueItems: async () => ({}),
    ...overrides
  }
}

test('runtime queue controller loads the current session and ignores stale responses', async () => {
  const first = deferred()
  const second = deferred()
  const calls = []
  const api = createApi({
    listRuntimeSessionQueueItems(sessionId) {
      calls.push(sessionId)
      return calls.length === 1 ? first.promise : second.promise
    }
  })
  const sessionId = ref('session-1')
  const controller = createRuntimeSessionQueueController({
    getSessionId: () => sessionId.value,
    getContextRef: () => ({}),
    setRuntimeError: () => {},
    clearRuntimeError: () => {},
    api
  })

  const firstLoad = controller.load()
  sessionId.value = 'session-2'
  const secondLoad = controller.load()
  second.resolve({ data: { items: [{ queue_item_id: 'new' }] } })
  await secondLoad
  first.resolve({ data: { items: [{ queue_item_id: 'old' }] } })
  await firstLoad

  assert.deepEqual(calls, ['session-1', 'session-2'])
  assert.deepEqual(controller.items.value, [{ queue_item_id: 'new' }])
  assert.equal(controller.loading.value, false)
})

test('runtime queue controller enqueues one client item id and current context per action', async () => {
  const requests = []
  let nextId = 0
  const api = createApi({
    enqueueRuntimeSessionQueueItem: async (sessionId, data) => {
      requests.push({ sessionId, data })
      return { data: { queued: true } }
    }
  })
  const controller = createRuntimeSessionQueueController({
    getSessionId: () => 'session-1',
    getContextRef: () => ({ route_path: '/pipelines/1' }),
    setRuntimeError: () => {},
    clearRuntimeError: () => {},
    nextClientId: () => `queue-${++nextId}`,
    api
  })

  const result = await controller.enqueue('first', 'follow_up')
  await controller.enqueue('second', 'steer')
  await controller.enqueue('retry with file', 'follow_up', {
    attachments: [{ name: 'note.md' }]
  })

  assert.deepEqual(result, { queued: true })
  assert.deepEqual(requests, [
    {
      sessionId: 'session-1',
      data: {
        content: 'first',
        mode: 'follow_up',
        client_item_id: 'queue-1',
        context_ref: { route_path: '/pipelines/1' }
      }
    },
    {
      sessionId: 'session-1',
      data: {
        content: 'second',
        mode: 'steer',
        client_item_id: 'queue-2',
        context_ref: { route_path: '/pipelines/1' }
      }
    },
    {
      sessionId: 'session-1',
      data: {
        content: 'retry with file',
        mode: 'follow_up',
        client_item_id: 'queue-3',
        context_ref: { route_path: '/pipelines/1' },
        attachments: [{ name: 'note.md' }]
      }
    }
  ])
})

test('runtime queue controller refreshes stable queue events and patches applied rows immediately', async () => {
  let loads = 0
  const api = createApi({
    listRuntimeSessionQueueItems: async () => {
      loads += 1
      return { data: { items: [{ queue_item_id: 'queue-1', status: 'pending' }] } }
    }
  })
  const controller = createRuntimeSessionQueueController({
    getSessionId: () => 'session-1',
    getContextRef: () => ({}),
    setRuntimeError: () => {},
    clearRuntimeError: () => {},
    api
  })
  controller.items.value = [{ queue_item_id: 'queue-1', status: 'claimed' }]

  for (const eventName of [
    'session.queue.added',
    'session.queue.claimed',
    'session.queue.cancelled',
    'session.queue.expired',
    'session.queue.failed'
  ]) {
    await controller.applyRuntimeEvent({ type: eventName, queue_item_id: 'queue-1' })
  }

  const steerRefresh = controller.applyRuntimeEvent({
    type: 'session.steer.applied',
    queue_item: { queue_item_id: 'queue-1', status: 'applied' }
  })
  assert.equal(controller.items.value[0].status, 'applied')
  await steerRefresh

  controller.items.value = [{ queue_item_id: 'queue-1', status: 'claimed' }]
  const followUpRefresh = controller.applyRuntimeEvent({
    event: 'session.follow_up.started',
    payload: { queue_item: { queue_item_id: 'queue-1', status: 'consumed' } }
  })
  assert.equal(controller.items.value[0].status, 'consumed')
  await followUpRefresh

  assert.equal(loads, 7)
})

test('runtime queue controller cancels, reorders, reports errors, and resets state', async () => {
  const calls = []
  const errors = []
  let loads = 0
  const api = createApi({
    cancelRuntimeSessionQueueItem: async (sessionId, queueItemId) => {
      calls.push(['cancel', sessionId, queueItemId])
      return { data: { cancelled: true } }
    },
    reorderRuntimeSessionQueueItems: async (sessionId, data) => {
      calls.push(['reorder', sessionId, data])
      return { data: { reordered: true } }
    },
    listRuntimeSessionQueueItems: async () => {
      loads += 1
      if (loads === 3) throw new Error('load failed')
      return { data: { items: [] } }
    }
  })
  const controller = createRuntimeSessionQueueController({
    getSessionId: () => 'session-1',
    getContextRef: () => ({}),
    setRuntimeError: (error, fallback) => errors.push([error.message, fallback]),
    clearRuntimeError: () => {},
    api
  })

  assert.deepEqual(await controller.cancel('queue-1'), { cancelled: true })
  assert.deepEqual(await controller.reorder(['queue-2', 'queue-1']), { reordered: true })
  await assert.rejects(controller.load(), /load failed/)
  controller.items.value = [{ queue_item_id: 'queue-1' }]
  controller.mode.value = 'steer'
  controller.reset()

  assert.deepEqual(calls, [
    ['cancel', 'session-1', 'queue-1'],
    ['reorder', 'session-1', { queue_item_ids: ['queue-2', 'queue-1'] }]
  ])
  assert.deepEqual(errors, [['load failed', '队列加载失败']])
  assert.deepEqual(controller.items.value, [])
  assert.equal(controller.mode.value, 'follow_up')
})
