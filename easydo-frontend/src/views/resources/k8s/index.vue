<template>
  <div class="k8s-browser-page">
    <PageHeader>
      <template #title><h1>K8s 资源浏览</h1></template>
      <template #subtitle>
        在资源管理下直接浏览 {{ overview?.name || `集群 #${resourceId}` }} 的命名空间与资源，并将安全操作统一写入资源审计。
        当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}
      </template>
      <template #actions>
        <PageHeaderActions>
          <el-button @click="goBackToResources">返回资源管理</el-button>
          <el-button v-if="selectedNamespace" @click="goToDeployRecords">发布记录</el-button>
          <el-button v-if="selectedNamespace" type="primary" @click="goToStoreDeploy">前往商店部署</el-button>
        </PageHeaderActions>
      </template>
    </PageHeader>

    <K8sOverviewPanel :overview="overview" :loading="loadingOverview" />

    <div class="namespace-context" v-if="selectedNamespace">
      <div>
        <span class="context-label">当前命名空间</span>
        <strong class="context-value">{{ selectedNamespace }}</strong>
        <span class="context-hint">从这里发起商店部署时会自动带入目标集群与命名空间。</span>
      </div>
      <el-tag type="info" size="large">{{ filteredResources.length }} 个资源</el-tag>
    </div>

    <div class="browser-grid">
      <K8sNamespaceList
        :namespaces="filteredNamespaces"
        :selected-namespace="selectedNamespace"
        :keyword="namespaceKeyword"
        :loading="loadingNamespaces"
        :querying="queryingNamespaces"
        @refresh="loadNamespaces"
        @select="handleNamespaceSelect"
        @update:keyword="namespaceKeyword = $event"
      />

      <div class="browser-main">
        <K8sResourceTable
          :namespace="selectedNamespace"
          :items="filteredResources"
          :selected-kinds="selectedKinds"
          :keyword="resourceKeyword"
          :loading="loadingResources"
          :querying="queryingResources"
          :can-operate="canOperate"
          @refresh="loadResources"
          @request-action="openActionDialog"
          @update:selectedKinds="handleKindsChange"
          @update:keyword="resourceKeyword = $event"
        />

        <K8sAuditList :audits="audits" :loading="loadingAudits" @refresh="loadAudits" />
      </div>
    </div>

    <K8sActionDialog
      v-model="actionDialogVisible"
      :namespace="selectedNamespace"
      :resource-item="actionTarget"
      :submitting="submittingAction"
      @submit="submitAction"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import PageHeader from '../../store/components/PageHeader.vue'
import PageHeaderActions from '../../store/components/PageHeaderActions.vue'
import {
  createResourceK8sAction,
  getResourceActionAudits,
  getResourceK8sOverview,
  queryResourceK8sNamespaces,
  queryResourceK8sResources
} from '@/api/resource'
import { getTaskDetail } from '@/api/task'
import K8sActionDialog from './components/K8sActionDialog.vue'
import K8sAuditList from './components/K8sAuditList.vue'
import K8sNamespaceList from './components/K8sNamespaceList.vue'
import K8sOverviewPanel from './components/K8sOverviewPanel.vue'
import K8sResourceTable from './components/K8sResourceTable.vue'
import {
  DEFAULT_K8S_KINDS,
  extractTaskErrorMessage,
  extractTaskStdout,
  normalizeK8sOverview,
  normalizeK8sResourceItem,
  normalizeNamespaceItem,
  parseKubectlListOutput,
  sortK8sResources,
  sortNamespaces
} from './utils'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const resourceId = computed(() => Number(route.params.id || 0))
const canOperate = computed(() => userStore.hasPermission('resource.operate'))

const loadingOverview = ref(false)
const loadingNamespaces = ref(false)
const loadingResources = ref(false)
const loadingAudits = ref(false)
const queryingNamespaces = ref(false)
const queryingResources = ref(false)
const submittingAction = ref(false)

const overview = ref(null)
const namespaces = ref([])
const resources = ref([])
const audits = ref([])

const namespaceKeyword = ref('')
const resourceKeyword = ref('')
const selectedNamespace = ref('')
const selectedKinds = ref([...DEFAULT_K8S_KINDS])
const actionDialogVisible = ref(false)
const actionTarget = ref(null)

let namespaceRequestToken = 0
let resourceRequestToken = 0
let auditRequestToken = 0

const filteredNamespaces = computed(() => {
  const keyword = namespaceKeyword.value.trim().toLowerCase()
  if (!keyword) return namespaces.value
  return namespaces.value.filter(item => String(item.name || '').toLowerCase().includes(keyword))
})

const filteredResources = computed(() => {
  const keyword = resourceKeyword.value.trim().toLowerCase()
  if (!keyword) return resources.value
  return resources.value.filter(item => [item.name, item.kind, item.statusText, item.summaryText].some(value => String(value || '').toLowerCase().includes(keyword)))
})

const scopedNavigationQuery = computed(() => ({
  target_resource_id: String(resourceId.value),
  namespace: selectedNamespace.value,
  source: 'resource-k8s',
  resource_name: overview.value?.name || ''
}))

const resolvePreferredNamespace = (items) => {
  const queryNamespace = String(route.query.namespace || '').trim()
  const candidateNames = [queryNamespace, selectedNamespace.value, 'default']
  const availableNames = new Set(items.map(item => item.name))
  const matched = candidateNames.find(name => name && availableNames.has(name))
  return matched || items[0]?.name || ''
}

const syncNamespaceIntoRoute = async (namespace) => {
  const nextQuery = { ...route.query }
  if (namespace) {
    nextQuery.namespace = namespace
  } else {
    delete nextQuery.namespace
  }
  await router.replace({ name: 'ResourceK8sBrowser', params: { id: String(resourceId.value) }, query: nextQuery })
}

const waitForTaskSuccess = async (taskId) => {
  const deadline = Date.now() + 180000
  while (Date.now() < deadline) {
    const res = await getTaskDetail(taskId)
    const task = res?.data || {}
    const status = task.status || ''
    if (status === 'execute_success') {
      return task
    }
    if (['execute_failed', 'schedule_failed', 'dispatch_timeout', 'lease_expired', 'cancelled'].includes(status)) {
      throw new Error(extractTaskErrorMessage(task))
    }
    await new Promise(resolve => setTimeout(resolve, 2000))
  }
  throw new Error('任务执行超时，请稍后重试')
}

const loadOverview = async () => {
  if (!resourceId.value) return
  loadingOverview.value = true
  try {
    const res = await getResourceK8sOverview(resourceId.value)
    overview.value = normalizeK8sOverview(res?.data || {})
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '加载集群概览失败')
  } finally {
    loadingOverview.value = false
  }
}

const loadNamespaces = async () => {
  if (!resourceId.value) return
  loadingNamespaces.value = true
  queryingNamespaces.value = true
  const requestToken = ++namespaceRequestToken
  try {
    const res = await queryResourceK8sNamespaces(resourceId.value, {
      keyword: namespaceKeyword.value.trim()
    })
    const taskId = Number(res?.data?.task_id || 0)
    if (!taskId) {
      throw new Error('未获取到命名空间查询任务 ID')
    }
    const task = await waitForTaskSuccess(taskId)
    if (requestToken !== namespaceRequestToken) return
    const items = sortNamespaces(parseKubectlListOutput(extractTaskStdout(task)).map(normalizeNamespaceItem))
    namespaces.value = items
    const preferredNamespace = resolvePreferredNamespace(items)
    if (preferredNamespace && preferredNamespace !== selectedNamespace.value) {
      selectedNamespace.value = preferredNamespace
      return
    }
    if (!preferredNamespace) {
      selectedNamespace.value = ''
      resources.value = []
      audits.value = []
      return
    }
    await Promise.all([loadResources(), loadAudits()])
  } catch (error) {
    if (requestToken === namespaceRequestToken) {
      ElMessage.error(error?.response?.data?.message || error?.message || '加载命名空间失败')
    }
  } finally {
    if (requestToken === namespaceRequestToken) {
      loadingNamespaces.value = false
      queryingNamespaces.value = false
    }
  }
}

const loadResources = async () => {
  if (!resourceId.value || !selectedNamespace.value) {
    resources.value = []
    return
  }

  loadingResources.value = true
  queryingResources.value = true
  const requestToken = ++resourceRequestToken
  try {
    const res = await queryResourceK8sResources(resourceId.value, {
      namespace: selectedNamespace.value,
      kinds: selectedKinds.value,
      keyword: resourceKeyword.value.trim()
    })
    const taskId = Number(res?.data?.task_id || 0)
    if (!taskId) {
      throw new Error('未获取到资源查询任务 ID')
    }
    const task = await waitForTaskSuccess(taskId)
    if (requestToken !== resourceRequestToken) return
    resources.value = sortK8sResources(parseKubectlListOutput(extractTaskStdout(task)).map(normalizeK8sResourceItem))
  } catch (error) {
    if (requestToken === resourceRequestToken) {
      ElMessage.error(error?.response?.data?.message || error?.message || '加载命名空间资源失败')
    }
  } finally {
    if (requestToken === resourceRequestToken) {
      loadingResources.value = false
      queryingResources.value = false
    }
  }
}

const loadAudits = async () => {
  if (!resourceId.value) return
  loadingAudits.value = true
  const requestToken = ++auditRequestToken
  try {
    const res = await getResourceActionAudits(resourceId.value, {
      domain: 'k8s',
      namespace: selectedNamespace.value || undefined
    })
    if (requestToken !== auditRequestToken) return
    audits.value = Array.isArray(res?.data) ? res.data : []
  } catch (error) {
    if (requestToken === auditRequestToken) {
      ElMessage.error(error?.response?.data?.message || error?.message || '加载操作审计失败')
    }
  } finally {
    if (requestToken === auditRequestToken) {
      loadingAudits.value = false
    }
  }
}

const handleNamespaceSelect = (namespace) => {
  if (!namespace || namespace === selectedNamespace.value) return
  selectedNamespace.value = namespace
}

const handleKindsChange = (value) => {
  selectedKinds.value = Array.isArray(value) && value.length > 0 ? value : [...DEFAULT_K8S_KINDS]
}

const openActionDialog = (resource) => {
  if (!canOperate.value || !resource?.actionOptions?.length) return
  actionTarget.value = resource
  actionDialogVisible.value = true
}

const submitAction = async (payload) => {
  if (!resourceId.value) return
  submittingAction.value = true
  try {
    await createResourceK8sAction(resourceId.value, payload)
    ElMessage.success('K8s 操作已提交，已写入资源审计')
    actionDialogVisible.value = false
    await loadAudits()
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '提交 K8s 操作失败')
  } finally {
    submittingAction.value = false
  }
}

const goBackToResources = () => {
  router.push('/resources')
}

const goToStoreDeploy = () => {
  router.push({ path: '/store/apps', query: scopedNavigationQuery.value })
}

const goToDeployRecords = () => {
  router.push({ path: '/deploy', query: scopedNavigationQuery.value })
}

watch(
  () => selectedNamespace.value,
  async (namespace, previous) => {
    if (namespace === previous) return
    await syncNamespaceIntoRoute(namespace)
    await Promise.all([loadResources(), loadAudits()])
  }
)

watch(
  () => [...selectedKinds.value].join(','),
  () => {
    if (!selectedNamespace.value) return
    loadResources().catch(() => {})
  }
)

watch(
  () => route.query.namespace,
  value => {
    const namespace = String(value || '').trim()
    if (!namespace || namespace === selectedNamespace.value) return
    if (namespaces.value.some(item => item.name === namespace)) {
      selectedNamespace.value = namespace
    }
  }
)

onMounted(async () => {
  if (!resourceId.value) {
    ElMessage.warning('未找到目标资源')
    router.push('/resources')
    return
  }
  await Promise.all([loadOverview(), loadNamespaces()])
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.k8s-browser-page {
  display: flex;
  flex-direction: column;
  gap: $space-5;
  padding: $space-6;
}

:deep(.page-header-subtitle) {
  max-width: 920px;
  line-height: 1.8;
}

.namespace-context {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: $space-4;
  padding: $space-4 $space-5;
  border-radius: $radius-xl;
  border: 1px solid rgba($primary-color, 0.16);
  background: linear-gradient(140deg, rgba($primary-color, 0.12), rgba($primary-color, 0.04));
  box-shadow: var(--shadow-sm);
}

.context-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.context-value {
  display: inline-block;
  margin-top: $space-1;
  margin-right: $space-2;
  font-size: 20px;
  color: var(--text-primary);
}

.context-hint {
  color: var(--text-secondary);
}

.browser-grid {
  display: grid;
  grid-template-columns: 320px minmax(0, 1fr);
  gap: $space-5;
  align-items: start;
}

.browser-main {
  display: flex;
  flex-direction: column;
  gap: $space-5;
  min-width: 0;
}

@media (max-width: 1200px) {
  .browser-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .k8s-browser-page {
    padding: $space-4;
  }

  .namespace-context {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
