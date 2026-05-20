import test from 'node:test'
import assert from 'node:assert/strict'
import {
  createRunInputs,
  createRunInputsFromRerunPreview,
  extractManualRunNodes,
  extractWebhookRuntimeInputTargets,
  buildRunInputsPayload,
  normalizeRunParameterViewPayload,
  normalizeRerunPreviewPayload,
  normalizeWebhookRuntimeInputMappings,
  normalizeWebhookRuntimePreviewPayload,
  normalizeWebhookRuntimeStructuredErrors,
  normalizeWebhookRuntimeTriggerConfig,
  createWebhookRuntimeInputMappingRow,
  serializeWebhookRuntimeInputMappings,
  buildGitlabWebhookRuntimePresetMappings,
  buildWebhookRuntimeMappingEditorRows,
  applyGitlabWebhookRuntimePresetToRows,
  validateWebhookRuntimeJSONPath,
  parseJSONField
} from './runtimeConfig.js'

const buildTaskRuntimeSummary = (source) => {
  const runtimeProfileID = Number(source?.runtime_profile_id || 0)
  const providerID = Number(source?.provider_id || 0)
  const modelID = Number(source?.model_id || 0)
  const parts = []
  if (runtimeProfileID > 0) parts.push(`Runtime #${runtimeProfileID}`)
  if (providerID > 0) parts.push(`Provider #${providerID}`)
  if (modelID > 0) parts.push(`Model #${modelID}`)
  return parts.join(' / ')
}

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
      Agent: latestAttempt?.agent_id ? { name: `Agent #${latestAttempt.agent_id}` } : null,
      runtime_summary: buildTaskRuntimeSummary(latestAttempt)
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
    Agent: task?.Agent || (task?.agent_name ? { name: task.agent_name } : fallback?.Agent || null),
    runtime_summary: task?.runtime_summary || fallback?.runtime_summary || buildTaskRuntimeSummary(task) || buildTaskRuntimeSummary(fallback)
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
            error_msg: 'build failed',
            runtime_profile_id: 11,
            provider_id: 22,
            model_id: 33
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
  assert.equal(tasks[0].runtime_summary, 'Runtime #11 / Provider #22 / Model #33')
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

  fallbackTaskMap.get('node_2').runtime_summary = 'Runtime #11 / Provider #22 / Model #33'

  const normalized = normalizeRunTaskFromApi({
    id: 2,
    node_id: 'node_2',
    status: 'execute_failed',
    error_msg: 'failed'
  }, 0, fallbackTaskMap)

  assert.equal(normalized.ignore_failure, true)
  assert.equal(normalized.exit_code, 9)
  assert.equal(normalized.duration, 21)
  assert.equal(normalized.runtime_summary, 'Runtime #11 / Provider #22 / Model #33')
})

test('createRunInputsFromRerunPreview builds run-form inputs from prefill_inputs', () => {
  const manualRunNodes = [
    {
      node_id: 'node_1',
      node_index: 1,
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
      node_index: 1,
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
      node_index: 1,
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

test('createRunInputsFromRerunPreview ignores malformed prefill payloads and clones array/object values', () => {
  const manualRunNodes = [
    {
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      params: [
        { key: 'script', value: 'echo current' },
        { key: 'args', value: ['--prod'] },
        { key: 'advanced', value: { retries: 1 } }
      ]
    }
  ]

  const previewArgs = ['--debug']
  const previewAdvanced = { retries: 3 }
  const inputs = createRunInputsFromRerunPreview(manualRunNodes, {
    prefill_inputs: {
      node_1: {
        script: null,
        args: previewArgs,
        advanced: previewAdvanced,
        unset: undefined
      },
      node_2: ['ignored-array-node-payload']
    },
    failure: []
  })

  assert.deepEqual(inputs, {
    node_1: {
      script: 'echo current',
      args: ['--debug'],
      advanced: { retries: 3 }
    }
  })
  assert.notStrictEqual(inputs.node_1.args, previewArgs)
  assert.notStrictEqual(inputs.node_1.advanced, previewAdvanced)
})

test('normalizeRunParameterViewPayload keeps runtime and default params as separate node sections', () => {
  const normalized = normalizeRunParameterViewPayload({
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

test('normalizeRunParameterViewPayload converts historical param arrays into keyed records', () => {
  const normalized = normalizeRunParameterViewPayload({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        runtime_params: [
          { key: 'script', value: 'echo historical' },
          { key: 'args', value: ['--prod'] }
        ],
        default_params: [
          { key: 'script', value: 'echo default' },
          { key: 'image', value: 'node:20' }
        ]
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
      }
    ]
  })
})

test('extractManualRunNodes uses task version specific field schema and preserves richer input metadata', () => {
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
      node_index: 1,
      node_name: 'Build',
      params: [
        {
          key: 'script',
          label: '脚本',
          value: 'echo current',
          default_value: 'echo current',
          field_type: 'text',
          input_type: 'textarea',
          runtime_value_type: 'string',
          is_string_like: true,
          placeholder: '',
          options: []
        },
        {
          key: 'advanced',
          label: '高级配置',
          value: { retries: 2 },
          default_value: { retries: 2 },
          field_type: 'json',
          input_type: 'textarea',
          runtime_value_type: '',
          is_string_like: false,
          placeholder: '',
          options: []
        },
        {
          key: 'dry_run',
          label: '试运行',
          value: true,
          default_value: true,
          field_type: 'boolean',
          input_type: 'boolean',
          runtime_value_type: 'boolean',
          is_string_like: false,
          placeholder: '',
          options: []
        },
        {
          key: 'targets',
          label: '目标环境',
          value: ['prod'],
          default_value: ['prod'],
          field_type: 'multiselect',
          input_type: 'checkbox_group',
          runtime_value_type: '',
          is_string_like: false,
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

test('extractManualRunNodes only keeps flexible params that exist in the task field schema', () => {
  const manualRunNodes = extractManualRunNodes({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        params: [
          { key: 'script', value: 'echo current', is_flexible: true },
          { key: 'missing', value: 'ignored', is_flexible: true },
          { key: 'non_flexible', value: 'ignored', is_flexible: false }
        ]
      }
    ]
  }, [
    {
      task_key: 'shell',
      fields_schema: [
        { key: 'script', label: '脚本', type: 'text' },
        { key: 'non_flexible', label: '非运行时字段', type: 'text' }
      ]
    }
  ])

  assert.deepEqual(manualRunNodes, [
    {
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      params: [
        {
          key: 'script',
          label: '脚本',
          value: 'echo current',
          default_value: 'echo current',
          field_type: 'text',
          input_type: 'text',
          runtime_value_type: 'string',
          is_string_like: true,
          placeholder: '',
          options: []
        }
      ]
    }
  ])
})

test('extractManualRunNodes falls back to saved flexible params when task definitions are unavailable', () => {
  const manualRunNodes = extractManualRunNodes({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        params: [
          { key: 'script', value: 'echo current', is_flexible: true },
          { key: 'dry_run', value: true, is_flexible: true },
          { key: 'targets', value: ['prod'], is_flexible: true },
          { key: 'fixed', value: 'ignored', is_flexible: false }
        ]
      }
    ]
  })

  assert.deepEqual(manualRunNodes, [
    {
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      params: [
        {
          key: 'script',
          label: 'script',
          value: 'echo current',
          default_value: 'echo current',
          field_type: 'text',
          input_type: 'text',
          runtime_value_type: 'string',
          is_string_like: true,
          placeholder: '',
          options: []
        },
        {
          key: 'dry_run',
          label: 'dry_run',
          value: true,
          default_value: true,
          field_type: 'boolean',
          input_type: 'boolean',
          runtime_value_type: 'boolean',
          is_string_like: false,
          placeholder: '',
          options: []
        },
        {
          key: 'targets',
          label: 'targets',
          value: ['prod'],
          default_value: ['prod'],
          field_type: 'text',
          input_type: 'checkbox_group',
          runtime_value_type: '',
          is_string_like: true,
          placeholder: '',
          options: []
        }
      ]
    }
  ])
})

test('extractWebhookRuntimeInputTargets flattens reusable runtime targets for mapping UI and exposes node_index', () => {
  const targets = extractWebhookRuntimeInputTargets({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        params: [
          { key: 'script', value: 'echo current', is_flexible: true },
          { key: 'port', value: 8080, is_flexible: true },
          { key: 'dry_run', value: true, is_flexible: true }
        ]
      },
      {
        node_id: 'node_2',
        node_name: 'Deploy',
        task_key: 'shell',
        params: [
          { key: 'ignored_json', value: { retries: 2 }, is_flexible: true },
          { key: 'git_ref', value: 'main', is_flexible: true }
        ]
      }
    ]
  }, [
    {
      task_key: 'shell',
      fields_schema: [
        { key: 'script', label: '脚本', type: 'text', ui_component: 'textarea' },
        { key: 'port', label: '端口', type: 'number' },
        { key: 'dry_run', label: '试运行', type: 'boolean' },
        { key: 'ignored_json', label: '高级配置', type: 'json' },
        { key: 'git_ref', label: 'Git 引用', type: 'text' }
      ]
    }
  ])

  assert.deepEqual(targets, [
    {
      target_key: 'node_1.script',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'script',
      param_label: '脚本',
      default_value: 'echo current',
      field_type: 'text',
      input_type: 'textarea',
      runtime_value_type: 'string',
      is_string_like: true,
      placeholder: '',
      options: []
    },
    {
      target_key: 'node_1.port',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'port',
      param_label: '端口',
      default_value: 8080,
      field_type: 'number',
      input_type: 'number',
      runtime_value_type: 'number',
      is_string_like: false,
      placeholder: '',
      options: []
    },
    {
      target_key: 'node_1.dry_run',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'dry_run',
      param_label: '试运行',
      default_value: true,
      field_type: 'boolean',
      input_type: 'boolean',
      runtime_value_type: 'boolean',
      is_string_like: false,
      placeholder: '',
      options: []
    },
    {
      target_key: 'node_2.git_ref',
      node_id: 'node_2',
      node_index: 2,
      node_name: 'Deploy',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      default_value: 'main',
      field_type: 'text',
      input_type: 'text',
      runtime_value_type: 'string',
      is_string_like: true,
      placeholder: '',
      options: []
    }
  ])
})

test('extractWebhookRuntimeInputTargets excludes unsupported non-scalar mapping targets', () => {
  const targets = extractWebhookRuntimeInputTargets({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        params: [
          { key: 'script', value: 'echo current', is_flexible: true },
          { key: 'advanced', value: { retries: 2 }, is_flexible: true },
          { key: 'targets', value: ['prod'], is_flexible: true }
        ]
      }
    ]
  }, [
    {
      task_key: 'shell',
      fields_schema: [
        { key: 'script', label: '脚本', type: 'text' },
        { key: 'advanced', label: '高级配置', type: 'json' },
        { key: 'targets', label: '目标环境', type: 'multiselect', options: ['prod'] }
      ]
    }
  ])

  assert.deepEqual(targets.map(item => item.param_key), ['script'])
})

test('extractWebhookRuntimeInputTargets falls back to saved flexible params when task definitions are unavailable', () => {
  const targets = extractWebhookRuntimeInputTargets({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'shell',
        params: [
          { key: 'script', value: 'echo current', is_flexible: true },
          { key: 'port', value: 8080, is_flexible: true },
          { key: 'dry_run', value: true, is_flexible: true },
          { key: 'advanced', value: { retries: 2 }, is_flexible: true },
          { key: 'targets', value: ['prod'], is_flexible: true }
        ]
      }
    ]
  })

  assert.deepEqual(targets, [
    {
      target_key: 'node_1.script',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'script',
      param_label: 'script',
      default_value: 'echo current',
      field_type: 'text',
      input_type: 'text',
      runtime_value_type: 'string',
      is_string_like: true,
      placeholder: '',
      options: []
    },
    {
      target_key: 'node_1.port',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'port',
      param_label: 'port',
      default_value: 8080,
      field_type: 'number',
      input_type: 'number',
      runtime_value_type: 'number',
      is_string_like: false,
      placeholder: '',
      options: []
    },
    {
      target_key: 'node_1.dry_run',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'dry_run',
      param_label: 'dry_run',
      default_value: true,
      field_type: 'boolean',
      input_type: 'boolean',
      runtime_value_type: 'boolean',
      is_string_like: false,
      placeholder: '',
      options: []
    }
  ])
})

test('extractWebhookRuntimeInputTargets keeps string schema fields as webhook-mappable targets', () => {
  const targets = extractWebhookRuntimeInputTargets({
    nodes: [
      {
        node_id: 'node_1',
        node_name: 'Build',
        task_key: 'git_clone',
        params: [
          { key: 'git_ref', value: 'main', is_flexible: true },
          { key: 'checkout_path', value: './app', is_flexible: false }
        ]
      }
    ]
  }, [
    {
      task_key: 'git_clone',
      fields_schema: [
        { key: 'git_ref', label: 'Git 引用', type: 'string', ui_component: 'input' },
        { key: 'checkout_path', label: '检出目录', type: 'string', ui_component: 'input' }
      ]
    }
  ])

  assert.deepEqual(targets, [
    {
      target_key: 'node_1.git_ref',
      node_id: 'node_1',
      node_index: 1,
      node_name: 'Build',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      default_value: 'main',
      field_type: 'string',
      input_type: 'text',
      runtime_value_type: 'string',
      is_string_like: true,
      placeholder: '',
      options: []
    }
  ])
})

test('normalizeWebhookRuntimeInputMappings parses raw backend mapping rows into stable objects', () => {
  const mappings = normalizeWebhookRuntimeInputMappings(`[
    {"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"script"},"missing_policy":"ignore"},
    {"source_type":"jsonpath","source_expr":"$.port","target":{"node_id":"node-2","param_key":"port"}}
  ]`)

  assert.deepEqual(mappings, [
    {
      id: 'rule-1',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: {
        node_id: 'node-1',
        param_key: 'script'
      },
      missing_policy: 'ignore',
      deleted: false
    },
    {
      id: 'rule-2',
      source_type: 'jsonpath',
      source_expr: '$.port',
      target: {
        node_id: 'node-2',
        param_key: 'port'
      },
      missing_policy: 'ignore',
      deleted: false
    }
  ])
})

test('normalizeWebhookRuntimeTriggerConfig preserves raw transport fields and exposes parsed rows', () => {
  const normalized = normalizeWebhookRuntimeTriggerConfig({
    webhook_runtime_input_mappings: '[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"script"},"missing_policy":"ignore"}]',
    webhook_config_status: 'invalid_target',
    webhook_config_invalid_reason: 'node-1.script no longer exists'
  })

  assert.deepEqual(normalized, {
    webhook_runtime_input_mappings: '[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"script"},"missing_policy":"ignore"}]',
    mapping_rows: [
      {
        id: 'rule-1',
        source_type: 'jsonpath',
        source_expr: '$.ref',
        target: {
          node_id: 'node-1',
          param_key: 'script'
        },
        missing_policy: 'ignore',
        deleted: false
      }
    ],
    webhook_config_status: 'invalid_target',
    webhook_config_invalid_reason: 'node-1.script no longer exists'
  })
})

test('normalizeWebhookRuntimePreviewPayload normalizes preview values summary and structured errors', () => {
  const normalized = normalizeWebhookRuntimePreviewPayload({
    data: {
      values: {
        'node-1': {
          script: 'main'
        },
        'node-2': ['ignored']
      },
      rule_results: {
        matched: {
          code: 'matched',
          value: 'main'
        },
        missing_fail: {
          code: 'missing'
        }
      },
      summary: {
        total: 2,
        matched: 1,
        missing: 1,
        failed: 0
      }
    },
    errors: [
      {
        mapping_id: 'missing_fail',
        field: 'source_expr',
        code: 'missing',
        message: 'value not found'
      },
      null
    ]
  })

  assert.deepEqual(normalized, {
    values: {
      'node-1': {
        script: 'main'
      }
    },
    rule_results: {
      matched: {
        code: 'matched',
        value: 'main'
      },
      missing_fail: {
        code: 'missing',
        value: undefined
      }
    },
    summary: {
      total: 2,
      matched: 1,
      missing: 1,
      failed: 0
    },
    errors: [
      {
        mapping_id: 'missing_fail',
        field: 'source_expr',
        code: 'missing',
        message: 'value not found'
      }
    ]
  })
})

test('normalizeWebhookRuntimeStructuredErrors accepts alternate casing and ignores empty entries', () => {
  const normalized = normalizeWebhookRuntimeStructuredErrors([
    {
      mappingID: 'rule-1',
      field: 'target',
      code: 'invalid_target',
      message: 'target not found'
    },
    {},
    'ignored'
  ])

  assert.deepEqual(normalized, [
    {
      mapping_id: 'rule-1',
      field: 'target',
      code: 'invalid_target',
      message: 'target not found'
    }
  ])
})

test('buildWebhookRuntimeMappingEditorRows overlays saved mappings onto full targets and preserves ids', () => {
  const rows = buildWebhookRuntimeMappingEditorRows([
    {
      target_key: 'clone.git_ref',
      node_id: 'clone',
      node_index: 1,
      node_name: 'Clone',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      runtime_value_type: 'string'
    },
    {
      target_key: 'deploy.port',
      node_id: 'deploy',
      node_index: 2,
      node_name: 'Deploy',
      param_key: 'port',
      param_label: '端口',
      runtime_value_type: 'number'
    }
  ], [
    {
      id: 'saved-rule',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: { node_id: 'clone', param_key: 'git_ref' },
      missing_policy: 'fail'
    }
  ])

  assert.deepEqual(rows, [
    {
      id: 'saved-rule',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: {
        node_id: 'clone',
        param_key: 'git_ref'
      },
      missing_policy: 'fail',
      deleted: false,
      target_key: 'clone.git_ref',
      node_id: 'clone',
      node_index: 1,
      node_name: 'Clone',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-2',
      source_type: 'jsonpath',
      source_expr: '',
      target: {
        node_id: 'deploy',
        param_key: 'port'
      },
      missing_policy: 'ignore',
      deleted: true,
      target_key: 'deploy.port',
      node_id: 'deploy',
      node_index: 2,
      node_name: 'Deploy',
      param_key: 'port',
      param_label: '端口',
      runtime_value_type: 'number'
    }
  ])
})

test('createWebhookRuntimeInputMappingRow fills defaults and trims target fields', () => {
  const row = createWebhookRuntimeInputMappingRow({
    source_expr: ' $.ref ',
    target: {
      node_id: ' node-1 ',
      param_key: ' git_ref '
    }
  }, 2)

  assert.deepEqual(row, {
    id: 'rule-3',
    source_type: 'jsonpath',
    source_expr: '$.ref',
    target: {
      node_id: 'node-1',
      param_key: 'git_ref'
    },
    missing_policy: 'ignore',
    deleted: false
  })
})

test('serializeWebhookRuntimeInputMappings omits deleted rows from backend format', () => {
  const serialized = serializeWebhookRuntimeInputMappings([
    {
      id: 'rule-1',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: { node_id: 'node-1', param_key: 'git_ref' },
      missing_policy: 'ignore',
      deleted: false
    },
    {
      id: 'rule-2',
      source_type: 'jsonpath',
      source_expr: '$.object_attributes.source_branch',
      target: { node_id: 'node-2', param_key: 'source_branch' },
      missing_policy: 'fail',
      deleted: true
    },
    {
      id: 'rule-3',
      source_type: 'jsonpath',
      source_expr: '',
      target: { node_id: 'node-2', param_key: 'port' },
      missing_policy: 'fail',
      deleted: false
    }
  ])

  assert.equal(serialized, '[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"git_ref"},"missing_policy":"ignore"}]')
})

test('buildGitlabWebhookRuntimePresetMappings fills all matching fixed targets when presets apply', () => {
  const mappings = buildGitlabWebhookRuntimePresetMappings([
    {
      target_key: 'clone.git_ref',
      node_id: 'clone',
      node_name: 'Clone',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      runtime_value_type: 'string'
    },
    {
      target_key: 'mirror.git_ref',
      node_id: 'mirror',
      node_name: 'Mirror',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      runtime_value_type: 'string'
    },
    {
      target_key: 'review.source_branch',
      node_id: 'review',
      node_name: 'Review',
      param_key: 'source_branch',
      param_label: '源分支',
      runtime_value_type: 'string'
    },
    {
      target_key: 'deploy.port',
      node_id: 'deploy',
      node_name: 'Deploy',
      param_key: 'port',
      param_label: '端口',
      runtime_value_type: 'number'
    }
  ])

  assert.deepEqual(mappings, [
    {
      id: 'rule-1',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: {
        node_id: 'clone',
        param_key: 'git_ref'
      },
      missing_policy: 'ignore',
      deleted: false
    },
    {
      id: 'rule-2',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: {
        node_id: 'mirror',
        param_key: 'git_ref'
      },
      missing_policy: 'ignore',
      deleted: false
    },
    {
      id: 'rule-3',
      source_type: 'jsonpath',
      source_expr: '$.object_attributes.source_branch',
      target: {
        node_id: 'review',
        param_key: 'source_branch'
      },
      missing_policy: 'ignore',
      deleted: false
    }
  ])
})

test('applyGitlabWebhookRuntimePresetToRows restores and fills all matching fixed rows only', () => {
  const rows = applyGitlabWebhookRuntimePresetToRows([
    {
      id: 'rule-1',
      source_type: 'jsonpath',
      source_expr: '',
      target: { node_id: 'clone', param_key: 'git_ref' },
      missing_policy: 'fail',
      deleted: true,
      target_key: 'clone.git_ref',
      node_id: 'clone',
      node_index: 1,
      node_name: 'Clone',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-2',
      source_type: 'jsonpath',
      source_expr: '',
      target: { node_id: 'mirror', param_key: 'git_ref' },
      missing_policy: 'fail',
      deleted: true,
      target_key: 'mirror.git_ref',
      node_id: 'mirror',
      node_index: 2,
      node_name: 'Mirror',
      param_key: 'git_ref',
      param_label: '镜像 Git 引用',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-3',
      source_type: 'jsonpath',
      source_expr: '',
      target: { node_id: 'review', param_key: 'source_branch' },
      missing_policy: 'fail',
      deleted: true,
      target_key: 'review.source_branch',
      node_id: 'review',
      node_index: 3,
      node_name: 'Review',
      param_key: 'source_branch',
      param_label: '源分支',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-4',
      source_type: 'jsonpath',
      source_expr: '$.custom',
      target: { node_id: 'deploy', param_key: 'port' },
      missing_policy: 'fail',
      deleted: false,
      target_key: 'deploy.port',
      node_id: 'deploy',
      node_index: 4,
      node_name: 'Deploy',
      param_key: 'port',
      param_label: '端口',
      runtime_value_type: 'number'
    }
  ])

  assert.deepEqual(rows, [
    {
      id: 'rule-1',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: { node_id: 'clone', param_key: 'git_ref' },
      missing_policy: 'ignore',
      deleted: false,
      target_key: 'clone.git_ref',
      node_id: 'clone',
      node_index: 1,
      node_name: 'Clone',
      param_key: 'git_ref',
      param_label: 'Git 引用',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-2',
      source_type: 'jsonpath',
      source_expr: '$.ref',
      target: { node_id: 'mirror', param_key: 'git_ref' },
      missing_policy: 'ignore',
      deleted: false,
      target_key: 'mirror.git_ref',
      node_id: 'mirror',
      node_index: 2,
      node_name: 'Mirror',
      param_key: 'git_ref',
      param_label: '镜像 Git 引用',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-3',
      source_type: 'jsonpath',
      source_expr: '$.object_attributes.source_branch',
      target: { node_id: 'review', param_key: 'source_branch' },
      missing_policy: 'ignore',
      deleted: false,
      target_key: 'review.source_branch',
      node_id: 'review',
      node_index: 3,
      node_name: 'Review',
      param_key: 'source_branch',
      param_label: '源分支',
      runtime_value_type: 'string'
    },
    {
      id: 'rule-4',
      source_type: 'jsonpath',
      source_expr: '$.custom',
      target: { node_id: 'deploy', param_key: 'port' },
      missing_policy: 'fail',
      deleted: false,
      target_key: 'deploy.port',
      node_id: 'deploy',
      node_index: 4,
      node_name: 'Deploy',
      param_key: 'port',
      param_label: '端口',
      runtime_value_type: 'number'
    }
  ])
})

test('validateWebhookRuntimeJSONPath accepts required supported forms and returns error text for invalid ones', () => {
  assert.equal(validateWebhookRuntimeJSONPath('$.ref'), '')
  assert.equal(validateWebhookRuntimeJSONPath('$.object_attributes.source_branch'), '')
  assert.equal(validateWebhookRuntimeJSONPath(`$['ref']`), '')
  assert.equal(validateWebhookRuntimeJSONPath(`$["object_attributes"]["source_branch"]`), '')
  assert.equal(validateWebhookRuntimeJSONPath('$.refs[0]'), '')
  assert.equal(validateWebhookRuntimeJSONPath('$.refs[*]'), '')

  assert.notEqual(validateWebhookRuntimeJSONPath(''), '')
  assert.notEqual(validateWebhookRuntimeJSONPath('ref'), '')
  assert.notEqual(validateWebhookRuntimeJSONPath('$.refs[]'), '')
  assert.notEqual(validateWebhookRuntimeJSONPath('$.object_attributes.'), '')
  assert.notEqual(validateWebhookRuntimeJSONPath('$..ref'), '')
  assert.notEqual(validateWebhookRuntimeJSONPath('$[ref]'), '')
})

test('createRunInputs clones arrays and object values for rerun capable params', () => {
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
      advanced: { retries: 2 },
      targets: ['prod']
    }
  })
  assert.notStrictEqual(inputs.node_1.targets, sourceArray)
  assert.notStrictEqual(inputs.node_1.advanced, sourceObject)
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
