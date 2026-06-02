<template>
  <div class="force-password-page">
    <section class="password-panel">
      <div class="panel-header">
        <h1>修改初始密码</h1>
        <p>当前账号需要先完成密码修改，才能继续访问系统功能。</p>
      </div>

      <el-form :model="form" label-width="100px" class="password-form">
        <el-form-item label="当前密码">
          <el-input v-model="form.current_password" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="form.new_password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item label="确认密码">
          <el-input v-model="form.confirm_password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
      </el-form>

      <ul class="password-rules">
        <li v-for="rule in passwordRules" :key="rule.text" :class="{ passed: rule.passed }">{{ rule.text }}</li>
      </ul>

      <div class="panel-actions">
        <el-button :loading="loggingOut" @click="handleLogout">退出登录</el-button>
        <el-button type="primary" :loading="saving" @click="submitPassword">保存并继续</el-button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { updatePassword } from '@/api/user'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const saving = ref(false)
const loggingOut = ref(false)
const form = reactive({
  current_password: '',
  new_password: '',
  confirm_password: ''
})

const passwordRules = computed(() => [
  { text: '新密码至少 6 位', passed: form.new_password.length >= 6 },
  { text: '两次输入的新密码一致', passed: Boolean(form.new_password) && form.new_password === form.confirm_password },
  { text: '新密码不能与当前密码相同', passed: Boolean(form.new_password) && form.new_password !== form.current_password }
])

const redirectPath = computed(() => {
  const raw = Array.isArray(route.query.redirect) ? route.query.redirect[0] : route.query.redirect
  const normalized = String(raw || '/').trim()
  return normalized && normalized !== '/force-password-change' ? normalized : '/'
})

const submitPassword = async () => {
  if (!form.current_password || !form.new_password || !form.confirm_password) {
    ElMessage.warning('请完整填写密码')
    return
  }
  if (passwordRules.value.some(rule => !rule.passed)) {
    ElMessage.warning('请确认新密码满足规则')
    return
  }
  saving.value = true
  try {
    const res = await updatePassword({
      current_password: form.current_password,
      new_password: form.new_password
    })
    if (res.code === 200) {
      await userStore.getUserInfoAction()
      ElMessage.success('密码已修改')
      router.replace(redirectPath.value)
      return
    }
    ElMessage.error(res.message || '修改密码失败')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '修改密码失败')
  } finally {
    saving.value = false
  }
}

const handleLogout = async () => {
  loggingOut.value = true
  try {
    await userStore.doLogout()
    router.replace('/login')
  } finally {
    loggingOut.value = false
  }
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.force-password-page {
  min-height: calc(100vh - 120px);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 48px 16px;
}

.password-panel {
  width: min(560px, 100%);
  padding: 28px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: $radius-lg;
  box-shadow: var(--shadow-sm);
}

.panel-header {
  margin-bottom: 24px;
}

.panel-header h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
  color: var(--text-primary);
}

.panel-header p {
  margin: 8px 0 0;
  color: var(--text-secondary);
  font-size: 14px;
}

.password-form {
  max-width: 480px;
}

.password-rules {
  margin: 4px 0 22px 100px;
  padding-left: 18px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.8;
}

.password-rules li.passed {
  color: var(--success-color);
}

.panel-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@media (max-width: 640px) {
  .password-panel {
    padding: 20px;
  }

  .password-rules {
    margin-left: 0;
  }
}
</style>
