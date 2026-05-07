<template>
  <div class="matrix-panel">
    <div class="matrix-toolbar">
      <div class="matrix-title">GPU Matrix</div>
      <div class="matrix-meta">{{ rows.length }} 服务 / {{ columns.length }} GPU</div>
    </div>
    <GpuMatrixGrid v-if="rows.length && columns.length" :rows="rows" :columns="columns" :cells="cells" />
    <el-empty v-else description="暂无 GPU 分配" :image-size="64" />
  </div>
</template>

<script setup>
import GpuMatrixGrid from './GpuMatrixGrid.vue'

defineProps({
  rows: {
    type: Array,
    default: () => []
  },
  columns: {
    type: Array,
    default: () => []
  },
  cells: {
    type: Object,
    default: () => ({})
  }
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
