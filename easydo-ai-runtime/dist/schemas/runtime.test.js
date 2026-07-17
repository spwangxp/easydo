import { describe, expect, it } from 'vitest';
import { agentResourceVersionSchema } from './runtime.js';
describe('agentResourceVersionSchema', () => {
    it('accepts canonical immutable resource revision fields', () => {
        const parsed = agentResourceVersionSchema.parse({
            resource_version_id: 101,
            resource_id: 7,
            workspace_id: 11,
            revision: 3,
            source_version: 'v1.2.3',
            snapshot_digest: `sha256:${'a'.repeat(64)}`,
            snapshot: {
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
                created_by: 5
            },
            created_by: 5,
            created_at: '2026-07-13T00:00:00.000Z'
        });
        expect(parsed.revision).toBe(3);
    });
});
