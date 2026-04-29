export const aiStoreSections = [
  { key: 'providers', title: 'AI Providers' },
  { key: 'agents', title: 'AI Agents' },
  { key: 'runtimeProfiles', title: 'AI Runtime Profiles' }
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
    agents: normalizeArray(payload.agents),
    runtimeProfiles: normalizeArray(payload.runtimeProfiles),
    deployments: normalizeArray(payload.deployments)
  }
}

export function buildAiStoreSummary(payload = {}) {
  const state = normalizeAiStoreState(payload)
  const activeBindings = state.providers.reduce((count, provider) => count + provider.bindingCount, 0)

  return [
    { key: 'providers', label: 'AI Providers', value: state.providers.length, tone: 'primary' },
    { key: 'bindings', label: 'Model Bindings', value: activeBindings, tone: 'warning' },
    { key: 'agents', label: 'AI Agents', value: state.agents.length, tone: 'success' },
    { key: 'runtimeProfiles', label: 'AI Runtime Profiles', value: state.runtimeProfiles.length, tone: 'info' }
  ]
}

export function buildModelRows({ models = [], providers = [], runtimeProfiles = [], agents = [], deployments = [], keyword = '' } = {}) {
  const state = normalizeAiStoreState({ models, providers, runtimeProfiles, agents, deployments })
  const normalizedKeyword = String(keyword).trim().toLowerCase()
  const agentById = new Map(state.agents.map((agent) => [String(agent.id), agent]))

  return state.models
    .map((model) => {
      const modelProviders = state.providers.filter((provider) => {
        if (String(provider.model_id) === String(model.id)) return true
        return provider.bindings.some((binding) => String(binding.model_id) === String(model.id))
      })
      const modelDeployments = state.deployments.filter((deployment) => String(deployment.model_id) === String(model.id))
      const modelRuntimeProfiles = state.runtimeProfiles.filter((runtimeProfile) => String(runtimeProfile.model_id) === String(model.id))

      const providerRows = modelProviders.map((provider) => {
        const providerBindings = provider.bindings.filter((binding) => String(binding.model_id) === String(model.id))
        return buildProviderRow(provider, providerBindings)
      })

      const runtimeUsage = modelRuntimeProfiles.map((runtimeProfile) => {
        const agent = agentById.get(String(runtimeProfile.agent_id))
        const bindingPriorityText = String(
          runtimeProfile.binding_priority_json || runtimeProfile.bindingPriorityJSON || ''
        ).trim() || '-'

        return {
          id: runtimeProfile.id,
          runtime_name: runtimeProfile.runtime_name || runtimeProfile.name || '',
          agent_name: agent?.name || runtimeProfile.agent_name || '',
          binding_priority_text: bindingPriorityText,
          status: runtimeProfile.status || ''
        }
      })

      const row = {
        id: model.id,
        name: model.name || '',
        parameterSize: model.parameter_size || model.parameterSize || '',
        modalitiesText: formatModalities(model.modalities),
        source: model.source || '',
        deploymentCount: modelDeployments.length,
        providerCount: modelProviders.length,
        runtimeCount: modelRuntimeProfiles.length,
        providers: providerRows,
        deployments: modelDeployments,
        runtimeUsage,
        searchText: [
          model.name,
          model.parameter_size,
          formatModalities(model.modalities),
          model.source,
          ...providerRows.flatMap((provider) => [provider.name, provider.source, provider.endpoint, provider.status, provider.binding_key]),
          ...modelDeployments.flatMap((deployment) => [deployment.resource_name, deployment.template_name, deployment.version_label, deployment.provider_name]),
          ...runtimeUsage.flatMap((runtimeRow) => [runtimeRow.runtime_name, runtimeRow.agent_name, runtimeRow.binding_priority_text, runtimeRow.status])
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

export function buildAIModelImportPayload({ source, sourceModelId, source_model_id } = {}) {
  return {
    source: String(source || '').trim(),
    source_model_id: String(sourceModelId || source_model_id || '').trim()
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

export function shouldResetRuntimeBindingPriority({ previousModelId, nextModelId, bindingPriorityJSON } = {}) {
  if (!previousModelId || !nextModelId) return false
  if (String(previousModelId) === String(nextModelId)) return false
  return String(bindingPriorityJSON || '').trim() !== '' && String(bindingPriorityJSON || '').trim() !== '[]'
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
