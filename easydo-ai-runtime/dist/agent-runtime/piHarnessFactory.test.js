import { describe, expect, it } from 'vitest';
import { createModels } from '@earendil-works/pi-ai';
import { NodeExecutionEnv } from '@earendil-works/pi-agent-core/node';
import { createPiHarnessFactory, configureEasyDoModels, MissingPiHarnessConfigurationError, modelSelectionFromRuntimeConfig } from './piHarnessFactory.js';
describe('createPiHarnessFactory', () => {
    it('uses the workspace execution environment supplied for the current run', async () => {
        const workspaceEnv = { cwd: '/workspace' };
        const factory = createPiHarnessFactory({ defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }) });
        const harness = await factory({
            sessionID: 'sess-workspace-env',
            runtimeRunID: 'run-workspace-env',
            prompt: 'hello',
            modelConfig: { provider_id: 'openrouter', id: 'qwen/qwen3-coder' },
            executionEnv: workspaceEnv
        });
        expect(harness.env).toBe(workspaceEnv);
        expect(harness.getTools().map((tool) => tool.name))
            .toEqual(expect.arrayContaining(['bash', 'read_file', 'write_file', 'edit_file', 'list_directory', 'file_info']));
    });
    it('fails explicitly when no model provider/id is supplied', async () => {
        const factory = createPiHarnessFactory({ defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }) });
        await expect(factory({ sessionID: 'sess-1', runtimeRunID: 'run-1', prompt: 'hello' })).rejects.toBeInstanceOf(MissingPiHarnessConfigurationError);
    });
    it('normalizes provider, model, credential, and inference config into a Pi model selection', () => {
        expect(modelSelectionFromRuntimeConfig({
            provider: {
                provider_id: 'openrouter',
                base_url: 'https://openrouter.ai/api/v1',
                headers: { 'HTTP-Referer': 'https://easydo.local' }
            },
            model: {
                provider_model_key: 'openrouter/qwen/qwen3-coder',
                api: 'openai-completions',
                context_window: 128000,
                max_tokens: 8192
            },
            credential: { api_key: 'sk-test' },
            inference: { temperature: 0.2 }
        })).toEqual({
            provider_id: 'openrouter',
            id: 'openrouter/qwen/qwen3-coder',
            api: 'openai-completions',
            base_url: 'https://openrouter.ai/api/v1',
            api_key: 'sk-test',
            headers: { 'HTTP-Referer': 'https://easydo.local' },
            context_window: 128000,
            max_tokens: 8192,
            inference: { temperature: 0.2 }
        });
    });
    it('prefers EasyDo runtime provider/model keys over database ids', () => {
        expect(modelSelectionFromRuntimeConfig({
            provider: {
                provider_id: 1,
                provider_type: 'openrouter',
                base_url: 'https://openrouter.ai/api/v1'
            },
            model: {
                model_id: 1,
                provider_model_key: 'google/gemma-4-31b-it:free'
            },
            credential: { credential_id: '1', secret_ref: { token: 'sk-openrouter' } }
        })).toMatchObject({
            provider_id: 'openrouter',
            id: 'google/gemma-4-31b-it:free',
            base_url: 'https://openrouter.ai/api/v1',
            api_key: 'sk-openrouter'
        });
    });
    it('lets an injected registry configure Pi models before resolving the selected model', async () => {
        let configuredProviderID = '';
        let configuredModelID = '';
        const model = {
            id: 'gpt-test',
            name: 'GPT Test',
            api: 'openai-responses',
            provider: 'openai',
            baseUrl: 'https://api.openai.test/v1',
            reasoning: false,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 8192
        };
        const factory = createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            configureModels(models, selection) {
                configuredProviderID = selection.provider_id;
                configuredModelID = selection.id;
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [model],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const harness = await factory({
            sessionID: 'sess-registry',
            runtimeRunID: 'run-registry',
            prompt: 'hello',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test' }
        });
        expect(configuredProviderID).toBe('openai');
        expect(configuredModelID).toBe('gpt-test');
        expect(harness).toBeTruthy();
    });
    it('injects available skills into the Pi system prompt', async () => {
        const model = {
            id: 'gpt-test',
            name: 'GPT Test',
            api: 'openai-responses',
            provider: 'openai',
            baseUrl: 'https://api.openai.test/v1',
            reasoning: false,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 8192
        };
        const factory = createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            configureModels(models, selection) {
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [model],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const harness = await factory({
            sessionID: 'sess-skill-prompt',
            runtimeRunID: 'run-skill-prompt',
            prompt: 'hello',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test' },
            skills: [{
                    name: 'grill-me',
                    description: 'A relentless interview to sharpen a plan or design.',
                    content: 'Ask one question at a time.',
                    filePath: '/easydo/agent-resources/grill-me/SKILL.md'
                }]
        });
        const turnState = await harness.createTurnState();
        expect(turnState.systemPrompt).toContain('<available_skills>');
        expect(turnState.systemPrompt).toContain('<name>grill-me</name>');
        expect(turnState.systemPrompt).toContain('/easydo/agent-resources/grill-me/SKILL.md');
        expect(turnState.systemPrompt).toContain('Available skills and MCP tools are capability directories, not pipelines');
        expect(turnState.systemPrompt).toContain('Do not list skills or MCP tools as pipelines');
        expect(turnState.systemPrompt).not.toContain('Ask one question at a time.');
    });
    it('uses the runtime profile system prompt instead of the generic factory default', async () => {
        const factory = createPiHarnessFactory({ defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }) });
        const harness = await factory({
            sessionID: 'sess-profile-system-prompt',
            runtimeRunID: 'run-profile-system-prompt',
            prompt: 'hello',
            modelConfig: {
                provider_id: 'openrouter',
                id: 'qwen/qwen3-coder'
            },
            systemPrompt: 'USER CONFIGURED SYSTEM PROMPT: only answer from workspace evidence.'
        });
        const turnState = await harness.createTurnState();
        expect(turnState.systemPrompt).toContain('USER CONFIGURED SYSTEM PROMPT: only answer from workspace evidence.');
        expect(turnState.systemPrompt).not.toContain('You are a helpful EasyDo agent runtime.');
    });
    it('tells Pi agents to call matching write or refresh tools instead of asking for prose confirmation', async () => {
        const factory = createPiHarnessFactory({ defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }) });
        const harness = await factory({
            sessionID: 'sess-tool-approval-policy',
            runtimeRunID: 'run-tool-approval-policy',
            prompt: '帮我触发 7023 的 gpu 使用采集',
            modelConfig: {
                provider_id: 'openrouter',
                id: 'qwen/qwen3-coder'
            }
        });
        const turnState = await harness.createTurnState();
        expect(turnState.systemPrompt).toContain('emit the tool_call');
        expect(turnState.systemPrompt).toContain('Do not replace the tool_call with a prose confirmation request');
        expect(turnState.systemPrompt).toContain('The EasyDo runtime will create the approval panel');
    });
    it('reuses the Pi session repo for the same runtime session while isolating different sessions', async () => {
        const model = {
            id: 'gpt-test',
            name: 'GPT Test',
            api: 'openai-responses',
            provider: 'openai',
            baseUrl: 'https://api.openai.test/v1',
            reasoning: false,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 8192
        };
        const factory = createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            configureModels(models, selection) {
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [model],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const firstHarness = await factory({
            sessionID: 'sess-pi-resume',
            runtimeRunID: 'run-pi-resume-1',
            prompt: 'first',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test' }
        });
        await harnessSession(firstHarness).appendCustomEntry('resume_checkpoint', { value: 'kept' });
        const resumedHarness = await factory({
            sessionID: 'sess-pi-resume',
            runtimeRunID: 'run-pi-resume-2',
            prompt: 'second',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test' }
        });
        const isolatedHarness = await factory({
            sessionID: 'sess-pi-isolated',
            runtimeRunID: 'run-pi-isolated',
            prompt: 'isolated',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test' }
        });
        expect(await harnessSession(resumedHarness).getEntries()).toEqual(expect.arrayContaining([
            expect.objectContaining({ type: 'custom', customType: 'resume_checkpoint', data: { value: 'kept' } })
        ]));
        expect(await harnessSession(isolatedHarness).getEntries()).not.toEqual(expect.arrayContaining([
            expect.objectContaining({ type: 'custom', customType: 'resume_checkpoint' })
        ]));
    });
    it('rebuilds a Pi session from canonical EasyDo history after the factory process cache is lost', async () => {
        const model = {
            id: 'gpt-test',
            name: 'GPT Test',
            api: 'openai-responses',
            provider: 'openai',
            baseUrl: 'https://api.openai.test/v1',
            reasoning: false,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 8192
        };
        const makeFactory = () => createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            configureModels(models, selection) {
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [model],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const restartedFactory = makeFactory();
        const harness = await restartedFactory({
            sessionID: 'sess-pi-restarted',
            runtimeRunID: 'run-pi-restarted-2',
            prompt: 'what did I ask before?',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test' },
            history: [
                { role: 'user', content: 'remember alpha', timestamp: 1783760000000, status: 'completed' },
                { role: 'assistant', content: 'alpha remembered', timestamp: 1783760001000, status: 'completed' }
            ]
        });
        const messageEntries = (await harnessSession(harness).getEntries()).filter((entry) => entry.type === 'message');
        expect(messageEntries).toEqual([
            expect.objectContaining({
                type: 'message',
                message: expect.objectContaining({ role: 'user', content: 'remember alpha', timestamp: 1783760000000 })
            }),
            expect.objectContaining({
                type: 'message',
                message: expect.objectContaining({
                    role: 'assistant',
                    content: [{ type: 'text', text: 'alpha remembered' }],
                    provider: 'openai',
                    model: 'gpt-test',
                    stopReason: 'stop',
                    timestamp: 1783760001000
                })
            })
        ]);
    });
    it('versions and compacts canonical history before rebuilding the Pi model context', async () => {
        const model = {
            id: 'gpt-test',
            name: 'GPT Test',
            api: 'openai-responses',
            provider: 'openai',
            baseUrl: 'https://api.openai.test/v1',
            reasoning: false,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 8192
        };
        const factory = createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            historyMaxMessages: 2,
            historyMaxChars: 120,
            configureModels(models, selection) {
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [model],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const history = [
            { role: 'user', content: 'old user message '.repeat(8), timestamp: 1, status: 'completed' },
            { role: 'assistant', content: 'old assistant response '.repeat(8), timestamp: 2, status: 'completed' },
            { role: 'user', content: 'recent user', timestamp: 3, status: 'completed' },
            { role: 'assistant', content: 'recent assistant', timestamp: 4, status: 'completed' }
        ];
        const harness = await factory({
            sessionID: 'sess-pi-compacted',
            runtimeRunID: 'run-pi-compacted',
            prompt: 'continue',
            model: { provider_id: 'openai', id: 'gpt-test' },
            modelConfig: { provider_id: 'openai', id: 'gpt-test', context_window: 4096, max_tokens: 512 },
            history
        });
        const entries = await harnessSession(harness).getEntries();
        expect(entries).toEqual(expect.arrayContaining([
            expect.objectContaining({
                type: 'custom',
                customType: 'context.budget.evaluated',
                data: expect.objectContaining({
                    context_window_tokens: 4096,
                    provider_id: 'openai',
                    model_key: 'gpt-test'
                })
            }),
            expect.objectContaining({
                type: 'custom',
                customType: 'easydo_history_snapshot',
                data: expect.objectContaining({ version: 1, source_message_count: 4, injected_message_count: 2, compacted_message_count: 2, context_window_tokens: 4096 })
            }),
            expect.objectContaining({
                type: 'custom_message',
                customType: 'easydo_history_compaction',
                details: expect.objectContaining({ version: 1, compacted_message_count: 2, context_window_tokens: 4096 })
            })
        ]));
        const context = await harnessSession(harness).buildContext();
        expect(context.messages).toHaveLength(3);
        expect(context.messages[0]).toMatchObject({ role: 'custom', customType: 'easydo_history_compaction' });
        expect(context.messages[1]).toMatchObject({ role: 'user', content: 'recent user' });
        expect(context.messages[2]).toMatchObject({ role: 'assistant', content: [{ type: 'text', text: 'recent assistant' }] });
        expect(context.messages).not.toEqual(expect.arrayContaining([expect.objectContaining({ role: 'toolResult' })]));
    });
    it('uses distinct frozen binding windows for the same model under different providers', async () => {
        const model = {
            id: 'qwen/qwen3',
            name: 'Qwen3',
            api: 'openai-completions',
            provider: 'openrouter',
            baseUrl: 'https://openrouter.ai/api/v1',
            reasoning: false,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 200000,
            maxTokens: 8192
        };
        const history = Array.from({ length: 12 }, (_, index) => ({
            role: (index % 2 === 0 ? 'user' : 'assistant'),
            content: `turn-${index}-${'x'.repeat(1200)}`,
            timestamp: index + 1,
            status: 'completed'
        }));
        const makeFactory = () => createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            historyMaxMessages: 100,
            configureModels(models, selection) {
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [{ ...model, provider: selection.provider_id, id: selection.id }],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const largeWindowHarness = await makeFactory()({
            sessionID: 'sess-provider-window-large',
            runtimeRunID: 'run-provider-window-large',
            prompt: 'continue',
            modelConfig: { provider_id: 'openrouter', id: 'qwen/qwen3', context_window: 131072, max_tokens: 4096 },
            history
        });
        const smallWindowHarness = await makeFactory()({
            sessionID: 'sess-provider-window-small',
            runtimeRunID: 'run-provider-window-small',
            prompt: 'continue',
            modelConfig: { provider_id: 'ollama', id: 'qwen/qwen3', context_window: 4096, max_tokens: 512 },
            history
        });
        const largeEntries = await harnessSession(largeWindowHarness).getEntries();
        const smallEntries = await harnessSession(smallWindowHarness).getEntries();
        const largeBudget = largeEntries.find((entry) => entry.type === 'custom' && entry.customType === 'context.budget.evaluated');
        const smallBudget = smallEntries.find((entry) => entry.type === 'custom' && entry.customType === 'context.budget.evaluated');
        expect(largeBudget?.data).toMatchObject({ provider_id: 'openrouter', model_key: 'qwen/qwen3', context_window_tokens: 131072, should_compact: false });
        expect(smallBudget?.data).toMatchObject({ provider_id: 'ollama', model_key: 'qwen/qwen3', context_window_tokens: 4096, should_compact: true });
        expect(smallEntries.some((entry) => entry.type === 'custom_message' && entry.customType === 'easydo_history_compaction')).toBe(true);
        expect(largeEntries.some((entry) => entry.type === 'custom_message' && entry.customType === 'easydo_history_compaction')).toBe(false);
    });
    it('registers built-in OpenRouter/OpenAI providers and treats model ids as opaque runtime keys', async () => {
        const models = createModels();
        const selection = modelSelectionFromRuntimeConfig({
            provider: { provider_id: 'openrouter' },
            model: { provider_model_key: 'openrouter/qwen/qwen3-coder' },
            credential: { api_key: 'sk-profile' }
        });
        if (!selection)
            throw new Error('selection is required');
        await configureEasyDoModels(models, selection);
        const model = models.getModel('openrouter', 'openrouter/qwen/qwen3-coder');
        const auth = model ? await models.getAuth(model) : undefined;
        expect(model).toMatchObject({ provider: 'openrouter', id: 'openrouter/qwen/qwen3-coder' });
        expect(auth).toMatchObject({ auth: { apiKey: 'sk-profile' } });
    });
    it('preserves OpenRouter router model ids that are real provider model keys', () => {
        expect(modelSelectionFromRuntimeConfig({
            provider: { provider_type: 'openrouter', base_url: 'http://10.159.69.8:8081/v1' },
            model: { provider_model_key: 'openrouter/free' },
            credential: { api_key: 'sk-profile' }
        })).toMatchObject({
            provider_id: 'openrouter',
            id: 'openrouter/free',
            base_url: 'http://10.159.69.8:8081/v1'
        });
    });
    it('overrides built-in OpenRouter catalog model baseUrl with the configured runtime provider API', async () => {
        const models = createModels();
        const selection = modelSelectionFromRuntimeConfig({
            provider: { provider_type: 'openrouter', base_url: 'http://10.159.69.8:8081/v1' },
            model: { provider_model_key: 'nvidia/nemotron-3-ultra-550b-a55b:free' },
            credential: { api_key: 'sk-profile' }
        });
        if (!selection)
            throw new Error('selection is required');
        await configureEasyDoModels(models, selection);
        const model = models.getModel('openrouter', 'nvidia/nemotron-3-ultra-550b-a55b:free');
        expect(model).toMatchObject({
            provider: 'openrouter',
            id: 'nvidia/nemotron-3-ultra-550b-a55b:free',
            baseUrl: 'http://10.159.69.8:8081/v1'
        });
    });
    it('registers third-party Anthropic-compatible providers and synthesizes custom models not in the built-in catalog', async () => {
        const models = createModels();
        const selection = modelSelectionFromRuntimeConfig({
            provider: { provider_type: 'anthropic', base_url: 'https://token.sensenova.cn' },
            model: { provider_model_key: 'sensenova-6.7-flash-lite' },
            credential: { api_key: 'sk-sensenova' }
        });
        if (!selection)
            throw new Error('selection is required');
        await configureEasyDoModels(models, selection);
        const model = models.getModel('anthropic', 'sensenova-6.7-flash-lite');
        expect(model).toMatchObject({
            provider: 'anthropic',
            id: 'sensenova-6.7-flash-lite',
            api: 'anthropic-messages',
            baseUrl: 'https://token.sensenova.cn'
        });
        const auth = model ? await models.getAuth(model) : undefined;
        expect(auth).toMatchObject({ auth: { apiKey: 'sk-sensenova' } });
    });
    it('maps OpenAI-compatible providers to Pi OpenAI chat-completions models', async () => {
        const models = createModels();
        const selection = modelSelectionFromRuntimeConfig({
            provider: { provider_type: 'openai-compatible', base_url: 'https://token.sensenova.cn' },
            model: { provider_model_key: 'sensenova-6.7-flash-lite' },
            credential: { api_key: 'sk-sensenova' }
        });
        if (!selection)
            throw new Error('selection is required');
        expect(selection).toMatchObject({
            provider_id: 'openai',
            id: 'sensenova-6.7-flash-lite',
            api: 'openai-completions',
            base_url: 'https://token.sensenova.cn',
            api_key: 'sk-sensenova',
            compat: {
                supportsStore: false,
                supportsDeveloperRole: false,
                supportsReasoningEffort: false,
                supportsUsageInStreaming: false,
                maxTokensField: 'max_tokens',
                supportsStrictMode: false,
                supportsLongCacheRetention: false
            }
        });
        await configureEasyDoModels(models, selection);
        const model = models.getModel('openai', 'sensenova-6.7-flash-lite');
        expect(model).toMatchObject({
            provider: 'openai',
            id: 'sensenova-6.7-flash-lite',
            api: 'openai-completions',
            baseUrl: 'https://token.sensenova.cn'
        });
        expect(model?.compat).toMatchObject({
            supportsStore: false,
            supportsDeveloperRole: false,
            supportsReasoningEffort: false,
            supportsUsageInStreaming: false,
            maxTokensField: 'max_tokens',
            supportsStrictMode: false,
            supportsLongCacheRetention: false
        });
        const auth = model ? await models.getAuth(model) : undefined;
        expect(auth).toMatchObject({ auth: { apiKey: 'sk-sensenova' } });
    });
    it('dispatches OpenAI-compatible custom models through Pi chat-completions payloads', async () => {
        const models = createModels();
        const selection = modelSelectionFromRuntimeConfig({
            provider: { provider_type: 'openai-compatible', base_url: 'https://token.sensenova.cn/v1' },
            model: { provider_model_key: 'sensenova-6.7-flash-lite' },
            credential: { api_key: 'sk-sensenova' }
        });
        if (!selection)
            throw new Error('selection is required');
        await configureEasyDoModels(models, selection);
        const model = models.getModel('openai', 'sensenova-6.7-flash-lite');
        const provider = models.getProvider('openai');
        if (!model || !provider)
            throw new Error('model and provider are required');
        const result = await provider.streamSimple(model, {
            messages: [{ role: 'user', content: 'hello', timestamp: Date.now() }]
        }, {
            apiKey: 'sk-sensenova',
            onPayload(payload) {
                const record = payload && typeof payload === 'object' ? payload : {};
                throw new Error(`payload_shape:${JSON.stringify({
                    hasMessages: Array.isArray(record.messages),
                    hasInput: record.input !== undefined,
                    stream: record.stream
                })}`);
            }
        }).result();
        expect(result.stopReason).toBe('error');
        expect(result.errorMessage).toContain('payload_shape:{"hasMessages":true,"hasInput":false,"stream":true}');
    });
    it('passes runtime credentials into Pi provider request headers', async () => {
        const factory = createPiHarnessFactory({ defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }) });
        const harness = await factory({
            sessionID: 'sess-provider-auth-header',
            runtimeRunID: 'run-provider-auth-header',
            prompt: 'hello',
            modelConfig: {
                provider_id: 'openrouter',
                id: 'qwen/qwen3-coder',
                api_key: 'sk-profile',
                headers: { 'HTTP-Referer': 'https://easydo.local' }
            }
        });
        expect(harnessStreamOptions(harness).headers).toMatchObject({
            authorization: 'Bearer sk-profile',
            'HTTP-Referer': 'https://easydo.local'
        });
    });
    it('passes profile thinking_level into Pi harness reasoning controls', async () => {
        const model = {
            id: 'openai/gpt-oss-120b:free',
            name: 'GPT OSS 120B',
            api: 'openai-completions',
            provider: 'openrouter',
            baseUrl: 'https://openrouter.ai/api/v1',
            reasoning: true,
            input: ['text'],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 40960
        };
        const factory = createPiHarnessFactory({
            defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }),
            configureModels(models, selection) {
                models.setProvider({
                    id: selection.provider_id,
                    name: selection.provider_id,
                    auth: { apiKey: { name: 'test', resolve: async () => ({ auth: { apiKey: 'test' } }) } },
                    getModels: () => [model],
                    stream: (() => { throw new Error('not used'); }),
                    streamSimple: (() => { throw new Error('not used'); })
                });
            }
        });
        const harness = await factory({
            sessionID: 'sess-thinking-level',
            runtimeRunID: 'run-thinking-level',
            prompt: 'hello',
            modelConfig: {
                provider_id: 'openrouter',
                id: 'openai/gpt-oss-120b:free',
                inference: { thinking_level: 'high' }
            }
        });
        expect(harnessThinkingLevel(harness)).toBe('high');
    });
});
it('disables silent SDK-level retries so provider errors are surfaced as visible events', async () => {
    const factory = createPiHarnessFactory({ defaultExecutionEnv: new NodeExecutionEnv({ cwd: process.cwd() }) });
    const harness = await factory({
        sessionID: 'sess-no-retries',
        runtimeRunID: 'run-no-retries',
        prompt: 'hello',
        modelConfig: {
            provider_id: 'openrouter',
            id: 'qwen/qwen3-coder',
            api_key: 'sk-profile'
        }
    });
    expect(harnessStreamOptions(harness).maxRetries).toBe(0);
});
function harnessSession(harness) {
    const session = harness.session;
    if (!session)
        throw new Error('Pi harness session is unavailable');
    return session;
}
function harnessStreamOptions(harness) {
    const getStreamOptions = harness.getStreamOptions;
    if (!getStreamOptions)
        throw new Error('Pi harness stream options are unavailable');
    return getStreamOptions.call(harness);
}
function harnessThinkingLevel(harness) {
    const getThinkingLevel = harness.getThinkingLevel;
    if (!getThinkingLevel)
        throw new Error('Pi harness thinking level is unavailable');
    return getThinkingLevel.call(harness);
}
