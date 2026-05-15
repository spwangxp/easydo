import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const readView = (relativePath) => readFile(join(currentDir, relativePath), 'utf8')

test('pipeline list keeps approved single-line columns and excludes creator columns', async () => {
  const source = await readView('index.vue')

  assert.match(source, /label="最近构建"/)
  assert.match(source, /label="编辑人员"/)
  assert.match(source, /label="构建人员"/)
  assert.match(source, /label="更新时间"/)
  assert.doesNotMatch(source, /label="创建人"/)
  assert.doesNotMatch(source, /label="创建时间"/)
  assert.match(source, /class="pipeline-primary-line"/)
  assert.match(source, /class="pipeline-build-line"/)
  assert.match(source, /class="table-actions"/)
  assert.match(source, /content="复制"/)
  assert.match(source, /@click="handleCopy\(row\)"/)
})

test('pipeline copy reuses create dialog with copy-mode payload preparation', async () => {
  const source = await readView('index.vue')

  assert.match(source, /const dialogMode = ref\('create'\)/)
  assert.match(source, /const copiedDefinitionJson = ref\(''\)/)
  assert.match(source, /const dialogTitle = computed\(\(\) => \(dialogMode\.value === 'copy' \? '复制流水线' : '新建流水线'\)\)/)
  assert.match(source, /const submitButtonText = computed\(\(\) => \(dialogMode\.value === 'copy' \? '复制' : '创建'\)\)/)
  assert.match(source, /:title="dialogTitle"/)
  assert.match(source, /@closed="resetPipelineDialogState"/)
  assert.match(source, /<el-option label="无项目" :value="0"\s*\/?>/)
  assert.match(source, /const payload = buildPipelineCopyPayload\(response\.data\)/)
  assert.match(source, /copiedDefinitionJson\.value = payload\.definition_json/)
  assert.match(source, /pipelineForm\.name = payload\.name \?\? ''/)
  assert.match(source, /pipelineForm\.project_id = payload\.project_id \?\? ''/)
  assert.match(source, /dialogVisible\.value = true/)
  assert.match(source, /dialogMode\.value === 'copy' && copiedDefinitionJson\.value\s*\? \{ definition_json: copiedDefinitionJson\.value \}/)
  assert.doesNotMatch(source, /const payload = buildPipelineCopyPayload\(response\.data\)\s*await createPipeline\(payload\)/)
})

test('pipeline detail execution tasks use single-line meta and isolated auxiliary blocks', async () => {
  const source = await readView('detail.vue')

  assert.match(source, /class="task-main-line"/)
  assert.match(source, /class="task-inline-meta"/)
  assert.match(source, /class="task-inline-outputs"/)
  assert.match(source, /class="task-actions"/)
  assert.match(source, /class="task-error"/)
  assert.match(source, /class="task-outputs task-outputs--expanded"/)
})

test('pipeline detail statistics tab uses shared date params and real-data sections', async () => {
  const source = await readView('detail.vue')

  assert.match(source, /buildStatisticsDateParams/)
  assert.match(source, /getDefaultStatisticsDateRange/)
  assert.match(source, /const statsDateRange = ref\(getDefaultStatisticsDateRange\(\)\)/)
  assert.match(source, /getPipelineStatistics\(pipelineId\.value, buildStatisticsDateParams\(statsDateRange\.value\)\)/)
  assert.match(source, /recent_failures/)
  assert.match(source, /distribution/)
  assert.match(source, /daily_runs/)
  assert.match(source, /运行趋势/)
  assert.match(source, /状态分布/)
  assert.match(source, /最近失败/)
  assert.doesNotMatch(source, /description="趋势图表"/)
  assert.doesNotMatch(source, /description="分布图表"/)
  assert.doesNotMatch(source, /recentFailures\.value = \[/)
  assert.doesNotMatch(source, /prop="stage" label="阶段"/)
})

test('pipeline detail keeps the approved compact single-line header structure', async () => {
  const source = await readView('detail.vue')

  assert.match(source, /class="detail-header"/)
  assert.match(source, /class="pipeline-info pipeline-info--compact"/)
  assert.match(source, /class="header-divider"/)
  assert.match(source, /class="detail-tabs"/)
  assert.match(source, /class="header-tab-btn"/)
  assert.match(source, /@click="activeTab = 'design'"/)
  assert.match(source, /@click="activeTab = 'history'"/)
  assert.match(source, /@click="activeTab = 'execution'"/)
  assert.match(source, /@click="activeTab = 'statistics'"/)
  assert.match(source, /@click="activeTab = 'settings'"/)
  assert.doesNotMatch(source, /@click="activeTab = 'report'"/)
  assert.doesNotMatch(source, /测试报告/)
  assert.doesNotMatch(source, /更多/)
  assert.doesNotMatch(source, /收起/)
  assert.match(source, /运行流水线/)
})

test('pipeline detail settings normalize project selection value so the selected label resolves to project name', async () => {
  const source = await readView('detail.vue')

  assert.match(source, /const normalizeProjectSelectionValue = \(value\) =>/)
  assert.match(source, /settingsForm\.project_id = normalizeProjectSelectionValue\(pipeline\.value\.project_id\)/)
  assert.match(source, /:value="normalizeProjectSelectionValue\(project\.id\)"/)
})

test('pipeline detail keeps current project label from detail payload and paginates project options through getProjectList', async () => {
  const source = await readView('detail.vue')

  assert.match(source, /const currentProjectOption = computed\(\(\) =>/)
  assert.match(source, /const projectOptions = computed\(\(\) =>/)
  assert.match(source, /pushOption\(currentProjectOption\.value\)/)
  assert.match(source, /filterable/)
  assert.match(source, /remote/)
  assert.match(source, /:remote-method="handleProjectSearch"/)
  assert.match(source, /@visible-change="handleProjectDropdownVisibleChange"/)
  assert.match(source, /popper-class="pipeline-project-select-dropdown"/)
  assert.match(source, /getProjectList\(\{\s*page: projectQuery\.page,\s*page_size: projectQuery\.pageSize,\s*keyword: projectQuery\.keyword\s*\}\)/)
  assert.match(source, /bindProjectDropdownScroll\(/)
  assert.match(source, /loadMoreProjects\(/)
})

test('shared layout shell keeps compact spacing and theme-driven content wrapper background', async () => {
  const source = await readView('../layout/index.vue')
  const variablesSource = await readView('../../assets/styles/variables.scss')

  assert.match(variablesSource, /\$sidebar-width: 170px;/)
  assert.match(source, /margin: 8px 0 8px 8px;/)
  assert.match(source, /padding: 8px;/)
  assert.match(source, /margin: 8px;/)
  assert.match(source, /background: var\(--glass-bg\);/)
  assert.match(source, /&\.content-wrapper--pipeline-detail\s*\{[\s\S]*margin-top: 8px;/)
})

test('pipeline detail uses unified radius tokens and shell-controlled horizontal spacing', async () => {
  const source = await readView('detail.vue')

  assert.match(source, /\.pipeline-detail-container\s*\{[\s\S]*padding: 0;/)
  assert.match(source, /\.pipeline-detail-container\s*\{[\s\S]*border-radius: \$radius-2xl;/)
  assert.match(source, /\.detail-header\s*\{[\s\S]*border-radius: \$radius-xl;/)
  assert.match(source, /\.tab-panel\s*\{[\s\S]*border-radius: \$radius-xl;/)
})

test('pipeline design tab fills remaining detail area instead of using viewport-fixed heights', async () => {
  const detailSource = await readView('detail.vue')
  const designSource = await readView('designTab.vue')

  assert.match(detailSource, /\.pipeline-detail-container\s*\{[\s\S]*display: flex;/)
  assert.match(detailSource, /\.pipeline-detail-container\s*\{[\s\S]*flex-direction: column;/)
  assert.match(detailSource, /\.detail-content\s*\{[\s\S]*display: flex;/)
  assert.match(detailSource, /\.detail-content\s*\{[\s\S]*flex-direction: column;/)
  assert.match(detailSource, /\.detail-content\s*\{[\s\S]*flex: 1;/)
  assert.match(detailSource, /\.detail-content\s*\{[\s\S]*min-height: 0;/)
  assert.match(detailSource, /\.tab-panel\s*\{[\s\S]*width: 100%;/)
  assert.match(detailSource, /\.tab-panel\s*\{[\s\S]*max-width: 100%;/)
  assert.match(detailSource, /\.tab-panel\s*\{[\s\S]*min-width: 0;/)
  assert.match(detailSource, /<el-table-column label="分支" min-width="150">/)
  assert.match(detailSource, /<el-table-column label="操作" min-width="280" fixed="right">/)
  assert.match(detailSource, /\.design-panel\s*\{[\s\S]*display: flex;/)
  assert.match(detailSource, /\.design-panel\s*\{[\s\S]*flex: 1;/)
  assert.match(detailSource, /\.design-panel\s*\{[\s\S]*min-height: 0;/)
  assert.doesNotMatch(detailSource, /\.design-panel\s*\{[\s\S]*min-height: calc\(100vh - 232px\);/)
  assert.match(designSource, /\.pipeline-design-container\s*\{[\s\S]*flex: 1;/)
  assert.match(designSource, /\.pipeline-design-container\s*\{[\s\S]*height: 100%;/)
  assert.match(designSource, /\.pipeline-design-container\s*\{[\s\S]*min-height: 100%;/)
  assert.match(designSource, /\.connections-layer-wrapper\s*\{[\s\S]*z-index: 10;/)
  assert.match(designSource, /\.nodes-layer\s*\{[\s\S]*z-index: 20;/)
  assert.doesNotMatch(designSource, /\.pipeline-design-container\s*\{[\s\S]*height: calc\(100vh - 232px\);/)
})

test('pipeline design nodes always initialize with explicit 90px height', async () => {
  const source = await readView('designTab.vue')

  const heightMatches = source.match(/height:\s*90,/g) || []
  assert.equal(heightMatches.length, 4)
})
