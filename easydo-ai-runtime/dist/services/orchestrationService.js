import { createHash } from 'node:crypto';
const TERMINAL_TASK_STATUSES = new Set([
    'completed',
    'failed',
    'cancelled'
]);
export class OrchestrationService {
    store;
    now;
    constructor(store, now = () => new Date().toISOString()) {
        this.store = store;
        this.now = now;
    }
    async assembleAndRun(input, options) {
        const plan = await this.assembleContext(input, options.emit);
        return this.drive(plan.orchestration_id, options);
    }
    async assembleContext(input, emit) {
        const existing = await this.store.getOrchestrationPlanByParentRun(input.workspace_id, input.parent_runtime_run_id);
        if (existing && existing.status !== 'failed') {
            return existing;
        }
        const timestamp = this.now();
        const orchestrationID = orchestrationBusinessID(input.workspace_id, input.parent_runtime_run_id);
        const content = {
            parent_summary: String(input.parent_summary || ''),
            task_objectives: input.tasks.map((task) => task.objective),
            assembled_at: timestamp
        };
        const digest = sha256JSON(content);
        const contextPack = {
            context_pack_id: `${orchestrationID}:pack:1`,
            workspace_id: input.workspace_id,
            orchestration_id: orchestrationID,
            parent_runtime_run_id: input.parent_runtime_run_id,
            source: String(input.source || 'parent_run'),
            trim_rules: input.trim_rules || { max_tasks: 16, summary_only: true },
            resource_versions: input.resource_versions || {},
            token_estimate: Math.max(0, Number(input.token_estimate) || estimateTokens(JSON.stringify(content))),
            content,
            digest,
            created_at: timestamp
        };
        await this.store.saveOrchestrationContextPack(contextPack);
        const plan = {
            orchestration_id: orchestrationID,
            workspace_id: input.workspace_id,
            parent_runtime_run_id: input.parent_runtime_run_id,
            parent_session_id: input.parent_session_id,
            status: 'running',
            phase: 'assemble_context',
            context_pack_digest: digest,
            synthesis_summary: '',
            final_summary: '',
            claim_owner: input.owner || null,
            claim_epoch: 1,
            claim_expires_at: leaseExpiry(timestamp),
            created_at: timestamp,
            updated_at: timestamp
        };
        await this.store.saveOrchestrationPlan(plan);
        let seq = 0;
        for (const spec of input.tasks) {
            seq += 1;
            const taskID = firstString(spec.task_id, `${orchestrationID}:task:${seq}`);
            const task = {
                task_id: taskID,
                workspace_id: input.workspace_id,
                orchestration_id: orchestrationID,
                parent_runtime_run_id: input.parent_runtime_run_id,
                task_seq: seq,
                task_kind: spec.task_kind || 'worker',
                objective: String(spec.objective || '').trim(),
                assigned_profile_id: Number(spec.assigned_profile_id || 0),
                assigned_profile_version_id: Number(spec.assigned_profile_version_id || 0),
                assigned_subagent_name: String(spec.assigned_subagent_name || ''),
                mode: String(spec.mode || 'read_only'),
                dependency_ids: Array.isArray(spec.dependency_ids) ? spec.dependency_ids.map(String) : [],
                context_pack_digest: digest,
                status: 'pending',
                claim_owner: null,
                claim_epoch: 0,
                attempt: 0,
                result_summary: '',
                artifact_refs: [],
                created_at: timestamp,
                updated_at: timestamp
            };
            await this.store.saveOrchestrationTask(task);
        }
        await emit?.('orchestration.context.assembled', {
            orchestration_id: orchestrationID,
            parent_runtime_run_id: input.parent_runtime_run_id,
            context_pack_digest: digest,
            token_estimate: contextPack.token_estimate,
            task_count: input.tasks.length,
            phase: 'assemble_context'
        });
        await this.transitionPhase(orchestrationID, 'dispatch_tasks', emit);
        return this.requirePlan(input.workspace_id, orchestrationID);
    }
    async drive(orchestrationID, options) {
        const plan = await this.store.getOrchestrationPlan(0, orchestrationID)
            || await this.findPlanByID(orchestrationID);
        if (!plan)
            throw new Error(`orchestration plan not found: ${orchestrationID}`);
        if (plan.status === 'completed')
            return plan;
        const owner = options.owner || plan.claim_owner || 'runtime';
        await this.store.claimOrchestrationPlan({
            workspace_id: plan.workspace_id,
            orchestration_id: plan.orchestration_id,
            owner,
            claim_expires_at: leaseExpiry(this.now())
        });
        await this.runDispatchCollectLoop(plan, options);
        let current = await this.requirePlan(plan.workspace_id, plan.orchestration_id);
        if (!allWorkerTasksTerminal(await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id))) {
            return current;
        }
        await this.synthesizeResults(current, options.emit);
        const followups = await this.reviewAndMaybeCreateFollowups(current, options.emit);
        if (followups.length > 0) {
            await this.transitionPhase(plan.orchestration_id, 'dispatch_followup_context', options.emit);
            current = await this.requirePlan(plan.workspace_id, plan.orchestration_id);
            await this.runDispatchCollectLoop(current, options);
            current = await this.requirePlan(plan.workspace_id, plan.orchestration_id);
            if (!allWorkerTasksTerminal(await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id))) {
                return current;
            }
            await this.synthesizeResults(current, options.emit);
        }
        return this.finalizeSummary(await this.requirePlan(plan.workspace_id, plan.orchestration_id), options.emit);
    }
    async runDispatchCollectLoop(plan, options) {
        for (let wave = 0; wave < 16; wave += 1) {
            const before = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
            const pendingBefore = before.filter((task) => task.status === 'pending' || task.status === 'queued').length;
            await this.dispatchReadyTasks(await this.requirePlan(plan.workspace_id, plan.orchestration_id), options);
            await this.collectStatus(await this.requirePlan(plan.workspace_id, plan.orchestration_id), options.emit);
            const after = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
            const pendingAfter = after.filter((task) => task.status === 'pending' || task.status === 'queued').length;
            const running = after.some((task) => ['claimed', 'dispatched', 'running', 'blocked_approval'].includes(task.status));
            if (pendingAfter === 0 || (pendingAfter === pendingBefore && !running && wave > 0))
                break;
            if (pendingAfter > 0 && pendingAfter < pendingBefore)
                continue;
            if (pendingAfter > 0 && wave === 0)
                continue;
            if (pendingAfter === 0)
                break;
        }
    }
    async dispatchReadyTasks(plan, options) {
        const tasks = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
        const byID = new Map(tasks.map((task) => [task.task_id, task]));
        const ready = tasks.filter((task) => {
            if (task.status !== 'pending' && task.status !== 'queued')
                return false;
            if (task.dependency_ids.length === 0)
                return true;
            return task.dependency_ids.every((dependencyID) => {
                const dependency = byID.get(dependencyID);
                return Boolean(dependency && dependency.status === 'completed');
            });
        });
        const owner = options.owner || plan.claim_owner || 'runtime';
        await Promise.all(ready.map(async (task) => {
            const claimed = await this.store.claimOrchestrationTask({
                workspace_id: plan.workspace_id,
                task_id: task.task_id,
                owner,
                claim_expires_at: leaseExpiry(this.now())
            });
            if (!claimed)
                return;
            const dispatched = await this.store.saveOrchestrationTask({
                ...claimed,
                status: 'dispatched',
                attempt: claimed.attempt + 1,
                updated_at: this.now()
            });
            await options.emit?.('orchestration.task.dispatched', taskEventPayload(dispatched, plan));
            await options.emit?.('orchestration.task.status_changed', {
                ...taskEventPayload(dispatched, plan),
                previous_status: claimed.status,
                status: 'dispatched'
            });
            try {
                const running = await this.store.saveOrchestrationTask({
                    ...dispatched,
                    status: 'running',
                    updated_at: this.now()
                });
                await options.emit?.('orchestration.task.status_changed', {
                    ...taskEventPayload(running, plan),
                    previous_status: 'dispatched',
                    status: 'running'
                });
                const result = await options.executeTask(running, plan);
                const nextStatus = result.status === 'completed' ? 'completed'
                    : result.status === 'blocked_approval' ? 'blocked_approval'
                        : result.status === 'cancelled' ? 'cancelled'
                            : result.status === 'running' ? 'running'
                                : 'failed';
                const completed = await this.store.saveOrchestrationTask({
                    ...running,
                    status: nextStatus,
                    child_run_link_id: result.child_run_link_id,
                    child_runtime_run_id: result.child_runtime_run_id,
                    result_summary: String(result.result_summary || ''),
                    artifact_refs: Array.isArray(result.artifact_refs) ? result.artifact_refs : [],
                    error_code: result.error_code,
                    error_msg: result.error_msg,
                    completed_at: TERMINAL_TASK_STATUSES.has(nextStatus) ? this.now() : undefined,
                    updated_at: this.now()
                });
                if (nextStatus === 'completed') {
                    await options.emit?.('orchestration.task.completed', taskEventPayload(completed, plan));
                }
                else if (nextStatus === 'failed' || nextStatus === 'cancelled') {
                    await options.emit?.('orchestration.task.failed', {
                        ...taskEventPayload(completed, plan),
                        error_code: completed.error_code,
                        error_msg: completed.error_msg
                    });
                }
                else {
                    await options.emit?.('orchestration.task.status_changed', {
                        ...taskEventPayload(completed, plan),
                        status: nextStatus
                    });
                }
            }
            catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                const failed = await this.store.saveOrchestrationTask({
                    ...dispatched,
                    status: 'failed',
                    error_code: 'orchestration_task_execution_failed',
                    error_msg: message,
                    completed_at: this.now(),
                    updated_at: this.now()
                });
                await options.emit?.('orchestration.task.failed', {
                    ...taskEventPayload(failed, plan),
                    error_code: failed.error_code,
                    error_msg: failed.error_msg
                });
            }
        }));
        return this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
    }
    async collectStatus(plan, emit) {
        await this.transitionPhase(plan.orchestration_id, 'collect_status', emit);
        const tasks = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
        for (const task of tasks) {
            if (!task.child_runtime_run_id || TERMINAL_TASK_STATUSES.has(task.status))
                continue;
            const childRun = await this.store.getRun(plan.workspace_id, task.child_runtime_run_id);
            if (!childRun)
                continue;
            if (!isTerminalRunStatus(childRun.status))
                continue;
            const nextStatus = childRun.status === 'completed' ? 'completed'
                : childRun.status === 'cancelled' ? 'cancelled'
                    : 'failed';
            if (task.status === nextStatus)
                continue;
            const updated = await this.store.saveOrchestrationTask({
                ...task,
                status: nextStatus,
                result_summary: task.result_summary || firstString(asRecord(childRun.result).summary, childRun.status),
                completed_at: this.now(),
                updated_at: this.now()
            });
            await emit?.('orchestration.task.status_changed', {
                ...taskEventPayload(updated, plan),
                previous_status: task.status,
                status: nextStatus
            });
            if (nextStatus === 'completed') {
                await emit?.('orchestration.task.completed', taskEventPayload(updated, plan));
            }
            else {
                await emit?.('orchestration.task.failed', {
                    ...taskEventPayload(updated, plan),
                    error_code: updated.error_code || `child_${childRun.status}`,
                    error_msg: updated.error_msg || `Child run ${childRun.status}`
                });
            }
        }
        return this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
    }
    async synthesizeResults(plan, emit) {
        await this.transitionPhase(plan.orchestration_id, 'synthesize_results', emit);
        await emit?.('orchestration.synthesis.started', {
            orchestration_id: plan.orchestration_id,
            parent_runtime_run_id: plan.parent_runtime_run_id,
            phase: 'synthesize_results'
        });
        const tasks = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
        const workerTasks = tasks.filter((task) => task.task_kind === 'worker' || task.task_kind === 'followup');
        const completed = workerTasks.filter((task) => task.status === 'completed');
        const failed = workerTasks.filter((task) => task.status === 'failed' || task.status === 'cancelled');
        const summary = [
            `Orchestration ${plan.orchestration_id}`,
            `completed=${completed.length}`,
            `failed=${failed.length}`,
            ...completed.map((task) => `- [${task.task_id}] ${task.result_summary || 'ok'}`),
            ...failed.map((task) => `- [${task.task_id}] FAILED ${task.error_msg || task.status}`)
        ].join('\n');
        return this.store.saveOrchestrationPlan({
            ...plan,
            synthesis_summary: summary,
            updated_at: this.now()
        });
    }
    async reviewAndMaybeCreateFollowups(plan, emit) {
        await this.transitionPhase(plan.orchestration_id, 'review_task_outputs', emit);
        await emit?.('orchestration.review.started', {
            orchestration_id: plan.orchestration_id,
            parent_runtime_run_id: plan.parent_runtime_run_id,
            phase: 'review_task_outputs'
        });
        const tasks = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
        const failedWorkers = tasks.filter((task) => (task.task_kind === 'worker') && (task.status === 'failed' || task.status === 'cancelled'));
        const existingFollowups = tasks.filter((task) => task.task_kind === 'followup');
        if (failedWorkers.length === 0 || existingFollowups.length > 0)
            return [];
        const created = [];
        let nextSeq = Math.max(0, ...tasks.map((task) => task.task_seq)) + 1;
        for (const failed of failedWorkers) {
            const followup = {
                task_id: `${plan.orchestration_id}:followup:${failed.task_seq}`,
                workspace_id: plan.workspace_id,
                orchestration_id: plan.orchestration_id,
                parent_runtime_run_id: plan.parent_runtime_run_id,
                task_seq: nextSeq,
                task_kind: 'followup',
                objective: `Follow-up for failed task ${failed.task_id}: re-check and complete objective: ${failed.objective}`,
                assigned_profile_id: failed.assigned_profile_id,
                assigned_profile_version_id: failed.assigned_profile_version_id,
                assigned_subagent_name: failed.assigned_subagent_name,
                mode: failed.mode,
                dependency_ids: [],
                context_pack_digest: plan.context_pack_digest,
                status: 'pending',
                claim_owner: null,
                claim_epoch: 0,
                attempt: 0,
                result_summary: '',
                artifact_refs: [],
                created_at: this.now(),
                updated_at: this.now()
            };
            nextSeq += 1;
            created.push(await this.store.saveOrchestrationTask(followup));
            await emit?.('orchestration.followup.dispatched', taskEventPayload(followup, plan));
        }
        return created;
    }
    async finalizeSummary(plan, emit) {
        await this.transitionPhase(plan.orchestration_id, 'finalize_summary', emit);
        const tasks = await this.store.listOrchestrationTasks(plan.workspace_id, plan.orchestration_id);
        const failed = tasks.filter((task) => task.status === 'failed' || task.status === 'cancelled');
        const completed = tasks.filter((task) => task.status === 'completed');
        const finalSummary = plan.synthesis_summary
            || `Completed ${completed.length} task(s); failed ${failed.length} task(s).`;
        const finalized = await this.store.saveOrchestrationPlan({
            ...plan,
            status: failed.length > 0 && completed.length === 0 ? 'failed' : 'completed',
            phase: 'completed',
            final_summary: finalSummary,
            error_code: failed.length > 0 && completed.length === 0 ? 'orchestration_all_tasks_failed' : undefined,
            error_msg: failed.length > 0 && completed.length === 0 ? 'All orchestration tasks failed' : undefined,
            completed_at: this.now(),
            updated_at: this.now()
        });
        await emit?.('orchestration.completed', {
            orchestration_id: finalized.orchestration_id,
            parent_runtime_run_id: finalized.parent_runtime_run_id,
            status: finalized.status,
            phase: finalized.phase,
            final_summary: finalized.final_summary,
            completed_task_count: completed.length,
            failed_task_count: failed.length
        });
        return finalized;
    }
    async transitionPhase(orchestrationID, phase, emit) {
        const plan = await this.findPlanByID(orchestrationID);
        if (!plan)
            return;
        if (plan.phase === phase || plan.phase === 'completed')
            return;
        const updated = await this.store.saveOrchestrationPlan({
            ...plan,
            phase,
            updated_at: this.now()
        });
        await emit?.('orchestration.task.status_changed', {
            orchestration_id: updated.orchestration_id,
            parent_runtime_run_id: updated.parent_runtime_run_id,
            phase: updated.phase,
            status: updated.status
        });
    }
    async requirePlan(workspaceID, orchestrationID) {
        const plan = await this.store.getOrchestrationPlan(workspaceID, orchestrationID)
            || await this.findPlanByID(orchestrationID);
        if (!plan)
            throw new Error(`orchestration plan not found: ${orchestrationID}`);
        return plan;
    }
    async findPlanByID(orchestrationID) {
        return this.store.getOrchestrationPlan(0, orchestrationID);
    }
}
function taskEventPayload(task, plan) {
    return {
        orchestration_id: plan.orchestration_id,
        parent_runtime_run_id: plan.parent_runtime_run_id,
        task_id: task.task_id,
        task_seq: task.task_seq,
        task_kind: task.task_kind,
        status: task.status,
        mode: task.mode,
        assigned_profile_id: task.assigned_profile_id || undefined,
        assigned_subagent_name: task.assigned_subagent_name || undefined,
        child_run_link_id: task.child_run_link_id,
        child_runtime_run_id: task.child_runtime_run_id,
        result_summary: task.result_summary || undefined,
        attempt: task.attempt,
        phase: plan.phase
    };
}
function allWorkerTasksTerminal(tasks) {
    const relevant = tasks.filter((task) => task.task_kind === 'worker' || task.task_kind === 'followup' || task.task_kind === 'review');
    return relevant.length > 0 && relevant.every((task) => TERMINAL_TASK_STATUSES.has(task.status));
}
function isTerminalRunStatus(status) {
    return ['completed', 'failed', 'cancelled', 'timeout', 'interrupted'].includes(status);
}
function orchestrationBusinessID(workspaceID, parentRuntimeRunID) {
    return `orch_${workspaceID}_${parentRuntimeRunID}`;
}
function sha256JSON(value) {
    return `sha256:${createHash('sha256').update(JSON.stringify(value)).digest('hex')}`;
}
function estimateTokens(text) {
    return Math.max(1, Math.ceil(text.length / 4));
}
function leaseExpiry(nowISO) {
    return new Date(Date.parse(nowISO) + 60_000).toISOString();
}
function firstString(...values) {
    for (const value of values) {
        const text = value === undefined || value === null ? '' : String(value).trim();
        if (text)
            return text;
    }
    return '';
}
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
