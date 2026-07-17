import path from 'node:path';
import { ExecutionError, FileError, err, ok } from '@earendil-works/pi-agent-core';
export class WorkspaceExecutionEnv {
    workspace;
    client;
    cwd;
    constructor(workspace, client) {
        this.workspace = workspace;
        this.client = client;
        this.cwd = normalizeRoot(workspace.root_path);
    }
    async absolutePath(input, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => absolute, false);
    }
    async joinPath(parts, abortSignal) {
        if (abortSignal?.aborted)
            return err(new FileError('aborted', 'File operation aborted'));
        return this.absolutePath(path.posix.join(...parts), abortSignal);
    }
    async exec(command, options = {}) {
        if (options.abortSignal?.aborted)
            return err(new ExecutionError('aborted', 'Command execution aborted'));
        const cwd = this.addressedPath(options.cwd || this.cwd);
        if (!cwd.ok)
            return err(new ExecutionError('spawn_error', cwd.error.message, cwd.error));
        try {
            const canonical = await this.client.canonicalPath(this.workspace, cwd.value, options.abortSignal);
            this.requireContained(canonical, options.cwd || this.cwd);
            return ok(await this.client.exec(this.workspace, command, { ...options, cwd: cwd.value }));
        }
        catch (error) {
            return err(executionError(error, options.abortSignal));
        }
    }
    async readTextFile(input, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => Buffer.from(await this.client.readFile(this.workspace, absolute, abortSignal)).toString('utf8'));
    }
    async readTextLines(input, options = {}) {
        const result = await this.readTextFile(input, options.abortSignal);
        if (!result.ok)
            return result;
        const lines = result.value.split(/\r?\n/);
        if (lines.at(-1) === '')
            lines.pop();
        return ok(options.maxLines === undefined ? lines : lines.slice(0, Math.max(0, options.maxLines)));
    }
    async readBinaryFile(input, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => new Uint8Array(await this.client.readFile(this.workspace, absolute, abortSignal)));
    }
    async writeFile(input, content, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => {
            await this.client.writeFile(this.workspace, absolute, bytes(content), false, abortSignal);
        });
    }
    async appendFile(input, content, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => {
            await this.client.writeFile(this.workspace, absolute, bytes(content), true, abortSignal);
        });
    }
    async fileInfo(input, abortSignal) {
        return this.fileResult(input, abortSignal, (absolute) => this.client.fileInfo(this.workspace, absolute, abortSignal), false);
    }
    async listDir(input, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => {
            const entries = await this.client.listDir(this.workspace, absolute, abortSignal);
            for (const entry of entries)
                this.requireContained(entry.path, input);
            return entries;
        });
    }
    async canonicalPath(input, abortSignal) {
        return this.fileResult(input, abortSignal, async (absolute) => {
            const canonical = await this.client.canonicalPath(this.workspace, absolute, abortSignal);
            this.requireContained(canonical, input);
            return canonical;
        }, false);
    }
    async exists(input, abortSignal) {
        return this.fileResult(input, abortSignal, (absolute) => this.client.exists(this.workspace, absolute, abortSignal));
    }
    async createDir(input, options = {}) {
        return this.fileResult(input, options.abortSignal, async (absolute) => {
            await this.client.createDir(this.workspace, absolute, options.recursive ?? true, options.abortSignal);
        });
    }
    async remove(input, options = {}) {
        return this.fileResult(input, options.abortSignal, async (absolute) => {
            if (absolute === this.cwd)
                throw new FileError('permission_denied', 'Workspace root cannot be removed', absolute);
            await this.client.remove(this.workspace, absolute, options.recursive ?? false, options.force ?? false, options.abortSignal);
        });
    }
    async createTempDir(prefix = 'tmp-', abortSignal) {
        const invalid = invalidTempSegment(prefix);
        if (invalid)
            return err(new FileError('invalid', invalid));
        return this.unaddressedFileResult(abortSignal, async () => {
            const created = await this.client.createTempDir(this.workspace, prefix, abortSignal);
            this.requireContained(created, created);
            return created;
        });
    }
    async createTempFile(options = {}) {
        const invalid = invalidTempSegment(options.prefix ?? '') || invalidTempSegment(options.suffix ?? '');
        if (invalid)
            return err(new FileError('invalid', invalid));
        return this.unaddressedFileResult(options.abortSignal, async () => {
            const created = await this.client.createTempFile(this.workspace, options.prefix ?? '', options.suffix ?? '', options.abortSignal);
            this.requireContained(created, created);
            return created;
        });
    }
    async cleanup() {
        try {
            await this.client.cleanup(this.workspace);
        }
        catch {
            // Pi requires cleanup to be best-effort and never reject.
        }
    }
    async fileResult(input, abortSignal, operation, validateCanonical = true) {
        if (abortSignal?.aborted)
            return err(new FileError('aborted', 'File operation aborted', input));
        const addressed = this.addressedPath(input);
        if (!addressed.ok)
            return addressed;
        try {
            if (validateCanonical) {
                const canonical = await this.client.canonicalPath(this.workspace, addressed.value, abortSignal);
                this.requireContained(canonical, input);
            }
            return ok(await operation(addressed.value));
        }
        catch (error) {
            return err(fileError(error, input, abortSignal));
        }
    }
    async unaddressedFileResult(abortSignal, operation) {
        if (abortSignal?.aborted)
            return err(new FileError('aborted', 'File operation aborted'));
        try {
            return ok(await operation());
        }
        catch (error) {
            return err(fileError(error, undefined, abortSignal));
        }
    }
    addressedPath(input) {
        if (input.includes('\0'))
            return err(new FileError('invalid', 'Path contains a null byte', input));
        const absolute = path.posix.isAbsolute(input)
            ? path.posix.normalize(input)
            : path.posix.resolve(this.cwd, input);
        try {
            this.requireContained(absolute, input);
            return ok(absolute);
        }
        catch (error) {
            return err(fileError(error, input));
        }
    }
    requireContained(candidate, input) {
        const normalized = path.posix.normalize(candidate);
        if (normalized !== this.cwd && !normalized.startsWith(`${this.cwd}/`)) {
            throw new FileError('permission_denied', `Path escapes Agent workspace: ${input}`, candidate);
        }
    }
}
function normalizeRoot(root) {
    const normalized = path.posix.resolve('/', root);
    if (normalized === '/')
        throw new Error('Agent workspace root cannot be the filesystem root');
    return normalized;
}
function bytes(content) {
    return typeof content === 'string' ? Buffer.from(content, 'utf8') : new Uint8Array(content);
}
function fileError(error, addressedPath, abortSignal) {
    if (error instanceof FileError)
        return error;
    if (abortSignal?.aborted)
        return new FileError('aborted', 'File operation aborted', addressedPath, asError(error));
    const cause = asError(error);
    const code = errorCode(cause);
    return new FileError(code, cause.message, addressedPath, cause);
}
function executionError(error, abortSignal) {
    if (error instanceof ExecutionError)
        return error;
    if (abortSignal?.aborted)
        return new ExecutionError('aborted', 'Command execution aborted', asError(error));
    const cause = asError(error);
    const code = cause.name === 'TimeoutError' ? 'timeout' : cause.name === 'CallbackError' ? 'callback_error' : 'spawn_error';
    return new ExecutionError(code, cause.message, cause);
}
function errorCode(error) {
    const code = error.code;
    if (code === 'ENOENT')
        return 'not_found';
    if (code === 'EACCES' || code === 'EPERM')
        return 'permission_denied';
    if (code === 'ENOTDIR')
        return 'not_directory';
    if (code === 'EISDIR')
        return 'is_directory';
    if (code === 'EINVAL')
        return 'invalid';
    if (code === 'ENOTSUP' || code === 'EOPNOTSUPP')
        return 'not_supported';
    return 'unknown';
}
function invalidTempSegment(value) {
    if (value.includes('\0') || value.includes('/') || value.includes('\\') || value === '..' || value.includes('../')) {
        return 'Temporary path prefix and suffix must be plain filename segments';
    }
    return '';
}
function asError(error) {
    return error instanceof Error ? error : new Error(String(error));
}
