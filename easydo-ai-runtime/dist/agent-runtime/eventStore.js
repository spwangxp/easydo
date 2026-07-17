import { AgentRuntimeProjection } from './projection.js';
export class InMemoryAgentEventStore {
    eventsBySession = new Map();
    listenersBySession = new Map();
    async append(event) {
        const events = this.eventsBySession.get(event.session_id) ?? [];
        if (event.event_id) {
            const existing = events.find((candidate) => candidate.event_id === event.event_id);
            if (existing)
                return existing;
        }
        const seq = events.length + 1;
        const stored = {
            ...event,
            seq,
            event_id: event.event_id || `${event.session_id}:${seq}`
        };
        events.push(stored);
        this.eventsBySession.set(event.session_id, events);
        for (const listener of this.listenersBySession.get(event.session_id) ?? []) {
            void Promise.resolve(listener(stored));
        }
        return stored;
    }
    async replay(sessionID, afterSeq = 0) {
        return (this.eventsBySession.get(sessionID) ?? []).filter((event) => event.seq > afterSeq);
    }
    async replayRun(runtimeRunID, afterSeq = 0) {
        const events = [];
        for (const sessionEvents of this.eventsBySession.values()) {
            events.push(...sessionEvents.filter((event) => event.runtime_run_id === runtimeRunID && event.seq > afterSeq));
        }
        return events.sort((left, right) => left.seq - right.seq);
    }
    async project(sessionID) {
        const projection = new AgentRuntimeProjection();
        for (const event of await this.replay(sessionID))
            projection.apply(event);
        return projection.getSession(sessionID);
    }
    subscribe(sessionID, listener) {
        const listeners = this.listenersBySession.get(sessionID) ?? new Set();
        listeners.add(listener);
        this.listenersBySession.set(sessionID, listeners);
        return () => {
            listeners.delete(listener);
            if (listeners.size === 0)
                this.listenersBySession.delete(sessionID);
        };
    }
}
