<template>
  <div class="credentials-page">
    <div class="page-header">
      <div>
        <h2>
          <el-icon><Key /></el-icon>
          凭据管理
        </h2>
        <p>统一管理工作空间内可供流水线和服务任务使用的认证凭据。</p>
      </div>
      <el-button v-if="canWriteCredentials" type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>
        新建凭据
      </el-button>
    </div>

    <div class="stats-grid">
      <el-card>
        <div class="stat-value">{{ stats.total }}</div>
        <div class="stat-label">凭据总数</div>
      </el-card>
      <el-card>
        <div class="stat-value">{{ stats.active }}</div>
        <div class="stat-label">可用</div>
      </el-card>
      <el-card>
        <div class="stat-value">{{ stats.disabled }}</div>
        <div class="stat-label">禁用 / 撤销</div>
      </el-card>
      <el-card>
        <div class="stat-value">{{ stats.expired }}</div>
        <div class="stat-label">已过期</div>
      </el-card>
    </div>

    <div class="filter-bar">
      <el-input v-model="filters.keyword" placeholder="搜索名称或描述" clearable class="search-input" @keyup.enter="reloadList" @clear="reloadList">
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      <el-select v-model="filters.type" clearable placeholder="类型" class="filter-select">
        <el-option v-for="type in credentialTypes" :key="type.value" :label="type.label" :value="type.value" />
      </el-select>
      <el-select v-model="filters.category" clearable placeholder="分类" class="filter-select">
        <el-option v-for="category in credentialCategories" :key="category.value" :label="category.label" :value="category.value" />
      </el-select>
      <el-select v-model="filters.status" clearable placeholder="状态" class="filter-select">
        <el-option label="可用" value="active" />
        <el-option label="已禁用" value="inactive" />
        <el-option label="已撤销" value="revoked" />
        <el-option label="已过期" value="expired" />
      </el-select>
    </div>

    <div v-if="selectedIds.length > 0" class="batch-bar">
      <span>已选择 {{ selectedIds.length }} 项</span>
      <el-button type="danger" link @click="batchDelete">批量删除</el-button>
      <el-button link @click="selectedIds = []">取消</el-button>
    </div>

    <el-table v-loading="loading" :data="credentials" @selection-change="handleSelectionChange">
      <el-table-column v-if="hasDeletableCredentials" type="selection" width="48" :selectable="isRowDeletable" />
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column prop="type" label="类型" width="130">
        <template #default="{ row }">
          <el-tag size="small">{{ getTypeLabel(row.type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="category" label="分类" width="130">
        <template #default="{ row }">{{ getCategoryLabel(row.category) }}</template>
      </el-table-column>
      <el-table-column prop="scope" label="范围" width="120">
        <template #default="{ row }">{{ getScopeLabel(row.scope) }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="getStatusType(row.status)" size="small">{{ getStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="lock_state" label="锁定状态" width="120">
        <template #default="{ row }">
          <el-tag :type="getLockStateType(row.lock_state)" size="small">{{ getLockStateLabel(row.lock_state) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="used_count" label="使用次数" width="100" align="center" />
      <el-table-column prop="last_used_at" label="最后使用" width="180">
        <template #default="{ row }">{{ formatDateTime(row.last_used_at ? row.last_used_at * 1000 : null) }}</template>
      </el-table-column>
      <el-table-column prop="updated_at" label="更新时间" width="180">
        <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" min-width="360" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.can_view_secret" type="primary" link @click="handleViewPayload(row)">查看敏感载荷</el-button>
          <el-button v-if="row.can_verify" type="success" link @click="handleVerify(row)">验证</el-button>
          <el-button type="info" link @click="showUsage(row)">使用统计</el-button>
          <el-button type="warning" link @click="showImpact(row)">影响分析</el-button>
          <el-button v-if="row.can_edit" type="primary" link @click="handleEdit(row)">编辑</el-button>
          <el-button v-if="row.can_delete" type="danger" link @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.size"
        :total="pagination.total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadCredentials"
        @current-change="loadCredentials"
      />
    </div>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑凭据' : '新建凭据'" width="720px" destroy-on-close>
      <CredentialForm
        v-if="dialogVisible"
        :initial-data="currentCredential"
        :types="credentialTypes"
        :categories="credentialCategories"
        @submit="handleFormSubmit"
        @cancel="dialogVisible = false"
      />
    </el-dialog>

    <el-dialog v-model="payloadVisible" title="敏感载荷" width="720px">
      <el-alert type="warning" :closable="false" show-icon>敏感载荷仅会在显式敏感查看操作中返回，且仅对具备查看权限的成员可见。</el-alert>
      <pre class="payload-preview">{{ JSON.stringify(credentialPayload, null, 2) }}</pre>
    </el-dialog>

    <el-dialog v-model="usageDialogVisible" title="使用统计" width="520px">
      <div v-if="usageData" class="usage-grid">
        <div class="usage-card">
          <div class="usage-value">{{ usageData.used_count }}</div>
          <div class="usage-label">总使用次数</div>
        </div>
        <div class="usage-card success">
          <div class="usage-value">{{ usageData.success_count }}</div>
          <div class="usage-label">成功次数</div>
        </div>
        <div class="usage-card danger">
          <div class="usage-value">{{ usageData.failed_count }}</div>
          <div class="usage-label">失败次数</div>
        </div>
      </div>
      <div v-if="usageData" class="usage-footer">
        <div>成功率：{{ Math.round(Number(usageData.success_rate || 0)) }}%</div>
        <div>最后使用：{{ formatDateTime(usageData.last_used_at ? usageData.last_used_at * 1000 : null) }}</div>
      </div>
    </el-dialog>

    <el-dialog v-model="impactDialogVisible" title="影响分析" width="720px">
      <template v-if="impactData">
        <el-alert type="info" :closable="false" show-icon>
          当前凭据被 {{ impactData.pipeline_count }} 条流水线、{{ impactData.reference_count }} 个任务节点引用。
        </el-alert>
        <el-table :data="impactData.references || []" style="margin-top: 16px">
          <el-table-column prop="pipeline_name" label="流水线" min-width="180" />
          <el-table-column prop="task_type" label="任务类型" width="140" />
          <el-table-column prop="credential_slot" label="凭据槽位" width="140" />
          <el-table-column prop="node_id" label="节点 ID" width="140" />
          <el-table-column prop="updated_at" label="更新时间" width="180">
            <template #default="{ row }">{{ formatDateTime(row.updated_at ? row.updated_at * 1000 : null) }}</template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, h, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Key, Plus, Search } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'
import {
  batchDeleteCredentials,
  batchCredentialImpact,
  createCredential,
  deleteCredential,
  getCredentialCategories,
  getCredentialImpact,
  getCredentialList,
  getCredentialPayload,
  getCredentialTypes,
  getCredentialUsage,
  updateCredential,
  verifyCredential
} from '@/api/credential'
import CredentialForm from './components/CredentialForm.vue'

const userStore = useUserStore()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const credentials = ref([])
const credentialTypes = ref([])
const credentialCategories = ref([])
const selectedIds = ref([])
const dialogVisible = ref(false)
const isEdit = ref(false)
const currentCredential = ref(null)
const payloadVisible = ref(false)
const credentialPayload = ref({})
const usageDialogVisible = ref(false)
const usageData = ref(null)
const impactDialogVisible = ref(false)
const impactData = ref(null)
const canWriteCredentials = computed(() => userStore.hasPermission('credential.write'))
const hasDeletableCredentials = computed(() => credentials.value.some(item => item.can_delete))

const pagination = reactive({ page: 1, size: 10, total: 0 })
const filters = reactive({ keyword: '', type: '', category: '', status: '' })

const stats = computed(() => ({
  total: pagination.total,
  active: credentials.value.filter(item => item.status === 'active').length,
  disabled: credentials.value.filter(item => item.status === 'inactive' || item.status === 'revoked').length,
  expired: credentials.value.filter(item => item.status === 'expired').length
}))

async function loadCredentials() {
  loading.value = true
  try {
    const res = await getCredentialList({
      page: pagination.page,
      size: pagination.size,
      keyword: filters.keyword || undefined,
      type: filters.type || undefined,
      category: filters.category || undefined,
      status: filters.status || undefined
    })
    if (res.code === 200) {
      credentials.value = res.data.list
      pagination.total = res.data.total
    }
  } catch (error) {
    ElMessage.error('加载凭据列表失败')
  } finally {
    loading.value = false
  }
}

function reloadList() {
  pagination.page = 1
  loadCredentials()
}

async function loadTypesAndCategories() {
  const [typesRes, categoriesRes] = await Promise.all([getCredentialTypes(), getCredentialCategories()])
  if (typesRes.code === 200) {
    credentialTypes.value = typesRes.data.map(item => ({ value: item.value, label: item.label || item.name }))
  }
  if (categoriesRes.code === 200) {
    credentialCategories.value = categoriesRes.data.map(item => ({
      value: item.value,
      label: item.label || item.name || item.value,
      supported_modes: item.supported_modes || []
    }))
  }
}

function handleSelectionChange(rows) {
  selectedIds.value = rows.map(row => row.id)
}

function isRowDeletable(row) {
  return !!row?.can_delete
}

function showCreateDialog(preset = null) {
  currentCredential.value = preset ? { ...preset } : null
  isEdit.value = false
  dialogVisible.value = true
}

function buildCreatePresetFromRoute() {
  const { type, category, source } = route.query || {}
  if (!type && !category) return null
  const description = source === 'resource-vm'
    ? '用于 VM / 主机资源接入的用户名密码或 SSH 密钥。'
    : source === 'resource-k8s'
      ? '用于 Kubernetes 资源接入的集群认证凭据。'
      : ''
  return {
    name: '',
    description,
    type: type || '',
    category: category || '',
    scope: 'workspace',
    lock_state: 'locked',
    payload: {}
  }
}

async function openCreateDialogFromRoute() {
  if (route.query.create !== '1' || !canWriteCredentials.value) return
  showCreateDialog(buildCreatePresetFromRoute())
  const nextQuery = { ...route.query }
  delete nextQuery.create
  delete nextQuery.type
  delete nextQuery.category
  delete nextQuery.source
  await router.replace({ path: route.path, query: nextQuery })
}

async function handleEdit(credential) {
  if (!credential?.can_edit) {
    ElMessage.warning('当前凭据不允许编辑')
    return
  }
  isEdit.value = true
  try {
    const res = await getCredentialPayload(credential.id)
    currentCredential.value = { ...credential, payload: res?.data?.payload || {} }
  } catch (error) {
    currentCredential.value = { ...credential, payload: {} }
    ElMessage.warning('敏感字段获取失败，请手动补充后保存')
  }
  dialogVisible.value = true
}

async function handleViewPayload(credential) {
  if (!credential?.can_view_secret) {
    ElMessage.warning('当前凭据不允许查看敏感载荷')
    return
  }
  try {
    const res = await getCredentialPayload(credential.id)
    credentialPayload.value = res.data.payload || {}
    payloadVisible.value = true
  } catch (error) {
    ElMessage.error('读取敏感载荷失败')
  }
}

async function handleVerify(credential) {
  if (!credential?.can_verify) {
    ElMessage.warning('当前凭据不允许验证')
    return
  }
  try {
    const res = await verifyCredential(credential.id)
    if (res.code === 200 && res.data.valid) {
      ElMessage.success('凭据验证通过')
    } else {
      ElMessage.warning(res.data?.message || '凭据验证失败')
    }
  } catch (error) {
    ElMessage.error('凭据验证失败')
  }
}

async function showUsage(credential) {
  try {
    const res = await getCredentialUsage(credential.id)
    usageData.value = res.data
    usageDialogVisible.value = true
  } catch (error) {
    ElMessage.error('获取使用统计失败')
  }
}

async function showImpact(credential) {
  try {
    const res = await getCredentialImpact(credential.id)
    impactData.value = res.data
    impactDialogVisible.value = true
  } catch (error) {
    ElMessage.error('获取影响分析失败')
  }
}

async function handleDelete(credential) {
  if (!credential?.can_delete) {
    ElMessage.warning('当前凭据不允许删除')
    return
  }
  try {
    const impactRes = await getCredentialImpact(credential.id)
    const impact = impactRes?.data || null
    const references = Array.isArray(impact?.references) ? impact.references : []
    const uniquePipelines = [...new Set(references.map(item => item.pipeline_name).filter(Boolean))]
    const isInUse = Number(impact?.reference_count || 0) > 0

    const message = isInUse
      ? h('div', { class: 'delete-warning-content' }, [
          h('p', null, `凭据“${credential.name}”当前仍被 ${impact.pipeline_count} 条流水线、${impact.reference_count} 个任务节点引用。`),
          h('div', { class: 'delete-warning-pipelines' }, [
            h('div', { class: 'delete-warning-title' }, '当前使用该凭据的流水线：'),
            h('ul', null, uniquePipelines.map(name => h('li', { key: name }, name)))
          ]),
          h('p', { class: 'delete-warning-tip' }, '删除后，这些流水线中的当前凭据绑定不会自动替换。请前往对应流水线设计页，手动清理或重新配置当前凭据。'),
          h('p', { class: 'delete-warning-tip danger' }, '确认删除后，相关流水线下次运行可能因凭据缺失而失败。')
        ])
      : `确定删除凭据“${credential.name}”吗？此操作不可恢复。`

    await ElMessageBox.confirm(message, isInUse ? '删除前确认流水线影响' : '确认删除', {
      type: 'warning',
      dangerouslyUseHTMLString: false,
      confirmButtonText: '确认删除',
      cancelButtonText: '取消'
    })
    await deleteCredential(credential.id)
    ElMessage.success('删除成功')
    loadCredentials()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.message || '删除失败')
    }
  }
}

async function batchDelete() {
  try {
    const impactRes = await batchCredentialImpact(selectedIds.value)
    const impact = impactRes?.data || null
    const impactedItems = Array.isArray(impact?.items) ? impact.items.filter(item => Number(item.reference_count || 0) > 0) : []
    const message = impactedItems.length > 0
      ? h('div', { class: 'delete-warning-content' }, [
          h('p', null, `选中的 ${selectedIds.value.length} 个凭据中，有 ${impactedItems.length} 个仍被流水线引用。`),
          h('div', { class: 'delete-warning-pipelines' }, [
            h('div', { class: 'delete-warning-title' }, '受影响的凭据：'),
            h('ul', null, impactedItems.map(item => h('li', { key: item.credential_id }, `${item.credential_name}（${item.pipeline_count} 条流水线 / ${item.reference_count} 个节点）`)))
          ]),
          h('p', { class: 'delete-warning-tip' }, '删除后，请前往对应流水线设计页手动清理这些凭据绑定，否则相关流水线后续运行可能失败。')
        ])
      : `确定删除选中的 ${selectedIds.value.length} 个凭据吗？`

    await ElMessageBox.confirm(message, impactedItems.length > 0 ? '批量删除前确认流水线影响' : '批量删除', {
      type: 'warning',
      dangerouslyUseHTMLString: false,
      confirmButtonText: '确认删除',
      cancelButtonText: '取消'
    })
    await batchDeleteCredentials(selectedIds.value)
    selectedIds.value = []
    ElMessage.success('批量删除成功')
    loadCredentials()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.message || '批量删除失败')
    }
  }
}

async function handleFormSubmit(formData) {
  try {
    if (isEdit.value && currentCredential.value?.id) {
      await updateCredential(currentCredential.value.id, formData)
      ElMessage.success('凭据更新成功')
    } else {
      await createCredential(formData)
      ElMessage.success('凭据创建成功')
    }
    dialogVisible.value = false
    loadCredentials()
  } catch (error) {
    ElMessage.error(error?.message || '保存凭据失败')
  }
}

function getTypeLabel(type) {
  return credentialTypes.value.find(item => item.value === type)?.label || type
}

function getCategoryLabel(category) {
  return credentialCategories.value.find(item => item.value === category)?.label || category || '-'
}

function getScopeLabel(scope) {
  if (scope === 'personal') return '个人'
  if (scope === 'project') return '项目'
  if (scope === 'workspace') return '工作空间'
  return scope || '-'
}

function getStatusType(status) {
  if (status === 'active') return 'success'
  if (status === 'inactive') return 'info'
  if (status === 'expired') return 'warning'
  if (status === 'revoked') return 'danger'
  return 'info'
}

function getStatusLabel(status) {
  if (status === 'active') return '可用'
  if (status === 'inactive') return '已禁用'
  if (status === 'expired') return '已过期'
  if (status === 'revoked') return '已撤销'
  return status
}

function getLockStateType(lockState) {
  if (lockState === 'unlocked') return 'success'
  return 'warning'
}

function getLockStateLabel(lockState) {
  if (lockState === 'unlocked') return '已解锁'
  return '已锁定'
}

function formatDateTime(value) {
  if (!value) return '从未使用'
  return new Date(value).toLocaleString()
}

watch(() => ({ ...filters }), reloadList, { deep: true })

watch(() => userStore.currentWorkspaceId, () => {
  selectedIds.value = []
  reloadList()
})

onMounted(async () => {
  await loadTypesAndCategories()
  await loadCredentials()
  await openCreateDialogFromRoute()
})
</script>

<style scoped>
.credentials-page {
  padding: 24px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
}

.page-header h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 8px;
}

.page-header p {
  margin: 0;
  color: var(--text-muted);
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 20px;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
}

.stat-label {
  margin-top: 8px;
  color: var(--text-muted);
}

.filter-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.search-input {
  width: 280px;
}

.filter-select {
  width: 160px;
}

.batch-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
}

.payload-preview {
  margin: 16px 0 0;
  padding: 16px;
  border-radius: 8px;
  background: var(--bg-secondary);
  overflow: auto;
}

.usage-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.usage-card {
  padding: 16px;
  border-radius: 8px;
  background: var(--bg-secondary);
  text-align: center;
}

.usage-card.success {
  background: var(--success-light);
}

.usage-card.danger {
  background: var(--danger-light);
}

.usage-value {
  font-size: 28px;
  font-weight: 700;
}

.usage-label,
.usage-footer {
  color: var(--text-muted);
}

.usage-footer {
  display: flex;
  justify-content: space-between;
  margin-top: 16px;
}

:deep(.delete-warning-content) {
  display: grid;
  gap: 12px;
  line-height: 1.6;
}

:deep(.delete-warning-title) {
  font-weight: 600;
}

:deep(.delete-warning-pipelines ul) {
  margin: 8px 0 0;
  padding-left: 20px;
}

:deep(.delete-warning-tip) {
  color: var(--text-muted);
}

:deep(.delete-warning-tip.danger) {
  color: var(--danger-color);
  font-weight: 500;
}
</style>
