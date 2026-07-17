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

/**
 * Stable public failure fields for AssistantFailureCard (P2-04).
 * Always returns a non-empty reason so the card never renders blank when trace is missing.
 */
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
  precedingUserPrompt
}
