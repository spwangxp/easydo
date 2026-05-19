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

const normalizeTaskIdentity = (value) => String(value || '').trim().toLowerCase()
const normalizeTaskVersion = (value) => {
  const numericValue = Number(value)
  return Number.isFinite(numericValue) && numericValue > 0 ? numericValue : null
}

const normalizeFieldOptions = (options) => {
  if (!Array.isArray(options)) return []

  return options.map((item) => {
    if (item && typeof item === 'object') {
      const value = item.value ?? item.key ?? item.id ?? item.label
      return {
        label: String(item.label ?? value ?? ''),
        value
      }
    }

    return {
      label: String(item ?? ''),
      value: item
    }
  })
}

const inferFieldInputType = (field = {}) => {
  const fieldType = String(field.type || '').toLowerCase()
  const uiComponent = String(field.ui_component || '').toLowerCase()

  if (fieldType === 'boolean') return 'boolean'
  if (fieldType === 'number') return 'number'
  if (fieldType === 'text') return uiComponent === 'textarea' ? 'textarea' : 'text'
  if (fieldType === 'select' || uiComponent === 'select') return 'select'
  if (fieldType === 'multiselect' || fieldType === 'checkbox_group' || uiComponent === 'checkbox_group') return 'checkbox_group'
  if (fieldType === 'json' || fieldType === 'object' || fieldType === 'array') return 'textarea'
  if (uiComponent === 'switch') return 'boolean'
  if (uiComponent === 'textarea') return 'textarea'

  return 'text'
}

const inferSavedValueInputType = (value) => {
  if (typeof value === 'boolean') return 'boolean'
  if (typeof value === 'number' && Number.isFinite(value)) return 'number'
  if (Array.isArray(value)) return 'checkbox_group'
  if (value && typeof value === 'object') return 'textarea'
  return 'text'
}

const buildTaskDefinitionMap = (taskDefinitions = []) => {
  const definitions = Array.isArray(taskDefinitions) ? taskDefinitions : []
  const definitionMap = new Map()

  definitions.forEach((definition) => {
    const key = normalizeTaskIdentity(definition?.task_key || definition?.type || definition?.name)
    if (!key) return

    const version = normalizeTaskVersion(definition?.task_version ?? definition?.version)
    if (!definitionMap.has(key)) {
      definitionMap.set(key, {
        defaultDefinition: definition,
        versions: new Map()
      })
    }

    const entry = definitionMap.get(key)
    if (!entry.defaultDefinition) {
      entry.defaultDefinition = definition
    }
    if (version !== null) {
      entry.versions.set(version, definition)
    }
  })

  return definitionMap
}

const resolveTaskDefinition = (taskDefinitionMap, node = {}) => {
  const identity = normalizeTaskIdentity(node.task_key || node.type || node.task_type || node.name || node.node_name)
  if (!identity) return null

  const entry = taskDefinitionMap.get(identity)
  if (!entry) return null

  const version = normalizeTaskVersion(node.task_version ?? node.version)
  if (version !== null && entry.versions.has(version)) {
    return entry.versions.get(version) || null
  }

  return entry.defaultDefinition || null
}

const buildFieldDefinitionMap = (fieldsSchema = []) => {
  const fieldMap = new Map()

  if (!Array.isArray(fieldsSchema)) return fieldMap

  fieldsSchema.forEach((field) => {
    if (!field?.key) return
    fieldMap.set(field.key, field)
  })

  return fieldMap
}

const buildManualRunParam = (param, fieldDefinition = null) => {
  const inputType = fieldDefinition
    ? inferFieldInputType(fieldDefinition)
    : inferSavedValueInputType(param?.value)

  return {
    key: param.key,
    label: fieldDefinition?.label || param.label || param.key,
    value: param.value,
    input_type: inputType,
    placeholder: fieldDefinition?.ui_placeholder || fieldDefinition?.placeholder || param.placeholder || '',
    options: inputType === 'select' || inputType === 'checkbox_group'
      ? normalizeFieldOptions(fieldDefinition?.options)
      : []
  }
}

export const extractManualRunNodes = (pipelineConfig, taskDefinitions = []) => {
  if (!pipelineConfig || !Array.isArray(pipelineConfig.nodes)) return []

  const taskDefinitionMap = buildTaskDefinitionMap(taskDefinitions)

  return pipelineConfig.nodes
    .map((node) => {
      const nodeID = node.node_id || node.id
      if (!nodeID) return null

      const flexibleParams = Array.isArray(node.params)
        ? node.params.filter((param) => param && param.key && param.is_flexible === true)
        : []

      if (flexibleParams.length === 0) return null

      const taskDefinition = resolveTaskDefinition(taskDefinitionMap, node)
      const fieldDefinitionMap = buildFieldDefinitionMap(taskDefinition?.fields_schema)

      return {
        node_id: nodeID,
        node_name: node.node_name || node.name || nodeID,
        params: flexibleParams.map((param) => buildManualRunParam(param, fieldDefinitionMap.get(param.key) || null))
      }
    })
    .filter(Boolean)
}

const getTaskDefinitionsFromRecord = (pipelineRecord) => {
  const directDefinitions = parseJSONField(pipelineRecord?.task_definitions, null)
  if (Array.isArray(directDefinitions)) return directDefinitions

  const jsonDefinitions = parseJSONField(pipelineRecord?.task_definitions_json, null)
  if (Array.isArray(jsonDefinitions)) return jsonDefinitions

  return []
}

export const getManualRunNodes = (pipelineRecord) => {
  return extractManualRunNodes(getPipelineDefinition(pipelineRecord), getTaskDefinitionsFromRecord(pipelineRecord))
}

export const createRunInputs = (manualRunNodes = []) => {
  const inputs = {}

  manualRunNodes.forEach((node) => {
    inputs[node.node_id] = {}
    node.params.forEach((param) => {
      const value = param.value
      inputs[node.node_id][param.key] = Array.isArray(value) ? [...value] : (value ?? '')
    })
  })

  return inputs
}

const normalizeParamRecord = (value) => {
  if (Array.isArray(value)) {
    const result = {}

    value.forEach((item) => {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return
      const key = String(item.key || '').trim()
      if (!key) return
      result[key] = item.value
    })

    return result
  }

  if (!value || typeof value !== 'object') return {}
  return value
}

export const normalizeRunParameterViewPayload = (payload) => {
  const data = payload && typeof payload === 'object' && !Array.isArray(payload) ? payload : {}
  const nodes = Array.isArray(data.nodes) ? data.nodes : []

  return {
    nodes: nodes
      .map((node, index) => {
        const nodeID = String(node?.node_id || node?.id || '').trim()
        if (!nodeID) return null
        return {
          node_id: nodeID,
          node_name: String(node?.node_name || node?.name || nodeID || `节点 ${index + 1}`),
          runtime_params: normalizeParamRecord(node?.runtime_params),
          default_params: normalizeParamRecord(node?.default_params)
        }
      })
      .filter(Boolean)
  }
}

export const normalizeRerunPreviewPayload = (payload) => {
  const data = payload && typeof payload === 'object' && !Array.isArray(payload) ? payload : {}
  const prefillInputs = data.prefill_inputs && typeof data.prefill_inputs === 'object' && !Array.isArray(data.prefill_inputs)
    ? data.prefill_inputs
    : {}

  return {
    can_enter_run_dialog: data.can_enter_run_dialog === true,
    match_key: typeof data.match_key === 'string' ? data.match_key : '',
    matched: Array.isArray(data.matched) ? data.matched : [],
    mismatched: Array.isArray(data.mismatched) ? data.mismatched : [],
    prefill_inputs: prefillInputs,
    failure: data.failure && typeof data.failure === 'object' && !Array.isArray(data.failure) ? data.failure : null
  }
}

export const createRunInputsFromRerunPreview = (manualRunNodes = [], rerunPreview = null) => {
  const inputs = createRunInputs(manualRunNodes)
  const normalizedPreview = normalizeRerunPreviewPayload(rerunPreview)

  manualRunNodes.forEach((node) => {
    const prefillNodeInputs = normalizedPreview.prefill_inputs[node.node_id]
    if (!prefillNodeInputs || typeof prefillNodeInputs !== 'object' || Array.isArray(prefillNodeInputs)) return

    node.params.forEach((param) => {
      if (!Object.prototype.hasOwnProperty.call(prefillNodeInputs, param.key)) return
      const value = prefillNodeInputs[param.key]
      if (value === undefined || value === null) return
      inputs[node.node_id][param.key] = Array.isArray(value) ? [...value] : value
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
