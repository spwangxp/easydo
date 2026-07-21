import { PiEventAdapter } from './piAdapter.js'
import type { AgentEventStore } from './eventStore.js'
import type { AgentRuntimeEvent, ModelRef } from './events.js'
import type { PiModelSelection } from './piHarnessFactory.js'
import { PiToolApprovalRequiredError } from './piResources.js'
import type { AgentTool, Skill } from '@earendil-works/pi-agent-core/node'
import type { ExecutionEnv } from '@earendil-works/pi-agent-core'
import { classifyRuntimeError, RuntimeSourceError } from '../services/runtimeErrorClassifier.js'
import { runtimeLogger, type RuntimeLogger, type RuntimeLogInput } from '../observability/runtimeLogger.js'
import type { SubagentMode } from '../services/subagentMode.js'
import {
  MissingBindingContextWindowError,
  buildHistoryCompactionSummary,
  contextBudgetEventPayload,
  contextCompactionEventPayload,
  evaluateContextBudget,
  planHistoryCompaction,
  type ContextBudgetEvaluation,
  type HistoryCompactionPlan
} from '../services/contextBudget.js'

export interface PiHarnessLike {
  subscribe(listener: (event: unknown) => void | Promise<void>): () => void
  prompt(text: string, options?: unknown): Promise<unknown>
  steer?(text: string, options?: unknown): Promise<unknown>
  skill?(name: string, additionalInstructions?: string): Promise<unknown>
  abort?(): Promise<unknown>
}

export interface AgentHarnessRunnerOptions {
  store: AgentEventStore
  createHarness: (input: AgentHarnessRunInput) => PiHarnessLike | Promise<PiHarnessLike>
  now?: () => string
  nextID?: (prefix: string) => string
  logger?: RuntimeLogger
}

export interface AgentHarnessRunInput {
  sessionID: string
  runtimeRunID: string
  prompt: string
  agent?: string
  systemPrompt?: string
  model?: ModelRef
  modelConfig?: PiModelSelection
  tools?: AgentTool[]
  skills?: Skill[]
  skillInvocation?: { name: string; instructions?: string }
  files?: Array<{ path: string; mime?: string }>
  history?: PiSessionHistoryMessage[]
  executionEnv?: ExecutionEnv
  requestID?: string
  workspaceID?: number | string
  parentRuntimeRunID?: string
  runMode?: SubagentMode
  toolPolicy?: Record<string, unknown>
  actorRole?: string
  recordEvent?: (event: Record<string, unknown>) => Promise<void> | void
  decideTool?: (input: {
    approvalID: string
    callID: string
    toolName: string
    args: Record<string, unknown>
    reason: string
    operationType: string
    permissionKey: string
  }) => Promise<{ approved: boolean, reason?: string, source?: 'session_grant' | 'policy' } | undefined>
  onSafeCheckpoint?: (checkpoint: { runtime_run_id: string, checkpoint: 'turn_save_point' }) => void | Promise<void>
}

export interface PiSessionHistoryMessage {
  role: 'user' | 'assistant'
  content: string
  timestamp: number
  status?: string
}

export interface AgentHarnessRunResult {
  session_id: string
  user_message_id: string
  assistant_message_id: string
}

export interface AgentHarnessApprovalPause {
  approvalID: string
  callID: string
  toolName: string
  reason: string
}

export class AgentHarnessRunner {
  private readonly store: AgentEventStore
  private readonly createHarness: AgentHarnessRunnerOptions['createHarness']
  private readonly now: () => string
  private readonly nextID: (prefix: string) => string
  private readonly activeHarnesses = new Map<string, PiHarnessLike>()
  private readonly acceptedSteerQueueItems = new Map<string, Set<string>>()
  private readonly pendingSteerQueueItems = new Map<string, Map<string, Promise<void>>>()
  private readonly abortingRuns = new Set<string>()
  private readonly approvalPauses = new Map<string, AgentHarnessApprovalPause>()
  private readonly logger: RuntimeLogger

  constructor(options: AgentHarnessRunnerOptions) {
    this.store = options.store
    this.createHarness = options.createHarness
    this.now = options.now ?? (() => new Date().toISOString())
    let counter = 0
    this.nextID = options.nextID ?? ((prefix) => `${prefix}_${Date.now()}_${++counter}`)
    this.logger = options.logger ?? runtimeLogger
  }

  async prompt(input: AgentHarnessRunInput): Promise<AgentHarnessRunResult> {
    const logContext: RuntimeLogInput = {
      component: 'pi-harness',
      operation: 'prompt',
      request_id: input.requestID,
      workspace_id: input.workspaceID,
      session_id: input.sessionID,
      runtime_run_id: input.runtimeRunID,
      parent_runtime_run_id: input.parentRuntimeRunID,
      provider_id: input.model?.provider_id
    }
    this.logger.info({ ...logContext, outcome: 'started' })
    const userMessageID = this.nextID('user')
    const assistantMessageID = this.nextID('assistant')
    let textID = `${assistantMessageID}:text`
    let assistantText = ''
    let unsubscribe = () => {}

    try {
      await this.append(input.runtimeRunID, {
        type: 'session.prompted',
        session_id: input.sessionID,
        event_id: '',
        seq: 0,
        timestamp: this.now(),
        message_id: userMessageID,
        prompt: input.prompt,
        files: input.files ?? [],
        delivery: 'prompt'
      })
      await this.append(input.runtimeRunID, {
        type: 'session.step.started',
        session_id: input.sessionID,
        event_id: '',
        seq: 0,
        timestamp: this.now(),
        assistant_message_id: assistantMessageID,
        parent_message_id: userMessageID,
        agent: input.agent,
        model: input.model
      })

      let budgetEvaluation: ContextBudgetEvaluation | undefined
      let compactionPlan: HistoryCompactionPlan | undefined
      const preparedBudget = tryPrepareProviderBindingContextBudget(input)
      if (preparedBudget) {
        budgetEvaluation = preparedBudget.evaluation
        compactionPlan = preparedBudget.plan
        await this.append(input.runtimeRunID, {
          type: 'context.budget.evaluated',
          session_id: input.sessionID,
          event_id: '',
          seq: 0,
          timestamp: this.now(),
          message_id: userMessageID,
          should_compact: budgetEvaluation.should_compact,
          reason: budgetEvaluation.reason,
          ...contextBudgetEventPayload(budgetEvaluation)
        } as AgentRuntimeEvent)
        if (budgetEvaluation.should_compact && compactionPlan.compacted.length > 0) {
          await this.append(input.runtimeRunID, {
            type: 'context.compaction.started',
            session_id: input.sessionID,
            event_id: '',
            seq: 0,
            timestamp: this.now(),
            message_id: userMessageID,
            reason: budgetEvaluation.reason,
            ...contextCompactionEventPayload(budgetEvaluation, compactionPlan)
          } as AgentRuntimeEvent)
          await this.append(input.runtimeRunID, {
            type: 'session.compaction.started',
            session_id: input.sessionID,
            event_id: '',
            seq: 0,
            timestamp: this.now(),
            message_id: userMessageID,
            reason: budgetEvaluation.reason,
            ...contextCompactionEventPayload(budgetEvaluation, compactionPlan)
          } as AgentRuntimeEvent)
        }
      }

      const harness = await this.createHarness(input)
      if (this.abortingRuns.has(input.runtimeRunID)) {
        await harness.abort?.()
        throw new Error('Pi harness stopped with aborted')
      }
      this.activeHarnesses.set(input.runtimeRunID, harness)
      if (this.approvalPauses.has(input.runtimeRunID)) {
        await harness.abort?.()
      }
      if (budgetEvaluation?.should_compact && compactionPlan && compactionPlan.compacted.length > 0) {
        const summary = buildHistoryCompactionSummary(
          compactionPlan.compacted,
          Math.min(compactionPlan.max_chars, 1200)
        )
        await this.append(input.runtimeRunID, {
          type: 'context.compaction.completed',
          session_id: input.sessionID,
          event_id: '',
          seq: 0,
          timestamp: this.now(),
          message_id: userMessageID,
          reason: budgetEvaluation.reason,
          summary,
          recent: '',
          ...contextCompactionEventPayload(budgetEvaluation, compactionPlan)
        } as AgentRuntimeEvent)
        await this.append(input.runtimeRunID, {
          type: 'session.compaction.ended',
          session_id: input.sessionID,
          event_id: '',
          seq: 0,
          timestamp: this.now(),
          message_id: userMessageID,
          reason: budgetEvaluation.reason,
          summary,
          recent: '',
          ...contextCompactionEventPayload(budgetEvaluation, compactionPlan)
        } as AgentRuntimeEvent)
      }
      const adapter = new PiEventAdapter({ sessionID: input.sessionID, assistantMessageID })
      unsubscribe = harness.subscribe(async (event) => {
        if (input.onSafeCheckpoint && isPiSavePointEvent(event)) {
          await input.onSafeCheckpoint({ runtime_run_id: input.runtimeRunID, checkpoint: 'turn_save_point' })
        }
        for (const runtimeEvent of adapter.accept(event as never)) {
          if (runtimeEvent.type === 'session.text.delta') {
            assistantText += runtimeEvent.delta
            textID = runtimeEvent.text_id
          }
          await this.append(input.runtimeRunID, runtimeEvent)
        }
      })

      const result = await this.invokeHarness(harness, input)
      const approvalPause = this.approvalPauses.get(input.runtimeRunID)
      if (approvalPause) {
        throw new PiToolApprovalRequiredError(
          approvalPause.approvalID,
          approvalPause.callID,
          approvalPause.toolName,
          approvalPause.reason
        )
      }
      const failureMessage = readAssistantFailureMessage(result)
      if (failureMessage) throw new Error(failureMessage)
      if (!assistantText) assistantText = readAssistantText(result)
      // Only emit session.text.ended when there is actual text content.
      // When the assistant produced only reasoning and/or tool calls (no visible
      // text), emitting an empty text part would create a misleading blank card.
      if (assistantText) {
        await this.append(input.runtimeRunID, {
          type: 'session.text.ended',
          session_id: input.sessionID,
          event_id: '',
          seq: 0,
          timestamp: this.now(),
          assistant_message_id: assistantMessageID,
          text_id: textID,
          text: assistantText
        })
      }
      await this.append(input.runtimeRunID, {
        type: 'session.step.ended',
        session_id: input.sessionID,
        event_id: '',
        seq: 0,
        timestamp: this.now(),
        assistant_message_id: assistantMessageID,
        finish_reason: 'stop',
        tokens: readTokenUsage(result),
        cost: readUsageCost(result),
        files: []
      })
      this.logger.info({ ...logContext, outcome: 'completed' })
      return { session_id: input.sessionID, user_message_id: userMessageID, assistant_message_id: assistantMessageID }
    } catch (error) {
      const approvalPause = this.approvalPauses.get(input.runtimeRunID)
      if (approvalPause) {
        this.logger.info({ ...logContext, outcome: 'awaiting_approval', code: 'tool_approval_required', category: 'permission' })
        throw new PiToolApprovalRequiredError(
          approvalPause.approvalID,
          approvalPause.callID,
          approvalPause.toolName,
          approvalPause.reason
        )
      }
      if (error instanceof PiToolApprovalRequiredError) {
        this.logger.info({ ...logContext, outcome: 'awaiting_approval', code: 'tool_approval_required', category: 'permission' })
        throw error
      }
      if (this.abortingRuns.has(input.runtimeRunID)) {
        this.logger.info({ ...logContext, outcome: 'cancelled', code: 'runtime_cancelled', category: 'cancelled' })
        throw error
      }
      const descriptor = classifyRuntimeError(error)
      const sourceError = error instanceof RuntimeSourceError
        ? error
        : new RuntimeSourceError({
            source: descriptor.source,
            code: descriptor.code,
            message: descriptor.message,
            http_status: descriptor.http_status,
            retryable: descriptor.retryable,
            cause: error
          })
      await this.append(input.runtimeRunID, {
        type: 'session.step.failed',
        session_id: input.sessionID,
        event_id: '',
        seq: 0,
        timestamp: this.now(),
        assistant_message_id: assistantMessageID,
        error: {
          type: descriptor.category,
          message: descriptor.message,
          code: descriptor.code,
          category: descriptor.category,
          retryable: descriptor.retryable,
          http_status: descriptor.http_status,
          source: descriptor.source,
          terminal_status: descriptor.terminal_status
        }
      })
      this.logger.error({
        ...logContext,
        outcome: descriptor.terminal_status,
        code: descriptor.code,
        category: descriptor.category,
        message: descriptor.message,
        error_message: descriptor.message
      })
      throw sourceError
    } finally {
      this.activeHarnesses.delete(input.runtimeRunID)
      this.acceptedSteerQueueItems.delete(input.runtimeRunID)
      this.pendingSteerQueueItems.delete(input.runtimeRunID)
      this.abortingRuns.delete(input.runtimeRunID)
      this.approvalPauses.delete(input.runtimeRunID)
      unsubscribe()
    }
  }

  private async invokeHarness(harness: PiHarnessLike, input: AgentHarnessRunInput) {
    const invocation = input.skillInvocation
    if (invocation?.name && typeof harness.skill === 'function') {
      await this.append(input.runtimeRunID, {
        type: 'skill.used',
        session_id: input.sessionID,
        event_id: '',
        seq: 0,
        timestamp: this.now(),
        name: invocation.name,
        operation: 'invoked',
        prompt: invocation.instructions ?? input.prompt
      } as unknown as AgentRuntimeEvent)
      return harness.skill(invocation.name, invocation.instructions ?? input.prompt)
    }
    return harness.prompt(input.prompt)
  }

  async abort(runtimeRunID: string): Promise<boolean> {
    this.approvalPauses.delete(runtimeRunID)
    this.abortingRuns.add(runtimeRunID)
    const harness = this.activeHarnesses.get(runtimeRunID)
    if (!harness?.abort) return true
    await harness.abort()
    return true
  }

  async steer(runtimeRunID: string, queueItemID: string, instruction: string): Promise<'accepted' | 'already_accepted' | 'run_not_active'> {
    const harness = this.activeHarnesses.get(runtimeRunID)
    if (!harness?.steer) return 'run_not_active'
    const acceptedQueueItems = this.acceptedSteerQueueItems.get(runtimeRunID) ?? new Set<string>()
    if (acceptedQueueItems.has(queueItemID)) return 'already_accepted'
    const pendingQueueItems = this.pendingSteerQueueItems.get(runtimeRunID) ?? new Map<string, Promise<void>>()
    const pending = pendingQueueItems.get(queueItemID)
    if (pending) {
      await pending
      return 'already_accepted'
    }
    const steering = Promise.resolve(harness.steer(instruction)).then(() => {})
    pendingQueueItems.set(queueItemID, steering)
    this.pendingSteerQueueItems.set(runtimeRunID, pendingQueueItems)
    try {
      await steering
    } finally {
      pendingQueueItems.delete(queueItemID)
      if (pendingQueueItems.size === 0) this.pendingSteerQueueItems.delete(runtimeRunID)
    }
    acceptedQueueItems.add(queueItemID)
    this.acceptedSteerQueueItems.set(runtimeRunID, acceptedQueueItems)
    return 'accepted'
  }

  async pauseForApproval(runtimeRunID: string, approval: AgentHarnessApprovalPause): Promise<boolean> {
    if (this.approvalPauses.has(runtimeRunID)) return true
    this.approvalPauses.set(runtimeRunID, approval)
    const harness = this.activeHarnesses.get(runtimeRunID)
    void Promise.resolve(harness?.abort?.()).catch(() => {})
    return true
  }

  private async append(runtimeRunID: string, event: AgentRuntimeEvent) {
    await this.store.append({ ...event, runtime_run_id: runtimeRunID })
  }
}

function isPiSavePointEvent(event: unknown) {
  if (!event || typeof event !== 'object' || Array.isArray(event)) return false
  const type = (event as Record<string, unknown>).type
  return type === 'save_point' || type === 'session.save_point' || type === 'turn_save_point'
}

function readAssistantFailureMessage(result: unknown): string {
  if (!result || typeof result !== 'object') return ''
  const record = result as Record<string, unknown>
  const stopReason = typeof record.stopReason === 'string' ? record.stopReason : ''
  const errorMessage = typeof record.errorMessage === 'string' ? record.errorMessage.trim() : ''
  if (errorMessage) return errorMessage
  return stopReason === 'error' || stopReason === 'aborted'
    ? `Pi harness stopped with ${stopReason}`
    : ''
}

function readAssistantText(result: unknown): string {
  if (!result || typeof result !== 'object') return ''
  const record = result as Record<string, unknown>
  if (typeof record.text === 'string') return record.text
  const content = record.content
  if (!Array.isArray(content)) return ''
  return content
    .map((item) => item && typeof item === 'object' ? item as Record<string, unknown> : {})
    .filter((item) => item.type === 'text' && typeof item.text === 'string')
    .map((item) => String(item.text))
    .join('')
}

function readTokenUsage(result: unknown) {
  const usage = asRecord(asRecord(result).usage)
  const cache = asRecord(usage.cache)
  const input = firstNumber(usage.input, usage.input_tokens, usage.prompt_tokens)
  const output = firstNumber(usage.output, usage.output_tokens, usage.completion_tokens)
  const reasoning = firstNumber(usage.reasoning, usage.reasoning_tokens, usage.reasoning_output_tokens)
  const total = firstNumber(usage.total, usage.totalTokens, usage.total_tokens)
  return {
    input: input || Math.max(0, total - output - reasoning),
    output,
    reasoning,
    cache: {
      read: firstNumber(cache.read, usage.cacheRead, usage.cache_read, usage.cache_read_tokens, usage.cached_tokens),
      write: firstNumber(cache.write, usage.cacheWrite, usage.cache_write, usage.cache_write_tokens)
    }
  }
}

function readUsageCost(result: unknown) {
  const usage = asRecord(asRecord(result).usage)
  const cost = asRecord(usage.cost)
  return firstNumber(cost.total, usage.cost_total, usage.total_cost, typeof usage.cost === 'number' ? usage.cost : 0)
}

function tryPrepareProviderBindingContextBudget(input: AgentHarnessRunInput): {
  evaluation: ContextBudgetEvaluation
  plan: HistoryCompactionPlan
} | undefined {
  const modelConfig = asRecord(input.modelConfig)
  const model = asRecord(input.model)
  const contextWindow = firstNumber(
    modelConfig.context_window,
    modelConfig.context_window_tokens,
    model.context_window_tokens,
    model.context_window
  )
  if (!contextWindow) return undefined
  const history = Array.isArray(input.history)
    ? input.history.map((message) => ({ role: message.role, content: message.content }))
    : []
  try {
    const evaluation = evaluateContextBudget({
      binding: {
        provider_id: firstString(modelConfig.provider_id, model.provider_id),
        provider_model_key: firstString(modelConfig.id, model.id, model.provider_model_key),
        context_window_tokens: contextWindow,
        max_output_tokens: firstNumber(modelConfig.max_tokens, model.max_tokens, model.max_output_tokens),
        capability_source: firstString(asRecord(modelConfig.inference).capability_source, model.capability_source, 'binding_snapshot'),
        capability_snapshot_hash: firstString(asRecord(modelConfig.inference).capability_snapshot_hash, model.capability_snapshot_hash)
      },
      history,
      system_prompt: input.systemPrompt,
      prompt: input.prompt
    })
    return { evaluation, plan: planHistoryCompaction(history, evaluation) }
  } catch (error) {
    if (error instanceof MissingBindingContextWindowError) return undefined
    throw error
  }
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const text = value === undefined || value === null ? '' : String(value).trim()
    if (text) return text
  }
  return ''
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function firstNumber(...values: unknown[]) {
  for (const value of values) {
    const number = Number(value)
    if (Number.isFinite(number) && number > 0) return number
  }
  return 0
}
