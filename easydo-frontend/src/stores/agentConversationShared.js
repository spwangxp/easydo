export function firstString(...values) {
  for (const value of values) {
    const text = value === undefined || value === null ? '' : String(value).trim()
    if (text) return text
  }
  return ''
}

export function asRecord(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

export function parseRecord(value) {
  if (value && typeof value === 'object' && !Array.isArray(value)) return value
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    return asRecord(JSON.parse(value))
  } catch {
    return {}
  }
}

export function firstPositiveNumber(...values) {
  for (const value of values) {
    const number = Number(value)
    if (Number.isFinite(number) && number > 0) return number
  }
  return undefined
}

export function piApprovalRequestPayload(agentAction = {}) {
  const input = asRecord(agentAction.input_json)
  const display = asRecord(agentAction.display_json)
  const approval = asRecord(display.approval_request)
  return {
    approval_id: firstString(approval.approval_id, agentAction.action_id, agentAction.id),
    request_id: firstString(approval.request_id, approval.approval_id, agentAction.action_id, agentAction.id),
    call_id: firstString(
      approval.provider_tool_call_id,
      approval.tool_call_id,
      approval.call_id,
      input.provider_tool_call_id,
      input.tool_call_id,
      input.call_id
    ),
    tool_name: firstString(approval.tool_name, agentAction.capability_id)
  }
}

export function actionIdFromRef(actionOrId) {
  if (actionOrId && typeof actionOrId === 'object' && !Array.isArray(actionOrId)) {
    return firstString(actionOrId.id, actionOrId.action_id)
  }
  return firstString(actionOrId)
}

export function isPiApprovalAction(agentAction = {}) {
  if (!agentAction || typeof agentAction !== 'object') return false
  if (agentAction.pi_approval === true) return true
  const id = firstString(agentAction.id, agentAction.action_id)
  const actionKind = firstString(agentAction.action_kind)
  return id.startsWith('approval:') || actionKind === 'pi.tool_approval'
}

export function resolveAgentActionRef(actionOrId, pendingActions = [], fallbackFinder = null) {
  const pendingList = Array.isArray(pendingActions) ? pendingActions : []
  if (actionOrId && typeof actionOrId === 'object' && !Array.isArray(actionOrId)) {
    const id = firstString(actionOrId.id, actionOrId.action_id)
    const pending = id
      ? pendingList.find((item) => String(item?.id) === String(id) || String(item?.action_id) === String(id))
      : null
    const merged = {
      ...(pending || {}),
      ...actionOrId,
      id: firstString(actionOrId.id, actionOrId.action_id, pending?.id, pending?.action_id),
      action_id: firstString(actionOrId.action_id, actionOrId.id, pending?.action_id, pending?.id),
      runtime_run_id: firstString(actionOrId.runtime_run_id, pending?.runtime_run_id),
      pi_approval: isPiApprovalAction(actionOrId) || isPiApprovalAction(pending || {})
    }
    return merged
  }
  const id = firstString(actionOrId)
  if (!id) return { id: '' }
  const pending = pendingList.find((item) => String(item?.id) === String(id) || String(item?.action_id) === String(id))
  if (pending) {
    return {
      ...pending,
      id: firstString(pending.id, pending.action_id, id),
      action_id: firstString(pending.action_id, pending.id, id),
      pi_approval: isPiApprovalAction(pending)
    }
  }
  const fallback = typeof fallbackFinder === 'function' ? fallbackFinder(id) : null
  if (fallback && typeof fallback === 'object') {
    return {
      ...fallback,
      id: firstString(fallback.id, fallback.action_id, id),
      action_id: firstString(fallback.action_id, fallback.id, id),
      pi_approval: isPiApprovalAction(fallback)
    }
  }
  return {
    id,
    action_id: id,
    pi_approval: id.startsWith('approval:')
  }
}

export function modelKeyFromBinding(binding) {
  return firstString(binding?.provider_model_key, binding?.model_key, binding?.model, binding?.name, binding?.id)
}

export function normalizeThinkingLevel(value) {
  const text = firstString(value).toLowerCase()
  if (['off', 'minimal', 'low', 'medium', 'high', 'xhigh'].includes(text)) return text
  return 'medium'
}

export function buildSessionModelOverride(provider, binding, thinkingLevel) {
  const bindingMetadata = parseRecord(binding?.metadata_json || binding?.metadata)
  const catalogModel = asRecord(binding?.model)
  const catalogMetadata = parseRecord(catalogModel.metadata || catalogModel.metadata_json)
  const contextWindow = firstPositiveNumber(
    binding?.context_window_tokens,
    binding?.context_window,
    binding?.contextWindow,
    bindingMetadata.context_window_tokens,
    bindingMetadata.context_window,
    bindingMetadata.contextWindow,
    binding?.model?.context_window_tokens,
    binding?.model?.context_window,
    binding?.model?.contextWindow,
    catalogModel.context_window_tokens,
    catalogModel.context_window,
    catalogModel.contextWindow,
    catalogMetadata.context_window_tokens,
    catalogMetadata.context_window,
    catalogMetadata.contextWindow
  )
  const maxOutputTokens = firstPositiveNumber(
    binding?.max_output_tokens,
    bindingMetadata.max_output_tokens,
    catalogModel.max_output_tokens,
    catalogMetadata.max_output_tokens
  )
  return {
    provider: {
      provider_id: firstString(provider?.provider_id, provider?.id, provider?.name, provider?.provider_type, provider?.type),
      provider_type: firstString(provider?.provider_type, provider?.type),
      name: firstString(provider?.name, provider?.display_name),
      base_url: firstString(provider?.base_url, provider?.baseUrl),
      headers: asRecord(provider?.headers_json || provider?.headers)
    },
    binding: {
      ...asRecord(binding),
      context_window_tokens: contextWindow,
      context_window: contextWindow,
      max_output_tokens: maxOutputTokens,
      capability_source: firstString(binding?.capability_source, bindingMetadata.capability_source, catalogMetadata.capability_source),
      capability_snapshot_hash: firstString(binding?.capability_snapshot_hash, bindingMetadata.capability_snapshot_hash)
    },
    model: {
      ...catalogModel,
      ...asRecord(binding),
      provider_model_key: modelKeyFromBinding(binding),
      base_url: firstString(binding?.base_url, binding?.baseUrl, provider?.base_url, provider?.baseUrl),
      context_window: contextWindow,
      context_window_tokens: contextWindow,
      max_output_tokens: maxOutputTokens
    },
    provider_credential_ref: {
      provider_id: provider?.id,
      credential_id: provider?.credential_id
    },
    inference: {
      ...asRecord(binding?.inference_json || binding?.inference),
      thinking_level: normalizeThinkingLevel(thinkingLevel)
    }
  }
}

function sameEntry(entry, target) {
  if (!entry || !target) return false
  if (entry.id != null && target.id != null && String(entry.id) === String(target.id)) return true
  return Boolean(entry.idempotency_key && target.idempotency_key && entry.idempotency_key === target.idempotency_key)
}

export function appendEntryIfMissing(entryList, nextEntry) {
  if (!nextEntry) return
  if (!entryList.some((entry) => sameEntry(entry, nextEntry))) entryList.push(nextEntry)
}

export function replaceOptimisticEntry(entryList, clientEntryId, serverEntry) {
  if (!serverEntry) return
  const index = entryList.findIndex((entry) => entry.id === clientEntryId || entry.idempotency_key === clientEntryId)
  if (index >= 0) {
    entryList.splice(index, 1, serverEntry)
    return
  }
  appendEntryIfMissing(entryList, serverEntry)
}

export function patchConversationEntry(entriesRef, entryId, updater) {
  const index = entriesRef.value.findIndex((entry) => String(entry?.id || '') === String(entryId))
  if (index < 0) return
  const current = entriesRef.value[index]
  const nextEntry = {
    ...current,
    content_blocks: Array.isArray(current.content_blocks) ? [...current.content_blocks] : [],
    output: {
      ...(current.output || {}),
      timings: { ...(current.output?.timings || {}) }
    }
  }
  updater(nextEntry)
  nextEntry.updated_at = new Date().toISOString()
  entriesRef.value.splice(index, 1, nextEntry)
}

export function createStreamingAssistantEntry(clientEntryId) {
  const now = new Date().toISOString()
  return {
    id: `${clientEntryId}-assistant`,
    idempotency_key: `${clientEntryId}-assistant`,
    role: 'assistant',
    entry_type: 'message',
    status: 'streaming',
    content: '',
    content_blocks: [{ type: 'text', text: '' }],
    input: {},
    output: {
      reasoning: '',
      timings: {},
      runtime_events: [],
      capabilities: {},
      available_skills: [],
      loaded_skills: [],
      subagent_results: [],
      agent_actions: [],
      structured_output: {},
      output_schema_valid: true,
      output_schema_errors: []
    },
    created_at: now,
    updated_at: now
  }
}

export function actionDecisionKey(id, decision) {
  return `${id}:${decision}`
}

export function nextPendingActionDecisionKeys(currentKeys, id, decision, pending) {
  const nextKeys = new Set(currentKeys)
  const key = actionDecisionKey(id, decision)
  if (pending) nextKeys.add(key)
  else nextKeys.delete(key)
  return nextKeys
}

export function hasPendingActionDecision(currentKeys, id) {
  const prefix = `${id}:`
  return [...currentKeys].some((key) => key.startsWith(prefix))
}
