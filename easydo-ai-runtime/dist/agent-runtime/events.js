const RUN_STATE_BY_EVENT = {
    'run.completed': 'completed',
    'run.failed': 'failed',
    'run.cancelled': 'cancelled',
    'run.timeout': 'timeout',
    'run.awaiting_decision': 'awaiting_decision',
    'run.interrupted': 'interrupted'
};
const TERMINAL_RUN_STATE_EVENTS = new Set([
    'run.completed',
    'run.failed',
    'run.cancelled',
    'run.timeout',
    'run.interrupted'
]);
export function isRunStateEventType(type) {
    return Object.prototype.hasOwnProperty.call(RUN_STATE_BY_EVENT, type);
}
export function isTerminalRunStateEventType(type) {
    return isRunStateEventType(type) && TERMINAL_RUN_STATE_EVENTS.has(type);
}
export function runStateEventTypeForStatus(status) {
    const match = Object.entries(RUN_STATE_BY_EVENT)
        .find(([, projectedStatus]) => projectedStatus === status);
    return match?.[0] || '';
}
export function projectRunState(events) {
    const stateEvents = events.filter((event) => isRunStateEventType(event.type));
    const terminalEvents = stateEvents.filter((event) => isTerminalRunStateEventType(event.type));
    const latest = stateEvents.at(-1);
    return {
        status: latest && isRunStateEventType(latest.type) ? RUN_STATE_BY_EVENT[latest.type] : undefined,
        terminalEventCount: terminalEvents.length,
        hasTerminalConflict: new Set(terminalEvents.map((event) => event.type)).size > 1
    };
}
export function isDurableRuntimeEvent(type) {
    return Boolean(type);
}
