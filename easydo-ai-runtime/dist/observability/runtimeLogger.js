const REDACTED = '[REDACTED]';
const CIRCULAR = '[CIRCULAR]';
const MAX_REDACTION_DEPTH = 12;
const CONTEXT_FIELDS = [
    'component',
    'operation',
    'outcome',
    'request_id',
    'workspace_id',
    'session_id',
    'runtime_run_id',
    'parent_runtime_run_id',
    'provider_id',
    'mcp_server_id',
    'subagent_id',
    'code',
    'category'
];
function normalizedKey(key) {
    return key.toLowerCase().replace(/[^a-z0-9]/g, '');
}
function isSensitiveKey(key) {
    const normalized = normalizedKey(key);
    return normalized.endsWith('key') ||
        normalized === 'auth' ||
        normalized.includes('cookie') ||
        normalized === 'request' ||
        normalized === 'requestbody' ||
        normalized === 'responsebody' ||
        normalized === 'body' ||
        normalized === 'content' ||
        normalized === 'messages' ||
        normalized === 'profile' ||
        normalized.includes('authorization') ||
        normalized.includes('token') ||
        normalized.includes('apikey') ||
        normalized.includes('password') ||
        normalized.includes('passwd') ||
        normalized.includes('secret') ||
        normalized.includes('credential') ||
        normalized.includes('prompt') ||
        normalized.includes('toolresult') ||
        normalized.includes('tooloutput') ||
        normalized.includes('profilesnapshot');
}
function redactString(value) {
    return value
        .replace(/\bBearer\s+[^\s,;]+/gi, `Bearer ${REDACTED}`)
        .replace(/\b(Authorization|Proxy-Authorization)\s*[:=]\s*(?!Bearer\s+\[REDACTED\])(?:Basic\s+)?[^\s,;]+/gi, '$1: [REDACTED]')
        .replace(/\b(token|api[_-]?key|password|passwd|secret|credential)\s*[:=]\s*[^\s,;]+/gi, '$1=[REDACTED]');
}
function redactValue(value, seen, depth) {
    if (depth > MAX_REDACTION_DEPTH)
        return REDACTED;
    if (typeof value === 'string')
        return redactString(value);
    if (typeof value === 'bigint')
        return value.toString();
    if (value === null || typeof value !== 'object')
        return value;
    if (seen.has(value))
        return CIRCULAR;
    seen.add(value);
    if (value instanceof Error) {
        return {
            name: redactString(value.name || 'Error'),
            message: redactString(value.message || 'Runtime operation failed')
        };
    }
    if (Array.isArray(value)) {
        return value.map((item) => redactValue(item, seen, depth + 1));
    }
    const result = {};
    for (const [key, nestedValue] of Object.entries(value)) {
        result[key] = isSensitiveKey(key)
            ? REDACTED
            : redactValue(nestedValue, seen, depth + 1);
    }
    return result;
}
function safeValue(value) {
    return redactValue(value, new WeakSet(), 0);
}
function safeContextValue(value) {
    return ['string', 'number', 'boolean', 'bigint'].includes(typeof value)
        ? safeValue(value)
        : undefined;
}
export function createRuntimeLogger(options = {}) {
    const sink = options.sink ?? ((line) => process.stdout.write(`${line}\n`));
    const currentTime = options.now ?? (() => new Date().toISOString());
    const write = (level, input) => {
        const entry = {
            timestamp: currentTime(),
            level
        };
        for (const field of CONTEXT_FIELDS) {
            const value = input[field];
            const safe = safeContextValue(value);
            if (safe !== undefined && safe !== '')
                entry[field] = safe;
        }
        sink(JSON.stringify(entry));
    };
    return {
        debug: (input) => write('debug', input),
        info: (input) => write('info', input),
        warn: (input) => write('warn', input),
        error: (input) => write('error', input)
    };
}
export const runtimeLogger = createRuntimeLogger();
