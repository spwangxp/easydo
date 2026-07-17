import { createHash, randomUUID } from 'node:crypto';
import { RuntimeDomainError } from '../services/runtimeErrors.js';
export class AgentWorkspaceService {
    store;
    provider;
    now;
    defaultImage;
    defaultProviderEndpoint;
    constructor(store, provider, options = {}) {
        this.store = store;
        this.provider = provider;
        this.now = options.now ?? (() => new Date().toISOString());
        this.defaultImage = options.defaultImage || 'easydo-ai-workspace:latest';
        this.defaultProviderEndpoint = options.defaultProviderEndpoint || '';
    }
    async ensureDefault(actor) {
        const timestamp = this.now();
        const workspaceRuntimeID = workspaceRuntimeIDFor(actor.workspace_id, actor.user_id, 'default');
        const workspace = {
            id: await this.store.nextAgentWorkspaceId(),
            workspace_runtime_id: workspaceRuntimeID,
            workspace_id: actor.workspace_id,
            owner_user_id: actor.user_id,
            workspace_key: 'default',
            provider: 'docker',
            provider_endpoint: this.defaultProviderEndpoint || undefined,
            status: 'provisioning',
            image: this.defaultImage,
            root_path: '/workspace',
            repository: {},
            network_policy: { mode: 'none', allow_hosts: [] },
            secret_refs: [],
            quota: { cpu_cores: 1, memory_mb: 1024, disk_mb: 10240, pids: 256 },
            snapshot_hash: '',
            lease_epoch: 0,
            provision_epoch: 0,
            state_version: 0,
            created_at: timestamp,
            updated_at: timestamp
        };
        workspace.snapshot_hash = workspaceSnapshotHash(workspace);
        const canonical = await this.store.createOrGetDefaultAgentWorkspace(workspace);
        if (canonical.workspace.status === 'ready' || canonical.workspace.status === 'paused' || canonical.workspace.status === 'recycling') {
            return canonical.workspace;
        }
        return this.provisionDefault(canonical.workspace, actor.user_id);
    }
    async provisionDefault(workspace, actorUserID) {
        const observedAt = this.now();
        const claimed = await this.store.claimAgentWorkspaceProvision({
            workspace_id: workspace.workspace_id,
            workspace_runtime_id: workspace.workspace_runtime_id,
            owner_user_id: workspace.owner_user_id,
            instance_id: 'runtime:workspace-provision',
            lease_expires_at: new Date(Date.parse(observedAt) + 5 * 60_000).toISOString(),
            observed_at: observedAt
        });
        if (!claimed)
            return this.waitForDefaultProvision(workspace);
        try {
            const provisioned = await this.provider.provision(claimed);
            const ready = await this.store.completeAgentWorkspaceProvision({
                workspace_id: claimed.workspace_id,
                workspace_runtime_id: claimed.workspace_runtime_id,
                instance_id: claimed.provision_owner_instance_id || 'runtime:workspace-provision',
                provision_epoch: claimed.provision_epoch,
                sandbox_id: provisioned.sandbox_id,
                updated_at: this.now()
            });
            if (!ready)
                return this.waitForDefaultProvision(claimed);
            await this.safeAudit(ready, actorUserID, 'created', 'succeeded', { provider: ready.provider, sandbox_id: ready.sandbox_id });
            return ready;
        }
        catch (error) {
            const errorMessage = error instanceof Error ? error.message : String(error);
            const failed = await this.store.failAgentWorkspaceProvision({
                workspace_id: claimed.workspace_id,
                workspace_runtime_id: claimed.workspace_runtime_id,
                instance_id: claimed.provision_owner_instance_id || 'runtime:workspace-provision',
                provision_epoch: claimed.provision_epoch,
                error_code: 'agent_workspace_provision_failed',
                error_msg: errorMessage,
                updated_at: this.now()
            });
            if (failed) {
                await this.safeAudit(failed, actorUserID, 'failed', 'failed', { error_code: failed.error_code, error_msg: failed.error_msg });
            }
            throw new RuntimeDomainError('agent_workspace_provision_failed', errorMessage || 'Agent workspace provisioning failed', 502);
        }
    }
    async waitForDefaultProvision(workspace) {
        for (let attempt = 0; attempt < 100; attempt += 1) {
            const current = await this.store.getAgentWorkspace(workspace.workspace_id, workspace.workspace_runtime_id);
            if (!current)
                break;
            if (current.status === 'ready' || current.status === 'paused')
                return current;
            if (current.status === 'failed')
                return current;
            await new Promise((resolve) => setTimeout(resolve, 10));
        }
        const current = await this.store.getAgentWorkspace(workspace.workspace_id, workspace.workspace_runtime_id);
        if (current)
            return current;
        throw new RuntimeDomainError('agent_workspace_provision_in_progress', 'Agent workspace provisioning is still in progress', 409);
    }
    async get(actor, workspaceRuntimeID) {
        const workspace = await this.store.getAgentWorkspace(actor.workspace_id, workspaceRuntimeID);
        if (!workspace)
            throw new RuntimeDomainError('agent_workspace_not_found', 'Agent workspace not found', 404);
        if (workspace.owner_user_id !== actor.user_id) {
            throw new RuntimeDomainError('agent_workspace_access_denied', 'Agent workspace belongs to another user', 403);
        }
        return workspace;
    }
    async list(actor) {
        return this.store.listAgentWorkspaces(actor.workspace_id, actor.user_id);
    }
    async listAudits(actor, workspaceRuntimeID) {
        await this.get(actor, workspaceRuntimeID);
        return this.store.listAgentWorkspaceAudits(actor.workspace_id, workspaceRuntimeID);
    }
    async connect(actor, workspaceRuntimeID) {
        const workspace = await this.requireStatus(actor, workspaceRuntimeID, ['ready']);
        await this.provider.connect(workspace);
        const connected = { ...workspace, last_connected_at: this.now(), updated_at: this.now() };
        await this.store.saveAgentWorkspace(connected);
        await this.audit(connected, actor.user_id, 'connected', 'succeeded');
        return connected;
    }
    async pause(actor, workspaceRuntimeID) {
        const current = await this.get(actor, workspaceRuntimeID);
        if (current.status === 'paused')
            return current;
        if (current.status !== 'ready')
            throw workspaceStateError(current, ['ready']);
        await this.provider.pause(current);
        const paused = {
            ...current,
            status: 'paused',
            lease_owner_instance_id: undefined,
            lease_expires_at: undefined,
            paused_at: this.now(),
            updated_at: this.now()
        };
        await this.store.saveAgentWorkspace(paused);
        await this.audit(paused, actor.user_id, 'paused', 'succeeded');
        return paused;
    }
    async resume(actor, workspaceRuntimeID) {
        const current = await this.get(actor, workspaceRuntimeID);
        if (current.status === 'ready')
            return current;
        if (current.status !== 'paused')
            throw workspaceStateError(current, ['paused']);
        await this.provider.resume(current);
        const resumed = {
            ...current,
            status: 'ready',
            paused_at: undefined,
            updated_at: this.now()
        };
        await this.store.saveAgentWorkspace(resumed);
        await this.audit(resumed, actor.user_id, 'resumed', 'succeeded');
        return resumed;
    }
    async recycle(actor, workspaceRuntimeID) {
        const current = await this.get(actor, workspaceRuntimeID);
        if (!['ready', 'paused', 'recycled', 'failed'].includes(current.status)) {
            throw workspaceStateError(current, ['ready', 'paused', 'recycled', 'failed']);
        }
        const observedAt = this.now();
        const recycling = await this.store.beginAgentWorkspaceRecycle(current.workspace_id, current.workspace_runtime_id, current.owner_user_id, `workspace-recycle:${randomUUID()}`, new Date(Date.parse(observedAt) + 5 * 60_000).toISOString(), observedAt);
        if (!recycling)
            throw workspaceInUseError(current);
        try {
            await this.provider.recycle(recycling);
        }
        catch (error) {
            await this.failRecycle(recycling, actor.user_id, 'recycle', 'agent_workspace_recycle_failed', error);
        }
        const recycledAt = this.now();
        let provisioned;
        try {
            provisioned = await this.provider.provision({ ...recycling, sandbox_id: undefined });
        }
        catch (error) {
            await this.failRecycle(recycling, actor.user_id, 'provision', 'agent_workspace_reprovision_failed', error, recycledAt);
        }
        const ready = {
            ...recycling,
            sandbox_id: provisioned.sandbox_id,
            status: 'ready',
            lease_owner_instance_id: undefined,
            lease_expires_at: undefined,
            paused_at: undefined,
            recycled_at: recycledAt,
            error_code: undefined,
            error_msg: undefined,
            updated_at: this.now()
        };
        await this.store.saveAgentWorkspace(ready);
        await this.audit(ready, actor.user_id, 'recycled', 'succeeded', { sandbox_id: ready.sandbox_id });
        return ready;
    }
    async failRecycle(workspace, actorUserID, phase, errorCode, error, recycledAt) {
        const errorMessage = error instanceof Error ? error.message : String(error);
        const failed = {
            ...workspace,
            status: 'failed',
            sandbox_id: undefined,
            lease_owner_instance_id: undefined,
            lease_expires_at: undefined,
            paused_at: undefined,
            recycled_at: recycledAt ?? workspace.recycled_at,
            error_code: errorCode,
            error_msg: errorMessage,
            updated_at: this.now()
        };
        await this.store.saveAgentWorkspace(failed);
        await this.audit(failed, actorUserID, 'failed', 'failed', {
            phase,
            error_code: errorCode,
            error_msg: errorMessage
        });
        throw new RuntimeDomainError(errorCode, errorMessage, 502);
    }
    async requireStatus(actor, workspaceRuntimeID, statuses) {
        const workspace = await this.get(actor, workspaceRuntimeID);
        if (!statuses.includes(workspace.status))
            throw workspaceStateError(workspace, statuses);
        return workspace;
    }
    async audit(workspace, actorUserID, operation, outcome, details = {}) {
        const createdAt = this.now();
        return this.store.appendAgentWorkspaceAudit({
            audit_id: randomUUID(),
            workspace_runtime_id: workspace.workspace_runtime_id,
            workspace_id: workspace.workspace_id,
            owner_user_id: workspace.owner_user_id,
            actor_user_id: actorUserID,
            operation,
            outcome,
            details,
            created_at: createdAt
        });
    }
    async safeAudit(workspace, actorUserID, operation, outcome, details = {}) {
        try {
            await this.audit(workspace, actorUserID, operation, outcome, details);
        }
        catch (error) {
            if (!(error instanceof Error))
                throw error;
        }
    }
}
function workspaceRuntimeIDFor(workspaceID, userID, key) {
    return `aws_w${workspaceID}_u${userID}_${key.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '') || 'default'}`;
}
function workspaceSnapshotHash(workspace) {
    return `sha256:${createHash('sha256').update(JSON.stringify({
        workspace_runtime_id: workspace.workspace_runtime_id,
        workspace_id: workspace.workspace_id,
        owner_user_id: workspace.owner_user_id,
        workspace_key: workspace.workspace_key,
        provider: workspace.provider,
        provider_endpoint: workspace.provider_endpoint || '',
        image: workspace.image,
        root_path: workspace.root_path,
        repository: workspace.repository,
        network_policy: workspace.network_policy,
        secret_refs: workspace.secret_refs,
        quota: workspace.quota
    })).digest('hex')}`;
}
function workspaceStateError(workspace, expected) {
    return new RuntimeDomainError('agent_workspace_not_ready', `Agent workspace ${workspace.workspace_runtime_id} is ${workspace.status}; expected ${expected.join(' or ')}`, 409, { workspace_status: workspace.status, expected_statuses: expected });
}
function workspaceInUseError(workspace) {
    return new RuntimeDomainError('agent_workspace_in_use', `Agent workspace ${workspace.workspace_runtime_id} is in use by an active run`, 409);
}
