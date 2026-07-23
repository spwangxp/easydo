import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const viewPath = join(currentDir, 'ai-store.vue')

async function readViewSource() {
  return readFile(viewPath, 'utf8')
}

test('ai store deploy dialog uses shared store parameter fields component', async () => {
  const source = await readViewSource()

  assert.match(source, /import\s+StoreParameterFields\s+from\s+'\.\/components\/StoreParameterFields\.vue'/)
  assert.match(source, /<StoreParameterFields/)
})

test('ai store page does not render summary cards', async () => {
  const source = await readViewSource()

  assert.doesNotMatch(source, /summaryCards/)
  assert.doesNotMatch(source, /catalog-overview/)
  assert.doesNotMatch(source, /overview-card/)
})

test('ai store loads deploy templates with ai template type', async () => {
  const source = await readViewSource()

  assert.match(source, /getTemplateList\(\{\s*template_type:\s*'ai'\s*\}\)/)
  assert.doesNotMatch(source, /getTemplateList\(\{\s*template_type:\s*'llm'\s*\}\)/)
})

test('ai store uses shared header actions component', async () => {
  const source = await readViewSource()

  assert.match(source, /import\s+StoreHeaderActions\s+from\s+'\.\/components\/StoreHeaderActions\.vue'/)
  assert.match(source, /<StoreHeaderActions>/)
  assert.doesNotMatch(source, /<div class="store-tabs-actions">/)
})

test('ai store does not manage agent profiles or runtime profiles', async () => {
  const source = await readViewSource()

  assert.doesNotMatch(source, /openRuntimeProfile/)
  assert.doesNotMatch(source, /Runtime Profile/)
  assert.doesNotMatch(source, /runtime_profile_id/)
  assert.match(source, /Agent Profile 请在 AI Agent 商店管理/)
})

test('ai store removes binding capabilities json from provider binding flow', async () => {
  const source = await readViewSource()

  assert.doesNotMatch(source, /Capabilities JSON/)
  assert.doesNotMatch(source, /capabilities_json:/)
  assert.doesNotMatch(source, /providerForm\.capabilitiesJSON/)
  assert.doesNotMatch(source, /Binding Settings JSON/)
  assert.doesNotMatch(source, /Binding Metadata JSON/)
  assert.match(source, /const providerHeaders = providerForm\.headersJSON\.trim\(\) \? JSON\.parse\(providerForm\.headersJSON\) : \{\}/)
  assert.match(source, /const providerSettings = providerForm\.settingsJSON\.trim\(\) \? JSON\.parse\(providerForm\.settingsJSON\) : \{\}/)
  assert.match(source, /headers_json: providerHeaders/)
  assert.match(source, /\.\.\.providerSettings/)
  assert.doesNotMatch(source, /model_adapter/)
  assert.doesNotMatch(source, /modelAdapter/)
  assert.doesNotMatch(source, /Model Adapter/)
  assert.match(source, /settings_json:\s*\{\}/)
  assert.match(source, /buildModelBindingCapabilityPayload\(row\)/)
  assert.match(source, /metadata_json:\s*capability\.metadata_json/)
  assert.match(source, /context_window_tokens:\s*capability\.context_window_tokens/)
})

test('ai store uses llm endpoint field and renders resolved endpoint previews', async () => {
  const source = await readViewSource()

  assert.match(source, /label="LLM Endpoint"/)
  assert.match(source, /providerForm\.llmEndpoint/)
  assert.match(source, /llm_endpoint:\s*providerForm\.llmEndpoint/)
  assert.match(source, /providerModelsEndpointURL/)
  assert.match(source, /providerLlmEndpointURL/)
  assert.doesNotMatch(source, /<template #append>\s*<span class="endpoint-preview-inline">/)
  assert.match(source, /<div class="endpoint-preview-field">\s*<el-input v-model="providerForm\.modelsEndpoint"/)
  assert.match(source, /<span class="endpoint-preview-inline">\{\{ providerModelsEndpointURL \}\}<\/span>\s*<\/div>/)
  assert.match(source, /<div class="endpoint-preview-field">\s*<el-input v-model="providerForm\.llmEndpoint"/)
  assert.match(source, /<span class="endpoint-preview-inline">\{\{ providerLlmEndpointURL \}\}<\/span>\s*<\/div>/)
  assert.doesNotMatch(source, /<div class="endpoint-preview">/)
  assert.match(source, /resolveProviderEndpointURL/)
  assert.match(source, /\.endpoint-preview-field\s*\{[^}]*position:\s*relative/)
  assert.match(source, /\.endpoint-preview-inline\s*\{[^}]*position:\s*absolute[^}]*right:\s*12px[^}]*color:\s*var\(--text-tertiary/)
})

test('ai store imports only api functions exported by store api module', async () => {
  const [viewSource, apiSource] = await Promise.all([
    readViewSource(),
    readFile(join(currentDir, '../../api/store.js'), 'utf8')
  ])

  const importBlock = viewSource.match(/import\s*\{([^}]*)\}\s*from\s*'@\/api\/store'/)
  assert.ok(importBlock, 'expected ai-store.vue to import from @/api/store')

  const importedNames = importBlock[1]
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)

  for (const name of importedNames) {
    assert.match(apiSource, new RegExp(`export\\s+(?:async\\s+)?function\\s+${name}\\s*\\(`), `expected ${name} to be exported from src/api/store.js`)
  }
})

test('store api keeps model store separate from agent profile store', async () => {
  const apiSource = await readFile(join(currentDir, '../../api/store.js'), 'utf8')

  assert.match(apiSource, /export function getAIModelCatalog\(params\) \{\s*return getWorkspaceAIModelCatalog\(params\)\s*\}/)
  assert.doesNotMatch(apiSource, /url:\s*'\/ai\/agents'/)
  assert.doesNotMatch(apiSource, /url:\s*'\/ai\/runtime-profiles'/)
  assert.doesNotMatch(apiSource, /RuntimeProfile/)
})

test('ai store deploy dialog wires gpu estimate section', async () => {
  const source = await readViewSource()

  assert.match(source, /显存估算/)
  assert.match(source, /deployVramEstimateViewModel\.summary/)
  assert.match(source, /deployVramEstimateViewModel\.composition/)
  assert.match(source, /deployVramEstimateViewModel\.selection/)
  assert.match(source, /先选择目标资源以继续核对 GPU 容量/)
})

test('ai store deploy dialog uses compact vram estimate layout with composition details', async () => {
  const source = await readViewSource()

  assert.match(source, /deploy-vram-estimate-metrics/)
  assert.match(source, /deploy-vram-estimate-composition/)
  assert.match(source, /deploy-vram-estimate-selection/)
  assert.match(source, /显存组成/)
  assert.match(source, /当前组合/)
  assert.match(source, /padding:\s*12px/)
  assert.match(source, /gap:\s*8px/)
})

test('ai store maps estimate display status to chinese ui labels', async () => {
  const source = await readViewSource()

  assert.match(source, /sufficient:\s*'充足'/)
  assert.match(source, /warning:\s*'预警'/)
  assert.match(source, /insufficient:\s*'不足'/)
  assert.match(source, /'missing-data':\s*'数据不足'/)
  assert.match(source, /collecting:\s*'采集中'/)
  assert.match(source, /failed:\s*'失败'/)
  assert.match(source, /idle:\s*'待选择资源'/)
  assert.match(source, /<el-tag>\{\{\s*deployVramEstimateViewModel\.displayStatusLabel\s*\}\}<\/el-tag>/)
  assert.doesNotMatch(source, /<el-tag>\{\{\s*deployVramEstimateViewModel\.displayStatus\s*\}\}<\/el-tag>/)
})

test('ai store keeps page-local gpu cache and selected gpu state', async () => {
  const source = await readViewSource()

  assert.match(source, /const\s+resourceGpuInfoCache\s*=\s*reactive\s*\(\s*\{\s*\}\s*\)/)
  assert.match(source, /const\s+resourceRefreshTimers\s*=\s*new\s+Map\s*\(\s*\)/)
  assert.match(source, /const\s+selectedGpuDeviceKeys\s*=\s*ref\s*\(\s*\[\s*\]\s*\)/)
  assert.match(source, /selectedDeployResource/)
  assert.match(source, /ensureResourceGpuInfo/)
  assert.match(source, /startResourceGpuRefresh/)
  assert.match(source, /retryResourceGpuRefresh/)
  assert.match(source, /refreshToken/)
  assert.match(source, /setTimeout\s*\(/)
  assert.match(source, /clearTimeout\s*\(/)
})

test('ai store deploy resource selection explicitly starts gpu estimation refresh', async () => {
  const source = await readViewSource()

  assert.match(source, /v-model="deployForm\.targetResourceId"[\s\S]*@change="handleDeployResourceChange"/)
  assert.match(source, /function\s+handleDeployResourceChange\s*\(\s*resourceId\s*\)/)
  assert.match(source, /ensureResourceGpuInfo\(resourceId\)/)
})

test('ai store imports shared gpu estimate and resource helpers', async () => {
  const source = await readViewSource()

  assert.match(source, /from\s+'\.\/aiVramEstimate'/)
  assert.match(source, /buildDeployVramEstimate/)
  assert.match(source, /buildDeployVramEstimateViewModel/)
  assert.match(source, /from\s+'\.\/aiResourceGpuInfo'/)
  assert.match(source, /createGpuInfoCacheEntry/)
  assert.match(source, /normalizeResourceGpuInfo/)
})

test('ai store wires resource refresh apis for gpu polling flow', async () => {
  const source = await readViewSource()

  assert.match(source, /getResourceList/)
  assert.match(source, /getResourceDetail/)
  assert.match(source, /refreshResourceBaseInfo/)
  assert.match(source, /refreshResourceBaseInfo\s*\(/)
  assert.match(source, /getResourceList\s*\(/)
})

test('ai store model rows omit agent profile usage', async () => {
  const { buildModelRows } = await import('./aiStoreConfig.js')

  const rows = buildModelRows({
    models: [{ id: 101, name: 'Qwen 3' }],
    providers: [],
    deployments: []
  })

  assert.equal(rows.length, 1)
  assert.equal(Object.prototype.hasOwnProperty.call(rows[0], 'runtimeUsage'), false)
  assert.equal(Object.prototype.hasOwnProperty.call(rows[0], 'runtimeCount'), false)
})

test('ai store model rows show provider-specific context window min-max range', async () => {
  const { buildModelRows, formatContextLengthLabel, formatContextWindowRange } = await import('./aiStoreConfig.js')

  assert.equal(formatContextLengthLabel(131072), '128K')
  assert.equal(formatContextWindowRange([8192, 131072]), '8K–128K')

  const rows = buildModelRows({
    models: [{ id: 101, name: 'Qwen 3', context_window: 32768 }],
    providers: [
      {
        id: 1,
        name: 'openrouter',
        bindings: [{
          id: 11,
          model_id: 101,
          provider_model_key: 'qwen/qwen3',
          context_window_tokens: 131072
        }]
      },
      {
        id: 2,
        name: 'ollama',
        bindings: [{
          id: 22,
          model_id: 101,
          provider_model_key: 'qwen3',
          context_window_tokens: 8192
        }]
      }
    ],
    deployments: []
  })

  assert.equal(rows.length, 1)
  assert.equal(rows[0].contextWindowLabel, '8K–128K')
  assert.equal(rows[0].providers[0].context_window_label, '128K')
  assert.equal(rows[0].providers[1].context_window_label, '8K')
})

test('ai store page renders context length columns for models and provider bindings', async () => {
  const source = await readViewSource()
  assert.match(source, /label="上下文长度"/)
  assert.match(source, /row\.contextWindowLabel \|\| row\.context_window_label/)
  assert.match(source, /binding\.context_window_label/)
  assert.match(source, /formatDiscoveredContextLabel/)
})

test('ai store synchronizes selected gpu devices into deploy parameters', async () => {
  const source = await readViewSource()

  assert.match(source, /cuda_visible_devices/)
  assert.match(source, /nvidia_visible_devices/)
  assert.match(source, /gpu_indices/)
  assert.match(source, /gpu_ids/)
  assert.match(source, /device_ids/)
  assert.match(source, /gpu_devices/)
  assert.match(source, /gpu_uuids/)
  assert.match(source, /gpu_count/)
})

test('ai store exposes external provider management only to non-viewer workspace users', async () => {
  const source = await readViewSource()

  assert.match(source, /import\s+\{\s*useUserStore\s*\}\s+from\s+'@\/stores\/user'/)
  assert.match(source, /const\s+userStore\s*=\s*useUserStore\(\)/)
  assert.match(source, /const\s+canManageAIProviders\s*=\s*computed/)
  assert.match(source, /userStore\.currentWorkspaceId/)
  assert.match(source, /userStore\.currentWorkspaceRole/)
  assert.match(source, /currentWorkspaceRole[^=]*!==\s*'viewer'/)
  assert.match(source, /<el-button\s+v-if="canManageAIProviders"\s+type="primary"\s+@click="openProviderDialog\(\)">接入外部 Provider<\/el-button>/)
  assert.match(source, /if\s*\(!canManageAIProviders\.value\)\s*\{\s*ElMessage\.warning\('当前工作空间角色无权接入外部 Provider'\)/)
})

test('ai store create provider button does not pass click event as provider row', async () => {
  const source = await readViewSource()

  assert.match(source, /@click="openProviderDialog\(\)">接入外部 Provider/)
  assert.match(source, /normalizeProviderDialogRow/)
  assert.match(source, /event instanceof Event/)
})

test('ai store provider form sends required provider type', async () => {
  const source = await readViewSource()

  assert.match(source, /Provider 类型/)
  assert.match(source, /providerForm\.providerType/)
  assert.match(source, /provider_type:\s*providerForm\.providerType/)
})

test('ai store exposes model and provider supply views', async () => {
  const source = await readViewSource()

  assert.match(source, /supplyView/)
  assert.match(source, /模型视角/)
  assert.match(source, /Provider 视角/)
  assert.match(source, /providerRows/)
  assert.match(source, /openProviderDiscoveryDialog/)
})

test('ai store radio buttons use element plus value api', async () => {
  const source = await readViewSource()

  assert.match(source, /<el-radio-button\s+value="models">模型视角<\/el-radio-button>/)
  assert.match(source, /<el-radio-button\s+value="providers">Provider 视角<\/el-radio-button>/)
  assert.match(source, /<el-radio-button\s+v-for="option in modelTargetModeOptions"\s+:key="option\.value"\s+:value="option\.value">/)
  assert.doesNotMatch(source, /<el-radio-button[^>]+label=/)
})

test('ai store provider import wizard allows zero discovered model selections', async () => {
  const source = await readViewSource()

  assert.match(source, /providerDialog\.step/)
  assert.match(source, /discoveredProviderModelRows/)
  assert.match(source, /toggleAllDiscoveredModels/)
  assert.match(source, /selectedDiscoveredModelRows/)
  assert.match(source, /0 个模型供给/)
  assert.doesNotMatch(source, /立即绑定模型/)
})

test('ai store provider import merges connection test into config step and gates next step', async () => {
  const source = await readViewSource()

  assert.doesNotMatch(source, /<el-step\s+title="测试连接"\s*\/>/)
  assert.match(source, /<el-step\s+title="Provider 配置"\s*\/>/)
  assert.match(source, /<el-step\s+title="发现模型"\s*\/>/)
  assert.match(source, /<el-step\s+title="建立供给关系"\s*\/>/)
  assert.match(source, /testAIProviderConnection/)
  assert.match(source, /function\s+handleTestProviderConnection\s*\(/)
  assert.match(source, /providerConnection\.status\s*===\s*'success'/)
  assert.match(source, /providerNextDisabled/)
  assert.match(source, /:disabled="providerNextDisabled"/)
})

test('ai store provider discovery uses backend api instead of fake model candidates', async () => {
  const source = await readViewSource()

  assert.match(source, /discoverAIProviderModels/)
  assert.match(source, /function\s+loadProviderDiscovery\s*\(/)
  assert.match(source, /discoverAIProviderModels\s*\(/)
  assert.doesNotMatch(source, /buildProviderModelCandidates/)
  assert.doesNotMatch(source, /prepareProviderDiscovery/)
})

test('ai store provider discovery exposes search and pagination controls', async () => {
  const source = await readViewSource()

  assert.match(source, /providerDiscoveryFilters\.keyword/)
  assert.match(source, /filteredDiscoveredProviderModelRows/)
  assert.match(source, /paginatedDiscoveredProviderModelRows/)
  assert.match(source, /<el-input[\s\S]*placeholder="搜索模型 \/ Provider Key \/ 能力"/)
  assert.match(source, /<el-pagination/)
  assert.match(source, /:data="paginatedDiscoveredProviderModelRows"/)
  assert.match(source, /provider-discovery-toolbar/)
  assert.match(source, /provider-discovery-summary/)
})

test('ai store provider discovery filters and paginates discovered models', async () => {
  const {
    filterDiscoveredProviderModelRows,
    paginateDiscoveredProviderModelRows
  } = await import('./aiStoreConfig.js')

  const rows = [
    {
      provider_display_name: 'GPT 4.1',
      provider_model_key: 'openai/gpt-4.1',
      modalities: ['text'],
      capabilities: ['chat'],
      context_window: 128000,
      max_output_tokens: 16384
    },
    {
      provider_display_name: 'Qwen Vision',
      provider_model_key: 'qwen/qwen-vl-plus',
      modalities: ['text', 'image'],
      capabilities: ['vision'],
      context_window: 32000,
      max_output_tokens: 8192
    },
    {
      provider_display_name: 'Claude Sonnet',
      provider_model_key: 'anthropic/claude-sonnet-4',
      modalities: ['text'],
      capabilities: ['tool-use'],
      context_window: 200000,
      max_output_tokens: 64000
    }
  ]

  assert.deepEqual(
    filterDiscoveredProviderModelRows({ rows, keyword: 'vision image' }).map((row) => row.provider_model_key),
    ['qwen/qwen-vl-plus']
  )
  assert.deepEqual(
    filterDiscoveredProviderModelRows({ rows, keyword: '128000' }).map((row) => row.provider_model_key),
    ['openai/gpt-4.1']
  )
  assert.deepEqual(
    paginateDiscoveredProviderModelRows({ rows, page: 2, pageSize: 2 }).map((row) => row.provider_model_key),
    ['anthropic/claude-sonnet-4']
  )
})

test('store api exposes provider connection and discovery endpoints', async () => {
  const apiSource = await readFile(join(currentDir, '../../api/store.js'), 'utf8')

  assert.match(apiSource, /export function testAIProviderConnection\(data\)/)
  assert.match(apiSource, /url:\s*'\/store\/ai-providers\/test-connection'/)
  assert.match(apiSource, /export function discoverAIProviderModels\(data\)/)
  assert.match(apiSource, /url:\s*'\/store\/ai-providers\/discover-models'/)
})

test('ai store creates provider bindings only for selected discovered models', async () => {
  const source = await readViewSource()

  assert.match(source, /for \(const row of selectedDiscoveredModelRows\.value\)/)
  assert.match(source, /ensureDiscoveredModelCatalog/)
  assert.match(source, /createAIModelBinding\(createdProvider\.id/)
  assert.match(source, /buildModelBindingCapabilityPayload\(row\)/)
  assert.match(source, /context_window_tokens:\s*capability\.context_window_tokens/)
  assert.match(source, /metadata_json:\s*capability\.metadata_json/)
})

test('discovered model import payload preserves context window as a structured field', async () => {
  const {
    buildDiscoveredModelBindingMetadata,
    buildDiscoveredModelImportPayload
  } = await import('./aiStoreConfig.js')

  const row = {
    provider_model_key: 'qwen3.6-27b-local',
    provider_display_name: 'qwen3.6-27b-local',
    model_name: 'qwen3.6-27b-local',
    source_model_id: 'qwen3.6-27b-local',
    model_kind: 'chat',
    model_family: 'qwen',
    modalities: ['text'],
    capabilities: ['tool'],
    context_window: 262144,
    max_output_tokens: 8192,
    raw: { id: 'qwen3.6-27b-local', max_model_len: 262144 }
  }

  const importPayload = buildDiscoveredModelImportPayload(row)
  const bindingMetadata = buildDiscoveredModelBindingMetadata(row)

  assert.equal(importPayload.context_window, 262144)
  assert.equal(importPayload.metadata.context_window, 262144)
  assert.equal(importPayload.metadata.raw.max_model_len, 262144)
  assert.equal(bindingMetadata.context_window, 262144)
})

test('ai store import model dialog supports private and manual model metadata fields', async () => {
  const source = await readViewSource()

  assert.match(source, /<el-option label="私有模型仓库" value="private-repo" \/>/)
  assert.match(source, /<el-option label="手动录入" value="manual" \/>/)
  assert.match(source, /importModelForm\.name/)
  assert.match(source, /importModelForm\.displayName/)
  assert.match(source, /importModelForm\.modelKind/)
  assert.match(source, /importModelForm\.parameterSize/)
  assert.match(source, /importModelForm\.tagsText/)
  assert.match(source, /importModelForm\.repositoryUrl/)
  assert.match(source, /importModelForm\.recommendedRuntime/)
  assert.match(source, /CredentialSelector[\s\S]*v-model="importModelForm\.credentialId"/)
})

test('ai store import payload includes model metadata without raw json editing', async () => {
  const { buildAIModelImportPayload } = await import('./aiStoreConfig.js')

  const payload = buildAIModelImportPayload({
    source: 'private-repo',
    sourceModelId: 'git.example.com/models/qwen3',
    name: 'Qwen3 Private',
    displayName: 'Qwen3 Private Chat',
    modelKind: 'chat',
    parameterSize: '32B',
    license: 'internal',
    tagsText: 'qwen, chat',
    summary: 'Internal model',
    contextWindow: 32768,
    repositoryUrl: 'ssh://git.example.com/models/qwen3.git',
    revision: 'main',
    format: 'safetensors',
    recommendedRuntime: 'vllm',
    credentialId: 18
  })

  assert.equal(payload.source, 'private-repo')
  assert.equal(payload.source_model_id, 'git.example.com/models/qwen3')
  assert.equal(payload.name, 'Qwen3 Private')
  assert.equal(payload.display_name, 'Qwen3 Private Chat')
  assert.equal(payload.parameter_size, '32B')
  assert.equal(payload.context_window, 32768)
  assert.deepEqual(payload.tags, ['qwen', 'chat'])
  assert.equal(payload.metadata.model_kind, 'chat')
  assert.equal(payload.metadata.context_window, 32768)
  assert.equal(payload.metadata.repository_url, 'ssh://git.example.com/models/qwen3.git')
  assert.equal(payload.metadata.recommended_runtime, 'vllm')
  assert.equal(payload.metadata.credential_id, 18)
})
