<template>
  <div class="credential-selector">
    <el-select
      v-model="selectedId"
      :loading="loading"
      :placeholder="placeholder"
      clearable
      filterable
      style="width: 100%"
      @change="handleChange"
    >
      <el-option
        v-for="cred in filteredCredentials"
        :key="cred.id"
        :label="cred.name"
        :value="cred.id"
      >
        <div class="credential-option">
          <div class="credential-option__icon">
            <el-icon :size="16">
              <component :is="getTypeIcon(cred.type)" />
            </el-icon>
          </div>
          <div class="credential-option__body">
            <div class="credential-option__header">
              <span class="credential-option__name">{{ cred.name }}</span>
              <div class="credential-option__tags">
                <el-tag size="small" effect="plain">{{ getTypeLabel(cred.type) }}</el-tag>
                <el-tag size="small" :type="getStatusType(cred.status)">{{ getStatusLabel(cred.status) }}</el-tag>
                <el-tag size="small" :type="getLockStateType(cred.lock_state)">{{ getLockStateLabel(cred.lock_state) }}</el-tag>
              </div>
            </div>
            <div class="credential-option__meta">{{ getCredentialMeta(cred) }}</div>
          </div>
        </div>
      </el-option>
    </el-select>

    <div class="credential-actions">
      <el-button type="primary" link size="small" @click="refreshCredentials">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
      <el-button type="primary" link size="small" @click="goToCredentials">
        <el-icon><Plus /></el-icon>
        新建凭据
      </el-button>
    </div>

    <div class="credential-summary" :class="{ empty: !selectedCredential }">
      <template v-if="selectedCredential">
        <div class="credential-summary__icon">
          <el-icon :size="18">
            <component :is="getTypeIcon(selectedCredential.type)" />
          </el-icon>
        </div>
        <div class="credential-summary__content">
          <div class="credential-summary__title">{{ selectedCredential.name }}</div>
          <div class="credential-summary__meta">{{ getCredentialMeta(selectedCredential) }}</div>
        </div>
        <div class="credential-summary__tags">
          <el-tag effect="plain">{{ getCategoryLabel(selectedCredential.category) }}</el-tag>
          <el-tag :type="getStatusType(selectedCredential.status)">{{ getStatusLabel(selectedCredential.status) }}</el-tag>
          <el-tag :type="getLockStateType(selectedCredential.lock_state)">{{ getLockStateLabel(selectedCredential.lock_state) }}</el-tag>
        </div>
      </template>
      <template v-else>
        <span class="credential-summary__placeholder">选择后会在这里展示凭据类型、锁定状态与安全摘要。</span>
      </template>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh, Plus, Lock, Key, Ticket, Connection, Document } from '@element-plus/icons-vue'
import { getCredentialList } from '@/api/credential'

const props = defineProps({
  modelValue: {
    type: [Number, String],
    default: null
  },
  credentialType: {
    type: String,
    default: null
  },
  credentialTypes: {
    type: Array,
    default: () => []
  },
  credentialCategory: {
    type: String,
    default: null
  },
  credentialCategories: {
    type: Array,
    default: () => []
  },
  placeholder: {
    type: String,
    default: '选择凭据'
  },
  createRouteQuery: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['update:modelValue', 'change', 'invalid-selection'])

const router = useRouter()
const credentials = ref([])
const loading = ref(false)

const selectedId = computed({
  get: () => props.modelValue,
  set: (val) => {
    emit('update:modelValue', val)
  }
})

const selectedCredential = computed(() => credentials.value.find(item => String(item.id) === String(props.modelValue)) || null)

const filteredCredentials = computed(() => {
  const typeSet = new Set((props.credentialTypes || []).map(v => String(v)))
  const categorySet = new Set((props.credentialCategories || []).map(v => String(v)))

  return credentials.value.filter(cred => {
    if (cred.status !== 'active') return false
    if (props.credentialType && cred.type !== props.credentialType) return false
    if (typeSet.size > 0 && !typeSet.has(String(cred.type))) return false
    if (props.credentialCategory && cred.category !== props.credentialCategory) return false
    if (categorySet.size > 0 && !categorySet.has(String(cred.category))) return false
    return true
  })
})

const typeIconMap = {
  PASSWORD: Lock,
  SSH_KEY: Key,
  TOKEN: Ticket,
  OAUTH2: Connection,
  CERTIFICATE: Document
}

const typeLabelMap = {
  PASSWORD: '密码',
  SSH_KEY: 'SSH',
  TOKEN: 'Token',
  OAUTH2: 'OAuth2',
  CERTIFICATE: '证书'
}

const statusTypeMap = {
  active: 'success',
  inactive: 'info',
  expired: 'warning',
  revoked: 'danger'
}

const statusLabelMap = {
  active: '可用',
  inactive: '已禁用',
  expired: '已过期',
  revoked: '已撤销'
}

const categoryLabelMap = {
  custom: '通用',
  kubernetes: 'Kubernetes',
  aws: 'AWS',
  gcp: 'GCP',
  azure: 'Azure'
}

const scopeLabelMap = {
  personal: '个人范围',
  project: '项目范围',
  workspace: '工作空间范围'
}

onMounted(() => {
  loadCredentials()
})

watch(
  () => [props.modelValue, props.credentialType, JSON.stringify(props.credentialTypes || []), props.credentialCategory, JSON.stringify(props.credentialCategories || []), credentials.value.length],
  () => {
    if (!props.modelValue) {
      return
    }
    const current = credentials.value.find(item => String(item.id) === String(props.modelValue))
    if (!current) {
      return
    }
    const stillAllowed = filteredCredentials.value.some(item => String(item.id) === String(props.modelValue))
    if (!stillAllowed) {
      emit('invalid-selection', current)
      emit('update:modelValue', null)
    }
  },
  { deep: true }
)

async function loadCredentials() {
  loading.value = true
  try {
    const pageSize = 200
    let page = 1
    let total = Infinity
    const items = []

    while (items.length < total && page <= 20) {
      const res = await getCredentialList({ page, size: pageSize })
      if (res.code !== 200) break
      const list = Array.isArray(res.data?.list) ? res.data.list : []
      total = Number(res.data?.total || list.length)
      items.push(...list)
      if (list.length < pageSize) break
      page += 1
    }
    credentials.value = items
  } catch (error) {
    console.error('加载凭据失败', error)
  } finally {
    loading.value = false
  }
}

function handleChange(val) {
  emit('change', val)
}

function refreshCredentials() {
  loadCredentials()
  ElMessage.success('凭据列表已刷新')
}

function goToCredentials() {
  router.push({
    path: '/credentials',
    query: {
      create: '1',
      ...props.createRouteQuery
    }
  })
}

function getTypeIcon(type) {
  return typeIconMap[type] || Document
}

function getTypeLabel(type) {
  return typeLabelMap[type] || type
}

function getStatusType(status) {
  return statusTypeMap[status] || 'info'
}

function getStatusLabel(status) {
  return statusLabelMap[status] || status || '未知状态'
}

function getLockStateType(lockState) {
  return lockState === 'unlocked' ? 'success' : 'warning'
}

function getLockStateLabel(lockState) {
  return lockState === 'unlocked' ? '已解锁' : '已锁定'
}

function getCategoryLabel(category) {
  return categoryLabelMap[category] || category || '未分类'
}

function getCredentialSummaryText(credential) {
  const summary = credential?.summary || {}

  if (summary.username) return `用户 ${summary.username}`
  if (summary.key_type) return `密钥 ${String(summary.key_type).toUpperCase()}`

  if (summary.auth_mode === 'kubeconfig') {
    return [summary.server || 'Kubeconfig', summary.namespace ? `命名空间 ${summary.namespace}` : ''].filter(Boolean).join(' · ')
  }

  if (summary.server || summary.namespace || summary.auth_mode) {
    const authModeLabel = summary.auth_mode === 'server_token'
      ? 'Server + Token'
      : summary.auth_mode === 'server_cert'
        ? '客户端证书'
        : summary.auth_mode || ''

    return [summary.server, summary.namespace ? `命名空间 ${summary.namespace}` : '', authModeLabel].filter(Boolean).join(' · ')
  }

  return ''
}

function getCredentialMeta(credential) {
  return [
    getCategoryLabel(credential.category),
    scopeLabelMap[credential.scope] || credential.scope || '工作空间范围',
    getLockStateLabel(credential.lock_state),
    getCredentialSummaryText(credential)
  ].filter(Boolean).join(' · ')
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.credential-selector {
  width: 100%;
}

.credential-option {
  display: flex;
  align-items: flex-start;
  gap: $space-3;
  padding: $space-2 0;
}

.credential-option__icon,
.credential-summary__icon {
  width: 36px;
  height: 36px;
  border-radius: $radius-md;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--primary-color);
  background: var(--primary-lighter);
  flex-shrink: 0;
}

.credential-option__body,
.credential-summary__content {
  min-width: 0;
  flex: 1;
}

.credential-option__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: $space-3;
}

.credential-option__name,
.credential-summary__title {
  color: var(--text-primary);
  font-weight: 700;
  line-height: 1.4;
}

.credential-option__tags,
.credential-summary__tags {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: $space-2;
}

.credential-option__meta,
.credential-summary__meta,
.credential-summary__placeholder {
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}

.credential-actions {
  display: flex;
  justify-content: flex-end;
  gap: $space-2;
  margin-top: $space-2;
}

.credential-summary {
  display: flex;
  align-items: center;
  gap: $space-3;
  margin-top: $space-3;
  padding: $space-3 $space-4;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: linear-gradient(145deg, rgba($primary-color, 0.08), rgba(255, 255, 255, 0.7));

  &.empty {
    background: var(--bg-secondary);
  }
}

@media (max-width: 768px) {
  .credential-option__header,
  .credential-summary {
    flex-direction: column;
    align-items: flex-start;
  }

  .credential-option__tags,
  .credential-summary__tags {
    justify-content: flex-start;
  }
}
</style>
