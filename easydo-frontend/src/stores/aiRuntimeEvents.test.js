import test from 'node:test'
import assert from 'node:assert/strict'
import { mergeRuntimeEvents } from './aiRuntimeEvents.js'

function eventIds(events) {
  return events.map((event) => event.event_id || event.payload?.event_id || event.data?.event_id)
}

test('mergeRuntimeEvents sorts valid positive event sequences before unsequenced events', () => {
  const events = mergeRuntimeEvents([
    { type: 'session.error', event_id: 'unsequenced', timestamp: '2026-07-20T08:00:00.000Z' },
    { type: 'session.step.ended', event_id: 'seq-3', seq: 3, timestamp: '2026-07-20T08:00:03.000Z' },
    { type: 'session.step.started', event_id: 'seq-1', event_seq: 1, timestamp: '2026-07-20T08:00:01.000Z' },
    { type: 'session.reasoning.ended', payload: { event_id: 'seq-2', event_seq: 2 }, timestamp: '2026-07-20T08:00:02.000Z' }
  ], [])

  assert.deepEqual(eventIds(events), ['seq-1', 'seq-2', 'seq-3', 'unsequenced'])
})

test('mergeRuntimeEvents orders unsequenced events by their event timestamps', () => {
  const events = mergeRuntimeEvents([
    { type: 'session.step.ended', event_id: 'third', timestamp: '2026-07-20T08:00:03.000Z' },
    { type: 'session.step.started', event_id: 'first', created_at: '2026-07-20T08:00:01.000Z' },
    { type: 'session.reasoning.ended', payload: { event_id: 'second', timestamp: '2026-07-20T08:00:02.000Z' } }
  ], [])

  assert.deepEqual(eventIds(events), ['first', 'second', 'third'])
})

test('mergeRuntimeEvents preserves input order when events remain incomparable', () => {
  const events = mergeRuntimeEvents([
    { type: 'session.step.started', event_id: 'first' },
    { type: 'session.reasoning.ended', event_id: 'second', timestamp: 'invalid' },
    { type: 'session.step.ended', event_id: 'third', event_seq: 0 }
  ], [])

  assert.deepEqual(eventIds(events), ['first', 'second', 'third'])
})

test('mergeRuntimeEvents deduplicates by event_id before payload identity', () => {
  const original = {
    type: 'session.step.started',
    event_id: 'same-event',
    event_seq: 1,
    payload: { status: 'original' }
  }
  const replay = {
    type: 'session.step.ended',
    event_id: 'same-event',
    event_seq: 2,
    payload: { status: 'replayed' }
  }

  assert.deepEqual(mergeRuntimeEvents([original], [replay]), [original])
})
