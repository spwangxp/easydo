<template>
  <section v-if="processItems.length" class="ai-runtime-trace">
    <ol class="ai-runtime-events process-timeline" aria-label="Agent process timeline">
      <li
        v-for="item in processItems"
        :key="item.key"
        class="ai-runtime-event-line event-item"
        :class="[
          item.lineKind,
          item.status,
          `ai-runtime-event-line--${item.lineKind}`,
          `ai-runtime-event-line--${item.status}`
        ]"
      >
        <div class="ai-runtime-event-line__rail event-dot" aria-hidden="true"></div>
        <div class="ai-runtime-event-line__content event-content">
          <div class="ai-runtime-event-line__header event-header">
            <span class="ai-runtime-event-line__meta event-meta">{{ item.lineMeta }}</span>
            <strong v-if="item.lineTitle" class="ai-runtime-event-line__title event-title-text">{{ item.lineTitle }}</strong>
            <code v-if="item.lineTarget" class="ai-runtime-event-line__target">{{ item.lineTarget }}</code>
          </div>
          <div v-if="item.sections?.length" class="ai-runtime-event-line__sections event-body">
            <section
              v-for="section in item.sections"
              :key="section.kind"
              class="ai-runtime-event-line__section"
              :class="`ai-runtime-event-line__section--${section.kind}`"
            >
              <span>{{ section.label }}</span>
              <div v-if="isMarkdownThoughtSection(section)" class="ai-runtime-event-line__section-markdown" v-html="renderThoughtSectionMarkdown(section)"></div>
              <p v-else>{{ section.text }}</p>
            </section>
          </div>
          <p v-else-if="item.lineSummary && !['action', 'subagent'].includes(item.lineKind)" class="ai-runtime-event-line__summary event-body">{{ item.lineSummary }}</p>

          <div v-if="item.inputPreviewText" class="ai-runtime-event-line__input event-body">
            <span>输入</span>
            <code>{{ item.inputPreviewText }}</code>
          </div>

          <div v-if="item.reasonText" class="ai-runtime-event-line__reason event-body">
            <span>调用理由</span>
            <p>{{ item.reasonText }}</p>
          </div>

          <section v-if="item.lineKind === 'subagent'" class="ai-runtime-subagent-card">
            <div class="ai-runtime-subagent-card__header">
              <div>
                <strong>{{ item.lineTitle || 'Subagent' }}</strong>
                <span>child thread {{ shortId(item.childRunLinkId || item.childRuntimeRunId) }}</span>
              </div>
              <el-tag size="small" effect="plain" :type="subagentStatusType(item)">
                {{ subagentStatusLabel(item) }}
              </el-tag>
            </div>
            <ol v-if="item.subagentProgress?.length" class="ai-runtime-subagent-card__progress">
              <li v-for="progress in item.subagentProgress" :key="progress">{{ progress }}</li>
            </ol>
            <p v-else-if="item.lineSummary" class="ai-runtime-subagent-card__summary">{{ item.lineSummary }}</p>
            <div v-if="approvalActionForItem(item)" class="approval-actions" @click.stop>
              <slot name="approval-actions" :item="item" :agent-action="approvalActionForItem(item)" />
            </div>
            <div class="ai-runtime-subagent-card__footer">
              <span v-if="item.status === 'waiting'">blocked_approval：请回到父会话处理确认。</span>
              <span v-else>进度已回挂到 parent action。</span>
              <el-button
                v-if="item.childRuntimeRunId && loadRunEvents"
                size="small"
                text
                :icon="Connection"
                @click.prevent="openChildRun(item)"
              >
                查看子线程
              </el-button>
            </div>
          </section>

          <div
            v-if="item.resultSummary && item.lineKind !== 'subagent'"
            class="ai-runtime-event-line__result"
            :class="[
              item.lineKind === 'subagent' ? 'subagent-result' : 'tool-result',
              `ai-runtime-event-line__result--${item.resultStatus || 'success'}`
            ]"
          >
            {{ item.resultSummary }}
          </div>

          <div v-if="item.lineKind === 'action' && item.lineSummary" class="ai-runtime-event-line__approval approval">
            <span>{{ item.lineSummary }}</span>
            <div v-if="approvalActionForItem(item)" class="approval-actions" @click.stop>
              <slot name="approval-actions" :item="item" :agent-action="approvalActionForItem(item)" />
            </div>
          </div>

          <div v-if="item.artifactRefs.length || (item.childRunLinkId && item.lineKind !== 'subagent')" class="ai-runtime-trace__actions" @click.stop>
            <el-button
              v-for="artifactRef in item.artifactRefs"
              :key="artifactRef.artifact_id"
              size="small"
              text
              :icon="Document"
              @click.prevent="openArtifact(artifactRef, item)"
            >
              {{ artifactButtonLabel(artifactRef) }}
            </el-button>
            <el-button
              v-if="item.childRuntimeRunId && loadRunEvents"
              size="small"
              text
              :icon="Connection"
              @click.prevent="openChildRun(item)"
            >
              子运行 {{ shortId(item.childRunLinkId || item.childRuntimeRunId) }}
            </el-button>
            <el-tag v-else-if="item.childRunLinkId" size="small" effect="plain" type="info">
              子运行 {{ shortId(item.childRunLinkId) }}
            </el-tag>
          </div>
        </div>
      </li>
    </ol>
  </section>

  <el-dialog v-model="artifactDialogOpen" title="运行产物" width="720px" append-to-body>
    <el-skeleton v-if="artifactLoading" :rows="5" animated />
    <el-alert v-else-if="artifactError" :title="artifactError" type="error" show-icon :closable="false" />
    <section v-else-if="artifactDetail" class="ai-runtime-artifact">
      <dl class="ai-runtime-artifact__meta">
        <div>
          <dt>ID</dt>
          <dd>{{ artifactDetail.artifact_id }}</dd>
        </div>
        <div>
          <dt>类型</dt>
          <dd>{{ artifactDetail.artifact_type || '-' }}</dd>
        </div>
        <div>
          <dt>可见性</dt>
          <dd>{{ artifactDetail.visibility || '-' }}</dd>
        </div>
        <div>
          <dt>大小</dt>
          <dd>{{ formatBytes(artifactDetail.size_bytes) }}</dd>
        </div>
      </dl>
      <pre>{{ artifactPreviewText }}</pre>
    </section>
  </el-dialog>

  <el-dialog v-model="childRunDialogOpen" title="子 Agent 事件时间线" width="860px" append-to-body>
    <el-skeleton v-if="childRunLoading" :rows="5" animated />
    <el-alert v-else-if="childRunError" :title="childRunError" type="error" show-icon :closable="false" />
    <section v-else class="ai-runtime-child-run">
      <p class="ai-runtime-child-run__id">{{ childRunRuntimeId }}</p>
      <p class="ai-runtime-child-run__parent">子 Agent 运行事件（工具调用、文本输出、审批与终态）。可继续在父会话查看汇总。</p>
      <ol class="ai-runtime-events ai-runtime-events--child">
        <li
          v-for="item in childRunProcessItems"
          :key="item.key"
          class="ai-runtime-event-line event-item"
          :class="[
            item.lineKind,
            item.status,
            `ai-runtime-event-line--${item.lineKind}`,
            `ai-runtime-event-line--${item.status}`
          ]"
        >
          <div class="ai-runtime-event-line__rail event-dot" aria-hidden="true"></div>
          <div class="ai-runtime-event-line__content event-content">
            <div class="ai-runtime-event-line__header event-header">
              <span class="ai-runtime-event-line__meta event-meta">{{ item.lineMeta }}</span>
              <strong v-if="item.lineTitle" class="ai-runtime-event-line__title event-title-text">{{ item.lineTitle }}</strong>
              <code v-if="item.lineTarget" class="ai-runtime-event-line__target">{{ item.lineTarget }}</code>
            </div>
            <div v-if="item.sections?.length" class="ai-runtime-event-line__sections event-body">
              <section
                v-for="section in item.sections"
                :key="section.kind"
                class="ai-runtime-event-line__section"
                :class="`ai-runtime-event-line__section--${section.kind}`"
              >
                <span>{{ section.label }}</span>
                <div v-if="isMarkdownThoughtSection(section)" class="ai-runtime-event-line__section-markdown" v-html="renderThoughtSectionMarkdown(section)"></div>
                <p v-else>{{ section.text }}</p>
              </section>
            </div>
            <p v-else-if="item.lineSummary && !['action', 'subagent'].includes(item.lineKind)" class="ai-runtime-event-line__summary event-body">{{ item.lineSummary }}</p>
            <div v-if="item.inputPreviewText" class="ai-runtime-event-line__input event-body">
              <span>输入</span>
              <code>{{ item.inputPreviewText }}</code>
            </div>
            <div v-if="item.reasonText" class="ai-runtime-event-line__reason event-body">
              <span>调用理由</span>
              <p>{{ item.reasonText }}</p>
            </div>
            <section v-if="item.lineKind === 'subagent'" class="ai-runtime-subagent-card">
              <div class="ai-runtime-subagent-card__header">
                <div>
                  <strong>{{ item.lineTitle || 'Subagent' }}</strong>
                  <span>child thread {{ shortId(item.childRunLinkId || item.childRuntimeRunId) }}</span>
                </div>
                <el-tag size="small" effect="plain" :type="subagentStatusType(item)">
                  {{ subagentStatusLabel(item) }}
                </el-tag>
              </div>
              <ol v-if="item.subagentProgress?.length" class="ai-runtime-subagent-card__progress">
                <li v-for="progress in item.subagentProgress" :key="progress">{{ progress }}</li>
              </ol>
              <p v-else-if="item.lineSummary" class="ai-runtime-subagent-card__summary">{{ item.lineSummary }}</p>
              <div v-if="approvalActionForItem(item)" class="approval-actions" @click.stop>
                <slot name="approval-actions" :item="item" :agent-action="approvalActionForItem(item)" />
              </div>
            </section>
            <div
              v-if="item.resultSummary && item.lineKind !== 'subagent'"
              class="ai-runtime-event-line__result"
              :class="[
                item.lineKind === 'subagent' ? 'subagent-result' : 'tool-result',
                `ai-runtime-event-line__result--${item.resultStatus || 'success'}`
              ]"
            >
              {{ item.resultSummary }}
            </div>
          </div>
        </li>
      </ol>
    </section>
  </el-dialog>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Connection, Document } from '@element-plus/icons-vue'
import {
  buildRuntimeProcessItems
} from './runtimeProcessEvents.js'
import { renderMarkdownToHtml } from './messageContent.js'

const props = defineProps({
  events: {
    type: Array,
    default: () => []
  },
  reasoning: {
    type: String,
    default: ''
  },
  answer: {
    type: String,
    default: ''
  },
  timings: {
    type: Object,
    default: () => ({})
  },
  limit: {
    type: Number,
    default: 100
  },
  loadArtifact: {
    type: Function,
    required: true
  },
  loadRunEvents: {
    type: Function,
    default: null
  },
  pendingActions: {
    type: Array,
    default: () => []
  }
})

const artifactDialogOpen = ref(false)
const artifactLoading = ref(false)
const artifactError = ref('')
const artifactDetail = ref(null)
const childRunDialogOpen = ref(false)
const childRunLoading = ref(false)
const childRunError = ref('')
const childRunEvents = ref([])
const childRunRuntimeId = ref('')
const currentTime = ref(Date.now())
let clockTimer = null

const processItems = computed(() => buildRuntimeProcessItems({
  events: props.events,
  reasoning: props.reasoning,
  answer: props.answer,
  timings: props.timings,
  limit: props.limit,
  now: currentTime.value
}))

const childRunProcessItems = computed(() => buildRuntimeProcessItems({
  events: childRunEvents.value,
  limit: props.limit,
  now: currentTime.value
}))

onMounted(() => {
  clockTimer = window.setInterval(() => {
    currentTime.value = Date.now()
  }, 1000)
})

onUnmounted(() => {
  if (clockTimer !== null) window.clearInterval(clockTimer)
  clockTimer = null
})

const loadRunEvents = computed(() => props.loadRunEvents)

const artifactPreviewText = computed(() => {
  const preview = artifactDetail.value?.preview_json
  if (!preview || typeof preview !== 'object') return ''
  return JSON.stringify(preview, null, 2)
})

async function openArtifact(artifactRef, item) {
  if (!artifactRef?.artifact_id) return
  const fallbackDetail = fallbackArtifactDetail(artifactRef)
  artifactDialogOpen.value = true
  artifactLoading.value = true
  artifactError.value = ''
  artifactDetail.value = null
  if (fallbackDetail && (firstString(artifactRef.artifact_id).startsWith('file:') || firstString(artifactRef.artifact_id).startsWith('patch:'))) {
    artifactDetail.value = fallbackDetail
    artifactLoading.value = false
    return
  }
  try {
    const response = await props.loadArtifact(artifactRef.artifact_id, artifactContext(artifactRef, item))
    artifactDetail.value = response?.data || response || fallbackDetail
  } catch (error) {
    if (fallbackDetail) {
      artifactDetail.value = fallbackDetail
    } else {
      artifactError.value = error instanceof Error ? error.message : '产物加载失败'
    }
  } finally {
    artifactLoading.value = false
  }
}

async function openChildRun(item) {
  if (!item.childRuntimeRunId || !props.loadRunEvents) return
  childRunDialogOpen.value = true
  childRunLoading.value = true
  childRunError.value = ''
  childRunEvents.value = []
  childRunRuntimeId.value = item.childRuntimeRunId
  try {
    const response = await props.loadRunEvents(item.childRuntimeRunId, '', childRunContext(item))
    const events = response?.data?.events || response?.events || []
    childRunEvents.value = Array.isArray(events) ? events : []
  } catch (error) {
    childRunError.value = error instanceof Error ? error.message : '子运行轨迹加载失败'
  } finally {
    childRunLoading.value = false
  }
}

async function openChildRunByRuntimeId(runtimeRunId, context = {}) {
  const childRuntimeRunId = firstString(runtimeRunId)
  if (!childRuntimeRunId || !props.loadRunEvents) return
  childRunDialogOpen.value = true
  childRunLoading.value = true
  childRunError.value = ''
  childRunEvents.value = []
  childRunRuntimeId.value = childRuntimeRunId
  try {
    const response = await props.loadRunEvents(childRuntimeRunId, '', context)
    const events = response?.data?.events || response?.events || []
    childRunEvents.value = Array.isArray(events) ? events : []
  } catch (error) {
    childRunError.value = error instanceof Error ? error.message : '子运行轨迹加载失败'
  } finally {
    childRunLoading.value = false
  }
}

function artifactContext(artifactRef, item) {
  return {
    child_run_link_id: firstString(
      artifactRef.child_run_link_id,
      artifactRef.preview_json?.child_run_link_id,
      item.data.child_run_link_id,
      item.display.child_run_link_id
    ),
    parent_runtime_run_id: firstString(
      artifactRef.parent_runtime_run_id,
      item.data.parent_runtime_run_id,
      item.data.runtime_run_id
    ),
    parent_action_id: firstString(
      artifactRef.parent_action_id,
      item.data.parent_action_id,
      item.data.action_id,
      item.display.action_id
    ),
    parent_entry_id: firstString(
      artifactRef.parent_entry_id,
      item.data.parent_entry_id,
      item.data.entry_id
    )
  }
}

function childRunContext(item) {
  return {
    child_run_link_id: firstString(
      item.childRunLinkId,
      item.data.child_run_link_id,
      item.display.child_run_link_id,
      item.artifactRefs[0]?.preview_json?.child_run_link_id
    ),
    parent_runtime_run_id: firstString(
      item.data.parent_runtime_run_id,
      item.data.runtime_run_id
    ),
    parent_action_id: firstString(
      item.data.parent_action_id,
      item.data.action_id,
      item.display.action_id
    ),
    parent_entry_id: firstString(
      item.data.parent_entry_id,
      item.data.entry_id
    )
  }
}

function firstString(...values) {
  for (const value of values) {
    if (value !== undefined && value !== null && String(value).trim()) return String(value).trim()
  }
  return ''
}

function shortId(value) {
  const text = firstString(value)
  if (text.length <= 18) return text || '-'
  return `${text.slice(0, 8)}...${text.slice(-6)}`
}

function isPatchArtifact(artifactRef) {
  const type = firstString(artifactRef?.artifact_type, artifactRef?.type).toLowerCase()
  if (['patch', 'diff', 'file_patch'].includes(type)) return true
  return Boolean(firstString(artifactRef?.preview_json?.patch_path, artifactRef?.patch_path))
}

function patchArtifactPath(artifactRef) {
  return firstString(
    artifactRef?.preview_json?.patch_path,
    artifactRef?.patch_path,
    artifactRef?.preview_json?.path,
    artifactRef?.path
  )
}

function artifactButtonLabel(artifactRef) {
  const filePath = firstString(artifactRef?.preview_json?.path, artifactRef?.path)
  if (artifactRef?.artifact_type === 'file_output' && filePath) return `文件 ${filePath}`
  if (isPatchArtifact(artifactRef)) {
    const patchPath = patchArtifactPath(artifactRef)
    if (patchPath) return `Patch ${patchPath}`
    return `Patch ${shortId(artifactRef?.artifact_id)}`
  }
  return `产物 ${shortId(artifactRef?.artifact_id)}`
}

function fallbackArtifactDetail(artifactRef) {
  const filePath = firstString(artifactRef?.preview_json?.path, artifactRef?.path)
  const preview = artifactRef?.preview_json && typeof artifactRef.preview_json === 'object' && !Array.isArray(artifactRef.preview_json)
    ? artifactRef.preview_json
    : {}
  if (isPatchArtifact(artifactRef)) {
    const patchPath = patchArtifactPath(artifactRef)
    if (!patchPath) return null
    return {
      artifact_id: firstString(artifactRef.artifact_id, `patch:${patchPath}`),
      artifact_type: 'patch',
      visibility: 'runtime_event',
      size_bytes: Number(artifactRef.size_bytes || 0),
      preview_json: {
        ...preview,
        patch_path: patchPath,
        path: patchPath,
        source: 'runtime_event'
      }
    }
  }
  if (artifactRef?.artifact_type !== 'file_output' || !filePath) return null
  return {
    artifact_id: firstString(artifactRef.artifact_id, `file:${filePath}`),
    artifact_type: 'file_output',
    visibility: 'runtime_event',
    size_bytes: Number(artifactRef.size_bytes || 0),
    preview_json: {
      ...preview,
      path: filePath,
      source: 'runtime_event'
    }
  }
}

function formatBytes(value) {
  const bytes = Number(value || 0)
  if (!Number.isFinite(bytes) || bytes <= 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function subagentStatusLabel(item) {
  if (item?.status === 'waiting') return 'blocked_approval'
  if (item?.status === 'success') return 'completed'
  if (item?.status === 'failed') return 'failed'
  if (item?.status === 'cancelled') return 'cancelled'
  if (item?.status === 'timeout') return 'timeout'
  if (item?.status === 'interrupted') return 'interrupted'
  return 'running'
}

function subagentStatusType(item) {
  if (item?.status === 'success') return 'success'
  if (item?.status === 'waiting') return 'warning'
  if (['failed', 'cancelled', 'timeout', 'interrupted'].includes(item?.status)) return 'danger'
  return 'info'
}

function renderThoughtSectionMarkdown(section) {
  return renderMarkdownToHtml(section?.text || '')
}

function isMarkdownThoughtSection(section) {
  return section?.kind === 'reasoning' || section?.kind === 'answer'
}

function pendingActionForItem(item) {
  const ids = new Set(itemActionIds(item))
  if (!ids.size) return null
  return props.pendingActions.find((action) => actionCandidateIds(action).some((id) => ids.has(id))) || null
}

function actionCandidateIds(action = {}) {
  const input = action?.input_json || {}
  const display = action?.display_json || {}
  const approvalRequest = display?.approval_request || {}
  return compactIds(
    action?.id,
    action?.action_id,
    input.provider_tool_call_id,
    input.tool_call_id,
    input.call_id,
    display.action_id,
    display.request_id,
    display.approval_id,
    display.provider_tool_call_id,
    display.tool_call_id,
    display.call_id,
    approvalRequest.action_id,
    approvalRequest.request_id,
    approvalRequest.approval_id,
    approvalRequest.provider_tool_call_id,
    approvalRequest.tool_call_id,
    approvalRequest.call_id
  )
}

function approvalActionForItem(item) {
  if (item?.status !== 'waiting') return null
  const pending = pendingActionForItem(item)
  if (pending) return pending
  if (item?.lineKind !== 'action') return null
  const id = itemActionId(item)
  if (!id) return null
  // Waiting rows must still expose Approve/Reject even when pendingActions is empty
  // (e.g. live permission.asked before runtime_run_id is attached to the draft entry).
  if (item.status === 'waiting') {
    const callId = firstString(
      item.data?.call_id,
      item.data?.tool_call_id,
      item.data?.provider_tool_call_id,
      item.display?.call_id,
      item.display?.provider_tool_call_id
    )
    const toolName = firstString(item.data?.tool_name, item.data?.tool, item.display?.name, item.lineTarget)
    return {
      id,
      action_id: id,
      action_kind: firstString(item.data?.action_kind, item.event) === 'permission.asked'
        ? 'pi.tool_approval'
        : firstString(item.data?.action_kind, item.data?.action?.action_kind, item.event),
      capability_id: toolName,
      runtime_run_id: firstString(item.data?.runtime_run_id, item.display?.runtime_run_id),
      input_json: {
        provider_tool_call_id: callId,
        tool_name: toolName,
        arguments: item.data?.input || item.data?.input_json || {}
      },
      target_json: item.data?.target_json || {},
      policy_json: {
        requires_decision: true,
        risk_summary: firstString(item.data?.reason, item.lineSummary)
      },
      display_json: {
        title: firstString(item.lineTitle, toolName ? `需要确认 ${toolName}` : '需要确认'),
        name: toolName,
        summary: firstString(item.data?.reason, item.lineSummary),
        approval_request: {
          approval_id: id,
          request_id: id,
          provider_tool_call_id: callId,
          call_id: callId,
          tool_name: toolName,
          reason: firstString(item.data?.reason, item.lineSummary)
        }
      },
      result_json: {},
      status: 'awaiting_decision',
      decision: '',
      pi_approval: item.event === 'permission.asked' || String(id).startsWith('approval:')
    }
  }
  return {
    id,
    action_id: id,
    action_kind: firstString(item.data?.action_kind, item.data?.action?.action_kind, item.event),
    capability_id: firstString(item.data?.capability_id, item.data?.tool_name, item.display?.name),
    runtime_run_id: firstString(item.data?.runtime_run_id, item.display?.runtime_run_id),
    input_json: item.data?.input_json || item.data?.action?.input_json || {},
    target_json: item.data?.target_json || item.data?.action?.target_json || {},
    policy_json: item.data?.policy_json || item.data?.action?.policy_json || {},
    display_json: item.display || item.data?.display_json || item.data?.action?.display_json || {},
    result_json: item.data?.result_json || item.data?.action?.result_json || {},
    status: item.status === 'success' ? 'approved' : item.status,
    decision: firstString(item.data?.decision, item.display?.decision, item.data?.result)
  }
}

function itemActionId(item) {
  return itemActionIds(item)[0] || ''
}

function itemActionIds(item) {
  const action = item?.data?.action || {}
  const approvalRequest = item?.data?.approval_request || item?.display?.approval_request || action.display_json?.approval_request || {}
  const awaitingApproval = item?.data?.awaiting_approval || item?.display?.awaiting_approval || {}
  return compactIds(
    item?.data?.action_id,
    item?.display?.action_id,
    action.action_id,
    action.id,
    item?.data?.request_id,
    item?.data?.approval_id,
    item?.display?.request_id,
    item?.display?.approval_id,
    approvalRequest.action_id,
    approvalRequest.request_id,
    approvalRequest.approval_id,
    approvalRequest.provider_tool_call_id,
    approvalRequest.tool_call_id,
    approvalRequest.call_id,
    awaitingApproval.request_id,
    awaitingApproval.approval_id,
    awaitingApproval.provider_tool_call_id,
    awaitingApproval.tool_call_id,
    awaitingApproval.call_id,
    item?.data?.provider_tool_call_id,
    item?.data?.tool_call_id,
    item?.data?.call_id
  )
}

function compactIds(...values) {
  return values.map((value) => firstString(value)).filter(Boolean)
}

defineExpose({
  openChildRunByRuntimeId
})
</script>

<style scoped>
.ai-runtime-trace {
  margin-bottom: 14px;
  --runtime-trace-bg: var(--surface-subtle);
  --runtime-trace-card-bg: var(--surface-overlay);
  --runtime-trace-rail-bg: var(--surface-subtle);
  --runtime-trace-muted: var(--text-tertiary);
  --runtime-trace-danger-bg: var(--status-danger-soft);
  --runtime-trace-approval-bg: var(--status-warning-soft);
  --runtime-trace-subagent-bg: var(--primary-lighter);
  --runtime-trace-subagent-border: var(--border-color-hover);
  --runtime-trace-action-color: var(--warning-color);
  --runtime-trace-thought-color: var(--info-color);
  --runtime-trace-subagent-color: var(--primary-color);
}

.ai-runtime-events {
  position: relative;
  margin: 0;
  padding: 16px 16px 16px 32px;
  list-style: none;
  border-radius: 8px;
  background: var(--runtime-trace-bg);
  font-size: 13px;
}

.ai-runtime-events::before {
  content: "";
  position: absolute;
  top: 20px;
  bottom: 20px;
  left: 16px;
  width: 2px;
  border-radius: 1px;
  background: var(--border-color-light);
}

.ai-runtime-events--child {
  margin-top: 0;
}

.ai-runtime-event-line {
  position: relative;
  min-width: 0;
  margin-bottom: 18px;
  padding: 0;
}

.ai-runtime-event-line:last-child {
  margin-bottom: 0;
}

.ai-runtime-event-line__rail {
  position: absolute;
  z-index: 1;
  left: -21px;
  top: 6px;
  width: 10px;
  height: 10px;
  border: 2px solid var(--runtime-trace-rail-bg);
  border-radius: 50%;
  background: var(--runtime-trace-muted);
}

.ai-runtime-event-line__content {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.ai-runtime-event-line__header {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 4px;
}

.ai-runtime-event-line__meta {
  color: var(--text-tertiary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 12px;
  font-weight: 650;
  line-height: 1.35;
  white-space: nowrap;
}

.ai-runtime-event-line__title {
  min-width: 0;
  color: var(--text-primary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 13px;
  font-weight: 500;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__target {
  min-width: 0;
  color: var(--text-tertiary);
  font-size: 11px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__summary {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__sections {
  display: grid;
  gap: 6px;
}

.ai-runtime-event-line__input,
.ai-runtime-event-line__reason {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
  padding: 6px 10px;
  border: 1px solid var(--border-color-light);
  border-radius: 6px;
  background: var(--runtime-trace-card-bg);
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.4;
}

.ai-runtime-event-line__input span,
.ai-runtime-event-line__reason span {
  flex: 0 0 auto;
  color: var(--text-tertiary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 10px;
  font-weight: 650;
}

.ai-runtime-event-line__input code {
  min-width: 0;
  color: var(--text-primary);
  background: transparent;
  font-size: 12px;
  white-space: normal;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__reason p {
  min-width: 0;
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__section {
  display: grid;
  gap: 2px;
}

.ai-runtime-event-line__section span {
  color: var(--text-tertiary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 10px;
  font-weight: 650;
  line-height: 1.2;
}

.ai-runtime-event-line__section p {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__section-markdown {
  color: var(--text-primary);
  font-size: 13px;
  line-height: 1.5;
}

.ai-runtime-event-line__section-markdown :deep(p) {
  margin: 0;
}

.ai-runtime-event-line__section-markdown :deep(p + p),
.ai-runtime-event-line__section-markdown :deep(p + ul),
.ai-runtime-event-line__section-markdown :deep(p + ol),
.ai-runtime-event-line__section-markdown :deep(p + table),
.ai-runtime-event-line__section-markdown :deep(ul + p),
.ai-runtime-event-line__section-markdown :deep(ol + p),
.ai-runtime-event-line__section-markdown :deep(table + p) {
  margin-top: 6px;
}

.ai-runtime-event-line__section-markdown :deep(ul),
.ai-runtime-event-line__section-markdown :deep(ol) {
  margin: 0;
  padding-left: 18px;
}

.ai-runtime-event-line__section-markdown :deep(pre) {
  margin: 0;
}

.ai-runtime-event-line__section-markdown :deep(table) {
  display: block;
  width: 100%;
  max-width: 100%;
  margin: 6px 0;
  overflow-x: auto;
  border-collapse: collapse;
  font-size: 12px;
  line-height: 1.45;
}

.ai-runtime-event-line__section-markdown :deep(th),
.ai-runtime-event-line__section-markdown :deep(td) {
  padding: 5px 7px;
  border: 1px solid var(--border-color-light);
  text-align: left;
  vertical-align: top;
  white-space: normal;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__section-markdown :deep(th) {
  background: var(--surface-muted);
  color: var(--text-primary);
  font-weight: 650;
}

.ai-runtime-event-line__section-markdown :deep(td) {
  color: var(--text-secondary);
}

.ai-runtime-event-line__section--answer p {
  color: var(--text-primary);
}

.ai-runtime-event-line--thought .ai-runtime-event-line__rail {
  background: var(--runtime-trace-thought-color);
}

.ai-runtime-event-line--thought .ai-runtime-event-line__meta {
  color: var(--runtime-trace-thought-color);
}

.ai-runtime-event-line--tool .ai-runtime-event-line__rail,
.ai-runtime-event-line--mcp .ai-runtime-event-line__rail {
  background: var(--primary-color);
}

.ai-runtime-event-line--tool .ai-runtime-event-line__meta,
.ai-runtime-event-line--mcp .ai-runtime-event-line__meta {
  color: var(--primary-color);
}

.ai-runtime-event-line--skill .ai-runtime-event-line__rail {
  background: var(--info-color);
}

.ai-runtime-event-line--skill .ai-runtime-event-line__meta {
  color: var(--info-color);
}

.ai-runtime-event-line--subagent .ai-runtime-event-line__rail {
  background: var(--runtime-trace-subagent-color);
}

.ai-runtime-event-line--subagent .ai-runtime-event-line__meta {
  color: var(--runtime-trace-subagent-color);
}

.ai-runtime-event-line--action .ai-runtime-event-line__rail {
  background: var(--runtime-trace-action-color);
}

.ai-runtime-event-line--action .ai-runtime-event-line__meta {
  color: var(--runtime-trace-action-color);
}

.ai-runtime-event-line--result .ai-runtime-event-line__rail,
.ai-runtime-event-line--answer .ai-runtime-event-line__rail {
  background: var(--success-color);
}

.ai-runtime-event-line--result .ai-runtime-event-line__meta,
.ai-runtime-event-line--answer .ai-runtime-event-line__meta {
  color: var(--success-color);
}

.ai-runtime-event-line--failed .ai-runtime-event-line__rail,
.ai-runtime-event-line--rejected .ai-runtime-event-line__rail {
  background: var(--danger-color);
}

.ai-runtime-event-line--failed .ai-runtime-event-line__meta,
.ai-runtime-event-line--rejected .ai-runtime-event-line__meta {
  color: var(--danger-color);
}

.ai-runtime-event-line__result {
  margin-top: 6px;
  padding: 8px 12px;
  background: var(--runtime-trace-card-bg);
  border-radius: 6px;
  border: 1px solid var(--border-color-light);
  color: var(--text-primary);
  font-size: 13px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.ai-runtime-event-line__result--failed,
.ai-runtime-event-line__result--rejected {
  border-color: var(--danger-color);
  background: var(--runtime-trace-danger-bg);
  color: var(--danger-color);
}

.ai-runtime-subagent-card {
  margin-top: 8px;
  overflow: hidden;
  border: 1px solid var(--runtime-trace-subagent-border);
  border-radius: 7px;
  background: var(--runtime-trace-subagent-bg);
}

.ai-runtime-subagent-card__header {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: flex-start;
  padding: 9px 10px;
  border-bottom: 1px solid var(--border-color-light);
  background: var(--runtime-trace-card-bg);
}

.ai-runtime-subagent-card__header strong {
  display: block;
  color: var(--text-primary);
  font-size: 13px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.ai-runtime-subagent-card__header span,
.ai-runtime-subagent-card__footer span {
  color: var(--text-tertiary);
  font-size: 12px;
  line-height: 1.35;
}

.ai-runtime-subagent-card__progress {
  margin: 0;
  padding: 9px 10px 9px 26px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.45;
}

.ai-runtime-subagent-card__progress li + li {
  margin-top: 4px;
}

.ai-runtime-subagent-card__summary {
  margin: 0;
  padding: 9px 10px;
  color: var(--text-secondary);
  font-size: 13px;
}

.ai-runtime-subagent-card__footer {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
  padding: 8px 10px;
  border-top: 1px solid var(--border-color-light);
  flex-wrap: wrap;
}

.ai-runtime-event-line__approval {
  margin-top: 6px;
}

.approval {
  border: 1px solid var(--border-color-light);
  border-left: 3px solid var(--warning-color);
  border-radius: 6px;
  background: var(--surface-overlay);
  padding: 6px 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.35;
}

.approval-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.ai-runtime-trace__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 2px;
  align-items: center;
}

.ai-runtime-artifact {
  display: grid;
  gap: 12px;
}

.ai-runtime-artifact__meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
  margin: 0;
}

.ai-runtime-artifact__meta div {
  min-width: 0;
}

.ai-runtime-artifact__meta dt {
  font-size: 12px;
  color: var(--text-secondary);
}

.ai-runtime-artifact__meta dd {
  margin: 2px 0 0;
  font-size: 13px;
  color: var(--text-primary);
  word-break: break-word;
}

.ai-runtime-artifact pre {
  max-height: 360px;
  overflow: auto;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border-color-light);
  border-radius: 6px;
  background: var(--bg-elevated);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.ai-runtime-child-run {
  display: grid;
  gap: 10px;
}

.ai-runtime-child-run__id {
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
  word-break: break-word;
}

.ai-runtime-child-run__parent {
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
}

@media (max-width: 640px) {
  .ai-runtime-events {
    padding: 12px 12px 12px 24px;
  }

  .ai-runtime-events::before {
    left: 12px;
  }

  .ai-runtime-event-line__rail {
    left: -17px;
  }
}
</style>
