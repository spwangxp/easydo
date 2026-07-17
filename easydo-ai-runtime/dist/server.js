import { serve } from '@hono/node-server';
import { Hono } from 'hono';
import { loadConfig } from './config.js';
import { runtimeEnvelopeSchema } from './schemas/runtime.js';
import { AgentProfileService, RuntimeDomainError, probeMcpServerConfig } from './services/profileService.js';
import { ActionService, SessionService } from './services/sessionService.js';
import { createMariadbRuntimeStore } from './store/mariadbRuntimeStore.js';
import { createMemoryRuntimeStore } from './store/memoryRuntimeStore.js';
import { InMemoryAgentEventStore } from './agent-runtime/eventStore.js';
import { MariadbAgentEventStore } from './agent-runtime/mariadbEventStore.js';
import { AgentHarnessRunner } from './agent-runtime/harnessRunner.js';
import { createPiHarnessFactory } from './agent-runtime/piHarnessFactory.js';
import { DockerWorkspaceProvider } from './workspace/dockerWorkspaceProvider.js';
import { AgentWorkspaceService } from './workspace/workspaceService.js';
import { WorkspaceRuntimeBinding } from './workspace/workspaceRuntimeBinding.js';
import { classifyRuntimeError } from './services/runtimeErrorClassifier.js';
import { RuntimeMetrics } from './observability/runtimeMetrics.js';
import { ObservedAgentEventStore } from './observability/observedAgentEventStore.js';
import { runtimeLogger } from './observability/runtimeLogger.js';
export function startRunOwnerSweeper(reconcile, intervalMs, onError, logger = runtimeLogger) {
    const delayMs = Math.max(100, Number(intervalMs) || 500);
    let stopped = false;
    let timer;
    const schedule = () => {
        if (stopped)
            return;
        timer = setTimeout(run, delayMs);
        timer.unref?.();
    };
    const run = async () => {
        try {
            await reconcile(true);
        }
        catch (error) {
            if (onError)
                onError(error);
            else {
                const descriptor = classifyRuntimeError(error);
                logger.error({
                    component: 'run-owner-sweeper',
                    operation: 'reconcile',
                    outcome: descriptor.terminal_status,
                    code: descriptor.code,
                    category: descriptor.category
                });
            }
        }
        finally {
            schedule();
        }
    };
    schedule();
    return {
        stop() {
            stopped = true;
            if (timer)
                clearTimeout(timer);
        }
    };
}
function unauthorized() {
    return new Response(JSON.stringify({ error: 'runtime_internal_token_required' }), {
        status: 401,
        headers: { 'content-type': 'application/json' }
    });
}
function ok(data) {
    return { code: 200, data };
}
function sse(event, data) {
    const nestedEvent = data.event && typeof data.event === 'object'
        ? data.event
        : {};
    const eventID = firstString(data.event_id, nestedEvent.event_id);
    const idLine = eventID ? `id: ${eventID}\n` : '';
    return `${idLine}event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}
function firstString(...values) {
    for (const value of values) {
        if (value !== undefined && value !== null && String(value).trim())
            return String(value).trim();
    }
    return '';
}
function parseID(value, name = 'id') {
    const id = Number(value || 0);
    if (!Number.isInteger(id) || id <= 0) {
        throw new RuntimeDomainError(`${name}_invalid`, `${name} is invalid`);
    }
    return id;
}
function parseActionDecision(payload) {
    const raw = String(payload.decision || payload.action || '').trim().toLowerCase();
    if (raw === 'approve_once' || raw === 'approve_session' || raw === 'steer')
        return raw;
    if (raw === 'reject')
        return 'reject';
    throw new RuntimeDomainError('action_decision_invalid', 'Action decision must be approve_once, approve_session, reject, or steer');
}
async function readEnvelope(c) {
    const body = await c.req.json();
    return runtimeEnvelopeSchema.parse(body);
}
function runtimePayload(envelope) {
    return { ...envelope.payload, runtime_request_id: envelope.request_id };
}
function errorBody(error, requestID = '', runtimeRunID = '') {
    if (error instanceof RuntimeDomainError) {
        const descriptor = classifyRuntimeError(error);
        return {
            status: error.status,
            body: {
                ...descriptor,
                message: descriptor.user_message,
                ...error.details,
                ...(requestID ? { request_id: requestID } : {}),
                ...(runtimeRunID ? { runtime_run_id: runtimeRunID } : {})
            }
        };
    }
    if (error && typeof error === 'object' && 'issues' in error) {
        return {
            status: 400,
            body: {
                code: 'runtime_request_invalid',
                category: 'validation',
                message: 'Runtime request is invalid',
                user_message: 'Runtime request is invalid',
                retryable: false,
                http_status: 400,
                source: 'validation',
                terminal_status: 'failed',
                issues: error.issues,
                ...(requestID ? { request_id: requestID } : {})
            }
        };
    }
    const descriptor = classifyRuntimeError(error);
    return {
        status: descriptor.http_status,
        body: {
            ...descriptor,
            message: descriptor.user_message,
            ...(requestID ? { request_id: requestID } : {}),
            ...(runtimeRunID ? { runtime_run_id: runtimeRunID } : {})
        }
    };
}
export async function createSseResponse(factory, requestID = '', options = {}) {
    const startedAt = performance.now();
    const operation = options.operation || 'runtime_stream';
    const logger = options.logger || runtimeLogger;
    let iterator;
    let first;
    try {
        iterator = factory()[Symbol.asyncIterator]();
        first = await iterator.next();
    }
    catch (error) {
        const mapped = errorBody(error, requestID);
        options.metrics?.increment('ai_runtime_requests_total', { operation, outcome: 'failed' });
        options.metrics?.increment('ai_runtime_terminal_total', {
            category: firstString(mapped.body.category, 'internal'),
            code: firstString(mapped.body.code, 'runtime_internal_error')
        });
        options.metrics?.observe('ai_runtime_request_duration_seconds', (performance.now() - startedAt) / 1000, { operation, outcome: 'failed' });
        logger.error({
            component: 'runtime-server',
            operation,
            outcome: 'failed',
            request_id: requestID,
            code: mapped.body.code,
            category: mapped.body.category
        });
        return new Response(JSON.stringify(mapped.body), {
            status: mapped.status,
            headers: {
                'content-type': 'application/json',
                ...(requestID ? { 'x-request-id': requestID } : {})
            }
        });
    }
    const encoder = new TextEncoder();
    options.metrics?.increment('ai_runtime_sse_connections_total', { operation, outcome: 'opened' });
    options.metrics?.addGauge('ai_runtime_sse_connections', 1, { operation });
    const stream = new ReadableStream({
        async start(controller) {
            let terminalErrorSent = false;
            let runtimeRunID = '';
            let outcome = 'completed';
            let terminalCode = '';
            let terminalCategory = '';
            let firstResponseObserved = false;
            const enqueue = (item) => {
                runtimeRunID = firstString(item.data.runtime_run_id, asRecord(item.data.event).runtime_run_id, runtimeRunID);
                if (!firstResponseObserved && isRuntimeFirstResponseEvent(item)) {
                    firstResponseObserved = true;
                    options.metrics?.observe('ai_runtime_first_response_seconds', (performance.now() - startedAt) / 1000, { outcome: 'started' });
                }
                if (item.event === 'error') {
                    if (!terminalErrorSent) {
                        options.metrics?.increment('ai_runtime_terminal_total', {
                            category: firstString(item.data.category, 'internal'),
                            code: firstString(item.data.code, 'runtime_internal_error')
                        });
                    }
                    terminalErrorSent = true;
                    outcome = 'failed';
                    terminalCode = firstString(item.data.code, terminalCode);
                    terminalCategory = firstString(item.data.category, terminalCategory);
                }
                controller.enqueue(encoder.encode(sse(item.event, item.data)));
            };
            try {
                if (!first.done)
                    enqueue(first.value);
                while (true) {
                    const next = await iterator.next();
                    if (next.done)
                        break;
                    enqueue(next.value);
                }
            }
            catch (error) {
                if (!terminalErrorSent) {
                    const mapped = errorBody(error, requestID, runtimeRunID);
                    terminalCode = firstString(mapped.body.code, 'runtime_internal_error');
                    terminalCategory = firstString(mapped.body.category, 'internal');
                    enqueue({ event: 'error', data: mapped.body });
                }
            }
            finally {
                options.metrics?.addGauge('ai_runtime_sse_connections', -1, { operation });
                options.metrics?.increment('ai_runtime_requests_total', { operation, outcome });
                options.metrics?.observe('ai_runtime_request_duration_seconds', (performance.now() - startedAt) / 1000, { operation, outcome });
                const logInput = {
                    component: 'runtime-server',
                    operation,
                    outcome,
                    request_id: requestID,
                    runtime_run_id: runtimeRunID,
                    code: terminalCode,
                    category: terminalCategory
                };
                if (outcome === 'failed')
                    logger.error(logInput);
                else
                    logger.info(logInput);
                controller.close();
            }
        },
        async cancel() {
            await iterator?.return?.();
        }
    });
    return new Response(stream, {
        headers: {
            'content-type': 'text/event-stream',
            'cache-control': options.noTransform ? 'no-cache, no-transform' : 'no-cache',
            connection: 'keep-alive',
            ...(options.noTransform ? { 'x-accel-buffering': 'no' } : {}),
            ...(requestID ? { 'x-request-id': requestID } : {})
        }
    });
}
function isRuntimeFirstResponseEvent(item) {
    const nestedType = firstString(asRecord(item.data.event).type);
    return /(?:reasoning|text|tool)/i.test(nestedType)
        || /^(?:answer_delta|reasoning_delta|text_delta|tool_call|tool_entry|action_entry)$/i.test(item.event);
}
function runtimeLatencyWindow(metrics, windowMs, observedAt) {
    return {
        first_response: metrics.latencySummary('ai_runtime_first_response_seconds', windowMs, observedAt),
        publish_delay: metrics.latencySummary('ai_runtime_publish_delay_seconds', windowMs, observedAt),
        replay_delay: metrics.latencySummary('ai_runtime_replay_delay_seconds', windowMs, observedAt)
    };
}
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
function createAppContext(options) {
    const app = new Hono();
    const logger = options.logger || runtimeLogger;
    const store = options.store || createMemoryRuntimeStore();
    const runtimeInstanceID = options.runtimeInstanceID || process.env.AI_RUNTIME_INSTANCE_ID || process.env.HOSTNAME || 'runtime-local';
    const metrics = new RuntimeMetrics({ instanceID: runtimeInstanceID });
    const profileService = new AgentProfileService(store);
    const agentEventStore = new ObservedAgentEventStore(options.agentEventStore || new InMemoryAgentEventStore(), metrics);
    const agentHarnessRunner = options.agentHarnessRunner || new AgentHarnessRunner({
        store: agentEventStore,
        createHarness: createPiHarnessFactory({}),
        logger
    });
    const sessionService = new SessionService(store, profileService, options.chatModelClient, undefined, {
        easydoServerURL: options.easydoServerURL,
        runtimeInstanceID: options.runtimeInstanceID,
        runLeaseMs: options.runLeaseMs,
        runStreamPollMs: options.runStreamPollMs,
        autoDrainSessionQueue: options.autoDrainSessionQueue,
        logger,
        metrics
    }, undefined, { runner: agentHarnessRunner, eventStore: agentEventStore, workspace: options.workspaceRuntime });
    const actionService = new ActionService(store, sessionService);
    const agentWorkspaceService = options.agentWorkspaceService;
    const requestLogContexts = new WeakMap();
    metrics.setGauge('ai_runtime_runtime_info', 1, { status: 'ready' });
    const requireAgentWorkspaceService = () => {
        if (!agentWorkspaceService) {
            throw new RuntimeDomainError('agent_workspace_service_unavailable', 'Agent workspace service is not configured', 503);
        }
        return agentWorkspaceService;
    };
    app.get('/healthz', (c) => c.json({
        status: 'ok',
        service: 'easydo-ai-runtime',
        replica_mode: 'durable_owner_lease',
        instance_id: runtimeInstanceID
    }));
    app.get('/metrics', async () => {
        const summary = await store.getRuntimeOperationsSummary(new Date().toISOString());
        metrics.setGauge('ai_runtime_runs_active', summary.runs.active, { status: 'active' });
        metrics.setGauge('ai_runtime_runs_active', summary.runs.awaiting_approval, { status: 'awaiting_approval' });
        metrics.setGauge('ai_runtime_runs_active', summary.runs.awaiting_input, { status: 'awaiting_input' });
        metrics.setGauge('ai_runtime_runs_active', summary.runs.stale, { status: 'stale' });
        metrics.setGauge('ai_runtime_runs_active', summary.runs.orphaned, { status: 'orphaned' });
        return new Response(metrics.render(), {
            status: 200,
            headers: { 'content-type': 'text/plain; version=0.0.4; charset=utf-8' }
        });
    });
    app.get('/readyz', async (c) => {
        const persistentStore = store;
        let databaseStatus = typeof persistentStore.ping === 'function' ? 'ok' : 'memory';
        let status = 'ok';
        if (persistentStore.ping) {
            try {
                await persistentStore.ping();
            }
            catch {
                databaseStatus = 'unavailable';
                status = 'unavailable';
            }
        }
        return c.json({
            status,
            service: 'easydo-ai-runtime',
            checks: {
                database: { status: databaseStatus },
                pi_runtime: { status: 'ok' },
                model_provider: { status: 'per_profile_runtime_check' },
                mcp: { status: 'per_resource_runtime_check' }
            }
        }, status === 'ok' ? 200 : 503);
    });
    app.use('/v1/*', async (c, next) => {
        const expectedToken = options.internalToken;
        const actualToken = c.req.header('x-internal-token') || c.req.header('authorization')?.replace(/^Bearer\s+/i, '');
        if (!expectedToken || actualToken !== expectedToken) {
            return unauthorized();
        }
        await next();
    });
    app.use('/v1/*', async (_c, next) => {
        await sessionService.reconcileExpiredRunOwners();
        await next();
    });
    app.use('/v1/*', async (c, next) => {
        const operation = `${c.req.method} ${c.req.path}`;
        let envelope = {};
        try {
            envelope = asRecord(await c.req.raw.clone().json());
        }
        catch {
            // Some internal GET routes do not carry a Runtime envelope.
        }
        const actor = asRecord(envelope.actor);
        const payload = asRecord(envelope.payload);
        const pathRunID = c.req.path.match(/\/runs\/([^/]+)/)?.[1];
        const rawPathSessionID = c.req.path.match(/\/sessions\/([^/]+)/)?.[1] || '';
        const pathSessionID = /^\d+$/.test(rawPathSessionID) ? rawPathSessionID : '';
        const context = {
            component: 'runtime-server',
            operation,
            request_id: firstString(envelope.request_id),
            workspace_id: actor.workspace_id,
            session_id: firstString(payload.session_id, pathSessionID),
            runtime_run_id: firstString(payload.runtime_run_id, pathRunID),
            parent_runtime_run_id: firstString(payload.parent_runtime_run_id)
        };
        requestLogContexts.set(c.req.raw, context);
        logger.info({ ...context, outcome: 'started' });
        await next();
        if (c.res.status < 400) {
            logger.info({ ...context, outcome: 'completed' });
        }
    });
    app.onError((error, c) => {
        const context = requestLogContexts.get(c.req.raw) || {
            component: 'runtime-server',
            operation: `${c.req.method} ${c.req.path}`
        };
        const mapped = errorBody(error, firstString(context.request_id), firstString(context.runtime_run_id));
        logger.error({
            ...context,
            outcome: mapped.body.terminal_status || 'failed',
            code: mapped.body.code,
            category: mapped.body.category
        });
        return new Response(JSON.stringify(mapped.body), {
            status: mapped.status,
            headers: {
                'content-type': 'application/json',
                ...(context.request_id ? { 'x-request-id': String(context.request_id) } : {})
            }
        });
    });
    app.post('/v1/agent-workspaces/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().list(envelope.actor)));
    });
    app.post('/v1/operations/summary', async (c) => {
        await readEnvelope(c);
        const observedAt = Date.now();
        return c.json(ok({
            instance_id: runtimeInstanceID,
            replica: { instance_id: runtimeInstanceID, status: 'ready' },
            ...(await store.getRuntimeOperationsSummary(new Date(observedAt).toISOString())),
            latency_5m: runtimeLatencyWindow(metrics, 5 * 60_000, observedAt),
            latency_1h: runtimeLatencyWindow(metrics, 60 * 60_000, observedAt)
        }));
    });
    app.post('/v1/agent-workspaces', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().ensureDefault(envelope.actor)));
    });
    app.post('/v1/agent-workspaces/:id/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().get(envelope.actor, c.req.param('id'))));
    });
    app.post('/v1/agent-workspaces/:id/connect', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().connect(envelope.actor, c.req.param('id'))));
    });
    app.post('/v1/agent-workspaces/:id/pause', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().pause(envelope.actor, c.req.param('id'))));
    });
    app.post('/v1/agent-workspaces/:id/resume', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().resume(envelope.actor, c.req.param('id'))));
    });
    app.post('/v1/agent-workspaces/:id/recycle', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().recycle(envelope.actor, c.req.param('id'))));
    });
    app.post('/v1/agent-workspaces/:id/audits/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await requireAgentWorkspaceService().listAudits(envelope.actor, c.req.param('id'))));
    });
    app.post('/v1/agent-profiles/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.listProfiles(envelope.actor)));
    });
    app.post('/v1/agent-profiles', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.createProfile(envelope.actor, envelope.payload)));
    });
    app.post('/v1/agent-profiles/:id/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.getProfile(envelope.actor, parseID(c.req.param('id'), 'profile_id'))));
    });
    app.put('/v1/agent-profiles/:id', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.updateProfile(envelope.actor, parseID(c.req.param('id'), 'profile_id'), envelope.payload)));
    });
    app.delete('/v1/agent-profiles/:id', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.deleteProfile(envelope.actor, parseID(c.req.param('id'), 'profile_id'))));
    });
    app.post('/v1/agent-profiles/:id/publish', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.publishProfile(envelope.actor, parseID(c.req.param('id'), 'profile_id'), envelope.payload)));
    });
    app.post('/v1/agent-profiles/:id/validate', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.validateProfile(envelope.actor, parseID(c.req.param('id'), 'profile_id'))));
    });
    app.post('/v1/agent-profiles/:id/versions/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.listVersions(envelope.actor, parseID(c.req.param('id'), 'profile_id'))));
    });
    app.post('/v1/agent-profiles/:id/dependencies/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.getProfileDependencies(envelope.actor, parseID(c.req.param('id'), 'profile_id'))));
    });
    app.post('/v1/agent-resources/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.listResources(envelope.actor)));
    });
    app.post('/v1/agent-resources/:id/versions/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.listResourceVersions(envelope.actor, parseID(c.req.param('id'), 'agent_resource_id'))));
    });
    app.post('/v1/agent-resources/:id/dependencies/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.getResourceDependencies(envelope.actor, parseID(c.req.param('id'), 'agent_resource_id'))));
    });
    app.post('/v1/agent-resources', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.createResource(envelope.actor, envelope.payload)));
    });
    app.put('/v1/agent-resources/:id', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.updateResource(envelope.actor, parseID(c.req.param('id'), 'agent_resource_id'), envelope.payload)));
    });
    app.post('/v1/agent-resources/:id/scan', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.scanResource(envelope.actor, parseID(c.req.param('id'), 'agent_resource_id'))));
    });
    app.post('/v1/agent-resources/mcp/probe', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await probeMcpServerConfig(asRecord(envelope.payload?.config || envelope.payload))));
    });
    app.delete('/v1/agent-resources/:id', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await profileService.deleteResource(envelope.actor, parseID(c.req.param('id'), 'agent_resource_id'))));
    });
    app.post('/v1/sessions/current', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.getCurrentSession(envelope.actor, envelope.payload)));
    });
    app.post('/v1/sessions/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.listSessions(envelope.actor, envelope.payload)));
    });
    app.post('/v1/sessions/:id/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.getSession(envelope.actor, parseID(c.req.param('id'), 'session_id'))));
    });
    app.post('/v1/sessions/:id/entries/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.listEntries(envelope.actor, parseID(c.req.param('id'), 'session_id'))));
    });
    app.post('/v1/sessions/:id/entries', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.createEntryRun(envelope.actor, parseID(c.req.param('id'), 'session_id'), runtimePayload(envelope), envelope.auth)));
    });
    app.post('/v1/sessions/:id/queue/items', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.enqueueSessionQueueItem(envelope.actor, parseID(c.req.param('id'), 'session_id'), envelope.payload)));
    });
    app.post('/v1/sessions/:id/queue/query', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.listSessionQueueItems(envelope.actor, parseID(c.req.param('id'), 'session_id'))));
    });
    app.post('/v1/sessions/:id/queue/items/:queue_item_id/cancel', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.cancelSessionQueueItem(envelope.actor, parseID(c.req.param('id'), 'session_id'), String(c.req.param('queue_item_id') || ''))));
    });
    app.post('/v1/sessions/:id/queue/reorder', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.reorderSessionQueueItems(envelope.actor, parseID(c.req.param('id'), 'session_id'), envelope.payload)));
    });
    app.post('/v1/sessions/:id/entries/stream', async (c) => {
        const envelope = await readEnvelope(c);
        const sessionID = parseID(c.req.param('id'), 'session_id');
        const payload = runtimePayload(envelope);
        await sessionService.assertSessionCanAcceptEntry(envelope.actor, sessionID, payload);
        return createSseResponse(() => sessionService.streamEntryRun(envelope.actor, sessionID, payload, envelope.auth), envelope.request_id, { metrics, operation: 'entry_stream', logger });
    });
    app.post('/v1/sessions/:id/cancel', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.cancelSessionRun(envelope.actor, parseID(c.req.param('id'), 'session_id'), envelope.payload)));
    });
    app.post('/v1/sessions/:id/archive', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.archiveSession(envelope.actor, parseID(c.req.param('id'), 'session_id'))));
    });
    app.put('/v1/sessions/:id/model', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.updateSessionModelOverride(envelope.actor, parseID(c.req.param('id'), 'session_id'), envelope.payload)));
    });
    app.post('/v1/sessions/:id/continue/stream', async (c) => {
        const envelope = await readEnvelope(c);
        const sessionID = parseID(c.req.param('id'), 'session_id');
        await sessionService.assertSessionCanAcceptEntry(envelope.actor, sessionID, {
            ...envelope.payload,
            content: envelope.payload.content || '继续'
        });
        return createSseResponse(async function* () {
            const continuation = await sessionService.continueSessionStream(envelope.actor, sessionID, envelope.payload, envelope.auth);
            yield* continuation;
        }, envelope.request_id, { metrics, operation: 'continue_stream', logger });
    });
    app.post('/v1/actions/:id/decision', async (c) => {
        const envelope = await readEnvelope(c);
        const decision = parseActionDecision(envelope.payload);
        return c.json(ok(await actionService.decide(envelope.actor, String(c.req.param('id') || ''), decision, envelope.payload)));
    });
    app.post('/v1/actions/:id/decision/stream', async (c) => {
        const envelope = await readEnvelope(c);
        const decision = parseActionDecision(envelope.payload);
        return createSseResponse(() => actionService.decideAndContinueStream(envelope.actor, String(c.req.param('id') || ''), decision, envelope.payload, envelope.auth), envelope.request_id, { metrics, operation: 'action_decision_stream', logger });
    });
    app.post('/v1/runs/:runtime_run_id/pi-approval/decision', async (c) => {
        const envelope = await readEnvelope(c);
        return c.json(ok(await sessionService.decidePiApproval(envelope.actor, String(c.req.param('runtime_run_id') || ''), envelope.payload, envelope.auth)));
    });
    app.post('/v1/runs/:runtime_run_id/pi-approval/decision/stream', async (c) => {
        const envelope = await readEnvelope(c);
        const runtimeRunID = String(c.req.param('runtime_run_id') || '');
        return createSseResponse(() => sessionService.streamPiApproval(envelope.actor, runtimeRunID, envelope.payload, envelope.auth), envelope.request_id, { metrics, operation: 'pi_approval_stream', logger });
    });
    app.get('/v1/runs/:runtime_run_id/events', async (c) => {
        const runtimeRunID = String(c.req.param('runtime_run_id') || '').trim();
        const afterEventID = String(c.req.query('after_event_id') || '').trim();
        return c.json(ok(await sessionService.listRunEventsByRuntimeID(runtimeRunID, afterEventID)));
    });
    app.post('/v1/runs/:runtime_run_id/events/query', async (c) => {
        const envelope = await readEnvelope(c);
        const runtimeRunID = String(c.req.param('runtime_run_id') || '').trim();
        const afterEventID = firstString(envelope.payload.after_event_id);
        return c.json(ok(await sessionService.listRunEventsScoped(envelope.actor, runtimeRunID, envelope.payload, afterEventID)));
    });
    app.post('/v1/runs/:runtime_run_id/events/stream', async (c) => {
        const envelope = await readEnvelope(c);
        const runtimeRunID = String(c.req.param('runtime_run_id') || '').trim();
        if (firstString(envelope.payload.after_event_id)) {
            metrics.increment('ai_runtime_reconnects_total', { operation: 'run_event_stream', outcome: 'started' });
        }
        return createSseResponse(() => sessionService.streamRunEvents(envelope.actor, runtimeRunID, envelope.payload), envelope.request_id, { noTransform: true, metrics, operation: 'run_event_stream', logger });
    });
    app.post('/v1/artifacts/:artifact_id/query', async (c) => {
        const envelope = await readEnvelope(c);
        const artifactID = String(c.req.param('artifact_id') || '').trim();
        return c.json(ok(await sessionService.getRuntimeArtifact(envelope.actor, artifactID, envelope.payload)));
    });
    app.post('/v1/runtime-events', async (c) => {
        const envelope = await readEnvelope(c);
        const event = envelope.payload.event;
        if (!event || typeof event !== 'object' || !('type' in event) || !('session_id' in event)) {
            throw new RuntimeDomainError('runtime_event_invalid', 'Runtime event is invalid');
        }
        return c.json(ok(await agentEventStore.append(event)));
    });
    app.post('/v1/runtime-events/query', async (c) => {
        const envelope = await readEnvelope(c);
        const sessionID = firstString(envelope.payload.session_id);
        if (!sessionID)
            throw new RuntimeDomainError('session_id_required', 'session_id is required');
        const afterSeq = Number(envelope.payload.after_seq || 0);
        return c.json(ok({ events: await agentEventStore.replay(sessionID, Number.isFinite(afterSeq) ? afterSeq : 0) }));
    });
    app.post('/v1/runtime-events/projection', async (c) => {
        const envelope = await readEnvelope(c);
        const sessionID = firstString(envelope.payload.session_id);
        if (!sessionID)
            throw new RuntimeDomainError('session_id_required', 'session_id is required');
        return c.json(ok(await agentEventStore.project(sessionID)));
    });
    app.post('/v1/runtime-harness/prompt', async (c) => {
        const envelope = await readEnvelope(c);
        const sessionID = firstString(envelope.payload.session_id);
        const runtimeRunID = firstString(envelope.payload.runtime_run_id);
        const prompt = firstString(envelope.payload.prompt, envelope.payload.content);
        if (!sessionID)
            throw new RuntimeDomainError('session_id_required', 'session_id is required');
        if (!runtimeRunID)
            throw new RuntimeDomainError('runtime_run_id_required', 'runtime_run_id is required');
        if (!prompt)
            throw new RuntimeDomainError('prompt_required', 'prompt is required');
        const result = await agentHarnessRunner.prompt({
            sessionID,
            runtimeRunID,
            requestID: envelope.request_id,
            workspaceID: envelope.actor.workspace_id,
            parentRuntimeRunID: firstString(envelope.payload.parent_runtime_run_id),
            prompt,
            agent: firstString(envelope.payload.agent) || undefined,
            model: typeof envelope.payload.model === 'object' && envelope.payload.model !== null
                ? envelope.payload.model
                : undefined
        });
        return c.json(ok({ ...result, projection: await agentEventStore.project(sessionID) }));
    });
    app.get('/v1/artifacts/:artifact_id', async (c) => {
        const artifactID = String(c.req.param('artifact_id') || '').trim();
        return c.json(ok(await sessionService.getRuntimeArtifactByID(artifactID)));
    });
    return { app, sessionService };
}
export function createApp(options) {
    return createAppContext(options).app;
}
export function start(config = loadConfig()) {
    const mariadbOptions = {
        uri: config.database.uri,
        host: config.database.host,
        port: config.database.port,
        user: config.database.user,
        password: config.database.password,
        database: config.database.name
    };
    const store = config.database.enabled
        ? createMariadbRuntimeStore(mariadbOptions)
        : createMemoryRuntimeStore();
    const agentEventStore = config.database.enabled
        ? MariadbAgentEventStore.create(mariadbOptions)
        : new InMemoryAgentEventStore();
    const workspaceProvider = new DockerWorkspaceProvider(undefined, { dockerHost: config.workspace.dockerHost });
    const workspaceService = new AgentWorkspaceService(store, workspaceProvider, {
        defaultImage: config.workspace.image,
        defaultProviderEndpoint: config.workspace.dockerHost
    });
    const workspaceRuntime = new WorkspaceRuntimeBinding(store, workspaceService, workspaceProvider, {
        instanceID: config.instanceID,
        leaseMs: config.runLeaseMs
    });
    const { app, sessionService } = createAppContext({
        internalToken: config.internalToken,
        store,
        agentEventStore,
        easydoServerURL: config.easydoServerURL,
        runtimeInstanceID: config.instanceID,
        runLeaseMs: config.runLeaseMs,
        runStreamPollMs: config.runStreamPollMs,
        workspaceRuntime,
        agentWorkspaceService: workspaceService
    });
    const sweeper = startRunOwnerSweeper((force) => sessionService.reconcileExpiredRunOwners(force), config.runStreamPollMs);
    const server = serve({
        fetch: app.fetch,
        port: config.port
    });
    server.once('close', () => sweeper.stop());
    return server;
}
if (process.env.NODE_ENV !== 'test' && process.argv[1]?.endsWith('server.js')) {
    start();
}
