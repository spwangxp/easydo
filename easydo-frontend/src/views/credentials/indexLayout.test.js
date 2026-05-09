import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

async function readView() {
  return readFile(join(currentDir, 'index.vue'), 'utf8')
}

test('credentials list uses compact separated columns and streamlined actions', async () => {
  const source = await readView()

  assert.match(source, /<el-table[^>]*:fit="true"/)
  assert.match(source, /<el-table-column prop="name" label="名称" min-width="220"/)
  assert.match(source, /<el-table-column prop="type" label="类型" min-width="120"/)
  assert.match(source, /<el-table-column prop="category" label="分类" min-width="120"/)
  assert.doesNotMatch(source, /label="范围"/)
  assert.doesNotMatch(source, /label="使用次数"/)
  assert.doesNotMatch(source, /label="最后使用"/)
  assert.match(source, /<el-table-column label="影响" min-width="120"/)
  assert.match(source, /@click="showImpact\(row\)"/)
  assert.match(source, /<el-button v-if="row\.can_edit" type="primary" link @click="handleEdit\(row\)">编辑<\/el-button>/)
  assert.match(source, /<el-button v-if="row\.can_verify" type="success" link @click="handleVerify\(row\)">验证<\/el-button>/)
  assert.match(source, /<el-dropdown/)
})

test('credentials list shows unavailable impact state instead of fake zeros when batch impact data is missing', async () => {
  const source = await readView()

  assert.match(source, /const impactSummaryUnavailable = '-- \/ --'/)
  assert.match(source, /if \(!impact\) return impactSummaryUnavailable/)
  assert.match(source, /if \(!impact\) return '影响数据暂不可用'/)
})

test('credentials list formats updated time separately from last used time', async () => {
  const source = await readView()

  assert.match(source, /\{\{ formatUpdatedAt\(row\.updated_at\) \}\}/)
  assert.match(source, /\{\{ formatLastUsedAt\(usageData\.last_used_at \? usageData\.last_used_at \* 1000 : null\) \}\}/)
  assert.match(source, /function formatUpdatedAt\(value\) \{/)
  assert.match(source, /if \(!value\) return '-'/)
  assert.match(source, /function formatLastUsedAt\(value\) \{/)
  assert.match(source, /if \(!value\) return '从未使用'/)
})

test('credentials list clears table selection state when cancelling batch actions', async () => {
  const source = await readView()

  assert.match(source, /<el-button link @click="clearBatchSelection">取消<\/el-button>/)
  assert.match(source, /ref="credentialsTableRef"/)
  assert.match(source, /const credentialsTableRef = ref\(null\)/)
  assert.match(source, /credentialsTableRef\.value\?\.clearSelection\?\.\(\)/)
})

test('credentials list keeps info and action columns as adaptive peers without fixed overlay', async () => {
  const source = await readView()

  assert.match(source, /<el-table[^>]*:fit="true"/)
  assert.match(source, /<el-table-column prop="type" label="类型" min-width="120"/)
  assert.match(source, /<el-table-column prop="category" label="分类" min-width="120"/)
  assert.match(source, /<el-table-column prop="status" label="状态" min-width="100"/)
  assert.match(source, /<el-table-column prop="lock_state" label="锁定状态" min-width="100"/)
  assert.match(source, /<el-table-column label="影响" min-width="120"/)
  assert.match(source, /<el-table-column prop="updated_at" label="更新时间" min-width="160"/)
  assert.match(source, /<el-table-column label="操作" min-width="160" align="right"/)
  assert.doesNotMatch(source, /fixed="right"/)
})
