const test = require('node:test')
const assert = require('node:assert/strict')
const { readFile } = require('node:fs/promises')
const { join } = require('node:path')

const currentDir = __dirname

function serviceBlock(source, serviceName) {
  const lines = source.split('\n')
  const start = lines.indexOf(`  ${serviceName}:`)
  assert.notEqual(start, -1, `missing ${serviceName} service`)
  const end = lines.findIndex((line, index) => index > start && (/^  [\w-]+:$/.test(line) || /^\w[\w-]*:$/.test(line)))
  return lines.slice(start, end === -1 ? lines.length : end).join('\n')
}

test('docker compose does not create a separate ai runtime migrate service', async () => {
  const source = await readFile(join(currentDir, 'docker-compose.yml'), 'utf8')
  const aiRuntime = serviceBlock(source, 'ai-runtime')

  assert.doesNotMatch(source, /^\s{2}ai-runtime-migrate:/m)
  assert.doesNotMatch(aiRuntime, /ai-runtime-migrate:[\s\S]*service_completed_successfully/)
  assert.doesNotMatch(source, /runtime-schema\.sql/)
  assert.match(source, /^\s{2}ai-runtime:/m)
  assert.match(source, /AI_RUNTIME_DB_ENABLED=true/)
})

test('ai runtime controls isolated workspace containers through the local Docker endpoint', async () => {
  const source = await readFile(join(currentDir, 'docker-compose.yml'), 'utf8')

  assert.match(source, /AI_RUNTIME_WORKSPACE_IMAGE=easydo-ai-workspace:latest/)
  assert.match(source, /\/var\/run\/docker\.sock:\/var\/run\/docker\.sock/)
  assert.match(source, /^\s{2}ai-workspace-image:/m)
  assert.match(source, /dockerfile: Dockerfile\.workspace/)
})

test('normal ai runtime startup builds the required workspace image first', async () => {
  const source = await readFile(join(currentDir, 'docker-compose.yml'), 'utf8')
  const aiRuntime = serviceBlock(source, 'ai-runtime')
  const imageBuilder = serviceBlock(source, 'ai-workspace-image')
  const violations = []

  if (!/depends_on:[\s\S]*ai-workspace-image:\s*\n\s+condition: service_completed_successfully/.test(aiRuntime)) {
    violations.push('ai-runtime must depend on ai-workspace-image with service_completed_successfully')
  }
  if (/^\s+profiles:/m.test(imageBuilder)) {
    violations.push('ai-workspace-image must be available without activating a special profile')
  }
  assert.deepEqual(violations, [])
})
