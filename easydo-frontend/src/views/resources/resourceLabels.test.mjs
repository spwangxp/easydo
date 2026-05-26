import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import {
  formatResourceLabel,
  getResourceLabelOptions,
  labelObjectToRows,
  labelRowsToObject,
  normalizeResourceLabels,
  resourceMatchesLabelFilters,
  validateResourceLabelRows
} from './resourceLabels.js'

assert.deepEqual(normalizeResourceLabels(null), {})
assert.deepEqual(normalizeResourceLabels(''), {})
assert.deepEqual(normalizeResourceLabels('{" team ":" platform ","role":"gpu"}'), { team: 'platform', role: 'gpu' })
assert.deepEqual(normalizeResourceLabels('["bad"]'), {})
assert.deepEqual(normalizeResourceLabels({ team: ' platform ', nested: { bad: true }, empty: ' ' }), { team: 'platform' })

assert.deepEqual(labelObjectToRows({ team: 'platform', role: 'gpu' }), [
  { key: 'team', value: 'platform' },
  { key: 'role', value: 'gpu' }
])

assert.deepEqual(labelRowsToObject([
  { key: ' team ', value: ' platform ' },
  { key: '', value: 'ignored' },
  { key: 'empty', value: ' ' }
]), {
  team: 'platform'
})

assert.deepEqual(validateResourceLabelRows([
  { key: 'team', value: 'platform' }
]), {
  ok: true,
  labels: { team: 'platform' },
  errors: []
})

assert.equal(validateResourceLabelRows([
  { key: 'team', value: '' }
]).ok, false)
assert.equal(validateResourceLabelRows([
  { key: ' team ', value: 'platform' },
  { key: 'team', value: 'gpu' }
]).ok, false)

const resources = [
  { labels: { team: 'platform', role: 'gpu', region: 'cn' } },
  { labels: { team: 'app', role: 'cpu' } },
  { labels: '{"team":"platform","region":"us"}' },
  { labels: null }
]

assert.deepEqual(getResourceLabelOptions(resources).keys, ['region', 'role', 'team'])
assert.deepEqual(getResourceLabelOptions(resources, 'team').values, ['app', 'platform'])

assert.equal(resourceMatchesLabelFilters({ labels: { team: 'platform', role: 'gpu' } }, { labelKey: '', labelValue: '' }), true)
assert.equal(resourceMatchesLabelFilters({ labels: { team: 'platform', role: 'gpu' } }, { labelKey: 'team', labelValue: '' }), true)
assert.equal(resourceMatchesLabelFilters({ labels: { team: 'platform', role: 'gpu' } }, { labelKey: 'missing', labelValue: '' }), false)
assert.equal(resourceMatchesLabelFilters({ labels: { team: 'platform', role: 'gpu' } }, { labelKey: 'team', labelValue: 'platform' }), true)
assert.equal(resourceMatchesLabelFilters({ labels: { team: 'platform', role: 'gpu' } }, { labelKey: 'team', labelValue: 'cpu' }), false)
assert.equal(resourceMatchesLabelFilters({ labels: { team: 'platform', role: 'gpu' } }, { labelValue: 'gpu' }), true)

const rowsWithOnlyFiftyActiveLabels = Array.from({ length: 51 }, (_, index) => ({
  key: index < 50 ? `key-${index}` : '',
  value: index < 50 ? `value-${index}` : ''
}))
assert.deepEqual(validateResourceLabelRows(rowsWithOnlyFiftyActiveLabels), {
  ok: true,
  labels: Object.fromEntries(Array.from({ length: 50 }, (_, index) => [`key-${index}`, `value-${index}`])),
  errors: []
})

const rowsWithFiftyOneActiveLabels = Array.from({ length: 51 }, (_, index) => ({
  key: `key-${index}`,
  value: `value-${index}`
}))
assert.deepEqual(validateResourceLabelRows(rowsWithFiftyOneActiveLabels), {
  ok: false,
  labels: {},
  errors: [{ index: -1, field: 'row', message: '最多允许 50 条标签' }]
})

assert.deepEqual(validateResourceLabelRows([
  { key: 'team', value: '' }
]), {
  ok: false,
  labels: {},
  errors: [{ index: 0, field: 'value', message: '标签值不能为空' }]
})

assert.equal(formatResourceLabel('team', 'platform'), 'team=platform')

const labelEditorSource = readFileSync(
  new URL('./components/ResourceLabelEditor.vue', import.meta.url),
  'utf8'
)
assert.doesNotMatch(labelEditorSource, /:key="`label-row-\$\{index\}`"/)
assert.match(labelEditorSource, /:key="row\.id"/)
assert.match(labelEditorSource, /createLabelRow/)

console.log('resource label tests passed')
