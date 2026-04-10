<template>
  <div class="store-page">
    <div class="store-header-card">
      <div class="page-header">
        <div>
          <h1 class="page-title">商店</h1>
          <div class="page-subtitle">{{ pageSubtitle }}</div>
        </div>
        <div v-if="canManageTemplates" class="header-actions">
          <el-button v-if="isLLMStore" @click="openImportModelDialog">导入模型</el-button>
          <el-button type="primary" @click="openTemplateDialog">
            {{ isLLMStore ? '新建工作空间部署工具' : '新建工作空间模板' }}
          </el-button>
        </div>
      </div>

      <el-tabs :model-value="storeKind" class="store-kind-tabs" @tab-change="handleStoreTabChange">
        <el-tab-pane label="应用商店" name="app" />
        <el-tab-pane label="LLM 商店" name="llm" />
      </el-tabs>

      <div class="catalog-overview">
        <template v-if="isLLMStore">
          <div class="overview-card accent-primary">
            <span class="overview-label">已导入模型目录</span>
            <strong class="overview-value">{{ importedModelCount }}</strong>
            <span class="overview-hint">仅展示管理员从 Hugging Face / ModelScope 导入的本地模型元数据</span>
          </div>
          <div class="overview-card">
            <span class="overview-label">平台部署工具</span>
            <strong class="overview-value">{{ platformTemplates.length }}</strong>
            <span class="overview-hint">平台维护的稳定部署方式，可直接选版本发起部署</span>
          </div>
          <div class="overview-card accent-success">
            <span class="overview-label">工作空间部署工具</span>
            <strong class="overview-value">{{ workspaceTemplates.length }}</strong>
            <span class="overview-hint">当前团队维护，可继续新增版本和复用发布流水线</span>
          </div>
        </template>
        <template v-else>
          <div class="overview-card">
            <span class="overview-label">平台目录</span>
            <strong class="overview-value">{{ platformTemplates.length }}</strong>
            <span class="overview-hint">平台预置，适合直接选择发布</span>
          </div>
          <div class="overview-card accent-success">
            <span class="overview-label">工作空间目录</span>
            <strong class="overview-value">{{ workspaceTemplates.length }}</strong>
            <span class="overview-hint">当前团队维护，可继续迭代版本</span>
          </div>
        </template>
      </div>
    </div>

    <div v-if="resourceScopedDeployContext" class="resource-scope-card">
      <div>
        <span class="resource-scope-label">已从 K8s 资源浏览带入部署上下文</span>
        <strong class="resource-scope-value">{{ resourceScopedDeployContext.resourceName }}</strong>
        <span class="resource-scope-hint">
          目标资源已预设为当前集群
          <template v-if="resourceScopedDeployContext.namespace"> · 命名空间 {{ resourceScopedDeployContext.namespace }}</template>
          ，选择 K8s 模板后会自动带入。
        </span>
      </div>
      <el-button @click="goBackToScopedK8sBrowser">返回 K8s 浏览器</el-button>
    </div>

    <div class="page-filters card-shell">
      <el-input
        v-model="filters.keyword"
        :placeholder="isLLMStore ? '搜索本地模型或部署工具' : '搜索模板名称'"
        clearable
        style="width: 240px"
      />
      <el-select v-model="filters.resourceType" placeholder="目标资源类型" clearable style="width: 180px">
        <el-option label="VM" value="vm" />
        <el-option label="K8s 集群" value="k8s" />
      </el-select>
      <el-button @click="loadData">刷新</el-button>
    </div>

    <section v-if="isLLMStore" class="catalog-section card-shell local-model-section">
      <div class="catalog-header">
        <div>
          <h2 class="catalog-title">已导入模型目录</h2>
          <p class="catalog-description">先从管理员已导入的模型目录选择模型，再选部署工具、版本和参数完成部署，不提供终端用户远程搜索。</p>
        </div>
        <el-tag type="primary" size="large">{{ localModelsFiltered.length }} 个模型</el-tag>
      </div>

      <el-table v-if="localModelsFiltered.length > 0" :data="localModelsFiltered" class="model-list-table" style="width: 100%">
        <el-table-column label="模型" min-width="320">
          <template #default="{ row }">
            <div class="model-row-main">
              <div class="model-row-title">{{ row.name }}</div>
              <div class="model-row-subtitle">{{ row.sourceModelId || row.modelIdentifier || '-' }}</div>
              <div v-if="modelSummary(row)" class="model-row-summary">{{ modelSummary(row) }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="关键信息" min-width="360">
          <template #default="{ row }">
            <div class="model-row-metrics">
              <span>来源 {{ row.source || '-' }}</span>
              <span v-if="row.pipelineTag">任务 {{ row.pipelineTag }}</span>
              <span>License {{ row.license || '-' }}</span>
              <span>上下文 {{ row.contextLength || '-' }}</span>
              <span>下载 {{ formatCompactNumber(row.downloads) }}</span>
              <span>点赞 {{ formatCompactNumber(row.likes) }}</span>
            </div>
            <div v-if="modelChips(row).length > 0" class="model-row-chips">
              <el-tag v-for="chip in modelChips(row)" :key="`${row.catalogKey}-${chip}`" effect="plain" size="small">{{ chip }}</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="180">
          <template #default="{ row }">{{ formatDateTime(row.importedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link :disabled="!canDeployTemplates" @click="openLlmDeployDialog(row)">部署此模型</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-else description="暂无已导入模型，请先由管理员从 Hugging Face / ModelScope 导入模型元数据" />
    </section>

    <el-dialog v-model="importDialogVisible" title="导入模型元数据" width="560px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="模型来源" required>
          <el-select v-model="importForm.source" style="width: 100%">
            <el-option label="Hugging Face" value="huggingface" />
            <el-option label="ModelScope" value="modelscope" />
          </el-select>
        </el-form-item>
        <el-form-item label="模型 ID" required>
          <el-input v-model="importForm.source_model_id" placeholder="如 Qwen/Qwen2.5-7B-Instruct" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="importDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="importingModel" @click="submitImportModel">导入</el-button>
      </template>
    </el-dialog>

    <section class="catalog-section card-shell">
      <div class="catalog-header">
        <div>
          <h2 class="catalog-title">{{ isLLMStore ? '平台部署工具' : '平台目录' }}</h2>
          <p class="catalog-description">
            {{ isLLMStore ? '平台统一维护的稳定部署工具，可结合本地模型和版本参数直接发起部署。' : '面向全平台统一维护的稳定模板，适合直接选择版本后发起部署。' }}
          </p>
        </div>
        <el-tag type="info" size="large">{{ platformTemplates.length }} {{ isLLMStore ? '个工具' : '个模板' }}</el-tag>
      </div>

      <el-table :data="platformTemplates" v-loading="loading" style="width: 100%">
        <el-table-column prop="name" :label="isLLMStore ? '部署工具' : '模板名称'" min-width="220" />
        <el-table-column prop="target_resource_type" label="目标资源" width="120">
          <template #default="{ row }">
            <el-tag>{{ row.target_resource_type === 'vm' ? 'VM' : 'K8s' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="getTemplateStatusType(row.status)">{{ row.status || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="summary" label="摘要" min-width="260" />
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDeployDialog(row)">
              {{ isLLMStore ? '使用此工具' : '一键部署' }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && platformTemplates.length === 0" :description="isLLMStore ? '暂无平台部署工具' : '暂无平台模板'" />
    </section>

    <section class="catalog-section card-shell">
      <div class="catalog-header">
        <div>
          <h2 class="catalog-title">{{ isLLMStore ? '工作空间部署工具' : '工作空间目录' }}</h2>
          <p class="catalog-description">
            {{ isLLMStore ? '团队自定义部署工具和版本维护区，可继续绑定发布流水线并维护工具版本。' : '团队自定义模板和版本维护区，发布入口和版本管理都在这里继续演进。' }}
          </p>
        </div>
        <el-tag type="success" size="large">{{ workspaceTemplates.length }} {{ isLLMStore ? '个工具' : '个模板' }}</el-tag>
      </div>

      <el-table :data="workspaceTemplates" v-loading="loading" style="width: 100%">
        <el-table-column prop="name" :label="isLLMStore ? '部署工具' : '模板名称'" min-width="220" />
        <el-table-column prop="target_resource_type" label="目标资源" width="120">
          <template #default="{ row }">
            <el-tag>{{ row.target_resource_type === 'vm' ? 'VM' : 'K8s' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="getTemplateStatusType(row.status)">{{ row.status || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="summary" label="摘要" min-width="240" />
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDeployDialog(row)">
              {{ isLLMStore ? '使用此工具' : '一键部署' }}
            </el-button>
            <el-button v-if="canManageTemplates" link type="primary" @click="openVersionDialog(row)">
              新增版本
            </el-button>
            <el-button v-if="canManageTemplates" link type="danger" @click="removeTemplate(row)">
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && workspaceTemplates.length === 0" :description="isLLMStore ? '暂无工作空间部署工具' : '暂无工作空间模板'" />
    </section>

    <el-dialog v-model="templateDialogVisible" :title="isLLMStore ? '新增工作空间部署工具' : '新增工作空间模板'" width="620px">
      <el-form label-width="120px">
        <el-form-item :label="isLLMStore ? '工具名称' : '模板名称'" required><el-input v-model="templateForm.name" /></el-form-item>
        <el-form-item label="目标资源类型" required>
          <el-select v-model="templateForm.target_resource_type" style="width: 100%">
            <el-option label="VM" value="vm" />
            <el-option label="K8s 集群" value="k8s" />
          </el-select>
        </el-form-item>
        <el-form-item label="摘要"><el-input v-model="templateForm.summary" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="templateForm.description" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="templateDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="savingTemplate" @click="submitTemplate">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="versionDialogVisible" :title="isLLMStore ? '新增部署工具版本' : '新增模板版本'" width="620px">
      <el-form label-width="120px">
        <el-form-item label="版本号" required><el-input v-model="versionForm.version" placeholder="如 1.0.0" /></el-form-item>
        <el-form-item label="绑定流水线" required>
          <el-select v-model="versionForm.pipeline_id" filterable style="width: 100%">
            <el-option v-for="item in pipelines" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="versionDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="savingVersion" @click="submitTemplateVersion">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="deployDialogVisible"
      :title="isLLMStore ? '部署本地模型' : '一键部署'"
      width="760px"
      destroy-on-close
      class="store-deploy-dialog"
    >
      <div class="deploy-dialog-shell">
        <div v-if="isLLMStore" class="deploy-vram-panel" :class="`status-${deployVramEstimate.status}`">
          <div class="deploy-vram-cell">
            <span class="deploy-vram-label">模型权重</span>
            <strong class="deploy-vram-value">{{ formatEstimateGigabytes(deployVramEstimate.weightsGb) }}</strong>
            <span class="deploy-vram-detail">{{ deployVramEstimate.weightsRatioText }}</span>
          </div>
          <div class="deploy-vram-cell">
            <span class="deploy-vram-label">KV Cache</span>
            <strong class="deploy-vram-value">{{ formatEstimateGigabytes(deployVramEstimate.kvCacheGb) }}</strong>
            <span class="deploy-vram-detail">{{ deployVramEstimate.kvCacheRatioText }}</span>
          </div>
          <div class="deploy-vram-cell">
            <span class="deploy-vram-label">运行时预留</span>
            <strong class="deploy-vram-value">{{ formatEstimateGigabytes(deployVramEstimate.runtimeReserveGb) }}</strong>
            <span class="deploy-vram-detail">{{ deployVramEstimate.runtimeReserveRatioText }}</span>
          </div>
          <div class="deploy-vram-cell">
            <span class="deploy-vram-label">CPU 卸载</span>
            <strong class="deploy-vram-value">{{ formatEstimateGigabytes(deployVramEstimate.cpuOffloadGb, { signed: true, invertSign: true }) }}</strong>
            <span class="deploy-vram-detail">{{ deployVramEstimate.cpuOffloadRatioText }}</span>
          </div>
          <div class="deploy-vram-cell emphasis">
            <span class="deploy-vram-label">总计 / 已选显卡</span>
            <strong class="deploy-vram-value">{{ deployVramEstimate.totalVsGpuText }}</strong>
            <span class="deploy-vram-detail">{{ deployVramEstimate.totalRatioText }}</span>
          </div>
        </div>

        <el-form label-position="top" class="deploy-form">
          <el-form-item v-if="isLLMStore" label="本地模型" required>
            <el-select
              v-model="deployForm.model_id"
              filterable
              placeholder="选择管理员已导入的本地模型"
              style="width: 100%"
              @change="handleDeployModelChange"
            >
              <el-option
                v-for="model in localModels"
                :key="model.catalogKey"
                :label="modelSelectLabel(model)"
                :value="model.catalogId"
              />
            </el-select>
          </el-form-item>

          <el-form-item :label="isLLMStore ? '部署工具' : '模板'" required>
            <el-select
              v-if="isLLMStore"
              v-model="deployForm.template_id"
              filterable
              placeholder="选择部署工具"
              style="width: 100%"
              @change="handleDeployTemplateChange"
            >
              <el-option
                v-for="item in deployableTemplates"
                :key="item.id"
                :label="item.name"
                :value="item.id"
              />
            </el-select>
            <el-input v-else :model-value="selectedTemplate?.name || '-'" disabled />
          </el-form-item>

          <div class="deploy-grid">
            <el-form-item label="模板版本" required>
              <el-select
                v-model="deployForm.template_version_id"
                placeholder="选择模板版本"
                style="width: 100%"
                @change="handleDeployVersionChange"
              >
                <el-option v-for="item in templateVersions" :key="item.id" :label="item.version" :value="item.id" />
              </el-select>
            </el-form-item>

            <el-form-item label="目标资源" required>
              <el-select v-model="deployForm.target_resource_id" filterable placeholder="选择目标资源" style="width: 100%">
                <el-option
                  v-for="item in availableResources"
                  :key="item.id"
                  :label="`${item.name} (${item.endpoint || item.type})`"
                  :value="item.id"
                />
              </el-select>
            </el-form-item>
          </div>

          <el-form-item v-if="isLLMStore && canSelectGpuDevices" label="GPU">
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

          <div class="parameter-panel">
            <div class="parameter-header">
              <div>
                <h3 class="parameter-title">{{ isLLMStore ? '工具参数' : '部署参数' }}</h3>
                <p class="parameter-description">{{ parameterSectionHint }}</p>
              </div>
              <el-tag v-if="deployParameterFields.length > 0" type="info">{{ deployParameterFields.length }} 项</el-tag>
            </div>

            <div v-if="basicDeployParameterFields.length > 0" class="parameter-grid">
              <div
                v-for="field in basicDeployParameterFields"
                :key="field.key"
                class="parameter-field"
                :class="{ full: field.fullWidth }"
              >
                <el-form-item :required="field.required">
                  <template #label>
                    <span class="parameter-label">
                        <span>{{ field.label }}</span>
                        <span v-if="field.description" class="parameter-recommendation">{{ field.description }}</span>
                        <el-tooltip v-if="field.extraTip" :content="field.extraTip" placement="top" effect="dark">
                          <el-icon class="parameter-help-icon"><QuestionFilled /></el-icon>
                        </el-tooltip>
                      <el-tag v-if="field.mutable === false" size="small" effect="plain">只读</el-tag>
                    </span>
                  </template>
                  <el-input
                    v-if="field.type === 'text' || field.type === 'password'"
                    v-model="deployForm.parameters[field.key]"
                    :type="field.type === 'password' ? 'password' : 'text'"
                    :show-password="field.type === 'password'"
                    :placeholder="field.placeholder"
                    :disabled="field.mutable === false"
                  />
                  <el-input
                    v-else-if="field.type === 'textarea' || field.type === 'json'"
                    v-model="deployForm.parameters[field.key]"
                    type="textarea"
                    :rows="field.rows"
                    :placeholder="field.placeholder"
                    :disabled="field.mutable === false"
                  />
                  <el-input-number
                    v-else-if="field.type === 'number'"
                    v-model="deployForm.parameters[field.key]"
                    :min="field.min"
                    :max="field.max"
                    :step="field.step"
                    class="parameter-number"
                    :disabled="field.mutable === false"
                  />
                  <el-switch
                    v-else-if="field.type === 'switch'"
                    v-model="deployForm.parameters[field.key]"
                    :disabled="field.mutable === false"
                  />
                  <el-select
                    v-else-if="field.type === 'select'"
                    v-model="deployForm.parameters[field.key]"
                    :placeholder="field.placeholder || '请选择'"
                    style="width: 100%"
                    :disabled="field.mutable === false"
                  >
                    <el-option
                      v-for="option in field.options"
                      :key="`${field.key}-${option.value}`"
                      :label="option.label"
                      :value="option.value"
                    />
                  </el-select>
                  <el-input
                    v-else
                    v-model="deployForm.parameters[field.key]"
                    :placeholder="field.placeholder"
                    :disabled="field.mutable === false"
                  />
                </el-form-item>
              </div>
            </div>

            <el-collapse v-if="advancedDeployParameterFields.length > 0" v-model="advancedPanels" class="advanced-parameter-collapse">
              <el-collapse-item :title="`高阶参数（${advancedDeployParameterFields.length} 项）`" name="advanced">
                <div class="parameter-grid advanced-grid">
                  <div
                    v-for="field in advancedDeployParameterFields"
                    :key="field.key"
                    class="parameter-field"
                    :class="{ full: field.fullWidth }"
                  >
                    <el-form-item :required="field.required">
                      <template #label>
                        <span class="parameter-label">
                          <span>{{ field.label }}</span>
                          <span v-if="field.description" class="parameter-recommendation">{{ field.description }}</span>
                          <el-tooltip v-if="field.extraTip" :content="field.extraTip" placement="top" effect="dark">
                            <el-icon class="parameter-help-icon"><QuestionFilled /></el-icon>
                          </el-tooltip>
                          <el-tag v-if="field.mutable === false" size="small" effect="plain">只读</el-tag>
                        </span>
                      </template>
                      <el-input
                        v-if="field.type === 'text' || field.type === 'password'"
                        v-model="deployForm.parameters[field.key]"
                        :type="field.type === 'password' ? 'password' : 'text'"
                        :show-password="field.type === 'password'"
                        :placeholder="field.placeholder"
                        :disabled="field.mutable === false"
                      />
                      <el-input
                        v-else-if="field.type === 'textarea' || field.type === 'json'"
                        v-model="deployForm.parameters[field.key]"
                        type="textarea"
                        :rows="field.rows"
                        :placeholder="field.placeholder"
                        :disabled="field.mutable === false"
                      />
                      <el-input-number
                        v-else-if="field.type === 'number'"
                        v-model="deployForm.parameters[field.key]"
                        :min="field.min"
                        :max="field.max"
                        :step="field.step"
                        class="parameter-number"
                        :disabled="field.mutable === false"
                      />
                      <el-switch
                        v-else-if="field.type === 'switch'"
                        v-model="deployForm.parameters[field.key]"
                        :disabled="field.mutable === false"
                      />
                      <el-select
                        v-else-if="field.type === 'select'"
                        v-model="deployForm.parameters[field.key]"
                        :placeholder="field.placeholder || '请选择'"
                        style="width: 100%"
                        :disabled="field.mutable === false"
                      >
                        <el-option
                          v-for="option in field.options"
                          :key="`${field.key}-${option.value}`"
                          :label="option.label"
                          :value="option.value"
                        />
                      </el-select>
                      <el-input
                        v-else
                        v-model="deployForm.parameters[field.key]"
                        :placeholder="field.placeholder"
                        :disabled="field.mutable === false"
                      />
                    </el-form-item>
                  </div>
                </div>
              </el-collapse-item>
            </el-collapse>

            <el-empty
              v-else-if="basicDeployParameterFields.length === 0 && advancedDeployParameterFields.length === 0"
              :description="isLLMStore ? '当前版本未返回额外参数，提交时将附带所选本地模型信息。' : '当前版本未返回额外参数，将沿用默认部署参数。'"
              :image-size="76"
            />
          </div>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="deployDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="creatingDeployment" @click="submitDeployment">发起部署</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'
import { getResourceList } from '@/api/resource'
import { createDeploymentRequest } from '@/api/deployment'
import { applyNamespacePreset, buildResourceK8sRouteLocation } from '@/views/resources/k8s/utils'
import {
  createTemplate,
  createTemplateVersion,
  deleteTemplate,
  importLocalLlmModel,
  getLocalLlmCatalog,
  getTemplateList,
  getTemplateVersions
} from '@/api/store'
import { getPipelineList } from '@/api/pipeline'
import { extractCanonicalParameters } from '../appStoreHelpers'

const props = defineProps({
  storeKind: { type: String, required: true }
})

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const savingTemplate = ref(false)
const savingVersion = ref(false)
const creatingDeployment = ref(false)
const importingModel = ref(false)
const templateDialogVisible = ref(false)
const versionDialogVisible = ref(false)
const deployDialogVisible = ref(false)
const importDialogVisible = ref(false)
const advancedPanels = ref([])
const templates = ref([])
const resources = ref([])
const pipelines = ref([])
const templateVersions = ref([])
const localModels = ref([])
const deployParameterFields = ref([])
const selectedTemplateIdForVersion = ref(0)
const selectedGpuDeviceKeys = ref([])
const filters = reactive({ keyword: '', resourceType: '' })
const templateForm = reactive({ name: '', target_resource_type: 'vm', summary: '', description: '' })
const versionForm = reactive({ version: '', pipeline_id: null })
const importForm = reactive({ source: 'huggingface', source_model_id: '' })
const deployForm = reactive({
  template_id: null,
  template_version_id: null,
  target_resource_id: null,
  model_id: null,
  parameters: {}
})

const isLLMStore = computed(() => props.storeKind === 'llm')
const canManageTemplates = computed(() => userStore.hasPermission('store.template.manage'))
const canDeployTemplates = computed(() => userStore.hasPermission('store.template.use') && userStore.hasPermission('resource.use'))

const pageSubtitle = computed(() => isLLMStore.value
  ? '先选择本地导入模型，再选择部署工具、版本和工具参数完成部署。'
  : '平台目录与工作空间目录统一承载应用模板发布入口。'
)

const filteredTemplates = computed(() => templates.value.filter(item => {
  if (item.template_type !== props.storeKind) return false
  if (filters.resourceType && item.target_resource_type !== filters.resourceType) return false
  if (!filters.keyword) return true
  const keyword = filters.keyword.toLowerCase()
  return [item.name, item.summary, item.description].some(value => String(value || '').toLowerCase().includes(keyword))
}))

const deployableTemplates = computed(() => templates.value.filter(item => item.template_type === props.storeKind))
const platformTemplates = computed(() => filteredTemplates.value.filter(item => item.source === 'platform'))
const workspaceTemplates = computed(() => filteredTemplates.value.filter(item => item.source !== 'platform'))
const selectedTemplate = computed(() => templates.value.find(item => String(item.id) === String(deployForm.template_id)) || null)
const selectedModel = computed(() => localModels.value.find(item => String(item.catalogId) === String(deployForm.model_id)) || null)
const selectedResource = computed(() => resources.value.find(item => String(item.id) === String(deployForm.target_resource_id)) || null)
const selectedResourceType = computed(() => selectedResource.value?.type || selectedTemplate.value?.target_resource_type || '')
const selectedResourceBaseInfo = computed(() => parseJSONSafely(selectedResource.value?.base_info || selectedResource.value?.baseInfo))
const selectedResourceGpuDevices = computed(() => normalizeGpuDevices(selectedResourceBaseInfo.value?.machine?.gpu?.devices))
const selectedGpuDevices = computed(() => {
  const selectedKeys = new Set(selectedGpuDeviceKeys.value)
  return selectedResourceGpuDevices.value.filter(device => selectedKeys.has(device.deviceKey))
})
const canSelectGpuDevices = computed(() => isLLMStore.value && selectedResourceType.value === 'vm' && selectedResourceGpuDevices.value.length > 0)
const selectedTensorParallelSize = computed(() => {
  const value = pickFirstParameterValue(['tensor_parallel_size', 'tensor-parallel-size', 'tensorParallelSize'])
  const numeric = Math.round(parseScaledNumber(value))
  return Math.max(numeric || 1, 1)
})

const availableResources = computed(() => resources.value.filter(item => {
  if (!selectedTemplate.value?.target_resource_type) return true
  return item.type === selectedTemplate.value.target_resource_type
}))

const localModelsFiltered = computed(() => {
  if (!isLLMStore.value) return []
  if (!filters.keyword) return localModels.value
  const keyword = filters.keyword.toLowerCase()
  return localModels.value.filter(item => [item.name, item.modelIdentifier, item.path, item.source].some(value => String(value || '').toLowerCase().includes(keyword)))
})

const importedModelCount = computed(() => localModels.value.filter(item => item.imported).length)
const basicDeployParameterFields = computed(() => deployParameterFields.value.filter(field => field.advanced !== true))
const advancedDeployParameterFields = computed(() => deployParameterFields.value.filter(field => field.advanced === true))
const resourceScopedDeployContext = computed(() => {
  const targetResourceId = Number(route.query.target_resource_id || 0)
  const source = String(route.query.source || '').trim()
  if (!targetResourceId || source !== 'resource-k8s') return null

  const resource = resources.value.find(item => String(item.id) === String(targetResourceId)) || null
  if (resource?.type && resource.type !== 'k8s') return null

  const namespace = String(route.query.namespace || '').trim()

  return {
    targetResourceId,
    namespace,
    resourceName: resource?.name || String(route.query.resource_name || '').trim() || `资源#${targetResourceId}`,
    browserLink: buildResourceK8sRouteLocation(targetResourceId, namespace)
  }
})

const parameterSectionHint = computed(() => {
  if (isLLMStore.value) {
    return '参数表单根据所选部署工具版本的 parameters（统一数组结构）动态生成。'
  }
  return '优先读取模板版本返回的 parameters（统一数组结构）；未提供时保留原有默认部署参数。'
})

const deployVramEstimate = computed(() => {
  const parameterCount = parseModelParameterCount(
    selectedModel.value?.parameterSize
    || selectedModel.value?.metadata?.parameter_size
    || selectedModel.value?.metadata?.model_size
    || selectedModel.value?.metadata?.ModelInfos?.safetensor?.model_size
    || selectedModel.value?.metadata?.modelInfos?.safetensor?.model_size
    || selectedModel.value?.metadata?.model_infos?.safetensor?.model_size
    || selectedModel.value?.metadata?.cardData?.model_size
  )
  const modelPrecision = resolveModelPrecisionBytes(selectedModel.value)
  const kvPrecision = resolveKvPrecisionBytes(selectedModel.value)
  const contextLength = resolveEstimateContextLength(selectedModel.value?.contextLength)
  const maxSequences = resolveEstimateMaxSequences()
  const gpuUtilization = resolveEstimateGpuUtilization()
  const cpuOffloadGb = resolveEstimateCpuOffloadGb()
  const selectedGpuMemoryBytes = selectedGpuDevices.value.reduce((total, device) => total + device.memoryBytes, 0)
  const selectedGpuCount = selectedGpuDevices.value.length
  const totalGpuCount = selectedResourceGpuDevices.value.length
  const tensorParallelSize = selectedTensorParallelSize.value
  const runtimeGpuCount = Math.max(selectedGpuCount || totalGpuCount || 0, 1)

  const weightsBytes = parameterCount > 0 ? parameterCount * modelPrecision : 0
  const kvCacheBytes = parameterCount > 0 && contextLength > 0
    ? resolveKvBytesPerToken(parameterCount, selectedModel.value?.architecture, kvPrecision) * contextLength * maxSequences
    : 0
  const runtimeReserveBytes = parameterCount > 0
    ? resolveRuntimeReserveBytes(weightsBytes, kvCacheBytes, runtimeGpuCount)
    : 0
  const cpuOffloadBytes = Math.min(cpuOffloadGb * 1024 * 1024 * 1024, weightsBytes)
  const totalBytes = Math.max(weightsBytes - cpuOffloadBytes, 0) + kvCacheBytes + runtimeReserveBytes
  const usageRatio = selectedGpuMemoryBytes > 0 ? totalBytes / selectedGpuMemoryBytes : null
  const usageCapBytes = selectedGpuMemoryBytes > 0 ? selectedGpuMemoryBytes * gpuUtilization : 0
  const insufficientGpuSelection = canSelectGpuDevices.value && selectedGpuCount < tensorParallelSize
  const totalGb = bytesToGigabytes(totalBytes)

  const ratioText = (value, options = {}) => {
    const { signed = false, invertSign = false } = options
    const numeric = Number(value || 0)
    if (!Number.isFinite(numeric) || Math.abs(numeric) < 0.0001) return signed ? '0%' : '-'
    const resolved = invertSign ? -numeric : numeric
    const prefix = signed && resolved > 0 ? '+' : ''
    return `${prefix}${resolved.toFixed(1)}%`
  }
  const partRatio = (partGb) => totalGb > 0 ? (partGb / totalGb) * 100 : 0

  let status = 'info'
  if (selectedGpuMemoryBytes > 0 && parameterCount > 0) {
    if (insufficientGpuSelection || usageRatio > gpuUtilization) {
      status = 'danger'
    } else if (usageRatio > 0.8) {
      status = 'warning'
    } else {
      status = 'success'
    }
  } else if (canSelectGpuDevices.value && selectedGpuCount === 0) {
    status = 'danger'
  }

  return {
    status,
    weightsGb: bytesToGigabytes(weightsBytes),
    weightsRatioText: ratioText(partRatio(bytesToGigabytes(weightsBytes))),
    kvCacheGb: bytesToGigabytes(kvCacheBytes),
    kvCacheRatioText: ratioText(partRatio(bytesToGigabytes(kvCacheBytes))),
    runtimeReserveGb: bytesToGigabytes(runtimeReserveBytes),
    runtimeReserveRatioText: ratioText(partRatio(bytesToGigabytes(runtimeReserveBytes))),
    cpuOffloadGb,
    cpuOffloadRatioText: ratioText(partRatio(cpuOffloadGb), { signed: true, invertSign: true }),
    totalGb,
    totalVsGpuText: selectedGpuMemoryBytes > 0
      ? `${formatEstimateGigabytes(totalGb)} / ${formatEstimateGigabytes(bytesToGigabytes(selectedGpuMemoryBytes))}`
      : '-',
    totalRatioText: usageRatio !== null
      ? `${ratioText(usageRatio * 100)}${usageCapBytes > 0 ? ` / ${ratioText(gpuUtilization * 100)}` : ''}`
      : insufficientGpuSelection
        ? `${selectedGpuCount}/${tensorParallelSize} GPU`
        : totalGpuCount > 0
          ? `${selectedGpuCount}/${totalGpuCount} GPU`
          : selectedResourceType.value === 'k8s'
            ? '未上报'
            : '-',
    selectedGpuCount,
    tensorParallelSize
  }
})

const handleStoreTabChange = (tabName) => {
  if ((tabName === 'app' && route.path === '/store/apps') || (tabName === 'llm' && route.path === '/store/llms')) return
  router.push({
    path: tabName === 'llm' ? '/store/llms' : '/store/apps',
    query: resourceScopedDeployContext.value ? { ...route.query } : undefined
  })
}

const syncScopedStoreFilters = () => {
  if (!resourceScopedDeployContext.value) return
  filters.resourceType = 'k8s'
}

const applyExternalDeployContext = () => {
  const context = resourceScopedDeployContext.value
  if (!context) return []
  if (selectedTemplate.value?.target_resource_type && selectedTemplate.value.target_resource_type !== 'k8s') return []

  deployForm.target_resource_id = context.targetResourceId
  return applyNamespacePreset(deployForm.parameters, deployParameterFields.value, context.namespace)
}

const goBackToScopedK8sBrowser = () => {
  if (!resourceScopedDeployContext.value) return
  router.push(resourceScopedDeployContext.value.browserLink)
}

const loadData = async () => {
  loading.value = true
  try {
    const requests = [
      getTemplateList({ template_type: props.storeKind }),
      getResourceList(),
      getPipelineList({ page_size: 100, include_publish_owned: true })
    ]

    if (isLLMStore.value) {
      requests.push(getLocalLlmCatalog())
    }

    const [templateRes, resourceRes, pipelineRes, llmCatalogRes] = await Promise.all(requests)
    templates.value = extractArray(templateRes?.data)
    resources.value = extractArray(resourceRes?.data)
    pipelines.value = extractArray(pipelineRes?.data)
    localModels.value = isLLMStore.value ? normalizeLocalModels(llmCatalogRes?.data ?? llmCatalogRes) : []
    syncScopedStoreFilters()
  } finally {
    loading.value = false
  }
}

const openTemplateDialog = () => {
  templateForm.name = ''
  templateForm.target_resource_type = 'vm'
  templateForm.summary = ''
  templateForm.description = ''
  templateDialogVisible.value = true
}

const openImportModelDialog = () => {
  importForm.source = 'huggingface'
  importForm.source_model_id = ''
  importDialogVisible.value = true
}

const submitImportModel = async () => {
  if (!importForm.source_model_id.trim()) {
    ElMessage.warning('请输入模型 ID')
    return
  }
  importingModel.value = true
  try {
    await importLocalLlmModel({
      source: importForm.source,
      source_model_id: importForm.source_model_id.trim()
    })
    ElMessage.success('模型元数据已导入')
    importDialogVisible.value = false
    await loadData()
  } finally {
    importingModel.value = false
  }
}

const submitTemplate = async () => {
  if (!templateForm.name.trim()) {
    ElMessage.warning(`请输入${isLLMStore.value ? '部署工具' : '模板'}名称`)
    return
  }
  savingTemplate.value = true
  try {
    await createTemplate({
      name: templateForm.name.trim(),
      description: templateForm.description.trim(),
      summary: templateForm.summary.trim(),
      template_type: props.storeKind,
      target_resource_type: templateForm.target_resource_type,
      source: 'workspace'
    })
    ElMessage.success(isLLMStore.value ? '部署工具已创建' : '模板已创建')
    templateDialogVisible.value = false
    await loadData()
  } finally {
    savingTemplate.value = false
  }
}

const openVersionDialog = (row) => {
  selectedTemplateIdForVersion.value = row.id
  versionForm.version = ''
  versionForm.pipeline_id = null
  versionDialogVisible.value = true
}

const submitTemplateVersion = async () => {
  if (!versionForm.version.trim() || !versionForm.pipeline_id) {
    ElMessage.warning('请填写版本号并选择流水线')
    return
  }
  savingVersion.value = true
  try {
    await createTemplateVersion(selectedTemplateIdForVersion.value, {
      version: versionForm.version.trim(),
      pipeline_id: versionForm.pipeline_id,
      deployment_mode: 'pipeline',
      status: 'published',
      default_config: '{}',
      dependency_config: '{}',
      target_constraints: '{}'
    })
    ElMessage.success(isLLMStore.value ? '部署工具版本已创建' : '模板版本已创建')
    versionDialogVisible.value = false
  } finally {
    savingVersion.value = false
  }
}

const resetDeployDialog = (templateName = '') => {
  deployForm.template_id = null
  deployForm.template_version_id = null
  deployForm.target_resource_id = null
  deployForm.model_id = null
  deployForm.parameters = isLLMStore.value ? {} : buildFallbackParameterValues(templateName)
  deployParameterFields.value = isLLMStore.value ? [] : buildFallbackParameterFields(templateName)
  templateVersions.value = []
  advancedPanels.value = []
  selectedGpuDeviceKeys.value = []
}

const openDeployDialog = async (row) => {
  if (!canDeployTemplates.value) {
    ElMessage.warning('当前工作空间没有部署权限')
    return
  }

  resetDeployDialog(row?.name || '')
  deployDialogVisible.value = true

  if (row?.id) {
    deployForm.template_id = row.id
    applyExternalDeployContext()
    const hasVersions = await loadTemplateVersions(row.id)
    if (!hasVersions) {
      ElMessage.warning(`当前${isLLMStore.value ? '部署工具' : '模板'}还没有可用版本`)
    }
  }
}

const openLlmDeployDialog = (model) => {
  if (!canDeployTemplates.value) {
    ElMessage.warning('当前工作空间没有部署权限')
    return
  }
  resetDeployDialog()
  deployForm.model_id = model.catalogId
  applyExternalDeployContext()
  deployDialogVisible.value = true
}

const handleDeployModelChange = () => {
  syncSelectedModelIntoParameters()
}

const handleDeployTemplateChange = async (templateId) => {
  deployForm.target_resource_id = null
  deployForm.template_version_id = null
  templateVersions.value = []
  deployForm.parameters = isLLMStore.value ? {} : buildFallbackParameterValues(selectedTemplate.value?.name || '')
  deployParameterFields.value = isLLMStore.value ? [] : buildFallbackParameterFields(selectedTemplate.value?.name || '')

  if (!templateId) {
    return
  }

  const hasVersions = await loadTemplateVersions(templateId)
  applyExternalDeployContext()
  if (!hasVersions) {
    ElMessage.warning(`当前${isLLMStore.value ? '部署工具' : '模板'}还没有可用版本`)
  }
}

const loadTemplateVersions = async (templateId) => {
  const res = await getTemplateVersions(templateId)
  templateVersions.value = extractArray(res?.data)

  if (templateVersions.value.length === 0) {
    deployParameterFields.value = isLLMStore.value ? [] : buildFallbackParameterFields(selectedTemplate.value?.name || '')
    deployForm.parameters = isLLMStore.value ? {} : buildFallbackParameterValues(selectedTemplate.value?.name || '')
    applyExternalDeployContext()
    return false
  }

  deployForm.template_version_id = templateVersions.value[0].id
  await handleDeployVersionChange(deployForm.template_version_id)
  applyExternalDeployContext()
  return true
}

const handleDeployVersionChange = async (versionId) => {
  if (!versionId) {
    deployParameterFields.value = isLLMStore.value ? [] : buildFallbackParameterFields(selectedTemplate.value?.name || '')
    deployForm.parameters = isLLMStore.value ? {} : buildFallbackParameterValues(selectedTemplate.value?.name || '')
    applyExternalDeployContext()
    return
  }

  const currentVersion = templateVersions.value.find(item => String(item.id) === String(versionId))
  let fields = normalizeParameterFields(currentVersion)

  if (fields.length === 0 && !isLLMStore.value) {
    fields = buildFallbackParameterFields(selectedTemplate.value?.name || '')
  }

  deployParameterFields.value = fields
  deployForm.parameters = buildParameterValues(fields, deployForm.parameters, selectedTemplate.value?.name || '')
  advancedPanels.value = fields.some(field => field.advanced === true) ? ['advanced'] : []
  syncSelectedModelIntoParameters()
  syncSelectedGpuIntoParameters()
  applyExternalDeployContext()
}

const syncSelectedModelIntoParameters = () => {
  if (!isLLMStore.value || !selectedModel.value) return

  const bindings = {
    model_id: selectedModel.value.catalogId,
    model_name: selectedModel.value.name,
    model: selectedModel.value.modelIdentifier || selectedModel.value.name,
    model_identifier: selectedModel.value.modelIdentifier || selectedModel.value.name
  }

  deployParameterFields.value.forEach(field => {
    if (bindings[field.key] !== undefined) {
      deployForm.parameters[field.key] = bindings[field.key]
    }
  })
}

const syncSelectedGpuIntoParameters = () => {
  if (!isLLMStore.value) return

  const gpuIndexValue = selectedGpuDevices.value.map(device => device.index).join(',')
  const gpuUuidValue = selectedGpuDevices.value.map(device => device.uuid).filter(Boolean).join(',')
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

  deployParameterFields.value.forEach(field => {
    if (bindings[field.key] !== undefined && bindings[field.key]) {
      deployForm.parameters[field.key] = bindings[field.key]
    } else if (bindings[field.key] !== undefined && deployForm.parameters[field.key] === '') {
      delete deployForm.parameters[field.key]
    }
  })
}

const submitDeployment = async () => {
  if (isLLMStore.value && !deployForm.model_id) {
    ElMessage.warning('请选择本地模型')
    return
  }
  if (isLLMStore.value && !deployForm.template_id) {
    ElMessage.warning('请选择部署工具')
    return
  }
  if (!deployForm.template_version_id || !deployForm.target_resource_id) {
    ElMessage.warning('请选择模板版本和目标资源')
    return
  }

  const missingField = deployParameterFields.value.find(field => field.required && isEmptyValue(deployForm.parameters[field.key]))
  if (missingField) {
    ElMessage.warning(`请填写${missingField.label}`)
    return
  }
  if (
    isLLMStore.value &&
    selectedTemplate.value?.name === 'vLLM' &&
    selectedTemplate.value?.target_resource_type === 'vm' &&
    (!isEmptyValue(deployForm.parameters.model_path) || !isEmptyValue(deployForm.parameters.host_model_dir) || !isEmptyValue(deployForm.parameters.container_model_dir)) &&
    (isEmptyValue(deployForm.parameters.host_model_dir) || isEmptyValue(deployForm.parameters.container_model_dir))
  ) {
    ElMessage.warning('请同时填写宿主机模型路径和容器模型路径')
    return
  }

  creatingDeployment.value = true
  try {
    await createDeploymentRequest({
      template_version_id: deployForm.template_version_id,
      target_resource_id: deployForm.target_resource_id,
      llm_model_id: isLLMStore.value ? deployForm.model_id : undefined,
      parameters: buildSubmissionParameters()
    })
    ElMessage.success('部署请求已创建')
    deployDialogVisible.value = false
  } finally {
    creatingDeployment.value = false
  }
}

const buildSubmissionParameters = () => {
  const parameters = {}

  Object.entries(deployForm.parameters || {}).forEach(([key, value]) => {
    if (value === undefined || value === null) return
    if (typeof value === 'string' && value.trim() === '') return
    parameters[key] = typeof value === 'string' ? value.trim() : value
  })

  return parameters
}

const removeTemplate = async (row) => {
  await ElMessageBox.confirm(`确认删除${isLLMStore.value ? '部署工具' : '模板'} ${row.name} 吗？`, '提示', { type: 'warning' })
  await deleteTemplate(row.id)
  ElMessage.success(isLLMStore.value ? '部署工具已删除' : '模板已删除')
  await loadData()
}

const modelSelectLabel = (model) => {
  const detail = model.sourceModelId || model.modelIdentifier || '已导入模型'
  return `${model.name} · ${detail}`
}

const modelChips = (model) => [model.format, model.quantization, model.architecture, model.parameterSize].filter(Boolean).slice(0, 4)

const modelSummary = (model) => model.summary || model.pipelineTag || ''

const formatCompactNumber = (value) => {
  const numeric = Number(value)
  if (!Number.isFinite(numeric) || numeric <= 0) return '-'
  if (numeric >= 1000000) return `${(numeric / 1000000).toFixed(1)}M`
  if (numeric >= 1000) return `${(numeric / 1000).toFixed(1)}K`
  return String(numeric)
}

const bytesToGigabytes = (value) => Number(value || 0) / (1024 ** 3)

const formatEstimateGigabytes = (value, options = {}) => {
  const { signed = false, invertSign = false } = options
  const numeric = Number(value || 0)
  if (!Number.isFinite(numeric) || numeric <= 0) {
    return signed ? '0 GB' : '-'
  }
  const resolved = invertSign ? -numeric : numeric
  const absValue = Math.abs(resolved)
  const digits = absValue >= 100 ? 0 : absValue >= 10 ? 1 : 2
  const prefix = signed && resolved > 0 ? '+' : ''
  return `${prefix}${resolved.toFixed(digits)} GB`
}

const formatTokenCount = (value) => {
  const numeric = Number(value || 0)
  if (!Number.isFinite(numeric) || numeric <= 0) return '-'
  if (numeric >= 1024 ** 2) {
    const scaled = numeric / (1024 ** 2)
    return `${scaled >= 10 ? scaled.toFixed(0) : scaled.toFixed(1)}M`
  }
  if (numeric >= 1024) {
    const scaled = numeric / 1024
    return `${scaled >= 10 ? scaled.toFixed(0) : scaled.toFixed(1)}K`
  }
  return String(Math.round(numeric))
}

const parseScaledNumber = (value, unitMap = {}) => {
  if (typeof value === 'number') {
    return Number.isFinite(value) ? value : 0
  }
  const text = String(value || '').trim().toLowerCase().replace(/,/g, '')
  if (!text) return 0
  const match = text.match(/^(-?\d+(?:\.\d+)?)\s*([a-z%]+)?$/)
  if (!match) {
    const numeric = Number(text)
    return Number.isFinite(numeric) ? numeric : 0
  }
  const numeric = Number(match[1])
  const unit = match[2] || ''
  return numeric * (unitMap[unit] ?? 1)
}

const parseModelParameterCount = (value) => {
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value <= 0) return 0
    return value > 1000000 ? value : value * 1e9
  }

  const text = String(value || '').trim().toLowerCase().replace(/,/g, '')
  if (!text) return 0
  const unitMap = { t: 1e12, b: 1e9, m: 1e6, k: 1e3 }
  const mixedMatch = text.match(/(\d+(?:\.\d+)?)\s*x\s*(\d+(?:\.\d+)?)\s*([tbmk])/)
  if (mixedMatch) {
    return Number(mixedMatch[1]) * Number(mixedMatch[2]) * unitMap[mixedMatch[3]]
  }
  const directMatch = text.match(/(\d+(?:\.\d+)?)\s*([tbmk])/)
  if (directMatch) {
    return Number(directMatch[1]) * unitMap[directMatch[2]]
  }
  const numeric = Number(text.replace(/[^\d.]/g, ''))
  if (!Number.isFinite(numeric) || numeric <= 0) return 0
  return numeric > 1000000 ? numeric : numeric * 1e9
}

const pickFirstParameterValue = (keys) => {
  for (const key of keys) {
    const value = deployForm.parameters?.[key]
    if (!isEmptyValue(value)) {
      return value
    }
  }
  return undefined
}

const resolveModelPrecisionBytes = (model) => {
  const precisionHint = [
    pickFirstParameterValue(['quantization', 'load_format', 'dtype']),
    model?.quantization,
    model?.format
  ].filter(Boolean).join(' ').toLowerCase()

  if (/(awq|gptq|nf4|int4|4bit|q4)/.test(precisionHint)) return 0.5
  if (/(int5|5bit|q5)/.test(precisionHint)) return 0.625
  if (/(int6|6bit|q6)/.test(precisionHint)) return 0.75
  if (/(fp8|int8|8bit|q8)/.test(precisionHint)) return 1
  if (/(fp32|float32|32bit)/.test(precisionHint)) return 4
  return 2
}

const resolveKvPrecisionBytes = (model) => {
  const precisionHint = [
    pickFirstParameterValue(['kv_cache_dtype', 'kv-cache-dtype', 'cache_dtype', 'cache-dtype', 'dtype']),
    model?.quantization
  ].filter(Boolean).join(' ').toLowerCase()

  if (/(fp8|int8|8bit)/.test(precisionHint)) return 1
  if (/(fp32|float32|32bit)/.test(precisionHint)) return 4
  return 2
}

const resolveEstimateContextLength = (fallback) => {
  const value = pickFirstParameterValue([
    'max_model_len',
    'max-model-len',
    'context_length',
    'context-length',
    'context_len',
    'max_context_length',
    'max_seq_len',
    'max-seq-len'
  ]) ?? fallback
  return Math.max(0, Math.round(parseScaledNumber(value, { k: 1024, m: 1024 ** 2 })))
}

const resolveEstimateMaxSequences = () => {
  const value = pickFirstParameterValue([
    'max_num_seqs',
    'max-num-seqs',
    'max_num_sequences',
    'num_seqs',
    'batch_size',
    'max_batch_size',
    'max-batch-size'
  ])
  const numeric = Math.round(parseScaledNumber(value))
  return Math.max(numeric || 1, 1)
}

const resolveEstimateGpuUtilization = () => {
  const value = pickFirstParameterValue([
    'gpu_memory_utilization',
    'gpu-memory-utilization',
    'gpuMemoryUtilization'
  ])
  const numeric = parseScaledNumber(value)
  if (!numeric) return 1
  if (numeric > 1) {
    return Math.min(Math.max(numeric / 100, 0.1), 1)
  }
  return Math.min(Math.max(numeric, 0.1), 1)
}

const resolveEstimateCpuOffloadGb = () => {
  const value = pickFirstParameterValue([
    'cpu_offload_gb',
    'cpu-offload-gb',
    'cpuOffloadGb',
    'cpu_offload',
    'cpu-offload'
  ])
  return Math.max(parseScaledNumber(value, { g: 1, gb: 1 }), 0)
}

const resolveKvBytesPerToken = (parameterCount, architecture, kvPrecisionBytes) => {
  const parameterBillions = parameterCount / 1e9
  const architectureHint = String(architecture || '').toLowerCase()
  let factorMbPerTokenPerBillion = 0.012

  if (/(qwen|mistral|mixtral|gemma|deepseek|phi|internlm|yi)/.test(architectureHint)) {
    factorMbPerTokenPerBillion = 0.008
  } else if (/(llama3|llama-3|llama 3)/.test(architectureHint)) {
    factorMbPerTokenPerBillion = 0.016
  } else if (/(llama|baichuan|falcon|bloom)/.test(architectureHint)) {
    factorMbPerTokenPerBillion = 0.03
  }

  return parameterBillions * factorMbPerTokenPerBillion * 1024 * 1024 * (kvPrecisionBytes / 2)
}

const resolveRuntimeReserveBytes = (weightsBytes, kvCacheBytes, gpuCount) => {
  const baselineBytes = weightsBytes + kvCacheBytes
  const reserveByLoad = baselineBytes * 0.08
  const reserveByGpuCount = gpuCount * 1.5 * 1024 * 1024 * 1024
  return Math.max(reserveByLoad, reserveByGpuCount, 1.5 * 1024 * 1024 * 1024)
}

const normalizeGpuDevices = (devices) => {
  if (!Array.isArray(devices)) return []
  return devices
    .map((device, index) => {
      const resolvedIndex = Number.isFinite(Number(device?.index)) ? Number(device.index) : index
      const memoryBytes = Number(device?.memoryBytes || 0)
      const name = device?.model || device?.name || device?.vendor || `GPU ${resolvedIndex}`
      return {
        ...device,
        index: resolvedIndex,
        memoryBytes,
        uuid: device?.uuid || device?.deviceUUID || '',
        deviceKey: String(device?.uuid || device?.id || `${resolvedIndex}-${name}`),
        label: `#${resolvedIndex} · ${name}${memoryBytes > 0 ? ` · ${formatEstimateGigabytes(bytesToGigabytes(memoryBytes))}` : ''}`
      }
    })
    .filter(device => device.memoryBytes > 0 || device.uuid || Number.isFinite(device.index))
}

const getTemplateStatusType = (status) => {
  if (status === 'published' || status === 'active') return 'success'
  if (status === 'draft') return 'warning'
  if (status === 'archived') return 'info'
  return 'info'
}

const formatDateTime = (value) => {
  if (!value) return '暂无时间信息'
  return new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString('zh-CN')
}

const extractArray = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.models)) return payload.models
  return []
}

const normalizeLocalModels = (payload) => extractArray(payload)
  .map((item, index) => {
    const metadata = parseJSONSafely(item.metadata)
    const tags = parseArraySafely(item.tags)
    const catalogId = item.id ?? item.model_id ?? item.catalog_id ?? item.slug ?? `${item.name || 'model'}-${index}`
    const modelIdentifier = item.source_model_id || item.model_identifier || item.identifier || item.model || item.model_name || item.name || ''
    return {
      ...item,
      metadata,
      tags,
      catalogId,
      catalogKey: String(catalogId),
      name: item.display_name || item.name || item.model_name || `模型 ${index + 1}`,
      modelIdentifier,
      sourceModelId: item.source_model_id || modelIdentifier,
      path: item.local_path || item.model_path || item.path || item.file_path || '',
      source: item.source || item.provider || item.origin || 'local',
      summary: item.summary || metadata?.Description || metadata?.description || metadata?.pipeline_tag || '',
      license: item.license || metadata?.License || metadata?.license || metadata?.cardData?.license || '',
      pipelineTag: metadata?.pipeline_tag || metadata?.Task || metadata?.task || metadata?.cardData?.pipeline_tag || '',
      downloads: item.downloads || metadata?.Downloads || metadata?.downloads || 0,
      likes: item.likes || metadata?.likes || 0,
      languages: parseArraySafely(metadata?.Language || metadata?.language || metadata?.cardData?.language),
      baseModel: firstArrayValue(parseArraySafely(metadata?.BaseModel || metadata?.base_model || metadata?.cardData?.base_model)),
      format: item.format || metadata?.format || metadata?.library_name || item.model_format || item.runtime_format || '',
      quantization: item.quantization || metadata?.quantization || item.quant || item.precision || '',
      architecture: item.architecture || firstArrayValue(metadata?.Architectures) || firstArrayValue(metadata?.architectures) || item.family || item.engine || '',
      parameterSize: item.parameter_size || metadata?.parameter_size || metadata?.model_size || metadata?.ModelInfos?.safetensor?.model_size || metadata?.modelInfos?.safetensor?.model_size || metadata?.model_infos?.safetensor?.model_size || item.size_label || item.model_size || '',
      contextLength: item.context_length || metadata?.context_length || metadata?.context_window || item.context_window || item.max_context_length || item.max_tokens || '',
      importedAt: item.imported_at || item.updated_at || item.created_at || '',
      imported: item.is_imported ?? item.imported ?? Boolean(item.id || item.created_at || item.updated_at || item.imported_at)
    }
  })
  .sort((left, right) => {
    if (Number(right.imported) !== Number(left.imported)) {
      return Number(right.imported) - Number(left.imported)
    }
    const leftTime = new Date(left.importedAt || 0).getTime()
    const rightTime = new Date(right.importedAt || 0).getTime()
    return rightTime - leftTime || String(left.name).localeCompare(String(right.name), 'zh-CN')
  })

const normalizeParameterFields = (version) => extractCanonicalParameters(version)
  .map((field, index) => {
    const key = field.name || `param_${index}`
    const rawType = field.type || 'text'
    const normalizedType = normalizeFieldType(rawType)
    return {
      key,
      label: field.label || key,
      description: field.description || '',
      extraTip: field.extra_tip || '',
      placeholder: '',
      required: Boolean(field.required),
      defaultValue: normalizeFieldDefaultValue(normalizedType, field.default_value),
      type: normalizedType,
      options: normalizeFieldOptions(field.option_values),
      min: field.min,
      max: field.max,
      step: field.step ?? 1,
      rows: field.rows || (normalizedType === 'json' ? 6 : 4),
      fullWidth: Boolean(field.full_width || normalizedType === 'textarea' || normalizedType === 'json'),
      mutable: field.mutable !== false,
      advanced: field.advanced === true
    }
  })
  .filter(field => field.key)

const normalizeFieldType = (type) => {
  const normalized = String(type || '').toLowerCase()
  if (['textarea', 'multiline'].includes(normalized)) return 'textarea'
  if (['password', 'secret'].includes(normalized)) return 'password'
  if (['number', 'integer', 'float'].includes(normalized)) return 'number'
  if (['boolean', 'switch', 'toggle'].includes(normalized)) return 'switch'
  if (['select', 'enum', 'dropdown'].includes(normalized)) return 'select'
  if (['json', 'object', 'map'].includes(normalized)) return 'json'
  return 'text'
}

const normalizeFieldOptions = (options) => {
  if (typeof options === 'string') {
    try {
      options = JSON.parse(options)
    } catch {
      return []
    }
  }
  if (!Array.isArray(options)) return []
  return options.map(option => {
    if (typeof option === 'object') {
      return {
        label: option.label || option.name || option.title || String(option.value ?? option.code ?? option.key ?? ''),
        value: option.value ?? option.code ?? option.key ?? option.name
      }
    }
    return { label: String(option), value: option }
  })
}

const normalizeFieldDefaultValue = (type, value) => {
  if (value === undefined || value === null || value === '') return value
  if (type === 'number') {
    const numeric = Number(value)
    return Number.isFinite(numeric) ? numeric : value
  }
  if (type === 'switch') {
    if (typeof value === 'boolean') return value
    return String(value).toLowerCase() === 'true'
  }
  if (type === 'json' && typeof value === 'string') {
    return value
  }
  return value
}

const buildFallbackParameterValues = (templateName = '') => ({
  app_name: templateName,
  image_name: '',
  image_tag: 'latest'
})

const buildFallbackParameterFields = (templateName = '') => {
  const defaults = buildFallbackParameterValues(templateName)
  return [
    {
      key: 'app_name',
      label: '应用名称',
      description: '可选，用于 VM Docker 容器名等部署标识。',
      placeholder: '请输入应用名称',
      required: false,
      defaultValue: defaults.app_name,
      type: 'text',
      options: [],
      rows: 4,
      fullWidth: false
    },
    {
      key: 'image_name',
      label: '镜像名称',
      description: '如 nginx、vllm/vllm-openai。',
      placeholder: '请输入镜像名称',
      required: false,
      defaultValue: defaults.image_name,
      type: 'text',
      options: [],
      rows: 4,
      fullWidth: false
    },
    {
      key: 'image_tag',
      label: '镜像标签',
      description: '未填写时通常使用 latest。',
      placeholder: '如 latest',
      required: false,
      defaultValue: defaults.image_tag,
      type: 'text',
      options: [],
      rows: 4,
      fullWidth: false
    }
  ]
}

const buildParameterValues = (fields, currentValues, templateName = '') => {
  const defaults = buildFallbackParameterValues(templateName)
  const next = {}

  fields.forEach(field => {
    const currentValue = currentValues?.[field.key]
    if (!isEmptyValue(currentValue)) {
      next[field.key] = currentValue
      return
    }

    if (field.defaultValue !== undefined) {
      next[field.key] = cloneValue(field.defaultValue)
      return
    }

    if (defaults[field.key] !== undefined) {
      next[field.key] = cloneValue(defaults[field.key])
      return
    }

    if (field.type === 'switch') {
      next[field.key] = false
      return
    }

    next[field.key] = field.type === 'number' ? undefined : ''
  })

  return next
}

const isEmptyValue = (value) => {
  if (value === undefined || value === null) return true
  if (typeof value === 'string') return value.trim() === ''
  return false
}

const cloneValue = (value) => {
  if (Array.isArray(value)) return [...value]
  if (value && typeof value === 'object') return { ...value }
  return value
}

const parseJSONSafely = (value) => {
  if (!value) return {}
  if (typeof value === 'object') return value
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch {
      return {}
    }
  }
  return {}
}

const parseArraySafely = (value) => {
  if (Array.isArray(value)) return value
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value)
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }
  return []
}

const firstArrayValue = (value) => Array.isArray(value) && value.length > 0 ? value[0] : ''

const syncSelectedGpuDevices = () => {
  if (!canSelectGpuDevices.value) {
    if (selectedGpuDeviceKeys.value.length > 0) {
      selectedGpuDeviceKeys.value = []
    }
    return
  }

  const availableKeys = new Set(selectedResourceGpuDevices.value.map(device => device.deviceKey))
  const retainedKeys = selectedGpuDeviceKeys.value.filter(key => availableKeys.has(key))
  selectedGpuDeviceKeys.value = retainedKeys.length > 0 ? retainedKeys : selectedResourceGpuDevices.value.map(device => device.deviceKey)
}

watch(selectedResourceGpuDevices, () => {
  syncSelectedGpuDevices()
  syncSelectedGpuIntoParameters()
}, { immediate: true })

watch(() => selectedResourceType.value, () => {
  syncSelectedGpuDevices()
  syncSelectedGpuIntoParameters()
})

watch(() => selectedGpuDeviceKeys.value.join(','), () => {
  syncSelectedGpuIntoParameters()
})

watch(
  () => [route.query.target_resource_id, route.query.namespace, route.query.source].join('|'),
  () => {
    syncScopedStoreFilters()
    if (deployDialogVisible.value) {
      applyExternalDeployContext()
    }
  },
  { immediate: true }
)

onMounted(loadData)
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.store-page {
  display: flex;
  flex-direction: column;
  gap: $space-5;
  animation: float-up 0.45s ease both;
}

.resource-scope-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: $space-4;
  padding: $space-4 $space-5;
  border-radius: $radius-xl;
  border: 1px solid rgba($primary-color, 0.18);
  background: linear-gradient(140deg, rgba($primary-color, 0.12), rgba($primary-color, 0.04));
  box-shadow: var(--shadow-sm);
}

.resource-scope-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.resource-scope-value {
  display: inline-block;
  margin-top: $space-1;
  margin-right: $space-2;
  color: var(--text-primary);
  font-size: 20px;
}

.resource-scope-hint {
  color: var(--text-secondary);
}

.store-header-card,
.card-shell {
  border-radius: $radius-xl;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-md);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
}

.store-header-card {
  padding: 22px 22px 18px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: $space-4;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: $space-3;
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
  margin-top: $space-2;
  color: var(--text-secondary);
}

.store-kind-tabs {
  margin-top: 18px;
}

.catalog-overview {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 14px;
  margin-top: $space-3;
}

.overview-card {
  display: flex;
  flex-direction: column;
  gap: $space-2;
  padding: 18px 20px;
  border-radius: $radius-lg;
  background: var(--bg-elevated);
  border: 1px solid var(--border-color-light);
  box-shadow: var(--shadow-sm);

  &.accent-success {
    background: linear-gradient(140deg, rgba($success-color, 0.12), rgba($success-color, 0.04));
  }

  &.accent-primary {
    background: linear-gradient(140deg, rgba($primary-color, 0.14), rgba($primary-color, 0.05));
  }
}

.overview-label {
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}

.overview-value {
  font-family: $font-family-display;
  font-size: 32px;
  line-height: 1;
  color: var(--text-primary);
}

.overview-hint {
  color: var(--text-muted);
  font-size: 12px;
}

.page-filters {
  display: flex;
  gap: $space-3;
  flex-wrap: wrap;
  padding: 16px 18px;
}

.catalog-section {
  padding: 18px;
}

.catalog-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: $space-4;
  margin-bottom: $space-4;
}

.catalog-title {
  margin: 0;
  font-family: $font-family-display;
  font-size: 22px;
  font-weight: 720;
  color: var(--text-primary);
}

.catalog-description {
  margin-top: $space-2;
  color: var(--text-secondary);
}

.model-list-table :deep(.el-table__cell) {
  vertical-align: top;
}

.model-row-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.model-row-title {
  color: var(--text-primary);
  font-size: 15px;
  font-weight: 700;
}

.model-row-subtitle {
  color: var(--text-secondary);
  font-size: 12px;
  word-break: break-all;
}

.model-row-summary {
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.45;
}

.model-row-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
  color: var(--text-secondary);
  font-size: 12px;
}

.model-row-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.model-card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: $space-3;
  margin-top: auto;
}

.model-timestamp {
  color: var(--text-muted);
  font-size: 12px;
}

.deploy-dialog-shell {
  display: flex;
  flex-direction: column;
  gap: $space-4;
}

.deploy-vram-panel {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-secondary);
  overflow: hidden;

  &.status-success {
    border-color: rgba($success-color, 0.28);
    background: linear-gradient(140deg, rgba($success-color, 0.12), rgba(255, 255, 255, 0.8));
  }

  &.status-warning {
    border-color: rgba($warning-color, 0.3);
    background: linear-gradient(140deg, rgba($warning-color, 0.16), rgba(255, 255, 255, 0.8));
  }

  &.status-danger {
    border-color: rgba($danger-color, 0.28);
    background: linear-gradient(140deg, rgba($danger-color, 0.14), rgba(255, 255, 255, 0.8));
  }
}

.deploy-vram-cell {
  display: flex;
  flex-direction: column;
  gap: $space-1;
  padding: $space-4;
  min-width: 0;

  &:not(:last-child) {
    border-right: 1px solid var(--border-color-light);
  }

  &.emphasis {
    background: rgba(255, 255, 255, 0.28);
  }
}

.deploy-vram-label {
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
}

.deploy-vram-value {
  color: var(--text-primary);
  font-family: $font-family-display;
  font-size: 20px;
  font-weight: 700;
  line-height: 1.1;
}

.deploy-vram-detail {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.45;
  word-break: break-word;
}

.deploy-form {
  display: flex;
  flex-direction: column;
  gap: $space-2;
}

.deploy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 12px;
}

.parameter-panel {
  padding: 14px 16px;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-secondary);
}

.parameter-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: $space-4;
  margin-bottom: 10px;
}

.parameter-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.parameter-description {
  margin-top: 2px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.45;
}

.parameter-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
}

.advanced-parameter-collapse {
  margin-top: $space-4;
}

.parameter-field {
  &.full {
    grid-column: 1 / -1;
  }
}

.parameter-label {
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  font-size: 13px;
  line-height: 1.35;
}

.parameter-help-icon {
  color: var(--text-muted);
  cursor: help;
}

.parameter-recommendation {
  color: var(--text-muted);
}

.parameter-number {
  width: 100%;
}

:deep(.parameter-number .el-input__wrapper) {
  width: 100%;
}

:deep(.deploy-form .el-form-item) {
  margin-bottom: 12px;
}

:deep(.deploy-form .el-form-item__label) {
  padding-bottom: 4px;
  line-height: 1.35;
}

:deep(.deploy-form .el-input__wrapper),
:deep(.deploy-form .el-textarea__inner),
:deep(.deploy-form .el-select__wrapper),
:deep(.deploy-form .el-input-number) {
  min-height: 34px;
}

:deep(.advanced-parameter-collapse .el-collapse-item__header) {
  min-height: 40px;
  font-size: 13px;
}

@media (max-width: 900px) {
  .page-header,
  .resource-scope-card,
  .catalog-header,
  .parameter-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .deploy-vram-panel {
    grid-template-columns: 1fr;
  }

  .deploy-vram-cell:not(:last-child) {
    border-right: none;
    border-bottom: 1px solid var(--border-color-light);
  }

  .deploy-grid,
  .parameter-grid,
  .advanced-grid {
    grid-template-columns: 1fr;
  }
}
</style>
