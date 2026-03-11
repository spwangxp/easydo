<template>
  <div class="invitation-page">
    <el-card class="invitation-card">
      <h1>工作空间邀请</h1>
      <p>{{ description }}</p>
      <div class="actions">
        <el-button v-if="needsLogin" type="primary" @click="goToLogin">登录后接受邀请</el-button>
        <el-button v-else type="primary" :loading="loading" @click="acceptInvitation">接受邀请</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { acceptWorkspaceInvitation } from '@/api/workspace'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)

const token = computed(() => route.params.token)
const needsLogin = computed(() => !userStore.isLoggedIn)
const description = computed(() => (
  needsLogin.value
    ? '请先登录，再接受当前工作空间邀请。'
    : '确认接受邀请并加入对应工作空间。'
))

const goToLogin = () => {
  router.push({ name: 'Login', query: { redirect: route.fullPath } })
}

const acceptInvitation = async () => {
  loading.value = true
  try {
    const res = await acceptWorkspaceInvitation(token.value)
    if (res.code === 200) {
      await userStore.getUserInfoAction()
      if (res.data?.workspace_id) {
        userStore.setCurrentWorkspaceById(res.data.workspace_id)
      }
      ElMessage.success('已加入工作空间')
      router.push('/settings')
    } else {
      ElMessage.error(res.message || '接受邀请失败')
    }
  } catch (error) {
    ElMessage.error('接受邀请失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.invitation-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f5f7fa;
}

.invitation-card {
  width: 460px;
}

.actions {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
