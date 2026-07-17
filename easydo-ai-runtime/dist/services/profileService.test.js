import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js';
import { AgentProfileService } from './profileService.js';
import { scanSkillRepository } from './skillScanner.js';
const mcpClientRequest = vi.hoisted(() => vi.fn());
vi.mock('./skillScanner.js', () => ({
    scanSkillRepository: vi.fn(async () => [{
            key: 'review-skill',
            name: 'Review Skill',
            description: 'Review changes',
            version: 'commit-def',
            path: 'skills/review/SKILL.md',
            entry: 'skills/review/SKILL.md',
            content: '# Review Skill',
            tags: ['review'],
            manifest: {}
        }])
}));
vi.mock('./mcpStreamableHttpClient.js', () => ({
    resolveMcpClientConfigSecrets: vi.fn((config) => config),
    createMcpClient: vi.fn().mockImplementation(() => ({
        request: mcpClientRequest,
        close: vi.fn()
    }))
}));
const actor = {
    user_id: 7,
    username: 'developer',
    system_role: 'user',
    workspace_id: 11,
    workspace_role: 'developer',
    auth_session_id: 'auth-1'
};
beforeEach(() => {
    mcpClientRequest.mockReset();
});
function skillPayload(overrides = {}) {
    return {
        resource_kind: 'skill',
        resource_key: 'review-skill',
        name: 'Review Skill',
        version: 'commit-abc',
        status: 'active',
        spec: { instructions: 'Review carefully.' },
        endpoint: {},
        secret_ref: { credential_id: 23 },
        tags: ['review'],
        ...overrides
    };
}
function mcpPayload(overrides = {}) {
    return {
        resource_kind: 'mcp_server',
        resource_key: 'ops-mcp',
        name: 'Ops MCP',
        version: 'mcp-abc',
        status: 'active',
        spec: {
            discovered_tools: [
                { name: 'easydo_pipeline_list', operation_type: 'read' },
                { name: 'easydo_pipeline_trigger', operation_type: 'write' }
            ],
            tool_permissions: {
                tools: {
                    easydo_pipeline_list: 'allow',
                    easydo_pipeline_trigger: 'request'
                }
            }
        },
        endpoint: {},
        secret_ref: {},
        tags: ['mcp-server'],
        ...overrides
    };
}
describe('AgentProfileService resource revisions', () => {
    it('atomically creates and updates immutable resource revisions', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const created = await service.createResource(actor, skillPayload());
        const initialVersions = await store.listAgentResourceVersions(actor.workspace_id, created.id);
        expect(initialVersions).toHaveLength(1);
        expect(initialVersions[0]).toMatchObject({
            revision: 1,
            source_version: 'commit-abc',
            snapshot: {
                name: 'Review Skill',
                secret_ref: { credential_id: 23, configured: true, masked: true }
            }
        });
        expect(initialVersions[0]?.snapshot_digest).toMatch(/^sha256:/);
        await service.updateResource(actor, created.id, {
            name: 'Review Skill v2',
            version: 'commit-def',
            spec: { instructions: 'Review more carefully.' }
        });
        const versions = await store.listAgentResourceVersions(actor.workspace_id, created.id);
        expect(versions.map((version) => version.revision)).toEqual([2, 1]);
        expect(versions[0]).toMatchObject({
            source_version: 'commit-def',
            snapshot: { name: 'Review Skill v2', spec: { instructions: 'Review more carefully.' } }
        });
        expect(versions[1]).toMatchObject({
            source_version: 'commit-abc',
            snapshot: { name: 'Review Skill', spec: { instructions: 'Review carefully.' } }
        });
    });
    it('appends a new immutable revision after a successful Skill scan', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const created = await service.createResource(actor, skillPayload());
        const result = await service.scanResource(actor, created.id);
        expect(scanSkillRepository).toHaveBeenCalledOnce();
        expect(result.discovered_skills).toHaveLength(1);
        const versions = await store.listAgentResourceVersions(actor.workspace_id, created.id);
        expect(versions.map((version) => version.revision)).toEqual([2, 1]);
        expect(versions[0]?.snapshot.spec).toMatchObject({
            discovered_skills: [expect.objectContaining({ key: 'review-skill' })],
            last_scan: expect.objectContaining({ status: 'success', discovered_count: 1 })
        });
    });
    it('appends a new immutable revision after a successful MCP scan', async () => {
        mcpClientRequest.mockResolvedValueOnce({
            tools: [
                { name: 'ops.read', description: 'Read ops state', operationType: 'read', inputSchema: { type: 'object', properties: {} } },
                { name: 'ops.restart', description: 'Restart service', operation_type: 'write', input_schema: { type: 'object', properties: {} } }
            ]
        });
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const created = await service.createResource(actor, mcpPayload({
            spec: {
                mcpServers: {
                    ops: {
                        type: 'streamable_http',
                        url: 'http://mcp.local/mcp',
                        timeout_ms: 2500
                    }
                },
                discovered_tools: [],
                tool_permissions: { tools: {} }
            }
        }));
        const result = await service.scanResource(actor, created.id);
        expect(mcpClientRequest).toHaveBeenCalledWith('tools/list', {}, expect.stringContaining('mcp_tools_'), expect.objectContaining({ mcp_server_id: 'ops' }));
        expect(result.discovered_tools).toEqual([
            expect.objectContaining({ name: 'ops.read', operation_type: 'read' }),
            expect.objectContaining({ name: 'ops.restart', operation_type: 'write' })
        ]);
        const versions = await store.listAgentResourceVersions(actor.workspace_id, created.id);
        expect(versions.map((version) => version.revision)).toEqual([2, 1]);
        expect(versions[0]?.snapshot.spec).toMatchObject({
            discovered_tools: [
                expect.objectContaining({ name: 'ops.read', operation_type: 'read' }),
                expect.objectContaining({ name: 'ops.restart', operation_type: 'write' })
            ],
            last_scan: expect.objectContaining({ status: 'success', discovered_count: 2, capability: 'mcp' })
        });
    });
    it('pins Profile refs to exact immutable revisions and publishes the pinned snapshot', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload({
            spec: { instructions: 'P114_OLD_RESOURCE_MARKER' }
        }));
        const firstVersion = (await store.listAgentResourceVersions(actor.workspace_id, resource.id))[0];
        const profile = await service.createProfile(actor, {
            name: 'Pinned Resource Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: resource.id }],
            status: 'draft'
        });
        expect(profile.skills[0]).toMatchObject({
            resource_version_id: firstVersion?.resource_version_id,
            resource_version: 'commit-abc',
            snapshot_digest: firstVersion?.snapshot_digest
        });
        await service.updateResource(actor, resource.id, {
            version: 'commit-def',
            spec: {}
        });
        const published = await service.publishProfile(actor, profile.id, {});
        expect(published.snapshot.skills[0]).toMatchObject({
            resource_version_id: firstVersion?.resource_version_id,
            snapshot_digest: firstVersion?.snapshot_digest
        });
        expect(published.snapshot.frozen_resources?.skills[0]?.spec).toMatchObject({
            instructions: 'P114_OLD_RESOURCE_MARKER'
        });
    });
    it('persists profile tool_policy and freezes it into published snapshots', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const profile = await service.createProfile(actor, {
            name: 'Tool Policy Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            tool_policy: {
                rules: [{ id: 'allow-safe-read', tool_name: 'safe.read', operation_type: 'read', decision: 'allow' }]
            }
        });
        expect(profile.tool_policy).toMatchObject({
            rules: [expect.objectContaining({ id: 'allow-safe-read', decision: 'allow' })]
        });
        const published = await service.publishProfile(actor, profile.id, {});
        expect(published.snapshot.tool_policy).toMatchObject({
            rules: [expect.objectContaining({ id: 'allow-safe-read', tool_name: 'safe.read' })]
        });
    });
    it('copies MCP resource tool permissions into profile refs when the ref has no override', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, mcpPayload());
        const version = (await store.listAgentResourceVersions(actor.workspace_id, resource.id))[0];
        const profile = await service.createProfile(actor, {
            name: 'MCP Permission Snapshot Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            mcp_servers: [{ resource_type: 'mcp_server', resource_id: resource.id }]
        });
        expect(profile.mcp_servers[0]).toMatchObject({
            resource_version_id: version?.resource_version_id,
            config: {
                tool_permissions: {
                    tools: {
                        easydo_pipeline_list: 'allow',
                        easydo_pipeline_trigger: 'request'
                    }
                }
            }
        });
        const published = await service.publishProfile(actor, profile.id, {});
        expect(published.snapshot.mcp_servers[0]?.config).toMatchObject(profile.mcp_servers[0]?.config || {});
    });
    it('keeps explicit MCP profile ref tool permission overrides when repinning', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, mcpPayload());
        const profile = await service.createProfile(actor, {
            name: 'MCP Permission Override Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            mcp_servers: [{
                    resource_type: 'mcp_server',
                    resource_id: resource.id,
                    config: {
                        tool_permissions: {
                            tools: { easydo_pipeline_trigger: 'deny' }
                        }
                    }
                }]
        });
        expect(profile.mcp_servers[0]?.config).toMatchObject({
            tool_permissions: {
                tools: { easydo_pipeline_trigger: 'deny' }
            }
        });
    });
    it('rejects an explicit resource version that belongs to another resource', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const selected = await service.createResource(actor, skillPayload());
        const other = await service.createResource(actor, skillPayload({
            resource_key: 'other-skill',
            name: 'Other Skill'
        }));
        const otherVersion = (await store.listAgentResourceVersions(actor.workspace_id, other.id))[0];
        await expect(service.createProfile(actor, {
            name: 'Invalid Version Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{
                    resource_type: 'skill',
                    resource_id: selected.id,
                    resource_version_id: otherVersion?.resource_version_id
                }]
        })).rejects.toMatchObject({ code: 'agent_profile_resource_version_not_found' });
    });
    it('resolves an explicit source version to the exact immutable revision', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload({
            spec: { instructions: 'P114_SOURCE_VERSION_V1' }
        }));
        const firstVersion = (await store.listAgentResourceVersions(actor.workspace_id, resource.id))[0];
        await service.updateResource(actor, resource.id, {
            version: 'commit-def',
            spec: { instructions: 'P114_SOURCE_VERSION_V2' }
        });
        const profile = await service.createProfile(actor, {
            name: 'Explicit Source Version Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: resource.id, resource_version: 'commit-abc' }]
        });
        expect(profile.skills[0]).toMatchObject({
            resource_version_id: firstVersion?.resource_version_id,
            resource_version: 'commit-abc',
            snapshot_digest: firstVersion?.snapshot_digest
        });
        const published = await service.publishProfile(actor, profile.id, {});
        expect(published.snapshot.frozen_resources?.skills[0]?.spec).toMatchObject({
            instructions: 'P114_SOURCE_VERSION_V1'
        });
    });
    it('rejects contradictory resource version ID and source version selectors', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload());
        const firstVersion = (await store.listAgentResourceVersions(actor.workspace_id, resource.id))[0];
        await service.updateResource(actor, resource.id, { version: 'commit-def' });
        await expect(service.createProfile(actor, {
            name: 'Conflicting Resource Selector Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{
                    resource_type: 'skill',
                    resource_id: resource.id,
                    resource_version_id: firstVersion?.resource_version_id,
                    resource_version: 'commit-def'
                }]
        })).rejects.toMatchObject({
            code: 'agent_profile_resource_version_selector_conflict',
            status: 409
        });
    });
    it.each([
        ['skill', 'skills'],
        ['mcp_server', 'mcp_servers']
    ])('rejects an ambiguous %s source version', async (resourceKind, section) => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload({
            resource_kind: resourceKind,
            resource_key: `${resourceKind}-ambiguous`,
            name: `${resourceKind} Ambiguous`
        }));
        await service.updateResource(actor, resource.id, {
            name: `${resourceKind} Ambiguous v2`,
            version: 'commit-abc'
        });
        await expect(service.createProfile(actor, {
            name: `${resourceKind} Ambiguous Agent`,
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            [section]: [{ resource_type: resourceKind, resource_id: resource.id, resource_version: 'commit-abc' }]
        })).rejects.toMatchObject({
            code: 'agent_profile_resource_version_ambiguous',
            status: 409
        });
    });
    it('keeps numeric resource IDs separate from string resource keys', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const numericIDResource = await service.createResource(actor, skillPayload({
            resource_key: 'numeric-id-owner',
            name: 'Numeric ID Owner',
            spec: { instructions: 'P114_NUMERIC_ID_RESOURCE' }
        }));
        const stringKeyResource = await service.createResource(actor, skillPayload({
            resource_key: String(numericIDResource.id),
            name: 'String Key Owner',
            spec: { instructions: 'P114_STRING_KEY_RESOURCE' }
        }));
        const numericVersion = (await store.listAgentResourceVersions(actor.workspace_id, numericIDResource.id))[0];
        const stringVersion = (await store.listAgentResourceVersions(actor.workspace_id, stringKeyResource.id))[0];
        const numericProfile = await service.createProfile(actor, {
            name: 'Numeric Resource Selector Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: numericIDResource.id }]
        });
        const stringProfile = await service.createProfile(actor, {
            name: 'String Resource Selector Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: String(numericIDResource.id) }]
        });
        expect(numericProfile.skills[0]?.resource_version_id).toBe(numericVersion?.resource_version_id);
        expect(stringProfile.skills[0]?.resource_version_id).toBe(stringVersion?.resource_version_id);
    });
    it('does not block deletion because another resource uses the numeric ID as a string key', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const numericIDResource = await service.createResource(actor, skillPayload({
            resource_key: 'delete-numeric-owner',
            name: 'Delete Numeric Owner'
        }));
        await service.createResource(actor, skillPayload({
            resource_key: String(numericIDResource.id),
            name: 'Delete String Key Owner'
        }));
        await service.createProfile(actor, {
            name: 'String Key Dependency Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: String(numericIDResource.id) }]
        });
        await expect(service.deleteResource(actor, numericIDResource.id)).resolves.toEqual({ deleted: true });
    });
    it('pins a Subagent to one deterministic Published version without following later publishes', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const child = await service.createProfile(actor, {
            name: 'Pinned Child Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            prompt: { system: 'P114_CHILD_V1' },
            status: 'draft'
        });
        const childV1 = await service.publishProfile(actor, child.id, {});
        const parent = await service.createProfile(actor, {
            name: 'Pinned Parent Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            subagents: [{ resource_type: 'subagent_profile', resource_id: child.id }],
            status: 'draft'
        });
        expect(parent.subagents[0]).toMatchObject({
            resource_version_id: childV1.profile_version_id,
            snapshot_digest: childV1.snapshot_hash
        });
        await service.updateProfile(actor, child.id, { prompt: { system: 'P114_CHILD_V2' } });
        await service.publishProfile(actor, child.id, {});
        const parentVersion = await service.publishProfile(actor, parent.id, {});
        expect(parentVersion.snapshot.frozen_resources?.subagents[0]).toMatchObject({
            profile_version_id: childV1.profile_version_id,
            snapshot_hash: childV1.snapshot_hash,
            snapshot: { prompt: { system: 'P114_CHILD_V1' } }
        });
    });
    it('selects Subagents by logical Published version rather than physical version ID', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const primer = await service.createProfile(actor, {
            name: 'Version Sequence Primer',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        await service.publishProfile(actor, primer.id, {});
        const child = await service.createProfile(actor, {
            name: 'Logical Version Child',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            prompt: { system: 'P114_LOGICAL_CHILD_V1' }
        });
        const childV1 = await service.publishProfile(actor, child.id, {});
        await service.updateProfile(actor, child.id, { prompt: { system: 'P114_LOGICAL_CHILD_V2' } });
        const childV2 = await service.publishProfile(actor, child.id, {});
        const parent = await service.createProfile(actor, {
            name: 'Logical Version Parent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            subagents: [{ resource_type: 'subagent_profile', resource_id: child.id, resource_version: '1' }]
        });
        expect(parent.subagents[0]).toMatchObject({
            resource_version_id: childV1.profile_version_id,
            resource_version: '1',
            snapshot_digest: childV1.snapshot_hash
        });
        expect(childV2.profile_version_id).not.toBe(childV2.version);
    });
    it('rejects a missing logical Subagent version even when it equals a physical version ID', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const primer = await service.createProfile(actor, {
            name: 'Missing Version Sequence Primer',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        await service.publishProfile(actor, primer.id, {});
        const child = await service.createProfile(actor, {
            name: 'Missing Logical Version Child',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        await service.publishProfile(actor, child.id, {});
        await service.updateProfile(actor, child.id, { prompt: { system: 'second version' } });
        const childV2 = await service.publishProfile(actor, child.id, {});
        expect(childV2.profile_version_id).toBeGreaterThan(childV2.version);
        await expect(service.createProfile(actor, {
            name: 'Missing Logical Version Parent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            subagents: [{
                    resource_type: 'subagent_profile',
                    resource_id: child.id,
                    resource_version: String(childV2.profile_version_id)
                }]
        })).rejects.toMatchObject({
            code: 'agent_profile_subagent_version_not_found',
            status: 404
        });
    });
    it('rejects contradictory Subagent physical and logical version selectors', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const child = await service.createProfile(actor, {
            name: 'Conflicting Selector Child',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        const childV1 = await service.publishProfile(actor, child.id, {});
        await service.updateProfile(actor, child.id, { prompt: { system: 'second version' } });
        await service.publishProfile(actor, child.id, {});
        await expect(service.createProfile(actor, {
            name: 'Conflicting Selector Parent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            subagents: [{
                    resource_type: 'subagent_profile',
                    resource_id: child.id,
                    resource_version_id: childV1.profile_version_id,
                    resource_version: '2'
                }]
        })).rejects.toMatchObject({
            code: 'agent_profile_subagent_version_selector_conflict',
            status: 409
        });
    });
    it('returns the persisted version when an unchanged Profile snapshot is published again', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const profile = await service.createProfile(actor, {
            name: 'Idempotent Publish Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        const first = await service.publishProfile(actor, profile.id, { change_summary: 'first publish' });
        const repeated = await service.publishProfile(actor, profile.id, { change_summary: 'repeated publish' });
        expect(repeated.profile_version_id).toBe(first.profile_version_id);
        expect(repeated.version).toBe(first.version);
        await expect(store.listProfileVersions(actor.workspace_id, profile.id)).resolves.toEqual([first]);
    });
    it('serializes concurrent unchanged publishes without returning phantom versions', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const profile = await service.createProfile(actor, {
            name: 'Concurrent Publish Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        const [left, right] = await Promise.all([
            service.publishProfile(actor, profile.id, { change_summary: 'left' }),
            service.publishProfile(actor, profile.id, { change_summary: 'right' })
        ]);
        expect(right.profile_version_id).toBe(left.profile_version_id);
        await expect(store.listProfileVersions(actor.workspace_id, profile.id)).resolves.toHaveLength(1);
    });
    it('blocks resource deletion for live Draft refs but treats frozen Published refs as historical', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload());
        const profile = await service.createProfile(actor, {
            name: 'Resource Dependency Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: resource.id }]
        });
        await expect(service.deleteResource(actor, resource.id)).rejects.toMatchObject({
            code: 'agent_resource_in_use',
            status: 409,
            details: {
                dependencies: expect.arrayContaining([
                    expect.objectContaining({ dependency_class: 'blocking_live_ref', profile_id: profile.id })
                ])
            }
        });
        await service.publishProfile(actor, profile.id, {});
        await service.updateProfile(actor, profile.id, { skills: [] });
        const dependencies = await service.getResourceDependencies(actor, resource.id);
        expect(dependencies.dependencies).toEqual(expect.arrayContaining([
            expect.objectContaining({ dependency_class: 'historical_snapshot', profile_id: profile.id })
        ]));
        await expect(service.deleteResource(actor, resource.id)).resolves.toEqual({ deleted: true });
    });
    it('maps a resource reference created after the dependency preview to an in-use conflict', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload());
        const guardedDelete = store.deleteAgentResourceGuarded.bind(store);
        store.deleteAgentResourceGuarded = async (workspaceID, resourceID) => {
            await service.createProfile(actor, {
                name: 'Concurrent Resource Ref Agent',
                provider: { provider_id: 'openrouter' },
                model: { provider_model_key: 'openrouter/test-model' },
                skills: [{ resource_type: 'skill', resource_id: resource.id }]
            });
            return guardedDelete(workspaceID, resourceID);
        };
        await expect(service.deleteResource(actor, resource.id)).rejects.toMatchObject({
            code: 'agent_resource_in_use',
            status: 409,
            details: {
                dependencies: expect.arrayContaining([
                    expect.objectContaining({ dependency_class: 'blocking_live_ref' })
                ])
            }
        });
        await expect(store.getAgentResource(actor.workspace_id, resource.id)).resolves.toBeDefined();
    });
    it('blocks disabling or archiving a resource while a Profile draft references it', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const resource = await service.createResource(actor, skillPayload());
        await service.createProfile(actor, {
            name: 'Live Resource Ref Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            skills: [{ resource_type: 'skill', resource_id: resource.id }]
        });
        const dependencies = await service.getResourceDependencies(actor, resource.id);
        expect(dependencies.operations).toMatchObject({
            disable: { allowed: false },
            archive: { allowed: false }
        });
        await expect(service.updateResource(actor, resource.id, { status: 'disabled' })).rejects.toMatchObject({
            code: 'agent_resource_in_use',
            status: 409
        });
        await expect(service.updateResource(actor, resource.id, { status: 'archived' })).rejects.toMatchObject({
            code: 'agent_resource_in_use',
            status: 409
        });
    });
    it('allows hard deletion only for an unpublished, unreferenced Profile', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const child = await service.createProfile(actor, {
            name: 'Profile Dependency Child',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        const parent = await service.createProfile(actor, {
            name: 'Profile Dependency Parent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            subagents: [{ resource_type: 'subagent_profile', resource_id: child.id }]
        });
        await expect(service.deleteProfile(actor, child.id)).rejects.toMatchObject({
            code: 'agent_profile_in_use',
            status: 409,
            details: {
                dependencies: expect.arrayContaining([
                    expect.objectContaining({ dependency_class: 'blocking_live_ref', profile_id: parent.id })
                ])
            }
        });
        await service.updateProfile(actor, parent.id, { subagents: [] });
        await service.publishProfile(actor, child.id, {});
        await expect(service.deleteProfile(actor, child.id)).rejects.toMatchObject({
            code: 'agent_profile_in_use',
            details: {
                dependencies: expect.arrayContaining([
                    expect.objectContaining({ dependency_class: 'published_version', profile_id: child.id })
                ])
            }
        });
        await expect(service.deleteProfile(actor, parent.id)).resolves.toEqual({ deleted: true });
    });
    it('reports Profile disable operations from dependency analysis', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const child = await service.createProfile(actor, {
            name: 'Disable Dependency Child',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        const parent = await service.createProfile(actor, {
            name: 'Disable Dependency Parent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            subagents: [{ resource_type: 'subagent_profile', resource_id: child.id }]
        });
        const blocked = await service.getProfileDependencies(actor, child.id);
        expect(blocked.operations).toMatchObject({
            disable: { allowed: false }
        });
        await service.updateProfile(actor, parent.id, { subagents: [] });
        const allowed = await service.getProfileDependencies(actor, child.id);
        expect(allowed.operations).toMatchObject({
            disable: { allowed: true }
        });
    });
    it('maps a parent reference created after the dependency preview to a Profile in-use conflict', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        const child = await service.createProfile(actor, {
            name: 'Concurrent Child Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' }
        });
        const guardedDelete = store.deleteProfileGuarded.bind(store);
        store.deleteProfileGuarded = async (workspaceID, profileID) => {
            await service.createProfile(actor, {
                name: 'Concurrent Parent Agent',
                provider: { provider_id: 'openrouter' },
                model: { provider_model_key: 'openrouter/test-model' },
                subagents: [{ resource_type: 'subagent_profile', resource_id: child.id }]
            });
            return guardedDelete(workspaceID, profileID);
        };
        await expect(service.deleteProfile(actor, child.id)).rejects.toMatchObject({
            code: 'agent_profile_in_use',
            status: 409,
            details: {
                dependencies: expect.arrayContaining([
                    expect.objectContaining({ dependency_class: 'blocking_live_ref' })
                ])
            }
        });
        await expect(store.getProfile(actor.workspace_id, child.id)).resolves.toBeDefined();
    });
    it('rejects plaintext Provider credentials before saving a Profile Draft', async () => {
        const store = createMemoryRuntimeStore();
        const service = new AgentProfileService(store);
        await expect(service.createProfile(actor, {
            name: 'Unsafe Credential Agent',
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/test-model' },
            provider_credential_ref: { secret_ref: { api_key: 'must-not-persist' } }
        })).rejects.toMatchObject({
            code: 'provider_credential_reference_required',
            status: 400
        });
        expect(await store.listProfiles(actor.workspace_id)).toEqual([]);
    });
});
