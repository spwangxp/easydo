import { describe, expect, it } from 'vitest'
import { RuntimeMetrics } from './runtimeMetrics.js'

describe('RuntimeMetrics', () => {
  it('renders low-cardinality counters gauges and bounded histograms', () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' })
    metrics.increment('ai_runtime_requests_total', { operation: 'entry_stream', outcome: 'completed' })
    metrics.increment('ai_runtime_requests_total', { operation: 'entry_stream', outcome: 'completed' })
    metrics.setGauge('ai_runtime_runs_active', 3, { status: 'active' })
    metrics.observe('ai_runtime_first_response_seconds', 0.42, { outcome: 'completed' })

    const rendered = metrics.render()
    expect(rendered).toContain('ai_runtime_requests_total{instance="runtime-a",operation="entry_stream",outcome="completed"} 2')
    expect(rendered).toContain('ai_runtime_runs_active{instance="runtime-a",status="active"} 3')
    expect(rendered).toContain('ai_runtime_first_response_seconds_bucket{instance="runtime-a",outcome="completed",le="0.5"} 1')
    expect(rendered).toContain('ai_runtime_first_response_seconds_count{instance="runtime-a",outcome="completed"} 1')
    expect(rendered).toContain('ai_runtime_first_response_seconds_sum{instance="runtime-a",outcome="completed"} 0.42')
  })

  it.each(['workspace_id', 'user_id', 'session_id', 'runtime_run_id', 'message'])('rejects high-cardinality label %s', (label) => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' })

    expect(() => metrics.increment('ai_runtime_requests_total', { [label]: 'unsafe' }))
      .toThrow(`Metric label ${label} is not allowed`)
  })

  it('escapes label values and keeps metric output deterministic', () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' })
    metrics.increment('ai_runtime_terminal_total', { category: 'transport', code: 'provider_"reset"' })
    metrics.increment('ai_runtime_terminal_total', { category: 'validation', code: 'bad\\request' })

    const lines = metrics.render().trim().split('\n')
    expect(lines).toEqual([...lines].sort())
    expect(metrics.render()).toContain('code="provider_\\"reset\\""')
    expect(metrics.render()).toContain('code="bad\\\\request"')
  })

  it('returns bounded rolling latency summaries for operations windows', () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' })
    const observedAt = Date.parse('2026-07-12T01:00:00.000Z')
    metrics.observe('ai_runtime_first_response_seconds', 0.1, { outcome: 'completed' }, observedAt - 70 * 60_000)
    metrics.observe('ai_runtime_first_response_seconds', 0.2, { outcome: 'completed' }, observedAt - 30 * 60_000)
    metrics.observe('ai_runtime_first_response_seconds', 0.5, { outcome: 'completed' }, observedAt - 4 * 60_000)
    metrics.observe('ai_runtime_first_response_seconds', 1, { outcome: 'failed' }, observedAt - 60_000)

    expect(metrics.latencySummary('ai_runtime_first_response_seconds', 5 * 60_000, observedAt)).toEqual({
      count: 2,
      average_ms: 750,
      p50_ms: 500,
      p95_ms: 1000,
      max_ms: 1000
    })
    expect(metrics.latencySummary('ai_runtime_first_response_seconds', 60 * 60_000, observedAt)).toEqual({
      count: 3,
      average_ms: 566.667,
      p50_ms: 500,
      p95_ms: 1000,
      max_ms: 1000
    })
  })

  it('drops observations outside the longest operations window', () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' })
    const observedAt = Date.parse('2026-07-12T01:00:00.000Z')
    metrics.observe('ai_runtime_replay_delay_seconds', 0.25, {}, observedAt - 61 * 60_000)

    expect(metrics.latencySummary('ai_runtime_replay_delay_seconds', 60 * 60_000, observedAt)).toEqual({
      count: 0,
      average_ms: 0,
      p50_ms: 0,
      p95_ms: 0,
      max_ms: 0
    })
  })

  it('increments and decrements gauges without exposing internal values', () => {
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' })
    metrics.addGauge('ai_runtime_sse_connections', 1, { operation: 'entry_stream' })
    metrics.addGauge('ai_runtime_sse_connections', 1, { operation: 'entry_stream' })
    metrics.addGauge('ai_runtime_sse_connections', -1, { operation: 'entry_stream' })

    expect(metrics.render()).toContain('ai_runtime_sse_connections{instance="runtime-a",operation="entry_stream"} 1')
  })
})
