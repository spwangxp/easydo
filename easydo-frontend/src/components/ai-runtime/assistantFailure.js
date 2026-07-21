function isRecord(value) {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function parseRecord(value) {
  if (isRecord(value)) return value
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    const parsed = JSON.parse(value)
    return isRecord(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function firstText(...values) {
  for (const value of values) {
    if (value === undefined || value === null) continue
    const text = String(value).trim()
    if (text) return text
  }
  return ''
}

function errorRecords(entry = {}) {
  const output = parseRecord(entry.output)
  const runtimeError = parseRecord(output.runtime_error)
  const outputError = parseRecord(output.error)
  const entryError = parseRecord(entry.error)
  const payload = parseRecord(output.error_payload || entry.error_payload)
  const providerError = parseRecord(output.provider_error)
  return { output, runtimeError, outputError, entryError, payload, providerError }
}

function errorField(records, field) {
  return firstText(
    records.runtimeError[field],
    records.payload[field],
    records.outputError[field],
    records.providerError[field],
    records.entryError[field],
    records.output[field]
  )
}

function entryRuntimeRunId(entry = {}) {
  const records = errorRecords(entry)
  return firstText(
    entry.runtime_run_id,
    records.output.runtime_run_id,
    errorField(records, 'runtime_run_id')
  )
}

function isAssistantFailureEntry(entry = {}) {
  if (entry.role !== 'assistant') return false
  return entry.entry_type === 'error' || entry.status === 'failed' || entry.status === 'cancelled'
}

/** Stable failure fields for rendering terminal timeline events and recovery actions. */
function assistantFailureDetails(entry = {}) {
  const records = errorRecords(entry)
  const status = entry.status === 'cancelled' ? 'cancelled' : 'failed'
  const reason = firstText(
    errorField(records, 'user_message'),
    errorField(records, 'message'),
    errorField(records, 'reason'),
    // Avoid treating partial model answer as the failure reason when structured error exists elsewhere.
    isLikelyPartialAnswerContent(entry) ? '' : entry.content,
    status === 'cancelled' ? '本次生成已停止' : '本次生成未完成'
  )
  const retryableValue =
    records.runtimeError.retryable ??
    records.payload.retryable ??
    records.outputError.retryable ??
    records.providerError.retryable ??
    records.entryError.retryable

  const requestId = firstText(
    errorField(records, 'request_id'),
    entry.request_id,
    records.output.request_id
  )
  const occurredAt = firstText(
    errorField(records, 'occurred_at'),
    entry.updated_at,
    entry.finished_at,
    entry.created_at
  )

  return {
    status,
    statusLabel: status === 'cancelled' ? '已停止' : '生成失败',
    reason,
    user_message: reason,
    code: errorField(records, 'code'),
    category: errorField(records, 'category'),
    retryable: status === 'failed' && retryableValue === true,
    request_id: requestId,
    requestId,
    runtime_run_id: entryRuntimeRunId(entry),
    runtimeRunId: entryRuntimeRunId(entry),
    occurred_at: occurredAt,
    occurredAt
  }
}

function isLikelyPartialAnswerContent(entry = {}) {
  const records = errorRecords(entry)
  const errorText = firstText(errorField(records, 'user_message'), errorField(records, 'message'))
  if (!errorText) return false
  const content = firstText(entry.content, records.output.text)
  if (!content) return false
  return content !== errorText
}

/** Partial answer / reasoning preserved after a failed or cancelled run. */
function assistantPartialContent(entry = {}) {
  if (!isAssistantFailureEntry(entry)) return { answer: '', reasoning: '' }
  const records = errorRecords(entry)
  const details = assistantFailureDetails(entry)
  const rawAnswer = firstText(records.output.text, entry.content)
  // Do not treat the failure reason itself as partial answer.
  const answer = rawAnswer && rawAnswer !== details.reason ? rawAnswer : ''
  const message = parseRecord(records.output.message)
  const reasoning = firstText(
    records.output.reasoning,
    message.reasoning,
    message.reasoning_content,
    message.thinking
  )
  return { answer, reasoning }
}

function hasAssistantPartialContent(entry = {}) {
  const partial = assistantPartialContent(entry)
  return Boolean(partial.answer || partial.reasoning)
}

/** Fill transport-only failure details into their chronological timeline positions. */
function runtimeEventsWithFailureTerminal(entry = {}, events = []) {
  if (!isAssistantFailureEntry(entry)) return events
  const details = assistantFailureDetails(entry)
  const partial = assistantPartialContent(entry)
  const terminalEvent = details.status === 'cancelled' ? 'run.cancelled' : 'run.failed'
  const entryId = firstText(entry.id, entry.idempotency_key, 'unknown')
  const syntheticEvents = []

  if (partial.reasoning && !hasRuntimeEvent(events, ['session.reasoning.started', 'session.reasoning.delta', 'session.reasoning.ended', 'reasoning_delta'])) {
    syntheticEvents.push(syntheticRuntimeEvent(entryId, 'session.reasoning.ended', details, { text: partial.reasoning }))
  }
  if (partial.answer && !hasRuntimeEvent(events, ['session.text.started', 'session.text.delta', 'session.text.ended', 'answer_delta'])) {
    syntheticEvents.push(syntheticRuntimeEvent(entryId, 'session.text.ended', details, { text: partial.answer }))
  }

  const terminalIndex = events.findIndex((event) => runtimeEventNames(event).includes(terminalEvent))
  const timelineEvents = terminalIndex < 0
    ? [...events, ...syntheticEvents]
    : [...events.slice(0, terminalIndex), ...syntheticEvents, ...events.slice(terminalIndex)]
  if (terminalIndex >= 0) return timelineEvents
  return [...timelineEvents, syntheticRuntimeEvent(entryId, terminalEvent, details, {
    status: details.status,
    message: details.reason,
    code: details.code,
    category: details.category,
    retryable: details.retryable
  })]
}

function syntheticRuntimeEvent(entryId, eventName, details, fields = {}) {
  const eventId = `assistant-entry-${entryId}-${eventName}`
  const data = {
    event_id: eventId,
    event_type: eventName,
    runtime_run_id: details.runtime_run_id,
    timestamp: details.occurred_at,
    synthetic: true,
    ...fields
  }
  return {
    event_id: eventId,
    event: eventName,
    timestamp: details.occurred_at,
    runtime_run_id: details.runtime_run_id,
    payload: data,
    data,
    display_json: {
      event_type: eventName,
      runtime_run_id: details.runtime_run_id,
      timestamp: details.occurred_at,
      status: fields.status,
      summary: fields.message,
      code: fields.code,
      category: fields.category,
      retryable: fields.retryable
    }
  }
}

function hasRuntimeEvent(events = [], names = []) {
  const nameSet = new Set(names)
  return events.some((event) => runtimeEventNames(event).some((name) => nameSet.has(name)))
}

function runtimeEventNames(event) {
  return [
    event?.event,
    event?.type,
    event?.display_json?.event_type,
    event?.data?.event_type,
    event?.payload?.event_type,
    event?.data?.event?.event,
    event?.data?.event?.type,
    event?.payload?.event?.event,
    event?.payload?.event?.type
  ].map((value) => String(value || '')).filter(Boolean)
}

/**
 * Locate the user prompt that produced a failed assistant entry for retry.
 * Prefer parent_entry_id, then the nearest preceding user entry.
 */
function precedingUserPrompt(entries = [], failedEntry = {}) {
  const list = Array.isArray(entries) ? entries : []
  const failedId = firstText(failedEntry.id, failedEntry.idempotency_key)
  const parentId = firstText(failedEntry.parent_entry_id, failedEntry.parentEntryId)

  if (parentId) {
    const parent = list.find((entry) =>
      entry?.role === 'user' &&
      firstText(entry.id, entry.idempotency_key, entry.client_entry_id) === parentId
    )
    if (parent) {
      return {
        content: firstText(parent.content, parent.output?.text),
        attachments: normalizeAttachments(parent.attachments || parent.input?.attachments || parent.output?.attachments),
        entry: parent
      }
    }
  }

  let failedIndex = list.findIndex((entry) =>
    firstText(entry?.id, entry?.idempotency_key) === failedId
  )
  if (failedIndex < 0) failedIndex = list.length
  for (let index = failedIndex - 1; index >= 0; index -= 1) {
    const entry = list[index]
    if (entry?.role !== 'user') continue
    return {
      content: firstText(entry.content, entry.output?.text),
      attachments: normalizeAttachments(entry.attachments || entry.input?.attachments || entry.output?.attachments),
      entry
    }
  }
  return { content: '', attachments: [], entry: null }
}

function normalizeAttachments(value) {
  if (!Array.isArray(value)) return []
  return value.filter((item) => item && (typeof item === 'object' || typeof item === 'string'))
}

export {
  assistantFailureDetails,
  assistantPartialContent,
  entryRuntimeRunId,
  hasAssistantPartialContent,
  isAssistantFailureEntry,
  precedingUserPrompt,
  runtimeEventsWithFailureTerminal
}
