import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildChartSourcePayload,
  normalizeParameterRows,
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
