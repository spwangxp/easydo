import { describe, expect, it } from 'vitest'
import type { ActionDecision, AgentAction, SessionPermissionGrant } from '../domain/runtime.js'
import { createMemoryRuntimeStore } from './memoryRuntimeStore.js'

const timestamp = '2026-07-15T00:00:00.000Z'
const digest = `sha256:${'a'.repeat(64)}`

function action(overrides: Partial<AgentAction> = {}): AgentAction {
  return {
    id: 1,
    action_id: 'act-1',
    workspace_id: 11,
    context_tags: [],
    session_id: 3,
    runtime_run_id: 'run-1',
    action_kind: 'mcp.tool',
    idempotency_key: 'action-key-1',
    source: 'model',
    capability_id: 'mcp.tool',
    input_json: { tool_name: 'dangerous_write', arguments: { value: 'original' } },
    input_digest: digest,
    target_json: { target_type: 'mcp', target_id: 'dangerous' },
    policy_json: { permission_key: 'mcp:dangerous:tools:dangerous_write:write', operation_type: 'write' },
    display_json: { title: 'Dangerous write' },
    status: 'awaiting_decision',
    requested_by: 7,
    result_json: {},
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides
  }
}

function decision(overrides: Partial<ActionDecision> = {}): ActionDecision {
  return {
    decision_id: 'decision-1',
    decision_seq: 1,
    workspace_id: 11,
    session_id: 3,
    runtime_run_id: 'run-1',
    action_id: 1,
    decision_type: 'user',
    decision: 'approve_once',
    actor_user_id: 7,
    reason: '',
    input_patch_json: {},
    client_decision_id: 'client-decision-1',
    idempotency_key: 'decision-key-1',
    created_at: timestamp,
    ...overrides
  }
}

function grant(overrides: Partial<SessionPermissionGrant> = {}): SessionPermissionGrant {
  return {
    grant_id: 'grant-1',
    grant_seq: 1,
    workspace_id: 11,
    session_id: 3,
    runtime_run_id: 'run-1',
    action_id: 1,
    permission_key: 'mcp:dangerous:tools:dangerous_write:write',
    tool_name: 'dangerous_write',
    executor_type: 'mcp',
    resource_type: 'mcp_server',
    resource_id: 'dangerous',
    status: 'active',
    granted_by: 7,
    created_at: timestamp,
    expires_at: '2026-07-15T01:00:00.000Z',
    ...overrides
  }
}

describe('MemoryRuntimeStore AgentAction atomic operations', () => {
  it('returns the existing frozen Tool action when the canonical action already exists', async () => {
    const store = createMemoryRuntimeStore()
    const first = action()

    const created = await store.createToolAction(first)
    first.input_json.arguments = { value: 'mutated outside store' }
    const existing = await store.createToolAction(action({ id: 2, input_json: { tool_name: 'changed' } }))

    expect(created.outcome).toBe('created')
    expect(existing.outcome).toBe('existing')
    expect(existing.action).toMatchObject({ id: 1, input_json: { arguments: { value: 'original' } } })
  })

  it('decides an awaiting action once and stores a scoped session grant atomically', async () => {
    const store = createMemoryRuntimeStore()
    await store.createToolAction(action())

    const decided = await store.decideAwaitingAction({
      workspace_id: 11,
      action_id: 1,
      decision: decision({ decision: 'approve_session' }),
      session_grant: grant(),
      decided_at: timestamp
    })
    const duplicate = await store.decideAwaitingAction({
      workspace_id: 11,
      action_id: 1,
      decision: decision({ decision_id: 'decision-2', client_decision_id: 'client-decision-2' }),
      decided_at: timestamp
    })

    expect(decided).toMatchObject({ outcome: 'decided', action: { status: 'approved', decided_by: 7 } })
    expect(duplicate).toMatchObject({ outcome: 'already_decided', action: { status: 'approved' } })
    await expect(store.listSessionPermissionGrants(11, 3)).resolves.toEqual([grant()])
  })

  it('claims approved action execution exactly once and rejects digest mismatch', async () => {
    const store = createMemoryRuntimeStore()
    await store.createToolAction(action({ status: 'approved' }))

    await expect(store.claimApprovedActionExecution({
      workspace_id: 11,
      action_id: 1,
      input_digest: `sha256:${'b'.repeat(64)}`,
      claimed_at: timestamp
    })).resolves.toEqual({ outcome: 'input_mismatch' })

    const claimed = await store.claimApprovedActionExecution({
      workspace_id: 11,
      action_id: 1,
      input_digest: digest,
      claimed_at: timestamp
    })
    const duplicate = await store.claimApprovedActionExecution({
      workspace_id: 11,
      action_id: 1,
      input_digest: digest,
      claimed_at: timestamp
    })

    expect(claimed).toMatchObject({ outcome: 'claimed', action: { status: 'executing' }, execution: { attempt: 1, status: 'running' } })
    expect(duplicate).toMatchObject({ outcome: 'already_claimed', action: { status: 'executing' } })
  })

  it('does not claim rejected actions for execution', async () => {
    const store = createMemoryRuntimeStore()
    await store.createToolAction(action({ status: 'rejected' }))

    await expect(store.claimApprovedActionExecution({
      workspace_id: 11,
      action_id: 1,
      input_digest: digest,
      claimed_at: timestamp
    })).resolves.toEqual({ outcome: 'not_approved' })
  })
})
