import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildMirrorSubmitPayload,
  deriveMirrorEditorText
} from './mirrorSettings.js'

test('deriveMirrorEditorText uses effective defaults when agent does not customize mirrors', () => {
  const text = deriveMirrorEditorText({
    dockerhub_mirrors_configured: false,
    dockerhub_mirrors: [],
    system_default_dockerhub_mirrors: ['https://mirror-a.example', 'https://mirror-b.example']
  })

  assert.equal(text, 'https://mirror-a.example\nhttps://mirror-b.example')
})

test('buildMirrorSubmitPayload treats unchanged default text as inheriting system defaults', () => {
  const payload = buildMirrorSubmitPayload({
    dockerhub_mirrors_text: 'https://mirror-a.example\nhttps://mirror-b.example',
    system_default_dockerhub_mirrors: ['https://mirror-a.example', 'https://mirror-b.example']
  })

  assert.deepEqual(payload, {
    dockerhub_mirrors_configured: false,
    dockerhub_mirrors: []
  })
})

test('buildMirrorSubmitPayload switches to custom mirrors when text differs from defaults', () => {
  const payload = buildMirrorSubmitPayload({
    dockerhub_mirrors_text: 'https://mirror-a.example\nhttps://mirror-c.example',
    system_default_dockerhub_mirrors: ['https://mirror-a.example', 'https://mirror-b.example']
  })

  assert.deepEqual(payload, {
    dockerhub_mirrors_configured: true,
    dockerhub_mirrors: ['https://mirror-a.example', 'https://mirror-c.example']
  })
})

test('buildMirrorSubmitPayload keeps explicit empty input as configured empty override', () => {
  const payload = buildMirrorSubmitPayload({
    dockerhub_mirrors_text: '',
    system_default_dockerhub_mirrors: ['https://mirror-a.example']
  })

  assert.deepEqual(payload, {
    dockerhub_mirrors_configured: true,
    dockerhub_mirrors: []
  })
})
