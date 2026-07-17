<template>
  <section class="runtime-operations" aria-label="AI Runtime Operations">
    <header class="runtime-operations__header">
      <div>
        <h1>Runtime Operations</h1>
        <p>AI Agent Runtime 当前运行状态与最近窗口指标。</p>
      </div>
      <div class="runtime-operations__header-meta">
        <span>更新于 {{ observedAtText }}</span>
        <el-button :icon="Refresh" :loading="loading" @click="loadSummary">刷新</el-button>
      </div>
    </header>

    <div v-if="errorMessage" class="runtime-operations__error" role="alert">
      <CircleCloseFilled />
      <span>{{ errorMessage }}</span>
    </div>

    <section class="runtime-operations__band" aria-labelledby="runtime-current-status">
      <div class="runtime-operations__section-title">
        <div>
          <h2 id="runtime-current-status">运行状态</h2>
          <p>待审批和待输入属于活动 Run 的子集。</p>
        </div>
        <div class="runtime-replica">
          <span>实例状态</span>
          <el-tag :type="replicaTagType" effect="plain">{{ summary.replica.status }}</el-tag>
          <code>{{ summary.replica.instance_id || '-' }}</code>
        </div>
      </div>
      <div class="runtime-stat-grid">
        <div v-for="item in runStatusItems" :key="item.key" class="runtime-stat">
          <span>{{ item.label }}</span>
          <strong :class="{ 'is-alert': item.alert && item.value > 0 }">{{ item.value }}</strong>
        </div>
      </div>
    </section>

    <div class="runtime-operations__columns">
      <section class="runtime-operations__band">
        <div class="runtime-operations__section-title">
          <div><h2>5 分钟</h2><p>最近终态与延迟 P95。</p></div>
        </div>
        <div class="runtime-window">
          <div class="runtime-window__terminal">
            <div v-for="item in terminalRows(summary.terminal_5m)" :key="item.key" class="runtime-window__row">
              <span>{{ item.label }}</span><strong>{{ item.count }}</strong>
            </div>
          </div>
          <div class="runtime-window__latency">
            <div v-for="item in latencyRows(summary.latency_5m)" :key="item.key" class="runtime-window__row">
              <span>{{ item.label }} P95</span><strong>{{ item.value }}</strong>
            </div>
          </div>
        </div>
      </section>
      <section class="runtime-operations__band">
        <div class="runtime-operations__section-title">
          <div><h2>1 小时</h2><p>最近终态与延迟 P95。</p></div>
        </div>
        <div class="runtime-window">
          <div class="runtime-window__terminal">
            <div v-for="item in terminalRows(summary.terminal_1h)" :key="item.key" class="runtime-window__row">
              <span>{{ item.label }}</span><strong>{{ item.count }}</strong>
            </div>
          </div>
          <div class="runtime-window__latency">
            <div v-for="item in latencyRows(summary.latency_1h)" :key="item.key" class="runtime-window__row">
              <span>{{ item.label }} P95</span><strong>{{ item.value }}</strong>
            </div>
          </div>
        </div>
      </section>
    </div>

    <section class="runtime-operations__band">
      <div class="runtime-operations__section-title">
        <div><h2>失败分类</h2><p>最近 1 小时的终态错误 taxonomy。</p></div>
      </div>
      <el-table :data="summary.failures_1h" empty-text="最近 1 小时无失败" size="small">
        <el-table-column prop="category" label="Category" min-width="180" />
        <el-table-column prop="code" label="Code" min-width="260" />
        <el-table-column prop="count" label="次数" width="100" align="right" />
      </el-table>
    </section>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { CircleCloseFilled, Refresh } from '@element-plus/icons-vue'
import { getAgentRuntimeOperationsSummary } from '@/api/aiAgentStore'
import {
  createRuntimeOperationsLoader,
  formatDurationMetric,
  normalizeRuntimeOperationsSummary,
  terminalRows
} from './aiRuntimeOperations'

const REFRESH_INTERVAL_MS = 60_000
const props = defineProps({ active: { type: Boolean, default: false } })
const loading = ref(false)
const errorMessage = ref('')
const summary = ref(normalizeRuntimeOperationsSummary())
let requestSequence = 0
let refreshTimer
const summaryLoader = createRuntimeOperationsLoader(getAgentRuntimeOperationsSummary)

function latencyRows(latency) {
  return [
    ['first_response', '首响应'],
    ['publish_delay', '事件发布'],
    ['replay_delay', '事件回放']
  ].map(([key, label]) => ({ key, label, value: formatDurationMetric(latency?.[key]) }))
}

const observedAtText = computed(() => {
  if (!summary.value.observed_at) return '-'
  const value = new Date(summary.value.observed_at)
  return Number.isNaN(value.getTime()) ? '-' : value.toLocaleString()
})
const replicaTagType = computed(() => summary.value.replica.status === 'ready' ? 'success' : 'warning')
const runStatusItems = computed(() => [
  { key: 'active', label: '活动 Run', value: summary.value.runs.active },
  { key: 'awaiting_approval', label: '待审批', value: summary.value.runs.awaiting_approval },
  { key: 'awaiting_input', label: '待输入', value: summary.value.runs.awaiting_input },
  { key: 'stale', label: '租约过期', value: summary.value.runs.stale, alert: true },
  { key: 'orphaned', label: '无 Owner', value: summary.value.runs.orphaned, alert: true }
])

async function loadSummary() {
  const sequence = ++requestSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await summaryLoader.load()
    if (!result.applied || sequence !== requestSequence) return
    summary.value = result.summary
  } catch (error) {
    if (sequence !== requestSequence) return
    errorMessage.value = error?.message || 'Runtime Operations 加载失败'
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function stopRefresh() {
  if (refreshTimer) window.clearInterval(refreshTimer)
  refreshTimer = undefined
  requestSequence += 1
  summaryLoader.invalidate()
  loading.value = false
}

function startRefresh() {
  stopRefresh()
  loadSummary()
  refreshTimer = window.setInterval(loadSummary, REFRESH_INTERVAL_MS)
}

watch(() => props.active, (active) => {
  if (active) startRefresh()
  else stopRefresh()
}, { immediate: true })
onBeforeUnmount(stopRefresh)
defineExpose({ loadSummary })
</script>

<style scoped lang="scss">
.runtime-operations { display: grid; gap: 12px; }
.runtime-operations__header,
.runtime-operations__section-title,
.runtime-replica { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.runtime-operations__header { padding: 2px 0 8px; }
h1, h2, p { margin: 0; }
h1 { color: var(--text-primary); font-size: 20px; font-weight: 650; }
h2 { color: var(--text-primary); font-size: 15px; font-weight: 650; }
p { margin-top: 4px; color: var(--text-secondary); font-size: 12px; line-height: 1.45; }
.runtime-operations__header-meta { display: flex; align-items: center; gap: 10px; color: var(--text-tertiary); font-size: 12px; }
.runtime-operations__error { display: flex; align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid var(--el-color-danger-light-7); color: var(--el-color-danger); background: var(--el-color-danger-light-9); }
.runtime-operations__band { padding: 16px; border: 1px solid var(--border-color-light); background: var(--bg-card); }
.runtime-replica { justify-content: flex-end; color: var(--text-secondary); font-size: 12px; }
.runtime-replica code { max-width: 260px; overflow: hidden; color: var(--text-tertiary); text-overflow: ellipsis; white-space: nowrap; }
.runtime-stat-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); margin-top: 14px; border-top: 1px solid var(--border-color-light); border-left: 1px solid var(--border-color-light); }
.runtime-stat { min-height: 78px; padding: 12px; border-right: 1px solid var(--border-color-light); border-bottom: 1px solid var(--border-color-light); display: grid; align-content: center; gap: 6px; }
.runtime-stat span, .runtime-window__row span { color: var(--text-secondary); font-size: 12px; }
.runtime-stat strong { color: var(--text-primary); font-size: 24px; font-weight: 650; }
.runtime-stat strong.is-alert { color: var(--el-color-danger); }
.runtime-operations__columns { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.runtime-window { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; margin-top: 12px; }
.runtime-window__terminal, .runtime-window__latency { border-top: 1px solid var(--border-color-light); }
.runtime-window__row { min-height: 34px; border-bottom: 1px solid var(--border-color-light); display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.runtime-window__row strong { color: var(--text-primary); font-size: 13px; font-weight: 600; }
@media (max-width: 900px) {
  .runtime-operations__columns { grid-template-columns: 1fr; }
  .runtime-stat-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 640px) {
  .runtime-operations__header, .runtime-operations__section-title { align-items: flex-start; flex-direction: column; }
  .runtime-operations__header-meta, .runtime-replica { width: 100%; justify-content: space-between; flex-wrap: wrap; }
  .runtime-stat-grid, .runtime-window { grid-template-columns: 1fr; }
}
</style>
