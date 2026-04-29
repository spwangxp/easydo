<template>
  <div class="app-store-page">
    <PageHeader>
      <template #title>
        <StoreKindSwitch :model-value="storeKind" @update:model-value="handleStoreTabChange" />
      </template>
      <template #subtitle>
        {{ selectedApp ? '当前正在查看应用详情，可直接管理版本、参数和部署入口。' : '按功能分类浏览应用，外层只保留关键信息，进入详情后再管理版本与部署。' }}
      </template>
      <template #actions>
        <PageHeaderActions>
          <template v-if="selectedApp">
            <el-button @click="resetSelection">返回目录</el-button>
            <el-button @click="loadVariants(selectedApp.id)">刷新版本</el-button>
          </template>
          <template v-else>
            <el-button @click="loadInitialData">刷新</el-button>
            <el-button type="primary" @click="openAppDialog()">新增应用</el-button>
          </template>
        </PageHeaderActions>
      </template>
    </PageHeader>

    <template v-if="!selectedApp">
      <section class="catalog-shell">
        <aside class="category-rail card-shell">
          <button
            v-for="category in categoryOptions"
            :key="category.value"
            class="category-chip"
            :class="{ active: filters.category === category.value }"
            @click="filters.category = category.value"
          >
            <span>{{ category.label }}</span>
            <strong>{{ category.count }}</strong>
          </button>
        </aside>

        <div class="catalog-content card-shell">
          <div class="catalog-toolbar">
            <el-input v-model="filters.keyword" clearable placeholder="搜索应用名称或摘要" class="toolbar-search" />
          </div>

          <div v-loading="loading.apps" class="catalog-grid">
            <button
              v-for="app in filteredApps"
              :key="app.id"
              class="app-card"
              @click="selectApp(app)"
            >
              <div class="app-card-top">
                <div>
                  <h3>{{ app.name }}</h3>
                  <p>{{ app.summary || app.description || '暂无摘要' }}</p>
                </div>
                <span class="source-badge" :class="`source-${app.source}`">{{ app.source === 'platform' ? '官方' : '工作空间' }}</span>
              </div>
              <div class="app-card-meta">
                <span>{{ categoryLabel(app.category) }}</span>
                <span>{{ formatInfraList(app.supported_infra || app.supported_infras || app.supported_resource_types) }}</span>
              </div>
            </button>
            <el-empty v-if="!loading.apps && filteredApps.length === 0" description="当前分类下暂无应用" />
          </div>
        </div>
      </section>
    </template>

    <template v-else>
      <section class="detail-shell card-shell">
        <div class="detail-header">
          <div>
            <p class="detail-path">{{ categoryLabel(selectedApp.category) }} / {{ selectedApp.source === 'platform' ? '官方' : '工作空间' }}</p>
            <h2>{{ selectedApp.name }}</h2>
            <p>{{ selectedApp.description || selectedApp.summary || '暂无说明' }}</p>
          </div>
          <div class="detail-stats">
            <div>
              <span>支持 Infra</span>
              <strong>{{ formatInfraList(selectedApp.supported_infra || selectedApp.supported_infras || selectedApp.supported_resource_types) }}</strong>
            </div>
            <div>
              <span>版本数</span>
              <strong>{{ variants.length }}</strong>
            </div>
            <div class="detail-header-actions">
              <el-button size="small" @click="openAppDialog(selectedApp)">编辑应用</el-button>
              <el-button size="small" type="primary" @click="openVariantDialog()">新增版本</el-button>
              <el-button size="small" type="danger" plain @click="removeApp(selectedApp)">删除应用</el-button>
            </div>
          </div>
        </div>

        <div class="variant-list">
          <div class="variant-list-head">
            <span>版本号</span>
            <span>状态</span>
            <span>版本说明</span>
            <span>更新时间</span>
            <span>Infra</span>
            <span class="actions-cell">动作</span>
          </div>

          <div v-for="variant in variants" :key="variant.id" class="variant-row">
            <div class="variant-version">
              <strong>{{ variant.version }}</strong>
            </div>
            <div>
              <el-tag size="small" :type="variant.status === 'published' ? 'success' : 'info'">{{ variant.status || 'draft' }}</el-tag>
            </div>
            <div class="variant-description">{{ variant.version_description || '暂无版本说明' }}</div>
            <div>{{ formatDateTime(variant.updated_at) }}</div>
            <div><el-tag size="small" effect="plain">{{ variant.infra_type === 'k8s' ? 'K8s' : 'VM' }}</el-tag></div>
            <div class="variant-actions">
              <el-button size="small" @click="openDeployDialog(variant)">部署</el-button>
              <el-button size="small" @click="openVariantDialog(variant)">编辑</el-button>
              <el-button size="small" type="danger" plain @click="removeVariant(variant)">删除版本</el-button>
            </div>
          </div>

          <el-empty v-if="!loading.variants && variants.length === 0" description="当前应用还没有版本" />
        </div>
      </section>
    </template>

    <el-dialog v-model="dialogs.app" :title="appForm.id ? '编辑应用' : '新增应用'" width="620px" destroy-on-close>
        <el-form label-position="top">
          <el-form-item label="应用名称" required>
            <el-input v-model="appForm.name" />
          </el-form-item>
          <el-form-item label="功能分类" required>
            <el-select v-model="appForm.category" style="width: 100%">
              <el-option v-for="item in baseCategories" :key="item.value" :label="item.label" :value="item.value" />
            </el-select>
          </el-form-item>
        <el-form-item label="一句话摘要">
          <el-input v-model="appForm.summary" maxlength="120" show-word-limit />
        </el-form-item>
        <el-form-item label="应用说明">
          <el-input v-model="appForm.description" type="textarea" :rows="4" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.app = false">取消</el-button>
        <el-button type="primary" :loading="loading.submitApp" @click="submitApp">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialogs.variant" :title="variantForm.id ? '编辑版本' : '新增版本'" width="1120px" destroy-on-close>
      <div class="dialog-grid split">
        <section class="panel-card">
          <div class="panel-heading">
            <h3>版本信息</h3>
            <p>版本行只展示关键信息，复杂配置收敛到这里。</p>
          </div>
          <el-form label-position="top">
            <div class="dialog-grid two">
              <el-form-item label="版本号" required>
                <el-input v-model="variantForm.version" placeholder="如 7.2.0" />
              </el-form-item>
              <el-form-item label="状态">
                <el-select v-model="variantForm.status" style="width: 100%">
                  <el-option label="草稿" value="draft" />
                  <el-option label="已发布" value="published" />
                  <el-option label="已下线" value="unpublished" />
                </el-select>
              </el-form-item>
            </div>
            <div class="dialog-grid two">
              <el-form-item label="Infra" required>
                <el-segmented v-model="variantForm.infra_type" :options="infraOptions" block />
              </el-form-item>
            </div>
            <el-form-item label="版本说明">
              <el-input v-model="variantForm.version_description" type="textarea" :rows="3" />
            </el-form-item>
          </el-form>

          <template v-if="variantForm.infra_type === 'vm'">
            <div class="panel-heading compact">
              <h3>VM 命令模板</h3>
              <p>命令顺序完全由模板控制，参数只负责填空。</p>
            </div>
            <el-input v-model="variantForm.command_template" type="textarea" :rows="10" class="mono-input" placeholder="docker run -d --name {{container_name}} -p {{vm_port}}:{{redis_port}} redis:{{version}}" />
          </template>

          <template v-else>
            <div class="panel-heading compact">
              <h3>Chart Source</h3>
              <p>对象存储地址由服务端生成，这里只选择来源和原始信息。</p>
            </div>
            <div class="dialog-grid two">
              <el-form-item label="Source Type">
                <el-segmented v-model="variantForm.chart_source_type" :options="chartSourceOptions" block />
              </el-form-item>
              <el-form-item label="Chart Version">
                <el-input v-model="variantForm.chart_version" placeholder="如 19.6.0" />
              </el-form-item>
            </div>
            <div class="dialog-grid two" v-if="variantForm.chart_source_type === 'repo'">
              <el-form-item label="Repo URL">
                <el-input v-model="variantForm.chart_repo_url" placeholder="https://charts.bitnami.com/bitnami" />
              </el-form-item>
              <el-form-item label="Chart Name">
                <el-input v-model="variantForm.chart_name" placeholder="redis" />
              </el-form-item>
            </div>
            <div class="dialog-grid two" v-else-if="variantForm.chart_source_type === 'oci'">
              <el-form-item label="OCI URL">
                <el-input v-model="variantForm.chart_oci_url" placeholder="oci://registry-1.docker.io/bitnamicharts/redis" />
              </el-form-item>
              <el-form-item label="Chart Name">
                <el-input v-model="variantForm.chart_name" placeholder="redis" />
              </el-form-item>
            </div>
            <div v-else class="upload-shell">
              <el-upload
                :auto-upload="false"
                :show-file-list="false"
                :on-change="handleChartFileChange"
                accept=".tgz,.tar.gz,.zip"
              >
                <el-button size="small">选择 Chart 文件</el-button>
              </el-upload>
              <span class="upload-hint">
                {{ selectedChartFile ? selectedChartFile.name : (variantForm.chart_file_name || '未选择文件') }}
              </span>
              <div v-if="chartResolveState.message" class="upload-status" :class="chartResolveState.status">
                {{ chartResolveState.message }}
              </div>
            </div>
            <div v-if="variantForm.chart_source_type !== 'upload'" class="chart-resolve-toolbar">
              <el-button
                               type="primary"
                plain
                :loading="chartResolveState.status === 'resolving'"
                :disabled="!canResolveRemoteChart()"
                @click="resolveRemoteChartSource(true)"
              >
                解析 Chart
              </el-button>
              <span v-if="chartResolveState.message" class="upload-status" :class="chartResolveState.status">
                {{ chartResolveState.message }}
              </span>
            </div>
            <el-form-item label="Base values.yaml">
              <el-input v-model="variantForm.base_values_yaml" type="textarea" :rows="10" class="mono-input" placeholder="architecture: standalone&#10;auth:&#10;  enabled: true" />
            </el-form-item>
          </template>
        </section>

        <section class="panel-card">
          <div class="panel-heading">
            <h3>参数定义</h3>
            <p>名称同时承担映射职责。VM 用于模板占位，K8s 推荐直接使用 values 路径。</p>
          </div>
          <div class="parameter-toolbar">
            <el-button size="small" @click="addParameterRow">新增参数</el-button>
          </div>
          <el-table :data="variantForm.parameters" size="small" border class="parameter-table">
            <el-table-column label="名称" min-width="170">
              <template #default="{ row }"><el-input v-model="row.name" size="small" placeholder="如 auth.password / vm_port" /></template>
            </el-table-column>
            <el-table-column label="标题" min-width="140">
              <template #default="{ row }"><el-input v-model="row.label" size="small" /></template>
            </el-table-column>
            <el-table-column label="类型" width="110">
              <template #default="{ row }">
                <el-select v-model="row.type" size="small">
                  <el-option label="文本" value="text" />
                  <el-option label="数字" value="number" />
                  <el-option label="下拉" value="select" />
                  <el-option label="开关" value="switch" />
                  <el-option label="多行" value="textarea" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column label="默认值" min-width="140">
              <template #default="{ row }"><el-input v-model="row.default_value" size="small" /></template>
            </el-table-column>
            <el-table-column label="选项" min-width="160">
              <template #default="{ row }"><el-input v-model="row.option_values_text" size="small" placeholder="a,b,c" :disabled="row.type !== 'select'" /></template>
            </el-table-column>
            <el-table-column label="描述" min-width="180">
              <template #default="{ row }"><el-input v-model="row.description" size="small" /></template>
            </el-table-column>
            <el-table-column label="额外提示" min-width="180">
              <template #default="{ row }"><el-input v-model="row.extra_tip" size="small" placeholder="如：显存紧张时先下调" /></template>
            </el-table-column>
            <el-table-column label="必填" width="70">
              <template #default="{ row }"><el-switch v-model="row.required" size="small" /></template>
            </el-table-column>
            <el-table-column label="高级" width="70">
              <template #default="{ row }"><el-switch v-model="row.advanced" size="small" /></template>
            </el-table-column>
            <el-table-column label="" width="80">
              <template #default="{ $index }">
                <el-button size="small" type="danger" text @click="removeParameterRow($index)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </section>
      </div>
      <template #footer>
        <el-button @click="dialogs.variant = false">取消</el-button>
        <el-button type="primary" :loading="loading.submitVariant" @click="submitVariant">保存版本</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialogs.deploy" :title="deployState.variant?.version ? `部署 ${deployState.variant.version}` : '部署应用'" width="1180px" destroy-on-close>
      <div class="dialog-grid split">
        <section class="panel-card">
          <div class="panel-heading">
            <h3>部署参数</h3>
            <p>左侧填写参数和目标资源，右侧实时预览最终执行结果。</p>
          </div>
          <el-form label-position="top">
            <el-form-item label="目标资源" required>
              <el-select v-model="deployState.target_resource_id" filterable style="width: 100%">
                <el-option
                  v-for="item in deployResources"
                  :key="item.id"
                  :label="`${item.name} · ${item.endpoint || item.type}`"
                  :value="item.id"
                />
              </el-select>
            </el-form-item>

            <el-form-item v-if="deployState.variant?.infra_type === 'k8s'" label="命名空间" required>
              <el-input v-model="deployState.parameters.namespace" placeholder="default" />
            </el-form-item>

            <div class="parameter-stack" v-if="deployBasicParameters.length > 0">
              <div v-for="parameter in deployBasicParameters" :key="parameter.name" class="parameter-field">
                <label class="parameter-label">
                  <span>{{ parameter.label || parameter.name }}</span>
                  <span v-if="parameter.description" class="parameter-recommendation">{{ parameter.description }}</span>
                  <el-tooltip v-if="parameter.extra_tip" :content="parameter.extra_tip" placement="top" effect="dark">
                    <el-icon class="parameter-help-icon"><QuestionFilled /></el-icon>
                  </el-tooltip>
                </label>
                <el-input-number
                  v-if="parameter.type === 'number'"
                  v-model="deployState.parameters[parameter.name]"
                  controls-position="right"
                  style="width: 100%"
                />
                <el-switch
                  v-else-if="parameter.type === 'switch'"
                  v-model="deployState.parameters[parameter.name]"
                />
                <el-select
                  v-else-if="parameter.type === 'select'"
                  v-model="deployState.parameters[parameter.name]"
                  clearable
                  style="width: 100%"
                >
                  <el-option
                    v-for="option in parameter.option_values || []"
                    :key="`${parameter.name}-${option}`"
                    :label="option"
                    :value="option"
                  />
                </el-select>
                <el-input
                  v-else
                  v-model="deployState.parameters[parameter.name]"
                  :type="parameter.type === 'textarea' ? 'textarea' : 'text'"
                  :rows="parameter.type === 'textarea' ? 3 : undefined"
                />
              </div>
            </div>

            <el-collapse v-if="deployAdvancedParameters.length > 0" class="advanced-collapse">
              <el-collapse-item title="更多参数" name="advanced">
                <div class="parameter-stack">
                  <div v-for="parameter in deployAdvancedParameters" :key="parameter.name" class="parameter-field">
                    <label class="parameter-label">
                      <span>{{ parameter.label || parameter.name }}</span>
                      <span v-if="parameter.description" class="parameter-recommendation">{{ parameter.description }}</span>
                      <el-tooltip v-if="parameter.extra_tip" :content="parameter.extra_tip" placement="top" effect="dark">
                        <el-icon class="parameter-help-icon"><QuestionFilled /></el-icon>
                      </el-tooltip>
                    </label>
                    <el-input-number
                      v-if="parameter.type === 'number'"
                      v-model="deployState.parameters[parameter.name]"
                      controls-position="right"
                      style="width: 100%"
                    />
                    <el-switch
                      v-else-if="parameter.type === 'switch'"
                      v-model="deployState.parameters[parameter.name]"
                    />
                    <el-select
                      v-else-if="parameter.type === 'select'"
                      v-model="deployState.parameters[parameter.name]"
                      clearable
                      style="width: 100%"
                    >
                      <el-option
                        v-for="option in parameter.option_values || []"
                        :key="`${parameter.name}-${option}`"
                        :label="option"
                        :value="option"
                      />
                    </el-select>
                    <el-input
                      v-else
                      v-model="deployState.parameters[parameter.name]"
                      :type="parameter.type === 'textarea' ? 'textarea' : 'text'"
                      :rows="parameter.type === 'textarea' ? 3 : undefined"
                    />
                  </div>
                </div>
              </el-collapse-item>
            </el-collapse>
          </el-form>
        </section>

        <section class="panel-card preview-panel">
          <div class="panel-heading">
            <h3>执行预览</h3>
            <p>{{ deployState.variant?.infra_type === 'k8s' ? '显示 Base + Diff 和最终 Helm 命令。' : '显示最终渲染后的命令。' }}</p>
          </div>

          <template v-if="deployPreview">
            <div v-if="deployState.variant?.infra_type === 'vm'" class="preview-block">
              <span class="preview-label">最终命令</span>
              <pre>{{ deployPreview.rendered_command }}</pre>
            </div>

            <template v-else>
              <div class="preview-block">
                <span class="preview-label">Base + Diff</span>
                <div class="diff-list">
                  <div v-for="(line, index) in deployPreview.diff_lines || []" :key="`${line.text}-${index}`" class="diff-line" :class="line.type">
                    <span>{{ line.type === 'add' ? '+' : ' ' }}</span>
                    <code>{{ formatDiffText(line) }}</code>
                  </div>
                </div>
              </div>
              <div class="preview-block">
                <span class="preview-label">Helm 命令</span>
                <pre>{{ deployPreview.helm_command }}</pre>
              </div>
            </template>
          </template>
          <el-empty v-else description="选择资源并填写参数后展示预览" />
        </section>
      </div>
      <template #footer>
        <el-button @click="dialogs.deploy = false">取消</el-button>
        <el-button type="primary" :loading="loading.submitDeploy" @click="submitDeploy">发起部署</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'

import { createDeploymentRequest } from '@/api/deployment'
import { getResourceList } from '@/api/resource'
import {
  createTemplate,
  createTemplateVersion,
  deleteTemplate,
  deleteTemplateVersion,
  getTemplateList,
  getTemplateVersions,
  previewTemplateVersion,
  resolveTemplateChartSource,
  updateTemplate,
  updateTemplateVersion
} from '@/api/store'
import PageHeaderActions from './PageHeaderActions.vue'
import StoreKindSwitch from './StoreKindSwitch.vue'
import PageHeader from './PageHeader.vue'
import {
  createParameterRow,
  normalizeChartSourcePayload,
  normalizeParameterRows,
  resolveUploadChart,
  splitParametersByAdvanced
} from '../appStoreHelpers'
import { applyNamespacePreset } from '@/views/resources/k8s/utils'

const route = useRoute()
const router = useRouter()

const baseCategories = [
  { label: '全部', value: 'all' },
  { label: 'Web 服务', value: 'web-service' },
  { label: '数据库', value: 'database' },
  { label: '缓存', value: 'cache' },
  { label: '存储', value: 'storage' },
  { label: '消息队列', value: 'message-queue' },
  { label: 'AI 服务', value: 'ai-service' },
  { label: '开发工具', value: 'developer-tool' }
]

const infraOptions = [
  { label: 'VM', value: 'vm' },
  { label: 'K8s', value: 'k8s' }
]

const chartSourceOptions = [
  { label: 'Repo', value: 'repo' },
  { label: 'OCI', value: 'oci' },
  { label: 'Upload', value: 'upload' }
]

const loading = reactive({
  apps: false,
  variants: false,
  resources: false,
  submitApp: false,
  submitVariant: false,
  submitDeploy: false
})

const dialogs = reactive({
  app: false,
  variant: false,
  deploy: false
})

const filters = reactive({
  category: 'all',
  keyword: ''
})

const apps = ref([])
const variants = ref([])
const selectedApp = ref(null)
const selectedChartFile = ref(null)
const resolvedChartFile = ref(null)
const chartResolveState = reactive({
  status: 'idle',
  message: '',
  resolvedFrom: '',
  resolvedAt: 0
})
const deployResources = ref([])
const deployPreview = ref(null)

const appForm = reactive({
  id: null,
  name: '',
  category: 'web-service',
  target_resource_type: 'vm',
  summary: '',
  description: ''
})

const variantForm = reactive(createEmptyVariantForm())
const deployState = reactive({
  variant: null,
  target_resource_id: null,
  parameters: {}
})

const categoryOptions = computed(() => {
  return baseCategories.map((item) => ({
    ...item,
    count: item.value === 'all'
      ? apps.value.length
      : apps.value.filter((app) => app.category === item.value).length
  }))
})

const filteredApps = computed(() => {
  const keyword = filters.keyword.trim().toLowerCase()
  return apps.value.filter((app) => {
    const matchCategory = filters.category === 'all' || app.category === filters.category
    const haystack = `${app.name} ${app.summary || ''} ${app.description || ''}`.toLowerCase()
    const matchKeyword = !keyword || haystack.includes(keyword)
    return matchCategory && matchKeyword
  })
})

const normalizedVariantParameters = computed(() => normalizeParameterRows(variantForm.parameters))
const deployParameterGroups = computed(() => splitParametersByAdvanced(normalizeParameterRows(deployState.variant?.parameters || [])))
const deployBasicParameters = computed(() => deployParameterGroups.value.basic)
const deployAdvancedParameters = computed(() => deployParameterGroups.value.advanced)
const storeKind = computed(() => 'app')

let previewTimer = null

onMounted(() => {
  loadInitialData()
})

watch(
  () => [deployState.target_resource_id, JSON.stringify(deployState.parameters), deployState.variant?.id],
  () => {
    if (!dialogs.deploy) return
    clearTimeout(previewTimer)
    previewTimer = window.setTimeout(() => {
      refreshDeployPreview()
    }, 260)
  }
)

watch(
  () => [
    dialogs.variant,
    variantForm.infra_type,
    variantForm.chart_source_type,
    variantForm.chart_repo_url,
    variantForm.chart_oci_url,
    variantForm.chart_name,
    variantForm.chart_version
  ],
  () => {
    if (!dialogs.variant || variantForm.infra_type !== 'k8s') return
    invalidateResolvedChart({ preserveValues: true })
    if (variantForm.chart_source_type === 'repo' || variantForm.chart_source_type === 'oci') {
      scheduleRemoteChartResolve()
    }
  }
)

function createEmptyVariantForm() {
  return {
    id: null,
    version: '',
    status: 'draft',
    infra_type: 'vm',
    pipeline_id: null,
    version_description: '',
    command_template: '',
    chart_source_type: 'repo',
    chart_repo_url: '',
    chart_oci_url: '',
    chart_name: '',
    chart_version: '',
    chart_file_name: '',
    chart_object_key: '',
    base_values_yaml: '',
    parameters: [createParameterViewRow()]
  }
}

function createParameterViewRow() {
  return {
    ...createParameterRow(),
    option_values_text: ''
  }
}

function handleStoreTabChange(tabName) {
  if (tabName === 'app') return
	router.push('/store/ai')
}

function categoryLabel(value) {
  return baseCategories.find((item) => item.value === value)?.label || value || '未分类'
}

function formatInfraList(value = []) {
  if (!value || value.length === 0) return '未声明'
  return value.map((item) => (item === 'k8s' ? 'K8s' : 'VM')).join(' / ')
}

function formatDateTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

async function loadInitialData() {
  loading.apps = true
  try {
    const templatesResp = await getTemplateList({ template_type: 'app' })
    apps.value = templatesResp.data || []
  } finally {
    loading.apps = false
  }
}

async function selectApp(app) {
  selectedApp.value = app
  await loadVariants(app.id)
}

async function loadVariants(templateId) {
  loading.variants = true
  try {
    const response = await getTemplateVersions(templateId)
    variants.value = response.data || []
  } finally {
    loading.variants = false
  }
}

function resetSelection() {
  selectedApp.value = null
  variants.value = []
}

function openAppDialog(app = null) {
  if (app) {
    Object.assign(appForm, {
      id: app.id,
      name: app.name,
      category: app.category || 'web-service',
      target_resource_type: app.target_resource_type || 'vm',
      summary: app.summary || '',
      description: app.description || ''
    })
  } else {
    Object.assign(appForm, {
      id: null,
      name: '',
      category: 'web-service',
      target_resource_type: 'vm',
      summary: '',
      description: ''
    })
  }
  dialogs.app = true
}

async function submitApp() {
  if (!appForm.name.trim()) {
    ElMessage.warning('应用名称不能为空')
    return
  }
  loading.submitApp = true
  const payload = {
    name: appForm.name.trim(),
    category: appForm.category,
    template_type: 'app',
    target_resource_type: appForm.target_resource_type,
    source: 'workspace',
    summary: appForm.summary.trim(),
    description: appForm.description.trim()
  }
  try {
    if (appForm.id) {
      await updateTemplate(appForm.id, payload)
      ElMessage.success('应用已更新')
    } else {
      await createTemplate(payload)
      ElMessage.success('应用已创建')
    }
    dialogs.app = false
    await loadInitialData()
    if (selectedApp.value?.id) {
      const next = apps.value.find((item) => item.id === selectedApp.value.id)
      if (next) selectedApp.value = next
    }
  } finally {
    loading.submitApp = false
  }
}

async function removeApp(app) {
  await ElMessageBox.confirm(`确认删除应用 ${app.name} 吗？`, '删除应用', { type: 'warning' })
  await deleteTemplate(app.id)
  ElMessage.success('应用已删除')
  if (selectedApp.value?.id === app.id) {
    resetSelection()
  }
  await loadInitialData()
}

function openVariantDialog(variant = null) {
  selectedChartFile.value = null
  resetChartResolveState()
  Object.assign(variantForm, createEmptyVariantForm())
  if (variant) {
    Object.assign(variantForm, {
      id: variant.id,
      version: variant.version,
      status: variant.status || 'draft',
      infra_type: variant.infra_type || 'vm',
      pipeline_id: variant.pipeline_id || null,
      version_description: variant.version_description || '',
      command_template: variant.command_template || '',
      chart_source_type: variant.chart_source?.type || 'repo',
      chart_repo_url: variant.chart_source?.repo_url || '',
      chart_oci_url: variant.chart_source?.oci_url || '',
      chart_name: variant.chart_source?.chart_name || '',
      chart_version: variant.chart_source?.chart_version || '',
      chart_file_name: variant.chart_source?.file_name || '',
      chart_object_key: variant.chart_source?.object_key || '',
      base_values_yaml: variant.base_values_yaml || '',
      parameters: normalizeParameterRows(variant.parameters || []).map(toParameterViewRow)
    })
  }
  if (!variantForm.parameters.length) {
    variantForm.parameters = [createParameterViewRow()]
  }
  dialogs.variant = true
}

function toParameterViewRow(parameter) {
  return {
    ...parameter,
    option_values_text: (parameter.option_values || []).join(',')
  }
}

function addParameterRow() {
  variantForm.parameters.push({
    ...createParameterViewRow(),
    sort_order: variantForm.parameters.length + 1
  })
}

function removeParameterRow(index) {
  variantForm.parameters.splice(index, 1)
  if (!variantForm.parameters.length) {
    addParameterRow()
  }
}

function handleChartFileChange(file) {
  selectedChartFile.value = file.raw
  variantForm.chart_file_name = file.name
  variantForm.chart_name = variantForm.chart_name || file.name.replace(/(\.tar\.gz|\.tgz|\.zip)$/i, '')
  invalidateResolvedChart({ preserveValues: false })
  void resolveSelectedUploadChart()
}

function resetChartResolveState() {
  chartResolveState.status = 'idle'
  chartResolveState.message = ''
  chartResolveState.resolvedFrom = ''
  chartResolveState.resolvedAt = 0
}

function invalidateResolvedChart({ preserveValues } = { preserveValues: false }) {
  resolvedChartFile.value = null
  resetChartResolveState()
  if (!preserveValues) {
    variantForm.base_values_yaml = ''
  }
}

async function resolveSelectedUploadChart() {
  if (variantForm.infra_type !== 'k8s' || variantForm.chart_source_type !== 'upload' || !selectedChartFile.value) {
    resetChartResolveState()
    return
  }
  chartResolveState.status = 'resolving'
  chartResolveState.message = '正在解析 Chart 文件...'
  try {
    const resolved = await resolveChartSource({
      sourceType: 'upload',
      file: selectedChartFile.value
    })
    applyResolvedChartResult(resolved, 'upload', '已从上传的 Chart 中提取 values.yaml')
  } catch (error) {
    chartResolveState.status = 'error'
    chartResolveState.message = error?.message || '解析 Chart 文件失败'
    ElMessage.error(chartResolveState.message)
  }
}

function applyResolvedChartResult(resolved, sourceType, successMessage) {
  resolvedChartFile.value = resolved.chartFile || null
  if (!variantForm.chart_name.trim() && resolved.chartName) {
    variantForm.chart_name = resolved.chartName
  }
  if (resolved.chartVersion && !variantForm.chart_version.trim()) {
    variantForm.chart_version = resolved.chartVersion
  }
  variantForm.chart_file_name = resolved.fileName || variantForm.chart_file_name
  variantForm.base_values_yaml = resolved.valuesYAML || ''
  chartResolveState.status = 'success'
  chartResolveState.message = successMessage
  chartResolveState.resolvedFrom = sourceType
  chartResolveState.resolvedAt = Date.now()
}

function canResolveRemoteChart() {
  if (variantForm.infra_type !== 'k8s') return false
  if (variantForm.chart_source_type === 'repo') {
    return Boolean(variantForm.chart_repo_url.trim() && variantForm.chart_name.trim() && variantForm.chart_version.trim())
  }
  if (variantForm.chart_source_type === 'oci') {
    return Boolean(variantForm.chart_oci_url.trim() && variantForm.chart_version.trim())
  }
  return false
}

let chartResolveTimer = null

function scheduleRemoteChartResolve() {
  if (!canResolveRemoteChart()) return
  clearTimeout(chartResolveTimer)
  chartResolveTimer = window.setTimeout(() => {
    void resolveRemoteChartSource(false)
  }, 350)
}

async function resolveRemoteChartSource(showError = true) {
  if (!canResolveRemoteChart() || !selectedApp.value?.id) return
  chartResolveState.status = 'resolving'
  chartResolveState.message = variantForm.chart_source_type === 'repo'
    ? '正在从 Helm Repo 解析 Chart...'
    : '正在从 OCI Registry 解析 Chart...'
  try {
    const response = await resolveTemplateChartSource(selectedApp.value.id, {
      chart_source: normalizeChartSourcePayload(variantForm)
    })
    const resolved = response.data || {}
    resolved.chartFile = resolved.chart_file_base64
      ? new File(
        [Uint8Array.from(atob(resolved.chart_file_base64), (char) => char.charCodeAt(0))],
        resolved.chart_file_name || `${variantForm.chart_name || 'chart'}.tgz`,
        { type: resolved.chart_content_type || 'application/gzip' }
      )
      : null
    resolved.fileName = resolved.chart_file_name || resolved.resolved_chart?.file_name || ''
    resolved.chartName = resolved.chart_source?.chart_name || variantForm.chart_name
    resolved.chartVersion = resolved.chart_source?.chart_version || variantForm.chart_version
    resolved.valuesYAML = resolved.base_values_yaml || ''
    applyResolvedChartResult(
      resolved,
      variantForm.chart_source_type,
      variantForm.chart_source_type === 'repo'
        ? '已从 Helm Repo 获取 Chart 并提取 values.yaml'
        : '已从 OCI Registry 获取 Chart 并提取 values.yaml'
    )
  } catch (error) {
    resolvedChartFile.value = null
    chartResolveState.status = 'error'
    chartResolveState.message = error?.message || '解析 Chart 失败'
    if (showError) {
      ElMessage.error(chartResolveState.message)
    }
  }
}

function buildVariantPayload() {
  const chartSource = normalizeChartSourcePayload(variantForm)
  return {
    version: variantForm.version.trim(),
    status: variantForm.status,
    pipeline_id: variantForm.pipeline_id || 0,
    infra_type: variantForm.infra_type,
    version_description: variantForm.version_description.trim(),
    command_template: variantForm.infra_type === 'vm' ? variantForm.command_template : '',
    chart_source: chartSource,
    base_values_yaml: variantForm.infra_type === 'k8s' ? variantForm.base_values_yaml : '',
    parameters: variantForm.parameters
      .map((row, index) => ({
        name: row.name.trim(),
        label: (row.label || row.name).trim(),
        description: row.description.trim(),
        extra_tip: row.extra_tip.trim(),
        type: row.type,
        default_value: row.default_value,
        option_values: row.type === 'select'
          ? row.option_values_text.split(',').map((item) => item.trim()).filter(Boolean)
          : [],
        required: Boolean(row.required),
        advanced: Boolean(row.advanced),
        sort_order: index + 1
      }))
      .filter((row) => row.name)
  }
}

function buildVariantRequestData(payload) {
  if (variantForm.infra_type === 'k8s' && resolvedChartFile.value) {
    const formData = new FormData()
    formData.append('payload', JSON.stringify(payload))
    formData.append('chart_file', resolvedChartFile.value, resolvedChartFile.value.name)
    return formData
  }
  return payload
}

async function submitVariant() {
  if (!selectedApp.value?.id) return
  if (!variantForm.version.trim()) {
    ElMessage.warning('版本号不能为空')
    return
  }
  if (variantForm.infra_type === 'vm' && !variantForm.command_template.trim()) {
    ElMessage.warning('VM 版本必须提供命令模板')
    return
  }
  const chartSource = normalizeChartSourcePayload(variantForm)
  if (variantForm.infra_type === 'k8s') {
    const validationMessage = chartSource?.type === 'repo'
      ? (!chartSource.repo_url ? '请填写 Repo URL' : (!chartSource.chart_name ? '请填写 Chart 名称' : ''))
      : chartSource?.type === 'oci'
        ? (!chartSource.oci_url ? '请填写 OCI URL' : (!chartSource.chart_name ? '请填写 Chart 名称' : (!chartSource.chart_version ? '请填写 Chart Version' : '')))
        : chartSource?.type === 'upload'
          ? (!chartSource.file_name ? '请先选择 Chart 文件' : '')
          : 'Chart 来源配置无效'
    if (validationMessage) {
      ElMessage.warning(validationMessage)
      return
    }
    if (variantForm.chart_source_type === 'upload' && selectedChartFile.value && chartResolveState.status !== 'success') {
      await resolveSelectedUploadChart()
      if (chartResolveState.status !== 'success') {
        return
      }
    }
    if ((variantForm.chart_source_type === 'repo' || variantForm.chart_source_type === 'oci') && chartResolveState.status !== 'success') {
      await resolveRemoteChartSource(true)
      if (chartResolveState.status !== 'success') {
        return
      }
    }
    if (!resolvedChartFile.value) {
      ElMessage.warning('请先解析 Chart 并确认 values.yaml')
      return
    }
  }
  loading.submitVariant = true
  const payload = buildVariantPayload()
  const requestData = buildVariantRequestData(payload)

  try {
    let savedVariant
    if (variantForm.id) {
      const response = await updateTemplateVersion(selectedApp.value.id, variantForm.id, requestData)
      savedVariant = response.data
    } else {
      const response = await createTemplateVersion(selectedApp.value.id, requestData)
      savedVariant = response.data
    }

    dialogs.variant = false
    ElMessage.success('版本已保存')
    await loadVariants(selectedApp.value.id)
    await loadInitialData()
  } finally {
    loading.submitVariant = false
  }
}

async function removeVariant(variant) {
  if (!selectedApp.value?.id) return
  await ElMessageBox.confirm(`确认删除版本 ${variant.version} 吗？`, '删除版本', { type: 'warning' })
  await deleteTemplateVersion(selectedApp.value.id, variant.id)
  ElMessage.success('版本已删除')
  await loadVariants(selectedApp.value.id)
  await loadInitialData()
}

async function openDeployDialog(variant) {
  deployPreview.value = null
  deployState.variant = variant
  deployState.parameters = Object.fromEntries(
    normalizeParameterRows(variant.parameters || []).map((item) => [item.name, normalizeDeployDefaultValue(item)])
  )
  if (variant.infra_type === 'k8s') {
    deployState.parameters.namespace = String(deployState.parameters.namespace || '').trim() || 'default'
  }
  deployState.target_resource_id = null
  dialogs.deploy = true
  await loadDeployResources(variant.infra_type)
  const routeResourceId = Number(route.query.resource_id || route.query.target_resource_id)
  if (routeResourceId && deployResources.value.some((item) => item.id === routeResourceId)) {
    deployState.target_resource_id = routeResourceId
  } else if (deployResources.value.length === 1) {
    deployState.target_resource_id = deployResources.value[0].id
  }
  if (variant.infra_type === 'k8s') {
    const selectedResource = deployResources.value.find((item) => item.id === deployState.target_resource_id)
    const presetNamespace = String(route.query.namespace || selectedResource?.namespace || '').trim()
    applyNamespacePreset(deployState.parameters, deployParameterFields.value, presetNamespace || 'default')
    deployState.parameters.namespace = String(deployState.parameters.namespace || '').trim() || presetNamespace || 'default'
  }
  await refreshDeployPreview()
}

async function loadDeployResources(infraType) {
  loading.resources = true
  try {
    const response = await getResourceList({ type: infraType === 'k8s' ? 'k8s' : 'vm' })
    deployResources.value = response.data || []
  } finally {
    loading.resources = false
  }
}

async function refreshDeployPreview() {
  if (!dialogs.deploy || !selectedApp.value?.id || !deployState.variant?.id || !deployState.target_resource_id) {
    deployPreview.value = null
    return
  }
  try {
    const response = await previewTemplateVersion(selectedApp.value.id, deployState.variant.id, {
      target_resource_id: deployState.target_resource_id,
      parameters: deployState.parameters
    })
    deployPreview.value = response.data
  } catch {
    deployPreview.value = null
  }
}

async function submitDeploy() {
  if (!deployState.variant?.id || !deployState.target_resource_id) {
    ElMessage.warning('请选择目标资源')
    return
  }
  loading.submitDeploy = true
  try {
    await createDeploymentRequest({
      template_version_id: deployState.variant.id,
      target_resource_id: deployState.target_resource_id,
      parameters: deployState.parameters
    })
    dialogs.deploy = false
    ElMessage.success('部署请求已创建')
  } finally {
    loading.submitDeploy = false
  }
}

function normalizeDeployDefaultValue(parameter) {
  if (parameter.type === 'number') {
    const numeric = Number(parameter.default_value)
    return Number.isFinite(numeric) ? numeric : undefined
  }
  if (parameter.type === 'switch') {
    if (typeof parameter.default_value === 'boolean') return parameter.default_value
    return String(parameter.default_value || '').toLowerCase() === 'true'
  }
  return parameter.default_value ?? ''
}

function formatDiffText(line) {
  if (!line?.text) return ''
  return line.type === 'add' ? String(line.text).replace(/^\+/, '') : line.text
}
</script>

<style scoped>
.app-store-page {
  min-height: 100%;
  padding: 0;
}

.card-shell,
.panel-card {
  border-radius: 24px;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-md);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.detail-header h2 {
  margin: 0;
  font-family: "Outfit", "Plus Jakarta Sans", "PingFang SC", "Microsoft YaHei", sans-serif;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: -0.02em;
  color: var(--text-primary);
}

.detail-path,
.preview-label,
.panel-heading p,
.parameter-field small,
.upload-hint {
  color: var(--text-secondary);
}

.upload-status {
  margin-top: 8px;
  font-size: 12px;
}

.chart-resolve-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}

.upload-status.success {
  color: var(--color-success);
}

.upload-status.error {
  color: var(--color-danger);
}

.catalog-shell {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 16px;
}

.category-rail {
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.category-chip {
  border: 0;
  border-radius: 16px;
  padding: 12px 14px;
  background: transparent;
  color: var(--text-primary);
  display: flex;
  justify-content: space-between;
  align-items: center;
  font: inherit;
  cursor: pointer;
  transition: background 0.2s ease, transform 0.2s ease;
}

.category-chip.active,
.category-chip:hover {
  background: var(--primary-lighter);
  color: var(--primary-color);
  transform: translateX(2px);
}

.catalog-content {
  padding: 16px;
}

.catalog-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 14px;
}

.toolbar-search {
  max-width: 300px;
}

.catalog-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 14px;
}

.app-card {
  border: 1px solid var(--border-color-light);
  border-radius: 22px;
  background: var(--bg-elevated);
  padding: 16px;
  text-align: left;
  cursor: pointer;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}

.app-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-lg);
  border-color: var(--primary-light);
}

.app-card-top {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.app-card h3 {
  margin: 0;
  font-size: 18px;
}

.app-card p {
  margin: 8px 0 0;
  color: var(--text-secondary);
  line-height: 1.5;
}

.source-badge {
  border-radius: 999px;
  padding: 6px 10px;
  font-size: 12px;
  background: var(--primary-lighter);
  color: var(--primary-color);
  white-space: nowrap;
  height: fit-content;
}

.source-workspace {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.app-card-meta {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 14px;
  font-size: 13px;
  color: var(--text-secondary);
}

.detail-shell {
  padding: 20px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
}

.detail-stats {
  min-width: 220px;
  display: grid;
  gap: 10px;
}

.detail-stats > div {
  border: 1px solid var(--border-color-light);
  border-radius: 18px;
  padding: 12px 14px;
  background: var(--bg-elevated);
}

.detail-header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.detail-stats span {
  display: block;
  color: var(--text-secondary);
  font-size: 12px;
  margin-bottom: 4px;
}

.variant-list {
  border-top: 1px solid var(--border-color-light);
  padding-top: 12px;
}

.variant-list-head,
.variant-row {
  display: grid;
  grid-template-columns: 120px 90px minmax(0, 1.6fr) 180px 90px 260px;
  gap: 12px;
  align-items: center;
}

.variant-list-head {
  padding: 0 10px 10px;
  color: var(--text-secondary);
  font-size: 12px;
}

.variant-row {
  padding: 12px 10px;
  border-radius: 18px;
  border: 1px solid transparent;
}

.variant-row + .variant-row {
  margin-top: 8px;
}

.variant-row:hover {
  border-color: var(--border-color-light);
  background: var(--bg-elevated);
}

.variant-description {
  color: var(--text-secondary);
  line-height: 1.5;
}

.variant-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}

.dialog-grid {
  display: grid;
  gap: 16px;
}

.dialog-grid.two {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.dialog-grid.split {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
}

.panel-card {
  padding: 16px;
}

.panel-heading {
  margin-bottom: 14px;
}

.panel-heading h3 {
  margin: 0;
  font-size: 18px;
}

.panel-heading p {
  margin: 6px 0 0;
  line-height: 1.5;
}

.panel-heading.compact {
  margin-top: 20px;
}

.mono-input :deep(textarea),
.preview-block pre {
  font-family: "SFMono-Regular", "Menlo", "Consolas", monospace;
}

.parameter-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 10px;
}

.upload-shell {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}

.parameter-stack {
  display: grid;
  gap: 12px;
}

.parameter-field {
  border: 1px solid var(--border-color-light);
  border-radius: 18px;
  padding: 12px;
  background: var(--bg-elevated);
}

.parameter-label {
  display: block;
  margin-bottom: 8px;
  font-size: 13px;
  color: var(--text-primary);
}

.parameter-help-icon {
  color: var(--text-muted);
  margin-left: 4px;
  cursor: help;
}

.parameter-recommendation {
  margin-left: 8px;
  color: var(--text-muted);
  font-size: 12px;
}

.advanced-collapse {
  margin-top: 14px;
}

.preview-panel {
  display: flex;
  flex-direction: column;
}

.preview-block {
  border: 1px solid var(--border-color-light);
  border-radius: 18px;
  padding: 12px;
  background: var(--bg-elevated);
  box-shadow: var(--shadow-sm);
  color: var(--text-primary);
}

.preview-block + .preview-block {
  margin-top: 12px;
}

.preview-label {
  display: block;
  margin-bottom: 8px;
  color: var(--text-secondary);
}

.preview-block pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.6;
}

.diff-list {
  display: grid;
  gap: 4px;
}

.diff-line {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  border-radius: 10px;
  padding: 4px 6px;
}

.diff-line.add {
  color: var(--success-color);
  background: rgba(34, 197, 94, 0.08);
}

.diff-line.context {
  color: var(--text-secondary);
}

@media (max-width: 1024px) {
  .catalog-shell,
  .dialog-grid.split,
  .detail-header {
    grid-template-columns: 1fr;
  }

  .variant-list-head,
  .variant-row {
    grid-template-columns: 1fr;
  }

  .variant-actions {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
}
</style>
