import test from 'node:test'
import assert from 'node:assert/strict'
import {
  createRunInputs,
  createRunInputsFromRerunPreview,
  extractManualRunNodes,
  buildRunInputsPayload,
  normalizeRunParameterViewPayload,
  normalizeRerunPreviewPayload,
  parseJSONField
} from './runtimeConfig.js'

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

test('createRunInputsFromRerunPreview builds run-form inputs from prefill_inputs', () => {
  const manualRunNodes = [
    {
      node_id: 'node_1',
      node_name: 'Build',
      params: [
        { key: 'script', value: 'echo current' },
        { key: 'git_ref', value: 'main' }
      ]
    },
    {
      node_id: 'node_2',
      node_name: 'Deploy',
      params: [
        { key: 'image', value: 'nginx:latest' }
      ]
    }
  ]

  const rerunPreview = normalizeRerunPreviewPayload({
    prefill_inputs: {
      node_1: {
        script: 'echo historical',
        git_ref: 'release/2026.05'
      },
      node_2: {
        image: 'nginx:1.27'
      }
    }
  })

  assert.deepEqual(createRunInputsFromRerunPreview(manualRunNodes, rerunPreview), {
    node_1: {
      script: 'echo historical',
      git_ref: 'release/2026.05'
    },
    node_2: {
      image: 'nginx:1.27'
    }
  })
})

test('createRunInputsFromRerunPreview preserves current manual-run node structure and ignores unknown keys', () => {
  const manualRunNodes = [
    {
      node_id: 'node_1',
      node_name: 'Build',
      params: [
        { key: 'script', value: 'echo current' },
        { key: 'args', value: ['--prod'] }
      ]
    }
  ]

  const rerunPreview = normalizeRerunPreviewPayload({
    prefill_inputs: {
      node_1: {
        script: 'echo historical',
        extra: 'ignored'
      },
      node_9: {
        script: 'missing node ignored'
      }
    }
  })

  const inputs = createRunInputsFromRerunPreview(manualRunNodes, rerunPreview)

  assert.deepEqual(inputs, {
    node_1: {
      script: 'echo historical',
      args: ['--prod']
    }
  })
  assert.equal(Object.prototype.hasOwnProperty.call(inputs.node_1, 'extra'), false)
})

test('createRunInputsFromRerunPreview preserves existing run dialog data shape when no rerun preview is involved', () => {
  const manualRunNodes = [
    {
      node_id: 'node_1',
      node_name: 'Build',
      params: [
        { key: 'script', value: 'echo current' },
        { key: 'enabled', value: true }
      ]
    }
  ]

  assert.deepEqual(createRunInputsFromRerunPreview(manualRunNodes, null), createRunInputs(manualRunNodes))
  assert.deepEqual(createRunInputs(manualRunNodes), {
    node_1: {
      script: 'echo current',
      enabled: true
    }
  })
})

test('createRunInputsFromRerunPreview ignores malformed prefill payloads and clones array values', () => {
  const manualRunNodes = [
    {
      node_id: 'node_1',
      node_name: 'Build',
      params: [
        { key: 'script', value: 'echo current' },
        { key: 'args', value: ['--prod'] }
      ]
    }
  ]

  const previewArgs = ['--debug']
  const inputs = createRunInputsFromRerunPreview(manualRunNodes, {
    prefill_inputs: {
      node_1: {
        script: null,
        args: previewArgs,
        unset: undefined
      },
      node_2: ['ignored-array-node-payload']
    },
    failure: []
  })

  assert.deepEqual(inputs, {
    node_1: {
      script: 'echo current',
      args: ['--debug']
    }
  })
  assert.notStrictEqual(inputs.node_1.args, previewArgs)
})

test('normalizeRunParameterViewPayload keeps runtime and default params as separate node sections', () => {
  const normalized = normalizeRunParameterViewPayload({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        runtime_params: [
          { key: 'script', label: '脚本', value: 'echo historical' },
          { key: 'args', label: '参数', value: ['--prod'] }
        ],
        default_params: [
          { key: 'script', label: '脚本', value: 'echo default' },
          { key: 'image', label: '镜像', value: 'node:20' }
        ]
      },
      {
        node_id: 'node_2',
        runtime_params: null,
        default_params: ['ignored']
      }
    ]
  })

  assert.deepEqual(normalized, {
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        runtime_params: {
          script: 'echo historical',
          args: ['--prod']
        },
        default_params: {
          script: 'echo default',
          image: 'node:20'
        }
      },
      {
        node_id: 'node_2',
        node_name: 'node_2',
        runtime_params: {},
        default_params: {}
      }
    ]
  })
})

test('extractManualRunNodes uses task version specific field schema and preserves richer input types', () => {
  const manualRunNodes = extractManualRunNodes({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        task_version: 2,
        params: [
          { key: 'script', value: 'echo current', is_flexible: true },
          { key: 'advanced', value: { retries: 2 }, is_flexible: true },
          { key: 'dry_run', value: true, is_flexible: true },
          { key: 'targets', value: ['prod'], is_flexible: true }
        ]
      }
    ]
  }, [
    {
      task_key: 'shell',
      task_version: 1,
      fields_schema: [
        { key: 'script', label: '脚本 V1', type: 'text' }
      ]
    },
    {
      task_key: 'shell',
      task_version: 2,
      fields_schema: [
        { key: 'script', label: '脚本', type: 'text', ui_component: 'textarea' },
        { key: 'advanced', label: '高级配置', type: 'json' },
        { key: 'dry_run', label: '试运行', type: 'boolean' },
        { key: 'targets', label: '目标环境', type: 'multiselect', options: ['prod', 'staging'] }
      ]
    }
  ])

  assert.deepEqual(manualRunNodes, [
    {
      node_id: 'node_1',
      node_name: 'Build',
      params: [
        {
          key: 'script',
          label: '脚本',
          value: 'echo current',
          input_type: 'textarea',
          placeholder: '',
          options: []
        },
        {
          key: 'advanced',
          label: '高级配置',
          value: { retries: 2 },
          input_type: 'textarea',
          placeholder: '',
          options: []
        },
        {
          key: 'dry_run',
          label: '试运行',
          value: true,
          input_type: 'boolean',
          placeholder: '',
          options: []
        },
        {
          key: 'targets',
          label: '目标环境',
          value: ['prod'],
          input_type: 'checkbox_group',
          placeholder: '',
          options: [
            { label: 'prod', value: 'prod' },
            { label: 'staging', value: 'staging' }
          ]
        }
      ]
    }
  ])
})

test('createRunInputs clones arrays and keeps object values for rerun capable params', () => {
  const sourceObject = { retries: 2 }
  const sourceArray = ['prod']
  const inputs = createRunInputs([
    {
      node_id: 'node_1',
      params: [
        { key: 'advanced', value: sourceObject },
        { key: 'targets', value: sourceArray }
      ]
    }
  ])

  assert.deepEqual(inputs, {
    node_1: {
      advanced: sourceObject,
      targets: ['prod']
    }
  })
  assert.notStrictEqual(inputs.node_1.targets, sourceArray)
})

test('buildRunInputsPayload omits empty values and preserves filled structured values', () => {
  const payload = buildRunInputsPayload([
    {
      node_id: 'node_1',
      params: [
        { key: 'script' },
        { key: 'advanced' },
        { key: 'targets' },
        { key: 'skip' }
      ]
    }
  ], {
    node_1: {
      script: '',
      advanced: { retries: 3 },
      targets: ['prod'],
      skip: null
    }
  })

  assert.deepEqual(payload, {
    inputs: {
      node_1: {
        advanced: { retries: 3 },
        targets: ['prod']
      }
    }
  })
})
