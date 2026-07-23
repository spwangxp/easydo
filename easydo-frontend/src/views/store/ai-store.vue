<template>
  <div class="ai-store-page">
    <div class="content-toolbar store-page-toolbar">
      <div class="content-toolbar__start">
        <StoreKindSwitch :model-value="storeKind" @update:model-value="handleStoreTabChange" />
      </div>
      <div class="content-toolbar__meta store-page-hint">模型、Provider 与 Deployment 一体查看，Agent Profile 请在 AI Agent 商店管理。</div>
      <div class="content-toolbar__actions">
        <StoreHeaderActions>
          <el-input v-model="filters.keyword" clearable placeholder="搜索模型 / Provider / Deployment" style="width: 280px" />
          <el-button type="primary" @click="openImportModelDialog">导入模型</el-button>
          <el-button type="primary" @click="openDeployDialog">部署模型</el-button>
          <el-button v-if="canManageAIProviders" type="primary" @click="openProviderDialog()">接入外部 Provider</el-button>
        </StoreHeaderActions>
      </div>
    </div>

    <section class="card-shell section-shell">
      <div class="section-header">
        <div>
          <h2>{{ supplyView === 'models' ? '模型视角' : 'Provider 视角' }}</h2>
          <p>{{ supplyView === 'models' ? '按模型聚合展示 Provider 与部署。' : '按 Provider 查看当前供应商提供哪些模型，并支持重新发现模型。' }}</p>
        </div>
        <el-radio-group v-model="supplyView" size="small" class="supply-view-switch">
          <el-radio-button value="models">模型视角</el-radio-button>
          <el-radio-button value="providers">Provider 视角</el-radio-button>
        </el-radio-group>
      </div>

      <el-table
        v-if="supplyView === 'models'"
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
                  <el-table-column label="上下文长度" width="140">
                    <template #default="{ row: provider }">{{ provider.context_window_label || '-' }}</template>
                  </el-table-column>
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

            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="模型名" min-width="220" />
        <el-table-column prop="parameterSize" label="参数大小" min-width="120" />
        <el-table-column prop="modalitiesText" label="模态" min-width="160" />
        <el-table-column prop="source" label="来源" min-width="120" />
        <el-table-column label="上下文长度" min-width="140">
          <template #default="{ row }">{{ row.contextWindowLabel || row.context_window_label || '-' }}</template>
        </el-table-column>
        <el-table-column prop="deploymentCount" label="已部署数" width="120" />
        <el-table-column prop="providerCount" label="Provider 数" width="120" />
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

      <el-table
        v-else
        v-loading="loading"
        :data="providerRows"
        row-key="id"
        empty-text="暂无 AI Provider"
      >
        <el-table-column type="expand" width="56">
          <template #default="{ row }">
            <div class="expand-panel">
              <div class="detail-block">
                <div class="detail-block-header">
                  <strong>模型供给关系</strong>
                  <span class="detail-count">{{ providerBindingRows(row).length }}</span>
                </div>
                <el-table :data="providerBindingRows(row)" size="small" empty-text="暂无模型供给">
                  <el-table-column prop="modelName" label="模型" min-width="180" />
                  <el-table-column prop="provider_model_key" label="Provider Model Key" min-width="220" />
                  <el-table-column prop="status" label="状态" width="120" />
                  <el-table-column label="上下文长度" width="140">
                    <template #default="{ row: binding }">{{ binding.context_window_label || '-' }}</template>
                  </el-table-column>
                  <el-table-column label="输出上限" width="120">
                    <template #default="{ row: binding }">{{ binding.max_output_tokens || '-' }}</template>
                  </el-table-column>
                  <el-table-column label="能力" min-width="180">
                    <template #default="{ row: binding }">
                      <div class="tag-list">
                        <el-tag v-for="item in binding.capabilities" :key="item" size="small" effect="plain">{{ item }}</el-tag>
                        <span v-if="binding.capabilities.length === 0">-</span>
                      </div>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="Provider 名称" min-width="200" />
        <el-table-column prop="provider_type" label="类型" min-width="150" />
        <el-table-column prop="base_url" label="Base URL" min-width="260" />
        <el-table-column prop="bindingCount" label="模型数" width="100" />
        <el-table-column prop="status" label="状态" width="120" />
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button link type="primary" @click="openProviderDiscoveryDialog(row)">重新发现模型</el-button>
              <el-button link type="primary" @click="openProviderDialog(row)">编辑</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="dialogs.deploy" title="部署模型" width="920px" destroy-on-close>
      <el-form label-position="top">
        <el-steps :active="deployStep" finish-status="success" class="dialog-stepper" simple>
          <el-step title="选择模型资产" />
          <el-step title="模板与资源" />
          <el-step title="参数预览" />
          <el-step title="同步 Provider" />
        </el-steps>

        <div v-show="deployStep === 0" class="dialog-step-body">
          <el-form-item label="模型" required>
            <el-select v-model="deployForm.modelId" filterable style="width: 100%" @change="handleDeployModelChange">
              <el-option v-for="item in aiState.models" :key="item.id" :label="item.name" :value="item.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="Model Asset" required>
            <el-select v-model="deployForm.modelAssetId" style="width: 100%">
              <el-option v-for="item in selectedModelAssetOptions" :key="item.value" :label="item.label" :value="item.value" />
            </el-select>
          </el-form-item>
        </div>

        <div v-show="deployStep === 1" class="dialog-step-body">
          <div class="form-grid form-grid--two">
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
              <el-select v-model="deployForm.targetResourceId" filterable style="width: 100%" @change="handleDeployResourceChange">
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
          </div>
          <el-alert title="选择 GPU 后会自动同步 cuda_visible_devices、gpu_count、gpu_uuids 等隐式部署参数。" type="info" :closable="false" show-icon />
        </div>

        <div v-show="deployStep === 2" class="dialog-step-body">
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
        </div>

        <div v-show="deployStep === 3" class="dialog-step-body">
          <el-descriptions :column="2" border>
            <el-descriptions-item label="模型">{{ selectedDeployModel?.name || '-' }}</el-descriptions-item>
            <el-descriptions-item label="Model Asset">{{ selectedModelAssetOptions.find((item) => item.value === deployForm.modelAssetId)?.label || '-' }}</el-descriptions-item>
            <el-descriptions-item label="模板">{{ selectedDeployTemplate?.name || '-' }}</el-descriptions-item>
            <el-descriptions-item label="模板版本">{{ deployTemplateVersions.find((item) => String(item.id) === String(deployForm.templateVersionId))?.version || '-' }}</el-descriptions-item>
            <el-descriptions-item label="资源">{{ selectedDeployResource?.name || '-' }}</el-descriptions-item>
            <el-descriptions-item label="GPU">{{ selectedGpuDevices.map((item) => item.label).join(' / ') || '-' }}</el-descriptions-item>
          </el-descriptions>
          <el-alert class="dialog-summary-alert" title="部署成功后，系统会由部署同步任务创建或更新 self-deploy Provider 供给关系。" type="success" :closable="false" show-icon />
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.deploy = false">取消</el-button>
        <el-button :disabled="deployStep === 0" @click="deployStep -= 1">上一步</el-button>
        <el-button v-if="deployStep < 3" type="primary" @click="goNextDeployStep">下一步</el-button>
        <el-button v-else type="primary" :loading="deploySubmitting" @click="submitDeploy">开始部署</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialogs.importModel" title="导入模型" width="780px" destroy-on-close>
      <el-form label-position="top">
        <div class="form-grid form-grid--two">
          <el-form-item label="模型来源" required>
            <el-select v-model="importModelForm.source" style="width: 100%">
              <el-option label="Hugging Face" value="huggingface" />
              <el-option label="ModelScope" value="modelscope" />
              <el-option label="私有模型仓库" value="private-repo" />
              <el-option label="手动录入" value="manual" />
            </el-select>
          </el-form-item>
          <el-form-item label="来源模型 ID" required>
            <el-input v-model="importModelForm.sourceModelId" placeholder="如 Qwen/Qwen2.5-7B-Instruct" />
          </el-form-item>
          <el-form-item label="模型名称">
            <el-input v-model="importModelForm.name" placeholder="Qwen2.5-7B-Instruct" />
          </el-form-item>
          <el-form-item label="Display Name">
            <el-input v-model="importModelForm.displayName" placeholder="Qwen2.5 7B Instruct" />
          </el-form-item>
          <el-form-item label="模型类型">
            <el-select v-model="importModelForm.modelKind" style="width: 100%">
              <el-option label="chat" value="chat" />
              <el-option label="embedding" value="embedding" />
              <el-option label="rerank" value="rerank" />
              <el-option label="image" value="image" />
              <el-option label="audio" value="audio" />
              <el-option label="multimodal" value="multimodal" />
            </el-select>
          </el-form-item>
          <el-form-item label="参数规模">
            <el-input v-model="importModelForm.parameterSize" placeholder="7B / 32B / 235B" />
          </el-form-item>
          <el-form-item label="License">
            <el-input v-model="importModelForm.license" placeholder="apache-2.0 / internal" />
          </el-form-item>
          <el-form-item label="标签">
            <el-input v-model="importModelForm.tagsText" placeholder="qwen, chat, tool" />
          </el-form-item>
          <el-form-item label="仓库 / 下载地址">
            <el-input v-model="importModelForm.repositoryUrl" placeholder="https://huggingface.co/Qwen/... 或私有仓库地址" />
          </el-form-item>
          <el-form-item label="Revision">
            <el-input v-model="importModelForm.revision" placeholder="main / commit hash" />
          </el-form-item>
          <el-form-item label="文件格式">
            <el-select v-model="importModelForm.format" clearable style="width: 100%">
              <el-option label="safetensors" value="safetensors" />
              <el-option label="gguf" value="gguf" />
              <el-option label="onnx" value="onnx" />
              <el-option label="pytorch bin" value="pytorch-bin" />
            </el-select>
          </el-form-item>
          <el-form-item label="推荐 Runtime">
            <el-select v-model="importModelForm.recommendedRuntime" clearable style="width: 100%">
              <el-option label="vLLM" value="vllm" />
              <el-option label="SGLang" value="sglang" />
              <el-option label="Ollama" value="ollama" />
              <el-option label="TGI" value="tgi" />
              <el-option label="自定义" value="custom" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item v-if="importModelForm.source === 'private-repo'" label="私有仓库 Credential">
          <CredentialSelector
            v-model="importModelForm.credentialId"
            :credential-types="['TOKEN', 'SSH_KEY', 'PASSWORD']"
            credential-category="custom"
            placeholder="选择访问私有仓库的凭据"
            :create-route-query="{ category: 'custom', type: 'TOKEN', source: 'ai-model-import' }"
          />
        </el-form-item>
        <el-form-item label="摘要">
          <el-input v-model="importModelForm.summary" type="textarea" :rows="3" placeholder="模型用途、来源和部署注意事项" />
        </el-form-item>
        <el-alert title="导入模型只创建模型目录元数据，不会自动创建 Provider 供给关系；外部可调用模型请走接入 Provider。" type="info" :closable="false" show-icon />
      </el-form>
      <template #footer>
        <el-button @click="dialogs.importModel = false">取消</el-button>
        <el-button type="primary" :loading="importSubmitting" @click="submitImportModel">导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialogs.provider" :title="providerDialogTitle" width="1040px" destroy-on-close>
      <el-form label-position="top">
        <el-steps :active="providerDialog.step" finish-status="success" class="dialog-stepper" simple>
          <el-step title="Provider 配置" />
          <el-step title="发现模型" />
          <el-step title="建立供给关系" />
        </el-steps>

        <div v-show="providerDialog.step === 0" class="dialog-step-body">
          <div class="form-grid form-grid--two">
            <el-form-item label="Provider 名称" required>
              <el-input v-model="providerForm.providerName" placeholder="OpenRouter Main" />
            </el-form-item>
            <el-form-item label="Provider 类型" required>
              <el-select v-model="providerForm.providerType" filterable allow-create default-first-option style="width: 100%" @change="applyProviderTemplate">
                <el-option label="OpenRouter" value="openrouter" />
                <el-option label="OpenAI Compatible" value="openai-compatible" />
                <el-option label="OpenAI" value="openai" />
                <el-option label="Azure OpenAI" value="azure-openai" />
                <el-option label="Anthropic" value="anthropic" />
                <el-option label="Ollama" value="ollama" />
                <el-option label="vLLM / SGLang" value="vllm" />
                <el-option label="Custom" value="custom" />
              </el-select>
            </el-form-item>
            <el-form-item label="Base URL" required>
              <el-input v-model="providerForm.endpoint" placeholder="https://openrouter.ai/api/v1" />
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
                <el-option label="disabled" value="disabled" />
              </el-select>
            </el-form-item>
            <el-form-item label="Models Endpoint">
              <div class="endpoint-preview-field">
                <el-input v-model="providerForm.modelsEndpoint" class="endpoint-path-input" placeholder="/models" />
                <span class="endpoint-preview-inline">{{ providerModelsEndpointURL }}</span>
              </div>
            </el-form-item>
            <el-form-item label="LLM Endpoint">
              <div class="endpoint-preview-field">
                <el-input v-model="providerForm.llmEndpoint" class="endpoint-path-input" placeholder="/chat/completions" />
                <span class="endpoint-preview-inline">{{ providerLlmEndpointURL }}</span>
              </div>
            </el-form-item>
          </div>
          <el-alert title="API key 必须通过 Credential 引用，不能写入 Headers JSON 或部署环境变量。" type="info" :closable="false" show-icon />
          <el-collapse class="advanced-collapse">
            <el-collapse-item title="Provider 高级配置">
              <div class="form-grid form-grid--two">
                <el-form-item label="Headers JSON">
                  <el-input v-model="providerForm.headersJSON" type="textarea" :rows="3" placeholder='{"HTTP-Referer":"https://easydo.local"}' />
                </el-form-item>
                <el-form-item label="Settings JSON">
                  <el-input v-model="providerForm.settingsJSON" type="textarea" :rows="3" placeholder='{"timeout":30000,"retry":2}' />
                </el-form-item>
              </div>
            </el-collapse-item>
          </el-collapse>
          <div class="provider-connection-panel">
            <div class="provider-connection-header">
              <div>
                <strong>连接测试</strong>
                <p>使用当前配置调用 Provider Models API，通过后才能继续发现模型。</p>
              </div>
              <div class="provider-connection-actions">
                <el-tag :type="providerConnectionTagType">{{ providerConnectionStatusLabel }}</el-tag>
                <el-button type="primary" :loading="providerConnectionTesting" @click="handleTestProviderConnection">测试连接</el-button>
              </div>
            </div>
            <div class="provider-test-grid">
              <div class="provider-test-card">
                <span>连接</span>
                <strong>{{ providerForm.endpoint || '-' }}</strong>
                <el-tag size="small" effect="plain">{{ providerForm.modelsEndpoint || '/models' }}</el-tag>
              </div>
              <div class="provider-test-card">
                <span>认证</span>
                <strong>Credential #{{ providerForm.credentialId || '-' }}</strong>
                <el-tag type="success" size="small">后端注入密钥</el-tag>
              </div>
              <div class="provider-test-card">
                <span>发现能力</span>
                <strong>{{ providerConnection.modelsCount ?? '-' }} 个模型</strong>
                <el-tag size="small" effect="plain">{{ providerConnection.latencyMs == null ? '-' : `${providerConnection.latencyMs} ms` }}</el-tag>
              </div>
            </div>
            <el-alert
              v-if="providerConnection.message"
              :title="providerConnection.message"
              :type="providerConnectionAlertType"
              :closable="false"
              show-icon
            />
          </div>
        </div>

        <div v-show="providerDialog.step === 1" class="dialog-step-body">
          <div class="provider-discovery-toolbar">
            <div class="provider-discovery-summary">
              <strong>{{ filteredDiscoveredProviderModelRows.length }} 个候选模型</strong>
              <span>全部 {{ discoveredProviderModelRows.length }}，已选择 {{ selectedDiscoveredModelRows.length }}</span>
            </div>
            <div class="provider-discovery-controls">
              <el-input
                v-model="providerDiscoveryFilters.keyword"
                clearable
                placeholder="搜索模型 / Provider Key / 能力"
                class="provider-discovery-search"
              />
              <el-button size="small" :loading="providerDiscovery.loading" @click="loadProviderDiscovery">重新发现</el-button>
              <el-button size="small" @click="toggleAllDiscoveredModels(false)">全不选</el-button>
              <el-button size="small" @click="toggleRecommendedDiscoveredModels">选择推荐模型</el-button>
              <el-tag type="primary" effect="plain">{{ selectedDiscoveredModelRows.length }} / {{ discoveredProviderModelRows.length }}</el-tag>
            </div>
          </div>
          <el-alert v-if="providerDiscovery.error" :title="providerDiscovery.error" type="error" :closable="false" show-icon />
          <el-table
            v-loading="providerDiscovery.loading"
            :data="paginatedDiscoveredProviderModelRows"
            row-key="local_id"
            size="small"
            class="provider-discovery-table"
            empty-text="暂无发现模型"
          >
            <el-table-column width="72" label="导入">
              <template #default="{ row }">
                <el-checkbox v-model="row.selected" />
              </template>
            </el-table-column>
            <el-table-column prop="provider_display_name" label="发现模型" min-width="260">
              <template #default="{ row }">
                <div class="provider-model-cell">
                  <strong>{{ row.provider_display_name }}</strong>
                  <code>{{ row.provider_model_key }}</code>
                  <div class="provider-model-meta">
                    <el-tag size="small" effect="plain">{{ row.model_kind || 'chat' }}</el-tag>
                    <el-tag v-if="row.model_family" size="small" effect="plain">{{ row.model_family }}</el-tag>
                  </div>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="模型归属" min-width="260">
              <template #default="{ row }">
                <el-radio-group v-model="row.target_mode" size="small">
                  <el-radio-button v-for="option in modelTargetModeOptions" :key="option.value" :value="option.value">{{ option.label }}</el-radio-button>
                </el-radio-group>
                <el-select v-if="row.target_mode === 'existing'" v-model="row.model_id" filterable placeholder="选择已有 Model" style="width: 100%; margin-top: 8px">
                  <el-option v-for="item in aiState.models" :key="item.id" :label="item.name" :value="item.id" />
                </el-select>
                <el-input v-else v-model="row.model_name" style="margin-top: 8px" placeholder="新建模型名称" />
              </template>
            </el-table-column>
            <el-table-column label="能力" min-width="220">
              <template #default="{ row }">
                <div class="tag-list provider-capability-list">
                  <el-tag v-for="item in [...row.modalities, ...row.capabilities]" :key="item" size="small" effect="plain">{{ item }}</el-tag>
                  <span v-if="[...row.modalities, ...row.capabilities].length === 0" class="field-tip">-</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="上下文 / 输出" width="180">
              <template #default="{ row }">
                <div class="provider-metric-pair">
                  <el-input-number
                    v-model="row.context_window"
                    :min="0"
                    :step="1024"
                    controls-position="right"
                    size="small"
                    placeholder="Ctx"
                    style="width: 100%"
                  />
                  <span class="field-tip">Ctx {{ formatDiscoveredContextLabel(row.context_window) }} · Out {{ row.max_output_tokens || '-' }}</span>
                </div>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="filteredDiscoveredProviderModelRows.length > 0" class="provider-discovery-pagination">
            <el-pagination
              v-model:current-page="providerDiscoveryFilters.page"
              v-model:page-size="providerDiscoveryFilters.pageSize"
              :page-sizes="providerDiscoveryPageSizeOptions"
              :total="filteredDiscoveredProviderModelRows.length"
              background
              small
              layout="total, sizes, prev, pager, next"
            />
          </div>
        </div>

        <div v-show="providerDialog.step === 2" class="dialog-step-body">
          <el-alert :title="selectedDiscoveredModelRows.length ? `将保存 ${selectedDiscoveredModelRows.length} 个模型供给关系` : '将保存 Provider，0 个模型供给'" type="success" :closable="false" show-icon />
          <el-table :data="selectedDiscoveredModelRows" size="small" empty-text="未选择任何发现模型">
            <el-table-column prop="provider_display_name" label="模型" min-width="180" />
            <el-table-column prop="provider_model_key" label="Provider Model Key" min-width="220" />
            <el-table-column label="动作" width="160">
              <template #default="{ row }">{{ row.target_mode === 'existing' ? '关联已有 Model' : '新建 Model' }}</template>
            </el-table-column>
          </el-table>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.provider = false">取消</el-button>
        <el-button :disabled="providerDialog.step === 0" @click="providerDialog.step -= 1">上一步</el-button>
        <el-button v-if="providerDialog.step < 2" type="primary" :disabled="providerNextDisabled" @click="goNextProviderStep">下一步</el-button>
        <el-button v-else type="primary" :loading="providerSubmitting" @click="submitProviderDemo">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import CredentialSelector from '@/views/pipeline/components/CredentialSelector.vue'
import StoreHeaderActions from './components/StoreHeaderActions.vue'
import StoreKindSwitch from './components/StoreKindSwitch.vue'
import StoreParameterFields from './components/StoreParameterFields.vue'
import { createDeploymentRequest } from '@/api/deployment'
import { getResourceDetail, getResourceList, refreshResourceBaseInfo } from '@/api/resource'
import {
  getTemplateList,
  getTemplateVersions,
  importLocalAIModel,
  listAIModels,
  listAIProviders,
  createAIProvider,
  updateAIProvider,
  testAIProviderConnection,
  discoverAIProviderModels,
  createAIModelBinding,
  updateAIModelBinding
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
  buildDiscoveredModelBindingMetadata,
  buildModelBindingCapabilityPayload,
  buildDiscoveredModelImportPayload,
  buildDiscoveredProviderModelRows,
  buildDemoDeploymentRecord,
  buildDemoProviderRecord,
  buildDeployParameterState,
  filterDiscoveredProviderModelRows,
  buildProviderRows,
  buildModelRows,
  getInvalidJsonFieldLabels,
  paginateDiscoveredProviderModelRows,
  normalizeAiStoreState
} from './aiStoreConfig'

const router = useRouter()
const userStore = useUserStore()
const loading = ref(false)
const deploySubmitting = ref(false)
const importSubmitting = ref(false)
const providerSubmitting = ref(false)
const providerConnectionTesting = ref(false)
const filters = reactive({ keyword: '' })
const dialogs = reactive({ deploy: false, provider: false, importModel: false })
const supplyView = ref('models')
const deployStep = ref(0)
const expandedRowId = ref(null)
const deployTemplates = ref([])
const deployTemplateVersions = ref([])
const deployResources = ref([])
const discoveredProviderModelRows = ref([])
const resourceGpuInfoCache = reactive({})
const resourceRefreshTimers = new Map()
const selectedGpuDeviceKeys = ref([])
const RESOURCE_GPU_REFRESH_POLL_MS = 1500
const RESOURCE_GPU_REFRESH_TIMEOUT_MS = 15000
const DEPLOY_VRAM_IDLE_MESSAGE = '先选择目标资源以继续核对 GPU 容量；模型侧显存需求已先行估算。'

const aiState = reactive({
  models: [],
  providers: [],
  deployments: []
})

const localDemoState = reactive({
  providers: [],
  deployments: []
})

const deployForm = reactive(createDeployForm())
const providerForm = reactive(createProviderForm())
const importModelForm = reactive(createImportModelForm())
const providerDialog = reactive({ step: 0, mode: 'create', providerId: null })
const providerConnection = reactive({ status: 'idle', message: '', endpoint: '', modelsCount: null, latencyMs: null })
const providerDiscovery = reactive({ loading: false, loaded: false, error: '' })
const providerDiscoveryFilters = reactive({ keyword: '', page: 1, pageSize: 10 })
const providerDiscoveryPageSizeOptions = [10, 20, 50]
const modelTargetModeOptions = [
  { label: '关联已有 Model', value: 'existing' },
  { label: '新建 Model', value: 'create' }
]

const mergedState = computed(() => normalizeAiStoreState({
  models: aiState.models,
  providers: [...aiState.providers, ...localDemoState.providers],
  deployments: [...aiState.deployments, ...localDemoState.deployments]
}))

const modelRows = computed(() => buildModelRows({
  models: mergedState.value.models,
  providers: mergedState.value.providers,
  deployments: mergedState.value.deployments,
  keyword: filters.keyword
}))
const providerRows = computed(() => buildProviderRows({
  providers: mergedState.value.providers,
  keyword: filters.keyword
}))

const expandedRowKeys = computed(() => (expandedRowId.value ? [expandedRowId.value] : []))
const selectedDeployModel = computed(() => modelRows.value.find((item) => String(item.id) === String(deployForm.modelId)) || null)
const selectedDeployTemplate = computed(() => deployTemplates.value.find((item) => String(item.id) === String(deployForm.templateId)) || null)
const selectedModelAssetOptions = computed(() => buildSelectedModelAssetOptions(selectedDeployModel.value))
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
const filteredDiscoveredProviderModelRows = computed(() => filterDiscoveredProviderModelRows({
  rows: discoveredProviderModelRows.value,
  keyword: providerDiscoveryFilters.keyword
}))
const paginatedDiscoveredProviderModelRows = computed(() => paginateDiscoveredProviderModelRows({
  rows: filteredDiscoveredProviderModelRows.value,
  page: providerDiscoveryFilters.page,
  pageSize: providerDiscoveryFilters.pageSize
}))
const selectedDiscoveredModelRows = computed(() => discoveredProviderModelRows.value.filter((row) => row.selected))
const providerDialogTitle = computed(() => providerDialog.mode === 'rediscover' ? '重新发现 Provider 模型' : '接入外部 Provider')
const providerConnectionStatusLabel = computed(() => {
  if (providerConnection.status === 'success') return '连接通过'
  if (providerConnection.status === 'failed') return '连接失败'
  if (providerConnection.status === 'testing') return '测试中'
  return '未测试'
})
const providerConnectionTagType = computed(() => {
  if (providerConnection.status === 'success') return 'success'
  if (providerConnection.status === 'failed') return 'danger'
  if (providerConnection.status === 'testing') return 'warning'
  return 'info'
})
const providerConnectionAlertType = computed(() => providerConnection.status === 'success' ? 'success' : 'error')
const providerNextDisabled = computed(() => {
  if (providerDialog.step === 0) {
    return providerConnectionTesting.value || providerConnection.status !== 'success'
  }
  if (providerDialog.step === 1) return providerDiscovery.loading || !providerDiscovery.loaded
  return false
})
const canManageAIProviders = computed(() => {
  return Boolean(userStore.currentWorkspaceId) && String(userStore.currentWorkspaceRole || '').toLowerCase() !== 'viewer'
})
const providerModelsEndpointURL = computed(() => resolveProviderEndpointURL(providerForm.endpoint, providerForm.modelsEndpoint))
const providerLlmEndpointURL = computed(() => resolveProviderEndpointURL(providerForm.endpoint, providerForm.llmEndpoint))
const providerConfigSignature = computed(() => JSON.stringify({
  providerName: providerForm.providerName,
  providerType: providerForm.providerType,
  endpoint: providerForm.endpoint,
  credentialId: providerForm.credentialId,
  modelsEndpoint: providerForm.modelsEndpoint,
  llmEndpoint: providerForm.llmEndpoint,
  headersJSON: providerForm.headersJSON,
  settingsJSON: providerForm.settingsJSON
}))

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
  handleDeployResourceChange(resourceId)
})

watch(selectedGpuDeviceKeys, () => {
  syncSelectedGpuIntoParameters()
})

watch(providerConfigSignature, () => {
  if (!dialogs.provider) return
  resetProviderConnectionState()
  resetProviderDiscoveryState()
})

watch(() => providerDiscoveryFilters.keyword, () => {
  providerDiscoveryFilters.page = 1
})

watch(
  () => [filteredDiscoveredProviderModelRows.value.length, providerDiscoveryFilters.pageSize],
  ([total, pageSize]) => {
    const maxPage = Math.max(Math.ceil(total / Math.max(Number(pageSize) || 1, 1)), 1)
    if (providerDiscoveryFilters.page > maxPage) {
      providerDiscoveryFilters.page = maxPage
    }
  }
)

async function loadData() {
  loading.value = true
  try {
    const [providerRes, aiModelRes, templateRes, resourceRes] = await Promise.all([
      listAIProviders(),
      listAIModels(),
      getTemplateList({ template_type: 'ai' }),
      getResourceList()
    ])

    const nextState = normalizeAiStoreState({
      models: extractArray(aiModelRes?.data),
      providers: extractArray(providerRes?.data),
      deployments: []
    })

    aiState.models = nextState.models
    aiState.providers = nextState.providers
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
  } else if (name === 'ai-agent') {
    router.push('/store/ai-agents')
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
  deployStep.value = 0
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

function normalizeProviderDialogRow(event) {
  if (!event || typeof event !== 'object') return null
  if (typeof Event !== 'undefined' && event instanceof Event) return null
  if ('target' in event && 'currentTarget' in event && !('id' in event)) return null
  return event
}

function openProviderDialog(row = null) {
  if (!canManageAIProviders.value) {
    ElMessage.warning('当前工作空间角色无权接入外部 Provider')
    return
  }
  const provider = normalizeProviderDialogRow(row)
  Object.assign(providerForm, createProviderForm(provider || {}))
  providerDialog.mode = provider?.id ? 'edit' : 'create'
  providerDialog.providerId = provider?.id || null
  providerDialog.step = 0
  resetProviderConnectionState()
  resetProviderDiscoveryState()
  dialogs.provider = true
}

function openProviderDiscoveryDialog(row) {
  openProviderDialog(row)
  providerDialog.mode = 'rediscover'
  supplyView.value = 'providers'
}

function handleDeployModelChange() {
  deployForm.parameters = buildDeployParameterState({
    fields: deployForm.parameterFields,
    selectedModel: selectedDeployModel.value || {}
  })
  deployForm.modelAssetId = selectedModelAssetOptions.value[0]?.value || null
}

function handleDeployResourceChange(resourceId) {
  syncSelectedGpuDevices()
  syncSelectedGpuIntoParameters()
  if (resourceId) {
    ensureResourceGpuInfo(resourceId)
  }
}

function goNextDeployStep() {
  if (deployStep.value === 0) {
    if (!deployForm.modelId) {
      ElMessage.warning('请选择模型')
      return
    }
    if (!deployForm.modelAssetId) {
      ElMessage.warning('请选择 Model Asset')
      return
    }
  }
  if (deployStep.value === 1 && (!deployForm.templateId || !deployForm.templateVersionId || !deployForm.targetResourceId)) {
    ElMessage.warning('请选择部署模板、模板版本和目标资源')
    return
  }
  if (deployStep.value === 1) {
    handleDeployResourceChange(deployForm.targetResourceId)
  }
  if (deployStep.value === 2 && findMissingDeployRequiredField()) {
    const missingField = findMissingDeployRequiredField()
    ElMessage.warning(`请填写${missingField.label || missingField.name}`)
    return
  }
  deployStep.value = Math.min(deployStep.value + 1, 3)
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

function applyProviderTemplate() {
  const template = providerTemplateDefaults(providerForm.providerType)
  providerForm.endpoint = providerForm.endpoint || template.endpoint
  providerForm.modelsEndpoint = template.modelsEndpoint
  providerForm.llmEndpoint = template.llmEndpoint
}

function resetProviderConnectionState() {
  providerConnectionTesting.value = false
  Object.assign(providerConnection, {
    status: 'idle',
    message: '',
    endpoint: '',
    modelsCount: null,
    latencyMs: null
  })
}

function resetProviderDiscoveryState() {
  Object.assign(providerDiscovery, {
    loading: false,
    loaded: false,
    error: ''
  })
  providerDiscoveryFilters.keyword = ''
  providerDiscoveryFilters.page = 1
  discoveredProviderModelRows.value = []
}

function validateProviderJsonFields() {
  const invalidJsonFields = getInvalidJsonFieldLabels([
    { label: 'Headers JSON', value: providerForm.headersJSON },
    { label: 'Settings JSON', value: providerForm.settingsJSON }
  ])
  if (invalidJsonFields.length > 0) {
    ElMessage.warning(`JSON 格式无效：${invalidJsonFields.join('、')}`)
    return false
  }
  return true
}

function buildProviderPayload() {
  const providerHeaders = providerForm.headersJSON.trim() ? JSON.parse(providerForm.headersJSON) : {}
  const providerSettings = providerForm.settingsJSON.trim() ? JSON.parse(providerForm.settingsJSON) : {}

  return {
    name: providerForm.providerName.trim(),
    provider_type: providerForm.providerType,
    base_url: providerForm.endpoint.trim(),
    credential_id: providerForm.credentialId,
    headers_json: providerHeaders,
    settings_json: {
      ...providerSettings,
      models_endpoint: providerForm.modelsEndpoint,
      llm_endpoint: providerForm.llmEndpoint
    },
    status: providerForm.status
  }
}

async function handleTestProviderConnection() {
  if (!validateProviderBaseForm() || !validateProviderJsonFields()) return
  providerConnectionTesting.value = true
  providerConnection.status = 'testing'
  providerConnection.message = '正在调用 Provider Models API'
  try {
    const response = await testAIProviderConnection(buildProviderPayload())
    const data = response?.data || {}
    providerConnection.status = 'success'
    providerConnection.endpoint = data.endpoint || ''
    providerConnection.modelsCount = data.models_count ?? 0
    providerConnection.latencyMs = data.latency_ms ?? null
    providerConnection.message = `连接通过，发现 ${providerConnection.modelsCount} 个模型候选`
    resetProviderDiscoveryState()
    ElMessage.success('Provider 连接测试通过')
  } catch (error) {
    providerConnection.status = 'failed'
    providerConnection.endpoint = ''
    providerConnection.modelsCount = null
    providerConnection.latencyMs = null
    providerConnection.message = error?.response?.data?.message || error?.message || 'Provider 连接测试失败'
    ElMessage.error(providerConnection.message)
  } finally {
    providerConnectionTesting.value = false
  }
}

async function loadProviderDiscovery() {
  if (providerConnection.status !== 'success') {
    ElMessage.warning('请先完成 Provider 连接测试')
    return
  }
  if (!validateProviderBaseForm() || !validateProviderJsonFields()) return
  providerDiscovery.loading = true
  providerDiscovery.error = ''
  try {
    const response = await discoverAIProviderModels(buildProviderPayload())
    const candidates = extractArray(response?.data?.models)
    discoveredProviderModelRows.value = buildDiscoveredProviderModelRows({
      candidates,
      models: aiState.models
    })
    providerDiscoveryFilters.page = 1
    providerDiscovery.loaded = true
    if (candidates.length === 0) {
      ElMessage.warning('Provider 未返回可导入模型')
    }
  } catch (error) {
    providerDiscovery.error = error?.response?.data?.message || error?.message || '发现 Provider 模型失败'
    ElMessage.error(providerDiscovery.error)
  } finally {
    providerDiscovery.loading = false
  }
}

async function goNextProviderStep() {
  if (providerDialog.step === 0) {
    if (!validateProviderBaseForm() || !validateProviderJsonFields()) return
    if (providerConnection.status !== 'success') {
      ElMessage.warning('请先通过 Provider 连接测试')
      return
    }
    providerDialog.step = 1
    if (!providerDiscovery.loaded) {
      await loadProviderDiscovery()
    }
    return
  }
  if (providerDialog.step === 1) {
    if (!providerDiscovery.loaded) {
      ElMessage.warning('请先完成模型发现')
      return
    }
    const invalidRow = selectedDiscoveredModelRows.value.find((row) => row.target_mode === 'existing' && !row.model_id)
    if (invalidRow) {
      ElMessage.warning(`请选择 ${invalidRow.provider_display_name} 要关联的已有 Model`)
      return
    }
  }
  providerDialog.step = Math.min(providerDialog.step + 1, 2)
}

function toggleAllDiscoveredModels(selected) {
  discoveredProviderModelRows.value.forEach((row) => {
    row.selected = selected
  })
}

function toggleRecommendedDiscoveredModels() {
  discoveredProviderModelRows.value.forEach((row, index) => {
    row.selected = index === 0 || row.provider_model_key.includes('gpt-4.1') || row.provider_model_key.includes('qwen')
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
  if (!deployForm.modelAssetId) {
    ElMessage.warning('请选择 Model Asset')
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

  const missingField = findMissingDeployRequiredField()
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
  if (!validateProviderBaseForm()) return
  if (!validateProviderJsonFields()) return
  if (providerConnection.status !== 'success') {
    providerDialog.step = 0
    ElMessage.warning('保存前必须先通过 Provider 连接测试')
    return
  }

  providerSubmitting.value = true
  try {
    const providerPayload = buildProviderPayload()
    const providerResp = providerDialog.providerId
      ? await updateAIProvider(providerDialog.providerId, providerPayload)
      : await createAIProvider(providerPayload)

    const createdProvider = providerResp?.data
    if (createdProvider?.id) {
      for (const row of selectedDiscoveredModelRows.value) {
        const modelID = await ensureDiscoveredModelCatalog(row)
        const existingBinding = findExistingProviderBinding(createdProvider.id, row.provider_model_key)
        const capability = buildModelBindingCapabilityPayload(row)
        const payload = {
          model_id: modelID,
          provider_model_key: row.provider_model_key.trim(),
          settings_json: {},
          metadata_json: capability.metadata_json,
          context_window_tokens: capability.context_window_tokens,
          max_output_tokens: capability.max_output_tokens,
          supports_tool_use: capability.supports_tool_use,
          supports_streaming: capability.supports_streaming,
          capability_source: capability.capability_source,
          status: providerForm.status === 'disabled' ? 'disabled' : 'active'
        }
        if (existingBinding?.id) {
          await updateAIModelBinding(createdProvider.id, existingBinding.id, payload)
        } else {
          await createAIModelBinding(createdProvider.id, payload)
        }
      }
    }

    dialogs.provider = false
    await loadData()
    if (selectedDiscoveredModelRows.value[0]?.model_id) {
      expandedRowId.value = selectedDiscoveredModelRows.value[0].model_id
    }
    ;(providerResp?.warnings || []).forEach((warning) => ElMessage.warning(warning))
    ElMessage.success(selectedDiscoveredModelRows.value.length ? 'Provider 与模型供给关系已保存' : 'Provider 已保存，0 个模型供给')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '保存 Provider 失败')
  } finally {
    providerSubmitting.value = false
  }
}

async function ensureDiscoveredModelCatalog(row) {
  if (row.target_mode === 'existing') {
    return row.model_id
  }
  const response = await importLocalAIModel(buildDiscoveredModelImportPayload(row))
  const model = response?.data || {}
  row.model_id = model.id
  return model.id
}

function findExistingProviderBinding(providerId, providerModelKey) {
  const provider = mergedState.value.providers.find((item) => String(item.id) === String(providerId))
  return (provider?.bindings || []).find((binding) => String(binding.provider_model_key || binding.binding_key || '') === String(providerModelKey)) || null
}

function providerBindingRows(provider) {
  return (provider?.bindings || []).map((binding) => {
    const model = mergedState.value.models.find((item) => String(item.id) === String(binding.model_id)) || {}
    const metadata = parseMaybeObject(binding.metadata_json)
    const contextWindow = Number(
      binding.context_window_tokens
      || binding.context_window
      || metadata.context_window_tokens
      || metadata.context_window
      || model.context_window
      || 0
    )
    const maxOutput = Number(
      binding.max_output_tokens
      || metadata.max_output_tokens
      || 0
    )
    return {
      ...binding,
      modelName: model.name || metadata.model_name || binding.model_name || `Model #${binding.model_id}`,
      capabilities: normalizeTagList(metadata.capabilities),
      context_window_tokens: contextWindow > 0 ? contextWindow : null,
      context_window_label: formatDiscoveredContextLabel(contextWindow),
      max_output_tokens: maxOutput > 0 ? maxOutput : null
    }
  })
}

function formatDiscoveredContextLabel(value) {
  const number = Number(value)
  if (!Number.isFinite(number) || number <= 0) return '-'
  if (number % 1024 === 0 && number >= 1024) return `${Math.round(number / 1024)}K`
  if (number >= 1000) return `${Math.round(number / 1000)}K`
  return String(Math.round(number))
}

function buildSelectedModelAssetOptions(model) {
  if (!model) return []
  const source = model.source || 'manual'
  const sourceModelID = model.source_model_id || model.sourceModelId || model.name
  return [{
    label: `${source} · ${sourceModelID || model.name}`,
    value: `${model.id}:${source}:${sourceModelID || model.name}`
  }]
}

function providerTemplateDefaults(providerType) {
  const normalized = String(providerType || '').toLowerCase()
  if (normalized === 'openrouter') {
    return {
      endpoint: 'https://openrouter.ai/api/v1',
      modelsEndpoint: '/models',
      llmEndpoint: '/chat/completions'
    }
  }
  if (normalized === 'anthropic') {
    return {
      // Anthropic SDK（Pi 运行时）会在 baseURL 后自行拼接 /v1，因此版本前缀必须放在
      // endpoint 路径里，而不是 base_url。这样真实 Anthropic 与第三方兼容端点（如
      // SenseNova https://token.sensenova.cn）都能正确工作：
      //   test-connection: base + /v1/models
      //   runtime(Pi):     new Anthropic({ baseURL: base }) -> base + /v1/messages
      endpoint: 'https://api.anthropic.com',
      modelsEndpoint: '/v1/models',
      llmEndpoint: '/v1/messages'
    }
  }
  if (normalized === 'ollama') {
    return {
      endpoint: 'http://localhost:11434',
      modelsEndpoint: '/api/tags',
      llmEndpoint: '/api/chat'
    }
  }
  return {
    endpoint: '',
    modelsEndpoint: '/models',
    llmEndpoint: '/chat/completions'
  }
}

function validateProviderBaseForm() {
  if (!providerForm.providerName.trim()) {
    ElMessage.warning('请填写 Provider 名称')
    return false
  }
  if (!providerForm.providerType.trim()) {
    ElMessage.warning('请选择 Provider 类型')
    return false
  }
  if (!providerForm.endpoint.trim()) {
    ElMessage.warning('请填写 Base URL')
    return false
  }
  if (!providerForm.credentialId) {
    ElMessage.warning('请选择 Credential')
    return false
  }
  return true
}

function findMissingDeployRequiredField() {
  return (deployForm.parameterFields || []).find((field) => {
    if (!field.required) return false
    const value = deployForm.parameters?.[field.name]
    return value === undefined || value === null || String(value).trim() === ''
  })
}

function parseMaybeObject(value) {
  if (!value) return {}
  if (typeof value === 'object') return value
  try {
    const parsed = JSON.parse(String(value))
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function normalizeTagList(value) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  if (typeof value === 'string') return value.split(',').map((item) => item.trim()).filter(Boolean)
  return []
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
    modelAssetId: null,
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
    sourceModelId: '',
    name: '',
    displayName: '',
    modelKind: 'chat',
    parameterSize: '',
    license: '',
    tagsText: '',
    repositoryUrl: '',
    revision: 'main',
    format: 'safetensors',
    recommendedRuntime: 'vllm',
    credentialId: null,
    summary: ''
  }
}

function createProviderForm(provider = {}) {
  const providerType = provider.provider_type || provider.type || 'openrouter'
  const defaults = providerTemplateDefaults(providerType)
  const settings = parseMaybeObject(provider.settings_json)
  const headers = parseMaybeObject(provider.headers_json)
  return {
    id: provider.id || null,
    providerName: provider.name || '',
    providerType,
    endpoint: provider.base_url || provider.endpoint || defaults.endpoint,
    credentialId: provider.credential_id || provider.credentialId || null,
    status: provider.status || 'active',
    modelsEndpoint: settings.models_endpoint || defaults.modelsEndpoint,
    llmEndpoint: settings.llm_endpoint || defaults.llmEndpoint,
    headersJSON: Object.keys(headers).length ? JSON.stringify(headers, null, 2) : '',
    settingsJSON: JSON.stringify(stripProviderRuntimeSettings(settings), null, 2)
  }
}

function resolveProviderEndpointURL(baseURL, endpointPath) {
  const base = String(baseURL || '').trim().replace(/\/+$/, '')
  const path = String(endpointPath || '').trim()
  if (!path) return base || '-'
  if (/^https?:\/\//i.test(path)) return path
  if (!base) return path
  return `${base}/${path.replace(/^\/+/, '')}`
}

function stripProviderRuntimeSettings(settings) {
  const {
    models_endpoint: _modelsEndpoint,
    llm_endpoint: _llmEndpoint,
    ...rest
  } = settings || {}
  return rest
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

.store-page-toolbar {
  margin-bottom: 16px;
}

.store-page-hint {
  max-width: 420px;
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

.provider-connection-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 14px;
  padding: 12px;
  border: 1px solid var(--border-color-light);
  border-radius: 12px;
  background: var(--bg-card);
}

.provider-connection-header,
.provider-connection-actions,
.provider-test-grid,
.provider-test-card {
  display: flex;
  gap: 10px;
}

.provider-connection-header {
  justify-content: space-between;
  align-items: flex-start;
}

.provider-connection-actions {
  align-items: center;
  flex-wrap: wrap;
}

.provider-connection-header p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}

.provider-test-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.provider-test-card {
  flex-direction: column;
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--border-color-light);
  border-radius: 10px;
  background: var(--bg-page);
}

.provider-test-card span {
  color: var(--text-secondary);
  font-size: 12px;
}

.provider-test-card strong {
  overflow-wrap: anywhere;
}

.provider-discovery-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
  padding: 12px;
  border: 1px solid var(--border-color-light);
  border-radius: 12px;
  background: var(--bg-page);
}

.provider-discovery-summary {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 180px;
}

.provider-discovery-summary strong {
  color: var(--text-primary);
  font-size: 14px;
}

.provider-discovery-summary span,
.provider-model-cell code,
.provider-metric-pair span {
  color: var(--text-secondary);
  font-size: 12px;
}

.provider-discovery-controls {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.provider-discovery-search {
  width: 280px;
}

.provider-discovery-table {
  border: 1px solid var(--border-color-light);
  border-radius: 10px;
  overflow: hidden;
}

.provider-model-cell {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}

.provider-model-cell code {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 2px 6px;
  border-radius: 6px;
  background: var(--bg-page);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.endpoint-preview-field {
  position: relative;
  width: 100%;
}

.endpoint-path-input :deep(.el-input__wrapper) {
  padding-right: min(430px, 50%);
}

.endpoint-preview-inline {
  position: absolute;
  right: 12px;
  top: 50%;
  z-index: 1;
  max-width: min(410px, 46%);
  color: var(--text-tertiary);
  font-size: 12px;
  line-height: 1.35;
  overflow: hidden;
  overflow-wrap: anywhere;
  pointer-events: none;
  text-align: right;
  transform: translateY(-50%);
  white-space: nowrap;
}

.provider-model-meta,
.provider-capability-list {
  display: flex;
  gap: 5px;
  flex-wrap: wrap;
}

.provider-metric-pair {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.provider-discovery-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
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
  .detail-block-header,
  .provider-connection-header,
  .provider-discovery-toolbar {
    flex-direction: column;
    align-items: stretch;
  }

  .provider-test-grid {
    grid-template-columns: 1fr;
  }

  .provider-discovery-search {
    width: 100%;
  }
}
</style>
