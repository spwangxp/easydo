import { RuntimeSourceError } from './runtimeErrorClassifier.js';
import { classifyRuntimeError } from './runtimeErrorClassifier.js';
import { runtimeLogger } from '../observability/runtimeLogger.js';
export class ModelProviderError extends RuntimeSourceError {
    status;
    details;
    constructor(code, message, status = 500, details = {}) {
        super({
            source: 'provider',
            code,
            message,
            http_status: status,
            retryable: status === 429 || status >= 500
        });
        this.name = 'ModelProviderError';
        this.status = status;
        this.details = details;
    }
}
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
function parseJSONRecord(value) {
    if (value && typeof value === 'object' && !Array.isArray(value))
        return value;
    if (typeof value !== 'string')
        return {};
    const trimmed = value.trim();
    if (!trimmed)
        return {};
    try {
        const parsed = JSON.parse(trimmed);
        return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
    }
    catch {
        return {};
    }
}
function asString(value) {
    return value === undefined || value === null ? '' : String(value);
}
function asNumber(value) {
    const numberValue = Number(value);
    return Number.isFinite(numberValue) ? numberValue : undefined;
}
function asStringOrNumber(value) {
    if (typeof value === 'string' || typeof value === 'number')
        return value;
    return undefined;
}
function firstString(...values) {
    for (const value of values) {
        const stringValue = asString(value).trim();
        if (stringValue)
            return stringValue;
    }
    return '';
}
function deltaString(...values) {
    for (const value of values) {
        if (value === undefined || value === null)
            continue;
        const stringValue = asString(value);
        if (stringValue.trim())
            return stringValue;
    }
    return '';
}
function seq36(value, width = 4) {
    const safe = Number.isFinite(value) && value > 0 ? Math.floor(value) : 1;
    const max = Math.pow(36, width) - 1;
    return Math.min(safe, max).toString(36).padStart(width, '0');
}
function readableIDSegment(value, fallback = 'tool', maxLength = 64) {
    const normalized = asString(value)
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '_')
        .replace(/^_+|_+$/g, '');
    return (normalized || fallback).slice(0, maxLength);
}
function providerLabel(profile) {
    return firstString(profile.provider.provider_type, profile.provider.type, profile.provider.name, profile.provider.id, profile.binding.provider_type, 'model provider');
}
function providerType(profile) {
    return firstString(profile.provider.provider_type, profile.provider.type, profile.provider.id, profile.provider.name, profile.binding.provider_type).toLowerCase();
}
function normalizedProviderType(profile) {
    return providerType(profile).replace(/_/g, '-');
}
function isOpenRouterProfile(profile) {
    return normalizedProviderType(profile) === 'openrouter' || resolveBaseURL(profile).includes('openrouter.ai');
}
function providerRuntimeSettings(profile) {
    return {
        ...parseJSONRecord(profile.provider.settings_json),
        ...asRecord(profile.provider.settings),
        ...parseJSONRecord(profile.binding.settings_json),
        ...asRecord(profile.binding.settings)
    };
}
function resolveBaseURL(profile) {
    const baseURL = firstString(profile.provider.base_url, profile.provider.endpoint, profile.binding.base_url, profile.binding.endpoint);
    return (baseURL || 'https://openrouter.ai/api/v1').replace(/\/+$/, '');
}
function joinURL(baseURL, path) {
    return `${baseURL.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`;
}
function resolveEndpointOverride(profile, key) {
    const settings = providerRuntimeSettings(profile);
    return firstString(profile.provider[key], settings[key], profile.binding[key]);
}
function resolveModel(profile) {
    const model = firstString(profile.model.provider_model_key, profile.model.model, profile.model.model_id, profile.model.name, profile.binding.provider_model_key);
    if (!model) {
        throw new ModelProviderError('model_required', 'Agent profile model is required', 400);
    }
    return model;
}
function resolveCredential(profile) {
    const ref = asRecord(profile.provider_credential_ref);
    const secretRef = asRecord(ref.secret_ref);
    const envName = firstString(ref.env, ref.env_var, ref.api_key_env, ref.token_env, secretRef.env, secretRef.env_var, secretRef.api_key_env, secretRef.token_env);
    if (envName) {
        const value = process.env[envName];
        if (!value) {
            throw new ModelProviderError('provider_credential_env_missing', `Provider credential env ${envName} is not set`, 500);
        }
        return value;
    }
    const direct = firstString(ref.api_key, ref.token, ref.access_token, ref.key, ref.password, ref.bearer_token, secretRef.api_key, secretRef.token, secretRef.access_token, secretRef.key, secretRef.password, secretRef.bearer_token);
    if (direct)
        return direct;
    throw new ModelProviderError('provider_credential_required', 'Provider credential reference is required', 400);
}
function credentialAuthHeader(profile) {
    const ref = asRecord(profile.provider_credential_ref);
    const secretRef = asRecord(ref.secret_ref);
    const credential = resolveCredential(profile);
    const headerName = firstString(ref.auth_header, ref.header_name, ref.api_key_header, secretRef.auth_header, secretRef.header_name, secretRef.api_key_header, 'Authorization');
    if (!/^authorization$/i.test(headerName)) {
        return { headerName, headerValue: credential };
    }
    const tokenType = firstString(ref.token_type, ref.auth_scheme, secretRef.token_type, secretRef.auth_scheme, 'Bearer');
    if (['raw', 'none', 'no_prefix'].includes(tokenType.toLowerCase())) {
        return { headerName: 'Authorization', headerValue: credential };
    }
    return { headerName: 'Authorization', headerValue: `${tokenType} ${credential}`.trim() };
}
function providerConfiguredHeaders(profile) {
    const rawHeaders = {
        ...parseJSONRecord(profile.provider.headers_json),
        ...asRecord(profile.provider.headers),
        ...parseJSONRecord(profile.binding.headers_json),
        ...asRecord(profile.binding.headers)
    };
    const headers = {};
    for (const [rawName, rawValue] of Object.entries(rawHeaders)) {
        const name = rawName.trim();
        const value = asString(rawValue).trim();
        if (!name || !value)
            continue;
        if (/^(authorization|proxy-authorization|connection|keep-alive|proxy-authenticate|te|trailer|transfer-encoding|upgrade|host|content-length)$/i.test(name)) {
            continue;
        }
        headers[name] = value;
    }
    return headers;
}
function jsonProviderHeaders(profile, authHeader, defaults = {}) {
    return {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        'Accept-Encoding': 'identity',
        ...defaults,
        ...providerConfiguredHeaders(profile),
        [authHeader.headerName]: authHeader.headerValue
    };
}
function resolveEndpointDialect(profile) {
    const type = normalizedProviderType(profile);
    const endpointOverride = resolveEndpointOverride(profile, 'llm_endpoint').toLowerCase();
    if (endpointOverride.includes('/chat/completions') || endpointOverride === 'chat/completions')
        return 'openai-chat';
    if (endpointOverride.includes('/responses') || endpointOverride === 'responses')
        return 'openai-responses';
    if (type === 'anthropic' || type === 'claude')
        return 'anthropic-messages';
    if (type === 'google' || type === 'gemini' || type === 'google-gemini')
        return 'gemini-generate-content';
    if (['openai', 'openai-responses', 'responses', 'openai-response'].includes(type))
        return 'openai-responses';
    if (['openrouter', 'openai-compatible', 'azure-openai', 'vllm', 'sglang', 'self-hosted', 'custom'].includes(type)) {
        return 'openai-chat';
    }
    return undefined;
}
function providerFamilyForDialect(dialect) {
    if (dialect === 'openai-chat')
        return 'openai-compatible';
    if (dialect === 'openai-responses')
        return 'openai';
    if (dialect === 'anthropic-messages')
        return 'anthropic';
    return 'google-gemini';
}
function endpointForDialect(dialect, baseURL, model, endpointOverride = '') {
    if (endpointOverride) {
        const parsed = URL.canParse(endpointOverride) ? new URL(endpointOverride) : null;
        if (parsed?.protocol && parsed.host)
            return endpointOverride;
        return joinURL(baseURL, endpointOverride);
    }
    if (dialect === 'openai-chat')
        return joinURL(baseURL, '/chat/completions');
    if (dialect === 'openai-responses')
        return joinURL(baseURL, '/responses');
    if (dialect === 'anthropic-messages')
        return joinURL(baseURL, '/messages');
    return joinURL(baseURL, `/models/${encodeURIComponent(model)}:generateContent`);
}
export class ProviderAdapterRegistry {
    resolve(profile) {
        const dialect = resolveEndpointDialect(profile);
        if (!dialect)
            return null;
        const baseURL = resolveBaseURL(profile);
        const model = resolveModel(profile);
        const family = providerFamilyForDialect(dialect);
        const endpointOverride = resolveEndpointOverride(profile, 'llm_endpoint');
        return {
            provider_family: family,
            endpoint_dialect: dialect,
            base_url: baseURL,
            endpoint: endpointForDialect(dialect, baseURL, model, endpointOverride),
            model,
            request_builder: `${dialect}.request`,
            response_parser: `${dialect}.response`,
            stream_parser: dialect === 'openai-chat' ? 'openai-chat.sse' : 'complete-to-events',
            tool_protocol: `${dialect}.tools`,
            tool_result_formatter: `${dialect}.tool-results`,
            reasoning_policy: `${family}.reasoning`,
            json_schema_policy: `${family}.json-schema`,
            capabilities_internal: {
                tools: true,
                stream: dialect === 'openai-chat',
                reasoning: dialect === 'openai-chat' || dialect === 'openai-responses',
                json_schema: dialect === 'openai-chat' || dialect === 'openai-responses'
            },
            diagnostics: {
                provider_type: normalizedProviderType(profile),
                base_url: baseURL,
                endpoint_dialect: dialect
            }
        };
    }
}
function providerErrorFromResponse(profile, status, payload) {
    const message = firstString(asRecord(payload.error).message, payload.message, `Provider returned HTTP ${status}`);
    const authFailure = status === 401 || status === 403 || /auth|authentication|authorization|credential|api key|api_key|token/i.test(message);
    if (authFailure) {
        const credentialRef = asRecord(profile.provider_credential_ref);
        const credentialID = firstString(credentialRef.credential_id, credentialRef.id);
        const target = credentialID
            ? `${providerLabel(profile)} credential #${credentialID}`
            : `${providerLabel(profile)} credential`;
        return new ModelProviderError('provider_auth_failed', `模型服务认证失败（${target}）：${message}。请检查 AI Provider 绑定的 token/API Key 是否有效。`, status);
    }
    const code = isNativeToolUnsupportedMessage(message) ? 'provider_native_tools_unsupported' : 'provider_request_failed';
    return new ModelProviderError(code, message, status);
}
function isNativeToolUnsupportedMessage(message) {
    return /tool use|tool_use|tools?|function calling|function_calling/i.test(message) &&
        /not support|unsupported|no endpoints|disable|cannot|can't|not available/i.test(message);
}
function resolvePromptString(prompt, key) {
    const value = prompt[key];
    if (typeof value === 'string')
        return value.trim();
    return '';
}
function hasData(value) {
    if (Array.isArray(value))
        return value.length > 0;
    return Boolean(value && typeof value === 'object' && Object.keys(value).length > 0);
}
function hasEnforcedOutputSchema(value) {
    const schema = asRecord(value);
    const keys = Object.keys(schema);
    if (keys.length === 0)
        return false;
    // { type: "object" } is the profile editor's placeholder, not a contract.
    if (keys.length === 1 && asString(schema.type) === 'object')
        return false;
    return true;
}
function reasoningOptions(profile) {
    if (!isOpenRouterProfile(profile))
        return {};
    const inference = asRecord(profile.inference);
    const configuredReasoning = asRecord(inference.reasoning);
    const rawLevel = firstString(inference.thinking_level, inference.thinkingLevel, configuredReasoning.effort, configuredReasoning.level).toLowerCase();
    if (!rawLevel || rawLevel === 'none' || rawLevel === 'off' || rawLevel === 'disabled') {
        return {};
    }
    const effort = ['low', 'medium', 'high'].includes(rawLevel) ? rawLevel : 'medium';
    return {
        include_reasoning: true,
        reasoning: {
            effort
        }
    };
}
function entryPayloadSummary(entry) {
    const payload = {
        content: entry.content,
        input: entry.input,
        output: entry.output
    };
    return JSON.stringify(payload, null, 2);
}
function appendModelVisibleHistoryEntry(messages, entry) {
    if (entry.entry_type === 'message') {
        if (entry.role === 'user' || entry.role === 'assistant') {
            messages.push({ role: entry.role, content: entry.content });
        }
        const subagentResults = Array.isArray(entry.output?.subagent_results) ? entry.output.subagent_results : [];
        if (subagentResults.length > 0) {
            messages.push({
                role: 'system',
                content: `Sub-agent results from prior assistant turn:\n${JSON.stringify(subagentResults, null, 2)}`
            });
        }
        const toolResults = Array.isArray(entry.output?.tool_results) ? entry.output.tool_results : [];
        if (toolResults.length > 0) {
            messages.push({
                role: 'system',
                content: `Tool/action results from prior assistant turn:\n${JSON.stringify(toolResults, null, 2)}`
            });
        }
        return;
    }
    if (entry.entry_type === 'run_event' && entry.role === 'user') {
        messages.push({
            role: 'user',
            content: `User action decision:\n${entryPayloadSummary(entry)}`
        });
        return;
    }
    if (entry.entry_type === 'tool_result') {
        messages.push({
            role: 'system',
            content: `Tool/action result:\n${entryPayloadSummary(entry)}`
        });
        return;
    }
    if (entry.entry_type === 'summary') {
        messages.push({
            role: 'system',
            content: `Conversation summary:\n${entry.content}`
        });
        return;
    }
    if (entry.entry_type === 'error') {
        messages.push({
            role: 'system',
            content: `Previous assistant error:\n${entryPayloadSummary(entry)}`
        });
    }
}
function buildMessages(request, extraSystemMessages = []) {
    const messages = [];
    const prompt = asRecord(request.profile.prompt);
    const systemPrompt = resolvePromptString(prompt, 'system') ||
        'You are EasyDo page assistant. Answer concisely and use the current page context when it is relevant.';
    messages.push({ role: 'system', content: systemPrompt });
    const developerPrompt = request.developer_instructions ||
        resolvePromptString(prompt, 'developer') ||
        resolvePromptString(prompt, 'developer_instructions');
    if (developerPrompt) {
        messages.push({ role: 'system', content: `Developer instructions:\n${developerPrompt}` });
    }
    if (request.include_current_page || Object.keys(request.context_ref).length > 0) {
        messages.push({
            role: 'system',
            content: `Current EasyDo context:\n${JSON.stringify(request.context_ref, null, 2)}`
        });
    }
    if (hasData(request.capabilities)) {
        messages.push({
            role: 'system',
            content: `Allowed EasyDo runtime capabilities:\n${JSON.stringify(request.capabilities, null, 2)}`
        });
    }
    if (hasData(request.loaded_skills)) {
        messages.push({
            role: 'system',
            content: `Loaded EasyDo skills:\n${JSON.stringify(request.loaded_skills, null, 2)}`
        });
    }
    if (hasData(request.subagent_results)) {
        messages.push({
            role: 'system',
            content: `Sub-agent results to consider:\n${JSON.stringify(request.subagent_results, null, 2)}`
        });
    }
    if (hasEnforcedOutputSchema(request.output_schema)) {
        messages.push({
            role: 'system',
            content: `Final answer must satisfy this JSON schema:\n${JSON.stringify(request.output_schema, null, 2)}`
        });
    }
    for (const message of extraSystemMessages.map((item) => item.trim()).filter(Boolean)) {
        messages.push({ role: 'system', content: message });
    }
    for (const entry of request.history.slice(-12)) {
        appendModelVisibleHistoryEntry(messages, entry);
    }
    messages.push({ role: 'user', content: request.content });
    return messages;
}
function extractText(payload) {
    const choices = Array.isArray(payload.choices) ? payload.choices : [];
    const firstChoice = asRecord(choices[0]);
    const message = asRecord(firstChoice.message);
    const content = message.content;
    if (typeof content === 'string')
        return content.trim();
    if (Array.isArray(content)) {
        return content
            .map((block) => {
            const record = asRecord(block);
            return firstString(record.text, asRecord(record.content).text);
        })
            .filter(Boolean)
            .join('\n')
            .trim();
    }
    return '';
}
function extractDeltaText(delta) {
    const content = delta.content;
    if (typeof content === 'string')
        return deltaString(content);
    if (Array.isArray(content)) {
        return content
            .map((block) => {
            const record = asRecord(block);
            return deltaString(record.text, asRecord(record.content).text);
        })
            .filter((item) => String(item).trim())
            .join('\n');
    }
    return '';
}
function extractReasoningDelta(delta) {
    return deltaString(delta.reasoning, delta.reasoning_content, delta.thinking);
}
function parseToolArguments(value) {
    if (value === undefined || value === null)
        return {};
    if (isRecordLike(value))
        return asRecord(value);
    if (typeof value !== 'string')
        return String(value);
    const trimmed = value.trim();
    if (!trimmed)
        return {};
    try {
        const parsed = JSON.parse(trimmed);
        return isRecordLike(parsed) ? asRecord(parsed) : trimmed;
    }
    catch {
        return trimmed;
    }
}
function isRecordLike(value) {
    return Boolean(value && typeof value === 'object' && !Array.isArray(value));
}
function normalizeToolCall(rawValue, index = 0) {
    const raw = asRecord(rawValue);
    const functionValue = raw.function;
    const fn = asRecord(functionValue);
    const name = firstString(raw.name, fn.name, raw.tool_name, typeof functionValue === 'string' ? functionValue : '');
    if (!name)
        return null;
    const argumentsValue = raw.arguments ?? fn.arguments ?? raw.input ?? raw.params ?? {};
    return {
        id: firstString(raw.id, raw.tool_call_id, `tc_${readableIDSegment(name)}_${seq36(index + 1)}`),
        name,
        arguments: parseToolArguments(argumentsValue),
        operation_type: firstString(raw.operation_type, raw.operation, asRecord(argumentsValue).operation_type) || undefined,
        target_type: firstString(raw.target_type, asRecord(argumentsValue).target_type) || undefined,
        target_id: firstString(raw.target_id, asRecord(argumentsValue).target_id) || undefined,
        risk_summary: firstString(raw.risk_summary, asRecord(argumentsValue).risk_summary) || undefined,
        requires_confirmation: raw.requires_confirmation === true || asRecord(argumentsValue).requires_confirmation === true,
        mcp_server_key: firstString(raw.mcp_server_key, raw.mcp_server, raw.server_key, asRecord(argumentsValue).mcp_server_key) || undefined,
        mcp_server_id: asStringOrNumber(raw.mcp_server_id ?? asRecord(argumentsValue).mcp_server_id),
        capability: firstString(raw.capability, asRecord(argumentsValue).capability) || undefined,
        raw
    };
}
function extractToolCallsFromMessage(message) {
    const rawToolCalls = Array.isArray(message.tool_calls) ? message.tool_calls : [];
    return rawToolCalls
        .map((item, index) => normalizeToolCall(item, index))
        .filter(Boolean);
}
function textFromParts(parts) {
    return parts
        .filter((part) => part.type === 'text' || part.type === 'refusal' || part.type === 'warning')
        .map((part) => part.text.trim())
        .filter(Boolean)
        .join('\n');
}
function toolCallsFromParts(parts) {
    return parts
        .filter((part) => part.type === 'tool_call')
        .map((part) => part.tool_call);
}
function hasUsablePart(part) {
    if (part.type === 'tool_call')
        return true;
    if (part.type === 'reasoning')
        return false;
    return Boolean(part.text.trim());
}
function finishReasonFromOpenAIChat(payload) {
    const choices = Array.isArray(payload.choices) ? payload.choices : [];
    return firstString(asRecord(choices[0]).finish_reason);
}
function resultFromParts(params) {
    if (!params.parts.some(hasUsablePart)) {
        throw new ModelProviderError('provider_response_empty', 'Provider response did not include assistant text, refusal, warning, or tool calls', 502, params.empty_details || {});
    }
    const toolCalls = toolCallsFromParts(params.parts);
    return {
        text: textFromParts(params.parts),
        usage: params.usage || {},
        raw: params.raw,
        tool_calls: toolCalls,
        parts: params.parts,
        finish: {
            reason: params.finish_reason || undefined,
            has_more_tool_work: toolCalls.length > 0,
            raw: params.raw
        }
    };
}
function providerResponseDiagnostics(runtime, payload, rawText, attempt) {
    const choices = Array.isArray(payload.choices) ? payload.choices : [];
    const firstChoice = asRecord(choices[0]);
    const message = asRecord(firstChoice.message);
    const content = message.content;
    const toolCalls = Array.isArray(message.tool_calls) ? message.tool_calls : [];
    const reasoning = firstString(message.reasoning, message.reasoning_content, message.thinking);
    return {
        attempt,
        provider_family: runtime.provider_family,
        endpoint_dialect: runtime.endpoint_dialect,
        model: runtime.model,
        response_top_keys: Object.keys(payload).slice(0, 24),
        raw_text_length: rawText.length,
        choices_count: choices.length,
        first_choice_keys: Object.keys(firstChoice).slice(0, 24),
        finish_reason: firstString(firstChoice.finish_reason) || undefined,
        native_finish_reason: firstString(firstChoice.native_finish_reason) || undefined,
        message_keys: Object.keys(message).slice(0, 24),
        content_type: Array.isArray(content) ? 'array' : content === null ? 'null' : typeof content,
        content_length: typeof content === 'string' ? content.length : Array.isArray(content) ? content.length : 0,
        reasoning_length: reasoning.length,
        tool_calls_count: toolCalls.length
    };
}
function isProviderEmptyResponse(error) {
    return error instanceof ModelProviderError && error.code === 'provider_response_empty';
}
function isNativeToolUnsupportedError(error) {
    return error instanceof ModelProviderError &&
        (error.code === 'provider_native_tools_unsupported' || isNativeToolUnsupportedMessage(error.message));
}
function parseOpenAIChatParts(payload) {
    const choices = Array.isArray(payload.choices) ? payload.choices : [];
    const firstChoice = asRecord(choices[0]);
    const message = asRecord(firstChoice.message);
    const parts = [];
    const reasoning = firstString(message.reasoning, message.reasoning_content, message.thinking);
    if (reasoning)
        parts.push({ type: 'reasoning', text: reasoning, raw: message });
    const text = extractText(payload);
    if (text)
        parts.push({ type: 'text', text, raw: message });
    for (const toolCall of extractToolCallsFromMessage(message)) {
        parts.push({ type: 'tool_call', tool_call: toolCall, raw: toolCall.raw });
    }
    return parts;
}
function parseOpenAIResponsesParts(payload) {
    const parts = [];
    const output = Array.isArray(payload.output) ? payload.output : [];
    let toolCallIndex = 0;
    for (const item of output) {
        const record = asRecord(item);
        const type = firstString(record.type);
        if (type === 'reasoning') {
            const summary = Array.isArray(record.summary) ? record.summary : [];
            const text = summary
                .map((summaryItem) => firstString(asRecord(summaryItem).text, asRecord(summaryItem).summary_text))
                .filter(Boolean)
                .join('\n');
            if (text)
                parts.push({ type: 'reasoning', text, raw: record });
            continue;
        }
        if (type === 'function_call') {
            const toolCall = normalizeToolCall({
                id: firstString(record.call_id, record.id),
                name: record.name,
                arguments: record.arguments,
                raw: record
            }, toolCallIndex);
            toolCallIndex += 1;
            if (toolCall)
                parts.push({ type: 'tool_call', tool_call: toolCall, raw: record });
            continue;
        }
        if (type === 'message') {
            const content = Array.isArray(record.content) ? record.content : [];
            for (const contentItem of content) {
                const contentRecord = asRecord(contentItem);
                const contentType = firstString(contentRecord.type);
                const text = firstString(contentRecord.text, contentRecord.output_text);
                if (text && (contentType === 'output_text' || contentType === 'text' || !contentType)) {
                    parts.push({ type: 'text', text, raw: contentRecord });
                }
                const refusal = firstString(contentRecord.refusal);
                if (refusal || contentType === 'refusal') {
                    parts.push({ type: 'refusal', text: refusal || text, raw: contentRecord });
                }
            }
        }
    }
    const outputText = firstString(payload.output_text);
    if (outputText && !parts.some((part) => part.type === 'text')) {
        parts.push({ type: 'text', text: outputText, raw: payload });
    }
    return parts;
}
function parseAnthropicParts(payload) {
    const parts = [];
    const content = Array.isArray(payload.content) ? payload.content : [];
    let toolCallIndex = 0;
    for (const item of content) {
        const record = asRecord(item);
        const type = firstString(record.type);
        if (type === 'text') {
            const text = firstString(record.text);
            if (text)
                parts.push({ type: 'text', text, raw: record });
            continue;
        }
        if (type === 'thinking') {
            const text = firstString(record.thinking, record.text);
            if (text)
                parts.push({ type: 'reasoning', text, raw: record });
            continue;
        }
        if (type === 'tool_use') {
            const toolCall = normalizeToolCall({
                id: record.id,
                name: record.name,
                arguments: record.input,
                raw: record
            }, toolCallIndex);
            toolCallIndex += 1;
            if (toolCall)
                parts.push({ type: 'tool_call', tool_call: toolCall, raw: record });
        }
    }
    return parts;
}
function parseGeminiParts(payload) {
    const candidates = Array.isArray(payload.candidates) ? payload.candidates : [];
    const firstCandidate = asRecord(candidates[0]);
    const content = asRecord(firstCandidate.content);
    const rawParts = Array.isArray(content.parts) ? content.parts : [];
    const parts = [];
    let toolCallIndex = 0;
    for (const item of rawParts) {
        const record = asRecord(item);
        const text = firstString(record.text);
        if (text) {
            parts.push({ type: 'text', text, raw: record });
            continue;
        }
        const functionCall = asRecord(record.functionCall || record.function_call);
        if (Object.keys(functionCall).length > 0) {
            const toolCall = normalizeToolCall({
                id: functionCall.id,
                name: functionCall.name,
                arguments: functionCall.args || functionCall.arguments,
                raw: functionCall
            }, toolCallIndex);
            toolCallIndex += 1;
            if (toolCall)
                parts.push({ type: 'tool_call', tool_call: toolCall, raw: functionCall });
        }
    }
    return parts;
}
function responseFormatOptions(request) {
    if (hasEnforcedOutputSchema(request.output_schema)) {
        return {
            response_format: {
                type: 'json_schema',
                json_schema: {
                    name: 'easydo_output',
                    schema: request.output_schema,
                    strict: true
                }
            }
        };
    }
    if (request.profile.response_mode === 'json') {
        return { response_format: { type: 'json_object' } };
    }
    return {};
}
function textToolProtocolInstruction(request) {
    const tools = Array.isArray(request.tools) ? request.tools : [];
    const definitions = tools
        .map((tool) => ({
        name: firstString(tool.name),
        description: firstString(tool.description),
        input_schema: Object.keys(asRecord(tool.input_schema)).length > 0
            ? asRecord(tool.input_schema)
            : { type: 'object', properties: {} }
    }))
        .filter((tool) => tool.name);
    if (definitions.length === 0)
        return '';
    return [
        'Native tool calling is unavailable for this provider/model. Use the EasyDo text tool protocol when a tool is required.',
        'If no tool is required, answer normally.',
        'If a tool is required, respond with only valid JSON in this exact shape:',
        '{"tool_calls":[{"id":"stable-call-id","name":"tool_name","operation_type":"read","arguments":{}}]}',
        'Use operation_type "read" for read-only queries and "write" for actions that mutate EasyDo state.',
        `Available tools:\n${JSON.stringify(definitions, null, 2)}`
    ].join('\n');
}
function toolOptions(request) {
    const tools = Array.isArray(request.tools) ? request.tools : [];
    const definitions = tools
        .map((tool) => ({
        name: firstString(tool.name),
        description: firstString(tool.description),
        parameters: asRecord(tool.input_schema)
    }))
        .filter((tool) => tool.name)
        .map((tool) => ({
        type: 'function',
        function: {
            name: tool.name,
            description: tool.description || `Call EasyDo MCP tool ${tool.name}.`,
            parameters: Object.keys(tool.parameters).length > 0
                ? tool.parameters
                : { type: 'object', properties: {} }
        }
    }));
    if (definitions.length === 0)
        return {};
    return {
        tools: definitions,
        tool_choice: 'auto'
    };
}
function openAIResponsesToolOptions(request) {
    const tools = Array.isArray(request.tools) ? request.tools : [];
    const definitions = tools
        .map((tool) => ({
        type: 'function',
        name: firstString(tool.name),
        description: firstString(tool.description) || `Call EasyDo MCP tool ${tool.name}.`,
        parameters: Object.keys(asRecord(tool.input_schema)).length > 0
            ? asRecord(tool.input_schema)
            : { type: 'object', properties: {} }
    }))
        .filter((tool) => tool.name);
    if (definitions.length === 0)
        return {};
    return { tools: definitions, tool_choice: 'auto' };
}
function anthropicToolOptions(request) {
    const tools = Array.isArray(request.tools) ? request.tools : [];
    const definitions = tools
        .map((tool) => ({
        name: firstString(tool.name),
        description: firstString(tool.description) || `Call EasyDo MCP tool ${tool.name}.`,
        input_schema: Object.keys(asRecord(tool.input_schema)).length > 0
            ? asRecord(tool.input_schema)
            : { type: 'object', properties: {} }
    }))
        .filter((tool) => tool.name);
    return definitions.length > 0 ? { tools: definitions } : {};
}
function geminiToolOptions(request) {
    const tools = Array.isArray(request.tools) ? request.tools : [];
    const declarations = tools
        .map((tool) => ({
        name: firstString(tool.name),
        description: firstString(tool.description) || `Call EasyDo MCP tool ${tool.name}.`,
        parameters: Object.keys(asRecord(tool.input_schema)).length > 0
            ? asRecord(tool.input_schema)
            : { type: 'object', properties: {} }
    }))
        .filter((tool) => tool.name);
    return declarations.length > 0 ? { tools: [{ functionDeclarations: declarations }] } : {};
}
function buildOpenAIResponsesInput(request) {
    return buildMessages(request).map((message) => ({
        role: message.role,
        content: message.content
    }));
}
function buildAnthropicMessages(request) {
    const system = [];
    const messages = [];
    for (const message of buildMessages(request)) {
        if (message.role === 'system') {
            system.push(message.content);
        }
        else {
            messages.push({ role: message.role, content: message.content });
        }
    }
    return { system: system.join('\n\n'), messages };
}
function buildGeminiContents(request) {
    return buildMessages(request).map((message) => ({
        role: message.role === 'assistant' ? 'model' : 'user',
        parts: [{ text: message.role === 'system' ? `[system]\n${message.content}` : message.content }]
    }));
}
async function* parseOpenAICompatibleStream(response) {
    if (!response.body) {
        throw new ModelProviderError('provider_stream_empty', 'Provider response did not include a stream body', 502);
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    const pendingToolCalls = new Map();
    let emittedToolCallCount = 0;
    const mergeToolCallDelta = (rawValue, position) => {
        const raw = asRecord(rawValue);
        const key = firstString(raw.index, raw.id, String(position));
        const existing = pendingToolCalls.get(key) || {};
        const existingFunction = asRecord(existing.function);
        const deltaFunction = asRecord(raw.function);
        const mergedFunction = { ...existingFunction, ...deltaFunction };
        if (typeof deltaFunction.arguments === 'string') {
            mergedFunction.arguments = `${typeof existingFunction.arguments === 'string' ? existingFunction.arguments : ''}${deltaFunction.arguments}`;
        }
        pendingToolCalls.set(key, {
            ...existing,
            ...raw,
            function: Object.keys(mergedFunction).length > 0 ? mergedFunction : existing.function
        });
    };
    function* flushToolCalls(raw) {
        for (const rawToolCall of pendingToolCalls.values()) {
            const toolCall = normalizeToolCall(rawToolCall, emittedToolCallCount);
            emittedToolCallCount += 1;
            if (toolCall) {
                yield { type: 'tool_call', tool_call: toolCall, raw };
            }
        }
        pendingToolCalls.clear();
    }
    function* handleDataLine(data) {
        if (!data)
            return;
        if (data === '[DONE]') {
            yield* flushToolCalls({ done: true });
            return;
        }
        const payload = JSON.parse(data);
        const choices = Array.isArray(payload.choices) ? payload.choices : [];
        const firstChoice = asRecord(choices[0]);
        const delta = asRecord(firstChoice.delta);
        const reasoning = extractReasoningDelta(delta);
        if (reasoning) {
            yield { type: 'reasoning_delta', delta: reasoning, raw: payload };
        }
        const toolCalls = Array.isArray(delta.tool_calls) ? delta.tool_calls : [];
        toolCalls.forEach((rawToolCall, index) => mergeToolCallDelta(rawToolCall, index));
        const text = extractDeltaText(delta);
        if (text) {
            yield { type: 'answer_delta', delta: text, raw: payload };
        }
        if (firstString(firstChoice.finish_reason) === 'tool_calls') {
            yield* flushToolCalls(payload);
        }
    }
    while (true) {
        const { done, value } = await reader.read();
        if (done)
            break;
        buffer += decoder.decode(value, { stream: true });
        const chunks = buffer.split(/\r?\n\r?\n/);
        buffer = chunks.pop() || '';
        for (const chunk of chunks) {
            const dataLines = chunk
                .split(/\r?\n/)
                .filter((line) => line.startsWith('data:'))
                .map((line) => line.replace(/^data:\s?/, '').trim());
            for (const data of dataLines) {
                yield* handleDataLine(data);
            }
        }
    }
    if (buffer.trim()) {
        const dataLines = buffer
            .split(/\r?\n/)
            .filter((line) => line.startsWith('data:'))
            .map((line) => line.replace(/^data:\s?/, '').trim());
        for (const data of dataLines) {
            yield* handleDataLine(data);
        }
    }
    yield* flushToolCalls({ done: true });
}
export class ProviderAdapterChatModelClient {
    fetchImpl;
    registry;
    logger;
    constructor(fetchImpl = globalThis.fetch.bind(globalThis), registry = new ProviderAdapterRegistry(), logger = runtimeLogger) {
        this.fetchImpl = fetchImpl;
        this.registry = registry;
        this.logger = logger;
    }
    async complete(request) {
        const runtime = this.registry.resolve(request.profile);
        if (!runtime)
            return null;
        const context = providerLogContext(request, runtime, 'complete');
        this.logger.info({ ...context, outcome: 'started' });
        try {
            let result = null;
            if (runtime.endpoint_dialect === 'openai-chat')
                result = await this.completeOpenAIChat(request, runtime);
            if (runtime.endpoint_dialect === 'openai-responses')
                result = await this.completeOpenAIResponses(request, runtime);
            if (runtime.endpoint_dialect === 'anthropic-messages')
                result = await this.completeAnthropicMessages(request, runtime);
            if (runtime.endpoint_dialect === 'gemini-generate-content')
                result = await this.completeGeminiGenerateContent(request, runtime);
            this.logger.info({ ...context, outcome: result ? 'completed' : 'unsupported' });
            return result;
        }
        catch (error) {
            const descriptor = classifyRuntimeError(error, { source: 'provider' });
            this.logger.error({ ...context, outcome: descriptor.terminal_status, code: descriptor.code, category: descriptor.category });
            throw error;
        }
    }
    async retryProviderEmptyResponse(operation) {
        try {
            return await operation(1);
        }
        catch (error) {
            if (!isProviderEmptyResponse(error))
                throw error;
        }
        return operation(2);
    }
    async recoverNativeToolUnsupported(request, nativeOperation, textToolFallbackOperation) {
        try {
            return await this.retryProviderEmptyResponse(nativeOperation);
        }
        catch (error) {
            if (!isNativeToolUnsupportedError(error) || !textToolProtocolInstruction(request))
                throw error;
        }
        return this.retryProviderEmptyResponse(textToolFallbackOperation);
    }
    async completeOpenAIChat(request, runtime) {
        const authHeader = credentialAuthHeader(request.profile);
        const inference = asRecord(request.profile.inference);
        const buildBody = (nativeTools) => ({
            model: runtime.model,
            messages: buildMessages(request, nativeTools ? [] : [textToolProtocolInstruction(request)]),
            temperature: asNumber(inference.temperature) ?? 0.2,
            max_tokens: asNumber(inference.max_tokens) ?? asNumber(inference.maxTokens) ?? 2048,
            ...reasoningOptions(request.profile),
            ...responseFormatOptions(request),
            ...(nativeTools ? toolOptions(request) : {})
        });
        const completeWithBody = async (attempt, nativeTools) => {
            const response = await this.fetchImpl(runtime.endpoint, {
                method: 'POST',
                headers: jsonProviderHeaders(request.profile, authHeader, { 'X-Title': 'EasyDo Page Assistant' }),
                body: JSON.stringify(buildBody(nativeTools))
            });
            const rawText = await response.text();
            const payload = rawText ? JSON.parse(rawText) : {};
            if (!response.ok) {
                throw providerErrorFromResponse(request.profile, response.status, payload);
            }
            return resultFromParts({
                parts: parseOpenAIChatParts(payload),
                usage: asRecord(payload.usage),
                raw: nativeTools ? payload : { ...payload, native_tools_fallback: true },
                finish_reason: finishReasonFromOpenAIChat(payload),
                empty_details: providerResponseDiagnostics(runtime, payload, rawText, attempt)
            });
        };
        return this.recoverNativeToolUnsupported(request, (attempt) => completeWithBody(attempt, true), (attempt) => completeWithBody(attempt, false));
    }
    async completeOpenAIResponses(request, runtime) {
        const authHeader = credentialAuthHeader(request.profile);
        const inference = asRecord(request.profile.inference);
        const body = {
            model: runtime.model,
            input: buildOpenAIResponsesInput(request),
            temperature: asNumber(inference.temperature) ?? 0.2,
            max_output_tokens: asNumber(inference.max_output_tokens) ?? asNumber(inference.max_tokens) ?? asNumber(inference.maxTokens) ?? 2048,
            ...openAIResponsesToolOptions(request)
        };
        return this.retryProviderEmptyResponse(async (attempt) => {
            const response = await this.fetchImpl(runtime.endpoint, {
                method: 'POST',
                headers: jsonProviderHeaders(request.profile, authHeader, { 'X-Title': 'EasyDo Page Assistant' }),
                body: JSON.stringify(body)
            });
            const rawText = await response.text();
            const payload = rawText ? JSON.parse(rawText) : {};
            if (!response.ok) {
                throw providerErrorFromResponse(request.profile, response.status, payload);
            }
            return resultFromParts({
                parts: parseOpenAIResponsesParts(payload),
                usage: asRecord(payload.usage),
                raw: payload,
                finish_reason: firstString(payload.status),
                empty_details: providerResponseDiagnostics(runtime, payload, rawText, attempt)
            });
        });
    }
    async completeAnthropicMessages(request, runtime) {
        const credential = resolveCredential(request.profile);
        const inference = asRecord(request.profile.inference);
        const anthropicMessages = buildAnthropicMessages(request);
        const body = {
            model: runtime.model,
            system: anthropicMessages.system,
            messages: anthropicMessages.messages,
            temperature: asNumber(inference.temperature) ?? 0.2,
            max_tokens: asNumber(inference.max_tokens) ?? asNumber(inference.maxTokens) ?? 2048,
            ...anthropicToolOptions(request)
        };
        return this.retryProviderEmptyResponse(async (attempt) => {
            const response = await this.fetchImpl(runtime.endpoint, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Accept': 'application/json',
                    'Accept-Encoding': 'identity',
                    'anthropic-version': '2023-06-01',
                    ...providerConfiguredHeaders(request.profile),
                    'x-api-key': credential
                },
                body: JSON.stringify(body)
            });
            const rawText = await response.text();
            const payload = rawText ? JSON.parse(rawText) : {};
            if (!response.ok) {
                throw providerErrorFromResponse(request.profile, response.status, payload);
            }
            return resultFromParts({
                parts: parseAnthropicParts(payload),
                usage: asRecord(payload.usage),
                raw: payload,
                finish_reason: firstString(payload.stop_reason),
                empty_details: providerResponseDiagnostics(runtime, payload, rawText, attempt)
            });
        });
    }
    async completeGeminiGenerateContent(request, runtime) {
        const credential = resolveCredential(request.profile);
        const inference = asRecord(request.profile.inference);
        const body = {
            contents: buildGeminiContents(request),
            generationConfig: {
                temperature: asNumber(inference.temperature) ?? 0.2,
                maxOutputTokens: asNumber(inference.max_output_tokens) ?? asNumber(inference.max_tokens) ?? asNumber(inference.maxTokens) ?? 2048
            },
            ...geminiToolOptions(request)
        };
        return this.retryProviderEmptyResponse(async (attempt) => {
            const response = await this.fetchImpl(runtime.endpoint, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Accept': 'application/json',
                    'Accept-Encoding': 'identity',
                    ...providerConfiguredHeaders(request.profile),
                    'x-goog-api-key': credential
                },
                body: JSON.stringify(body)
            });
            const rawText = await response.text();
            const payload = rawText ? JSON.parse(rawText) : {};
            if (!response.ok) {
                throw providerErrorFromResponse(request.profile, response.status, payload);
            }
            const candidates = Array.isArray(payload.candidates) ? payload.candidates : [];
            return resultFromParts({
                parts: parseGeminiParts(payload),
                usage: asRecord(payload.usageMetadata || payload.usage),
                raw: payload,
                finish_reason: firstString(asRecord(candidates[0]).finishReason),
                empty_details: providerResponseDiagnostics(runtime, payload, rawText, attempt)
            });
        });
    }
    async *stream(request) {
        const runtime = this.registry.resolve(request.profile);
        if (!runtime)
            return;
        const context = providerLogContext(request, runtime, 'stream');
        this.logger.info({ ...context, outcome: 'started' });
        try {
            if (runtime.endpoint_dialect !== 'openai-chat') {
                const completion = await this.complete(request);
                for (const part of completion?.parts || []) {
                    if (part.type === 'reasoning')
                        yield { type: 'reasoning_delta', delta: part.text, raw: part.raw };
                    if (part.type === 'text')
                        yield { type: 'answer_delta', delta: part.text, raw: part.raw };
                    if (part.type === 'tool_call')
                        yield { type: 'tool_call', tool_call: part.tool_call, raw: part.raw };
                }
                this.logger.info({ ...context, outcome: 'completed' });
                return;
            }
            const authHeader = credentialAuthHeader(request.profile);
            const inference = asRecord(request.profile.inference);
            const buildBody = (nativeTools) => ({
                model: runtime.model,
                messages: buildMessages(request, nativeTools ? [] : [textToolProtocolInstruction(request)]),
                temperature: asNumber(inference.temperature) ?? 0.2,
                max_tokens: asNumber(inference.max_tokens) ?? asNumber(inference.maxTokens) ?? 2048,
                stream: true,
                ...reasoningOptions(request.profile),
                ...responseFormatOptions(request),
                ...(nativeTools ? toolOptions(request) : {})
            });
            const fetchStream = async (nativeTools) => {
                const response = await this.fetchImpl(runtime.endpoint, {
                    method: 'POST',
                    headers: jsonProviderHeaders(request.profile, authHeader, { 'X-Title': 'EasyDo Page Assistant' }),
                    body: JSON.stringify(buildBody(nativeTools))
                });
                if (!response.ok) {
                    const rawText = await response.text();
                    const payload = rawText ? JSON.parse(rawText) : {};
                    throw providerErrorFromResponse(request.profile, response.status, payload);
                }
                return response;
            };
            let response;
            try {
                response = await fetchStream(true);
            }
            catch (error) {
                if (!isNativeToolUnsupportedError(error) || !textToolProtocolInstruction(request))
                    throw error;
                response = await fetchStream(false);
            }
            yield* parseOpenAICompatibleStream(response);
            this.logger.info({ ...context, outcome: 'completed' });
        }
        catch (error) {
            const descriptor = classifyRuntimeError(error, { source: 'provider' });
            this.logger.error({ ...context, outcome: descriptor.terminal_status, code: descriptor.code, category: descriptor.category });
            throw error;
        }
    }
}
function providerLogContext(request, runtime, operation) {
    return {
        component: 'model-provider',
        operation,
        request_id: request.request_id,
        workspace_id: request.session.workspace_id || request.profile.workspace_id,
        session_id: request.session.id,
        runtime_run_id: request.runtime_run_id,
        parent_runtime_run_id: request.parent_runtime_run_id,
        provider_id: providerType(request.profile) || runtime.provider_family
    };
}
export class FallbackChatModelClient {
    async complete(request) {
        return { text: `Assistant response for: ${request.content}` };
    }
    async *stream(request) {
        yield { type: 'answer_delta', delta: `Assistant response for: ${request.content}` };
    }
}
export class CompositeChatModelClient {
    clients;
    constructor(clients) {
        this.clients = clients;
    }
    async complete(request) {
        for (const client of this.clients) {
            const result = await client.complete(request);
            if (result)
                return result;
        }
        throw new ModelProviderError('model_provider_unsupported', 'No chat model provider supports this agent profile', 400);
    }
    async *stream(request) {
        for (const client of this.clients) {
            if (!client.stream)
                continue;
            let emitted = false;
            for await (const event of client.stream(request)) {
                emitted = true;
                yield event;
            }
            if (emitted)
                return;
        }
    }
}
export function createDefaultChatModelClient() {
    return new CompositeChatModelClient([
        new ProviderAdapterChatModelClient()
    ]);
}
