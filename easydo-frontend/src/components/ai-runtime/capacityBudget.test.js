import test from 'node:test'
import assert from 'node:assert/strict'
import { buildRuntimeProcessItems } from './runtimeProcessEvents.js'
import {
  CAPACITY_BUDGET,
  buildBoundedTimelineItems,
  runtimeTraceLimit,
  selectClientResidentEntries
} from './capacityBudget.js'

test('frontend capacity budgets match P2-05 hard product limits', () => {
  assert.equal(CAPACITY_BUDGET.timelineInitialRows, 100)
  assert.equal(CAPACITY_BUDGET.timelineExpandMax, 500)
  assert.equal(CAPACITY_BUDGET.clientResidentEntries, 200)
  assert.equal(CAPACITY_BUDGET.replayPageSize, 200)
  assert.equal(runtimeTraceLimit(false), 100)
  assert.equal(runtimeTraceLimit(true), 500)
})

test('client resident entries keep only the newest window', () => {
  const entries = Array.from({ length: 250 }, (_, index) => ({ id: index + 1 }))
  const kept = selectClientResidentEntries(entries)
  assert.equal(kept.length, 200)
  assert.equal(kept[0].id, 51)
  assert.equal(kept.at(-1).id, 250)
})

test('10k-event timeline stays within expand max and remains finite', () => {
  const events = Array.from({ length: 10_000 }, (_, index) => ({
    type: index % 3 === 0 ? 'session.step.started' : 'session.reasoning.delta',
    payload: {
      event_id: `evt-${index}`,
      delta: index % 3 === 0 ? undefined : 'x',
      message_id: `msg-${Math.floor(index / 20)}`
    }
  }))
  const items = buildBoundedTimelineItems(buildRuntimeProcessItems, {
    events,
    limit: CAPACITY_BUDGET.timelineExpandMax
  })
  assert.ok(Array.isArray(items))
  assert.ok(items.length <= CAPACITY_BUDGET.timelineExpandMax)
  assert.ok(items.length > 0)
})
