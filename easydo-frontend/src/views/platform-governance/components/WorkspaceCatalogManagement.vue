<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">工作区管理</h2>
        <div class="section-subtitle">查看平台可见工作区、编辑基础信息，并快速切到工作区治理页。</div>
      </div>
      <el-button type="primary" @click="openCreateDialog">创建工作区</el-button>
    </div>

    <el-table :data="workspaces" style="width: 100%">
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column label="类型" width="140">
        <template #default="{ row }">{{ workspaceKindText(row) }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column prop="description" label="描述" min-width="220" show-overflow-tooltip />
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEditDialog(row)">编辑</el-button>
          <el-button v-if="canEnterWorkspaceGovernance(row)" link type="primary" @click="enterWorkspaceGovernance(row)">进入治理</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑工作区' : '创建工作区'" width="560px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="form.name" />
          <div v-if="!form.id" class="field-tip">只允许英文字母和数字</div>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="状态" v-if="form.id">
          <el-select v-model="form.status" style="width: 100%">
            <el-option label="active" value="active" />
            <el-option label="archived" value="archived" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitWorkspace">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { createWorkspace, getWorkspaceList, updateWorkspace } from '@/api/workspace'

const router = useRouter()
const userStore = useUserStore()
const workspaces = ref([])
const dialogVisible = ref(false)
const saving = ref(false)
const form = reactive({
  id: 0,
  name: '',
  description: '',
  status: 'active'
})

const extractList = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  return []
}

const isCurrentAdminWorkspace = (row) => {
  return userStore.isAdminWorkspace && Number(row?.id) === Number(userStore.currentWorkspaceId)
}

const workspaceKindText = (row) => {
  return isCurrentAdminWorkspace(row) ? 'Admin Workspace' : 'Normal Workspace'
}

const canEnterWorkspaceGovernance = (row) => {
  return !isCurrentAdminWorkspace(row)
}

const loadWorkspaces = async () => {
  try {
    const res = await getWorkspaceList()
    workspaces.value = extractList(res?.data)
  } catch (error) {
    ElMessage.error('加载工作区列表失败')
  }
}

const resetForm = () => {
  form.id = 0
  form.name = ''
  form.description = ''
  form.status = 'active'
}

const openCreateDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const openEditDialog = (row) => {
  form.id = row.id
  form.name = row.name || ''
  form.description = row.description || ''
  form.status = row.status || 'active'
  dialogVisible.value = true
}

const submitWorkspace = async () => {
  if (!form.name) {
    ElMessage.warning('请输入工作区名称')
    return
  }
  if (!/^[A-Za-z0-9]+$/.test(form.name)) {
    ElMessage.warning('工作区名称只允许英文字母和数字')
    return
  }
  saving.value = true
  try {
    if (form.id) {
      const res = await updateWorkspace(form.id, {
        name: form.name,
        description: form.description,
        status: form.status
      })
      if (res.code !== 200) {
        ElMessage.error(res.message || '更新工作区失败')
        return
      }
      ElMessage.success('工作区已更新')
    } else {
      const res = await createWorkspace({
        name: form.name,
        description: form.description
      })
      if (res.code !== 200) {
        ElMessage.error(res.message || '创建工作区失败')
        return
      }
      ElMessage.success('工作区已创建')
    }
    dialogVisible.value = false
    await Promise.all([loadWorkspaces(), userStore.getUserInfoAction()])
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '保存工作区失败')
  } finally {
    saving.value = false
  }
}

const enterWorkspaceGovernance = async (row) => {
  try {
    localStorage.setItem('current_workspace_id', String(row.id))
    userStore.setCurrentWorkspaceById(row.id)
    await userStore.getUserInfoAction()
    await router.push('/workspace-governance')
  } catch (error) {
    ElMessage.error('切换工作区失败')
  }
}

onMounted(() => {
  loadWorkspaces()
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

.field-tip {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.4;
}
</style>
