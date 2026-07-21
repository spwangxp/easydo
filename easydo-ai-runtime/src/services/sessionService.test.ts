import { describe, expect, it } from 'vitest'
import { createHash } from 'node:crypto'
import { createServer } from 'node:http'
import { ModelProviderError, type ChatModelClient } from './modelProvider.js'
import { AgentProfileService, RuntimeDomainError } from './profileService.js'
import { ActionService, SessionService, type RuntimeStreamEvent, type RuntimeToolCall, type RuntimeToolExecutor } from './sessionService.js'
import { createMemoryRuntimeStore } from '../store/memoryRuntimeStore.js'
import type { AgentResource, RuntimeActor } from '../domain/runtime.js'
import type { AgentHarnessRunInput, AgentHarnessRunResult } from '../agent-runtime/harnessRunner.js'
import { AgentRuntimeProjection } from '../agent-runtime/projection.js'
import type { AgentEventStore } from '../agent-runtime/eventStore.js'
import type { AgentRuntimeEvent } from '../agent-runtime/events.js'
import { PiToolApprovalRequiredError } from '../agent-runtime/piResources.js'
import { RuntimeSourceError } from './runtimeErrorClassifier.js'
import { RuntimeMetrics } from '../observability/runtimeMetrics.js'

const actor: RuntimeActor = {
  user_id: 7,
  username: 'developer',
  system_role: 'user',
  workspace_id: 11,
  workspace_role: 'developer',
  auth_session_id: 'auth-1'
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function stableJSONStringifyForTest(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value)
  if (Array.isArray(value)) return `[${value.map(stableJSONStringifyForTest).join(',')}]`
  const record = value as Record<string, unknown>
  return `{${Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableJSONStringifyForTest(record[key])}`)
    .join(',')}}`
}

function profileSnapshotHashForTest(profile: unknown) {
  return `sha256:${createHash('sha256').update(stableJSONStringifyForTest(profile)).digest('hex')}`
}

function chatClient(text = 'ok'): ChatModelClient {
  return {
    async complete() {
      return { text }
    },
    async *stream() {
      yield { type: 'answer_delta', delta: text }
    }
  }
}

function leakingProviderFailureClient(secret: string): ChatModelClient {
  return {
    async complete() {
      return { text: 'safe generated title' }
    },
    async *stream() {
      throw new ModelProviderError(
        'openrouter_unavailable',
        `503 service unavailable api_key=${secret} token=provider-bearer-secret`,
        503,
        {
          endpoint: 'https://provider.example.test/v1/chat/completions',
          response_body: { api_key: secret, authorization: 'Bearer provider-bearer-secret' }
        }
      )
    }
  }
}

class FakeAgentEventStore implements AgentEventStore {
  private events: AgentRuntimeEvent[] = []
  private listenersBySession = new Map<string, Set<(event: AgentRuntimeEvent) => void | Promise<void>>>()
  private activeRunBySession = new Map<string, string>()

  async append(event: AgentRuntimeEvent) {
    const explicitRunID = firstStringForTest(asRecord(event).runtime_run_id, asRecord(asRecord(event).run).runtime_run_id)
    if (asRecord(event).type === 'run.started' && explicitRunID) this.activeRunBySession.set(event.session_id, explicitRunID)
    const runtimeRunID = explicitRunID || this.activeRunBySession.get(event.session_id)
    const stored = {
      ...event,
      ...(runtimeRunID ? { runtime_run_id: runtimeRunID } : {}),
      seq: this.events.length + 1,
      event_id: event.event_id || `${event.session_id}:${this.events.length + 1}`
    } as AgentRuntimeEvent
    this.events.push(stored)
    for (const listener of this.listenersBySession.get(event.session_id) ?? []) {
      void Promise.resolve(listener(stored))
    }
    return stored
  }

  async replay(sessionID: string, afterSeq = 0) {
    return this.events.filter((event) => event.session_id === sessionID && event.seq > afterSeq)
  }

  async replayRun(runtimeRunID: string, afterSeq = 0) {
    return this.events.filter((event) => asRecord(event).runtime_run_id === runtimeRunID && event.seq > afterSeq)
  }

  async project(sessionID: string) {
    const projection = new AgentRuntimeProjection()
    for (const event of await this.replay(sessionID)) projection.apply(event)
    return projection.getSession(sessionID)
  }

  subscribe(sessionID: string, listener: (event: AgentRuntimeEvent) => void | Promise<void>) {
    const listeners = this.listenersBySession.get(sessionID) ?? new Set<(event: AgentRuntimeEvent) => void | Promise<void>>()
    listeners.add(listener)
    this.listenersBySession.set(sessionID, listeners)
    return () => {
      listeners.delete(listener)
      if (listeners.size === 0) this.listenersBySession.delete(sessionID)
    }
  }
}

function firstStringForTest(...values: unknown[]) {
  return values.find((value) => typeof value === 'string' && value.trim()) as string | undefined
}

function fakePiRunner(store: FakeAgentEventStore, text = 'pi answer') {
  return {
    async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
      const userMessageID = 'pi-user-1'
      const assistantMessageID = 'pi-assistant-1'
      await store.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
      await store.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
      await store.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-1', text })
      await store.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop', tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } }, cost: 0, files: [] })
      return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
    }
  }
}

function failingPiRunnerAfterStart(store: FakeAgentEventStore, failure: string | Error = 'Pi harness requires model.provider_id and model.id') {
  let promptCount = 0
  return {
    async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
      promptCount += 1
      const userMessageID = `pi-user-failed-${promptCount}`
      const assistantMessageID = `pi-assistant-failed-${promptCount}`
      await store.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
      await store.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
      throw failure instanceof Error ? failure : new Error(failure)
    }
  }
}

function nowForTest() {
  return '2026-06-29T00:00:00.000Z'
}

async function waitFor(assertion: () => Promise<void> | void, timeoutMs = 1000) {
  const startedAt = Date.now()
  let lastError: unknown
  while (Date.now() - startedAt < timeoutMs) {
    try {
      await assertion()
      return
    } catch (error) {
      lastError = error
      await new Promise((resolve) => setTimeout(resolve, 10))
    }
  }
  if (lastError) throw lastError
}

function profilePayload(overrides: Record<string, unknown> = {}) {
  return {
    name: 'Draft Common Agent',
    description: 'Common agent for chatbox tests',
    profile_kind: 'generic',
    context_tags: [],
    provider: { provider_id: 'google' },
    model: { provider_model_key: 'google/gemma-4-31b-free' },
    inference: { max_tokens: 20 },
    prompt: { system: 'Answer as a common EasyDo agent.' },
    status: 'draft',
    ...overrides
  }
}

function allowToolPolicy(...toolNames: string[]) {
  return {
    rules: toolNames.map((toolName) => ({
      tool_name: toolName,
      decision: 'allow'
    }))
  }
}

function chatboxSessionPayload(profileID: number, versionID: number | 'latest' | 'draft' = 'draft') {
  return {
    session_kind: 'chat',
    business_type: 'agent_profile',
    business_id: `${profileID}:${versionID}`,
    title: 'Agent Chatbox',
    profile_selection: {
      agent_profile_id: profileID,
      agent_profile_version_id: versionID,
      first_session_timestamp: '2026-06-05T00:00:00.000Z'
    }
  }
}

function automaticTitleSessionPayload(profileID: number, versionID: number | 'latest' | 'draft' = 'draft') {
  const payload = chatboxSessionPayload(profileID, versionID)
  delete (payload as { title?: string }).title
  return payload
}

function pageAssistantPayload(overrides: Record<string, unknown> = {}) {
  return profilePayload({
    name: 'page-ai-assistant',
    description: 'Fixed page assistant profile',
    context_tags: ['page-assistant'],
    prompt: { system: 'Answer with the current EasyDo page context.' },
    ...overrides
  })
}

async function withMcpServer(handler: (req: import('node:http').IncomingMessage, res: import('node:http').ServerResponse) => void | Promise<void>) {
  const server = createServer((req, res) => {
    Promise.resolve(handler(req, res)).catch((error) => {
      res.statusCode = 500
      res.end(error instanceof Error ? error.message : String(error))
    })
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('server address unavailable')
  return {
    url: `http://127.0.0.1:${address.port}`,
    close: () => new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
  }
}

describe('SessionService Agent Profile Chatbox', () => {
  it('creates chatbox sessions with a fallback title until the first user message is summarized', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('unused'))
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))

    const session = await service.getCurrentSession(actor, automaticTitleSessionPayload(profile.id))

    expect(session.title).toMatch(/^新会话 - /)
    expect(session.title_source).toBe('fallback')
    expect(session.title_generated_at).toBeUndefined()
    expect(session.title_generation_error).toBeUndefined()
  })

  it('generates a concise session title from the first user message without blocking the Pi runtime answer', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const titleRequests: string[] = []
    const modelClient: ChatModelClient = {
      async complete(request) {
        titleRequests.push(request.content)
        return { text: '排查流水线失败' }
      },
      async *stream() {
        throw new Error('title generation must not use streaming')
      }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, automaticTitleSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: '帮我排查 demoWorkspace 里最近失败的流水线，并说明失败原因',
      client_entry_id: 'pi-title-entry-1'
    })

    expect(result.assistant_entry.content).toBe('pi answer')
    await waitFor(async () => {
      const updated = await store.getSession(actor.workspace_id, session.id)
      expect(updated?.title).toBe('排查流水线失败')
      expect(updated?.title_source).toBe('generated')
      expect(updated?.title_generated_at).toBeTruthy()
      expect(updated?.title_generation_error).toBeUndefined()
    })
    expect(titleRequests).toHaveLength(1)
    expect(titleRequests[0]).toContain('帮我排查 demoWorkspace')
  })

  it('repairs a failed fallback session title from the first user message when the session is loaded again', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let titleAttempts = 0
    const modelClient: ChatModelClient = {
      async complete() {
        titleAttempts += 1
        throw new Error('NOT_FOUND')
      },
      async *stream() {
        throw new Error('title generation must not use streaming')
      }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, automaticTitleSessionPayload(profile.id))

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: '帮我触发 7023 的 gpu 使用采集',
      client_entry_id: 'pi-title-entry-retry-1'
    })

    await waitFor(async () => {
      const generated = await store.getSession(actor.workspace_id, session.id)
      expect(generated?.title).toBe('触发 7023 的 gpu 使用采集')
      expect(generated?.title_source).toBe('user_fallback')
      expect(generated?.title_generation_error).toBe('NOT_FOUND')
    })
    const generated = await store.getSession(actor.workspace_id, session.id)
    await store.saveSession({
      ...generated!,
      title: '新会话 - 06-05 00:00',
      title_source: 'fallback',
      title_generated_at: undefined,
      title_generation_error: 'NOT_FOUND'
    })

    const reloaded = await service.getSession(actor, session.id)

    expect(reloaded.title).toBe('触发 7023 的 gpu 使用采集')
    expect(reloaded.title_source).toBe('user_fallback')
    expect(reloaded.title_generation_error).toBe('NOT_FOUND')
    expect(titleAttempts).toBe(2)
  })

  it('does not mark provider-echoed user content as a generated session title', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const userMessage = '我现在有一些流水线，想要执行一下e3流水线，你帮我弄吧'
    const modelClient: ChatModelClient = {
      async complete() {
        return { text: userMessage }
      },
      async *stream() {
        throw new Error('title generation must not use streaming')
      }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, automaticTitleSessionPayload(profile.id))

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: userMessage,
      client_entry_id: 'pi-title-entry-echo-1'
    })

    await waitFor(async () => {
      const updated = await store.getSession(actor.workspace_id, session.id)
      expect(updated?.title_source).toBe('user_fallback')
      expect(updated?.title_generation_error).toBe('echoed_user_content')
      expect(updated?.title).toBe('有一些流水线')
      expect(updated?.title).not.toBe(userMessage)
    })
  })

  it('does not replace an explicitly provided manual session title', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let titleCalls = 0
    const modelClient: ChatModelClient = {
      async complete() {
        titleCalls += 1
        return { text: 'should not be used' }
      },
      async *stream() {
        throw new Error('unused')
      }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, {
      ...chatboxSessionPayload(profile.id),
      title: '我的固定会话'
    })

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: '帮我排查失败原因',
      client_entry_id: 'pi-title-manual-1'
    })

    await new Promise((resolve) => setTimeout(resolve, 0))
    const updated = await store.getSession(actor.workspace_id, session.id)
    expect(updated?.title).toBe('我的固定会话')
    expect(updated?.title_source).toBe('manual')
    expect(titleCalls).toBe(0)
  })

  it('routes runtime_engine=pi createEntryRun through the AgentHarnessRunner event core', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const modelClient: ChatModelClient = {
      async complete() {
        throw new Error('legacy model client must not be called')
      },
      async *stream() {
        throw new Error('legacy model client must not be called')
      }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello pi',
      client_entry_id: 'pi-entry-1'
    })

    expect(result.user_entry.content).toBe('hello pi')
    expect(result.assistant_entry.content).toBe('pi answer')
    expect(result.run?.result.runtime_engine).toBe('pi')
    expect(result.run?.result.runtime_events).toEqual([
      expect.objectContaining({ type: 'run.started' }),
      expect.objectContaining({ type: 'context.build_started' }),
      expect.objectContaining({ type: 'capability.snapshot' }),
      expect.objectContaining({ type: 'context.build_completed' }),
      expect.objectContaining({ type: 'model.selected' }),
      expect.objectContaining({ type: 'session.prompted' }),
      expect.objectContaining({ type: 'session.step.started' }),
      expect.objectContaining({ type: 'session.text.ended' }),
      expect.objectContaining({ type: 'session.step.ended' }),
      expect.objectContaining({ type: 'run.completed', status: 'completed' })
    ])
  })

  it('passes canonical prior EasyDo entries into each newly created Pi harness', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const runnerInputs: AgentHarnessRunInput[] = []
    const delegate = fakePiRunner(eventStore, 'pi canonical answer')
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput) {
          runnerInputs.push(input)
          return delegate.prompt(input)
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'remember alpha',
      client_entry_id: 'pi-history-entry-1'
    })
    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'what did I ask before?',
      client_entry_id: 'pi-history-entry-2'
    })

    expect(runnerInputs[0].history).toEqual([])
    expect(runnerInputs[1].history).toEqual([
      expect.objectContaining({ role: 'user', content: 'remember alpha', status: 'completed' }),
      expect.objectContaining({ role: 'assistant', content: 'pi canonical answer', status: 'completed' })
    ])
  })

  it('preserves Pi session runtime events in listed assistant entries', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi listed answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello listed pi',
      client_entry_id: 'pi-entry-listed-1'
    })

    const listedAssistant = (await service.listEntries(actor, session.id))
      .find((entry) => entry.id === result.assistant_entry.id)
    const listedRuntimeEvents = listedAssistant?.output.runtime_events as AgentRuntimeEvent[]
    expect(listedRuntimeEvents.map((event) => event.type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.text.ended',
      'session.step.ended',
      'run.completed'
    ])
  })

  it('keeps token-level Pi deltas out of embedded entry and run runtime event summaries', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-delta-summary-1'
          const assistantMessageID = 'pi-assistant-delta-summary-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent })
          await eventStore.append({ type: 'session.reasoning.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, reasoning_id: 'reasoning-delta-summary-1' })
          await eventStore.append({ type: 'session.reasoning.delta', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, reasoning_id: 'reasoning-delta-summary-1', delta: '先分析' })
          await eventStore.append({ type: 'session.reasoning.delta', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, reasoning_id: 'reasoning-delta-summary-1', delta: '上下文。' })
          await eventStore.append({ type: 'session.reasoning.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, reasoning_id: 'reasoning-delta-summary-1', text: '先分析上下文。' })
          await eventStore.append({ type: 'session.text.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'text-delta-summary-1' })
          await eventStore.append({ type: 'session.text.delta', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'text-delta-summary-1', delta: '最终' })
          await eventStore.append({ type: 'session.text.delta', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'text-delta-summary-1', delta: '答案' })
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'text-delta-summary-1', text: '最终答案' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'stream many deltas',
      client_entry_id: 'pi-entry-delta-summary-1'
    })

    const returnedRunEventTypes = (result.run?.result.runtime_events as AgentRuntimeEvent[]).map((event) => event.type)
    expect(returnedRunEventTypes).toContain('session.reasoning.started')
    expect(returnedRunEventTypes).toContain('session.text.ended')
    expect(returnedRunEventTypes).not.toContain('session.reasoning.delta')
    expect(returnedRunEventTypes).not.toContain('session.text.delta')

    const listedAssistant = (await service.listEntries(actor, session.id))
      .find((entry) => entry.id === result.assistant_entry.id)
    const listedEventTypes = ((listedAssistant?.output.runtime_events || []) as AgentRuntimeEvent[]).map((event) => event.type)
    expect(listedEventTypes).not.toContain('session.reasoning.delta')
    expect(listedEventTypes).not.toContain('session.text.delta')

    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
    const replayEventTypes = replay.events.map((event: { event_type: string }) => event.event_type)
    expect(replayEventTypes).toContain('session.reasoning.delta')
    expect(replayEventTypes).toContain('session.text.delta')
  })

  it('replays Pi runtime events through the run event API shape', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi replay answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello replay pi',
      client_entry_id: 'pi-entry-replay-1'
    })
    const runtimeRunID = String(result.run?.runtime_run_id)

    const replay = await service.listRunEventsByRuntimeID(runtimeRunID)

    expect(replay.runtime_run_id).toBe(runtimeRunID)
    expect(replay.events.map((event: { event_type: string }) => event.event_type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.text.ended',
      'session.step.ended',
      'run.completed'
    ])
    const textEvent = replay.events.find((event: { event_type: string }) => event.event_type === 'session.text.ended') as Record<string, unknown> | undefined
    expect(textEvent).toMatchObject({
      type: 'session.text.ended',
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      payload_json: expect.objectContaining({
        type: 'session.text.ended',
        text: 'pi replay answer',
        runtime_run_id: runtimeRunID,
        session_id: session.id
      }),
      display_json: expect.objectContaining({ event_type: 'session.text.ended' })
    })
    expect(String(textEvent?.event_id)).toBe(String(asRecord(textEvent?.payload_json).event_id))

    const after = await service.listRunEventsByRuntimeID(runtimeRunID, String(textEvent?.event_id))
    expect(after.events.map((event: { event_type: string }) => event.event_type)).toEqual(['session.step.ended', 'run.completed'])
  })

  it('keeps historical Pi replay isolated after a second run in the same session', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let promptCount = 0
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          promptCount += 1
          const runtimeRunID = String((input as AgentHarnessRunInput & { runtimeRunID: string }).runtimeRunID)
          const userMessageID = `pi-user-isolated-${promptCount}`
          const assistantMessageID = `pi-assistant-isolated-${promptCount}`
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, runtime_run_id: runtimeRunID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, delivery: 'prompt' } as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, runtime_run_id: runtimeRunID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID } as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, runtime_run_id: runtimeRunID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: `pi-text-isolated-${promptCount}`, text: `answer ${promptCount}` } as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, runtime_run_id: runtimeRunID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' } as AgentRuntimeEvent)
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'first isolated prompt',
      client_entry_id: 'pi-entry-isolated-1'
    })
    const second = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'second isolated prompt',
      client_entry_id: 'pi-entry-isolated-2'
    })

    const firstReplay = await service.listRunEventsByRuntimeID(String(first.run?.runtime_run_id))
    const secondReplay = await service.listRunEventsByRuntimeID(String(second.run?.runtime_run_id))
    expect(firstReplay.events.map((event: { payload_json: Record<string, unknown> }) => event.payload_json.runtime_run_id)).toEqual(
      expect.arrayContaining([first.run?.runtime_run_id])
    )
    expect(firstReplay.events.every((event: { payload_json: Record<string, unknown> }) => event.payload_json.runtime_run_id === first.run?.runtime_run_id)).toBe(true)
    expect(JSON.stringify(firstReplay.events)).not.toContain('second isolated prompt')
    expect(secondReplay.events.every((event: { payload_json: Record<string, unknown> }) => event.payload_json.runtime_run_id === second.run?.runtime_run_id)).toBe(true)
    expect(JSON.stringify(secondReplay.events)).not.toContain('first isolated prompt')
  })

  it('resumes a cross-instance Pi run by polling durable events after a persisted cursor', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const metrics = new RuntimeMetrics({ instanceID: 'runtime-replay-test' })
    const remoteSubscriberStore = {
      append: eventStore.append.bind(eventStore),
      replay: eventStore.replay.bind(eventStore),
      replayRun: eventStore.replayRun.bind(eventStore),
      subscribe: () => () => {}
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {
      runStreamHeartbeatMs: 5,
      runStreamPollMs: 2,
      metrics
    }, undefined, {
      runner: fakePiRunner(eventStore, 'unused'),
      eventStore: remoteSubscriberStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = nowForTest()
    const runtimeRunID = 'r_wb_000001_00resume'
    const assistantEntryID = await store.nextEntryId()
    await store.saveEntry({
      id: assistantEntryID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: { runtime_engine: 'pi' },
      output: { runtime_engine: 'pi', runtime_session_id: 'resume-session', runtime_run_id: runtimeRunID },
      runtime_run_id: runtimeRunID,
      idempotency_key: 'resume-assistant',
      created_at: timestamp,
      updated_at: timestamp
    })
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: session.context_tags,
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      input_entry_id: 1,
      output_entry_id: assistantEntryID,
      request: { runtime_engine: 'pi', runtime_session_id: 'resume-session' },
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })
    const first = await eventStore.append({
      type: 'session.prompted',
      session_id: 'resume-session',
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp,
      message_id: 'resume-user',
      prompt: 'resume me',
      delivery: 'prompt'
    } as AgentRuntimeEvent)
    const second = await eventStore.append({
      type: 'session.step.started',
      session_id: 'resume-session',
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp,
      assistant_message_id: 'resume-assistant',
      parent_message_id: 'resume-user'
    } as AgentRuntimeEvent)

    const iterator = service.streamRunEvents(actor, runtimeRunID, {
      after_event_id: first.event_id
    })[Symbol.asyncIterator]()
    await expect(iterator.next()).resolves.toMatchObject({
      value: {
        event: 'runtime_event',
        data: { event: { event_id: second.event_id, runtime_run_id: runtimeRunID } }
      },
      done: false
    })
    await expect(iterator.next()).resolves.toMatchObject({
      value: {
        event: 'heartbeat',
        data: {
          runtime_run_id: runtimeRunID,
          last_event_id: second.event_id,
          status: 'running'
        }
      },
      done: false
    })
    expect(metrics.latencySummary('ai_runtime_replay_delay_seconds', 5 * 60_000)).toMatchObject({ count: 1 })

    const finalEvent = await eventStore.append({
      type: 'session.step.ended',
      session_id: 'resume-session',
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp,
      assistant_message_id: 'resume-assistant',
      finish_reason: 'stop'
    } as AgentRuntimeEvent)
    await store.saveEntry({
      ...(await store.listEntries(actor.workspace_id, session.id))[0],
      status: 'completed',
      content: 'resumed answer',
      content_blocks: [{ type: 'text', text: 'resumed answer' }],
      updated_at: timestamp
    })
    const activeRun = await store.getRun(actor.workspace_id, runtimeRunID)
    await store.saveRun({
      ...activeRun!,
      status: 'completed',
      result: { text: 'resumed answer', runtime_engine: 'pi', runtime_session_id: 'resume-session' },
      finished_at: timestamp,
      updated_at: timestamp
    })

    await expect(iterator.next()).resolves.toMatchObject({
      value: { event: 'runtime_event', data: { event: { event_id: finalEvent.event_id } } },
      done: false
    })
    await expect(iterator.next()).resolves.toMatchObject({
      value: { event: 'runtime_event', data: { event: { event_type: 'run.completed' } } },
      done: false
    })
    await expect(iterator.next()).resolves.toMatchObject({
      value: { event: 'assistant_entry', data: { entry: { id: assistantEntryID, status: 'completed' } } },
      done: false
    })
    await expect(iterator.next()).resolves.toMatchObject({
      value: { event: 'done', data: { runtime_run_id: runtimeRunID, last_event_id: 'resume-session:4' } },
      done: false
    })
    await expect(iterator.next()).resolves.toEqual({ value: undefined, done: true })
  })

  it('rejects an unknown Pi run event cursor instead of replaying duplicate history', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'unused'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_read_large_report')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = nowForTest()
    const runtimeRunID = 'r_wb_000001_0cursor'
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: session.context_tags,
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      input_entry_id: 1,
      request: { runtime_engine: 'pi', runtime_session_id: 'cursor-session' },
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })

    const iterator = service.streamRunEvents(actor, runtimeRunID, {
      after_event_id: 'cursor-session:999'
    })[Symbol.asyncIterator]()
    await expect(iterator.next()).rejects.toMatchObject({
      code: 'runtime_event_cursor_not_found',
      status: 409
    })
  })

  it('persists and exposes one streaming Assistant entry while a Pi run is active', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let releaseRun: (() => void) | undefined
    const runGate = new Promise<void>((resolve) => { releaseRun = resolve })
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-refresh-recovery'
          const assistantMessageID = 'pi-assistant-refresh-recovery'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID })
          await runGate
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-refresh-recovery', text: 'recovered answer' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const running = service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'keep running across refresh',
      client_entry_id: 'pi-entry-refresh-recovery'
    })

    try {
      await waitFor(async () => {
        expect(await store.findActiveRun(actor.workspace_id, session.id)).toMatchObject({ status: 'running' })
        expect(await store.listEntries(actor.workspace_id, session.id)).toHaveLength(2)
      })
      const activeRun = await store.findActiveRun(actor.workspace_id, session.id)
      const listed = await service.listEntries(actor, session.id)
      const streamingAssistant = listed.find((entry) => entry.role === 'assistant')
      expect(streamingAssistant).toMatchObject({
        status: 'streaming',
        runtime_run_id: activeRun?.runtime_run_id,
        parent_entry_id: listed.find((entry) => entry.role === 'user')?.id
      })
      expect(activeRun?.output_entry_id).toBe(streamingAssistant?.id)
      expect(asRecord(await service.getSession(actor, session.id)).active_run).toMatchObject({
        runtime_run_id: activeRun?.runtime_run_id,
        status: 'running',
        output_entry_id: streamingAssistant?.id
      })

      releaseRun?.()
      const completed = await running
      expect(completed.assistant_entry.id).toBe(streamingAssistant?.id)
      expect(completed.assistant_entry.status).toBe('completed')
      expect(await store.listEntries(actor.workspace_id, session.id)).toHaveLength(2)
      expect(asRecord(await service.getSession(actor, session.id)).active_run).toBeNull()
    } finally {
      releaseRun?.()
      await running.catch(() => null)
    }
  })

  it('finalizes Pi runs as failed when the harness fails after emitting early runtime events', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: failingPiRunnerAfterStart(eventStore),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: {},
      model: {}
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello broken pi',
      client_entry_id: 'pi-entry-config-fail-1'
    })

    expect(first.run?.status).toBe('failed')
    expect(first.run?.error_code).toBe('runtime_validation_failed')
    expect(first.run?.error_msg).toBe('Pi harness requires model.provider_id and model.id')
    expect(first.assistant_entry.entry_type).toBe('error')
    expect(first.assistant_entry.status).toBe('failed')
    expect(first.assistant_entry.output.runtime_engine).toBe('pi')
    expect(first.assistant_entry.output.runtime_session_id).toBe(first.run?.result.runtime_session_id)
    const runtimeEvents = first.run?.result.runtime_events as AgentRuntimeEvent[]
    expect(runtimeEvents.map((event) => event.type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.step.failed',
      'session.error',
      'run.failed'
    ])
    expect(runtimeEvents.find((event) => event.type === 'session.step.failed')).toMatchObject({
      assistant_message_id: 'pi-assistant-failed-1',
      error: {
        type: 'validation',
        code: 'runtime_validation_failed',
        message: 'Pi harness requires model.provider_id and model.id',
        retryable: false
      }
    })
    expect(runtimeEvents.find((event) => event.type === 'session.error')).toMatchObject({
      code: 'runtime_validation_failed',
      category: 'validation',
      message: 'Pi harness requires model.provider_id and model.id',
      retryable: false
    })

    const replay = await service.listRunEventsByRuntimeID(String(first.run?.runtime_run_id))
    expect(replay.events.map((event: { event_type: string }) => event.event_type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.step.failed',
      'session.error',
      'run.failed'
    ])
    expect(replay.events.find((event: { event_type: string }) => event.event_type === 'session.error')).toMatchObject({
      display_json: expect.objectContaining({ event_type: 'session.error', status: 'failed' }),
      payload_json: expect.objectContaining({
        code: 'runtime_validation_failed',
        runtime_run_id: first.run?.runtime_run_id
      })
    })

    const second = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'try again after failed pi',
      client_entry_id: 'pi-entry-config-fail-2'
    })
    expect(second.run?.runtime_run_id).not.toBe(first.run?.runtime_run_id)
    expect(second.run?.status).toBe('failed')
    const secondRuntimeEvents = second.run?.result.runtime_events as AgentRuntimeEvent[]
    expect(secondRuntimeEvents.map((event) => event.type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.step.failed',
      'session.error',
      'run.failed'
    ])
    expect(secondRuntimeEvents.filter((event) => event.type === 'session.error')).toHaveLength(1)
    expect(String(secondRuntimeEvents.find((event) => event.type === 'session.error')?.event_id)).toMatch(
      new RegExp(`^s_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}:\\d+$`)
    )
  })

  it('classifies Pi provider timeouts as a timeout Run with one explicit timeout event', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          await eventStore.append({
            type: 'session.step.started',
            session_id: input.sessionID,
            runtime_run_id: input.runtimeRunID,
            event_id: '',
            seq: 0,
            timestamp: nowForTest(),
            assistant_message_id: 'pi-timeout-assistant'
          })
          throw new RuntimeSourceError({
            source: 'provider',
            code: 'provider_timeout',
            message: 'Request timed out.',
            http_status: 504,
            retryable: true
          })
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'wait for timeout',
      client_entry_id: 'pi-timeout-run-1',
      runtime_request_id: 'req-pi-timeout-1'
    })
    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))

    expect(result.run?.status).toBe('timeout')
    expect(result.run?.request.request_id).toBe('req-pi-timeout-1')
    expect(result.assistant_entry.status).toBe('failed')
    expect(replay.events.filter((event: { event_type: string }) => event.event_type === 'run.timeout')).toHaveLength(1)
    expect(replay.events.at(-1)).toMatchObject({
      event_type: 'run.timeout',
      payload_json: expect.objectContaining({ status: 'timeout', code: 'provider_timeout' })
    })
    expect(replay.events.find((event: { event_type: string }) => event.event_type === 'session.error')).toMatchObject({
      payload_json: expect.objectContaining({
        code: 'provider_timeout',
        category: 'provider_timeout',
        retryable: true,
        http_status: 504,
        terminal_status: 'timeout',
        request_id: 'req-pi-timeout-1'
      })
    })
    expect(result.assistant_entry.output.error).toMatchObject({
      code: 'provider_timeout',
      category: 'provider_timeout',
      retryable: true,
      terminal_status: 'timeout',
      request_id: 'req-pi-timeout-1'
    })
  })

  it('marks transient 429/rate-limit errors as retryable in session.error events', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const modelClient: ChatModelClient = {
      async complete() { throw new Error('unused') },
      async *stream() { throw new Error('unused') }
    }
    const rateLimitMessage = '429 Provider returned error\nopenai/gpt-oss-120b:free is temporarily rate-limited upstream. Please retry shortly.'
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: failingPiRunnerAfterStart(eventStore, new RuntimeSourceError({
        source: 'provider',
        code: 'provider_rate_limit',
        message: rateLimitMessage,
        http_status: 429,
        retryable: true
      })),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: {},
      model: {}
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello rate limited',
      client_entry_id: 'pi-entry-429-1'
    })

    expect(result.run?.status).toBe('failed')
    const runtimeEvents = result.run?.result.runtime_events as AgentRuntimeEvent[]
    const errorEvent = runtimeEvents.find((event) => event.type === 'session.error')
    expect(errorEvent).toMatchObject({
      code: 'provider_rate_limit',
      category: 'provider_rate_limit',
      retryable: true,
      http_status: 429,
      terminal_status: 'failed'
    })
    const stepFailedEvent = runtimeEvents.find((event) => event.type === 'session.step.failed')
    expect(stepFailedEvent?.error?.http_status).toBe(429)
  })

  it('marks non-transient config errors as non-retryable in session.error events', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const modelClient: ChatModelClient = {
      async complete() { throw new Error('unused') },
      async *stream() { throw new Error('unused') }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: failingPiRunnerAfterStart(eventStore, 'Pi harness requires model.provider_id and model.id'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: {},
      model: {}
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello config error',
      client_entry_id: 'pi-entry-config-1'
    })

    expect(result.run?.status).toBe('failed')
    const runtimeEvents = result.run?.result.runtime_events as AgentRuntimeEvent[]
    const errorEvent = runtimeEvents.find((event) => event.type === 'session.error')
    expect(errorEvent).toMatchObject({
      code: 'runtime_validation_failed',
      category: 'validation',
      retryable: false
    })
    expect(errorEvent?.http_status).toBe(400)
  })

  it('uses the Pi harness runner by default when no runtime_engine is provided', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const modelClient: ChatModelClient = {
      async complete() {
        throw new Error('legacy model client must not be called')
      },
      async *stream() {
        throw new Error('legacy model client must not be called')
      }
    }
    const service = new SessionService(store, profiles, modelClient, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'default pi answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'hello default pi',
      client_entry_id: 'pi-entry-default-1'
    })

    expect(result.assistant_entry.content).toBe('default pi answer')
    expect(result.run?.request.runtime_engine).toBe('pi')
    expect(result.run?.result.runtime_engine).toBe('pi')
  })

  it('passes resolved provider, model, credential, and inference config into the Pi harness runner', async () => {
    const store = createMemoryRuntimeStore()
    store.resolveProviderCredentialRef = async () => ({
      credential_id: '123',
      secret_ref: { api_key: 'sk-test' }
    })
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          return fakePiRunner(eventStore, 'pi config answer').prompt(input)
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: {
        provider_id: 'openrouter',
        base_url: 'https://openrouter.ai/api/v1',
        headers: { 'HTTP-Referer': 'https://easydo.local' }
      },
      model: {
        provider_model_key: 'openrouter/qwen/qwen3-coder',
        api: 'openai-completions',
        max_tokens: 8192
      },
      provider_credential_ref: { credential_id: '123' },
      inference: { temperature: 0.2 },
      prompt: { system: 'USER CONFIGURED SYSTEM PROMPT: inspect EasyDo state before answering.' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello pi config',
      client_entry_id: 'pi-entry-config-1'
    })

    expect(capturedInput?.model).toEqual({ provider_id: 'openrouter', id: 'openrouter/qwen/qwen3-coder' })
    expect(capturedInput?.systemPrompt).toBe('USER CONFIGURED SYSTEM PROMPT: inspect EasyDo state before answering.')
    expect(capturedInput?.modelConfig).toEqual({
      provider_id: 'openrouter',
      id: 'openrouter/qwen/qwen3-coder',
      api: 'openai-completions',
      base_url: 'https://openrouter.ai/api/v1',
      api_key: 'sk-test',
      headers: { 'HTTP-Referer': 'https://easydo.local' },
      max_tokens: 8192,
      inference: { temperature: 0.2 }
    })
    expect(result.run?.request.model_config).toEqual({
      provider_id: 'openrouter',
      id: 'openrouter/qwen/qwen3-coder',
      api: 'openai-completions',
      base_url: 'https://openrouter.ai/api/v1',
      has_api_key: true,
      headers: ['HTTP-Referer'],
      context_window: undefined,
      max_tokens: 8192,
      thinking_level: undefined,
      inference: { temperature: 0.2 }
    })
    expect((result.run?.result.runtime_events as Array<{ type: string }>).map((event) => event.type)).toContain('model.selected')
  })

  it('keeps resolved provider credentials ephemeral while hashing the exact persisted Run snapshot', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const apiKey = 'sk-p114-persisted-snapshot-secret'
    const bearerToken = 'p114-persisted-snapshot-token'
    store.resolveProviderCredentialRef = async () => ({
      credential_id: '987',
      secret_ref: { api_key: apiKey, token: bearerToken }
    })
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          return fakePiRunner(eventStore, 'credential isolation answer').prompt(input)
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/test-model' },
      provider_credential_ref: { credential_id: '987' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'verify persisted credential isolation',
      client_entry_id: 'p114-credential-isolation-1'
    })
    if (!result.run) throw new Error('run is required')
    if (!result.run.profile_snapshot) throw new Error('run profile snapshot is required')

    expect(capturedInput?.modelConfig?.api_key).toBe(apiKey)
    expect(result.run.profile_snapshot.provider_credential_ref).toEqual({ credential_id: '987' })
    expect(result.run.agent_profile_snapshot_hash).toBe(profileSnapshotHashForTest(result.run.profile_snapshot))

    const persistedRun = await store.getRun(actor.workspace_id, result.run.runtime_run_id)
    const persistedSession = await store.getSession(actor.workspace_id, session.id)
    const persistedEntries = await store.listEntries(actor.workspace_id, session.id)
    const persistedJSON = JSON.stringify({ persistedRun, persistedSession, persistedEntries })
    expect(persistedJSON).not.toContain(apiKey)
    expect(persistedJSON).not.toContain(bearerToken)
  })

  it('rejects direct provider secrets before saving a Profile', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const directSecret = 'sk-p114-direct-profile-secret'
    await expect(profiles.createProfile(actor, profilePayload({
      provider_credential_ref: { api_key: directSecret }
    }))).rejects.toMatchObject({
      code: 'provider_credential_reference_required',
      status: 400
    } satisfies Partial<RuntimeDomainError>)
    expect(JSON.stringify(await store.listProfiles(actor.workspace_id))).not.toContain(directSecret)
  })

  it('switches provider, model, and thinking level per session without mutating the agent profile default', async () => {
    const store = createMemoryRuntimeStore()
    store.resolveProviderCredentialRef = async (_workspaceID, _providerID, credentialID) => ({
      credential_id: String(credentialID),
      secret_ref: { api_key: String(credentialID) === '456' ? 'sk-session' : 'sk-default' }
    })
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          return fakePiRunner(eventStore, 'session override answer').prompt(input)
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: { provider_id: 'openrouter', base_url: 'https://default.example/v1' },
      model: { provider_model_key: 'openrouter/default-model' },
      provider_credential_ref: { credential_id: '123' },
      inference: { thinking_level: 'low' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const switched = await service.updateSessionModelOverride(actor, session.id, {
      provider: { provider_id: 'anthropic', provider_type: 'anthropic', base_url: 'https://anthropic.example' },
      model: { provider_model_key: 'claude-sonnet-4-5' },
      provider_credential_ref: { credential_id: '456' },
      inference: { thinking_level: 'high' }
    })
    expect(switched.model_config).toMatchObject({
      provider_id: 'anthropic',
      id: 'claude-sonnet-4-5',
      base_url: 'https://anthropic.example',
      has_api_key: false,
      thinking_level: 'high'
    })

    const reloaded = await store.getSession(actor.workspace_id, session.id)
    expect(reloaded?.model_override?.model).toMatchObject({ provider_model_key: 'claude-sonnet-4-5' })

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'hello switched model',
      client_entry_id: 'pi-entry-model-switch-1'
    })

    expect(capturedInput?.model).toEqual({ provider_id: 'anthropic', id: 'claude-sonnet-4-5' })
    expect(capturedInput?.modelConfig).toMatchObject({
      provider_id: 'anthropic',
      id: 'claude-sonnet-4-5',
      base_url: 'https://anthropic.example',
      api_key: 'sk-session',
      thinking_level: 'high'
    })
    expect((await profiles.getProfile(actor, profile.id)).model).toMatchObject({ provider_model_key: 'openrouter/default-model' })
  })

  it('exposes selected skills, MCP tools, and subagent descriptors progressively in the Pi harness runner', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          if (input.skillInvocation?.name) {
            await eventStore.append({
              type: 'skill.used',
              session_id: input.sessionID,
              event_id: '',
              seq: 0,
              timestamp: nowForTest(),
              name: input.skillInvocation.name,
              operation: 'invoked',
              prompt: input.skillInvocation.instructions ?? input.prompt
            } as unknown as AgentRuntimeEvent)
          }
          return fakePiRunner(eventStore, 'pi resources answer').prompt(input)
        }
      },
      eventStore
    })
    const skill = await profiles.createResource(actor, {
      resource_kind: 'skill',
      resource_key: 'debug-skill',
      name: 'Debug Skill',
      description: 'Debugging guidance',
      status: 'active',
      spec: { instructions: 'Inspect evidence before edits.' }
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      description: 'Ops tools',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_resource_list', description: 'List resources' }] }
    })
    const child = await profiles.createProfile(actor, profilePayload({ name: 'Reviewer Agent', description: 'Reviews work' }))
    await profiles.publishProfile(actor, child.id, {})
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      skills: [{ resource_type: 'skill', resource_id: skill.resource_key, config: { auto_load: true } }],
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }],
      subagents: [{ resource_type: 'subagent_profile', resource_id: child.id }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'use Debug Skill skill resources',
      client_entry_id: 'pi-entry-resources-1'
    })

    expect(capturedInput?.skills?.map((skill) => skill.name)).toEqual(['Debug Skill'])
    expect(capturedInput?.tools?.map((tool) => tool.name)).toEqual(['easydo_resource_list', 'easydo_subagent_spawn', 'easydo_skill_load'])
    expect(capturedInput?.skillInvocation).toEqual({ name: 'Debug Skill', instructions: 'use Debug Skill skill resources' })
    expect(result.run?.request.pi_resources).toMatchObject({ skill_count: 1, mcp_tool_count: 1, subagent_count: 1 })
    expect(result.run?.result.pi_resources).toMatchObject({ skill_count: 1, mcp_tool_count: 1, subagent_count: 1 })
    expect(result.assistant_entry.output.pi_resources).toMatchObject({ skill_count: 1, mcp_tool_count: 1, subagent_count: 1 })
    expect(result.run?.result.pi_resources).toMatchObject({
      skills: [expect.objectContaining({ key: 'debug-skill', name: 'Debug Skill' })],
      mcp_tools: [expect.objectContaining({ name: 'easydo_resource_list' })],
      subagents: [expect.objectContaining({ id: child.id, name: 'Reviewer Agent' })]
    })
    const runtimeEvents = result.run?.result.runtime_events as Array<{ type: string }>
    expect(runtimeEvents.map((event) => event.type)).toEqual(expect.arrayContaining([
      'context.build_started',
      'capability.snapshot',
      'mcp.tools.available',
      'skill.available',
      'skill.loaded',
      'skill.used',
      'context.build_completed'
    ]))
    const listedAssistant = (await service.listEntries(actor, session.id)).find((entry) => entry.id === result.assistant_entry.id)
    const listedEventTypes = (listedAssistant?.output.runtime_events as Array<{ type: string }> || []).map((event) => event.type)
    expect(listedEventTypes).toEqual(expect.arrayContaining(['skill.available', 'skill.loaded', 'skill.used', 'mcp.tools.available']))
  })

  it('runs a published profile with the resource content frozen at publish time', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          return fakePiRunner(eventStore, 'published resource answer').prompt(input)
        }
      },
      eventStore
    })
    const skill = await profiles.createResource(actor, {
      resource_kind: 'skill',
      resource_key: 'frozen-skill',
      name: 'Frozen Skill',
      status: 'active',
      version: '1',
      spec: { instructions: 'Published instructions.' }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      skills: [{ resource_type: 'skill', resource_id: skill.resource_key, config: { auto_load: true } }]
    }))
    const published = await profiles.publishProfile(actor, profile.id, {})
    await profiles.updateResource(actor, skill.id, {
      version: '2',
      spec: { instructions: 'Draft instructions changed after publication.' }
    })
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id, published.profile_version_id))

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'use Frozen Skill',
      client_entry_id: 'published-frozen-resource'
    })

    expect(capturedInput?.skills).toEqual([
      expect.objectContaining({ name: 'Frozen Skill', content: 'Published instructions.' })
    ])
    expect(published.snapshot.skills[0]?.resource_version).toBe('1')
    const frozenResources = (published.snapshot as unknown as { frozen_resources?: { skills: AgentResource[] } }).frozen_resources
    expect(frozenResources?.skills[0]).toMatchObject({
      resource_key: 'frozen-skill',
      version: '1',
      spec: { instructions: 'Published instructions.' }
    })
  })

  it('does not load skills or execute MCP tools when the user only asks about pipelines', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          return fakePiRunner(eventStore, 'no pipeline resources are available').prompt(input)
        }
      },
      eventStore
    })
    const skill = await profiles.createResource(actor, {
      resource_kind: 'skill',
      resource_key: 'grill-me',
      name: 'grill-me',
      description: 'A relentless interview to sharpen a plan or design.',
      status: 'active',
      spec: { instructions: 'Run a /grilling session.' }
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      description: 'Ops tools',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_pipeline_list', description: 'List real pipelines' }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      skills: [{ resource_type: 'skill', resource_id: skill.resource_key, config: { auto_load: true } }],
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: '先列一下流水线',
      client_entry_id: 'pi-entry-pipeline-list-1'
    })

    expect(capturedInput?.skillInvocation).toBeUndefined()
    expect(capturedInput?.skills?.[0]).toMatchObject({ name: 'grill-me', content: '' })
    expect(capturedInput?.tools?.map((tool) => tool.name)).toEqual(['easydo_pipeline_list', 'easydo_skill_load'])
    const eventTypes = (result.run?.result.runtime_events as Array<{ type: string }>).map((event) => event.type)
    expect(eventTypes).toEqual(expect.arrayContaining(['skill.available', 'mcp.tools.available']))
    expect(eventTypes).not.toContain('skill.loaded')
    expect(eventTypes).not.toContain('skill.used')
    expect(eventTypes).not.toContain('session.tool.called')
    expect(eventTypes).not.toContain('session.tool.success')
  })

  it('exposes built-in EasyDo MCP tools in Pi resources when the profile references easydo without a stored resource row', async () => {
    const server = createServer((request, response) => {
      let body = ''
      request.on('data', (chunk) => {
        body += String(chunk)
      })
      request.on('end', () => {
        const decoded = JSON.parse(body) as { id?: string | number; method?: string }
        response.setHeader('content-type', 'application/json')
        if (request.url === '/mcp' && decoded.method === 'initialize') {
          response.setHeader('mcp-session-id', 'builtin-session')
          response.end(JSON.stringify({
            jsonrpc: '2.0',
            id: decoded.id,
            result: { protocolVersion: '2025-06-18', capabilities: {} }
          }))
          return
        }
        if (request.url === '/mcp' && decoded.method === 'notifications/initialized') {
          response.statusCode = 202
          response.end()
          return
        }
        if (request.url === '/mcp' && decoded.method === 'tools/list') {
          response.end(JSON.stringify({
            jsonrpc: '2.0',
            id: decoded.id,
            result: {
              tools: [{
                name: 'easydo_pipeline_list',
                description: 'List EasyDo pipelines',
                inputSchema: { type: 'object', properties: {} }
              }]
            }
          }))
          return
        }
        response.statusCode = 404
        response.end(JSON.stringify({ error: { message: 'not found' } }))
      })
    })
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
    try {
      const address = server.address()
      if (!address || typeof address === 'string') throw new Error('test server address is unavailable')
      const store = createMemoryRuntimeStore()
      const eventStore = new FakeAgentEventStore()
      const profiles = new AgentProfileService(store)
      let capturedInput: AgentHarnessRunInput | undefined
      const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {
        easydoServerURL: `http://127.0.0.1:${address.port}`
      }, undefined, {
        runner: {
          async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
            capturedInput = input
            return fakePiRunner(eventStore, 'pipeline tool available').prompt(input)
          }
        },
        eventStore
      })
      const profile = await profiles.createProfile(actor, profilePayload({
        model: { provider_id: 'test', id: 'fake' },
        mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'easydo' }]
      }))
      const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

      const result = await service.createEntryRun(actor, session.id, {
        runtime_engine: 'pi',
        content: '帮我触发一下流水线',
        client_entry_id: 'pi-entry-builtin-easydo-mcp-1'
      })

      expect(capturedInput?.tools?.map((tool) => tool.name)).toEqual(['easydo_pipeline_list'])
      expect(result.run?.request.pi_resources).toMatchObject({
        mcp_tool_count: 1,
        mcp_servers: [expect.objectContaining({ key: 'easydo', name: 'easydo' })],
        mcp_tools: [expect.objectContaining({ name: 'easydo_pipeline_list' })]
      })
      const eventTypes = (result.run?.result.runtime_events as Array<{ type: string }>).map((event) => event.type)
      expect(eventTypes).toEqual(expect.arrayContaining(['capability.snapshot', 'mcp.tools.available']))
    } finally {
      await new Promise<void>((resolve, reject) => {
        server.close((error) => error ? reject(error) : resolve())
      })
    }
  })

  it('wires Pi subagent.spawn to real child run orchestration and stores parent-visible child links', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let childID = 0
    const childModelClient: ChatModelClient = {
      async complete(request) {
        if (String(request.capabilities?.mode || '') === 'subagent') {
          return { text: 'child reviewed patch' }
        }
        return { text: 'parent answer' }
      }
    }
    const service = new SessionService(store, profiles, childModelClient, undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const subagentTool = input.tools?.find((tool) => tool.name === 'easydo_subagent_spawn')
          await subagentTool?.execute('call-subagent-pi-1', { subagent_id: childID, task: 'review patch' })
          return fakePiRunner(eventStore, 'parent used subagent').prompt(input)
        }
      },
      eventStore
    })
    const child = await profiles.createProfile(actor, profilePayload({ name: 'Reviewer Agent', description: 'Reviews work' }))
    await profiles.publishProfile(actor, child.id, {})
    childID = child.id
    const parent = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      subagents: [{ resource_type: 'subagent_profile', resource_id: child.id, config: { mode: 'write' } }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parent.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'spawn reviewer',
      client_entry_id: 'pi-subagent-spawn-1'
    })

    const runtimeEvents = result.run?.result.runtime_events as Array<{ type: string; structured?: Record<string, unknown>; payload?: Record<string, unknown> }>
    const toolSuccess = runtimeEvents.find((event) => event.type === 'session.tool.success')
    expect(toolSuccess?.structured).toMatchObject({
      status: 'completed',
      subagent_id: child.id,
      child_run_link_id: expect.any(String),
      child_runtime_run_id: expect.any(String)
    })
    const childRun = await store.getRun(actor.workspace_id, String(toolSuccess?.structured?.child_runtime_run_id))
    expect(childRun?.request.mode).toBe('write')
    const subagentEventTypes = runtimeEvents.filter((event) => event.type.startsWith('subagent.')).map((event) => event.type)
    expect(subagentEventTypes[0]).toBe('subagent.spawned')
    expect(subagentEventTypes.at(-1)).toBe('subagent.completed')
    expect(subagentEventTypes.filter((event) => event === 'subagent.progress').length).toBeGreaterThan(0)
    expect(result.assistant_entry.output.runtime_events).toEqual(runtimeEvents)
  })

  it('wires Pi MCP tools to the runtime tool executor and stores tool lifecycle events', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: 'tool ok', structured_content: { ok: true }, metadata: { request_id: 'tool-1' } }
      }
    }
    let capturedInput: AgentHarnessRunInput | undefined
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          capturedInput = input
          await input.tools?.[0].execute('call-pi-tool-1', { value: 'x' })
          return fakePiRunner(eventStore, 'tool answer').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      description: 'Ops tools',
      status: 'active',
      spec: {
        discovered_tools: [{ name: 'easydo_test_read', description: 'Read test data', operation_type: 'read' }],
        tool_permissions: { tools: { easydo_test_read: 'allow' } }
      }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call pi tool',
      client_entry_id: 'pi-entry-tool-exec-1'
    })
    const runtimeSessionID = result.run?.request.runtime_session_id as string
    const runtimeEvents = await eventStore.replay(runtimeSessionID)

    expect(capturedInput?.tools?.map((tool) => tool.name)).toEqual(['easydo_test_read'])
    expect(executed).toEqual([expect.objectContaining({ tool_call_id: 'call-pi-tool-1', tool_name: 'easydo_test_read', arguments: { value: 'x' } })])
    expect(runtimeEvents.map((event) => event.type)).toEqual(expect.arrayContaining(['session.tool.called', 'session.tool.success']))
    expect(runtimeEvents.find((event) => event.type === 'session.tool.success')).toMatchObject({ tool_name: 'easydo_test_read' })
  })

  it('passes discovered MCP server keys from Pi tools into the runtime tool executor', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: 'jira result', structured_content: { ok: true }, metadata: { request_id: 'jira-search' } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const jiraSearch = input.tools?.find((tool) => tool.name === 'search' && tool.description === 'Search Jira')
          expect(jiraSearch).toBeDefined()
          await jiraSearch?.execute('call-pi-jira-search', { q: 'EASYDO-1' })
          return fakePiRunner(eventStore, 'jira answer').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'multi-mcp',
      name: 'Multi MCP',
      description: 'Multiple MCP servers',
      status: 'active',
      spec: {
        discovered_tools: [
          { name: 'search', description: 'Search GitHub', operation_type: 'read', mcp_server_key: 'github' },
          { name: 'search', description: 'Search Jira', operation_type: 'read', mcp_server_key: 'jira' }
        ],
        tool_permissions: {
          rules: [{ id: 'allow-jira-search', match: 'mcp.jira.tools.search.read', decision: 'allow' }]
        }
      }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'search jira',
      client_entry_id: 'pi-entry-tool-server-key'
    })

    expect(executed).toEqual([expect.objectContaining({
      tool_call_id: 'call-pi-jira-search',
      tool_name: 'search',
      mcp_server_key: 'jira',
      arguments: { q: 'EASYDO-1' }
    })])
  })

  it('keeps duplicate MCP tool approvals scoped by server key and capability', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const jiraSearch = input.tools?.find((tool) => tool.name === 'search' && tool.description === 'Search Jira')
          expect(jiraSearch).toBeDefined()
          await jiraSearch?.execute('call-pi-jira-approval', { q: 'EASYDO-2' })
          return fakePiRunner(eventStore, 'blocked').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'multi-mcp-approval',
      name: 'Multi MCP Approval',
      status: 'active',
      spec: {
        discovered_tools: [
          { name: 'search', description: 'Search GitHub', operation_type: 'read', mcp_server_key: 'github', capability: 'tools' },
          { name: 'search', description: 'Search Jira', operation_type: 'write', mcp_server_key: 'jira', capability: 'tools', requires_confirmation: true }
        ],
        tool_permissions: {
          rules: [{ id: 'request-jira-search', match: 'mcp.jira.tools.search.write', decision: 'request' }]
        }
      }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'search jira with approval',
      client_entry_id: 'pi-entry-tool-server-key-approval'
    })
    const approval = result.run?.result.awaiting_approval as Record<string, unknown>

    expect(result.run?.status).toBe('awaiting_decision')
    expect(approval).toMatchObject({
      approval_id: 'approval:call-pi-jira-approval',
      tool_name: 'search',
      permission_key: 'mcp:jira:tools:search:write',
      input_preview: {
        mcp_server_key: 'jira',
        capability: 'tools',
        arguments: { q: 'EASYDO-2' }
      }
    })
  })

  it('persists MCP discovery timeouts before the Pi run continues', async () => {
    const mcpServer = await withMcpServer((_request, response) => {
      setTimeout(() => {
        response.setHeader('content-type', 'application/json')
        response.end('{}')
      }, 100)
    })
    try {
      const store = createMemoryRuntimeStore()
      const eventStore = new FakeAgentEventStore()
      const profiles = new AgentProfileService(store)
      const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
        runner: fakePiRunner(eventStore, 'continued without mcp'),
        eventStore
      })
      const mcp = await profiles.createResource(actor, {
        resource_kind: 'mcp_server',
        resource_key: 'timeout-mcp',
        name: 'Timeout MCP',
        status: 'active',
        spec: {
          mcpServers: {
            timeout: {
              type: 'streamable_http',
              url: `${mcpServer.url}/mcp`,
              timeout_ms: 20
            }
          }
        }
      })
      const profile = await profiles.createProfile(actor, profilePayload({
        model: { provider_id: 'test', id: 'fake' },
        mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
      }))
      const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

      const result = await service.createEntryRun(actor, session.id, {
        runtime_engine: 'pi',
        content: 'continue after mcp timeout',
        client_entry_id: 'pi-entry-mcp-timeout-1'
      })
      const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
      const timeoutEvent = replay.events.find((event: { event_type: string }) => event.event_type === 'mcp.tools_discovery_failed') as Record<string, unknown> | undefined

      expect(timeoutEvent).toMatchObject({
        payload_json: expect.objectContaining({
          mcp_server: 'timeout',
          message: 'MCP request timed out after 20ms'
        })
      })
      const listedAssistant = (await service.listEntries(actor, session.id))
        .find((entry) => entry.id === result.assistant_entry.id)
      const listedTimeoutEvent = (listedAssistant?.output.runtime_events as Array<Record<string, unknown>> || [])
        .find((event) => event.type === 'mcp.tools_discovery_failed')
      expect(listedTimeoutEvent).toMatchObject({
        mcp_server: 'timeout',
        message: 'MCP request timed out after 20ms'
      })
    } finally {
      await mcpServer.close()
    }
  })

  it('marks Pi MCP tool runs as awaiting_decision when approval is required', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let executed = false
    const toolExecutor: RuntimeToolExecutor = {
      async execute() {
        executed = true
        return { content: 'write ok' }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          await input.tools?.[0].execute('call-pi-write-1', { value: 'x' })
          return fakePiRunner(eventStore, 'blocked').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      description: 'Ops tools',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call write tool',
      client_entry_id: 'pi-entry-tool-approval-1'
    })

    const runtimeEvents = await eventStore.replay(`s_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}`)
    expect(executed).toBe(false)
    expect(result.run?.status).toBe('awaiting_decision')
    expect(result.assistant_entry.status).toBe('streaming')
    expect(result.run?.result.awaiting_approval).toMatchObject({ approval_id: 'approval:call-pi-write-1', tool_name: 'easydo_write' })
    expect(runtimeEvents.map((event) => event.type)).toEqual(expect.arrayContaining(['permission.asked', 'run.awaiting_decision']))

    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
    expect(replay.events.map((event: { event_type: string }) => event.event_type)).toEqual(expect.arrayContaining(['permission.asked', 'run.awaiting_decision']))
    const approvalEvent = replay.events.find((event: { event_type: string }) => event.event_type === 'permission.asked') as Record<string, unknown> | undefined
    expect(approvalEvent).toMatchObject({
      payload_json: expect.objectContaining({
        request_id: 'approval:call-pi-write-1',
        call_id: 'call-pi-write-1',
        tool_name: 'easydo_write',
        runtime_run_id: result.run?.runtime_run_id
      }),
      display_json: expect.objectContaining({ event_type: 'permission.asked' })
    })
  })

  it('marks Pi MCP tool runs as awaiting_decision when Pi reports approval as a tool failure', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-swallowed-approval-1'
          const assistantMessageID = 'pi-assistant-swallowed-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-swallowed-approval-1', tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { workspace_id: 1 } } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: 'approval:call-pi-swallowed-approval-1', approval_id: 'approval:call-pi-swallowed-approval-1', call_id: 'call-pi-swallowed-approval-1', tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { workspace_id: 1 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-swallowed-approval-1', error: { type: 'unknown', message: 'Tool easydo_pipeline_run_list requires approval' } })
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-swallowed-approval-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list pipeline runs',
      client_entry_id: 'pi-entry-tool-approval-swallowed-1'
    })

    expect(result.run?.status).toBe('awaiting_decision')
    expect(result.assistant_entry.status).toBe('streaming')
    expect(result.run?.result.awaiting_approval).toMatchObject({
      approval_id: 'approval:call-pi-swallowed-approval-1',
      call_id: 'call-pi-swallowed-approval-1',
      tool_name: 'easydo_pipeline_run_list',
      input: { workspace_id: 1 }
    })
    expect(result.assistant_entry.content).toBe('')
    const persistedEvents = Array.isArray(result.run?.result.runtime_events) ? result.run?.result.runtime_events : []
    expect(persistedEvents.map((event: AgentRuntimeEvent) => event.type)).toEqual(expect.arrayContaining([
      'session.prompted',
      'session.step.started',
      'session.tool.called',
      'permission.asked'
    ]))
    expect(persistedEvents.map((event: AgentRuntimeEvent) => event.type)).not.toEqual(expect.arrayContaining([
      'session.tool.failed',
      'session.text.ended',
      'session.step.ended'
    ]))
    // Approval-wait narration must not become the user-visible assistant answer.
    expect(String(result.assistant_entry.content || '')).not.toMatch(/需要用户审批|等待用户审批/)
    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
    const replayEventTypes = replay.events.map((event: { event_type: string }) => event.event_type)
    expect(replayEventTypes).toEqual(expect.arrayContaining(persistedEvents.map((event: AgentRuntimeEvent) => event.type)))
    expect(replayEventTypes).not.toEqual(expect.arrayContaining([
      'session.tool.failed',
      'session.text.ended',
      'session.step.ended'
    ]))
  })

  it('strips approval-wait narration from Pi awaiting_decision assistant content', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-approval-narration-1'
          const assistantMessageID = 'pi-assistant-approval-narration-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-approval-narration-1', text: '两个 refresh 都需要审批。我需要等待用户审批。' })
          await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-approval-narration-1', tool: 'easydo_resource_base_info_refresh', tool_name: 'easydo_resource_base_info_refresh', input: { resource_id: 2 } } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: 'approval:call-pi-approval-narration-1', approval_id: 'approval:call-pi-approval-narration-1', call_id: 'call-pi-approval-narration-1', tool_name: 'easydo_resource_base_info_refresh', reason: 'Tool easydo_resource_base_info_refresh requires approval', input: { resource_id: 2 }, message: 'Tool easydo_resource_base_info_refresh requires approval' } as unknown as AgentRuntimeEvent)
          throw new PiToolApprovalRequiredError(
            'approval:call-pi-approval-narration-1',
            'call-pi-approval-narration-1',
            'easydo_resource_base_info_refresh',
            'Tool easydo_resource_base_info_refresh requires approval'
          )
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'refresh resource',
      client_entry_id: 'pi-entry-approval-narration-1'
    })

    expect(result.run?.status).toBe('awaiting_decision')
    expect(result.assistant_entry.content).toBe('')
    expect(String(result.run?.result?.text || '')).toBe('')
  })

  it('detects pending Pi approvals that only expose provider tool call ids', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-provider-call-id-1'
          const assistantMessageID = 'pi-assistant-provider-call-id-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, provider_tool_call_id: 'call-provider-only-1', tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { workspace_id: 1 } } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: 'approval:call-provider-only-1', approval_id: 'approval:call-provider-only-1', provider_tool_call_id: 'call-provider-only-1', tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { workspace_id: 1 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, provider_tool_call_id: 'call-provider-only-1', error: { type: 'unknown', message: 'Tool easydo_pipeline_run_list requires approval' } } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-provider-call-id-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list pipeline runs',
      client_entry_id: 'pi-entry-provider-call-id-approval-1'
    })

    expect(result.run?.status).toBe('awaiting_decision')
    expect(result.run?.result.awaiting_approval).toMatchObject({
      approval_id: 'approval:call-provider-only-1',
      call_id: 'call-provider-only-1',
      tool_name: 'easydo_pipeline_run_list'
    })
    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
    const replayEventTypes = replay.events.map((event: { event_type: string }) => event.event_type)
    expect(replayEventTypes).toEqual(expect.arrayContaining(['permission.asked']))
    expect(replayEventTypes).not.toEqual(expect.arrayContaining([
      'session.tool.failed',
      'session.text.ended',
      'session.step.ended'
    ]))
  })

  it('continues Pi MCP tool runs after approval and completes the runtime run', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const initialSecret = 'sk-p114-continuation-initial'
    const rotatedSecret = 'sk-p114-continuation-rotated'
    const overrideSecret = 'sk-p114-continuation-override'
    let resolvedSecret = initialSecret
    store.resolveProviderCredentialRef = async (_workspaceID, _providerID, credentialID) => ({
      credential_id: String(credentialID),
      secret_ref: { api_key: String(credentialID) === '999' ? overrideSecret : resolvedSecret }
    })
    const harnessSecrets: Array<string | undefined> = []
    const harnessModels: Array<string | undefined> = []
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: 'write ok', structured_content: { ok: true }, metadata: { request_id: 'pi-write-1' } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          harnessSecrets.push(input.modelConfig?.api_key)
          harnessModels.push(input.model?.id)
          if (!String(input.prompt).includes('tool_result')) {
            await input.tools?.[0].execute('call-pi-write-1', { value: 'x' })
          }
          return fakePiRunner(eventStore, `final with ${input.prompt}`).prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/test-model' },
      provider_credential_ref: { credential_id: '654' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call write tool',
      client_entry_id: 'pi-entry-tool-approval-continue-1'
    })
    await service.updateSessionModelOverride(actor, session.id, {
      provider: { provider_id: 'anthropic' },
      model: { provider_model_key: 'claude-override-model' },
      provider_credential_ref: { credential_id: '999' }
    })
    resolvedSecret = rotatedSecret

    const continued = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      client_decision_id: 'approve-pi-write-1'
    })

    expect(executed).toEqual([expect.objectContaining({ tool_call_id: 'call-pi-write-1', tool_name: 'easydo_write', arguments: { value: 'x' } })])
    expect(harnessSecrets).toEqual([initialSecret, rotatedSecret])
    expect(harnessModels).toEqual(['openrouter/test-model', 'openrouter/test-model'])
    expect(continued.run.profile_snapshot?.provider_credential_ref).toEqual({ credential_id: '654' })
    expect(continued.run.agent_profile_snapshot_hash).toBe(profileSnapshotHashForTest(continued.run.profile_snapshot))
    expect(JSON.stringify(continued.run)).not.toContain(initialSecret)
    expect(JSON.stringify(continued.run)).not.toContain(rotatedSecret)
    expect(JSON.stringify(continued.run)).not.toContain(overrideSecret)
    expect(continued.run.status).toBe('completed')
    expect(continued.assistant_entry.status).toBe('completed')
    expect(continued.assistant_entry.content).toContain('final with')
    expect(continued.run.result.awaiting_approval).toBeUndefined()
    expect(continued.run.result.tool_results).toEqual([expect.objectContaining({ tool_call_id: 'call-pi-write-1', status: 'completed' })])
    expect((continued.run.result.runtime_events as AgentRuntimeEvent[]).map((event) => event.type)).toEqual(expect.arrayContaining([
      'permission.resolved',
      'session.tool.called',
      'session.tool.success',
      'session.text.ended',
      'session.step.ended'
    ]))

    const replay = await service.listRunEventsByRuntimeID(String(continued.run.runtime_run_id))
    expect(replay.events.map((event: { event_type: string }) => event.event_type)).toEqual(expect.arrayContaining([
      'permission.asked',
      'permission.resolved',
      'session.tool.called',
      'session.tool.success',
      'session.text.ended',
      'session.step.ended'
    ]))
    await expect(service.decidePiApproval(actor, String(continued.run.runtime_run_id), {
      decision: 'approve_once',
      client_decision_id: 'approve-pi-write-1-repeat'
    })).rejects.toMatchObject({
      code: 'pi_approval_not_awaiting_decision',
      status: 409
    } satisfies Partial<RuntimeDomainError>)
  })

  it('rejects a Pi approval before events, state changes, Secret resolution, or tool execution when the Run snapshot hash is tampered', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let credentialResolutions = 0
    let toolExecutions = 0
    store.resolveProviderCredentialRef = async () => {
      credentialResolutions += 1
      return { credential_id: '654', secret_ref: { api_key: 'pi-integrity-secret' } }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), {
      async execute() {
        toolExecutions += 1
        return { content: 'write ok', structured_content: { ok: true } }
      }
    }, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (!String(input.prompt).includes('tool_result')) {
            await input.tools?.[0].execute('call-pi-integrity-1', { value: 'x' })
          }
          return fakePiRunner(eventStore, 'pi integrity answer').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'integrity-mcp',
      name: 'Integrity MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_integrity_write', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/test-model' },
      provider_credential_ref: { credential_id: '654' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call integrity write tool',
      client_entry_id: 'pi-integrity-entry-1'
    })
    const run = first.run!
    expect(run.status).toBe('awaiting_decision')
    const eventsBefore = await eventStore.replayRun(run.runtime_run_id)
    const entriesBefore = await store.listEntries(actor.workspace_id, session.id)
    const resolutionsBefore = credentialResolutions
    await store.saveRun({ ...run, agent_profile_snapshot_hash: `sha256:${'0'.repeat(64)}` })

    await expect(service.decidePiApproval(actor, run.runtime_run_id, {
      decision: 'approve_once',
      client_decision_id: 'approve-pi-integrity-1'
    })).rejects.toMatchObject({
      code: 'runtime_profile_snapshot_integrity_failed',
      status: 500
    } satisfies Partial<RuntimeDomainError>)

    expect(await eventStore.replayRun(run.runtime_run_id)).toEqual(eventsBefore)
    expect(await store.listEntries(actor.workspace_id, session.id)).toEqual(entriesBefore)
    expect((await store.getRun(actor.workspace_id, run.runtime_run_id))?.status).toBe('awaiting_decision')
    expect(credentialResolutions).toBe(resolutionsBefore)
    expect(toolExecutions).toBe(0)
  })

  it('redacts resolved credentials from Pi approval continuation failures', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const secret = 'sk-p114-continuation-failure-secret'
    store.resolveProviderCredentialRef = async () => ({
      credential_id: '741',
      secret_ref: { api_key: secret }
    })
    const service = new SessionService(store, profiles, chatClient('legacy'), {
      async execute() {
        return { content: 'write ok', structured_content: { ok: true } }
      }
    }, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (!String(input.prompt).includes('tool_result')) {
            await input.tools?.[0].execute('call-p114-failure-1', { value: 'x' })
          }
          throw new Error(`provider failed api_key=${secret} token=p114-continuation-token`)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'p114-failure-mcp',
      name: 'P114 Failure MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'p114_write', operation_type: 'write', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/failure-model' },
      provider_credential_ref: { credential_id: '741' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call failing write tool',
      client_entry_id: 'p114-continuation-failure-1'
    })

    const continued = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      client_decision_id: 'p114-continuation-failure-approved'
    })
    const entries = await store.listEntries(actor.workspace_id, session.id)
    const persistedJSON = JSON.stringify({ run: continued.run, entries })

    expect(continued.run.status).toBe('failed')
    expect(persistedJSON).not.toContain(secret)
    expect(persistedJSON).not.toContain('p114-continuation-token')
    expect(continued.run.error_msg).toBe('The AI runtime encountered an internal error.')
  })

  it('uses approve_session grants for later matching Pi tools only in the same Session', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    let callSequence = 0
    const service = new SessionService(store, profiles, chatClient('legacy'), {
      async execute(call) {
        executed.push(call)
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (!String(input.prompt).includes('tool_result')) {
            callSequence += 1
            await input.tools?.[0].execute(`call-session-grant-${callSequence}`, { target_id: 'resource-1' })
          }
          return fakePiRunner(eventStore, `completed ${input.prompt}`).prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server', resource_key: 'grant-mcp', name: 'Grant MCP', status: 'active',
      spec: { discovered_tools: [{ name: 'explicit_write', operation_type: 'write', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi', content: 'first write', client_entry_id: 'pi-session-grant-1'
    })
    await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_session', client_decision_id: 'pi-session-grant-decision-1'
    })

    expect(await store.listSessionPermissionGrants(actor.workspace_id, session.id)).toHaveLength(1)
    const second = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi', content: 'second write', client_entry_id: 'pi-session-grant-2'
    })
    expect(second.run?.status).toBe('completed')
    expect(executed.map((call) => call.tool_call_id)).toEqual(['call-session-grant-1', 'call-session-grant-2'])

    const otherPayload = chatboxSessionPayload(profile.id)
    otherPayload.profile_selection.first_session_timestamp = '2026-06-06T00:00:00.000Z'
    const otherSession = await service.getCurrentSession(actor, otherPayload)
    const third = await service.createEntryRun(actor, otherSession.id, {
      runtime_engine: 'pi', content: 'third write', client_entry_id: 'pi-session-grant-3'
    })
    expect(otherSession.id).not.toBe(session.id)
    expect(third.run?.status).toBe('awaiting_decision')
  })

  it('continues a Pi run from a steer checkpoint without executing or cancelling the blocked tool', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const prompts: string[] = []
    const executed: RuntimeToolCall[] = []
    const service = new SessionService(store, profiles, chatClient('legacy'), {
      async execute(call) {
        executed.push(call)
        return { content: 'unexpected', structured_content: {} }
      }
    }, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          prompts.push(String(input.prompt))
          if (prompts.length === 1) await input.tools?.[0].execute('call-steer-1', { target_id: 'resource-1' })
          return fakePiRunner(eventStore, prompts.length === 1 ? 'blocked' : 'steered result').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server', resource_key: 'steer-mcp', name: 'Steer MCP', status: 'active',
      spec: { discovered_tools: [{ name: 'explicit_write', operation_type: 'write', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi', content: 'write something', client_entry_id: 'pi-steer-1'
    })
    const steered = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'steer', steer_text: 'Do not write. Inspect the current state instead.', client_decision_id: 'pi-steer-decision-1'
    })

    expect(steered.run.status).toBe('completed')
    expect(executed).toEqual([])
    expect(prompts[1]).toContain('Do not write. Inspect the current state instead.')
    const eventTypes = (await eventStore.replayRun(String(first.run?.runtime_run_id))).map((event) => event.type)
    expect(eventTypes).toContain('session.steered')
    expect(eventTypes).not.toContain('run.cancelled')
  })

  it('processes a queued steer from a parent Pi save point and injects it once', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const steerCalls: Array<{ runtimeRunID: string, queueItemID: string, instruction: string }> = []
    const serviceRef: { current?: SessionService } = {}
    let sessionID = 0
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, { autoDrainSessionQueue: false }, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const currentService = serviceRef.current
          if (!currentService) throw new Error('expected session service')
          const queued = await currentService.enqueueSessionQueueItem(actor, sessionID, {
            mode: 'steer',
            content: 'Use the checkpoint instruction',
            client_item_id: 'save-point-steer-1'
          })
          await input.onSafeCheckpoint?.({ runtime_run_id: input.runtimeRunID, checkpoint: 'turn_save_point' })
          await input.onSafeCheckpoint?.({ runtime_run_id: input.runtimeRunID, checkpoint: 'turn_save_point' })
          expect(queued.item.status).toBe('pending')
          return fakePiRunner(eventStore, 'checkpoint response').prompt(input)
        },
        async steer(runtimeRunID, queueItemID, instruction) {
          steerCalls.push({ runtimeRunID, queueItemID, instruction })
          return steerCalls.length === 1 ? 'accepted' : 'already_accepted'
        }
      },
      eventStore
    })
    serviceRef.current = service
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    sessionID = session.id

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi', content: 'start checkpoint run', client_entry_id: 'pi-checkpoint-run-1'
    })

    expect(steerCalls).toHaveLength(1)
    expect(steerCalls[0]).toMatchObject({
      runtimeRunID: result.run?.runtime_run_id,
      instruction: 'Use the checkpoint instruction'
    })
    const queue = await service.listSessionQueueItems(actor, session.id)
    expect(queue.items).toEqual([expect.objectContaining({ status: 'applied' })])
    const events = await eventStore.replayRun(String(result.run?.runtime_run_id))
    expect(events.filter((event) => event.type === 'session.steer.applied')).toHaveLength(1)
  })

  it('keeps Pi runs awaiting when one approval from a multi-tool batch is still unresolved', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (String(input.prompt).includes('tool_result')) {
            return fakePiRunner(eventStore, `continued with ${input.prompt}`).prompt(input)
          }
          const userMessageID = 'pi-user-multi-approval-1'
          const assistantMessageID = 'pi-assistant-multi-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          for (const callID of ['call-pi-multi-1', 'call-pi-multi-2']) {
            await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { pipeline_id: callID.endsWith('1') ? 14 : 13 } } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: `approval:${callID}`, approval_id: `approval:${callID}`, call_id: callID, tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { pipeline_id: callID.endsWith('1') ? 14 : 13 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, error: { type: 'unknown', message: 'Tool easydo_pipeline_run_list requires approval' } })
          }
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-multi-approval-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list two pipeline runs',
      client_entry_id: 'pi-entry-multi-approval-1'
    })

    expect(first.run?.status).toBe('awaiting_decision')
    const firstRun = first.run
    expect(firstRun).toBeTruthy()
    if (!firstRun) throw new Error('expected first run')
    expect(first.run?.result.awaiting_approvals).toEqual([
      expect.objectContaining({ approval_id: 'approval:call-pi-multi-1', call_id: 'call-pi-multi-1' }),
      expect.objectContaining({ approval_id: 'approval:call-pi-multi-2', call_id: 'call-pi-multi-2' })
    ])
    const firstReplayEventTypes = (await service.listRunEventsByRuntimeID(firstRun.runtime_run_id)).events
      .map((event: { event_type: string }) => event.event_type)
    const firstPermissionIndex = firstReplayEventTypes.indexOf('permission.asked')
    expect(firstPermissionIndex).toBeGreaterThanOrEqual(0)
    expect(firstReplayEventTypes.filter((eventType) => eventType === 'permission.asked')).toHaveLength(1)
    expect(firstReplayEventTypes.slice(firstPermissionIndex + 1)).toEqual(['run.awaiting_decision'])
    const noisyHistoricalEvents = await eventStore.replay(`s_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}`)
    const noisyHistoricalEventTypes = noisyHistoricalEvents.map((event) => event.type)
    expect(noisyHistoricalEventTypes.filter((eventType) => eventType === 'permission.asked')).toHaveLength(2)
    expect(noisyHistoricalEventTypes).toEqual(expect.arrayContaining(['session.tool.failed', 'session.text.ended', 'session.step.ended']))
    await store.saveRun({
      ...firstRun,
      result: {
        ...firstRun.result,
        runtime_events: noisyHistoricalEvents
      }
    })
    await store.saveEntry({
      ...first.assistant_entry,
      output: {
        ...first.assistant_entry.output,
        runtime_events: noisyHistoricalEvents
      }
    })
    const listedEntries = await service.listEntries(actor, session.id)
    const listedAssistant = listedEntries.find((entry) => entry.role === 'assistant')
    const listedRuntimeEventTypes = (listedAssistant?.output.runtime_events as AgentRuntimeEvent[]).map((event) => event.type)
    const listedPermissionIndex = listedRuntimeEventTypes.indexOf('permission.asked')
    expect(listedPermissionIndex).toBeGreaterThanOrEqual(0)
    expect(listedRuntimeEventTypes.filter((eventType) => eventType === 'permission.asked')).toHaveLength(1)
    expect(listedRuntimeEventTypes.slice(listedPermissionIndex + 1)).toEqual([])

    const continued = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-multi-2',
      client_decision_id: 'approve-pi-multi-2'
    })

    expect(executed).toEqual([])
    expect(continued.run.status).toBe('awaiting_decision')
    expect(continued.assistant_entry.status).toBe('streaming')
    expect(continued.run.result.awaiting_approval).toMatchObject({ approval_id: 'approval:call-pi-multi-1', call_id: 'call-pi-multi-1' })
    expect(continued.run.result.awaiting_approvals).toEqual([
      expect.objectContaining({ approval_id: 'approval:call-pi-multi-1', call_id: 'call-pi-multi-1' })
    ])
    expect(continued.run.result.tool_results).toBeUndefined()

    const completed = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-multi-1',
      client_decision_id: 'approve-pi-multi-1'
    })

    expect(executed).toEqual([
      expect.objectContaining({ tool_call_id: 'call-pi-multi-1', tool_name: 'easydo_pipeline_run_list', arguments: { pipeline_id: 14 } }),
      expect.objectContaining({ tool_call_id: 'call-pi-multi-2', tool_name: 'easydo_pipeline_run_list', arguments: { pipeline_id: 13 } })
    ])
    expect(completed.run.status).toBe('completed')
    expect(completed.assistant_entry.status).toBe('completed')
    expect(completed.assistant_entry.input.awaiting_approval).toBeUndefined()
    expect(completed.assistant_entry.input.awaiting_approvals).toBeUndefined()
    expect(completed.assistant_entry.output.awaiting_approval).toBeUndefined()
    expect(completed.assistant_entry.output.awaiting_approvals).toBeUndefined()
    expect(completed.run.result.awaiting_approval).toBeUndefined()
    expect(completed.run.result.awaiting_approvals).toBeUndefined()
    expect(completed.run.result.tool_results).toEqual([
      expect.objectContaining({ tool_call_id: 'call-pi-multi-1', status: 'completed' }),
      expect.objectContaining({ tool_call_id: 'call-pi-multi-2', status: 'completed' })
    ])
  })

  it('keeps sequential Pi approvals pending when pause failures use tool descriptions instead of approval phrases', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return {
          content: JSON.stringify({ ok: true, tool: call.tool_name }),
          structured_content: { ok: true, tool: call.tool_name }
        }
      }
    }
    let continuationCount = 0
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (String(input.prompt).includes('tool_result')) {
            continuationCount += 1
            const userMessageID = `pi-user-seq-approval-cont-${continuationCount}`
            const assistantMessageID = `pi-assistant-seq-approval-cont-${continuationCount}`
            await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
            await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
            if (continuationCount === 1) {
              await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-seq-2', tool: 'easydo_pipeline_list', tool_name: 'easydo_pipeline_list', input: { workspace_id: 1, query: 'e1' } } as unknown as AgentRuntimeEvent)
              await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: 'approval:call-pi-seq-2', approval_id: 'approval:call-pi-seq-2', call_id: 'call-pi-seq-2', tool_name: 'easydo_pipeline_list', reason: 'List pipelines in one workspace', input: { workspace_id: 1, query: 'e1' }, message: 'List pipelines in one workspace' } as unknown as AgentRuntimeEvent)
              await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-seq-2', error: { type: 'unknown', message: 'List pipelines in one workspace' } })
              throw new PiToolApprovalRequiredError(
                'approval:call-pi-seq-2',
                'call-pi-seq-2',
                'easydo_pipeline_list',
                'List pipelines in one workspace'
              )
            }
            await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-seq-approval-done', text: 'pipeline listed' })
            await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
            return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
          }
          const userMessageID = 'pi-user-seq-approval-1'
          const assistantMessageID = 'pi-assistant-seq-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-seq-1', tool: 'easydo_workspace_list', tool_name: 'easydo_workspace_list', input: {} } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: 'approval:call-pi-seq-1', approval_id: 'approval:call-pi-seq-1', call_id: 'call-pi-seq-1', tool_name: 'easydo_workspace_list', reason: 'List workspaces the actor can access', input: {}, message: 'List workspaces the actor can access' } as unknown as AgentRuntimeEvent)
          await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: 'call-pi-seq-1', error: { type: 'unknown', message: 'List workspaces the actor can access' } })
          throw new PiToolApprovalRequiredError(
            'approval:call-pi-seq-1',
            'call-pi-seq-1',
            'easydo_workspace_list',
            'List workspaces the actor can access'
          )
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'trigger pipeline e1',
      client_entry_id: 'pi-entry-seq-approval-1'
    })

    expect(first.run?.status).toBe('awaiting_decision')
    expect(first.run?.result.awaiting_approvals).toEqual([
      expect.objectContaining({
        approval_id: 'approval:call-pi-seq-1',
        call_id: 'call-pi-seq-1',
        tool_name: 'easydo_workspace_list'
      })
    ])

    const continued = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-seq-1',
      client_decision_id: 'approve-pi-seq-1'
    })

    expect(executed).toEqual([
      expect.objectContaining({ tool_call_id: 'call-pi-seq-1', tool_name: 'easydo_workspace_list' })
    ])
    expect(continued.run.status).toBe('awaiting_decision')
    expect(continued.run.result.awaiting_approvals).toEqual([
      expect.objectContaining({
        approval_id: 'approval:call-pi-seq-2',
        call_id: 'call-pi-seq-2',
        tool_name: 'easydo_pipeline_list',
        reason: 'List pipelines in one workspace'
      })
    ])
    expect(continued.run.result.awaiting_approval).toMatchObject({
      approval_id: 'approval:call-pi-seq-2',
      call_id: 'call-pi-seq-2'
    })

    const finished = await service.decidePiApproval(actor, String(continued.run.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-seq-2',
      client_decision_id: 'approve-pi-seq-2'
    })

    expect(executed).toEqual([
      expect.objectContaining({ tool_call_id: 'call-pi-seq-1', tool_name: 'easydo_workspace_list' }),
      expect.objectContaining({ tool_call_id: 'call-pi-seq-2', tool_name: 'easydo_pipeline_list' })
    ])
    expect(finished.run.status).toBe('completed')
    expect(finished.run.result.awaiting_approvals).toBeUndefined()
  })

  it('recovers Pi approval decisions for runs that were incorrectly completed with unresolved replay approvals', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (String(input.prompt).includes('tool_result')) {
            return fakePiRunner(eventStore, `recovered with ${input.prompt}`).prompt(input)
          }
          const userMessageID = 'pi-user-corrupt-completed-approval-1'
          const assistantMessageID = 'pi-assistant-corrupt-completed-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          for (const callID of ['call-pi-corrupt-1', 'call-pi-corrupt-2']) {
            await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 } } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: `approval:${callID}`, approval_id: `approval:${callID}`, call_id: callID, tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
          }
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-corrupt-completed-approval-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list two pipeline runs',
      client_entry_id: 'pi-entry-corrupt-completed-approval-1'
    })
    const firstRun = first.run
    expect(firstRun).toBeTruthy()
    if (!firstRun) throw new Error('expected first run')
    await store.saveRun({
      ...firstRun,
      status: 'completed',
      finished_at: nowForTest(),
      result: {
        ...firstRun.result,
        awaiting_approval: {
          approval_id: 'approval:call-pi-corrupt-2',
          call_id: 'call-pi-corrupt-2',
          tool_name: 'easydo_pipeline_run_list',
          reason: 'Tool easydo_pipeline_run_list requires approval',
          input: { pipeline_id: 13 }
        }
      }
    })

    const recovered = await service.decidePiApproval(actor, String(firstRun.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-corrupt-1',
      client_decision_id: 'approve-pi-corrupt-1'
    })

    expect(executed).toEqual([])
    expect(recovered.run.status).toBe('awaiting_decision')
    expect(recovered.run.result.awaiting_approval).toMatchObject({ approval_id: 'approval:call-pi-corrupt-2' })
  })

  it('rejects stale Pi approvals after a previous approval already let the agent loop continue', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-stale-approval-1'
          const assistantMessageID = 'pi-assistant-stale-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          for (const callID of ['call-pi-stale-1', 'call-pi-stale-2']) {
            await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 } } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: `approval:${callID}`, approval_id: `approval:${callID}`, call_id: callID, tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, error: { type: 'unknown', message: 'Tool easydo_pipeline_run_list requires approval' } })
          }
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-stale-approval-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list two pipeline runs',
      client_entry_id: 'pi-entry-stale-approval-1'
    })
    const firstRun = first.run
    expect(firstRun).toBeTruthy()
    if (!firstRun) throw new Error('expected first run')
    const runtimeSessionID = String(firstRun.result.runtime_session_id || '')
    await eventStore.append({ type: 'permission.resolved', session_id: runtimeSessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: 'approval:call-pi-stale-2', approval_id: 'approval:call-pi-stale-2', call_id: 'call-pi-stale-2', tool_name: 'easydo_pipeline_run_list', result: 'approved', decision: 'approve_once' } as unknown as AgentRuntimeEvent)
    await eventStore.append({ type: 'session.tool.called', session_id: runtimeSessionID, event_id: '', seq: 0, timestamp: nowForTest(), call_id: 'call-pi-stale-2', tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { pipeline_id: 13 } } as unknown as AgentRuntimeEvent)
    await eventStore.append({ type: 'session.tool.success', session_id: runtimeSessionID, event_id: '', seq: 0, timestamp: nowForTest(), call_id: 'call-pi-stale-2', tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', result: { content: [{ type: 'text', text: '{"ok":true}' }] } } as unknown as AgentRuntimeEvent)
    await eventStore.append({ type: 'session.step.started', session_id: runtimeSessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: 'pi-assistant-stale-approval-2', model: { provider_id: 'test', id: 'fake' } } as unknown as AgentRuntimeEvent)
    await store.saveRun({
      ...firstRun,
      status: 'completed',
      finished_at: nowForTest(),
      result: {
        ...firstRun.result,
        awaiting_approval: {
          approval_id: 'approval:call-pi-stale-1',
          call_id: 'call-pi-stale-1',
          tool_name: 'easydo_pipeline_run_list',
          reason: 'Tool easydo_pipeline_run_list requires approval',
          input: { pipeline_id: 1 }
        }
      }
    })

    await expect(service.decidePiApproval(actor, String(firstRun.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-stale-1',
      client_decision_id: 'approve-pi-stale-1'
    })).rejects.toMatchObject({
      code: 'pi_approval_not_awaiting_decision',
      status: 409
    } satisfies Partial<RuntimeDomainError>)
    expect(executed).toEqual([])
  })

  it('rejects unknown Pi approval ids instead of approving a different pending tool', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-unknown-approval-1'
          const assistantMessageID = 'pi-assistant-unknown-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          for (const callID of ['call-pi-unknown-1', 'call-pi-unknown-2']) {
            await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 } } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: `approval:${callID}`, approval_id: `approval:${callID}`, call_id: callID, tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, error: { type: 'unknown', message: 'Tool easydo_pipeline_run_list requires approval' } })
          }
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-unknown-approval-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list two pipeline runs',
      client_entry_id: 'pi-entry-unknown-approval-1'
    })

    await expect(service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:does-not-exist',
      client_decision_id: 'approve-pi-unknown'
    })).rejects.toMatchObject({
      code: 'pi_approval_not_awaiting_decision',
      status: 409
    } satisfies Partial<RuntimeDomainError>)
    expect(executed).toEqual([])
  })

  it('serializes concurrent Pi approvals for the same run so tools execute once', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        await new Promise((resolve) => setTimeout(resolve, 10))
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (String(input.prompt).includes('tool_result')) {
            return fakePiRunner(eventStore, `continued with ${input.prompt}`).prompt(input)
          }
          const userMessageID = 'pi-user-concurrent-approval-1'
          const assistantMessageID = 'pi-assistant-concurrent-approval-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, files: [], delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent, model: input.model })
          for (const callID of ['call-pi-concurrent-1', 'call-pi-concurrent-2']) {
            await eventStore.append({ type: 'session.tool.called', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, tool: 'easydo_pipeline_run_list', tool_name: 'easydo_pipeline_run_list', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 } } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'permission.asked', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), request_id: `approval:${callID}`, approval_id: `approval:${callID}`, call_id: callID, tool_name: 'easydo_pipeline_run_list', reason: 'Tool easydo_pipeline_run_list requires approval', input: { pipeline_id: callID.endsWith('1') ? 1 : 13 }, message: 'Tool easydo_pipeline_run_list requires approval' } as unknown as AgentRuntimeEvent)
            await eventStore.append({ type: 'session.tool.failed', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, call_id: callID, error: { type: 'unknown', message: 'Tool easydo_pipeline_run_list requires approval' } })
          }
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'pi-text-concurrent-approval-1', text: '需要用户审批后继续。' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'list two pipeline runs',
      client_entry_id: 'pi-entry-concurrent-approval-1'
    })

    const decisions = await Promise.allSettled([
      service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
        decision: 'approve_once',
        approval_id: 'approval:call-pi-concurrent-1',
        client_decision_id: 'approve-pi-concurrent-1'
      }),
      service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
        decision: 'approve_once',
        approval_id: 'approval:call-pi-concurrent-2',
        client_decision_id: 'approve-pi-concurrent-2'
      })
    ])

    expect(decisions.map((item) => item.status).sort()).toEqual(['fulfilled', 'fulfilled'])
    expect(executed.map((call) => call.tool_call_id).sort()).toEqual([
      'call-pi-concurrent-1',
      'call-pi-concurrent-2'
    ])
  })

  it('continues Pi approvals with tool resources so new tool calls remain runtime events', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    let continuationToolNames: string[] = []
    const toolExecutor: RuntimeToolExecutor = {
      async execute(call) {
        executed.push(call)
        return { content: `ok ${call.tool_call_id}`, structured_content: { ok: true } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (!String(input.prompt).includes('tool_result')) {
            await input.tools?.[0].execute('call-pi-write-chain-1', { value: 'x' })
            return fakePiRunner(eventStore, 'blocked').prompt(input)
          }
          continuationToolNames = input.tools?.map((tool) => tool.name) || []
          await input.tools?.[0].execute('call-pi-write-chain-2', { value: 'y' })
          return fakePiRunner(eventStore, 'should wait for the second approval').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'write twice',
      client_entry_id: 'pi-entry-tool-approval-chain-1'
    })

    const continued = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      approval_id: 'approval:call-pi-write-chain-1',
      client_decision_id: 'approve-pi-write-chain-1'
    })

    expect(continuationToolNames).toEqual(['easydo_write'])
    expect(executed).toEqual([
      expect.objectContaining({ tool_call_id: 'call-pi-write-chain-1', tool_name: 'easydo_write', arguments: { value: 'x' } })
    ])
    expect(continued.run.status).toBe('awaiting_decision')
    expect(continued.assistant_entry.status).toBe('streaming')
    expect(continued.run.result.awaiting_approval).toMatchObject({ approval_id: 'approval:call-pi-write-chain-2', call_id: 'call-pi-write-chain-2' })
    expect((continued.run.result.runtime_events as AgentRuntimeEvent[]).map((event) => event.type)).toEqual(expect.arrayContaining([
      'permission.resolved',
      'session.tool.success',
      'permission.asked'
    ]))
  })

  it('streams Pi approval continuation events while completing the runtime run', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const toolExecutor: RuntimeToolExecutor = {
      async execute() {
        return { content: 'write ok', structured_content: { ok: true }, metadata: { request_id: 'pi-write-stream-1' } }
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), toolExecutor, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (!String(input.prompt).includes('tool_result')) {
            await input.tools?.[0].execute('call-pi-write-stream-1', { value: 'x' })
          }
          return fakePiRunner(eventStore, `stream final with ${input.prompt}`).prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call write tool for stream approval',
      client_entry_id: 'pi-entry-tool-approval-stream-1'
    })

    const streamed: RuntimeStreamEvent[] = []
    for await (const event of service.streamPiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'approve_once',
      client_decision_id: 'approve-pi-write-stream-1'
    })) {
      streamed.push(event)
    }

    expect(streamed.map((event) => event.event)).toEqual(expect.arrayContaining([
      'runtime_event',
      'assistant_entry',
      'done'
    ]))
    expect(streamed.filter((event) => event.event === 'runtime_event').map((event) => asRecord(event.data.event).type)).toEqual(expect.arrayContaining([
      'permission.resolved',
      'session.tool.called',
      'session.tool.success',
      'session.text.ended',
      'session.step.ended'
    ]))
    const finalAssistant = streamed.find((event) => event.event === 'assistant_entry')?.data.entry as { status?: string; content?: string } | undefined
    expect(finalAssistant?.status).toBe('completed')
    expect(finalAssistant?.content).toContain('stream final with')
  })

  it('rejects Pi MCP tool approval and releases the active runtime run', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          if (!String(input.prompt).includes('new prompt after reject')) {
            await input.tools?.[0].execute('call-pi-write-reject-1', { value: 'x' })
          }
          return fakePiRunner(eventStore, 'blocked').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call write tool',
      client_entry_id: 'pi-entry-tool-approval-reject-1'
    })

    const rejected = await service.decidePiApproval(actor, String(first.run?.runtime_run_id), {
      decision: 'reject',
      client_decision_id: 'reject-pi-write-1'
    })

    expect(rejected.run.status).toBe('cancelled')
    expect(rejected.assistant_entry.status).toBe('cancelled')
    expect(rejected.run.result.awaiting_approval).toBeUndefined()
    expect(rejected.run.result.text).toContain('已拒绝执行工具 easydo_write')
    await expect(service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'new prompt after reject',
      client_entry_id: 'pi-entry-after-reject-1'
    })).resolves.toMatchObject({ run: expect.objectContaining({ status: 'completed' }) })
  })

  it('records OpenCode-style cancellation events when cancelling a Pi run awaiting approval', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          await input.tools?.[0].execute('call-pi-cancel-1', { value: 'x' })
          return fakePiRunner(eventStore, 'blocked').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'ops-mcp',
      name: 'Ops MCP',
      status: 'active',
      spec: { discovered_tools: [{ name: 'easydo_write', description: 'Write data', requires_confirmation: true }] }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'call write tool then cancel',
      client_entry_id: 'pi-entry-cancel-awaiting-1'
    })

    const cancelled = await service.cancelSessionRun(actor, session.id, {
      runtime_run_id: first.run?.runtime_run_id,
      reason: 'User stopped generation'
    })

    const runtimeEvents = cancelled.run.result.runtime_events as AgentRuntimeEvent[]
    expect(cancelled.status).toBe('cancelled')
    expect(runtimeEvents.map((event) => event.type)).toEqual(expect.arrayContaining([
      'permission.resolved',
      'session.step.failed',
      'session.error',
      'run.cancelled'
    ]))
    expect(runtimeEvents.find((event) => event.type === 'permission.resolved')).toMatchObject({ result: 'rejected' })
    expect(runtimeEvents.find((event) => event.type === 'session.step.failed')).toMatchObject({ error: { type: 'cancelled', message: 'User stopped generation' } })
    expect(runtimeEvents.find((event) => event.type === 'session.error')).toMatchObject({ code: 'user_cancelled', message: 'User stopped generation' })
    expect((await eventStore.replay(String(first.run?.result.runtime_session_id))).map((event) => event.type)).toEqual(expect.arrayContaining([
      'permission.resolved',
      'session.step.failed',
      'session.error',
      'run.cancelled'
    ]))

    const replay = await service.listRunEventsByRuntimeID(String(first.run?.runtime_run_id))
    expect(replay.events.map((event: { event_type: string }) => event.event_type)).toEqual(expect.arrayContaining([
      'permission.resolved',
      'session.step.failed',
      'session.error',
      'run.cancelled'
    ]))
    expect(replay.events.find((event: { event_type: string }) => event.event_type === 'session.error')).toMatchObject({
      payload_json: expect.objectContaining({
        code: 'user_cancelled',
        message: 'User stopped generation',
        runtime_run_id: first.run?.runtime_run_id
      }),
      display_json: expect.objectContaining({ event_type: 'session.error', status: 'failed' })
    })
  })

  it('replays cancellation events for early Pi runs whose result has not recorded the runtime engine yet', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let releaseRun: (() => void) | undefined
    const runGate = new Promise<void>((resolve) => { releaseRun = resolve })
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: 'pi-user-early', prompt: input.prompt, files: [], delivery: 'prompt' })
          await runGate
          return { session_id: input.sessionID, user_message_id: 'pi-user-early', assistant_message_id: 'pi-assistant-early' }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const running = service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'early pi run',
      client_entry_id: 'pi-entry-cancel-early-1'
    })
    await waitFor(async () => {
      expect(await store.findActiveRun(actor.workspace_id, session.id)).toMatchObject({ status: 'running' })
    })
    const activeRun = await store.findActiveRun(actor.workspace_id, session.id)
    const runtimeRunID = String(activeRun?.runtime_run_id)
    const stored = await store.getRun(actor.workspace_id, runtimeRunID)
    if (!stored) throw new Error('stored run missing')
    await store.saveRun({
      ...stored,
      result: {},
      updated_at: nowForTest()
    })

    await service.cancelSessionRun(actor, session.id, {
      runtime_run_id: runtimeRunID,
      reason: 'User stopped early generation'
    })
    releaseRun?.()
    await running

    const replay = await service.listRunEventsByRuntimeID(runtimeRunID)
    expect(replay.events.map((event: { event_type: string }) => event.event_type)).toEqual(expect.arrayContaining([
      'session.prompted',
      'session.step.failed',
      'session.error',
      'run.cancelled'
    ]))
    expect(replay.events.find((event: { event_type: string }) => event.event_type === 'session.error')).toMatchObject({
      payload_json: expect.objectContaining({
        code: 'user_cancelled',
        message: 'User stopped early generation',
        runtime_run_id: runtimeRunID
      })
    })
  })

  it('replays Pi compaction lifecycle events from completed runtime runs', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          const userMessageID = 'pi-user-compact-1'
          const assistantMessageID = 'pi-assistant-compact-1'
          await eventStore.append({ type: 'session.prompted', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: userMessageID, prompt: input.prompt, delivery: 'prompt' })
          await eventStore.append({ type: 'session.step.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, parent_message_id: userMessageID, agent: input.agent })
          await eventStore.append({ type: 'session.compaction.started', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: 'compact-msg-1', reason: 'auto' })
          await eventStore.append({ type: 'session.compaction.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), message_id: 'compact-msg-1', reason: 'auto', summary: 'Earlier context summarized.', recent: 'Current request remains active.' })
          await eventStore.append({ type: 'session.text.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, text_id: 'compact-text-1', text: 'compaction done' })
          await eventStore.append({ type: 'session.step.ended', session_id: input.sessionID, event_id: '', seq: 0, timestamp: nowForTest(), assistant_message_id: assistantMessageID, finish_reason: 'stop' })
          return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'compact context',
      client_entry_id: 'pi-entry-compaction-1'
    })

    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
    const compactionEvents = replay.events.filter((event: { event_type: string }) => event.event_type.startsWith('session.compaction.'))
    expect(compactionEvents.map((event: { event_type: string }) => event.event_type)).toEqual([
      'session.compaction.started',
      'session.compaction.ended'
    ])
    expect(compactionEvents[1]).toMatchObject({
      payload_json: expect.objectContaining({
        reason: 'auto',
        summary: 'Earlier context summarized.',
        recent: 'Current request remains active.',
        runtime_run_id: result.run?.runtime_run_id
      }),
      display_json: expect.objectContaining({ event_type: 'session.compaction.ended', status: 'success' })
    })
  })

  it('aborts the active Pi harness when cancelling a Pi runtime run', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let abortedRuntimeRunID = ''
    const runner = {
      async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
        return fakePiRunner(eventStore, 'pi answer').prompt(input)
      },
      async abort(runtimeRunID: string) {
        abortedRuntimeRunID = runtimeRunID
        return true
      }
    }
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner,
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = nowForTest()
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: 'run-pi-abort',
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: session.context_tags,
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      input_entry_id: 1,
      request: { runtime_engine: 'pi', runtime_session_id: 'pi-session-abort-1' },
      result: { runtime_engine: 'pi', runtime_session_id: 'pi-session-abort-1' },
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })
    await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: 'partial',
      content_blocks: [{ type: 'text', text: 'partial' }],
      input: {},
      output: {},
      runtime_run_id: 'run-pi-abort',
      idempotency_key: 'pi-abort-entry',
      created_at: timestamp,
      updated_at: timestamp
    })

    await service.cancelSessionRun(actor, session.id, { runtime_run_id: 'run-pi-abort' })

    expect(abortedRuntimeRunID).toBe('run-pi-abort')
  })

  it('requires the exact runtime run id when cancelling a session run', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_read_large_report')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    await expect(service.cancelSessionRun(actor, session.id, {})).rejects.toMatchObject({
      code: 'runtime_run_id_required',
      status: 400
    })
  })

  it('archives an idle chatbox session and keeps it out of active lists', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const archived = await service.archiveSession(actor, session.id)
    expect(archived).toMatchObject({ id: session.id, status: 'archived', active_run: null })

    const activeSessions = await service.listSessions(actor, {
      business_type: 'agent_profile',
      business_id: `${profile.id}:draft`,
      status: 'active'
    })
    expect(activeSessions.find((item) => item.id === session.id)).toBeUndefined()

    const archivedSessions = await service.listSessions(actor, {
      business_type: 'agent_profile',
      business_id: `${profile.id}:draft`,
      status: 'archived'
    })
    expect(archivedSessions.map((item) => item.id)).toContain(session.id)

    await expect(service.archiveSession(actor, session.id)).rejects.toMatchObject({
      code: 'ai_session_not_found',
      status: 404
    })
  })

  it('rejects archive while a chatbox session still has an active run', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = nowForTest()
    const runtimeRunID = `r_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}_archive`
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [],
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      input_entry_id: 1,
      request: {},
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })

    await expect(service.archiveSession(actor, session.id)).rejects.toMatchObject({
      code: 'active_run_conflict',
      status: 409
    })
  })


  it('persists one explicit cancelled state event for legacy runs', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = nowForTest()
    const runtimeRunID = `r_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}_000001`
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [],
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      input_entry_id: 1,
      request: {},
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })

    await service.cancelSessionRun(actor, session.id, { runtime_run_id: runtimeRunID })
    const events = await store.listActionEvents(actor.workspace_id, runtimeRunID)

    expect(events.filter((event) => event.event_type === 'run.cancelled')).toHaveLength(1)
    expect(events.at(-1)?.payload_json).toMatchObject({ status: 'cancelled', code: 'user_cancelled' })
  })

  it('repairs an expired Pi owner lease as one interrupted terminal Run', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {
      runtimeInstanceID: 'runtime-survivor',
      runStreamPollMs: 1
    }, undefined, {
      runner: fakePiRunner(eventStore),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const runtimeSessionID = `s_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}`
    const runtimeRunID = `r_w${actor.workspace_id.toString(36)}_${session.id.toString(36).padStart(6, '0')}_000001`
    const timestamp = nowForTest()
    const assistantEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: '',
      content_blocks: [],
      input: {},
      output: {},
      runtime_run_id: runtimeRunID,
      idempotency_key: 'expired-owner-assistant',
      created_at: timestamp,
      updated_at: timestamp
    })
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [],
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      owner_instance_id: 'runtime-dead',
      owner_epoch: 1,
      owner_lease_expires_at: '2020-01-01T00:00:00.000Z',
      input_entry_id: 1,
      output_entry_id: assistantEntry.id,
      request: { runtime_engine: 'pi', runtime_session_id: runtimeSessionID },
      result: { runtime_engine: 'pi', runtime_session_id: runtimeSessionID },
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })
    await eventStore.append({
      type: 'run.started',
      session_id: runtimeSessionID,
      runtime_run_id: runtimeRunID,
      event_id: '',
      seq: 0,
      timestamp,
      status: 'running',
      run: { runtime_run_id: runtimeRunID, status: 'running' }
    } as unknown as AgentRuntimeEvent)

    const interrupted = await service.reconcileExpiredRunOwners(true)
    const replay = await service.listRunEventsByRuntimeID(runtimeRunID)
    const repairedEntry = (await store.listEntries(actor.workspace_id, session.id)).find((entry) => entry.id === assistantEntry.id)

    expect(interrupted).toHaveLength(1)
    const repairedRun = await store.getRun(actor.workspace_id, runtimeRunID)
    expect(repairedRun?.status).toBe('interrupted')
    expect(repairedRun?.active_slot).toBeUndefined()
    expect(replay.events.filter((event: { event_type: string }) => event.event_type === 'run.interrupted')).toHaveLength(1)
    expect(replay.events.at(-1)).toMatchObject({ event_type: 'run.interrupted' })
    expect(repairedEntry).toMatchObject({ entry_type: 'error', status: 'failed' })
  })

  it('does not rewrite a terminal runtime run as cancelled', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_gpu_usage')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = nowForTest()
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: 'run-terminal-cancel',
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: session.context_tags,
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'completed',
      input_entry_id: 1,
      request: {},
      result: { text: 'done' },
      usage: {},
      started_at: timestamp,
      finished_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })

    await expect(service.cancelSessionRun(actor, session.id, {
      runtime_run_id: 'run-terminal-cancel'
    })).rejects.toMatchObject({
      code: 'runtime_run_not_cancellable',
      status: 409
    })
    await expect(store.getRun(actor.workspace_id, 'run-terminal-cancel')).resolves.toMatchObject({ status: 'completed' })
  })

  it('keeps an aborted Pi run and its durable Assistant entry cancelled', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let rejectPrompt: ((error: Error) => void) | undefined
    let abortedRuntimeRunID = ''
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          await eventStore.append({
            type: 'session.prompted',
            session_id: input.sessionID,
            runtime_run_id: input.runtimeRunID,
            event_id: '',
            seq: 0,
            timestamp: nowForTest(),
            message_id: 'pi-user-cancel-race',
            prompt: input.prompt,
            delivery: 'prompt'
          } as AgentRuntimeEvent)
          return new Promise<AgentHarnessRunResult>((_resolve, reject) => {
            rejectPrompt = reject
          })
        },
        async abort(runtimeRunID: string) {
          abortedRuntimeRunID = runtimeRunID
          rejectPrompt?.(new Error('Pi harness stopped with aborted'))
          return true
        }
      },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({ model: { provider_id: 'test', id: 'fake' } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const running = service.createEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'cancel this exact run',
      client_entry_id: 'pi-entry-cancel-race'
    })

    await waitFor(async () => {
      expect(await store.findActiveRun(actor.workspace_id, session.id)).toMatchObject({ status: 'running' })
      expect(rejectPrompt).toBeTypeOf('function')
    })
    const activeRun = await store.findActiveRun(actor.workspace_id, session.id)
    const cancelled = await service.cancelSessionRun(actor, session.id, {
      runtime_run_id: activeRun?.runtime_run_id,
      reason: 'User stopped generation'
    })
    const executionResult = await running
    const finalRun = await store.getRun(actor.workspace_id, String(activeRun?.runtime_run_id))
    const finalEntries = await store.listEntries(actor.workspace_id, session.id)
    const assistantEntries = finalEntries.filter((entry) => entry.role === 'assistant')

    expect(abortedRuntimeRunID).toBe(activeRun?.runtime_run_id)
    expect(cancelled.status).toBe('cancelled')
    expect(executionResult.run?.status).toBe('cancelled')
    expect(executionResult.assistant_entry.status).toBe('cancelled')
    expect(finalRun?.status).toBe('cancelled')
    expect(assistantEntries).toHaveLength(1)
    expect(assistantEntries[0]).toMatchObject({
      id: activeRun?.output_entry_id,
      runtime_run_id: activeRun?.runtime_run_id,
      status: 'cancelled'
    })
  })

  it('streams runtime_engine=pi entries through the AgentHarnessRunner event core', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi streamed answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const events: RuntimeStreamEvent[] = []
    for await (const event of service.streamEntryRun(actor, session.id, {
      runtime_engine: 'pi',
      content: 'stream pi',
      client_entry_id: 'pi-stream-1'
    })) {
      events.push(event)
    }

    expect(events.map((event) => event.event)).toEqual([
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'user_entry',
      'assistant_entry',
      'done'
    ])
    expect(events.filter((event) => event.event === 'runtime_event').map((event) => asRecord(event.data.event).type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.text.ended',
      'session.step.ended',
      'run.completed'
    ])
    const assistant = events.find((event) => event.event === 'assistant_entry')?.data.entry as { content?: string; output?: Record<string, unknown> } | undefined
    expect(assistant?.content).toBe('pi streamed answer')
    expect(assistant?.output?.runtime_engine).toBe('pi')
    expect((assistant?.output?.runtime_events as Array<{ type: string }>).map((event) => event.type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.text.ended',
      'session.step.ended',
      'run.completed'
    ])
  })

  it('replays stored Pi runtime events when a stream request is idempotently retried', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('legacy'), undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'pi retry answer'),
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      model: { provider_id: 'test', id: 'fake' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const payload = {
      runtime_engine: 'pi',
      content: 'stream pi retry',
      client_entry_id: 'pi-stream-retry-1'
    }

    for await (const _event of service.streamEntryRun(actor, session.id, payload)) {
      // Drain the first stream to persist the run and entries.
    }

    const retryEvents: RuntimeStreamEvent[] = []
    for await (const event of service.streamEntryRun(actor, session.id, payload)) {
      retryEvents.push(event)
    }

    expect(retryEvents.filter((event) => event.event === 'runtime_event').map((event) => asRecord(event.data.event).type)).toEqual([
      'run.started',
      'context.build_started',
      'capability.snapshot',
      'context.build_completed',
      'model.selected',
      'session.prompted',
      'session.step.started',
      'session.text.ended',
      'session.step.ended',
      'run.completed'
    ])
    expect(retryEvents.map((event) => event.event)).toEqual([
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'runtime_event',
      'user_entry',
      'assistant_entry',
      'done'
    ])
  })

  it('opens a common draft profile only through the explicit draft version key', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('draft answer'))

    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    expect(session.context_tags).toEqual([])
    expect(session.agent_profile_id).toBe(profile.id)
    expect(session.agent_profile_version_key).toBe('draft')

    const result = await service.createEntryRun(actor, session.id, {
      content: 'hello draft',
      client_entry_id: 'draft-run-1'
    })
    expect(result.run).not.toBeNull()
    if (!result.run) throw new Error('run is required')

    expect(result.run.agent_profile_id).toBe(profile.id)
    expect(result.run.agent_profile_version_key).toBe('draft')
    expect(result.run.profile_snapshot).toMatchObject({
      id: profile.id,
      name: 'Draft Common Agent',
      context_tags: [],
      status: 'draft'
    })
    expect(result.run.agent_profile_snapshot_hash).toMatch(/^sha256:/)
  })

  it('resolves latest to the newest published profile while draft remains mutable', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('versioned answer'))

    const profile = await profiles.createProfile(actor, profilePayload({
      prompt: { system: 'published prompt' }
    }))
    const published = await profiles.publishProfile(actor, profile.id, { change_summary: 'publish stable prompt' })
    await profiles.updateProfile(actor, profile.id, { prompt: { system: 'draft prompt' } })

    const latestSession = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id, 'latest'))
    const draftPayload = chatboxSessionPayload(profile.id, 'draft')
    draftPayload.profile_selection.first_session_timestamp = '2026-06-05T00:00:01.000Z'
    const draftSession = await service.getCurrentSession(actor, draftPayload)

    expect(latestSession.agent_profile_version_id).toBe(published.profile_version_id)
    expect(latestSession.agent_profile_version_key).toBe('latest')
    expect(draftSession.agent_profile_version_id).toBe(0)
    expect(draftSession.agent_profile_version_key).toBe('draft')

    const latestRun = await service.createEntryRun(actor, latestSession.id, {
      content: 'use published',
      client_entry_id: 'published-latest-run'
    })
    const draftRun = await service.createEntryRun(actor, draftSession.id, {
      content: 'use draft',
      client_entry_id: 'explicit-draft-run'
    })

    expect(latestRun.run?.profile_snapshot?.prompt).toEqual({ system: 'published prompt' })
    expect(draftRun.run?.profile_snapshot?.prompt).toEqual({ system: 'draft prompt' })
  })

  it('treats a bare object output schema as unconstrained chat output', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('plain assistant answer'))

    const profile = await profiles.createProfile(actor, profilePayload({
      output_schema: { type: 'object' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const result = await service.createEntryRun(actor, session.id, {
      content: 'normal chat',
      client_entry_id: 'bare-object-schema-chat'
    })

    expect(result.run?.status).toBe('completed')
    expect(result.assistant_entry.status).toBe('completed')
    expect(result.assistant_entry.entry_type).toBe('message')
    expect(result.assistant_entry.content).toBe('plain assistant answer')
    expect(result.assistant_entry.output.output_schema_valid).toBe(true)
    expect(result.assistant_entry.output.output_schema_errors).toEqual([])
    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))
    expect(replay.events.filter((event: { event_type: string }) => event.event_type === 'run.completed')).toHaveLength(1)
    expect(replay.events.at(-1)).toMatchObject({
      event_type: 'run.completed',
      payload_json: expect.objectContaining({ status: 'completed' })
    })
  })

  it('persists one explicit failed state event for legacy model failures', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, {
      async complete() { throw new Error('legacy provider failed') },
      async *stream() { throw new Error('legacy provider failed') }
    })
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'fail this run',
      client_entry_id: 'legacy-failed-state-1'
    })
    const replay = await service.listRunEventsByRuntimeID(String(result.run?.runtime_run_id))

    expect(result.run?.status).toBe('failed')
    expect(replay.events.filter((event: { event_type: string }) => event.event_type === 'run.failed')).toHaveLength(1)
    expect(replay.events.at(-1)).toMatchObject({
      event_type: 'run.failed',
      payload_json: expect.objectContaining({ status: 'failed', message: 'legacy provider failed' })
    })
  })

  it('stores provider raw responses as internal artifacts instead of normal output payload', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, {
      async complete() {
        return {
          text: 'raw artifact answer',
          usage: { total_tokens: 17 },
          raw: {
            provider: 'unit-test-provider',
            full_response: {
              id: 'provider-response-1',
              diagnostic: 'this full raw payload must not be projected to UI output'
            }
          }
        }
      }
    })

    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const result = await service.createEntryRun(actor, session.id, {
      content: 'show raw storage behavior',
      client_entry_id: 'provider-raw-artifact'
    })

    expect(result.assistant_entry.output).not.toHaveProperty('raw')
    expect(result.run?.result).not.toHaveProperty('raw')

    const rawRefs = result.run?.result.provider_raw_refs as Array<{ artifact_id: string }> | undefined
    expect(rawRefs).toHaveLength(1)
    const artifact = rawRefs ? await store.getRuntimeArtifact(actor.workspace_id, rawRefs[0].artifact_id) : undefined
    expect(artifact).toMatchObject({
      artifact_type: 'provider_raw',
      visibility: 'internal_only',
      preview_json: {
        provider_call_index: 1,
        has_raw: true
      }
    })
    expect(artifact?.storage_ref).toContain('unit-test-provider')
    expect(result.run?.result.runtime_events).toEqual(expect.arrayContaining([
      expect.objectContaining({
        type: 'provider.raw_stored',
        payload: expect.objectContaining({
          artifact_id: rawRefs?.[0].artifact_id
        })
      })
    ]))
  })

  it('truncates oversized tool output into preview plus artifact and tells the model', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const hugeToolOutput = 'line '.repeat(1200)
    const continuationRequests: string[] = []
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('tool_result')) {
          continuationRequests.push(request.content)
          return { text: 'used truncated tool result' }
        }
        return {
          text: '',
          tool_calls: [{
            id: 'call-large-read-result',
            name: 'easydo_read_large_report',
            arguments: { report_id: 'r1' },
            operation_type: 'read',
            target_type: 'report',
            target_id: 'r1'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: hugeToolOutput,
          structured_content: { rows: hugeToolOutput },
          metadata: { request_id: 'large-tool-output-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_read_large_report')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'read large report',
      client_entry_id: 'large-tool-output-preview'
    })

    const toolResults = result.run?.result.tool_results as Array<{
      truncated: boolean
      content: string
      model_notice: string
      artifact_refs: Array<{ artifact_id: string }>
    }>
    expect(toolResults[0]).toMatchObject({
      truncated: true,
      model_notice: expect.stringContaining('truncated')
    })
    expect(toolResults[0].content.length).toBeLessThan(hugeToolOutput.length)
    expect(toolResults[0].artifact_refs).toHaveLength(1)
    expect(continuationRequests[0]).toContain('"truncated":true')
    expect(continuationRequests[0]).toContain('Tool result was truncated')
    expect(continuationRequests[0]).not.toContain(hugeToolOutput)

    const artifact = await store.getRuntimeArtifact(actor.workspace_id, toolResults[0].artifact_refs[0].artifact_id)
    expect(artifact).toMatchObject({
      artifact_type: 'tool_output',
      visibility: 'user_visible',
      preview_json: expect.objectContaining({
        truncated: true
      })
    })
    expect(artifact?.storage_ref).toBe(hugeToolOutput)
  })

  it('records explicit model and tool loop timeline events for automatic tool runs', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const modelRequests: Array<Parameters<ChatModelClient['complete']>[0]> = []
    const client: ChatModelClient = {
      async complete(request) {
        modelRequests.push(request)
        if (request.content.includes('tool_result')) {
          return { text: 'GPU usage is 42%.' }
        }
        return {
          text: '',
          tool_calls: [{
            id: 'call-gpu-usage',
            name: 'easydo_resource_gpu_usage',
            arguments: { resource_id: 7023 },
            operation_type: 'read',
            target_type: 'resource',
            target_id: '7023'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        return {
          content: `executed ${call.tool_name}`,
          structured_content: { gpu_usage: 42 },
          metadata: { request_id: 'gpu-request-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const profile = await profiles.createProfile(actor, profilePayload({
      model: {
        provider_model_key: 'google/gemma-4-31b-free',
        context_window: 262144,
        max_tokens: 8192
      },
      inference: { thinking_level: 'high' },
      tool_policy: allowToolPolicy('easydo_resource_gpu_usage')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: '查询 GPU 使用率',
      client_entry_id: 'explicit-tool-loop-events',
      runtime_request_id: 'req-tool-loop',
      parent_runtime_run_id: 'run-parent-tool-loop'
    })

    const runtimeEvents = (result.run?.result.runtime_events || []) as Array<{
      type: string
      payload: Record<string, unknown>
    }>
    const eventTypes = runtimeEvents.map((event) => event.type)
    const firstIndex = (type: string) => {
      const index = eventTypes.indexOf(type)
      expect(index, `${type} should be recorded`).toBeGreaterThanOrEqual(0)
      return index
    }

    expect(modelRequests).toHaveLength(2)
    expect(modelRequests).toEqual([
      expect.objectContaining({
        request_id: 'req-tool-loop',
        runtime_run_id: result.run?.runtime_run_id,
        parent_runtime_run_id: 'run-parent-tool-loop'
      }),
      expect.objectContaining({
        request_id: 'req-tool-loop',
        runtime_run_id: result.run?.runtime_run_id,
        parent_runtime_run_id: 'run-parent-tool-loop'
      })
    ])
    expect(eventTypes).toEqual(expect.arrayContaining([
      'context.build_started',
      'context.build_completed',
      'model.request_prepared',
      'model.call_started',
      'model.call_completed',
      'model.tool_call_detected',
      'action.execution_started',
      'tool.executed',
      'tool.result_prepared',
      'model.tool_result_submitted',
      'model.continuation_started',
      'model.continuation_completed'
    ]))
    expect(firstIndex('context.build_started')).toBeLessThan(firstIndex('context.build_completed'))
    expect(firstIndex('model.request_prepared')).toBeLessThan(firstIndex('model.call_started'))
    expect(firstIndex('model.call_started')).toBeLessThan(firstIndex('model.call_completed'))
    expect(firstIndex('model.call_completed')).toBeLessThan(firstIndex('model.tool_call_detected'))
    expect(firstIndex('model.tool_call_detected')).toBeLessThan(firstIndex('action.execution_started'))
    expect(firstIndex('tool.executed')).toBeLessThan(firstIndex('tool.result_prepared'))
    expect(firstIndex('tool.result_prepared')).toBeLessThan(firstIndex('model.tool_result_submitted'))
    expect(firstIndex('model.tool_result_submitted')).toBeLessThan(firstIndex('model.continuation_started'))
    expect(firstIndex('model.continuation_started')).toBeLessThan(firstIndex('model.continuation_completed'))

    expect(runtimeEvents.find((event) => event.type === 'model.request_prepared')?.payload).toMatchObject({
      phase: 'initial',
      tool_count: 0,
      context_window: 262144,
      max_tokens: 8192,
      thinking_level: 'high'
    })
    expect(runtimeEvents.find((event) => event.type === 'model.call_started')?.payload).toMatchObject({
      context_window: 262144,
      max_tokens: 8192,
      thinking_level: 'high'
    })
    expect(runtimeEvents.find((event) => event.type === 'model.tool_call_detected')?.payload).toMatchObject({
      provider_tool_call_id: 'call-gpu-usage',
      tool_name: 'easydo_resource_gpu_usage',
      operation_type: 'read',
      target_type: 'resource',
      target_id: '7023'
    })
    expect(runtimeEvents.find((event) => event.type === 'tool.result_prepared')?.payload).toMatchObject({
      provider_tool_call_id: 'call-gpu-usage',
      tool_name: 'easydo_resource_gpu_usage',
      status: 'completed',
      truncated: false
    })
    expect(runtimeEvents.find((event) => event.type === 'model.tool_result_submitted')?.payload).toMatchObject({
      phase: 'tool_continuation',
      tool_result_count: 1
    })
  })

  it('does not complete non-streaming automatic tool runs with empty text when the model repeats a tool call', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('tool_result')) {
          return {
            text: '',
            finish: { reason: 'tool_calls' },
            tool_calls: [{
              id: 'call-list-resource-repeat',
              name: 'easydo_resource_list',
              arguments: { workspace_id: actor.workspace_id, query: '7024' },
              operation_type: 'read',
              target_type: 'resource'
            }]
          }
        }
        return {
          text: '',
          tool_calls: [{
            id: 'call-list-resource',
            name: 'easydo_resource_list',
            arguments: { workspace_id: actor.workspace_id, query: '7024' },
            operation_type: 'read',
            target_type: 'resource'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: JSON.stringify({ list: [], total: 0, page: 1, limit: 20 }),
          structured_content: { list: [], total: 0, page: 1, limit: 20 },
          metadata: { request_id: 'resource-list-empty' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: '触发一下7024机器的显卡占用采集',
      client_entry_id: 'repeat-tool-call-empty-non-stream'
    })

    const runtimeEvents = (result.run?.result.runtime_events || []) as Array<{
      type: string
      payload: Record<string, unknown>
    }>

    expect(runtimeEvents).toEqual(expect.arrayContaining([
      expect.objectContaining({
        type: 'model.continuation_completed',
        payload: expect.objectContaining({
          finish_reason: 'tool_calls',
          tool_call_count: 1
        })
      })
    ]))
    expect(result.run?.status).toBe('completed')
    expect(result.assistant_entry.status).toBe('completed')
    expect(result.assistant_entry.content.trim()).not.toBe('')
    expect(result.assistant_entry.content).toContain('easydo_resource_list')
    expect(result.assistant_entry.content).toContain('"total":0')
  })

  it('does not expose internal tool-call plans as final answers during automatic tool recovery', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('automatic_tool_loop_recovery')) {
          return { text: 'We will call easydo_resource_list with query "7024".' }
        }
        if (request.content.includes('tool_result')) {
          return {
            text: '',
            finish: { reason: 'tool_calls' },
            tool_calls: [{
              id: 'call-list-resource-repeat-plan',
              name: 'easydo_resource_list',
              arguments: { workspace_id: actor.workspace_id, query: '7024' },
              operation_type: 'read',
              target_type: 'resource'
            }]
          }
        }
        return {
          text: '',
          tool_calls: [{
            id: 'call-list-resource-plan',
            name: 'easydo_resource_list',
            arguments: { workspace_id: actor.workspace_id, query: '7024' },
            operation_type: 'read',
            target_type: 'resource'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: JSON.stringify({ list: [], total: 0, page: 1, limit: 20 }),
          structured_content: { list: [], total: 0, page: 1, limit: 20 },
          metadata: { request_id: 'resource-list-empty-plan' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: '触发一下7024机器的显卡占用采集',
      client_entry_id: 'repeat-tool-call-plan-non-stream'
    })

    const runtimeEvents = (result.run?.result.runtime_events || []) as Array<{ type: string }>

    expect(result.assistant_entry.content).not.toContain("We'll call")
    expect(result.assistant_entry.content).toContain('工具 easydo_resource_list 已执行')
    expect(result.assistant_entry.content).toContain('"total":0')
    expect(runtimeEvents.map((event) => event.type)).toContain('model.answer_synthesized')
  })

  it('emits runtime approval request schema and permission events for gated tool calls', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        return {
          text: 'approval is needed',
          tool_calls: [{
            id: 'call-approval-schema',
            name: 'easydo_pipeline_trigger',
            arguments: { pipeline_id: 13 },
            operation_type: 'execute',
            target_type: 'pipeline',
            target_id: '13',
            risk_summary: 'Triggering pipeline 13 changes server state.'
          }]
        }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: { default_decision: 'request' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'trigger pipeline',
      client_entry_id: 'approval-runtime-schema'
    })

    const action = (result.run?.result.agent_actions as Array<Record<string, unknown>>)[0]
    const approvalRequest = asRecord(asRecord(action.display_json).approval_request)
    expect(approvalRequest).toMatchObject({
      run_id: result.run?.runtime_run_id,
      action_id: action.action_id,
      tool_name: 'easydo_pipeline_trigger',
      permission_key: 'tool:easydo_pipeline_trigger:execute',
      risk_level: 'write',
      steer_supported: true,
      options: ['approve_once', 'approve_session', 'reject']
    })
    expect(String(approvalRequest.approval_id)).toBe('ap_wb_000001_000001_000001')
    expect(approvalRequest.created_at).toEqual(expect.any(String))
    expect(approvalRequest.expires_at).toEqual(expect.any(String))

    expect(result.run?.result.runtime_events).toEqual(expect.arrayContaining([
      expect.objectContaining({
        type: 'action.permission_evaluated',
        payload: expect.objectContaining({
          decision: 'ask',
          action_id: action.action_id
        })
      }),
      expect.objectContaining({
        type: 'approval.requested',
        payload: expect.objectContaining({
          event_id: expect.any(String),
          approval_request: expect.objectContaining({
            approval_id: approvalRequest.approval_id
          })
        })
      })
    ]))
  })

  it('requires approval for unannotated tools without guessing operation type from the name', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        return {
          text: 'refresh resource',
          tool_calls: [{
            id: 'call-refresh-resource',
            name: 'easydo_resource_base_info_refresh',
            arguments: { workspace_id: actor.workspace_id, resource_id: 2 }
          }]
        }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: '触发 7022 GPU 信息采集',
      client_entry_id: 'refresh-requires-approval'
    })

    const action = (result.run?.result.agent_actions as Array<Record<string, unknown>>)[0]
    const approvalRequest = asRecord(asRecord(action.display_json).approval_request)
    expect(action.status).toBe('awaiting_decision')
    expect(approvalRequest).toMatchObject({
      tool_name: 'easydo_resource_base_info_refresh',
      permission_key: 'tool:easydo_resource_base_info_refresh:unknown',
      risk_level: 'write'
    })
    expect(result.run?.result.runtime_events).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'approval.requested' }),
      expect.objectContaining({ type: 'action.decision_required' })
    ]))
  })

  it('adds EasyDo tool approval and resource-label resolution policy to model requests', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let capturedDeveloperInstructions = ''
    const client: ChatModelClient = {
      async complete(request) {
        capturedDeveloperInstructions = request.developer_instructions || ''
        return { text: 'ok' }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload({
      prompt: {
        system: 'Answer as a common EasyDo agent.',
        developer: 'Keep answers concise.'
      }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    await service.createEntryRun(actor, session.id, {
      content: '触发一下7022 的显卡使用情况刷新',
      client_entry_id: 'runtime-tool-policy-instructions'
    })

    expect(capturedDeveloperInstructions).toContain('Keep answers concise.')
    expect(capturedDeveloperInstructions).toContain('emit a tool_call')
    expect(capturedDeveloperInstructions).toContain('approval.requested')
    expect(capturedDeveloperInstructions).toContain('7022 or 7023')
    expect(capturedDeveloperInstructions).toContain('Resolve it first with a read/list resource tool')
    expect(capturedDeveloperInstructions).toContain('do not hide a required tool call inside markdown')
  })

  it('continues streaming runs when the provider first returns reasoning and blank text', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let completions = 0
    const client: ChatModelClient = {
      async complete() {
        completions += 1
        if (completions === 1) return { text: '   ' }
        return { text: 'Recovered final answer.' }
      },
      async *stream() {
        yield { type: 'reasoning_delta', delta: 'thinking without answer' }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: 'hello',
      client_entry_id: 'blank-stream-answer'
    })) {
      events.push(event)
    }

    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const assistantEntry = assistantEvent?.data?.entry as {
      status?: string
      content?: string
      output?: { runtime_events?: Array<{ type: string }> }
    } | undefined
    expect(events.map((event) => event.event)).toContain('provider.empty_output_detected')
    expect(events.map((event) => event.event)).not.toContain('error')
    expect(assistantEntry?.status).toBe('completed')
    expect(assistantEntry?.content).toContain('Recovered final answer')
    expect(assistantEntry?.output?.runtime_events?.map((event) => event.type)).toEqual(expect.arrayContaining([
      'provider.empty_output_detected',
      'provider.continuation_completed'
    ]))
  })

  it('persists streaming reasoning and answer deltas as runtime process events', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        return { text: 'unused' }
      },
      async *stream() {
        yield { type: 'reasoning_delta', delta: '先确认用户真正要做的是需求澄清。' }
        yield { type: 'reasoning_delta', delta: '再组织流水线创建前必须回答的问题。' }
        yield { type: 'answer_delta', delta: '请先回答流水线目标、触发方式和任务阶段。' }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: 'grill-me 添加一个新流水线',
      client_entry_id: 'persist-reasoning-process-events'
    })) {
      events.push(event)
    }

    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const assistantEntry = assistantEvent?.data?.entry as {
      output?: { runtime_events?: Array<{ type: string, payload: Record<string, unknown> }> }
    } | undefined
    const runtimeEvents = assistantEntry?.output?.runtime_events || []
    const reasoningEvents = runtimeEvents.filter((event) => event.type === 'reasoning_delta')
    const answerEvents = runtimeEvents.filter((event) => event.type === 'answer_delta')

    expect(reasoningEvents).toHaveLength(2)
    expect(reasoningEvents[0].payload).toMatchObject({
      delta: '先确认用户真正要做的是需求澄清。'
    })
    expect(reasoningEvents[0].payload.event_id).toBeTruthy()
    expect(reasoningEvents[0].payload.runtime_run_id).toBeTruthy()
    expect(answerEvents).toHaveLength(1)
    expect(answerEvents[0].payload).toMatchObject({
      delta: '请先回答流水线目标、触发方式和任务阶段。'
    })
    expect(runtimeEvents.map((event) => event.type)).toEqual(expect.arrayContaining([
      'reasoning_delta',
      'answer_delta'
    ]))
    expect(runtimeEvents.findIndex((event) => event.type === 'reasoning_delta')).toBeLessThan(
      runtimeEvents.findIndex((event) => event.type === 'answer_delta')
    )
  })

  it('emits a canonical safe error descriptor for non-Pi page-assistant provider failures', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const secret = 'sk-page-assistant-provider-leak'
    const service = new SessionService(store, profiles, leakingProviderFailureClient(secret))
    const profile = await profiles.createProfile(actor, pageAssistantPayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events: RuntimeStreamEvent[] = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: 'explain the current page',
      client_entry_id: 'non-pi-provider-error-sse',
      runtime_request_id: 'req-non-pi-provider-error',
      context_ref: {
        kind: 'current-page',
        route_path: '/store/ai-agents',
        route_name: 'AIAgentStore',
        workspace_id: actor.workspace_id
      }
    })) {
      events.push(event)
    }

    const errorEvent = events.find((event) => event.event === 'error')
    expect(errorEvent?.data).toMatchObject({
      code: 'openrouter_unavailable',
      category: 'transport',
      message: 'The AI provider is temporarily unavailable. Try again.',
      user_message: 'The AI provider is temporarily unavailable. Try again.',
      retryable: true,
      http_status: 503,
      source: 'provider',
      terminal_status: 'failed',
      request_id: 'req-non-pi-provider-error'
    })
    expect(errorEvent?.data).not.toHaveProperty('details')
    expect(JSON.stringify(errorEvent)).not.toContain(secret)
    expect(JSON.stringify(errorEvent)).not.toContain('provider-bearer-secret')
  })

  it('persists the canonical descriptor and safe user message for non-Pi page-assistant provider failures', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const secret = 'sk-page-assistant-persisted-leak'
    const service = new SessionService(store, profiles, leakingProviderFailureClient(secret))
    const profile = await profiles.createProfile(actor, pageAssistantPayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    for await (const _event of service.streamEntryRun(actor, session.id, {
      content: 'explain the current page',
      client_entry_id: 'non-pi-provider-error-persistence',
      runtime_request_id: 'req-non-pi-persisted',
      context_ref: {
        kind: 'current-page',
        route_path: '/store/ai-agents',
        route_name: 'AIAgentStore',
        workspace_id: actor.workspace_id
      }
    })) {
      // Consume the full stream so the terminal Assistant Entry and Run are durable.
    }

    const persistedEntries = await store.listEntries(actor.workspace_id, session.id)
    const assistantEntry = persistedEntries.find((entry) => entry.role === 'assistant')
    const persistedRun = assistantEntry?.runtime_run_id
      ? await store.getRun(actor.workspace_id, assistantEntry.runtime_run_id)
      : undefined
    const safeUserMessage = 'The AI provider is temporarily unavailable. Try again.'
    const canonicalDescriptor = {
      code: 'openrouter_unavailable',
      category: 'transport',
      message: safeUserMessage,
      user_message: safeUserMessage,
      retryable: true,
      http_status: 503,
      source: 'provider',
      terminal_status: 'failed',
      request_id: 'req-non-pi-persisted'
    }

    expect(assistantEntry).toMatchObject({
      entry_type: 'error',
      status: 'failed',
      content: `模型调用失败：${safeUserMessage}`,
      output: {
        text: `模型调用失败：${safeUserMessage}`,
        output_schema_errors: [safeUserMessage],
        provider_error: canonicalDescriptor
      }
    })
    expect(persistedRun).toMatchObject({
      status: 'failed',
      error_code: 'openrouter_unavailable',
      error_msg: safeUserMessage,
      request: expect.objectContaining({ request_id: 'req-non-pi-persisted' }),
      result: {
        text: `模型调用失败：${safeUserMessage}`,
        output_schema_errors: [safeUserMessage],
        provider_error: canonicalDescriptor
      }
    })
    expect(asRecord(assistantEntry?.output.provider_error)).not.toHaveProperty('details')
    expect(asRecord(persistedRun?.result.provider_error)).not.toHaveProperty('details')
    const persistedJSON = JSON.stringify({ assistantEntry, persistedRun })
    expect(persistedJSON).not.toContain(secret)
    expect(persistedJSON).not.toContain('provider-bearer-secret')
    expect(persistedJSON).not.toContain('response_body')
    expect(persistedJSON).not.toContain('authorization')
  })

  it('persists a safe provider error descriptor when bounded continuation also fails', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        throw new ModelProviderError('provider_response_empty', 'Provider response did not include assistant text or tool calls', 502, {
          endpoint_dialect: 'openai-chat',
          choices_count: 1,
          finish_reason: 'stop',
          content_length: 0,
          tool_calls_count: 0
        })
      },
      async *stream() {
        return
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload({
      name: 'page-ai-assistant',
      context_tags: ['page-assistant']
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: 'e1',
      client_entry_id: 'provider-diagnostics',
      context_ref: {
        kind: 'current-page',
        route_path: '/store/ai-agents',
        route_name: 'AIAgentStore',
        workspace_id: actor.workspace_id
      }
    })) {
      events.push(event)
    }

    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const assistantEntry = assistantEvent?.data?.entry as {
      status?: string
      output?: { provider_error?: Record<string, unknown>, runtime_events?: Array<{ type: string, payload: Record<string, unknown> }> }
    } | undefined
    const providerError = assistantEntry?.output?.provider_error as Record<string, unknown> | undefined
    const errorEvent = assistantEntry?.output?.runtime_events?.find((event) => event.type === 'run.error')

    expect(assistantEntry?.status).toBe('failed')
    expect(providerError).toMatchObject({
      code: 'provider_response_empty',
      category: 'transport',
      message: 'The AI provider is temporarily unavailable. Try again.',
      user_message: 'The AI provider is temporarily unavailable. Try again.',
      retryable: true,
      http_status: 502,
      source: 'provider',
      terminal_status: 'failed'
    })
    expect(providerError).not.toHaveProperty('details')
    expect(providerError).not.toHaveProperty('status')
    expect(errorEvent?.payload).toMatchObject(providerError || {})
  })

  it('continues streaming runs instead of completing blank entries for reasoning-only completions', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let completions = 0
    const client: ChatModelClient = {
      async complete() {
        completions += 1
        if (completions === 2) {
          return { text: 'Recovered from reasoning-only completion.' }
        }
        return {
          text: '',
          parts: [{ type: 'reasoning', text: 'thinking without final answer' }]
        }
      },
      async *stream() {
        return
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: 'hello',
      client_entry_id: 'reasoning-only-completion'
    })) {
      events.push(event)
    }

    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const assistantEntry = assistantEvent?.data?.entry as { status?: string, entry_type?: string, content?: string } | undefined

    expect(events.map((event) => event.event)).toContain('provider.empty_output_detected')
    expect(events.map((event) => event.event)).not.toContain('error')
    expect(assistantEntry?.status).toBe('completed')
    expect(assistantEntry?.entry_type).toBe('message')
    expect(assistantEntry?.content).toContain('Recovered from reasoning-only completion')
  })

  it('accepts the new top-level profile_id fields used by server facades', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('top-level profile answer'))

    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, {
      profile_id: profile.id,
      profile_version_id: 'draft',
      first_session_timestamp: '2026-06-06T00:00:00.000Z',
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '11',
      title: 'Page Assistant'
    })

    expect(session.agent_profile_id).toBe(profile.id)
    expect(session.agent_profile_version_key).toBe('draft')
    expect(session.context_tags).toEqual([])
  })

  it('defaults a top-level profile_id session to the latest published version when no version is provided', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('implicit latest profile answer'))

    const profile = await profiles.createProfile(actor, profilePayload())
    const published = await profiles.publishProfile(actor, profile.id, { change_summary: 'page assistant stable version' })
    const session = await service.getCurrentSession(actor, {
      profile_id: profile.id,
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '11',
      source: 'page-ai-assistant',
      title: 'Page Assistant'
    })

    expect(session.agent_profile_id).toBe(profile.id)
    expect(session.agent_profile_version_id).toBe(published.profile_version_id)
    expect(session.agent_profile_version_key).toBe('latest')
  })

  it('reuses the page assistant current session when first_session_timestamp is omitted', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('page assistant answer'))
    const profile = await profiles.createProfile(actor, pageAssistantPayload())
    await profiles.publishProfile(actor, profile.id, { change_summary: 'stable page assistant' })
    const payload = {
      profile_id: profile.id,
      session_kind: 'chat',
      business_type: 'workspace',
      business_id: '11',
      source: 'page-ai-assistant',
      title: 'Page Assistant'
    }

    const first = await service.getCurrentSession(actor, payload)
    await new Promise((resolve) => setTimeout(resolve, 5))
    const second = await service.getCurrentSession(actor, payload)

    expect(second.id).toBe(first.id)
    expect(second.session_key).toBe(first.session_key)
  })

  it('allows profiles with empty ordered context tags and does not require scene tags during validation', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const profile = await profiles.createProfile(actor, profilePayload({
      context_tags: []
    }))

    const validation = await profiles.validateProfile(actor, profile.id)

    expect(profile.context_tags).toEqual([])
    expect(validation.errors.map((item) => item.code)).not.toContain('scene_type_required')
    expect(validation.status).toBe('passed')
  })

  it('fails profile validation when required resources are missing disabled or unpublished', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const disabledMcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'disabled-mcp',
      name: 'Disabled MCP',
      status: 'disabled',
      spec: { discovered_tools: [{ name: 'disabled_tool' }] }
    })
    const draftSubagent = await profiles.createProfile(actor, profilePayload({ name: 'Draft Subagent' }))
    const profile = await profiles.createProfile(actor, profilePayload({
      skills: [{ resource_type: 'skill', resource_id: 'missing-skill', required: true }],
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: disabledMcp.resource_key, required: true }],
      subagents: [{ resource_type: 'subagent_profile', resource_id: draftSubagent.id, required: true }]
    }))

    const validation = await profiles.validateProfile(actor, profile.id)

    expect(validation.status).toBe('failed')
    expect(validation.errors.map((item) => item.code)).toEqual(expect.arrayContaining([
      'agent_profile_resource_not_found',
      'agent_profile_resource_not_active',
      'agent_profile_subagent_version_not_found'
    ]))
    expect(validation.resource_health).toEqual(expect.arrayContaining([
      expect.objectContaining({ resource_id: 'missing-skill', status: 'failed' }),
      expect.objectContaining({ resource_id: 'disabled-mcp', status: 'failed' }),
      expect.objectContaining({ resource_id: draftSubagent.id, status: 'failed' })
    ]))
    await expect(profiles.publishProfile(actor, profile.id, {})).rejects.toMatchObject({
      code: 'agent_profile_validation_failed'
    } satisfies Partial<RuntimeDomainError>)
  })

  it('materializes the fixed page assistant profile when profiles are listed', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)

    const firstList = await profiles.listProfiles(actor)
    const pageProfile = firstList.find((profile) => profile.name === 'page-ai-assistant')
    expect(pageProfile).toMatchObject({
      name: 'page-ai-assistant',
      context_tags: ['page-assistant'],
      status: 'draft'
    })

    const secondList = await profiles.listProfiles(actor)
    expect(secondList.filter((profile) => profile.name === 'page-ai-assistant')).toHaveLength(1)
  })

  it('rejects duplicate profile names before the store can upsert by workspace name', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)

    await profiles.createProfile(actor, profilePayload({ name: 'Shared Name' }))

    await expect(profiles.createProfile(actor, profilePayload({ name: 'Shared Name' }))).rejects.toMatchObject({
      code: 'agent_profile_name_exists'
    } satisfies Partial<RuntimeDomainError>)
  })

  it('rejects page-assistant runs without current page context_ref based on the profile tag', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())

    const profile = await profiles.createProfile(actor, pageAssistantPayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    expect(session.context_tags).toEqual(['page-assistant'])
    await expect(service.createEntryRun(actor, session.id, {
      content: 'explain this page',
      client_entry_id: 'missing-page-context'
    })).rejects.toMatchObject({
      code: 'page_context_required'
    } satisfies Partial<RuntimeDomainError>)
  })

  it('assembles multiple context tag fragments in profile order before the user message', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let capturedContent = ''
    let capturedCapabilities: Record<string, unknown> = {}
    const client: ChatModelClient = {
      async complete(request) {
        capturedContent = request.content
        capturedCapabilities = request.capabilities as Record<string, unknown>
        return { text: 'ordered context ok' }
      }
    }
    const service = new SessionService(store, profiles, client)

    const profile = await profiles.createProfile(actor, pageAssistantPayload({
      context_tags: ['workspace', 'page-assistant']
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const result = await service.createEntryRun(actor, session.id, {
      content: 'what page am I on?',
      context_ref: { kind: 'current-page', route_path: '/store/ai-agents', route_name: 'AgentStore' },
      client_entry_id: 'ordered-context-tags'
    })

    expect(session.context_tags).toEqual(['workspace', 'page-assistant'])
    expect(capturedContent.indexOf('Context tag: workspace')).toBeLessThan(capturedContent.indexOf('Context tag: page-assistant'))
    expect(capturedContent.indexOf('Context tag: page-assistant')).toBeLessThan(capturedContent.indexOf('User message:'))
    expect(capturedContent).toContain('what page am I on?')
    expect(capturedCapabilities.context_tags).toEqual(['workspace', 'page-assistant'])
    expect(result.run?.request.context_tags).toEqual(['workspace', 'page-assistant'])
    expect(result.run?.request.context_tag_context).toEqual(expect.arrayContaining([
      expect.objectContaining({ tag: 'workspace' }),
      expect.objectContaining({ tag: 'page-assistant' })
    ]))
  })

  it('resolves an explicit published profile version without checking scene/profile tag compatibility', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())

    const profile = await profiles.createProfile(actor, profilePayload({
      name: 'Explicit Profile',
      context_tags: []
    }))
    const published = await profiles.publishProfile(actor, profile.id, {})
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id, published.profile_version_id))

    expect(session.agent_profile_id).toBe(profile.id)
    expect(session.agent_profile_version_id).toBe(published.profile_version_id)
    expect(session.context_tags).toEqual([])
  })

  it('rejects user input longer than half of the selected profile max tokens', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())

    const profile = await profiles.createProfile(actor, profilePayload({ inference: { max_tokens: 6 } }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    await expect(service.createEntryRun(actor, session.id, {
      content: 'one two three four',
      client_entry_id: 'too-long'
    })).rejects.toMatchObject({
      code: 'chatbox_input_too_large'
    } satisfies Partial<RuntimeDomainError>)
  })

  it('executes a confirmed tool call and continues the final model answer', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The write tool completed.' }
        }
        return {
          text: 'I need to write.',
          tool_calls: [{
            id: 'call-1',
            name: 'easydo_test_write',
            arguments: { value: 'created' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        return {
          content: `executed ${call.tool_name}`,
          structured_content: { ok: true, arguments: call.arguments },
          metadata: { request_id: 'mcp-request-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write',
      client_entry_id: 'tool-run-1'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    const entries = await store.listEntries(actor.workspace_id, session.id)

    expect(continued.action.status).toBe('executed')
    expect(continued.action.result_json.structured_content).toMatchObject({ ok: true })
    expect(entries.some((entry) => entry.entry_type === 'tool_result' && entry.status === 'completed')).toBe(true)
    expect(continued.assistant_entry.content).toContain('write tool completed')
    expect(continued.run.result.runtime_events).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'tool.executed' }),
      expect.objectContaining({ type: 'run.completed' })
    ]))
  })

  it('rejects an action continuation before decisions, state changes, entries, or tool execution when the Run snapshot hash is tampered', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let toolExecutions = 0
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) return { text: 'continued' }
        return {
          text: 'approval required',
          tool_calls: [{
            id: 'call-integrity-action-1',
            name: 'easydo_integrity_write',
            arguments: { value: 'created' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      }
    }
    const service = new SessionService(store, profiles, client, {
      async execute() {
        toolExecutions += 1
        return { content: 'executed', structured_content: { ok: true } }
      }
    })
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const first = await service.createEntryRun(actor, session.id, {
      content: 'please write with integrity guard',
      client_entry_id: 'integrity-action-run-1'
    })
    const run = first.run
    if (!run) throw new Error('runtime run must exist')
    const actionID = String((first.assistant_entry.output.agent_actions as Array<{ id: string }>)[0]?.id)
    const actionBefore = await store.getActionByBusinessID(actor.workspace_id, actionID)
    if (!actionBefore) throw new Error('pending action must exist')
    const entriesBefore = await store.listEntries(actor.workspace_id, session.id)
    const decisionsBefore = await store.listActionDecisions(actor.workspace_id, actionBefore.id)
    await store.saveRun({ ...run, agent_profile_snapshot_hash: `sha256:${'0'.repeat(64)}` })

    await expect(actions.decideAndContinue(actor, actionID, 'approve_once', {
      client_decision_id: 'approve-integrity-action-1'
    })).rejects.toMatchObject({
      code: 'runtime_profile_snapshot_integrity_failed',
      status: 500
    } satisfies Partial<RuntimeDomainError>)

    expect(await store.getActionByBusinessID(actor.workspace_id, actionID)).toEqual(actionBefore)
    expect(await store.listActionDecisions(actor.workspace_id, actionBefore.id)).toEqual(decisionsBefore)
    expect(await store.listEntries(actor.workspace_id, session.id)).toEqual(entriesBefore)
    expect((await store.getRun(actor.workspace_id, run.runtime_run_id))?.status).toBe('awaiting_decision')
    expect(toolExecutions).toBe(0)
  })

  it('lists chat entries with lightweight runtime event summaries instead of full delta snapshots', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, {
      async complete() {
        return { text: 'full answer' }
      },
      async *stream() {
        yield { type: 'reasoning_delta', delta: 'first thought' }
        yield { type: 'reasoning_delta', delta: 'second thought' }
        yield { type: 'answer_delta', delta: 'full answer' }
      }
    })
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    let assistantEntryID = 0
    for await (const item of service.streamEntryRun(actor, session.id, {
      content: 'stream a concise answer',
      client_entry_id: 'lightweight-entry-events'
    })) {
      if (item.event === 'assistant_entry') {
        assistantEntryID = Number(asRecord(item.data.entry).id)
      }
    }
    const storedEntries = await store.listEntries(actor.workspace_id, session.id)
    const storedAssistant = storedEntries.find((entry) => entry.id === assistantEntryID)
    if (!storedAssistant) throw new Error('stored assistant entry must exist')
    const storedEvents = storedAssistant.output.runtime_events as Array<{ type: string }>
    const listedEntries = await service.listEntries(actor, session.id)
    const listedAssistant = listedEntries.find((entry) => entry.id === storedAssistant.id)
    if (!listedAssistant) throw new Error('assistant entry must be listed')

    expect(storedEvents.map((event) => event.type)).toEqual(expect.arrayContaining(['reasoning_delta', 'answer_delta']))
    expect(listedAssistant.output.runtime_events).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'model.call_started' }),
      expect.objectContaining({ type: 'model.call_completed' }),
      expect.objectContaining({ type: 'output_schema.validated' })
    ]))
    expect((listedAssistant.output.runtime_events as Array<{ type: string }>).some((event) => event.type === 'reasoning_delta')).toBe(false)
    expect((listedAssistant.output.runtime_events as Array<{ type: string }>).some((event) => event.type === 'answer_delta')).toBe(false)
  })

  it('does not expose internal runtime event sequence conflicts through entry or Pi run event projections', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient(), undefined, {}, undefined, {
      runner: { async prompt() { throw new Error('unused') } },
      eventStore
    })
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const createdAt = new Date().toISOString()
    const runtimeRunID = 'r_wb_000001_000004'
    const runtimeSessionID = 's_wb_000001'
    const runtimeEvents = [
      {
        type: 'session.step.started',
        session_id: runtimeSessionID,
        event_id: 'pi-visible-1',
        seq: 1,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict'
      },
      {
        type: 'session.tool.failed',
        session_id: runtimeSessionID,
        event_id: 'pi-internal-runtime-conflict-2',
        seq: 2,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        call_id: 'call-runtime-persistence',
        error: {
          type: 'database_error',
          message: 'Database write failed',
          code: 'ER_DUP_ENTRY',
          constraint: 'uk_ai_agent_runtime_events_session_seq'
        },
        result: {
          table: 'ai_agent_runtime_events',
          constraint: 'uk_ai_agent_runtime_events_session_seq'
        }
      },
      {
        type: 'session.reasoning.ended',
        session_id: runtimeSessionID,
        event_id: 'pi-internal-runtime-narration-3',
        seq: 3,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-runtime-conflict',
        text: '注意：Duplicate entry 是服务端日志写入失败，不是工具调用失败。'
      },
      {
        type: 'session.reasoning.started',
        session_id: runtimeSessionID,
        event_id: 'pi-split-narration-start-4',
        seq: 4,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-split-runtime-conflict'
      },
      {
        type: 'session.reasoning.delta',
        session_id: runtimeSessionID,
        event_id: 'pi-split-narration-delta-5',
        seq: 5,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-split-runtime-conflict',
        delta: '注意：pipeline_get 返回了 Dupli'
      },
      {
        type: 'session.reasoning.delta',
        session_id: runtimeSessionID,
        event_id: 'pi-split-narration-delta-6',
        seq: 6,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-split-runtime-conflict',
        delta: 'cate entry 错误，这是服务端日志'
      },
      {
        type: 'session.reasoning.delta',
        session_id: runtimeSessionID,
        event_id: 'pi-split-narration-delta-7',
        seq: 7,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-split-runtime-conflict',
        delta: '写入的 bug'
      },
      {
        type: 'session.reasoning.ended',
        session_id: runtimeSessionID,
        event_id: 'pi-split-narration-end-8',
        seq: 8,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-split-runtime-conflict',
        text: ''
      },
      {
        type: 'session.reasoning.ended',
        session_id: runtimeSessionID,
        event_id: 'pi-benign-duplicate-9',
        seq: 9,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        reasoning_id: 'reasoning-benign-duplicate',
        text: "The import report contains Duplicate entry 'customer-42' for key 'uk_customers_external_id'."
      },
      {
        type: 'session.tool.failed',
        session_id: runtimeSessionID,
        event_id: 'pi-domain-duplicate-10',
        seq: 10,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        call_id: 'call-create-pipeline',
        error: {
          type: 'database_error',
          message: "Duplicate entry 'pipeline-a' for key 'uk_pipelines_workspace_name'",
          code: 'ER_DUP_ENTRY',
          constraint: 'uk_pipelines_workspace_name'
        },
        result: {
          table: 'pipelines',
          constraint: 'uk_pipelines_workspace_name'
        }
      },
      {
        type: 'session.tool.failed',
        session_id: runtimeSessionID,
        event_id: 'pi-tool-failure-11',
        seq: 11,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        call_id: 'call-pipeline-get',
        error: { type: 'tool_error', message: 'Tool easydo_pipeline_get returned 500', http_status: 500 },
        result: { content: [{ type: 'text', text: 'Tool easydo_pipeline_get returned 500' }] }
      },
      {
        type: 'session.step.ended',
        session_id: runtimeSessionID,
        timestamp: createdAt,
        assistant_message_id: 'assistant-runtime-conflict',
        finish_reason: 'stop'
      }
    ] as unknown as AgentRuntimeEvent[]
    for (const event of runtimeEvents) {
      await eventStore.append({ ...event, runtime_run_id: runtimeRunID })
    }
    const userEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content: 'replay runtime errors',
      content_blocks: [{ type: 'text', text: 'replay runtime errors' }],
      input: {},
      output: {},
      idempotency_key: 'internal-runtime-seq-conflict-user',
      created_at: createdAt,
      updated_at: createdAt
    })
    const assistantEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: 2,
      entry_type: 'message',
      role: 'assistant',
      status: 'completed',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: {},
      output: { runtime_events: runtimeEvents },
      runtime_run_id: runtimeRunID,
      idempotency_key: 'internal-runtime-seq-conflict-assistant',
      created_at: createdAt,
      updated_at: createdAt
    })
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: runtimeRunID,
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [],
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:runtime-conflict',
      profile_snapshot: profile,
      status: 'completed',
      input_entry_id: userEntry.id,
      output_entry_id: assistantEntry.id,
      request: { runtime_engine: 'pi', runtime_session_id: runtimeSessionID },
      result: { runtime_engine: 'pi', runtime_session_id: runtimeSessionID, runtime_events: runtimeEvents },
      usage: {},
      started_at: createdAt,
      finished_at: createdAt,
      created_at: createdAt,
      updated_at: createdAt
    })

    const visibleEventSeqs = [1, 9, 10, 11, 12]
    const listedAssistant = (await service.listEntries(actor, session.id)).find((entry) => entry.id === assistantEntry.id)
    const listedRuntimeEvents = (listedAssistant?.output.runtime_events || []) as AgentRuntimeEvent[]
    const listedPayload = JSON.stringify(listedRuntimeEvents)
    const replay = await service.listRunEventsByRuntimeID(runtimeRunID)
    const scopedReplay = await service.listRunEventsScoped(actor, runtimeRunID)

    expect(listedRuntimeEvents.map((event) => event.event_id)).toEqual([
      'pi-visible-1',
      'pi-benign-duplicate-9',
      'pi-domain-duplicate-10',
      'pi-tool-failure-11',
      undefined
    ])
    expect(listedPayload).not.toContain('uk_ai_agent_runtime_events_session_seq')
    expect(listedPayload).not.toContain('服务端日志写入失败')
    expect(listedPayload).toContain('uk_customers_external_id')
    expect(listedPayload).toContain('uk_pipelines_workspace_name')
    expect(listedPayload).toContain('Tool easydo_pipeline_get returned 500')
    expect(replay.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([...visibleEventSeqs, 13])
    expect(scopedReplay.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([...visibleEventSeqs, 13])
    expect(JSON.stringify(replay.events)).not.toContain('uk_ai_agent_runtime_events_session_seq')
    expect(JSON.stringify(scopedReplay.events)).not.toContain('uk_ai_agent_runtime_events_session_seq')
    const replayNarration = replay.events.flatMap((event: { payload_json?: Record<string, unknown> }) => {
      const payload = event.payload_json || {}
      return [payload.text, payload.delta, payload.message].filter((value): value is string => typeof value === 'string')
    }).join('')
    expect(replayNarration).not.toContain('Duplicate entry 错误，这是服务端日志写入的 bug')
    expect(replayNarration).toContain("Duplicate entry 'customer-42'")
    expect(replay.events.find((event: { event_seq: number }) => event.event_seq === 12)?.event_id).toBe(`${runtimeSessionID}:12`)

    const afterVisibleBeforeHidden = await service.listRunEventsByRuntimeID(runtimeRunID, 'pi-visible-1')
    expect(afterVisibleBeforeHidden.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([9, 10, 11, 12, 13])
    const afterVisibleAfterHidden = await service.listRunEventsByRuntimeID(runtimeRunID, 'pi-domain-duplicate-10')
    expect(afterVisibleAfterHidden.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([11, 12, 13])
    const afterHidden = await service.listRunEventsScoped(actor, runtimeRunID, {}, 'pi-internal-runtime-conflict-2')
    expect(afterHidden.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([9, 10, 11, 12, 13])
    const afterHiddenSplitDelta = await service.listRunEventsByRuntimeID(runtimeRunID, 'pi-split-narration-delta-6')
    expect(afterHiddenSplitDelta.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([9, 10, 11, 12, 13])
    const afterUnknown = await service.listRunEventsByRuntimeID(runtimeRunID, 'pi-unknown-cursor')
    expect(afterUnknown.events.map((event: { event_seq: number }) => event.event_seq)).toEqual([...visibleEventSeqs, 13])
    await expect(service.listRunEventsScoped({
      ...actor,
      user_id: actor.user_id + 1,
      auth_session_id: 'auth-other-user'
    }, runtimeRunID)).rejects.toMatchObject({
      code: 'ai_session_not_found'
    } satisfies Partial<RuntimeDomainError>)
  })

  it('projects a tool-result summary for historical completed assistant entries with empty content', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const createdAt = new Date().toISOString()
    const userEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content: '触发一下7024机器的显卡占用采集',
      content_blocks: [{ type: 'text', text: '触发一下7024机器的显卡占用采集' }],
      input: {},
      output: {},
      idempotency_key: 'historical-empty-user',
      created_at: createdAt,
      updated_at: createdAt
    })
    const assistantEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: 2,
      entry_type: 'message',
      role: 'assistant',
      status: 'completed',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: {},
      output: {
        text: '',
        runtime_events: [{ type: 'model.continuation_completed', payload: { tool_call_count: 1 } }],
        tool_results: [{
          tool_call_id: 'call-list-empty',
          tool_name: 'easydo_resource_list',
          status: 'completed',
          content: '{"list":[],"total":0,"page":1,"limit":20}',
          structured_content: { list: [], total: 0, page: 1, limit: 20 },
          metadata: {},
          truncated: false,
          artifact_refs: []
        }]
      },
      runtime_run_id: 'r_wb_000001_000001',
      idempotency_key: 'historical-empty-assistant',
      created_at: createdAt,
      updated_at: createdAt
    })

    const storedAssistant = (await store.listEntries(actor.workspace_id, session.id)).find((entry) => entry.id === assistantEntry.id)
    const listedAssistant = (await service.listEntries(actor, session.id)).find((entry) => entry.id === assistantEntry.id)

    expect(storedAssistant?.content).toBe('')
    expect(listedAssistant?.content).toContain('工具 easydo_resource_list 已执行')
    expect(String(listedAssistant?.output.text || '')).toContain('工具 easydo_resource_list 已执行')
    expect(listedAssistant?.content_blocks).toEqual([{ type: 'text', text: listedAssistant?.content }])
  })

  it('projects completed run text over a stale streaming assistant draft for historical action continuations', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const createdAt = new Date().toISOString()
    const userEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content: '触发一下7022的显卡使用情况采集',
      content_blocks: [{ type: 'text', text: '触发一下7022的显卡使用情况采集' }],
      input: {},
      output: {},
      idempotency_key: 'historical-action-user',
      created_at: createdAt,
      updated_at: createdAt
    })
    const staleAssistant = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: 2,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: {},
      output: { text: '' },
      runtime_run_id: 'r_wb_000001_000002',
      idempotency_key: 'historical-action-stale-assistant',
      created_at: createdAt,
      updated_at: createdAt
    })
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: 'r_wb_000001_000002',
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [],
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      profile_snapshot: profile,
      status: 'completed',
      input_entry_id: userEntry.id,
      output_entry_id: staleAssistant.id,
      request: {},
      result: {
        text: '工具 easydo_resource_base_info_refresh 已执行，结果：{"task_id":12,"status":"queued","agent_id":4}',
        runtime_events: [{ type: 'model.continuation_completed', payload: { phase: 'action_continuation' } }],
        tool_results: [{
          tool_call_id: 'call-refresh-resource',
          tool_name: 'easydo_resource_base_info_refresh',
          status: 'completed',
          content: '{"task_id":12,"status":"queued","agent_id":4}',
          structured_content: { task_id: 12, status: 'queued', agent_id: 4 },
          metadata: {},
          truncated: false,
          artifact_refs: []
        }]
      },
      usage: {},
      started_at: createdAt,
      finished_at: createdAt,
      created_at: createdAt,
      updated_at: createdAt
    })

    const listedAssistant = (await service.listEntries(actor, session.id)).find((entry) => entry.id === staleAssistant.id)

    expect(listedAssistant?.status).toBe('completed')
    expect(listedAssistant?.content).toContain('easydo_resource_base_info_refresh')
    expect(listedAssistant?.content).toContain('"task_id":12')
    expect(String(listedAssistant?.output.text || '')).toContain('easydo_resource_base_info_refresh')
  })

  it('coalesces historical action continuation drafts and removes internal plan text from listed entries', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const createdAt = new Date().toISOString()
    const userEntry = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'user',
      status: 'completed',
      content: '触发一下7022的显卡使用情况采集',
      content_blocks: [{ type: 'text', text: '触发一下7022的显卡使用情况采集' }],
      input: {},
      output: {},
      idempotency_key: 'historical-coalesce-user',
      created_at: createdAt,
      updated_at: createdAt
    })
    const staleAssistant = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: 2,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: '',
      content_blocks: [{ type: 'text', text: '' }],
      input: {},
      output: {
        text: 'I will attempt to query the status of task12.'
      },
      runtime_run_id: 'r_wb_000001_000003',
      idempotency_key: 'historical-coalesce-stale-assistant',
      created_at: createdAt,
      updated_at: createdAt
    })
    const toolResult = {
      tool_call_id: 'call-refresh-resource',
      tool_name: 'easydo_resource_base_info_refresh',
      status: 'completed',
      content: '{"task_id":12,"status":"queued","agent_id":4}',
      structured_content: { task_id: 12, status: 'queued', agent_id: 4 },
      metadata: {},
      truncated: false,
      artifact_refs: []
    }
    const finalAssistant = await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      parent_entry_id: userEntry.id,
      seq: 5,
      entry_type: 'message',
      role: 'assistant',
      status: 'completed',
      content: '工具 easydo_resource_base_info_refresh 已执行，结果：{"task_id":12,"status":"queued","agent_id":4}',
      content_blocks: [{ type: 'text', text: '工具 easydo_resource_base_info_refresh 已执行，结果：{"task_id":12,"status":"queued","agent_id":4}' }],
      input: { continuation_for_action_id: 'a_wb_000001_000003_000001' },
      output: {
        text: 'I will attempt to query the status of task12.',
        runtime_events: [{ type: 'model.continuation_completed', payload: { phase: 'action_continuation' } }],
        tool_results: [toolResult]
      },
      runtime_run_id: 'r_wb_000001_000003',
      idempotency_key: 'historical-coalesce-final-assistant',
      created_at: createdAt,
      updated_at: createdAt
    })
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: 'r_wb_000001_000003',
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: [],
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      profile_snapshot: profile,
      status: 'completed',
      input_entry_id: userEntry.id,
      output_entry_id: finalAssistant.id,
      request: {},
      result: {
        text: 'I will attempt to query the status of task12.',
        runtime_events: [{ type: 'model.continuation_completed', payload: { phase: 'action_continuation' } }]
      },
      usage: {},
      started_at: createdAt,
      finished_at: createdAt,
      created_at: createdAt,
      updated_at: createdAt
    })

    const listedEntries = await service.listEntries(actor, session.id)
    const listedStaleAssistant = listedEntries.find((entry) => entry.id === staleAssistant.id)
    const listedFinalAssistant = listedEntries.find((entry) => entry.id === finalAssistant.id)

    expect(listedStaleAssistant).toBeUndefined()
    expect(listedFinalAssistant?.content).toContain('easydo_resource_base_info_refresh')
    expect(String(listedFinalAssistant?.output.text || '')).toContain('easydo_resource_base_info_refresh')
    expect(String(listedFinalAssistant?.output.text || '')).not.toContain('I will attempt')
  })

  it('records parent-visible subagent progress and child thread links for replay', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        return { text: 'parent received child summary' }
      }
    }
    const service = new SessionService(store, profiles, client, undefined, {}, undefined, {
      runner: fakePiRunner(eventStore, 'run #29 server and front succeeded; health check returned 200'),
      eventStore
    })
    const childProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Run Watcher',
      description: 'Watch pipeline run until terminal state',
      model: { provider_id: 'test', id: 'fake' }
    }))
    await profiles.publishProfile(actor, childProfile.id, {})
    const parentProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Pipeline Ops Agent',
      inference: { max_tokens: 200 },
      confirmation_policy: { auto_spawn_subagents: true },
      subagents: [{
        resource_type: 'subagent_profile',
        resource_id: childProfile.id,
        config: {
          objective: 'watch run #29 until terminal state',
          mode: 'read_only'
        },
        required: true
      }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parentProfile.id))
    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'legacy',
      content: 'continue after approval and watch run #29',
      client_entry_id: 'subagent-parent-progress',
      spawn_subagents: true
    })

    const runtimeEvents = result.run?.result.runtime_events as Array<{ type: string, payload: Record<string, unknown> }>
    const subagentEvents = runtimeEvents.filter((event) => event.type.startsWith('subagent.'))

    expect(subagentEvents[0].type).toBe('subagent.spawned')
    expect(subagentEvents.at(-1)?.type).toBe('subagent.completed')
    expect(subagentEvents.some((event) => event.type === 'subagent.progress')).toBe(true)
    expect(subagentEvents[0].payload.child_runtime_run_id).toBeTruthy()
    expect(subagentEvents[0].payload.child_run_link_id).toBeTruthy()
    expect(subagentEvents[1].payload.summary).toContain('Run Watcher')
    expect(subagentEvents.at(-1)?.payload.summary).toContain('health check returned 200')
    expect(subagentEvents.at(-1)?.payload.artifact_refs).toEqual(expect.arrayContaining([
      expect.objectContaining({ artifact_id: expect.stringMatching(/^art_/) })
    ]))

    const replay = await service.listRunEvents(actor, String(result.run?.runtime_run_id))
    const replaySubagentEvents = replay.events.filter((event) => event.event_type.startsWith('subagent.'))
    expect(replaySubagentEvents[0].event_type).toBe('subagent.spawned')
    expect(replaySubagentEvents.at(-1)?.event_type).toBe('subagent.completed')
    expect(replaySubagentEvents.some((event) => event.event_type === 'subagent.progress')).toBe(true)
    expect(replaySubagentEvents[0].display_json).toMatchObject({
      event_type: 'subagent.spawned',
      title: 'Run Watcher',
      child_runtime_run_id: subagentEvents[0].payload.child_runtime_run_id,
      child_run_link_id: subagentEvents[0].payload.child_run_link_id
    })
    expect(replaySubagentEvents[1].display_json).toMatchObject({
      event_type: 'subagent.progress',
      status: 'running'
    })
    expect(replaySubagentEvents.at(-1)?.display_json).toMatchObject({
      event_type: 'subagent.completed',
      status: 'completed',
      child_runtime_run_id: subagentEvents[0].payload.child_runtime_run_id,
      child_run_link_id: subagentEvents[0].payload.child_run_link_id
    })
    const childReplay = await service.listRunEventsByRuntimeID(String(subagentEvents[0].payload.child_runtime_run_id))
    expect(childReplay.events.at(-1)).toMatchObject({
      event_type: 'run.completed',
      payload_json: expect.objectContaining({ status: 'completed' })
    })
  })

  it('runs subagents through their own Pi loop with child-scoped tools and events', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const childSecret = 'sk-p114-child-ephemeral'
    store.resolveProviderCredentialRef = async () => ({
      credential_id: '321',
      secret_ref: { api_key: childSecret }
    })
    const executed: RuntimeToolCall[] = []
    let childHarnessCalls = 0
    let childHarnessSecret: string | undefined
    const service = new SessionService(store, profiles, chatClient('parent received child summary'), {
      async execute(call) {
        executed.push(call)
        return { content: 'child tool result', structured_content: { child: true } }
      }
    }, {}, undefined, {
      runner: {
         async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
           childHarnessCalls += 1
           childHarnessSecret = input.modelConfig?.api_key
           expect(input.runtimeRunID).toMatch(/^r_/)
           expect(input.runMode).toBe('read_only')
           expect(input.tools?.map((tool) => tool.name)).toContain('child_read')
           expect(input.tools?.map((tool) => tool.name)).not.toEqual(expect.arrayContaining(['bash', 'write_file', 'edit_file']))
          await input.tools?.find((tool) => tool.name === 'child_read')?.execute('child-call-1', { target_id: 'child-resource' })
          return fakePiRunner(eventStore, 'child Pi completed').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server', resource_key: 'child-mcp', name: 'Child MCP', status: 'active',
      spec: { discovered_tools: [
        { name: 'child_read', description: 'Read child state', operation_type: 'read' },
        { name: 'child_write_hidden', description: 'Write child state', operation_type: 'write' },
        { name: 'child_unknown_hidden', description: 'Unknown child operation' }
      ], tool_permissions: { tools: { child_read: 'allow' } } }
    })
    const childProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Pi Child',
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/child-model' },
      provider_credential_ref: { credential_id: '321' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    await profiles.publishProfile(actor, childProfile.id, {})
    const parentProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Pi Parent',
      confirmation_policy: { auto_spawn_subagents: true },
      subagents: [{
        resource_type: 'subagent_profile', resource_id: childProfile.id,
        config: { objective: 'inspect child state', mode: 'read_only' }, required: true
      }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parentProfile.id))
    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'legacy', content: 'delegate child inspection', client_entry_id: 'subagent-child-pi-loop', spawn_subagents: true
    })
    const spawned = (result.run?.result.runtime_events as Array<Record<string, unknown>>)
      .find((event) => event.type === 'subagent.spawned')
    const childRuntimeRunID = String(asRecord(spawned?.payload).child_runtime_run_id)
    const childReplay = await service.listRunEventsByRuntimeID(childRuntimeRunID)
    const childRun = await store.getRun(actor.workspace_id, childRuntimeRunID)

    expect(childHarnessCalls).toBe(1)
    expect(childHarnessSecret).toBe(childSecret)
    expect(childRun?.profile_snapshot?.provider_credential_ref).toEqual({ credential_id: '321' })
    expect(childRun?.agent_profile_snapshot_hash).toBe(profileSnapshotHashForTest(childRun?.profile_snapshot))
    expect(JSON.stringify(childRun)).not.toContain(childSecret)
    expect(executed).toEqual([expect.objectContaining({ tool_call_id: 'child-call-1', tool_name: 'child_read' })])
    expect((await store.getActionByBusinessID(actor.workspace_id, String(asRecord(spawned?.payload).action_id)))?.status).toBe('executed')
    expect(childReplay.events.map((event: { event_type: string }) => event.event_type)).toEqual(expect.arrayContaining([
      'run.started',
      'session.tool.called',
      'session.tool.success',
      'run.completed'
    ]))
  })

  it('rejects direct child Profile credentials before saving the Child Draft', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const directSecret = 'sk-p114-direct-child-secret'
    await expect(profiles.createProfile(actor, profilePayload({
      name: 'Unsafe Draft Child',
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/unsafe-child' },
      provider_credential_ref: { api_key: directSecret }
    }))).rejects.toMatchObject({ code: 'provider_credential_reference_required' })
    expect(JSON.stringify(await store.listProfiles(actor.workspace_id))).not.toContain(directSecret)
  })

  it('redacts resolved credentials from failed Child Run persistence', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const secret = 'sk-p114-child-failure-secret'
    store.resolveProviderCredentialRef = async () => ({
      credential_id: '852',
      secret_ref: { api_key: secret }
    })
    const service = new SessionService(store, profiles, chatClient('parent received safe child failure'), undefined, {}, undefined, {
      runner: {
        async prompt() {
          throw new Error(`child provider failed authorization=Bearer ${secret}`)
        }
      },
      eventStore
    })
    const childProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Failing Child',
      provider: { provider_id: 'openrouter' },
      model: { provider_model_key: 'openrouter/failing-child' },
      provider_credential_ref: { credential_id: '852' }
    }))
    await profiles.publishProfile(actor, childProfile.id, {})
    const parentProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Parent With Failing Child',
      confirmation_policy: { auto_spawn_subagents: true },
      subagents: [{
        resource_type: 'subagent_profile',
        resource_id: childProfile.id,
        config: { objective: 'run failing child', mode: 'read_only' },
        required: true
      }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parentProfile.id))
    const result = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'legacy',
      content: 'delegate failing child',
      client_entry_id: 'p114-child-failure-parent',
      spawn_subagents: true
    })
    const spawned = (result.run?.result.runtime_events as Array<Record<string, unknown>>)
      .find((event) => event.type === 'subagent.spawned')
    const childRuntimeRunID = String(asRecord(spawned?.payload).child_runtime_run_id)
    const childRun = await store.getRun(actor.workspace_id, childRuntimeRunID)
    const childEntries = childRun ? await store.listEntries(actor.workspace_id, childRun.session_id) : []
    const childReplay = await service.listRunEventsByRuntimeID(childRuntimeRunID)
    const persistedJSON = JSON.stringify({ childRun, childEntries, childReplay })

    expect(childRun?.status).toBe('failed')
    expect(persistedJSON).not.toContain(secret)
    expect(childRun?.error_msg).toBe('The AI runtime encountered an internal error.')
  })

  it('keeps child Pi runs awaiting approval and resumes the child without failing the parent thread', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    const executed: RuntimeToolCall[] = []
    const service = new SessionService(store, profiles, chatClient('parent saw blocked child'), {
      async execute(call) {
        executed.push(call)
        return { content: 'child write result', structured_content: { ok: true } }
      }
    }, {}, undefined, {
      runner: {
         async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
           expect(input.runMode).toBe('write')
           if (!String(input.prompt).includes('tool_result')) {
            await input.tools?.find((tool) => tool.name === 'child_write')?.execute('child-write-call-1', { target_id: 'child-resource' })
          }
          return fakePiRunner(eventStore, 'child continued after approval').prompt(input)
        }
      },
      eventStore
    })
    const mcp = await profiles.createResource(actor, {
      resource_kind: 'mcp_server', resource_key: 'child-write-mcp', name: 'Child Write MCP', status: 'active',
      spec: { discovered_tools: [{ name: 'child_write', operation_type: 'write', requires_confirmation: true }] }
    })
    const childProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Writing Child', model: { provider_id: 'test', id: 'fake' },
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: mcp.resource_key }]
    }))
    await profiles.publishProfile(actor, childProfile.id, {})
    const parentProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Parent', confirmation_policy: { auto_spawn_subagents: true },
      subagents: [{ resource_type: 'subagent_profile', resource_id: childProfile.id, config: { objective: 'perform child write', mode: 'write' } }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parentProfile.id))
    const parentResult = await service.createEntryRun(actor, session.id, {
      runtime_engine: 'legacy', content: 'delegate write', client_entry_id: 'subagent-child-approval', spawn_subagents: true
    })
    const parentEvents = parentResult.run?.result.runtime_events as Array<Record<string, unknown>>
    const blocked = parentEvents.find((event) => event.type === 'subagent.blocked_approval')
    const childRuntimeRunID = String(asRecord(blocked?.payload).child_runtime_run_id)
     const childBefore = await store.getRun(actor.workspace_id, childRuntimeRunID)

     expect(blocked).toBeTruthy()
     expect(childBefore?.status).toBe('awaiting_decision')
     expect(childBefore?.request.mode).toBe('write')
     expect((await store.getActionByBusinessID(actor.workspace_id, String(asRecord(blocked?.payload).action_id)))?.status).toBe('awaiting_decision')
     expect(executed).toEqual([])
     if (!childBefore) throw new Error('child run missing before approval')
     await store.saveRun({ ...childBefore, request: { ...childBefore.request, mode: 'read_only' } })
     await expect(service.decidePiApproval(actor, childRuntimeRunID, {
       decision: 'approve_session', client_decision_id: 'deny-read-only-child-write'
     })).rejects.toMatchObject({ code: 'subagent_read_only_violation', status: 403 })
     expect(executed).toEqual([])
     expect(await store.listSessionPermissionGrants(actor.workspace_id, childBefore.session_id)).toEqual([])
     expect(await eventStore.replayRun(childRuntimeRunID)).toEqual(expect.arrayContaining([
       expect.objectContaining({
         type: 'action.permission_evaluated',
         code: 'subagent_read_only_violation',
         decision: 'deny'
       })
     ]))
     const readOnlyChild = await store.getRun(actor.workspace_id, childRuntimeRunID)
     if (!readOnlyChild) throw new Error('read-only child run missing')
     await store.saveRun({ ...readOnlyChild, request: { ...readOnlyChild.request, mode: 'write' } })
     const continued = await service.decidePiApproval(actor, childRuntimeRunID, {
      decision: 'approve_once', client_decision_id: 'approve-child-write-1'
    })
    expect(continued.run.status).toBe('completed')
    expect(executed).toEqual([expect.objectContaining({ tool_name: 'child_write' })])
    const childLink = await store.getParentRunLink(actor.workspace_id, childRuntimeRunID)
    expect(childLink?.status).toBe('completed')
    expect((await store.getActionByBusinessID(actor.workspace_id, String(asRecord(blocked?.payload).action_id)))?.status).toBe('executed')
    const parentReplay = await service.listRunEvents(actor, String(parentResult.run?.runtime_run_id))
    expect(parentReplay.events.map((event) => event.event_type)).toContain('subagent.completed')
  })

  it('streams parent-visible subagent lifecycle before the parent model call', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let releaseChild: () => void = () => {}
    const childCanFinish = new Promise<void>((resolve) => {
      releaseChild = resolve
    })
    const client: ChatModelClient = {
      async complete(request) {
        return { text: 'parent received watcher summary' }
      },
      async *stream() {
        yield { type: 'answer_delta', delta: 'parent received watcher summary' }
      }
    }
    const service = new SessionService(store, profiles, client, undefined, {}, undefined, {
      runner: {
        async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
          await childCanFinish
          return fakePiRunner(eventStore, 'run #31 completed; health check returned 200').prompt(input)
        }
      },
      eventStore
    })
    const childProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Run Watcher',
      description: 'Watch pipeline run until terminal state',
      model: { provider_id: 'test', id: 'fake' }
    }))
    await profiles.publishProfile(actor, childProfile.id, {})
    const parentProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Pipeline Ops Agent',
      confirmation_policy: { auto_spawn_subagents: true },
      subagents: [{
        resource_type: 'subagent_profile',
        resource_id: childProfile.id,
        config: {
          objective: 'watch run #31 until terminal state',
          mode: 'read_only'
        },
        required: true
      }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parentProfile.id))

    const events: RuntimeStreamEvent[] = []
    const iterator = service.streamEntryRun(actor, session.id, {
      runtime_engine: 'legacy',
      content: 'watch run #31',
      client_entry_id: 'subagent-stream-progress',
      spawn_subagents: true
    })[Symbol.asyncIterator]()
    const nextEventWithin = async (timeoutMs: number) => {
      return Promise.race([
        iterator.next(),
        new Promise<IteratorResult<RuntimeStreamEvent>>((resolve) => {
          setTimeout(() => resolve({ done: true, value: undefined }), timeoutMs)
        })
      ])
    }

    let spawnedBeforeChildCompletion: RuntimeStreamEvent | null = null
    try {
      for (let index = 0; index < 20; index += 1) {
        const next = await nextEventWithin(100)
        if (next.done) break
        events.push(next.value)
        if (next.value.event === 'subagent.spawned') {
          spawnedBeforeChildCompletion = next.value
          break
        }
      }
    } finally {
      releaseChild()
    }
    expect(spawnedBeforeChildCompletion).toBeTruthy()
    expect(spawnedBeforeChildCompletion?.data.child_runtime_run_id).toBeTruthy()
    expect(spawnedBeforeChildCompletion?.data.child_run_link_id).toBeTruthy()

    for (let index = 0; index < 20; index += 1) {
      const next = await nextEventWithin(1000)
      if (next.done) break
      events.push(next.value)
    }
    await iterator.return?.()

    const eventNames = events.map((item) => item.event)
    const spawnedIndex = eventNames.indexOf('subagent.spawned')
    const progressIndex = eventNames.indexOf('subagent.progress')
    const completedIndex = eventNames.indexOf('subagent.completed')
    const modelCallIndex = eventNames.indexOf('model.request_prepared')
    expect(spawnedIndex).toBeGreaterThanOrEqual(0)
    expect(progressIndex).toBeGreaterThan(spawnedIndex)
    expect(completedIndex).toBeGreaterThan(progressIndex)
    expect(modelCallIndex).toBeGreaterThan(completedIndex)
    expect(events[completedIndex].data.summary).toContain('health check returned 200')
  })

  it('cancels active child Pi runs when the parent run is cancelled', async () => {
    const store = createMemoryRuntimeStore()
    const eventStore = new FakeAgentEventStore()
    const profiles = new AgentProfileService(store)
    let rejectChild: (error: Error) => void = () => {}
    const service = new SessionService(store, profiles, chatClient('parent answer must not replace cancellation'), undefined, {}, undefined, {
      runner: {
        async prompt(): Promise<AgentHarnessRunResult> {
          return new Promise<AgentHarnessRunResult>((_resolve, reject) => {
            rejectChild = reject
          })
        },
        async abort() {
          rejectChild(new Error('child harness aborted'))
          return true
        }
      },
      eventStore
    })
    const childProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Cancellable Child',
      model: { provider_id: 'test', id: 'fake' }
    }))
    await profiles.publishProfile(actor, childProfile.id, {})
    const parentProfile = await profiles.createProfile(actor, profilePayload({
      name: 'Cancellable Parent',
      confirmation_policy: { auto_spawn_subagents: true },
      subagents: [{
        resource_type: 'subagent_profile',
        resource_id: childProfile.id,
        config: { objective: 'wait until cancelled', mode: 'read_only' },
        required: true
      }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(parentProfile.id))
    const iterator = service.streamEntryRun(actor, session.id, {
      runtime_engine: 'legacy',
      content: 'start child and then cancel',
      client_entry_id: 'subagent-parent-cancel',
      spawn_subagents: true
    })[Symbol.asyncIterator]()

    let spawned: RuntimeStreamEvent | undefined
    for (let index = 0; index < 20; index += 1) {
      const next = await iterator.next()
      if (next.done) break
      if (next.value.event === 'subagent.spawned') {
        spawned = next.value
        break
      }
    }
    expect(spawned).toBeTruthy()
    const parentRuntimeRunID = String(spawned?.data.parent_runtime_run_id)
    const childRuntimeRunID = String(spawned?.data.child_runtime_run_id)

    await service.cancelSessionRun(actor, session.id, {
      runtime_run_id: parentRuntimeRunID,
      reason: 'cancel parent with active child'
    })
    await iterator.return?.()

    await waitFor(async () => {
      expect((await store.getRun(actor.workspace_id, childRuntimeRunID))?.status).toBe('cancelled')
    })
    expect((await store.getParentRunLink(actor.workspace_id, childRuntimeRunID))?.status).toBe('cancelled')
    expect((await eventStore.replayRun(childRuntimeRunID)).map((event) => event.type)).toContain('run.cancelled')
    const parentReplay = await service.listRunEvents(actor, parentRuntimeRunID)
    expect(parentReplay.events.map((event) => event.event_type)).toContain('subagent.cancelled')
  })

  it('replaces internal continuation plan text with a user-visible action result summary', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'Now I will call the tool.' }
        }
        return {
          text: 'I need to refresh the resource.',
          tool_calls: [{
            id: 'call-refresh-resource',
            name: 'easydo_resource_base_info_refresh',
            arguments: { workspace_id: 1, resource_id: 2 },
            operation_type: 'write',
            target_type: 'resource',
            target_id: '2'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: JSON.stringify({ task_id: 8, status: 'queued', agent_id: 4 }),
          structured_content: { task_id: 8, status: 'queued', agent_id: 4 },
          metadata: { request_id: 'refresh-resource-queued' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_gpu_usage')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: '刷新一下 7022 机器的显卡使用情况采集',
      client_entry_id: 'refresh-resource-result-summary'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string }>

    const continued = await actions.decideAndContinue(actor, String(agentActions[0].id), 'approve_once', {
      client_decision_id: 'approve-refresh-resource'
    })

    expect(continued.action.status).toBe('executed')
    expect(continued.assistant_entry.content).toContain('easydo_resource_base_info_refresh')
    expect(continued.assistant_entry.content).toContain('queued')
    expect(continued.assistant_entry.content).toContain('task_id')
    expect(continued.assistant_entry.content).not.toContain('Now I will call the tool')
  })

  it('does not stream internal action continuation plan text before falling back to the tool result summary', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const completeRefreshRun: ChatModelClient['complete'] = async (request) => {
      if (request.content.includes('action_result')) {
        return { text: 'I will attempt to query the status of task12.' }
      }
      return {
        text: 'I need to refresh the resource.',
        tool_calls: [{
          id: 'call-refresh-resource-stream',
          name: 'easydo_resource_base_info_refresh',
          arguments: { workspace_id: 1, resource_id: 2 },
          operation_type: 'write',
          target_type: 'resource',
          target_id: '2'
        }]
      }
    }
    const client: ChatModelClient = {
      complete: completeRefreshRun,
      async *stream(request) {
        const completion = await completeRefreshRun(request)
        if (!completion) return
        for (const toolCall of completion.tool_calls || []) {
          yield { type: 'tool_call', tool_call: toolCall }
        }
        if (completion.text) {
          yield { type: 'answer_delta', delta: completion.text }
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: JSON.stringify({ task_id: 12, status: 'queued', agent_id: 4 }),
          structured_content: { task_id: 12, status: 'queued', agent_id: 4 },
          metadata: { request_id: 'refresh-resource-stream-queued' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: '触发一下7022的显卡使用情况采集',
      client_entry_id: 'refresh-resource-stream-result-summary'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string }>
    const events: RuntimeStreamEvent[] = []

    for await (const event of actions.decideAndContinueStream(actor, String(agentActions[0].id), 'approve_session', {
      client_decision_id: 'approve-refresh-resource-stream'
    })) {
      events.push(event)
    }

    const answerDeltas = events
      .filter((event) => event.event === 'answer_delta')
      .map((event) => String(event.data.delta || ''))
      .join('')
    const assistantEntry = events.find((event) => event.event === 'assistant_entry')?.data.entry as { content?: string } | undefined

    expect(answerDeltas).not.toContain('I will attempt')
    expect(answerDeltas).toContain('easydo_resource_base_info_refresh')
    expect(answerDeltas).toContain('"task_id":12')
    expect(assistantEntry?.content).toContain('easydo_resource_base_info_refresh')
    expect(assistantEntry?.content).not.toContain('I will attempt')
  })

  it('blocks new session entries while a run is awaiting an action decision and releases after continuation', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The blocked run can continue now.' }
        }
        return {
          text: 'I need a pipeline confirmation.',
          tool_calls: [{
            id: 'call-active-run-conflict',
            name: 'easydo_pipeline_trigger',
            arguments: { pipeline_id: 13 },
            operation_type: 'execute',
            target_type: 'pipeline',
            target_id: '13'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: 'pipeline triggered',
          structured_content: { ok: true },
          metadata: { request_id: 'mcp-active-run-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'trigger e1',
      client_entry_id: 'active-run-first'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)
    const entriesBeforeConflict = await store.listEntries(actor.workspace_id, session.id)
    const originalFindActiveRun = store.findActiveRun.bind(store)
    let hideNextActiveRunLookup = false
    store.findActiveRun = async (workspaceID, sessionID) => {
      if (hideNextActiveRunLookup) {
        hideNextActiveRunLookup = false
        return undefined
      }
      return originalFindActiveRun(workspaceID, sessionID)
    }

    expect(firstRun.run?.status).toBe('awaiting_decision')
    expect(firstRun.run?.active_slot).toBe('active')
    await expect(store.findActiveRun(actor.workspace_id, session.id)).resolves.toMatchObject({
      runtime_run_id: firstRun.run?.runtime_run_id,
      active_slot: 'active'
    })
    hideNextActiveRunLookup = true
    await expect(service.createEntryRun(actor, session.id, {
      content: 'start another request',
      client_entry_id: 'active-run-second'
    })).rejects.toMatchObject({
      code: 'active_run_conflict',
      status: 409,
      details: {
        active_run: expect.objectContaining({
          runtime_run_id: firstRun.run?.runtime_run_id,
          status: 'awaiting_decision',
          next_steps: ['approve_once', 'approve_session', 'reject', 'cancel']
        })
      }
    } satisfies Partial<RuntimeDomainError>)
    await expect(store.listEntries(actor.workspace_id, session.id)).resolves.toHaveLength(entriesBeforeConflict.length)

    const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    expect(continued.run.status).toBe('completed')
    expect(continued.run.active_slot).toBeUndefined()
    await expect(store.findActiveRun(actor.workspace_id, session.id)).resolves.toBeUndefined()

    const nextRun = await service.createEntryRun(actor, session.id, {
      content: 'now answer normally',
      client_entry_id: 'active-run-after-release'
    })
    expect(nextRun.user_entry.content).toBe('now answer normally')
    expect(nextRun.run?.runtime_run_id).not.toBe(firstRun.run?.runtime_run_id)
    expect(nextRun.run?.active_slot).toBe('active')
  })

  it('persists action decisions and executions as first-class readable objects', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The persisted execution completed.' }
        }
        return {
          text: 'I need approval.',
          tool_calls: [{
            id: 'call-decision-execution',
            name: 'easydo_pipeline_trigger',
            arguments: { pipeline_id: 13 },
            operation_type: 'execute',
            target_type: 'pipeline',
            target_id: '13'
          }]
        }
      }
    }
    let executeCount = 0
    const executor: RuntimeToolExecutor = {
      async execute() {
        executeCount += 1
        return {
          content: 'pipeline triggered',
          structured_content: { ok: true, run_id: 99 },
          metadata: { request_id: 'mcp-decision-execution-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'trigger with persisted decision',
      client_entry_id: 'decision-execution-run'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)
    const actionInternalID = Number(agentActions[0]?.internal_id)

    const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', {
      client_decision_id: 'decision-execution-approve'
    })
    await expect(actions.decideAndContinue(actor, actionID, 'approve_once', {
      client_decision_id: 'decision-execution-approve'
    })).rejects.toMatchObject({
      code: 'action_already_decided',
      status: 409
    })
    const decisions = await store.listActionDecisions(actor.workspace_id, actionInternalID)
    const executions = await store.listActionExecutions(actor.workspace_id, actionInternalID)

    expect(executeCount).toBe(1)
    expect(continued.action.status).toBe('executed')
    expect(decisions).toHaveLength(1)
    expect(decisions[0]).toMatchObject({
      decision_id: 'd_wb_000001_000001_000001_000001',
      decision: 'approve_once',
      decision_type: 'user',
      client_decision_id: 'decision-execution-approve',
      idempotency_key: 'decision:a_wb_000001_000001_000001:user7:client:decision-execution-approve'
    })
    expect(executions).toHaveLength(1)
    expect(executions[0]).toMatchObject({
      execution_id: 'x_wb_000001_000001_000001_000001',
      attempt: 1,
      status: 'succeeded',
      external_request_id: 'mcp-decision-execution-1',
      idempotency_key: 'execution:a_wb_000001_000001_000001:attempt000001'
    })
  })

  it('uses approve_session grants to skip repeated approval in the same session and resource scope', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The granted action completed.' }
        }
        return {
          text: 'I need a session-scoped approval.',
          tool_calls: [{
            id: `call-session-grant-${request.content.includes('second') ? '2' : '1'}`,
            name: 'easydo_pipeline_trigger',
            arguments: { pipeline_id: 13 },
            operation_type: 'execute',
            target_type: 'pipeline',
            target_id: '13'
          }]
        }
      }
    }
    let executeCount = 0
    const executor: RuntimeToolExecutor = {
      async execute() {
        executeCount += 1
        return {
          content: 'pipeline triggered',
          structured_content: { ok: true, count: executeCount },
          metadata: { request_id: `session-grant-${executeCount}` }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: { default_decision: 'request' }
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'first session grant action',
      client_entry_id: 'session-grant-first'
    })
    const firstAction = (firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>)[0]
    expect(firstRun.run?.status).toBe('awaiting_decision')

    const continued = await actions.decideAndContinue(actor, firstAction.id, 'approve_session', {
      client_decision_id: 'session-grant-approve'
    })
    const grants = await store.listSessionPermissionGrants(actor.workspace_id, session.id)
    expect(grants).toHaveLength(1)
    expect(grants[0]).toMatchObject({
      grant_id: 'grant_wb_000001_000001',
      permission_key: 'tool:easydo_pipeline_trigger:execute',
      tool_name: 'easydo_pipeline_trigger',
      resource_type: 'pipeline',
      resource_id: '13',
      status: 'active'
    })
    const continuationEvents = continued.assistant_entry.output.runtime_events as Array<{ type: string, payload: Record<string, unknown> }>
    const grantEvent = continuationEvents.find((event) => event.type === 'approval.session_granted')
    expect(grantEvent?.payload).toMatchObject({
      grant_id: grants[0].grant_id,
      action_id: firstAction.id,
      permission_key: 'tool:easydo_pipeline_trigger:execute',
      tool_name: 'easydo_pipeline_trigger',
      resource_type: 'pipeline',
      resource_id: '13'
    })

    const secondRun = await service.createEntryRun(actor, session.id, {
      content: 'second session grant action',
      client_entry_id: 'session-grant-second'
    })
    const secondActions = secondRun.assistant_entry.output.agent_actions as Array<{ status: string, policy_json: Record<string, unknown> }>
    expect(secondRun.run?.status).toBe('completed')
    expect(secondActions[0]).toMatchObject({
      status: 'executed',
      policy_json: expect.objectContaining({
        requires_decision: false,
        session_grant_id: grants[0].grant_id
      })
    })
    expect(executeCount).toBe(2)
  })

  it('persists approved actions and user decision entries as first-class session history', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The approved action completed.' }
        }
        return {
          text: 'I need to run an action.',
          tool_calls: [{
            id: 'call-action-approve',
            name: 'easydo_pipeline_trigger',
            arguments: { pipeline_id: 13 },
            operation_type: 'execute',
            target_type: 'pipeline',
            target_id: '13'
          }]
        }
      }
    }
    let executeCount = 0
    const executor: RuntimeToolExecutor = {
      async execute() {
        executeCount += 1
        return {
          content: 'pipeline triggered',
          structured_content: { ok: true, run_id: 99 },
          metadata: { request_id: 'action-request-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'trigger e1',
      client_entry_id: 'action-decision-history'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number, status: string }>
    const actionID = String(agentActions[0]?.id)

    const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    await expect(actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })).rejects.toMatchObject({
      code: 'action_already_decided',
      status: 409
    })
    const entries = await service.listEntries(actor, session.id)

    expect(firstRun.assistant_entry.output).not.toHaveProperty('tool_confirmations')
    expect(agentActions[0]).toMatchObject({ status: 'awaiting_decision', action_kind: 'pipeline.trigger' })
    expect(agentActions[0]).not.toHaveProperty('tool_name')
    expect(agentActions[0]).not.toHaveProperty('tool_call_id')
    expect(executeCount).toBe(1)
    expect(continued.action.status).toBe('executed')
    expect(entries.some((entry) =>
      entry.role === 'user' &&
      entry.entry_type === 'run_event' &&
      entry.content.includes('已确认') &&
      String(entry.input.action_id || '') === actionID
    )).toBe(true)
  })

  it('streams provider empty-output retry status before waiting for action continuation recovery', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let releaseRecovery: () => void = () => {}
    const recoveryGate = new Promise<void>((resolve) => {
      releaseRecovery = resolve
    })
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('provider_empty_output_detected')) {
          await recoveryGate
          return { text: 'Recovered visible action continuation.' }
        }
        if (request.content.includes('action_result')) {
          return { text: '   ' }
        }
        return {
          text: 'I need to run an action.',
          tool_calls: [{
            id: 'call-action-empty-retry',
            name: 'easydo_test_write',
            arguments: { value: 'created' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      },
      async *stream(request) {
        if (request.content.includes('action_result')) {
          yield { type: 'reasoning_delta', delta: 'internal-only action continuation' }
          return
        }
        return
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: 'write queued',
          structured_content: { ok: true },
          metadata: { request_id: 'action-empty-retry' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write',
      client_entry_id: 'action-empty-retry'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string }>
    const actionID = String(agentActions[0]?.id)
    const events: RuntimeStreamEvent[] = []

    const reader = (async () => {
      for await (const event of actions.decideAndContinueStream(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })) {
        events.push(event)
      }
    })()

    await new Promise<void>((resolve, reject) => {
      const startedAt = Date.now()
      const poll = () => {
        if (events.some((event) => event.event === 'provider.empty_output_detected')) {
          resolve()
          return
        }
        if (Date.now() - startedAt > 1000) {
          reject(new Error('provider.empty_output_detected was not streamed before recovery completed'))
          return
        }
        setTimeout(poll, 5)
      }
      poll()
    })
    expect(events.map((event) => event.event)).not.toContain('answer_delta')

    releaseRecovery()
    await reader

    const eventNames = events.map((event) => event.event)
    expect(eventNames.indexOf('provider.empty_output_detected')).toBeLessThan(eventNames.indexOf('answer_delta'))
    expect(eventNames).toContain('provider.continuation_completed')
    expect(eventNames).toContain('run.completed')
    expect(eventNames).toContain('done')
  })

  it('rejects an already completed confirmed tool continuation without executing the tool again', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The write tool completed once.' }
        }
        return {
          text: 'I need to write once.',
          tool_calls: [{
            id: 'call-repeat-confirm',
            name: 'easydo_test_write',
            arguments: { value: 'created' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      }
    }
    let executeCount = 0
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        executeCount += 1
        return {
          content: `executed ${call.tool_name}`,
          structured_content: { ok: true, count: executeCount },
          metadata: { request_id: `mcp-repeat-${executeCount}` }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write once',
      client_entry_id: 'tool-run-repeat-confirm'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    await expect(actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })).rejects.toMatchObject({
      code: 'action_already_decided',
      status: 409
    })

    expect(executeCount).toBe(1)
    expect(continued.action.status).toBe('executed')
  })

  it('waits for an in-flight repeated action decision and rejects it after the first completes', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The slow action completed.' }
        }
        return {
          text: 'I need a slow write.',
          tool_calls: [{
            id: 'call-slow-repeat',
            name: 'easydo_test_write',
            arguments: { value: 'slow' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'slow'
          }]
        }
      }
    }
    let executeCount = 0
    let resolveExecuteStarted: () => void = () => {}
    let releaseExecute: () => void = () => {}
    const executeStarted = new Promise<void>((resolve) => {
      resolveExecuteStarted = resolve
    })
    const executeReleased = new Promise<void>((resolve) => {
      releaseExecute = resolve
    })
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        executeCount += 1
        resolveExecuteStarted()
        await executeReleased
        return {
          content: `executed ${call.tool_name}`,
          structured_content: { ok: true, count: executeCount },
          metadata: { request_id: `mcp-slow-${executeCount}` }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write slowly',
      client_entry_id: 'tool-run-repeat-inflight'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    const firstDecision = actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    await executeStarted
    const repeatedDecision = actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    releaseExecute()
    const [continued, repeatedError] = await Promise.all([
      firstDecision,
      repeatedDecision.catch((error) => error)
    ])

    expect(executeCount).toBe(1)
    expect(continued.action.status).toBe('executed')
    expect(repeatedError).toMatchObject({
      code: 'action_already_decided',
      status: 409
    })
  })

  it('serializes simultaneous action decisions and rejects the second settled decision', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The simultaneous action completed.' }
        }
        return {
          text: 'I need a write.',
          tool_calls: [{
            id: 'call-simultaneous-repeat',
            name: 'easydo_test_write',
            arguments: { value: 'simultaneous' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'simultaneous'
          }]
        }
      }
    }
    let executeCount = 0
    let resolveExecuteStarted: () => void = () => {}
    let releaseExecute: () => void = () => {}
    const executeStarted = new Promise<void>((resolve) => {
      resolveExecuteStarted = resolve
    })
    const executeReleased = new Promise<void>((resolve) => {
      releaseExecute = resolve
    })
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        executeCount += 1
        resolveExecuteStarted()
        await executeReleased
        return {
          content: `executed ${call.tool_name}`,
          structured_content: { ok: true, count: executeCount },
          metadata: { request_id: `mcp-simultaneous-${executeCount}` }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write with duplicate clicks',
      client_entry_id: 'tool-run-repeat-simultaneous'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    const firstDecision = actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    const repeatedDecision = actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    await executeStarted
    releaseExecute()
    const [continued, repeatedError] = await Promise.all([
      firstDecision,
      repeatedDecision.catch((error) => error)
    ])

    expect(executeCount).toBe(1)
    expect(continued.action.status).toBe('executed')
    expect(repeatedError).toMatchObject({
      code: 'action_already_decided',
      status: 409
    })
  })

  it('deduplicates semantically identical pending tool actions in one run', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        return {
          text: 'I need the same tool twice.',
          tool_calls: [
            {
              id: 'duplicate-call-1',
              name: 'easydo_pipeline_trigger',
              arguments: { pipeline_id: 13, parameters: { branch: 'main' } },
              operation_type: 'execute',
              target_type: 'pipeline',
              target_id: '13'
            },
            {
              id: 'duplicate-call-2',
              name: 'easydo_pipeline_trigger',
              arguments: { parameters: { branch: 'main' }, pipeline_id: 13 },
              operation_type: 'execute',
              target_type: 'pipeline',
              target_id: '13'
            }
          ]
        }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'trigger e1',
      client_entry_id: 'tool-run-duplicate-confirmation'
    })
    const actions = result.assistant_entry.output.agent_actions as Array<Record<string, unknown>>

    expect(actions).toHaveLength(1)
    expect(actions[0]).toMatchObject({
      action_kind: 'pipeline.trigger',
      capability_id: 'easydo_pipeline_trigger',
      input_json: expect.objectContaining({ tool_name: 'easydo_pipeline_trigger' }),
      target_json: expect.objectContaining({ target_type: 'pipeline', target_id: '13' })
    })
    expect(actions[0]).not.toHaveProperty('tool_name')
    expect(actions[0]).not.toHaveProperty('tool_call_id')
  })

  it('hydrates action status when listing existing entries', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          return { text: 'The write tool completed.' }
        }
        return {
          text: 'I need to write.',
          tool_calls: [{
            id: 'call-hydrate',
            name: 'easydo_test_write',
            arguments: { value: 'created' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: 'executed write',
          structured_content: { ok: true },
          metadata: { request_id: 'mcp-hydrate' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write',
      client_entry_id: 'tool-run-hydrate'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    await actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })
    const entries = await service.listEntries(actor, session.id)
    const originalAssistant = entries.find((entry) => entry.id === firstRun.assistant_entry.id)
    const hydrated = originalAssistant?.output.agent_actions as Array<Record<string, unknown>>

    expect(hydrated[0]).toMatchObject({
      id: actionID,
      status: 'executed',
      result_json: expect.objectContaining({
        structured_content: { ok: true },
        external_request_id: 'mcp-hydrate'
      })
    })
  })

  it('keeps confirmed tool execution visible when continuation model call fails', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_result')) {
          throw new Error('Provider returned error')
        }
        return {
          text: 'I need to write.',
          tool_calls: [{
            id: 'call-model-fail-after-tool',
            name: 'easydo_test_write',
            arguments: { value: 'created' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        return {
          content: `executed ${call.tool_name}`,
          structured_content: { ok: true },
          metadata: { request_id: 'mcp-request-model-fail' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please write',
      client_entry_id: 'tool-run-model-fails'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', { client_decision_id: 'approve-action' })

    expect(continued.action.status).toBe('executed')
    expect(continued.action.result_json.structured_content).toMatchObject({ ok: true })
    expect(continued.tool_entry.status).toBe('completed')
    expect(continued.assistant_entry.status).toBe('failed')
    expect(continued.assistant_entry.content).toContain('Provider returned error')
    expect(continued.run.status).toBe('failed')
    expect(continued.run.result.runtime_events).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'tool.executed' }),
      expect.objectContaining({ type: 'model_provider.failed' }),
      expect.objectContaining({ type: 'run.failed' })
    ]))
  })

  it('records a rejected tool call and continues with a rejection-aware answer', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('action_rejected')) {
          return { text: 'I will answer without the rejected tool.' }
        }
        return {
          text: 'I need a risky tool.',
          tool_calls: [{
            id: 'call-reject',
            name: 'easydo_test_write',
            arguments: { value: 'blocked' },
            operation_type: 'write',
            target_type: 'test_record',
            target_id: 'local'
          }]
        }
      }
    }
    const service = new SessionService(store, profiles, client)
    const actions = new ActionService(store, service)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const firstRun = await service.createEntryRun(actor, session.id, {
      content: 'please use risky tool',
      client_entry_id: 'tool-run-reject'
    })
    const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const actionID = String(agentActions[0]?.id)

    const continued = await actions.decideAndContinue(actor, actionID, 'reject', { client_decision_id: 'reject-action' })
    const entries = await store.listEntries(actor.workspace_id, session.id)

    expect(continued.action.status).toBe('rejected')
    expect(entries.some((entry) => entry.entry_type === 'tool_result' && entry.status === 'cancelled')).toBe(true)
    expect(continued.assistant_entry.content).toContain('without the rejected tool')
  })

  it('executes a confirmed tool through the bound MCP server by default', async () => {
    let sawToolCall = false
    let sawAuth = ''
    let sawRequestID = ''
    const mcp = await withMcpServer(async (req, res) => {
      sawAuth = String(req.headers.authorization || '')
      let body = ''
      for await (const chunk of req) body += chunk
      const rpc = JSON.parse(body)
      res.setHeader('content-type', 'application/json')
      if (rpc.method === 'initialize') {
        res.setHeader('mcp-session-id', 'tool-session')
        res.end(JSON.stringify({ jsonrpc: '2.0', id: rpc.id, result: { protocolVersion: '2025-06-18', capabilities: {} } }))
        return
      }
      if (rpc.method === 'notifications/initialized') {
        res.statusCode = 202
        res.end()
        return
      }
      sawToolCall = rpc.method === 'tools/call' && rpc.params?.name === 'easydo_test_write'
      sawRequestID = String(rpc.id || '')
      res.end(JSON.stringify({
        jsonrpc: '2.0',
        id: rpc.id,
        result: {
          content: [{ type: 'text', text: 'mcp created record' }],
          structuredContent: { ok: true, id: 123 },
          metadata: { request_id: 'mcp-jsonrpc-1' }
        }
      }))
    })
    try {
      const store = createMemoryRuntimeStore()
      const profiles = new AgentProfileService(store)
      const client: ChatModelClient = {
        async complete(request) {
          if (request.content.includes('action_result')) {
            return { text: 'The MCP result was applied.' }
          }
          return {
            text: 'Need MCP write.',
            tool_calls: [{
              id: 'mcp-call-1',
              name: 'easydo_test_write',
              arguments: { value: 'created' },
              operation_type: 'write',
              target_type: 'test_record',
              target_id: '123'
            }]
          }
        }
      }
      const service = new SessionService(store, profiles, client)
      const actions = new ActionService(store, service)
      await profiles.createResource(actor, {
        resource_kind: 'mcp_server',
        resource_key: 'local-mcp',
        name: 'Local MCP',
        status: 'active',
        spec: {
          discovered_tools: [{ name: 'easydo_test_write' }],
          mcpServers: {
            local: {
              type: 'streamable_http',
              url: `${mcp.url}/mcp`
            }
          }
        }
      })
      const profile = await profiles.createProfile(actor, profilePayload({
        mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'local-mcp' }]
      }))
      const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
      const firstRun = await service.createEntryRun(actor, session.id, {
        content: 'please write through mcp',
        client_entry_id: 'mcp-tool-run'
      })
      const agentActions = firstRun.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
      const actionID = String(agentActions[0]?.id)
      const continued = await actions.decideAndContinue(actor, actionID, 'approve_once', {
        client_decision_id: 'mcp-tool-approve'
      }, {
        delegated_user_token: 'Bearer jwt-token',
        server_internal_token: 'runtime-secret'
      })

      expect(sawToolCall).toBe(true)
      expect(sawAuth).toBe('Bearer jwt-token')
      expect(sawRequestID).toBe('mcp_call_wb_000001_000001_easydo_test_write_000001')
      expect(continued.action.status).toBe('executed')
      expect(continued.action.result_json.structured_content).toMatchObject({ ok: true, id: 123 })
      expect(continued.tool_entry.content).toContain('mcp created record')
    } finally {
      await mcp.close()
    }
  })

  it('applies server-scoped MCP permission rules to actual model tool calls', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let executedCall: RuntimeToolCall | undefined
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('tool_result')) {
          return { text: 'MCP write completed.' }
        }
        return {
          text: 'Need MCP write.',
          tool_calls: [{
            id: 'mcp-scoped-call-1',
            name: 'easydo_test_write',
            arguments: { value: 'created' },
            operation_type: 'write'
          }]
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        executedCall = call
        return {
          content: 'scoped mcp executed',
          structured_content: { ok: true },
          metadata: { request_id: 'scoped-mcp-1' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'local-mcp',
      name: 'Local MCP',
      status: 'active',
      spec: {
        discovered_tools: [{ name: 'easydo_test_write', operation_type: 'write' }],
        mcpServers: {
          local: {
            type: 'streamable_http',
            url: 'http://127.0.0.1/mcp'
          }
        }
      }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      mcp_servers: [{
        resource_type: 'mcp_server',
        resource_id: 'local-mcp',
        config: {
          tool_permissions: {
            rules: [{
              id: 'allow-local-mcp-write',
              match: 'mcp.local-mcp.tools.easydo_test_write.write',
              decision: 'allow'
            }]
          }
        }
      }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'please write through scoped mcp policy',
      client_entry_id: 'mcp-scoped-policy'
    })

    const agentActions = result.assistant_entry.output.agent_actions as Array<Record<string, unknown>>
    expect(result.run?.status).toBe('completed')
    expect(executedCall).toMatchObject({
      tool_name: 'easydo_test_write',
      mcp_server_key: 'local-mcp',
      capability: 'tools'
    })
    expect(agentActions[0]).toMatchObject({
      status: 'executed',
      policy_json: expect.objectContaining({
        decision: 'allow',
        matched_rule: 'allow-local-mcp-write',
        permission_key: 'mcp:local-mcp:tools:easydo_test_write:write'
      }),
      input_json: expect.objectContaining({
        mcp_server_key: 'local-mcp',
        capability: 'tools'
      })
    })
  })

  it('passes bound MCP tools to the chat model as callable function definitions', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let capturedTools: unknown
    const client: ChatModelClient = {
      async complete(request) {
        capturedTools = request.tools
        return { text: 'I can see the tools.' }
      }
    }
    const service = new SessionService(store, profiles, client)
    await profiles.createResource(actor, {
      resource_kind: 'mcp_server',
      resource_key: 'local-mcp',
      name: 'Local MCP',
      status: 'active',
      spec: {
        discovered_tools: [{
          name: 'easydo_pipeline_trigger',
          description: 'Trigger an EasyDo pipeline.',
          input_schema: {
            type: 'object',
            required: ['pipeline_id'],
            properties: {
              pipeline_id: { type: 'integer' }
            }
          }
        }],
        mcpServers: {
          local: {
            type: 'streamable_http',
            url: 'http://127.0.0.1/mcp'
          }
        }
      }
    })
    const profile = await profiles.createProfile(actor, profilePayload({
      mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'local-mcp' }]
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    await service.createEntryRun(actor, session.id, {
      content: '帮我触发流水线',
      client_entry_id: 'mcp-tool-definitions'
    })

    expect(capturedTools).toEqual([expect.objectContaining({
      name: 'easydo_pipeline_trigger',
      description: 'Trigger an EasyDo pipeline.',
      input_schema: {
        type: 'object',
        required: ['pipeline_id'],
        properties: {
          pipeline_id: { type: 'integer' }
        }
      },
      mcp_server_key: 'local-mcp',
      mcp_server_id: 1,
      capability: 'tools'
    })])
  })

  it('converts JSON tool calls with string function names into actions', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        return {
          text: JSON.stringify({
            tool_calls: [{
              function: 'easydo_pipeline_trigger_preview',
              arguments: {
                workspace_id: actor.workspace_id,
                pipeline_id: 13
              }
            }]
          })
        }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'preview pipeline 13',
      client_entry_id: 'string-function-tool-call'
    })

    const actions = result.assistant_entry.output.agent_actions as Array<Record<string, unknown>>
    expect(asRecord(actions[0]?.input_json).tool_name).toBe('easydo_pipeline_trigger_preview')
    expect(asRecord(actions[0]?.input_json).provider_tool_call_id).toBe('tc_easydo_pipeline_trigger_preview_000001')
    expect(actions[0]).not.toHaveProperty('tool_name')
    expect(actions[0]).not.toHaveProperty('tool_call_id')
  })

  it('uses readable action idempotency keys that do not hash long MCP arguments', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete() {
        return {
          text: '',
          tool_calls: [{
            id: 'long-argument-call',
            name: 'easydo_pipeline_trigger',
            operation_type: 'write',
            arguments: {
              workspace_id: actor.workspace_id,
              pipeline_id: 13,
              note: 'x'.repeat(600)
            }
          }]
        }
      }
    }
    const service = new SessionService(store, profiles, client)
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

    const result = await service.createEntryRun(actor, session.id, {
      content: 'trigger with long arguments',
      client_entry_id: 'long-action-idempotency'
    })

    const agentActions = result.assistant_entry.output.agent_actions as Array<{ id: string, internal_id: number }>
    const action = await store.getActionByBusinessID(actor.workspace_id, agentActions[0].id)
    expect(action?.idempotency_key).toMatch(/^action:r_w[a-z0-9]+_[a-z0-9]{6}_[a-z0-9]{6}:turn[a-z0-9]{6}:part[a-z0-9]{6}:easydo_pipeline_trigger$/)
    expect(action?.idempotency_key.length).toBeLessThanOrEqual(191)
    expect(action?.idempotency_key).not.toContain('xxxxxxxxxxxxxxxxxxxx')
    expect(action?.idempotency_key).not.toMatch(/[a-f0-9]{64}$/)
  })

  it('rejects long client entry ids instead of hashing them into idempotency keys', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient('ok'))
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const clientEntryID = `client-${'x'.repeat(260)}`

    await expect(service.createEntryRun(actor, session.id, {
      content: 'hello',
      client_entry_id: clientEntryID
    })).rejects.toMatchObject({ code: 'client_entry_id_invalid' })
  })

  it('automatically executes read-only JSON tool calls in stream runs and continues with the tool result', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    let executedTool = ''
    let continuationContent = ''
    const client: ChatModelClient = {
      async complete(request) {
        continuationContent = request.content
        return { text: 'GPU usage is 42%.' }
      },
      async *stream() {
        yield {
          type: 'answer_delta',
          delta: JSON.stringify({
            tool_calls: [{
              id: 'gpu-read-call',
              name: 'easydo_resource_gpu_usage',
              operation_type: 'read',
              arguments: {
                workspace_id: actor.workspace_id,
                resource_id: 1
              }
            }]
          })
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute(call) {
        executedTool = call.tool_name
        await new Promise((resolve) => setTimeout(resolve, 20))
        toolExecutionCompleted = true
        return {
          content: 'GPU 0: 42%',
          structured_content: { gpu_usage: [{ index: 0, utilization: 42 }] },
          metadata: { request_id: 'gpu-read-request' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_gpu_usage')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events = []
    let toolExecutionCompleted = false
    const eventsBeforeToolCompletion: string[] = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: '查询 GPU 使用率',
      client_entry_id: 'readonly-tool-stream'
    })) {
      events.push(event)
      if (!toolExecutionCompleted) {
        eventsBeforeToolCompletion.push(event.event)
      }
    }

    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const assistantEntry = assistantEvent?.data?.entry as { content?: string, output?: Record<string, unknown> } | undefined
    const answerDeltas = events
      .filter((event) => event.event === 'answer_delta')
      .map((event) => event.data?.delta)
    expect(executedTool).toBe('easydo_resource_gpu_usage')
    expect(continuationContent).toContain('tool_result')
    expect(events).toEqual(expect.arrayContaining([
      expect.objectContaining({ event: 'tool.executed' })
    ]))
    expect(eventsBeforeToolCompletion).toContain('action.execution_started')
    expect(eventsBeforeToolCompletion).not.toContain('tool.executed')
    expect(answerDeltas).toEqual(['GPU usage is 42%.'])
    expect(assistantEntry?.content).toBe('GPU usage is 42%.')
    const agentActions = assistantEntry?.output?.agent_actions as Array<Record<string, unknown>> | undefined
    expect(agentActions?.[0]).toMatchObject({
      action_kind: 'mcp.tool',
      status: 'executed',
      input_json: expect.objectContaining({ tool_name: 'easydo_resource_gpu_usage' })
    })
    expect(agentActions?.[0]).not.toHaveProperty('tool_name')
    const executedEvent = events.find((event) => event.event === 'tool.executed')
    expect(executedEvent?.data?.action_id).toBe(agentActions?.[0]?.id)
  })

  it('does not complete stream automatic tool runs with empty text when the model repeats a tool call', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const client: ChatModelClient = {
      async complete(request) {
        if (request.content.includes('tool_result')) {
          return {
            text: '',
            finish: { reason: 'tool_calls' },
            tool_calls: [{
              id: 'stream-list-resource-repeat',
              name: 'easydo_resource_list',
              arguments: { workspace_id: actor.workspace_id, query: '7024' },
              operation_type: 'read',
              target_type: 'resource'
            }]
          }
        }
        return { text: 'unused stream fallback' }
      },
      async *stream() {
        yield {
          type: 'tool_call',
          tool_call: {
            id: 'stream-list-resource',
            name: 'easydo_resource_list',
            arguments: { workspace_id: actor.workspace_id, query: '7024' },
            operation_type: 'read',
            target_type: 'resource'
          }
        }
      }
    }
    const executor: RuntimeToolExecutor = {
      async execute() {
        return {
          content: JSON.stringify({ list: [], total: 0, page: 1, limit: 20 }),
          structured_content: { list: [], total: 0, page: 1, limit: 20 },
          metadata: { request_id: 'stream-resource-list-empty' }
        }
      }
    }
    const service = new SessionService(store, profiles, client, executor)
    const profile = await profiles.createProfile(actor, profilePayload({
      tool_policy: allowToolPolicy('easydo_resource_list')
    }))
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const events: RuntimeStreamEvent[] = []

    for await (const event of service.streamEntryRun(actor, session.id, {
      content: '触发一下7024机器的显卡占用采集',
      client_entry_id: 'repeat-tool-call-empty-stream'
    })) {
      events.push(event)
    }

    const assistantEvent = events.find((event) => event.event === 'assistant_entry')
    const assistantEntry = assistantEvent?.data?.entry as {
      status?: string
      content?: string
      output?: { runtime_events?: Array<{ type: string, payload: Record<string, unknown> }> }
    } | undefined
    const runtimeEvents = assistantEntry?.output?.runtime_events || []

    expect(runtimeEvents).toEqual(expect.arrayContaining([
      expect.objectContaining({
        type: 'model.continuation_completed',
        payload: expect.objectContaining({
          finish_reason: 'tool_calls',
          tool_call_count: 1
        })
      })
    ]))
    expect(assistantEntry?.status).toBe('completed')
    expect(String(assistantEntry?.content || '').trim()).not.toBe('')
    expect(assistantEntry?.content).toContain('easydo_resource_list')
    expect(assistantEntry?.content).toContain('"total":0')
  })

  it('discovers built-in EasyDo MCP tools with delegated auth before the model call', async () => {
    let sawAuth = ''
    let sawRequestID = ''
    const mcp = await withMcpServer(async (req, res) => {
      sawAuth = String(req.headers.authorization || '')
      let body = ''
      for await (const chunk of req) body += chunk
      const rpc = JSON.parse(body)
      res.setHeader('content-type', 'application/json')
      if (rpc.method === 'initialize') {
        res.setHeader('mcp-session-id', 'discovery-session')
        res.end(JSON.stringify({ jsonrpc: '2.0', id: rpc.id, result: { protocolVersion: '2025-06-18', capabilities: {} } }))
        return
      }
      if (rpc.method === 'notifications/initialized') {
        res.statusCode = 202
        res.end()
        return
      }
      sawRequestID = String(rpc.id || '')
      res.end(JSON.stringify({
        jsonrpc: '2.0',
        id: rpc.id,
        result: {
          tools: [{
            name: 'easydo_pipeline_list',
            description: 'List EasyDo pipelines.',
            inputSchema: {
              type: 'object',
              properties: {
                workspace_id: { type: 'integer' }
              }
            }
          }]
        }
      }))
    })
    try {
      const store = createMemoryRuntimeStore()
      const profiles = new AgentProfileService(store)
      let capturedTools: unknown
      const client: ChatModelClient = {
        async complete(request) {
          capturedTools = request.tools
          return { text: 'I can list pipelines.' }
        }
      }
      const service = new SessionService(store, profiles, client, undefined, {
        easydoServerURL: mcp.url
      })
      const profile = await profiles.createProfile(actor, profilePayload({
        mcp_servers: [{ resource_type: 'mcp_server', resource_id: 'easydo' }]
      }))
      const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))

      await service.createEntryRun(actor, session.id, {
        content: '列出流水线',
        client_entry_id: 'builtin-mcp-tools'
      }, {
        delegated_user_token: 'Bearer delegated-token',
        server_internal_token: 'runtime-secret'
      })

      expect(sawAuth).toBe('Bearer delegated-token')
      expect(sawRequestID).toBe('mcp_tools_wb_000000_easydo')
      expect(capturedTools).toEqual([expect.objectContaining({
        name: 'easydo_pipeline_list',
        description: 'List EasyDo pipelines.',
        input_schema: {
          type: 'object',
          properties: {
            workspace_id: { type: 'integer' }
          }
        },
        mcp_server_key: 'easydo',
        mcp_server_id: 0,
        capability: 'tools'
      })])
    } finally {
      await mcp.close()
    }
  })

  it('cancels the active streaming run and entry for a chatbox session', async () => {
    const store = createMemoryRuntimeStore()
    const profiles = new AgentProfileService(store)
    const service = new SessionService(store, profiles, chatClient())
    const profile = await profiles.createProfile(actor, profilePayload())
    const session = await service.getCurrentSession(actor, chatboxSessionPayload(profile.id))
    const timestamp = new Date().toISOString()
    await store.saveRun({
      id: await store.nextRunId(),
      runtime_run_id: 'run-cancel',
      session_id: session.id,
      workspace_id: actor.workspace_id,
      context_tags: session.context_tags,
      agent_profile_id: profile.id,
      agent_profile_version_id: 0,
      agent_profile_version_key: 'latest',
      agent_profile_snapshot_hash: 'sha256:test',
      status: 'running',
      input_entry_id: 1,
      request: {},
      result: {},
      usage: {},
      started_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp
    })
    await store.saveEntry({
      id: await store.nextEntryId(),
      session_id: session.id,
      workspace_id: actor.workspace_id,
      user_id: actor.user_id,
      seq: 1,
      entry_type: 'message',
      role: 'assistant',
      status: 'streaming',
      content: 'partial',
      content_blocks: [{ type: 'text', text: 'partial' }],
      input: {},
      output: {},
      runtime_run_id: 'run-cancel',
      idempotency_key: 'cancel-entry',
      created_at: timestamp,
      updated_at: timestamp
    })

    const result = await service.cancelSessionRun(actor, session.id, { runtime_run_id: 'run-cancel' })
    const run = await store.getRun(actor.workspace_id, 'run-cancel')
    const entries = await store.listEntries(actor.workspace_id, session.id)

    expect(result.status).toBe('cancelled')
    expect(run?.status).toBe('cancelled')
    expect(entries[0]?.status).toBe('cancelled')
  })
})
