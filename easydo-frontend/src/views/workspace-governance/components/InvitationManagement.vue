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
      <el-table-column label="操作" width="240">
        <template #default="{ row }">
          <el-button v-if="row.status === 'pending'" link size="small" @click="copyInvitationLink(row)">复制链接</el-button>
          <el-button v-if="canManageMembers && row.status === 'pending'" link size="small" @click="handleRegenerateInvitation(row)">重新生成</el-button>
          <el-button v-if="canManageMembers && row.status === 'pending'" type="danger" link size="small" @click="handleRevokeInvitation(row)">撤销</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="inviteDialogVisible" title="邀请成员" width="460px">
      <el-form :model="inviteForm" label-width="80px">
        <el-form-item label="邮箱">
          <el-input v-model="inviteForm.emails" type="textarea" :rows="6" placeholder="每行一个邮箱" />
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
      <div class="result-list">
        <div v-for="item in inviteResultLinks" :key="item.email || item.link" class="result-block">
          <span class="result-email">{{ item.email || '-' }}</span>
          <el-input :model-value="item.link" readonly />
          <el-button type="primary" @click="copyInviteLink(item.link)">复制链接</el-button>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { createWorkspaceInvitation, getWorkspaceInvitations, regenerateWorkspaceInvitation, revokeWorkspaceInvitation } from '@/api/workspace'

const userStore = useUserStore()
const canManageMembers = computed(() => userStore.canAccessWorkspaceGovernance)
const invitations = ref([])
const inviteDialogVisible = ref(false)
const inviteLoading = ref(false)
const inviteResultVisible = ref(false)
const inviteResultLink = ref('')
const inviteResultLinks = ref([])
const inviteForm = reactive({
  emails: '',
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

const splitInviteEmails = () => {
  const seen = new Set()
  return String(inviteForm.emails || '')
    .split(/[\n,;，；]+/)
    .map(item => item.trim().toLowerCase())
    .filter((item) => {
      if (!item || seen.has(item)) return false
      seen.add(item)
      return true
    })
}

const buildInvitationLink = (item = {}) => {
  const locator = item.token || item.id
  return locator ? `${window.location.origin}/workspace-invitations/${locator}` : ''
}

const showInvitationResults = (items = []) => {
  inviteResultLinks.value = items
    .map(item => ({
      email: item.email,
      link: buildInvitationLink(item)
    }))
    .filter(item => item.link)
  inviteResultLink.value = inviteResultLinks.value[0]?.link || ''
  inviteResultVisible.value = inviteResultLinks.value.length > 0
}

const handleInviteSubmit = async () => {
  if (!userStore.currentWorkspaceId) {
    return
  }
  const emails = splitInviteEmails()
  if (emails.length === 0) {
    ElMessage.warning('请输入邮箱')
    return
  }
  inviteLoading.value = true
  try {
    const res = await createWorkspaceInvitation(userStore.currentWorkspaceId, {
      emails,
      role: inviteForm.role
    })
    if (res.code === 200) {
      const createdInvitations = Array.isArray(res.data?.list) ? res.data.list : [res.data]
      showInvitationResults(createdInvitations)
      inviteDialogVisible.value = false
      inviteForm.emails = ''
      inviteForm.role = 'viewer'
      ElMessage.success(`已生成 ${createdInvitations.length} 个邀请`)
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

const copyInviteLink = async (link = inviteResultLink.value) => {
  try {
    await navigator.clipboard.writeText(link)
    ElMessage.success('邀请链接已复制')
  } catch (error) {
    ElMessage.error('复制失败，请手动复制链接')
  }
}

const copyInvitationLink = async (row) => {
  await copyInviteLink(buildInvitationLink(row))
}

const handleRegenerateInvitation = async (row) => {
  if (!userStore.currentWorkspaceId) {
    return
  }
  try {
    await ElMessageBox.confirm(`确认重新生成发往 ${row.email} 的邀请链接吗？`, '重新生成邀请', { type: 'warning' })
    const res = await regenerateWorkspaceInvitation(userStore.currentWorkspaceId, row.id)
    if (res.code === 200) {
      showInvitationResults([res.data])
      ElMessage.success('邀请链接已重新生成')
      await loadInvitations()
      return
    }
    ElMessage.error(res.message || '重新生成失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '重新生成失败')
    }
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

.result-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.result-email {
  width: 180px;
  color: var(--text-secondary);
  font-size: 13px;
  overflow-wrap: anywhere;
}

@media (max-width: 760px) {
  .result-block {
    align-items: stretch;
    flex-direction: column;
  }

  .result-email {
    width: auto;
  }
}
</style>
