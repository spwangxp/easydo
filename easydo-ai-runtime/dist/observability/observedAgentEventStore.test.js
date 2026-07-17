import { describe, expect, it } from 'vitest';
import { InMemoryAgentEventStore } from '../agent-runtime/eventStore.js';
import { ObservedAgentEventStore } from './observedAgentEventStore.js';
import { RuntimeMetrics } from './runtimeMetrics.js';
describe('ObservedAgentEventStore', () => {
    it('records event timestamp to durable append completion as publish delay', async () => {
        let now = Date.parse('2026-07-12T01:00:00.100Z');
        const metrics = new RuntimeMetrics({ instanceID: 'runtime-a' });
        const store = new ObservedAgentEventStore(new InMemoryAgentEventStore(), metrics, () => now);
        const event = {
            type: 'session.text.delta',
            session_id: 'session-1',
            runtime_run_id: 'run-1',
            event_id: '',
            seq: 0,
            timestamp: '2026-07-12T01:00:00.000Z'
        };
        now += 25;
        await store.append(event);
        expect(metrics.latencySummary('ai_runtime_publish_delay_seconds', 5 * 60_000, now)).toEqual({
            count: 1,
            average_ms: 125,
            p50_ms: 125,
            p95_ms: 125,
            max_ms: 125
        });
        await expect(store.replayRun('run-1')).resolves.toHaveLength(1);
    });
});
