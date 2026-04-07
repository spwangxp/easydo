<template>
  <div class="deploy-container">
    <div class="deploy-header">
      <div>
        <h1 class="page-title">发布</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
      <div class="deploy-header-actions">
        <el-button v-if="deployScopeContext?.backLink" @click="goBackToScopedK8sBrowser">返回 K8s 浏览器</el-button>
        <el-button type="primary" @click="handleCreate">
          <el-icon><Plus /></el-icon>
          新建发布
        </el-button>
      </div>
    </div>

    <div v-if="deployScopeContext" class="deploy-scope-card">
      <div>
        <span class="deploy-scope-label">当前按 K8s 资源上下文查看发布记录</span>
        <strong class="deploy-scope-value">{{ deployScopeContext.resourceName }}</strong>
        <span class="deploy-scope-hint">
          目标资源已锁定
          <template v-if="deployScopeContext.namespace"> · 命名空间 {{ deployScopeContext.namespace }}</template>
        </span>
      </div>
      <el-button @click="clearDeployScope">查看全部发布</el-button>
    </div>

    <div class="deploy-overview">
      <div class="overview-card">
        <span class="overview-label">发布总数</span>
        <strong class="overview-value">{{ total }}</strong>
      </div>
      <div class="overview-card running">
        <span class="overview-label">进行中</span>
        <strong class="overview-value">{{ statusCounts.running }}</strong>
      </div>
      <div class="overview-card success">
        <span class="overview-label">成功</span>
        <strong class="overview-value">{{ statusCounts.success }}</strong>
      </div>
      <div class="overview-card danger">
        <span class="overview-label">失败 / 已取消</span>
        <strong class="overview-value">{{ statusCounts.failed }}</strong>
      </div>
    </div>

    <div class="deploy-filters card-shell">
      <div class="filter-tabs">
        <div class="tab-item" :class="{ active: statusTab === 'all' }" @click="statusTab = 'all'">
          <span>全部</span>
          <span class="tab-count">{{ total }}</span>
        </div>
        <div class="tab-item" :class="{ active: statusTab === 'running' }" @click="statusTab = 'running'">
          <span>进行中</span>
          <span class="tab-count">{{ statusCounts.running }}</span>
        </div>
        <div class="tab-item" :class="{ active: statusTab === 'success' }" @click="statusTab = 'success'">
          <span>成功</span>
          <span class="tab-count">{{ statusCounts.success }}</span>
        </div>
        <div class="tab-item" :class="{ active: statusTab === 'failed' }" @click="statusTab = 'failed'">
          <span>失败 / 已取消</span>
          <span class="tab-count">{{ statusCounts.failed }}</span>
        </div>
      </div>

      <div class="filter-controls">
        <el-input v-model="searchKeyword" placeholder="搜索发布名称" clearable style="width: 240px" />

        <el-select v-model="filterProject" placeholder="项目" clearable style="width: 160px">
          <el-option v-for="project in projectList" :key="project.id" :label="project.name" :value="project.id" />
        </el-select>

        <el-select v-model="filterEnvironment" placeholder="环境" clearable style="width: 160px">
          <el-option label="开发环境" value="development" />
          <el-option label="测试环境" value="testing" />
          <el-option label="生产环境" value="production" />
        </el-select>

        <el-button @click="fetchDeploys">刷新</el-button>
      </div>
    </div>

    <div class="deploy-table card-shell">
      <el-table
        :data="filteredDeployList"
        v-loading="loading"
        style="width: 100%"
        :default-sort="{ prop: 'created_at', order: 'descending' }"
      >
        <el-table-column prop="name" label="发布名称" min-width="220">
          <template #default="{ row }">
            <div class="deploy-name">
              <span class="name-icon">{{ row.name.charAt(0).toUpperCase() }}</span>
              <div class="name-main">
                <span class="name-text">{{ row.name }}</span>
                <span class="name-subtext">{{ row.template_type === 'llm' ? 'LLM 模板' : '应用模板' }} · 版本 {{ row.versionLabel }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column prop="environment" label="环境" width="120">
          <template #default="{ row }">
            <el-tag :type="getEnvironmentType(row.environment)" size="small">
              {{ getEnvironmentName(row.environment) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column prop="target_resource_name" label="目标资源" min-width="180" />

        <el-table-column prop="status" label="状态" width="130">
          <template #default="{ row }">
            <div class="status-cell">
              <el-icon v-if="row.status === 'success'" class="status-icon success"><CircleCheck /></el-icon>
              <el-icon v-else-if="isRunningStatus(row.status)" class="status-icon running"><Loading /></el-icon>
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

        <el-table-column prop="created_at" label="创建时间" width="170" sortable>
          <template #default="{ row }">
            {{ formatDateTime(row.created_at) }}
          </template>
        </el-table-column>

        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button link type="primary" @click="handleView(row)">查看详情</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-drawer v-model="detailVisible" :title="selectedDeployment?.name || '发布详情'" size="56%" destroy-on-close @closed="stopDetailPolling">
      <template #header>
        <div class="drawer-header" v-if="selectedDeployment">
          <div>
            <div class="drawer-title-row">
              <h2>{{ selectedDeployment.name }}</h2>
              <el-tag :type="getStatusType(selectedDeployment.status)" size="large">{{ getStatusName(selectedDeployment.status) }}</el-tag>
            </div>
            <p>{{ selectedDeployment.target_resource_name }} · {{ getEnvironmentName(selectedDeployment.environment) }}</p>
          </div>
          <div class="drawer-actions">
            <el-button @click="refreshSelectedDeployment" :loading="detailLoading">刷新详情</el-button>
            <el-button v-if="selectedDeploymentBackLink" @click="goBackToSelectedDeploymentK8sBrowser">返回 K8s 浏览器</el-button>
            <el-button v-if="canViewRunLogs" @click="viewRunLogs">运行日志</el-button>
          </div>
        </div>
      </template>

      <div v-loading="detailLoading" class="detail-content" v-if="selectedDeployment">
        <div class="detail-summary-grid">
          <div class="detail-summary-card">
            <span class="summary-label">模板版本</span>
            <strong class="summary-value">{{ selectedDeployment.versionLabel }}</strong>
          </div>
          <div class="detail-summary-card">
            <span class="summary-label">目标资源</span>
            <strong class="summary-value">{{ selectedDeployment.target_resource_name }}</strong>
          </div>
          <div class="detail-summary-card">
            <span class="summary-label">流水线运行</span>
            <strong class="summary-value">{{ getDeploymentRunLabel(selectedDeployment) }}</strong>
          </div>
          <div class="detail-summary-card">
            <span class="summary-label">发布人</span>
            <strong class="summary-value">{{ selectedDeployment.deployer }}</strong>
          </div>
        </div>

        <div v-if="selectedDeployment.validation_error" class="validation-alert">
          <el-alert type="warning" :closable="false" show-icon>
            {{ selectedDeployment.validation_error }}
          </el-alert>
        </div>

        <section class="detail-section card-shell soft">
          <div class="section-header">
            <div>
              <h3 class="section-title">发布概览</h3>
              <p class="section-description">发布请求本身保留模板、资源与参数快照；执行可见性直接在这里查看。</p>
            </div>
          </div>

          <div class="detail-meta-grid">
            <div class="meta-item">
              <span class="meta-label">模板类型</span>
              <span class="meta-value">{{ selectedDeployment.template_type === 'llm' ? 'LLM 商店' : '应用商店' }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">目标资源类型</span>
              <span class="meta-value">{{ selectedDeployment.target_resource_type === 'k8s' ? 'K8s 集群' : 'VM' }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">创建时间</span>
              <span class="meta-value">{{ formatDateTime(selectedDeployment.created_at) }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">接入地址</span>
              <span class="meta-value">{{ selectedDeployment.target_resource_endpoint || '-' }}</span>
            </div>
          </div>
        </section>

        <section class="detail-section card-shell soft">
          <div class="section-header">
            <div>
              <h3 class="section-title">执行过程</h3>
              <p class="section-description">发布页直接承接部署执行状态、任务调度与日志查看。</p>
            </div>
            <el-progress :percentage="executionProgress" :status="executionProgressStatus" :stroke-width="10" style="width: 220px" />
          </div>

          <div class="task-list" v-if="selectedDeployment.pipeline_id && selectedDeployment.pipeline_run_id">
            <div
              v-for="task in sortedRunTasks"
              :key="task.id || task.node_id || task.name"
              class="task-item"
              :class="{
                running: task.status === 'running',
                failed: failedTaskStatuses.includes(task.status),
                blocked: task.display_status === 'blocked'
              }"
            >
              <div class="task-main">
                <div class="task-name-row">
                  <span class="task-name">{{ task.name || task.node_id || `任务 #${task.id}` }}</span>
                  <el-tag :type="getStatusType(task.display_status || task.status)" size="small">{{ getStatusName(task.display_status || task.status) }}</el-tag>
                </div>
                <div class="task-meta">
                  <span>{{ task.Agent?.name || '待分配执行器' }}</span>
                  <span>开始：{{ formatDateTime(task.start_time) }}</span>
                  <span v-if="task.duration > 0">耗时：{{ formatDuration(task.duration) }}</span>
                </div>
                <div v-if="task.error_msg" class="task-error">{{ task.error_msg }}</div>
              </div>
              <div class="task-actions">
                <el-button link type="primary" :disabled="!task.id" @click="viewTaskLogs(task)">查看日志</el-button>
              </div>
            </div>

            <el-empty v-if="!detailLoading && sortedRunTasks.length === 0" description="发布已创建，但当前还没有任务执行记录" />
          </div>

          <el-empty v-else description="当前发布还没有关联到可查看的流水线执行记录" />
        </section>
      </div>
    </el-drawer>

    <LogViewer
      ref="logViewerRef"
      :task-id="logViewerContext.taskId"
      :pipeline-id="logViewerContext.pipelineId"
      :pipeline-run-id="logViewerContext.pipelineRunId"
      :title="logViewerContext.title"
    />
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { getDeploymentRequestDetail, getDeploymentRequestList } from '@/api/deployment'
import { getTemplateList } from '@/api/store'
import { getResourceList } from '@/api/resource'
import { getProjectList } from '@/api/project'
import { getPipelineRunDetail } from '@/api/pipeline'
import LogViewer from '@/views/pipeline/components/LogViewer.vue'
import { buildResourceK8sRouteLocation, resolveNamespaceFromParameterSnapshot } from '@/views/resources/k8s/utils'
import {
  Plus,
  CircleCheck,
  CircleClose,
  Loading
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const statusTab = ref('all')
const searchKeyword = ref('')
const filterProject = ref('')
const filterEnvironment = ref('')
const total = ref(0)
const loading = ref(false)
const detailLoading = ref(false)
const detailVisible = ref(false)

const projectList = ref([])
const templateMap = ref({})
const resourceMap = ref({})
const deployList = ref([])
const selectedDeployment = ref(null)
const runTasks = ref([])
const logViewerRef = ref(null)
const logViewerContext = reactive({ taskId: null, pipelineId: null, pipelineRunId: null, title: '发布日志' })
let detailPollingTimer = null

const activeStatuses = ['pending', 'validating', 'queued', 'running']
const failedTaskStatuses = ['execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired', 'cancelled', 'failed']
const scopedTargetResourceId = computed(() => Number(route.query.target_resource_id || 0))
const scopedNamespace = computed(() => String(route.query.namespace || '').trim())

const filteredDeployList = computed(() => deployList.value.filter(item => {
  if (statusTab.value === 'running' && !isRunningStatus(item.status)) return false
  if (statusTab.value === 'success' && item.status !== 'success') return false
  if (statusTab.value === 'failed' && !['failed', 'cancelled'].includes(item.status)) return false
  if (searchKeyword.value && !String(item.name || '').toLowerCase().includes(searchKeyword.value.toLowerCase())) return false
  if (filterEnvironment.value && item.environment !== filterEnvironment.value) return false
  if (filterProject.value && String(item.project_id || '') !== String(filterProject.value)) return false
  if (scopedTargetResourceId.value && Number(item.target_resource_id) !== scopedTargetResourceId.value) return false
  if (scopedNamespace.value && item.target_namespace && item.target_namespace !== scopedNamespace.value) return false
  return true
}))

const statusCounts = computed(() => ({
  running: deployList.value.filter(item => isRunningStatus(item.status)).length,
  success: deployList.value.filter(item => item.status === 'success').length,
  failed: deployList.value.filter(item => ['failed', 'cancelled'].includes(item.status)).length
}))

const sortedRunTasks = computed(() => {
  return [...runTasks.value].sort((a, b) => parseTaskOrderTime(a.created_at || a.start_time) - parseTaskOrderTime(b.created_at || b.start_time))
})

const parseJSONField = (value, fallback) => {
  if (value === null || value === undefined || value === '') return fallback
  if (typeof value === 'object') return value
  if (typeof value !== 'string') return fallback
  try {
    return JSON.parse(value)
  } catch {
    return fallback
  }
}

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
      id: latestAttempt?.task_id || 0,
      node_id: nodeID,
      name: node.node_name || snapshotNode?.node_name || snapshotNode?.name || nodeID,
      task_type: node.task_key || '',
      status: normalizedStatus,
      display_status: normalizedStatus,
      start_time: latestAttempt?.start_time || startEvent?.time || 0,
      created_at: latestAttempt?.start_time || startEvent?.time || run?.created_at || 0,
      duration: latestAttempt?.duration || 0,
      error_msg: latestAttempt?.error_msg || '',
      outputs: outputsByNode[nodeID] || {},
      _order: Number.isFinite(snapshotNode?.__index) ? snapshotNode.__index : index,
      Agent: latestAttempt?.agent_id ? { name: `Agent #${latestAttempt.agent_id}` } : null
    }
  }).sort((a, b) => a._order - b._order)
}

const executionProgress = computed(() => {
  if (!runTasks.value.length) return 0
  const completed = runTasks.value.filter(task => ['execute_success', 'execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired', 'cancelled'].includes(task.status)).length
  return Math.round((completed / runTasks.value.length) * 100)
})

const executionProgressStatus = computed(() => {
  if (!selectedDeployment.value) return ''
  if (selectedDeployment.value.status === 'success') return 'success'
  if (['failed', 'cancelled'].includes(selectedDeployment.value.status)) return 'exception'
  return ''
})

const canViewRunLogs = computed(() => !!(selectedDeployment.value?.pipeline_id && selectedDeployment.value?.pipeline_run_id))
const deployScopeContext = computed(() => {
  if (!scopedTargetResourceId.value || String(route.query.source || '').trim() !== 'resource-k8s') return null
  const resource = resourceMap.value[scopedTargetResourceId.value] || null
  if (resource?.type && resource.type !== 'k8s') return null
  return {
    targetResourceId: scopedTargetResourceId.value,
    namespace: scopedNamespace.value,
    resourceName: resource?.name || String(route.query.resource_name || '').trim() || `资源#${scopedTargetResourceId.value}`,
    backLink: buildResourceK8sRouteLocation(scopedTargetResourceId.value, scopedNamespace.value)
  }
})
const selectedDeploymentBackLink = computed(() => {
  if (selectedDeployment.value?.target_resource_type !== 'k8s' || !selectedDeployment.value?.target_resource_id) return null
  return buildResourceK8sRouteLocation(selectedDeployment.value.target_resource_id, selectedDeployment.value.target_namespace || scopedNamespace.value)
})

const normalizeSnapshot = (value) => {
  if (!value) return null
  if (typeof value === 'object') return value
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch {
      return null
    }
  }
  return null
}

const normalizeDeployment = (item) => {
  const templateVersionSnapshot = normalizeSnapshot(item.template_version_snapshot)
  const resourceSnapshot = normalizeSnapshot(item.resource_snapshot)
  const parameterSnapshot = normalizeSnapshot(item.parameter_snapshot)
  const template = templateMap.value[item.template_id] || {}
  const resource = resourceMap.value[item.target_resource_id] || {}
  return {
    id: item.id,
    name: template.name || `部署请求 #${item.id}`,
    template_id: item.template_id,
    template_type: item.template_type || template.template_type || 'app',
    template_version_id: item.template_version_id,
    versionLabel: templateVersionSnapshot?.version || item.template_version_id || '-',
    project_id: item.project_id,
    environment: resource.environment || resourceSnapshot?.environment || 'development',
    status: item.status,
    deployer: item.requested_by ? `用户#${item.requested_by}` : '-',
    created_at: item.created_at,
    validation_error: item.validation_error || '',
    target_resource_id: item.target_resource_id,
    target_resource_type: item.target_resource_type || resource.type || 'vm',
    target_resource_name: resource.name || resourceSnapshot?.name || `资源#${item.target_resource_id}`,
    target_resource_endpoint: resource.endpoint || resourceSnapshot?.endpoint || '',
    pipeline_id: item.pipeline_id,
    pipeline_run_id: item.pipeline_run_id,
    pipeline_build_number: item.pipeline_build_number || 0,
    target_namespace: resolveNamespaceFromParameterSnapshot(parameterSnapshot),
    parameter_snapshot: parameterSnapshot,
    template_version_snapshot: templateVersionSnapshot,
    resource_snapshot: resourceSnapshot
  }
}

const getDeploymentRunLabel = (deployment) => {
  if (!deployment?.pipeline_run_id) return '尚未生成'
  if (deployment.pipeline_build_number) return `#${deployment.pipeline_build_number}`
  return `运行 ID ${deployment.pipeline_run_id}`
}

const syncDeploymentWithRunDetail = async () => {
  if (!selectedDeployment.value?.pipeline_id || !selectedDeployment.value?.pipeline_run_id) {
    runTasks.value = []
    return
  }

  const runRes = await getPipelineRunDetail(selectedDeployment.value.pipeline_id, selectedDeployment.value.pipeline_run_id)
  const runRecord = runRes?.data || null
  if (!runRecord) {
    runTasks.value = []
    return
  }

  runTasks.value = buildRunTasksFromRunRecord(runRecord)
  if (runRecord.build_number) {
    selectedDeployment.value.pipeline_build_number = runRecord.build_number
  }
}

const fetchDeploys = async () => {
  loading.value = true
  try {
    const [reqRes, templateRes, resourceRes, projectRes] = await Promise.all([
      getDeploymentRequestList(),
      getTemplateList(),
      getResourceList(),
      getProjectList({ page: 1, page_size: 200 })
    ])
    const templateItems = Array.isArray(templateRes.data) ? templateRes.data : []
    const resourceItems = Array.isArray(resourceRes.data) ? resourceRes.data : []
    templateMap.value = Object.fromEntries(templateItems.map(item => [item.id, item]))
    resourceMap.value = Object.fromEntries(resourceItems.map(item => [item.id, item]))
    projectList.value = Array.isArray(projectRes.data?.list) ? projectRes.data.list : []
    const requestItems = Array.isArray(reqRes.data) ? reqRes.data : []
    deployList.value = requestItems.map(normalizeDeployment)
    total.value = deployList.value.length
  } finally {
    loading.value = false
  }
}

const refreshSelectedDeployment = async () => {
  if (!selectedDeployment.value?.id) return
  detailLoading.value = true
  try {
    const res = await getDeploymentRequestDetail(selectedDeployment.value.id)
    const item = res?.data || null
    if (!item) return
    selectedDeployment.value = normalizeDeployment(item)
    await syncDeploymentWithRunDetail()
    if (!activeStatuses.includes(selectedDeployment.value.status)) {
      stopDetailPolling()
    }
  } finally {
    detailLoading.value = false
  }
}

const startDetailPolling = () => {
  stopDetailPolling()
  if (!selectedDeployment.value || !activeStatuses.includes(selectedDeployment.value.status)) return
  detailPollingTimer = setInterval(() => {
    refreshSelectedDeployment()
  }, 5000)
}

const stopDetailPolling = () => {
  if (detailPollingTimer) {
    clearInterval(detailPollingTimer)
    detailPollingTimer = null
  }
}

const handleCreate = () => {
  if (deployScopeContext.value) {
    router.push({ path: '/store/apps', query: { ...route.query } })
    return
  }
  router.push('/store/apps')
}

const goBackToScopedK8sBrowser = () => {
  if (!deployScopeContext.value?.backLink) return
  router.push(deployScopeContext.value.backLink)
}

const goBackToSelectedDeploymentK8sBrowser = () => {
  if (!selectedDeploymentBackLink.value) return
  router.push(selectedDeploymentBackLink.value)
}

const clearDeployScope = async () => {
  const nextQuery = { ...route.query }
  delete nextQuery.target_resource_id
  delete nextQuery.namespace
  delete nextQuery.source
  delete nextQuery.resource_name
  await router.replace({ path: route.path, query: nextQuery })
}

const handleView = async (row) => {
  selectedDeployment.value = { ...row }
  detailVisible.value = true
  await refreshSelectedDeployment()
  startDetailPolling()
}

const viewRunLogs = async () => {
  if (!selectedDeployment.value?.pipeline_id || !selectedDeployment.value?.pipeline_run_id) return
  logViewerContext.taskId = null
  logViewerContext.pipelineId = selectedDeployment.value.pipeline_id
  logViewerContext.pipelineRunId = selectedDeployment.value.pipeline_run_id
  logViewerContext.title = `${selectedDeployment.value.name} · 运行日志`
  await nextTick()
  logViewerRef.value?.open()
}

const viewTaskLogs = async (task) => {
  if (!task?.id) return
  logViewerContext.taskId = task.id
  logViewerContext.pipelineId = null
  logViewerContext.pipelineRunId = null
  logViewerContext.title = `${selectedDeployment.value?.name || '发布'} · ${task.name || task.node_id || `任务 #${task.id}`}`
  await nextTick()
  logViewerRef.value?.open()
}

const parseTaskOrderTime = (value) => {
  if (!value) return Number.POSITIVE_INFINITY
  if (typeof value === 'number') {
    return value > 1e12 ? value : value * 1000
  }
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? Number.POSITIVE_INFINITY : parsed
}

const isRunningStatus = (status) => activeStatuses.includes(status)

const formatDateTime = (date) => {
  if (!date) return '-'
  const timestamp = typeof date === 'number' ? date * 1000 : date
  return new Date(timestamp).toLocaleString('zh-CN')
}

const formatDuration = (seconds) => {
  if (!seconds || seconds < 0) return '-'
  if (seconds > 31536000) {
    seconds = Math.floor(seconds / 1000)
  }
  const mins = Math.floor(seconds / 60)
  const secs = Math.floor(seconds % 60)
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`
}

const getEnvironmentType = (env) => ({ development: 'info', testing: 'warning', production: 'danger' }[env] || 'info')
const getEnvironmentName = (env) => ({ development: '开发环境', testing: '测试环境', production: '生产环境' }[env] || env || '-')

const getStatusType = (status) => ({
  success: 'success',
  execute_success: 'success',
  pending: 'info',
  validating: 'warning',
  queued: 'warning',
  running: 'warning',
  assigned: 'warning',
  dispatching: 'warning',
  pulling: 'warning',
  acked: 'warning',
  failed: 'danger',
  execute_failed: 'danger',
  schedule_failed: 'danger',
  dispatch_timeout: 'danger',
  lease_expired: 'danger',
  blocked: 'danger',
  cancelled: 'info'
}[status] || 'info')

const getStatusName = (status) => ({
  success: '成功',
  execute_success: '成功',
  pending: '待处理',
  validating: '校验中',
  queued: '排队中',
  running: '运行中',
  assigned: '已分配',
  dispatching: '派发中',
  pulling: '等待拉取',
  acked: '已确认',
  failed: '失败',
  execute_failed: '执行失败',
  schedule_failed: '调度失败',
  dispatch_timeout: '派发超时',
  lease_expired: '租约失效',
  blocked: '已阻塞',
  cancelled: '已取消'
}[status] || status || '-')

onMounted(fetchDeploys)

onUnmounted(() => {
  stopDetailPolling()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.deploy-container {
  animation: float-up 0.45s ease both;
}

.card-shell {
  border-radius: $radius-xl;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-md);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;

  &.soft {
    padding: 18px;
  }
}

.deploy-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  gap: 16px;
}

.deploy-header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.deploy-scope-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 18px;
  padding: 16px 18px;
  border-radius: $radius-xl;
  border: 1px solid rgba($primary-color, 0.18);
  background: linear-gradient(140deg, rgba($primary-color, 0.12), rgba($primary-color, 0.04));
  box-shadow: var(--shadow-sm);
}

.deploy-scope-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.deploy-scope-value {
  display: inline-block;
  margin-top: 6px;
  margin-right: 8px;
  color: var(--text-primary);
  font-size: 20px;
}

.deploy-scope-hint {
  color: var(--text-secondary);
}

.page-title {
  margin: 0;
  font-family: $font-family-display;
  font-size: 32px;
  font-weight: 760;
  letter-spacing: -0.03em;
  color: var(--text-primary);
}

.page-subtitle {
  margin-top: 8px;
  color: var(--text-secondary);
}

.deploy-overview {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 18px;
}

.overview-card,
.detail-summary-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 18px 20px;
  border-radius: $radius-xl;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-sm);

  &.running {
    background: linear-gradient(140deg, rgba($warning-color, 0.14), rgba($warning-color, 0.03));
  }

  &.success {
    background: linear-gradient(140deg, rgba($success-color, 0.14), rgba($success-color, 0.03));
  }

  &.danger {
    background: linear-gradient(140deg, rgba($danger-color, 0.14), rgba($danger-color, 0.03));
  }
}

.overview-label,
.summary-label,
.meta-label,
.section-description,
.task-meta,
.drawer-header p {
  color: var(--text-secondary);
}

.overview-value,
.summary-value,
.meta-value,
.task-name,
.name-text,
.section-title,
.drawer-title-row h2 {
  color: var(--text-primary);
}

.overview-value,
.summary-value {
  font-family: $font-family-display;
  font-size: 30px;
  line-height: 1;
}

.deploy-filters {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
  padding: 16px 18px;
}

.filter-tabs,
.filter-controls {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

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

.deploy-table {
  padding: 16px 18px;
}

.deploy-name {
  display: flex;
  align-items: center;
  gap: 12px;
}

.name-icon,
.deployer-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(140deg, $primary-color 0%, $primary-hover 100%);
}

.name-icon {
  width: 36px;
  height: 36px;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 700;
  box-shadow: 0 10px 22px rgba($primary-color, 0.28);
}

.name-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.name-text {
  font-weight: 650;
}

.name-subtext {
  color: var(--text-muted);
  font-size: 12px;
}

.status-cell,
.deployer-info,
.drawer-title-row,
.drawer-actions,
.task-name-row,
.task-actions,
.section-header {
  display: flex;
  align-items: center;
}

.status-cell {
  gap: 5px;
  color: var(--text-secondary);
}

.status-icon.success {
  color: $success-color;
}

.status-icon.running {
  color: $warning-color;
}

.status-icon.failed {
  color: $danger-color;
}

.deployer-info {
  gap: 8px;
}

.deployer-avatar {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  font-size: 11px;
  font-weight: 700;
}

.table-actions {
  display: flex;
  justify-content: flex-start;
}

.drawer-header {
  width: 100%;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.drawer-title-row {
  gap: 12px;
}

.drawer-title-row h2 {
  margin: 0;
  font-size: 24px;
}

.drawer-header p {
  margin: 8px 0 0;
}

.detail-content {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.detail-summary-grid,
.detail-meta-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.validation-alert {
  margin-top: -2px;
}

.section-header {
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}

.section-title {
  margin: 0;
  font-size: 20px;
}

.section-description {
  margin: 6px 0 0;
}

.meta-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px 16px;
  border-radius: $radius-lg;
  background: var(--bg-elevated);
  border: 1px solid var(--border-color-light);
}

.meta-value {
  font-weight: 600;
  word-break: break-all;
}

.task-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.task-item {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 18px;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);

  &.running {
    border-color: rgba($warning-color, 0.32);
  }

  &.failed,
  &.blocked {
    border-color: rgba($danger-color, 0.28);
  }
}

.task-main {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.task-name-row {
  gap: 10px;
  flex-wrap: wrap;
}

.task-name {
  font-weight: 650;
}

.task-meta {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
  font-size: 12px;
}

.task-error {
  color: $danger-color;
  font-size: 13px;
}

@media (max-width: 1200px) {
  .deploy-overview,
  .detail-summary-grid,
  .detail-meta-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .deploy-scope-card,
  .deploy-filters,
  .drawer-header,
  .section-header,
  .task-item {
    flex-direction: column;
    align-items: stretch;
  }
}

@media (max-width: 768px) {
  .deploy-header,
  .deploy-overview,
  .detail-summary-grid,
  .detail-meta-grid {
    grid-template-columns: 1fr;
  }

  .deploy-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .page-title {
    font-size: 27px;
  }
}
</style>
