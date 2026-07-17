import type { AIRuntimeRun, RuntimeActor } from '../domain/runtime.js'
import type { RuntimeStore } from '../store/memoryRuntimeStore.js'
import { RuntimeDomainError } from '../services/runtimeErrors.js'
import type { AgentWorkspaceService } from './workspaceService.js'
import { WorkspaceExecutionEnv, type WorkspaceExecutionClient } from './workspaceExecutionEnv.js'

export interface WorkspaceRuntimeBindingOptions {
  instanceID: string
  leaseMs: number
}

export class WorkspaceRuntimeBinding {
  constructor(
    private readonly store: RuntimeStore,
    private readonly service: Pick<AgentWorkspaceService, 'ensureDefault'>,
    private readonly executionClient: WorkspaceExecutionClient,
    private readonly options: WorkspaceRuntimeBindingOptions
  ) {}

  ensureDefault(actor: RuntimeActor) {
    return this.service.ensureDefault(actor)
  }

  async acquire(run: AIRuntimeRun) {
    const workspaceRuntimeID = run.agent_workspace_runtime_id
    const snapshotHash = run.agent_workspace_snapshot_hash
    if (!workspaceRuntimeID || !snapshotHash) {
      throw new RuntimeDomainError('agent_workspace_binding_missing', 'Runtime run is not bound to an Agent workspace snapshot', 409)
    }
    const session = await this.store.getSession(run.workspace_id, run.session_id)
    if (!session) throw new RuntimeDomainError('session_not_found', 'Session not found', 404)
    if (session.agent_workspace_runtime_id !== workspaceRuntimeID || session.agent_workspace_snapshot_hash !== snapshotHash) {
      throw new RuntimeDomainError('agent_workspace_binding_changed', 'Runtime run workspace binding does not match its Session snapshot', 409)
    }
    const workspace = await this.store.getAgentWorkspace(run.workspace_id, workspaceRuntimeID)
    if (!workspace) throw new RuntimeDomainError('agent_workspace_not_found', 'Agent workspace not found', 404)
    if (workspace.owner_user_id !== session.user_id) {
      throw new RuntimeDomainError('agent_workspace_access_denied', 'Agent workspace belongs to another user', 403)
    }
    if (workspace.status !== 'ready') {
      throw new RuntimeDomainError('agent_workspace_not_ready', `Agent workspace ${workspaceRuntimeID} is ${workspace.status}`, 409)
    }
    if (workspace.snapshot_hash !== snapshotHash) {
      throw new RuntimeDomainError('agent_workspace_snapshot_changed', 'Agent workspace configuration changed after Session creation', 409)
    }

    const instanceID = this.options.instanceID
    const claimed = await this.store.claimAgentWorkspaceLease(
      run.workspace_id,
      workspaceRuntimeID,
      instanceID,
      this.leaseExpiry()
    )
    if (!claimed) {
      throw new RuntimeDomainError('agent_workspace_lease_conflict', 'Agent workspace is executing on another Runtime instance', 409)
    }
    const leaseEpoch = claimed.lease_epoch
    let released = false
    return {
      executionEnv: new WorkspaceExecutionEnv(claimed, this.executionClient),
      renew: async () => Boolean(await this.store.renewAgentWorkspaceLease(
        run.workspace_id,
        workspaceRuntimeID,
        instanceID,
        leaseEpoch,
        this.leaseExpiry()
      )),
      release: async () => {
        if (released) return
        released = true
        await this.store.releaseAgentWorkspaceLease(run.workspace_id, workspaceRuntimeID, instanceID, leaseEpoch)
      }
    }
  }

  private leaseExpiry() {
    return new Date(Date.now() + Math.max(3_000, this.options.leaseMs)).toISOString()
  }
}
