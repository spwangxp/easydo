import { spawn } from 'node:child_process';
import { z } from 'zod';
export class DockerWorkspaceProvider {
    runner;
    dockerHost;
    volumePrefix;
    constructor(runner = new SpawnDockerCommandRunner(), options = {}) {
        this.runner = runner;
        this.dockerHost = options.dockerHost || '';
        this.volumePrefix = options.volumePrefix || 'easydo-ai-workspace';
    }
    async provision(workspace) {
        if (workspace.network_policy.mode === 'allowlist') {
            throw new Error('Docker workspace network allowlist requires an explicitly configured host-level enforcement provider');
        }
        const sandboxID = requiredSandboxID(workspace);
        const inspected = await this.run(['inspect', sandboxID]);
        if (inspected.exitCode === 0) {
            requireRunningWorkspaceContainer(inspected, workspace.workspace_runtime_id);
            return { sandbox_id: sandboxID };
        }
        if (!isMissingContainer(inspected))
            throw commandError(`inspect Agent workspace ${workspace.workspace_runtime_id}`, inspected);
        const volumeName = this.volumeName(workspace);
        const network = workspace.network_policy.mode === 'none' ? 'none' : 'bridge';
        const runResult = await this.run([
            'run', '-d', '--name', sandboxID,
            '--user', '1000:1000',
            '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges',
            '--pids-limit', String(workspace.quota.pids),
            '--memory', `${workspace.quota.memory_mb}m`,
            '--cpus', String(workspace.quota.cpu_cores),
            '--network', network,
            '--read-only',
            '--tmpfs', '/tmp:rw,noexec,nosuid,nodev,size=256m',
            '--mount', `type=volume,src=${volumeName},dst=${workspace.root_path}`,
            '--label', `easydo.workspace_runtime_id=${workspace.workspace_runtime_id}`,
            '--label', `easydo.disk_quota_mb=${workspace.quota.disk_mb}`,
            workspace.image,
            'sleep', 'infinity'
        ]);
        if (runResult.exitCode === 0)
            return { sandbox_id: sandboxID };
        const winner = await this.run(['inspect', sandboxID]);
        if (isConcurrentProvisionWinner(winner, workspace.workspace_runtime_id)) {
            return { sandbox_id: sandboxID };
        }
        throw commandError(`provision Agent workspace ${workspace.workspace_runtime_id}`, runResult);
    }
    async connect(workspace) {
        await this.mustRun(['inspect', requiredSandboxID(workspace)], `connect Agent workspace ${workspace.workspace_runtime_id}`);
    }
    async pause(workspace) {
        await this.mustRun(['pause', requiredSandboxID(workspace)], `pause Agent workspace ${workspace.workspace_runtime_id}`);
    }
    async resume(workspace) {
        await this.mustRun(['unpause', requiredSandboxID(workspace)], `resume Agent workspace ${workspace.workspace_runtime_id}`);
    }
    async recycle(workspace) {
        const result = await this.run(['rm', '-f', requiredSandboxID(workspace)]);
        if (result.exitCode !== 0 && !/no such (container|object)/i.test(result.stderr)) {
            throw commandError(`recycle Agent workspace ${workspace.workspace_runtime_id}`, result);
        }
        const volumeResult = await this.run(['volume', 'rm', '-f', this.volumeName(workspace)]);
        if (volumeResult.exitCode !== 0 && !/no such volume/i.test(volumeResult.stderr || volumeResult.stdout)) {
            throw commandError(`remove Agent workspace volume ${workspace.workspace_runtime_id}`, volumeResult);
        }
    }
    async exec(workspace, command, options) {
        const args = ['exec', '--workdir', options.cwd];
        for (const [name, value] of Object.entries(options.env || {}).sort(([left], [right]) => left.localeCompare(right))) {
            args.push('--env', `${name}=${value}`);
        }
        args.push(requiredSandboxID(workspace), 'bash', '-lc', command);
        const result = await this.run(args, {
            timeoutMs: options.timeout === undefined ? undefined : options.timeout * 1000,
            abortSignal: options.abortSignal,
            onStdout: options.onStdout,
            onStderr: options.onStderr
        });
        return result;
    }
    async readFile(workspace, targetPath, abortSignal) {
        const value = await this.fileOperation(workspace, 'read', { path: targetPath, root: workspace.root_path }, undefined, abortSignal);
        return Buffer.from(requiredString(value.data, 'file data'), 'base64');
    }
    async writeFile(workspace, targetPath, content, append, abortSignal) {
        await this.fileOperation(workspace, 'write', { path: targetPath, root: workspace.root_path, append }, Buffer.from(content).toString('base64'), abortSignal);
    }
    async fileInfo(workspace, targetPath, abortSignal) {
        return await this.fileOperation(workspace, 'stat', { path: targetPath, root: workspace.root_path }, undefined, abortSignal);
    }
    async listDir(workspace, targetPath, abortSignal) {
        const value = await this.fileOperation(workspace, 'list', { path: targetPath, root: workspace.root_path }, undefined, abortSignal);
        return Array.isArray(value.entries) ? value.entries : [];
    }
    async canonicalPath(workspace, targetPath, abortSignal) {
        const value = await this.fileOperation(workspace, 'canonical', { path: targetPath, root: workspace.root_path }, undefined, abortSignal);
        return requiredString(value.path, 'canonical path');
    }
    async exists(workspace, targetPath, abortSignal) {
        const value = await this.fileOperation(workspace, 'exists', { path: targetPath, root: workspace.root_path }, undefined, abortSignal);
        return value.exists === true;
    }
    async createDir(workspace, targetPath, recursive, abortSignal) {
        await this.fileOperation(workspace, 'mkdir', { path: targetPath, root: workspace.root_path, recursive }, undefined, abortSignal);
    }
    async remove(workspace, targetPath, recursive, force, abortSignal) {
        await this.fileOperation(workspace, 'remove', { path: targetPath, root: workspace.root_path, recursive, force }, undefined, abortSignal);
    }
    async createTempDir(workspace, prefix, abortSignal) {
        const value = await this.fileOperation(workspace, 'tempDir', { root: workspace.root_path, prefix }, undefined, abortSignal);
        return requiredString(value.path, 'temporary directory path');
    }
    async createTempFile(workspace, prefix, suffix, abortSignal) {
        const value = await this.fileOperation(workspace, 'tempFile', { root: workspace.root_path, prefix, suffix }, undefined, abortSignal);
        return requiredString(value.path, 'temporary file path');
    }
    async cleanup() {
        // Containers and volumes are lifecycle-managed, not per-harness resources.
    }
    async fileOperation(workspace, operation, payload, input, abortSignal) {
        const result = await this.run([
            'exec', '-i', requiredSandboxID(workspace), 'node', '-e', FILE_OPERATION_SCRIPT,
            operation, JSON.stringify(payload)
        ], { input: input === undefined ? undefined : Buffer.from(input, 'utf8'), abortSignal });
        if (result.exitCode !== 0)
            throw fileCommandError(result);
        try {
            return JSON.parse(result.stdout || '{}');
        }
        catch (error) {
            throw new Error(`Docker workspace file operation returned invalid JSON: ${asError(error).message}`);
        }
    }
    async mustRun(args, action) {
        const result = await this.run(args);
        if (result.exitCode !== 0)
            throw commandError(action, result);
        return result;
    }
    run(args, options) {
        return this.runner.run(this.dockerHost ? ['--host', this.dockerHost, ...args] : args, options);
    }
    volumeName(workspace) {
        return `${this.volumePrefix}-${workspace.workspace_runtime_id}`.replace(/[^a-zA-Z0-9_.-]/g, '-');
    }
}
export class SpawnDockerCommandRunner {
    async run(args, options = {}) {
        return new Promise((resolve, reject) => {
            const child = spawn('docker', args, { stdio: ['pipe', 'pipe', 'pipe'] });
            let stdout = '';
            let stderr = '';
            let settled = false;
            let timeout;
            const finish = (callback) => {
                if (settled)
                    return;
                settled = true;
                if (timeout)
                    clearTimeout(timeout);
                options.abortSignal?.removeEventListener('abort', abort);
                callback();
            };
            const abort = () => {
                child.kill('SIGKILL');
                finish(() => reject(Object.assign(new Error('Docker command aborted'), { name: 'AbortError' })));
            };
            if (options.abortSignal?.aborted) {
                abort();
                return;
            }
            options.abortSignal?.addEventListener('abort', abort, { once: true });
            if (options.timeoutMs !== undefined && options.timeoutMs > 0) {
                timeout = setTimeout(() => {
                    child.kill('SIGKILL');
                    finish(() => reject(Object.assign(new Error(`Docker command timed out after ${options.timeoutMs}ms`), { name: 'TimeoutError' })));
                }, options.timeoutMs);
            }
            child.stdout.on('data', (chunk) => {
                const text = chunk.toString('utf8');
                stdout += text;
                try {
                    options.onStdout?.(text);
                }
                catch (error) {
                    child.kill('SIGKILL');
                    finish(() => reject(Object.assign(asError(error), { name: 'CallbackError' })));
                }
            });
            child.stderr.on('data', (chunk) => {
                const text = chunk.toString('utf8');
                stderr += text;
                try {
                    options.onStderr?.(text);
                }
                catch (error) {
                    child.kill('SIGKILL');
                    finish(() => reject(Object.assign(asError(error), { name: 'CallbackError' })));
                }
            });
            child.on('error', (error) => finish(() => reject(error)));
            child.on('close', (code) => finish(() => resolve({ stdout, stderr, exitCode: code ?? 1 })));
            if (options.input)
                child.stdin.end(options.input);
            else
                child.stdin.end();
        });
    }
}
function requiredSandboxID(workspace) {
    const sandboxID = workspace.sandbox_id || `easydo-ai-${workspace.workspace_runtime_id}`;
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]+$/.test(sandboxID))
        throw new Error(`Invalid Docker sandbox ID: ${sandboxID}`);
    return sandboxID;
}
function commandError(action, result) {
    return new Error(`Failed to ${action}: ${(result.stderr || result.stdout || `exit ${result.exitCode}`).trim()}`);
}
function isMissingContainer(result) {
    return result.exitCode === 1 && /(no such (container|object)|not found)/i.test(result.stderr || result.stdout);
}
const dockerInspectSchema = z.array(z.object({
    State: z.object({ Running: z.boolean() }),
    Config: z.object({ Labels: z.record(z.string()).nullish() })
})).length(1);
function requireRunningWorkspaceContainer(result, workspaceRuntimeID) {
    if (!isRunningWorkspaceContainer(result, workspaceRuntimeID)) {
        throw new Error(`Docker sandbox is not a running container for Agent workspace ${workspaceRuntimeID}`);
    }
}
function isRunningWorkspaceContainer(result, workspaceRuntimeID) {
    let value;
    try {
        value = JSON.parse(result.stdout);
    }
    catch (error) {
        throw new Error(`Docker inspect returned invalid JSON for Agent workspace ${workspaceRuntimeID}: ${asError(error).message}`);
    }
    const parsed = dockerInspectSchema.safeParse(value);
    if (!parsed.success) {
        throw new Error(`Docker inspect returned an invalid container record for Agent workspace ${workspaceRuntimeID}`);
    }
    const container = parsed.data[0];
    return container.State.Running && container.Config.Labels?.['easydo.workspace_runtime_id'] === workspaceRuntimeID;
}
function isConcurrentProvisionWinner(result, workspaceRuntimeID) {
    if (result.exitCode !== 0)
        return false;
    try {
        return isRunningWorkspaceContainer(result, workspaceRuntimeID);
    }
    catch {
        return false;
    }
}
function fileCommandError(result) {
    try {
        const parsed = JSON.parse(result.stderr.trim());
        return Object.assign(new Error(parsed.message || 'Docker workspace file operation failed'), { code: parsed.code });
    }
    catch {
        return commandError('execute Docker workspace file operation', result);
    }
}
function requiredString(value, label) {
    if (typeof value !== 'string')
        throw new Error(`Docker workspace response is missing ${label}`);
    return value;
}
function asError(error) {
    return error instanceof Error ? error : new Error(String(error));
}
const FILE_OPERATION_SCRIPT = String.raw `
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const [operation, rawPayload] = process.argv.slice(1);
const payload = JSON.parse(rawPayload || '{}');
const info = async (target) => {
  const value = await fs.lstat(target);
  return { name: path.basename(target), path: path.posix.normalize(target), kind: value.isSymbolicLink() ? 'symlink' : value.isDirectory() ? 'directory' : 'file', size: value.size, mtimeMs: value.mtimeMs };
};
const canonicalForAddress = async (target) => {
  try { return await fs.realpath(target); } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    const parent = path.dirname(target);
    if (parent === target) throw error;
    return path.join(await canonicalForAddress(parent), path.basename(target));
  }
};
const assertContained = async (target, root) => {
  const canonicalRoot = await fs.realpath(root);
  const canonicalTarget = await canonicalForAddress(target);
  if (canonicalTarget !== canonicalRoot && !canonicalTarget.startsWith(canonicalRoot + path.sep)) {
    const error = new Error('Path escapes Agent workspace'); error.code = 'EACCES'; throw error;
  }
  return canonicalTarget;
};
const run = async () => {
  if (payload.path) await assertContained(payload.path, payload.root);
  switch (operation) {
    case 'read': return { data: (await fs.readFile(payload.path)).toString('base64') };
    case 'write': {
      const chunks = []; for await (const chunk of process.stdin) chunks.push(chunk);
      await fs.mkdir(path.dirname(payload.path), { recursive: true });
      await fs.writeFile(payload.path, Buffer.from(Buffer.concat(chunks).toString('utf8'), 'base64'), { flag: payload.append ? 'a' : 'w' }); return {};
    }
    case 'stat': return await info(payload.path);
    case 'list': return { entries: await Promise.all((await fs.readdir(payload.path)).map((name) => info(path.join(payload.path, name)))) };
    case 'canonical': return { path: await canonicalForAddress(payload.path) };
    case 'exists': try { await fs.access(payload.path); return { exists: true }; } catch (error) { if (error.code === 'ENOENT') return { exists: false }; throw error; }
    case 'mkdir': await fs.mkdir(payload.path, { recursive: payload.recursive }); return {};
    case 'remove': await fs.rm(payload.path, { recursive: payload.recursive, force: payload.force }); return {};
    case 'tempDir': return { path: await fs.mkdtemp(path.join(payload.root, payload.prefix || 'tmp-')) };
    case 'tempFile': { const dir = await fs.mkdtemp(path.join(payload.root, '.tmp-')); const target = path.join(dir, (payload.prefix || '') + 'file' + (payload.suffix || '')); await fs.writeFile(target, ''); return { path: target }; }
    default: throw Object.assign(new Error('Unsupported file operation'), { code: 'ENOTSUP' });
  }
};
run().then((value) => process.stdout.write(JSON.stringify(value))).catch((error) => { process.stderr.write(JSON.stringify({ code: error.code || 'UNKNOWN', message: error.message })); process.exit(1); });
`;
