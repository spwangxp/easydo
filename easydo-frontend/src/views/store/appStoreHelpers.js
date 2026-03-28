export function createParameterRow() {
  return {
    name: '',
    label: '',
    description: '',
    type: 'text',
    default_value: '',
    option_values: [],
    required: false,
    advanced: false,
    sort_order: 0
  }
}

export function normalizeParameterRows(parameters = []) {
  return [...parameters]
    .map((item, index) => ({
      name: item.name || '',
      label: item.label || item.name || '',
      description: item.description || '',
      type: item.type || 'text',
      default_value: item.default_value ?? '',
      option_values: normalizeOptionValues(item.option_values),
      required: Boolean(item.required),
      advanced: Boolean(item.advanced),
      sort_order: Number.isFinite(Number(item.sort_order)) ? Number(item.sort_order) : index + 1
    }))
    .sort((left, right) => left.sort_order - right.sort_order)
}

export function splitParametersByAdvanced(parameters = []) {
  const basic = []
  const advanced = []
  parameters.forEach((item) => {
    if (item.advanced) {
      advanced.push(item)
    } else {
      basic.push(item)
    }
  })
  return { basic, advanced }
}

export function normalizeOptionValues(optionValues) {
  if (Array.isArray(optionValues)) {
    return optionValues.filter((item) => item !== '')
  }
  if (typeof optionValues === 'string' && optionValues.trim()) {
    try {
      const parsed = JSON.parse(optionValues)
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return optionValues
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    }
  }
  return []
}

export function buildChartSourcePayload(form) {
  if (form.infra_type !== 'k8s') {
    return null
  }
  const source = {
    type: form.chart_source_type || 'repo',
    repo_url: form.chart_repo_url || '',
    oci_url: form.chart_oci_url || '',
    chart_name: form.chart_name || '',
    chart_version: form.chart_version || ''
  }
  if (form.chart_source_type === 'upload') {
    source.file_name = form.chart_file_name || ''
    source.object_key = form.chart_object_key || ''
  }
  return source
}
