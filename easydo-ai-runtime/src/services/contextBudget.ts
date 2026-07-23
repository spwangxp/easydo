export type ContextBudgetSource = {
  context_window_tokens?: unknown
  context_window?: unknown
  contextWindow?: unknown
  max_output_tokens?: unknown
  max_tokens?: unknown
  maxTokens?: unknown
  capability_source?: unknown
  capability_snapshot_hash?: unknown
  provider_id?: unknown
  provider_type?: unknown
  provider_model_key?: unknown
  model_id?: unknown
  id?: unknown
  model?: unknown
}

export type ContextBudgetHistoryMessage = {
  role?: string
  content?: string
}

/** Compaction trigger reasons shown to users and persisted in events. */
export type ContextBudgetCompactionReason =
  | 'within_budget'
  | 'context-window'
  | 'prompt-count'

/** When binding context window is unknown, compact after this many user prompts. */
export const DEFAULT_PROMPT_COUNT_COMPACTION_THRESHOLD = 20

export type ContextBudgetEvaluationInput = {
  binding?: ContextBudgetSource | null
  model?: ContextBudgetSource | null
  provider?: ContextBudgetSource | null
  history?: ContextBudgetHistoryMessage[]
  system_prompt?: string
  prompt?: string
  tool_schema_chars?: number
  reserved_output_tokens?: number
  safety_margin_tokens?: number
  /** @deprecated prefer prompt_count_threshold; kept for backward compatibility */
  history_max_messages?: number
  /** Trigger compaction when user prompt count reaches this. Default 20. */
  prompt_count_threshold?: number
  /** Trigger compaction when current_context exceeds this fraction of usable_context. Default 0.85 */
  compaction_threshold_ratio?: number
}

export type ContextBudgetEvaluation = {
  provider_id: string
  model_key: string
  capability_source: string
  capability_snapshot_hash: string
  context_window_tokens: number
  reserved_output_tokens: number
  tool_schema_budget: number
  safety_margin_tokens: number
  usable_context_tokens: number
  current_context_tokens: number
  system_tokens: number
  history_tokens: number
  prompt_tokens: number
  tool_schema_tokens: number
  history_message_count: number
  history_max_messages: number
  user_prompt_count: number
  prompt_count_threshold: number
  has_context_window: boolean
  threshold_ratio: number
  threshold_tokens: number
  should_compact: boolean
  reason: ContextBudgetCompactionReason
}

export type HistoryMessageRange = {
  start_index: number
  end_index: number
  message_count: number
  tokens: number
}

export type HistoryCompactionPlan = {
  injected: ContextBudgetHistoryMessage[]
  compacted: ContextBudgetHistoryMessage[]
  injected_tokens: number
  compacted_tokens: number
  retained_range: HistoryMessageRange
  dropped_range: HistoryMessageRange | null
  max_messages: number
  max_chars: number
  context_window_tokens: number
  reason: ContextBudgetCompactionReason
}

export type CompactionGuardState = {
  has_pending_approval?: boolean
  has_active_tool_call?: boolean
  status?: string
}

export class MissingBindingContextWindowError extends Error {
  readonly code = 'binding_context_window_required'

  constructor(message = 'Model Binding capability snapshot is missing context_window_tokens for this provider+model') {
    super(message)
    this.name = 'MissingBindingContextWindowError'
  }
}

export function estimateTokensFromText(value: unknown): number {
  const text = value === undefined || value === null ? '' : String(value)
  if (!text) return 0
  return Math.max(1, Math.ceil(text.length / 4))
}

function firstPositiveNumber(...values: unknown[]): number | undefined {
  for (const value of values) {
    const number = Number(value)
    if (Number.isFinite(number) && number > 0) return Math.floor(number)
  }
  return undefined
}

function firstString(...values: unknown[]): string {
  for (const value of values) {
    const text = value === undefined || value === null ? '' : String(value).trim()
    if (text) return text
  }
  return ''
}

/** Strict resolver — throws when binding capability snapshot lacks context window. */
export function resolveBindingContextWindowTokens(input: ContextBudgetEvaluationInput): number {
  const tokens = tryResolveBindingContextWindowTokens(input)
  if (!tokens) {
    const binding = input.binding || {}
    const model = input.model || {}
    const provider = input.provider || {}
    throw new MissingBindingContextWindowError(
      `Missing context_window_tokens for provider=${firstString(binding.provider_id, provider.provider_id) || 'unknown'} model=${firstString(binding.provider_model_key, binding.model_id, model.provider_model_key, model.model_id) || 'unknown'}`
    )
  }
  return tokens
}

/**
 * Soft resolver for agent loop: missing provider+model context window is allowed.
 * Auto-compaction then falls back to prompt-count (default 20 user prompts).
 */
export function tryResolveBindingContextWindowTokens(input: ContextBudgetEvaluationInput): number | undefined {
  const binding = input.binding || {}
  return firstPositiveNumber(
    binding.context_window_tokens,
    binding.context_window,
    binding.contextWindow
  )
}

export function countUserPrompts(
  history: ContextBudgetHistoryMessage[] = [],
  prompt?: string
): number {
  const historyUserCount = history.reduce((sum, message) => {
    const role = firstString(message?.role).toLowerCase()
    return role === 'user' ? sum + 1 : sum
  }, 0)
  const hasCurrentPrompt = firstString(prompt).length > 0
  return historyUserCount + (hasCurrentPrompt ? 1 : 0)
}

export function evaluateContextBudget(input: ContextBudgetEvaluationInput): ContextBudgetEvaluation {
  const binding = input.binding || {}
  const model = input.model || {}
  const provider = input.provider || {}
  const history = Array.isArray(input.history) ? input.history : []
  // Soft: missing window does not fail the agent loop.
  const contextWindow = tryResolveBindingContextWindowTokens(input) || 0
  const hasContextWindow = contextWindow > 0
  const reservedOutput = hasContextWindow
    ? (firstPositiveNumber(
      input.reserved_output_tokens,
      binding.max_output_tokens,
      binding.max_tokens,
      model.max_output_tokens,
      model.max_tokens,
      model.maxTokens
    ) || Math.min(8192, Math.floor(contextWindow * 0.15)))
    : 0
  const toolSchemaChars = Math.max(0, Number(input.tool_schema_chars) || 0)
  const toolSchemaTokens = Math.ceil(toolSchemaChars / 4)
  const safetyMargin = hasContextWindow
    ? (firstPositiveNumber(input.safety_margin_tokens) || Math.max(512, Math.floor(contextWindow * 0.05)))
    : 0
  const usable = hasContextWindow
    ? Math.max(1, contextWindow - reservedOutput - toolSchemaTokens - safetyMargin)
    : 0
  const systemTokens = estimateTokensFromText(input.system_prompt)
  const promptTokens = estimateTokensFromText(input.prompt)
  const historyTokens = history.reduce((sum, message) => sum + estimateTokensFromText(message?.content), 0)
  const current = systemTokens + historyTokens + promptTokens + toolSchemaTokens
  // Prompt-count path: default 20 user prompts (user decision). history_max_messages kept as alias.
  const promptCountThreshold = firstPositiveNumber(
    input.prompt_count_threshold,
    input.history_max_messages
  ) || DEFAULT_PROMPT_COUNT_COMPACTION_THRESHOLD
  const userPromptCount = countUserPrompts(history, input.prompt)
  const thresholdRatio = Math.min(0.98, Math.max(0.5, Number(input.compaction_threshold_ratio) || 0.85))
  const thresholdTokens = hasContextWindow ? Math.max(1, Math.floor(usable * thresholdRatio)) : 0
  const overContextWindow = hasContextWindow && current > thresholdTokens
  const overPromptCount = userPromptCount >= promptCountThreshold
  // Prefer context-window reason when both fire; prompt-count is the soft fallback path.
  let reason: ContextBudgetCompactionReason = 'within_budget'
  if (overContextWindow) reason = 'context-window'
  else if (overPromptCount) reason = 'prompt-count'
  const shouldCompact = overContextWindow || overPromptCount
  return {
    provider_id: firstString(binding.provider_id, provider.provider_id, provider.provider_type, provider.id),
    model_key: firstString(binding.provider_model_key, binding.model_id, model.provider_model_key, model.id, model.model),
    capability_source: firstString(
      binding.capability_source,
      model.capability_source,
      hasContextWindow ? 'binding_snapshot' : 'prompt_count_fallback'
    ),
    capability_snapshot_hash: firstString(binding.capability_snapshot_hash, model.capability_snapshot_hash),
    context_window_tokens: contextWindow,
    reserved_output_tokens: reservedOutput,
    tool_schema_budget: toolSchemaTokens,
    safety_margin_tokens: safetyMargin,
    usable_context_tokens: usable,
    current_context_tokens: current,
    system_tokens: systemTokens,
    history_tokens: historyTokens,
    prompt_tokens: promptTokens,
    tool_schema_tokens: toolSchemaTokens,
    history_message_count: history.length,
    history_max_messages: promptCountThreshold,
    user_prompt_count: userPromptCount,
    prompt_count_threshold: promptCountThreshold,
    has_context_window: hasContextWindow,
    threshold_ratio: thresholdRatio,
    threshold_tokens: thresholdTokens,
    should_compact: shouldCompact,
    reason
  }
}

function historyRange(history: ContextBudgetHistoryMessage[], startIndex: number, endIndex: number): HistoryMessageRange {
  const start = Math.max(0, Math.min(startIndex, history.length))
  const end = Math.max(start, Math.min(endIndex, history.length))
  const slice = history.slice(start, end)
  return {
    start_index: start,
    end_index: end,
    message_count: slice.length,
    tokens: slice.reduce((sum, message) => sum + estimateTokensFromText(message?.content), 0)
  }
}

export function canCompactHistory(state: CompactionGuardState = {}): { allowed: boolean, reason?: string } {
  const status = firstString(state.status).toLowerCase()
  if (state.has_pending_approval || status === 'awaiting_decision' || status === 'awaiting_input') {
    return { allowed: false, reason: 'pending_approval' }
  }
  if (state.has_active_tool_call) {
    return { allowed: false, reason: 'active_tool_call' }
  }
  return { allowed: true }
}

export function planHistoryCompaction(
  history: ContextBudgetHistoryMessage[],
  evaluation: ContextBudgetEvaluation,
  options: { max_messages?: number, max_chars?: number, guard?: CompactionGuardState } = {}
): HistoryCompactionPlan {
  const guard = canCompactHistory(options.guard)
  // Retain roughly half of the prompt-count threshold so compaction actually drops older turns.
  const defaultRetain = Math.max(4, Math.floor((evaluation.prompt_count_threshold || DEFAULT_PROMPT_COUNT_COMPACTION_THRESHOLD) / 2))
  const maxMessages = firstPositiveNumber(options.max_messages, defaultRetain, evaluation.history_max_messages) || defaultRetain
  const maxChars = firstPositiveNumber(
    options.max_chars,
    evaluation.has_context_window
      ? Math.max(4_000, Math.min(evaluation.usable_context_tokens * 2, Math.floor(evaluation.threshold_tokens * 2)))
      : 200_000
  ) || 8_000

  if (!guard.allowed || !evaluation.should_compact) {
    const retained = historyRange(history, 0, history.length)
    return {
      injected: [...history],
      compacted: [],
      injected_tokens: retained.tokens,
      compacted_tokens: 0,
      retained_range: retained,
      dropped_range: null,
      max_messages: maxMessages,
      max_chars: maxChars,
      context_window_tokens: evaluation.context_window_tokens,
      reason: evaluation.reason
    }
  }

  const injected: ContextBudgetHistoryMessage[] = []
  let usedChars = 0
  for (let index = history.length - 1; index >= 0; index -= 1) {
    const candidate = history[index] || {}
    const candidateChars = String(candidate.content || '').length
    if (injected.length >= maxMessages || (injected.length > 0 && usedChars + candidateChars > maxChars)) break
    injected.unshift(candidate)
    usedChars += candidateChars
  }
  const dropEnd = history.length - injected.length
  const compacted = history.slice(0, dropEnd)
  const retainedRange = historyRange(history, dropEnd, history.length)
  const droppedRange = dropEnd > 0 ? historyRange(history, 0, dropEnd) : null
  return {
    injected,
    compacted,
    injected_tokens: retainedRange.tokens,
    compacted_tokens: droppedRange?.tokens || 0,
    retained_range: retainedRange,
    dropped_range: droppedRange,
    max_messages: maxMessages,
    max_chars: maxChars,
    context_window_tokens: evaluation.context_window_tokens,
    reason: evaluation.reason
  }
}

export function formatCompactionReasonLabel(reason: unknown): string {
  const value = firstString(reason)
  if (value === 'context-window') return 'context-window (usage exceeded provider model context window)'
  if (value === 'prompt-count') return 'prompt-count (user prompt count reached compaction threshold)'
  if (value === 'within_budget') return 'within_budget'
  return value || 'unknown'
}

export function buildHistoryCompactionSummary(
  compacted: ContextBudgetHistoryMessage[],
  maxChars: number,
  reason?: ContextBudgetCompactionReason | string
): string {
  const reasonLabel = formatCompactionReasonLabel(reason)
  const header = `EasyDo history compaction; reason=${reasonLabel}; ${compacted.length} earlier messages summarized for provider-specific context budget:`
  const body = compacted.map((message) => `${message.role || 'unknown'}: ${String(message.content || '')}`).join('\n')
  const text = `${header}\n${body}`
  if (text.length <= maxChars) return text
  if (maxChars <= 3) return text.slice(0, maxChars)
  return `${text.slice(0, maxChars - 3)}...`
}

export function contextBudgetEventPayload(evaluation: ContextBudgetEvaluation): Record<string, unknown> {
  return {
    provider_id: evaluation.provider_id,
    model_key: evaluation.model_key,
    capability_source: evaluation.capability_source,
    capability_snapshot_hash: evaluation.capability_snapshot_hash || undefined,
    context_window_tokens: evaluation.context_window_tokens,
    has_context_window: evaluation.has_context_window,
    reserved_output_tokens: evaluation.reserved_output_tokens,
    tool_schema_budget: evaluation.tool_schema_budget,
    safety_margin_tokens: evaluation.safety_margin_tokens,
    usable_context_tokens: evaluation.usable_context_tokens,
    current_context_tokens: evaluation.current_context_tokens,
    system_tokens: evaluation.system_tokens,
    history_tokens: evaluation.history_tokens,
    prompt_tokens: evaluation.prompt_tokens,
    history_message_count: evaluation.history_message_count,
    user_prompt_count: evaluation.user_prompt_count,
    prompt_count_threshold: evaluation.prompt_count_threshold,
    threshold_ratio: evaluation.threshold_ratio,
    threshold_tokens: evaluation.threshold_tokens,
    should_compact: evaluation.should_compact,
    reason: evaluation.reason,
    reason_label: formatCompactionReasonLabel(evaluation.reason)
  }
}

export function contextCompactionEventPayload(
  evaluation: ContextBudgetEvaluation,
  plan: HistoryCompactionPlan,
  extras: Record<string, unknown> = {}
): Record<string, unknown> {
  return {
    ...contextBudgetEventPayload(evaluation),
    before_tokens: evaluation.current_context_tokens,
    after_tokens: evaluation.system_tokens + plan.injected_tokens + evaluation.prompt_tokens + evaluation.tool_schema_tokens,
    compacted_message_count: plan.compacted.length,
    retained_message_count: plan.injected.length,
    compacted_tokens: plan.compacted_tokens,
    retained_tokens: plan.injected_tokens,
    retained_range: plan.retained_range,
    dropped_range: plan.dropped_range || undefined,
    max_messages: plan.max_messages,
    max_chars: plan.max_chars,
    reason: plan.reason || evaluation.reason,
    reason_label: formatCompactionReasonLabel(plan.reason || evaluation.reason),
    ...extras
  }
}

export function buildCompactionPersistenceRecord(
  evaluation: ContextBudgetEvaluation,
  plan: HistoryCompactionPlan,
  extras: Record<string, unknown> = {}
): Record<string, unknown> {
  return {
    version: 1,
    algorithm: 'history_tail_retain_v1',
    ...contextCompactionEventPayload(evaluation, plan, extras),
    original_message_count: plan.retained_range.message_count + (plan.dropped_range?.message_count || 0),
    failure_policy: 'retain_original_context'
  }
}
