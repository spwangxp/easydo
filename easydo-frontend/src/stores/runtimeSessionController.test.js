import test from 'node:test'
import assert from 'node:assert/strict'
import { ref } from 'vue'

import { createRuntimeSessionController } from './runtimeSessionController.js'

test('runtime session controller owns cursor heartbeat and runtime_event projection', () => {
  const entries = ref([{
    id: 'assistant-1',
    role: 'assistant',
    status: 'streaming',
    content: '',
    output: { runtime_events: [] }
  }])
  const pendingActions = ref([])
  const appended = []
  const liveSignals = []
  const runtimeSignals = []

  const controller = createRuntimeSessionController({
    getSessionId: () => 'session-1',
    getContextRef: () => ({}),
    setRuntimeError: () => {},
    clearRuntimeError: () => {},
    nextClientId: () => 'queue-1',
    patchEntry: (entriesRef, entryId, mutator) => {
      const entry = entriesRef.value.find((item) => String(item.id) === String(entryId))
      if (entry) mutator(entry)
    },
    entriesRef: entries,
    pendingActionsRef: pendingActions,
    appendRuntimeEvent: (_entriesRef, assistantDraftId, eventName, runtimeEvent) => {
      appended.push({ assistantDraftId, eventName, eventId: runtimeEvent.event_id })
    },
    onConnectionLive: () => liveSignals.push('live'),
    onRuntimeEvent: (event) => runtimeSignals.push(event.type),
    initialConnectionState: 'terminal',
    queueApi: {
      listRuntimeSessionQueueItems: async () => ({ items: [] }),
      enqueueRuntimeSessionQueueItem: async () => ({}),
      cancelRuntimeSessionQueueItem: async () => ({}),
      reorderRuntimeSessionQueueItems: async () => ({})
    }
  })

  assert.equal(controller.connectionState.value, 'terminal')

  controller.handleHeartbeat({
    runtime_run_id: 'run-1',
    last_event_id: 'evt-hb-1'
  })
  assert.equal(controller.connectionState.value, 'live')
  assert.equal(controller.lastEventIdByRun.value['run-1'], 'evt-hb-1')
  assert.deepEqual(liveSignals, ['live'])

  controller.handleRuntimeEventPayload('assistant-1', {
    event: {
      type: 'session.step.started',
      event_id: 'evt-1',
      runtime_run_id: 'run-1',
      timestamp: '2026-07-17T00:00:00.000Z'
    }
  })

  assert.equal(controller.lastEventIdByRun.value['run-1'], 'evt-1')
  assert.deepEqual(appended, [{
    assistantDraftId: 'assistant-1',
    eventName: 'session.step.started',
    eventId: 'evt-1'
  }])
  assert.deepEqual(runtimeSignals, ['session.step.started'])
  assert.ok(liveSignals.length >= 2)

  controller.setRuntimeEventsRefreshing('run-1', true)
  assert.equal(controller.isRuntimeEventsRefreshing('run-1'), true)
  controller.setRuntimeEventsRefreshing('run-1', false)
  assert.equal(controller.isRuntimeEventsRefreshing('run-1'), false)

  controller.reset('terminal')
  assert.deepEqual(controller.lastEventIdByRun.value, {})
  assert.equal(controller.connectionState.value, 'terminal')
  assert.equal(controller.refreshingRuntimeRunIds.value.size, 0)
})
