<template>
  <div class="page-assistant" :style="assistantPositionStyle">
    <transition name="assistant-panel">
      <section v-if="open" class="assistant-panel" :class="assistantPanelClass" aria-label="页面助手">
        <header class="assistant-header" @pointerdown="startAssistantDrag">
          <div>
            <strong>页面助手</strong>
            <span>{{ contextRef.object_type || 'page' }}</span>
          </div>
          <button type="button" class="assistant-icon-button" @pointerdown.stop @click="open = false">
            <el-icon><Close /></el-icon>
          </button>
        </header>

        <div ref="scrollBox" class="assistant-messages">
          <div v-if="assistantStore.loading" class="assistant-empty">会话加载中</div>
          <div v-else-if="assistantStore.entries.length === 0" class="assistant-empty">可以直接询问当前页面</div>
          <article
            v-for="entry in assistantStore.entries"
            :key="entry.id || entry.idempotency_key"
            class="assistant-message"
            :class="`assistant-message--${entry.role}`"
          >
            <span class="assistant-message__sender">{{ messageHeaderText(entry) }}</span>

            <template v-if="entry.role === 'user'">
              <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-user-${index}`">
                <div v-if="block.kind === 'markdown'" class="assistant-content assistant-content--markdown" v-html="renderMessageMarkdown(block.text)" />
                <div v-else class="assistant-content assistant-content--text">{{ block.text }}</div>
              </template>
            </template>

            <div v-if="assistantStatusText(entry) || assistantPhaseTimings(entry).length || entryRuntimeRunId(entry)" class="assistant-message__meta-row">
              <div>
                <div v-if="assistantStatusText(entry)" class="assistant-status">{{ assistantStatusText(entry) }}</div>
                <div v-if="assistantPhaseTimings(entry).length" class="assistant-stage-timings">
                  <span v-for="item in assistantPhaseTimings(entry)" :key="item.key">{{ item.label }}耗时 {{ item.value }}</span>
                </div>
              </div>
              <el-button
                v-if="entryRuntimeRunId(entry)"
                size="small"
                text
                :icon="Refresh"
                :loading="assistantStore.isRuntimeEventsRefreshing(entryRuntimeRunId(entry))"
                @click="refreshRuntimeTrace(entry)"
              >
                刷新轨迹
              </el-button>
            </div>

            <RuntimeTrace
              v-if="entry.role === 'assistant' && (assistantRuntimeEvents(entry, assistantStore.entries).length || assistantReasoningText(entry))"
              :events="assistantRuntimeEvents(entry, assistantStore.entries)"
              :reasoning="assistantReasoningText(entry)"
              :answer="runtimeTraceAnswer(entry)"
              :timings="entry.output?.timings || {}"
              :limit="runtimeTraceLimit(entry)"
              :load-artifact="getPageAssistantArtifact"
              :load-run-events="listPageAssistantRunEvents"
              :pending-actions="assistantStore.pendingActions"
            >
              <template #approval-actions="{ agentAction }">
                <el-button
                  class="assistant-approval-button"
                  :class="{ 'is-selected': approvalButtonSelected(agentAction, 'approve_once') }"
                  size="small"
                  type="primary"
                  :plain="!approvalButtonSelected(agentAction, 'approve_once')"
                  :title="approvalActionTooltip(agentAction)"
                  :disabled="approvalButtonDisabled(agentAction)"
                  @click="assistantStore.approveAction(agentAction)"
                >
                  批准一次
                </el-button>
                <el-button
                  class="assistant-approval-button"
                  :class="{ 'is-selected': approvalButtonSelected(agentAction, 'approve_session') }"
                  size="small"
                  :text="!approvalButtonSelected(agentAction, 'approve_session')"
                  :type="approvalButtonSelected(agentAction, 'approve_session') ? 'primary' : ''"
                  :title="approvalActionTooltip(agentAction)"
                  :disabled="approvalButtonDisabled(agentAction)"
                  @click="assistantStore.approveActionForSession(agentAction)"
                >
                  本会话批准
                </el-button>
                <el-button
                  class="assistant-approval-button"
                  :class="{ 'is-selected': approvalButtonSelected(agentAction, 'reject') }"
                  size="small"
                  :text="!approvalButtonSelected(agentAction, 'reject')"
                  type="danger"
                  :title="approvalActionTooltip(agentAction)"
                  :disabled="approvalButtonDisabled(agentAction)"
                  @click="assistantStore.rejectAction(agentAction)"
                >
                  拒绝
                </el-button>
                <div v-if="agentAction.pi_approval" class="assistant-approval-steer">
                  <el-input
                    v-model="steerInstructions[agentAction.id]"
                    size="small"
                    maxlength="500"
                    placeholder="输入新的执行指令"
                    :disabled="approvalButtonDisabled(agentAction)"
                    @keyup.enter="submitSteer(agentAction)"
                  />
                  <el-button
                    size="small"
                    :icon="Position"
                    :disabled="approvalButtonDisabled(agentAction) || !String(steerInstructions[agentAction.id] || '').trim()"
                    @click="submitSteer(agentAction)"
                  >
                    调整
                  </el-button>
                </div>
              </template>
              <template #terminal-actions="{ item }">
                <div v-if="isFailureTerminalItem(item, entry)" class="assistant-terminal-actions">
                  <el-button
                    v-if="entryRuntimeRunId(entry)"
                    size="small"
                    text
                    :icon="Refresh"
                    :loading="assistantStore.isRuntimeEventsRefreshing(entryRuntimeRunId(entry))"
                    @click="refreshRuntimeTrace(entry)"
                  >
                    刷新轨迹
                  </el-button>
                  <el-button
                    size="small"
                    :icon="Position"
                    :disabled="failureActionDisabled"
                    @click="continueAfterFailure(entry)"
                  >
                    继续
                  </el-button>
                  <el-button
                    v-if="canRetryFailedEntry(entry)"
                    size="small"
                    type="primary"
                    :icon="Refresh"
                    :disabled="failureActionDisabled"
                    @click="retryFailedEntry(entry)"
                  >
                    重试
                  </el-button>
                </div>
              </template>
            </RuntimeTrace>

            <template
              v-if="entry.role === 'assistant' && showFinalAnswer(entry)"
            >
              <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-assistant-${index}`">
                <div v-if="block.kind === 'markdown'" class="assistant-content assistant-content--markdown" v-html="renderMessageMarkdown(block.text)" />
                <div v-else-if="block.kind === 'html'" class="assistant-content assistant-content--html-preview">
                  <div>
                    <strong>{{ block.label || 'HTML' }}</strong>
                    <span>{{ block.text.length }} 字符</span>
                  </div>
                  <el-button size="small" text :icon="View" @click="openHtmlPreview(block)">预览 HTML</el-button>
                </div>
                <div v-else class="assistant-content assistant-content--text">{{ block.text }}</div>
              </template>
            </template>

            <template v-else-if="entry?.role === 'tool'">
              <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-tool-${index}`">
                <div v-if="block.kind === 'markdown'" class="assistant-content assistant-content--markdown" v-html="renderMessageMarkdown(block.text)" />
                <div v-else class="assistant-content assistant-content--text">{{ block.text }}</div>
              </template>
            </template>

            <div v-if="entry.role === 'assistant' && (rawDetailsText(entry) || assistantAgentRunMetrics(entry).length)" class="assistant-agent-footer">
              <details v-if="rawDetailsText(entry)" class="assistant-structured" :open="entry.output?.output_schema_valid === false">
                <summary>诊断详情</summary>
                <pre>{{ rawDetailsText(entry) }}</pre>
              </details>
              <div v-if="assistantAgentRunMetrics(entry).length" class="assistant-agent-metrics" aria-label="Agent run metrics">
                <span v-for="metric in assistantAgentRunMetrics(entry)" :key="metric.key">
                  <strong>{{ metric.label }}</strong>{{ metric.value }}
                </span>
              </div>
            </div>
          </article>
        </div>

        <RuntimeSessionQueue
          v-if="assistantStore.queueItems.length"
          class="assistant-queue-panel"
          :items="assistantStore.queueItems"
          :loading="assistantStore.queueLoading"
          @cancel="assistantStore.cancelQueueItem"
        />
        <form class="assistant-input-row" @submit.prevent="handleSend">
          <el-input
            v-model="draft"
            :autosize="{ minRows: 1, maxRows: 4 }"
            type="textarea"
            placeholder="输入问题"
            :disabled="inputDisabled"
            @keydown.enter.exact.prevent="handleSend"
          />
          <el-select v-model="assistantStore.queueMode" size="small" class="assistant-queue-mode" :disabled="inputDisabled">
            <el-option label="跟进" value="follow_up" />
            <el-option label="注入" value="steer" :disabled="!assistantStore.hasActiveRun" />
            <el-option label="停发" value="stop_and_run" />
          </el-select>
          <button v-if="assistantStore.sending || assistantStore.hasActiveRun" type="button" class="assistant-stop" @click="assistantStore.stopGeneration">
            <el-icon><CircleCloseFilled /></el-icon>
          </button>
          <button type="submit" class="assistant-send" :disabled="!canSend">
            <el-icon><Position /></el-icon>
          </button>
        </form>
        <div class="assistant-model-row">
          <el-popover
            v-model:visible="modelPopoverOpen"
            placement="top-start"
            width="380"
            trigger="click"
            :disabled="!assistantStore.canSwitchSessionModel"
          >
            <template #reference>
              <button
                type="button"
                class="assistant-model-trigger"
                :disabled="!assistantStore.canSwitchSessionModel"
                :title="assistantStore.canSwitchSessionModel ? '切换本会话模型' : 'Agent 运行中或等待审批，处理完成后才能切换模型'"
              >
              <span>{{ assistantStore.currentSessionModel.provider }}</span>
              <span>{{ assistantStore.currentSessionModel.model }}</span>
              <span v-if="assistantStore.currentSessionModel.context_window_label && assistantStore.currentSessionModel.context_window_label !== '-'">{{ assistantStore.currentSessionModel.context_window_label }}</span>
              <span>{{ assistantStore.currentSessionModel.thinking_level }}</span>
                <el-icon><ArrowDown /></el-icon>
              </button>
            </template>
            <div class="assistant-model-popover">
              <el-segmented v-model="selectedThinkingLevel" :options="thinkingLevelOptions" size="small" />
              <div class="assistant-model-list">
                <button
                  v-for="option in assistantStore.modelSwitchOptions"
                  :key="option.key"
                  type="button"
                  class="assistant-model-option"
                  :disabled="assistantStore.switchingModel"
                  @click="handleSessionModelSwitch(option)"
                >
                <strong>{{ option.provider_label }}</strong>
                <span>{{ option.model_label }}</span>
                <span v-if="option.context_window_label && option.context_window_label !== '-'">Ctx {{ option.context_window_label }}</span>
                </button>
                <div v-if="!assistantStore.modelSwitchOptions.length" class="assistant-empty">暂无可用模型</div>
              </div>
            </div>
          </el-popover>
        </div>
        <p v-if="assistantStore.error" class="assistant-error">{{ assistantStore.error }}</p>
      </section>
    </transition>

    <button type="button" class="assistant-fab" :class="{ active: open }" @pointerdown="startAssistantDrag" @click="togglePanel">
      <el-icon><ChatDotRound /></el-icon>
    </button>
    <el-dialog v-model="htmlPreviewDialogOpen" title="HTML 预览" width="760px" append-to-body>
      <iframe
        class="assistant-html-preview-frame"
        sandbox="allow-popups allow-popups-to-escape-sandbox"
        :srcdoc="htmlPreviewDocument"
        title="HTML preview"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { usePageAiAssistantStore } from '@/stores/pageAiAssistant'
import { usePageAiContext } from '@/composables/usePageAiContext'
import { useStickyScroll } from '@/composables/useStickyScroll'
import { getPageAssistantArtifact, listPageAssistantRunEvents } from '@/api/pageAiAssistant'
import RuntimeTrace from '@/components/ai-runtime/RuntimeTrace.vue'
import RuntimeSessionQueue from '@/components/ai-runtime/RuntimeSessionQueue.vue'
import { actionInputPreview, actionMeta, actionSummary, actionTitle } from '@/components/ai-runtime/actionDisplay'
import {
  assistantFailureDetails,
  entryRuntimeRunId,
  isAssistantFailureEntry,
  precedingUserPrompt,
  runtimeEventsWithFailureTerminal
} from '@/components/ai-runtime/assistantFailure'
import { buildHtmlPreviewDocument, contentBlocksFromEntry, renderMarkdownToHtml } from '@/components/ai-runtime/messageContent'
import {
  agentRunMetricsForEntry,
  displayRuntimeEventsForEntry,
  isActionAwaitingDecision,
  isApprovalDecisionButtonSelected
} from '@/stores/agentChatboxState'
import { ArrowDown, ChatDotRound, CircleCloseFilled, Close, Position, Refresh, View } from '@element-plus/icons-vue'

const userStore = useUserStore()
const assistantStore = usePageAiAssistantStore()
const { contextRef } = usePageAiContext()
const open = ref(false)
const draft = ref('')
const steerInstructions = ref({})
const scrollBox = ref(null)
const {
  captureStickyScrollState,
  restoreStickyScrollPosition,
  requestScrollToBottom
} = useStickyScroll(scrollBox)
const htmlPreviewDialogOpen = ref(false)
const htmlPreviewDocument = ref('')

function submitSteer(agentAction) {
  const instruction = String(steerInstructions.value[agentAction.id] || '').trim()
  if (!instruction) return
  void assistantStore.steerAction(agentAction, instruction)
}
const modelPopoverOpen = ref(false)
const selectedThinkingLevel = ref('medium')
const thinkingLevelOptions = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh']
const ASSISTANT_POSITION_KEY = 'page_ai_assistant_position'
const FAB_SIZE = 52
const EDGE_GAP = 12
const DRAG_THRESHOLD = 4
const MESSAGE_TIMESTAMP_FORMAT = 'YYYY/MM/DD HH:mm:ss'
const assistantPosition = ref(readAssistantPosition())
const suppressNextToggle = ref(false)
const dragState = reactive({
  active: false,
  moved: false,
  pointerId: null,
  startX: 0,
  startY: 0,
  originX: 0,
  originY: 0
})

const assistantContext = computed(() => ({
  ...contextRef.value,
  workspace_id: userStore.currentWorkspaceId || ''
}))

const inputDisabled = computed(() => assistantStore.sending || assistantStore.loading)
const failureActionDisabled = computed(() => assistantStore.sending || assistantStore.loading)
const canSend = computed(() => {
  if (!draft.value.trim() || inputDisabled.value) return false
  if (assistantStore.queueMode === 'steer' && !assistantStore.hasActiveRun) return false
  return true
})
const assistantPositionStyle = computed(() => ({
  left: `${assistantPosition.value.x}px`,
  top: `${assistantPosition.value.y}px`
}))
const assistantPanelClass = computed(() => ({
  'assistant-panel--right': assistantPosition.value.x > viewportWidth() / 2,
  'assistant-panel--bottom': assistantPosition.value.y > viewportHeight() / 2
}))

onMounted(() => {
  assistantPosition.value = clampAssistantPosition(assistantPosition.value)
  window.addEventListener('resize', handleViewportResize)
  assistantStore.loadModelCatalog().catch(() => {})
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleViewportResize)
  removeDragListeners()
})

watch(() => userStore.currentWorkspaceId, () => {
  assistantStore.reset()
})

watch(() => contextRef.value.route_path, () => {
  if (open.value) {
    requestScrollToBottom()
    assistantStore.ensureCurrentSession(assistantContext.value).catch(() => {})
  }
})

watch(() => assistantStore.entries.map((entry) => [
  entry.id || entry.idempotency_key,
  entry.updated_at,
  Array.isArray(entry.output?.runtime_events) ? entry.output.runtime_events.length : 0,
  String(entry.content || '').length,
  String(entry.output?.reasoning || '').length
].join(':')).join('|'), async () => {
  const scrollState = captureStickyScrollState()
  await nextTick()
  restoreStickyScrollPosition(scrollState)
})

async function togglePanel() {
  if (suppressNextToggle.value) {
    suppressNextToggle.value = false
    return
  }
  open.value = !open.value
  if (open.value) {
    requestScrollToBottom()
    await Promise.all([
      assistantStore.ensureCurrentSession(assistantContext.value).catch(() => {}),
      assistantStore.loadModelCatalog().catch(() => {})
    ])
    await nextTick()
    restoreStickyScrollPosition(captureStickyScrollState())
  }
}

async function handleSend() {
  if (!canSend.value) return
  const content = draft.value.trim()
  draft.value = ''
  requestScrollToBottom()
  await assistantStore.sendMessage(content, assistantContext.value, { mode: assistantStore.queueMode }).catch(() => {
    draft.value = content
  })
}

async function refreshRuntimeTrace(entry) {
  const runtimeRunId = entryRuntimeRunId(entry)
  if (!runtimeRunId) return
  await assistantStore.refreshRuntimeEvents(runtimeRunId).catch(() => {})
}

async function handleSessionModelSwitch(option) {
  await assistantStore.switchSessionModel(option, selectedThinkingLevel.value)
  modelPopoverOpen.value = false
  ElMessage.success('页面助手模型已切换')
}

function isFailureTerminalItem(item, entry) {
  if (!isAssistantFailureEntry(entry)) return false
  const details = assistantFailureDetails(entry)
  const terminalEvent = details.status === 'cancelled' ? 'run.cancelled' : 'run.failed'
  return String(item?.event || '') === terminalEvent
}

async function continueAfterFailure(entry) {
  if (!isAssistantFailureEntry(entry) || failureActionDisabled.value) return
  await assistantStore.sendMessage('继续', assistantContext.value, { mode: 'follow_up' }).catch(() => {})
}

function canRetryFailedEntry(entry) {
  const prompt = precedingUserPrompt(assistantStore.entries, entry)
  return Boolean(prompt.content)
}

async function retryFailedEntry(entry) {
  if (!isAssistantFailureEntry(entry) || failureActionDisabled.value) return
  const prompt = precedingUserPrompt(assistantStore.entries, entry)
  if (!prompt.content) return
  requestScrollToBottom()
  await assistantStore.sendMessage(prompt.content, assistantContext.value, {
    mode: 'follow_up',
    attachments: prompt.attachments
  }).catch(() => {})
}

function messageHeaderText(entry) {
  if (entry?.role === 'assistant') return 'AI'
  if (entry?.role === 'tool') return 'Tool'
  const timestamp = formatEntryTimestamp(entry)
  return timestamp ? `${formatEntryTimestamp(entry)} 你` : '你'
}

function formatEntryTimestamp(entry) {
  const value = entry?.created_at || entry?.createdAt || entry?.timestamp
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (number) => String(number).padStart(2, '0')
  return MESSAGE_TIMESTAMP_FORMAT
    .replace('YYYY', String(date.getFullYear()))
    .replace('MM', pad(date.getMonth() + 1))
    .replace('DD', pad(date.getDate()))
    .replace('HH', pad(date.getHours()))
    .replace('mm', pad(date.getMinutes()))
    .replace('ss', pad(date.getSeconds()))
}

function assistantReasoningText(entry) {
  if (entry?.role !== 'assistant') return ''
  const output = entry.output || {}
  const message = output.message || {}
  const reasoning = firstReasoningText(
    output.reasoning,
    message.reasoning,
    message.reasoning_content,
    message.thinking
  )
  if (reasoning) return reasoning
  if (Array.isArray(message.reasoning_details)) {
    return message.reasoning_details
      .map((detail) => firstReasoningText(detail?.text, detail?.reasoning, detail?.content))
      .filter(Boolean)
      .join('\n')
      .trim()
  }
  return ''
}

function assistantAnswerText(entry) {
  if (entry?.role !== 'assistant') return ''
  return String(entry.content || entry.output?.text || '').trim()
}

function isCompletedAssistantEntry(entry) {
  if (entry?.role !== 'assistant') return false
  return entry.status === 'completed' || Boolean(assistantAnswerText(entry) && entry.status !== 'streaming')
}

function showFinalAnswer(entry) {
  if (!messageContentBlocks(entry).length) return false
  if (assistantRuntimeEvents(entry, assistantStore.entries).length || assistantReasoningText(entry)) {
    return false
  }
  if (entry?.status === 'streaming') return true
  return isCompletedAssistantEntry(entry)
}

function runtimeTraceAnswer(entry) {
  // Avoid reinjecting cumulative entry.content into the process timeline when
  // runtime events already contain the streamed answer sections.
  if (showFinalAnswer(entry)) return ''
  if (assistantRuntimeEvents(entry, assistantStore.entries).length) return ''
  return assistantAnswerText(entry)
}

function runtimeTraceLimit(entry) {
  if (entry?.status === 'streaming') return 100
  return 500
}

function messageContentBlocks(entry) {
  return contentBlocksFromEntry(entry).filter((block) => block.text)
}

function renderMessageMarkdown(value) {
  return renderMarkdownToHtml(value)
}

function openHtmlPreview(block) {
  htmlPreviewDocument.value = buildHtmlPreviewDocument(block?.text || '')
  htmlPreviewDialogOpen.value = true
}

function assistantStatusText(entry) {
  if (entry?.role !== 'assistant') return ''
  if (entry.status === 'streaming') {
    const phase = latestStreamingPhase(entry)
    if (phase === 'reasoning') return '思考中'
    if (phase === 'answer') return '生成回答中'
    if (phase === 'tool') return '调用工具中'
    if (phase === 'approval') return '等待确认'
    if (assistantReasoningText(entry) && !entry.content) return '思考中'
    if (entry.content) return '生成回答中'
    return '运行中'
  }
  if (entry.status === 'failed') return '生成失败'
  if (entry.status === 'cancelled') return '已停止'
  return ''
}

function latestStreamingPhase(entry) {
  const events = Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : []
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const name = String(events[index]?.type || events[index]?.event || '')
    if (name === 'permission.asked') return 'approval'
    if (name.startsWith('session.tool.') || name === 'model.tool_call_detected') return 'tool'
    if (name.startsWith('session.text.') || name === 'answer_delta') return 'answer'
    if (name.startsWith('session.reasoning.') || name === 'reasoning_delta') return 'reasoning'
    if (name === 'session.step.started') return 'reasoning'
  }
  return ''
}

function assistantPhaseTimings(entry) {
  const timings = entry?.output?.timings || {}
  const items = [
    ['accepted', '接收'],
    ['profile_resolved', '配置'],
    ['first_reasoning', '首个思考'],
    ['first_answer', '首字'],
    ['completed', '完成'],
    ['total_ms', '总计']
  ]
  return items
    .map(([key, label]) => {
      const rawValue = timings[key] ?? timings[`${key}_ms`]
      const value = Number(rawValue)
      if (!Number.isFinite(value)) return null
      return { key, label, value: formatDuration(value) }
    })
    .filter(Boolean)
}

function assistantRuntimeEventsWithPending(entry) {
  const events = Array.isArray(entry?.output?.runtime_events) ? entry.output.runtime_events : []
  if (entry?.role !== 'assistant') return events
  const existingActionIds = new Set(events.flatMap(runtimeEventActionIds))
  const runtimeRunId = firstNonEmptyString(entry.runtime_run_id, entry.output?.runtime_run_id)
  const syntheticApprovalEvents = assistantStore.pendingActions
    .filter((action) => pendingActionBelongsToEntry(action, runtimeRunId))
    .filter((action) => pendingActionIds(action).every((id) => !existingActionIds.has(id)))
    .map((action) => pendingActionToRuntimeEvent(action, entry))
  return syntheticApprovalEvents.length ? [...events, ...syntheticApprovalEvents] : events
}

function assistantRuntimeEvents(entry, allEntries = []) {
  if (entry?.role !== 'assistant') return assistantRuntimeEventsWithPending(entry)
  const output = {
    ...(entry.output || {}),
    runtime_events: assistantRuntimeEventsWithPending(entry)
  }
  const enrichedEntry = { ...entry, output }
  const enrichedEntries = allEntries.map((item) => sameEntry(item, entry) ? enrichedEntry : item)
  return runtimeEventsWithFailureTerminal(
    enrichedEntry,
    displayRuntimeEventsForEntry(enrichedEntry, enrichedEntries)
  )
}

function assistantStructuredOutput(entry) {
  const output = entry?.output?.structured_output
  if (!output || typeof output !== 'object' || Array.isArray(output) || Object.keys(output).length === 0) return ''
  return JSON.stringify(output, null, 2)
}

function rawDetailsText(entry) {
  const parts = []
  const structured = assistantStructuredOutput(entry)
  if (structured) {
    parts.push(`结构化结果\n${structured}`)
  }
  const events = assistantRuntimeEvents(entry, assistantStore.entries)
    .map((event) => event?.event || event?.type || event?.payload?.event_type)
    .filter(Boolean)
  if (events.length) {
    parts.push(`runtime events\n${events.join('\n')}`)
  }
  return parts.join('\n\n')
}

function assistantAgentRunMetrics(entry) {
  return agentRunMetricsForEntry(entry, assistantStore.entries)
}

function approvalActionTooltip(agentAction) {
  return [
    actionTitle(agentAction),
    actionSummary(agentAction),
    actionMeta(agentAction),
    actionInputPreview(agentAction)
  ].filter(Boolean).join('\n')
}

function approvalButtonDisabled(agentAction) {
  return !isActionAwaitingDecision(agentAction) || assistantStore.isActionDecisionPending(agentAction.id)
}

function approvalButtonSelected(agentAction, decision) {
  return isApprovalDecisionButtonSelected(agentAction, decision)
}

function sameEntry(entry, target) {
  if (!entry || !target) return false
  if (entry.id != null && target.id != null && String(entry.id) === String(target.id)) return true
  if (entry.idempotency_key && target.idempotency_key && entry.idempotency_key === target.idempotency_key) return true
  return false
}

function formatDuration(value) {
  if (value < 1000) return `${Math.max(0, Math.round(value))}ms`
  return `${(value / 1000).toFixed(1)}s`
}

function firstReasoningText(...values) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function firstNonEmptyString(...values) {
  for (const value of values) {
    if (value !== undefined && value !== null && String(value).trim()) return String(value).trim()
  }
  return ''
}

function runtimeEventActionIds(event = {}) {
  const data = event.data || event.payload || event
  const action = data.action || {}
  const approvalRequest = data.approval_request || data.display_json?.approval_request || action.display_json?.approval_request || {}
  return [
    data.action_id,
    data.request_id,
    data.approval_id,
    data.call_id,
    data.provider_tool_call_id,
    approvalRequest.action_id,
    approvalRequest.approval_id,
    action.id,
    action.action_id,
    action.input_json?.provider_tool_call_id
  ].map((value) => firstNonEmptyString(value)).filter(Boolean)
}

function pendingActionIds(action = {}) {
  return [
    action.id,
    action.action_id,
    action.display_json?.approval_request?.approval_id,
    action.display_json?.approval_request?.action_id,
    action.input_json?.provider_tool_call_id
  ].map((value) => firstNonEmptyString(value)).filter(Boolean)
}

function pendingActionBelongsToEntry(action = {}, runtimeRunId = '') {
  const actionRuntimeRunId = firstNonEmptyString(action.runtime_run_id)
  if (!runtimeRunId || !actionRuntimeRunId) return true
  return actionRuntimeRunId === runtimeRunId
}

function pendingActionToRuntimeEvent(action = {}, entry = {}) {
  const eventName = action.pi_approval ? 'permission.asked' : 'action.decision_required'
  const approvalRequest = action.display_json?.approval_request || {}
  const payload = action.pi_approval
    ? {
        event_id: `pending:${action.id || action.action_id}`,
        request_id: action.id || action.action_id,
        approval_id: approvalRequest.approval_id || action.action_id || action.id,
        call_id: action.input_json?.provider_tool_call_id,
        tool_name: action.input_json?.tool_name || action.capability_id,
        reason: action.policy_json?.risk_summary || action.display_json?.summary || approvalRequest.reason || '',
        input: action.input_json?.arguments || {},
        display_json: action.display_json || {}
      }
    : {
        event_id: `pending:${action.id || action.action_id}`,
        action_id: action.action_id || action.id,
        action,
        display_json: action.display_json || {}
      }
  return {
    type: eventName,
    event: eventName,
    event_id: payload.event_id,
    payload,
    data: payload,
    display_json: action.display_json || {},
    timestamp: entry.updated_at || entry.created_at
  }
}

function startAssistantDrag(event) {
  if (event.button !== undefined && event.button !== 0) return
  dragState.active = true
  dragState.moved = false
  dragState.pointerId = event.pointerId
  dragState.startX = event.clientX
  dragState.startY = event.clientY
  dragState.originX = assistantPosition.value.x
  dragState.originY = assistantPosition.value.y
  event.currentTarget?.setPointerCapture?.(event.pointerId)
  window.addEventListener('pointermove', moveAssistant)
  window.addEventListener('pointerup', stopAssistantDrag)
  window.addEventListener('pointercancel', stopAssistantDrag)
}

function moveAssistant(event) {
  if (!dragState.active) return
  const deltaX = event.clientX - dragState.startX
  const deltaY = event.clientY - dragState.startY
  if (Math.hypot(deltaX, deltaY) >= DRAG_THRESHOLD) {
    dragState.moved = true
  }
  assistantPosition.value = clampAssistantPosition({
    x: dragState.originX + deltaX,
    y: dragState.originY + deltaY
  })
}

function stopAssistantDrag() {
  if (!dragState.active) return
  dragState.active = false
  removeDragListeners()
  if (dragState.moved) {
    suppressNextToggle.value = true
    persistAssistantPosition()
    window.setTimeout(() => {
      suppressNextToggle.value = false
    }, 0)
  }
}

function removeDragListeners() {
  window.removeEventListener('pointermove', moveAssistant)
  window.removeEventListener('pointerup', stopAssistantDrag)
  window.removeEventListener('pointercancel', stopAssistantDrag)
}

function handleViewportResize() {
  assistantPosition.value = clampAssistantPosition(assistantPosition.value)
  persistAssistantPosition()
}

function readAssistantPosition() {
  const fallback = defaultAssistantPosition()
  if (typeof window === 'undefined') return fallback
  try {
    const saved = JSON.parse(localStorage.getItem(ASSISTANT_POSITION_KEY) || 'null')
    if (Number.isFinite(saved?.x) && Number.isFinite(saved?.y)) {
      return clampAssistantPosition({ x: saved.x, y: saved.y })
    }
  } catch {
    localStorage.removeItem(ASSISTANT_POSITION_KEY)
  }
  return fallback
}

function persistAssistantPosition() {
  if (typeof window === 'undefined') return
  localStorage.setItem(ASSISTANT_POSITION_KEY, JSON.stringify(assistantPosition.value))
}

function defaultAssistantPosition() {
  return {
    x: viewportWidth() - FAB_SIZE - 24,
    y: viewportHeight() - FAB_SIZE - 24
  }
}

function clampAssistantPosition(position) {
  return {
    x: clampNumber(position.x, EDGE_GAP, viewportWidth() - FAB_SIZE - EDGE_GAP),
    y: clampNumber(position.y, EDGE_GAP, viewportHeight() - FAB_SIZE - EDGE_GAP)
  }
}

function clampNumber(value, min, max) {
  const safeMax = Math.max(min, max)
  if (!Number.isFinite(value)) return min
  return Math.min(Math.max(value, min), safeMax)
}

function viewportWidth() {
  return typeof window === 'undefined' ? 1280 : window.innerWidth
}

function viewportHeight() {
  return typeof window === 'undefined' ? 800 : window.innerHeight
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.page-assistant {
  position: fixed;
  z-index: 60;
  width: 52px;
  height: 52px;
}

.assistant-fab,
.assistant-icon-button,
.assistant-send {
  border: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.assistant-fab {
  width: 52px;
  height: 52px;
  border-radius: 18px;
  color: #fff;
  background: linear-gradient(135deg, #0f766e, #2563eb);
  box-shadow: 0 18px 42px rgba(15, 118, 110, 0.28);
  transition: transform $transition-fast, box-shadow $transition-fast;
  touch-action: none;

  &:hover,
  &.active {
    transform: translateY(-2px);
    box-shadow: 0 22px 52px rgba(37, 99, 235, 0.3);
  }
}

.assistant-panel {
  position: absolute;
  left: 0;
  top: 68px;
  width: min(520px, calc(100vw - 32px));
  height: min(620px, calc(100vh - 132px));
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-xl;
  background: var(--bg-elevated);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

.assistant-panel--right {
  left: auto;
  right: 0;
}

.assistant-panel--bottom {
  top: auto;
  bottom: 68px;
}

.assistant-header {
  height: 58px;
  padding: 0 14px 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--border-color-light);
  cursor: grab;
  user-select: none;
  touch-action: none;

  div {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  strong {
    color: var(--text-primary);
    font-size: 15px;
  }

  span {
    color: var(--text-tertiary);
    font-size: 12px;
  }
}

.assistant-icon-button {
  width: 32px;
  height: 32px;
  border-radius: $radius-md;
  background: var(--bg-card);
  color: var(--text-secondary);
}

.assistant-messages {
  flex: 1;
  min-height: 0;
  padding: 14px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.assistant-empty {
  margin: auto;
  color: var(--text-tertiary);
  font-size: 13px;
}

.assistant-message {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-width: 90%;

  span {
    color: var(--text-tertiary);
    font-size: 12px;
  }

  .assistant-content {
    margin: 0;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--bg-card);
    color: var(--text-primary);
    font-size: 14px;
    line-height: 1.55;
    overflow-wrap: anywhere;
  }
}

.assistant-content--text {
  white-space: pre-wrap;
}

.assistant-content--markdown {
  :deep(p) {
    margin: 0 0 8px;

    &:last-child {
      margin-bottom: 0;
    }
  }

  :deep(ul),
  :deep(ol) {
    margin: 6px 0;
    padding-left: 20px;
  }

  :deep(pre) {
    margin: 8px 0;
    padding: 8px;
    border-radius: 6px;
    background: color-mix(in srgb, var(--bg-card) 78%, #111827);
    overflow-x: auto;
  }

  :deep(code) {
    padding: 1px 4px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--bg-card) 80%, #111827);
  }
}

.assistant-content--html-preview {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;

  > div {
    min-width: 0;
    display: grid;
    gap: 2px;
  }

  strong {
    color: var(--text-primary);
    font-size: 13px;
  }
}

.assistant-message--user {
  align-self: flex-end;
  align-items: flex-end;

  .assistant-content {
    color: #fff;
    background: #2563eb;
  }
}

.assistant-message--tool {
  align-self: stretch;
  max-width: 100%;

  .assistant-content {
    border: 1px solid var(--border-color-light);
    background: color-mix(in srgb, var(--bg-card) 88%, #0f766e);
    color: var(--text-secondary);
    font-size: 12px;
  }
}

.assistant-status {
  color: var(--text-tertiary);
  font-size: 12px;
}

.assistant-message__meta-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;

  > div {
    min-width: 0;
    display: grid;
    gap: 6px;
  }
}

.assistant-stage-timings {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;

  span {
    padding: 3px 7px;
    border-radius: 999px;
    background: var(--bg-card);
    color: var(--text-tertiary);
    font-size: 11px;
    line-height: 1.4;
  }
}

.assistant-runtime,
.assistant-structured {
  border: 1px solid var(--border-color-light);
  border-radius: 10px;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 12px;
  overflow: hidden;

  summary {
    padding: 7px 10px;
    cursor: pointer;
    user-select: none;
    font-weight: 600;
  }

  ol {
    margin: 0;
    padding: 0 10px 10px 24px;
  }

  li {
    margin: 3px 0;
    line-height: 1.45;
  }

  strong {
    margin-right: 6px;
    color: var(--text-secondary);
  }

  pre {
    margin: 0;
    padding: 0 10px 10px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-family: inherit;
    line-height: 1.55;
  }
}

.assistant-approval-button {
  min-width: 72px;
}

.assistant-approval-button.is-selected {
  font-weight: 600;
}

.assistant-approval-button.is-selected.is-disabled {
  opacity: 0.86;
}

.assistant-approval-steer {
  display: grid;
  grid-template-columns: minmax(140px, 1fr) auto;
  gap: 6px;
  width: 100%;
  margin-top: 6px;
}

.assistant-terminal-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.assistant-agent-footer {
  display: grid;
  gap: 8px;
}

.assistant-agent-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;

  span {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 7px;
    border: 1px solid var(--border-color-light);
    border-radius: 999px;
    background: var(--bg-card);
    color: var(--text-tertiary);
    font-size: 11px;
    line-height: 1.3;
  }

  strong {
    color: var(--text-secondary);
    font-weight: 600;
  }
}

.assistant-input-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 42px 42px;
  gap: 8px;
  padding: 12px 12px 8px;
  border-top: 1px solid var(--border-color-light);
}

.assistant-queue-panel {
  margin: 0 12px 8px;
}

.assistant-send,
.assistant-stop {
  width: 42px;
  height: 42px;
  border-radius: 14px;
  color: #fff;
}

.assistant-send {
  background: #0f766e;

  &:disabled {
    opacity: 0.42;
    cursor: not-allowed;
  }
}

.assistant-stop {
  background: #9f1239;
}

.assistant-error {
  margin: 0;
  padding: 0 12px 12px;
  color: $danger-color;
  font-size: 12px;
}

.assistant-model-row {
  display: flex;
  align-items: center;
  padding: 0 12px 10px;
}

.assistant-model-trigger {
  max-width: 260px;
  min-width: 0;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--border-color-light);
  border-radius: 999px;
  background: var(--bg-card);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;

  span {
    min-width: 0;
    max-width: 90px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
  }

  &:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
}

.assistant-model-popover {
  display: grid;
  gap: 10px;
}

.assistant-model-list {
  max-height: 260px;
  overflow-y: auto;
  display: grid;
  gap: 6px;
}

.assistant-model-option {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  background: var(--bg-card);
  color: var(--text-secondary);
  display: grid;
  gap: 2px;
  text-align: left;
  cursor: pointer;

  strong {
    color: var(--text-primary);
    font-size: 12px;
  }

  span {
    color: var(--text-tertiary);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  &:hover {
    border-color: color-mix(in srgb, var(--primary-color) 48%, var(--border-color-light));
  }
}

.assistant-html-preview-frame {
  width: 100%;
  min-height: 520px;
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  background: #fff;
}

.assistant-panel-enter-active,
.assistant-panel-leave-active {
  transition: opacity $transition-base, transform $transition-base;
}

.assistant-panel-enter-from,
.assistant-panel-leave-to {
  opacity: 0;
  transform: translateY(12px) scale(0.98);
}

@media (max-width: 640px) {
  .page-assistant {
    right: 16px;
    bottom: 16px;
  }

  .assistant-panel {
    top: 64px;
  }

  .assistant-panel--bottom {
    top: auto;
    bottom: 64px;
  }
}
</style>
