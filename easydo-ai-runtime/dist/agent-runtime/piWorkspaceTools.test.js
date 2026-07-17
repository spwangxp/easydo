import { describe, expect, it } from 'vitest';
import { ok } from '@earendil-works/pi-agent-core';
import { createWorkspaceTools } from './piWorkspaceTools.js';
describe('createWorkspaceTools', () => {
    it('exposes the complete sandbox coding tool set', () => {
        const tools = createWorkspaceTools({ cwd: '/workspace' });
        expect(tools.map((tool) => tool.name)).toEqual([
            'bash',
            'read_file',
            'write_file',
            'edit_file',
            'list_directory',
            'file_info'
        ]);
    });
    it('exposes only explicitly read-classified tools in read_only mode', () => {
        const tools = createWorkspaceTools({ cwd: '/workspace' }, 'read_only');
        expect(tools.map((tool) => tool.name)).toEqual(['read_file', 'list_directory', 'file_info']);
        expect(tools.map((tool) => tool.operation_type)).toEqual(['read', 'read', 'read']);
    });
    it('classifies the complete write-mode tool catalog with explicit operation metadata', () => {
        const tools = createWorkspaceTools({ cwd: '/workspace' }, 'write');
        expect(tools.map((tool) => [tool.name, tool.operation_type])).toEqual([
            ['bash', 'execute'],
            ['read_file', 'read'],
            ['write_file', 'write'],
            ['edit_file', 'write'],
            ['list_directory', 'read'],
            ['file_info', 'read']
        ]);
    });
    it('executes shell and file writes through the supplied workspace environment', async () => {
        const calls = [];
        const env = {
            cwd: '/workspace',
            async exec(command, options) {
                calls.push({ method: 'exec', args: [command, options] });
                return ok({ stdout: 'ok\n', stderr: '', exitCode: 0 });
            },
            async writeFile(path, content) {
                calls.push({ method: 'writeFile', args: [path, content] });
                return ok(undefined);
            }
        };
        const tools = createWorkspaceTools(env);
        await tools.find((tool) => tool.name === 'bash').execute('call-1', { command: 'pwd', cwd: 'src' });
        await tools.find((tool) => tool.name === 'write_file').execute('call-2', { path: 'demo.txt', content: 'hello' });
        expect(calls).toEqual([
            { method: 'exec', args: ['pwd', { cwd: 'src', timeout: undefined }] },
            { method: 'writeFile', args: ['demo.txt', 'hello'] }
        ]);
    });
});
