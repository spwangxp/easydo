import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('ai agent store page manages agent profiles and resources', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /StoreKindSwitch/)
  assert.match(source, /storeKind\s*=\s*computed\(\(\)\s*=>\s*'ai-agent'\)/)
  assert.match(source, /listAgentProfiles/)
  assert.match(source, /createAgentProfile/)
  assert.match(source, /publishAgentProfile/)
  assert.match(source, /validateAgentProfile/)
  assert.match(source, /listAgentResources/)
  assert.match(source, /profileForm\.skills/)
  assert.match(source, /profileForm\.subagents/)
  assert.match(source, /profileForm\.mcp_servers/)
  assert.doesNotMatch(source, /saveSceneBindings/)
  assert.doesNotMatch(source, /runtime_profile_id/)
  assert.doesNotMatch(source, /pipeline-task/)
})

test('ai agent profile form does not expose or persist model adapter', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.doesNotMatch(source, /model_adapter/)
  assert.doesNotMatch(source, /modelAdapter/)
  assert.doesNotMatch(source, /Model Adapter/)
})

test('ai agent profile snapshots carry provider runtime endpoint and header metadata', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /providerRuntimeSettings/)
  assert.match(source, /profileForm\.provider\.llm_endpoint/)
  assert.match(source, /profileForm\.provider\.settings_json/)
  assert.match(source, /profileForm\.provider\.headers_json/)
  assert.match(source, /settings\.llm_endpoint/)
  assert.match(source, /provider\.headers_json/)
})

test('ai agent store waits for workspace context before querying runtime facade', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /useUserStore/)
  assert.match(source, /resolveWorkspaceId/)
  assert.match(source, /userStore\.getUserInfoAction\(\)/)
  assert.match(source, /watch\(\(\) => userStore\.currentWorkspaceId/)
  assert.match(source, /if \(!workspaceId\)/)
})

test('ai agent store provides structured editors for profile resources without scene bindings', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /profile-form-section/)
  assert.match(source, /appendResourceRef/)
  assert.match(source, /profileForm\.skills/)
  assert.match(source, /profileForm\.mcp_servers/)
  assert.match(source, /profileForm\.subagents/)
  assert.match(source, /createAgentResource/)
  assert.match(source, /updateAgentResource/)
  assert.match(source, /deleteAgentResource/)
  assert.match(source, /Promise\.allSettled/)
  assert.match(source, /loadErrors/)
  assert.doesNotMatch(source, /resourceKindConfigs/)
  assert.doesNotMatch(source, /resourceKindTab/)
  assert.doesNotMatch(source, /class="agent-resource-tabs"/)
  assert.doesNotMatch(source, /bindingRows/)
  assert.doesNotMatch(source, /appendSceneBinding/)
  assert.doesNotMatch(source, /profileVersionOptions/)
  assert.doesNotMatch(source, /agent-resource-strip/)
  assert.doesNotMatch(source, /skillsJSON/)
  assert.doesNotMatch(source, /subagentsJSON/)
  assert.doesNotMatch(source, /profileForm\.mcpServersJSON/)
})

test('ai agent resources are limited to mcp servers, skills repositories, and profile references', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /mcpServerRows/)
  assert.match(source, /skillRepositoryRows/)
  assert.match(source, /importedSkillRows/)
  assert.doesNotMatch(source, /name:\s*'agent_profiles'/)
  assert.doesNotMatch(source, /name:\s*'subagents'/)
  assert.doesNotMatch(source, /label:\s*'Subagents'/)
  assert.doesNotMatch(source, /resource_kind\s*===\s*'credential'/)
  assert.doesNotMatch(source, /Provider Credential/)
  assert.doesNotMatch(source, /Context Providers/)
  assert.doesNotMatch(source, /Memory Stores/)
  assert.doesNotMatch(source, /Credentials/)
})

test('mcp server resource editor exposes core MCP transports and lifecycle fields', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /mcpServerForm/)
  assert.match(source, /mcpServers/)
  assert.match(source, /mcp_server/)
  assert.match(source, /mcpForm\.type/)
  assert.match(source, /streamable_http/)
  assert.match(source, /<el-option label="stdio"/)
  assert.match(source, /<el-option label="sse"/)
  assert.doesNotMatch(source, /<el-option label="http"/)
  assert.doesNotMatch(source, /<el-option label="websocket"/)
  assert.match(source, /v-model="mcpForm\.command"/)
  assert.match(source, /v-model="mcpForm\.argsJSON"/)
  assert.match(source, /v-model="mcpForm\.envJSON"/)
  assert.match(source, /mcpForm\.url/)
  assert.match(source, /mcpForm\.headersJSON/)
  assert.match(source, /v-model="mcpForm\.cwd"/)
  assert.match(source, /mcpForm\.timeout/)
  assert.match(source, /timeout_ms/)
  assert.match(source, /mcpForm\.disabled/)
  assert.match(source, /mcpConfigPreview/)
  assert.match(source, /testMcpConnection/)
  assert.match(source, /discoverMcpTools/)
  assert.match(source, /probeMcpResource/)
  assert.match(source, /buildMcpProbeConfigFromForm/)
  assert.match(source, /await scanAgentResource\(mcpForm\.id\)/)
  assert.match(source, /mcpForm\.mcpServersJSON/)
  assert.match(source, /mcpServerConfigsFromForm/)
  assert.match(source, /normalizeMcpServerConfig/)
  assert.match(source, /secret_ref/)
  assert.match(source, /rulesJSON/)
  assert.match(source, /defaultDecision/)
  assert.match(source, /result\?\.data\?\.discovered_tools/)
})

test('mcp server resource editor keeps secret placeholders resolvable without previewing raw values', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /secretRef\.values/)
  assert.match(source, /secretRef\.values\[refKey\]\s*=\s*text/)
  assert.match(source, /delete preview\.secret_ref\.values/)
  assert.match(source, /serverName\}\.headers/)
  assert.match(source, /serverName\}\.env/)
})

test('skills repositories can be added, scanned, selected, and imported without old skill operations', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /skillRepoForm/)
  assert.match(source, /scanAgentResource/)
  assert.match(source, /openSkillRepoDialog/)
  assert.match(source, /deleteSkillRepository/)
  assert.match(source, /async function scanSkillRepository/)
  assert.match(source, /importSelectedSkills/)
  assert.match(source, /selectedDiscoveredSkillKeys/)
  assert.match(source, /importedSkillOptions/)
  assert.match(source, /导入已选择/)
  assert.match(source, /content:\s*skill\.content/)
  assert.match(source, /instructions:\s*skill\.content/)
  assert.doesNotMatch(source, /未发现 Skills，请确认仓库 manifest 已同步到 runtime/)
  assert.doesNotMatch(source, /生成新版本/)
  assert.doesNotMatch(source, /配置参数/)
  assert.doesNotMatch(source, /安装/)
})

test('skills scan results are shown in a dialog instead of a page-side panel', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /skillScanDialog/)
  assert.match(source, /<el-dialog[\s\S]*v-model="skillScanDialog\.visible"[\s\S]*扫描结果/)
  assert.match(source, /class="skill-scan-dialog"/)
  assert.match(source, /class="skill-scan-dialog__footer"/)
  assert.doesNotMatch(source, /class="resource-split"/)
})

test('skill repository identity is derived from git url instead of manual name and key fields', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.doesNotMatch(source, /label="仓库名称"/)
  assert.doesNotMatch(source, /label="仓库 Key"/)
  assert.doesNotMatch(source, /skillRepoForm\.resource_key/)
  assert.match(source, /deriveSkillRepositoryIdentity/)
  assert.match(source, /skillRepoDerivedIdentity/)
  assert.match(source, /仓库名称：/)
  assert.match(source, /obra\/superpowers/)
  assert.match(source, /resource_key:\s*identity\.resourceKey/)
  assert.match(source, /name:\s*identity\.name/)
})

test('ai agent store resource panels include dedicated polished styling', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /\.agent-mode-switch\s*\{/)
  assert.match(source, /\.agent-mode-button\s*\{/)
  assert.match(source, /\.resource-pane-toolbar\s*\{/)
  assert.match(source, /\.resource-pane-actions\s*\{/)
  assert.match(source, /\.resource-subsection\s*\{/)
  assert.match(source, /\.skills-workspace\s*\{/)
  assert.match(source, /\.skill-import-card\s*\{/)
  assert.match(source, /\.code-preview\s*\{/)
  assert.match(source, /\.tool-chip-list\s*\{/)
})

test('ai agent store official layout follows the interaction demo workspace shell without local side menu', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /class="ai-agent-layout"/)
  assert.match(source, /agentModeItems/)
  assert.match(source, /data-agent-view="agent"/)
  assert.match(source, /:data-agent-view="activeAgentStoreView"/)
  assert.doesNotMatch(source, /class="agent-view-tabs"/)
  assert.doesNotMatch(source, /class="agent-view-tab"/)
  assert.doesNotMatch(source, /data-agent-view="profiles"/)
  assert.doesNotMatch(source, /data-agent-view="resources"/)
  assert.doesNotMatch(source, /data-agent-view="bindings"/)
  assert.doesNotMatch(source, /data-agent-view="settings"/)
  assert.match(source, /class="agent-view active"/)
  assert.match(source, /class="agent-workspace"/)
  assert.match(source, /class="agent-workspace agent-workspace--single"/)
  assert.match(source, /class="agent-side-stack"/)
  assert.match(source, /\.agent-panel\s*\{/)
  assert.match(source, /\.agent-panel-header\s*\{/)
  assert.match(source, /\.agent-filters\s*\{/)
  assert.doesNotMatch(source, /class="agent-local-sidebar"/)
  assert.doesNotMatch(source, /agent-local-nav-button/)
  assert.doesNotMatch(source, /agent-local-title/)
  assert.doesNotMatch(source, /agent-local-dot/)
  assert.doesNotMatch(source, /<section class="card-shell section-shell">/)
})

test('ai agent store top agent actions are workspace mode switches only', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /agentModeItems\s*=\s*\[/)
  assert.match(source, /label:\s*'agent'/)
  assert.match(source, /label:\s*'mcp'/)
  assert.match(source, /label:\s*'skills'/)
  assert.match(source, /label:\s*'tools'/)
  assert.doesNotMatch(source, /label:\s*'operations'/)
  assert.match(source, /data-agent-view="tools"/)
  assert.match(source, /workspaceTools/)
  assert.match(source, /subagentMode/)
  assert.doesNotMatch(source, />Profiles</)
  assert.doesNotMatch(source, />Resources</)
  assert.doesNotMatch(source, />Scene Bindings</)
  assert.doesNotMatch(source, />Workspace Settings</)
  assert.doesNotMatch(source, /新增资源/)
  assert.doesNotMatch(source, /场景绑定<\/el-button>/)
})

test('builtin easydo mcp server is visible but cannot be edited discovered or deleted', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /isBuiltinEasyDoMcpServer/)
  assert.match(source, /内置/)
  assert.match(source, /:disabled="isBuiltinEasyDoMcpServer\(row\)"/)
  assert.match(source, /openMcpServerDialog\(row\)[\s\S]*:disabled="isBuiltinEasyDoMcpServer\(row\)"/)
  assert.match(source, /openMcpServerDialog\(row, \{ discover: true \}\)[\s\S]*:disabled="isBuiltinEasyDoMcpServer\(row\)"/)
  assert.match(source, /handleDeleteResource\(row\)[\s\S]*:disabled="isBuiltinEasyDoMcpServer\(row\)"/)
  assert.match(source, /这是每个用户在当前工作空间的内置 easydo MCP Server/)
})

test('ai agent profile dependency graph renders only real resource references', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /dependencyRowsForRefs/)
  assert.match(source, /resolveProfileRef/)
  assert.match(source, /resolveMcpServerRef/)
  assert.match(source, /resolveSkillRef/)
  assert.match(source, /暂无 Profile 依赖关系/)
  assert.doesNotMatch(source, /Pipeline Reviewer/)
  assert.doesNotMatch(source, /EasyDo MCP/)
  assert.doesNotMatch(source, /Failure Analysis/)
})

test('ai agent store does not render load failure alert blocks in the page', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /Promise\.allSettled/)
  assert.match(source, /loadErrors/)
  assert.doesNotMatch(source, /v-if="loadErrors\.length"/)
  assert.doesNotMatch(source, /class="agent-load-errors"/)
  assert.doesNotMatch(source, /<el-alert[\s\S]*:title="error\.message"/)
})

test('ai agent profile model selection is provider first from ai store supply relationships', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /listAIProviders/)
  assert.match(source, /aiProviderOptions/)
  assert.match(source, /providerFirstModelOptions/)
  assert.match(source, /handleProfileProviderChange/)
  assert.match(source, /handleProfileModelProviderChange/)
  assert.match(source, /profileForm\.model_provider_id/)
  assert.doesNotMatch(source, /label="Provider ID" required>[\s\S]*?<el-input v-model="profileForm\.provider\.provider_id"/)
  assert.match(source, /label="Model ID" required>[\s\S]*?<el-input v-model="profileForm\.model\.model_id" disabled/)
})

test('ai agent store treats context tags as profile context injection metadata', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /context_tags/)
  assert.match(source, /profileForm\.context_tags/)
  assert.match(source, /label="上下文标签"/)
  assert.match(source, /contextTagOptions/)
  assert.match(source, /pageAssistantContextTag\s*=\s*'page-assistant'/)
  assert.match(source, /userStore\.currentWorkspaceRole/)
  assert.doesNotMatch(source, /supported_scene_types/)
  assert.doesNotMatch(source, /businessSceneTypeOptions/)
  assert.doesNotMatch(source, /availableSceneTypeOptions/)
  assert.doesNotMatch(source, /listSceneBindings/)
  assert.doesNotMatch(source, /saveSceneBindings/)
  assert.doesNotMatch(source, /sceneBindings/)
  assert.doesNotMatch(source, /bindingRows/)
  assert.doesNotMatch(source, /bindingDialog/)
})

test('ai agent store opens chatbox in a new browser tab for active explicit profiles', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /openAgentChatboxSession/)
  assert.match(source, /canOpenChatboxProfile/)
  assert.match(source, /handleOpenChatbox/)
  assert.match(source, /打开对话/)
  assert.match(source, /window\.open\(url,\s*'_blank',\s*'noopener'\)/)
  assert.match(source, /status === 'active'/)
  assert.match(source, /v-if="canOpenChatboxProfile\(row\)"/)
  assert.doesNotMatch(source, /profileSceneTags\(profile\)\.includes/)
  assert.doesNotMatch(source, /aiAgentSceneCommon/)
})

test('ai agent profile validation keeps detailed dependency failures visible', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /validationResult/)
  assert.match(source, /validation-result/)
  assert.match(source, /validationResult\.errors/)
  assert.match(source, /validationResult\.warnings/)
  assert.match(source, /result\?\.data\?\.resource_health/)
})

test('ai agent profile resource references use exact version pins instead of free text latest selectors', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /buildPinnedResourceRefs/)
  assert.match(source, /createPinnedResourceRefRow/)
  assert.match(source, /applyVersionPinToRow/)
  assert.match(source, /listAgentResourceVersions/)
  assert.match(source, /listAgentProfileVersions/)
  assert.match(source, /v-model="row\.resource_version_id"/)
  assert.match(source, /handleResourceRefChange/)
  assert.match(source, /handleResourceVersionChange/)
  assert.doesNotMatch(source, /<el-input v-model="row\.resource_version"/)
  assert.doesNotMatch(source, /allow-create default-first-option placeholder="选择或输入 MCP Server ID"/)
})

test('ai agent destructive resource operations are gated by backend dependency operations', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /getAgentProfileDependencies/)
  assert.match(source, /getAgentResourceDependencies/)
  assert.match(source, /dependencyDrawer/)
  assert.match(source, /openDependencyDrawer/)
  assert.match(source, /dependencyOperationAllowed/)
  assert.match(source, /confirmDependencyOperation/)
  assert.match(source, /operations\?\.\[dependencyDrawer\.operation\]\?\.allowed/)
  assert.match(source, /extractDependencyConflict/)
  assert.match(source, /openDependencyDrawer\(\{\s*targetKind:\s*'profile'/)
  assert.match(source, /openDependencyDrawer\(\{\s*targetKind:\s*'resource'/)
  assert.doesNotMatch(source, /await ElMessageBox\.confirm\(`确认删除 Agent Profile/)
  assert.doesNotMatch(source, /await ElMessageBox\.confirm\(`确认删除 Agent 资源/)
})

test('ai agent profile list flags stale and invalid pinned resource configuration', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')
  const helper = await readFile(join(currentDir, 'aiAgentStoreDependencies.js'), 'utf8')

  assert.match(source, /profileDependencyHealth/)
  assert.match(source, /profileResourceHealthIssues/)
  assert.match(helper, /缺少固定版本/)
  assert.match(helper, /配置失效/)
  assert.match(helper, /可更新/)
  assert.match(source, /profileDependencyHealth\(row\)\.label/)
  assert.match(source, /profileDependencyHealth\(row\)\.type/)
  assert.match(source, /openProfileDependencyHealth\(row\)/)
  assert.match(source, /dependencyDrawer\.healthIssues/)
})

test('ai agent profile list preloads pinned dependency versions before reporting health', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')
  const loadStart = source.indexOf('async function loadData()')
  const loadEnd = source.indexOf('async function resolveWorkspaceId()', loadStart)
  const loadSource = source.slice(loadStart, loadEnd)

  assert.match(source, /async function prefetchProfileDependencyVersions/)
  assert.match(loadSource, /await prefetchProfileDependencyVersions\(profiles\.value/)
  assert.match(source, /ensureVersionOptions\(row, field, \{ force: true, silent: true/)
})

test('ai agent profile edit and save verify every pinned version before mutation', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')
  const editStart = source.indexOf('async function openEditDialog(row)')
  const editEnd = source.indexOf('async function submitProfile()', editStart)
  const editSource = source.slice(editStart, editEnd)
  const submitStart = source.indexOf('async function submitProfile()')
  const submitEnd = source.indexOf('function openMcpServerDialog', submitStart)
  const submitSource = source.slice(submitStart, submitEnd)

  assert.match(editSource, /await ensureProfileVersionOptions\(profileForm/)
  assert.match(submitSource, /await ensureProfileVersionOptions\(profileForm, \{ force: true, silent: true \}\)/)
  assert.match(submitSource, /blockingProfileHealthIssues/)
  assert.match(submitSource, /if \(blockingIssues\.length > 0\)[\s\S]*return/)
  assert.ok(submitSource.indexOf('blockingProfileHealthIssues') < submitSource.indexOf('updateAgentProfile'))
})

test('ai agent disable and archive transitions are preflighted by backend dependency operations', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')
  const profileStart = source.indexOf('async function submitProfile()')
  const profileEnd = source.indexOf('function openMcpServerDialog', profileStart)
  const profileSource = source.slice(profileStart, profileEnd)
  const mcpStart = source.indexOf('async function submitMcpServer()')
  const mcpEnd = source.indexOf('function testMcpConnection', mcpStart)
  const mcpSource = source.slice(mcpStart, mcpEnd)
  const skillStart = source.indexOf('async function submitSkillRepository()')
  const skillEnd = source.indexOf('async function deleteSkillRepository', skillStart)
  const skillSource = source.slice(skillStart, skillEnd)

  assert.match(source, /statusTransitionOperation/)
  assert.match(source, /async function preflightStatusMutation/)
  assert.match(source, /targetKind === 'profile'[\s\S]*getAgentProfileDependencies/)
  assert.match(source, /targetKind === 'resource'[\s\S]*getAgentResourceDependencies/)
  assert.match(profileSource, /preflightStatusMutation/)
  assert.match(mcpSource, /preflightStatusMutation/)
  assert.match(skillSource, /preflightStatusMutation/)
})

test('ai agent store hides profile kind controls and payload fields', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.doesNotMatch(source, /profileFilterKind/)
  assert.doesNotMatch(source, /profileKindOptions/)
  assert.doesNotMatch(source, /profileForm\.profile_kind/)
  assert.doesNotMatch(source, /profile_kind:\s*/)
  assert.doesNotMatch(source, /placeholder="全部类型"/)
})

test('ai agent store locks fixed page assistant profile name delete and required context tag', async () => {
  const source = await readFile(join(currentDir, 'ai-agent-store.vue'), 'utf8')

  assert.match(source, /pageAssistantProfileName\s*=\s*'page-ai-assistant'/)
  assert.match(source, /pageAssistantContextTag\s*=\s*'page-assistant'/)
  assert.match(source, /isReservedPageAssistantProfile/)
  assert.match(source, /isEditingReservedPageAssistantProfile/)
  assert.match(source, /:disabled="isEditingReservedPageAssistantProfile"/)
  assert.match(source, /系统保留/)
  assert.match(source, /v-if="!isReservedPageAssistantProfile\(row\)"/)
  assert.match(source, /handleContextTagRemove/)
  assert.match(source, /ensureReservedPageAssistantContextTag/)
  assert.match(source, /普通 Agent Profile 不允许配置 page-assistant 上下文标签/)
})
