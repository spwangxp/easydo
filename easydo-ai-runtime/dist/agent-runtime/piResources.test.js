import { describe, expect, it } from 'vitest';
import { buildPiHarnessResources, PiToolApprovalRequiredError } from './piResources.js';
function resource(overrides) {
    return {
        id: 1,
        workspace_id: 11,
        resource_kind: 'skill',
        resource_key: 'resource-1',
        resource_id: 'resource-1',
        name: 'Resource One',
        description: 'Resource description',
        version: 'latest',
        status: 'active',
        spec: {},
        endpoint: {},
        secret_ref: {},
        tags: [],
        created_by: 7,
        created_at: '2026-06-30T00:00:00.000Z',
        updated_at: '2026-06-30T00:00:00.000Z',
        ...overrides
    };
}
function profile(overrides) {
    return {
        id: 1,
        workspace_id: 11,
        name: 'Runtime Profile',
        description: '',
        profile_kind: 'assistant',
        context_tags: [],
        provider: {},
        binding: {},
        model: {},
        provider_credential_ref: {},
        inference: {},
        prompt: {},
        skills: [],
        subagents: [],
        mcp_servers: [],
        context_contract: {},
        input_schema: {},
        output_schema: {},
        tool_policy: {},
        memory_policy: {},
        confirmation_policy: {},
        response_mode: 'text',
        status: 'draft',
        created_by: 7,
        ...overrides
    };
}
describe('buildPiHarnessResources', () => {
    it('hides write, unknown, and subagent-spawn tools from a read_only Run catalog', () => {
        let grantChecks = 0;
        let executions = 0;
        const built = buildPiHarnessResources({
            runMode: 'read_only',
            profile: profile({
                mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }],
                subagents: [{ resource_type: 'subagent_profile', resource_id: 9 }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [
                            { name: 'read_tool', operation_type: 'read' },
                            { name: 'write_tool', operation_type: 'write' },
                            { name: 'unknown_tool' }
                        ] }
                })],
            subagents: [{ id: 9, name: 'Writer Agent', description: 'Writes data', profile_kind: 'worker', context_tags: [] }],
            decideTool: async () => {
                grantChecks += 1;
                return { approved: true, source: 'session_grant' };
            },
            executeTool: async () => {
                executions += 1;
                return { content: 'unexpected' };
            }
        });
        expect(built.tools.map((tool) => tool.name)).toEqual(['read_tool']);
        expect(built.summary.mcp_tools).toEqual([expect.objectContaining({ name: 'read_tool' })]);
        expect(grantChecks).toBe(0);
        expect(executions).toBe(0);
    });
    it('keeps the complete MCP and subagent catalog in write mode', () => {
        const built = buildPiHarnessResources({
            runMode: 'write',
            profile: profile({
                mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }],
                subagents: [{ resource_type: 'subagent_profile', resource_id: 9 }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [
                            { name: 'read_tool', operation_type: 'read' },
                            { name: 'write_tool', operation_type: 'write' },
                            { name: 'unknown_tool' }
                        ] }
                })],
            subagents: [{ id: 9, name: 'Writer Agent', description: 'Writes data', profile_kind: 'worker', context_tags: [] }]
        });
        expect(built.tools.map((tool) => tool.name)).toEqual(['read_tool', 'write_tool', 'unknown_tool', 'subagent.spawn']);
    });
    it('maps selected skill, MCP, and subagent resources into Pi harness resources and tools', () => {
        const built = buildPiHarnessResources({
            profile: profile({
                skills: [{ resource_type: 'skill', resource_id: 'skill-1', config: { auto_load: true } }],
                mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }],
                subagents: [{ resource_type: 'subagent_profile', resource_id: 9 }]
            }),
            resources: [
                resource({
                    id: 101,
                    resource_kind: 'skill',
                    resource_key: 'skill-1',
                    name: 'Debugging Skill',
                    description: 'Use for debugging',
                    spec: { instructions: 'Inspect evidence before changing code.' }
                }),
                resource({
                    id: 102,
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    name: 'Ops MCP',
                    description: 'Operations tools',
                    spec: {
                        discovered_tools: [{ name: 'easydo_resource_list', description: 'List resources', input_schema: { type: 'object', properties: {} } }]
                    }
                })
            ],
            subagents: [{ id: 9, name: 'Reviewer Agent', description: 'Reviews work', profile_kind: 'reviewer', context_tags: ['review'] }]
        });
        expect(built.skills).toEqual([expect.objectContaining({ name: 'Debugging Skill', content: '' })]);
        expect(built.tools.map((tool) => tool.name)).toEqual(['easydo_resource_list', 'subagent.spawn', 'easydo_skill_load']);
        expect(built.tools[0]).toMatchObject({ label: 'List resources', description: 'List resources' });
        expect(built.summary).toMatchObject({
            skill_count: 1,
            mcp_tool_count: 1,
            subagent_count: 1,
            skills: [expect.objectContaining({ key: 'skill-1', name: 'Debugging Skill' })],
            mcp_tools: [expect.objectContaining({ name: 'easydo_resource_list' })],
            subagents: [expect.objectContaining({ id: 9, name: 'Reviewer Agent' })]
        });
    });
    it('only includes full skill instructions for explicitly loaded skills', () => {
        const input = {
            profile: profile({
                skills: [{ resource_type: 'skill', resource_id: 'skill-1', config: { auto_load: true } }]
            }),
            resources: [
                resource({
                    id: 101,
                    resource_kind: 'skill',
                    resource_key: 'skill-1',
                    name: 'Debugging Skill',
                    description: 'Use for debugging',
                    spec: { instructions: 'Inspect evidence before changing code.' }
                })
            ]
        };
        expect(buildPiHarnessResources(input).skills[0]?.content).toBe('');
        expect(buildPiHarnessResources({ ...input, loadedSkillNames: ['Debugging Skill'] }).skills[0]?.content)
            .toBe('Inspect evidence before changing code.');
    });
    it('keeps numeric resource IDs separate from string resource keys', () => {
        const resources = [
            resource({ id: 1, resource_key: 'numeric-owner', resource_id: 'numeric-owner', name: 'Numeric Owner' }),
            resource({ id: 2, resource_key: '1', resource_id: '1', name: 'String Key Owner' })
        ];
        const numeric = buildPiHarnessResources({
            profile: profile({ skills: [{ resource_type: 'skill', resource_id: 1 }] }),
            resources
        });
        const stringKey = buildPiHarnessResources({
            profile: profile({ skills: [{ resource_type: 'skill', resource_id: '1' }] }),
            resources
        });
        expect(numeric.summary.skills).toEqual([expect.objectContaining({ name: 'Numeric Owner' })]);
        expect(stringKey.summary.skills).toEqual([expect.objectContaining({ name: 'String Key Owner' })]);
    });
    it('lets the model load an allowed skill dynamically and records its digest', async () => {
        const events = [];
        const built = buildPiHarnessResources({
            profile: profile({
                skills: [{ resource_type: 'skill', resource_id: 'skill-1' }]
            }),
            resources: [resource({
                    id: 1,
                    resource_kind: 'skill',
                    resource_key: 'skill-1',
                    resource_id: 'skill-1',
                    name: 'Evidence Debugging',
                    description: 'Trace failures from evidence before editing.',
                    version: '3',
                    spec: { instructions: 'Collect evidence, isolate the boundary, then change one cause.' }
                })],
            recordEvent(event) {
                events.push(event);
            }
        });
        const loader = built.tools.find((tool) => tool.name === 'easydo_skill_load');
        expect(loader).toBeDefined();
        const loaded = await loader?.execute('skill-load-1', { skill_name: 'Evidence Debugging' });
        expect(loaded).toMatchObject({
            content: [expect.objectContaining({ text: expect.stringContaining('Collect evidence') })]
        });
        expect(events).toContainEqual(expect.objectContaining({
            type: 'skill.loaded',
            name: 'Evidence Debugging',
            version: '3',
            digest: expect.stringMatching(/^sha256:/),
            source: 'dynamic_tool'
        }));
    });
    it('maps MCP JSON input schema into Pi tool parameters', () => {
        const built = buildPiHarnessResources({
            profile: profile({ mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }] }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: {
                        discovered_tools: [{
                                name: 'easydo_search',
                                input_schema: {
                                    type: 'object',
                                    required: ['query'],
                                    properties: {
                                        query: { type: 'string', description: 'Search query' },
                                        limit: { type: 'integer', minimum: 1 },
                                        include_archived: { type: 'boolean' },
                                        mode: { enum: ['fast', 'deep'] }
                                    }
                                }
                            }]
                    }
                })]
        });
        expect(built.tools[0].parameters).toMatchObject({
            type: 'object',
            required: ['query'],
            properties: {
                query: expect.objectContaining({ type: 'string', description: 'Search query' }),
                limit: expect.objectContaining({ type: 'integer', minimum: 1 }),
                include_archived: expect.objectContaining({ type: 'boolean' }),
                mode: expect.objectContaining({ enum: ['fast', 'deep'] })
            }
        });
    });
    it('executes MCP tools through the supplied adapter and records projectable OpenCode-style tool events', async () => {
        const events = [];
        const calls = [];
        const built = buildPiHarnessResources({
            profile: profile({
                mcp_servers: [{
                        resource_type: 'mcp_server',
                        resource_id: 'mcp-1',
                        config: { tool_permissions: { tools: { easydo_resource_list: 'allow' } } }
                    }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [{ name: 'easydo_resource_list', description: 'List resources', operation_type: 'read' }] }
                })],
            executeTool: async ({ callID, toolName, args }) => {
                calls.push({ callID, toolName, args });
                return { content: 'listed resources', structured_content: { ok: true }, metadata: { request_id: 'mcp-1' } };
            },
            recordEvent: async (event) => {
                events.push(event);
            }
        });
        const result = await built.tools[0].execute('call-1', { filter: 'gpu' });
        expect(calls).toEqual([{ callID: 'call-1', toolName: 'easydo_resource_list', args: { filter: 'gpu' } }]);
        expect(result).toEqual({
            content: [{ type: 'text', text: 'listed resources' }],
            details: { structured_content: { ok: true }, metadata: { request_id: 'mcp-1' } }
        });
        expect(events.map((event) => event.type)).toEqual(['action.permission_evaluated', 'session.tool.called', 'session.tool.success']);
        expect(events[0]).toMatchObject({ decision: 'allow', matched_rule: 'profile_ref.easydo_resource_list.allow' });
        expect(events[1]).toMatchObject({ assistant_message_id: 'assistant:call-1', call_id: 'call-1', tool: 'easydo_resource_list', tool_name: 'easydo_resource_list' });
        expect(events[2]).toMatchObject({ assistant_message_id: 'assistant:call-1', call_id: 'call-1', content: [{ type: 'text', text: 'listed resources' }], structured: { ok: true } });
    });
    it('applies server-scoped MCP rules before Pi tool execution', async () => {
        const events = [];
        let executions = 0;
        const built = buildPiHarnessResources({
            profile: profile({
                mcp_servers: [{
                        resource_type: 'mcp_server',
                        resource_id: 'mcp-1',
                        config: {
                            tool_permissions: {
                                rules: [{
                                        id: 'allow-mcp-1-write',
                                        match: 'mcp.mcp-1.tools.write_tool.write',
                                        decision: 'allow'
                                    }]
                            }
                        }
                    }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [{ name: 'write_tool', description: 'Write data', operation_type: 'write' }] }
                })],
            executeTool: async () => {
                executions += 1;
                return { content: 'write ok' };
            },
            recordEvent: async (event) => {
                events.push(event);
            }
        });
        await built.tools[0].execute('call-scoped-write', { value: 'x' });
        expect(executions).toBe(1);
        expect(events[0]).toMatchObject({
            type: 'action.permission_evaluated',
            decision: 'allow',
            matched_rule: 'allow-mcp-1-write',
            permission_key: 'mcp:mcp-1:tools:write_tool:write'
        });
    });
    it('propagates MCP server keys from discovered tools into policy evaluation approvals and execution', async () => {
        const events = [];
        const approvals = [];
        const calls = [];
        const built = buildPiHarnessResources({
            profile: profile({
                mcp_servers: [{
                        resource_type: 'mcp_server',
                        resource_id: 'mcp-1',
                        config: {
                            tool_permissions: {
                                rules: [{
                                        id: 'allow-jira-search',
                                        match: 'mcp.jira.tools.search.read',
                                        decision: 'allow'
                                    }]
                            }
                        }
                    }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: {
                        discovered_tools: [
                            { name: 'search', description: 'Search GitHub', operation_type: 'read', mcp_server_key: 'github' },
                            { name: 'search', description: 'Search Jira', operation_type: 'read', mcp_server_key: 'jira' }
                        ]
                    }
                })],
            decideTool: async (input) => {
                approvals.push(input);
                return { approved: true, reason: 'approved' };
            },
            executeTool: async (input) => {
                calls.push(input);
                return { content: 'jira result' };
            },
            recordEvent: async (event) => {
                events.push(event);
            }
        });
        const jiraSearch = built.tools.find((tool) => tool.name === 'search' && tool.description === 'Search Jira');
        expect(jiraSearch).toBeDefined();
        await jiraSearch?.execute('call-jira-search', { q: 'EASYDO-1' });
        expect(events[0]).toMatchObject({
            type: 'action.permission_evaluated',
            decision: 'allow',
            matched_rule: 'allow-jira-search',
            permission_key: 'mcp:jira:tools:search:read',
            mcp_server_key: 'jira'
        });
        expect(approvals).toEqual([]);
        expect(calls).toEqual([expect.objectContaining({
                callID: 'call-jira-search',
                toolName: 'search',
                mcpServerKey: 'jira',
                args: { q: 'EASYDO-1' }
            })]);
        expect(events[1]).toMatchObject({ type: 'session.tool.called', mcp_server_key: 'jira' });
        expect(events[2]).toMatchObject({ type: 'session.tool.success', mcp_server_key: 'jira' });
    });
    it('asks by default for MCP tools that are not configured on the profile reference', async () => {
        const events = [];
        let executed = false;
        const built = buildPiHarnessResources({
            profile: profile({ mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }] }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [{ name: 'easydo_resource_list', description: 'List resources', operation_type: 'read' }] }
                })],
            executeTool: async () => {
                executed = true;
                return { content: 'unexpected direct execution' };
            },
            recordEvent: async (event) => {
                events.push(event);
            }
        });
        await expect(built.tools[0].execute('call-default-request-1', {})).rejects.toBeInstanceOf(PiToolApprovalRequiredError);
        expect(executed).toBe(false);
        expect(events.map((event) => event.type)).toEqual(['action.permission_evaluated', 'permission.asked']);
        expect(events[0]).toMatchObject({ decision: 'ask', matched_rule: 'mcp.default_request' });
    });
    it('asks and resolves permission before executing confirmation-gated MCP tools with projectable approval fields', async () => {
        const events = [];
        let executed = false;
        const built = buildPiHarnessResources({
            profile: profile({ mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }] }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
                })],
            decideTool: async () => ({ approved: true, reason: 'approved for test' }),
            executeTool: async () => {
                executed = true;
                return { content: 'write ok' };
            },
            recordEvent: async (event) => {
                events.push(event);
            }
        });
        await built.tools[0].execute('call-write-1', { value: 'x' });
        expect(executed).toBe(true);
        expect(events.map((event) => event.type)).toEqual([
            'action.permission_evaluated',
            'permission.asked',
            'permission.resolved',
            'session.tool.called',
            'session.tool.success'
        ]);
        expect(events[1]).toMatchObject({ request_id: 'approval:call-write-1', approval_id: 'approval:call-write-1', message: 'Tool easydo_write requires approval' });
        expect(events[2]).toMatchObject({ request_id: 'approval:call-write-1', approval_id: 'approval:call-write-1', result: 'approved' });
    });
    it('rejects confirmation-gated MCP tools when no approval decision is available', async () => {
        const events = [];
        let executed = false;
        const built = buildPiHarnessResources({
            profile: profile({ mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }] }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
                })],
            executeTool: async () => {
                executed = true;
                return { content: 'write ok' };
            },
            recordEvent: async (event) => {
                events.push(event);
            }
        });
        await expect(built.tools[0].execute('call-write-2', { value: 'x' })).rejects.toBeInstanceOf(PiToolApprovalRequiredError);
        expect(executed).toBe(false);
        expect(events.map((event) => event.type)).toEqual(['action.permission_evaluated', 'permission.asked']);
    });
    it('requires permission for MCP tools when the profile policy requires all tools', async () => {
        const events = [];
        const built = buildPiHarnessResources({
            profile: profile({
                tool_policy: { all_tools_require_confirmation: true },
                mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'mcp-1' }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: { discovered_tools: [{ name: 'easydo_read', description: 'Read data' }] }
                })],
            executeTool: async () => ({ content: 'read ok' }),
            recordEvent: async (event) => { events.push(event); }
        });
        await expect(built.tools[0].execute('call-policy-1', {})).rejects.toBeInstanceOf(PiToolApprovalRequiredError);
        expect(events.map((event) => event.type)).toEqual(['action.permission_evaluated', 'permission.asked']);
    });
    it('allows explicitly configured read MCP tools without guessing write intent from the tool name', async () => {
        const events = [];
        const built = buildPiHarnessResources({
            profile: profile({
                tool_policy: { default_decision: 'request' },
                mcp_servers: [{
                        resource_type: 'mcp_server',
                        resource_id: 'mcp-1',
                        config: { tool_permissions: { tools: { easydo_pipeline_run_list: 'allow' } } }
                    }]
            }),
            resources: [resource({
                    resource_kind: 'mcp_server',
                    resource_key: 'mcp-1',
                    spec: {
                        discovered_tools: [{
                                name: 'easydo_pipeline_run_list',
                                description: 'List pipeline runs',
                                operation_type: 'read'
                            }]
                    }
                })],
            executeTool: async () => ({ content: 'listed' }),
            recordEvent: async (event) => { events.push(event); }
        });
        await built.tools[0].execute('call-read-run-list', {});
        expect(events.map((event) => event.type)).not.toContain('permission.asked');
        expect(events.map((event) => event.type)).toEqual(expect.arrayContaining([
            'session.tool.called',
            'session.tool.success'
        ]));
    });
    it('executes subagent.spawn through the supplied child-run adapter and records projectable child link output', async () => {
        const events = [];
        const calls = [];
        const built = buildPiHarnessResources({
            profile: profile({ subagents: [{ resource_type: 'subagent_profile', resource_id: 9 }] }),
            resources: [],
            subagents: [{ id: 9, name: 'Reviewer Agent', description: 'Reviews work', profile_kind: 'reviewer', context_tags: ['review'] }],
            executeSubagent: async ({ callID, subagent, task }) => {
                calls.push({ callID, subagentID: subagent.id, task });
                return {
                    task_id: 'review-task',
                    status: 'completed',
                    agent_profile_id: subagent.id,
                    name: subagent.name,
                    summary: 'review complete',
                    child_run_link_id: 'child-link-1',
                    child_session_id: 51,
                    child_runtime_run_id: 'r_child_1',
                    artifact_refs: [{ artifact_id: 'artifact-1', artifact_type: 'subagent_transcript' }]
                };
            },
            recordEvent: async (event) => { events.push(event); }
        });
        const result = await built.tools.find((tool) => tool.name === 'subagent.spawn')?.execute('call-subagent-1', {
            subagent_id: 9,
            task: 'review the patch'
        });
        expect(result).toMatchObject({
            content: [{ type: 'text', text: 'review complete' }],
            details: {
                subagent_id: 9,
                subagent_name: 'Reviewer Agent',
                task: 'review the patch',
                status: 'completed',
                child_run_link_id: 'child-link-1',
                child_runtime_run_id: 'r_child_1'
            }
        });
        expect(calls).toEqual([{ callID: 'call-subagent-1', subagentID: 9, task: 'review the patch' }]);
        expect(events.map((event) => event.type)).toEqual(['session.tool.called', 'session.tool.success']);
        expect(events[0]).toMatchObject({ tool: 'subagent.spawn', call_id: 'call-subagent-1' });
        expect(events[1]).toMatchObject({ structured: { subagent_id: 9, subagent_name: 'Reviewer Agent', status: 'completed', child_run_link_id: 'child-link-1' } });
    });
});
