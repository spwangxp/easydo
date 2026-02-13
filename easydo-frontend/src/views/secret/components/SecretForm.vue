<template>
  <el-form
    ref="formRef"
    :model="form"
    :rules="rules"
    label-width="100px"
    label-position="top"
  >
    <el-form-item label="密钥名称" prop="name">
      <el-input v-model="form.name" placeholder="请输入密钥名称" maxlength="128" show-word-limit />
    </el-form-item>

    <el-form-item label="密钥类型" prop="type">
      <el-select v-model="form.type" placeholder="选择密钥类型" style="width: 100%" :disabled="isEdit">
        <el-option label="SSH密钥" value="ssh" />
        <el-option label="访问令牌" value="token" />
        <el-option label="镜像仓库凭证" value="registry" />
        <el-option label="API密钥" value="api_key" />
        <el-option label="Kubernetes凭证" value="kubernetes" />
      </el-select>
    </el-form-item>

    <el-form-item label="分类" prop="category">
      <el-select v-model="form.category" placeholder="选择分类" style="width: 100%">
        <el-option label="GitHub" value="github" />
        <el-option label="GitLab" value="gitlab" />
        <el-option label="Gitee" value="gitee" />
        <el-option label="Docker" value="docker" />
        <el-option label="钉钉" value="dingtalk" />
        <el-option label="企业微信" value="wechat" />
        <el-option label="Kubernetes" value="kubernetes" />
        <el-option label="自定义" value="custom" />
      </el-select>
    </el-form-item>

    <!-- SSH密钥特殊处理 -->
    <template v-if="form.type === 'ssh'">
      <el-form-item label="生成方式">
        <el-radio-group v-model="sshMode">
          <el-radio label="generate">自动生成</el-radio>
          <el-radio label="import">导入已有</el-radio>
        </el-radio-group>
      </el-form-item>

      <template v-if="sshMode === 'generate'">
        <el-form-item label="密钥位数">
          <el-radio-group v-model="sshBits">
            <el-radio :label="2048">2048位</el-radio>
            <el-radio :label="4096">4096位</el-radio>
          </el-radio-group>
        </el-form-item>
        
        <el-form-item>
          <el-button type="primary" @click="generateSSHKey" :loading="generating">
            生成SSH密钥对
          </el-button>
        </el-form-item>

        <el-form-item label="私钥" prop="value" v-if="form.value">
          <el-input
            v-model="form.value"
            type="textarea"
            :rows="6"
            :readonly="sshMode === 'generate'"
            placeholder="私钥内容（自动生成或导入）"
          />
        </el-form-item>

        <el-form-item label="公钥" v-if="sshPublicKey">
          <el-input
            v-model="sshPublicKey"
            type="textarea"
            :rows="3"
            readonly
            placeholder="公钥内容"
          />
          <el-button type="primary" link @click="copyPublicKey" style="margin-top: 8px;">
            复制公钥
          </el-button>
        </el-form-item>
      </template>

      <template v-else>
        <el-form-item label="私钥内容" prop="value">
          <el-input
            v-model="form.value"
            type="textarea"
            :rows="8"
            placeholder="粘贴私钥内容 (-----BEGIN OPENSSH PRIVATE KEY----- ...)"
          />
        </el-form-item>
      </template>
    </template>

    <!-- 其他类型密钥值输入 -->
    <el-form-item v-else label="密钥值" prop="value">
      <el-input
        v-model="form.value"
        type="textarea"
        :rows="4"
        placeholder="请输入密钥值"
        show-password
      />
    </el-form-item>

    <el-form-item label="使用范围" prop="scope">
      <el-radio-group v-model="form.scope">
        <el-radio label="all">所有项目</el-radio>
        <el-radio label="project">指定项目</el-radio>
      </el-radio-group>
    </el-form-item>

    <el-form-item label="描述">
      <el-input
        v-model="form.description"
        type="textarea"
        :rows="2"
        placeholder="可选：输入密钥描述"
        maxlength="500"
        show-word-limit
      />
    </el-form-item>

    <el-form-item>
      <div style="display: flex; justify-content: flex-end; gap: 12px;">
        <el-button @click="handleCancel">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ isEdit ? '更新' : '创建' }}
        </el-button>
      </div>
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref, reactive, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { createSecret, updateSecret, generateSSHKey as apiGenerateSSHKey } from '@/api/secret'

const props = defineProps({
  type: {
    type: String,
    default: 'create'
  },
  data: {
    type: Object,
    default: null
  }
})

const emit = defineEmits(['submit', 'cancel'])

const isEdit = computed(() => props.type === 'edit')

const formRef = ref(null)
const submitting = ref(false)
const generating = ref(false)
const sshMode = ref('generate')
const sshBits = ref(2048)
const sshPublicKey = ref('')

const form = reactive({
  name: '',
  type: 'ssh',
  category: 'custom',
  value: '',
  scope: 'all',
  project_id: 0,
  description: ''
})

// 编辑模式初始化数据
if (isEdit.value && props.data) {
  form.name = props.data.name
  form.type = props.data.type
  form.category = props.data.category
  form.scope = props.data.scope
  form.project_id = props.data.project_id
  form.description = props.data.description
}

const rules = {
  name: [
    { required: true, message: '请输入密钥名称', trigger: 'blur' },
    { min: 2, max: 128, message: '长度在 2 到 128 个字符', trigger: 'blur' }
  ],
  type: [
    { required: true, message: '请选择密钥类型', trigger: 'change' }
  ],
  category: [
    { required: true, message: '请选择分类', trigger: 'change' }
  ],
  value: [
    { required: true, message: '请输入密钥值', trigger: 'blur' }
  ],
  scope: [
    { required: true, message: '请选择使用范围', trigger: 'change' }
  ]
}

const generateSSHKey = async () => {
  generating.value = true
  try {
    const res = await apiGenerateSSHKey({
      bits: sshBits.value,
      comment: form.name || 'easydo-generated'
    })
    if (res.code === 200) {
      form.value = res.data.private_key
      sshPublicKey.value = res.data.public_key
      ElMessage.success('SSH密钥生成成功')
    }
  } catch (error) {
    ElMessage.error('生成SSH密钥失败')
  } finally {
    generating.value = false
  }
}

const copyPublicKey = () => {
  navigator.clipboard.writeText(sshPublicKey.value)
  ElMessage.success('公钥已复制到剪贴板')
}

const handleSubmit = async () => {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    let res
    if (isEdit.value) {
      res = await updateSecret(props.data.id, {
        name: form.name,
        description: form.description,
        value: form.value,
        scope: form.scope,
        project_id: form.project_id,
        status: props.data.status
      })
    } else {
      res = await createSecret({
        name: form.name,
        type: form.type,
        category: form.category,
        value: form.value,
        scope: form.scope,
        project_id: form.project_id,
        description: form.description,
        metadata: sshPublicKey.value ? { public_key: sshPublicKey.value } : {}
      })
    }

    if (res.code === 200) {
      emit('submit', form)
    }
  } catch (error) {
    ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
  } finally {
    submitting.value = false
  }
}

const handleCancel = () => {
  emit('cancel')
}
</script>
