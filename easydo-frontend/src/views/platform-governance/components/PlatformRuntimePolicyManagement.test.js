import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(currentDir, 'PlatformRuntimePolicyManagement.vue'), 'utf8')

test('runtime policy management no longer exposes binding capabilities json', () => {
  assert.doesNotMatch(source, /Capabilities JSON/)
  assert.doesNotMatch(source, /capabilities_json/)
})

test('runtime policy management submits binding settings and metadata json', () => {
  assert.match(source, /settings_json: parseJsonField\('Settings', bindingForm\.settings_json, \{\}\)/)
  assert.match(source, /metadata_json: parseJsonField\('Metadata', bindingForm\.metadata_json, \{\}\)/)
  assert.match(source, /bindingForm\.metadata_json = row\?\.metadata_json \|\| '\{\}'/)
  assert.match(source, /provider_model_key: bindingForm\.provider_model_key/)
})
