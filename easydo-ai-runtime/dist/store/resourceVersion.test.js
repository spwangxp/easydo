import { describe, expect, it } from 'vitest';
import { createAgentResourceVersion } from '../domain/runtime.js';
import { createMemoryRuntimeStore } from './memoryRuntimeStore.js';
function resource() {
    return {
        id: 7,
        workspace_id: 11,
        resource_kind: 'skill',
        resource_key: 'review-skill',
        resource_id: 'review-skill',
        name: 'Review Skill',
        description: 'Review changes',
        version: 'commit-abc',
        status: 'active',
        spec: { instructions: 'Review carefully.' },
        endpoint: {},
        secret_ref: {},
        tags: ['skill'],
        created_by: 5,
        created_at: '2026-07-13T00:00:00.000Z',
        updated_at: '2026-07-13T00:00:00.000Z'
    };
}
describe('MemoryRuntimeStore Agent resource versions', () => {
    it('appends immutable revisions and rejects revision replacement', async () => {
        const store = createMemoryRuntimeStore();
        const first = createAgentResourceVersion({
            resourceVersionID: await store.nextAgentResourceVersionId(),
            revision: 1,
            resource: resource(),
            createdAt: '2026-07-13T00:01:00.000Z'
        });
        await store.appendAgentResourceVersion(first);
        first.snapshot.spec.instructions = 'mutated outside the store';
        await expect(store.getAgentResourceVersion(11, first.resource_version_id)).resolves.toMatchObject({
            revision: 1,
            snapshot: { spec: { instructions: 'Review carefully.' } }
        });
        const duplicateRevision = createAgentResourceVersion({
            resourceVersionID: await store.nextAgentResourceVersionId(),
            revision: 1,
            resource: resource(),
            createdAt: '2026-07-13T00:02:00.000Z'
        });
        await expect(store.appendAgentResourceVersion(duplicateRevision)).rejects.toThrow(/already exists/i);
        await expect(store.listAgentResourceVersions(11, 7)).resolves.toHaveLength(1);
    });
    it('does not mutate the resource head when its revision append fails', async () => {
        const store = createMemoryRuntimeStore();
        const initial = resource();
        const firstVersion = createAgentResourceVersion({
            resourceVersionID: await store.nextAgentResourceVersionId(),
            revision: 1,
            resource: initial,
            createdAt: '2026-07-13T00:01:00.000Z'
        });
        await store.createAgentResourceWithVersion(initial, firstVersion);
        const updated = { ...initial, name: 'Changed head' };
        const duplicateRevision = createAgentResourceVersion({
            resourceVersionID: await store.nextAgentResourceVersionId(),
            revision: 1,
            resource: updated,
            createdAt: '2026-07-13T00:02:00.000Z'
        });
        await expect(store.updateAgentResourceWithVersion(updated, duplicateRevision)).rejects.toThrow(/already exists/i);
        await expect(store.getAgentResource(11, 7)).resolves.toMatchObject({ name: 'Review Skill' });
        await expect(store.listAgentResourceVersions(11, 7)).resolves.toHaveLength(1);
    });
    it('rejects standalone revision snapshots that contain resolved secrets', async () => {
        const store = createMemoryRuntimeStore();
        const version = createAgentResourceVersion({
            resourceVersionID: await store.nextAgentResourceVersionId(),
            revision: 1,
            resource: resource(),
            createdAt: '2026-07-13T00:01:00.000Z'
        });
        version.snapshot.secret_ref = { api_key: 'must-not-persist' };
        await expect(store.appendAgentResourceVersion(version)).rejects.toThrow(/secret-safe/i);
        await expect(store.listAgentResourceVersions(11, 7)).resolves.toEqual([]);
    });
});
