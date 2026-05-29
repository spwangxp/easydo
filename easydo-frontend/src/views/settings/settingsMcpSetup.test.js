import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(currentDir, 'index.vue'), 'utf8')

test('renders EasyDo MCP section inside integrations content', () => {
  assert.match(source, /activeMenu === 'integrations'/)
  assert.match(source, /<h2 class="section-title">第三方集成<\/h2>/)
  assert.match(source, /EasyDo MCP 接入/)
  assert.match(source, /workspace_id/)
  assert.match(source, /复制完整片段/)
})

test('contains explicit empty-state and token-unavailable branches', () => {
  assert.match(source, /请先在顶部切换到一个工作空间/)
  assert.match(source, /当前 token 获取失败/)
  assert.match(source, /!hasReadableToken/)
})

test('does not add a new sidebar item for MCP setup', () => {
  assert.doesNotMatch(source, /key:\s*'mcp'/)
  assert.doesNotMatch(source, /name:\s*'EasyDo MCP 接入'/)
})
