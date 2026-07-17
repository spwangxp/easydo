import { RuntimeDomainError } from './runtimeErrors.js'

export type SubagentMode = 'read_only' | 'write'

const WRITE_OPERATION_TYPES = new Set([
  'write',
  'create',
  'update',
  'delete',
  'mutate',
  'mutation',
  'execute',
  'deploy',
  'run'
])

export function normalizeSubagentMode(raw: unknown): SubagentMode {
  return typeof raw === 'string' && raw.toLowerCase().includes('write') ? 'write' : 'read_only'
}

export function resolveSubagentMode(configuredRaw: unknown, requestedRaw: unknown): SubagentMode {
  const configuredMode = normalizeSubagentMode(configuredRaw)
  if (configuredMode === 'read_only') return 'read_only'
  return requestedRaw === undefined || requestedRaw === null || requestedRaw === ''
    ? 'write'
    : normalizeSubagentMode(requestedRaw)
}

export function isWriteOperationType(operationType: string): boolean {
  return WRITE_OPERATION_TYPES.has(operationType.trim().toLowerCase())
}

export function operationTypeFromToolMeta(meta: Record<string, unknown>): string {
  for (const value of [meta.operation_type, meta.operationType, meta.operation]) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

export function assertReadOnlyAllowsOperation(mode: SubagentMode, operationType: string): void {
  if (mode === 'write' || operationType.trim().toLowerCase() === 'read') return
  throw new RuntimeDomainError(
    'subagent_read_only_violation',
    `Subagent mode read_only does not allow operation ${operationType.trim() || 'unknown'}`,
    403
  )
}

export function filterToolsByMode<T extends { operation_type?: string; name?: string }>(
  mode: SubagentMode,
  tools: T[],
  resolveOperation: (tool: T) => string
): T[] {
  if (mode === 'write') return tools
  return tools.filter((tool) => resolveOperation(tool).trim().toLowerCase() === 'read')
}
