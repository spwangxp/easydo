import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

async function readView(relativePath) {
  return readFile(join(currentDir, relativePath), 'utf8')
}

test('shared page header component still exists for out-of-scope pages', async () => {
  const source = await readView('store/components/PageHeader.vue')

  assert.match(source, /class="page-header"/)
  assert.match(source, /class="page-header-main"/)
  assert.match(source, /class="page-header-actions"/)
  assert.match(source, /<slot\s+name="title"\s*\/>/)
  assert.match(source, /<slot\s+name="subtitle"\s*\/>/)
  assert.match(source, /<slot\s+name="actions"\s*\/>/)
})

test('layout shell keeps only top title and workspace switch in the top bar', async () => {
  const source = await readView('layout/index.vue')

  assert.match(source, /<header class="topbar">/)
  assert.match(source, /<div class="title-block">/)
  assert.match(source, /<h1>\{\{ currentPageTitle \}\}<\/h1>/)
  assert.match(source, /class="workspace-select"/)
  assert.doesNotMatch(source, /el-breadcrumb/)
  assert.doesNotMatch(source, /time-chip/)
  assert.doesNotMatch(source, /QuestionFilled/)
  assert.doesNotMatch(source, /user-chip/)
})

test('layout shell keeps sidebar footer actions and collapsed user affordance', async () => {
  const source = await readView('layout/index.vue')

  assert.match(source, /class="sidebar-footer-icons"/)
  assert.match(source, /router\.push\('\/messages'\)/)
  assert.match(source, /toggleTheme/)
  assert.match(source, /class="user-info" @click="handleUserInfoClick"/)
  assert.match(source, /if \(isCollapsed\.value\) \{/)
  assert.match(source, /router\.push\('\/profile'\)/)
  assert.match(source, /showUserMenu\.value = false/)
})

test('layout shell removes sidebar slogan and keeps title/workspace truncation rules', async () => {
  const source = await readView('layout/index.vue')

  assert.doesNotMatch(source, /Delivery Control/)
  assert.match(source, /text-overflow:\s*ellipsis/)
  assert.match(source, /white-space:\s*nowrap/)
  assert.match(source, /workspace-select \{/)
  assert.match(source, /width:\s*min\(280px,\s*32vw\)/)
})

test('top-level content pages do not render duplicate PageHeader blocks', async () => {
  const [dashboardPage, pipelinePage, projectPage, agentPage, resourcesPage, deployPage, credentialsPage, statisticsPage, settingsPage] = await Promise.all([
    readView('dashboard/index.vue'),
    readView('pipeline/index.vue'),
    readView('project/index.vue'),
    readView('agent/index.vue'),
    readView('resources/index.vue'),
    readView('deploy/index.vue'),
    readView('credentials/index.vue'),
    readView('statistics/index.vue'),
    readView('settings/index.vue')
  ])

  for (const pageSource of [dashboardPage, pipelinePage, projectPage, agentPage, resourcesPage, deployPage, credentialsPage, statisticsPage, settingsPage]) {
    assert.doesNotMatch(pageSource, /<PageHeader>/)
  }
})

test('only App Store and AI Store keep StoreKindSwitch', async () => {
  const [switchSource, aiStore, appStore, templatePage, pipelinePage, deployPage, projectPage, resourcePage, credentialPage] = await Promise.all([
    readView('store/components/StoreKindSwitch.vue'),
    readView('store/ai-store.vue'),
    readView('store/components/AppStorePage.vue'),
    readView('store/components/StoreTemplatePage.vue'),
    readView('pipeline/index.vue'),
    readView('deploy/index.vue'),
    readView('project/index.vue'),
    readView('resources/index.vue'),
    readView('credentials/index.vue')
  ])

  assert.match(switchSource, /class="store-kind-switch"/)
  assert.match(aiStore, /StoreKindSwitch/)
  assert.match(appStore, /StoreKindSwitch/)
  assert.doesNotMatch(templatePage, /StoreKindSwitch/)

  for (const pageSource of [pipelinePage, deployPage, projectPage, resourcePage, credentialPage]) {
    assert.doesNotMatch(pageSource, /StoreKindSwitch/)
  }
})

test('template store page does not keep a local same-name page title block', async () => {
  const source = await readView('store/components/StoreTemplatePage.vue')

  assert.doesNotMatch(source, /<h1>商店<\/h1>/)
  assert.doesNotMatch(source, /<template\s+#title>[\s\S]*商店[\s\S]*<\/template>/)
})
