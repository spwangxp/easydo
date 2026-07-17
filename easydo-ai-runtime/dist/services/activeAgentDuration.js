/**
 * Canonical active-agent duration (agent_active_duration_v2).
 * Runtime is the source of truth when finalizing a Run/Entry; frontend uses the
 * same rules for live partial projection and historical entries without timings.
 */
export const AGENT_ACTIVE_DURATION_ALGORITHM = 'agent_active_duration_v2';
const STEP_START = 'session.step.started';
const STEP_END = 'session.step.ended';
const STEP_FAILED = 'session.step.failed';
const RUN_TERMINALS = new Set([
    'run.completed',
    'run.failed',
    'run.cancelled',
    'run.timeout',
    'run.interrupted'
]);
const PERMISSION_ASKED = 'permission.asked';
const PERMISSION_RESOLVED = 'permission.resolved';
function eventName(event = {}) {
    return String(event.type ||
        event.event ||
        event.event_type ||
        event.payload?.event_type ||
        event.data?.event_type ||
        '');
}
function eventData(event = {}) {
    const payload = event.payload;
    const data = event.data;
    const payloadJson = event.payload_json;
    const nested = (payload && typeof payload === 'object' && !Array.isArray(payload) ? payload : null) ||
        (data && typeof data === 'object' && !Array.isArray(data) ? data : null) ||
        (payloadJson && typeof payloadJson === 'object' && !Array.isArray(payloadJson) ? payloadJson : null) ||
        {};
    return { ...event, ...nested };
}
function eventTimestampMs(event = {}) {
    const data = eventData(event);
    const value = Date.parse(String(data.timestamp || event.timestamp || event.created_at || ''));
    return Number.isFinite(value) ? value : NaN;
}
function firstString(...values) {
    for (const value of values) {
        if (value !== undefined && value !== null && String(value).trim())
            return String(value).trim();
    }
    return '';
}
function overlapMs(start, end, pauseStart, pauseEnd) {
    const overlapStart = Math.max(start, pauseStart);
    const overlapEnd = Math.min(end, pauseEnd);
    return overlapEnd > overlapStart ? overlapEnd - overlapStart : 0;
}
/**
 * Sum active agent time from runtime events.
 * - Closes steps on session.step.ended | session.step.failed
 * - Truncates open steps on run terminal events
 * - Excludes permission.asked → permission.resolved waits
 */
export function computeActiveAgentDuration(events = []) {
    const stepWindows = [];
    let openStepStart = NaN;
    let quality = 'exact';
    let degradedReason = '';
    const ordered = [...events];
    for (const event of ordered) {
        const name = eventName(event);
        const ts = eventTimestampMs(event);
        if (!Number.isFinite(ts))
            continue;
        if (name === STEP_START) {
            openStepStart = ts;
            continue;
        }
        if ((name === STEP_END || name === STEP_FAILED) && Number.isFinite(openStepStart)) {
            stepWindows.push([openStepStart, ts, name === STEP_FAILED ? 'failed' : 'exact']);
            if (name === STEP_FAILED) {
                quality = 'degraded';
                degradedReason = degradedReason || 'step_failed';
            }
            openStepStart = NaN;
            continue;
        }
        if (RUN_TERMINALS.has(name) && Number.isFinite(openStepStart)) {
            stepWindows.push([openStepStart, ts, 'run_terminal']);
            quality = 'degraded';
            degradedReason = degradedReason || 'run_terminal_truncation';
            openStepStart = NaN;
        }
    }
    const pauseWindows = [];
    const openAsks = new Map();
    for (const event of ordered) {
        const name = eventName(event);
        const ts = eventTimestampMs(event);
        if (!Number.isFinite(ts))
            continue;
        const data = eventData(event);
        const callId = firstString(data.call_id, data.tool_call_id, data.provider_tool_call_id, data.request_id, data.approval_id);
        if (name === PERMISSION_ASKED && callId && !openAsks.has(callId)) {
            openAsks.set(callId, ts);
            continue;
        }
        if (name === PERMISSION_RESOLVED && callId && openAsks.has(callId)) {
            pauseWindows.push([openAsks.get(callId), ts]);
            openAsks.delete(callId);
        }
    }
    // Unresolved approvals: do not keep accruing past last step end (already truncated by run terminal).
    let total = 0;
    for (const [start, end] of stepWindows) {
        let active = Math.max(0, end - start);
        for (const [pauseStart, pauseEnd] of pauseWindows) {
            active -= overlapMs(start, end, pauseStart, pauseEnd);
        }
        total += Math.max(0, active);
    }
    if (stepWindows.length === 0) {
        return {
            active_duration_ms: 0,
            metric_quality: 'degraded',
            algorithm_version: AGENT_ACTIVE_DURATION_ALGORITHM,
            degraded_reason: 'no_step_windows'
        };
    }
    return {
        active_duration_ms: Math.max(0, Math.round(total)),
        metric_quality: quality,
        algorithm_version: AGENT_ACTIVE_DURATION_ALGORITHM,
        ...(degradedReason ? { degraded_reason: degradedReason } : {})
    };
}
/** Merge active-duration fields into a timings record (Runtime entry/run output). */
export function attachActiveDurationTimings(timings = {}, events = []) {
    const computed = computeActiveAgentDuration(events);
    const next = {
        ...timings,
        active_duration_ms: computed.active_duration_ms,
        metric_quality: computed.metric_quality,
        algorithm_version: computed.algorithm_version
    };
    if (computed.degraded_reason)
        next.degraded_reason = computed.degraded_reason;
    // Keep wall-clock total_ms if already present; do not overwrite with active duration.
    return next;
}
