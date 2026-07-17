const zeroTokens = () => ({ input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } });
function partID(messageID, type, id) {
    return `${messageID}:${type}:${id}`;
}
export class AgentRuntimeProjection {
    sessions = new Map();
    apply(event) {
        const state = this.state(event.session_id);
        switch (event.type) {
            case 'session.prompted': {
                this.upsertMessage(state, {
                    id: event.message_id,
                    session_id: event.session_id,
                    role: 'user',
                    text: event.prompt,
                    files: event.files ?? [],
                    time: { created: event.timestamp }
                });
                state.status = { session_id: event.session_id, type: 'busy' };
                return;
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
                });
                state.status = { session_id: event.session_id, type: 'busy' };
                return;
            }
            case 'session.step.ended': {
                const message = state.messagesByID.get(event.assistant_message_id);
                if (message) {
                    message.finish = event.finish_reason;
                    message.tokens = event.tokens;
                    message.cost = event.cost;
                    message.time.completed = event.timestamp;
                }
                state.status = { session_id: event.session_id, type: 'idle' };
                return;
            }
            case 'session.step.failed': {
                const message = state.messagesByID.get(event.assistant_message_id);
                if (message) {
                    message.error = event.error;
                    message.time.completed = event.timestamp;
                }
                state.status = { session_id: event.session_id, type: 'idle', message: event.error.message };
                return;
            }
            case 'session.text.started':
                this.upsertTextPart(state, event.assistant_message_id, event.session_id, event.text_id, '');
                return;
            case 'session.text.delta': {
                const existing = this.findTextPart(state, event.assistant_message_id, 'text', event.text_id);
                this.upsertTextPart(state, event.assistant_message_id, event.session_id, event.text_id, `${existing?.text ?? ''}${event.delta}`);
                return;
            }
            case 'session.text.ended':
                this.upsertTextPart(state, event.assistant_message_id, event.session_id, event.text_id, event.text);
                return;
            case 'session.reasoning.started':
                this.upsertReasoningPart(state, event.assistant_message_id, event.session_id, event.reasoning_id, '');
                return;
            case 'session.reasoning.delta': {
                const existing = this.findTextPart(state, event.assistant_message_id, 'reasoning', event.reasoning_id);
                this.upsertReasoningPart(state, event.assistant_message_id, event.session_id, event.reasoning_id, `${existing?.text ?? ''}${event.delta}`);
                return;
            }
            case 'session.reasoning.ended':
                this.upsertReasoningPart(state, event.assistant_message_id, event.session_id, event.reasoning_id, event.text);
                return;
            case 'session.tool.input.started': {
                const part = this.ensureToolPart(state, event.assistant_message_id, event.session_id, event.call_id, event.tool_name, event.timestamp);
                part.state = 'pending';
                part.tool = event.tool_name;
                this.upsertPart(state, event.assistant_message_id, part);
                return;
            }
            case 'session.tool.input.delta': {
                const part = this.ensureToolPart(state, event.assistant_message_id, event.session_id, event.call_id, event.tool_name, event.timestamp);
                part.input_draft = `${part.input_draft ?? ''}${event.delta}`;
                part.state = 'pending';
                this.upsertPart(state, event.assistant_message_id, part);
                return;
            }
            case 'session.tool.input.ended': {
                const part = this.ensureToolPart(state, event.assistant_message_id, event.session_id, event.call_id, undefined, event.timestamp);
                part.input_text = event.text;
                part.input_draft = undefined;
                part.input = parseToolInput(event.text) ?? part.input;
                part.state = 'pending';
                this.upsertPart(state, event.assistant_message_id, part);
                return;
            }
            case 'session.tool.called': {
                const existing = state.toolPartByCallID.get(event.call_id);
                const approval = this.approvalForCall(state, event.call_id);
                const part = {
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
                };
                state.toolPartByCallID.set(event.call_id, part);
                this.upsertPart(state, event.assistant_message_id, part);
                return;
            }
            case 'session.tool.progress': {
                const part = state.toolPartByCallID.get(event.call_id);
                if (!part)
                    return;
                part.state = 'running';
                part.content = event.content ?? part.content;
                part.structured = event.structured ?? part.structured;
                this.upsertPart(state, part.message_id, part);
                return;
            }
            case 'session.tool.success': {
                const part = state.toolPartByCallID.get(event.call_id);
                if (!part)
                    return;
                part.state = 'completed';
                part.content = event.content;
                part.structured = event.structured;
                part.output_paths = event.output_paths;
                part.result = event.result;
                part.timestamps.completed = event.timestamp;
                this.upsertPart(state, part.message_id, part);
                return;
            }
            case 'session.tool.failed': {
                const part = state.toolPartByCallID.get(event.call_id);
                if (!part)
                    return;
                part.state = 'error';
                part.error = event.error;
                part.result = event.result;
                part.timestamps.completed = event.timestamp;
                this.upsertPart(state, part.message_id, part);
                return;
            }
            case 'permission.asked': {
                const approval = {
                    id: event.request_id,
                    session_id: event.session_id,
                    call_id: event.call_id,
                    tool_name: event.tool_name,
                    input: event.input,
                    reason: event.reason,
                    status: 'pending',
                    message: event.message,
                    created_at: event.timestamp
                };
                state.approvalByID.set(event.request_id, approval);
                state.approvals = [...state.approvalByID.values()];
                if (event.call_id) {
                    const part = state.toolPartByCallID.get(event.call_id);
                    if (part)
                        part.approval = { id: event.request_id, status: 'pending', reason: event.reason };
                }
                return;
            }
            case 'permission.resolved': {
                const approval = state.approvalByID.get(event.request_id);
                if (approval) {
                    approval.status = event.result;
                    state.approvals = [...state.approvalByID.values()];
                    if (approval.call_id) {
                        const part = state.toolPartByCallID.get(approval.call_id);
                        if (part?.approval)
                            part.approval.status = event.result;
                    }
                }
                return;
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
                });
                return;
            case 'session.model.switched':
                this.upsertMessage(state, {
                    id: event.message_id,
                    session_id: event.session_id,
                    role: 'system',
                    text: `model switched to ${event.model.provider_id}/${event.model.id}`,
                    model: event.model,
                    time: { created: event.timestamp }
                });
                return;
            case 'session.agent.switched':
                this.upsertMessage(state, {
                    id: event.message_id,
                    session_id: event.session_id,
                    role: 'system',
                    text: `agent switched to ${event.agent}`,
                    agent: event.agent,
                    time: { created: event.timestamp }
                });
                return;
            case 'session.error':
                state.status = { session_id: event.session_id, type: 'interrupted', message: event.message };
                return;
            default:
                return;
        }
    }
    getSession(sessionID) {
        const state = this.state(sessionID);
        const tokens = zeroTokens();
        let cost = 0;
        let agent;
        let model;
        for (const message of state.messages) {
            if (message.agent)
                agent = message.agent;
            if (message.model)
                model = message.model;
            if (message.tokens) {
                tokens.input += message.tokens.input;
                tokens.output += message.tokens.output;
                tokens.reasoning += message.tokens.reasoning;
                tokens.cache.read += message.tokens.cache.read;
                tokens.cache.write += message.tokens.cache.write;
            }
            cost += message.cost ?? 0;
        }
        const partsByMessage = {};
        for (const [messageID, parts] of state.partsByMessage.entries()) {
            partsByMessage[messageID] = parts.map((part, index) => ({ ...part, index }));
        }
        return {
            session: { id: sessionID, status: state.status.type, agent, model, tokens, cost },
            messages: state.messages,
            partsByMessage,
            approvals: state.approvals,
            status: state.status
        };
    }
    state(sessionID) {
        const existing = this.sessions.get(sessionID);
        if (existing)
            return existing;
        const created = {
            messages: [],
            messagesByID: new Map(),
            partsByMessage: new Map(),
            toolPartByCallID: new Map(),
            approvals: [],
            approvalByID: new Map(),
            status: { session_id: sessionID, type: 'idle' }
        };
        this.sessions.set(sessionID, created);
        return created;
    }
    upsertMessage(state, message) {
        const existing = state.messagesByID.get(message.id);
        if (existing) {
            Object.assign(existing, message, { time: { ...existing.time, ...message.time } });
            return;
        }
        state.messagesByID.set(message.id, message);
        state.messages.push(message);
    }
    upsertPart(state, messageID, part) {
        const parts = state.partsByMessage.get(messageID) ?? [];
        const index = parts.findIndex((item) => item.id === part.id);
        if (index >= 0)
            parts[index] = { ...part, index };
        else
            parts.push({ ...part, index: parts.length });
        state.partsByMessage.set(messageID, parts);
    }
    findTextPart(state, messageID, type, id) {
        const targetID = partID(messageID, type, id);
        return (state.partsByMessage.get(messageID) ?? []).find((part) => part.id === targetID && part.type === type);
    }
    upsertTextPart(state, messageID, sessionID, textID, text) {
        this.upsertPart(state, messageID, {
            id: partID(messageID, 'text', textID),
            session_id: sessionID,
            message_id: messageID,
            type: 'text',
            index: 0,
            text
        });
    }
    upsertReasoningPart(state, messageID, sessionID, reasoningID, text) {
        this.upsertPart(state, messageID, {
            id: partID(messageID, 'reasoning', reasoningID),
            session_id: sessionID,
            message_id: messageID,
            type: 'reasoning',
            index: 0,
            text
        });
    }
    ensureToolPart(state, messageID, sessionID, callID, toolName, timestamp) {
        const existing = state.toolPartByCallID.get(callID);
        if (existing)
            return existing;
        const part = {
            id: partID(messageID, 'tool', callID),
            session_id: sessionID,
            message_id: messageID,
            type: 'tool',
            index: 0,
            call_id: callID,
            tool: toolName || callID,
            state: 'pending',
            timestamps: { created: timestamp }
        };
        state.toolPartByCallID.set(callID, part);
        return part;
    }
    approvalForCall(state, callID) {
        const approval = state.approvals.find((item) => item.call_id === callID);
        if (!approval)
            return undefined;
        return { id: approval.id, status: approval.status, reason: approval.reason };
    }
}
function parseToolInput(text) {
    const trimmed = text.trim();
    if (!trimmed)
        return undefined;
    try {
        const parsed = JSON.parse(trimmed);
        return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : undefined;
    }
    catch {
        return undefined;
    }
}
