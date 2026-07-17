const TERMINAL_ITEMS = [
  ['completed', '完成'],
  ['failed', '失败'],
  ['cancelled', '取消'],
  ['timeout', '超时'],
  ['interrupted', '中断']
]

function number(value) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
}

function terminal(value = {}) {
  return Object.fromEntries(TERMINAL_ITEMS.map(([key]) => [key, number(value?.[key])]))
}

export function normalizeRuntimeOperationsSummary(payload = {}) {
  const source = payload?.data || payload || {}
  const replica = source.replica || {}
  return {
    observed_at: String(source.observed_at || ''),
    replica: {
      instance_id: String(replica.instance_id || source.instance_id || ''),
      status: String(replica.status || source.replica_status || 'unknown')
    },
    runs: {
      active: number(source.runs?.active),
      awaiting_approval: number(source.runs?.awaiting_approval),
      awaiting_input: number(source.runs?.awaiting_input),
      stale: number(source.runs?.stale),
      orphaned: number(source.runs?.orphaned)
    },
    terminal_5m: terminal(source.terminal_5m),
    terminal_1h: terminal(source.terminal_1h),
    latency_5m: source.latency_5m && typeof source.latency_5m === 'object' ? source.latency_5m : {},
    latency_1h: source.latency_1h && typeof source.latency_1h === 'object' ? source.latency_1h : {},
    failures_1h: Array.isArray(source.failures_1h)
      ? source.failures_1h.map((item) => ({
          category: String(item?.category || 'internal'),
          code: String(item?.code || 'runtime_error'),
          count: number(item?.count)
        }))
      : []
  }
}

export function terminalRows(value) {
  const normalized = terminal(value)
  return TERMINAL_ITEMS.map(([key, label]) => ({ key, label, count: normalized[key] }))
}

export function formatDurationMetric(metric) {
  if (!metric || number(metric.count) === 0) return '-'
  const milliseconds = Number(metric.p95_ms)
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-'
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 2 : 1)} s`
}

export function createRuntimeOperationsLoader(fetchSummary) {
  let sequence = 0
  return {
    async load() {
      const current = ++sequence
      const payload = await fetchSummary()
      if (current !== sequence) return { applied: false }
      return { applied: true, summary: normalizeRuntimeOperationsSummary(payload) }
    },
    invalidate() {
      sequence += 1
    }
  }
}
