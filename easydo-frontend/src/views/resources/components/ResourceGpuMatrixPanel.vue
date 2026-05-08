<template>
  <div class="matrix-panel">
    <div class="matrix-toolbar">
      <div class="matrix-title">GPU 视图</div>
      <div class="matrix-meta">{{ rows.length }} 节点 / {{ gpuCount }} GPU / {{ activeServiceCount }} 运行载体</div>
    </div>
    <GpuMatrixGrid v-if="rows.length" :rows="rows" />
    <el-empty v-else description="暂无 GPU 数据" :image-size="64" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import GpuMatrixGrid from './GpuMatrixGrid.vue'

const props = defineProps({
  rows: {
    type: Array,
    default: () => []
  }
})

const gpuCount = computed(() => props.rows.reduce((total, row) => total + (row.gpuCells?.length || 0), 0))
const activeServiceCount = computed(() => {
  const ids = new Set()
  props.rows.forEach(row => {
    ;(row.gpuCells || []).forEach(cell => {
      ;(cell.segments || []).forEach(segment => {
        if (segment?.service?.id) ids.add(segment.service.id)
      })
    })
  })
  return ids.size
})
</script>

<style lang="scss" scoped>
.matrix-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.matrix-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  font-size: 12px;
}

.matrix-title {
  font-weight: 700;
  color: var(--text-primary);
}

.matrix-meta {
  color: var(--text-secondary);
}
</style>
