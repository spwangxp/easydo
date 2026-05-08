import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const readSource = (relativePath) => readFileSync(resolve(currentDir, relativePath), 'utf8')
const extractBlock = (source, selector) => {
  const start = source.indexOf(`${selector} {`)
  assert.notEqual(start, -1, `${selector} block should exist`)
  let depth = 0
  for (let index = start; index < source.length; index += 1) {
    if (source[index] === '{') depth += 1
    if (source[index] === '}') {
      depth -= 1
      if (depth === 0) return source.slice(start, index + 1)
    }
  }
  throw new Error(`${selector} block is not closed`)
}

const resourcesIndex = readSource('index.vue')
const gpuMatrixGrid = readSource('components/GpuMatrixGrid.vue')
const gpuHoverPopover = readSource('components/GpuHoverPopover.vue')
const k8sResourceTable = readSource('k8s/components/K8sResourceTable.vue')
const k8sAuditList = readSource('k8s/components/K8sAuditList.vue')

assert.doesNotMatch(resourcesIndex, /label="操作"\s+width="620"/)
assert.match(resourcesIndex, /class="resource-actions"/)
assert.match(resourcesIndex, /class="action-line action-line--primary"/)
assert.match(resourcesIndex, /class="action-line action-line--secondary"/)
assert.match(resourcesIndex, /<el-dropdown[^>]*class="more-actions"/)
assert.match(resourcesIndex, /<el-dropdown-item[^>]*command="edit"[^>]*>编辑<\/el-dropdown-item>/)
assert.match(resourcesIndex, /<el-dropdown-item[^>]*command="delete"[^>]*>删除<\/el-dropdown-item>/)
assert.match(resourcesIndex, /class="compact-table"/)
assert.match(resourcesIndex, /class="resource-identity-cell"/)
assert.match(resourcesIndex, /class="access-info-cell"/)
assert.match(resourcesIndex, /class="base-summary-cell"/)

assert.match(gpuMatrixGrid, /minmax\(136px,\s*1fr\)/)
assert.match(gpuMatrixGrid, /class="gpu-grid-scroll"/)
assert.match(gpuMatrixGrid, /-webkit-line-clamp:\s*2/)
assert.match(gpuMatrixGrid, /max-height:/)
assert.doesNotMatch(extractBlock(gpuMatrixGrid, '.gpu-segment'), /\n\s*span\s*\{/)

assert.match(gpuHoverPopover, /:width="popoverWidth"/)
assert.match(gpuHoverPopover, /max-width:\s*calc\(100vw - 32px\)/)
assert.match(gpuHoverPopover, /grid-template-columns:\s*minmax\(0,\s*1fr\)/)

assert.match(k8sResourceTable, /class="k8s-compact-table"/)
assert.match(k8sResourceTable, /class="clamp-two-lines resource-summary"/)
assert.match(k8sResourceTable, /overflow-wrap:\s*anywhere/)
assert.match(k8sAuditList, /class="audit-compact-table"/)
assert.match(k8sAuditList, /class="clamp-two-lines"/)
assert.match(k8sAuditList, /overflow-wrap:\s*anywhere/)

console.log('resources layout contract tests passed')
