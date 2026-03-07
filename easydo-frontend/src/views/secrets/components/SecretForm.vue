<template>
  <div class="secret-form">
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-position="top"
    >
      <!-- 基本信息 -->
      <el-divider content-position="left">基本信息</el-divider>
      
      <el-row :gutter="20">
        <el-col :span="12">
          <el-form-item label="密钥名称" prop="name">
            <el-input v-model="form.name" placeholder="请输入密钥名称" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="使用范围" prop="scope">
            <el-select v-model="form.scope" placeholder="选择使用范围" style="width: 100%">
              <el-option label="个人" value="user" />
              <el-option label="项目" value="project" />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>
      
      <el-form-item label="描述" prop="description">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="2"
          placeholder="可选：输入密钥描述"
        />
      </el-form-item>
      
      <el-row :gutter="20">
         <el-col :span="12">
            <el-form-item label="密钥类型" prop="type">
              <el-select
                v-model="form.type"
                placeholder="选择密钥类型"
                style="width: 100%"
                @change="handleTypeChange"
              >
               <el-option
                 v-for="type in types"
                 :key="type.value"
                 :label="type.label"
                 :value="type.value"
               />
             </el-select>
           </el-form-item>
         </el-col>
        <el-col :span="12">
          <el-form-item label="分类" prop="category">
            <el-select v-model="form.category" placeholder="选择分类" style="width: 100%">
              <el-option
                v-for="cat in categories"
                :key="cat.value"
                :label="cat.label"
                :value="cat.value"
              />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>
      
      <!-- 敏感数据 -->
      <div class="section-header">
        <el-icon><Lock /></el-icon>
        <span>敏感数据</span>
      </div>

      <!-- 未选择类型时的提示 -->
      <template v-if="!credentialType">
        <el-alert type="info" :closable="false" show-icon>
          请先在上方选择「密钥类型」，然后填写对应的敏感数据。
        </el-alert>
      </template>

      <!-- 密码类型 -->
      <div v-if="credentialType === '密码'" class="credential-section">
        <el-form-item label="用户名" prop="secret_data.username">
          <el-input v-model="form.secret_data.username" placeholder="请输入用户名">
            <template #prefix>
              <el-icon><User /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item label="密码" prop="secret_data.password">
          <el-input
            v-model="form.secret_data.password"
            type="password"
            placeholder="请输入密码"
            show-password
          >
            <template #prefix>
              <el-icon><Lock /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item label="域（可选）" prop="secret_data.domain">
          <el-input v-model="form.secret_data.domain" placeholder="如: corp.example.com" />
        </el-form-item>
        <el-form-item label="端口（可选）" prop="secret_data.port">
          <el-input-number v-model="form.secret_data.port" :min="1" :max="65535" placeholder="服务端口" />
        </el-form-item>
      </div>
      
      <!-- SSH 密钥类型 -->
      <div v-if="credentialType === 'SSH 密钥'" class="credential-section">
        <el-form-item label="私钥" prop="secret_data.private_key">
          <el-input
            v-model="form.secret_data.private_key"
            type="textarea"
            :rows="6"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            font-family="monospace"
          />
        </el-form-item>
        <el-form-item label="公钥（可选）" prop="secret_data.public_key">
          <el-input
            v-model="form.secret_data.public_key"
            type="textarea"
            :rows="3"
            placeholder="ssh-rsa AAAA..."
          />
        </el-form-item>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="密钥类型" prop="secret_data.key_type">
              <el-select v-model="form.secret_data.key_type" placeholder="选择密钥类型" style="width: 100%">
                <el-option label="RSA" value="rsa" />
                <el-option label="Ed25519" value="ed25519" />
                <el-option label="ECDSA" value="ecdsa" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="密钥密码（可选）" prop="secret_data.passphrase">
              <el-input
                v-model="form.secret_data.passphrase"
                type="password"
                placeholder="如果私钥有密码保护"
                show-password
              />
            </el-form-item>
          </el-col>
        </el-row>
      </div>
      
      <!-- Token 类型 -->
      <div v-if="credentialType === 'API 令牌'" class="credential-section">
        <el-form-item label="令牌值" prop="secret_data.token">
          <el-input
            v-model="form.secret_data.token"
            type="password"
            placeholder="ghp_xxxxxxxxxxxx 或 glptt_xxxxxxxxxx"
            show-password
          >
            <template #prefix>
              <el-icon><Key /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="令牌类型" prop="secret_data.token_type">
              <el-select v-model="form.secret_data.token_type" placeholder="选择令牌类型" style="width: 100%">
                <el-option label="Bearer" value="bearer" />
                <el-option label="Basic" value="basic" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="权限范围（可选）" prop="secret_data.scopes">
              <el-select
                v-model="form.secret_data.scopes"
                multiple
                placeholder="选择权限范围"
                style="width: 100%"
              >
                <el-option label="repo" value="repo" />
                <el-option label="workflow" value="workflow" />
                <el-option label="admin:repo_hook" value="admin:repo_hook" />
                <el-option label="user" value="user" />
                <el-option label="read:org" value="read:org" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
      </div>
      
      <!-- OAuth2 类型 -->
      <div v-if="credentialType === 'OAuth2'" class="credential-section">
        <el-form-item label="Client ID" prop="secret_data.client_id">
          <el-input v-model="form.secret_data.client_id" placeholder="OAuth2 Client ID" />
        </el-form-item>
        <el-form-item label="Client Secret" prop="secret_data.client_secret">
          <el-input
            v-model="form.secret_data.client_secret"
            type="password"
            placeholder="OAuth2 Client Secret"
            show-password
          />
        </el-form-item>
        <el-form-item label="Provider URL" prop="secret_data.provider_url">
          <el-input v-model="form.secret_data.provider_url" placeholder="https://oauth.provider.com" />
        </el-form-item>
        <el-form-item label="授权范围（可选）" prop="secret_data.scope">
          <el-input v-model="form.secret_data.scope" placeholder="read write admin" />
        </el-form-item>
      </div>
      
      <!-- 证书类型 -->
      <template v-if="credentialType === '证书'">
        <el-form-item label="证书 PEM" prop="secret_data.cert_pem">
          <el-input
            v-model="form.secret_data.cert_pem"
            type="textarea"
            :rows="6"
            placeholder="-----BEGIN CERTIFICATE-----"
            font-family="monospace"
          />
        </el-form-item>
        <el-form-item label="私钥 PEM" prop="secret_data.key_pem">
          <el-input
            v-model="form.secret_data.key_pem"
            type="textarea"
            :rows="6"
            placeholder="-----BEGIN PRIVATE KEY-----"
            font-family="monospace"
          />
        </el-form-item>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="证书类型" prop="secret_data.cert_type">
              <el-select v-model="form.secret_data.cert_type" placeholder="选择证书类型" style="width: 100%">
                <el-option label="X.509" value="x509" />
                <el-option label="PKCS12" value="pkcs12" />
                <el-option label="PEM" value="pem" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="CA 证书（可选）" prop="secret_data.ca_cert">
              <el-input
                v-model="form.secret_data.ca_cert"
                type="textarea"
                :rows="3"
                placeholder="-----BEGIN CERTIFICATE-----（CA证书）"
              />
            </el-form-item>
          </el-col>
        </el-row>
      </template>
      
      <!-- Passkey 类型 -->
      <template v-if="credentialType === 'Passkey'">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            Passkey 凭据通常通过浏览器注册生成，不支持手动创建。
          </template>
        </el-alert>
      </template>
      
      <!-- MFA 类型 -->
      <template v-if="credentialType === '多因素认证'">
        <el-form-item label="MFA 类型" prop="secret_data.mfa_type">
          <el-select v-model="form.secret_data.mfa_type" placeholder="选择 MFA 类型" style="width: 100%">
            <el-option label="TOTP（时间同步）" value="totp" />
            <el-option label="SMS" value="sms" />
            <el-option label="Email" value="email" />
            <el-option label="硬件令牌" value="hardware" />
          </el-select>
        </el-form-item>
        <el-form-item label="TOTP 密钥" prop="secret_data.secret">
          <el-input
            v-model="form.secret_data.secret"
            placeholder="Base32 编码的密钥，如 JBSWY3DPEHPK3PXP"
          />
        </el-form-item>
        <el-form-item label="签发者" prop="secret_data.issuer">
          <el-input v-model="form.secret_data.issuer" placeholder="如: MyApp, GitHub" />
        </el-form-item>
        <el-form-item label="账户" prop="secret_data.account">
          <el-input v-model="form.secret_data.account" placeholder="如: user@example.com" />
        </el-form-item>
      </template>
      
      <!-- IAM 角色类型 -->
      <template v-if="credentialType === 'IAM 角色'">
        <el-form-item label="云平台" prop="secret_data.provider">
          <el-select v-model="form.secret_data.provider" placeholder="选择云平台" style="width: 100%">
            <el-option label="AWS" value="aws" />
            <el-option label="Google Cloud" value="gcp" />
            <el-option label="Azure" value="azure" />
          </el-select>
        </el-form-item>
        <el-form-item label="角色 ARN" prop="secret_data.role_arn">
          <el-input
            v-model="form.secret_data.role_arn"
            placeholder="arn:aws:iam::123456789012:role/MyRole"
          />
        </el-form-item>
        <el-form-item label="区域（可选）" prop="secret_data.region">
          <el-input v-model="form.secret_data.region" placeholder="如: us-east-1" />
        </el-form-item>
      </template>
      
      <!-- 高级选项 -->
      <div class="section-header">
        <el-icon><Setting /></el-icon>
        <span>高级选项</span>
      </div>
      
      <el-row :gutter="20">
        <el-col :span="12">
          <el-form-item label="过期时间（可选）">
            <el-date-picker
              v-model="expiresAt"
              type="datetime"
              placeholder="选择过期时间"
              style="width: 100%"
              :disabled-date="disabledDate"
              @change="handleExpiresAtChange"
            />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="自动轮换">
            <el-switch v-model="form.auto_rotate" />
            <span class="form-hint">启用后系统将定期提醒更新凭据</span>
          </el-form-item>
        </el-col>
      </el-row>
      
      <el-form-item>
        <div class="form-actions">
          <el-button @click="handleCancel">取消</el-button>
          <el-button type="primary" @click="handleSubmit" :loading="submitting">
            {{ isEdit ? '保存更改' : '创建密钥' }}
          </el-button>
        </div>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Lock, User, Key, Setting } from '@element-plus/icons-vue'
import { createCredential, updateCredential } from '@/api/credential'

const props = defineProps({
  initialData: {
    type: Object,
    default: null
  },
  types: {
    type: Array,
    default: () => []
  },
  categories: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['submit', 'cancel'])

const formRef = ref(null)
const submitting = ref(false)
const expiresAt = ref(null)

// Keep template compatibility - computed from form.type
const credentialType = computed({
  get: () => form.type,
  set: (val) => { form.type = val }
})

const isEdit = computed(() => !!props.initialData?.id)

const form = reactive({
  name: '',
  type: '',
  category: '',
  description: '',
  secret_data: {
    username: '',
    password: '',
    domain: '',
    port: 1,
    private_key: '',
    public_key: '',
    key_type: 'rsa',
    passphrase: '',
    token: '',
    token_type: 'bearer',
    scopes: [],
    client_id: '',
    client_secret: '',
    provider_url: '',
    scope: '',
    cert_pem: '',
    key_pem: '',
    cert_type: 'x509',
    ca_cert: '',
    mfa_type: '',
    secret: '',
    issuer: '',
    account: '',
    provider: '',
    role_arn: '',
    region: ''
  },
  scope: 'user',
  auto_rotate: false
})

function handleTypeChange(val) {
  console.log('handleTypeChange called with value:', val)
  // Reset secret_data using Object.assign to maintain reactivity
  Object.assign(form.secret_data, {
    username: '',
    password: '',
    domain: '',
    port: 1,
    private_key: '',
    public_key: '',
    key_type: 'rsa',
    passphrase: '',
    token: '',
    token_type: 'bearer',
    scopes: [],
    client_id: '',
    client_secret: '',
    provider_url: '',
    scope: '',
    cert_pem: '',
    key_pem: '',
    cert_type: 'x509',
    ca_cert: '',
    mfa_type: '',
    secret: '',
    issuer: '',
    account: '',
    provider: '',
    role_arn: '',
    region: ''
  })
  form.category = ''
  console.log('Type after change:', form.type)
}

const rules = {
  name: [
    { required: true, message: '请输入密钥名称', trigger: 'blur' },
    { min: 1, max: 128, message: '名称长度必须在 1-128 之间', trigger: 'blur' }
  ],
  type: [
    { required: true, message: '请选择密钥类型', trigger: 'change' }
  ],
  category: [
    { required: true, message: '请选择分类', trigger: 'change' }
  ],
  'secret_data.username': [
    { required: true, message: '请输入用户名', trigger: 'blur' }
  ],
  'secret_data.password': [
    { required: true, message: '请输入密码', trigger: 'blur' }
  ],
  'secret_data.private_key': [
    { required: true, message: '请输入私钥', trigger: 'blur' }
  ],
  'secret_data.token': [
    { required: true, message: '请输入令牌', trigger: 'blur' }
  ],
  'secret_data.client_id': [
    { required: true, message: '请输入 Client ID', trigger: 'blur' }
  ],
  'secret_data.client_secret': [
    { required: true, message: '请输入 Client Secret', trigger: 'blur' }
  ],
  'secret_data.provider_url': [
    { required: true, message: '请输入 Provider URL', trigger: 'blur' }
  ],
  'secret_data.cert_pem': [
    { required: true, message: '请输入证书', trigger: 'blur' }
  ],
  'secret_data.key_pem': [
    { required: true, message: '请输入私钥', trigger: 'blur' }
  ],
  'secret_data.mfa_type': [
    { required: true, message: '请选择 MFA 类型', trigger: 'change' }
  ],
  'secret_data.secret': [
    { required: true, message: '请输入 TOTP 密钥', trigger: 'blur' }
  ],
  'secret_data.provider': [
    { required: true, message: '请选择云平台', trigger: 'change' }
  ],
  'secret_data.role_arn': [
    { required: true, message: '请输入角色 ARN', trigger: 'blur' }
  ]
}

// 初始化表单数据
watch(() => props.initialData, (val) => {
  if (val) {
    form.name = val.name
    form.type = val.type
    form.category = val.category
    form.description = val.description
    form.scope = val.scope
    form.auto_rotate = val.auto_rotate
    
    // Reset secret_data with initial values while maintaining reactivity
    Object.assign(form.secret_data, {
      username: '',
      password: '',
      domain: '',
      port: 1,
      private_key: '',
      public_key: '',
      key_type: 'rsa',
      passphrase: '',
      token: '',
      token_type: 'bearer',
      scopes: [],
      client_id: '',
      client_secret: '',
      provider_url: '',
      scope: '',
      cert_pem: '',
      key_pem: '',
      cert_type: 'x509',
      ca_cert: '',
      mfa_type: '',
      secret: '',
      issuer: '',
      account: '',
      provider: '',
      role_arn: '',
      region: ''
    })
    
    if (val.expires_at) {
      expiresAt.value = new Date(val.expires_at * 1000)
    }
  }
}, { immediate: true })

function resetForm() {
  form.name = ''
  form.type = ''
  form.category = ''
  form.description = ''
  Object.assign(form.secret_data, {
    username: '',
    password: '',
    domain: '',
    port: 1,
    private_key: '',
    public_key: '',
    key_type: 'rsa',
    passphrase: '',
    token: '',
    token_type: 'bearer',
    scopes: [],
    client_id: '',
    client_secret: '',
    provider_url: '',
    scope: '',
    cert_pem: '',
    key_pem: '',
    cert_type: 'x509',
    ca_cert: '',
    mfa_type: '',
    secret: '',
    issuer: '',
    account: '',
    provider: '',
    role_arn: '',
    region: ''
  })
  form.scope = 'user'
  form.auto_rotate = false
  expiresAt.value = null
}

function handleExpiresAtChange(val) {
  if (val) {
    form.expires_at = Math.floor(val.getTime() / 1000)
  } else {
    form.expires_at = null
  }
}

function disabledDate(time) {
  return time.getTime() < Date.now() - 8.64e7
}

function handleCancel() {
  emit('cancel')
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  
  submitting.value = true
  try {
    const data = {
      name: form.name,
      type: form.type,
      category: form.category,
      description: form.description,
      secret_data: form.secret_data,
      scope: form.scope,
      auto_rotate: form.auto_rotate,
      expires_at: form.expires_at
    }
    
    let res
    if (props.initialData) {
      res = await updateCredential(props.initialData.id, data)
    } else {
      res = await createCredential(data)
    }
    
    if (res.code === 200) {
      ElMessage.success(props.initialData ? '更新成功' : '创建成功')
      emit('submit', res.data)
      resetForm()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (error) {
    ElMessage.error('操作失败: ' + error.message)
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.secret-form {
  padding: 0 20px;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 24px 0 16px 0;
  padding-bottom: 8px;
  border-bottom: 1px solid #e4e7ed;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 500;
}

.section-header .el-icon {
  color: var(--text-muted);
}

.section-divider {
  margin: 24px 0 16px 0;
}

.section-divider :deep(.el-divider__text) {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 500;
  background-color: transparent;
}

.form-hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--text-muted);
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  width: 100%;
  margin-top: 24px;
}

:deep(.el-divider__text) {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 14px;
}

:deep(.el-textarea__inner) {
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
}
</style>
