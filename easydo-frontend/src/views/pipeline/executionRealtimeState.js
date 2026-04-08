const parseJSONRecord = (value) => {
  if (!value) return null
  if (typeof value === 'object' && !Array.isArray(value)) {
    return value
  }
  if (typeof value !== 'string') return null

  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null
  } catch {
    return null
  }
}

export const buildTaskOutputsFromPayload = (payload = {}) => {
  const directOutputs = parseJSONRecord(payload.outputs)
  if (directOutputs) return directOutputs

  const resultOutputs = parseJSONRecord(payload.result_data ?? payload.resultData)
  if (resultOutputs) return resultOutputs

  return null
}

export const normalizeExecutionTaskOutputs = (task = {}) => {
  const outputs = buildTaskOutputsFromPayload(task)
  if (!outputs) return task

  return {
    ...task,
    outputs
  }
}

const mergeTaskRecord = (task, payload) => {
  const outputs = buildTaskOutputsFromPayload(payload)

  if (payload.task_id && (!task.id || task.id === 0)) {
    task.id = payload.task_id
  }
  if (payload.node_id) {
    task.node_id = payload.node_id
  }
  if (!task.created_at && payload.timestamp) {
    task.created_at = payload.timestamp
  }

  task.status = payload.status
  if (payload.status) {
    task.display_status = payload.status
  }
  if (!task.name && (payload.name || payload.task_name || payload.node_name)) {
    task.name = payload.name || payload.task_name || payload.node_name
  }
  task.exit_code = payload.exit_code
  task.error_msg = payload.error_msg
  task.duration = payload.duration
  if (payload.start_time !== undefined && payload.start_time !== null) {
    task.start_time = payload.start_time
  } else if (payload.status === 'running' && !task.start_time && payload.timestamp) {
    task.start_time = payload.timestamp
  } else if (payload.status === 'queued' && payload.retrying) {
    task.start_time = 0
  }
  if (payload.agent_name) {
    task.Agent = { name: payload.agent_name }
  }
  if (outputs) {
    task.outputs = outputs
  }

  return task
}

export const applyTaskStatusPayload = (tasks = [], payload = {}) => {
  const nextTasks = Array.isArray(tasks) ? tasks.slice() : []

  let taskIndex = -1
  if (payload.task_id) {
    taskIndex = nextTasks.findIndex((task) => Number(task.id || 0) === Number(payload.task_id))
  }
  if (taskIndex === -1 && payload.node_id) {
    taskIndex = nextTasks.findIndex((task) => (task?.node_id || task?.NodeID || '') === payload.node_id)
  }

  if (taskIndex !== -1) {
    const task = { ...nextTasks[taskIndex] }
    nextTasks[taskIndex] = mergeTaskRecord(task, payload)
    return nextTasks
  }

  if (!payload.node_id) {
    return nextTasks
  }

  const newTask = mergeTaskRecord({
    id: payload.task_id || 0,
    node_id: payload.node_id,
    name: payload.name || payload.task_name || payload.node_name || payload.node_id,
    status: payload.status || 'queued',
    display_status: payload.status || 'queued',
    start_time: payload.start_time || 0,
    duration: payload.duration || 0,
    error_msg: payload.error_msg || '',
    created_at: payload.timestamp || 0,
    outputs: {},
    Agent: payload.agent_name ? { name: payload.agent_name } : null
  }, payload)

  nextTasks.push(newTask)
  return nextTasks
}
