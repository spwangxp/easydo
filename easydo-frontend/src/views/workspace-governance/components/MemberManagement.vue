<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">成员管理</h2>
        <div class="section-subtitle">管理当前工作区成员角色，并保留危险操作二次确认。</div>
      </div>
      <el-button v-if="canManageMembers" type="primary" @click="openCreateDialog">创建用户</el-button>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

    <el-table v-else :data="members" style="width: 100%">
      <el-table-column prop="username" label="用户名" width="160" />
      <el-table-column prop="email" label="邮箱" min-width="220" />
      <el-table-column prop="role" label="角色" width="180">
        <template #default="{ row }">
          <el-select
            v-if="canManageMembers"
            :model-value="row.role"
            size="small"
            @change="(value) => handleRoleChange(row, value)"
          >
            <el-option label="Viewer" value="viewer" />
            <el-option label="Developer" value="developer" />
            <el-option label="Maintainer" value="maintainer" />
            <el-option label="Owner" value="owner" />
          </el-select>
          <el-tag v-else size="small">{{ roleText(row.role) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
            {{ row.status === 'active' ? '已启用' : '已禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="加入时间" width="180">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="120">
        <template #default="{ row }">
          <el-button v-if="canManageMembers" type="danger" link size="small" @click="handleRemoveMember(row)">移除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" title="创建工作区用户" width="520px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="用户名">
          <el-input v-model="form.username" />
        </el-form-item>
        <el-form-item label="初始密码">
          <el-input v-model="form.password" type="password" show-password />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickname" />
        </el-form-item>
        <el-form-item label="工作区角色">
          <el-select v-model="form.workspace_role" style="width: 100%">
            <el-option label="Viewer" value="viewer" />
            <el-option label="Developer" value="developer" />
            <el-option label="Maintainer" value="maintainer" />
            <el-option label="Owner" value="owner" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitUser">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { createUser } from '@/api/user'
import { getWorkspaceMembers, removeWorkspaceMember, updateWorkspaceMember } from '@/api/workspace'

const userStore = useUserStore()
const members = ref([])
const dialogVisible = ref(false)
const saving = ref(false)
const canManageMembers = computed(() => userStore.canAccessWorkspaceGovernance)
const form = reactive({
  username: '',
  password: '',
  email: '',
  nickname: '',
  workspace_role: 'viewer'
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

const loadMembers = async () => {
  if (!userStore.currentWorkspaceId) {
    members.value = []
    return
  }
  try {
    const res = await getWorkspaceMembers(userStore.currentWorkspaceId)
    const memberList = res?.data?.list || []
    members.value = userStore.isPlatformAdmin
      ? memberList
      : memberList.filter(member => String(member.system_role || '').toLowerCase() !== 'admin')
  } catch (error) {
    ElMessage.error('加载工作空间成员失败')
  }
}

const resetForm = () => {
  form.username = ''
  form.password = ''
  form.email = ''
  form.nickname = ''
  form.workspace_role = 'viewer'
}

const openCreateDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const submitUser = async () => {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和初始密码')
    return
  }
  saving.value = true
  try {
    const res = await createUser({
      username: form.username,
      password: form.password,
      email: form.email,
      nickname: form.nickname,
      workspace_role: form.workspace_role
    })
    if (res.code === 200) {
      dialogVisible.value = false
      await Promise.all([loadMembers(), userStore.getUserInfoAction()])
      ElMessage.success('工作区用户已创建')
      return
    }
    ElMessage.error(res.message || '创建工作区用户失败')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '创建工作区用户失败')
  } finally {
    saving.value = false
  }
}

const handleRoleChange = async (row, role) => {
  if (!userStore.currentWorkspaceId || role === row.role) {
    return
  }
  try {
    if (role === 'owner' && row.role !== 'owner') {
      await ElMessageBox.confirm(`确认将 ${row.username} 提升为 Owner 吗？`, '确认操作', { type: 'warning' })
    }
    const res = await updateWorkspaceMember(userStore.currentWorkspaceId, row.id, { role })
    if (res.code === 200) {
      ElMessage.success('角色已更新')
      await Promise.all([loadMembers(), userStore.getUserInfoAction()])
      return
    }
    ElMessage.error(res.message || '更新失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('更新失败')
    }
  }
}

const handleRemoveMember = async (row) => {
  if (!userStore.currentWorkspaceId) {
    return
  }
  try {
    await ElMessageBox.confirm(`确认移除成员 ${row.username} 吗？`, '移除成员', { type: 'warning' })
    const res = await removeWorkspaceMember(userStore.currentWorkspaceId, row.id)
    if (res.code === 200) {
      ElMessage.success('成员已移除')
      await Promise.all([loadMembers(), userStore.getUserInfoAction()])
      return
    }
    ElMessage.error(res.message || '移除失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('移除失败')
    }
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadMembers()
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
</style>
