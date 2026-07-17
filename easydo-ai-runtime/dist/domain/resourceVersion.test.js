import { describe, expect, it } from 'vitest';
import { createAgentResourceVersion } from './runtime.js';
function resource(overrides = {}) {
    return {
        id: 7,
        workspace_id: 11,
        resource_kind: 'mcp_server',
        resource_key: 'audit-mcp',
        resource_id: 'audit-mcp',
        name: 'Audit MCP',
        description: 'Read-only audit tools',
        version: 'v1.2.3',
        status: 'active',
        spec: {
            tools: [{ name: 'audit_query' }],
            api_key: 'spec-secret',
            token: 'plain-token-secret'
        },
        endpoint: {
            url: 'https://mcp.example.test',
            headers: {
                Authorization: 'Bearer endpoint-secret',
                'X-Api-Token': 'custom-header-secret',
                'X-Workspace': '11'
            }
        },
        secret_ref: {
            credential_id: 29,
            api_key: 'resolved-secret'
        },
        tags: ['mcp', 'audit'],
        created_by: 5,
        created_at: '2026-07-13T00:00:00.000Z',
        updated_at: '2026-07-13T00:01:00.000Z',
        ...overrides
    };
}
describe('AgentResourceVersion', () => {
    it('creates a stable sha256 digest from an unresolved secret-safe snapshot', () => {
        const first = createAgentResourceVersion({
            resourceVersionID: 101,
            revision: 3,
            resource: resource(),
            createdAt: '2026-07-13T00:02:00.000Z'
        });
        const reordered = createAgentResourceVersion({
            resourceVersionID: 102,
            revision: 3,
            resource: resource({
                endpoint: {
                    headers: {
                        'X-Workspace': '11',
                        'X-Api-Token': 'another-custom-header-secret',
                        Authorization: 'Bearer another-secret'
                    },
                    url: 'https://mcp.example.test'
                },
                spec: {
                    api_key: 'another-spec-secret',
                    token: 'another-plain-token-secret',
                    tools: [{ name: 'audit_query' }]
                },
                secret_ref: {
                    api_key: 'another-resolved-secret',
                    credential_id: 29
                }
            }),
            createdAt: '2026-07-13T00:03:00.000Z'
        });
        expect(first.snapshot_digest).toMatch(/^sha256:[a-f0-9]{64}$/);
        expect(first.snapshot_digest).toBe(reordered.snapshot_digest);
        expect(first.source_version).toBe('v1.2.3');
        expect(first.snapshot.secret_ref).toEqual({ credential_id: 29, configured: true, masked: true });
        expect(first.snapshot.spec.api_key).toBe('[REDACTED]');
        expect(first.snapshot.spec.token).toBe('[REDACTED]');
        expect(first.snapshot.endpoint.headers.Authorization).toBe('[REDACTED]');
        expect(first.snapshot.endpoint.headers['X-Api-Token']).toBe('[REDACTED]');
        expect(JSON.stringify(first.snapshot)).not.toContain('endpoint-secret');
        expect(JSON.stringify(first.snapshot)).not.toContain('resolved-secret');
    });
});
