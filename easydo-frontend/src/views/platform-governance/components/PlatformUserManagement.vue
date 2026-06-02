<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">平台用户</h2>
        <div class="section-subtitle">管理全站账号、账号状态、平台角色和初始密码。</div>
      </div>
      <el-button type="primary" @click="openCreateDialog">创建用户</el-button>
    </div>

    <div class="filter-row">
      <el-input
        v-model="filters.keyword"
        clearable
        placeholder="搜索用户名 / 昵称 / 邮箱"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-select v-model="filters.system_role" clearable placeholder="系统角色" @change="handleSearch">
        <el-option label="User" value="user" />
        <el-option label="Platform Admin" value="admin" />
      </el-select>
      <el-select v-model="filters.status" clearable placeholder="账号状态" @change="handleSearch">
        <el-option label="已启用" value="active" />
        <el-option label="已禁用" value="disabled" />
      </el-select>
      <el-select v-model="filters.workspace_id" clearable filterable placeholder="所属工作空间" @change="handleSearch">
        <el-option
          v-for="workspace in workspaceOptions"
          :key="workspace.id"
          :label="workspace.name"
          :value="workspace.id"
        />
      </el-select>
      <el-button type="primary" @click="handleSearch">筛选</el-button>
    </div>

    <el-table v-loading="loading" :data="users" style="width: 100%">
      <el-table-column prop="username" label="用户名" min-width="150" />
      <el-table-column prop="nickname" label="昵称" min-width="130">
        <template #default="{ row }">{{ row.nickname || '-' }}</template>
      </el-table-column>
      <el-table-column prop="email" label="邮箱" min-width="220" />
      <el-table-column label="所属工作空间" min-width="220">
        <template #default="{ row }">
          <div v-if="formatWorkspaceAssignments(row.workspace_assignments).length" class="workspace-tags">
            <el-tag
              v-for="assignment in formatWorkspaceAssignments(row.workspace_assignments)"
              :key="assignment.workspace_id"
              size="small"
              type="info"
            >
              {{ assignment.workspace_name }} · {{ roleText(assignment.role) }}
            </el-tag>
          </div>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="系统角色" width="150">
        <template #default="{ row }">
          <el-tag :type="roleTagType(row.system_role || row.role)">{{ roleText(row.system_role || row.role) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="首次改密" width="120">
        <template #default="{ row }">
          <el-tag :type="row.must_change_password ? 'warning' : 'info'" size="small">
            {{ row.must_change_password ? '待修改' : '已完成' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="最近登录" width="180">
        <template #default="{ row }">{{ formatDateTime(row.last_login_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="320" fixed="right">
        <template #default="{ row }">
          <el-button link size="small" @click="openDetailDrawer(row)">详情</el-button>
          <el-button link size="small" @click="openEditDialog(row)">编辑</el-button>
          <el-button link size="small" @click="handleToggleSystemRole(row)">
            {{ String(row.system_role || row.role).toLowerCase() === 'admin' ? '降为 User' : '设为 Admin' }}
          </el-button>
          <el-button link size="small" @click="handleResetPassword(row)">重置密码</el-button>
          <el-button
            v-if="row.status === 'active'"
            type="danger"
            link
            size="small"
            @click="handleDisableUser(row)"
          >
            禁用
          </el-button>
          <el-button v-else type="success" link size="small" @click="handleEnableUser(row)">启用</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination-row">
      <el-pagination
        background
        layout="prev, pager, next, total"
        :current-page="page"
        :page-size="pageSize"
        :total="total"
        @current-change="handlePageChange"
      />
    </div>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="520px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="用户名">
          <el-input v-model="form.username" :disabled="Boolean(editingUser)" />
        </el-form-item>
        <el-form-item v-if="!editingUser" label="初始密码">
          <el-input v-model="form.password" type="password" show-password />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickname" />
        </el-form-item>
        <el-form-item v-if="editingUser" label="手机号">
          <el-input v-model="form.phone" />
        </el-form-item>
        <el-form-item v-if="!editingUser" label="系统角色">
          <el-select v-model="form.system_role" style="width: 100%">
            <el-option label="User" value="user" />
            <el-option label="Platform Admin" value="admin" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitUser">保存</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="detailDrawerVisible" title="用户详情" size="640px">
      <div v-loading="detailLoading" class="detail-drawer">
        <template v-if="selectedUser.id">
          <div class="detail-summary">
            <div class="detail-item">
              <span>用户名</span>
              <strong>{{ selectedUser.username }}</strong>
            </div>
            <div class="detail-item">
              <span>邮箱</span>
              <strong>{{ selectedUser.email || '-' }}</strong>
            </div>
            <div class="detail-item">
              <span>昵称</span>
              <strong>{{ selectedUser.nickname || '-' }}</strong>
            </div>
            <div class="detail-item">
              <span>手机号</span>
              <strong>{{ selectedUser.phone || '-' }}</strong>
            </div>
            <div class="detail-item">
              <span>系统角色</span>
              <el-tag :type="roleTagType(selectedUser.system_role || selectedUser.role)">
                {{ roleText(selectedUser.system_role || selectedUser.role) }}
              </el-tag>
            </div>
            <div class="detail-item">
              <span>账号状态</span>
              <el-tag :type="statusTagType(selectedUser.status)">
                {{ statusText(selectedUser.status) }}
              </el-tag>
            </div>
          </div>

          <div class="drawer-section-header">
            <h3>工作区归属</h3>
            <span>{{ workspaceAssignments.length }} 个工作区</span>
          </div>

          <div class="assignment-form">
            <el-select v-model="assignmentForm.workspace_id" filterable placeholder="选择工作区">
              <el-option
                v-for="workspace in availableWorkspaceOptions"
                :key="workspace.id"
                :label="workspace.name"
                :value="workspace.id"
              />
            </el-select>
            <el-select v-model="assignmentForm.role" placeholder="角色">
              <el-option label="Viewer" value="viewer" />
              <el-option label="Developer" value="developer" />
              <el-option label="Maintainer" value="maintainer" />
              <el-option label="Owner" value="owner" />
            </el-select>
            <el-button type="primary" :loading="assignmentSaving" @click="handleAddWorkspaceAssignment">添加</el-button>
          </div>

          <el-table :data="workspaceAssignments" style="width: 100%">
            <el-table-column prop="workspace_name" label="工作区" min-width="170" />
            <el-table-column label="类型" width="120">
              <template #default="{ row }">
                {{ row.workspace_kind === 'admin' ? 'Admin' : 'Normal' }}
              </template>
            </el-table-column>
            <el-table-column label="角色" width="130">
              <template #default="{ row }">{{ roleText(row.role) }}</template>
            </el-table-column>
            <el-table-column label="加入时间" width="180">
              <template #default="{ row }">{{ formatDateTime(row.joined_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="90">
              <template #default="{ row }">
                <el-button type="danger" link size="small" @click="handleRemoveWorkspaceAssignment(row)">移除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { addUserWorkspace, createUser, disableUser, enableUser, getUserDetail, getUserList, getUserWorkspaces, removeUserWorkspace, resetUserPassword, updateUser, updateUserSystemRole } from '@/api/user'
import { getWorkspaceList } from '@/api/workspace'

const users = ref([])
const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
const loading = ref(false)
const detailLoading = ref(false)
const dialogVisible = ref(false)
const detailDrawerVisible = ref(false)
const saving = ref(false)
const assignmentSaving = ref(false)
const editingUser = ref(null)
const selectedUser = reactive({})
const workspaceAssignments = ref([])
const workspaceOptions = ref([])
const filters = reactive({
  keyword: '',
  system_role: '',
  status: '',
  workspace_id: ''
})
const form = reactive({
  username: '',
  password: '',
  email: '',
  nickname: '',
  phone: '',
  system_role: 'user'
})
const assignmentForm = reactive({
  workspace_id: '',
  role: 'viewer'
})

const dialogTitle = computed(() => editingUser.value ? '编辑平台用户' : '创建平台用户')

const roleText = (role) => {
  const map = {
    admin: 'Platform Admin',
    user: 'User',
    viewer: 'Viewer',
    developer: 'Developer',
    maintainer: 'Maintainer',
    owner: 'Owner'
  }
  return map[String(role || '').toLowerCase()] || role || '-'
}

const roleTagType = (role) => {
  return String(role || '').toLowerCase() === 'admin' ? 'danger' : 'info'
}

const statusText = (status) => {
  return status === 'active' ? '已启用' : '已禁用'
}

const statusTagType = (status) => {
  return status === 'active' ? 'success' : 'info'
}

const extractUsers = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  return []
}

const extractList = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  return []
}

const availableWorkspaceOptions = computed(() => {
  const assigned = new Set(workspaceAssignments.value.map(item => Number(item.workspace_id || item.id)))
  return workspaceOptions.value.filter(item => !assigned.has(Number(item.id)))
})

const formatWorkspaceAssignments = (assignments = []) => {
  return Array.isArray(assignments)
    ? assignments.filter(item => item && item.workspace_id)
    : []
}

const formatDateTime = (value) => {
  if (!value) return '-'
  const parsed = typeof value === 'number'
    ? new Date(value < 1e12 ? value * 1000 : value)
    : new Date(value)
  return Number.isNaN(parsed.getTime()) ? '-' : parsed.toLocaleString('zh-CN')
}

const buildListParams = () => {
  const params = {
    page: page.value,
    page_size: pageSize.value
  }
  if (filters.keyword) params.q = filters.keyword
  if (filters.system_role) params.role = filters.system_role
  if (filters.status) params.status = filters.status
  if (filters.workspace_id) params.workspace_filter_id = filters.workspace_id
  return params
}

const loadUsers = async () => {
  loading.value = true
  try {
    const res = await getUserList(buildListParams())
    users.value = extractUsers(res?.data)
    total.value = Number(res?.data?.total || res?.data?.count || users.value.length || 0)
  } catch (error) {
    ElMessage.error('加载平台用户失败')
  } finally {
    loading.value = false
  }
}

const resetForm = () => {
  form.username = ''
  form.password = ''
  form.email = ''
  form.nickname = ''
  form.phone = ''
  form.system_role = 'user'
  editingUser.value = null
}

const openCreateDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const openEditDialog = (row) => {
  editingUser.value = row
  form.username = row.username || ''
  form.password = ''
  form.email = row.email || ''
  form.nickname = row.nickname || ''
  form.phone = row.phone || ''
  form.system_role = row.system_role || row.role || 'user'
  dialogVisible.value = true
}

const submitUser = async () => {
  if (!editingUser.value) {
    if (!form.username || !form.password || !form.email) {
      ElMessage.warning('请输入用户名、邮箱和初始密码')
      return
    }
  }
  if (editingUser.value && !form.email) {
    ElMessage.warning('请输入邮箱')
    return
  }
  saving.value = true
  try {
    if (editingUser.value) {
      const res = await updateUser(editingUser.value.id, {
        email: form.email,
        phone: form.phone,
        nickname: form.nickname
      })
      if (res.code === 200) {
        dialogVisible.value = false
        await loadUsers()
        ElMessage.success('平台用户已更新')
        return
      }
      ElMessage.error(res.message || '更新平台用户失败')
      return
    }
    if (form.system_role === 'admin') {
      await ElMessageBox.confirm('确认授予 Platform Admin 吗？', '确认操作', { type: 'warning' })
    }
    const res = await createUser({
      username: form.username,
      password: form.password,
      email: form.email,
      nickname: form.nickname,
      system_role: form.system_role
    })
    if (res.code === 200) {
      dialogVisible.value = false
      await loadUsers()
      ElMessage.success('平台用户已创建')
      return
    }
    ElMessage.error(res.message || '创建平台用户失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || (editingUser.value ? '更新平台用户失败' : '创建平台用户失败'))
    }
  } finally {
    saving.value = false
  }
}

const setSelectedUser = (nextUser = {}) => {
  Object.keys(selectedUser).forEach(key => {
    delete selectedUser[key]
  })
  Object.assign(selectedUser, nextUser || {})
}

const resetAssignmentForm = () => {
  assignmentForm.workspace_id = ''
  assignmentForm.role = 'viewer'
}

const loadWorkspaceOptions = async () => {
  try {
    const res = await getWorkspaceList()
    workspaceOptions.value = extractList(res?.data)
  } catch (error) {
    ElMessage.error('加载工作区列表失败')
  }
}

const loadUserWorkspaceAssignments = async (userId) => {
  const res = await getUserWorkspaces(userId)
  workspaceAssignments.value = extractList(res?.data)
}

const openDetailDrawer = async (row) => {
  setSelectedUser(row)
  detailDrawerVisible.value = true
  detailLoading.value = true
  resetAssignmentForm()
  try {
    const detailRes = await getUserDetail(row.id)
    const workspaceRes = await getUserWorkspaces(row.id)
    workspaceAssignments.value = extractList(workspaceRes?.data)
    await loadWorkspaceOptions()
    setSelectedUser({ ...row, ...(detailRes?.data || {}) })
  } catch (error) {
    ElMessage.error('加载用户详情失败')
  } finally {
    detailLoading.value = false
  }
}

const handleAddWorkspaceAssignment = async () => {
  if (!selectedUser.id) {
    return
  }
  const workspaceID = Number(assignmentForm.workspace_id)
  if (!workspaceID) {
    ElMessage.warning('请选择工作区')
    return
  }
  assignmentSaving.value = true
  try {
    const res = await addUserWorkspace(selectedUser.id, {
      workspace_id: workspaceID,
      role: assignmentForm.role
    })
    if (res.code === 200) {
      ElMessage.success('工作区归属已添加')
      resetAssignmentForm()
      await loadUserWorkspaceAssignments(selectedUser.id)
      return
    }
    ElMessage.error(res.message || '添加工作区归属失败')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '添加工作区归属失败')
  } finally {
    assignmentSaving.value = false
  }
}

const handleRemoveWorkspaceAssignment = async (row) => {
  if (!selectedUser.id) {
    return
  }
  try {
    await ElMessageBox.confirm(`确认从 ${row.workspace_name} 移除该用户吗？`, '移除工作区归属', { type: 'warning' })
    const res = await removeUserWorkspace(selectedUser.id, row.workspace_id)
    if (res.code === 200) {
      ElMessage.success('工作区归属已移除')
      await loadUserWorkspaceAssignments(selectedUser.id)
      return
    }
    ElMessage.error(res.message || '移除工作区归属失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '移除工作区归属失败')
    }
  }
}

const handleDisableUser = async (row) => {
  try {
    const { value: disableReason } = await ElMessageBox.prompt(`确认禁用用户 ${row.username} 吗？`, '禁用用户', {
      confirmButtonText: '禁用',
      cancelButtonText: '取消',
      type: 'warning',
      inputPlaceholder: '禁用原因'
    })
    const res = await disableUser(row.id, { reason: disableReason })
    if (res.code === 200) {
      ElMessage.success('用户已禁用')
      await loadUsers()
      return
    }
    ElMessage.error(res.message || '禁用用户失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '禁用用户失败')
    }
  }
}

const handleEnableUser = async (row) => {
  try {
    await ElMessageBox.confirm(`确认启用用户 ${row.username} 吗？`, '启用用户', { type: 'warning' })
    const res = await enableUser(row.id)
    if (res.code === 200) {
      ElMessage.success('用户已启用')
      await loadUsers()
      return
    }
    ElMessage.error(res.message || '启用用户失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '启用用户失败')
    }
  }
}

const handleResetPassword = async (row) => {
  try {
    const { value: password } = await ElMessageBox.prompt(`请输入 ${row.username} 的新临时密码`, '重置密码', {
      confirmButtonText: '重置',
      cancelButtonText: '取消',
      inputType: 'password',
      inputPattern: /^.{6,}$/,
      inputErrorMessage: '密码至少 6 位'
    })
    const res = await resetUserPassword(row.id, { new_password: password })
    if (res.code === 200) {
      ElMessage.success('密码已重置，用户下次登录需修改密码')
      await loadUsers()
      return
    }
    ElMessage.error(res.message || '重置密码失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '重置密码失败')
    }
  }
}

const handleToggleSystemRole = async (row) => {
  const currentRole = String(row.system_role || row.role || '').toLowerCase()
  const nextRole = currentRole === 'admin' ? 'user' : 'admin'
  try {
    await ElMessageBox.confirm(
      `确认将 ${row.username} 的系统角色调整为 ${roleText(nextRole)} 吗？`,
      '调整系统角色',
      { type: 'warning' }
    )
    const res = await updateUserSystemRole(row.id, { system_role: nextRole })
    if (res.code === 200) {
      ElMessage.success('系统角色已更新')
      await loadUsers()
      return
    }
    ElMessage.error(res.message || '更新系统角色失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '更新系统角色失败')
    }
  }
}

const handleSearch = async () => {
  page.value = 1
  await loadUsers()
}

const handlePageChange = async (nextPage) => {
  page.value = nextPage
  await loadUsers()
}

onMounted(async () => {
  await loadWorkspaceOptions()
  await loadUsers()
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
  max-width: 1120px;
}

.filter-row .el-input {
  flex: 1;
}

.filter-row .el-select {
  width: 160px;
}

.pagination-row {
  display: flex;
  justify-content: flex-end;
}

.workspace-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.detail-drawer {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.detail-summary {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.detail-item {
  display: flex;
  min-height: 58px;
  flex-direction: column;
  justify-content: center;
  gap: 6px;
  border-bottom: 1px solid var(--border-light);
}

.detail-item span,
.drawer-section-header span {
  color: var(--text-secondary);
  font-size: 13px;
}

.detail-item strong {
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 600;
  word-break: break-word;
}

.drawer-section-header,
.assignment-form {
  display: flex;
  align-items: center;
  gap: 12px;
}

.drawer-section-header {
  justify-content: space-between;
}

.drawer-section-header h3 {
  margin: 0;
  color: var(--text-primary);
  font-size: 16px;
  font-weight: 600;
}

.assignment-form .el-select:first-child {
  flex: 1;
}

.assignment-form .el-select:nth-child(2) {
  width: 150px;
}

@media (max-width: 900px) {
  .section-header-row,
  .filter-row,
  .assignment-form {
    flex-direction: column;
    align-items: stretch;
  }

  .filter-row .el-select,
  .assignment-form .el-select:nth-child(2) {
    width: 100%;
  }

  .detail-summary {
    grid-template-columns: 1fr;
  }
}
</style>
