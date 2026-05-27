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
  if (fieldType === 'text' || fieldType === 'string') return uiComponent === 'textarea' ? 'textarea' : 'text'
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

const isStringLikeFieldType = (fieldType = '') => fieldType === 'text' || fieldType === 'string' || fieldType === 'select'

const inferRuntimeValueType = (fieldDefinition = null, inputType = 'text') => {
  const fieldType = String(fieldDefinition?.type || '').toLowerCase()

  if (fieldType === 'boolean' || inputType === 'boolean') return 'boolean'
  if (fieldType === 'number' || inputType === 'number') return 'number'
  if (isStringLikeFieldType(fieldType)) return 'string'
  if (!fieldType && inputType === 'text') return 'string'
  return ''
}

const buildRuntimeSettableParam = (param, fieldDefinition = null) => {
  const inputType = fieldDefinition
    ? inferFieldInputType(fieldDefinition)
    : inferSavedValueInputType(param?.value)
  const fieldType = String(fieldDefinition?.type || '').toLowerCase() || (inputType === 'boolean'
    ? 'boolean'
    : inputType === 'number'
      ? 'number'
      : 'text')

  return {
    key: param.key,
    label: fieldDefinition?.label || param.label || param.key,
    value: param.value,
    default_value: param.value,
    field_type: fieldType,
    input_type: inputType,
    runtime_value_type: inferRuntimeValueType(fieldDefinition, inputType),
    is_string_like: isStringLikeFieldType(fieldType),
    placeholder: fieldDefinition?.ui_placeholder || fieldDefinition?.placeholder || param.placeholder || '',
    options: inputType === 'select' || inputType === 'checkbox_group'
      ? normalizeFieldOptions(fieldDefinition?.options)
      : []
  }
}

const extractRuntimeSettableNodes = (pipelineConfig, taskDefinitions = [], options = {}) => {
  if (!pipelineConfig || !Array.isArray(pipelineConfig.nodes)) return []

  const taskDefinitionMap = buildTaskDefinitionMap(taskDefinitions)
  const requireSchema = options.requireSchema === true

  return pipelineConfig.nodes
    .map((node, index) => {
      const nodeID = node.node_id || node.id
      if (!nodeID) return null

      const taskDefinition = resolveTaskDefinition(taskDefinitionMap, node)
      const fieldDefinitionMap = buildFieldDefinitionMap(taskDefinition?.fields_schema)

      const flexibleParams = Array.isArray(node.params)
        ? node.params.filter((param) => {
          if (!param || !param.key || param.is_flexible !== true) return false
          if (fieldDefinitionMap.size === 0) return requireSchema !== true
          return fieldDefinitionMap.has(param.key)
        })
        : []

      if (flexibleParams.length === 0) return null

      return {
        node_id: nodeID,
        node_index: index + 1,
        node_name: node.node_name || node.name || nodeID,
        params: flexibleParams.map((param) => buildRuntimeSettableParam(param, fieldDefinitionMap.get(param.key) || null))
      }
    })
    .filter(Boolean)
}

export const extractManualRunNodes = (pipelineConfig, taskDefinitions = []) => {
  return extractRuntimeSettableNodes(pipelineConfig, taskDefinitions)
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

export const extractWebhookRuntimeInputTargets = (pipelineConfig, taskDefinitions = []) => {
  return extractRuntimeSettableNodes(pipelineConfig, taskDefinitions)
    .flatMap((node) => node.params
      .filter((param) => Boolean(param.runtime_value_type))
      .map((param) => ({
        target_key: `${node.node_id}.${param.key}`,
        node_id: node.node_id,
        node_index: node.node_index,
        node_name: node.node_name,
        param_key: param.key,
        param_label: param.label,
        default_value: param.default_value,
        field_type: param.field_type,
        input_type: param.input_type,
        runtime_value_type: param.runtime_value_type,
        is_string_like: param.is_string_like,
        placeholder: param.placeholder,
        options: param.options
      })))
}

export const getWebhookRuntimeInputTargets = (pipelineRecord) => {
  return extractWebhookRuntimeInputTargets(getPipelineDefinition(pipelineRecord), getTaskDefinitionsFromRecord(pipelineRecord))
}

const cloneRunInputValue = (value, fallback = undefined) => {
  if (Array.isArray(value)) return [...value]
  if (value && typeof value === 'object') return { ...value }
  return value ?? fallback
}

export const createRunInputs = (manualRunNodes = []) => {
  const inputs = {}

  manualRunNodes.forEach((node) => {
    inputs[node.node_id] = {}
    node.params.forEach((param) => {
      inputs[node.node_id][param.key] = cloneRunInputValue(param.value, '')
    })
  })

  return inputs
}

const normalizeParamRecord = (value) => {
  if (Array.isArray(value)) {
    return value.reduce((acc, item) => {
      const key = String(item?.key || '').trim()
      if (!key) return acc
      acc[key] = item?.value
      return acc
    }, {})
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

const normalizeWebhookRuntimeMappingTarget = (target) => ({
  node_id: String(target?.node_id || '').trim(),
  param_key: String(target?.param_key || '').trim()
})

const getWebhookRuntimeTargetKey = (target) => {
  const normalizedTarget = normalizeWebhookRuntimeMappingTarget(target)
  if (!normalizedTarget.node_id || !normalizedTarget.param_key) return ''
  return `${normalizedTarget.node_id}.${normalizedTarget.param_key}`
}

const normalizeWebhookRuntimeEditorRowMeta = (value) => {
  const data = value && typeof value === 'object' && !Array.isArray(value) ? value : {}
  return {
    target_key: String(data.target_key || '').trim(),
    node_id: String(data.node_id || '').trim(),
    node_index: Number.isInteger(data.node_index) ? data.node_index : -1,
    node_name: String(data.node_name || '').trim(),
    param_key: String(data.param_key || '').trim(),
    param_label: String(data.param_label || '').trim(),
    runtime_value_type: String(data.runtime_value_type || '').trim()
  }
}

const buildWebhookRuntimeEditorRow = (row, target = null) => {
  const normalizedRow = createWebhookRuntimeInputMappingRow(row)
  const normalizedTarget = normalizeWebhookRuntimeEditorRowMeta(target || row)
  const targetKey = normalizedTarget.target_key || getWebhookRuntimeTargetKey(normalizedRow.target)

  return {
    ...normalizedRow,
    target_key: targetKey,
    node_id: normalizedTarget.node_id || normalizedRow.target.node_id,
    node_index: normalizedTarget.node_index,
    node_name: normalizedTarget.node_name,
    param_key: normalizedTarget.param_key || normalizedRow.target.param_key,
    param_label: normalizedTarget.param_label,
    runtime_value_type: normalizedTarget.runtime_value_type
  }
}

export const createWebhookRuntimeInputMappingRow = (mapping = {}, index = 0) => {
  const data = mapping && typeof mapping === 'object' && !Array.isArray(mapping) ? mapping : {}

  return {
    id: String(data.id || '').trim() || `rule-${index + 1}`,
    source_type: String(data.source_type || '').trim() || 'jsonpath',
    source_expr: String(data.source_expr || '').trim(),
    target: normalizeWebhookRuntimeMappingTarget(data.target),
    missing_policy: String(data.missing_policy || '').trim() || 'ignore',
    deleted: data.deleted === true
  }
}

const normalizeWebhookRuntimeMappingRow = (mapping, index) => createWebhookRuntimeInputMappingRow(mapping, index)

export const buildWebhookRuntimeMappingEditorRows = (targets = [], mappings = []) => {
  const normalizedTargets = Array.isArray(targets) ? targets : []
  const normalizedMappings = Array.isArray(mappings) ? mappings : normalizeWebhookRuntimeInputMappings(mappings)
  const mappingByTargetKey = new Map(normalizedMappings.map((mapping, index) => {
    const normalizedRow = createWebhookRuntimeInputMappingRow(mapping, index)
    return [getWebhookRuntimeTargetKey(normalizedRow.target), normalizedRow]
  }).filter(([targetKey]) => Boolean(targetKey)))

  return normalizedTargets.map((target, index) => {
    const targetMeta = normalizeWebhookRuntimeEditorRowMeta(target)
    const matchedRow = mappingByTargetKey.get(targetMeta.target_key)
    const row = matchedRow || createWebhookRuntimeInputMappingRow({
      id: `rule-${index + 1}`,
      target: {
        node_id: targetMeta.node_id,
        param_key: targetMeta.param_key
      },
      deleted: true
    }, index)

    return buildWebhookRuntimeEditorRow({
      ...row,
      target: {
        node_id: targetMeta.node_id,
        param_key: targetMeta.param_key
      },
      deleted: matchedRow ? matchedRow.deleted === true : true
    }, targetMeta)
  })
}

export const serializeWebhookRuntimeInputMappings = (value) => {
  const rows = Array.isArray(value) ? value : []

  return JSON.stringify(rows
    .map((row, index) => createWebhookRuntimeInputMappingRow(row, index))
    .filter((row) => row.deleted !== true)
    .filter((row) => row.source_type && row.source_expr && row.target.node_id && row.target.param_key)
    .map((row) => ({
      id: row.id,
      source_type: row.source_type,
      source_expr: row.source_expr,
      target: row.target,
      missing_policy: row.missing_policy
    })))
}

const gitlabWebhookRuntimePresetDefinitions = [
  {
    param_keys: ['git_ref', 'ref', 'branch', 'branch_name'],
    runtime_value_type: 'string',
    source_expr: '$.ref'
  },
  {
    param_keys: ['source_branch'],
    runtime_value_type: 'string',
    source_expr: '$.object_attributes.source_branch'
  },
  {
    param_keys: ['target_branch'],
    runtime_value_type: 'string',
    source_expr: '$.object_attributes.target_branch'
  },
  {
    param_keys: ['commit_sha', 'git_commit', 'sha'],
    runtime_value_type: 'string',
    source_expr: '$.checkout_sha'
  }
]

const matchGitlabWebhookRuntimePreset = (value) => {
  const paramKey = String(value?.param_key || '').trim()
  const runtimeValueType = String(value?.runtime_value_type || '').trim()

  return gitlabWebhookRuntimePresetDefinitions.find((preset) => {
    return preset.param_keys.includes(paramKey) && runtimeValueType === preset.runtime_value_type
  }) || null
}

export const buildGitlabWebhookRuntimePresetMappings = (targets = []) => {
  const availableTargets = Array.isArray(targets) ? targets : []

  return gitlabWebhookRuntimePresetDefinitions.reduce((acc, preset) => {
    const matchingTargets = availableTargets.filter((item) => {
      const paramKey = String(item?.param_key || '').trim()
      return preset.param_keys.includes(paramKey) && String(item?.runtime_value_type || '').trim() === preset.runtime_value_type
    })

    matchingTargets.forEach((target) => {
      acc.push(createWebhookRuntimeInputMappingRow({
        id: `rule-${acc.length + 1}`,
        source_type: 'jsonpath',
        source_expr: preset.source_expr,
        target: {
          node_id: target.node_id,
          param_key: target.param_key
        },
        missing_policy: 'ignore',
        deleted: false
      }, acc.length))
    })

    return acc
  }, [])
}

export const applyGitlabWebhookRuntimePresetToRows = (rows = []) => {
  const normalizedRows = Array.isArray(rows) ? rows : []

  return normalizedRows.map((row) => {
    const normalizedRow = buildWebhookRuntimeEditorRow(row)
    const preset = matchGitlabWebhookRuntimePreset(normalizedRow)
    if (!preset) return normalizedRow

    return buildWebhookRuntimeEditorRow({
      ...normalizedRow,
      source_type: 'jsonpath',
      source_expr: preset.source_expr,
      missing_policy: 'ignore',
      deleted: false
    }, normalizedRow)
  })
}

const webhookRuntimeJSONPathDotSegmentPattern = '[A-Za-z_][A-Za-z0-9_]*'
const webhookRuntimeJSONPathBracketSegmentPattern = '\\[(?:"[A-Za-z_][A-Za-z0-9_]*"|\'[A-Za-z_][A-Za-z0-9_]*\')\\]'
const webhookRuntimeJSONPathIndexPattern = '\\[(?:\\d+|\\*)\\]'
const webhookRuntimeJSONPathPattern = new RegExp(
  `^\\$(?:\\.${webhookRuntimeJSONPathDotSegmentPattern}|${webhookRuntimeJSONPathBracketSegmentPattern})+(?:${webhookRuntimeJSONPathIndexPattern})?$`
)

export const validateWebhookRuntimeJSONPath = (value) => {
  const normalizedValue = String(value || '').trim()
  if (!normalizedValue) return 'JSONPath 不能为空'
  if (webhookRuntimeJSONPathPattern.test(normalizedValue)) return ''
  return '仅支持当前示例使用的 JSONPath 格式'
}

export const normalizeWebhookRuntimeInputMappings = (value) => {
  const parsed = Array.isArray(value)
    ? value
    : parseJSONField(value, [])

  if (!Array.isArray(parsed)) return []

  return parsed.map((mapping, index) => normalizeWebhookRuntimeMappingRow(mapping, index))
}

export const normalizeWebhookRuntimeTriggerConfig = (payload) => {
  const data = payload && typeof payload === 'object' && !Array.isArray(payload) ? payload : {}
  const rawMappings = typeof data.webhook_runtime_input_mappings === 'string'
    ? data.webhook_runtime_input_mappings
    : Array.isArray(data.webhook_runtime_input_mappings)
      ? JSON.stringify(data.webhook_runtime_input_mappings)
      : ''

  return {
    webhook_runtime_input_mappings: rawMappings,
    mapping_rows: normalizeWebhookRuntimeInputMappings(rawMappings),
    webhook_config_status: typeof data.webhook_config_status === 'string' ? data.webhook_config_status : '',
    webhook_config_invalid_reason: typeof data.webhook_config_invalid_reason === 'string' ? data.webhook_config_invalid_reason : ''
  }
}

export const normalizeWebhookRuntimeStructuredErrors = (errors) => {
  if (!Array.isArray(errors)) return []

  return errors
    .map((item) => {
      const data = item && typeof item === 'object' && !Array.isArray(item) ? item : {}
      const mappingID = String(data.mapping_id || data.mappingID || '').trim()
      const field = String(data.field || '').trim()
      const code = String(data.code || '').trim()
      const message = String(data.message || '').trim()

      if (!mappingID && !field && !code && !message) return null

      return {
        mapping_id: mappingID,
        field,
        code,
        message
      }
    })
    .filter(Boolean)
}

const normalizeWebhookRuntimePreviewValues = (values) => {
  if (!values || typeof values !== 'object' || Array.isArray(values)) return {}

  return Object.entries(values).reduce((acc, [nodeID, nodeValues]) => {
    if (!nodeValues || typeof nodeValues !== 'object' || Array.isArray(nodeValues)) return acc
    acc[nodeID] = nodeValues
    return acc
  }, {})
}

const normalizeWebhookRuntimePreviewRuleResults = (ruleResults) => {
  if (!ruleResults || typeof ruleResults !== 'object' || Array.isArray(ruleResults)) return {}

  return Object.entries(ruleResults).reduce((acc, [ruleID, result]) => {
    const data = result && typeof result === 'object' && !Array.isArray(result) ? result : {}
    acc[ruleID] = {
      code: String(data.code || '').trim(),
      value: data.value
    }
    return acc
  }, {})
}

export const normalizeWebhookRuntimePreviewPayload = (payload) => {
  const envelope = payload && typeof payload === 'object' && !Array.isArray(payload) ? payload : {}
  const directData = envelope.data && typeof envelope.data === 'object' && !Array.isArray(envelope.data)
    ? envelope.data
    : envelope

  const summary = directData.summary && typeof directData.summary === 'object' && !Array.isArray(directData.summary)
    ? directData.summary
    : {}

  return {
    values: normalizeWebhookRuntimePreviewValues(directData.values),
    rule_results: normalizeWebhookRuntimePreviewRuleResults(directData.rule_results),
    summary: {
      total: Number.isFinite(Number(summary.total)) ? Number(summary.total) : 0,
      matched: Number.isFinite(Number(summary.matched)) ? Number(summary.matched) : 0,
      missing: Number.isFinite(Number(summary.missing)) ? Number(summary.missing) : 0,
      failed: Number.isFinite(Number(summary.failed)) ? Number(summary.failed) : 0
    },
    errors: normalizeWebhookRuntimeStructuredErrors(envelope.errors)
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
      inputs[node.node_id][param.key] = cloneRunInputValue(value)
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
