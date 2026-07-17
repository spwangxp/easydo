import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const chartDir = dirname(fileURLToPath(import.meta.url))

async function source(path) {
  return readFile(join(chartDir, path), 'utf8')
}

test('Helm chart declares AI Runtime as a durable multi-replica workload', async () => {
  const [values, deployment, service, helpers] = await Promise.all([
    source('values.yaml'),
    source('templates/ai-runtime-deployment.yaml'),
    source('templates/ai-runtime-service.yaml'),
    source('templates/_helpers.tpl')
  ])

  assert.match(values, /aiRuntime:\s*[\s\S]*replicaCount:\s*2/)
  assert.match(values, /repository:\s*easydo-ai-runtime/)
  assert.match(deployment, /AI_RUNTIME_INTERNAL_TOKEN/)
  assert.match(deployment, /AI_RUNTIME_DB_ENABLED/)
  assert.match(deployment, /AI_RUNTIME_EASYDO_SERVER_URL/)
  assert.match(deployment, /livenessProbe:/)
  assert.match(deployment, /readinessProbe:/)
  assert.match(deployment, /path:\s*\/readyz/)
  assert.doesNotMatch(deployment, /replicaCount.*must be 1/)
  assert.match(deployment, /AI_RUNTIME_INSTANCE_ID/)
  assert.match(deployment, /fieldPath:\s*metadata\.name/)
  assert.match(deployment, /AI_RUNTIME_RUN_LEASE_MS/)
  assert.match(deployment, /AI_RUNTIME_WORKSPACE_DOCKER_HOST/)
  assert.match(deployment, /AI_RUNTIME_WORKSPACE_IMAGE/)
  assert.doesNotMatch(deployment, /docker\.sock|hostPath:|privileged:\s*true/)
  assert.match(service, /app\.kubernetes\.io\/component:\s*ai-runtime/)
  assert.match(helpers, /define "easydo\.aiRuntimeFullname"/)
})

test('Helm Server and proxy configuration wire AI Runtime and long-lived SSE', async () => {
  const [server, frontend, ingress, secret] = await Promise.all([
    source('templates/server-deployment.yaml'),
    source('templates/frontend-configmap.yaml'),
    source('templates/ingress.yaml'),
    source('templates/runtime-secret.yaml')
  ])

  assert.match(server, /AI_RUNTIME_BASE_URL/)
  assert.match(server, /AI_RUNTIME_INTERNAL_TOKEN/)
  assert.match(server, /secretKeyRef:/)
  assert.match(frontend, /agent-chatbox\|sessions\|actions\|runs/)
  assert.match(frontend, /proxy_buffering off;/)
  assert.match(frontend, /proxy_read_timeout 3600s;/)
  assert.match(ingress, /nginx\.ingress\.kubernetes\.io\/proxy-read-timeout/)
  assert.match(ingress, /nginx\.ingress\.kubernetes\.io\/proxy-buffering/)
  assert.match(secret, /kind:\s*Secret/)
  assert.match(secret, /ai-runtime-internal-token:/)
})
