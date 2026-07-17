import { describe, expect, it } from 'vitest';
import { AgentHarnessRunner } from './harnessRunner.js';
import { InMemoryAgentEventStore } from './eventStore.js';
import { PiToolApprovalRequiredError } from './piResources.js';
import { createRuntimeLogger } from '../observability/runtimeLogger.js';
class FakeHarness {
    listeners = [];
    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter((candidate) => candidate !== listener);
        };
    }
    async prompt(text) {
        for (const listener of this.listeners) {
            await listener({ type: 'message_update', message: { role: 'assistant', content: [{ type: 'text', text: `answer:${text}` }] } });
            await listener({ type: 'tool_execution_start', toolCallId: 'call-1', toolName: 'lookup', args: { query: text } });
            await listener({ type: 'tool_execution_end', toolCallId: 'call-1', toolName: 'lookup', result: { content: [{ type: 'text', text: 'ok' }], details: { ok: true } }, isError: false });
        }
        return { role: 'assistant', content: [{ type: 'text', text: `answer:${text}` }] };
    }
    async abort() { }
}
class UsageHarness {
    subscribe() {
        return () => { };
    }
    async prompt() {
        return {
            role: 'assistant',
            content: [{ type: 'text', text: 'usage answer' }],
            usage: {
                input: 123,
                output: 45,
                reasoning: 6,
                cache: { read: 7, write: 8 },
                cost: { total: 0.0123 }
            }
        };
    }
}
class PiCamelUsageHarness {
    subscribe() {
        return () => { };
    }
    async prompt() {
        return {
            role: 'assistant',
            content: [{ type: 'text', text: 'pi usage answer' }],
            usage: {
                input: 21,
                output: 13,
                reasoning: 5,
                cacheRead: 3,
                cacheWrite: 2,
                totalTokens: 44,
                cost: { total: 0.0044 }
            }
        };
    }
}
class PiTotalOnlyUsageHarness {
    subscribe() {
        return () => { };
    }
    async prompt() {
        return {
            role: 'assistant',
            content: [{ type: 'text', text: 'pi total only usage answer' }],
            usage: {
                totalTokens: 44,
                cost: { total: 0.0044 }
            }
        };
    }
}
class SkillInvocationHarness {
    promptCalled = false;
    skillCall = null;
    listeners = [];
    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter((candidate) => candidate !== listener);
        };
    }
    async prompt() {
        this.promptCalled = true;
        return { role: 'assistant', content: [{ type: 'text', text: 'prompt fallback' }] };
    }
    async skill(name, instructions) {
        this.skillCall = { name, instructions };
        for (const listener of this.listeners) {
            await listener({ type: 'message_update', message: { role: 'assistant', content: [{ type: 'text', text: `skill:${name}:${instructions}` }] } });
        }
        return { role: 'assistant', content: [{ type: 'text', text: `skill:${name}:${instructions}` }] };
    }
}
class ApprovalHarness {
    subscribe() {
        return () => { };
    }
    async prompt() {
        throw new PiToolApprovalRequiredError('approval:call-1', 'call-1', 'deploy.write', 'Deploy write requires approval');
    }
}
class EventApprovalHarness {
    rejectPrompt;
    markStarted;
    started = new Promise((resolve) => {
        this.markStarted = resolve;
    });
    subscribe() {
        return () => { };
    }
    prompt() {
        return new Promise((_resolve, reject) => {
            this.rejectPrompt = reject;
            this.markStarted?.();
        });
    }
    async abort() {
        this.rejectPrompt?.(new Error('Pi harness stopped with aborted'));
    }
}
class FailureMessageHarness {
    subscribe() {
        return () => { };
    }
    async prompt() {
        return {
            role: 'assistant',
            content: [{ type: 'text', text: '' }],
            stopReason: 'error',
            errorMessage: 'No API key for provider: openrouter'
        };
    }
}
class CompactionHarness {
    listeners = [];
    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter((candidate) => candidate !== listener);
        };
    }
    async prompt() {
        for (const listener of this.listeners) {
            await listener({ type: 'session_before_compact', preparation: { firstKeptEntryId: 'compact-msg-1' } });
            await listener({ type: 'session_compact', compactionEntry: { id: 'compact-msg-1', summary: 'Earlier context summarized.', recent: 'Current request remains active.' } });
        }
        return { role: 'assistant', content: [{ type: 'text', text: 'done' }] };
    }
    async abort() { }
}
class AbortableHarness {
    aborted = false;
    listeners = [];
    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter((candidate) => candidate !== listener);
        };
    }
    async prompt() {
        await new Promise(() => { });
    }
    async abort() {
        this.aborted = true;
    }
}
class SafeCheckpointHarness {
    listeners = [];
    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter((candidate) => candidate !== listener);
        };
    }
    async prompt() {
        for (const listener of this.listeners) {
            await listener({ type: 'save_point' });
            await listener({ type: 'session.save_point' });
            await listener({ type: 'turn_save_point' });
        }
        return { role: 'assistant', content: [{ type: 'text', text: 'checkpoint complete' }] };
    }
}
class SteerableHarness {
    steerCalls = [];
    releasePrompt;
    started;
    markStarted;
    constructor() {
        this.started = new Promise((resolve) => {
            this.markStarted = resolve;
        });
    }
    subscribe() {
        return () => { };
    }
    async prompt() {
        this.markStarted?.();
        await new Promise((resolve) => {
            this.releasePrompt = resolve;
        });
        return { role: 'assistant', content: [{ type: 'text', text: 'steered' }] };
    }
    async steer(text) {
        this.steerCalls.push(text);
    }
    finish() {
        this.releasePrompt?.();
    }
}
describe('AgentHarnessRunner', () => {
    it('logs correlated harness outcomes without logging the prompt', async () => {
        const store = new InMemoryAgentEventStore();
        const lines = [];
        const runner = new AgentHarnessRunner({
            store,
            createHarness: () => new FakeHarness(),
            logger: createRuntimeLogger({ sink: (line) => lines.push(line) })
        });
        await runner.prompt({
            sessionID: 'sess-log-1',
            runtimeRunID: 'run-log-1',
            parentRuntimeRunID: 'run-parent-1',
            requestID: 'req-log-1',
            workspaceID: 11,
            prompt: 'prompt content must stay out of logs',
            model: { provider_id: 'provider-log-1', id: 'model-log-1' }
        });
        expect(lines.map((line) => JSON.parse(line))).toEqual([
            expect.objectContaining({
                level: 'info',
                component: 'pi-harness',
                operation: 'prompt',
                outcome: 'started',
                request_id: 'req-log-1',
                workspace_id: 11,
                session_id: 'sess-log-1',
                runtime_run_id: 'run-log-1',
                parent_runtime_run_id: 'run-parent-1',
                provider_id: 'provider-log-1'
            }),
            expect.objectContaining({
                level: 'info',
                component: 'pi-harness',
                operation: 'prompt',
                outcome: 'completed',
                runtime_run_id: 'run-log-1'
            })
        ]);
        expect(lines.join('\n')).not.toContain('prompt content must stay out of logs');
    });
    it('logs a terminal outcome when harness creation fails', async () => {
        const store = new InMemoryAgentEventStore();
        const lines = [];
        const runner = new AgentHarnessRunner({
            store,
            createHarness: () => { throw new Error('factory unavailable'); },
            logger: createRuntimeLogger({ sink: (line) => lines.push(line) })
        });
        await expect(runner.prompt({
            sessionID: 'sess-factory-log',
            runtimeRunID: 'run-factory-log',
            prompt: 'factory prompt secret'
        })).rejects.toThrow('factory unavailable');
        expect(lines.map((line) => JSON.parse(line))).toEqual([
            expect.objectContaining({ outcome: 'started', runtime_run_id: 'run-factory-log' }),
            expect.objectContaining({
                level: 'error',
                outcome: 'failed',
                runtime_run_id: 'run-factory-log',
                code: 'runtime_internal_error',
                category: 'internal'
            })
        ]);
        expect(lines.join('\n')).not.toContain('factory prompt secret');
    });
    it('runs a prompt through a Pi-compatible harness and stores OpenCode-style events', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new FakeHarness() });
        const result = await runner.prompt({ sessionID: 'sess-runner-1', runtimeRunID: 'run-runner-1', prompt: 'deploy status', agent: 'ops', model: { provider_id: 'test', id: 'fake' } });
        expect(result).toMatchObject({ session_id: 'sess-runner-1', user_message_id: expect.any(String), assistant_message_id: expect.any(String) });
        const events = await store.replay('sess-runner-1');
        expect(events.map((event) => event.type)).toEqual([
            'session.prompted',
            'session.step.started',
            'session.text.delta',
            'session.tool.called',
            'session.tool.success',
            'session.text.ended',
            'session.step.ended'
        ]);
        const projection = await store.project('sess-runner-1');
        expect(projection.messages).toHaveLength(2);
        expect(projection.messages[0]).toMatchObject({ role: 'user', text: 'deploy status' });
        expect(projection.partsByMessage[result.assistant_message_id]).toEqual([
            expect.objectContaining({ type: 'text', text: 'answer:deploy status' }),
            expect.objectContaining({ type: 'tool', tool: 'lookup', state: 'completed' })
        ]);
    });
    it('calls the safe checkpoint callback for Pi save point event shapes', async () => {
        const store = new InMemoryAgentEventStore();
        const checkpoints = [];
        const runner = new AgentHarnessRunner({ store, createHarness: () => new SafeCheckpointHarness() });
        await runner.prompt({
            sessionID: 'sess-checkpoint-1',
            runtimeRunID: 'run-checkpoint-1',
            prompt: 'wait for checkpoints',
            onSafeCheckpoint: async (checkpoint) => {
                checkpoints.push(checkpoint);
            }
        });
        expect(checkpoints).toEqual([
            { runtime_run_id: 'run-checkpoint-1', checkpoint: 'turn_save_point' },
            { runtime_run_id: 'run-checkpoint-1', checkpoint: 'turn_save_point' },
            { runtime_run_id: 'run-checkpoint-1', checkpoint: 'turn_save_point' }
        ]);
    });
    it('steers an active Pi harness once per queue item and clears acceptance after completion', async () => {
        const store = new InMemoryAgentEventStore();
        const harness = new SteerableHarness();
        const runner = new AgentHarnessRunner({ store, createHarness: () => harness });
        const running = runner.prompt({ sessionID: 'sess-steer-1', runtimeRunID: 'run-steer-1', prompt: 'long run' });
        await harness.started;
        await expect(runner.steer('run-steer-1', 'queue-steer-1', 'change direction')).resolves.toBe('accepted');
        await expect(runner.steer('run-steer-1', 'queue-steer-1', 'change direction')).resolves.toBe('already_accepted');
        expect(harness.steerCalls).toEqual(['change direction']);
        harness.finish();
        await running;
        await expect(runner.steer('run-steer-1', 'queue-steer-1', 'change direction')).resolves.toBe('run_not_active');
    });
    it('records token usage from the final Pi assistant message', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new UsageHarness() });
        await runner.prompt({ sessionID: 'sess-usage-1', runtimeRunID: 'run-usage-1', prompt: 'count tokens', agent: 'ops', model: { provider_id: 'test', id: 'fake' } });
        const events = await store.replay('sess-usage-1');
        const ended = events.find((event) => event.type === 'session.step.ended');
        expect(ended).toMatchObject({
            type: 'session.step.ended',
            tokens: {
                input: 123,
                output: 45,
                reasoning: 6,
                cache: { read: 7, write: 8 }
            },
            cost: 0.0123
        });
    });
    it('records Pi camelCase token usage from the final assistant message', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new PiCamelUsageHarness() });
        await runner.prompt({ sessionID: 'sess-usage-pi-camel-1', runtimeRunID: 'run-usage-pi-camel-1', prompt: 'count pi tokens', agent: 'ops', model: { provider_id: 'test', id: 'fake' } });
        const events = await store.replay('sess-usage-pi-camel-1');
        const ended = events.find((event) => event.type === 'session.step.ended');
        expect(ended).toMatchObject({
            type: 'session.step.ended',
            tokens: {
                input: 21,
                output: 13,
                reasoning: 5,
                cache: { read: 3, write: 2 }
            },
            cost: 0.0044
        });
    });
    it('records Pi totalTokens when detailed token usage is unavailable', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new PiTotalOnlyUsageHarness() });
        await runner.prompt({ sessionID: 'sess-usage-pi-total-only-1', runtimeRunID: 'run-usage-pi-total-only-1', prompt: 'count pi total tokens', agent: 'ops', model: { provider_id: 'test', id: 'fake' } });
        const events = await store.replay('sess-usage-pi-total-only-1');
        const ended = events.find((event) => event.type === 'session.step.ended');
        expect(ended).toMatchObject({
            type: 'session.step.ended',
            tokens: {
                input: 44,
                output: 0,
                reasoning: 0,
                cache: { read: 0, write: 0 }
            },
            cost: 0.0044
        });
    });
    it('does not record a failed step when Pi pauses for tool approval', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new ApprovalHarness() });
        await expect(runner.prompt({ sessionID: 'sess-approval-1', runtimeRunID: 'run-approval-1', prompt: 'deploy', agent: 'ops' })).rejects.toBeInstanceOf(PiToolApprovalRequiredError);
        const events = await store.replay('sess-approval-1');
        expect(events.map((event) => event.type)).toEqual(['session.prompted', 'session.step.started']);
    });
    it('aborts the active harness when an external permission event pauses the run', async () => {
        const store = new InMemoryAgentEventStore();
        const harness = new EventApprovalHarness();
        const runner = new AgentHarnessRunner({ store, createHarness: () => harness });
        const prompt = runner.prompt({ sessionID: 'sess-approval-event', runtimeRunID: 'run-approval-event', prompt: 'deploy', agent: 'ops' });
        await harness.started;
        await runner.pauseForApproval('run-approval-event', {
            approvalID: 'approval:call-event',
            callID: 'call-event',
            toolName: 'deploy.write',
            reason: 'Deploy write requires approval'
        });
        await expect(prompt).rejects.toMatchObject({
            name: 'PiToolApprovalRequiredError',
            approvalID: 'approval:call-event',
            callID: 'call-event'
        });
        const events = await store.replay('sess-approval-event');
        expect(events.map((event) => event.type)).not.toContain('session.step.failed');
    });
    it('invokes an explicit Pi skill and records the skill usage event', async () => {
        const store = new InMemoryAgentEventStore();
        const harness = new SkillInvocationHarness();
        const runner = new AgentHarnessRunner({ store, createHarness: () => harness });
        await runner.prompt({
            sessionID: 'sess-skill-1',
            runtimeRunID: 'run-skill-1',
            prompt: 'build a demo',
            agent: 'designer',
            skillInvocation: { name: 'grill-me', instructions: 'build a demo with grill-me' }
        });
        expect(harness.promptCalled).toBe(false);
        expect(harness.skillCall).toEqual({ name: 'grill-me', instructions: 'build a demo with grill-me' });
        const events = await store.replay('sess-skill-1');
        expect(events.map((event) => event.type)).toEqual([
            'session.prompted',
            'session.step.started',
            'skill.used',
            'session.text.delta',
            'session.text.ended',
            'session.step.ended'
        ]);
        expect(events[2]).toMatchObject({ type: 'skill.used', name: 'grill-me', operation: 'invoked' });
    });
    it('treats Pi failure messages as failed runs instead of completed empty answers', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new FailureMessageHarness() });
        await expect(runner.prompt({ sessionID: 'sess-failure-message-1', runtimeRunID: 'run-failure-message-1', prompt: 'hello', agent: 'ops' }))
            .rejects.toMatchObject({
            source: 'validation',
            code: 'runtime_validation_failed',
            http_status: 400,
            retryable: false,
            message: 'No API key for provider: openrouter'
        });
        const events = await store.replay('sess-failure-message-1');
        expect(events.map((event) => event.type)).toEqual([
            'session.prompted',
            'session.step.started',
            'session.step.failed'
        ]);
        expect(events[2]).toMatchObject({
            type: 'session.step.failed',
            error: {
                type: 'validation',
                code: 'runtime_validation_failed',
                message: 'No API key for provider: openrouter',
                retryable: false,
                http_status: 400,
                source: 'validation'
            }
        });
    });
    it('stores Pi compaction lifecycle events as OpenCode-style session events', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new CompactionHarness() });
        await runner.prompt({ sessionID: 'sess-compact-1', runtimeRunID: 'run-compact-1', prompt: 'continue', agent: 'ops' });
        const events = await store.replay('sess-compact-1');
        expect(events.map((event) => event.type)).toEqual([
            'session.prompted',
            'session.step.started',
            'session.compaction.started',
            'session.compaction.ended',
            'session.text.ended',
            'session.step.ended'
        ]);
        expect(events[2]).toMatchObject({ type: 'session.compaction.started', message_id: 'compact-msg-1' });
        expect(events[3]).toMatchObject({ type: 'session.compaction.ended', summary: 'Earlier context summarized.', recent: 'Current request remains active.' });
    });
    it('aborts the active Pi harness for an exact runtime run', async () => {
        const store = new InMemoryAgentEventStore();
        const harness = new AbortableHarness();
        const runner = new AgentHarnessRunner({ store, createHarness: () => harness });
        void runner.prompt({ sessionID: 'sess-abort-1', runtimeRunID: 'run-abort-1', prompt: 'long run', agent: 'ops' }).catch(() => { });
        await new Promise((resolve) => setTimeout(resolve, 0));
        const aborted = await runner.abort('run-abort-1');
        expect(aborted).toBe(true);
        expect(harness.aborted).toBe(true);
    });
    it('remembers an exact run abort while its harness is still being created', async () => {
        const store = new InMemoryAgentEventStore();
        const harness = new AbortableHarness();
        let releaseHarness;
        const harnessGate = new Promise((resolve) => { releaseHarness = resolve; });
        const runner = new AgentHarnessRunner({ store, createHarness: () => harnessGate });
        const running = runner.prompt({
            sessionID: 'sess-pre-abort-1',
            runtimeRunID: 'run-pre-abort-1',
            prompt: 'cancel before provider starts',
            agent: 'ops'
        });
        await new Promise((resolve) => setTimeout(resolve, 0));
        const aborted = await runner.abort('run-pre-abort-1');
        releaseHarness?.(harness);
        await expect(running).rejects.toThrow('Pi harness stopped with aborted');
        expect(aborted).toBe(true);
        expect(harness.aborted).toBe(true);
    });
});
class ReasoningHarness {
    listeners = [];
    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter((candidate) => candidate !== listener);
        };
    }
    async prompt(text) {
        const partial = (content) => ({ role: 'assistant', content });
        for (const listener of this.listeners) {
            await listener({
                type: 'message_update',
                message: partial([{ type: 'thinking', thinking: '' }]),
                assistantMessageEvent: { type: 'thinking_start', contentIndex: 0, partial: partial([{ type: 'thinking', thinking: '' }]) }
            });
            await listener({
                type: 'message_update',
                message: partial([{ type: 'thinking', thinking: 'Analyzing the request' }]),
                assistantMessageEvent: { type: 'thinking_delta', contentIndex: 0, delta: 'Analyzing the request', partial: partial([{ type: 'thinking', thinking: 'Analyzing the request' }]) }
            });
            await listener({
                type: 'message_update',
                message: partial([{ type: 'thinking', thinking: 'Analyzing the request' }]),
                assistantMessageEvent: { type: 'thinking_end', contentIndex: 0, content: 'Analyzing the request', partial: partial([{ type: 'thinking', thinking: 'Analyzing the request' }]) }
            });
            await listener({
                type: 'message_update',
                message: partial([{ type: 'text', text: '' }]),
                assistantMessageEvent: { type: 'text_start', contentIndex: 1, partial: partial([{ type: 'text', text: '' }]) }
            });
            await listener({
                type: 'message_update',
                message: partial([{ type: 'text', text: `answer:${text}` }]),
                assistantMessageEvent: { type: 'text_delta', contentIndex: 1, delta: `answer:${text}`, partial: partial([{ type: 'text', text: `answer:${text}` }]) }
            });
        }
        return { role: 'assistant', content: [{ type: 'text', text: `answer:${text}` }] };
    }
    async abort() { }
}
describe('AgentHarnessRunner reasoning bridge', () => {
    it('emits session.reasoning.started/delta/ended when Pi harness sends thinking events', async () => {
        const store = new InMemoryAgentEventStore();
        const runner = new AgentHarnessRunner({ store, createHarness: () => new ReasoningHarness() });
        const result = await runner.prompt({ sessionID: 'sess-reasoning-1', runtimeRunID: 'run-reasoning-1', prompt: 'what is 2+2', agent: 'math' });
        const events = await store.replay('sess-reasoning-1');
        expect(events.map((event) => event.type)).toEqual([
            'session.prompted',
            'session.step.started',
            'session.reasoning.started',
            'session.reasoning.delta',
            'session.reasoning.ended',
            'session.text.started',
            'session.text.delta',
            'session.text.ended',
            'session.step.ended'
        ]);
        const projection = await store.project('sess-reasoning-1');
        const parts = projection.partsByMessage[result.assistant_message_id];
        expect(parts).toEqual([
            expect.objectContaining({ type: 'reasoning', text: 'Analyzing the request' }),
            expect.objectContaining({ type: 'text', text: 'answer:what is 2+2' })
        ]);
    });
    it('skips session.text.ended when assistant produces only reasoning and no text', async () => {
        const store = new InMemoryAgentEventStore();
        class ReasoningOnlyHarness {
            listeners = [];
            subscribe(listener) {
                this.listeners.push(listener);
                return () => { this.listeners = this.listeners.filter((c) => c !== listener); };
            }
            async prompt() {
                const partial = (content) => ({ role: 'assistant', content });
                for (const listener of this.listeners) {
                    await listener({
                        type: 'message_update',
                        message: partial([{ type: 'thinking', thinking: 'Silent reasoning only' }]),
                        assistantMessageEvent: { type: 'thinking_start', contentIndex: 0, partial: partial([{ type: 'thinking', thinking: '' }]) }
                    });
                    await listener({
                        type: 'message_update',
                        message: partial([{ type: 'thinking', thinking: 'Silent reasoning only' }]),
                        assistantMessageEvent: { type: 'thinking_end', contentIndex: 0, content: 'Silent reasoning only', partial: partial([{ type: 'thinking', thinking: 'Silent reasoning only' }]) }
                    });
                }
                return { role: 'assistant', content: [{ type: 'thinking', thinking: 'Silent reasoning only' }] };
            }
            async abort() { }
        }
        const runner = new AgentHarnessRunner({ store, createHarness: () => new ReasoningOnlyHarness() });
        const result = await runner.prompt({ sessionID: 'sess-reasoning-only', runtimeRunID: 'run-reasoning-only', prompt: 'think silently', agent: 'math' });
        const events = await store.replay('sess-reasoning-only');
        const types = events.map((event) => event.type);
        expect(types).not.toContain('session.text.ended');
        expect(types).toEqual([
            'session.prompted',
            'session.step.started',
            'session.reasoning.started',
            'session.reasoning.ended',
            'session.step.ended'
        ]);
        const projection = await store.project('sess-reasoning-only');
        const parts = projection.partsByMessage[result.assistant_message_id];
        expect(parts).toEqual([
            expect.objectContaining({ type: 'reasoning', text: 'Silent reasoning only' })
        ]);
    });
});
