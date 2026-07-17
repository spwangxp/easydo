import { describe, expect, it } from 'vitest'
import {
  CAPACITY_BUDGET,
  enforceInlineToolResultBudget,
  enforceSseEventBudget,
  estimatePayloadBytes,
  paginateEventsByCursor,
  selectRetainedRunEvents
} from './capacityBudget.js'

describe('capacityBudget', () => {
  it('pins hard product budgets for P2-05', () => {
    expect(CAPACITY_BUDGET.max_sse_event_bytes).toBe(256 * 1024)
    expect(CAPACITY_BUDGET.max_inline_tool_result_bytes).toBe(32 * 1024)
    expect(CAPACITY_BUDGET.max_run_events_retained).toBe(2_000)
    expect(CAPACITY_BUDGET.replay_page_size).toBe(200)
    expect(CAPACITY_BUDGET.client_resident_entries).toBe(200)
    expect(CAPACITY_BUDGET.timeline_initial_rows).toBe(100)
    expect(CAPACITY_BUDGET.timeline_expand_max).toBe(500)
  })

  it('truncates oversized inline tool results and sse payloads', () => {
    const huge = 'x'.repeat(CAPACITY_BUDGET.max_inline_tool_result_bytes + 1_024)
    const tool = enforceInlineToolResultBudget(huge)
    expect(tool.truncated).toBe(true)
    expect(estimatePayloadBytes(tool.value)).toBeLessThanOrEqual(CAPACITY_BUDGET.max_inline_tool_result_bytes + 128)

    const sse = enforceSseEventBudget({
      type: 'session.tool.success',
      event_id: 'e1',
      runtime_run_id: 'r1',
      result: 'y'.repeat(CAPACITY_BUDGET.max_sse_event_bytes + 4_096)
    })
    expect(sse.truncated).toBe(true)
    expect(sse.payload.truncated).toBe(true)
    expect(estimatePayloadBytes(sse.payload)).toBeLessThan(CAPACITY_BUDGET.max_sse_event_bytes)
  })

  it('retains only the newest run events under the hard cap', () => {
    const events = Array.from({ length: 2_050 }, (_, index) => ({
      event_id: `evt-${index}`,
      event_seq: index + 1,
      type: 'session.reasoning.delta'
    }))
    const result = selectRetainedRunEvents(events, CAPACITY_BUDGET.max_run_events_retained)
    expect(result.retained).toHaveLength(2_000)
    expect(result.dropped_count).toBe(50)
    expect(result.retained[0].event_id).toBe('evt-50')
    expect(result.retained.at(-1)?.event_id).toBe('evt-2049')
  })

  it('paginates replay with cursor and bounded page size', () => {
    const events = Array.from({ length: 450 }, (_, index) => ({
      event_id: `evt-${index}`,
      event_seq: index + 1,
      type: 'session.step.started'
    }))
    const page1 = paginateEventsByCursor(events, { page_size: CAPACITY_BUDGET.replay_page_size })
    expect(page1.items).toHaveLength(200)
    expect(page1.has_more).toBe(true)
    expect(page1.next_after_event_id).toBe('evt-199')

    const page2 = paginateEventsByCursor(events, {
      after_event_id: page1.next_after_event_id,
      page_size: CAPACITY_BUDGET.replay_page_size
    })
    expect(page2.items).toHaveLength(200)
    expect(page2.items[0].event_id).toBe('evt-200')
    expect(page2.has_more).toBe(true)

    const page3 = paginateEventsByCursor(events, {
      after_event_id: page2.next_after_event_id,
      page_size: CAPACITY_BUDGET.replay_page_size
    })
    expect(page3.items).toHaveLength(50)
    expect(page3.has_more).toBe(false)
  })

  it('handles 10k-event retention and pagination without unbounded page output', () => {
    const events = Array.from({ length: 10_000 }, (_, index) => ({
      event_id: `evt-${index}`,
      event_seq: index + 1,
      type: index % 2 === 0 ? 'session.reasoning.delta' : 'session.text.delta'
    }))
    const retained = selectRetainedRunEvents(events)
    expect(retained.retained).toHaveLength(CAPACITY_BUDGET.max_run_events_retained)
    expect(retained.dropped_count).toBe(8_000)

    let cursor = ''
    let pages = 0
    let total = 0
    let hasMore = true
    while (hasMore) {
      const page = paginateEventsByCursor(retained.retained, {
        after_event_id: cursor,
        page_size: CAPACITY_BUDGET.replay_page_size
      })
      expect(page.items.length).toBeLessThanOrEqual(CAPACITY_BUDGET.replay_page_size)
      total += page.items.length
      pages += 1
      cursor = page.next_after_event_id
      hasMore = page.has_more
      if (pages > 20) break
    }
    expect(pages).toBe(10)
    expect(total).toBe(CAPACITY_BUDGET.max_run_events_retained)
  })
})
