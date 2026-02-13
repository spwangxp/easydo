<template>
  <div class="pipeline-detail-container">
    <!-- 头部信息 -->
    <div class="detail-header">
      <div class="header-left">
        <div class="back-btn" @click="goBack">
          <el-icon><ArrowLeft /></el-icon>
          <span>返回首页</span>
        </div>
        <div class="pipeline-info">
          <div class="pipeline-icon" :style="{ background: pipeline?.color || '#409EFF' }">
            {{ pipeline?.name?.charAt(0)?.toUpperCase() || 'P' }}
          </div>
          <div class="pipeline-meta">
            <h1 class="pipeline-name">{{ pipeline?.name || '-' }}</h1>
            <div class="pipeline-tags">
              <el-tag size="small" :type="getEnvironmentType(pipeline?.environment)">
                {{ getEnvironmentText(pipeline?.environment) }}
              </el-tag>
              <span class="create-time">创建于 {{ formatDate(pipeline?.created_at) }}</span>
            </div>
          </div>
        </div>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="handleRun">
          <el-icon><VideoPlay /></el-icon>
          运行流水线
        </el-button>
        <el-dropdown trigger="click" @command="handleCommand">
          <el-button :icon="MoreFilled">更多</el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="edit">编辑</el-dropdown-item>
              <el-dropdown-item command="copy">复制</el-dropdown-item>
              <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </div>
    
    <!-- Tab 导航 -->
    <div class="detail-tabs">
      <div 
        class="tab-item" 
        :class="{ active: activeTab === 'design' }"
        @click="activeTab = 'design'"
      >
        <el-icon><Edit /></el-icon>
        <span>设计</span>
      </div>
      <div 
        class="tab-item" 
        :class="{ active: activeTab === 'history' }"
        @click="activeTab = 'history'"
      >
        <el-icon><Clock /></el-icon>
        <span>历史</span>
        <span class="tab-count">{{ totalRuns }}</span>
      </div>
      <div 
        class="tab-item" 
        :class="{ active: activeTab === 'execution' }"
        @click="activeTab = 'execution'"
        v-if="currentRun"
      >
        <el-icon><VideoPlay /></el-icon>
        <span>执行过程</span>
        <el-tag v-if="currentRun.status === 'running'" type="warning" size="small" class="running-tag">运行中</el-tag>
      </div>
      <div 
        class="tab-item" 
        :class="{ active: activeTab === 'report' }"
        @click="activeTab = 'report'"
      >
        <el-icon><Document /></el-icon>
        <span>测试报告</span>
      </div>
      <div 
        class="tab-item" 
        :class="{ active: activeTab === 'statistics' }"
        @click="activeTab = 'statistics'"
      >
        <el-icon><DataAnalysis /></el-icon>
        <span>统计</span>
      </div>
      <div 
        class="tab-item" 
        :class="{ active: activeTab === 'settings' }"
        @click="activeTab = 'settings'"
      >
        <el-icon><Setting /></el-icon>
        <span>设置</span>
      </div>
      <div class="tab-expand" @click="expanded = !expanded">
        <el-icon>
          <Fold v-if="expanded" />
          <Expand v-else />
        </el-icon>
        <span>{{ expanded ? '收起' : '展开' }}</span>
      </div>
    </div>
    
    <!-- Tab 内容 -->
    <div class="detail-content">
      <!-- 设计 Tab -->
      <div v-show="activeTab === 'design'" class="tab-panel design-panel">
        <design-tab :pipeline-id="pipelineId" />
      </div>
      
      <!-- 历史 Tab -->
      <div v-show="activeTab === 'history'" class="tab-panel history-panel">
        <div class="panel-header">
          <h3>执行历史</h3>
          <el-button :icon="Refresh" circle @click="fetchRunHistory" />
        </div>
        <div class="history-list" v-loading="historyLoading">
          <el-table :data="runHistory" style="width: 100%" row-key="id">
            <el-table-column prop="build_number" label="构建号" width="100">
              <template #default="{ row }">
                <span class="build-number">#{{ row.build_number }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="120">
              <template #default="{ row }">
                <el-tag :type="getStatusType(row.status)" size="small">
                  {{ getStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="trigger_type" label="触发方式" width="120" />
            <el-table-column prop="trigger_user" label="触发人" width="100" />
            <el-table-column prop="branch" label="分支" width="150" />
            <el-table-column label="耗时" width="100">
              <template #default="{ row }">
                {{ formatDuration(row.duration) }}
              </template>
            </el-table-column>
            <el-table-column prop="created_at" label="开始时间" width="180">
              <template #default="{ row }">
                {{ formatDateTime(row.created_at) }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="180" fixed="right">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="viewExecutionDetail(row)">
                  查看执行
                </el-button>
                <el-button type="primary" link size="small" @click="viewRunTasks(row)">
                  任务详情
                </el-button>
              </template>
            </el-table-column>
          </el-table>
          
          <el-empty v-if="!historyLoading && runHistory.length === 0" description="暂无执行记录" />
        </div>
      </div>
      
      <!-- 执行过程 Tab -->
      <div v-show="activeTab === 'execution'" class="tab-panel execution-panel" v-loading="executionLoading">
        <div class="panel-header">
          <div class="execution-header-left">
            <h3>执行过程 #{{ currentRun?.build_number }}</h3>
            <el-tag :type="getStatusType(currentRun?.status)" size="large">
              {{ getStatusText(currentRun?.status) }}
            </el-tag>
          </div>
          <div class="execution-header-right">
            <el-button :icon="Refresh" circle @click="fetchExecutionDetail" :loading="executionLoading" />
            <el-button v-if="currentRun?.status === 'running'" type="danger" @click="stopExecution">
              停止执行
            </el-button>
          </div>
        </div>
        
        <div class="execution-content">
          <!-- 执行概览 -->
          <div class="execution-summary">
            <el-card class="summary-item">
              <div class="summary-label">触发方式</div>
              <div class="summary-value">{{ currentRun?.trigger_type || '-' }}</div>
            </el-card>
            <el-card class="summary-item">
              <div class="summary-label">触发人</div>
              <div class="summary-value">{{ currentRun?.trigger_user || '-' }}</div>
            </el-card>
            <el-card class="summary-item">
              <div class="summary-label">开始时间</div>
              <div class="summary-value">{{ formatDateTime(currentRun?.start_time) }}</div>
            </el-card>
            <el-card class="summary-item">
              <div class="summary-label">耗时</div>
              <div class="summary-value">{{ formatDuration(currentRun?.duration) }}</div>
            </el-card>
          </div>
          
          <!-- 任务进度 -->
          <el-card class="task-progress-card">
            <template #header>
              <div class="card-header">
                <span>任务执行进度</span>
                <el-progress 
                  :percentage="executionProgress" 
                  :status="executionProgressStatus"
                  :stroke-width="10"
                  style="width: 200px;"
                />
              </div>
            </template>
            
            <div class="task-list">
              <div 
                v-for="task in runTasks" 
                :key="task.id"
                class="task-item"
                :class="{ 
                  'task-running': task.status === 'running', 
                  'task-failed': task.status === 'failed' || task.display_status === 'blocked',
                  'task-not-executed': task.display_status === 'not_executed'
                }"
              >
                <div class="task-status-icon">
                  <el-icon v-if="task.status === 'pending' || task.display_status === 'not_executed'" color="#909399"><Clock /></el-icon>
                  <el-icon v-else-if="task.status === 'running'" color="#E6A23C" class="running-icon"><Loading /></el-icon>
                  <el-icon v-else-if="task.status === 'success'" color="#67C23A"><SuccessFilled /></el-icon>
                  <el-icon v-else-if="task.status === 'failed' || task.display_status === 'blocked'" color="#F56C6C"><CircleCloseFilled /></el-icon>
                </div>
                <div class="task-info">
                  <div class="task-name">{{ task.name || `任务 #${task.id}` }}</div>
                  <div class="task-meta">
                    <el-tag v-if="task.display_status === 'not_executed'" type="info" size="small">暂未执行</el-tag>
                    <el-tag v-else-if="task.display_status === 'blocked'" type="danger" size="small">已阻塞</el-tag>
                    <span class="task-agent" v-if="task.Agent">{{ task.Agent.name }}</span>
                    <span class="task-duration" v-if="task.duration > 0">耗时: {{ formatDuration(task.duration) }}</span>
                  </div>
                  <div class="task-error" v-if="task.error_msg">
                    <el-icon><Warning /></el-icon>
                    {{ task.error_msg }}
                  </div>
                </div>
                <div class="task-actions">
                  <el-button 
                    type="primary" 
                    link 
                    size="small" 
                    @click="viewTaskLogs(task)"
                    :disabled="task.status === 'pending' || task.display_status === 'not_executed' || task.display_status === 'blocked'"
                  >
                    查看日志
                  </el-button>
                </div>
              </div>
              
              <el-empty v-if="runTasks.length === 0 && !executionLoading" description="暂无任务信息" />
            </div>
          </el-card>
          
          <!-- 实时日志 -->
          <el-card class="execution-logs-card" v-if="showLogPanel">
            <template #header>
              <div class="card-header">
                <span>执行日志 - {{ selectedTask?.name || '全部' }}</span>
                <el-button type="primary" link size="small" @click="showLogPanel = false">
                  <el-icon><Close /></el-icon>
                  关闭
                </el-button>
              </div>
            </template>
            
            <div class="log-panel">
              <div class="log-content" ref="logContentRef">
                <div 
                  v-for="(log, index) in taskLogs" 
                  :key="index"
                  class="log-line"
                  :class="`log-${log.level}`"
                >
                  <span class="log-time">{{ formatLogTime(log.timestamp) }}</span>
                  <span class="log-message">{{ log.message }}</span>
                </div>
              </div>
            </div>
          </el-card>
        </div>
      </div>
      
      <!-- 测试报告 Tab -->
      <div v-show="activeTab === 'report'" class="tab-panel report-panel">
        <div class="panel-header">
          <h3>测试报告</h3>
        </div>
        <div class="report-content">
          <div class="report-summary">
            <el-card class="summary-card">
              <div class="summary-item">
                <span class="summary-value">{{ reportStats.total }}</span>
                <span class="summary-label">总测试数</span>
              </div>
              <div class="summary-item success">
                <span class="summary-value">{{ reportStats.passed }}</span>
                <span class="summary-label">通过</span>
              </div>
              <div class="summary-item danger">
                <span class="summary-value">{{ reportStats.failed }}</span>
                <span class="summary-label">失败</span>
              </div>
              <div class="summary-item warning">
                <span class="summary-value">{{ reportStats.skipped }}</span>
                <span class="summary-label">跳过</span>
              </div>
              <div class="summary-item info">
                <span class="summary-value">{{ reportStats.passRate }}%</span>
                <span class="summary-label">通过率</span>
              </div>
            </el-card>
          </div>
          <div class="report-list">
            <el-table :data="testReports" style="width: 100%">
              <el-table-column prop="name" label="测试套件" min-width="200" />
              <el-table-column prop="total" label="总数" width="100" align="center" />
              <el-table-column prop="passed" label="通过" width="80" align="center">
                <template #default="{ row }">
                  <span class="text-success">{{ row.passed }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="failed" label="失败" width="80" align="center">
                <template #default="{ row }">
                  <span class="text-danger">{{ row.failed }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="duration" label="耗时" width="100">
                <template #default="{ row }">
                  {{ formatDuration(row.duration) }}
                </template>
              </el-table-column>
              <el-table-column prop="run_time" label="执行时间" width="180">
                <template #default="{ row }">
                  {{ formatDateTime(row.run_time) }}
                </template>
              </el-table-column>
            </el-table>
            <el-empty v-if="testReports.length === 0" description="暂无测试报告" />
          </div>
        </div>
      </div>
      
      <!-- 统计 Tab -->
      <div v-show="activeTab === 'statistics'" class="tab-panel statistics-panel">
        <div class="panel-header">
          <h3>运行统计</h3>
          <el-date-picker
            v-model="statsDateRange"
            type="daterange"
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            @change="fetchStatistics"
          />
        </div>
        <div class="statistics-content">
          <el-row :gutter="20" class="stats-overview">
            <el-col :span="6">
              <el-card class="stat-card">
                <el-statistic title="总运行次数" :value="statistics.total_runs" />
              </el-card>
            </el-col>
            <el-col :span="6">
              <el-card class="stat-card">
                <el-statistic title="成功次数" :value="statistics.successful_runs">
                  <template #suffix>
                    <span class="success-rate">({{ statistics.success_rate }}%)</span>
                  </template>
                </el-statistic>
              </el-card>
            </el-col>
            <el-col :span="6">
              <el-card class="stat-card">
                <el-statistic title="失败次数" :value="statistics.failed_runs" />
              </el-card>
            </el-col>
            <el-col :span="6">
              <el-card class="stat-card">
                <el-statistic title="平均耗时" :value="statistics.avg_duration" suffix="分钟" />
              </el-card>
            </el-col>
          </el-row>
          <el-row :gutter="20" class="stats-charts">
            <el-col :span="12">
              <el-card class="chart-card">
                <template #header>
                  <span>成功率趋势</span>
                </template>
                <div class="chart-placeholder">
                  <el-empty description="趋势图表" :image-size="100" />
                </div>
              </el-card>
            </el-col>
            <el-col :span="12">
              <el-card class="chart-card">
                <template #header>
                  <span>运行分布</span>
                </template>
                <div class="chart-placeholder">
                  <el-empty description="分布图表" :image-size="100" />
                </div>
              </el-card>
            </el-col>
          </el-row>
          <el-card class="recent-failures">
            <template #header>
              <span>最近失败</span>
            </template>
            <el-table :data="recentFailures" style="width: 100%">
              <el-table-column prop="build_number" label="构建号" width="100">
                <template #default="{ row }">
                  <span class="build-number">#{{ row.build_number }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="stage" label="阶段" width="120" />
              <el-table-column prop="error" label="错误信息" min-width="200" show-overflow-tooltip />
              <el-table-column prop="created_at" label="时间" width="180">
                <template #default="{ row }">
                  {{ formatDateTime(row.created_at) }}
                </template>
              </el-table-column>
            </el-table>
            <el-empty v-if="recentFailures.length === 0" description="暂无失败记录" />
          </el-card>
        </div>
      </div>
      
      <!-- 设置 Tab -->
      <div v-show="activeTab === 'settings'" class="tab-panel settings-panel">
        <div class="panel-header">
          <h3>流水线设置</h3>
        </div>
        <div class="settings-content">
          <el-tabs v-model="settingsActiveTab">
            <el-tab-pane label="基本设置" name="basic">
              <el-form :model="settingsForm" label-width="100px" class="settings-form">
                <el-form-item label="流水线名称">
                  <el-input v-model="settingsForm.name" placeholder="请输入流水线名称" />
                </el-form-item>
                <el-form-item label="所属项目">
                  <el-select v-model="settingsForm.project_id" placeholder="请选择项目" style="width: 100%">
                    <el-option
                      v-for="project in projectList"
                      :key="project.id"
                      :label="project.name"
                      :value="project.id"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item label="环境">
                  <el-select v-model="settingsForm.environment" placeholder="请选择环境" style="width: 100%">
                    <el-option label="开发环境" value="development" />
                    <el-option label="测试环境" value="testing" />
                    <el-option label="生产环境" value="production" />
                  </el-select>
                </el-form-item>
                <el-form-item label="描述">
                  <el-input
                    v-model="settingsForm.description"
                    type="textarea"
                    :rows="3"
                    placeholder="请输入描述信息"
                  />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="handleSaveSettings">保存设置</el-button>
                </el-form-item>
              </el-form>
            </el-tab-pane>
            <el-tab-pane label="触发设置" name="trigger">
              <el-form label-width="120px" class="settings-form">
                <el-form-item label="代码变更触发">
                  <el-switch v-model="triggerSettings.on_push" />
                  <span class="form-tip">当代码推送到仓库时自动触发</span>
                </el-form-item>
                <el-form-item label="标签触发">
                  <el-switch v-model="triggerSettings.on_tag" />
                  <span class="form-tip">当创建或更新标签时自动触发</span>
                </el-form-item>
                <el-form-item label="定时触发">
                  <el-switch v-model="triggerSettings.on_cron" />
                </el-form-item>
                <el-form-item v-if="triggerSettings.on_cron" label="Cron 表达式">
                  <el-input v-model="triggerSettings.cron_expression" placeholder="0 0 * * *" />
                </el-form-item>
                <el-form-item label="手动触发">
                  <el-switch v-model="triggerSettings.manual" disabled />
                  <span class="form-tip">始终允许手动触发</span>
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="handleSaveTriggers">保存触发设置</el-button>
                </el-form-item>
              </el-form>
            </el-tab-pane>
            <el-tab-pane label="通知设置" name="notification">
              <el-form label-width="120px" class="settings-form">
                <el-form-item label="成功通知">
                  <el-switch v-model="notificationSettings.on_success" />
                  <span class="form-tip">构建成功时发送通知</span>
                </el-form-item>
                <el-form-item label="失败通知">
                  <el-switch v-model="notificationSettings.on_failure" />
                  <span class="form-tip">构建失败时发送通知</span>
                </el-form-item>
                <el-form-item label="邮件通知">
                  <el-switch v-model="notificationSettings.by_email" />
                </el-form-item>
                <el-form-item label="企业微信">
                  <el-switch v-model="notificationSettings.by_dingtalk" />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="handleSaveNotifications">保存通知设置</el-button>
                </el-form-item>
              </el-form>
            </el-tab-pane>
          </el-tabs>
        </div>
      </div>
    </div>
    
    <!-- 运行确认对话框 -->
    <el-dialog
      v-model="runDialogVisible"
      title="运行流水线"
      width="500px"
    >
      <el-form :model="runForm" label-width="100px">
        <el-form-item label="分支">
          <el-input v-model="runForm.branch" placeholder="master" />
        </el-form-item>
        <el-form-item label="提交">
          <el-input v-model="runForm.commit" placeholder="latest" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="runDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="runLoading" @click="confirmRun">运行</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch, onUnmounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft,
  VideoPlay,
  MoreFilled,
  Edit,
  Clock,
  Document,
  DataAnalysis,
  Setting,
  Fold,
  Expand,
  Refresh,
  Loading,
  SuccessFilled,
  CircleCloseFilled,
  Warning,
  Close
} from '@element-plus/icons-vue'
import { getPipelineDetail, runPipeline, updatePipeline, getPipelineRuns, getPipelineStatistics, getPipelineTestReports, getRunTasks, getRunLogs } from '@/api/pipeline'
import { getTaskLogs as fetchTaskLogsFromApi } from '@/api/task'
import { getProjectList } from '@/api/project'
import DesignTab from './designTab.vue'
import realtime from '@/utils/realtime'

const route = useRoute()
const router = useRouter()

const pipelineId = computed(() => parseInt(route.params.id))

// 状态
const activeTab = ref('design')
const expanded = ref(true)
const historyLoading = ref(false)
const runDialogVisible = ref(false)
const runLoading = ref(false)
const settingsActiveTab = ref('basic')

// 数据
const pipeline = ref(null)
const runHistory = ref([])
const totalRuns = ref(0)
const projectList = ref([])
const testReports = ref([])
const recentFailures = ref([])

// 运行表单
const runForm = reactive({
  branch: 'master',
  commit: ''
})

// 设置表单
const settingsForm = reactive({
  name: '',
  project_id: null,
  environment: 'development',
  description: ''
})

// 触发设置
const triggerSettings = reactive({
  on_push: true,
  on_tag: false,
  on_cron: false,
  cron_expression: '',
  manual: true
})

// 通知设置
const notificationSettings = reactive({
  on_success: true,
  on_failure: true,
  by_email: true,
  by_dingtalk: false
})

// 统计
const statsDateRange = ref(null)
const statistics = reactive({
  total_runs: 0,
  successful_runs: 0,
  failed_runs: 0,
  success_rate: 0,
  avg_duration: 0
})

// 报告统计
const reportStats = reactive({
  total: 0,
  passed: 0,
  failed: 0,
  skipped: 0,
  passRate: 0
})

// 执行过程相关
const currentRun = ref(null)
const runTasks = ref([])
const taskLogs = ref([])
const executionLoading = ref(false)
const showLogPanel = ref(false)
const selectedTask = ref(null)
const logContentRef = ref(null)
let executionPollingTimer = null

// 计算执行进度
const executionProgress = computed(() => {
  if (!runTasks.value || runTasks.value.length === 0) return 0
  const completed = runTasks.value.filter(t => t.status === 'success' || t.status === 'failed').length
  return Math.round((completed / runTasks.value.length) * 100)
})

// 计算执行进度状态
const executionProgressStatus = computed(() => {
  if (!currentRun.value) return ''
  if (currentRun.value.status === 'success') return 'success'
  if (currentRun.value.status === 'failed') return 'exception'
  if (currentRun.value.status === 'running') return ''
  return ''
})

// 获取流水线详情
const fetchPipelineDetail = async () => {
  try {
    const response = await getPipelineDetail(pipelineId.value)
    if (response.code === 200) {
      pipeline.value = response.data
      // 更新设置表单
      settingsForm.name = pipeline.value.name
      settingsForm.project_id = pipeline.value.project_id
      settingsForm.environment = pipeline.value.environment
      settingsForm.description = pipeline.value.description || ''
    }
  } catch (error) {
    console.error('获取流水线详情失败:', error)
    ElMessage.error('获取流水线详情失败')
  }
}

// 获取运行历史
const fetchRunHistory = async () => {
  historyLoading.value = true
  try {
    const response = await getPipelineRuns(pipelineId.value, { page: 1, page_size: 50 })
    if (response.code === 200) {
      runHistory.value = response.data.list || []
      totalRuns.value = response.data.total || 0
    }
  } catch (error) {
    console.error('获取运行历史失败:', error)
  } finally {
    historyLoading.value = false
  }
}

// 获取项目列表
const fetchProjects = async () => {
  try {
    const response = await getProjectList({})
    projectList.value = response.data.list || []
  } catch (error) {
    console.error('获取项目列表失败:', error)
  }
}

// 获取统计数据
const fetchStatistics = async () => {
  try {
    const response = await getPipelineStatistics(pipelineId.value)
    if (response.code === 200) {
      const data = response.data
      statistics.total_runs = data.total_runs || 0
      statistics.successful_runs = data.successful_runs || 0
      statistics.failed_runs = data.failed_runs || 0
      statistics.success_rate = data.success_rate || 0
      statistics.avg_duration = data.avg_duration || 0
    }
  } catch (error) {
    console.error('获取统计数据失败:', error)
  }
  
  // 模拟最近失败数据
  recentFailures.value = [
    { id: 1, build_number: 15, stage: '构建', error: 'npm install failed', created_at: new Date(Date.now() - 86400000).toISOString() },
    { id: 2, build_number: 12, stage: '测试', error: 'test failed: 2 cases', created_at: new Date(Date.now() - 172800000).toISOString() }
  ]
}

// 获取测试报告
const fetchTestReports = async () => {
  try {
    const response = await getPipelineTestReports(pipelineId.value)
    if (response.code === 200) {
      testReports.value = response.data.list || []
      
      // 计算统计数据
      reportStats.total = 0
      reportStats.passed = 0
      reportStats.failed = 0
      reportStats.skipped = 0
      
      testReports.value.forEach(report => {
        reportStats.total += report.total || 0
        reportStats.passed += report.passed || 0
        reportStats.failed += report.failed || 0
        reportStats.skipped += report.skipped || 0
      })
      
      if (reportStats.total > 0) {
        reportStats.passRate = Math.round((reportStats.passed / reportStats.total) * 100)
      }
    }
  } catch (error) {
    console.error('获取测试报告失败:', error)
  }
}

// 返回
const goBack = () => {
  router.push('/pipeline')
}

// 运行流水线
const handleRun = () => {
  runForm.branch = 'master'
  runForm.commit = ''
  runDialogVisible.value = true
}

// 确认运行
const confirmRun = async () => {
  runLoading.value = true
  try {
    const response = await runPipeline(pipelineId.value)
    if (response.code === 200) {
      ElMessage.success('流水线已开始运行')
      runDialogVisible.value = false

      // 获取最新的运行记录并切换到执行Tab
      await fetchRunHistory()
      if (runHistory.value.length > 0) {
        currentRun.value = runHistory.value[0]
        activeTab.value = 'execution'

        // 停止旧轮询，启动 WebSocket 实时更新
        stopExecutionPolling()
        startRealtimeUpdates(currentRun.value.id)

        // 初始加载任务列表
        await fetchExecutionDetail()
      }
    } else {
      // 显示服务器返回的错误消息
      ElMessage.error(response.message || '运行失败')
    }
  } catch (error) {
    console.error('运行流水线失败:', error)
    // 尝试从错误响应中提取消息
    const errorMessage = error.response?.data?.message || error.message || '运行失败'
    ElMessage.error(errorMessage)
  } finally {
    runLoading.value = false
  }
}

// 处理下拉菜单命令
const handleCommand = (command) => {
  switch (command) {
    case 'edit':
      ElMessage.info('编辑功能开发中')
      break
    case 'copy':
      ElMessage.info('复制功能开发中')
      break
    case 'delete':
      ElMessage.info('请在列表页删除')
      break
  }
}

// 查看运行详情
const viewRunDetail = (run) => {
  ElMessage.info(`查看构建 #${run.build_number} 详情`)
}

// 查看执行详情（切换到执行Tab）
const viewExecutionDetail = async (run) => {
  currentRun.value = run
  activeTab.value = 'execution'
  stopExecutionPolling()
  stopRealtimeUpdates()
  startRealtimeUpdates(run.id)
  await fetchExecutionDetail()
}

// 查看运行任务
const viewRunTasks = async (run) => {
  currentRun.value = run
  activeTab.value = 'execution'
  stopExecutionPolling()
  stopRealtimeUpdates()
  startRealtimeUpdates(run.id)
  await fetchExecutionDetail()
}

// 获取执行详情
const fetchExecutionDetail = async () => {
  if (!currentRun.value) return
  
  executionLoading.value = true
  try {
    // 获取运行详情
    const runResponse = await getPipelineDetail(pipelineId.value)
    if (runResponse.code === 200) {
      // 查找当前运行的记录
      const runs = runResponse.data.runs || []
      const found = runs.find(r => r.id === currentRun.value.id)
      if (found) {
        currentRun.value = found
      }
    }
    
    // 获取任务列表
    const tasksResponse = await getRunTasks(pipelineId.value, currentRun.value.id)
    if (tasksResponse.code === 200) {
      runTasks.value = tasksResponse.data.list || []
    }
    
    // 如果有选中的任务，获取任务日志
    if (selectedTask.value && showLogPanel.value) {
      await fetchTaskLogs(selectedTask.value)
    }
  } catch (error) {
    console.error('获取执行详情失败:', error)
  } finally {
    executionLoading.value = false
  }
}

// 获取任务日志
const fetchTaskLogs = async (task) => {
  try {
    const response = await getTaskLogs(task.id)
    if (response.code === 200) {
      taskLogs.value = response.data.list || []
      await nextTick()
      // 滚动到底部
      if (logContentRef.value) {
        logContentRef.value.scrollTop = logContentRef.value.scrollHeight
      }
    }
  } catch (error) {
    console.error('获取任务日志失败:', error)
  }
}

// 查看任务日志
const viewTaskLogs = async (task) => {
  selectedTask.value = task
  showLogPanel.value = true
  taskLogs.value = []
  await fetchTaskLogs(task)
}

// 停止执行
const stopExecution = () => {
  ElMessageBox.confirm('确定要停止当前执行吗？', '停止确认', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(() => {
    ElMessage.info('停止执行功能开发中')
  }).catch(() => {})
}

// 开始轮询执行状态（备用方案，当 WebSocket 不可用时）
const startExecutionPolling = () => {
  if (executionPollingTimer) return
  
  executionPollingTimer = setInterval(() => {
    if (activeTab.value === 'execution' && currentRun.value?.status === 'running') {
      fetchExecutionDetail()
    } else if (currentRun.value?.status !== 'running') {
      // 执行完成，停止轮询
      stopExecutionPolling()
    }
  }, 5000) // 5秒轮询
}

// 停止轮询
const stopExecutionPolling = () => {
  if (executionPollingTimer) {
    clearInterval(executionPollingTimer)
    executionPollingTimer = null
  }
}

// 停止轮询和 WebSocket
const stopAllUpdates = () => {
  stopExecutionPolling()
  stopRealtimeUpdates()
}

// 格式化日志时间
const formatLogTime = (timestamp) => {
  if (!timestamp) return ''
  return new Date(timestamp * 1000).toLocaleTimeString('zh-CN', { hour12: false })
}

// 获取任务日志API
const getTaskLogs = async (taskId, params = {}) => {
  try {
    const response = await fetchTaskLogsFromApi(taskId, params)
    return response
  } catch (error) {
    console.error('获取任务日志失败:', error)
    return { code: 500, message: '获取日志失败', data: { list: [] } }
  }
}

// 保存设置
const handleSaveSettings = async () => {
  try {
    await updatePipeline(pipelineId.value, settingsForm)
    ElMessage.success('设置已保存')
    fetchPipelineDetail()
  } catch (error) {
    console.error('保存设置失败:', error)
    ElMessage.error('保存失败')
  }
}

// 保存触发设置
const handleSaveTriggers = () => {
  ElMessage.success('触发设置已保存')
}

// 保存通知设置
const handleSaveNotifications = () => {
  ElMessage.success('通知设置已保存')
}

// 格式化
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
  return new Date(timestamp).toLocaleString('zh-CN')
}

const formatDuration = (seconds) => {
  if (!seconds || seconds < 0) return '-'
  // 如果值太大（超过1年），可能是毫秒，需要转换
  if (seconds > 31536000) {
    seconds = Math.floor(seconds / 1000)
  }
  const mins = Math.floor(seconds / 60)
  const secs = Math.floor(seconds % 60)
  if (mins > 0) {
    return `${mins}m ${secs}s`
  }
  return `${secs}s`
}

const getStatusType = (status) => {
  const types = {
    success: 'success',
    running: 'warning',
    failed: 'danger',
    pending: 'info',
    not_executed: 'info',
    blocked: 'danger'
  }
  return types[status] || 'info'
}

const getStatusText = (status) => {
  const texts = {
    success: '成功',
    running: '运行中',
    failed: '失败',
    pending: '等待',
    not_executed: '暂未执行',
    blocked: '已阻塞'
  }
  return texts[status] || status
}

const getEnvironmentType = (env) => {
  const types = {
    development: '',
    testing: 'warning',
    production: 'danger'
  }
  return types[env] || ''
}

const getEnvironmentText = (env) => {
  const texts = {
    development: '开发环境',
    testing: '测试环境',
    production: '生产环境'
  }
  return texts[env] || env
}

// 监听 Tab 切换
watch(activeTab, (newTab) => {
  if (newTab === 'history') {
    fetchRunHistory()
  } else if (newTab === 'statistics') {
    fetchStatistics()
  } else if (newTab === 'report') {
    fetchTestReports()
  }
})

// 设置 WebSocket 实时更新
const setupRealtimeUpdates = () => {
  // 监听任务状态更新
  realtime.on('task_status', (payload) => {
    if (!currentRun.value || payload.run_id !== currentRun.value.id) return
    
    // 更新任务状态
    const taskIndex = runTasks.value.findIndex(t => t.id === payload.task_id)
    if (taskIndex !== -1) {
      runTasks.value[taskIndex].status = payload.status
      runTasks.value[taskIndex].exit_code = payload.exit_code
      runTasks.value[taskIndex].error_msg = payload.error_msg
      runTasks.value[taskIndex].duration = payload.duration
      if (payload.agent_name) {
        runTasks.value[taskIndex].Agent = { name: payload.agent_name }
      }
    }
    
    // 如果有选中的任务，显示错误信息
    if (payload.status === 'failed' && selectedTask.value?.id === payload.task_id) {
      selectedTask.value.error_msg = payload.error_msg
    }
  })
  
  // 监听任务日志更新
  realtime.on('task_log', (payload) => {
    if (!currentRun.value || payload.run_id !== currentRun.value.id) return
    
    // 如果有选中的任务，显示日志
    if (selectedTask.value && payload.task_id === selectedTask.value.id) {
      taskLogs.value.push({
        level: payload.level,
        message: payload.message,
        source: payload.source,
        timestamp: payload.timestamp,
        line_number: payload.line_number
      })
      
      nextTick(() => {
        if (logContentRef.value) {
          logContentRef.value.scrollTop = logContentRef.value.scrollHeight
        }
      })
    }
  })
  
  // 监听流水线状态更新
  realtime.on('run_status', (payload) => {
    if (!currentRun.value || payload.run_id !== currentRun.value.id) return

    // 更新流水线状态
    currentRun.value.status = payload.status
    if (payload.error_msg) {
      currentRun.value.stage = payload.error_msg
    }

    // 更新耗时（如果提供了）
    if (payload.duration !== undefined && payload.duration !== null) {
      currentRun.value.duration = payload.duration
    }

    console.log(`Pipeline run ${payload.run_id} status updated to: ${payload.status}`)
  })
  
  // 连接断开时提示
  realtime.on('disconnected', () => {
    console.log('实时连接已断开')
  })
  
  // 重连成功提示
  realtime.on('connected', () => {
    console.log('实时连接已建立')
  })
}

// 开始实时更新
const startRealtimeUpdates = (runID) => {
  realtime.connect(runID)
}

// 停止实时更新
const stopRealtimeUpdates = () => {
  realtime.disconnect()
}

onMounted(() => {
  fetchPipelineDetail()
  fetchProjects()
  fetchRunHistory()
  setupRealtimeUpdates()
})

// 组件卸载时停止轮询和 WebSocket
onUnmounted(() => {
  stopExecutionPolling()
  stopRealtimeUpdates()
})
</script>

<style lang="scss" scoped>
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.pipeline-detail-container {
  min-height: 100%;
  background: #f5f7fa;
  
  .detail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 24px;
    background: white;
    border-bottom: 1px solid #e4e7ed;
    
    .header-left {
      display: flex;
      align-items: center;
      gap: 24px;
      
      .back-btn {
        display: flex;
        align-items: center;
        gap: 4px;
        color: #606266;
        cursor: pointer;
        padding: 8px 12px;
        border-radius: 4px;
        transition: background 0.3s;
        
        &:hover {
          background: #f5f7fa;
        }
      }
      
      .pipeline-info {
        display: flex;
        align-items: center;
        gap: 12px;
        
        .pipeline-icon {
          width: 48px;
          height: 48px;
          display: flex;
          align-items: center;
          justify-content: center;
          color: white;
          font-size: 20px;
          font-weight: 600;
          border-radius: 8px;
        }
        
        .pipeline-meta {
          .pipeline-name {
            font-size: 20px;
            font-weight: 600;
            color: #303133;
            margin: 0 0 8px;
          }
          
          .pipeline-tags {
            display: flex;
            align-items: center;
            gap: 12px;
            
            .create-time {
              color: #909399;
              font-size: 12px;
            }
          }
        }
      }
    }
    
    .header-right {
      display: flex;
      gap: 12px;
    }
  }
  
  .detail-tabs {
    display: flex;
    align-items: center;
    padding: 0 24px;
    background: white;
    border-bottom: 1px solid #e4e7ed;
    
    .tab-item {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 16px 20px;
      color: #606266;
      cursor: pointer;
      border-bottom: 2px solid transparent;
      transition: all 0.3s;
      
      &:hover {
        color: #409EFF;
      }
      
      &.active {
        color: #409EFF;
        border-bottom-color: #409EFF;
      }
      
      .tab-count {
        font-size: 12px;
        color: #909399;
        margin-left: 4px;
      }
    }
    
    .tab-expand {
      margin-left: auto;
      display: flex;
      align-items: center;
      gap: 4px;
      padding: 16px;
      color: #606266;
      cursor: pointer;
      
      &:hover {
        color: #409EFF;
      }
    }
  }
  
  .detail-content {
    padding: 24px;
    
    .tab-panel {
      background: white;
      border-radius: 8px;
      min-height: 500px;
      
      .panel-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 16px 20px;
        border-bottom: 1px solid #ebeef5;
        
        h3 {
          margin: 0;
          font-size: 16px;
          font-weight: 600;
          color: #303133;
        }
      }
    }
    
    // 设计面板
    .design-panel {
      min-height: calc(100vh - 280px);
      background: transparent;
      border-radius: 0;
      padding: 0;
    }
    
    // 历史面板
    .history-panel {
      .history-list {
        padding: 20px;
        
        .build-number {
          color: #409EFF;
          font-weight: 500;
        }
      }
    }
    
    // 报告面板
    .report-panel {
      .report-content {
        padding: 20px;
        
        .report-summary {
          margin-bottom: 20px;
          
          .summary-card {
            :deep(.el-card__body) {
              display: flex;
              justify-content: space-around;
            }
            
            .summary-item {
              text-align: center;
              
              .summary-value {
                display: block;
                font-size: 28px;
                font-weight: 600;
                color: #303133;
              }
              
              .summary-label {
                font-size: 12px;
                color: #909399;
              }
              
              &.success .summary-value { color: #67C23A; }
              &.danger .summary-value { color: #F56C6C; }
              &.warning .summary-value { color: #E6A23C; }
              &.info .summary-value { color: #409EFF; }
            }
          }
        }
        
        .text-success { color: #67C23A; }
        .text-danger { color: #F56C6C; }
      }
    }
    
    // 统计面板
    .statistics-panel {
      .statistics-content {
        padding: 20px;
        
        .stats-overview {
          margin-bottom: 20px;
          
          .stat-card {
            :deep(.el-statistic__number) {
              font-size: 32px;
            }
            
            .success-rate {
              font-size: 14px;
              color: #67C23A;
            }
          }
        }
        
        .stats-charts {
          margin-bottom: 20px;
          
          .chart-card {
            .chart-placeholder {
              height: 250px;
              display: flex;
              align-items: center;
              justify-content: center;
            }
          }
        }
        
        .build-number {
          color: #409EFF;
          font-weight: 500;
        }
      }
    }
    
    // 设置面板
    .settings-panel {
      .settings-content {
        padding: 20px;
        
        .settings-form {
          max-width: 600px;
          
          .form-tip {
            margin-left: 12px;
            font-size: 12px;
            color: #909399;
          }
        }
      }
    }
    
    // 执行面板
    .execution-panel {
      .execution-header-left {
        display: flex;
        align-items: center;
        gap: 16px;
        
        h3 {
          margin: 0;
        }
      }
      
      .execution-header-right {
        display: flex;
        align-items: center;
        gap: 12px;
      }
      
      .execution-content {
        padding: 20px;
        
        .execution-summary {
          display: flex;
          gap: 16px;
          margin-bottom: 20px;
          
          .summary-item {
            flex: 1;
            
            .summary-label {
              font-size: 12px;
              color: #909399;
              margin-bottom: 8px;
            }
            
            .summary-value {
              font-size: 16px;
              font-weight: 500;
              color: #303133;
            }
          }
        }
        
        .task-progress-card {
          margin-bottom: 20px;
          
          .card-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
          }
          
          .task-list {
            .task-item {
              display: flex;
              align-items: flex-start;
              padding: 16px;
              border-bottom: 1px solid #ebeef5;
              transition: background 0.3s;
              
              &:last-child {
                border-bottom: none;
              }
              
              &:hover {
                background: #f5f7fa;
              }
              
              &.task-running {
                background: #fdf6ec;
              }
              
              &.task-failed {
                background: #fef0f0;
              }
              
              &.task-not-executed {
                background: #f5f7fa;
                opacity: 0.7;
              }
              
              .task-status-icon {
                margin-right: 16px;
                font-size: 24px;
                
                .running-icon {
                  animation: spin 1s linear infinite;
                }
              }
              
              .task-info {
                flex: 1;
                
                .task-name {
                  font-size: 14px;
                  font-weight: 500;
                  color: #303133;
                  margin-bottom: 4px;
                }
                
                .task-meta {
                  font-size: 12px;
                  color: #909399;
                  
                  .task-agent {
                    margin-right: 16px;
                  }
                }
                
                .task-error {
                  margin-top: 8px;
                  padding: 8px 12px;
                  background: #fef0f0;
                  border-radius: 4px;
                  font-size: 12px;
                  color: #F56C6C;
                  display: flex;
                  align-items: center;
                  gap: 4px;
                }
              }
              
              .task-actions {
                margin-left: 16px;
              }
            }
          }
        }
        
        .execution-logs-card {
          .card-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
          }
          
          .log-panel {
            .log-content {
              max-height: 400px;
              overflow-y: auto;
              background: #1e1e1e;
              border-radius: 4px;
              padding: 12px;
              font-family: 'Consolas', 'Monaco', monospace;
              font-size: 12px;
              line-height: 1.6;
              
              .log-line {
                display: flex;
                margin-bottom: 2px;
                
                .log-time {
                  color: #858585;
                  margin-right: 12px;
                  white-space: nowrap;
                }
                
                .log-message {
                  color: #d4d4d4;
                  word-break: break-all;
                }
                
                &.log-info .log-message {
                  color: #d4d4d4;
                }
                
                &.log-warn .log-message {
                  color: #cca700;
                }
                
                &.log-error .log-message {
                  color: #f14c4c;
                }
                
                &.log-debug .log-message {
                  color: #6a9955;
                }
              }
            }
          }
        }
      }
      
      .running-tag {
        margin-left: 8px;
        animation: pulse 2s ease-in-out infinite;
      }
    }
  }
}
</style>
