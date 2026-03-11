<template>
  <div class="deploy-container">
    <div class="deploy-header">
      <div>
        <h1 class="page-title">发布</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
      <el-button type="primary" @click="handleCreate">
        <el-icon><Plus /></el-icon>
        新建发布
      </el-button>
    </div>
    
    <div class="deploy-filters">
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
          <span class="tab-count">{{ total }}</span>
        </div>
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'created' }"
          @click="activeTab = 'created'"
        >
          <span>我创建的</span>
          <span class="tab-count">0</span>
        </div>
        <div 
          class="tab-item" 
          :class="{ active: activeTab === 'favorited' }"
          @click="activeTab = 'favorited'"
        >
          <span>我收藏的</span>
          <span class="tab-count">0</span>
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
      </div>
    </div>
    
    <div class="deploy-table">
      <el-table
        :data="deployList"
        style="width: 100%"
        :default-sort="{ prop: 'created_at', order: 'descending' }"
      >
        <el-table-column prop="name" label="发布名称" min-width="200">
          <template #default="{ row }">
            <div class="deploy-name">
              <span class="name-icon">{{ row.name.charAt(0).toUpperCase() }}</span>
              <span class="name-text">{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="version" label="版本" width="120">
          <template #default="{ row }">
            <span class="version-tag">v{{ row.version }}</span>
          </template>
        </el-table-column>
        
        <el-table-column prop="environment" label="环境" width="120">
          <template #default="{ row }">
            <el-tag :type="getEnvironmentType(row.environment)" size="small">
              {{ getEnvironmentName(row.environment) }}
            </el-tag>
          </template>
        </el-table-column>
        
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <div class="status-cell">
              <el-icon v-if="row.status === 'success'" class="status-icon success"><CircleCheck /></el-icon>
              <el-icon v-else-if="row.status === 'running'" class="status-icon running"><Loading /></el-icon>
              <el-icon v-else class="status-icon failed"><CircleClose /></el-icon>
              <span>{{ getStatusName(row.status) }}</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="deployer" label="发布人" width="150">
          <template #default="{ row }">
            <div class="deployer-info">
              <span class="deployer-avatar">{{ row.deployer.charAt(0) }}</span>
              <span>{{ row.deployer }}</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="created_at" label="创建时间" width="160" sortable>
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-tooltip content="查看详情" placement="top">
                <el-icon class="action-icon" @click="handleView(row)">
                  <View />
                </el-icon>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useUserStore } from '@/stores/user'
import { 
  Plus, 
  Search, 
  CircleCheck, 
  CircleClose, 
  Loading,
  View
} from '@element-plus/icons-vue'

const userStore = useUserStore()
const activeTab = ref('all')
const searchKeyword = ref('')
const filterProject = ref('')
const filterEnvironment = ref('')
const total = ref(3)

const projectList = ref([
  { id: 1, name: 'frontend' },
  { id: 2, name: 'backend' }
])

const deployList = ref([
  {
    id: 1,
    name: 'frontend-prod',
    version: '1.0.0',
    environment: 'production',
    status: 'success',
    deployer: '管理员',
    created_at: '2026-01-22'
  },
  {
    id: 2,
    name: 'backend-prod',
    version: '2.0.0',
    environment: 'production',
    status: 'running',
    deployer: '管理员',
    created_at: '2026-01-21'
  },
  {
    id: 3,
    name: 'frontend-test',
    version: '1.0.1',
    environment: 'testing',
    status: 'failed',
    deployer: '管理员',
    created_at: '2026-01-20'
  }
])

const fetchDeploys = async () => {
  total.value = deployList.value.length
}

onMounted(() => {
  fetchDeploys()
})

const formatDate = (date) => {
  if (!date) return '-'
  // 处理 Unix 时间戳（秒）
  const timestamp = typeof date === 'number' ? date * 1000 : date
  return new Date(timestamp).toLocaleDateString('zh-CN')
}

const getEnvironmentType = (env) => {
  const types = {
    development: 'info',
    testing: 'warning',
    production: 'danger'
  }
  return types[env] || 'info'
}

const getEnvironmentName = (env) => {
  const names = {
    development: '开发环境',
    testing: '测试环境',
    production: '生产环境'
  }
  return names[env] || env
}

const getStatusName = (status) => {
  const names = {
    success: '成功',
    running: '部署中',
    failed: '失败'
  }
  return names[status] || status
}

const handleCreate = () => {
  console.log('创建发布')
}

const handleView = (row) => {
  console.log('查看发布:', row.id)
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.deploy-container {
  animation: float-up 0.45s ease both;

  .deploy-header {
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

  .deploy-filters {
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
    }
  }

  .deploy-table {
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
        background: var(--bg-secondary);
        border-bottom: 1px solid var(--border-color-light);
        height: 44px;
      }

      td.el-table__cell {
        height: 56px;
      }
    }

    .deploy-name {
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

      .name-text {
        color: var(--text-primary);
        font-weight: 650;
      }
    }

    .version-tag {
      display: inline-flex;
      align-items: center;
      height: 24px;
      padding: 0 10px;
      border-radius: $radius-full;
      background: var(--bg-elevated);
      border: 1px solid var(--border-color-light);
      color: var(--text-secondary);
      font-size: 12px;
      font-family: $font-family-mono;
      font-weight: 600;
    }

    .status-cell {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      color: var(--text-secondary);

      .status-icon {
        &.success { color: $success-color; }
        &.running { color: $warning-color; }
        &.failed { color: $danger-color; }
      }
    }

    .deployer-info {
      display: inline-flex;
      align-items: center;
      gap: 8px;

      .deployer-avatar {
        width: 28px;
        height: 28px;
        border-radius: 50%;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        color: #fff;
        font-size: 11px;
        font-weight: 700;
        background: linear-gradient(140deg, var(--primary-color), var(--primary-hover));
      }
    }

    .table-actions {
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
        }
      }
    }
  }
}

@media (max-width: 1200px) {
  .deploy-container .deploy-filters {
    flex-wrap: wrap;
  }
}

@media (max-width: 768px) {
  .deploy-container {
    .deploy-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 10px;

      .page-title {
        font-size: 27px;
      }
    }

    .deploy-filters {
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
