<template>
  <div class="resource-page">
    <div class="page-header">
      <div>
        <h2>资源管理</h2>
        <p>
          统一管理工作空间内的 VM 与 K8s 集群资源，发布流程会直接复用这里绑定好的接入凭据。
          当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}
        </p>
      </div>
      <el-button v-if="canManage" type="primary" @click="openCreateDialog">新建资源</el-button>
    </div>

    <div class="filter-bar">
      <el-input v-model="filters.keyword" placeholder="搜索资源名称" clearable class="search-input" />
      <el-select v-model="filters.type" clearable placeholder="资源类型" class="filter-select">
        <el-option label="VM" value="vm" />
        <el-option label="K8s 集群" value="k8s" />
      </el-select>
      <el-select v-model="filters.environment" clearable placeholder="环境" class="filter-select">
        <el-option label="开发环境" value="development" />
        <el-option label="测试环境" value="testing" />
        <el-option label="生产环境" value="production" />
      </el-select>
      <el-button @click="fetchResources">刷新</el-button>
    </div>

    <el-table v-loading="loading" :data="filteredResources">
      <el-table-column prop="name" label="资源名称" min-width="180" />
      <el-table-column prop="type" label="类型" width="120">
        <template #default="{ row }">
          <el-tag :type="row.type === 'vm' ? 'success' : 'primary'">{{ row.type === 'vm' ? 'VM' : 'K8s 集群' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="environment" label="环境" width="120">
        <template #default="{ row }">{{ environmentText(row.environment) }}</template>
      </el-table-column>
      <el-table-column label="接入凭据" min-width="180">
        <template #default="{ row }">
          <div class="binding-cell">
            <span class="binding-name">{{ getCredentialBindingName(row) }}</span>
            <span class="binding-meta">{{ getBindingSummaryText(row) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="endpoint" label="接入地址" min-width="180" />
      <el-table-column label="基础资源" min-width="220">
        <template #default="{ row }">
          <div class="binding-cell">
            <span class="binding-name">{{ getBaseInfoSummary(row) }}</span>
            <span class="binding-meta">{{ getBaseInfoMeta(row) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)">{{ row.status || '-' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="updated_at" label="更新时间" width="180">
        <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column v-if="canManage || canOpenWebTerminal || canBrowseCluster" label="操作" width="520" fixed="right">
        <template #default="{ row }">
          <el-button v-if="canManage" link type="info" @click="openBaseInfoDialog(row)">基础资源详情</el-button>
          <el-button v-if="canManage" link type="success" :loading="refreshingId === row.id" @click="refreshBaseInfo(row)">刷新基础信息</el-button>
          <el-button v-if="canBrowseCluster && row.type === 'k8s'" link type="primary" @click="openK8sBrowser(row)">Browse Cluster</el-button>
          <el-button v-if="canOpenWebTerminal && row.type === 'vm'" link type="primary" @click="openWebTerminal(row)">Web Terminal</el-button>
          <el-button v-if="canManage" link type="primary" @click="openEditDialog(row)">编辑</el-button>
          <el-button v-if="canManage" link type="danger" @click="removeResource(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑资源' : '新建资源'" width="720px" destroy-on-close>
      <ResourceForm
        v-if="dialogVisible"
        :initial-data="currentResource"
        :submitting="saving"
        @submit="handleFormSubmit"
        @cancel="dialogVisible = false"
      />
    </el-dialog>

    <el-dialog v-model="baseInfoDialogVisible" title="基础资源详情" width="720px" destroy-on-close>
      <div v-if="baseInfoDialogResource" class="base-info-detail">
        <el-alert :type="baseInfoDialogStatusType" :closable="false" show-icon class="base-info-alert">
          {{ getBaseInfoMeta(baseInfoDialogResource) }}
        </el-alert>

        <el-descriptions v-if="baseInfoDialogResource.type === 'k8s'" :column="2" border>
          <el-descriptions-item label="资源名称">{{ baseInfoDialogResource.name }}</el-descriptions-item>
          <el-descriptions-item label="集群版本">{{ baseInfoDialogResource.baseInfo?.k8s?.cluster?.serverVersion || '-' }}</el-descriptions-item>
          <el-descriptions-item label="节点数">{{ baseInfoDialogResource.baseInfo?.k8s?.summary?.nodeCount || '-' }}</el-descriptions-item>
          <el-descriptions-item label="可分配 CPU">{{ formatCPUMilli(baseInfoDialogResource.baseInfo?.k8s?.summary?.cpuAllocatableMilli) }}</el-descriptions-item>
          <el-descriptions-item label="可分配内存">{{ formatBytes(baseInfoDialogResource.baseInfo?.k8s?.summary?.memoryAllocatableBytes) }}</el-descriptions-item>
          <el-descriptions-item label="可分配 GPU">{{ baseInfoDialogResource.baseInfo?.k8s?.summary?.gpuAllocatable || 0 }}</el-descriptions-item>
        </el-descriptions>

        <el-descriptions v-else :column="2" border>
          <el-descriptions-item label="资源名称">{{ baseInfoDialogResource.name }}</el-descriptions-item>
          <el-descriptions-item label="主机名">{{ baseInfoDialogResource.baseInfo?.machine?.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item label="IP 地址">{{ baseInfoDialogResource.baseInfo?.machine?.primaryIpv4 || '-' }}</el-descriptions-item>
          <el-descriptions-item label="系统">{{ getMachineSystemText(baseInfoDialogResource) }}</el-descriptions-item>
          <el-descriptions-item label="CPU">{{ getMachineCpuText(baseInfoDialogResource) }}</el-descriptions-item>
          <el-descriptions-item label="内存">{{ formatBytes(baseInfoDialogResource.baseInfo?.machine?.memory?.totalBytes) }}</el-descriptions-item>
          <el-descriptions-item label="磁盘">{{ formatBytes(getMachineDiskTotal(baseInfoDialogResource)) }}</el-descriptions-item>
          <el-descriptions-item label="GPU 数量">{{ `${baseInfoDialogResource.baseInfo?.machine?.gpu?.count || 0}` }}</el-descriptions-item>
          <el-descriptions-item label="GPU 规格" :span="2">{{ getMachineGpuSpecText(baseInfoDialogResource) }}</el-descriptions-item>
        </el-descriptions>
      </div>
      <template #footer>
        <el-button @click="baseInfoDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import {
  bindResourceCredential,
  createResource,
  deleteResource,
  getResourceCredentialBindings,
  getResourceDetail,
  getResourceList,
  refreshResourceBaseInfo,
  updateResource
} from '@/api/resource'
import { getTaskDetail } from '@/api/task'
import ResourceForm from './components/ResourceForm.vue'
import { createTerminalLaunchPath } from './terminal/terminalPageState'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const saving = ref(false)
const dialogVisible = ref(false)
const editingId = ref(0)
const resources = ref([])
const currentResource = ref(null)
const filters = reactive({ keyword: '', type: '', environment: '' })
const refreshingId = ref(0)
const baseInfoDialogVisible = ref(false)
const baseInfoDialogResource = ref(null)

const canManage = computed(() => userStore.hasPermission('resource.write'))
const canBrowseCluster = computed(() => userStore.hasPermission('resource.read'))
const canOpenWebTerminal = computed(() => userStore.hasPermission('resource.use'))
const isEdit = computed(() => editingId.value > 0)

const filteredResources = computed(() => resources.value.filter(item => {
  if (filters.type && item.type !== filters.type) return false
  if (filters.environment && item.environment !== filters.environment) return false
  if (filters.keyword && !String(item.name || '').toLowerCase().includes(filters.keyword.toLowerCase())) return false
  return true
}))

const normalizeObjectField = (value) => {
  if (!value) return {}
  if (typeof value === 'object') return { ...value }
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch {
      return {}
    }
  }
  return {}
}

const normalizeBindings = (value) => {
  if (Array.isArray(value)) return value
  if (Array.isArray(value?.bindings)) return value.bindings
  if (Array.isArray(value?.list)) return value.list
  if (Array.isArray(value?.items)) return value.items
  return []
}

const getPreferredBinding = (bindings, resourceType) => {
  const list = normalizeBindings(bindings)
  const preferredPurpose = resourceType === 'k8s' ? 'cluster_auth' : 'ssh_auth'
  return list.find(item => item.purpose === preferredPurpose) || list.find(item => item.purpose === 'primary') || list[0] || null
}

const normalizeResource = (resource = {}) => ({
  id: resource.id || 0,
  name: resource.name || '',
  type: resource.type || 'vm',
  environment: resource.environment || 'development',
  endpoint: resource.endpoint || '',
  description: resource.description || '',
  status: resource.status || '',
  updated_at: resource.updated_at || resource.updatedAt || '',
  credentialId: resource.credentialId || null,
  bindings: Array.isArray(resource.bindings) ? resource.bindings : [],
  labels: normalizeObjectField(resource.labels),
  metadata: normalizeObjectField(resource.metadata),
  baseInfo: normalizeObjectField(resource.base_info || resource.baseInfo),
  baseInfoStatus: resource.base_info_status || resource.baseInfoStatus || '',
  baseInfoSource: resource.base_info_source || resource.baseInfoSource || '',
  baseInfoCollectedAt: resource.base_info_collected_at || resource.baseInfoCollectedAt || 0,
  baseInfoLastError: resource.base_info_last_error || resource.baseInfoLastError || ''
})

const resetDialogState = () => {
  editingId.value = 0
  currentResource.value = null
}

const fetchResources = async () => {
  loading.value = true
  try {
    const resourceRes = await getResourceList()
    resources.value = Array.isArray(resourceRes.data) ? resourceRes.data.map(item => normalizeResource(item)) : []
  } finally {
    loading.value = false
  }
}

const openCreateDialog = () => {
  resetDialogState()
  dialogVisible.value = true
}

const openEditDialog = async (row) => {
  resetDialogState()
  editingId.value = row.id

  let resourceData = normalizeResource(row)
  const [detailResult, bindingResult] = await Promise.allSettled([
    getResourceDetail(row.id),
    getResourceCredentialBindings(row.id)
  ])

  if (detailResult.status === 'fulfilled') {
    resourceData = normalizeResource(detailResult.value?.data || row)
  } else {
    ElMessage.warning('资源详情刷新失败，已使用列表中的基础信息')
  }

  const authoritativeBindings = bindingResult.status === 'fulfilled'
    ? normalizeBindings(bindingResult.value?.data)
    : []

  if (bindingResult.status === 'rejected') {
    ElMessage.warning('资源绑定信息刷新失败，请重新确认当前凭据')
  }

  const preferredBinding = getPreferredBinding(
    authoritativeBindings.length > 0
      ? authoritativeBindings
      : detailResult.status === 'fulfilled'
        ? detailResult.value?.data?.bindings
        : row?.bindings,
    resourceData.type
  )

  currentResource.value = {
    ...resourceData,
    credentialId: preferredBinding?.credential_id || preferredBinding?.credential?.id || null
  }
  dialogVisible.value = true
}

const openBaseInfoDialog = async (row) => {
  let resourceData = normalizeResource(row)
  try {
    const detailRes = await getResourceDetail(row.id)
    resourceData = normalizeResource(detailRes?.data || row)
  } catch {
    ElMessage.warning('基础资源详情刷新失败，已使用列表中的基础信息')
  }
  baseInfoDialogResource.value = resourceData
  baseInfoDialogVisible.value = true
}

const openWebTerminal = (row) => {
  if (!row?.id || row.type !== 'vm') return
  const target = router.resolve(createTerminalLaunchPath(row.id))
  window.open(target.href, '_blank', 'noopener')
}

const openK8sBrowser = (row) => {
  if (!row?.id || row.type !== 'k8s') return
  router.push({ name: 'ResourceK8sBrowser', params: { id: row.id } })
}

const syncResourceCredentialBinding = async (resourceId, credentialId, resourceType) => {
  if (!resourceId || !credentialId) return
  await bindResourceCredential(resourceId, {
    credential_id: credentialId,
    purpose: resourceType === 'k8s' ? 'cluster_auth' : 'ssh_auth'
  })
}

const handleFormSubmit = async (formData) => {
  const payload = {
    name: formData.name.trim(),
    type: formData.type,
    environment: formData.environment,
    endpoint: formData.endpoint,
    description: formData.description.trim(),
    labels: JSON.stringify(formData.labels || {}),
    metadata: JSON.stringify(formData.metadata || {})
  }

  saving.value = true
  try {
    let resourceId = editingId.value
    if (editingId.value) {
      await updateResource(editingId.value, payload)
      const resourceRes = await getResourceDetail(editingId.value)
      resourceId = resourceRes?.data?.id || editingId.value
      await syncResourceCredentialBinding(resourceId, formData.credentialId, formData.type)
      ElMessage.success('资源已更新')
    } else {
      const createRes = await createResource({
        ...payload,
        credential_id: formData.credentialId,
        verification_task_id: formData.verificationTaskId
      })
      resourceId = createRes?.data?.id
      ElMessage.success('资源已创建')
    }
    dialogVisible.value = false
    await fetchResources()
  } finally {
    saving.value = false
  }
}

const removeResource = async (row) => {
  await ElMessageBox.confirm(`确认删除资源 ${row.name} 吗？`, '提示', { type: 'warning' })
  await deleteResource(row.id)
  ElMessage.success('资源已删除')
  await fetchResources()
}

const waitForTaskCompletion = async (taskId) => {
  const deadline = Date.now() + 180000
  while (Date.now() < deadline) {
    const res = await getTaskDetail(taskId)
    const task = res?.data || {}
    const status = task.status || ''
    if (status === 'execute_success') {
      return { ok: true, task }
    }
    if (['execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired', 'cancelled'].includes(status)) {
      return { ok: false, task }
    }
    await new Promise(resolve => setTimeout(resolve, 2000))
  }
  return { ok: false, task: { error_msg: '基础信息采集超时，请稍后刷新查看。' } }
}

const refreshBaseInfo = async (row) => {
  if (!row?.id || refreshingId.value) return
  refreshingId.value = row.id
  try {
    const res = await refreshResourceBaseInfo(row.id)
    const taskId = Number(res?.data?.task_id || 0)
    if (!taskId) {
      throw new Error('未拿到基础信息采集任务 ID')
    }
    ElMessage.info('已提交基础资源信息采集任务，请稍候…')
    const result = await waitForTaskCompletion(taskId)
    await fetchResources()
    if (!result.ok) {
      ElMessage.warning(result.task?.error_msg || '基础资源信息采集失败')
      return
    }
    if (dialogVisible.value && currentResource.value?.id === row.id) {
      const detailRes = await getResourceDetail(row.id)
      currentResource.value = normalizeResource(detailRes?.data || row)
    }
    ElMessage.success('基础资源信息已刷新')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '基础资源信息刷新失败')
  } finally {
    refreshingId.value = 0
  }
}

const getPrimaryBinding = (row) => {
  const bindings = Array.isArray(row?.bindings) ? row.bindings : []
  const preferredPurpose = row?.type === 'k8s' ? 'cluster_auth' : row?.type === 'vm' ? 'ssh_auth' : ''
  return bindings.find(item => item.purpose === preferredPurpose) || bindings.find(item => item.purpose === 'primary') || bindings[0] || null
}

const getCredentialBindingName = (row) => getPrimaryBinding(row)?.credential?.name || '未绑定'

const getBindingSummaryText = (row) => {
  const binding = getPrimaryBinding(row)
  if (!binding?.credential) return '待配置'
  if (row.type === 'vm') return '登录凭据'
  return `Kubernetes 凭据 · ${binding.purpose || 'cluster_auth'}`
}

const environmentText = (value) => ({ development: '开发环境', testing: '测试环境', production: '生产环境' }[value] || value || '-')
const statusType = (value) => ({ online: 'success', offline: 'info', error: 'danger', archived: 'warning' }[value] || 'info')
const formatDateTime = (value) => value ? new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString('zh-CN') : '-'
const formatBytes = (value) => {
  const size = Number(value || 0)
  if (!size) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = size
  let unitIndex = 0
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024
    unitIndex += 1
  }
  return `${current >= 10 || unitIndex === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[unitIndex]}`
}
const formatCPUMilli = (value) => {
  const milli = Number(value || 0)
  if (!milli) return '-'
  const cores = milli / 1000
  return `${Number.isInteger(cores) ? cores : cores.toFixed(1)} CPU`
}
const getMachineDiskTotal = (row) => row?.baseInfo?.machine?.storage?.totalDiskBytes || row?.baseInfo?.machine?.storage?.rootTotalBytes || 0
const getMachineCpuText = (row) => {
  const cpu = row?.baseInfo?.machine?.cpu || {}
  if (!cpu.logicalCores && !cpu.model) return '-'
  if (!cpu.logicalCores) return cpu.model
  return cpu.model ? `${cpu.logicalCores} 核 / ${cpu.model}` : `${cpu.logicalCores} 核`
}
const getMachineSystemText = (row) => {
  const machine = row?.baseInfo?.machine || {}
  return [machine?.os?.name, machine?.arch].filter(Boolean).join(' / ') || '-'
}
const getMachineGpuSpecText = (row) => {
  const devices = Array.isArray(row?.baseInfo?.machine?.gpu?.devices) ? row.baseInfo.machine.gpu.devices : []
  if (devices.length === 0) return '-'
  return devices.map((device, index) => {
    const parts = [
      devices.length > 1 ? `#${device?.index ?? index}` : '',
      device?.vendor,
      device?.model,
      formatBytes(device?.memoryBytes)
    ].filter(Boolean)
    return parts.join(' / ')
  }).join('；')
}
const getBaseInfoSummary = (row) => {
  const info = row?.baseInfo || {}
  if (row?.type === 'k8s') {
    const summary = info?.k8s?.summary || {}
    if (!summary.nodeCount) return row?.baseInfoStatus === 'pending' ? '采集中…' : row?.baseInfoStatus === 'failed' ? '采集失败' : '未采集'
    return `${summary.nodeCount} 节点 / ${formatCPUMilli(summary.cpuAllocatableMilli)} / ${formatBytes(summary.memoryAllocatableBytes)} / ${summary.gpuAllocatable || 0} GPU`
  }
  const machine = info?.machine || {}
  const cpu = machine?.cpu?.logicalCores
  const memory = machine?.memory?.totalBytes
  const disk = getMachineDiskTotal(row)
  const gpu = machine?.gpu?.count || 0
  if (!cpu && !memory && !disk && !gpu) return row?.baseInfoStatus === 'pending' ? '采集中…' : row?.baseInfoStatus === 'failed' ? '采集失败' : '未采集'
  return `${cpu || '-'}C / ${formatBytes(memory)} / ${formatBytes(disk)} / ${gpu} GPU`
}
const getBaseInfoMeta = (row) => {
  if (row?.baseInfoStatus === 'failed') return row?.baseInfoLastError || '最近一次采集失败'
  if (row?.baseInfoCollectedAt) return `最近采集：${formatDateTime(row.baseInfoCollectedAt)}`
  if (row?.baseInfoStatus === 'pending') return '执行器正在采集基础资源信息'
  return '尚未采集基础资源信息'
}
const baseInfoDialogStatusType = computed(() => {
  if (baseInfoDialogResource.value?.baseInfoStatus === 'success') return 'success'
  if (baseInfoDialogResource.value?.baseInfoStatus === 'failed') return 'warning'
  if (baseInfoDialogResource.value?.baseInfoStatus === 'pending') return 'info'
  return 'info'
})

onMounted(fetchResources)
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.resource-page {
  padding: $space-6;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: $space-4;
  margin-bottom: $space-5;

  h2 {
    margin: 0 0 $space-2;
    font-family: $font-family-display;
    font-size: 30px;
    font-weight: 760;
    letter-spacing: -0.03em;
    color: var(--text-primary);
  }

  p {
    margin: 0;
    max-width: 760px;
    color: var(--text-muted);
    line-height: 1.7;
  }
}

.filter-bar {
  display: flex;
  gap: $space-3;
  flex-wrap: wrap;
  margin-bottom: $space-4;
}

.search-input {
  width: 280px;
}

.filter-select {
  width: 160px;
}

.binding-cell {
  display: flex;
  flex-direction: column;
  gap: $space-1;
}

.binding-name {
  color: var(--text-primary);
  font-weight: 600;
}

.binding-meta {
  color: var(--text-muted);
  font-size: 12px;
}

.base-info-detail {
  display: flex;
  flex-direction: column;
  gap: $space-4;
}

.base-info-alert {
  margin-bottom: $space-2;
}

@media (max-width: 768px) {
  .resource-page {
    padding: $space-4;
  }

  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .search-input,
  .filter-select {
    width: 100%;
  }
}
</style>
