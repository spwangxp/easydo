import { describe, expect, it } from 'vitest'
import type { AIRuntimeRun } from '../domain/runtime.js'
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js'

function run(id: number, status: AIRuntimeRun['status'], overrides: Partial<AIRuntimeRun> = {}): AIRuntimeRun {
  const runtimeRunID = `r_w1_${String(id).padStart(6, '0')}_000001`
  return {
    id,
    runtime_run_id: runtimeRunID,
    session_id: id,
    workspace_id: 1,
    context_tags: [],
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_snapshot_hash: 'sha256:test',
    status,
    input_entry_id: id,
    request: {},
    result: {},
    usage: {},
    started_at: '2026-07-12T00:00:00.000Z',
    created_at: '2026-07-12T00:00:00.000Z',
    updated_at: '2026-07-12T00:59:00.000Z',
    ...overrides
  }
}

describe('Runtime operations summary', () => {
  it('counts active approval stale orphaned terminal and classified failures', async () => {
    const store = createMemoryRuntimeStore()
    await store.saveRun(run(1, 'running', {
      owner_instance_id: 'runtime-a',
      owner_lease_expires_at: '2026-07-12T01:05:00.000Z'
    }))
    await store.saveRun(run(2, 'awaiting_decision', {
      owner_instance_id: 'runtime-a',
      owner_lease_expires_at: '2026-07-12T00:59:00.000Z'
    }))
    await store.saveRun(run(3, 'awaiting_input'))
    await store.saveRun(run(4, 'failed', {
      error_code: 'provider_timeout',
      result: { error: { category: 'provider_timeout' } },
      finished_at: '2026-07-12T00:58:00.000Z'
    }))
    await store.saveRun(run(5, 'completed', { finished_at: '2026-07-12T00:30:00.000Z' }))
    await store.saveRun(run(6, 'running'))
    await store.saveRun(run(7, 'running', {
      owner_instance_id: 'runtime-a',
      owner_lease_expires_at: '2026-07-12T00:59:00.000Z'
    }))
    await store.saveRun(run(8, 'failed', {
      error_code: 'future_failure',
      result: { error: { category: 'internal' } },
      finished_at: '2026-07-12T01:01:00.000Z'
    }))

    await expect(store.getRuntimeOperationsSummary('2026-07-12T01:00:00.000Z')).resolves.toEqual({
      observed_at: '2026-07-12T01:00:00.000Z',
      runs: { active: 5, awaiting_approval: 1, awaiting_input: 1, stale: 1, orphaned: 1 },
      terminal_5m: { completed: 0, failed: 1, cancelled: 0, timeout: 0, interrupted: 0 },
      terminal_1h: { completed: 1, failed: 1, cancelled: 0, timeout: 0, interrupted: 0 },
      failures_1h: [{ category: 'provider_timeout', code: 'provider_timeout', count: 1 }]
    })
  })
})
