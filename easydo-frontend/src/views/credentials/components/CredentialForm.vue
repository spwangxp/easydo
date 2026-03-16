<template>
  <div class="credential-form">
    <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="凭据名称" prop="name">
            <el-input v-model="form.name" placeholder="请输入凭据名称" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="凭据类型" prop="type">
            <el-select v-model="form.type" placeholder="选择凭据类型" style="width: 100%" @change="handleTypeChange">
              <el-option v-for="type in types" :key="type.value" :label="type.label" :value="type.value" />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>

      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="分类" prop="category">
            <el-select v-model="form.category" placeholder="选择分类" style="width: 100%">
              <el-option v-for="category in categories" :key="category.value" :label="category.label" :value="category.value" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="范围" prop="scope">
            <el-select v-model="form.scope" placeholder="选择范围" style="width: 100%">
              <el-option label="个人" value="personal" />
              <el-option label="项目" value="project" />
              <el-option label="工作空间" value="workspace" />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>

      <el-form-item v-if="form.scope === 'project'" label="项目" prop="project_id">
        <el-select v-model="form.project_id" placeholder="选择项目" style="width: 100%">
          <el-option v-for="project in projects" :key="project.id" :label="project.name" :value="project.id" />
        </el-select>
      </el-form-item>

      <el-form-item label="描述" prop="description">
        <el-input v-model="form.description" type="textarea" :rows="2" placeholder="请输入凭据描述" />
      </el-form-item>

      <el-divider content-position="left">敏感数据</el-divider>

      <div v-if="form.type === 'PASSWORD'">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="用户名" prop="payload.username">
              <el-input v-model="form.payload.username" placeholder="请输入用户名" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="密码" prop="payload.password">
              <el-input v-model="form.payload.password" type="password" show-password placeholder="请输入密码" />
            </el-form-item>
          </el-col>
        </el-row>
      </div>

      <div v-else-if="form.type === 'SSH_KEY'">
        <el-form-item label="私钥" prop="payload.private_key">
          <el-input v-model="form.payload.private_key" type="textarea" :rows="6" placeholder="请输入私钥内容" />
        </el-form-item>
        <el-form-item label="公钥" prop="payload.public_key">
          <el-input v-model="form.payload.public_key" type="textarea" :rows="3" placeholder="可选：请输入公钥内容" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="凭据算法" prop="payload.key_type">
              <el-select v-model="form.payload.key_type" style="width: 100%">
                <el-option label="RSA" value="rsa" />
                <el-option label="Ed25519" value="ed25519" />
                <el-option label="ECDSA" value="ecdsa" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="私钥密码" prop="payload.passphrase">
              <el-input v-model="form.payload.passphrase" type="password" show-password placeholder="可选：输入私钥密码" />
            </el-form-item>
          </el-col>
        </el-row>
      </div>

      <div v-else-if="form.type === 'TOKEN'">
        <el-form-item label="令牌值" prop="payload.token">
          <el-input v-model="form.payload.token" type="password" show-password placeholder="请输入令牌" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="令牌类型" prop="payload.token_type">
              <el-select v-model="form.payload.token_type" style="width: 100%">
                <el-option label="Bearer" value="bearer" />
                <el-option label="Basic" value="basic" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="用户名" prop="payload.username">
              <el-input v-model="form.payload.username" placeholder="可选：Basic 认证用户名" />
            </el-form-item>
          </el-col>
        </el-row>
      </div>

      <div v-else-if="form.type === 'OAUTH2'">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="Client ID" prop="payload.client_id">
              <el-input v-model="form.payload.client_id" placeholder="请输入 Client ID" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Client Secret" prop="payload.client_secret">
              <el-input v-model="form.payload.client_secret" type="password" show-password placeholder="请输入 Client Secret" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="Provider URL" prop="payload.provider_url">
          <el-input v-model="form.payload.provider_url" placeholder="https://provider.example.com" />
        </el-form-item>
        <el-form-item label="Access Token" prop="payload.access_token">
          <el-input v-model="form.payload.access_token" type="password" show-password placeholder="可选：预先获取的 Access Token" />
        </el-form-item>
      </div>

      <div v-else-if="form.type === 'CERTIFICATE'">
        <el-form-item label="证书 PEM" prop="payload.cert_pem">
          <el-input v-model="form.payload.cert_pem" type="textarea" :rows="5" placeholder="请输入证书 PEM" />
        </el-form-item>
        <el-form-item label="私钥 PEM" prop="payload.key_pem">
          <el-input v-model="form.payload.key_pem" type="textarea" :rows="5" placeholder="请输入私钥 PEM" />
        </el-form-item>
        <el-form-item label="CA 证书" prop="payload.ca_cert">
          <el-input v-model="form.payload.ca_cert" type="textarea" :rows="3" placeholder="可选：请输入 CA 证书" />
        </el-form-item>
      </div>

      <div v-else-if="form.type === 'IAM_ROLE'">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="云平台" prop="payload.provider">
              <el-select v-model="form.payload.provider" style="width: 100%">
                <el-option label="AWS" value="aws" />
                <el-option label="Google Cloud" value="gcp" />
                <el-option label="Azure" value="azure" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Region" prop="payload.region">
              <el-input v-model="form.payload.region" placeholder="可选：如 us-east-1" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="角色 ARN" prop="payload.role_arn">
          <el-input v-model="form.payload.role_arn" placeholder="请输入角色 ARN 或服务账号标识" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="Access Key ID" prop="payload.access_key_id">
              <el-input v-model="form.payload.access_key_id" placeholder="可选：临时 Access Key" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Secret Access Key" prop="payload.secret_access_key">
              <el-input v-model="form.payload.secret_access_key" type="password" show-password placeholder="可选：临时 Secret Key" />
            </el-form-item>
          </el-col>
        </el-row>
      </div>

      <el-divider content-position="left">高级选项</el-divider>

      <el-form-item label="过期时间" prop="expires_at">
        <el-date-picker v-model="expiresAt" type="datetime" placeholder="可选：选择过期时间" style="width: 100%" :disabled-date="disabledDate" />
      </el-form-item>

      <div class="actions">
        <el-button @click="emit('cancel')">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ isEdit ? '保存更改' : '创建凭据' }}</el-button>
      </div>
    </el-form>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getProjectList } from '@/api/project'

const props = defineProps({
  initialData: { type: Object, default: null },
  types: { type: Array, default: () => [] },
  categories: { type: Array, default: () => [] }
})

const emit = defineEmits(['submit', 'cancel'])

const formRef = ref(null)
const submitting = ref(false)
const projects = ref([])
const expiresAt = ref(null)

const form = reactive({
  name: '',
  description: '',
  type: '',
  category: '',
  scope: 'workspace',
  project_id: null,
  expires_at: null,
  payload: {}
})

const isEdit = computed(() => !!props.initialData?.id)

const rules = computed(() => {
  const base = {
    name: [{ required: true, message: '请输入凭据名称', trigger: 'blur' }],
    type: [{ required: true, message: '请选择凭据类型', trigger: 'change' }],
    category: [{ required: true, message: '请选择分类', trigger: 'change' }],
    scope: [{ required: true, message: '请选择范围', trigger: 'change' }],
    project_id: form.scope === 'project' ? [{ required: true, message: '请选择项目', trigger: 'change' }] : []
  }

  if (form.type === 'PASSWORD') {
    base['payload.username'] = [{ required: true, message: '请输入用户名', trigger: 'blur' }]
    base['payload.password'] = [{ required: true, message: '请输入密码', trigger: 'blur' }]
  }
  if (form.type === 'SSH_KEY') {
    base['payload.private_key'] = [{ required: true, message: '请输入私钥', trigger: 'blur' }]
  }
  if (form.type === 'TOKEN') {
    base['payload.token'] = [{ required: true, message: '请输入令牌值', trigger: 'blur' }]
  }
  if (form.type === 'OAUTH2') {
    base['payload.client_id'] = [{ required: true, message: '请输入 Client ID', trigger: 'blur' }]
    base['payload.client_secret'] = [{ required: true, message: '请输入 Client Secret', trigger: 'blur' }]
    base['payload.provider_url'] = [{ required: true, message: '请输入 Provider URL', trigger: 'blur' }]
  }
  if (form.type === 'CERTIFICATE') {
    base['payload.cert_pem'] = [{ required: true, message: '请输入证书 PEM', trigger: 'blur' }]
    base['payload.key_pem'] = [{ required: true, message: '请输入私钥 PEM', trigger: 'blur' }]
  }
  if (form.type === 'IAM_ROLE') {
    base['payload.provider'] = [{ required: true, message: '请选择云平台', trigger: 'change' }]
    base['payload.role_arn'] = [{ required: true, message: '请输入角色 ARN', trigger: 'blur' }]
  }

  return base
})

function defaultPayloadByType(type) {
  if (type === 'PASSWORD') return { username: '', password: '' }
  if (type === 'SSH_KEY') return { private_key: '', public_key: '', key_type: 'rsa', passphrase: '' }
  if (type === 'TOKEN') return { token: '', token_type: 'bearer', username: '' }
  if (type === 'OAUTH2') return { client_id: '', client_secret: '', provider_url: '', access_token: '' }
  if (type === 'CERTIFICATE') return { cert_pem: '', key_pem: '', ca_cert: '' }
  if (type === 'IAM_ROLE') return { provider: 'aws', role_arn: '', region: '', access_key_id: '', secret_access_key: '' }
  return {}
}

function handleTypeChange(value) {
  form.payload = defaultPayloadByType(value)
  if (!props.initialData) {
    form.category = ''
  }
}

function disabledDate(date) {
  return date.getTime() < Date.now() - 86400000
}

async function loadProjects() {
  try {
    const res = await getProjectList({ page: 1, page_size: 200 })
    const payload = res?.data
    projects.value = Array.isArray(payload?.list) ? payload.list : Array.isArray(payload) ? payload : []
  } catch (error) {
    projects.value = []
  }
}

watch(
  () => props.initialData,
  value => {
    form.name = value?.name || ''
    form.description = value?.description || ''
    form.type = value?.type || ''
    form.category = value?.category || ''
    form.scope = value?.scope || 'workspace'
    form.project_id = value?.project_id || null
    form.payload = { ...defaultPayloadByType(value?.type || ''), ...(value?.payload || {}) }
    expiresAt.value = value?.expires_at ? new Date(value.expires_at * 1000) : null
  },
  { immediate: true }
)

loadProjects()

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    emit('submit', {
      name: form.name,
      description: form.description,
      type: form.type,
      category: form.category,
      scope: form.scope,
      project_id: form.scope === 'project' ? form.project_id : 0,
      expires_at: expiresAt.value ? Math.floor(expiresAt.value.getTime() / 1000) : null,
      payload: form.payload
    })
  } catch (error) {
    ElMessage.error('提交凭据失败')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.credential-form {
  padding: 0 8px;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
}

:deep(.el-textarea__inner) {
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
}
</style>
