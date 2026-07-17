export const CAPACITY_BUDGET = Object.freeze({
  maxSseEventBytes: 256 * 1024,
  maxInlineToolResultBytes: 32 * 1024,
  maxRunEventsRetained: 2000,
  replayPageSize: 200,
  clientResidentEntries: 200,
  timelineInitialRows: 100,
  timelineExpandMax: 500
})

export function runtimeTraceLimit(expanded = false) {
  return expanded ? CAPACITY_BUDGET.timelineExpandMax : CAPACITY_BUDGET.timelineInitialRows
}

export function selectClientResidentEntries(entries = [], maxEntries = CAPACITY_BUDGET.clientResidentEntries) {
  if (!Array.isArray(entries) || entries.length <= maxEntries) return Array.isArray(entries) ? [...entries] : []
  return entries.slice(entries.length - maxEntries)
}

export function buildBoundedTimelineItems(buildItems, input = {}) {
  const limit = Number.isFinite(Number(input.limit))
    ? Number(input.limit)
    : CAPACITY_BUDGET.timelineInitialRows
  const boundedLimit = Math.min(Math.max(limit, 1), CAPACITY_BUDGET.timelineExpandMax)
  return buildItems({
    ...input,
    limit: boundedLimit
  })
}
