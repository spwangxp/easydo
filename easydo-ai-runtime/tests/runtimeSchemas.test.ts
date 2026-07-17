import { describe, expect, it } from 'vitest'
import {
  aiAgentActionSchema,
  approvalRequestSchema,
  contextTagsSchema,
  runtimeActorSchema,
  runtimeEnvelopeSchema
} from '../src/schemas/runtime.js'

describe('runtime schemas', () => {
  it('accepts a valid actor envelope', () => {
    const parsed = runtimeEnvelopeSchema.parse({
      request_id: 'req-1',
      idempotency_key: 'idem-1',
      actor: {
        user_id: 12,
        username: 'demo',
        system_role: 'user',
        workspace_id: 3,
        workspace_role: 'viewer',
        auth_session_id: 'session-1'
      },
      auth: {
        delegated_user_token: 'token',
        server_internal_token: 'internal'
      },
      payload: {}
    })

    expect(parsed.actor.workspace_id).toBe(3)
  })

  it('rejects missing workspace actor data', () => {
    expect(() => runtimeActorSchema.parse({ user_id: 1 })).toThrow()
  })

  it('accepts ordered context tags', () => {
    const parsed = contextTagsSchema.parse(['workspace', 'page-assistant'])

    expect(parsed).toEqual(['workspace', 'page-assistant'])
  })

  it('strictly rejects legacy top-level fields on agent actions', () => {
    const typedAction = {
      id: 1,
      action_id: 'a_w3_000001_000001_1',
      internal_id: 1,
      workspace_id: 3,
      context_tags: ['page-assistant'],
      session_id: 1,
      runtime_run_id: 'r_w3_000001_000001',
      action_kind: 'pipeline.trigger',
      idempotency_key: 'rt:action:r_w3_000001_000001:turn1:part1:easydo_pipeline_trigger',
      source: 'model',
      capability_id: 'easydo_pipeline_trigger',
      input_json: {
        provider_tool_call_id: 'tc_pipeline_1',
        arguments: { pipeline_id: 13 }
      },
      target_json: {
        target_type: 'pipeline',
        target_id: '13'
      },
      policy_json: {
        operation_type: 'execute',
        risk_summary: 'Trigger pipeline 13'
      },
      display_json: {
        title: '触发流水线 13',
        summary: 'Trigger pipeline 13'
      },
      status: 'awaiting_decision',
      requested_by: 12,
      result_json: {},
      created_at: '2026-06-08T00:00:00.000Z',
      updated_at: '2026-06-08T00:00:00.000Z'
    }

    expect(aiAgentActionSchema.parse(typedAction).action_id).toBe(typedAction.action_id)
    expect(aiAgentActionSchema.safeParse({
      ...typedAction,
      tool_name: 'easydo_pipeline_trigger',
      arguments: { pipeline_id: 13 },
      risk_summary: 'legacy top-level field'
    }).success).toBe(false)
  })

  it('defines the runtime approval request UI schema explicitly', () => {
    const parsed = approvalRequestSchema.parse({
      approval_id: 'ap_w3_000001_000001_000001',
      run_id: 'r_w3_000001_000001',
      action_id: 'a_w3_000001_000001_000001',
      tool_name: 'easydo_pipeline_trigger',
      permission_key: 'tool:easydo_pipeline_trigger:execute',
      risk_level: 'write',
      reason: 'Trigger pipeline 13',
      input_preview: { pipeline_id: 13 },
      affected_resources: [{ resource_type: 'pipeline', resource_id: '13', operation_type: 'execute' }],
      options: ['approve_once', 'approve_session', 'reject'],
      steer_supported: true,
      created_at: '2026-06-08T00:00:00.000Z',
      expires_at: '2026-06-08T00:15:00.000Z'
    })

    expect(parsed.options).toEqual(['approve_once', 'approve_session', 'reject'])
    expect(approvalRequestSchema.safeParse({
      ...parsed,
      ui_component: 'runtime must not define UI implementation details'
    }).success).toBe(false)
  })
})
