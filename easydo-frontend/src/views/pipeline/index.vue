<template>
  <div class="pipeline-container">
    <div class="pipeline-header">
      <div>
        <h1 class="page-title">流水线</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
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
              <el-icon v-if="['success', 'execute_success'].includes(row.last_build.status)" class="status-icon success"><CircleCheck /></el-icon>
              <el-icon v-else-if="row.last_build.status === 'running'" class="status-icon running"><Loading /></el-icon>
              <el-icon v-else-if="['failed', 'execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired'].includes(row.last_build.status)" class="status-icon failed"><CircleClose /></el-icon>
              <el-icon v-else-if="['queued', 'assigned', 'dispatching', 'pulling', 'acked', 'cancelled'].includes(row.last_build.status)" class="status-icon pending"><Clock /></el-icon>
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
      width="640px"
      :close-on-click-modal="false"
      :append-to-body="true"
      top="100px"
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
      :append-to-body="true"
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
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
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
    
    const res = await runPipeline(row.id)
    const runStatus = res?.data?.status
    if (runStatus === 'queued') {
      ElMessage.success('已进入排队')
    } else {
      ElMessage.success('已开始运行')
    }
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
  animation: float-up 0.45s ease both;

  .pipeline-header {
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

  .pipeline-filters {
    display: flex;
    align-items: center;
    gap: 16px;
    margin-bottom: 18px;
    padding: 14px 16px;
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

    .filter-selects {
      display: flex;
      align-items: center;
      gap: 10px;

      :deep(.el-button) {
        border-radius: 12px;
      }
    }
  }

  .pipeline-table {
    border-radius: $radius-xl;
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    box-shadow: var(--shadow-md);
    backdrop-filter: $blur-md;
    -webkit-backdrop-filter: $blur-md;
    overflow: hidden;

    :deep(.el-table) {
      background: transparent;

      th.el-table__cell {
        background: rgba(255, 255, 255, 0.45);
        border-bottom: 1px solid var(--border-color-light);
        height: 44px;
      }

      td.el-table__cell {
        height: 56px;
      }
    }

    .pipeline-name {
      display: flex;
      align-items: center;
      gap: 10px;

      .name-icon {
        width: 34px;
        height: 34px;
        border-radius: 12px;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        color: #fff;
        font-size: 13px;
        font-weight: 700;
        background: linear-gradient(140deg, $primary-color 0%, $primary-hover 100%);
        box-shadow: 0 10px 22px rgba($primary-color, 0.28);
      }

      .name-link {
        color: var(--text-primary);
        font-weight: 650;
        transition: color $transition-fast;

        &:hover {
          color: var(--primary-color);
        }
      }
    }

    .last-build {
      display: flex;
      align-items: center;
      gap: 6px;

      .build-time {
        color: var(--text-secondary);
        font-size: 12px;
      }

      .build-number {
        color: var(--text-primary);
        font-family: $font-family-mono;
        font-weight: 650;
      }

      .status-icon {
        margin-left: 2px;

        &.success { color: $success-color; }
        &.running { color: $warning-color; }
        &.failed { color: $danger-color; }
        &.pending { color: $info-color; }
      }
    }

    .no-build {
      color: var(--text-muted);
      font-size: 12px;
    }

    .user-info,
    .owner-info {
      display: flex;
      align-items: center;
      gap: 8px;

      .user-avatar,
      .owner-avatar {
        width: 28px;
        height: 28px;
        border-radius: 50%;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        color: #fff;
        font-size: 11px;
        font-weight: 700;
        box-shadow: 0 8px 18px rgba(10, 32, 66, 0.2);
      }

      .user-avatar {
        background: linear-gradient(140deg, $primary-color, $primary-hover);
      }

      .owner-avatar {
        background: linear-gradient(140deg, #6d85a8, #8da5c5);
      }
    }

    .table-actions {
      display: flex;
      align-items: center;
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
          border-color: rgba($primary-color, 0.26);
          transform: translateY(-1px);
        }

        &.active {
          color: $warning-color;
          border-color: rgba($warning-color, 0.34);
          background: $warning-light;
        }

        &.danger:hover {
          color: $danger-color;
          border-color: rgba($danger-color, 0.34);
          background: $danger-light;
        }
      }
    }
  }
}

:deep(.el-dialog) {
  border-radius: $radius-xl;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);

  .el-dialog__header {
    border-bottom: 1px solid var(--border-color-light);
  }

  .el-dialog__footer {
    border-top: 1px solid var(--border-color-light);
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

@media (max-width: 1200px) {
  .pipeline-container {
    .pipeline-filters {
      flex-wrap: wrap;

      .filter-search {
        min-width: 220px;
      }
    }
  }
}

@media (max-width: 768px) {
  .pipeline-container {
    .pipeline-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 10px;

      .page-title {
        font-size: 27px;
      }
    }

    .pipeline-filters {
      flex-direction: column;
      align-items: stretch;

      .filter-tabs {
        overflow: auto;
        padding-bottom: 4px;
      }

      .filter-selects {
        flex-wrap: wrap;
      }
    }
  }
}
</style>
