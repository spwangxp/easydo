<template>
  <div class="credential-selector">
    <el-select
      v-model="selectedId"
      placeholder="选择凭据"
      clearable
      style="width: 100%"
      @change="handleChange"
    >
      <el-option
        v-for="cred in filteredCredentials"
        :key="cred.id"
        :label="cred.name"
        :value="cred.id"
      >
        <div style="display: flex; align-items: center; gap: 8px;">
          <el-icon :size="16">
            <component :is="getTypeIcon(cred.type)" />
          </el-icon>
          <span>{{ cred.name }}</span>
          <el-tag size="small" :type="getStatusType(cred.status)">{{ getTypeLabel(cred.type) }}</el-tag>
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
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
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
  credentialCategory: {
    type: String,
    default: null
  }
})

const emit = defineEmits(['update:modelValue', 'change'])

const router = useRouter()
const credentials = ref([])
const loading = ref(false)
const selectedId = computed({
  get: () => props.modelValue,
  set: (val) => {
    emit('update:modelValue', val)
  }
})

const filteredCredentials = computed(() => {
  return credentials.value.filter(cred => {
    if (cred.status !== 'active') return false
    if (props.credentialType && cred.type !== props.credentialType) return false
    if (props.credentialCategory && cred.category !== props.credentialCategory) return false
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

onMounted(() => {
  loadCredentials()
})

async function loadCredentials() {
  loading.value = true
  try {
    const res = await getCredentialList({ page: 1, size: 100 })
    if (res.code === 200) {
      credentials.value = res.data.list
    }
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
  router.push('/secrets')
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
</script>

<style scoped>
.credential-selector {
  width: 100%;
}

.credential-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}
</style>
