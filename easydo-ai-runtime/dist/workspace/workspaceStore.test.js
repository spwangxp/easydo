import { describe, expect, it } from 'vitest';
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js';
function workspace(overrides = {}) {
    return {
        id: 1,
        workspace_runtime_id: 'aws_w1_u7_default',
        workspace_id: 1,
        owner_user_id: 7,
        workspace_key: 'default',
        provider: 'docker',
        provider_endpoint: '',
        sandbox_id: 'easydo-ai-w1-u7-default',
        status: 'ready',
        image: 'easydo-ai-workspace:latest',
        root_path: '/workspace',
        repository: {},
        network_policy: { mode: 'none', allow_hosts: [] },
        secret_refs: [],
        quota: { cpu_cores: 1, memory_mb: 1024, disk_mb: 10240, pids: 256 },
        snapshot_hash: 'sha256:workspace-1',
        lease_epoch: 0,
        provision_epoch: 0,
        state_version: 0,
        created_at: '2026-07-12T00:00:00.000Z',
        updated_at: '2026-07-12T00:00:00.000Z',
        ...overrides
    };
}
describe('AI Agent workspace store', () => {
    it('isolates workspace records by EasyDo workspace and preserves the owner', async () => {
        const store = createMemoryRuntimeStore();
        await store.saveAgentWorkspace(workspace());
        await store.saveAgentWorkspace(workspace({
            id: 2,
            workspace_runtime_id: 'aws_w2_u9_default',
            workspace_id: 2,
            owner_user_id: 9,
            snapshot_hash: 'sha256:workspace-2'
        }));
        await expect(store.getAgentWorkspace(1, 'aws_w1_u7_default')).resolves.toMatchObject({
            workspace_id: 1,
            owner_user_id: 7
        });
        await expect(store.getAgentWorkspace(1, 'aws_w2_u9_default')).resolves.toBeUndefined();
    });
    it('claims one workspace lease atomically and increments the epoch after expiry', async () => {
        const store = createMemoryRuntimeStore();
        await store.saveAgentWorkspace(workspace());
        const first = await store.claimAgentWorkspaceLease(1, 'aws_w1_u7_default', 'runtime-a', '2026-07-12T00:00:10.000Z', '2026-07-12T00:00:00.000Z');
        expect(first).toMatchObject({ lease_owner_instance_id: 'runtime-a', lease_epoch: 1 });
        await expect(store.claimAgentWorkspaceLease(1, 'aws_w1_u7_default', 'runtime-b', '2026-07-12T00:00:11.000Z', '2026-07-12T00:00:01.000Z')).resolves.toBeUndefined();
        const takeover = await store.claimAgentWorkspaceLease(1, 'aws_w1_u7_default', 'runtime-b', '2026-07-12T00:00:21.000Z', '2026-07-12T00:00:11.000Z');
        expect(takeover).toMatchObject({ lease_owner_instance_id: 'runtime-b', lease_epoch: 2 });
    });
    it.each(['ready', 'paused', 'failed'])('allows only one lifecycle recycle claim from %s', async (status) => {
        const store = createMemoryRuntimeStore();
        await store.saveAgentWorkspace(workspace({ status }));
        const first = await store.beginAgentWorkspaceRecycle(1, 'aws_w1_u7_default', 7, 'recycle-operation-a', '2026-07-12T00:05:00.000Z', '2026-07-12T00:00:00.000Z');
        expect(first).toMatchObject({
            status: 'recycling',
            lease_owner_instance_id: 'recycle-operation-a',
            lease_epoch: 1
        });
        await expect(store.beginAgentWorkspaceRecycle(1, 'aws_w1_u7_default', 7, 'recycle-operation-b', '2026-07-12T00:05:01.000Z', '2026-07-12T00:00:01.000Z')).resolves.toBeUndefined();
    });
    it('creates or gets one default workspace without overwriting an existing ready row', async () => {
        const store = createMemoryRuntimeStore();
        const created = await store.createOrGetDefaultAgentWorkspace(workspace({ status: 'provisioning', sandbox_id: undefined }));
        const existing = await store.createOrGetDefaultAgentWorkspace(workspace({
            id: 2,
            status: 'failed',
            sandbox_id: undefined,
            error_code: 'stale_failure',
            snapshot_hash: 'sha256:stale'
        }));
        expect(created).toMatchObject({ outcome: 'created', workspace: { id: 1, status: 'provisioning' } });
        expect(existing).toMatchObject({ outcome: 'existing', workspace: { id: 1, status: 'provisioning', snapshot_hash: 'sha256:workspace-1' } });
        await expect(store.listAgentWorkspaces(1, 7)).resolves.toHaveLength(1);
    });
    it('fences provisioning so stale success and stale failure cannot overwrite a newer ready workspace', async () => {
        const store = createMemoryRuntimeStore();
        await store.saveAgentWorkspace(workspace({ status: 'provisioning', sandbox_id: undefined }));
        const first = await store.claimAgentWorkspaceProvision({
            workspace_id: 1,
            workspace_runtime_id: 'aws_w1_u7_default',
            owner_user_id: 7,
            instance_id: 'runtime-a',
            lease_expires_at: '2026-07-12T00:05:00.000Z',
            observed_at: '2026-07-12T00:00:00.000Z'
        });
        expect(first).toMatchObject({ provision_owner_instance_id: 'runtime-a', provision_epoch: 1 });
        const ready = await store.completeAgentWorkspaceProvision({
            workspace_id: 1,
            workspace_runtime_id: 'aws_w1_u7_default',
            instance_id: 'runtime-a',
            provision_epoch: 1,
            sandbox_id: 'sandbox-a',
            updated_at: '2026-07-12T00:00:10.000Z'
        });
        expect(ready).toMatchObject({ status: 'ready', sandbox_id: 'sandbox-a', provision_owner_instance_id: undefined });
        await expect(store.failAgentWorkspaceProvision({
            workspace_id: 1,
            workspace_runtime_id: 'aws_w1_u7_default',
            instance_id: 'runtime-stale',
            provision_epoch: 1,
            error_code: 'stale_failure',
            error_msg: 'must not win',
            updated_at: '2026-07-12T00:00:11.000Z'
        })).resolves.toBeUndefined();
        await expect(store.completeAgentWorkspaceProvision({
            workspace_id: 1,
            workspace_runtime_id: 'aws_w1_u7_default',
            instance_id: 'runtime-stale',
            provision_epoch: 1,
            sandbox_id: 'sandbox-stale',
            updated_at: '2026-07-12T00:00:12.000Z'
        })).resolves.toBeUndefined();
        await expect(store.getAgentWorkspace(1, 'aws_w1_u7_default')).resolves.toMatchObject({ status: 'ready', sandbox_id: 'sandbox-a' });
    });
    it('preserves live lease fields when provisioning completes', async () => {
        const store = createMemoryRuntimeStore();
        await store.saveAgentWorkspace(workspace({ status: 'provisioning', sandbox_id: undefined }));
        const claimed = await store.claimAgentWorkspaceProvision({
            workspace_id: 1,
            workspace_runtime_id: 'aws_w1_u7_default',
            owner_user_id: 7,
            instance_id: 'runtime-a',
            lease_expires_at: '2026-07-12T00:05:00.000Z',
            observed_at: '2026-07-12T00:00:00.000Z'
        });
        await store.saveAgentWorkspace({
            ...claimed,
            lease_owner_instance_id: 'runtime-live',
            lease_epoch: 9,
            lease_expires_at: '2026-07-12T00:10:00.000Z'
        });
        await expect(store.completeAgentWorkspaceProvision({
            workspace_id: 1,
            workspace_runtime_id: 'aws_w1_u7_default',
            instance_id: 'runtime-a',
            provision_epoch: 1,
            sandbox_id: 'sandbox-a',
            updated_at: '2026-07-12T00:00:10.000Z'
        })).resolves.toMatchObject({
            status: 'ready',
            lease_owner_instance_id: 'runtime-live',
            lease_epoch: 9,
            lease_expires_at: '2026-07-12T00:10:00.000Z'
        });
    });
});
