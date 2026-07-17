import { createPool } from 'mysql2/promise';
import { AgentRuntimeProjection } from './projection.js';
export const MARIADB_AGENT_EVENT_STORE_SCHEMA = `
CREATE TABLE ai_agent_runtime_event_sequences (
  session_id VARCHAR(191) NOT NULL,
  last_seq BIGINT UNSIGNED NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE ai_agent_runtime_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  session_id VARCHAR(191) NOT NULL,
  runtime_run_id VARCHAR(191) DEFAULT NULL,
  event_seq BIGINT UNSIGNED NOT NULL,
  event_id VARCHAR(191) NOT NULL,
  event_type VARCHAR(96) NOT NULL,
  event_json JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_ai_agent_runtime_events_session_seq (session_id, event_seq),
  UNIQUE KEY uk_ai_agent_runtime_events_event_id (event_id),
  KEY idx_ai_agent_runtime_events_run_seq (runtime_run_id, event_seq),
  KEY idx_ai_agent_runtime_events_session_type (session_id, event_type, event_seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`.trim();
export class MariadbAgentEventStore {
    pool;
    listenersBySession = new Map();
    constructor(pool) {
        this.pool = pool;
    }
    static create(options) {
        const uri = options.uri?.trim();
        const pool = uri
            ? createPool({ uri, waitForConnections: true, connectionLimit: 10, dateStrings: true })
            : createPool({
                host: options.host,
                port: options.port || 3306,
                user: options.user,
                password: options.password,
                database: options.database,
                waitForConnections: true,
                connectionLimit: 10,
                dateStrings: true
            });
        return new MariadbAgentEventStore(pool);
    }
    async append(event) {
        const sessionID = event.session_id;
        const connection = await this.pool.getConnection();
        let stored;
        try {
            await connection.beginTransaction();
            if (event.event_id) {
                const existing = await this.findByEventID(connection, event.event_id);
                if (existing) {
                    await connection.commit();
                    return existing;
                }
            }
            const seq = await this.nextSeq(connection, sessionID);
            stored = {
                ...event,
                seq,
                event_id: event.event_id || `${sessionID}:${seq}`
            };
            await connection.execute(`INSERT INTO ai_agent_runtime_events (
          session_id, runtime_run_id, event_seq, event_id, event_type, event_json
        ) VALUES (?, ?, ?, ?, ?, ?)`, [sessionID, stored.runtime_run_id || null, stored.seq, stored.event_id, stored.type, JSON.stringify(stored)]);
            await connection.commit();
        }
        catch (error) {
            await connection.rollback();
            if (event.event_id && isDuplicateEventIDError(error)) {
                const existing = await this.findByEventID(this.pool, event.event_id);
                if (existing)
                    return existing;
            }
            throw error;
        }
        finally {
            connection.release();
        }
        for (const listener of this.listenersBySession.get(sessionID) ?? []) {
            void Promise.resolve(listener(stored));
        }
        return stored;
    }
    async replay(sessionID, afterSeq = 0) {
        const [rows] = await this.pool.query(`SELECT event_json
      FROM ai_agent_runtime_events
      WHERE session_id = ? AND event_seq > ?
      ORDER BY event_seq ASC`, [sessionID, afterSeq]);
        return rows.map((row) => parseEvent(row.event_json)).filter((event) => Boolean(event));
    }
    async replayRun(runtimeRunID, afterSeq = 0) {
        const [rows] = await this.pool.query(`SELECT event_json
      FROM ai_agent_runtime_events
      WHERE runtime_run_id = ? AND event_seq > ?
      ORDER BY event_seq ASC`, [runtimeRunID, afterSeq]);
        return rows.map((row) => parseEvent(row.event_json)).filter((event) => Boolean(event));
    }
    async project(sessionID) {
        const projection = new AgentRuntimeProjection();
        for (const event of await this.replay(sessionID))
            projection.apply(event);
        return projection.getSession(sessionID);
    }
    subscribe(sessionID, listener) {
        const listeners = this.listenersBySession.get(sessionID) ?? new Set();
        listeners.add(listener);
        this.listenersBySession.set(sessionID, listeners);
        return () => {
            listeners.delete(listener);
            if (listeners.size === 0)
                this.listenersBySession.delete(sessionID);
        };
    }
    async nextSeq(connection, sessionID) {
        const [result] = await connection.execute(`INSERT INTO ai_agent_runtime_event_sequences (session_id, last_seq)
      VALUES (?, LAST_INSERT_ID(1))
      ON DUPLICATE KEY UPDATE last_seq = LAST_INSERT_ID(last_seq + 1)`, [sessionID]);
        return Number(result.insertId || 0);
    }
    async findByEventID(connection, eventID) {
        const [rows] = await connection.query(`SELECT event_json
      FROM ai_agent_runtime_events
      WHERE event_id = ?
      LIMIT 1`, [eventID]);
        const row = rows[0];
        return row ? parseEvent(row.event_json) : null;
    }
}
function isDuplicateEventIDError(error) {
    return error instanceof Error && error.message.includes('Duplicate entry');
}
function parseEvent(value) {
    const parsed = typeof value === 'string' ? JSON.parse(value) : value;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
        return null;
    if (typeof parsed.type !== 'string' || typeof parsed.session_id !== 'string')
        return null;
    return parsed;
}
