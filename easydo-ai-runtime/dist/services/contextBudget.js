export class MissingBindingContextWindowError extends Error {
    code = 'binding_context_window_required';
    constructor(message = 'Model Binding capability snapshot is missing context_window_tokens for this provider+model') {
        super(message);
        this.name = 'MissingBindingContextWindowError';
    }
}
export function estimateTokensFromText(value) {
    const text = value === undefined || value === null ? '' : String(value);
    if (!text)
        return 0;
    return Math.max(1, Math.ceil(text.length / 4));
}
function firstPositiveNumber(...values) {
    for (const value of values) {
        const number = Number(value);
        if (Number.isFinite(number) && number > 0)
            return Math.floor(number);
    }
    return undefined;
}
function firstString(...values) {
    for (const value of values) {
        const text = value === undefined || value === null ? '' : String(value).trim();
        if (text)
            return text;
    }
    return '';
}
export function resolveBindingContextWindowTokens(input) {
    const binding = input.binding || {};
    const model = input.model || {};
    const provider = input.provider || {};
    const tokens = firstPositiveNumber(binding.context_window_tokens, binding.context_window, binding.contextWindow);
    if (!tokens) {
        throw new MissingBindingContextWindowError(`Missing context_window_tokens for provider=${firstString(binding.provider_id, provider.provider_id) || 'unknown'} model=${firstString(binding.provider_model_key, binding.model_id, model.provider_model_key, model.model_id) || 'unknown'}`);
    }
    return tokens;
}
export function evaluateContextBudget(input) {
    const binding = input.binding || {};
    const model = input.model || {};
    const provider = input.provider || {};
    const history = Array.isArray(input.history) ? input.history : [];
    const contextWindow = resolveBindingContextWindowTokens(input);
    const reservedOutput = firstPositiveNumber(input.reserved_output_tokens, binding.max_output_tokens, binding.max_tokens, model.max_output_tokens, model.max_tokens, model.maxTokens) || Math.min(8192, Math.floor(contextWindow * 0.15));
    const toolSchemaChars = Math.max(0, Number(input.tool_schema_chars) || 0);
    const toolSchemaTokens = Math.ceil(toolSchemaChars / 4);
    const safetyMargin = firstPositiveNumber(input.safety_margin_tokens) || Math.max(512, Math.floor(contextWindow * 0.05));
    const usable = Math.max(1, contextWindow - reservedOutput - toolSchemaTokens - safetyMargin);
    const systemTokens = estimateTokensFromText(input.system_prompt);
    const promptTokens = estimateTokensFromText(input.prompt);
    const historyTokens = history.reduce((sum, message) => sum + estimateTokensFromText(message?.content), 0);
    const current = systemTokens + historyTokens + promptTokens + toolSchemaTokens;
    const historyMaxMessages = firstPositiveNumber(input.history_max_messages) || 100;
    const thresholdRatio = Math.min(0.98, Math.max(0.5, Number(input.compaction_threshold_ratio) || 0.85));
    const thresholdTokens = Math.max(1, Math.floor(usable * thresholdRatio));
    const overThreshold = current > thresholdTokens;
    const overMessageCap = history.length > historyMaxMessages;
    const shouldCompact = overThreshold || overMessageCap;
    return {
        provider_id: firstString(binding.provider_id, provider.provider_id, provider.provider_type, provider.id),
        model_key: firstString(binding.provider_model_key, binding.model_id, model.provider_model_key, model.id, model.model),
        capability_source: firstString(binding.capability_source, model.capability_source, 'binding_snapshot'),
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
        history_max_messages: historyMaxMessages,
        threshold_ratio: thresholdRatio,
        threshold_tokens: thresholdTokens,
        should_compact: shouldCompact,
        reason: overThreshold ? 'over_threshold' : overMessageCap ? 'history_message_cap' : 'within_budget'
    };
}
function historyRange(history, startIndex, endIndex) {
    const start = Math.max(0, Math.min(startIndex, history.length));
    const end = Math.max(start, Math.min(endIndex, history.length));
    const slice = history.slice(start, end);
    return {
        start_index: start,
        end_index: end,
        message_count: slice.length,
        tokens: slice.reduce((sum, message) => sum + estimateTokensFromText(message?.content), 0)
    };
}
export function canCompactHistory(state = {}) {
    const status = firstString(state.status).toLowerCase();
    if (state.has_pending_approval || status === 'awaiting_decision' || status === 'awaiting_input') {
        return { allowed: false, reason: 'pending_approval' };
    }
    if (state.has_active_tool_call) {
        return { allowed: false, reason: 'active_tool_call' };
    }
    return { allowed: true };
}
export function planHistoryCompaction(history, evaluation, options = {}) {
    const guard = canCompactHistory(options.guard);
    const maxMessages = firstPositiveNumber(options.max_messages, evaluation.history_max_messages) || 100;
    const maxChars = firstPositiveNumber(options.max_chars, Math.max(4_000, Math.min(evaluation.usable_context_tokens * 2, Math.floor(evaluation.threshold_tokens * 2)))) || 8_000;
    if (!guard.allowed || !evaluation.should_compact) {
        const retained = historyRange(history, 0, history.length);
        return {
            injected: [...history],
            compacted: [],
            injected_tokens: retained.tokens,
            compacted_tokens: 0,
            retained_range: retained,
            dropped_range: null,
            max_messages: maxMessages,
            max_chars: maxChars,
            context_window_tokens: evaluation.context_window_tokens
        };
    }
    const injected = [];
    let usedChars = 0;
    for (let index = history.length - 1; index >= 0; index -= 1) {
        const candidate = history[index] || {};
        const candidateChars = String(candidate.content || '').length;
        if (injected.length >= maxMessages || (injected.length > 0 && usedChars + candidateChars > maxChars))
            break;
        injected.unshift(candidate);
        usedChars += candidateChars;
    }
    const dropEnd = history.length - injected.length;
    const compacted = history.slice(0, dropEnd);
    const retainedRange = historyRange(history, dropEnd, history.length);
    const droppedRange = dropEnd > 0 ? historyRange(history, 0, dropEnd) : null;
    return {
        injected,
        compacted,
        injected_tokens: retainedRange.tokens,
        compacted_tokens: droppedRange?.tokens || 0,
        retained_range: retainedRange,
        dropped_range: droppedRange,
        max_messages: maxMessages,
        max_chars: maxChars,
        context_window_tokens: evaluation.context_window_tokens
    };
}
export function buildHistoryCompactionSummary(compacted, maxChars) {
    const header = `EasyDo history compaction; ${compacted.length} earlier messages summarized for provider-specific context budget:`;
    const body = compacted.map((message) => `${message.role || 'unknown'}: ${String(message.content || '')}`).join('\n');
    const text = `${header}\n${body}`;
    if (text.length <= maxChars)
        return text;
    if (maxChars <= 3)
        return text.slice(0, maxChars);
    return `${text.slice(0, maxChars - 3)}...`;
}
export function contextBudgetEventPayload(evaluation) {
    return {
        provider_id: evaluation.provider_id,
        model_key: evaluation.model_key,
        capability_source: evaluation.capability_source,
        capability_snapshot_hash: evaluation.capability_snapshot_hash || undefined,
        context_window_tokens: evaluation.context_window_tokens,
        reserved_output_tokens: evaluation.reserved_output_tokens,
        tool_schema_budget: evaluation.tool_schema_budget,
        safety_margin_tokens: evaluation.safety_margin_tokens,
        usable_context_tokens: evaluation.usable_context_tokens,
        current_context_tokens: evaluation.current_context_tokens,
        system_tokens: evaluation.system_tokens,
        history_tokens: evaluation.history_tokens,
        prompt_tokens: evaluation.prompt_tokens,
        history_message_count: evaluation.history_message_count,
        threshold_ratio: evaluation.threshold_ratio,
        threshold_tokens: evaluation.threshold_tokens,
        should_compact: evaluation.should_compact,
        reason: evaluation.reason
    };
}
export function contextCompactionEventPayload(evaluation, plan, extras = {}) {
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
        ...extras
    };
}
export function buildCompactionPersistenceRecord(evaluation, plan, extras = {}) {
    return {
        version: 1,
        algorithm: 'history_tail_retain_v1',
        ...contextCompactionEventPayload(evaluation, plan, extras),
        original_message_count: plan.retained_range.message_count + (plan.dropped_range?.message_count || 0),
        failure_policy: 'retain_original_context'
    };
}
