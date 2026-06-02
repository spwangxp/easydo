import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(currentDir, 'AuditLogPanel.vue'), 'utf8')

test('audit log target id filter stays empty until the user enters a value', () => {
  assert.match(source, /<el-input\s+v-model="filters\.target_id"/)
  assert.doesNotMatch(source, /<el-input-number\s+v-model="filters\.target_id"/)
  assert.match(source, /const targetID = Number\(filters\.target_id\)/)
  assert.match(source, /if \(Number\.isInteger\(targetID\) && targetID > 0\) params\.target_id = targetID/)
})
