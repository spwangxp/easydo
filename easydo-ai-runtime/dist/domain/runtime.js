import { createHash } from 'node:crypto';
export function stableJSONStringify(value) {
    if (value === undefined)
        return 'null';
    if (value === null || typeof value !== 'object')
        return JSON.stringify(value);
    if (Array.isArray(value))
        return `[${value.map((item) => stableJSONStringify(item)).join(',')}]`;
    const record = value;
    return `{${Object.keys(record)
        .filter((key) => record[key] !== undefined)
        .sort()
        .map((key) => `${JSON.stringify(key)}:${stableJSONStringify(record[key])}`)
        .join(',')}}`;
}
function isSensitiveSnapshotField(key) {
    return /^(authorization|proxy-authorization|cookie|set-cookie|token|secret)$/i.test(key) ||
        /(api[_-]?key|access[_-]?token|bearer[_-]?token|password|private[_-]?key|client[_-]?secret|auth[_-]?token|[_-]token|[_-]secret)/i.test(key);
}
function isReferenceIdentityField(key) {
    return key === 'id' || key === 'reference' || key === 'ref' || /_(id|ref)$/.test(key);
}
function sanitizeSecretReference(value) {
    if (!value || typeof value !== 'object' || Array.isArray(value))
        return {};
    const source = value;
    const sanitized = {};
    for (const [key, item] of Object.entries(source)) {
        if ((key === 'configured' || key === 'masked') && typeof item === 'boolean') {
            sanitized[key] = item;
            continue;
        }
        if (isReferenceIdentityField(key) && (typeof item === 'string' || typeof item === 'number')) {
            sanitized[key] = item;
        }
    }
    if (Object.keys(source).length > 0) {
        sanitized.configured = true;
        sanitized.masked = true;
    }
    return sanitized;
}
function sanitizeUnresolvedSnapshotValue(value) {
    if (Array.isArray(value))
        return value.map((item) => sanitizeUnresolvedSnapshotValue(item));
    if (!value || typeof value !== 'object')
        return value;
    const source = value;
    const sanitized = {};
    for (const [key, item] of Object.entries(source)) {
        if (key === 'secret_ref') {
            sanitized[key] = sanitizeSecretReference(item);
        }
        else if (isSensitiveSnapshotField(key)) {
            sanitized[key] = '[REDACTED]';
        }
        else {
            sanitized[key] = sanitizeUnresolvedSnapshotValue(item);
        }
    }
    return sanitized;
}
export function unresolvedAgentResourceSnapshot(resource) {
    const { created_at: _createdAt, updated_at: _updatedAt, ...snapshot } = resource;
    return sanitizeUnresolvedSnapshotValue(snapshot);
}
export function agentResourceSnapshotDigest(snapshot) {
    return `sha256:${createHash('sha256').update(stableJSONStringify(snapshot)).digest('hex')}`;
}
export function createAgentResourceVersion(params) {
    const snapshot = unresolvedAgentResourceSnapshot(params.resource);
    return {
        resource_version_id: params.resourceVersionID,
        resource_id: params.resource.id,
        workspace_id: params.resource.workspace_id,
        revision: params.revision,
        source_version: params.resource.version,
        snapshot_digest: agentResourceSnapshotDigest(snapshot),
        snapshot,
        created_by: params.createdBy ?? params.resource.created_by,
        created_at: params.createdAt || new Date().toISOString()
    };
}
export function assertAgentResourceVersionMatchesResource(resource, version) {
    const snapshot = unresolvedAgentResourceSnapshot(resource);
    const digest = agentResourceSnapshotDigest(snapshot);
    if (version.resource_id !== resource.id ||
        version.workspace_id !== resource.workspace_id ||
        version.source_version !== resource.version ||
        version.snapshot_digest !== digest ||
        agentResourceSnapshotDigest(version.snapshot) !== digest) {
        throw new Error('Agent resource revision does not match the resource head snapshot');
    }
}
export function assertAgentResourceVersionSnapshotSafe(version) {
    const sanitized = sanitizeUnresolvedSnapshotValue(version.snapshot);
    const digest = agentResourceSnapshotDigest(sanitized);
    if (stableJSONStringify(sanitized) !== stableJSONStringify(version.snapshot) || version.snapshot_digest !== digest) {
        throw new Error('Agent resource revision snapshot must be unresolved, secret-safe, and digest verified');
    }
}
export function agentProfileSnapshotHash(snapshot) {
    return `sha256:${createHash('sha256').update(stableJSONStringify(snapshot)).digest('hex')}`;
}
export const SESSION_QUEUE_ACTIVE_STATUSES = new Set([
    'pending',
    'claimed'
]);
export const SESSION_QUEUE_TERMINAL_STATUSES = new Set([
    'applied',
    'consumed',
    'cancelled',
    'expired',
    'failed'
]);
export function sessionQueueItemBusinessID(workspaceID, sessionID, itemSeq) {
    return `qi_w${workspaceID.toString(36)}_${sessionID.toString(36).padStart(6, '0')}_${itemSeq.toString(36).padStart(6, '0')}`;
}
export function sameSessionQueueItemSet(current, requested) {
    if (current.length !== requested.length || new Set(requested).size !== requested.length)
        return false;
    const expected = new Set(current);
    return requested.every((queueItemID) => expected.has(queueItemID));
}
export const ACTIVE_RUNTIME_RUN_STATUSES = new Set([
    'queued',
    'running',
    'awaiting_decision',
    'awaiting_input'
]);
export function activeSlotForRunStatus(status) {
    return ACTIVE_RUNTIME_RUN_STATUSES.has(status) ? 'active' : undefined;
}
export function normalizeRuntimeRunActiveSlot(run) {
    const activeSlot = activeSlotForRunStatus(run.status);
    if (activeSlot) {
        return { ...run, active_slot: activeSlot };
    }
    const { active_slot: _activeSlot, ...rest } = run;
    return rest;
}
