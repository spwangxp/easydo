import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildDisplayedWebhookURL } from './triggerWebhookDisplay.js'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('detail view keeps webhook url and secret token copy controls wired', async () => {
  const source = await readFile(join(currentDir, 'detail.vue'), 'utf8')

  assert.match(source, /:model-value="displayedWebhookURL"/)
  assert.match(source, /:disabled="!canCopyWebhookURL"/)
  assert.match(source, /@click="copyTriggerField\(displayedWebhookURL\)"/)
  assert.match(source, /:model-value="displaySecretToken"/)
  assert.match(source, /:disabled="!canCopySecretToken"/)
  assert.match(source, /@click="copyTriggerField\(displaySecretToken\)"/)
  assert.match(source, /@click="handleRotateTriggerSecret"/)
})

test('buildDisplayedWebhookURL trims trailing slashes from origin', () => {
  assert.equal(
    buildDisplayedWebhookURL({
      origin: ' https://example.com/// ',
      webhookToken: 'token-123'
    }),
    'https://example.com/api/pipeline/run/webhook/token-123'
  )
})

test('detail view loads task definitions during initial mount so trigger settings can render runtime mappings', async () => {
  const source = await readFile(join(currentDir, 'detail.vue'), 'utf8')

  assert.match(source, /onMounted\(\(\) => \{[\s\S]*fetchPipelineDetail\(\)[\s\S]*fetchPipelineTaskDefinitions\(\)[\s\S]*fetchTriggerSettings\(\)/)
})
