import test from 'node:test'
import assert from 'node:assert/strict'
import {
  PLACEHOLDER_VALUE,
  buildDisplayedWebhookURL,
  buildDisplayedSecretToken,
  canCopyDisplayValue,
  copyDisplayedValue
} from './triggerWebhookDisplay.js'

test('buildDisplayedWebhookURL prefers browser origin and webhook token', () => {
  assert.equal(
    buildDisplayedWebhookURL({
      origin: 'http://10.159.69.131:30994',
      webhookToken: 'abc-token',
      fallbackURL: 'http://localhost/api/pipeline/run/webhook/abc-token'
    }),
    'http://10.159.69.131:30994/api/pipeline/run/webhook/abc-token'
  )
})

test('buildDisplayedWebhookURL falls back to backend url when token is missing', () => {
  assert.equal(
    buildDisplayedWebhookURL({
      origin: 'http://10.159.69.131:30994',
      webhookToken: '',
      fallbackURL: 'http://example.com/api/pipeline/run/webhook/fallback'
    }),
    'http://example.com/api/pipeline/run/webhook/fallback'
  )
})

test('buildDisplayedWebhookURL returns placeholder when token and fallback are missing', () => {
  assert.equal(buildDisplayedWebhookURL({ origin: 'http://10.159.69.131:30994' }), PLACEHOLDER_VALUE)
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

test('buildDisplayedSecretToken prefers secret token then webhook token', () => {
  assert.equal(buildDisplayedSecretToken({ secretToken: 'secret', webhookToken: 'token' }), 'secret')
  assert.equal(buildDisplayedSecretToken({ secretToken: '', webhookToken: 'token' }), 'token')
  assert.equal(buildDisplayedSecretToken({ secretToken: '', webhookToken: '' }), PLACEHOLDER_VALUE)
})

test('canCopyDisplayValue disables copy for placeholder', () => {
  assert.equal(canCopyDisplayValue(PLACEHOLDER_VALUE), false)
  assert.equal(canCopyDisplayValue('real-value'), true)
})

test('copyDisplayedValue writes displayed webhook url and reports success', async () => {
  const calls = []
  await copyDisplayedValue({
    value: 'http://10.159.69.131:30994/api/pipeline/run/webhook/abc',
    canCopy: () => true,
    writeText: async (text) => calls.push(text),
    onSuccess: (msg) => calls.push(msg),
    onError: () => calls.push('error')
  })

  assert.deepEqual(calls, [
    'http://10.159.69.131:30994/api/pipeline/run/webhook/abc',
    '复制成功'
  ])
})

test('copyDisplayedValue writes displayed secret token and reports success', async () => {
  const calls = []
  await copyDisplayedValue({
    value: 'secret-token',
    canCopy: () => true,
    writeText: async (text) => calls.push(text),
    onSuccess: (msg) => calls.push(msg),
    onError: () => calls.push('error')
  })

  assert.deepEqual(calls, [
    'secret-token',
    '复制成功'
  ])
})

test('copyDisplayedValue reports failure when clipboard write throws', async () => {
  const calls = []
  await copyDisplayedValue({
    value: 'secret-token',
    canCopy: () => true,
    writeText: async () => {
      throw new Error('denied')
    },
    onSuccess: () => calls.push('success'),
    onError: (msg) => calls.push(msg)
  })

  assert.deepEqual(calls, ['复制失败'])
})

test('copyDisplayedValue skips clipboard work when value is placeholder', async () => {
  let wrote = false
  let successCalled = false
  let errorCalled = false

  const copied = await copyDisplayedValue({
    value: PLACEHOLDER_VALUE,
    canCopy: () => false,
    writeText: async () => {
      wrote = true
    },
    onSuccess: () => {
      successCalled = true
    },
    onError: () => {
      errorCalled = true
    }
  })

  assert.equal(copied, false)
  assert.equal(wrote, false)
  assert.equal(successCalled, false)
  assert.equal(errorCalled, false)
})
