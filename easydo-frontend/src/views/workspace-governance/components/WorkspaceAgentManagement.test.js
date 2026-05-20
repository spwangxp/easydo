import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(currentDir, 'WorkspaceAgentManagement.vue'), 'utf8')

test('agent form no longer depends on scenario ownership', () => {
  assert.doesNotMatch(source, /scenario/)
})

test('agent form posts runtime profile and runtime definition fields', () => {
  assert.match(source, /runtime_profile_id/)
  assert.match(source, /user_prompt_template/)
  assert.match(source, /input_schema_json/)
  assert.match(source, /output_schema_json/)
  assert.match(source, /tool_policy_json/)
  assert.match(source, /tools_json/)
  assert.match(source, /skills_json/)
  assert.match(source, /memory_json/)
  assert.match(source, /mcp_servers_json/)
  assert.match(source, /sub_agents_json/)
  assert.match(source, /metadata_json/)
})

test('agent form sends null when runtime profile is cleared', () => {
  assert.match(source, /runtime_profile_id:\s*normalizeNullableId\(agentForm\.runtime_profile_id\)\s*\?\?\s*null/)
  assert.doesNotMatch(source, /runtime_profile_id:\s*Number\(agentForm\.runtime_profile_id\)\s*\|\|\s*0/)
})

test('agent edit form hydrates runtime profile id from normalized relation helper', () => {
  assert.match(source, /agentForm\.runtime_profile_id = normalizeNullableId\(runtimeProfileRelationId\(row\)\)/)
  assert.match(source, /agentForm\.user_prompt_template = row\.user_prompt_template \|\| ''/)
  assert.match(source, /agentForm\.input_schema_json = formatJsonText\(row\.input_schema_json, \{\}\)/)
  assert.match(source, /agentForm\.output_schema_json = formatJsonText\(row\.output_schema_json, \{\}\)/)
  assert.match(source, /agentForm\.tool_policy_json = formatJsonText\(row\.tool_policy_json, \{\}\)/)
  assert.match(source, /return record\?\.runtime_profile_id \?\? record\?\.runtimeProfileID \?\? record\?\.runtime_profile\?\.id \?\? null/)
})

test('agent form parses and submits runtime definition json', () => {
  assert.match(source, /const inputSchema = parseJsonField\(agentForm\.input_schema_json, \{\}, 'Input Schema JSON'\)/)
  assert.match(source, /const outputSchema = parseJsonField\(agentForm\.output_schema_json, \{\}, 'Output Schema JSON'\)/)
  assert.match(source, /const toolPolicy = parseJsonField\(agentForm\.tool_policy_json, \{\}, 'Tool Policy JSON'\)/)
  assert.match(source, /user_prompt_template: agentForm\.user_prompt_template/)
  assert.match(source, /input_schema_json: inputSchema/)
  assert.match(source, /output_schema_json: outputSchema/)
  assert.match(source, /tool_policy_json: toolPolicy/)
})

test('agent runtime profile display prefers loaded profile map before nested relation fallback', () => {
  assert.match(source, /const profileId = runtimeProfileRelationId\(row\) \?\? ''/)
  assert.match(source, /const profile = runtimeProfileById\.value\.get\(String\(profileId\)\)/)
})

test('agent management extracts wrapped list payloads for agents and runtime profiles', () => {
  assert.match(source, /function extractList\(payload\)/)
  assert.match(source, /payload\?\.agents/)
  assert.match(source, /payload\?\.runtimeProfiles/)
  assert.match(source, /payload\?\.runtime_profiles/)
})