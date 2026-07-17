import { describe, expect, it } from 'vitest';
import { WorkspaceExecutionEnv } from './workspaceExecutionEnv.js';
function workspace() {
    return {
        id: 1,
        workspace_runtime_id: 'aws_w1_u7_default',
        workspace_id: 1,
        owner_user_id: 7,
        workspace_key: 'default',
        provider: 'docker',
        sandbox_id: 'sandbox-1',
        status: 'ready',
        image: 'easydo-ai-workspace:test',
        root_path: '/workspace',
        repository: {},
        network_policy: { mode: 'none', allow_hosts: [] },
        secret_refs: [],
        quota: { cpu_cores: 1, memory_mb: 1024, disk_mb: 10240, pids: 256 },
        snapshot_hash: 'sha256:test',
        lease_epoch: 1,
        provision_epoch: 0,
        state_version: 0,
        created_at: '2026-07-12T00:00:00.000Z',
        updated_at: '2026-07-12T00:00:00.000Z'
    };
}
function client() {
    const calls = [];
    const implementation = {
        async exec(_workspace, command, options) {
            calls.push({ method: 'exec', args: [command, options] });
            return { stdout: 'ok\n', stderr: '', exitCode: 0 };
        },
        async readFile(_workspace, path) {
            calls.push({ method: 'readFile', args: [path] });
            return Buffer.from('hello');
        },
        async writeFile(_workspace, path, content, append) {
            calls.push({ method: 'writeFile', args: [path, content, append] });
        },
        async fileInfo(_workspace, path) {
            calls.push({ method: 'fileInfo', args: [path] });
            return { name: 'demo.txt', path, kind: 'file', size: 5, mtimeMs: 1 };
        },
        async listDir(_workspace, path) {
            calls.push({ method: 'listDir', args: [path] });
            return [{ name: 'demo.txt', path: `${path}/demo.txt`, kind: 'file', size: 5, mtimeMs: 1 }];
        },
        async canonicalPath(_workspace, path) {
            calls.push({ method: 'canonicalPath', args: [path] });
            return path.endsWith('/escape') ? '/etc/passwd' : path;
        },
        async exists() { return true; },
        async createDir() { },
        async remove() { },
        async createTempDir() { return '/workspace/tmp/demo'; },
        async createTempFile() { return '/workspace/tmp/demo.txt'; },
        async cleanup() { }
    };
    return { calls, implementation };
}
describe('WorkspaceExecutionEnv', () => {
    it('keeps shell execution inside the workspace root', async () => {
        const backend = client();
        const env = new WorkspaceExecutionEnv(workspace(), backend.implementation);
        const result = await env.exec('pwd', { cwd: 'src', env: { CI: 'true' } });
        expect(result).toMatchObject({ ok: true, value: { stdout: 'ok\n', exitCode: 0 } });
        expect(backend.calls[1]).toMatchObject({
            method: 'exec',
            args: ['pwd', { cwd: '/workspace/src', env: { CI: 'true' } }]
        });
    });
    it('rejects syntactic traversal and absolute paths outside the workspace', async () => {
        const backend = client();
        const env = new WorkspaceExecutionEnv(workspace(), backend.implementation);
        await expect(env.readTextFile('../secret')).resolves.toMatchObject({ ok: false, error: { code: 'permission_denied' } });
        await expect(env.readTextFile('/etc/passwd')).resolves.toMatchObject({ ok: false, error: { code: 'permission_denied' } });
        expect(backend.calls).toHaveLength(0);
    });
    it('rejects canonical symlink targets outside the workspace root', async () => {
        const backend = client();
        const env = new WorkspaceExecutionEnv(workspace(), backend.implementation);
        await expect(env.canonicalPath('escape')).resolves.toMatchObject({ ok: false, error: { code: 'permission_denied' } });
    });
    it('rejects a shell cwd whose canonical target escapes the workspace root', async () => {
        const backend = client();
        const env = new WorkspaceExecutionEnv(workspace(), backend.implementation);
        await expect(env.exec('pwd', { cwd: 'escape' })).resolves.toMatchObject({
            ok: false,
            error: { code: 'spawn_error' }
        });
        expect(backend.calls.map((call) => call.method)).toEqual(['canonicalPath']);
    });
    it('rejects temporary path prefixes and suffixes that contain path separators', async () => {
        const backend = client();
        const env = new WorkspaceExecutionEnv(workspace(), backend.implementation);
        await expect(env.createTempDir('../escape')).resolves.toMatchObject({ ok: false, error: { code: 'invalid' } });
        await expect(env.createTempFile({ suffix: '/escape' })).resolves.toMatchObject({ ok: false, error: { code: 'invalid' } });
        expect(backend.calls).toHaveLength(0);
    });
    it('delegates binary-safe file reads and writes after path validation', async () => {
        const backend = client();
        const env = new WorkspaceExecutionEnv(workspace(), backend.implementation);
        await expect(env.readBinaryFile('demo.txt')).resolves.toMatchObject({ ok: true });
        await expect(env.writeFile('nested/demo.bin', new Uint8Array([1, 2, 3]))).resolves.toMatchObject({ ok: true });
        expect(backend.calls.map((call) => call.method)).toEqual(['canonicalPath', 'readFile', 'canonicalPath', 'writeFile']);
    });
});
