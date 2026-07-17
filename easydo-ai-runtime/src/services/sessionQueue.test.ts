import { describe, expect, it } from 'vitest'
import { InMemoryAgentEventStore } from '../agent-runtime/eventStore.js'
import type { AgentRuntimeEvent } from '../agent-runtime/events.js'
import type { RuntimeActor } from '../domain/runtime.js'
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js'
import { AgentProfileService } from './profileService.js'
import { SessionService } from './sessionService.js'

const actor: RuntimeActor = {
  user_id: 7,
  username: 'developer',
  system_role: 'user',
  workspace_id: 11,
  workspace_role: 'developer',
  auth_session_id: 'auth-queue-1'
}

async function queueFixture(options: { runtimeEvents?: boolean, autoDrainSessionQueue?: boolean } = {}) {
  const store = createMemoryRuntimeStore()
  const profiles = new AgentProfileService(store)
  const eventStore = new InMemoryAgentEventStore()
  const harnessSteers: Array<{ runtimeRunID: string, queueItemID: string, instruction: string }> = []
  const service = new SessionService(
    store,
    profiles,
    undefined,
    undefined,
    { autoDrainSessionQueue: options.autoDrainSessionQueue === true },
    undefined,
    options.runtimeEvents || options.autoDrainSessionQueue
      ? {
           runner: {
            async prompt(input) {
              const userMessageID = `${input.runtimeRunID}:user`
              const assistantMessageID = `${input.runtimeRunID}:assistant`
              const eventBase = {
                session_id: input.sessionID,
                runtime_run_id: input.runtimeRunID,
                event_id: '',
                seq: 0,
                timestamp: '2026-07-14T00:00:00.000Z'
              }
              await eventStore.append({ ...eventBase, type: 'session.prompted', message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
              await eventStore.append({ ...eventBase, type: 'session.step.started', assistant_message_id: assistantMessageID, parent_message_id: userMessageID })
              await eventStore.append({ ...eventBase, type: 'session.text.ended', assistant_message_id: assistantMessageID, text_id: `${assistantMessageID}:text`, text: 'queued response' })
              await eventStore.append({ ...eventBase, type: 'session.step.ended', assistant_message_id: assistantMessageID, finish_reason: 'stop' })
               return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
             },
             async steer(runtimeRunID, queueItemID, instruction) {
               harnessSteers.push({ runtimeRunID, queueItemID, instruction })
               return 'accepted'
             }
           },
          eventStore
        }
      : undefined
  )
  const profile = await profiles.createProfile(actor, {
    name: 'Queue Contract Agent',
    provider: { provider_id: 'test' },
    model: { provider_model_key: 'test/model' }
  })
  const session = await service.getCurrentSession(actor, {
    session_kind: 'chat',
    business_type: 'agent_profile',
    business_id: `${profile.id}:draft`,
    title: 'Queue Contract',
    profile_selection: {
      agent_profile_id: profile.id,
      agent_profile_version_id: 'draft',
      first_session_timestamp: '2026-07-14T00:00:00.000Z'
    }
  })
  return { service, session, store, eventStore, harnessSteers }
}

function runtimeSessionID(sessionID: number) {
  return `s_w${actor.workspace_id.toString(36)}_${sessionID.toString(36).padStart(6, '0')}`
}

async function durableQueueEvents(eventStore: InMemoryAgentEventStore, sessionID: number) {
  return eventStore.replay(runtimeSessionID(sessionID))
}

function queueEvent(events: AgentRuntimeEvent[], type: AgentRuntimeEvent['type']) {
  return events.find((event) => event.type === type)
}

describe('SessionService durable queue management', () => {
  it('returns an uncommitted stable lifecycle event from the memory queue store', async () => {
    const { session, store } = await queueFixture()

    const result = await store.enqueueSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      mode: 'follow_up',
      content: 'memory lifecycle handoff',
      attachments: [],
      context_ref: {},
      client_item_id: 'memory-lifecycle-1',
      created_by: actor.user_id,
      created_at: '2026-07-14T00:00:00.000Z',
      lifecycle_event: {
        type: 'session.queue.added',
        session_id: runtimeSessionID(session.id),
        marker: 'created'
      }
    }, 50)

    expect(result).toMatchObject({
      outcome: 'created',
      event_committed: false,
      event: {
        type: 'session.queue.added',
        event_id: `${runtimeSessionID(session.id)}:session.queue.added:${result.outcome === 'created' ? result.item.queue_item_id : 'missing'}:created`,
        queue_item: { status: 'pending' }
      }
    })
  })

  it('enqueues idempotently and enforces a 50 active-item cap', async () => {
    const { service, session } = await queueFixture()
    const firstPayload = {
      mode: 'follow_up',
      content: 'First queued instruction',
      client_item_id: 'queue-item-1',
      attachments: [{ name: 'context.txt' }],
      context_ref: { route_path: '/store/ai-agents' }
    }

    const first = await service.enqueueSessionQueueItem(actor, session.id, firstPayload)
    const repeated = await service.enqueueSessionQueueItem(actor, session.id, firstPayload)

    expect(first).toMatchObject({ idempotent: false, item: { item_seq: 1, position: 1, status: 'pending' } })
    expect(repeated).toEqual({ ...first, idempotent: true })
    await expect(service.enqueueSessionQueueItem(actor, session.id, {
      ...firstPayload,
      content: 'Conflicting reuse'
    })).rejects.toMatchObject({ code: 'session_queue_idempotency_conflict', status: 409 })

    const concurrentPayload = {
      mode: 'follow_up',
      content: 'Concurrent duplicate',
      client_item_id: 'queue-item-2'
    }
    const concurrent = await Promise.all([
      service.enqueueSessionQueueItem(actor, session.id, concurrentPayload),
      service.enqueueSessionQueueItem(actor, session.id, concurrentPayload)
    ])
    expect(new Set(concurrent.map((result) => result.item.queue_item_id)).size).toBe(1)
    expect(concurrent.map((result) => result.idempotent).sort()).toEqual([false, true])

    for (let index = 3; index <= 50; index += 1) {
      await service.enqueueSessionQueueItem(actor, session.id, {
        mode: 'follow_up',
        content: `Queued instruction ${index}`,
        client_item_id: `queue-item-${index}`
      })
    }

    await expect(service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up',
      content: 'Over capacity',
      client_item_id: 'queue-item-51'
    })).rejects.toMatchObject({ code: 'session_queue_full', status: 409 })

    const cancelled = await service.cancelSessionQueueItem(actor, session.id, first.item.queue_item_id)
    expect(cancelled).toMatchObject({ idempotent: false, item: { status: 'cancelled' } })
    await expect(service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up',
      content: 'Capacity released by cancellation',
      client_item_id: 'queue-item-51'
    })).resolves.toMatchObject({ idempotent: false, item: { item_seq: 51, status: 'pending' } })

    const listed = await service.listSessionQueueItems(actor, session.id)
    expect(listed.limit).toBe(50)
    expect(listed.pending_count).toBe(50)
    expect(listed.items).toHaveLength(51)
  })

  it('reorders the exact pending set and cancels pending items idempotently', async () => {
    const { service, session, store } = await queueFixture()
    await seedActiveRun(store, session)
    const first = await service.enqueueSessionQueueItem(actor, session.id, { mode: 'steer', content: 'A', client_item_id: 'queue-a' })
    const second = await service.enqueueSessionQueueItem(actor, session.id, { mode: 'follow_up', content: 'B', client_item_id: 'queue-b' })
    const third = await service.enqueueSessionQueueItem(actor, session.id, { mode: 'stop_and_run', content: 'C', client_item_id: 'queue-c' })

    const reordered = await service.reorderSessionQueueItems(actor, session.id, {
      queue_item_ids: [third.item.queue_item_id, first.item.queue_item_id, second.item.queue_item_id]
    })
    expect(reordered.items.map((item) => [item.content, item.position])).toEqual([
      ['C', 1],
      ['A', 2],
      ['B', 3]
    ])

    await expect(service.reorderSessionQueueItems(actor, session.id, {
      queue_item_ids: [third.item.queue_item_id, second.item.queue_item_id]
    })).rejects.toMatchObject({ code: 'session_queue_reorder_conflict', status: 409 })

    const cancelled = await service.cancelSessionQueueItem(actor, session.id, first.item.queue_item_id)
    const repeated = await service.cancelSessionQueueItem(actor, session.id, first.item.queue_item_id)
    expect(cancelled.idempotent).toBe(false)
    expect(repeated).toEqual({ ...cancelled, idempotent: true })

    const pending = await service.reorderSessionQueueItems(actor, session.id, {
      queue_item_ids: [second.item.queue_item_id, third.item.queue_item_id]
    })
    expect(pending.items.map((item) => item.content)).toEqual(['B', 'C'])
  })

  it('emits added and cancelled once with the final durable queue item snapshot', async () => {
    const { service, session, store, eventStore } = await queueFixture({ runtimeEvents: true })
    await seedActiveRun(store, session)

    const payload = { mode: 'steer', content: 'A', client_item_id: 'queue-event-a' }
    const first = await service.enqueueSessionQueueItem(actor, session.id, payload)
    await service.enqueueSessionQueueItem(actor, session.id, payload)
    await service.cancelSessionQueueItem(actor, session.id, first.item.queue_item_id)
    await service.cancelSessionQueueItem(actor, session.id, first.item.queue_item_id)

    const events = await durableQueueEvents(eventStore, session.id)
    const added = events.filter((event) => event.type === 'session.queue.added')
    const cancelled = events.filter((event) => event.type === 'session.queue.cancelled')
    expect(added).toHaveLength(1)
    expect(added[0]).toMatchObject({
      event_id: `${runtimeSessionID(session.id)}:session.queue.added:${first.item.queue_item_id}:created`,
      queue_item: { queue_item_id: first.item.queue_item_id, status: 'pending', expires_at: first.item.expires_at }
    })
    expect(first.item.expires_at).toBeTruthy()
    expect(cancelled).toHaveLength(1)
    expect(cancelled[0]).toMatchObject({ queue_item: { queue_item_id: first.item.queue_item_id, status: 'cancelled' } })
  })

  it('rejects steer without an active run and binds target_run_id when one exists', async () => {
    const { service, session, store } = await queueFixture({ runtimeEvents: true })
    await expect(service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'inject now',
      client_item_id: 'steer-no-active'
    })).rejects.toMatchObject({ code: 'active_run_required', status: 409 })

    const runtimeRunID = await seedActiveRun(store, session)
    const steered = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'inject later',
      client_item_id: 'steer-with-active'
    })
    expect(steered.item).toMatchObject({
      mode: 'steer',
      status: 'pending',
      target_run_id: runtimeRunID
    })
  })

  it('consumes follow_up immediately when no active run exists', async () => {
    const { service, session, store, eventStore } = await queueFixture({ runtimeEvents: true })
    const enqueued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up',
      content: 'start after idle',
      client_item_id: 'follow-idle-1'
    })
    expect(enqueued.item.status).toBe('pending')

    const processed = await service.processSessionQueue(actor, session.id)
    expect(processed).toMatchObject({
      applied: 0,
      consumed: 1,
      failed: 0
    })
    const listed = await service.listSessionQueueItems(actor, session.id)
    const item = listed.items.find((row) => row.queue_item_id === enqueued.item.queue_item_id)
    expect(item?.status).toBe('consumed')
    expect(item?.consumed_runtime_run_id).toMatch(/^r_/)
    const consumedRun = await store.getRun(actor.workspace_id, String(item?.consumed_runtime_run_id))
    expect(consumedRun?.session_id).toBe(session.id)
    expect(consumedRun?.request.content).toBe('start after idle')
    const events = await durableQueueEvents(eventStore, session.id)
    expect(queueEvent(events, 'session.queue.claimed')).toMatchObject({
      queue_item: { queue_item_id: enqueued.item.queue_item_id, status: 'claimed', claim_epoch: 1 }
    })
    expect(queueEvent(events, 'session.follow_up.started')).toMatchObject({
      queue_item: { queue_item_id: enqueued.item.queue_item_id, status: 'consumed' },
      consumed_runtime_run_id: item?.consumed_runtime_run_id
    })
  })

  it('starts one Pi run and consumes one follow_up when two queue processors race', async () => {
    const { service, session, store, eventStore } = await queueFixture({ runtimeEvents: true })
    const enqueued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up',
      content: 'start exactly once',
      client_item_id: 'follow-race-1'
    })

    const processed = await Promise.all([
      service.processSessionQueue(actor, session.id, { claimed_by: 'runtime-a' }),
      service.processSessionQueue(actor, session.id, { claimed_by: 'runtime-b' })
    ])

    expect(processed.reduce((total, result) => total + result.consumed, 0)).toBe(1)
    const listed = await service.listSessionQueueItems(actor, session.id)
    const item = listed.items.find((row) => row.queue_item_id === enqueued.item.queue_item_id)
    expect(item).toMatchObject({ status: 'consumed' })
    const runtimeRunID = item?.consumed_runtime_run_id
    expect(runtimeRunID).toMatch(/^r_/)
    const entries = await store.listEntries(actor.workspace_id, session.id)
    expect(entries).toHaveLength(2)
    expect(new Set(entries.map((entry) => entry.runtime_run_id).filter(Boolean))).toEqual(new Set([runtimeRunID]))
    const events = await durableQueueEvents(eventStore, session.id)
    expect(events.filter((event) => event.type === 'session.follow_up.started')).toHaveLength(1)
  })

  it('applies steer at a safe checkpoint for the bound active run', async () => {
    const { service, session, store, eventStore, harnessSteers } = await queueFixture({ runtimeEvents: true })
    const runtimeRunID = await seedActiveRun(store, session)
    const steered = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'checkpoint instruction',
      client_item_id: 'steer-apply-1'
    })
    const processed = await service.processSessionQueue(actor, session.id, {
      safe_checkpoint: true,
      runtime_run_id: runtimeRunID
    })
    expect(processed).toMatchObject({ applied: 1, consumed: 0, failed: 0 })
    expect(harnessSteers).toEqual([{
      runtimeRunID,
      queueItemID: steered.item.queue_item_id,
      instruction: 'checkpoint instruction'
    }])
    const listed = await service.listSessionQueueItems(actor, session.id)
    expect(listed.items.find((row) => row.queue_item_id === steered.item.queue_item_id)?.status).toBe('applied')
    const run = await store.getRun(actor.workspace_id, runtimeRunID)
    const result = run?.result && typeof run.result === 'object' ? run.result as Record<string, unknown> : {}
    expect(result.last_queue_steer).toBe('checkpoint instruction')
    const steers = Array.isArray(result.queue_steers) ? result.queue_steers as Array<Record<string, unknown>> : []
    expect(steers.some((entry) => entry.content === 'checkpoint instruction' && entry.status === 'applied')).toBe(true)
    const events = await durableQueueEvents(eventStore, session.id)
    expect(queueEvent(events, 'session.queue.claimed')).toMatchObject({ queue_item: { queue_item_id: steered.item.queue_item_id } })
    expect(queueEvent(events, 'session.steer.applied')).toMatchObject({
      runtime_run_id: runtimeRunID,
      queue_item: { queue_item_id: steered.item.queue_item_id, status: 'applied' }
    })
    expect(events.some((event) => String(event.type) === 'session.steered')).toBe(false)
  })

  it('commits the durable steer fence before invoking the runner side effect', async () => {
    const { service, session, store, harnessSteers } = await queueFixture({ runtimeEvents: true })
    const runtimeRunID = await seedActiveRun(store, session)
    const ordering: string[] = []
    const applySessionQueueSteer = store.applySessionQueueSteer.bind(store)
    store.applySessionQueueSteer = async (input) => {
      const result = await applySessionQueueSteer(input)
      ordering.push(`fence:${result.outcome}`)
      return result
    }
    const steered = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'ordered checkpoint instruction',
      client_item_id: 'steer-ordering-1'
    })
    const originalPush = harnessSteers.push.bind(harnessSteers)
    harnessSteers.push = (...items) => {
      ordering.push('runner:steer')
      return originalPush(...items)
    }

    await service.processSessionQueue(actor, session.id, {
      safe_checkpoint: true,
      runtime_run_id: runtimeRunID
    })

    expect(ordering).toEqual(['fence:applied', 'runner:steer'])
    expect(harnessSteers.map((entry) => ({ ...entry }))).toEqual([{
      runtimeRunID,
      queueItemID: steered.item.queue_item_id,
      instruction: 'ordered checkpoint instruction'
    }])
  })

  it('applies steer immediately for an active running run without waiting for a later drain', async () => {
    const { service, session, store, eventStore, harnessSteers } = await queueFixture({ runtimeEvents: true, autoDrainSessionQueue: true })
    const runtimeRunID = await seedActiveRun(store, session)
    const steered = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'apply while running',
      client_item_id: 'steer-running-apply-1'
    })

    const listed = await service.listSessionQueueItems(actor, session.id)
    expect(listed.items.find((row) => row.queue_item_id === steered.item.queue_item_id)?.status).toBe('applied')
    expect(harnessSteers).toEqual([{
      runtimeRunID,
      queueItemID: steered.item.queue_item_id,
      instruction: 'apply while running'
    }])
    const events = await durableQueueEvents(eventStore, session.id)
    expect(queueEvent(events, 'session.steer.applied')).toMatchObject({
      runtime_run_id: runtimeRunID,
      queue_item: { queue_item_id: steered.item.queue_item_id, status: 'applied' }
    })
  })

  it('keeps unapplied steer pending when the target Run is interrupted by owner lease expiry', async () => {
    const { service, session, store, eventStore } = await queueFixture({ runtimeEvents: true })
    const runtimeRunID = await seedActiveRun(store, session)
    const steered = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'survive owner interrupt',
      client_item_id: 'steer-interrupt-pending-1'
    })
    await store.updateSessionQueueItem({
      ...steered.item,
      status: 'pending',
      claimed_by: undefined,
      claim_expires_at: undefined
    })
    const activeRun = await store.getRun(actor.workspace_id, runtimeRunID)
    expect(activeRun).toBeTruthy()
    await store.saveRun({
      ...activeRun!,
      status: 'interrupted',
      active_slot: undefined,
      owner_instance_id: undefined,
      owner_lease_expires_at: undefined,
      error_code: 'runtime_owner_lease_expired',
      error_msg: 'Runtime owner lease expired',
      finished_at: new Date().toISOString()
    })

    const processed = await service.processSessionQueue(actor, session.id, {
      safe_checkpoint: true,
      runtime_run_id: runtimeRunID
    })
    expect(processed).toMatchObject({ applied: 0, failed: 0 })
    const listed = await service.listSessionQueueItems(actor, session.id)
    expect(listed.items.find((row) => row.queue_item_id === steered.item.queue_item_id)).toMatchObject({
      status: 'pending',
      error_code: undefined
    })
    const events = await durableQueueEvents(eventStore, session.id)
    expect(events.some((event) => String(event.type) === 'session.queue.failed')).toBe(false)
  })

  it('emits expired and failed queue events for terminal steer failures', async () => {
    const expiredFixture = await queueFixture({ runtimeEvents: true })
    const expiredRunID = await seedActiveRun(expiredFixture.store, expiredFixture.session)
    const expiring = await expiredFixture.service.enqueueSessionQueueItem(actor, expiredFixture.session.id, {
      mode: 'steer', content: 'expire me', client_item_id: 'steer-expire-1'
    })
    await expiredFixture.store.updateSessionQueueItem({ ...expiring.item, expires_at: '2000-01-01T00:00:00.000Z' })

    await expiredFixture.service.processSessionQueue(actor, expiredFixture.session.id, {
      safe_checkpoint: true,
      runtime_run_id: expiredRunID
    })

    const expiredEvents = await durableQueueEvents(expiredFixture.eventStore, expiredFixture.session.id)
    expect(queueEvent(expiredEvents, 'session.queue.expired')).toMatchObject({
      queue_item: { queue_item_id: expiring.item.queue_item_id, status: 'expired', error_code: 'steer_checkpoint_timeout' }
    })

    const failedFixture = await queueFixture({ runtimeEvents: true })
    const failedRunID = await seedActiveRun(failedFixture.store, failedFixture.session)
    const failing = await failedFixture.service.enqueueSessionQueueItem(actor, failedFixture.session.id, {
      mode: 'steer', content: 'lose target', client_item_id: 'steer-fail-1'
    })
    const applySessionQueueSteer = failedFixture.store.applySessionQueueSteer.bind(failedFixture.store)
    failedFixture.store.applySessionQueueSteer = async (input) => {
      const activeRun = await failedFixture.store.getRun(actor.workspace_id, failedRunID)
      if (!activeRun) throw new Error('expected active run')
      await failedFixture.store.saveRun({ ...activeRun, status: 'completed', finished_at: input.updated_at })
      return applySessionQueueSteer(input)
    }

    await failedFixture.service.processSessionQueue(actor, failedFixture.session.id, {
      safe_checkpoint: true,
      runtime_run_id: failedRunID
    })

    const failedEvents = await durableQueueEvents(failedFixture.eventStore, failedFixture.session.id)
    expect(queueEvent(failedEvents, 'session.queue.failed')).toMatchObject({
      queue_item: { queue_item_id: failing.item.queue_item_id, status: 'failed', error_code: 'queue_target_not_active' }
    })
  })

  it('emits queue failed when processing encounters an unsupported persisted mode', async () => {
    const { service, session, store, eventStore } = await queueFixture({ runtimeEvents: true })
    const enqueued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up', content: 'invalid persisted mode', client_item_id: 'queue-invalid-mode-1'
    })
    const corrupted = { ...enqueued.item }
    Object.defineProperty(corrupted, 'mode', { value: 'unsupported', enumerable: true })
    await store.updateSessionQueueItem(corrupted)

    await service.processSessionQueue(actor, session.id)

    const events = await durableQueueEvents(eventStore, session.id)
    expect(queueEvent(events, 'session.queue.failed')).toMatchObject({
      queue_item: { queue_item_id: enqueued.item.queue_item_id, status: 'failed', error_code: 'queue_mode_invalid' }
    })
  })

  it('applies a claimed steer exactly once and rejects stale claim epochs', async () => {
    const { service, session, store } = await queueFixture()
    const runtimeRunID = await seedActiveRun(store, session)
    const steered = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer',
      content: 'atomic checkpoint instruction',
      client_item_id: 'steer-atomic-1'
    })
    const claimed = await store.claimNextSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      claimed_by: 'runtime-a',
      claim_expires_at: '2026-07-14T00:05:00.000Z',
      updated_at: '2026-07-14T00:00:01.000Z',
      queue_item_id: steered.item.queue_item_id
    })
    expect(claimed).toMatchObject({ outcome: 'claimed', item: { claim_epoch: 1 } })
    if (claimed.outcome !== 'claimed') throw new Error('expected claimed queue item')

    await expect(store.applySessionQueueSteer({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: claimed.item.queue_item_id,
      claimed_by: 'runtime-b',
      claim_epoch: claimed.item.claim_epoch,
      runtime_run_id: runtimeRunID,
      run_result: { last_queue_steer: 'must not win' },
      updated_at: '2026-07-14T00:00:02.000Z'
    })).resolves.toEqual({ outcome: 'stale_claim' })

    const applied = await store.applySessionQueueSteer({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: claimed.item.queue_item_id,
      claimed_by: 'runtime-a',
      claim_epoch: claimed.item.claim_epoch,
      runtime_run_id: runtimeRunID,
      run_result: { last_queue_steer: 'atomic checkpoint instruction' },
      updated_at: '2026-07-14T00:00:03.000Z'
    })
    expect(applied).toMatchObject({ outcome: 'applied', item: { status: 'applied' }, run: { result: { last_queue_steer: 'atomic checkpoint instruction' } } })

    await expect(store.applySessionQueueSteer({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: claimed.item.queue_item_id,
      claimed_by: 'runtime-a',
      claim_epoch: claimed.item.claim_epoch,
      runtime_run_id: runtimeRunID,
      run_result: { last_queue_steer: 'duplicate must not append' },
      updated_at: '2026-07-14T00:00:04.000Z'
    })).resolves.toMatchObject({ outcome: 'already_applied', item: { status: 'applied' } })
  })

  it('consumes a claimed follow-up exactly once and rejects stale claim epochs', async () => {
    const { service, session, store } = await queueFixture()
    const enqueued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up',
      content: 'atomic follow-up',
      client_item_id: 'follow-atomic-1'
    })
    const claimed = await store.claimNextSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      claimed_by: 'runtime-a',
      claim_expires_at: '2026-07-14T00:05:00.000Z',
      updated_at: '2026-07-14T00:00:01.000Z',
      queue_item_id: enqueued.item.queue_item_id
    })
    expect(claimed).toMatchObject({ outcome: 'claimed', item: { claim_epoch: 1 } })
    if (claimed.outcome !== 'claimed') throw new Error('expected claimed queue item')
    const runtimeRunID = await seedActiveRun(store, session)

    await expect(store.consumeSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: claimed.item.queue_item_id,
      claimed_by: 'runtime-b',
      claim_epoch: claimed.item.claim_epoch,
      consumed_runtime_run_id: runtimeRunID,
      updated_at: '2026-07-14T00:00:02.000Z'
    })).resolves.toEqual({ outcome: 'stale_claim' })

    await expect(store.consumeSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: claimed.item.queue_item_id,
      claimed_by: 'runtime-a',
      claim_epoch: claimed.item.claim_epoch,
      consumed_runtime_run_id: runtimeRunID,
      updated_at: '2026-07-14T00:00:03.000Z'
    })).resolves.toMatchObject({ outcome: 'consumed', item: { status: 'consumed', consumed_runtime_run_id: runtimeRunID } })

    await expect(store.consumeSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: claimed.item.queue_item_id,
      claimed_by: 'runtime-a',
      claim_epoch: claimed.item.claim_epoch,
      consumed_runtime_run_id: runtimeRunID,
      updated_at: '2026-07-14T00:00:04.000Z'
    })).resolves.toMatchObject({ outcome: 'already_consumed', item: { status: 'consumed' } })
  })

  it('reclaims an expired claim and fences the stale owner epoch', async () => {
    const { service, session, store } = await queueFixture()
    const runtimeRunID = await seedActiveRun(store, session)
    const enqueued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'steer', content: 'reclaim expired claim', client_item_id: 'steer-reclaim-1'
    })
    const firstClaim = await store.claimNextSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      claimed_by: 'runtime-a',
      claim_expires_at: '2026-07-14T00:01:00.000Z',
      updated_at: '2026-07-14T00:00:00.000Z',
      queue_item_id: enqueued.item.queue_item_id
    })
    const reclaimed = await store.claimNextSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      claimed_by: 'runtime-b',
      claim_expires_at: '2026-07-14T00:03:00.000Z',
      updated_at: '2026-07-14T00:02:00.000Z',
      queue_item_id: enqueued.item.queue_item_id
    })
    expect(reclaimed).toMatchObject({ outcome: 'claimed', item: { claimed_by: 'runtime-b', claim_epoch: 2 } })
    if (firstClaim.outcome !== 'claimed' || reclaimed.outcome !== 'claimed') throw new Error('expected reclaimed queue item')

    await expect(store.applySessionQueueSteer({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: enqueued.item.queue_item_id,
      claimed_by: 'runtime-a',
      claim_epoch: firstClaim.item.claim_epoch,
      runtime_run_id: runtimeRunID,
      run_result: { last_queue_steer: 'stale owner' },
      updated_at: '2026-07-14T00:02:01.000Z'
    })).resolves.toEqual({ outcome: 'stale_claim' })
    await expect(store.applySessionQueueSteer({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      queue_item_id: enqueued.item.queue_item_id,
      claimed_by: 'runtime-b',
      claim_epoch: reclaimed.item.claim_epoch,
      runtime_run_id: runtimeRunID,
      run_result: { last_queue_steer: 'new owner' },
      updated_at: '2026-07-14T00:02:02.000Z'
    })).resolves.toMatchObject({ outcome: 'applied', item: { status: 'applied' } })
  })

  it('uses P1-11 public queue error codes', async () => {
    const { service, session, store } = await queueFixture()
    await expect(service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'unsupported', content: 'invalid', client_item_id: 'invalid-mode-1'
    })).rejects.toMatchObject({ code: 'queue_mode_invalid' })
    await expect(service.cancelSessionQueueItem(actor, session.id, 'missing-item')).rejects.toMatchObject({ code: 'queue_item_not_found' })

    const enqueued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'follow_up', content: 'claimed item', client_item_id: 'claimed-cancel-1'
    })
    await store.claimNextSessionQueueItem({
      workspace_id: actor.workspace_id,
      session_id: session.id,
      claimed_by: 'runtime-a',
      claim_expires_at: '2099-01-01T00:00:00.000Z',
      updated_at: '2026-07-14T00:00:00.000Z',
      queue_item_id: enqueued.item.queue_item_id
    })
    await expect(service.cancelSessionQueueItem(actor, session.id, enqueued.item.queue_item_id)).rejects.toMatchObject({
      code: 'queue_item_not_cancellable'
    })
  })

  it('stop_and_run cancels the active run then starts a new run once terminal', async () => {
    const { service, session, store } = await queueFixture()
    const runtimeRunID = await seedActiveRun(store, session)
    const queued = await service.enqueueSessionQueueItem(actor, session.id, {
      mode: 'stop_and_run',
      content: 'replace current',
      client_item_id: 'stop-run-1'
    })
    const firstPass = await service.processSessionQueue(actor, session.id)
    expect(firstPass).toMatchObject({ cancelled_runs: 1 })
    const cancelled = await store.getRun(actor.workspace_id, runtimeRunID)
    expect(cancelled?.status).toBe('cancelled')

    const secondPass = await service.processSessionQueue(actor, session.id)
    expect(secondPass.consumed).toBe(1)
    const listed = await service.listSessionQueueItems(actor, session.id)
    const item = listed.items.find((row) => row.queue_item_id === queued.item.queue_item_id)
    expect(item?.status).toBe('consumed')
    expect(item?.consumed_runtime_run_id).toBeTruthy()
    expect(item?.consumed_runtime_run_id).not.toBe(runtimeRunID)
  })
})

async function seedActiveRun(store: ReturnType<typeof createMemoryRuntimeStore>, session: { id: number, agent_profile_id: number, agent_profile_version_id: number, agent_profile_snapshot_hash: string }) {
  const runID = await store.nextRunId()
  const runtimeRunID = `r_w${actor.workspace_id}_${session.id}_${runID}`
  const timestamp = '2026-07-14T00:00:00.000Z'
  await store.saveRun({
    id: runID,
    runtime_run_id: runtimeRunID,
    session_id: session.id,
    workspace_id: actor.workspace_id,
    context_tags: [],
    agent_profile_id: session.agent_profile_id,
    agent_profile_version_id: session.agent_profile_version_id,
    agent_profile_version_key: 'draft',
    agent_profile_snapshot_hash: session.agent_profile_snapshot_hash,
    profile_snapshot: undefined,
    status: 'running',
    input_entry_id: 1,
    request: { content: 'active' },
    result: {},
    usage: {},
    started_at: timestamp,
    created_at: timestamp,
    updated_at: timestamp
  })
  return runtimeRunID
}
