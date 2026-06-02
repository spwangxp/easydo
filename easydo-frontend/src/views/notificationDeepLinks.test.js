import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

const readSource = (relativePath) => {
  return readFileSync(resolve(currentDir, relativePath), 'utf8')
}

test('pipeline email link opens the referenced run execution view', () => {
  const source = readSource('pipeline/detail.vue')

  assert.match(source, /const targetRunIDFromQuery = computed\(\(\) => Number\(route\.query\.run_id \|\| 0\)\)/)
  assert.match(source, /const resolveQueryRunFromHistory = \(\) =>/)
  assert.match(source, /runHistory\.value\.find\(run => Number\(run\?\.id \|\| 0\) === targetRunIDFromQuery\.value\)/)
  assert.match(source, /await openRunExecutionView\(targetRun\)/)
  assert.match(source, /watch\(targetRunIDFromQuery/)
})

test('agent email link opens the referenced agent detail dialog', () => {
  const source = readSource('agent/index.vue')

  assert.match(source, /import \{ useRoute \} from 'vue-router'/)
  assert.match(source, /const targetAgentIDFromQuery = computed\(\(\) => Number\(route\.query\.agent_id \|\| 0\)\)/)
  assert.match(source, /const openAgentDetailFromQuery = async \(\) =>/)
  assert.match(source, /await handleDetail\(\{ id: targetAgentIDFromQuery\.value \}\)/)
  assert.match(source, /watch\(targetAgentIDFromQuery/)
})

test('deployment email link opens the referenced deployment detail drawer', () => {
  const source = readSource('deploy/index.vue')

  assert.match(source, /const targetRequestIDFromQuery = computed\(\(\) => Number\(route\.query\.request_id \|\| 0\)\)/)
  assert.match(source, /const openDeploymentDetailFromQuery = async \(\) =>/)
  assert.match(source, /deployList\.value\.find\(item => Number\(item\?\.id \|\| 0\) === targetRequestIDFromQuery\.value\)/)
  assert.match(source, /await handleView\(targetDeployment\)/)
  assert.match(source, /watch\(targetRequestIDFromQuery/)
})
