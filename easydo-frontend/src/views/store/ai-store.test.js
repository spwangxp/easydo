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
