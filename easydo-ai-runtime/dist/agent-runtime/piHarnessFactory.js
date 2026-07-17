import { createModels, createProvider } from '@earendil-works/pi-ai';
import { openAICompletionsApi } from '@earendil-works/pi-ai/api/openai-completions.lazy';
import { openAIResponsesApi } from '@earendil-works/pi-ai/api/openai-responses.lazy';
import { openaiProvider } from '@earendil-works/pi-ai/providers/openai';
import { openrouterProvider } from '@earendil-works/pi-ai/providers/openrouter';
import { anthropicProvider } from '@earendil-works/pi-ai/providers/anthropic';
import { AgentHarness, InMemorySessionRepo, formatSkillsForSystemPrompt } from '@earendil-works/pi-agent-core/node';
import { createWorkspaceTools } from './piWorkspaceTools.js';
import { buildCompactionPersistenceRecord, buildHistoryCompactionSummary, contextBudgetEventPayload, evaluateContextBudget, planHistoryCompaction } from '../services/contextBudget.js';
export class MissingPiHarnessConfigurationError extends Error {
    constructor(message) {
        super(message);
        this.name = 'MissingPiHarnessConfigurationError';
    }
}
const PI_HISTORY_SNAPSHOT_VERSION = 1;
export function createPiHarnessFactory(options) {
    const sessionRepo = new InMemorySessionRepo();
    return async (input) => {
        const selection = normalizeModelSelection(input.modelConfig ?? modelSelectionFromRuntimeConfig({ model: input.model }));
        if (!selection?.provider_id || !selection.id) {
            throw new MissingPiHarnessConfigurationError('Pi harness requires model.provider_id and model.id');
        }
        const models = createModels();
        await (options.configureModels ?? configureEasyDoModels)(models, selection);
        const model = resolveModel(models, selection.provider_id, selection.id);
        const executionEnv = input.executionEnv ?? options.defaultExecutionEnv;
        if (!executionEnv) {
            throw new MissingPiHarnessConfigurationError('Pi harness requires a workspace execution environment');
        }
        const { session, created } = await openOrCreateSession(sessionRepo, input.sessionID);
        if (created) {
            const historySnapshot = compactPiHistory(input.history ?? [], selection, model, options);
            if (historySnapshot.evaluation.context_window_tokens > 0) {
                await session.appendCustomEntry('context.budget.evaluated', {
                    version: PI_HISTORY_SNAPSHOT_VERSION,
                    ...contextBudgetEventPayload(historySnapshot.evaluation)
                });
            }
            const compactionRecord = buildCompactionPersistenceRecord(historySnapshot.evaluation, historySnapshot.plan, {
                version: PI_HISTORY_SNAPSHOT_VERSION,
                reason: historySnapshot.evaluation.reason
            });
            await session.appendCustomEntry('easydo_history_snapshot', {
                ...compactionRecord,
                source_message_count: historySnapshot.source.length,
                injected_message_count: historySnapshot.injected.length,
                compacted_message_count: historySnapshot.compacted.length,
                max_messages: historySnapshot.maxMessages,
                max_chars: historySnapshot.maxChars,
                context_window_tokens: historySnapshot.evaluation.context_window_tokens || undefined,
                provider_id: historySnapshot.evaluation.provider_id || selection.provider_id,
                model_key: historySnapshot.evaluation.model_key || selection.id
            });
            if (historySnapshot.compacted.length > 0) {
                if (historySnapshot.evaluation.context_window_tokens > 0) {
                    await session.appendCustomEntry('context.compaction.started', {
                        ...compactionRecord
                    });
                }
                await session.appendCustomMessageEntry('easydo_history_compaction', buildHistoryCompactionSummary(historySnapshot.compacted, historySnapshot.maxChars), false, {
                    ...compactionRecord,
                    compacted_message_count: historySnapshot.compacted.length
                });
                if (historySnapshot.evaluation.context_window_tokens > 0) {
                    await session.appendCustomEntry('context.compaction.completed', {
                        ...compactionRecord,
                        summary: buildHistoryCompactionSummary(historySnapshot.compacted, Math.min(historySnapshot.maxChars, 1200))
                    });
                }
            }
            for (const historyMessage of historySnapshot.injected) {
                await session.appendMessage(piHistoryMessage(historyMessage, selection, model));
            }
        }
        const baseSystemPrompt = firstString(input.systemPrompt, options.systemPrompt);
        return new AgentHarness({
            env: executionEnv,
            session,
            models,
            model,
            systemPrompt: ({ resources }) => [
                baseSystemPrompt,
                progressiveCapabilityDisclosurePrompt(),
                formatSkillsForSystemPrompt(resources.skills ?? [])
            ].filter(Boolean).join('\n\n'),
            streamOptions: streamOptionsFromSelection(selection),
            thinkingLevel: selection.thinking_level,
            tools: [...createWorkspaceTools(executionEnv, input.runMode ?? 'write'), ...(input.tools ?? options.tools ?? [])],
            resources: { skills: input.skills ?? options.skills ?? [] }
        });
    };
}
function compactPiHistory(history, selection, model, options) {
    const historyRecords = history.map((message) => ({ role: message.role, content: message.content }));
    const bindingWindow = positiveInteger(selection.context_window, 0);
    if (!bindingWindow) {
        return compactPiHistoryByMessageCap(history, historyRecords, options);
    }
    const evaluation = evaluateContextBudget({
        binding: {
            provider_id: selection.provider_id,
            provider_model_key: selection.id,
            context_window_tokens: bindingWindow,
            max_output_tokens: selection.max_tokens,
            capability_source: firstString((selection.inference || {}).capability_source, 'binding_snapshot'),
            capability_snapshot_hash: firstString((selection.inference || {}).capability_snapshot_hash)
        },
        history: historyRecords,
        history_max_messages: options.historyMaxMessages
    });
    const plan = planHistoryCompaction(historyRecords, evaluation, {
        max_messages: options.historyMaxMessages,
        max_chars: options.historyMaxChars
    });
    return {
        source: history,
        injected: plan.injected.map((message, index) => history[history.length - plan.injected.length + index] || {
            role: message.role || 'user',
            content: String(message.content || '')
        }),
        compacted: plan.compacted.map((message) => ({
            role: (message.role || 'user'),
            content: String(message.content || '')
        })),
        maxMessages: plan.max_messages,
        maxChars: plan.max_chars,
        evaluation,
        plan
    };
}
function compactPiHistoryByMessageCap(history, historyRecords, options) {
    const maxMessages = positiveInteger(options.historyMaxMessages, 100);
    const maxChars = positiveInteger(options.historyMaxChars, 200_000);
    const injected = [];
    let usedChars = 0;
    for (let index = history.length - 1; index >= 0; index -= 1) {
        const candidate = history[index];
        const candidateChars = candidate.content.length;
        if (injected.length >= maxMessages || (injected.length > 0 && usedChars + candidateChars > maxChars))
            break;
        injected.unshift(candidate);
        usedChars += candidateChars;
    }
    const compacted = history.slice(0, history.length - injected.length);
    const evaluation = {
        provider_id: '',
        model_key: '',
        capability_source: 'message_cap_fallback',
        capability_snapshot_hash: '',
        context_window_tokens: 0,
        reserved_output_tokens: 0,
        tool_schema_budget: 0,
        safety_margin_tokens: 0,
        usable_context_tokens: maxChars,
        current_context_tokens: historyRecords.reduce((sum, message) => sum + Math.ceil(String(message.content || '').length / 4), 0),
        system_tokens: 0,
        history_tokens: historyRecords.reduce((sum, message) => sum + Math.ceil(String(message.content || '').length / 4), 0),
        prompt_tokens: 0,
        tool_schema_tokens: 0,
        history_message_count: history.length,
        history_max_messages: maxMessages,
        threshold_ratio: 1,
        threshold_tokens: maxChars,
        should_compact: compacted.length > 0,
        reason: compacted.length > 0 ? 'history_message_cap' : 'within_budget'
    };
    const dropEnd = historyRecords.length - injected.length;
    const retainedTokens = injected.reduce((sum, message) => sum + Math.ceil(message.content.length / 4), 0);
    const droppedTokens = compacted.reduce((sum, message) => sum + Math.ceil(message.content.length / 4), 0);
    const plan = {
        injected: historyRecords.slice(dropEnd),
        compacted: historyRecords.slice(0, dropEnd),
        injected_tokens: retainedTokens,
        compacted_tokens: droppedTokens,
        retained_range: {
            start_index: dropEnd,
            end_index: historyRecords.length,
            message_count: injected.length,
            tokens: retainedTokens
        },
        dropped_range: dropEnd > 0
            ? {
                start_index: 0,
                end_index: dropEnd,
                message_count: compacted.length,
                tokens: droppedTokens
            }
            : null,
        max_messages: maxMessages,
        max_chars: maxChars,
        context_window_tokens: 0
    };
    return {
        source: history,
        injected,
        compacted,
        maxMessages,
        maxChars,
        evaluation,
        plan
    };
}
function positiveInteger(value, fallback) {
    const number = Number(value);
    return Number.isSafeInteger(number) && number > 0 ? number : fallback;
}
function progressiveCapabilityDisclosurePrompt() {
    return [
        'EasyDo capability disclosure policy:',
        '- Available skills and MCP tools are capability directories, not pipelines, workflow runs, or completed work.',
        '- Do not list skills or MCP tools as pipelines. Only describe real pipeline resources when a pipeline resource or pipeline-list tool result confirms them.',
        '- Skill details are progressively disclosed: use the available skill name/description to decide whether to invoke it, and rely on explicit skill invocation for full instructions.',
        '- MCP/tool inputs and outputs are only available after an actual tool call. Do not invent tool results from the capability list.',
        '- When the user asks to inspect, query, refresh, trigger, update, delete, execute, deploy, or otherwise operate EasyDo state and a matching MCP tool is available, emit the tool_call.',
        '- For write/execute/refresh/update/delete actions, emit the tool_call immediately after resolving required identifiers. Do not replace the tool_call with a prose confirmation request.',
        '- The EasyDo runtime will create the approval panel and pause for the user when a matching tool requires confirmation.',
        '- After emitting a tool_call that may require approval, stop. Do not narrate waiting for approval, do not ask the user to approve in prose, and do not invent a status update about the approval queue.',
        '- If the user uses a resource label such as 7022 or 7023, resolve it first with a read/list resource tool, then pass the resolved internal id to write or refresh tools.'
    ].join('\n');
}
function normalizeModelSelection(selection) {
    if (!selection)
        return undefined;
    const inference = asRecord(selection.inference);
    return {
        ...selection,
        thinking_level: selection.thinking_level ?? normalizeThinkingLevel(inference.thinking_level, inference.thinkingLevel, inference.reasoning, inference.reasoning_effort, inference.reasoningEffort)
    };
}
async function openOrCreateSession(repo, sessionID) {
    const existing = (await repo.list()).find((metadata) => metadata.id === sessionID);
    if (existing)
        return { session: await repo.open(existing), created: false };
    return { session: await repo.create({ id: sessionID }), created: true };
}
function piHistoryMessage(history, selection, model) {
    if (history.role === 'user') {
        return {
            role: 'user',
            content: history.content,
            timestamp: history.timestamp
        };
    }
    const status = firstString(history.status).toLowerCase();
    const stopReason = status === 'cancelled' ? 'aborted' : status === 'failed' || status === 'timeout' || status === 'interrupted' ? 'error' : 'stop';
    return {
        role: 'assistant',
        content: history.content ? [{ type: 'text', text: history.content }] : [],
        api: model.api,
        provider: selection.provider_id,
        model: selection.id,
        usage: {
            input: 0,
            output: 0,
            cacheRead: 0,
            cacheWrite: 0,
            totalTokens: 0,
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 }
        },
        stopReason,
        ...(stopReason === 'stop' ? {} : { errorMessage: history.content || `Previous EasyDo run ${status || 'failed'}` }),
        timestamp: history.timestamp
    };
}
export async function configureEasyDoModels(models, selection) {
    switch (selection.provider_id) {
        case 'openai':
            models.setProvider(applyProviderOverrides(openAIProviderForSelection(selection), selection));
            break;
        case 'openrouter':
            models.setProvider(applyProviderOverrides(openrouterProvider(), selection));
            break;
        case 'anthropic':
            models.setProvider(applyProviderOverrides(anthropicProvider(), selection));
            break;
        default:
            return;
    }
    // 自定义/第三方模型通常不在内置 catalog 中（例如 SenseNova 的 sensenova-6.7-flash-lite
    // 走 anthropic 协议）。注册完 provider 后，确保本次 selection 的模型可被解析：若内置
    // 列表中没有，则按 selection 元数据合成一个 Model 并并入该 provider 的模型列表。
    ensureSelectionModel(models, selection);
}
/**
 * 确保 selection 指定的模型在 Pi 模型注册表中可解析。内置 provider 只包含官方 catalog
 * 模型，第三方兼容端点（SenseNova/自建 OpenAI 兼容服务等）的自定义模型需要动态合成。
 * 通过覆写 provider.getModels() 把合成模型并入列表，保留原 provider 的 stream/auth 等行为。
 */
function ensureSelectionModel(models, selection) {
    const provider = models.getProvider(selection.provider_id);
    if (!provider)
        return;
    const baseModels = provider.getModels();
    const selectedModel = buildModelFromSelection(selection, provider, baseModels);
    // 运行时 profile/provider 配置比 Pi 内置 catalog 更新、更贴近当前租户环境。
    // 即使内置 catalog 已有该模型，也必须覆盖 baseUrl/headers/compat，否则会把请求打回默认官方端点。
    const merged = [selectedModel, ...baseModels.filter((model) => model.id !== selectedModel.id)];
    models.setProvider({
        ...provider,
        getModels: () => merged
    });
}
function buildModelFromSelection(selection, provider, baseModels) {
    const existingModel = baseModels.find((model) => model.id === selection.id);
    const baseModel = existingModel ?? baseModels[0];
    const api = selection.api ?? baseModel?.api ?? defaultApiForProvider(selection.provider_id);
    const contextWindow = selection.context_window ?? existingModel?.contextWindow ?? baseModel?.contextWindow;
    if (!contextWindow || contextWindow <= 0) {
        throw new Error('model binding capability snapshot is missing context_window_tokens');
    }
    const maxTokens = selection.max_tokens ?? existingModel?.maxTokens ?? baseModel?.maxTokens ?? 8192;
    const mergedHeaders = {
        ...asRecord(existingModel?.headers),
        ...asRecord(selection.headers)
    };
    return {
        ...existingModel,
        id: selection.id,
        name: existingModel?.name || selection.id,
        api,
        provider: selection.provider_id,
        baseUrl: selection.base_url || existingModel?.baseUrl || provider.baseUrl || '',
        // 默认不开启 reasoning：并非所有模型都支持思考模式，避免默认请求 thinking 触发 4xx。
        // 支持思考的模型可在 profile 元数据显式声明 reasoning 后再扩展此处。
        reasoning: false,
        input: existingModel?.input ?? ['text'],
        cost: existingModel?.cost ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow,
        maxTokens,
        ...(Object.keys(mergedHeaders).length > 0 ? { headers: mergedHeaders } : {}),
        ...(selection.compat ? { compat: selection.compat } : existingModel?.compat ? { compat: existingModel.compat } : {})
    };
}
function openAIProviderForSelection(selection) {
    const provider = openaiProvider();
    if (selection.api !== 'openai-completions')
        return provider;
    // pi-ai 的内置 openaiProvider 只挂 openai-responses API。第三方 OpenAI-compatible
    // 模型需要 chat/completions 分发，否则会被错误地发到 /responses，导致兼容端点 404。
    return createProvider({
        id: provider.id,
        name: provider.name,
        baseUrl: provider.baseUrl,
        headers: provider.headers,
        auth: provider.auth,
        models: provider.getModels(),
        api: {
            'openai-responses': openAIResponsesApi(),
            'openai-completions': openAICompletionsApi()
        }
    });
}
function defaultApiForProvider(providerID) {
    switch (providerID) {
        case 'anthropic':
            return 'anthropic-messages';
        case 'openai':
            return 'openai-responses';
        case 'openrouter':
            return 'openai-completions';
        default:
            return 'openai-completions';
    }
}
export function modelSelectionFromRuntimeConfig(input) {
    const provider = asRecord(input.provider);
    const model = asRecord(input.model);
    const binding = asRecord(input.binding);
    const credential = asRecord(input.credential);
    const inference = asRecord(input.inference);
    const rawProviderID = runtimeProviderID(provider, model);
    const providerID = normalizePiProviderID(rawProviderID);
    const rawModelID = runtimeModelID(model);
    const modelID = rawModelID;
    if (!providerID || !modelID)
        return undefined;
    const baseURL = firstString(model.base_url, model.baseUrl, provider.base_url, provider.baseUrl, provider.endpoint);
    const secretRef = asRecord(credential.secret_ref);
    const secret = asRecord(credential.secret);
    const apiKey = firstString(credential.api_key, credential.token, credential.bearer_token, credential.key, secretRef.api_key, secretRef.token, secretRef.bearer_token, secretRef.key, secret.api_key, secret.token, secret.bearer_token, secret.key);
    const headers = asRecord(model.headers);
    const providerHeaders = asRecord(provider.headers);
    const contextWindow = firstNumber(binding.context_window_tokens, binding.context_window, model.context_window_tokens, model.context_window, model.contextWindow, model.context_tokens, provider.context_window);
    const maxTokens = firstNumber(binding.max_output_tokens, model.max_output_tokens, model.max_tokens, model.maxTokens, inference.max_tokens);
    const api = firstString(model.api, provider.api) || defaultApiForRuntimeProvider(providerID, rawProviderID);
    const thinkingLevel = normalizeThinkingLevel(inference.thinking_level, inference.thinkingLevel, inference.reasoning, inference.reasoning_effort, inference.reasoningEffort, model.thinking_level, model.thinkingLevel, model.reasoning);
    return {
        provider_id: providerID,
        id: modelID,
        api,
        base_url: baseURL || undefined,
        api_key: apiKey || undefined,
        headers: Object.keys({ ...providerHeaders, ...headers }).length > 0 ? { ...providerHeaders, ...headers } : undefined,
        compat: openAICompatibleCompat({ providerID, rawProviderID, api, baseURL, provider, model }),
        context_window: contextWindow,
        max_tokens: maxTokens,
        thinking_level: thinkingLevel,
        inference: Object.keys(inference).length > 0 ? inference : undefined
    };
}
function runtimeProviderID(provider, model) {
    return firstString(model.provider_type, provider.provider_type, provider.type, model.provider, provider.provider, model.provider_id, provider.provider_id, provider.id, provider.name);
}
function normalizePiProviderID(providerID) {
    const normalized = normalizedRuntimeID(providerID);
    switch (normalized) {
        case 'openai-compatible':
        case 'openai-compat':
        case 'openai-compatible-api':
        case 'openai-chat':
        case 'chat-completions':
        case 'custom-openai':
        case 'vllm':
        case 'sglang':
        case 'ollama-openai':
            return 'openai';
        case 'openrouter':
            return 'openrouter';
        case 'anthropic':
        case 'claude':
            return 'anthropic';
        default:
            return providerID;
    }
}
function normalizedRuntimeID(value) {
    return value.trim().toLowerCase().replace(/[\s_]+/g, '-');
}
function defaultApiForRuntimeProvider(providerID, rawProviderID) {
    if (!providerID)
        return undefined;
    const normalizedRaw = normalizedRuntimeID(rawProviderID);
    if (providerID === 'openai' && normalizedRaw && normalizedRaw !== 'openai') {
        return 'openai-completions';
    }
    return undefined;
}
function runtimeModelID(model) {
    return firstString(model.provider_model_key, model.model, model.name, model.id, model.model_id);
}
function resolveModel(models, providerID, modelID) {
    const existing = models.getModel(providerID, modelID);
    if (existing)
        return existing;
    // Pi providers are pluggable and may be registered outside this factory. Keep
    // the boundary explicit: if no provider is registered yet, fail before a run.
    throw new MissingPiHarnessConfigurationError(`Pi model is not registered: ${providerID}/${modelID}`);
}
function applyProviderOverrides(provider, selection) {
    if (!selection.base_url && !selection.headers && !selection.api_key)
        return provider;
    return {
        ...provider,
        baseUrl: selection.base_url || provider.baseUrl,
        headers: selection.headers || provider.headers,
        auth: selection.api_key
            ? {
                apiKey: {
                    name: `${provider.name} API key`,
                    resolve: async () => ({ auth: { apiKey: selection.api_key } })
                }
            }
            : provider.auth
    };
}
function streamOptionsFromSelection(selection) {
    const headers = {};
    for (const [key, value] of Object.entries(selection.headers ?? {})) {
        if (typeof value === 'string' && value.trim())
            headers[key] = value;
    }
    if (selection.api_key && !hasHeader(headers, 'authorization') && !hasHeader(headers, 'cf-aig-authorization')) {
        headers.authorization = `Bearer ${selection.api_key}`;
    }
    // maxRetries: 0 — no silent SDK-level retries.  Provider errors (429/5xx)
    // are surfaced as visible runtime events so the user can see the status code
    // and reason, then decide whether to retry manually.
    return {
        maxRetries: 0,
        ...(Object.keys(headers).length > 0 ? { headers } : {})
    };
}
function hasHeader(headers, name) {
    const expected = name.toLowerCase();
    return Object.keys(headers).some((key) => key.toLowerCase() === expected);
}
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
function firstString(...values) {
    for (const value of values) {
        const stringValue = value === undefined || value === null ? '' : String(value).trim();
        if (stringValue)
            return stringValue;
    }
    return '';
}
function firstNumber(...values) {
    for (const value of values) {
        const number = Number(value);
        if (Number.isFinite(number) && number > 0)
            return number;
    }
    return undefined;
}
function normalizeThinkingLevel(...values) {
    for (const value of values) {
        if (value === true)
            return 'medium';
        if (value === false)
            return 'off';
        const normalized = String(value ?? '').trim().toLowerCase().replace(/[_\s-]+/g, '');
        switch (normalized) {
            case 'off':
            case 'none':
            case 'false':
            case 'disabled':
                return 'off';
            case 'minimal':
                return 'minimal';
            case 'low':
                return 'low';
            case 'medium':
            case 'true':
            case 'enabled':
            case 'on':
                return 'medium';
            case 'high':
                return 'high';
            case 'xhigh':
            case 'extrahigh':
                return 'xhigh';
            default:
                break;
        }
    }
    return undefined;
}
function openAICompatibleCompat(input) {
    if (input.api !== 'openai-completions')
        return undefined;
    const configuredCompat = configuredOpenAICompat(input.provider, input.model);
    const needsConservativeDefaults = isCustomOpenAICompatibleRuntimeProvider(input.providerID, input.rawProviderID, input.baseURL);
    if (!needsConservativeDefaults && Object.keys(configuredCompat).length === 0)
        return undefined;
    return {
        ...(needsConservativeDefaults ? conservativeOpenAICompatibleCompat() : {}),
        ...configuredCompat
    };
}
function conservativeOpenAICompatibleCompat() {
    return {
        supportsStore: false,
        supportsDeveloperRole: false,
        supportsReasoningEffort: false,
        supportsUsageInStreaming: false,
        maxTokensField: 'max_tokens',
        supportsStrictMode: false,
        supportsLongCacheRetention: false
    };
}
function isCustomOpenAICompatibleRuntimeProvider(providerID, rawProviderID, baseURL) {
    if (providerID !== 'openai')
        return false;
    const normalizedRaw = normalizedRuntimeID(rawProviderID);
    if (normalizedRaw && normalizedRaw !== 'openai')
        return true;
    const normalizedBaseURL = baseURL.trim().toLowerCase();
    return Boolean(normalizedBaseURL && !normalizedBaseURL.includes('api.openai.com'));
}
function configuredOpenAICompat(provider, model) {
    const providerSettings = jsonRecord(provider.settings_json, provider.settings);
    const modelSettings = jsonRecord(model.settings_json, model.settings);
    const sources = [
        provider.compat,
        provider.openai_compat,
        provider.pi_compat,
        providerSettings.compat,
        providerSettings.openai_compat,
        providerSettings.pi_compat,
        model.compat,
        model.openai_compat,
        model.pi_compat,
        modelSettings.compat,
        modelSettings.openai_compat,
        modelSettings.pi_compat
    ];
    return sources.reduce((compat, source) => ({
        ...compat,
        ...sanitizeOpenAICompat(asRecord(source))
    }), {});
}
function sanitizeOpenAICompat(source) {
    const compat = {};
    copyBooleanCompat(compat, source, 'supportsStore');
    copyBooleanCompat(compat, source, 'supportsDeveloperRole');
    copyBooleanCompat(compat, source, 'supportsReasoningEffort');
    copyBooleanCompat(compat, source, 'supportsUsageInStreaming');
    copyBooleanCompat(compat, source, 'supportsStrictMode');
    copyBooleanCompat(compat, source, 'supportsLongCacheRetention');
    copyBooleanCompat(compat, source, 'requiresToolResultName');
    copyBooleanCompat(compat, source, 'requiresAssistantAfterToolResult');
    copyBooleanCompat(compat, source, 'requiresThinkingAsText');
    copyBooleanCompat(compat, source, 'requiresReasoningContentOnAssistantMessages');
    copyBooleanCompat(compat, source, 'sendSessionAffinityHeaders');
    copyBooleanCompat(compat, source, 'zaiToolStream');
    const maxTokensField = firstString(source.maxTokensField, source.max_tokens_field);
    if (maxTokensField === 'max_tokens' || maxTokensField === 'max_completion_tokens') {
        compat.maxTokensField = maxTokensField;
    }
    const thinkingFormat = firstString(source.thinkingFormat, source.thinking_format);
    if (isOpenAIThinkingFormat(thinkingFormat))
        compat.thinkingFormat = thinkingFormat;
    const chatTemplateKwargs = asRecord(source.chatTemplateKwargs || source.chat_template_kwargs);
    if (Object.keys(chatTemplateKwargs).length > 0)
        compat.chatTemplateKwargs = chatTemplateKwargs;
    const openRouterRouting = asRecord(source.openRouterRouting || source.openrouter_routing || source.open_router_routing);
    if (Object.keys(openRouterRouting).length > 0)
        compat.openRouterRouting = openRouterRouting;
    const vercelGatewayRouting = asRecord(source.vercelGatewayRouting || source.vercel_gateway_routing);
    if (Object.keys(vercelGatewayRouting).length > 0)
        compat.vercelGatewayRouting = vercelGatewayRouting;
    const cacheControlFormat = firstString(source.cacheControlFormat, source.cache_control_format);
    if (cacheControlFormat === 'anthropic')
        compat.cacheControlFormat = 'anthropic';
    return compat;
}
function copyBooleanCompat(target, source, key) {
    if (typeof source[key] === 'boolean') {
        target[key] = source[key];
    }
}
function jsonRecord(...values) {
    for (const value of values) {
        if (value && typeof value === 'object' && !Array.isArray(value))
            return value;
        if (typeof value !== 'string' || !value.trim())
            continue;
        try {
            const parsed = JSON.parse(value);
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed))
                return parsed;
        }
        catch {
            // Invalid optional JSON settings should not block model selection.
        }
    }
    return {};
}
function isOpenAIThinkingFormat(value) {
    return [
        'openai',
        'openrouter',
        'deepseek',
        'together',
        'zai',
        'qwen',
        'chat-template',
        'qwen-chat-template',
        'string-thinking',
        'ant-ling'
    ].includes(value);
}
