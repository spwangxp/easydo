<template>
  <div class="governance-card audit-log-panel">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">{{ title }}</h2>
        <div class="section-subtitle">追踪人员、成员、角色和通知配置相关的治理操作。</div>
      </div>
      <el-button :loading="loading" @click="loadLogs">刷新</el-button>
    </div>

    <div class="filter-row">
      <el-input
        v-model="filters.action"
        clearable
        placeholder="操作类型"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-input
        v-model="filters.target_type"
        clearable
        placeholder="对象类型"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-input
        v-model="filters.target_id"
        clearable
        placeholder="对象 ID"
        class="target-id-input"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-button type="primary" @click="handleSearch">筛选</el-button>
    </div>

    <el-table v-loading="loading" :data="logs" style="width: 100%">
      <el-table-column label="时间" width="180">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="action" label="操作" min-width="180" />
      <el-table-column label="对象" min-width="180">
        <template #default="{ row }">{{ row.target_type || '-' }} #{{ row.target_id || '-' }}</template>
      </el-table-column>
      <el-table-column prop="workspace_id" label="工作区 ID" width="120">
        <template #default="{ row }">{{ row.workspace_id || '-' }}</template>
      </el-table-column>
      <el-table-column prop="actor_user_id" label="操作者 ID" width="120" />
      <el-table-column prop="actor_role" label="操作者角色" width="130">
        <template #default="{ row }">{{ row.actor_role || '-' }}</template>
      </el-table-column>
      <el-table-column prop="ip" label="IP" min-width="140">
        <template #default="{ row }">{{ row.ip || '-' }}</template>
      </el-table-column>
    </el-table>

    <div class="pagination-row">
      <el-pagination
        background
        layout="prev, pager, next, total"
        :current-page="page"
        :page-size="pageSize"
        :total="total"
        @current-change="handlePageChange"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getAuditLogs, getWorkspaceAuditLogs } from '@/api/audit'

const props = defineProps({
  scope: {
    type: String,
    required: true
  },
  workspaceId: {
    type: Number,
    default: 0
  }
})

const loading = ref(false)
const logs = ref([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({
  action: '',
  target_type: '',
  target_id: ''
})

const title = computed(() => props.scope === 'platform' ? '平台审计日志' : '工作区审计日志')

const extractLogs = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  return []
}

const buildParams = () => {
  const params = {
    page: page.value,
    page_size: pageSize.value
  }
  if (filters.action) params.action = filters.action
  if (filters.target_type) params.target_type = filters.target_type
  const targetID = Number(filters.target_id)
  if (Number.isInteger(targetID) && targetID > 0) params.target_id = targetID
  return params
}

const formatDateTime = (value) => {
  if (!value) return '-'
  const parsed = typeof value === 'number'
    ? new Date(value < 1e12 ? value * 1000 : value)
    : new Date(value)
  return Number.isNaN(parsed.getTime()) ? '-' : parsed.toLocaleString('zh-CN')
}

const loadLogs = async () => {
  if (props.scope === 'workspace' && !props.workspaceId) {
    logs.value = []
    total.value = 0
    return
  }
  loading.value = true
  try {
    const res = props.scope === 'workspace'
      ? await getWorkspaceAuditLogs(props.workspaceId, buildParams())
      : await getAuditLogs(buildParams())
    logs.value = extractLogs(res?.data)
    total.value = Number(res?.data?.total || res?.data?.count || logs.value.length || 0)
  } catch (error) {
    ElMessage.error('加载审计日志失败')
  } finally {
    loading.value = false
  }
}

const handleSearch = async () => {
  page.value = 1
  await loadLogs()
}

const handlePageChange = async (nextPage) => {
  page.value = nextPage
  await loadLogs()
}

watch(() => props.workspaceId, async () => {
  page.value = 1
  await loadLogs()
})

onMounted(loadLogs)
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.audit-log-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header-row,
.filter-row,
.pagination-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.section-header-row {
  justify-content: space-between;
}

.section-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.section-subtitle {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 14px;
}

.filter-row {
  align-items: center;
  max-width: 780px;
}

.target-id-input {
  width: 160px;
}

.pagination-row {
  justify-content: flex-end;
}

@media (max-width: 900px) {
  .section-header-row,
  .filter-row {
    flex-direction: column;
    align-items: stretch;
  }

  .target-id-input {
    width: 100%;
  }
}
</style>
