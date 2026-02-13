<template>
  <el-form
    ref="formRef"
    :model="form"
    :rules="rules"
    label-position="top"
  >
    <el-form-item label="凭据名称" prop="name">
      <el-input v-model="form.name" placeholder="请输入凭据名称" />
    </el-form-item>

    <el-form-item label="凭据类型" prop="type">
      <el-select v-model="form.type" placeholder="选择凭据类型" style="width: 100%" @change="handleTypeChange">
        <el-option
          v-for="type in types"
          :key="type.value"
          :label="type.label"
          :value="type.value"
        >
          <div style="display: flex; align-items: center; gap: 8px;">
            <el-icon><component :is="type.icon" /></el-icon>
            <span>{{ type.label }}</span>
            <span style="color: #909399; font-size: 12px;">({{ type.description }})</span>
          </div>
        </el-option>
      </el-select>
    </el-form-item>

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

    <el-form-item label="描述" prop="description">
      <el-input
        v-model="form.description"
        type="textarea"
        :rows="2"
        placeholder="可选：输入凭据描述"
      />
    </el-form-item>

    <!-- 密码类型 -->
    <template v-if="form.type === 'PASSWORD'">
      <el-form-item label="用户名" prop="secret_data.username">
        <el-input v-model="form.secret_data.username" placeholder="用户名" />
      </el-form-item>
      <el-form-item label="密码" prop="secret_data.password">
        <el-input
          v-model="form.secret_data.password"
          type="password"
          placeholder="密码"
          show-password
        />
      </el-form-item>
      <el-form-item label="域（可选）" prop="secret_data.domain">
        <el-input v-model="form.secret_data.domain" placeholder="Windows/LDAP 域" />
      </el-form-item>
    </template>

    <!-- SSH Key 类型 -->
    <template v-if="form.type === 'SSH_KEY'">
      <el-form-item label="私钥" prop="secret_data.private_key">
        <el-input
          v-model="form.secret_data.private_key"
          type="textarea"
          :rows="6"
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
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
      <el-form-item label="密钥密码（可选）" prop="secret_data.passphrase">
        <el-input
          v-model="form.secret_data.passphrase"
          type="password"
          placeholder="如果私钥有密码保护，请输入"
          show-password
        />
      </el-form-item>
      <el-form-item label="密钥类型" prop="secret_data.key_type">
        <el-select v-model="form.secret_data.key_type" placeholder="选择密钥类型">
          <el-option label="RSA" value="rsa" />
          <el-option label="Ed25519" value="ed25519" />
          <el-option label="ECDSA" value="ecdsa" />
        </el-select>
      </el-form-item>
    </template>

    <!-- Token 类型 -->
    <template v-if="form.type === 'TOKEN'">
      <el-form-item label="令牌值" prop="secret_data.token">
        <el-input
          v-model="form.secret_data.token"
          type="password"
          placeholder="ghp_xxxxxxxxxxxx"
          show-password
        />
      </el-form-item>
      <el-form-item label="令牌类型" prop="secret_data.token_type">
        <el-select v-model="form.secret_data.token_type" placeholder="选择令牌类型">
          <el-option label="Bearer" value="bearer" />
          <el-option label="Basic" value="basic" />
        </el-select>
      </el-form-item>
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
        </el-select>
      </el-form-item>
    </template>

    <!-- OAuth2 类型 -->
    <template v-if="form.type === 'OAUTH2'">
      <el-form-item label="Client ID" prop="secret_data.client_id">
        <el-input v-model="form.secret_data.client_id" placeholder="Client ID" />
      </el-form-item>
      <el-form-item label="Client Secret" prop="secret_data.client_secret">
        <el-input
          v-model="form.secret_data.client_secret"
          type="password"
          placeholder="Client Secret"
          show-password
        />
      </el-form-item>
      <el-form-item label="Provider URL" prop="secret_data.provider_url">
        <el-input v-model="form.secret_data.provider_url" placeholder="https://oauth.provider.com" />
      </el-form-item>
      <el-form-item label="授权范围（可选）" prop="secret_data.scope">
        <el-input v-model="form.secret_data.scope" placeholder="read write" />
      </el-form-item>
    </template>

    <!-- 证书类型 -->
    <template v-if="form.type === 'CERTIFICATE'">
      <el-form-item label="证书 PEM" prop="secret_data.cert_pem">
        <el-input
          v-model="form.secret_data.cert_pem"
          type="textarea"
          :rows="6"
          placeholder="-----BEGIN CERTIFICATE-----"
        />
      </el-form-item>
      <el-form-item label="私钥 PEM（可选）" prop="secret_data.key_pem">
        <el-input
          v-model="form.secret_data.key_pem"
          type="textarea"
          :rows="6"
          placeholder="-----BEGIN PRIVATE KEY-----"
        />
      </el-form-item>
      <el-form-item label="CA 证书（可选）" prop="secret_data.ca_cert">
        <el-input
          v-model="form.secret_data.ca_cert"
          type="textarea"
          :rows="4"
          placeholder="-----BEGIN CERTIFICATE-----（CA证书）"
        />
      </el-form-item>
    </template>

    <el-form-item label="使用范围" prop="scope">
      <el-radio-group v-model="form.scope">
        <el-radio label="user">个人</el-radio>
        <el-radio label="project">项目</el-radio>
      </el-radio-group>
    </el-form-item>

    <el-form-item>
      <div style="display: flex; justify-content: flex-end; gap: 10px;">
        <el-button @click="handleCancel">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">保存</el-button>
      </div>
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
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

const form = reactive({
  name: '',
  type: '',
  category: '',
  description: '',
  secret_data: {},
  scope: 'user'
})

const rules = {
  name: [{ required: true, message: '请输入凭据名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择凭据类型', trigger: 'change' }],
  category: [{ required: true, message: '请选择分类', trigger: 'change' }],
  'secret_data.username': [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  'secret_data.password': [{ required: true, message: '请输入密码', trigger: 'blur' }],
  'secret_data.private_key': [{ required: true, message: '请输入私钥', trigger: 'blur' }],
  'secret_data.token': [{ required: true, message: '请输入令牌', trigger: 'blur' }],
  'secret_data.client_id': [{ required: true, message: '请输入 Client ID', trigger: 'blur' }],
  'secret_data.client_secret': [{ required: true, message: '请输入 Client Secret', trigger: 'blur' }],
  'secret_data.provider_url': [{ required: true, message: '请输入 Provider URL', trigger: 'blur' }],
  'secret_data.cert_pem': [{ required: true, message: '请输入证书', trigger: 'blur' }]
}

watch(() => props.initialData, (val) => {
  if (val) {
    form.name = val.name
    form.type = val.type
    form.category = val.category
    form.description = val.description
    form.scope = val.scope
    form.secret_data = {}
  } else {
    resetForm()
  }
}, { immediate: true })

function resetForm() {
  form.name = ''
  form.type = ''
  form.category = ''
  form.description = ''
  form.secret_data = {}
  form.scope = 'user'
}

function handleTypeChange() {
  form.secret_data = {}
  form.category = ''
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
      scope: form.scope
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
