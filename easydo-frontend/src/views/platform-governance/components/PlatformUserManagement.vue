<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">平台用户</h2>
        <div class="section-subtitle">查看平台用户列表，并在当前 Admin Workspace 下创建平台用户。</div>
      </div>
      <el-button type="primary" @click="openCreateDialog">创建用户</el-button>
    </div>

    <el-table :data="users" style="width: 100%">
      <el-table-column prop="username" label="用户名" min-width="160" />
      <el-table-column prop="nickname" label="昵称" min-width="140" />
      <el-table-column prop="email" label="邮箱" min-width="220" />
      <el-table-column label="系统角色" width="140">
        <template #default="{ row }">
          <el-tag :type="roleTagType(row.system_role || row.role)">{{ roleText(row.system_role || row.role) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120">
        <template #default="{ row }">{{ row.status || '-' }}</template>
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

    <el-dialog v-model="dialogVisible" title="创建平台用户" width="520px">
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
        <el-form-item label="系统角色">
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
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createUser, getUserList } from '@/api/user'

const users = ref([])
const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
const dialogVisible = ref(false)
const saving = ref(false)
const form = reactive({
  username: '',
  password: '',
  email: '',
  nickname: '',
  system_role: 'user'
})

const roleText = (role) => {
  return String(role || '').toLowerCase() === 'admin' ? 'Platform Admin' : 'User'
}

const roleTagType = (role) => {
  return String(role || '').toLowerCase() === 'admin' ? 'danger' : 'info'
}

const extractUsers = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  return []
}

const loadUsers = async () => {
  try {
    const res = await getUserList({ page: page.value, page_size: pageSize.value })
    users.value = extractUsers(res?.data)
    total.value = Number(res?.data?.total || res?.data?.count || users.value.length || 0)
  } catch (error) {
    ElMessage.error('加载平台用户失败')
  }
}

const resetForm = () => {
  form.username = ''
  form.password = ''
  form.email = ''
  form.nickname = ''
  form.system_role = 'user'
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
      ElMessage.error(error?.response?.data?.message || '创建平台用户失败')
    }
  } finally {
    saving.value = false
  }
}

const handlePageChange = async (nextPage) => {
  page.value = nextPage
  await loadUsers()
}

onMounted(() => {
  loadUsers()
})
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

.pagination-row {
  display: flex;
  justify-content: flex-end;
}
</style>
