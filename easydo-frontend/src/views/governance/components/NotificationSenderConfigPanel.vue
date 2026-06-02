<template>
  <div class="governance-card sender-config-panel">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">{{ title }}</h2>
        <div class="section-subtitle">
          <el-tag size="small" :type="effectiveScopeType">{{ effectiveScopeText }}</el-tag>
          <span v-if="form.smtp_password_configured" class="inline-status">密码已保存</span>
        </div>
      </div>
      <div class="header-actions">
        <el-button :loading="loading" @click="loadConfig">刷新</el-button>
        <el-button type="primary" :loading="saving" @click="saveConfig">保存</el-button>
      </div>
    </div>

    <el-form :model="form" label-width="120px" class="sender-form">
      <div class="form-grid">
        <el-form-item label="启用发送">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="TLS 模式">
          <el-select v-model="form.smtp_tls_mode" style="width: 100%">
            <el-option label="Plain" value="plain" />
            <el-option label="STARTTLS" value="starttls" />
            <el-option label="TLS" value="tls" />
          </el-select>
        </el-form-item>
        <el-form-item label="发送名称">
          <el-input v-model="form.from_name" placeholder="EasyDo" />
        </el-form-item>
        <el-form-item label="发送邮箱">
          <el-input v-model="form.from_address" placeholder="noreply@example.com" />
        </el-form-item>
        <el-form-item label="SMTP Host">
          <el-input v-model="form.smtp_host" placeholder="smtp.example.com" />
        </el-form-item>
        <el-form-item label="SMTP Port">
          <el-input-number v-model="form.smtp_port" :min="1" :max="65535" style="width: 100%" />
        </el-form-item>
        <el-form-item label="SMTP 用户">
          <el-input v-model="form.smtp_username" />
        </el-form-item>
        <el-form-item label="SMTP 密码">
          <el-input v-model="form.smtp_password" type="password" show-password placeholder="留空则保留已保存密码" />
        </el-form-item>
      </div>
    </el-form>

    <div class="test-row">
      <el-input v-model="testAddress" placeholder="test@example.com" />
      <el-button :loading="testing" @click="testConfig">发送测试</el-button>
      <span v-if="lastTestText" class="test-result">{{ lastTestText }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  getEffectiveNotificationSender,
  savePlatformNotificationSender,
  saveWorkspaceNotificationSender,
  testNotificationSender
} from '@/api/notificationSender'

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
const saving = ref(false)
const testing = ref(false)
const testAddress = ref('')
const lastTestText = ref('')
const form = reactive({
  enabled: false,
  scope: '',
  workspace_id: 0,
  from_name: '',
  from_address: '',
  smtp_host: '',
  smtp_port: 25,
  smtp_username: '',
  smtp_password: '',
  smtp_password_configured: false,
  smtp_tls_mode: 'plain'
})

const title = computed(() => props.scope === 'platform' ? '平台邮件配置' : '工作区邮件配置')
const effectiveScopeText = computed(() => {
  if (!form.enabled) return '未启用'
  return form.scope === 'workspace' ? '工作区配置' : '平台默认'
})
const effectiveScopeType = computed(() => form.scope === 'workspace' ? 'success' : (form.enabled ? 'info' : 'warning'))

const applyConfig = (config = {}) => {
  form.enabled = Boolean(config.enabled)
  form.scope = config.scope || ''
  form.workspace_id = Number(config.workspace_id || 0)
  form.from_name = config.from_name || ''
  form.from_address = config.from_address || ''
  form.smtp_host = config.smtp_host || ''
  form.smtp_port = Number(config.smtp_port || 25)
  form.smtp_username = config.smtp_username || ''
  form.smtp_password = ''
  form.smtp_password_configured = Boolean(config.smtp_password_configured)
  form.smtp_tls_mode = config.smtp_tls_mode || 'plain'
  if (config.last_test_status) {
    lastTestText.value = config.last_test_status === 'success' ? '最近测试成功' : (config.last_test_error || '最近测试失败')
  } else {
    lastTestText.value = ''
  }
}

const buildPayload = () => ({
  enabled: form.enabled,
  from_name: form.from_name,
  from_address: form.from_address,
  smtp_host: form.smtp_host,
  smtp_port: form.smtp_port,
  smtp_username: form.smtp_username,
  smtp_password: form.smtp_password,
  smtp_tls_mode: form.smtp_tls_mode
})

const loadConfig = async () => {
  if (props.scope === 'workspace' && !props.workspaceId) return
  loading.value = true
  try {
    const res = await getEffectiveNotificationSender()
    applyConfig(res?.data || {})
  } catch (error) {
    ElMessage.error('加载邮件配置失败')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (props.scope === 'workspace' && !props.workspaceId) {
    ElMessage.warning('请先选择工作区')
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    const res = props.scope === 'platform'
      ? await savePlatformNotificationSender(payload)
      : await saveWorkspaceNotificationSender(props.workspaceId, payload)
    if (res.code === 200) {
      applyConfig(res.data || {})
      ElMessage.success('邮件配置已保存')
      return
    }
    ElMessage.error(res.message || '保存邮件配置失败')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '保存邮件配置失败')
  } finally {
    saving.value = false
  }
}

const testConfig = async () => {
  if (!testAddress.value) {
    ElMessage.warning('请输入测试收件人')
    return
  }
  testing.value = true
  try {
    const res = await testNotificationSender({ to_address: testAddress.value })
    const result = res?.data || {}
    lastTestText.value = result.status === 'success' ? '测试邮件已发送' : (result.error || '测试发送失败')
    if (result.status === 'success') {
      ElMessage.success('测试邮件已发送')
    } else {
      ElMessage.warning(lastTestText.value)
    }
    await loadConfig()
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '测试发送失败')
  } finally {
    testing.value = false
  }
}

watch(() => props.workspaceId, loadConfig)

onMounted(loadConfig)
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.sender-config-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header-row {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}

.section-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.section-subtitle {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 13px;
}

.inline-status,
.test-result {
  color: var(--text-secondary);
  font-size: 13px;
}

.header-actions,
.test-row {
  display: flex;
  gap: 10px;
  align-items: center;
}

.sender-form {
  padding-top: 4px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(260px, 1fr));
  gap: 0 20px;
}

.test-row {
  max-width: 720px;
}

@media (max-width: 900px) {
  .section-header-row,
  .test-row {
    flex-direction: column;
    align-items: stretch;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
