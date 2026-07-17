export const aiStoreSections = [
  { key: 'providers', title: 'AI Providers' },
  { key: 'deployments', title: 'AI Deployments' }
]

export function normalizeAiStoreState(payload = {}) {
  const models = normalizeArray(payload.models).map((model) => ({
    ...model,
    modalities: normalizeModalities(model.modalities, model.modalitiesText)
  }))

  const providers = normalizeArray(payload.providers).map((provider) => {
    const bindings = normalizeBindings(
      provider.model_bindings,
      provider.bindings,
      payload.modelBindingsByProvider?.[provider.id]
    )

    return {
      ...provider,
      bindings,
      bindingCount: bindings.length
    }
  })

  return {
    models,
    providers,
    deployments: normalizeArray(payload.deployments)
  }
}

export function buildAiStoreSummary(payload = {}) {
  const state = normalizeAiStoreState(payload)
  const activeBindings = state.providers.reduce((count, provider) => count + provider.bindingCount, 0)

  return [
    { key: 'providers', label: 'AI Providers', value: state.providers.length, tone: 'primary' },
    { key: 'bindings', label: 'Model Bindings', value: activeBindings, tone: 'warning' },
    { key: 'deployments', label: 'AI Deployments', value: state.deployments.length, tone: 'success' }
  ]
}

export function buildModelRows({ models = [], providers = [], deployments = [], keyword = '' } = {}) {
  const state = normalizeAiStoreState({ models, providers, deployments })
  const normalizedKeyword = String(keyword).trim().toLowerCase()

  return state.models
    .map((model) => {
      const modelProviders = state.providers.filter((provider) => {
        if (String(provider.model_id) === String(model.id)) return true
        return provider.bindings.some((binding) => String(binding.model_id) === String(model.id))
      })
      const modelDeployments = state.deployments.filter((deployment) => String(deployment.model_id) === String(model.id))

      const providerRows = modelProviders.map((provider) => {
        const providerBindings = provider.bindings.filter((binding) => String(binding.model_id) === String(model.id))
        return buildProviderRow(provider, providerBindings)
      })

      const row = {
        id: model.id,
        name: model.name || '',
        parameterSize: model.parameter_size || model.parameterSize || '',
        modalitiesText: formatModalities(model.modalities),
        source: model.source || '',
        deploymentCount: modelDeployments.length,
        providerCount: modelProviders.length,
        providers: providerRows,
        deployments: modelDeployments,
        searchText: [
          model.name,
          model.parameter_size,
          formatModalities(model.modalities),
          model.source,
          ...providerRows.flatMap((provider) => [provider.name, provider.source, provider.endpoint, provider.status, provider.binding_key]),
          ...modelDeployments.flatMap((deployment) => [deployment.resource_name, deployment.template_name, deployment.version_label, deployment.provider_name])
        ]
          .filter(Boolean)
          .join(' ')
          .toLowerCase()
      }

      return row
    })
    .filter((row) => {
      if (!normalizedKeyword) return true
      return normalizedKeyword.split(/\s+/).every((term) => row.searchText.includes(term))
    })
}

export function buildProviderRows({ providers = [], keyword = '' } = {}) {
  const normalizedKeyword = String(keyword).trim().toLowerCase()

  return normalizeAiStoreState({ providers }).providers.filter((provider) => {
    if (!normalizedKeyword) return true
    const bindingText = provider.bindings
      .map((binding) => `${binding.model_name || ''} ${binding.provider_model_key || ''}`)
      .join(' ')

    return [
      provider.name,
      provider.description,
      provider.base_url,
      bindingText,
      provider.status
    ].join(' ').toLowerCase().includes(normalizedKeyword)
  })
}

export function buildDiscoveredProviderModelRows({ candidates = [], models = [] } = {}) {
  return normalizeArray(candidates).map((candidate, index) => {
    const providerModelKey = String(candidate.provider_model_key || candidate.id || '').trim()
    const modelName = String(candidate.model_name || candidate.name || candidate.provider_display_name || providerModelKey).trim()
    const matchedModel = findMatchingModelForCandidate(candidate, models)
    return {
      local_id: `discovered-${index}-${providerModelKey || modelName}`,
      selected: candidate.recommended === true || index === 0,
      provider_model_key: providerModelKey,
      provider_display_name: String(candidate.provider_display_name || modelName || providerModelKey).trim(),
      model_name: modelName || providerModelKey,
      model_kind: candidate.model_kind || 'chat',
      model_family: candidate.model_family || '',
      source_model_id: String(candidate.source_model_id || providerModelKey || modelName).trim(),
      target_mode: matchedModel ? 'existing' : 'create',
      model_id: matchedModel?.id ?? null,
      modalities: normalizeStringList(candidate.modalities),
      capabilities: normalizeStringList(candidate.capabilities),
      context_window: numberOrNull(candidate.context_window),
      max_output_tokens: numberOrNull(candidate.max_output_tokens),
      pricing: candidate.pricing || {},
      raw: candidate.raw || candidate
    }
  })
}

export function filterDiscoveredProviderModelRows({ rows = [], keyword = '' } = {}) {
  const terms = String(keyword || '').trim().toLowerCase().split(/\s+/).filter(Boolean)
  const normalizedRows = normalizeArray(rows)
  if (terms.length === 0) return normalizedRows

  return normalizedRows.filter((row) => {
    const searchText = [
      row.provider_display_name,
      row.provider_model_key,
      row.model_name,
      row.model_kind,
      row.model_family,
      row.source_model_id,
      row.context_window,
      row.max_output_tokens,
      ...normalizeStringList(row.modalities),
      ...normalizeStringList(row.capabilities)
    ]
      .filter((item) => item !== undefined && item !== null && item !== '')
      .join(' ')
      .toLowerCase()

    return terms.every((term) => searchText.includes(term))
  })
}

export function paginateDiscoveredProviderModelRows({ rows = [], page = 1, pageSize = 10 } = {}) {
  const normalizedRows = normalizeArray(rows)
  const safePageSize = Math.max(Number(pageSize) || 10, 1)
  const safePage = Math.max(Number(page) || 1, 1)
  const start = (safePage - 1) * safePageSize
  return normalizedRows.slice(start, start + safePageSize)
}

export function buildDiscoveredModelImportPayload(row = {}) {
  const metadata = {
    provider_model_key: row.provider_model_key,
    provider_display_name: row.provider_display_name,
    model_family: row.model_family,
    model_kind: row.model_kind,
    modalities: row.modalities || [],
    capabilities: row.capabilities || [],
    context_window: row.context_window,
    max_output_tokens: row.max_output_tokens,
    pricing: row.pricing || {},
    raw: row.raw || {}
  }

  return {
    source: 'provider-discovered',
    source_model_id: String(row.source_model_id || row.provider_model_key || row.model_name || '').trim(),
    name: String(row.model_name || row.provider_display_name || row.provider_model_key || '').trim(),
    display_name: String(row.provider_display_name || row.model_name || row.provider_model_key || '').trim(),
    context_window: numberOrNull(row.context_window),
    tags: [...normalizeStringList(row.modalities), ...normalizeStringList(row.capabilities)],
    metadata
  }
}

export function buildDiscoveredModelBindingMetadata(row = {}) {
  const contextWindow = numberOrNull(row.context_window)
  const maxOutputTokens = numberOrNull(row.max_output_tokens)
  const capabilities = normalizeStringList(row.capabilities)
  const supportsToolUse = capabilities.some((item) => ['tool', 'tools', 'function_calling', 'tool_use'].includes(String(item).toLowerCase()))
  return {
    source: 'discovered',
    capability_source: contextWindow ? 'provider_api' : 'manual_override',
    provider_display_name: row.provider_display_name || '',
    model_name: row.model_name || '',
    model_kind: row.model_kind || '',
    model_family: row.model_family || '',
    modalities: normalizeStringList(row.modalities),
    capabilities,
    context_window: contextWindow,
    context_window_tokens: contextWindow,
    max_output_tokens: maxOutputTokens,
    supports_tool_use: supportsToolUse,
    supports_streaming: true,
    pricing: row.pricing || {},
    raw: row.raw || {}
  }
}

export function buildModelBindingCapabilityPayload(row = {}, metadata = {}) {
  const merged = {
    ...buildDiscoveredModelBindingMetadata(row),
    ...(metadata && typeof metadata === 'object' ? metadata : {})
  }
  return {
    context_window_tokens: numberOrNull(merged.context_window_tokens || merged.context_window),
    max_output_tokens: numberOrNull(merged.max_output_tokens),
    supports_tool_use: Boolean(merged.supports_tool_use),
    supports_streaming: merged.supports_streaming !== false,
    capability_source: String(merged.capability_source || (numberOrNull(merged.context_window_tokens || merged.context_window) ? 'provider_api' : 'manual_override')),
    metadata_json: merged
  }
}

export function buildRuntimeBindingHints({ modelId, providers = [] } = {}) {
  if (!modelId) return []

  return normalizeAiStoreState({ providers }).providers
    .flatMap((provider) => provider.bindings
      .filter((binding) => String(binding.model_id) === String(modelId))
      .map((binding) => ({
        bindingId: binding.id,
        providerId: provider.id,
        providerName: provider.name,
        providerModelKey: binding.provider_model_key,
        modelName: binding.model_name || ''
      })))
}

export function buildDemoDeploymentRecord(form = {}, modelRow = {}) {
  const endpoint = String(form.endpoint || form.base_url || '').trim()
  const deploymentName = String(form.deploymentName || form.resource_name || form.name || 'demo-deployment').trim()
  const providerName = String(form.providerName || form.provider_name || modelRow.providers?.[0]?.name || 'Demo Provider').trim()
  const bindingKey = String(
    modelRow.providers?.[0]?.binding_key ||
      modelRow.binding_key ||
      modelRow.providerModelKey ||
      modelRow.provider_model_key ||
      ''
  ).trim()

  return {
    deployment: {
      id: `local-deployment-${deploymentName || modelRow.id || 'demo'}`,
      model_id: modelRow.id ?? null,
      name: deploymentName,
      resource_name: String(form.resourceName || form.resource_name || deploymentName).trim(),
      template_name: String(form.templateName || form.template_name || '').trim(),
      version_label: String(form.versionLabel || form.version_label || '').trim(),
      status: String(form.status || 'running').trim(),
      endpoint,
      provider_name: providerName
    },
    provider: {
      id: `local-provider-${providerName || modelRow.id || 'demo'}`,
      model_id: modelRow.id ?? null,
      name: providerName,
      endpoint,
      base_url: endpoint,
      source: 'demo',
      status: String(form.providerStatus || form.status || 'active').trim() || 'active'
    },
    binding: {
      id: `local-binding-${bindingKey || deploymentName || modelRow.id || 'demo'}`,
      model_id: modelRow.id ?? null,
      provider_model_key: bindingKey,
      binding_key: bindingKey,
      provider_name: providerName,
      resource_name: deploymentName
    }
  }
}

export function buildAIDeploymentRequestPayload({ modelId, templateVersionId, targetResourceId, parameters = {} } = {}) {
  return {
    ai_model_id: modelId,
    template_version_id: templateVersionId,
    target_resource_id: targetResourceId,
    parameters: sanitizeParameters(parameters)
  }
}

export function buildAIModelImportPayload({
  source,
  sourceModelId,
  source_model_id,
  name,
  displayName,
  display_name,
  modelKind,
  parameterSize,
  parameter_size,
  summary,
  license,
  tagsText,
  tags,
  repositoryUrl,
  repository_url,
  revision,
  format,
  recommendedRuntime,
  recommended_runtime,
  credentialId,
  credential_id,
  contextWindow,
  context_window
} = {}) {
  const trimmedName = String(name || '').trim()
  const modelSourceID = String(sourceModelId || source_model_id || trimmedName || '').trim()
  const resolvedContextWindow = numberOrNull(contextWindow || context_window)
  const metadata = {
    model_kind: String(modelKind || '').trim(),
    context_window: resolvedContextWindow,
    repository_url: String(repositoryUrl || repository_url || '').trim(),
    revision: String(revision || '').trim(),
    format: String(format || '').trim(),
    recommended_runtime: String(recommendedRuntime || recommended_runtime || '').trim()
  }
  const resolvedCredentialID = credentialId || credential_id || null
  if (resolvedCredentialID) {
    metadata.credential_id = resolvedCredentialID
  }

  return {
    source: String(source || '').trim(),
    source_model_id: modelSourceID,
    name: trimmedName,
    display_name: String(displayName || display_name || '').trim(),
    parameter_size: String(parameterSize || parameter_size || '').trim(),
    context_window: resolvedContextWindow,
    summary: String(summary || '').trim(),
    license: String(license || '').trim(),
    tags: normalizeStringList(tags || tagsText),
    metadata
  }
}

export function buildDeployParameterState({ fields = [], selectedModel = {} } = {}) {
  const values = {}

  normalizeParameterFields(fields).forEach((field) => {
    values[field.name] = field.default_value ?? ''
  })

  const modelIdentifier = String(
    selectedModel.model_identifier ||
      selectedModel.modelIdentifier ||
      selectedModel.source_model_id ||
      selectedModel.sourceModelId ||
      selectedModel.name ||
      ''
  ).trim()

  if (selectedModel.id != null) values.model_id = selectedModel.id
  if (selectedModel.name) values.model_name = selectedModel.name
  if (modelIdentifier) values.model_identifier = modelIdentifier

  return values
}

export function buildDemoProviderRecord(form = {}, selectedModel = {}) {
  const endpoint = String(form.endpoint || form.base_url || '').trim()
  const providerName = String(form.providerName || form.provider_name || 'Demo Provider').trim()
  const bindModelNow = form.bindModelNow !== false
  const existingProviders = normalizeArray(form.existingProviders || form.providers)
  const warnings = []

  if (
    endpoint &&
    existingProviders.some((provider) => {
      const candidateEndpoint = String(provider.base_url || provider.endpoint || '').trim()
      return candidateEndpoint && candidateEndpoint === endpoint
    })
  ) {
    warnings.push('检测到重复 Endpoint，demo 中仅提示，不阻止保存')
  }

  const provider = {
    id: `local-provider-${providerName || selectedModel.id || 'demo'}`,
    model_id: selectedModel.id ?? null,
    name: providerName,
    endpoint,
    base_url: endpoint,
    source: 'demo',
    status: String(form.status || 'active').trim() || 'active',
    credential_id: form.credentialId || form.credential_id || null,
    headers_json: String(form.headersJSON || form.headers_json || '').trim(),
    settings_json: String(form.settingsJSON || form.settings_json || '').trim()
  }

  const createdBinding = bindModelNow
    ? {
        id: `local-binding-${selectedModel.id || providerName || 'demo'}`,
        model_id: selectedModel.id ?? null,
        provider_id: provider.id,
        provider_model_key: String(
          selectedModel.providerModelKey ||
            selectedModel.provider_model_key ||
            selectedModel.binding_key ||
            selectedModel.bindingKey ||
            ''
        ).trim(),
        binding_key: String(
          selectedModel.providerModelKey ||
            selectedModel.provider_model_key ||
            selectedModel.binding_key ||
            selectedModel.bindingKey ||
            ''
        ).trim()
      }
    : null

  return {
    provider,
    createdBinding,
    warnings
  }
}

export function getInvalidJsonFieldLabels(fields = []) {
  return normalizeArray(fields)
    .filter((field) => {
      const raw = String(field?.value || '').trim()
      if (!raw) return false
      try {
        JSON.parse(raw)
        return false
      } catch {
        return true
      }
    })
    .map((field) => field.label)
}

function buildProviderRow(provider, bindings = []) {
  const firstBinding = bindings[0] || null

  return {
    id: provider.id,
    name: provider.name || '',
    source: provider.source || '',
    endpoint: provider.base_url || provider.endpoint || '',
    status: provider.status || '',
    binding_id: firstBinding?.id ?? null,
    binding_key: firstBinding?.provider_model_key || firstBinding?.binding_key || '',
    binding_count: bindings.length
  }
}

function findMatchingModelForCandidate(candidate = {}, models = []) {
  const sourceModelID = String(candidate.source_model_id || candidate.provider_model_key || '').trim().toLowerCase()
  const modelName = String(candidate.model_name || candidate.name || candidate.provider_display_name || '').trim().toLowerCase()
  return normalizeArray(models).find((model) => {
    const names = [
      model.id,
      model.name,
      model.display_name,
      model.displayName,
      model.source_model_id,
      model.sourceModelId
    ].map((value) => String(value || '').trim().toLowerCase()).filter(Boolean)
    return names.includes(sourceModelID) || names.includes(modelName)
  }) || null
}

function normalizeBindings(...candidates) {
  for (const candidate of candidates) {
    if (Array.isArray(candidate)) {
      return candidate
    }
  }
  return []
}

function normalizeParameterFields(fields = []) {
  return normalizeArray(fields)
    .map((field, index) => {
      const type = normalizeParameterFieldType(field.type)
      return {
        name: field.name || '',
        label: field.label || field.name || '',
        description: field.description || '',
        extra_tip: field.extra_tip || field.extraTip || '',
        type,
        default_value: normalizeParameterFieldDefaultValue(type, field.default_value ?? field.defaultValue ?? ''),
        option_values: normalizeParameterFieldOptions(field.option_values ?? field.optionValues),
        required: Boolean(field.required),
        advanced: Boolean(field.advanced),
        min: field.min,
        max: field.max,
        step: field.step ?? 1,
        rows: field.rows || (type === 'json' ? 6 : 4),
        sort_order: Number.isFinite(Number(field.sort_order)) ? Number(field.sort_order) : index + 1
      }
    })
    .filter((field) => field.name)
    .sort((left, right) => left.sort_order - right.sort_order)
}

function normalizeParameterFieldType(type) {
  const normalized = String(type || 'text').toLowerCase()
  if (['textarea', 'multiline'].includes(normalized)) return 'textarea'
  if (['password', 'secret'].includes(normalized)) return 'password'
  if (['number', 'integer', 'float'].includes(normalized)) return 'number'
  if (['boolean', 'switch', 'toggle'].includes(normalized)) return 'switch'
  if (['select', 'enum', 'dropdown'].includes(normalized)) return 'select'
  if (['json', 'object', 'map'].includes(normalized)) return 'json'
  return 'text'
}

function normalizeParameterFieldDefaultValue(type, value) {
  if (value === undefined || value === null || value === '') return value
  if (type === 'number') {
    const numeric = Number(value)
    return Number.isFinite(numeric) ? numeric : value
  }
  if (type === 'switch') {
    if (typeof value === 'boolean') return value
    return String(value).toLowerCase() === 'true'
  }
  return value
}

function normalizeParameterFieldOptions(optionValues) {
  if (!Array.isArray(optionValues)) return []
  return optionValues.map((option) => {
    if (option && typeof option === 'object') {
      return {
        label: option.label || option.name || option.title || String(option.value ?? option.code ?? option.key ?? ''),
        value: option.value ?? option.code ?? option.key ?? option.name
      }
    }
    return { label: String(option), value: option }
  })
}

function normalizeArray(value) {
  return Array.isArray(value) ? value : []
}

function normalizeStringList(value) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  if (typeof value === 'string') return value.split(',').map((item) => item.trim()).filter(Boolean)
  return []
}

function numberOrNull(value) {
  const numeric = Number(value)
  return Number.isFinite(numeric) ? numeric : null
}

function normalizeModalities(modalities, modalitiesText) {
  if (Array.isArray(modalities)) return modalities
  if (typeof modalitiesText === 'string' && modalitiesText.trim()) {
    return modalitiesText.split(',').map((item) => item.trim()).filter(Boolean)
  }
  return []
}

function formatModalities(modalities) {
  return Array.isArray(modalities) ? modalities.join(', ') : ''
}

function sanitizeParameters(parameters = {}) {
  const sanitized = {}

  Object.entries(parameters).forEach(([key, value]) => {
    if (value === undefined || value === null) return
    if (typeof value === 'string') {
      const normalized = value.trim()
      if (!normalized) return
      sanitized[key] = normalized
      return
    }
    sanitized[key] = value
  })

  return sanitized
}
