import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(resolve(currentDir, 'RuntimeProfileManagement.vue'), 'utf8')
const apiSource = readFileSync(resolve(currentDir, '../../../api/agent.js'), 'utf8')

test('runtime profile management does not depend on agent selection state', () => {
  assert.doesNotMatch(viewSource, /activeAgentId/)
  assert.doesNotMatch(viewSource, /选择 AI Agent/)
})

test('workspace runtime profile api uses workspace-level routes', () => {
  assert.match(apiSource, /url:\s*'\/ai\/runtime-profiles'/)
  assert.match(apiSource, /url:\s*`\/ai\/runtime-profiles\/\$\{id\}`/)
  assert.doesNotMatch(apiSource, /\/ai\/agents\/\$\{id\}\/runtime-profiles/)
})

test('runtime profile list renders normalized binding priority text', () => {
  assert.match(viewSource, /bindingPriorityText\(row\.binding_priority_json\)/)
  assert.doesNotMatch(viewSource, /<el-table-column prop="binding_priority_json" label="模型绑定顺序"/)
})

test('runtime profile referenced agent lookup supports nested runtime profile relations and camel aliases', () => {
  assert.match(viewSource, /runtimeProfileRelationId\(agent\)/)
  assert.match(viewSource, /return record\?\.runtime_profile_id \?\? record\?\.runtimeProfileID \?\? record\?\.runtime_profile\?\.id \?\? null/)
})

test('runtime profile management extracts wrapped list payloads and model aliases', () => {
  assert.match(viewSource, /function extractList\(payload\)/)
  assert.match(viewSource, /payload\?\.models/)
  assert.match(viewSource, /payload\?\.runtimeProfiles/)
  assert.match(viewSource, /payload\?\.runtime_profiles/)
  assert.match(viewSource, /runtimeProfileModelId\(row\)/)
  assert.match(viewSource, /return record\?\.model_id \?\? record\?\.modelID \?\? record\?\.model\?\.id \?\? null/)
})

test('runtime profile submit validates binding priority array and runtime settings object', () => {
  assert.match(viewSource, /Array\.isArray\(bindingPriority\)/)
  assert.match(viewSource, /isPlainObject\(runtimeSettings\)/)
  assert.match(viewSource, /模型绑定顺序\(JSON\) 必须是数组/)
  assert.match(viewSource, /运行参数\(JSON\) 必须是对象/)
})