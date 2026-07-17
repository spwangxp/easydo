import { describe, expect, it } from 'vitest'
import type { AIRuntimeRun } from '../domain/runtime.js'
import { createMemoryRuntimeStore } from './memoryRuntimeStore.js'

function run(overrides: Partial<AIRuntimeRun> = {}): AIRuntimeRun {
  return {
    id: 1,
    runtime_run_id: 'r_w1_000001_000001',
    session_id: 1,
    workspace_id: 1,
    context_tags: [],
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_snapshot_hash: 'sha256:test',
    status: 'running',
    input_entry_id: 1,
    request: {},
    result: {},
    usage: {},
    started_at: '2026-07-11T00:00:00.000Z',
    created_at: '2026-07-11T00:00:00.000Z',
    updated_at: '2026-07-11T00:00:00.000Z',
    ...overrides
  }
}

describe('RuntimeStore Run leases', () => {
  it('claims renews and releases one active Run owner atomically', async () => {
    const store = createMemoryRuntimeStore()
    await store.saveRun(run())

    const claimed = await store.claimRunLease(1, 'r_w1_000001_000001', 'runtime-a', '2026-07-11T00:00:10.000Z', '2026-07-11T00:00:01.000Z')
    expect(claimed).toMatchObject({ owner_instance_id: 'runtime-a', owner_epoch: 1, owner_lease_expires_at: '2026-07-11T00:00:10.000Z' })
    await expect(store.claimRunLease(1, 'r_w1_000001_000001', 'runtime-b', '2026-07-11T00:00:11.000Z', '2026-07-11T00:00:02.000Z')).resolves.toBeUndefined()

    const renewed = await store.renewRunLease(1, 'r_w1_000001_000001', 'runtime-a', 1, '2026-07-11T00:00:20.000Z')
    expect(renewed?.owner_lease_expires_at).toBe('2026-07-11T00:00:20.000Z')
    await expect(store.releaseRunLease(1, 'r_w1_000001_000001', 'runtime-a', 1)).resolves.toBe(true)
    await expect(store.getRun(1, 'r_w1_000001_000001')).resolves.toMatchObject({ owner_instance_id: undefined })
  })

  it('lets a new owner claim an expired lease with a new epoch', async () => {
    const store = createMemoryRuntimeStore()
    await store.saveRun(run({ owner_instance_id: 'runtime-a', owner_epoch: 2, owner_lease_expires_at: '2026-07-11T00:00:05.000Z' }))

    const claimed = await store.claimRunLease(1, 'r_w1_000001_000001', 'runtime-b', '2026-07-11T00:00:20.000Z', '2026-07-11T00:00:10.000Z')

    expect(claimed).toMatchObject({ owner_instance_id: 'runtime-b', owner_epoch: 3 })
  })

  it('atomically interrupts expired owned Runs', async () => {
    const store = createMemoryRuntimeStore()
    await store.saveRun(run({ owner_instance_id: 'runtime-dead', owner_epoch: 1, owner_lease_expires_at: '2026-07-11T00:00:05.000Z' }))

    const interrupted = await store.interruptExpiredRunLeases('2026-07-11T00:00:10.000Z')

    expect(interrupted).toHaveLength(1)
    expect(interrupted[0]).toMatchObject({ status: 'interrupted', error_code: 'runtime_owner_lease_expired', owner_instance_id: undefined })
    await expect(store.interruptExpiredRunLeases('2026-07-11T00:00:20.000Z')).resolves.toEqual([])
  })
})
