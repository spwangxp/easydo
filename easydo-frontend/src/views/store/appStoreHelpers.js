import { gunzipSync } from 'fflate'

const HELM_OCI_LAYER_MEDIA_TYPES = [
  'application/vnd.cncf.helm.chart.content.v1.tar+gzip',
  'application/tar+gzip',
  'application/gzip'
]

export function createParameterRow() {
  return {
    name: '',
    label: '',
    description: '',
    extra_tip: '',
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
      extra_tip: item.extra_tip || '',
      type: item.type || 'text',
      default_value: item.default_value ?? '',
      option_values: normalizeOptionValues(item.option_values),
      required: Boolean(item.required),
      advanced: Boolean(item.advanced),
      sort_order: Number.isFinite(Number(item.sort_order)) ? Number(item.sort_order) : index + 1
    }))
    .sort((left, right) => left.sort_order - right.sort_order)
}

export function extractCanonicalParameters(version = {}) {
  return normalizeParameterRows(Array.isArray(version?.parameters) ? version.parameters : [])
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
  return normalizeChartSourcePayload(form)
}

export function normalizeChartSourcePayload(form) {
  if (form.infra_type !== 'k8s') {
    return null
  }

  const type = (form.chart_source_type || 'repo').trim()
  const chartVersion = (form.chart_version || '').trim()
  const source = {
    type,
    repo_url: '',
    oci_url: '',
    chart_name: '',
    chart_version: chartVersion
  }

  if (type === 'repo') {
    source.repo_url = (form.chart_repo_url || '').trim()
    source.chart_name = (form.chart_name || '').trim()
  } else if (type === 'oci') {
    source.oci_url = (form.chart_oci_url || '').trim()
    source.chart_name = (form.chart_name || deriveChartNameFromOCIUrl(source.oci_url) || '').trim()
  } else if (type === 'upload') {
    source.chart_name = (form.chart_name || deriveChartNameFromFileName(form.chart_file_name) || '').trim()
    source.file_name = (form.chart_file_name || '').trim()
    source.object_key = (form.chart_object_key || '').trim()
  }

  return source
}

export function validateChartSourcePayload(payload) {
  if (!payload) {
    return '请选择 Chart 来源'
  }
  if (payload.type === 'repo') {
    if (!payload.repo_url) return '请填写 Repo URL'
    if (!payload.chart_name) return '请填写 Chart 名称'
    return ''
  }
  if (payload.type === 'oci') {
    if (!payload.oci_url) return '请填写 OCI URL'
    if (!payload.chart_name) return '请填写 Chart 名称'
    if (!payload.chart_version) return '请填写 Chart Version'
    return ''
  }
  if (payload.type === 'upload') {
    if (!payload.file_name) return '请先选择 Chart 文件'
    return ''
  }
  return 'Chart 来源类型无效'
}

export function deriveChartNameFromOCIUrl(url = '') {
  const normalized = url.trim().replace(/^oci:\/\//, '').replace(/:+[^/]+$/, '')
  const segments = normalized.split('/').filter(Boolean)
  return segments.at(-1) || ''
}

export function deriveChartNameFromFileName(fileName = '') {
  return fileName.trim().replace(/(\.tar\.gz|\.tgz|\.zip)$/i, '')
}

export async function resolveUploadChart(file) {
  if (!file) {
    throw new Error('请先选择 Chart 文件')
  }
  const fileName = file.name || ''
  if (!/\.(tgz|tar\.gz)$/i.test(fileName)) {
    throw new Error('当前仅支持解析 .tgz / .tar.gz Helm Chart 文件')
  }
  const arrayBuffer = await file.arrayBuffer()
  const valuesYAML = extractValuesYAMLFromChartArchive(new Uint8Array(arrayBuffer))
  return {
    chartName: deriveChartNameFromFileName(fileName),
    chartFile: file,
    chartVersion: '',
    fileName,
    valuesYAML
  }
}

export async function resolveChartSource(source, options = {}) {
  const sourceType = (source?.sourceType || source?.type || '').trim()
  if (sourceType === 'upload') {
    return resolveUploadChart(source.file)
  }
  if (sourceType === 'repo') {
    return resolveRepoChart(source, options)
  }
  if (sourceType === 'oci') {
    return resolveOCIChart(source, options)
  }
  throw new Error('不支持的 Chart 来源类型')
}

export async function resolveRepoChart(source, options = {}) {
  const repoUrl = (source.repoUrl || source.repo_url || '').trim().replace(/\/$/, '')
  const chartName = (source.chartName || source.chart_name || '').trim()
  const chartVersion = (source.chartVersion || source.chart_version || '').trim()
  if (!repoUrl || !chartName || !chartVersion) {
    throw new Error('Repo Chart 解析参数不完整')
  }
  const fetchImpl = options.fetchImpl || fetch
  const indexResponse = await fetchWithError(fetchImpl, `${repoUrl}/index.yaml`, '获取 Helm Repo index.yaml 失败')
  const indexText = await indexResponse.text()
  const chartDownloadURL = extractHelmChartDownloadURL(indexText, { repoUrl, chartName, chartVersion })
  const archiveResponse = await fetchWithError(fetchImpl, chartDownloadURL, '下载 Repo Chart 包失败')
  const chartBytes = new Uint8Array(await archiveResponse.arrayBuffer())
  const fileName = deriveFileNameFromURL(chartDownloadURL, `${chartName}-${chartVersion}.tgz`)
  return buildResolvedChartResult(chartBytes, fileName, chartName, chartVersion)
}

export function extractHelmChartDownloadURL(indexText, options) {
  const { repoUrl, chartName, chartVersion } = options
  const index = parseSimpleYAML(indexText)
  const entries = index?.entries?.[chartName]
  if (!Array.isArray(entries) || entries.length === 0) {
    throw new Error(`Repo 中未找到 Chart：${chartName}`)
  }
  const matched = entries.find((entry) => String(entry.version || '').trim() === chartVersion)
  if (!matched) {
    throw new Error(`Repo 中未找到版本：${chartName}@${chartVersion}`)
  }
  const rawURL = Array.isArray(matched.urls) ? matched.urls.find(Boolean) : ''
  if (!rawURL) {
    throw new Error(`Chart ${chartName}@${chartVersion} 未提供下载地址`)
  }
  return buildRepoChartDownloadURL(repoUrl, rawURL)
}

export function buildRepoChartDownloadURL(repoUrl, rawURL) {
  try {
    return new URL(rawURL, `${repoUrl.replace(/\/$/, '')}/`).toString()
  } catch {
    throw new Error('Chart 下载地址无效')
  }
}

export async function resolveOCIChart(source, options = {}) {
  const parsed = parseOCIChartReference(source.ociUrl || source.oci_url || '', source.chartVersion || source.chart_version || '')
  const fetchImpl = options.fetchImpl || fetch
  const token = await resolveOCIRegistryToken(fetchImpl, parsed)
  const manifestURL = `https://${parsed.registry}/v2/${parsed.repository}/manifests/${parsed.reference}`
  const manifestResponse = await fetchWithError(fetchImpl, manifestURL, '获取 OCI Chart manifest 失败', {
    headers: buildOCIHeaders(token)
  })
  const manifest = await manifestResponse.json()
  const chartLayer = selectOCIChartLayer(manifest)
  const blobURL = `https://${parsed.registry}/v2/${parsed.repository}/blobs/${chartLayer.digest}`
  const blobResponse = await fetchWithError(fetchImpl, blobURL, '下载 OCI Chart blob 失败', {
    headers: buildOCIHeaders(token)
  })
  const chartBytes = new Uint8Array(await blobResponse.arrayBuffer())
  const fileName = `${parsed.chartName}-${parsed.reference}.tgz`
  return buildResolvedChartResult(chartBytes, fileName, parsed.chartName, parsed.reference)
}

export function parseOCIChartReference(ociUrl, version) {
  const normalized = String(ociUrl || '').trim().replace(/^oci:\/\//, '')
  const segments = normalized.split('/').filter(Boolean)
  if (segments.length < 2) {
    throw new Error('OCI URL 无效')
  }
  const registry = segments[0]
  const repository = segments.slice(1).join('/')
  const chartName = segments.at(-1) || ''
  const reference = String(version || '').trim()
  if (!reference) {
    throw new Error('OCI Chart Version 不能为空')
  }
  return { registry, repository, chartName, reference }
}

async function resolveOCIRegistryToken(fetchImpl, parsed) {
  if (parsed.registry !== 'registry-1.docker.io') {
    return ''
  }
  const tokenURL = `https://auth.docker.io/token?service=registry.docker.io&scope=repository:${parsed.repository}:pull`
  const response = await fetchWithError(fetchImpl, tokenURL, '获取 OCI Registry Token 失败')
  const payload = await response.json()
  return String(payload.token || payload.access_token || '').trim()
}

function buildOCIHeaders(token) {
  return {
    Accept: 'application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.cncf.helm.chart.content.v1.tar+gzip',
    ...(token ? { Authorization: `Bearer ${token}` } : {})
  }
}

function selectOCIChartLayer(manifest) {
  const layer = Array.isArray(manifest?.layers)
    ? manifest.layers.find((item) => HELM_OCI_LAYER_MEDIA_TYPES.includes(String(item.mediaType || '').trim()))
    : null
  if (!layer?.digest) {
    throw new Error('OCI manifest 中未找到 Helm Chart layer')
  }
  return layer
}

function buildResolvedChartResult(chartBytes, fileName, chartName, chartVersion) {
  const valuesYAML = extractValuesYAMLFromChartArchive(chartBytes)
  return {
    chartName,
    chartVersion,
    chartFile: new File([chartBytes], fileName, { type: 'application/gzip' }),
    fileName,
    valuesYAML
  }
}

function deriveFileNameFromURL(url, fallback) {
  try {
    const pathname = new URL(url).pathname
    const name = pathname.split('/').filter(Boolean).at(-1)
    return name || fallback
  } catch {
    return fallback
  }
}

async function fetchWithError(fetchImpl, url, errorMessage, init = {}) {
  let response
  try {
    response = await fetchImpl(url, init)
  } catch (error) {
    throw new Error(error?.message || errorMessage)
  }
  if (!response?.ok) {
    throw new Error(`${errorMessage}（HTTP ${response?.status || 'unknown'}）`)
  }
  return response
}

function parseSimpleYAML(text) {
  const root = {}
  const stack = [{ indent: -1, value: root }]
  const lines = String(text || '').split(/\r?\n/)
  for (const rawLine of lines) {
    if (!rawLine.trim() || rawLine.trimStart().startsWith('#')) continue
    const indent = rawLine.match(/^\s*/)?.[0].length || 0
    const line = rawLine.trim()
    while (stack.length > 1 && indent <= stack[stack.length - 1].indent) {
      stack.pop()
    }
    const parent = stack[stack.length - 1].value
    if (line.startsWith('- ')) {
      const itemText = line.slice(2)
      if (!Array.isArray(parent)) {
        throw new Error('暂不支持的 YAML 结构')
      }
      if (itemText.includes(':')) {
        const [key, ...rest] = itemText.split(':')
        const valueText = rest.join(':').trim()
        const item = {}
        parent.push(item)
        if (valueText) {
          item[key.trim()] = parseSimpleYAMLScalar(valueText)
          stack.push({ indent, value: item })
        } else {
          item[key.trim()] = {}
          stack.push({ indent, value: item[key.trim()] })
        }
      } else {
        parent.push(parseSimpleYAMLScalar(itemText))
      }
      continue
    }
    const [key, ...rest] = line.split(':')
    const valueText = rest.join(':').trim()
    if (valueText) {
      parent[key.trim()] = parseSimpleYAMLScalar(valueText)
      continue
    }
    const nextRawLine = lines[lines.indexOf(rawLine) + 1] || ''
    const nextTrimmed = nextRawLine.trim()
    const nextIndent = nextRawLine.match(/^\s*/)?.[0].length || 0
    const container = nextTrimmed.startsWith('- ') && nextIndent > indent ? [] : {}
    parent[key.trim()] = container
    stack.push({ indent, value: container })
  }
  return root
}

function parseSimpleYAMLScalar(value) {
  if (value === 'true') return true
  if (value === 'false') return false
  if (/^-?\d+(\.\d+)?$/.test(value)) return Number(value)
  return value.replace(/^['"]|['"]$/g, '')
}

export function extractValuesYAMLFromChartArchive(compressedBytes) {
  const archiveBytes = gunzipSync(compressedBytes)
  const valuesEntry = readTarEntries(archiveBytes).find((entry) => /(?:^|\/)values\.ya?ml$/i.test(entry.name))
  if (!valuesEntry) {
    throw new Error('Chart 包中未找到 values.yaml')
  }
  return new TextDecoder().decode(valuesEntry.body)
}

function readTarEntries(bytes) {
  const entries = []
  let offset = 0
  while (offset + 512 <= bytes.length) {
    const header = bytes.subarray(offset, offset + 512)
    if (isEmptyTarBlock(header)) {
      break
    }
    const name = decodeTarText(header.subarray(0, 100))
    const sizeText = decodeTarText(header.subarray(124, 136)).replace(/\0/g, '').trim()
    const size = sizeText ? Number.parseInt(sizeText, 8) : 0
    const bodyStart = offset + 512
    const bodyEnd = bodyStart + size
    entries.push({
      name,
      body: bytes.slice(bodyStart, bodyEnd)
    })
    offset = bodyStart + Math.ceil(size / 512) * 512
  }
  return entries
}

function decodeTarText(bytes) {
  return new TextDecoder().decode(bytes).replace(/\0.*$/, '').trim()
}

function isEmptyTarBlock(bytes) {
  return bytes.every((item) => item === 0)
}
