import test from 'node:test'
import assert from 'node:assert/strict'
import { gzipSync } from 'fflate'

import {
  buildChartSourcePayload,
  buildRepoChartDownloadURL,
  deriveChartNameFromOCIUrl,
  extractHelmChartDownloadURL,
  parseOCIChartReference,
  normalizeParameterRows,
  normalizeChartSourcePayload,
  resolveChartSource,
  resolveUploadChart,
  splitParametersByAdvanced
} from './appStoreHelpers.js'

test('normalizeParameterRows sorts rows and parses option values', () => {
  const rows = normalizeParameterRows([
    { name: 'service.type', type: 'select', option_values: '["ClusterIP","LoadBalancer"]', sort_order: 2 },
    { name: 'release_name', advanced: false, sort_order: 1 }
  ])

  assert.equal(rows[0].name, 'release_name')
  assert.deepEqual(rows[1].option_values, ['ClusterIP', 'LoadBalancer'])
})

test('splitParametersByAdvanced separates basic and advanced rows', () => {
  const grouped = splitParametersByAdvanced([
    { name: 'release_name', advanced: false },
    { name: 'master.count', advanced: true }
  ])

  assert.equal(grouped.basic.length, 1)
  assert.equal(grouped.advanced.length, 1)
  assert.equal(grouped.advanced[0].name, 'master.count')
})

test('buildChartSourcePayload keeps upload metadata only for upload source', () => {
  const repoPayload = buildChartSourcePayload({
    infra_type: 'k8s',
    chart_source_type: 'repo',
    chart_repo_url: 'https://charts.bitnami.com/bitnami',
    chart_name: 'redis',
    chart_version: '19.6.0'
  })
  assert.equal(repoPayload.object_key, undefined)

  const uploadPayload = buildChartSourcePayload({
    infra_type: 'k8s',
    chart_source_type: 'upload',
    chart_name: 'redis',
    chart_version: '19.6.0',
    chart_file_name: 'redis-19.6.0.tgz',
    chart_object_key: 'store/charts/workspace-1/app-1/version-1/redis-19.6.0.tgz'
  })
  assert.equal(uploadPayload.file_name, 'redis-19.6.0.tgz')
  assert.equal(uploadPayload.object_key, 'store/charts/workspace-1/app-1/version-1/redis-19.6.0.tgz')
})

test('normalizeChartSourcePayload derives chart name from oci url', () => {
  const payload = normalizeChartSourcePayload({
    infra_type: 'k8s',
    chart_source_type: 'oci',
    chart_oci_url: 'oci://registry-1.docker.io/bitnamicharts/mysql',
    chart_version: '14.0.3'
  })

  assert.equal(payload.chart_name, 'mysql')
  assert.equal(payload.oci_url, 'oci://registry-1.docker.io/bitnamicharts/mysql')
})

test('deriveChartNameFromOCIUrl returns trailing chart segment', () => {
  assert.equal(deriveChartNameFromOCIUrl('oci://registry-1.docker.io/bitnamicharts/mysql'), 'mysql')
})

test('resolveUploadChart extracts values yaml from chart tgz', async () => {
  const chartBytes = createSampleChartArchive()
  const file = new File([chartBytes], 'mysql-14.0.3.tgz', { type: 'application/gzip' })

  const resolved = await resolveUploadChart(file)

  assert.equal(resolved.chartName, 'mysql-14.0.3')
  assert.match(resolved.valuesYAML, /auth:/)
  assert.match(resolved.valuesYAML, /enabled: true/)
})

test('extractHelmChartDownloadURL resolves relative repo chart urls', () => {
  const indexText = `apiVersion: v1
entries:
  mysql:
    - version: 14.0.3
      urls:
        - charts/mysql-14.0.3.tgz
`

  const url = extractHelmChartDownloadURL(indexText, {
    repoUrl: 'https://charts.bitnami.com/bitnami',
    chartName: 'mysql',
    chartVersion: '14.0.3'
  })

  assert.equal(url, 'https://charts.bitnami.com/bitnami/charts/mysql-14.0.3.tgz')
})

test('buildRepoChartDownloadURL keeps absolute urls unchanged', () => {
  assert.equal(
    buildRepoChartDownloadURL('https://charts.bitnami.com/bitnami', 'https://cdn.example.com/mysql.tgz'),
    'https://cdn.example.com/mysql.tgz'
  )
})

test('parseOCIChartReference splits registry and repository path', () => {
  const parsed = parseOCIChartReference('oci://registry-1.docker.io/bitnamicharts/mysql', '14.0.3')

  assert.equal(parsed.registry, 'registry-1.docker.io')
  assert.equal(parsed.repository, 'bitnamicharts/mysql')
  assert.equal(parsed.reference, '14.0.3')
})

test('resolveChartSource resolves repo source and extracts values yaml', async () => {
  const chartBytes = createSampleChartArchive()
  const chartResponse = makeResponse({ arrayBuffer: async () => chartBytes.buffer.slice(0) })
  const indexResponse = makeResponse({
    text: async () => `apiVersion: v1
entries:
  mysql:
    - version: 14.0.3
      urls:
        - charts/mysql-14.0.3.tgz
`
  })
  const calls = []
  const fetchImpl = async (url) => {
    calls.push(url)
    if (String(url).endsWith('/index.yaml')) {
      return indexResponse
    }
    return chartResponse
  }

  const resolved = await resolveChartSource({
    sourceType: 'repo',
    repoUrl: 'https://charts.bitnami.com/bitnami',
    chartName: 'mysql',
    chartVersion: '14.0.3'
  }, { fetchImpl })

  assert.equal(calls[0], 'https://charts.bitnami.com/bitnami/index.yaml')
  assert.equal(calls[1], 'https://charts.bitnami.com/bitnami/charts/mysql-14.0.3.tgz')
  assert.equal(resolved.chartName, 'mysql')
  assert.equal(resolved.chartVersion, '14.0.3')
  assert.match(resolved.valuesYAML, /auth:/)
  assert.equal(resolved.chartFile.name, 'mysql-14.0.3.tgz')
})

test('resolveChartSource resolves OCI source and extracts values yaml', async () => {
  const chartBytes = createSampleChartArchive()
  const tokenResponse = makeResponse({
    json: async () => ({ token: 'public-token' })
  })
  const manifestResponse = makeResponse({
    json: async () => ({
      layers: [{
        mediaType: 'application/vnd.cncf.helm.chart.content.v1.tar+gzip',
        digest: 'sha256:chart-layer'
      }]
    })
  })
  const blobResponse = makeResponse({ arrayBuffer: async () => chartBytes.buffer.slice(0) })
  const calls = []
  const fetchImpl = async (url) => {
    calls.push(String(url))
    if (String(url).includes('/token?')) {
      return tokenResponse
    }
    if (String(url).includes('/manifests/14.0.3')) {
      return manifestResponse
    }
    if (String(url).includes('/blobs/sha256:chart-layer')) {
      return blobResponse
    }
    throw new Error(`unexpected fetch ${url}`)
  }

  const resolved = await resolveChartSource({
    sourceType: 'oci',
    ociUrl: 'oci://registry-1.docker.io/bitnamicharts/mysql',
    chartVersion: '14.0.3'
  }, { fetchImpl })

  assert.ok(calls.some((item) => item.includes('auth.docker.io/token')))
  assert.ok(calls.some((item) => item.includes('/v2/bitnamicharts/mysql/manifests/14.0.3')))
  assert.ok(calls.some((item) => item.includes('/v2/bitnamicharts/mysql/blobs/sha256:chart-layer')))
  assert.equal(resolved.chartName, 'mysql')
  assert.equal(resolved.chartVersion, '14.0.3')
  assert.equal(resolved.chartFile.name, 'mysql-14.0.3.tgz')
  assert.match(resolved.valuesYAML, /auth:/)
})

function createSampleChartArchive() {
  const values = 'auth:\n  enabled: true\nprimary:\n  persistence:\n    enabled: false\n'
  const files = [{ name: 'mysql/values.yaml', content: values }]
  const tar = createTarArchive(files)
  return gzipSync(tar)
}

function createTarArchive(files) {
  const chunks = []
  files.forEach((file) => {
    const body = new TextEncoder().encode(file.content)
    const header = new Uint8Array(512)
    writeTarString(header, 0, 100, file.name)
    writeTarOctal(header, 100, 8, 0o644)
    writeTarOctal(header, 108, 8, 0)
    writeTarOctal(header, 116, 8, 0)
    writeTarOctal(header, 124, 12, body.length)
    writeTarOctal(header, 136, 12, Math.floor(Date.now() / 1000))
    for (let i = 148; i < 156; i += 1) {
      header[i] = 32
    }
    header[156] = '0'.charCodeAt(0)
    writeTarString(header, 257, 6, 'ustar')
    writeTarString(header, 263, 2, '00')
    writeTarString(header, 265, 32, 'easydo')
    writeTarString(header, 297, 32, 'easydo')
    const checksum = header.reduce((sum, value) => sum + value, 0)
    writeTarChecksum(header, 148, 8, checksum)
    chunks.push(header)
    chunks.push(body)
    const padding = (512 - (body.length % 512)) % 512
    if (padding > 0) {
      chunks.push(new Uint8Array(padding))
    }
  })
  chunks.push(new Uint8Array(1024))
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0)
  const result = new Uint8Array(total)
  let offset = 0
  chunks.forEach((chunk) => {
    result.set(chunk, offset)
    offset += chunk.length
  })
  return result
}

function writeTarString(target, offset, length, value) {
  const encoded = new TextEncoder().encode(value)
  target.set(encoded.slice(0, length), offset)
}

function writeTarOctal(target, offset, length, value) {
  const text = value.toString(8).padStart(length - 1, '0')
  writeTarString(target, offset, length - 1, text)
  target[offset + length - 1] = 0
}

function writeTarChecksum(target, offset, length, value) {
  const text = value.toString(8).padStart(length - 2, '0')
  writeTarString(target, offset, length - 2, text)
  target[offset + length - 2] = 0
  target[offset + length - 1] = 32
}

function makeResponse(overrides = {}) {
  return {
    ok: true,
    status: 200,
    headers: new Headers(),
    text: async () => '',
    json: async () => ({}),
    arrayBuffer: async () => new ArrayBuffer(0),
    ...overrides
  }
}
