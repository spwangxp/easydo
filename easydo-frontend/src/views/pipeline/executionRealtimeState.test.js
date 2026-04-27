import test from 'node:test'
import assert from 'node:assert/strict'

import {
  applyTaskStatusPayload,
  buildTaskOutputsFromPayload,
  normalizeExecutionTaskOutputs
} from './executionRealtimeState.js'

test('buildTaskOutputsFromPayload prefers explicit outputs object', () => {
  const outputs = buildTaskOutputsFromPayload({
    outputs: {
      artifact: 'bundle.tgz',
      exit_code: 0
    },
    result_data: '{"artifact":"ignored.tgz"}'
  })

  assert.deepEqual(outputs, {
    artifact: 'bundle.tgz',
    exit_code: 0
  })
})

test('buildTaskOutputsFromPayload parses result_data json string', () => {
  const outputs = buildTaskOutputsFromPayload({
    result_data: '{"artifact":"bundle.tgz","commit_sha":"abc123"}'
  })

  assert.deepEqual(outputs, {
    artifact: 'bundle.tgz',
    commit_sha: 'abc123'
  })
})

test('applyTaskStatusPayload merges websocket outputs into existing task', () => {
  const tasks = [{
    id: 17,
    node_id: 'node_build',
    name: 'Build',
    status: 'running',
    display_status: 'running',
    outputs: {}
  }]

  const updatedTasks = applyTaskStatusPayload(tasks, {
    task_id: 17,
    run_id: 9,
    node_id: 'node_build',
    status: 'execute_success',
    duration: 12,
    outputs: {
      artifact: 'bundle.tgz',
      exit_code: 0
    }
  })

  assert.equal(updatedTasks[0].status, 'execute_success')
  assert.deepEqual(updatedTasks[0].outputs, {
    artifact: 'bundle.tgz',
    exit_code: 0
  })
})

test('applyTaskStatusPayload creates placeholder task with websocket outputs', () => {
  const updatedTasks = applyTaskStatusPayload([], {
    task_id: 21,
    run_id: 9,
    node_id: 'node_test',
    node_name: 'Unit Test',
    status: 'execute_success',
    outputs: {
      tests_passed: 18,
      tests_failed: 0
    }
  })

  assert.equal(updatedTasks.length, 1)
  assert.equal(updatedTasks[0].name, 'Unit Test')
  assert.deepEqual(updatedTasks[0].outputs, {
    tests_passed: 18,
    tests_failed: 0
  })
})

test('normalizeExecutionTaskOutputs maps result_data onto outputs for initial snapshot tasks', () => {
  const normalized = normalizeExecutionTaskOutputs({
    id: 31,
    node_id: 'node_package',
    result_data: '{"artifact":"release.zip","duration":30}'
  })

  assert.deepEqual(normalized.outputs, {
    artifact: 'release.zip',
    duration: 30
  })
})

test('applyTaskStatusPayload preserves exit code and duration on failed tasks', () => {
  const updatedTasks = applyTaskStatusPayload([], {
    task_id: 44,
    node_id: 'node_2',
    node_name: 'Build',
    status: 'execute_failed',
    exit_code: 7,
    duration: 19,
    outputs: {
      exit_code: 7,
      duration: 19
    },
    error_msg: 'command failed'
  })

  assert.equal(updatedTasks[0].status, 'execute_failed')
  assert.equal(updatedTasks[0].exit_code, 7)
  assert.equal(updatedTasks[0].duration, 19)
  assert.deepEqual(updatedTasks[0].outputs, {
    exit_code: 7,
    duration: 19
  })
})
