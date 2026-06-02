<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">成员管理</h2>
        <div class="section-subtitle">管理当前工作区成员角色，并保留危险操作二次确认。</div>
      </div>
      <div class="header-actions">
        <el-button v-if="canManageMembers" @click="openAddExistingDialog">添加已有用户</el-button>
        <el-button v-if="canManageMembers" type="primary" @click="openCreateDialog">创建用户</el-button>
      </div>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

    <template v-else>
      <div class="filter-row">
        <el-input
          v-model="filters.keyword"
          clearable
          placeholder="搜索用户名 / 昵称 / 邮箱"
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
        <el-select v-model="filters.role" clearable placeholder="工作区角色" @change="handleSearch">
          <el-option label="Viewer" value="viewer" />
          <el-option label="Developer" value="developer" />
          <el-option label="Maintainer" value="maintainer" />
          <el-option label="Owner" value="owner" />
        </el-select>
        <el-button type="primary" @click="handleSearch">筛选</el-button>
      </div>

      <el-table v-loading="loading" :data="members" style="width: 100%">
        <el-table-column prop="username" label="用户名" width="160" />
        <el-table-column prop="nickname" label="昵称" width="140">
          <template #default="{ row }">{{ row.nickname || '-' }}</template>
        </el-table-column>
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
    </template>

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

    <el-dialog v-model="addExistingDialogVisible" title="添加已有用户" width="720px">
      <el-form :model="addExistingForm" label-width="110px">
        <el-form-item label="查找用户">
          <el-input
            v-model="memberCandidateKeyword"
            clearable
            placeholder="输入邮箱 / 手机号 / 用户名搜索"
            @input="handleCandidateKeywordInput"
            @keyup.enter="searchMemberCandidatesNow"
            @clear="clearMemberCandidates"
          />
        </el-form-item>
        <el-form-item label="候选用户">
          <div class="candidate-picker">
            <el-radio-group v-model="selectedCandidateUserId" class="candidate-radio-group">
              <el-table
                v-loading="memberCandidateLoading"
                :data="memberCandidateRows"
                :empty-text="memberCandidateEmptyText"
                height="240"
                style="width: 100%"
                @row-click="selectCandidateRow"
              >
                <el-table-column width="54">
                  <template #default="{ row }">
                    <el-radio :label="row.id"><span class="sr-only">选择 {{ row.username }}</span></el-radio>
                  </template>
                </el-table-column>
                <el-table-column prop="username" label="用户名" min-width="130" />
                <el-table-column prop="nickname" label="昵称" min-width="120">
                  <template #default="{ row }">{{ row.nickname || '-' }}</template>
                </el-table-column>
                <el-table-column prop="email" label="邮箱" min-width="190">
                  <template #default="{ row }">{{ row.email || '-' }}</template>
                </el-table-column>
                <el-table-column prop="phone" label="手机号" min-width="130">
                  <template #default="{ row }">{{ row.phone || '-' }}</template>
                </el-table-column>
              </el-table>
            </el-radio-group>
          </div>
        </el-form-item>
        <el-form-item label="工作区角色">
          <el-select v-model="addExistingForm.role" style="width: 100%">
            <el-option label="Viewer" value="viewer" />
            <el-option label="Developer" value="developer" />
            <el-option label="Maintainer" value="maintainer" />
            <el-option label="Owner" value="owner" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addExistingDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="addingExisting" @click="submitExistingMember">添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { createUser } from '@/api/user'
import { addWorkspaceMember, getWorkspaceMembers, removeWorkspaceMember, searchWorkspaceMemberCandidates, updateWorkspaceMember } from '@/api/workspace'

const userStore = useUserStore()
const members = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const addExistingDialogVisible = ref(false)
const saving = ref(false)
const addingExisting = ref(false)
const memberCandidateKeyword = ref('')
const memberCandidateRows = ref([])
const memberCandidateLoading = ref(false)
const selectedCandidateUserId = ref(null)
const canManageMembers = computed(() => userStore.canAccessWorkspaceGovernance)
const filters = reactive({
  keyword: '',
  role: ''
})
const form = reactive({
  username: '',
  password: '',
  email: '',
  nickname: '',
  workspace_role: 'viewer'
})
const addExistingForm = reactive({
  role: 'viewer'
})
let memberCandidateSearchTimer = null

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

const selectedCandidate = computed(() => {
  return memberCandidateRows.value.find(item => Number(item.id) === Number(selectedCandidateUserId.value)) || null
})

const memberCandidateEmptyText = computed(() => {
  const keyword = memberCandidateKeyword.value.trim()
  if (!keyword) return '输入邮箱、手机号或用户名后搜索'
  return memberCandidateLoading.value ? '搜索中...' : '没有匹配的可添加用户'
})

const buildMemberParams = () => {
  const params = {}
  if (filters.keyword) params.q = filters.keyword
  if (filters.role) params.role = filters.role
  return params
}

const loadMembers = async () => {
  if (!userStore.currentWorkspaceId) {
    members.value = []
    return
  }
  loading.value = true
  try {
    const res = await getWorkspaceMembers(userStore.currentWorkspaceId, buildMemberParams())
    const memberList = res?.data?.list || []
    members.value = userStore.isPlatformAdmin
      ? memberList
      : memberList.filter(member => String(member.system_role || '').toLowerCase() !== 'admin')
  } catch (error) {
    ElMessage.error('加载工作空间成员失败')
  } finally {
    loading.value = false
  }
}

const resetForm = () => {
  form.username = ''
  form.password = ''
  form.email = ''
  form.nickname = ''
  form.workspace_role = 'viewer'
}

const resetAddExistingForm = () => {
  addExistingForm.role = 'viewer'
  memberCandidateKeyword.value = ''
  memberCandidateRows.value = []
  selectedCandidateUserId.value = null
  clearCandidateSearchTimer()
}

const openCreateDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const openAddExistingDialog = () => {
  resetAddExistingForm()
  addExistingDialogVisible.value = true
}

const clearCandidateSearchTimer = () => {
  if (memberCandidateSearchTimer) {
    clearTimeout(memberCandidateSearchTimer)
    memberCandidateSearchTimer = null
  }
}

const clearMemberCandidates = () => {
  clearCandidateSearchTimer()
  memberCandidateRows.value = []
  selectedCandidateUserId.value = null
}

const normalizeCandidateRows = (rows = []) => {
  return Array.isArray(rows)
    ? rows.filter(row => row && row.id)
    : []
}

const searchMemberCandidatesNow = async () => {
  clearCandidateSearchTimer()
  selectedCandidateUserId.value = null
  const keyword = memberCandidateKeyword.value.trim()
  if (!userStore.currentWorkspaceId || !keyword) {
    memberCandidateRows.value = []
    return
  }
  memberCandidateLoading.value = true
  try {
    const res = await searchWorkspaceMemberCandidates(userStore.currentWorkspaceId, {
      q: keyword,
      limit: 20
    })
    memberCandidateRows.value = normalizeCandidateRows(res?.data?.list)
  } catch (error) {
    memberCandidateRows.value = []
    ElMessage.error(error?.response?.data?.message || '搜索用户失败')
  } finally {
    memberCandidateLoading.value = false
  }
}

const handleCandidateKeywordInput = () => {
  clearCandidateSearchTimer()
  selectedCandidateUserId.value = null
  const keyword = memberCandidateKeyword.value.trim()
  if (!keyword) {
    memberCandidateRows.value = []
    return
  }
  memberCandidateSearchTimer = setTimeout(() => {
    searchMemberCandidatesNow()
  }, 300)
}

const selectCandidateRow = (row) => {
  if (row?.id) {
    selectedCandidateUserId.value = row.id
  }
}

const submitUser = async () => {
  if (!form.username || !form.password || !form.email) {
    ElMessage.warning('请输入用户名、邮箱和初始密码')
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

const submitExistingMember = async () => {
  if (!userStore.currentWorkspaceId) {
    return
  }
  if (!selectedCandidateUserId.value) {
    ElMessage.warning('请先选择用户')
    return
  }
  addingExisting.value = true
  try {
    if (addExistingForm.role === 'owner') {
      const candidateName = selectedCandidate.value?.username || '该用户'
      await ElMessageBox.confirm(`确认将 ${candidateName} 加入为 Owner 吗？`, '确认操作', { type: 'warning' })
    }
    const res = await addWorkspaceMember(userStore.currentWorkspaceId, {
      user_id: selectedCandidateUserId.value,
      role: addExistingForm.role
    })
    if (res.code === 200) {
      addExistingDialogVisible.value = false
      await Promise.all([loadMembers(), userStore.getUserInfoAction()])
      ElMessage.success('成员已添加')
      return
    }
    ElMessage.error(res.message || '添加成员失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '添加成员失败')
    }
  } finally {
    addingExisting.value = false
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

const handleSearch = async () => {
  await loadMembers()
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadMembers()
}, { immediate: true })

watch(addExistingDialogVisible, (visible) => {
  if (!visible) {
    resetAddExistingForm()
  }
})

onBeforeUnmount(() => {
  clearCandidateSearchTimer()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.governance-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header-row,
.header-actions,
.filter-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.section-header-row {
  justify-content: space-between;
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

.filter-row {
  align-items: center;
  max-width: 720px;
}

.filter-row .el-input {
  flex: 1;
}

.filter-row .el-select {
  width: 180px;
}

.candidate-picker,
.candidate-radio-group {
  width: 100%;
}

.candidate-picker {
  border: 1px solid var(--border-color);
  border-radius: $radius-md;
  overflow: hidden;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.empty-hint {
  padding: 24px;
  background: var(--bg-card);
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-align: center;
}

@media (max-width: 900px) {
  .section-header-row,
  .header-actions,
  .filter-row {
    flex-direction: column;
    align-items: stretch;
  }

  .filter-row .el-select {
    width: 100%;
  }
}
</style>
