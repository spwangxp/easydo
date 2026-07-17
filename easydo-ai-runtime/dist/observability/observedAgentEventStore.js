export class ObservedAgentEventStore {
    delegate;
    metrics;
    now;
    constructor(delegate, metrics, now = Date.now) {
        this.delegate = delegate;
        this.metrics = metrics;
        this.now = now;
    }
    async append(event) {
        const startedAt = this.now();
        const stored = await this.delegate.append(event);
        const completedAt = this.now();
        const eventAt = Date.parse(String(event.timestamp || ''));
        const delayStartedAt = Number.isFinite(eventAt) ? eventAt : startedAt;
        this.metrics.observe('ai_runtime_publish_delay_seconds', Math.max(0, completedAt - delayStartedAt) / 1000, { outcome: 'completed' }, completedAt);
        return stored;
    }
    replay(sessionID, afterSeq = 0) {
        return this.delegate.replay(sessionID, afterSeq);
    }
    replayRun(runtimeRunID, afterSeq = 0) {
        return this.delegate.replayRun(runtimeRunID, afterSeq);
    }
    project(sessionID) {
        return this.delegate.project(sessionID);
    }
    subscribe(sessionID, listener) {
        return this.delegate.subscribe(sessionID, listener);
    }
}
