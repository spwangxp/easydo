import { describe, expect, it } from 'vitest'
import { AgentRuntimeProjection } from './projection.js'
import { PiEventAdapter } from './piAdapter.js'
import type { AgentRuntimeEvent } from './events.js'

describe('agent runtime event projection', () => {
  it('projects OpenCode-style text, reasoning, tool, permission, and compaction events into frontend messages and parts', () => {
    const projection = new AgentRuntimeProjection()
    const events: AgentRuntimeEvent[] = [
      {
        type: 'session.prompted',
        session_id: 'sess-1',
        event_id: 'evt-1',
        seq: 1,
        timestamp: '2026-06-29T00:00:00.000Z',
        message_id: 'user-1',
        prompt: '帮我检查部署状态',
        files: [],
        delivery: 'prompt'
      },
      {
        type: 'session.step.started',
        session_id: 'sess-1',
        event_id: 'evt-2',
        seq: 2,
        timestamp: '2026-06-29T00:00:01.000Z',
        assistant_message_id: 'assistant-1',
        parent_message_id: 'user-1',
        agent: 'ops-agent',
        model: { provider_id: 'openai', id: 'gpt-4.1' }
      },
      {
        type: 'session.reasoning.ended',
        session_id: 'sess-1',
        event_id: 'evt-3',
        seq: 3,
        timestamp: '2026-06-29T00:00:02.000Z',
        assistant_message_id: 'assistant-1',
        reasoning_id: 'reasoning-1',
        text: '需要先读取部署列表，再查看最近一次状态。'
      },
      {
        type: 'session.text.ended',
        session_id: 'sess-1',
        event_id: 'evt-4',
        seq: 4,
        timestamp: '2026-06-29T00:00:03.000Z',
        assistant_message_id: 'assistant-1',
        text_id: 'text-1',
        text: '我先查看部署列表。'
      },
      {
        type: 'session.tool.called',
        session_id: 'sess-1',
        event_id: 'evt-5',
        seq: 5,
        timestamp: '2026-06-29T00:00:04.000Z',
        assistant_message_id: 'assistant-1',
        call_id: 'call-1',
        tool: 'deployment.list',
        input: { workspace_id: 11 }
      },
      {
        type: 'permission.asked',
        session_id: 'sess-1',
        event_id: 'evt-6',
        seq: 6,
        timestamp: '2026-06-29T00:00:05.000Z',
        request_id: 'approval-1',
        call_id: 'call-1',
        tool_name: 'deployment.list',
        input: { workspace_id: 11 },
        reason: '读取部署状态需要用户授权',
        message: '是否允许读取部署状态？'
      },
      {
        type: 'permission.resolved',
        session_id: 'sess-1',
        event_id: 'evt-7',
        seq: 7,
        timestamp: '2026-06-29T00:00:06.000Z',
        request_id: 'approval-1',
        result: 'approved'
      },
      {
        type: 'session.tool.success',
        session_id: 'sess-1',
        event_id: 'evt-8',
        seq: 8,
        timestamp: '2026-06-29T00:00:07.000Z',
        assistant_message_id: 'assistant-1',
        call_id: 'call-1',
        content: [{ type: 'text', text: '找到 2 个部署' }],
        structured: { count: 2 },
        output_paths: []
      },
      {
        type: 'session.compaction.ended',
        session_id: 'sess-1',
        event_id: 'evt-9',
        seq: 9,
        timestamp: '2026-06-29T00:00:08.000Z',
        message_id: 'user-1',
        reason: 'auto',
        summary: '用户要求检查部署状态。',
        recent: '保留最近部署检查上下文。'
      },
      {
        type: 'session.step.ended',
        session_id: 'sess-1',
        event_id: 'evt-10',
        seq: 10,
        timestamp: '2026-06-29T00:00:09.000Z',
        assistant_message_id: 'assistant-1',
        finish_reason: 'stop',
        tokens: { input: 10, output: 12, reasoning: 3, cache: { read: 0, write: 0 } },
        cost: 0.01,
        files: []
      }
    ]

    for (const event of events) projection.apply(event)
    const view = projection.getSession('sess-1')

    expect(view.messages).toHaveLength(2)
    expect(view.messages[0]).toMatchObject({ id: 'user-1', role: 'user', text: '帮我检查部署状态' })
    expect(view.messages[1]).toMatchObject({ id: 'assistant-1', role: 'assistant', parent_id: 'user-1', agent: 'ops-agent' })
    expect(view.partsByMessage['assistant-1']).toMatchObject([
      { type: 'reasoning', text: '需要先读取部署列表，再查看最近一次状态。' },
      { type: 'text', text: '我先查看部署列表。' },
      { type: 'tool', call_id: 'call-1', tool: 'deployment.list', state: 'completed', structured: { count: 2 } }
    ])
    expect(view.partsByMessage['user-1']).toMatchObject([
      { type: 'compaction', reason: 'auto', summary: '用户要求检查部署状态。' }
    ])
    expect(view.approvals).toEqual([
      expect.objectContaining({ id: 'approval-1', status: 'approved', call_id: 'call-1' })
    ])
    expect(view.partsByMessage['assistant-1'][2]).toMatchObject({
      type: 'tool',
      call_id: 'call-1',
      approval: { id: 'approval-1', status: 'approved', reason: '读取部署状态需要用户授权' }
    })
    expect(view.status.type).toBe('idle')
  })

  it('projects streamed Pi tool input into the final tool part for replay', () => {
    const projection = new AgentRuntimeProjection()
    const events: AgentRuntimeEvent[] = [
      {
        type: 'session.step.started',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-1',
        seq: 1,
        timestamp: '2026-06-29T00:00:00.000Z',
        assistant_message_id: 'assistant-tool',
        agent: 'ops-agent'
      },
      {
        type: 'session.tool.input.started',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-2',
        seq: 2,
        timestamp: '2026-06-29T00:00:01.000Z',
        assistant_message_id: 'assistant-tool',
        call_id: 'call-streamed-input',
        tool_name: 'easydo_resource_list'
      },
      {
        type: 'session.tool.input.delta',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-3',
        seq: 3,
        timestamp: '2026-06-29T00:00:02.000Z',
        assistant_message_id: 'assistant-tool',
        call_id: 'call-streamed-input',
        tool_name: 'easydo_resource_list',
        delta: '{"kind":"gpu"'
      },
      {
        type: 'session.tool.input.delta',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-4',
        seq: 4,
        timestamp: '2026-06-29T00:00:03.000Z',
        assistant_message_id: 'assistant-tool',
        call_id: 'call-streamed-input',
        tool_name: 'easydo_resource_list',
        delta: ',"limit":2}'
      },
      {
        type: 'session.tool.input.ended',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-5',
        seq: 5,
        timestamp: '2026-06-29T00:00:04.000Z',
        assistant_message_id: 'assistant-tool',
        call_id: 'call-streamed-input',
        text: '{"kind":"gpu","limit":2}'
      },
      {
        type: 'session.tool.called',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-6',
        seq: 6,
        timestamp: '2026-06-29T00:00:05.000Z',
        assistant_message_id: 'assistant-tool',
        call_id: 'call-streamed-input',
        tool: 'easydo_resource_list',
        input: { kind: 'gpu', limit: 2 }
      },
      {
        type: 'session.tool.success',
        session_id: 'sess-tool-input',
        event_id: 'evt-tool-7',
        seq: 7,
        timestamp: '2026-06-29T00:00:06.000Z',
        assistant_message_id: 'assistant-tool',
        call_id: 'call-streamed-input',
        content: [{ type: 'text', text: 'found 2 gpu resources' }],
        structured: { count: 2 },
        output_paths: []
      }
    ]

    for (const event of events) projection.apply(event)
    const view = projection.getSession('sess-tool-input')

    expect(view.partsByMessage['assistant-tool']).toMatchObject([
      {
        type: 'tool',
        call_id: 'call-streamed-input',
        tool: 'easydo_resource_list',
        state: 'completed',
        input: { kind: 'gpu', limit: 2 },
        input_text: '{"kind":"gpu","limit":2}',
        structured: { count: 2 }
      }
    ])
  })

  it('keeps approval status on tool parts when permission is asked before the tool call is recorded', () => {
    const projection = new AgentRuntimeProjection()
    const events: AgentRuntimeEvent[] = [
      {
        type: 'session.step.started',
        session_id: 'sess-approval-before-tool',
        event_id: 'evt-approval-1',
        seq: 1,
        timestamp: '2026-06-29T00:00:00.000Z',
        assistant_message_id: 'assistant-approval',
        agent: 'ops-agent'
      },
      {
        type: 'permission.asked',
        session_id: 'sess-approval-before-tool',
        event_id: 'evt-approval-2',
        seq: 2,
        timestamp: '2026-06-29T00:00:01.000Z',
        request_id: 'approval-before-tool',
        call_id: 'call-approval-late-tool',
        tool_name: 'easydo_write',
        input: { value: 'x' },
        reason: 'Tool easydo_write requires approval',
        message: 'Tool easydo_write requires approval'
      },
      {
        type: 'permission.resolved',
        session_id: 'sess-approval-before-tool',
        event_id: 'evt-approval-3',
        seq: 3,
        timestamp: '2026-06-29T00:00:02.000Z',
        request_id: 'approval-before-tool',
        result: 'approved'
      },
      {
        type: 'session.tool.called',
        session_id: 'sess-approval-before-tool',
        event_id: 'evt-approval-4',
        seq: 4,
        timestamp: '2026-06-29T00:00:03.000Z',
        assistant_message_id: 'assistant-approval',
        call_id: 'call-approval-late-tool',
        tool: 'easydo_write',
        input: { value: 'x' }
      }
    ]

    for (const event of events) projection.apply(event)
    const view = projection.getSession('sess-approval-before-tool')

    expect(view.partsByMessage['assistant-approval']).toMatchObject([
      {
        type: 'tool',
        call_id: 'call-approval-late-tool',
        tool: 'easydo_write',
        approval: {
          id: 'approval-before-tool',
          status: 'approved',
          reason: 'Tool easydo_write requires approval'
        }
      }
    ])
  })

  it('projects text and reasoning deltas before ended events for interrupted replay', () => {
    const projection = new AgentRuntimeProjection()
    const events: AgentRuntimeEvent[] = [
      {
        type: 'session.step.started',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-1',
        seq: 1,
        timestamp: '2026-06-29T00:00:00.000Z',
        assistant_message_id: 'assistant-delta',
        agent: 'ops-agent'
      },
      {
        type: 'session.reasoning.started',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-2',
        seq: 2,
        timestamp: '2026-06-29T00:00:01.000Z',
        assistant_message_id: 'assistant-delta',
        reasoning_id: 'reasoning-delta'
      },
      {
        type: 'session.reasoning.delta',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-3',
        seq: 3,
        timestamp: '2026-06-29T00:00:02.000Z',
        assistant_message_id: 'assistant-delta',
        reasoning_id: 'reasoning-delta',
        delta: '先检查'
      },
      {
        type: 'session.reasoning.delta',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-4',
        seq: 4,
        timestamp: '2026-06-29T00:00:03.000Z',
        assistant_message_id: 'assistant-delta',
        reasoning_id: 'reasoning-delta',
        delta: '上下文。'
      },
      {
        type: 'session.text.started',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-5',
        seq: 5,
        timestamp: '2026-06-29T00:00:04.000Z',
        assistant_message_id: 'assistant-delta',
        text_id: 'text-delta'
      },
      {
        type: 'session.text.delta',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-6',
        seq: 6,
        timestamp: '2026-06-29T00:00:05.000Z',
        assistant_message_id: 'assistant-delta',
        text_id: 'text-delta',
        delta: '正在检查'
      },
      {
        type: 'session.text.delta',
        session_id: 'sess-delta-only',
        event_id: 'evt-delta-7',
        seq: 7,
        timestamp: '2026-06-29T00:00:06.000Z',
        assistant_message_id: 'assistant-delta',
        text_id: 'text-delta',
        delta: '部署状态。'
      }
    ]

    for (const event of events) projection.apply(event)
    const view = projection.getSession('sess-delta-only')

    expect(view.partsByMessage['assistant-delta']).toMatchObject([
      { type: 'reasoning', text: '先检查上下文。' },
      { type: 'text', text: '正在检查部署状态。' }
    ])
  })
})

describe('Pi event adapter', () => {
  it('expands Pi coarse lifecycle events into OpenCode-style runtime events', () => {
    const adapter = new PiEventAdapter({ sessionID: 'sess-1', assistantMessageID: 'assistant-1' })

    const events = [
      ...adapter.accept({ type: 'message_update', message: { role: 'assistant', content: [{ type: 'text', text: 'hello' }] } }),
      ...adapter.accept({ type: 'tool_execution_start', toolCallId: 'call-1', toolName: 'read_file', args: { path: 'README.md' } }),
      ...adapter.accept({ type: 'tool_execution_update', toolCallId: 'call-1', toolName: 'read_file', args: { path: 'README.md' }, partialResult: { content: [{ type: 'text', text: 'reading' }], details: { bytes: 128 } } }),
      ...adapter.accept({ type: 'tool_execution_end', toolCallId: 'call-1', toolName: 'read_file', result: { content: [{ type: 'text', text: 'done' }], details: { bytes: 256 } }, isError: false })
    ]

    expect(events.map((event) => event.type)).toEqual([
      'session.text.delta',
      'session.tool.called',
      'session.tool.progress',
      'session.tool.success'
    ])
    expect(events[0]).toMatchObject({ assistant_message_id: 'assistant-1', delta: 'hello' })
    expect(events[1]).toMatchObject({ call_id: 'call-1', tool: 'read_file', input: { path: 'README.md' } })
    expect(events[3]).toMatchObject({ call_id: 'call-1', content: [{ type: 'text', text: 'done' }], structured: { bytes: 256 } })
  })
})
