import { describe, expect, it } from 'vitest'
import type { RuntimeActor } from '../domain/runtime.js'
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js'
import { RuntimeDomainError } from '../services/runtimeErrors.js'
import { AgentWorkspaceService } from './workspaceService.js'
import type { AIRuntimeWorkspace } from './types.js'

function actor(userID = 7): RuntimeActor {
  return {
    user_id: userID,
    username: `user-${userID}`,
    system_role: 'user',
    workspace_id: 1,
    workspace_role: 'owner',
    auth_session_id: `auth-${userID}`
  }
}

function fakeProvider() {
  const calls: string[] = []
  return {
    calls,
    async provision(workspace: AIRuntimeWorkspace) {
      calls.push(`provision:${workspace.workspace_runtime_id}`)
      return { sandbox_id: `sandbox-${workspace.workspace_runtime_id}` }
    },
    async connect(workspace: AIRuntimeWorkspace) {
      calls.push(`connect:${workspace.workspace_runtime_id}`)
    },
    async pause(workspace: AIRuntimeWorkspace) {
      calls.push(`pause:${workspace.workspace_runtime_id}`)
    },
    async resume(workspace: AIRuntimeWorkspace) {
      calls.push(`resume:${workspace.workspace_runtime_id}`)
    },
    async recycle(workspace: AIRuntimeWorkspace) {
      calls.push(`recycle:${workspace.workspace_runtime_id}`)
    }
  }
}

function workspaceIdentity(workspace: AIRuntimeWorkspace) {
  return {
    id: workspace.id,
    workspace_runtime_id: workspace.workspace_runtime_id,
    snapshot_hash: workspace.snapshot_hash
  }
}

describe('AgentWorkspaceService', () => {
  it('provisions one durable default workspace idempotently for its owner', async () => {
    const store = createMemoryRuntimeStore()
    const provider = fakeProvider()
    const service = new AgentWorkspaceService(store, provider, {
      now: () => '2026-07-12T00:00:00.000Z',
      defaultImage: 'easydo-ai-workspace:test'
    })

    const first = await service.ensureDefault(actor())
    const second = await service.ensureDefault(actor())

    expect(second.workspace_runtime_id).toBe(first.workspace_runtime_id)
    expect(first).toMatchObject({
      workspace_id: 1,
      owner_user_id: 7,
      workspace_key: 'default',
      status: 'ready',
      image: 'easydo-ai-workspace:test',
      root_path: '/workspace'
    })
    expect(provider.calls).toEqual([`provision:${first.workspace_runtime_id}`])
    await expect(store.listAgentWorkspaceAudits(1, first.workspace_runtime_id)).resolves.toMatchObject([
      { operation: 'created', outcome: 'succeeded', actor_user_id: 7 }
    ])
  })

  it('uses the provision fence so concurrent default provisioning calls invoke the provider once', async () => {
    const store = createMemoryRuntimeStore()
    let releaseProvision!: () => void
    const provisionGate = new Promise<void>((resolve) => { releaseProvision = resolve })
    let provisionCalls = 0
    const provider = fakeProvider()
    provider.provision = async (workspace) => {
      provisionCalls += 1
      await provisionGate
      return { sandbox_id: `sandbox-${workspace.workspace_runtime_id}` }
    }
    const service = new AgentWorkspaceService(store, provider, {
      now: () => '2026-07-12T00:00:00.000Z',
      defaultImage: 'easydo-ai-workspace:test'
    })

    const first = service.ensureDefault(actor())
    await expect.poll(() => provisionCalls).toBe(1)
    const second = service.ensureDefault(actor())
    await expect.poll(async () => (await store.listAgentWorkspaces(1, 7))[0]?.provision_owner_instance_id).toBe('runtime:workspace-provision')
    releaseProvision()

    await expect(first).resolves.toMatchObject({ status: 'ready' })
    await expect(second).resolves.toMatchObject({ status: 'ready' })
    expect(provisionCalls).toBe(1)
    await expect(store.listAgentWorkspaces(1, 7)).resolves.toHaveLength(1)
  })

  it('keeps a ready default workspace ready when audit append fails after provisioning', async () => {
    const store = createMemoryRuntimeStore()
    const appendAudit = store.appendAgentWorkspaceAudit.bind(store)
    let auditCalls = 0
    store.appendAgentWorkspaceAudit = async (audit) => {
      auditCalls += 1
      if (auditCalls === 1) throw new Error('audit database unavailable')
      return appendAudit(audit)
    }
    const provider = fakeProvider()
    const service = new AgentWorkspaceService(store, provider, {
      now: () => '2026-07-12T00:00:00.000Z',
      defaultImage: 'easydo-ai-workspace:test'
    })

    await expect(service.ensureDefault(actor())).resolves.toMatchObject({ status: 'ready' })
    await expect(store.getAgentWorkspace(1, 'aws_w1_u7_default')).resolves.toMatchObject({
      status: 'ready',
      error_code: undefined,
      error_msg: undefined
    })
    expect(provider.calls).toEqual(['provision:aws_w1_u7_default'])
  })

  it('recovers the durable default workspace when provisioning becomes available', async () => {
    const store = createMemoryRuntimeStore()
    let providerAvailable = false
    const provider = fakeProvider()
    const provision = provider.provision
    provider.provision = async (workspace) => {
      if (!providerAvailable) {
        provider.calls.push(`provision:${workspace.workspace_runtime_id}`)
        throw new Error('workspace image unavailable')
      }
      return provision(workspace)
    }
    const service = new AgentWorkspaceService(store, provider, {
      now: () => '2026-07-12T00:00:00.000Z',
      defaultImage: 'easydo-ai-workspace:test'
    })

    await expect(service.ensureDefault(actor())).rejects.toMatchObject({
      code: 'agent_workspace_provision_failed',
      status: 502
    } satisfies Partial<RuntimeDomainError>)
    const failed = (await store.listAgentWorkspaces(1, 7))[0]
    expect(failed).toMatchObject({
      status: 'failed',
      error_code: 'agent_workspace_provision_failed',
      error_msg: 'workspace image unavailable'
    })
    const identity = workspaceIdentity(failed)

    providerAvailable = true
    const recovered = await service.ensureDefault(actor())

    expect(recovered).toMatchObject({
      status: 'ready',
      sandbox_id: `sandbox-${failed.workspace_runtime_id}`,
      error_code: undefined,
      error_msg: undefined
    })
    expect(workspaceIdentity(recovered)).toEqual(identity)
    expect(provider.calls).toEqual([
      `provision:${failed.workspace_runtime_id}`,
      `provision:${failed.workspace_runtime_id}`
    ])
    const audits = await store.listAgentWorkspaceAudits(1, failed.workspace_runtime_id)
    expect(audits).toEqual(expect.arrayContaining([
      expect.objectContaining({ operation: 'failed', outcome: 'failed', actor_user_id: 7 }),
      expect.objectContaining({ operation: 'created', outcome: 'succeeded', actor_user_id: 7 })
    ]))
  })

  it('rejects access from a different user in the same EasyDo workspace', async () => {
    const store = createMemoryRuntimeStore()
    const service = new AgentWorkspaceService(store, fakeProvider(), {
      now: () => '2026-07-12T00:00:00.000Z'
    })
    const workspace = await service.ensureDefault(actor(7))

    await expect(service.get(actor(8), workspace.workspace_runtime_id)).rejects.toMatchObject({
      code: 'agent_workspace_access_denied',
      status: 403
    } satisfies Partial<RuntimeDomainError>)
  })

  it('persists pause resume and recycle lifecycle transitions', async () => {
    let tick = 0
    const store = createMemoryRuntimeStore()
    const provider = fakeProvider()
    const service = new AgentWorkspaceService(store, provider, {
      now: () => new Date(Date.UTC(2026, 6, 12, 0, 0, tick++)).toISOString()
    })
    const workspace = await service.ensureDefault(actor())
    const identity = workspaceIdentity(workspace)

    await expect(service.pause(actor(), workspace.workspace_runtime_id)).resolves.toMatchObject({ status: 'paused' })
    await expect(service.resume(actor(), workspace.workspace_runtime_id)).resolves.toMatchObject({ status: 'ready' })
    const recycled = await service.recycle(actor(), workspace.workspace_runtime_id)
    expect(recycled).toMatchObject({
      status: 'ready',
      recycled_at: expect.any(String),
      sandbox_id: `sandbox-${workspace.workspace_runtime_id}`,
      lease_owner_instance_id: undefined,
      lease_expires_at: undefined,
      paused_at: undefined,
      error_code: undefined,
      error_msg: undefined
    })
    expect(workspaceIdentity(recycled)).toEqual(identity)
    await expect(service.connect(actor(), workspace.workspace_runtime_id)).resolves.toMatchObject({ status: 'ready' })

    expect(provider.calls).toEqual([
      `provision:${workspace.workspace_runtime_id}`,
      `pause:${workspace.workspace_runtime_id}`,
      `resume:${workspace.workspace_runtime_id}`,
      `recycle:${workspace.workspace_runtime_id}`,
      `provision:${workspace.workspace_runtime_id}`,
      `connect:${workspace.workspace_runtime_id}`
    ])
    const audits = await store.listAgentWorkspaceAudits(1, workspace.workspace_runtime_id)
    expect(audits.map((audit) => audit.operation)).toEqual(['created', 'paused', 'resumed', 'recycled', 'connected'])
  })

  it('rejects recycle while an Agent run owns a live workspace lease', async () => {
    const store = createMemoryRuntimeStore()
    const provider = fakeProvider()
    const service = new AgentWorkspaceService(store, provider, {
      now: () => '2026-07-12T00:00:00.000Z'
    })
    const workspace = await service.ensureDefault(actor())
    await store.claimAgentWorkspaceLease(
      workspace.workspace_id,
      workspace.workspace_runtime_id,
      'runtime-active',
      '2026-07-12T00:01:00.000Z',
      '2026-07-12T00:00:00.000Z'
    )

    await expect(service.recycle(actor(), workspace.workspace_runtime_id)).rejects.toMatchObject({
      code: 'agent_workspace_in_use',
      status: 409
    } satisfies Partial<RuntimeDomainError>)
    expect(provider.calls).toEqual([`provision:${workspace.workspace_runtime_id}`])
    await expect(service.get(actor(), workspace.workspace_runtime_id)).resolves.toMatchObject({
      status: 'ready',
      lease_owner_instance_id: 'runtime-active'
    })
  })

  it('persists a failed state and audit when replacement provisioning fails', async () => {
    const store = createMemoryRuntimeStore()
    let provisions = 0
    const service = new AgentWorkspaceService(store, {
      async provision(workspace) {
        provisions += 1
        if (provisions === 2) throw new Error('replacement unavailable')
        return { sandbox_id: `sandbox-${workspace.workspace_runtime_id}` }
      },
      async connect() {},
      async pause() {},
      async resume() {},
      async recycle() {}
    }, { now: () => '2026-07-12T00:00:00.000Z' })
    const workspace = await service.ensureDefault(actor())

    await expect(service.recycle(actor(), workspace.workspace_runtime_id)).rejects.toMatchObject({
      code: 'agent_workspace_reprovision_failed',
      status: 502
    } satisfies Partial<RuntimeDomainError>)
    await expect(service.get(actor(), workspace.workspace_runtime_id)).resolves.toMatchObject({
      status: 'failed',
      error_code: 'agent_workspace_reprovision_failed',
      error_msg: 'replacement unavailable',
      lease_owner_instance_id: undefined,
      lease_expires_at: undefined
    })
    const audits = await store.listAgentWorkspaceAudits(1, workspace.workspace_runtime_id)
    const failedAudit = audits.find((audit) => audit.operation === 'failed')
    expect(failedAudit).toMatchObject({
      operation: 'failed',
      outcome: 'failed',
      details: { phase: 'provision', error_code: 'agent_workspace_reprovision_failed' }
    })
  })

  it('persists a failed state and audit when sandbox recycling fails', async () => {
    const store = createMemoryRuntimeStore()
    const service = new AgentWorkspaceService(store, {
      async provision(workspace) { return { sandbox_id: `sandbox-${workspace.workspace_runtime_id}` } },
      async connect() {},
      async pause() {},
      async resume() {},
      async recycle() { throw new Error('docker unavailable') }
    }, { now: () => '2026-07-12T00:00:00.000Z' })
    const workspace = await service.ensureDefault(actor())

    await expect(service.recycle(actor(), workspace.workspace_runtime_id)).rejects.toMatchObject({
      code: 'agent_workspace_recycle_failed',
      status: 502
    } satisfies Partial<RuntimeDomainError>)
    await expect(service.get(actor(), workspace.workspace_runtime_id)).resolves.toMatchObject({
      status: 'failed',
      error_code: 'agent_workspace_recycle_failed',
      error_msg: 'docker unavailable',
      lease_owner_instance_id: undefined,
      lease_expires_at: undefined
    })
    const audits = await store.listAgentWorkspaceAudits(1, workspace.workspace_runtime_id)
    const failedAudit = audits.find((audit) => audit.operation === 'failed')
    expect(failedAudit).toMatchObject({
      operation: 'failed',
      outcome: 'failed',
      details: { phase: 'recycle', error_code: 'agent_workspace_recycle_failed' }
    })
  })

  it.each(['ready', 'paused'] as const)('serializes concurrent recycle requests from %s', async (status) => {
    const store = createMemoryRuntimeStore()
    let releaseRecycle!: () => void
    const recycleGate = new Promise<void>((resolve) => { releaseRecycle = resolve })
    let recycleCalls = 0
    const service = new AgentWorkspaceService(store, {
      async provision(workspace) { return { sandbox_id: `sandbox-${workspace.workspace_runtime_id}` } },
      async connect() {},
      async pause() {},
      async resume() {},
      async recycle() {
        recycleCalls += 1
        await recycleGate
      }
    }, { now: () => '2026-07-12T00:00:00.000Z' })
    const workspace = await service.ensureDefault(actor())
    if (status === 'paused') await service.pause(actor(), workspace.workspace_runtime_id)

    const first = service.recycle(actor(), workspace.workspace_runtime_id)
    await expect.poll(() => recycleCalls).toBe(1)
    const second = service.recycle(actor(), workspace.workspace_runtime_id)
    await expect(second).rejects.toMatchObject({ status: 409 } satisfies Partial<RuntimeDomainError>)
    expect(recycleCalls).toBe(1)

    releaseRecycle()
    await expect(first).resolves.toMatchObject({ status: 'ready' })
    expect(recycleCalls).toBe(1)
  })
})
