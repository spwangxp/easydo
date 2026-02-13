<template>
  <div class="dashboard-container">
    <div class="dashboard-header">
      <h1 class="page-title">工作台</h1>
      <div class="header-actions">
        <el-button type="primary" @click="handleCreatePipeline">
          <el-icon><Plus /></el-icon>
          新建流水线
        </el-button>
      </div>
    </div>
    
    <div class="stats-overview" v-loading="loading">
      <div class="stat-card">
        <div class="stat-icon blue">
          <el-icon :size="24"><Connection /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.pipelineCount }}</div>
          <div class="stat-label">流水线</div>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon green">
          <el-icon :size="24"><Box /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.projectCount }}</div>
          <div class="stat-label">项目</div>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon orange">
          <el-icon :size="24"><Clock /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.todayRuns }}</div>
          <div class="stat-label">今日运行</div>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon success">
          <el-icon :size="24"><CircleCheck /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.successRate }}%</div>
          <div class="stat-label">成功率</div>
        </div>
      </div>
    </div>
    
    <div class="dashboard-content">
      <div class="content-main">
        <div class="section-card">
          <div class="section-header">
            <h3 class="section-title">最近运行</h3>
            <el-button type="text" @click="viewAllPipelines">查看全部</el-button>
          </div>
          
          <el-table :data="recentRuns" style="width: 100%" v-loading="loading" empty-text="暂无运行记录">
            <el-table-column prop="pipeline" label="流水线" min-width="150">
              <template #default="{ row }">
                <div class="pipeline-cell">
                  <span class="pipeline-icon">{{ row.pipeline.charAt(0).toUpperCase() }}</span>
                  <span>{{ row.pipeline }}</span>
                </div>
              </template>
            </el-table-column>
            
            <el-table-column prop="build_id" label="构建号" width="100">
              <template #default="{ row }">
                <span class="build-id">#{{ row.build_id }}</span>
              </template>
            </el-table-column>
            
            <el-table-column prop="status" label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="getStatusType(row.status)" size="small">
                  {{ getStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            
            <el-table-column prop="duration" label="耗时" width="100">
              <template #default="{ row }">
                {{ row.duration }}
              </template>
            </el-table-column>
            
            <el-table-column prop="time" label="时间" width="180">
              <template #default="{ row }">
                {{ row.time }}
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
      
      <div class="content-sidebar">
        <div class="section-card">
          <div class="section-header">
            <h3 class="section-title">快捷操作</h3>
          </div>
          
          <div class="quick-actions">
            <div class="action-item" @click="handleCreatePipeline">
              <div class="action-icon blue">
                <el-icon :size="20"><Plus /></el-icon>
              </div>
              <span>新建流水线</span>
            </div>
            
            <div class="action-item" @click="handleCreateProject">
              <div class="action-icon green">
                <el-icon :size="20"><FolderAdd /></el-icon>
              </div>
              <span>新建项目</span>
            </div>
            
            <div class="action-item" @click="handleManageDeploys">
              <div class="action-icon orange">
                <el-icon :size="20"><Promotion /></el-icon>
              </div>
              <span>发布管理</span>
            </div>
            
            <div class="action-item" @click="handleGoSettings">
              <div class="action-icon purple">
                <el-icon :size="20"><Setting /></el-icon>
              </div>
              <span>系统设置</span>
            </div>
          </div>
        </div>
        
        <div class="section-card">
          <div class="section-header">
            <h3 class="section-title">公告</h3>
          </div>
          
          <div class="announcements">
            <div class="announcement-item">
              <el-tag type="info" size="small">系统</el-tag>
              <p class="announcement-title">系统维护通知</p>
              <p class="announcement-time">1 天前</p>
            </div>
            <div class="announcement-item">
              <el-tag type="success" size="small">功能</el-tag>
              <p class="announcement-title">新增流水线模板功能</p>
              <p class="announcement-time">3 天前</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Plus,
  Connection,
  Box,
  Clock,
  CircleCheck,
  FolderAdd,
  Promotion,
  Setting
} from '@element-plus/icons-vue'
import { getPipelineList, getPipelineHistory } from '@/api/pipeline'
import { getProjectList } from '@/api/project'

const router = useRouter()
const loading = ref(false)

// 统计数据
const stats = ref({
  pipelineCount: 0,
  projectCount: 0,
  todayRuns: 0,
  successRate: 0
})

// 最近运行记录
const recentRuns = ref([])

// 获取今日日期范围
const getTodayRange = () => {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const tomorrow = new Date(today)
  tomorrow.setDate(tomorrow.getDate() + 1)
  return { start: today.toISOString(), end: tomorrow.toISOString() }
}

// 格式化持续时间
const formatDuration = (seconds) => {
  if (!seconds || seconds <= 0) return '-'
  // 如果值太大（超过1年），可能是毫秒，需要转换
  if (seconds > 31536000) {
    seconds = Math.floor(seconds / 1000)
  }
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.floor(seconds % 60)
  if (minutes > 0) {
    return `${minutes}m ${remainingSeconds}s`
  }
  return `${seconds}s`
}

// 格式化时间
const formatTime = (dateString) => {
  if (!dateString) return '-'
  // 处理 Unix 时间戳（秒）
  const timestamp = typeof dateString === 'number' ? dateString * 1000 : dateString
  const date = new Date(timestamp)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).replace(/\//g, '-')
}

// 获取工作台统计数据
const fetchDashboardData = async () => {
  loading.value = true
  try {
    // 并行获取流水线和项目列表
    const [pipelineRes, projectRes] = await Promise.all([
      getPipelineList({ page_size: 1000 }),
      getProjectList({ page_size: 1000 })
    ])

    // 计算流水线和项目数量
    const pipelines = pipelineRes.data?.list || pipelineRes.list || []
    const projects = projectRes.data?.list || projectRes.list || []
    stats.value.pipelineCount = pipelines.length
    stats.value.projectCount = projects.length

    // 获取所有流水线的运行记录
    const allRuns = []
    const todayRuns = []
    const { start: todayStart, end: todayEnd } = getTodayRange()
    let successCount = 0

    for (const pipeline of pipelines) {
      try {
        const historyRes = await getPipelineHistory(pipeline.id, { page_size: 10 })
        const runs = historyRes.data?.list || historyRes.list || []

        for (const run of runs) {
          const runData = {
            pipeline: pipeline.name,
            pipeline_id: pipeline.id,
            build_id: run.build_number || run.id,
            status: run.status || 'unknown',
            duration: formatDuration(run.duration),
            time: formatTime(run.start_time || run.created_at),
            rawTime: run.start_time || run.created_at
          }
          allRuns.push(runData)

          // 检查是否是今日运行
          // 注意：start_time 是 Unix 时间戳(秒)，需要 *1000 转换为毫秒
          const timestamp = run.start_time ? run.start_time * 1000 : run.created_at
          const runTime = new Date(timestamp)
          if (runTime >= new Date(todayStart) && runTime < new Date(todayEnd)) {
            todayRuns.push(runData)
            if (run.status === 'success') {
              successCount++
            }
          }
        }
      } catch (err) {
        console.warn(`获取流水线 ${pipeline.name} 历史失败:`, err)
      }
    }

    // 更新统计数据
    stats.value.todayRuns = todayRuns.length
    stats.value.successRate = todayRuns.length > 0
      ? Math.round((successCount / todayRuns.length) * 100)
      : 0

    // 按时间排序取最近4条记录
    recentRuns.value = allRuns
      .sort((a, b) => new Date(b.rawTime) - new Date(a.rawTime))
      .slice(0, 4)

  } catch (error) {
    console.error('获取工作台数据失败:', error)
    ElMessage.error('获取数据失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchDashboardData()
})

const getStatusType = (status) => {
  const types = {
    success: 'success',
    running: 'warning',
    failed: 'danger'
  }
  return types[status] || 'info'
}

const getStatusText = (status) => {
  const texts = {
    success: '成功',
    running: '运行中',
    failed: '失败'
  }
  return texts[status] || status
}

const handleCreatePipeline = () => {
  router.push('/pipeline')
}

const handleCreateProject = () => {
  router.push('/project')
}

const handleManageDeploys = () => {
  router.push('/deploy')
}

const handleGoSettings = () => {
  router.push('/settings')
}

const viewAllPipelines = () => {
  router.push('/pipeline')
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.dashboard-container {
  .dashboard-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 28px;
    
    .page-title {
      font-family: $font-family-display;
      font-size: 28px;
      font-weight: 700;
      color: var(--text-primary);
      letter-spacing: -0.02em;
    }
  }
  
  // ============================================
  // Modern Stat Cards with Neumorphic Style
  // ============================================
  .stats-overview {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 20px;
    margin-bottom: 28px;
    
    .stat-card {
      display: flex;
      align-items: center;
      padding: 24px;
      background: var(--bg-card);
      border-radius: $radius-xl;
      box-shadow: $shadow-md;
      transition: all $transition-base;
      
      &:hover {
        transform: translateY(-2px);
        box-shadow: $shadow-lg;
      }
      
      .stat-icon {
        width: 56px;
        height: 56px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: $radius-lg;
        margin-right: 18px;
        box-shadow: $shadow-sm;
        
        &.blue {
          background: linear-gradient(135deg, $primary-light 0%, rgba($primary-color, 0.15) 100%);
          color: $primary-color;
        }
        
        &.green {
          background: linear-gradient(135deg, $success-light 0%, rgba($success-color, 0.15) 100%);
          color: darken($success-color, 15%);
        }
        
        &.orange {
          background: linear-gradient(135deg, $warning-light 0%, rgba($warning-color, 0.15) 100%);
          color: darken($warning-color, 15%);
        }
        
        &.success {
          background: linear-gradient(135deg, $success-light 0%, rgba($success-color, 0.15) 100%);
          color: darken($success-color, 15%);
        }
      }
      
      .stat-content {
        .stat-value {
          font-family: $font-family-display;
          font-size: 32px;
          font-weight: 700;
          color: var(--text-primary);
          letter-spacing: -0.02em;
        }
        
        .stat-label {
          font-size: 14px;
          color: var(--text-secondary);
          margin-top: 4px;
          font-weight: 500;
        }
      }
    }
  }
  
  .dashboard-content {
    display: flex;
    gap: 24px;
    
    .content-main {
      flex: 1;
    }
    
    .content-sidebar {
      width: 340px;
      flex-shrink: 0;
    }
    
    // ============================================
    // Modern Section Cards
    // ============================================
    .section-card {
      background: var(--bg-card);
      border-radius: $radius-xl;
      padding: 24px;
      margin-bottom: 20px;
      box-shadow: $shadow-md;
      
      .section-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 20px;
        
        .section-title {
          font-family: $font-family-display;
          font-size: 18px;
          font-weight: 600;
          color: var(--text-primary);
        }
        
        :deep(.el-button--text) {
          color: $primary-color;
          font-weight: 500;
          
          &:hover {
            color: $primary-active;
          }
        }
      }
      
      // Modern Table Styles
      :deep(.el-table) {
        background: transparent;
        
        th.el-table__cell {
          background: transparent;
          color: var(--text-secondary);
          font-weight: 600;
          font-size: 13px;
          border-bottom: 1px solid $border-color;
          padding: 14px 0;
        }
        
        td.el-table__cell {
          color: var(--text-primary);
          border-bottom: 1px solid $border-color-light;
          padding: 14px 0;
        }
        
        .el-table__row:hover > td.el-table__cell {
          background: rgba($primary-color, 0.04);
        }
        
        .el-tag {
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
          
          &.el-tag--info {
            background: $info-light;
            color: darken($info-color, 20%);
          }
        }
      }
      
      .pipeline-cell {
        display: flex;
        align-items: center;
        gap: 10px;
        
        .pipeline-icon {
          width: 28px;
          height: 28px;
          display: flex;
          align-items: center;
          justify-content: center;
          background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
          color: white;
          font-size: 12px;
          font-weight: 600;
          border-radius: $radius-sm;
          box-shadow: 0 2px 6px rgba($primary-color, 0.3);
        }
      }
      
      .build-id {
        font-family: $font-family-mono;
        color: var(--text-secondary);
        font-weight: 500;
      }
    }
    
    // ============================================
    // Modern Quick Actions
    // ============================================
    .quick-actions {
      display: grid;
      grid-template-columns: repeat(2, 1fr);
      gap: 14px;
      
      .action-item {
        display: flex;
        flex-direction: column;
        align-items: center;
        padding: 20px;
        background: var(--bg-secondary);
        border-radius: $radius-lg;
        cursor: pointer;
        transition: all $transition-base;
        box-shadow: $shadow-sm;
        
        &:hover {
          transform: translateY(-3px);
          box-shadow: $shadow-md;
          background: var(--bg-card);
          
          .action-icon {
            transform: scale(1.1);
            box-shadow: 0 4px 12px rgba(0,0,0,0.15);
          }
        }
        
        &:active {
          transform: translateY(-1px);
          box-shadow: $shadow-inset;
        }
        
        .action-icon {
          width: 48px;
          height: 48px;
          display: flex;
          align-items: center;
          justify-content: center;
          border-radius: $radius-md;
          margin-bottom: 12px;
          transition: all $transition-base;
          box-shadow: $shadow-sm;
          
          &.blue { 
            background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
            color: white;
          }
          &.green { 
            background: linear-gradient(135deg, $success-color 0%, lighten($success-color, 10%) 100%);
            color: white;
          }
          &.orange { 
            background: linear-gradient(135deg, $warning-color 0%, lighten($warning-color, 10%) 100%);
            color: white;
          }
          &.purple { 
            background: linear-gradient(135deg, #A78BFA 0%, #8B5CF6 100%);
            color: white;
          }
        }
        
        span {
          font-size: 13px;
          color: var(--text-secondary);
          font-weight: 500;
        }
      }
    }
    
    // ============================================
    // Modern Announcements
    // ============================================
    .announcements {
      .announcement-item {
        padding: 16px 0;
        border-bottom: 1px solid $border-color-light;
        
        &:last-child {
          border-bottom: none;
        }
        
        .el-tag {
          border-radius: $radius-full;
          font-weight: 500;
          border: none;
        }
        
        .announcement-title {
          font-size: 14px;
          color: var(--text-primary);
          margin: 10px 0 6px;
          font-weight: 500;
        }
        
        .announcement-time {
          font-size: 12px;
          color: $text-muted;
        }
      }
    }
  }
}
</style>
