import test from 'node:test'
import assert from 'node:assert/strict'
import { buildRuntimeProcessItems } from './runtimeProcessEvents.js'

function event(type, eventSeq, payload = {}) {
  return {
    type,
    event_seq: eventSeq,
    payload: {
      event_id: `event-${eventSeq}`,
      event_seq: eventSeq,
      ...payload
    }
  }
}

test('runtime process keeps context approval tool and subagent lifecycle events in event order', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      event('context.build_started', 1, { profile_id: 3 }),
      event('context.build_completed', 2, { profile_id: 3 }),
      event('permission.asked', 3, { call_id: 'call-1', tool_name: 'easydo_write' }),
      event('permission.resolved', 4, { call_id: 'call-1', tool_name: 'easydo_write', decision: 'approve_once' }),
      event('session.tool.called', 5, { call_id: 'call-1', tool_name: 'easydo_write' }),
      event('session.tool.progress', 6, { call_id: 'call-1', tool_name: 'easydo_write', message: 'writing' }),
      event('session.tool.success', 7, { call_id: 'call-1', tool_name: 'easydo_write', result: { ok: true } }),
      event('subagent.started', 8, { child_runtime_run_id: 'child-1', agent_name: 'reviewer' }),
      event('subagent.progress', 9, { child_runtime_run_id: 'child-1', agent_name: 'reviewer', progress: 'checking' }),
      event('subagent.completed', 10, { child_runtime_run_id: 'child-1', agent_name: 'reviewer', result: 'done' })
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'context.build_started',
    'context.build_completed',
    'permission.asked',
    'permission.resolved',
    'session.tool.called',
    'session.tool.progress',
    'session.tool.success',
    'subagent.started',
    'subagent.progress',
    'subagent.completed'
  ])
})

test('runtime process keeps repeated action lifecycle events as separate rows', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      event('action.decision_required', 1, { action_id: 'action-1', capability_id: 'write' }),
      event('action.approved', 2, { action_id: 'action-1', capability_id: 'write', decision: 'approved' }),
      event('action.execution_started', 3, { action_id: 'action-1', tool_name: 'easydo_write' }),
      event('action.execution_succeeded', 4, { action_id: 'action-1', tool_name: 'easydo_write', status: 'success' })
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'action.decision_required',
    'action.approved',
    'action.execution_started',
    'action.execution_succeeded'
  ])
})

test('runtime process does not hide narration or failures around approval pauses', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      event('session.step.started', 1),
      event('session.text.ended', 2, { text: '等待用户审批后继续。' }),
      event('permission.asked', 3, { call_id: 'call-1', tool_name: 'easydo_write' }),
      event('session.tool.failed', 4, {
        call_id: 'call-1',
        tool_name: 'easydo_write',
        error: { message: 'Tool easydo_write requires approval' }
      }),
      event('session.step.ended', 5)
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.text.ended',
    'permission.asked',
    'session.tool.failed',
    'session.step.ended'
  ])
  assert.ok(JSON.stringify(lines[1]).includes('等待用户审批后继续。'))
})

test('runtime process only compacts adjacent deltas and keeps lifecycle events as rows', () => {
  const lines = buildRuntimeProcessItems({
    events: [
      event('session.step.started', 1),
      event('session.reasoning.started', 2, { reasoning_id: 'reason-1' }),
      event('session.reasoning.delta', 3, { reasoning_id: 'reason-1', delta: '先' }),
      event('session.reasoning.delta', 4, { reasoning_id: 'reason-1', delta: '分析' }),
      event('session.reasoning.ended', 5, { reasoning_id: 'reason-1', text: '先分析' }),
      event('session.text.started', 6, { text_id: 'text-1' }),
      event('session.text.delta', 7, { text_id: 'text-1', delta: '完成' }),
      event('session.text.ended', 8, { text_id: 'text-1', text: '完成' }),
      event('session.tool.input.started', 9, { call_id: 'call-1', tool_name: 'read_file' }),
      event('session.tool.input.delta', 10, { call_id: 'call-1', tool_name: 'read_file', delta: '{"path":' }),
      event('session.tool.input.delta', 11, { call_id: 'call-1', tool_name: 'read_file', delta: '"a"}' }),
      event('session.tool.input.ended', 12, { call_id: 'call-1', tool_name: 'read_file', text: '{"path":"a"}' }),
      event('session.tool.progress', 13, { call_id: 'call-1', tool_name: 'read_file', message: 'reading' }),
      event('session.step.ended', 14)
    ]
  })

  assert.deepEqual(lines.map((item) => item.event), [
    'session.step.started',
    'session.reasoning.started',
    'session.reasoning.delta',
    'session.reasoning.ended',
    'session.text.started',
    'session.text.delta',
    'session.text.ended',
    'session.tool.input.started',
    'session.tool.input.delta',
    'session.tool.input.ended',
    'session.tool.progress',
    'session.step.ended'
  ])
  assert.equal(lines[2].sections[0].text, '先分析')
  assert.equal(lines[8].inputPreviewText.includes('path'), true)
})
