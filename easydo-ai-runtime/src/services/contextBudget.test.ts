import { describe, expect, it } from 'vitest'
import {
  DEFAULT_PROMPT_COUNT_COMPACTION_THRESHOLD,
  MissingBindingContextWindowError,
  buildCompactionPersistenceRecord,
  buildHistoryCompactionSummary,
  canCompactHistory,
  contextBudgetEventPayload,
  contextCompactionEventPayload,
  countUserPrompts,
  evaluateContextBudget,
  planHistoryCompaction,
  resolveBindingContextWindowTokens,
  tryResolveBindingContextWindowTokens
} from './contextBudget.js'

function bulkyHistory(messageCount: number, charsPerMessage: number) {
  return Array.from({ length: messageCount }, (_, index) => ({
    role: index % 2 === 0 ? 'user' : 'assistant',
    content: `turn-${index}-${'x'.repeat(charsPerMessage)}`
  }))
}

describe('contextBudget', () => {
  it('requires provider-specific binding context_window_tokens for strict resolver only', () => {
    expect(() => resolveBindingContextWindowTokens({
      model: { provider_model_key: 'gpt-4.1', context_window: undefined },
      provider: { provider_id: 'openai' }
    })).toThrow(MissingBindingContextWindowError)
    expect(tryResolveBindingContextWindowTokens({
      model: { provider_model_key: 'gpt-4.1' },
      provider: { provider_id: 'openai' }
    })).toBeUndefined()
  })

  it('uses distinct binding windows for the same model name under different providers', () => {
    const history = bulkyHistory(40, 2_000)
    const openrouter = evaluateContextBudget({
      binding: {
        provider_id: 'openrouter',
        provider_model_key: 'qwen/qwen3',
        context_window_tokens: 131072,
        capability_source: 'provider_api',
        capability_snapshot_hash: 'sha256:openrouter'
      },
      history,
      system_prompt: 'system',
      prompt: 'continue',
      prompt_count_threshold: 100
    })
    const ollama = evaluateContextBudget({
      binding: {
        provider_id: 'ollama',
        provider_model_key: 'qwen/qwen3',
        context_window_tokens: 8_192,
        capability_source: 'manual_override',
        capability_snapshot_hash: 'sha256:ollama'
      },
      history,
      system_prompt: 'system',
      prompt: 'continue',
      prompt_count_threshold: 100
    })

    expect(openrouter.context_window_tokens).toBe(131072)
    expect(ollama.context_window_tokens).toBe(8192)
    expect(openrouter.threshold_tokens).toBeGreaterThan(ollama.threshold_tokens)
    expect(openrouter.current_context_tokens).toBe(ollama.current_context_tokens)
    expect(ollama.should_compact).toBe(true)
    expect(ollama.reason).toBe('context-window')
    expect(openrouter.should_compact).toBe(false)
    expect(openrouter.reason).toBe('within_budget')
    expect(openrouter.model_key).toBe('qwen/qwen3')
    expect(ollama.model_key).toBe('qwen/qwen3')
    expect(openrouter.provider_id).toBe('openrouter')
    expect(ollama.provider_id).toBe('ollama')
    expect(contextBudgetEventPayload(ollama)).toMatchObject({
      provider_id: 'ollama',
      model_key: 'qwen/qwen3',
      context_window_tokens: 8192,
      should_compact: true,
      reason: 'context-window'
    })
  })

  it('plans retained recent history against the binding usable budget', () => {
    const history = [
      { role: 'user', content: 'old-1 '.repeat(4_000) },
      { role: 'assistant', content: 'old-2 '.repeat(4_000) },
      { role: 'user', content: 'old-3 '.repeat(4_000) },
      { role: 'user', content: 'recent-1' },
      { role: 'assistant', content: 'recent-2' }
    ]
    const evaluation = evaluateContextBudget({
      binding: {
        provider_id: 'openrouter',
        provider_model_key: 'qwen/qwen3',
        context_window_tokens: 4_096,
        max_output_tokens: 512,
        capability_source: 'provider_api'
      },
      history,
      system_prompt: 'system',
      prompt: 'next',
      prompt_count_threshold: 100
    })
    expect(evaluation.should_compact).toBe(true)
    expect(evaluation.reason).toBe('context-window')

    const plan = planHistoryCompaction(history, evaluation, {
      max_messages: 3,
      max_chars: 400
    })

    expect(plan.context_window_tokens).toBe(4096)
    expect(plan.reason).toBe('context-window')
    expect(plan.injected.map((item) => item.content)).toContain('recent-2')
    expect(plan.compacted.length).toBeGreaterThan(0)
    const summary = buildHistoryCompactionSummary(plan.compacted, plan.max_chars, plan.reason)
    expect(summary).toContain('reason=context-window')
    expect(summary).toContain('provider-specific context budget')
    expect(contextCompactionEventPayload(evaluation, plan)).toMatchObject({
      before_tokens: evaluation.current_context_tokens,
      compacted_message_count: plan.compacted.length,
      retained_message_count: plan.injected.length,
      context_window_tokens: 4096,
      provider_id: 'openrouter',
      model_key: 'qwen/qwen3',
      reason: 'context-window'
    })
    expect(contextCompactionEventPayload(evaluation, plan).after_tokens)
      .toBeLessThan(contextCompactionEventPayload(evaluation, plan).before_tokens as number)
  })

  it('does not fail when binding context window is absent; uses prompt-count at 20 user prompts', () => {
    const history = Array.from({ length: 38 }, (_, index) => ({
      role: index % 2 === 0 ? 'user' : 'assistant',
      content: `turn-${index}`
    }))
    // 19 history user turns + current prompt = 20
    expect(countUserPrompts(history, 'next')).toBe(20)

    const evaluation = evaluateContextBudget({
      binding: { provider_id: 'openrouter', provider_model_key: 'qwen/qwen3' },
      history,
      prompt: 'next'
    })
    expect(evaluation.has_context_window).toBe(false)
    expect(evaluation.context_window_tokens).toBe(0)
    expect(evaluation.prompt_count_threshold).toBe(DEFAULT_PROMPT_COUNT_COMPACTION_THRESHOLD)
    expect(evaluation.user_prompt_count).toBe(20)
    expect(evaluation.should_compact).toBe(true)
    expect(evaluation.reason).toBe('prompt-count')
    expect(contextBudgetEventPayload(evaluation)).toMatchObject({
      reason: 'prompt-count',
      reason_label: expect.stringContaining('prompt-count')
    })

    const under = evaluateContextBudget({
      binding: { provider_id: 'openrouter', provider_model_key: 'qwen/qwen3' },
      history: history.slice(0, 36),
      prompt: 'next'
    })
    expect(under.user_prompt_count).toBe(19)
    expect(under.should_compact).toBe(false)
    expect(under.reason).toBe('within_budget')
  })

  it('emits retained and dropped ranges for structured compaction persistence', () => {
    const history = [
      { role: 'user', content: 'drop-me-1' },
      { role: 'assistant', content: 'drop-me-2' },
      { role: 'user', content: 'keep-me-1' },
      { role: 'assistant', content: 'keep-me-2' }
    ]
    const evaluation = evaluateContextBudget({
      binding: {
        provider_id: 'openrouter',
        provider_model_key: 'qwen/qwen3',
        context_window_tokens: 1_024,
        max_output_tokens: 128
      },
      history,
      system_prompt: 'system',
      prompt: 'next',
      prompt_count_threshold: 2
    })
    const plan = planHistoryCompaction(history, evaluation, {
      max_messages: 2,
      max_chars: 200
    })
    expect(plan.dropped_range).toMatchObject({
      start_index: 0,
      end_index: 2,
      message_count: 2
    })
    expect(plan.retained_range).toMatchObject({
      start_index: 2,
      end_index: 4,
      message_count: 2
    })
    const persisted = buildCompactionPersistenceRecord(evaluation, plan)
    expect(persisted).toMatchObject({
      version: 1,
      algorithm: 'history_tail_retain_v1',
      failure_policy: 'retain_original_context',
      retained_range: plan.retained_range,
      dropped_range: plan.dropped_range,
      original_message_count: 4
    })
  })

  it('blocks compaction while approval is pending and keeps original context', () => {
    expect(canCompactHistory({ has_pending_approval: true })).toEqual({
      allowed: false,
      reason: 'pending_approval'
    })
    const history = [
      { role: 'user', content: 'a'.repeat(4_000) },
      { role: 'assistant', content: 'b'.repeat(4_000) },
      { role: 'user', content: 'recent' }
    ]
    const evaluation = evaluateContextBudget({
      binding: {
        provider_id: 'ollama',
        provider_model_key: 'qwen/qwen3',
        context_window_tokens: 2_048,
        max_output_tokens: 256
      },
      history,
      system_prompt: 'system',
      prompt: 'next',
      prompt_count_threshold: 1
    })
    expect(evaluation.should_compact).toBe(true)
    const plan = planHistoryCompaction(history, evaluation, {
      max_messages: 1,
      max_chars: 100,
      guard: { has_pending_approval: true }
    })
    expect(plan.compacted).toEqual([])
    expect(plan.injected).toEqual(history)
    expect(plan.dropped_range).toBeNull()
    expect(plan.retained_range.message_count).toBe(history.length)
  })
})
