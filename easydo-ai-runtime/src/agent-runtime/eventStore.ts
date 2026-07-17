import { AgentRuntimeProjection } from './projection.js'
import type { AgentRuntimeEvent } from './events.js'

export interface AgentEventStore {
  append(event: AgentRuntimeEvent): Promise<AgentRuntimeEvent>
  replay(sessionID: string, afterSeq?: number): Promise<AgentRuntimeEvent[]>
  replayRun(runtimeRunID: string, afterSeq?: number): Promise<AgentRuntimeEvent[]>
  project(sessionID: string): Promise<ReturnType<AgentRuntimeProjection['getSession']>>
  subscribe(sessionID: string, listener: (event: AgentRuntimeEvent) => void | Promise<void>): () => void
}

export class InMemoryAgentEventStore implements AgentEventStore {
  private eventsBySession = new Map<string, AgentRuntimeEvent[]>()
  private listenersBySession = new Map<string, Set<(event: AgentRuntimeEvent) => void | Promise<void>>>()

  async append(event: AgentRuntimeEvent): Promise<AgentRuntimeEvent> {
    const events = this.eventsBySession.get(event.session_id) ?? []
    if (event.event_id) {
      const existing = events.find((candidate) => candidate.event_id === event.event_id)
      if (existing) return existing
    }
    const seq = events.length + 1
    const stored = {
      ...event,
      seq,
      event_id: event.event_id || `${event.session_id}:${seq}`
    } as AgentRuntimeEvent
    events.push(stored)
    this.eventsBySession.set(event.session_id, events)
    for (const listener of this.listenersBySession.get(event.session_id) ?? []) {
      void Promise.resolve(listener(stored))
    }
    return stored
  }

  async replay(sessionID: string, afterSeq = 0): Promise<AgentRuntimeEvent[]> {
    return (this.eventsBySession.get(sessionID) ?? []).filter((event) => event.seq > afterSeq)
  }

  async replayRun(runtimeRunID: string, afterSeq = 0): Promise<AgentRuntimeEvent[]> {
    const events: AgentRuntimeEvent[] = []
    for (const sessionEvents of this.eventsBySession.values()) {
      events.push(...sessionEvents.filter((event) => event.runtime_run_id === runtimeRunID && event.seq > afterSeq))
    }
    return events.sort((left, right) => left.seq - right.seq)
  }

  async project(sessionID: string): Promise<ReturnType<AgentRuntimeProjection['getSession']>> {
    const projection = new AgentRuntimeProjection()
    for (const event of await this.replay(sessionID)) projection.apply(event)
    return projection.getSession(sessionID)
  }

  subscribe(sessionID: string, listener: (event: AgentRuntimeEvent) => void | Promise<void>): () => void {
    const listeners = this.listenersBySession.get(sessionID) ?? new Set<(event: AgentRuntimeEvent) => void | Promise<void>>()
    listeners.add(listener)
    this.listenersBySession.set(sessionID, listeners)
    return () => {
      listeners.delete(listener)
      if (listeners.size === 0) this.listenersBySession.delete(sessionID)
    }
  }
}
