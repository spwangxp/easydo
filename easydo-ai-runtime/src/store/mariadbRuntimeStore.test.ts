import { describe, expect, it } from 'vitest'
import { createHash } from 'node:crypto'
import {
  createAgentResourceVersion,
  stableJSONStringify,
  type AgentProfileVersion,
  type AIRuntimeRun,
  type AISession,
  type AISessionEntry,
  type SessionQueueItem
} from '../domain/runtime.js'
import { MariadbRuntimeStore } from './mariadbRuntimeStore.js'
import type { SessionQueueConsumeWithRunInput } from './memoryRuntimeStore.js'

class FakePool {
  statements: Array<{ sql: string; params: unknown[] }> = []
  queryResults: unknown[][] = []
  failVersionInsert = false
  failRuntimeEventInsert = false
  connectionQueryCount = 0

  async execute(sql: string, params: unknown[] = []) {
    this.statements.push({ sql, params })
    if (sql.trimStart().startsWith('SELECT')) {
      return [this.queryResults.shift() || [], undefined]
    }
    if (this.failVersionInsert && sql.includes('INSERT INTO ai_agent_resource_versions')) {
      throw new Error('revision insert failed')
    }
    if (this.failRuntimeEventInsert && sql.includes('INSERT INTO ai_agent_runtime_events')) {
      throw new Error('runtime event insert failed')
    }
    return [{ affectedRows: 1 }, undefined]
  }

  async query(sql: string, params: unknown[] = []) {
    this.statements.push({ sql, params })
    return [this.queryResults.shift() || [], undefined]
  }

  async getConnection() {
    return {
      beginTransaction: async () => { this.statements.push({ sql: 'BEGIN', params: [] }) },
      execute: this.execute.bind(this),
      query: async (sql: string, params: unknown[] = []) => {
        this.connectionQueryCount += 1
        this.statements.push({ sql, params })
        return [this.queryResults.shift() || [], undefined]
      },
      commit: async () => { this.statements.push({ sql: 'COMMIT', params: [] }) },
      rollback: async () => { this.statements.push({ sql: 'ROLLBACK', params: [] }) },
      release: () => { this.statements.push({ sql: 'RELEASE', params: [] }) }
    }
  }
}

function queueRow(overrides: Record<string, unknown> = {}) {
  return {
    queue_item_id: 'qi_wb_000009_000001',
    workspace_id: 11,
    session_id: 9,
    item_seq: 1,
    position: 1,
    mode: 'steer',
    content: 'inspect current state',
    attachments_json: '[]',
    context_ref_json: '{}',
    target_run_id: 'r_wb_000009_000001',
    status: 'claimed',
    client_item_id: 'mariadb-queue-existing',
    claimed_by: 'runtime-a',
    claim_epoch: 1,
    claim_expires_at: '2026-07-14 00:05:00.000',
    expires_at: null,
    consumed_runtime_run_id: null,
    error_code: null,
    error_msg: null,
    created_by: 7,
    created_at: '2026-07-14 00:00:00.000',
    updated_at: '2026-07-14 00:00:00.000',
    ...overrides
  }
}

function queueItem(overrides: Partial<SessionQueueItem> = {}): SessionQueueItem {
  return {
    queue_item_id: 'qi_wb_000009_000001',
    workspace_id: 11,
    session_id: 9,
    item_seq: 1,
    position: 1,
    mode: 'steer',
    content: 'inspect current state',
    attachments: [],
    context_ref: {},
    target_run_id: 'r_wb_000009_000001',
    status: 'claimed',
    client_item_id: 'mariadb-queue-existing',
    claimed_by: 'runtime-a',
    claim_epoch: 1,
    claim_expires_at: '2026-07-14T00:05:00.000Z',
    created_by: 7,
    created_at: '2026-07-14T00:00:00.000Z',
    updated_at: '2026-07-14T00:00:00.000Z',
    ...overrides
  }
}

function runRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    runtime_run_id: 'r_wb_000009_000001',
    session_id: 9,
    workspace_id: 11,
    context_tags_json: '[]',
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_version_key: 'draft',
    agent_profile_snapshot_hash: `sha256:${'a'.repeat(64)}`,
    agent_workspace_runtime_id: null,
    agent_workspace_snapshot_hash: null,
    profile_snapshot_json: '{}',
    status: 'running',
    active_slot: 'active',
    owner_instance_id: null,
    owner_epoch: 0,
    owner_lease_expires_at: null,
    input_entry_id: 1,
    output_entry_id: null,
    request_json: '{}',
    result_json: '{}',
    usage_json: '{}',
    error_code: null,
    error_msg: null,
    started_at: '2026-07-14 00:00:00.000',
    finished_at: null,
    created_at: '2026-07-14 00:00:00.000',
    updated_at: '2026-07-14 00:00:00.000',
    ...overrides
  }
}

function queuedRunStartInput(): SessionQueueConsumeWithRunInput {
  const timestamp = '2026-07-14T00:00:02.000Z'
  const userEntry: AISessionEntry = {
    id: 31,
    session_id: 9,
    workspace_id: 11,
    user_id: 7,
    seq: 1,
    entry_type: 'message',
    role: 'user',
    status: 'completed',
    content: 'queued follow-up',
    content_blocks: [{ type: 'text', text: 'queued follow-up' }],
    input: { runtime_engine: 'pi' },
    output: {},
    idempotency_key: 'entry-user-31',
    created_at: timestamp,
    updated_at: timestamp
  }
  const assistantEntry: AISessionEntry = {
    id: 32,
    session_id: 9,
    workspace_id: 11,
    user_id: 7,
    parent_entry_id: userEntry.id,
    seq: 2,
    entry_type: 'message',
    role: 'assistant',
    status: 'streaming',
    content: '',
    content_blocks: [{ type: 'text', text: '' }],
    input: { runtime_engine: 'pi' },
    output: { runtime_engine: 'pi', runtime_run_id: 'r_wb_000009_000002' },
    runtime_run_id: 'r_wb_000009_000002',
    event_seq: 0,
    idempotency_key: 'entry-assistant-32',
    created_at: timestamp,
    updated_at: timestamp
  }
  const run: AIRuntimeRun = {
    id: 2,
    runtime_run_id: 'r_wb_000009_000002',
    session_id: 9,
    workspace_id: 11,
    context_tags: [],
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_version_key: 'draft',
    agent_profile_snapshot_hash: `sha256:${'a'.repeat(64)}`,
    status: 'running',
    input_entry_id: userEntry.id,
    output_entry_id: assistantEntry.id,
    request: { runtime_engine: 'pi', content: 'queued follow-up' },
    result: {},
    usage: {},
    started_at: timestamp,
    created_at: timestamp,
    updated_at: timestamp
  }
  const updatedSessionProgress: AISession = {
    id: 9,
    workspace_id: 11,
    context_tags: [],
    session_kind: 'chat',
    user_id: 7,
    auth_session_id: 'auth-queue-1',
    business_type: 'agent_profile',
    business_id: '1:draft',
    status: 'active',
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_snapshot_hash: `sha256:${'a'.repeat(64)}`,
    title: 'Queue Contract',
    entry_count: 2,
    last_entry_at: timestamp,
    created_at: timestamp,
    updated_at: timestamp
  }
  return {
    workspace_id: 11,
    session_id: 9,
    queue_item_id: 'qi_wb_000009_000001',
    claimed_by: 'runtime-a',
    claim_epoch: 1,
    user_entry: userEntry,
    assistant_entry: assistantEntry,
    run,
    updated_session_progress: updatedSessionProgress,
    follow_up_started_event: {
      type: 'session.follow_up.started' as const,
      session_id: 's_wb_000009',
      runtime_run_id: run.runtime_run_id,
      consumed_runtime_run_id: run.runtime_run_id,
      event_id: 's_wb_000009:session.follow_up.started:qi_wb_000009_000001:r_wb_000009_000002',
      seq: 0,
      timestamp,
      queue_item: {
        queue_item_id: 'qi_wb_000009_000001',
        workspace_id: 11,
        session_id: 9,
        item_seq: 1,
        position: 1,
        mode: 'follow_up' as const,
        content: 'queued follow-up',
        attachments: [],
        context_ref: {},
        status: 'consumed' as const,
        client_item_id: 'mariadb-queue-existing',
        claimed_by: 'runtime-a',
        claim_epoch: 1,
        consumed_runtime_run_id: run.runtime_run_id,
        created_by: 7,
        created_at: timestamp,
        updated_at: timestamp
      }
    }
  }
}

describe('MariadbRuntimeStore entries', () => {
  it('preserves numeric database IDs and numeric string keys when reading Profile refs', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    const readRefs = (store as unknown as {
      profileRefs(workspaceID: number, profileID: number, section: string): Promise<Array<{ resource_id: string | number }>>
    }).profileRefs.bind(store)
    pool.queryResults.push([{
      resource_type: 'skill',
      resource_id: '123',
      resource_id_kind: 'resource_key',
      resource_version_id: 7,
      resource_version: 'v1',
      snapshot_digest: `sha256:${'a'.repeat(64)}`,
      config_json: '{}',
      required: 1
    }])
    await expect(readRefs(11, 1, 'skills')).resolves.toEqual([
      expect.objectContaining({ resource_id: '123' })
    ])

    pool.queryResults.push([{
      resource_type: 'skill',
      resource_id: '123',
      resource_id_kind: 'database_id',
      resource_version_id: 8,
      resource_version: 'v1',
      snapshot_digest: `sha256:${'b'.repeat(64)}`,
      config_json: '{}',
      required: 1
    }])
    await expect(readRefs(11, 1, 'skills')).resolves.toEqual([
      expect.objectContaining({ resource_id: 123 })
    ])
  })

  it('guards resource deletion while a Run is awaiting a decision', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    pool.queryResults.push(
      [{ id: 7, workspace_id: 11, resource_kind: 'skill', resource_key: 'guarded-skill' }],
      [],
      [],
      [{ profile_snapshot_json: JSON.stringify({ frozen_resources: { skills: [{ id: 7 }] } }) }]
    )

    await expect(store.deleteAgentResourceGuarded(11, 7)).resolves.toBe('in_use')
    const activeRunQuery = pool.statements.find((statement) => statement.sql.includes('FROM ai_runtime_runs'))
    expect(activeRunQuery?.sql).toContain("'awaiting_decision'")
    expect(activeRunQuery?.sql).not.toContain("'awaiting_approval'")
  })

  it('fails closed when a persisted Agent resource revision snapshot no longer matches its digest', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    const version = createAgentResourceVersion({
      resourceVersionID: 101,
      revision: 1,
      resource: {
        id: 7,
        workspace_id: 11,
        resource_kind: 'skill',
        resource_key: 'review-skill',
        resource_id: 'review-skill',
        name: 'Review Skill',
        description: '',
        version: 'v1',
        status: 'active',
        spec: {},
        endpoint: {},
        secret_ref: {},
        tags: [],
        created_by: 5,
        created_at: '2026-07-13T00:00:00.000Z',
        updated_at: '2026-07-13T00:00:00.000Z'
      },
      createdAt: '2026-07-13T00:00:00.000Z'
    })
    pool.queryResults.push([{
      id: version.resource_version_id,
      resource_id: version.resource_id,
      workspace_id: version.workspace_id,
      revision: version.revision,
      source_version: version.source_version,
      snapshot_digest: version.snapshot_digest,
      snapshot_json: JSON.stringify({ ...version.snapshot, name: 'Tampered Skill' }),
      created_by: version.created_by,
      created_at: '2026-07-13 00:00:00.000'
    }])

    await expect(store.getAgentResourceVersion(11, 101)).rejects.toMatchObject({
      code: 'agent_resource_version_integrity_failed',
      status: 500
    })
  })

  it('fails closed when a persisted Profile version snapshot no longer matches its hash', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    const snapshot: AgentProfileVersion['snapshot'] = {
      id: 7,
      workspace_id: 11,
      name: 'Published Agent',
      description: '',
      profile_kind: 'generic',
      context_tags: [],
      provider: {},
      binding: {},
      model: {},
      provider_credential_ref: {},
      inference: {},
      prompt: {},
      skills: [],
      subagents: [],
      mcp_servers: [],
      context_contract: {},
      input_schema: {},
      output_schema: {},
      tool_policy: {},
      memory_policy: {},
      confirmation_policy: {},
      response_mode: 'text',
      status: 'active',
      created_by: 5
    }
    const snapshotHash = `sha256:${createHash('sha256').update(stableJSONStringify(snapshot)).digest('hex')}`
    pool.queryResults.push([{
      id: 17,
      profile_id: 7,
      workspace_id: 11,
      version: 2,
      snapshot_hash: snapshotHash,
      snapshot_json: JSON.stringify({ ...snapshot, name: 'Tampered Agent' }),
      status: 'published',
      published_by: 5,
      published_at: '2026-07-13 00:00:00.000',
      change_summary: ''
    }])

    await expect(store.getProfileVersion(11, 17)).rejects.toMatchObject({
      code: 'agent_profile_version_integrity_failed',
      status: 500
    })
  })

  it('allocates sequence IDs using one pinned connection', async () => {
    const pool = new FakePool()
    pool.queryResults.push([{ id: 41 }])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.nextProfileId()).resolves.toBe(41)

    expect(pool.connectionQueryCount).toBe(1)
    expect(pool.statements.map((statement) => statement.sql)).toEqual(expect.arrayContaining([
      'UPDATE ai_runtime_sequences SET next_value = LAST_INSERT_ID(next_value) + 1 WHERE `name` = ?',
      'SELECT LAST_INSERT_ID() AS id',
      'RELEASE'
    ]))
  })

  it('appends and reads immutable Agent resource revisions without an upsert path', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    const version = createAgentResourceVersion({
      resourceVersionID: 101,
      revision: 3,
      resource: {
        id: 7,
        workspace_id: 11,
        resource_kind: 'skill',
        resource_key: 'review-skill',
        resource_id: 'review-skill',
        name: 'Review Skill',
        description: '',
        version: 'v1.2.3',
        status: 'active',
        spec: {},
        endpoint: {},
        secret_ref: {},
        tags: [],
        created_by: 5,
        created_at: '2026-07-13T00:00:00.000Z',
        updated_at: '2026-07-13T00:00:00.000Z'
      },
      createdAt: '2026-07-13T00:00:00.000Z'
    })

    await store.appendAgentResourceVersion(version)

    expect(pool.statements[0]?.sql).toContain('INSERT INTO ai_agent_resource_versions')
    expect(pool.statements[0]?.sql).not.toContain('ON DUPLICATE KEY UPDATE')

    pool.queryResults.push([{
      id: 101,
      resource_id: 7,
      workspace_id: 11,
      revision: 3,
      source_version: 'v1.2.3',
      snapshot_digest: version.snapshot_digest,
      snapshot_json: JSON.stringify(version.snapshot),
      created_by: 5,
      created_at: '2026-07-13 00:00:00.000'
    }])
    await expect(store.listAgentResourceVersions(11, 7)).resolves.toEqual([version])
    expect(pool.statements.at(-1)?.sql).toContain('ORDER BY revision DESC, id DESC')
  })

  it('rolls back an Agent resource head update when revision append fails', async () => {
    const pool = new FakePool()
    pool.failVersionInsert = true
    const store = new MariadbRuntimeStore(pool as never)
    const resource = {
      id: 7,
      workspace_id: 11,
      resource_kind: 'skill' as const,
      resource_key: 'review-skill',
      resource_id: 'review-skill',
      name: 'Review Skill',
      description: '',
      version: 'v1',
      status: 'active' as const,
      spec: {},
      endpoint: {},
      secret_ref: {},
      tags: [],
      created_by: 5,
      created_at: '2026-07-13T00:00:00.000Z',
      updated_at: '2026-07-13T00:00:00.000Z'
    }
    const version = createAgentResourceVersion({
      resourceVersionID: 101,
      revision: 1,
      resource,
      createdAt: '2026-07-13T00:00:00.000Z'
    })

    pool.queryResults.push([{ id: resource.id }], [{ revision: 0 }])

    await expect(store.updateAgentResourceWithVersion(resource, version)).rejects.toThrow('revision insert failed')
    const headWrite = pool.statements.find((statement) => statement.sql.includes('ai_agent_resources'))
    expect(headWrite?.sql).not.toContain('ON DUPLICATE KEY UPDATE')
    expect(pool.statements.map((statement) => statement.sql)).toEqual(expect.arrayContaining(['BEGIN', 'ROLLBACK', 'RELEASE']))
    expect(pool.statements.map((statement) => statement.sql)).not.toContain('COMMIT')
  })

  it('strictly inserts Profile versions instead of returning phantom versions on a unique conflict', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    const version: AgentProfileVersion = {
      profile_version_id: 17,
      profile_id: 7,
      workspace_id: 11,
      version: 2,
      snapshot_hash: 'sha256:profile-snapshot',
      snapshot: {
        id: 7,
        workspace_id: 11,
        name: 'Strict Publish Agent',
        description: '',
        profile_kind: 'generic',
        context_tags: [],
        provider: {},
        binding: {},
        model: {},
        provider_credential_ref: {},
        inference: {},
        prompt: {},
        skills: [],
        subagents: [],
        mcp_servers: [],
        context_contract: {},
        input_schema: {},
        output_schema: {},
        tool_policy: {},
        memory_policy: {},
        confirmation_policy: {},
        response_mode: 'text',
        status: 'active',
        created_by: 5
      },
      status: 'published',
      published_by: 5,
      published_at: '2026-07-13T00:00:00.000Z',
      change_summary: ''
    }

    await store.saveProfileVersion(version)

    const insert = pool.statements.find((statement) => statement.sql.includes('INSERT INTO ai_agent_profile_versions'))
    expect(insert?.sql).not.toContain('ON DUPLICATE KEY UPDATE')
  })

  it('strictly inserts a new Profile identity instead of upserting a conflicting name', async () => {
    const pool = new FakePool()
    pool.queryResults.push([])
    const store = new MariadbRuntimeStore(pool as never)
    const snapshot = {
      id: 7,
      workspace_id: 11,
      name: 'Strict Profile Identity',
      description: '',
      profile_kind: 'generic',
      context_tags: [],
      provider: {},
      binding: {},
      model: {},
      provider_credential_ref: {},
      inference: {},
      prompt: {},
      skills: [],
      subagents: [],
      mcp_servers: [],
      context_contract: {},
      input_schema: {},
      output_schema: {},
      tool_policy: {},
      memory_policy: {},
      confirmation_policy: {},
      response_mode: 'text' as const,
      status: 'draft' as const,
      created_by: 5,
      created_at: '2026-07-13T00:00:00.000Z',
      updated_at: '2026-07-13T00:00:00.000Z'
    }

    await store.saveProfile(snapshot)

    const insert = pool.statements.find((statement) => statement.sql.includes('INSERT INTO ai_agent_profiles'))
    expect(insert?.sql).not.toContain('ON DUPLICATE KEY UPDATE')
  })

  it('updates entry_type when an existing Assistant Entry is repaired', async () => {
    const pool = new FakePool()
    const store = new MariadbRuntimeStore(pool as never)
    const entry: AISessionEntry = {
      id: 2,
      session_id: 1,
      workspace_id: 1,
      user_id: 1,
      seq: 2,
      entry_type: 'error',
      role: 'assistant',
      status: 'failed',
      content: 'runtime interrupted',
      content_blocks: [{ type: 'text', text: 'runtime interrupted' }],
      input: {},
      output: {},
      runtime_run_id: 'r_w1_000001_000001',
      idempotency_key: 'assistant-1',
      created_at: '2026-07-11T00:00:00.000Z',
      updated_at: '2026-07-11T00:00:01.000Z'
    }

    await store.saveEntry(entry)

    const statement = pool.statements.at(-1)
    expect(statement?.sql).toContain('entry_type=VALUES(entry_type)')
  })

  it('uses one conditional update to claim a workspace recycle lifecycle', async () => {
    const pool = new FakePool()
    pool.execute = async (sql: string, params: unknown[] = []) => {
      pool.statements.push({ sql, params })
      return [{ affectedRows: 0 }, undefined]
    }
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.beginAgentWorkspaceRecycle(
      1,
      'aws_w1_u7_default',
      7,
      'recycle-operation-a',
      '2026-07-12T00:05:00.000Z',
      '2026-07-12T00:00:00.000Z'
    )).resolves.toBeUndefined()

    const statement = pool.statements.at(-1)
    expect(statement?.sql).toContain("status = 'recycling'")
    expect(statement?.sql).toContain("status IN ('ready', 'paused', 'recycled', 'failed')")
    expect(statement?.sql).toContain('lease_owner_instance_id IS NULL')
    expect(statement?.sql).toContain('owner_user_id = ?')
  })

  it('queries durable Runtime operations status and failure aggregates', async () => {
    const pool = new FakePool()
    pool.queryResults.push([
      {
        active: 3,
        awaiting_approval: 1,
        awaiting_input: 1,
        stale: 1,
        orphaned: 1,
        completed_5m: 0,
        failed_5m: 1,
        cancelled_5m: 0,
        timeout_5m: 0,
        interrupted_5m: 0,
        completed_1h: 2,
        failed_1h: 1,
        cancelled_1h: 0,
        timeout_1h: 1,
        interrupted_1h: 0
      }
    ], [
      { category: 'provider_timeout', code: 'provider_timeout', count: 2 }
    ])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.getRuntimeOperationsSummary('2026-07-12T01:00:00.000Z')).resolves.toMatchObject({
      runs: { active: 3, awaiting_approval: 1, awaiting_input: 1, stale: 1, orphaned: 1 },
      terminal_5m: { failed: 1 },
      terminal_1h: { completed: 2, failed: 1, timeout: 1 },
      failures_1h: [{ category: 'provider_timeout', code: 'provider_timeout', count: 2 }]
    })
    expect(pool.statements[0]?.sql).toContain("status IN ('queued', 'running', 'awaiting_decision', 'awaiting_input')")
    expect(pool.statements[0]?.sql).toContain("status IN ('queued', 'running')")
    expect(pool.statements[0]?.sql).toContain('<= bounds.observed_at')
    expect(pool.statements[1]?.sql).toContain("JSON_EXTRACT(result_json, '$.error.category')")
    expect(pool.statements[1]?.sql).toContain('COALESCE(finished_at, updated_at) <= ?')
  })

  it('serializes queue enqueue under the Session lock before allocating sequence and position', async () => {
    const pool = new FakePool()
    pool.queryResults.push(
      [{ id: 9 }],
      [],
      [{ active_count: 2, max_item_seq: 4, max_position: 2 }]
    )
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.enqueueSessionQueueItem({
      workspace_id: 11,
      session_id: 9,
      mode: 'follow_up',
      content: 'queued after current run',
      attachments: [],
      context_ref: {},
       client_item_id: 'mariadb-queue-1',
       created_by: 7,
       created_at: '2026-07-14T00:00:00.000Z',
       lifecycle_event: {
         type: 'session.queue.added',
         session_id: 's_wb_000009',
         marker: 'created'
       }
     }, 50)).resolves.toMatchObject({
       outcome: 'created',
       item: {
        queue_item_id: 'qi_wb_000009_000005',
        item_seq: 5,
         position: 3,
         status: 'pending'
       },
       event_committed: true,
       event: {
         type: 'session.queue.added',
         event_id: 's_wb_000009:session.queue.added:qi_wb_000009_000005:created',
         queue_item: { queue_item_id: 'qi_wb_000009_000005', status: 'pending' }
       }
     })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql[0]).toBe('BEGIN')
    expect(sql[1]).toContain('FROM ai_sessions')
    expect(sql[1]).toContain('FOR UPDATE')
    expect(sql[2]).toContain('client_item_id = ?')
     expect(sql[3]).toContain("status IN ('pending', 'claimed')")
     expect(sql[4]).toContain('INSERT INTO ai_session_queue_items')
     expect(sql.some((statement) => statement.includes('INSERT INTO ai_agent_runtime_events'))).toBe(true)
     expect(sql.findIndex((statement) => statement.includes('INSERT INTO ai_agent_runtime_events')))
       .toBeLessThan(sql.indexOf('COMMIT'))
     expect(sql).toContain('COMMIT')
     expect(sql).not.toContain('ROLLBACK')
   })

   it('rolls back a queue enqueue when its lifecycle event insert fails', async () => {
     const pool = new FakePool()
     pool.failRuntimeEventInsert = true
     pool.queryResults.push(
       [{ id: 9 }],
       [],
       [{ active_count: 0, max_item_seq: 0, max_position: 0 }],
       [{ last_seq: 4 }]
     )
     const store = new MariadbRuntimeStore(pool as never)

     await expect(store.enqueueSessionQueueItem({
       workspace_id: 11,
       session_id: 9,
       mode: 'follow_up',
       content: 'must roll back with event',
       attachments: [],
       context_ref: {},
       client_item_id: 'mariadb-queue-rollback',
       created_by: 7,
       created_at: '2026-07-14T00:00:00.000Z',
       lifecycle_event: {
         type: 'session.queue.added',
         session_id: 's_wb_000009',
         marker: 'created'
       }
     }, 50)).rejects.toThrow('runtime event insert failed')

     const sql = pool.statements.map((statement) => statement.sql)
     expect(sql.some((statement) => statement.includes('INSERT INTO ai_session_queue_items'))).toBe(true)
     expect(sql.some((statement) => statement.includes('INSERT INTO ai_agent_runtime_events'))).toBe(true)
     expect(sql).toContain('ROLLBACK')
     expect(sql).not.toContain('COMMIT')
   })

  it('returns an existing queue item before capacity checks for client_item_id idempotency', async () => {
    const pool = new FakePool()
    pool.queryResults.push([{ id: 9 }], [{
      queue_item_id: 'qi_wb_000009_000001',
      workspace_id: 11,
      session_id: 9,
      item_seq: 1,
      position: 1,
      mode: 'steer',
      content: 'inspect current state',
      attachments_json: '[]',
      context_ref_json: '{}',
      target_run_id: 'r_wb_000009_000001',
      status: 'pending',
      client_item_id: 'mariadb-queue-existing',
      claimed_by: null,
      claim_epoch: 0,
      claim_expires_at: null,
      error_code: null,
      error_msg: null,
      created_by: 7,
      created_at: '2026-07-14 00:00:00.000',
      updated_at: '2026-07-14 00:00:00.000'
    }])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.enqueueSessionQueueItem({
      workspace_id: 11,
      session_id: 9,
      mode: 'steer',
      content: 'inspect current state',
      attachments: [],
      context_ref: {},
      target_run_id: 'r_wb_000009_000001',
      client_item_id: 'mariadb-queue-existing',
      created_by: 7,
      created_at: '2026-07-14T00:00:01.000Z'
    }, 50)).resolves.toMatchObject({ outcome: 'existing', item: { item_seq: 1 } })

    expect(pool.statements.some((statement) => statement.sql.includes('INSERT INTO ai_session_queue_items'))).toBe(false)
    expect(pool.statements.map((statement) => statement.sql)).toContain('COMMIT')
  })

  it('commits cancel and claim lifecycle events with their queue transitions', async () => {
    const cancelPool = new FakePool()
    cancelPool.queryResults.push([{ id: 9 }], [queueRow({ status: 'pending', claimed_by: null, claim_epoch: 0 })])
    const cancelStore = new MariadbRuntimeStore(cancelPool as never)

    await expect(cancelStore.cancelSessionQueueItem({
      workspace_id: 11,
      session_id: 9,
      queue_item_id: 'qi_wb_000009_000001',
      updated_at: '2026-07-14T00:00:01.000Z',
      lifecycle_event: {
        type: 'session.queue.cancelled',
        session_id: 's_wb_000009',
        marker: 'cancelled'
      }
    })).resolves.toMatchObject({ outcome: 'cancelled', event_committed: true })

    const cancelSQL = cancelPool.statements.map((statement) => statement.sql)
    expect(cancelSQL.findIndex((statement) => statement.includes('INSERT INTO ai_agent_runtime_events')))
      .toBeLessThan(cancelSQL.indexOf('COMMIT'))

    const claimPool = new FakePool()
    claimPool.queryResults.push(
      [{ id: 9 }],
      [queueRow({ status: 'pending', claimed_by: null, claim_epoch: 0 })]
    )
    const claimStore = new MariadbRuntimeStore(claimPool as never)

    await expect(claimStore.claimNextSessionQueueItem({
      workspace_id: 11,
      session_id: 9,
      claimed_by: 'runtime-a',
      claim_expires_at: '2026-07-14T00:05:00.000Z',
      updated_at: '2026-07-14T00:00:01.000Z',
      lifecycle_event: { type: 'session.queue.claimed', session_id: 's_wb_000009' }
    })).resolves.toMatchObject({
      outcome: 'claimed',
      event_committed: true,
      event: { event_id: 's_wb_000009:session.queue.claimed:qi_wb_000009_000001:1' }
    })

    const claimSQL = claimPool.statements.map((statement) => statement.sql)
    expect(claimSQL.findIndex((statement) => statement.includes('INSERT INTO ai_agent_runtime_events')))
      .toBeLessThan(claimSQL.indexOf('COMMIT'))
  })

  it('reorders only when the request contains the exact pending queue set', async () => {
    const row = (queueItemID: string, itemSeq: number, position: number) => ({
      queue_item_id: queueItemID,
      workspace_id: 11,
      session_id: 9,
      item_seq: itemSeq,
      position,
      mode: 'follow_up',
      content: queueItemID,
      attachments_json: '[]',
      context_ref_json: '{}',
      target_run_id: null,
      status: 'pending',
      client_item_id: `client-${itemSeq}`,
      claimed_by: null,
      claim_epoch: 0,
      claim_expires_at: null,
      error_code: null,
      error_msg: null,
      created_by: 7,
      created_at: '2026-07-14 00:00:00.000',
      updated_at: '2026-07-14 00:00:00.000'
    })
    const pool = new FakePool()
    pool.queryResults.push([{ id: 9 }], [row('queue-a', 1, 1), row('queue-b', 2, 2)])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.reorderSessionQueueItems(11, 9, ['queue-b', 'queue-a'], '2026-07-14T00:00:01.000Z'))
      .resolves.toMatchObject({
        outcome: 'reordered',
        items: [{ queue_item_id: 'queue-b', position: 1 }, { queue_item_id: 'queue-a', position: 2 }]
      })

    const updates = pool.statements.filter((statement) => statement.sql.includes('UPDATE ai_session_queue_items SET position'))
    expect(updates.map((statement) => statement.params[0])).toEqual([1, 2])
    expect(pool.statements.map((statement) => statement.sql)).toContain('COMMIT')
  })

  it('applies a claimed queue steer with run and item updates in one transaction', async () => {
    const pool = new FakePool()
    pool.queryResults.push([queueRow()], [runRow()])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.applySessionQueueSteer({
      workspace_id: 11,
      session_id: 9,
      queue_item_id: 'qi_wb_000009_000001',
      claimed_by: 'runtime-a',
      claim_epoch: 1,
      runtime_run_id: 'r_wb_000009_000001',
      run_result: { last_queue_steer: 'inspect current state' },
      updated_at: '2026-07-14T00:00:02.000Z',
      lifecycle_event: {
        type: 'session.steer.applied',
        session_id: 's_wb_000009',
        marker: 'applied',
        runtime_run_id: 'r_wb_000009_000001'
      }
    })).resolves.toMatchObject({
      outcome: 'applied',
      item: { status: 'applied' },
      run: { result: { last_queue_steer: 'inspect current state' } },
      event_committed: true
    })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql[0]).toBe('BEGIN')
    expect(sql[1]).toContain('FROM ai_session_queue_items')
    expect(sql[1]).toContain('FOR UPDATE')
    expect(sql[2]).toContain('FROM ai_runtime_runs')
    expect(sql[2]).toContain('FOR UPDATE')
    expect(sql[3]).toContain('UPDATE ai_runtime_runs SET result_json')
    expect(sql[3]).toContain("active_slot = 'active'")
    expect(sql[4]).toContain("status = 'claimed'")
    expect(sql[4]).toContain('claimed_by = ?')
    expect(sql[4]).toContain('claim_epoch = ?')
    expect(sql.findIndex((statement) => statement.includes('INSERT INTO ai_agent_runtime_events')))
      .toBeLessThan(sql.indexOf('COMMIT'))
    expect(sql).toContain('COMMIT')
  })

  it('commits a claim-fenced terminal lifecycle event with the queue transition', async () => {
    const pool = new FakePool()
    pool.queryResults.push([queueRow()])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.transitionSessionQueueItem({
      item: {
        ...queueItem(),
        status: 'failed',
        claimed_by: undefined,
        claim_expires_at: undefined,
        error_code: 'queue_target_not_active',
        error_msg: 'steer target Run is no longer active',
        updated_at: '2026-07-14T00:00:02.000Z'
      },
      claimed_by: 'runtime-a',
      claim_epoch: 1,
      lifecycle_event: {
        type: 'session.queue.failed',
        session_id: 's_wb_000009',
        marker: 'queue_target_not_active'
      }
    })).resolves.toMatchObject({ outcome: 'transitioned', event_committed: true })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql[1]).toContain('FOR UPDATE')
    expect(sql[2]).toContain("status = 'claimed'")
    expect(sql.findIndex((statement) => statement.includes('INSERT INTO ai_agent_runtime_events')))
      .toBeLessThan(sql.indexOf('COMMIT'))
  })

  it('consumes a claimed queue item only through the matching claim fence', async () => {
    const pool = new FakePool()
    pool.queryResults.push([queueRow({ mode: 'follow_up', target_run_id: null })], [runRow()], [])
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.consumeSessionQueueItem({
      workspace_id: 11,
      session_id: 9,
      queue_item_id: 'qi_wb_000009_000001',
      claimed_by: 'runtime-a',
      claim_epoch: 1,
      consumed_runtime_run_id: 'r_wb_000009_000001',
      updated_at: '2026-07-14T00:00:02.000Z'
    })).resolves.toMatchObject({
      outcome: 'consumed',
      item: { status: 'consumed', consumed_runtime_run_id: 'r_wb_000009_000001' }
    })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql[0]).toBe('BEGIN')
    expect(sql[1]).toContain('FROM ai_session_queue_items')
    expect(sql[1]).toContain('FOR UPDATE')
    expect(sql[2]).toContain('FROM ai_runtime_runs')
    expect(sql[2]).toContain('FOR UPDATE')
    expect(sql[3]).toContain("active_slot = 'active'")
    expect(sql[3]).toContain('runtime_run_id <> ?')
    expect(sql[4]).toContain("status = 'consumed'")
    expect(sql[4]).toContain("status = 'claimed'")
    expect(sql[4]).toContain('claimed_by = ?')
    expect(sql[4]).toContain('claim_epoch = ?')
    expect(sql).toContain('COMMIT')
  })

  it('atomically consumes a claimed queue item with Pi entries, run, session progress, and started event', async () => {
    const pool = new FakePool()
    pool.queryResults.push(
      [{ id: 9 }],
      [queueRow({ mode: 'follow_up', target_run_id: null })],
      [],
      [{ last_seq: 8 }]
    )
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.consumeSessionQueueItemWithRun(queuedRunStartInput())).resolves.toMatchObject({
      outcome: 'consumed',
      item: { status: 'consumed', consumed_runtime_run_id: 'r_wb_000009_000002' },
      run: { runtime_run_id: 'r_wb_000009_000002' }
    })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql[0]).toBe('BEGIN')
    expect(sql.filter((statement) => statement === 'BEGIN')).toHaveLength(1)
    expect(sql.some((statement) => statement.includes('FROM ai_sessions') && statement.includes('FOR UPDATE'))).toBe(true)
    expect(sql.some((statement) => statement.includes('FROM ai_session_queue_items') && statement.includes('FOR UPDATE'))).toBe(true)
    expect(sql.some((statement) => statement.includes("active_slot = 'active'") && statement.includes('FOR UPDATE'))).toBe(true)
    expect(sql.some((statement) => statement.includes('INSERT INTO ai_session_entries'))).toBe(true)
    expect(sql.some((statement) => statement.includes('INSERT INTO ai_runtime_runs'))).toBe(true)
    expect(sql.some((statement) => statement.includes('UPDATE ai_sessions'))).toBe(true)
    expect(sql.some((statement) => statement.includes('INSERT INTO ai_agent_runtime_events'))).toBe(true)
    expect(sql).toContain('COMMIT')
  })

  it('returns already_consumed without creating another queued Pi run', async () => {
    const pool = new FakePool()
    pool.queryResults.push(
      [{ id: 9 }],
      [queueRow({
        mode: 'follow_up',
        status: 'consumed',
        consumed_runtime_run_id: 'r_wb_000009_000001'
      })],
      [runRow()]
    )
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.consumeSessionQueueItemWithRun(queuedRunStartInput())).resolves.toMatchObject({
      outcome: 'already_consumed',
      item: { status: 'consumed' },
      run: { runtime_run_id: 'r_wb_000009_000001' }
    })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql.some((statement) => statement.includes('INSERT INTO ai_runtime_runs'))).toBe(false)
    expect(sql).toContain('COMMIT')
  })

  it('returns stale_claim without creating a queued Pi run when the claim fence changed', async () => {
    const pool = new FakePool()
    pool.queryResults.push(
      [{ id: 9 }],
      [queueRow({ mode: 'follow_up', claim_epoch: 2 })]
    )
    const store = new MariadbRuntimeStore(pool as never)

    await expect(store.consumeSessionQueueItemWithRun(queuedRunStartInput())).resolves.toEqual({ outcome: 'stale_claim' })

    const sql = pool.statements.map((statement) => statement.sql)
    expect(sql.some((statement) => statement.includes('INSERT INTO ai_runtime_runs'))).toBe(false)
    expect(sql).toContain('COMMIT')
  })
})
