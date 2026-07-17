import { describe, expect, it } from 'vitest'
import { PiEventAdapter } from './piAdapter.js'

function makeAdapter() {
  let id = 0
  return new PiEventAdapter({
    sessionID: 'sess-1',
    assistantMessageID: 'msg-1',
    now: () => '2026-01-01T00:00:00Z',
    nextEventID: () => `evt_${++id}`,
    initialSeq: 0
  })
}

function partial(content: unknown[]) {
  return { role: 'assistant', content }
}

describe('PiEventAdapter', () => {
  describe('thinking events -> reasoning', () => {
    it('bridges thinking_start/delta/end into session.reasoning.started/delta/ended', () => {
      const adapter = makeAdapter()
      const start = adapter.accept({
        type: 'message_update',
        message: partial([{ type: 'thinking', thinking: '' }]),
        assistantMessageEvent: { type: 'thinking_start', contentIndex: 0, partial: partial([{ type: 'thinking', thinking: '' }]) }
      })
      expect(start).toHaveLength(1)
      expect(start[0]).toMatchObject({ type: 'session.reasoning.started', reasoning_id: 'msg-1:reasoning:0' })

      const delta = adapter.accept({
        type: 'message_update',
        message: partial([{ type: 'thinking', thinking: 'Let me think' }]),
        assistantMessageEvent: { type: 'thinking_delta', contentIndex: 0, delta: 'Let me think', partial: partial([{ type: 'thinking', thinking: 'Let me think' }]) }
      })
      expect(delta).toHaveLength(1)
      expect(delta[0]).toMatchObject({ type: 'session.reasoning.delta', delta: 'Let me think' })

      const end = adapter.accept({
        type: 'message_update',
        message: partial([{ type: 'thinking', thinking: 'Let me think about this' }]),
        assistantMessageEvent: { type: 'thinking_end', contentIndex: 0, content: 'Let me think about this', partial: partial([{ type: 'thinking', thinking: 'Let me think about this' }]) }
      })
      expect(end).toHaveLength(1)
      expect(end[0]).toMatchObject({ type: 'session.reasoning.ended', text: 'Let me think about this' })
    })

    it('skips empty thinking_delta', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'message_update',
        message: partial([]),
        assistantMessageEvent: { type: 'thinking_delta', contentIndex: 0, delta: '', partial: partial([]) }
      })
      expect(result).toEqual([])
    })
  })

  describe('text events -> text', () => {
    it('bridges text_start/delta into session.text.started/delta', () => {
      const adapter = makeAdapter()
      const start = adapter.accept({
        type: 'message_update',
        message: partial([{ type: 'text', text: '' }]),
        assistantMessageEvent: { type: 'text_start', contentIndex: 0, partial: partial([{ type: 'text', text: '' }]) }
      })
      expect(start).toHaveLength(1)
      expect(start[0]).toMatchObject({ type: 'session.text.started' })

      const delta = adapter.accept({
        type: 'message_update',
        message: partial([{ type: 'text', text: 'Hello' }]),
        assistantMessageEvent: { type: 'text_delta', contentIndex: 0, delta: 'Hello', partial: partial([{ type: 'text', text: 'Hello' }]) }
      })
      expect(delta).toHaveLength(1)
      expect(delta[0]).toMatchObject({ type: 'session.text.delta', delta: 'Hello' })
    })

    it('does not emit on text_end (harnessRunner owns session.text.ended)', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'message_update',
        message: partial([{ type: 'text', text: 'Hello world' }]),
        assistantMessageEvent: { type: 'text_end', contentIndex: 0, content: 'Hello world', partial: partial([{ type: 'text', text: 'Hello world' }]) }
      })
      expect(result).toEqual([])
    })
  })

  describe('toolcall events -> tool input', () => {
    it('bridges toolcall_start/delta/end into session.tool.input.started/delta/ended', () => {
      const adapter = makeAdapter()
      const toolCallPartial = partial([{ type: 'toolCall', id: 'call-abc', name: 'read_file', arguments: {} }])

      const start = adapter.accept({
        type: 'message_update',
        message: toolCallPartial,
        assistantMessageEvent: { type: 'toolcall_start', contentIndex: 0, partial: toolCallPartial }
      })
      expect(start).toHaveLength(1)
      expect(start[0]).toMatchObject({ type: 'session.tool.input.started', call_id: 'call-abc', tool_name: 'read_file' })

      const delta = adapter.accept({
        type: 'message_update',
        message: toolCallPartial,
        assistantMessageEvent: { type: 'toolcall_delta', contentIndex: 0, delta: '{"path":"foo', partial: toolCallPartial }
      })
      expect(delta).toHaveLength(1)
      expect(delta[0]).toMatchObject({ type: 'session.tool.input.delta', call_id: 'call-abc', tool_name: 'read_file', delta: '{"path":"foo' })

      const end = adapter.accept({
        type: 'message_update',
        message: toolCallPartial,
        assistantMessageEvent: { type: 'toolcall_end', contentIndex: 0, toolCall: { type: 'toolCall', id: 'call-abc', name: 'read_file', arguments: { path: 'foo.txt' } }, partial: toolCallPartial }
      })
      expect(end).toHaveLength(1)
      expect(end[0]).toMatchObject({ type: 'session.tool.input.ended', call_id: 'call-abc' })
      expect(typeof (end[0] as { text: string }).text).toBe('string')
    })

    it('uses provisional call_id when partial lacks toolCall info at start', () => {
      const adapter = makeAdapter()
      const start = adapter.accept({
        type: 'message_update',
        message: partial([]),
        assistantMessageEvent: { type: 'toolcall_start', contentIndex: 2, partial: partial([]) }
      })
      expect(start).toHaveLength(1)
      expect(start[0]).toMatchObject({ call_id: 'call:2', tool_name: 'unknown' })
    })
  })

  describe('fallback (no assistantMessageEvent)', () => {
    it('extracts text from message content as session.text.delta', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'message_update',
        message: { role: 'assistant', content: [{ type: 'text', text: 'fallback answer' }] }
      })
      expect(result).toHaveLength(1)
      expect(result[0]).toMatchObject({ type: 'session.text.delta', delta: 'fallback answer' })
    })

    it('extracts assistant text from final message_end when provider did not stream deltas', () => {
      const adapter = makeAdapter()
      expect(adapter.accept({
        type: 'message_end',
        message: { role: 'user', content: 'hello' }
      })).toEqual([])

      const result = adapter.accept({
        type: 'message_end',
        message: { role: 'assistant', content: [{ type: 'text', text: 'final answer' }] }
      })

      expect(result).toHaveLength(1)
      expect(result[0]).toMatchObject({ type: 'session.text.delta', delta: 'final answer' })
    })
  })

  describe('tool execution events', () => {
    it('bridges tool_execution_start into session.tool.called', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'tool_execution_start',
        toolCallId: 'call-1',
        toolName: 'lookup',
        args: { query: 'test' }
      })
      expect(result).toHaveLength(1)
      expect(result[0]).toMatchObject({ type: 'session.tool.called', tool: 'lookup', call_id: 'call-1' })
    })

    it('bridges tool_execution_end (success) into session.tool.success', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'tool_execution_end',
        toolCallId: 'call-1',
        toolName: 'lookup',
        result: { content: [{ type: 'text', text: 'found' }], details: { ok: true } },
        isError: false
      })
      expect(result).toHaveLength(1)
      expect(result[0]).toMatchObject({ type: 'session.tool.success', call_id: 'call-1' })
    })

    it('preserves Pi tool output paths on successful tool execution', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'tool_execution_end',
        toolCallId: 'call-write',
        toolName: 'write_file',
        result: {
          content: [{ type: 'text', text: 'wrote files' }],
          details: { ok: true },
          outputPaths: ['src/app.ts'],
          output_paths: ['src/app.test.ts']
        },
        isError: false
      })

      expect(result).toHaveLength(1)
      expect(result[0]).toMatchObject({
        type: 'session.tool.success',
        call_id: 'call-write',
        output_paths: ['src/app.ts', 'src/app.test.ts']
      })
    })

    it('bridges tool_execution_end (error) into session.tool.failed', () => {
      const adapter = makeAdapter()
      const result = adapter.accept({
        type: 'tool_execution_end',
        toolCallId: 'call-1',
        toolName: 'lookup',
        result: { content: [{ type: 'text', text: 'boom' }] },
        isError: true
      })
      expect(result).toHaveLength(1)
      expect(result[0]).toMatchObject({ type: 'session.tool.failed', call_id: 'call-1' })
    })
  })

  describe('compaction events', () => {
    it('bridges Pi session_before_compact/session_compact into session.compaction.started/ended', () => {
      const adapter = makeAdapter()

      const started = adapter.accept({
        type: 'session_before_compact',
        reason: 'auto',
        messageId: 'msg-compact-1'
      })
      expect(started).toHaveLength(1)
      expect(started[0]).toMatchObject({
        type: 'session.compaction.started',
        message_id: 'msg-compact-1',
        reason: 'auto'
      })

      const ended = adapter.accept({
        type: 'session_compact',
        compactionEntry: {
          id: 'msg-compact-1',
          summary: 'Earlier deployment context was summarized.',
          recent: 'Latest user asked for status.'
        }
      })
      expect(ended).toHaveLength(1)
      expect(ended[0]).toMatchObject({
        type: 'session.compaction.ended',
        message_id: 'msg-compact-1',
        reason: 'auto',
        summary: 'Earlier deployment context was summarized.',
        recent: 'Latest user asked for status.'
      })
    })
  })

  describe('seq ordering', () => {
    it('increments seq across multiple events', () => {
      const adapter = makeAdapter()
      const e1 = adapter.accept({ type: 'tool_execution_start', toolCallId: 'a', toolName: 't', args: {} })
      const e2 = adapter.accept({ type: 'tool_execution_start', toolCallId: 'b', toolName: 't', args: {} })
      expect((e1[0] as { seq: number }).seq).toBe(1)
      expect((e2[0] as { seq: number }).seq).toBe(2)
    })
  })
})
