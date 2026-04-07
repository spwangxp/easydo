export const parseJSONField = (value, fallback) => {
  if (value === null || value === undefined || value === '') return fallback
  if (typeof value === 'object') return value
  if (typeof value !== 'string') return fallback

  try {
    return JSON.parse(value)
  } catch {
    return fallback
  }
}

export const getPipelineDefinition = (pipelineRecord) => (
  parseJSONField(pipelineRecord?.definition_json, null) ||
  parseJSONField(pipelineRecord?.config, null)
)

const inferFieldInputType = (field = {}) => {
  const fieldType = String(field.type || '').toLowerCase()
  if (fieldType === 'boolean') return 'boolean'
  if (fieldType === 'number') return 'number'
  return 'text'
}

export const extractManualRunNodes = (pipelineConfig) => {
  if (!pipelineConfig || !Array.isArray(pipelineConfig.nodes)) return []

  return pipelineConfig.nodes
    .map((node) => {
      const nodeID = node.node_id || node.id
      if (!nodeID) return null

      const flexibleParams = Array.isArray(node.params)
        ? node.params.filter((param) => param && param.key && param.is_flexible === true)
        : []

      if (flexibleParams.length === 0) return null

      return {
        node_id: nodeID,
        node_name: node.node_name || node.name || nodeID,
        params: flexibleParams.map((param) => ({
          key: param.key,
          label: param.label || param.key,
          value: param.value,
          input_type: inferFieldInputType(param),
          placeholder: param.placeholder || ''
        }))
      }
    })
    .filter(Boolean)
}

export const getManualRunNodes = (pipelineRecord) => (
  extractManualRunNodes(getPipelineDefinition(pipelineRecord))
)

export const createRunInputs = (manualRunNodes = []) => {
  const inputs = {}

  manualRunNodes.forEach((node) => {
    inputs[node.node_id] = {}
    node.params.forEach((param) => {
      inputs[node.node_id][param.key] = param.value ?? ''
    })
  })

  return inputs
}

export const buildRunInputsPayload = (manualRunNodes = [], currentInputs = {}) => {
  const inputs = {}

  manualRunNodes.forEach((node) => {
    const nodeInputs = currentInputs[node.node_id] || {}
    const current = {}

    node.params.forEach((param) => {
      const value = nodeInputs[param.key]
      if (value === '' || value === null || value === undefined) return
      current[param.key] = value
    })

    if (Object.keys(current).length > 0) {
      inputs[node.node_id] = current
    }
  })

  return { inputs }
}
