import { RuntimeDomainError } from './runtimeErrors.js';
export const PAGE_ASSISTANT_PROFILE_NAME = 'page-ai-assistant';
export const PAGE_ASSISTANT_CONTEXT_TAG = 'page-assistant';
const BUILTIN_CONTEXT_TAGS = new Set([
    PAGE_ASSISTANT_CONTEXT_TAG,
    'workspace',
    'mcp-easydo'
]);
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : {};
}
function asString(value) {
    return value === undefined || value === null ? '' : String(value);
}
function firstString(...values) {
    for (const value of values) {
        const stringValue = asString(value).trim();
        if (stringValue)
            return stringValue;
    }
    return '';
}
export function normalizeContextTags(value) {
    if (!Array.isArray(value))
        return [];
    return value.map((item) => asString(item).trim()).filter(Boolean);
}
export function assertKnownContextTags(tags) {
    const unknown = tags.find((tag) => !BUILTIN_CONTEXT_TAGS.has(tag));
    if (unknown) {
        throw new RuntimeDomainError('context_tag_unknown', `Context tag is not registered: ${unknown}`);
    }
}
export function assertProfileContextTags(profileName, tags) {
    assertKnownContextTags(tags);
    const normalizedName = profileName.trim();
    if (normalizedName === PAGE_ASSISTANT_PROFILE_NAME && !tags.includes(PAGE_ASSISTANT_CONTEXT_TAG)) {
        throw new RuntimeDomainError('page_assistant_context_tag_required', 'page-ai-assistant profile must include page-assistant context tag');
    }
    if (normalizedName !== PAGE_ASSISTANT_PROFILE_NAME && tags.includes(PAGE_ASSISTANT_CONTEXT_TAG)) {
        throw new RuntimeDomainError('page_assistant_context_tag_reserved', 'page-assistant context tag is reserved for page-ai-assistant profile');
    }
}
function pageAssistantProvider() {
    return {
        tag: PAGE_ASSISTANT_CONTEXT_TAG,
        required: true,
        resolve(input, order) {
            const contextRef = asRecord(input.payload.context_ref);
            if (asString(contextRef.kind) !== 'current-page') {
                throw new RuntimeDomainError('page_context_required', 'Page assistant requires current-page context_ref');
            }
            if (!firstString(contextRef.route_path, contextRef.route_name)) {
                throw new RuntimeDomainError('page_context_route_required', 'Page assistant context_ref requires route_path or route_name');
            }
            return {
                tag: PAGE_ASSISTANT_CONTEXT_TAG,
                order,
                required: true,
                title: 'Current page',
                content: JSON.stringify(contextRef, null, 2),
                data: {
                    context_ref: contextRef
                }
            };
        }
    };
}
function workspaceProvider() {
    return {
        tag: 'workspace',
        resolve(input, order) {
            return {
                tag: 'workspace',
                order,
                required: false,
                title: 'Workspace',
                content: JSON.stringify({
                    workspace_id: input.actor.workspace_id,
                    workspace_role: input.actor.workspace_role,
                    user_id: input.actor.user_id
                }, null, 2),
                data: {
                    workspace_id: input.actor.workspace_id,
                    workspace_role: input.actor.workspace_role,
                    user_id: input.actor.user_id
                }
            };
        }
    };
}
function easydoMcpProvider() {
    return {
        tag: 'mcp-easydo',
        resolve(_input, order) {
            return {
                tag: 'mcp-easydo',
                order,
                required: false,
                title: 'EasyDo MCP',
                content: 'EasyDo MCP capability context requested by profile context tag.',
                data: {}
            };
        }
    };
}
export class ContextTagRegistry {
    providers = new Map();
    constructor(providers = [
        pageAssistantProvider(),
        workspaceProvider(),
        easydoMcpProvider()
    ]) {
        for (const provider of providers) {
            this.providers.set(provider.tag, provider);
        }
    }
    resolve(tags, input) {
        return this.resolveInternal(tags, input);
    }
    async resolveInternal(tags, input) {
        assertKnownContextTags(tags);
        const fragments = [];
        for (const [index, tag] of tags.entries()) {
            const provider = this.providers.get(tag);
            if (!provider) {
                throw new RuntimeDomainError('context_tag_unknown', `Context tag is not registered: ${tag}`);
            }
            fragments.push(await provider.resolve(input, index));
        }
        return {
            tags: [...tags],
            fragments,
            warnings: []
        };
    }
}
export function composeTaggedUserContent(content, fragments) {
    if (fragments.length === 0)
        return content;
    const context = fragments.map((fragment) => [
        `Context tag: ${fragment.tag}`,
        `Title: ${fragment.title}`,
        fragment.content
    ].join('\n')).join('\n\n');
    return [
        'Runtime context:',
        context,
        '',
        'User message:',
        content
    ].join('\n');
}
