<template>
  <div class="statistics-container">
    <div class="statistics-header">
      <h1 class="page-title">统计</h1>
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
                stroke="#67C23A" 
                stroke-width="20"
                :stroke-dasharray="successDashArray"
                stroke-dashoffset="0"
                transform="rotate(-90 50 50)"
              />
              <circle 
                cx="50" cy="50" r="40" 
                fill="transparent" 
                stroke="#F56C6C" 
                stroke-width="20"
                :stroke-dasharray="failedDashArray"
                :stroke-dashoffset="-'-' + successDashArray"
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
.statistics-container {
  .statistics-header {
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
  
  .stats-overview {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
    margin-bottom: 20px;
    
    .stat-card {
      display: flex;
      align-items: center;
      padding: 20px;
      background: white;
      border-radius: 8px;
      
      .stat-icon {
        width: 48px;
        height: 48px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: 8px;
        margin-right: 16px;
        
        &.blue {
          background: #ecf5ff;
          color: #409EFF;
        }
        
        &.green {
          background: #f0f9eb;
          color: #67C23A;
        }
        
        &.orange {
          background: #fdf6ec;
          color: #E6A23C;
        }
        
        &.red {
          background: #fef0f0;
          color: #F56C6C;
        }
      }
      
      .stat-content {
        .stat-value {
          font-size: 24px;
          font-weight: 600;
          color: #303133;
        }
        
        .stat-label {
          font-size: 14px;
          color: #909399;
          margin-top: 4px;
        }
      }
    }
  }
  
  .stats-charts {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 16px;
    margin-bottom: 20px;
    
    .chart-card {
      background: white;
      border-radius: 8px;
      padding: 20px;
      
      .chart-title {
        font-size: 16px;
        font-weight: 500;
        color: #303133;
        margin-bottom: 16px;
      }
      
      .chart-area {
        height: 220px;
        display: flex;
        flex-direction: column;
      }
      
      .bar-chart {
        flex: 1;
        display: flex;
        align-items: flex-end;
        justify-content: space-around;
        padding: 0 10px;
        gap: 8px;
        
        .bar-item {
          display: flex;
          flex-direction: column;
          align-items: center;
          flex: 1;
          max-width: 50px;
          
          .bar-container {
            height: 120px;
            width: 100%;
            display: flex;
            align-items: flex-end;
            justify-content: center;
            
            .bar-stack {
              width: 100%;
              display: flex;
              flex-direction: column;
              justify-content: flex-end;
              border-radius: 4px 4px 0 0;
              transition: height 0.3s ease;
              
              .bar-segment {
                width: 100%;
                display: flex;
                align-items: center;
                justify-content: center;
                transition: height 0.3s ease;
                
                &.failed {
                  background: linear-gradient(180deg, #F56C6C 0%, #f89898 100%);
                  border-radius: 0;
                }
                
                &.success {
                  background: linear-gradient(180deg, #67C23A 0%, #85ce61 100%);
                  border-radius: 4px 4px 0 0;
                }
                
                .bar-value {
                  font-size: 11px;
                  font-weight: 600;
                  color: white;
                  text-shadow: 0 1px 2px rgba(0,0,0,0.3);
                }
              }
            }
          }
          
          .bar-label {
            margin-top: 4px;
            font-size: 12px;
            color: #909399;
            text-align: center;
          }
          
          .bar-total {
            font-size: 12px;
            font-weight: 600;
            color: #303133;
            margin-top: 4px;
            
            &.empty {
              color: #c0c4cc;
            }
          }
        }
      }
      
      .trend-summary {
        display: flex;
        justify-content: center;
        gap: 20px;
        padding: 10px 0;
        border-top: 1px solid #ebeef5;
        margin-top: 10px;
        
        span {
          font-size: 13px;
          color: #606266;
        }
        
        .trend-success {
          color: #67C23A;
        }
        
        .trend-failed {
          color: #F56C6C;
        }
      }
      
      .pie-container {
        position: relative;
        width: 160px;
        height: 160px;
        margin: 0 auto;
        
        .pie-svg {
          width: 100%;
          height: 100%;
        }
        
        .pie-center {
          position: absolute;
          top: 50%;
          left: 50%;
          transform: translate(-50%, -50%);
          text-align: center;
          
          .pie-percentage {
            display: block;
            font-size: 28px;
            font-weight: 700;
            color: #303133;
            line-height: 1.2;
          }
          
          .pie-label {
            display: block;
            font-size: 12px;
            color: #909399;
            margin-top: 4px;
          }
        }
      }
      
      .pie-legend {
        display: flex;
        justify-content: center;
        gap: 24px;
        margin-top: 16px;
        
        .legend-item {
          display: flex;
          align-items: center;
          gap: 8px;
          font-size: 14px;
          color: #606266;
          
          .legend-dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
            
            &.success {
              background: #67C23A;
            }
            
            &.failed {
              background: #F56C6C;
            }
          }
        }
      }
    }
  }
  
  .stats-table {
    background: white;
    border-radius: 8px;
    padding: 20px;
    
    .table-title {
      font-size: 16px;
      font-weight: 500;
      color: #303133;
      margin-bottom: 16px;
    }
    
    .pipeline-name {
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
    }
    
    .text-success {
      color: #67C23A;
    }
    
    .text-warning {
      color: #E6A23C;
    }
  }
}
</style>
