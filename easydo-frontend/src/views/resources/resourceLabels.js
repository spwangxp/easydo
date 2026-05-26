export const MAX_RESOURCE_LABELS = 50
export const MAX_RESOURCE_LABEL_KEY_LENGTH = 64
export const MAX_RESOURCE_LABEL_VALUE_LENGTH = 256

/**
 * @param {unknown} value
 * @returns {string}
 */
function trimString(value) {
  return typeof value === 'string' ? value.trim() : ''
}

/**
 * @param {unknown} value
 * @returns {Record<string, unknown> | null}
 */
function parseLabelSource(value) {
  if (value == null || value === '') return null
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value)
      return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null
    } catch {
      return null
    }
  }

  return value && typeof value === 'object' && !Array.isArray(value) ? value : null
}

/**
 * @param {unknown} value
 * @returns {Record<string, string>}
 */
export function normalizeResourceLabels(value) {
  const source = parseLabelSource(value)
  if (!source) return {}

  return Object.entries(source).reduce((labels, [rawKey, rawValue]) => {
    if (typeof rawValue !== 'string') return labels

    const key = trimString(rawKey)
    const normalizedValue = trimString(rawValue)
    if (!key || !normalizedValue) return labels

    labels[key] = normalizedValue
    return labels
  }, /** @type {Record<string, string>} */ ({}))
}

/**
 * @param {Record<string, string>} labels
 * @returns {Array<{key: string, value: string}>}
 */
export function labelObjectToRows(labels = {}) {
  return Object.entries(normalizeResourceLabels(labels)).map(([key, value]) => ({ key, value }))
}

/**
 * @param {Array<{key?: unknown, value?: unknown}>} rows
 * @returns {Record<string, string>}
 */
export function labelRowsToObject(rows = []) {
  return rows.reduce((labels, row) => {
    const key = trimString(row?.key)
    const value = trimString(row?.value)
    if (!key || !value) return labels

    labels[key] = value
    return labels
  }, /** @type {Record<string, string>} */ ({}))
}

/**
 * @param {Array<{key?: unknown, value?: unknown}>} rows
 * @returns {{ok: boolean, labels: Record<string, string>, errors: Array<{index: number, field: 'key' | 'value' | 'row', message: string}>}}
 */
export function validateResourceLabelRows(rows = []) {
  const errors = []
  const labels = {}
  const seenKeys = new Set()
  const safeRows = Array.isArray(rows) ? rows : []
  const activeRows = safeRows.filter(row => trimString(row?.key) !== '' || trimString(row?.value) !== '')

  if (activeRows.length > MAX_RESOURCE_LABELS) {
    errors.push({ index: -1, field: 'row', message: `最多允许 ${MAX_RESOURCE_LABELS} 条标签` })
  }

  safeRows.forEach((row, index) => {
    const rawKey = trimString(row?.key)
    const rawValue = trimString(row?.value)
    const isActive = rawKey !== '' || rawValue !== ''

    if (!isActive) return

    if (!rawKey) {
      errors.push({ index, field: 'key', message: '标签键不能为空' })
      return
    }

    if (!rawValue) {
      errors.push({ index, field: 'value', message: '标签值不能为空' })
      return
    }

    if (rawKey.length > MAX_RESOURCE_LABEL_KEY_LENGTH) {
      errors.push({ index, field: 'key', message: `标签键长度不能超过 ${MAX_RESOURCE_LABEL_KEY_LENGTH}` })
    }

    if (rawValue.length > MAX_RESOURCE_LABEL_VALUE_LENGTH) {
      errors.push({ index, field: 'value', message: `标签值长度不能超过 ${MAX_RESOURCE_LABEL_VALUE_LENGTH}` })
    }

    if (seenKeys.has(rawKey)) {
      errors.push({ index, field: 'key', message: '标签键不能重复' })
      return
    }

    seenKeys.add(rawKey)
    labels[rawKey] = rawValue
  })

  return {
    ok: errors.length === 0,
    labels: errors.length === 0 ? labels : {},
    errors
  }
}

/**
 * @param {Array<{labels?: unknown}>} resources
 * @param {string} [labelKey='']
 * @returns {{keys: Array<string>, values: Array<string>}}
 */
export function getResourceLabelOptions(resources = [], labelKey = '') {
  const normalizedKey = trimString(labelKey)
  const keys = new Set()
  const values = new Set()

  resources.forEach(resource => {
    const labels = normalizeResourceLabels(resource?.labels)
    Object.entries(labels).forEach(([key, value]) => {
      keys.add(key)
      if (!normalizedKey || key === normalizedKey) {
        values.add(value)
      }
    })
  })

  return {
    keys: [...keys].sort((left, right) => left.localeCompare(right, 'en')),
    values: [...values].sort((left, right) => left.localeCompare(right, 'en'))
  }
}

/**
 * @param {{labels?: unknown}} resource
 * @param {{labelKey?: unknown, labelValue?: unknown}} filters
 * @returns {boolean}
 */
export function resourceMatchesLabelFilters(resource = {}, filters = {}) {
  const labels = normalizeResourceLabels(resource?.labels)
  const labelKey = trimString(filters?.labelKey)
  const labelValue = trimString(filters?.labelValue)

  if (!labelKey && !labelValue) return true
  if (labelKey && !labelValue) return Object.prototype.hasOwnProperty.call(labels, labelKey)
  if (!labelKey && labelValue) return Object.values(labels).some(value => value === labelValue)
  return labels[labelKey] === labelValue
}

/**
 * @param {unknown} key
 * @param {unknown} value
 * @returns {string}
 */
export function formatResourceLabel(key, value) {
  return `${trimString(key)}=${trimString(value)}`
}
