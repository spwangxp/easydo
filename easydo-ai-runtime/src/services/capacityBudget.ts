/**
 * Hard capacity budgets for Runtime events / replay / public projection (P2-05).
 * Values are intentional product limits; tests pin them so regressions are visible.
 */

export const CAPACITY_BUDGET = {
  max_sse_event_bytes: 256 * 1024,
  max_inline_tool_result_bytes: 32 * 1024,
  max_run_events_retained: 2_000,
  replay_page_size: 200,
  client_resident_entries: 200,
  timeline_initial_rows: 100,
  timeline_expand_max: 500
} as const

export type CapacityBudget = typeof CAPACITY_BUDGET

export type EventLike = {
  event_id?: unknown
  event_seq?: unknown
  type?: unknown
  event?: unknown
  [key: string]: unknown
}

export type PaginatedEvents<T> = {
  items: T[]
  next_after_event_id: string
  has_more: boolean
  page_size: number
  total_scanned: number
}

export function estimatePayloadBytes(value: unknown): number {
  if (value === undefined || value === null) return 0
  if (typeof value === 'string') return Buffer.byteLength(value, 'utf8')
  try {
    return Buffer.byteLength(JSON.stringify(value), 'utf8')
  } catch {
    return Buffer.byteLength(String(value), 'utf8')
  }
}

export function enforceInlineToolResultBudget(
  value: unknown,
  maxBytes = CAPACITY_BUDGET.max_inline_tool_result_bytes
): { value: unknown, truncated: boolean, original_bytes: number } {
  const originalBytes = estimatePayloadBytes(value)
  if (originalBytes <= maxBytes) {
    return { value, truncated: false, original_bytes: originalBytes }
  }
  if (typeof value === 'string') {
    const slice = Buffer.from(value, 'utf8').subarray(0, Math.max(0, maxBytes - 64)).toString('utf8')
    return {
      value: `${slice}\n…[truncated ${originalBytes} bytes; moved to artifact policy]`,
      truncated: true,
      original_bytes: originalBytes
    }
  }
  return {
    value: {
      truncated: true,
      original_bytes: originalBytes,
      preview: typeof value === 'object' ? '[object truncated for capacity budget]' : String(value).slice(0, 200)
    },
    truncated: true,
    original_bytes: originalBytes
  }
}

export function enforceSseEventBudget(
  payload: Record<string, unknown>,
  maxBytes = CAPACITY_BUDGET.max_sse_event_bytes
): { payload: Record<string, unknown>, truncated: boolean, original_bytes: number } {
  const originalBytes = estimatePayloadBytes(payload)
  if (originalBytes <= maxBytes) {
    return { payload, truncated: false, original_bytes: originalBytes }
  }
  const compact: Record<string, unknown> = {
    type: payload.type || payload.event || 'capacity.truncated',
    event_id: payload.event_id,
    runtime_run_id: payload.runtime_run_id,
    truncated: true,
    original_bytes: originalBytes,
    message: 'SSE event exceeded hard capacity budget and was compacted'
  }
  return { payload: compact, truncated: true, original_bytes: originalBytes }
}

export function selectRetainedRunEvents<T extends EventLike>(
  events: readonly T[] = [],
  maxEvents = CAPACITY_BUDGET.max_run_events_retained
): { retained: T[], dropped_count: number, dropped_summary: string } {
  if (events.length <= maxEvents) {
    return { retained: [...events], dropped_count: 0, dropped_summary: '' }
  }
  const droppedCount = events.length - maxEvents
  const retained = events.slice(droppedCount)
  return {
    retained,
    dropped_count: droppedCount,
    dropped_summary: `Dropped ${droppedCount} older events to honor max_run_events_retained=${maxEvents}`
  }
}

export function paginateEventsByCursor<T extends EventLike>(
  events: readonly T[] = [],
  options: { after_event_id?: string, page_size?: number } = {}
): PaginatedEvents<T> {
  const pageSize = Math.max(1, Math.min(
    CAPACITY_BUDGET.replay_page_size,
    Number(options.page_size) || CAPACITY_BUDGET.replay_page_size
  ))
  const afterEventId = String(options.after_event_id || '').trim()
  let startIndex = 0
  if (afterEventId) {
    const afterIndex = events.findIndex((event) => String(event.event_id || '') === afterEventId)
    startIndex = afterIndex >= 0 ? afterIndex + 1 : 0
  }
  const slice = events.slice(startIndex, startIndex + pageSize)
  const last = slice[slice.length - 1]
  const nextAfter = last ? String(last.event_id || '') : afterEventId
  const hasMore = startIndex + slice.length < events.length
  return {
    items: slice,
    next_after_event_id: nextAfter,
    has_more: hasMore,
    page_size: pageSize,
    total_scanned: events.length
  }
}
