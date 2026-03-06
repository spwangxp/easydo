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
  animation: float-up 0.45s ease both;

  .dashboard-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 22px;

    .page-title {
      font-family: $font-family-display;
      font-size: 32px;
      font-weight: 760;
      letter-spacing: -0.03em;
      color: var(--text-primary);
    }

    .header-actions :deep(.el-button--primary) {
      height: 42px;
      padding: 0 18px;
    }
  }

  .stats-overview {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
    margin-bottom: 22px;

    .stat-card {
      position: relative;
      overflow: hidden;
      display: flex;
      align-items: center;
      padding: 20px;
      border-radius: $radius-xl;
      border: 1px solid var(--border-color-light);
      background: var(--bg-card);
      box-shadow: var(--shadow-md);
      backdrop-filter: $blur-md;
      -webkit-backdrop-filter: $blur-md;
      transition: transform $transition-base, box-shadow $transition-base;

      &::after {
        content: '';
        position: absolute;
        width: 120px;
        height: 120px;
        border-radius: 50%;
        right: -44px;
        top: -44px;
        opacity: 0.2;
        pointer-events: none;
      }

      &:hover {
        transform: translateY(-3px);
        box-shadow: var(--shadow-lg);
      }

      .stat-icon {
        width: 54px;
        height: 54px;
        border-radius: $radius-lg;
        display: flex;
        align-items: center;
        justify-content: center;
        margin-right: 16px;
        box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.6);

        &.blue {
          color: var(--primary-color);
          background: linear-gradient(145deg, rgba($primary-color, 0.24), rgba($primary-color, 0.1));
        }

        &.green {
          color: $success-color;
          background: linear-gradient(145deg, rgba($success-color, 0.26), rgba($success-color, 0.1));
        }

        &.orange {
          color: $warning-color;
          background: linear-gradient(145deg, rgba($warning-color, 0.28), rgba($warning-color, 0.12));
        }

        &.success {
          color: $success-color;
          background: linear-gradient(145deg, rgba($success-color, 0.3), rgba($success-color, 0.12));
        }
      }

      .stat-content {
        .stat-value {
          font-family: $font-family-display;
          font-size: 30px;
          line-height: 1;
          font-weight: 760;
          letter-spacing: -0.03em;
          color: var(--text-primary);
        }

        .stat-label {
          margin-top: 8px;
          font-size: 13px;
          font-weight: 600;
          color: var(--text-secondary);
        }
      }

      &:nth-child(1)::after { background: rgba($primary-color, 0.4); }
      &:nth-child(2)::after { background: rgba($success-color, 0.36); }
      &:nth-child(3)::after { background: rgba($warning-color, 0.34); }
      &:nth-child(4)::after { background: rgba($info-color, 0.36); }
    }
  }

  .dashboard-content {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 320px;
    gap: 18px;

    .section-card {
      border-radius: $radius-xl;
      border: 1px solid var(--border-color-light);
      background: var(--bg-card);
      box-shadow: var(--shadow-md);
      backdrop-filter: $blur-md;
      -webkit-backdrop-filter: $blur-md;
      padding: 20px;
      margin-bottom: 18px;

      .section-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 14px;

        .section-title {
          font-family: $font-family-display;
          font-size: 18px;
          font-weight: 700;
          letter-spacing: -0.02em;
          color: var(--text-primary);
        }

        :deep(.el-button--text) {
          color: var(--text-secondary);
          font-weight: 600;

          &:hover {
            color: var(--primary-color);
          }
        }
      }

      :deep(.el-table) {
        background: transparent;

        th.el-table__cell {
          height: 44px;
          border-bottom: 1px solid var(--border-color-light);
          background: rgba(255, 255, 255, 0.36);
        }

        td.el-table__cell {
          height: 52px;
        }
      }

      .pipeline-cell {
        display: flex;
        align-items: center;
        gap: 10px;

        .pipeline-icon {
          width: 30px;
          height: 30px;
          border-radius: 10px;
          display: flex;
          align-items: center;
          justify-content: center;
          color: white;
          font-size: 12px;
          font-weight: 700;
          background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
          box-shadow: 0 10px 20px rgba($primary-color, 0.25);
        }
      }

      .build-id {
        font-family: $font-family-mono;
        font-weight: 600;
        color: var(--text-secondary);
      }
    }

    .quick-actions {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;

      .action-item {
        border-radius: $radius-lg;
        border: 1px solid var(--border-color-light);
        background: var(--bg-elevated);
        box-shadow: var(--shadow-sm);
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 10px;
        padding: 16px 12px;
        cursor: pointer;
        transition: all $transition-base;

        &:hover {
          transform: translateY(-3px);
          box-shadow: var(--shadow-md);
          border-color: var(--border-color-hover);
        }

        .action-icon {
          width: 46px;
          height: 46px;
          border-radius: 14px;
          display: flex;
          align-items: center;
          justify-content: center;
          color: #fff;
          box-shadow: 0 10px 18px rgba(12, 36, 74, 0.26);

          &.blue { background: linear-gradient(140deg, #2d7bff, #4ca3ff); }
          &.green { background: linear-gradient(140deg, #2bbf89, #40d49b); }
          &.orange { background: linear-gradient(140deg, #f29f38, #ffc067); }
          &.purple { background: linear-gradient(140deg, #1f8bbf, #4bb9ea); }
        }

        span {
          font-size: 13px;
          font-weight: 600;
          color: var(--text-secondary);
        }
      }
    }

    .announcements {
      .announcement-item {
        padding: 12px 0;
        border-bottom: 1px solid var(--border-color-light);

        &:last-child {
          border-bottom: none;
          padding-bottom: 0;
        }

        .announcement-title {
          margin: 10px 0 6px;
          font-size: 14px;
          font-weight: 600;
          color: var(--text-primary);
        }

        .announcement-time {
          font-size: 12px;
          color: var(--text-muted);
        }
      }
    }
  }
}

@media (max-width: 1200px) {
  .dashboard-container {
    .stats-overview {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .dashboard-content {
      grid-template-columns: 1fr;
    }
  }
}

@media (max-width: 768px) {
  .dashboard-container {
    .dashboard-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 12px;

      .page-title {
        font-size: 27px;
      }
    }

    .stats-overview {
      grid-template-columns: 1fr;
    }
  }
}
</style>
