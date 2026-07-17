import type { AgentEventStore } from '../agent-runtime/eventStore.js'
import type { AgentRuntimeEvent } from '../agent-runtime/events.js'
import type { RuntimeMetrics } from './runtimeMetrics.js'

export class ObservedAgentEventStore implements AgentEventStore {
  constructor(
    private readonly delegate: AgentEventStore,
    private readonly metrics: RuntimeMetrics,
    private readonly now: () => number = Date.now
  ) {}

  async append(event: AgentRuntimeEvent) {
    const startedAt = this.now()
    const stored = await this.delegate.append(event)
    const completedAt = this.now()
    const eventAt = Date.parse(String(event.timestamp || ''))
    const delayStartedAt = Number.isFinite(eventAt) ? eventAt : startedAt
    this.metrics.observe(
      'ai_runtime_publish_delay_seconds',
      Math.max(0, completedAt - delayStartedAt) / 1000,
      { outcome: 'completed' },
      completedAt
    )
    return stored
  }

  replay(sessionID: string, afterSeq = 0) {
    return this.delegate.replay(sessionID, afterSeq)
  }

  replayRun(runtimeRunID: string, afterSeq = 0) {
    return this.delegate.replayRun(runtimeRunID, afterSeq)
  }

  project(sessionID: string) {
    return this.delegate.project(sessionID)
  }

  subscribe(sessionID: string, listener: (event: AgentRuntimeEvent) => void | Promise<void>) {
    return this.delegate.subscribe(sessionID, listener)
  }
}
