<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">邀请管理</h2>
        <div class="section-subtitle">为当前工作区生成邀请链接，并支持撤销待处理邀请。</div>
      </div>
      <el-button v-if="canManageMembers" type="primary" @click="inviteDialogVisible = true">邀请成员</el-button>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

    <el-table v-else :data="invitations" style="width: 100%">
      <el-table-column prop="email" label="邮箱" min-width="220" />
      <el-table-column prop="role" label="角色" width="140">
        <template #default="{ row }">{{ roleText(row.role) }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column prop="expires_at" label="过期时间" width="180">
        <template #default="{ row }">{{ formatDateTime(row.expires_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="120">
        <template #default="{ row }">
          <el-button v-if="canManageMembers && row.status === 'pending'" type="danger" link size="small" @click="handleRevokeInvitation(row)">撤销</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="inviteDialogVisible" title="邀请成员" width="460px">
      <el-form :model="inviteForm" label-width="80px">
        <el-form-item label="邮箱">
          <el-input v-model="inviteForm.email" placeholder="member@example.com" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="inviteForm.role" style="width: 100%">
            <el-option label="Viewer" value="viewer" />
            <el-option label="Developer" value="developer" />
            <el-option label="Maintainer" value="maintainer" />
            <el-option label="Owner" value="owner" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="inviteDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="inviteLoading" @click="handleInviteSubmit">生成邀请</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="inviteResultVisible" title="邀请链接已生成" width="560px">
      <div class="result-block">
        <el-input :model-value="inviteResultLink" readonly />
        <el-button type="primary" @click="copyInviteLink">复制链接</el-button>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { createWorkspaceInvitation, getWorkspaceInvitations, revokeWorkspaceInvitation } from '@/api/workspace'

const userStore = useUserStore()
const canManageMembers = computed(() => userStore.canAccessWorkspaceGovernance)
const invitations = ref([])
const inviteDialogVisible = ref(false)
const inviteLoading = ref(false)
const inviteResultVisible = ref(false)
const inviteResultLink = ref('')
const inviteForm = reactive({
  email: '',
  role: 'viewer'
})

const roleText = (role) => {
  const map = {
    viewer: 'Viewer',
    developer: 'Developer',
    maintainer: 'Maintainer',
    owner: 'Owner'
  }
  return map[role] || role || '-'
}

const formatDateTime = (value) => {
  if (!value) return '-'
  const parsed = typeof value === 'number'
    ? new Date(value < 1e12 ? value * 1000 : value)
    : new Date(value)
  return Number.isNaN(parsed.getTime()) ? '-' : parsed.toLocaleString('zh-CN')
}

const loadInvitations = async () => {
  if (!userStore.currentWorkspaceId) {
    invitations.value = []
    return
  }
  try {
    const res = await getWorkspaceInvitations(userStore.currentWorkspaceId)
    invitations.value = res?.data?.list || []
  } catch (error) {
    ElMessage.error('加载工作区邀请失败')
  }
}

const handleInviteSubmit = async () => {
  if (!userStore.currentWorkspaceId) {
    return
  }
  if (!inviteForm.email) {
    ElMessage.warning('请输入邮箱')
    return
  }
  inviteLoading.value = true
  try {
    const res = await createWorkspaceInvitation(userStore.currentWorkspaceId, inviteForm)
    if (res.code === 200) {
      inviteResultLink.value = `${window.location.origin}/workspace-invitations/${res.data.id}`
      inviteResultVisible.value = true
      inviteDialogVisible.value = false
      inviteForm.email = ''
      inviteForm.role = 'viewer'
      ElMessage.success('邀请已生成')
      await loadInvitations()
      return
    }
    ElMessage.error(res.message || '邀请失败')
  } catch (error) {
    ElMessage.error('邀请失败')
  } finally {
    inviteLoading.value = false
  }
}

const copyInviteLink = async () => {
  try {
    await navigator.clipboard.writeText(inviteResultLink.value)
    ElMessage.success('邀请链接已复制')
  } catch (error) {
    ElMessage.error('复制失败，请手动复制链接')
  }
}

const handleRevokeInvitation = async (row) => {
  if (!userStore.currentWorkspaceId) {
    return
  }
  try {
    await ElMessageBox.confirm(`确认撤销发往 ${row.email} 的邀请吗？`, '撤销邀请', { type: 'warning' })
    const res = await revokeWorkspaceInvitation(userStore.currentWorkspaceId, row.id)
    if (res.code === 200) {
      ElMessage.success('邀请已撤销')
      await loadInvitations()
      return
    }
    ElMessage.error(res.message || '撤销失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('撤销失败')
    }
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadInvitations()
}, { immediate: true })
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.governance-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.section-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.section-subtitle {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 14px;
}

.empty-hint {
  padding: 24px;
  background: var(--bg-card);
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-align: center;
}

.result-block {
  display: flex;
  gap: 12px;
  align-items: center;
}
</style>
