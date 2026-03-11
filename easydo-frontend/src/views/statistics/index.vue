<template>
  <div class="statistics-container">
    <div class="statistics-header">
      <div>
        <h1 class="page-title">统计</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
      <div class="date-range-picker">
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="-"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          size="default"
        />
      </div>
    </div>
    
    <div class="stats-overview" v-loading="loading">
      <div class="stat-card">
        <div class="stat-icon blue">
          <el-icon :size="24"><Connection /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.total_runs }}</div>
          <div class="stat-label">总运行次数</div>
        </div>
      </div>
      
      <div class="stat-card">
        <div class="stat-icon green">
          <el-icon :size="24"><CircleCheck /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.success_rate }}%</div>
          <div class="stat-label">成功率</div>
        </div>
      </div>
      
      <div class="stat-card">
        <div class="stat-icon orange">
          <el-icon :size="24"><Clock /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.avg_duration }}</div>
          <div class="stat-label">平均耗时</div>
        </div>
      </div>
      
      <div class="stat-card">
        <div class="stat-icon red">
          <el-icon :size="24"><WarningFilled /></el-icon>
        </div>
        <div class="stat-content">
          <div class="stat-value">{{ stats.failed_count }}</div>
          <div class="stat-label">失败次数</div>
        </div>
      </div>
    </div>
    
    <div class="stats-charts" v-loading="loading">
      <div class="chart-card">
        <h3 class="chart-title">运行趋势</h3>
        <div class="chart-area">
          <div class="bar-chart">
            <div 
              v-for="(day, index) in trendData" 
              :key="index" 
              class="bar-item"
            >
              <div class="bar-container">
                <div class="bar-stack" :style="{ height: getStackHeight(day) + '%' }">
                  <div 
                    v-if="day.failed > 0"
                    class="bar-segment failed"
                    :style="{ height: (day.failed / day.total * 100) + '%' }"
                  >
                    <span class="bar-value">{{ day.failed }}</span>
                  </div>
                  <div 
                    v-if="day.success > 0"
                    class="bar-segment success"
                    :style="{ height: (day.success / day.total * 100) + '%' }"
                  >
                    <span class="bar-value">{{ day.success }}</span>
                  </div>
                </div>
              </div>
              <div class="bar-label">{{ day.date_label || '' }}</div>
              <div class="bar-total" v-if="day.total > 0">{{ day.total }}</div>
              <div class="bar-total empty" v-else>-</div>
            </div>
          </div>
          <div class="trend-summary" v-if="trendData.length > 0">
            <span class="trend-total">总计: {{ totalTrendRuns }} 次运行</span>
            <span class="trend-success">成功: {{ trendSuccessCount }}</span>
            <span class="trend-failed">失败: {{ trendFailedCount }}</span>
          </div>
        </div>
      </div>
      
      <div class="chart-card">
        <h3 class="chart-title">成功率分布</h3>
        <div class="chart-area">
          <div class="pie-container">
            <svg viewBox="0 0 100 100" class="pie-svg">
              <circle 
                cx="50" cy="50" r="40" 
                fill="transparent" 
                stroke="var(--success-color)" 
                stroke-width="20"
                :stroke-dasharray="successDashArray"
                stroke-dashoffset="0"
                transform="rotate(-90 50 50)"
              />
              <circle 
                cx="50" cy="50" r="40" 
                fill="transparent" 
                stroke="var(--danger-color)" 
                stroke-width="20"
                :stroke-dasharray="failedDashArray"
                :stroke-dashoffset="failedDashOffset"
                transform="rotate(-90 50 50)"
              />
            </svg>
            <div class="pie-center">
              <span class="pie-percentage">{{ stats.success_rate }}%</span>
              <span class="pie-label">成功率</span>
            </div>
          </div>
          <div class="pie-legend">
            <div class="legend-item">
              <span class="legend-dot success"></span>
              <span>成功: {{ trendSuccessCount }} 次</span>
            </div>
            <div class="legend-item">
              <span class="legend-dot failed"></span>
              <span>失败: {{ trendFailedCount }} 次</span>
            </div>
          </div>
        </div>
      </div>
    </div>
    
    <div class="stats-table" v-loading="loading">
      <h3 class="table-title">Top 流水线排行</h3>
      <el-table :data="topPipelines" style="width: 100%" empty-text="暂无流水线数据">
        <el-table-column prop="name" label="流水线名称" min-width="200">
          <template #default="{ row }">
            <div class="pipeline-name">
              <span class="name-icon">{{ row.name.charAt(0).toUpperCase() }}</span>
              <span>{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>
        
        <el-table-column prop="run_count" label="运行次数" width="120" sortable>
          <template #default="{ row }">
            {{ row.run_count }} 次
          </template>
        </el-table-column>
        
        <el-table-column prop="success_rate" label="成功率" width="120" sortable>
          <template #default="{ row }">
            <span :class="{ 'text-success': row.success_rate >= 90, 'text-warning': row.success_rate < 90 }">
              {{ row.success_rate }}%
            </span>
          </template>
        </el-table-column>
        
        <el-table-column prop="avg_duration" label="平均耗时" width="120">
          <template #default="{ row }">
            {{ row.avg_duration }}
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch, computed } from 'vue'
import { 
  Connection, 
  CircleCheck, 
  Clock, 
  WarningFilled 
} from '@element-plus/icons-vue'
import { getStatsOverview, getStatsTrend, getTopPipelines } from '@/api/statistics'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

const dateRange = ref([
  new Date(Date.now() - 7 * 24 * 60 * 60 * 1000),
  new Date()
])

const loading = ref(false)

const stats = ref({
  total_runs: 0,
  success_rate: 0,
  avg_duration: '0s',
  failed_count: 0
})

const trendData = ref([])
const topPipelines = ref([])

const maxTrendValue = computed(() => {
  if (!trendData.value || trendData.value.length === 0) return 1
  const max = Math.max(...trendData.value.map(d => d.total))
  return max > 0 ? max : 1
})

const totalTrendRuns = computed(() => {
  if (!trendData.value) return 0
  return trendData.value.reduce((sum, d) => sum + d.total, 0)
})

const trendSuccessCount = computed(() => {
  if (!trendData.value) return 0
  return trendData.value.reduce((sum, d) => sum + d.success, 0)
})

const trendFailedCount = computed(() => {
  if (!trendData.value) return 0
  return trendData.value.reduce((sum, d) => sum + d.failed, 0)
})

const getStackHeight = (day) => {
  if (day.total === 0) return 0
  return 100
}

const circumference = 2 * Math.PI * 40

const successDashArray = computed(() => {
  const successRate = stats.value.success_rate || 0
  const successLength = (successRate / 100) * circumference
  return `${successLength} ${circumference}`
})

const failedDashArray = computed(() => {
  const failedRate = 100 - (stats.value.success_rate || 0)
  const failedLength = (failedRate / 100) * circumference
  return `${failedLength} ${circumference}`
})

const failedDashOffset = computed(() => {
  const successRate = stats.value.success_rate || 0
  return -((successRate / 100) * circumference)
})

const fetchOverview = async () => {
  try {
    const params = {}
    if (dateRange.value && dateRange.value[0] && dateRange.value[1]) {
      params.start_date = formatDate(dateRange.value[0])
      params.end_date = formatDate(dateRange.value[1])
    }
    const res = await getStatsOverview(params)
    if (res.code === 200) {
      stats.value = {
        total_runs: res.data.total_runs || 0,
        success_rate: res.data.success_rate || 0,
        avg_duration: res.data.avg_duration || '0s',
        failed_count: res.data.failed_count || 0
      }
    }
  } catch (error) {
    console.error('获取概览数据失败:', error)
  }
}

const fetchTrend = async () => {
  try {
    const days = 7
    const res = await getStatsTrend({ days })
    if (res.code === 200 && res.data.daily_runs) {
      trendData.value = res.data.daily_runs.map(day => ({
        date: day.date,
        date_label: day.date_label,
        total: day.total,
        success: day.success,
        failed: day.failed,
        success_rate: day.success_rate
      }))
    }
  } catch (error) {
    console.error('获取趋势数据失败:', error)
  }
}

const fetchTopPipelines = async () => {
  try {
    const params = { limit: 10 }
    if (dateRange.value && dateRange.value[0] && dateRange.value[1]) {
      params.start_date = formatDate(dateRange.value[0])
      params.end_date = formatDate(dateRange.value[1])
    }
    const res = await getTopPipelines(params)
    if (res.code === 200 && res.data.pipelines) {
      topPipelines.value = res.data.pipelines.map(p => ({
        name: p.name,
        run_count: p.run_count,
        success_rate: p.success_rate,
        avg_duration: p.avg_duration
      }))
    }
  } catch (error) {
    console.error('获取Top流水线失败:', error)
  }
}

const fetchAllData = async () => {
  loading.value = true
  try {
    await Promise.all([
      fetchOverview(),
      fetchTrend(),
      fetchTopPipelines()
    ])
  } finally {
    loading.value = false
  }
}

const formatDate = (date) => {
  if (!date) return ''
  const d = new Date(date)
  return d.toISOString().split('T')[0]
}

watch(dateRange, () => {
  fetchAllData()
}, { deep: true })

onMounted(() => {
  fetchAllData()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.statistics-container {
  animation: float-up 0.45s ease both;

  .statistics-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }

  .page-title {
    font-family: $font-family-display;
    font-size: 32px;
    font-weight: 760;
    letter-spacing: -0.03em;
    color: var(--text-primary);
  }

  .stats-overview {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
    margin-bottom: 18px;

    .stat-card {
      display: flex;
      align-items: center;
      gap: 14px;
      padding: 18px;
      border-radius: $radius-xl;
      border: 1px solid var(--border-color-light);
      background: var(--bg-card);
      backdrop-filter: $blur-md;
      -webkit-backdrop-filter: $blur-md;
      box-shadow: var(--shadow-md);

      .stat-icon {
        width: 46px;
        height: 46px;
        border-radius: 14px;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.6);

        &.blue {
          background: var(--primary-lighter);
          color: var(--primary-color);
        }

        &.green {
          background: var(--success-light);
          color: var(--success-color);
        }

        &.orange {
          background: var(--warning-light);
          color: var(--warning-color);
        }

        &.red {
          background: var(--danger-light);
          color: var(--danger-color);
        }
      }

      .stat-value {
        font-size: 30px;
        line-height: 1;
        font-weight: 760;
        color: var(--text-primary);
      }

      .stat-label {
        margin-top: 6px;
        font-size: 13px;
        color: var(--text-muted);
      }
    }
  }

  .stats-charts {
    display: grid;
    grid-template-columns: 1.5fr 1fr;
    gap: 16px;
    margin-bottom: 18px;
  }

  .chart-card,
  .stats-table {
    border-radius: $radius-xl;
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    backdrop-filter: $blur-md;
    -webkit-backdrop-filter: $blur-md;
    box-shadow: var(--shadow-md);
    padding: 20px;
  }

  .chart-title,
  .table-title {
    font-size: 16px;
    font-weight: 650;
    color: var(--text-primary);
    margin-bottom: 16px;
  }

  .chart-area {
    height: 230px;
    display: flex;
    flex-direction: column;
  }

  .bar-chart {
    flex: 1;
    display: flex;
    align-items: flex-end;
    justify-content: space-around;
    gap: 8px;
    padding: 0 10px;
  }

  .bar-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    flex: 1;
    max-width: 56px;
  }

  .bar-container {
    width: 100%;
    height: 128px;
    display: flex;
    align-items: flex-end;
    justify-content: center;
  }

  .bar-stack {
    width: 100%;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    border-radius: 6px 6px 0 0;
    transition: height $transition-base;
    overflow: hidden;
  }

  .bar-segment {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: height $transition-base;

    &.failed {
      background: linear-gradient(180deg, var(--danger-color), rgba(231, 90, 90, 0.72));
    }

    &.success {
      background: linear-gradient(180deg, var(--success-color), rgba(31, 188, 132, 0.72));
    }
  }

  .bar-value {
    font-size: 11px;
    font-weight: 700;
    color: #fff;
    text-shadow: 0 1px 2px rgba(0, 0, 0, 0.25);
  }

  .bar-label {
    margin-top: 4px;
    font-size: 12px;
    color: var(--text-muted);
  }

  .bar-total {
    margin-top: 4px;
    font-size: 12px;
    font-weight: 700;
    color: var(--text-primary);

    &.empty {
      color: var(--text-tertiary);
    }
  }

  .trend-summary {
    margin-top: 8px;
    padding-top: 10px;
    border-top: 1px solid var(--border-color-light);
    display: flex;
    justify-content: center;
    gap: 18px;
    font-size: 13px;
    color: var(--text-secondary);

    .trend-success {
      color: var(--success-color);
    }

    .trend-failed {
      color: var(--danger-color);
    }
  }

  .pie-container {
    position: relative;
    width: 158px;
    height: 158px;
    margin: 0 auto;
  }

  .pie-svg {
    width: 100%;
    height: 100%;
    filter: drop-shadow(0 4px 10px rgba(0, 0, 0, 0.15));
  }

  .pie-center {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    text-align: center;
  }

  .pie-percentage {
    display: block;
    font-size: 28px;
    line-height: 1;
    font-weight: 780;
    color: var(--text-primary);
  }

  .pie-label {
    display: block;
    margin-top: 5px;
    font-size: 12px;
    color: var(--text-muted);
  }

  .pie-legend {
    margin-top: 16px;
    display: flex;
    justify-content: center;
    gap: 24px;
    color: var(--text-secondary);
    font-size: 13px;
  }

  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .legend-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;

    &.success {
      background: var(--success-color);
    }

    &.failed {
      background: var(--danger-color);
    }
  }

  .pipeline-name {
    display: inline-flex;
    align-items: center;
    gap: 10px;
  }

  .name-icon {
    width: 26px;
    height: 26px;
    border-radius: 8px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    font-weight: 700;
    color: #fff;
    background: linear-gradient(140deg, var(--primary-color), var(--primary-hover));
  }

  .text-success {
    color: var(--success-color);
  }

  .text-warning {
    color: var(--warning-color);
  }
}

@media (max-width: 1200px) {
  .statistics-container {
    .stats-overview {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .stats-charts {
      grid-template-columns: 1fr;
    }
  }
}

@media (max-width: 768px) {
  .statistics-container {
    .statistics-header {
      flex-direction: column;
      align-items: flex-start;
      gap: 10px;
    }

    .stats-overview {
      grid-template-columns: 1fr;
    }
  }
}
</style>
