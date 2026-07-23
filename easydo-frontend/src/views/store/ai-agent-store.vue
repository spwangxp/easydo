<template>
  <div class="ai-agent-store-page">
    <div class="ai-agent-layout">
      <main class="agent-page">
        <div class="content-toolbar store-page-toolbar agent-toolbar">
          <div class="content-toolbar__start agent-toolbar-start">
            <StoreKindSwitch :model-value="storeKind" @update:model-value="handleStoreTabChange" />
            <div class="agent-mode-switch" role="tablist" aria-label="AI Agent workspace modes">
              <button
                v-for="item in agentModeItems"
                :key="item.name"
                type="button"
                class="agent-mode-button"
                :class="{ active: activeAgentStoreView === item.name }"
                :aria-selected="activeAgentStoreView === item.name"
                role="tab"
                @click="setAgentStoreView(item.name)"
              >
                {{ item.label }}
              </button>
            </div>
          </div>
          <div class="content-toolbar__actions agent-toolbar-actions">
            <StoreHeaderActions>
              <el-input v-if="activeAgentStoreView !== 'tools'" v-model="keyword" clearable placeholder="搜索 Agent / MCP / Skills" style="width: 260px" />
              <el-button :icon="Refresh" @click="refreshCurrentView">刷新</el-button>
              <el-button v-if="activeAgentStoreView === 'agent'" type="primary" :icon="Plus" @click="openCreateDialog">新建 Agent</el-button>
              <el-button v-else-if="activeAgentStoreView === 'mcp'" type="primary" :icon="Plus" @click="openMcpServerDialog()">新建 MCP Server</el-button>
              <el-button v-else-if="activeAgentStoreView === 'skills'" type="primary" :icon="Plus" @click="openSkillRepoDialog()">添加仓库</el-button>
            </StoreHeaderActions>
          </div>
        </div>

        <section v-show="activeAgentStoreView === 'agent'" class="agent-view active" data-agent-view="agent">
          <div class="agent-workspace agent-workspace--single">
            <section class="agent-panel">
              <div class="agent-panel-header">
                <div>
                  <h1>Agent Profiles</h1>
                  <p>模型、Prompt、MCP Server、Skills、Subagents、Schema、Memory、Confirmation 的发布单元</p>
                </div>
              </div>
              <div class="agent-panel-body">
                <div class="agent-filters">
                  <el-input v-model="keyword" clearable placeholder="搜索名称、模型、上下文标签" />
                  <el-select v-model="profileFilterStatus" clearable placeholder="全部状态">
                    <el-option label="active" value="active" />
                    <el-option label="draft" value="draft" />
                    <el-option label="disabled" value="disabled" />
                    <el-option label="archived" value="archived" />
                  </el-select>
                  <el-button @click="resetProfileFilters">重置</el-button>
                </div>

                <el-table v-loading="loading" :data="filteredProfileRows" row-key="id" empty-text="暂无 Agent Profile">
                  <el-table-column label="名称" min-width="180">
                    <template #default="{ row }">
                      <div class="profile-name-cell">
                        <span>{{ row.name }}</span>
                        <el-tag v-if="isReservedPageAssistantProfile(row)" size="small" type="info" effect="plain">系统保留</el-tag>
                      </div>
                    </template>
                  </el-table-column>
                  <el-table-column label="上下文标签" min-width="200">
                    <template #default="{ row }">{{ profileContextTags(row).join(' / ') || '-' }}</template>
                  </el-table-column>
                  <el-table-column label="模型供给" min-width="180">
                    <template #default="{ row }">{{ modelText(row) }}</template>
                  </el-table-column>
                  <el-table-column prop="status" label="版本" width="120" />
                  <el-table-column label="操作" width="350" fixed="right">
	                    <template #default="{ row }">
	                      <div class="table-actions">
	                        <el-button v-if="canOpenChatboxProfile(row)" link type="success" @click="handleOpenChatbox(row)">打开对话</el-button>
	                        <el-button link type="primary" :disabled="!canMutateProfile(row)" @click="openEditDialog(row)">编辑</el-button>
	                        <el-button link type="primary" :disabled="!canMutateProfile(row)" @click="handleValidate(row)">校验</el-button>
	                        <el-button link type="primary" :disabled="!canMutateProfile(row)" @click="handlePublish(row)">发布</el-button>
	                        <el-button v-if="!isReservedPageAssistantProfile(row)" link type="danger" :disabled="!canMutateProfile(row)" @click="handleDelete(row)">删除</el-button>
	                      </div>
	                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </section>


          </div>
        </section>

        <section
          v-show="activeAgentStoreView === 'tools'"
          class="agent-view"
          :class="{ active: activeAgentStoreView === 'tools' }"
          data-agent-view="tools"
        >
          <div class="agent-workspace agent-workspace--single">
            <section class="agent-panel">
              <div class="agent-panel-header">
                <div>
                  <h1>Tools</h1>
                  <p>EasyDo 内置工作区 Tools 目录。可在 Agent Profile 中按需启用。</p>
                </div>
              </div>
              <div class="agent-panel-body">
                <el-table :data="workspaceToolCatalog" row-key="name" empty-text="暂无内置 Tools">
                  <el-table-column prop="name" label="Tool" min-width="140" />
                  <el-table-column label="类型" width="120">
                    <template #default="{ row }">
                      <el-tag size="small" effect="plain" :type="workspaceToolTypeTag(row.operation_type)">
                        {{ row.operation_type }}
                      </el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column label="风险" width="100">
                    <template #default="{ row }">
                      <el-tag size="small" effect="plain" :type="workspaceToolRiskTag(row.risk)">
                        {{ row.risk }}
                      </el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column prop="description" label="说明" min-width="320" />
                </el-table>
              </div>
            </section>
          </div>
        </section>

        <section
          v-show="activeAgentStoreView === 'mcp' || activeAgentStoreView === 'skills'"
          class="agent-view"
          :class="{ active: activeAgentStoreView === 'mcp' || activeAgentStoreView === 'skills' }"
          :data-agent-view="activeAgentStoreView"
        >
          <div class="agent-workspace agent-workspace--single">
            <section class="agent-panel">
              <div class="agent-panel-header">
                <div>
                  <template v-if="activeAgentStoreView === 'mcp'">
                    <h1>MCP Server</h1>
                    <p>维护 MCP Server 连接配置和工具发现结果。</p>
                  </template>
                </div>
              </div>
              <div class="agent-panel-body">
                    <template v-if="activeAgentStoreView === 'mcp'">
                      <div class="resource-pane-toolbar">
                        <div>
                          <h3>MCP Server</h3>
                          <p>按标准 MCP Server 配置维护连接信息，工具发现结果挂在 Server 下，不作为独立资源。</p>
                        </div>
                      </div>
                      <el-table :data="mcpServerRows" row-key="id" empty-text="暂无 MCP Server">
                        <el-table-column label="名称" min-width="200">
                          <template #default="{ row }">
                            <div class="mcp-server-name-cell">
                              <strong>{{ row.name }}</strong>
                              <el-tag v-if="isBuiltinEasyDoMcpServer(row)" size="small" type="success" effect="plain">内置</el-tag>
                              <span v-if="isBuiltinEasyDoMcpServer(row)">这是每个用户在当前工作空间的内置 easydo MCP Server</span>
                            </div>
                          </template>
                        </el-table-column>
                        <el-table-column prop="resource_key" label="Key" min-width="170" />
                        <el-table-column label="类型" width="120">
                          <template #default="{ row }">{{ mcpServerConfig(row).type || '-' }}</template>
                        </el-table-column>
                        <el-table-column label="连接" min-width="240">
                          <template #default="{ row }">{{ mcpServerEndpointText(row) }}</template>
                        </el-table-column>
                        <el-table-column label="Tools" width="110">
                          <template #default="{ row }">{{ discoveredMcpTools(row).length }}</template>
                        </el-table-column>
                        <el-table-column prop="status" label="状态" width="110" />
                        <el-table-column prop="updated_at" label="更新时间" min-width="170" />
                        <el-table-column label="操作" width="230" fixed="right">
                          <template #default="{ row }">
                            <div class="table-actions">
                              <el-button link type="primary" @click="openMcpServerDialog(row)" :disabled="isBuiltinEasyDoMcpServer(row)">编辑</el-button>
                              <el-button link type="primary" @click="openMcpServerDialog(row, { discover: true })" :disabled="isBuiltinEasyDoMcpServer(row)">发现工具</el-button>
                              <el-button link type="danger" @click="handleDeleteResource(row)" :disabled="isBuiltinEasyDoMcpServer(row)">删除</el-button>
                            </div>
                          </template>
                        </el-table-column>
                      </el-table>
                    </template>

                    <template v-else>
                      <div class="skills-workspace">
                        <section class="resource-subsection">
                          <div class="resource-pane-toolbar">
                            <div>
                              <h3>Skills 仓库</h3>
                              <p>添加仓库后扫描，扫描结果会弹窗展示并支持勾选导入。</p>
                            </div>
                          </div>
                          <el-table :data="skillRepositoryRows" row-key="id" empty-text="暂无 Skills 仓库">
                            <el-table-column prop="name" label="仓库" min-width="190" />
                            <el-table-column label="地址" min-width="260">
                              <template #default="{ row }">{{ skillRepositorySpec(row).url || '-' }}</template>
                            </el-table-column>
                            <el-table-column label="分支 / 目录" min-width="160">
                              <template #default="{ row }">{{ skillRepositorySpec(row).branch || 'main' }} / {{ skillRepositorySpec(row).subdirectory || '.' }}</template>
                            </el-table-column>
                            <el-table-column label="已缓存扫描" width="120">
                              <template #default="{ row }">{{ repositoryDiscoveredSkills(row).length }}</template>
                            </el-table-column>
                            <el-table-column prop="status" label="状态" width="110" />
                            <el-table-column label="操作" width="220" fixed="right">
                              <template #default="{ row }">
                                <div class="table-actions">
                                  <el-button link type="primary" @click="openSkillRepoDialog(row)">编辑</el-button>
                                  <el-button
                                    link
                                    type="primary"
                                    :loading="scanningRepositoryId === row.id"
                                    @click="scanSkillRepository(row)"
                                  >
                                    扫描
                                  </el-button>
                                  <el-button link type="danger" @click="deleteSkillRepository(row)">删除</el-button>
                                </div>
                              </template>
                            </el-table-column>
                          </el-table>
                        </section>

                        <section class="resource-subsection">
                          <div class="resource-pane-toolbar resource-pane-toolbar--compact">
                            <div>
                              <h3>已导入 Skills</h3>
                              <p>这些 Skills 可在 Agent Profile 编辑器中启用或取消启用。</p>
                            </div>
                            <el-input v-model="importedSkillKeyword" clearable placeholder="搜索已导入 Skills" style="width: 240px" />
                          </div>
                          <el-table :data="filteredImportedSkillRows" row-key="id" empty-text="暂无已导入 Skills">
                            <el-table-column prop="name" label="名称" min-width="200" />
                            <el-table-column prop="resource_key" label="Key" min-width="180" />
                            <el-table-column prop="version" label="版本" width="120" />
                            <el-table-column label="来源仓库" min-width="180">
                              <template #default="{ row }">{{ row.spec?.source_repository || '-' }}</template>
                            </el-table-column>
                            <el-table-column prop="status" label="状态" width="110" />
                            <el-table-column label="操作" width="90" fixed="right">
                              <template #default="{ row }">
                                <el-button link type="danger" @click="handleDeleteResource(row)">删除</el-button>
                              </template>
                            </el-table-column>
                          </el-table>
                        </section>
                      </div>
                    </template>
              </div>
            </section>
          </div>
        </section>
      </main>
    </div>

    <el-dialog v-model="skillScanDialog.visible" title="扫描结果" width="900px" class="skill-scan-dialog" destroy-on-close>
      <div class="skill-scan-dialog__body">
        <div class="skill-scan-summary">
          <div>
            <strong>{{ activeSkillRepository?.name || 'Skills 仓库' }}</strong>
            <p>{{ skillRepositorySpec(activeSkillRepository || {}).url || '当前仓库' }}</p>
          </div>
          <div class="resource-pane-actions">
            <el-tag :type="skillScanStatusType" effect="plain">{{ skillScanStatus }}</el-tag>
            <el-tag effect="plain">{{ selectedDiscoveredSkillKeys.length }} 已选择</el-tag>
          </div>
        </div>
        <div class="agent-filters agent-filters--dialog">
          <el-input v-model="skillDiscoveryKeyword" clearable placeholder="搜索 Skill 名称 / 描述 / 目录" />
        </div>
        <div v-if="filteredDiscoveredSkills.length === 0" class="resource-empty">当前仓库没有发现可导入的 Skills。</div>
        <div v-else class="skill-card-grid">
          <label v-for="skill in filteredDiscoveredSkills" :key="skill.key" class="skill-import-card">
            <el-checkbox
              :model-value="selectedDiscoveredSkillKeys.includes(skill.key)"
              :disabled="isSkillImported(skill.key)"
              @change="toggleDiscoveredSkill(skill.key, $event)"
            />
            <span class="skill-import-card__main">
              <strong>{{ skill.name }}</strong>
              <span>{{ skill.description || skill.path || '暂无描述' }}</span>
              <span class="skill-import-card__meta">
                <el-tag size="small" effect="plain">{{ skill.version || 'latest' }}</el-tag>
                <el-tag v-if="isSkillImported(skill.key)" size="small" type="success" effect="plain">已导入</el-tag>
              </span>
            </span>
          </label>
        </div>
      </div>
      <template #footer>
        <div class="skill-scan-dialog__footer">
          <el-button @click="skillScanDialog.visible = false">关闭</el-button>
          <el-button
            type="primary"
            :loading="resourceSubmitting"
            :disabled="selectedDiscoveredSkillKeys.length === 0"
            @click="importSelectedSkills"
          >
            导入已选择
          </el-button>
        </div>
      </template>
    </el-dialog>

    <el-dialog v-model="profileDialog.visible" :title="profileDialog.mode === 'create' ? '新建 Agent Profile' : '编辑 Agent Profile'" width="1080px" destroy-on-close>
      <el-form label-position="top" class="agent-profile-form">
        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>基础信息</h3>
          </div>
          <div class="profile-form-grid profile-form-grid--three">
            <el-form-item label="名称" required>
              <div class="reserved-profile-name-field">
                <el-input v-model="profileForm.name" placeholder="Page Assistant" :disabled="isEditingReservedPageAssistantProfile" />
                <el-tag v-if="isReservedPageAssistantProfile(profileForm)" type="info" effect="plain">系统保留</el-tag>
              </div>
            </el-form-item>
            <el-form-item label="响应模式">
              <el-select v-model="profileForm.response_mode" style="width: 100%">
                <el-option label="text" value="text" />
                <el-option label="json" value="json" />
                <el-option label="schema" value="schema" />
                <el-option label="mixed" value="mixed" />
              </el-select>
            </el-form-item>
            <el-form-item label="状态">
              <el-select v-model="profileForm.status" style="width: 100%">
                <el-option label="draft" value="draft" />
                <el-option label="active" value="active" />
                <el-option label="disabled" value="disabled" />
                <el-option label="archived" value="archived" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="描述">
            <el-input v-model="profileForm.description" type="textarea" :rows="2" placeholder="这个 Agent Profile 的用途和能力边界" />
          </el-form-item>
          <el-form-item label="上下文标签">
            <el-select
              v-model="profileForm.context_tags"
              multiple
              filterable
              default-first-option
              placeholder="选择上下文标签"
              style="width: 100%"
              @change="handleContextTagsChange"
              @remove-tag="handleContextTagRemove"
            >
              <el-option
                v-for="option in contextTagOptions"
                :key="option"
                :label="option"
                :value="option"
                :disabled="isLockedContextTagOption(option)"
              />
            </el-select>
          </el-form-item>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>Provider / Model</h3>
          </div>
          <div class="profile-form-grid profile-form-grid--three">
            <el-form-item label="Provider" required>
              <el-select v-model="profileForm.provider.provider_id" filterable placeholder="先选择 Provider" style="width: 100%" @change="handleProfileProviderChange">
                <el-option v-for="option in aiProviderOptions" :key="option.value" :label="option.label" :value="option.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="模型供给" required>
              <el-select v-model="profileForm.model_provider_id" :disabled="!profileForm.provider.provider_id" filterable placeholder="再选择该 Provider 提供的模型" style="width: 100%" @change="handleProfileModelProviderChange">
                <el-option v-for="option in providerFirstModelOptions" :key="option.value" :label="option.label" :value="option.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="Provider Type">
              <el-input v-model="profileForm.provider.provider_type" disabled />
            </el-form-item>
            <el-form-item label="Provider Base URL">
              <el-input v-model="profileForm.provider.base_url" disabled />
            </el-form-item>
            <el-form-item label="Binding ID">
              <el-input v-model="profileForm.binding.binding_id" disabled />
            </el-form-item>
            <el-form-item label="Model ID" required>
              <el-input v-model="profileForm.model.model_id" disabled />
            </el-form-item>
            <el-form-item label="Provider Model Key">
              <el-input v-model="profileForm.model.provider_model_key" disabled />
            </el-form-item>
            <el-form-item label="上下文长度">
              <el-input :model-value="selectedProfileBindingContextLabel" disabled />
            </el-form-item>
            <el-form-item label="Credential 引用">
              <el-input v-model="profileForm.provider_credential_ref.credential_id" disabled />
            </el-form-item>
          </div>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>推理参数</h3>
          </div>
          <div class="profile-form-grid profile-form-grid--four">
            <el-form-item label="Temperature">
              <el-input-number v-model="profileForm.inference.temperature" :min="0" :max="2" :step="0.1" controls-position="right" style="width: 100%" />
            </el-form-item>
            <el-form-item label="Max Tokens">
              <el-input-number v-model="profileForm.inference.max_tokens" :min="1" :step="256" controls-position="right" style="width: 100%" />
            </el-form-item>
            <el-form-item label="Thinking Level">
              <el-select v-model="profileForm.inference.thinking_level" style="width: 100%">
                <el-option label="none" value="none" />
                <el-option label="low" value="low" />
                <el-option label="medium" value="medium" />
                <el-option label="high" value="high" />
              </el-select>
            </el-form-item>
            <el-form-item label="Fallback">
              <el-switch v-model="profileForm.inference.fallback.enabled" active-text="启用" inactive-text="关闭" />
            </el-form-item>
          </div>
          <el-form-item v-if="profileForm.inference.fallback.enabled" label="Fallback Model">
            <el-input v-model="profileForm.inference.fallback.model_id" placeholder="备用模型 ID" />
          </el-form-item>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>Prompt</h3>
          </div>
          <div class="profile-form-grid profile-form-grid--two">
            <el-form-item label="System Prompt">
              <el-input v-model="profileForm.prompt.system" type="textarea" :rows="5" placeholder="角色、边界、工具使用规则" />
            </el-form-item>
            <el-form-item label="User Template">
              <el-input v-model="profileForm.prompt.user_template" type="textarea" :rows="5" placeholder="{{input}}" />
            </el-form-item>
          </div>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>Skills</h3>
            <el-button size="small" :icon="Plus" @click="appendResourceRef('skills', 'skill')">添加 Skill</el-button>
          </div>
          <div v-if="profileForm.skills.length === 0" class="resource-empty">暂无 Skill 引用</div>
          <div v-for="(row, index) in profileForm.skills" :key="row.local_id" class="resource-row">
            <el-form-item label="类型">
              <el-input v-model="row.resource_type" />
            </el-form-item>
            <el-form-item label="Skill">
              <el-select v-model="row.resource_id" filterable placeholder="选择已导入 Skill" style="width: 100%" @change="handleResourceRefChange('skills', index)">
                <el-option v-for="option in importedSkillOptions" :key="option.value" :label="option.label" :value="option.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="固定版本">
              <el-select
                v-model="row.resource_version_id"
                filterable
                placeholder="选择版本"
                style="width: 100%"
                :loading="isVersionLoading(row, 'skills')"
                :disabled="!hasValue(row.resource_id)"
                @visible-change="(visible) => visible && ensureVersionOptions(row, 'skills')"
                @change="handleResourceVersionChange('skills', index)"
              >
                <el-option v-for="option in versionOptionsForRow(row, 'skills')" :key="option.value" :label="option.label" :value="option.value" :disabled="option.unavailable" />
              </el-select>
              <p v-if="versionVerificationMessage(row, 'skills')" class="field-tip">{{ versionVerificationMessage(row, 'skills') }}</p>
            </el-form-item>
            <el-form-item label="必需">
              <el-switch v-model="row.required" />
            </el-form-item>
            <el-form-item label="配置 JSON" class="resource-row__config">
              <el-input v-model="row.configJSON" type="textarea" :rows="2" placeholder="{}" />
            </el-form-item>
            <el-button class="resource-row__remove" type="danger" text :icon="Delete" @click="removeResourceRef('skills', index)">删除</el-button>
          </div>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>MCP Servers</h3>
            <el-button size="small" :icon="Plus" @click="appendResourceRef('mcp_servers', 'mcp_server')">添加 MCP</el-button>
          </div>
          <div v-if="profileForm.mcp_servers.length === 0" class="resource-empty">暂无 MCP Server 引用</div>
          <div v-for="(row, index) in profileForm.mcp_servers" :key="row.local_id" class="resource-row">
            <el-form-item label="类型">
              <el-input v-model="row.resource_type" />
            </el-form-item>
            <el-form-item label="MCP Server">
              <el-select v-model="row.resource_id" filterable placeholder="选择 MCP Server" style="width: 100%" @change="handleResourceRefChange('mcp_servers', index)">
                <el-option v-for="option in resourceOptions('mcp_servers')" :key="option.value" :label="option.label" :value="option.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="固定版本">
              <el-select
                v-model="row.resource_version_id"
                filterable
                placeholder="选择版本"
                style="width: 100%"
                :loading="isVersionLoading(row, 'mcp_servers')"
                :disabled="!hasValue(row.resource_id)"
                @visible-change="(visible) => visible && ensureVersionOptions(row, 'mcp_servers')"
                @change="handleResourceVersionChange('mcp_servers', index)"
              >
                <el-option v-for="option in versionOptionsForRow(row, 'mcp_servers')" :key="option.value" :label="option.label" :value="option.value" :disabled="option.unavailable" />
              </el-select>
              <p v-if="versionVerificationMessage(row, 'mcp_servers')" class="field-tip">{{ versionVerificationMessage(row, 'mcp_servers') }}</p>
            </el-form-item>
            <el-form-item label="必需">
              <el-switch v-model="row.required" />
            </el-form-item>
            <el-form-item label="配置 JSON" class="resource-row__config">
              <el-input v-model="row.configJSON" type="textarea" :rows="2" placeholder="{}" />
            </el-form-item>
            <el-button class="resource-row__remove" type="danger" text :icon="Delete" @click="removeResourceRef('mcp_servers', index)">删除</el-button>
          </div>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>Subagents</h3>
            <el-button size="small" :icon="Plus" @click="appendResourceRef('subagents', 'subagent_profile')">添加 Subagent</el-button>
          </div>
          <div v-if="profileForm.subagents.length === 0" class="resource-empty">暂无 Subagent 引用</div>
          <div v-for="(row, index) in profileForm.subagents" :key="row.local_id" class="resource-row">
            <el-form-item label="类型">
              <el-input v-model="row.resource_type" />
            </el-form-item>
            <el-form-item label="Agent Profile">
              <el-select v-model="row.resource_id" filterable placeholder="选择 Agent Profile" style="width: 100%" @change="handleResourceRefChange('subagents', index)">
                <el-option v-for="option in subagentProfileOptions" :key="option.id" :label="option.name" :value="option.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="固定版本">
              <el-select
                v-model="row.resource_version_id"
                filterable
                placeholder="选择已发布版本"
                style="width: 100%"
                :loading="isVersionLoading(row, 'subagents')"
                :disabled="!hasValue(row.resource_id)"
                @visible-change="(visible) => visible && ensureVersionOptions(row, 'subagents')"
                @change="handleResourceVersionChange('subagents', index)"
              >
                <el-option v-for="option in versionOptionsForRow(row, 'subagents')" :key="option.value" :label="option.label" :value="option.value" :disabled="option.unavailable" />
              </el-select>
              <p v-if="versionVerificationMessage(row, 'subagents')" class="field-tip">{{ versionVerificationMessage(row, 'subagents') }}</p>
            </el-form-item>
            <el-form-item label="运行模式">
              <el-select v-model="row.subagentMode" style="width: 100%" @change="handleSubagentModeChange(row)">
                <el-option label="write（可写/可执行）" value="write" />
                <el-option label="read_only（只读）" value="read_only" />
              </el-select>
              <p class="field-tip">控制子 Agent 可用工作区 Tools 范围。生成文件请使用 write。</p>
            </el-form-item>
            <el-form-item label="必需">
              <el-switch v-model="row.required" />
            </el-form-item>
            <el-form-item label="配置 JSON" class="resource-row__config">
              <el-input v-model="row.configJSON" type="textarea" :rows="2" placeholder="{}" @change="syncSubagentModeFromConfig(row)" />
            </el-form-item>
            <el-button class="resource-row__remove" type="danger" text :icon="Delete" @click="removeResourceRef('subagents', index)">删除</el-button>
          </div>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>Tools</h3>
          </div>
          <p class="field-tip">选择本 Profile 可使用的 EasyDo 内置工作区 Tools。留空等同于全部启用。</p>
          <div class="workspace-tools-grid">
            <label v-for="tool in workspaceToolCatalog" :key="tool.name" class="workspace-tool-item">
              <div class="workspace-tool-item__header">
                <el-checkbox
                  :model-value="profileForm.workspaceTools.includes(tool.name)"
                  @change="(checked) => toggleProfileWorkspaceTool(tool.name, checked)"
                >
                  <span class="workspace-tool-item__name">{{ tool.name }}</span>
                  <el-tag size="small" effect="plain" :type="workspaceToolTypeTag(tool.operation_type)">{{ tool.operation_type }}</el-tag>
                </el-checkbox>
              </div>
              <span class="workspace-tool-item__desc">{{ tool.description }}</span>
              <div class="workspace-tool-item__permission">
                <el-radio-group
                  :model-value="profileForm.workspaceToolDecisions[tool.name] || 'request'"
                  size="small"
                  @change="(val) => profileForm.workspaceToolDecisions[tool.name] = val"
                >
                  <el-radio-button v-for="decision in TOOL_PERMISSION_DECISIONS" :key="decision" :value="decision">{{ decision }}</el-radio-button>
                </el-radio-group>
              </div>
            </label>
          </div>
        </section>

        <section class="profile-form-section">
          <el-collapse>
            <el-collapse-item title="高级契约与策略 JSON" name="advanced">
              <div class="profile-form-grid profile-form-grid--two">
                <el-form-item v-for="field in advancedJsonFields" :key="field.key" :label="field.label">
                  <el-input v-model="profileForm[field.key]" type="textarea" :rows="4" />
                </el-form-item>
              </div>
            </el-collapse-item>
          </el-collapse>
        </section>
      </el-form>
      <template #footer>
        <el-button @click="profileDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitProfile">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="mcpServerDialog.visible" :title="mcpServerDialog.mode === 'create' ? '新建 MCP Server' : '编辑 MCP Server'" width="min(960px, calc(100vw - 32px))" append-to-body destroy-on-close>
      <el-form label-position="top" class="agent-resource-form">
        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>基础信息</h3>
            <el-tag :type="mcpConnectionTested ? 'success' : 'info'" effect="plain">{{ mcpConnectionTested ? '连接已测试' : '未测试' }}</el-tag>
          </div>
          <div class="profile-form-grid profile-form-grid--three">
            <el-form-item label="Server Name" required>
              <el-input v-model="mcpForm.serverName" placeholder="easydo-builtin" />
            </el-form-item>
            <el-form-item label="名称" required>
              <el-input v-model="mcpForm.name" placeholder="EasyDo Builtin MCP" />
            </el-form-item>
            <el-form-item label="状态">
              <el-select v-model="mcpForm.status" style="width: 100%">
                <el-option label="draft" value="draft" />
                <el-option label="active" value="active" />
                <el-option label="disabled" value="disabled" />
                <el-option label="archived" value="archived" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="描述">
            <el-input v-model="mcpForm.description" type="textarea" :rows="2" placeholder="这个 MCP Server 提供的工具能力边界" />
          </el-form-item>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>MCP 配置</h3>
          </div>
          <div class="profile-form-grid profile-form-grid--three mcpServerForm">
            <el-form-item label="Type">
              <el-select v-model="mcpForm.type" style="width: 100%">
                <el-option label="streamable_http" value="streamable_http" />
                <el-option label="sse" value="sse" />
                <el-option label="stdio" value="stdio" />
              </el-select>
            </el-form-item>
            <el-form-item v-if="mcpForm.type !== 'stdio'" label="URL">
              <el-input v-model="mcpForm.url" placeholder="<remote-url>" />
            </el-form-item>
            <el-form-item v-if="mcpForm.type !== 'stdio'" label="Headers JSON">
              <el-input v-model="mcpForm.headersJSON" placeholder='{"Header-Key":"Header-Value"}' />
            </el-form-item>
            <el-form-item v-if="mcpForm.type === 'stdio'" label="Command">
              <el-input v-model="mcpForm.command" placeholder="npx" />
            </el-form-item>
            <el-form-item v-if="mcpForm.type === 'stdio'" label="Args JSON">
              <el-input v-model="mcpForm.argsJSON" placeholder='["-y","@modelcontextprotocol/server-filesystem"]' />
            </el-form-item>
            <el-form-item v-if="mcpForm.type === 'stdio'" label="Env JSON">
              <el-input v-model="mcpForm.envJSON" placeholder='{"TOKEN":"..."}' />
            </el-form-item>
            <el-form-item v-if="mcpForm.type === 'stdio'" label="CWD">
              <el-input v-model="mcpForm.cwd" placeholder="/workspace" />
            </el-form-item>
            <el-form-item label="Timeout (ms)">
              <el-input-number v-model="mcpForm.timeout" :min="100" :step="100" controls-position="right" style="width: 100%" />
            </el-form-item>
            <el-form-item label="Disabled">
              <el-switch v-model="mcpForm.disabled" active-text="禁用" inactive-text="启用" />
            </el-form-item>
          </div>
          <div class="resource-dialog-actions">
            <el-button @click="testMcpConnection">测试连接</el-button>
            <el-button :disabled="!mcpConnectionTested" :loading="mcpToolDiscoveryLoading" @click="discoverMcpTools">发现工具</el-button>
          </div>
          <pre class="code-preview mcpConfigPreview">{{ mcpConfigPreview }}</pre>
        </section>

        <section class="profile-form-section">
          <div class="profile-form-section__title">
            <h3>已发现工具</h3>
            <el-tag effect="plain">{{ mcpForm.toolPermissionRows.length }} actions</el-tag>
          </div>
          <div v-if="mcpDiscoveredTools.length === 0" class="resource-empty">暂无工具发现结果。连接测试通过后可触发发现。</div>
          <div v-else class="tool-permission-editor">
            <div class="tool-permission-toolbar">
              <el-input v-model="mcpToolPermissionKeyword" clearable placeholder="搜索工具、操作类型或策略" />
              <el-button-group class="tool-permission-bulk">
                <el-button v-for="decision in TOOL_PERMISSION_DECISIONS" :key="decision" size="small" @click="setAllMcpToolPermissions(decision)">
                  {{ decision }}
                </el-button>
              </el-button-group>
            </div>
            <div class="tool-permission-list">
              <div v-for="row in filteredMcpToolPermissionRows" :key="row.tool_name" class="tool-permission-row">
                <div class="tool-permission-row__main">
                  <strong>{{ row.tool_name }}</strong>
                  <span>{{ row.description || 'No description' }}</span>
                </div>
                <el-tag class="tool-permission-row__operation" effect="plain">{{ row.operation_type || 'unknown' }}</el-tag>
                <el-radio-group v-model="row.decision" size="small" class="tool-permission-row__decision">
                  <el-radio-button v-for="decision in TOOL_PERMISSION_DECISIONS" :key="decision" :value="decision">{{ decision }}</el-radio-button>
                </el-radio-group>
              </div>
              <div v-if="filteredMcpToolPermissionRows.length === 0" class="resource-empty">没有匹配的工具。</div>
            </div>
          </div>
          <el-collapse class="mcp-advanced-permissions">
            <el-collapse-item title="高级权限规则（rules / capabilities / default）" name="advanced">
              <div class="profile-form-grid profile-form-grid--two">
                <el-form-item label="Default Decision">
                  <el-select v-model="mcpForm.defaultDecision" clearable style="width: 100%">
                    <el-option v-for="decision in TOOL_PERMISSION_DECISIONS" :key="decision" :label="decision" :value="decision" />
                  </el-select>
                </el-form-item>
                <el-form-item label="Capabilities JSON">
                  <el-input v-model="mcpForm.capabilitiesJSON" type="textarea" :rows="3" placeholder='{"tools":"request","resources":"deny"}' />
                </el-form-item>
              </div>
              <el-form-item label="Rules JSON">
                <el-input
                  v-model="mcpForm.rulesJSON"
                  type="textarea"
                  :rows="6"
                  placeholder='[{"id":"deny-write","match":"*.write","decision":"deny","mcp_server_keys":["server-a"]}]'
                />
                <p class="field-tip">支持 runtime rules：match/tool_names 通配、mcp_server_keys、capability、operation_types、args 条件。</p>
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </section>
      </el-form>
      <template #footer>
        <el-button @click="mcpServerDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="resourceSubmitting" @click="submitMcpServer">保存 MCP Server</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="skillRepoDialog.visible" :title="skillRepoDialog.mode === 'create' ? '添加 Skills 仓库' : '编辑 Skills 仓库'" width="780px" destroy-on-close>
      <el-form label-position="top" class="agent-resource-form">
        <div class="profile-form-grid profile-form-grid--two">
          <el-form-item label="Git URL" required>
            <el-input v-model="skillRepoForm.url" placeholder="https://github.com/obra/superpowers.git" />
            <p class="field-tip">保存时自动识别仓库名称，例如 obra/superpowers。</p>
            <p v-if="skillRepoDerivedIdentity" class="field-tip field-tip--info">仓库名称：{{ skillRepoDerivedIdentity.name }}</p>
          </el-form-item>
          <el-form-item label="Branch">
            <el-input v-model="skillRepoForm.branch" placeholder="main" />
          </el-form-item>
          <el-form-item label="Subdirectory">
            <el-input v-model="skillRepoForm.subdirectory" placeholder="skills" />
          </el-form-item>
          <el-form-item label="状态">
            <el-select v-model="skillRepoForm.status" style="width: 100%">
              <el-option label="draft" value="draft" />
              <el-option label="active" value="active" />
              <el-option label="disabled" value="disabled" />
              <el-option label="archived" value="archived" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="描述">
          <el-input v-model="skillRepoForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="skillRepoDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="resourceSubmitting" @click="submitSkillRepository">保存仓库</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="dependencyDrawer.visible" title="依赖详情" size="420px">
      <div class="dependency-drawer">
        <div class="dependency-drawer__summary">
          <strong>{{ dependencyDrawer.targetName || '-' }}</strong>
          <el-tag :type="dependencyDrawer.operation === 'inspect' ? 'info' : (dependencyOperationAllowed ? 'success' : 'danger')" effect="plain">
            {{ dependencyDrawer.operation === 'inspect' ? '配置详情' : (dependencyOperationAllowed ? '允许操作' : '存在阻断') }}
          </el-tag>
        </div>
        <el-alert v-if="dependencyDrawer.error" :title="dependencyDrawer.error" type="error" :closable="false" />
        <div v-loading="dependencyDrawer.loading" class="dependency-drawer__body">
          <div v-if="dependencyDrawer.healthIssues.length > 0" class="dependency-drawer__section">
            <h4>配置健康</h4>
            <div v-for="item in dependencyDrawer.healthIssues" :key="healthIssueKey(item)" class="dependency-item dependency-item--health">
              <div>
                <strong>{{ item.label || '配置项' }}</strong>
                <span>{{ item.message || '-' }}</span>
              </div>
              <el-tag :type="healthIssueTagType(item)" size="small" effect="plain">
                {{ healthIssueLabel(item) }}
              </el-tag>
            </div>
          </div>
          <div v-if="dependencyDrawer.healthIssues.length === 0 && dependencyDrawer.dependencies.length === 0" class="resource-empty">没有依赖阻断。</div>
          <div v-for="item in dependencyDrawer.dependencies" :key="dependencyItemKey(item)" class="dependency-item">
            <div>
              <strong>{{ dependencyClassLabel(item.dependency_class) }}</strong>
              <span>{{ dependencyTypeLabel(item) }}</span>
            </div>
            <el-tag :type="dependencyBlocksOperation(item) ? 'danger' : 'info'" size="small" effect="plain">
              {{ dependencyBlocksOperation(item) ? '阻断' : '提示' }}
            </el-tag>
          </div>
        </div>
        <div class="dependency-drawer__footer">
          <el-button @click="dependencyDrawer.visible = false">关闭</el-button>
          <el-button
            v-if="dependencyDrawer.operation !== 'inspect'"
            type="danger"
            :loading="dependencyDrawer.submitting"
            :disabled="dependencyDrawer.loading || !dependencyOperationAllowed"
            @click="confirmDependencyOperation"
          >
            确认{{ destructiveOperationLabel(dependencyDrawer.operation) }}
          </el-button>
        </div>
      </div>
    </el-drawer>

  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Plus, Refresh } from '@element-plus/icons-vue'
import StoreHeaderActions from './components/StoreHeaderActions.vue'
import StoreKindSwitch from './components/StoreKindSwitch.vue'
import { useUserStore } from '@/stores/user'
import {
  EASYDO_WORKSPACE_TOOLS,
  extractWorkspaceToolDecisions,
  mergeSubagentConfigWithMode,
  mergeToolPolicyWithWorkspaceTools,
  subagentModeFromConfig,
  workspaceToolsFromToolPolicy
} from './aiAgentWorkspaceTools'
import {
  createAgentProfile,
  createAgentResource,
  deleteAgentProfile,
  deleteAgentResource,
  getAgentProfileDependencies,
  getAgentResourceDependencies,
  listAgentProfileVersions,
  listAgentProfiles,
  listAgentResourceVersions,
  listAgentResources,
  publishAgentProfile,
  scanAgentResource,
  probeMcpResource,
  updateAgentProfile,
  updateAgentResource,
  validateAgentProfile
} from '@/api/aiAgentStore'
import { openAgentChatboxSession } from '@/api/agentChatbox'
import { listAIProviders } from '@/api/store'
import {
  applyVersionPinToRow,
  buildPinnedResourceRefs,
  BUILTIN_EASYDO_MCP_DIGEST,
  createPinnedResourceRefRow,
  mergeResourceToolPermissionsIntoRefRow,
  profileDependencyHealth as evaluateProfileDependencyHealth,
  profileResourceHealthIssues as evaluateProfileResourceHealthIssues,
  statusTransitionOperation,
  TOOL_PERMISSION_DECISIONS,
  toolPermissionConfigFromRows,
  toolPermissionRowsForResource
} from './aiAgentStoreDependencies'

const router = useRouter()
const userStore = useUserStore()
const storeKind = computed(() => 'ai-agent')
const loading = ref(false)
const submitting = ref(false)
const resourceSubmitting = ref(false)
const keyword = ref('')
const activeAgentStoreView = ref('agent')
const workspaceToolCatalog = EASYDO_WORKSPACE_TOOLS
const profileFilterStatus = ref('')
const skillDiscoveryKeyword = ref('')
const importedSkillKeyword = ref('')
const mcpToolPermissionKeyword = ref('')
const skillScanStatus = ref('未扫描')
const selectedDiscoveredSkillKeys = ref([])
const discoveredSkillRows = ref([])
const activeSkillRepository = ref(null)
const scanningRepositoryId = ref(null)
const mcpConnectionTested = ref(false)
const mcpDiscoveredTools = ref([])
const mcpToolDiscoveryLoading = ref(false)
const profiles = ref([])
const resources = ref({})
const aiProviders = ref([])
const versionCache = reactive({})
const loadErrors = ref([])
const validationResult = ref(null)
const profileDialog = reactive({ visible: false, mode: 'create' })
const mcpServerDialog = reactive({ visible: false, mode: 'create' })
const skillRepoDialog = reactive({ visible: false, mode: 'create' })
const skillScanDialog = reactive({ visible: false })
const dependencyDrawer = reactive({
  visible: false,
  loading: false,
  submitting: false,
  error: '',
  targetKind: '',
  targetId: '',
  targetName: '',
  operation: '',
  dependencies: [],
  healthIssues: [],
  operations: {},
  requestSeq: 0
})
let dependencyDrawerConfirmAction = null

const pageAssistantProfileName = 'page-ai-assistant'
const pageAssistantContextTag = 'page-assistant'
const systemContextTagOptions = []
const canManageSystemContextTags = computed(() => {
  return userStore.isPlatformAdmin || userStore.currentWorkspaceRole === 'owner'
})
const profileForm = reactive(createProfileForm())
const profileOriginalStatus = ref('')
const isEditingReservedPageAssistantProfile = computed(() => {
  return profileDialog.mode === 'edit' && isReservedPageAssistantProfile(profileForm)
})
const contextTagOptions = computed(() => {
  return isReservedPageAssistantProfile(profileForm)
    ? [pageAssistantContextTag, ...systemContextTagOptions]
    : systemContextTagOptions
})
const mcpForm = reactive(createMcpServerForm())
const mcpOriginalStatus = ref('')
const skillRepoForm = reactive(createSkillRepoForm())
const skillRepoOriginalStatus = ref('')
let loadDataSequence = 0
let localRowSequence = 0

const agentModeItems = [
  { name: 'agent', label: 'agent' },
  { name: 'mcp', label: 'mcp' },
  { name: 'skills', label: 'skills' },
  { name: 'tools', label: 'tools' }
]
const advancedJsonFields = [
  { key: 'inputSchemaJSON', label: 'Input Schema' },
  { key: 'outputSchemaJSON', label: 'Output Schema' },
  { key: 'toolPolicyJSON', label: 'Tool Policy' },
  { key: 'memoryPolicyJSON', label: 'Memory Policy' },
  { key: 'confirmationPolicyJSON', label: 'Confirmation Policy' }
]

const filteredProfiles = computed(() => {
  const term = keyword.value.trim().toLowerCase()
  if (!term) return profiles.value
  return profiles.value.filter((profile) => [
    profile.name,
    profile.description,
    profile.status,
    ...profileContextTags(profile)
  ].join(' ').toLowerCase().includes(term))
})

const filteredProfileRows = computed(() => {
  return filteredProfiles.value
    .filter((profile) => !profileFilterStatus.value || profile.status === profileFilterStatus.value)
})

const aiProviderOptions = computed(() => {
  return aiProviders.value.map((provider) => ({
    label: `${provider.name || provider.id} · ${provider.provider_type || provider.type || '-'}`,
    value: provider.id
  }))
})

const providerFirstModelOptions = computed(() => {
  const provider = selectedProfileAIProvider()
  return (provider?.bindings || []).filter((binding) => String(binding.status || 'active') === 'active').map((binding) => {
    const contextLabel = formatBindingContextLabel(binding)
    const modelKey = binding.provider_model_key || binding.binding_key || `Binding #${binding.id}`
    return {
      label: contextLabel === '-'
        ? `${modelKey} · Model #${binding.model_id}`
        : `${modelKey} · Ctx ${contextLabel} · Model #${binding.model_id}`,
      value: binding.id,
      context_window_tokens: resolveBindingContextTokens(binding),
      context_window_label: contextLabel
    }
  })
})

function resolveBindingContextTokens(binding = {}) {
  const metadata = parseMaybeJSON(binding.metadata_json || binding.metadata)
  const number = Number(
    binding.context_window_tokens
    || binding.context_window
    || metadata.context_window_tokens
    || metadata.context_window
    || binding.model?.context_window_tokens
    || binding.model?.context_window
    || 0
  )
  return Number.isFinite(number) && number > 0 ? number : null
}

function formatBindingContextLabel(binding = {}) {
  const number = resolveBindingContextTokens(binding)
  if (!number) return '-'
  if (number % 1024 === 0 && number >= 1024) return `${Math.round(number / 1024)}K`
  if (number >= 1000) return `${Math.round(number / 1000)}K`
  return String(Math.round(number))
}

function parseMaybeJSON(value) {
  if (value && typeof value === 'object' && !Array.isArray(value)) return value
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

const subagentProfileOptions = computed(() => {
  return profiles.value.filter((profile) => !profileForm.id || Number(profile.id) !== Number(profileForm.id))
})

const mcpServerRows = computed(() => resourceGroupRows('mcp_servers'))

const skillResourceRows = computed(() => resourceGroupRows('skills'))

const skillRepositoryRows = computed(() => {
  return skillResourceRows.value.filter((resource) => isSkillRepository(resource))
})

const importedSkillRows = computed(() => {
  return skillResourceRows.value.filter((resource) => isImportedSkill(resource))
})

const importedSkillOptions = computed(() => {
  return importedSkillRows.value
    .filter((resource) => String(resource.status || 'active') === 'active')
    .map((item) => ({
      label: `${item.name || item.resource_key} · ${item.version || 'latest'}`,
      value: item.resource_key ?? item.resource_id ?? item.id
    }))
    .filter((item) => hasValue(item.value))
})

const filteredMcpToolPermissionRows = computed(() => {
  const term = mcpToolPermissionKeyword.value.trim().toLowerCase()
  const rows = Array.isArray(mcpForm.toolPermissionRows) ? mcpForm.toolPermissionRows : []
  if (!term) return rows
  return rows.filter((row) => [
    row.tool_name,
    row.description,
    row.operation_type,
    row.decision
  ].join(' ').toLowerCase().includes(term))
})

const filteredImportedSkillRows = computed(() => {
  const term = importedSkillKeyword.value.trim().toLowerCase()
  if (!term) return importedSkillRows.value
  return importedSkillRows.value.filter((resource) => [
    resource.name,
    resource.resource_key,
    resource.description,
    resource.spec?.source_repository
  ].join(' ').toLowerCase().includes(term))
})

const filteredDiscoveredSkills = computed(() => {
  const term = skillDiscoveryKeyword.value.trim().toLowerCase()
  if (!term) return discoveredSkillRows.value
  return discoveredSkillRows.value.filter((skill) => [
    skill.name,
    skill.key,
    skill.description,
    skill.path
  ].join(' ').toLowerCase().includes(term))
})

const skillScanStatusType = computed(() => {
  if (skillScanStatus.value.includes('已扫描')) return 'success'
  if (skillScanStatus.value.includes('未发现')) return 'warning'
  return 'info'
})

const skillRepoDerivedIdentity = computed(() => {
  if (!String(skillRepoForm.url || '').trim()) return null
  try {
    return deriveSkillRepositoryIdentity(skillRepoForm.url)
  } catch {
    return null
  }
})

const dependencyHealthRows = computed(() => [
  {
    key: 'profiles',
    label: 'Agent Profiles',
    value: `${profiles.value.length} 个`,
    type: profiles.value.length ? 'success' : 'info',
    description: '主 Agent 与可引用 Subagent 都在同一 Profile 列表中管理。'
  },
  {
    key: 'mcp',
    label: 'MCP Server',
    value: `${mcpServerRows.value.length} 个`,
    type: mcpServerRows.value.length ? 'success' : 'info',
    description: '工具发现结果挂在 MCP Server 下，不作为独立资源。'
  },
  {
    key: 'skills',
    label: '已导入 Skills',
    value: `${importedSkillRows.value.length} 个`,
    type: importedSkillRows.value.length ? 'success' : 'warning',
    description: '只有导入后的 Skills 才会出现在 Agent Profile 编辑器中。'
  }
])

const dependencyGraphRows = computed(() => {
  return profiles.value.flatMap((profile) => [
    ...dependencyRowsForRefs(profile, 'subagents', 'subagent profile', resolveProfileRef),
    ...dependencyRowsForRefs(profile, 'mcp_servers', 'tool policy', resolveMcpServerRef),
    ...dependencyRowsForRefs(profile, 'skills', 'skill policy', resolveSkillRef)
  ])
})

const dependencyOperationAllowed = computed(() => {
  return dependencyDrawer.operations?.[dependencyDrawer.operation]?.allowed === true
})

const mcpConfigPreview = computed(() => {
  try {
    const built = buildMcpServerSpecFromForm()
    const preview = {
      spec: built.spec,
      secret_ref: built.secretRef
    }
    if (preview.secret_ref?.values) delete preview.secret_ref.values
    return stringifyJSON(preview)
  } catch (error) {
    return error.message
  }
})

onMounted(() => {
  loadData()
})

watch(() => userStore.currentWorkspaceId, (workspaceId, previousWorkspaceId) => {
  if (workspaceId && workspaceId !== previousWorkspaceId) {
    loadData()
  }
})

async function loadData() {
  const sequence = ++loadDataSequence
  loading.value = true
  loadErrors.value = []
  validationResult.value = null
  clearVersionCache()
  try {
    const workspaceId = await resolveWorkspaceId()
    if (sequence !== loadDataSequence) return
    if (!workspaceId) {
      resetRuntimeData()
      return
    }

    const [profileRes, resourceRes, providerRes] = await Promise.allSettled([
      listAgentProfiles(),
      listAgentResources(),
      listAIProviders()
    ])
    if (sequence !== loadDataSequence) return

    applyLoadResult(profileRes, 'profiles', 'Agent Profile 列表', (payload) => {
      profiles.value = extractArray(payload?.data)
    })
    applyLoadResult(resourceRes, 'resources', 'Agent 资源', (payload) => {
      resources.value = payload?.data || {}
    })
    applyLoadResult(providerRes, 'ai-providers', 'AI Provider', (payload) => {
      aiProviders.value = extractArray(payload?.data)
    })
    await prefetchProfileDependencyVersions(profiles.value, { force: true, silent: true })
    if (sequence !== loadDataSequence) return
  } finally {
    if (sequence === loadDataSequence) {
      loading.value = false
    }
  }
}

async function resolveWorkspaceId() {
  if (userStore.currentWorkspaceId) {
    return userStore.currentWorkspaceId
  }
  await userStore.getUserInfoAction()
  return userStore.currentWorkspaceId
}

function resetRuntimeData() {
  profiles.value = []
  resources.value = {}
  aiProviders.value = []
}

function applyLoadResult(result, key, label, applyData) {
  if (result.status === 'fulfilled') {
    applyData(result.value)
    return
  }
  loadErrors.value.push({
    key,
    message: `${label}加载失败：${errorMessage(result.reason)}`
  })
}

function handleStoreTabChange(name) {
  if (name === 'app') router.push('/store/apps')
  if (name === 'ai') router.push('/store/ai')
}

function setAgentStoreView(name) {
  activeAgentStoreView.value = name
}

function refreshCurrentView() {
  if (activeAgentStoreView.value === 'tools') return
  loadData()
}

function workspaceToolTypeTag(type) {
  if (type === 'read') return 'success'
  if (type === 'write') return 'warning'
  if (type === 'execute') return 'danger'
  return 'info'
}

function workspaceToolRiskTag(risk) {
  if (risk === 'low') return 'success'
  if (risk === 'high') return 'danger'
  return 'warning'
}

function toggleProfileWorkspaceTool(name, checked) {
  const current = new Set(profileForm.workspaceTools || [])
  if (checked) current.add(name)
  else current.delete(name)
  profileForm.workspaceTools = EASYDO_WORKSPACE_TOOLS
    .map((tool) => tool.name)
    .filter((toolName) => current.has(toolName))
}

function parseObjectLoose(value) {
  if (value && typeof value === 'object' && !Array.isArray(value)) return { ...value }
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function handleSubagentModeChange(row) {
  const config = parseObjectLoose(row.configJSON)
  row.configJSON = stringifyJSON(mergeSubagentConfigWithMode(config, row.subagentMode || 'write'))
}

function syncSubagentModeFromConfig(row) {
  const config = parseObjectLoose(row.configJSON)
  row.subagentMode = subagentModeFromConfig(config)
  row.configJSON = stringifyJSON(mergeSubagentConfigWithMode(config, row.subagentMode))
}

function resetProfileFilters() {
  keyword.value = ''
  profileFilterStatus.value = ''
}

function openCreateDialog() {
  Object.assign(profileForm, createProfileForm())
  profileOriginalStatus.value = ''
  hydrateProfileProviderSelection()
  profileDialog.mode = 'create'
  profileDialog.visible = true
}

async function openEditDialog(row) {
  Object.assign(profileForm, createProfileForm(row))
  profileOriginalStatus.value = String(row?.status || '')
  hydrateProfileProviderSelection()
  profileDialog.mode = 'edit'
  profileDialog.visible = true
  await ensureProfileVersionOptions(profileForm, { force: true })
}

async function submitProfile() {
  submitting.value = true
  try {
    await ensureProfileVersionOptions(profileForm, { force: true, silent: true })
    const blockingIssues = blockingProfileHealthIssues(profileForm)
    if (blockingIssues.length > 0) {
      ElMessage.error(blockingIssues[0].message || '引用的固定版本不可用')
      return
    }
    const payload = buildProfilePayload(profileForm)
    if (profileDialog.mode === 'edit' && profileForm.id) {
      const profileId = profileForm.id
      const preflighted = await preflightStatusMutation({
        targetKind: 'profile',
        targetId: profileId,
        targetName: profileForm.name,
        previousStatus: profileOriginalStatus.value,
        nextStatus: payload.status,
        mutate: async () => {
          await updateAgentProfile(profileId, payload)
          profileDialog.visible = false
        }
      })
      if (preflighted) return
      await updateAgentProfile(profileId, payload)
    } else {
      await createAgentProfile(payload)
    }
    profileDialog.visible = false
    await loadData()
  } catch (error) {
    ElMessage.error(errorMessage(error, '保存 Agent Profile 失败'))
  } finally {
    submitting.value = false
  }
}

function openMcpServerDialog(row = null, options = {}) {
  if (isBuiltinEasyDoMcpServer(row)) {
    ElMessage.info('内置 easydo MCP Server 不允许编辑')
    return
  }
  Object.assign(mcpForm, createMcpServerForm(row || {}))
  mcpOriginalStatus.value = String(row?.status || '')
  mcpConnectionTested.value = false
  mcpDiscoveredTools.value = discoveredMcpTools(row || {})
  mcpToolDiscoveryLoading.value = false
  mcpToolPermissionKeyword.value = ''
  mcpServerDialog.mode = row?.id ? 'edit' : 'create'
  mcpServerDialog.visible = true
  if (options.discover) {
    setTimeout(() => {
      testMcpConnection()
      discoverMcpTools()
    })
  }
}

async function submitMcpServer() {
  let payload
  try {
    payload = buildMcpResourcePayload(mcpForm)
  } catch (error) {
    ElMessage.error(error.message)
    return
  }
  resourceSubmitting.value = true
  try {
    if (mcpServerDialog.mode === 'edit' && mcpForm.id) {
      const resourceId = mcpForm.id
      const preflighted = await preflightStatusMutation({
        targetKind: 'resource',
        targetId: resourceId,
        targetName: mcpForm.name || mcpForm.serverName,
        previousStatus: mcpOriginalStatus.value,
        nextStatus: payload.status,
        mutate: async () => {
          await updateAgentResource(resourceId, payload)
          mcpServerDialog.visible = false
        }
      })
      if (preflighted) return
      await updateAgentResource(resourceId, payload)
    } else {
      await createAgentResource(payload)
    }
    mcpServerDialog.visible = false
    await loadData()
  } catch (error) {
    ElMessage.error(errorMessage(error, '保存 MCP Server 失败'))
  } finally {
    resourceSubmitting.value = false
  }
}

async function testMcpConnection() {
  try {
    const config = buildMcpProbeConfigFromForm()
    mcpToolDiscoveryLoading.value = true
    const result = await probeMcpResource({ config })
    const data = result?.data || result || {}
    const discovered = normalizeMcpTools(data.discovered_tools || [])
    mcpConnectionTested.value = true
    if (discovered.length > 0) {
      mcpForm.discoveredToolsJSON = stringifyJSON(discovered)
      mcpDiscoveredTools.value = discovered
      syncMcpToolPermissionRows(discovered)
    }
    ElMessage.success(discovered.length > 0
      ? `MCP 连接成功，发现 ${discovered.length} 个工具`
      : 'MCP 连接成功（initialize + tools/list）')
  } catch (error) {
    mcpConnectionTested.value = false
    ElMessage.error(errorMessage(error, error?.message || 'MCP 连接测试失败'))
  } finally {
    mcpToolDiscoveryLoading.value = false
  }
}

async function discoverMcpTools() {
  if (!mcpConnectionTested.value) {
    ElMessage.warning('请先测试连接')
    return
  }
  mcpToolDiscoveryLoading.value = true
  try {
    let discovered = []
    if (mcpForm.id) {
      const result = await scanAgentResource(mcpForm.id)
      const scannedResource = result?.data?.resource || result?.resource || null
      discovered = normalizeMcpTools(
        result?.data?.discovered_tools || result?.discovered_tools || discoveredMcpTools(scannedResource || {})
      )
      await loadData()
    } else {
      const config = buildMcpProbeConfigFromForm()
      const result = await probeMcpResource({ config })
      const data = result?.data || result || {}
      discovered = normalizeMcpTools(data.discovered_tools || [])
    }
    mcpForm.discoveredToolsJSON = stringifyJSON(discovered)
    mcpDiscoveredTools.value = discovered
    syncMcpToolPermissionRows(discovered)
    if (discovered.length === 0) {
      ElMessage.warning('MCP Server 没有返回工具')
    } else {
      ElMessage.success(`发现 ${discovered.length} 个工具`)
    }
  } catch (error) {
    ElMessage.error(errorMessage(error, '发现 MCP 工具失败'))
  } finally {
    mcpToolDiscoveryLoading.value = false
  }
}

function buildMcpProbeConfigFromForm() {
  const payload = buildMcpResourcePayload(mcpForm)
  const serverName = String(mcpForm.serverName || 'custom-mcp').trim()
  const server = (payload.spec?.mcpServers || {})[serverName] || Object.values(payload.spec?.mcpServers || {})[0] || {}
  if (['streamable_http', 'sse'].includes(server.type) && !server.url) {
    throw new Error(`${server.type} 类型需要填写 URL`)
  }
  if (server.type === 'stdio' && !server.command) {
    throw new Error('stdio 类型需要填写 Command')
  }
  return {
    ...server,
    server_key: serverName,
    type: server.type || mcpForm.type || 'streamable_http'
  }
}

function syncMcpToolPermissionRows(tools = mcpDiscoveredTools.value) {
  const currentConfig = toolPermissionConfigFromRows(mcpForm.toolPermissionRows || [])
  mcpForm.toolPermissionRows = toolPermissionRowsForResource({
    spec: { discovered_tools: normalizeMcpTools(tools) }
  }, currentConfig)
}

function setAllMcpToolPermissions(decision) {
  const normalized = TOOL_PERMISSION_DECISIONS.includes(decision) ? decision : 'request'
  mcpForm.toolPermissionRows.forEach((row) => {
    row.decision = normalized
  })
}

function openSkillRepoDialog(row = null) {
  Object.assign(skillRepoForm, createSkillRepoForm(row || {}))
  skillRepoOriginalStatus.value = String(row?.status || '')
  skillRepoDialog.mode = row?.id ? 'edit' : 'create'
  skillRepoDialog.visible = true
}

async function submitSkillRepository() {
  let payload
  try {
    payload = buildSkillRepositoryPayload(skillRepoForm)
  } catch (error) {
    ElMessage.error(error.message)
    return
  }
  resourceSubmitting.value = true
  try {
    if (skillRepoDialog.mode === 'edit' && skillRepoForm.id) {
      const resourceId = skillRepoForm.id
      const preflighted = await preflightStatusMutation({
        targetKind: 'resource',
        targetId: resourceId,
        targetName: skillRepoDerivedIdentity.value?.name || skillRepoForm.url,
        previousStatus: skillRepoOriginalStatus.value,
        nextStatus: payload.status,
        mutate: async () => {
          await updateAgentResource(resourceId, payload)
          skillRepoDialog.visible = false
        }
      })
      if (preflighted) return
      await updateAgentResource(resourceId, payload)
    } else {
      await createAgentResource(payload)
    }
    skillRepoDialog.visible = false
    await loadData()
  } catch (error) {
    ElMessage.error(errorMessage(error, '保存 Skills 仓库失败'))
  } finally {
    resourceSubmitting.value = false
  }
}

async function deleteSkillRepository(row) {
  await handleDeleteResource(row)
  if (activeSkillRepository.value?.id === row.id) {
    activeSkillRepository.value = null
    discoveredSkillRows.value = []
    selectedDiscoveredSkillKeys.value = []
    skillScanStatus.value = '未扫描'
    skillScanDialog.visible = false
  }
}

async function scanSkillRepository(row) {
  activeSkillRepository.value = row
  scanningRepositoryId.value = row.id
  skillScanStatus.value = '扫描中'
  skillDiscoveryKeyword.value = ''
  skillScanDialog.visible = false
  try {
    const result = await scanAgentResource(row.id)
    const scannedResource = result?.data?.resource || result?.resource || row
    const discovered = normalizeDiscoveredSkills(
      result?.data?.discovered_skills || result?.discovered_skills || repositoryDiscoveredSkills(scannedResource),
      scannedResource
    )
    activeSkillRepository.value = scannedResource
    discoveredSkillRows.value = discovered
    selectedDiscoveredSkillKeys.value = discovered
      .filter((skill) => !isSkillImported(skill.key))
      .map((skill) => skill.key)
    skillScanStatus.value = discovered.length ? `已扫描 ${discovered.length} 个` : '未发现 Skills'
    await loadData()
    skillScanDialog.visible = true
    if (discovered.length === 0) {
      ElMessage.warning('仓库中没有发现 SKILL.md')
    } else {
      ElMessage.success(`发现 ${discovered.length} 个 Skills`)
    }
  } catch (error) {
    skillScanStatus.value = '扫描失败'
    ElMessage.error(errorMessage(error, '扫描 Skills 仓库失败'))
  } finally {
    scanningRepositoryId.value = null
  }
}

function toggleDiscoveredSkill(skillKey, checked) {
  const current = new Set(selectedDiscoveredSkillKeys.value)
  if (checked) {
    current.add(skillKey)
  } else {
    current.delete(skillKey)
  }
  selectedDiscoveredSkillKeys.value = [...current]
}

async function importSelectedSkills() {
  const repository = activeSkillRepository.value
  if (!repository) {
    ElMessage.warning('请先扫描 Skills 仓库')
    return
  }
  const selected = discoveredSkillRows.value.filter((skill) => selectedDiscoveredSkillKeys.value.includes(skill.key))
  if (selected.length === 0) {
    ElMessage.warning('请先勾选需要导入的 Skills')
    return
  }
  resourceSubmitting.value = true
  try {
    await Promise.all(selected.map((skill) => {
      const existing = importedSkillRows.value.find((resource) => String(resource.resource_key) === String(skill.key))
      const payload = buildImportedSkillPayload(repository, skill)
      return existing ? updateAgentResource(existing.id, payload) : createAgentResource(payload)
    }))
    selectedDiscoveredSkillKeys.value = []
    await loadData()
    skillScanDialog.visible = false
    ElMessage.success(`已导入 ${selected.length} 个 Skills`)
  } catch (error) {
    ElMessage.error(errorMessage(error, '导入 Skills 失败'))
  } finally {
    resourceSubmitting.value = false
  }
}

async function handleValidate(row) {
  try {
    const result = await validateAgentProfile(row.id)
    const payload = result?.data || result || {}
    const resourceHealth = result?.data?.resource_health || result?.resource_health || []
    validationResult.value = {
      profile_id: row.id,
      profile_name: row.name,
      status: payload.status || 'unknown',
      errors: Array.isArray(payload.errors) ? payload.errors : [],
      warnings: Array.isArray(payload.warnings) ? payload.warnings : [],
      resource_health: Array.isArray(resourceHealth) ? resourceHealth : []
    }
    if (validationResult.value.status === 'passed') {
      ElMessage.success('Agent Profile 校验通过')
    } else {
      ElMessage.warning('Agent Profile 校验失败')
    }
  } catch (error) {
    validationResult.value = null
    ElMessage.error(errorMessage(error, '校验失败'))
  }
}

async function handlePublish(row) {
  try {
    await publishAgentProfile(row.id, { change_summary: 'publish from ai-agent store' })
    await loadData()
    ElMessage.success('Agent Profile 已发布')
  } catch (error) {
    ElMessage.error(errorMessage(error, '发布失败'))
  }
}

async function preflightStatusMutation(options) {
  const operation = statusTransitionOperation(options.previousStatus, options.nextStatus)
  if (!operation) return false
  const loadDependencies = options.targetKind === 'profile'
    ? () => getAgentProfileDependencies(options.targetId)
    : options.targetKind === 'resource'
      ? () => getAgentResourceDependencies(options.targetId)
      : null
  if (!loadDependencies) throw new Error('不支持的依赖目标类型')

  await openDependencyDrawer({
    targetKind: options.targetKind,
    targetId: options.targetId,
    targetName: options.targetName,
    operation,
    loadDependencies,
    confirmAction: options.mutate
  })
  return true
}

async function handleDelete(row) {
  await openDependencyDrawer({
    targetKind: 'profile',
    targetId: row.id,
    targetName: row.name,
    operation: 'delete',
    loadDependencies: () => getAgentProfileDependencies(row.id),
    confirmAction: () => deleteAgentProfile(row.id)
  })
}

async function openProfileDependencyHealth(row) {
  const issues = profileResourceHealthIssues(row)
  await openDependencyDrawer({
    targetKind: 'profile',
    targetId: row.id,
    targetName: row.name,
    operation: 'inspect',
    healthIssues: issues,
    loadDependencies: () => getAgentProfileDependencies(row.id),
    confirmAction: null
  })
}

async function handleOpenChatbox(profile) {
  if (!canOpenChatboxProfile(profile)) {
    ElMessage.warning('该 Agent Profile 不支持打开对话')
    return
  }
  try {
    const result = await openAgentChatboxSession({
      agent_profile_id: profile.id,
      agent_profile_version_id: 'draft',
      title: profile.name || 'Agent Chatbox'
    })
    const url = result?.data?.url || result?.url
    if (!url) throw new Error('Agent Chatbox 会话地址为空')
    window.open(url, '_blank', 'noopener')
  } catch (error) {
    ElMessage.error(errorMessage(error, '打开 Agent Chatbox 失败'))
  }
}

async function handleDeleteResource(row) {
  if (isBuiltinEasyDoMcpServer(row)) {
    ElMessage.info('内置 easydo MCP Server 不允许删除')
    return
  }
  await openDependencyDrawer({
    targetKind: 'resource',
    targetId: row.id,
    targetName: row.name,
    operation: 'delete',
    loadDependencies: () => getAgentResourceDependencies(row.id),
    confirmAction: () => deleteAgentResource(row.id)
  })
}

async function openDependencyDrawer(options) {
  const sequence = ++dependencyDrawer.requestSeq
  dependencyDrawer.visible = true
  dependencyDrawer.loading = true
  dependencyDrawer.submitting = false
  dependencyDrawer.error = ''
  dependencyDrawer.targetKind = options.targetKind
  dependencyDrawer.targetId = options.targetId
  dependencyDrawer.targetName = options.targetName || ''
  dependencyDrawer.operation = options.operation
  dependencyDrawer.dependencies = []
  dependencyDrawer.healthIssues = Array.isArray(options.healthIssues) ? options.healthIssues : []
  dependencyDrawer.operations = {}
  dependencyDrawerConfirmAction = options.confirmAction
  try {
    const response = await options.loadDependencies()
    if (sequence !== dependencyDrawer.requestSeq) return
    applyDependencyPayload(response?.data || response || {})
  } catch (error) {
    if (sequence === dependencyDrawer.requestSeq) {
      const conflict = extractDependencyConflict(error)
      if (conflict) {
        applyDependencyPayload(conflict)
      }
      dependencyDrawer.error = errorMessage(error, '加载依赖失败')
    }
  } finally {
    if (sequence === dependencyDrawer.requestSeq) dependencyDrawer.loading = false
  }
}

async function confirmDependencyOperation() {
  if (!dependencyOperationAllowed.value || typeof dependencyDrawerConfirmAction !== 'function') return
  try {
    await ElMessageBox.confirm(`确认${destructiveOperationLabel(dependencyDrawer.operation)} ${dependencyDrawer.targetName} 吗？`, '提示', { type: 'warning' })
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(errorMessage(error, '操作取消失败'))
    return
  }
  dependencyDrawer.submitting = true
  dependencyDrawer.error = ''
  try {
    await dependencyDrawerConfirmAction()
    dependencyDrawer.visible = false
    dependencyDrawerConfirmAction = null
    await loadData()
  } catch (error) {
    const conflict = extractDependencyConflict(error)
    if (conflict) {
      applyDependencyPayload(conflict)
      dependencyDrawer.error = errorMessage(error, '依赖状态已变化')
    } else {
      dependencyDrawer.error = errorMessage(error, '操作失败')
    }
  } finally {
    dependencyDrawer.submitting = false
  }
}

function applyDependencyPayload(payload = {}) {
  dependencyDrawer.dependencies = Array.isArray(payload.dependencies) ? payload.dependencies : []
  dependencyDrawer.operations = payload.operations && typeof payload.operations === 'object' ? payload.operations : {}
}

function profileHealthContext() {
  return {
    profiles: profiles.value,
    resources: resources.value,
    versionCache,
    validationResult: validationResult.value
  }
}

function profileResourceHealthIssues(profile = {}) {
  return evaluateProfileResourceHealthIssues(profile, profileHealthContext())
}

function profileDependencyHealth(profile = {}) {
  const issues = profileResourceHealthIssues(profile)
  return evaluateProfileDependencyHealth(profile, { issues })
}

function healthIssueKey(item = {}) {
  return [
    item.section,
    item.index,
    item.resource_id,
    item.resource_version_id,
    item.severity,
    item.message
  ].map((part) => String(part || '')).join(':')
}

function healthIssueTagType(item = {}) {
  if (item.severity === 'invalid') return 'danger'
  if (item.severity === 'update_available') return 'warning'
  return 'info'
}

function healthIssueLabel(item = {}) {
  if (item.severity === 'invalid') return '配置失效'
  if (item.severity === 'update_available') return '可更新'
  return '提示'
}

function extractDependencyConflict(error) {
  const data = error?.response?.data || {}
  if (Array.isArray(data.dependencies) || data.operations) return data
  if (Array.isArray(data.details?.dependencies) || data.details?.operations) return data.details
  return null
}

function dependencyBlocksOperation(item = {}) {
  return ['blocking_live_ref', 'published_version', 'session_reference', 'active_execution'].includes(String(item.dependency_class || ''))
}

function dependencyItemKey(item = {}) {
  return [
    item.dependency_class,
    item.dependency_type,
    item.profile_id,
    item.profile_version_id,
    item.session_id,
    item.runtime_run_id
  ].map((part) => String(part || '')).join(':')
}

function dependencyClassLabel(value) {
  const key = String(value || '')
  const labels = {
    blocking_live_ref: '当前引用',
    published_version: '已发布版本',
    historical_snapshot: '历史快照',
    session_reference: '会话引用',
    active_execution: '运行中执行'
  }
  return labels[key] || key || '依赖'
}

function dependencyTypeLabel(item = {}) {
  return [
    item.profile_name,
    item.profile_id ? `Profile #${item.profile_id}` : '',
    item.profile_version ? `v${item.profile_version}` : '',
    item.session_id ? `Session #${item.session_id}` : '',
    item.runtime_run_id ? `Run ${item.runtime_run_id}` : ''
  ].filter(Boolean).join(' · ') || String(item.dependency_type || '-')
}

function destructiveOperationLabel(operation) {
  const labels = {
    delete: '删除',
    disable: '禁用',
    archive: '归档'
  }
  return labels[operation] || '操作'
}

function createProfileForm(profile = {}) {
  return {
    id: profile.id || null,
    name: profile.name || '',
    description: profile.description || '',
    context_tags: contextTagsForForm(profile),
    response_mode: profile.response_mode || 'mixed',
    status: profile.status || 'draft',
    model_provider_id: profile.model_provider_id || profile.binding?.model_provider_id || profile.binding?.binding_id || '',
    provider: {
      provider_id: profile.provider?.provider_id || '',
      provider_type: profile.provider?.provider_type || profile.provider?.type || '',
      base_url: profile.provider?.base_url || '',
      llm_endpoint: profile.provider?.llm_endpoint || '',
      settings_json: profile.provider?.settings_json || '',
      headers_json: profile.provider?.headers_json || ''
    },
    binding: {
      binding_id: profile.binding?.binding_id || ''
    },
    model: {
      model_id: profile.model?.model_id || '',
      provider_model_key: profile.model?.provider_model_key || ''
    },
    provider_credential_ref: {
      credential_id: profile.provider_credential_ref?.credential_id || ''
    },
    inference: {
      temperature: numberOrDefault(profile.inference?.temperature, 0.2),
      max_tokens: numberOrDefault(profile.inference?.max_tokens, 4096),
      thinking_level: profile.inference?.thinking_level || 'medium',
      fallback: {
        enabled: profile.inference?.fallback?.enabled === true,
        model_id: profile.inference?.fallback?.model_id || ''
      }
    },
    prompt: {
      system: profile.prompt?.system || '',
      user_template: profile.prompt?.user_template || '{{input}}'
    },
    skills: normalizeResourceRefs(profile.skills, 'skill'),
    subagents: normalizeResourceRefs(profile.subagents, 'subagent_profile'),
    mcp_servers: normalizeResourceRefs(profile.mcp_servers, 'mcp_server'),
    workspaceTools: workspaceToolsFromToolPolicy(profile.tool_policy || {}),
    workspaceToolDecisions: extractWorkspaceToolDecisions(profile.tool_policy || {}),
    inputSchemaJSON: stringifyJSON(profile.input_schema || { type: 'object' }),
    outputSchemaJSON: stringifyJSON(profile.output_schema || { type: 'object' }),
    toolPolicyJSON: stringifyJSON(profile.tool_policy || {}),
    memoryPolicyJSON: stringifyJSON(profile.memory_policy || {}),
    confirmationPolicyJSON: stringifyJSON(profile.confirmation_policy || {})
  }
}

function buildProfilePayload(form) {
  const name = form.name.trim()
  if (!name) throw new Error('名称不能为空')
  const contextTags = contextTagsForPayload(form)

  return {
    name,
    description: form.description,
    context_tags: contextTags,
    response_mode: form.response_mode,
    status: form.status,
    model_provider_id: form.model_provider_id || undefined,
    provider: compactObject(form.provider),
    binding: compactObject(form.binding),
    model: compactObject(form.model),
    provider_credential_ref: compactObject(form.provider_credential_ref),
    inference: buildInference(form.inference),
    prompt: compactObject(form.prompt),
    skills: buildResourceRefs(form.skills, 'Skills', 'skill'),
    subagents: buildResourceRefs(form.subagents, 'Subagents', 'subagent_profile'),
    mcp_servers: buildResourceRefs(form.mcp_servers, 'MCP Servers', 'mcp_server'),
    input_schema: parseJSONObject(form.inputSchemaJSON, 'Input Schema'),
    output_schema: parseJSONObject(form.outputSchemaJSON, 'Output Schema'),
    tool_policy: mergeToolPolicyWithWorkspaceTools(
      parseJSONObject(form.toolPolicyJSON, 'Tool Policy'),
      form.workspaceTools,
      form.workspaceToolDecisions
    ),
    memory_policy: parseJSONObject(form.memoryPolicyJSON, 'Memory Policy'),
    confirmation_policy: parseJSONObject(form.confirmationPolicyJSON, 'Confirmation Policy')
  }
}

function profileContextTags(profile = {}) {
  return normalizeStringArray(profile.context_tags)
}

function isReservedPageAssistantProfile(profile = {}) {
  return String(profile.name || '').trim() === pageAssistantProfileName
}

function ensureReservedPageAssistantContextTag(value = []) {
  return uniqueStrings([pageAssistantContextTag, ...normalizeStringArray(value)])
}

function contextTagsForForm(profile = {}) {
  const tags = profileContextTags(profile)
  if (isReservedPageAssistantProfile(profile)) {
    return ensureReservedPageAssistantContextTag(tags)
  }
  return tags.filter((tag) => tag !== pageAssistantContextTag)
}

function contextTagsForPayload(form) {
  const tags = uniqueStrings(normalizeStringArray(form.context_tags))
  if (isReservedPageAssistantProfile(form)) {
    return ensureReservedPageAssistantContextTag(tags)
  }
  if (tags.includes(pageAssistantContextTag)) {
    throw new Error('普通 Agent Profile 不允许配置 page-assistant 上下文标签')
  }
  return tags
}

function handleContextTagsChange(value) {
  const tags = uniqueStrings(normalizeStringArray(value))
  if (isReservedPageAssistantProfile(profileForm)) {
    profileForm.context_tags = ensureReservedPageAssistantContextTag(tags)
    return
  }
  if (tags.includes(pageAssistantContextTag)) {
    ElMessage.warning('普通 Agent Profile 不允许配置 page-assistant 上下文标签')
  }
  profileForm.context_tags = tags.filter((tag) => tag !== pageAssistantContextTag)
}

function handleContextTagRemove(tag) {
  if (tag !== pageAssistantContextTag || !isReservedPageAssistantProfile(profileForm)) return
  profileForm.context_tags = ensureReservedPageAssistantContextTag(profileForm.context_tags)
  ElMessage.warning('page-ai-assistant 必须包含 page-assistant 上下文标签')
}

function isLockedContextTagOption(tag) {
  return isReservedPageAssistantProfile(profileForm) && tag === pageAssistantContextTag
}

function canMutateProfile(profile = {}) {
  if (isReservedPageAssistantProfile(profile)) return canManageSystemContextTags.value
  return true
}

function canOpenChatboxProfile(profile = {}) {
  const status = String(profile.status || '').trim()
  return status === 'active' && !profileContextTags(profile).includes(pageAssistantContextTag)
}

function selectedProfileAIProvider() {
  return aiProviders.value.find((provider) => String(provider.id) === String(profileForm.provider.provider_id)) || null
}

function selectedProfileModelBinding() {
  const provider = selectedProfileAIProvider()
  return (provider?.bindings || []).find((binding) => String(binding.id) === String(profileForm.model_provider_id)) || null
}

const selectedProfileBindingContextLabel = computed(() => {
  return formatBindingContextLabel(selectedProfileModelBinding() || {})
})

function handleProfileProviderChange() {
  const provider = selectedProfileAIProvider()
  applyProfileProviderSnapshot(provider)
  const firstBinding = providerFirstModelOptions.value[0]
  profileForm.model_provider_id = firstBinding?.value || ''
  handleProfileModelProviderChange()
}

function handleProfileModelProviderChange() {
  const binding = selectedProfileModelBinding()
  if (!binding) {
    profileForm.binding.binding_id = ''
    profileForm.binding.model_provider_id = ''
    profileForm.binding.provider_model_key = ''
    profileForm.binding.context_window_tokens = null
    profileForm.model.model_id = ''
    profileForm.model.provider_model_key = ''
    profileForm.model.context_window_tokens = null
    profileForm.model.context_window = null
    return
  }
  profileForm.binding.binding_id = binding.id
  profileForm.binding.model_provider_id = binding.id
  profileForm.binding.provider_model_key = binding.provider_model_key || binding.binding_key || ''
  profileForm.binding.context_window_tokens = resolveBindingContextTokens(binding)
  profileForm.model.model_id = binding.model_id
  profileForm.model.provider_model_key = binding.provider_model_key || binding.binding_key || ''
  profileForm.model.context_window_tokens = resolveBindingContextTokens(binding)
  profileForm.model.context_window = resolveBindingContextTokens(binding)
}

function hydrateProfileProviderSelection() {
  if (!profileForm.provider.provider_id && aiProviderOptions.value.length > 0) {
    profileForm.provider.provider_id = aiProviderOptions.value[0].value
  }
  if (profileForm.provider.provider_id) {
    applyProfileProviderSnapshot(selectedProfileAIProvider())
  }
  if (!profileForm.model_provider_id && providerFirstModelOptions.value.length > 0) {
    profileForm.model_provider_id = providerFirstModelOptions.value[0].value
  }
  if (profileForm.model_provider_id) {
    handleProfileModelProviderChange()
  }
}

function applyProfileProviderSnapshot(provider) {
  if (!provider) return
  const settings = providerRuntimeSettings(provider)
  const headers = parseMaybeObject(provider.headers_json)
  profileForm.provider.provider_id = provider.id
  profileForm.provider.provider_type = provider.provider_type || provider.type || ''
  profileForm.provider.base_url = provider.base_url || provider.endpoint || ''
  profileForm.provider.llm_endpoint = settings.llm_endpoint || provider.llm_endpoint || ''
  profileForm.provider.settings_json = provider.settings_json || ''
  profileForm.provider.headers_json = Object.keys(headers).length > 0 ? headers : ''
  profileForm.provider_credential_ref.credential_id = provider.credential_id || ''
}

function providerRuntimeSettings(provider = {}) {
  return parseMaybeObject(provider.settings_json)
}

function createMcpServerForm(resource = {}) {
  const serverName = firstMcpServerName(resource)
  const config = mcpServerConfig(resource)
  const permissions = resource.spec?.tool_permissions || {}
  return {
    id: resource.id || null,
    serverName: serverName || resource.resource_key || '',
    name: resource.name || '',
    description: resource.description || '',
    version: resource.version || 'latest',
    status: resource.status || 'draft',
    type: config.type || 'streamable_http',
    url: config.url || '',
    headersJSON: stringifyJSON(config.headers || {}),
    command: config.command || '',
    argsJSON: stringifyJSON(Array.isArray(config.args) ? config.args : []),
    envJSON: stringifyJSON(config.env || {}),
    cwd: config.cwd || '',
    timeout: numberOrDefault(config.timeout_ms, 30000),
    disabled: config.disabled === true,
    mcpServersJSON: stringifyJSON(resource.spec?.mcpServers || {}),
    discoveredToolsJSON: stringifyJSON(discoveredMcpTools(resource)),
    toolPermissionRows: toolPermissionRowsForResource(resource, { tool_permissions: permissions }),
    defaultDecision: permissions.default_decision || permissions.defaultDecision || '',
    capabilitiesJSON: stringifyJSON(permissions.capabilities || {}),
    rulesJSON: stringifyJSON(Array.isArray(permissions.rules) ? permissions.rules : [])
  }
}

function buildMcpResourcePayload(form) {
  const resourceKey = String(form.serverName || '').trim()
  const name = String(form.name || '').trim()
  if (!resourceKey) throw new Error('Server Name 不能为空')
  if (!name) throw new Error('名称不能为空')
  const { spec, endpoint, secretRef } = buildMcpServerSpecFromForm()
  return {
    resource_kind: 'mcp_server',
    resource_key: resourceKey,
    name,
    description: form.description,
    version: form.version || 'latest',
    status: form.disabled ? 'disabled' : form.status,
    spec: {
      ...spec,
      discovered_tools: normalizeMcpTools(form.discoveredToolsJSON),
      tool_permissions: toolPermissionConfigFromRows(form.toolPermissionRows, {
        default_decision: form.defaultDecision,
        capabilities: parseJSONObject(form.capabilitiesJSON || '{}', 'Capabilities'),
        rules: parseJSONArray(form.rulesJSON || '[]', 'Rules')
      }).tool_permissions
    },
    endpoint,
    secret_ref: secretRef,
    tags: ['mcp-server', spec.mcpServers[resourceKey]?.type].filter(Boolean)
  }
}

function buildMcpServerSpecFromForm() {
  const serverName = String(mcpForm.serverName || 'custom-mcp').trim()
  const type = String(mcpForm.type || 'streamable_http').trim()
  const secretRef = { keys: [], values: {} }
  const serverConfigs = mcpServerConfigsFromForm(secretRef)
  const firstServer = serverConfigs[serverName] || Object.values(serverConfigs)[0] || {}
  return {
    spec: {
      mcpServers: serverConfigs
    },
    endpoint: {
      type: firstServer.type || type,
      command: firstServer.command,
      url: firstServer.url,
      cwd: firstServer.cwd,
      timeout_ms: firstServer.timeout_ms
    },
    secretRef: secretRef.keys.length > 0 ? secretRef : {}
  }
}

function mcpServerConfigsFromForm(secretRef) {
  const serverName = String(mcpForm.serverName || 'custom-mcp').trim()
  const rawServers = parseMaybeObject(mcpForm.mcpServersJSON)
  if (Object.keys(rawServers).length > 0) {
    return Object.fromEntries(Object.entries(rawServers).map(([key, raw]) => [
      key,
      normalizeMcpServerConfig(raw, secretRef, key)
    ]))
  }
  const type = String(mcpForm.type || 'streamable_http').trim()
  const config = {
    type,
    timeout_ms: numberOrDefault(mcpForm.timeout, 30000),
    disabled: mcpForm.disabled === true
  }
  if (type === 'stdio') {
    config.command = String(mcpForm.command || '').trim()
    config.args = parseJSONArray(mcpForm.argsJSON, 'Args')
    config.env = redactSecretMap(parseJSONObject(mcpForm.envJSON, 'Env'), secretRef, `${serverName}.env`)
    config.cwd = String(mcpForm.cwd || '').trim()
  } else {
    config.url = String(mcpForm.url || '').trim()
    config.headers = redactSecretMap(parseJSONObject(mcpForm.headersJSON, 'Headers'), secretRef, `${serverName}.headers`)
  }
  return { [serverName]: config }
}

function normalizeMcpServerConfig(raw, secretRef, serverName) {
  const input = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {}
  const type = String(input.type || input.transport || (input.command ? 'stdio' : 'streamable_http')).trim()
  const config = { ...input, type }
  if (type === 'stdio' || type === 'command') {
    config.env = redactSecretMap(parseMaybeObject(input.env), secretRef, `${serverName}.env`)
  } else {
    config.headers = redactSecretMap(parseMaybeObject(input.headers), secretRef, `${serverName}.headers`)
  }
  return config
}

function redactSecretMap(input = {}, secretRef = { keys: [], values: {} }, prefix = 'headers') {
  const output = {}
  for (const [key, value] of Object.entries(input || {})) {
    const text = String(value ?? '')
    const looksSecret = /token|secret|password|authorization|api[_-]?key|bearer/i.test(key) || /^Bearer\s+/i.test(text)
    if (looksSecret && text && !String(text).startsWith('secret_ref:')) {
      const refKey = `${prefix}.${key}`
      secretRef.keys.push({ key: refKey, placeholder: `secret_ref:${refKey}` })
      secretRef.values[refKey] = text
      output[key] = `secret_ref:${refKey}`
    } else {
      output[key] = value
    }
  }
  return output
}

function createSkillRepoForm(resource = {}) {
  const spec = skillRepositorySpec(resource)
  return {
    id: resource.id || null,
    description: resource.description || '',
    status: resource.status || 'active',
    url: spec.url || '',
    branch: spec.branch || 'main',
    subdirectory: spec.subdirectory || 'skills'
  }
}

function buildSkillRepositoryPayload(form) {
  const url = String(form.url || '').trim()
  if (!url) throw new Error('Git URL 不能为空')
  const identity = deriveSkillRepositoryIdentity(url)
  const existing = form.id ? skillRepositoryRows.value.find((repo) => Number(repo.id) === Number(form.id)) : null
  return {
    resource_kind: 'skill',
    resource_key: identity.resourceKey,
    name: identity.name,
    description: form.description,
    version: 'latest',
    status: form.status,
    spec: {
      ...(existing?.spec || {}),
      resource_subtype: 'skill_repository',
      repository: {
        url,
        branch: String(form.branch || 'main').trim() || 'main',
        subdirectory: String(form.subdirectory || '.').trim() || '.'
      }
    },
    endpoint: {},
    secret_ref: {},
    tags: uniqueStrings([...(existing?.tags || []), 'skill-repository'])
  }
}

function deriveSkillRepositoryIdentity(url) {
  const raw = String(url || '').trim()
  let path = raw
  const scpLikeMatch = raw.match(/^[^@\s]+@[^:\s]+:(.+)$/)
  if (scpLikeMatch) {
    path = scpLikeMatch[1]
  } else {
    try {
      const parsed = new URL(raw)
      path = parsed.protocol === 'file:' ? parsed.pathname : parsed.pathname
    } catch {
      path = raw
    }
  }
  const cleanPath = decodeURIComponent(path)
    .replace(/[?#].*$/, '')
    .replace(/\/+$/, '')
    .replace(/\.git$/i, '')
  const parts = cleanPath.split('/').map((part) => part.trim()).filter(Boolean)
  const name = parts.length >= 2 ? parts.slice(-2).join('/') : parts[0] || ''
  if (!name) throw new Error('无法从 Git URL 识别仓库名称')
  return {
    name,
    resourceKey: name
  }
}

function buildImportedSkillPayload(repository, skill) {
  return {
    resource_kind: 'skill',
    resource_key: skill.key,
    name: skill.name || skill.key,
    description: skill.description || '',
    version: skill.version || 'latest',
    status: 'active',
    spec: {
      resource_subtype: 'skill',
      source_repository: repository.resource_key,
      path: skill.path || '',
      entry: skill.entry || '',
      manifest: skill.manifest || {},
      content: skill.content || '',
      instructions: skill.content || ''
    },
    endpoint: {},
    secret_ref: {},
    tags: uniqueStrings(['skill', repository.resource_key, ...(skill.tags || [])])
  }
}

function buildInference(inference) {
  return {
    temperature: numberOrDefault(inference.temperature, 0.2),
    max_tokens: numberOrDefault(inference.max_tokens, 4096),
    thinking_level: inference.thinking_level || 'medium',
    fallback: compactObject({
      enabled: inference.fallback?.enabled === true,
      model_id: inference.fallback?.enabled ? inference.fallback?.model_id : ''
    }, { keepFalse: true })
  }
}

function appendResourceRef(field, resourceType) {
  profileForm[field].push(createResourceRef({ resource_type: resourceType }))
}

function removeResourceRef(field, index) {
  profileForm[field].splice(index, 1)
}

function normalizeResourceRefs(items, resourceType) {
  if (!Array.isArray(items)) return []
  return items.map((item) => createResourceRef(item, resourceType))
}

function createResourceRef(item = {}, fallbackResourceType = 'resource') {
  const row = createPinnedResourceRefRow(item, fallbackResourceType, `resource-${++localRowSequence}`)
  if (fallbackResourceType === 'subagent_profile' || row.resource_type === 'subagent_profile') {
    const config = parseObjectLoose(row.configJSON)
    const hasMode = Object.prototype.hasOwnProperty.call(config, 'mode')
    const mode = hasMode ? subagentModeFromConfig(config) : 'write'
    row.subagentMode = mode
    row.configJSON = stringifyJSON(mergeSubagentConfigWithMode(config, mode))
  }
  return row
}

function buildResourceRefs(rows, label, fallbackResourceType) {
  return buildPinnedResourceRefs(rows, {
    label,
    fallbackResourceType,
    parseConfig: (value) => parseJSONObject(value, `${label} 配置`)
  })
}

function resourceGroupRows(group) {
  return Array.isArray(resources.value?.[group]) ? resources.value[group] : []
}

function resourceOptions(group) {
  const items = group === 'skills' ? importedSkillRows.value : resourceGroupRows(group)
  return items.map((item) => ({
    label: item.name || item.label || item.id || item.resource_id,
    value: item.id ?? item.resource_id ?? item.resource_key ?? item.name ?? item.label,
    version: item.version || item.resource_version
  })).filter((item) => hasValue(item.value))
}

function clearVersionCache() {
  Object.keys(versionCache).forEach((key) => {
    delete versionCache[key]
  })
}

function versionCacheEntry(row, field) {
  const key = versionCacheKey(row, field)
  if (!key) return null
  if (!versionCache[key]) {
    versionCache[key] = {
      loading: false,
      loaded: false,
      error: '',
      requestSeq: 0,
      versions: [],
      pending: null
    }
  }
  return versionCache[key]
}

function versionCacheKey(row, field) {
  const id = versionTargetId(row, field)
  if (!hasValue(id)) return ''
  return `${field}:${id}`
}

function versionTargetId(row, field) {
  if (field === 'subagents') return row.resource_id
  const resource = resolveResourceForRef(row, field)
  return resource?.id || row.resource_id
}

function resolveResourceForRef(row, field) {
  const items = field === 'skills' ? importedSkillRows.value : resourceGroupRows(field)
  return findByReference(items, row.resource_id)
}

function isVersionLoading(row, field) {
  return versionCacheEntry(row, field)?.loading === true
}

function versionVerificationMessage(row, field) {
  if (!hasValue(row?.resource_version_id) || isBuiltinEasyDoRef(row)) return ''
  const entry = versionCacheEntry(row, field)
  if (entry?.error) return `版本校验失败：${entry.error}`
  if (!entry?.loaded) return ''
  const selected = (entry.versions || []).find((version) => String(
    version.resource_version_id || version.profile_version_id || version.id
  ) === String(row.resource_version_id))
  if (!selected) return '版本不可用：已保留原固定值，请重新选择有效版本'
  const selectedDigest = String(selected.snapshot_digest || selected.snapshot_hash || '').trim()
  if (row.snapshot_digest && selectedDigest && row.snapshot_digest !== selectedDigest) {
    return '固定版本 digest 不一致，请重新选择版本'
  }
  const selectedVersion = String(selected.source_version || selected.resource_version || selected.version || '').trim()
  if (row.resource_version && selectedVersion && row.resource_version !== selectedVersion) {
    return '固定版本号不一致，请重新选择版本'
  }
  return ''
}

function versionOptionsForRow(row, field) {
  if (isBuiltinEasyDoRef(row)) {
    return [{
      value: '',
      label: 'builtin-v1',
      digest: BUILTIN_EASYDO_MCP_DIGEST,
      raw: {
        resource_version: 'builtin-v1',
        snapshot_digest: BUILTIN_EASYDO_MCP_DIGEST
      }
    }]
  }
  const entry = versionCacheEntry(row, field)
  const options = (entry?.versions || []).map((version) => {
    const value = version.resource_version_id || version.profile_version_id || version.id
    const versionText = version.source_version || version.resource_version || version.version || `#${value}`
    const revision = version.revision ? `r${version.revision}` : `v${versionText}`
    const digest = version.snapshot_digest || version.snapshot_hash || ''
    return {
      value,
      label: `${revision} · ${shortDigest(digest)}`,
      digest,
      raw: version
    }
  }).filter((option) => hasValue(option.value))
  const selectedID = row?.resource_version_id
  const selectedMissing = hasValue(selectedID) && !options.some((option) => String(option.value) === String(selectedID))
  if (selectedMissing && (entry?.loaded || entry?.error)) {
    options.unshift({
      value: selectedID,
      label: entry?.error
        ? `版本校验失败 · ${row.resource_version || `#${selectedID}`}`
        : `版本不可用 · ${row.resource_version || `#${selectedID}`}`,
      digest: row.snapshot_digest || '',
      unavailable: true,
      raw: null
    })
  }
  return options
}

async function ensureVersionOptions(row, field, options = {}) {
  if (!hasValue(row?.resource_id)) return []
  if (isBuiltinEasyDoRef(row)) {
    row.resource_version_id = ''
    row.resource_version = 'builtin-v1'
    row.snapshot_digest = BUILTIN_EASYDO_MCP_DIGEST
    return versionOptionsForRow(row, field)
  }
  const entry = versionCacheEntry(row, field)
  if (!entry) return []
  if (entry.loading && entry.pending) return entry.pending
  if (options.force !== true && entry.loaded) return versionOptionsForRow(row, field)
  const sequence = ++entry.requestSeq
  entry.loading = true
  entry.loaded = false
  entry.error = ''
  entry.versions = []
  entry.pending = (async () => {
    try {
      const id = versionTargetId(row, field)
      const response = field === 'subagents'
        ? await listAgentProfileVersions(id)
        : await listAgentResourceVersions(id)
      if (sequence !== entry.requestSeq) return versionOptionsForRow(row, field)
      const versions = extractArray(response?.data)
      entry.versions = field === 'subagents'
        ? versions.filter((version) => String(version.status || 'published') === 'published')
        : versions
      entry.loaded = true
      return versionOptionsForRow(row, field)
    } catch (error) {
      if (sequence === entry.requestSeq) {
        entry.error = errorMessage(error, '加载版本失败')
        if (options.silent !== true) ElMessage.error(entry.error)
      }
      return []
    } finally {
      if (sequence === entry.requestSeq) {
        entry.loading = false
        entry.pending = null
      }
    }
  })()
  return entry.pending
}

async function prefetchProfileDependencyVersions(profileRows, options = {}) {
  const pendingByKey = new Map()
  for (const profile of Array.isArray(profileRows) ? profileRows : []) {
    for (const field of ['skills', 'mcp_servers', 'subagents']) {
      for (const row of Array.isArray(profile?.[field]) ? profile[field] : []) {
        if (!hasValue(row?.resource_id) || isBuiltinEasyDoRef(row)) continue
        const key = versionCacheKey(row, field)
        if (!key || pendingByKey.has(key)) continue
        pendingByKey.set(key, ensureVersionOptions(row, field, { force: true, silent: true, ...options }))
      }
    }
  }
  await Promise.all(pendingByKey.values())
}

async function ensureProfileVersionOptions(profile, options = {}) {
  await prefetchProfileDependencyVersions([profile], options)
}

function blockingProfileHealthIssues(profile) {
  return evaluateProfileResourceHealthIssues(profile, {
    ...profileHealthContext(),
    validationResult: null
  }).filter((issue) => issue.severity === 'invalid')
}

async function handleResourceRefChange(field, index) {
  const row = profileForm[field]?.[index]
  if (!row) return
  row.resource_version_id = ''
  row.resource_version = ''
  row.snapshot_digest = ''
  if (!hasValue(row.resource_id)) return
  if (field === 'mcp_servers') {
    mergeResourceToolPermissionsIntoRefRow(row, resolveResourceForRef(row, field))
  }
  const options = await ensureVersionOptions(row, field)
  const latest = options[0]
  if (latest?.raw) {
    applyVersionPinToRow(row, latest.raw)
  }
}

function handleResourceVersionChange(field, index) {
  const row = profileForm[field]?.[index]
  if (!row) return
  const selected = versionOptionsForRow(row, field).find((option) => String(option.value) === String(row.resource_version_id))
  if (selected?.raw) applyVersionPinToRow(row, selected.raw)
}

function isBuiltinEasyDoRef(row = {}) {
  return String(row.resource_type || '').trim() === 'mcp_server' &&
    String(row.resource_id || '').trim().toLowerCase() === 'easydo'
}

function shortDigest(value) {
  const digest = String(value || '').trim()
  if (!digest) return 'no digest'
  return digest.length > 18 ? `${digest.slice(0, 15)}...` : digest
}

function modelText(profile) {
  return profile.model?.provider_model_key || profile.model?.model_id || '-'
}

function dependencyRowsForRefs(profile, field, fromMeta, resolveRef) {
  const refs = Array.isArray(profile?.[field]) ? profile[field] : []
  return refs
    .filter((ref) => hasValue(ref?.resource_id))
    .map((ref, index) => {
      const resolved = resolveRef(ref)
      return {
        key: `${profile.id || profile.name}-${field}-${ref.resource_id}-${index}`,
        from: profile.name || `Profile ${profile.id || '-'}`,
        fromMeta,
        to: resolved.label,
        toMeta: resolved.meta,
        type: resolved.exists ? 'success' : 'warning'
      }
    })
}

function resolveProfileRef(ref) {
  const profile = findByReference(profiles.value, ref.resource_id)
  return {
    label: profile?.name || String(ref.resource_id),
    meta: profileContextTags(profile).join(', ') || 'subagent profile',
    exists: Boolean(profile)
  }
}

function resolveMcpServerRef(ref) {
  const server = findByReference(mcpServerRows.value, ref.resource_id)
  return {
    label: server?.name || String(ref.resource_id),
    meta: `${server ? discoveredMcpTools(server).length : 0} tools`,
    exists: Boolean(server)
  }
}

function resolveSkillRef(ref) {
  const skill = findByReference(importedSkillRows.value, ref.resource_id)
  return {
    label: skill?.name || String(ref.resource_id),
    meta: skill?.version || ref.resource_version || 'latest',
    exists: Boolean(skill)
  }
}

function findByReference(items, reference) {
  return items.find((item) => [
    item?.resource_key,
    item?.resource_id,
    item?.id,
    item?.name
  ].some((value) => hasValue(value) && String(value) === String(reference)))
}

function isSkillRepository(resource) {
  return resource?.spec?.resource_subtype === 'skill_repository' || normalizeStringArray(resource?.tags).includes('skill-repository')
}

function isImportedSkill(resource) {
  return resource?.resource_kind === 'skill' && !isSkillRepository(resource)
}

function isSkillImported(skillKey) {
  return importedSkillRows.value.some((resource) => String(resource.resource_key) === String(skillKey))
}

function skillRepositorySpec(resource = {}) {
  const repository = resource.spec?.repository || resource.endpoint || {}
  return {
    url: repository.url || resource.spec?.url || '',
    branch: repository.branch || resource.spec?.branch || 'main',
    subdirectory: repository.subdirectory || resource.spec?.subdirectory || 'skills'
  }
}

function repositoryDiscoveredSkills(repository = {}) {
  const raw = repository.spec?.discovered_skills || repository.spec?.skills || []
  return normalizeDiscoveredSkills(raw, repository)
}

function normalizeDiscoveredSkills(items, repository = {}) {
  if (!Array.isArray(items)) return []
  return items
    .filter((item) => item && typeof item === 'object')
    .map((item) => {
      const key = String(item.key || item.name || item.path || '').trim()
      return {
        key,
        name: String(item.name || key).trim(),
        description: String(item.description || item.desc || ''),
        version: String(item.version || 'latest'),
        path: String(item.path || ''),
        entry: String(item.entry || ''),
        content: String(item.content || item.markdown || item.instructions || item.prompt || ''),
        tags: normalizeStringArray(item.tags),
        manifest: item.manifest && typeof item.manifest === 'object' ? item.manifest : item,
        source_repository: repository.resource_key || ''
      }
    })
    .filter((skill) => skill.key)
}

function firstMcpServerName(resource = {}) {
  const servers = resource.spec?.mcpServers
  if (!servers || typeof servers !== 'object' || Array.isArray(servers)) return ''
  return Object.keys(servers)[0] || ''
}

function mcpServerConfig(resource = {}) {
  const serverName = firstMcpServerName(resource)
  const servers = resource.spec?.mcpServers
  if (!serverName || !servers || typeof servers !== 'object') return {}
  return servers[serverName] || {}
}

function isBuiltinEasyDoMcpServer(resource = {}) {
  return resource?.builtin === true ||
    resource?.readonly === true ||
    resource?.spec?.builtin === true ||
    resource?.spec?.readonly === true ||
    String(resource?.resource_key || resource?.resource_id || resource?.name || '').trim().toLowerCase() === 'easydo'
}

function mcpServerEndpointText(resource = {}) {
  const config = mcpServerConfig(resource)
  return config.url || config.command || '-'
}

function discoveredMcpTools(resource = {}) {
  return normalizeMcpTools(resource.spec?.discovered_tools || resource.spec?.tools || [])
}

function normalizeMcpTools(raw) {
  let value = raw
  if (typeof raw === 'string') {
    try {
      value = JSON.parse(raw || '[]')
    } catch {
      value = []
    }
  }
  if (!Array.isArray(value)) return []
  return value
    .map((tool) => {
      if (typeof tool === 'string') return { name: tool }
      if (tool && typeof tool === 'object') return tool
      return null
    })
    .filter(Boolean)
}

function extractArray(payload) {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.items)) return payload.items
  return []
}

function normalizeStringArray(value, fallback = []) {
  if (Array.isArray(value)) {
    const normalized = value.map((item) => String(item).trim()).filter(Boolean)
    return normalized.length ? normalized : fallback
  }
  if (typeof value === 'string') {
    const normalized = value.split(',').map((item) => item.trim()).filter(Boolean)
    return normalized.length ? normalized : fallback
  }
  return fallback
}

function uniqueStrings(items) {
  return [...new Set(normalizeStringArray(items))]
}

function compactObject(value, options = {}) {
  return Object.entries(value || {}).reduce((result, [key, raw]) => {
    if (raw === undefined || raw === null) return result
    if (typeof raw === 'string' && raw.trim() === '') return result
    if (raw === false && !options.keepFalse) return result
    result[key] = typeof raw === 'string' ? raw.trim() : raw
    return result
  }, {})
}

function numberOrDefault(value, fallback) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function hasValue(value) {
  return value !== undefined && value !== null && String(value).trim() !== ''
}

function stringifyJSON(value) {
  return JSON.stringify(value, null, 2)
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

function parseJSONObject(raw, label) {
  const value = parseJSON(raw, label)
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${label} 必须是 JSON object`)
  }
  return value
}

function parseJSONArray(raw, label) {
  const value = parseJSON(raw || '[]', label)
  if (!Array.isArray(value)) {
    throw new Error(`${label} 必须是 JSON array`)
  }
  return value
}

function parseJSON(raw, label) {
  try {
    return JSON.parse(String(raw || '').trim() || '{}')
  } catch {
    throw new Error(`${label} JSON 格式无效`)
  }
}

function errorMessage(error, fallback = '请求失败') {
  return error?.response?.data?.message || error?.response?.data?.error || error?.message || fallback
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.ai-agent-store-page {
  padding-top: 0;
}

.ai-agent-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: start;
}

.agent-page {
  min-width: 0;
}

.agent-toolbar {
  min-height: 46px;
  margin-bottom: 14px;
  align-items: center;
}

.agent-toolbar-start {
  gap: 14px;
  overflow: hidden;
}

.agent-toolbar-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.agent-mode-switch {
  display: inline-flex;
  max-width: 100%;
  padding: 4px;
  gap: 2px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-card);
  box-shadow: var(--shadow-sm);
  overflow-x: auto;
}

.agent-mode-button {
  min-height: 34px;
  white-space: nowrap;
  border: 0;
  border-radius: calc($radius-md - 2px);
  padding: 7px 14px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  transition: background 0.18s ease, color 0.18s ease, box-shadow 0.18s ease;
}

.agent-mode-button:hover {
  background: var(--bg-elevated);
  color: var(--text-primary);
}

.agent-mode-button.active {
  background: var(--primary-color);
  color: #fff;
  box-shadow: 0 8px 18px rgba(37, 99, 235, 0.16);
}

.agent-view {
  display: none;
  animation: agentViewIn 140ms ease-out;
}

.agent-view.active {
  display: block;
}

@keyframes agentViewIn {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.agent-workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  gap: 14px;
  align-items: start;
}

.agent-workspace--single {
  grid-template-columns: minmax(0, 1fr);
}

.agent-panel {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-lg;
  background: var(--bg-card);
  box-shadow: var(--shadow-sm);
}

.agent-panel-header {
  min-height: 58px;
  padding: 13px 14px;
  border-bottom: 1px solid var(--border-color-light);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;

  h1,
  h2,
  h3 {
    margin: 0;
    color: var(--text-primary);
    letter-spacing: 0;
  }

  h1 {
    font-size: 22px;
    line-height: 1.1;
  }

  h2,
  h3 {
    font-size: 16px;
  }

  p {
    margin: 3px 0 0;
    color: var(--text-tertiary);
    font-size: 12px;
    line-height: 1.45;
  }
}

.agent-panel-body {
  padding: 14px;
}

.agent-filters {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) 160px auto;
  gap: 8px;
  margin-bottom: 12px;
  align-items: center;
}

.agent-filters--three {
  grid-template-columns: minmax(220px, 1fr) 160px 160px;
}

.agent-side-stack {
  display: grid;
  gap: 14px;
}

.health-list {
  display: grid;
  gap: 10px;
}

.validation-result {
  margin-bottom: 12px;
  display: grid;
  gap: 8px;
}

.validation-result__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.validation-result__issue {
  padding-left: 10px;
  border-left: 3px solid $warning-color;
  display: grid;
  gap: 2px;
  font-size: 12px;

  strong {
    color: var(--text-primary);
  }

  span {
    color: var(--text-secondary);
    line-height: 1.4;
  }
}

.validation-result__issue--error {
  border-left-color: $danger-color;
}

.health-item {
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  padding: 10px;
  background: var(--bg-elevated);
  display: grid;
  gap: 6px;

  p {
    margin: 0;
    color: var(--text-tertiary);
    font-size: 12px;
    line-height: 1.45;
  }
}

.health-item__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.dependency-graph {
  display: grid;
  gap: 8px;
}

.graph-row {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  gap: 8px;
  align-items: center;
}

.graph-node {
  min-height: 48px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  padding: 10px;
  background: var(--bg-elevated);
}

.graph-arrow {
  color: var(--text-tertiary);
  font-weight: 700;
}

.agent-profile-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
  overflow-x: hidden;
}

.agent-resource-tabs {
  :deep(.el-tabs__header) {
    display: none;
  }

  :deep(.el-tabs__nav-wrap::after) {
    display: none;
  }

  :deep(.el-tabs__active-bar) {
    display: none;
  }

  :deep(.el-tabs__item) {
    height: 34px;
    padding: 0 14px;
    border-radius: $radius-md;
    color: var(--text-secondary);
    font-weight: 600;
  }

  :deep(.el-tabs__item.is-active) {
    background: var(--primary-color);
    color: #fff;
    box-shadow: 0 8px 18px rgba(37, 99, 235, 0.18);
  }

  :deep(.el-tabs__content) {
    overflow: visible;
  }
}

.agent-resource-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.profile-name-cell,
.reserved-profile-name-field {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;

  span:first-child {
    min-width: 0;
    overflow-wrap: anywhere;
  }
}

.profile-health-trigger {
  max-width: 100%;
  border: 0;
  padding: 0;
  background: transparent;
  cursor: pointer;
  line-height: 1;
}

.reserved-profile-name-field {
  align-items: stretch;

  .el-input {
    min-width: 0;
  }
}

.resource-pane-toolbar {
  min-height: 54px;
  margin-bottom: 12px;
  padding: 14px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-lg;
  background:
    linear-gradient(135deg, rgba(37, 99, 235, 0.08), rgba(20, 184, 166, 0.06)),
    var(--bg-card);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;

  h3 {
    margin: 0;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 700;
  }

  p {
    margin: 5px 0 0;
    color: var(--text-secondary);
    font-size: 13px;
    line-height: 1.45;
  }
}

.resource-pane-toolbar--compact {
  background: var(--bg-card);
}

.resource-pane-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.resource-subsection {
  padding: 14px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-lg;
  background: var(--bg-card);

  & + & {
    margin-top: 12px;
  }
}

.skills-workspace {
  display: grid;
  gap: 12px;
}

.skill-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 10px;
}

.skill-scan-dialog__body {
  display: grid;
  gap: 12px;
}

.skill-scan-summary {
  min-height: 64px;
  padding: 12px 14px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-lg;
  background: var(--bg-elevated);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;

  strong {
    color: var(--text-primary);
    font-size: 15px;
  }

  p {
    margin: 4px 0 0;
    color: var(--text-tertiary);
    font-size: 12px;
    line-height: 1.4;
    word-break: break-all;
  }
}

.agent-filters--dialog {
  grid-template-columns: minmax(220px, 1fr);
  margin-bottom: 0;
}

.skill-scan-dialog__footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.skill-import-card {
  min-height: 112px;
  padding: 12px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-lg;
  background: var(--bg-elevated);
  cursor: pointer;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.18s ease;

  &:hover {
    border-color: rgba(37, 99, 235, 0.35);
    box-shadow: 0 10px 24px rgba(15, 23, 42, 0.08);
    transform: translateY(-1px);
  }
}

.skill-import-card__main {
  min-width: 0;
  display: grid;
  gap: 6px;

  strong {
    color: var(--text-primary);
    font-size: 14px;
    font-weight: 700;
  }

  > span:not(.skill-import-card__meta) {
    color: var(--text-secondary);
    font-size: 13px;
    line-height: 1.45;
  }
}

.skill-import-card__meta,
.tool-chip-list {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.tool-permission-editor {
  display: grid;
  gap: 10px;
}

.tool-permission-toolbar {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) auto;
  gap: 10px;
  align-items: center;
}

.tool-permission-bulk {
  white-space: nowrap;
}

.tool-permission-list {
  display: grid;
  gap: 6px;
  max-height: 360px;
  overflow: auto;
  padding-right: 4px;
}

.tool-permission-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 96px auto;
  gap: 10px;
  align-items: center;
  min-height: 46px;
  padding: 8px 10px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fff;
}

.tool-permission-row__main {
  min-width: 0;
  display: grid;
  gap: 2px;
}

.tool-permission-row__main strong,
.tool-permission-row__main span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tool-permission-row__main strong {
  color: #111827;
  font-size: 13px;
}

.tool-permission-row__main span {
  color: #6b7280;
  font-size: 12px;
}

.tool-permission-row__operation {
  justify-self: start;
  max-width: 96px;
}

.tool-permission-row__decision {
  justify-self: end;
}

.resource-dialog-actions {
  margin: 4px 0 12px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.mcp-server-name-cell {
  min-width: 0;
  display: grid;
  grid-template-columns: auto auto;
  justify-content: start;
  align-items: center;
  gap: 4px 8px;

  strong {
    min-width: 0;
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.3;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  span:not(.el-tag) {
    grid-column: 1 / -1;
    color: var(--text-tertiary);
    font-size: 12px;
    line-height: 1.35;
  }
}

.code-preview {
  max-height: 260px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  border: 1px solid rgba(15, 23, 42, 0.12);
  border-radius: $radius-md;
  background: #0f172a;
  color: #dbeafe;
  font-family: 'JetBrains Mono', 'Fira Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
}

.workspace-tools-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}

.workspace-tool-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-page);
}

.workspace-tool-item__name {
  margin-right: 8px;
  font-weight: 600;
}

.workspace-tool-item__header {
  min-width: 0;
}

.workspace-tool-item__desc {
  color: var(--text-tertiary);
  font-size: 12px;
  line-height: 1.4;
}

.workspace-tool-item__permission {
  margin-top: 2px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
}

.profile-form-section {
  border: 1px solid var(--border-color-light);
  border-radius: $radius-lg;
  padding: 14px;
  background: var(--bg-card);
}

.profile-form-section__title {
  min-height: 32px;
  margin-bottom: 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;

  h3 {
    margin: 0;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 600;
  }
}

.profile-form-grid {
  display: grid;
  column-gap: 14px;
  row-gap: 4px;
}

.profile-form-grid--two {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.profile-form-grid--three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.profile-form-grid--four {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.resource-empty {
  border: 1px dashed var(--border-color-light);
  border-radius: $radius-md;
  padding: 12px;
  color: var(--text-tertiary);
  background: var(--bg-elevated);
  font-size: 13px;
}

.resource-row {
  position: relative;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  padding: 12px;
  background: var(--bg-elevated);

  & + & {
    margin-top: 10px;
  }
}

.resource-row {
  display: grid;
  grid-template-columns: 130px minmax(140px, 1fr) 130px 80px minmax(160px, 1fr) 64px;
  gap: 8px;
  align-items: start;
}

.resource-row__config {
  min-width: 0;
}

.resource-row__remove {
  margin-top: 28px;
}

.dependency-drawer {
  min-height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dependency-drawer__summary {
  min-height: 48px;
  padding: 10px 12px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-elevated);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.dependency-drawer__body {
  min-height: 160px;
  display: grid;
  align-content: start;
  gap: 8px;
}

.dependency-drawer__section {
  display: grid;
  gap: 8px;

  h4 {
    margin: 0;
    color: var(--text-primary);
    font-size: 13px;
  }
}

.dependency-item {
  padding: 10px 12px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-card);
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;

  div {
    min-width: 0;
    display: grid;
    gap: 3px;
  }

  strong {
    color: var(--text-primary);
    font-size: 13px;
  }

  span {
    min-width: 0;
    color: var(--text-secondary);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
}

.dependency-item--health {
  border-color: rgba(245, 158, 11, 0.28);
}

.dependency-drawer__footer {
  margin-top: auto;
  padding-top: 12px;
  border-top: 1px solid var(--border-color-light);
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.field-tip {
  margin: 6px 0 0;
  color: $warning-color;
  font-size: 12px;
  line-height: 1.4;
}

.field-tip--info {
  color: var(--text-secondary);
}

@media (max-width: 1180px) {
  .ai-agent-layout,
  .agent-workspace {
    grid-template-columns: 1fr;
  }

  .agent-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .profile-form-grid--three,
  .profile-form-grid--four,
  .resource-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .agent-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .agent-toolbar-start,
  .agent-toolbar-actions {
    width: 100%;
  }

  .agent-toolbar-start {
    align-items: stretch;
    flex-direction: column;
  }

  .agent-filters,
  .agent-filters--three {
    grid-template-columns: 1fr;
  }

  .agent-mode-switch {
    display: flex;
    width: 100%;
  }

  .agent-mode-button {
    flex: 1 0 auto;
  }

  .profile-form-grid--two,
  .profile-form-grid--three,
  .profile-form-grid--four,
  .resource-row {
    grid-template-columns: 1fr;
  }

  .skill-scan-summary {
    align-items: flex-start;
    flex-direction: column;
  }

  .resource-row__remove {
    margin-top: 0;
    justify-self: start;
  }

  .tool-permission-toolbar,
  .tool-permission-row {
    grid-template-columns: 1fr;
  }

  .tool-permission-bulk {
    overflow-x: auto;
  }

  .tool-permission-row {
    align-items: stretch;
  }

  .tool-permission-row__operation,
  .tool-permission-row__decision {
    justify-self: start;
  }

  .tool-permission-row__main span {
    white-space: normal;
  }
}
</style>
