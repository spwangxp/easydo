import { describe, expect, it } from 'vitest';
import { ProviderAdapterChatModelClient } from './modelProvider.js';
import { createRuntimeLogger } from '../observability/runtimeLogger.js';
function openAIProfile() {
    return {
        id: 1,
        workspace_id: 11,
        name: 'OpenAI compatible agent',
        description: '',
        profile_kind: 'common-assistant',
        context_tags: [],
        provider: { provider_type: 'openai-compatible', base_url: 'http://provider.test/v1' },
        model: { provider_model_key: 'test-model' },
        binding: {},
        provider_credential_ref: { api_key: 'test-key' },
        inference: {},
        prompt: {},
        context_contract: {},
        input_schema: {},
        response_mode: 'mixed',
        output_schema: {},
        mcp_servers: [],
        skills: [],
        subagents: [],
        confirmation_policy: {},
        memory_policy: {},
        status: 'draft',
        created_by: 7,
        version: 'latest',
        created_at: '2026-06-05T00:00:00.000Z',
        updated_at: '2026-06-05T00:00:00.000Z'
    };
}
describe('ProviderAdapterChatModelClient', () => {
    it('logs correlated provider outcomes without logging prompts or credentials', async () => {
        const lines = [];
        const client = new ProviderAdapterChatModelClient(async () => new Response(JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'ok' } }] }), {
            status: 200,
            headers: { 'content-type': 'application/json' }
        }), undefined, createRuntimeLogger({ sink: (line) => lines.push(line) }));
        await client.complete({
            profile: openAIProfile(),
            session: { id: 9, workspace_id: 11 },
            history: [],
            content: 'provider prompt secret',
            context_ref: {},
            include_current_page: false,
            request_id: 'req-provider-1',
            runtime_run_id: 'run-provider-1',
            parent_runtime_run_id: 'run-parent-1'
        });
        expect(lines.map((line) => JSON.parse(line))).toEqual([
            expect.objectContaining({
                component: 'model-provider',
                operation: 'complete',
                outcome: 'started',
                request_id: 'req-provider-1',
                workspace_id: 11,
                session_id: 9,
                runtime_run_id: 'run-provider-1',
                parent_runtime_run_id: 'run-parent-1',
                provider_id: 'openai-compatible'
            }),
            expect.objectContaining({ component: 'model-provider', operation: 'complete', outcome: 'completed' })
        ]);
        expect(lines.join('\n')).not.toContain('provider prompt secret');
        expect(lines.join('\n')).not.toContain('test-key');
    });
    it('accepts assistant responses that contain tool calls without text content', async () => {
        const client = new ProviderAdapterChatModelClient(async () => new Response(JSON.stringify({
            choices: [{
                    message: {
                        role: 'assistant',
                        content: null,
                        tool_calls: [{
                                id: 'call-gpu',
                                type: 'function',
                                function: {
                                    name: 'easydo_resource_gpu_usage',
                                    arguments: '{"workspace_id":11,"resource_id":1}'
                                }
                            }]
                    }
                }]
        }), { status: 200, headers: { 'content-type': 'application/json' } }));
        const result = await client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: '查询 GPU',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(result?.text).toBe('');
        expect(result?.tool_calls).toEqual([expect.objectContaining({
                id: 'call-gpu',
                name: 'easydo_resource_gpu_usage',
                arguments: { workspace_id: 11, resource_id: 1 }
            })]);
    });
    it('generates readable bounded tool call ids when provider omits them', async () => {
        const client = new ProviderAdapterChatModelClient(async () => new Response(JSON.stringify({
            choices: [{
                    message: {
                        role: 'assistant',
                        content: null,
                        tool_calls: [{
                                type: 'function',
                                function: {
                                    name: 'easydo_resource_gpu_usage',
                                    arguments: '{"workspace_id":11,"resource_id":1}'
                                }
                            }]
                    }
                }]
        }), { status: 200, headers: { 'content-type': 'application/json' } }));
        const result = await client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: '查询 GPU',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(result?.tool_calls?.[0]?.id).toBe('tc_easydo_resource_gpu_usage_0001');
    });
    it('does not force response_format for a bare object output schema', async () => {
        let requestBody = {};
        const client = new ProviderAdapterChatModelClient(async (_url, init) => {
            requestBody = JSON.parse(String(init?.body || '{}'));
            return new Response(JSON.stringify({
                choices: [{
                        message: {
                            role: 'assistant',
                            content: 'plain text answer'
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        await client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: { type: 'object' },
            developer_instructions: ''
        });
        expect(requestBody.response_format).toBeUndefined();
        expect(JSON.stringify(requestBody.messages)).not.toContain('Final answer must satisfy this JSON schema');
    });
    it('calls custom OpenAI-compatible proxy providers with configured LLM endpoint and headers', async () => {
        let requestURL = '';
        let requestHeaders = {};
        const client = new ProviderAdapterChatModelClient(async (url, init) => {
            requestURL = String(url);
            requestHeaders = Object.fromEntries(new Headers(init?.headers).entries());
            return new Response(JSON.stringify({
                choices: [{
                        finish_reason: 'stop',
                        message: {
                            role: 'assistant',
                            content: 'proxy answer'
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const profile = openAIProfile();
        profile.provider = {
            provider_type: 'custom',
            base_url: 'http://10.159.69.8:8081/v1',
            llm_endpoint: '/chat/completions',
            headers_json: {
                'HTTP-Referer': 'https://easydo.local'
            }
        };
        const result = await client.complete({
            profile,
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(requestURL).toBe('http://10.159.69.8:8081/v1/chat/completions');
        expect(requestHeaders['accept-encoding']).toBe('identity');
        expect(requestHeaders.authorization).toBe('Bearer test-key');
        expect(requestHeaders['http-referer']).toBe('https://easydo.local');
        expect(result?.text).toBe('proxy answer');
    });
    it('uses the configured LLM endpoint for the OpenAI provider type', async () => {
        let requestURL = '';
        let requestBody = {};
        const client = new ProviderAdapterChatModelClient(async (url, init) => {
            requestURL = String(url);
            requestBody = JSON.parse(String(init?.body || '{}'));
            return new Response(JSON.stringify({
                choices: [{
                        finish_reason: 'stop',
                        message: {
                            role: 'assistant',
                            content: 'LLM endpoint answer'
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const profile = openAIProfile();
        profile.provider = {
            provider_type: 'openai',
            base_url: 'http://10.159.69.8:8081/v1',
            llm_endpoint: '/chat/completions'
        };
        const result = await client.complete({
            profile,
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(requestURL).toBe('http://10.159.69.8:8081/v1/chat/completions');
        expect(Array.isArray(requestBody.messages)).toBe(true);
        expect(requestBody.input).toBeUndefined();
        expect(result?.text).toBe('LLM endpoint answer');
    });
    it('keeps Anthropic providers on the messages API when an LLM endpoint is configured', async () => {
        let requestURL = '';
        let requestBody = {};
        const client = new ProviderAdapterChatModelClient(async (url, init) => {
            requestURL = String(url);
            requestBody = JSON.parse(String(init?.body || '{}'));
            return new Response(JSON.stringify({
                content: [{ type: 'text', text: 'anthropic answer' }],
                stop_reason: 'end_turn'
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const profile = openAIProfile();
        profile.provider = {
            provider_type: 'anthropic',
            base_url: 'https://token.sensenova.cn',
            llm_endpoint: '/v1/messages'
        };
        const result = await client.complete({
            profile,
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: 'Generate only the final session title text.'
        });
        expect(requestURL).toBe('https://token.sensenova.cn/v1/messages');
        expect(requestBody.system).toContain('Generate only the final session title text.');
        expect(requestBody.messages).toEqual([expect.objectContaining({ role: 'user', content: 'hello' })]);
        expect(JSON.stringify(requestBody.messages)).not.toContain('"role":"system"');
        expect(result?.text).toBe('anthropic answer');
    });
    it('uses a configured relative LLM endpoint for Gemini providers', async () => {
        let requestURL = '';
        let requestBody = {};
        const client = new ProviderAdapterChatModelClient(async (url, init) => {
            requestURL = String(url);
            requestBody = JSON.parse(String(init?.body || '{}'));
            return new Response(JSON.stringify({
                candidates: [{
                        finishReason: 'STOP',
                        content: {
                            parts: [{ text: 'gemini answer' }]
                        }
                    }],
                usageMetadata: {
                    promptTokenCount: 3,
                    candidatesTokenCount: 2
                }
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const profile = openAIProfile();
        profile.provider = {
            provider_type: 'gemini',
            base_url: 'https://generativelanguage.googleapis.com/v1beta',
            settings_json: JSON.stringify({
                llm_endpoint: '/custom:generateContent'
            })
        };
        profile.model = { provider_model_key: 'gemini-test' };
        const result = await client.complete({
            profile,
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(requestURL).toBe('https://generativelanguage.googleapis.com/v1beta/custom:generateContent');
        expect(Array.isArray(requestBody.contents)).toBe(true);
        expect(result?.text).toBe('gemini answer');
    });
    it('uses provider type and llm endpoint to build Anthropic requests', async () => {
        let requestURL = '';
        let requestBody = {};
        const client = new ProviderAdapterChatModelClient(async (url, init) => {
            requestURL = String(url);
            requestBody = JSON.parse(String(init?.body || '{}'));
            return new Response(JSON.stringify({
                content: [{ type: 'text', text: 'anthropic answer' }],
                stop_reason: 'end_turn'
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const profile = openAIProfile();
        profile.provider = {
            provider_type: 'anthropic',
            base_url: 'https://token.sensenova.cn',
            settings_json: JSON.stringify({
                llm_endpoint: '/v1/messages'
            })
        };
        const result = await client.complete({
            profile,
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(requestURL).toBe('https://token.sensenova.cn/v1/messages');
        expect(requestBody.system).toBeTruthy();
        expect(JSON.stringify(requestBody.messages)).not.toContain('"role":"system"');
        expect(result?.text).toBe('anthropic answer');
    });
    it('retries one empty OpenAI-compatible response before failing the turn', async () => {
        let calls = 0;
        const client = new ProviderAdapterChatModelClient(async () => {
            calls += 1;
            if (calls === 1) {
                return new Response(JSON.stringify({
                    choices: [{
                            finish_reason: 'stop',
                            message: {
                                role: 'assistant',
                                content: ''
                            }
                        }]
                }), { status: 200, headers: { 'content-type': 'application/json' } });
            }
            return new Response(JSON.stringify({
                choices: [{
                        finish_reason: 'stop',
                        message: {
                            role: 'assistant',
                            content: 'answer after retry'
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const result = await client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(calls).toBe(2);
        expect(result?.text).toBe('answer after retry');
    });
    it('keeps safe response diagnostics when empty OpenAI-compatible responses persist', async () => {
        let calls = 0;
        const client = new ProviderAdapterChatModelClient(async () => {
            calls += 1;
            return new Response(JSON.stringify({
                choices: [{
                        finish_reason: 'stop',
                        native_finish_reason: 'empty',
                        message: {
                            role: 'assistant',
                            content: '',
                            reasoning: '',
                            tool_calls: []
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        await expect(client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        })).rejects.toMatchObject({
            code: 'provider_response_empty',
            details: expect.objectContaining({
                attempt: 2,
                endpoint_dialect: 'openai-chat',
                choices_count: 1,
                finish_reason: 'stop',
                native_finish_reason: 'empty',
                content_type: 'string',
                content_length: 0,
                tool_calls_count: 0
            })
        });
        expect(calls).toBe(2);
    });
    it('does not treat reasoning-only provider responses as final assistant output', async () => {
        let calls = 0;
        const client = new ProviderAdapterChatModelClient(async () => {
            calls += 1;
            return new Response(JSON.stringify({
                choices: [{
                        finish_reason: 'stop',
                        message: {
                            role: 'assistant',
                            content: '',
                            reasoning: 'internal reasoning without final answer'
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        await expect(client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: 'hello',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        })).rejects.toMatchObject({
            code: 'provider_response_empty',
            details: expect.objectContaining({
                attempt: 2,
                reasoning_length: 39,
                content_length: 0,
                tool_calls_count: 0
            })
        });
        expect(calls).toBe(2);
    });
    it('falls back to the text tool protocol when an OpenAI-compatible provider rejects native tools', async () => {
        const requestBodies = [];
        const client = new ProviderAdapterChatModelClient(async (_url, init) => {
            const body = JSON.parse(String(init?.body || '{}'));
            requestBodies.push(body);
            if (requestBodies.length === 1) {
                return new Response(JSON.stringify({
                    error: {
                        message: 'No endpoints found that support tool use. Try disabling "easydo_pipeline_list".'
                    }
                }), { status: 400, headers: { 'content-type': 'application/json' } });
            }
            return new Response(JSON.stringify({
                choices: [{
                        finish_reason: 'stop',
                        message: {
                            role: 'assistant',
                            content: JSON.stringify({
                                tool_calls: [{
                                        id: 'text-tool-call',
                                        name: 'easydo_pipeline_list',
                                        operation_type: 'read',
                                        arguments: { workspace_id: 11 }
                                    }]
                            })
                        }
                    }]
            }), { status: 200, headers: { 'content-type': 'application/json' } });
        });
        const result = await client.complete({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: '列出流水线',
            context_ref: {},
            include_current_page: false,
            tools: [{
                    name: 'easydo_pipeline_list',
                    description: 'List pipelines.',
                    input_schema: {
                        type: 'object',
                        properties: { workspace_id: { type: 'integer' } },
                        required: ['workspace_id']
                    }
                }],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        });
        expect(requestBodies).toHaveLength(2);
        expect(requestBodies[0]?.tools).toHaveLength(1);
        expect(requestBodies[1]?.tools).toBeUndefined();
        expect(JSON.stringify(requestBodies[1]?.messages)).toContain('text tool protocol');
        expect(result?.raw?.native_tools_fallback).toBe(true);
        expect(result?.text).toContain('easydo_pipeline_list');
    });
    it('falls back to text tool protocol streaming when native streaming tools are rejected', async () => {
        const requestBodies = [];
        const client = new ProviderAdapterChatModelClient(async (_url, init) => {
            const body = JSON.parse(String(init?.body || '{}'));
            requestBodies.push(body);
            if (requestBodies.length === 1) {
                return new Response(JSON.stringify({
                    error: {
                        message: 'No endpoints found that support tool use. Try disabling "easydo_pipeline_list".'
                    }
                }), { status: 400, headers: { 'content-type': 'application/json' } });
            }
            const payload = {
                choices: [{
                        delta: {
                            content: JSON.stringify({
                                tool_calls: [{
                                        id: 'stream-text-tool-call',
                                        name: 'easydo_pipeline_list',
                                        operation_type: 'read',
                                        arguments: { workspace_id: 11 }
                                    }]
                            })
                        },
                        finish_reason: null
                    }]
            };
            return new Response(`data: ${JSON.stringify(payload)}\n\ndata: [DONE]\n\n`, {
                status: 200,
                headers: { 'content-type': 'text/event-stream' }
            });
        });
        const events = [];
        for await (const event of client.stream?.({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: '列出流水线',
            context_ref: {},
            include_current_page: false,
            tools: [{
                    name: 'easydo_pipeline_list',
                    description: 'List pipelines.',
                    input_schema: {
                        type: 'object',
                        properties: { workspace_id: { type: 'integer' } },
                        required: ['workspace_id']
                    }
                }],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        }) || []) {
            events.push(event);
        }
        expect(requestBodies).toHaveLength(2);
        expect(requestBodies[0]?.tools).toHaveLength(1);
        expect(requestBodies[1]?.tools).toBeUndefined();
        expect(JSON.stringify(requestBodies[1]?.messages)).toContain('text tool protocol');
        expect(events).toEqual([expect.objectContaining({
                type: 'answer_delta',
                delta: expect.stringContaining('easydo_pipeline_list')
            })]);
    });
    it('preserves leading whitespace in streamed reasoning and answer deltas', async () => {
        const chunks = [
            { choices: [{ delta: { reasoning: 'The user says:' }, finish_reason: null }] },
            { choices: [{ delta: { reasoning: ' "add a pipeline".\nI should ask questions.' }, finish_reason: null }] },
            { choices: [{ delta: { content: '先确认：' }, finish_reason: null }] },
            { choices: [{ delta: { content: ' **目标** 和触发方式。' }, finish_reason: null }] }
        ];
        const client = new ProviderAdapterChatModelClient(async () => new Response(`${chunks.map((chunk) => `data: ${JSON.stringify(chunk)}`).join('\n\n')}\n\ndata: [DONE]\n\n`, { status: 200, headers: { 'content-type': 'text/event-stream' } }));
        const events = [];
        for await (const event of client.stream?.({
            profile: openAIProfile(),
            session: {},
            history: [],
            content: '添加流水线',
            context_ref: {},
            include_current_page: false,
            tools: [],
            capabilities: {},
            loaded_skills: [],
            subagent_results: [],
            output_schema: {},
            developer_instructions: ''
        }) || []) {
            events.push(event);
        }
        const deltaEvents = events.filter((event) => event.type === 'reasoning_delta' || event.type === 'answer_delta');
        expect(deltaEvents.map((event) => event.delta).join('')).toBe('The user says: "add a pipeline".\nI should ask questions.先确认： **目标** 和触发方式。');
        expect(deltaEvents).toEqual([
            expect.objectContaining({ type: 'reasoning_delta', delta: 'The user says:' }),
            expect.objectContaining({ type: 'reasoning_delta', delta: ' "add a pipeline".\nI should ask questions.' }),
            expect.objectContaining({ type: 'answer_delta', delta: '先确认：' }),
            expect.objectContaining({ type: 'answer_delta', delta: ' **目标** 和触发方式。' })
        ]);
    });
});
