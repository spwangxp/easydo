<template>
  <div class="pipeline-container">
    <div class="pipeline-header">
      <h1 class="page-title">流水线</h1>
      <el-button type="primary" @click="handleCreate">
        <el-icon><Plus /></el-icon>
        新建流水线
      </el-button>
    </div>
    
    <div class="pipeline-filters">
      <div class="filter-tabs">
        <div class="tab-item active">
          <span>常用</span>
        </div>
        <div class="tab-divider"></div>
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'all' }"
          @click="activeTab = 'all'"
        >
          <span>所有</span>
          <span class="tab-count">{{ tabCounts.all }}</span>
        </div>
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'created' }"
          @click="activeTab = 'created'"
        >
          <span>我创建的</span>
          <span class="tab-count">{{ tabCounts.created }}</span>
        </div>
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'favorited' }"
          @click="activeTab = 'favorited'"
        >
          <span>我收藏的</span>
          <span class="tab-count">{{ tabCounts.favorited }}</span>
        </div>
      </div>
      
      <div class="filter-search">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索名称"
          prefix-icon="Search"
          clearable
          style="width: 240px"
        />
      </div>
      
      <div class="filter-selects">
        <el-select v-model="filterProject" placeholder="项目" clearable style="width: 160px">
          <el-option
            v-for="project in projectList"
            :key="project.id"
            :label="project.name"
            :value="project.id"
          />
        </el-select>
        
        <el-select v-model="filterEnvironment" placeholder="环境" clearable style="width: 160px">
          <el-option label="开发环境" value="development" />
          <el-option label="测试环境" value="testing" />
          <el-option label="生产环境" value="production" />
        </el-select>
        
        <el-button :icon="Refresh" circle @click="fetchPipelines" />
      </div>
    </div>
    
    <div class="pipeline-table">
      <el-table
        :data="pipelineList"
        style="width: 100%"
        :default-sort="{ prop: 'updated_at', order: 'descending' }"
      >
        <el-table-column prop="name" label="流水线名称" min-width="200">
          <template #default="{ row }">
            <div class="pipeline-name">
              <span class="name-icon">{{ row.name.charAt(0).toUpperCase() }}</span>
              <router-link :to="`/pipeline/${row.id}`" class="name-link">
                {{ row.name }}
              </router-link>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="project_name" label="所属项目" width="120" align="center">
          <template #default="{ row }">
            <span>{{ row.project_name || '-' }}</span>
          </template>
        </el-table-column>
        
        <el-table-column prop="environment_text" label="环境" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="getEnvironmentTagType(row.environment)" size="small">
              {{ row.environment_text || row.environment }}
            </el-tag>
          </template>
        </el-table-column>
        
        <el-table-column prop="last_editor" label="编辑人员" width="120" align="center">
          <template #default="{ row }">
            <div class="user-info">
              <span class="user-avatar">{{ row.last_editor?.charAt(0) || row.owner?.username?.charAt(0) || '?' }}</span>
              <span>{{ row.last_editor || row.owner?.username || '-' }}</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="updated_at" label="编辑时间" width="160" sortable>
          <template #default="{ row }">
            {{ formatDateTime(row.updated_at) }}
          </template>
        </el-table-column>
        
        <el-table-column prop="last_build" label="最近构建" width="200">
          <template #default="{ row }">
            <div v-if="row.last_build" class="last-build">
              <span class="build-time">{{ formatRelativeTime(row.last_build.created_at) }}</span>
              <span class="build-number">#{{ row.last_build.build_number }}</span>
              <el-icon v-if="row.last_build.status === 'success'" class="status-icon success"><CircleCheck /></el-icon>
              <el-icon v-else-if="row.last_build.status === 'running'" class="status-icon running"><Loading /></el-icon>
              <el-icon v-else-if="row.last_build.status === 'failed'" class="status-icon failed"><CircleClose /></el-icon>
              <el-icon v-else-if="row.last_build.status === 'pending'" class="status-icon pending"><Clock /></el-icon>
              <el-icon v-else class="status-icon warning"><Warning /></el-icon>
            </div>
            <span v-else class="no-build">无构建</span>
          </template>
        </el-table-column>
        
        <el-table-column prop="latest_runner" label="构建人员" width="120" align="center">
          <template #default="{ row }">
            <div v-if="row.last_build" class="user-info">
              <span class="user-avatar">{{ row.latest_runner?.charAt(0) || row.last_build?.trigger_user?.charAt(0) || '?' }}</span>
              <span>{{ row.latest_runner || row.last_build?.trigger_user || '-' }}</span>
            </div>
            <span v-else class="no-build">-</span>
          </template>
        </el-table-column>
        
        <el-table-column prop="owner" label="创建人" width="120" align="center">
          <template #default="{ row }">
            <div class="owner-info">
              <span class="owner-avatar">{{ row.owner?.username?.charAt(0) || '?' }}</span>
              <span>{{ row.owner?.username || '-' }}</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="created_at" label="创建时间" width="160" sortable>
          <template #default="{ row }">
            {{ formatDateTime(row.created_at) }}
          </template>
        </el-table-column>
        
        <el-table-column label="最新构建时间" width="160" align="center">
          <template #default="{ row }">
            <span v-if="row.last_build">{{ formatDateTime(row.last_build.created_at) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-tooltip content="运行流水线" placement="top">
                <el-icon class="action-icon" @click="handleRun(row)">
                  <VideoPlay />
                </el-icon>
              </el-tooltip>
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
              <el-tooltip content="删除" placement="top">
                <el-icon class="action-icon danger" @click="handleDelete(row)">
                  <Delete />
                </el-icon>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>
    
    <!-- 新建流水线对话框 -->
    <el-dialog
      v-model="dialogVisible"
      title="新建流水线"
      width="500px"
      :close-on-click-modal="false"
    >
      <el-form
        ref="pipelineFormRef"
        :model="pipelineForm"
        :rules="pipelineRules"
        label-width="100px"
      >
        <el-form-item label="流水线名称" prop="name">
          <el-input v-model="pipelineForm.name" placeholder="请输入流水线名称" />
        </el-form-item>
        
        <el-form-item label="所属项目" prop="project_id">
          <el-select v-model="pipelineForm.project_id" placeholder="请选择项目" style="width: 100%">
            <el-option
              v-for="project in projectList"
              :key="project.id"
              :label="project.name"
              :value="project.id"
            />
          </el-select>
        </el-form-item>
        
        <el-form-item label="环境" prop="environment">
          <el-select v-model="pipelineForm.environment" placeholder="请选择环境" style="width: 100%">
            <el-option label="开发环境" value="development" />
            <el-option label="测试环境" value="testing" />
            <el-option label="生产环境" value="production" />
          </el-select>
        </el-form-item>
        
        <el-form-item label="描述">
          <el-input
            v-model="pipelineForm.description"
            type="textarea"
            :rows="3"
            placeholder="请输入描述信息"
          />
        </el-form-item>
      </el-form>
      
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitLoading" @click="handleSubmit">
          创建
        </el-button>
      </template>
    </el-dialog>
    
    <!-- 删除确认对话框 -->
    <el-dialog
      v-model="deleteDialogVisible"
      title="确认删除"
      width="400px"
      :close-on-click-modal="false"
    >
      <div class="delete-warning">
        <el-icon color="#E6A23C" size="24"><Warning /></el-icon>
        <p>确定要删除流水线 <strong>{{ deleteForm.name }}</strong> 吗？</p>
        <p class="delete-tip">此操作不可恢复，请输入流水线名称以确认。</p>
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
import { ref, onMounted, watch, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus,
  Search,
  Star,
  StarFilled,
  Refresh,
  CircleCheck,
  CircleClose,
  Loading,
  VideoPlay,
  Delete,
  Warning,
  Clock
} from '@element-plus/icons-vue'
import { getPipelineList, createPipeline, runPipeline, toggleFavorite as apiToggleFavorite, deletePipeline } from '@/api/pipeline'
import { getProjectList } from '@/api/project'

const activeTab = ref('all')
const searchKeyword = ref('')
const filterProject = ref('')
const filterEnvironment = ref('')
const total = ref(0)
const tabCounts = ref({
  all: 0,
  created: 0,
  favorited: 0
})

// 对话框相关
const dialogVisible = ref(false)
const submitLoading = ref(false)
const pipelineFormRef = ref(null)
const pipelineForm = reactive({
  name: '',
  project_id: '',
  environment: 'development',
  description: ''
})

// 删除对话框相关
const deleteDialogVisible = ref(false)
const deleteLoading = ref(false)
const deleteForm = reactive({
  id: null,
  name: '',
  confirmName: ''
})

const pipelineRules = {
  name: [
    { required: true, message: '请输入流水线名称', trigger: 'blur' },
    { min: 2, max: 64, message: '名称长度在 2-64 个字符', trigger: 'blur' }
  ],
  project_id: [
    { required: true, message: '请选择所属项目', trigger: 'change' }
  ],
  environment: [
    { required: true, message: '请选择环境', trigger: 'change' }
  ]
}

const pipelineList = ref([])
const projectList = ref([])

// 加载项目列表
const fetchProjects = async () => {
  try {
    const response = await getProjectList({})
    projectList.value = response.data.list || []
  } catch (error) {
    console.error('获取项目列表失败:', error)
    ElMessage.error('获取项目列表失败')
    projectList.value = []
  }
}

const fetchPipelines = async () => {
  try {
    const params = {
      page: 1,
      page_size: 100,
      tab: activeTab.value,
      keyword: searchKeyword.value,
      project_id: filterProject.value || undefined,
      environment: filterEnvironment.value || undefined
    }
    
    const response = await getPipelineList(params)
    pipelineList.value = response.data.list || []
    total.value = response.data.total || 0
    // 更新tab数量统计
    if (response.data.tab_counts) {
      tabCounts.value = response.data.tab_counts
    }
  } catch (error) {
    console.error('获取流水线列表失败:', error)
    ElMessage.error('获取流水线列表失败')
    pipelineList.value = []
    total.value = 0
  }
}

onMounted(() => {
  fetchProjects()
  fetchPipelines()
})

watch([activeTab, searchKeyword, filterProject, filterEnvironment], () => {
  fetchPipelines()
}, { deep: true })

const formatDate = (date) => {
  if (!date) return '-'
  // 处理 Unix 时间戳（秒）
  const timestamp = typeof date === 'number' ? date * 1000 : date
  return new Date(timestamp).toLocaleDateString('zh-CN')
}

const formatDateTime = (date) => {
  if (!date) return '-'
  // 处理 Unix 时间戳（秒）
  const timestamp = typeof date === 'number' ? date * 1000 : date
  const d = new Date(timestamp)
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hours = String(d.getHours()).padStart(2, '0')
  const minutes = String(d.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day} ${hours}:${minutes}`
}

const formatRelativeTime = (date) => {
  if (!date) return ''
  const now = new Date()
  const diff = now - new Date(date)
  const hours = Math.floor(diff / (1000 * 60 * 60))
  const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60))
  
  if (hours > 0) {
    return `${hours} 小时 ${minutes} 分前`
  } else if (minutes > 0) {
    return `${minutes} 分钟前`
  }
  return '刚刚'
}

const getEnvironmentTagType = (env) => {
  switch (env) {
    case 'development':
      return 'success'
    case 'testing':
      return 'warning'
    case 'production':
      return 'danger'
    default:
      return 'info'
  }
}

// 新建流水线
const handleCreate = () => {
  pipelineForm.name = ''
  pipelineForm.project_id = ''
  pipelineForm.environment = 'development'
  pipelineForm.description = ''
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!pipelineFormRef.value) return
  
  await pipelineFormRef.value.validate(async (valid) => {
    if (valid) {
      submitLoading.value = true
      try {
        await createPipeline(pipelineForm)
        ElMessage.success('创建成功')
        dialogVisible.value = false
        fetchPipelines()
      } catch (error) {
        console.error('创建流水线失败:', error)
        ElMessage.error('创建失败')
      } finally {
        submitLoading.value = false
      }
    }
  })
}

// 运行流水线
const handleRun = async (row) => {
  try {
    await ElMessageBox.confirm(`确定要运行流水线 "${row.name}" 吗？`, '确认运行', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'info'
    })
    
    await runPipeline(row.id)
    ElMessage.success('已开始运行')
    fetchPipelines()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('运行流水线失败:', error)
      ElMessage.error('运行失败')
    }
  }
}

// 收藏/取消收藏
const handleFavorite = async (row) => {
  try {
    await apiToggleFavorite(row.id)
    row.is_favorited = !row.is_favorited
    ElMessage.success(row.is_favorited ? '收藏成功' : '取消收藏')
  } catch (error) {
    console.error('操作失败:', error)
    ElMessage.error('操作失败')
  }
}

// 删除流水线
const handleDelete = (row) => {
  deleteForm.id = row.id
  deleteForm.name = row.name
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
    await deletePipeline(deleteForm.id)
    ElMessage.success('删除成功')
    deleteDialogVisible.value = false
    fetchPipelines()
  } catch (error) {
    console.error('删除流水线失败:', error)
    ElMessage.error('删除失败')
  } finally {
    deleteLoading.value = false
  }
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.pipeline-container {
  .pipeline-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 28px;
    
    .page-title {
      font-family: $font-family-display;
      font-size: 28px;
      font-weight: 700;
      color: $text-primary;
      letter-spacing: -0.02em;
    }
    
    :deep(.el-button--primary) {
      height: 44px;
      padding: 0 24px;
      border-radius: $radius-md;
      font-weight: 600;
      background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
      border: none;
      box-shadow: $shadow-md;
      
      &:hover {
        transform: translateY(-2px);
        box-shadow: $shadow-lg;
      }
      
      &:active {
        transform: translateY(0);
        box-shadow: $shadow-inset;
      }
    }
  }
  
  // ============================================
  // Modern Filter Bar
  // ============================================
  .pipeline-filters {
    display: flex;
    align-items: center;
    gap: 20px;
    margin-bottom: 24px;
    padding: 14px 20px;
    background: $bg-card;
    border-radius: $radius-xl;
    box-shadow: $shadow-sm;
    
    .filter-tabs {
      display: flex;
      align-items: center;
      gap: 6px;
      
      .tab-item {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 8px 16px;
        color: $text-secondary;
        cursor: pointer;
        border-radius: $radius-full;
        transition: all $transition-base;
        font-weight: 500;
        font-size: 14px;
        
        &:hover {
          background: rgba($primary-color, 0.06);
          color: $primary-color;
        }
        
        &.active {
          color: $primary-color;
          background: rgba($primary-color, 0.1);
          box-shadow: inset 0 0 0 1px rgba($primary-color, 0.15);
        }
        
        .tab-count {
          font-size: 12px;
          color: $text-muted;
          background: $bg-secondary;
          padding: 2px 8px;
          border-radius: $radius-full;
        }
      }
      
      .tab-divider {
        width: 1px;
        height: 20px;
        background: $border-color;
        margin: 0 8px;
      }
    }
    
    .filter-search {
      flex: 1;
      
      :deep(.el-input__wrapper) {
        background: $bg-secondary;
        border-radius: $radius-md;
        box-shadow: $shadow-inset;
        border: 1px solid $border-color-light;
        
        &:hover, &.is-focus {
          border-color: rgba($primary-color, 0.4);
        }
      }
    }
    
    .filter-selects {
      display: flex;
      align-items: center;
      gap: 12px;
      
      :deep(.el-select .el-input__wrapper) {
        background: $bg-secondary;
        border-radius: $radius-md;
        box-shadow: $shadow-inset;
        border: 1px solid $border-color-light;
      }
      
      :deep(.el-button) {
        background: $bg-secondary;
        border: 1px solid $border-color-light;
        border-radius: $radius-md;
        box-shadow: $shadow-sm;
        
        &:hover {
          background: $bg-card;
          box-shadow: $shadow-md;
          color: $primary-color;
        }
        
        &:active {
          box-shadow: $shadow-inset;
        }
      }
    }
  }
  
  // ============================================
  // Modern Pipeline Table
  // ============================================
  .pipeline-table {
    background: $bg-card;
    border-radius: $radius-xl;
    overflow: hidden;
    box-shadow: $shadow-md;
    
    :deep(.el-table) {
      background: transparent;
      
      th.el-table__cell {
        background: $bg-secondary;
        color: $text-secondary;
        font-weight: 600;
        font-size: 13px;
        border-bottom: 1px solid $border-color;
        padding: 16px 0;
      }
      
      td.el-table__cell {
        color: $text-primary;
        border-bottom: 1px solid $border-color-light;
        padding: 16px 0;
      }
      
      .el-table__row:hover > td.el-table__cell {
        background: rgba($primary-color, 0.04);
      }
    }
    
    .pipeline-name {
      display: flex;
      align-items: center;
      gap: 12px;
      
      .name-icon {
        width: 32px;
        height: 32px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
        color: white;
        font-size: 13px;
        font-weight: 600;
        border-radius: $radius-md;
        box-shadow: 0 2px 8px rgba($primary-color, 0.25);
      }
      
      .name-link {
        color: $primary-color;
        text-decoration: none;
        font-weight: 600;
        
        &:hover {
          text-decoration: underline;
        }
      }
    }
    
    // Status tags
    :deep(.el-tag) {
      border-radius: $radius-full;
      padding: 4px 12px;
      font-weight: 500;
      border: none;
      
      &.el-tag--success {
        background: $success-light;
        color: darken($success-color, 20%);
      }
      
      &.el-tag--warning {
        background: $warning-light;
        color: darken($warning-color, 20%);
      }
      
      &.el-tag--danger {
        background: $danger-light;
        color: darken($danger-color, 20%);
      }
    }
    
    .last-build {
      display: flex;
      align-items: center;
      gap: 6px;
      
      .build-time {
        color: $text-secondary;
        font-size: 13px;
      }
      
      .build-number {
        color: $text-primary;
        font-weight: 600;
        font-family: $font-family-mono;
      }
      
      .status-icon {
        margin-left: 4px;
        
        &.success {
          color: $success-color;
        }
        
        &.running {
          color: $warning-color;
        }
        
        &.failed {
          color: $danger-color;
        }
      }
    }
    
    .no-build {
      color: $text-muted;
    }
    
    .user-info, .owner-info {
      display: flex;
      align-items: center;
      gap: 10px;
      
      .user-avatar, .owner-avatar {
        width: 28px;
        height: 28px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
        color: white;
        font-size: 11px;
        font-weight: 600;
        border-radius: 50%;
        box-shadow: 0 2px 6px rgba($primary-color, 0.25);
      }
      
      .owner-avatar {
        background: linear-gradient(135deg, $text-tertiary 0%, $text-secondary 100%);
      }
    }
    
    .table-actions {
      display: flex;
      gap: 8px;
      
      .action-icon {
        width: 32px;
        height: 32px;
        display: flex;
        align-items: center;
        justify-content: center;
        font-size: 16px;
        color: $text-secondary;
        cursor: pointer;
        transition: all $transition-fast;
        border-radius: $radius-md;
        
        &:hover {
          color: $primary-color;
          background: rgba($primary-color, 0.08);
        }
        
        &.active {
          color: $warning-color;
          background: $warning-light;
        }
        
        &.danger:hover {
          color: $danger-color;
          background: $danger-light;
        }
      }
    }
  }
}

// ============================================
// Modern Dialog Styles
// ============================================
:deep(.el-dialog) {
  border-radius: $radius-xl;
  box-shadow: $shadow-xl;
  overflow: hidden;
  
  .el-dialog__header {
    background: $bg-secondary;
    padding: 20px 24px;
    margin: 0;
    border-bottom: 1px solid $border-color;
    
    .el-dialog__title {
      font-family: $font-family-display;
      font-size: 18px;
      font-weight: 600;
      color: $text-primary;
    }
  }
  
  .el-dialog__body {
    padding: 24px;
  }
  
  .el-dialog__footer {
    padding: 16px 24px;
    border-top: 1px solid $border-color;
    background: $bg-secondary;
  }
  
  // Form inputs
  .el-input__wrapper {
    background: $bg-secondary;
    border-radius: $radius-md;
    box-shadow: $shadow-inset;
    border: 1px solid $border-color-light;
    
    &:hover, &.is-focus {
      border-color: rgba($primary-color, 0.4);
    }
  }
}

.delete-warning {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 0;
  
  p {
    margin: 16px 0 8px;
    color: $text-primary;
    font-size: 15px;
    font-weight: 500;
  }
  
  .delete-tip {
    color: $text-muted;
    font-size: 13px;
  }
}
</style>
