<template>
  <div class="agent-chatbox-shell">
    <aside class="agent-chatbox-sidebar">
      <section class="chatbox-profile-panel">
        <div class="chatbox-profile-panel__title">
          <strong>{{ currentProfileName }}</strong>
          <el-tag size="small" :type="profileStatusType" effect="plain">{{ currentProfileStatus }}</el-tag>
        </div>
        <div class="chatbox-profile-meta">
          <span>Profile #{{ chatboxStore.session?.agent_profile_id || '-' }}</span>
          <span>{{ profileModelText }}</span>
          <span>{{ profileVersionText }}</span>
        </div>
        <div v-if="currentContextTags.length" class="chatbox-context-tags">
          <el-tag v-for="tag in currentContextTags" :key="tag" size="small" effect="plain">{{ tag }}</el-tag>
        </div>
      </section>

      <div class="chatbox-sidebar-actions">
        <el-button size="small" :icon="Plus" @click="createNewSession">新建会话</el-button>
        <el-button size="small" :icon="Refresh" @click="loadSessionFromRoute">刷新</el-button>
      </div>

      <nav class="chatbox-session-list" aria-label="Agent Chatbox sessions">
        <button
          v-for="item in chatboxStore.sessions"
          :key="item.id"
          type="button"
          class="chatbox-session-item"
          :class="{ active: String(item.id) === String(chatboxStore.session?.id) }"
          @click="switchSession(item.id)"
        >
          <span>{{ item.title || `Session #${item.id}` }}</span>
          <small>{{ sessionListMeta(item) }}</small>
        </button>
        <div v-if="!chatboxStore.sessions.length" class="chatbox-empty-state">暂无历史会话</div>
      </nav>
    </aside>

    <main class="agent-chatbox-workspace">
      <header class="chatbox-topbar">
        <div>
          <h1>{{ currentSessionTitle }}</h1>
          <p>{{ profileModelText }} · {{ profileVersionText }}</p>
        </div>
        <div class="chatbox-topbar-actions">
          <el-tag v-if="chatboxStore.sending || chatboxStore.hasActiveRun" size="small" :type="connectionStatusType" effect="plain">{{ connectionStatusText }}</el-tag>
          <el-button :icon="CopyDocument" @click="copySessionUrl">复制链接</el-button>
          <el-button :icon="Delete" :disabled="!chatboxStore.session?.id || chatboxStore.sending || chatboxStore.hasActiveRun" @click="archiveCurrentSession">归档</el-button>
          <el-button v-if="chatboxStore.sending || chatboxStore.hasActiveRun" type="danger" :icon="Close" :loading="chatboxStore.stopping" @click="chatboxStore.stopGeneration">停止</el-button>
        </div>
      </header>

      <section ref="scrollBox" class="chatbox-message-list thread">
        <div v-if="chatboxStore.loading" class="chatbox-empty-state">会话加载中</div>
        <div v-else-if="visibleEntries.length === 0" class="chatbox-empty-state">开始一轮 Agent Profile 对话</div>
        <article
          v-for="entry in visibleEntries"
          :key="entry.id || entry.idempotency_key"
          class="turn"
          :class="entry.role"
        >
          <div class="turn-head chatbox-message__header">
            <strong>{{ messageHeaderText(entry) }}</strong>
            <span>{{ formatEntryTimestamp(entry) }}</span>
          </div>

          <div v-if="entry.role === 'user'" class="user-card">
            <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-user-${index}`">
              <div v-if="block.kind === 'markdown'" class="chatbox-content chatbox-content--markdown" v-html="renderMessageMarkdown(block.text)" />
              <div v-else class="chatbox-content chatbox-content--text">{{ block.text }}</div>
            </template>
          </div>

          <div v-else-if="entry.role === 'assistant'" class="agent-card">
            <div class="chatbox-message__body">
              <div v-if="!isAssistantFailureEntry(entry) && (chatboxStatusText(entry) || chatboxPhaseTimings(entry).length || entryRuntimeRunId(entry))" class="chatbox-message__meta-row">
                <span v-if="chatboxStatusText(entry)" class="chatbox-status">{{ chatboxStatusText(entry) }}</span>
                <div v-if="chatboxPhaseTimings(entry).length" class="chatbox-stage-timings">
                  <span v-for="item in chatboxPhaseTimings(entry)" :key="item.key">{{ item.label }} {{ item.value }}</span>
                </div>
                <el-button
                  v-if="entryRuntimeRunId(entry)"
                  size="small"
                  text
                  :icon="Refresh"
                  :loading="chatboxStore.isRuntimeEventsRefreshing(entryRuntimeRunId(entry))"
                  @click="refreshRuntimeTrace(entry)"
                >
                  刷新轨迹
                </el-button>
              </div>
              <AssistantFailureCard
                v-if="isAssistantFailureEntry(entry)"
                :entry="entry"
                :refreshing="chatboxStore.isRuntimeEventsRefreshing(entryRuntimeRunId(entry))"
                :can-continue="canContinueAfterFailure"
                :can-retry="canRetryFailedEntry(entry)"
                :continuing="chatboxStore.sending"
                @refresh="refreshRuntimeTrace(entry)"
                @continue="continueAfterFailure"
                @retry="retryFailedEntry(entry)"
              />
              <div
                v-if="isAssistantFailureEntry(entry) && hasAssistantPartialContent(entry)"
                class="chatbox-partial-answer"
              >
                <div class="chatbox-partial-answer__label">已生成内容（未完成）</div>
                <div v-if="assistantPartialContent(entry).reasoning" class="chatbox-partial-answer__reasoning">
                  {{ assistantPartialContent(entry).reasoning }}
                </div>
                <div v-if="assistantPartialContent(entry).answer" class="chatbox-partial-answer__body">
                  <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-partial-${index}`">
                    <div
                      v-if="block.kind === 'markdown'"
                      class="chatbox-content chatbox-content--markdown"
                      v-html="renderMessageMarkdown(block.text)"
                    />
                    <div v-else class="chatbox-content chatbox-content--text">{{ block.text }}</div>
                  </template>
                </div>
              </div>
              <RuntimeTrace
                ref="runtimeTraceRefs"
                v-if="chatboxRuntimeEvents(entry, chatboxStore.entries).length || chatboxReasoningText(entry)"
                :events="chatboxRuntimeEvents(entry, chatboxStore.entries)"
                :reasoning="chatboxReasoningText(entry)"
                :answer="runtimeTraceAnswer(entry)"
                :timings="entry.output?.timings || {}"
                :limit="runtimeTraceLimit(entry)"
                :load-artifact="getAgentChatboxArtifact"
                :load-run-events="listAgentChatboxRunEvents"
                :pending-actions="chatboxStore.pendingActions"
              >
                <template #approval-actions="{ agentAction }">
                  <el-button
                    class="chatbox-approval-button"
                    :class="{ 'is-selected': approvalButtonSelected(agentAction, 'approve_once') }"
                    size="small"
                    type="primary"
                    :plain="!approvalButtonSelected(agentAction, 'approve_once')"
                    :title="approvalActionTooltip(agentAction)"
                    :disabled="approvalButtonDisabled(agentAction)"
                    @click="chatboxStore.approveAction(agentAction)"
                  >
                    批准一次
                  </el-button>
                  <el-button
                    class="chatbox-approval-button"
                    :class="{ 'is-selected': approvalButtonSelected(agentAction, 'approve_session') }"
                    size="small"
                    :text="!approvalButtonSelected(agentAction, 'approve_session')"
                    :type="approvalButtonSelected(agentAction, 'approve_session') ? 'primary' : ''"
                    :title="approvalActionTooltip(agentAction)"
                    :disabled="approvalButtonDisabled(agentAction)"
                    @click="chatboxStore.approveActionForSession(agentAction)"
                  >
                    本会话批准
                  </el-button>
                  <el-button
                    class="chatbox-approval-button"
                    :class="{ 'is-selected': approvalButtonSelected(agentAction, 'reject') }"
                    size="small"
                    :text="!approvalButtonSelected(agentAction, 'reject')"
                    type="danger"
                    :title="approvalActionTooltip(agentAction)"
                    :disabled="approvalButtonDisabled(agentAction)"
                    @click="chatboxStore.rejectAction(agentAction)"
                  >
                    拒绝
                  </el-button>
                  <div v-if="agentAction.pi_approval" class="chatbox-approval-steer">
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
              </RuntimeTrace>
              <div v-if="showFinalAnswer(entry)" class="final-answer">
                <div class="chatbox-answer">
                  <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-content-${index}`">
                    <div
                      v-if="block.kind === 'markdown'"
                      class="chatbox-content chatbox-content--markdown"
                      v-html="renderMessageMarkdown(block.text)"
                    />
                    <div v-else-if="block.kind === 'html'" class="chatbox-content chatbox-content--html-preview">
                      <div>
                        <strong>{{ block.label || 'HTML' }}</strong>
                        <span>{{ block.text.length }} 字符</span>
                      </div>
                      <el-button size="small" text :icon="View" @click="openHtmlPreview(block)">
                        预览 HTML
                      </el-button>
                    </div>
                    <div v-else class="chatbox-content chatbox-content--text">{{ block.text }}</div>
                  </template>
                </div>
              </div>
              <div v-if="rawDetailsText(entry) || chatboxAgentRunMetrics(entry).length" class="chatbox-agent-footer">
                <details v-if="rawDetailsText(entry)" class="raw" :open="entry.output?.output_schema_valid === false">
                  <summary>诊断详情</summary>
                  <pre>{{ rawDetailsText(entry) }}</pre>
                </details>
                <div v-if="chatboxAgentRunMetrics(entry).length" class="chatbox-agent-metrics" aria-label="Agent run metrics">
                  <span v-for="metric in chatboxAgentRunMetrics(entry)" :key="metric.key">
                    <strong>{{ metric.label }}</strong>{{ metric.value }}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <div v-else class="tool-card">
            <div class="chatbox-message__body">
              <div v-if="messageContentBlocks(entry).length" class="chatbox-answer">
                <template v-for="(block, index) in messageContentBlocks(entry)" :key="`${entry.id || entry.idempotency_key}-tool-${index}`">
                  <div v-if="block.kind === 'markdown'" class="chatbox-content chatbox-content--markdown" v-html="renderMessageMarkdown(block.text)" />
                  <div v-else class="chatbox-content chatbox-content--text">{{ block.text }}</div>
                </template>
              </div>
            </div>
          </div>
        </article>
      </section>

      <RuntimeSessionQueue
        v-if="chatboxStore.queueItems.length"
        class="chatbox-queue-panel"
        :items="chatboxStore.queueItems"
        :loading="chatboxStore.queueLoading"
        @cancel="chatboxStore.cancelQueueItem"
      />
      <form class="chatbox-input-row" @submit.prevent="handleSend">
        <el-input
          v-model="draft"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 8 }"
          placeholder="输入消息"
          :disabled="inputDisabled"
          @keydown.enter.exact.prevent="handleSend"
        />
        <el-select
          v-model="chatboxStore.queueMode"
          class="chatbox-queue-mode"
          size="default"
          :disabled="inputDisabled"
        >
          <el-option label="继续跟进" value="follow_up" />
          <el-option label="注入当前" value="steer" :disabled="!chatboxStore.hasActiveRun" />
          <el-option label="停止并重发" value="stop_and_run" />
        </el-select>
        <el-button v-if="chatboxStore.sending" type="danger" :icon="CircleCloseFilled" :loading="chatboxStore.stopping" @click="chatboxStore.stopGeneration" />
        <el-button type="primary" :icon="Position" :disabled="!canSend" native-type="submit" />
      </form>
      <div class="chatbox-model-row">
        <el-popover
          v-model:visible="modelPopoverOpen"
          placement="top-start"
          width="440"
          trigger="click"
          :disabled="!chatboxStore.canSwitchSessionModel"
        >
          <template #reference>
            <button
              type="button"
              class="chatbox-model-trigger"
              :disabled="!chatboxStore.canSwitchSessionModel"
              :title="chatboxStore.canSwitchSessionModel ? '切换本会话模型' : 'Agent 运行中，停止或等待输入后才能切换模型'"
            >
              <span>{{ chatboxStore.currentSessionModel.provider }}</span>
              <span>{{ chatboxStore.currentSessionModel.model }}</span>
              <span>{{ chatboxStore.currentSessionModel.thinking_level }}</span>
              <el-icon><ArrowDown /></el-icon>
            </button>
          </template>
          <div class="chatbox-model-popover">
            <el-segmented v-model="selectedThinkingLevel" :options="thinkingLevelOptions" size="small" />
            <div class="chatbox-model-list">
              <button
                v-for="option in chatboxStore.modelSwitchOptions"
                :key="option.key"
                type="button"
                class="chatbox-model-option"
                :disabled="chatboxStore.switchingModel"
                @click="handleSessionModelSwitch(option)"
              >
                <strong>{{ option.provider_label }}</strong>
                <span>{{ option.model_label }}</span>
              </button>
              <div v-if="!chatboxStore.modelSwitchOptions.length" class="chatbox-empty-state">暂无可用模型</div>
            </div>
          </div>
        </el-popover>
      </div>
      <p v-if="chatboxStore.error" class="chatbox-error">{{ chatboxStore.error }}</p>
    </main>

    <el-dialog v-model="htmlPreviewDialogOpen" title="HTML 预览" width="860px" append-to-body>
      <iframe
        class="chatbox-html-preview-frame"
        sandbox="allow-popups allow-popups-to-escape-sandbox"
        :srcdoc="htmlPreviewDocument"
        title="HTML preview"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowDown, CircleCloseFilled, Close, CopyDocument, Delete, Plus, Position, Refresh, View } from '@element-plus/icons-vue'
import { useAgentChatboxStore } from '@/stores/agentChatbox'
import { getAgentChatboxArtifact, listAgentChatboxRunEvents } from '@/api/agentChatbox'
import { useStickyScroll } from '@/composables/useStickyScroll'
import RuntimeTrace from '@/components/ai-runtime/RuntimeTrace.vue'
import RuntimeSessionQueue from '@/components/ai-runtime/RuntimeSessionQueue.vue'
import AssistantFailureCard from '@/components/ai-runtime/AssistantFailureCard.vue'
import { actionInputPreview, actionMeta, actionSummary, actionTitle } from '@/components/ai-runtime/actionDisplay'
import {
  assistantFailureDetails,
  assistantPartialContent,
  entryRuntimeRunId,
  hasAssistantPartialContent,
  isAssistantFailureEntry,
  precedingUserPrompt
} from '@/components/ai-runtime/assistantFailure'
import { buildHtmlPreviewDocument, contentBlocksFromEntry, renderMarkdownToHtml } from '@/components/ai-runtime/messageContent'
import {
  agentRunMetricsForEntry,
  displayRuntimeEventsForEntry,
  isActionAwaitingDecision,
  isApprovalDecisionButtonSelected
} from '@/stores/agentChatboxState'

const route = useRoute()
const router = useRouter()
const chatboxStore = useAgentChatboxStore()
const draft = ref('')
const steerInstructions = ref({})
const scrollBox = ref(null)
const {
  captureStickyScrollState,
  restoreStickyScrollPosition,
  requestScrollToBottom
} = useStickyScroll(scrollBox)
const runtimeTraceRefs = ref([])
const htmlPreviewDialogOpen = ref(false)
const htmlPreviewDocument = ref('')
const modelPopoverOpen = ref(false)
const selectedThinkingLevel = ref('medium')
const thinkingLevelOptions = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh']

const currentProfileName = computed(() => {
  return chatboxStore.currentProfile?.name || chatboxStore.session?.title || `Agent #${chatboxStore.session?.agent_profile_id || '-'}`
})
const currentSessionTitle = computed(() => {
  return chatboxStore.session?.title || chatboxStore.currentProfile?.name || `Agent #${chatboxStore.session?.agent_profile_id || '-'}`
})
const currentProfileStatus = computed(() => chatboxStore.currentProfile?.status || chatboxStore.session?.status || '-')
const profileStatusType = computed(() => {
  if (currentProfileStatus.value === 'active') return 'success'
  if (currentProfileStatus.value === 'draft') return 'warning'
  return 'info'
})
const profileModelText = computed(() => {
  const profile = chatboxStore.currentProfile
  return profile?.model?.provider_model_key || profile?.model?.model_id || 'model -'
})
const profileVersionText = computed(() => `version ${chatboxStore.session?.agent_profile_version_key || chatboxStore.session?.agent_profile_version_id || 'latest'}`)
const currentContextTags = computed(() => {
  return profileContextTags(chatboxStore.currentProfile || chatboxStore.session)
})
const sessionUrl = computed(() => {
  if (typeof window === 'undefined') return ''
  return `${window.location.origin}/store/ai-agents/chat/${chatboxStore.session?.id || route.params.session_id || ''}`
})
const inputDisabled = computed(() => chatboxStore.sending || chatboxStore.loading || !chatboxStore.session?.id)
const canContinueAfterFailure = computed(() => Boolean(chatboxStore.session?.id) && !chatboxStore.sending && !chatboxStore.loading && !chatboxStore.hasActiveRun)
const canSend = computed(() => {
  if (!draft.value.trim() || inputDisabled.value) return false
  if (chatboxStore.queueMode === 'steer' && !chatboxStore.hasActiveRun) return false
  return true
})
const connectionStatusText = computed(() => ({
  connecting: '连接中',
  live: '实时',
  reconnecting: `重连中 ${chatboxStore.reconnectAttempt}`,
  stale: '连接中断',
  terminal: '已结束'
}[chatboxStore.connectionState] || '连接中'))
const connectionStatusType = computed(() => ({
  live: 'success',
  stale: 'danger',
  reconnecting: 'warning'
}[chatboxStore.connectionState] || 'info'))

function submitSteer(agentAction) {
  const instruction = String(steerInstructions.value[agentAction.id] || '').trim()
  if (!instruction) return
  void chatboxStore.steerAction(agentAction, instruction)
}
const visibleEntries = computed(() => {
  return chatboxStore.entries.filter((entry) => {
    if (entry?.role === 'assistant') return true
    return entry?.role === 'user' && (entry.entry_type || 'message') === 'message'
  })
})

onMounted(() => {
  loadSessionFromRoute()
  chatboxStore.loadModelCatalog().catch(() => {})
})

onBeforeUnmount(() => {
  chatboxStore.disconnectStream()
})

watch(() => route.params.session_id, () => {
  loadSessionFromRoute()
})

watch(() => route.params.child_run_id, () => {
  openRouteChildThread()
})

watch(() => chatboxStore.entries.map((entry) => [
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

async function loadSessionFromRoute() {
  const sessionId = route.params.session_id
  if (!sessionId) return
  requestScrollToBottom()
  await chatboxStore.loadSession(sessionId).catch(() => {})
  await nextTick()
  restoreStickyScrollPosition(captureStickyScrollState())
  await openRouteChildThread()
}

async function switchSession(sessionId) {
  if (!sessionId || String(sessionId) === String(route.params.session_id)) return
  await router.push(`/store/ai-agents/chat/${sessionId}`)
}

async function createNewSession() {
  const nextSession = await chatboxStore.createNewSession().catch(() => null)
  if (nextSession?.id) {
    await router.push(`/store/ai-agents/chat/${nextSession.id}`)
  }
}

async function archiveCurrentSession() {
  const currentId = chatboxStore.session?.id
  if (!currentId || chatboxStore.sending || chatboxStore.hasActiveRun) return
  const archived = await chatboxStore.archiveSession(currentId).catch(() => null)
  if (!archived) return
  ElMessage.success('会话已归档')
  const next = chatboxStore.sessions[0]
  if (next?.id) {
    await router.push(`/store/ai-agents/chat/${next.id}`)
    return
  }
  const created = await chatboxStore.createNewSession().catch(() => null)
  if (created?.id) {
    await router.push(`/store/ai-agents/chat/${created.id}`)
  }
}

async function handleSend() {
  if (!canSend.value) return
  const content = draft.value.trim()
  draft.value = ''
  requestScrollToBottom()
  await chatboxStore.sendMessage(content, { mode: chatboxStore.queueMode }).catch(() => {
    draft.value = content
  })
}

async function handleSessionModelSwitch(option) {
  await chatboxStore.switchSessionModel(option, selectedThinkingLevel.value)
  modelPopoverOpen.value = false
  ElMessage.success('会话模型已切换')
}

async function openRouteChildThread() {
  const childRunId = String(route.params.child_run_id || '').trim()
  if (!childRunId) return
  await nextTick()
  const traceRefs = Array.isArray(runtimeTraceRefs.value) ? runtimeTraceRefs.value : [runtimeTraceRefs.value]
  const trace = traceRefs.find((item) => item?.openChildRunByRuntimeId)
  await trace?.openChildRunByRuntimeId(childRunId, {
    parent_entry_id: activeAssistantEntryId(),
    parent_runtime_run_id: activeParentRuntimeRunId()
  })
}

function activeAssistantEntryId() {
  const assistantEntry = [...chatboxStore.entries].reverse().find((entry) => entry?.role === 'assistant')
  return assistantEntry?.id || ''
}

function activeParentRuntimeRunId() {
  const assistantEntry = [...chatboxStore.entries].reverse().find((entry) => entry?.role === 'assistant' && entry?.runtime_run_id)
  return assistantEntry?.runtime_run_id || ''
}

async function copySessionUrl() {
  try {
    await navigator.clipboard?.writeText(sessionUrl.value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}

async function refreshRuntimeTrace(entry) {
  const runtimeRunId = entryRuntimeRunId(entry)
  if (!runtimeRunId) return
  await chatboxStore.refreshRuntimeEvents(runtimeRunId).catch(() => {})
}

async function continueAfterFailure() {
  await chatboxStore.continueSession().catch(() => {})
}

function canRetryFailedEntry(entry) {
  if (!canContinueAfterFailure.value) return false
  const details = assistantFailureDetails(entry)
  if (!details.retryable) return false
  const prompt = precedingUserPrompt(chatboxStore.entries, entry)
  return Boolean(prompt.content)
}

async function retryFailedEntry(entry) {
  const prompt = precedingUserPrompt(chatboxStore.entries, entry)
  if (!prompt.content) {
    await continueAfterFailure()
    return
  }
  await chatboxStore.sendMessage(prompt.content, {
    mode: chatboxStore.queueMode,
    attachments: prompt.attachments
  }).catch(() => {})
}

function messageHeaderText(entry) {
  if (entry?.role === 'assistant') return 'AI'
  if (entry?.role === 'tool') return 'Tool'
  if (entry?.role === 'user') return '你'
  return entry?.role || 'system'
}

function formatEntryTimestamp(entry) {
  const value = entry?.created_at || entry?.createdAt || entry?.timestamp
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

function chatboxReasoningText(entry) {
  if (entry?.role !== 'assistant') return ''
  const output = entry.output || {}
  const message = output.message || {}
  const reasoning = firstReasoningText(output.reasoning, message.reasoning, message.reasoning_content, message.thinking)
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

function chatboxAnswerText(entry) {
  if (entry?.role !== 'assistant') return ''
  return String(entry.content || entry.output?.text || '').trim()
}

function isCompletedAssistantEntry(entry) {
  if (entry?.role !== 'assistant') return false
  if (isAssistantFailureEntry(entry)) return false
  return entry.status === 'completed' || Boolean(chatboxAnswerText(entry) && entry.status !== 'streaming')
}

function showFinalAnswer(entry) {
  // Failures render partial content in a dedicated partition, not final-answer.
  if (isAssistantFailureEntry(entry)) return false
  if (!messageContentBlocks(entry).length) return false
  if (entry?.status === 'streaming') {
    return !chatboxRuntimeEvents(entry, chatboxStore.entries).length && !chatboxReasoningText(entry)
  }
  return isCompletedAssistantEntry(entry)
}

function runtimeTraceAnswer(entry) {
  // Never reinject entry.content into the live process timeline. Multi-step runs
  // already carry session.text.* events; passing the cumulative answer makes the
  // previous conclusion reappear inside the newest thought bubble.
  if (showFinalAnswer(entry)) return ''
  if (chatboxRuntimeEvents(entry, chatboxStore.entries).length) return ''
  return chatboxAnswerText(entry)
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

function chatboxStatusText(entry) {
  if (entry?.role !== 'assistant') return ''
  if (entry.status === 'streaming') {
    const phase = latestStreamingPhase(entry)
    if (phase === 'reasoning') return '思考中'
    if (phase === 'answer') return '生成回答中'
    if (phase === 'tool') return '调用工具中'
    if (phase === 'approval') return '等待确认'
    if (chatboxReasoningText(entry) && !entry.content) return '思考中'
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

function chatboxPhaseTimings(entry) {
  const timings = entry?.output?.timings || {}
  return [
    ['accepted', '接收'],
    ['profile_resolved', '配置'],
    ['first_reasoning', '首个思考'],
    ['first_answer', '首字'],
    ['completed', '完成'],
    ['total_ms', '总计']
  ].map(([key, label]) => {
    const rawValue = timings[key] ?? timings[`${key}_ms`]
    const value = Number(rawValue)
    if (!Number.isFinite(value)) return null
    return { key, label, value: formatDuration(value) }
  }).filter(Boolean)
}

function chatboxRuntimeEvents(entry, allEntries = []) {
  return displayRuntimeEventsForEntry(entry, allEntries)
}

function chatboxStructuredOutput(entry) {
  const output = entry?.output?.structured_output
  if (!output || typeof output !== 'object' || Array.isArray(output) || Object.keys(output).length === 0) return ''
  return JSON.stringify(output, null, 2)
}

function rawDetailsText(entry) {
  const parts = []
  const structured = chatboxStructuredOutput(entry)
  if (structured) {
    parts.push(`结构化结果\n${structured}`)
  }
  const events = chatboxRuntimeEvents(entry, chatboxStore.entries)
    .map((event) => event?.event || event?.type || event?.payload?.event_type)
    .filter(Boolean)
  if (events.length) {
    parts.push(`runtime events\n${events.join('\n')}`)
  }
  return parts.join('\n\n')
}

function chatboxAgentRunMetrics(entry) {
  return agentRunMetricsForEntry(entry, chatboxStore.entries)
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
  return !isActionAwaitingDecision(agentAction) || chatboxStore.isActionDecisionPending(agentAction.id)
}

function approvalButtonSelected(agentAction, decision) {
  return isApprovalDecisionButtonSelected(agentAction, decision)
}

function sessionListMeta(item) {
  const parts = [item.status || 'active', item.entry_count ? `${item.entry_count} entries` : '0 entries']
  return parts.join(' · ')
}

function profileContextTags(profile) {
  const sourceProfile = profile || {}
  return normalizeStringArray(sourceProfile.context_tags)
}

function normalizeStringArray(value) {
  if (Array.isArray(value)) {
    return [...new Set(value.map((item) => String(item).trim()).filter(Boolean))]
  }
  if (typeof value === 'string') {
    return [...new Set(value.split(',').map((item) => item.trim()).filter(Boolean))]
  }
  return []
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

</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.agent-chatbox-shell {
  height: calc(100vh - 92px);
  min-height: 640px;
  display: grid;
  grid-template-columns: 276px minmax(0, 1fr);
  background: var(--surface-base);
}

.agent-chatbox-sidebar,
.agent-chatbox-workspace {
  min-height: 0;
  overflow: hidden;
}

.agent-chatbox-sidebar {
  border-right: 1px solid var(--border-color-light);
  background: var(--surface-sidebar);
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  gap: 14px;
  padding: 14px 12px;
}

.chatbox-profile-panel {
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  padding: 10px;
  background: var(--surface-overlay);
  display: grid;
  gap: 5px;
}

.chatbox-profile-panel__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;

  strong {
    min-width: 0;
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
}

.chatbox-profile-meta {
  display: grid;
  gap: 3px;
  color: var(--text-tertiary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.chatbox-context-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.chatbox-sidebar-actions,
.chatbox-topbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.chatbox-session-list {
  min-height: 0;
  overflow-y: auto;
  display: grid;
  align-content: start;
  gap: 7px;
}

.chatbox-session-item {
  width: 100%;
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  padding: 9px 10px;
  background: var(--surface-overlay);
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  display: grid;
  gap: 3px;

  span {
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 650;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  small {
    color: var(--text-tertiary);
    font-size: 11px;
  }

  &.active {
    border-color: var(--border-color-hover);
    background: var(--primary-lighter);
    box-shadow: inset 3px 0 0 var(--primary-color);
  }
}

.agent-chatbox-workspace {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto auto;
}

.chatbox-topbar {
  min-height: 58px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border-color-light);
  background: var(--surface-overlay);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;

  h1 {
    margin: 0;
    color: var(--text-primary);
    font-size: 17px;
    line-height: 1.25;
    letter-spacing: 0;
  }

  p {
    margin: 2px 0 0;
    color: var(--text-tertiary);
    font-size: 12px;
  }
}

.chatbox-message-list {
  min-height: 0;
  padding: 18px 20px 24px;
  overflow-y: auto;
  display: grid;
  align-content: start;
  gap: 18px;
}

.chatbox-empty-state {
  margin: auto;
  color: var(--text-tertiary);
  font-size: 13px;
}

.turn {
  width: min(980px, 100%);
  display: grid;
  gap: 8px;
}

.turn.user {
  justify-self: end;
  width: min(720px, 92%);
}

.turn.assistant {
  justify-self: start;
}

.user-card,
.agent-card,
.tool-card {
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  background: var(--surface-overlay);
}

.user-card {
  padding: 11px 13px;
  background: var(--terminal-bg);
  border-color: var(--terminal-bg);
  color: var(--terminal-fg);

  .chatbox-content {
    color: var(--terminal-fg);
  }
}

.agent-card {
  padding: 16px;
  overflow: hidden;
}

.tool-card {
  padding: 12px;
}

.chatbox-message__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--text-tertiary);
  font-size: 12px;

  strong {
    color: inherit;
  }
}

.chatbox-message__body {
  min-width: 0;
  display: grid;
  gap: 12px;
}

.chatbox-answer {
  min-width: 0;
  display: grid;
  gap: 8px;
}

.chatbox-content {
  margin: 0;
  color: var(--text-primary);
  font-family: inherit;
  font-size: 14px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.chatbox-content--text {
  white-space: pre-wrap;
}

.chatbox-content--markdown {
  white-space: normal;

  :deep(h1),
  :deep(h2),
  :deep(h3),
  :deep(h4),
  :deep(h5),
  :deep(h6) {
    margin: 0.45em 0 0.28em;
    color: var(--text-primary);
    font-size: 15px;
    line-height: 1.35;
    letter-spacing: 0;
  }

  :deep(h1:first-child),
  :deep(h2:first-child),
  :deep(h3:first-child),
  :deep(p:first-child),
  :deep(ul:first-child),
  :deep(ol:first-child),
  :deep(blockquote:first-child),
  :deep(pre:first-child) {
    margin-top: 0;
  }

  :deep(p) {
    margin: 0.35em 0;
  }

  :deep(ul),
  :deep(ol) {
    margin: 0.4em 0;
    padding-left: 20px;
  }

  :deep(li + li) {
    margin-top: 2px;
  }

  :deep(blockquote) {
    margin: 0.45em 0;
    padding-left: 10px;
    border-left: 2px solid var(--border-color);
    color: var(--text-secondary);
  }

  :deep(code) {
    padding: 1px 4px;
    border-radius: 4px;
    background: var(--surface-muted);
    font-family: 'JetBrains Mono', Consolas, monospace;
    font-size: 12px;
  }

  :deep(pre) {
    margin: 0.5em 0;
    padding: 9px 10px;
    overflow: auto;
    border-radius: $radius-md;
    background: var(--terminal-bg);
    color: var(--terminal-fg);
    font-size: 12px;
    line-height: 1.5;
  }

  :deep(pre code) {
    padding: 0;
    background: transparent;
    color: inherit;
  }

  :deep(table) {
    display: block;
    width: 100%;
    max-width: 100%;
    margin: 0.55em 0;
    overflow-x: auto;
    border-collapse: collapse;
    font-size: 13px;
    line-height: 1.45;
  }

  :deep(th),
  :deep(td) {
    padding: 6px 8px;
    border: 1px solid var(--border-color-light);
    text-align: left;
    vertical-align: top;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  :deep(th) {
    background: var(--surface-muted);
    color: var(--text-primary);
    font-weight: 650;
  }

  :deep(td) {
    color: var(--text-secondary);
  }

  :deep(a) {
    color: var(--primary-color);
    text-decoration: none;
  }
}

.chatbox-content--html-preview {
  padding: 8px 10px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--surface-muted);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  white-space: normal;

  div {
    min-width: 0;
    display: grid;
    gap: 2px;
  }

  strong {
    color: var(--text-primary);
    font-size: 13px;
  }

  span {
    color: var(--text-tertiary);
    font-size: 11px;
  }
}

.chatbox-html-preview-frame {
  width: 100%;
  height: min(70vh, 620px);
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--surface-overlay);
}

.final-answer {
  color: var(--text-primary);
  font-size: 14px;
  line-height: 1.6;
}

.chatbox-partial-answer {
  display: grid;
  gap: 8px;
  padding: 10px 12px;
  border: 1px dashed color-mix(in srgb, var(--warning-color, #c9892d) 40%, var(--border-color-light));
  border-radius: var(--border-radius-base, 8px);
  background: color-mix(in srgb, var(--warning-color, #c9892d) 8%, var(--surface-overlay, transparent));
}

.chatbox-partial-answer__label {
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
}

.chatbox-partial-answer__reasoning {
  color: var(--text-secondary);
  font-size: 13px;
  white-space: pre-wrap;
  line-height: 1.5;
}

.chatbox-partial-answer__body {
  color: var(--text-primary);
  font-size: 14px;
  line-height: 1.6;
}

details.raw {
  margin-top: 2px;

  summary {
    cursor: pointer;
    color: var(--text-tertiary);
    font-size: 12px;
    list-style: none;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    user-select: none;
  }

  summary::before {
    content: '>';
    font-size: 10px;
    transition: transform 0.2s;
  }

  &[open] summary::before {
    transform: rotate(90deg);
  }

  summary::-webkit-details-marker {
    display: none;
  }

  pre {
    max-height: 170px;
    margin: 7px 0 0;
    padding: 9px;
    overflow: auto;
    border: 1px solid var(--border-color-medium);
    border-radius: 7px;
    background: var(--terminal-bg-alt);
    color: var(--terminal-fg);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-family: 'JetBrains Mono', Consolas, monospace;
    font-size: 12px;
    line-height: 1.45;
  }
}

.chatbox-agent-footer {
  min-width: 0;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.chatbox-agent-metrics {
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 5px;
  flex-wrap: wrap;

  span {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    min-height: 22px;
    padding: 2px 7px;
    border-radius: 999px;
    background: var(--surface-muted);
    color: var(--text-tertiary);
    font-size: 10px;
    line-height: 1.2;
    white-space: nowrap;
  }

  strong {
    color: var(--text-secondary);
    font-weight: 600;
  }
}

.chatbox-message__meta-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}

.chatbox-status {
  color: var(--text-tertiary);
  font-size: 12px;
}

.chatbox-stage-timings {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;

  span {
    padding: 2px 6px;
    border-radius: 999px;
    background: var(--surface-muted);
    color: var(--text-tertiary);
    font-size: 10px;
  }
}

.chatbox-queue-panel {
  margin: 0 18px 8px;
}

.chatbox-input-row {
  padding: 12px 18px;
  border-top: 1px solid var(--border-color-light);
  background: var(--surface-overlay);
  display: grid;
  grid-template-columns: minmax(0, 1fr) 132px 44px 44px;
  gap: 8px;
  align-items: end;
}

.chatbox-queue-mode {
  width: 132px;
}

.chatbox-model-row {
  padding: 0 18px 12px;
  background: var(--surface-overlay);
  display: flex;
  align-items: center;
  gap: 8px;
}

.chatbox-model-trigger {
  width: min(320px, 100%);
  max-width: 320px;
  min-height: 28px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  display: grid;
  grid-template-columns: minmax(60px, auto) minmax(0, 1fr) auto 14px;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  font-size: 11px;
  text-align: left;
  cursor: pointer;
}

.chatbox-model-trigger:hover:not(:disabled) {
  border-color: var(--border-color-light);
  background: var(--surface-muted);
}

.chatbox-model-trigger:disabled {
  cursor: not-allowed;
  opacity: 0.58;
}

.chatbox-model-trigger span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chatbox-approval-button {
  min-height: 24px;
  padding: 3px 8px;
  font-size: 12px;
}

.chatbox-approval-button.is-selected {
  font-weight: 600;
}

.chatbox-approval-button.is-selected.is-disabled {
  opacity: 0.86;
}

.chatbox-approval-steer {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto;
  gap: 6px;
  width: min(100%, 520px);
  margin-top: 6px;
}

.chatbox-model-popover {
  display: grid;
  gap: 10px;
}

.chatbox-model-list {
  max-height: 280px;
  overflow: auto;
  display: grid;
  gap: 6px;
}

.chatbox-model-option {
  border: 1px solid var(--border-color-light);
  border-radius: 6px;
  background: var(--surface-card);
  color: var(--text-primary);
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr);
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  text-align: left;
  cursor: pointer;
}

.chatbox-model-option:hover {
  border-color: var(--primary-color);
}

.chatbox-model-option span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chatbox-error {
  margin: 0;
  padding: 0 14px 12px;
  color: var(--danger-color);
  font-size: 12px;
}

@media (max-width: 900px) {
  .agent-chatbox-shell {
    height: auto;
    min-height: calc(100vh - 84px);
    grid-template-columns: 1fr;
  }

  .agent-chatbox-sidebar {
    max-height: 260px;
    border-right: 0;
    border-bottom: 1px solid var(--border-color-light);
  }

  .agent-chatbox-workspace {
    min-height: 620px;
  }

}

@media (max-width: 640px) {
  .chatbox-topbar,
  .chatbox-input-row {
    grid-template-columns: 1fr;
    align-items: stretch;
  }

  .turn.user,
  .turn {
    width: 100%;
  }
}
</style>
