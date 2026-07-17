import type { AgentRuntimeEvent, ContentPart, FileRef, ModelRef, RuntimeErrorInfo, TokenUsage } from './events.js'

export interface ProjectedMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system'
  parent_id?: string
  text?: string
  files?: FileRef[]
  agent?: string
  model?: ModelRef
  finish?: string
  tokens?: TokenUsage
  cost?: number
  error?: RuntimeErrorInfo
  time: {
    created: string
    completed?: string
  }
}

export type ProjectedPart = ProjectedTextPart | ProjectedReasoningPart | ProjectedToolPart | ProjectedCompactionPart

export interface ProjectedPartBase {
  id: string
  session_id: string
  message_id: string
  type: string
  index: number
}

export interface ProjectedTextPart extends ProjectedPartBase {
  type: 'text'
  text: string
}

export interface ProjectedReasoningPart extends ProjectedPartBase {
  type: 'reasoning'
  text: string
}

export interface ProjectedToolPart extends ProjectedPartBase {
  type: 'tool'
  call_id: string
  tool: string
  state: 'pending' | 'running' | 'completed' | 'error'
  input?: Record<string, unknown>
  input_text?: string
  input_draft?: string
  content?: ContentPart[]
  structured?: Record<string, unknown>
  output_paths?: string[]
  result?: unknown
  error?: RuntimeErrorInfo
  approval?: {
    id: string
    status: 'pending' | 'approved' | 'rejected' | string
    reason?: string
  }
  timestamps: {
    created: string
    ran?: string
    completed?: string
  }
}

export interface ProjectedCompactionPart extends ProjectedPartBase {
  type: 'compaction'
  reason: string
  summary: string
  recent: string
}

export interface ProjectedApproval {
  id: string
  session_id: string
  call_id?: string
  tool_name: string
  input: Record<string, unknown>
  reason?: string
  status: 'pending' | 'approved' | 'rejected' | string
  message: string
  created_at: string
}

export interface SessionStatus {
  session_id: string
  type: 'idle' | 'busy' | 'retrying' | 'interrupted'
  message?: string
}

export interface SessionProjection {
  session: {
    id: string
    status: SessionStatus['type']
    agent?: string
    model?: ModelRef
    tokens: TokenUsage
    cost: number
  }
  messages: ProjectedMessage[]
  partsByMessage: Record<string, ProjectedPart[]>
  approvals: ProjectedApproval[]
  status: SessionStatus
}

interface MutableSessionState {
  messages: ProjectedMessage[]
  messagesByID: Map<string, ProjectedMessage>
  partsByMessage: Map<string, ProjectedPart[]>
  toolPartByCallID: Map<string, ProjectedToolPart>
  approvals: ProjectedApproval[]
  approvalByID: Map<string, ProjectedApproval>
  status: SessionStatus
}

const zeroTokens = (): TokenUsage => ({ input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } })

function partID(messageID: string, type: string, id: string) {
  return `${messageID}:${type}:${id}`
}

export class AgentRuntimeProjection {
  private sessions = new Map<string, MutableSessionState>()

  apply(event: AgentRuntimeEvent): void {
    const state = this.state(event.session_id)
    switch (event.type) {
      case 'session.prompted': {
        this.upsertMessage(state, {
          id: event.message_id,
          session_id: event.session_id,
          role: 'user',
          text: event.prompt,
          files: event.files ?? [],
          time: { created: event.timestamp }
        })
        state.status = { session_id: event.session_id, type: 'busy' }
        return
      }
      case 'session.step.started': {
        this.upsertMessage(state, {
          id: event.assistant_message_id,
          session_id: event.session_id,
          role: 'assistant',
          parent_id: event.parent_message_id,
          agent: event.agent,
          model: event.model,
          time: { created: event.timestamp }
        })
        state.status = { session_id: event.session_id, type: 'busy' }
        return
      }
      case 'session.step.ended': {
        const message = state.messagesByID.get(event.assistant_message_id)
        if (message) {
          message.finish = event.finish_reason
          message.tokens = event.tokens
          message.cost = event.cost
          message.time.completed = event.timestamp
        }
        state.status = { session_id: event.session_id, type: 'idle' }
        return
      }
      case 'session.step.failed': {
        const message = state.messagesByID.get(event.assistant_message_id)
        if (message) {
          message.error = event.error
          message.time.completed = event.timestamp
        }
        state.status = { session_id: event.session_id, type: 'idle', message: event.error.message }
        return
      }
      case 'session.text.started':
        this.upsertTextPart(state, event.assistant_message_id, event.session_id, event.text_id, '')
        return
      case 'session.text.delta': {
        const existing = this.findTextPart(state, event.assistant_message_id, 'text', event.text_id)
        this.upsertTextPart(state, event.assistant_message_id, event.session_id, event.text_id, `${existing?.text ?? ''}${event.delta}`)
        return
      }
      case 'session.text.ended':
        this.upsertTextPart(state, event.assistant_message_id, event.session_id, event.text_id, event.text)
        return
      case 'session.reasoning.started':
        this.upsertReasoningPart(state, event.assistant_message_id, event.session_id, event.reasoning_id, '')
        return
      case 'session.reasoning.delta': {
        const existing = this.findTextPart(state, event.assistant_message_id, 'reasoning', event.reasoning_id)
        this.upsertReasoningPart(state, event.assistant_message_id, event.session_id, event.reasoning_id, `${existing?.text ?? ''}${event.delta}`)
        return
      }
      case 'session.reasoning.ended':
        this.upsertReasoningPart(state, event.assistant_message_id, event.session_id, event.reasoning_id, event.text)
        return
      case 'session.tool.input.started': {
        const part = this.ensureToolPart(state, event.assistant_message_id, event.session_id, event.call_id, event.tool_name, event.timestamp)
        part.state = 'pending'
        part.tool = event.tool_name
        this.upsertPart(state, event.assistant_message_id, part)
        return
      }
      case 'session.tool.input.delta': {
        const part = this.ensureToolPart(state, event.assistant_message_id, event.session_id, event.call_id, event.tool_name, event.timestamp)
        part.input_draft = `${part.input_draft ?? ''}${event.delta}`
        part.state = 'pending'
        this.upsertPart(state, event.assistant_message_id, part)
        return
      }
      case 'session.tool.input.ended': {
        const part = this.ensureToolPart(state, event.assistant_message_id, event.session_id, event.call_id, undefined, event.timestamp)
        part.input_text = event.text
        part.input_draft = undefined
        part.input = parseToolInput(event.text) ?? part.input
        part.state = 'pending'
        this.upsertPart(state, event.assistant_message_id, part)
        return
      }
      case 'session.tool.called': {
        const existing = state.toolPartByCallID.get(event.call_id)
        const approval = this.approvalForCall(state, event.call_id)
        const part: ProjectedToolPart = {
          ...existing,
          id: partID(event.assistant_message_id, 'tool', event.call_id),
          session_id: event.session_id,
          message_id: event.assistant_message_id,
          type: 'tool',
          index: 0,
          call_id: event.call_id,
          tool: event.tool,
          state: 'running',
          input: event.input,
          approval: existing?.approval ?? approval,
          timestamps: { created: event.timestamp, ran: event.timestamp }
        }
        state.toolPartByCallID.set(event.call_id, part)
        this.upsertPart(state, event.assistant_message_id, part)
        return
      }
      case 'session.tool.progress': {
        const part = state.toolPartByCallID.get(event.call_id)
        if (!part) return
        part.state = 'running'
        part.content = event.content ?? part.content
        part.structured = event.structured ?? part.structured
        this.upsertPart(state, part.message_id, part)
        return
      }
      case 'session.tool.success': {
        const part = state.toolPartByCallID.get(event.call_id)
        if (!part) return
        part.state = 'completed'
        part.content = event.content
        part.structured = event.structured
        part.output_paths = event.output_paths
        part.result = event.result
        part.timestamps.completed = event.timestamp
        this.upsertPart(state, part.message_id, part)
        return
      }
      case 'session.tool.failed': {
        const part = state.toolPartByCallID.get(event.call_id)
        if (!part) return
        part.state = 'error'
        part.error = event.error
        part.result = event.result
        part.timestamps.completed = event.timestamp
        this.upsertPart(state, part.message_id, part)
        return
      }
      case 'permission.asked': {
        const approval: ProjectedApproval = {
          id: event.request_id,
          session_id: event.session_id,
          call_id: event.call_id,
          tool_name: event.tool_name,
          input: event.input,
          reason: event.reason,
          status: 'pending',
          message: event.message,
          created_at: event.timestamp
        }
        state.approvalByID.set(event.request_id, approval)
        state.approvals = [...state.approvalByID.values()]
        if (event.call_id) {
          const part = state.toolPartByCallID.get(event.call_id)
          if (part) part.approval = { id: event.request_id, status: 'pending', reason: event.reason }
        }
        return
      }
      case 'permission.resolved': {
        const approval = state.approvalByID.get(event.request_id)
        if (approval) {
          approval.status = event.result
          state.approvals = [...state.approvalByID.values()]
          if (approval.call_id) {
            const part = state.toolPartByCallID.get(approval.call_id)
            if (part?.approval) part.approval.status = event.result
          }
        }
        return
      }
      case 'session.compaction.ended':
      case 'context.compaction.completed':
        this.upsertPart(state, event.message_id, {
          id: partID(event.message_id, 'compaction', event.event_id),
          session_id: event.session_id,
          message_id: event.message_id,
          type: 'compaction',
          index: 0,
          reason: event.reason,
          summary: event.summary,
          recent: event.recent
        })
        return
      case 'session.model.switched':
        this.upsertMessage(state, {
          id: event.message_id,
          session_id: event.session_id,
          role: 'system',
          text: `model switched to ${event.model.provider_id}/${event.model.id}`,
          model: event.model,
          time: { created: event.timestamp }
        })
        return
      case 'session.agent.switched':
        this.upsertMessage(state, {
          id: event.message_id,
          session_id: event.session_id,
          role: 'system',
          text: `agent switched to ${event.agent}`,
          agent: event.agent,
          time: { created: event.timestamp }
        })
        return
      case 'session.error':
        state.status = { session_id: event.session_id, type: 'interrupted', message: event.message }
        return
      default:
        return
    }
  }

  getSession(sessionID: string): SessionProjection {
    const state = this.state(sessionID)
    const tokens = zeroTokens()
    let cost = 0
    let agent: string | undefined
    let model: ModelRef | undefined
    for (const message of state.messages) {
      if (message.agent) agent = message.agent
      if (message.model) model = message.model
      if (message.tokens) {
        tokens.input += message.tokens.input
        tokens.output += message.tokens.output
        tokens.reasoning += message.tokens.reasoning
        tokens.cache.read += message.tokens.cache.read
        tokens.cache.write += message.tokens.cache.write
      }
      cost += message.cost ?? 0
    }
    const partsByMessage: Record<string, ProjectedPart[]> = {}
    for (const [messageID, parts] of state.partsByMessage.entries()) {
      partsByMessage[messageID] = parts.map((part, index) => ({ ...part, index }))
    }
    return {
      session: { id: sessionID, status: state.status.type, agent, model, tokens, cost },
      messages: state.messages,
      partsByMessage,
      approvals: state.approvals,
      status: state.status
    }
  }

  private state(sessionID: string): MutableSessionState {
    const existing = this.sessions.get(sessionID)
    if (existing) return existing
    const created: MutableSessionState = {
      messages: [],
      messagesByID: new Map(),
      partsByMessage: new Map(),
      toolPartByCallID: new Map(),
      approvals: [],
      approvalByID: new Map(),
      status: { session_id: sessionID, type: 'idle' }
    }
    this.sessions.set(sessionID, created)
    return created
  }

  private upsertMessage(state: MutableSessionState, message: ProjectedMessage) {
    const existing = state.messagesByID.get(message.id)
    if (existing) {
      Object.assign(existing, message, { time: { ...existing.time, ...message.time } })
      return
    }
    state.messagesByID.set(message.id, message)
    state.messages.push(message)
  }

  private upsertPart(state: MutableSessionState, messageID: string, part: ProjectedPart) {
    const parts = state.partsByMessage.get(messageID) ?? []
    const index = parts.findIndex((item) => item.id === part.id)
    if (index >= 0) parts[index] = { ...part, index }
    else parts.push({ ...part, index: parts.length })
    state.partsByMessage.set(messageID, parts)
  }

  private findTextPart(state: MutableSessionState, messageID: string, type: 'text' | 'reasoning', id: string) {
    const targetID = partID(messageID, type, id)
    return (state.partsByMessage.get(messageID) ?? []).find((part): part is ProjectedTextPart | ProjectedReasoningPart =>
      part.id === targetID && part.type === type
    )
  }

  private upsertTextPart(state: MutableSessionState, messageID: string, sessionID: string, textID: string, text: string) {
    this.upsertPart(state, messageID, {
      id: partID(messageID, 'text', textID),
      session_id: sessionID,
      message_id: messageID,
      type: 'text',
      index: 0,
      text
    })
  }

  private upsertReasoningPart(state: MutableSessionState, messageID: string, sessionID: string, reasoningID: string, text: string) {
    this.upsertPart(state, messageID, {
      id: partID(messageID, 'reasoning', reasoningID),
      session_id: sessionID,
      message_id: messageID,
      type: 'reasoning',
      index: 0,
      text
    })
  }

  private ensureToolPart(state: MutableSessionState, messageID: string, sessionID: string, callID: string, toolName: string | undefined, timestamp: string) {
    const existing = state.toolPartByCallID.get(callID)
    if (existing) return existing
    const part: ProjectedToolPart = {
      id: partID(messageID, 'tool', callID),
      session_id: sessionID,
      message_id: messageID,
      type: 'tool',
      index: 0,
      call_id: callID,
      tool: toolName || callID,
      state: 'pending',
      timestamps: { created: timestamp }
    }
    state.toolPartByCallID.set(callID, part)
    return part
  }

  private approvalForCall(state: MutableSessionState, callID: string) {
    const approval = state.approvals.find((item) => item.call_id === callID)
    if (!approval) return undefined
    return { id: approval.id, status: approval.status, reason: approval.reason }
  }
}

function parseToolInput(text: string): Record<string, unknown> | undefined {
  const trimmed = text.trim()
  if (!trimmed) return undefined
  try {
    const parsed = JSON.parse(trimmed)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined
  } catch {
    return undefined
  }
}
