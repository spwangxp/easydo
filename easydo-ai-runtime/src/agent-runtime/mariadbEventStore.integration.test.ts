import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { createPool, type Pool, type RowDataPacket } from 'mysql2/promise'
import { MariadbAgentEventStore } from './mariadbEventStore.js'
import type { AgentRuntimeEvent } from './events.js'

const databaseURI = String(process.env.RUNTIME_MARIADB_TEST_URI || '').trim()
const integration = databaseURI ? describe : describe.skip

function prompted(sessionID: string, runtimeRunID: string, prompt: string): AgentRuntimeEvent {
  return {
    type: 'session.prompted',
    session_id: sessionID,
    runtime_run_id: runtimeRunID,
    event_id: '',
    seq: 0,
    timestamp: '2026-07-11T00:00:00.000Z',
    message_id: `message-${prompt}`,
    prompt,
    delivery: 'prompt'
  }
}

integration('MariadbAgentEventStore integration', () => {
  const sessionID = 'p001-integration-session'
  const firstRunID = 'p001-integration-run-1'
  const secondRunID = 'p001-integration-run-2'
  let pool: Pool

  beforeAll(async () => {
    pool = createPool({ uri: databaseURI, connectionLimit: 2 })
    await pool.execute('DELETE FROM ai_agent_runtime_events WHERE session_id = ?', [sessionID])
    await pool.execute('DELETE FROM ai_agent_runtime_event_sequences WHERE session_id = ?', [sessionID])
  })

  afterAll(async () => {
    if (!pool) return
    await pool.execute('DELETE FROM ai_agent_runtime_events WHERE session_id = ?', [sessionID])
    await pool.execute('DELETE FROM ai_agent_runtime_event_sequences WHERE session_id = ?', [sessionID])
    await pool.end()
  })

  it('persists indexed run ownership and isolates interleaved runs', async () => {
    const store = new MariadbAgentEventStore(pool)
    const first = await store.append(prompted(sessionID, firstRunID, 'first'))
    await store.append(prompted(sessionID, secondRunID, 'second'))
    const firstTail = await store.append(prompted(sessionID, firstRunID, 'first-tail'))

    expect(await store.replayRun(firstRunID)).toEqual([first, firstTail])
    expect(await store.replayRun(secondRunID)).toEqual([
      expect.objectContaining({ runtime_run_id: secondRunID, prompt: 'second' })
    ])

    const [indexes] = await pool.query<Array<RowDataPacket & { COLUMN_NAME: string, SEQ_IN_INDEX: number }>>(
      `SELECT COLUMN_NAME, SEQ_IN_INDEX
       FROM information_schema.STATISTICS
       WHERE TABLE_SCHEMA = DATABASE()
         AND TABLE_NAME = 'ai_agent_runtime_events'
         AND INDEX_NAME = 'idx_ai_agent_runtime_events_run_seq'
       ORDER BY SEQ_IN_INDEX`
    )
    expect(indexes.map((index) => index.COLUMN_NAME)).toEqual(['runtime_run_id', 'event_seq'])
  })
})
