<template>
  <div class="resource-form">
    <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="资源名称" prop="name">
            <el-input v-model="form.name" placeholder="请输入资源名称" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="资源类型" prop="type">
            <el-select v-model="form.type" placeholder="选择资源类型" style="width: 100%">
              <el-option label="VM" value="vm" />
              <el-option label="K8s 集群" value="k8s" />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>

      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="环境" prop="environment">
            <el-select v-model="form.environment" placeholder="选择环境" style="width: 100%">
              <el-option label="开发环境" value="development" />
              <el-option label="测试环境" value="testing" />
              <el-option label="生产环境" value="production" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item :label="endpointLabel" prop="endpoint">
            <el-input v-model="form.endpoint" :placeholder="endpointPlaceholder" />
          </el-form-item>
        </el-col>
      </el-row>

      <el-alert v-if="isVmMode" type="info" :closable="false" show-icon class="section-alert">
        VM 资源会绑定登录凭据，发布与执行工具将直接复用这里维护的主机地址与认证摘要。
      </el-alert>
      <el-alert v-else type="info" :closable="false" show-icon class="section-alert">
        K8s 资源的认证信息来自所选 Kubernetes 凭据；若未填写 API Server，会优先使用凭据中的 server / api_server。
      </el-alert>

      <el-divider content-position="left">接入凭据</el-divider>

      <el-form-item :label="credentialLabel" prop="credentialId">
        <CredentialSelector
          v-model="form.credentialId"
          :placeholder="credentialPlaceholder"
          :credential-types="credentialTypes"
          :credential-categories="credentialCategories"
          :create-route-query="createRouteQuery"
          @change="handleCredentialChange"
        />
      </el-form-item>

      <div v-if="currentAccessSummary.length > 0" class="access-summary">
        <div v-for="item in currentAccessSummary" :key="item.label" class="summary-item">
          <span class="summary-label">{{ item.label }}</span>
          <span class="summary-value">{{ item.value }}</span>
        </div>
      </div>

      <template v-if="isEdit">
        <el-divider content-position="left">基础资源信息</el-divider>
        <el-alert
          :type="baseInfoStatusType"
          :closable="false"
          show-icon
          class="section-alert"
        >
          {{ baseInfoStatusText }}
        </el-alert>
        <div v-if="baseInfoCards.length > 0" class="access-summary">
          <div v-for="item in baseInfoCards" :key="item.label" class="summary-item">
            <span class="summary-label">{{ item.label }}</span>
            <span class="summary-value">{{ item.value }}</span>
          </div>
        </div>
      </template>

      <el-form-item label="描述" prop="description">
        <el-input v-model="form.description" type="textarea" :rows="3" placeholder="可选：补充资源用途、维护人或访问约束" />
      </el-form-item>

      <div class="actions">
        <el-button @click="emit('cancel')">取消</el-button>
        <el-button v-if="!isEdit" :loading="verifying" @click="handleValidateConnection">验证连接</el-button>
        <el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="handleSubmit">{{ isEdit ? '保存更改' : '创建资源' }}</el-button>
      </div>

      <el-alert
        v-if="!isEdit && validationState.message"
        :type="validationState.status === 'success' ? 'success' : validationState.status === 'running' ? 'info' : 'warning'"
        :closable="false"
        show-icon
        class="validation-alert"
      >
        {{ validationState.message }}
      </el-alert>
    </el-form>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getCredentialList } from '@/api/credential'
import { verifyResourceConnection } from '@/api/resource'
import { getTaskDetail } from '@/api/task'
import CredentialSelector from '@/views/pipeline/components/CredentialSelector.vue'

const props = defineProps({
  initialData: {
    type: Object,
    default: null
  },
  submitting: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['submit', 'cancel'])

const formRef = ref(null)
const credentialOptions = ref([])
const verifying = ref(false)
const validationState = reactive({
  status: 'idle',
  taskId: 0,
  validatedKey: '',
  message: ''
})

const form = reactive({
  name: '',
  type: 'vm',
  environment: 'development',
  endpoint: '',
  description: '',
  credentialId: null,
  labels: {},
  metadata: {}
})

const isEdit = computed(() => !!props.initialData?.id)
const isVmMode = computed(() => form.type === 'vm')
const credentialMap = computed(() => Object.fromEntries(credentialOptions.value.map(item => [String(item.id), item])))
const currentCredential = computed(() => form.credentialId ? credentialMap.value[String(form.credentialId)] || null : null)
const currentCredentialSummary = computed(() => normalizeObjectField(currentCredential.value?.summary))
const currentBaseInfo = computed(() => normalizeObjectField(props.initialData?.baseInfo || props.initialData?.base_info))
const currentBaseInfoStatus = computed(() => props.initialData?.baseInfoStatus || props.initialData?.base_info_status || '')
const currentBaseInfoCollectedAt = computed(() => props.initialData?.baseInfoCollectedAt || props.initialData?.base_info_collected_at || 0)
const currentBaseInfoLastError = computed(() => props.initialData?.baseInfoLastError || props.initialData?.base_info_last_error || '')
const endpointLabel = computed(() => isVmMode.value ? '接入地址' : 'API Server')
const endpointPlaceholder = computed(() => isVmMode.value ? '如 10.0.0.21:22' : '可选；为空时优先使用凭据中的 server / api_server')
const credentialLabel = computed(() => isVmMode.value ? '登录凭据' : 'Kubernetes 凭据')
const credentialPlaceholder = computed(() => isVmMode.value ? '选择登录凭据' : '选择 Kubernetes 凭据')
const credentialTypes = computed(() => isVmMode.value ? ['PASSWORD', 'SSH_KEY'] : ['TOKEN', 'CERTIFICATE'])
const credentialCategories = computed(() => isVmMode.value ? [] : ['kubernetes', 'custom'])
const createRouteQuery = computed(() => isVmMode.value
  ? { category: 'custom', source: 'resource-vm' }
  : { type: 'TOKEN', category: 'kubernetes', source: 'resource-k8s' }
)
const effectiveEndpoint = computed(() => (isVmMode.value ? form.endpoint : form.endpoint || resolveKubernetesServer(currentCredentialSummary.value) || '').trim())
const validationDraftKey = computed(() => JSON.stringify({
  type: form.type,
  endpoint: effectiveEndpoint.value,
  credentialId: form.credentialId || 0
}))
const canSubmit = computed(() => isEdit.value || (validationState.status === 'success' && validationState.validatedKey === validationDraftKey.value))

const rules = computed(() => ({
  name: [{ required: true, message: '请输入资源名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择资源类型', trigger: 'change' }],
  environment: [{ required: true, message: '请选择环境', trigger: 'change' }],
  credentialId: [{ required: true, message: isVmMode.value ? '请选择登录凭据' : '请选择 Kubernetes 凭据', trigger: 'change' }],
  endpoint: isVmMode.value ? [{ required: true, message: '请输入 VM 接入地址', trigger: 'blur' }] : []
}))

const currentAccessSummary = computed(() => {
  if (!form.credentialId) return []

  if (isVmMode.value) {
    const authType = currentCredential.value?.type === 'SSH_KEY'
      ? 'SSH 密钥'
      : currentCredential.value?.type === 'PASSWORD'
        ? '用户名 + 密码'
        : '未识别'

    const authDetail = currentCredential.value?.type === 'SSH_KEY'
      ? { label: '密钥算法', value: currentCredentialSummary.value?.key_type || '未声明' }
      : { label: '登录用户', value: currentCredentialSummary.value?.username || '未声明' }

    return [
      { label: '接入地址', value: form.endpoint || '-' },
      { label: '认证方式', value: authType },
      { label: '锁定状态', value: getLockStateLabel(currentCredential.value?.lock_state) },
      { label: '凭据名称', value: currentCredential.value?.name || `凭据 #${form.credentialId}` },
      authDetail
    ]
  }

  return [
    { label: 'API Server', value: resolveKubernetesServer(currentCredentialSummary.value) || form.endpoint || '-' },
    { label: '命名空间', value: currentCredentialSummary.value?.namespace || 'default / 未声明' },
    { label: '认证方式', value: resolveKubernetesAuthMode(currentCredentialSummary.value, currentCredential.value) },
    { label: '锁定状态', value: getLockStateLabel(currentCredential.value?.lock_state) },
    { label: '凭据名称', value: currentCredential.value?.name || `凭据 #${form.credentialId}` }
  ]
})

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
  return Number.isInteger(cores) ? `${cores}` : cores.toFixed(1)
}
const baseInfoStatusType = computed(() => {
  if (currentBaseInfoStatus.value === 'success') return 'success'
  if (currentBaseInfoStatus.value === 'pending') return 'info'
  if (currentBaseInfoStatus.value === 'failed') return 'warning'
  return 'info'
})
const baseInfoStatusText = computed(() => {
  if (currentBaseInfoStatus.value === 'success') {
    return `最近采集时间：${formatDateTime(currentBaseInfoCollectedAt.value)}`
  }
  if (currentBaseInfoStatus.value === 'pending') {
    return '执行器正在采集基础资源信息，请稍后关闭弹窗后刷新列表查看结果。'
  }
  if (currentBaseInfoStatus.value === 'failed') {
    return currentBaseInfoLastError.value || '最近一次基础资源信息采集失败。'
  }
  return '当前资源尚未采集基础资源信息。'
})
const baseInfoCards = computed(() => {
  const info = currentBaseInfo.value || {}
  if (form.type === 'k8s') {
    const summary = info?.k8s?.summary || {}
    if (!summary.nodeCount) return []
    return [
      { label: '节点数', value: `${summary.nodeCount}` },
      { label: '可分配 CPU', value: `${formatCPUMilli(summary.cpuAllocatableMilli)} CPU` },
      { label: '可分配内存', value: formatBytes(summary.memoryAllocatableBytes) },
      { label: '可分配 GPU', value: `${summary.gpuAllocatable || 0}` }
    ]
  }
  const machine = info?.machine || {}
  return [
    { label: '主机名', value: machine?.hostname || '-' },
    { label: 'CPU', value: machine?.cpu?.logicalCores ? `${machine.cpu.logicalCores} 核` : '-' },
    { label: '内存', value: formatBytes(machine?.memory?.totalBytes) },
    { label: '磁盘', value: formatBytes(machine?.storage?.totalDiskBytes) },
    { label: 'GPU', value: `${machine?.gpu?.count || 0}` },
    { label: '系统', value: [machine?.os?.name, machine?.arch].filter(Boolean).join(' / ') || '-' }
  ]
})

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

const applyInitialData = (value) => {
  form.name = value?.name || ''
  form.type = value?.type || 'vm'
  form.environment = value?.environment || 'development'
  form.endpoint = value?.endpoint || ''
  form.description = value?.description || ''
  form.credentialId = value?.credentialId || null
  form.labels = normalizeObjectField(value?.labels)
  form.metadata = normalizeObjectField(value?.metadata)
}

const loadCredentialOptions = async () => {
  const pageSize = 200
  let page = 1
  let total = Infinity
  const items = []

  while (items.length < total && page <= 20) {
    const res = await getCredentialList({ page, size: pageSize })
    const list = Array.isArray(res.data?.list) ? res.data.list : []
    total = Number(res.data?.total || list.length)
    items.push(...list)
    if (list.length < pageSize) break
    page += 1
  }

  credentialOptions.value = items
}

const resolveKubernetesServer = (summary) => summary?.server || summary?.api_server || ''

const resolveKubernetesAuthMode = (summary, credential) => {
  if (summary?.auth_mode === 'kubeconfig') return 'Kubeconfig'
  if (summary?.auth_mode === 'server_token') return 'Server + Token'
  if (summary?.auth_mode === 'server_cert') return '客户端证书'
  if (credential?.type === 'TOKEN') return 'Token'
  if (credential?.type === 'CERTIFICATE') return '客户端证书'
  return '未识别'
}

const getLockStateLabel = (lockState) => lockState === 'unlocked' ? '已解锁' : '已锁定'

const hasEmbeddedKubernetesConfig = (summary) => summary?.auth_mode === 'kubeconfig'

const getCredentialSummary = (credentialId) => normalizeObjectField(credentialMap.value[String(credentialId)]?.summary)

const syncSelectedCredential = async (credentialId, options = {}) => {
  if (!credentialId) return
  const { silent = false } = options
  try {
    if (!credentialMap.value[String(credentialId)]) {
      await loadCredentialOptions()
    }
    const summary = getCredentialSummary(credentialId)
    if (!isVmMode.value && !form.endpoint && resolveKubernetesServer(summary)) {
      form.endpoint = resolveKubernetesServer(summary)
    }
  } catch {
    if (!silent) {
      ElMessage.warning('凭据已选择，但访问摘要暂时无法加载')
    }
  }
}

const handleCredentialChange = async (credentialId) => {
  await syncSelectedCredential(credentialId)
}

const resetValidationState = () => {
  if (isEdit.value) return
  validationState.status = 'idle'
  validationState.taskId = 0
  validationState.validatedKey = ''
  validationState.message = ''
}

const hydrateForm = async (value) => {
  applyInitialData(value)
  try {
    await loadCredentialOptions()
  } catch {
    ElMessage.warning('凭据列表加载失败，请稍后重试')
  }

  if (value?.credentialId) {
    await syncSelectedCredential(value.credentialId, { silent: true })
  }
}

watch(
  () => props.initialData,
  value => {
    hydrateForm(value).catch(() => {})
  },
  { immediate: true }
)

watch(
  () => form.type,
  () => {
    resetValidationState()
    if (!isVmMode.value && !form.endpoint && resolveKubernetesServer(currentCredentialSummary.value)) {
      form.endpoint = resolveKubernetesServer(currentCredentialSummary.value)
    }
  }
)

watch(() => form.endpoint, () => resetValidationState())
watch(() => form.credentialId, () => resetValidationState())

const validateConnectionFields = () => {
  if (!form.credentialId) {
    ElMessage.warning(isVmMode.value ? '请选择登录凭据' : '请选择 Kubernetes 凭据')
    return false
  }
  if (isVmMode.value && !effectiveEndpoint.value) {
    ElMessage.warning('请输入 VM 接入地址')
    return false
  }
  if (!isVmMode.value && !effectiveEndpoint.value && !hasEmbeddedKubernetesConfig(currentCredentialSummary.value)) {
    ElMessage.warning('请填写 API Server 或选择内含 kubeconfig / server 的 Kubernetes 凭据')
    return false
  }
  return true
}

const waitForValidationTask = async (taskId) => {
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
  return { ok: false, task: { error_msg: '连接验证超时，请稍后重试' } }
}

const handleValidateConnection = async () => {
  if (verifying.value || props.submitting) return
  if (!validateConnectionFields()) return
  verifying.value = true
  validationState.status = 'running'
  validationState.message = '正在通过执行器验证资源连接，请稍候…'
  validationState.taskId = 0
  validationState.validatedKey = ''
  try {
    const verifyRes = await verifyResourceConnection({
      type: form.type,
      endpoint: effectiveEndpoint.value,
      credential_id: form.credentialId
    })
    const taskId = Number(verifyRes?.data?.task_id || 0)
    if (!taskId) {
      throw new Error('未拿到验证任务 ID')
    }
    validationState.taskId = taskId
    const result = await waitForValidationTask(taskId)
    if (result.ok) {
      validationState.status = 'success'
      validationState.validatedKey = validationDraftKey.value
      validationState.message = '连接验证通过，可以保存资源。'
      ElMessage.success('连接验证通过')
      return
    }
    validationState.status = 'failed'
    validationState.message = result.task?.error_msg || '连接验证失败，请检查资源地址、凭据和执行器连通性。'
    ElMessage.warning(validationState.message)
  } catch (error) {
    validationState.status = 'failed'
    validationState.message = error?.response?.data?.message || error?.message || '连接验证失败，请稍后重试'
    ElMessage.error(validationState.message)
  } finally {
    verifying.value = false
  }
}

const handleSubmit = async () => {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid || props.submitting) return
  if (!isEdit.value) {
    if (validationState.status !== 'success' || validationState.validatedKey !== validationDraftKey.value || !validationState.taskId) {
      ElMessage.warning('请先完成连接验证，验证通过后才能创建资源')
      return
    }
  }

  emit('submit', {
    name: form.name,
    type: form.type,
    environment: form.environment,
    endpoint: (isVmMode.value ? form.endpoint : form.endpoint || resolveKubernetesServer(currentCredentialSummary.value) || '').trim(),
    description: form.description,
    credentialId: form.credentialId,
    verificationTaskId: isEdit.value ? 0 : validationState.taskId,
    labels: normalizeObjectField(form.labels),
    metadata: normalizeObjectField(form.metadata),
  })
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.resource-form {
  padding: 0 $space-2;
}

.section-alert {
  margin-bottom: $space-4;
}

.access-summary {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: $space-3;
  margin-bottom: $space-4;
}

.summary-item {
  display: flex;
  flex-direction: column;
  gap: $space-1;
  padding: $space-3;
  border-radius: $radius-md;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color-light);
}

.summary-label {
  color: var(--text-muted);
  font-size: 12px;
}

.summary-value {
  color: var(--text-primary);
  font-weight: 600;
  line-height: 1.6;
  word-break: break-all;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: $space-3;
  margin-top: $space-6;
}

.validation-alert {
  margin-top: $space-4;
}

@media (max-width: 768px) {
  .access-summary {
    grid-template-columns: 1fr;
  }

  :deep(.el-col) {
    max-width: 100%;
    flex: 0 0 100%;
  }
}
</style>
