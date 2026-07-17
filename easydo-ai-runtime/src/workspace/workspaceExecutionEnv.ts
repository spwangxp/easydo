import path from 'node:path'
import {
  ExecutionError,
  FileError,
  err,
  ok,
  type ExecutionEnv,
  type FileInfo,
  type Result,
  type ShellExecOptions
} from '@earendil-works/pi-agent-core'
import type { AIRuntimeWorkspace } from './types.js'

export interface WorkspaceExecutionClient {
  exec(
    workspace: AIRuntimeWorkspace,
    command: string,
    options: ShellExecOptions & { cwd: string }
  ): Promise<{ stdout: string, stderr: string, exitCode: number }>
  readFile(workspace: AIRuntimeWorkspace, path: string, abortSignal?: AbortSignal): Promise<Uint8Array>
  writeFile(workspace: AIRuntimeWorkspace, path: string, content: Uint8Array, append: boolean, abortSignal?: AbortSignal): Promise<void>
  fileInfo(workspace: AIRuntimeWorkspace, path: string, abortSignal?: AbortSignal): Promise<FileInfo>
  listDir(workspace: AIRuntimeWorkspace, path: string, abortSignal?: AbortSignal): Promise<FileInfo[]>
  canonicalPath(workspace: AIRuntimeWorkspace, path: string, abortSignal?: AbortSignal): Promise<string>
  exists(workspace: AIRuntimeWorkspace, path: string, abortSignal?: AbortSignal): Promise<boolean>
  createDir(workspace: AIRuntimeWorkspace, path: string, recursive: boolean, abortSignal?: AbortSignal): Promise<void>
  remove(workspace: AIRuntimeWorkspace, path: string, recursive: boolean, force: boolean, abortSignal?: AbortSignal): Promise<void>
  createTempDir(workspace: AIRuntimeWorkspace, prefix: string, abortSignal?: AbortSignal): Promise<string>
  createTempFile(workspace: AIRuntimeWorkspace, prefix: string, suffix: string, abortSignal?: AbortSignal): Promise<string>
  cleanup(workspace: AIRuntimeWorkspace): Promise<void>
}

export class WorkspaceExecutionEnv implements ExecutionEnv {
  readonly cwd: string

  constructor(
    private readonly workspace: AIRuntimeWorkspace,
    private readonly client: WorkspaceExecutionClient
  ) {
    this.cwd = normalizeRoot(workspace.root_path)
  }

  async absolutePath(input: string, abortSignal?: AbortSignal): Promise<Result<string, FileError>> {
    return this.fileResult(input, abortSignal, async (absolute) => absolute, false)
  }

  async joinPath(parts: string[], abortSignal?: AbortSignal): Promise<Result<string, FileError>> {
    if (abortSignal?.aborted) return err(new FileError('aborted', 'File operation aborted'))
    return this.absolutePath(path.posix.join(...parts), abortSignal)
  }

  async exec(
    command: string,
    options: ShellExecOptions = {}
  ): Promise<Result<{ stdout: string, stderr: string, exitCode: number }, ExecutionError>> {
    if (options.abortSignal?.aborted) return err(new ExecutionError('aborted', 'Command execution aborted'))
    const cwd = this.addressedPath(options.cwd || this.cwd)
    if (!cwd.ok) return err(new ExecutionError('spawn_error', cwd.error.message, cwd.error))
    try {
      const canonical = await this.client.canonicalPath(this.workspace, cwd.value, options.abortSignal)
      this.requireContained(canonical, options.cwd || this.cwd)
      return ok(await this.client.exec(this.workspace, command, { ...options, cwd: cwd.value }))
    } catch (error) {
      return err(executionError(error, options.abortSignal))
    }
  }

  async readTextFile(input: string, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, async (absolute) => Buffer.from(await this.client.readFile(this.workspace, absolute, abortSignal)).toString('utf8'))
  }

  async readTextLines(
    input: string,
    options: { maxLines?: number, abortSignal?: AbortSignal } = {}
  ): Promise<Result<string[], FileError>> {
    const result = await this.readTextFile(input, options.abortSignal)
    if (!result.ok) return result
    const lines = result.value.split(/\r?\n/)
    if (lines.at(-1) === '') lines.pop()
    return ok(options.maxLines === undefined ? lines : lines.slice(0, Math.max(0, options.maxLines)))
  }

  async readBinaryFile(input: string, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, async (absolute) => new Uint8Array(await this.client.readFile(this.workspace, absolute, abortSignal)))
  }

  async writeFile(input: string, content: string | Uint8Array, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, async (absolute) => {
      await this.client.writeFile(this.workspace, absolute, bytes(content), false, abortSignal)
    })
  }

  async appendFile(input: string, content: string | Uint8Array, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, async (absolute) => {
      await this.client.writeFile(this.workspace, absolute, bytes(content), true, abortSignal)
    })
  }

  async fileInfo(input: string, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, (absolute) => this.client.fileInfo(this.workspace, absolute, abortSignal), false)
  }

  async listDir(input: string, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, async (absolute) => {
      const entries = await this.client.listDir(this.workspace, absolute, abortSignal)
      for (const entry of entries) this.requireContained(entry.path, input)
      return entries
    })
  }

  async canonicalPath(input: string, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, async (absolute) => {
      const canonical = await this.client.canonicalPath(this.workspace, absolute, abortSignal)
      this.requireContained(canonical, input)
      return canonical
    }, false)
  }

  async exists(input: string, abortSignal?: AbortSignal) {
    return this.fileResult(input, abortSignal, (absolute) => this.client.exists(this.workspace, absolute, abortSignal))
  }

  async createDir(input: string, options: { recursive?: boolean, abortSignal?: AbortSignal } = {}) {
    return this.fileResult(input, options.abortSignal, async (absolute) => {
      await this.client.createDir(this.workspace, absolute, options.recursive ?? true, options.abortSignal)
    })
  }

  async remove(input: string, options: { recursive?: boolean, force?: boolean, abortSignal?: AbortSignal } = {}) {
    return this.fileResult(input, options.abortSignal, async (absolute) => {
      if (absolute === this.cwd) throw new FileError('permission_denied', 'Workspace root cannot be removed', absolute)
      await this.client.remove(this.workspace, absolute, options.recursive ?? false, options.force ?? false, options.abortSignal)
    })
  }

  async createTempDir(prefix = 'tmp-', abortSignal?: AbortSignal): Promise<Result<string, FileError>> {
    const invalid = invalidTempSegment(prefix)
    if (invalid) return err(new FileError('invalid', invalid))
    return this.unaddressedFileResult(abortSignal, async () => {
      const created = await this.client.createTempDir(this.workspace, prefix, abortSignal)
      this.requireContained(created, created)
      return created
    })
  }

  async createTempFile(
    options: { prefix?: string, suffix?: string, abortSignal?: AbortSignal } = {}
  ): Promise<Result<string, FileError>> {
    const invalid = invalidTempSegment(options.prefix ?? '') || invalidTempSegment(options.suffix ?? '')
    if (invalid) return err(new FileError('invalid', invalid))
    return this.unaddressedFileResult(options.abortSignal, async () => {
      const created = await this.client.createTempFile(this.workspace, options.prefix ?? '', options.suffix ?? '', options.abortSignal)
      this.requireContained(created, created)
      return created
    })
  }

  async cleanup() {
    try {
      await this.client.cleanup(this.workspace)
    } catch {
      // Pi requires cleanup to be best-effort and never reject.
    }
  }

  private async fileResult<T>(
    input: string,
    abortSignal: AbortSignal | undefined,
    operation: (absolute: string) => Promise<T>,
    validateCanonical = true
  ): Promise<Result<T, FileError>> {
    if (abortSignal?.aborted) return err(new FileError('aborted', 'File operation aborted', input))
    const addressed = this.addressedPath(input)
    if (!addressed.ok) return addressed
    try {
      if (validateCanonical) {
        const canonical = await this.client.canonicalPath(this.workspace, addressed.value, abortSignal)
        this.requireContained(canonical, input)
      }
      return ok(await operation(addressed.value))
    } catch (error) {
      return err(fileError(error, input, abortSignal))
    }
  }

  private async unaddressedFileResult<T>(abortSignal: AbortSignal | undefined, operation: () => Promise<T>): Promise<Result<T, FileError>> {
    if (abortSignal?.aborted) return err(new FileError('aborted', 'File operation aborted'))
    try {
      return ok(await operation())
    } catch (error) {
      return err(fileError(error, undefined, abortSignal))
    }
  }

  private addressedPath(input: string): Result<string, FileError> {
    if (input.includes('\0')) return err(new FileError('invalid', 'Path contains a null byte', input))
    const absolute = path.posix.isAbsolute(input)
      ? path.posix.normalize(input)
      : path.posix.resolve(this.cwd, input)
    try {
      this.requireContained(absolute, input)
      return ok(absolute)
    } catch (error) {
      return err(fileError(error, input))
    }
  }

  private requireContained(candidate: string, input: string) {
    const normalized = path.posix.normalize(candidate)
    if (normalized !== this.cwd && !normalized.startsWith(`${this.cwd}/`)) {
      throw new FileError('permission_denied', `Path escapes Agent workspace: ${input}`, candidate)
    }
  }
}

function normalizeRoot(root: string) {
  const normalized = path.posix.resolve('/', root)
  if (normalized === '/') throw new Error('Agent workspace root cannot be the filesystem root')
  return normalized
}

function bytes(content: string | Uint8Array) {
  return typeof content === 'string' ? Buffer.from(content, 'utf8') : new Uint8Array(content)
}

function fileError(error: unknown, addressedPath?: string, abortSignal?: AbortSignal) {
  if (error instanceof FileError) return error
  if (abortSignal?.aborted) return new FileError('aborted', 'File operation aborted', addressedPath, asError(error))
  const cause = asError(error)
  const code = errorCode(cause)
  return new FileError(code, cause.message, addressedPath, cause)
}

function executionError(error: unknown, abortSignal?: AbortSignal) {
  if (error instanceof ExecutionError) return error
  if (abortSignal?.aborted) return new ExecutionError('aborted', 'Command execution aborted', asError(error))
  const cause = asError(error)
  const code = cause.name === 'TimeoutError' ? 'timeout' : cause.name === 'CallbackError' ? 'callback_error' : 'spawn_error'
  return new ExecutionError(code, cause.message, cause)
}

function errorCode(error: Error): FileError['code'] {
  const code = (error as NodeJS.ErrnoException).code
  if (code === 'ENOENT') return 'not_found'
  if (code === 'EACCES' || code === 'EPERM') return 'permission_denied'
  if (code === 'ENOTDIR') return 'not_directory'
  if (code === 'EISDIR') return 'is_directory'
  if (code === 'EINVAL') return 'invalid'
  if (code === 'ENOTSUP' || code === 'EOPNOTSUPP') return 'not_supported'
  return 'unknown'
}

function invalidTempSegment(value: string) {
  if (value.includes('\0') || value.includes('/') || value.includes('\\') || value === '..' || value.includes('../')) {
    return 'Temporary path prefix and suffix must be plain filename segments'
  }
  return ''
}

function asError(error: unknown) {
  return error instanceof Error ? error : new Error(String(error))
}
