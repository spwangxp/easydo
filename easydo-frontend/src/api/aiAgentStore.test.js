import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('ai agent store api uses generic agent profile routes', async () => {
  const source = await readFile(join(currentDir, 'aiAgentStore.js'), 'utf8')

  assert.match(source, /url:\s*'\/store\/ai-agents\/profiles'/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/profiles\/\$\{id\}`/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/profiles\/\$\{id\}\/publish`/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/profiles\/\$\{id\}\/validate`/)
  assert.match(source, /getAgentProfileDependencies/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/profiles\/\$\{id\}\/dependencies`/)
  assert.match(source, /url:\s*'\/store\/ai-agents\/resources'/)
  assert.match(source, /createAgentResource/)
  assert.match(source, /updateAgentResource/)
  assert.match(source, /deleteAgentResource/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/resources\/\$\{id\}`/)
  assert.match(source, /scanAgentResource/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/resources\/\$\{id\}\/scan`/)
  assert.match(source, /probeMcpResource/)
  assert.match(source, /\/store\/ai-agents\/resources\/mcp\/probe/)
  assert.match(source, /listAgentResourceVersions/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/resources\/\$\{id\}\/versions`/)
  assert.match(source, /getAgentResourceDependencies/)
  assert.match(source, /url:\s*`\/store\/ai-agents\/resources\/\$\{id\}\/dependencies`/)
  assert.match(source, /getAgentRuntimeOperationsSummary/)
  assert.match(source, /url:\s*'\/store\/ai-agents\/operations\/summary'/)
  assert.doesNotMatch(source, /scene-bindings/)
  assert.doesNotMatch(source, /listSceneBindings/)
  assert.doesNotMatch(source, /saveSceneBindings/)
  assert.doesNotMatch(source, /runtime[-_]profile/i)
  assert.doesNotMatch(source, /url:\s*'\/ai\/agents'/)
})
