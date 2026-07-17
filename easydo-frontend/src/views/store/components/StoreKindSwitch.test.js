import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('store kind switch uses compact store-switch segmented control styling', async () => {
  const source = await readFile(join(currentDir, 'StoreKindSwitch.vue'), 'utf8')

  assert.match(source, /class="store-switch"/)
  assert.match(source, /class="store-switch__option"/)
  assert.match(source, /\.store-switch\s*\{[\s\S]*border:\s*1px solid/)
  assert.match(source, /\.store-switch\s*\{[\s\S]*border-radius:/)
  assert.match(source, /\.store-switch__option\s*\{[\s\S]*min-height:\s*30px/)
  assert.match(source, /\.store-switch__option\.active\s*\{[\s\S]*background:\s*var\(--primary-color\)/)
  assert.doesNotMatch(source, /font-size:\s*28px/)
  assert.doesNotMatch(source, /store-kind-divider/)
})
