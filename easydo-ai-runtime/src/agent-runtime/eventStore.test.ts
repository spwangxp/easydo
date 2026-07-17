import { describe, expect, it } from 'vitest'
import { InMemoryAgentEventStore } from './eventStore.js'
import { isDurableRuntimeEvent, type AgentRuntimeEvent } from './events.js'

function prompted(seq: number, runtimeRunID = ''): AgentRuntimeEvent {
  return {
    type: 'session.prompted',
    session_id: 'sess-1',
    runtime_run_id: runtimeRunID,
    event_id: `evt-${seq}`,
    seq,
    timestamp: `2026-06-29T00:00:0${seq}.000Z`,
    message_id: `user-${seq}`,
    prompt: `hello ${seq}`,
    files: [],
    delivery: 'prompt'
  }
}

describe('InMemoryAgentEventStore', () => {
  it('classifies streaming deltas as durable so interrupted sessions can be replayed', () => {
    expect(isDurableRuntimeEvent('session.text.delta')).toBe(true)
    expect(isDurableRuntimeEvent('session.reasoning.delta')).toBe(true)
    expect(isDurableRuntimeEvent('session.tool.input.delta')).toBe(true)
  })

  it('appends events with monotonic sequence, replays by session, and rebuilds projection', async () => {
    const store = new InMemoryAgentEventStore()
    const first = await store.append({ ...prompted(1), seq: 99, event_id: '' })
    const second = await store.append({ ...prompted(2), seq: 99, event_id: '' })

    expect(first).toMatchObject({ seq: 1, event_id: 'sess-1:1' })
    expect(second).toMatchObject({ seq: 2, event_id: 'sess-1:2' })

    expect(await store.replay('sess-1')).toHaveLength(2)
    expect(await store.replay('sess-1', 1)).toEqual([second])

    const projection = await store.project('sess-1')
    expect(projection.messages.map((message) => message.text)).toEqual(['hello 1', 'hello 2'])
  })

  it('notifies session subscribers with stored events until they unsubscribe', async () => {
    const store = new InMemoryAgentEventStore()
    const received: AgentRuntimeEvent[] = []
    const unsubscribe = store.subscribe('sess-1', (event) => {
      received.push(event)
    })

    await store.append({ ...prompted(1), seq: 99, event_id: '' })
    unsubscribe()
    await store.append({ ...prompted(2), seq: 99, event_id: '' })

    expect(received).toEqual([expect.objectContaining({ seq: 1, event_id: 'sess-1:1' })])
  })

  it('deduplicates stable event IDs without allocating another sequence or notifying twice', async () => {
    const store = new InMemoryAgentEventStore()
    const received: AgentRuntimeEvent[] = []
    store.subscribe('sess-1', (event) => {
      received.push(event)
    })

    const first = await store.append({ ...prompted(1), seq: 0, event_id: 'stable-queue-event' })
    const duplicate = await store.append({ ...prompted(1), seq: 0, event_id: 'stable-queue-event' })

    expect(duplicate).toEqual(first)
    expect(await store.replay('sess-1')).toEqual([first])
    expect(received).toEqual([first])
  })

  it('replays only events owned by the requested runtime run', async () => {
    const store = new InMemoryAgentEventStore()
    const firstRunEvent = await store.append(prompted(1, 'run-1'))
    await store.append(prompted(2, 'run-2'))
    const firstRunTail = await store.append(prompted(3, 'run-1'))

    expect(await store.replayRun('run-1')).toEqual([firstRunEvent, firstRunTail])
    expect(await store.replayRun('run-1', firstRunEvent.seq)).toEqual([firstRunTail])
    expect(await store.replayRun('run-missing')).toEqual([])
  })
})
