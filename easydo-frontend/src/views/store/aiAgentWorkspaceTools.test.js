import test from 'node:test'
import assert from 'node:assert/strict'
import {
  EASYDO_WORKSPACE_TOOL_NAMES,
  mergeSubagentConfigWithMode,
  mergeToolPolicyWithWorkspaceTools,
  normalizeWorkspaceTools,
  subagentModeFromConfig,
  workspaceToolsFromToolPolicy
} from './aiAgentWorkspaceTools.js'

test('normalizeWorkspaceTools defaults to full catalog when empty', () => {
  assert.deepEqual(normalizeWorkspaceTools(undefined), EASYDO_WORKSPACE_TOOL_NAMES)
  assert.deepEqual(normalizeWorkspaceTools([]), EASYDO_WORKSPACE_TOOL_NAMES)
  assert.deepEqual(normalizeWorkspaceTools(['read_file', 'bash', 'unknown']), ['read_file', 'bash'])
})

test('mergeToolPolicyWithWorkspaceTools keeps other policy fields', () => {
  const merged = mergeToolPolicyWithWorkspaceTools(
    { default_decision: 'request', rules: [{ id: 'r1' }] },
    ['read_file', 'write_file']
  )
  assert.equal(merged.default_decision, 'request')
  assert.deepEqual(merged.rules, [{ id: 'r1' }])
  assert.deepEqual(merged.workspace_tools, ['read_file', 'write_file'])
})

test('workspaceToolsFromToolPolicy reads configured tools', () => {
  assert.deepEqual(
    workspaceToolsFromToolPolicy({ workspace_tools: ['bash', 'list_directory'] }),
    ['bash', 'list_directory']
  )
})

test('subagent mode helpers round-trip config.mode', () => {
  assert.equal(subagentModeFromConfig({}), 'read_only')
  assert.equal(subagentModeFromConfig({ mode: 'write' }), 'write')
  assert.deepEqual(mergeSubagentConfigWithMode({ policy: 'ask' }, 'write'), {
    policy: 'ask',
    mode: 'write'
  })
})
