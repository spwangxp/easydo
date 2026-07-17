import { describe, expect, it } from 'vitest';
import { DockerWorkspaceProvider } from './dockerWorkspaceProvider.js';
function workspace(overrides = {}) {
    return {
        id: 1,
        workspace_runtime_id: 'aws_w1_u7_default',
        workspace_id: 1,
        owner_user_id: 7,
        workspace_key: 'default',
        provider: 'docker',
        sandbox_id: 'easydo-ai-w1-u7-default',
        status: 'provisioning',
        image: 'easydo-ai-workspace:test',
        root_path: '/workspace',
        repository: {},
        network_policy: { mode: 'none', allow_hosts: [] },
        secret_refs: [],
        quota: { cpu_cores: 2, memory_mb: 2048, disk_mb: 10240, pids: 128 },
        snapshot_hash: 'sha256:test',
        lease_epoch: 0,
        provision_epoch: 0,
        state_version: 0,
        created_at: '2026-07-12T00:00:00.000Z',
        updated_at: '2026-07-12T00:00:00.000Z',
        ...overrides
    };
}
function runner() {
    const calls = [];
    const implementation = {
        async run(args) {
            calls.push(args);
            if (args.includes('inspect'))
                return { stdout: '', stderr: 'not found', exitCode: 1 };
            if (args.includes('run'))
                return { stdout: 'container-id\n', stderr: '', exitCode: 0 };
            return { stdout: '', stderr: '', exitCode: 0 };
        }
    };
    return { calls, implementation };
}
describe('DockerWorkspaceProvider', () => {
    it('provisions a non-root constrained isolated workspace container', async () => {
        const command = runner();
        const provider = new DockerWorkspaceProvider(command.implementation);
        await expect(provider.provision(workspace())).resolves.toEqual({ sandbox_id: 'easydo-ai-w1-u7-default' });
        const createArgs = command.calls.find((args) => args.includes('run')) || [];
        expect(createArgs).toEqual(expect.arrayContaining([
            'run', '-d', '--name', 'easydo-ai-w1-u7-default',
            '--user', '1000:1000', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges', '--pids-limit', '128',
            '--memory', '2048m', '--cpus', '2', '--network', 'none',
            'easydo-ai-workspace:test'
        ]));
        expect(createArgs.join(' ')).toContain('type=volume');
        expect(createArgs.join(' ')).toContain('dst=/workspace');
    });
    it('accepts the running workspace created by a concurrent provisioner after a Docker name conflict', async () => {
        const calls = [];
        let inspectCount = 0;
        const provider = new DockerWorkspaceProvider({
            async run(args) {
                calls.push(args);
                if (args[0] === 'inspect') {
                    inspectCount += 1;
                    if (inspectCount === 1)
                        return { stdout: '', stderr: 'Error: No such container: easydo-ai-w1-u7-default', exitCode: 1 };
                    return {
                        stdout: JSON.stringify([{
                                Id: 'winner-container-id',
                                Name: '/easydo-ai-w1-u7-default',
                                State: { Status: 'running', Running: true },
                                Config: { Labels: { 'easydo.workspace_runtime_id': 'aws_w1_u7_default' } }
                            }]),
                        stderr: '',
                        exitCode: 0
                    };
                }
                return {
                    stdout: '',
                    stderr: 'docker: Error response from daemon: Conflict. The container name "/easydo-ai-w1-u7-default" is already in use by container "winner-container-id".',
                    exitCode: 125
                };
            }
        });
        await expect(provider.provision(workspace())).resolves.toEqual({ sandbox_id: 'easydo-ai-w1-u7-default' });
        expect(calls).toHaveLength(3);
        expect(calls[0]).toEqual(['inspect', 'easydo-ai-w1-u7-default']);
        expect(calls[1]?.[0]).toBe('run');
        expect(calls[2]).toEqual(['inspect', 'easydo-ai-w1-u7-default']);
    });
    it('maps pause resume recycle and shell execution to the same sandbox', async () => {
        const command = runner();
        const provider = new DockerWorkspaceProvider(command.implementation);
        const target = workspace({ status: 'ready' });
        await provider.pause(target);
        await provider.resume({ ...target, status: 'paused' });
        await provider.recycle(target);
        await provider.exec(target, 'pwd', { cwd: '/workspace', env: { CI: 'true' } });
        expect(command.calls).toEqual(expect.arrayContaining([
            ['pause', target.sandbox_id],
            ['unpause', target.sandbox_id],
            ['rm', '-f', target.sandbox_id],
            ['volume', 'rm', '-f', `easydo-ai-workspace-${target.workspace_runtime_id}`]
        ]));
        const execArgs = command.calls.find((args) => args[0] === 'exec') || [];
        expect(execArgs).toEqual(expect.arrayContaining(['exec', '--workdir', '/workspace', '--env', 'CI=true', target.sandbox_id, 'bash', '-lc', 'pwd']));
    });
    it('fails recycle when the persistent workspace volume cannot be removed', async () => {
        const provider = new DockerWorkspaceProvider({
            async run(args) {
                if (args[0] === 'volume')
                    return { stdout: '', stderr: 'volume is in use', exitCode: 1 };
                return { stdout: '', stderr: '', exitCode: 0 };
            }
        });
        await expect(provider.recycle(workspace({ status: 'ready' }))).rejects.toThrow('volume is in use');
    });
    it('does not provision a replacement when Docker inspect fails at the daemon boundary', async () => {
        const calls = [];
        const provider = new DockerWorkspaceProvider({
            async run(args) {
                calls.push(args);
                return { stdout: '', stderr: 'Cannot connect to the Docker daemon', exitCode: 125 };
            }
        });
        await expect(provider.provision(workspace())).rejects.toThrow('Cannot connect to the Docker daemon');
        expect(calls).toHaveLength(1);
    });
    it('rejects allowlist networking until host-level enforcement is configured', async () => {
        const command = runner();
        const provider = new DockerWorkspaceProvider(command.implementation);
        await expect(provider.provision(workspace({
            network_policy: { mode: 'allowlist', allow_hosts: ['git.example.test'] }
        }))).rejects.toThrow('allowlist');
        expect(command.calls).toHaveLength(0);
    });
});
