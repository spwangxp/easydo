<template>
  <div class="ai-store-page">
    <PageHeader>
      <template #title>
        <StoreKindSwitch :model-value="storeKind" @update:model-value="handleStoreTabChange" />
      </template>
      <template #subtitle>模型、Provider、Deployment、Runtime 一体查看。</template>
      <template #actions>
        <PageHeaderActions>
          <el-input
            v-model="filters.keyword"
            clearable
            placeholder="搜索模型 / Provider / Runtime"
            style="width: 280px"
          />
          <el-button @click="openImportModelDialog">导入模型</el-button>
          <el-button type="primary" @click="openDeployDialog">部署模型</el-button>
          <el-button type="primary" plain @click="openProviderDialog">接入外部 Provider</el-button>
        </PageHeaderActions>
      </template>
    </PageHeader>

    <section class="card-shell section-shell">
      <div class="section-header">
        <div>
          <h2>模型总览</h2>
          <p>按模型聚合展示 Provider、部署与 Runtime 引用，并支持单行展开查看详情。</p>
        </div>
      </div>

      <el-table
        v-loading="loading"
        :data="modelRows"
        :expand-row-keys="expandedRowKeys"
        row-key="id"
        empty-text="暂无 AI 模型"
        @row-click="toggleExpandedRow"
        @expand-change="handleExpandChange"
      >
        <el-table-column type="expand" width="56">
          <template #default="{ row }">
            <div v-if="isRowExpanded(row)" class="expand-panel">
              <div class="detail-block">
                <div class="detail-block-header">
                  <strong>Providers</strong>
                  <span class="detail-count">{{ row.providerCount }}</span>
                </div>
                <el-table :data="row.providers" size="small" empty-text="暂无 Provider">
                  <el-table-column prop="name" label="Provider 名称" min-width="180" />
                  <el-table-column prop="source" label="来源" min-width="120" />
                  <el-table-column prop="endpoint" label="Endpoint" min-width="240" />
                  <el-table-column prop="status" label="状态" width="120" />
                  <el-table-column prop="binding_key" label="Binding Key" min-width="180" />
                  <el-table-column label="操作" width="180" fixed="right">
                    <template #default>
                      <div class="table-actions">
                        <el-button link type="primary">编辑</el-button>
                        <el-button link type="danger">删除</el-button>
                        <el-button link type="primary">新增 Binding</el-button>
                      </div>
                    </template>
                  </el-table-column>
                </el-table>
              </div>

              <div class="detail-block">
                <div class="detail-block-header">
                  <strong>Deployments</strong>
                  <span class="detail-count">{{ row.deploymentCount }}</span>
                </div>
                <el-table :data="row.deployments" size="small" empty-text="暂无 Deployment">
                  <el-table-column prop="name" label="部署名" min-width="180">
                    <template #default="{ row: deployment }">{{ deployment.name || deployment.resource_name || '-' }}</template>
                  </el-table-column>
                  <el-table-column prop="resource_name" label="资源" min-width="160" />
                  <el-table-column prop="template_name" label="模板" min-width="160" />
                  <el-table-column prop="version_label" label="版本" min-width="160" />
                  <el-table-column prop="status" label="状态" width="120" />
                  <el-table-column prop="provider_name" label="生成的 Provider" min-width="180" />
                  <el-table-column label="操作" width="180" fixed="right">
                    <template #default>
                      <div class="table-actions">
                        <el-button link type="primary">查看</el-button>
                        <el-button link type="primary">跳转部署详情</el-button>
                      </div>
                    </template>
                  </el-table-column>
                </el-table>
              </div>

              <div class="detail-block">
                <div class="detail-block-header">
                  <strong>Runtime Usage</strong>
                  <span class="detail-count">{{ row.runtimeCount }}</span>
                </div>
                <el-table :data="row.runtimeUsage" size="small" empty-text="暂无 Runtime 引用">
                  <el-table-column prop="runtime_name" label="Runtime 名称" min-width="180" />
                  <el-table-column prop="agent_name" label="Agent" min-width="180" />
                  <el-table-column prop="binding_priority_text" label="Binding 优先级" min-width="200" />
                  <el-table-column prop="status" label="状态" width="120" />
                  <el-table-column label="操作" width="140" fixed="right">
                    <template #default>
                      <div class="table-actions">
                        <el-button link type="primary">查看 Runtime</el-button>
                      </div>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="模型名" min-width="220" />
        <el-table-column prop="parameterSize" label="参数大小" min-width="120" />
        <el-table-column prop="modalitiesText" label="模态" min-width="160" />
        <el-table-column prop="source" label="来源" min-width="120" />
        <el-table-column prop="deploymentCount" label="已部署数" width="120" />
        <el-table-column prop="providerCount" label="Provider 数" width="120" />
        <el-table-column prop="runtimeCount" label="Runtime 引用数" width="140" />
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button link type="primary" @click.stop="toggleExpandedRow(row)">
                {{ isRowExpanded(row) ? '收起' : '展开' }}
              </el-button>
              <el-button link type="primary" @click.stop="openDeployDialog(row)">部署</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="dialogs.deploy" title="部署模型" width="920px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="模型" required>
          <el-select v-model="deployForm.modelId" filterable style="width: 100%" @change="handleDeployModelChange">
            <el-option v-for="item in aiState.models" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="部署模板" required>
          <el-select v-model="deployForm.templateId" filterable style="width: 100%" @change="handleDeployTemplateChange">
            <el-option
              v-for="item in deployTemplates"
              :key="item.id"
              :label="`${item.name} · ${item.target_resource_type === 'k8s' ? 'K8s' : 'VM'}`"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="版本 / 部署参数" required>
          <el-select v-model="deployForm.templateVersionId" filterable style="width: 100%" @change="handleDeployVersionChange">
            <el-option
              v-for="item in deployTemplateVersions"
              :key="item.id"
              :label="item.version"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="目标资源" required>
          <el-select v-model="deployForm.targetResourceId" filterable style="width: 100%">
            <el-option
              v-for="item in availableDeployResources"
              :key="item.id"
              :label="`${item.name} · ${item.endpoint || item.type}`"
              :value="item.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item v-if="canSelectGpuDevices" label="GPU">
          <el-select
            v-model="selectedGpuDeviceKeys"
            multiple
            collapse-tags
            collapse-tags-tooltip
            placeholder="选择 GPU"
            style="width: 100%"
          >
            <el-option
              v-for="device in selectedResourceGpuDevices"
              :key="device.deviceKey"
              :label="device.label"
              :value="device.deviceKey"
            />
          </el-select>
        </el-form-item>

        <div class="deploy-vram-estimate-panel">
          <div class="deploy-vram-estimate-header">
            <div>
              <h3>显存估算</h3>
              <p>{{ deployVramEstimateViewModel.message }}</p>
            </div>
            <el-button v-if="deployVramEstimateViewModel.showRetry" link type="primary" @click="retryResourceGpuRefresh">重试</el-button>
          </div>
          <div class="deploy-vram-estimate-status">
            <el-tag>{{ deployVramEstimateViewModel.displayStatusLabel }}</el-tag>
            <span class="deploy-vram-estimate-resource-state">资源状态：{{ deployVramEstimateViewModel.resourceStateLabel }}</span>
          </div>
          <div class="deploy-vram-estimate-metrics">
            <div v-for="item in deployVramEstimateViewModel.summary" :key="item.label" class="deploy-vram-estimate-metric">
              <span class="estimate-label">{{ item.label }}</span>
              <strong>{{ item.value || '-' }}</strong>
            </div>
          </div>
          <div class="deploy-vram-estimate-composition">
            <div class="deploy-vram-estimate-subtitle">显存组成</div>
            <div class="deploy-vram-estimate-inline-list">
              <div v-for="item in deployVramEstimateViewModel.composition || []" :key="item.label" class="deploy-vram-estimate-inline-item">
                <span>{{ item.label }}</span>
                <strong>{{ item.value || '-' }}</strong>
                <em>{{ item.hint || '-' }}</em>
              </div>
            </div>
          </div>
          <div class="deploy-vram-estimate-selection">
            <div class="deploy-vram-estimate-subtitle">当前组合</div>
            <div class="deploy-vram-estimate-inline-list">
              <div v-for="item in deployVramEstimateViewModel.selection || []" :key="item.label" class="deploy-vram-estimate-inline-item">
                <span>{{ item.label }}</span>
                <strong>{{ item.value || '-' }}</strong>
              </div>
            </div>
          </div>
        </div>

        <StoreParameterFields
          v-model="deployForm.parameters"
          :basic-fields="deployBasicFields"
          :advanced-fields="deployAdvancedFields"
          :advanced-title="`高级配置（${deployAdvancedFields.length} 项）`"
          :default-open-advanced="deployAdvancedFields.length > 0"
        />
      </el-form>
      <template #footer>
        <el-button @click="dialogs.deploy = false">取消</el-button>
        <el-button type="primary" :loading="deploySubmitting" @click="submitDeploy">开始部署</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialogs.importModel" title="导入模型元数据" width="560px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="模型来源" required>
          <el-select v-model="importModelForm.source" style="width: 100%">
            <el-option label="Hugging Face" value="huggingface" />
            <el-option label="ModelScope" value="modelscope" />
          </el-select>
        </el-form-item>
        <el-form-item label="模型 ID" required>
          <el-input v-model="importModelForm.sourceModelId" placeholder="如 Qwen/Qwen2.5-7B-Instruct" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.importModel = false">取消</el-button>
        <el-button type="primary" :loading="importSubmitting" @click="submitImportModel">导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialogs.provider" title="接入外部 Provider" width="720px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="Provider 名称" required>
          <el-input v-model="providerForm.providerName" />
        </el-form-item>
        <el-form-item label="Endpoint" required>
          <el-input v-model="providerForm.endpoint" placeholder="https://api.example.com/v1" />
        </el-form-item>
        <el-form-item label="Credential" required>
          <CredentialSelector
            v-model="providerForm.credentialId"
            :credential-types="['TOKEN', 'OAUTH2', 'PASSWORD']"
            credential-category="custom"
            placeholder="选择用于 Provider 的凭据"
            :create-route-query="{ category: 'custom', type: 'TOKEN', source: 'ai-provider' }"
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="providerForm.status" style="width: 100%">
            <el-option label="active" value="active" />
            <el-option label="draft" value="draft" />
          </el-select>
        </el-form-item>

        <el-form-item label="立即绑定模型">
          <el-switch v-model="providerForm.bindModelNow" />
        </el-form-item>

        <template v-if="providerForm.bindModelNow">
          <el-form-item label="模型" required>
            <el-select v-model="providerForm.modelId" filterable style="width: 100%">
              <el-option v-for="item in aiState.models" :key="item.id" :label="item.name" :value="item.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="Provider Model Key" required>
            <el-input v-model="providerForm.providerModelKey" placeholder="如 qwen2.5-7b-instruct" />
          </el-form-item>
          <el-collapse>
            <el-collapse-item title="Binding 高级配置">
              <el-form-item label="Capabilities JSON">
                <el-input v-model="providerForm.capabilitiesJSON" type="textarea" :rows="3" placeholder='{"chat":true}' />
              </el-form-item>
              <el-form-item label="Binding Settings JSON">
                <el-input v-model="providerForm.bindingSettingsJSON" type="textarea" :rows="3" placeholder='{"temperature":0.2}' />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </template>

        <el-collapse>
          <el-collapse-item title="Provider 高级配置">
            <el-form-item label="Headers JSON">
              <el-input v-model="providerForm.headersJSON" type="textarea" :rows="3" placeholder='{"Authorization":"Bearer ..."}' />
            </el-form-item>
            <el-form-item label="Settings JSON">
              <el-input v-model="providerForm.settingsJSON" type="textarea" :rows="3" placeholder='{"timeout":30000}' />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.provider = false">取消</el-button>
        <el-button type="primary" @click="submitProviderDemo">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import CredentialSelector from '@/views/pipeline/components/CredentialSelector.vue'
import PageHeaderActions from './components/PageHeaderActions.vue'
import StoreKindSwitch from './components/StoreKindSwitch.vue'
import PageHeader from './components/PageHeader.vue'
import StoreParameterFields from './components/StoreParameterFields.vue'
import { createDeploymentRequest } from '@/api/deployment'
import { getResourceDetail, getResourceList, refreshResourceBaseInfo } from '@/api/resource'
import {
  getTemplateList,
  getTemplateVersions,
  importLocalAIModel,
  listAIAgents,
  listAIModels,
  listAIProviders,
  listAIRuntimeProfiles,
  createAIProvider,
  createAIModelBinding
} from '@/api/store'
import { splitParametersByAdvanced } from './appStoreHelpers'
import { buildDeployVramEstimate, buildDeployVramEstimateViewModel } from './aiVramEstimate'
import {
  beginGpuInfoRefresh,
  createGpuInfoCacheEntry,
  createGpuRefreshToken,
  invalidateGpuInfoRefresh,
  markGpuInfoTimeout,
  normalizeResourceGpuInfo,
  resolveGpuInfoTerminalState,
  shouldRefreshResourceGpuInfo
} from './aiResourceGpuInfo'
import {
  buildAIDeploymentRequestPayload,
  buildAIModelImportPayload,
  buildDemoDeploymentRecord,
  buildDemoProviderRecord,
  buildDeployParameterState,
  buildModelRows,
  getInvalidJsonFieldLabels,
  normalizeAiStoreState
} from './aiStoreConfig'

const router = useRouter()
const loading = ref(false)
const deploySubmitting = ref(false)
const importSubmitting = ref(false)
const filters = reactive({ keyword: '' })
const dialogs = reactive({ deploy: false, provider: false, importModel: false })
const expandedRowId = ref(null)
const deployTemplates = ref([])
const deployTemplateVersions = ref([])
const deployResources = ref([])
const resourceGpuInfoCache = reactive({})
const resourceRefreshTimers = new Map()
const selectedGpuDeviceKeys = ref([])
const RESOURCE_GPU_REFRESH_POLL_MS = 1500
const RESOURCE_GPU_REFRESH_TIMEOUT_MS = 15000
const DEPLOY_VRAM_IDLE_MESSAGE = '先选择目标资源以继续核对 GPU 容量；模型侧显存需求已先行估算。'

const aiState = reactive({
  models: [],
  providers: [],
  agents: [],
  runtimeProfiles: [],
  deployments: []
})

const localDemoState = reactive({
  providers: [],
  deployments: []
})

const deployForm = reactive(createDeployForm())
const providerForm = reactive(createProviderForm())
const importModelForm = reactive(createImportModelForm())

const mergedState = computed(() => normalizeAiStoreState({
  models: aiState.models,
  providers: [...aiState.providers, ...localDemoState.providers],
  agents: aiState.agents,
  runtimeProfiles: aiState.runtimeProfiles,
  deployments: [...aiState.deployments, ...localDemoState.deployments]
}))

const modelRows = computed(() => buildModelRows({
  models: mergedState.value.models,
  providers: mergedState.value.providers,
  runtimeProfiles: mergedState.value.runtimeProfiles,
  agents: mergedState.value.agents,
  deployments: mergedState.value.deployments,
  keyword: filters.keyword
}))

const expandedRowKeys = computed(() => (expandedRowId.value ? [expandedRowId.value] : []))
const selectedDeployModel = computed(() => modelRows.value.find((item) => String(item.id) === String(deployForm.modelId)) || null)
const selectedProviderModel = computed(() => modelRows.value.find((item) => String(item.id) === String(providerForm.modelId)) || null)
const selectedDeployTemplate = computed(() => deployTemplates.value.find((item) => String(item.id) === String(deployForm.templateId)) || null)
const availableDeployResources = computed(() => {
  if (!selectedDeployTemplate.value?.target_resource_type) return deployResources.value
  return deployResources.value.filter((item) => item.type === selectedDeployTemplate.value.target_resource_type)
})
const selectedDeployResource = computed(() => availableDeployResources.value.find((item) => String(item.id) === String(deployForm.targetResourceId)) || null)
const selectedResourceGpuEntry = computed(() => ensureSelectedResourceGpuEntry(selectedDeployResource.value))
const selectedResourceGpuDevices = computed(() => selectedResourceGpuEntry.value?.gpuDevices || [])
const selectedGpuDevices = computed(() => {
  const selectedKeys = new Set(selectedGpuDeviceKeys.value.map((item) => String(item)))
  return selectedResourceGpuDevices.value.filter((device) => selectedKeys.has(String(device.deviceKey)))
})
const canSelectGpuDevices = computed(() => selectedResourceGpuDevices.value.length > 0)
const deployVramEstimate = computed(() => buildDeployVramEstimate({
  model: selectedDeployModel.value || {},
  parameterValues: deployForm.parameters || {},
  gpuDevices: selectedResourceGpuDevices.value,
  selectedGpuDeviceKeys: selectedGpuDeviceKeys.value
}))
const deployVramEstimateViewModel = computed(() => {
  const baseViewModel = buildDeployVramEstimateViewModel({
    resourceState: selectedDeployResource.value ? selectedResourceGpuEntry.value?.status : 'idle',
    resourceError: selectedResourceGpuEntry.value?.error,
    estimate: deployVramEstimate.value
  })

  return {
    ...baseViewModel,
    message: (selectedDeployResource.value ? selectedResourceGpuEntry.value?.status : 'idle') === 'idle'
      ? DEPLOY_VRAM_IDLE_MESSAGE
      : baseViewModel.message,
    displayStatusLabel: mapEstimateDisplayStatusLabel(baseViewModel.displayStatus),
    resourceStateLabel: mapResourceGpuStateLabel(selectedDeployResource.value ? selectedResourceGpuEntry.value?.status : 'idle'),
    breakdown: (baseViewModel.breakdown || []).map((item) => ({
      ...item,
      label: mapEstimateBreakdownLabel(item.label)
    }))
  }
})
const deployParameterGroups = computed(() => splitParametersByAdvanced(deployForm.parameterFields || []))
const deployBasicFields = computed(() => deployParameterGroups.value.basic)
const deployAdvancedFields = computed(() => deployParameterGroups.value.advanced)

onMounted(() => {
  loadData()
})

onBeforeUnmount(() => {
  clearResourceRefreshTimers()
})

watch(() => dialogs.deploy, (visible) => {
  if (!visible) {
    resetDeployDialogRuntimeState()
  }
})

watch(selectedResourceGpuDevices, () => {
  syncSelectedGpuDevices()
  syncSelectedGpuIntoParameters()
}, { immediate: true })

watch(() => deployForm.targetResourceId, (resourceId, previousResourceId) => {
  if (previousResourceId && String(previousResourceId) !== String(resourceId)) {
    invalidateResourceGpuRefresh(previousResourceId)
  }
  syncSelectedGpuDevices()
  syncSelectedGpuIntoParameters()
  if (resourceId) {
    ensureResourceGpuInfo(resourceId)
  }
})

watch(selectedGpuDeviceKeys, () => {
  syncSelectedGpuIntoParameters()
})

async function loadData() {
  loading.value = true
  try {
    const [providerRes, agentRes, runtimeRes, aiModelRes, templateRes, resourceRes] = await Promise.all([
      listAIProviders(),
      listAIAgents(),
      listAIRuntimeProfiles(),
      listAIModels(),
      getTemplateList({ template_type: 'ai' }),
      getResourceList()
    ])

    const nextState = normalizeAiStoreState({
      models: extractArray(aiModelRes?.data),
      providers: extractArray(providerRes?.data),
      agents: extractArray(agentRes?.data),
      runtimeProfiles: extractArray(runtimeRes?.data),
      deployments: []
    })

    aiState.models = nextState.models
    aiState.providers = nextState.providers
    aiState.agents = nextState.agents
    aiState.runtimeProfiles = nextState.runtimeProfiles
    aiState.deployments = nextState.deployments
    deployTemplates.value = extractArray(templateRes?.data)
    deployResources.value = extractArray(resourceRes?.data)
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || 'AI 商店数据加载失败')
  } finally {
    loading.value = false
  }
}

const storeKind = computed(() => 'ai')

function handleStoreTabChange(name) {
  if (name === 'app') {
    router.push('/store/apps')
  }
}

function toggleExpandedRow(row) {
  expandedRowId.value = isRowExpanded(row) ? null : row.id
}

function handleExpandChange(row) {
  toggleExpandedRow(row)
}

function isRowExpanded(row) {
  return String(expandedRowId.value) === String(row.id)
}

function mapResourceGpuStateLabel(status) {
  return {
    idle: '待选择资源',
    loading: '采集中',
    ready: '已就绪',
    error: '采集失败',
    unsupported: '不支持'
  }[status] || '未知状态'
}

function mapEstimateDisplayStatusLabel(status) {
  return {
    sufficient: '充足',
    warning: '预警',
    insufficient: '不足',
    'missing-data': '数据不足',
    collecting: '采集中',
    failed: '失败',
    idle: '待选择资源'
  }[status] || '未知状态'
}

function mapEstimateBreakdownLabel(label) {
  return {
    Weights: '权重占用',
    'KV Cache': 'KV Cache',
    'Runtime Reserve': '运行预留',
    'CPU Offload': 'CPU 卸载'
  }[label] || label
}

function ensureSelectedResourceGpuEntry(resource) {
  if (!resource?.id) return createGpuInfoCacheEntry()
  const cacheKey = String(resource.id)
  if (!resourceGpuInfoCache[cacheKey]) {
    resourceGpuInfoCache[cacheKey] = normalizeResourceGpuInfo(resource)
  }
  return resourceGpuInfoCache[cacheKey]
}

function clearResourceRefreshTimer(resourceId) {
  const cacheKey = String(resourceId)
  const timer = resourceRefreshTimers.get(cacheKey)
  if (timer) {
    clearTimeout(timer)
    resourceRefreshTimers.delete(cacheKey)
  }
}

function invalidateResourceGpuRefresh(resourceId) {
  if (!resourceId) return
  const cacheKey = String(resourceId)
  clearResourceRefreshTimer(cacheKey)
  if (resourceGpuInfoCache[cacheKey]) {
    resourceGpuInfoCache[cacheKey] = invalidateGpuInfoRefresh(resourceGpuInfoCache[cacheKey])
  }
}

function clearResourceRefreshTimers() {
  resourceRefreshTimers.forEach((timer) => {
    clearTimeout(timer)
  })
  resourceRefreshTimers.clear()
  Object.keys(resourceGpuInfoCache).forEach((cacheKey) => {
    resourceGpuInfoCache[cacheKey] = invalidateGpuInfoRefresh(resourceGpuInfoCache[cacheKey])
  })
}

function resetDeployDialogRuntimeState() {
  selectedGpuDeviceKeys.value = []
  clearResourceRefreshTimers()
}

function ensureResourceGpuInfo(resourceId) {
  const resource = deployResources.value.find((item) => String(item.id) === String(resourceId)) || null
  if (!resource?.id) return createGpuInfoCacheEntry()

  const cacheKey = String(resource.id)
  const currentEntry = resourceGpuInfoCache[cacheKey] || normalizeResourceGpuInfo(resource)
  const normalizedEntry = normalizeResourceGpuInfo(resource)

  if (normalizedEntry.status === 'ready') {
    resourceGpuInfoCache[cacheKey] = normalizedEntry
    clearResourceRefreshTimer(cacheKey)
    return resourceGpuInfoCache[cacheKey]
  }

  resourceGpuInfoCache[cacheKey] = currentEntry
  if (shouldRefreshResourceGpuInfo(currentEntry)) {
    startResourceGpuRefresh(resource.id)
  }
  return resourceGpuInfoCache[cacheKey]
}

async function startResourceGpuRefresh(resourceId) {
  const cacheKey = String(resourceId)
  const currentEntry = resourceGpuInfoCache[cacheKey] || createGpuInfoCacheEntry()
  const refreshToken = createGpuRefreshToken()
  const nextRefresh = beginGpuInfoRefresh(currentEntry, refreshToken, Date.now())

  resourceGpuInfoCache[cacheKey] = nextRefresh.entry
  if (!nextRefresh.started) return nextRefresh.entry

  clearResourceRefreshTimer(cacheKey)

  try {
    await refreshResourceBaseInfo(resourceId)
  } catch (error) {
    resourceGpuInfoCache[cacheKey] = markGpuInfoTimeout(resourceGpuInfoCache[cacheKey], refreshToken, error?.response?.data?.message || error?.message || 'GPU 信息刷新失败')
    return resourceGpuInfoCache[cacheKey]
  }

  const poll = async () => {
    const activeEntry = resourceGpuInfoCache[cacheKey]
    if (!activeEntry || activeEntry.refreshToken !== refreshToken) {
      clearResourceRefreshTimer(cacheKey)
      return
    }

    if (Date.now() - activeEntry.requestedAt >= RESOURCE_GPU_REFRESH_TIMEOUT_MS) {
      resourceGpuInfoCache[cacheKey] = markGpuInfoTimeout(activeEntry, refreshToken, 'GPU 信息采集超时')
      clearResourceRefreshTimer(cacheKey)
      return
    }

    try {
      const resourceRes = await getResourceList()
      deployResources.value = extractArray(resourceRes?.data)
      const listedResource = deployResources.value.find((item) => String(item.id) === cacheKey)
      const listedEntry = normalizeResourceGpuInfo(listedResource || {})

      if (listedResource && listedEntry.status !== 'loading' && listedEntry.status !== 'idle') {
        resourceGpuInfoCache[cacheKey] = resolveGpuInfoTerminalState(resourceGpuInfoCache, cacheKey, refreshToken, listedResource)[cacheKey]
        clearResourceRefreshTimer(cacheKey)
        return
      }

      const detailRes = await getResourceDetail(resourceId)
      const detailResource = detailRes?.data || {}
      const detailEntry = normalizeResourceGpuInfo(detailResource)
      if (detailResource?.id && detailEntry.status !== 'loading' && detailEntry.status !== 'idle') {
        resourceGpuInfoCache[cacheKey] = resolveGpuInfoTerminalState(resourceGpuInfoCache, cacheKey, refreshToken, detailResource)[cacheKey]
        clearResourceRefreshTimer(cacheKey)
        return
      }
    } catch (error) {
      if (Date.now() - activeEntry.requestedAt >= RESOURCE_GPU_REFRESH_TIMEOUT_MS) {
        resourceGpuInfoCache[cacheKey] = markGpuInfoTimeout(activeEntry, refreshToken, error?.response?.data?.message || error?.message || 'GPU 信息采集超时')
        clearResourceRefreshTimer(cacheKey)
        return
      }
    }

    clearResourceRefreshTimer(cacheKey)
    const timer = setTimeout(() => {
      poll()
    }, RESOURCE_GPU_REFRESH_POLL_MS)
    resourceRefreshTimers.set(cacheKey, timer)
  }

  const timer = setTimeout(() => {
    poll()
  }, RESOURCE_GPU_REFRESH_POLL_MS)
  resourceRefreshTimers.set(cacheKey, timer)
  return resourceGpuInfoCache[cacheKey]
}

function syncSelectedGpuDevices() {
  if (!canSelectGpuDevices.value) {
    if (selectedGpuDeviceKeys.value.length > 0) {
      selectedGpuDeviceKeys.value = []
    }
    return
  }

  const availableKeys = new Set(selectedResourceGpuDevices.value.map((device) => String(device.deviceKey)))
  const retainedKeys = selectedGpuDeviceKeys.value.filter((key) => availableKeys.has(String(key)))
  selectedGpuDeviceKeys.value = retainedKeys.length > 0 ? retainedKeys : selectedResourceGpuDevices.value.map((device) => device.deviceKey)
}

function syncSelectedGpuIntoParameters() {
  const gpuIndexValue = selectedGpuDevices.value.map((device) => device.index).join(',')
  const gpuUuidValue = selectedGpuDevices.value.map((device) => device.uuid).filter(Boolean).join(',')
  const gpuCountValue = selectedGpuDevices.value.length > 0 ? String(selectedGpuDevices.value.length) : ''
  const bindings = {
    cuda_visible_devices: gpuIndexValue,
    nvidia_visible_devices: gpuIndexValue,
    gpu_indices: gpuIndexValue,
    gpu_ids: gpuIndexValue,
    device_ids: gpuIndexValue,
    gpu_devices: gpuIndexValue,
    gpu_uuids: gpuUuidValue,
    gpu_count: gpuCountValue
  }

  Object.entries(bindings).forEach(([key, value]) => {
    if (value) {
      deployForm.parameters[key] = value
    } else if (deployForm.parameters[key] !== undefined) {
      delete deployForm.parameters[key]
    }
  })
}

function retryResourceGpuRefresh() {
  const resource = selectedDeployResource.value
  if (!resource?.id) return
  invalidateResourceGpuRefresh(resource.id)
  startResourceGpuRefresh(resource.id)
}

async function openDeployDialog(row = null) {
  Object.assign(deployForm, createDeployForm())
  deployTemplateVersions.value = []
  resetDeployDialogRuntimeState()
  if (row) {
    deployForm.modelId = row.id
    handleDeployModelChange()
  }
  dialogs.deploy = true
}

function openImportModelDialog() {
  Object.assign(importModelForm, createImportModelForm())
  dialogs.importModel = true
}

function openProviderDialog() {
  Object.assign(providerForm, createProviderForm())
  dialogs.provider = true
}

function handleDeployModelChange() {
  deployForm.parameters = buildDeployParameterState({
    fields: deployForm.parameterFields,
    selectedModel: selectedDeployModel.value || {}
  })
}

async function handleDeployTemplateChange(templateId) {
  deployForm.templateVersionId = null
  deployForm.targetResourceId = null
  deployTemplateVersions.value = []
  deployForm.parameterFields = []
  deployForm.parameters = buildDeployParameterState({ selectedModel: selectedDeployModel.value || {} })

  if (!templateId) return

  const response = await getTemplateVersions(templateId)
  deployTemplateVersions.value = extractArray(response?.data)
  if (deployTemplateVersions.value.length > 0) {
    deployForm.templateVersionId = deployTemplateVersions.value[0].id
    handleDeployVersionChange(deployForm.templateVersionId)
  }
}

function handleDeployVersionChange(versionId) {
  const currentVersion = deployTemplateVersions.value.find((item) => String(item.id) === String(versionId)) || null
  deployForm.parameterFields = Array.isArray(currentVersion?.parameters) ? currentVersion.parameters : []
  deployForm.parameters = buildDeployParameterState({
    fields: deployForm.parameterFields,
    selectedModel: selectedDeployModel.value || {}
  })
}

async function submitImportModel() {
  const payload = buildAIModelImportPayload(importModelForm)
  if (!payload.source) {
    ElMessage.warning('请选择模型来源')
    return
  }
  if (!payload.source_model_id) {
    ElMessage.warning('请填写模型 ID')
    return
  }

  importSubmitting.value = true
  try {
    await importLocalAIModel(payload)
    dialogs.importModel = false
    ElMessage.success('模型元数据已导入')
    await loadData()
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '导入模型失败')
  } finally {
    importSubmitting.value = false
  }
}

async function submitDeploy() {
  if (!deployForm.modelId) {
    ElMessage.warning('请选择模型')
    return
  }
  if (!deployForm.templateId) {
    ElMessage.warning('请选择部署模板')
    return
  }
  if (!deployForm.templateVersionId || !deployForm.targetResourceId) {
    ElMessage.warning('请选择模板版本和目标资源')
    return
  }

  const missingField = (deployForm.parameterFields || []).find((field) => {
    if (!field.required) return false
    const value = deployForm.parameters?.[field.name]
    return value === undefined || value === null || String(value).trim() === ''
  })
  if (missingField) {
    ElMessage.warning(`请填写${missingField.label || missingField.name}`)
    return
  }

  const modelRow = selectedDeployModel.value
  if (!modelRow) {
    ElMessage.warning('当前模型不存在')
    return
  }

  deploySubmitting.value = true
  try {
    await createDeploymentRequest(buildAIDeploymentRequestPayload({
      modelId: deployForm.modelId,
      templateVersionId: deployForm.templateVersionId,
      targetResourceId: deployForm.targetResourceId,
      parameters: deployForm.parameters
    }))

    const resource = deployResources.value.find((item) => String(item.id) === String(deployForm.targetResourceId)) || {}
    const version = deployTemplateVersions.value.find((item) => String(item.id) === String(deployForm.templateVersionId)) || {}
    const result = buildDemoDeploymentRecord({
      deploymentName: `${modelRow.name}-deployment`,
      resourceName: resource.name || `资源#${deployForm.targetResourceId}`,
      templateName: selectedDeployTemplate.value?.name || '',
      versionLabel: version.version || '',
      providerName: `${modelRow.name} Provider`,
      endpoint: resource.endpoint || '',
      status: 'running',
      providerStatus: 'active'
    }, modelRow)

    localDemoState.deployments.push(result.deployment)
    localDemoState.providers.push({
      ...result.provider,
      model_bindings: [result.binding]
    })

    dialogs.deploy = false
    expandedRowId.value = modelRow.id
    ElMessage.success('部署请求已创建，并已在当前页追加 Deployment / Provider / Binding 展示')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '创建部署请求失败')
  } finally {
    deploySubmitting.value = false
  }
}

async function submitProviderDemo() {
  if (!providerForm.providerName.trim()) {
    ElMessage.warning('请填写 Provider 名称')
    return
  }
  if (!providerForm.endpoint.trim()) {
    ElMessage.warning('请填写 Endpoint')
    return
  }
  if (!providerForm.credentialId) {
    ElMessage.warning('请选择 Credential')
    return
  }
  if (providerForm.bindModelNow && !providerForm.modelId) {
    ElMessage.warning('请先选择模型')
    return
  }
  if (providerForm.bindModelNow && !providerForm.providerModelKey.trim()) {
    ElMessage.warning('请填写 Provider Model Key')
    return
  }

  const invalidJsonFields = getInvalidJsonFieldLabels([
    { label: 'Headers JSON', value: providerForm.headersJSON },
    { label: 'Settings JSON', value: providerForm.settingsJSON },
    ...(providerForm.bindModelNow
      ? [
          { label: 'Capabilities JSON', value: providerForm.capabilitiesJSON },
          { label: 'Binding Settings JSON', value: providerForm.bindingSettingsJSON }
        ]
      : [])
  ])
  if (invalidJsonFields.length > 0) {
    ElMessage.warning(`JSON 格式无效：${invalidJsonFields.join('、')}`)
    return
  }

  const baseModel = selectedProviderModel.value || {}
  const selectedModel = providerForm.bindModelNow
    ? {
        ...baseModel,
        id: providerForm.modelId,
        providerModelKey: providerForm.providerModelKey,
        provider_model_key: providerForm.providerModelKey,
        binding_key: providerForm.providerModelKey
      }
    : {}

  try {
    const providerResp = await createAIProvider({
      name: providerForm.providerName.trim(),
      base_url: providerForm.endpoint.trim(),
      credential_id: providerForm.credentialId,
      headers_json: providerForm.headersJSON.trim(),
      settings_json: providerForm.settingsJSON.trim(),
      status: providerForm.status
    })

    const createdProvider = providerResp?.data
    if (providerForm.bindModelNow && createdProvider?.id) {
      await createAIModelBinding(createdProvider.id, {
        model_id: providerForm.modelId,
        provider_model_key: providerForm.providerModelKey.trim(),
        capabilities_json: providerForm.capabilitiesJSON.trim(),
        settings_json: providerForm.bindingSettingsJSON.trim(),
        status: providerForm.status
      })
    }

    dialogs.provider = false
    await loadData()
    if (providerForm.bindModelNow && providerForm.modelId) {
      expandedRowId.value = providerForm.modelId
    }
    ;(providerResp?.warnings || []).forEach((warning) => ElMessage.warning(warning))
    ElMessage.success(providerForm.bindModelNow ? 'Provider 与 Binding 已保存' : 'Provider 已保存，可稍后再补 Binding')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '保存 Provider 失败')
  }
}

function extractArray(payload) {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.models)) return payload.models
  return []
}

function createDeployForm() {
  return {
    modelId: null,
    templateId: null,
    templateVersionId: null,
    targetResourceId: null,
    parameterFields: [],
    parameters: {}
  }
}

function createImportModelForm() {
  return {
    source: 'huggingface',
    sourceModelId: ''
  }
}

function createProviderForm() {
  return {
    providerName: '',
    endpoint: '',
    credentialId: null,
    status: 'active',
    bindModelNow: false,
    modelId: null,
    providerModelKey: '',
    capabilitiesJSON: '',
    bindingSettingsJSON: '',
    headersJSON: '',
    settingsJSON: ''
  }
}
</script>

<style scoped>
.ai-store-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.card-shell {
  border-radius: 24px;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-md);
}

.section-shell {
  padding: 22px 22px 18px;
}

.store-tabs-bar,
.section-header,
.detail-block-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.table-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.section-header p,
.detail-count {
  color: var(--text-secondary);
}

.section-header h2,
.detail-block-header strong {
  margin: 0;
  color: var(--text-primary);
}

.expand-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 8px 12px 12px;
}

.detail-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.detail-count {
  font-size: 12px;
}

.deploy-vram-estimate-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 12px;
  padding: 12px;
  border: 1px solid var(--border-color-light);
  border-radius: 14px;
  background: var(--bg-card);
}

.deploy-vram-estimate-header,
.deploy-vram-estimate-status,
.deploy-vram-estimate-metrics,
.deploy-vram-estimate-inline-list {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.deploy-vram-estimate-header {
  justify-content: space-between;
  align-items: flex-start;
}

.deploy-vram-estimate-header h3,
.deploy-vram-estimate-header p,
.deploy-vram-estimate-subtitle {
  margin: 0;
}

.deploy-vram-estimate-header h3,
.deploy-vram-estimate-subtitle {
  font-size: 13px;
}

.deploy-vram-estimate-header p,
.deploy-vram-estimate-resource-state,
.estimate-label,
.deploy-vram-estimate-inline-item span,
.deploy-vram-estimate-inline-item em {
  color: var(--text-secondary);
}

.deploy-vram-estimate-metric,
.deploy-vram-estimate-inline-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 108px;
}

.deploy-vram-estimate-inline-item em {
  font-style: normal;
  font-size: 12px;
}

.deploy-vram-estimate-composition,
.deploy-vram-estimate-selection {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

@media (max-width: 960px) {
  .section-header,
  .detail-block-header {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
