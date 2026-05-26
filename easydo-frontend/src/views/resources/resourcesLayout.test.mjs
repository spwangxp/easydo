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
const resourceForm = readSource('components/ResourceForm.vue')
const resourceLabelEditor = readSource('components/ResourceLabelEditor.vue')
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
assert.match(resourcesIndex, /label="标签"/)
assert.match(resourcesIndex, /class="resource-label-cell"/)
assert.match(resourcesIndex, /labelKey/)
assert.match(resourcesIndex, /labelValue/)
assert.match(resourcesIndex, /标签键/)
assert.match(resourcesIndex, /标签值/)
assert.match(resourcesIndex, /resource-label-tags/)
assert.match(resourcesIndex, /resourceMatchesLabelFilters/)
assert.match(resourcesIndex, /openLabelDialog/)
assert.match(resourcesIndex, /saveLabelDialog/)
assert.match(resourcesIndex, /updateResourceLabels/)
assert.match(resourcesIndex, /编辑标签/)

assert.match(gpuMatrixGrid, /grid-template-columns:\s*128px\s+minmax\(0,\s*1fr\)/)
assert.match(gpuMatrixGrid, /minmax\(136px,\s*1fr\)/)
assert.match(gpuMatrixGrid, /--gpu-summary-row-height:\s*62px/)
assert.match(extractBlock(gpuMatrixGrid, '.node-card'), /height:\s*var\(--gpu-summary-row-height\)/)
assert.match(extractBlock(gpuMatrixGrid, '.gpu-cell'), /height:\s*var\(--gpu-summary-row-height\)/)
assert.match(gpuMatrixGrid, /class="gpu-grid-scroll"/)
assert.match(gpuMatrixGrid, /-webkit-line-clamp:\s*2/)
assert.doesNotMatch(extractBlock(gpuMatrixGrid, '.gpu-segment'), /\n\s*span\s*\{/)

assert.match(gpuHoverPopover, /:width="popoverWidth"/)
assert.match(gpuHoverPopover, /max-width:\s*calc\(100vw - 32px\)/)
assert.match(gpuHoverPopover, /grid-template-columns:\s*minmax\(0,\s*1fr\)/)

assert.match(resourceLabelEditor, /defineProps\s*\(/)
assert.match(resourceLabelEditor, /modelValue/)
assert.match(resourceLabelEditor, /update:modelValue/)
assert.match(resourceLabelEditor, /defineExpose\s*\(\s*\{\s*validate\s*\}\s*\)/)
assert.match(resourceLabelEditor, /添加标签/)
assert.match(resourceLabelEditor, /标签键/)
assert.match(resourceLabelEditor, /标签值/)
assert.match(resourceLabelEditor, /resource-label-editor/)

assert.match(resourceForm, /ResourceLabelEditor/)
assert.match(resourceForm, /资源标签/)
assert.match(resourceForm, /ref="labelEditorRef"/)
assert.match(resourceForm, /labelEditorRef\.value\?\.\s*validate/)

assert.match(k8sResourceTable, /class="k8s-compact-table"/)
assert.match(k8sResourceTable, /class="clamp-two-lines resource-summary"/)
assert.match(k8sResourceTable, /overflow-wrap:\s*anywhere/)
assert.match(k8sAuditList, /class="audit-compact-table"/)
assert.match(k8sAuditList, /class="clamp-two-lines"/)
assert.match(k8sAuditList, /overflow-wrap:\s*anywhere/)

console.log('resources layout contract tests passed')
