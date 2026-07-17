import { firstString } from './agentConversationShared.js'
import { derivePendingActions } from './agentChatboxState.js'
import { isPersistentRuntimeEvent, isProcessRuntimeEvent } from './aiRuntimeEvents.js'

/**
 * Shared Pi/runtime stream projection used by Chatbox and Page Assistant.
 * Keeps dual Stores from diverging on session.text/reasoning deltas and run/permission IDs.
 */

export function applyRuntimeStreamEventToAssistantEntry({
  patchEntry,
  entriesRef,
  assistantDraftId,
  runtimeEventName,
  runtimeEvent = {},
  data = {},
  onPendingActions
}) {
  if (!runtimeEventName) return

  if (runtimeEventName === 'session.step.started' || runtimeEventName === 'session.reasoning.started') {
    patchEntry(entriesRef, assistantDraftId, (entry) => {
      entry.output.reasoning = ''
    })
  }

  if (runtimeEventName === 'session.text.started') {
    patchEntry(entriesRef, assistantDraftId, (entry) => {
      entry.content = ''
      entry.content_blocks = [{ type: 'text', text: '' }]
    })
  }

  if (runtimeEventName === 'session.text.delta') {
    const delta = firstString(runtimeEvent.delta, runtimeEvent.text)
    if (delta) {
      patchEntry(entriesRef, assistantDraftId, (entry) => {
        entry.content = `${entry.content || ''}${delta}`
        entry.content_blocks = [{ type: 'text', text: entry.content }]
      })
    }
  }

  if (runtimeEventName === 'session.text.ended') {
    const finalText = firstString(runtimeEvent.text, runtimeEvent.delta)
    if (finalText) {
      patchEntry(entriesRef, assistantDraftId, (entry) => {
        entry.content = finalText
        entry.content_blocks = [{ type: 'text', text: finalText }]
      })
    }
  }

  if (runtimeEventName === 'session.reasoning.delta') {
    const delta = firstString(runtimeEvent.delta, runtimeEvent.text)
    if (delta) {
      patchEntry(entriesRef, assistantDraftId, (entry) => {
        entry.output.reasoning = `${entry.output.reasoning || ''}${delta}`
      })
    }
  }

  if (runtimeEventName === 'session.reasoning.ended') {
    const finalReasoning = firstString(runtimeEvent.text, runtimeEvent.delta)
    if (finalReasoning) {
      patchEntry(entriesRef, assistantDraftId, (entry) => {
        entry.output.reasoning = finalReasoning
      })
    }
  }

  if (runtimeEventName === 'run.started') {
    const runtimeRunId = firstString(
      runtimeEvent.runtime_run_id,
      runtimeEvent.run?.runtime_run_id,
      runtimeEvent.payload?.runtime_run_id,
      data.runtime_run_id,
      data.run?.runtime_run_id
    )
    if (runtimeRunId) {
      patchEntry(entriesRef, assistantDraftId, (entry) => {
        entry.runtime_run_id = entry.runtime_run_id || runtimeRunId
        entry.output.runtime_run_id = entry.output.runtime_run_id || runtimeRunId
      })
      onPendingActions?.(derivePendingActions(entriesRef.value))
    }
  }

  if (runtimeEventName === 'permission.asked') {
    const runtimeRunId = firstString(
      runtimeEvent.runtime_run_id,
      runtimeEvent.run?.runtime_run_id,
      data.runtime_run_id,
      data.run?.runtime_run_id
    )
    patchEntry(entriesRef, assistantDraftId, (entry) => {
      if (runtimeRunId) {
        entry.runtime_run_id = entry.runtime_run_id || runtimeRunId
        entry.output.runtime_run_id = entry.output.runtime_run_id || runtimeRunId
      }
      entry.output.awaiting_approval = {
        approval_id: firstString(runtimeEvent.request_id, runtimeEvent.approval_id),
        call_id: firstString(
          runtimeEvent.call_id,
          runtimeEvent.tool_call_id,
          runtimeEvent.provider_tool_call_id
        ),
        tool_name: firstString(runtimeEvent.tool_name, runtimeEvent.tool),
        reason: firstString(runtimeEvent.reason, runtimeEvent.message),
        input: runtimeEvent.input || {},
        runtime_run_id: runtimeRunId || entry.runtime_run_id || ''
      }
    })
    onPendingActions?.(derivePendingActions(entriesRef.value))
  }

  if (runtimeEventName === 'permission.resolved') {
    const resolvedID = firstString(runtimeEvent.request_id, runtimeEvent.approval_id)
    if (resolvedID) {
      // Clear awaiting_approval from any entry that matches the resolved ID.
      // This prevents Source 1 (normalizePiApprovals) in derivePendingActions
      // from re-projecting a resolved approval as pending.
      const entries = Array.isArray(entriesRef.value) ? entriesRef.value : []
      entries.forEach((entry, index) => {
        const approval = entry?.output?.awaiting_approval
        if (!approval || typeof approval !== 'object') return
        const approvalID = firstString(approval.approval_id, approval.request_id)
        if (approvalID && approvalID === resolvedID) {
          patchEntry(entriesRef, entry.id || index, (e) => {
            delete e.output.awaiting_approval
          })
        }
      })
    }
    onPendingActions?.(derivePendingActions(entriesRef.value))
  }

  if (runtimeEventName === 'skill.available') {
    patchEntry(entriesRef, assistantDraftId, (entry) => {
      const skills = Array.isArray(entry.output.available_skills) ? entry.output.available_skills : []
      entry.output.available_skills = [...skills, runtimeEvent]
    })
  }

  if (runtimeEventName === 'skill.loaded') {
    patchEntry(entriesRef, assistantDraftId, (entry) => {
      const skills = Array.isArray(entry.output.loaded_skills) ? entry.output.loaded_skills : []
      entry.output.loaded_skills = [...skills, runtimeEvent]
    })
  }
}

export function shouldAppendRuntimeEvent(runtimeEventName) {
  return isPersistentRuntimeEvent(runtimeEventName) || isProcessRuntimeEvent(runtimeEventName)
}
