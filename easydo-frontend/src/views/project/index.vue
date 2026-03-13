<template>
  <div class="project-container">
    <div class="project-header">
      <div>
        <h1 class="page-title">项目</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
      <el-button type="primary" @click="handleCreate">
        <el-icon><Plus /></el-icon>
        添加项目
      </el-button>
    </div>

    <div class="project-filters">
      <div class="filter-tabs">
        <div class="tab-item active">
          <span>常用</span>
        </div>
        <div class="tab-divider"></div>
        <div
          class="tab-item"
          :class="{ active: activeTab === 'all' }"
          @click="activeTab = 'all'; fetchProjects()"
        >
          <span>所有</span>
          <span class="tab-count">{{ total }}</span>
        </div>
        <div
          class="tab-item"
          :class="{ active: activeTab === 'created' }"
          @click="activeTab = 'created'; fetchProjects()"
        >
          <span>我创建的</span>
          <span class="tab-count">{{ createdCount }}</span>
        </div>
        <div
          class="tab-item"
          :class="{ active: activeTab === 'favorited' }"
          @click="activeTab = 'favorited'; fetchProjects()"
        >
          <span>我收藏的</span>
          <span class="tab-count">{{ favoritedCount }}</span>
        </div>
      </div>

      <div class="filter-search">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索名称"
          prefix-icon="Search"
          clearable
          style="width: 240px"
          @input="handleSearch"
        />
      </div>
    </div>

    <div class="project-table" v-loading="loading">
      <el-table :data="projectList" style="width: 100%" row-key="id">
        <el-table-column prop="name" label="名称" min-width="240">
          <template #default="{ row }">
            <div class="project-name-cell">
              <div class="project-icon" :style="{ background: row.color || '#409EFF' }">
                {{ row.name.charAt(0).toUpperCase() }}
              </div>
              <span class="project-name-text">{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="pipeline_count" label="流水线数" width="100" align="center" />
        <el-table-column label="最新执行人" width="120" align="center">
          <template #default="{ row }">
            <span>{{ row.latest_runner || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="最新执行时间" width="160" align="center">
          <template #default="{ row }">
            <span v-if="row.latest_run_time && row.latest_run_time !== '0001-01-01T00:00:00Z'">{{ formatDateTime(row.latest_run_time) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="执行结果" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.latest_run_status" :type="getStatusType(row.latest_run_status)" size="small">
              {{ getStatusText(row.latest_run_status) }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="创建人" width="120" align="center">
          <template #default="{ row }">
            <span>{{ row.owner?.nickname || row.owner?.username || '未知' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="120" align="center">
          <template #default="{ row }">
            <span>{{ formatDate(row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80" align="center">
          <template #default="{ row }">
            <div class="action-cell">
              <el-tooltip :content="row.is_favorited ? '取消收藏' : '收藏'" placement="top">
                <el-icon
                  class="action-icon"
                  :class="{ active: row.is_favorited }"
                  @click="handleFavorite(row)"
                >
                  <Star v-if="row.is_favorited" />
                  <StarFilled v-else />
                </el-icon>
              </el-tooltip>
              <el-dropdown trigger="click" @command="(cmd) => handleCommand(cmd, row)">
                <el-icon class="action-icon more-icon"><MoreFilled /></el-icon>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="edit">编辑</el-dropdown-item>
                    <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && projectList.length === 0" description="暂无项目" />
    </div>

    <!-- 创建/编辑项目弹窗 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑项目' : '创建项目'"
      width="640px"
      :close-on-click-modal="false"
      :append-to-body="true"
      top="100px"
    >

      <el-form
        ref="formRef"
        :model="formData"
        :rules="formRules"
        label-width="80px"
      >
        <el-form-item label="项目名称" prop="name">
          <el-input v-model="formData.name" placeholder="请输入项目名称" maxlength="64" show-word-limit />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="formData.description"
            type="textarea"
            placeholder="请输入项目描述"
            :rows="3"
            maxlength="500"
            show-word-limit
          />
        </el-form-item>
        <el-form-item label="颜色" prop="color">
          <el-color-picker v-model="formData.color" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">
          确定
        </el-button>
      </template>
    </el-dialog>
    
    <!-- 删除确认对话框 -->
    <el-dialog
      v-model="deleteDialogVisible"
      title="确认删除"
      width="400px"
      :close-on-click-modal="false"
      :append-to-body="true"
    >
      <div class="delete-warning">
        <el-icon color="#E6A23C" size="24"><Warning /></el-icon>
        <p>确定要删除项目 <strong>{{ deleteForm.name }}</strong> 吗？</p>
        <p class="delete-tip">此操作不可恢复，请输入项目名称以确认。</p>
      </div>
      <el-form :model="deleteForm" label-width="0">
        <el-form-item>
          <el-input
            v-model="deleteForm.confirmName"
            :placeholder="`请输入 '${deleteForm.name}' 以确认`"
          />
        </el-form-item>
      </el-form>
      
      <template #footer>
        <el-button @click="deleteDialogVisible = false">取消</el-button>
        <el-button
          type="danger"
          :loading="deleteLoading"
          :disabled="deleteForm.confirmName !== deleteForm.name"
          @click="handleDeleteConfirm"
        >
          删除
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus,
  Search,
  Star,
  StarFilled,
  MoreFilled,
  Warning
} from '@element-plus/icons-vue'
import { getProjectList, createProject, updateProject, deleteProject, toggleFavorite } from '@/api/project'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const activeTab = ref('all')
const searchKeyword = ref('')
const total = ref(0)
const createdCount = ref(0)
const favoritedCount = ref(0)
const loading = ref(false)
const projectList = ref([])

const dialogVisible = ref(false)
const isEdit = ref(false)
const submitting = ref(false)
const currentProjectId = ref(null)
const formRef = ref(null)

// 删除对话框相关
const deleteDialogVisible = ref(false)
const deleteLoading = ref(false)
const deleteForm = reactive({
  id: null,
  name: '',
  confirmName: ''
})

const formData = reactive({
  name: '',
  description: '',
  color: '#409EFF'
})

const formRules = {
  name: [
    { required: true, message: '请输入项目名称', trigger: 'blur' },
    { min: 2, max: 64, message: '项目名称长度为2-64个字符', trigger: 'blur' }
  ]
}

let searchTimer = null

const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  // 处理 Unix 时间戳（秒）
  const timestamp = typeof dateStr === 'number' ? dateStr * 1000 : dateStr
  const date = new Date(timestamp)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const formatDateTime = (dateStr) => {
  if (!dateStr) return '-'
  // 处理 Unix 时间戳（秒）
  const timestamp = typeof dateStr === 'number' ? dateStr * 1000 : dateStr
  const date = new Date(timestamp)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day} ${hours}:${minutes}`
}

const getStatusType = (status) => {
  switch (status) {
    case 'success':
    case 'execute_success':
      return 'success'
    case 'running':
      return 'warning'
    case 'failed':
    case 'execute_failed':
    case 'schedule_failed':
    case 'dispatch_timeout':
    case 'lease_expired':
      return 'danger'
    case 'queued':
    case 'assigned':
    case 'dispatching':
    case 'pulling':
    case 'acked':
      return 'warning'
    case 'cancelled':
      return 'info'
    default:
      return 'info'
  }
}

const getStatusText = (status) => {
  switch (status) {
    case 'success':
    case 'execute_success':
      return '成功'
    case 'running':
      return '运行中'
    case 'failed':
    case 'execute_failed':
      return '失败'
    case 'schedule_failed':
      return '调度失败'
    case 'dispatch_timeout':
      return '派发超时'
    case 'lease_expired':
      return '租约失效'
    case 'queued':
      return '排队中'
    case 'assigned':
      return '已分配'
    case 'dispatching':
      return '派发中'
    case 'pulling':
      return '等待拉取'
    case 'acked':
      return '已确认'
    case 'cancelled':
      return '已取消'
    default:
      return status
  }
}

const fetchProjects = async () => {
  loading.value = true
  try {
    const params = {
      page: 1,
      page_size: 100,
      tab: activeTab.value
    }

    if (searchKeyword.value) {
      params.keyword = searchKeyword.value
    }

    const res = await getProjectList(params)
    if (res.code === 200) {
      projectList.value = res.data.list || []
      total.value = res.data.total || 0
      // 更新统计数据
      createdCount.value = projectList.value.length
      favoritedCount.value = projectList.value.filter(p => p.is_favorited).length
    }
  } catch (error) {
    console.error('获取项目列表失败:', error)
    ElMessage.error('获取项目列表失败')
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    fetchProjects()
  }, 300)
}

const handleCreate = () => {
  isEdit.value = false
  currentProjectId.value = null
  formData.name = ''
  formData.description = ''
  formData.color = '#409EFF'
  dialogVisible.value = true
}

const handleEdit = (project) => {
  isEdit.value = true
  currentProjectId.value = project.id
  formData.name = project.name
  formData.description = project.description || ''
  formData.color = project.color || '#409EFF'
  dialogVisible.value = true
}

const handleDelete = (project) => {
  deleteForm.id = project.id
  deleteForm.name = project.name
  deleteForm.confirmName = ''
  deleteDialogVisible.value = true
}

const handleDeleteConfirm = async () => {
  if (deleteForm.confirmName !== deleteForm.name) {
    ElMessage.error('名称不匹配')
    return
  }
  
  deleteLoading.value = true
  try {
    const res = await deleteProject(deleteForm.id)
    if (res.code === 200) {
      ElMessage.success('删除成功')
      deleteDialogVisible.value = false
      fetchProjects()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (error) {
    console.error('删除项目失败:', error)
    ElMessage.error('删除失败')
  } finally {
    deleteLoading.value = false
  }
}

const handleFavorite = async (project) => {
  try {
    const res = await toggleFavorite(project.id)
    if (res.code === 200) {
      project.is_favorited = !project.is_favorited
      ElMessage.success(project.is_favorited ? '收藏成功' : '取消收藏')
      // 重新获取项目列表以更新排序
      fetchProjects()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (error) {
    console.error('收藏操作失败:', error)
    ElMessage.error('操作失败')
  }
}

const handleCommand = (command, project) => {
  switch (command) {
    case 'edit':
      handleEdit(project)
      break
    case 'delete':
      handleDelete(project)
      break
  }
}

const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    submitting.value = true

    if (isEdit.value) {
      const res = await updateProject(currentProjectId.value, formData)
      if (res.code === 200) {
        ElMessage.success('更新成功')
        dialogVisible.value = false
        fetchProjects()
      } else {
        ElMessage.error(res.message || '更新失败')
      }
    } else {
      const res = await createProject(formData)
      if (res.code === 200) {
        ElMessage.success('创建成功')
        dialogVisible.value = false
        fetchProjects()
      } else {
        ElMessage.error(res.message || '创建失败')
      }
    }
  } catch (error) {
    console.error('提交失败:', error)
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  fetchProjects()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.project-container {
  animation: float-up 0.45s ease both;

  .project-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;

    .page-title {
      font-family: $font-family-display;
      font-size: 32px;
      font-weight: 760;
      letter-spacing: -0.03em;
      color: var(--text-primary);
    }

    :deep(.el-button--primary) {
      height: 42px;
      padding: 0 20px;
    }
  }

  .project-filters {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 14px 16px;
    margin-bottom: 18px;
    border-radius: $radius-xl;
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    box-shadow: var(--shadow-sm);
    backdrop-filter: $blur-sm;
    -webkit-backdrop-filter: $blur-sm;

    .filter-tabs {
      display: flex;
      align-items: center;
      gap: 5px;

      .tab-item {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 8px 14px;
        border-radius: $radius-full;
        color: var(--text-secondary);
        cursor: pointer;
        font-size: 13px;
        font-weight: 600;
        transition: all $transition-fast;

        &:hover {
          color: var(--primary-color);
          background: var(--primary-lighter);
        }

        &.active {
          color: var(--primary-color);
          background: rgba($primary-color, 0.17);
          box-shadow: inset 0 0 0 1px rgba($primary-color, 0.32);
        }

        .tab-count {
          min-width: 20px;
          height: 20px;
          padding: 0 7px;
          border-radius: $radius-full;
          display: inline-flex;
          align-items: center;
          justify-content: center;
          background: var(--bg-elevated);
          color: var(--text-tertiary);
          font-size: 11px;
        }
      }

      .tab-divider {
        width: 1px;
        height: 16px;
        background: var(--border-color-light);
        margin: 0 4px;
      }
    }

    .filter-search {
      flex: 1;
      min-width: 180px;
    }
  }

  .project-table {
    border-radius: $radius-xl;
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    box-shadow: var(--shadow-md);
    backdrop-filter: $blur-md;
    -webkit-backdrop-filter: $blur-md;
    padding: 16px;

    :deep(.el-table) {
      background: transparent;

      th.el-table__cell {
        background: rgba(255, 255, 255, 0.45);
        border-bottom: 1px solid var(--border-color-light);
        height: 44px;
      }

      td.el-table__cell {
        height: 54px;
      }
    }

    .project-name-cell {
      display: flex;
      align-items: center;
      gap: 12px;

      .project-icon {
        width: 34px;
        height: 34px;
        border-radius: 12px;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        color: white;
        font-size: 13px;
        font-weight: 700;
        box-shadow: 0 10px 20px rgba(11, 38, 73, 0.2);
      }

      .project-name-text {
        color: var(--text-primary);
        font-weight: 620;
      }
    }

    .action-cell {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;

      .action-icon {
        width: 32px;
        height: 32px;
        border-radius: 10px;
        border: 1px solid transparent;
        color: var(--text-secondary);
        display: inline-flex;
        align-items: center;
        justify-content: center;
        cursor: pointer;
        transition: all $transition-fast;

        &:hover {
          color: var(--primary-color);
          background: var(--primary-lighter);
          border-color: rgba($primary-color, 0.28);
        }

        &.active {
          color: $warning-color;
          background: $warning-light;
          border-color: rgba($warning-color, 0.34);
        }

        &.more-icon {
          font-size: 17px;
        }
      }
    }

    .el-empty {
      padding: 54px 0;
    }
  }
}

.delete-warning {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 18px 0;

  p {
    margin: 14px 0 6px;
    color: var(--text-primary);
    font-size: 14px;
    font-weight: 600;
  }

  .delete-tip {
    color: var(--text-muted);
    font-size: 12px;
  }
}

@media (max-width: 1024px) {
  .project-container {
    .project-filters {
      flex-direction: column;
      align-items: stretch;

      .filter-tabs {
        overflow: auto;
        padding-bottom: 4px;
      }
    }
  }
}

@media (max-width: 768px) {
  .project-container {
    .project-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 10px;

      .page-title {
        font-size: 27px;
      }
    }
  }
}
</style>
