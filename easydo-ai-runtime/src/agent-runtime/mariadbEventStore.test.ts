import { describe, expect, it } from 'vitest'
import { MariadbAgentEventStore, MARIADB_AGENT_EVENT_STORE_SCHEMA } from './mariadbEventStore.js'
import type { AgentRuntimeEvent } from './events.js'

type QueryRecord = { sql: string; params: unknown[] }
type FakeResult = [unknown, undefined]

class FakePool {
  readonly queries: QueryRecord[] = []
  readonly transactions: string[] = []
  private rows: Array<{ session_id: string; runtime_run_id: string | null; event_seq: number; event_id: string; event_type: string; event_json: string; created_at: string }> = []
  private sequences = new Map<string, number>()
  private lastAllocatedSeq = 0
  failEventInsert = false

  async execute(sql: string, params: unknown[] = []) {
    this.queries.push({ sql, params })
    return this.executeStatement(sql, params)
  }

  async query(sql: string, params: unknown[] = []) {
    this.queries.push({ sql, params })
    return this.queryStatement(sql, params)
  }

  async getConnection() {
    return new FakeConnection(this)
  }

  async executeStatement(sql: string, params: unknown[] = []): Promise<FakeResult> {
    if (sql.includes('ai_agent_runtime_event_sequences')) {
      const sessionID = String(params[0])
      const nextSeq = (this.sequences.get(sessionID) || 0) + 1
      this.sequences.set(sessionID, nextSeq)
      this.lastAllocatedSeq = nextSeq
      return [{ affectedRows: 1, insertId: nextSeq }, undefined]
    }
    if (sql.includes('INSERT INTO ai_agent_runtime_events')) {
      if (this.failEventInsert) throw new Error('event insert failed')
      const sessionID = String(params[0])
      const runtimeRunID = params[1] ? String(params[1]) : null
      const eventSeq = Number(params[2])
      const eventID = String(params[3])
      const eventType = String(params[4])
      const event = JSON.parse(String(params[5])) as AgentRuntimeEvent
      if (this.rows.some((row) => row.event_id === eventID)) {
        const error = new Error('Duplicate entry') as Error & { code: string, errno: number }
        error.code = 'ER_DUP_ENTRY'
        error.errno = 1062
        throw error
      }
      const stored = {
        ...event,
        seq: eventSeq,
        event_id: eventID || `${sessionID}:${eventSeq}`
      } as AgentRuntimeEvent
      this.rows.push({
        session_id: sessionID,
        runtime_run_id: runtimeRunID,
        event_seq: eventSeq,
        event_id: stored.event_id,
        event_type: eventType,
        event_json: JSON.stringify(stored),
        created_at: '2026-06-30 00:00:00.000'
      })
    }
    return [{ affectedRows: 1 }, undefined]
  }

  async queryStatement(sql: string, params: unknown[] = []): Promise<FakeResult> {
    if (sql.includes('LAST_INSERT_ID()')) {
      return [[{ event_seq: this.lastAllocatedSeq }], undefined]
    }
    if (sql.includes('MAX(event_seq)')) {
      const sessionID = String(params[0])
      const max_seq = this.rows
        .filter((row) => row.session_id === sessionID)
        .reduce((max, row) => Math.max(max, row.event_seq), 0)
      return [[{ max_seq }], undefined]
    }
    if (sql.includes('FROM ai_agent_runtime_events')) {
      if (sql.includes('event_id = ?')) {
        const eventID = String(params[0])
        return [this.rows.filter((row) => row.event_id === eventID), undefined]
      }
      if (sql.includes('runtime_run_id = ?')) {
        const runtimeRunID = String(params[0])
        const afterSeq = Number(params[1] || 0)
        return [this.rows.filter((row) => row.runtime_run_id === runtimeRunID && row.event_seq > afterSeq), undefined]
      }
      const sessionID = String(params[0])
      const afterSeq = Number(params[1] || 0)
      return [this.rows.filter((row) => row.session_id === sessionID && row.event_seq > afterSeq), undefined]
    }
    return [[], undefined]
  }
}

class FakeConnection {
  constructor(private readonly pool: FakePool) {}

  async beginTransaction() {
    this.pool.transactions.push('BEGIN')
  }

  async commit() {
    this.pool.transactions.push('COMMIT')
  }

  async rollback() {
    this.pool.transactions.push('ROLLBACK')
  }

  release() {
    this.pool.transactions.push('RELEASE')
  }

  async execute(sql: string, params: unknown[] = []) {
    this.pool.queries.push({ sql, params })
    return this.pool.executeStatement(sql, params)
  }

  async query(sql: string, params: unknown[] = []) {
    this.pool.queries.push({ sql, params })
    return this.pool.queryStatement(sql, params)
  }
}

function prompted(prompt: string, runtimeRunID = ''): AgentRuntimeEvent {
  return {
    type: 'session.prompted',
    session_id: 'sess-db-1',
    runtime_run_id: runtimeRunID,
    event_id: '',
    seq: 0,
    timestamp: '2026-06-30T00:00:00.000Z',
    message_id: `user-${prompt}`,
    prompt,
    files: [],
    delivery: 'prompt'
  }
}

describe('MariadbAgentEventStore', () => {
  it('documents the required persistent event table without applying migrations at runtime', () => {
    expect(MARIADB_AGENT_EVENT_STORE_SCHEMA).toContain('CREATE TABLE ai_agent_runtime_events')
    expect(MARIADB_AGENT_EVENT_STORE_SCHEMA).toContain('CREATE TABLE ai_agent_runtime_event_sequences')
    expect(MARIADB_AGENT_EVENT_STORE_SCHEMA).toContain('UNIQUE KEY uk_ai_agent_runtime_events_session_seq')
    expect(MARIADB_AGENT_EVENT_STORE_SCHEMA).toContain('event_json JSON NOT NULL')
    expect(MARIADB_AGENT_EVENT_STORE_SCHEMA).toContain('runtime_run_id VARCHAR(191)')
    expect(MARIADB_AGENT_EVENT_STORE_SCHEMA).toContain('idx_ai_agent_runtime_events_run_seq')
  })

  it('appends OpenCode-style runtime events, replays them, projects them, and notifies subscribers', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)
    const received: AgentRuntimeEvent[] = []
    const unsubscribe = store.subscribe('sess-db-1', (event) => {
      received.push(event)
    })

    const first = await store.append(prompted('hello'))
    const second = await store.append(prompted('again'))
    unsubscribe()

    expect(first).toMatchObject({ seq: 1, event_id: 'sess-db-1:1' })
    expect(second).toMatchObject({ seq: 2, event_id: 'sess-db-1:2' })
    expect(received.map((event) => event.event_id)).toEqual(['sess-db-1:1', 'sess-db-1:2'])
    expect((await store.replay('sess-db-1', 1)).map((event) => event.type === 'session.prompted' ? event.prompt : '')).toEqual(['again'])

    const projection = await store.project('sess-db-1')
    expect(projection.messages.map((message) => message.text)).toEqual(['hello', 'again'])
  })

  it('allocates per-session event sequence numbers through the atomic sequence table instead of MAX scans', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)

    await store.append(prompted('one'))
    await store.append(prompted('two'))

    expect(pool.queries.some((query) => query.sql.includes('MAX(event_seq)'))).toBe(false)
    expect(pool.queries.some((query) => query.sql.includes('ai_agent_runtime_event_sequences'))).toBe(true)
    expect((await store.replay('sess-db-1')).map((event) => event.seq)).toEqual([1, 2])
  })

  it('reads allocated event sequence from the sequence statement result instead of connection-local LAST_INSERT_ID queries', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)

    await store.append(prompted('pooled-connection-safe'))

    expect(pool.queries.some((query) => query.sql.includes('SELECT LAST_INSERT_ID()'))).toBe(false)
    expect((await store.replay('sess-db-1')).map((event) => event.seq)).toEqual([1])
  })

  it('allocates sequence and inserts an event on one pinned transaction', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)

    await store.append(prompted('transactional'))

    expect(pool.transactions).toEqual(['BEGIN', 'COMMIT', 'RELEASE'])
    const sequenceIndex = pool.queries.findIndex((query) => query.sql.includes('ai_agent_runtime_event_sequences'))
    const insertIndex = pool.queries.findIndex((query) => query.sql.includes('INSERT INTO ai_agent_runtime_events'))
    expect(sequenceIndex).toBeGreaterThanOrEqual(0)
    expect(insertIndex).toBeGreaterThan(sequenceIndex)
  })

  it('returns the stored event for a duplicate stable event ID without notifying twice', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)
    const received: AgentRuntimeEvent[] = []
    store.subscribe('sess-db-1', (event) => {
      received.push(event)
    })

    const first = await store.append({ ...prompted('dedupe'), event_id: 'stable-db-event' })
    const duplicate = await store.append({ ...prompted('dedupe'), event_id: 'stable-db-event' })

    expect(duplicate).toEqual(first)
    expect(await store.replay('sess-db-1')).toEqual([first])
    expect(received).toEqual([first])
    expect(pool.queries.filter((query) => query.sql.includes('ai_agent_runtime_event_sequences'))).toHaveLength(1)
  })

  it('rolls back and does not notify when the event insert fails', async () => {
    const pool = new FakePool()
    pool.failEventInsert = true
    const store = new MariadbAgentEventStore(pool as never)
    const received: AgentRuntimeEvent[] = []
    store.subscribe('sess-db-1', (event) => {
      received.push(event)
    })

    await expect(store.append(prompted('rollback'))).rejects.toThrow('event insert failed')

    expect(pool.transactions).toEqual(['BEGIN', 'ROLLBACK', 'RELEASE'])
    expect(received).toEqual([])
  })

  it('inserts event JSON with MariaDB-compatible parameter binding instead of MySQL-only JSON casts', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)

    await store.append(prompted('mariadb-json'))

    const insert = pool.queries.find((query) => query.sql.includes('INSERT INTO ai_agent_runtime_events'))
    expect(insert?.sql).not.toContain('CAST(? AS JSON)')
    expect(() => JSON.parse(String(insert?.params[5]))).not.toThrow()
  })

  it('persists runtime run ownership and replays one run without session leakage', async () => {
    const pool = new FakePool()
    const store = new MariadbAgentEventStore(pool as never)
    const firstRunEvent = await store.append(prompted('first run', 'run-db-1'))
    await store.append(prompted('second run', 'run-db-2'))
    const firstRunTail = await store.append(prompted('first run tail', 'run-db-1'))

    expect(await store.replayRun('run-db-1')).toEqual([firstRunEvent, firstRunTail])
    expect(await store.replayRun('run-db-1', firstRunEvent.seq)).toEqual([firstRunTail])
    const runQuery = pool.queries.filter((query) => query.sql.includes('runtime_run_id = ?')).at(-1)
    expect(runQuery?.params).toEqual(['run-db-1', firstRunEvent.seq])
  })
})
