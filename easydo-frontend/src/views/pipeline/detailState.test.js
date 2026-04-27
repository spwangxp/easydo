import test from 'node:test'
import assert from 'node:assert/strict'
import { parseJSONField } from './runtimeConfig.js'

const buildRunTasksFromRunRecord = (run) => {
  const resolvedNodes = parseJSONField(run?.resolved_nodes_json, []) || []
  const outputsByNode = parseJSONField(run?.outputs_json, {}) || {}
  const events = parseJSONField(run?.events_json, []) || []
  const snapshot = parseJSONField(run?.pipeline_snapshot_json, {}) || {}
  const snapshotNodes = Array.isArray(snapshot?.nodes) ? snapshot.nodes : []
  const snapshotNodeMap = new Map(snapshotNodes.map((node, index) => [
    node.node_id || node.id,
    { ...node, __index: index }
  ]))

  const eventBuckets = new Map()
  events.forEach((event) => {
    const nodeID = event?.payload?.node_id
    if (!nodeID) return
    if (!eventBuckets.has(nodeID)) {
      eventBuckets.set(nodeID, [])
    }
    eventBuckets.get(nodeID).push(event)
  })

  return resolvedNodes.map((node, index) => {
    const nodeID = node.node_id || `node_${index + 1}`
    const snapshotNode = snapshotNodeMap.get(nodeID) || null
    const attempts = Array.isArray(node.attempts) ? node.attempts : []
    const latestAttempt = attempts.length > 0 ? attempts[attempts.length - 1] : null
    const nodeEvents = eventBuckets.get(nodeID) || []
    const startEvent = nodeEvents.find(item => item?.event_type === 'node_running')

    const normalizedStatus =
      node.status === 'success' ? 'execute_success' :
      node.status === 'failed' ? 'execute_failed' :
      node.status || 'queued'

    return {
      id: latestAttempt?.task_id || latestAttempt?.attempt_no || 0,
      node_id: nodeID,
      name: node.node_name || snapshotNode?.node_name || snapshotNode?.name || nodeID,
      task_type: node.task_key || '',
      status: normalizedStatus,
      display_status: normalizedStatus,
      ignore_failure: Boolean(snapshotNode?.ignore_failure),
      start_time: latestAttempt?.start_time || startEvent?.time || 0,
      created_at: latestAttempt?.start_time || startEvent?.time || run?.created_at || 0,
      duration: latestAttempt?.duration || 0,
      exit_code: latestAttempt?.exit_code ?? outputsByNode[nodeID]?.exit_code ?? 0,
      error_msg: latestAttempt?.error_msg || '',
      outputs: outputsByNode[nodeID] || {},
      _order: Number.isFinite(snapshotNode?.__index) ? snapshotNode.__index : index,
      Agent: latestAttempt?.agent_id ? { name: `Agent #${latestAttempt.agent_id}` } : null
    }
  }).sort((a, b) => a._order - b._order)
}

const normalizeRunTaskFromApi = (task, index, fallbackTaskMap = new Map()) => {
  const nodeID = task?.node_id || task?.NodeID || task?.nodeId || ''
  const taskID = Number(task?.id || task?.task_id || 0)
  const fallback = (nodeID && fallbackTaskMap.get(nodeID))
    || (taskID > 0 ? Array.from(fallbackTaskMap.values()).find(item => Number(item.id || 0) === taskID) : null)
    || null

  const rawStatus = task?.status || fallback?.status || 'queued'
  const normalizedStatus =
    rawStatus === 'success' ? 'execute_success'
      : rawStatus === 'failed' ? 'execute_failed'
        : rawStatus

  const rawDisplayStatus = task?.display_status || task?.displayStatus || ''
  const normalizedDisplayStatus = rawDisplayStatus || normalizedStatus
  const resolvedName = task?.name || task?.task_name || task?.node_name || fallback?.name || nodeID || (taskID ? `任务 #${taskID}` : `任务 #${index + 1}`)

  return {
    ...fallback,
    ...task,
    id: taskID || Number(fallback?.id || 0),
    node_id: nodeID || fallback?.node_id || fallback?.NodeID || '',
    name: resolvedName,
    task_type: task?.task_type || task?.type || task?.task_key || fallback?.task_type || '',
    status: normalizedStatus,
    display_status: normalizedDisplayStatus,
    ignore_failure: Boolean(task?.ignore_failure ?? fallback?.ignore_failure),
    start_time: task?.start_time ?? fallback?.start_time ?? 0,
    created_at: task?.created_at ?? fallback?.created_at ?? 0,
    duration: task?.duration ?? fallback?.duration ?? 0,
    exit_code: task?.exit_code ?? fallback?.exit_code ?? 0,
    error_msg: task?.error_msg || fallback?.error_msg || '',
    outputs: task?.outputs || fallback?.outputs || {},
    _order: Number.isFinite(task?._order)
      ? task._order
      : Number.isFinite(fallback?._order)
        ? fallback._order
        : index,
    Agent: task?.Agent || (task?.agent_name ? { name: task.agent_name } : fallback?.Agent || null)
  }
}

test('buildRunTasksFromRunRecord carries node ignore_failure and failed attempt exit code', () => {
  const run = {
    created_at: 1710000000,
    pipeline_snapshot_json: JSON.stringify({
      nodes: [
        { node_id: 'node_2', node_name: 'Build', ignore_failure: true }
      ]
    }),
    resolved_nodes_json: JSON.stringify([
      {
        node_id: 'node_2',
        node_name: 'Build',
        status: 'failed',
        attempts: [
          {
            task_id: 2,
            start_time: 1710000001,
            duration: 33,
            exit_code: 7,
            error_msg: 'build failed'
          }
        ]
      }
    ]),
    outputs_json: JSON.stringify({
      node_2: {
        exit_code: 7,
        duration: 33
      }
    }),
    events_json: '[]'
  }

  const tasks = buildRunTasksFromRunRecord(run)

  assert.equal(tasks.length, 1)
  assert.equal(tasks[0].status, 'execute_failed')
  assert.equal(tasks[0].ignore_failure, true)
  assert.equal(tasks[0].exit_code, 7)
  assert.equal(tasks[0].duration, 33)
})

test('normalizeRunTaskFromApi preserves ignore_failure exit code and duration from fallback snapshot', () => {
  const fallbackTaskMap = new Map([
    ['node_2', {
      id: 2,
      node_id: 'node_2',
      name: 'Build',
      status: 'execute_failed',
      display_status: 'execute_failed',
      ignore_failure: true,
      exit_code: 9,
      duration: 21,
      outputs: { exit_code: 9, duration: 21 },
      _order: 0
    }]
  ])

  const normalized = normalizeRunTaskFromApi({
    id: 2,
    node_id: 'node_2',
    status: 'execute_failed',
    error_msg: 'failed'
  }, 0, fallbackTaskMap)

  assert.equal(normalized.ignore_failure, true)
  assert.equal(normalized.exit_code, 9)
  assert.equal(normalized.duration, 21)
})
