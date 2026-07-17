import { describe, expect, it } from 'vitest'
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js'
import { OrchestrationService } from './orchestrationService.js'

describe('OrchestrationService', () => {
  it('assembles context, dispatches parallel tasks, reviews failures into follow-ups, and finalizes once', async () => {
    const store = createMemoryRuntimeStore()
    const service = new OrchestrationService(store, () => '2026-07-15T03:00:00.000Z')
    const events: Array<{ type: string, payload: Record<string, unknown> }> = []
    let attempts = 0

    const plan = await service.assembleAndRun({
      workspace_id: 1,
      parent_runtime_run_id: 'r_parent_1',
      parent_session_id: 11,
      parent_summary: 'Investigate deploy and auth failures',
      tasks: [
        { task_id: 't-deploy', objective: 'Check deploy status', assigned_profile_id: 2, mode: 'read_only' },
        { task_id: 't-auth', objective: 'Check auth errors', assigned_profile_id: 3, mode: 'read_only' },
        { task_id: 't-logs', objective: 'Scan recent logs', assigned_profile_id: 4, mode: 'read_only' }
      ]
    }, {
      owner: 'runtime-a',
      emit: async (type, payload) => { events.push({ type, payload }) },
      executeTask: async (task) => {
        attempts += 1
        if (task.task_id === 't-auth' && task.attempt <= 1 && task.task_kind === 'worker') {
          return {
            status: 'failed',
            error_code: 'child_failed',
            error_msg: 'auth probe failed',
            child_runtime_run_id: `child_${task.task_id}_${task.attempt}`,
            child_run_link_id: `link_${task.task_id}_${task.attempt}`
          }
        }
        return {
          status: 'completed',
          result_summary: `${task.task_id} ok`,
          child_runtime_run_id: `child_${task.task_id}_${task.attempt}`,
          child_run_link_id: `link_${task.task_id}_${task.attempt}`,
          artifact_refs: [{ artifact_id: `art_${task.task_id}` }]
        }
      }
    })

    expect(plan.status).toBe('completed')
    expect(plan.phase).toBe('completed')
    expect(plan.final_summary).toContain('completed=')
    const tasks = await store.listOrchestrationTasks(1, plan.orchestration_id)
    expect(tasks.length).toBeGreaterThanOrEqual(4)
    expect(tasks.some((task) => task.task_kind === 'followup')).toBe(true)
    expect(tasks.filter((task) => task.status === 'completed').length).toBeGreaterThanOrEqual(3)
    expect(events.map((event) => event.type)).toEqual(expect.arrayContaining([
      'orchestration.context.assembled',
      'orchestration.task.dispatched',
      'orchestration.task.completed',
      'orchestration.task.failed',
      'orchestration.synthesis.started',
      'orchestration.review.started',
      'orchestration.followup.dispatched',
      'orchestration.completed'
    ]))
    expect(events.filter((event) => event.type === 'orchestration.completed')).toHaveLength(1)
    expect(attempts).toBeGreaterThanOrEqual(4)

    const again = await service.drive(plan.orchestration_id, {
      owner: 'runtime-b',
      executeTask: async () => {
        throw new Error('should not re-execute completed orchestration')
      }
    })
    expect(again.status).toBe('completed')
    expect(events.filter((event) => event.type === 'orchestration.completed')).toHaveLength(1)
  })

  it('keeps dependent tasks blocked until prerequisites complete', async () => {
    const store = createMemoryRuntimeStore()
    const service = new OrchestrationService(store, () => '2026-07-15T03:10:00.000Z')
    const executed: string[] = []

    await service.assembleContext({
      workspace_id: 1,
      parent_runtime_run_id: 'r_parent_dep',
      parent_session_id: 12,
      tasks: [
        { task_id: 'root', objective: 'root', assigned_profile_id: 1 },
        { task_id: 'child', objective: 'child', assigned_profile_id: 2, dependency_ids: ['root'] }
      ]
    })

    await service.drive('orch_1_r_parent_dep', {
      executeTask: async (task) => {
        executed.push(task.task_id)
        return {
          status: 'completed',
          result_summary: `${task.task_id} done`,
          child_runtime_run_id: `child_${task.task_id}`,
          child_run_link_id: `link_${task.task_id}`
        }
      }
    })

    expect(executed[0]).toBe('root')
    expect(executed).toContain('child')
    expect(executed.indexOf('root')).toBeLessThan(executed.indexOf('child'))
  })
})
