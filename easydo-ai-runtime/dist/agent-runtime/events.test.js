import { describe, expect, it } from 'vitest';
import { projectRunState } from './events.js';
describe('projectRunState', () => {
    it.each([
        ['run.completed', 'completed'],
        ['run.failed', 'failed'],
        ['run.cancelled', 'cancelled'],
        ['run.timeout', 'timeout'],
        ['run.awaiting_decision', 'awaiting_decision']
    ])('derives %s replay state', (type, status) => {
        const projection = projectRunState([
            { type: 'run.started' },
            { type }
        ]);
        expect(projection.status).toBe(status);
        expect(projection.terminalEventCount).toBe(type === 'run.awaiting_decision' ? 0 : 1);
    });
    it('reports conflicting terminal replay instead of silently choosing one', () => {
        const projection = projectRunState([
            { type: 'run.started' },
            { type: 'run.completed' },
            { type: 'run.failed' }
        ]);
        expect(projection.terminalEventCount).toBe(2);
        expect(projection.hasTerminalConflict).toBe(true);
    });
});
