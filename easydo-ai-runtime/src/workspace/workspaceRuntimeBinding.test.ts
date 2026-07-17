import { describe, expect, it } from 'vitest'
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js'
import { WorkspaceRuntimeBinding } from './workspaceRuntimeBinding.js'
import type { AIRuntimeWorkspace } from './types.js'
import type { WorkspaceExecutionClient } from './workspaceExecutionEnv.js'
import type { AIRuntimeRun, AISession } from '../domain/runtime.js'

const timestamp = '2026-07-12T00:00:00.000Z'

function workspace(overrides: Partial<AIRuntimeWorkspace> = {}): AIRuntimeWorkspace {
  return {
    id: 1,
    workspace_runtime_id: 'aws_w11_u7_default',
    workspace_id: 11,
    owner_user_id: 7,
    workspace_key: 'default',
    provider: 'docker',
    sandbox_id: 'sandbox-1',
    status: 'ready',
    image: 'easydo-ai-workspace:test',
    root_path: '/workspace',
    repository: {},
    network_policy: { mode: 'none', allow_hosts: [] },
    secret_refs: [],
    quota: { cpu_cores: 1, memory_mb: 1024, disk_mb: 10240, pids: 256 },
    snapshot_hash: 'sha256:workspace',
    lease_epoch: 0,
    provision_epoch: 0,
    state_version: 0,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides
  }
}

function session(overrides: Partial<AISession> = {}): AISession {
  return {
    id: 3,
    workspace_id: 11,
    context_tags: [],
    session_kind: 'chat',
    user_id: 7,
    auth_session_id: 'auth-1',
    business_type: 'agent_profile',
    business_id: '1',
    status: 'active',
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_snapshot_hash: 'sha256:profile',
    agent_workspace_runtime_id: 'aws_w11_u7_default',
    agent_workspace_snapshot_hash: 'sha256:workspace',
    title: 'test',
    entry_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides
  }
}

function run(overrides: Partial<AIRuntimeRun> = {}): AIRuntimeRun {
  return {
    id: 5,
    runtime_run_id: 'run-5',
    session_id: 3,
    workspace_id: 11,
    context_tags: [],
    agent_profile_id: 1,
    agent_profile_version_id: 1,
    agent_profile_snapshot_hash: 'sha256:profile',
    agent_workspace_runtime_id: 'aws_w11_u7_default',
    agent_workspace_snapshot_hash: 'sha256:workspace',
    status: 'running',
    input_entry_id: 1,
    request: {},
    result: {},
    usage: {},
    started_at: timestamp,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides
  }
}

const executionClient = {} as WorkspaceExecutionClient
const lifecycleService = { async ensureDefault() { return workspace() } }

describe('WorkspaceRuntimeBinding', () => {
  it('acquires and releases the immutable Session workspace snapshot', async () => {
    const store = createMemoryRuntimeStore()
    await store.saveAgentWorkspace(workspace())
    await store.saveSession(session())
    const binding = new WorkspaceRuntimeBinding(store, lifecycleService, executionClient, { instanceID: 'runtime-1', leaseMs: 15000 })

    const acquired = await binding.acquire(run())
    expect(acquired.executionEnv.cwd).toBe('/workspace')
    expect((await store.getAgentWorkspace(11, 'aws_w11_u7_default'))?.lease_owner_instance_id).toBe('runtime-1')
    await expect(acquired.renew()).resolves.toBe(true)
    await acquired.release()
    expect((await store.getAgentWorkspace(11, 'aws_w11_u7_default'))?.lease_owner_instance_id).toBeUndefined()
  })

  it('rejects cross-owner and changed snapshot bindings', async () => {
    const store = createMemoryRuntimeStore()
    await store.saveSession(session())
    const binding = new WorkspaceRuntimeBinding(store, lifecycleService, executionClient, { instanceID: 'runtime-1', leaseMs: 15000 })

    await store.saveAgentWorkspace(workspace({ owner_user_id: 8 }))
    await expect(binding.acquire(run())).rejects.toMatchObject({ code: 'agent_workspace_access_denied' })

    await store.saveAgentWorkspace(workspace({ id: 2, snapshot_hash: 'sha256:changed' }))
    await expect(binding.acquire(run())).rejects.toMatchObject({ code: 'agent_workspace_snapshot_changed' })
  })
})
