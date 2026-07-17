import { describe, expect, it } from 'vitest'
import {
  AGENT_ACTIVE_DURATION_ALGORITHM,
  attachActiveDurationTimings,
  computeActiveAgentDuration
} from './activeAgentDuration.js'

describe('computeActiveAgentDuration', () => {
  it('sums exact step windows without approval waits', () => {
    const result = computeActiveAgentDuration([
      { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
      { type: 'session.step.ended', timestamp: '2026-07-06T00:00:04.200Z' }
    ])
    expect(result.active_duration_ms).toBe(4200)
    expect(result.metric_quality).toBe('exact')
    expect(result.algorithm_version).toBe(AGENT_ACTIVE_DURATION_ALGORITHM)
  })

  it('excludes permission asked→resolved overlap from active duration', () => {
    const result = computeActiveAgentDuration([
      { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
      { type: 'permission.asked', call_id: 'call-1', timestamp: '2026-07-06T00:00:02.000Z' },
      { type: 'permission.resolved', call_id: 'call-1', timestamp: '2026-07-06T00:10:02.000Z' },
      { type: 'session.step.ended', timestamp: '2026-07-06T00:10:05.000Z' }
    ])
    expect(result.active_duration_ms).toBe(5000)
    expect(result.metric_quality).toBe('exact')
  })

  it('closes open steps on session.step.failed and marks degraded', () => {
    const result = computeActiveAgentDuration([
      { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
      { type: 'session.reasoning.delta', timestamp: '2026-07-06T00:00:02.000Z', delta: 'thinking' },
      { type: 'session.step.failed', timestamp: '2026-07-06T00:00:05.000Z', error: { message: 'provider failed' } }
    ])
    expect(result.active_duration_ms).toBe(5000)
    expect(result.metric_quality).toBe('degraded')
    expect(result.degraded_reason).toBe('step_failed')
    expect(result.algorithm_version).toBe(AGENT_ACTIVE_DURATION_ALGORITHM)
  })

  it('truncates open steps on run terminal events as degraded', () => {
    const result = computeActiveAgentDuration([
      { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
      { type: 'run.failed', timestamp: '2026-07-06T00:00:08.000Z' }
    ])
    expect(result.active_duration_ms).toBe(8000)
    expect(result.metric_quality).toBe('degraded')
    expect(result.degraded_reason).toBe('run_terminal_truncation')
  })

  it('returns degraded zero when no step windows exist', () => {
    const result = computeActiveAgentDuration([
      { type: 'model.call_started', timestamp: '2026-07-06T00:00:00.000Z' }
    ])
    expect(result.active_duration_ms).toBe(0)
    expect(result.metric_quality).toBe('degraded')
    expect(result.degraded_reason).toBe('no_step_windows')
  })
})

describe('attachActiveDurationTimings', () => {
  it('merges active duration fields without dropping total_ms', () => {
    const timings = attachActiveDurationTimings(
      { total_ms: 9000, first_answer_ms: 100 },
      [
        { type: 'session.step.started', timestamp: '2026-07-06T00:00:00.000Z' },
        { type: 'session.step.failed', timestamp: '2026-07-06T00:00:05.000Z' }
      ]
    )
    expect(timings.total_ms).toBe(9000)
    expect(timings.active_duration_ms).toBe(5000)
    expect(timings.metric_quality).toBe('degraded')
    expect(timings.algorithm_version).toBe(AGENT_ACTIVE_DURATION_ALGORITHM)
  })
})
