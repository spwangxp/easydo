import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

async function readView(relativePath) {
  return readFile(join(currentDir, relativePath), 'utf8')
}

test('shared page header component exists with title subtitle and actions slots', async () => {
  const source = await readView('store/components/PageHeader.vue')

  assert.match(source, /class="page-header"/)
  assert.match(source, /class="page-header-main"/)
  assert.match(source, /class="page-header-actions"/)
  assert.match(source, /<slot\s+name="title"\s*\/>/)
  assert.match(source, /<slot\s+name="subtitle"\s*\/>/)
  assert.match(source, /<slot\s+name="actions"\s*\/>/)
})

test('shared page header top aligns actions even when subtitle height differs', async () => {
  const source = await readView('store/components/PageHeader.vue')
  const pageHeaderBlock = source.match(/\.page-header\s*\{[\s\S]*?\n\}/)

  assert.ok(pageHeaderBlock)
  assert.match(pageHeaderBlock[0], /align-items:\s*flex-start;/)
  assert.doesNotMatch(pageHeaderBlock[0], /align-items:\s*center;/)
})

test('only store pages use shared store kind switch component', async () => {
  const [switchSource, aiStore, appStore, pipelinePage, deployPage, projectPage, resourcePage, k8sPage, credentialPage, templatePage] = await Promise.all([
    readView('store/components/StoreKindSwitch.vue'),
    readView('store/ai-store.vue'),
    readView('store/components/AppStorePage.vue'),
    readView('pipeline/index.vue'),
    readView('deploy/index.vue'),
    readView('project/index.vue'),
    readView('resources/index.vue'),
    readView('resources/k8s/index.vue'),
    readView('credentials/index.vue'),
    readView('store/components/StoreTemplatePage.vue')
  ])

  assert.match(switchSource, /class="store-kind-switch"/)
  assert.match(switchSource, /class="store-kind-option"/)
  assert.match(switchSource, /class="store-kind-divider"/)

  assert.match(aiStore, /import\s+PageHeader\s+from\s+'\.\/components\/PageHeader\.vue'/)
  assert.match(aiStore, /import\s+StoreKindSwitch\s+from\s+'\.\/components\/StoreKindSwitch\.vue'/)
  assert.match(aiStore, /<PageHeader>/)
  assert.match(aiStore, /<StoreKindSwitch/)
  assert.doesNotMatch(aiStore, /<section class="store-tabs-bar">/)
  assert.doesNotMatch(aiStore, /class="store-kind-switch"/)

  assert.match(appStore, /import\s+PageHeader\s+from\s+'\.\/PageHeader\.vue'/)
  assert.match(appStore, /import\s+StoreKindSwitch\s+from\s+'\.\/StoreKindSwitch\.vue'/)
  assert.match(appStore, /<PageHeader>/)
  assert.match(appStore, /<StoreKindSwitch/)
  assert.doesNotMatch(appStore, /<section class="store-tabs-bar">/)
  assert.doesNotMatch(appStore, /class="store-kind-switch"/)

  for (const pageSource of [pipelinePage, deployPage, projectPage, resourcePage, k8sPage, credentialPage, templatePage]) {
    assert.doesNotMatch(pageSource, /StoreKindSwitch/)
    assert.doesNotMatch(pageSource, /store-kind-switch/)
  }
})

test('all fixed-title pages use shared page header component instead of local title blocks', async () => {
  const [pipelinePage, deployPage, projectPage, resourcePage, k8sPage, credentialPage, templatePage, dashboardPage, statisticsPage, messagesPage, settingsPage, profilePage, agentPage, agentPendingPage] = await Promise.all([
    readView('pipeline/index.vue'),
    readView('deploy/index.vue'),
    readView('project/index.vue'),
    readView('resources/index.vue'),
    readView('resources/k8s/index.vue'),
    readView('credentials/index.vue'),
    readView('store/components/StoreTemplatePage.vue'),
    readView('dashboard/index.vue'),
    readView('statistics/index.vue'),
    readView('messages/index.vue'),
    readView('settings/index.vue'),
    readView('profile/index.vue'),
    readView('agent/index.vue'),
    readView('agent/pending.vue')
  ])

  assert.match(pipelinePage, /import\s+PageHeader\s+from\s+'\.\.\/store\/components\/PageHeader\.vue'/)
  assert.match(pipelinePage, /<PageHeader>/)
  assert.doesNotMatch(pipelinePage, /<div class="pipeline-header">/)

  assert.match(deployPage, /import\s+PageHeader\s+from\s+'\.\.\/store\/components\/PageHeader\.vue'/)
  assert.match(deployPage, /<PageHeader>/)
  assert.doesNotMatch(deployPage, /<div class="deploy-header">/)

  assert.match(projectPage, /import\s+PageHeader\s+from\s+'\.\.\/store\/components\/PageHeader\.vue'/)
  assert.match(projectPage, /<PageHeader>/)
  assert.doesNotMatch(projectPage, /<div class="project-header">/)

  assert.match(resourcePage, /import\s+PageHeader\s+from\s+'\.\.\/store\/components\/PageHeader\.vue'/)
  assert.match(resourcePage, /<PageHeader>/)
  assert.doesNotMatch(resourcePage, /<div class="page-header">/)

  assert.match(k8sPage, /import\s+PageHeader\s+from\s+'\.\.\/\.\.\/store\/components\/PageHeader\.vue'/)
  assert.match(k8sPage, /<PageHeader>/)
  assert.doesNotMatch(k8sPage, /<div class="page-header">/)

  assert.match(credentialPage, /import\s+PageHeader\s+from\s+'\.\.\/store\/components\/PageHeader\.vue'/)
  assert.match(credentialPage, /<PageHeader>/)
  assert.doesNotMatch(credentialPage, /<div class="page-header">/)

  assert.match(templatePage, /import\s+PageHeader\s+from\s+'\.\/PageHeader\.vue'/)
  assert.match(templatePage, /<PageHeader>/)
  assert.doesNotMatch(templatePage, /<div class="page-header">/)

  for (const pageSource of [dashboardPage, statisticsPage, messagesPage, settingsPage, profilePage, agentPage, agentPendingPage]) {
    assert.match(pageSource, /PageHeader/)
    assert.doesNotMatch(pageSource, /<h1 class="page-title">/)
  }
})

test('shared page header actions component provides common layout and button sizing', async () => {
  const source = await readView('store/components/PageHeaderActions.vue')

  assert.match(source, /class="page-header-actions-group"/)
  assert.match(source, /:deep\(\.el-button\)/)
  assert.match(source, /height:\s*42px/)
  assert.match(source, /padding:\s*0\s+18px/)
  assert.doesNotMatch(source, /store-header-actions/)
})

test('all tab pages with header actions use shared generic actions abstraction', async () => {
  const [pipelinePage, projectPage, dashboardPage, deployPage, resourcesPage, statisticsPage, messagesPage, credentialsPage, k8sPage, aiStorePage, appStorePage, templatePage] = await Promise.all([
    readView('pipeline/index.vue'),
    readView('project/index.vue'),
    readView('dashboard/index.vue'),
    readView('deploy/index.vue'),
    readView('resources/index.vue'),
    readView('statistics/index.vue'),
    readView('messages/index.vue'),
    readView('credentials/index.vue'),
    readView('resources/k8s/index.vue'),
    readView('store/ai-store.vue'),
    readView('store/components/AppStorePage.vue'),
    readView('store/components/StoreTemplatePage.vue')
  ])

  assert.match(aiStorePage, /import\s+PageHeaderActions\s+from\s+'\.\/components\/PageHeaderActions\.vue'/)
  assert.match(aiStorePage, /<PageHeaderActions>/)
  assert.doesNotMatch(aiStorePage, /StoreHeaderActions/)

  assert.match(appStorePage, /import\s+PageHeaderActions\s+from\s+'\.\/PageHeaderActions\.vue'/)
  assert.match(appStorePage, /<PageHeaderActions>/)
  assert.doesNotMatch(appStorePage, /StoreHeaderActions/)

  assert.match(templatePage, /import\s+PageHeaderActions\s+from\s+'\.\/PageHeaderActions\.vue'/)
  assert.match(templatePage, /<PageHeaderActions/)
  assert.doesNotMatch(templatePage, /class="header-actions"/)

  assert.match(deployPage, /import\s+PageHeaderActions\s+from\s+'\.\.\/store\/components\/PageHeaderActions\.vue'/)
  assert.match(deployPage, /<PageHeaderActions>/)
  assert.doesNotMatch(deployPage, /class="deploy-header-actions"/)
  assert.doesNotMatch(deployPage, /:deep\(\.page-header-actions \.el-button--primary\)/)

  assert.match(k8sPage, /import\s+PageHeaderActions\s+from\s+'\.\.\/\.\.\/store\/components\/PageHeaderActions\.vue'/)
  assert.match(k8sPage, /<PageHeaderActions>/)
  assert.doesNotMatch(k8sPage, /class="header-actions"/)

  assert.match(statisticsPage, /import\s+PageHeaderActions\s+from\s+'\.\.\/store\/components\/PageHeaderActions\.vue'/)
  assert.match(statisticsPage, /<PageHeaderActions>/)
  assert.doesNotMatch(statisticsPage, /<div class="date-range-picker">/)

  for (const pageSource of [pipelinePage, projectPage, dashboardPage, resourcesPage, messagesPage, credentialsPage]) {
    assert.match(pageSource, /import\s+PageHeaderActions\s+from\s+/)
    assert.match(pageSource, /<PageHeaderActions/)
  }

  for (const pageSource of [pipelinePage, projectPage, dashboardPage, deployPage]) {
    assert.doesNotMatch(pageSource, /:deep\(\.page-header-actions \.el-button--primary\)/)
    assert.doesNotMatch(pageSource, /\.header-actions\s*:deep\(\.el-button--primary\)/)
  }

  assert.doesNotMatch(messagesPage, /:deep\(\.page-header-actions \.el-button--text\)/)
})
