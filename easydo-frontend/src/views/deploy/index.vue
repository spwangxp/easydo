<template>
  <div class="deploy-container">
    <div class="deploy-header">
      <h1 class="page-title">发布</h1>
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
import { 
  Plus, 
  Search, 
  CircleCheck, 
  CircleClose, 
  Loading,
  View
} from '@element-plus/icons-vue'

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
.deploy-container {
  .deploy-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
    
    .page-title {
      font-size: 24px;
      font-weight: 600;
      color: #303133;
    }
  }
  
  .deploy-filters {
    display: flex;
    align-items: center;
    gap: 20px;
    margin-bottom: 20px;
    padding: 12px 16px;
    background: white;
    border-radius: 8px;
    
    .filter-tabs {
      display: flex;
      align-items: center;
      gap: 4px;
      
      .tab-item {
        display: flex;
        align-items: center;
        gap: 4px;
        padding: 6px 12px;
        color: #606266;
        cursor: pointer;
        border-radius: 4px;
        transition: all 0.3s;
        
        &:hover {
          background: #f5f7fa;
        }
        
        &.active {
          color: #409EFF;
          background: #ecf5ff;
        }
        
        .tab-count {
          font-size: 12px;
          color: #909399;
        }
      }
      
      .tab-divider {
        width: 1px;
        height: 16px;
        background: #dcdfe6;
        margin: 0 8px;
      }
    }
    
    .filter-search {
      flex: 1;
    }
    
    .filter-selects {
      display: flex;
      gap: 12px;
    }
  }
  
  .deploy-table {
    background: white;
    border-radius: 8px;
    overflow: hidden;
    
    .deploy-name {
      display: flex;
      align-items: center;
      gap: 12px;
      
      .name-icon {
        width: 24px;
        height: 24px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: #409EFF;
        color: white;
        font-size: 12px;
        font-weight: 500;
        border-radius: 4px;
      }
      
      .name-text {
        color: #303133;
        font-weight: 500;
      }
    }
    
    .version-tag {
      font-family: monospace;
      color: #303133;
      background: #f4f4f5;
      padding: 2px 8px;
      border-radius: 4px;
      font-size: 12px;
    }
    
    .status-cell {
      display: flex;
      align-items: center;
      gap: 4px;
      
      .status-icon {
        &.success { color: #67C23A; }
        &.running { color: #E6A23C; }
        &.failed { color: #F56C6C; }
      }
    }
    
    .deployer-info {
      display: flex;
      align-items: center;
      gap: 8px;
      
      .deployer-avatar {
        width: 24px;
        height: 24px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: #909399;
        color: white;
        font-size: 12px;
        border-radius: 50%;
      }
    }
    
    .table-actions {
      display: flex;
      gap: 12px;
      
      .action-icon {
        font-size: 16px;
        color: #606266;
        cursor: pointer;
        
        &:hover {
          color: #409EFF;
        }
      }
    }
  }
}
</style>
