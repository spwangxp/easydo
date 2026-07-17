import { describe, expect, it } from 'vitest';
import { evaluateToolPermission } from './toolPermissionPolicy.js';
describe('evaluateToolPermission', () => {
    it('returns an explainable per-tool decision with role and argument matching', () => {
        expect(evaluateToolPermission({
            toolName: 'workspace.write', operationType: 'write', actorRole: 'developer', args: { path: '/repo' },
            policy: { rules: [{ id: 'deny-developer-repo-write', tool_name: 'workspace.write', roles: ['developer'], argument_equals: { path: '/repo' }, decision: 'deny' }] }
        })).toMatchObject({
            decision: 'deny', matched_rule: 'deny-developer-repo-write', scope: 'profile', permission_key: 'tool:workspace.write:write'
        });
        expect(evaluateToolPermission({
            toolName: 'workspace.write', operationType: 'write', actorRole: 'developer', args: { path: '/repo' },
            policy: { rules: [{ id: 'deny-developer-repo-write', tool_name: 'workspace.write', roles: ['developer'], argument_equals: { path: '/repo' }, decision: 'deny' }] }
        }).reason).toBe('deny by deny-developer-repo-write');
    });
    it('requests approval for any configured tool action unless the profile explicitly allows it', () => {
        expect(evaluateToolPermission({ toolName: 'pipeline_run_list', operationType: 'read' })).toMatchObject({
            decision: 'ask',
            matched_rule: 'default.missing_tool_action'
        });
        expect(evaluateToolPermission({
            toolName: 'pipeline_run_list',
            operationType: 'read',
            policy: { rules: [{ tool_name: 'pipeline_run_list', operation_type: 'read', decision: 'allow' }] }
        }).decision).toBe('allow');
        expect(evaluateToolPermission({ toolName: 'harmless_name' }).decision).toBe('ask');
    });
    it('denies write operations before profile rules when the Subagent Run is read_only', () => {
        expect(evaluateToolPermission({
            toolName: 'workspace.write',
            operationType: 'write',
            runMode: 'read_only',
            policy: { rules: [{ tool_name: 'workspace.write', operation_type: 'write', decision: 'allow' }] }
        })).toMatchObject({
            decision: 'deny',
            matched_rule: 'subagent.read_only',
            scope: 'run',
            permission_key: 'tool:workspace.write:write',
            operation_type: 'write'
        });
        expect(evaluateToolPermission({ toolName: 'workspace.write', operationType: 'write', runMode: 'read_only' }).reason)
            .toContain('subagent.read_only');
    });
    it('denies unknown operations in read_only mode and still requires profile policy for reads', () => {
        expect(evaluateToolPermission({ toolName: 'unclassified', runMode: 'read_only' }).decision).toBe('deny');
        expect(evaluateToolPermission({ toolName: 'resource.list', operationType: 'read', runMode: 'read_only' }).decision).toBe('ask');
        expect(evaluateToolPermission({
            toolName: 'resource.list',
            operationType: 'read',
            runMode: 'read_only',
            policy: { rules: [{ tool_name: 'resource.list', operation_type: 'read', decision: 'allow' }] }
        }).decision).toBe('allow');
    });
    it('supports MCP server scoped wildcard rules with deny taking precedence over allow', () => {
        expect(evaluateToolPermission({
            toolName: 'issues.delete',
            mcpServerKey: 'github',
            capability: 'tools',
            operationType: 'write',
            policy: {
                default_decision: 'request',
                capabilities: { tools: 'request' },
                rules: [
                    { id: 'allow-github-tools', match: 'mcp.github.tools.*', decision: 'allow' },
                    { id: 'deny-github-delete', match: 'mcp.github.tools.issues.delete', decision: 'deny' }
                ]
            }
        })).toMatchObject({
            decision: 'deny',
            matched_rule: 'deny-github-delete',
            permission_key: 'mcp:github:tools:issues.delete:write'
        });
        expect(evaluateToolPermission({
            toolName: 'issues.get',
            mcpServerKey: 'github',
            capability: 'tools',
            operationType: 'read',
            policy: {
                default_decision: 'request',
                capabilities: { tools: 'request' },
                rules: [{ id: 'allow-github-tools', match: 'mcp.github.tools.*', decision: 'allow' }]
            }
        })).toMatchObject({
            decision: 'allow',
            matched_rule: 'allow-github-tools',
            permission_key: 'mcp:github:tools:issues.get:read'
        });
    });
});
