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
