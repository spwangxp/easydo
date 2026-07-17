import test from 'node:test'
import assert from 'node:assert/strict'
import {
  isPiApprovalAction,
  resolveAgentActionRef
} from './agentConversationShared.js'

test('resolveAgentActionRef keeps pi_approval when only id is missing from pending list', () => {
  const fallback = {
    id: 'approval:abc',
    action_id: 'approval:abc',
    pi_approval: true,
    runtime_run_id: 'r_w1_1',
    display_json: {
      approval_request: {
        approval_id: 'approval:abc',
        tool_name: 'easydo_workspace_list'
      }
    }
  }
  const resolved = resolveAgentActionRef(fallback, [])
  assert.equal(resolved.pi_approval, true)
  assert.equal(resolved.runtime_run_id, 'r_w1_1')
  assert.equal(isPiApprovalAction(resolved), true)
})

test('resolveAgentActionRef treats approval: ids as Pi approvals even without flag', () => {
  const resolved = resolveAgentActionRef('approval:xyz', [])
  assert.equal(resolved.id, 'approval:xyz')
  assert.equal(resolved.pi_approval, true)
  assert.equal(isPiApprovalAction(resolved), true)
})

test('resolveAgentActionRef merges full object over pending action metadata', () => {
  const pending = [{
    id: 'approval:1',
    pi_approval: false,
    runtime_run_id: 'stale-run'
  }]
  const resolved = resolveAgentActionRef({
    id: 'approval:1',
    pi_approval: true,
    runtime_run_id: 'live-run'
  }, pending)
  assert.equal(resolved.pi_approval, true)
  assert.equal(resolved.runtime_run_id, 'live-run')
})
