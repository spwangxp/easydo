import type {
  AgentRuntimeEvent,
  ContentPart,
  ReasoningStartedEvent,
  ReasoningDeltaEvent,
  ReasoningEndedEvent,
  TextStartedEvent,
  TextDeltaEvent,
  ToolInputStartedEvent,
  ToolInputDeltaEvent,
  ToolInputEndedEvent
} from './events.js'

type PiAgentEvent =
  | { type: 'message_update'; message: unknown; assistantMessageEvent?: unknown }
  | { type: 'message_end'; message: unknown }
  | { type: 'tool_execution_start'; toolCallId: string; toolName: string; args: unknown }
  | { type: 'tool_execution_update'; toolCallId: string; toolName: string; args: unknown; partialResult: unknown }
  | { type: 'tool_execution_end'; toolCallId: string; toolName: string; result: unknown; isError: boolean }
  | { type: string; [key: string]: unknown }

interface PiEventAdapterOptions {
  sessionID: string
  assistantMessageID: string
  now?: () => string
  nextEventID?: () => string
  initialSeq?: number
}

export class PiEventAdapter {
  private seq: number
  private readonly now: () => string
  private readonly nextEventID: () => string
  private readonly sessionID: string
  private readonly assistantMessageID: string
  private emittedText = false

  constructor(options: PiEventAdapterOptions) {
    this.sessionID = options.sessionID
    this.assistantMessageID = options.assistantMessageID
    this.seq = options.initialSeq ?? 0
    this.now = options.now ?? (() => new Date().toISOString())
    let counter = 0
    this.nextEventID = options.nextEventID ?? (() => `evt_${Date.now()}_${++counter}`)
  }

  accept(event: PiAgentEvent): AgentRuntimeEvent[] {
    switch (event.type) {
      case 'message_update': {
        const ame = event.assistantMessageEvent
        if (ame && typeof ame === 'object' && 'type' in ame) {
          return this.handleAssistantMessageEvent(ame as Record<string, unknown>)
        }
        // Fallback: no granular event (e.g. fake harness), extract text from message
        const delta = readAssistantText(event.message)
        if (!delta) return []
        this.emittedText = true
        return [{
          ...this.base('session.text.delta'),
          assistant_message_id: this.assistantMessageID,
          text_id: `${this.assistantMessageID}:text`,
          delta
        }]
      }
      case 'message_end': {
        if (this.emittedText) return []
        const delta = readAssistantText(event.message)
        if (!delta) return []
        this.emittedText = true
        return [{
          ...this.base('session.text.delta'),
          assistant_message_id: this.assistantMessageID,
          text_id: `${this.assistantMessageID}:text`,
          delta
        }]
      }
      case 'tool_execution_start':
        if (!isToolStartEvent(event)) return []
        return [{
          ...this.base('session.tool.called'),
          assistant_message_id: this.assistantMessageID,
          call_id: event.toolCallId,
          tool: event.toolName,
          input: asRecord(event.args)
        }]
      case 'tool_execution_update': {
        if (!isToolUpdateEvent(event)) return []
        const partial = asRecord(event.partialResult)
        return [{
          ...this.base('session.tool.progress'),
          assistant_message_id: this.assistantMessageID,
          call_id: event.toolCallId,
          content: readContent(partial.content),
          structured: asRecord(partial.details)
        }]
      }
      case 'tool_execution_end': {
        if (!isToolEndEvent(event)) return []
        const result = asRecord(event.result)
        if (event.isError) {
          return [{
            ...this.base('session.tool.failed'),
            assistant_message_id: this.assistantMessageID,
            call_id: event.toolCallId,
            error: { type: 'unknown', message: readErrorMessage(result) },
            result
          }]
        }
        return [{
          ...this.base('session.tool.success'),
          assistant_message_id: this.assistantMessageID,
          call_id: event.toolCallId,
          content: readContent(result.content),
          structured: asRecord(result.details),
          output_paths: readOutputPaths(result),
          result
        }]
      }
      case 'session_before_compact': {
        const preparation = asRecord(asRecord(event).preparation)
        return [{
          ...this.base('session.compaction.started'),
          message_id: firstString(asRecord(event).messageId, preparation.firstKeptEntryId, this.assistantMessageID),
          reason: firstString(asRecord(event).reason, 'auto')
        }]
      }
      case 'session_compact': {
        const entry = asRecord(asRecord(event).compactionEntry)
        const details = asRecord(entry.details)
        return [{
          ...this.base('session.compaction.ended'),
          message_id: firstString(entry.id, entry.firstKeptEntryId, this.assistantMessageID),
          reason: firstString(asRecord(event).reason, details.reason, 'auto'),
          summary: firstString(entry.summary),
          recent: firstString(entry.recent, details.recent, details.recentSummary, '')
        }]
      }
      default:
        return []
    }
  }

  /**
   * Bridges Pi's granular AssistantMessageEvent into OpenCode-style runtime events.
   * Pi emits thinking_*, text_*, and toolcall_* sub-events inside message_update;
   * each is mapped to the corresponding session.* event so the frontend can render
   * reasoning, text streaming, and tool-input composition in real time.
   */
  private handleAssistantMessageEvent(ame: Record<string, unknown>): AgentRuntimeEvent[] {
    const type = ame.type as string
    const contentIndex = typeof ame.contentIndex === 'number' ? ame.contentIndex : 0
    switch (type) {
      case 'text_start':
        return [this.textEvent<TextStartedEvent>('session.text.started', contentIndex, { text_id: this.textID(contentIndex) })]
      case 'text_delta': {
        const delta = String(ame.delta ?? '')
        if (!delta) return []
        this.emittedText = true
        return [this.textEvent<TextDeltaEvent>('session.text.delta', contentIndex, { text_id: this.textID(contentIndex), delta })]
      }
      case 'text_end':
        // HarnessRunner owns session.text.ended (it accumulates deltas and emits the final text)
        return []
      case 'thinking_start':
        return [this.reasoningEvent<ReasoningStartedEvent>('session.reasoning.started', contentIndex, {})]
      case 'thinking_delta': {
        const delta = String(ame.delta ?? '')
        if (!delta) return []
        return [this.reasoningEvent<ReasoningDeltaEvent>('session.reasoning.delta', contentIndex, { delta })]
      }
      case 'thinking_end':
        return [this.reasoningEvent<ReasoningEndedEvent>('session.reasoning.ended', contentIndex, { text: String(ame.content ?? '') })]
      case 'toolcall_start': {
        const info = extractToolCallInfo(ame.partial, contentIndex)
        return [this.toolInputEvent<ToolInputStartedEvent>('session.tool.input.started', info.id, info.name, {})]
      }
      case 'toolcall_delta': {
        const delta = String(ame.delta ?? '')
        if (!delta) return []
        const info = extractToolCallInfo(ame.partial, contentIndex)
        return [this.toolInputEvent<ToolInputDeltaEvent>('session.tool.input.delta', info.id, info.name, { delta })]
      }
      case 'toolcall_end': {
        const toolCall = asRecord(ame.toolCall)
        const callId = typeof toolCall.id === 'string' ? toolCall.id : `call:${contentIndex}`
        const args = asRecord(toolCall.arguments)
        return [this.toolInputEvent<ToolInputEndedEvent>('session.tool.input.ended', callId, undefined, { text: JSON.stringify(args) })]
      }
      default:
        return []
    }
  }

  private textID(contentIndex: number): string {
    return `${this.assistantMessageID}:text:${contentIndex}`
  }

  private reasoningID(contentIndex: number): string {
    return `${this.assistantMessageID}:reasoning:${contentIndex}`
  }

  private textEvent<T extends AgentRuntimeEvent>(type: T['type'], contentIndex: number, extra: Partial<T>): T {
    return {
      ...this.base(type),
      assistant_message_id: this.assistantMessageID,
      ...extra
    } as T
  }

  private reasoningEvent<T extends AgentRuntimeEvent>(type: T['type'], contentIndex: number, extra: Partial<T>): T {
    return {
      ...this.base(type),
      assistant_message_id: this.assistantMessageID,
      reasoning_id: this.reasoningID(contentIndex),
      ...extra
    } as T
  }

  private toolInputEvent<T extends AgentRuntimeEvent>(type: T['type'], callId: string, toolName: string | undefined, extra: Partial<T>): T {
    const payload: Record<string, unknown> = {
      ...this.base(type),
      assistant_message_id: this.assistantMessageID,
      call_id: callId,
      ...extra
    }
    if (toolName !== undefined) payload.tool_name = toolName
    return payload as T
  }

  private base<TType extends AgentRuntimeEvent['type']>(type: TType) {
    this.seq += 1
    return {
      type,
      session_id: this.sessionID,
      event_id: this.nextEventID(),
      seq: this.seq,
      timestamp: this.now()
    }
  }
}

function isToolStartEvent(event: PiAgentEvent): event is Extract<PiAgentEvent, { type: 'tool_execution_start' }> {
  const record = asRecord(event)
  return typeof record.toolCallId === 'string' && typeof record.toolName === 'string'
}

function isToolUpdateEvent(event: PiAgentEvent): event is Extract<PiAgentEvent, { type: 'tool_execution_update' }> {
  const record = asRecord(event)
  return typeof record.toolCallId === 'string' && typeof record.toolName === 'string'
}

function isToolEndEvent(event: PiAgentEvent): event is Extract<PiAgentEvent, { type: 'tool_execution_end' }> {
  const record = asRecord(event)
  return typeof record.toolCallId === 'string' && typeof record.toolName === 'string'
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? { ...(value as Record<string, unknown>) } : {}
}

function readContent(value: unknown): ContentPart[] {
  return Array.isArray(value)
    ? value.filter((item): item is ContentPart => Boolean(item && typeof item === 'object' && 'type' in item))
    : []
}

function readOutputPaths(result: Record<string, unknown>): string[] {
  const values = [result.outputPaths, result.output_paths]
    .flatMap((value) => Array.isArray(value) ? value : [])
    .map((value) => firstString(value))
    .filter(Boolean)
  return [...new Set(values)]
}

function readAssistantText(message: unknown): string {
  const record = asRecord(message)
  const role = firstString(record.role)
  if (role && role !== 'assistant') return ''
  const content = record.content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .map((item) => asRecord(item))
    .filter((item) => item.type === 'text' && typeof item.text === 'string')
    .map((item) => String(item.text))
    .join('')
}

function readErrorMessage(result: Record<string, unknown>): string {
  const content = readContent(result.content)
  const text = content.map((item) => item.text).filter(Boolean).join('\n')
  return text || 'Tool execution failed'
}

function firstString(...values: unknown[]): string {
  for (const value of values) {
    if (value !== undefined && value !== null && String(value).trim()) return String(value).trim()
  }
  return ''
}

/**
 * Extracts tool call id and name from a Pi partial AssistantMessage at a given contentIndex.
 * At toolcall_start the model has usually sent the id and name but arguments may be empty.
 * Falls back to a contentIndex-based provisional id when the partial doesn't carry toolCall info.
 */
function extractToolCallInfo(partial: unknown, contentIndex: number): { id: string; name: string } {
  const record = asRecord(partial)
  const content = record.content
  if (!Array.isArray(content)) return { id: `call:${contentIndex}`, name: 'unknown' }
  const item = asRecord(content[contentIndex])
  if (item.type !== 'toolCall') return { id: `call:${contentIndex}`, name: 'unknown' }
  return {
    id: typeof item.id === 'string' ? item.id : `call:${contentIndex}`,
    name: typeof item.name === 'string' ? item.name : 'unknown'
  }
}
