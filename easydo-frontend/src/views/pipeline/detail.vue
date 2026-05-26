<template>
  <div ref="detailContainerRef" class="pipeline-detail-container">
    <!-- 头部信息 -->
    <div class="detail-header">
      <div class="header-left">
        <button type="button" class="back-btn back-btn--icon" @click="goBack" title="返回列表" aria-label="返回列表">
          <el-icon><ArrowLeft /></el-icon>
        </button>
        <div class="pipeline-info pipeline-info--compact">
          <div class="pipeline-icon">
            {{ pipeline?.name?.charAt(0)?.toUpperCase() || 'P' }}
          </div>
          <div class="pipeline-name">{{ pipeline?.name || '-' }}</div>
          <el-tag size="small" :type="getEnvironmentType(pipeline?.environment)" class="pipeline-env-tag">
            {{ getEnvironmentText(pipeline?.environment) }}
          </el-tag>
          <div class="pipeline-project-name">{{ currentProjectOption?.name || pipeline?.project?.name || '-' }}</div>
        </div>
        <div class="header-divider">|</div>
      </div>

      <div class="detail-tabs">
        <el-button
          class="header-tab-btn"
          type="primary"
          :plain="activeTab !== 'design'"
          @click="activeTab = 'design'"
        >
          <el-icon><Edit /></el-icon>
          <span>设计</span>
        </el-button>
        <el-button
          class="header-tab-btn"
          type="primary"
          :plain="activeTab !== 'history'"
          @click="activeTab = 'history'"
        >
          <el-icon><Clock /></el-icon>
          <span>历史</span>
          <span class="tab-count">{{ totalRuns }}</span>
        </el-button>
        <el-button
          v-if="currentRun"
          class="header-tab-btn"
          type="primary"
          :plain="activeTab !== 'execution'"
          @click="activeTab = 'execution'"
        >
          <el-icon><VideoPlay /></el-icon>
          <span>执行过程</span>
          <el-tag v-if="currentRun.status === 'running'" type="warning" size="small" class="running-tag">运行中</el-tag>
          <el-tag v-else-if="currentRun.status === 'queued'" type="info" size="small" class="running-tag">排队中</el-tag>
        </el-button>
        <el-button
          class="header-tab-btn"
          type="primary"
          :plain="activeTab !== 'statistics'"
          @click="activeTab = 'statistics'"
        >
          <el-icon><DataAnalysis /></el-icon>
          <span>统计</span>
        </el-button>
        <el-button
          class="header-tab-btn"
          type="primary"
          :plain="activeTab !== 'settings'"
          @click="activeTab = 'settings'"
        >
          <el-icon><Setting /></el-icon>
          <span>设置</span>
        </el-button>
        <el-button class="header-tab-btn header-tab-btn--run" type="primary" @click="handleRun">
          <el-icon><VideoPlay /></el-icon>
          运行流水线
        </el-button>
      </div>
    </div>
    
    <!-- Tab 内容 -->
    <div class="detail-content">
      <!-- 设计 Tab -->
      <div v-show="activeTab === 'design'" class="tab-panel design-panel">
        <design-tab :pipeline-id="pipelineId" @saved="fetchPipelineDetail" />
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
            <el-table-column label="触发方式" width="120">
              <template #default="{ row }">
                {{ getRunTriggerType(row) }}
              </template>
            </el-table-column>
            <el-table-column label="触发人" width="100">
              <template #default="{ row }">
                {{ getRunTriggerUser(row) }}
              </template>
            </el-table-column>
            <el-table-column label="分支" min-width="150">
              <template #default="{ row }">
                {{ getRunBranch(row) }}
              </template>
            </el-table-column>
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
            <el-table-column label="操作" min-width="280" fixed="right">
              <template #default="{ row }">
                <el-button type="primary" link size="small" @click="viewExecutionDetail(row)">
                  查看执行
                </el-button>
                <el-button type="primary" link size="small" @click="viewRunParameters(row)">
                  查看参数
                </el-button>
                <el-button type="primary" link size="small" @click="handleRerun(row)">
                  重新运行
                </el-button>
                <el-button type="primary" link size="small" @click="viewRunLogs(row)">
                  运行日志
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
            <el-button v-if="currentRun?.id" @click="viewRunParameters(currentRun)">
              查看本次参数
            </el-button>
            <el-button v-if="currentRun?.id" @click="handleRerun(currentRun)">
              按本次参数重新运行
            </el-button>
            <el-button v-if="currentRun?.id" @click="viewRunLogs(currentRun)">
              运行日志
            </el-button>
            <el-button v-if="['queued', 'pending', 'running'].includes(currentRun?.status)" type="danger" @click="stopExecution">
              停止执行
            </el-button>
          </div>
        </div>
        
        <div class="execution-content">
          <!-- 执行概览 -->
          <div class="execution-summary">
            <el-card class="summary-item">
              <div class="summary-label">触发方式</div>
              <div class="summary-value">{{ getRunTriggerType(currentRun) }}</div>
            </el-card>
            <el-card class="summary-item">
              <div class="summary-label">触发人</div>
              <div class="summary-value">{{ getRunTriggerUser(currentRun) }}</div>
            </el-card>
            <el-card class="summary-item">
              <div class="summary-label">开始时间</div>
              <div class="summary-value">{{ formatDateTime(getRunStartTime(currentRun)) }}</div>
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
                v-for="task in sortedRunTasks"
                :key="task.id ? `task-${task.id}` : `node-${task.node_id || task.NodeID || task.name || 'unknown'}`"
                class="task-item"
                :class="{ 
                  'task-running': task.status === 'running', 
                  'task-failed': ['execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired'].includes(task.status),
                  'task-blocked': task.display_status === 'blocked',
                  'task-not-executed': task.display_status === 'not_executed'
                }"
              >
                <div class="task-status-icon">
                  <el-icon v-if="['queued', 'assigned', 'dispatching', 'pulling', 'acked'].includes(task.status) || task.display_status === 'not_executed'" :color="'var(--text-muted)'"><Clock /></el-icon>
                  <el-icon v-else-if="task.status === 'running'" color="#E6A23C" class="running-icon"><Loading /></el-icon>
                  <el-icon v-else-if="task.status === 'execute_success'" color="#67C23A"><SuccessFilled /></el-icon>
                  <el-icon v-else-if="task.status === 'cancelled'" :color="'var(--text-secondary)'"><CircleCloseFilled /></el-icon>
                  <el-icon v-else-if="task.display_status === 'blocked'" :color="'var(--warning-color)'"><Warning /></el-icon>
                  <el-icon v-else-if="['execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired'].includes(task.status)" :color="'var(--danger-color)'"><CircleCloseFilled /></el-icon>
                </div>
                <div class="task-body">
                  <div class="task-main-line">
                    <span class="task-title-text">{{ task.name || `任务 #${task.id}` }}</span>
                    <span v-if="isIgnoredFailureTask(task)" class="task-ignored-hint">已忽略执行失败</span>
                    <div class="task-inline-meta">
                      <el-tag v-if="task.display_status === 'not_executed'" type="info" size="small">暂未执行</el-tag>
                      <el-tag v-else-if="task.display_status === 'blocked'" type="warning" size="small">已阻塞</el-tag>
                      <span class="task-agent" v-if="task.Agent">{{ task.Agent.name }}</span>
                      <span class="task-runtime" v-if="task.runtime_summary">{{ task.runtime_summary }}</span>
                      <span class="task-start-time">开始: {{ formatDateTime(task.start_time) }}</span>
                      <span class="task-duration" v-if="task.duration > 0">耗时: {{ formatDuration(task.duration) }}</span>
                      <span class="task-exit-code" v-if="shouldShowTaskExitCode(task)">退出码: {{ getTaskExitCode(task) }}</span>
                    </div>
                  </div>
                  <div class="task-error" v-if="task.error_msg">
                    <el-icon><Warning /></el-icon>
                    {{ task.error_msg }}
                  </div>
                  <div
                    v-if="getFormattedTaskOutputs(task).length > 0"
                    class="task-inline-outputs"
                  >
                    <div class="task-outputs task-outputs--expanded">
                      <div class="task-outputs-grid">
                        <div
                          v-for="(output, idx) in getFormattedTaskOutputs(task)"
                          :key="idx"
                          class="task-output-item"
                        >
                          <span class="output-label">{{ output.label }}</span>
                          <span class="output-value" :class="`output-${output.type}`">{{ output.value }}</span>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="task-actions">
                  <el-button 
                    type="primary" 
                    link 
                    size="small" 
                    @click="viewTaskLogs(task)"
                    :disabled="['queued', 'assigned', 'dispatching', 'pulling'].includes(task.status) || task.display_status === 'not_executed' || task.display_status === 'blocked'"
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
                <div>
                  <el-button type="primary" link size="small" @click="downloadTaskLogs">
                    下载
                  </el-button>
                  <el-button type="primary" link size="small" @click="showLogPanel = false">
                    <el-icon><Close /></el-icon>
                    关闭
                  </el-button>
                </div>
              </div>
            </template>
            
            <div class="log-panel">
              <div class="log-content" ref="logContentRef">
                <div 
                  v-for="(log, index) in taskLogs" 
                  :key="index"
                  class="log-line"
                  :class="[`log-${log.level}`, getLogSemanticClass(log.message)]"
                >
                  <span class="log-time">{{ formatLogTime(log.timestamp) }}</span>
                  <span class="log-message">{{ log.message }}</span>
                </div>
              </div>
            </div>
          </el-card>
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
                  <span>运行趋势</span>
                </template>
                <div class="chart-area">
                  <div v-if="statisticsTrend.length > 0" class="bar-chart">
                    <div
                      v-for="(day, index) in statisticsTrend"
                      :key="`${day.date}-${index}`"
                      class="bar-item"
                    >
                      <div class="bar-container">
                        <div class="bar-stack" :style="{ height: `${getTrendStackHeight(day)}%` }">
                          <div
                            v-if="day.failed > 0"
                            class="bar-segment failed"
                            :style="{ height: `${(day.failed / day.total) * 100}%` }"
                          >
                            <span class="bar-value">{{ day.failed }}</span>
                          </div>
                          <div
                            v-if="day.success > 0"
                            class="bar-segment success"
                            :style="{ height: `${(day.success / day.total) * 100}%` }"
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
                  <el-empty v-else description="暂无趋势数据" :image-size="100" />
                  <div v-if="statisticsTrend.length > 0" class="trend-summary">
                    <span class="trend-total">总计: {{ statisticsTrendTotalCount }} 次运行</span>
                    <span class="trend-success">成功: {{ statisticsTrendSuccessCount }}</span>
                    <span class="trend-failed">失败: {{ statisticsTrendFailedCount }}</span>
                  </div>
                </div>
              </el-card>
            </el-col>
            <el-col :span="12">
              <el-card class="chart-card">
                <template #header>
                  <span>状态分布</span>
                </template>
                <div class="chart-area chart-area--distribution">
                  <div v-if="statisticsDistribution.length > 0" class="pie-container">
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
                      <span class="pie-percentage">{{ statistics.success_rate }}%</span>
                      <span class="pie-label">成功率</span>
                    </div>
                  </div>
                  <el-empty v-else description="暂无分布数据" :image-size="100" />
                  <div v-if="statisticsDistribution.length > 0" class="pie-legend pie-legend--stacked">
                    <div v-for="item in statisticsDistribution" :key="item.status" class="legend-item">
                      <span class="legend-dot" :class="item.status === 'success' ? 'success' : 'failed'"></span>
                      <span>{{ getDistributionStatusLabel(item.status) }}: {{ item.count }} 次 ({{ item.rate }}%)</span>
                    </div>
                  </div>
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
              <el-table-column prop="status" label="状态" width="120">
                <template #default="{ row }">
                  {{ getStatusText(row.status) }}
                </template>
              </el-table-column>
              <el-table-column prop="error_msg" label="错误信息" min-width="200" show-overflow-tooltip />
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
                  <el-select
                    v-model="settingsForm.project_id"
                    placeholder="请选择项目"
                    style="width: 100%"
                    filterable
                    remote
                    reserve-keyword
                    :remote-method="handleProjectSearch"
                    :loading="projectQuery.loading"
                    popper-class="pipeline-project-select-dropdown"
                    @visible-change="handleProjectDropdownVisibleChange"
                  >
                    <el-option
                      v-for="project in projectOptions"
                      :key="project.id"
                      :label="project.name"
                      :value="normalizeProjectSelectionValue(project.id)"
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
              <el-form label-width="120px" class="settings-form trigger-settings-form" v-loading="triggerSettingsLoading">
                <div class="trigger-sections">
                  <section class="trigger-section">
                    <div class="trigger-section__header">
                      <div class="trigger-section__heading">
                        <div class="trigger-section__title-row">
                          <h4>定时触发</h4>
                          <el-tag size="small" :type="triggerSettings.schedule_enabled ? 'success' : 'info'">
                            {{ triggerSettings.schedule_enabled ? '已启用' : '未启用' }}
                          </el-tag>
                        </div>
                        <p>优先使用常用预设安排执行计划，只有复杂场景时才切换到 Cron 高级模式。</p>
                      </div>
                      <el-switch v-model="triggerSettings.schedule_enabled" />
                    </div>

                    <div v-if="triggerSettings.schedule_enabled" class="trigger-section__body">
                      <el-form-item label="执行方式">
                        <el-radio-group v-model="scheduleBuilder.mode" class="schedule-mode-group" @change="handleScheduleModeChange">
                          <el-radio-button label="every_minutes">每隔 N 分钟</el-radio-button>
                          <el-radio-button label="daily">每天</el-radio-button>
                          <el-radio-button label="weekly">每周</el-radio-button>
                          <el-radio-button label="custom">自定义 Cron</el-radio-button>
                        </el-radio-group>
                      </el-form-item>

                      <el-form-item v-if="scheduleBuilder.mode === 'every_minutes'" label="执行间隔">
                        <div class="trigger-inline-field trigger-inline-field--compact">
                          <el-input-number
                            v-model="scheduleBuilder.everyMinutes"
                            :min="1"
                            :max="1440"
                            controls-position="right"
                          />
                          <span class="form-tip form-tip--inline">分钟</span>
                        </div>
                      </el-form-item>

                      <el-form-item v-else-if="scheduleBuilder.mode === 'daily'" label="执行时间">
                        <el-time-picker
                          v-model="scheduleBuilder.dailyTime"
                          format="HH:mm"
                          value-format="HH:mm"
                          placeholder="请选择每天执行时间"
                        />
                      </el-form-item>

                      <el-form-item v-else-if="scheduleBuilder.mode === 'weekly'" label="执行时间">
                        <div class="trigger-inline-field trigger-inline-field--wrap">
                          <el-select v-model="scheduleBuilder.weeklyDay" placeholder="请选择星期" style="width: 160px">
                            <el-option
                              v-for="item in scheduleWeekdayOptions"
                              :key="item.value"
                              :label="item.label"
                              :value="item.value"
                            />
                          </el-select>
                          <el-time-picker
                            v-model="scheduleBuilder.weeklyTime"
                            format="HH:mm"
                            value-format="HH:mm"
                            placeholder="请选择每周执行时间"
                          />
                        </div>
                      </el-form-item>

                      <el-form-item v-else label="Cron 表达式">
                        <div class="trigger-advanced-box trigger-advanced-box--visible">
                          <el-input v-model="scheduleBuilder.customCron" placeholder="0 0 * * *" />
                          <span class="form-tip form-tip--block">无法识别为常用预设的规则会保留在高级模式中。</span>
                        </div>
                      </el-form-item>

                      <el-form-item label="时区">
                        <el-input v-model="triggerSettings.timezone" placeholder="Asia/Shanghai" />
                      </el-form-item>

                      <el-form-item v-if="scheduleBuilder.mode !== 'custom'" label="Cron 预览">
                        <div class="trigger-advanced-box">
                          <div class="trigger-advanced-box__header">
                            <span>当前预设会自动生成对应的 Cron 表达式。</span>
                            <el-button link type="primary" @click="scheduleBuilder.advancedMode = !scheduleBuilder.advancedMode">
                              {{ scheduleBuilder.advancedMode ? '隐藏 Cron' : '查看 Cron' }}
                            </el-button>
                          </div>
                          <el-input v-if="scheduleBuilder.advancedMode" :model-value="scheduleCronPreview || '-'" readonly />
                        </div>
                      </el-form-item>
                    </div>

                    <div class="trigger-meta-grid">
                      <div class="trigger-meta-card">
                        <span class="trigger-meta-label">下次运行</span>
                        <span class="trigger-meta-value">{{ formatDateTime(triggerSettings.next_run_at) }}</span>
                      </div>
                      <div class="trigger-meta-card">
                        <span class="trigger-meta-label">最近运行</span>
                        <span class="trigger-meta-value">{{ formatDateTime(triggerSettings.last_run_at) }}</span>
                      </div>
                    </div>
                  </section>

                  <section class="trigger-section">
                    <div class="trigger-section__header">
                      <div class="trigger-section__heading">
                        <div class="trigger-section__title-row">
                          <h4>Webhook 触发</h4>
                          <el-tag size="small" :type="webhookSectionEnabled ? 'success' : 'info'">
                            {{ webhookSectionEnabled ? '已启用' : '未启用' }}
                          </el-tag>
                        </div>
                        <p>按事件类型控制自动触发，并为 Push、Tag、Merge Request 配置对应的过滤条件。</p>
                      </div>
                    </div>

                    <div class="trigger-section__body">
                      <div class="webhook-event-list">
                        <div class="webhook-event-card">
                          <div class="webhook-event-card__header">
                            <div>
                              <div class="webhook-event-card__title">Push 事件</div>
                              <p>当代码推送到仓库时自动触发流水线。</p>
                            </div>
                            <el-switch v-model="triggerSettings.push_enabled" />
                          </div>
                          <el-form-item label="分支过滤" class="trigger-nested-item">
                            <el-input
                              v-model="triggerSettings.push_branch_filters"
                              type="textarea"
                              :rows="2"
                              placeholder="示例：main, release/*，支持换行或逗号分隔"
                            />
                          </el-form-item>
                        </div>

                        <div class="webhook-event-card">
                          <div class="webhook-event-card__header">
                            <div>
                              <div class="webhook-event-card__title">Tag 事件</div>
                              <p>当创建或更新标签时自动触发流水线。</p>
                            </div>
                            <el-switch v-model="triggerSettings.tag_enabled" />
                          </div>
                          <el-form-item label="标签过滤" class="trigger-nested-item">
                            <el-input
                              v-model="triggerSettings.tag_filters"
                              type="textarea"
                              :rows="2"
                              placeholder="示例：v*，支持换行或逗号分隔"
                            />
                          </el-form-item>
                        </div>

                        <div class="webhook-event-card">
                          <div class="webhook-event-card__header">
                            <div>
                              <div class="webhook-event-card__title">Merge Request 事件</div>
                              <p>当 Merge Request 发生变化时自动触发流水线。</p>
                            </div>
                            <el-switch v-model="triggerSettings.merge_request_enabled" />
                          </div>
                          <el-form-item label="源分支过滤" class="trigger-nested-item">
                            <el-input
                              v-model="triggerSettings.merge_request_source_branch_filters"
                              type="textarea"
                              :rows="2"
                              placeholder="示例：feature/*，支持换行或逗号分隔"
                            />
                          </el-form-item>
                          <el-form-item label="目标分支过滤" class="trigger-nested-item">
                            <el-input
                              v-model="triggerSettings.merge_request_target_branch_filters"
                              type="textarea"
                              :rows="2"
                              placeholder="示例：main, release/*，支持换行或逗号分隔"
                            />
                          </el-form-item>
                        </div>
                      </div>

                      <div class="webhook-runtime-config">
                        <div class="webhook-runtime-config__header">
                          <div>
                            <div class="webhook-runtime-config__title">运行时输入映射</div>
                            <p>按 JSONPath 从 Webhook Payload 提取值，并写入当前流水线允许在运行时覆盖的参数；未映射的参数继续使用节点默认值。</p>
                          </div>
                        </div>

                        <el-alert
                          v-if="webhookRuntimeConfigStatusText"
                          :title="webhookRuntimeConfigStatusText"
                          :type="webhookRuntimeConfigStatusType"
                          :closable="false"
                          show-icon
                        >
                          <template #default>
                            <div>{{ triggerSettings.webhook_config_invalid_reason || '当前保存的配置可正常用于 Webhook 触发。' }}</div>
                          </template>
                        </el-alert>

                        <div v-if="webhookRuntimeTargets.length === 0" class="webhook-runtime-empty">
                          当前流水线里还没有可用于 Webhook 运行时映射的参数。
                        </div>
                        <div v-else class="webhook-runtime-table">
                          <div
                            v-for="(row, index) in webhookRuntimeMappingRows"
                            :key="row.id || `mapping-${index}`"
                            :class="['webhook-runtime-row-card', { 'is-deleted': row.deleted }]"
                          >
                            <div class="webhook-runtime-row">
                              <div class="webhook-runtime-row__index">{{ index + 1 }}</div>
                              <div class="webhook-runtime-row__target-card">
                                <div class="webhook-runtime-row__target-label">{{ getWebhookRuntimeTargetLabel(row) }}</div>
                                <div class="webhook-runtime-row__target-key">{{ getWebhookRuntimeMappingTargetKey(row) }}</div>
                              </div>
                              <el-input
                                v-model="row.source_expr"
                                :disabled="row.deleted"
                                placeholder="JSONPath，例如 $.ref 或 $.object_attributes.source_branch"
                                class="webhook-runtime-row__expr"
                              />
                              <el-select v-model="row.missing_policy" :disabled="row.deleted" class="webhook-runtime-row__policy">
                                <el-option label="缺失时忽略" value="ignore" />
                                <el-option label="缺失时报错" value="fail" />
                              </el-select>
                              <el-button
                                text
                                :type="row.deleted ? 'primary' : 'danger'"
                                @click="toggleWebhookRuntimeMappingDeleted(row, !row.deleted)"
                              >
                                {{ row.deleted ? '恢复' : '删除' }}
                              </el-button>
                            </div>
                            <div v-if="getWebhookRuntimeMappingRowErrors(row).length > 0" class="webhook-runtime-row__errors">
                              <div v-for="(item, errorIndex) in getWebhookRuntimeMappingRowErrors(row)" :key="`${row.id || index}-error-${errorIndex}`">
                                {{ item }}
                              </div>
                            </div>
                          </div>
                        </div>

                        <div v-if="webhookRuntimeTargets.length > 0" class="webhook-runtime-config__footer-actions">
                          <el-button @click="applyGitlabWebhookRuntimePreset">一键填充 GitLab 常用映射</el-button>
                        </div>

                        <div class="webhook-runtime-preview" v-loading="webhookRuntimePreviewLoading">
                          <div class="webhook-runtime-preview__header">
                            <div>
                              <div class="webhook-runtime-config__title">示例 Payload 预览</div>
                              <p>保存前可先验证映射结果、缺失项和类型问题。</p>
                            </div>
                            <div class="webhook-runtime-preview__actions">
                              <el-button text @click="webhookRuntimePreviewExpanded = !webhookRuntimePreviewExpanded">
                                {{ webhookRuntimePreviewExpanded ? '收起预览' : '展开预览' }}
                              </el-button>
                              <el-button
                                v-if="webhookRuntimePreviewExpanded"
                                type="primary"
                                plain
                                :disabled="activeWebhookRuntimeMappingRows.length === 0 || hasInvalidActiveWebhookRuntimeMapping"
                                @click="handlePreviewWebhookRuntimeMappings"
                              >
                                运行预览
                              </el-button>
                            </div>
                          </div>

                          <template v-if="webhookRuntimePreviewExpanded">
                            <el-input
                              v-model="webhookRuntimeSamplePayload"
                              type="textarea"
                              :rows="8"
                              placeholder='示例：{"ref":"refs/heads/main"}'
                            />

                            <el-alert
                              v-if="webhookRuntimePreviewRequestError"
                              :title="webhookRuntimePreviewRequestError"
                              type="warning"
                              :closable="false"
                              show-icon
                            />

                            <div v-if="webhookRuntimePreviewSummary.total > 0" class="webhook-runtime-preview__summary">
                              <div class="trigger-meta-card">
                                <span class="trigger-meta-label">总规则</span>
                                <span class="trigger-meta-value">{{ webhookRuntimePreviewSummary.total }}</span>
                              </div>
                              <div class="trigger-meta-card">
                                <span class="trigger-meta-label">命中</span>
                                <span class="trigger-meta-value">{{ webhookRuntimePreviewSummary.matched }}</span>
                              </div>
                              <div class="trigger-meta-card">
                                <span class="trigger-meta-label">缺失</span>
                                <span class="trigger-meta-value">{{ webhookRuntimePreviewSummary.missing }}</span>
                              </div>
                              <div class="trigger-meta-card">
                                <span class="trigger-meta-label">失败</span>
                                <span class="trigger-meta-value">{{ webhookRuntimePreviewSummary.failed }}</span>
                              </div>
                            </div>

                            <div v-if="webhookRuntimePreviewRows.length > 0" class="webhook-runtime-preview__results">
                              <div
                                v-for="row in webhookRuntimePreviewRows"
                                :key="`preview-${row.id}`"
                                class="webhook-runtime-preview__row"
                              >
                                <div class="webhook-runtime-preview__row-head">
                                  <span>{{ row.target_label }}</span>
                                  <el-tag size="small" :type="row.result_code === 'matched' ? 'success' : row.result_code ? 'warning' : 'info'">
                                    {{ row.result_code || '未预览' }}
                                  </el-tag>
                                </div>
                                <div class="webhook-runtime-preview__row-item">
                                  <span>JSONPath</span>
                                  <span>{{ row.source_expr || '-' }}</span>
                                </div>
                                <div class="webhook-runtime-preview__row-item">
                                  <span>缺失策略</span>
                                  <span>{{ row.missing_policy === 'fail' ? '缺失时报错' : '缺失时忽略' }}</span>
                                </div>
                                <div class="webhook-runtime-preview__row-item">
                                  <span>预览值</span>
                                  <span>{{ formatTaskOutputValue(row.preview_value) || '-' }}</span>
                                </div>
                                <div v-if="row.errors.length > 0" class="webhook-runtime-preview__errors">
                                  <div v-for="(item, errorIndex) in row.errors" :key="`${row.id}-error-${errorIndex}`">
                                    {{ item.message || item.code || '预览失败' }}
                                  </div>
                                </div>
                              </div>
                            </div>
                          </template>
                        </div>
                      </div>

                      <el-form-item label="Webhook URL">
                        <div class="trigger-inline-field">
                          <el-input :model-value="displayedWebhookURL" readonly />
                          <el-button :disabled="!canCopyWebhookURL" @click="copyTriggerField(displayedWebhookURL)">
                            复制
                          </el-button>
                        </div>
                      </el-form-item>
                      <el-form-item label="Secret Token">
                        <div class="trigger-inline-field">
                          <el-input :model-value="displaySecretToken" readonly />
                          <el-button :disabled="!canCopySecretToken" @click="copyTriggerField(displaySecretToken)">
                            复制
                          </el-button>
                          <el-button :loading="triggerSettingsSaving" @click="handleRotateTriggerSecret">
                            轮换
                          </el-button>
                        </div>
                      </el-form-item>

                      <div class="trigger-meta-grid">
                        <div class="trigger-meta-card">
                          <span class="trigger-meta-label">最近触发</span>
                          <span class="trigger-meta-value">{{ formatDateTime(triggerSettings.last_triggered_at) }}</span>
                        </div>
                      </div>
                    </div>
                  </section>

                  <section class="trigger-section trigger-section--compact">
                    <el-form-item label="手动触发">
                      <el-switch v-model="triggerSettings.manual" disabled />
                      <span class="form-tip">始终允许手动触发</span>
                    </el-form-item>
                    <el-form-item>
                      <el-button type="primary" :loading="triggerSettingsSaving" :disabled="hasInvalidActiveWebhookRuntimeMapping" @click="handleSaveTriggers">保存触发设置</el-button>
                    </el-form-item>
                  </section>
                </div>
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
    
    <el-drawer
      v-model="parameterDrawerVisible"
      title="历史参数"
      size="720px"
      @close="closeParameterDrawer"
    >
      <div class="parameter-drawer" v-loading="parameterDrawerLoading">
        <div class="parameter-drawer__meta" v-if="parameterDrawerRun">
          <span>构建 #{{ parameterDrawerRun.build_number || '-' }}</span>
          <span>{{ formatDateTime(getRunStartTime(parameterDrawerRun)) }}</span>
        </div>
        <div v-if="parameterDrawerNodes.length > 0" class="parameter-node-list">
          <el-card
            v-for="node in parameterDrawerNodes"
            :key="node.node_id"
            class="parameter-node-card"
            shadow="never"
          >
            <template #header>
              <div class="parameter-node-card__header">
                <span class="parameter-node-card__title">{{ node.node_name }}</span>
                <span class="parameter-node-card__id">{{ node.node_id }}</span>
              </div>
            </template>
            <div class="parameter-sections">
              <div class="parameter-section">
                <div class="parameter-section__title">运行时参数</div>
                <div v-if="Object.keys(node.runtime_params).length > 0" class="parameter-kv-list">
                  <div v-for="(value, key) in node.runtime_params" :key="`${node.node_id}-runtime-${key}`" class="parameter-kv-item">
                    <span class="parameter-kv-item__key">{{ key }}</span>
                    <span class="parameter-kv-item__value">{{ formatTaskOutputValue(value) }}</span>
                  </div>
                </div>
                <el-empty v-else :image-size="56" description="该节点无运行时参数" />
              </div>
              <div class="parameter-section">
                <div class="parameter-section__title">默认参数</div>
                <div v-if="Object.keys(node.default_params).length > 0" class="parameter-kv-list">
                  <div v-for="(value, key) in node.default_params" :key="`${node.node_id}-default-${key}`" class="parameter-kv-item">
                    <span class="parameter-kv-item__key">{{ key }}</span>
                    <span class="parameter-kv-item__value">{{ formatTaskOutputValue(value) }}</span>
                  </div>
                </div>
                <el-empty v-else :image-size="56" description="该节点无默认参数" />
              </div>
            </div>
          </el-card>
        </div>
        <el-empty v-else-if="!parameterDrawerLoading" description="当前执行未返回可展示的历史参数" />
      </div>
    </el-drawer>

    <el-dialog
      v-model="rerunPreviewVisible"
      title="重新运行预览"
      width="720px"
      @close="closeRerunPreviewDialog"
    >
      <div class="rerun-preview" v-loading="rerunPreviewLoading">
        <div class="rerun-preview__meta" v-if="rerunSourceRun">
          <span>构建 #{{ rerunSourceRun.build_number || '-' }}</span>
          <span>{{ rerunPreviewSummaryText }}</span>
        </div>
        <template v-if="rerunPreviewData.failure">
          <el-alert
            type="error"
            :closable="false"
            :title="rerunPreviewFailureTitle"
            show-icon
          />
          <div class="rerun-preview__section">
            <div class="rerun-preview__section-title">失败详情</div>
            <div class="rerun-preview__detail-list">
              <div v-if="rerunPreviewData.failure.code" class="rerun-preview__detail-item">
                <span class="rerun-preview__detail-label">错误码</span>
                <span class="rerun-preview__detail-value">{{ rerunPreviewData.failure.code }}</span>
              </div>
              <div class="rerun-preview__detail-item">
                <span class="rerun-preview__detail-label">说明</span>
                <span class="rerun-preview__detail-value">{{ rerunPreviewFailureTitle }}</span>
              </div>
            </div>
          </div>
        </template>
        <template v-else>
          <el-alert
            :type="rerunPreviewBlocked ? 'warning' : 'success'"
            :closable="false"
            :title="rerunPreviewBlocked ? '当前历史参数与最新流水线定义不兼容，无法进入运行对话框。' : '可按历史参数进入运行对话框。'"
            show-icon
          />
          <div v-if="rerunPreviewMatchedItems.length > 0" class="rerun-preview__section">
            <div class="rerun-preview__section-title">已匹配参数</div>
            <div class="rerun-preview__detail-list">
              <div v-for="(item, index) in rerunPreviewMatchedItems" :key="`matched-${index}`" class="rerun-preview__detail-card">
                <div class="rerun-preview__detail-head">
                  <span class="rerun-preview__detail-title">{{ item.node_name || item.node_id || '未命名节点' }}</span>
                  <span class="rerun-preview__detail-subtitle">{{ item.param_label || item.param_key || '-' }}</span>
                </div>
                <div class="rerun-preview__detail-item">
                  <span class="rerun-preview__detail-label">匹配键</span>
                  <span class="rerun-preview__detail-value">{{ item.match_key || '-' }}</span>
                </div>
                <div class="rerun-preview__detail-item">
                  <span class="rerun-preview__detail-label">历史值</span>
                  <span class="rerun-preview__detail-value">{{ formatTaskOutputValue(item.historical_value) || '-' }}</span>
                </div>
                <div class="rerun-preview__detail-item" v-if="item.current_value !== undefined">
                  <span class="rerun-preview__detail-label">当前值</span>
                  <span class="rerun-preview__detail-value">{{ formatTaskOutputValue(item.current_value) || '-' }}</span>
                </div>
              </div>
            </div>
          </div>
          <div v-if="rerunPreviewMismatchedItems.length > 0" class="rerun-preview__section">
            <div class="rerun-preview__section-title">不匹配项</div>
            <div class="rerun-preview__detail-list">
              <div v-for="(item, index) in rerunPreviewMismatchedItems" :key="`mismatch-${index}`" class="rerun-preview__detail-card rerun-preview__detail-card--warning">
                <div class="rerun-preview__detail-head">
                  <span class="rerun-preview__detail-title">{{ item.node_name || item.node_id || '未命名节点' }}</span>
                  <span class="rerun-preview__detail-subtitle">{{ item.param_label || item.param_key || '-' }}</span>
                </div>
                <div class="rerun-preview__detail-item">
                  <span class="rerun-preview__detail-label">匹配键</span>
                  <span class="rerun-preview__detail-value">{{ item.match_key || '-' }}</span>
                </div>
                <div class="rerun-preview__detail-item">
                  <span class="rerun-preview__detail-label">失配原因</span>
                  <span class="rerun-preview__detail-value">{{ item.reason || '-' }}</span>
                </div>
                <div class="rerun-preview__detail-item">
                  <span class="rerun-preview__detail-label">历史值</span>
                  <span class="rerun-preview__detail-value">{{ formatTaskOutputValue(item.historical_value) || '-' }}</span>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
      <template #footer>
        <el-button @click="closeRerunPreviewDialog">关闭</el-button>
        <el-button v-if="!rerunPreviewBlocked" type="primary" :loading="rerunPreviewLoading" @click="openRunDialogFromRerunPreview">继续</el-button>
      </template>
    </el-dialog>

    <!-- 运行确认对话框 -->
    <el-dialog
      v-model="runDialogVisible"
      title="运行流水线"
      :width="runDialogWidth"
      @close="closeRunDialog"
    >
      <div v-if="runDialogPrefillSource" class="manual-run-prefill-hint">
        已预填构建 #{{ runDialogPrefillSource.build_number || '-' }} 的历史参数，请确认后运行。
      </div>
      <div v-if="manualRunNodes.length === 0" class="manual-run-empty">
        当前流水线未配置可手动覆盖参数，将按定义直接运行。
      </div>
      <div v-else class="manual-run-sections">
        <div
          v-for="node in manualRunNodes"
          :key="node.node_id"
          class="manual-run-node"
        >
          <div class="manual-run-node-title">{{ node.node_name }}（{{ node.node_id }}）</div>
          <el-form :model="runForm.inputs[node.node_id]" label-width="120px">
            <el-form-item
              v-for="param in node.params"
              :key="`${node.node_id}-${param.key}`"
              :label="param.label || param.key"
            >
              <el-switch
                v-if="param.input_type === 'boolean'"
                v-model="runForm.inputs[node.node_id][param.key]"
              />
              <el-input-number
                v-else-if="param.input_type === 'number'"
                v-model="runForm.inputs[node.node_id][param.key]"
                style="width: 100%"
              />
              <el-checkbox-group
                v-else-if="param.input_type === 'checkbox_group'"
                v-model="runForm.inputs[node.node_id][param.key]"
              >
                <el-checkbox
                  v-for="option in param.options || []"
                  :key="`${node.node_id}-${param.key}-${option.value}`"
                  :label="option.value"
                >
                  {{ option.label }}
                </el-checkbox>
              </el-checkbox-group>
              <el-select
                v-else-if="param.input_type === 'select'"
                v-model="runForm.inputs[node.node_id][param.key]"
                :placeholder="param.placeholder || '请选择运行时覆盖值'"
                style="width: 100%"
              >
                <el-option
                  v-for="option in param.options || []"
                  :key="`${node.node_id}-${param.key}-${option.value}`"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
              <el-input
                v-else
                v-model="runForm.inputs[node.node_id][param.key]"
                :placeholder="param.placeholder || '请输入运行时覆盖值'"
              />
            </el-form-item>
          </el-form>
        </div>
      </div>
      <template #footer>
        <el-button @click="closeRunDialog">取消</el-button>
        <el-button type="primary" :loading="runLoading" @click="confirmRun">运行</el-button>
      </template>
    </el-dialog>
    <log-viewer
      ref="runLogViewerRef"
      :pipeline-id="pipelineId"
      :pipeline-run-id="currentRun?.id || null"
      :title="`构建 #${currentRun?.build_number || ''}`"
      :task-list="runTasks"
    />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch, onUnmounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft,
  VideoPlay,
  Edit,
  Clock,
  DataAnalysis,
  Setting,
  Refresh,
  Loading,
  SuccessFilled,
  CircleCloseFilled,
  Warning,
  Close
} from '@element-plus/icons-vue'
import { getPipelineDetail, getPipelineTaskTypes, getPipelineTriggers, runPipeline, updatePipeline, updatePipelineTriggers, getPipelineRuns, getPipelineRunDetail, getRunTasks, getPipelineStatistics, cancelPipelineRun, getPipelineRunParameterView, previewPipelineRunRerun, previewPipelineWebhookRuntimeMappings } from '@/api/pipeline'
import { getTaskLogs as fetchTaskLogsFromApi } from '@/api/task'
import { getProjectList } from '@/api/project'
import { buildStatisticsDateParams, getDefaultStatisticsDateRange } from '@/views/statistics/dateRange'
import DesignTab from './designTab.vue'
import LogViewer from './components/LogViewer.vue'
import realtime from '@/utils/realtime'
import {
  applyGitlabWebhookRuntimePresetToRows,
  buildWebhookRuntimeMappingEditorRows,
  buildRunInputsPayload as buildManualRunPayload,
  createRunInputs,
  createRunInputsFromRerunPreview,
  createWebhookRuntimeInputMappingRow,
  getManualRunNodes,
  getWebhookRuntimeInputTargets,
  normalizeRerunPreviewPayload,
  normalizeRunParameterViewPayload,
  normalizeWebhookRuntimePreviewPayload,
  normalizeWebhookRuntimeStructuredErrors,
  normalizeWebhookRuntimeTriggerConfig,
  parseJSONField,
  serializeWebhookRuntimeInputMappings,
  validateWebhookRuntimeJSONPath
} from './runtimeConfig'
import { applyTaskStatusPayload, getTaskOutputDisplayKind, normalizeExecutionTaskOutputs } from './executionRealtimeState'
import { buildDisplayedWebhookURL, buildDisplayedSecretToken, canCopyDisplayValue, copyDisplayedValue } from './triggerWebhookDisplay.js'

const route = useRoute()
const router = useRouter()

const pipelineId = computed(() => parseInt(route.params.id))

// 状态
const activeTab = ref('design')
const historyLoading = ref(false)
const runDialogVisible = ref(false)
const runLoading = ref(false)
const parameterDrawerVisible = ref(false)
const parameterDrawerLoading = ref(false)
const rerunPreviewVisible = ref(false)
const rerunPreviewLoading = ref(false)
const settingsActiveTab = ref('basic')
const triggerSettingsLoading = ref(false)
const triggerSettingsSaving = ref(false)

// 数据
const pipeline = ref(null)
const runHistory = ref([])
const totalRuns = ref(0)
const projectList = ref([])
const projectSelectRef = ref(null)
const recentFailures = ref([])
const statisticsTrend = ref([])
const statisticsDistribution = ref([])
const pipelineTaskDefinitions = ref([])
const parameterDrawerRun = ref(null)
const parameterDrawerNodes = ref([])
const rerunSourceRun = ref(null)
const rerunPreviewData = ref(normalizeRerunPreviewPayload(null))
const runDialogPrefillSource = ref(null)
const webhookRuntimeMappingRows = ref([])
const webhookRuntimeSamplePayload = ref(`{
  "ref": "refs/heads/main"
}`)
const webhookRuntimePreviewLoading = ref(false)
const webhookRuntimePreviewData = ref(normalizeWebhookRuntimePreviewPayload(null))
const webhookRuntimePreviewRequestError = ref('')
const webhookRuntimePreviewExpanded = ref(false)
const webhookRuntimeSavedMappings = ref([])
const webhookRuntimeDraftRows = ref([])

// 运行表单（node-scoped runtime inputs）
const runForm = reactive({
  inputs: {}
})

const getRunConfig = (run) => parseJSONField(run?.run_config_json, {}) || {}
const getPipelineSnapshot = (run) => parseJSONField(run?.pipeline_snapshot_json, {}) || {}
const getResolvedNodes = (run) => {
  const resolved = parseJSONField(run?.resolved_nodes_json, [])
  return Array.isArray(resolved) ? resolved : []
}
const getOutputsByNode = (run) => parseJSONField(run?.outputs_json, {}) || {}
const getEvents = (run) => {
  const events = parseJSONField(run?.events_json, [])
  return Array.isArray(events) ? events : []
}

const getRunTriggerType = (run) => {
  const trigger = getRunConfig(run)?.trigger || {}
  return trigger.type || run?.trigger_type || '-'
}

const getRunTriggerUser = (run) => {
  const trigger = getRunConfig(run)?.trigger || {}
  return trigger.operator || run?.trigger_user || '-'
}

const getRunStartTime = (run) => {
  if (!run) return null
  return run.start_time || run.created_at || null
}

const updateRunHistoryEntry = (runID, patch) => {
  const targetRunID = Number(runID || 0)
  if (!targetRunID) return
  const index = runHistory.value.findIndex(run => Number(run?.id || 0) === targetRunID)
  if (index === -1) return
  runHistory.value.splice(index, 1, {
    ...runHistory.value[index],
    ...patch
  })
}

const getRunBranch = (run) => {
  const runInputs = getRunConfig(run)?.inputs || {}
  for (const params of Object.values(runInputs)) {
    if (params && typeof params === 'object') {
      if (params.git_ref) return params.git_ref
    }
  }

  const outputs = getOutputsByNode(run)
  for (const nodeOutput of Object.values(outputs)) {
    if (nodeOutput && typeof nodeOutput === 'object' && nodeOutput.git_ref) {
      return nodeOutput.git_ref
    }
  }

  return run?.branch || '-'
}

const manualRunNodes = computed(() => getManualRunNodes({
  ...pipeline.value,
  task_definitions: pipelineTaskDefinitions.value
}))

const webhookRuntimeTargets = computed(() => getWebhookRuntimeInputTargets({
  ...pipeline.value,
  task_definitions: pipelineTaskDefinitions.value
}))

const webhookRuntimeTargetMap = computed(() => new Map(
  webhookRuntimeTargets.value.map(target => [target.target_key, target])
))

const activeWebhookRuntimeMappingRows = computed(() => webhookRuntimeMappingRows.value.filter(row => row?.deleted !== true))

const initializeRunInputs = (rerunPreview = null) => {
  runForm.inputs = rerunPreview
    ? createRunInputsFromRerunPreview(manualRunNodes.value, rerunPreview)
    : createRunInputs(manualRunNodes.value)
}

const buildRunInputsPayload = () => buildManualRunPayload(manualRunNodes.value, runForm.inputs)

const closeRunDialog = () => {
  runDialogVisible.value = false
  runLoading.value = false
  runForm.inputs = {}
  runDialogPrefillSource.value = null
}

const fetchPipelineTaskDefinitions = async () => {
  try {
    const response = await getPipelineTaskTypes()
    if (response.code === 200) {
      pipelineTaskDefinitions.value = Array.isArray(response.data) ? response.data : (response.data?.list || [])
      return pipelineTaskDefinitions.value
    }
  } catch (error) {
    console.error('获取任务定义失败:', error)
  }

  pipelineTaskDefinitions.value = []
  return pipelineTaskDefinitions.value
}

// 设置表单
const settingsForm = reactive({
  name: '',
  project_id: null,
  environment: 'development',
  description: ''
})

const normalizeProjectSelectionValue = (value) => {
  if (value === null || value === undefined || value === '') {
    return null
  }

  const numericValue = Number(value)
  return Number.isNaN(numericValue) ? value : numericValue
}

const currentProjectOption = computed(() => {
  const project = pipeline.value?.project
  if (!project?.id || !project?.name) return null
  return {
    id: normalizeProjectSelectionValue(project.id),
    name: project.name
  }
})

const projectOptions = computed(() => {
  const options = []
  const seen = new Set()

  const pushOption = (project) => {
    if (!project?.id || !project?.name) return
    const normalizedID = normalizeProjectSelectionValue(project.id)
    const cacheKey = String(normalizedID)
    if (seen.has(cacheKey)) return
    seen.add(cacheKey)
    options.push({
      id: normalizedID,
      name: project.name
    })
  }

  pushOption(currentProjectOption.value)
  projectList.value.forEach(pushOption)

  return options
})

const projectQuery = reactive({
  keyword: '',
  page: 1,
  pageSize: 20,
  total: 0,
  loading: false,
  initialized: false,
  visible: false
})

let projectDropdownScrollCleanup = null

const resetProjectQuery = (keyword = '') => {
  projectQuery.keyword = keyword
  projectQuery.page = 1
  projectQuery.total = 0
}

const fetchProjects = async ({ append = false } = {}) => {
  if (projectQuery.loading) return

  projectQuery.loading = true
  try {
    const response = await getProjectList({
      page: projectQuery.page,
      page_size: projectQuery.pageSize,
      keyword: projectQuery.keyword
    })
    const fetchedProjects = Array.isArray(response?.data?.list) ? response.data.list : []
    projectQuery.total = Number(response?.data?.total || 0)
    projectList.value = append ? [...projectList.value, ...fetchedProjects] : fetchedProjects
    projectQuery.initialized = true
  } catch (error) {
    console.error('获取项目列表失败:', error)
  } finally {
    projectQuery.loading = false
    nextTick(() => {
      bindProjectDropdownScroll()
    })
  }
}

const loadMoreProjects = async () => {
  if (projectQuery.loading) return
  if (projectList.value.length >= projectQuery.total) return

  projectQuery.page += 1
  await fetchProjects({ append: true })
}

const bindProjectDropdownScroll = () => {
  if (!projectQuery.visible) return

  const dropdown = document.querySelector('.pipeline-project-select-dropdown .el-select-dropdown__wrap')
  if (!dropdown || dropdown.dataset.easydoBound === 'true') return

  const onScroll = () => {
    const threshold = 24
    const reachedBottom = dropdown.scrollTop + dropdown.clientHeight >= dropdown.scrollHeight - threshold
    if (reachedBottom) {
      loadMoreProjects()
    }
  }

  dropdown.addEventListener('scroll', onScroll)
  dropdown.dataset.easydoBound = 'true'
  projectDropdownScrollCleanup = () => {
    dropdown.removeEventListener('scroll', onScroll)
    delete dropdown.dataset.easydoBound
  }
}

const handleProjectSearch = async (keyword) => {
  resetProjectQuery((keyword || '').trim())
  await fetchProjects()
}

const handleProjectDropdownVisibleChange = async (visible) => {
  projectQuery.visible = visible
  if (!visible) {
    if (projectDropdownScrollCleanup) {
      projectDropdownScrollCleanup()
      projectDropdownScrollCleanup = null
    }
    return
  }

  if (!projectQuery.initialized) {
    resetProjectQuery(projectQuery.keyword)
    await fetchProjects()
    return
  }

  nextTick(() => {
    bindProjectDropdownScroll()
  })
}

// 触发设置
const triggerSettings = reactive({
  provider: '',
  webhook_enabled: false,
  push_enabled: false,
  tag_enabled: false,
  merge_request_enabled: false,
  push_branch_filters: '',
  tag_filters: '',
  merge_request_source_branch_filters: '',
  merge_request_target_branch_filters: '',
  webhook_runtime_input_mappings: '',
  webhook_config_status: '',
  webhook_config_invalid_reason: '',
  schedule_enabled: false,
  cron_expression: '',
  timezone: '',
  secret_token: '',
  webhook_token: '',
  webhook_url: '',
  next_run_at: '',
  last_run_at: '',
  last_triggered_at: '',
  manual: true
})

const scheduleWeekdayOptions = [
  { label: '周日', value: '0' },
  { label: '周一', value: '1' },
  { label: '周二', value: '2' },
  { label: '周三', value: '3' },
  { label: '周四', value: '4' },
  { label: '周五', value: '5' },
  { label: '周六', value: '6' }
]

const createDefaultScheduleBuilder = () => ({
  mode: 'every_minutes',
  everyMinutes: 15,
  dailyTime: '09:00',
  weeklyDay: '1',
  weeklyTime: '09:00',
  customCron: '',
  advancedMode: false
})

const scheduleBuilder = reactive(createDefaultScheduleBuilder())

// 通知设置
const notificationSettings = reactive({
  on_success: true,
  on_failure: true,
  by_email: true,
  by_dingtalk: false
})

// 统计
const statsDateRange = ref(getDefaultStatisticsDateRange())
const statistics = reactive({
  total_runs: 0,
  successful_runs: 0,
  failed_runs: 0,
  success_rate: 0,
  avg_duration: 0
})

const maxStatisticsTrendValue = computed(() => {
  if (!statisticsTrend.value.length) return 1
  return Math.max(...statisticsTrend.value.map(item => Number(item.total || 0)), 1)
})

const statisticsTrendSuccessCount = computed(() => statisticsTrend.value.reduce((sum, item) => sum + Number(item.success || 0), 0))
const statisticsTrendFailedCount = computed(() => statisticsTrend.value.reduce((sum, item) => sum + Number(item.failed || 0), 0))
const statisticsTrendTotalCount = computed(() => statisticsTrend.value.reduce((sum, item) => sum + Number(item.total || 0), 0))
const distributionTotalCount = computed(() => statisticsDistribution.value.reduce((sum, item) => sum + Number(item.count || 0), 0))

const statisticsChartCircumference = 2 * Math.PI * 40

const successDistribution = computed(() => statisticsDistribution.value.find(item => item.status === 'success') || { count: 0, rate: 0 })
const failedDistribution = computed(() => {
  const count = statisticsDistribution.value
    .filter(item => item.status !== 'success')
    .reduce((sum, item) => sum + Number(item.count || 0), 0)
  const rate = distributionTotalCount.value > 0 ? (count * 100) / distributionTotalCount.value : 0
  return {
    count,
    rate
  }
})

const successDashArray = computed(() => {
  const successLength = (Number(successDistribution.value.rate || 0) / 100) * statisticsChartCircumference
  return `${successLength} ${statisticsChartCircumference}`
})

const failedDashArray = computed(() => {
  const failedLength = (Number(failedDistribution.value.rate || 0) / 100) * statisticsChartCircumference
  return `${failedLength} ${statisticsChartCircumference}`
})

const failedDashOffset = computed(() => -((Number(successDistribution.value.rate || 0) / 100) * statisticsChartCircumference))

const getTrendStackHeight = (item) => {
  const total = Number(item?.total || 0)
  if (total <= 0) return 0
  return Math.max((total / maxStatisticsTrendValue.value) * 100, 8)
}

// 执行过程相关
const currentRun = ref(null)
const runTasks = ref([])
const taskLogs = ref([])
const executionLoading = ref(false)
const showLogPanel = ref(false)
const selectedTask = ref(null)
const logContentRef = ref(null)
const realtimeMode = ref('idle')
const runLogViewerRef = ref(null)
let realtimeHandlersBound = false
const detailContainerRef = ref(null)
const detailContainerWidth = ref(0)
let detailResizeObserver = null
const realtimeHandlers = {}

const parseTaskOrderTime = (value) => {
  if (value === null || value === undefined || value === '' || value === 0) {
    return Number.POSITIVE_INFINITY
  }

  if (typeof value === 'number') {
    if (value <= 0) return Number.POSITIVE_INFINITY
    return value > 1e12 ? value : value * 1000
  }

  if (typeof value === 'string') {
    const ms = Date.parse(value)
    if (Number.isNaN(ms) || ms <= 0) {
      return Number.POSITIVE_INFINITY
    }
    return ms
  }

  return Number.POSITIVE_INFINITY
}

const sortedRunTasks = computed(() => {
  if (!Array.isArray(runTasks.value)) return []

  return [...runTasks.value]
    .map((task, index) => ({
      task,
      index,
      hasScheduledRecord: (task.id || 0) > 0,
      scheduledAt: parseTaskOrderTime(task.created_at),
      startedAt: parseTaskOrderTime(task.start_time)
    }))
    .sort((a, b) => {
      if (a.hasScheduledRecord !== b.hasScheduledRecord) {
        return a.hasScheduledRecord ? -1 : 1
      }

      if (a.scheduledAt !== b.scheduledAt) {
        return a.scheduledAt - b.scheduledAt
      }

      if (a.startedAt !== b.startedAt) {
        return a.startedAt - b.startedAt
      }

      const aID = a.task.id || 0
      const bID = b.task.id || 0
      if (aID !== bID) {
        return aID - bID
      }

      return a.index - b.index
    })
    .map(item => item.task)
})

const getTaskNodeID = (task) => {
  return task?.node_id || task?.NodeID || ''
}

const buildRunTasksFromRunRecord = (run) => {
  const resolvedNodes = getResolvedNodes(run)
  const outputsByNode = getOutputsByNode(run)
  const events = getEvents(run)
  const snapshot = getPipelineSnapshot(run)
  const snapshotNodes = Array.isArray(snapshot?.nodes) ? snapshot.nodes : []
  const snapshotNodeMap = new Map(snapshotNodes.map((node, index) => [
    node.node_id || node.id,
    { ...node, __index: index }
  ]))

  const eventBuckets = new Map()
  events.forEach((event) => {
    const nodeID = event?.payload?.node_id
    if (!nodeID) return
    if (!eventBuckets.has(nodeID)) {
      eventBuckets.set(nodeID, [])
    }
    eventBuckets.get(nodeID).push(event)
  })

  return resolvedNodes.map((node, index) => {
    const nodeID = node.node_id || `node_${index + 1}`
    const snapshotNode = snapshotNodeMap.get(nodeID) || null
    const attempts = Array.isArray(node.attempts) ? node.attempts : []
    const latestAttempt = attempts.length > 0 ? attempts[attempts.length - 1] : null
    const nodeEvents = eventBuckets.get(nodeID) || []
    const startEvent = nodeEvents.find(item => item?.event_type === 'node_running')

    const normalizedStatus =
      node.status === 'success' ? 'execute_success' :
      node.status === 'failed' ? 'execute_failed' :
      node.status || 'queued'

    return {
      id: latestAttempt?.task_id || latestAttempt?.attempt_no || 0,
      node_id: nodeID,
      name: node.node_name || snapshotNode?.node_name || snapshotNode?.name || nodeID,
      task_type: node.task_key || '',
      status: normalizedStatus,
      display_status: normalizedStatus,
      ignore_failure: Boolean(snapshotNode?.ignore_failure),
      start_time: latestAttempt?.start_time || startEvent?.time || 0,
      created_at: latestAttempt?.start_time || startEvent?.time || run?.created_at || 0,
      duration: latestAttempt?.duration || 0,
      exit_code: latestAttempt?.exit_code ?? outputsByNode[nodeID]?.exit_code ?? null,
      error_msg: latestAttempt?.error_msg || '',
      outputs: outputsByNode[nodeID] || {},
      _order: Number.isFinite(snapshotNode?.__index) ? snapshotNode.__index : index,
      Agent: latestAttempt?.agent_id ? { name: `Agent #${latestAttempt.agent_id}` } : null
    }
  }).sort((a, b) => a._order - b._order)
}

const normalizeRunTaskFromApi = (task, index, fallbackTaskMap = new Map()) => {
  const nodeID = task?.node_id || task?.NodeID || task?.nodeId || ''
  const taskID = Number(task?.id || task?.task_id || 0)
  const fallback = (nodeID && fallbackTaskMap.get(nodeID))
    || (taskID > 0 ? Array.from(fallbackTaskMap.values()).find(item => Number(item.id || 0) === taskID) : null)
    || null

  const rawStatus = task?.status || fallback?.status || 'queued'
  const normalizedStatus =
    rawStatus === 'success' ? 'execute_success'
      : rawStatus === 'failed' ? 'execute_failed'
        : rawStatus

  const rawDisplayStatus = task?.display_status || task?.displayStatus || ''
  const normalizedDisplayStatus = rawDisplayStatus || normalizedStatus
  const resolvedName = task?.name || task?.task_name || task?.node_name || fallback?.name || nodeID || (taskID ? `任务 #${taskID}` : `任务 #${index + 1}`)

  return {
    ...fallback,
    ...task,
    id: taskID || Number(fallback?.id || 0),
    node_id: nodeID || fallback?.node_id || fallback?.NodeID || '',
    name: resolvedName,
    task_type: task?.task_type || task?.type || task?.task_key || fallback?.task_type || '',
    status: normalizedStatus,
    display_status: normalizedDisplayStatus,
    ignore_failure: Boolean(task?.ignore_failure ?? fallback?.ignore_failure),
    start_time: task?.start_time ?? fallback?.start_time ?? 0,
    created_at: task?.created_at ?? fallback?.created_at ?? 0,
    duration: task?.duration ?? fallback?.duration ?? 0,
    exit_code: task?.exit_code ?? fallback?.exit_code ?? null,
    error_msg: task?.error_msg || fallback?.error_msg || '',
    outputs: task?.outputs || fallback?.outputs || {},
    _order: Number.isFinite(task?._order)
      ? task._order
      : Number.isFinite(fallback?._order)
        ? fallback._order
        : index,
    Agent: task?.Agent || (task?.agent_name ? { name: task.agent_name } : fallback?.Agent || null)
  }
}

// 计算执行进度
const executionProgress = computed(() => {
  if (!runTasks.value || runTasks.value.length === 0) return 0
  const completed = runTasks.value.filter(t => ['execute_success', 'execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired', 'cancelled'].includes(t.status)).length
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

const runDialogWidth = computed(() => {
  const containerWidth = detailContainerWidth.value
  if (!containerWidth) return '680px'
  const calculated = Math.floor(containerWidth * 0.7)
  const clamped = Math.max(520, Math.min(860, calculated))
  return `${clamped}px`
})

const rerunPreviewBlocked = computed(() => {
  const preview = rerunPreviewData.value
  return Boolean(preview?.failure) || preview?.can_enter_run_dialog !== true
})

const rerunPreviewSummaryText = computed(() => {
  const preview = rerunPreviewData.value
  const matchedCount = Array.isArray(preview?.matched) ? preview.matched.length : 0
  const mismatchedCount = Array.isArray(preview?.mismatched) ? preview.mismatched.length : 0
  return `匹配 ${matchedCount} 项，不匹配 ${mismatchedCount} 项`
})

const rerunPreviewFailureTitle = computed(() => {
  const failure = rerunPreviewData.value?.failure || null
  return failure?.message || failure?.error || '重新运行预览失败'
})

const normalizeRerunPreviewItem = (item) => {
  if (!item || typeof item !== 'object' || Array.isArray(item)) {
    return {
      node_id: '',
      node_name: '',
      param_key: '',
      param_label: '',
      match_key: '',
      reason: '',
      historical_value: undefined,
      current_value: undefined
    }
  }

  return {
    node_id: item.node_id || '',
    node_name: item.node_name || '',
    param_key: item.param_key || '',
    param_label: item.param_label || '',
    match_key: item.match_key || '',
    reason: item.reason || '',
    historical_value: item.historical_value,
    current_value: item.current_value
  }
}

const rerunPreviewMatchedItems = computed(() => {
  const matched = Array.isArray(rerunPreviewData.value?.matched) ? rerunPreviewData.value.matched : []
  return matched.map(normalizeRerunPreviewItem)
})

const rerunPreviewMismatchedItems = computed(() => {
  const mismatched = Array.isArray(rerunPreviewData.value?.mismatched) ? rerunPreviewData.value.mismatched : []
  return mismatched.map(normalizeRerunPreviewItem)
})

const currentPageOrigin = window.location.origin
const displayedWebhookURL = computed(() => buildDisplayedWebhookURL({
  origin: currentPageOrigin,
  webhookToken: triggerSettings.webhook_token,
  fallbackURL: triggerSettings.webhook_url
}))
const displaySecretToken = computed(() => buildDisplayedSecretToken({
  secretToken: triggerSettings.secret_token,
  webhookToken: triggerSettings.webhook_token
}))
const canCopyWebhookURL = computed(() => canCopyDisplayValue(displayedWebhookURL.value))
const canCopySecretToken = computed(() => canCopyDisplayValue(displaySecretToken.value))
const webhookSectionEnabled = computed(() => Boolean(
  triggerSettings.push_enabled
  || triggerSettings.tag_enabled
  || triggerSettings.merge_request_enabled
))
const webhookRuntimeConfigStatusType = computed(() => {
  const status = String(triggerSettings.webhook_config_status || '').trim()
  if (!status || status === 'valid') return 'success'
  return 'warning'
})
const webhookRuntimeConfigStatusText = computed(() => {
  const status = String(triggerSettings.webhook_config_status || '').trim()
  if (!status) return ''
  if (status === 'valid') return '配置校验正常'
  return '配置待修正'
})
const webhookRuntimePreviewSummary = computed(() => webhookRuntimePreviewData.value?.summary || {
  total: 0,
  matched: 0,
  missing: 0,
  failed: 0
})
const webhookRuntimePreviewErrorMap = computed(() => {
  return normalizeWebhookRuntimeStructuredErrors(webhookRuntimePreviewData.value?.errors).reduce((acc, item) => {
    if (!item?.mapping_id) return acc
    if (!acc[item.mapping_id]) acc[item.mapping_id] = []
    acc[item.mapping_id].push(item)
    return acc
  }, {})
})
const cloneWebhookRuntimeEditorRow = (row) => ({
  ...row,
  target: {
    ...(row?.target || {})
  }
})

const mergeWebhookRuntimeEditorRows = (targets = [], preferredRows = [], fallbackRows = []) => {
  return buildWebhookRuntimeMappingEditorRows(targets, [
    ...fallbackRows,
    ...preferredRows
  ])
}

const getWebhookRuntimeMappingTargetKey = (row) => {
  const targetKey = String(row?.target_key || '').trim()
  if (targetKey) return targetKey
  const normalizedRow = createWebhookRuntimeInputMappingRow(row)
  if (!normalizedRow.target.node_id || !normalizedRow.target.param_key) return ''
  return `${normalizedRow.target.node_id}.${normalizedRow.target.param_key}`
}

const getWebhookRuntimeTargetLabel = (row) => {
  const targetKey = getWebhookRuntimeMappingTargetKey(row)
  const target = targetKey ? webhookRuntimeTargetMap.value.get(targetKey) : null
  const nodeIndex = Number(target?.node_index ?? row?.node_index)
  const nodeName = target?.node_name || row?.node_name || row?.node_id || '-'
  const paramLabel = target?.param_label || row?.param_label || row?.param_key || '-'
  const paramKey = target?.param_key || row?.param_key || '-'
  const nodePrefix = Number.isFinite(nodeIndex) && nodeIndex > 0 ? `#${nodeIndex} ` : ''
  return `${nodePrefix}${nodeName} / ${paramLabel === paramKey ? paramLabel : `${paramLabel} (${paramKey})`}`
}

const getWebhookRuntimeMappingRowErrors = (row) => {
  const normalizedRow = createWebhookRuntimeInputMappingRow(row)
  if (normalizedRow.deleted === true) return []
  if (validateWebhookRuntimeJSONPath(normalizedRow.source_expr)) {
    return ['请填写合法的 JSONPath']
  }
  return []
}

const hasInvalidActiveWebhookRuntimeMapping = computed(() => {
  return webhookRuntimeMappingRows.value.some(row => getWebhookRuntimeMappingRowErrors(row).length > 0)
})

const webhookRuntimePreviewRows = computed(() => {
  return activeWebhookRuntimeMappingRows.value.map((row, index) => {
    const normalizedRow = createWebhookRuntimeInputMappingRow(row, index)
    const targetKey = getWebhookRuntimeMappingTargetKey(row)
    const target = targetKey ? webhookRuntimeTargetMap.value.get(targetKey) : null
    const ruleResult = webhookRuntimePreviewData.value?.rule_results?.[normalizedRow.id] || null
    const previewValue = ruleResult && Object.prototype.hasOwnProperty.call(ruleResult, 'value')
      ? ruleResult.value
      : undefined

    return {
      ...normalizedRow,
      target_key: targetKey,
      target_label: getWebhookRuntimeTargetLabel(target || row),
      result_code: ruleResult?.code || '',
      preview_value: previewValue,
      errors: webhookRuntimePreviewErrorMap.value[normalizedRow.id] || []
    }
  })
})

const resetWebhookRuntimePreviewState = () => {
  webhookRuntimePreviewData.value = normalizeWebhookRuntimePreviewPayload(null)
  webhookRuntimePreviewRequestError.value = ''
}

const syncWebhookRuntimeMappingsToTriggerSettings = () => {
  triggerSettings.webhook_runtime_input_mappings = serializeWebhookRuntimeInputMappings(webhookRuntimeMappingRows.value)
}

const validateWebhookRuntimeMappings = () => {
  const invalidIndex = webhookRuntimeMappingRows.value.findIndex((row) => getWebhookRuntimeMappingRowErrors(row).length > 0)
  if (invalidIndex === -1) return true

  ElMessage.error(`请先修正第 ${invalidIndex + 1} 条映射规则的 JSONPath`)
  return false
}

const toggleWebhookRuntimeMappingDeleted = (row, deleted) => {
  row.deleted = deleted
}

const applyGitlabWebhookRuntimePreset = () => {
  const presetEligibleParamKeys = new Set(['git_ref', 'ref', 'branch', 'branch_name', 'source_branch', 'target_branch', 'commit_sha', 'git_commit', 'sha'])
  const matchedCount = webhookRuntimeMappingRows.value.filter((row) => {
    return presetEligibleParamKeys.has(String(row?.param_key || '').trim()) && String(row?.runtime_value_type || '').trim() === 'string'
  }).length
  if (matchedCount === 0) {
    ElMessage.warning('当前流水线里没有可直接套用 GitLab 预设的运行时参数')
    return
  }

  webhookRuntimeMappingRows.value = applyGitlabWebhookRuntimePresetToRows(webhookRuntimeMappingRows.value)
  ElMessage.success(`已填入 ${matchedCount} 条 GitLab 常用映射`)
}

watch(webhookRuntimeMappingRows, (rows) => {
  syncWebhookRuntimeMappingsToTriggerSettings()
  if (webhookRuntimeTargets.value.length > 0) {
    webhookRuntimeDraftRows.value = rows.map(cloneWebhookRuntimeEditorRow)
  }
  resetWebhookRuntimePreviewState()
}, { deep: true })

watch(webhookRuntimeSamplePayload, () => {
  resetWebhookRuntimePreviewState()
})

watch(webhookRuntimeTargets, (targets) => {
  const normalizedTargets = Array.isArray(targets) ? targets : []
  webhookRuntimeMappingRows.value = mergeWebhookRuntimeEditorRows(normalizedTargets, webhookRuntimeDraftRows.value, webhookRuntimeSavedMappings.value)
}, { deep: true })

const normalizeCronExpression = (expression = '') => String(expression || '').trim().replace(/\s+/g, ' ')

const padTimeUnit = (value) => String(value).padStart(2, '0')

const normalizeTimeValue = (value, fallback = '09:00') => {
  if (typeof value !== 'string') return fallback

  const matched = value.trim().match(/^(\d{1,2}):(\d{2})/)
  if (!matched) return fallback

  const hour = Number.parseInt(matched[1], 10)
  const minute = Number.parseInt(matched[2], 10)
  if (Number.isNaN(hour) || Number.isNaN(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) {
    return fallback
  }

  return `${padTimeUnit(hour)}:${padTimeUnit(minute)}`
}

const normalizeScheduleInterval = (value) => {
  const parsed = Number.parseInt(value, 10)
  if (Number.isNaN(parsed)) return 15
  return Math.min(Math.max(parsed, 1), 1440)
}

const buildScheduleCronExpression = (mode = scheduleBuilder.mode) => {
  if (mode === 'every_minutes') {
    const interval = normalizeScheduleInterval(scheduleBuilder.everyMinutes)
    return interval <= 1 ? '* * * * *' : `*/${interval} * * * *`
  }

  if (mode === 'daily') {
    const [hour, minute] = normalizeTimeValue(scheduleBuilder.dailyTime).split(':')
    return `${Number.parseInt(minute, 10)} ${Number.parseInt(hour, 10)} * * *`
  }

  if (mode === 'weekly') {
    const [hour, minute] = normalizeTimeValue(scheduleBuilder.weeklyTime).split(':')
    const weekday = scheduleWeekdayOptions.some(item => item.value === String(scheduleBuilder.weeklyDay))
      ? String(scheduleBuilder.weeklyDay)
      : '1'
    return `${Number.parseInt(minute, 10)} ${Number.parseInt(hour, 10)} * * ${weekday}`
  }

  return normalizeCronExpression(scheduleBuilder.customCron)
}

const scheduleCronPreview = computed(() => buildScheduleCronExpression())

const applyScheduleBuilderState = (state = {}) => {
  scheduleBuilder.mode = state.mode || 'every_minutes'
  scheduleBuilder.everyMinutes = normalizeScheduleInterval(state.everyMinutes)
  scheduleBuilder.dailyTime = normalizeTimeValue(state.dailyTime)
  scheduleBuilder.weeklyDay = scheduleWeekdayOptions.some(item => item.value === String(state.weeklyDay))
    ? String(state.weeklyDay)
    : '1'
  scheduleBuilder.weeklyTime = normalizeTimeValue(state.weeklyTime)
  scheduleBuilder.customCron = normalizeCronExpression(state.customCron)
  scheduleBuilder.advancedMode = Boolean(state.advancedMode)
}

const deriveScheduleBuilderFromCron = (expression = '') => {
  const normalized = normalizeCronExpression(expression)
  if (!normalized) {
    return createDefaultScheduleBuilder()
  }

  const everyMinutesMatch = normalized.match(/^(\*|\*\/(\d+)|0\/(\d+)) \* \* \* \*$/)
  if (everyMinutesMatch) {
    const interval = everyMinutesMatch[1] === '*'
      ? 1
      : Number.parseInt(everyMinutesMatch[2] || everyMinutesMatch[3], 10)

    return {
      mode: 'every_minutes',
      everyMinutes: normalizeScheduleInterval(interval),
      dailyTime: '09:00',
      weeklyDay: '1',
      weeklyTime: '09:00',
      customCron: normalized,
      advancedMode: false
    }
  }

  const dailyMatch = normalized.match(/^([0-5]?\d) ([01]?\d|2[0-3]) \* \* \*$/)
  if (dailyMatch) {
    return {
      mode: 'daily',
      everyMinutes: 15,
      dailyTime: `${padTimeUnit(dailyMatch[2])}:${padTimeUnit(dailyMatch[1])}`,
      weeklyDay: '1',
      weeklyTime: '09:00',
      customCron: normalized,
      advancedMode: false
    }
  }

  const weeklyMatch = normalized.match(/^([0-5]?\d) ([01]?\d|2[0-3]) \* \* ([0-7])$/)
  if (weeklyMatch) {
    return {
      mode: 'weekly',
      everyMinutes: 15,
      dailyTime: '09:00',
      weeklyDay: weeklyMatch[3] === '7' ? '0' : weeklyMatch[3],
      weeklyTime: `${padTimeUnit(weeklyMatch[2])}:${padTimeUnit(weeklyMatch[1])}`,
      customCron: normalized,
      advancedMode: false
    }
  }

  return {
    mode: 'custom',
    everyMinutes: 15,
    dailyTime: '09:00',
    weeklyDay: '1',
    weeklyTime: '09:00',
    customCron: normalized,
    advancedMode: true
  }
}

const handleScheduleModeChange = (mode) => {
  scheduleBuilder.advancedMode = mode === 'custom'
}

watch(
  () => [scheduleBuilder.mode, scheduleBuilder.everyMinutes, scheduleBuilder.dailyTime, scheduleBuilder.weeklyDay, scheduleBuilder.weeklyTime],
  () => {
    if (scheduleBuilder.mode !== 'custom') {
      scheduleBuilder.customCron = buildScheduleCronExpression(scheduleBuilder.mode)
    }
  },
  { immediate: true }
)

const applyTriggerSettings = (data = {}) => {
  const normalizedWebhookRuntime = normalizeWebhookRuntimeTriggerConfig(data)

  triggerSettings.provider = data.provider || ''
  triggerSettings.webhook_enabled = Boolean(data.webhook_enabled)
  triggerSettings.push_enabled = Boolean(data.push_enabled)
  triggerSettings.tag_enabled = Boolean(data.tag_enabled)
  triggerSettings.merge_request_enabled = Boolean(data.merge_request_enabled)
  triggerSettings.push_branch_filters = data.push_branch_filters || ''
  triggerSettings.tag_filters = data.tag_filters || ''
  triggerSettings.merge_request_source_branch_filters = data.merge_request_source_branch_filters || ''
  triggerSettings.merge_request_target_branch_filters = data.merge_request_target_branch_filters || ''
  triggerSettings.webhook_runtime_input_mappings = normalizedWebhookRuntime.webhook_runtime_input_mappings
  triggerSettings.webhook_config_status = normalizedWebhookRuntime.webhook_config_status
  triggerSettings.webhook_config_invalid_reason = normalizedWebhookRuntime.webhook_config_invalid_reason
  webhookRuntimeSavedMappings.value = normalizedWebhookRuntime.mapping_rows
  webhookRuntimeMappingRows.value = mergeWebhookRuntimeEditorRows(webhookRuntimeTargets.value, normalizedWebhookRuntime.mapping_rows)
  webhookRuntimeDraftRows.value = webhookRuntimeMappingRows.value.map(cloneWebhookRuntimeEditorRow)
  triggerSettings.schedule_enabled = Boolean(data.schedule_enabled)
  triggerSettings.cron_expression = normalizeCronExpression(data.cron_expression)
  triggerSettings.timezone = data.timezone || ''
  triggerSettings.secret_token = data.secret_token || ''
  triggerSettings.webhook_token = data.webhook_token || ''
  triggerSettings.webhook_url = data.webhook_url || ''
  triggerSettings.next_run_at = data.next_run_at || ''
  triggerSettings.last_run_at = data.last_run_at || ''
  triggerSettings.last_triggered_at = data.last_triggered_at || ''
  triggerSettings.manual = data.manual !== undefined ? Boolean(data.manual) : true

  resetWebhookRuntimePreviewState()
  applyScheduleBuilderState(deriveScheduleBuilderFromCron(triggerSettings.cron_expression))
}

// 获取流水线详情
const fetchPipelineDetail = async () => {
  try {
    const response = await getPipelineDetail(pipelineId.value)
    if (response.code === 200) {
      pipeline.value = response.data
      // 更新设置表单
      settingsForm.name = pipeline.value.name
      settingsForm.project_id = normalizeProjectSelectionValue(pipeline.value.project_id)
      settingsForm.environment = pipeline.value.environment
      settingsForm.description = pipeline.value.description || ''
      return pipeline.value
    }
  } catch (error) {
    console.error('获取流水线详情失败:', error)
    ElMessage.error('获取流水线详情失败')
  }
  return null
}

// 当执行视图可见时，确保按选中/最新构建触发详情与任务接口，避免展示陈旧执行状态。
const hydrateExecutionViewIfVisible = async () => {
  if (activeTab.value !== 'execution') return

  const latestRun = runHistory.value[0] || null
  const targetRun = currentRun.value?.id ? currentRun.value : latestRun
  if (!targetRun?.id) return

  const targetRunID = Number(targetRun.id || 0)
  const currentRunID = Number(currentRun.value?.id || 0)
  if (targetRunID !== currentRunID) {
    currentRun.value = targetRun
    runTasks.value = []
    selectedTask.value = null
    taskLogs.value = []
  }

  stopRealtimeUpdates()
  startRealtimeUpdates(targetRun.id)
  await fetchExecutionDetail()
}

const fetchTriggerSettings = async () => {
  triggerSettingsLoading.value = true
  try {
    const response = await getPipelineTriggers(pipelineId.value)
    if (response.code === 200) {
      applyTriggerSettings(response.data || {})
    }
  } catch (error) {
    console.error('获取触发设置失败:', error)
    ElMessage.error('获取触发设置失败')
  } finally {
    triggerSettingsLoading.value = false
  }
}

// 获取运行历史
const fetchRunHistory = async (options = {}) => {
  const { rehydrateExecution = false } = options
  historyLoading.value = true

  // 页面初始加载若执行面板可见，先清空旧任务展示，避免接口返回前出现陈旧内容。
  if (rehydrateExecution && activeTab.value === 'execution') {
    runTasks.value = []
    selectedTask.value = null
    taskLogs.value = []
  }

  try {
    const response = await getPipelineRuns(pipelineId.value, { page: 1, page_size: 50 })
    if (response.code === 200) {
      runHistory.value = response.data.list || []
      totalRuns.value = response.data.total || 0

      if (rehydrateExecution) {
        const latestRun = runHistory.value[0] || null

        if (!latestRun) {
          currentRun.value = null
          runTasks.value = []
          selectedTask.value = null
          taskLogs.value = []
          stopRealtimeUpdates()
          return
        }

        const latestRunID = Number(latestRun.id || 0)
        const currentRunID = Number(currentRun.value?.id || 0)
        const runChanged = latestRunID !== currentRunID

        currentRun.value = latestRun
        if (runChanged) {
          runTasks.value = []
          selectedTask.value = null
          taskLogs.value = []
        }

        await hydrateExecutionViewIfVisible()
      }
    }
  } catch (error) {
    console.error('获取运行历史失败:', error)
  } finally {
    historyLoading.value = false
  }
}

// 获取项目列表
// 获取统计数据
const fetchStatistics = async () => {
  try {
    const response = await getPipelineStatistics(pipelineId.value, buildStatisticsDateParams(statsDateRange.value))
    if (response.code === 200) {
      const data = response.data
      statistics.total_runs = data.total_runs || 0
      statistics.successful_runs = data.successful_runs || 0
      statistics.failed_runs = data.failed_runs || 0
      statistics.success_rate = data.success_rate || 0
      statistics.avg_duration = data.avg_duration || 0
      statisticsTrend.value = Array.isArray(data.daily_runs) ? data.daily_runs : []
      statisticsDistribution.value = Array.isArray(data.distribution) ? data.distribution : []
      recentFailures.value = Array.isArray(data.recent_failures) ? data.recent_failures : []
    }
  } catch (error) {
    console.error('获取统计数据失败:', error)
  }
}

// 返回
const goBack = () => {
  router.push('/pipeline')
}

const openRunExecutionView = async (run) => {
  if (!run?.id) return
  currentRun.value = run

  if (activeTab.value !== 'execution') {
    activeTab.value = 'execution'
    return
  }

  await hydrateExecutionViewIfVisible()
}

const closeParameterDrawer = () => {
  parameterDrawerVisible.value = false
  parameterDrawerLoading.value = false
  parameterDrawerRun.value = null
  parameterDrawerNodes.value = []
}

const viewRunParameters = async (run) => {
  if (!run?.id) return
  parameterDrawerRun.value = run
  parameterDrawerVisible.value = true
  parameterDrawerLoading.value = true
  parameterDrawerNodes.value = []

  try {
    const response = await getPipelineRunParameterView(pipelineId.value, run.id)
    if (response?.code === 200) {
      const normalized = normalizeRunParameterViewPayload(response.data)
      parameterDrawerNodes.value = normalized.nodes
      return
    }
    ElMessage.error(response?.message || '获取参数视图失败')
  } catch (error) {
    console.error('获取参数视图失败:', error)
    ElMessage.error('获取参数视图失败')
  } finally {
    parameterDrawerLoading.value = false
  }
}

const closeRerunPreviewDialog = () => {
  rerunPreviewVisible.value = false
  rerunPreviewLoading.value = false
  rerunSourceRun.value = null
  rerunPreviewData.value = normalizeRerunPreviewPayload(null)
}

const openRunDialogFromRerunPreview = async () => {
  if (!rerunSourceRun.value?.id || rerunPreviewBlocked.value) return

  const sourceRun = rerunSourceRun.value
  const preview = rerunPreviewData.value
  const [latestPipeline] = await Promise.all([
    fetchPipelineDetail(),
    fetchPipelineTaskDefinitions()
  ])
  if (!latestPipeline) return

  initializeRunInputs(preview)
  runDialogPrefillSource.value = sourceRun
  closeRerunPreviewDialog()
  runDialogVisible.value = true
}

const handleRerun = async (run) => {
  if (!run?.id) return
  rerunSourceRun.value = run
  rerunPreviewVisible.value = true
  rerunPreviewLoading.value = true
  rerunPreviewData.value = normalizeRerunPreviewPayload(null)

  try {
    const response = await previewPipelineRunRerun(pipelineId.value, run.id)
    const normalized = normalizeRerunPreviewPayload(response?.data)
    rerunPreviewData.value = normalized
    if (response?.code !== 200) {
      ElMessage.error(response?.message || '获取重新运行预览失败')
    }
  } catch (error) {
    console.error('获取重新运行预览失败:', error)
    rerunPreviewData.value = normalizeRerunPreviewPayload({
      failure: {
        message: error.response?.data?.message || error.message || '获取重新运行预览失败'
      }
    })
  } finally {
    rerunPreviewLoading.value = false
  }
}

// 运行流水线
const handleRun = async () => {
  runDialogVisible.value = false
  runForm.inputs = {}
  runDialogPrefillSource.value = null

  const [latestPipeline] = await Promise.all([
    fetchPipelineDetail(),
    fetchPipelineTaskDefinitions()
  ])
  if (!latestPipeline) return

  initializeRunInputs()
  runDialogVisible.value = true
}

// 确认运行
const confirmRun = async () => {
  runLoading.value = true
  try {
    const response = await runPipeline(pipelineId.value, buildRunInputsPayload())
    if (response.code === 200) {
      const runStatus = response?.data?.status
      ElMessage.success(runStatus === 'queued' ? '流水线已进入排队' : '流水线已开始运行')
      runDialogVisible.value = false

      // 获取最新的运行记录并切换到执行Tab
      await fetchRunHistory()
      if (runHistory.value.length > 0) {
        await openRunExecutionView(runHistory.value[0])
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

// 查看运行详情
const viewRunDetail = (run) => {
  ElMessage.info(`查看构建 #${run.build_number} 详情`)
}

// 查看执行详情（切换到执行Tab）
const viewExecutionDetail = async (run) => {
  await openRunExecutionView(run)
}

// 查看运行任务
const viewRunTasks = async (run) => {
  await openRunExecutionView(run)
}

const viewRunLogs = async (run) => {
  currentRun.value = run
  await nextTick()
  runLogViewerRef.value?.open()
}

// 获取执行详情
const fetchExecutionDetail = async () => {
  if (!currentRun.value) return
  
  executionLoading.value = true
  try {
    const detailResponse = await getPipelineRunDetail(pipelineId.value, currentRun.value.id)
    if (detailResponse.code === 200 && detailResponse.data) {
      currentRun.value = detailResponse.data
    }

    const fallbackTasks = buildRunTasksFromRunRecord(currentRun.value)
    const fallbackTaskMap = new Map(
      fallbackTasks
        .filter(task => getTaskNodeID(task))
        .map(task => [getTaskNodeID(task), task])
    )

    const tasksResponse = await getRunTasks(pipelineId.value, currentRun.value.id)
    const taskList = Array.isArray(tasksResponse?.data)
      ? tasksResponse.data
      : Array.isArray(tasksResponse?.data?.list)
        ? tasksResponse.data.list
        : Array.isArray(tasksResponse?.data?.tasks)
          ? tasksResponse.data.tasks
          : []

    if (tasksResponse?.code === 200 && taskList.length > 0) {
      runTasks.value = taskList
        .map((task) => normalizeExecutionTaskOutputs(task))
        .map((task, index) => normalizeRunTaskFromApi(task, index, fallbackTaskMap))
        .sort((a, b) => (a._order ?? Number.POSITIVE_INFINITY) - (b._order ?? Number.POSITIVE_INFINITY))
    } else {
      runTasks.value = fallbackTasks
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

const isRunActive = (status) => ['queued', 'pending', 'running'].includes(status)

const isRunTerminal = (status) => ['success', 'failed', 'cancelled'].includes(status)

// 查看任务日志
const viewTaskLogs = async (task) => {
  selectedTask.value = task
  showLogPanel.value = true
  taskLogs.value = []
  await fetchTaskLogs(task)
}

const downloadTaskLogs = () => {
  const text = taskLogs.value.map(log => `[${formatLogTime(log.timestamp)}] ${log.message}`).join('\n')
  const blob = new Blob([text], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${selectedTask.value?.name || 'task'}-logs.txt`
  a.click()
  URL.revokeObjectURL(url)
}

// 停止执行
const stopExecution = () => {
  ElMessageBox.confirm('确定要停止当前执行吗？停止后所有正在运行的任务将被取消。', '停止确认', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    if (!currentRun.value?.id || !pipelineId.value) return
    try {
      const res = await cancelPipelineRun(pipelineId.value, currentRun.value.id)
      if (res.code === 200) {
        ElMessage.success('流水线已停止')
        currentRun.value.status = 'cancel_requested'
        updateRunHistoryEntry(currentRun.value.id, { status: 'cancel_requested' })
        await fetchRunHistory()
      } else {
        ElMessage.error(res.message || '停止失败')
      }
    } catch (error) {
      ElMessage.error('停止执行失败')
    }
  }).catch(() => {})
}

// 停止 WebSocket 更新
const stopAllUpdates = () => {
  stopRealtimeUpdates()
}

// 格式化日志时间
const formatLogTime = (timestamp) => {
  if (!timestamp) return ''
  return new Date(timestamp * 1000).toLocaleTimeString('zh-CN', { hour12: false })
}

const getLogSemanticClass = (message) => {
  if (!message) return ''
  if (message.startsWith('[easydo][cmd]')) return 'log-command'
  if (message.startsWith('[easydo][step]')) return 'log-step'
  if (message.startsWith('[easydo][warn]')) return 'log-warning-line'
  return ''
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

// 解析任务输出数据
const getTaskOutputs = (task) => {
  if (!task) return null
  return task.outputs || null
}

const formatTaskOutputValue = (value) => {
  if (value === undefined || value === null || value === '') return ''
  if (Array.isArray(value) || (typeof value === 'object' && value !== null)) {
    return JSON.stringify(value, null, 2)
  }
  return String(value)
}

// 格式化任务输出显示
const formatTaskOutputs = (outputs, taskType) => {
  if (!outputs) return []
  const lines = []

  // 通用字段
  if (outputs.exit_code !== undefined) {
    lines.push({ label: 'Exit Code', value: outputs.exit_code, type: outputs.exit_code === 0 ? 'success' : 'danger' })
  }
  if (outputs.duration !== undefined) {
    lines.push({ label: 'Duration', value: `${outputs.duration}s`, type: 'info' })
  }

  // git_clone 任务输出
  if (taskType === 'git_clone' || taskType === 'git-clone') {
    if (outputs.git_commit) lines.push({ label: 'Git Commit', value: outputs.git_commit, type: 'primary' })
    if (outputs.git_commit_short) lines.push({ label: 'Git Commit (Short)', value: outputs.git_commit_short, type: 'primary' })
    if (outputs.git_ref) lines.push({ label: 'Git Ref', value: outputs.git_ref, type: 'primary' })
    if (outputs.git_repo_url) lines.push({ label: 'Git Repo URL', value: outputs.git_repo_url, type: 'info' })
    if (outputs.git_checkout_path) lines.push({ label: 'Checkout Path', value: outputs.git_checkout_path, type: 'info' })
  }

  // docker 任务输出
  if (taskType === 'docker') {
    if (outputs.image_name) lines.push({ label: 'Image Name', value: outputs.image_name, type: 'primary' })
    if (outputs.image_tag) lines.push({ label: 'Image Tag', value: outputs.image_tag, type: 'primary' })
    if (outputs.image_full_name) lines.push({ label: 'Image Full Name', value: outputs.image_full_name, type: 'info' })
    if (outputs.pushed !== undefined) lines.push({ label: 'Pushed', value: outputs.pushed ? 'Yes' : 'No', type: outputs.pushed ? 'success' : 'warning' })
  }

  // npm/maven/gradle 构建任务输出
  if (taskType === 'npm' || taskType === 'maven' || taskType === 'gradle' || taskType === 'build') {
    if (outputs.artifact_path) lines.push({ label: 'Artifact Path', value: outputs.artifact_path, type: 'info' })
  }

  // 测试任务输出
  if (taskType === 'unit' || taskType === 'integration' || taskType === 'e2e' || taskType === 'test') {
    if (outputs.tests_passed !== undefined) lines.push({ label: 'Tests Passed', value: outputs.tests_passed, type: 'success' })
    if (outputs.tests_failed !== undefined) lines.push({ label: 'Tests Failed', value: outputs.tests_failed, type: 'danger' })
    if (outputs.tests_skipped !== undefined) lines.push({ label: 'Tests Skipped', value: outputs.tests_skipped, type: 'warning' })
  }

  // coverage 任务输出
  if (taskType === 'coverage') {
    if (outputs.coverage_percentage !== undefined) lines.push({ label: 'Coverage', value: `${outputs.coverage_percentage}%`, type: outputs.coverage_percentage >= 80 ? 'success' : 'warning' })
  }

  // docker-run 任务输出
  if (taskType === 'docker-run' || taskType === 'docker_run') {
    if (outputs.container_id) lines.push({ label: 'Container ID', value: outputs.container_id.substring(0, 12), type: 'info' })
    if (outputs.container_name) lines.push({ label: 'Container Name', value: outputs.container_name, type: 'info' })
    if (outputs.image_ref) lines.push({ label: 'Image Ref', value: outputs.image_ref, type: 'info' })
  }

  // shell 任务输出
  if (taskType === 'shell' || taskType === 'ssh' || taskType === 'kubernetes' || taskType === 'sleep') {
    // shell 类型输出已经在通用字段中显示
  }

  if (taskType === 'mr_quality_check') {
    if (outputs.summary) lines.push({ label: 'Summary', value: outputs.summary, type: 'info' })
    if (outputs.quality_score !== undefined) lines.push({ label: 'Quality Score', value: outputs.quality_score, type: Number(outputs.quality_score) >= 80 ? 'success' : 'warning' })
    if (outputs.issues_count !== undefined) lines.push({ label: 'Issues Count', value: outputs.issues_count, type: outputs.issues_count > 0 ? 'warning' : 'success' })
    if (Array.isArray(outputs.issues) && outputs.issues.length > 0) {
      lines.push({ label: 'Issues', value: formatTaskOutputValue(outputs.issues), type: 'default' })
    }
  }

  if (taskType === 'requirement_defect_check') {
    if (outputs.summary) lines.push({ label: 'Summary', value: outputs.summary, type: 'info' })
    if (outputs.defect_count !== undefined) lines.push({ label: 'Defect Count', value: outputs.defect_count, type: outputs.defect_count > 0 ? 'warning' : 'success' })
    if (Array.isArray(outputs.defects) && outputs.defects.length > 0) {
      lines.push({ label: 'Defects', value: formatTaskOutputValue(outputs.defects), type: 'default' })
    }
    if (Array.isArray(outputs.suggestions) && outputs.suggestions.length > 0) {
      lines.push({ label: 'Suggestions', value: formatTaskOutputValue(outputs.suggestions), type: 'info' })
    }
  }

  // 添加其他未处理的字段
  const knownKeys = ['exit_code', 'duration', 'git_commit', 'git_commit_short', 'git_ref', 'git_repo_url', 'git_checkout_path',
    'image_name', 'image_tag', 'image_full_name', 'pushed', 'artifact_path',
    'tests_passed', 'tests_failed', 'tests_skipped', 'coverage_percentage',
    'container_id', 'container_name', 'image_ref',
    'summary', 'quality_score', 'issues', 'issues_count', 'defects', 'defect_count', 'suggestions']
  for (const [key, value] of Object.entries(outputs)) {
    if (!knownKeys.includes(key) && value !== undefined && value !== null && value !== '') {
      lines.push({ label: key, value: formatTaskOutputValue(value), type: 'default' })
    }
  }

  return lines
}

const getFormattedTaskOutputs = (task) => {
  if (!task) return []
  return formatTaskOutputs(getTaskOutputs(task), getTaskOutputDisplayKind(task))
}

// 判断任务是否有输出可显示
const hasTaskOutputs = (task) => {
  return getFormattedTaskOutputs(task).length > 0
}

const isIgnoredFailureTask = (task) => {
  if (!task?.ignore_failure) return false
  return ['execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired'].includes(task.status)
}

const getTaskExitCode = (task) => {
  if (!task) return null
  if (task.exit_code !== undefined && task.exit_code !== null) return task.exit_code
  const outputs = getTaskOutputs(task)
  if (outputs && outputs.exit_code !== undefined && outputs.exit_code !== null) return outputs.exit_code
  return null
}

const shouldShowTaskExitCode = (task) => {
  const exitCode = getTaskExitCode(task)
  return exitCode !== null && exitCode !== undefined
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
const saveTriggerSettings = async (rotateSecret = false) => {
  const cronExpression = triggerSettings.schedule_enabled ? buildScheduleCronExpression() : ''
  triggerSettings.cron_expression = cronExpression

  if (!validateWebhookRuntimeMappings()) return

  triggerSettingsSaving.value = true
  syncWebhookRuntimeMappingsToTriggerSettings()

  const payload = {
    manual: true,
    webhook_enabled: webhookSectionEnabled.value,
    push_enabled: Boolean(triggerSettings.push_enabled),
    tag_enabled: Boolean(triggerSettings.tag_enabled),
    merge_request_enabled: Boolean(triggerSettings.merge_request_enabled),
    push_branch_filters: triggerSettings.push_branch_filters?.trim() || '',
    tag_filters: triggerSettings.tag_filters?.trim() || '',
    merge_request_source_branch_filters: triggerSettings.merge_request_source_branch_filters?.trim() || '',
    merge_request_target_branch_filters: triggerSettings.merge_request_target_branch_filters?.trim() || '',
    webhook_runtime_input_mappings: triggerSettings.webhook_runtime_input_mappings,
    schedule_enabled: Boolean(triggerSettings.schedule_enabled),
    cron_expression: cronExpression,
    timezone: triggerSettings.timezone?.trim() || '',
    rotate_secret: rotateSecret
  }

  if (triggerSettings.provider) {
    payload.provider = triggerSettings.provider
  }

  try {
    const response = await updatePipelineTriggers(pipelineId.value, payload)
    if (response.code === 200) {
      ElMessage.success(rotateSecret ? 'Secret Token 已轮换' : '触发设置已保存')
      await fetchTriggerSettings()
      return
    }

    ElMessage.error(response.message || '保存触发设置失败')
  } catch (error) {
    console.error('保存触发设置失败:', error)
    ElMessage.error('保存触发设置失败')
  } finally {
    triggerSettingsSaving.value = false
  }
}

const handleSaveTriggers = async () => {
  await saveTriggerSettings(false)
}

const handleRotateTriggerSecret = async () => {
  await saveTriggerSettings(true)
}

const handlePreviewWebhookRuntimeMappings = async () => {
  if (!validateWebhookRuntimeMappings()) return

  let parsedPayload
  try {
    parsedPayload = webhookRuntimeSamplePayload.value.trim()
      ? JSON.parse(webhookRuntimeSamplePayload.value)
      : {}
  } catch {
    webhookRuntimePreviewRequestError.value = '示例 Payload 不是合法 JSON'
    webhookRuntimePreviewData.value = normalizeWebhookRuntimePreviewPayload(null)
    return
  }

  syncWebhookRuntimeMappingsToTriggerSettings()
  webhookRuntimePreviewLoading.value = true
  webhookRuntimePreviewRequestError.value = ''

  try {
    const response = await previewPipelineWebhookRuntimeMappings(pipelineId.value, {
      payload: parsedPayload,
      webhook_runtime_input_mappings: triggerSettings.webhook_runtime_input_mappings
    })

    webhookRuntimePreviewData.value = normalizeWebhookRuntimePreviewPayload(response)
    if (response?.code !== 200) {
      webhookRuntimePreviewRequestError.value = response?.message || '预览失败'
    }
  } catch (error) {
    const responseData = error.response?.data || {}
    webhookRuntimePreviewData.value = normalizeWebhookRuntimePreviewPayload(responseData)
    webhookRuntimePreviewRequestError.value = responseData.message || error.message || '预览失败'
  } finally {
    webhookRuntimePreviewLoading.value = false
  }
}

const fallbackCopyTriggerField = async (text) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand('copy')
  document.body.removeChild(textarea)
  if (!copied) {
    throw new Error('copy failed')
  }
}

const copyTriggerField = async (value) => {
  await copyDisplayedValue({
    value,
    canCopy: canCopyDisplayValue,
    writeText: (text) => navigator.clipboard.writeText(text),
    fallbackWriteText: fallbackCopyTriggerField,
    onSuccess: (message) => ElMessage.success(message),
    onError: (message) => ElMessage.error(message)
  })
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
    failed: 'danger',
    execute_success: 'success',
    running: 'warning',
    execute_failed: 'danger',
    schedule_failed: 'danger',
    dispatch_timeout: 'danger',
    lease_expired: 'danger',
    assigned: 'info',
    dispatching: 'warning',
    pulling: 'warning',
    acked: 'warning',
    queued: 'warning',
    cancelled: 'info',
    not_executed: 'info',
    blocked: 'danger'
  }
  return types[status] || 'info'
}

const getStatusText = (status) => {
  const texts = {
    success: '成功',
    failed: '失败',
    execute_success: '成功',
    running: '运行中',
    execute_failed: '执行失败',
    schedule_failed: '调度失败',
    dispatch_timeout: '派发超时',
    lease_expired: '租约失效',
    assigned: '已分配',
    dispatching: '派发中',
    pulling: '等待拉取',
    acked: '已确认',
    queued: '排队中',
    cancelled: '已取消',
    not_executed: '暂未执行',
    blocked: '已阻塞'
  }
  return texts[status] || status
}

const getDistributionStatusLabel = (status) => {
  const texts = {
    success: '成功',
    failed: '失败',
    cancelled: '取消',
    running: '运行中',
    queued: '排队中',
    pending: '等待中'
  }
  return texts[status] || getStatusText(status)
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
  } else if (newTab === 'execution') {
    hydrateExecutionViewIfVisible()
  } else if (newTab === 'statistics') {
    fetchStatistics()
  }
})

// 设置 WebSocket 实时更新
const setupRealtimeUpdates = () => {
  if (realtimeHandlersBound) return

  const isActiveRealtimeRun = (payload) => {
    if (!currentRun.value?.id) return false
    const eventRunID = Number(payload?.runID ?? payload?.run_id ?? 0)
    return eventRunID > 0 && eventRunID === Number(currentRun.value.id)
  }

  realtimeHandlers.taskStatus = (payload) => {
    if (!currentRun.value || Number(payload.run_id) !== Number(currentRun.value.id)) return
    runTasks.value = applyTaskStatusPayload(runTasks.value, payload)

    if (selectedTask.value && payload.task_id) {
      const updatedTask = runTasks.value.find(task => Number(task.id || 0) === Number(payload.task_id))
      if (updatedTask && Number(selectedTask.value.id || 0) === Number(payload.task_id)) {
        selectedTask.value = updatedTask
      }
    }
    
    // 如果有选中的任务，显示错误信息
    if ((payload.status === 'execute_failed' || payload.status === 'schedule_failed' || payload.status === 'dispatch_timeout' || payload.status === 'lease_expired') && selectedTask.value?.id === payload.task_id) {
      selectedTask.value.error_msg = payload.error_msg
    }
  }

  realtimeHandlers.taskLog = (payload) => {
    if (!currentRun.value || Number(payload.run_id) !== Number(currentRun.value.id)) return
    
    // 如果有选中的任务，显示日志
    if (selectedTask.value && Number(payload.task_id) === Number(selectedTask.value.id || 0)) {
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
  }

  realtimeHandlers.runStatus = (payload) => {
    if (!currentRun.value || Number(payload.run_id) !== Number(currentRun.value.id)) return

    currentRun.value.status = payload.status
    if (payload.error_msg !== undefined) {
      currentRun.value.error_msg = payload.error_msg
      if (payload.error_msg) {
        currentRun.value.stage = payload.error_msg
      }
    }
    if (payload.duration !== undefined && payload.duration !== null) {
      currentRun.value.duration = payload.duration
    }
    if (payload.end_time !== undefined && payload.end_time !== null) {
      currentRun.value.end_time = payload.end_time
    }

    updateRunHistoryEntry(payload.run_id, {
      status: payload.status,
      error_msg: payload.error_msg,
      duration: payload.duration,
      end_time: payload.end_time
    })

    if (isRunTerminal(payload.status)) {
      fetchRunHistory()
    }

    console.log(`Pipeline run ${payload.run_id} status updated to: ${payload.status}`)
  }

  realtimeHandlers.disconnected = (payload) => {
    if (!isActiveRealtimeRun(payload)) return
    realtimeMode.value = 'reconnecting'
    console.log('实时连接已断开')
  }

  realtimeHandlers.reconnecting = (payload) => {
    if (!isActiveRealtimeRun(payload)) return
    realtimeMode.value = 'reconnecting'
  }

  realtimeHandlers.polling = (payload) => {
    if (!isActiveRealtimeRun(payload)) return
    realtimeMode.value = 'polling'
  }

  realtimeHandlers.connected = (payload) => {
    if (!isActiveRealtimeRun(payload)) return
    realtimeMode.value = 'connected'
    console.log('实时连接已建立')
  }

  realtimeHandlers.recovered = async (payload) => {
    if (!isActiveRealtimeRun(payload)) return
    realtimeMode.value = 'recovered'
    await fetchExecutionDetail()
  }

  realtime.on('task_status', realtimeHandlers.taskStatus)
  realtime.on('task_log', realtimeHandlers.taskLog)
  realtime.on('run_status', realtimeHandlers.runStatus)
  realtime.on('disconnected', realtimeHandlers.disconnected)
  realtime.on('reconnecting', realtimeHandlers.reconnecting)
  realtime.on('polling', realtimeHandlers.polling)
  realtime.on('connected', realtimeHandlers.connected)
  realtime.on('recovered', realtimeHandlers.recovered)
  realtimeHandlersBound = true
}

// 开始实时更新
const startRealtimeUpdates = (runID) => {
  realtimeMode.value = 'connecting'
  realtime.connect(runID)
}

// 停止实时连接，但保留 handlers，便于执行视图在不同 run 间重连。
const stopRealtimeUpdates = () => {
  realtime.disconnect()
  realtimeMode.value = 'idle'
}

// 仅在组件销毁时解绑 handlers，避免 stop/start 流程把监听器提前移除。
const teardownRealtimeUpdates = () => {
  if (!realtimeHandlersBound) return

  Object.entries(realtimeHandlers).forEach(([event, handler]) => {
    if (handler) {
      const normalized = event === 'taskStatus'
        ? 'task_status'
        : event === 'taskLog'
          ? 'task_log'
          : event === 'runStatus'
            ? 'run_status'
            : event
      realtime.off(normalized, handler)
    }
  })
  realtimeHandlersBound = false
}

const updateDetailContainerWidth = () => {
  if (!detailContainerRef.value) return
  detailContainerWidth.value = Math.floor(detailContainerRef.value.clientWidth)
}

onMounted(() => {
  nextTick(() => {
    updateDetailContainerWidth()
    if (typeof ResizeObserver !== 'undefined' && detailContainerRef.value) {
      detailResizeObserver = new ResizeObserver(() => {
        updateDetailContainerWidth()
      })
      detailResizeObserver.observe(detailContainerRef.value)
    }
  })
  window.addEventListener('resize', updateDetailContainerWidth)
  fetchPipelineDetail()
  fetchPipelineTaskDefinitions()
  fetchTriggerSettings()
  fetchRunHistory({ rehydrateExecution: true })
  setupRealtimeUpdates()
})

// 组件卸载时停止 WebSocket
onUnmounted(() => {
  window.removeEventListener('resize', updateDetailContainerWidth)
  if (detailResizeObserver) {
    detailResizeObserver.disconnect()
    detailResizeObserver = null
  }
  if (projectDropdownScrollCleanup) {
    projectDropdownScrollCleanup()
    projectDropdownScrollCleanup = null
  }
  stopRealtimeUpdates()
  teardownRealtimeUpdates()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.pipeline-detail-container {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-width: 0;
  min-height: 100%;
  height: 100%;
  padding: 0;
  background: var(--bg-secondary);
  border-radius: $radius-2xl;

  .detail-header {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
    margin: 0;
    padding: 12px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border-color-light);
    border-radius: $radius-xl;
    box-shadow: var(--shadow-sm);

    .header-left {
      display: flex;
      align-items: center;
      gap: 12px;
      min-width: 0;
      flex-shrink: 0;
    }

    .back-btn,
    .tab-expand {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      height: 34px;
      padding: 0 10px;
      border: none;
      border-radius: 10px;
      background: transparent;
      color: var(--text-secondary);
      cursor: pointer;
      transition: background 0.2s ease, color 0.2s ease;

      &:hover {
        background: var(--bg-secondary);
        color: var(--text-primary);
      }
    }

    .back-btn--icon {
      width: 28px;
      min-width: 28px;
      padding: 0;
      border: 1px solid var(--border-color-light);
      background: var(--bg-elevated);
    }

    .pipeline-info {
      display: flex;
      align-items: center;
      gap: 10px;
      min-width: 0;

      &.pipeline-info--compact {
        .pipeline-icon {
          width: 32px;
          height: 32px;
          display: flex;
          align-items: center;
          justify-content: center;
          flex-shrink: 0;
          border-radius: 10px;
          color: #fff;
          font-size: 14px;
          font-weight: 700;
          background: linear-gradient(140deg, var(--primary-color) 0%, var(--primary-hover) 100%);
          box-shadow: 0 8px 18px rgba(64, 158, 255, 0.24);
        }

        .pipeline-name {
          min-width: 0;
          font-size: 16px;
          font-weight: 600;
          color: var(--text-primary);
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }

        .pipeline-env-tag {
          flex-shrink: 0;
        }

        .pipeline-project-name {
          font-size: 13px;
          color: var(--text-secondary);
          white-space: nowrap;
        }
      }
    }

    .header-divider {
      color: var(--border-color);
      font-size: 14px;
      line-height: 1;
      flex-shrink: 0;
    }

    .detail-tabs {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 0;
      flex: 1;
      flex-wrap: wrap;

      .header-tab-btn {
        height: 36px;
        padding: 0 14px;
        border-radius: 10px;
        font-weight: 500;

        :deep(.el-button) {
          font-weight: 500;
        }

        :deep(.el-icon) {
          margin-right: 0;
        }

        &:not(.is-plain) {
          color: #fff;
          border-color: transparent;
          background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);

          :deep(span),
          :deep(.el-icon) {
            color: #fff;
          }
        }

        &.is-plain {
          color: var(--primary-color);
          border-color: rgba(64, 158, 255, 0.28);
          background: rgba(64, 158, 255, 0.08);

          :deep(span),
          :deep(.el-icon) {
            color: var(--primary-color);
          }

          &:hover {
            border-color: rgba(64, 158, 255, 0.42);
            background: rgba(64, 158, 255, 0.14);
          }

          .tab-count {
            background: rgba(64, 158, 255, 0.12);
            color: var(--primary-color);
          }
        }

        .tab-count {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          min-width: 18px;
          height: 18px;
          padding: 0 5px;
          border-radius: 999px;
          background: rgba(255, 255, 255, 0.16);
          font-size: 12px;
          line-height: 1;
        }
      }

      .header-tab-btn--run {
        margin-left: auto;
      }
    }
  }

  .detail-content {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    padding: 10px 0 0;

    .tab-panel {
      width: 100%;
      max-width: 100%;
      min-width: 0;
      flex: 1;
      min-height: 500px;
      background: var(--bg-card);
      border: 1px solid var(--border-color-light);
      border-radius: $radius-xl;
      box-shadow: var(--shadow-sm);
      overflow: hidden;

      .panel-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        padding: 16px 18px;
        border-bottom: 1px solid var(--border-color-light);
        flex-shrink: 0;

        h3 {
          margin: 0;
          font-size: 15px;
          font-weight: 600;
          color: var(--text-primary);
        }
      }
    }

    // 设计面板
    .design-panel {
      display: flex;
      flex: 1;
      min-height: 0;
      background: transparent;
      border: none;
      border-radius: 0;
      box-shadow: none;
      overflow: hidden;
      padding: 0;
    }

    // 历史面板
    .history-panel {
      display: flex;
      flex-direction: column;
      min-height: 0;

      .history-list {
        flex: 1;
        min-height: 0;
        overflow-y: auto;
        padding: 20px;

        .build-number {
          color: var(--primary-color);
          font-weight: 500;
        }
      }
    }

    // 统计面板
    .statistics-panel {
      display: flex;
      flex-direction: column;
      min-height: 0;

      .statistics-content {
        flex: 1;
        min-height: 0;
        overflow-y: auto;
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
            .chart-area {
              min-height: 250px;
              display: flex;
              flex-direction: column;

              &.chart-area--distribution {
                justify-content: center;
                align-items: center;
                gap: 16px;
              }
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
              overflow: hidden;
            }

            .bar-segment {
              width: 100%;
              display: flex;
              align-items: center;
              justify-content: center;

              &.failed {
                background: linear-gradient(180deg, var(--danger-color), rgba(231, 90, 90, 0.72));
              }

              &.success {
                background: linear-gradient(180deg, var(--success-color), rgba(31, 188, 132, 0.72));
              }
            }

            .bar-value,
            .bar-total {
              font-size: 12px;
              color: var(--text-secondary);
            }

            .bar-value {
              color: #fff;
              font-weight: 600;
            }

            .bar-label {
              margin-top: 10px;
              font-size: 12px;
              color: var(--text-muted);
            }

            .bar-total.empty {
              color: var(--text-muted);
            }

            .trend-summary {
              display: flex;
              gap: 16px;
              margin-top: 12px;
              flex-wrap: wrap;
              font-size: 13px;

              .trend-total {
                color: var(--text-secondary);
              }

              .trend-success {
                color: var(--success-color);
              }

              .trend-failed {
                color: var(--danger-color);
              }
            }

            .pie-container {
              position: relative;
              width: 170px;
              height: 170px;
            }

            .pie-svg {
              width: 100%;
              height: 100%;
            }

            .pie-center {
              position: absolute;
              inset: 0;
              display: flex;
              flex-direction: column;
              align-items: center;
              justify-content: center;
            }

            .pie-percentage {
              font-size: 28px;
              font-weight: 700;
              color: var(--text-primary);
            }

            .pie-label {
              margin-top: 4px;
              font-size: 12px;
              color: var(--text-muted);
            }

            .pie-legend {
              display: flex;
              gap: 12px;
              flex-wrap: wrap;
              justify-content: center;

              &.pie-legend--stacked {
                flex-direction: column;
                align-items: flex-start;
              }
            }

            .legend-item {
              display: flex;
              align-items: center;
              gap: 8px;
              font-size: 13px;
              color: var(--text-secondary);
            }

            .legend-dot {
              width: 10px;
              height: 10px;
              border-radius: 999px;

              &.success {
                background: var(--success-color);
              }

              &.failed {
                background: var(--danger-color);
              }
            }
          }
        }
        
        .build-number {
          color: var(--primary-color);
          font-weight: 500;
        }
      }
    }
    
    // 设置面板
    .settings-panel {
      display: flex;
      flex-direction: column;
      min-height: 0;

      .settings-content {
        flex: 1;
        min-height: 0;
        overflow-y: auto;
        padding: 20px;

        .settings-form {
          max-width: 600px;

          &.trigger-settings-form {
            max-width: 920px;

            .trigger-sections {
              display: flex;
              flex-direction: column;
              gap: 20px;
            }

            .trigger-section {
              padding: 20px;
              border: 1px solid #ebeef5;
              border-radius: 8px;
              background: var(--bg-card);

              &.trigger-section--compact {
                padding-bottom: 4px;
              }

              .trigger-section__header {
                display: flex;
                justify-content: space-between;
                align-items: flex-start;
                gap: 16px;
                margin-bottom: 20px;
              }

              .trigger-section__heading {
                flex: 1;
              }

              .trigger-section__title-row {
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 8px;
              }

              h4 {
                margin: 0;
                font-size: 16px;
                font-weight: 600;
                color: var(--text-primary);
              }

              p {
                margin: 0;
                font-size: 13px;
                line-height: 1.6;
                color: var(--text-secondary);
              }

              .trigger-section__body {
                margin-bottom: 20px;
              }

              .schedule-mode-group {
                display: flex;
                flex-wrap: wrap;
                gap: 8px;
              }

              .trigger-inline-field--compact {
                width: auto;
              }

              .trigger-inline-field--wrap {
                flex-wrap: wrap;
              }

              .trigger-advanced-box {
                width: 100%;
                padding: 12px 14px;
                background: var(--bg-secondary);
                border-radius: 8px;

                &.trigger-advanced-box--visible {
                  display: flex;
                  flex-direction: column;
                  gap: 8px;
                }

                .trigger-advanced-box__header {
                  display: flex;
                  justify-content: space-between;
                  align-items: center;
                  gap: 12px;
                  margin-bottom: 8px;
                  font-size: 13px;
                  color: var(--text-secondary);
                }
              }

              .trigger-meta-grid {
                display: grid;
                grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
                gap: 12px;
              }

              .trigger-meta-card {
                display: flex;
                flex-direction: column;
                gap: 6px;
                padding: 12px 14px;
                border-radius: 8px;
                background: var(--bg-secondary);
              }

              .trigger-meta-label {
                font-size: 12px;
                color: var(--text-muted);
              }

              .trigger-meta-value {
                font-size: 14px;
                font-weight: 500;
                color: var(--text-primary);
                word-break: break-word;
              }

              .webhook-event-list {
                display: flex;
                flex-direction: column;
                gap: 16px;
                margin-bottom: 20px;
              }

              .webhook-event-card {
                padding: 16px;
                border: 1px solid #ebeef5;
                border-radius: 8px;
                background: var(--bg-secondary);

                .webhook-event-card__header {
                  display: flex;
                  justify-content: space-between;
                  align-items: flex-start;
                  gap: 16px;
                  margin-bottom: 16px;
                }

                .webhook-event-card__title {
                  margin-bottom: 6px;
                  font-size: 14px;
                  font-weight: 600;
                  color: var(--text-primary);
                }

                p {
                  font-size: 12px;
                  color: var(--text-muted);
                }

                .trigger-nested-item {
                  margin-bottom: 0;
                }
              }

              .webhook-runtime-config {
                display: flex;
                flex-direction: column;
                gap: 16px;
                margin-bottom: 20px;
                padding: 16px;
                border: 1px solid #ebeef5;
                border-radius: 8px;
                background: var(--bg-secondary);

                .webhook-runtime-config__header,
                .webhook-runtime-preview__header {
                  display: flex;
                  justify-content: space-between;
                  align-items: flex-start;
                  gap: 16px;
                }

                .webhook-runtime-config__title {
                  margin-bottom: 6px;
                  font-size: 14px;
                  font-weight: 600;
                  color: var(--text-primary);
                }

                .webhook-runtime-config__footer-actions,
                .webhook-runtime-preview__actions {
                  display: flex;
                  gap: 8px;
                  flex-wrap: wrap;
                  align-items: center;
                }

                .webhook-runtime-empty {
                  padding: 12px 14px;
                  border-radius: 8px;
                  background: var(--bg-card);
                  color: var(--text-secondary);
                  font-size: 13px;
                }

                .webhook-runtime-table {
                  display: flex;
                  flex-direction: column;
                  gap: 10px;
                }

                .webhook-runtime-row-card {
                  display: flex;
                  flex-direction: column;
                  gap: 6px;
                  padding: 12px;
                  border: 1px solid #ebeef5;
                  border-radius: 8px;
                  background: var(--bg-card);
                  transition: opacity 0.2s ease;

                  &.is-deleted {
                    opacity: 0.66;
                  }
                }

                .webhook-runtime-row {
                  display: grid;
                  grid-template-columns: 32px minmax(260px, 1.4fr) minmax(240px, 1.6fr) 140px auto;
                  gap: 10px;
                  align-items: center;
                }

                .webhook-runtime-row__index {
                  font-size: 12px;
                  color: var(--text-muted);
                  text-align: center;
                }

                .webhook-runtime-row__target-card {
                  display: flex;
                  flex-direction: column;
                  gap: 4px;
                  min-width: 0;
                  padding: 10px 12px;
                  border-radius: 8px;
                  background: var(--bg-secondary);
                  border: 1px solid #ebeef5;
                }

                .webhook-runtime-row__target-label {
                  font-size: 13px;
                  font-weight: 600;
                  color: var(--text-primary);
                  white-space: nowrap;
                  overflow: hidden;
                  text-overflow: ellipsis;
                }

                .webhook-runtime-row__target-key {
                  font-size: 12px;
                  color: var(--text-muted);
                  font-family: 'Consolas', 'Monaco', monospace;
                  word-break: break-all;
                }

                .webhook-runtime-row__errors {
                  padding-left: 42px;
                  color: var(--danger-color);
                  font-size: 12px;
                  line-height: 1.5;
                }

                .webhook-runtime-preview {
                  display: flex;
                  flex-direction: column;
                  gap: 12px;
                  padding-top: 4px;
                }

                .webhook-runtime-preview__summary {
                  display: grid;
                  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
                  gap: 12px;
                }

                .webhook-runtime-preview__results {
                  display: flex;
                  flex-direction: column;
                  gap: 10px;
                }

                .webhook-runtime-preview__row {
                  display: flex;
                  flex-direction: column;
                  gap: 8px;
                  padding: 12px;
                  border-radius: 8px;
                  border: 1px solid #ebeef5;
                  background: var(--bg-card);
                }

                .webhook-runtime-preview__row-head {
                  display: flex;
                  justify-content: space-between;
                  align-items: center;
                  gap: 12px;
                  flex-wrap: wrap;
                  font-size: 13px;
                  font-weight: 600;
                  color: var(--text-primary);
                }

                .webhook-runtime-preview__row-item {
                  display: grid;
                  grid-template-columns: 84px minmax(0, 1fr);
                  gap: 8px;
                  align-items: start;
                  font-size: 12px;

                  span:first-child {
                    color: var(--text-muted);
                  }

                  span:last-child {
                    color: var(--text-primary);
                    white-space: pre-wrap;
                    word-break: break-all;
                    overflow-wrap: anywhere;
                    font-family: 'Consolas', 'Monaco', monospace;
                  }
                }

                .webhook-runtime-preview__errors {
                  padding: 10px 12px;
                  border-radius: 8px;
                  background: var(--warning-light);
                  color: #e6a23c;
                  font-size: 12px;
                  line-height: 1.6;
                }
              }
            }
          }

          .trigger-inline-field {
            display: flex;
            align-items: center;
            gap: 12px;
            width: 100%;

            :deep(.el-input) {
              flex: 1;
            }
          }
            
          .form-tip {
            margin-left: 12px;
            font-size: 12px;
            color: var(--text-muted);

            &.form-tip--inline {
              margin-left: 0;
            }

            &.form-tip--block {
              display: block;
              margin-left: 0;
            }
          }
        }
      }
    }
    
    // 执行面板
    .execution-panel {
      display: flex;
      flex-direction: column;
      min-height: 0;

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
        flex: 1;
        min-height: 0;
        overflow-y: auto;
        padding: 20px;

        .execution-summary {
          display: flex;
          gap: 16px;
          margin-bottom: 20px;
          
          .summary-item {
            flex: 1;
            
            .summary-label {
              font-size: 12px;
              color: var(--text-muted);
              margin-bottom: 8px;
            }
            
            .summary-value {
              font-size: 16px;
              font-weight: 500;
              color: var(--text-primary);
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
              gap: 12px;
              padding: 14px 16px;
              border-bottom: 1px solid #ebeef5;
              transition: background 0.3s;

              &:last-child {
                border-bottom: none;
              }

              &:hover {
                background: var(--bg-secondary);
              }

              &.task-running {
                background: var(--warning-light);
              }

              &.task-failed {
                background: var(--danger-light);
              }

              &.task-blocked {
                background: var(--warning-light);
              }

              &.task-not-executed {
                background: var(--bg-secondary);
                opacity: 0.7;
              }

              .task-status-icon {
                font-size: 22px;
                flex-shrink: 0;

                .running-icon {
                  animation: spin 1s linear infinite;
                }
              }

              .task-body {
                flex: 1;
                min-width: 0;
              }

              .task-main-line {
                display: flex;
                align-items: center;
                gap: 10px;
                min-width: 0;
                flex-wrap: wrap;
              }

              .task-title-text {
                font-size: 14px;
                font-weight: 500;
                color: var(--text-primary);
                min-width: 0;
                overflow: hidden;
                text-overflow: ellipsis;
                flex-shrink: 1;
              }

              .task-ignored-hint {
                font-size: 12px;
                font-weight: 400;
                color: var(--warning-color);
                flex-shrink: 0;
              }

              .task-inline-meta,
              .task-inline-outputs {
                display: flex;
                align-items: center;
                gap: 10px;
                min-width: 0;
                flex-wrap: wrap;
              }

              .task-inline-meta {
                font-size: 12px;
                color: var(--text-muted);
                flex-shrink: 1;

                .task-agent {
                  margin-right: 2px;
                  flex-shrink: 0;
                }

                .task-start-time,
                .task-duration,
                .task-exit-code {
                  color: var(--text-secondary);
                  flex-shrink: 0;
                }
              }

              .task-inline-outputs {
                flex-shrink: 1;

                .task-output-chip,
                .task-output-item {
                  display: flex;
                  align-items: center;
                  gap: 4px;
                  min-width: 0;
                  overflow: hidden;
                }

                .output-label {
                  color: var(--text-muted);
                  flex-shrink: 0;
                }

                .output-value {
                  min-width: 0;
                  overflow: hidden;
                  text-overflow: ellipsis;
                  font-weight: 500;
                  font-family: 'Consolas', 'Monaco', monospace;

                  &.output-primary { color: var(--primary-color); }
                  &.output-success { color: #67C23A; }
                  &.output-danger { color: #F56C6C; }
                  &.output-warning { color: #E6A23C; }
                  &.output-info { color: var(--text-secondary); }
                  &.output-default { color: var(--text-primary); }
                }
              }

              .task-error {
                margin-top: 8px;
                padding: 8px 12px;
                background: var(--danger-light);
                border-radius: 4px;
                font-size: 12px;
                color: #F56C6C;
                display: flex;
                align-items: flex-start;
                gap: 4px;
                white-space: pre-wrap;
                word-break: break-word;
              }

              .task-outputs.task-outputs--expanded {
                margin-top: 10px;
                padding: 12px;
                background: var(--bg-secondary);
                border-radius: 8px;
                font-size: 12px;

                .task-outputs-grid {
                  display: grid;
                  gap: 10px;
                }

                .task-output-item {
                  display: grid;
                  grid-template-columns: 132px minmax(0, 1fr);
                  align-items: start;
                  gap: 8px;
                  min-width: 0;
                }

                .output-label {
                  color: var(--text-muted);
                  font-weight: 600;
                  line-height: 1.6;
                }

                .output-value {
                  min-width: 0;
                  white-space: pre-wrap;
                  word-break: break-all;
                  overflow-wrap: anywhere;
                  line-height: 1.7;
                  font-weight: 500;
                  font-family: 'Consolas', 'Monaco', monospace;

                  &.output-primary { color: var(--primary-color); }
                  &.output-success { color: #67C23A; }
                  &.output-danger { color: #F56C6C; }
                  &.output-warning { color: #E6A23C; }
                  &.output-info { color: var(--text-secondary); }
                  &.output-default { color: var(--text-primary); }
                }
              }

              .task-actions {
                flex-shrink: 0;
                margin-left: 12px;
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

                  &.log-command .log-message {
                    color: #4fc1ff;
                  }

                  &.log-step .log-message {
                    color: #ffd866;
                    font-weight: 600;
                  }

                  &.log-warning-line .log-message {
                    color: #ffb454;
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

  .parameter-drawer {
    display: flex;
    flex-direction: column;
    gap: 16px;

    .parameter-drawer__meta {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      font-size: 13px;
      color: var(--text-secondary);
    }

    .parameter-node-list {
      display: flex;
      flex-direction: column;
      gap: 16px;
    }

    .parameter-node-card__header {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
    }

    .parameter-node-card__title {
      font-size: 14px;
      font-weight: 600;
      color: var(--text-primary);
    }

    .parameter-node-card__id {
      font-size: 12px;
      color: var(--text-muted);
      font-family: 'Consolas', 'Monaco', monospace;
    }

    .parameter-sections {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 16px;
    }

    .parameter-section {
      padding: 12px;
      border-radius: 8px;
      background: var(--bg-secondary);
    }

    .parameter-section__title {
      margin-bottom: 10px;
      font-size: 13px;
      font-weight: 600;
      color: var(--text-primary);
    }

    .parameter-kv-list {
      display: flex;
      flex-direction: column;
      gap: 10px;
    }

    .parameter-kv-item {
      display: grid;
      grid-template-columns: 120px minmax(0, 1fr);
      gap: 8px;
      align-items: start;
    }

    .parameter-kv-item__key {
      font-size: 12px;
      color: var(--text-muted);
      word-break: break-word;
    }

    .parameter-kv-item__value {
      font-size: 12px;
      color: var(--text-primary);
      white-space: pre-wrap;
      word-break: break-all;
      font-family: 'Consolas', 'Monaco', monospace;
    }
  }

  .rerun-preview {
    display: flex;
    flex-direction: column;
    gap: 16px;

    .rerun-preview__meta {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      font-size: 13px;
      color: var(--text-secondary);
    }

    .rerun-preview__section {
      display: flex;
      flex-direction: column;
      gap: 10px;
    }

    .rerun-preview__section-title {
      font-size: 13px;
      font-weight: 600;
      color: var(--text-primary);
    }

    .rerun-preview__detail-list {
      display: flex;
      flex-direction: column;
      gap: 10px;
    }

    .rerun-preview__detail-card {
      display: flex;
      flex-direction: column;
      gap: 8px;
      padding: 12px;
      border-radius: 8px;
      background: var(--bg-secondary);
      border: 1px solid #ebeef5;

      &.rerun-preview__detail-card--warning {
        background: var(--warning-light);
        border-color: rgba(230, 162, 60, 0.2);
      }
    }

    .rerun-preview__detail-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      flex-wrap: wrap;
    }

    .rerun-preview__detail-title {
      font-size: 13px;
      font-weight: 600;
      color: var(--text-primary);
    }

    .rerun-preview__detail-subtitle {
      font-size: 12px;
      color: var(--text-muted);
    }

    .rerun-preview__detail-item {
      display: grid;
      grid-template-columns: 96px minmax(0, 1fr);
      gap: 8px;
      align-items: start;
    }

    .rerun-preview__detail-label {
      font-size: 12px;
      color: var(--text-muted);
    }

    .rerun-preview__detail-value {
      font-size: 12px;
      color: var(--text-primary);
      line-height: 1.6;
      white-space: pre-wrap;
      word-break: break-all;
      overflow-wrap: anywhere;
      font-family: 'Consolas', 'Monaco', monospace;
    }
  }

  .manual-run-prefill-hint {
    margin-bottom: 16px;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: 13px;
    line-height: 1.6;
  }
}
</style>
