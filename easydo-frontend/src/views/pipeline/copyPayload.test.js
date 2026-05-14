import test from 'node:test'
import assert from 'node:assert/strict'
import { buildPipelineCopyPayload } from './copyPayload.js'

const parseDefinitionJson = (payload) => JSON.parse(payload.definition_json)

test('buildPipelineCopyPayload copies core fields and removes webhook triggers only', () => {
  const sourcePipeline = {
    name: 'Release Pipeline',
    description: 'deploy to production',
    project_id: 42,
    environment: 'production',
    definition_json: JSON.stringify({
      version: '2.0',
      nodes: [{ node_id: 'node_1', node_name: 'Build' }],
      triggers: [
        { type: 'cron', cron: '0 0 * * *', enabled: true },
        { type: 'webhook', path: '/hooks/release', secret: 'keep-out' },
        { type: 'manual', label: 'Manual run' }
      ],
      metadata: { version: '2.0' }
    }),
    config: JSON.stringify({
      version: 'config-only',
      triggers: [{ type: 'webhook', path: '/wrong-source' }]
    })
  }

  const payload = buildPipelineCopyPayload(sourcePipeline)

  assert.deepEqual(payload, {
    name: 'Release Pipeline-copy',
    description: 'deploy to production',
    project_id: 42,
    environment: 'production',
    definition_json: payload.definition_json
  })
  assert.deepEqual(parseDefinitionJson(payload), {
    version: '2.0',
    nodes: [{ node_id: 'node_1', node_name: 'Build' }],
    triggers: [
      { type: 'cron', cron: '0 0 * * *', enabled: true },
      { type: 'manual', label: 'Manual run' }
    ],
    metadata: { version: '2.0' }
  })
})

test('buildPipelineCopyPayload does not use config when definition_json is present', () => {
  const payload = buildPipelineCopyPayload({
    name: 'Config Guard',
    description: 'uses definition_json only',
    project_id: 1,
    environment: 'development',
    definition_json: JSON.stringify({
      version: 'definition-source',
      triggers: [{ type: 'manual' }]
    }),
    config: JSON.stringify({
      version: 'config-source',
      triggers: [{ type: 'webhook' }]
    })
  })

  assert.deepEqual(parseDefinitionJson(payload), {
    version: 'definition-source',
    triggers: [{ type: 'manual' }]
  })
})

test('buildPipelineCopyPayload throws on missing, blank, or malformed definition_json', () => {
  assert.throws(() => buildPipelineCopyPayload({ name: 'Missing' }), /definition_json is required/)
  assert.throws(() => buildPipelineCopyPayload({ name: 'Blank', definition_json: '   ' }), /definition_json is required/)
  assert.throws(() => buildPipelineCopyPayload({ name: 'Bad JSON', definition_json: '{' }), /definition_json must be valid JSON/)
  assert.throws(() => buildPipelineCopyPayload({ name: 'Wrong Shape', definition_json: '[]' }), /definition_json must be a JSON object/)
})

test('buildPipelineCopyPayload throws when source pipeline name is missing or blank', () => {
  assert.throws(
    () => buildPipelineCopyPayload({
      description: 'missing name',
      project_id: 1,
      environment: 'dev',
      definition_json: JSON.stringify({ version: '2.0' })
    }),
    /source pipeline name is required/
  )
  assert.throws(
    () => buildPipelineCopyPayload({
      name: '   ',
      description: 'blank name',
      project_id: 1,
      environment: 'dev',
      definition_json: JSON.stringify({ version: '2.0' })
    }),
    /source pipeline name is required/
  )
})

test('buildPipelineCopyPayload throws when triggers is present but not an array', () => {
  assert.throws(
    () => buildPipelineCopyPayload({
      name: 'Bad Triggers',
      description: 'invalid triggers',
      project_id: 1,
      environment: 'dev',
      definition_json: JSON.stringify({
        version: '2.0',
        triggers: { type: 'webhook' }
      })
    }),
    /definition_json\.triggers must be an array when present/
  )
})

test('buildPipelineCopyPayload rejects legacy config-only pipelines', () => {
  assert.throws(
    () => buildPipelineCopyPayload({
      name: 'Legacy',
      config: JSON.stringify({ version: 'legacy' })
    }),
    /definition_json is required/
  )
})

test('buildPipelineCopyPayload keeps the copied name as source-name-copy without extra renaming', () => {
  const payload = buildPipelineCopyPayload({
    name: 'Already Stable',
    description: '',
    project_id: 7,
    environment: 'testing',
    definition_json: JSON.stringify({
      version: '2.0',
      triggers: []
    })
  })

  assert.equal(payload.name, 'Already Stable-copy')
  assert.equal(payload.description, '')
  assert.equal(payload.project_id, 7)
  assert.equal(payload.environment, 'testing')
})
