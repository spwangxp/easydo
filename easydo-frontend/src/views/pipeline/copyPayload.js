const WEBHOOK_TRIGGER_TYPE = 'webhook'

const isPlainObject = (value) => Boolean(value) && typeof value === 'object' && !Array.isArray(value)

const parseDefinitionJson = (definitionJson) => {
  if (typeof definitionJson !== 'string' || definitionJson.trim() === '') {
    throw new Error('definition_json is required')
  }

  let parsed
  try {
    parsed = JSON.parse(definitionJson)
  } catch {
    throw new Error('definition_json must be valid JSON')
  }

  if (!isPlainObject(parsed)) {
    throw new Error('definition_json must be a JSON object')
  }

  return parsed
}

const normalizeTriggers = (definition) => {
  if (!('triggers' in definition)) return definition

  if (!Array.isArray(definition.triggers)) {
    throw new Error('definition_json.triggers must be an array when present')
  }

  return {
    ...definition,
    triggers: definition.triggers.filter((trigger) => trigger?.type !== WEBHOOK_TRIGGER_TYPE)
  }
}

const getSourcePipelineName = (sourcePipeline) => {
  const name = sourcePipeline?.name

  if (typeof name !== 'string' || name.trim() === '') {
    throw new Error('source pipeline name is required')
  }

  return name
}

export const buildPipelineCopyPayload = (sourcePipeline) => {
  const definitionJson = sourcePipeline?.definition_json
  const definition = normalizeTriggers(parseDefinitionJson(definitionJson))
  const name = getSourcePipelineName(sourcePipeline)

  return {
    name: `${name}-copy`,
    description: sourcePipeline?.description,
    project_id: sourcePipeline?.project_id,
    environment: sourcePipeline?.environment,
    definition_json: JSON.stringify(definition)
  }
}
