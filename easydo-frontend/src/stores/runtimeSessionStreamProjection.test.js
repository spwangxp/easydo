import test from 'node:test'
import assert from 'node:assert/strict'
import { ref } from 'vue'
import {
  applyRuntimeStreamEventToAssistantEntry,
  shouldAppendRuntimeEvent
} from './runtimeSessionStreamProjection.js'

function createHarness() {
  const entries = ref([{
    id: 'a1',
    role: 'assistant',
    status: 'streaming',
    content: '',
    content_blocks: [{ type: 'text', text: '' }],
    output: { reasoning: '', available_skills: [], loaded_skills: [], runtime_events: [] }
  }])
  function patchEntry(entriesRef, id, updater) {
    const index = entriesRef.value.findIndex((entry) => entry.id === id)
    const next = { ...entriesRef.value[index], output: { ...entriesRef.value[index].output } }
    updater(next)
    entriesRef.value.splice(index, 1, next)
  }
  return { entries, patchEntry }
}

test('shared stream projection applies text deltas and ended payload', () => {
  const { entries, patchEntry } = createHarness()
  applyRuntimeStreamEventToAssistantEntry({
    patchEntry,
    entriesRef: entries,
    assistantDraftId: 'a1',
    runtimeEventName: 'session.text.delta',
    runtimeEvent: { delta: 'hel' }
  })
  applyRuntimeStreamEventToAssistantEntry({
    patchEntry,
    entriesRef: entries,
    assistantDraftId: 'a1',
    runtimeEventName: 'session.text.delta',
    runtimeEvent: { delta: 'lo' }
  })
  assert.equal(entries.value[0].content, 'hello')
  applyRuntimeStreamEventToAssistantEntry({
    patchEntry,
    entriesRef: entries,
    assistantDraftId: 'a1',
    runtimeEventName: 'session.text.ended',
    runtimeEvent: { text: 'hello world' }
  })
  assert.equal(entries.value[0].content, 'hello world')
})

test('shared stream projection sets permission awaiting_approval and run id', () => {
  const { entries, patchEntry } = createHarness()
  let pending = null
  applyRuntimeStreamEventToAssistantEntry({
    patchEntry,
    entriesRef: entries,
    assistantDraftId: 'a1',
    runtimeEventName: 'permission.asked',
    runtimeEvent: {
      runtime_run_id: 'run-1',
      request_id: 'appr-1',
      call_id: 'call-1',
      tool_name: 'easydo_write',
      reason: 'needs approval',
      input: { path: '/tmp/a' }
    },
    onPendingActions: (actions) => { pending = actions }
  })
  assert.equal(entries.value[0].runtime_run_id, 'run-1')
  assert.equal(entries.value[0].output.awaiting_approval.approval_id, 'appr-1')
  assert.equal(entries.value[0].output.awaiting_approval.tool_name, 'easydo_write')
  assert.ok(Array.isArray(pending))
})

test('shouldAppendRuntimeEvent keeps process events', () => {
  assert.equal(typeof shouldAppendRuntimeEvent('session.step.started'), 'boolean')
})
